// storage_cache.go implements multi-level storage value caching for EVM
// execution. It provides a per-transaction L1 cache and a per-block L2
// cache that integrate with EIP-2929 warm/cold access tracking.
//
// The L1 cache captures storage reads/writes within a single transaction,
// eliminating redundant state trie lookups. The L2 cache is shared across
// all transactions in a block, allowing later transactions to benefit from
// earlier trie lookups. Cache invalidation occurs on writes to maintain
// consistency with speculative execution and rollback.
package vm

import (
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
)

// CacheAccessKind distinguishes cache hit types for metrics.
type CacheAccessKind uint8

const (
	CacheL1Hit CacheAccessKind = iota // found in per-tx L1 cache
	CacheL2Hit                        // found in per-block L2 cache
	CacheMiss                         // not in any cache, required trie read
)

// storageSlotKey is defined in state_prefetch.go as:
//   type storageSlotKey struct { addr types.Address; key types.Hash }
// We reuse it here for cache lookups.

// cachedValue holds a storage value along with metadata.
type cachedValue struct {
	Value types.Hash
	Dirty bool // true if modified by current tx (L1) or any tx (L2)
	Warm  bool // true if slot has been accessed (EIP-2929 integration)
}

// CacheStats collects cache performance statistics. All fields are
// safe for concurrent access.
type CacheStats struct {
	L1Hits     atomic.Uint64
	L2Hits     atomic.Uint64
	Misses     atomic.Uint64
	L1Evicts   atomic.Uint64
	L2Evicts   atomic.Uint64
	WriteInval atomic.Uint64 // L2 invalidations due to writes
}

// Snapshot returns an immutable copy of the current statistics.
func (cs *CacheStats) Snapshot() CacheStatsSnapshot {
	return CacheStatsSnapshot{
		L1Hits:     cs.L1Hits.Load(),
		L2Hits:     cs.L2Hits.Load(),
		Misses:     cs.Misses.Load(),
		L1Evicts:   cs.L1Evicts.Load(),
		L2Evicts:   cs.L2Evicts.Load(),
		WriteInval: cs.WriteInval.Load(),
	}
}

// HitRate returns the overall cache hit rate (L1+L2 hits / total accesses).
// Returns 0.0 if no accesses have been recorded.
func (cs *CacheStats) HitRate() float64 {
	hits := cs.L1Hits.Load() + cs.L2Hits.Load()
	total := hits + cs.Misses.Load()
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total)
}

// CacheStatsSnapshot is an immutable snapshot of cache statistics.
type CacheStatsSnapshot struct {
	L1Hits     uint64
	L2Hits     uint64
	Misses     uint64
	L1Evicts   uint64
	L2Evicts   uint64
	WriteInval uint64
}

// TxStorageCache is the per-transaction L1 storage cache. It captures all
// storage reads and writes during a single transaction's execution.
// Not safe for concurrent use; each transaction gets its own instance.
type TxStorageCache struct {
	entries map[storageSlotKey]*cachedValue
	stats   *CacheStats
	parent  *BlockStorageCache // L2 cache for fallback lookups
}

// NewTxStorageCache creates a new L1 cache backed by the given L2 block cache.
// The stats collector is shared across all caches for unified metrics.
func NewTxStorageCache(parent *BlockStorageCache, stats *CacheStats) *TxStorageCache {
	return &TxStorageCache{
		entries: make(map[storageSlotKey]*cachedValue),
		stats:   stats,
		parent:  parent,
	}
}

// Get retrieves a storage value from the cache hierarchy.
// Returns the value, access kind (L1 hit, L2 hit, or miss), and whether
// the value was found in any cache level.
func (tc *TxStorageCache) Get(addr types.Address, slot types.Hash) (types.Hash, CacheAccessKind, bool) {
	sk := storageSlotKey{addr: addr, key: slot}

	// L1 lookup.
	if cv, ok := tc.entries[sk]; ok {
		tc.stats.L1Hits.Add(1)
		return cv.Value, CacheL1Hit, true
	}

	// L2 fallback.
	if tc.parent != nil {
		if val, ok := tc.parent.Get(addr, slot); ok {
			// Promote to L1.
			tc.entries[sk] = &cachedValue{
				Value: val,
				Dirty: false,
				Warm:  true,
			}
			tc.stats.L2Hits.Add(1)
			return val, CacheL2Hit, true
		}
	}

	tc.stats.Misses.Add(1)
	return types.Hash{}, CacheMiss, false
}

// Put stores a read value in the L1 cache. This is used for trie reads
// that should be cached for subsequent lookups within the same tx.
func (tc *TxStorageCache) Put(addr types.Address, slot, value types.Hash) {
	sk := storageSlotKey{addr: addr, key: slot}
	tc.entries[sk] = &cachedValue{
		Value: value,
		Dirty: false,
		Warm:  true,
	}
}

// Write stores a modified value in the L1 cache and invalidates the L2 entry.
// The dirty flag is set to indicate the value was modified by this transaction.
func (tc *TxStorageCache) Write(addr types.Address, slot, value types.Hash) {
	sk := storageSlotKey{addr: addr, key: slot}
	tc.entries[sk] = &cachedValue{
		Value: value,
		Dirty: true,
		Warm:  true,
	}
	// Invalidate L2 so other txs don't read stale data.
	if tc.parent != nil {
		tc.parent.Invalidate(addr, slot)
		tc.stats.WriteInval.Add(1)
	}
}

// IsWarm returns whether the given slot has been accessed (read or written)
// in this transaction. Integrates with EIP-2929 warm/cold tracking.
func (tc *TxStorageCache) IsWarm(addr types.Address, slot types.Hash) bool {
	sk := storageSlotKey{addr: addr, key: slot}
	if cv, ok := tc.entries[sk]; ok {
		return cv.Warm
	}
	return false
}

// MarkWarm marks a slot as warm (accessed) without caching a value.
// Used when the access list pre-warms slots.
func (tc *TxStorageCache) MarkWarm(addr types.Address, slot types.Hash) {
	sk := storageSlotKey{addr: addr, key: slot}
	if cv, ok := tc.entries[sk]; ok {
		cv.Warm = true
		return
	}
	tc.entries[sk] = &cachedValue{Warm: true}
}

// DirtySlots returns all storage slots that were modified in this transaction.
// Used to flush dirty values to the state trie after execution.
func (tc *TxStorageCache) DirtySlots() []StorageCacheEntry {
	var dirty []StorageCacheEntry
	for sk, cv := range tc.entries {
		if cv.Dirty {
			dirty = append(dirty, StorageCacheEntry{
				Address: sk.addr,
				Slot:    sk.key,
				Value:   cv.Value,
			})
		}
	}
	return dirty
}

// Flush promotes all L1 entries to the L2 block cache. Called after
// a transaction commits successfully.
func (tc *TxStorageCache) Flush() {
	if tc.parent == nil {
		return
	}
	for sk, cv := range tc.entries {
		tc.parent.Put(sk.addr, sk.key, cv.Value)
	}
}

// Reset clears the L1 cache for reuse with a new transaction.
func (tc *TxStorageCache) Reset() {
	tc.entries = make(map[storageSlotKey]*cachedValue)
}

// Size returns the number of entries in the L1 cache.
func (tc *TxStorageCache) Size() int {
	return len(tc.entries)
}

// StorageCacheEntry represents a single cached storage slot with its
// address, slot key, and current value.
type StorageCacheEntry struct {
	Address types.Address
	Slot    types.Hash
	Value   types.Hash
}

// BlockStorageCache is the per-block L2 storage cache shared across all
// transactions. It is safe for concurrent access.
type BlockStorageCache struct {
	mu      sync.RWMutex
	entries map[storageSlotKey]types.Hash
	maxSize int
	stats   *CacheStats
}

// NewBlockStorageCache creates a new L2 cache with the given maximum
// number of entries. If maxSize <= 0, a default of 65536 is used.
func NewBlockStorageCache(maxSize int, stats *CacheStats) *BlockStorageCache {
	if maxSize <= 0 {
		maxSize = 65536
	}
	return &BlockStorageCache{
		entries: make(map[storageSlotKey]types.Hash),
		maxSize: maxSize,
		stats:   stats,
	}
}

// Get retrieves a value from the L2 block cache.
func (bc *BlockStorageCache) Get(addr types.Address, slot types.Hash) (types.Hash, bool) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	sk := storageSlotKey{addr: addr, key: slot}
	val, ok := bc.entries[sk]
	return val, ok
}

// Put stores a value in the L2 block cache. If the cache exceeds its
// maximum size, older entries may be evicted.
func (bc *BlockStorageCache) Put(addr types.Address, slot, value types.Hash) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	sk := storageSlotKey{addr: addr, key: slot}

	// Evict if at capacity and this is a new key.
	if _, exists := bc.entries[sk]; !exists && len(bc.entries) >= bc.maxSize {
		bc.evictOne()
	}

	bc.entries[sk] = value
}

// Invalidate removes a specific entry from the L2 cache. Called when
// a transaction writes to a slot, ensuring no stale reads.
func (bc *BlockStorageCache) Invalidate(addr types.Address, slot types.Hash) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	sk := storageSlotKey{addr: addr, key: slot}
	delete(bc.entries, sk)
}

// InvalidateAddress removes all entries for the given address from the
// L2 cache. Used when an account is self-destructed or re-created.
func (bc *BlockStorageCache) InvalidateAddress(addr types.Address) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	for sk := range bc.entries {
		if sk.addr == addr {
			delete(bc.entries, sk)
			bc.stats.L2Evicts.Add(1)
		}
	}
}

// Size returns the number of entries in the L2 cache.
func (bc *BlockStorageCache) Size() int {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return len(bc.entries)
}

// Reset clears the entire L2 cache. Called at the start of a new block.
func (bc *BlockStorageCache) Reset() {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.entries = make(map[storageSlotKey]types.Hash)
}

// evictOne removes one entry from the cache to make room. Uses a simple
// strategy of removing the first entry found during map iteration.
// Must be called with bc.mu held.
func (bc *BlockStorageCache) evictOne() {
	for sk := range bc.entries {
		delete(bc.entries, sk)
		bc.stats.L2Evicts.Add(1)
		return
	}
}

// NewStorageCacheHierarchy creates a complete L1+L2 cache hierarchy for
// block execution. Returns the shared stats, L2 block cache, and a factory
// function that creates L1 caches for individual transactions.
func NewStorageCacheHierarchy(l2MaxSize int) (
	*CacheStats,
	*BlockStorageCache,
	func() *TxStorageCache,
) {
	stats := &CacheStats{}
	l2 := NewBlockStorageCache(l2MaxSize, stats)
	factory := func() *TxStorageCache {
		return NewTxStorageCache(l2, stats)
	}
	return stats, l2, factory
}
