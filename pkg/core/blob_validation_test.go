package core

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// newBlobTx creates a blob transaction with the given hashes and fee cap.
func newBlobTx(blobHashes []types.Hash, blobFeeCap *big.Int) *types.Transaction {
	return types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1337),
		Nonce:      0,
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(1_000_000_000),
		Gas:        21000,
		To:         types.HexToAddress("0xdead"),
		Value:      big.NewInt(0),
		BlobFeeCap: blobFeeCap,
		BlobHashes: blobHashes,
	})
}

// validBlobHash returns a blob hash with the correct version byte.
func validBlobHash(suffix byte) types.Hash {
	var h types.Hash
	h[0] = BlobTxHashVersion
	h[31] = suffix
	return h
}

func TestValidateBlobTx_Valid(t *testing.T) {
	hashes := []types.Hash{validBlobHash(0x01)}
	// With zero excess blob gas, base fee = 1 (minimum). Set fee cap to 1.
	tx := newBlobTx(hashes, big.NewInt(1))

	if err := ValidateBlobTx(tx, 0); err != nil {
		t.Fatalf("expected valid blob tx, got error: %v", err)
	}
}

func TestValidateBlobTx_MultipleBlobs(t *testing.T) {
	hashes := make([]types.Hash, MaxBlobsPerBlock)
	for i := range hashes {
		hashes[i] = validBlobHash(byte(i + 1))
	}
	tx := newBlobTx(hashes, big.NewInt(1))

	if err := ValidateBlobTx(tx, 0); err != nil {
		t.Fatalf("expected valid blob tx with %d blobs, got error: %v", MaxBlobsPerBlock, err)
	}
}

func TestValidateBlobTx_NoBlobHashes(t *testing.T) {
	tx := newBlobTx(nil, big.NewInt(1))

	err := ValidateBlobTx(tx, 0)
	if err == nil {
		t.Fatal("expected error for blob tx with no blob hashes")
	}
	if !containsError(err, ErrBlobTxNoBlobHashes) {
		t.Fatalf("expected ErrBlobTxNoBlobHashes, got: %v", err)
	}
}

func TestValidateBlobTx_EmptyBlobHashes(t *testing.T) {
	tx := newBlobTx([]types.Hash{}, big.NewInt(1))

	err := ValidateBlobTx(tx, 0)
	if err == nil {
		t.Fatal("expected error for blob tx with empty blob hashes")
	}
	if !containsError(err, ErrBlobTxNoBlobHashes) {
		t.Fatalf("expected ErrBlobTxNoBlobHashes, got: %v", err)
	}
}

func TestValidateBlobTx_TooManyBlobs(t *testing.T) {
	hashes := make([]types.Hash, MaxBlobsPerBlock+1)
	for i := range hashes {
		hashes[i] = validBlobHash(byte(i))
	}
	tx := newBlobTx(hashes, big.NewInt(1))

	err := ValidateBlobTx(tx, 0)
	if err == nil {
		t.Fatal("expected error for blob tx with too many blobs")
	}
	if !containsError(err, ErrBlobTxTooManyBlobs) {
		t.Fatalf("expected ErrBlobTxTooManyBlobs, got: %v", err)
	}
}

func TestValidateBlobTx_InvalidVersionByte(t *testing.T) {
	var badHash types.Hash
	badHash[0] = 0x00 // wrong version
	badHash[31] = 0x01

	tx := newBlobTx([]types.Hash{badHash}, big.NewInt(1))

	err := ValidateBlobTx(tx, 0)
	if err == nil {
		t.Fatal("expected error for invalid blob hash version")
	}
	if !containsError(err, ErrBlobTxInvalidHashVersion) {
		t.Fatalf("expected ErrBlobTxInvalidHashVersion, got: %v", err)
	}
}

func TestValidateBlobTx_SecondHashInvalid(t *testing.T) {
	good := validBlobHash(0x01)
	var bad types.Hash
	bad[0] = 0x02 // wrong version
	bad[31] = 0x02

	tx := newBlobTx([]types.Hash{good, bad}, big.NewInt(1))

	err := ValidateBlobTx(tx, 0)
	if err == nil {
		t.Fatal("expected error for second invalid blob hash")
	}
	if !containsError(err, ErrBlobTxInvalidHashVersion) {
		t.Fatalf("expected ErrBlobTxInvalidHashVersion, got: %v", err)
	}
}

func TestValidateBlobTx_FeeCapTooLow(t *testing.T) {
	hashes := []types.Hash{validBlobHash(0x01)}
	// Use a large excess blob gas to create a high blob base fee.
	// With excess = 10_000_000, the base fee will be significantly above 1.
	tx := newBlobTx(hashes, big.NewInt(1))

	err := ValidateBlobTx(tx, 10_000_000)
	if err == nil {
		t.Fatal("expected error for blob fee cap too low")
	}
	if !containsError(err, ErrBlobFeeCapTooLow) {
		t.Fatalf("expected ErrBlobFeeCapTooLow, got: %v", err)
	}
}

func TestValidateBlobTx_FeeCapExact(t *testing.T) {
	hashes := []types.Hash{validBlobHash(0x01)}
	// Calculate the exact blob base fee and set fee cap to match.
	excessBlobGas := uint64(393216) // equal to TargetBlobGasPerBlock
	baseFee := CalcBlobBaseFee(excessBlobGas)
	tx := newBlobTx(hashes, baseFee)

	if err := ValidateBlobTx(tx, excessBlobGas); err != nil {
		t.Fatalf("expected valid blob tx with exact fee cap, got error: %v", err)
	}
}

func TestValidateBlobTx_NilFeeCap(t *testing.T) {
	hashes := []types.Hash{validBlobHash(0x01)}
	tx := newBlobTx(hashes, nil)

	err := ValidateBlobTx(tx, 0)
	if err == nil {
		t.Fatal("expected error for nil blob fee cap")
	}
	if !containsError(err, ErrBlobFeeCapTooLow) {
		t.Fatalf("expected ErrBlobFeeCapTooLow, got: %v", err)
	}
}

func TestCalcExcessBlobGas(t *testing.T) {
	tests := []struct {
		name       string
		parentExcess uint64
		parentUsed   uint64
		want         uint64
	}{
		{
			name:       "zero parent values",
			parentExcess: 0,
			parentUsed:   0,
			want:         0,
		},
		{
			name:       "below target returns zero",
			parentExcess: 0,
			parentUsed:   GasPerBlob, // 1 blob = 131072
			want:         0,
		},
		{
			name:       "exactly at target returns zero",
			parentExcess: 0,
			parentUsed:   TargetBlobGasPerBlock,
			want:         0,
		},
		{
			name:       "one blob above target",
			parentExcess: 0,
			parentUsed:   TargetBlobGasPerBlock + GasPerBlob,
			want:         GasPerBlob,
		},
		{
			name:       "full block above target",
			parentExcess: 0,
			parentUsed:   MaxBlobGasPerBlock,
			want:         MaxBlobGasPerBlock - TargetBlobGasPerBlock,
		},
		{
			name:       "carry forward excess",
			parentExcess: GasPerBlob * 2,
			parentUsed:   TargetBlobGasPerBlock,
			want:         GasPerBlob * 2,
		},
		{
			name:       "excess decreases with low usage",
			parentExcess: GasPerBlob * 2,
			parentUsed:   0,
			want:         0, // 2*131072 = 262144 < 393216 = target
		},
		{
			name:       "excess partially consumed",
			parentExcess: TargetBlobGasPerBlock,
			parentUsed:   GasPerBlob, // 131072
			want:         GasPerBlob, // 393216 + 131072 - 393216 = 131072
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcExcessBlobGas(tt.parentExcess, tt.parentUsed)
			if got != tt.want {
				t.Errorf("CalcExcessBlobGas(%d, %d) = %d, want %d",
					tt.parentExcess, tt.parentUsed, got, tt.want)
			}
		})
	}
}

func TestCountBlobGas(t *testing.T) {
	tests := []struct {
		name   string
		nBlobs int
		want   uint64
	}{
		{"zero blobs (non-blob tx)", 0, 0},
		{"one blob", 1, GasPerBlob},
		{"three blobs", 3, 3 * GasPerBlob},
		{"max blobs", MaxBlobsPerBlock, MaxBlobGasPerBlock},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hashes []types.Hash
			for i := 0; i < tt.nBlobs; i++ {
				hashes = append(hashes, validBlobHash(byte(i)))
			}

			var tx *types.Transaction
			if tt.nBlobs > 0 {
				tx = newBlobTx(hashes, big.NewInt(1))
			} else {
				// Non-blob transaction.
				tx = newTransferTx(0, types.HexToAddress("0xdead"), big.NewInt(0), 21000, big.NewInt(1))
			}

			got := CountBlobGas(tx)
			if got != tt.want {
				t.Errorf("CountBlobGas() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidateBlockBlobGas_Valid(t *testing.T) {
	parentBlobGasUsed := uint64(GasPerBlob * 3) // 3 blobs
	parentExcessBlobGas := uint64(0)
	expectedExcess := CalcExcessBlobGas(parentExcessBlobGas, parentBlobGasUsed)

	blobGasUsed := uint64(GasPerBlob * 2) // 2 blobs in current block
	header := &types.Header{
		Number:        big.NewInt(2),
		BlobGasUsed:   &blobGasUsed,
		ExcessBlobGas: &expectedExcess,
	}

	parentHeader := &types.Header{
		Number:        big.NewInt(1),
		BlobGasUsed:   &parentBlobGasUsed,
		ExcessBlobGas: &parentExcessBlobGas,
	}

	if err := ValidateBlockBlobGas(header, parentHeader); err != nil {
		t.Fatalf("expected valid block blob gas, got error: %v", err)
	}
}

func TestValidateBlockBlobGas_NilBlobGasUsed(t *testing.T) {
	excess := uint64(0)
	header := &types.Header{
		Number:        big.NewInt(2),
		BlobGasUsed:   nil,
		ExcessBlobGas: &excess,
	}
	parentHeader := &types.Header{
		Number: big.NewInt(1),
	}

	err := ValidateBlockBlobGas(header, parentHeader)
	if err == nil {
		t.Fatal("expected error for nil BlobGasUsed")
	}
	if !containsError(err, ErrBlobGasUsedNil) {
		t.Fatalf("expected ErrBlobGasUsedNil, got: %v", err)
	}
}

func TestValidateBlockBlobGas_ExceedsMaximum(t *testing.T) {
	overMax := uint64(MaxBlobGasPerBlock + 1)
	excess := uint64(0)
	header := &types.Header{
		Number:        big.NewInt(2),
		BlobGasUsed:   &overMax,
		ExcessBlobGas: &excess,
	}
	parentHeader := &types.Header{
		Number: big.NewInt(1),
	}

	err := ValidateBlockBlobGas(header, parentHeader)
	if err == nil {
		t.Fatal("expected error for BlobGasUsed exceeding maximum")
	}
	if !containsError(err, ErrBlobGasUsedExceeded) {
		t.Fatalf("expected ErrBlobGasUsedExceeded, got: %v", err)
	}
}

func TestValidateBlockBlobGas_NilExcessBlobGas(t *testing.T) {
	used := uint64(0)
	header := &types.Header{
		Number:        big.NewInt(2),
		BlobGasUsed:   &used,
		ExcessBlobGas: nil,
	}
	parentHeader := &types.Header{
		Number: big.NewInt(1),
	}

	err := ValidateBlockBlobGas(header, parentHeader)
	if err == nil {
		t.Fatal("expected error for nil ExcessBlobGas")
	}
	if !containsError(err, ErrExcessBlobGasNil) {
		t.Fatalf("expected ErrExcessBlobGasNil, got: %v", err)
	}
}

func TestValidateBlockBlobGas_ExcessMismatch(t *testing.T) {
	parentUsed := uint64(MaxBlobGasPerBlock) // full block
	parentExcess := uint64(0)
	expected := CalcExcessBlobGas(parentExcess, parentUsed)

	wrongExcess := expected + 1
	used := uint64(0)
	header := &types.Header{
		Number:        big.NewInt(2),
		BlobGasUsed:   &used,
		ExcessBlobGas: &wrongExcess,
	}
	parentHeader := &types.Header{
		Number:        big.NewInt(1),
		BlobGasUsed:   &parentUsed,
		ExcessBlobGas: &parentExcess,
	}

	err := ValidateBlockBlobGas(header, parentHeader)
	if err == nil {
		t.Fatal("expected error for excess blob gas mismatch")
	}
	if !containsError(err, ErrExcessBlobGasMismatch) {
		t.Fatalf("expected ErrExcessBlobGasMismatch, got: %v", err)
	}
}

func TestValidateBlockBlobGas_ZeroBlobGasUsed(t *testing.T) {
	used := uint64(0)
	excess := uint64(0) // CalcExcessBlobGas(0, 0) = 0
	header := &types.Header{
		Number:        big.NewInt(2),
		BlobGasUsed:   &used,
		ExcessBlobGas: &excess,
	}
	parentHeader := &types.Header{
		Number: big.NewInt(1),
	}

	if err := ValidateBlockBlobGas(header, parentHeader); err != nil {
		t.Fatalf("expected valid block with zero blob gas, got error: %v", err)
	}
}

func TestValidateBlockBlobGas_MaxBlobGasUsed(t *testing.T) {
	parentUsed := uint64(MaxBlobGasPerBlock)
	parentExcess := uint64(0)
	expectedExcess := CalcExcessBlobGas(parentExcess, parentUsed)

	used := uint64(MaxBlobGasPerBlock)
	header := &types.Header{
		Number:        big.NewInt(2),
		BlobGasUsed:   &used,
		ExcessBlobGas: &expectedExcess,
	}
	parentHeader := &types.Header{
		Number:        big.NewInt(1),
		BlobGasUsed:   &parentUsed,
		ExcessBlobGas: &parentExcess,
	}

	if err := ValidateBlockBlobGas(header, parentHeader); err != nil {
		t.Fatalf("expected valid block with max blob gas, got error: %v", err)
	}
}

func TestValidateBlockBlobGas_ParentNilFields(t *testing.T) {
	// Parent has nil ExcessBlobGas and BlobGasUsed (pre-Cancun parent).
	// CalcExcessBlobGas(0, 0) = 0, so excess should be 0.
	used := uint64(GasPerBlob)
	excess := uint64(0)
	header := &types.Header{
		Number:        big.NewInt(2),
		BlobGasUsed:   &used,
		ExcessBlobGas: &excess,
	}
	parentHeader := &types.Header{
		Number:        big.NewInt(1),
		BlobGasUsed:   nil,
		ExcessBlobGas: nil,
	}

	if err := ValidateBlockBlobGas(header, parentHeader); err != nil {
		t.Fatalf("expected valid block with pre-Cancun parent, got error: %v", err)
	}
}

func TestCalcBlobBaseFee(t *testing.T) {
	// Zero excess blob gas should give minimum base fee of 1.
	fee := CalcBlobBaseFee(0)
	if fee.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected blob base fee 1 with zero excess, got %v", fee)
	}

	// A large excess blob gas should yield a blob base fee greater than 1.
	// Use a value large enough that the exponential formula produces > 1.
	// With excess = 10 * BLOB_BASE_FEE_UPDATE_FRACTION (33384770),
	// fee = e^10 ~ 22026, so clearly > 1.
	fee2 := CalcBlobBaseFee(33384770)
	if fee2.Cmp(big.NewInt(1)) <= 0 {
		t.Fatalf("expected blob base fee > 1 with large excess, got %v", fee2)
	}

	// Monotonicity: higher excess should yield higher or equal fee.
	fee3 := CalcBlobBaseFee(33384770 * 2)
	if fee3.Cmp(fee2) < 0 {
		t.Fatalf("expected monotonically increasing blob base fee, got %v < %v", fee3, fee2)
	}
}

// containsError checks if err matches or wraps the target error.
func containsError(err, target error) bool {
	return err == target || (err != nil && target != nil && (err.Error() == target.Error() || containsWrapped(err, target)))
}

func containsWrapped(err, target error) bool {
	for {
		if err == target {
			return true
		}
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			// Check if the error message contains the target message.
			return err != nil && target != nil && len(target.Error()) > 0 &&
				len(err.Error()) >= len(target.Error()) &&
				containsString(err.Error(), target.Error())
		}
		err = unwrapped
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
