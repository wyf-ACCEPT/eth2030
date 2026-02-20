// state_pruner.go implements state-level pruning that tracks recent state roots
// and prunes nodes not reachable from any kept root. Unlike the node-level
// TriePruner (trie_pruner.go), StatePruner operates at the block/state level,
// managing which complete state snapshots should be retained and coordinating
// garbage collection of the underlying node database.
package trie

import (
	"errors"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

var (
	// ErrPrunerStopped is returned when operations are attempted on a stopped pruner.
	ErrPrunerStopped = errors.New("state pruner: stopped")
)

// StateRoot associates a block number with its state trie root hash.
type StateRoot struct {
	Block uint64
	Root  types.Hash
}

// StatePruner tracks recent state roots and coordinates pruning of trie nodes
// that are no longer reachable from any retained state. It maintains a sliding
// window of recent roots and allows marking specific roots as permanently alive.
//
// The pruner does not directly access the node database; instead it provides
// the set of roots that should be kept, and callers use this information to
// drive garbage collection.
type StatePruner struct {
	mu sync.RWMutex

	// recent holds the sliding window of state roots, ordered by block number.
	recent []StateRoot

	// alive holds roots explicitly marked as permanently retained (e.g.,
	// checkpoint roots, finalized roots). These survive any pruning.
	alive map[types.Hash]struct{}

	// maxRecent is the maximum number of recent roots to keep in the window.
	maxRecent int

	// pruned accumulates the total number of roots that have been pruned.
	pruned uint64

	stopped bool
}

// NewStatePruner creates a new state pruner that retains up to maxRecent
// state roots in its sliding window.
func NewStatePruner(maxRecent int) *StatePruner {
	if maxRecent <= 0 {
		maxRecent = 128
	}
	return &StatePruner{
		alive:     make(map[types.Hash]struct{}),
		maxRecent: maxRecent,
	}
}

// AddRoot adds a state root to the recent window. If the window exceeds
// maxRecent, the oldest roots are evicted. Returns the evicted roots.
func (sp *StatePruner) AddRoot(block uint64, root types.Hash) []StateRoot {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.stopped {
		return nil
	}

	sp.recent = append(sp.recent, StateRoot{Block: block, Root: root})

	// Keep sorted by block number.
	sort.Slice(sp.recent, func(i, j int) bool {
		return sp.recent[i].Block < sp.recent[j].Block
	})

	// Evict oldest if over capacity.
	var evicted []StateRoot
	for len(sp.recent) > sp.maxRecent {
		evicted = append(evicted, sp.recent[0])
		sp.recent = sp.recent[1:]
		sp.pruned++
	}
	return evicted
}

// MarkAlive marks a state root as permanently alive. It will not be pruned
// regardless of the sliding window. This is used for checkpoint roots,
// finalized roots, or roots needed for snap sync.
func (sp *StatePruner) MarkAlive(root types.Hash) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.stopped {
		return
	}
	sp.alive[root] = struct{}{}
}

// UnmarkAlive removes a root from the permanently alive set.
func (sp *StatePruner) UnmarkAlive(root types.Hash) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	delete(sp.alive, root)
}

// IsAlive returns true if the given root is either in the recent window
// or explicitly marked as permanently alive.
func (sp *StatePruner) IsAlive(root types.Hash) bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	if _, ok := sp.alive[root]; ok {
		return true
	}
	for _, sr := range sp.recent {
		if sr.Root == root {
			return true
		}
	}
	return false
}

// Prune removes state roots older than keepRecent blocks from the head.
// It returns the pruned roots. Alive-marked roots are never pruned.
func (sp *StatePruner) Prune(keepRecent int) []StateRoot {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.stopped || len(sp.recent) == 0 {
		return nil
	}

	if keepRecent <= 0 {
		keepRecent = sp.maxRecent
	}

	// Find the cutoff: keep the most recent keepRecent entries.
	if keepRecent >= len(sp.recent) {
		return nil
	}

	cutoff := len(sp.recent) - keepRecent
	var pruned []StateRoot
	var kept []StateRoot

	for i := 0; i < cutoff; i++ {
		sr := sp.recent[i]
		if _, isAlive := sp.alive[sr.Root]; isAlive {
			// Keep alive-marked roots even if they're old.
			kept = append(kept, sr)
		} else {
			pruned = append(pruned, sr)
			sp.pruned++
		}
	}

	// Rebuild recent: kept old roots + recent roots within window.
	sp.recent = append(kept, sp.recent[cutoff:]...)

	return pruned
}

// RecentRoots returns a copy of the current recent root window.
func (sp *StatePruner) RecentRoots() []StateRoot {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	result := make([]StateRoot, len(sp.recent))
	copy(result, sp.recent)
	return result
}

// AliveRoots returns a copy of all permanently alive root hashes.
func (sp *StatePruner) AliveRoots() []types.Hash {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	result := make([]types.Hash, 0, len(sp.alive))
	for h := range sp.alive {
		result = append(result, h)
	}
	return result
}

// RetainedRoots returns the full set of roots that should be retained:
// the union of recent roots and permanently alive roots.
func (sp *StatePruner) RetainedRoots() []types.Hash {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	seen := make(map[types.Hash]struct{})
	var result []types.Hash

	for _, sr := range sp.recent {
		if _, ok := seen[sr.Root]; !ok {
			seen[sr.Root] = struct{}{}
			result = append(result, sr.Root)
		}
	}
	for h := range sp.alive {
		if _, ok := seen[h]; !ok {
			seen[h] = struct{}{}
			result = append(result, h)
		}
	}
	return result
}

// WindowSize returns the current number of roots in the recent window.
func (sp *StatePruner) WindowSize() int {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return len(sp.recent)
}

// MaxRecent returns the configured maximum window size.
func (sp *StatePruner) MaxRecent() int {
	return sp.maxRecent
}

// PrunedTotal returns the total number of roots that have been pruned
// since creation.
func (sp *StatePruner) PrunedTotal() uint64 {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.pruned
}

// HeadRoot returns the most recent state root, or an empty hash if none.
func (sp *StatePruner) HeadRoot() (types.Hash, uint64) {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	if len(sp.recent) == 0 {
		return types.Hash{}, 0
	}
	head := sp.recent[len(sp.recent)-1]
	return head.Root, head.Block
}

// Stop prevents any further pruning operations.
func (sp *StatePruner) Stop() {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.stopped = true
}

// Reset clears all state and allows the pruner to be reused.
func (sp *StatePruner) Reset() {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	sp.recent = nil
	sp.alive = make(map[types.Hash]struct{})
	sp.pruned = 0
	sp.stopped = false
}

// StatePrunerStats holds statistics about the state pruner.
type StatePrunerStats struct {
	WindowSize  int
	AliveCount  int
	PrunedTotal uint64
	HeadBlock   uint64
}

// Stats returns a snapshot of pruner statistics.
func (sp *StatePruner) Stats() StatePrunerStats {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	var headBlock uint64
	if len(sp.recent) > 0 {
		headBlock = sp.recent[len(sp.recent)-1].Block
	}

	return StatePrunerStats{
		WindowSize:  len(sp.recent),
		AliveCount:  len(sp.alive),
		PrunedTotal: sp.pruned,
		HeadBlock:   headBlock,
	}
}
