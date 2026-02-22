package vm

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// EVMLogger captures EVM execution traces step by step.
type EVMLogger interface {
	// CaptureStart is called at the beginning of a top-level call.
	CaptureStart(from, to types.Address, create bool, input []byte, gas uint64, value *big.Int)
	// CaptureState is called before each opcode execution.
	CaptureState(pc uint64, op OpCode, gas, cost uint64, stack *Stack, memory *Memory, depth int, err error)
	// CaptureEnd is called at the end of a top-level call.
	CaptureEnd(output []byte, gasUsed uint64, err error)
}

// StructLogEntry is a single step recorded by StructLogTracer.
type StructLogEntry struct {
	Pc      uint64
	Op      OpCode
	Gas     uint64
	GasCost uint64
	Depth   int
	Stack   []*big.Int
	Err     error
}

// StructLogTracer collects step-by-step EVM execution logs.
type StructLogTracer struct {
	Logs    []StructLogEntry
	output  []byte
	err     error
	gasUsed uint64
}

// NewStructLogTracer returns a new StructLogTracer.
func NewStructLogTracer() *StructLogTracer {
	return &StructLogTracer{}
}

// CaptureStart records the start of a top-level call.
func (t *StructLogTracer) CaptureStart(from, to types.Address, create bool, input []byte, gas uint64, value *big.Int) {
	// Nothing to record at start; metadata is implicit in the trace result.
}

// CaptureState records one opcode step.
func (t *StructLogTracer) CaptureState(pc uint64, op OpCode, gas, cost uint64, stack *Stack, memory *Memory, depth int, err error) {
	// Copy the stack to avoid aliasing with mutable big.Ints.
	data := stack.Data()
	stackCopy := make([]*big.Int, len(data))
	for i, v := range data {
		stackCopy[i] = new(big.Int).Set(v)
	}

	t.Logs = append(t.Logs, StructLogEntry{
		Pc:      pc,
		Op:      op,
		Gas:     gas,
		GasCost: cost,
		Depth:   depth,
		Stack:   stackCopy,
		Err:     err,
	})
}

// CaptureEnd records the end of a top-level call.
func (t *StructLogTracer) CaptureEnd(output []byte, gasUsed uint64, err error) {
	t.output = output
	t.gasUsed = gasUsed
	t.err = err
}

// Output returns the return data from the traced execution.
func (t *StructLogTracer) Output() []byte { return t.output }

// GasUsed returns the total gas consumed by the traced execution.
func (t *StructLogTracer) GasUsed() uint64 { return t.gasUsed }

// Error returns the error from the traced execution, if any.
func (t *StructLogTracer) Error() error { return t.err }
