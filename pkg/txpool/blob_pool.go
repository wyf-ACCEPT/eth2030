// blob_pool.go implements a dedicated blob transaction pool (BlobTxPool)
// that manages EIP-4844 type-3 transactions separately from the main pool.
// It enforces blob gas limits and tracks excess blob gas for base fee computation.
package txpool

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// BlobTxPool constants.
const (
	// MaxBlobsPerBlock is the target number of blobs per block (EIP-4844).
	MaxBlobsPerBlock = 6

	// TargetBlobsPerBlock is the target blob count for base fee adjustment.
	TargetBlobsPerBlock = 3

	// BlobTxPoolCapacity is the default maximum number of blob transactions.
	BlobTxPoolCapacity = 512

	// BlobTxPoolPerAccountMax is the default per-account limit for blob txs.
	BlobTxPoolPerAccountMax = 16

	// BlobGasPerBlobUnit is the gas consumed per blob (EIP-4844: 2^17 = 131072).
	BlobGasPerBlobUnit = 131072

	// MaxBlobGasPerBlock is the maximum blob gas allowed per block.
	MaxBlobGasPerBlock = MaxBlobsPerBlock * BlobGasPerBlobUnit

	// TargetBlobGasPerBlock is the target blob gas per block for fee adjustment.
	TargetBlobGasPerBlock = TargetBlobsPerBlock * BlobGasPerBlobUnit

	// MinBlobBaseFee is the minimum blob base fee in wei (1 wei).
	MinBlobBaseFee = 1

	// BlobBaseFeeUpdateFraction is the denominator for the blob base fee
	// exponential update rule (EIP-4844).
	BlobBaseFeeUpdateFraction = 3338477
)

// BlobTxPool errors.
var (
	ErrBlobTxPoolFull       = errors.New("blob tx pool is full")
	ErrBlobTxNotType3       = errors.New("transaction is not a blob transaction (type 3)")
	ErrBlobTxDuplicate      = errors.New("blob transaction already in pool")
	ErrBlobTxNonceLow       = errors.New("blob tx nonce below state nonce")
	ErrBlobTxNoHashes       = errors.New("blob tx has no versioned hashes")
	ErrBlobTxFeeTooLow      = errors.New("blob fee cap below current blob base fee")
	ErrBlobTxGasExceeded    = errors.New("blob gas exceeds per-block maximum")
	ErrBlobTxAccountMax     = errors.New("blob tx per-account limit exceeded")
	ErrBlobTxReplaceTooLow  = errors.New("replacement blob tx gas price too low")
)

// BlobTxPoolConfig configures the BlobTxPool.
type BlobTxPoolConfig struct {
	Capacity      int      // max blob transactions in pool
	PerAccountMax int      // max blob txs per account
	MinBlobFee    *big.Int // minimum blob gas fee to accept
}

// DefaultBlobTxPoolConfig returns sensible default configuration.
func DefaultBlobTxPoolConfig() BlobTxPoolConfig {
	return BlobTxPoolConfig{
		Capacity:      BlobTxPoolCapacity,
		PerAccountMax: BlobTxPoolPerAccountMax,
		MinBlobFee:    big.NewInt(MinBlobBaseFee),
	}
}

// blobTxEntry stores a blob transaction with its sender for quick lookup.
type blobTxEntry struct {
	tx   *types.Transaction
	from types.Address
}

// BlobTxPool manages EIP-4844 type-3 blob transactions separately from
// the main transaction pool. It enforces blob gas limits, per-account
// caps, and tracks excess blob gas for blob base fee computation.
type BlobTxPool struct {
	config       BlobTxPoolConfig
	state        StateReader
	blobBaseFee  *big.Int // current blob base fee
	excessBlobGas uint64  // excess blob gas from the latest block

	mu       sync.RWMutex
	txs      map[types.Hash]*blobTxEntry            // hash -> entry
	byNonce  map[types.Address][]*types.Transaction  // sender -> nonce-sorted txs
}

// NewBlobTxPool creates a new dedicated blob transaction pool.
func NewBlobTxPool(config BlobTxPoolConfig, state StateReader) *BlobTxPool {
	if config.Capacity <= 0 {
		config.Capacity = BlobTxPoolCapacity
	}
	if config.PerAccountMax <= 0 {
		config.PerAccountMax = BlobTxPoolPerAccountMax
	}
	return &BlobTxPool{
		config:      config,
		state:       state,
		blobBaseFee: big.NewInt(MinBlobBaseFee),
		txs:         make(map[types.Hash]*blobTxEntry),
		byNonce:     make(map[types.Address][]*types.Transaction),
	}
}

// Add inserts a blob transaction into the pool. Only type-3 (BlobTx)
// transactions are accepted. Validates blob gas limits, fee caps,
// nonce ordering, and per-account limits.
func (bp *BlobTxPool) Add(tx *types.Transaction) error {
	if tx.Type() != types.BlobTxType {
		return ErrBlobTxNotType3
	}

	hash := tx.Hash()
	blobHashes := tx.BlobHashes()
	if len(blobHashes) == 0 {
		return ErrBlobTxNoHashes
	}

	// Validate blob gas does not exceed per-block maximum.
	blobGas := tx.BlobGas()
	if blobGas > MaxBlobGasPerBlock {
		return ErrBlobTxGasExceeded
	}

	bp.mu.Lock()
	defer bp.mu.Unlock()

	// Check for duplicates.
	if _, exists := bp.txs[hash]; exists {
		return ErrBlobTxDuplicate
	}

	// Validate blob fee cap against current blob base fee.
	if bp.blobBaseFee != nil {
		blobFeeCap := tx.BlobGasFeeCap()
		if blobFeeCap == nil || blobFeeCap.Cmp(bp.blobBaseFee) < 0 {
			return ErrBlobTxFeeTooLow
		}
	}

	// Determine sender.
	from := blobTxSenderOf(tx)

	// Nonce check against state.
	if bp.state != nil {
		stateNonce := bp.state.GetNonce(from)
		if tx.Nonce() < stateNonce {
			return ErrBlobTxNonceLow
		}
	}

	// Check for same-nonce replacement.
	existingTxs := bp.byNonce[from]
	replaced := false
	for i, existing := range existingTxs {
		if existing.Nonce() == tx.Nonce() {
			// Require price bump for replacement.
			if !blobTxHasBump(existing, tx) {
				return ErrBlobTxReplaceTooLow
			}
			// Remove old from lookup.
			delete(bp.txs, existing.Hash())
			existingTxs[i] = tx
			bp.byNonce[from] = existingTxs
			replaced = true
			break
		}
	}

	// Per-account limit (only for new additions).
	if !replaced {
		if len(existingTxs) >= bp.config.PerAccountMax {
			return ErrBlobTxAccountMax
		}
	}

	// Pool capacity with eviction.
	if !replaced && len(bp.txs) >= bp.config.Capacity {
		if !bp.evictCheapestLocked(tx) {
			return ErrBlobTxPoolFull
		}
	}

	// Insert into pool.
	bp.txs[hash] = &blobTxEntry{tx: tx, from: from}

	if !replaced {
		bp.byNonce[from] = append(bp.byNonce[from], tx)
		// Sort by nonce.
		sort.Slice(bp.byNonce[from], func(i, j int) bool {
			return bp.byNonce[from][i].Nonce() < bp.byNonce[from][j].Nonce()
		})
	}

	return nil
}

// Get retrieves a blob transaction by hash.
func (bp *BlobTxPool) Get(hash types.Hash) *types.Transaction {
	bp.mu.RLock()
	defer bp.mu.RUnlock()

	entry, ok := bp.txs[hash]
	if !ok {
		return nil
	}
	return entry.tx
}

// Remove removes a blob transaction from the pool by hash.
func (bp *BlobTxPool) Remove(hash types.Hash) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	entry, ok := bp.txs[hash]
	if !ok {
		return
	}
	delete(bp.txs, hash)

	// Remove from byNonce.
	acctTxs := bp.byNonce[entry.from]
	for i, tx := range acctTxs {
		if tx.Hash() == hash {
			bp.byNonce[entry.from] = append(acctTxs[:i], acctTxs[i+1:]...)
			break
		}
	}
	if len(bp.byNonce[entry.from]) == 0 {
		delete(bp.byNonce, entry.from)
	}
}

// Pending returns all blob transactions as a flat list sorted by
// blob fee cap descending (highest-paying first).
func (bp *BlobTxPool) Pending() []*types.Transaction {
	bp.mu.RLock()
	defer bp.mu.RUnlock()

	result := make([]*types.Transaction, 0, len(bp.txs))
	for _, entry := range bp.txs {
		result = append(result, entry.tx)
	}

	sort.Slice(result, func(i, j int) bool {
		fi := result[i].BlobGasFeeCap()
		fj := result[j].BlobGasFeeCap()
		if fi == nil {
			return false
		}
		if fj == nil {
			return true
		}
		return fi.Cmp(fj) > 0
	})
	return result
}

// Len returns the number of blob transactions in the pool.
func (bp *BlobTxPool) Len() int {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return len(bp.txs)
}

// TotalBlobGas returns the total blob gas across all transactions in the pool.
func (bp *BlobTxPool) TotalBlobGas() uint64 {
	bp.mu.RLock()
	defer bp.mu.RUnlock()

	var total uint64
	for _, entry := range bp.txs {
		total += entry.tx.BlobGas()
	}
	return total
}

// SetExcessBlobGas updates the excess blob gas from the latest block header.
// This is used to compute the current blob base fee.
func (bp *BlobTxPool) SetExcessBlobGas(excessBlobGas uint64) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	bp.excessBlobGas = excessBlobGas
	bp.blobBaseFee = CalcBlobBaseFee(excessBlobGas)

	// Evict transactions whose blob fee cap is below the new base fee.
	var toRemove []types.Hash
	for hash, entry := range bp.txs {
		blobFeeCap := entry.tx.BlobGasFeeCap()
		if blobFeeCap != nil && blobFeeCap.Cmp(bp.blobBaseFee) < 0 {
			toRemove = append(toRemove, hash)
		}
	}
	for _, hash := range toRemove {
		bp.removeLocked(hash)
	}
}

// ExcessBlobGas returns the current tracked excess blob gas.
func (bp *BlobTxPool) ExcessBlobGas() uint64 {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.excessBlobGas
}

// BlobBaseFee returns the current blob base fee.
func (bp *BlobTxPool) BlobBaseFee() *big.Int {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	if bp.blobBaseFee == nil {
		return big.NewInt(MinBlobBaseFee)
	}
	return new(big.Int).Set(bp.blobBaseFee)
}

// CalcBlobBaseFee computes the blob base fee from excess blob gas using the
// EIP-4844 exponential formula: baseFee = MIN_BLOB_BASE_FEE * e^(excessBlobGas / BLOB_BASE_FEE_UPDATE_FRACTION).
// This is a simplified integer approximation.
func CalcBlobBaseFee(excessBlobGas uint64) *big.Int {
	if excessBlobGas == 0 {
		return big.NewInt(MinBlobBaseFee)
	}
	// Use the fake exponential: fakeExponential(1, excessBlobGas, BlobBaseFeeUpdateFraction)
	// This computes: factor * numerator / denominator using a Taylor series approximation.
	return fakeExponential(big.NewInt(MinBlobBaseFee), new(big.Int).SetUint64(excessBlobGas), big.NewInt(BlobBaseFeeUpdateFraction))
}

// fakeExponential computes factor * e^(numerator/denominator) using an integer
// Taylor series approximation as specified in EIP-4844.
// result = factor * sum(numerator^i / (denominator^i * i!)) for i=0,1,2,...
func fakeExponential(factor, numerator, denominator *big.Int) *big.Int {
	output := new(big.Int)
	accum := new(big.Int).Mul(factor, denominator)

	i := 1
	for accum.Sign() > 0 {
		output.Add(output, accum)
		accum.Mul(accum, numerator)
		accum.Div(accum, denominator)
		accum.Div(accum, big.NewInt(int64(i)))
		i++
		// Safety bound to prevent infinite loops.
		if i > 100 {
			break
		}
	}
	return output.Div(output, denominator)
}

// CalcExcessBlobGas computes the new excess blob gas for the next block
// given the parent's excess and the actual blob gas used.
func CalcExcessBlobGas(parentExcess, parentBlobGasUsed uint64) uint64 {
	total := parentExcess + parentBlobGasUsed
	if total < TargetBlobGasPerBlock {
		return 0
	}
	return total - TargetBlobGasPerBlock
}

// removeLocked removes a blob tx. Caller must hold bp.mu.
func (bp *BlobTxPool) removeLocked(hash types.Hash) {
	entry, ok := bp.txs[hash]
	if !ok {
		return
	}
	delete(bp.txs, hash)

	acctTxs := bp.byNonce[entry.from]
	for i, tx := range acctTxs {
		if tx.Hash() == hash {
			bp.byNonce[entry.from] = append(acctTxs[:i], acctTxs[i+1:]...)
			break
		}
	}
	if len(bp.byNonce[entry.from]) == 0 {
		delete(bp.byNonce, entry.from)
	}
}

// evictCheapestLocked removes the lowest-fee blob tx to make room for newTx.
// Returns true if eviction occurred. Caller must hold bp.mu.
func (bp *BlobTxPool) evictCheapestLocked(newTx *types.Transaction) bool {
	newFeeCap := newTx.BlobGasFeeCap()
	if newFeeCap == nil {
		return false
	}

	var cheapestHash types.Hash
	var cheapestFee *big.Int

	for hash, entry := range bp.txs {
		fee := entry.tx.BlobGasFeeCap()
		if fee == nil {
			continue
		}
		if cheapestFee == nil || fee.Cmp(cheapestFee) < 0 {
			cheapestFee = fee
			cheapestHash = hash
		}
	}

	if cheapestFee == nil || newFeeCap.Cmp(cheapestFee) <= 0 {
		return false
	}

	bp.removeLocked(cheapestHash)
	return true
}

// blobTxSenderOf extracts the sender address from a blob transaction.
func blobTxSenderOf(tx *types.Transaction) types.Address {
	if from := tx.Sender(); from != nil {
		return *from
	}
	return types.Address{}
}

// blobTxHasBump checks if newTx has >= 10% higher blob fee cap than oldTx.
func blobTxHasBump(oldTx, newTx *types.Transaction) bool {
	oldFee := oldTx.BlobGasFeeCap()
	newFee := newTx.BlobGasFeeCap()
	if oldFee == nil || newFee == nil {
		return newFee != nil
	}
	// Require 10% bump.
	threshold := new(big.Int).Mul(oldFee, big.NewInt(110))
	threshold.Div(threshold, big.NewInt(100))
	return newFee.Cmp(threshold) >= 0
}
