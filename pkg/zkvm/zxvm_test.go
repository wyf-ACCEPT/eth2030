package zkvm

import (
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
)

// --- Config tests ---

func TestDefaultZxVMConfig(t *testing.T) {
	cfg := DefaultZxVMConfig()
	if cfg.MaxCycles == 0 {
		t.Error("expected non-zero MaxCycles")
	}
	if cfg.MemoryLimit == 0 {
		t.Error("expected non-zero MemoryLimit")
	}
	if cfg.ProofSystem != "stark" {
		t.Errorf("expected proof system 'stark', got %q", cfg.ProofSystem)
	}
	if cfg.StackDepth != 1024 {
		t.Errorf("expected stack depth 1024, got %d", cfg.StackDepth)
	}
}

// --- NewZxVM tests ---

func TestNewZxVM(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())
	if vm == nil {
		t.Fatal("expected non-nil vm")
	}
}

// --- LoadProgram tests ---

func TestLoadProgram(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())
	prog := &ZxProgram{
		Code:         []byte{ZxOpHALT},
		EntryPoint:   0,
		MemoryLayout: 128,
		GasLimit:     1000,
	}
	err := vm.LoadProgram(prog)
	if err != nil {
		t.Fatalf("LoadProgram failed: %v", err)
	}
}

func TestLoadProgramNil(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())
	err := vm.LoadProgram(nil)
	if err != ErrZxEmptyCode {
		t.Errorf("expected ErrZxEmptyCode, got %v", err)
	}
}

func TestLoadProgramEmptyCode(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())
	err := vm.LoadProgram(&ZxProgram{Code: nil})
	if err != ErrZxEmptyCode {
		t.Errorf("expected ErrZxEmptyCode, got %v", err)
	}
	err = vm.LoadProgram(&ZxProgram{Code: []byte{}})
	if err != ErrZxEmptyCode {
		t.Errorf("expected ErrZxEmptyCode for empty slice, got %v", err)
	}
}

func TestLoadProgramMemoryClamp(t *testing.T) {
	cfg := DefaultZxVMConfig()
	cfg.MemoryLimit = 64
	vm := NewZxVM(cfg)

	// Request more than limit.
	prog := &ZxProgram{
		Code:         []byte{ZxOpHALT},
		MemoryLayout: 256,
	}
	err := vm.LoadProgram(prog)
	if err != nil {
		t.Fatalf("LoadProgram failed: %v", err)
	}
	// Memory should be clamped to limit.
	if uint64(len(vm.memory)) != 64 {
		t.Errorf("expected memory clamped to 64, got %d", len(vm.memory))
	}
}

// --- Execute tests ---

func TestExecuteNoProgram(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())
	_, err := vm.Execute()
	if err != ErrZxProgramNotLoaded {
		t.Errorf("expected ErrZxProgramNotLoaded, got %v", err)
	}
}

func TestExecuteHalt(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())
	prog := &ZxProgram{Code: []byte{ZxOpHALT}}
	vm.LoadProgram(prog)

	result, err := vm.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.CycleCount != 1 {
		t.Errorf("expected 1 cycle, got %d", result.CycleCount)
	}
	if result.GasUsed != 0 {
		t.Errorf("HALT costs 0 gas, got %d", result.GasUsed)
	}
	if len(result.ProofCommitment) == 0 {
		t.Error("expected non-empty proof commitment")
	}
}

func TestExecutePushAndHalt(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(42)...)
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	result, err := vm.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result.Output) != 8 {
		t.Fatalf("expected 8-byte output, got %d", len(result.Output))
	}
	val := binary.LittleEndian.Uint64(result.Output)
	if val != 42 {
		t.Errorf("expected output 42, got %d", val)
	}
}

func TestExecuteAdd(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(10)...)
	code = append(code, BuildZxPush(20)...)
	code = append(code, ZxOpADD)
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	result, err := vm.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	val := binary.LittleEndian.Uint64(result.Output)
	if val != 30 {
		t.Errorf("expected 30, got %d", val)
	}
}

func TestExecuteSub(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(50)...)
	code = append(code, BuildZxPush(8)...)
	code = append(code, ZxOpSUB)
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	result, err := vm.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	val := binary.LittleEndian.Uint64(result.Output)
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
}

func TestExecuteMul(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(6)...)
	code = append(code, BuildZxPush(7)...)
	code = append(code, ZxOpMUL)
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	result, err := vm.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	val := binary.LittleEndian.Uint64(result.Output)
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
}

func TestExecuteLoadStore(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	// Store 99 at address 5.
	code = append(code, BuildZxPush(5)...)   // addr
	code = append(code, BuildZxPush(99)...)  // val
	code = append(code, ZxOpSTORE)
	// Load from address 5.
	code = append(code, BuildZxPush(5)...)
	code = append(code, ZxOpLOAD)
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code, MemoryLayout: 256}
	vm.LoadProgram(prog)

	result, err := vm.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	val := binary.LittleEndian.Uint64(result.Output)
	if val != 99 {
		t.Errorf("expected 99, got %d", val)
	}
	if result.MemoryPeak == 0 {
		t.Error("expected non-zero memory peak after STORE")
	}
}

func TestExecuteHash(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(42)...)
	code = append(code, ZxOpHASH)
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	result, err := vm.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	val := binary.LittleEndian.Uint64(result.Output)
	if val == 42 {
		t.Error("hash output should differ from input")
	}
	if val == 0 {
		t.Error("expected non-zero hash result")
	}
}

func TestExecuteJump(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	// Build code: PUSH 19, JUMP, <unreachable PUSH 0xFF, HALT>, PUSH 77, HALT
	// Layout:
	// [0..8]   PUSH 19 (9 bytes)
	// [9]      JUMP (1 byte)
	// [10..18] PUSH 0xFF (9 bytes, unreachable)
	// [19]     HALT (1 byte, unreachable path skipped)
	// But we want to jump to a PUSH 77 + HALT at offset 19.
	// [19..27] PUSH 77 (9 bytes)
	// [28]     HALT
	var code []byte
	code = append(code, BuildZxPush(19)...) // target = 19
	code = append(code, ZxOpJUMP)
	// Pad to offset 19 (currently at 10). Need 9 bytes of padding.
	code = append(code, BuildZxPush(0xFF)...) // unreachable
	// Now at offset 19.
	code = append(code, BuildZxPush(77)...)
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	result, err := vm.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	val := binary.LittleEndian.Uint64(result.Output)
	if val != 77 {
		t.Errorf("expected 77 (jumped over unreachable), got %d", val)
	}
}

func TestExecuteInvalidJump(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(9999)...) // out of bounds
	code = append(code, ZxOpJUMP)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	_, err := vm.Execute()
	if err != ErrZxInvalidJump {
		t.Errorf("expected ErrZxInvalidJump, got %v", err)
	}
}

func TestZxVMExecuteStackUnderflow(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	// ADD with empty stack.
	prog := &ZxProgram{Code: []byte{ZxOpADD}}
	vm.LoadProgram(prog)

	_, err := vm.Execute()
	if err != ErrZxStackUnderflow {
		t.Errorf("expected ErrZxStackUnderflow, got %v", err)
	}
}

func TestExecuteStackOverflow(t *testing.T) {
	cfg := DefaultZxVMConfig()
	cfg.StackDepth = 2
	vm := NewZxVM(cfg)

	var code []byte
	code = append(code, BuildZxPush(1)...)
	code = append(code, BuildZxPush(2)...)
	code = append(code, BuildZxPush(3)...) // overflow
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	_, err := vm.Execute()
	if err != ErrZxStackOverflow {
		t.Errorf("expected ErrZxStackOverflow, got %v", err)
	}
}

func TestExecuteMemoryOverflowLoad(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(999999)...) // out of bounds
	code = append(code, ZxOpLOAD)

	prog := &ZxProgram{Code: code, MemoryLayout: 16}
	vm.LoadProgram(prog)

	_, err := vm.Execute()
	if err != ErrZxMemoryOverflow {
		t.Errorf("expected ErrZxMemoryOverflow, got %v", err)
	}
}

func TestExecuteMemoryOverflowStore(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(999999)...) // addr out of bounds
	code = append(code, BuildZxPush(42)...)      // val
	code = append(code, ZxOpSTORE)

	prog := &ZxProgram{Code: code, MemoryLayout: 16}
	vm.LoadProgram(prog)

	_, err := vm.Execute()
	if err != ErrZxMemoryOverflow {
		t.Errorf("expected ErrZxMemoryOverflow, got %v", err)
	}
}

func TestExecuteInvalidOpcode(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	prog := &ZxProgram{Code: []byte{0xFF}} // unknown opcode
	vm.LoadProgram(prog)

	_, err := vm.Execute()
	if err == nil {
		t.Fatal("expected error for invalid opcode")
	}
}

func TestExecuteCycleLimit(t *testing.T) {
	cfg := DefaultZxVMConfig()
	cfg.MaxCycles = 3
	vm := NewZxVM(cfg)

	var code []byte
	code = append(code, BuildZxPush(1)...)
	code = append(code, BuildZxPush(2)...)
	code = append(code, BuildZxPush(3)...)
	code = append(code, BuildZxPush(4)...) // 4th cycle
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	_, err := vm.Execute()
	if err != ErrZxCycleLimitHit {
		t.Errorf("expected ErrZxCycleLimitHit, got %v", err)
	}
}

func TestZxVMExecuteOutOfGas(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(1)...)  // 1 gas
	code = append(code, BuildZxPush(2)...)  // 1 gas
	code = append(code, ZxOpHASH)           // 10 gas -> total 12
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code, GasLimit: 5} // only 5 gas
	vm.LoadProgram(prog)

	_, err := vm.Execute()
	if err != ErrZxOutOfGas {
		t.Errorf("expected ErrZxOutOfGas, got %v", err)
	}
}

func TestExecuteTruncatedPush(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	// PUSH with only 3 bytes of immediate (needs 8).
	prog := &ZxProgram{Code: []byte{ZxOpPUSH, 0x01, 0x02, 0x03}}
	vm.LoadProgram(prog)

	_, err := vm.Execute()
	if err != ErrZxTruncatedPush {
		t.Errorf("expected ErrZxTruncatedPush, got %v", err)
	}
}

func TestExecuteImplicitHalt(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	// Code without explicit HALT; falls off the end.
	code := BuildZxPush(55)
	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	result, err := vm.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	val := binary.LittleEndian.Uint64(result.Output)
	if val != 55 {
		t.Errorf("expected 55, got %d", val)
	}
}

func TestExecuteDeterministic(t *testing.T) {
	var code []byte
	code = append(code, BuildZxPush(10)...)
	code = append(code, BuildZxPush(20)...)
	code = append(code, ZxOpADD)
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}

	vm1 := NewZxVM(DefaultZxVMConfig())
	vm1.LoadProgram(prog)
	r1, _ := vm1.Execute()

	vm2 := NewZxVM(DefaultZxVMConfig())
	vm2.LoadProgram(prog)
	r2, _ := vm2.Execute()

	if !bytesEqual(r1.Output, r2.Output) {
		t.Error("expected deterministic output")
	}
	if r1.GasUsed != r2.GasUsed {
		t.Error("expected deterministic gas")
	}
	if r1.CycleCount != r2.CycleCount {
		t.Error("expected deterministic cycles")
	}
	if !bytesEqual(r1.ProofCommitment, r2.ProofCommitment) {
		t.Error("expected deterministic proof commitment")
	}
}

// --- GenerateTrace tests ---

func TestGenerateTrace(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(5)...)
	code = append(code, BuildZxPush(3)...)
	code = append(code, ZxOpADD)
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	trace, err := vm.GenerateTrace()
	if err != nil {
		t.Fatalf("GenerateTrace failed: %v", err)
	}
	// 4 steps: PUSH, PUSH, ADD, HALT
	if len(trace.Steps) != 4 {
		t.Fatalf("expected 4 trace steps, got %d", len(trace.Steps))
	}
	if trace.Steps[0].Opcode != ZxOpPUSH {
		t.Errorf("step 0: expected PUSH opcode")
	}
	if trace.Steps[2].Opcode != ZxOpADD {
		t.Errorf("step 2: expected ADD opcode")
	}
	if trace.Steps[3].Opcode != ZxOpHALT {
		t.Errorf("step 3: expected HALT opcode")
	}
	// Each step should have 32-byte hashes.
	for i, s := range trace.Steps {
		if len(s.StackHash) != 32 {
			t.Errorf("step %d: expected 32-byte stack hash, got %d", i, len(s.StackHash))
		}
		if len(s.MemHash) != 32 {
			t.Errorf("step %d: expected 32-byte mem hash, got %d", i, len(s.MemHash))
		}
	}
}

func TestGenerateTraceNoProgram(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())
	_, err := vm.GenerateTrace()
	if err != ErrZxProgramNotLoaded {
		t.Errorf("expected ErrZxProgramNotLoaded, got %v", err)
	}
}

// --- VerifyTrace tests ---

func TestVerifyTrace(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(10)...)
	code = append(code, BuildZxPush(20)...)
	code = append(code, ZxOpADD)
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	trace, err := vm.GenerateTrace()
	if err != nil {
		t.Fatalf("GenerateTrace failed: %v", err)
	}

	// Reload program to reset state before verification.
	vm.LoadProgram(prog)
	valid, err := vm.VerifyTrace(trace)
	if err != nil {
		t.Fatalf("VerifyTrace failed: %v", err)
	}
	if !valid {
		t.Error("expected valid trace")
	}
}

func TestVerifyTraceNilTrace(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())
	vm.LoadProgram(&ZxProgram{Code: []byte{ZxOpHALT}})

	valid, err := vm.VerifyTrace(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("nil trace should not verify")
	}
}

func TestVerifyTraceTampered(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(10)...)
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	trace, _ := vm.GenerateTrace()

	// Tamper with a stack hash.
	trace.Steps[0].StackHash[0] ^= 0xFF

	vm.LoadProgram(prog)
	valid, _ := vm.VerifyTrace(trace)
	if valid {
		t.Error("expected tampered trace to fail verification")
	}
}

func TestVerifyTraceNoProgram(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())
	_, err := vm.VerifyTrace(&ZxTrace{Steps: []ZxStep{{}}})
	if err != ErrZxProgramNotLoaded {
		t.Errorf("expected ErrZxProgramNotLoaded, got %v", err)
	}
}

// --- EstimateGas tests ---

func TestEstimateGas(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(10)...)  // 1 gas
	code = append(code, BuildZxPush(20)...)  // 1 gas
	code = append(code, ZxOpADD)             // 1 gas
	code = append(code, ZxOpHALT)            // 0 gas

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	gas, err := vm.EstimateGas()
	if err != nil {
		t.Fatalf("EstimateGas failed: %v", err)
	}
	if gas != 3 {
		t.Errorf("expected gas estimate 3, got %d", gas)
	}
}

func TestEstimateGasNoProgram(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())
	_, err := vm.EstimateGas()
	if err != ErrZxProgramNotLoaded {
		t.Errorf("expected ErrZxProgramNotLoaded, got %v", err)
	}
}

func TestEstimateGasPreservesState(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	var code []byte
	code = append(code, BuildZxPush(42)...)
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code}
	vm.LoadProgram(prog)

	// Run estimate.
	vm.EstimateGas()

	// Execute should still work correctly.
	vm.LoadProgram(prog) // reload
	result, err := vm.Execute()
	if err != nil {
		t.Fatalf("Execute after EstimateGas failed: %v", err)
	}
	val := binary.LittleEndian.Uint64(result.Output)
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
}

// --- Thread safety test ---

func TestThreadSafety(t *testing.T) {
	var wg sync.WaitGroup
	errs := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			vm := NewZxVM(DefaultZxVMConfig())

			var code []byte
			code = append(code, BuildZxPush(uint64(n))...)
			code = append(code, BuildZxPush(100)...)
			code = append(code, ZxOpADD)
			code = append(code, ZxOpHALT)

			prog := &ZxProgram{Code: code}
			if err := vm.LoadProgram(prog); err != nil {
				errs <- err
				return
			}

			result, err := vm.Execute()
			if err != nil {
				errs <- err
				return
			}

			val := binary.LittleEndian.Uint64(result.Output)
			expected := uint64(n) + 100
			if val != expected {
				errs <- fmt.Errorf("goroutine %d: expected %d, got %d", n, expected, val)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent execution error: %v", err)
	}
}

// --- BuildZxPush test ---

func TestBuildZxPush(t *testing.T) {
	buf := BuildZxPush(0x0102030405060708)
	if len(buf) != 9 {
		t.Fatalf("expected 9 bytes, got %d", len(buf))
	}
	if buf[0] != ZxOpPUSH {
		t.Errorf("expected PUSH opcode 0x%02x, got 0x%02x", ZxOpPUSH, buf[0])
	}
	val := binary.LittleEndian.Uint64(buf[1:])
	if val != 0x0102030405060708 {
		t.Errorf("expected 0x0102030405060708, got 0x%X", val)
	}
}

// --- Opcode gas costs ---

func TestZxOpcodeGasCosts(t *testing.T) {
	tests := []struct {
		op   byte
		gas  uint64
		name string
	}{
		{ZxOpADD, zxGasArith, "ADD"},
		{ZxOpSUB, zxGasArith, "SUB"},
		{ZxOpMUL, zxGasArith, "MUL"},
		{ZxOpLOAD, zxGasLoad, "LOAD"},
		{ZxOpSTORE, zxGasStore, "STORE"},
		{ZxOpJUMP, zxGasJump, "JUMP"},
		{ZxOpHALT, zxGasHalt, "HALT"},
		{ZxOpHASH, zxGasHash, "HASH"},
		{ZxOpPUSH, zxGasPush, "PUSH"},
	}
	for _, tc := range tests {
		if zxOpcodeGas(tc.op) != tc.gas {
			t.Errorf("%s: expected gas %d, got %d", tc.name, tc.gas, zxOpcodeGas(tc.op))
		}
	}
}

// --- Complex program test ---

func TestExecuteComplexProgram(t *testing.T) {
	vm := NewZxVM(DefaultZxVMConfig())

	// Compute (3 + 4) * 5 = 35, store at address 0, load it back.
	var code []byte
	code = append(code, BuildZxPush(3)...)
	code = append(code, BuildZxPush(4)...)
	code = append(code, ZxOpADD)             // 7
	code = append(code, BuildZxPush(5)...)
	code = append(code, ZxOpMUL)             // 35
	code = append(code, BuildZxPush(0)...)   // addr
	code = append(code, BuildZxPush(35)...)  // store 35 to check later
	code = append(code, ZxOpSTORE)
	code = append(code, BuildZxPush(0)...)   // addr
	code = append(code, ZxOpLOAD)            // load from addr 0
	code = append(code, ZxOpHALT)

	prog := &ZxProgram{Code: code, MemoryLayout: 64}
	vm.LoadProgram(prog)

	result, err := vm.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	val := binary.LittleEndian.Uint64(result.Output)
	if val != 35 {
		t.Errorf("expected 35, got %d", val)
	}
	if result.GasUsed == 0 {
		t.Error("expected non-zero gas used")
	}
}

// --- Opcode constants sanity ---

func TestZxOpcodeConstants(t *testing.T) {
	// Verify all opcodes have distinct values.
	opcodes := []byte{
		ZxOpADD, ZxOpSUB, ZxOpMUL, ZxOpLOAD, ZxOpSTORE,
		ZxOpJUMP, ZxOpHALT, ZxOpHASH, ZxOpPUSH,
	}
	seen := make(map[byte]bool)
	for _, op := range opcodes {
		if seen[op] {
			t.Errorf("duplicate opcode: 0x%02x", op)
		}
		seen[op] = true
	}
}
