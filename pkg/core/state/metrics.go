package state

import (
	"sync/atomic"
	"time"
)

// StateMetrics tracks execution layer state operation metrics.
// All counters use sync/atomic for thread-safe concurrent access.
type StateMetrics struct {
	AccountsRead      uint64
	AccountsWritten   uint64
	StorageReads      uint64
	StorageWrites     uint64
	CodeReads         uint64
	CodeWrites        uint64
	BytesRead         uint64
	BytesWritten      uint64
	SnapshotCount     int64
	RevertCount       int64
	SelfDestructCount int64
	TotalGasUsed      uint64
	BlockNumber       uint64
	Timestamp         int64
}

// NewStateMetrics creates a new StateMetrics with the current timestamp.
func NewStateMetrics() *StateMetrics {
	return &StateMetrics{
		Timestamp: time.Now().UnixNano(),
	}
}

// RecordAccountRead increments the account read counter.
func (m *StateMetrics) RecordAccountRead() {
	atomic.AddUint64(&m.AccountsRead, 1)
}

// RecordAccountWrite increments the account write counter.
func (m *StateMetrics) RecordAccountWrite() {
	atomic.AddUint64(&m.AccountsWritten, 1)
}

// RecordStorageRead increments the storage read counter and adds bytes read.
func (m *StateMetrics) RecordStorageRead(bytes int) {
	atomic.AddUint64(&m.StorageReads, 1)
	atomic.AddUint64(&m.BytesRead, uint64(bytes))
}

// RecordStorageWrite increments the storage write counter and adds bytes written.
func (m *StateMetrics) RecordStorageWrite(bytes int) {
	atomic.AddUint64(&m.StorageWrites, 1)
	atomic.AddUint64(&m.BytesWritten, uint64(bytes))
}

// RecordCodeRead increments the code read counter and adds bytes read.
func (m *StateMetrics) RecordCodeRead(size int) {
	atomic.AddUint64(&m.CodeReads, 1)
	atomic.AddUint64(&m.BytesRead, uint64(size))
}

// RecordCodeWrite increments the code write counter and adds bytes written.
func (m *StateMetrics) RecordCodeWrite(size int) {
	atomic.AddUint64(&m.CodeWrites, 1)
	atomic.AddUint64(&m.BytesWritten, uint64(size))
}

// RecordSnapshot increments the snapshot counter.
func (m *StateMetrics) RecordSnapshot() {
	atomic.AddInt64(&m.SnapshotCount, 1)
}

// RecordRevert increments the revert counter.
func (m *StateMetrics) RecordRevert() {
	atomic.AddInt64(&m.RevertCount, 1)
}

// RecordSelfDestruct increments the self-destruct counter.
func (m *StateMetrics) RecordSelfDestruct() {
	atomic.AddInt64(&m.SelfDestructCount, 1)
}

// RecordGas adds the given gas amount to the total gas used.
func (m *StateMetrics) RecordGas(gas uint64) {
	atomic.AddUint64(&m.TotalGasUsed, gas)
}

// Reset clears all counters and sets the block number and timestamp.
func (m *StateMetrics) Reset(blockNumber uint64) {
	atomic.StoreUint64(&m.AccountsRead, 0)
	atomic.StoreUint64(&m.AccountsWritten, 0)
	atomic.StoreUint64(&m.StorageReads, 0)
	atomic.StoreUint64(&m.StorageWrites, 0)
	atomic.StoreUint64(&m.CodeReads, 0)
	atomic.StoreUint64(&m.CodeWrites, 0)
	atomic.StoreUint64(&m.BytesRead, 0)
	atomic.StoreUint64(&m.BytesWritten, 0)
	atomic.StoreInt64(&m.SnapshotCount, 0)
	atomic.StoreInt64(&m.RevertCount, 0)
	atomic.StoreInt64(&m.SelfDestructCount, 0)
	atomic.StoreUint64(&m.TotalGasUsed, 0)
	atomic.StoreUint64(&m.BlockNumber, blockNumber)
	atomic.StoreInt64(&m.Timestamp, time.Now().UnixNano())
}

// Summary returns all metrics as a map of string to uint64.
// Signed counters (SnapshotCount, RevertCount, SelfDestructCount, Timestamp)
// are cast to uint64 for uniform representation.
func (m *StateMetrics) Summary() map[string]uint64 {
	return map[string]uint64{
		"accounts_read":       atomic.LoadUint64(&m.AccountsRead),
		"accounts_written":    atomic.LoadUint64(&m.AccountsWritten),
		"storage_reads":       atomic.LoadUint64(&m.StorageReads),
		"storage_writes":      atomic.LoadUint64(&m.StorageWrites),
		"code_reads":          atomic.LoadUint64(&m.CodeReads),
		"code_writes":         atomic.LoadUint64(&m.CodeWrites),
		"bytes_read":          atomic.LoadUint64(&m.BytesRead),
		"bytes_written":       atomic.LoadUint64(&m.BytesWritten),
		"snapshot_count":      uint64(atomic.LoadInt64(&m.SnapshotCount)),
		"revert_count":        uint64(atomic.LoadInt64(&m.RevertCount)),
		"self_destruct_count": uint64(atomic.LoadInt64(&m.SelfDestructCount)),
		"total_gas_used":      atomic.LoadUint64(&m.TotalGasUsed),
		"block_number":        atomic.LoadUint64(&m.BlockNumber),
		"timestamp":           uint64(atomic.LoadInt64(&m.Timestamp)),
	}
}

// Merge adds the counters from another StateMetrics into this one.
// This is useful for aggregating metrics from parallel execution.
// BlockNumber and Timestamp are not merged; only operation counters are summed.
func (m *StateMetrics) Merge(other *StateMetrics) {
	atomic.AddUint64(&m.AccountsRead, atomic.LoadUint64(&other.AccountsRead))
	atomic.AddUint64(&m.AccountsWritten, atomic.LoadUint64(&other.AccountsWritten))
	atomic.AddUint64(&m.StorageReads, atomic.LoadUint64(&other.StorageReads))
	atomic.AddUint64(&m.StorageWrites, atomic.LoadUint64(&other.StorageWrites))
	atomic.AddUint64(&m.CodeReads, atomic.LoadUint64(&other.CodeReads))
	atomic.AddUint64(&m.CodeWrites, atomic.LoadUint64(&other.CodeWrites))
	atomic.AddUint64(&m.BytesRead, atomic.LoadUint64(&other.BytesRead))
	atomic.AddUint64(&m.BytesWritten, atomic.LoadUint64(&other.BytesWritten))
	atomic.AddInt64(&m.SnapshotCount, atomic.LoadInt64(&other.SnapshotCount))
	atomic.AddInt64(&m.RevertCount, atomic.LoadInt64(&other.RevertCount))
	atomic.AddInt64(&m.SelfDestructCount, atomic.LoadInt64(&other.SelfDestructCount))
	atomic.AddUint64(&m.TotalGasUsed, atomic.LoadUint64(&other.TotalGasUsed))
}
