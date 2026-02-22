package core

import (
	"math"
	"testing"
)

func TestNewConversionFactor(t *testing.T) {
	f := NewConversionFactor(1, 2)
	if f.Numerator != 1 || f.Denominator != 2 {
		t.Fatalf("got %d/%d, want 1/2", f.Numerator, f.Denominator)
	}
}

func TestNewConversionFactorPanicsOnZeroDenom(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for zero denominator")
		}
	}()
	NewConversionFactor(1, 0)
}

func TestConversionFactorApply(t *testing.T) {
	tests := []struct {
		name  string
		num   uint64
		denom uint64
		gas   uint64
		want  uint64
	}{
		{"identity", 1, 1, 100, 100},
		{"half", 1, 2, 100, 50},
		{"quarter", 1, 4, 100, 25},
		{"three_quarters", 3, 4, 100, 75},
		{"double", 2, 1, 100, 200},
		{"zero gas", 1, 2, 0, 0},
		{"result rounds down", 1, 3, 10, 3},      // 10/3 = 3.33 -> 3
		{"minimum 1 for tiny gas", 1, 100, 1, 1}, // 1/100 = 0 -> min 1
		{"tenth", 1, 10, 1000, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewConversionFactor(tt.num, tt.denom)
			got := f.Apply(tt.gas)
			if got != tt.want {
				t.Errorf("(%d/%d).Apply(%d) = %d, want %d",
					tt.num, tt.denom, tt.gas, got, tt.want)
			}
		})
	}
}

func TestConversionFactorRatio(t *testing.T) {
	f := NewConversionFactor(1, 2)
	if f.Ratio() != 0.5 {
		t.Errorf("Ratio() = %f, want 0.5", f.Ratio())
	}
	f = NewConversionFactor(3, 4)
	if f.Ratio() != 0.75 {
		t.Errorf("Ratio() = %f, want 0.75", f.Ratio())
	}
}

func TestConversionFactorFlags(t *testing.T) {
	half := NewConversionFactor(1, 2)
	if !half.IsReduction() {
		t.Error("1/2 should be a reduction")
	}
	if half.IsIncrease() {
		t.Error("1/2 should not be an increase")
	}
	if half.IsIdentity() {
		t.Error("1/2 should not be identity")
	}

	double := NewConversionFactor(2, 1)
	if double.IsReduction() {
		t.Error("2/1 should not be a reduction")
	}
	if !double.IsIncrease() {
		t.Error("2/1 should be an increase")
	}

	identity := NewConversionFactor(5, 5)
	if !identity.IsIdentity() {
		t.Error("5/5 should be identity")
	}
}

func TestNewConversionTable(t *testing.T) {
	factor := NewConversionFactor(1, 2)
	table := NewConversionTable(factor, 1)
	if table == nil {
		t.Fatal("expected non-nil table")
	}

	summary := table.Summarize()
	if summary.DefaultRatio != 0.5 {
		t.Errorf("default ratio = %f, want 0.5", summary.DefaultRatio)
	}
	if summary.ExplicitCount != 0 {
		t.Errorf("explicit count = %d, want 0", summary.ExplicitCount)
	}
	if summary.MinGas != 1 {
		t.Errorf("min gas = %d, want 1", summary.MinGas)
	}
	if !summary.IsReduction {
		t.Error("should be a reduction")
	}
}

func TestConvertGasExplicit(t *testing.T) {
	factor := NewConversionFactor(1, 2)
	table := NewConversionTable(factor, 1)

	// Set explicit override for opcode 0x54 (SLOAD).
	table.SetExplicit(0x54, 150)

	// Explicit override should take precedence.
	got := ConvertGas(table, 0x54, 2100)
	if got != 150 {
		t.Errorf("ConvertGas(SLOAD) = %d, want 150 (explicit)", got)
	}

	// Non-explicit opcode should use default factor.
	got = ConvertGas(table, 0x55, 2000)
	if got != 1000 { // 2000 * 1/2 = 1000
		t.Errorf("ConvertGas(0x55) = %d, want 1000 (default factor)", got)
	}
}

func TestConvertGasMinimum(t *testing.T) {
	// Factor 1/100 with minGas 5.
	factor := NewConversionFactor(1, 100)
	table := NewConversionTable(factor, 5)

	// Gas 10: 10/100 = 0 -> should use factor.Apply minimum of 1,
	// then enforce table minimum of 5.
	got := ConvertGas(table, 0x01, 10)
	if got != 5 {
		t.Errorf("ConvertGas with low gas = %d, want 5 (min)", got)
	}

	// Gas 0 should return 0 (zero is always zero).
	got = ConvertGas(table, 0x01, 0)
	if got != 0 {
		t.Errorf("ConvertGas(0) = %d, want 0", got)
	}
}

func TestGetExplicit(t *testing.T) {
	factor := NewConversionFactor(1, 1)
	table := NewConversionTable(factor, 1)

	table.SetExplicit(0x54, 800)

	gas, ok := table.GetExplicit(0x54)
	if !ok || gas != 800 {
		t.Errorf("GetExplicit(0x54) = %d, %v, want 800, true", gas, ok)
	}

	_, ok = table.GetExplicit(0x55)
	if ok {
		t.Error("GetExplicit(0x55) should return false for unmapped opcode")
	}
}

func TestBatchConvert(t *testing.T) {
	factor := NewConversionFactor(1, 2)
	table := NewConversionTable(factor, 1)
	table.SetExplicit(0x54, 150)

	ops := []GasOp{
		{Opcode: 0x54, Gas: 2100}, // explicit -> 150
		{Opcode: 0x55, Gas: 2000}, // default -> 1000
		{Opcode: 0xF1, Gas: 100},  // default -> 50
	}

	results := BatchConvert(table, ops)
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}

	expected := []uint64{150, 1000, 50}
	for i, want := range expected {
		if results[i] != want {
			t.Errorf("results[%d] = %d, want %d", i, results[i], want)
		}
	}
}

func TestBatchConvertDetailed(t *testing.T) {
	factor := NewConversionFactor(3, 4)
	table := NewConversionTable(factor, 1)

	ops := []GasOp{
		{Opcode: 0x54, Gas: 2000},
		{Opcode: 0x55, Gas: 400},
	}

	results := BatchConvertDetailed(table, ops)
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}

	// 2000 * 3/4 = 1500
	if results[0].OriginalGas != 2000 || results[0].ConvertedGas != 1500 {
		t.Errorf("result[0] = {orig: %d, conv: %d}, want {2000, 1500}",
			results[0].OriginalGas, results[0].ConvertedGas)
	}

	// 400 * 3/4 = 300
	if results[1].OriginalGas != 400 || results[1].ConvertedGas != 300 {
		t.Errorf("result[1] = {orig: %d, conv: %d}, want {400, 300}",
			results[1].OriginalGas, results[1].ConvertedGas)
	}
}

func TestBatchConvertEmpty(t *testing.T) {
	factor := NewConversionFactor(1, 2)
	table := NewConversionTable(factor, 1)

	results := BatchConvert(table, nil)
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0 for nil input", len(results))
	}

	results = BatchConvert(table, []GasOp{})
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0 for empty input", len(results))
	}
}

func TestApplyConversion(t *testing.T) {
	table := DefaultHogotaGasTable()
	factor := NewConversionFactor(1, 2)

	result := ApplyConversion(table, factor)

	// Should return the same pointer.
	if result != table {
		t.Fatal("ApplyConversion should return the same pointer")
	}

	// All values should be halved.
	checks := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"SloadCold", table.SloadCold, 100},     // 200/2
		{"SloadWarm", table.SloadWarm, 50},      // 100/2
		{"SstoreCold", table.SstoreCold, 1250},  // 2500/2
		{"SstoreWarm", table.SstoreWarm, 50},    // 100/2
		{"CallCold", table.CallCold, 50},        // 100/2
		{"BalanceCold", table.BalanceCold, 100}, // 200/2
		{"Create", table.Create, 4000},          // 8000/2
		{"ExtCodeSize", table.ExtCodeSize, 100}, // 200/2
		{"Log", table.Log, 150},                 // 300/2
		{"LogData", table.LogData, 3},           // 6/2
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestApplyConversionIdentity(t *testing.T) {
	table := DefaultHogotaGasTable()
	original := *table // copy
	factor := NewConversionFactor(1, 1)

	ApplyConversion(table, factor)

	// All values should be unchanged.
	if table.SloadCold != original.SloadCold {
		t.Errorf("SloadCold changed from %d to %d", original.SloadCold, table.SloadCold)
	}
	if table.Create != original.Create {
		t.Errorf("Create changed from %d to %d", original.Create, table.Create)
	}
}

func TestPreDefinedConversionFactors(t *testing.T) {
	expected := map[string]float64{
		"half":           0.5,
		"quarter":        0.25,
		"three_quarters": 0.75,
		"tenth":          0.1,
		"identity":       1.0,
	}

	for name, wantRatio := range expected {
		f, ok := PreDefinedConversionFactors[name]
		if !ok {
			t.Errorf("missing predefined factor %q", name)
			continue
		}
		got := f.Ratio()
		if got != wantRatio {
			t.Errorf("factor %q ratio = %f, want %f", name, got, wantRatio)
		}
	}
}

func TestTotalGasSavings(t *testing.T) {
	factor := NewConversionFactor(1, 2)
	table := NewConversionTable(factor, 1)

	ops := []GasOp{
		{Opcode: 0x54, Gas: 200},
		{Opcode: 0x55, Gas: 400},
	}

	savings := TotalGasSavings(table, ops)
	// Original: 200 + 400 = 600
	// Converted: 100 + 200 = 300
	// Savings: 300
	if savings != 300 {
		t.Errorf("TotalGasSavings = %d, want 300", savings)
	}
}

func TestTotalGasSavingsNoSavings(t *testing.T) {
	factor := NewConversionFactor(2, 1) // double (increase)
	table := NewConversionTable(factor, 1)

	ops := []GasOp{
		{Opcode: 0x54, Gas: 200},
	}

	savings := TotalGasSavings(table, ops)
	if savings != 0 {
		t.Errorf("TotalGasSavings = %d, want 0 (factor is an increase)", savings)
	}
}

func TestConversionSummary(t *testing.T) {
	factor := NewConversionFactor(1, 4)
	table := NewConversionTable(factor, 2)
	table.SetExplicit(0x54, 100)
	table.SetExplicit(0x55, 200)

	summary := table.Summarize()
	if summary.DefaultRatio != 0.25 {
		t.Errorf("default ratio = %f, want 0.25", summary.DefaultRatio)
	}
	if summary.ExplicitCount != 2 {
		t.Errorf("explicit count = %d, want 2", summary.ExplicitCount)
	}
	if summary.MinGas != 2 {
		t.Errorf("min gas = %d, want 2", summary.MinGas)
	}
	if !summary.IsReduction {
		t.Error("should be a reduction")
	}
}

func TestSafeMulDiv(t *testing.T) {
	tests := []struct {
		name    string
		a, b, c uint64
		want    uint64
	}{
		{"simple", 100, 3, 4, 75},
		{"zero a", 0, 100, 50, 0},
		{"zero b", 100, 0, 50, 0},
		{"identity", 100, 1, 1, 100},
		{"exact division", 200, 1, 2, 100},
		{"large values no overflow", 1000000, 1000000, 1000000, 1000000},
		{"overflow intermediate", math.MaxUint64, 2, 2, math.MaxUint64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeMulDiv(tt.a, tt.b, tt.c)
			if got != tt.want {
				t.Errorf("safeMulDiv(%d, %d, %d) = %d, want %d",
					tt.a, tt.b, tt.c, got, tt.want)
			}
		})
	}
}

func TestSafeMulDivZeroDenom(t *testing.T) {
	got := safeMulDiv(100, 200, 0)
	if got != math.MaxUint64 {
		t.Errorf("safeMulDiv(100, 200, 0) = %d, want MaxUint64", got)
	}
}

func TestConvertGasTypicalOpcodes(t *testing.T) {
	// Simulate a post-Hogota conversion that halves all costs.
	factor := NewConversionFactor(1, 2)
	table := NewConversionTable(factor, 1)

	// Set some explicit overrides for critical opcodes.
	table.SetExplicit(0x54, 100)  // SLOAD -> 100
	table.SetExplicit(0x55, 1250) // SSTORE -> 1250

	tests := []struct {
		name   string
		opcode byte
		oldGas uint64
		want   uint64
	}{
		{"SLOAD explicit", 0x54, 200, 100},
		{"SSTORE explicit", 0x55, 2500, 1250},
		{"CALL default factor", 0xF1, 100, 50},
		{"CREATE default factor", 0xF0, 8000, 4000},
		{"BALANCE default factor", 0x31, 200, 100},
		{"LOG default factor", 0xA0, 300, 150},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertGas(table, tt.opcode, tt.oldGas)
			if got != tt.want {
				t.Errorf("ConvertGas(0x%02X, %d) = %d, want %d",
					tt.opcode, tt.oldGas, got, tt.want)
			}
		})
	}
}

func TestProgressiveConversionChain(t *testing.T) {
	// Simulate applying successive conversion factors (fork progression).
	table := DefaultHogotaGasTable()

	// First conversion: reduce by 25% (3/4).
	ApplyConversion(table, NewConversionFactor(3, 4))
	if table.SloadCold != 150 { // 200 * 3/4 = 150
		t.Errorf("after 3/4: SloadCold = %d, want 150", table.SloadCold)
	}

	// Second conversion: reduce by another 50% (1/2).
	ApplyConversion(table, NewConversionFactor(1, 2))
	if table.SloadCold != 75 { // 150 * 1/2 = 75
		t.Errorf("after 1/2: SloadCold = %d, want 75", table.SloadCold)
	}

	// Total reduction: 200 -> 75 = 62.5% reduction, ~2.67x throughput.
}

func TestConversionPreservesMinimum(t *testing.T) {
	table := DefaultHogotaGasTable()

	// Apply an extreme reduction.
	ApplyConversion(table, NewConversionFactor(1, 10000))

	// No field should be 0 (minimum of 1 enforced).
	if table.SloadCold == 0 {
		t.Error("SloadCold should not be 0 after extreme reduction")
	}
	if table.SloadWarm == 0 {
		t.Error("SloadWarm should not be 0 after extreme reduction")
	}
	if table.Create == 0 {
		t.Error("Create should not be 0 after extreme reduction")
	}
}
