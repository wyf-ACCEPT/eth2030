package engine

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

func TestNewBlobValidator(t *testing.T) {
	v := NewBlobValidator()
	if v.maxBlobsPerBlock != 6 {
		t.Errorf("expected maxBlobsPerBlock=6, got %d", v.maxBlobsPerBlock)
	}
	if v.targetBlobsPerBlock != 3 {
		t.Errorf("expected targetBlobsPerBlock=3, got %d", v.targetBlobsPerBlock)
	}
	if v.blobGasPerBlob != types.BlobTxBlobGasPerBlob {
		t.Errorf("expected blobGasPerBlob=%d, got %d", types.BlobTxBlobGasPerBlob, v.blobGasPerBlob)
	}
}

func TestNewBlobValidatorWithConfig(t *testing.T) {
	v := NewBlobValidatorWithConfig(12, 6, 200000)
	if v.MaxBlobsPerBlock() != 12 {
		t.Errorf("expected 12, got %d", v.MaxBlobsPerBlock())
	}
	if v.TargetBlobsPerBlock() != 6 {
		t.Errorf("expected 6, got %d", v.TargetBlobsPerBlock())
	}
	if v.BlobGasPerBlob() != 200000 {
		t.Errorf("expected 200000, got %d", v.BlobGasPerBlob())
	}
}

func makeFixedSidecar(index uint64) FixedBlobSidecar {
	sc := FixedBlobSidecar{BlobIndex: index}
	// Set a non-zero KZG commitment.
	sc.KZGCommitment[0] = 0xAB
	sc.KZGCommitment[1] = byte(index + 1)
	return sc
}

func TestValidateBlobSidecars_Valid(t *testing.T) {
	v := NewBlobValidator()
	sidecars := []FixedBlobSidecar{
		makeFixedSidecar(0),
		makeFixedSidecar(1),
		makeFixedSidecar(2),
	}
	blockHash := [32]byte{0x01}
	if err := v.ValidateBlobSidecars(sidecars, blockHash); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateBlobSidecars_Empty(t *testing.T) {
	v := NewBlobValidator()
	if err := v.ValidateBlobSidecars([]FixedBlobSidecar{}, [32]byte{}); err != nil {
		t.Errorf("unexpected error for empty sidecars: %v", err)
	}
}

func TestValidateBlobSidecars_Nil(t *testing.T) {
	v := NewBlobValidator()
	err := v.ValidateBlobSidecars(nil, [32]byte{})
	if err != ErrBlobSidecarsNil {
		t.Errorf("expected ErrBlobSidecarsNil, got %v", err)
	}
}

func TestValidateBlobSidecars_TooMany(t *testing.T) {
	v := NewBlobValidator()
	sidecars := make([]FixedBlobSidecar, 7)
	for i := range sidecars {
		sidecars[i] = makeFixedSidecar(uint64(i))
	}
	err := v.ValidateBlobSidecars(sidecars, [32]byte{})
	if err == nil {
		t.Error("expected error for too many sidecars")
	}
}

func TestValidateBlobSidecars_OutOfOrder(t *testing.T) {
	v := NewBlobValidator()
	sidecars := []FixedBlobSidecar{
		makeFixedSidecar(1), // should be 0
		makeFixedSidecar(0), // should be 1
	}
	err := v.ValidateBlobSidecars(sidecars, [32]byte{})
	if err == nil {
		t.Error("expected error for out-of-order indices")
	}
}

func TestValidateBlobSidecars_EmptyCommitment(t *testing.T) {
	v := NewBlobValidator()
	sidecars := []FixedBlobSidecar{
		{BlobIndex: 0, KZGCommitment: [48]byte{}}, // zero commitment
	}
	err := v.ValidateBlobSidecars(sidecars, [32]byte{})
	if err == nil {
		t.Error("expected error for empty KZG commitment")
	}
}

func TestValidateKZGCommitments_Valid(t *testing.T) {
	v := NewBlobValidator()
	commitments := [][48]byte{{0x01}, {0x02}}
	blobs := [][131072]byte{{}, {}}
	if err := v.ValidateKZGCommitments(commitments, blobs); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateKZGCommitments_NilCommitments(t *testing.T) {
	v := NewBlobValidator()
	err := v.ValidateKZGCommitments(nil, [][131072]byte{{}})
	if err != ErrBlobCommitmentsNil {
		t.Errorf("expected ErrBlobCommitmentsNil, got %v", err)
	}
}

func TestValidateKZGCommitments_NilBlobs(t *testing.T) {
	v := NewBlobValidator()
	err := v.ValidateKZGCommitments([][48]byte{{0x01}}, nil)
	if err != ErrBlobBlobsNil {
		t.Errorf("expected ErrBlobBlobsNil, got %v", err)
	}
}

func TestValidateKZGCommitments_CountMismatch(t *testing.T) {
	v := NewBlobValidator()
	err := v.ValidateKZGCommitments([][48]byte{{0x01}}, [][131072]byte{{}, {}})
	if err == nil {
		t.Error("expected error for count mismatch")
	}
}

func TestValidateKZGCommitments_EmptyCommitment(t *testing.T) {
	v := NewBlobValidator()
	err := v.ValidateKZGCommitments([][48]byte{{}}, [][131072]byte{{}})
	if err == nil {
		t.Error("expected error for empty commitment")
	}
}

func TestComputeBlobGas(t *testing.T) {
	tests := []struct {
		numBlobs int
		expected uint64
	}{
		{0, 0},
		{1, types.BlobTxBlobGasPerBlob},
		{3, 3 * types.BlobTxBlobGasPerBlob},
		{6, 6 * types.BlobTxBlobGasPerBlob},
	}
	for _, tt := range tests {
		got := ComputeBlobGas(tt.numBlobs)
		if got != tt.expected {
			t.Errorf("ComputeBlobGas(%d) = %d, want %d", tt.numBlobs, got, tt.expected)
		}
	}
}

func TestComputeExcessBlobGas(t *testing.T) {
	tests := []struct {
		parentExcess uint64
		parentUsed   uint64
		expected     uint64
	}{
		{0, 0, 0},
		{0, types.TargetBlobGasPerBlock, 0},
		{0, types.TargetBlobGasPerBlock + types.BlobTxBlobGasPerBlob, types.BlobTxBlobGasPerBlob},
		{types.BlobTxBlobGasPerBlob, types.TargetBlobGasPerBlock, types.BlobTxBlobGasPerBlob},
		{types.MaxBlobGasPerBlock, 0, types.MaxBlobGasPerBlock - types.TargetBlobGasPerBlock},
	}
	for _, tt := range tests {
		got := ComputeExcessBlobGas(tt.parentExcess, tt.parentUsed)
		if got != tt.expected {
			t.Errorf("ComputeExcessBlobGas(%d, %d) = %d, want %d",
				tt.parentExcess, tt.parentUsed, got, tt.expected)
		}
	}
}

func TestComputeBlobBaseFee(t *testing.T) {
	// At zero excess, blob base fee should be the minimum (1).
	fee := ComputeBlobBaseFee(0)
	if fee.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("ComputeBlobBaseFee(0) = %s, want 1", fee.String())
	}

	// With some excess, fee should be > 1.
	fee = ComputeBlobBaseFee(types.BlobBaseFeeUpdateFraction)
	if fee.Cmp(big.NewInt(1)) <= 0 {
		t.Errorf("ComputeBlobBaseFee(%d) = %s, expected > 1",
			types.BlobBaseFeeUpdateFraction, fee.String())
	}

	// Fee should increase monotonically with excess.
	fee1 := ComputeBlobBaseFee(10000000)
	fee2 := ComputeBlobBaseFee(20000000)
	if fee2.Cmp(fee1) <= 0 {
		t.Errorf("expected fee2 > fee1, got fee1=%s, fee2=%s", fee1.String(), fee2.String())
	}
}

func TestValidateBlobTransactionSidecar_Valid(t *testing.T) {
	// Create a sidecar with a known commitment.
	var commitment [48]byte
	commitment[0] = 0xAA
	commitment[1] = 0xBB
	commitment[2] = 0xCC

	// Compute the expected versioned hash.
	commitHash := crypto.Keccak256(commitment[:])
	var expectedHash types.Hash
	expectedHash[0] = types.VersionedHashVersionKZG
	copy(expectedHash[1:], commitHash[1:])

	sidecar := &FixedBlobSidecar{
		BlobIndex:     0,
		KZGCommitment: commitment,
	}

	addr := types.Address{0x01}
	blobTx := &types.BlobTx{
		ChainID:    big.NewInt(1),
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(1),
		Gas:        21000,
		To:         addr,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(1),
		BlobHashes: []types.Hash{expectedHash},
	}
	tx := types.NewTransaction(blobTx)

	err := ValidateBlobTransactionSidecar(tx, sidecar)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateBlobTransactionSidecar_NilSidecar(t *testing.T) {
	addr := types.Address{0x01}
	blobTx := &types.BlobTx{
		ChainID:    big.NewInt(1),
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(1),
		Gas:        21000,
		To:         addr,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(1),
		BlobHashes: []types.Hash{{0x01}},
	}
	tx := types.NewTransaction(blobTx)

	err := ValidateBlobTransactionSidecar(tx, nil)
	if err != ErrBlobTxSidecarNil {
		t.Errorf("expected ErrBlobTxSidecarNil, got %v", err)
	}
}

func TestValidateBlobTransactionSidecar_NonBlobTx(t *testing.T) {
	legacyTx := &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		Value:    big.NewInt(0),
	}
	tx := types.NewTransaction(legacyTx)

	sidecar := &FixedBlobSidecar{BlobIndex: 0, KZGCommitment: [48]byte{0x01}}
	err := ValidateBlobTransactionSidecar(tx, sidecar)
	if err == nil {
		t.Error("expected error for non-blob transaction")
	}
}

func TestCountBlobsInTransactions(t *testing.T) {
	addr := types.Address{0x01}
	blobTx1 := types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(1),
		Gas:        21000,
		To:         addr,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(1),
		BlobHashes: []types.Hash{{0x01}, {0x01}}, // 2 blobs
	})
	blobTx2 := types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(1),
		Gas:        21000,
		To:         addr,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(1),
		BlobHashes: []types.Hash{{0x01}}, // 1 blob
	})
	legacyTx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		Value:    big.NewInt(0),
	})

	txs := []*types.Transaction{blobTx1, legacyTx, blobTx2}
	count := CountBlobsInTransactions(txs)
	if count != 3 {
		t.Errorf("expected 3 blobs, got %d", count)
	}
}

func TestVerifySidecarCount(t *testing.T) {
	v := NewBlobValidator()
	addr := types.Address{0x01}

	blobTx := types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(1),
		Gas:        21000,
		To:         addr,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(1),
		BlobHashes: []types.Hash{{0x01}, {0x01}},
	})

	txs := []*types.Transaction{blobTx}

	// Correct count.
	sidecars := []FixedBlobSidecar{makeFixedSidecar(0), makeFixedSidecar(1)}
	err := v.VerifySidecarCount(sidecars, txs)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Wrong count.
	err = v.VerifySidecarCount([]FixedBlobSidecar{makeFixedSidecar(0)}, txs)
	if err == nil {
		t.Error("expected error for count mismatch")
	}
}
