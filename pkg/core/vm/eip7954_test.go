package vm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// TestMaxCodeSizeConstants verifies the EIP-7954 constants.
func TestMaxCodeSizeConstants(t *testing.T) {
	if MaxCodeSizeGlamsterdam != 32768 {
		t.Errorf("MaxCodeSizeGlamsterdam = %d, want 32768", MaxCodeSizeGlamsterdam)
	}
	if MaxInitCodeSizeGlamsterdam != 65536 {
		t.Errorf("MaxInitCodeSizeGlamsterdam = %d, want 65536", MaxInitCodeSizeGlamsterdam)
	}
	// Relationship: init code limit = 2 * code size limit.
	if MaxInitCodeSizeGlamsterdam != 2*MaxCodeSizeGlamsterdam {
		t.Error("MaxInitCodeSizeGlamsterdam should be 2 * MaxCodeSizeGlamsterdam")
	}
}

// TestMaxCodeSizeForFork verifies the fork-gated code size selection.
func TestMaxCodeSizeForFork(t *testing.T) {
	// Pre-Glamsterdam: original limits.
	pre := ForkRules{IsEIP7954: false}
	if got := MaxCodeSizeForFork(pre); got != MaxCodeSize {
		t.Errorf("pre-Glamsterdam MaxCodeSize = %d, want %d", got, MaxCodeSize)
	}
	if got := MaxInitCodeSizeForFork(pre); got != MaxInitCodeSize {
		t.Errorf("pre-Glamsterdam MaxInitCodeSize = %d, want %d", got, MaxInitCodeSize)
	}

	// Post-Glamsterdam: increased limits.
	post := ForkRules{IsEIP7954: true}
	if got := MaxCodeSizeForFork(post); got != MaxCodeSizeGlamsterdam {
		t.Errorf("post-Glamsterdam MaxCodeSize = %d, want %d", got, MaxCodeSizeGlamsterdam)
	}
	if got := MaxInitCodeSizeForFork(post); got != MaxInitCodeSizeGlamsterdam {
		t.Errorf("post-Glamsterdam MaxInitCodeSize = %d, want %d", got, MaxInitCodeSizeGlamsterdam)
	}
}

// TestEIP7954CreateLargerCodeAllowed verifies that post-Glamsterdam, contracts
// with code size between 24KB and 32KB can be deployed.
func TestEIP7954CreateLargerCodeAllowed(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0x1111111111111111111111111111111111111111")

	statedb.CreateAccount(caller)
	statedb.AddBalance(caller, big.NewInt(1e18))
	statedb.SetNonce(caller, 1)

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = statedb
	evm.forkRules.IsEIP7954 = true
	evm.SetJumpTable(NewCancunJumpTable())

	// Create init code that returns 28000 bytes of code (> 24KB old limit,
	// < 32KB new limit).
	codeSize := 28000
	code := make([]byte, codeSize)
	for i := range code {
		code[i] = byte(STOP)
	}

	// Build init code: PUSH2 codeSize, PUSH1 offset, PUSH1 0, CODECOPY, PUSH2 codeSize, PUSH1 0, RETURN
	// The init code stores itself + the deployed code, then returns the deployed code.
	// Simpler approach: just use RETURN with data from memory (pre-filled with zeros = STOP).
	initCode := buildInitCodeForSize(codeSize)

	_, addr, _, err := evm.Create(caller, initCode, 10000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("Create with %d byte code failed: %v", codeSize, err)
	}

	deployed := statedb.GetCode(addr)
	if len(deployed) != codeSize {
		t.Errorf("deployed code size = %d, want %d", len(deployed), codeSize)
	}
}

// TestEIP7954CreateExceedsNewLimit verifies that even post-Glamsterdam,
// contracts exceeding 32KB are rejected.
func TestEIP7954CreateExceedsNewLimit(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0x1111111111111111111111111111111111111111")

	statedb.CreateAccount(caller)
	statedb.AddBalance(caller, big.NewInt(1e18))
	statedb.SetNonce(caller, 1)

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = statedb
	evm.forkRules.IsEIP7954 = true
	evm.SetJumpTable(NewCancunJumpTable())

	// Try to deploy 33000 bytes of code (> 32KB new limit).
	codeSize := 33000
	initCode := buildInitCodeForSize(codeSize)

	_, _, _, err := evm.Create(caller, initCode, 10000000, big.NewInt(0))
	if err == nil {
		t.Error("expected error for code exceeding 32KB, got nil")
	}
}

// TestEIP7954PreGlamsterdamRejectsLargeCode verifies that pre-Glamsterdam,
// code > 24KB is still rejected.
func TestEIP7954PreGlamsterdamRejectsLargeCode(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0x1111111111111111111111111111111111111111")

	statedb.CreateAccount(caller)
	statedb.AddBalance(caller, big.NewInt(1e18))
	statedb.SetNonce(caller, 1)

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = statedb
	// EIP-7954 NOT active.
	evm.SetJumpTable(NewCancunJumpTable())

	// Try to deploy 25000 bytes of code (> 24KB old limit).
	codeSize := 25000
	initCode := buildInitCodeForSize(codeSize)

	_, _, _, err := evm.Create(caller, initCode, 10000000, big.NewInt(0))
	if err == nil {
		t.Error("expected error for code exceeding 24KB pre-Glamsterdam, got nil")
	}
}

// TestEIP7954InitCodeSizeLimit verifies the init code size limit is enforced.
func TestEIP7954InitCodeSizeLimit(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0x1111111111111111111111111111111111111111")

	statedb.CreateAccount(caller)
	statedb.AddBalance(caller, big.NewInt(1e18))
	statedb.SetNonce(caller, 1)

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = statedb
	evm.forkRules.IsEIP7954 = true
	evm.SetJumpTable(NewCancunJumpTable())

	// Init code exactly at the limit (65536 bytes) should succeed.
	initCode := make([]byte, MaxInitCodeSizeGlamsterdam)
	initCode[0] = byte(STOP) // Just stop immediately (empty contract).

	_, _, _, err := evm.Create(caller, initCode, 10000000, big.NewInt(0))
	if err != nil {
		t.Errorf("Create with init code at limit (%d bytes) failed: %v",
			MaxInitCodeSizeGlamsterdam, err)
	}

	// Init code exceeding the limit should fail.
	statedb.SetNonce(caller, 2)
	bigInitCode := make([]byte, MaxInitCodeSizeGlamsterdam+1)
	bigInitCode[0] = byte(STOP)

	_, _, _, err = evm.Create(caller, bigInitCode, 10000000, big.NewInt(0))
	if err == nil {
		t.Error("expected error for init code exceeding Glamsterdam limit")
	}
}

// buildInitCodeForSize creates EVM bytecode that, when executed as init code,
// returns `size` bytes of 0x00 (STOP opcodes) as the deployed code.
func buildInitCodeForSize(size int) []byte {
	// The deployed code is `size` bytes of zeros.
	// Init code copies it from memory (which is zero-initialized) and returns it.
	//
	// PUSH2 size, PUSH1 0, RETURN
	//
	// But we need the deployed code size in memory. Since EVM memory is zero-init,
	// we can just RETURN the right amount from offset 0.
	var initCode []byte
	// PUSH2 <size>
	initCode = append(initCode, byte(PUSH2))
	initCode = append(initCode, byte(size>>8), byte(size))
	// PUSH1 0 (memory offset)
	initCode = append(initCode, byte(PUSH1), 0x00)
	// RETURN
	initCode = append(initCode, byte(RETURN))

	// But the deployed code needs to be actual bytes. The memory will be zero,
	// so RETURN(0, size) returns `size` zero bytes = `size` STOP opcodes.
	// This works perfectly for our test.

	// However, we also need to ensure the memory is expanded. RETURN with
	// uninitialized memory will auto-expand (charging gas), and return zeros.
	// This is fine.

	// But let's append the actual code bytes after the init code so CODECOPY could
	// use them. Actually the simpler approach (RETURN from zero memory) works.
	// Let's verify by just appending enough padding to meet the init code's own
	// expectations. The init code itself is just 6 bytes, returning `size` bytes
	// from memory offset 0 (which will be zeros).

	return initCode
}

// TestBuildInitCodeForSize verifies the helper.
func TestBuildInitCodeForSize(t *testing.T) {
	code := buildInitCodeForSize(100)
	// Should be: PUSH2 0x00 0x64, PUSH1 0x00, RETURN = 6 bytes
	if len(code) != 6 {
		t.Fatalf("initCode length = %d, want 6", len(code))
	}
	if code[0] != byte(PUSH2) {
		t.Errorf("byte 0 = %x, want PUSH2", code[0])
	}
	if code[3] != byte(PUSH1) {
		t.Errorf("byte 3 = %x, want PUSH1", code[3])
	}
	if code[5] != byte(RETURN) {
		t.Errorf("byte 5 = %x, want RETURN", code[5])
	}
}

// TestEIP7954LegacyMaxConstants verifies the original constants are unchanged.
func TestEIP7954LegacyMaxConstants(t *testing.T) {
	if MaxCodeSize != 24576 {
		t.Errorf("MaxCodeSize = %d, want 24576", MaxCodeSize)
	}
	if MaxInitCodeSize != 49152 {
		t.Errorf("MaxInitCodeSize = %d, want 49152", MaxInitCodeSize)
	}
}

// TestEIP7954ExactOldLimitAllowedPreFork verifies that a contract exactly at
// the old limit (24576 bytes) deploys successfully pre-Glamsterdam.
func TestEIP7954ExactOldLimitAllowedPreFork(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0x1111111111111111111111111111111111111111")

	statedb.CreateAccount(caller)
	statedb.AddBalance(caller, big.NewInt(1e18))
	statedb.SetNonce(caller, 1)

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = statedb
	evm.SetJumpTable(NewCancunJumpTable())

	codeSize := MaxCodeSize // 24576 bytes -- exactly at limit.
	initCode := buildInitCodeForSize(codeSize)

	_, addr, _, err := evm.Create(caller, initCode, 10000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("Create at exact old limit failed: %v", err)
	}

	deployed := statedb.GetCode(addr)
	if len(deployed) != codeSize {
		t.Errorf("deployed code size = %d, want %d", len(deployed), codeSize)
	}

	// Verify the deployed code is all zeros (STOP opcodes).
	expected := make([]byte, codeSize)
	if !bytes.Equal(deployed, expected) {
		t.Error("deployed code is not all zeros")
	}
}
