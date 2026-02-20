package vm

import (
	"testing"
)

func newTestExecutor() *WASMExecutor {
	return NewWASMExecutor(DefaultWASMExecutorConfig())
}

// buildCode is a helper to construct bytecode from opcodes and operands.
func buildCode(parts ...interface{}) []byte {
	var code []byte
	for _, p := range parts {
		switch v := p.(type) {
		case WASMOpcode:
			code = append(code, byte(v))
		case byte:
			code = append(code, v)
		case int:
			// Encode as signed LEB128.
			code = appendSLEB128(code, int32(v))
		case uint32:
			code = appendULEB128(code, v)
		case []byte:
			code = append(code, v...)
		}
	}
	return code
}

func appendULEB128(buf []byte, v uint32) []byte {
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		buf = append(buf, b)
		if v == 0 {
			return buf
		}
	}
}

func appendSLEB128(buf []byte, v int32) []byte {
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			buf = append(buf, b)
			return buf
		}
		buf = append(buf, b|0x80)
	}
}

func TestWASMExecutorI32Add(t *testing.T) {
	ex := newTestExecutor()
	// i32.const 10, i32.const 20, i32.add, end
	code := buildCode(WASMOpcodeI32Const, 10, WASMOpcodeI32Const, 20, WASMOpcodeI32Add, WASMOpcodeEnd)
	results, gas, err := ex.ExecuteBytecode(code, "add", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || uint32(results[0]) != 30 {
		t.Fatalf("expected [30], got %v", results)
	}
	if gas == 0 {
		t.Fatal("expected non-zero gas")
	}
}

func TestWASMExecutorI32Sub(t *testing.T) {
	ex := newTestExecutor()
	code := buildCode(WASMOpcodeI32Const, 50, WASMOpcodeI32Const, 20, WASMOpcodeI32Sub, WASMOpcodeEnd)
	results, _, err := ex.ExecuteBytecode(code, "sub", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || uint32(results[0]) != 30 {
		t.Fatalf("expected [30], got %v", results)
	}
}

func TestWASMExecutorI32Mul(t *testing.T) {
	ex := newTestExecutor()
	code := buildCode(WASMOpcodeI32Const, 6, WASMOpcodeI32Const, 7, WASMOpcodeI32Mul, WASMOpcodeEnd)
	results, _, err := ex.ExecuteBytecode(code, "mul", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || uint32(results[0]) != 42 {
		t.Fatalf("expected [42], got %v", results)
	}
}

func TestWASMExecutorI32DivS(t *testing.T) {
	ex := newTestExecutor()
	code := buildCode(WASMOpcodeI32Const, 100, WASMOpcodeI32Const, 10, WASMOpcodeI32DivS, WASMOpcodeEnd)
	results, _, err := ex.ExecuteBytecode(code, "div", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || uint32(results[0]) != 10 {
		t.Fatalf("expected [10], got %v", results)
	}
}

func TestWASMExecutorDivisionByZero(t *testing.T) {
	ex := newTestExecutor()
	code := buildCode(WASMOpcodeI32Const, 42, WASMOpcodeI32Const, 0, WASMOpcodeI32DivS, WASMOpcodeEnd)
	_, _, err := ex.ExecuteBytecode(code, "divzero", 0, nil)
	if err != ErrWASMDivisionByZero {
		t.Fatalf("expected ErrWASMDivisionByZero, got %v", err)
	}
}

func TestWASMExecutorUnreachable(t *testing.T) {
	ex := newTestExecutor()
	code := buildCode(WASMOpcodeUnreachable, WASMOpcodeEnd)
	_, _, err := ex.ExecuteBytecode(code, "trap", 0, nil)
	if err != ErrWASMUnreachable {
		t.Fatalf("expected ErrWASMUnreachable, got %v", err)
	}
}

func TestWASMExecutorStackOverflow(t *testing.T) {
	cfg := DefaultWASMExecutorConfig()
	cfg.MaxStackDepth = 4
	cfg.GasLimit = 100_000
	ex := NewWASMExecutor(cfg)
	// Push 5 values onto a stack of depth 4.
	code := buildCode(
		WASMOpcodeI32Const, 1,
		WASMOpcodeI32Const, 2,
		WASMOpcodeI32Const, 3,
		WASMOpcodeI32Const, 4,
		WASMOpcodeI32Const, 5, // overflow
		WASMOpcodeEnd,
	)
	_, _, err := ex.ExecuteBytecode(code, "overflow", 0, nil)
	if err != ErrWASMStackOverflow {
		t.Fatalf("expected ErrWASMStackOverflow, got %v", err)
	}
}

func TestWASMExecutorStackUnderflow(t *testing.T) {
	ex := newTestExecutor()
	// Try to add with empty stack.
	code := buildCode(WASMOpcodeI32Add, WASMOpcodeEnd)
	_, _, err := ex.ExecuteBytecode(code, "underflow", 0, nil)
	if err != ErrWASMStackUnderflow {
		t.Fatalf("expected ErrWASMStackUnderflow, got %v", err)
	}
}

func TestWASMExecutorLocalGetSet(t *testing.T) {
	ex := newTestExecutor()
	// local.get 0 (arg), i32.const 5, i32.add, local.set 1, local.get 1, end
	code := buildCode(
		WASMOpcodeLocalGet, uint32(0),
		WASMOpcodeI32Const, 5,
		WASMOpcodeI32Add,
		WASMOpcodeLocalSet, uint32(1),
		WASMOpcodeLocalGet, uint32(1),
		WASMOpcodeEnd,
	)
	results, _, err := ex.ExecuteBytecode(code, "localtest", 1, []uint64{10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || uint32(results[0]) != 15 {
		t.Fatalf("expected [15], got %v", results)
	}
}

func TestWASMExecutorI32LoadStore(t *testing.T) {
	ex := newTestExecutor()
	// Store 42 at memory offset 100, then load it back.
	// i32.const 100 (base addr), i32.const 42, i32.store align=2 offset=0
	// i32.const 100 (base addr), i32.load align=2 offset=0
	code := buildCode(
		WASMOpcodeI32Const, 100,
		WASMOpcodeI32Const, 42,
		WASMOpcodeI32Store, uint32(2), uint32(0), // align=2, offset=0
		WASMOpcodeI32Const, 100,
		WASMOpcodeI32Load, uint32(2), uint32(0), // align=2, offset=0
		WASMOpcodeEnd,
	)
	results, _, err := ex.ExecuteBytecode(code, "memtest", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || uint32(results[0]) != 42 {
		t.Fatalf("expected [42], got %v", results)
	}
}

func TestWASMExecutorOutOfMemory(t *testing.T) {
	cfg := DefaultWASMExecutorConfig()
	cfg.MaxMemoryPages = 1 // 65536 bytes
	ex := NewWASMExecutor(cfg)
	// Try to load from beyond memory bounds.
	code := buildCode(
		WASMOpcodeI32Const, int(70000), // beyond 65536
		WASMOpcodeI32Load, uint32(0), uint32(0),
		WASMOpcodeEnd,
	)
	_, _, err := ex.ExecuteBytecode(code, "oom", 0, nil)
	if err != ErrWASMOutOfMemory {
		t.Fatalf("expected ErrWASMOutOfMemory, got %v", err)
	}
}

func TestWASMExecutorGasExhausted(t *testing.T) {
	cfg := DefaultWASMExecutorConfig()
	cfg.GasLimit = 5 // very low
	ex := NewWASMExecutor(cfg)
	// This should run out of gas after a few instructions.
	code := buildCode(
		WASMOpcodeI32Const, 1,
		WASMOpcodeI32Const, 2,
		WASMOpcodeI32Add,
		WASMOpcodeI32Const, 3,
		WASMOpcodeI32Add,
		WASMOpcodeEnd,
	)
	_, gasUsed, err := ex.ExecuteBytecode(code, "gastest", 0, nil)
	if err != ErrWASMGasExhausted {
		t.Fatalf("expected ErrWASMGasExhausted, got %v (gas=%d)", err, gasUsed)
	}
}

func TestWASMExecutorReturn(t *testing.T) {
	ex := newTestExecutor()
	// Push a value and return early, skipping the unreachable.
	code := buildCode(
		WASMOpcodeI32Const, 99,
		WASMOpcodeReturn,
		WASMOpcodeUnreachable,
		WASMOpcodeEnd,
	)
	results, _, err := ex.ExecuteBytecode(code, "rettest", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || uint32(results[0]) != 99 {
		t.Fatalf("expected [99], got %v", results)
	}
}

func TestWASMExecutorBlockAndBranch(t *testing.T) {
	ex := newTestExecutor()
	// block, i32.const 42, br 0 (exit block), unreachable, end, end
	code := buildCode(
		WASMOpcodeBlock, byte(0x40), // void block
		WASMOpcodeI32Const, 42,
		WASMOpcodeBr, uint32(0), // break to end of block
		WASMOpcodeUnreachable,
		WASMOpcodeEnd,
		WASMOpcodeEnd,
	)
	results, _, err := ex.ExecuteBytecode(code, "blocktest", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || uint32(results[0]) != 42 {
		t.Fatalf("expected [42], got %v", results)
	}
}

func TestWASMExecutorLoopBrIf(t *testing.T) {
	ex := newTestExecutor()
	// Compute sum 1+2+3+4+5 using a loop with local counter.
	// local 0 = counter (arg, starts at 5), local 1 = sum (local, starts at 0)
	code := buildCode(
		WASMOpcodeBlock, byte(0x40), // outer block for exit
		WASMOpcodeLoop, byte(0x40), // loop
		// sum += counter
		WASMOpcodeLocalGet, uint32(1), // get sum
		WASMOpcodeLocalGet, uint32(0), // get counter
		WASMOpcodeI32Add,
		WASMOpcodeLocalSet, uint32(1), // store sum
		// counter -= 1
		WASMOpcodeLocalGet, uint32(0),
		WASMOpcodeI32Const, 1,
		WASMOpcodeI32Sub,
		WASMOpcodeLocalSet, uint32(0),
		// if counter > 0, branch back to loop
		WASMOpcodeLocalGet, uint32(0),
		WASMOpcodeBrIf, uint32(0), // branch to loop start
		WASMOpcodeEnd, // end loop
		WASMOpcodeEnd, // end block
		// Push result
		WASMOpcodeLocalGet, uint32(1),
		WASMOpcodeEnd,
	)
	results, _, err := ex.ExecuteBytecode(code, "looptest", 1, []uint64{5})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || uint32(results[0]) != 15 {
		t.Fatalf("expected [15], got %v", results)
	}
}

func TestWASMExecutorCallFunction(t *testing.T) {
	ex := newTestExecutor()

	// Register function 0: "double" - takes 1 arg, returns arg*2
	doubleCode := buildCode(
		WASMOpcodeLocalGet, uint32(0),
		WASMOpcodeI32Const, 2,
		WASMOpcodeI32Mul,
		WASMOpcodeEnd,
	)
	ex.RegisterFunction("double", doubleCode, 1, 0)

	// Register function 1: "main" - calls double(21)
	mainCode := buildCode(
		WASMOpcodeI32Const, 21,
		WASMOpcodeCall, uint32(0), // call "double"
		WASMOpcodeEnd,
	)
	ex.RegisterFunction("main", mainCode, 0, 0)

	results, _, err := ex.ExecuteFunction("main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || uint32(results[0]) != 42 {
		t.Fatalf("expected [42], got %v", results)
	}
}

func TestWASMExecutorNop(t *testing.T) {
	ex := newTestExecutor()
	code := buildCode(
		WASMOpcodeNop,
		WASMOpcodeI32Const, 7,
		WASMOpcodeNop,
		WASMOpcodeEnd,
	)
	results, _, err := ex.ExecuteBytecode(code, "nop", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || uint32(results[0]) != 7 {
		t.Fatalf("expected [7], got %v", results)
	}
}

func TestWASMExecutorFunctionNotFound(t *testing.T) {
	ex := newTestExecutor()
	_, _, err := ex.ExecuteFunction("nonexistent", nil)
	if err != errWASMNoFunction {
		t.Fatalf("expected errWASMNoFunction, got %v", err)
	}
}

func TestWASMExecutorInvalidOpcode(t *testing.T) {
	ex := newTestExecutor()
	code := []byte{0xFF} // invalid opcode
	_, _, err := ex.ExecuteBytecode(code, "invalid", 0, nil)
	if err != errWASMInvalidOpcode {
		t.Fatalf("expected errWASMInvalidOpcode, got %v", err)
	}
}

func TestWASMExecutorMemorySliceAndSet(t *testing.T) {
	ex := newTestExecutor()
	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	if err := ex.SetMemory(256, data); err != nil {
		t.Fatal(err)
	}
	got, err := ex.MemorySlice(256, 4)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range data {
		if got[i] != b {
			t.Fatalf("memory[%d]: got 0x%02x, want 0x%02x", i, got[i], b)
		}
	}
}

func TestWASMExecutorMemorySliceOutOfBounds(t *testing.T) {
	cfg := DefaultWASMExecutorConfig()
	cfg.MaxMemoryPages = 1 // 65536 bytes
	ex := NewWASMExecutor(cfg)
	_, err := ex.MemorySlice(65530, 10)
	if err != ErrWASMOutOfMemory {
		t.Fatalf("expected ErrWASMOutOfMemory, got %v", err)
	}
}

func TestWASMExecutorGasAccounting(t *testing.T) {
	ex := newTestExecutor()
	// Simple: const + const + add + end = 1 + 1 + 3 + 1 = 6 gas
	code := buildCode(
		WASMOpcodeI32Const, 1,
		WASMOpcodeI32Const, 2,
		WASMOpcodeI32Add,
		WASMOpcodeEnd,
	)
	_, gas, err := ex.ExecuteBytecode(code, "gasmeasure", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	// const(1) + const(1) + arith(3) + end(1) = 6
	if gas != 6 {
		t.Fatalf("expected 6 gas, got %d", gas)
	}
}

func TestWASMExecutorNegativeConst(t *testing.T) {
	ex := newTestExecutor()
	// i32.const -5, i32.const 10, i32.add -> 5
	code := buildCode(
		WASMOpcodeI32Const, -5,
		WASMOpcodeI32Const, 10,
		WASMOpcodeI32Add,
		WASMOpcodeEnd,
	)
	results, _, err := ex.ExecuteBytecode(code, "neg", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || uint32(results[0]) != 5 {
		t.Fatalf("expected [5], got %v (raw: %d)", results, results[0])
	}
}

func TestWASMExecutorI32DivSNegative(t *testing.T) {
	ex := newTestExecutor()
	// -10 / 3 = -3 (truncated towards zero)
	code := buildCode(
		WASMOpcodeI32Const, -10,
		WASMOpcodeI32Const, 3,
		WASMOpcodeI32DivS,
		WASMOpcodeEnd,
	)
	results, _, err := ex.ExecuteBytecode(code, "divneg", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	got := int32(uint32(results[0]))
	if got != -3 {
		t.Fatalf("expected -3, got %d", got)
	}
}

func TestWASMExecutorDefaultConfig(t *testing.T) {
	cfg := DefaultWASMExecutorConfig()
	if cfg.MaxMemoryPages != 16 {
		t.Fatalf("MaxMemoryPages: got %d, want 16", cfg.MaxMemoryPages)
	}
	if cfg.MaxStackDepth != 1024 {
		t.Fatalf("MaxStackDepth: got %d, want 1024", cfg.MaxStackDepth)
	}
	if cfg.GasLimit != 1_000_000 {
		t.Fatalf("GasLimit: got %d, want 1000000", cfg.GasLimit)
	}
	if cfg.MaxCallDepth != 64 {
		t.Fatalf("MaxCallDepth: got %d, want 64", cfg.MaxCallDepth)
	}
}

func TestWASMExecutorOpcodeConstants(t *testing.T) {
	// Verify opcode constants match the WASM spec.
	if WASMOpcodeUnreachable != 0x00 {
		t.Fatal("unreachable opcode mismatch")
	}
	if WASMOpcodeI32Add != 0x6A {
		t.Fatal("i32.add opcode mismatch")
	}
	if WASMOpcodeI32Const != 0x41 {
		t.Fatal("i32.const opcode mismatch")
	}
	if WASMOpcodeEnd != 0x0B {
		t.Fatal("end opcode mismatch")
	}
}

func TestWASMExecutorSetMemoryOutOfBounds(t *testing.T) {
	cfg := DefaultWASMExecutorConfig()
	cfg.MaxMemoryPages = 1 // 65536 bytes
	ex := NewWASMExecutor(cfg)
	data := make([]byte, 100)
	err := ex.SetMemory(65500, data) // 65500+100 > 65536
	if err != ErrWASMOutOfMemory {
		t.Fatalf("expected ErrWASMOutOfMemory, got %v", err)
	}
}
