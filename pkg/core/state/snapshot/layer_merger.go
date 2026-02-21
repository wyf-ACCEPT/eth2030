// layer_merger.go implements a LayerMerger that reconciles and flattens
// multiple diff layers into a single consolidated diff layer. It supports
// conflict resolution policies, batch merge operations, and produces
// detailed statistics about the merge process.
//
// The merger is designed for snapshot tree maintenance: when the diff layer
// chain grows too long, older layers can be merged to reduce memory usage
// and lookup depth. Unlike the simple flatten (which writes to disk), the
// merger produces an in-memory merged diff layer suitable for intermediate
// compaction.
//
// Key features:
//   - Configurable conflict resolution (newest-wins, oldest-wins, custom)
//   - Deletion tracking (properly propagates account/storage deletions)
//   - Merge statistics (accounts merged, storage slots merged, conflicts)
//   - Priority-based ordering (newer diffs override older)
//   - Efficient seek: binary search over sorted merged keys
package snapshot

import (
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// ConflictPolicy determines how the merger resolves conflicts when the same
// key appears in multiple diff layers.
type ConflictPolicy int

const (
	// PolicyNewestWins resolves conflicts by keeping the value from the
	// newest (highest-index) layer. This is the default and matches
	// Ethereum semantics where later blocks override earlier state.
	PolicyNewestWins ConflictPolicy = iota

	// PolicyOldestWins resolves conflicts by keeping the value from the
	// oldest (lowest-index) layer. Useful for producing a "base" view.
	PolicyOldestWins
)

// String returns a human-readable label for the policy.
func (p ConflictPolicy) String() string {
	switch p {
	case PolicyNewestWins:
		return "newest-wins"
	case PolicyOldestWins:
		return "oldest-wins"
	default:
		return "unknown"
	}
}

// MergeStats records statistics from a layer merge operation.
type MergeStats struct {
	// LayersProcessed is the number of diff layers that were merged.
	LayersProcessed int

	// AccountsMerged is the total number of unique accounts in the result.
	AccountsMerged int

	// AccountConflicts is the number of accounts that appeared in more
	// than one layer (i.e., were overwritten by a later layer).
	AccountConflicts int

	// AccountDeletions is the number of accounts marked as deleted.
	AccountDeletions int

	// StorageSlotsMerged is the total number of unique storage slots.
	StorageSlotsMerged int

	// StorageConflicts is the number of storage slots that appeared in
	// more than one layer.
	StorageConflicts int

	// StorageDeletions is the number of storage slots marked as deleted.
	StorageDeletions int

	// MemoryBefore is the sum of memory estimates for all input layers.
	MemoryBefore uint64

	// MemoryAfter is the memory estimate for the merged output layer.
	MemoryAfter uint64
}

// MemorySaved returns the amount of memory freed by the merge. Returns 0
// if the merged layer is larger (e.g., no deduplication).
func (ms MergeStats) MemorySaved() uint64 {
	if ms.MemoryBefore > ms.MemoryAfter {
		return ms.MemoryBefore - ms.MemoryAfter
	}
	return 0
}

// MergeResult holds the output of a layer merge operation.
type MergeResult struct {
	// Merged is the resulting consolidated diff layer.
	Merged *diffLayer

	// Stats contains detailed merge statistics.
	Stats MergeStats

	// SortedAccountKeys contains the merged account hashes in sorted order,
	// useful for efficient sequential access after merge.
	SortedAccountKeys []types.Hash
}

// LayerMerger merges multiple diff layers into a single consolidated layer.
// It is safe for concurrent use.
type LayerMerger struct {
	mu     sync.Mutex
	policy ConflictPolicy
	stats  MergeStats // cumulative stats across all merges
}

// NewLayerMerger creates a new merger with the given conflict resolution policy.
func NewLayerMerger(policy ConflictPolicy) *LayerMerger {
	return &LayerMerger{
		policy: policy,
	}
}

// Policy returns the current conflict resolution policy.
func (lm *LayerMerger) Policy() ConflictPolicy {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return lm.policy
}

// SetPolicy changes the conflict resolution policy.
func (lm *LayerMerger) SetPolicy(p ConflictPolicy) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.policy = p
}

// CumulativeStats returns aggregate statistics across all merges performed.
func (lm *LayerMerger) CumulativeStats() MergeStats {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return lm.stats
}

// Merge combines multiple diff layers into a single diff layer. Layers must
// be provided from oldest (index 0) to newest (index len-1). The resulting
// layer's root is taken from the newest layer, and its parent is taken from
// the oldest layer's parent.
//
// The policy determines which value wins when the same key appears in
// multiple layers:
//   - PolicyNewestWins: layers are processed oldest-first so newer overwrites.
//   - PolicyOldestWins: layers are processed newest-first so older overwrites.
func (lm *LayerMerger) Merge(layers []*diffLayer) (*MergeResult, error) {
	if len(layers) == 0 {
		return nil, ErrNoLayersToCompact
	}
	if len(layers) == 1 {
		// Single layer: return a copy of it as-is.
		accts, storage := copyLayerData(layers[0])
		parent := layerParent(layers[0])
		merged := newDiffLayer(parent, layers[0].root, accts, storage)

		// Count deletions in the single layer.
		acctDels := 0
		for _, data := range accts {
			if data == nil {
				acctDels++
			}
		}
		storageDels := 0
		totalSlots := 0
		for _, slots := range storage {
			totalSlots += len(slots)
			for _, data := range slots {
				if data == nil {
					storageDels++
				}
			}
		}
		stats := MergeStats{
			LayersProcessed:    1,
			AccountsMerged:     len(accts),
			AccountDeletions:   acctDels,
			StorageSlotsMerged: totalSlots,
			StorageDeletions:   storageDels,
			MemoryBefore:       estimateLayerMemory(layers[0]),
			MemoryAfter:        estimateLayerMemory(merged),
		}

		// Update cumulative stats.
		lm.mu.Lock()
		lm.stats.LayersProcessed += stats.LayersProcessed
		lm.stats.AccountsMerged += stats.AccountsMerged
		lm.stats.AccountDeletions += stats.AccountDeletions
		lm.stats.StorageSlotsMerged += stats.StorageSlotsMerged
		lm.stats.StorageDeletions += stats.StorageDeletions
		lm.stats.MemoryBefore += stats.MemoryBefore
		lm.stats.MemoryAfter += stats.MemoryAfter
		lm.mu.Unlock()

		return &MergeResult{
			Merged:            merged,
			Stats:             stats,
			SortedAccountKeys: sortedKeys(accts),
		}, nil
	}

	lm.mu.Lock()
	policy := lm.policy
	lm.mu.Unlock()

	// Compute memory before.
	var memBefore uint64
	for _, dl := range layers {
		memBefore += estimateLayerMemory(dl)
	}

	// Build iteration order based on policy.
	order := buildOrder(layers, policy)

	// Merge accounts and storage.
	mergedAccounts := make(map[types.Hash][]byte)
	mergedStorage := make(map[types.Hash]map[types.Hash][]byte)
	accountConflicts := 0
	storageConflicts := 0
	accountDeletions := 0
	storageDeletions := 0

	for _, dl := range order {
		dl.lock.RLock()
		for hash, data := range dl.accountData {
			if _, exists := mergedAccounts[hash]; exists {
				accountConflicts++
			}
			if data == nil {
				mergedAccounts[hash] = nil
				accountDeletions++
			} else {
				cp := make([]byte, len(data))
				copy(cp, data)
				mergedAccounts[hash] = cp
			}
		}
		for acctHash, slots := range dl.storageData {
			if mergedStorage[acctHash] == nil {
				mergedStorage[acctHash] = make(map[types.Hash][]byte)
			}
			for slotHash, data := range slots {
				if _, exists := mergedStorage[acctHash][slotHash]; exists {
					storageConflicts++
				}
				if data == nil {
					mergedStorage[acctHash][slotHash] = nil
					storageDeletions++
				} else {
					cp := make([]byte, len(data))
					copy(cp, data)
					mergedStorage[acctHash][slotHash] = cp
				}
			}
		}
		dl.lock.RUnlock()
	}

	// Count total storage slots.
	totalStorageSlots := 0
	for _, slots := range mergedStorage {
		totalStorageSlots += len(slots)
	}

	// Root from the newest layer, parent from the oldest.
	newestRoot := layers[len(layers)-1].root
	parent := layerParent(layers[0])

	merged := newDiffLayer(parent, newestRoot, mergedAccounts, mergedStorage)
	memAfter := estimateLayerMemory(merged)

	stats := MergeStats{
		LayersProcessed:    len(layers),
		AccountsMerged:     len(mergedAccounts),
		AccountConflicts:   accountConflicts,
		AccountDeletions:   accountDeletions,
		StorageSlotsMerged: totalStorageSlots,
		StorageConflicts:   storageConflicts,
		StorageDeletions:   storageDeletions,
		MemoryBefore:       memBefore,
		MemoryAfter:        memAfter,
	}

	// Update cumulative stats.
	lm.mu.Lock()
	lm.stats.LayersProcessed += stats.LayersProcessed
	lm.stats.AccountsMerged += stats.AccountsMerged
	lm.stats.AccountConflicts += stats.AccountConflicts
	lm.stats.AccountDeletions += stats.AccountDeletions
	lm.stats.StorageSlotsMerged += stats.StorageSlotsMerged
	lm.stats.StorageConflicts += stats.StorageConflicts
	lm.stats.StorageDeletions += stats.StorageDeletions
	lm.stats.MemoryBefore += stats.MemoryBefore
	lm.stats.MemoryAfter += stats.MemoryAfter
	lm.mu.Unlock()

	return &MergeResult{
		Merged:            merged,
		Stats:             stats,
		SortedAccountKeys: sortedKeys(mergedAccounts),
	}, nil
}

// SeekAccount performs a binary search over sorted account keys to find the
// first key >= target. Returns the index and hash, or -1 if not found.
func SeekAccount(keys []types.Hash, target types.Hash) (int, types.Hash) {
	idx := sort.Search(len(keys), func(i int) bool {
		return !hashLess(keys[i], target) // keys[i] >= target
	})
	if idx >= len(keys) {
		return -1, types.Hash{}
	}
	return idx, keys[idx]
}

// CollectLayerChain walks from the given diff layer up through its parent
// chain, collecting diff layers in order from newest (index 0) to oldest.
// Stops when it hits a non-diff layer (e.g., disk layer) or nil.
func CollectLayerChain(top *diffLayer) []*diffLayer {
	var chain []*diffLayer
	var current snapshot = top
	for current != nil {
		dl, ok := current.(*diffLayer)
		if !ok {
			break
		}
		chain = append(chain, dl)
		dl.lock.RLock()
		current = dl.parent
		dl.lock.RUnlock()
	}
	return chain
}

// ReverseLayerChain reverses the order of a diff layer slice in-place.
// This is useful for converting newest-first to oldest-first ordering.
func ReverseLayerChain(layers []*diffLayer) {
	for i, j := 0, len(layers)-1; i < j; i, j = i+1, j-1 {
		layers[i], layers[j] = layers[j], layers[i]
	}
}

// MergedAccountHashes returns the sorted set of unique account hashes from
// multiple diff layers. This is useful for determining which accounts were
// touched without performing a full merge.
func MergedAccountHashes(layers []*diffLayer) []types.Hash {
	set := make(map[types.Hash]struct{})
	for _, dl := range layers {
		dl.lock.RLock()
		for hash := range dl.accountData {
			set[hash] = struct{}{}
		}
		dl.lock.RUnlock()
	}
	hashes := make([]types.Hash, 0, len(set))
	for h := range set {
		hashes = append(hashes, h)
	}
	sort.Slice(hashes, func(i, j int) bool {
		return hashLess(hashes[i], hashes[j])
	})
	return hashes
}

// MergedStorageHashes returns the sorted set of unique (account, slot)
// pairs from multiple diff layers.
func MergedStorageHashes(layers []*diffLayer) map[types.Hash][]types.Hash {
	result := make(map[types.Hash]map[types.Hash]struct{})
	for _, dl := range layers {
		dl.lock.RLock()
		for acctHash, slots := range dl.storageData {
			if result[acctHash] == nil {
				result[acctHash] = make(map[types.Hash]struct{})
			}
			for slotHash := range slots {
				result[acctHash][slotHash] = struct{}{}
			}
		}
		dl.lock.RUnlock()
	}
	out := make(map[types.Hash][]types.Hash, len(result))
	for acctHash, slots := range result {
		hashes := make([]types.Hash, 0, len(slots))
		for h := range slots {
			hashes = append(hashes, h)
		}
		sort.Slice(hashes, func(i, j int) bool {
			return hashLess(hashes[i], hashes[j])
		})
		out[acctHash] = hashes
	}
	return out
}

// --- internal helpers ---

// buildOrder returns layers in processing order based on the policy.
// For PolicyNewestWins, layers are oldest-first (newer overwrites).
// For PolicyOldestWins, layers are newest-first (older overwrites).
func buildOrder(layers []*diffLayer, policy ConflictPolicy) []*diffLayer {
	order := make([]*diffLayer, len(layers))
	copy(order, layers)
	if policy == PolicyOldestWins {
		// Reverse: process newest first, so oldest overwrites.
		for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
			order[i], order[j] = order[j], order[i]
		}
	}
	return order
}

// copyLayerData returns deep copies of the account and storage data maps.
func copyLayerData(dl *diffLayer) (map[types.Hash][]byte, map[types.Hash]map[types.Hash][]byte) {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	accts := make(map[types.Hash][]byte, len(dl.accountData))
	for k, v := range dl.accountData {
		if v == nil {
			accts[k] = nil
		} else {
			cp := make([]byte, len(v))
			copy(cp, v)
			accts[k] = cp
		}
	}

	storage := make(map[types.Hash]map[types.Hash][]byte, len(dl.storageData))
	for acctHash, slots := range dl.storageData {
		slotsCopy := make(map[types.Hash][]byte, len(slots))
		for k, v := range slots {
			if v == nil {
				slotsCopy[k] = nil
			} else {
				cp := make([]byte, len(v))
				copy(cp, v)
				slotsCopy[k] = cp
			}
		}
		storage[acctHash] = slotsCopy
	}
	return accts, storage
}

// layerParent returns the parent snapshot of the given diff layer.
func layerParent(dl *diffLayer) snapshot {
	dl.lock.RLock()
	defer dl.lock.RUnlock()
	return dl.parent
}

// estimateLayerMemory estimates a diff layer's memory footprint.
func estimateLayerMemory(dl *diffLayer) uint64 {
	if dl == nil {
		return 0
	}
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	mem := dl.memory
	const overhead = 50
	mem += uint64(len(dl.accountData)) * overhead
	for _, slots := range dl.storageData {
		mem += uint64(len(slots)) * overhead
	}
	mem += uint64(len(dl.storageData)) * overhead
	return mem
}

// sortedKeys returns the account hashes from a map sorted in ascending order.
func sortedKeys(m map[types.Hash][]byte) []types.Hash {
	keys := make([]types.Hash, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return hashLess(keys[i], keys[j])
	})
	return keys
}
