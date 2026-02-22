package core

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Block executor errors.
var (
	ErrNilHeader       = errors.New("block executor: nil header")
	ErrNilTransaction  = errors.New("block executor: nil transaction")
	ErrGasExceeded     = errors.New("block executor: gas exceeds block limit")
	ErrNoTransactions  = errors.New("block executor: no transactions")
	ErrExecutionFailed = errors.New("block executor: execution failed")
	ErrGasMismatch     = errors.New("block executor: gas used mismatch")
	ErrTxCountMismatch = errors.New("block executor: tx count mismatch")
	ErrRootMismatch    = errors.New("block executor: state root mismatch")
	ErrReceiptMismatch = errors.New("block executor: receipts root mismatch")
)

// ExecutorConfig configures the block executor.
type ExecutorConfig struct {
	// ParallelTxs enables parallel transaction execution.
	ParallelTxs bool
	// MaxGasPerBlock is the maximum gas allowed per block (0 = no limit).
	MaxGasPerBlock uint64
	// TraceExecution enables execution tracing for debugging.
	TraceExecution bool
}

// DefaultExecutorConfig returns sensible defaults for the executor.
func DefaultExecutorConfig() ExecutorConfig {
	return ExecutorConfig{
		ParallelTxs:    false,
		MaxGasPerBlock: 30_000_000,
		TraceExecution: false,
	}
}

// BlockExecutionResult holds the outcome of executing a block.
type BlockExecutionResult struct {
	StateRoot    types.Hash
	ReceiptsRoot types.Hash
	LogsBloom    []byte
	GasUsed      uint64
	TxCount      int
	Success      bool
}

// ExecutorStats tracks cumulative execution statistics.
type ExecutorStats struct {
	BlocksExecuted uint64
	TxsProcessed   uint64
	TotalGasUsed   uint64
}

// BlockExecutor processes blocks and generates state transitions.
// It is safe for concurrent use.
type BlockExecutor struct {
	mu     sync.RWMutex
	config ExecutorConfig
	stats  ExecutorStats
}

// NewBlockExecutor creates a new block executor with the given config.
func NewBlockExecutor(config ExecutorConfig) *BlockExecutor {
	return &BlockExecutor{
		config: config,
	}
}

// Execute processes a block's transactions against the given header and
// produces an ExecutionResult containing the resulting state root,
// receipts root, logs bloom, and gas usage. Each transaction consumes
// its declared gas; if cumulative gas exceeds the header gas limit (or
// the configured MaxGasPerBlock), execution stops with an error.
func (be *BlockExecutor) Execute(header *types.Header, txs []*types.Transaction) (*BlockExecutionResult, error) {
	if header == nil {
		return nil, ErrNilHeader
	}

	gasLimit := header.GasLimit
	if be.config.MaxGasPerBlock > 0 && be.config.MaxGasPerBlock < gasLimit {
		gasLimit = be.config.MaxGasPerBlock
	}

	var (
		cumulativeGas uint64
		allLogs       []*types.Log
		receipts      []*types.Receipt
	)

	for i, tx := range txs {
		if tx == nil {
			return nil, fmt.Errorf("%w: index %d", ErrNilTransaction, i)
		}

		txGas := tx.Gas()
		if cumulativeGas+txGas > gasLimit {
			return nil, fmt.Errorf("%w: cumulative %d + tx %d > limit %d",
				ErrGasExceeded, cumulativeGas, txGas, gasLimit)
		}
		cumulativeGas += txGas

		// Build a receipt for this transaction.
		receipt := &types.Receipt{
			Type:              tx.Type(),
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: cumulativeGas,
			GasUsed:           txGas,
		}

		// Generate a synthetic log for tracing if enabled.
		if be.config.TraceExecution {
			logEntry := &types.Log{
				Address:     header.Coinbase,
				Topics:      []types.Hash{tx.Hash()},
				Data:        tx.Data(),
				BlockNumber: headerNumber(header),
				TxIndex:     uint(i),
			}
			receipt.Logs = []*types.Log{logEntry}
			allLogs = append(allLogs, logEntry)
		}

		// Compute per-receipt bloom from logs.
		receipt.Bloom = types.LogsBloom(receipt.Logs)

		receipts = append(receipts, receipt)
	}

	// Compute the combined logs bloom.
	combinedBloom := types.CreateBloom(receipts)

	// Compute state root: hash of header + gas used.
	stateRoot := computeStateRoot(header, cumulativeGas)

	// Compute receipts root by hashing receipt data.
	receiptsRoot := computeSimpleReceiptsRoot(receipts)

	result := &BlockExecutionResult{
		StateRoot:    stateRoot,
		ReceiptsRoot: receiptsRoot,
		LogsBloom:    combinedBloom[:],
		GasUsed:      cumulativeGas,
		TxCount:      len(txs),
		Success:      true,
	}

	// Update stats atomically.
	be.mu.Lock()
	be.stats.BlocksExecuted++
	be.stats.TxsProcessed += uint64(len(txs))
	be.stats.TotalGasUsed += cumulativeGas
	be.mu.Unlock()

	return result, nil
}

// ValidateExecution checks that an execution result matches the expected
// header fields. It verifies gas used, state root, and receipts root.
func (be *BlockExecutor) ValidateExecution(result *BlockExecutionResult, header *types.Header) error {
	if result == nil {
		return fmt.Errorf("%w: nil result", ErrExecutionFailed)
	}
	if header == nil {
		return ErrNilHeader
	}
	if !result.Success {
		return ErrExecutionFailed
	}

	// Verify gas used matches header.
	if result.GasUsed != header.GasUsed {
		return fmt.Errorf("%w: result %d != header %d",
			ErrGasMismatch, result.GasUsed, header.GasUsed)
	}

	// Verify state root matches header.
	if result.StateRoot != header.Root {
		return fmt.Errorf("%w: result %s != header %s",
			ErrRootMismatch, result.StateRoot.Hex(), header.Root.Hex())
	}

	// Verify receipts root matches header.
	if result.ReceiptsRoot != header.ReceiptHash {
		return fmt.Errorf("%w: result %s != header %s",
			ErrReceiptMismatch, result.ReceiptsRoot.Hex(), header.ReceiptHash.Hex())
	}

	return nil
}

// EstimateGas returns the gas that a transaction will consume.
// For simple transfers (no data, has recipient), the intrinsic gas is
// 21000. For contract creation (no recipient), it is 53000. Additional
// gas is added for calldata bytes: 4 gas per zero byte, 16 per nonzero.
func (be *BlockExecutor) EstimateGas(tx *types.Transaction) (uint64, error) {
	if tx == nil {
		return 0, ErrNilTransaction
	}

	// Base intrinsic gas.
	var gas uint64
	if tx.To() == nil {
		gas = 53000 // contract creation
	} else {
		gas = 21000 // simple transfer
	}

	// Calldata cost: 4 per zero byte, 16 per nonzero byte.
	for _, b := range tx.Data() {
		if b == 0 {
			gas += 4
		} else {
			gas += 16
		}
	}

	// Access list cost: 2400 per address + 1900 per storage key.
	for _, tuple := range tx.AccessList() {
		gas += 2400
		gas += uint64(len(tuple.StorageKeys)) * 1900
	}

	// Cap at the transaction's declared gas limit.
	if txGas := tx.Gas(); txGas > 0 && gas > txGas {
		gas = txGas
	}

	return gas, nil
}

// ExecutionStats returns a snapshot of the cumulative execution statistics.
func (be *BlockExecutor) ExecutionStats() ExecutorStats {
	be.mu.RLock()
	defer be.mu.RUnlock()
	return be.stats
}

// Reset clears all accumulated execution statistics.
func (be *BlockExecutor) Reset() {
	be.mu.Lock()
	defer be.mu.Unlock()
	be.stats = ExecutorStats{}
}

// headerNumber safely extracts the block number from a header.
func headerNumber(h *types.Header) uint64 {
	if h.Number == nil {
		return 0
	}
	return h.Number.Uint64()
}

// computeStateRoot produces a deterministic state root from the header
// fields and gas used. This is a simplified computation; a real
// implementation would commit the full state trie.
func computeStateRoot(header *types.Header, gasUsed uint64) types.Hash {
	var buf []byte
	buf = append(buf, header.ParentHash[:]...)
	buf = append(buf, header.Root[:]...)
	buf = append(buf, new(big.Int).SetUint64(gasUsed).Bytes()...)
	if header.Number != nil {
		buf = append(buf, header.Number.Bytes()...)
	}
	return crypto.Keccak256Hash(buf)
}

// computeSimpleReceiptsRoot produces a receipts root by hashing the
// concatenation of each receipt's status, cumulative gas, and gas used.
func computeSimpleReceiptsRoot(receipts []*types.Receipt) types.Hash {
	if len(receipts) == 0 {
		return types.EmptyRootHash
	}
	var buf []byte
	for _, r := range receipts {
		buf = append(buf, byte(r.Status))
		buf = append(buf, new(big.Int).SetUint64(r.CumulativeGasUsed).Bytes()...)
		buf = append(buf, new(big.Int).SetUint64(r.GasUsed).Bytes()...)
	}
	return crypto.Keccak256Hash(buf)
}
