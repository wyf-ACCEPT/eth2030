package core

import (
	"math/big"
	"sort"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// BlockBuilder constructs new blocks from pending transactions.
type BlockBuilder struct {
	config  *ChainConfig
	statedb state.StateDB
}

// NewBlockBuilder creates a new block builder.
func NewBlockBuilder(config *ChainConfig, statedb state.StateDB) *BlockBuilder {
	return &BlockBuilder{
		config:  config,
		statedb: statedb,
	}
}

// BuildBlock constructs a new block by selecting and applying transactions.
// It selects transactions from the given set, ordered by effective gas price
// (descending), applying them until the block gas limit is reached.
func (b *BlockBuilder) BuildBlock(parent *types.Header, txsByPrice []*types.Transaction, timestamp uint64, coinbase types.Address, extra []byte) (*types.Block, []*types.Receipt, error) {
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, big.NewInt(1)),
		GasLimit:   calcGasLimit(parent.GasLimit, parent.GasUsed),
		Time:       timestamp,
		Coinbase:   coinbase,
		Difficulty: new(big.Int),
		Extra:      extra,
		BaseFee:    calcBaseFee(parent),
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

	snapshot := b.statedb.Snapshot()

	for _, tx := range sorted {
		// Skip if not enough gas left for this tx.
		if gasPool.Gas() < tx.Gas() {
			continue
		}

		// Try to apply the transaction.
		snap := b.statedb.Snapshot()
		receipt, used, err := ApplyTransaction(b.config, b.statedb, header, tx, gasPool)
		if err != nil {
			// Transaction failed: revert and skip it.
			b.statedb.RevertToSnapshot(snap)
			continue
		}

		txs = append(txs, tx)
		receipts = append(receipts, receipt)
		gasUsed += used
	}

	header.GasUsed = gasUsed

	// If no transactions were included, revert to the parent state.
	if len(txs) == 0 {
		b.statedb.RevertToSnapshot(snapshot)
	}

	block := types.NewBlock(header, &types.Body{
		Transactions: txs,
	})

	return block, receipts, nil
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
			return 5000 // absolute minimum
		}
		newLimit := parentGasLimit - delta
		if newLimit < 5000 {
			return 5000
		}
		return newLimit
	}
	return parentGasLimit
}

// calcBaseFee calculates the base fee for the next block per EIP-1559.
func calcBaseFee(parent *types.Header) *big.Int {
	if parent.BaseFee == nil {
		return big.NewInt(1000000000) // 1 gwei default
	}

	parentBaseFee := parent.BaseFee
	target := parent.GasLimit / 2

	if parent.GasUsed == target {
		return new(big.Int).Set(parentBaseFee)
	}

	if parent.GasUsed > target {
		// Increase base fee.
		gasUsedDelta := parent.GasUsed - target
		feeDelta := new(big.Int).Mul(parentBaseFee, new(big.Int).SetUint64(gasUsedDelta))
		feeDelta.Div(feeDelta, new(big.Int).SetUint64(target))
		feeDelta.Div(feeDelta, big.NewInt(8))
		if feeDelta.Sign() == 0 {
			feeDelta = big.NewInt(1)
		}
		return new(big.Int).Add(parentBaseFee, feeDelta)
	}

	// Decrease base fee.
	gasUsedDelta := target - parent.GasUsed
	feeDelta := new(big.Int).Mul(parentBaseFee, new(big.Int).SetUint64(gasUsedDelta))
	feeDelta.Div(feeDelta, new(big.Int).SetUint64(target))
	feeDelta.Div(feeDelta, big.NewInt(8))
	result := new(big.Int).Sub(parentBaseFee, feeDelta)
	if result.Sign() <= 0 {
		return big.NewInt(1)
	}
	return result
}
