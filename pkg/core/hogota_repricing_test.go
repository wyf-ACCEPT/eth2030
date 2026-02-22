package core

import (
	"math"
	"math/big"
	"testing"
)

func TestDefaultHogotaGasTable(t *testing.T) {
	table := DefaultHogotaGasTable()

	checks := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"SloadCold", table.SloadCold, 200},
		{"SloadWarm", table.SloadWarm, 100},
		{"SstoreCold", table.SstoreCold, 2500},
		{"SstoreWarm", table.SstoreWarm, 100},
		{"CallCold", table.CallCold, 100},
		{"CallWarm", table.CallWarm, 100},
		{"BalanceCold", table.BalanceCold, 200},
		{"BalanceWarm", table.BalanceWarm, 100},
		{"Create", table.Create, 8000},
		{"ExtCodeSize", table.ExtCodeSize, 200},
		{"ExtCodeCopy", table.ExtCodeCopy, 200},
		{"ExtCodeHash", table.ExtCodeHash, 200},
		{"Log", table.Log, 300},
		{"LogData", table.LogData, 6},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestApplyHogotaRepricing(t *testing.T) {
	// Start with a table that has Glamsterdam values.
	table := &HogotaGasTable{
		SloadCold:   800,
		SstoreCold:  5000,
		SstoreWarm:  1500,
		BalanceCold: 400,
		Create:      10000,
		ExtCodeSize: 400,
		Log:         375,
		LogData:     8,
	}

	result := ApplyHogotaRepricing(table)

	// Should return the same pointer.
	if result != table {
		t.Fatal("ApplyHogotaRepricing should return the same pointer")
	}

	// Verify all values were updated to Hogota values.
	if table.SloadCold != 200 {
		t.Errorf("SloadCold = %d, want 200", table.SloadCold)
	}
	if table.SstoreCold != 2500 {
		t.Errorf("SstoreCold = %d, want 2500", table.SstoreCold)
	}
	if table.SstoreWarm != 100 {
		t.Errorf("SstoreWarm = %d, want 100", table.SstoreWarm)
	}
	if table.BalanceCold != 200 {
		t.Errorf("BalanceCold = %d, want 200", table.BalanceCold)
	}
	if table.Create != 8000 {
		t.Errorf("Create = %d, want 8000", table.Create)
	}
	if table.ExtCodeSize != 200 {
		t.Errorf("ExtCodeSize = %d, want 200", table.ExtCodeSize)
	}
	if table.Log != 300 {
		t.Errorf("Log = %d, want 300", table.Log)
	}
	if table.LogData != 6 {
		t.Errorf("LogData = %d, want 6", table.LogData)
	}
}

func TestDefaultStateAccessRepricing(t *testing.T) {
	sar := DefaultStateAccessRepricing()

	if sar.SloadCold != 200 {
		t.Errorf("SloadCold = %d, want 200", sar.SloadCold)
	}
	if sar.SstoreCold != 2500 {
		t.Errorf("SstoreCold = %d, want 2500", sar.SstoreCold)
	}
	if sar.SstoreWarm != 100 {
		t.Errorf("SstoreWarm = %d, want 100", sar.SstoreWarm)
	}
	if sar.BalanceCold != 200 {
		t.Errorf("BalanceCold = %d, want 200", sar.BalanceCold)
	}
}

func TestHogotaRepricingDeltas(t *testing.T) {
	deltas := HogotaRepricingDeltas()

	// All deltas should be positive (cost reductions).
	for name, delta := range deltas {
		if delta <= 0 {
			t.Errorf("%s delta = %d, expected positive (cost reduction)", name, delta)
		}
	}

	// Check specific known values.
	checks := map[string]int64{
		"SLOAD_COLD":    600,
		"SSTORE_COLD":   2500,
		"SSTORE_WARM":   1400,
		"BALANCE_COLD":  200,
		"CREATE":        2000,
		"EXTCODESIZE":   200,
		"EXTCODECOPY":   200,
		"EXTCODEHASH":   200,
		"LOG_BASE":      75,
		"LOG_DATA_BYTE": 2,
	}

	for name, want := range checks {
		got, ok := deltas[name]
		if !ok {
			t.Errorf("missing delta for %s", name)
			continue
		}
		if got != want {
			t.Errorf("%s delta = %d, want %d", name, got, want)
		}
	}
}

func TestDefaultBlobBaseFeePricing(t *testing.T) {
	pricing := DefaultBlobBaseFeePricing()

	if pricing.MinBlobBaseFee != 1<<25 {
		t.Errorf("MinBlobBaseFee = %d, want %d", pricing.MinBlobBaseFee, 1<<25)
	}
	if pricing.BlobBaseFeeUpdateFraction != 5376681 {
		t.Errorf("BlobBaseFeeUpdateFraction = %d, want 5376681", pricing.BlobBaseFeeUpdateFraction)
	}
	if pricing.TargetBlobsPerBlock != 6 {
		t.Errorf("TargetBlobsPerBlock = %d, want 6", pricing.TargetBlobsPerBlock)
	}
	if pricing.MaxBlobsPerBlock != 9 {
		t.Errorf("MaxBlobsPerBlock = %d, want 9", pricing.MaxBlobsPerBlock)
	}
}

func TestComputeHogotaBlobFee(t *testing.T) {
	tests := []struct {
		name          string
		excessBlobGas uint64
		blobCount     uint64
		wantMin       uint64
		wantNonZero   bool
	}{
		{
			name:          "zero blobs",
			excessBlobGas: 0,
			blobCount:     0,
			wantMin:       0,
		},
		{
			name:          "one blob, zero excess",
			excessBlobGas: 0,
			blobCount:     1,
			wantNonZero:   true,
		},
		{
			name:          "three blobs, zero excess",
			excessBlobGas: 0,
			blobCount:     3,
			wantNonZero:   true,
		},
		{
			name:          "one blob, moderate excess",
			excessBlobGas: 1_000_000,
			blobCount:     1,
			wantNonZero:   true,
		},
		{
			name:          "max blobs, zero excess",
			excessBlobGas: 0,
			blobCount:     9,
			wantNonZero:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeHogotaBlobFee(tt.excessBlobGas, tt.blobCount)
			if tt.wantNonZero && got == 0 {
				t.Error("expected non-zero blob fee")
			}
			if got < tt.wantMin {
				t.Errorf("blob fee = %d, want >= %d", got, tt.wantMin)
			}
		})
	}
}

func TestComputeHogotaBlobFeeMonotonicallyIncreasing(t *testing.T) {
	// As excess blob gas increases, the fee should increase.
	prev := ComputeHogotaBlobFee(0, 1)
	for excess := uint64(GasPerBlob); excess <= 10*GasPerBlob; excess += GasPerBlob {
		cur := ComputeHogotaBlobFee(excess, 1)
		if cur < prev {
			t.Fatalf("blob fee decreased at excess=%d: %d < %d", excess, cur, prev)
		}
		prev = cur
	}
}

func TestComputeHogotaBlobFeeLinearInBlobCount(t *testing.T) {
	// Fee should scale linearly with blob count.
	fee1 := ComputeHogotaBlobFee(0, 1)
	fee3 := ComputeHogotaBlobFee(0, 3)

	// fee3 should be 3x fee1.
	if fee3 != fee1*3 {
		t.Errorf("fee(3 blobs) = %d, want 3 * fee(1 blob) = %d", fee3, fee1*3)
	}
}

func TestComputeHogotaBlobBaseFee(t *testing.T) {
	// At zero excess, the base fee should equal the minimum.
	baseFee := ComputeHogotaBlobBaseFee(0)
	pricing := DefaultBlobBaseFeePricing()
	if baseFee != pricing.MinBlobBaseFee {
		t.Errorf("base fee at zero excess = %d, want %d",
			baseFee, pricing.MinBlobBaseFee)
	}

	// At higher excess, the base fee should be higher.
	higherFee := ComputeHogotaBlobBaseFee(10_000_000)
	if higherFee <= baseFee {
		t.Errorf("base fee at high excess (%d) should exceed zero-excess fee (%d)",
			higherFee, baseFee)
	}
}

func TestComputeHogotaExcessBlobGas(t *testing.T) {
	pricing := DefaultBlobBaseFeePricing()
	targetGas := pricing.TargetBlobsPerBlock * GasPerBlob

	tests := []struct {
		name         string
		parentExcess uint64
		parentUsed   uint64
		want         uint64
	}{
		{
			name:         "both zero",
			parentExcess: 0,
			parentUsed:   0,
			want:         0,
		},
		{
			name:         "below target",
			parentExcess: 0,
			parentUsed:   GasPerBlob, // 1 blob
			want:         0,          // 1 blob < 6 target
		},
		{
			name:         "at target",
			parentExcess: 0,
			parentUsed:   targetGas,
			want:         0, // exactly at target
		},
		{
			name:         "above target",
			parentExcess: 0,
			parentUsed:   targetGas + GasPerBlob, // 7 blobs
			want:         GasPerBlob,             // 1 blob excess
		},
		{
			name:         "accumulated excess",
			parentExcess: 500_000,
			parentUsed:   targetGas + GasPerBlob,
			want:         500_000 + GasPerBlob,
		},
		{
			name:         "excess decreases below target",
			parentExcess: targetGas,
			parentUsed:   0,
			want:         0, // 0 + 0 < target
		},
		{
			name:         "excess partially decreases",
			parentExcess: targetGas + 2*GasPerBlob,
			parentUsed:   0,
			want:         2 * GasPerBlob, // excess + 0 - target
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeHogotaExcessBlobGas(tt.parentExcess, tt.parentUsed)
			if got != tt.want {
				t.Errorf("ComputeHogotaExcessBlobGas(%d, %d) = %d, want %d",
					tt.parentExcess, tt.parentUsed, got, tt.want)
			}
		})
	}
}

func TestHogotaFakeExponential(t *testing.T) {
	// At numerator=0, result should equal factor.
	got := hogotaFakeExponential(100, 0, 1000)
	if got != 100 {
		t.Errorf("fakeExponential(100, 0, 1000) = %d, want 100", got)
	}

	// For factor=1, numerator=denominator, result should approximate e (~2.718).
	got = hogotaFakeExponential(1000000, 1000000, 1000000)
	// e * 1000000 ~ 2718281
	if got < 2700000 || got > 2720000 {
		t.Errorf("fakeExponential approximation of e*1M = %d, want ~2718281", got)
	}

	// Large factor with moderate exponent should cap at MaxUint64.
	got = hogotaFakeExponential(math.MaxUint64/2, 100, 1)
	if got != math.MaxUint64 {
		t.Errorf("fakeExponential with huge factor = %d, want MaxUint64", got)
	}
}

func TestIsHogotaActive(t *testing.T) {
	tests := []struct {
		name      string
		blockNum  *big.Int
		forkBlock *big.Int
		want      bool
	}{
		{"nil block num", nil, big.NewInt(100), false},
		{"nil fork block", big.NewInt(100), nil, false},
		{"before fork", big.NewInt(99), big.NewInt(100), false},
		{"at fork", big.NewInt(100), big.NewInt(100), true},
		{"after fork", big.NewInt(200), big.NewInt(100), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsHogotaActive(tt.blockNum, tt.forkBlock)
			if got != tt.want {
				t.Errorf("IsHogotaActive(%v, %v) = %v, want %v",
					tt.blockNum, tt.forkBlock, got, tt.want)
			}
		})
	}
}

func TestHogotaGasReduction(t *testing.T) {
	// Verify the DeFi pattern gas reduction calculation.
	reduction := HogotaGasReduction()

	// Glamsterdam: 5*800 + 2*5000 + 100 + 2*400 = 4000 + 10000 + 100 + 800 = 14900
	// Hogota: 5*200 + 2*2500 + 100 + 2*200 = 1000 + 5000 + 100 + 400 = 6500
	// Reduction: 14900 - 6500 = 8400
	expectedReduction := uint64(8400)
	if reduction != expectedReduction {
		t.Errorf("HogotaGasReduction = %d, want %d", reduction, expectedReduction)
	}
}

func TestHogotaCostsLowerThanGlamsterdam(t *testing.T) {
	hogota := DefaultHogotaGasTable()
	glamst := DefaultGlamsterdamGasTable()

	// All Hogota cold access costs should be <= Glamsterdam.
	if hogota.SloadCold > glamst.SloadCold {
		t.Errorf("Hogota SloadCold (%d) > Glamsterdam (%d)", hogota.SloadCold, glamst.SloadCold)
	}
	if hogota.BalanceCold > glamst.BalanceCold {
		t.Errorf("Hogota BalanceCold (%d) > Glamsterdam (%d)", hogota.BalanceCold, glamst.BalanceCold)
	}
	if hogota.Create > glamst.Create {
		t.Errorf("Hogota Create (%d) > Glamsterdam (%d)", hogota.Create, glamst.Create)
	}
	if hogota.ExtCodeSize > glamst.ExtCodeSize {
		t.Errorf("Hogota ExtCodeSize (%d) > Glamsterdam (%d)", hogota.ExtCodeSize, glamst.ExtCodeSize)
	}
}

func TestBlobFeePricingConsistency(t *testing.T) {
	pricing := DefaultBlobBaseFeePricing()

	// Max blobs should be greater than target.
	if pricing.MaxBlobsPerBlock <= pricing.TargetBlobsPerBlock {
		t.Errorf("MaxBlobsPerBlock (%d) should be > TargetBlobsPerBlock (%d)",
			pricing.MaxBlobsPerBlock, pricing.TargetBlobsPerBlock)
	}

	// MinBlobBaseFee should be > 0.
	if pricing.MinBlobBaseFee == 0 {
		t.Error("MinBlobBaseFee should be > 0")
	}

	// Update fraction should be > 0.
	if pricing.BlobBaseFeeUpdateFraction == 0 {
		t.Error("BlobBaseFeeUpdateFraction should be > 0")
	}
}

func TestComputeHogotaBlobFeeOverflow(t *testing.T) {
	// Large excess (but not astronomically large) should not panic.
	// excessBlobGas of 1 billion should produce a very high fee.
	fee := ComputeHogotaBlobFee(1_000_000_000, 9)
	if fee == 0 {
		t.Error("expected non-zero fee for large excess")
	}

	// Extremely large excess should cap at MaxUint64 due to overflow protection.
	fee = ComputeHogotaBlobFee(math.MaxUint64/2, 1)
	if fee != math.MaxUint64 {
		t.Errorf("expected MaxUint64 for extreme excess, got %d", fee)
	}
}

func TestValidateHogotaGas(t *testing.T) {
	// Nil table.
	if err := ValidateHogotaGas(nil); err == nil {
		t.Fatal("expected error for nil gas table")
	}

	// Valid default table.
	table := DefaultHogotaGasTable()
	if err := ValidateHogotaGas(table); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cold < warm should fail.
	bad := DefaultHogotaGasTable()
	bad.SloadCold = 50
	bad.SloadWarm = 100
	if err := ValidateHogotaGas(bad); err == nil {
		t.Fatal("expected error when cold < warm")
	}

	// Zero SLOAD should fail.
	bad2 := DefaultHogotaGasTable()
	bad2.SloadCold = 0
	if err := ValidateHogotaGas(bad2); err == nil {
		t.Fatal("expected error for zero SLOAD cold")
	}

	// Create too low.
	bad3 := DefaultHogotaGasTable()
	bad3.Create = 500
	if err := ValidateHogotaGas(bad3); err == nil {
		t.Fatal("expected error for CREATE gas too low")
	}
}
