// execution_context.go implements the execution context for the EXECUTE
// precompile in native rollups (EIP-8079). It manages gas metering,
// nested execution depth tracking, and result verification for cross-rollup
// calls routed through the L1 EXECUTE precompile address.
//
// The ExecutionContext carries the execution environment: gas budgets,
// call depth, input/output buffers, and a trace of nested calls. It
// enforces depth limits, gas accounting, and deterministic result
// verification so that the L1 validator can confirm execution correctness.
package rollup

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Execution context errors.
var (
	ErrExecCtxDepthExceeded   = errors.New("exec_context: maximum call depth exceeded")
	ErrExecCtxGasExhausted    = errors.New("exec_context: insufficient gas for execution")
	ErrExecCtxNilInput        = errors.New("exec_context: nil input data")
	ErrExecCtxZeroTarget      = errors.New("exec_context: target rollup ID is zero")
	ErrExecCtxAlreadyFinished = errors.New("exec_context: context already finished")
	ErrExecCtxResultMismatch  = errors.New("exec_context: result hash does not match expected")
	ErrExecCtxNotFinished     = errors.New("exec_context: context not yet finished")
	ErrExecCtxEmptyTrace      = errors.New("exec_context: trace is empty")
)

// ExecutePrecompileAddr is the sentinel address for the EXECUTE precompile
// as defined in EIP-8079. Calls to this address trigger cross-rollup execution.
const ExecutePrecompileAddr = "0x0100000000000000000000000000000000000100"

// DefaultMaxCallDepth is the maximum nested EXECUTE call depth.
const DefaultMaxCallDepth = 32

// DefaultMaxGasPerExec is the default gas budget per EXECUTE call (10M gas).
const DefaultMaxGasPerExec = 10_000_000

// ExecCallRecord records a single EXECUTE precompile call.
type ExecCallRecord struct {
	// Depth is the call depth at which this call was made (0 = top-level).
	Depth int

	// TargetRollupID is the rollup targeted by this EXECUTE call.
	TargetRollupID uint64

	// Caller is the address that initiated the call.
	Caller types.Address

	// Input is the calldata passed to the EXECUTE precompile.
	Input []byte

	// GasProvided is the gas allocated for this call.
	GasProvided uint64

	// GasUsed is the gas consumed by this call.
	GasUsed uint64

	// Success indicates whether the call succeeded.
	Success bool

	// OutputHash is the keccak256 of the output data.
	OutputHash types.Hash
}

// ExecutionContextConfig configures the execution context.
type ExecutionContextConfig struct {
	// MaxCallDepth is the maximum nesting depth for EXECUTE calls.
	MaxCallDepth int

	// MaxGasPerExec is the maximum gas budget per EXECUTE call.
	MaxGasPerExec uint64
}

// DefaultExecutionContextConfig returns sensible defaults.
func DefaultExecutionContextConfig() ExecutionContextConfig {
	return ExecutionContextConfig{
		MaxCallDepth:  DefaultMaxCallDepth,
		MaxGasPerExec: DefaultMaxGasPerExec,
	}
}

// ExecutionContext manages the state of an EXECUTE precompile invocation,
// including gas metering, depth tracking, and call tracing. Thread-safe.
type ExecutionContext struct {
	mu     sync.Mutex
	config ExecutionContextConfig

	// rollupID is the source rollup initiating the execution.
	rollupID uint64

	// currentDepth tracks the current call depth.
	currentDepth int

	// gasRemaining tracks remaining gas for the entire execution.
	gasRemaining uint64

	// gasUsed tracks total gas consumed.
	gasUsed uint64

	// trace records all EXECUTE calls in order.
	trace []ExecCallRecord

	// finished indicates whether the context has been finalized.
	finished bool

	// resultHash is the hash of the final execution result, set on finish.
	resultHash types.Hash
}

// NewExecutionContext creates a new execution context for the given source
// rollup with the specified gas budget.
func NewExecutionContext(rollupID uint64, gasBudget uint64, config ExecutionContextConfig) *ExecutionContext {
	if config.MaxCallDepth <= 0 {
		config.MaxCallDepth = DefaultMaxCallDepth
	}
	if config.MaxGasPerExec == 0 {
		config.MaxGasPerExec = DefaultMaxGasPerExec
	}
	if gasBudget > config.MaxGasPerExec {
		gasBudget = config.MaxGasPerExec
	}
	return &ExecutionContext{
		config:       config,
		rollupID:     rollupID,
		gasRemaining: gasBudget,
		trace:        make([]ExecCallRecord, 0, 8),
	}
}

// BeginCall starts a nested EXECUTE call. It checks the depth limit and
// reserves gas. Returns an error if the depth limit is exceeded or
// insufficient gas is available.
func (ec *ExecutionContext) BeginCall(targetRollupID uint64, caller types.Address, input []byte, gas uint64) error {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	if ec.finished {
		return ErrExecCtxAlreadyFinished
	}
	if targetRollupID == 0 {
		return ErrExecCtxZeroTarget
	}
	if input == nil {
		return ErrExecCtxNilInput
	}
	if ec.currentDepth >= ec.config.MaxCallDepth {
		return fmt.Errorf("%w: depth=%d, max=%d", ErrExecCtxDepthExceeded, ec.currentDepth, ec.config.MaxCallDepth)
	}
	if gas > ec.gasRemaining {
		return fmt.Errorf("%w: need=%d, remaining=%d", ErrExecCtxGasExhausted, gas, ec.gasRemaining)
	}

	// Reserve the gas.
	ec.gasRemaining -= gas

	// Record the call (GasUsed and Success will be updated on EndCall).
	ec.trace = append(ec.trace, ExecCallRecord{
		Depth:          ec.currentDepth,
		TargetRollupID: targetRollupID,
		Caller:         caller,
		Input:          copyBytes(input),
		GasProvided:    gas,
	})

	ec.currentDepth++
	return nil
}

// EndCall completes the most recently started EXECUTE call. It updates the
// gas accounting and records the result.
func (ec *ExecutionContext) EndCall(gasUsed uint64, success bool, output []byte) error {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	if ec.finished {
		return ErrExecCtxAlreadyFinished
	}
	if len(ec.trace) == 0 {
		return ErrExecCtxEmptyTrace
	}
	if ec.currentDepth <= 0 {
		return ErrExecCtxEmptyTrace
	}

	ec.currentDepth--

	// Find the latest unfinished call record at the current depth.
	idx := len(ec.trace) - 1
	for idx >= 0 && ec.trace[idx].Depth != ec.currentDepth {
		idx--
	}
	if idx < 0 {
		return ErrExecCtxEmptyTrace
	}

	record := &ec.trace[idx]

	// Cap gas used to gas provided.
	if gasUsed > record.GasProvided {
		gasUsed = record.GasProvided
	}
	record.GasUsed = gasUsed
	record.Success = success

	if len(output) > 0 {
		record.OutputHash = crypto.Keccak256Hash(output)
	}

	ec.gasUsed += gasUsed

	// Refund unused gas.
	refund := record.GasProvided - gasUsed
	ec.gasRemaining += refund

	return nil
}

// Finish marks the execution context as complete and computes the result hash.
// The result hash is a commitment over the entire execution trace.
func (ec *ExecutionContext) Finish() (types.Hash, error) {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	if ec.finished {
		return types.Hash{}, ErrExecCtxAlreadyFinished
	}

	ec.finished = true
	ec.resultHash = computeExecResultHash(ec.rollupID, ec.gasUsed, ec.trace)
	return ec.resultHash, nil
}

// VerifyResult checks that a claimed result hash matches the execution
// context's computed result. The context must be finished first.
func (ec *ExecutionContext) VerifyResult(claimedHash types.Hash) (bool, error) {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	if !ec.finished {
		return false, ErrExecCtxNotFinished
	}
	if claimedHash != ec.resultHash {
		return false, ErrExecCtxResultMismatch
	}
	return true, nil
}

// GasRemaining returns the remaining gas budget.
func (ec *ExecutionContext) GasRemaining() uint64 {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return ec.gasRemaining
}

// GasUsed returns the total gas consumed so far.
func (ec *ExecutionContext) GasUsed() uint64 {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return ec.gasUsed
}

// CurrentDepth returns the current call nesting depth.
func (ec *ExecutionContext) CurrentDepth() int {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return ec.currentDepth
}

// TraceLength returns the number of recorded calls.
func (ec *ExecutionContext) TraceLength() int {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return len(ec.trace)
}

// Trace returns a copy of the execution trace.
func (ec *ExecutionContext) Trace() []ExecCallRecord {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	result := make([]ExecCallRecord, len(ec.trace))
	for i, r := range ec.trace {
		result[i] = r
		result[i].Input = copyBytes(r.Input)
	}
	return result
}

// IsFinished returns whether the execution context has been finalized.
func (ec *ExecutionContext) IsFinished() bool {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return ec.finished
}

// RollupID returns the source rollup ID.
func (ec *ExecutionContext) RollupID() uint64 {
	return ec.rollupID
}

// --- Internal helpers ---

// computeExecResultHash builds a deterministic hash over the execution trace.
func computeExecResultHash(rollupID, gasUsed uint64, trace []ExecCallRecord) types.Hash {
	var data []byte
	var buf [8]byte

	binary.BigEndian.PutUint64(buf[:], rollupID)
	data = append(data, buf[:]...)

	binary.BigEndian.PutUint64(buf[:], gasUsed)
	data = append(data, buf[:]...)

	for _, r := range trace {
		binary.BigEndian.PutUint64(buf[:], r.TargetRollupID)
		data = append(data, buf[:]...)
		data = append(data, r.Caller[:]...)
		binary.BigEndian.PutUint64(buf[:], r.GasUsed)
		data = append(data, buf[:]...)
		if r.Success {
			data = append(data, 1)
		} else {
			data = append(data, 0)
		}
		data = append(data, r.OutputHash[:]...)
	}

	return crypto.Keccak256Hash(data)
}

// copyBytes returns a copy of the input byte slice.
func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}
