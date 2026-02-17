package core

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/bal"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

var (
	// ErrBALHashMismatch is returned when the BAL hash in the header does not
	// match the computed hash of the provided BlockAccessList.
	ErrBALHashMismatch = errors.New("block access list hash mismatch")
)

// ParallelProcessor processes blocks by executing independent transaction
// groups in parallel, as identified by Block Access Lists (EIP-7928).
type ParallelProcessor struct {
	config *ChainConfig
}

// NewParallelProcessor creates a new parallel processor.
func NewParallelProcessor(config *ChainConfig) *ParallelProcessor {
	return &ParallelProcessor{config: config}
}

// ProcessParallel executes all transactions in a block, using the BAL to
// identify independent transaction groups that can run concurrently.
// If bal is nil, it falls back to sequential execution.
// The returned receipts are always in original transaction order.
func (p *ParallelProcessor) ProcessParallel(statedb state.StateDB, block *types.Block, accessList *bal.BlockAccessList) ([]*types.Receipt, error) {
	txs := block.Transactions()
	if len(txs) == 0 {
		return nil, nil
	}

	// Fall back to sequential execution when no BAL is available.
	if accessList == nil {
		return p.processSequential(statedb, block)
	}

	groups := bal.ComputeParallelSets(accessList)
	if len(groups) == 0 {
		// No parallel groups computed (e.g. BAL has no transaction entries).
		return p.processSequential(statedb, block)
	}

	// Require a *MemoryStateDB for Copy/Merge support.
	memDB, ok := statedb.(*state.MemoryStateDB)
	if !ok {
		return p.processSequential(statedb, block)
	}

	header := block.Header()
	receipts := make([]*types.Receipt, len(txs))

	// Process each execution group. Groups are processed sequentially relative
	// to each other (they may have ordering dependencies across groups), but
	// transactions within a group execute in parallel.
	for _, group := range groups {
		if len(group.TxIndices) == 1 {
			// Single transaction in the group -- execute directly, no goroutine needed.
			idx := group.TxIndices[0]
			if idx >= len(txs) {
				return nil, fmt.Errorf("BAL references tx index %d but block has only %d transactions", idx, len(txs))
			}
			gasPool := new(GasPool).AddGas(block.GasLimit())
			receipt, _, err := applyTransactionAt(p.config, memDB, header, txs[idx], gasPool, idx)
			if err != nil {
				return nil, fmt.Errorf("could not apply tx %d: %w", idx, err)
			}
			receipts[idx] = receipt
			continue
		}

		// Multiple transactions in the group -- execute in parallel.
		type txResult struct {
			idx     int
			receipt *types.Receipt
			stateDB *state.MemoryStateDB
			err     error
		}

		results := make([]txResult, len(group.TxIndices))
		var wg sync.WaitGroup

		for i, txIdx := range group.TxIndices {
			if txIdx >= len(txs) {
				return nil, fmt.Errorf("BAL references tx index %d but block has only %d transactions", txIdx, len(txs))
			}

			wg.Add(1)
			go func(slot int, idx int) {
				defer wg.Done()

				// Each goroutine gets its own copy of state.
				localState := memDB.Copy()
				localGasPool := new(GasPool).AddGas(block.GasLimit())

				receipt, _, err := applyTransactionAt(p.config, localState, header, txs[idx], localGasPool, idx)
				results[slot] = txResult{
					idx:     idx,
					receipt: receipt,
					stateDB: localState,
					err:     err,
				}
			}(i, txIdx)
		}

		wg.Wait()

		// Check for errors and merge state changes back in order.
		for _, res := range results {
			if res.err != nil {
				return nil, fmt.Errorf("could not apply tx %d: %w", res.idx, res.err)
			}
			memDB.Merge(res.stateDB)
			receipts[res.idx] = res.receipt
		}
	}

	return receipts, nil
}

// processSequential falls back to the standard sequential execution path.
func (p *ParallelProcessor) processSequential(statedb state.StateDB, block *types.Block) ([]*types.Receipt, error) {
	var (
		receipts []*types.Receipt
		gasPool  = new(GasPool).AddGas(block.GasLimit())
		header   = block.Header()
		txs      = block.Transactions()
	)

	for i, tx := range txs {
		receipt, _, err := ApplyTransaction(p.config, statedb, header, tx, gasPool)
		if err != nil {
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx, err)
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

// applyTransactionAt applies a transaction and sets the TransactionIndex on the receipt.
func applyTransactionAt(config *ChainConfig, statedb state.StateDB, header *types.Header, tx *types.Transaction, gp *GasPool, txIndex int) (*types.Receipt, uint64, error) {
	receipt, gasUsed, err := ApplyTransaction(config, statedb, header, tx, gp)
	if err != nil {
		return nil, 0, err
	}
	receipt.TransactionIndex = uint(txIndex)
	return receipt, gasUsed, nil
}

// validateBAL checks that the BAL hash stored in the block header matches
// the hash computed from the provided BlockAccessList.
func validateBAL(header *types.Header, accessList *bal.BlockAccessList) error {
	if header.BlockAccessListHash == nil {
		return errors.New("header has no BlockAccessListHash")
	}
	if accessList == nil {
		return errors.New("block access list is nil")
	}
	computed := accessList.Hash()
	if *header.BlockAccessListHash != computed {
		return fmt.Errorf("%w: header=%s computed=%s", ErrBALHashMismatch,
			header.BlockAccessListHash.Hex(), computed.Hex())
	}
	return nil
}
