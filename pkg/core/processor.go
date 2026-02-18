package core

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/bal"
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

	// EIP-7702: per-authorization base gas cost charged for every entry
	// in the authorization list, regardless of whether the target account
	// is empty or not.
	PerAuthBaseCost uint64 = 12500

	// EIP-7702: additional gas charged per authorization entry that targets
	// an account that does not yet exist in the state trie (empty account).
	PerEmptyAccountCost uint64 = 25000
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
	result, err := p.ProcessWithBAL(block, statedb)
	if err != nil {
		return nil, err
	}
	return result.Receipts, nil
}

// ProcessWithBAL executes all transactions in a block and returns the receipts
// along with the computed Block Access List (EIP-7928). The BAL is populated
// only when the Amsterdam fork is active; otherwise it is nil.
func (p *StateProcessor) ProcessWithBAL(block *types.Block, statedb state.StateDB) (*ProcessResult, error) {
	var (
		receipts []*types.Receipt
		gasPool  = new(GasPool).AddGas(block.GasLimit())
		header   = block.Header()
	)

	// EIP-4788: store the parent beacon block root in the beacon root contract.
	// This is a system-level operation that runs before any user transactions.
	if p.config != nil && p.config.IsCancun(header.Time) {
		ProcessBeaconBlockRoot(statedb, header)
	}

	// Determine if BAL tracking is active for this block.
	balActive := p.config != nil && p.config.IsAmsterdam(header.Time)

	var blockBAL *bal.BlockAccessList
	if balActive {
		blockBAL = bal.NewBlockAccessList()
	}

	// EIP-2935: store parent block hash in history storage contract (Prague+).
	if p.config != nil && p.config.IsPrague(header.Time) && header.Number.Uint64() > 0 {
		ProcessParentBlockHash(statedb, header.Number.Uint64()-1, header.ParentHash)
	}

	var cumulativeGasUsed uint64

	for i, tx := range block.Transactions() {
		statedb.SetTxContext(tx.Hash(), i)

		// Capture pre-state for BAL tracking before applying the transaction.
		var (
			preBalances map[types.Address]*big.Int
			preNonces   map[types.Address]uint64
		)
		if balActive {
			preBalances, preNonces = capturePreState(statedb, tx)
		}

		receipt, usedGas, err := applyTransaction(p.config, p.getHash, statedb, header, tx, gasPool)
		if err != nil {
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx, err)
		}

		// Track cumulative gas across all transactions in the block.
		cumulativeGasUsed += usedGas
		receipt.CumulativeGasUsed = cumulativeGasUsed
		receipt.TransactionIndex = uint(i)
		receipt.BlockHash = block.Hash()
		receipt.BlockNumber = new(big.Int).Set(header.Number)

		// Set log context fields (BlockNumber, BlockHash, Index).
		setLogContext(receipt, header, block.Hash())

		receipts = append(receipts, receipt)

		// After successful tx, record state changes in the BAL.
		if balActive {
			tracker := bal.NewTracker()
			populateTracker(tracker, statedb, preBalances, preNonces)
			txBAL := tracker.Build(uint64(i + 1)) // AccessIndex 1..n for transactions
			for _, entry := range txBAL.Entries {
				blockBAL.AddEntry(entry)
			}
		}
	}

	// Assign global log indices across all receipts so that each log
	// in the block has a unique, sequential Index value.
	var logIndex uint
	for _, receipt := range receipts {
		for _, log := range receipt.Logs {
			log.Index = logIndex
			logIndex++
		}
	}

	// EIP-4895: process beacon chain withdrawals after all transactions.
	// Withdrawals are applied post-Shanghai (activated with the merge).
	if p.config != nil && p.config.IsShanghai(header.Time) {
		ProcessWithdrawals(statedb, block.Withdrawals())
	}

	return &ProcessResult{
		Receipts:        receipts,
		BlockAccessList: blockBAL,
	}, nil
}

// capturePreState captures balance and nonce values for addresses involved
// in a transaction before it is applied. This allows computing the delta
// for the BAL after the transaction completes.
func capturePreState(statedb state.StateDB, tx *types.Transaction) (map[types.Address]*big.Int, map[types.Address]uint64) {
	balances := make(map[types.Address]*big.Int)
	nonces := make(map[types.Address]uint64)

	// Sender (from cached sender on the tx).
	if sender := tx.Sender(); sender != nil {
		balances[*sender] = new(big.Int).Set(statedb.GetBalance(*sender))
		nonces[*sender] = statedb.GetNonce(*sender)
	}

	// Recipient.
	if to := tx.To(); to != nil {
		balances[*to] = new(big.Int).Set(statedb.GetBalance(*to))
		nonces[*to] = statedb.GetNonce(*to)
	}

	return balances, nonces
}

// populateTracker records balance and nonce changes into the BAL tracker
// by comparing pre-tx state snapshots with post-tx state.
func populateTracker(tracker *bal.AccessTracker, statedb state.StateDB, preBalances map[types.Address]*big.Int, preNonces map[types.Address]uint64) {
	for addr, preBal := range preBalances {
		postBal := statedb.GetBalance(addr)
		if preBal.Cmp(postBal) != 0 {
			tracker.RecordBalanceChange(addr, preBal, postBal)
		}
	}
	for addr, preNonce := range preNonces {
		postNonce := statedb.GetNonce(addr)
		if preNonce != postNonce {
			tracker.RecordNonceChange(addr, preNonce, postNonce)
		}
	}
}

// ProcessResult holds the output of block processing: receipts, EIP-7685 requests,
// and the Block Access List (EIP-7928) when the Amsterdam fork is active.
type ProcessResult struct {
	Receipts        []*types.Receipt
	Requests        types.Requests
	BlockAccessList *bal.BlockAccessList
}

// ProcessWithdrawals applies EIP-4895 beacon chain withdrawals to the state.
// Each withdrawal credits the specified address with the withdrawal amount.
// The amount field is denominated in Gwei and is converted to Wei (1 Gwei = 1e9 Wei).
// Withdrawals do not consume gas and are applied after all transactions.
// A nil or empty withdrawals slice is a no-op.
func ProcessWithdrawals(statedb state.StateDB, withdrawals []*types.Withdrawal) {
	for _, w := range withdrawals {
		if w == nil {
			continue
		}
		// Convert Gwei to Wei: amount_wei = amount_gwei * 1e9.
		amount := new(big.Int).SetUint64(w.Amount)
		amount.Mul(amount, big.NewInt(1_000_000_000))
		statedb.AddBalance(w.Address, amount)
	}
}

// CalcWithdrawalsHash computes the withdrawals root hash from a slice of
// withdrawals. Each withdrawal is RLP-encoded as [index, validatorIndex,
// address, amount] and inserted into a Merkle Patricia Trie keyed by its
// position index. Returns EmptyRootHash for nil or empty withdrawals.
func CalcWithdrawalsHash(withdrawals []*types.Withdrawal) types.Hash {
	return deriveWithdrawalsRoot(withdrawals)
}

// ProcessWithRequests executes all transactions in a block and then collects
// EIP-7685 execution layer requests from system contracts. Use this for
// post-Prague blocks that include the requests_hash field.
func (p *StateProcessor) ProcessWithRequests(block *types.Block, statedb state.StateDB) (*ProcessResult, error) {
	receipts, err := p.Process(block, statedb)
	if err != nil {
		return nil, err
	}

	requests, err := ProcessRequests(p.config, statedb, block.Header())
	if err != nil {
		return nil, fmt.Errorf("processing execution requests: %w", err)
	}

	return &ProcessResult{
		Receipts: receipts,
		Requests: requests,
	}, nil
}

// ProcessRequests collects execution layer requests from system contracts
// after all transactions are processed. This implements EIP-7685.
//
// Per the EIP, requests are generated by calling specific system contracts:
//   - Deposit requests (0x00): read from the deposit contract
//   - Withdrawal requests (0x01): call the withdrawal request contract
//   - Consolidation requests (0x02): call the consolidation request contract
//
// System calls use a special system address as the caller, with a large gas
// allowance. The calls are not user-initiated transactions and do not
// consume block gas.
func ProcessRequests(config *ChainConfig, statedb state.StateDB, header *types.Header) (types.Requests, error) {
	if config == nil || !config.IsPrague(header.Time) {
		return nil, nil
	}

	var requests types.Requests

	// Collect deposit requests (type 0x00).
	depositRequests, err := processDepositRequests(statedb)
	if err != nil {
		return nil, fmt.Errorf("deposit requests: %w", err)
	}
	requests = append(requests, depositRequests...)

	// Collect withdrawal requests (type 0x01).
	withdrawalRequests, err := processWithdrawalRequests(statedb)
	if err != nil {
		return nil, fmt.Errorf("withdrawal requests: %w", err)
	}
	requests = append(requests, withdrawalRequests...)

	// Collect consolidation requests (type 0x02).
	consolidationRequests, err := processConsolidationRequests(statedb)
	if err != nil {
		return nil, fmt.Errorf("consolidation requests: %w", err)
	}
	requests = append(requests, consolidationRequests...)

	return requests, nil
}

// processDepositRequests reads deposit request data from the deposit contract.
// In a full implementation, this would read the contract's storage or logs.
// For now, it reads any data stored at well-known storage slots.
func processDepositRequests(statedb state.StateDB) (types.Requests, error) {
	addr := types.DepositContractAddress
	if !statedb.Exist(addr) {
		return nil, nil
	}
	return readRequestsFromStorage(statedb, addr, types.DepositRequestType)
}

// processWithdrawalRequests calls the withdrawal request system contract
// and collects the resulting requests.
func processWithdrawalRequests(statedb state.StateDB) (types.Requests, error) {
	addr := types.WithdrawalRequestAddress
	if !statedb.Exist(addr) {
		return nil, nil
	}
	return readRequestsFromStorage(statedb, addr, types.WithdrawalRequestType)
}

// processConsolidationRequests calls the consolidation request system contract
// and collects the resulting requests.
func processConsolidationRequests(statedb state.StateDB) (types.Requests, error) {
	addr := types.ConsolidationRequestAddress
	if !statedb.Exist(addr) {
		return nil, nil
	}
	return readRequestsFromStorage(statedb, addr, types.ConsolidationRequestType)
}

// requestCountSlot is the well-known storage slot (slot 0) where system
// contracts store the count of pending requests.
var requestCountSlot = types.Hash{}

// requestDataSlotBase is the base storage slot (slot 1) where system
// contracts store request data sequentially.
var requestDataSlotBase = types.BytesToHash([]byte{0x01})

// readRequestsFromStorage reads requests from a system contract's storage.
//
// Convention: slot 0 holds the request count (as a uint256). Slots 1..N each
// hold one request's data as a raw 32-byte word. The contract is expected to
// pack request data into consecutive slots starting at slot 1.
//
// After reading, the count slot is cleared to zero (requests are consumed).
func readRequestsFromStorage(statedb state.StateDB, addr types.Address, reqType byte) (types.Requests, error) {
	countVal := statedb.GetState(addr, requestCountSlot)
	count := countToUint64(countVal)
	if count == 0 {
		return nil, nil
	}

	var requests types.Requests
	for i := uint64(0); i < count; i++ {
		slot := incrementSlot(requestDataSlotBase, i)
		data := statedb.GetState(addr, slot)

		if data == (types.Hash{}) {
			continue
		}

		trimmed := trimTrailingZeros(data[:])
		if len(trimmed) > 0 {
			requests = append(requests, types.NewRequest(reqType, trimmed))
		}
	}

	// Clear the request count after consumption.
	statedb.SetState(addr, requestCountSlot, types.Hash{})

	return requests, nil
}

// countToUint64 extracts a uint64 count from a 32-byte storage value.
// The value is stored as big-endian uint256; we take the low 8 bytes.
func countToUint64(val types.Hash) uint64 {
	var count uint64
	for i := 24; i < 32; i++ {
		count = (count << 8) | uint64(val[i])
	}
	return count
}

// incrementSlot adds an offset to a storage slot hash. Used to compute
// sequential slot addresses: slot = base + offset.
func incrementSlot(base types.Hash, offset uint64) types.Hash {
	var result types.Hash
	copy(result[:], base[:])
	carry := offset
	for i := 31; i >= 0 && carry > 0; i-- {
		sum := uint64(result[i]) + (carry & 0xFF)
		result[i] = byte(sum & 0xFF)
		carry = (carry >> 8) + (sum >> 8)
	}
	return result
}

// trimTrailingZeros removes trailing zero bytes from a slice.
func trimTrailingZeros(b []byte) []byte {
	end := len(b)
	for end > 0 && b[end-1] == 0 {
		end--
	}
	if end == 0 {
		return nil
	}
	out := make([]byte, end)
	copy(out, b[:end])
	return out
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

	// Create receipt. CumulativeGasUsed is set to this transaction's gas
	// usage as a placeholder; the caller (Process/ProcessWithBAL) is
	// responsible for accumulating it across all transactions in the block.
	var receiptStatus uint64
	if result.Failed() {
		receiptStatus = types.ReceiptStatusFailed
	} else {
		receiptStatus = types.ReceiptStatusSuccessful
	}

	receipt := types.NewReceipt(receiptStatus, result.UsedGas)
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

// setLogContext populates block-level context fields on each log in the
// receipt: BlockNumber, BlockHash, and the global Index (log position within
// the block). The TxHash and TxIndex are already set by StateDB.AddLog.
func setLogContext(receipt *types.Receipt, header *types.Header, blockHash types.Hash) {
	for _, log := range receipt.Logs {
		log.BlockNumber = header.Number.Uint64()
		log.BlockHash = blockHash
	}
}

// intrinsicGas computes the base gas cost of a transaction before EVM execution.
// For EIP-7702 SetCode transactions, authCount is the number of authorization
// entries, and emptyAuthCount is the number of those entries targeting accounts
// that do not yet exist in state.
func intrinsicGas(data []byte, isCreate bool, authCount, emptyAuthCount uint64) uint64 {
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
	// EIP-7702: per-authorization gas costs.
	gas += authCount * PerAuthBaseCost
	gas += emptyAuthCount * PerEmptyAccountCost
	return gas
}

// EIP-7623: calldata gas cost floor constants.
// These define a higher floor cost for calldata to incentivize blob usage.
const (
	// TotalCostFloorPerToken is the floor gas cost per non-zero calldata byte
	// under EIP-7623. The actual gas charged is max(standard_cost, floor_cost).
	TotalCostFloorPerToken uint64 = 10

	// StandardTokenCost is the standard EIP-2028 calldata cost for non-zero bytes.
	StandardTokenCost uint64 = 16

	// FloorTokenCost is the EIP-7623 floor cost applied after execution.
	// floorDataGas = tokens * TOTAL_COST_FLOOR_PER_TOKEN
	// where tokens = zero_bytes * 1 + nonzero_bytes * 4
	FloorTokenCost uint64 = 10
)

// calldataFloorGas computes the EIP-7623 calldata floor gas cost.
// tokens = zero_bytes * 1 + nonzero_bytes * 4
// floor_gas = 21000 + tokens * TOTAL_COST_FLOOR_PER_TOKEN
func calldataFloorGas(data []byte, isCreate bool) uint64 {
	var tokens uint64
	for _, b := range data {
		if b == 0 {
			tokens += 1
		} else {
			tokens += 4
		}
	}
	floor := TxGas + tokens*TotalCostFloorPerToken
	if isCreate {
		floor += TxCreateGas
	}
	return floor
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

	// Count EIP-7702 authorizations for intrinsic gas calculation.
	var authCount, emptyAuthCount uint64
	if msg.TxType == types.SetCodeTxType && len(msg.AuthList) > 0 {
		authCount = uint64(len(msg.AuthList))
		for _, auth := range msg.AuthList {
			if !statedb.Exist(auth.Address) || statedb.Empty(auth.Address) {
				emptyAuthCount++
			}
		}
	}

	// Compute intrinsic gas (includes access list costs per EIP-2930
	// and EIP-7702 authorization costs).
	igas := intrinsicGas(msg.Data, isCreate, authCount, emptyAuthCount)
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
		forkRules := vm.ForkRules{
			IsGlamsterdan:    rules.IsGlamsterdan,
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
		}
		evm.SetJumpTable(vm.SelectJumpTable(forkRules))
		evm.SetPrecompiles(vm.SelectPrecompiles(forkRules))
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

	// EIP-7702: process authorization list for SetCode (type 0x04) transactions.
	// Per the spec, authorizations are processed before main EVM execution.
	// The authorization list sets delegation code on signer accounts.
	if msg.TxType == types.SetCodeTxType && len(msg.AuthList) > 0 {
		var chainID *big.Int
		if config != nil && config.ChainID != nil {
			chainID = config.ChainID
		}
		if err := ProcessAuthorizations(statedb, msg.AuthList, chainID); err != nil {
			return nil, fmt.Errorf("processing EIP-7702 authorizations: %w", err)
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
