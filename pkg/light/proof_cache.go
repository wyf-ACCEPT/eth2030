package light

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// ProofType distinguishes proof categories stored in the cache.
type ProofType uint8

const (
	// ProofTypeHeader is a header/block-level proof.
	ProofTypeHeader ProofType = iota
	// ProofTypeAccount is an account (state trie) proof.
	ProofTypeAccount
	// ProofTypeStorage is a storage slot proof.
	ProofTypeStorage
)

// CacheKey is a composite key for cached Merkle proofs.
// It combines block number, account address, and storage key
// so that proofs at different state roots are properly separated.
type CacheKey struct {
	BlockNumber uint64
	Address     types.Address
	StorageKey  types.Hash
	Type        ProofType
}

// CachedProof is a proof entry with expiration metadata.
type CachedProof struct {
	Key       CacheKey
	Proof     []byte
	Value     []byte
	ExpiresAt time.Time
}

// isExpired returns true if the entry has passed its TTL.
func (cp *CachedProof) isExpired(now time.Time) bool {
	return now.After(cp.ExpiresAt)
}

// CacheStats tracks operational statistics of the proof cache.
type CacheStats struct {
	Hits       uint64
	Misses     uint64
	Evictions  uint64
	Inserts    uint64
	MemoryUsed uint64 // approximate bytes
}

// HitRate returns the cache hit rate as a fraction in [0, 1].
// Returns 0 if no lookups have been performed.
func (s *CacheStats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}

// ProofCache is an LRU cache for Merkle proofs used by light clients.
// It supports configurable size limits and time-to-live expiration.
type ProofCache struct {
	mu      sync.Mutex
	maxSize int
	ttl     time.Duration
	entries map[CacheKey]*lruEntry
	order   lruList // doubly-linked list for LRU ordering
	stats   CacheStats
	memUsed uint64
	nowFunc func() time.Time // injectable clock for testing
}

// lruEntry is a node in the LRU doubly-linked list.
type lruEntry struct {
	proof *CachedProof
	prev  *lruEntry
	next  *lruEntry
}

// lruList is a sentinel-based doubly-linked list for LRU ordering.
type lruList struct {
	head lruEntry // sentinel head
	tail lruEntry // sentinel tail
	len  int
}

func (l *lruList) init() {
	l.head.next = &l.tail
	l.tail.prev = &l.head
	l.len = 0
}

// pushFront inserts an entry at the front (most recently used).
func (l *lruList) pushFront(e *lruEntry) {
	e.prev = &l.head
	e.next = l.head.next
	l.head.next.prev = e
	l.head.next = e
	l.len++
}

// remove detaches an entry from the list.
func (l *lruList) remove(e *lruEntry) {
	e.prev.next = e.next
	e.next.prev = e.prev
	e.prev = nil
	e.next = nil
	l.len--
}

// moveToFront moves an existing entry to the front.
func (l *lruList) moveToFront(e *lruEntry) {
	l.remove(e)
	l.pushFront(e)
}

// back returns the least recently used entry, or nil if empty.
func (l *lruList) back() *lruEntry {
	if l.len == 0 {
		return nil
	}
	return l.tail.prev
}

// NewProofCache creates a new proof cache with the given maximum size and TTL.
// maxSize limits the number of cached entries. ttl controls expiration.
func NewProofCache(maxSize int, ttl time.Duration) *ProofCache {
	if maxSize <= 0 {
		maxSize = 1024
	}
	pc := &ProofCache{
		maxSize: maxSize,
		ttl:     ttl,
		entries: make(map[CacheKey]*lruEntry, maxSize),
		nowFunc: time.Now,
	}
	pc.order.init()
	return pc
}

// estimateSize returns the approximate memory footprint of a proof entry.
func estimateSize(p *CachedProof) uint64 {
	// Fixed struct overhead + proof bytes + value bytes.
	return uint64(96 + len(p.Proof) + len(p.Value))
}

// Get retrieves a cached proof by key. Returns the proof and true on hit,
// or nil and false on miss or expiration.
func (pc *ProofCache) Get(key CacheKey) (*CachedProof, bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	entry, ok := pc.entries[key]
	if !ok {
		pc.stats.Misses++
		return nil, false
	}

	// Check TTL expiration.
	if entry.proof.isExpired(pc.now()) {
		pc.evictLocked(entry)
		pc.stats.Misses++
		return nil, false
	}

	// Promote to most recently used.
	pc.order.moveToFront(entry)
	pc.stats.Hits++
	return entry.proof, true
}

// Put inserts or updates a proof in the cache. If the cache is at capacity,
// the least recently used entry is evicted.
func (pc *ProofCache) Put(key CacheKey, proof, value []byte) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	now := pc.now()

	// If the key already exists, update it in place.
	if existing, ok := pc.entries[key]; ok {
		pc.memUsed -= estimateSize(existing.proof)
		existing.proof.Proof = copyBytes(proof)
		existing.proof.Value = copyBytes(value)
		existing.proof.ExpiresAt = now.Add(pc.ttl)
		pc.memUsed += estimateSize(existing.proof)
		pc.order.moveToFront(existing)
		return
	}

	// Evict LRU entries until we have room.
	for pc.order.len >= pc.maxSize {
		victim := pc.order.back()
		if victim == nil {
			break
		}
		pc.evictLocked(victim)
	}

	cp := &CachedProof{
		Key:       key,
		Proof:     copyBytes(proof),
		Value:     copyBytes(value),
		ExpiresAt: now.Add(pc.ttl),
	}

	entry := &lruEntry{proof: cp}
	pc.entries[key] = entry
	pc.order.pushFront(entry)
	pc.memUsed += estimateSize(cp)
	pc.stats.Inserts++
}

// Evict explicitly removes a proof from the cache by key.
// Returns true if the entry was found and removed.
func (pc *ProofCache) Evict(key CacheKey) bool {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	entry, ok := pc.entries[key]
	if !ok {
		return false
	}
	pc.evictLocked(entry)
	return true
}

// evictLocked removes an entry from both the map and the LRU list.
// Caller must hold pc.mu.
func (pc *ProofCache) evictLocked(entry *lruEntry) {
	pc.memUsed -= estimateSize(entry.proof)
	delete(pc.entries, entry.proof.Key)
	pc.order.remove(entry)
	pc.stats.Evictions++
}

// Prefetch asynchronously caches proofs for a range of upcoming block numbers.
// The fetch function is called for each block number to retrieve the proof data.
// If fetch returns a nil proof, the entry is skipped.
func (pc *ProofCache) Prefetch(baseBlock uint64, addr types.Address, storageKey types.Hash, proofType ProofType, count int, fetch func(block uint64) (proof, value []byte)) {
	if count <= 0 || fetch == nil {
		return
	}

	// Use an atomic counter so the goroutine can be traced in tests.
	var done atomic.Int32
	go func() {
		for i := 0; i < count; i++ {
			blockNum := baseBlock + uint64(i)
			proof, value := fetch(blockNum)
			if proof == nil {
				continue
			}
			key := CacheKey{
				BlockNumber: blockNum,
				Address:     addr,
				StorageKey:  storageKey,
				Type:        proofType,
			}
			pc.Put(key, proof, value)
			done.Add(1)
		}
	}()
}

// Stats returns a snapshot of the cache statistics.
func (pc *ProofCache) Stats() CacheStats {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	s := pc.stats
	s.MemoryUsed = pc.memUsed
	return s
}

// Len returns the current number of entries in the cache.
func (pc *ProofCache) Len() int {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return pc.order.len
}

// MaxSize returns the configured maximum number of entries.
func (pc *ProofCache) MaxSize() int {
	return pc.maxSize
}

// now returns the current time, using the injected clock if set.
func (pc *ProofCache) now() time.Time {
	if pc.nowFunc != nil {
		return pc.nowFunc()
	}
	return time.Now()
}

// copyBytes returns a copy of the given byte slice.
func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
