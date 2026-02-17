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
	// TxDataZeroGas is the gas cost per zero byte of transaction data.
	TxDataZeroGas uint64 = 4
	// TxDataNonZeroGas is the gas cost per non-zero byte of transaction data.
	TxDataNonZeroGas uint64 = 16
	// TxCreateGas is the extra gas for contract creation transactions.
	TxCreateGas uint64 = 32000
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
		statedb.SetTxContext(tx.Hash(), i)
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

	// Collect logs from state and compute bloom filter
	receipt.Logs = statedb.GetLogs(tx.Hash())
	receipt.Bloom = types.LogsBloom(receipt.Logs)

	return receipt, result.UsedGas, nil
}

// intrinsicGas computes the base gas cost of a transaction before EVM execution.
func intrinsicGas(data []byte, isCreate bool) uint64 {
	gas := TxGas
	if isCreate {
		gas += TxCreateGas
	}
	for _, b := range data {
		if b == 0 {
			gas += TxDataZeroGas
		} else {
			gas += TxDataNonZeroGas
		}
	}
	return gas
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

	isCreate := msg.To == nil

	// Increment nonce (for contract creation, EVM.Create handles it)
	if !isCreate {
		statedb.SetNonce(msg.From, msg.Nonce+1)
	}

	// Compute intrinsic gas
	igas := intrinsicGas(msg.Data, isCreate)
	if igas > msg.GasLimit {
		// Intrinsic gas exceeds gas limit — consume all gas
		gp.AddGas(0) // nothing to return
		return &ExecutionResult{
			UsedGas:    msg.GasLimit,
			Err:        fmt.Errorf("intrinsic gas too low: have %d, want %d", msg.GasLimit, igas),
			ReturnData: nil,
		}, nil
	}

	gasLeft := msg.GasLimit - igas

	// Create EVM
	blockCtx := vm.BlockContext{
		BlockNumber: header.Number,
		Time:        header.Time,
		Coinbase:    header.Coinbase,
		GasLimit:    header.GasLimit,
		BaseFee:     header.BaseFee,
		PrevRandao:  header.MixDigest,
	}
	txCtx := vm.TxContext{
		Origin:   msg.From,
		GasPrice: gasPrice,
	}
	evm := vm.NewEVMWithState(blockCtx, txCtx, vm.Config{}, statedb)

	var (
		execErr       error
		returnData    []byte
		gasRemaining  uint64
		contractAddr  types.Address
	)

	if isCreate {
		// Contract creation: run EVM Create
		var ret []byte
		ret, contractAddr, gasRemaining, execErr = evm.Create(msg.From, msg.Data, gasLeft, msg.Value)
		returnData = ret
		_ = contractAddr // used in receipt below
	} else if statedb.GetCodeSize(*msg.To) > 0 {
		// Contract call: run EVM Call
		returnData, gasRemaining, execErr = evm.Call(msg.From, *msg.To, msg.Data, gasLeft, msg.Value)
	} else {
		// Simple value transfer (no code at destination)
		gasRemaining = gasLeft
		if msg.Value.Sign() > 0 {
			statedb.SubBalance(msg.From, msg.Value)
			statedb.AddBalance(*msg.To, msg.Value)
		}
	}

	// Calculate gas used = intrinsic + (gasLeft - gasRemaining)
	gasUsed := igas + (gasLeft - gasRemaining)

	// Apply refund (EIP-3529: max refund = gasUsed / 5)
	refund := statedb.GetRefund()
	maxRefund := gasUsed / 5
	if refund > maxRefund {
		refund = maxRefund
	}
	gasUsed -= refund

	// Refund remaining gas to sender
	remainingGas := msg.GasLimit - gasUsed
	if remainingGas > 0 {
		refundAmount := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(remainingGas))
		statedb.AddBalance(msg.From, refundAmount)
	}

	// Return unused gas to the pool
	gp.AddGas(remainingGas)

	return &ExecutionResult{
		UsedGas:    gasUsed,
		Err:        execErr,
		ReturnData: returnData,
	}, nil
}
