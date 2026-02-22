// compaction_scheduler.go implements a snapshot compaction scheduler that
// analyzes the diff layer stack and plans merges to reduce memory usage.
// It tracks compaction metrics and provides estimation utilities for
// memory consumption of individual diff layers.
package snapshot

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
)

var (
	// ErrNoLayersToCompact is returned when there are no diff layers to merge.
	ErrNoLayersToCompact = errors.New("compaction: no layers to compact")

	// ErrInvalidMergeRange is returned when an empty or nil layer slice is
	// passed to MergeRange.
	ErrInvalidMergeRange = errors.New("compaction: invalid merge range")
)

const (
	// defaultMaxLayers is the default maximum number of diff layers before
	// compaction is triggered.
	defaultMaxLayers = 128

	// defaultMaxMemory is the default maximum aggregate memory (in bytes)
	// of diff layers before compaction is triggered.
	defaultMaxMemory = 256 * 1024 * 1024 // 256 MB
)

// CompactionConfig holds configuration for the CompactionScheduler.
type CompactionConfig struct {
	// MaxLayers is the maximum number of diff layers above the disk layer
	// before compaction is triggered.
	MaxLayers int

	// MaxMemory is the maximum aggregate memory usage (bytes) across all diff
	// layers before compaction is triggered.
	MaxMemory uint64
}

// DefaultCompactionConfig returns a default compaction configuration.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		MaxLayers: defaultMaxLayers,
		MaxMemory: defaultMaxMemory,
	}
}

// CompactionPlan describes a planned merge of diff layers.
type CompactionPlan struct {
	// SourceLayers lists the diff layer roots to be merged, ordered from
	// bottom (oldest) to top (newest).
	SourceLayers []types.Hash

	// TargetRoot is the root of the resulting merged layer.
	TargetRoot types.Hash

	// EstimatedSavings is the estimated memory savings in bytes from the merge.
	EstimatedSavings uint64

	// LayerCount is the number of layers to be merged.
	LayerCount int

	// TotalMemory is the aggregate memory of all source layers.
	TotalMemory uint64
}

// CompactionMetrics tracks cumulative compaction statistics.
type CompactionMetrics struct {
	// CompactionsPerformed is the total number of compaction operations.
	CompactionsPerformed atomic.Uint64

	// LayersMerged is the total number of diff layers merged.
	LayersMerged atomic.Uint64

	// BytesSaved is the total estimated bytes saved by compaction.
	BytesSaved atomic.Uint64
}

// Snapshot returns a point-in-time copy of the metrics.
func (m *CompactionMetrics) Snapshot() CompactionMetricsSnapshot {
	return CompactionMetricsSnapshot{
		CompactionsPerformed: m.CompactionsPerformed.Load(),
		LayersMerged:         m.LayersMerged.Load(),
		BytesSaved:           m.BytesSaved.Load(),
	}
}

// CompactionMetricsSnapshot is a point-in-time copy of CompactionMetrics.
type CompactionMetricsSnapshot struct {
	CompactionsPerformed uint64
	LayersMerged         uint64
	BytesSaved           uint64
}

// CompactionScheduler evaluates the snapshot diff layer tree and plans
// compaction operations to merge layers and reduce memory usage.
type CompactionScheduler struct {
	mu      sync.RWMutex
	config  CompactionConfig
	metrics CompactionMetrics
}

// NewCompactionScheduler creates a new compaction scheduler with the given
// configuration. If cfg is nil, defaults are used.
func NewCompactionScheduler(cfg *CompactionConfig) *CompactionScheduler {
	c := DefaultCompactionConfig()
	if cfg != nil {
		if cfg.MaxLayers > 0 {
			c.MaxLayers = cfg.MaxLayers
		}
		if cfg.MaxMemory > 0 {
			c.MaxMemory = cfg.MaxMemory
		}
	}
	return &CompactionScheduler{
		config: c,
	}
}

// ShouldCompact checks whether the given snapshot tree needs compaction based
// on the configured thresholds. It examines the diff layer chain from the
// given root down to the disk layer.
func (cs *CompactionScheduler) ShouldCompact(tree *Tree) bool {
	if tree == nil {
		return false
	}
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	layers := cs.collectDiffLayers(tree)
	if len(layers) == 0 {
		return false
	}

	// Check layer count threshold.
	if len(layers) > cs.config.MaxLayers {
		return true
	}

	// Check aggregate memory threshold.
	var totalMemory uint64
	for _, dl := range layers {
		totalMemory += EstimateMemory(dl)
	}
	return totalMemory > cs.config.MaxMemory
}

// PlanCompaction creates a compaction plan for the given snapshot tree.
// The plan describes which layers should be merged and the expected savings.
// Returns a zero-value plan if compaction is not needed.
func (cs *CompactionScheduler) PlanCompaction(tree *Tree) CompactionPlan {
	if tree == nil {
		return CompactionPlan{}
	}
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	layers := cs.collectDiffLayers(tree)
	if len(layers) == 0 {
		return CompactionPlan{}
	}

	// Determine how many layers to merge. We keep at most MaxLayers/2
	// to allow headroom for new layers before the next compaction.
	targetKeep := cs.config.MaxLayers / 2
	if targetKeep < 1 {
		targetKeep = 1
	}

	toMerge := len(layers) - targetKeep
	if toMerge < 2 {
		// Need at least 2 layers to merge.
		// Check memory-based compaction as a fallback.
		var totalMem uint64
		for _, dl := range layers {
			totalMem += EstimateMemory(dl)
		}
		if totalMem <= cs.config.MaxMemory {
			return CompactionPlan{}
		}
		// Memory triggered: merge bottom half.
		toMerge = len(layers) / 2
		if toMerge < 2 {
			toMerge = len(layers)
		}
	}
	if toMerge > len(layers) {
		toMerge = len(layers)
	}

	// Bottom layers are at the end of the slice (oldest first).
	// Merge the bottom `toMerge` layers.
	mergeLayers := layers[len(layers)-toMerge:]

	var plan CompactionPlan
	plan.LayerCount = len(mergeLayers)

	var totalMem uint64
	for _, dl := range mergeLayers {
		plan.SourceLayers = append(plan.SourceLayers, dl.root)
		totalMem += EstimateMemory(dl)
	}
	plan.TotalMemory = totalMem

	// The target root is the root of the newest layer in the merge set.
	plan.TargetRoot = mergeLayers[0].root

	// Estimate savings as a fraction of total memory (overlapping keys deduplicate).
	// Conservative estimate: ~30% savings from deduplication.
	if plan.LayerCount > 1 {
		plan.EstimatedSavings = totalMem * 30 / 100
	}

	return plan
}

// EstimateMemory estimates the memory usage of a diff layer in bytes.
// It accounts for account data, storage data, hash keys, and Go overhead.
func EstimateMemory(layer *diffLayer) uint64 {
	if layer == nil {
		return 0
	}
	layer.lock.RLock()
	defer layer.lock.RUnlock()

	// The layer already tracks a basic memory estimate.
	mem := layer.memory

	// Add overhead for map entries (Go map overhead ~50 bytes per entry).
	const mapEntryOverhead = 50
	mem += uint64(len(layer.accountData)) * mapEntryOverhead
	for _, slots := range layer.storageData {
		mem += uint64(len(slots)) * mapEntryOverhead
	}
	// Account for the storage outer map entries.
	mem += uint64(len(layer.storageData)) * mapEntryOverhead

	return mem
}

// MergeRange merges a range of diff layers into a single combined diff layer.
// The layers must be provided from newest to oldest (top to bottom). The
// resulting layer will have the root of the first (newest) layer.
func (cs *CompactionScheduler) MergeRange(layers []*diffLayer) (*diffLayer, error) {
	if len(layers) == 0 {
		return nil, ErrInvalidMergeRange
	}
	if len(layers) == 1 {
		return layers[0], nil
	}

	// Merge account data. Newer layers (lower index) take precedence.
	mergedAccounts := make(map[types.Hash][]byte)
	mergedStorage := make(map[types.Hash]map[types.Hash][]byte)

	// Walk from oldest to newest so newer values overwrite.
	for i := len(layers) - 1; i >= 0; i-- {
		dl := layers[i]
		dl.lock.RLock()
		for hash, data := range dl.accountData {
			if data == nil {
				// Deletion: store nil to mark account deleted.
				mergedAccounts[hash] = nil
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
				if data == nil {
					mergedStorage[acctHash][slotHash] = nil
				} else {
					cp := make([]byte, len(data))
					copy(cp, data)
					mergedStorage[acctHash][slotHash] = cp
				}
			}
		}
		dl.lock.RUnlock()
	}

	// The merged layer's parent is the parent of the oldest layer.
	oldestLayer := layers[len(layers)-1]
	oldestLayer.lock.RLock()
	parent := oldestLayer.parent
	oldestLayer.lock.RUnlock()

	// The root is from the newest layer.
	newestRoot := layers[0].root

	merged := newDiffLayer(parent, newestRoot, mergedAccounts, mergedStorage)

	// Update metrics.
	var memBefore uint64
	for _, dl := range layers {
		memBefore += EstimateMemory(dl)
	}
	memAfter := EstimateMemory(merged)
	var saved uint64
	if memBefore > memAfter {
		saved = memBefore - memAfter
	}

	cs.metrics.CompactionsPerformed.Add(1)
	cs.metrics.LayersMerged.Add(uint64(len(layers)))
	cs.metrics.BytesSaved.Add(saved)

	return merged, nil
}

// Metrics returns the cumulative compaction metrics.
func (cs *CompactionScheduler) Metrics() CompactionMetricsSnapshot {
	return cs.metrics.Snapshot()
}

// Config returns the current compaction configuration.
func (cs *CompactionScheduler) Config() CompactionConfig {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.config
}

// collectDiffLayers gathers all diff layers from the tree. It walks each
// snapshot chain from root to disk, collecting unique diff layers. The result
// is ordered from newest to oldest.
func (cs *CompactionScheduler) collectDiffLayers(tree *Tree) []*diffLayer {
	tree.lock.RLock()
	defer tree.lock.RUnlock()

	// Find the longest chain.
	var longestChain []*diffLayer
	for _, snap := range tree.layers {
		chain := cs.walkChain(snap)
		if len(chain) > len(longestChain) {
			longestChain = chain
		}
	}
	return longestChain
}

// walkChain walks from a snapshot down to the disk layer, collecting diff
// layers in order from newest to oldest.
func (cs *CompactionScheduler) walkChain(snap snapshot) []*diffLayer {
	var chain []*diffLayer
	current := snap
	for current != nil {
		if dl, ok := current.(*diffLayer); ok {
			chain = append(chain, dl)
			current = dl.Parent()
		} else {
			break // Hit disk layer.
		}
	}
	return chain
}
