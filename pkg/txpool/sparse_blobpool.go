package txpool

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Sparse blob pool constants.
const (
	// DefaultSparseMaxBlobs is the default capacity for the sparse blob pool.
	DefaultSparseMaxBlobs = 512

	// DefaultSparseMaxPerAccount is the per-account limit for the sparse pool.
	DefaultSparseMaxPerAccount = 32

	// DefaultSparseExpirySlots is how many slots a blob tx remains valid.
	DefaultSparseExpirySlots = 4096

	// VersionedHashPrefix is the EIP-4844 versioned hash version byte.
	VersionedHashPrefix = 0x01
)

// Sparse blob pool errors.
var (
	ErrSparseBlobPoolFull    = errors.New("sparse blob pool is full")
	ErrSparseNotBlobTx       = errors.New("not a blob transaction")
	ErrSparseBlobDuplicate   = errors.New("sparse blob tx already known")
	ErrSparseBlobMissing     = errors.New("blob tx has no versioned hashes")
	ErrSparseBlobExpired     = errors.New("blob tx has expired")
	ErrSparseAccountLimit    = errors.New("sparse blob per-account limit exceeded")
	ErrSparseBlobNotFound    = errors.New("sparse blob tx not found")
	ErrSparseInvalidVersion  = errors.New("invalid versioned hash prefix")
)

// SparseBlobEntry stores metadata for a blob transaction without holding
// the full blob data. Only versioned hashes are kept for reference.
type SparseBlobEntry struct {
	TxHash         types.Hash
	VersionedHashes []types.Hash
	BlobCount      int
	BlobGas        uint64
	BlobFeeCap     *big.Int
	GasFeeCap      *big.Int
	GasTipCap      *big.Int
	From           types.Address
	Nonce          uint64
	InsertSlot     uint64 // slot at which the tx was inserted
	EstimatedSize  uint64 // estimated blob data size (blobCount * FieldElements * 32)
}

// SparseBlobPoolConfig holds configuration for SparseBlobPool.
type SparseBlobPoolConfig struct {
	MaxBlobs       int    // maximum blob transactions in pool
	MaxPerAccount  int    // maximum blob transactions per account
	ExpirySlots    uint64 // number of slots before a tx expires
}

// DefaultSparseBlobPoolConfig returns sensible defaults.
func DefaultSparseBlobPoolConfig() SparseBlobPoolConfig {
	return SparseBlobPoolConfig{
		MaxBlobs:      DefaultSparseMaxBlobs,
		MaxPerAccount: DefaultSparseMaxPerAccount,
		ExpirySlots:   DefaultSparseExpirySlots,
	}
}

// SparseBlobPool manages blob transactions with space-efficient storage.
// Instead of holding full blob data in memory, it tracks only the versioned
// hashes and fee metadata. Full blob data can be fetched from peers or
// disk when needed.
type SparseBlobPool struct {
	config SparseBlobPoolConfig

	mu          sync.RWMutex
	txs         map[types.Hash]*types.Transaction // full transactions (without blob data)
	entries     map[types.Hash]*SparseBlobEntry    // sparse metadata per tx hash
	byAccount   map[types.Address][]types.Hash     // tx hashes grouped by sender
	totalBlobs  int                                // total blob count across all txs
	currentSlot uint64                             // current slot for expiry tracking
}

// NewSparseBlobPool creates a new sparse blob pool with the given config.
func NewSparseBlobPool(config SparseBlobPoolConfig) *SparseBlobPool {
	return &SparseBlobPool{
		config:    config,
		txs:      make(map[types.Hash]*types.Transaction),
		entries:   make(map[types.Hash]*SparseBlobEntry),
		byAccount: make(map[types.Address][]types.Hash),
	}
}

// AddBlobTx adds a blob transaction to the sparse pool. Only the versioned
// hashes and fee metadata are retained; full blob data is not stored.
func (sp *SparseBlobPool) AddBlobTx(tx *types.Transaction) error {
	if tx.Type() != types.BlobTxType {
		return ErrSparseNotBlobTx
	}

	blobHashes := tx.BlobHashes()
	if len(blobHashes) == 0 {
		return ErrSparseBlobMissing
	}

	// Validate versioned hash prefix.
	for _, vh := range blobHashes {
		if vh[0] != VersionedHashPrefix {
			return ErrSparseInvalidVersion
		}
	}

	hash := tx.Hash()

	sp.mu.Lock()
	defer sp.mu.Unlock()

	if _, exists := sp.txs[hash]; exists {
		return ErrSparseBlobDuplicate
	}

	// Determine sender.
	from := sparseSenderOf(tx)

	// Per-account limit check.
	if acctHashes := sp.byAccount[from]; len(acctHashes) >= sp.config.MaxPerAccount {
		return ErrSparseAccountLimit
	}

	// Pool capacity check.
	if len(sp.txs) >= sp.config.MaxBlobs {
		// Try to evict the lowest-fee tx.
		if !sp.evictLowestLocked(tx) {
			return ErrSparseBlobPoolFull
		}
	}

	// Build sparse entry.
	blobFeeCap := tx.BlobGasFeeCap()
	if blobFeeCap == nil {
		blobFeeCap = new(big.Int)
	}
	gasFeeCap := tx.GasFeeCap()
	if gasFeeCap == nil {
		gasFeeCap = new(big.Int)
	}
	gasTipCap := tx.GasTipCap()
	if gasTipCap == nil {
		gasTipCap = new(big.Int)
	}

	blobCount := len(blobHashes)
	entry := &SparseBlobEntry{
		TxHash:          hash,
		VersionedHashes: blobHashes,
		BlobCount:       blobCount,
		BlobGas:         tx.BlobGas(),
		BlobFeeCap:      new(big.Int).Set(blobFeeCap),
		GasFeeCap:       new(big.Int).Set(gasFeeCap),
		GasTipCap:       new(big.Int).Set(gasTipCap),
		From:            from,
		Nonce:           tx.Nonce(),
		InsertSlot:      sp.currentSlot,
		EstimatedSize:   uint64(blobCount) * 128 * 1024, // 128KB per blob estimate
	}

	sp.txs[hash] = tx
	sp.entries[hash] = entry
	sp.byAccount[from] = append(sp.byAccount[from], hash)
	sp.totalBlobs += blobCount

	return nil
}

// GetBlobTx retrieves a blob transaction by hash.
func (sp *SparseBlobPool) GetBlobTx(hash types.Hash) (*types.Transaction, bool) {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	tx, ok := sp.txs[hash]
	return tx, ok
}

// RemoveBlobTx removes a blob transaction from the pool. Returns true if removed.
func (sp *SparseBlobPool) RemoveBlobTx(hash types.Hash) bool {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	return sp.removeLocked(hash)
}

// removeLocked removes a tx. Caller must hold sp.mu.
func (sp *SparseBlobPool) removeLocked(hash types.Hash) bool {
	entry, ok := sp.entries[hash]
	if !ok {
		return false
	}

	delete(sp.txs, hash)
	delete(sp.entries, hash)
	sp.totalBlobs -= entry.BlobCount

	// Remove from account index.
	acctHashes := sp.byAccount[entry.From]
	for i, h := range acctHashes {
		if h == hash {
			sp.byAccount[entry.From] = append(acctHashes[:i], acctHashes[i+1:]...)
			break
		}
	}
	if len(sp.byAccount[entry.From]) == 0 {
		delete(sp.byAccount, entry.From)
	}

	return true
}

// PendingBlobTxs returns all pending blob transactions sorted by effective
// blob fee cap (descending). Higher-fee transactions come first.
func (sp *SparseBlobPool) PendingBlobTxs() []*types.Transaction {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	txs := make([]*types.Transaction, 0, len(sp.txs))
	for _, tx := range sp.txs {
		txs = append(txs, tx)
	}

	sort.Slice(txs, func(i, j int) bool {
		fi := txs[i].BlobGasFeeCap()
		fj := txs[j].BlobGasFeeCap()
		if fi == nil {
			fi = new(big.Int)
		}
		if fj == nil {
			fj = new(big.Int)
		}
		cmp := fi.Cmp(fj)
		if cmp != 0 {
			return cmp > 0
		}
		// Tiebreak by gas tip cap.
		ti := txs[i].GasTipCap()
		tj := txs[j].GasTipCap()
		if ti == nil {
			ti = new(big.Int)
		}
		if tj == nil {
			tj = new(big.Int)
		}
		return ti.Cmp(tj) > 0
	})

	return txs
}

// BlobCount returns the total number of blobs tracked across all transactions.
func (sp *SparseBlobPool) BlobCount() int {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.totalBlobs
}

// TxCount returns the number of transactions in the pool.
func (sp *SparseBlobPool) TxCount() int {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return len(sp.txs)
}

// PruneExpired removes blob transactions whose insert slot is older than
// currentSlot - ExpirySlots.
func (sp *SparseBlobPool) PruneExpired(currentSlot uint64) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	sp.currentSlot = currentSlot

	if currentSlot < sp.config.ExpirySlots {
		return // no transactions can be expired yet
	}
	cutoff := currentSlot - sp.config.ExpirySlots

	var toRemove []types.Hash
	for hash, entry := range sp.entries {
		if entry.InsertSlot < cutoff {
			toRemove = append(toRemove, hash)
		}
	}
	for _, hash := range toRemove {
		sp.removeLocked(hash)
	}
}

// SpaceUsed returns the estimated total bytes used by blob data referenced
// in the pool. Since the sparse pool does not store actual blob data, this
// returns the estimated size based on blob count.
func (sp *SparseBlobPool) SpaceUsed() uint64 {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	var total uint64
	for _, entry := range sp.entries {
		total += entry.EstimatedSize
	}
	return total
}

// GetEntry returns the sparse metadata entry for a tx hash.
func (sp *SparseBlobPool) GetEntry(hash types.Hash) (*SparseBlobEntry, bool) {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	entry, ok := sp.entries[hash]
	return entry, ok
}

// SetCurrentSlot updates the pool's current slot counter.
func (sp *SparseBlobPool) SetCurrentSlot(slot uint64) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.currentSlot = slot
}

// evictLowestLocked evicts the tx with the lowest blob fee cap to make room
// for newTx. Returns true if an eviction occurred. Caller must hold sp.mu.
func (sp *SparseBlobPool) evictLowestLocked(newTx *types.Transaction) bool {
	newFeeCap := newTx.BlobGasFeeCap()
	if newFeeCap == nil {
		newFeeCap = new(big.Int)
	}

	var lowestHash types.Hash
	var lowestFee *big.Int

	for hash, entry := range sp.entries {
		if lowestFee == nil || entry.BlobFeeCap.Cmp(lowestFee) < 0 {
			lowestFee = entry.BlobFeeCap
			lowestHash = hash
		}
	}

	if lowestFee == nil {
		return false
	}
	// Only evict if new tx has a strictly higher fee.
	if newFeeCap.Cmp(lowestFee) <= 0 {
		return false
	}

	sp.removeLocked(lowestHash)
	return true
}

// sparseSenderOf extracts the sender address from a transaction.
func sparseSenderOf(tx *types.Transaction) types.Address {
	if from := tx.Sender(); from != nil {
		return *from
	}
	return types.Address{}
}
