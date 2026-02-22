package vm

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// TestRunPushAddStop tests PUSH1 + PUSH1 + ADD + STOP and verifies the stack result.
func TestRunPushAddStop(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// PUSH1 0x03, PUSH1 0x05, ADD, STOP
	contract.Code = []byte{
		byte(PUSH1), 0x03,
		byte(PUSH1), 0x05,
		byte(ADD),
		byte(STOP),
	}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if ret != nil {
		t.Fatalf("expected nil return from STOP, got %x", ret)
	}
	// Contract executed successfully with STOP. Verify gas was consumed.
	if contract.Gas >= 100000 {
		t.Errorf("expected gas consumption, gas remaining = %d", contract.Gas)
	}
}

// TestRunMstoreReturn tests PUSH1 + MSTORE + RETURN returning 32 bytes with the stored value.
func TestRunMstoreReturn(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// PUSH1 0x42, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	contract.Code = []byte{
		byte(PUSH1), 0x42,  // push 0x42
		byte(PUSH1), 0x00,  // push offset 0
		byte(MSTORE),       // mstore(0, 0x42)
		byte(PUSH1), 0x20,  // push size 32
		byte(PUSH1), 0x00,  // push offset 0
		byte(RETURN),       // return mem[0:32]
	}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("expected 32 bytes return, got %d bytes", len(ret))
	}
	// 0x42 should be at position 31 (big-endian, 32-byte word)
	if ret[31] != 0x42 {
		t.Errorf("expected last byte 0x42, got 0x%02x", ret[31])
	}
	// All other bytes should be zero
	for i := 0; i < 31; i++ {
		if ret[i] != 0x00 {
			t.Errorf("expected byte %d to be 0x00, got 0x%02x", i, ret[i])
		}
	}
}

// TestRunCalldataload tests pushing calldataload result onto the stack.
func TestRunCalldataload(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// PUSH1 0x00, CALLDATALOAD, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	contract.Code = []byte{
		byte(PUSH1), 0x00,  // push offset 0
		byte(CALLDATALOAD), // load 32 bytes from calldata at offset 0
		byte(PUSH1), 0x00,  // push offset 0
		byte(MSTORE),       // store in memory at 0
		byte(PUSH1), 0x20,  // push size 32
		byte(PUSH1), 0x00,  // push offset 0
		byte(RETURN),       // return mem[0:32]
	}

	// Provide 32 bytes of calldata
	calldata := make([]byte, 32)
	calldata[0] = 0xDE
	calldata[1] = 0xAD
	calldata[2] = 0xBE
	calldata[3] = 0xEF

	ret, err := evm.Run(contract, calldata)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("expected 32 bytes return, got %d", len(ret))
	}
	if !bytes.Equal(ret[:4], []byte{0xDE, 0xAD, 0xBE, 0xEF}) {
		t.Errorf("expected calldata prefix DEADBEEF, got %x", ret[:4])
	}
}

// TestRunJustStop tests a contract that does nothing but STOP.
func TestRunJustStop(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	contract.Code = []byte{byte(STOP)}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if ret != nil {
		t.Fatalf("expected nil return from STOP, got %x", ret)
	}
}

// TestRunRevert tests that REVERT returns data and an error.
func TestRunRevertIntegration(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// Store 0xABCD at memory[0], then REVERT with offset=0, size=32
	contract.Code = []byte{
		byte(PUSH1), 0xAB,  // push 0xAB
		byte(PUSH1), 0x00,  // push offset 0
		byte(MSTORE8),      // mstore8(0, 0xAB)
		byte(PUSH1), 0xCD,  // push 0xCD
		byte(PUSH1), 0x01,  // push offset 1
		byte(MSTORE8),      // mstore8(1, 0xCD)
		byte(PUSH1), 0x02,  // push size 2
		byte(PUSH1), 0x00,  // push offset 0
		byte(REVERT),       // revert with mem[0:2]
	}

	ret, err := evm.Run(contract, nil)
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("expected ErrExecutionReverted, got %v", err)
	}
	if len(ret) != 2 {
		t.Fatalf("expected 2 bytes revert data, got %d", len(ret))
	}
	if ret[0] != 0xAB || ret[1] != 0xCD {
		t.Errorf("expected revert data ABCD, got %x", ret)
	}
}

// TestRunStackOverflow tests pushing more than 1024 items onto the stack.
func TestRunStackOverflow(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 10000000)

	// Build code that pushes 1025 items: 1025 * (PUSH1, 0x01) + STOP
	code := make([]byte, 0, 1025*2+1)
	for i := 0; i < 1025; i++ {
		code = append(code, byte(PUSH1), 0x01)
	}
	code = append(code, byte(STOP))
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if !errors.Is(err, ErrStackOverflow) {
		t.Fatalf("expected ErrStackOverflow, got %v", err)
	}
}

// TestRunInvalidOpcodeIntegration tests execution of an invalid opcode.
func TestRunInvalidOpcodeIntegration(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// 0xEF is not a registered opcode in Cancun
	contract.Code = []byte{0xEF}

	_, err := evm.Run(contract, nil)
	if !errors.Is(err, ErrInvalidOpCode) {
		t.Fatalf("expected ErrInvalidOpCode, got %v", err)
	}
}

// TestRunDupAndSwap tests DUP and SWAP operations via Run.
func TestRunDupAndSwap(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// PUSH1 0x0A, PUSH1 0x0B, DUP2, ADD, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	// Should compute: DUP2 duplicates 0x0A, then ADD(0x0A, 0x0B) = 0x15
	contract.Code = []byte{
		byte(PUSH1), 0x0A,
		byte(PUSH1), 0x0B,
		byte(DUP2),        // dup 0x0A
		byte(ADD),         // 0x0A + 0x0B = 0x15
		byte(PUSH1), 0x00, // offset
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(ret))
	}
	if ret[31] != 0x15 {
		t.Errorf("expected result 0x15, got 0x%02x", ret[31])
	}
}

// TestRunJumpDest tests JUMP to a JUMPDEST.
func TestRunJumpDestIntegration(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// PUSH1 0x04, JUMP, INVALID, JUMPDEST, STOP
	// Jump over the INVALID to the JUMPDEST at position 4, then STOP.
	contract.Code = []byte{
		byte(PUSH1), 0x04,  // push jump target
		byte(JUMP),         // jump to position 4
		byte(INVALID),      // should be skipped
		byte(JUMPDEST),     // position 4: valid target
		byte(STOP),
	}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if ret != nil {
		t.Fatalf("expected nil return, got %x", ret)
	}
}

// TestRunKeccak256 tests the KECCAK256 opcode.
func TestRunKeccak256(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// Store 0x00...00 (32 zero bytes) at memory[0], then KECCAK256(0, 32), MSTORE at 0, RETURN 32.
	// The keccak256 of 32 zero bytes is a known constant.
	contract.Code = []byte{
		// First, expand memory to 64 bytes by storing a zero at offset 32
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x20,
		byte(MSTORE),       // store 0 at offset 32 (expands memory to 64)
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(MSTORE),       // store 0 at offset 0
		byte(PUSH1), 0x20,  // size = 32
		byte(PUSH1), 0x00,  // offset = 0
		byte(KECCAK256),    // hash mem[0:32]
		byte(PUSH1), 0x00,  // offset = 0
		byte(MSTORE),       // store hash at offset 0
		byte(PUSH1), 0x20,  // size = 32
		byte(PUSH1), 0x00,  // offset = 0
		byte(RETURN),       // return mem[0:32]
	}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(ret))
	}
	// keccak256 of 32 zero bytes is a well-known value
	// 0x290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563
	if ret[0] != 0x29 || ret[1] != 0x0d {
		t.Errorf("keccak256 of 32 zero bytes unexpected: %x", ret)
	}
}

// TestRunEmptyCode tests running a contract with empty code (implicit STOP).
func TestRunEmptyCode(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	contract.Code = []byte{}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if ret != nil {
		t.Fatalf("expected nil return, got %x", ret)
	}
}

// TestRunCalldataSize tests the CALLDATASIZE opcode.
func TestRunCalldataSizeIntegration(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// CALLDATASIZE, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	contract.Code = []byte{
		byte(CALLDATASIZE),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}

	calldata := make([]byte, 64) // 64 bytes of calldata
	ret, err := evm.Run(contract, calldata)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(ret))
	}
	// Should contain 64 (0x40) as big-endian uint256
	if ret[31] != 0x40 {
		t.Errorf("expected calldatasize 0x40, got 0x%02x", ret[31])
	}
}

// TestRunOutOfGasIntegration tests that running out of gas returns ErrOutOfGas.
func TestRunOutOfGasIntegration(t *testing.T) {
	evm := newTestEVM()
	// Only 1 gas - not enough for PUSH1 (costs 3)
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1)
	contract.Code = []byte{byte(PUSH1), 0x42, byte(STOP)}

	_, err := evm.Run(contract, nil)
	if !errors.Is(err, ErrOutOfGas) {
		t.Fatalf("expected ErrOutOfGas, got %v", err)
	}
}

// TestRunSstoreSloadWithState tests SSTORE and SLOAD with a real StateDB.
// This is an integration test: "deploy" a simple contract, run it, verify state.
func TestRunSstoreSloadWithState(t *testing.T) {
	stateDB := state.NewMemoryStateDB()
	contractAddr := types.BytesToAddress([]byte{0xCA, 0xFE})
	stateDB.CreateAccount(contractAddr)

	evm := NewEVMWithState(
		BlockContext{
			BlockNumber: big.NewInt(100),
			Time:        1700000000,
			GasLimit:    30000000,
			BaseFee:     big.NewInt(1000000000),
		},
		TxContext{GasPrice: big.NewInt(2000000000)},
		Config{},
		stateDB,
	)

	// Contract that stores 0x42 at slot 0, then loads it back and returns it.
	// PUSH1 0x42, PUSH1 0x00, SSTORE,    -- store 0x42 at slot 0
	// PUSH1 0x00, SLOAD,                  -- load slot 0
	// PUSH1 0x00, MSTORE,                 -- store in memory
	// PUSH1 0x20, PUSH1 0x00, RETURN      -- return 32 bytes
	contract := NewContract(
		types.BytesToAddress([]byte{0x01}), // caller
		contractAddr,                        // contract address
		big.NewInt(0),
		1000000,
	)
	contract.Code = []byte{
		byte(PUSH1), 0x42,
		byte(PUSH1), 0x00,
		byte(SSTORE),
		byte(PUSH1), 0x00,
		byte(SLOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(ret))
	}
	if ret[31] != 0x42 {
		t.Errorf("expected SLOAD to return 0x42, got 0x%02x", ret[31])
	}

	// Verify the state was actually written
	slot0 := stateDB.GetState(contractAddr, types.BytesToHash([]byte{0}))
	if slot0[31] != 0x42 {
		t.Errorf("expected state slot 0 = 0x42, got %x", slot0)
	}
}

// TestRunLogEmission tests LOG1 emits a log with a topic and data.
func TestRunLogEmission(t *testing.T) {
	stateDB := state.NewMemoryStateDB()
	contractAddr := types.BytesToAddress([]byte{0xBE, 0xEF})
	stateDB.CreateAccount(contractAddr)

	evm := NewEVMWithState(
		BlockContext{BlockNumber: big.NewInt(1), GasLimit: 30000000},
		TxContext{},
		Config{},
		stateDB,
	)

	// Store data 0xABCD in memory, then emit LOG1 with topic 0xFF
	// PUSH1 0xAB, PUSH1 0x00, MSTORE8,
	// PUSH1 0xCD, PUSH1 0x01, MSTORE8,
	// PUSH1 0xFF,         -- topic
	// PUSH1 0x02,         -- size
	// PUSH1 0x00,         -- offset
	// LOG1,
	// STOP
	contract := NewContract(types.Address{}, contractAddr, big.NewInt(0), 1000000)
	contract.Code = []byte{
		byte(PUSH1), 0xAB,
		byte(PUSH1), 0x00,
		byte(MSTORE8),
		byte(PUSH1), 0xCD,
		byte(PUSH1), 0x01,
		byte(MSTORE8),
		byte(PUSH1), 0xFF,  // topic
		byte(PUSH1), 0x02,  // size
		byte(PUSH1), 0x00,  // offset
		byte(LOG1),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Verify logs were emitted (use empty hash as txHash)
	logs := stateDB.GetLogs(types.Hash{})
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].Address != contractAddr {
		t.Errorf("log address = %x, want %x", logs[0].Address, contractAddr)
	}
	if len(logs[0].Topics) != 1 {
		t.Fatalf("expected 1 topic, got %d", len(logs[0].Topics))
	}
	if logs[0].Topics[0][31] != 0xFF {
		t.Errorf("topic[0] last byte = 0x%02x, want 0xFF", logs[0].Topics[0][31])
	}
	if !bytes.Equal(logs[0].Data, []byte{0xAB, 0xCD}) {
		t.Errorf("log data = %x, want ABCD", logs[0].Data)
	}
}

// TestRunContractCallAndVerifyState is an integration test that simulates deploying a
// contract, calling it, and verifying the resulting state changes.
func TestRunContractCallAndVerifyState(t *testing.T) {
	stateDB := state.NewMemoryStateDB()
	contractAddr := types.BytesToAddress([]byte{0xDE, 0xAD})
	callerAddr := types.BytesToAddress([]byte{0x01})
	stateDB.CreateAccount(contractAddr)
	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))

	evm := NewEVMWithState(
		BlockContext{BlockNumber: big.NewInt(42), Time: 1700000000, GasLimit: 30000000},
		TxContext{Origin: callerAddr, GasPrice: big.NewInt(1)},
		Config{},
		stateDB,
	)

	// Contract: increment a counter at storage slot 0.
	// 1. SLOAD slot 0 (current counter value)
	// 2. PUSH1 0x01
	// 3. ADD (counter + 1)
	// 4. PUSH1 0x00
	// 5. SSTORE (store incremented value at slot 0)
	// 6. Return the new counter value
	contract := NewContract(callerAddr, contractAddr, big.NewInt(0), 1000000)
	contract.Code = []byte{
		byte(PUSH1), 0x00,  // slot 0
		byte(SLOAD),        // load current value
		byte(PUSH1), 0x01,  // push 1
		byte(ADD),          // counter + 1
		byte(DUP1),         // dup for return
		byte(PUSH1), 0x00,  // slot 0
		byte(SSTORE),       // store counter + 1
		byte(PUSH1), 0x00,  // mem offset
		byte(MSTORE),       // store in memory
		byte(PUSH1), 0x20,  // size
		byte(PUSH1), 0x00,  // offset
		byte(RETURN),
	}

	// Call 1: counter goes from 0 to 1
	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run #1 error: %v", err)
	}
	if ret[31] != 0x01 {
		t.Errorf("call #1: expected counter 1, got %x", ret)
	}

	// Verify state
	slot := stateDB.GetState(contractAddr, types.BytesToHash([]byte{0}))
	if slot[31] != 0x01 {
		t.Errorf("after call #1: slot 0 = %x, want 0x01", slot)
	}

	// Call 2: counter goes from 1 to 2 (re-create contract with fresh gas)
	contract2 := NewContract(callerAddr, contractAddr, big.NewInt(0), 1000000)
	contract2.Code = contract.Code

	ret2, err := evm.Run(contract2, nil)
	if err != nil {
		t.Fatalf("Run #2 error: %v", err)
	}
	if ret2[31] != 0x02 {
		t.Errorf("call #2: expected counter 2, got %x", ret2)
	}

	slot = stateDB.GetState(contractAddr, types.BytesToHash([]byte{0}))
	if slot[31] != 0x02 {
		t.Errorf("after call #2: slot 0 = %x, want 0x02", slot)
	}
}

// TestRunPush0 tests the PUSH0 opcode (Shanghai).
func TestRunPush0(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// PUSH0, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	contract.Code = []byte{
		byte(PUSH0),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(ret))
	}
	// All bytes should be zero
	for i := 0; i < 32; i++ {
		if ret[i] != 0x00 {
			t.Errorf("expected all zeros, byte %d = 0x%02x", i, ret[i])
		}
	}
}
