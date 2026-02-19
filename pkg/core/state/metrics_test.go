package state

import (
	"sync"
	"testing"
)

func TestNewStateMetrics(t *testing.T) {
	m := NewStateMetrics()
	if m == nil {
		t.Fatal("NewStateMetrics returned nil")
	}
	if m.Timestamp == 0 {
		t.Fatal("expected non-zero timestamp")
	}
	if m.AccountsRead != 0 || m.AccountsWritten != 0 {
		t.Fatal("expected zero counters on new metrics")
	}
}

func TestRecordOperations(t *testing.T) {
	m := NewStateMetrics()

	m.RecordAccountRead()
	m.RecordAccountRead()
	m.RecordAccountWrite()
	if m.AccountsRead != 2 {
		t.Fatalf("AccountsRead = %d, want 2", m.AccountsRead)
	}
	if m.AccountsWritten != 1 {
		t.Fatalf("AccountsWritten = %d, want 1", m.AccountsWritten)
	}

	m.RecordStorageRead(32)
	m.RecordStorageRead(64)
	if m.StorageReads != 2 {
		t.Fatalf("StorageReads = %d, want 2", m.StorageReads)
	}
	if m.BytesRead != 96 {
		t.Fatalf("BytesRead = %d, want 96", m.BytesRead)
	}

	m.RecordStorageWrite(32)
	if m.StorageWrites != 1 {
		t.Fatalf("StorageWrites = %d, want 1", m.StorageWrites)
	}
	if m.BytesWritten != 32 {
		t.Fatalf("BytesWritten = %d, want 32", m.BytesWritten)
	}

	m.RecordCodeRead(256)
	if m.CodeReads != 1 {
		t.Fatalf("CodeReads = %d, want 1", m.CodeReads)
	}
	// BytesRead should now be 96 + 256 = 352
	if m.BytesRead != 352 {
		t.Fatalf("BytesRead = %d, want 352", m.BytesRead)
	}

	m.RecordCodeWrite(512)
	if m.CodeWrites != 1 {
		t.Fatalf("CodeWrites = %d, want 1", m.CodeWrites)
	}
	// BytesWritten should now be 32 + 512 = 544
	if m.BytesWritten != 544 {
		t.Fatalf("BytesWritten = %d, want 544", m.BytesWritten)
	}

	m.RecordSnapshot()
	m.RecordSnapshot()
	m.RecordSnapshot()
	if m.SnapshotCount != 3 {
		t.Fatalf("SnapshotCount = %d, want 3", m.SnapshotCount)
	}

	m.RecordRevert()
	if m.RevertCount != 1 {
		t.Fatalf("RevertCount = %d, want 1", m.RevertCount)
	}

	m.RecordSelfDestruct()
	m.RecordSelfDestruct()
	if m.SelfDestructCount != 2 {
		t.Fatalf("SelfDestructCount = %d, want 2", m.SelfDestructCount)
	}

	m.RecordGas(21000)
	m.RecordGas(50000)
	if m.TotalGasUsed != 71000 {
		t.Fatalf("TotalGasUsed = %d, want 71000", m.TotalGasUsed)
	}
}

func TestReset(t *testing.T) {
	m := NewStateMetrics()

	// Record some operations.
	m.RecordAccountRead()
	m.RecordStorageWrite(32)
	m.RecordCodeRead(256)
	m.RecordSnapshot()
	m.RecordRevert()
	m.RecordSelfDestruct()
	m.RecordGas(21000)

	// Reset to block 42.
	m.Reset(42)

	if m.BlockNumber != 42 {
		t.Fatalf("BlockNumber = %d, want 42", m.BlockNumber)
	}
	if m.AccountsRead != 0 {
		t.Fatalf("AccountsRead = %d, want 0 after reset", m.AccountsRead)
	}
	if m.StorageWrites != 0 {
		t.Fatalf("StorageWrites = %d, want 0 after reset", m.StorageWrites)
	}
	if m.CodeReads != 0 {
		t.Fatalf("CodeReads = %d, want 0 after reset", m.CodeReads)
	}
	if m.BytesRead != 0 {
		t.Fatalf("BytesRead = %d, want 0 after reset", m.BytesRead)
	}
	if m.BytesWritten != 0 {
		t.Fatalf("BytesWritten = %d, want 0 after reset", m.BytesWritten)
	}
	if m.SnapshotCount != 0 {
		t.Fatalf("SnapshotCount = %d, want 0 after reset", m.SnapshotCount)
	}
	if m.RevertCount != 0 {
		t.Fatalf("RevertCount = %d, want 0 after reset", m.RevertCount)
	}
	if m.SelfDestructCount != 0 {
		t.Fatalf("SelfDestructCount = %d, want 0 after reset", m.SelfDestructCount)
	}
	if m.TotalGasUsed != 0 {
		t.Fatalf("TotalGasUsed = %d, want 0 after reset", m.TotalGasUsed)
	}
	if m.Timestamp == 0 {
		t.Fatal("expected non-zero timestamp after reset")
	}
}

func TestSummary(t *testing.T) {
	m := NewStateMetrics()
	m.RecordAccountRead()
	m.RecordAccountWrite()
	m.RecordStorageRead(32)
	m.RecordStorageWrite(64)
	m.RecordCodeRead(128)
	m.RecordCodeWrite(256)
	m.RecordSnapshot()
	m.RecordRevert()
	m.RecordSelfDestruct()
	m.RecordGas(21000)

	s := m.Summary()

	checks := map[string]uint64{
		"accounts_read":       1,
		"accounts_written":    1,
		"storage_reads":       1,
		"storage_writes":      1,
		"code_reads":          1,
		"code_writes":         1,
		"bytes_read":          160, // 32 + 128
		"bytes_written":       320, // 64 + 256
		"snapshot_count":      1,
		"revert_count":        1,
		"self_destruct_count": 1,
		"total_gas_used":      21000,
	}

	for key, want := range checks {
		if got, ok := s[key]; !ok {
			t.Errorf("summary missing key %q", key)
		} else if got != want {
			t.Errorf("summary[%q] = %d, want %d", key, got, want)
		}
	}

	// Check that block_number and timestamp keys exist.
	if _, ok := s["block_number"]; !ok {
		t.Error("summary missing key block_number")
	}
	if _, ok := s["timestamp"]; !ok {
		t.Error("summary missing key timestamp")
	}
}

func TestMerge(t *testing.T) {
	a := NewStateMetrics()
	a.RecordAccountRead()
	a.RecordAccountRead()
	a.RecordStorageRead(32)
	a.RecordCodeWrite(100)
	a.RecordSnapshot()
	a.RecordGas(21000)

	b := NewStateMetrics()
	b.RecordAccountRead()
	b.RecordAccountWrite()
	b.RecordStorageRead(64)
	b.RecordStorageWrite(128)
	b.RecordCodeRead(200)
	b.RecordRevert()
	b.RecordSelfDestruct()
	b.RecordGas(50000)

	a.Merge(b)

	if a.AccountsRead != 3 {
		t.Fatalf("AccountsRead = %d, want 3", a.AccountsRead)
	}
	if a.AccountsWritten != 1 {
		t.Fatalf("AccountsWritten = %d, want 1", a.AccountsWritten)
	}
	if a.StorageReads != 2 {
		t.Fatalf("StorageReads = %d, want 2", a.StorageReads)
	}
	if a.StorageWrites != 1 {
		t.Fatalf("StorageWrites = %d, want 1", a.StorageWrites)
	}
	if a.CodeReads != 1 {
		t.Fatalf("CodeReads = %d, want 1", a.CodeReads)
	}
	if a.CodeWrites != 1 {
		t.Fatalf("CodeWrites = %d, want 1", a.CodeWrites)
	}
	if a.BytesRead != 296 { // 32 + 64 + 200
		t.Fatalf("BytesRead = %d, want 296", a.BytesRead)
	}
	if a.BytesWritten != 228 { // 100 + 128
		t.Fatalf("BytesWritten = %d, want 228", a.BytesWritten)
	}
	if a.SnapshotCount != 1 {
		t.Fatalf("SnapshotCount = %d, want 1", a.SnapshotCount)
	}
	if a.RevertCount != 1 {
		t.Fatalf("RevertCount = %d, want 1", a.RevertCount)
	}
	if a.SelfDestructCount != 1 {
		t.Fatalf("SelfDestructCount = %d, want 1", a.SelfDestructCount)
	}
	if a.TotalGasUsed != 71000 {
		t.Fatalf("TotalGasUsed = %d, want 71000", a.TotalGasUsed)
	}
}

func TestMetricsConcurrentAccess(t *testing.T) {
	m := NewStateMetrics()
	const goroutines = 100
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				m.RecordAccountRead()
				m.RecordAccountWrite()
				m.RecordStorageRead(32)
				m.RecordStorageWrite(32)
				m.RecordCodeRead(64)
				m.RecordCodeWrite(64)
				m.RecordSnapshot()
				m.RecordRevert()
				m.RecordSelfDestruct()
				m.RecordGas(21000)
			}
		}()
	}

	wg.Wait()

	total := uint64(goroutines * opsPerGoroutine)
	if m.AccountsRead != total {
		t.Fatalf("AccountsRead = %d, want %d", m.AccountsRead, total)
	}
	if m.AccountsWritten != total {
		t.Fatalf("AccountsWritten = %d, want %d", m.AccountsWritten, total)
	}
	if m.StorageReads != total {
		t.Fatalf("StorageReads = %d, want %d", m.StorageReads, total)
	}
	if m.StorageWrites != total {
		t.Fatalf("StorageWrites = %d, want %d", m.StorageWrites, total)
	}
	if m.CodeReads != total {
		t.Fatalf("CodeReads = %d, want %d", m.CodeReads, total)
	}
	if m.CodeWrites != total {
		t.Fatalf("CodeWrites = %d, want %d", m.CodeWrites, total)
	}
	if m.BytesRead != total*(32+64) {
		t.Fatalf("BytesRead = %d, want %d", m.BytesRead, total*(32+64))
	}
	if m.BytesWritten != total*(32+64) {
		t.Fatalf("BytesWritten = %d, want %d", m.BytesWritten, total*(32+64))
	}
	if m.SnapshotCount != int64(total) {
		t.Fatalf("SnapshotCount = %d, want %d", m.SnapshotCount, total)
	}
	if m.RevertCount != int64(total) {
		t.Fatalf("RevertCount = %d, want %d", m.RevertCount, total)
	}
	if m.SelfDestructCount != int64(total) {
		t.Fatalf("SelfDestructCount = %d, want %d", m.SelfDestructCount, total)
	}
	if m.TotalGasUsed != total*21000 {
		t.Fatalf("TotalGasUsed = %d, want %d", m.TotalGasUsed, total*21000)
	}
}

func TestConcurrentMerge(t *testing.T) {
	m := NewStateMetrics()
	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			other := NewStateMetrics()
			other.RecordAccountRead()
			other.RecordStorageRead(32)
			other.RecordGas(21000)
			m.Merge(other)
		}()
	}

	wg.Wait()

	if m.AccountsRead != goroutines {
		t.Fatalf("AccountsRead = %d, want %d", m.AccountsRead, goroutines)
	}
	if m.StorageReads != goroutines {
		t.Fatalf("StorageReads = %d, want %d", m.StorageReads, goroutines)
	}
	if m.TotalGasUsed != goroutines*21000 {
		t.Fatalf("TotalGasUsed = %d, want %d", m.TotalGasUsed, goroutines*21000)
	}
}

func TestSummaryKeyCount(t *testing.T) {
	m := NewStateMetrics()
	s := m.Summary()
	// Should have exactly 14 keys.
	if len(s) != 14 {
		t.Fatalf("summary has %d keys, want 14", len(s))
	}
}

func TestResetPreservesBlockNumber(t *testing.T) {
	m := NewStateMetrics()
	m.RecordAccountRead()
	m.RecordGas(100)
	m.Reset(100)
	m.Reset(200)
	if m.BlockNumber != 200 {
		t.Fatalf("BlockNumber = %d, want 200", m.BlockNumber)
	}
	if m.AccountsRead != 0 {
		t.Fatalf("AccountsRead = %d, want 0", m.AccountsRead)
	}
}
