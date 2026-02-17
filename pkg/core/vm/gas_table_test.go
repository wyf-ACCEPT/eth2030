package vm

import (
	"math/big"
	"testing"
)

func TestMemoryGasCost(t *testing.T) {
	tests := []struct {
		size uint64
		want uint64
	}{
		{0, 0},
		{32, 3},           // 1 word * 3 + 1^2/512
		{64, 6},           // 2 words * 3 + 4/512
		{1024, 98},        // 32 words * 3 + 32^2/512 = 96 + 2 = 98
		{32768, 5120},     // 1024 words * 3 + 1024^2/512 = 3072 + 2048
	}

	for _, tt := range tests {
		got := MemoryGasCost(tt.size)
		if got != tt.want {
			t.Errorf("MemoryGasCost(%d) = %d, want %d", tt.size, got, tt.want)
		}
	}
}

func TestMemoryExpansionGas(t *testing.T) {
	// No expansion.
	if gas := MemoryExpansionGas(100, 50); gas != 0 {
		t.Errorf("expected 0 for no expansion, got %d", gas)
	}

	// Expansion from 0 to 32.
	gas := MemoryExpansionGas(0, 32)
	expected := MemoryGasCost(32)
	if gas != expected {
		t.Errorf("MemoryExpansionGas(0, 32) = %d, want %d", gas, expected)
	}

	// Expansion from 32 to 64.
	gas = MemoryExpansionGas(32, 64)
	expected = MemoryGasCost(64) - MemoryGasCost(32)
	if gas != expected {
		t.Errorf("MemoryExpansionGas(32, 64) = %d, want %d", gas, expected)
	}
}

func TestToWordSize(t *testing.T) {
	tests := []struct {
		size uint64
		want uint64
	}{
		{0, 0},
		{1, 1},
		{31, 1},
		{32, 1},
		{33, 2},
		{64, 2},
		{65, 3},
	}
	for _, tt := range tests {
		got := toWordSize(tt.size)
		if got != tt.want {
			t.Errorf("toWordSize(%d) = %d, want %d", tt.size, got, tt.want)
		}
	}
}

func TestCallGas(t *testing.T) {
	// 63/64 rule: available gas 6400, requested 10000.
	// maxGas = 6400 - 6400/64 = 6400 - 100 = 6300
	gas := CallGas(6400, 10000)
	if gas != 6300 {
		t.Errorf("CallGas(6400, 10000) = %d, want 6300", gas)
	}

	// Requested less than max.
	gas = CallGas(6400, 5000)
	if gas != 5000 {
		t.Errorf("CallGas(6400, 5000) = %d, want 5000", gas)
	}
}

func TestSstoreGas(t *testing.T) {
	var zero, one, two [32]byte
	one[31] = 1
	two[31] = 2

	// No-op: current == new.
	gas, refund := SstoreGas(zero, zero, zero, false)
	if gas != WarmStorageReadCost {
		t.Errorf("no-op gas = %d, want %d", gas, WarmStorageReadCost)
	}
	if refund != 0 {
		t.Errorf("no-op refund = %d, want 0", refund)
	}

	// Set: 0 -> 1 (original == current == 0).
	gas, refund = SstoreGas(zero, zero, one, false)
	if gas != GasSstoreSet {
		t.Errorf("set gas = %d, want %d", gas, GasSstoreSet)
	}
	if refund != 0 {
		t.Errorf("set refund = %d, want 0", refund)
	}

	// Reset: 1 -> 2 (original == current == 1).
	gas, refund = SstoreGas(one, one, two, false)
	if gas != GasSstoreReset {
		t.Errorf("reset gas = %d, want %d", gas, GasSstoreReset)
	}

	// Clear: 1 -> 0 (original == current == 1).
	gas, refund = SstoreGas(one, one, zero, false)
	if gas != GasSstoreReset {
		t.Errorf("clear gas = %d, want %d", gas, GasSstoreReset)
	}
	if refund <= 0 {
		t.Errorf("clear refund = %d, expected positive", refund)
	}

	// Cold access should add cold cost.
	gas, _ = SstoreGas(zero, zero, one, true)
	if gas != GasSstoreSet+ColdSloadCost {
		t.Errorf("cold set gas = %d, want %d", gas, GasSstoreSet+ColdSloadCost)
	}
}

func TestLogGas(t *testing.T) {
	// LOG0 with 32 bytes of data.
	gas := LogGas(0, 32)
	expected := GasLog + 0*GasLogTopic + 32*GasLogData
	if gas != expected {
		t.Errorf("LOG0(32) gas = %d, want %d", gas, expected)
	}

	// LOG2 with 64 bytes.
	gas = LogGas(2, 64)
	expected = GasLog + 2*GasLogTopic + 64*GasLogData
	if gas != expected {
		t.Errorf("LOG2(64) gas = %d, want %d", gas, expected)
	}
}

func TestSha3Gas(t *testing.T) {
	// 32 bytes = 1 word.
	gas := Sha3Gas(32)
	expected := GasKeccak256 + 1*GasKeccak256Word
	if gas != expected {
		t.Errorf("Sha3Gas(32) = %d, want %d", gas, expected)
	}

	// 0 bytes.
	gas = Sha3Gas(0)
	if gas != GasKeccak256 {
		t.Errorf("Sha3Gas(0) = %d, want %d", gas, GasKeccak256)
	}
}

func TestExpGas(t *testing.T) {
	// Exponent = 0: just base cost.
	gas := ExpGas(big.NewInt(0))
	if gas != GasSlowStep {
		t.Errorf("ExpGas(0) = %d, want %d", gas, GasSlowStep)
	}

	// Exponent = 255 (1 byte).
	gas = ExpGas(big.NewInt(255))
	expected := GasSlowStep + 50*1
	if gas != expected {
		t.Errorf("ExpGas(255) = %d, want %d", gas, expected)
	}

	// Exponent = 256 (2 bytes).
	gas = ExpGas(big.NewInt(256))
	expected = GasSlowStep + 50*2
	if gas != expected {
		t.Errorf("ExpGas(256) = %d, want %d", gas, expected)
	}
}

func TestCopyGas(t *testing.T) {
	gas := CopyGas(32)
	if gas != GasCopy {
		t.Errorf("CopyGas(32) = %d, want %d", gas, GasCopy)
	}

	gas = CopyGas(33)
	if gas != GasCopy*2 {
		t.Errorf("CopyGas(33) = %d, want %d", gas, GasCopy*2)
	}

	gas = CopyGas(0)
	if gas != 0 {
		t.Errorf("CopyGas(0) = %d, want 0", gas)
	}
}

func TestIsZero(t *testing.T) {
	var zero [32]byte
	if !isZero(zero) {
		t.Error("expected zero")
	}

	nonZero := zero
	nonZero[31] = 1
	if isZero(nonZero) {
		t.Error("expected non-zero")
	}
}
