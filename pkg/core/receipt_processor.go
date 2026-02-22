package core

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Receipt processor errors.
var (
	ErrNilReceipt          = errors.New("receipt processor: nil receipt")
	ErrMaxReceiptsExceeded = errors.New("receipt processor: max receipts exceeded")
)

// ReceiptProcessorConfig configures the receipt processor.
type ReceiptProcessorConfig struct {
	// MaxReceipts is the maximum total receipts stored (0 = unlimited).
	MaxReceipts int
	// CacheReceipts enables caching of receipts for faster retrieval.
	CacheReceipts bool
	// ComputeBloom enables automatic bloom filter computation on add.
	ComputeBloom bool
}

// DefaultReceiptProcessorConfig returns sensible defaults.
func DefaultReceiptProcessorConfig() ReceiptProcessorConfig {
	return ReceiptProcessorConfig{
		MaxReceipts:   0,
		CacheReceipts: true,
		ComputeBloom:  true,
	}
}

// receiptKey uniquely identifies a receipt by block number and tx index.
type receiptKey struct {
	blockNum uint64
	txIndex  uint64
}

// ReceiptProcessor manages receipts and provides Merkle root computation.
// It indexes receipts by block number and transaction index.
// It is safe for concurrent use.
type ReceiptProcessor struct {
	mu       sync.RWMutex
	config   ReceiptProcessorConfig
	receipts map[receiptKey]*types.Receipt
	// blockIndex maps block numbers to sets of tx indices for fast lookup.
	blockIndex map[uint64]map[uint64]struct{}
	// total count of receipts.
	total int
	// highest block number with receipts.
	latestBlock uint64
}

// NewReceiptProcessor creates a new receipt processor with the given config.
func NewReceiptProcessor(config ReceiptProcessorConfig) *ReceiptProcessor {
	return &ReceiptProcessor{
		config:     config,
		receipts:   make(map[receiptKey]*types.Receipt),
		blockIndex: make(map[uint64]map[uint64]struct{}),
	}
}

// AddReceipt stores a receipt for the given block number and transaction index.
// If a receipt already exists at that position, it is replaced.
func (rp *ReceiptProcessor) AddReceipt(blockNum uint64, txIndex uint64, receipt *types.Receipt) error {
	if receipt == nil {
		return ErrNilReceipt
	}

	rp.mu.Lock()
	defer rp.mu.Unlock()

	key := receiptKey{blockNum: blockNum, txIndex: txIndex}

	// Check capacity (only for new entries).
	if _, exists := rp.receipts[key]; !exists {
		if rp.config.MaxReceipts > 0 && rp.total >= rp.config.MaxReceipts {
			return fmt.Errorf("%w: limit %d", ErrMaxReceiptsExceeded, rp.config.MaxReceipts)
		}
		rp.total++
	}

	// Compute bloom if configured.
	if rp.config.ComputeBloom && len(receipt.Logs) > 0 {
		receipt.Bloom = types.LogsBloom(receipt.Logs)
	}

	rp.receipts[key] = receipt

	// Update block index.
	if rp.blockIndex[blockNum] == nil {
		rp.blockIndex[blockNum] = make(map[uint64]struct{})
	}
	rp.blockIndex[blockNum][txIndex] = struct{}{}

	// Track latest block number.
	if blockNum > rp.latestBlock {
		rp.latestBlock = blockNum
	}

	return nil
}

// GetReceipt retrieves a receipt by block number and transaction index.
// Returns nil if no receipt exists at that position.
func (rp *ReceiptProcessor) GetReceipt(blockNum uint64, txIndex uint64) *types.Receipt {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	return rp.receipts[receiptKey{blockNum: blockNum, txIndex: txIndex}]
}

// GetBlockReceipts returns all receipts for a block, sorted by tx index.
// Returns nil if no receipts exist for the block.
func (rp *ReceiptProcessor) GetBlockReceipts(blockNum uint64) []*types.Receipt {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	indices, ok := rp.blockIndex[blockNum]
	if !ok || len(indices) == 0 {
		return nil
	}

	// Collect and sort tx indices.
	sorted := make([]uint64, 0, len(indices))
	for idx := range indices {
		sorted = append(sorted, idx)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Build result in tx index order.
	result := make([]*types.Receipt, 0, len(sorted))
	for _, idx := range sorted {
		if r, ok := rp.receipts[receiptKey{blockNum: blockNum, txIndex: idx}]; ok {
			result = append(result, r)
		}
	}
	return result
}

// ComputeReceiptsRoot computes a Merkle root hash for a block's receipts.
// Receipts are ordered by transaction index. If the block has no receipts,
// EmptyRootHash is returned.
func (rp *ReceiptProcessor) ComputeReceiptsRoot(blockNum uint64) types.Hash {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	indices, ok := rp.blockIndex[blockNum]
	if !ok || len(indices) == 0 {
		return types.EmptyRootHash
	}

	// Sort indices for deterministic ordering.
	sorted := make([]uint64, 0, len(indices))
	for idx := range indices {
		sorted = append(sorted, idx)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Hash each receipt's key fields: status + cumulative gas + gas used + bloom.
	var buf []byte
	for _, idx := range sorted {
		r := rp.receipts[receiptKey{blockNum: blockNum, txIndex: idx}]
		if r == nil {
			continue
		}
		buf = append(buf, byte(r.Status))
		buf = appendUint64(buf, r.CumulativeGasUsed)
		buf = appendUint64(buf, r.GasUsed)
		buf = append(buf, r.Bloom[:]...)
	}
	return crypto.Keccak256Hash(buf)
}

// BlockReceiptCount returns the number of receipts stored for a block.
func (rp *ReceiptProcessor) BlockReceiptCount(blockNum uint64) int {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	if indices, ok := rp.blockIndex[blockNum]; ok {
		return len(indices)
	}
	return 0
}

// TotalReceipts returns the total number of receipts stored.
func (rp *ReceiptProcessor) TotalReceipts() int {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	return rp.total
}

// PruneBlock removes all receipts for the given block number.
// Returns the number of receipts removed.
func (rp *ReceiptProcessor) PruneBlock(blockNum uint64) int {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	indices, ok := rp.blockIndex[blockNum]
	if !ok {
		return 0
	}

	count := len(indices)
	for idx := range indices {
		delete(rp.receipts, receiptKey{blockNum: blockNum, txIndex: idx})
	}
	delete(rp.blockIndex, blockNum)
	rp.total -= count

	// Recompute latest block if we pruned it.
	if blockNum == rp.latestBlock {
		rp.recomputeLatest()
	}

	return count
}

// LatestBlock returns the highest block number that has receipts stored.
// Returns 0 if no receipts are stored.
func (rp *ReceiptProcessor) LatestBlock() uint64 {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	return rp.latestBlock
}

// recomputeLatest recalculates the latest block number from the index.
// Must be called with mu held.
func (rp *ReceiptProcessor) recomputeLatest() {
	rp.latestBlock = 0
	for num := range rp.blockIndex {
		if num > rp.latestBlock {
			rp.latestBlock = num
		}
	}
}

// appendUint64 appends a uint64 as 8 big-endian bytes to the buffer.
func appendUint64(buf []byte, v uint64) []byte {
	return append(buf,
		byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v),
	)
}
