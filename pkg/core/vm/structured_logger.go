package vm

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/eth2028/eth2028/core/types"
)

// StructuredLog represents a single step in EVM execution with optional
// memory and storage snapshots. This is the richer counterpart to
// StructLogEntry, designed for JSON-RPC debug_traceTransaction output.
type StructuredLog struct {
	PC      uint64                    `json:"pc"`
	Op      string                    `json:"op"`
	Gas     uint64                    `json:"gas"`
	GasCost uint64                    `json:"gasCost"`
	Depth   int                       `json:"depth"`
	Stack   []string                  `json:"stack"`
	Memory  []byte                    `json:"memory,omitempty"`
	Storage map[types.Hash]types.Hash `json:"storage,omitempty"`
	Error   string                    `json:"error,omitempty"`
}

// StructuredLoggerConfig controls which optional data the structured logger
// captures at each step.
type StructuredLoggerConfig struct {
	EnableMemory     bool
	EnableStorage    bool
	EnableReturnData bool
}

// ExecutionResult summarises a traced EVM execution.
type ExecutionResult struct {
	Gas         uint64          `json:"gas"`
	Failed      bool            `json:"failed"`
	ReturnValue []byte          `json:"returnValue"`
	Logs        []StructuredLog `json:"structLogs"`
}

// StructuredLogger implements EVMLogger and collects rich step-by-step
// execution traces with configurable memory/storage capture.
type StructuredLogger struct {
	config  StructuredLoggerConfig
	logs    []StructuredLog
	output  []byte
	err     error
	gasUsed uint64
	storage map[types.Hash]types.Hash // running storage snapshot
}

// NewStructuredLogger returns a new StructuredLogger with the given config.
func NewStructuredLogger(config StructuredLoggerConfig) *StructuredLogger {
	return &StructuredLogger{
		config:  config,
		storage: make(map[types.Hash]types.Hash),
	}
}

// CaptureStart is called at the beginning of a top-level call.
func (l *StructuredLogger) CaptureStart(from, to types.Address, create bool, input []byte, gas uint64, value *big.Int) {
	// Reset per-execution state so the logger can be reused across calls.
	l.logs = l.logs[:0]
	l.output = nil
	l.err = nil
	l.gasUsed = 0
	l.storage = make(map[types.Hash]types.Hash)
}

// CaptureState is called before each opcode execution. It records the
// current execution context into a StructuredLog entry.
func (l *StructuredLogger) CaptureState(pc uint64, op OpCode, gas, cost uint64, stack *Stack, memory *Memory, depth int, err error) {
	entry := StructuredLog{
		PC:      pc,
		Op:      op.String(),
		Gas:     gas,
		GasCost: cost,
		Depth:   depth,
	}

	// Copy the stack as hex strings (bottom to top).
	data := stack.Data()
	entry.Stack = make([]string, len(data))
	for i, v := range data {
		entry.Stack[i] = fmt.Sprintf("0x%x", v)
	}

	// Optionally capture memory.
	if l.config.EnableMemory && memory.Len() > 0 {
		memData := memory.Data()
		entry.Memory = make([]byte, len(memData))
		copy(entry.Memory, memData)
	}

	// Optionally capture storage snapshot.
	if l.config.EnableStorage {
		snap := make(map[types.Hash]types.Hash, len(l.storage))
		for k, v := range l.storage {
			snap[k] = v
		}
		entry.Storage = snap
	}

	if err != nil {
		entry.Error = err.Error()
	}

	// Track SSTORE operations so subsequent steps include the written slot.
	if l.config.EnableStorage && op == SSTORE && stack.Len() >= 2 {
		key := types.IntToHash(new(big.Int).Set(stack.Back(0)))
		val := types.IntToHash(new(big.Int).Set(stack.Back(1)))
		l.storage[key] = val
	}

	l.logs = append(l.logs, entry)
}

// CaptureEnd is called at the end of a top-level call.
func (l *StructuredLogger) CaptureEnd(output []byte, gasUsed uint64, err error) {
	if output != nil {
		l.output = make([]byte, len(output))
		copy(l.output, output)
	}
	l.gasUsed = gasUsed
	l.err = err
}

// GetLogs returns the captured structured logs.
func (l *StructuredLogger) GetLogs() []StructuredLog {
	return l.logs
}

// GetResult returns an ExecutionResult summarising the traced execution.
func (l *StructuredLogger) GetResult() *ExecutionResult {
	return &ExecutionResult{
		Gas:         l.gasUsed,
		Failed:      l.err != nil,
		ReturnValue: l.output,
		Logs:        l.logs,
	}
}

// Reset clears all captured state so the logger can be reused.
func (l *StructuredLogger) Reset() {
	l.logs = nil
	l.output = nil
	l.err = nil
	l.gasUsed = 0
	l.storage = make(map[types.Hash]types.Hash)
}

// FormatLogs formats a slice of StructuredLog entries as human-readable text
// with one line per step.
func FormatLogs(logs []StructuredLog) string {
	var b strings.Builder
	for i, log := range logs {
		fmt.Fprintf(&b, "%-4d  %-14s  gas=%-8d cost=%-6d depth=%d",
			log.PC, log.Op, log.Gas, log.GasCost, log.Depth)

		if len(log.Stack) > 0 {
			b.WriteString("  stack=[")
			for j, v := range log.Stack {
				if j > 0 {
					b.WriteString(", ")
				}
				b.WriteString(v)
			}
			b.WriteString("]")
		}

		if len(log.Memory) > 0 {
			fmt.Fprintf(&b, "  mem=%x", log.Memory)
		}

		if log.Error != "" {
			fmt.Fprintf(&b, "  err=%q", log.Error)
		}

		if i < len(logs)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
