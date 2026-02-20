package vm

import (
	"testing"
)

// TestGasTableBerlinConstants verifies the Berlin gas table returns expected values.
func TestGasTableBerlinConstants(t *testing.T) {
	gt := GasTableBerlin()

	checks := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"WarmStorageRead", gt.WarmStorageRead, 100},
		{"ColdSload", gt.ColdSload, 2100},
		{"ColdAccountAccess", gt.ColdAccountAccess, 2600},
		{"SstoreSet", gt.SstoreSet, 20000},
		{"SstoreReset", gt.SstoreReset, 2900},
		{"SstoreClearsRefund", gt.SstoreClearsRefund, 4800},
		{"CreateBaseCost", gt.CreateBaseCost, 32000},
		{"InitCodeWordCost", gt.InitCodeWordCost, 2},
		{"MaxInitCodeBytes", gt.MaxInitCodeBytes, 49152},
		{"CodeDepositPerByte", gt.CodeDepositPerByte, 200},
		{"CallValueTransfer", gt.CallValueTransfer, 9000},
		{"CallNewAccount", gt.CallNewAccount, 25000},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, c.got, c.want)
		}
	}
}

// TestGasTableGlamsterdamConstants verifies the Glamsterdam gas table overrides.
func TestGasTableGlamsterdamConstants(t *testing.T) {
	gt := GasTableGlamsterdam()

	checks := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"WarmStorageRead", gt.WarmStorageRead, WarmStorageReadGlamst},
		{"ColdSload", gt.ColdSload, ColdSloadGlamst},
		{"ColdAccountAccess", gt.ColdAccountAccess, ColdAccountAccessGlamst},
		{"SstoreSet", gt.SstoreSet, GasSstoreSetGlamsterdam},
		{"SstoreClearsRefund", gt.SstoreClearsRefund, SstoreClearsRefundGlam},
		{"CreateBaseCost", gt.CreateBaseCost, GasCreateGlamsterdam},
		{"CodeDepositPerByte", gt.CodeDepositPerByte, GasCodeDepositGlamsterdam},
		{"CallValueTransfer", gt.CallValueTransfer, CallValueTransferGlamst},
		{"CallNewAccount", gt.CallNewAccount, CallNewAccountGlamst},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, c.got, c.want)
		}
	}
}

// TestSelectGasTableExt verifies fork-based gas table selection.
func TestSelectGasTableExt(t *testing.T) {
	berlinRules := ForkRules{IsBerlin: true}
	gt := SelectGasTableExt(berlinRules)
	if gt.ForkName != "Berlin" {
		t.Fatalf("expected Berlin gas table, got %s", gt.ForkName)
	}

	glamRules := ForkRules{IsGlamsterdan: true, IsCancun: true}
	gt = SelectGasTableExt(glamRules)
	if gt.ForkName != "Glamsterdam" {
		t.Fatalf("expected Glamsterdam gas table, got %s", gt.ForkName)
	}
}

// TestSloadGas verifies SLOAD gas for cold and warm slots.
func TestSloadGas(t *testing.T) {
	gt := GasTableBerlin()

	if gas := gt.SloadGas(true); gas != 2100 {
		t.Fatalf("cold SLOAD: got %d, want 2100", gas)
	}
	if gas := gt.SloadGas(false); gas != 100 {
		t.Fatalf("warm SLOAD: got %d, want 100", gas)
	}

	// Glamsterdam.
	gg := GasTableGlamsterdam()
	if gas := gg.SloadGas(true); gas != ColdSloadGlamst {
		t.Fatalf("Glamsterdam cold SLOAD: got %d, want %d", gas, ColdSloadGlamst)
	}
	if gas := gg.SloadGas(false); gas != WarmStorageReadGlamst {
		t.Fatalf("Glamsterdam warm SLOAD: got %d, want %d", gas, WarmStorageReadGlamst)
	}
}

// TestSstoreGasEIP2200Noop verifies that writing the same value is a no-op.
func TestSstoreGasEIP2200Noop(t *testing.T) {
	gt := GasTableBerlin()
	val := [32]byte{0x01}
	gas, refund := gt.SstoreGasEIP2200(val, val, val, false)

	// current == new -> noop cost = WarmStorageRead.
	if gas != 100 {
		t.Fatalf("noop gas: got %d, want 100", gas)
	}
	if refund != 0 {
		t.Fatalf("noop refund: got %d, want 0", refund)
	}
}

// TestSstoreGasEIP2200CreateSlot verifies gas for creating a new slot (0 -> non-zero).
func TestSstoreGasEIP2200CreateSlot(t *testing.T) {
	gt := GasTableBerlin()
	zero := [32]byte{}
	nonZero := [32]byte{0x42}

	// original=0, current=0, new=nonzero -> SstoreSet cost.
	gas, refund := gt.SstoreGasEIP2200(zero, zero, nonZero, false)
	if gas != 20000 {
		t.Fatalf("create slot gas: got %d, want 20000", gas)
	}
	if refund != 0 {
		t.Fatalf("create slot refund: got %d, want 0", refund)
	}
}

// TestSstoreGasEIP2200ClearSlot verifies gas and refund for clearing a slot.
func TestSstoreGasEIP2200ClearSlot(t *testing.T) {
	gt := GasTableBerlin()
	zero := [32]byte{}
	nonZero := [32]byte{0x42}

	// original=nonzero, current=nonzero, new=zero -> SstoreReset + refund.
	gas, refund := gt.SstoreGasEIP2200(nonZero, nonZero, zero, false)
	if gas != 2900 {
		t.Fatalf("clear slot gas: got %d, want 2900", gas)
	}
	if refund != int64(SstoreClearsScheduleRefund) {
		t.Fatalf("clear slot refund: got %d, want %d", refund, SstoreClearsScheduleRefund)
	}
}

// TestSstoreGasEIP2200ColdAccess verifies the cold surcharge is added.
func TestSstoreGasEIP2200ColdAccess(t *testing.T) {
	gt := GasTableBerlin()
	val := [32]byte{0x01}

	// Same value (noop) but cold: should add ColdSload cost.
	gas, _ := gt.SstoreGasEIP2200(val, val, val, true)
	expected := gt.ColdSload + gt.SstoreNoopCost
	if gas != expected {
		t.Fatalf("cold noop gas: got %d, want %d", gas, expected)
	}
}

// TestSstoreGasEIP2200DirtySlot verifies dirty slot (original != current) handling.
func TestSstoreGasEIP2200DirtySlot(t *testing.T) {
	gt := GasTableBerlin()
	original := [32]byte{0x01}
	current := [32]byte{0x02}
	newVal := [32]byte{0x03}

	// Dirty slot, not restoring original.
	gas, refund := gt.SstoreGasEIP2200(original, current, newVal, false)
	if gas != gt.SstoreNoopCost {
		t.Fatalf("dirty slot gas: got %d, want %d", gas, gt.SstoreNoopCost)
	}
	if refund != 0 {
		t.Fatalf("dirty slot refund: got %d, want 0", refund)
	}
}

// TestSstoreGasEIP2200RestoreOriginal verifies restoring to original value refund.
func TestSstoreGasEIP2200RestoreOriginal(t *testing.T) {
	gt := GasTableBerlin()
	original := [32]byte{0x01}
	current := [32]byte{0x02}

	// Restoring original: dirty slot, newVal == original.
	gas, refund := gt.SstoreGasEIP2200(original, current, original, false)
	if gas != gt.SstoreNoopCost {
		t.Fatalf("restore gas: got %d, want %d", gas, gt.SstoreNoopCost)
	}
	// Should get a refund of SstoreReset - SstoreNoopCost.
	expectedRefund := int64(gt.SstoreReset - gt.SstoreNoopCost)
	if refund != expectedRefund {
		t.Fatalf("restore refund: got %d, want %d", refund, expectedRefund)
	}
}

// TestCreateGasEIP3860 verifies CREATE gas with EIP-3860 initcode charging.
func TestCreateGasEIP3860(t *testing.T) {
	gt := GasTableBerlin()

	// 256 bytes of initcode = 8 words, gas = 32000 + 2*8 = 32016.
	gas, ok := gt.CreateGasEIP3860(256, false)
	if !ok {
		t.Fatal("expected ok for 256 bytes initcode")
	}
	if gas != 32000+2*8 {
		t.Fatalf("create gas: got %d, want %d", gas, 32000+2*8)
	}
}

// TestCreateGasEIP3860Create2 verifies CREATE2 includes keccak word cost.
func TestCreateGasEIP3860Create2(t *testing.T) {
	gt := GasTableBerlin()

	// 64 bytes of initcode = 2 words.
	// gas = 32000 + 2*2 (initcode) + 6*2 (keccak) = 32000 + 4 + 12 = 32016.
	gas, ok := gt.CreateGasEIP3860(64, true)
	if !ok {
		t.Fatal("expected ok for 64 bytes initcode")
	}
	expected := uint64(32000 + 2*2 + 6*2)
	if gas != expected {
		t.Fatalf("create2 gas: got %d, want %d", gas, expected)
	}
}

// TestCreateGasEIP3860ExceedsMax verifies rejection of oversized initcode.
func TestCreateGasEIP3860ExceedsMax(t *testing.T) {
	gt := GasTableBerlin()

	_, ok := gt.CreateGasEIP3860(gt.MaxInitCodeBytes+1, false)
	if ok {
		t.Fatal("expected rejection for oversized initcode")
	}
}

// TestAccountAccessGas verifies cold/warm account access gas.
func TestAccountAccessGas(t *testing.T) {
	gt := GasTableBerlin()

	if gas := gt.AccountAccessGas(true); gas != 2600 {
		t.Fatalf("cold account access: got %d, want 2600", gas)
	}
	if gas := gt.AccountAccessGas(false); gas != 100 {
		t.Fatalf("warm account access: got %d, want 100", gas)
	}
}

// TestGtHelperFunctions verifies the helper functions.
func TestGtHelperFunctions(t *testing.T) {
	// gtIsZero
	if !gtIsZero([32]byte{}) {
		t.Fatal("expected zero")
	}
	if gtIsZero([32]byte{0x01}) {
		t.Fatal("expected non-zero")
	}

	// gtSafeAdd overflow
	result := gtSafeAdd(^uint64(0), 1)
	if result != ^uint64(0) {
		t.Fatalf("expected max uint64, got %d", result)
	}

	// gtSafeMul overflow
	result = gtSafeMul(^uint64(0), 2)
	if result != ^uint64(0) {
		t.Fatalf("expected max uint64, got %d", result)
	}

	// gtToWordSize
	if gtToWordSize(0) != 0 {
		t.Fatal("expected 0 words for 0 bytes")
	}
	if gtToWordSize(1) != 1 {
		t.Fatal("expected 1 word for 1 byte")
	}
	if gtToWordSize(32) != 1 {
		t.Fatal("expected 1 word for 32 bytes")
	}
	if gtToWordSize(33) != 2 {
		t.Fatal("expected 2 words for 33 bytes")
	}
}
