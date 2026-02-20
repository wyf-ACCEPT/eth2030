// Package vm implements the Ethereum Virtual Machine.
//
// ewasm_interpreter.go provides a stack-based WASM interpreter for executing
// simple WASM instruction sequences. Part of the J+/K+ roadmap: "more
// precompiles in eWASM", "STF in eRISC", "canonical guest".
package vm

import (
	"errors"
	"fmt"
)

// eWASM interpreter errors (Interp-prefixed to avoid collision with ewasm_engine).
var (
	ErrInterpStackOverflow   = errors.New("ewasm-interp: stack overflow")
	ErrInterpStackUnderflow  = errors.New("ewasm-interp: stack underflow")
	ErrInterpDivisionByZero  = errors.New("ewasm-interp: division by zero")
	ErrInterpOutOfGas        = errors.New("ewasm-interp: out of gas")
	ErrInterpUnknownOpcode   = errors.New("ewasm-interp: unknown opcode")
	ErrInterpLocalOutOfRange = errors.New("ewasm-interp: local index out of range")
	ErrInterpUnreachable     = errors.New("ewasm-interp: unreachable executed")
	ErrInterpInvalidSelect   = errors.New("ewasm-interp: select requires i32 condition")
)

// WASM opcodes supported by the interpreter (InterpOp-prefixed to avoid collision).
const (
	InterpOpUnreachable byte = 0x00
	InterpOpNop         byte = 0x01
	InterpOpReturn      byte = 0x0F
	InterpOpDrop        byte = 0x1A
	InterpOpSelect      byte = 0x1B
	InterpOpLocalGet    byte = 0x20
	InterpOpLocalSet    byte = 0x21
	InterpOpI32Const    byte = 0x41
	InterpOpI32Eq       byte = 0x46
	InterpOpI32LtU      byte = 0x48
	InterpOpI32GtU      byte = 0x4A
	InterpOpI32Add      byte = 0x6A
	InterpOpI32Sub      byte = 0x6B
	InterpOpI32Mul      byte = 0x6C
	InterpOpI32DivU     byte = 0x6E
)

// WasmValueType distinguishes typed WASM values.
type WasmValueType byte

const (
	WasmTypeI32 WasmValueType = 0x7F
	WasmTypeI64 WasmValueType = 0x7E
	WasmTypeF32 WasmValueType = 0x7D
	WasmTypeF64 WasmValueType = 0x7C
)

// WasmValue is a typed value on the WASM stack.
type WasmValue struct {
	Type WasmValueType
	I32  int32
	I64  int64
	F32  float32
	F64  float64
}

// I32Val creates an i32 WasmValue.
func I32Val(v int32) WasmValue {
	return WasmValue{Type: WasmTypeI32, I32: v}
}

// I64Val creates an i64 WasmValue.
func I64Val(v int64) WasmValue {
	return WasmValue{Type: WasmTypeI64, I64: v}
}

// WasmInstruction represents a single decoded WASM instruction with immediates.
type WasmInstruction struct {
	Opcode    byte
	Immediate int64 // immediate operand (e.g., constant value, local index)
}

// EWASMInterpreterConfig configures the stack-based interpreter.
type EWASMInterpreterConfig struct {
	MaxMemoryPages uint32 // max linear memory pages (64 KiB each)
	MaxStackDepth  uint32 // max operand stack depth
	GasLimit       uint64 // total gas available
}

// DefaultEWASMInterpreterConfig returns sensible defaults.
func DefaultEWASMInterpreterConfig() EWASMInterpreterConfig {
	return EWASMInterpreterConfig{
		MaxMemoryPages: 256,
		MaxStackDepth:  1024,
		GasLimit:       1_000_000,
	}
}

// EWASMInterpreter is a minimal stack-based WASM interpreter.
type EWASMInterpreter struct {
	config  EWASMInterpreterConfig
	stack   []WasmValue
	locals  []WasmValue
	gasUsed uint64
}

// NewEWASMInterpreter creates a new interpreter with the given config.
func NewEWASMInterpreter(config EWASMInterpreterConfig) *EWASMInterpreter {
	if config.MaxStackDepth == 0 {
		config.MaxStackDepth = 1024
	}
	if config.GasLimit == 0 {
		config.GasLimit = 1_000_000
	}
	return &EWASMInterpreter{
		config: config,
		stack:  make([]WasmValue, 0, 64),
		locals: nil,
	}
}

// GasUsed returns the gas consumed by the last execution.
func (interp *EWASMInterpreter) GasUsed() uint64 {
	return interp.gasUsed
}

// Reset clears the interpreter state for reuse.
func (interp *EWASMInterpreter) Reset() {
	interp.stack = interp.stack[:0]
	interp.locals = nil
	interp.gasUsed = 0
}

// push pushes a value onto the operand stack.
func (interp *EWASMInterpreter) push(v WasmValue) error {
	if uint32(len(interp.stack)) >= interp.config.MaxStackDepth {
		return ErrInterpStackOverflow
	}
	interp.stack = append(interp.stack, v)
	return nil
}

// pop removes and returns the top value from the operand stack.
func (interp *EWASMInterpreter) pop() (WasmValue, error) {
	if len(interp.stack) == 0 {
		return WasmValue{}, ErrInterpStackUnderflow
	}
	v := interp.stack[len(interp.stack)-1]
	interp.stack = interp.stack[:len(interp.stack)-1]
	return v, nil
}

// useGas charges gas and returns an error if the limit is exceeded.
func (interp *EWASMInterpreter) useGas(amount uint64) error {
	interp.gasUsed += amount
	if interp.gasUsed > interp.config.GasLimit {
		return ErrInterpOutOfGas
	}
	return nil
}

// Execute runs a sequence of WASM instructions. Args are set as locals.
// Returns the values remaining on the stack, gas used, and any error.
func (interp *EWASMInterpreter) Execute(instructions []WasmInstruction, args []WasmValue) ([]WasmValue, uint64, error) {
	interp.Reset()

	// Initialize locals from arguments.
	interp.locals = make([]WasmValue, len(args))
	copy(interp.locals, args)

	for pc := 0; pc < len(instructions); pc++ {
		inst := instructions[pc]

		// Charge base gas per instruction.
		if err := interp.useGas(1); err != nil {
			return nil, interp.gasUsed, err
		}

		switch inst.Opcode {
		case InterpOpNop:
			// No operation.

		case InterpOpUnreachable:
			return nil, interp.gasUsed, ErrInterpUnreachable

		case InterpOpReturn:
			// Return remaining stack contents.
			result := make([]WasmValue, len(interp.stack))
			copy(result, interp.stack)
			return result, interp.gasUsed, nil

		case InterpOpI32Const:
			if err := interp.push(I32Val(int32(inst.Immediate))); err != nil {
				return nil, interp.gasUsed, err
			}

		case InterpOpI32Add:
			b, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			a, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			if err := interp.push(I32Val(a.I32 + b.I32)); err != nil {
				return nil, interp.gasUsed, err
			}

		case InterpOpI32Sub:
			b, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			a, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			if err := interp.push(I32Val(a.I32 - b.I32)); err != nil {
				return nil, interp.gasUsed, err
			}

		case InterpOpI32Mul:
			b, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			a, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			if err := interp.useGas(2); err != nil { // mul costs extra
				return nil, interp.gasUsed, err
			}
			if err := interp.push(I32Val(a.I32 * b.I32)); err != nil {
				return nil, interp.gasUsed, err
			}

		case InterpOpI32DivU:
			b, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			a, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			if b.I32 == 0 {
				return nil, interp.gasUsed, ErrInterpDivisionByZero
			}
			if err := interp.useGas(2); err != nil { // div costs extra
				return nil, interp.gasUsed, err
			}
			// Unsigned division: treat as uint32.
			result := uint32(a.I32) / uint32(b.I32)
			if err := interp.push(I32Val(int32(result))); err != nil {
				return nil, interp.gasUsed, err
			}

		case InterpOpI32Eq:
			b, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			a, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			var r int32
			if a.I32 == b.I32 {
				r = 1
			}
			if err := interp.push(I32Val(r)); err != nil {
				return nil, interp.gasUsed, err
			}

		case InterpOpI32LtU:
			b, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			a, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			var r int32
			if uint32(a.I32) < uint32(b.I32) {
				r = 1
			}
			if err := interp.push(I32Val(r)); err != nil {
				return nil, interp.gasUsed, err
			}

		case InterpOpI32GtU:
			b, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			a, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			var r int32
			if uint32(a.I32) > uint32(b.I32) {
				r = 1
			}
			if err := interp.push(I32Val(r)); err != nil {
				return nil, interp.gasUsed, err
			}

		case InterpOpLocalGet:
			idx := int(inst.Immediate)
			if idx < 0 || idx >= len(interp.locals) {
				return nil, interp.gasUsed, fmt.Errorf("%w: %d", ErrInterpLocalOutOfRange, idx)
			}
			if err := interp.push(interp.locals[idx]); err != nil {
				return nil, interp.gasUsed, err
			}

		case InterpOpLocalSet:
			idx := int(inst.Immediate)
			if idx < 0 || idx >= len(interp.locals) {
				return nil, interp.gasUsed, fmt.Errorf("%w: %d", ErrInterpLocalOutOfRange, idx)
			}
			v, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			interp.locals[idx] = v

		case InterpOpDrop:
			_, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}

		case InterpOpSelect:
			// select: [val1, val2, cond] -> [val1 if cond != 0 else val2]
			cond, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			val2, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			val1, err := interp.pop()
			if err != nil {
				return nil, interp.gasUsed, err
			}
			if cond.I32 != 0 {
				if err := interp.push(val1); err != nil {
					return nil, interp.gasUsed, err
				}
			} else {
				if err := interp.push(val2); err != nil {
					return nil, interp.gasUsed, err
				}
			}

		default:
			return nil, interp.gasUsed, fmt.Errorf("%w: 0x%02x", ErrInterpUnknownOpcode, inst.Opcode)
		}
	}

	// Implicit return: return stack contents.
	result := make([]WasmValue, len(interp.stack))
	copy(result, interp.stack)
	return result, interp.gasUsed, nil
}
