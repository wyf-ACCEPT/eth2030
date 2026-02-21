// signature_cache_lru.go implements a simplified LRU cache for verified ECDSA
// signature results, keyed by keccak256(hash || sig). This avoids redundant
// ecrecover work when the same transaction is re-validated (e.g., after reorgs
// or mempool re-broadcasts).
//
// The cache uses a doubly-linked list for LRU eviction and a map for O(1)
// lookups. All operations are thread-safe via sync.RWMutex.
package crypto

import (
	"sync"
	"sync/atomic"
)

// SigCacheStats holds hit/miss statistics for a SigLRUCache.
type SigCacheStats struct {
	Hits    uint64
	Misses  uint64
	Entries uint64
}

// sigLRUNode is a doubly-linked list node for the LRU eviction list.
type sigLRUNode struct {
	cacheKey [32]byte
	hash     [32]byte
	sig      [65]byte
	address  [20]byte
	prev     *sigLRUNode
	next     *sigLRUNode
}

// SigLRUCache is a thread-safe LRU cache for verified ECDSA signature results.
// Cache keys are derived from keccak256(hash || sig) to uniquely identify
// (message, signature) pairs. On a hit, the recovered address is returned
// without repeating the expensive ecrecover operation.
type SigLRUCache struct {
	mu       sync.RWMutex
	capacity int
	items    map[[32]byte]*sigLRUNode

	// Doubly-linked list: head is MRU, tail is LRU.
	head *sigLRUNode
	tail *sigLRUNode

	// Atomic counters for lock-free stats reads.
	hits   atomic.Uint64
	misses atomic.Uint64
}

// NewSigLRUCache creates a new signature LRU cache with the given capacity.
// If capacity is <= 0, a default of 4096 is used.
func NewSigLRUCache(capacity int) *SigLRUCache {
	if capacity <= 0 {
		capacity = 4096
	}
	return &SigLRUCache{
		capacity: capacity,
		items:    make(map[[32]byte]*sigLRUNode, capacity),
	}
}

// makeSigCacheKey derives a deterministic cache key from a message hash and
// signature by computing keccak256(hash || sig).
func makeSigCacheKey(hash [32]byte, sig [65]byte) [32]byte {
	buf := make([]byte, 32+65)
	copy(buf[:32], hash[:])
	copy(buf[32:], sig[:])
	return Keccak256Hash(buf)
}

// Lookup checks the cache for a verified (hash, sig) pair. If found, returns
// the previously recovered address and true. On a miss, returns a zero address
// and false. A hit promotes the entry to MRU position.
func (c *SigLRUCache) Lookup(hash [32]byte, sig [65]byte) (address [20]byte, found bool) {
	key := makeSigCacheKey(hash, sig)

	c.mu.RLock()
	node, ok := c.items[key]
	c.mu.RUnlock()

	if !ok {
		c.misses.Add(1)
		return [20]byte{}, false
	}

	// Promote to MRU.
	c.mu.Lock()
	c.sigMoveToHead(node)
	c.mu.Unlock()

	c.hits.Add(1)
	return node.address, true
}

// Add inserts a verified signature result into the cache. If the cache is at
// capacity, the least recently used entry is evicted. If the (hash, sig) pair
// already exists, the address is updated and the entry is promoted.
func (c *SigLRUCache) Add(hash [32]byte, sig [65]byte, address [20]byte) {
	key := makeSigCacheKey(hash, sig)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing entry.
	if existing, ok := c.items[key]; ok {
		existing.address = address
		c.sigMoveToHead(existing)
		return
	}

	// Create new node.
	node := &sigLRUNode{
		cacheKey: key,
		hash:     hash,
		sig:      sig,
		address:  address,
	}
	c.items[key] = node
	c.sigPushHead(node)

	// Evict LRU if over capacity.
	if len(c.items) > c.capacity {
		c.sigEvictTail()
	}
}

// Remove invalidates the cache entry for the given message hash. This removes
// all entries whose message hash matches, regardless of signature. This is
// useful when a transaction is replaced or dropped.
func (c *SigLRUCache) Remove(hash [32]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// We must scan all entries since the cache key incorporates the sig.
	// For safety, collect keys to remove first.
	var toRemove [][32]byte
	for _, node := range c.items {
		if node.hash == hash {
			toRemove = append(toRemove, node.cacheKey)
		}
	}
	for _, key := range toRemove {
		if node, ok := c.items[key]; ok {
			c.sigRemoveNode(node)
			delete(c.items, key)
		}
	}
}

// Len returns the number of entries currently in the cache.
func (c *SigLRUCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Clear removes all entries from the cache and resets hit/miss counters.
func (c *SigLRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[[32]byte]*sigLRUNode, c.capacity)
	c.head = nil
	c.tail = nil
	c.hits.Store(0)
	c.misses.Store(0)
}

// HitRate returns the cache hit percentage as a value in [0, 1].
// Returns 0 if no lookups have been performed.
func (c *SigLRUCache) HitRate() float64 {
	h := c.hits.Load()
	m := c.misses.Load()
	total := h + m
	if total == 0 {
		return 0
	}
	return float64(h) / float64(total)
}

// Stats returns a snapshot of the cache statistics.
func (c *SigLRUCache) Stats() *SigCacheStats {
	c.mu.RLock()
	entries := uint64(len(c.items))
	c.mu.RUnlock()
	return &SigCacheStats{
		Hits:    c.hits.Load(),
		Misses:  c.misses.Load(),
		Entries: entries,
	}
}

// --- internal linked-list operations (caller must hold c.mu write lock) ---

func (c *SigLRUCache) sigPushHead(node *sigLRUNode) {
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

func (c *SigLRUCache) sigRemoveNode(node *sigLRUNode) {
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

func (c *SigLRUCache) sigMoveToHead(node *sigLRUNode) {
	if c.head == node {
		return
	}
	c.sigRemoveNode(node)
	c.sigPushHead(node)
}

func (c *SigLRUCache) sigEvictTail() {
	if c.tail == nil {
		return
	}
	evicted := c.tail
	c.sigRemoveNode(evicted)
	delete(c.items, evicted.cacheKey)
}
