package vm

import (
	"math"
	"testing"
)

func TestCalcCallGas_63_64Rule(t *testing.T) {
	calc := NewDefaultGasCalculator()

	tests := []struct {
		name      string
		available uint64
		requested uint64
		codeSize  uint64
		wantGas   uint64
	}{
		{"requested less than max", 6400, 100, 0, 100},
		{"requested equals max", 6400, 6300, 0, 6300},
		{"requested exceeds max, capped", 6400, 10000, 0, 6300},
		{"zero available", 0, 100, 0, 0},
		{"zero requested", 6400, 0, 0, 0},
		{"large available", 1_000_000, 500_000, 1024, 500_000},
		{"large available, capped", 1_000_000, 999_999, 0, 984375},
		{"exact 63/64 boundary", 64, 63, 0, 63},
		{"one gas available", 1, 1, 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calc.CalcCallGas(tt.available, tt.requested, tt.codeSize)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantGas {
				t.Errorf("CalcCallGas(%d, %d, %d) = %d, want %d",
					tt.available, tt.requested, tt.codeSize, got, tt.wantGas)
			}
		})
	}
}

func TestCalcCallGas_ZeroFraction(t *testing.T) {
	calc := NewDynamicGasCalculator(GasPricingRules{CallGasFraction: 0})
	_, err := calc.CalcCallGas(100, 50, 0)
	if err != ErrGasOverflow {
		t.Fatalf("expected ErrGasOverflow, got %v", err)
	}
}

func TestCalcExpGas(t *testing.T) {
	calc := NewDefaultGasCalculator()

	tests := []struct {
		name    string
		expLen  uint64
		wantGas uint64
	}{
		{"zero exponent", 0, 10},
		{"1 byte exponent", 1, 60},
		{"2 byte exponent", 2, 110},
		{"32 byte exponent (max uint256)", 32, 1610},
		{"8 byte exponent", 8, 410},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calc.CalcExpGas(tt.expLen)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantGas {
				t.Errorf("CalcExpGas(%d) = %d, want %d", tt.expLen, got, tt.wantGas)
			}
		})
	}
}

func TestCalcSStoreGas_CleanSlot(t *testing.T) {
	calc := NewDefaultGasCalculator()

	var zero, nonZeroA, nonZeroB [32]byte
	nonZeroA[31] = 1
	nonZeroB[31] = 2

	tests := []struct {
		name       string
		current    [32]byte
		original   [32]byte
		newVal     [32]byte
		cold       bool
		wantGas    uint64
		wantRefund uint64
	}{
		{"no-op warm", nonZeroA, nonZeroA, nonZeroA, false, 100, 0},
		{"no-op cold", nonZeroA, nonZeroA, nonZeroA, true, 2200, 0},
		{"create slot 0->1", zero, zero, nonZeroA, false, 20000, 0},
		{"create slot cold", zero, zero, nonZeroA, true, 22100, 0},
		{"update slot", nonZeroA, nonZeroA, nonZeroB, false, 2900, 0},
		{"delete slot", nonZeroA, nonZeroA, zero, false, 2900, 4800},
		{"dirty slot warm", nonZeroB, nonZeroA, nonZeroA, false, 100, 2800},
		{"dirty slot restore from zero", zero, zero, zero, false, 100, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gas, refund, err := calc.CalcSStoreGas(tt.current, tt.original, tt.newVal, tt.cold)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gas != tt.wantGas {
				t.Errorf("gas = %d, want %d", gas, tt.wantGas)
			}
			if refund != tt.wantRefund {
				t.Errorf("refund = %d, want %d", refund, tt.wantRefund)
			}
		})
	}
}

func TestCalcSStoreGas_DirtySlot(t *testing.T) {
	calc := NewDefaultGasCalculator()

	var zero, one, two [32]byte
	one[31] = 1
	two[31] = 2

	// original=1, current=2, new=0 -> clear dirty non-zero slot
	gas, refund, err := calc.CalcSStoreGas(two, one, zero, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 100 {
		t.Errorf("gas = %d, want 100", gas)
	}
	if refund != 4800 {
		t.Errorf("refund = %d, want 4800", refund)
	}

	// original=1, current=0, new=1 -> restore from cleared to original
	gas, refund, err = calc.CalcSStoreGas(zero, one, one, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 100 {
		t.Errorf("gas = %d, want 100", gas)
	}
	// Restoring original non-zero: refund = SstoreResetGas - WarmReadGas = 2800
	if refund != 2800 {
		t.Errorf("refund = %d, want 2800", refund)
	}
}

func TestCalcLogGas(t *testing.T) {
	calc := NewDefaultGasCalculator()

	tests := []struct {
		name     string
		topics   int
		dataSize uint64
		wantGas  uint64
		wantErr  bool
	}{
		{"LOG0 no data", 0, 0, 375, false},
		{"LOG0 with data", 0, 100, 1175, false},
		{"LOG1 no data", 1, 0, 750, false},
		{"LOG2 with data", 2, 32, 1381, false},
		{"LOG4 with data", 4, 64, 2387, false},
		{"invalid topic count -1", -1, 0, 0, true},
		{"invalid topic count 5", 5, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calc.CalcLogGas(tt.topics, tt.dataSize)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantGas {
				t.Errorf("CalcLogGas(%d, %d) = %d, want %d", tt.topics, tt.dataSize, got, tt.wantGas)
			}
		})
	}
}

func TestCalcKeccak256Gas(t *testing.T) {
	calc := NewDefaultGasCalculator()

	tests := []struct {
		name    string
		size    uint64
		wantGas uint64
	}{
		{"empty data", 0, 30},
		{"1 byte", 1, 36},
		{"32 bytes (1 word)", 32, 36},
		{"33 bytes (2 words)", 33, 42},
		{"64 bytes (2 words)", 64, 42},
		{"256 bytes (8 words)", 256, 78},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calc.CalcKeccak256Gas(tt.size)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantGas {
				t.Errorf("CalcKeccak256Gas(%d) = %d, want %d", tt.size, got, tt.wantGas)
			}
		})
	}
}

func TestCalcCopyGas(t *testing.T) {
	calc := NewDefaultGasCalculator()

	tests := []struct {
		size    uint64
		wantGas uint64
	}{
		{0, 0},
		{1, 3},
		{32, 3},
		{33, 6},
		{64, 6},
		{256, 24},
	}

	for _, tt := range tests {
		got, err := calc.CalcCopyGas(tt.size)
		if err != nil {
			t.Fatalf("CalcCopyGas(%d): unexpected error: %v", tt.size, err)
		}
		if got != tt.wantGas {
			t.Errorf("CalcCopyGas(%d) = %d, want %d", tt.size, got, tt.wantGas)
		}
	}
}

func TestCalcCreateGas(t *testing.T) {
	calc := NewDefaultGasCalculator()

	tests := []struct {
		name      string
		size      uint64
		isCreate2 bool
		wantGas   uint64
		wantErr   bool
	}{
		{"CREATE empty", 0, false, 32000, false},
		{"CREATE 32 bytes", 32, false, 32002, false},
		{"CREATE 64 bytes", 64, false, 32004, false},
		{"CREATE2 empty", 0, true, 32000, false},
		{"CREATE2 32 bytes", 32, true, 32008, false},
		{"CREATE2 64 bytes", 64, true, 32016, false},
		{"CREATE max size", 49152, false, 32000 + 2*1536, false},
		{"CREATE exceeds max", 49153, false, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calc.CalcCreateGas(tt.size, tt.isCreate2)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantGas {
				t.Errorf("CalcCreateGas(%d, %v) = %d, want %d",
					tt.size, tt.isCreate2, got, tt.wantGas)
			}
		})
	}
}

func TestCalcSelfDestructGas(t *testing.T) {
	calc := NewDefaultGasCalculator()

	tests := []struct {
		name         string
		targetExists bool
		hasValue     bool
		coldAccess   bool
		wantGas      uint64
	}{
		{"warm, exists, no value", true, false, false, 5000},
		{"warm, exists, has value", true, true, false, 5000},
		{"warm, not exists, no value", false, false, false, 5000},
		{"warm, not exists, has value", false, true, false, 30000},
		{"cold, exists, no value", true, false, true, 7600},
		{"cold, not exists, has value", false, true, true, 32600},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calc.CalcSelfDestructGas(tt.targetExists, tt.hasValue, tt.coldAccess)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantGas {
				t.Errorf("CalcSelfDestructGas(%v, %v, %v) = %d, want %d",
					tt.targetExists, tt.hasValue, tt.coldAccess, got, tt.wantGas)
			}
		})
	}
}

func TestGlamsterdamPricing(t *testing.T) {
	calc := NewDynamicGasCalculator(GlamsterdamPricingRules())

	// Verify increased SSTORE set cost (EIP-8037).
	var zero, nonZero [32]byte
	nonZero[31] = 1

	gas, _, err := calc.CalcSStoreGas(zero, zero, nonZero, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != GasSstoreSetGlamsterdam {
		t.Errorf("Glamsterdam SSTORE set = %d, want %d", gas, GasSstoreSetGlamsterdam)
	}

	// Verify increased cold access cost.
	gas, _, err = calc.CalcSStoreGas(nonZero, nonZero, nonZero, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// no-op cold: ColdSloadGlamst + WarmStorageReadGlamst
	wantGas := ColdSloadGlamst + WarmStorageReadGlamst
	if gas != wantGas {
		t.Errorf("Glamsterdam SSTORE noop cold = %d, want %d", gas, wantGas)
	}
}

func TestDgHelpers(t *testing.T) {
	// dgSafeAdd overflow.
	if got := dgSafeAdd(math.MaxUint64, 1); got != math.MaxUint64 {
		t.Errorf("dgSafeAdd overflow: got %d, want MaxUint64", got)
	}

	// dgSafeMul overflow.
	if got := dgSafeMul(math.MaxUint64, 2); got != math.MaxUint64 {
		t.Errorf("dgSafeMul overflow: got %d, want MaxUint64", got)
	}

	// dgSafeMul zero.
	if got := dgSafeMul(0, math.MaxUint64); got != 0 {
		t.Errorf("dgSafeMul(0, max) = %d, want 0", got)
	}

	// dgToWordSize edge cases.
	if got := dgToWordSize(0); got != 0 {
		t.Errorf("dgToWordSize(0) = %d, want 0", got)
	}
	if got := dgToWordSize(math.MaxUint64); got != math.MaxUint64/32+1 {
		t.Errorf("dgToWordSize(MaxUint64) = %d, want %d", got, math.MaxUint64/32+1)
	}

	// dgIsZero.
	var zero, nonZero [32]byte
	nonZero[15] = 0xff
	if !dgIsZero(zero) {
		t.Error("dgIsZero(zero) = false, want true")
	}
	if dgIsZero(nonZero) {
		t.Error("dgIsZero(nonZero) = true, want false")
	}
}
