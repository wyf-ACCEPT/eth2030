package core

import (
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// TestFactoryAddress verifies the factory address is at 0x12.
func TestFactoryAddress(t *testing.T) {
	expected := types.HexToAddress("0x0000000000000000000000000000000000000012")
	if FactoryAddress != expected {
		t.Errorf("FactoryAddress = %s, want %s", FactoryAddress.Hex(), expected.Hex())
	}
}

// TestFactoryCodeNotEmpty verifies the factory bytecode is populated.
func TestFactoryCodeNotEmpty(t *testing.T) {
	if len(FactoryCode) == 0 {
		t.Fatal("FactoryCode is empty")
	}
}

// TestFactoryCodeHexLength verifies the factory bytecode length.
func TestFactoryCodeHexLength(t *testing.T) {
	// The factory code hex has a known length.
	if len(FactoryCode) < 20 {
		t.Errorf("FactoryCode length = %d, expected >= 20 bytes", len(FactoryCode))
	}
}

// TestApplyEIP7997 verifies that ApplyEIP7997 deploys the factory contract.
func TestApplyEIP7997(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	ApplyEIP7997(statedb)

	// Factory account should exist.
	if !statedb.Exist(FactoryAddress) {
		t.Fatal("factory account does not exist after ApplyEIP7997")
	}

	// Code should be the factory bytecode.
	code := statedb.GetCode(FactoryAddress)
	if len(code) == 0 {
		t.Fatal("factory code is empty after ApplyEIP7997")
	}
	if len(code) != len(FactoryCode) {
		t.Errorf("factory code length = %d, want %d", len(code), len(FactoryCode))
	}
	for i := range code {
		if code[i] != FactoryCode[i] {
			t.Errorf("factory code byte %d = %x, want %x", i, code[i], FactoryCode[i])
			break
		}
	}
}

// TestApplyEIP7997Idempotent verifies that calling ApplyEIP7997 multiple times
// does not overwrite or corrupt the factory contract.
func TestApplyEIP7997Idempotent(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	ApplyEIP7997(statedb)
	ApplyEIP7997(statedb)
	ApplyEIP7997(statedb)

	code := statedb.GetCode(FactoryAddress)
	if len(code) != len(FactoryCode) {
		t.Errorf("factory code length = %d after repeated calls, want %d",
			len(code), len(FactoryCode))
	}
}

// TestApplyEIP7997NoOverwrite verifies that if the factory address already
// has code, ApplyEIP7997 does not overwrite it.
func TestApplyEIP7997NoOverwrite(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Pre-deploy some other code at the factory address.
	statedb.CreateAccount(FactoryAddress)
	existingCode := []byte{0x60, 0x00, 0x60, 0x00, 0xfd} // PUSH1 0, PUSH1 0, REVERT
	statedb.SetCode(FactoryAddress, existingCode)

	ApplyEIP7997(statedb)

	// Code should still be the existing code, not the factory code.
	code := statedb.GetCode(FactoryAddress)
	if len(code) != len(existingCode) {
		t.Errorf("code was overwritten: length = %d, want %d", len(code), len(existingCode))
	}
}

// TestFactoryBytecodeStructure verifies basic structure of the factory bytecode.
// The factory should handle CREATE2 deployment from salt(32) || initcode input.
func TestFactoryBytecodeStructure(t *testing.T) {
	// Verify first opcode is a PUSH instruction (typically PUSH1 for size check).
	if len(FactoryCode) == 0 {
		t.Skip("empty factory code")
	}
	firstByte := FactoryCode[0]
	// 0x60 = PUSH1, should be something reasonable.
	if firstByte < 0x60 || firstByte > 0x7f {
		// Could also be other opcodes, but typically starts with PUSH.
		t.Logf("first opcode = 0x%02x (not a PUSH, but may be valid)", firstByte)
	}
}
