package rawdb

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// HashCacheConfig holds configuration for the HashCache.
type HashCacheConfig struct {
	MaxEntries    int  // maximum number of entries (default 1024)
	EnableMetrics bool // whether to track hit/miss/eviction stats
}

// DefaultHashCacheConfig returns a HashCacheConfig with sensible defaults.
func DefaultHashCacheConfig() HashCacheConfig {
	return HashCacheConfig{
		MaxEntries:    1024,
		EnableMetrics: true,
	}
}

// HashCacheEntry represents a cached block number -> hash mapping.
type HashCacheEntry struct {
	Number    uint64
	Hash      types.Hash
	Timestamp int64 // unix timestamp when this entry was added
}

// HashCacheStats holds hit/miss/eviction statistics for the cache.
type HashCacheStats struct {
	Hits      uint64
	Misses    uint64
	Evictions uint64
	Size      int
}

// hashCacheNode is a doubly-linked list node for LRU ordering.
type hashCacheNode struct {
	entry HashCacheEntry
	prev  *hashCacheNode
	next  *hashCacheNode
}

// HashCache is a thread-safe LRU cache mapping block numbers to block hashes.
// It supports both forward (number->hash) and reverse (hash->number) lookups.
type HashCache struct {
	mu         sync.RWMutex
	maxEntries int
	metrics    bool

	// Primary map: block number -> linked list node.
	byNumber map[uint64]*hashCacheNode
	// Reverse map: hash -> block number.
	byHash map[types.Hash]uint64

	// Doubly-linked list for LRU: head is most recently used, tail is least.
	head *hashCacheNode
	tail *hashCacheNode

	// Statistics (atomic for lock-free reads).
	hits      atomic.Uint64
	misses    atomic.Uint64
	evictions atomic.Uint64
}

// NewHashCache creates a new HashCache with the given configuration.
func NewHashCache(config HashCacheConfig) *HashCache {
	maxEntries := config.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	return &HashCache{
		maxEntries: maxEntries,
		metrics:    config.EnableMetrics,
		byNumber:   make(map[uint64]*hashCacheNode, maxEntries),
		byHash:     make(map[types.Hash]uint64, maxEntries),
	}
}

// Put stores a block number -> hash mapping. If the cache is full, the
// least recently used entry is evicted.
func (c *HashCache) Put(number uint64, hash types.Hash) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If this number already exists, update it.
	if node, ok := c.byNumber[number]; ok {
		// Remove old reverse mapping.
		delete(c.byHash, node.entry.Hash)
		// Update the entry.
		node.entry.Hash = hash
		node.entry.Timestamp = time.Now().Unix()
		// Add new reverse mapping.
		c.byHash[hash] = number
		// Move to front (most recently used).
		c.moveToFront(node)
		return
	}

	// Evict if at capacity.
	if len(c.byNumber) >= c.maxEntries {
		c.evictLRU()
	}

	// Create new node.
	node := &hashCacheNode{
		entry: HashCacheEntry{
			Number:    number,
			Hash:      hash,
			Timestamp: time.Now().Unix(),
		},
	}
	c.byNumber[number] = node
	c.byHash[hash] = number
	c.pushFront(node)
}

// Get retrieves the hash for a given block number. Returns the hash and
// true if found, or a zero hash and false if not.
func (c *HashCache) Get(number uint64) (types.Hash, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	node, ok := c.byNumber[number]
	if !ok {
		if c.metrics {
			c.misses.Add(1)
		}
		return types.Hash{}, false
	}
	if c.metrics {
		c.hits.Add(1)
	}
	c.moveToFront(node)
	return node.entry.Hash, true
}

// GetByHash performs a reverse lookup: given a hash, returns the block number.
func (c *HashCache) GetByHash(hash types.Hash) (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	number, ok := c.byHash[hash]
	if !ok {
		if c.metrics {
			c.misses.Add(1)
		}
		return 0, false
	}
	if c.metrics {
		c.hits.Add(1)
	}
	// Move the corresponding node to front.
	if node, exists := c.byNumber[number]; exists {
		c.moveToFront(node)
	}
	return number, true
}

// Contains checks whether a block number is in the cache.
func (c *HashCache) Contains(number uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.byNumber[number]
	return ok
}

// Remove evicts a specific block number from the cache.
func (c *HashCache) Remove(number uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	node, ok := c.byNumber[number]
	if !ok {
		return
	}
	c.removeNode(node)
	delete(c.byNumber, number)
	delete(c.byHash, node.entry.Hash)
}

// Len returns the current number of entries in the cache.
func (c *HashCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.byNumber)
}

// Purge removes all entries from the cache.
func (c *HashCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.byNumber = make(map[uint64]*hashCacheNode, c.maxEntries)
	c.byHash = make(map[types.Hash]uint64, c.maxEntries)
	c.head = nil
	c.tail = nil
}

// Entries returns all cached entries in MRU (most recently used) order.
func (c *HashCache) Entries() []HashCacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries := make([]HashCacheEntry, 0, len(c.byNumber))
	node := c.head
	for node != nil {
		entries = append(entries, node.entry)
		node = node.next
	}
	return entries
}

// Stats returns current cache hit/miss/eviction statistics.
func (c *HashCache) Stats() HashCacheStats {
	c.mu.RLock()
	size := len(c.byNumber)
	c.mu.RUnlock()

	return HashCacheStats{
		Hits:      c.hits.Load(),
		Misses:    c.misses.Load(),
		Evictions: c.evictions.Load(),
		Size:      size,
	}
}

// --- Internal linked-list operations (caller must hold c.mu write lock) ---

// pushFront adds a node to the front of the list (most recently used).
func (c *HashCache) pushFront(node *hashCacheNode) {
	node.prev = nil
	node.next = c.head
	if c.head != nil {
		c.head.prev = node
	}
	c.head = node
	if c.tail == nil {
		c.tail = node
	}
}

// removeNode removes a node from the linked list.
func (c *HashCache) removeNode(node *hashCacheNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		c.tail = node.prev
	}
	node.prev = nil
	node.next = nil
}

// moveToFront moves an existing node to the front (most recently used).
func (c *HashCache) moveToFront(node *hashCacheNode) {
	if c.head == node {
		return // already at front
	}
	c.removeNode(node)
	c.pushFront(node)
}

// evictLRU removes the least recently used entry (tail).
func (c *HashCache) evictLRU() {
	if c.tail == nil {
		return
	}
	victim := c.tail
	c.removeNode(victim)
	delete(c.byNumber, victim.entry.Number)
	delete(c.byHash, victim.entry.Hash)
	if c.metrics {
		c.evictions.Add(1)
	}
}
