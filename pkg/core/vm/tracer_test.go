package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

func TestStructLogTracer_CaptureState(t *testing.T) {
	tracer := NewStructLogTracer()

	stack := NewStack()
	stack.Push(big.NewInt(42))
	stack.Push(big.NewInt(99))
	mem := NewMemory()

	tracer.CaptureState(0, PUSH1, 1000, 3, stack, mem, 1, nil)

	if len(tracer.Logs) != 1 {
		t.Fatalf("want 1 log, got %d", len(tracer.Logs))
	}

	entry := tracer.Logs[0]
	if entry.Pc != 0 {
		t.Fatalf("want pc 0, got %d", entry.Pc)
	}
	if entry.Op != PUSH1 {
		t.Fatalf("want op PUSH1, got %v", entry.Op)
	}
	if entry.Gas != 1000 {
		t.Fatalf("want gas 1000, got %d", entry.Gas)
	}
	if entry.GasCost != 3 {
		t.Fatalf("want gasCost 3, got %d", entry.GasCost)
	}
	if entry.Depth != 1 {
		t.Fatalf("want depth 1, got %d", entry.Depth)
	}
	if len(entry.Stack) != 2 {
		t.Fatalf("want 2 stack items, got %d", len(entry.Stack))
	}
	if entry.Stack[0].Int64() != 42 {
		t.Fatalf("want stack[0] = 42, got %d", entry.Stack[0].Int64())
	}
	if entry.Stack[1].Int64() != 99 {
		t.Fatalf("want stack[1] = 99, got %d", entry.Stack[1].Int64())
	}
}

func TestStructLogTracer_StackCopied(t *testing.T) {
	// Verify that captured stack entries are deep copies, not references.
	tracer := NewStructLogTracer()
	stack := NewStack()
	val := big.NewInt(100)
	stack.Push(val)
	mem := NewMemory()

	tracer.CaptureState(0, ADD, 500, 3, stack, mem, 1, nil)

	// Mutate the original value.
	val.SetInt64(999)

	// The captured entry should still have the original value.
	if tracer.Logs[0].Stack[0].Int64() != 100 {
		t.Fatalf("captured stack value should be independent copy, got %d", tracer.Logs[0].Stack[0].Int64())
	}
}

func TestStructLogTracer_CaptureEnd(t *testing.T) {
	tracer := NewStructLogTracer()
	output := []byte{0xde, 0xad}

	tracer.CaptureEnd(output, 21000, nil)

	if tracer.GasUsed() != 21000 {
		t.Fatalf("want gasUsed 21000, got %d", tracer.GasUsed())
	}
	if tracer.Error() != nil {
		t.Fatalf("want nil error, got %v", tracer.Error())
	}
	if len(tracer.Output()) != 2 || tracer.Output()[0] != 0xde {
		t.Fatalf("want output [0xde 0xad], got %x", tracer.Output())
	}
}

func TestTracingEVM_SimpleCode(t *testing.T) {
	// Bytecode: PUSH1 0x42 PUSH1 0x00 MSTORE PUSH1 0x20 PUSH1 0x00 RETURN
	// This stores 0x42 at memory[0] and returns 32 bytes.
	code := []byte{
		byte(PUSH1), 0x42, // PUSH1 0x42
		byte(PUSH1), 0x00, // PUSH1 0x00
		byte(MSTORE),      // MSTORE
		byte(PUSH1), 0x20, // PUSH1 0x20
		byte(PUSH1), 0x00, // PUSH1 0x00
		byte(RETURN), // RETURN
	}

	sdb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0xaaaa")
	target := types.HexToAddress("0xbbbb")
	sdb.AddBalance(caller, big.NewInt(1e18))
	sdb.CreateAccount(target)
	sdb.SetCode(target, code)

	tracer := NewStructLogTracer()
	cfg := Config{
		Debug:  true,
		Tracer: tracer,
	}
	blockCtx := BlockContext{
		BlockNumber: big.NewInt(1),
		GasLimit:    30_000_000,
		BaseFee:     big.NewInt(1),
	}
	txCtx := TxContext{
		Origin:   caller,
		GasPrice: big.NewInt(1),
	}
	evm := NewEVMWithState(blockCtx, txCtx, cfg, sdb)

	ret, gasLeft, err := evm.Call(caller, target, nil, 100000, big.NewInt(0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The return value should be 32 bytes with 0x42 as the last byte.
	if len(ret) != 32 {
		t.Fatalf("want 32-byte return, got %d bytes", len(ret))
	}
	if ret[31] != 0x42 {
		t.Fatalf("want ret[31] = 0x42, got 0x%02x", ret[31])
	}

	// Gas should have been consumed.
	if gasLeft >= 100000 {
		t.Fatalf("expected gas to be consumed, got gasLeft=%d", gasLeft)
	}

	// Verify the tracer captured steps.
	if len(tracer.Logs) == 0 {
		t.Fatal("expected non-zero trace logs")
	}

	// Verify the opcodes in order:
	// PUSH1, PUSH1, MSTORE, PUSH1, PUSH1, RETURN
	expectedOps := []OpCode{PUSH1, PUSH1, MSTORE, PUSH1, PUSH1, RETURN}
	if len(tracer.Logs) != len(expectedOps) {
		t.Fatalf("want %d trace entries, got %d", len(expectedOps), len(tracer.Logs))
	}
	for i, expected := range expectedOps {
		if tracer.Logs[i].Op != expected {
			t.Fatalf("step %d: want op %v, got %v", i, expected, tracer.Logs[i].Op)
		}
	}

	// Verify first step: PUSH1 0x42 should have empty stack and gas > 0.
	if tracer.Logs[0].Gas == 0 {
		t.Fatal("first step should have non-zero gas")
	}
	if len(tracer.Logs[0].Stack) != 0 {
		t.Fatalf("first step stack should be empty, got %d items", len(tracer.Logs[0].Stack))
	}

	// After PUSH1 0x42, the stack should have [0x42].
	if len(tracer.Logs[1].Stack) != 1 {
		t.Fatalf("step 1 stack should have 1 item, got %d", len(tracer.Logs[1].Stack))
	}
	if tracer.Logs[1].Stack[0].Int64() != 0x42 {
		t.Fatalf("step 1 stack[0] should be 0x42, got %d", tracer.Logs[1].Stack[0].Int64())
	}

	// Verify CaptureStart/CaptureEnd were called (gasUsed should be set).
	if tracer.GasUsed() == 0 {
		t.Fatal("CaptureEnd should have recorded gas used")
	}
}

func TestTracingEVM_Disabled(t *testing.T) {
	// Verify that with Debug=false, no tracing occurs.
	code := []byte{byte(PUSH1), 0x01, byte(STOP)}

	sdb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0xaaaa")
	target := types.HexToAddress("0xbbbb")
	sdb.AddBalance(caller, big.NewInt(1e18))
	sdb.CreateAccount(target)
	sdb.SetCode(target, code)

	tracer := NewStructLogTracer()
	cfg := Config{
		Debug:  false, // disabled
		Tracer: tracer,
	}
	blockCtx := BlockContext{
		BlockNumber: big.NewInt(1),
		GasLimit:    30_000_000,
		BaseFee:     big.NewInt(1),
	}
	txCtx := TxContext{
		Origin:   caller,
		GasPrice: big.NewInt(1),
	}
	evm := NewEVMWithState(blockCtx, txCtx, cfg, sdb)

	_, _, err := evm.Call(caller, target, nil, 100000, big.NewInt(0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tracer.Logs) != 0 {
		t.Fatalf("expected no trace logs when Debug=false, got %d", len(tracer.Logs))
	}
}
