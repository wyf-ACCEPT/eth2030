package vm

import (
	"errors"
	"testing"
)

func defaultInterp() *EWASMInterpreter {
	return NewEWASMInterpreter(DefaultEWASMInterpreterConfig())
}

func TestInterpI32Const(t *testing.T) {
	interp := defaultInterp()
	result, gas, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 42},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 42 {
		t.Fatalf("expected [42], got %v", result)
	}
	if gas == 0 {
		t.Fatal("expected gas > 0")
	}
}

func TestInterpI32Add(t *testing.T) {
	interp := defaultInterp()
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 10},
		{Opcode: InterpOpI32Const, Immediate: 20},
		{Opcode: InterpOpI32Add},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 30 {
		t.Fatalf("expected [30], got %v", result)
	}
}

func TestInterpI32Sub(t *testing.T) {
	interp := defaultInterp()
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 50},
		{Opcode: InterpOpI32Const, Immediate: 20},
		{Opcode: InterpOpI32Sub},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 30 {
		t.Fatalf("expected [30], got %v", result)
	}
}

func TestInterpI32Mul(t *testing.T) {
	interp := defaultInterp()
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 6},
		{Opcode: InterpOpI32Const, Immediate: 7},
		{Opcode: InterpOpI32Mul},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 42 {
		t.Fatalf("expected [42], got %v", result)
	}
}

func TestInterpI32DivU(t *testing.T) {
	interp := defaultInterp()
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 100},
		{Opcode: InterpOpI32Const, Immediate: 10},
		{Opcode: InterpOpI32DivU},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 10 {
		t.Fatalf("expected [10], got %v", result)
	}
}

func TestInterpI32DivUByZero(t *testing.T) {
	interp := defaultInterp()
	_, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 42},
		{Opcode: InterpOpI32Const, Immediate: 0},
		{Opcode: InterpOpI32DivU},
	}, nil)
	if !errors.Is(err, ErrInterpDivisionByZero) {
		t.Fatalf("expected ErrInterpDivisionByZero, got %v", err)
	}
}

func TestInterpI32Eq(t *testing.T) {
	interp := defaultInterp()
	// Equal case.
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 5},
		{Opcode: InterpOpI32Const, Immediate: 5},
		{Opcode: InterpOpI32Eq},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 1 {
		t.Fatalf("expected [1], got %v", result)
	}

	interp.Reset() // Not equal case.
	result, _, err = interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 5},
		{Opcode: InterpOpI32Const, Immediate: 6},
		{Opcode: InterpOpI32Eq},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 0 {
		t.Fatalf("expected [0], got %v", result)
	}
}

func TestInterpI32LtU(t *testing.T) {
	interp := defaultInterp()
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 3},
		{Opcode: InterpOpI32Const, Immediate: 10},
		{Opcode: InterpOpI32LtU},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 1 {
		t.Fatalf("expected [1] (3 < 10), got %v", result)
	}

	interp.Reset()
	result, _, err = interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 10},
		{Opcode: InterpOpI32Const, Immediate: 3},
		{Opcode: InterpOpI32LtU},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].I32 != 0 {
		t.Fatalf("expected 0 (10 not < 3), got %v", result[0].I32)
	}
}

func TestInterpI32GtU(t *testing.T) {
	interp := defaultInterp()
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 10},
		{Opcode: InterpOpI32Const, Immediate: 3},
		{Opcode: InterpOpI32GtU},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 1 {
		t.Fatalf("expected [1] (10 > 3), got %v", result)
	}
}

func TestInterpLocalGetSet(t *testing.T) {
	interp := defaultInterp()
	args := []WasmValue{I32Val(100), I32Val(200)}
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpLocalGet, Immediate: 0},
		{Opcode: InterpOpLocalGet, Immediate: 1},
		{Opcode: InterpOpI32Add},
	}, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 300 {
		t.Fatalf("expected [300], got %v", result)
	}
}

func TestInterpLocalSet(t *testing.T) {
	interp := defaultInterp()
	args := []WasmValue{I32Val(0)}
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 99},
		{Opcode: InterpOpLocalSet, Immediate: 0},
		{Opcode: InterpOpLocalGet, Immediate: 0},
	}, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 99 {
		t.Fatalf("expected [99], got %v", result)
	}
}

func TestInterpDrop(t *testing.T) {
	interp := defaultInterp()
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 1},
		{Opcode: InterpOpI32Const, Immediate: 2},
		{Opcode: InterpOpDrop},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 1 {
		t.Fatalf("expected [1] after drop, got %v", result)
	}
}

func TestInterpSelect(t *testing.T) {
	interp := defaultInterp()
	result, _, err := interp.Execute([]WasmInstruction{ // cond != 0: val1
		{Opcode: InterpOpI32Const, Immediate: 10},
		{Opcode: InterpOpI32Const, Immediate: 20},
		{Opcode: InterpOpI32Const, Immediate: 1},
		{Opcode: InterpOpSelect},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 10 {
		t.Fatalf("expected [10] (select val1), got %v", result)
	}

	interp.Reset() // Condition is zero: select val2.
	result, _, err = interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 10},
		{Opcode: InterpOpI32Const, Immediate: 20},
		{Opcode: InterpOpI32Const, Immediate: 0},
		{Opcode: InterpOpSelect},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 20 {
		t.Fatalf("expected [20] (select val2), got %v", result)
	}
}

func TestInterpReturn(t *testing.T) {
	interp := defaultInterp()
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 42},
		{Opcode: InterpOpReturn},
		{Opcode: InterpOpI32Const, Immediate: 99}, // should not execute
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 42 {
		t.Fatalf("expected [42] from return, got %v", result)
	}
}

func TestInterpNop(t *testing.T) {
	interp := defaultInterp()
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpNop},
		{Opcode: InterpOpI32Const, Immediate: 7},
		{Opcode: InterpOpNop},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 7 {
		t.Fatalf("expected [7], got %v", result)
	}
}

func TestInterpUnreachable(t *testing.T) {
	interp := defaultInterp()
	_, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpUnreachable},
	}, nil)
	if !errors.Is(err, ErrInterpUnreachable) {
		t.Fatalf("expected ErrInterpUnreachable, got %v", err)
	}
}

func TestInterpStackOverflow(t *testing.T) {
	cfg := EWASMInterpreterConfig{MaxStackDepth: 2, GasLimit: 1_000_000}
	interp := NewEWASMInterpreter(cfg)
	_, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 1},
		{Opcode: InterpOpI32Const, Immediate: 2},
		{Opcode: InterpOpI32Const, Immediate: 3}, // overflow
	}, nil)
	if !errors.Is(err, ErrInterpStackOverflow) {
		t.Fatalf("expected ErrInterpStackOverflow, got %v", err)
	}
}

func TestInterpStackUnderflow(t *testing.T) {
	interp := defaultInterp()
	_, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Add}, // nothing on stack
	}, nil)
	if !errors.Is(err, ErrInterpStackUnderflow) {
		t.Fatalf("expected ErrInterpStackUnderflow, got %v", err)
	}
}

func TestInterpOutOfGas(t *testing.T) {
	cfg := EWASMInterpreterConfig{MaxStackDepth: 1024, GasLimit: 2}
	interp := NewEWASMInterpreter(cfg)
	_, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 1},
		{Opcode: InterpOpI32Const, Immediate: 2},
		{Opcode: InterpOpI32Const, Immediate: 3}, // gas=3 > limit=2
	}, nil)
	if !errors.Is(err, ErrInterpOutOfGas) {
		t.Fatalf("expected ErrInterpOutOfGas, got %v", err)
	}
}

func TestInterpUnknownOpcode(t *testing.T) {
	interp := defaultInterp()
	_, _, err := interp.Execute([]WasmInstruction{
		{Opcode: 0xFF},
	}, nil)
	if !errors.Is(err, ErrInterpUnknownOpcode) {
		t.Fatalf("expected ErrInterpUnknownOpcode, got %v", err)
	}
}

func TestInterpLocalOutOfRange(t *testing.T) {
	interp := defaultInterp()
	_, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpLocalGet, Immediate: 5},
	}, nil)
	if !errors.Is(err, ErrInterpLocalOutOfRange) {
		t.Fatalf("expected ErrInterpLocalOutOfRange, got %v", err)
	}
}

func TestInterpGasUsed(t *testing.T) {
	interp := defaultInterp()
	_, gas, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 5},
		{Opcode: InterpOpI32Const, Immediate: 3},
		{Opcode: InterpOpI32Add},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 3 instructions x 1 gas each = 3.
	if gas != 3 {
		t.Fatalf("expected gas=3, got %d", gas)
	}
	if interp.GasUsed() != 3 {
		t.Fatalf("GasUsed() expected 3, got %d", interp.GasUsed())
	}
}

func TestInterpReset(t *testing.T) {
	interp := defaultInterp()
	_, _, _ = interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 1},
	}, nil)
	if interp.GasUsed() == 0 {
		t.Fatal("expected gas > 0 before reset")
	}
	interp.Reset()
	if interp.GasUsed() != 0 {
		t.Fatal("expected gas=0 after reset")
	}
}

func TestInterpComplexProgram(t *testing.T) {
	// Compute (a + b) * (a - b) where a=10, b=3 -> (13) * (7) = 91.
	interp := defaultInterp()
	args := []WasmValue{I32Val(10), I32Val(3)}
	result, _, err := interp.Execute([]WasmInstruction{
		// a + b
		{Opcode: InterpOpLocalGet, Immediate: 0},
		{Opcode: InterpOpLocalGet, Immediate: 1},
		{Opcode: InterpOpI32Add},
		// a - b
		{Opcode: InterpOpLocalGet, Immediate: 0},
		{Opcode: InterpOpLocalGet, Immediate: 1},
		{Opcode: InterpOpI32Sub},
		// multiply
		{Opcode: InterpOpI32Mul},
	}, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].I32 != 91 {
		t.Fatalf("expected [91], got %v", result)
	}
}

func TestInterpDropUnderflow(t *testing.T) {
	interp := defaultInterp()
	_, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpDrop},
	}, nil)
	if !errors.Is(err, ErrInterpStackUnderflow) {
		t.Fatalf("expected ErrInterpStackUnderflow from drop, got %v", err)
	}
}

func TestInterpEmptyProgram(t *testing.T) {
	interp := defaultInterp()
	result, gas, err := interp.Execute(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %v", result)
	}
	if gas != 0 {
		t.Fatalf("expected gas=0, got %d", gas)
	}
}

func TestInterpI32MulGasExtra(t *testing.T) {
	// Mul costs 1 (base) + 2 (extra) = 3 gas for the mul instruction.
	interp := defaultInterp()
	_, gas, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 2},
		{Opcode: InterpOpI32Const, Immediate: 3},
		{Opcode: InterpOpI32Mul},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2 consts (1 each) + 1 mul (1 base + 2 extra) = 5.
	if gas != 5 {
		t.Fatalf("expected gas=5, got %d", gas)
	}
}

func TestInterpLocalSetOutOfRange(t *testing.T) {
	interp := defaultInterp()
	_, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 1},
		{Opcode: InterpOpLocalSet, Immediate: 0},
	}, nil) // no locals
	if !errors.Is(err, ErrInterpLocalOutOfRange) {
		t.Fatalf("expected ErrInterpLocalOutOfRange, got %v", err)
	}
}

func TestInterpSelectUnderflow(t *testing.T) {
	interp := defaultInterp()
	_, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 1},
		{Opcode: InterpOpI32Const, Immediate: 2},
		{Opcode: InterpOpSelect}, // needs 3 values, only 2
	}, nil)
	if !errors.Is(err, ErrInterpStackUnderflow) {
		t.Fatalf("expected ErrInterpStackUnderflow, got %v", err)
	}
}

func TestInterpI32DivUUnsigned(t *testing.T) {
	// -1 as signed int32 is 0xFFFFFFFF = 4294967295 as unsigned.
	// 4294967295 / 2 = 2147483647 unsigned.
	interp := defaultInterp()
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: -1},
		{Opcode: InterpOpI32Const, Immediate: 2},
		{Opcode: InterpOpI32DivU},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := int32(uint32(0xFFFFFFFF) / 2)
	if len(result) != 1 || result[0].I32 != expected {
		t.Fatalf("expected [%d], got %v", expected, result)
	}
}

func TestInterpMultipleReturnValues(t *testing.T) {
	interp := defaultInterp()
	result, _, err := interp.Execute([]WasmInstruction{
		{Opcode: InterpOpI32Const, Immediate: 1},
		{Opcode: InterpOpI32Const, Immediate: 2},
		{Opcode: InterpOpI32Const, Immediate: 3},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 values, got %d", len(result))
	}
	if result[0].I32 != 1 || result[1].I32 != 2 || result[2].I32 != 3 {
		t.Fatalf("expected [1,2,3], got %v", result)
	}
}
