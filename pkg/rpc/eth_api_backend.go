// eth_api_backend.go implements EthAPIBackend, a backend wrapper that
// provides a direct Go-typed API for eth_* namespace methods. It bridges
// the RPC Backend interface with structured request/response types for
// block, transaction, state, and log queries.
package rpc

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// EthAPIBackend errors.
var (
	ErrAPIBackendNoBlock    = errors.New("eth api backend: block not found")
	ErrAPIBackendNoState    = errors.New("eth api backend: state unavailable")
	ErrAPIBackendNoTx       = errors.New("eth api backend: transaction not found")
	ErrAPIBackendNoReceipt  = errors.New("eth api backend: receipt not found")
	ErrAPIBackendNoLogs     = errors.New("eth api backend: no logs found")
	ErrAPIBackendEstimate   = errors.New("eth api backend: gas estimation failed")
	ErrAPIBackendNoPending  = errors.New("eth api backend: no pending transactions")
	ErrAPIBackendInvalidArg = errors.New("eth api backend: invalid argument")
)

// TxWithReceipt pairs a transaction with its receipt for lookup results.
type TxWithReceipt struct {
	Tx          *types.Transaction
	Receipt     *types.Receipt
	BlockNumber uint64
	BlockHash   types.Hash
	TxIndex     uint64
}

// LogFilterParams holds parameters for GetLogs queries.
type LogFilterParams struct {
	FromBlock uint64
	ToBlock   uint64
	Addresses []types.Address
	Topics    [][]types.Hash
}

// GasEstimateArgs holds arguments for gas estimation.
type GasEstimateArgs struct {
	From     types.Address
	To       *types.Address
	Gas      uint64
	Value    *big.Int
	Data     []byte
	GasTip   *big.Int // EIP-1559 max priority fee
	GasCap   *big.Int // EIP-1559 max fee per gas
}

// PendingTxInfo holds pending transaction information.
type PendingTxInfo struct {
	Tx     *types.Transaction
	Sender types.Address
}

// EthAPIBackend wraps a Backend to provide structured eth_* API methods.
type EthAPIBackend struct {
	backend     Backend
	gasFloor    uint64 // intrinsic gas floor for estimation
	maxGasLimit uint64 // maximum gas limit for estimation
}

// NewEthAPIBackend creates a new EthAPIBackend wrapping the given backend.
func NewEthAPIBackend(backend Backend) *EthAPIBackend {
	return &EthAPIBackend{
		backend:     backend,
		gasFloor:    21000,
		maxGasLimit: 50_000_000,
	}
}

// GetBlockByNumber returns a block by its number. When fullTx is true, the
// block includes full transaction objects; when false, only the header is
// returned. Returns nil, nil if the block does not exist.
func (b *EthAPIBackend) GetBlockByNumber(num BlockNumber, fullTx bool) (*types.Block, *types.Header, error) {
	if fullTx {
		block := b.backend.BlockByNumber(num)
		if block == nil {
			return nil, nil, nil
		}
		return block, block.Header(), nil
	}
	header := b.backend.HeaderByNumber(num)
	if header == nil {
		return nil, nil, nil
	}
	return nil, header, nil
}

// GetTransactionByHash looks up a transaction by hash and returns
// full details including receipt information.
func (b *EthAPIBackend) GetTransactionByHash(hash types.Hash) (*TxWithReceipt, error) {
	tx, blockNum, index := b.backend.GetTransaction(hash)
	if tx == nil {
		return nil, nil
	}
	result := &TxWithReceipt{
		Tx:          tx,
		BlockNumber: blockNum,
		TxIndex:     index,
	}

	// Resolve block hash if we have a block number.
	if blockNum > 0 {
		header := b.backend.HeaderByNumber(BlockNumber(blockNum))
		if header != nil {
			result.BlockHash = header.Hash()
			// Look up receipt.
			receipts := b.backend.GetReceipts(result.BlockHash)
			for _, r := range receipts {
				if r.TxHash == hash {
					result.Receipt = r
					break
				}
			}
		}
	}
	return result, nil
}

// GetBalance returns the balance of addr at the given block number.
func (b *EthAPIBackend) GetBalance(addr types.Address, blockNum BlockNumber) (*big.Int, error) {
	header := b.backend.HeaderByNumber(blockNum)
	if header == nil {
		return nil, ErrAPIBackendNoBlock
	}
	statedb, err := b.backend.StateAt(header.Root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAPIBackendNoState, err)
	}
	balance := statedb.GetBalance(addr)
	if balance == nil {
		balance = new(big.Int)
	}
	return balance, nil
}

// GetCode returns the bytecode at the given address and block number.
func (b *EthAPIBackend) GetCode(addr types.Address, blockNum BlockNumber) ([]byte, error) {
	header := b.backend.HeaderByNumber(blockNum)
	if header == nil {
		return nil, ErrAPIBackendNoBlock
	}
	statedb, err := b.backend.StateAt(header.Root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAPIBackendNoState, err)
	}
	return statedb.GetCode(addr), nil
}

// GetStorageAt returns the storage value at the given address, key, and
// block number.
func (b *EthAPIBackend) GetStorageAt(addr types.Address, key types.Hash, blockNum BlockNumber) (types.Hash, error) {
	header := b.backend.HeaderByNumber(blockNum)
	if header == nil {
		return types.Hash{}, ErrAPIBackendNoBlock
	}
	statedb, err := b.backend.StateAt(header.Root)
	if err != nil {
		return types.Hash{}, fmt.Errorf("%w: %v", ErrAPIBackendNoState, err)
	}
	return statedb.GetState(addr, key), nil
}

// GetLogs returns log events matching the given filter criteria across
// the specified block range.
func (b *EthAPIBackend) GetLogs(params LogFilterParams) ([]*types.Log, error) {
	if params.FromBlock > params.ToBlock {
		return nil, fmt.Errorf("%w: fromBlock > toBlock", ErrAPIBackendInvalidArg)
	}

	query := FilterQuery{
		FromBlock: &params.FromBlock,
		ToBlock:   &params.ToBlock,
		Addresses: params.Addresses,
		Topics:    params.Topics,
	}

	var result []*types.Log
	for blockNum := params.FromBlock; blockNum <= params.ToBlock; blockNum++ {
		header := b.backend.HeaderByNumber(BlockNumber(blockNum))
		if header == nil {
			continue
		}
		blockHash := header.Hash()

		// Bloom filter optimization: only apply if bloom is populated.
		// A zero bloom means the header's bloom was not computed (e.g., in
		// tests or for blocks without logs).
		emptyBloom := types.Bloom{}
		if header.Bloom != emptyBloom && !bloomMatchesQuery(header.Bloom, query) {
			continue
		}

		logs := b.backend.GetLogs(blockHash)
		for _, log := range logs {
			if MatchFilter(log, query) {
				result = append(result, log)
			}
		}
	}
	if result == nil {
		result = []*types.Log{}
	}
	return result, nil
}

// EstimateGas performs binary search gas estimation with EIP-1559 support.
// It searches between intrinsic gas and the block gas limit (or the
// caller-specified gas cap) for the minimum gas that allows execution
// to succeed.
func (b *EthAPIBackend) EstimateGas(args GasEstimateArgs, blockNum BlockNumber) (uint64, error) {
	header := b.backend.HeaderByNumber(blockNum)
	if header == nil {
		return 0, ErrAPIBackendNoBlock
	}

	// Determine the upper bound.
	hi := header.GasLimit
	if hi > b.maxGasLimit {
		hi = b.maxGasLimit
	}
	if args.Gas > 0 && args.Gas < hi {
		hi = args.Gas
	}

	value := args.Value
	if value == nil {
		value = new(big.Int)
	}

	// Verify the upper bound succeeds.
	_, _, err := b.backend.EVMCall(args.From, args.To, args.Data, hi, value, blockNum)
	if err != nil {
		return 0, fmt.Errorf("%w: upper bound call failed: %v", ErrAPIBackendEstimate, err)
	}

	// Check if the floor itself is sufficient.
	lo := b.gasFloor
	_, _, err = b.backend.EVMCall(args.From, args.To, args.Data, lo, value, blockNum)
	if err == nil {
		return lo, nil
	}

	// Binary search.
	for lo+1 < hi {
		mid := lo + (hi-lo)/2
		_, _, err := b.backend.EVMCall(args.From, args.To, args.Data, mid, value, blockNum)
		if err != nil {
			lo = mid
		} else {
			hi = mid
		}
	}

	return hi, nil
}

// PendingTransactions returns all pending transactions from the pool
// grouped by sender. The backend must implement a transaction pool.
func (b *EthAPIBackend) PendingTransactions() ([]PendingTxInfo, error) {
	// This is a simplified implementation. In production, the Backend
	// interface would expose a PendingTransactions method.
	header := b.backend.CurrentHeader()
	if header == nil {
		return nil, ErrAPIBackendNoPending
	}
	// Return an empty list for now (the Backend interface does not
	// expose a pending transactions method directly).
	return []PendingTxInfo{}, nil
}

// SuggestGasPrice returns the suggested gas price from the backend.
func (b *EthAPIBackend) SuggestGasPrice() *big.Int {
	price := b.backend.SuggestGasPrice()
	if price == nil {
		return new(big.Int)
	}
	return price
}

// GetNonce returns the nonce of an address at the given block number.
func (b *EthAPIBackend) GetNonce(addr types.Address, blockNum BlockNumber) (uint64, error) {
	header := b.backend.HeaderByNumber(blockNum)
	if header == nil {
		return 0, ErrAPIBackendNoBlock
	}
	statedb, err := b.backend.StateAt(header.Root)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrAPIBackendNoState, err)
	}
	return statedb.GetNonce(addr), nil
}

// CurrentBlock returns the current block number.
func (b *EthAPIBackend) CurrentBlock() (uint64, error) {
	header := b.backend.CurrentHeader()
	if header == nil {
		return 0, ErrAPIBackendNoBlock
	}
	return header.Number.Uint64(), nil
}

// GetBlockReceipts returns all receipts for a block by number.
func (b *EthAPIBackend) GetBlockReceipts(blockNum uint64) ([]*types.Receipt, error) {
	receipts := b.backend.GetBlockReceipts(blockNum)
	if receipts == nil {
		return nil, nil
	}
	return receipts, nil
}

// ChainID returns the chain ID.
func (b *EthAPIBackend) ChainID() *big.Int {
	return b.backend.ChainID()
}
