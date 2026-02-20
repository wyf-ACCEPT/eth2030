// trie_cache.go provides an LRU-evicting cache for trie nodes. It stores
// RLP-encoded trie nodes keyed by their Keccak-256 hash and tracks cache
// hit/miss/eviction statistics.
package trie

import (
	"sync"
	"sync/atomic"
)

// CacheStats holds trie cache performance metrics.
type CacheStats struct {
	Hits        uint64 // number of cache hits
	Misses      uint64 // number of cache misses
	Evictions   uint64 // number of entries evicted
	CurrentSize uint64 // current cache size in bytes
	EntryCount  int    // current number of cached entries
}

// cacheEntry is a node in the doubly-linked list used for LRU tracking.
type cacheEntry struct {
	hash  [32]byte
	data  []byte
	prev  *cacheEntry
	next  *cacheEntry
	size  uint64 // size of data in bytes
}

// TrieCache is a thread-safe LRU cache for trie nodes keyed by hash.
// It bounds memory usage by maxSize and evicts the least recently used
// entries when space is needed.
type TrieCache struct {
	mu      sync.RWMutex
	entries map[[32]byte]*cacheEntry
	head    *cacheEntry // most recently used
	tail    *cacheEntry // least recently used
	maxSize uint64      // maximum cache size in bytes
	curSize uint64      // current cache size in bytes

	hits      atomic.Uint64
	misses    atomic.Uint64
	evictions atomic.Uint64
}

// NewTrieCache creates a new trie node cache with the given maximum size
// in number of entries. Each entry's byte size is tracked for total memory
// accounting. The maxSize parameter sets the maximum total byte size of
// all cached node data.
func NewTrieCache(maxSize int) *TrieCache {
	if maxSize < 0 {
		maxSize = 0
	}
	return &TrieCache{
		entries: make(map[[32]byte]*cacheEntry),
		maxSize: uint64(maxSize),
	}
}

// Get retrieves a cached trie node by its hash. Returns the data and true
// if found, or nil and false if the node is not cached.
func (c *TrieCache) Get(hash [32]byte) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[hash]
	if !ok {
		c.misses.Add(1)
		return nil, false
	}

	c.hits.Add(1)

	// Move to front (most recently used).
	c.moveToFrontLocked(entry)

	// Return a copy to prevent external mutation.
	cp := make([]byte, len(entry.data))
	copy(cp, entry.data)
	return cp, true
}

// Put stores a trie node in the cache. If the cache is full, the least
// recently used entry is evicted. If the key already exists, the data
// is updated and the entry is moved to the front.
func (c *TrieCache) Put(hash [32]byte, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Make a copy of the data.
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	dataSize := uint64(len(dataCopy))

	// If entry already exists, update it.
	if existing, ok := c.entries[hash]; ok {
		c.curSize -= existing.size
		existing.data = dataCopy
		existing.size = dataSize
		c.curSize += dataSize
		c.moveToFrontLocked(existing)
		return
	}

	// Evict entries if we'd exceed maxSize.
	for c.maxSize > 0 && c.curSize+dataSize > c.maxSize && c.tail != nil {
		c.evictTailLocked()
	}

	entry := &cacheEntry{
		hash: hash,
		data: dataCopy,
		size: dataSize,
	}

	c.entries[hash] = entry
	c.curSize += dataSize
	c.pushFrontLocked(entry)
}

// Delete removes a node from the cache by its hash.
func (c *TrieCache) Delete(hash [32]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[hash]
	if !ok {
		return
	}

	c.removeLocked(entry)
	delete(c.entries, hash)
	c.curSize -= entry.size
}

// Len returns the number of entries currently in the cache.
func (c *TrieCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Size returns the total byte size of all cached node data.
func (c *TrieCache) Size() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.curSize
}

// Prune evicts the oldest entries until the cache is at or below the
// target byte size. Returns the number of entries evicted.
func (c *TrieCache) Prune(targetSize uint64) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	evicted := 0
	for c.curSize > targetSize && c.tail != nil {
		c.evictTailLocked()
		evicted++
	}
	return evicted
}

// Stats returns a snapshot of the cache performance statistics.
func (c *TrieCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		Hits:        c.hits.Load(),
		Misses:      c.misses.Load(),
		Evictions:   c.evictions.Load(),
		CurrentSize: c.curSize,
		EntryCount:  len(c.entries),
	}
}

// Reset clears all entries and resets statistics.
func (c *TrieCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[[32]byte]*cacheEntry)
	c.head = nil
	c.tail = nil
	c.curSize = 0
	c.hits.Store(0)
	c.misses.Store(0)
	c.evictions.Store(0)
}

// HitRate returns the cache hit rate as a float64 in [0, 1].
// Returns 0 if no lookups have been made.
func (c *TrieCache) HitRate() float64 {
	hits := c.hits.Load()
	misses := c.misses.Load()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// --- Internal linked list operations ---

// pushFrontLocked inserts an entry at the head of the LRU list.
// Caller must hold c.mu.
func (c *TrieCache) pushFrontLocked(entry *cacheEntry) {
	entry.prev = nil
	entry.next = c.head
	if c.head != nil {
		c.head.prev = entry
	}
	c.head = entry
	if c.tail == nil {
		c.tail = entry
	}
}

// moveToFrontLocked moves an existing entry to the head of the LRU list.
// Caller must hold c.mu.
func (c *TrieCache) moveToFrontLocked(entry *cacheEntry) {
	if entry == c.head {
		return
	}
	c.removeLocked(entry)
	c.pushFrontLocked(entry)
}

// removeLocked removes an entry from the LRU list without deleting from map.
// Caller must hold c.mu.
func (c *TrieCache) removeLocked(entry *cacheEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		c.head = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		c.tail = entry.prev
	}
	entry.prev = nil
	entry.next = nil
}

// evictTailLocked removes the least recently used entry from the cache.
// Caller must hold c.mu.
func (c *TrieCache) evictTailLocked() {
	if c.tail == nil {
		return
	}

	evicted := c.tail
	c.removeLocked(evicted)
	delete(c.entries, evicted.hash)
	c.curSize -= evicted.size
	c.evictions.Add(1)
}
