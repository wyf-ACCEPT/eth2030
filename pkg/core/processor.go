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
	ErrGasLimitExceeded    = errors.New("gas limit exceeded")
	ErrIntrinsicGasTooLow  = errors.New("intrinsic gas too low")
	ErrContractCreation    = errors.New("contract creation failed")
	ErrContractCall        = errors.New("contract call failed")
)

// StateProcessor processes blocks by applying transactions sequentially.
type StateProcessor struct {
	config  *ChainConfig
	getHash vm.GetHashFunc
}

// NewStateProcessor creates a new state processor.
func NewStateProcessor(config *ChainConfig) *StateProcessor {
	return &StateProcessor{config: config}
}

// SetGetHash sets the block hash lookup function for the BLOCKHASH opcode.
func (p *StateProcessor) SetGetHash(fn vm.GetHashFunc) {
	p.getHash = fn
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
		receipt, _, err := applyTransaction(p.config, p.getHash, statedb, header, tx, gasPool)
		if err != nil {
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx, err)
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

// ApplyTransaction applies a single transaction to the state and returns a receipt.
// It is a convenience wrapper that calls applyTransaction with no GetHash function.
func ApplyTransaction(config *ChainConfig, statedb state.StateDB, header *types.Header, tx *types.Transaction, gp *GasPool) (*types.Receipt, uint64, error) {
	return applyTransaction(config, nil, statedb, header, tx, gp)
}

// applyTransaction is the internal implementation that accepts an optional GetHash function.
func applyTransaction(config *ChainConfig, getHash vm.GetHashFunc, statedb state.StateDB, header *types.Header, tx *types.Transaction, gp *GasPool) (*types.Receipt, uint64, error) {
	msg := TransactionToMessage(tx)

	snapshot := statedb.Snapshot()

	result, err := applyMessage(config, getHash, statedb, header, &msg, gp)
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
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = result.UsedGas
	receipt.EffectiveGasPrice = msgEffectiveGasPrice(&msg, header.BaseFee)
	receipt.Type = tx.Type()

	// Set contract address for contract creation transactions.
	if msg.To == nil {
		receipt.ContractAddress = result.ContractAddress
	}

	// Set EIP-4844 blob gas fields.
	if blobGas := tx.BlobGas(); blobGas > 0 {
		receipt.BlobGasUsed = blobGas
		if header.ExcessBlobGas != nil {
			receipt.BlobGasPrice = calcBlobBaseFee(*header.ExcessBlobGas)
		}
	}

	// Collect logs from state and compute bloom filter.
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

// accessListGas computes the gas cost for an EIP-2930 access list.
// Per EIP-2930: 2400 gas per address, 1900 gas per storage key.
func accessListGas(accessList types.AccessList) uint64 {
	var gas uint64
	for _, tuple := range accessList {
		gas += 2400 // TxAccessListAddressGas
		gas += uint64(len(tuple.StorageKeys)) * 1900 // TxAccessListStorageKeyGas
	}
	return gas
}

// applyMessage executes a transaction message against the state.
func applyMessage(config *ChainConfig, getHash vm.GetHashFunc, statedb state.StateDB, header *types.Header, msg *Message, gp *GasPool) (*ExecutionResult, error) {
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

	// Calculate effective gas price per EIP-1559.
	gasPrice := msgEffectiveGasPrice(msg, header.BaseFee)
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

	// Compute intrinsic gas (includes access list costs per EIP-2930)
	igas := intrinsicGas(msg.Data, isCreate)
	igas += accessListGas(msg.AccessList)
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
		GetHash:     getHash,
		BlockNumber: header.Number,
		Time:        header.Time,
		Coinbase:    header.Coinbase,
		GasLimit:    header.GasLimit,
		BaseFee:     header.BaseFee,
		PrevRandao:  header.MixDigest,
	}
	txCtx := vm.TxContext{
		Origin:     msg.From,
		GasPrice:   gasPrice,
		BlobHashes: msg.BlobHashes,
	}
	evm := vm.NewEVMWithState(blockCtx, txCtx, vm.Config{}, statedb)

	// Select the correct jump table based on fork rules.
	if config != nil {
		rules := config.Rules(header.Number, config.IsMerge(), header.Time)
		evm.SetJumpTable(vm.SelectJumpTable(vm.ForkRules{
			IsPrague:         rules.IsPrague,
			IsCancun:         rules.IsCancun,
			IsShanghai:       rules.IsShanghai,
			IsMerge:          rules.IsMerge,
			IsLondon:         rules.IsLondon,
			IsBerlin:         rules.IsBerlin,
			IsIstanbul:       rules.IsIstanbul,
			IsConstantinople: rules.IsConstantinople,
			IsByzantium:      rules.IsByzantium,
			IsHomestead:      rules.IsHomestead,
		}))
	}

	// Pre-warm EIP-2930 access list: mark sender, destination, and precompiles as warm.
	statedb.AddAddressToAccessList(msg.From)
	if msg.To != nil {
		statedb.AddAddressToAccessList(*msg.To)
	}
	for _, tuple := range msg.AccessList {
		statedb.AddAddressToAccessList(tuple.Address)
		for _, key := range tuple.StorageKeys {
			statedb.AddSlotToAccessList(tuple.Address, key)
		}
	}

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

	// Pay tip to coinbase (EIP-1559: effective_tip * gasUsed goes to block producer).
	if header.BaseFee != nil && header.BaseFee.Sign() > 0 {
		tip := new(big.Int).Sub(gasPrice, header.BaseFee)
		if tip.Sign() > 0 {
			tipPayment := new(big.Int).Mul(tip, new(big.Int).SetUint64(gasUsed))
			statedb.AddBalance(header.Coinbase, tipPayment)
		}
	} else {
		// Pre-EIP-1559: all gas payment goes to coinbase.
		coinbasePayment := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(gasUsed))
		statedb.AddBalance(header.Coinbase, coinbasePayment)
	}

	return &ExecutionResult{
		UsedGas:         gasUsed,
		Err:             execErr,
		ReturnData:      returnData,
		ContractAddress: contractAddr,
	}, nil
}

// msgEffectiveGasPrice computes the actual gas price paid per EIP-1559.
// For legacy txs, it returns GasPrice directly.
// For EIP-1559 txs, it returns min(GasFeeCap, BaseFee + GasTipCap).
func msgEffectiveGasPrice(msg *Message, baseFee *big.Int) *big.Int {
	if msg.GasFeeCap != nil && baseFee != nil && baseFee.Sign() > 0 {
		// EIP-1559 transaction
		tip := msg.GasTipCap
		if tip == nil {
			tip = new(big.Int)
		}
		effectivePrice := new(big.Int).Add(baseFee, tip)
		if effectivePrice.Cmp(msg.GasFeeCap) > 0 {
			effectivePrice = new(big.Int).Set(msg.GasFeeCap)
		}
		return effectivePrice
	}
	// Legacy transaction
	if msg.GasPrice != nil {
		return new(big.Int).Set(msg.GasPrice)
	}
	return new(big.Int)
}

// calcBlobBaseFee computes the blob base fee from the excess blob gas.
// Per EIP-4844: blob_base_fee = MIN_BLOB_BASE_FEE * e^(excess_blob_gas / BLOB_BASE_FEE_UPDATE_FRACTION)
// We use the fake exponential approximation from the EIP.
func calcBlobBaseFee(excessBlobGas uint64) *big.Int {
	return fakeExponential(big.NewInt(1), new(big.Int).SetUint64(excessBlobGas), big.NewInt(3338477))
}

// fakeExponential approximates factor * e^(numerator / denominator) using Taylor expansion.
func fakeExponential(factor, numerator, denominator *big.Int) *big.Int {
	i := big.NewInt(1)
	output := new(big.Int)
	accum := new(big.Int).Mul(factor, denominator)
	for accum.Sign() > 0 {
		output.Add(output, accum)
		accum.Mul(accum, numerator)
		accum.Div(accum, new(big.Int).Mul(denominator, i))
		i.Add(i, big.NewInt(1))
	}
	return output.Div(output, denominator)
}
