package engine

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestValidateTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		parent  uint64
		payload uint64
		wantErr bool
	}{
		{"valid increasing", 100, 112, false},
		{"valid increment by 1", 100, 101, false},
		{"equal timestamps", 100, 100, true},
		{"payload before parent", 200, 100, true},
		{"zero payload", 100, 0, true},
		{"both zero", 0, 0, true},
		{"zero parent valid payload", 0, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTimestamp(tt.parent, tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTimestamp(%d, %d) error = %v, wantErr %v",
					tt.parent, tt.payload, err, tt.wantErr)
			}
		})
	}
}

func TestValidateBaseFee(t *testing.T) {
	tests := []struct {
		name      string
		parent    *big.Int
		current   *big.Int
		gasUsed   uint64
		gasTarget uint64
		wantErr   bool
	}{
		{
			name:      "same gas used as target, fee unchanged",
			parent:    big.NewInt(1000000000),
			current:   big.NewInt(1000000000),
			gasUsed:   15000000,
			gasTarget: 15000000,
			wantErr:   false,
		},
		{
			name:      "nil parent fee",
			parent:    nil,
			current:   big.NewInt(1000),
			gasUsed:   15000000,
			gasTarget: 15000000,
			wantErr:   true,
		},
		{
			name:      "nil current fee",
			parent:    big.NewInt(1000),
			current:   nil,
			gasUsed:   15000000,
			gasTarget: 15000000,
			wantErr:   true,
		},
		{
			name:      "zero current fee",
			parent:    big.NewInt(1000),
			current:   big.NewInt(0),
			gasUsed:   15000000,
			gasTarget: 15000000,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBaseFee(tt.parent, tt.current, tt.gasUsed, tt.gasTarget)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBaseFee() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCalcBaseFeeBig(t *testing.T) {
	tests := []struct {
		name      string
		parentFee *big.Int
		gasUsed   uint64
		gasTarget uint64
		wantFee   *big.Int
	}{
		{
			name:      "equal to target, no change",
			parentFee: big.NewInt(1000000000),
			gasUsed:   15000000,
			gasTarget: 15000000,
			wantFee:   big.NewInt(1000000000),
		},
		{
			name:      "above target, increases",
			parentFee: big.NewInt(1000000000),
			gasUsed:   30000000,
			gasTarget: 15000000,
			wantFee:   big.NewInt(1125000000), // 1000000000 + 1000000000 * 15000000 / 15000000 / 8
		},
		{
			name:      "below target, decreases",
			parentFee: big.NewInt(1000000000),
			gasUsed:   0,
			gasTarget: 15000000,
			wantFee:   big.NewInt(875000000), // 1000000000 - 1000000000 * 15000000 / 15000000 / 8
		},
		{
			name:      "zero gas target, no change",
			parentFee: big.NewInt(1000000000),
			gasUsed:   10000000,
			gasTarget: 0,
			wantFee:   big.NewInt(1000000000),
		},
		{
			name:      "small base fee, increase by at least 1",
			parentFee: big.NewInt(1),
			gasUsed:   30000000,
			gasTarget: 15000000,
			wantFee:   big.NewInt(2), // 1 + max(1*15M/15M/8, 1) = 1 + 1 = 2
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcBaseFeeBig(tt.parentFee, tt.gasUsed, tt.gasTarget)
			if got.Cmp(tt.wantFee) != 0 {
				t.Errorf("CalcBaseFeeBig() = %s, want %s", got.String(), tt.wantFee.String())
			}
		})
	}
}

func TestValidateGasLimit(t *testing.T) {
	tests := []struct {
		name    string
		parent  uint64
		payload uint64
		wantErr bool
	}{
		{"same gas limit", 30000000, 30000000, false},
		{"increase within bound", 30000000, 30029000, false},
		{"decrease within bound", 30000000, 29971000, false},
		{"increase too large", 30000000, 30100000, true},
		{"decrease too large", 30000000, 29800000, true},
		{"below minimum", 30000000, 4000, true},
		{"at minimum", 5000, 5000, false},
		{"exact upper bound", 30000000, 30000000 + 30000000/1024, false},
		{"one above upper bound", 30000000, 30000000 + 30000000/1024 + 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGasLimit(tt.parent, tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGasLimit(%d, %d) error = %v, wantErr %v",
					tt.parent, tt.payload, err, tt.wantErr)
			}
		})
	}
}

func TestValidateExtraData(t *testing.T) {
	tests := []struct {
		name    string
		extra   []byte
		wantErr bool
	}{
		{"empty", nil, false},
		{"short", []byte("hello"), false},
		{"exactly 32", make([]byte, 32), false},
		{"too long 33", make([]byte, 33), true},
		{"too long 100", make([]byte, 100), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExtraData(tt.extra)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateExtraData() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTransactions(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		txs, err := ValidateTransactions(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(txs) != 0 {
			t.Errorf("expected 0 transactions, got %d", len(txs))
		}
	})

	t.Run("empty transaction bytes", func(t *testing.T) {
		_, err := ValidateTransactions([][]byte{{}})
		if err == nil {
			t.Fatal("expected error for empty transaction bytes")
		}
	})

	t.Run("invalid rlp", func(t *testing.T) {
		_, err := ValidateTransactions([][]byte{{0xff, 0xfe, 0xfd}})
		if err == nil {
			t.Fatal("expected error for invalid RLP")
		}
	})
}

func TestValidateBlobGasUsed(t *testing.T) {
	tests := []struct {
		name     string
		blobGas  uint64
		numBlobs int
		wantErr  bool
	}{
		{"zero blobs, zero gas", 0, 0, false},
		{"one blob, correct gas", types.BlobTxBlobGasPerBlob, 1, false},
		{"six blobs, correct gas", 6 * types.BlobTxBlobGasPerBlob, 6, false},
		{"mismatch gas", types.BlobTxBlobGasPerBlob, 0, true},
		{"not aligned", 100, 0, true},
		{"exceeds max", types.MaxBlobGasPerBlock + types.BlobTxBlobGasPerBlob, 7, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var txs []*types.Transaction
			if tt.numBlobs > 0 {
				blobHashes := make([]types.Hash, tt.numBlobs)
				for i := range blobHashes {
					blobHashes[i][0] = 0x01
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
					BlobHashes: blobHashes,
				}
				txs = append(txs, types.NewTransaction(blobTx))
			}

			err := ValidateBlobGasUsed(tt.blobGas, txs)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBlobGasUsed(%d, %d blobs) error = %v, wantErr %v",
					tt.blobGas, tt.numBlobs, err, tt.wantErr)
			}
		})
	}
}

func TestValidateWithdrawals(t *testing.T) {
	validAddr := types.Address{0x01, 0x02, 0x03}

	tests := []struct {
		name        string
		withdrawals []*Withdrawal
		wantErr     bool
	}{
		{"nil withdrawals", nil, true},
		{"empty withdrawals", []*Withdrawal{}, false},
		{
			"valid withdrawals",
			[]*Withdrawal{
				{Index: 0, ValidatorIndex: 1, Address: validAddr, Amount: 1000},
				{Index: 1, ValidatorIndex: 2, Address: validAddr, Amount: 2000},
			},
			false,
		},
		{"nil entry", []*Withdrawal{nil}, true},
		{
			"zero address",
			[]*Withdrawal{{Index: 0, ValidatorIndex: 1, Address: types.Address{}, Amount: 1000}},
			true,
		},
		{
			"duplicate index",
			[]*Withdrawal{
				{Index: 0, ValidatorIndex: 1, Address: validAddr, Amount: 1000},
				{Index: 0, ValidatorIndex: 2, Address: validAddr, Amount: 2000},
			},
			true,
		},
		{
			"too many withdrawals",
			makeManyWithdrawals(MaxWithdrawalsPerPayloadV2+1, validAddr),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWithdrawals(tt.withdrawals)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWithdrawals() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func makeManyWithdrawals(n int, addr types.Address) []*Withdrawal {
	ws := make([]*Withdrawal, n)
	for i := 0; i < n; i++ {
		ws[i] = &Withdrawal{
			Index:          uint64(i),
			ValidatorIndex: uint64(i),
			Address:        addr,
			Amount:         1000,
		}
	}
	return ws
}

func TestValidateParentBeaconBlockRoot(t *testing.T) {
	nonZeroHash := types.Hash{0x01}

	tests := []struct {
		name    string
		root    *types.Hash
		wantErr bool
	}{
		{"nil root", nil, true},
		{"zero root", &types.Hash{}, true},
		{"valid root", &nonZeroHash, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateParentBeaconBlockRoot(tt.root)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateParentBeaconBlockRoot() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePayloadFull_NilPayload(t *testing.T) {
	v := NewPayloadValidator()
	errs := v.ValidatePayloadFull(nil)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for nil payload, got %d", len(errs))
	}
}

func TestValidatePayloadFull_Valid(t *testing.T) {
	v := NewPayloadValidator()
	payload := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				ParentHash:    types.Hash{0x01},
				FeeRecipient:  types.Address{0x02},
				StateRoot:     types.Hash{0x03},
				ReceiptsRoot:  types.Hash{0x04},
				BlockNumber:   1,
				GasLimit:      30000000,
				GasUsed:       21000,
				Timestamp:     100,
				BaseFeePerGas: big.NewInt(1000000000),
				Transactions:  [][]byte{},
			},
			Withdrawals: []*Withdrawal{},
		},
		BlobGasUsed:   0,
		ExcessBlobGas: 0,
	}
	errs := v.ValidatePayloadFull(payload)
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid payload, got %d: %v", len(errs), errs)
	}
}

func TestValidatePayloadFull_MultipleErrors(t *testing.T) {
	v := NewPayloadValidator()
	payload := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				GasLimit:      30000000,
				GasUsed:       40000000, // exceeds limit
				Timestamp:     0,        // zero timestamp
				ExtraData:     make([]byte, 50), // too long
				BaseFeePerGas: nil,      // nil base fee
				Transactions:  [][]byte{},
			},
			Withdrawals: nil, // nil withdrawals
		},
		BlobGasUsed: 100, // not aligned
	}
	errs := v.ValidatePayloadFull(payload)
	if len(errs) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(errs), errs)
	}
}

func TestNewPayloadValidator(t *testing.T) {
	v := NewPayloadValidator()
	if v.maxBlobsPerBlock != 6 {
		t.Errorf("expected maxBlobsPerBlock=6, got %d", v.maxBlobsPerBlock)
	}
	if v.blobGasPerBlob != types.BlobTxBlobGasPerBlob {
		t.Errorf("expected blobGasPerBlob=%d, got %d",
			types.BlobTxBlobGasPerBlob, v.blobGasPerBlob)
	}
}
