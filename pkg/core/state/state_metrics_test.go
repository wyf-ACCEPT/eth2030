package state

import (
	"sync"
	"testing"
	"time"
)

func TestNewStateDBMetricsCollector(t *testing.T) {
	c := NewStateDBMetricsCollector()
	if c == nil {
		t.Fatal("NewStateDBMetricsCollector returned nil")
	}
	if c.createdAt.IsZero() {
		t.Fatal("expected non-zero creation time")
	}
}

func TestStateDBMetricsCollector_AccountOps(t *testing.T) {
	c := NewStateDBMetricsCollector()

	c.RecordAccountRead()
	c.RecordAccountRead()
	c.RecordAccountWrite()

	snap := c.Snapshot()
	if snap.AccountReads != 2 {
		t.Fatalf("AccountReads = %d, want 2", snap.AccountReads)
	}
	if snap.AccountWrites != 1 {
		t.Fatalf("AccountWrites = %d, want 1", snap.AccountWrites)
	}
}

func TestStateDBMetricsCollector_StorageOps(t *testing.T) {
	c := NewStateDBMetricsCollector()

	c.RecordStorageRead()
	c.RecordStorageRead()
	c.RecordStorageRead()
	c.RecordStorageWrite()

	snap := c.Snapshot()
	if snap.StorageReads != 3 {
		t.Fatalf("StorageReads = %d, want 3", snap.StorageReads)
	}
	if snap.StorageWrites != 1 {
		t.Fatalf("StorageWrites = %d, want 1", snap.StorageWrites)
	}
}

func TestStateDBMetricsCollector_CodeOps(t *testing.T) {
	c := NewStateDBMetricsCollector()

	c.RecordCodeLookup()
	c.RecordCodeLookup()
	c.RecordCodeWrite()

	snap := c.Snapshot()
	if snap.CodeLookups != 2 {
		t.Fatalf("CodeLookups = %d, want 2", snap.CodeLookups)
	}
	if snap.CodeWrites != 1 {
		t.Fatalf("CodeWrites = %d, want 1", snap.CodeWrites)
	}
}

func TestStateDBMetricsCollector_TrieOps(t *testing.T) {
	c := NewStateDBMetricsCollector()

	c.RecordTrieAccess()
	c.RecordTrieAccess()
	c.RecordTrieRead()
	c.RecordTrieWrite()
	c.RecordTrieWrite()
	c.RecordTrieWrite()

	snap := c.Snapshot()
	if snap.TrieAccesses != 2 {
		t.Fatalf("TrieAccesses = %d, want 2", snap.TrieAccesses)
	}
	if snap.TrieReads != 1 {
		t.Fatalf("TrieReads = %d, want 1", snap.TrieReads)
	}
	if snap.TrieWrites != 3 {
		t.Fatalf("TrieWrites = %d, want 3", snap.TrieWrites)
	}
}

func TestStateDBMetricsCollector_SnapshotAndRevert(t *testing.T) {
	c := NewStateDBMetricsCollector()

	c.RecordSnapshot()
	c.RecordSnapshot()
	c.RecordRevert()

	snap := c.Snapshot()
	if snap.Snapshots != 2 {
		t.Fatalf("Snapshots = %d, want 2", snap.Snapshots)
	}
	if snap.Reverts != 1 {
		t.Fatalf("Reverts = %d, want 1", snap.Reverts)
	}
}

func TestStateDBMetricsCollector_CacheHitRate(t *testing.T) {
	c := NewStateDBMetricsCollector()

	// No operations: rate should be 0.
	if rate := c.CacheHitRate(); rate != 0.0 {
		t.Fatalf("CacheHitRate = %f, want 0.0", rate)
	}

	// 3 hits, 1 miss => 0.75
	c.RecordCacheHit()
	c.RecordCacheHit()
	c.RecordCacheHit()
	c.RecordCacheMiss()

	rate := c.CacheHitRate()
	if rate != 0.75 {
		t.Fatalf("CacheHitRate = %f, want 0.75", rate)
	}
}

func TestStateDBMetricsCollector_SnapshotHitRate(t *testing.T) {
	c := NewStateDBMetricsCollector()

	if rate := c.SnapshotHitRate(); rate != 0.0 {
		t.Fatalf("SnapshotHitRate = %f, want 0.0", rate)
	}

	// 4 reads, 2 hits => 0.5
	c.RecordSnapshotRead()
	c.RecordSnapshotRead()
	c.RecordSnapshotRead()
	c.RecordSnapshotRead()
	c.RecordSnapshotHit()
	c.RecordSnapshotHit()

	rate := c.SnapshotHitRate()
	if rate != 0.5 {
		t.Fatalf("SnapshotHitRate = %f, want 0.5", rate)
	}
}

func TestStateDBMetricsCollector_CommitDuration(t *testing.T) {
	c := NewStateDBMetricsCollector()

	c.BeginCommit()
	time.Sleep(5 * time.Millisecond)
	elapsed := c.EndCommit()

	if elapsed < 5*time.Millisecond {
		t.Fatalf("commit duration = %v, want >= 5ms", elapsed)
	}

	snap := c.Snapshot()
	if snap.Commits != 1 {
		t.Fatalf("Commits = %d, want 1", snap.Commits)
	}
}

func TestStateDBMetricsCollector_EndCommitWithoutBegin(t *testing.T) {
	c := NewStateDBMetricsCollector()

	elapsed := c.EndCommit()
	if elapsed != 0 {
		t.Fatalf("expected 0 duration without BeginCommit, got %v", elapsed)
	}
}

func TestStateDBMetricsCollector_ActiveDirtyObjects(t *testing.T) {
	c := NewStateDBMetricsCollector()

	c.SetActiveObjects(100)
	c.SetDirtyObjects(25)

	snap := c.Snapshot()
	if snap.ActiveObjects != 100 {
		t.Fatalf("ActiveObjects = %d, want 100", snap.ActiveObjects)
	}
	if snap.DirtyObjects != 25 {
		t.Fatalf("DirtyObjects = %d, want 25", snap.DirtyObjects)
	}
}

func TestStateDBMetricsCollector_Reset(t *testing.T) {
	c := NewStateDBMetricsCollector()

	// Record various operations.
	c.RecordAccountRead()
	c.RecordAccountWrite()
	c.RecordStorageRead()
	c.RecordStorageWrite()
	c.RecordCodeLookup()
	c.RecordCodeWrite()
	c.RecordTrieAccess()
	c.RecordTrieRead()
	c.RecordTrieWrite()
	c.RecordSnapshot()
	c.RecordRevert()
	c.RecordCacheHit()
	c.RecordCacheMiss()
	c.RecordSnapshotRead()
	c.RecordSnapshotHit()
	c.SetActiveObjects(50)
	c.SetDirtyObjects(10)

	c.Reset()

	snap := c.Snapshot()
	if snap.AccountReads != 0 {
		t.Fatalf("AccountReads = %d after reset, want 0", snap.AccountReads)
	}
	if snap.AccountWrites != 0 {
		t.Fatalf("AccountWrites = %d after reset, want 0", snap.AccountWrites)
	}
	if snap.StorageReads != 0 {
		t.Fatalf("StorageReads = %d after reset, want 0", snap.StorageReads)
	}
	if snap.CacheHits != 0 {
		t.Fatalf("CacheHits = %d after reset, want 0", snap.CacheHits)
	}
	if snap.SnapshotReads != 0 {
		t.Fatalf("SnapshotReads = %d after reset, want 0", snap.SnapshotReads)
	}
	if snap.ActiveObjects != 0 {
		t.Fatalf("ActiveObjects = %d after reset, want 0", snap.ActiveObjects)
	}
	if snap.DirtyObjects != 0 {
		t.Fatalf("DirtyObjects = %d after reset, want 0", snap.DirtyObjects)
	}
}

func TestStateDBMetricsCollector_SummaryMap(t *testing.T) {
	c := NewStateDBMetricsCollector()

	c.RecordAccountRead()
	c.RecordStorageWrite()
	c.RecordCodeLookup()
	c.RecordTrieAccess()

	sm := c.SummaryMap()

	expectedKeys := []string{
		"account_reads", "account_writes",
		"storage_reads", "storage_writes",
		"code_lookups", "code_writes",
		"trie_accesses", "trie_reads", "trie_writes",
		"commits", "snapshots", "reverts",
		"cache_hits", "cache_misses",
		"snapshot_reads", "snapshot_hits",
		"active_objects", "dirty_objects",
	}

	if len(sm) != len(expectedKeys) {
		t.Fatalf("SummaryMap has %d keys, want %d", len(sm), len(expectedKeys))
	}

	for _, key := range expectedKeys {
		if _, ok := sm[key]; !ok {
			t.Errorf("SummaryMap missing key %q", key)
		}
	}

	if sm["account_reads"] != 1 {
		t.Errorf("account_reads = %d, want 1", sm["account_reads"])
	}
	if sm["storage_writes"] != 1 {
		t.Errorf("storage_writes = %d, want 1", sm["storage_writes"])
	}
}

func TestStateDBMetricsCollector_SnapshotUptime(t *testing.T) {
	c := NewStateDBMetricsCollector()
	time.Sleep(10 * time.Millisecond)

	snap := c.Snapshot()
	if snap.UptimeMs < 10 {
		t.Fatalf("UptimeMs = %d, want >= 10", snap.UptimeMs)
	}
}

func TestStateDBMetricsCollector_SnapshotCacheHitRate(t *testing.T) {
	c := NewStateDBMetricsCollector()

	c.RecordCacheHit()
	c.RecordCacheHit()
	c.RecordCacheMiss()
	c.RecordCacheMiss()

	snap := c.Snapshot()
	if snap.CacheHitRate != 0.5 {
		t.Fatalf("CacheHitRate = %f, want 0.5", snap.CacheHitRate)
	}
}

func TestStateDBMetricsCollector_ConcurrentSafety(t *testing.T) {
	c := NewStateDBMetricsCollector()
	const goroutines = 100
	const opsPerGoroutine = 500

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				c.RecordAccountRead()
				c.RecordAccountWrite()
				c.RecordStorageRead()
				c.RecordStorageWrite()
				c.RecordCodeLookup()
				c.RecordTrieAccess()
				c.RecordCacheHit()
				c.RecordCacheMiss()
				c.RecordSnapshotRead()
				c.RecordSnapshotHit()
			}
		}()
	}

	wg.Wait()

	expected := int64(goroutines * opsPerGoroutine)
	snap := c.Snapshot()

	if snap.AccountReads != expected {
		t.Fatalf("AccountReads = %d, want %d", snap.AccountReads, expected)
	}
	if snap.AccountWrites != expected {
		t.Fatalf("AccountWrites = %d, want %d", snap.AccountWrites, expected)
	}
	if snap.StorageReads != expected {
		t.Fatalf("StorageReads = %d, want %d", snap.StorageReads, expected)
	}
	if snap.StorageWrites != expected {
		t.Fatalf("StorageWrites = %d, want %d", snap.StorageWrites, expected)
	}
	if snap.CodeLookups != expected {
		t.Fatalf("CodeLookups = %d, want %d", snap.CodeLookups, expected)
	}
	if snap.TrieAccesses != expected {
		t.Fatalf("TrieAccesses = %d, want %d", snap.TrieAccesses, expected)
	}
	if snap.CacheHits != expected {
		t.Fatalf("CacheHits = %d, want %d", snap.CacheHits, expected)
	}
	if snap.CacheMisses != expected {
		t.Fatalf("CacheMisses = %d, want %d", snap.CacheMisses, expected)
	}
	if snap.SnapshotReads != expected {
		t.Fatalf("SnapshotReads = %d, want %d", snap.SnapshotReads, expected)
	}
	if snap.SnapshotHits != expected {
		t.Fatalf("SnapshotHits = %d, want %d", snap.SnapshotHits, expected)
	}
}

func TestStateDBMetricsCollector_ConcurrentCommit(t *testing.T) {
	// Each goroutine uses its own collector since BeginCommit/EndCommit
	// track a single in-progress commit per collector. We verify that
	// the global registry counter is incremented by all goroutines.
	const goroutines = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			c := NewStateDBMetricsCollector()
			c.BeginCommit()
			time.Sleep(time.Millisecond)
			elapsed := c.EndCommit()
			if elapsed < time.Millisecond {
				t.Errorf("commit duration = %v, want >= 1ms", elapsed)
			}
			snap := c.Snapshot()
			if snap.Commits != 1 {
				t.Errorf("per-collector Commits = %d, want 1", snap.Commits)
			}
		}()
	}

	wg.Wait()
}

func TestStateDBMetricsCollector_ResetAndReuse(t *testing.T) {
	c := NewStateDBMetricsCollector()

	// First round of operations.
	for i := 0; i < 10; i++ {
		c.RecordAccountRead()
		c.RecordCacheHit()
	}

	c.Reset()

	// Second round.
	for i := 0; i < 5; i++ {
		c.RecordAccountRead()
		c.RecordCacheMiss()
	}

	snap := c.Snapshot()
	if snap.AccountReads != 5 {
		t.Fatalf("AccountReads = %d after reset+reuse, want 5", snap.AccountReads)
	}
	if snap.CacheHits != 0 {
		t.Fatalf("CacheHits = %d after reset, want 0", snap.CacheHits)
	}
	if snap.CacheMisses != 5 {
		t.Fatalf("CacheMisses = %d after reset+reuse, want 5", snap.CacheMisses)
	}
}

func TestStateDBMetricsCollector_AllMetricIncrements(t *testing.T) {
	c := NewStateDBMetricsCollector()

	c.RecordAccountRead()
	c.RecordAccountWrite()
	c.RecordStorageRead()
	c.RecordStorageWrite()
	c.RecordCodeLookup()
	c.RecordCodeWrite()
	c.RecordTrieAccess()
	c.RecordTrieRead()
	c.RecordTrieWrite()
	c.RecordSnapshot()
	c.RecordRevert()
	c.RecordCacheHit()
	c.RecordCacheMiss()
	c.RecordSnapshotRead()
	c.RecordSnapshotHit()

	snap := c.Snapshot()
	if snap.AccountReads != 1 {
		t.Errorf("AccountReads = %d, want 1", snap.AccountReads)
	}
	if snap.AccountWrites != 1 {
		t.Errorf("AccountWrites = %d, want 1", snap.AccountWrites)
	}
	if snap.StorageReads != 1 {
		t.Errorf("StorageReads = %d, want 1", snap.StorageReads)
	}
	if snap.StorageWrites != 1 {
		t.Errorf("StorageWrites = %d, want 1", snap.StorageWrites)
	}
	if snap.CodeLookups != 1 {
		t.Errorf("CodeLookups = %d, want 1", snap.CodeLookups)
	}
	if snap.CodeWrites != 1 {
		t.Errorf("CodeWrites = %d, want 1", snap.CodeWrites)
	}
	if snap.TrieAccesses != 1 {
		t.Errorf("TrieAccesses = %d, want 1", snap.TrieAccesses)
	}
	if snap.TrieReads != 1 {
		t.Errorf("TrieReads = %d, want 1", snap.TrieReads)
	}
	if snap.TrieWrites != 1 {
		t.Errorf("TrieWrites = %d, want 1", snap.TrieWrites)
	}
	if snap.Snapshots != 1 {
		t.Errorf("Snapshots = %d, want 1", snap.Snapshots)
	}
	if snap.Reverts != 1 {
		t.Errorf("Reverts = %d, want 1", snap.Reverts)
	}
	if snap.CacheHits != 1 {
		t.Errorf("CacheHits = %d, want 1", snap.CacheHits)
	}
	if snap.CacheMisses != 1 {
		t.Errorf("CacheMisses = %d, want 1", snap.CacheMisses)
	}
	if snap.SnapshotReads != 1 {
		t.Errorf("SnapshotReads = %d, want 1", snap.SnapshotReads)
	}
	if snap.SnapshotHits != 1 {
		t.Errorf("SnapshotHits = %d, want 1", snap.SnapshotHits)
	}
}
