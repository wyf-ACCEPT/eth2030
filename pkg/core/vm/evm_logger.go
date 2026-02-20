package vm

// evm_logger.go implements a configurable EVM execution tracer that provides
// step-by-step opcode logging with gas tracking, stack/memory snapshots,
// call enter/exit hooks, and structured trace output. It extends the base
// EVMLogger interface with richer tracing capabilities for debugging and
// analysis.

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strings"

	"github.com/eth2028/eth2028/core/types"
)

// TraceVerbosity controls the level of detail captured by the EVMTracer.
type TraceVerbosity uint8

const (
	// VerbosityMinimal captures only opcodes and gas.
	VerbosityMinimal TraceVerbosity = iota
	// VerbosityStandard adds stack snapshots at each step.
	VerbosityStandard
	// VerbosityDetailed adds memory snapshots and storage tracking.
	VerbosityDetailed
	// VerbosityFull captures everything including return data.
	VerbosityFull
)

// TraceConfig controls what the EVMTracer captures at each step.
type TraceConfig struct {
	Verbosity    TraceVerbosity
	MaxStackLen  int  // max stack items to capture per step (0 = unlimited)
	MaxMemoryLen int  // max memory bytes to capture per step (0 = unlimited)
	DisableGas   bool // if true, skip gas tracking to reduce overhead
}

// DefaultTraceConfig returns a TraceConfig with standard verbosity.
func DefaultTraceConfig() TraceConfig {
	return TraceConfig{
		Verbosity: VerbosityStandard,
	}
}

// TraceStep represents one opcode execution step in a trace.
type TraceStep struct {
	PC       uint64   `json:"pc"`
	OpCode   string   `json:"op"`
	OpByte   byte     `json:"opByte"`
	Gas      uint64   `json:"gas"`
	GasCost  uint64   `json:"gasCost"`
	Depth    int      `json:"depth"`
	Stack    []string `json:"stack,omitempty"`
	Memory   string   `json:"memory,omitempty"`
	MemSize  int      `json:"memSize"`
	Error    string   `json:"error,omitempty"`
}

// TraceCallEvent captures a call enter or exit.
type TraceCallEvent struct {
	Type     string        `json:"type"`               // "enter" or "exit"
	CallType string        `json:"callType"`           // CALL, STATICCALL, etc.
	From     types.Address `json:"from"`
	To       types.Address `json:"to"`
	Gas      uint64        `json:"gas"`
	Value    *big.Int      `json:"value,omitempty"`
	Input    string        `json:"input,omitempty"`    // hex-encoded for enter
	Output   string        `json:"output,omitempty"`   // hex-encoded for exit
	GasUsed  uint64        `json:"gasUsed,omitempty"`  // only for exit
	Error    string        `json:"error,omitempty"`    // only for exit
}

// EVMTracer implements EVMLogger with configurable verbosity and structured
// output. It supports both in-memory trace collection and streaming output.
type EVMTracer struct {
	config    TraceConfig
	steps     []TraceStep
	calls     []TraceCallEvent
	output    []byte
	err       error
	gasUsed   uint64
	writer    io.Writer // optional streaming output
	gasAccum  map[string]uint64 // opcode -> total gas consumed
}

// NewEVMTracer creates a tracer with the given configuration.
func NewEVMTracer(config TraceConfig) *EVMTracer {
	return &EVMTracer{
		config:   config,
		gasAccum: make(map[string]uint64),
	}
}

// NewStreamingEVMTracer creates a tracer that writes each step to w as it
// occurs, in addition to collecting steps in memory.
func NewStreamingEVMTracer(config TraceConfig, w io.Writer) *EVMTracer {
	t := NewEVMTracer(config)
	t.writer = w
	return t
}

// CaptureStart is called at the beginning of a top-level call.
func (t *EVMTracer) CaptureStart(from, to types.Address, create bool, input []byte, gas uint64, value *big.Int) {
	// Reset state for a new trace.
	t.steps = t.steps[:0]
	t.calls = t.calls[:0]
	t.output = nil
	t.err = nil
	t.gasUsed = 0
	t.gasAccum = make(map[string]uint64)

	callType := "CALL"
	if create {
		callType = "CREATE"
	}

	event := TraceCallEvent{
		Type:     "enter",
		CallType: callType,
		From:     from,
		To:       to,
		Gas:      gas,
		Value:    value,
		Input:    fmt.Sprintf("%x", input),
	}
	t.calls = append(t.calls, event)

	if t.writer != nil {
		t.writeJSON(event)
	}
}

// CaptureState is called before each opcode execution.
func (t *EVMTracer) CaptureState(pc uint64, op OpCode, gas, cost uint64, stack *Stack, memory *Memory, depth int, err error) {
	step := TraceStep{
		PC:      pc,
		OpCode:  op.String(),
		OpByte:  byte(op),
		Gas:     gas,
		GasCost: cost,
		Depth:   depth,
		MemSize: memory.Len(),
	}

	// Capture stack based on verbosity.
	if t.config.Verbosity >= VerbosityStandard {
		step.Stack = t.captureStack(stack)
	}

	// Capture memory based on verbosity.
	if t.config.Verbosity >= VerbosityDetailed && memory.Len() > 0 {
		step.Memory = t.captureMemory(memory)
	}

	if err != nil {
		step.Error = err.Error()
	}

	// Accumulate gas per opcode.
	if !t.config.DisableGas {
		t.gasAccum[step.OpCode] += cost
	}

	t.steps = append(t.steps, step)

	// Stream the step if a writer is configured.
	if t.writer != nil {
		t.writeJSON(step)
	}
}

// CaptureEnd is called at the end of a top-level call.
func (t *EVMTracer) CaptureEnd(output []byte, gasUsed uint64, err error) {
	if output != nil {
		t.output = make([]byte, len(output))
		copy(t.output, output)
	}
	t.gasUsed = gasUsed
	t.err = err

	event := TraceCallEvent{
		Type:    "exit",
		GasUsed: gasUsed,
		Output:  fmt.Sprintf("%x", output),
	}
	if err != nil {
		event.Error = err.Error()
	}
	t.calls = append(t.calls, event)

	if t.writer != nil {
		t.writeJSON(event)
	}
}

// CaptureEnter is called when entering a nested call (CALL, CREATE, etc.).
// This is not part of the base EVMLogger interface but provides richer
// tracing for call tree analysis.
func (t *EVMTracer) CaptureEnter(callType CallFrameType, from, to types.Address, input []byte, gas uint64, value *big.Int) {
	event := TraceCallEvent{
		Type:     "enter",
		CallType: callType.String(),
		From:     from,
		To:       to,
		Gas:      gas,
		Value:    value,
		Input:    fmt.Sprintf("%x", input),
	}
	t.calls = append(t.calls, event)

	if t.writer != nil {
		t.writeJSON(event)
	}
}

// CaptureExit is called when exiting a nested call.
func (t *EVMTracer) CaptureExit(output []byte, gasUsed uint64, err error) {
	event := TraceCallEvent{
		Type:    "exit",
		GasUsed: gasUsed,
		Output:  fmt.Sprintf("%x", output),
	}
	if err != nil {
		event.Error = err.Error()
	}
	t.calls = append(t.calls, event)

	if t.writer != nil {
		t.writeJSON(event)
	}
}

// captureStack creates a string snapshot of the stack.
func (t *EVMTracer) captureStack(stack *Stack) []string {
	data := stack.Data()
	limit := len(data)
	if t.config.MaxStackLen > 0 && limit > t.config.MaxStackLen {
		limit = t.config.MaxStackLen
	}
	// Capture from top of stack (most recent items first).
	result := make([]string, limit)
	for i := 0; i < limit; i++ {
		idx := len(data) - 1 - i
		if idx >= 0 {
			result[i] = fmt.Sprintf("0x%x", data[idx])
		}
	}
	return result
}

// captureMemory creates a hex snapshot of memory.
func (t *EVMTracer) captureMemory(memory *Memory) string {
	data := memory.Data()
	limit := len(data)
	if t.config.MaxMemoryLen > 0 && limit > t.config.MaxMemoryLen {
		limit = t.config.MaxMemoryLen
	}
	return fmt.Sprintf("%x", data[:limit])
}

// writeJSON writes a JSON-encoded value followed by a newline to the writer.
func (t *EVMTracer) writeJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = t.writer.Write(data)
}

// --- Query methods ---

// Steps returns all captured trace steps.
func (t *EVMTracer) Steps() []TraceStep {
	return t.steps
}

// StepCount returns the number of captured steps.
func (t *EVMTracer) StepCount() int {
	return len(t.steps)
}

// Calls returns all captured call events.
func (t *EVMTracer) Calls() []TraceCallEvent {
	return t.calls
}

// Output returns the return data from the traced execution.
func (t *EVMTracer) Output() []byte {
	return t.output
}

// GasUsed returns the total gas consumed.
func (t *EVMTracer) GasUsed() uint64 {
	return t.gasUsed
}

// Error returns the error from the traced execution, if any.
func (t *EVMTracer) Error() error {
	return t.err
}

// GasByOpcode returns a map of opcode name to total gas consumed by
// that opcode across the entire trace.
func (t *EVMTracer) GasByOpcode() map[string]uint64 {
	result := make(map[string]uint64, len(t.gasAccum))
	for k, v := range t.gasAccum {
		result[k] = v
	}
	return result
}

// TopGasConsumers returns the top N opcodes by gas consumption as a
// formatted string, useful for profiling contract execution.
func (t *EVMTracer) TopGasConsumers(n int) string {
	type entry struct {
		opcode string
		gas    uint64
	}
	// Collect and sort by gas (simple insertion sort for small N).
	entries := make([]entry, 0, len(t.gasAccum))
	for op, gas := range t.gasAccum {
		entries = append(entries, entry{op, gas})
	}
	// Sort descending by gas.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].gas > entries[j-1].gas; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}

	if n > len(entries) {
		n = len(entries)
	}

	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%-14s %d gas", entries[i].opcode, entries[i].gas)
	}
	return b.String()
}

// FormatTrace returns a human-readable multi-line trace of all captured
// steps. Each line shows the PC, opcode, gas, cost, depth, and optionally
// the top stack elements.
func (t *EVMTracer) FormatTrace() string {
	var b strings.Builder
	for i, step := range t.steps {
		fmt.Fprintf(&b, "%-4d  %-14s  gas=%-8d cost=%-6d depth=%d",
			step.PC, step.OpCode, step.Gas, step.GasCost, step.Depth)

		if len(step.Stack) > 0 {
			b.WriteString("  stack=[")
			for j, v := range step.Stack {
				if j > 0 {
					b.WriteString(", ")
				}
				b.WriteString(v)
			}
			b.WriteString("]")
		}

		if step.Error != "" {
			fmt.Fprintf(&b, "  err=%q", step.Error)
		}

		if i < len(t.steps)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Reset clears all captured state so the tracer can be reused.
func (t *EVMTracer) Reset() {
	t.steps = nil
	t.calls = nil
	t.output = nil
	t.err = nil
	t.gasUsed = 0
	t.gasAccum = make(map[string]uint64)
}
