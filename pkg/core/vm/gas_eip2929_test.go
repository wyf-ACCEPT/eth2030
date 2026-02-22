package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// newEIP2929EVM creates an EVM with a MemoryStateDB and pre-warmed access list.
func newEIP2929EVM(sender, to types.Address) (*EVM, *state.MemoryStateDB) {
	stateDB := state.NewMemoryStateDB()
	evm := NewEVMWithState(
		BlockContext{BlockNumber: big.NewInt(1), GasLimit: 30000000},
		TxContext{Origin: sender},
		Config{},
		stateDB,
	)
	evm.PreWarmAccessList(sender, &to)
	return evm, stateDB
}

// TestEIP2929BalanceColdAccess verifies that BALANCE on a cold address costs
// ColdAccountAccessCost (2600) total: WarmStorageReadCost (100) constant + 2500 dynamic.
func TestEIP2929BalanceColdAccess(t *testing.T) {
	sender := types.BytesToAddress([]byte{0xaa})
	to := types.BytesToAddress([]byte{0xbb})
	target := types.BytesToAddress([]byte{0xcc}) // not pre-warmed

	evm, stateDB := newEIP2929EVM(sender, to)
	stateDB.CreateAccount(target)

	gas := uint64(100000)
	contract := NewContract(sender, to, big.NewInt(0), gas)
	// PUSH20 <target>, BALANCE, STOP
	code := []byte{byte(PUSH20)}
	code = append(code, target[:]...)
	code = append(code, byte(BALANCE), byte(STOP))
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Gas consumed = PUSH20 (3) + BALANCE constant (100) + BALANCE dynamic cold (2500) + STOP (0)
	used := gas - contract.Gas
	expectedGas := GasPush + WarmStorageReadCost + (ColdAccountAccessCost - WarmStorageReadCost)
	if used != expectedGas {
		t.Errorf("cold BALANCE gas used = %d, want %d", used, expectedGas)
	}
}

// TestEIP2929BalanceWarmAccess verifies that BALANCE on a warm address costs
// only WarmStorageReadCost (100).
func TestEIP2929BalanceWarmAccess(t *testing.T) {
	sender := types.BytesToAddress([]byte{0xaa})
	to := types.BytesToAddress([]byte{0xbb})
	target := types.BytesToAddress([]byte{0xcc})

	evm, stateDB := newEIP2929EVM(sender, to)
	stateDB.CreateAccount(target)

	// Pre-warm the target address.
	stateDB.AddAddressToAccessList(target)

	gas := uint64(100000)
	contract := NewContract(sender, to, big.NewInt(0), gas)
	code := []byte{byte(PUSH20)}
	code = append(code, target[:]...)
	code = append(code, byte(BALANCE), byte(STOP))
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Gas consumed = PUSH20 (3) + BALANCE constant (100) + BALANCE dynamic warm (0) + STOP (0)
	used := gas - contract.Gas
	expectedGas := GasPush + WarmStorageReadCost
	if used != expectedGas {
		t.Errorf("warm BALANCE gas used = %d, want %d", used, expectedGas)
	}
}

// TestEIP2929BalanceSecondAccessWarm verifies that the second BALANCE to the
// same address is warm (costs 100 gas only).
func TestEIP2929BalanceSecondAccessWarm(t *testing.T) {
	sender := types.BytesToAddress([]byte{0xaa})
	to := types.BytesToAddress([]byte{0xbb})
	target := types.BytesToAddress([]byte{0xcc})

	evm, stateDB := newEIP2929EVM(sender, to)
	stateDB.CreateAccount(target)

	gas := uint64(100000)
	contract := NewContract(sender, to, big.NewInt(0), gas)
	// Two BALANCE calls to the same address:
	// PUSH20 <target>, BALANCE, POP, PUSH20 <target>, BALANCE, STOP
	code := []byte{byte(PUSH20)}
	code = append(code, target[:]...)
	code = append(code, byte(BALANCE), byte(POP))
	code = append(code, byte(PUSH20))
	code = append(code, target[:]...)
	code = append(code, byte(BALANCE), byte(STOP))
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// First BALANCE: PUSH20 (3) + cold BALANCE (100 + 2500) = 2603
	// POP: 2
	// Second BALANCE: PUSH20 (3) + warm BALANCE (100 + 0) = 103
	// STOP: 0
	// Total: 2603 + 2 + 103 = 2708
	used := gas - contract.Gas
	expectedGas := GasPush + ColdAccountAccessCost + GasPop + GasPush + WarmStorageReadCost
	if used != expectedGas {
		t.Errorf("double BALANCE gas used = %d, want %d", used, expectedGas)
	}
}

// TestEIP2929SloadCold verifies that SLOAD on a cold slot costs ColdSloadCost (2100).
func TestEIP2929SloadCold(t *testing.T) {
	sender := types.BytesToAddress([]byte{0xaa})
	to := types.BytesToAddress([]byte{0xbb})

	evm, stateDB := newEIP2929EVM(sender, to)
	stateDB.CreateAccount(to)

	gas := uint64(100000)
	contract := NewContract(sender, to, big.NewInt(0), gas)
	// PUSH1 0x00, SLOAD, STOP
	contract.Code = []byte{
		byte(PUSH1), 0x00,
		byte(SLOAD),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Gas consumed = PUSH1 (3) + SLOAD constant (100) + SLOAD cold dynamic (2000) + STOP (0)
	used := gas - contract.Gas
	expectedGas := GasPush + ColdSloadCost
	if used != expectedGas {
		t.Errorf("cold SLOAD gas used = %d, want %d", used, expectedGas)
	}
}

// TestEIP2929SloadWarm verifies that SLOAD on a warm slot costs WarmStorageReadCost (100).
func TestEIP2929SloadWarm(t *testing.T) {
	sender := types.BytesToAddress([]byte{0xaa})
	to := types.BytesToAddress([]byte{0xbb})
	slot := types.Hash{} // slot 0

	evm, stateDB := newEIP2929EVM(sender, to)
	stateDB.CreateAccount(to)
	// Pre-warm the slot.
	stateDB.AddSlotToAccessList(to, slot)

	gas := uint64(100000)
	contract := NewContract(sender, to, big.NewInt(0), gas)
	// PUSH1 0x00, SLOAD, STOP
	contract.Code = []byte{
		byte(PUSH1), 0x00,
		byte(SLOAD),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Gas consumed = PUSH1 (3) + SLOAD constant (100) + SLOAD warm dynamic (0) + STOP (0)
	used := gas - contract.Gas
	expectedGas := GasPush + WarmStorageReadCost
	if used != expectedGas {
		t.Errorf("warm SLOAD gas used = %d, want %d", used, expectedGas)
	}
}

// TestEIP2929SloadSecondAccessWarm verifies that a second SLOAD to the same
// slot is warm.
func TestEIP2929SloadSecondAccessWarm(t *testing.T) {
	sender := types.BytesToAddress([]byte{0xaa})
	to := types.BytesToAddress([]byte{0xbb})

	evm, stateDB := newEIP2929EVM(sender, to)
	stateDB.CreateAccount(to)

	gas := uint64(100000)
	contract := NewContract(sender, to, big.NewInt(0), gas)
	// Two SLOADs: PUSH1 0x00, SLOAD, POP, PUSH1 0x00, SLOAD, STOP
	contract.Code = []byte{
		byte(PUSH1), 0x00,
		byte(SLOAD),
		byte(POP),
		byte(PUSH1), 0x00,
		byte(SLOAD),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// First SLOAD: PUSH1 (3) + cold SLOAD (100 + 2000) = 2103
	// POP: 2
	// Second SLOAD: PUSH1 (3) + warm SLOAD (100 + 0) = 103
	// Total: 2103 + 2 + 103 = 2208
	used := gas - contract.Gas
	expectedGas := GasPush + ColdSloadCost + GasPop + GasPush + WarmStorageReadCost
	if used != expectedGas {
		t.Errorf("double SLOAD gas used = %d, want %d", used, expectedGas)
	}
}

// TestEIP2929PrecompilesPreWarmed verifies that precompile addresses (0x01-0x0a)
// are pre-warmed via PreWarmAccessList.
func TestEIP2929PrecompilesPreWarmed(t *testing.T) {
	sender := types.BytesToAddress([]byte{0xaa})
	to := types.BytesToAddress([]byte{0xbb})
	_, stateDB := newEIP2929EVM(sender, to)

	for i := 1; i <= 10; i++ {
		addr := types.BytesToAddress([]byte{byte(i)})
		if !stateDB.AddressInAccessList(addr) {
			t.Errorf("precompile 0x%02x not warmed in access list", i)
		}
	}
}

// TestEIP2929SenderAndToPreWarmed verifies that the sender and to address
// are pre-warmed.
func TestEIP2929SenderAndToPreWarmed(t *testing.T) {
	sender := types.BytesToAddress([]byte{0xaa})
	to := types.BytesToAddress([]byte{0xbb})
	_, stateDB := newEIP2929EVM(sender, to)

	if !stateDB.AddressInAccessList(sender) {
		t.Error("sender address not warmed in access list")
	}
	if !stateDB.AddressInAccessList(to) {
		t.Error("to address not warmed in access list")
	}
}

// TestEIP2929ExtCodeSizeCold verifies that EXTCODESIZE on a cold address
// costs ColdAccountAccessCost (2600).
func TestEIP2929ExtCodeSizeCold(t *testing.T) {
	sender := types.BytesToAddress([]byte{0xaa})
	to := types.BytesToAddress([]byte{0xbb})
	target := types.BytesToAddress([]byte{0xdd})

	evm, stateDB := newEIP2929EVM(sender, to)
	stateDB.CreateAccount(target)

	gas := uint64(100000)
	contract := NewContract(sender, to, big.NewInt(0), gas)
	code := []byte{byte(PUSH20)}
	code = append(code, target[:]...)
	code = append(code, byte(EXTCODESIZE), byte(POP), byte(STOP))
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	used := gas - contract.Gas
	expectedGas := GasPush + ColdAccountAccessCost + GasPop
	if used != expectedGas {
		t.Errorf("cold EXTCODESIZE gas used = %d, want %d", used, expectedGas)
	}
}

// TestEIP2929ExtCodeHashCold verifies that EXTCODEHASH on a cold address
// costs ColdAccountAccessCost (2600).
func TestEIP2929ExtCodeHashCold(t *testing.T) {
	sender := types.BytesToAddress([]byte{0xaa})
	to := types.BytesToAddress([]byte{0xbb})
	target := types.BytesToAddress([]byte{0xee})

	evm, stateDB := newEIP2929EVM(sender, to)
	stateDB.CreateAccount(target)

	gas := uint64(100000)
	contract := NewContract(sender, to, big.NewInt(0), gas)
	code := []byte{byte(PUSH20)}
	code = append(code, target[:]...)
	code = append(code, byte(EXTCODEHASH), byte(POP), byte(STOP))
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	used := gas - contract.Gas
	expectedGas := GasPush + ColdAccountAccessCost + GasPop
	if used != expectedGas {
		t.Errorf("cold EXTCODEHASH gas used = %d, want %d", used, expectedGas)
	}
}

// TestEIP2929BalanceOnPreWarmedSender verifies that BALANCE on the pre-warmed
// sender costs only WarmStorageReadCost.
func TestEIP2929BalanceOnPreWarmedSender(t *testing.T) {
	sender := types.BytesToAddress([]byte{0xaa})
	to := types.BytesToAddress([]byte{0xbb})

	evm, stateDB := newEIP2929EVM(sender, to)
	stateDB.CreateAccount(sender)

	gas := uint64(100000)
	contract := NewContract(sender, to, big.NewInt(0), gas)
	code := []byte{byte(PUSH20)}
	code = append(code, sender[:]...)
	code = append(code, byte(BALANCE), byte(STOP))
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	used := gas - contract.Gas
	expectedGas := GasPush + WarmStorageReadCost // sender is pre-warmed
	if used != expectedGas {
		t.Errorf("warm sender BALANCE gas used = %d, want %d", used, expectedGas)
	}
}
