// evm_log_ops.go implements LOG0-LOG4 operations for the EVM, including
// topic count validation, log entry construction, gas calculation
// (375 + 8*dataSize + 375*topicCount), memory expansion for log data,
// and log accumulation for receipt generation.
package vm

import (
	"errors"
	"fmt"
	"math"

	"github.com/eth2030/eth2030/core/types"
)

// Log operation errors.
var (
	ErrLogReadOnly         = errors.New("log: write operation in read-only context")
	ErrLogNoState          = errors.New("log: no state database for log accumulation")
	ErrLogTopicCount       = errors.New("log: invalid topic count, must be 0-4")
	ErrLogGasOverflow      = errors.New("log: gas cost calculation overflow")
	ErrLogMemoryOverflow   = errors.New("log: data size exceeds memory capacity")
	ErrLogDataSizeTooLarge = errors.New("log: data size too large")
)

// LogGasConfig holds gas cost parameters for LOG operations.
type LogGasConfig struct {
	BaseCost  uint64 // base gas per LOG operation (375)
	TopicCost uint64 // gas per topic (375)
	DataCost  uint64 // gas per byte of log data (8)
}

// DefaultLogGasConfig returns the standard LOG gas costs.
func DefaultLogGasConfig() LogGasConfig {
	return LogGasConfig{
		BaseCost:  GasLog,      // 375
		TopicCost: GasLogTopic, // 375
		DataCost:  GasLogData,  // 8
	}
}

// LogOpHandler executes LOG0-LOG4 operations with gas calculation, topic
// validation, and StateDB integration for log accumulation.
type LogOpHandler struct {
	config LogGasConfig
}

// NewLogOpHandler creates a handler with standard gas costs.
func NewLogOpHandler() *LogOpHandler {
	return &LogOpHandler{config: DefaultLogGasConfig()}
}

// NewLogOpHandlerWithConfig creates a handler with custom gas costs.
func NewLogOpHandlerWithConfig(config LogGasConfig) *LogOpHandler {
	return &LogOpHandler{config: config}
}

// CalcLogGasCost computes the total gas cost for a LOG operation.
// totalGas = baseCost + topicCount*topicCost + dataSize*dataCost
// Returns an error on overflow or invalid topic count.
func (h *LogOpHandler) CalcLogGasCost(topicCount int, dataSize uint64) (uint64, error) {
	if topicCount < 0 || topicCount > 4 {
		return 0, fmt.Errorf("%w: got %d", ErrLogTopicCount, topicCount)
	}

	// Base cost: 375
	gas := h.config.BaseCost

	// Topic cost: topicCount * 375
	topicGas := logSafeMul(uint64(topicCount), h.config.TopicCost)
	gas = logSafeAdd(gas, topicGas)

	// Data cost: dataSize * 8
	dataGas := logSafeMul(dataSize, h.config.DataCost)
	gas = logSafeAdd(gas, dataGas)

	// Check for overflow.
	if gas == math.MaxUint64 && dataSize > 0 {
		return 0, ErrLogGasOverflow
	}

	return gas, nil
}

// CalcLogMemoryExpansion computes the memory expansion gas for a LOG operation.
// The log data region [offset, offset+size) must fit in memory.
func (h *LogOpHandler) CalcLogMemoryExpansion(currentMemSize, offset, size uint64) uint64 {
	if size == 0 {
		return 0
	}
	requiredSize := offset + size
	if requiredSize < offset {
		// Overflow: return max to signal out-of-gas.
		return math.MaxUint64
	}
	return MemoryExpansionGas(currentMemSize, requiredSize)
}

// CalcLogTotalGas computes the complete gas cost for a LOG including memory
// expansion. This is the sum of the LOG gas cost and memory expansion gas.
func (h *LogOpHandler) CalcLogTotalGas(
	topicCount int,
	dataSize uint64,
	currentMemSize, offset uint64,
) (uint64, error) {
	logGas, err := h.CalcLogGasCost(topicCount, dataSize)
	if err != nil {
		return 0, err
	}

	memGas := h.CalcLogMemoryExpansion(currentMemSize, offset, dataSize)
	total := logSafeAdd(logGas, memGas)
	if total == math.MaxUint64 {
		return 0, ErrLogGasOverflow
	}
	return total, nil
}

// BuildLogEntry creates a types.Log from the given parameters.
// It validates the topic count and copies all data.
func (h *LogOpHandler) BuildLogEntry(
	contractAddr types.Address,
	topics []types.Hash,
	data []byte,
) (*types.Log, error) {
	if len(topics) > 4 {
		return nil, fmt.Errorf("%w: got %d", ErrLogTopicCount, len(topics))
	}

	// Deep copy topics and data to prevent aliasing.
	topicsCopy := make([]types.Hash, len(topics))
	copy(topicsCopy, topics)

	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	return &types.Log{
		Address: contractAddr,
		Topics:  topicsCopy,
		Data:    dataCopy,
	}, nil
}

// ExecLog executes a full LOG operation: validates context, calculates gas,
// builds the log entry, and adds it to the StateDB.
// Returns the total gas cost.
func (h *LogOpHandler) ExecLog(
	stateDB StateDB,
	contractAddr types.Address,
	topics []types.Hash,
	data []byte,
	readOnly bool,
) (uint64, error) {
	if readOnly {
		return 0, ErrLogReadOnly
	}
	if stateDB == nil {
		return 0, ErrLogNoState
	}
	if len(topics) > 4 {
		return 0, fmt.Errorf("%w: got %d", ErrLogTopicCount, len(topics))
	}

	gas, err := h.CalcLogGasCost(len(topics), uint64(len(data)))
	if err != nil {
		return 0, err
	}

	entry, err := h.BuildLogEntry(contractAddr, topics, data)
	if err != nil {
		return 0, err
	}

	stateDB.AddLog(entry)
	return gas, nil
}

// LogAccumulator collects log entries during EVM execution and provides
// them for receipt generation. It supports snapshots for revert handling.
type LogAccumulator struct {
	logs            []*types.Log
	snapshotLengths []int
}

// NewLogAccumulator creates an empty accumulator.
func NewLogAccumulator() *LogAccumulator {
	return &LogAccumulator{}
}

// AddLog appends a log entry.
func (la *LogAccumulator) AddLog(log *types.Log) {
	la.logs = append(la.logs, log)
}

// Logs returns all accumulated log entries.
func (la *LogAccumulator) Logs() []*types.Log {
	return la.logs
}

// LogCount returns the number of accumulated logs.
func (la *LogAccumulator) LogCount() int {
	return len(la.logs)
}

// Snapshot records the current number of logs for revert support.
func (la *LogAccumulator) Snapshot() int {
	id := len(la.snapshotLengths)
	la.snapshotLengths = append(la.snapshotLengths, len(la.logs))
	return id
}

// RevertToSnapshot discards logs added after the given snapshot.
func (la *LogAccumulator) RevertToSnapshot(id int) {
	if id < 0 || id >= len(la.snapshotLengths) {
		return
	}
	logLen := la.snapshotLengths[id]
	la.logs = la.logs[:logLen]
	la.snapshotLengths = la.snapshotLengths[:id]
}

// Reset clears all logs and snapshots.
func (la *LogAccumulator) Reset() {
	la.logs = la.logs[:0]
	la.snapshotLengths = la.snapshotLengths[:0]
}

// LogGasTable provides a per-topic-count lookup table for the constant
// gas portion of LOG operations (excludes data cost and memory expansion).
type LogGasTable struct {
	costs [5]uint64 // LOG0..LOG4 constant gas (base + topics)
}

// NewLogGasTable precomputes the constant gas for each LOG opcode.
func NewLogGasTable() *LogGasTable {
	config := DefaultLogGasConfig()
	t := &LogGasTable{}
	for i := 0; i <= 4; i++ {
		t.costs[i] = config.BaseCost + uint64(i)*config.TopicCost
	}
	return t
}

// ConstantGas returns the constant gas portion for LOG with n topics.
// This does not include the per-byte data cost or memory expansion.
func (t *LogGasTable) ConstantGas(topicCount int) uint64 {
	if topicCount < 0 || topicCount > 4 {
		return 0
	}
	return t.costs[topicCount]
}

// DynamicGas returns the per-byte data gas for the given data size.
func (t *LogGasTable) DynamicGas(dataSize uint64) uint64 {
	return logSafeMul(dataSize, GasLogData)
}

// TotalGas returns the total gas (constant + data) without memory expansion.
func (t *LogGasTable) TotalGas(topicCount int, dataSize uint64) uint64 {
	return logSafeAdd(t.ConstantGas(topicCount), t.DynamicGas(dataSize))
}

// logSafeAdd returns a+b, capping at math.MaxUint64 on overflow.
func logSafeAdd(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

// logSafeMul returns a*b, capping at math.MaxUint64 on overflow.
func logSafeMul(a, b uint64) uint64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > math.MaxUint64/b {
		return math.MaxUint64
	}
	return a * b
}
