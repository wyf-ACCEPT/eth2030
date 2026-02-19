package vm

import (
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

func TestNewAccessEventsGasCalculator(t *testing.T) {
	calc := NewAccessEventsGasCalculator()
	if calc == nil {
		t.Fatal("NewAccessEventsGasCalculator should not return nil")
	}
	if calc.Events == nil {
		t.Fatal("Events should not be nil")
	}
}

func TestAccessEventsGasCalculatorSLoadSStore(t *testing.T) {
	calc := NewAccessEventsGasCalculator()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	slot := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001")

	// Cold SLOAD
	gasRead := calc.SLoadGas(addr, slot, 1000000)
	if gasRead == 0 {
		t.Fatal("cold SLOAD should have non-zero gas")
	}

	// Warm SLOAD (same slot)
	gasWarm := calc.SLoadGas(addr, slot, 1000000)
	if gasWarm != state.WarmStorageReadCost {
		t.Fatalf("warm SLOAD: got %d, want %d", gasWarm, state.WarmStorageReadCost)
	}

	// SSTORE to different slot
	calc2 := NewAccessEventsGasCalculator()
	gasWrite := calc2.SStoreGas(addr, slot, 1000000)
	if gasWrite == 0 {
		t.Fatal("cold SSTORE should have non-zero gas")
	}
	if gasWrite <= gasRead {
		t.Fatalf("SSTORE gas (%d) should be > SLOAD gas (%d)", gasWrite, gasRead)
	}
}

func TestAccessEventsGasCalculatorBalance(t *testing.T) {
	calc := NewAccessEventsGasCalculator()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	gas := calc.BalanceGas(addr, 1000000)
	if gas == 0 {
		t.Fatal("cold BALANCE should have non-zero gas")
	}

	gas = calc.BalanceGas(addr, 1000000)
	if gas != state.WarmStorageReadCost {
		t.Fatalf("warm BALANCE: got %d, want %d", gas, state.WarmStorageReadCost)
	}
}

func TestAccessEventsGasCalculatorExtCodeSize(t *testing.T) {
	calc := NewAccessEventsGasCalculator()
	addr := types.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")

	gas := calc.ExtCodeSizeGas(addr, 1000000)
	if gas == 0 {
		t.Fatal("cold EXTCODESIZE should have non-zero gas")
	}
}

func TestAccessEventsGasCalculatorExtCodeHash(t *testing.T) {
	calc := NewAccessEventsGasCalculator()
	addr := types.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")

	gas := calc.ExtCodeHashGas(addr, 1000000)
	if gas == 0 {
		t.Fatal("cold EXTCODEHASH should have non-zero gas")
	}
}

func TestAccessEventsGasCalculatorCall(t *testing.T) {
	calc := NewAccessEventsGasCalculator()
	caller := types.HexToAddress("0x1111111111111111111111111111111111111111")
	target := types.HexToAddress("0x2222222222222222222222222222222222222222")

	// Without value transfer
	gas := calc.CallGas(caller, target, false, 1000000)
	if gas == 0 {
		t.Fatal("cold CALL should have non-zero gas")
	}

	// With value transfer (fresh calculator)
	calc2 := NewAccessEventsGasCalculator()
	gasTransfer := calc2.CallGas(caller, target, true, 1000000)
	if gasTransfer == 0 {
		t.Fatal("CALL with value should have non-zero gas")
	}
	if gasTransfer <= gas {
		t.Fatalf("CALL with value (%d) should cost more than without (%d)", gasTransfer, gas)
	}
}

func TestAccessEventsGasCalculatorSelfDestruct(t *testing.T) {
	calc := NewAccessEventsGasCalculator()
	contract := types.HexToAddress("0x1111111111111111111111111111111111111111")
	beneficiary := types.HexToAddress("0x2222222222222222222222222222222222222222")

	gas := calc.SelfDestructGas(contract, beneficiary, 1000000)
	if gas == 0 {
		t.Fatal("cold SELFDESTRUCT should have non-zero gas")
	}
}

func TestAccessEventsGasCalculatorTxPrewarm(t *testing.T) {
	calc := NewAccessEventsGasCalculator()
	origin := types.HexToAddress("0x1111111111111111111111111111111111111111")
	dest := types.HexToAddress("0x2222222222222222222222222222222222222222")

	calc.AddTxOrigin(origin)
	calc.AddTxDestination(dest, true, false)

	// Both should now be warm
	gas := calc.BalanceGas(origin, 1000000)
	if gas != state.WarmStorageReadCost {
		t.Fatalf("origin should be warm: got %d, want %d", gas, state.WarmStorageReadCost)
	}
}

func TestAccessEventsGasCalculatorMerge(t *testing.T) {
	calc1 := NewAccessEventsGasCalculator()
	calc2 := NewAccessEventsGasCalculator()

	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	calc1.AddTxOrigin(addr1)
	calc2.AddTxOrigin(addr2)

	calc1.Merge(calc2)

	gas1 := calc1.BalanceGas(addr1, 1000000)
	gas2 := calc1.BalanceGas(addr2, 1000000)

	if gas1 != state.WarmStorageReadCost || gas2 != state.WarmStorageReadCost {
		t.Fatalf("both should be warm after merge: gas1=%d, gas2=%d", gas1, gas2)
	}
}

func TestAccessEventsGasCalculatorCopy(t *testing.T) {
	calc := NewAccessEventsGasCalculator()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	calc.AddTxOrigin(addr)

	cp := calc.Copy()

	// Copy should have warm addresses
	gas := cp.BalanceGas(addr, 1000000)
	if gas != state.WarmStorageReadCost {
		t.Fatalf("copy should have warm addresses: got %d", gas)
	}

	// Modifying copy should not affect original
	addr2 := types.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	cp.AddTxOrigin(addr2)

	gas = calc.BalanceGas(addr2, 1000000)
	if gas == state.WarmStorageReadCost {
		t.Fatal("modifying copy should not affect original")
	}
}
