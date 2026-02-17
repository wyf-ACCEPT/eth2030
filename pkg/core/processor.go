package core

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/core/vm"
)

const (
	// TxGas is the base gas cost of a transaction (21000).
	TxGas uint64 = 21000
)

var (
	ErrNonceTooLow         = errors.New("nonce too low")
	ErrNonceTooHigh        = errors.New("nonce too high")
	ErrInsufficientBalance = errors.New("insufficient balance for transfer")
	ErrContractCreation    = errors.New("evm execution not implemented")
	ErrContractCall        = errors.New("evm execution not implemented")
)

// StateProcessor processes blocks by applying transactions sequentially.
type StateProcessor struct {
	config *ChainConfig
}

// NewStateProcessor creates a new state processor.
func NewStateProcessor(config *ChainConfig) *StateProcessor {
	return &StateProcessor{config: config}
}

// Process executes all transactions in a block sequentially and returns the receipts.
func (p *StateProcessor) Process(block *types.Block, statedb state.StateDB) ([]*types.Receipt, error) {
	var (
		receipts []*types.Receipt
		gasPool  = new(GasPool).AddGas(block.GasLimit())
		header   = block.Header()
	)

	for i, tx := range block.Transactions() {
		receipt, _, err := ApplyTransaction(p.config, statedb, header, tx, gasPool)
		if err != nil {
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx, err)
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

// ApplyTransaction applies a single transaction to the state and returns a receipt.
func ApplyTransaction(config *ChainConfig, statedb state.StateDB, header *types.Header, tx *types.Transaction, gp *GasPool) (*types.Receipt, uint64, error) {
	msg := TransactionToMessage(tx)

	// For this simplified processor, the From address is derived from the
	// transaction's first access list entry or uses a zero address.
	// In a full implementation, this would come from signature recovery.
	// The caller must set msg.From before calling this in production.

	snapshot := statedb.Snapshot()

	result, err := applyMessage(config, statedb, header, &msg, gp)
	if err != nil {
		statedb.RevertToSnapshot(snapshot)
		return nil, 0, err
	}

	// Create receipt
	var receiptStatus uint64
	if result.Failed() {
		receiptStatus = types.ReceiptStatusFailed
	} else {
		receiptStatus = types.ReceiptStatusSuccessful
	}

	receipt := types.NewReceipt(receiptStatus, header.GasUsed+result.UsedGas)
	receipt.GasUsed = result.UsedGas

	if msg.To == nil {
		// Contract creation: not supported yet, but set the field for completeness.
		receipt.ContractAddress = types.Address{}
	}

	return receipt, result.UsedGas, nil
}

// applyMessage executes a transaction message against the state.
func applyMessage(config *ChainConfig, statedb state.StateDB, header *types.Header, msg *Message, gp *GasPool) (*ExecutionResult, error) {
	// Validate and consume gas from the pool
	if err := gp.SubGas(msg.GasLimit); err != nil {
		return nil, err
	}

	// Validate nonce
	stateNonce := statedb.GetNonce(msg.From)
	if msg.Nonce < stateNonce {
		gp.AddGas(msg.GasLimit)
		return nil, fmt.Errorf("%w: address %v, tx nonce: %d, state nonce: %d", ErrNonceTooLow, msg.From, msg.Nonce, stateNonce)
	}
	if msg.Nonce > stateNonce {
		gp.AddGas(msg.GasLimit)
		return nil, fmt.Errorf("%w: address %v, tx nonce: %d, state nonce: %d", ErrNonceTooHigh, msg.From, msg.Nonce, stateNonce)
	}

	// Calculate gas cost upfront: gasLimit * gasPrice
	gasPrice := msg.GasPrice
	if gasPrice == nil {
		gasPrice = new(big.Int)
	}
	gasCost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(msg.GasLimit))

	// Check total cost: value + gasCost
	totalCost := new(big.Int).Add(msg.Value, gasCost)
	balance := statedb.GetBalance(msg.From)
	if balance.Cmp(totalCost) < 0 {
		gp.AddGas(msg.GasLimit)
		return nil, fmt.Errorf("%w: address %v have %v want %v", ErrInsufficientBalance, msg.From, balance, totalCost)
	}

	// Deduct gas cost from sender
	statedb.SubBalance(msg.From, gasCost)

	// Increment nonce
	statedb.SetNonce(msg.From, msg.Nonce+1)

	var (
		gasUsed    = TxGas // base transaction gas
		execErr    error
		returnData []byte
	)

	if msg.To == nil {
		// Contract creation
		execErr = ErrContractCreation
	} else if len(msg.Data) > 0 && statedb.GetCodeSize(*msg.To) > 0 {
		// Contract call with code at destination
		execErr = ErrContractCall
	} else {
		// Simple value transfer
		if msg.Value.Sign() > 0 {
			statedb.SubBalance(msg.From, msg.Value)
			statedb.AddBalance(*msg.To, msg.Value)
		}

		// Create EVM context (even though we don't execute, this validates the structure)
		_ = vm.NewEVM(
			vm.BlockContext{
				BlockNumber: header.Number,
				Time:        header.Time,
				Coinbase:    header.Coinbase,
				GasLimit:    header.GasLimit,
				BaseFee:     header.BaseFee,
			},
			vm.TxContext{
				Origin:   msg.From,
				GasPrice: gasPrice,
			},
			vm.Config{},
		)
	}

	// Cap gas used to gas limit
	if gasUsed > msg.GasLimit {
		gasUsed = msg.GasLimit
	}

	// Refund unused gas
	remainingGas := msg.GasLimit - gasUsed
	if remainingGas > 0 {
		refund := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(remainingGas))
		statedb.AddBalance(msg.From, refund)
	}

	// Return unused gas to the pool
	gp.AddGas(remainingGas)

	return &ExecutionResult{
		UsedGas:    gasUsed,
		Err:        execErr,
		ReturnData: returnData,
	}, nil
}
