// merkle_cache.go provides a thread-safe cache for SSZ merkleization
// intermediate hash results. By caching subtree roots and hash computations,
// repeated merkleization of unchanged objects can skip redundant SHA-256
// work, which is critical for beacon state root computation performance.
package ssz

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

// MerkleCacheStats holds cache performance counters.
type MerkleCacheStats struct {
	Hits      uint64
	Misses    uint64
	Entries   uint64
	Evictions uint64
}

// cacheEntry stores a cached hash value with a timestamp for age-based pruning.
type cacheEntry struct {
	value     [32]byte
	timestamp int64 // unix seconds when entry was stored
}

// subtreeKey is a composite key for subtree root lookups, combining an
// object hash with a tree depth.
type subtreeKey struct {
	objectHash [32]byte
	depth      int
}

// MerkleCache caches intermediate hash results from SSZ merkleization.
// It supports two kinds of lookups:
//   - Direct hash cache: key -> value for arbitrary 32-byte hash pairs
//   - Subtree root cache: (objectHash, depth) -> subtree root
//
// The cache uses a simple capacity-based eviction strategy: when full,
// the oldest entry is evicted. All operations are thread-safe.
type MerkleCache struct {
	mu         sync.RWMutex
	maxEntries int

	// Direct hash cache: maps a 32-byte key to a cached value.
	hashes map[[32]byte]*cacheEntry

	// Subtree root cache: maps (objectHash, depth) to a cached root.
	subtrees map[subtreeKey]*cacheEntry

	// Insertion order for eviction. Tracks keys in insertion order so
	// the oldest can be evicted when capacity is reached.
	hashOrder    [][32]byte
	subtreeOrder []subtreeKey

	// Statistics tracked atomically for lock-free reads.
	hits      atomic.Uint64
	misses    atomic.Uint64
	evictions atomic.Uint64
}

// NewMerkleCache creates a new cache with the given maximum number of total
// entries (hashes + subtree roots combined). If maxEntries <= 0, the cache
// stores nothing and all lookups miss.
func NewMerkleCache(maxEntries int) *MerkleCache {
	if maxEntries < 0 {
		maxEntries = 0
	}
	return &MerkleCache{
		maxEntries:   maxEntries,
		hashes:       make(map[[32]byte]*cacheEntry),
		subtrees:     make(map[subtreeKey]*cacheEntry),
		hashOrder:    make([][32]byte, 0),
		subtreeOrder: make([]subtreeKey, 0),
	}
}

// GetHash looks up a cached hash value for the given key. Returns the
// cached value and true on a hit, or a zero hash and false on a miss.
func (mc *MerkleCache) GetHash(key [32]byte) ([32]byte, bool) {
	mc.mu.RLock()
	entry, ok := mc.hashes[key]
	mc.mu.RUnlock()

	if ok {
		mc.hits.Add(1)
		return entry.value, true
	}
	mc.misses.Add(1)
	return [32]byte{}, false
}

// PutHash stores a hash value in the cache. If the cache is full, the
// oldest hash entry is evicted to make room.
func (mc *MerkleCache) PutHash(key [32]byte, value [32]byte) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.maxEntries <= 0 {
		return
	}

	// If key already exists, update in place.
	if entry, ok := mc.hashes[key]; ok {
		entry.value = value
		entry.timestamp = time.Now().Unix()
		return
	}

	// Evict oldest hash entry if at capacity.
	mc.evictHashIfNeeded()

	mc.hashes[key] = &cacheEntry{
		value:     value,
		timestamp: time.Now().Unix(),
	}
	mc.hashOrder = append(mc.hashOrder, key)
}

// GetSubtreeRoot looks up a cached subtree root for the given object hash
// and tree depth. Returns the cached root and true on a hit.
func (mc *MerkleCache) GetSubtreeRoot(objectHash [32]byte, depth int) ([32]byte, bool) {
	key := subtreeKey{objectHash: objectHash, depth: depth}

	mc.mu.RLock()
	entry, ok := mc.subtrees[key]
	mc.mu.RUnlock()

	if ok {
		mc.hits.Add(1)
		return entry.value, true
	}
	mc.misses.Add(1)
	return [32]byte{}, false
}

// PutSubtreeRoot stores a subtree root in the cache. If the cache is full,
// the oldest subtree entry is evicted.
func (mc *MerkleCache) PutSubtreeRoot(objectHash [32]byte, depth int, root [32]byte) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.maxEntries <= 0 {
		return
	}

	key := subtreeKey{objectHash: objectHash, depth: depth}

	// If key already exists, update in place.
	if entry, ok := mc.subtrees[key]; ok {
		entry.value = root
		entry.timestamp = time.Now().Unix()
		return
	}

	// Evict oldest subtree entry if at capacity.
	mc.evictSubtreeIfNeeded()

	mc.subtrees[key] = &cacheEntry{
		value:     root,
		timestamp: time.Now().Unix(),
	}
	mc.subtreeOrder = append(mc.subtreeOrder, key)
}

// InvalidateObject removes all subtree root entries associated with the
// given object hash. Direct hash entries are not affected because they
// are keyed by content hash, not object identity.
func (mc *MerkleCache) InvalidateObject(objectHash [32]byte) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Remove matching subtree entries and rebuild the order slice.
	newOrder := make([]subtreeKey, 0, len(mc.subtreeOrder))
	for _, key := range mc.subtreeOrder {
		if key.objectHash == objectHash {
			delete(mc.subtrees, key)
		} else {
			newOrder = append(newOrder, key)
		}
	}
	mc.subtreeOrder = newOrder
}

// Len returns the total number of cached entries (hashes + subtree roots).
func (mc *MerkleCache) Len() int {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return len(mc.hashes) + len(mc.subtrees)
}

// Clear removes all entries and resets statistics.
func (mc *MerkleCache) Clear() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.hashes = make(map[[32]byte]*cacheEntry)
	mc.subtrees = make(map[subtreeKey]*cacheEntry)
	mc.hashOrder = mc.hashOrder[:0]
	mc.subtreeOrder = mc.subtreeOrder[:0]

	mc.hits.Store(0)
	mc.misses.Store(0)
	mc.evictions.Store(0)
}

// HitRate returns the cache hit rate as a float64 in [0.0, 1.0].
// Returns 0.0 if no lookups have been performed.
func (mc *MerkleCache) HitRate() float64 {
	hits := mc.hits.Load()
	misses := mc.misses.Load()
	total := hits + misses
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total)
}

// Stats returns a snapshot of cache performance counters.
func (mc *MerkleCache) Stats() *MerkleCacheStats {
	mc.mu.RLock()
	entries := uint64(len(mc.hashes) + len(mc.subtrees))
	mc.mu.RUnlock()

	return &MerkleCacheStats{
		Hits:      mc.hits.Load(),
		Misses:    mc.misses.Load(),
		Entries:   entries,
		Evictions: mc.evictions.Load(),
	}
}

// PruneByAge removes entries older than maxAgeSecs seconds and returns the
// number of entries pruned.
func (mc *MerkleCache) PruneByAge(maxAgeSecs int64) int {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	cutoff := time.Now().Unix() - maxAgeSecs
	pruned := 0

	// Prune hash entries.
	newHashOrder := make([][32]byte, 0, len(mc.hashOrder))
	for _, key := range mc.hashOrder {
		entry, ok := mc.hashes[key]
		if !ok {
			continue
		}
		if entry.timestamp < cutoff {
			delete(mc.hashes, key)
			pruned++
		} else {
			newHashOrder = append(newHashOrder, key)
		}
	}
	mc.hashOrder = newHashOrder

	// Prune subtree entries.
	newSubOrder := make([]subtreeKey, 0, len(mc.subtreeOrder))
	for _, key := range mc.subtreeOrder {
		entry, ok := mc.subtrees[key]
		if !ok {
			continue
		}
		if entry.timestamp < cutoff {
			delete(mc.subtrees, key)
			pruned++
		} else {
			newSubOrder = append(newSubOrder, key)
		}
	}
	mc.subtreeOrder = newSubOrder

	return pruned
}

// evictHashIfNeeded evicts the oldest hash entry if the total cache size
// has reached maxEntries. Must be called with mc.mu held.
func (mc *MerkleCache) evictHashIfNeeded() {
	total := len(mc.hashes) + len(mc.subtrees)
	if total < mc.maxEntries {
		return
	}

	// Evict oldest hash entry.
	if len(mc.hashOrder) > 0 {
		oldest := mc.hashOrder[0]
		mc.hashOrder = mc.hashOrder[1:]
		delete(mc.hashes, oldest)
		mc.evictions.Add(1)
	}
}

// evictSubtreeIfNeeded evicts the oldest subtree entry if the total cache
// size has reached maxEntries. Must be called with mc.mu held.
func (mc *MerkleCache) evictSubtreeIfNeeded() {
	total := len(mc.hashes) + len(mc.subtrees)
	if total < mc.maxEntries {
		return
	}

	// Evict oldest subtree entry.
	if len(mc.subtreeOrder) > 0 {
		oldest := mc.subtreeOrder[0]
		mc.subtreeOrder = mc.subtreeOrder[1:]
		delete(mc.subtrees, oldest)
		mc.evictions.Add(1)
	}
}

// subtreeKeyBytes serializes a subtreeKey to a deterministic byte slice.
// Used internally if needed for hashing composite keys.
func subtreeKeyBytes(objectHash [32]byte, depth int) []byte {
	buf := make([]byte, 40)
	copy(buf[:32], objectHash[:])
	binary.BigEndian.PutUint64(buf[32:], uint64(depth))
	return buf
}
