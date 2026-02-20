package vm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// EOF validation errors for instruction-level and stack analysis.
var (
	ErrEOFInvalidOpcode       = errors.New("eof: invalid opcode in EOF code")
	ErrEOFTruncatedImmediate  = errors.New("eof: truncated immediate operand")
	ErrEOFInvalidRJUMPTarget  = errors.New("eof: RJUMP/RJUMPI target not at instruction boundary")
	ErrEOFStackOverflow       = errors.New("eof: stack height exceeds maximum")
	ErrEOFStackUnderflow      = errors.New("eof: stack underflow detected")
	ErrEOFStackMismatch       = errors.New("eof: inconsistent stack height at instruction")
	ErrEOFUnreachableCode     = errors.New("eof: unreachable code detected")
	ErrEOFInvalidStackHeight  = errors.New("eof: declared max_stack_height mismatch")
	ErrEOFInvalidCALLFTarget  = errors.New("eof: CALLF target section out of range")
	ErrEOFInvalidJUMPFTarget  = errors.New("eof: JUMPF target section out of range")
	ErrEOFFallsOffEnd         = errors.New("eof: code falls off the end without terminating")
	ErrEOFInvalidRJUMPTOffset = errors.New("eof: RJUMPT table out of bounds")
)

// EOF-specific opcodes for relative jumps and function calls (EIP-4200, EIP-4750, EIP-6206).
const (
	RJUMP  OpCode = 0xe0
	RJUMPI OpCode = 0xe1
	RJUMPT OpCode = 0xe2 // RJUMPV (jump table)
	CALLF  OpCode = 0xe3
	RETF   OpCode = 0xe4
	JUMPF  OpCode = 0xe5
)

// eofMaxStackHeight is the absolute maximum stack height in EOF (1023).
const eofMaxStackHeight = 1023

// opcodeStackDelta describes the stack effect and immediate size of an opcode.
type opcodeStackDelta struct {
	pops      int // items consumed
	pushes    int // items produced
	imm       int // immediate operand bytes (0 for most opcodes)
	terminal  bool
	validEOF  bool
}

// eofOpcodeTable defines the set of valid opcodes in EOF mode with stack effects.
// Opcodes not in this table are invalid in EOF.
var eofOpcodeTable [256]opcodeStackDelta

func init() {
	// Mark all as invalid by default.
	for i := range eofOpcodeTable {
		eofOpcodeTable[i] = opcodeStackDelta{validEOF: false}
	}

	set := func(op OpCode, pops, pushes, imm int, terminal bool) {
		eofOpcodeTable[op] = opcodeStackDelta{pops: pops, pushes: pushes, imm: imm, terminal: terminal, validEOF: true}
	}

	// Arithmetic / comparison / bitwise (all Gverylow/Glow/Gmid/Ghigh)
	set(STOP, 0, 0, 0, true)
	set(ADD, 2, 1, 0, false)
	set(MUL, 2, 1, 0, false)
	set(SUB, 2, 1, 0, false)
	set(DIV, 2, 1, 0, false)
	set(SDIV, 2, 1, 0, false)
	set(MOD, 2, 1, 0, false)
	set(SMOD, 2, 1, 0, false)
	set(ADDMOD, 3, 1, 0, false)
	set(MULMOD, 3, 1, 0, false)
	set(EXP, 2, 1, 0, false)
	set(SIGNEXTEND, 2, 1, 0, false)

	set(LT, 2, 1, 0, false)
	set(GT, 2, 1, 0, false)
	set(SLT, 2, 1, 0, false)
	set(SGT, 2, 1, 0, false)
	set(EQ, 2, 1, 0, false)
	set(ISZERO, 1, 1, 0, false)
	set(AND, 2, 1, 0, false)
	set(OR, 2, 1, 0, false)
	set(XOR, 2, 1, 0, false)
	set(NOT, 1, 1, 0, false)
	set(BYTE, 2, 1, 0, false)
	set(SHL, 2, 1, 0, false)
	set(SHR, 2, 1, 0, false)
	set(SAR, 2, 1, 0, false)

	set(KECCAK256, 2, 1, 0, false)

	// Environment
	set(ADDRESS, 0, 1, 0, false)
	set(BALANCE, 1, 1, 0, false)
	set(ORIGIN, 0, 1, 0, false)
	set(CALLER, 0, 1, 0, false)
	set(CALLVALUE, 0, 1, 0, false)
	set(CALLDATALOAD, 1, 1, 0, false)
	set(CALLDATASIZE, 0, 1, 0, false)
	set(CALLDATACOPY, 3, 0, 0, false)

	set(RETURNDATASIZE, 0, 1, 0, false)
	set(RETURNDATACOPY, 3, 0, 0, false)

	// Block
	set(BLOCKHASH, 1, 1, 0, false)
	set(COINBASE, 0, 1, 0, false)
	set(TIMESTAMP, 0, 1, 0, false)
	set(NUMBER, 0, 1, 0, false)
	set(PREVRANDAO, 0, 1, 0, false)
	set(GASLIMIT, 0, 1, 0, false)
	set(CHAINID, 0, 1, 0, false)
	set(SELFBALANCE, 0, 1, 0, false)
	set(BASEFEE, 0, 1, 0, false)
	set(BLOBHASH, 1, 1, 0, false)
	set(BLOBBASEFEE, 0, 1, 0, false)

	// Stack / memory / flow
	set(POP, 1, 0, 0, false)
	set(MLOAD, 1, 1, 0, false)
	set(MSTORE, 2, 0, 0, false)
	set(MSTORE8, 2, 0, 0, false)
	set(SLOAD, 1, 1, 0, false)
	set(SSTORE, 2, 0, 0, false)
	set(MSIZE, 0, 1, 0, false)
	set(TLOAD, 1, 1, 0, false)
	set(TSTORE, 2, 0, 0, false)
	set(MCOPY, 3, 0, 0, false)

	// NOTE: JUMP (0x56), JUMPI (0x57), JUMPDEST (0x5b), PC (0x58), GAS (0x5a)
	// are BANNED in EOF. They remain invalid (default).

	set(PUSH0, 0, 1, 0, false)
	// PUSH1..PUSH32
	for i := 1; i <= 32; i++ {
		set(PUSH1+OpCode(i-1), 0, 1, i, false)
	}
	// DUP1..DUP16
	for i := 1; i <= 16; i++ {
		set(DUP1+OpCode(i-1), i, i+1, 0, false)
	}
	// SWAP1..SWAP16
	for i := 1; i <= 16; i++ {
		set(SWAP1+OpCode(i-1), i+1, i+1, 0, false)
	}

	// LOG0..LOG4
	for i := 0; i <= 4; i++ {
		set(LOG0+OpCode(i), 2+i, 0, 0, false)
	}

	// Terminals
	set(RETURN, 2, 0, 0, true)
	set(REVERT, 2, 0, 0, true)
	set(INVALID, 0, 0, 0, true)

	// EOF relative jumps (EIP-4200): 2-byte signed immediate
	set(RJUMP, 0, 0, 2, false)   // unconditional, treated as terminal for flow
	set(RJUMPI, 1, 0, 2, false)  // conditional

	// RJUMPT (EIP-4200 RJUMPV): 1 byte count + count*2 byte offsets
	// imm=-1 signals variable-length immediate; handled specially.
	set(RJUMPT, 1, 0, -1, false)

	// EOF function calls (EIP-4750, EIP-6206)
	set(CALLF, 0, 0, 2, false)  // stack effect depends on target type section
	set(RETF, 0, 0, 0, true)
	set(JUMPF, 0, 0, 2, false)  // treated as terminal

	// EOF data access (EIP-7480)
	set(DATALOAD, 1, 1, 0, false)
	set(DATALOADN, 0, 1, 2, false)
	set(DATASIZE, 0, 1, 0, false)
	set(DATACOPY, 3, 0, 0, false)

	// EOF CALL family (EIP-7069)
	set(RETURNDATALOAD, 1, 1, 0, false)
	set(EXTCALL, 4, 1, 0, false)
	set(EXTDELEGATECALL, 3, 1, 0, false)
	set(EXTSTATICCALL, 3, 1, 0, false)

	// EOF create (EIP-7620): 1-byte immediate
	set(EOFCREATE, 4, 1, 1, false)
	set(RETURNCONTRACT, 2, 0, 1, true)

	// EIP-8024: extended stack
	set(DUPN, 0, 1, 1, false)   // dynamic pops based on immediate
	set(SWAPN, 0, 0, 1, false)  // dynamic stack access
	set(EXCHANGE, 0, 0, 1, false)
}

// EOFValidator performs full validation of EOF containers per EIP-3540.
// It is safe for concurrent use.
type EOFValidator struct {
	mu sync.Mutex // protects internal state during validation
}

// NewEOFValidator returns a new thread-safe EOF validator.
func NewEOFValidator() *EOFValidator {
	return &EOFValidator{}
}

// Validate parses and fully validates EOF bytecode.
// Returns the parsed container on success or an error describing the
// first validation failure.
func (v *EOFValidator) Validate(bytecode []byte) (*EOFContainer, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Step 1: Parse the container header and body.
	container, err := ParseEOF(bytecode)
	if err != nil {
		return nil, err
	}

	// Step 2: Structural validation.
	if err := ValidateEOF(container); err != nil {
		return nil, err
	}

	// Step 3: Validate each code section's instructions and stack.
	for i, code := range container.CodeSections {
		ts := container.TypeSections[i]
		if err := v.validateCodeSection(code, ts, container); err != nil {
			return nil, fmt.Errorf("eof: code section %d: %w", i, err)
		}
	}

	// Step 4: Validate container sections recursively.
	for i, sub := range container.ContainerSections {
		subValidator := &EOFValidator{}
		if _, err := subValidator.Validate(sub); err != nil {
			return nil, fmt.Errorf("eof: container section %d: %w", i, err)
		}
	}

	return container, nil
}

// validateCodeSection validates instructions and performs stack analysis on a
// single code section. It checks that all opcodes are EOF-valid, that
// immediates are not truncated, that RJUMP/RJUMPI targets land on instruction
// boundaries, and that stack heights are consistent.
func (v *EOFValidator) validateCodeSection(code []byte, ts TypeSection, container *EOFContainer) error {
	codeLen := len(code)
	if codeLen == 0 {
		return errors.New("empty code section")
	}

	// Build instruction boundary map and collect jump targets for validation.
	boundaries := make([]bool, codeLen)
	instrOffsets := []int{}

	pos := 0
	for pos < codeLen {
		boundaries[pos] = true
		instrOffsets = append(instrOffsets, pos)

		op := OpCode(code[pos])
		info := eofOpcodeTable[op]
		if !info.validEOF {
			return fmt.Errorf("%w: 0x%02x at offset %d", ErrEOFInvalidOpcode, byte(op), pos)
		}

		immLen := info.imm
		if immLen == -1 {
			// RJUMPT: 1-byte count + count*2 bytes
			if pos+1 >= codeLen {
				return fmt.Errorf("%w: RJUMPT at offset %d", ErrEOFTruncatedImmediate, pos)
			}
			count := int(code[pos+1])
			immLen = 1 + (count+1)*2
		}

		if pos+1+immLen > codeLen {
			return fmt.Errorf("%w: opcode 0x%02x at offset %d needs %d immediate bytes",
				ErrEOFTruncatedImmediate, byte(op), pos, immLen)
		}
		pos += 1 + immLen
	}

	// Validate RJUMP/RJUMPI/RJUMPT targets land on instruction boundaries.
	for _, offset := range instrOffsets {
		op := OpCode(code[offset])
		switch op {
		case RJUMP, RJUMPI:
			rel := int16(binary.BigEndian.Uint16(code[offset+1 : offset+3]))
			target := offset + 3 + int(rel) // target = pc after immediate + relative offset
			if target < 0 || target >= codeLen || !boundaries[target] {
				return fmt.Errorf("%w: offset %d -> target %d", ErrEOFInvalidRJUMPTarget, offset, target)
			}

		case RJUMPT:
			count := int(code[offset+1])
			immBase := offset + 2
			totalImm := (count + 1) * 2
			afterImm := offset + 2 + totalImm
			for i := 0; i <= count; i++ {
				off := immBase + i*2
				rel := int16(binary.BigEndian.Uint16(code[off : off+2]))
				target := afterImm + int(rel)
				if target < 0 || target >= codeLen || !boundaries[target] {
					return fmt.Errorf("%w: RJUMPT offset %d entry %d -> target %d",
						ErrEOFInvalidRJUMPTarget, offset, i, target)
				}
			}

		case CALLF:
			targetSection := binary.BigEndian.Uint16(code[offset+1 : offset+3])
			if int(targetSection) >= len(container.TypeSections) {
				return fmt.Errorf("%w: section %d", ErrEOFInvalidCALLFTarget, targetSection)
			}

		case JUMPF:
			targetSection := binary.BigEndian.Uint16(code[offset+1 : offset+3])
			if int(targetSection) >= len(container.TypeSections) {
				return fmt.Errorf("%w: section %d", ErrEOFInvalidJUMPFTarget, targetSection)
			}
		}
	}

	// Stack analysis using BFS. Track min/max stack heights at each instruction.
	type stackState struct {
		min int
		max int
	}
	states := make([]stackState, codeLen)
	visited := make([]bool, codeLen)

	// Seed the entry point with the declared input stack height.
	inputHeight := int(ts.Inputs)
	states[0] = stackState{min: inputHeight, max: inputHeight}

	queue := []int{0}
	visited[0] = true

	maxObserved := inputHeight

	for len(queue) > 0 {
		pc := queue[0]
		queue = queue[1:]

		st := states[pc]
		op := OpCode(code[pc])
		info := eofOpcodeTable[op]

		pops := info.pops
		pushes := info.pushes

		// For CALLF, the stack effect depends on the target's type section.
		if op == CALLF {
			targetSection := binary.BigEndian.Uint16(code[pc+1 : pc+3])
			targetTS := container.TypeSections[targetSection]
			pops = int(targetTS.Inputs)
			pushes = int(targetTS.Outputs)
			if targetTS.Outputs == eofNonReturning {
				pushes = 0
			}
		}

		// Check stack underflow.
		if st.min < pops {
			return fmt.Errorf("%w: offset %d, have min %d, need %d",
				ErrEOFStackUnderflow, pc, st.min, pops)
		}

		// Compute stack height after this instruction.
		newMin := st.min - pops + pushes
		newMax := st.max - pops + pushes

		if newMax > eofMaxStackHeight {
			return fmt.Errorf("%w: offset %d, height %d", ErrEOFStackOverflow, pc, newMax)
		}
		if newMax > maxObserved {
			maxObserved = newMax
		}

		// Determine successors.
		immLen := info.imm
		if immLen == -1 {
			count := int(code[pc+1])
			immLen = 1 + (count+1)*2
		}
		nextPC := pc + 1 + immLen

		// Propagate stack state to successor.
		propagate := func(target int, sMin, sMax int) {
			if visited[target] {
				// Merge: check consistency.
				if states[target].min != sMin || states[target].max != sMax {
					// Widen the range.
					changed := false
					if sMin < states[target].min {
						states[target] = stackState{min: sMin, max: states[target].max}
						changed = true
					}
					if sMax > states[target].max {
						states[target] = stackState{min: states[target].min, max: sMax}
						changed = true
					}
					if changed {
						queue = append(queue, target)
					}
				}
			} else {
				visited[target] = true
				states[target] = stackState{min: sMin, max: sMax}
				queue = append(queue, target)
			}
		}

		switch op {
		case STOP, RETURN, REVERT, INVALID, RETF, RETURNCONTRACT:
			// Terminal: no successors.
			continue

		case RJUMP:
			// Unconditional jump: only the target is a successor.
			rel := int16(binary.BigEndian.Uint16(code[pc+1 : pc+3]))
			target := pc + 3 + int(rel)
			propagate(target, newMin, newMax)

		case RJUMPI:
			// Conditional: fall-through and target.
			rel := int16(binary.BigEndian.Uint16(code[pc+1 : pc+3]))
			target := pc + 3 + int(rel)
			if nextPC < codeLen {
				propagate(nextPC, newMin, newMax)
			}
			propagate(target, newMin, newMax)

		case RJUMPT:
			// Jump table: fall-through and all targets.
			count := int(code[pc+1])
			immBase := pc + 2
			totalImm := (count + 1) * 2
			afterImm := pc + 2 + totalImm
			if afterImm < codeLen {
				propagate(afterImm, newMin, newMax)
			}
			for i := 0; i <= count; i++ {
				off := immBase + i*2
				rel := int16(binary.BigEndian.Uint16(code[off : off+2]))
				target := afterImm + int(rel)
				propagate(target, newMin, newMax)
			}

		case JUMPF:
			// Terminal from this section's perspective. No fall-through.
			continue

		default:
			// Linear: fall through to next instruction.
			if nextPC < codeLen {
				propagate(nextPC, newMin, newMax)
			} else if !info.terminal {
				return ErrEOFFallsOffEnd
			}
		}
	}

	// Check for unreachable code.
	for _, offset := range instrOffsets {
		if !visited[offset] {
			return fmt.Errorf("%w: offset %d", ErrEOFUnreachableCode, offset)
		}
	}

	// Verify declared max_stack_height.
	// max_stack_height is the maximum stack increase above inputs.
	observedIncrease := maxObserved - inputHeight
	if observedIncrease < 0 {
		observedIncrease = 0
	}
	if uint16(observedIncrease) != ts.MaxStackHeight {
		return fmt.Errorf("%w: declared %d, observed %d",
			ErrEOFInvalidStackHeight, ts.MaxStackHeight, observedIncrease)
	}

	return nil
}
