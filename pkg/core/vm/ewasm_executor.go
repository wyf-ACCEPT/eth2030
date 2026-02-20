// eWASM bytecode interpreter for Ethereum (EL roadmap: "canonical guest",
// "more precompiles in eWASM", "STF in eRISC"). Provides a stack-based WASM
// execution engine with gas metering, memory bounds checking, and structured
// control flow (block/loop/br/br_if).
package vm

import (
	"encoding/binary"
	"errors"
	"math"
)

// WASMOpcode defines a supported WASM bytecode instruction.
type WASMOpcode byte

const (
	WASMOpcodeUnreachable WASMOpcode = 0x00
	WASMOpcodeNop         WASMOpcode = 0x01
	WASMOpcodeBlock       WASMOpcode = 0x02
	WASMOpcodeLoop        WASMOpcode = 0x03
	WASMOpcodeBr          WASMOpcode = 0x0C
	WASMOpcodeBrIf        WASMOpcode = 0x0D
	WASMOpcodeReturn      WASMOpcode = 0x0F
	WASMOpcodeCall        WASMOpcode = 0x10
	WASMOpcodeEnd         WASMOpcode = 0x0B
	WASMOpcodeLocalGet    WASMOpcode = 0x20
	WASMOpcodeLocalSet    WASMOpcode = 0x21
	WASMOpcodeI32Load     WASMOpcode = 0x28
	WASMOpcodeI32Store    WASMOpcode = 0x36
	WASMOpcodeI32Const    WASMOpcode = 0x41
	WASMOpcodeI32Add      WASMOpcode = 0x6A
	WASMOpcodeI32Sub      WASMOpcode = 0x6B
	WASMOpcodeI32Mul      WASMOpcode = 0x6C
	WASMOpcodeI32DivS     WASMOpcode = 0x6D
)

// Execution errors for the WASM executor.
var (
	ErrWASMStackOverflow  = errors.New("wasm-executor: stack overflow")
	ErrWASMStackUnderflow = errors.New("wasm-executor: stack underflow")
	ErrWASMOutOfMemory    = errors.New("wasm-executor: out of memory bounds")
	ErrWASMDivisionByZero = errors.New("wasm-executor: division by zero")
	ErrWASMUnreachable    = errors.New("wasm-executor: unreachable executed")
	ErrWASMGasExhausted   = errors.New("wasm-executor: gas exhausted")
	errWASMInvalidOpcode  = errors.New("wasm-executor: invalid opcode")
	errWASMNoFunction     = errors.New("wasm-executor: function not found")
	errWASMBadBytecode    = errors.New("wasm-executor: malformed bytecode")
	errWASMCallDepth      = errors.New("wasm-executor: call stack depth exceeded")
	errWASMBranchDepth    = errors.New("wasm-executor: branch depth exceeds block nesting")
)

// WASMExecutorConfig holds configuration for the WASM executor.
type WASMExecutorConfig struct {
	MaxMemoryPages uint32 // each page = 65536 bytes
	MaxStackDepth  uint32 // maximum operand stack depth
	GasLimit       uint64 // total gas available
	DebugMode      bool   // enable debug tracing
	MaxCallDepth   uint32 // maximum call stack depth
}

// DefaultWASMExecutorConfig returns a reasonable default configuration.
func DefaultWASMExecutorConfig() WASMExecutorConfig {
	return WASMExecutorConfig{
		MaxMemoryPages: 16,
		MaxStackDepth:  1024,
		GasLimit:       1_000_000,
		MaxCallDepth:   64,
	}
}

// WASMFrame represents a call frame on the call stack.
type WASMFrame struct {
	FuncIndex  uint32   // index of the function being executed
	Locals     []uint64 // local variable slots
	ReturnPC   int      // program counter to return to
	StackBase  int      // operand stack depth at frame entry
}

// executorFunc represents a parsed function body.
type executorFunc struct {
	Name     string
	Code     []byte
	NumArgs  int
	NumLocal int
}

// controlEntry tracks a block/loop for branch resolution.
type controlEntry struct {
	opcode   WASMOpcode // Block or Loop
	startPC  int        // PC of the block/loop instruction
	endPC    int        // PC of the matching End instruction (-1 if unknown)
}

// WASMExecutor is a stack-based WASM bytecode interpreter with gas metering.
type WASMExecutor struct {
	config   WASMExecutorConfig
	memory   []byte
	stack    []uint64
	sp       int // stack pointer (next free slot)
	gasUsed  uint64
	frames   []WASMFrame
	funcs    []executorFunc
	controls []controlEntry
}

// NewWASMExecutor creates a new WASM executor with the given config.
func NewWASMExecutor(config WASMExecutorConfig) *WASMExecutor {
	if config.MaxMemoryPages == 0 {
		config.MaxMemoryPages = 16
	}
	if config.MaxStackDepth == 0 {
		config.MaxStackDepth = 1024
	}
	if config.MaxCallDepth == 0 {
		config.MaxCallDepth = 64
	}
	memSize := uint64(config.MaxMemoryPages) * 65536
	if memSize > 16*1024*1024 {
		memSize = 16 * 1024 * 1024 // cap at 16 MiB
	}
	return &WASMExecutor{
		config: config,
		memory: make([]byte, memSize),
		stack:  make([]uint64, config.MaxStackDepth),
		sp:     0,
		frames: make([]WASMFrame, 0, 16),
	}
}

// Gas costs per instruction category.
const (
	wasmGasConst    uint64 = 1
	wasmGasArith    uint64 = 3
	wasmGasLocal    uint64 = 2
	wasmGasMemory   uint64 = 5
	wasmGasControl  uint64 = 2
	wasmGasCall     uint64 = 10
	wasmGasBranch   uint64 = 3
)

func (e *WASMExecutor) useGas(cost uint64) error {
	e.gasUsed += cost
	if e.gasUsed > e.config.GasLimit {
		return ErrWASMGasExhausted
	}
	return nil
}

func (e *WASMExecutor) push(v uint64) error {
	if e.sp >= int(e.config.MaxStackDepth) {
		return ErrWASMStackOverflow
	}
	e.stack[e.sp] = v
	e.sp++
	return nil
}

func (e *WASMExecutor) pop() (uint64, error) {
	if e.sp <= 0 {
		return 0, ErrWASMStackUnderflow
	}
	e.sp--
	return e.stack[e.sp], nil
}

func (e *WASMExecutor) memLoad32(offset uint32) (uint32, error) {
	end := uint64(offset) + 4
	if end > uint64(len(e.memory)) {
		return 0, ErrWASMOutOfMemory
	}
	return binary.LittleEndian.Uint32(e.memory[offset : offset+4]), nil
}

func (e *WASMExecutor) memStore32(offset uint32, val uint32) error {
	end := uint64(offset) + 4
	if end > uint64(len(e.memory)) {
		return ErrWASMOutOfMemory
	}
	binary.LittleEndian.PutUint32(e.memory[offset:offset+4], val)
	return nil
}

// readLEB128U reads an unsigned LEB128 value from code at pos.
func readLEB128U(code []byte, pos int) (uint32, int, error) {
	var result uint32
	var shift uint
	for i := 0; i < 5; i++ {
		if pos+i >= len(code) {
			return 0, 0, errWASMBadBytecode
		}
		b := code[pos+i]
		result |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, i + 1, nil
		}
		shift += 7
	}
	return 0, 0, errWASMBadBytecode
}

// readLEB128S reads a signed LEB128 value from code at pos.
func readLEB128S(code []byte, pos int) (int32, int, error) {
	var result int32
	var shift uint
	for i := 0; i < 5; i++ {
		if pos+i >= len(code) {
			return 0, 0, errWASMBadBytecode
		}
		b := code[pos+i]
		result |= int32(b&0x7F) << shift
		shift += 7
		if b&0x80 == 0 {
			// Sign extend if needed.
			if shift < 32 && b&0x40 != 0 {
				result |= -(1 << shift)
			}
			return result, i + 1, nil
		}
	}
	return 0, 0, errWASMBadBytecode
}

// RegisterFunction adds a function to the executor's function table.
func (e *WASMExecutor) RegisterFunction(name string, code []byte, numArgs, numLocal int) {
	e.funcs = append(e.funcs, executorFunc{
		Name:     name,
		Code:     code,
		NumArgs:  numArgs,
		NumLocal: numLocal,
	})
}

// findEndPC scans forward from startPC to find the matching End opcode,
// handling nested block/loop/if.
func findEndPC(code []byte, startPC int) int {
	depth := 1
	pc := startPC
	for pc < len(code) {
		op := WASMOpcode(code[pc])
		pc++
		switch op {
		case WASMOpcodeBlock, WASMOpcodeLoop:
			pc++ // skip block type byte
			depth++
		case WASMOpcodeEnd:
			depth--
			if depth == 0 {
				return pc - 1 // points at the End opcode
			}
		case WASMOpcodeI32Const:
			_, n, err := readLEB128S(code, pc)
			if err != nil {
				return len(code)
			}
			pc += n
		case WASMOpcodeLocalGet, WASMOpcodeLocalSet, WASMOpcodeBr, WASMOpcodeBrIf, WASMOpcodeCall:
			_, n, err := readLEB128U(code, pc)
			if err != nil {
				return len(code)
			}
			pc += n
		case WASMOpcodeI32Load, WASMOpcodeI32Store:
			// skip alignment + offset (two LEB128 values)
			_, n1, err1 := readLEB128U(code, pc)
			if err1 != nil {
				return len(code)
			}
			pc += n1
			_, n2, err2 := readLEB128U(code, pc)
			if err2 != nil {
				return len(code)
			}
			pc += n2
		}
	}
	return len(code)
}

// executeFunc runs a single function body to completion.
func (e *WASMExecutor) executeFunc(fi int, args []uint64) error {
	if fi < 0 || fi >= len(e.funcs) {
		return errWASMNoFunction
	}
	fn := e.funcs[fi]
	code := fn.Code

	// Set up locals: args first, then zeros for declared locals.
	locals := make([]uint64, fn.NumArgs+fn.NumLocal)
	for i := 0; i < len(args) && i < fn.NumArgs; i++ {
		locals[i] = args[i]
	}

	frame := WASMFrame{
		FuncIndex: uint32(fi),
		Locals:    locals,
		ReturnPC:  -1,
		StackBase: e.sp,
	}

	if len(e.frames) >= int(e.config.MaxCallDepth) {
		return errWASMCallDepth
	}
	e.frames = append(e.frames, frame)
	e.controls = e.controls[:0]

	pc := 0
	for pc < len(code) {
		op := WASMOpcode(code[pc])
		pc++

		switch op {
		case WASMOpcodeUnreachable:
			if err := e.useGas(wasmGasControl); err != nil {
				return err
			}
			e.frames = e.frames[:len(e.frames)-1]
			return ErrWASMUnreachable

		case WASMOpcodeNop:
			if err := e.useGas(wasmGasConst); err != nil {
				return err
			}

		case WASMOpcodeBlock:
			if err := e.useGas(wasmGasControl); err != nil {
				return err
			}
			pc++ // skip block type
			endPC := findEndPC(code, pc)
			e.controls = append(e.controls, controlEntry{
				opcode:  WASMOpcodeBlock,
				startPC: pc,
				endPC:   endPC,
			})

		case WASMOpcodeLoop:
			if err := e.useGas(wasmGasControl); err != nil {
				return err
			}
			pc++ // skip block type
			endPC := findEndPC(code, pc)
			e.controls = append(e.controls, controlEntry{
				opcode:  WASMOpcodeLoop,
				startPC: pc,
				endPC:   endPC,
			})

		case WASMOpcodeBr:
			if err := e.useGas(wasmGasBranch); err != nil {
				return err
			}
			depth, n, err := readLEB128U(code, pc)
			if err != nil {
				return errWASMBadBytecode
			}
			pc += n
			target := int(depth)
			if target >= len(e.controls) {
				// Branch past all blocks means return.
				e.frames = e.frames[:len(e.frames)-1]
				return nil
			}
			ctrl := e.controls[len(e.controls)-1-target]
			if ctrl.opcode == WASMOpcodeLoop {
				pc = ctrl.startPC
			} else {
				pc = ctrl.endPC + 1 // skip past the End
				// Pop control entries.
				if len(e.controls) > target+1 {
					e.controls = e.controls[:len(e.controls)-target-1]
				}
			}

		case WASMOpcodeBrIf:
			if err := e.useGas(wasmGasBranch); err != nil {
				return err
			}
			depth, n, err := readLEB128U(code, pc)
			if err != nil {
				return errWASMBadBytecode
			}
			pc += n
			cond, err2 := e.pop()
			if err2 != nil {
				return err2
			}
			if cond != 0 {
				target := int(depth)
				if target >= len(e.controls) {
					e.frames = e.frames[:len(e.frames)-1]
					return nil
				}
				ctrl := e.controls[len(e.controls)-1-target]
				if ctrl.opcode == WASMOpcodeLoop {
					pc = ctrl.startPC
				} else {
					pc = ctrl.endPC + 1
					if len(e.controls) > target+1 {
						e.controls = e.controls[:len(e.controls)-target-1]
					}
				}
			}

		case WASMOpcodeReturn:
			if err := e.useGas(wasmGasControl); err != nil {
				return err
			}
			e.frames = e.frames[:len(e.frames)-1]
			return nil

		case WASMOpcodeCall:
			if err := e.useGas(wasmGasCall); err != nil {
				return err
			}
			funcIdx, n, err := readLEB128U(code, pc)
			if err != nil {
				return errWASMBadBytecode
			}
			pc += n
			if int(funcIdx) >= len(e.funcs) {
				return errWASMNoFunction
			}
			callee := e.funcs[int(funcIdx)]
			callArgs := make([]uint64, callee.NumArgs)
			for i := callee.NumArgs - 1; i >= 0; i-- {
				v, err2 := e.pop()
				if err2 != nil {
					return err2
				}
				callArgs[i] = v
			}
			if err := e.executeFunc(int(funcIdx), callArgs); err != nil {
				return err
			}

		case WASMOpcodeEnd:
			if err := e.useGas(wasmGasConst); err != nil {
				return err
			}
			if len(e.controls) > 0 {
				e.controls = e.controls[:len(e.controls)-1]
			}
			// If this is the function-level End, we're done.
			if len(e.controls) == 0 && pc >= len(code) {
				e.frames = e.frames[:len(e.frames)-1]
				return nil
			}

		case WASMOpcodeLocalGet:
			if err := e.useGas(wasmGasLocal); err != nil {
				return err
			}
			idx, n, err := readLEB128U(code, pc)
			if err != nil {
				return errWASMBadBytecode
			}
			pc += n
			curFrame := &e.frames[len(e.frames)-1]
			if int(idx) >= len(curFrame.Locals) {
				return errWASMBadBytecode
			}
			if err := e.push(curFrame.Locals[idx]); err != nil {
				return err
			}

		case WASMOpcodeLocalSet:
			if err := e.useGas(wasmGasLocal); err != nil {
				return err
			}
			idx, n, err := readLEB128U(code, pc)
			if err != nil {
				return errWASMBadBytecode
			}
			pc += n
			val, err2 := e.pop()
			if err2 != nil {
				return err2
			}
			curFrame := &e.frames[len(e.frames)-1]
			if int(idx) >= len(curFrame.Locals) {
				return errWASMBadBytecode
			}
			curFrame.Locals[idx] = val

		case WASMOpcodeI32Load:
			if err := e.useGas(wasmGasMemory); err != nil {
				return err
			}
			// Read alignment and offset.
			_, n1, err1 := readLEB128U(code, pc)
			if err1 != nil {
				return errWASMBadBytecode
			}
			pc += n1
			memOffset, n2, err2 := readLEB128U(code, pc)
			if err2 != nil {
				return errWASMBadBytecode
			}
			pc += n2
			base, err3 := e.pop()
			if err3 != nil {
				return err3
			}
			addr := uint32(base) + memOffset
			val, err4 := e.memLoad32(addr)
			if err4 != nil {
				return err4
			}
			if err := e.push(uint64(val)); err != nil {
				return err
			}

		case WASMOpcodeI32Store:
			if err := e.useGas(wasmGasMemory); err != nil {
				return err
			}
			_, n1, err1 := readLEB128U(code, pc)
			if err1 != nil {
				return errWASMBadBytecode
			}
			pc += n1
			memOffset, n2, err2 := readLEB128U(code, pc)
			if err2 != nil {
				return errWASMBadBytecode
			}
			pc += n2
			val, err3 := e.pop()
			if err3 != nil {
				return err3
			}
			base, err4 := e.pop()
			if err4 != nil {
				return err4
			}
			addr := uint32(base) + memOffset
			if err := e.memStore32(addr, uint32(val)); err != nil {
				return err
			}

		case WASMOpcodeI32Const:
			if err := e.useGas(wasmGasConst); err != nil {
				return err
			}
			val, n, err := readLEB128S(code, pc)
			if err != nil {
				return errWASMBadBytecode
			}
			pc += n
			if err := e.push(uint64(uint32(val))); err != nil {
				return err
			}

		case WASMOpcodeI32Add:
			if err := e.useGas(wasmGasArith); err != nil {
				return err
			}
			b, err1 := e.pop()
			if err1 != nil {
				return err1
			}
			a, err2 := e.pop()
			if err2 != nil {
				return err2
			}
			if err := e.push(uint64(uint32(a) + uint32(b))); err != nil {
				return err
			}

		case WASMOpcodeI32Sub:
			if err := e.useGas(wasmGasArith); err != nil {
				return err
			}
			b, err1 := e.pop()
			if err1 != nil {
				return err1
			}
			a, err2 := e.pop()
			if err2 != nil {
				return err2
			}
			if err := e.push(uint64(uint32(a) - uint32(b))); err != nil {
				return err
			}

		case WASMOpcodeI32Mul:
			if err := e.useGas(wasmGasArith); err != nil {
				return err
			}
			b, err1 := e.pop()
			if err1 != nil {
				return err1
			}
			a, err2 := e.pop()
			if err2 != nil {
				return err2
			}
			if err := e.push(uint64(uint32(a) * uint32(b))); err != nil {
				return err
			}

		case WASMOpcodeI32DivS:
			if err := e.useGas(wasmGasArith); err != nil {
				return err
			}
			b, err1 := e.pop()
			if err1 != nil {
				return err1
			}
			a, err2 := e.pop()
			if err2 != nil {
				return err2
			}
			bI32 := int32(uint32(b))
			aI32 := int32(uint32(a))
			if bI32 == 0 {
				return ErrWASMDivisionByZero
			}
			// Handle overflow: INT32_MIN / -1.
			if aI32 == math.MinInt32 && bI32 == -1 {
				if err := e.push(uint64(uint32(aI32))); err != nil {
					return err
				}
			} else {
				if err := e.push(uint64(uint32(aI32 / bI32))); err != nil {
					return err
				}
			}

		default:
			return errWASMInvalidOpcode
		}
	}

	// Implicit return at end of function body.
	if len(e.frames) > 0 {
		e.frames = e.frames[:len(e.frames)-1]
	}
	return nil
}

// ExecuteFunction runs a registered function by name with the given arguments.
// Returns the values remaining on the stack, gas used, and any error.
func (e *WASMExecutor) ExecuteFunction(funcName string, args []uint64) ([]uint64, uint64, error) {
	fi := -1
	for i, fn := range e.funcs {
		if fn.Name == funcName {
			fi = i
			break
		}
	}
	if fi < 0 {
		return nil, 0, errWASMNoFunction
	}

	e.sp = 0
	e.gasUsed = 0
	e.frames = e.frames[:0]

	err := e.executeFunc(fi, args)
	if err != nil {
		return nil, e.gasUsed, err
	}

	// Collect results from the stack.
	results := make([]uint64, e.sp)
	copy(results, e.stack[:e.sp])
	return results, e.gasUsed, nil
}

// ExecuteBytecode compiles a simple bytecode function and executes it.
// The code is a raw sequence of WASM opcodes for a single function body.
// numArgs is the number of arguments expected, entrypoint is the function name.
func (e *WASMExecutor) ExecuteBytecode(code []byte, entrypoint string, numArgs int, args []uint64) ([]uint64, uint64, error) {
	// Reset executor state.
	e.funcs = e.funcs[:0]
	e.sp = 0
	e.gasUsed = 0
	e.frames = e.frames[:0]

	e.RegisterFunction(entrypoint, code, numArgs, 4) // 4 extra locals
	return e.ExecuteFunction(entrypoint, args)
}

// GasUsed returns the total gas consumed by the most recent execution.
func (e *WASMExecutor) GasUsed() uint64 {
	return e.gasUsed
}

// MemorySlice returns a copy of executor memory in [offset, offset+length).
func (e *WASMExecutor) MemorySlice(offset, length uint32) ([]byte, error) {
	end := uint64(offset) + uint64(length)
	if end > uint64(len(e.memory)) {
		return nil, ErrWASMOutOfMemory
	}
	out := make([]byte, length)
	copy(out, e.memory[offset:offset+length])
	return out, nil
}

// SetMemory writes data into executor memory at offset.
func (e *WASMExecutor) SetMemory(offset uint32, data []byte) error {
	end := uint64(offset) + uint64(len(data))
	if end > uint64(len(e.memory)) {
		return ErrWASMOutOfMemory
	}
	copy(e.memory[offset:], data)
	return nil
}
