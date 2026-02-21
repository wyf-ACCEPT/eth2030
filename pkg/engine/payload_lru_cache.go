// payload_lru_cache.go implements an LRU cache for recently-built execution
// payloads, keyed by PayloadID. It avoids redundant computation when the
// consensus layer requests the same payload multiple times.
package engine

import (
	"errors"
	"sync"
	"sync/atomic"
)

// Default maximum entries for the payload LRU cache.
const DefaultLRUCacheMaxEntries = 64

// LRUCachePayload represents an execution payload stored in the LRU cache.
type LRUCachePayload struct {
	ParentHash    [32]byte
	FeeRecipient  [20]byte
	StateRoot     [32]byte
	ReceiptsRoot  [32]byte
	BlockNumber   uint64
	GasLimit      uint64
	GasUsed       uint64
	Timestamp     uint64
	BaseFeePerGas uint64
	BlockHash     [32]byte
	Transactions  [][]byte
}

// LRUCacheStats tracks hit/miss/eviction statistics for the payload cache.
type LRUCacheStats struct {
	Hits      uint64
	Misses    uint64
	Evictions uint64
}

// lruEntry wraps a payload with its key and doubly-linked list pointers
// for O(1) LRU tracking.
type lruEntry struct {
	id      PayloadID
	payload *LRUCachePayload
	prev    *lruEntry
	next    *lruEntry
}

// PayloadLRUCache is a thread-safe LRU cache for execution payloads keyed
// by PayloadID. When the cache is full, the least-recently-used entry is
// evicted to make room for a new one.
type PayloadLRUCache struct {
	mu         sync.RWMutex
	maxEntries int
	entries    map[PayloadID]*lruEntry
	// hashIndex maps BlockHash -> PayloadID for GetByBlockHash lookups.
	hashIndex map[[32]byte]PayloadID

	// Doubly-linked list head (most recent) and tail (least recent).
	head *lruEntry
	tail *lruEntry

	// Statistics tracked atomically for reads without locking.
	hits      atomic.Uint64
	misses    atomic.Uint64
	evictions atomic.Uint64
}

// NewPayloadLRUCache creates a new payload LRU cache. If maxEntries <= 0,
// DefaultLRUCacheMaxEntries is used.
func NewPayloadLRUCache(maxEntries int) *PayloadLRUCache {
	if maxEntries <= 0 {
		maxEntries = DefaultLRUCacheMaxEntries
	}
	return &PayloadLRUCache{
		maxEntries: maxEntries,
		entries:    make(map[PayloadID]*lruEntry),
		hashIndex:  make(map[[32]byte]PayloadID),
	}
}

// Put stores a payload in the cache under the given PayloadID. If the cache
// is at capacity, the least-recently-used entry is evicted first. Storing a
// payload with an existing ID updates the entry and moves it to the front.
func (c *PayloadLRUCache) Put(id PayloadID, payload *LRUCachePayload) error {
	if payload == nil {
		return errors.New("payload_lru_cache: nil payload")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// If key already exists, update in place and move to front.
	if entry, ok := c.entries[id]; ok {
		// Remove old block hash index entry if hash changed.
		if entry.payload.BlockHash != payload.BlockHash {
			delete(c.hashIndex, entry.payload.BlockHash)
		}
		entry.payload = payload
		c.hashIndex[payload.BlockHash] = id
		c.moveToFront(entry)
		return nil
	}

	// Evict if at capacity.
	if len(c.entries) >= c.maxEntries {
		c.evictLRU()
	}

	entry := &lruEntry{id: id, payload: payload}
	c.entries[id] = entry
	c.hashIndex[payload.BlockHash] = id
	c.pushFront(entry)
	return nil
}

// Get retrieves a payload by PayloadID. If found, the entry is moved to
// the front of the LRU list. Returns the payload and true if found.
func (c *PayloadLRUCache) Get(id PayloadID) (*LRUCachePayload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[id]
	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	c.hits.Add(1)
	c.moveToFront(entry)
	return entry.payload, true
}

// GetByBlockHash retrieves a payload by its BlockHash field. If found, the
// entry is moved to the front of the LRU list.
func (c *PayloadLRUCache) GetByBlockHash(hash [32]byte) (*LRUCachePayload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id, ok := c.hashIndex[hash]
	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	entry, ok := c.entries[id]
	if !ok {
		// Stale index entry; clean up.
		delete(c.hashIndex, hash)
		c.misses.Add(1)
		return nil, false
	}
	c.hits.Add(1)
	c.moveToFront(entry)
	return entry.payload, true
}

// Remove removes a payload by PayloadID. Returns true if the entry existed.
func (c *PayloadLRUCache) Remove(id PayloadID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[id]
	if !ok {
		return false
	}
	c.removeEntry(entry)
	return true
}

// Len returns the number of entries currently in the cache.
func (c *PayloadLRUCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Clear removes all entries from the cache and resets statistics.
func (c *PayloadLRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[PayloadID]*lruEntry)
	c.hashIndex = make(map[[32]byte]PayloadID)
	c.head = nil
	c.tail = nil
	c.hits.Store(0)
	c.misses.Store(0)
	c.evictions.Store(0)
}

// Stats returns a snapshot of cache hit/miss/eviction statistics.
func (c *PayloadLRUCache) Stats() *LRUCacheStats {
	return &LRUCacheStats{
		Hits:      c.hits.Load(),
		Misses:    c.misses.Load(),
		Evictions: c.evictions.Load(),
	}
}

// pushFront inserts an entry at the front (most-recently-used) of the list.
// Caller must hold c.mu.
func (c *PayloadLRUCache) pushFront(entry *lruEntry) {
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

// moveToFront moves an existing entry to the front of the list.
// Caller must hold c.mu.
func (c *PayloadLRUCache) moveToFront(entry *lruEntry) {
	if c.head == entry {
		return // Already at front.
	}
	c.detach(entry)
	c.pushFront(entry)
}

// detach removes an entry from the linked list without deleting it from the map.
// Caller must hold c.mu.
func (c *PayloadLRUCache) detach(entry *lruEntry) {
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

// removeEntry removes an entry from both the linked list and the maps.
// Caller must hold c.mu.
func (c *PayloadLRUCache) removeEntry(entry *lruEntry) {
	c.detach(entry)
	delete(c.entries, entry.id)
	delete(c.hashIndex, entry.payload.BlockHash)
}

// evictLRU removes the least-recently-used entry (the tail).
// Caller must hold c.mu.
func (c *PayloadLRUCache) evictLRU() {
	if c.tail == nil {
		return
	}
	c.removeEntry(c.tail)
	c.evictions.Add(1)
}
