// state_metrics.go implements a StateDB metrics collector that integrates with
// the global metrics registry. It tracks account reads/writes, storage
// reads/writes, code lookups, cache hit rates, trie access counts, commit
// durations, and snapshot usage stats using counters, gauges, and histograms.
package state

import (
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/metrics"
)

// Pre-registered metrics in the default registry for StateDB operations.
var (
	stateAccountReads   = metrics.DefaultRegistry.Counter("state.account_reads")
	stateAccountWrites  = metrics.DefaultRegistry.Counter("state.account_writes")
	stateStorageReads   = metrics.DefaultRegistry.Counter("state.storage_reads")
	stateStorageWrites  = metrics.DefaultRegistry.Counter("state.storage_writes")
	stateCodeLookups    = metrics.DefaultRegistry.Counter("state.code_lookups")
	stateCodeWrites     = metrics.DefaultRegistry.Counter("state.code_writes")
	stateTrieAccesses   = metrics.DefaultRegistry.Counter("state.trie_accesses")
	stateTrieReads      = metrics.DefaultRegistry.Counter("state.trie_reads")
	stateTrieWrites     = metrics.DefaultRegistry.Counter("state.trie_writes")
	stateCommits        = metrics.DefaultRegistry.Counter("state.commits")
	stateCommitDuration = metrics.DefaultRegistry.Histogram("state.commit_duration_ms")
	stateSnapshots      = metrics.DefaultRegistry.Counter("state.snapshots")
	stateReverts        = metrics.DefaultRegistry.Counter("state.reverts")
	stateCacheHits      = metrics.DefaultRegistry.Counter("state.cache_hits")
	stateCacheMisses    = metrics.DefaultRegistry.Counter("state.cache_misses")
	stateSnapshotReads  = metrics.DefaultRegistry.Counter("state.snapshot_reads")
	stateSnapshotHits   = metrics.DefaultRegistry.Counter("state.snapshot_hits")
	stateActiveObjects  = metrics.DefaultRegistry.Gauge("state.active_objects")
	stateDirtyObjects   = metrics.DefaultRegistry.Gauge("state.dirty_objects")
)

// StateDBMetricsCollector tracks operational metrics for StateDB instances.
// It records fine-grained counters for account, storage, code, trie, and
// snapshot operations, plus commit durations and cache effectiveness.
// All methods are safe for concurrent use via atomic operations.
type StateDBMetricsCollector struct {
	// Per-instance counters (atomic). These track the local state for
	// a single collector instance and are periodically flushed to the
	// global registry.
	accountReads  atomic.Int64
	accountWrites atomic.Int64
	storageReads  atomic.Int64
	storageWrites atomic.Int64
	codeLookups   atomic.Int64
	codeWrites    atomic.Int64
	trieAccesses  atomic.Int64
	trieReads     atomic.Int64
	trieWrites    atomic.Int64
	commits       atomic.Int64
	snapshots     atomic.Int64
	reverts       atomic.Int64
	cacheHits     atomic.Int64
	cacheMisses   atomic.Int64
	snapshotReads atomic.Int64
	snapshotHits  atomic.Int64
	activeObjects atomic.Int64
	dirtyObjects  atomic.Int64

	// commitStart tracks the start time of the current commit operation.
	commitStart atomic.Int64

	// createdAt records the collector creation time for uptime tracking.
	createdAt time.Time
}

// NewStateDBMetricsCollector creates a new metrics collector.
func NewStateDBMetricsCollector() *StateDBMetricsCollector {
	return &StateDBMetricsCollector{
		createdAt: time.Now(),
	}
}

// RecordAccountRead records an account read operation.
func (c *StateDBMetricsCollector) RecordAccountRead() {
	c.accountReads.Add(1)
	stateAccountReads.Inc()
}

// RecordAccountWrite records an account write operation.
func (c *StateDBMetricsCollector) RecordAccountWrite() {
	c.accountWrites.Add(1)
	stateAccountWrites.Inc()
}

// RecordStorageRead records a storage slot read operation.
func (c *StateDBMetricsCollector) RecordStorageRead() {
	c.storageReads.Add(1)
	stateStorageReads.Inc()
}

// RecordStorageWrite records a storage slot write operation.
func (c *StateDBMetricsCollector) RecordStorageWrite() {
	c.storageWrites.Add(1)
	stateStorageWrites.Inc()
}

// RecordCodeLookup records a code lookup (GetCode, GetCodeHash, GetCodeSize).
func (c *StateDBMetricsCollector) RecordCodeLookup() {
	c.codeLookups.Add(1)
	stateCodeLookups.Inc()
}

// RecordCodeWrite records a code write (SetCode).
func (c *StateDBMetricsCollector) RecordCodeWrite() {
	c.codeWrites.Add(1)
	stateCodeWrites.Inc()
}

// RecordTrieAccess records a generic trie access (read or write).
func (c *StateDBMetricsCollector) RecordTrieAccess() {
	c.trieAccesses.Add(1)
	stateTrieAccesses.Inc()
}

// RecordTrieRead records a trie read (node lookup during proof or state access).
func (c *StateDBMetricsCollector) RecordTrieRead() {
	c.trieReads.Add(1)
	stateTrieReads.Inc()
}

// RecordTrieWrite records a trie write (node insertion during commit).
func (c *StateDBMetricsCollector) RecordTrieWrite() {
	c.trieWrites.Add(1)
	stateTrieWrites.Inc()
}

// RecordSnapshot records a snapshot creation.
func (c *StateDBMetricsCollector) RecordSnapshot() {
	c.snapshots.Add(1)
	stateSnapshots.Inc()
}

// RecordRevert records a revert-to-snapshot operation.
func (c *StateDBMetricsCollector) RecordRevert() {
	c.reverts.Add(1)
	stateReverts.Inc()
}

// RecordCacheHit records a cache hit (account or storage found in cache).
func (c *StateDBMetricsCollector) RecordCacheHit() {
	c.cacheHits.Add(1)
	stateCacheHits.Inc()
}

// RecordCacheMiss records a cache miss (account or storage not in cache).
func (c *StateDBMetricsCollector) RecordCacheMiss() {
	c.cacheMisses.Add(1)
	stateCacheMisses.Inc()
}

// RecordSnapshotRead records a read from a snapshot layer.
func (c *StateDBMetricsCollector) RecordSnapshotRead() {
	c.snapshotReads.Add(1)
	stateSnapshotReads.Inc()
}

// RecordSnapshotHit records a successful hit when reading from snapshot layers.
func (c *StateDBMetricsCollector) RecordSnapshotHit() {
	c.snapshotHits.Add(1)
	stateSnapshotHits.Inc()
}

// SetActiveObjects sets the current number of live state objects.
func (c *StateDBMetricsCollector) SetActiveObjects(n int64) {
	c.activeObjects.Store(n)
	stateActiveObjects.Set(n)
}

// SetDirtyObjects sets the current number of dirty (modified) state objects.
func (c *StateDBMetricsCollector) SetDirtyObjects(n int64) {
	c.dirtyObjects.Store(n)
	stateDirtyObjects.Set(n)
}

// BeginCommit marks the start of a commit operation. Call EndCommit when
// the commit finishes to record the duration.
func (c *StateDBMetricsCollector) BeginCommit() {
	c.commitStart.Store(time.Now().UnixNano())
}

// EndCommit records the duration of the commit operation that was started
// with BeginCommit. Returns the elapsed duration.
func (c *StateDBMetricsCollector) EndCommit() time.Duration {
	start := c.commitStart.Load()
	if start == 0 {
		return 0
	}
	elapsed := time.Since(time.Unix(0, start))
	c.commits.Add(1)
	stateCommits.Inc()
	stateCommitDuration.Observe(float64(elapsed.Milliseconds()))
	c.commitStart.Store(0)
	return elapsed
}

// CacheHitRate returns the cache hit rate as a value between 0.0 and 1.0.
// Returns 0.0 if no cache operations have occurred.
func (c *StateDBMetricsCollector) CacheHitRate() float64 {
	hits := c.cacheHits.Load()
	misses := c.cacheMisses.Load()
	total := hits + misses
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total)
}

// SnapshotHitRate returns the snapshot read hit rate as a value between 0.0
// and 1.0. Returns 0.0 if no snapshot reads have occurred.
func (c *StateDBMetricsCollector) SnapshotHitRate() float64 {
	reads := c.snapshotReads.Load()
	hits := c.snapshotHits.Load()
	if reads == 0 {
		return 0.0
	}
	return float64(hits) / float64(reads)
}

// MetricsSnapshot returns a point-in-time copy of all local counters.
type MetricsSnapshot struct {
	AccountReads  int64
	AccountWrites int64
	StorageReads  int64
	StorageWrites int64
	CodeLookups   int64
	CodeWrites    int64
	TrieAccesses  int64
	TrieReads     int64
	TrieWrites    int64
	Commits       int64
	Snapshots     int64
	Reverts       int64
	CacheHits     int64
	CacheMisses   int64
	SnapshotReads int64
	SnapshotHits  int64
	ActiveObjects int64
	DirtyObjects  int64
	CacheHitRate  float64
	UptimeMs      int64
}

// Snapshot returns a point-in-time snapshot of all collector metrics.
func (c *StateDBMetricsCollector) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		AccountReads:  c.accountReads.Load(),
		AccountWrites: c.accountWrites.Load(),
		StorageReads:  c.storageReads.Load(),
		StorageWrites: c.storageWrites.Load(),
		CodeLookups:   c.codeLookups.Load(),
		CodeWrites:    c.codeWrites.Load(),
		TrieAccesses:  c.trieAccesses.Load(),
		TrieReads:     c.trieReads.Load(),
		TrieWrites:    c.trieWrites.Load(),
		Commits:       c.commits.Load(),
		Snapshots:     c.snapshots.Load(),
		Reverts:       c.reverts.Load(),
		CacheHits:     c.cacheHits.Load(),
		CacheMisses:   c.cacheMisses.Load(),
		SnapshotReads: c.snapshotReads.Load(),
		SnapshotHits:  c.snapshotHits.Load(),
		ActiveObjects: c.activeObjects.Load(),
		DirtyObjects:  c.dirtyObjects.Load(),
		CacheHitRate:  c.CacheHitRate(),
		UptimeMs:      time.Since(c.createdAt).Milliseconds(),
	}
}

// Reset zeroes all local counters. Global registry counters are not reset
// because they are monotonically increasing process-wide accumulators.
func (c *StateDBMetricsCollector) Reset() {
	c.accountReads.Store(0)
	c.accountWrites.Store(0)
	c.storageReads.Store(0)
	c.storageWrites.Store(0)
	c.codeLookups.Store(0)
	c.codeWrites.Store(0)
	c.trieAccesses.Store(0)
	c.trieReads.Store(0)
	c.trieWrites.Store(0)
	c.commits.Store(0)
	c.snapshots.Store(0)
	c.reverts.Store(0)
	c.cacheHits.Store(0)
	c.cacheMisses.Store(0)
	c.snapshotReads.Store(0)
	c.snapshotHits.Store(0)
	c.activeObjects.Store(0)
	c.dirtyObjects.Store(0)
	c.commitStart.Store(0)
}

// SummaryMap returns all metrics as a map of string to int64 for easy
// serialization and logging.
func (c *StateDBMetricsCollector) SummaryMap() map[string]int64 {
	return map[string]int64{
		"account_reads":  c.accountReads.Load(),
		"account_writes": c.accountWrites.Load(),
		"storage_reads":  c.storageReads.Load(),
		"storage_writes": c.storageWrites.Load(),
		"code_lookups":   c.codeLookups.Load(),
		"code_writes":    c.codeWrites.Load(),
		"trie_accesses":  c.trieAccesses.Load(),
		"trie_reads":     c.trieReads.Load(),
		"trie_writes":    c.trieWrites.Load(),
		"commits":        c.commits.Load(),
		"snapshots":      c.snapshots.Load(),
		"reverts":        c.reverts.Load(),
		"cache_hits":     c.cacheHits.Load(),
		"cache_misses":   c.cacheMisses.Load(),
		"snapshot_reads": c.snapshotReads.Load(),
		"snapshot_hits":  c.snapshotHits.Load(),
		"active_objects": c.activeObjects.Load(),
		"dirty_objects":  c.dirtyObjects.Load(),
	}
}
