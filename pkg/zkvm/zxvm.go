// zxvm.go implements the canonical zxVM -- a zero-knowledge virtual machine
// that can execute general-purpose computation with verifiable proofs.
// Part of the M+ roadmap: canonical zxVM for proof-carrying execution.
package zkvm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/crypto"
)

// zxVM instruction set (RISC-like opcodes).
const (
	ZxOpADD   byte = 0x01 // pop a, b; push a+b
	ZxOpSUB   byte = 0x02 // pop a, b; push a-b
	ZxOpMUL   byte = 0x03 // pop a, b; push a*b
	ZxOpLOAD  byte = 0x04 // pop addr; push mem[addr]
	ZxOpSTORE byte = 0x05 // pop addr, val; mem[addr] = val
	ZxOpJUMP  byte = 0x06 // pop target; pc = target
	ZxOpHALT  byte = 0x07 // stop execution
	ZxOpHASH  byte = 0x08 // pop val; push keccak256(val as 8 LE bytes)
	ZxOpPUSH  byte = 0x09 // push immediate uint64 (next 8 bytes, LE)
)

// Gas costs per zxVM opcode.
const (
	zxGasArith  uint64 = 1
	zxGasLoad   uint64 = 3
	zxGasStore  uint64 = 3
	zxGasJump   uint64 = 2
	zxGasHalt   uint64 = 0
	zxGasHash   uint64 = 10
	zxGasPush   uint64 = 1
)

// zxVM errors.
var (
	ErrZxEmptyCode       = errors.New("zxvm: empty program code")
	ErrZxCycleLimitHit   = errors.New("zxvm: cycle limit exceeded")
	ErrZxMemoryOverflow  = errors.New("zxvm: memory address out of bounds")
	ErrZxStackUnderflow  = errors.New("zxvm: stack underflow")
	ErrZxStackOverflow   = errors.New("zxvm: stack overflow")
	ErrZxInvalidOpcode   = errors.New("zxvm: invalid opcode")
	ErrZxOutOfGas        = errors.New("zxvm: out of gas")
	ErrZxProgramNotLoaded = errors.New("zxvm: no program loaded")
	ErrZxInvalidJump     = errors.New("zxvm: jump target out of bounds")
	ErrZxTruncatedPush   = errors.New("zxvm: truncated PUSH immediate")
)

// ZxVMConfig holds configuration for a zxVM instance.
type ZxVMConfig struct {
	MaxCycles   uint64 // maximum execution cycles before halting
	MemoryLimit uint64 // maximum memory words (each 8 bytes)
	ProofSystem string // proof system: "stark", "plonk", "groth16"
	StackDepth  uint64 // maximum stack depth
}

// DefaultZxVMConfig returns a sensible default configuration.
func DefaultZxVMConfig() ZxVMConfig {
	return ZxVMConfig{
		MaxCycles:   1 << 22, // ~4M cycles
		MemoryLimit: 1 << 16, // 64K words = 512 KiB
		ProofSystem: "stark",
		StackDepth:  1024,
	}
}

// ZxProgram represents a compiled program for the zxVM.
type ZxProgram struct {
	Code         []byte // bytecode instructions
	EntryPoint   uint64 // starting PC offset
	MemoryLayout uint64 // number of memory words to allocate
	GasLimit     uint64 // maximum gas for execution
}

// ZxExecutionResult holds the outcome of executing a zxVM program.
type ZxExecutionResult struct {
	Output          []byte // output data (stack top serialized)
	GasUsed         uint64 // total gas consumed
	ProofCommitment []byte // commitment hash for the execution proof
	CycleCount      uint64 // total cycles executed
	MemoryPeak      uint64 // peak memory words used
}

// ZxStep records a single step in the execution trace.
type ZxStep struct {
	PC        uint64 // program counter before this step
	Opcode    byte   // opcode executed
	StackHash []byte // keccak256 hash of the stack state after the step
	MemHash   []byte // keccak256 hash of memory state after the step
}

// ZxTrace records the full execution trace for verification.
type ZxTrace struct {
	Steps []ZxStep
}

// ZxVMInstance represents a running zxVM with loaded program and state.
type ZxVMInstance struct {
	mu      sync.Mutex
	config  ZxVMConfig
	program *ZxProgram
	memory  []uint64
	stack   []uint64
	pc      uint64
	halted  bool
}

// NewZxVM creates a new zxVM instance with the given configuration.
func NewZxVM(config ZxVMConfig) *ZxVMInstance {
	return &ZxVMInstance{
		config: config,
	}
}

// LoadProgram loads a program into the zxVM, resetting its state.
func (vm *ZxVMInstance) LoadProgram(prog *ZxProgram) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if prog == nil || len(prog.Code) == 0 {
		return ErrZxEmptyCode
	}

	memSize := prog.MemoryLayout
	if memSize == 0 {
		memSize = 256 // default 256 words
	}
	if memSize > vm.config.MemoryLimit {
		memSize = vm.config.MemoryLimit
	}

	vm.program = prog
	vm.memory = make([]uint64, memSize)
	vm.stack = make([]uint64, 0, 64)
	vm.pc = prog.EntryPoint
	vm.halted = false
	return nil
}

// Execute runs the loaded program to completion and returns the result.
func (vm *ZxVMInstance) Execute() (*ZxExecutionResult, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.program == nil {
		return nil, ErrZxProgramNotLoaded
	}

	var gasUsed uint64
	var cycles uint64
	var memoryPeak uint64
	code := vm.program.Code
	gasLimit := vm.program.GasLimit

	for !vm.halted {
		cycles++
		if cycles > vm.config.MaxCycles {
			return &ZxExecutionResult{
				GasUsed:    gasUsed,
				CycleCount: cycles - 1,
				MemoryPeak: memoryPeak,
			}, ErrZxCycleLimitHit
		}

		if vm.pc >= uint64(len(code)) {
			// Implicit halt at end of code.
			vm.halted = true
			break
		}

		op := code[vm.pc]
		cost := zxOpcodeGas(op)
		gasUsed += cost
		if gasLimit > 0 && gasUsed > gasLimit {
			return &ZxExecutionResult{
				GasUsed:    gasUsed,
				CycleCount: cycles,
				MemoryPeak: memoryPeak,
			}, ErrZxOutOfGas
		}

		if err := vm.step(op); err != nil {
			return &ZxExecutionResult{
				GasUsed:    gasUsed,
				CycleCount: cycles,
				MemoryPeak: memoryPeak,
			}, err
		}

		// Track memory peak.
		used := vm.memoryUsed()
		if used > memoryPeak {
			memoryPeak = used
		}
	}

	// Build output from top of stack.
	var output []byte
	if len(vm.stack) > 0 {
		val := vm.stack[len(vm.stack)-1]
		output = make([]byte, 8)
		binary.LittleEndian.PutUint64(output, val)
	}

	// Compute proof commitment: H(code || output || gasUsed || cycles).
	commitment := computeProofCommitment(code, output, gasUsed, cycles)

	return &ZxExecutionResult{
		Output:          output,
		GasUsed:         gasUsed,
		ProofCommitment: commitment,
		CycleCount:      cycles,
		MemoryPeak:      memoryPeak,
	}, nil
}

// GenerateTrace runs the loaded program and records every step.
func (vm *ZxVMInstance) GenerateTrace() (*ZxTrace, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.program == nil {
		return nil, ErrZxProgramNotLoaded
	}

	// Reset state for trace.
	vm.resetState()

	trace := &ZxTrace{}
	code := vm.program.Code
	var cycles uint64

	for !vm.halted {
		cycles++
		if cycles > vm.config.MaxCycles {
			return trace, ErrZxCycleLimitHit
		}
		if vm.pc >= uint64(len(code)) {
			vm.halted = true
			break
		}

		op := code[vm.pc]
		stepPC := vm.pc

		if err := vm.step(op); err != nil {
			return trace, err
		}

		step := ZxStep{
			PC:        stepPC,
			Opcode:    op,
			StackHash: vm.hashStack(),
			MemHash:   vm.hashMemory(),
		}
		trace.Steps = append(trace.Steps, step)
	}

	return trace, nil
}

// VerifyTrace checks that a trace is consistent by replaying it
// against the loaded program and verifying step hashes.
func (vm *ZxVMInstance) VerifyTrace(trace *ZxTrace) (bool, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.program == nil {
		return false, ErrZxProgramNotLoaded
	}
	if trace == nil || len(trace.Steps) == 0 {
		return false, nil
	}

	// Replay execution and compare hashes.
	vm.resetState()
	code := vm.program.Code

	for i, expected := range trace.Steps {
		if vm.halted {
			return false, nil
		}
		if vm.pc >= uint64(len(code)) {
			return false, nil
		}

		op := code[vm.pc]
		if op != expected.Opcode {
			return false, fmt.Errorf("step %d: opcode mismatch: got 0x%02x, trace has 0x%02x", i, op, expected.Opcode)
		}
		if vm.pc != expected.PC {
			return false, fmt.Errorf("step %d: PC mismatch: got %d, trace has %d", i, vm.pc, expected.PC)
		}

		if err := vm.step(op); err != nil {
			return false, err
		}

		stackHash := vm.hashStack()
		if !bytesEqual(stackHash, expected.StackHash) {
			return false, nil
		}
		memHash := vm.hashMemory()
		if !bytesEqual(memHash, expected.MemHash) {
			return false, nil
		}
	}

	return true, nil
}

// EstimateGas runs the program and returns the gas that would be consumed.
func (vm *ZxVMInstance) EstimateGas() (uint64, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.program == nil {
		return 0, ErrZxProgramNotLoaded
	}

	// Save and restore state.
	origMem := make([]uint64, len(vm.memory))
	copy(origMem, vm.memory)
	origStack := make([]uint64, len(vm.stack))
	copy(origStack, vm.stack)
	origPC := vm.pc
	origHalted := vm.halted

	vm.resetState()

	var gasUsed uint64
	var cycles uint64
	code := vm.program.Code

	for !vm.halted {
		cycles++
		if cycles > vm.config.MaxCycles {
			break
		}
		if vm.pc >= uint64(len(code)) {
			break
		}

		op := code[vm.pc]
		gasUsed += zxOpcodeGas(op)

		if err := vm.step(op); err != nil {
			// Restore state on error.
			vm.memory = origMem
			vm.stack = origStack
			vm.pc = origPC
			vm.halted = origHalted
			return gasUsed, err
		}
	}

	// Restore state.
	vm.memory = origMem
	vm.stack = origStack
	vm.pc = origPC
	vm.halted = origHalted

	return gasUsed, nil
}

// step executes a single opcode and advances the PC.
func (vm *ZxVMInstance) step(op byte) error {
	switch op {
	case ZxOpPUSH:
		if vm.pc+9 > uint64(len(vm.program.Code)) {
			return ErrZxTruncatedPush
		}
		val := binary.LittleEndian.Uint64(vm.program.Code[vm.pc+1 : vm.pc+9])
		if err := vm.push(val); err != nil {
			return err
		}
		vm.pc += 9
		return nil

	case ZxOpADD:
		b, err := vm.pop()
		if err != nil {
			return err
		}
		a, err := vm.pop()
		if err != nil {
			return err
		}
		if err := vm.push(a + b); err != nil {
			return err
		}

	case ZxOpSUB:
		b, err := vm.pop()
		if err != nil {
			return err
		}
		a, err := vm.pop()
		if err != nil {
			return err
		}
		if err := vm.push(a - b); err != nil {
			return err
		}

	case ZxOpMUL:
		b, err := vm.pop()
		if err != nil {
			return err
		}
		a, err := vm.pop()
		if err != nil {
			return err
		}
		if err := vm.push(a * b); err != nil {
			return err
		}

	case ZxOpLOAD:
		addr, err := vm.pop()
		if err != nil {
			return err
		}
		if addr >= uint64(len(vm.memory)) {
			return ErrZxMemoryOverflow
		}
		if err := vm.push(vm.memory[addr]); err != nil {
			return err
		}

	case ZxOpSTORE:
		val, err := vm.pop()
		if err != nil {
			return err
		}
		addr, err := vm.pop()
		if err != nil {
			return err
		}
		if addr >= uint64(len(vm.memory)) {
			return ErrZxMemoryOverflow
		}
		vm.memory[addr] = val

	case ZxOpJUMP:
		target, err := vm.pop()
		if err != nil {
			return err
		}
		if target >= uint64(len(vm.program.Code)) {
			return ErrZxInvalidJump
		}
		vm.pc = target
		return nil

	case ZxOpHALT:
		vm.halted = true

	case ZxOpHASH:
		val, err := vm.pop()
		if err != nil {
			return err
		}
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, val)
		h := crypto.Keccak256(buf)
		// Take first 8 bytes of hash as uint64 result.
		result := binary.LittleEndian.Uint64(h[:8])
		if err := vm.push(result); err != nil {
			return err
		}

	default:
		return fmt.Errorf("%w: 0x%02x at pc=%d", ErrZxInvalidOpcode, op, vm.pc)
	}

	vm.pc++
	return nil
}

// push pushes a value onto the stack.
func (vm *ZxVMInstance) push(val uint64) error {
	if uint64(len(vm.stack)) >= vm.config.StackDepth {
		return ErrZxStackOverflow
	}
	vm.stack = append(vm.stack, val)
	return nil
}

// pop pops a value from the stack.
func (vm *ZxVMInstance) pop() (uint64, error) {
	if len(vm.stack) == 0 {
		return 0, ErrZxStackUnderflow
	}
	val := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return val, nil
}

// memoryUsed returns the highest non-zero memory address + 1.
func (vm *ZxVMInstance) memoryUsed() uint64 {
	for i := len(vm.memory) - 1; i >= 0; i-- {
		if vm.memory[i] != 0 {
			return uint64(i + 1)
		}
	}
	return 0
}

// resetState resets the VM execution state without clearing the program.
func (vm *ZxVMInstance) resetState() {
	if vm.program != nil {
		memSize := vm.program.MemoryLayout
		if memSize == 0 {
			memSize = 256
		}
		if memSize > vm.config.MemoryLimit {
			memSize = vm.config.MemoryLimit
		}
		vm.memory = make([]uint64, memSize)
	}
	vm.stack = vm.stack[:0]
	vm.pc = vm.program.EntryPoint
	vm.halted = false
}

// hashStack returns keccak256 of the serialized stack.
func (vm *ZxVMInstance) hashStack() []byte {
	buf := make([]byte, len(vm.stack)*8)
	for i, v := range vm.stack {
		binary.LittleEndian.PutUint64(buf[i*8:], v)
	}
	return crypto.Keccak256(buf)
}

// hashMemory returns keccak256 of the serialized memory.
func (vm *ZxVMInstance) hashMemory() []byte {
	buf := make([]byte, len(vm.memory)*8)
	for i, v := range vm.memory {
		binary.LittleEndian.PutUint64(buf[i*8:], v)
	}
	return crypto.Keccak256(buf)
}

// zxOpcodeGas returns the gas cost for a zxVM opcode.
func zxOpcodeGas(op byte) uint64 {
	switch op {
	case ZxOpADD, ZxOpSUB, ZxOpMUL:
		return zxGasArith
	case ZxOpLOAD:
		return zxGasLoad
	case ZxOpSTORE:
		return zxGasStore
	case ZxOpJUMP:
		return zxGasJump
	case ZxOpHALT:
		return zxGasHalt
	case ZxOpHASH:
		return zxGasHash
	case ZxOpPUSH:
		return zxGasPush
	default:
		return 0
	}
}

// computeProofCommitment derives a commitment hash from execution parameters.
func computeProofCommitment(code, output []byte, gasUsed, cycles uint64) []byte {
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], gasUsed)
	binary.LittleEndian.PutUint64(buf[8:], cycles)
	return crypto.Keccak256(code, output, buf[:])
}

// bytesEqual compares two byte slices for equality.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// BuildZxPush builds a PUSH instruction with an immediate uint64 value.
func BuildZxPush(val uint64) []byte {
	buf := make([]byte, 9)
	buf[0] = ZxOpPUSH
	binary.LittleEndian.PutUint64(buf[1:], val)
	return buf
}
