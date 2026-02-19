package txpool

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Blob pool constants.
const (
	// DefaultMaxBlobs is the default maximum number of blob transactions in the pool.
	DefaultMaxBlobs = 256

	// DefaultMaxBlobsPerAccount is the maximum blob transactions per account.
	DefaultMaxBlobsPerAccount = 16

	// DefaultMaxBlobSize is the maximum allowed blob sidecar size (128KB per blob).
	DefaultMaxBlobSize = 128 * 1024

	// BlobGasPerBlob is the gas consumed by each blob (2^17 = 131072).
	BlobGasPerBlob = 131072
)

// Blob pool error codes.
var (
	ErrBlobPoolFull         = errors.New("blob pool is full")
	ErrNotBlobTx            = errors.New("not a blob transaction")
	ErrBlobAccountLimit     = errors.New("blob per-account limit exceeded")
	ErrBlobAlreadyKnown     = errors.New("blob transaction already known")
	ErrBlobNonceTooLow      = errors.New("blob tx nonce too low")
	ErrBlobMissingHashes    = errors.New("blob transaction missing versioned hashes")
	ErrBlobFeeCapTooLow     = errors.New("blob fee cap below blob base fee")
	ErrBlobReplaceTooLow    = errors.New("blob replacement gas price too low")
)

// BlobMetadata tracks blob-related metadata without holding full blob data in memory.
type BlobMetadata struct {
	TxHash         types.Hash
	BlobHashes     []types.Hash // versioned hashes of the blobs
	BlobCount      int
	BlobGas        uint64
	BlobFeeCap     *big.Int
	GasFeeCap      *big.Int
	GasTipCap      *big.Int
	From           types.Address
	Nonce          uint64
}

// BlobPoolConfig holds configuration for the BlobPool.
type BlobPoolConfig struct {
	MaxBlobs          int      // Maximum blob transactions in pool
	MaxBlobsPerAccount int     // Maximum blob transactions per account
	MaxBlobSize       int      // Maximum allowed blob sidecar size
	MinBlobGasPrice   *big.Int // Minimum blob gas price to accept
}

// DefaultBlobPoolConfig returns sensible defaults.
func DefaultBlobPoolConfig() BlobPoolConfig {
	return BlobPoolConfig{
		MaxBlobs:           DefaultMaxBlobs,
		MaxBlobsPerAccount: DefaultMaxBlobsPerAccount,
		MaxBlobSize:        DefaultMaxBlobSize,
		MinBlobGasPrice:    big.NewInt(1),
	}
}

// BlobPool implements a memory-efficient blob transaction pool.
// Full blob sidecar data is not kept in memory; only metadata (versioned hashes,
// sizes, fees) is tracked. The pool orders transactions by blob gas price for
// eviction and uses per-account limits to prevent abuse.
type BlobPool struct {
	config      BlobPoolConfig
	state       StateReader
	blobBaseFee *big.Int // current blob base fee

	mu       sync.RWMutex
	pending  map[types.Address]*blobTxList // blob txs by sender, sorted by nonce
	lookup   map[types.Hash]*types.Transaction
	metadata map[types.Hash]*BlobMetadata
}

// NewBlobPool creates a new blob transaction pool.
func NewBlobPool(config BlobPoolConfig, state StateReader) *BlobPool {
	return &BlobPool{
		config:   config,
		state:    state,
		pending:  make(map[types.Address]*blobTxList),
		lookup:   make(map[types.Hash]*types.Transaction),
		metadata: make(map[types.Hash]*BlobMetadata),
	}
}

// blobTxList maintains blob transactions sorted by nonce for a single account.
type blobTxList struct {
	items []*types.Transaction
}

func (l *blobTxList) Len() int { return len(l.items) }

func (l *blobTxList) Add(tx *types.Transaction) (replaced *types.Transaction) {
	idx := sort.Search(len(l.items), func(i int) bool {
		return l.items[i].Nonce() >= tx.Nonce()
	})
	if idx < len(l.items) && l.items[idx].Nonce() == tx.Nonce() {
		old := l.items[idx]
		l.items[idx] = tx
		return old
	}
	l.items = append(l.items, nil)
	copy(l.items[idx+1:], l.items[idx:])
	l.items[idx] = tx
	return nil
}

func (l *blobTxList) Remove(nonce uint64) *types.Transaction {
	for i, tx := range l.items {
		if tx.Nonce() == nonce {
			l.items = append(l.items[:i], l.items[i+1:]...)
			return tx
		}
	}
	return nil
}

func (l *blobTxList) Get(nonce uint64) *types.Transaction {
	idx := sort.Search(len(l.items), func(i int) bool {
		return l.items[i].Nonce() >= nonce
	})
	if idx < len(l.items) && l.items[idx].Nonce() == nonce {
		return l.items[idx]
	}
	return nil
}

// Add adds a blob transaction to the pool. Only type-3 (BlobTx) transactions
// are accepted. Returns an error if validation fails.
func (bp *BlobPool) Add(tx *types.Transaction) error {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if tx.Type() != types.BlobTxType {
		return ErrNotBlobTx
	}

	hash := tx.Hash()
	if _, ok := bp.lookup[hash]; ok {
		return ErrBlobAlreadyKnown
	}

	blobHashes := tx.BlobHashes()
	if len(blobHashes) == 0 {
		return ErrBlobMissingHashes
	}

	// Validate blob fee cap against base fee.
	if bp.blobBaseFee != nil {
		blobFeeCap := tx.BlobGasFeeCap()
		if blobFeeCap == nil || blobFeeCap.Cmp(bp.blobBaseFee) < 0 {
			return ErrBlobFeeCapTooLow
		}
	}

	from := bp.senderOf(tx)

	// Nonce validation.
	if bp.state != nil {
		stateNonce := bp.state.GetNonce(from)
		if tx.Nonce() < stateNonce {
			return ErrBlobNonceTooLow
		}
	}

	// Check for replacement.
	list := bp.pending[from]
	replaced := false
	if list != nil {
		if old := list.Get(tx.Nonce()); old != nil {
			// Replacement: require PriceBump % higher gas price.
			if !bp.hasSufficientBump(old, tx) {
				return ErrBlobReplaceTooLow
			}
			replaced = true
		}
	}

	// Per-account limit check (only for new additions, not replacements).
	if !replaced {
		if list != nil && list.Len() >= bp.config.MaxBlobsPerAccount {
			return ErrBlobAccountLimit
		}
	}

	// Pool size limit with eviction.
	if !replaced && len(bp.lookup) >= bp.config.MaxBlobs {
		if !bp.evictCheapest(tx) {
			return ErrBlobPoolFull
		}
	}

	// Insert into the pool.
	if list == nil {
		list = &blobTxList{}
		bp.pending[from] = list
	}
	oldTx := list.Add(tx)
	if oldTx != nil {
		delete(bp.lookup, oldTx.Hash())
		delete(bp.metadata, oldTx.Hash())
	}

	bp.lookup[hash] = tx
	bp.metadata[hash] = &BlobMetadata{
		TxHash:     hash,
		BlobHashes: blobHashes,
		BlobCount:  len(blobHashes),
		BlobGas:    tx.BlobGas(),
		BlobFeeCap: tx.BlobGasFeeCap(),
		GasFeeCap:  tx.GasFeeCap(),
		GasTipCap:  tx.GasTipCap(),
		From:       from,
		Nonce:      tx.Nonce(),
	}

	return nil
}

// Remove removes a blob transaction from the pool by hash.
func (bp *BlobPool) Remove(hash types.Hash) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	meta, ok := bp.metadata[hash]
	if !ok {
		return
	}

	if list, ok := bp.pending[meta.From]; ok {
		list.Remove(meta.Nonce)
		if list.Len() == 0 {
			delete(bp.pending, meta.From)
		}
	}

	delete(bp.lookup, hash)
	delete(bp.metadata, hash)
}

// Get retrieves a blob transaction by hash.
func (bp *BlobPool) Get(hash types.Hash) *types.Transaction {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.lookup[hash]
}

// Has returns true if the pool contains the blob transaction.
func (bp *BlobPool) Has(hash types.Hash) bool {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	_, ok := bp.lookup[hash]
	return ok
}

// GetMetadata returns blob metadata for a transaction hash.
func (bp *BlobPool) GetMetadata(hash types.Hash) *BlobMetadata {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.metadata[hash]
}

// Count returns the total number of blob transactions in the pool.
func (bp *BlobPool) Count() int {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return len(bp.lookup)
}

// Pending returns all blob transactions grouped by sender.
func (bp *BlobPool) Pending() map[types.Address][]*types.Transaction {
	bp.mu.RLock()
	defer bp.mu.RUnlock()

	result := make(map[types.Address][]*types.Transaction)
	for addr, list := range bp.pending {
		txs := make([]*types.Transaction, len(list.items))
		copy(txs, list.items)
		result[addr] = txs
	}
	return result
}

// PendingSorted returns all blob transactions sorted by effective blob gas price (descending).
func (bp *BlobPool) PendingSorted() []*types.Transaction {
	bp.mu.RLock()
	defer bp.mu.RUnlock()

	var all []*types.Transaction
	for _, list := range bp.pending {
		all = append(all, list.items...)
	}

	sort.Slice(all, func(i, j int) bool {
		pi := blobEffectivePrice(all[i])
		pj := blobEffectivePrice(all[j])
		return pi.Cmp(pj) > 0
	})
	return all
}

// SetBlobBaseFee updates the blob base fee and evicts transactions below it.
func (bp *BlobPool) SetBlobBaseFee(baseFee *big.Int) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	bp.blobBaseFee = new(big.Int).Set(baseFee)

	// Evict transactions whose blob fee cap is below the new base fee.
	var toRemove []types.Hash
	for hash, meta := range bp.metadata {
		if meta.BlobFeeCap != nil && meta.BlobFeeCap.Cmp(baseFee) < 0 {
			toRemove = append(toRemove, hash)
		}
	}
	for _, hash := range toRemove {
		bp.removeLocked(hash)
	}
}

// SetStateReader updates the state reader used for nonce validation.
func (bp *BlobPool) SetStateReader(state StateReader) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.state = state
}

// removeLocked removes a blob transaction. Caller must hold bp.mu.
func (bp *BlobPool) removeLocked(hash types.Hash) {
	meta, ok := bp.metadata[hash]
	if !ok {
		return
	}
	if list, ok := bp.pending[meta.From]; ok {
		list.Remove(meta.Nonce)
		if list.Len() == 0 {
			delete(bp.pending, meta.From)
		}
	}
	delete(bp.lookup, hash)
	delete(bp.metadata, hash)
}

// evictCheapest removes the blob transaction with the lowest effective price
// to make room for newTx. Returns true if an eviction occurred.
func (bp *BlobPool) evictCheapest(newTx *types.Transaction) bool {
	newPrice := blobEffectivePrice(newTx)

	var cheapestHash types.Hash
	var cheapestPrice *big.Int

	for hash := range bp.metadata {
		tx := bp.lookup[hash]
		if tx == nil {
			continue
		}
		price := blobEffectivePrice(tx)
		if cheapestPrice == nil || price.Cmp(cheapestPrice) < 0 {
			cheapestPrice = price
			cheapestHash = hash
		}
	}

	if cheapestPrice == nil {
		return false
	}

	// Only evict if the new tx has a higher price than the cheapest.
	if newPrice.Cmp(cheapestPrice) <= 0 {
		return false
	}

	bp.removeLocked(cheapestHash)
	return true
}

// hasSufficientBump checks if newTx has PriceBump% higher blob gas price than oldTx.
func (bp *BlobPool) hasSufficientBump(oldTx, newTx *types.Transaction) bool {
	oldPrice := blobEffectivePrice(oldTx)
	newPrice := blobEffectivePrice(newTx)

	threshold := new(big.Int).Mul(oldPrice, big.NewInt(100+PriceBump))
	threshold.Div(threshold, big.NewInt(100))
	return newPrice.Cmp(threshold) >= 0
}

// blobEffectivePrice returns the effective price for ordering blob transactions.
// It uses the blob fee cap as the primary ordering key, with gas tip cap as tiebreaker.
func blobEffectivePrice(tx *types.Transaction) *big.Int {
	blobFeeCap := tx.BlobGasFeeCap()
	if blobFeeCap != nil {
		return new(big.Int).Set(blobFeeCap)
	}
	return new(big.Int)
}

// senderOf extracts the sender address from a transaction.
func (bp *BlobPool) senderOf(tx *types.Transaction) types.Address {
	if from := tx.Sender(); from != nil {
		return *from
	}
	return types.Address{}
}
