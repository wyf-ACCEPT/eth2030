// proof_cache.go implements an LRU-style cache for recently verified proofs
// to avoid redundant verification. Cached results include the verification
// outcome, timing, prover identity, and gas cost. Entries expire after a
// configurable TTL and are evicted when the cache exceeds its size limit.
//
// Thread-safe: all public methods use sync.RWMutex for concurrent access.
package proofs

import (
	"sync"
	"sync/atomic"
	"time"
)

// CachedProofResult holds the outcome of a previously verified proof.
type CachedProofResult struct {
	// Valid indicates whether the proof passed verification.
	Valid bool
	// VerifiedAt is the unix timestamp (seconds) when verification completed.
	VerifiedAt int64
	// ProverID identifies the prover that submitted this proof.
	ProverID string
	// GasCost is the gas consumed by the proof verification.
	GasCost uint64
	// VerifyTimeMs is the verification duration in milliseconds.
	VerifyTimeMs int64
}

// ProofCacheStats holds aggregate statistics about the cache.
type ProofCacheStats struct {
	Hits        uint64
	Misses      uint64
	Entries     uint64
	Evictions   uint64
	Expirations uint64
}

// proofCacheEntry wraps a cached result with insertion metadata for eviction.
type proofCacheEntry struct {
	result     *CachedProofResult
	proofType  string
	insertedAt int64 // unix seconds
}

// ProofCache is a thread-safe, size-bounded, TTL-aware cache for verified
// proof results. Entries are keyed by proof hash ([32]byte).
type ProofCache struct {
	mu         sync.RWMutex
	entries    map[[32]byte]*proofCacheEntry
	maxEntries int
	ttlSeconds int64

	// insertOrder tracks insertion order for eviction (oldest first).
	insertOrder [][32]byte

	// Atomic counters for stats.
	hits        atomic.Uint64
	misses      atomic.Uint64
	evictions   atomic.Uint64
	expirations atomic.Uint64
}

// NewProofCache creates a new proof cache with the given size limit and TTL.
// If maxEntries <= 0, it defaults to 1024. If ttlSeconds <= 0, entries never
// expire based on time (but can still be evicted by size).
func NewProofCache(maxEntries int, ttlSeconds int64) *ProofCache {
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	return &ProofCache{
		entries:     make(map[[32]byte]*proofCacheEntry),
		maxEntries:  maxEntries,
		ttlSeconds:  ttlSeconds,
		insertOrder: make([][32]byte, 0, maxEntries),
	}
}

// CacheProof stores a verified proof result in the cache. If the cache is
// full, the oldest entry is evicted. If an entry for the same hash already
// exists, it is overwritten.
func (c *ProofCache) CacheProof(proofHash [32]byte, proofType string, result *CachedProofResult) {
	if result == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// If already present, update in place without changing insert order.
	if _, exists := c.entries[proofHash]; exists {
		c.entries[proofHash] = &proofCacheEntry{
			result:     result,
			proofType:  proofType,
			insertedAt: time.Now().Unix(),
		}
		return
	}

	// Evict oldest entry if at capacity.
	for len(c.entries) >= c.maxEntries && len(c.insertOrder) > 0 {
		oldest := c.insertOrder[0]
		c.insertOrder = c.insertOrder[1:]
		if _, ok := c.entries[oldest]; ok {
			delete(c.entries, oldest)
			c.evictions.Add(1)
		}
	}

	c.entries[proofHash] = &proofCacheEntry{
		result:     result,
		proofType:  proofType,
		insertedAt: time.Now().Unix(),
	}
	c.insertOrder = append(c.insertOrder, proofHash)
}

// LookupProof retrieves a cached proof result. Returns nil and false if the
// entry is not found or has expired (expired entries are removed on lookup).
func (c *ProofCache) LookupProof(proofHash [32]byte) (*CachedProofResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[proofHash]
	if !ok {
		c.misses.Add(1)
		return nil, false
	}

	// Check TTL expiration.
	if c.ttlSeconds > 0 && time.Now().Unix()-entry.insertedAt > c.ttlSeconds {
		delete(c.entries, proofHash)
		c.removeFromOrder(proofHash)
		c.expirations.Add(1)
		c.misses.Add(1)
		return nil, false
	}

	c.hits.Add(1)
	return entry.result, true
}

// Invalidate removes a specific proof from the cache.
func (c *ProofCache) Invalidate(proofHash [32]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.entries[proofHash]; ok {
		delete(c.entries, proofHash)
		c.removeFromOrder(proofHash)
	}
}

// InvalidateByProver removes all cached proofs submitted by the given prover.
// Returns the number of entries removed.
func (c *ProofCache) InvalidateByProver(proverID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toRemove [][32]byte
	for hash, entry := range c.entries {
		if entry.result.ProverID == proverID {
			toRemove = append(toRemove, hash)
		}
	}

	for _, hash := range toRemove {
		delete(c.entries, hash)
		c.removeFromOrder(hash)
	}

	return len(toRemove)
}

// PruneExpired removes all entries whose TTL has elapsed. Returns the number
// of entries removed. If TTL is 0 (no expiration), this is a no-op.
func (c *ProofCache) PruneExpired() int {
	if c.ttlSeconds <= 0 {
		return 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().Unix()
	var toRemove [][32]byte
	for hash, entry := range c.entries {
		if now-entry.insertedAt > c.ttlSeconds {
			toRemove = append(toRemove, hash)
		}
	}

	for _, hash := range toRemove {
		delete(c.entries, hash)
		c.removeFromOrder(hash)
		c.expirations.Add(1)
	}

	return len(toRemove)
}

// Stats returns a snapshot of the cache statistics.
func (c *ProofCache) Stats() *ProofCacheStats {
	c.mu.RLock()
	entries := uint64(len(c.entries))
	c.mu.RUnlock()

	return &ProofCacheStats{
		Hits:        c.hits.Load(),
		Misses:      c.misses.Load(),
		Entries:     entries,
		Evictions:   c.evictions.Load(),
		Expirations: c.expirations.Load(),
	}
}

// HitRate returns the cache hit rate as a fraction [0.0, 1.0]. Returns 0.0
// if no lookups have been performed.
func (c *ProofCache) HitRate() float64 {
	hits := c.hits.Load()
	misses := c.misses.Load()
	total := hits + misses
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total)
}

// removeFromOrder removes a hash from the insertion order slice.
// Caller must hold c.mu.
func (c *ProofCache) removeFromOrder(hash [32]byte) {
	for i, h := range c.insertOrder {
		if h == hash {
			c.insertOrder = append(c.insertOrder[:i], c.insertOrder[i+1:]...)
			return
		}
	}
}
