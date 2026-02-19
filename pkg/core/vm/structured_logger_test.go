package vm

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

func TestStructuredLogger_CaptureStart(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{})

	from := types.HexToAddress("0xaaaa")
	to := types.HexToAddress("0xbbbb")
	logger.CaptureStart(from, to, false, []byte{0x01}, 100000, big.NewInt(0))

	// After CaptureStart, logs should be empty (reset state).
	if len(logger.GetLogs()) != 0 {
		t.Fatalf("expected 0 logs after CaptureStart, got %d", len(logger.GetLogs()))
	}
}

func TestStructuredLogger_CaptureState(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{})

	stack := NewStack()
	stack.Push(big.NewInt(42))
	stack.Push(big.NewInt(0xff))
	mem := NewMemory()

	logger.CaptureState(10, PUSH1, 50000, 3, stack, mem, 1, nil)

	logs := logger.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	entry := logs[0]
	if entry.PC != 10 {
		t.Fatalf("expected PC=10, got %d", entry.PC)
	}
	if entry.Op != "PUSH1" {
		t.Fatalf("expected Op=PUSH1, got %s", entry.Op)
	}
	if entry.Gas != 50000 {
		t.Fatalf("expected Gas=50000, got %d", entry.Gas)
	}
	if entry.GasCost != 3 {
		t.Fatalf("expected GasCost=3, got %d", entry.GasCost)
	}
	if entry.Depth != 1 {
		t.Fatalf("expected Depth=1, got %d", entry.Depth)
	}
	if len(entry.Stack) != 2 {
		t.Fatalf("expected 2 stack items, got %d", len(entry.Stack))
	}
	if entry.Stack[0] != "0x2a" {
		t.Fatalf("expected stack[0]=0x2a, got %s", entry.Stack[0])
	}
	if entry.Stack[1] != "0xff" {
		t.Fatalf("expected stack[1]=0xff, got %s", entry.Stack[1])
	}
	if entry.Error != "" {
		t.Fatalf("expected no error, got %q", entry.Error)
	}
}

func TestStructuredLogger_CaptureStateWithError(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{})

	stack := NewStack()
	mem := NewMemory()

	logger.CaptureState(0, STOP, 100, 0, stack, mem, 1, ErrOutOfGas)

	logs := logger.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].Error != "out of gas" {
		t.Fatalf("expected error 'out of gas', got %q", logs[0].Error)
	}
}

func TestStructuredLogger_MemoryCapture(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{
		EnableMemory: true,
	})

	stack := NewStack()
	mem := NewMemory()
	mem.Resize(64)
	mem.Set(0, 4, []byte{0xde, 0xad, 0xbe, 0xef})

	logger.CaptureState(0, MSTORE, 1000, 6, stack, mem, 1, nil)

	logs := logger.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].Memory == nil {
		t.Fatal("expected memory to be captured")
	}
	if len(logs[0].Memory) != 64 {
		t.Fatalf("expected 64 bytes of memory, got %d", len(logs[0].Memory))
	}
	if logs[0].Memory[0] != 0xde || logs[0].Memory[3] != 0xef {
		t.Fatalf("unexpected memory contents: %x", logs[0].Memory[:4])
	}
}

func TestStructuredLogger_MemoryDisabled(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{
		EnableMemory: false,
	})

	stack := NewStack()
	mem := NewMemory()
	mem.Resize(32)

	logger.CaptureState(0, MSTORE, 1000, 6, stack, mem, 1, nil)

	logs := logger.GetLogs()
	if logs[0].Memory != nil {
		t.Fatal("expected memory to be nil when disabled")
	}
}

func TestStructuredLogger_MemoryCopied(t *testing.T) {
	// Verify captured memory is a copy, not a reference.
	logger := NewStructuredLogger(StructuredLoggerConfig{
		EnableMemory: true,
	})

	stack := NewStack()
	mem := NewMemory()
	mem.Resize(32)
	mem.Set(0, 1, []byte{0xaa})

	logger.CaptureState(0, MLOAD, 1000, 3, stack, mem, 1, nil)

	// Mutate the memory after capture.
	mem.Set(0, 1, []byte{0xbb})

	logs := logger.GetLogs()
	if logs[0].Memory[0] != 0xaa {
		t.Fatalf("captured memory should be independent copy, got 0x%02x", logs[0].Memory[0])
	}
}

func TestStructuredLogger_StorageCapture(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{
		EnableStorage: true,
	})

	stack := NewStack()
	mem := NewMemory()

	// Simulate an SSTORE: stack has [key, value] where key is top.
	stack.Push(big.NewInt(100)) // value (Back(1))
	stack.Push(big.NewInt(1))   // key (Back(0))

	logger.CaptureState(0, SSTORE, 5000, 20000, stack, mem, 1, nil)

	// The SSTORE should have been tracked; the next capture should show the slot.
	stack2 := NewStack()
	logger.CaptureState(1, STOP, 3000, 0, stack2, mem, 1, nil)

	logs := logger.GetLogs()
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}

	// Second log should have the storage entry from the SSTORE.
	storage := logs[1].Storage
	if storage == nil {
		t.Fatal("expected storage to be captured on second log")
	}

	key := types.IntToHash(big.NewInt(1))
	val := types.IntToHash(big.NewInt(100))
	if v, ok := storage[key]; !ok {
		t.Fatal("expected storage entry for key=1")
	} else if v != val {
		t.Fatalf("expected storage[1]=%s, got %s", val.Hex(), v.Hex())
	}
}

func TestStructuredLogger_StorageDisabled(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{
		EnableStorage: false,
	})

	stack := NewStack()
	mem := NewMemory()

	logger.CaptureState(0, STOP, 1000, 0, stack, mem, 1, nil)

	logs := logger.GetLogs()
	if logs[0].Storage != nil {
		t.Fatal("expected storage to be nil when disabled")
	}
}

func TestStructuredLogger_CaptureEnd(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{})

	output := []byte{0xca, 0xfe}
	logger.CaptureEnd(output, 21000, nil)

	result := logger.GetResult()
	if result.Gas != 21000 {
		t.Fatalf("expected gas=21000, got %d", result.Gas)
	}
	if result.Failed {
		t.Fatal("expected Failed=false")
	}
	if len(result.ReturnValue) != 2 || result.ReturnValue[0] != 0xca {
		t.Fatalf("unexpected return value: %x", result.ReturnValue)
	}
}

func TestStructuredLogger_CaptureEndWithError(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{})

	logger.CaptureEnd(nil, 50000, errors.New("revert"))

	result := logger.GetResult()
	if !result.Failed {
		t.Fatal("expected Failed=true")
	}
	if result.Gas != 50000 {
		t.Fatalf("expected gas=50000, got %d", result.Gas)
	}
}

func TestStructuredLogger_CaptureEndCopiesOutput(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{})

	output := []byte{0x01, 0x02}
	logger.CaptureEnd(output, 100, nil)

	// Mutate original.
	output[0] = 0xff

	result := logger.GetResult()
	if result.ReturnValue[0] != 0x01 {
		t.Fatal("CaptureEnd should copy output, not alias it")
	}
}

func TestStructuredLogger_GetResult(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{})

	stack := NewStack()
	mem := NewMemory()

	logger.CaptureState(0, PUSH1, 1000, 3, stack, mem, 1, nil)
	logger.CaptureState(2, STOP, 997, 0, stack, mem, 1, nil)
	logger.CaptureEnd([]byte{0x42}, 100, nil)

	result := logger.GetResult()
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Logs) != 2 {
		t.Fatalf("expected 2 logs in result, got %d", len(result.Logs))
	}
	if result.Gas != 100 {
		t.Fatalf("expected gas=100, got %d", result.Gas)
	}
	if result.Failed {
		t.Fatal("expected Failed=false")
	}
}

func TestStructuredLogger_Reset(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{EnableStorage: true})

	stack := NewStack()
	mem := NewMemory()
	logger.CaptureState(0, ADD, 500, 3, stack, mem, 1, nil)
	logger.CaptureEnd([]byte{0x01}, 100, errors.New("boom"))

	// Verify state is populated.
	if len(logger.GetLogs()) == 0 {
		t.Fatal("expected logs before reset")
	}

	logger.Reset()

	if len(logger.GetLogs()) != 0 {
		t.Fatalf("expected 0 logs after reset, got %d", len(logger.GetLogs()))
	}
	result := logger.GetResult()
	if result.Gas != 0 {
		t.Fatalf("expected gas=0 after reset, got %d", result.Gas)
	}
	if result.Failed {
		t.Fatal("expected Failed=false after reset")
	}
	if result.ReturnValue != nil {
		t.Fatal("expected nil return value after reset")
	}
}

func TestStructuredLogger_MultipleSteps(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{})

	stack := NewStack()
	mem := NewMemory()

	for i := 0; i < 5; i++ {
		stack.Push(big.NewInt(int64(i)))
		logger.CaptureState(uint64(i), ADD, uint64(1000-i*3), 3, stack, mem, 1, nil)
	}

	logs := logger.GetLogs()
	if len(logs) != 5 {
		t.Fatalf("expected 5 logs, got %d", len(logs))
	}

	for i, log := range logs {
		if log.PC != uint64(i) {
			t.Fatalf("step %d: expected PC=%d, got %d", i, i, log.PC)
		}
	}
}

func TestFormatLogs(t *testing.T) {
	logs := []StructuredLog{
		{PC: 0, Op: "PUSH1", Gas: 1000, GasCost: 3, Depth: 1, Stack: []string{"0x42"}},
		{PC: 2, Op: "STOP", Gas: 997, GasCost: 0, Depth: 1, Stack: []string{}},
	}

	output := FormatLogs(logs)

	if !strings.Contains(output, "PUSH1") {
		t.Fatal("expected output to contain PUSH1")
	}
	if !strings.Contains(output, "STOP") {
		t.Fatal("expected output to contain STOP")
	}
	if !strings.Contains(output, "0x42") {
		t.Fatal("expected output to contain stack value 0x42")
	}
	if !strings.Contains(output, "gas=1000") {
		t.Fatal("expected output to contain gas=1000")
	}
}

func TestFormatLogs_WithError(t *testing.T) {
	logs := []StructuredLog{
		{PC: 0, Op: "INVALID", Gas: 0, GasCost: 0, Depth: 1, Error: "invalid opcode"},
	}

	output := FormatLogs(logs)
	if !strings.Contains(output, "invalid opcode") {
		t.Fatal("expected output to contain error message")
	}
}

func TestFormatLogs_Empty(t *testing.T) {
	output := FormatLogs(nil)
	if output != "" {
		t.Fatalf("expected empty string for nil logs, got %q", output)
	}
}

func TestFormatLogs_WithMemory(t *testing.T) {
	logs := []StructuredLog{
		{PC: 0, Op: "MSTORE", Gas: 1000, GasCost: 6, Depth: 1, Memory: []byte{0xde, 0xad}},
	}

	output := FormatLogs(logs)
	if !strings.Contains(output, "mem=dead") {
		t.Fatalf("expected output to contain mem=dead, got %q", output)
	}
}

// TestStructuredLogger_EVMInterface verifies the logger satisfies EVMLogger.
func TestStructuredLogger_EVMInterface(t *testing.T) {
	var _ EVMLogger = (*StructuredLogger)(nil)
}

// TestStructuredLogger_IntegrationWithEVM runs a small bytecode and verifies
// structured logging works end-to-end.
func TestStructuredLogger_IntegrationWithEVM(t *testing.T) {
	// Bytecode: PUSH1 0x05 PUSH1 0x03 ADD PUSH1 0x00 MSTORE PUSH1 0x20 PUSH1 0x00 RETURN
	code := []byte{
		byte(PUSH1), 0x05,
		byte(PUSH1), 0x03,
		byte(ADD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}

	sdb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0xaaaa")
	target := types.HexToAddress("0xbbbb")
	sdb.AddBalance(caller, big.NewInt(1e18))
	sdb.CreateAccount(target)
	sdb.SetCode(target, code)

	logger := NewStructuredLogger(StructuredLoggerConfig{
		EnableMemory: true,
	})

	cfg := Config{
		Debug:  true,
		Tracer: logger,
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

	ret, _, err := evm.Call(caller, target, nil, 100000, big.NewInt(0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 5 + 3 = 8, stored at memory[0], returned as 32 bytes.
	if len(ret) != 32 {
		t.Fatalf("expected 32-byte return, got %d", len(ret))
	}
	if ret[31] != 0x08 {
		t.Fatalf("expected ret[31]=0x08, got 0x%02x", ret[31])
	}

	logs := logger.GetLogs()
	expectedOps := []string{"PUSH1", "PUSH1", "ADD", "PUSH1", "MSTORE", "PUSH1", "PUSH1", "RETURN"}
	if len(logs) != len(expectedOps) {
		t.Fatalf("expected %d trace entries, got %d", len(expectedOps), len(logs))
	}
	for i, expected := range expectedOps {
		if logs[i].Op != expected {
			t.Fatalf("step %d: expected op %s, got %s", i, expected, logs[i].Op)
		}
	}

	// Verify that after MSTORE, memory is captured in subsequent steps.
	mstoreIdx := 4 // MSTORE is step index 4
	if logs[mstoreIdx].Op != "MSTORE" {
		t.Fatalf("expected step %d to be MSTORE, got %s", mstoreIdx, logs[mstoreIdx].Op)
	}

	// After MSTORE executes, the next step should have memory captured.
	// The MSTORE step itself records memory before execution.

	result := logger.GetResult()
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Failed {
		t.Fatal("expected execution to succeed")
	}
	if result.Gas == 0 {
		t.Fatal("expected non-zero gas used")
	}

	// Verify FormatLogs produces output.
	formatted := FormatLogs(logs)
	if formatted == "" {
		t.Fatal("expected non-empty formatted output")
	}
}

func TestStructuredLogger_CaptureStartResetsState(t *testing.T) {
	logger := NewStructuredLogger(StructuredLoggerConfig{})

	stack := NewStack()
	mem := NewMemory()

	// First execution.
	logger.CaptureState(0, ADD, 1000, 3, stack, mem, 1, nil)
	logger.CaptureEnd([]byte{0x01}, 100, nil)

	if len(logger.GetLogs()) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logger.GetLogs()))
	}

	// Second execution via CaptureStart should reset.
	from := types.HexToAddress("0xaaaa")
	to := types.HexToAddress("0xbbbb")
	logger.CaptureStart(from, to, false, nil, 50000, big.NewInt(0))

	if len(logger.GetLogs()) != 0 {
		t.Fatalf("expected 0 logs after CaptureStart, got %d", len(logger.GetLogs()))
	}
}
