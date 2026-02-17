package types

import (
	"math/big"
	"testing"
)

func TestCalcExcessBlobGas(t *testing.T) {
	tests := []struct {
		name              string
		parentExcess      uint64
		parentBlobGasUsed uint64
		want              uint64
	}{
		{
			name:              "genesis (both zero)",
			parentExcess:      0,
			parentBlobGasUsed: 0,
			want:              0,
		},
		{
			name:              "below target returns zero",
			parentExcess:      0,
			parentBlobGasUsed: BlobTxBlobGasPerBlob, // 1 blob = 131072
			want:              0,
		},
		{
			name:              "exactly at target returns zero",
			parentExcess:      0,
			parentBlobGasUsed: TargetBlobGasPerBlock, // 393216
			want:              0,
		},
		{
			name:              "one blob above target",
			parentExcess:      0,
			parentBlobGasUsed: TargetBlobGasPerBlock + BlobTxBlobGasPerBlob,
			want:              BlobTxBlobGasPerBlob,
		},
		{
			name:              "max blobs (6 blobs)",
			parentExcess:      0,
			parentBlobGasUsed: MaxBlobGasPerBlock, // 786432 = 6 * 131072
			want:              MaxBlobGasPerBlock - TargetBlobGasPerBlock,
		},
		{
			name:              "accumulated excess",
			parentExcess:      TargetBlobGasPerBlock,
			parentBlobGasUsed: MaxBlobGasPerBlock,
			want:              TargetBlobGasPerBlock + MaxBlobGasPerBlock - TargetBlobGasPerBlock,
		},
		{
			name:              "excess decreases when below target",
			parentExcess:      BlobTxBlobGasPerBlob * 2,
			parentBlobGasUsed: 0,
			want:              0, // 2*131072 + 0 < 393216
		},
		{
			name:              "excess decreases partially",
			parentExcess:      TargetBlobGasPerBlock + BlobTxBlobGasPerBlob*2,
			parentBlobGasUsed: 0,
			want:              BlobTxBlobGasPerBlob * 2, // excess drains by TargetBlobGasPerBlock
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcExcessBlobGas(tt.parentExcess, tt.parentBlobGasUsed)
			if got != tt.want {
				t.Errorf("CalcExcessBlobGas(%d, %d) = %d, want %d",
					tt.parentExcess, tt.parentBlobGasUsed, got, tt.want)
			}
		})
	}
}

func TestCalcBlobFee(t *testing.T) {
	tests := []struct {
		name          string
		excessBlobGas uint64
		want          *big.Int
	}{
		{
			name:          "zero excess returns minimum price",
			excessBlobGas: 0,
			want:          big.NewInt(1),
		},
		{
			name:          "small excess still near minimum",
			excessBlobGas: BlobTxBlobGasPerBlob,
			want:          big.NewInt(1), // still rounds to 1 with small excess
		},
		{
			name:          "large excess increases price",
			excessBlobGas: BlobBaseFeeUpdateFraction, // one full fraction = e^1 ~ 2.71
			want:          big.NewInt(2),              // integer Taylor expansion truncation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcBlobFee(tt.excessBlobGas)
			if got.Cmp(tt.want) != 0 {
				t.Errorf("CalcBlobFee(%d) = %s, want %s",
					tt.excessBlobGas, got, tt.want)
			}
		})
	}
}

func TestCalcBlobFeeMonotonicallyIncreasing(t *testing.T) {
	prev := CalcBlobFee(0)
	for excess := uint64(BlobTxBlobGasPerBlob); excess <= BlobBaseFeeUpdateFraction*5; excess += BlobTxBlobGasPerBlob {
		cur := CalcBlobFee(excess)
		if cur.Cmp(prev) < 0 {
			t.Fatalf("blob fee decreased at excess=%d: %s < %s", excess, cur, prev)
		}
		prev = cur
	}
}

func TestGetBlobGasUsed(t *testing.T) {
	tests := []struct {
		numBlobs int
		want     uint64
	}{
		{0, 0},
		{1, BlobTxBlobGasPerBlob},
		{3, 3 * BlobTxBlobGasPerBlob},
		{6, MaxBlobGasPerBlock}, // 6 blobs = max per block
	}

	for _, tt := range tests {
		got := GetBlobGasUsed(tt.numBlobs)
		if got != tt.want {
			t.Errorf("GetBlobGasUsed(%d) = %d, want %d", tt.numBlobs, got, tt.want)
		}
	}
}

func TestBlobGasConstants(t *testing.T) {
	if BlobTxBlobGasPerBlob != 131072 {
		t.Errorf("BlobTxBlobGasPerBlob = %d, want 131072", BlobTxBlobGasPerBlob)
	}
	if MaxBlobGasPerBlock != 786432 {
		t.Errorf("MaxBlobGasPerBlock = %d, want 786432", MaxBlobGasPerBlock)
	}
	if TargetBlobGasPerBlock != 393216 {
		t.Errorf("TargetBlobGasPerBlock = %d, want 393216", TargetBlobGasPerBlock)
	}
	// 6 blobs at target = 3 blobs
	if TargetBlobGasPerBlock/BlobTxBlobGasPerBlob != 3 {
		t.Error("target should be 3 blobs")
	}
	if MaxBlobGasPerBlock/BlobTxBlobGasPerBlob != 6 {
		t.Error("max should be 6 blobs")
	}
}

func TestFakeExponential(t *testing.T) {
	tests := []struct {
		factor      int64
		numerator   int64
		denominator int64
		want        int64
	}{
		{1, 0, 1, 1},              // e^0 = 1
		{1, 1, 1, 2},              // floor(e^1) = floor(2.718...) = 2 -- but Taylor series integer math gives 2
		{38, 0, 1000, 38},         // 38 * e^0 = 38
		{100, 0, BlobBaseFeeUpdateFraction, 100}, // factor * e^0 = factor
	}

	for _, tt := range tests {
		got := fakeExponential(
			big.NewInt(tt.factor),
			big.NewInt(tt.numerator),
			big.NewInt(tt.denominator),
		)
		if got.Int64() != tt.want {
			t.Errorf("fakeExponential(%d, %d, %d) = %d, want %d",
				tt.factor, tt.numerator, tt.denominator, got.Int64(), tt.want)
		}
	}
}

func TestBlobTxFields(t *testing.T) {
	inner := &BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      42,
		GasTipCap:  big.NewInt(1_000_000_000),
		GasFeeCap:  big.NewInt(50_000_000_000),
		Gas:        21000,
		To:         HexToAddress("0xdead"),
		Value:      big.NewInt(1_000_000),
		BlobFeeCap: big.NewInt(5_000_000),
		BlobHashes: []Hash{
			HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001"),
			HexToHash("0x0100000000000000000000000000000000000000000000000000000000000002"),
			HexToHash("0x0100000000000000000000000000000000000000000000000000000000000003"),
		},
		Data: []byte{0xca, 0xfe},
		AccessList: AccessList{
			{Address: HexToAddress("0xaaaa"), StorageKeys: []Hash{HexToHash("0x01")}},
		},
	}
	tx := NewTransaction(inner)

	if tx.Type() != BlobTxType {
		t.Fatalf("expected type %d, got %d", BlobTxType, tx.Type())
	}
	if tx.Nonce() != 42 {
		t.Fatalf("nonce = %d, want 42", tx.Nonce())
	}
	if tx.GasTipCap().Cmp(big.NewInt(1_000_000_000)) != 0 {
		t.Fatal("GasTipCap mismatch")
	}
	if tx.GasFeeCap().Cmp(big.NewInt(50_000_000_000)) != 0 {
		t.Fatal("GasFeeCap mismatch")
	}
	if tx.Gas() != 21000 {
		t.Fatal("Gas mismatch")
	}
	if tx.To() == nil {
		t.Fatal("BlobTx To should never be nil")
	}
	if tx.Value().Cmp(big.NewInt(1_000_000)) != 0 {
		t.Fatal("Value mismatch")
	}
	if tx.ChainId().Int64() != 1 {
		t.Fatal("ChainID mismatch")
	}
	if len(tx.Data()) != 2 {
		t.Fatal("Data mismatch")
	}
	if len(tx.AccessList()) != 1 {
		t.Fatal("AccessList mismatch")
	}
}

func TestBlobTxCopyIndependence(t *testing.T) {
	inner := &BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      1,
		GasTipCap:  big.NewInt(100),
		GasFeeCap:  big.NewInt(200),
		Gas:        21000,
		To:         HexToAddress("0xdead"),
		Value:      big.NewInt(500),
		BlobFeeCap: big.NewInt(1000),
		BlobHashes: []Hash{HexToHash("0x01")},
	}
	tx := NewTransaction(inner)

	// Mutate original; tx should be unaffected.
	inner.Nonce = 99
	inner.GasTipCap.SetInt64(999)
	inner.BlobFeeCap.SetInt64(9999)
	inner.BlobHashes[0] = HexToHash("0xff")

	if tx.Nonce() != 1 {
		t.Fatal("Nonce should be independent")
	}
	if tx.GasTipCap().Int64() != 100 {
		t.Fatal("GasTipCap should be independent")
	}
}
