package zkvm

import (
	"errors"
	"testing"
)

// --- Compiler tests ---

func TestNewEWASMCompiler(t *testing.T) {
	c := NewEWASMCompiler()
	if c == nil {
		t.Fatal("expected non-nil compiler")
	}
}

func TestCompileEmptyBytecode(t *testing.T) {
	c := NewEWASMCompiler()
	_, err := c.Compile(nil)
	if err != ErrEWASMEmptyBytecode {
		t.Errorf("expected ErrEWASMEmptyBytecode, got %v", err)
	}
	_, err = c.Compile([]byte{})
	if err != ErrEWASMEmptyBytecode {
		t.Errorf("expected ErrEWASMEmptyBytecode for empty slice, got %v", err)
	}
}

func TestCompileSimpleBytecode(t *testing.T) {
	c := NewEWASMCompiler()
	// EVM: PUSH1 5, PUSH1 3, ADD
	evm := []byte{0x60, 0x05, 0x60, 0x03, 0x01}
	mod, err := c.Compile(evm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if mod == nil {
		t.Fatal("expected non-nil module")
	}
	if len(mod.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(mod.Functions))
	}
	fn := mod.GetFunction("main")
	if fn == nil {
		t.Fatal("expected main function")
	}
	if fn.Name != "main" {
		t.Errorf("expected function name 'main', got '%s'", fn.Name)
	}
	if len(fn.Body) == 0 {
		t.Fatal("expected non-empty function body")
	}
	if mod.MemorySize != 1 {
		t.Errorf("expected 1 memory page, got %d", mod.MemorySize)
	}
}

func TestCompileModuleHasMemoryAndGlobals(t *testing.T) {
	c := NewEWASMCompiler()
	mod, err := c.Compile([]byte{0x01}) // ADD
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(mod.Memory) != 65536 {
		t.Errorf("expected 65536 bytes of memory, got %d", len(mod.Memory))
	}
	if len(mod.Globals) != 4 {
		t.Errorf("expected 4 globals, got %d", len(mod.Globals))
	}
}

func TestCompileGetFunctionNotFound(t *testing.T) {
	c := NewEWASMCompiler()
	mod, _ := c.Compile([]byte{0x01})
	fn := mod.GetFunction("nonexistent")
	if fn != nil {
		t.Error("expected nil for nonexistent function")
	}
}

// --- Validate tests ---

func TestValidateNilModule(t *testing.T) {
	c := NewEWASMCompiler()
	err := c.Validate(nil)
	if err != ErrEWASMInvalidModule {
		t.Errorf("expected ErrEWASMInvalidModule, got %v", err)
	}
}

func TestValidateEmptyFunctions(t *testing.T) {
	c := NewEWASMCompiler()
	mod := &EWASMModule{Functions: nil, funcIndex: map[string]int{}}
	err := c.Validate(mod)
	if err != ErrEWASMEmptyFunctions {
		t.Errorf("expected ErrEWASMEmptyFunctions, got %v", err)
	}
}

func TestValidateEmptyBody(t *testing.T) {
	c := NewEWASMCompiler()
	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "test", Body: nil}},
		funcIndex: map[string]int{"test": 0},
	}
	err := c.Validate(mod)
	if !errors.Is(err, ErrEWASMEmptyBody) {
		t.Errorf("expected ErrEWASMEmptyBody, got %v", err)
	}
}

func TestValidateCompiledModule(t *testing.T) {
	c := NewEWASMCompiler()
	mod, err := c.Compile([]byte{0x60, 0x05, 0x60, 0x03, 0x01})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	err = c.Validate(mod)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateTruncatedConst(t *testing.T) {
	c := NewEWASMCompiler()
	mod := &EWASMModule{
		Functions: []*EWASMFunction{{
			Name: "bad",
			Body: []byte{byte(I64Const), 0x01, 0x02}, // truncated
		}},
		funcIndex: map[string]int{"bad": 0},
	}
	err := c.Validate(mod)
	if !errors.Is(err, ErrEWASMInvalidOpcode) {
		t.Errorf("expected ErrEWASMInvalidOpcode, got %v", err)
	}
}

func TestValidateUnknownOpcode(t *testing.T) {
	c := NewEWASMCompiler()
	mod := &EWASMModule{
		Functions: []*EWASMFunction{{
			Name: "bad",
			Body: []byte{0xFF},
		}},
		funcIndex: map[string]int{"bad": 0},
	}
	err := c.Validate(mod)
	if !errors.Is(err, ErrEWASMInvalidOpcode) {
		t.Errorf("expected ErrEWASMInvalidOpcode, got %v", err)
	}
}

// --- Interpreter tests ---

func TestNewEWASMInterpreter(t *testing.T) {
	interp := NewEWASMInterpreter(1000)
	if interp == nil {
		t.Fatal("expected non-nil interpreter")
	}
	if interp.gasLimit != 1000 {
		t.Errorf("expected gas limit 1000, got %d", interp.gasLimit)
	}
}

func TestExecuteNilModule(t *testing.T) {
	interp := NewEWASMInterpreter(1000)
	_, _, err := interp.Execute(nil, "main", nil)
	if err != ErrEWASMInvalidModule {
		t.Errorf("expected ErrEWASMInvalidModule, got %v", err)
	}
}

func TestExecuteFunctionNotFound(t *testing.T) {
	interp := NewEWASMInterpreter(1000)
	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "main", Body: []byte{byte(Return)}}},
		funcIndex: map[string]int{"main": 0},
	}
	_, _, err := interp.Execute(mod, "missing", nil)
	if !errors.Is(err, ErrEWASMNoFunction) {
		t.Errorf("expected ErrEWASMNoFunction, got %v", err)
	}
}

func TestExecuteSimpleAdd(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// Build bytecode: i64.const 10, i64.const 20, i64.add, return
	var body []byte
	body = append(body, BuildI64Const(10)...)
	body = append(body, BuildI64Const(20)...)
	body = append(body, byte(I64Add))
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{
			Name:      "add",
			Body:      body,
			NumLocals: 4,
		}},
		funcIndex: map[string]int{"add": 0},
	}

	results, gas, err := interp.Execute(mod, "add", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 30 {
		t.Errorf("expected [30], got %v", results)
	}
	if gas == 0 {
		t.Error("expected non-zero gas usage")
	}
}

func TestExecuteSubtract(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	var body []byte
	body = append(body, BuildI64Const(50)...)
	body = append(body, BuildI64Const(8)...)
	body = append(body, byte(I64Sub))
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "sub", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"sub": 0},
	}

	results, _, err := interp.Execute(mod, "sub", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 42 {
		t.Errorf("expected [42], got %v", results)
	}
}

func TestExecuteMultiply(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	var body []byte
	body = append(body, BuildI64Const(6)...)
	body = append(body, BuildI64Const(7)...)
	body = append(body, byte(I64Mul))
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "mul", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"mul": 0},
	}

	results, _, err := interp.Execute(mod, "mul", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 42 {
		t.Errorf("expected [42], got %v", results)
	}
}

func TestExecuteBitwiseAndOr(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// AND: 0xFF & 0x0F = 0x0F
	var bodyAnd []byte
	bodyAnd = append(bodyAnd, BuildI64Const(0xFF)...)
	bodyAnd = append(bodyAnd, BuildI64Const(0x0F)...)
	bodyAnd = append(bodyAnd, byte(I64And))
	bodyAnd = append(bodyAnd, byte(Return))

	modAnd := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: bodyAnd, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}
	results, _, err := interp.Execute(modAnd, "f", nil)
	if err != nil {
		t.Fatalf("AND Execute: %v", err)
	}
	if results[0] != 0x0F {
		t.Errorf("AND: expected 0x0F, got 0x%X", results[0])
	}

	// OR: 0xF0 | 0x0F = 0xFF
	var bodyOr []byte
	bodyOr = append(bodyOr, BuildI64Const(0xF0)...)
	bodyOr = append(bodyOr, BuildI64Const(0x0F)...)
	bodyOr = append(bodyOr, byte(I64Or))
	bodyOr = append(bodyOr, byte(Return))

	modOr := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: bodyOr, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}
	results, _, err = interp.Execute(modOr, "f", nil)
	if err != nil {
		t.Fatalf("OR Execute: %v", err)
	}
	if results[0] != 0xFF {
		t.Errorf("OR: expected 0xFF, got 0x%X", results[0])
	}
}

func TestExecuteLocals(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// local.get 0 (param), i64.const 100, i64.add, return
	var body []byte
	body = append(body, BuildLocalGet(0)...)
	body = append(body, BuildI64Const(100)...)
	body = append(body, byte(I64Add))
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{
			Name:       "f",
			ParamTypes: []byte{0x7E}, // one i64 param
			Body:       body,
			NumLocals:  4,
		}},
		funcIndex: map[string]int{"f": 0},
	}

	results, _, err := interp.Execute(mod, "f", []uint64{42})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 142 {
		t.Errorf("expected [142], got %v", results)
	}
}

func TestExecuteLocalSet(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// i64.const 99, local.set 1, local.get 1, return
	var body []byte
	body = append(body, BuildI64Const(99)...)
	body = append(body, BuildLocalSet(1)...)
	body = append(body, BuildLocalGet(1)...)
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{
			Name:      "f",
			Body:      body,
			NumLocals: 4,
		}},
		funcIndex: map[string]int{"f": 0},
	}

	results, _, err := interp.Execute(mod, "f", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 99 {
		t.Errorf("expected [99], got %v", results)
	}
}

func TestExecuteDrop(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// i64.const 1, i64.const 2, drop, return -> should return 1
	var body []byte
	body = append(body, BuildI64Const(1)...)
	body = append(body, BuildI64Const(2)...)
	body = append(body, byte(Drop))
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	results, _, err := interp.Execute(mod, "f", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 1 {
		t.Errorf("expected [1], got %v", results)
	}
}

func TestExecuteSelect(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// Select with condition=1 should pick first operand.
	// i64.const 10, i64.const 20, i64.const 1, select -> 10
	var body []byte
	body = append(body, BuildI64Const(10)...)
	body = append(body, BuildI64Const(20)...)
	body = append(body, BuildI64Const(1)...)
	body = append(body, byte(Select))
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	results, _, err := interp.Execute(mod, "f", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 10 {
		t.Errorf("select(10, 20, 1): expected [10], got %v", results)
	}

	// Select with condition=0 should pick second operand.
	var body2 []byte
	body2 = append(body2, BuildI64Const(10)...)
	body2 = append(body2, BuildI64Const(20)...)
	body2 = append(body2, BuildI64Const(0)...)
	body2 = append(body2, byte(Select))
	body2 = append(body2, byte(Return))

	mod2 := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body2, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	results, _, err = interp.Execute(mod2, "f", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 20 {
		t.Errorf("select(10, 20, 0): expected [20], got %v", results)
	}
}

func TestExecuteOutOfGas(t *testing.T) {
	interp := NewEWASMInterpreter(2) // very low gas limit

	var body []byte
	body = append(body, BuildI64Const(1)...)
	body = append(body, BuildI64Const(2)...)
	body = append(body, byte(I64Add))
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	_, _, err := interp.Execute(mod, "f", nil)
	if err != ErrEWASMOutOfGas {
		t.Errorf("expected ErrEWASMOutOfGas, got %v", err)
	}
}

func TestExecuteStackUnderflow(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// i64.add with empty stack
	body := []byte{byte(I64Add)}
	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	_, _, err := interp.Execute(mod, "f", nil)
	if err != ErrEWASMStackUnderflow {
		t.Errorf("expected ErrEWASMStackUnderflow, got %v", err)
	}
}

func TestExecuteParamCountMismatch(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{
			Name:       "f",
			ParamTypes: []byte{0x7E, 0x7E}, // expects 2 params
			Body:       []byte{byte(Return)},
			NumLocals:  4,
		}},
		funcIndex: map[string]int{"f": 0},
	}

	_, _, err := interp.Execute(mod, "f", []uint64{1}) // only 1 arg
	if !errors.Is(err, ErrEWASMBadParamCount) {
		t.Errorf("expected ErrEWASMBadParamCount, got %v", err)
	}
}

func TestExecuteLocalOutOfBounds(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// local.get 99 (out of bounds)
	body := BuildLocalGet(99)
	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	_, _, err := interp.Execute(mod, "f", nil)
	if err != ErrEWASMLocalOOB {
		t.Errorf("expected ErrEWASMLocalOOB, got %v", err)
	}
}

func TestExecuteEmptyReturn(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	body := []byte{byte(Return)}
	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	results, gas, err := interp.Execute(mod, "f", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected no results, got %v", results)
	}
	if gas != 1 {
		t.Errorf("expected 1 gas for Return, got %d", gas)
	}
}

func TestExecuteImplicitReturn(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// No explicit Return, falls through.
	body := BuildI64Const(77)
	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	results, _, err := interp.Execute(mod, "f", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 77 {
		t.Errorf("expected [77], got %v", results)
	}
}

func TestExecuteBrIfTaken(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// Push result, push non-zero condition, br_if -> returns result
	var body []byte
	body = append(body, BuildI64Const(55)...)
	body = append(body, BuildI64Const(1)...) // nonzero condition
	body = append(body, byte(BrIf))
	// Should not reach here.
	body = append(body, BuildI64Const(99)...)
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	results, _, err := interp.Execute(mod, "f", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 55 {
		t.Errorf("expected [55] (branch taken), got %v", results)
	}
}

func TestExecuteBrIfNotTaken(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// Push value, push zero condition, br_if (not taken), push another value, return
	var body []byte
	body = append(body, BuildI64Const(55)...)
	body = append(body, BuildI64Const(0)...) // zero condition -> not taken
	body = append(body, byte(BrIf))
	body = append(body, BuildI64Const(99)...)
	body = append(body, byte(I64Add))
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	results, _, err := interp.Execute(mod, "f", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 154 { // 55 + 99
		t.Errorf("expected [154] (branch not taken, 55+99), got %v", results)
	}
}

// --- Host function tests ---

func TestRegisterAndCallHostFunc(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	blockNumber := uint64(12345)
	interp.RegisterHostFunc("ethereum_getBlockNumber", func(args []uint64) ([]uint64, error) {
		return []uint64{blockNumber}, nil
	})

	results, err := interp.ExecuteHostCall("ethereum_getBlockNumber", nil)
	if err != nil {
		t.Fatalf("ExecuteHostCall: %v", err)
	}
	if len(results) != 1 || results[0] != blockNumber {
		t.Errorf("expected [%d], got %v", blockNumber, results)
	}
}

func TestHostFuncNotFound(t *testing.T) {
	interp := NewEWASMInterpreter(1000)
	_, err := interp.ExecuteHostCall("nonexistent", nil)
	if !errors.Is(err, ErrEWASMNoHostFunc) {
		t.Errorf("expected ErrEWASMNoHostFunc, got %v", err)
	}
}

func TestHostFuncWithArgs(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// Host function that adds two args.
	interp.RegisterHostFunc("test_add", func(args []uint64) ([]uint64, error) {
		if len(args) < 2 {
			return nil, errors.New("need 2 args")
		}
		return []uint64{args[0] + args[1]}, nil
	})

	results, err := interp.ExecuteHostCall("test_add", []uint64{3, 4})
	if err != nil {
		t.Fatalf("ExecuteHostCall: %v", err)
	}
	if results[0] != 7 {
		t.Errorf("expected 7, got %d", results[0])
	}
}

// --- Gas metering tests ---

func TestGasMeteringAccuracy(t *testing.T) {
	interp := NewEWASMInterpreter(100)

	// i64.const (1 gas) + i64.const (1 gas) + i64.add (1 gas) + return (1 gas) = 4
	var body []byte
	body = append(body, BuildI64Const(1)...)
	body = append(body, BuildI64Const(2)...)
	body = append(body, byte(I64Add))
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	_, gas, err := interp.Execute(mod, "f", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gas != 4 { // 2 consts + 1 add + 1 return
		t.Errorf("expected 4 gas, got %d", gas)
	}
}

func TestGasLocalOps(t *testing.T) {
	interp := NewEWASMInterpreter(100)

	// i64.const(1) + local.set(3) + local.get(3) + return(1) = 8
	var body []byte
	body = append(body, BuildI64Const(42)...)
	body = append(body, BuildLocalSet(0)...)
	body = append(body, BuildLocalGet(0)...)
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	_, gas, err := interp.Execute(mod, "f", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// 1 (const) + 3 (local.set) + 3 (local.get) + 1 (return) = 8
	if gas != 8 {
		t.Errorf("expected 8 gas, got %d", gas)
	}
}

// --- Compile and execute end-to-end ---

func TestCompileAndExecuteEndToEnd(t *testing.T) {
	c := NewEWASMCompiler()
	interp := NewEWASMInterpreter(10000)

	// EVM: PUSH1 10, PUSH1 20, ADD
	evm := []byte{0x60, 0x0A, 0x60, 0x14, 0x01}
	mod, err := c.Compile(evm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	err = c.Validate(mod)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	results, gas, err := interp.Execute(mod, "main", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 30 { // 10 + 20
		t.Errorf("expected [30], got %v", results)
	}
	if gas == 0 {
		t.Error("expected non-zero gas")
	}
}

func TestCompileAndExecuteMul(t *testing.T) {
	c := NewEWASMCompiler()
	interp := NewEWASMInterpreter(10000)

	// EVM: PUSH1 6, PUSH1 7, MUL
	evm := []byte{0x60, 0x06, 0x60, 0x07, 0x02}
	mod, err := c.Compile(evm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	results, _, err := interp.Execute(mod, "main", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0] != 42 {
		t.Errorf("expected [42], got %v", results)
	}
}

// --- Builder helpers ---

func TestBuildI64Const(t *testing.T) {
	buf := BuildI64Const(0x0102030405060708)
	if len(buf) != 9 {
		t.Fatalf("expected 9 bytes, got %d", len(buf))
	}
	if EWASMOpcode(buf[0]) != I64Const {
		t.Errorf("expected I64Const opcode, got 0x%02x", buf[0])
	}
	val := leU64(buf[1:])
	if val != 0x0102030405060708 {
		t.Errorf("expected 0x0102030405060708, got 0x%X", val)
	}
}

func TestBuildLocalGetSet(t *testing.T) {
	get := BuildLocalGet(5)
	if len(get) != 5 {
		t.Fatalf("LocalGet: expected 5 bytes, got %d", len(get))
	}
	if EWASMOpcode(get[0]) != LocalGet {
		t.Errorf("expected LocalGet opcode")
	}
	if leU32(get[1:]) != 5 {
		t.Errorf("expected index 5, got %d", leU32(get[1:]))
	}

	set := BuildLocalSet(3)
	if len(set) != 5 {
		t.Fatalf("LocalSet: expected 5 bytes, got %d", len(set))
	}
	if EWASMOpcode(set[0]) != LocalSet {
		t.Errorf("expected LocalSet opcode")
	}
	if leU32(set[1:]) != 3 {
		t.Errorf("expected index 3, got %d", leU32(set[1:]))
	}
}

// --- Opcode constants ---

func TestOpcodeConstants(t *testing.T) {
	// Spot-check important opcodes match WebAssembly spec values.
	checks := []struct {
		op   EWASMOpcode
		val  byte
		name string
	}{
		{I32Const, 0x41, "I32Const"},
		{I64Const, 0x42, "I64Const"},
		{I32Add, 0x6A, "I32Add"},
		{I64Add, 0x7C, "I64Add"},
		{LocalGet, 0x20, "LocalGet"},
		{LocalSet, 0x21, "LocalSet"},
		{Call, 0x10, "Call"},
		{Return, 0x0F, "Return"},
		{Drop, 0x1A, "Drop"},
		{Select, 0x1B, "Select"},
	}
	for _, tc := range checks {
		if byte(tc.op) != tc.val {
			t.Errorf("%s: expected 0x%02x, got 0x%02x", tc.name, tc.val, byte(tc.op))
		}
	}
}

func TestOpcodeGasCosts(t *testing.T) {
	if opcodeGas(I64Add) != gasSimpleOp {
		t.Errorf("I64Add gas: expected %d, got %d", gasSimpleOp, opcodeGas(I64Add))
	}
	if opcodeGas(LocalGet) != gasMemoryOp {
		t.Errorf("LocalGet gas: expected %d, got %d", gasMemoryOp, opcodeGas(LocalGet))
	}
	if opcodeGas(Call) != gasHostCall {
		t.Errorf("Call gas: expected %d, got %d", gasHostCall, opcodeGas(Call))
	}
	if opcodeGas(I64Const) != gasConstOp {
		t.Errorf("I64Const gas: expected %d, got %d", gasConstOp, opcodeGas(I64Const))
	}
}

// --- I32 operation tests ---

func TestExecuteI32Operations(t *testing.T) {
	interp := NewEWASMInterpreter(1000)

	// I32Add: 10 + 20 = 30
	var body []byte
	body = append(body, BuildI64Const(10)...)
	body = append(body, BuildI64Const(20)...)
	body = append(body, byte(I32Add))
	body = append(body, byte(Return))

	mod := &EWASMModule{
		Functions: []*EWASMFunction{{Name: "f", Body: body, NumLocals: 4}},
		funcIndex: map[string]int{"f": 0},
	}

	results, _, err := interp.Execute(mod, "f", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if results[0] != 30 {
		t.Errorf("I32Add: expected 30, got %d", results[0])
	}
}
