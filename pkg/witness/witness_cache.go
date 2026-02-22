// witness_cache.go implements a block-scoped cache for execution witnesses
// to support stateless validation. The cache stores CachedWitness entries
// keyed by block hash, supports pruning old blocks, and tracks hit/miss
// statistics.
//
// This cache allows validators and stateless nodes to quickly retrieve
// previously seen witnesses without re-downloading or recomputing them.
//
// Thread-safe: all public methods use sync.RWMutex for concurrent access.
package witness

import (
	"sync"
	"sync/atomic"
)

// CachedWitness holds a witness for a specific block, along with metadata
// needed for stateless validation.
type CachedWitness struct {
	// BlockHash is the hash of the block this witness covers.
	BlockHash [32]byte
	// BlockNumber is the block number.
	BlockNumber uint64
	// StateRoot is the pre-state root before this block's execution.
	StateRoot [32]byte
	// AccountProofs maps account address hashes to their Merkle proofs.
	AccountProofs map[[32]byte][]byte
	// StorageProofs maps account hashes to their storage proof sets.
	// Each inner map keys storage slot hashes to proof bytes.
	StorageProofs map[[32]byte]map[[32]byte][]byte
	// CodeChunks maps code hashes to their bytecode.
	CodeChunks map[[32]byte][]byte
	// Size is the estimated total size in bytes for this witness.
	Size uint64
}

// WitnessCacheStats holds aggregate statistics about the witness cache.
type WitnessCacheStats struct {
	Entries   uint64
	TotalSize uint64
	Hits      uint64
	Misses    uint64
}

// witnessCacheEntry wraps a CachedWitness for internal tracking.
type witnessCacheEntry struct {
	witness *CachedWitness
}

// WitnessCache is a block-scoped cache for execution witnesses.
// It stores witnesses keyed by block hash and supports size-bounded
// eviction based on block count.
type WitnessCache struct {
	mu          sync.RWMutex
	entries     map[[32]byte]*witnessCacheEntry
	maxBlocks   int
	insertOrder [][32]byte // tracks insertion order for eviction

	hits   atomic.Uint64
	misses atomic.Uint64
}

// NewWitnessCache creates a new witness cache with the given maximum
// number of block witnesses. If maxBlocks <= 0, defaults to 128.
func NewWitnessCache(maxBlocks int) *WitnessCache {
	if maxBlocks <= 0 {
		maxBlocks = 128
	}
	return &WitnessCache{
		entries:     make(map[[32]byte]*witnessCacheEntry),
		maxBlocks:   maxBlocks,
		insertOrder: make([][32]byte, 0, maxBlocks),
	}
}

// StoreWitness stores a witness for the given block hash. If the cache is
// full, the oldest entry is evicted. If a witness for the same block hash
// already exists, it is overwritten.
func (c *WitnessCache) StoreWitness(blockHash [32]byte, witness *CachedWitness) {
	if witness == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// If already present, update in place.
	if _, exists := c.entries[blockHash]; exists {
		c.entries[blockHash] = &witnessCacheEntry{witness: witness}
		return
	}

	// Evict oldest if at capacity.
	for len(c.entries) >= c.maxBlocks && len(c.insertOrder) > 0 {
		oldest := c.insertOrder[0]
		c.insertOrder = c.insertOrder[1:]
		delete(c.entries, oldest)
	}

	c.entries[blockHash] = &witnessCacheEntry{witness: witness}
	c.insertOrder = append(c.insertOrder, blockHash)
}

// GetWitness retrieves a cached witness by block hash. Returns nil and
// false if not found.
func (c *WitnessCache) GetWitness(blockHash [32]byte) (*CachedWitness, bool) {
	c.mu.RLock()
	entry, ok := c.entries[blockHash]
	c.mu.RUnlock()

	if !ok {
		c.misses.Add(1)
		return nil, false
	}

	c.hits.Add(1)
	return entry.witness, true
}

// HasWitness checks whether a witness exists in the cache for the given
// block hash, without affecting hit/miss stats.
func (c *WitnessCache) HasWitness(blockHash [32]byte) bool {
	c.mu.RLock()
	_, ok := c.entries[blockHash]
	c.mu.RUnlock()
	return ok
}

// RemoveWitness removes a specific witness from the cache.
func (c *WitnessCache) RemoveWitness(blockHash [32]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.entries[blockHash]; ok {
		delete(c.entries, blockHash)
		c.removeFromOrder(blockHash)
	}
}

// PruneBeforeBlock removes all cached witnesses with a block number
// strictly less than the given threshold. Returns the number of entries
// removed.
func (c *WitnessCache) PruneBeforeBlock(blockNumber uint64) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toRemove [][32]byte
	for hash, entry := range c.entries {
		if entry.witness.BlockNumber < blockNumber {
			toRemove = append(toRemove, hash)
		}
	}

	for _, hash := range toRemove {
		delete(c.entries, hash)
		c.removeFromOrder(hash)
	}

	return len(toRemove)
}

// TotalSize returns the sum of all cached witness sizes in bytes.
func (c *WitnessCache) TotalSize() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var total uint64
	for _, entry := range c.entries {
		total += entry.witness.Size
	}
	return total
}

// Stats returns a snapshot of cache statistics.
func (c *WitnessCache) Stats() *WitnessCacheStats {
	c.mu.RLock()
	entries := uint64(len(c.entries))
	var totalSize uint64
	for _, entry := range c.entries {
		totalSize += entry.witness.Size
	}
	c.mu.RUnlock()

	return &WitnessCacheStats{
		Entries:   entries,
		TotalSize: totalSize,
		Hits:      c.hits.Load(),
		Misses:    c.misses.Load(),
	}
}

// removeFromOrder removes a hash from the insertion order slice.
// Caller must hold c.mu.
func (c *WitnessCache) removeFromOrder(hash [32]byte) {
	for i, h := range c.insertOrder {
		if h == hash {
			c.insertOrder = append(c.insertOrder[:i], c.insertOrder[i+1:]...)
			return
		}
	}
}
