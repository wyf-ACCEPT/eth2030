package zkvm

import (
	"errors"
	"fmt"
)

// eWASM errors.
var (
	ErrEWASMEmptyBytecode  = errors.New("ewasm: empty bytecode")
	ErrEWASMInvalidOpcode  = errors.New("ewasm: invalid opcode")
	ErrEWASMOutOfGas       = errors.New("ewasm: out of gas")
	ErrEWASMStackUnderflow = errors.New("ewasm: stack underflow")
	ErrEWASMStackOverflow  = errors.New("ewasm: stack overflow")
	ErrEWASMInvalidModule  = errors.New("ewasm: invalid module")
	ErrEWASMNoFunction     = errors.New("ewasm: function not found")
	ErrEWASMNoHostFunc     = errors.New("ewasm: host function not found")
	ErrEWASMLocalOOB       = errors.New("ewasm: local index out of bounds")
	ErrEWASMEmptyFunctions = errors.New("ewasm: module has no functions")
	ErrEWASMEmptyBody      = errors.New("ewasm: function has empty body")
	ErrEWASMBadParamCount  = errors.New("ewasm: argument count mismatch")
)

// EWASMOpcode represents a WebAssembly-style opcode for the eWASM IR.
type EWASMOpcode byte

// eWASM opcode constants mapping to WebAssembly numeric instructions.
const (
	I32Const EWASMOpcode = 0x41
	I64Const EWASMOpcode = 0x42
	I32Add   EWASMOpcode = 0x6A
	I64Add   EWASMOpcode = 0x7C
	I32Sub   EWASMOpcode = 0x6B
	I64Sub   EWASMOpcode = 0x7D
	I32Mul   EWASMOpcode = 0x6C
	I64Mul   EWASMOpcode = 0x7E
	I32And   EWASMOpcode = 0x71
	I64And   EWASMOpcode = 0x83
	I32Or    EWASMOpcode = 0x72
	I64Or    EWASMOpcode = 0x84
	LocalGet EWASMOpcode = 0x20
	LocalSet EWASMOpcode = 0x21
	Call     EWASMOpcode = 0x10
	Return   EWASMOpcode = 0x0F
	Block    EWASMOpcode = 0x02
	Loop     EWASMOpcode = 0x03
	BrIf     EWASMOpcode = 0x0D
	Drop     EWASMOpcode = 0x1A
	Select   EWASMOpcode = 0x1B
)

// Gas costs per opcode category.
const (
	gasSimpleOp  uint64 = 1
	gasMemoryOp  uint64 = 3
	gasHostCall  uint64 = 10
	gasConstOp   uint64 = 1
	gasControlOp uint64 = 1
	maxStackSize        = 1024
)

// opcodeGas returns the gas cost for an opcode.
func opcodeGas(op EWASMOpcode) uint64 {
	switch op {
	case LocalGet, LocalSet:
		return gasMemoryOp
	case Call:
		return gasHostCall
	case I32Const, I64Const:
		return gasConstOp
	case Block, Loop, BrIf, Return:
		return gasControlOp
	default:
		return gasSimpleOp
	}
}

// HostFunc is a callback for host-provided functions (e.g., ethereum_*).
type HostFunc func(args []uint64) ([]uint64, error)

// EWASMFunction represents a single function in an eWASM module.
type EWASMFunction struct {
	Name        string
	ParamTypes  []byte // value type per param (0x7F=i32, 0x7E=i64)
	ReturnTypes []byte // value type per return
	Body        []byte // bytecode: opcode followed by immediate bytes
	NumLocals   uint32
}

// EWASMModule represents a compiled eWASM module.
type EWASMModule struct {
	Functions  []*EWASMFunction
	Memory     []byte   // linear memory
	Globals    []uint64 // global variables
	funcIndex  map[string]int
	MemorySize uint32 // pages (64 KiB each)
}

// GetFunction returns a function by name, or nil if not found.
func (m *EWASMModule) GetFunction(name string) *EWASMFunction {
	idx, ok := m.funcIndex[name]
	if !ok {
		return nil
	}
	return m.Functions[idx]
}

// EWASMCompiler compiles EVM bytecode to eWASM IR.
type EWASMCompiler struct {
	// optimizationLevel controls compilation aggressiveness (0=none, 1=basic).
	optimizationLevel int
}

// NewEWASMCompiler creates a new eWASM compiler.
func NewEWASMCompiler() *EWASMCompiler {
	return &EWASMCompiler{optimizationLevel: 0}
}

// Compile translates EVM bytecode into an eWASM module.
// Each EVM opcode is mapped to a sequence of eWASM instructions.
func (c *EWASMCompiler) Compile(evmBytecode []byte) (*EWASMModule, error) {
	if len(evmBytecode) == 0 {
		return nil, ErrEWASMEmptyBytecode
	}

	body := c.translateBytecode(evmBytecode)

	mainFunc := &EWASMFunction{
		Name:        "main",
		ParamTypes:  nil,
		ReturnTypes: []byte{0x7E}, // returns i64
		Body:        body,
		NumLocals:   16,
	}

	module := &EWASMModule{
		Functions:  []*EWASMFunction{mainFunc},
		Memory:     make([]byte, 65536), // 1 page
		Globals:    make([]uint64, 4),
		funcIndex:  map[string]int{"main": 0},
		MemorySize: 1,
	}

	return module, nil
}

// translateBytecode converts EVM opcodes to eWASM IR bytecode.
// This is a simplified translation; a full compiler would handle
// the complete EVM instruction set with stack-to-register mapping.
func (c *EWASMCompiler) translateBytecode(evm []byte) []byte {
	var body []byte
	for i := 0; i < len(evm); i++ {
		switch evm[i] {
		case 0x01: // ADD -> I64Add
			body = append(body, byte(I64Add))
		case 0x02: // MUL -> I64Mul
			body = append(body, byte(I64Mul))
		case 0x03: // SUB -> I64Sub
			body = append(body, byte(I64Sub))
		case 0x16: // AND -> I64And
			body = append(body, byte(I64And))
		case 0x17: // OR -> I64Or
			body = append(body, byte(I64Or))
		case 0x60: // PUSH1
			if i+1 < len(evm) {
				body = append(body, byte(I64Const))
				// Encode immediate as 8 LE bytes.
				body = append(body, evm[i+1], 0, 0, 0, 0, 0, 0, 0)
				i++
			}
		case 0x50: // POP -> Drop
			body = append(body, byte(Drop))
		default:
			// Unknown opcodes become I64Const 0 (no-op push).
			body = append(body, byte(I64Const))
			body = append(body, 0, 0, 0, 0, 0, 0, 0, 0)
		}
	}
	// Add implicit return.
	body = append(body, byte(Return))
	return body
}

// Validate checks that an eWASM module is well-formed.
func (c *EWASMCompiler) Validate(module *EWASMModule) error {
	if module == nil {
		return ErrEWASMInvalidModule
	}
	if len(module.Functions) == 0 {
		return ErrEWASMEmptyFunctions
	}
	for _, fn := range module.Functions {
		if len(fn.Body) == 0 {
			return fmt.Errorf("%w: %s", ErrEWASMEmptyBody, fn.Name)
		}
		// Validate opcodes in the body.
		for i := 0; i < len(fn.Body); i++ {
			op := EWASMOpcode(fn.Body[i])
			switch op {
			case I64Const, I32Const:
				if i+8 >= len(fn.Body) {
					return fmt.Errorf("%w: truncated const at offset %d", ErrEWASMInvalidOpcode, i)
				}
				i += 8 // skip 8-byte immediate
			case LocalGet, LocalSet:
				if i+4 >= len(fn.Body) {
					return fmt.Errorf("%w: truncated local op at offset %d", ErrEWASMInvalidOpcode, i)
				}
				i += 4 // skip 4-byte local index
			case Call:
				if i+4 >= len(fn.Body) {
					return fmt.Errorf("%w: truncated call at offset %d", ErrEWASMInvalidOpcode, i)
				}
				i += 4 // skip 4-byte function index
			case I32Add, I64Add, I32Sub, I64Sub, I32Mul, I64Mul,
				I32And, I64And, I32Or, I64Or,
				Return, Block, Loop, BrIf, Drop, Select:
				// No immediates.
			default:
				return fmt.Errorf("%w: 0x%02x at offset %d", ErrEWASMInvalidOpcode, op, i)
			}
		}
	}
	return nil
}

// EWASMInterpreter executes eWASM modules with gas metering.
type EWASMInterpreter struct {
	gasLimit  uint64
	hostFuncs map[string]HostFunc
}

// NewEWASMInterpreter creates a new interpreter with a gas limit.
func NewEWASMInterpreter(gasLimit uint64) *EWASMInterpreter {
	return &EWASMInterpreter{
		gasLimit:  gasLimit,
		hostFuncs: make(map[string]HostFunc),
	}
}

// RegisterHostFunc registers a host function by name.
func (interp *EWASMInterpreter) RegisterHostFunc(name string, fn HostFunc) {
	interp.hostFuncs[name] = fn
}

// Execute runs the named function in the module with the given arguments.
// Returns the result values and gas used, or an error.
func (interp *EWASMInterpreter) Execute(module *EWASMModule, entryFunc string, args []uint64) ([]uint64, uint64, error) {
	if module == nil {
		return nil, 0, ErrEWASMInvalidModule
	}
	fn := module.GetFunction(entryFunc)
	if fn == nil {
		return nil, 0, fmt.Errorf("%w: %s", ErrEWASMNoFunction, entryFunc)
	}

	// Validate argument count against param types.
	if len(args) != len(fn.ParamTypes) {
		return nil, 0, fmt.Errorf("%w: want %d, got %d", ErrEWASMBadParamCount, len(fn.ParamTypes), len(args))
	}

	// Initialize locals: params first, then zero-filled locals.
	numLocals := int(fn.NumLocals)
	if numLocals < len(args) {
		numLocals = len(args)
	}
	locals := make([]uint64, numLocals)
	copy(locals, args)

	// Value stack.
	stack := make([]uint64, 0, 64)
	var gasUsed uint64

	body := fn.Body
	pc := 0

	for pc < len(body) {
		op := EWASMOpcode(body[pc])
		cost := opcodeGas(op)
		gasUsed += cost
		if gasUsed > interp.gasLimit {
			return nil, gasUsed, ErrEWASMOutOfGas
		}

		switch op {
		case I64Const:
			if pc+8 >= len(body) {
				return nil, gasUsed, ErrEWASMInvalidOpcode
			}
			val := leU64(body[pc+1 : pc+9])
			stack = append(stack, val)
			if len(stack) > maxStackSize {
				return nil, gasUsed, ErrEWASMStackOverflow
			}
			pc += 9
			continue

		case I32Const:
			if pc+8 >= len(body) {
				return nil, gasUsed, ErrEWASMInvalidOpcode
			}
			val := uint64(leU32(body[pc+1 : pc+5]))
			stack = append(stack, val)
			if len(stack) > maxStackSize {
				return nil, gasUsed, ErrEWASMStackOverflow
			}
			pc += 9
			continue

		case I64Add, I32Add, I64Sub, I32Sub, I64Mul, I32Mul,
			I64And, I32And, I64Or, I32Or:
			if len(stack) < 2 {
				return nil, gasUsed, ErrEWASMStackUnderflow
			}
			b, a := stack[len(stack)-1], stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			switch op {
			case I64Add, I32Add:
				stack = append(stack, a+b)
			case I64Sub, I32Sub:
				stack = append(stack, a-b)
			case I64Mul, I32Mul:
				stack = append(stack, a*b)
			case I64And, I32And:
				stack = append(stack, a&b)
			case I64Or, I32Or:
				stack = append(stack, a|b)
			}

		case LocalGet:
			if pc+4 >= len(body) {
				return nil, gasUsed, ErrEWASMInvalidOpcode
			}
			idx := leU32(body[pc+1 : pc+5])
			if int(idx) >= len(locals) {
				return nil, gasUsed, ErrEWASMLocalOOB
			}
			stack = append(stack, locals[idx])
			if len(stack) > maxStackSize {
				return nil, gasUsed, ErrEWASMStackOverflow
			}
			pc += 5
			continue

		case LocalSet:
			if pc+4 >= len(body) {
				return nil, gasUsed, ErrEWASMInvalidOpcode
			}
			idx := leU32(body[pc+1 : pc+5])
			if int(idx) >= len(locals) {
				return nil, gasUsed, ErrEWASMLocalOOB
			}
			if len(stack) < 1 {
				return nil, gasUsed, ErrEWASMStackUnderflow
			}
			locals[idx] = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			pc += 5
			continue

		case Drop:
			if len(stack) < 1 {
				return nil, gasUsed, ErrEWASMStackUnderflow
			}
			stack = stack[:len(stack)-1]

		case Select:
			if len(stack) < 3 {
				return nil, gasUsed, ErrEWASMStackUnderflow
			}
			cond := stack[len(stack)-1]
			val2 := stack[len(stack)-2]
			val1 := stack[len(stack)-3]
			stack = stack[:len(stack)-3]
			if cond != 0 {
				stack = append(stack, val1)
			} else {
				stack = append(stack, val2)
			}

		case Return:
			// Return top of stack as results.
			if len(stack) > 0 {
				return stack[len(stack)-1:], gasUsed, nil
			}
			return nil, gasUsed, nil

		case Block, Loop:
			// Structured control: skip for now (nop).

		case BrIf:
			// Conditional branch: pop condition; if nonzero, jump to end.
			if len(stack) < 1 {
				return nil, gasUsed, ErrEWASMStackUnderflow
			}
			cond := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if cond != 0 {
				// Jump to end (simplified: return).
				if len(stack) > 0 {
					return stack[len(stack)-1:], gasUsed, nil
				}
				return nil, gasUsed, nil
			}

		case Call:
			if pc+4 >= len(body) {
				return nil, gasUsed, ErrEWASMInvalidOpcode
			}
			_ = leU32(body[pc+1 : pc+5])
			// Host function call by index not implemented in simple mode.
			pc += 5
			continue

		default:
			return nil, gasUsed, fmt.Errorf("%w: 0x%02x at pc=%d", ErrEWASMInvalidOpcode, op, pc)
		}

		pc++
	}

	// Implicit return at end of function.
	if len(stack) > 0 {
		return stack[len(stack)-1:], gasUsed, nil
	}
	return nil, gasUsed, nil
}

// ExecuteHostCall executes a registered host function by name.
func (interp *EWASMInterpreter) ExecuteHostCall(name string, args []uint64) ([]uint64, error) {
	fn, ok := interp.hostFuncs[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrEWASMNoHostFunc, name)
	}
	return fn(args)
}

// leU64 reads a little-endian uint64 from 8 bytes.
func leU64(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

// leU32 reads a little-endian uint32 from 4 bytes.
func leU32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func putLeU64(b []byte, v uint64) {
	b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
	b[4], b[5], b[6], b[7] = byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56)
}

func putLeU32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
}

// BuildI64Const builds an I64Const instruction with an immediate value.
func BuildI64Const(val uint64) []byte {
	buf := make([]byte, 9)
	buf[0] = byte(I64Const)
	putLeU64(buf[1:], val)
	return buf
}

// BuildLocalGet builds a LocalGet instruction for the given index.
func BuildLocalGet(idx uint32) []byte {
	buf := make([]byte, 5)
	buf[0] = byte(LocalGet)
	putLeU32(buf[1:], idx)
	return buf
}

// BuildLocalSet builds a LocalSet instruction for the given index.
func BuildLocalSet(idx uint32) []byte {
	buf := make([]byte, 5)
	buf[0] = byte(LocalSet)
	putLeU32(buf[1:], idx)
	return buf
}
