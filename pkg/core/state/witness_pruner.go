// witness_pruner.go implements a witness-aware state pruner that removes state
// entries unreachable from recent execution witnesses. It uses a bloom filter
// for fast reachability checks and a sliding window over block numbers to
// retain only recently-accessed state paths.
package state

import (
	"hash/fnv"
	"math"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

const (
	// defaultRetentionWindow is the default number of blocks to retain
	// state entries for before they become eligible for pruning.
	defaultRetentionWindow = 256

	// defaultBloomSize is the default size of the bloom filter in bits.
	defaultBloomSize = 1 << 20 // ~1M bits

	// defaultBloomHashes is the number of hash functions for the bloom filter.
	defaultBloomHashes = 7
)

// PruneResult contains statistics from a pruning operation.
type PruneResult struct {
	// PrunedCount is the number of state entries removed.
	PrunedCount uint64
	// RetainedCount is the number of state entries kept.
	RetainedCount uint64
	// BytesFreed is the estimated bytes freed by pruning.
	BytesFreed uint64
	// BlocksScanned is the number of block windows evaluated.
	BlocksScanned uint64
}

// WitnessStemDiff captures state diffs at a single Verkle tree stem for the
// pruner. This mirrors the witness.StemStateDiff type to avoid an import
// cycle (witness -> core/vm -> core/state).
type WitnessStemDiff struct {
	// Stem is the 31-byte Verkle tree stem.
	Stem [31]byte
	// Suffixes contains the leaf-level diffs under this stem.
	Suffixes []WitnessSuffixDiff
}

// WitnessSuffixDiff captures an individual leaf-level state diff for the
// pruner.
type WitnessSuffixDiff struct {
	// Suffix is the leaf index within the stem (0-255).
	Suffix byte
}

// ExecutionWitnessData is the minimal witness representation needed by the
// pruner. Callers convert from witness.ExecutionWitness before passing to
// MarkReachable. This avoids importing the witness package directly.
type ExecutionWitnessData struct {
	// State is the set of state diffs organized by Verkle tree stem.
	State []WitnessStemDiff
	// ParentRoot is the state root of the parent block.
	ParentRoot types.Hash
}

// reachabilityEntry tracks when a state path was last marked reachable.
type reachabilityEntry struct {
	lastBlock uint64
	addr      types.Address
	slot      types.Hash
}

// bloomFilter is a simple bloom filter for fast reachability checks.
type bloomFilter struct {
	bits   []uint64
	size   uint64 // number of bits
	hashes uint
}

// newBloomFilter creates a bloom filter with the given size (in bits) and
// number of hash functions.
func newBloomFilter(size uint64, hashes uint) *bloomFilter {
	words := (size + 63) / 64
	return &bloomFilter{
		bits:   make([]uint64, words),
		size:   size,
		hashes: hashes,
	}
}

// add inserts an element into the bloom filter.
func (bf *bloomFilter) add(data []byte) {
	for i := uint(0); i < bf.hashes; i++ {
		idx := bf.hashIndex(data, i)
		bf.bits[idx/64] |= 1 << (idx % 64)
	}
}

// test checks whether an element might be in the bloom filter.
// Returns false if definitely not present; true if possibly present.
func (bf *bloomFilter) test(data []byte) bool {
	for i := uint(0); i < bf.hashes; i++ {
		idx := bf.hashIndex(data, i)
		if bf.bits[idx/64]&(1<<(idx%64)) == 0 {
			return false
		}
	}
	return true
}

// reset clears the bloom filter.
func (bf *bloomFilter) reset() {
	for i := range bf.bits {
		bf.bits[i] = 0
	}
}

// hashIndex computes the bit index for a given hash function seed.
func (bf *bloomFilter) hashIndex(data []byte, seed uint) uint64 {
	h := fnv.New64a()
	// Seed each hash function differently.
	seedByte := byte(seed)
	h.Write([]byte{seedByte})
	h.Write(data)
	return h.Sum64() % bf.size
}

// estimatedFPR returns the estimated false positive rate for n inserted items.
func (bf *bloomFilter) estimatedFPR(n uint64) float64 {
	if n == 0 {
		return 0.0
	}
	m := float64(bf.size)
	k := float64(bf.hashes)
	nf := float64(n)
	return math.Pow(1-math.Exp(-k*nf/m), k)
}

// WitnessPruner removes state entries that are not reachable from recent
// execution witnesses. It maintains a bloom filter per block window for
// fast reachability checks and an explicit entry map for precise tracking.
type WitnessPruner struct {
	mu sync.RWMutex

	// retentionWindow is the number of blocks to keep entries reachable.
	retentionWindow uint64

	// bloomSize is the size of each bloom filter in bits.
	bloomSize uint64

	// bloomHashes is the number of hash functions per bloom filter.
	bloomHashes uint

	// entries tracks explicit reachability per state path (addr+slot key).
	entries map[string]*reachabilityEntry

	// blockBlooms maps block number to its bloom filter for fast checks.
	blockBlooms map[uint64]*bloomFilter

	// currentBloom is the bloom filter for the latest block being marked.
	currentBloom *bloomFilter

	// currentBlock tracks the latest block number with witness marks.
	currentBlock uint64

	// stats tracks cumulative pruning statistics.
	stats PruneResult

	// totalMarked tracks total items marked across all blocks.
	totalMarked uint64
}

// WitnessPrunerConfig holds configuration for the witness pruner.
type WitnessPrunerConfig struct {
	// RetentionWindow is the number of blocks to retain entries for.
	RetentionWindow uint64
	// BloomSize is the bloom filter size in bits.
	BloomSize uint64
	// BloomHashes is the number of hash functions.
	BloomHashes uint
}

// DefaultWitnessPrunerConfig returns a default configuration.
func DefaultWitnessPrunerConfig() WitnessPrunerConfig {
	return WitnessPrunerConfig{
		RetentionWindow: defaultRetentionWindow,
		BloomSize:       defaultBloomSize,
		BloomHashes:     defaultBloomHashes,
	}
}

// NewWitnessPruner creates a new witness-aware state pruner with the given
// configuration. If cfg is nil, defaults are used.
func NewWitnessPruner(cfg *WitnessPrunerConfig) *WitnessPruner {
	c := DefaultWitnessPrunerConfig()
	if cfg != nil {
		if cfg.RetentionWindow > 0 {
			c.RetentionWindow = cfg.RetentionWindow
		}
		if cfg.BloomSize > 0 {
			c.BloomSize = cfg.BloomSize
		}
		if cfg.BloomHashes > 0 {
			c.BloomHashes = cfg.BloomHashes
		}
	}
	return &WitnessPruner{
		retentionWindow: c.RetentionWindow,
		bloomSize:       c.BloomSize,
		bloomHashes:     c.BloomHashes,
		entries:         make(map[string]*reachabilityEntry),
		blockBlooms:     make(map[uint64]*bloomFilter),
	}
}

// makeKey creates a lookup key from an address and storage slot.
func makeKey(addr types.Address, slot types.Hash) string {
	var buf [types.AddressLength + types.HashLength]byte
	copy(buf[:types.AddressLength], addr[:])
	copy(buf[types.AddressLength:], slot[:])
	return string(buf[:])
}

// MarkReachable marks all state paths referenced by the given execution
// witness data as reachable at the specified block number. The
// ExecutionWitnessData struct avoids importing the witness package directly.
func (wp *WitnessPruner) MarkReachable(blockNumber uint64, w *ExecutionWitnessData) {
	if w == nil {
		return
	}

	wp.mu.Lock()
	defer wp.mu.Unlock()

	// Update current block tracking.
	if blockNumber > wp.currentBlock {
		wp.currentBlock = blockNumber
	}

	// Get or create bloom filter for this block.
	bf, ok := wp.blockBlooms[blockNumber]
	if !ok {
		bf = newBloomFilter(wp.bloomSize, wp.bloomHashes)
		wp.blockBlooms[blockNumber] = bf
	}

	// Process each stem state diff in the witness.
	for _, stemDiff := range w.State {
		// Build an address-like key from the stem for marking.
		// In Verkle trees, the stem identifies an account/storage region.
		var addr types.Address
		stemLen := len(stemDiff.Stem)
		if stemLen >= types.AddressLength {
			copy(addr[:], stemDiff.Stem[:types.AddressLength])
		} else {
			copy(addr[:stemLen], stemDiff.Stem[:])
		}

		for _, suffixDiff := range stemDiff.Suffixes {
			// Build a slot hash from stem + suffix.
			var slot types.Hash
			copy(slot[:], stemDiff.Stem[:])
			slot[types.HashLength-1] = suffixDiff.Suffix

			key := makeKey(addr, slot)

			// Update entry map.
			entry, exists := wp.entries[key]
			if !exists {
				entry = &reachabilityEntry{
					addr: addr,
					slot: slot,
				}
				wp.entries[key] = entry
			}
			entry.lastBlock = blockNumber

			// Mark in bloom filter.
			bf.add([]byte(key))
			wp.totalMarked++
		}
	}
}

// MarkReachableAccount explicitly marks an account address and storage slot
// as reachable at the given block number. Useful for direct marking outside
// of witness processing.
func (wp *WitnessPruner) MarkReachableAccount(blockNumber uint64, addr types.Address, slot types.Hash) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if blockNumber > wp.currentBlock {
		wp.currentBlock = blockNumber
	}

	bf, ok := wp.blockBlooms[blockNumber]
	if !ok {
		bf = newBloomFilter(wp.bloomSize, wp.bloomHashes)
		wp.blockBlooms[blockNumber] = bf
	}

	key := makeKey(addr, slot)

	entry, exists := wp.entries[key]
	if !exists {
		entry = &reachabilityEntry{
			addr: addr,
			slot: slot,
		}
		wp.entries[key] = entry
	}
	entry.lastBlock = blockNumber

	bf.add([]byte(key))
	wp.totalMarked++
}

// Prune removes state entries that were last seen before the retention window.
// Entries with lastBlock < (currentBlock - retentionWindow) are pruned.
func (wp *WitnessPruner) Prune(currentBlock uint64) PruneResult {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	var result PruneResult

	if currentBlock > wp.currentBlock {
		wp.currentBlock = currentBlock
	}

	// Calculate the cutoff block.
	var cutoff uint64
	if wp.currentBlock > wp.retentionWindow {
		cutoff = wp.currentBlock - wp.retentionWindow
	}

	// Prune entries older than the cutoff.
	for key, entry := range wp.entries {
		if entry.lastBlock < cutoff {
			result.PrunedCount++
			// Estimate bytes freed: address + slot + overhead.
			result.BytesFreed += uint64(types.AddressLength + types.HashLength + 64)
			delete(wp.entries, key)
		} else {
			result.RetainedCount++
		}
	}

	// Remove old bloom filters.
	for block := range wp.blockBlooms {
		if block < cutoff {
			delete(wp.blockBlooms, block)
			result.BlocksScanned++
		}
	}

	// Update cumulative stats.
	wp.stats.PrunedCount += result.PrunedCount
	wp.stats.RetainedCount = result.RetainedCount
	wp.stats.BytesFreed += result.BytesFreed
	wp.stats.BlocksScanned += result.BlocksScanned

	return result
}

// ReachabilityCheck tests whether a state entry (addr, slot) is currently
// reachable. Uses bloom filter for fast negative answers and falls back to
// the explicit entry map for definitive positive checks.
func (wp *WitnessPruner) ReachabilityCheck(addr types.Address, slot types.Hash) bool {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	key := makeKey(addr, slot)

	// First check the bloom filter for any block in the retention window.
	// If no bloom claims it, definitely not reachable.
	bloomHit := false
	for _, bf := range wp.blockBlooms {
		if bf.test([]byte(key)) {
			bloomHit = true
			break
		}
	}
	if !bloomHit && len(wp.blockBlooms) > 0 {
		return false
	}

	// Bloom filter says maybe; check the explicit entry map.
	_, exists := wp.entries[key]
	return exists
}

// PruneStats returns cumulative pruning statistics.
func (wp *WitnessPruner) PruneStats() PruneResult {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	return PruneResult{
		PrunedCount:   wp.stats.PrunedCount,
		RetainedCount: uint64(len(wp.entries)),
		BytesFreed:    wp.stats.BytesFreed,
		BlocksScanned: wp.stats.BlocksScanned,
	}
}

// RetentionWindow returns the configured retention window in blocks.
func (wp *WitnessPruner) RetentionWindow() uint64 {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	return wp.retentionWindow
}

// EntryCount returns the number of tracked reachability entries.
func (wp *WitnessPruner) EntryCount() int {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	return len(wp.entries)
}

// BloomFilterCount returns the number of active bloom filters.
func (wp *WitnessPruner) BloomFilterCount() int {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	return len(wp.blockBlooms)
}

// EstimatedFPR returns the estimated false positive rate of the bloom filter
// for the given block number, based on the number of items marked. Returns
// 0 if no bloom exists for that block.
func (wp *WitnessPruner) EstimatedFPR(blockNumber uint64) float64 {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	bf, ok := wp.blockBlooms[blockNumber]
	if !ok {
		return 0
	}
	return bf.estimatedFPR(wp.totalMarked)
}

// Reset clears all pruner state, removing all tracked entries and bloom filters.
func (wp *WitnessPruner) Reset() {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	wp.entries = make(map[string]*reachabilityEntry)
	wp.blockBlooms = make(map[uint64]*bloomFilter)
	wp.currentBlock = 0
	wp.totalMarked = 0
	wp.stats = PruneResult{}
}
