package engine

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Block builder errors.
var (
	ErrGasLimitExceeded = errors.New("block builder: transaction exceeds remaining gas")
	ErrNilTransaction   = errors.New("block builder: nil transaction")
	ErrZeroGasLimit     = errors.New("block builder: gas limit must be positive")
	ErrBuilderNotReady  = errors.New("block builder: no transactions added")
)

// TxBlockBuilder assembles blocks from pending transactions.
// It tracks gas usage and orders transactions by effective gas price.
type TxBlockBuilder struct {
	mu       sync.Mutex
	pending  []*types.Transaction
	gasUsed  uint64
	gasLimit uint64
}

// NewTxBlockBuilder creates a new block builder.
func NewTxBlockBuilder() *TxBlockBuilder {
	return &TxBlockBuilder{}
}

// AddTransaction validates and adds a transaction to the pending block.
// Returns ErrGasLimitExceeded if the transaction's gas exceeds the remaining
// gas (when a gas limit has been set via SetGasLimit).
func (bb *TxBlockBuilder) AddTransaction(tx *types.Transaction) error {
	if tx == nil {
		return ErrNilTransaction
	}

	bb.mu.Lock()
	defer bb.mu.Unlock()

	txGas := tx.Gas()

	// If gas limit is set, enforce it.
	if bb.gasLimit > 0 {
		remaining := bb.gasLimit - bb.gasUsed
		if txGas > remaining {
			return ErrGasLimitExceeded
		}
	}

	bb.pending = append(bb.pending, tx)
	bb.gasUsed += txGas
	return nil
}

// SetGasLimit sets the block gas limit for the builder. Transactions that
// would exceed this limit are rejected by AddTransaction.
func (bb *TxBlockBuilder) SetGasLimit(limit uint64) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	bb.gasLimit = limit
}

// GasUsed returns the total gas consumed by pending transactions.
func (bb *TxBlockBuilder) GasUsed() uint64 {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	return bb.gasUsed
}

// PendingCount returns the number of pending transactions.
func (bb *TxBlockBuilder) PendingCount() int {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	return len(bb.pending)
}

// BuildBlock assembles a final block from the pending transactions.
// Transactions are ordered by gas price (highest first) before inclusion.
// The parentHash, timestamp, coinbase, and gasLimit are set on the header.
func (bb *TxBlockBuilder) BuildBlock(
	parentHash types.Hash,
	timestamp uint64,
	coinbase types.Address,
	gasLimit uint64,
) (*types.Block, error) {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	if gasLimit == 0 {
		return nil, ErrZeroGasLimit
	}

	// Sort transactions by effective gas price (highest first).
	txs := make([]*types.Transaction, len(bb.pending))
	copy(txs, bb.pending)

	sort.Slice(txs, func(i, j int) bool {
		pi := txEffectivePrice(txs[i])
		pj := txEffectivePrice(txs[j])
		return pi.Cmp(pj) > 0
	})

	// Select transactions that fit within the gas limit.
	var (
		included []*types.Transaction
		usedGas  uint64
	)
	for _, tx := range txs {
		txGas := tx.Gas()
		if usedGas+txGas > gasLimit {
			continue // skip transactions that exceed the limit
		}
		included = append(included, tx)
		usedGas += txGas
	}

	header := &types.Header{
		ParentHash: parentHash,
		Coinbase:   coinbase,
		GasLimit:   gasLimit,
		GasUsed:    usedGas,
		Time:       timestamp,
		Number:     new(big.Int).SetUint64(0), // caller should set properly
		Difficulty: new(big.Int),              // post-merge: always 0
	}

	body := &types.Body{
		Transactions: included,
	}

	return types.NewBlock(header, body), nil
}

// Reset clears the builder's state for the next block.
func (bb *TxBlockBuilder) Reset() {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	bb.pending = nil
	bb.gasUsed = 0
	bb.gasLimit = 0
}

// txEffectivePrice returns the effective gas price for sorting.
// For EIP-1559 transactions, this returns GasFeeCap (the maximum
// the sender is willing to pay). For legacy transactions, GasPrice.
func txEffectivePrice(tx *types.Transaction) *big.Int {
	if tx.GasFeeCap() != nil {
		return tx.GasFeeCap()
	}
	gp := tx.GasPrice()
	if gp != nil {
		return gp
	}
	return new(big.Int)
}
