package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestCalldataTokenGas(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint64
	}{
		{
			name: "empty data",
			data: nil,
			want: 0,
		},
		{
			name: "all zero bytes",
			data: make([]byte, 10),
			// tokens = 10 * 1 = 10, gas = 10 * 4 = 40
			want: 40,
		},
		{
			name: "all nonzero bytes",
			data: []byte{0xff, 0xfe, 0xfd, 0xfc},
			// tokens = 4 * 4 = 16, gas = 16 * 4 = 64
			want: 64,
		},
		{
			name: "mixed bytes",
			data: []byte{0x00, 0xff, 0x00, 0xfe},
			// tokens = 2*1 + 2*4 = 10, gas = 10 * 4 = 40
			want: 40,
		},
		{
			name: "single zero byte",
			data: []byte{0x00},
			// tokens = 1, gas = 4
			want: 4,
		},
		{
			name: "single nonzero byte",
			data: []byte{0x01},
			// tokens = 4, gas = 16
			want: 16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := types.CalldataTokenGas(tt.data)
			if got != tt.want {
				t.Errorf("CalldataTokenGas(%x) = %d, want %d", tt.data, got, tt.want)
			}
		})
	}
}

func TestCalldataGasLimit(t *testing.T) {
	tests := []struct {
		executionGasLimit uint64
		want              uint64
	}{
		{30_000_000, 7_500_000},
		{60_000_000, 15_000_000},
		{0, 0},
		{100, 25},
		{3, 0}, // integer division rounds down
	}

	for _, tt := range tests {
		got := CalcCalldataGasLimit(tt.executionGasLimit)
		if got != tt.want {
			t.Errorf("CalcCalldataGasLimit(%d) = %d, want %d",
				tt.executionGasLimit, got, tt.want)
		}
	}
}

func TestCalldataGasTarget(t *testing.T) {
	tests := []struct {
		calldataGasLimit uint64
		want             uint64
	}{
		{7_500_000, 1_875_000},
		{0, 0},
		{100, 25},
	}

	for _, tt := range tests {
		got := CalcCalldataGasTarget(tt.calldataGasLimit)
		if got != tt.want {
			t.Errorf("CalcCalldataGasTarget(%d) = %d, want %d",
				tt.calldataGasLimit, got, tt.want)
		}
	}
}

func TestCalcCalldataExcessGas(t *testing.T) {
	gasLimit := uint64(30_000_000)
	// calldata gas limit = 7_500_000
	// target = 7_500_000 / 4 = 1_875_000

	tests := []struct {
		name         string
		parentExcess uint64
		parentUsed   uint64
		parentLimit  uint64
		want         uint64
	}{
		{
			name:         "both zero",
			parentExcess: 0,
			parentUsed:   0,
			parentLimit:  gasLimit,
			want:         0,
		},
		{
			name:         "below target returns zero",
			parentExcess: 0,
			parentUsed:   1_000_000,
			parentLimit:  gasLimit,
			want:         0,
		},
		{
			name:         "exactly at target returns zero",
			parentExcess: 0,
			parentUsed:   1_875_000,
			parentLimit:  gasLimit,
			want:         0,
		},
		{
			name:         "above target accumulates excess",
			parentExcess: 0,
			parentUsed:   2_000_000,
			parentLimit:  gasLimit,
			want:         125_000, // 2M - 1.875M
		},
		{
			name:         "excess accumulates over time",
			parentExcess: 500_000,
			parentUsed:   2_500_000,
			parentLimit:  gasLimit,
			want:         1_125_000, // 500K + 2.5M - 1.875M
		},
		{
			name:         "excess decreases when below target",
			parentExcess: 1_000_000,
			parentUsed:   0,
			parentLimit:  gasLimit,
			want:         0, // 1M + 0 < 1.875M => 0
		},
		{
			name:         "excess decreases partially",
			parentExcess: 3_000_000,
			parentUsed:   0,
			parentLimit:  gasLimit,
			want:         1_125_000, // 3M + 0 - 1.875M
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcCalldataExcessGas(tt.parentExcess, tt.parentUsed, tt.parentLimit)
			if got != tt.want {
				t.Errorf("CalcCalldataExcessGas(%d, %d, %d) = %d, want %d",
					tt.parentExcess, tt.parentUsed, tt.parentLimit, got, tt.want)
			}
		})
	}
}

func TestCalcCalldataBaseFee(t *testing.T) {
	calldataGasLimit := uint64(7_500_000)

	tests := []struct {
		name     string
		excess   uint64
		wantMin  int64
	}{
		{
			name:    "zero excess returns minimum",
			excess:  0,
			wantMin: 1,
		},
		{
			name:    "small excess still near minimum",
			excess:  100_000,
			wantMin: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcCalldataBaseFee(tt.excess, calldataGasLimit)
			if got.Cmp(big.NewInt(tt.wantMin)) < 0 {
				t.Errorf("CalcCalldataBaseFee(%d) = %s, want >= %d",
					tt.excess, got, tt.wantMin)
			}
		})
	}
}

func TestCalcCalldataBaseFeeMonotonicallyIncreasing(t *testing.T) {
	calldataGasLimit := uint64(7_500_000)
	target := CalcCalldataGasTarget(calldataGasLimit)

	prev := CalcCalldataBaseFee(0, calldataGasLimit)
	for excess := target; excess <= target*20; excess += target {
		cur := CalcCalldataBaseFee(excess, calldataGasLimit)
		if cur.Cmp(prev) < 0 {
			t.Fatalf("calldata base fee decreased at excess=%d: %s < %s",
				excess, cur, prev)
		}
		prev = cur
	}
}

func TestCalcCalldataBaseFeeFromHeader(t *testing.T) {
	// Header without EIP-7706 fields returns minimum.
	h := &types.Header{GasLimit: 30_000_000}
	fee := CalcCalldataBaseFeeFromHeader(h)
	if fee.Cmp(big.NewInt(MinCalldataBaseFee)) != 0 {
		t.Fatalf("expected minimum fee %d, got %s", MinCalldataBaseFee, fee)
	}

	// Header with zero excess returns minimum.
	excess := uint64(0)
	h.CalldataExcessGas = &excess
	fee = CalcCalldataBaseFeeFromHeader(h)
	if fee.Cmp(big.NewInt(MinCalldataBaseFee)) != 0 {
		t.Fatalf("expected minimum fee %d for zero excess, got %s",
			MinCalldataBaseFee, fee)
	}

	// Header with large excess should return > minimum.
	bigExcess := uint64(100_000_000)
	h.CalldataExcessGas = &bigExcess
	fee = CalcCalldataBaseFeeFromHeader(h)
	if fee.Cmp(big.NewInt(MinCalldataBaseFee)) <= 0 {
		t.Fatalf("expected fee > %d for large excess, got %s",
			MinCalldataBaseFee, fee)
	}
}

func TestCalldataGasCost(t *testing.T) {
	tests := []struct {
		gas     uint64
		baseFee int64
		want    int64
	}{
		{0, 1, 0},
		{100, 1, 100},
		{100, 10, 1000},
		{1_000_000, 5, 5_000_000},
	}

	for _, tt := range tests {
		got := CalldataGasCost(tt.gas, big.NewInt(tt.baseFee))
		if got.Int64() != tt.want {
			t.Errorf("CalldataGasCost(%d, %d) = %s, want %d",
				tt.gas, tt.baseFee, got, tt.want)
		}
	}
}

func TestTransactionCalldataGas(t *testing.T) {
	to := types.HexToAddress("0xdead")

	tests := []struct {
		name string
		data []byte
		want uint64
	}{
		{
			name: "no calldata",
			data: nil,
			want: 0,
		},
		{
			name: "standard transfer calldata",
			data: []byte{0xa9, 0x05, 0x9c, 0xbb}, // transfer(address,uint256) selector
			// 4 nonzero bytes: tokens = 4*4 = 16, gas = 16*4 = 64
			want: 64,
		},
		{
			name: "mixed calldata",
			data: []byte{0xa9, 0x00, 0x00, 0xbb},
			// 2 nonzero + 2 zero: tokens = 2*4 + 2*1 = 10, gas = 10*4 = 40
			want: 40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := types.NewTransaction(&types.DynamicFeeTx{
				ChainID:   big.NewInt(1),
				Nonce:     0,
				GasTipCap: big.NewInt(1),
				GasFeeCap: big.NewInt(100),
				Gas:       21000,
				To:        &to,
				Value:     big.NewInt(0),
				Data:      tt.data,
			})
			got := tx.CalldataGas()
			if got != tt.want {
				t.Errorf("CalldataGas() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCalldataGasConstants(t *testing.T) {
	if types.CalldataGasPerToken != 4 {
		t.Errorf("CalldataGasPerToken = %d, want 4", types.CalldataGasPerToken)
	}
	if types.CalldataTokensPerNonZeroByte != 4 {
		t.Errorf("CalldataTokensPerNonZeroByte = %d, want 4", types.CalldataTokensPerNonZeroByte)
	}
	if types.CalldataGasLimitRatio != 4 {
		t.Errorf("CalldataGasLimitRatio = %d, want 4", types.CalldataGasLimitRatio)
	}
	if CalldataTargetRatio != 4 {
		t.Errorf("CalldataTargetRatio = %d, want 4", CalldataTargetRatio)
	}
	if CalldataBaseFeeUpdateFraction != 8 {
		t.Errorf("CalldataBaseFeeUpdateFraction = %d, want 8", CalldataBaseFeeUpdateFraction)
	}
	if MinCalldataBaseFee != 1 {
		t.Errorf("MinCalldataBaseFee = %d, want 1", MinCalldataBaseFee)
	}
}

func TestCalldataGasLimitConsistency(t *testing.T) {
	// With a 30M gas limit, the calldata gas limit should be 7.5M.
	// The target should be 1.875M, which is about 187,500 bytes of
	// nonzero calldata (187500 * 4 tokens * 4 gas/token / 16 = 187,500).
	gasLimit := uint64(30_000_000)
	calldataGasLimit := CalcCalldataGasLimit(gasLimit)
	if calldataGasLimit != 7_500_000 {
		t.Fatalf("calldata gas limit = %d, want 7,500,000", calldataGasLimit)
	}

	target := CalcCalldataGasTarget(calldataGasLimit)
	if target != 1_875_000 {
		t.Fatalf("calldata gas target = %d, want 1,875,000", target)
	}

	// With standard calldata pricing (16 gas per nonzero byte), the target
	// corresponds to approximately:
	// 1,875,000 gas / 16 gas_per_byte = 117,187 bytes of nonzero calldata
	// This is roughly 2x the current ~100KB average block size.
	targetBytes := target / 16 // 16 = 4 tokens * 4 gas/token
	if targetBytes < 100_000 || targetBytes > 200_000 {
		t.Fatalf("target calldata bytes = %d, expected between 100K and 200K", targetBytes)
	}
}

func TestValidateCalldataGas(t *testing.T) {
	gasLimit := uint64(30_000_000)
	calldataGasLimit := CalcCalldataGasLimit(gasLimit)
	zeroExcess := uint64(0)
	zeroUsed := uint64(0)

	// Valid header should pass.
	parent := &types.Header{
		GasLimit:          gasLimit,
		CalldataGasUsed:   &zeroUsed,
		CalldataExcessGas: &zeroExcess,
	}
	childExcess := CalcCalldataExcessGas(0, 0, gasLimit)
	childUsed := uint64(1000)
	child := &types.Header{
		GasLimit:          gasLimit,
		CalldataGasUsed:   &childUsed,
		CalldataExcessGas: &childExcess,
	}
	if err := ValidateCalldataGas(child, parent); err != nil {
		t.Fatalf("ValidateCalldataGas failed for valid header: %v", err)
	}

	// Missing CalldataGasUsed should fail.
	childMissing := &types.Header{
		GasLimit:          gasLimit,
		CalldataExcessGas: &childExcess,
	}
	if err := ValidateCalldataGas(childMissing, parent); err == nil {
		t.Fatal("expected error for missing CalldataGasUsed")
	}

	// Missing CalldataExcessGas should fail.
	childMissing2 := &types.Header{
		GasLimit:        gasLimit,
		CalldataGasUsed: &childUsed,
	}
	if err := ValidateCalldataGas(childMissing2, parent); err == nil {
		t.Fatal("expected error for missing CalldataExcessGas")
	}

	// CalldataGasUsed exceeding limit should fail.
	tooMuch := calldataGasLimit + 1
	childOverLimit := &types.Header{
		GasLimit:          gasLimit,
		CalldataGasUsed:   &tooMuch,
		CalldataExcessGas: &childExcess,
	}
	if err := ValidateCalldataGas(childOverLimit, parent); err == nil {
		t.Fatal("expected error for calldata gas exceeding limit")
	}

	// Wrong excess should fail.
	wrongExcess := uint64(999999)
	childWrongExcess := &types.Header{
		GasLimit:          gasLimit,
		CalldataGasUsed:   &childUsed,
		CalldataExcessGas: &wrongExcess,
	}
	if err := ValidateCalldataGas(childWrongExcess, parent); err == nil {
		t.Fatal("expected error for wrong excess gas")
	}
}

func TestHeaderRLPWithCalldataGas(t *testing.T) {
	excess := uint64(12345)
	used := uint64(67890)
	blobGasUsed := uint64(131072)
	excessBlobGas := uint64(0)
	withdrawalsHash := types.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	beaconRoot := types.HexToHash("0xcccc")
	requestsHash := types.HexToHash("0xdddd")

	h := &types.Header{
		ParentHash:        types.HexToHash("0xaaaa"),
		Number:            big.NewInt(100),
		GasLimit:          30_000_000,
		GasUsed:           15_000_000,
		Time:              1234567890,
		BaseFee:           big.NewInt(1_000_000_000),
		WithdrawalsHash:   &withdrawalsHash,
		BlobGasUsed:       &blobGasUsed,
		ExcessBlobGas:     &excessBlobGas,
		ParentBeaconRoot:  &beaconRoot,
		RequestsHash:      &requestsHash,
		CalldataGasUsed:   &used,
		CalldataExcessGas: &excess,
	}

	enc, err := h.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}

	decoded, err := types.DecodeHeaderRLP(enc)
	if err != nil {
		t.Fatalf("DecodeHeaderRLP: %v", err)
	}

	if decoded.CalldataGasUsed == nil {
		t.Fatal("CalldataGasUsed should not be nil after decode")
	}
	if *decoded.CalldataGasUsed != used {
		t.Fatalf("CalldataGasUsed: got %d, want %d", *decoded.CalldataGasUsed, used)
	}
	if decoded.CalldataExcessGas == nil {
		t.Fatal("CalldataExcessGas should not be nil after decode")
	}
	if *decoded.CalldataExcessGas != excess {
		t.Fatalf("CalldataExcessGas: got %d, want %d", *decoded.CalldataExcessGas, excess)
	}
}

func TestHeaderRLPWithoutCalldataGas(t *testing.T) {
	// Pre-EIP-7706 header (no calldata gas fields) should still decode fine.
	h := &types.Header{
		ParentHash: types.HexToHash("0xbbbb"),
		Number:     big.NewInt(50),
		GasLimit:   30_000_000,
		GasUsed:    10_000_000,
		Time:       1234567890,
		BaseFee:    big.NewInt(1_000_000_000),
	}

	enc, err := h.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}

	decoded, err := types.DecodeHeaderRLP(enc)
	if err != nil {
		t.Fatalf("DecodeHeaderRLP: %v", err)
	}

	if decoded.CalldataGasUsed != nil {
		t.Fatal("CalldataGasUsed should be nil for pre-EIP-7706 header")
	}
	if decoded.CalldataExcessGas != nil {
		t.Fatal("CalldataExcessGas should be nil for pre-EIP-7706 header")
	}
}

func TestHeaderHashIncludesCalldataGas(t *testing.T) {
	excess := uint64(100)
	used := uint64(200)

	h1 := &types.Header{
		Number:            big.NewInt(1),
		GasLimit:          30_000_000,
		BaseFee:           big.NewInt(1_000_000_000),
		CalldataGasUsed:   &used,
		CalldataExcessGas: &excess,
	}

	// Different calldata gas values should produce different hash.
	differentExcess := uint64(999)
	h2 := &types.Header{
		Number:            big.NewInt(1),
		GasLimit:          30_000_000,
		BaseFee:           big.NewInt(1_000_000_000),
		CalldataGasUsed:   &used,
		CalldataExcessGas: &differentExcess,
	}

	if h1.Hash() == h2.Hash() {
		t.Fatal("headers with different CalldataExcessGas should have different hashes")
	}

	// Same values should produce same hash.
	excess3 := uint64(100)
	used3 := uint64(200)
	h3 := &types.Header{
		Number:            big.NewInt(1),
		GasLimit:          30_000_000,
		BaseFee:           big.NewInt(1_000_000_000),
		CalldataGasUsed:   &used3,
		CalldataExcessGas: &excess3,
	}
	if h1.Hash() != h3.Hash() {
		t.Fatal("headers with same values should have same hash")
	}
}
