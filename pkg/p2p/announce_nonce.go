// announce_nonce.go implements the nonce announcement protocol for efficient
// block propagation. Part of the J+ era roadmap: peers announce nonces
// associated with blocks to enable lightweight validation before full block
// download, reducing wasted bandwidth on invalid blocks.
package p2p

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Default nonce announcement parameters.
const (
	// DefaultNonceCacheSize is the maximum number of nonce entries per peer.
	DefaultNonceCacheSize = 1024

	// DefaultNonceTTL is how long a nonce record remains valid.
	DefaultNonceTTL = 5 * time.Minute

	// DefaultMaxPeers is the maximum number of tracked peers.
	DefaultMaxPeers = 256
)

// Nonce announcement errors.
var (
	ErrNonceEmpty     = errors.New("nonce: empty peer ID")
	ErrNonceZeroHash  = errors.New("nonce: zero block hash")
	ErrNonceDuplicate = errors.New("nonce: duplicate announcement")
	ErrNonceTooMany   = errors.New("nonce: max peers exceeded")
	ErrNonceNotFound  = errors.New("nonce: record not found")
)

// NonceRecord stores a single nonce announcement from a peer.
type NonceRecord struct {
	PeerID    string
	BlockHash types.Hash
	Nonce     uint64
	Timestamp time.Time
}

// nonceCacheEntry is an internal LRU-managed cache entry.
type nonceCacheEntry struct {
	record NonceRecord
	prev   *nonceCacheEntry
	next   *nonceCacheEntry
}

// NonceCache is a per-peer LRU cache of nonce records with TTL expiry.
type NonceCache struct {
	maxSize int
	ttl     time.Duration

	// Doubly-linked list for LRU ordering (head = most recent).
	head *nonceCacheEntry
	tail *nonceCacheEntry

	// Index by block hash for fast lookup.
	index map[types.Hash]*nonceCacheEntry
	size  int
}

// newNonceCache creates a new nonce cache with the given capacity and TTL.
func newNonceCache(maxSize int, ttl time.Duration) *NonceCache {
	return &NonceCache{
		maxSize: maxSize,
		ttl:     ttl,
		index:   make(map[types.Hash]*nonceCacheEntry, maxSize),
	}
}

// put adds a record to the cache, evicting the LRU entry if full.
// Returns true if the entry was newly added, false if it already existed.
func (nc *NonceCache) put(rec NonceRecord) bool {
	if entry, ok := nc.index[rec.BlockHash]; ok {
		// Update existing entry and move to front.
		entry.record = rec
		nc.moveToFront(entry)
		return false
	}

	// Evict LRU if at capacity.
	if nc.size >= nc.maxSize {
		nc.evictTail()
	}

	entry := &nonceCacheEntry{record: rec}
	nc.index[rec.BlockHash] = entry
	nc.pushFront(entry)
	nc.size++
	return true
}

// get retrieves a nonce record by block hash, returning nil if not found or expired.
func (nc *NonceCache) get(hash types.Hash) *NonceRecord {
	entry, ok := nc.index[hash]
	if !ok {
		return nil
	}

	// Check TTL.
	if time.Since(entry.record.Timestamp) > nc.ttl {
		nc.remove(entry)
		return nil
	}

	nc.moveToFront(entry)
	rec := entry.record
	return &rec
}

// getAll returns all non-expired records.
func (nc *NonceCache) getAll() []NonceRecord {
	var records []NonceRecord
	now := time.Now()
	entry := nc.head
	for entry != nil {
		if now.Sub(entry.record.Timestamp) <= nc.ttl {
			records = append(records, entry.record)
		}
		entry = entry.next
	}
	return records
}

// pruneStale removes entries older than maxAge. Returns count of removed entries.
func (nc *NonceCache) pruneStale(maxAge time.Duration) int {
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	entry := nc.tail
	for entry != nil {
		prev := entry.prev
		if entry.record.Timestamp.Before(cutoff) {
			nc.remove(entry)
			removed++
		}
		entry = prev
	}
	return removed
}

// len returns the number of entries.
func (nc *NonceCache) len() int {
	return nc.size
}

// pushFront adds an entry to the front of the LRU list.
func (nc *NonceCache) pushFront(entry *nonceCacheEntry) {
	entry.prev = nil
	entry.next = nc.head
	if nc.head != nil {
		nc.head.prev = entry
	}
	nc.head = entry
	if nc.tail == nil {
		nc.tail = entry
	}
}

// moveToFront moves an existing entry to the front.
func (nc *NonceCache) moveToFront(entry *nonceCacheEntry) {
	if entry == nc.head {
		return
	}
	nc.unlink(entry)
	nc.pushFront(entry)
}

// evictTail removes the least recently used entry.
func (nc *NonceCache) evictTail() {
	if nc.tail == nil {
		return
	}
	nc.remove(nc.tail)
}

// remove removes an entry from the cache entirely.
func (nc *NonceCache) remove(entry *nonceCacheEntry) {
	nc.unlink(entry)
	delete(nc.index, entry.record.BlockHash)
	nc.size--
}

// unlink removes an entry from the linked list without removing from index.
func (nc *NonceCache) unlink(entry *nonceCacheEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		nc.head = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		nc.tail = entry.prev
	}
	entry.prev = nil
	entry.next = nil
}

// NonceAnnouncer manages nonce announcements across all peers. It maintains
// a per-peer LRU cache of nonce records and supports validation, pruning,
// and lookup. Thread-safe.
type NonceAnnouncer struct {
	mu sync.RWMutex

	// Per-peer nonce caches.
	peers map[string]*NonceCache

	// Configuration.
	cacheSize int
	ttl       time.Duration
	maxPeers  int
}

// NewNonceAnnouncer creates a new nonce announcer with default settings.
func NewNonceAnnouncer() *NonceAnnouncer {
	return &NonceAnnouncer{
		peers:     make(map[string]*NonceCache),
		cacheSize: DefaultNonceCacheSize,
		ttl:       DefaultNonceTTL,
		maxPeers:  DefaultMaxPeers,
	}
}

// NewNonceAnnouncerWithConfig creates a nonce announcer with custom settings.
func NewNonceAnnouncerWithConfig(cacheSize int, ttl time.Duration, maxPeers int) *NonceAnnouncer {
	if cacheSize <= 0 {
		cacheSize = DefaultNonceCacheSize
	}
	if ttl <= 0 {
		ttl = DefaultNonceTTL
	}
	if maxPeers <= 0 {
		maxPeers = DefaultMaxPeers
	}
	return &NonceAnnouncer{
		peers:     make(map[string]*NonceCache),
		cacheSize: cacheSize,
		ttl:       ttl,
		maxPeers:  maxPeers,
	}
}

// AnnounceNonce records a nonce announcement from a peer for a block hash.
func (na *NonceAnnouncer) AnnounceNonce(peerID string, blockHash types.Hash, nonce uint64) error {
	if peerID == "" {
		return ErrNonceEmpty
	}
	if blockHash.IsZero() {
		return ErrNonceZeroHash
	}

	na.mu.Lock()
	defer na.mu.Unlock()

	cache, ok := na.peers[peerID]
	if !ok {
		if len(na.peers) >= na.maxPeers {
			return fmt.Errorf("%w: tracking %d peers", ErrNonceTooMany, len(na.peers))
		}
		cache = newNonceCache(na.cacheSize, na.ttl)
		na.peers[peerID] = cache
	}

	rec := NonceRecord{
		PeerID:    peerID,
		BlockHash: blockHash,
		Nonce:     nonce,
		Timestamp: time.Now(),
	}

	isNew := cache.put(rec)
	if !isNew {
		return ErrNonceDuplicate
	}

	return nil
}

// ValidateNonce checks whether the nonce announced by a peer for a given block
// hash matches the expected nonce. Returns true if a record exists and the
// nonce matches.
func (na *NonceAnnouncer) ValidateNonce(peerID string, blockHash types.Hash, nonce uint64) bool {
	na.mu.RLock()
	defer na.mu.RUnlock()

	cache, ok := na.peers[peerID]
	if !ok {
		return false
	}

	rec := cache.get(blockHash)
	if rec == nil {
		return false
	}

	return rec.Nonce == nonce
}

// GetPeerNonces returns all non-expired nonce records for a given peer.
func (na *NonceAnnouncer) GetPeerNonces(peerID string) []NonceRecord {
	na.mu.RLock()
	defer na.mu.RUnlock()

	cache, ok := na.peers[peerID]
	if !ok {
		return nil
	}

	return cache.getAll()
}

// PruneStale removes nonce entries older than maxAge across all peers.
// Returns the total number of pruned entries.
func (na *NonceAnnouncer) PruneStale(maxAge time.Duration) int {
	na.mu.Lock()
	defer na.mu.Unlock()

	total := 0
	for peerID, cache := range na.peers {
		removed := cache.pruneStale(maxAge)
		total += removed

		// Remove empty peer caches.
		if cache.len() == 0 {
			delete(na.peers, peerID)
		}
	}
	return total
}

// RemovePeer removes all nonce records for a peer.
func (na *NonceAnnouncer) RemovePeer(peerID string) {
	na.mu.Lock()
	defer na.mu.Unlock()
	delete(na.peers, peerID)
}

// PeerCount returns the number of peers being tracked.
func (na *NonceAnnouncer) PeerCount() int {
	na.mu.RLock()
	defer na.mu.RUnlock()
	return len(na.peers)
}

// RecordCount returns the total number of nonce records across all peers.
func (na *NonceAnnouncer) RecordCount() int {
	na.mu.RLock()
	defer na.mu.RUnlock()

	total := 0
	for _, cache := range na.peers {
		total += cache.len()
	}
	return total
}

// HasNonce returns whether a record exists for the given peer and block hash.
func (na *NonceAnnouncer) HasNonce(peerID string, blockHash types.Hash) bool {
	na.mu.RLock()
	defer na.mu.RUnlock()

	cache, ok := na.peers[peerID]
	if !ok {
		return false
	}
	return cache.get(blockHash) != nil
}
