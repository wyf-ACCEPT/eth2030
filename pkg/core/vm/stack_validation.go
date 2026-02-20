package vm

// stack_validation.go implements stack depth validation for all EVM opcodes.
// It provides per-opcode min/max stack requirements, overflow/underflow checks,
// and EOF stack analysis with type system support.

import (
	"errors"
	"fmt"
)

// Stack validation errors.
var (
	ErrStackValidationOverflow  = errors.New("stack validation: overflow")
	ErrStackValidationUnderflow = errors.New("stack validation: underflow")
	ErrStackInvalidDelta        = errors.New("stack validation: invalid stack delta for opcode")
)

// StackRequirement describes the stack constraints for a single opcode:
// how many items it pops, how many it pushes, and the net delta.
type StackRequirement struct {
	Pops   int // number of items consumed from the stack
	Pushes int // number of items produced onto the stack
	Delta  int // net change: Pushes - Pops
}

// opcodeStackRequirements maps every opcode to its stack requirement.
// Opcodes not present in this table are considered invalid.
var opcodeStackRequirements [256]StackRequirement

// opcodeValid marks which opcodes have defined stack requirements.
var opcodeValid [256]bool

func init() {
	reg := func(op OpCode, pops, pushes int) {
		opcodeStackRequirements[op] = StackRequirement{
			Pops:   pops,
			Pushes: pushes,
			Delta:  pushes - pops,
		}
		opcodeValid[op] = true
	}

	// 0x00: STOP
	reg(STOP, 0, 0)

	// Arithmetic
	reg(ADD, 2, 1)
	reg(MUL, 2, 1)
	reg(SUB, 2, 1)
	reg(DIV, 2, 1)
	reg(SDIV, 2, 1)
	reg(MOD, 2, 1)
	reg(SMOD, 2, 1)
	reg(ADDMOD, 3, 1)
	reg(MULMOD, 3, 1)
	reg(EXP, 2, 1)
	reg(SIGNEXTEND, 2, 1)

	// Comparison
	reg(LT, 2, 1)
	reg(GT, 2, 1)
	reg(SLT, 2, 1)
	reg(SGT, 2, 1)
	reg(EQ, 2, 1)
	reg(ISZERO, 1, 1)

	// Bitwise
	reg(AND, 2, 1)
	reg(OR, 2, 1)
	reg(XOR, 2, 1)
	reg(NOT, 1, 1)
	reg(BYTE, 2, 1)
	reg(SHL, 2, 1)
	reg(SHR, 2, 1)
	reg(SAR, 2, 1)
	reg(CLZ, 1, 1) // EIP-7939

	// Hash
	reg(KECCAK256, 2, 1)

	// Environment
	reg(ADDRESS, 0, 1)
	reg(BALANCE, 1, 1)
	reg(ORIGIN, 0, 1)
	reg(CALLER, 0, 1)
	reg(CALLVALUE, 0, 1)
	reg(CALLDATALOAD, 1, 1)
	reg(CALLDATASIZE, 0, 1)
	reg(CALLDATACOPY, 3, 0)
	reg(CODESIZE, 0, 1)
	reg(CODECOPY, 3, 0)
	reg(GASPRICE, 0, 1)
	reg(EXTCODESIZE, 1, 1)
	reg(EXTCODECOPY, 4, 0)
	reg(RETURNDATASIZE, 0, 1)
	reg(RETURNDATACOPY, 3, 0)
	reg(EXTCODEHASH, 1, 1)

	// Block
	reg(BLOCKHASH, 1, 1)
	reg(COINBASE, 0, 1)
	reg(TIMESTAMP, 0, 1)
	reg(NUMBER, 0, 1)
	reg(PREVRANDAO, 0, 1)
	reg(GASLIMIT, 0, 1)
	reg(CHAINID, 0, 1)
	reg(SELFBALANCE, 0, 1)
	reg(BASEFEE, 0, 1)
	reg(BLOBHASH, 1, 1)
	reg(BLOBBASEFEE, 0, 1)
	reg(SLOTNUM, 0, 1) // EIP-7843

	// Stack, memory, flow
	reg(POP, 1, 0)
	reg(MLOAD, 1, 1)
	reg(MSTORE, 2, 0)
	reg(MSTORE8, 2, 0)
	reg(SLOAD, 1, 1)
	reg(SSTORE, 2, 0)
	reg(JUMP, 1, 0)
	reg(JUMPI, 2, 0)
	reg(PC, 0, 1)
	reg(MSIZE, 0, 1)
	reg(GAS, 0, 1)
	reg(JUMPDEST, 0, 0)
	reg(TLOAD, 1, 1)
	reg(TSTORE, 2, 0)
	reg(MCOPY, 3, 0)

	// PUSH0 through PUSH32: all push 1 item
	reg(PUSH0, 0, 1)
	for i := 0; i < 32; i++ {
		reg(PUSH1+OpCode(i), 0, 1)
	}

	// DUP1..DUP16: peek n items, push 1 (net +1)
	for i := 1; i <= 16; i++ {
		reg(DUP1+OpCode(i-1), i, i+1)
	}

	// SWAP1..SWAP16: need n+1 items, produce n+1 (net 0)
	for i := 1; i <= 16; i++ {
		reg(SWAP1+OpCode(i-1), i+1, i+1)
	}

	// LOG0..LOG4
	for i := 0; i <= 4; i++ {
		reg(LOG0+OpCode(i), 2+i, 0)
	}

	// CALL family
	reg(CREATE, 3, 1)
	reg(CALL, 7, 1)
	reg(CALLCODE, 7, 1)
	reg(RETURN, 2, 0)
	reg(DELEGATECALL, 6, 1)
	reg(CREATE2, 4, 1)
	reg(STATICCALL, 6, 1)
	reg(REVERT, 2, 0)
	reg(INVALID, 0, 0)
	reg(SELFDESTRUCT, 1, 0)

	// EIP-8024: DUPN, SWAPN, EXCHANGE (dynamic stack, registered with
	// minimum requirements; actual depth is checked at runtime).
	reg(DUPN, 1, 2)
	reg(SWAPN, 2, 2)
	reg(EXCHANGE, 2, 2)
}

// GetStackRequirement returns the stack requirement for a given opcode.
// Returns the requirement and whether the opcode is valid.
func GetStackRequirement(op OpCode) (StackRequirement, bool) {
	return opcodeStackRequirements[op], opcodeValid[op]
}

// ValidateStackForOp checks if the current stack depth is valid for executing
// the given opcode. It returns an error on underflow or overflow.
func ValidateStackForOp(op OpCode, stackDepth int) error {
	if !opcodeValid[op] {
		return fmt.Errorf("%w: opcode 0x%02x", ErrStackInvalidDelta, byte(op))
	}
	req := opcodeStackRequirements[op]

	// Underflow: not enough items to pop.
	if stackDepth < req.Pops {
		return fmt.Errorf("%w: opcode %s needs %d items, stack has %d",
			ErrStackValidationUnderflow, op, req.Pops, stackDepth)
	}

	// Overflow: after executing, stack would exceed 1024.
	resultDepth := stackDepth + req.Delta
	if resultDepth > stackLimit {
		return fmt.Errorf("%w: opcode %s would produce depth %d (limit %d)",
			ErrStackValidationOverflow, op, resultDepth, stackLimit)
	}

	return nil
}

// StackValidator performs static stack analysis on bytecode sequences.
// It tracks stack height through linear code and validates that no opcode
// causes underflow or overflow.
type StackValidator struct {
	// MaxStackHeight is the highest stack depth observed during analysis.
	MaxStackHeight int
	// MinStackHeight is the lowest stack depth observed during analysis.
	MinStackHeight int
}

// NewStackValidator creates a new StackValidator.
func NewStackValidator() *StackValidator {
	return &StackValidator{}
}

// ValidateSequence performs static analysis on a bytecode sequence starting
// with the given initial stack depth. It walks through the opcodes linearly,
// tracking stack height and checking for underflow/overflow at each step.
// Returns the final stack depth and any validation error.
func (sv *StackValidator) ValidateSequence(code []byte, initialDepth int) (int, error) {
	depth := initialDepth
	sv.MaxStackHeight = initialDepth
	sv.MinStackHeight = initialDepth

	pos := 0
	for pos < len(code) {
		op := OpCode(code[pos])
		if err := ValidateStackForOp(op, depth); err != nil {
			return depth, fmt.Errorf("at offset %d: %w", pos, err)
		}

		req := opcodeStackRequirements[op]
		depth += req.Delta

		if depth > sv.MaxStackHeight {
			sv.MaxStackHeight = depth
		}
		if depth < sv.MinStackHeight {
			sv.MinStackHeight = depth
		}

		// Advance past immediate operands for PUSH instructions.
		if op >= PUSH1 && op <= PUSH32 {
			pos += int(op-PUSH1) + 1
		}
		pos++
	}

	return depth, nil
}
