// Package das implements PeerDAS (Peer Data Availability Sampling) data
// structures and verification logic per EIP-7594 and the Fulu DAS core spec.
//
// sparse_blobpool.go implements a sparse blob pool for the Glamsterdam era.
// Instead of storing all blobs locally, the pool keeps only a configurable
// fraction of blobs, relying on DAS for availability of the rest. This
// reduces disk and memory usage for nodes that participate in the PeerDAS
// network but do not need to serve the full blob dataset.
package das

import (
	"encoding/binary"
	"sync"
	"time"
)

// SparseBlobPool stores a subset of blobs based on a configurable sparsity
// factor. Only blobs whose hash satisfies (hash_prefix mod sparsity == 0) are
// retained locally. The pool is safe for concurrent use.
type SparseBlobPool struct {
	mu sync.RWMutex

	// sparsity controls the fraction of blobs kept (1/sparsity are stored).
	// A sparsity of 1 means every blob is stored; 4 means ~25% are stored.
	sparsity uint64

	// blobs maps blob hash -> blob data for locally stored blobs.
	blobs map[[32]byte]*storedBlob

	// stats tracks pool operation counters.
	stats PoolStats
}

// storedBlob wraps blob data with metadata for expiry.
type storedBlob struct {
	data []byte
	slot uint64
	// addedAt is the wall clock time the blob was added.
	addedAt time.Time
}

// PoolStats contains counters for pool operations.
type PoolStats struct {
	// Stored is the current number of blobs in the pool.
	Stored uint64
	// TotalAdded is the total number of blobs accepted into the pool.
	TotalAdded uint64
	// Pruned is the total number of blobs removed by pruning.
	Pruned uint64
	// Rejected is the total number of blobs rejected by the sparsity filter.
	Rejected uint64
}

// NewSparseBlobPool creates a new SparseBlobPool with the given sparsity
// factor. A sparsity of 1 stores all blobs; a sparsity of N stores
// approximately 1/N of all blobs. Panics if sparsity is 0.
func NewSparseBlobPool(sparsity uint64) *SparseBlobPool {
	if sparsity == 0 {
		panic("das: sparsity must be >= 1")
	}
	return &SparseBlobPool{
		sparsity: sparsity,
		blobs:    make(map[[32]byte]*storedBlob),
	}
}

// Sparsity returns the configured sparsity factor.
func (p *SparseBlobPool) Sparsity() uint64 {
	return p.sparsity
}

// shouldStore returns true if the blob hash passes the sparsity filter.
// It uses the first 8 bytes of the hash as a uint64 and checks divisibility.
func (p *SparseBlobPool) shouldStore(blobHash [32]byte) bool {
	prefix := binary.BigEndian.Uint64(blobHash[:8])
	return prefix%p.sparsity == 0
}

// AddBlob attempts to add a blob to the pool. The blob is only stored if
// its hash passes the sparsity filter (hash_prefix mod sparsity == 0).
// Returns true if the blob was stored, false if it was rejected.
// The slot parameter indicates the beacon chain slot this blob belongs to,
// used for later pruning.
func (p *SparseBlobPool) AddBlob(blobHash [32]byte, data []byte, slot uint64) bool {
	if !p.shouldStore(blobHash) {
		p.mu.Lock()
		p.stats.Rejected++
		p.mu.Unlock()
		return false
	}

	// Make a defensive copy of the data.
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	p.mu.Lock()
	defer p.mu.Unlock()

	// Avoid double-counting if the blob already exists.
	if _, exists := p.blobs[blobHash]; exists {
		return true
	}

	p.blobs[blobHash] = &storedBlob{
		data:    dataCopy,
		slot:    slot,
		addedAt: time.Now(),
	}
	p.stats.Stored++
	p.stats.TotalAdded++
	return true
}

// HasBlob returns true if the blob with the given hash is stored locally.
func (p *SparseBlobPool) HasBlob(blobHash [32]byte) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.blobs[blobHash]
	return ok
}

// GetBlob returns the blob data for the given hash, or nil and false if
// the blob is not stored locally.
func (p *SparseBlobPool) GetBlob(blobHash [32]byte) ([]byte, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	sb, ok := p.blobs[blobHash]
	if !ok {
		return nil, false
	}
	// Return a copy to prevent mutations.
	result := make([]byte, len(sb.data))
	copy(result, sb.data)
	return result, true
}

// PruneExpired removes all blobs with a slot strictly less than cutoffSlot.
// Returns the number of blobs pruned.
func (p *SparseBlobPool) PruneExpired(cutoffSlot uint64) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	var pruned int
	for hash, sb := range p.blobs {
		if sb.slot < cutoffSlot {
			delete(p.blobs, hash)
			pruned++
		}
	}
	p.stats.Pruned += uint64(pruned)
	p.stats.Stored -= uint64(pruned)
	return pruned
}

// GetPoolStats returns a snapshot of the pool operation counters.
func (p *SparseBlobPool) GetPoolStats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stats
}

// Size returns the number of blobs currently stored in the pool.
func (p *SparseBlobPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.blobs)
}

// BlobHashes returns all blob hashes currently stored in the pool.
// The order is non-deterministic.
func (p *SparseBlobPool) BlobHashes() [][32]byte {
	p.mu.RLock()
	defer p.mu.RUnlock()
	hashes := make([][32]byte, 0, len(p.blobs))
	for h := range p.blobs {
		hashes = append(hashes, h)
	}
	return hashes
}

// BlobSlot returns the slot number for a stored blob, or 0 and false
// if the blob is not found.
func (p *SparseBlobPool) BlobSlot(blobHash [32]byte) (uint64, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	sb, ok := p.blobs[blobHash]
	if !ok {
		return 0, false
	}
	return sb.slot, true
}

// Reset clears the pool and resets all stats counters.
func (p *SparseBlobPool) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blobs = make(map[[32]byte]*storedBlob)
	p.stats = PoolStats{}
}

// ShouldStore is an exported helper that checks whether a given blob hash
// would pass the sparsity filter without actually adding it to the pool.
// Useful for pre-filtering blobs before fetching their full data.
func (p *SparseBlobPool) ShouldStore(blobHash [32]byte) bool {
	return p.shouldStore(blobHash)
}

// MemoryUsage returns an estimate of the total memory used by stored blobs
// in bytes (blob data only, excluding overhead).
func (p *SparseBlobPool) MemoryUsage() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var total uint64
	for _, sb := range p.blobs {
		total += uint64(len(sb.data))
	}
	return total
}
