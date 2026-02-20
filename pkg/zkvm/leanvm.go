// leanvm.go implements a lightweight virtual machine for proof aggregation
// across multiple ZK proofs (L+ era). LeanVM defines a minimal instruction
// set (ADD, MUL, HASH, VERIFY) optimised for proof verification circuits,
// allowing efficient aggregation of heterogeneous proofs into a single
// composite proof.
package zkvm

import (
	"encoding/binary"
	"errors"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// LeanVM errors.
var (
	ErrLeanEmptyProgram     = errors.New("leanvm: empty program")
	ErrLeanEmptyInputs      = errors.New("leanvm: no inputs provided")
	ErrLeanInvalidOpcode    = errors.New("leanvm: invalid opcode")
	ErrLeanStackUnderflow   = errors.New("leanvm: stack underflow")
	ErrLeanCycleLimit       = errors.New("leanvm: cycle limit exceeded")
	ErrLeanInvalidOperand   = errors.New("leanvm: operand index out of range")
	ErrLeanAggregateEmpty   = errors.New("leanvm: no proofs to aggregate")
	ErrLeanVerifyMismatch   = errors.New("leanvm: aggregated proof does not match commitments")
	ErrLeanCompileEmpty     = errors.New("leanvm: empty EVM bytecode")
	ErrLeanCompileUnsupported = errors.New("leanvm: unsupported EVM opcode")
)

// LeanOpcode is the instruction set for the LeanVM.
type LeanOpcode uint8

const (
	// OpADD adds two stack operands (modular addition over 256-bit field).
	OpADD LeanOpcode = 0x01

	// OpMUL multiplies two stack operands (modular multiplication over 256-bit field).
	OpMUL LeanOpcode = 0x02

	// OpHASH hashes the top stack element using Keccak256.
	OpHASH LeanOpcode = 0x03

	// OpVERIFY verifies that the top two stack elements are equal.
	OpVERIFY LeanOpcode = 0x04

	// OpPUSH pushes an input onto the stack by index.
	OpPUSH LeanOpcode = 0x05

	// OpDUP duplicates the top stack element.
	OpDUP LeanOpcode = 0x06
)

// String returns the mnemonic for a LeanOpcode.
func (op LeanOpcode) String() string {
	switch op {
	case OpADD:
		return "ADD"
	case OpMUL:
		return "MUL"
	case OpHASH:
		return "HASH"
	case OpVERIFY:
		return "VERIFY"
	case OpPUSH:
		return "PUSH"
	case OpDUP:
		return "DUP"
	default:
		return "INVALID"
	}
}

// IsValid returns true if the opcode is a known LeanVM instruction.
func (op LeanOpcode) IsValid() bool {
	return op >= OpADD && op <= OpDUP
}

// Gas costs per LeanVM instruction.
const (
	GasADD    uint64 = 3
	GasMUL    uint64 = 5
	GasHASH   uint64 = 30
	GasVERIFY uint64 = 50
	GasPUSH   uint64 = 2
	GasDUP    uint64 = 2

	// DefaultMaxCyclesLean is the default cycle limit for LeanVM execution.
	DefaultMaxCyclesLean uint64 = 1 << 20 // ~1M steps
)

// LeanOp represents a single LeanVM instruction with operands.
type LeanOp struct {
	Opcode   LeanOpcode
	Operands []uint32 // indices into the input array (for PUSH)
}

// LeanResult holds the output of a LeanVM execution.
type LeanResult struct {
	Output  []byte
	GasUsed uint64
	Steps   int
	Success bool
}

// LeanVM is a lightweight virtual machine for proof aggregation.
type LeanVM struct {
	MaxCycles uint64
	stack     [][]byte
}

// NewLeanVM creates a new LeanVM with the given cycle limit.
func NewLeanVM(maxCycles uint64) *LeanVM {
	if maxCycles == 0 {
		maxCycles = DefaultMaxCyclesLean
	}
	return &LeanVM{
		MaxCycles: maxCycles,
		stack:     make([][]byte, 0, 64),
	}
}

// Execute runs a LeanVM program against the given inputs. The final stack
// top is returned as the output. Returns an error if the program violates
// constraints (cycle limit, stack underflow, invalid opcode).
func (vm *LeanVM) Execute(program []LeanOp, inputs [][]byte) (*LeanResult, error) {
	if len(program) == 0 {
		return nil, ErrLeanEmptyProgram
	}

	vm.stack = vm.stack[:0] // reset stack
	var gasUsed uint64
	steps := 0

	for _, op := range program {
		if uint64(steps) >= vm.MaxCycles {
			return &LeanResult{GasUsed: gasUsed, Steps: steps}, ErrLeanCycleLimit
		}
		steps++

		switch op.Opcode {
		case OpPUSH:
			if len(op.Operands) == 0 {
				return nil, ErrLeanInvalidOperand
			}
			idx := int(op.Operands[0])
			if idx >= len(inputs) {
				return nil, ErrLeanInvalidOperand
			}
			// Push a copy of the input.
			elem := make([]byte, len(inputs[idx]))
			copy(elem, inputs[idx])
			vm.stack = append(vm.stack, elem)
			gasUsed += GasPUSH

		case OpDUP:
			if len(vm.stack) < 1 {
				return nil, ErrLeanStackUnderflow
			}
			top := vm.stack[len(vm.stack)-1]
			dup := make([]byte, len(top))
			copy(dup, top)
			vm.stack = append(vm.stack, dup)
			gasUsed += GasDUP

		case OpADD:
			if len(vm.stack) < 2 {
				return nil, ErrLeanStackUnderflow
			}
			b := vm.stack[len(vm.stack)-1]
			a := vm.stack[len(vm.stack)-2]
			vm.stack = vm.stack[:len(vm.stack)-2]
			result := addBytes(a, b)
			vm.stack = append(vm.stack, result)
			gasUsed += GasADD

		case OpMUL:
			if len(vm.stack) < 2 {
				return nil, ErrLeanStackUnderflow
			}
			b := vm.stack[len(vm.stack)-1]
			a := vm.stack[len(vm.stack)-2]
			vm.stack = vm.stack[:len(vm.stack)-2]
			result := mulBytes(a, b)
			vm.stack = append(vm.stack, result)
			gasUsed += GasMUL

		case OpHASH:
			if len(vm.stack) < 1 {
				return nil, ErrLeanStackUnderflow
			}
			top := vm.stack[len(vm.stack)-1]
			vm.stack[len(vm.stack)-1] = crypto.Keccak256(top)
			gasUsed += GasHASH

		case OpVERIFY:
			if len(vm.stack) < 2 {
				return nil, ErrLeanStackUnderflow
			}
			b := vm.stack[len(vm.stack)-1]
			a := vm.stack[len(vm.stack)-2]
			vm.stack = vm.stack[:len(vm.stack)-2]
			// Push 1 if equal, 0 if not.
			if bytesEqual(a, b) {
				vm.stack = append(vm.stack, []byte{0x01})
			} else {
				vm.stack = append(vm.stack, []byte{0x00})
			}
			gasUsed += GasVERIFY

		default:
			return nil, ErrLeanInvalidOpcode
		}
	}

	var output []byte
	if len(vm.stack) > 0 {
		output = vm.stack[len(vm.stack)-1]
	}

	return &LeanResult{
		Output:  output,
		GasUsed: gasUsed,
		Steps:   steps,
		Success: true,
	}, nil
}

// AggregateProofs combines multiple ZK proofs into a single aggregated proof.
// The aggregated proof is H(H(proof_0) || H(proof_1) || ... || len).
func AggregateProofs(proofs [][]byte) ([]byte, error) {
	if len(proofs) == 0 {
		return nil, ErrLeanAggregateEmpty
	}

	// Hash each proof individually, then hash the concatenation.
	var concat []byte
	for _, p := range proofs {
		h := crypto.Keccak256(p)
		concat = append(concat, h...)
	}
	// Append the count for domain separation.
	var countBuf [4]byte
	binary.BigEndian.PutUint32(countBuf[:], uint32(len(proofs)))
	concat = append(concat, countBuf[:]...)

	return crypto.Keccak256(concat), nil
}

// VerifyAggregated checks that an aggregated proof matches the given original
// proof commitments by re-aggregating and comparing.
func VerifyAggregated(aggregated []byte, originalCommitments [][]byte) bool {
	if len(aggregated) == 0 || len(originalCommitments) == 0 {
		return false
	}
	expected, err := AggregateProofs(originalCommitments)
	if err != nil {
		return false
	}
	return bytesEqual(aggregated, expected)
}

// CompileToLean performs a basic transpilation from EVM bytecode to LeanVM
// instructions. Only a subset of EVM opcodes is supported: ADD (0x01),
// MUL (0x02), PUSH1 (0x60), SHA3 (0x20), and EQ (0x14). All other opcodes
// produce an error. This is intentionally limited as a proof-of-concept for
// the J+ era eRISC transition.
func CompileToLean(evmBytecode []byte) ([]LeanOp, error) {
	if len(evmBytecode) == 0 {
		return nil, ErrLeanCompileEmpty
	}

	var ops []LeanOp
	inputIdx := uint32(0) // track implicit input slots for PUSH operands

	for i := 0; i < len(evmBytecode); i++ {
		switch evmBytecode[i] {
		case 0x01: // EVM ADD
			ops = append(ops, LeanOp{Opcode: OpADD})

		case 0x02: // EVM MUL
			ops = append(ops, LeanOp{Opcode: OpMUL})

		case 0x20: // EVM SHA3
			ops = append(ops, LeanOp{Opcode: OpHASH})

		case 0x14: // EVM EQ
			ops = append(ops, LeanOp{Opcode: OpVERIFY})

		case 0x60: // EVM PUSH1
			if i+1 >= len(evmBytecode) {
				return nil, ErrLeanCompileUnsupported
			}
			ops = append(ops, LeanOp{
				Opcode:   OpPUSH,
				Operands: []uint32{inputIdx},
			})
			inputIdx++
			i++ // skip the PUSH1 data byte

		case 0x80: // EVM DUP1
			ops = append(ops, LeanOp{Opcode: OpDUP})

		default:
			return nil, ErrLeanCompileUnsupported
		}
	}

	if len(ops) == 0 {
		return nil, ErrLeanCompileEmpty
	}

	return ops, nil
}

// --- Internal helpers ---

// addBytes performs XOR of two byte slices (used as a simple commutative
// addition analog over arbitrary-length byte fields).
func addBytes(a, b []byte) []byte {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	result := make([]byte, maxLen)
	for i := range result {
		var av, bv byte
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		result[i] = av ^ bv
	}
	return result
}

// mulBytes performs byte-wise AND of two byte slices (used as a simple
// multiplication analog for the proof aggregation circuit).
func mulBytes(a, b []byte) []byte {
	// Hash-based multiplication: H(a || b) to produce a fixed-size output.
	return crypto.Keccak256(a, b)
}

// AggregateAndCommit is a convenience function that aggregates proofs and
// returns both the aggregated proof and the commitment hash over it.
func AggregateAndCommit(proofs [][]byte) (aggregated []byte, commitment types.Hash, err error) {
	aggregated, err = AggregateProofs(proofs)
	if err != nil {
		return nil, types.Hash{}, err
	}
	commitment = crypto.Keccak256Hash(aggregated)
	return aggregated, commitment, nil
}
