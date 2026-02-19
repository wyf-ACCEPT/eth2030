package core

import (
	"math/big"
	"testing"
)

func TestFusakaBlobGasConstants(t *testing.T) {
	if MinBaseFeePerBlobGas != 1<<25 {
		t.Errorf("MinBaseFeePerBlobGas = %d, want %d", MinBaseFeePerBlobGas, 1<<25)
	}
	if BlobBaseCost != 1<<13 {
		t.Errorf("BlobBaseCost = %d, want %d", BlobBaseCost, 1<<13)
	}
	if FusakaMaxBlobsPerBlock != 9 {
		t.Errorf("FusakaMaxBlobsPerBlock = %d, want 9", FusakaMaxBlobsPerBlock)
	}
	if FusakaTargetBlobsPerBlock != 6 {
		t.Errorf("FusakaTargetBlobsPerBlock = %d, want 6", FusakaTargetBlobsPerBlock)
	}
	if GasPerBlob != 131072 {
		t.Errorf("GasPerBlob = %d, want 131072", GasPerBlob)
	}
	if FusakaMaxBlobGasPerBlock != 9*131072 {
		t.Errorf("FusakaMaxBlobGasPerBlock = %d, want %d", FusakaMaxBlobGasPerBlock, 9*131072)
	}
	if FusakaTargetBlobGasPerBlock != 6*131072 {
		t.Errorf("FusakaTargetBlobGasPerBlock = %d, want %d", FusakaTargetBlobGasPerBlock, 6*131072)
	}
}

func TestCalcBlobBaseFeeV2Minimum(t *testing.T) {
	// With zero excess, fee should be MinBaseFeePerBlobGas (EIP-7762).
	fee := CalcBlobBaseFeeV2(0, big.NewInt(0))
	expected := big.NewInt(MinBaseFeePerBlobGas)
	if fee.Cmp(expected) != 0 {
		t.Errorf("CalcBlobBaseFeeV2(0, 0) = %s, want %s", fee, expected)
	}
}

func TestCalcBlobBaseFeeV2MonotonicallyIncreasing(t *testing.T) {
	prev := CalcBlobBaseFeeV2(0, big.NewInt(0))
	for excess := uint64(GasPerBlob); excess <= uint64(FusakaBlobBaseFeeUpdateFraction)*3; excess += uint64(GasPerBlob) {
		cur := CalcBlobBaseFeeV2(excess, big.NewInt(0))
		if cur.Cmp(prev) < 0 {
			t.Fatalf("blob fee decreased at excess=%d: %s < %s", excess, cur, prev)
		}
		prev = cur
	}
}

func TestCalcBlobBaseFeeV2ReservePrice(t *testing.T) {
	// When base fee is high, EIP-7918 reserve price should apply.
	// Reserve = BLOB_BASE_COST * baseFee / GAS_PER_BLOB
	// = 8192 * 30_000_000_000 / 131072 = 1,875,000,000
	baseFee := big.NewInt(30_000_000_000) // 30 gwei
	fee := CalcBlobBaseFeeV2(0, baseFee)

	reserve := new(big.Int).Mul(big.NewInt(BlobBaseCost), baseFee)
	reserve.Div(reserve, big.NewInt(GasPerBlob))

	minFloor := big.NewInt(MinBaseFeePerBlobGas)

	// Fee should be the max of computed (= MinBaseFeePerBlobGas at 0 excess)
	// and reserve price.
	if reserve.Cmp(minFloor) > 0 {
		// Reserve is higher; fee should equal reserve.
		if fee.Cmp(reserve) != 0 {
			t.Errorf("fee = %s, expected reserve = %s", fee, reserve)
		}
	} else {
		// Min floor is higher; fee should be at least min floor.
		if fee.Cmp(minFloor) < 0 {
			t.Errorf("fee = %s, expected >= %s", fee, minFloor)
		}
	}
}

func TestCalcBlobBaseFeeV2NilBaseFee(t *testing.T) {
	// Should work with nil base fee (no EIP-7918 reserve).
	fee := CalcBlobBaseFeeV2(0, nil)
	if fee.Cmp(big.NewInt(MinBaseFeePerBlobGas)) != 0 {
		t.Errorf("fee with nil baseFee = %s, want %d", fee, MinBaseFeePerBlobGas)
	}
}

func TestCalcExcessBlobGasV2BelowTarget(t *testing.T) {
	// When excess + used < target, should return 0.
	result := CalcExcessBlobGasV2(0, 0, big.NewInt(1))
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}

	result = CalcExcessBlobGasV2(0, GasPerBlob, big.NewInt(1))
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}

func TestCalcExcessBlobGasV2NormalMode(t *testing.T) {
	// When blob base fee > reserve price, normal subtraction applies.
	// Use high excess so blob base fee is well above reserve.
	excess := uint64(FusakaBlobBaseFeeUpdateFraction * 10)
	used := uint64(FusakaMaxBlobGasPerBlock)
	baseFee := big.NewInt(1) // very low base fee

	result := CalcExcessBlobGasV2(excess, used, baseFee)
	expected := excess + used - FusakaTargetBlobGasPerBlock
	if result != expected {
		t.Errorf("normal mode: got %d, want %d", result, expected)
	}
}

func TestCalcExcessBlobGasV2ExecutionFeeLed(t *testing.T) {
	// When BLOB_BASE_COST * baseFee > GAS_PER_BLOB * blobBaseFee,
	// we should NOT subtract target.
	//
	// At zero excess, blobBaseFee = MinBaseFeePerBlobGas = 2^25.
	// Reserve = BlobBaseCost * baseFee / GasPerBlob
	// For reserve > blobBaseFee: baseFee > MinBaseFeePerBlobGas * GasPerBlob / BlobBaseCost
	//   = 2^25 * 2^17 / 2^13 = 2^29 = 536870912
	// So use baseFee = 10^10 (10 gwei) which is > 2^29.
	highBaseFee := big.NewInt(10_000_000_000)
	used := uint64(FusakaMaxBlobGasPerBlock) // 9 blobs

	result := CalcExcessBlobGasV2(FusakaTargetBlobGasPerBlock, used, highBaseFee)

	// In execution-fee-led mode: excess + used * (max - target) / max
	// = FusakaTargetBlobGasPerBlock + FusakaMaxBlobGasPerBlock * (9-6)/9
	increase := used * (FusakaMaxBlobsPerBlock - FusakaTargetBlobsPerBlock) / FusakaMaxBlobsPerBlock
	expected := uint64(FusakaTargetBlobGasPerBlock) + increase

	if result != expected {
		t.Errorf("execution-fee-led mode: got %d, want %d", result, expected)
	}
}

func TestCalcExcessBlobGasV2NilBaseFee(t *testing.T) {
	// With nil base fee, should use normal mode.
	excess := uint64(FusakaTargetBlobGasPerBlock + GasPerBlob)
	used := uint64(FusakaMaxBlobGasPerBlock)

	result := CalcExcessBlobGasV2(excess, used, nil)
	expected := excess + used - FusakaTargetBlobGasPerBlock
	if result != expected {
		t.Errorf("nil baseFee: got %d, want %d", result, expected)
	}
}

func TestCalcExcessBlobGasV2AtTarget(t *testing.T) {
	// Exactly at target: excess + used == target should return 0.
	result := CalcExcessBlobGasV2(0, FusakaTargetBlobGasPerBlock, big.NewInt(1))
	if result != 0 {
		t.Errorf("at target: got %d, want 0", result)
	}
}

func TestCalcExcessBlobGasV2MaxBlobs(t *testing.T) {
	// Full blocks with max blobs.
	result := CalcExcessBlobGasV2(0, FusakaMaxBlobGasPerBlock, big.NewInt(1))
	expected := uint64(FusakaMaxBlobGasPerBlock - FusakaTargetBlobGasPerBlock)
	if result != expected {
		t.Errorf("max blobs from 0 excess: got %d, want %d", result, expected)
	}
}

// Tests for new EIP-7918 functions.

func TestBlobBaseFeeWithFloor(t *testing.T) {
	tests := []struct {
		name        string
		computedFee int64
		baseFee     *big.Int
		wantMin     int64 // fee must be >= this
	}{
		{
			name:        "nil base fee returns computed",
			computedFee: 1000,
			baseFee:     nil,
			wantMin:     1000,
		},
		{
			name:        "zero base fee returns computed",
			computedFee: 1000,
			baseFee:     big.NewInt(0),
			wantMin:     1000,
		},
		{
			name:        "low base fee, computed wins",
			computedFee: MinBaseFeePerBlobGas,
			baseFee:     big.NewInt(1),
			wantMin:     MinBaseFeePerBlobGas,
		},
		{
			name:        "high base fee, reserve wins",
			computedFee: MinBaseFeePerBlobGas,
			baseFee:     big.NewInt(30_000_000_000), // 30 gwei
			wantMin:     1_875_000_000,              // 8192 * 30e9 / 131072
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BlobBaseFeeWithFloor(big.NewInt(tt.computedFee), tt.baseFee)
			if got.Cmp(big.NewInt(tt.wantMin)) < 0 {
				t.Errorf("BlobBaseFeeWithFloor = %s, want >= %d", got, tt.wantMin)
			}
		})
	}
}

func TestBlobBaseFeeWithFloorHighBaseFeeExact(t *testing.T) {
	// Reserve = BlobBaseCost * baseFee / GasPerBlob = 8192 * 30e9 / 131072 = 1875000000
	baseFee := big.NewInt(30_000_000_000)
	computed := big.NewInt(100) // much lower than reserve
	got := BlobBaseFeeWithFloor(computed, baseFee)

	expected := new(big.Int).Mul(big.NewInt(BlobBaseCost), baseFee)
	expected.Div(expected, big.NewInt(GasPerBlob))

	if got.Cmp(expected) != 0 {
		t.Errorf("BlobBaseFeeWithFloor = %s, want %s", got, expected)
	}
}

func TestCalcBlobBaseFeeGlamst(t *testing.T) {
	// At zero excess with low base fee, should return MinBaseFeePerBlobGas.
	fee := CalcBlobBaseFeeGlamst(0, big.NewInt(1))
	if fee.Cmp(big.NewInt(MinBaseFeePerBlobGas)) != 0 {
		t.Errorf("CalcBlobBaseFeeGlamst(0, 1) = %s, want %d", fee, MinBaseFeePerBlobGas)
	}

	// At zero excess with high base fee, reserve should dominate.
	highBaseFee := big.NewInt(30_000_000_000)
	fee = CalcBlobBaseFeeGlamst(0, highBaseFee)
	reserve := CalcBlobReservePrice(highBaseFee)
	if fee.Cmp(reserve) < 0 {
		t.Errorf("CalcBlobBaseFeeGlamst = %s, should be >= reserve %s", fee, reserve)
	}
}

func TestCalcBlobReservePrice(t *testing.T) {
	// Reserve = BLOB_BASE_COST * baseFee / GAS_PER_BLOB
	tests := []struct {
		baseFee *big.Int
		want    int64
	}{
		{nil, 0},
		{big.NewInt(0), 0},
		{big.NewInt(-1), 0},
		{big.NewInt(131072), 8192},                       // 8192 * 131072 / 131072 = 8192
		{big.NewInt(30_000_000_000), 1_875_000_000},      // 8192 * 30e9 / 131072
	}

	for _, tt := range tests {
		got := CalcBlobReservePrice(tt.baseFee)
		if got.Int64() != tt.want {
			t.Errorf("CalcBlobReservePrice(%v) = %s, want %d", tt.baseFee, got, tt.want)
		}
	}
}

func TestIsExecutionFeeLed(t *testing.T) {
	// At zero excess, blob base fee = MinBaseFeePerBlobGas = 2^25.
	// Reserve > blob fee when: BlobBaseCost * baseFee > GasPerBlob * blobBaseFee
	// => baseFee > GasPerBlob * blobBaseFee / BlobBaseCost
	// => baseFee > 131072 * 33554432 / 8192 = 536870912 ~= 0.54 gwei

	blobBaseFee := big.NewInt(MinBaseFeePerBlobGas)

	// Low base fee: not execution-fee-led.
	if IsExecutionFeeLed(big.NewInt(1), blobBaseFee) {
		t.Error("should not be execution-fee-led at 1 wei base fee")
	}

	// High base fee: execution-fee-led.
	if !IsExecutionFeeLed(big.NewInt(10_000_000_000), blobBaseFee) {
		t.Error("should be execution-fee-led at 10 gwei base fee")
	}

	// Nil inputs.
	if IsExecutionFeeLed(nil, blobBaseFee) {
		t.Error("should not be execution-fee-led with nil base fee")
	}
	if IsExecutionFeeLed(big.NewInt(10_000_000_000), nil) {
		t.Error("should not be execution-fee-led with nil blob base fee")
	}
}

func TestCalcBlobBaseFeeGlamstMonotonic(t *testing.T) {
	// Fee should increase monotonically with excess blob gas.
	baseFee := big.NewInt(10_000_000_000)
	prev := CalcBlobBaseFeeGlamst(0, baseFee)
	for excess := uint64(GasPerBlob); excess <= uint64(FusakaBlobBaseFeeUpdateFraction)*2; excess += uint64(GasPerBlob) {
		cur := CalcBlobBaseFeeGlamst(excess, baseFee)
		if cur.Cmp(prev) < 0 {
			t.Fatalf("blob fee decreased at excess=%d: %s < %s", excess, cur, prev)
		}
		prev = cur
	}
}

func TestFakeExponentialV2(t *testing.T) {
	tests := []struct {
		factor      int64
		numerator   int64
		denominator int64
		want        int64
	}{
		{1, 0, 1, 1},
		{1, 1, 1, 2},
		{38, 0, 1000, 38},
		{MinBaseFeePerBlobGas, 0, FusakaBlobBaseFeeUpdateFraction, MinBaseFeePerBlobGas},
	}

	for _, tt := range tests {
		got := fakeExponentialV2(
			big.NewInt(tt.factor),
			big.NewInt(tt.numerator),
			big.NewInt(tt.denominator),
		)
		if got.Int64() != tt.want {
			t.Errorf("fakeExponentialV2(%d, %d, %d) = %d, want %d",
				tt.factor, tt.numerator, tt.denominator, got.Int64(), tt.want)
		}
	}
}
