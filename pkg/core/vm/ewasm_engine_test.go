package vm

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestEWASMEngineI32Const(t *testing.T) {
	code := []byte{engineOpI32Const, 0x2A}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, gas, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gas >= 10000 {
		t.Errorf("expected gas to be consumed, got %d", gas)
	}
	if len(out) != 4 {
		t.Fatalf("output length = %d, want 4", len(out))
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("result = %d, want 42", val)
	}
}

func TestEWASMEngineI32Add(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x0A,
		engineOpI32Const, 0x20,
		engineOpI32Add,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("10 + 32 = %d, want 42", val)
	}
}

func TestEWASMEngineI32Sub(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x32,
		engineOpI32Const, 0x08,
		engineOpI32Sub,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("50 - 8 = %d, want 42", val)
	}
}

func TestEWASMEngineI32Mul(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x06,
		engineOpI32Const, 0x07,
		engineOpI32Mul,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("6 * 7 = %d, want 42", val)
	}
}

func TestEWASMEngineI32DivU(t *testing.T) {
	// 84 in signed LEB128: 0xD4 0x00 (values >= 64 need 2 bytes).
	code := []byte{
		engineOpI32Const, 0xD4, 0x00,
		engineOpI32Const, 0x02,
		engineOpI32DivU,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("84 / 2 = %d, want 42", val)
	}
}

func TestEWASMEngineI32DivByZero(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x0A,
		engineOpI32Const, 0x00,
		engineOpI32DivU,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	_, _, err := engine.Execute(wasm, nil, 10000)
	if !errors.Is(err, errEngineDivisionByZero) {
		t.Errorf("expected errEngineDivisionByZero, got %v", err)
	}
}

func TestEWASMEngineI32RemU(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x0A,
		engineOpI32Const, 0x03,
		engineOpI32RemU,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 1 {
		t.Errorf("10 %% 3 = %d, want 1", val)
	}
}

func TestEWASMEngineBitwise(t *testing.T) {
	// Use values < 64 to fit in single-byte signed LEB128.
	// Signed LEB128: values 0-63 fit in a single byte.
	tests := []struct {
		name string
		op   byte
		a, b uint32
		want uint32
	}{
		{"and", engineOpI32And, 0x3F, 0x0F, 0x0F},
		{"or", engineOpI32Or, 0x30, 0x0F, 0x3F},
		{"xor", engineOpI32Xor, 0x3F, 0x0F, 0x30},
		{"shl", engineOpI32Shl, 1, 4, 16},
		{"shr_u", engineOpI32ShrU, 0x20, 2, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := []byte{
				engineOpI32Const, byte(tt.a),
				engineOpI32Const, byte(tt.b),
				tt.op,
			}
			wasm := BuildEngineWasm(code, 0, "main")
			engine := NewEWASMEngine()

			out, _, err := engine.Execute(wasm, nil, 10000)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			val := binary.LittleEndian.Uint32(out)
			if val != tt.want {
				t.Errorf("%s(%d, %d) = %d, want %d", tt.name, tt.a, tt.b, val, tt.want)
			}
		})
	}
}

func TestEWASMEngineLocals(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x2A,
		engineOpLocalSet, 0x00,
		engineOpLocalGet, 0x00,
	}
	wasm := BuildEngineWasm(code, 1, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("local get/set: got %d, want 42", val)
	}
}

func TestEWASMEngineDrop(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x2A, // push 42
		engineOpI32Const, 0xE3, 0x00, // push 99 (signed LEB128)
		engineOpDrop, // drop 99
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("drop: got %d, want 42", val)
	}
}

func TestEWASMEngineSelect(t *testing.T) {
	// select(val1=10, val2=20, cond=1) -> 10
	code := []byte{
		engineOpI32Const, 0x0A,
		engineOpI32Const, 0x14,
		engineOpI32Const, 0x01,
		engineOpSelect,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 10 {
		t.Errorf("select(10, 20, true): got %d, want 10", val)
	}

	// select(val1=10, val2=20, cond=0) -> 20
	code2 := []byte{
		engineOpI32Const, 0x0A,
		engineOpI32Const, 0x14,
		engineOpI32Const, 0x00,
		engineOpSelect,
	}
	wasm2 := BuildEngineWasm(code2, 0, "main")
	out2, _, err2 := engine.Execute(wasm2, nil, 10000)
	if err2 != nil {
		t.Fatalf("Execute: %v", err2)
	}
	val2 := binary.LittleEndian.Uint32(out2)
	if val2 != 20 {
		t.Errorf("select(10, 20, false): got %d, want 20", val2)
	}
}

func TestEWASMEngineMemoryLoadStore(t *testing.T) {
	// addr = 100 (0x64) in signed LEB128: 0xE4 0x00 (bit 6 set, needs 2 bytes)
	code := []byte{
		engineOpI32Const, 0xE4, 0x00, // addr = 100
		engineOpI32Const, 0x2A, // val = 42
		engineOpI32Store, 0x02, 0x00, // alignment=2, offset=0
		engineOpI32Const, 0xE4, 0x00, // addr = 100
		engineOpI32Load, 0x02, 0x00, // alignment=2, offset=0
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("memory load/store: got %d, want 42", val)
	}
}

func TestEWASMEngineMemoryOutOfBounds(t *testing.T) {
	// addr = 65536 in LEB128
	code := []byte{
		engineOpI32Const, 0x80, 0x80, 0x04, // 65536
		engineOpI32Load, 0x02, 0x00,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	_, _, err := engine.Execute(wasm, nil, 10000)
	if !errors.Is(err, errEngineMemoryOOB) {
		t.Errorf("expected errEngineMemoryOOB, got %v", err)
	}
}

func TestEWASMEngineOutOfGas(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x01,
		engineOpI32Const, 0x02,
		engineOpI32Add,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	// Give only 2 gas (need at least call gas of 10).
	_, _, err := engine.Execute(wasm, nil, 2)
	if !errors.Is(err, errEngineOutOfGas) {
		t.Errorf("expected errEngineOutOfGas, got %v", err)
	}
}

func TestEWASMEngineGasDeduction(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x0A,
		engineOpI32Const, 0x14,
		engineOpI32Add,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	_, remaining, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	used := 10000 - remaining
	if used == 0 {
		t.Error("expected some gas to be consumed")
	}
	// At minimum: 10 (call) + 3 (instructions) + 1 (end) = 14 gas.
	if used < 14 {
		t.Errorf("expected at least 14 gas used, got %d", used)
	}
}

func TestEWASMEngineStackUnderflow(t *testing.T) {
	code := []byte{engineOpI32Add}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	_, _, err := engine.Execute(wasm, nil, 10000)
	if !errors.Is(err, errEngineStackUnderflow) {
		t.Errorf("expected errEngineStackUnderflow, got %v", err)
	}
}

func TestEWASMEngineInvalidModule(t *testing.T) {
	engine := NewEWASMEngine()
	_, _, err := engine.Execute([]byte{0xFF, 0xFF}, nil, 10000)
	if !errors.Is(err, errEngineInvalidModule) {
		t.Errorf("expected errEngineInvalidModule, got %v", err)
	}
}

func TestEWASMEngineNop(t *testing.T) {
	code := []byte{
		engineOpNop,
		engineOpI32Const, 0x2A,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("nop + const 42: got %d, want 42", val)
	}
}

func TestEWASMEngineUnreachable(t *testing.T) {
	code := []byte{engineOpUnreachable}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	_, _, err := engine.Execute(wasm, nil, 10000)
	if !errors.Is(err, errEngineUnreachable) {
		t.Errorf("expected errEngineUnreachable, got %v", err)
	}
}

func TestEWASMEngineReturn(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x2A,
		engineOpReturn,
		engineOpI32Const, 0xE3, 0x00, // should not execute (99)
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("return: got %d, want 42", val)
	}
}

func TestEWASMEngineI32Eqz(t *testing.T) {
	// eqz(0) = 1
	code := []byte{engineOpI32Const, 0x00, engineOpI32Eqz}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if binary.LittleEndian.Uint32(out) != 1 {
		t.Errorf("eqz(0) = %d, want 1", binary.LittleEndian.Uint32(out))
	}

	// eqz(5) = 0
	code2 := []byte{engineOpI32Const, 0x05, engineOpI32Eqz}
	wasm2 := BuildEngineWasm(code2, 0, "main")
	out2, _, err2 := engine.Execute(wasm2, nil, 10000)
	if err2 != nil {
		t.Fatalf("Execute: %v", err2)
	}
	if binary.LittleEndian.Uint32(out2) != 0 {
		t.Errorf("eqz(5) = %d, want 0", binary.LittleEndian.Uint32(out2))
	}
}

func TestEWASMEngineI32Eq(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x05,
		engineOpI32Const, 0x05,
		engineOpI32Eq,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if binary.LittleEndian.Uint32(out) != 1 {
		t.Errorf("eq(5, 5) = %d, want 1", binary.LittleEndian.Uint32(out))
	}
}

func TestEWASMEngineComparisons(t *testing.T) {
	tests := []struct {
		name string
		op   byte
		a, b uint32
		want uint32
	}{
		{"lt_u true", engineOpI32LtU, 3, 5, 1},
		{"lt_u false", engineOpI32LtU, 5, 3, 0},
		{"gt_u true", engineOpI32GtU, 5, 3, 1},
		{"gt_u false", engineOpI32GtU, 3, 5, 0},
		{"le_u true eq", engineOpI32LeU, 5, 5, 1},
		{"le_u true lt", engineOpI32LeU, 3, 5, 1},
		{"ge_u true eq", engineOpI32GeU, 5, 5, 1},
		{"ge_u false", engineOpI32GeU, 3, 5, 0},
	}

	engine := NewEWASMEngine()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := []byte{
				engineOpI32Const, byte(tt.a),
				engineOpI32Const, byte(tt.b),
				tt.op,
			}
			wasm := BuildEngineWasm(code, 0, "main")
			out, _, err := engine.Execute(wasm, nil, 10000)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			val := binary.LittleEndian.Uint32(out)
			if val != tt.want {
				t.Errorf("%s: got %d, want %d", tt.name, val, tt.want)
			}
		})
	}
}

func TestEWASMEngineInputToMemory(t *testing.T) {
	input := make([]byte, 4)
	binary.LittleEndian.PutUint32(input, 12345)

	code := []byte{
		engineOpI32Const, 0x00, // addr = 0
		engineOpI32Load, 0x02, 0x00, // load from memory[0]
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, input, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 12345 {
		t.Errorf("input to memory: got %d, want 12345", val)
	}
}

func TestEWASMEngineBlock(t *testing.T) {
	code := []byte{
		engineOpBlock, 0x40,
		engineOpI32Const, 0x2A,
		engineOpEnd,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("block: got %d, want 42", val)
	}
}

func TestEWASMEngineBrIf(t *testing.T) {
	// br_if with false condition does not branch.
	code := []byte{
		engineOpBlock, 0x40,
		engineOpI32Const, 0x00, // false
		engineOpBrIf, 0x00,
		engineOpI32Const, 0x2A, // push 42
		engineOpEnd,
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("br_if false: got %d, want 42", val)
	}
}

func TestEWASMEngineThreadSafe(t *testing.T) {
	engine := NewEWASMEngine()
	code := []byte{
		engineOpI32Const, 0x2A,
		engineOpI32Const, 0x01,
		engineOpI32Add,
	}
	wasm := BuildEngineWasm(code, 0, "main")

	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			out, _, err := engine.Execute(wasm, nil, 10000)
			if err != nil {
				errs <- err
				return
			}
			val := binary.LittleEndian.Uint32(out)
			if val != 43 {
				errs <- errors.New("wrong result in concurrent execution")
				return
			}
			errs <- nil
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent execution %d: %v", i, err)
		}
	}
}

func TestEWASMEngineInvalidLocal(t *testing.T) {
	code := []byte{engineOpLocalGet, 0x05}
	wasm := BuildEngineWasm(code, 1, "main")
	engine := NewEWASMEngine()

	_, _, err := engine.Execute(wasm, nil, 10000)
	if !errors.Is(err, errEngineInvalidLocal) {
		t.Errorf("expected errEngineInvalidLocal, got %v", err)
	}
}

func TestEWASMEngineInvalidOpcode(t *testing.T) {
	code := []byte{0xFE}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	_, _, err := engine.Execute(wasm, nil, 10000)
	if !errors.Is(err, errEngineInvalidOpcode) {
		t.Errorf("expected errEngineInvalidOpcode, got %v", err)
	}
}

func TestEngineDecodeSLEB128Values(t *testing.T) {
	tests := []struct {
		input []byte
		want  int32
		wantN int
	}{
		{[]byte{0x00}, 0, 1},
		{[]byte{0x01}, 1, 1},
		{[]byte{0x7F}, -1, 1},
		{[]byte{0x80, 0x01}, 128, 2},
		{[]byte{0xFF, 0x7E}, -129, 2},
	}
	for _, tt := range tests {
		got, n, err := engineDecodeSLEB128(tt.input)
		if err != nil {
			t.Errorf("engineDecodeSLEB128(%v): %v", tt.input, err)
			continue
		}
		if got != tt.want || n != tt.wantN {
			t.Errorf("engineDecodeSLEB128(%v) = (%d, %d), want (%d, %d)",
				tt.input, got, n, tt.want, tt.wantN)
		}
	}
}

func TestEngineDecodeSLEB128Invalid(t *testing.T) {
	_, _, err := engineDecodeSLEB128([]byte{0x80, 0x80, 0x80, 0x80, 0x80})
	if err == nil {
		t.Error("expected error for unterminated signed LEB128")
	}
}

func TestBuildEngineWasmValid(t *testing.T) {
	code := []byte{engineOpI32Const, 0x01}
	wasm := BuildEngineWasm(code, 0, "test")
	if err := ValidateWasmBytecode(wasm); err != nil {
		t.Fatalf("BuildEngineWasm produced invalid WASM: %v", err)
	}
	mod, err := CompileModule(wasm)
	if err != nil {
		t.Fatalf("CompileModule: %v", err)
	}
	if len(mod.ExportedFunctions) != 1 || mod.ExportedFunctions[0] != "test" {
		t.Errorf("exports = %v, want [test]", mod.ExportedFunctions)
	}
}

func TestBuildEngineWasmLocals(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x2A,
		engineOpLocalSet, 0x00,
		engineOpLocalGet, 0x00,
	}
	wasm := BuildEngineWasm(code, 3, "run")
	if err := ValidateWasmBytecode(wasm); err != nil {
		t.Fatalf("BuildEngineWasm with locals produced invalid WASM: %v", err)
	}
}

func TestEWASMEngineNegativeConst(t *testing.T) {
	code := []byte{engineOpI32Const, 0x7F} // -1 in signed LEB128
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 0xFFFFFFFF {
		t.Errorf("const -1: got %d (0x%X), want 0xFFFFFFFF", val, val)
	}
}

func TestEWASMEngineMemoryStoreOffset(t *testing.T) {
	code := []byte{
		engineOpI32Const, 0x00, // addr = 0
		engineOpI32Const, 0x2A, // val = 42
		engineOpI32Store, 0x02, 0x08, // alignment=2, offset=8
		engineOpI32Const, 0x00, // addr = 0
		engineOpI32Load, 0x02, 0x08, // alignment=2, offset=8
	}
	wasm := BuildEngineWasm(code, 0, "main")
	engine := NewEWASMEngine()

	out, _, err := engine.Execute(wasm, nil, 10000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	val := binary.LittleEndian.Uint32(out)
	if val != 42 {
		t.Errorf("memory store with offset: got %d, want 42", val)
	}
}

func TestEngineFindExportFuncValid(t *testing.T) {
	wasm := BuildMinimalWasm("run")
	sections, _ := parseSections(wasm[8:])
	for _, s := range sections {
		if s.ID == WasmSectionExport {
			idx := engineFindExportFunc(s.Data)
			if idx != 0 {
				t.Errorf("engineFindExportFunc = %d, want 0", idx)
			}
		}
	}
}

func TestEngineFindExportFuncNil(t *testing.T) {
	idx := engineFindExportFunc(nil)
	if idx != -1 {
		t.Errorf("engineFindExportFunc(nil) = %d, want -1", idx)
	}
}
