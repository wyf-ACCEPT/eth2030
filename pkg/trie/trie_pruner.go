// trie_pruner.go implements a trie pruner for managing state trie garbage
// collection. It tracks live and dead trie nodes using a bloom filter and
// supports batched pruning with configurable retention windows.
package trie

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// PrunerConfig holds configuration for the trie pruner.
type PrunerConfig struct {
	// KeepBlocks is the number of recent blocks whose state must be retained.
	KeepBlocks uint64

	// BatchSize is the maximum number of nodes to prune in a single Prune call.
	BatchSize int

	// MaxPendingNodes is the maximum number of dead nodes to accumulate before
	// a forced prune is recommended. Zero means no limit.
	MaxPendingNodes uint64

	// BloomSize is the number of bits in the bloom filter for live node tracking.
	// Defaults to defaultPrunerBloomSize if zero.
	BloomSize uint64
}

const defaultPrunerBloomSize = 1 << 20 // 1 Mi bits

// PrunerStats reports pruning statistics.
type PrunerStats struct {
	LiveNodes     uint64
	DeadNodes     uint64
	PrunedTotal   uint64
	LastPruneTime int64 // unix timestamp of last prune, 0 if never
}

// TriePruner manages trie node lifecycle for garbage collection. It maintains
// a set of live nodes (via a bloom filter for fast membership tests) and a set
// of dead nodes eligible for pruning. All methods are safe for concurrent use.
type TriePruner struct {
	config PrunerConfig

	mu    sync.RWMutex
	bloom *prunerBloom
	live  map[types.Hash]struct{}
	dead  map[types.Hash]struct{}

	retainBlock uint64 // minimum block number to retain state for

	prunedTotal   atomic.Uint64
	lastPruneTime atomic.Int64
}

// NewTriePruner creates a new trie pruner with the given configuration.
func NewTriePruner(config PrunerConfig) *TriePruner {
	bloomSize := config.BloomSize
	if bloomSize == 0 {
		bloomSize = defaultPrunerBloomSize
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 1024
	}
	return &TriePruner{
		config: config,
		bloom:  newPrunerBloom(bloomSize),
		live:   make(map[types.Hash]struct{}),
		dead:   make(map[types.Hash]struct{}),
	}
}

// MarkLive marks a node hash as live (not prunable). The node is removed from
// the dead set if present and added to both the bloom filter and the live set.
func (p *TriePruner) MarkLive(nodeHash types.Hash) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.dead, nodeHash)
	p.live[nodeHash] = struct{}{}
	p.bloom.add(nodeHash[:])
}

// MarkDead marks a node hash as dead (eligible for pruning). If the node is
// currently in the live set it is removed from there first.
func (p *TriePruner) MarkDead(nodeHash types.Hash) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.live, nodeHash)
	p.dead[nodeHash] = struct{}{}
}

// IsLive returns true if the node hash is known to be live. The check uses
// the exact live set, not the bloom filter, so there are no false positives.
func (p *TriePruner) IsLive(nodeHash types.Hash) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	_, ok := p.live[nodeHash]
	return ok
}

// PrunableNodes returns the list of node hashes currently marked as dead and
// eligible for pruning. The returned slice is a copy.
func (p *TriePruner) PrunableNodes() []types.Hash {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]types.Hash, 0, len(p.dead))
	for h := range p.dead {
		result = append(result, h)
	}
	// Sort for deterministic output.
	sort.Slice(result, func(i, j int) bool {
		return comparHashes(result[i], result[j]) < 0
	})
	return result
}

// Prune executes pruning of dead nodes up to the configured BatchSize. It
// returns the number of nodes pruned. Pruned nodes are permanently removed
// from the dead set.
func (p *TriePruner) Prune() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	count := 0
	limit := p.config.BatchSize
	if limit <= 0 {
		limit = len(p.dead)
	}

	for h := range p.dead {
		if count >= limit {
			break
		}
		delete(p.dead, h)
		count++
	}

	p.prunedTotal.Add(uint64(count))
	if count > 0 {
		p.lastPruneTime.Store(time.Now().Unix())
	}
	return count, nil
}

// Stats returns a snapshot of pruning statistics.
func (p *TriePruner) Stats() PrunerStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return PrunerStats{
		LiveNodes:     uint64(len(p.live)),
		DeadNodes:     uint64(len(p.dead)),
		PrunedTotal:   p.prunedTotal.Load(),
		LastPruneTime: p.lastPruneTime.Load(),
	}
}

// SetRetainBlock sets the minimum block number whose state should be retained.
// Nodes associated with blocks before this number may be considered prunable.
func (p *TriePruner) SetRetainBlock(number uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.retainBlock = number
}

// RetainBlock returns the currently configured retain block number.
func (p *TriePruner) RetainBlock() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.retainBlock
}

// Reset clears all pruning state, resetting the pruner to an empty state.
// Statistics counters are preserved.
func (p *TriePruner) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	bloomSize := p.config.BloomSize
	if bloomSize == 0 {
		bloomSize = defaultPrunerBloomSize
	}
	p.bloom = newPrunerBloom(bloomSize)
	p.live = make(map[types.Hash]struct{})
	p.dead = make(map[types.Hash]struct{})
	p.retainBlock = 0
}

// comparHashes returns -1, 0, or 1 for byte-level hash comparison.
func comparHashes(a, b types.Hash) int {
	for i := 0; i < types.HashLength; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// prunerBloom is a simple bloom filter for tracking live trie node hashes.
type prunerBloom struct {
	bits []byte
	size uint64
}

func newPrunerBloom(size uint64) *prunerBloom {
	if size == 0 {
		size = defaultPrunerBloomSize
	}
	byteSize := (size + 7) / 8
	return &prunerBloom{
		bits: make([]byte, byteSize),
		size: size,
	}
}

func (b *prunerBloom) add(key []byte) {
	h1 := prunerFnv(key, 0)
	h2 := prunerFnv(key, 1)
	h3 := prunerFnv(key, 2)
	b.bits[h1%b.size/8] |= 1 << (h1 % 8)
	b.bits[h2%b.size/8] |= 1 << (h2 % 8)
	b.bits[h3%b.size/8] |= 1 << (h3 % 8)
}

func (b *prunerBloom) contains(key []byte) bool {
	h1 := prunerFnv(key, 0)
	h2 := prunerFnv(key, 1)
	h3 := prunerFnv(key, 2)
	return b.bits[h1%b.size/8]&(1<<(h1%8)) != 0 &&
		b.bits[h2%b.size/8]&(1<<(h2%8)) != 0 &&
		b.bits[h3%b.size/8]&(1<<(h3%8)) != 0
}

// prunerFnv computes FNV-1a with a seed byte for bloom filter hashing.
func prunerFnv(data []byte, seed byte) uint64 {
	var h uint64 = 14695981039346656037
	h ^= uint64(seed)
	h *= 1099511628211
	for _, b := range data {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return h
}
