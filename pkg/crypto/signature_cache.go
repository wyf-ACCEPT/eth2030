// signature_cache.go implements an LRU cache for signature verification results.
//
// Signature verification (ECDSA ecrecover, BLS pairing checks) is one of the
// most expensive per-transaction operations. Caching verification results keyed
// by (signature || message_hash) avoids redundant work when the same signed
// message is seen multiple times (e.g., transaction re-validation after a
// reorg, re-broadcast, or mempool churn).
//
// The cache is fully concurrent-safe and exposes hit/miss counters for
// observability.
package crypto

import (
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
)

// SignatureType distinguishes cached signature kinds.
type SignatureType byte

const (
	// SigTypeECDSA is a secp256k1 ECDSA signature (65 bytes).
	SigTypeECDSA SignatureType = 1
	// SigTypeBLS is a BLS12-381 signature (96 bytes).
	SigTypeBLS SignatureType = 2
)

// DefaultSigCacheSize is the default number of entries in the signature cache.
const DefaultSigCacheSize = 4096

// SigCacheEntry holds a cached verification result.
type SigCacheEntry struct {
	// Signer is the recovered address (ECDSA) or serialized public key (BLS).
	Signer types.Address

	// Valid indicates whether the signature verified successfully.
	Valid bool

	// SigType indicates which signature scheme was verified.
	SigType SignatureType
}

// sigCacheNode is a doubly-linked list node for the LRU eviction list.
type sigCacheNode struct {
	key  types.Hash
	val  SigCacheEntry
	prev *sigCacheNode
	next *sigCacheNode
}

// SignatureCache is a concurrent-safe LRU cache for signature verification
// results. Keys are derived from the concatenation of signature bytes and
// message hash to ensure uniqueness.
type SignatureCache struct {
	mu       sync.RWMutex
	capacity int
	items    map[types.Hash]*sigCacheNode

	// Doubly-linked list: head is most recently used, tail is least.
	head *sigCacheNode
	tail *sigCacheNode

	// Atomic hit/miss counters for observability.
	hits   atomic.Int64
	misses atomic.Int64
}

// NewSignatureCache creates a new signature verification cache with the given
// maximum number of entries. If capacity <= 0, DefaultSigCacheSize is used.
func NewSignatureCache(capacity int) *SignatureCache {
	if capacity <= 0 {
		capacity = DefaultSigCacheSize
	}
	return &SignatureCache{
		capacity: capacity,
		items:    make(map[types.Hash]*sigCacheNode, capacity),
	}
}

// SigCacheKey derives a deterministic cache key from a signature and message
// hash. The key is Keccak256(sigType || sig || msgHash), which uniquely
// identifies a (signature, message) pair regardless of encoding variations.
func SigCacheKey(sigType SignatureType, sig []byte, msgHash types.Hash) types.Hash {
	// Pre-allocate: 1 byte type + sig + 32 byte hash.
	buf := make([]byte, 1+len(sig)+types.HashLength)
	buf[0] = byte(sigType)
	copy(buf[1:], sig)
	copy(buf[1+len(sig):], msgHash[:])
	return Keccak256Hash(buf)
}

// Get looks up a cached verification result. Returns the entry and true if
// found, or a zero entry and false on cache miss.
func (c *SignatureCache) Get(key types.Hash) (SigCacheEntry, bool) {
	c.mu.RLock()
	node, ok := c.items[key]
	c.mu.RUnlock()

	if !ok {
		c.misses.Add(1)
		return SigCacheEntry{}, false
	}

	// Promote to head (most recently used). Requires write lock.
	c.mu.Lock()
	c.moveToHead(node)
	c.mu.Unlock()

	c.hits.Add(1)
	return node.val, true
}

// Add inserts a verification result into the cache. If the cache is at
// capacity, the least recently used entry is evicted.
func (c *SignatureCache) Add(key types.Hash, entry SigCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key already exists, update and promote.
	if existing, ok := c.items[key]; ok {
		existing.val = entry
		c.moveToHead(existing)
		return
	}

	// Create new node.
	node := &sigCacheNode{key: key, val: entry}
	c.items[key] = node
	c.pushHead(node)

	// Evict if over capacity.
	if len(c.items) > c.capacity {
		c.evictTail()
	}
}

// Contains checks whether a key exists in the cache without updating the
// LRU order. Useful for fast existence checks.
func (c *SignatureCache) Contains(key types.Hash) bool {
	c.mu.RLock()
	_, ok := c.items[key]
	c.mu.RUnlock()
	return ok
}

// Len returns the number of entries currently in the cache.
func (c *SignatureCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Hits returns the number of cache hits since creation.
func (c *SignatureCache) Hits() int64 {
	return c.hits.Load()
}

// Misses returns the number of cache misses since creation.
func (c *SignatureCache) Misses() int64 {
	return c.misses.Load()
}

// HitRate returns the cache hit rate as a fraction [0, 1]. Returns 0 if no
// lookups have been performed.
func (c *SignatureCache) HitRate() float64 {
	h := c.hits.Load()
	m := c.misses.Load()
	total := h + m
	if total == 0 {
		return 0
	}
	return float64(h) / float64(total)
}

// Purge removes all entries from the cache and resets counters.
func (c *SignatureCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[types.Hash]*sigCacheNode, c.capacity)
	c.head = nil
	c.tail = nil
	c.hits.Store(0)
	c.misses.Store(0)
}

// Remove deletes a single entry from the cache. Returns true if the key was
// present and removed.
func (c *SignatureCache) Remove(key types.Hash) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	node, ok := c.items[key]
	if !ok {
		return false
	}
	c.removeNode(node)
	delete(c.items, key)
	return true
}

// --- internal linked-list operations (caller must hold c.mu) ---

// pushHead inserts a node at the head of the LRU list.
func (c *SignatureCache) pushHead(node *sigCacheNode) {
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

// removeNode unlinks a node from the doubly-linked list.
func (c *SignatureCache) removeNode(node *sigCacheNode) {
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

// moveToHead moves an existing node to the head of the list.
func (c *SignatureCache) moveToHead(node *sigCacheNode) {
	if c.head == node {
		return // already at head
	}
	c.removeNode(node)
	c.pushHead(node)
}

// evictTail removes the least recently used (tail) node.
func (c *SignatureCache) evictTail() {
	if c.tail == nil {
		return
	}
	evicted := c.tail
	c.removeNode(evicted)
	delete(c.items, evicted.key)
}
