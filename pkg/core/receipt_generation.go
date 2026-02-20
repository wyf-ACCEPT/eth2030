// receipt_generation.go implements receipt generation from transaction execution
// results. It handles bloom filter creation from logs, cumulative gas tracking,
// status derivation, EIP-4844 blob gas accounting, and receipt trie root
// calculation using a proper Merkle Patricia Trie.
package core

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/rlp"
	"github.com/eth2028/eth2028/trie"
)

// ReceiptGeneratorConfig configures receipt generation behavior.
type ReceiptGeneratorConfig struct {
	// ComputeBlooms controls whether bloom filters are computed during
	// receipt generation. Disabling this can save CPU for light clients.
	ComputeBlooms bool

	// TrackBlobGas enables EIP-4844 blob gas accounting on receipts.
	TrackBlobGas bool

	// TrackCalldataGas enables EIP-7706 calldata gas tracking on receipts.
	TrackCalldataGas bool
}

// DefaultReceiptGeneratorConfig returns a config with all features enabled.
func DefaultReceiptGeneratorConfig() ReceiptGeneratorConfig {
	return ReceiptGeneratorConfig{
		ComputeBlooms:    true,
		TrackBlobGas:     true,
		TrackCalldataGas: true,
	}
}

// ReceiptGenerator produces receipts from transaction execution results.
// It tracks cumulative gas usage across a block and populates all receipt
// fields including bloom filters, status codes, and EIP-4844 blob gas.
type ReceiptGenerator struct {
	config          ReceiptGeneratorConfig
	cumulativeGas   uint64
	cumulativeBlobGas uint64
	cumulativeCalldataGas uint64
	receipts        []*types.Receipt
	blockBloom      types.Bloom
}

// NewReceiptGenerator creates a new generator with the given config.
func NewReceiptGenerator(config ReceiptGeneratorConfig) *ReceiptGenerator {
	return &ReceiptGenerator{
		config: config,
	}
}

// TxExecutionOutcome holds the outcome of a single transaction execution,
// providing all the data needed to construct a receipt.
type TxExecutionOutcome struct {
	// GasUsed is the execution gas consumed by the transaction.
	GasUsed uint64

	// Failed indicates whether the transaction execution reverted.
	Failed bool

	// Logs are the event logs emitted during execution.
	Logs []*types.Log

	// ContractAddress is set for contract-creation transactions.
	ContractAddress types.Address

	// EffectiveGasPrice is the actual gas price paid per unit of gas.
	EffectiveGasPrice *big.Int

	// BlobGasUsed is the blob gas consumed (EIP-4844). Zero for non-blob txs.
	BlobGasUsed uint64

	// BlobGasPrice is the blob base fee at the time of execution (EIP-4844).
	BlobGasPrice *big.Int

	// CalldataGasUsed is the calldata gas consumed (EIP-7706).
	CalldataGasUsed uint64

	// CalldataGasPrice is the calldata gas price (EIP-7706).
	CalldataGasPrice *big.Int

	// TxHash is the hash of the transaction.
	TxHash types.Hash

	// TxType is the transaction envelope type (0=legacy, 2=EIP-1559, 3=blob).
	TxType uint8
}

// GenerateReceipt creates a receipt from the given execution outcome.
// It updates cumulative gas counters and computes the bloom filter.
// The txIndex is the position of the transaction within the block.
func (g *ReceiptGenerator) GenerateReceipt(outcome *TxExecutionOutcome, txIndex uint) *types.Receipt {
	// Update cumulative gas tracking.
	g.cumulativeGas += outcome.GasUsed

	// Derive status from execution result.
	status := types.ReceiptStatusSuccessful
	if outcome.Failed {
		status = types.ReceiptStatusFailed
	}

	receipt := &types.Receipt{
		Type:              outcome.TxType,
		Status:            status,
		CumulativeGasUsed: g.cumulativeGas,
		GasUsed:           outcome.GasUsed,
		TxHash:            outcome.TxHash,
		ContractAddress:   outcome.ContractAddress,
		TransactionIndex:  txIndex,
		EffectiveGasPrice: outcome.EffectiveGasPrice,
	}

	// Attach logs and compute bloom filter.
	if len(outcome.Logs) > 0 {
		receipt.Logs = make([]*types.Log, len(outcome.Logs))
		copy(receipt.Logs, outcome.Logs)
	} else {
		receipt.Logs = make([]*types.Log, 0)
	}

	if g.config.ComputeBlooms && len(receipt.Logs) > 0 {
		receipt.Bloom = types.LogsBloom(receipt.Logs)
		// Accumulate into the block-level bloom.
		g.blockBloom.Or(receipt.Bloom)
	}

	// EIP-4844: blob gas accounting.
	if g.config.TrackBlobGas && outcome.BlobGasUsed > 0 {
		receipt.BlobGasUsed = outcome.BlobGasUsed
		receipt.BlobGasPrice = outcome.BlobGasPrice
		g.cumulativeBlobGas += outcome.BlobGasUsed
	}

	// EIP-7706: calldata gas accounting.
	if g.config.TrackCalldataGas && outcome.CalldataGasUsed > 0 {
		receipt.CalldataGasUsed = outcome.CalldataGasUsed
		receipt.CalldataGasPrice = outcome.CalldataGasPrice
		g.cumulativeCalldataGas += outcome.CalldataGasUsed
	}

	g.receipts = append(g.receipts, receipt)
	return receipt
}

// FinalizeBlock populates block-level context fields on all generated
// receipts and assigns global log indices. Call this after all transactions
// in the block have been processed.
func (g *ReceiptGenerator) FinalizeBlock(blockHash types.Hash, blockNumber uint64) {
	bn := new(big.Int).SetUint64(blockNumber)
	var logIndex uint

	for _, receipt := range g.receipts {
		receipt.BlockHash = blockHash
		receipt.BlockNumber = new(big.Int).Set(bn)

		// Assign monotonically increasing log indices within the block.
		for _, log := range receipt.Logs {
			log.BlockHash = blockHash
			log.BlockNumber = blockNumber
			log.TxHash = receipt.TxHash
			log.TxIndex = receipt.TransactionIndex
			log.Index = logIndex
			logIndex++
		}
	}
}

// Receipts returns all generated receipts in transaction order.
func (g *ReceiptGenerator) Receipts() []*types.Receipt {
	result := make([]*types.Receipt, len(g.receipts))
	copy(result, g.receipts)
	return result
}

// BlockBloom returns the aggregate bloom filter for all receipts in the block.
// This is the OR of every individual receipt bloom.
func (g *ReceiptGenerator) BlockBloom() types.Bloom {
	return g.blockBloom
}

// CumulativeGasUsed returns the total execution gas used across all
// transactions processed so far.
func (g *ReceiptGenerator) CumulativeGasUsed() uint64 {
	return g.cumulativeGas
}

// CumulativeBlobGasUsed returns the total blob gas used across all
// blob transactions processed so far (EIP-4844).
func (g *ReceiptGenerator) CumulativeBlobGasUsed() uint64 {
	return g.cumulativeBlobGas
}

// CumulativeCalldataGasUsed returns the total calldata gas used (EIP-7706).
func (g *ReceiptGenerator) CumulativeCalldataGasUsed() uint64 {
	return g.cumulativeCalldataGas
}

// ReceiptCount returns the number of receipts generated so far.
func (g *ReceiptGenerator) ReceiptCount() int {
	return len(g.receipts)
}

// ComputeReceiptTrieRoot computes the Merkle Patricia Trie root hash for
// the generated receipts. This matches the Ethereum specification where the
// receipt trie is keyed by RLP(txIndex) and values are RLP-encoded receipts.
// Returns EmptyRootHash if no receipts have been generated.
func (g *ReceiptGenerator) ComputeReceiptTrieRoot() types.Hash {
	return ReceiptTrieRoot(g.receipts)
}

// ReceiptTrieRoot computes the receipt trie root hash for a list of receipts
// using a Merkle Patricia Trie. Key = RLP(index), Value = RLP(receipt).
// This is the standard Ethereum receipt root computation.
func ReceiptTrieRoot(receipts []*types.Receipt) types.Hash {
	if len(receipts) == 0 {
		return types.EmptyRootHash
	}
	t := trie.New()
	for i, receipt := range receipts {
		key, err := rlp.EncodeToBytes(uint64(i))
		if err != nil {
			continue
		}
		val, err := receipt.EncodeRLP()
		if err != nil {
			continue
		}
		t.Put(key, val)
	}
	return t.Hash()
}

// ComputeBlockBloomFromReceipts computes the aggregate bloom filter for a
// list of receipts by OR-ing all individual receipt blooms. If any receipt
// has an empty bloom but has logs, the bloom is recomputed from logs.
func ComputeBlockBloomFromReceipts(receipts []*types.Receipt) types.Bloom {
	var bloom types.Bloom
	for _, receipt := range receipts {
		if receipt == nil {
			continue
		}
		receiptBloom := receipt.Bloom
		// If the receipt has logs but an empty bloom, recompute it.
		if len(receipt.Logs) > 0 && receiptBloom == (types.Bloom{}) {
			receiptBloom = types.LogsBloom(receipt.Logs)
		}
		bloom.Or(receiptBloom)
	}
	return bloom
}

// DeriveReceiptStatus returns the receipt status code based on whether
// the execution failed. Post-Byzantium, status 1 = success, 0 = failure.
func DeriveReceiptStatus(failed bool) uint64 {
	if failed {
		return types.ReceiptStatusFailed
	}
	return types.ReceiptStatusSuccessful
}

// CalcBlobGasUsed returns the total blob gas consumed by a transaction
// based on the number of blob versioned hashes. Each blob uses BlobGasPerBlob
// (131072) gas units as specified in EIP-4844.
func CalcBlobGasUsed(numBlobs int) uint64 {
	if numBlobs <= 0 {
		return 0
	}
	return uint64(numBlobs) * BlobGasPerBlob
}

// BlobGasPerBlob is the gas consumed per blob (2^17 = 131072).
const BlobGasPerBlob uint64 = 131072

// CalcEffectiveGasPrice computes the effective gas price for a transaction
// given the block's base fee. For EIP-1559 transactions, this is:
//
//	min(gasTipCap, gasFeeCap - baseFee) + baseFee
//
// For legacy transactions, it returns the gas price directly.
func CalcEffectiveGasPrice(baseFee, gasFeeCap, gasTipCap *big.Int) *big.Int {
	if baseFee == nil || baseFee.Sign() == 0 {
		// Pre-EIP-1559: effective price is gasFeeCap (= gasPrice for legacy).
		if gasFeeCap != nil {
			return new(big.Int).Set(gasFeeCap)
		}
		return new(big.Int)
	}

	// tip = min(gasTipCap, gasFeeCap - baseFee)
	tip := new(big.Int)
	if gasTipCap != nil && gasFeeCap != nil {
		maxTip := new(big.Int).Sub(gasFeeCap, baseFee)
		if maxTip.Sign() < 0 {
			maxTip.SetInt64(0)
		}
		if gasTipCap.Cmp(maxTip) < 0 {
			tip.Set(gasTipCap)
		} else {
			tip.Set(maxTip)
		}
	}

	// effective price = tip + baseFee
	return tip.Add(tip, baseFee)
}
