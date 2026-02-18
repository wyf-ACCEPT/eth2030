package core

import (
	"math/big"
	"sort"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/rlp"
	"github.com/eth2028/eth2028/trie"
)

// TxPoolReader is an interface for reading pending transactions from a pool.
type TxPoolReader interface {
	Pending() []*types.Transaction
}

// BuildBlockAttributes holds the payload attributes for building a new block.
type BuildBlockAttributes struct {
	Timestamp    uint64
	FeeRecipient types.Address
	Random       types.Hash
	Withdrawals  []*types.Withdrawal
	BeaconRoot   *types.Hash
	GasLimit     uint64
}

// BlockBuilder constructs new blocks from pending transactions.
type BlockBuilder struct {
	config *ChainConfig
	chain  *Blockchain
	txPool TxPoolReader
	state  state.StateDB
}

// NewBlockBuilder creates a new block builder.
// If chain is nil, a standalone builder is created (for backward compatibility).
func NewBlockBuilder(config *ChainConfig, chain *Blockchain, pool TxPoolReader) *BlockBuilder {
	return &BlockBuilder{
		config: config,
		chain:  chain,
		txPool: pool,
	}
}

// BuildBlock constructs a new block using payload attributes.
// It selects transactions from the txpool, orders them by effective gas price
// (descending), and applies them until the block gas limit is reached.
// After all transactions are applied, it computes the state root.
func (b *BlockBuilder) BuildBlock(parent *types.Header, attrs *BuildBlockAttributes) (*types.Block, []*types.Receipt, error) {
	// Determine gas limit: use attributes if provided, otherwise derive from parent.
	gasLimit := attrs.GasLimit
	if gasLimit == 0 {
		gasLimit = calcGasLimit(parent.GasLimit, parent.GasUsed)
	}

	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, big.NewInt(1)),
		GasLimit:   gasLimit,
		Time:       attrs.Timestamp,
		Coinbase:   attrs.FeeRecipient,
		Difficulty: new(big.Int), // always 0 post-merge
		MixDigest:  attrs.Random,
		BaseFee:    CalcBaseFee(parent),
		UncleHash:  EmptyUncleHash,
	}

	if attrs.BeaconRoot != nil {
		header.ParentBeaconRoot = attrs.BeaconRoot
	}

	// Get state at parent block.
	statedb := b.state
	if statedb == nil && b.chain != nil {
		parentBlock := b.chain.GetBlock(parent.Hash())
		if parentBlock == nil {
			// Try genesis.
			if parent.Hash() == b.chain.Genesis().Hash() {
				parentBlock = b.chain.Genesis()
			}
		}
		if parentBlock != nil {
			var err error
			statedb, err = b.chain.stateAt(parentBlock)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	if statedb == nil {
		// Fallback: create an empty state (useful for testing).
		statedb = state.NewMemoryStateDB()
	}

	gasPool := new(GasPool).AddGas(header.GasLimit)

	var (
		txs      []*types.Transaction
		receipts []*types.Receipt
		gasUsed  uint64
	)

	// Collect pending transactions from pool.
	var pendingTxs []*types.Transaction
	if b.txPool != nil {
		pendingTxs = b.txPool.Pending()
	}

	// Sort transactions by effective gas price descending.
	sorted := make([]*types.Transaction, len(pendingTxs))
	copy(sorted, pendingTxs)
	sort.Slice(sorted, func(i, j int) bool {
		pi := effectiveGasPrice(sorted[i], header.BaseFee)
		pj := effectiveGasPrice(sorted[j], header.BaseFee)
		return pi.Cmp(pj) > 0
	})

	// Filter out transactions that don't meet the base fee requirement.
	// A tx must have gasFeeCap >= baseFee to be included.

	txIndex := 0
	for _, tx := range sorted {
		// Check if transaction meets base fee requirement.
		if header.BaseFee != nil && tx.GasFeeCap() != nil {
			if tx.GasFeeCap().Cmp(header.BaseFee) < 0 {
				continue
			}
		}

		// Skip if not enough gas left for this tx.
		if gasPool.Gas() < tx.Gas() {
			continue
		}

		// Set tx context so logs are keyed correctly.
		statedb.SetTxContext(tx.Hash(), txIndex)

		// Try to apply the transaction.
		snap := statedb.Snapshot()
		receipt, used, err := ApplyTransaction(b.config, statedb, header, tx, gasPool)
		if err != nil {
			// Transaction failed: revert and skip it.
			statedb.RevertToSnapshot(snap)
			continue
		}

		txs = append(txs, tx)
		receipts = append(receipts, receipt)
		gasUsed += used
		txIndex++
	}

	header.GasUsed = gasUsed

	// Compute block-level bloom filter from all receipts.
	header.Bloom = types.CreateBloom(receipts)

	// Compute transaction and receipt roots.
	header.TxHash = deriveTxsRoot(txs)
	header.ReceiptHash = deriveReceiptsRoot(receipts)

	// Compute state root after applying all transactions.
	header.Root = statedb.GetRoot()

	// Build the block body with withdrawals if provided.
	body := &types.Body{
		Transactions: txs,
		Withdrawals:  attrs.Withdrawals,
	}

	// Process withdrawals: credit each withdrawal recipient.
	if attrs.Withdrawals != nil {
		wHash := deriveWithdrawalsRoot(attrs.Withdrawals)
		header.WithdrawalsHash = &wHash

		for _, w := range attrs.Withdrawals {
			// Withdrawal amount is in Gwei; convert to wei.
			amount := new(big.Int).SetUint64(w.Amount)
			amount.Mul(amount, big.NewInt(1_000_000_000)) // Gwei -> wei
			statedb.AddBalance(w.Address, amount)
		}
		// Recompute state root after withdrawals.
		header.Root = statedb.GetRoot()
	}

	block := types.NewBlock(header, body)

	return block, receipts, nil
}

// BuildBlockLegacy constructs a new block using the legacy interface (for backward
// compatibility with existing tests). Transactions are provided directly.
func (b *BlockBuilder) BuildBlockLegacy(parent *types.Header, txsByPrice []*types.Transaction, timestamp uint64, coinbase types.Address, extra []byte) (*types.Block, []*types.Receipt, error) {
	gasLimit := calcGasLimit(parent.GasLimit, parent.GasUsed)

	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, big.NewInt(1)),
		GasLimit:   gasLimit,
		Time:       timestamp,
		Coinbase:   coinbase,
		Difficulty: new(big.Int),
		Extra:      extra,
		BaseFee:    CalcBaseFee(parent),
		UncleHash:  EmptyUncleHash,
	}

	// Use provided state or create empty.
	statedb := b.state
	if statedb == nil {
		statedb = state.NewMemoryStateDB()
	}

	gasPool := new(GasPool).AddGas(header.GasLimit)

	var (
		txs      []*types.Transaction
		receipts []*types.Receipt
		gasUsed  uint64
	)

	// Sort transactions by gas price descending.
	sorted := make([]*types.Transaction, len(txsByPrice))
	copy(sorted, txsByPrice)
	sort.Slice(sorted, func(i, j int) bool {
		pi := effectiveGasPrice(sorted[i], header.BaseFee)
		pj := effectiveGasPrice(sorted[j], header.BaseFee)
		return pi.Cmp(pj) > 0
	})

	snapshot := statedb.Snapshot()

	txIndex := 0
	for _, tx := range sorted {
		// Skip if not enough gas left for this tx.
		if gasPool.Gas() < tx.Gas() {
			continue
		}

		// Set tx context so logs are keyed correctly.
		statedb.SetTxContext(tx.Hash(), txIndex)

		// Try to apply the transaction.
		snap := statedb.Snapshot()
		receipt, used, err := ApplyTransaction(b.config, statedb, header, tx, gasPool)
		if err != nil {
			// Transaction failed: revert and skip it.
			statedb.RevertToSnapshot(snap)
			continue
		}

		txs = append(txs, tx)
		receipts = append(receipts, receipt)
		gasUsed += used
		txIndex++
	}

	header.GasUsed = gasUsed

	// Compute block-level bloom filter from all receipts.
	header.Bloom = types.CreateBloom(receipts)

	// If no transactions were included, revert to the parent state.
	if len(txs) == 0 {
		statedb.RevertToSnapshot(snapshot)
	}

	// Compute transaction and receipt roots.
	header.TxHash = deriveTxsRoot(txs)
	header.ReceiptHash = deriveReceiptsRoot(receipts)

	// Compute state root.
	header.Root = statedb.GetRoot()

	block := types.NewBlock(header, &types.Body{
		Transactions: txs,
	})

	return block, receipts, nil
}

// SetState sets the state database for standalone builder usage (testing).
func (b *BlockBuilder) SetState(statedb state.StateDB) {
	b.state = statedb
}

// effectiveGasPrice returns the effective gas price for a transaction
// considering the base fee (EIP-1559).
func effectiveGasPrice(tx *types.Transaction, baseFee *big.Int) *big.Int {
	if baseFee == nil || tx.GasFeeCap() == nil || tx.GasTipCap() == nil {
		return tx.GasPrice()
	}
	// effectiveGasPrice = min(gasFeeCap, baseFee + gasTipCap)
	effectiveTip := new(big.Int).Add(baseFee, tx.GasTipCap())
	if effectiveTip.Cmp(tx.GasFeeCap()) > 0 {
		return new(big.Int).Set(tx.GasFeeCap())
	}
	return effectiveTip
}

// calcGasLimit calculates the gas limit for the next block.
// Per EIP-1559, the gas limit can change by at most 1/1024 per block.
func calcGasLimit(parentGasLimit, parentGasUsed uint64) uint64 {
	// Target gas usage is 50% of the limit.
	target := parentGasLimit / 2
	delta := parentGasLimit / 1024

	if parentGasUsed > target {
		// Increase gas limit.
		newLimit := parentGasLimit + delta
		if newLimit > parentGasLimit+delta {
			return parentGasLimit + delta
		}
		return newLimit
	} else if parentGasUsed < target {
		// Decrease gas limit (but not below minimum).
		if delta > parentGasLimit {
			return MinGasLimit
		}
		newLimit := parentGasLimit - delta
		if newLimit < MinGasLimit {
			return MinGasLimit
		}
		return newLimit
	}
	return parentGasLimit
}

// deriveTxsRoot computes the transactions root using a Merkle Patricia Trie.
// Key: RLP(index), Value: RLP-encoded transaction.
func deriveTxsRoot(txs []*types.Transaction) types.Hash {
	if len(txs) == 0 {
		return types.EmptyRootHash
	}
	t := trie.New()
	for i, tx := range txs {
		key, _ := rlp.EncodeToBytes(uint64(i))
		val, err := tx.EncodeRLP()
		if err != nil {
			continue
		}
		t.Put(key, val)
	}
	return t.Hash()
}

// deriveReceiptsRoot computes the receipts root using a Merkle Patricia Trie.
// Key: RLP(index), Value: RLP-encoded receipt.
func deriveReceiptsRoot(receipts []*types.Receipt) types.Hash {
	if len(receipts) == 0 {
		return types.EmptyRootHash
	}
	t := trie.New()
	for i, receipt := range receipts {
		key, _ := rlp.EncodeToBytes(uint64(i))
		val, err := receipt.EncodeRLP()
		if err != nil {
			continue
		}
		t.Put(key, val)
	}
	return t.Hash()
}

// deriveWithdrawalsRoot computes the withdrawals root using a Merkle Patricia Trie.
func deriveWithdrawalsRoot(ws []*types.Withdrawal) types.Hash {
	if len(ws) == 0 {
		return types.EmptyRootHash
	}
	t := trie.New()
	for i, w := range ws {
		key, _ := rlp.EncodeToBytes(uint64(i))
		// RLP-encode withdrawal as [index, validatorIndex, address, amount].
		val, _ := rlp.EncodeToBytes([]interface{}{w.Index, w.ValidatorIndex, w.Address, w.Amount})
		t.Put(key, val)
	}
	return t.Hash()
}
