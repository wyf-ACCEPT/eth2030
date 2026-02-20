package vm

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestStateAccessGasCalculatorNew(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	if calc == nil {
		t.Fatal("NewStateAccessGasCalculator returned nil")
	}
	if calc.AccessedLeafCount() != 0 {
		t.Fatalf("new calc should have 0 accessed leaves, got %d", calc.AccessedLeafCount())
	}
	if calc.AccessedBranchCount() != 0 {
		t.Fatalf("new calc should have 0 accessed branches, got %d", calc.AccessedBranchCount())
	}
}

func TestStateAccessGasLeafAccessCold(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x01})

	gas := calc.LeafAccessGas(addr, 0, 0)
	expected := BranchReadGas + LeafReadGas
	if gas != expected {
		t.Fatalf("cold leaf access: got %d, want %d", gas, expected)
	}
}

func TestStateAccessGasLeafAccessWarm(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x02})

	calc.LeafAccessGas(addr, 0, 0)

	// Second access to same leaf should be free.
	gas := calc.LeafAccessGas(addr, 0, 0)
	if gas != 0 {
		t.Fatalf("warm leaf access: got %d, want 0", gas)
	}
}

func TestStateAccessGasLeafAccessSameBranchDiffLeaf(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x03})

	// First leaf access: branch + leaf.
	calc.LeafAccessGas(addr, 0, 0)

	// Different leaf in same branch: only leaf cost.
	gas := calc.LeafAccessGas(addr, 0, 1)
	if gas != LeafReadGas {
		t.Fatalf("same branch diff leaf: got %d, want %d", gas, LeafReadGas)
	}
}

func TestStateAccessGasLeafAccessDiffBranch(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x04})

	calc.LeafAccessGas(addr, 0, 0)

	// Different branch: full cost.
	gas := calc.LeafAccessGas(addr, 1, 0)
	expected := BranchReadGas + LeafReadGas
	if gas != expected {
		t.Fatalf("different branch: got %d, want %d", gas, expected)
	}
}

func TestStateAccessGasLeafWriteChargeCold(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x05})

	gas := calc.LeafWriteGasCharge(addr, 0, 0, false)
	expected := BranchWriteGas + LeafWriteGas
	if gas != expected {
		t.Fatalf("cold write: got %d, want %d", gas, expected)
	}
}

func TestStateAccessGasLeafWriteChargeWithFill(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x06})

	gas := calc.LeafWriteGasCharge(addr, 0, 0, true)
	expected := BranchWriteGas + LeafWriteGas + LeafFillGas
	if gas != expected {
		t.Fatalf("cold write with fill: got %d, want %d", gas, expected)
	}
}

func TestStateAccessGasLeafWriteWarm(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x07})

	calc.LeafWriteGasCharge(addr, 0, 0, false)

	// Second write to same leaf.
	gas := calc.LeafWriteGasCharge(addr, 0, 0, false)
	if gas != 0 {
		t.Fatalf("warm write: got %d, want 0", gas)
	}
}

func TestStateAccessGasChunkAccessGas(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x08})

	// 62 bytes of code = 2 chunks (0-30 and 31-61).
	gas := calc.ChunkAccessGas(addr, 0, 62, 100)
	if gas == 0 {
		t.Fatal("chunk access should have non-zero gas")
	}

	// First chunk: branch + leaf. Second chunk: leaf only (same branch).
	expected := BranchReadGas + LeafReadGas + LeafReadGas
	if gas != expected {
		t.Fatalf("2 chunk access: got %d, want %d", gas, expected)
	}
}

func TestStateAccessGasChunkAccessGasEmptyCode(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x09})

	gas := calc.ChunkAccessGas(addr, 0, 0, 0)
	if gas != 0 {
		t.Fatalf("empty code chunk access: got %d, want 0", gas)
	}
}

func TestStateAccessGasChunkAccessGasBeyondCode(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x0a})

	gas := calc.ChunkAccessGas(addr, 100, 50, 50)
	if gas != 0 {
		t.Fatalf("beyond code chunk access: got %d, want 0", gas)
	}
}

func TestStateAccessGasChunkAccessGasSingleByte(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x0b})

	// 1 byte of code = 1 chunk.
	gas := calc.ChunkAccessGas(addr, 0, 1, 1)
	expected := BranchReadGas + LeafReadGas
	if gas != expected {
		t.Fatalf("single byte chunk: got %d, want %d", gas, expected)
	}
}

func TestStateAccessGasSloadAccessGas(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x0c})

	gas := calc.SloadAccessGas(addr, 0)
	if gas == 0 {
		t.Fatal("SLOAD cold access should have non-zero gas")
	}

	// Second SLOAD to same slot should be free.
	gas = calc.SloadAccessGas(addr, 0)
	if gas != 0 {
		t.Fatalf("SLOAD warm: got %d, want 0", gas)
	}
}

func TestStateAccessGasSstoreAccessGas(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x0d})

	gas := calc.SstoreAccessGas(addr, 0, true)
	// Access gas (branch + leaf) + write gas (branch write + leaf write + fill).
	expected := BranchReadGas + LeafReadGas + BranchWriteGas + LeafWriteGas + LeafFillGas
	if gas != expected {
		t.Fatalf("SSTORE cold fill: got %d, want %d", gas, expected)
	}
}

func TestStateAccessGasSstoreAccessNoFill(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x0e})

	gas := calc.SstoreAccessGas(addr, 0, false)
	expected := BranchReadGas + LeafReadGas + BranchWriteGas + LeafWriteGas
	if gas != expected {
		t.Fatalf("SSTORE cold no fill: got %d, want %d", gas, expected)
	}
}

func TestStateAccessGasBalanceAccess(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x0f})

	gas := calc.BalanceAccessGas(addr)
	expected := BranchReadGas + LeafReadGas
	if gas != expected {
		t.Fatalf("BALANCE cold: got %d, want %d", gas, expected)
	}

	gas = calc.BalanceAccessGas(addr)
	if gas != 0 {
		t.Fatalf("BALANCE warm: got %d, want 0", gas)
	}
}

func TestStateAccessGasCodeHashAccess(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x10})

	gas := calc.CodeHashAccessGas(addr)
	expected := BranchReadGas + LeafReadGas
	if gas != expected {
		t.Fatalf("EXTCODEHASH cold: got %d, want %d", gas, expected)
	}
}

func TestStateAccessGasCallAccess(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	target := types.BytesToAddress([]byte{0x11})

	gas := calc.CallAccessGas(target)
	expected := BranchReadGas + LeafReadGas
	if gas != expected {
		t.Fatalf("CALL cold: got %d, want %d", gas, expected)
	}
}

func TestStateAccessGasIsLeafWarm(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x12})

	if calc.IsLeafWarm(addr, 0, 0) {
		t.Fatal("leaf should not be warm initially")
	}

	calc.LeafAccessGas(addr, 0, 0)

	if !calc.IsLeafWarm(addr, 0, 0) {
		t.Fatal("leaf should be warm after access")
	}
}

func TestStateAccessGasIsBranchWarm(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x13})

	if calc.IsBranchWarm(addr, 0) {
		t.Fatal("branch should not be warm initially")
	}

	calc.LeafAccessGas(addr, 0, 0)

	if !calc.IsBranchWarm(addr, 0) {
		t.Fatal("branch should be warm after access")
	}
}

func TestStateAccessGasWrittenLeafCount(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr := types.BytesToAddress([]byte{0x14})

	calc.LeafWriteGasCharge(addr, 0, 0, false)
	calc.LeafWriteGasCharge(addr, 0, 1, false)
	calc.LeafWriteGasCharge(addr, 1, 0, false)

	if calc.WrittenLeafCount() != 3 {
		t.Fatalf("written leaf count: got %d, want 3", calc.WrittenLeafCount())
	}
}

func TestWitnessGasChargerBasic(t *testing.T) {
	charger := NewWitnessGasCharger()

	gas := charger.ChargeWitnessGas(100)
	expected := uint64(100) * WitnessGasPerByte
	if gas != expected {
		t.Fatalf("witness gas for 100 bytes: got %d, want %d", gas, expected)
	}

	if charger.TotalBytes() != 100 {
		t.Fatalf("total bytes: got %d, want 100", charger.TotalBytes())
	}
	if charger.TotalGas() != expected {
		t.Fatalf("total gas: got %d, want %d", charger.TotalGas(), expected)
	}
	if charger.ChargeCount() != 1 {
		t.Fatalf("charge count: got %d, want 1", charger.ChargeCount())
	}
}

func TestWitnessGasChargerCap(t *testing.T) {
	charger := NewWitnessGasCharger()

	// Very large witness should be capped.
	gas := charger.ChargeWitnessGas(MaxWitnessGasCharge)
	if gas != MaxWitnessGasCharge {
		t.Fatalf("capped witness gas: got %d, want %d", gas, MaxWitnessGasCharge)
	}
}

func TestWitnessGasChargerMultipleCharges(t *testing.T) {
	charger := NewWitnessGasCharger()

	charger.ChargeWitnessGas(50)
	charger.ChargeWitnessGas(75)
	charger.ChargeWitnessGas(25)

	if charger.TotalBytes() != 150 {
		t.Fatalf("total bytes: got %d, want 150", charger.TotalBytes())
	}
	if charger.ChargeCount() != 3 {
		t.Fatalf("charge count: got %d, want 3", charger.ChargeCount())
	}

	expectedGas := uint64(150) * WitnessGasPerByte
	if charger.TotalGas() != expectedGas {
		t.Fatalf("total gas: got %d, want %d", charger.TotalGas(), expectedGas)
	}
}

func TestWitnessGasChargerZeroBytes(t *testing.T) {
	charger := NewWitnessGasCharger()

	gas := charger.ChargeWitnessGas(0)
	if gas != 0 {
		t.Fatalf("zero bytes gas: got %d, want 0", gas)
	}
}

func TestStateAccessGasAddressIsolation(t *testing.T) {
	calc := NewStateAccessGasCalculator()
	addr1 := types.BytesToAddress([]byte{0x20})
	addr2 := types.BytesToAddress([]byte{0x21})

	gas1 := calc.BalanceAccessGas(addr1)
	gas2 := calc.BalanceAccessGas(addr2)

	if gas1 != gas2 {
		t.Fatalf("different addresses should both be cold: %d vs %d", gas1, gas2)
	}

	expected := BranchReadGas + LeafReadGas
	if gas1 != expected {
		t.Fatalf("cold balance: got %d, want %d", gas1, expected)
	}
}

func TestStateAccessGasConstants(t *testing.T) {
	if LeafReadGas != 200 {
		t.Errorf("LeafReadGas = %d, want 200", LeafReadGas)
	}
	if LeafWriteGas != 500 {
		t.Errorf("LeafWriteGas = %d, want 500", LeafWriteGas)
	}
	if BranchReadGas != 1900 {
		t.Errorf("BranchReadGas = %d, want 1900", BranchReadGas)
	}
	if BranchWriteGas != 3000 {
		t.Errorf("BranchWriteGas = %d, want 3000", BranchWriteGas)
	}
	if LeafFillGas != 6200 {
		t.Errorf("LeafFillGas = %d, want 6200", LeafFillGas)
	}
	if ChunkGasSize != 31 {
		t.Errorf("ChunkGasSize = %d, want 31", ChunkGasSize)
	}
	if WitnessGasPerByte != 12 {
		t.Errorf("WitnessGasPerByte = %d, want 12", WitnessGasPerByte)
	}
}
