// state_transition.go implements the Ethereum execution layer state transition
// function. It orchestrates block-level execution: validating transactions,
// applying them against the state, computing gas accounting (EIP-1559 base fee
// burning, EIP-4844 blob gas), and performing post-block validation.
package core

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// State transition errors.
var (
	ErrSTBlobGasExceeded     = errors.New("blob gas limit exceeded")
	ErrSTBlobGasUsedInvalid  = errors.New("blob gas used mismatch")
	ErrSTStateRootMismatch   = errors.New("post-state root mismatch")
	ErrSTReceiptRootMismatch = errors.New("receipt root mismatch")
	ErrSTBloomMismatch       = errors.New("logs bloom mismatch")
	ErrSTGasUsedMismatch     = errors.New("gas used mismatch")
	ErrSTInvalidSender       = errors.New("transaction sender not set")
	ErrSTMaxBlobGas          = errors.New("max blob gas per block exceeded")
)

// stBlobGasPerBlob is the gas cost per blob (EIP-4844).
const stBlobGasPerBlob = 131072

// stMaxBlobGasPerBlock is the max blob gas per block (Cancun: 6 blobs).
const stMaxBlobGasPerBlock = 6 * stBlobGasPerBlob

// StateTransition manages the execution of a block against the world state.
// It validates transactions, executes them sequentially, and applies post-block
// operations (withdrawals, state root validation). All public methods are
// thread-safe.
type StateTransition struct {
	mu     sync.Mutex
	config *ChainConfig
}

// NewStateTransition creates a new StateTransition with the given chain config.
func NewStateTransition(config *ChainConfig) *StateTransition {
	return &StateTransition{config: config}
}

// TransitionResult holds the outputs of a block state transition.
type TransitionResult struct {
	Receipts    []*types.Receipt
	GasUsed     uint64
	BlobGasUsed uint64
	LogsBloom   types.Bloom
	StateRoot   types.Hash
}

// ApplyBlock executes all transactions in the block against the given state
// and returns the collected receipts. It performs full transaction validation,
// gas accounting, EIP-1559 base fee burning, EIP-4844 blob gas tracking,
// withdrawal processing, and post-block validation.
func (st *StateTransition) ApplyBlock(block *types.Block, statedb state.StateDB) (*TransitionResult, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	header := block.Header()
	txs := block.Transactions()

	// Validate base fee is present for post-London blocks.
	if st.config != nil && st.config.IsLondon(header.Number) && header.BaseFee == nil {
		return nil, ErrInvalidBaseFee
	}

	gasPool := new(GasPool).AddGas(header.GasLimit)

	var (
		receipts          []*types.Receipt
		cumulativeGasUsed uint64
		cumulativeBlobGas uint64
		allLogs           []*types.Log
	)

	for i, tx := range txs {
		// Validate transaction before execution.
		if err := ValidateTransaction(tx, statedb, header, st.config); err != nil {
			return nil, fmt.Errorf("tx %d validation failed: %w", i, err)
		}

		statedb.SetTxContext(tx.Hash(), i)

		receipt, usedGas, err := applyTransaction(st.config, nil, statedb, header, tx, gasPool)
		if err != nil {
			return nil, fmt.Errorf("tx %d [%s] execution failed: %w", i, tx.Hash().Hex(), err)
		}

		cumulativeGasUsed += usedGas
		receipt.CumulativeGasUsed = cumulativeGasUsed
		receipt.TransactionIndex = uint(i)
		receipt.BlockHash = block.Hash()
		receipt.BlockNumber = new(big.Int).Set(header.Number)

		// EIP-4844: accumulate blob gas.
		if blobGas := tx.BlobGas(); blobGas > 0 {
			cumulativeBlobGas += blobGas
			if cumulativeBlobGas > stMaxBlobGasPerBlock {
				return nil, fmt.Errorf("%w: cumulative %d exceeds max %d",
					ErrSTMaxBlobGas, cumulativeBlobGas, stMaxBlobGasPerBlock)
			}
		}

		// Set log block context.
		for _, log := range receipt.Logs {
			log.BlockNumber = header.Number.Uint64()
			log.BlockHash = block.Hash()
		}
		allLogs = append(allLogs, receipt.Logs...)

		receipts = append(receipts, receipt)
	}

	// Assign global log indices across all receipts.
	var logIdx uint
	for _, r := range receipts {
		for _, l := range r.Logs {
			l.Index = logIdx
			logIdx++
		}
	}

	// EIP-4895: process beacon chain withdrawals.
	if st.config != nil && st.config.IsShanghai(header.Time) {
		ProcessWithdrawals(statedb, block.Withdrawals())
	}

	// EIP-4844: validate blob gas used matches header.
	if header.BlobGasUsed != nil {
		if *header.BlobGasUsed != cumulativeBlobGas {
			return nil, fmt.Errorf("%w: header %d, computed %d",
				ErrSTBlobGasUsedInvalid, *header.BlobGasUsed, cumulativeBlobGas)
		}
	}

	// Compute combined bloom filter.
	bloom := types.CreateBloom(receipts)

	// Compute state root.
	stateRoot, err := statedb.Commit()
	if err != nil {
		return nil, fmt.Errorf("state commit failed: %w", err)
	}

	return &TransitionResult{
		Receipts:    receipts,
		GasUsed:     cumulativeGasUsed,
		BlobGasUsed: cumulativeBlobGas,
		LogsBloom:   bloom,
		StateRoot:   stateRoot,
	}, nil
}

// ValidateTransaction performs full validation of a transaction against the
// current state and block header. It checks nonce, balance, gas limits,
// intrinsic gas, EIP-1559 fee caps, and EIP-4844 blob constraints.
func ValidateTransaction(tx *types.Transaction, statedb state.StateDB, header *types.Header, config *ChainConfig) error {
	sender := tx.Sender()
	if sender == nil {
		return ErrSTInvalidSender
	}
	from := *sender

	// Nonce validation.
	stateNonce := statedb.GetNonce(from)
	if tx.Nonce() < stateNonce {
		return fmt.Errorf("%w: tx %d, state %d", ErrNonceTooLow, tx.Nonce(), stateNonce)
	}
	if tx.Nonce() > stateNonce {
		return fmt.Errorf("%w: tx %d, state %d", ErrNonceTooHigh, tx.Nonce(), stateNonce)
	}

	// Gas limit validation: tx gas must not exceed block gas limit.
	if tx.Gas() > header.GasLimit {
		return fmt.Errorf("%w: tx gas %d > block limit %d",
			ErrGasLimitExceeded, tx.Gas(), header.GasLimit)
	}

	// Intrinsic gas validation using txIntrinsicGas.
	igas := txIntrinsicGas(tx)
	if tx.Gas() < igas {
		return fmt.Errorf("%w: have %d, want %d",
			ErrIntrinsicGasTooLow, tx.Gas(), igas)
	}

	// EIP-1559 fee cap validation: fee cap must cover base fee.
	if header.BaseFee != nil && header.BaseFee.Sign() > 0 {
		feeCap := tx.GasFeeCap()
		if feeCap != nil && feeCap.Cmp(header.BaseFee) < 0 {
			return fmt.Errorf("max fee per gas (%s) < base fee (%s)",
				feeCap.String(), header.BaseFee.String())
		}
	}

	// Balance validation: sender must have enough for value + max gas cost.
	cost := TxCost(tx, header.BaseFee)
	balance := statedb.GetBalance(from)
	if balance.Cmp(cost) < 0 {
		return fmt.Errorf("%w: have %s, want %s",
			ErrInsufficientBalance, balance.String(), cost.String())
	}

	// EIP-4844: validate blob constraints.
	if tx.Type() == types.BlobTxType {
		blobHashes := tx.BlobHashes()
		if len(blobHashes) == 0 {
			return errors.New("blob tx must have at least one blob")
		}
		if uint64(len(blobHashes))*stBlobGasPerBlob > stMaxBlobGasPerBlock {
			return fmt.Errorf("%w: %d blobs", ErrSTBlobGasExceeded, len(blobHashes))
		}
		// Blob fee cap must cover the blob base fee.
		if header.ExcessBlobGas != nil {
			blobBaseFee := calcBlobBaseFee(*header.ExcessBlobGas)
			blobFeeCap := tx.BlobGasFeeCap()
			if blobFeeCap != nil && blobFeeCap.Cmp(blobBaseFee) < 0 {
				return fmt.Errorf("blob fee cap (%s) < blob base fee (%s)",
					blobFeeCap.String(), blobBaseFee.String())
			}
		}
		// Blob txs must have a recipient (no contract creation).
		if tx.To() == nil {
			return errors.New("blob tx must not be contract creation")
		}
	}

	return nil
}

// txIntrinsicGas computes the base gas cost of a transaction before EVM
// execution, accounting for transaction type, data costs, access list,
// and contract creation overhead.
func txIntrinsicGas(tx *types.Transaction) uint64 {
	isCreate := tx.To() == nil
	gas := TxGas
	if isCreate {
		gas += TxCreateGas
	}
	for _, b := range tx.Data() {
		if b == 0 {
			gas += TxDataZeroGas
		} else {
			gas += TxDataNonZeroGas
		}
	}
	// EIP-2930 access list costs.
	for _, tuple := range tx.AccessList() {
		gas += 2400
		gas += uint64(len(tuple.StorageKeys)) * 1900
	}
	// EIP-7702 authorization list costs.
	if auths := tx.AuthorizationList(); len(auths) > 0 {
		gas += uint64(len(auths)) * PerAuthBaseCost
	}
	return gas
}

// TxCost computes the maximum cost a transaction can incur, including
// value transfer, gas cost at the fee cap, and blob gas cost.
func TxCost(tx *types.Transaction, baseFee *big.Int) *big.Int {
	cost := new(big.Int)
	if tx.Value() != nil {
		cost.Set(tx.Value())
	}
	// Gas cost: gasLimit * gasFeeCap (or gasPrice for legacy).
	gasPrice := tx.GasFeeCap()
	if gasPrice == nil {
		gasPrice = tx.GasPrice()
	}
	if gasPrice == nil {
		gasPrice = new(big.Int)
	}
	gasCost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(tx.Gas()))
	cost.Add(cost, gasCost)

	// EIP-4844: blob gas cost.
	if blobFeeCap := tx.BlobGasFeeCap(); blobFeeCap != nil {
		blobGas := tx.BlobGas()
		blobCost := new(big.Int).Mul(blobFeeCap, new(big.Int).SetUint64(blobGas))
		cost.Add(cost, blobCost)
	}

	return cost
}

// EffectiveGasPrice computes the actual gas price paid per EIP-1559.
// For legacy transactions it returns GasPrice. For EIP-1559 transactions
// it returns min(GasFeeCap, BaseFee + GasTipCap).
func EffectiveGasPrice(tx *types.Transaction, baseFee *big.Int) *big.Int {
	if baseFee == nil || baseFee.Sign() <= 0 {
		p := tx.GasPrice()
		if p == nil {
			return new(big.Int)
		}
		return new(big.Int).Set(p)
	}
	tip := tx.GasTipCap()
	if tip == nil {
		tip = new(big.Int)
	}
	feeCap := tx.GasFeeCap()
	if feeCap == nil {
		return new(big.Int).Set(baseFee)
	}
	effective := new(big.Int).Add(baseFee, tip)
	if effective.Cmp(feeCap) > 0 {
		effective.Set(feeCap)
	}
	return effective
}

// ValidatePostBlock checks that the block header fields match the computed
// values from execution. It verifies state root, gas used, and logs bloom.
func ValidatePostBlock(header *types.Header, result *TransitionResult) error {
	// Gas used validation.
	if header.GasUsed != result.GasUsed {
		return fmt.Errorf("%w: header %d, computed %d",
			ErrSTGasUsedMismatch, header.GasUsed, result.GasUsed)
	}

	// State root validation.
	if header.Root != result.StateRoot {
		return fmt.Errorf("%w: header %s, computed %s",
			ErrSTStateRootMismatch, header.Root.Hex(), result.StateRoot.Hex())
	}

	// Bloom validation.
	if header.Bloom != result.LogsBloom {
		return ErrSTBloomMismatch
	}

	return nil
}

// NextBlockBaseFee computes the EIP-1559 base fee for the next block given
// the parent header. This is a convenience wrapper around CalcBaseFee.
func NextBlockBaseFee(parent *types.Header) *big.Int {
	return CalcBaseFee(parent)
}

// NextExcessBlobGas computes the excess blob gas for the next block based
// on the parent's fields, per EIP-4844.
func NextExcessBlobGas(parentExcessBlobGas, parentBlobGasUsed uint64) uint64 {
	return CalcExcessBlobGas(parentExcessBlobGas, parentBlobGasUsed)
}

// BlockReward computes the static block reward for the given block number.
// Post-merge (PoS) blocks have zero block reward; the validator is
// compensated through the consensus layer.
func BlockReward(config *ChainConfig, header *types.Header) *big.Int {
	if config != nil && config.IsMerge() {
		return new(big.Int) // no block reward post-merge
	}
	// Pre-merge: 2 ETH per block (post-Constantinople).
	reward := new(big.Int).Mul(big.NewInt(2), new(big.Int).SetUint64(1e18))
	return reward
}
