package core

import (
	"sync"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

const (
	// maxCachedStates is the maximum number of state snapshots to keep in memory.
	maxCachedStates = 128

	// stateSnapshotInterval determines how often we cache a state snapshot.
	// Every N blocks, a snapshot is taken to avoid re-execution from genesis.
	stateSnapshotInterval = 16
)

// stateCache caches state snapshots at regular block intervals to avoid
// expensive re-execution from genesis when building state for arbitrary blocks.
type stateCache struct {
	mu        sync.RWMutex
	snapshots map[types.Hash]*stateCacheEntry // block hash → state snapshot
	order     []types.Hash                    // insertion order for eviction
}

type stateCacheEntry struct {
	blockNumber uint64
	stateDB     *state.MemoryStateDB
}

func newStateCache() *stateCache {
	return &stateCache{
		snapshots: make(map[types.Hash]*stateCacheEntry),
	}
}

// get returns a copy of the cached state for the given block hash.
func (sc *stateCache) get(blockHash types.Hash) (*state.MemoryStateDB, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	entry, ok := sc.snapshots[blockHash]
	if !ok {
		return nil, false
	}
	return entry.stateDB.Copy(), true
}

// put stores a state snapshot for the given block.
func (sc *stateCache) put(blockHash types.Hash, blockNumber uint64, stateDB *state.MemoryStateDB) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if _, ok := sc.snapshots[blockHash]; ok {
		return // already cached
	}

	// Evict oldest if at capacity.
	for len(sc.snapshots) >= maxCachedStates {
		oldest := sc.order[0]
		sc.order = sc.order[1:]
		delete(sc.snapshots, oldest)
	}

	sc.snapshots[blockHash] = &stateCacheEntry{
		blockNumber: blockNumber,
		stateDB:     stateDB.Copy(),
	}
	sc.order = append(sc.order, blockHash)
}

// closest finds the cached state snapshot closest to (but not after) the target
// block number. Returns the state copy, the block number it corresponds to,
// and whether a match was found.
func (sc *stateCache) closest(targetNumber uint64) (*state.MemoryStateDB, uint64, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	var best *stateCacheEntry
	for _, entry := range sc.snapshots {
		if entry.blockNumber <= targetNumber {
			if best == nil || entry.blockNumber > best.blockNumber {
				best = entry
			}
		}
	}
	if best == nil {
		return nil, 0, false
	}
	return best.stateDB.Copy(), best.blockNumber, true
}

// remove deletes a cached state entry (e.g. during reorg).
func (sc *stateCache) remove(blockHash types.Hash) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	delete(sc.snapshots, blockHash)
	// Clean order list.
	for i := 0; i < len(sc.order); i++ {
		if sc.order[i] == blockHash {
			sc.order = append(sc.order[:i], sc.order[i+1:]...)
			break
		}
	}
}

// clear removes all cached states (e.g. after a major reorg).
func (sc *stateCache) clear() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.snapshots = make(map[types.Hash]*stateCacheEntry)
	sc.order = nil
}

// shouldSnapshot returns true if we should take a state snapshot at this block.
func shouldSnapshot(blockNumber uint64) bool {
	return blockNumber%stateSnapshotInterval == 0
}
