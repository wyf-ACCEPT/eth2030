package rpc

import (
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
	"github.com/eth2030/eth2030/trie"
)

// Extended backend errors.
var (
	ErrBackendBlockNotFound   = errors.New("backend: block not found")
	ErrBackendStateUnavail    = errors.New("backend: state unavailable")
	ErrBackendTxNotFound      = errors.New("backend: transaction not found")
	ErrBackendGasCapExceeded  = errors.New("backend: gas cap exceeded")
	ErrBackendHistoryPruned   = errors.New("backend: historical data pruned (EIP-4444)")
	ErrBackendNoEstimate      = errors.New("backend: gas estimation failed")
	ErrBackendReceiptNotFound = errors.New("backend: receipt not found")
)

// GasEstimationConfig holds gas estimation parameters.
type GasEstimationConfig struct {
	MaxGasCap     uint64
	BinarySearch  bool
	MaxIterations int
}

// DefaultGasEstimationConfig returns sensible defaults.
func DefaultGasEstimationConfig() GasEstimationConfig {
	return GasEstimationConfig{
		MaxGasCap:     50_000_000,
		BinarySearch:  true,
		MaxIterations: 20,
	}
}

// FeeHistoryEntry holds fee data for a single block in an eth_feeHistory response.
type FeeHistoryEntry struct {
	BaseFee      *big.Int
	GasUsedRatio float64
	Rewards      []*big.Int
}

// ChainStateAccessor provides read access to chain state at a given block.
type ChainStateAccessor struct {
	mu      sync.RWMutex
	backend Backend
}

// NewChainStateAccessor wraps a Backend for higher-level state access.
func NewChainStateAccessor(backend Backend) *ChainStateAccessor {
	return &ChainStateAccessor{backend: backend}
}

// GetBalance returns the balance of an account at the given block number.
func (csa *ChainStateAccessor) GetBalance(addr types.Address, blockNum BlockNumber) (*big.Int, error) {
	header := csa.backend.HeaderByNumber(blockNum)
	if header == nil {
		return nil, ErrBackendBlockNotFound
	}
	stateDB, err := csa.backend.StateAt(header.Root)
	if err != nil {
		return nil, ErrBackendStateUnavail
	}
	return stateDB.GetBalance(addr), nil
}

// GetNonce returns the nonce of an account at the given block number.
func (csa *ChainStateAccessor) GetNonce(addr types.Address, blockNum BlockNumber) (uint64, error) {
	header := csa.backend.HeaderByNumber(blockNum)
	if header == nil {
		return 0, ErrBackendBlockNotFound
	}
	stateDB, err := csa.backend.StateAt(header.Root)
	if err != nil {
		return 0, ErrBackendStateUnavail
	}
	return stateDB.GetNonce(addr), nil
}

// GetCode returns the code of a contract at the given block number.
func (csa *ChainStateAccessor) GetCode(addr types.Address, blockNum BlockNumber) ([]byte, error) {
	header := csa.backend.HeaderByNumber(blockNum)
	if header == nil {
		return nil, ErrBackendBlockNotFound
	}
	stateDB, err := csa.backend.StateAt(header.Root)
	if err != nil {
		return nil, ErrBackendStateUnavail
	}
	return stateDB.GetCode(addr), nil
}

// GetStorageAt returns a storage slot value for a contract at a given block.
func (csa *ChainStateAccessor) GetStorageAt(addr types.Address, slot types.Hash, blockNum BlockNumber) (types.Hash, error) {
	header := csa.backend.HeaderByNumber(blockNum)
	if header == nil {
		return types.Hash{}, ErrBackendBlockNotFound
	}
	stateDB, err := csa.backend.StateAt(header.Root)
	if err != nil {
		return types.Hash{}, ErrBackendStateUnavail
	}
	return stateDB.GetState(addr, slot), nil
}

// GetAccountInfo returns balance, nonce, and code hash for an account.
type AccountInfo struct {
	Balance  *big.Int
	Nonce    uint64
	CodeHash types.Hash
}

// GetAccountInfo returns the balance, nonce, and code hash for the given address.
func (csa *ChainStateAccessor) GetAccountInfo(addr types.Address, blockNum BlockNumber) (*AccountInfo, error) {
	header := csa.backend.HeaderByNumber(blockNum)
	if header == nil {
		return nil, ErrBackendBlockNotFound
	}
	stateDB, err := csa.backend.StateAt(header.Root)
	if err != nil {
		return nil, ErrBackendStateUnavail
	}
	return &AccountInfo{
		Balance:  stateDB.GetBalance(addr),
		Nonce:    stateDB.GetNonce(addr),
		CodeHash: stateDB.GetCodeHash(addr),
	}, nil
}

// GasEstimator provides gas estimation via binary search over EVM calls.
type GasEstimator struct {
	backend Backend
	config  GasEstimationConfig
}

// NewGasEstimator creates a new gas estimator with the given config.
func NewGasEstimator(backend Backend, config GasEstimationConfig) *GasEstimator {
	return &GasEstimator{backend: backend, config: config}
}

// EstimateGas performs binary search gas estimation for a call.
func (ge *GasEstimator) EstimateGas(
	from types.Address,
	to *types.Address,
	data []byte,
	value *big.Int,
	blockNum BlockNumber,
) (uint64, error) {
	header := ge.backend.HeaderByNumber(blockNum)
	if header == nil {
		return 0, ErrBackendBlockNotFound
	}

	// Start with block gas limit as the ceiling.
	hi := header.GasLimit
	if hi > ge.config.MaxGasCap {
		hi = ge.config.MaxGasCap
	}

	// Intrinsic gas floor: 21000 for a basic transfer.
	lo := uint64(21000)

	if !ge.config.BinarySearch {
		// Simple mode: just run at hi and return gasUsed.
		_, gasUsed, err := ge.backend.EVMCall(from, to, data, hi, value, blockNum)
		if err != nil {
			return 0, err
		}
		return gasUsed, nil
	}

	// Binary search for the minimum gas that doesn't revert.
	for i := 0; i < ge.config.MaxIterations && lo < hi; i++ {
		mid := lo + (hi-lo)/2
		_, _, err := ge.backend.EVMCall(from, to, data, mid, value, blockNum)
		if err != nil {
			// Execution failed at mid, need more gas.
			lo = mid + 1
		} else {
			// Execution succeeded, try less gas.
			hi = mid
		}
	}

	// Verify the final estimate works.
	_, _, err := ge.backend.EVMCall(from, to, data, hi, value, blockNum)
	if err != nil {
		return 0, ErrBackendNoEstimate
	}
	return hi, nil
}

// FeeHistoryCollector aggregates fee history from chain headers.
type FeeHistoryCollector struct {
	backend Backend
}

// NewFeeHistoryCollector creates a new fee history collector.
func NewFeeHistoryCollector(backend Backend) *FeeHistoryCollector {
	return &FeeHistoryCollector{backend: backend}
}

// Collect returns fee history entries for blockCount blocks ending at newestBlock.
func (fhc *FeeHistoryCollector) Collect(blockCount uint64, newestBlock BlockNumber) ([]FeeHistoryEntry, uint64, error) {
	newestHeader := fhc.backend.HeaderByNumber(newestBlock)
	if newestHeader == nil {
		return nil, 0, ErrBackendBlockNotFound
	}
	newestNum := newestHeader.Number.Uint64()

	oldest := uint64(0)
	if newestNum+1 >= blockCount {
		oldest = newestNum + 1 - blockCount
	}

	entries := make([]FeeHistoryEntry, 0, blockCount)
	for i := oldest; i <= newestNum; i++ {
		header := fhc.backend.HeaderByNumber(BlockNumber(i))
		entry := FeeHistoryEntry{}
		if header != nil {
			if header.BaseFee != nil {
				entry.BaseFee = new(big.Int).Set(header.BaseFee)
			} else {
				entry.BaseFee = new(big.Int)
			}
			if header.GasLimit > 0 {
				entry.GasUsedRatio = float64(header.GasUsed) / float64(header.GasLimit)
			}
		} else {
			entry.BaseFee = new(big.Int)
		}
		entries = append(entries, entry)
	}
	return entries, oldest, nil
}

// ChainIDAccessor returns the chain ID from the backend.
type ChainIDAccessor struct {
	backend Backend
}

// NewChainIDAccessor creates a new chain ID accessor.
func NewChainIDAccessor(backend Backend) *ChainIDAccessor {
	return &ChainIDAccessor{backend: backend}
}

// ChainID returns the chain ID.
func (cia *ChainIDAccessor) ChainID() *big.Int {
	return cia.backend.ChainID()
}

// ReceiptAccessor provides receipt retrieval helpers.
type ReceiptAccessor struct {
	backend Backend
}

// NewReceiptAccessor creates a new receipt accessor.
func NewReceiptAccessor(backend Backend) *ReceiptAccessor {
	return &ReceiptAccessor{backend: backend}
}

// GetReceiptsByBlock returns receipts for a given block hash.
func (ra *ReceiptAccessor) GetReceiptsByBlock(blockHash types.Hash) []*types.Receipt {
	return ra.backend.GetReceipts(blockHash)
}

// GetReceiptByTxHash finds a specific receipt from the block's receipts.
func (ra *ReceiptAccessor) GetReceiptByTxHash(blockHash types.Hash, txHash types.Hash) *types.Receipt {
	receipts := ra.backend.GetReceipts(blockHash)
	for _, r := range receipts {
		if r.TxHash == txHash {
			return r
		}
	}
	return nil
}

// BackendServices bundles all backend service accessors.
type BackendServices struct {
	State        *ChainStateAccessor
	GasEstimator *GasEstimator
	FeeHistory   *FeeHistoryCollector
	ChainID      *ChainIDAccessor
	Receipts     *ReceiptAccessor
}

// NewBackendServices creates all service accessors from a Backend.
func NewBackendServices(backend Backend) *BackendServices {
	return &BackendServices{
		State:        NewChainStateAccessor(backend),
		GasEstimator: NewGasEstimator(backend, DefaultGasEstimationConfig()),
		FeeHistory:   NewFeeHistoryCollector(backend),
		ChainID:      NewChainIDAccessor(backend),
		Receipts:     NewReceiptAccessor(backend),
	}
}

// Ensure Backend interface usage is preserved.
var _ Backend = (*backendTypeCheck)(nil)

// backendTypeCheck is only used at compile time to verify the Backend interface
// is satisfied (it will never be instantiated).
type backendTypeCheck struct{}

func (b *backendTypeCheck) HeaderByNumber(_ BlockNumber) *types.Header           { return nil }
func (b *backendTypeCheck) HeaderByHash(_ types.Hash) *types.Header              { return nil }
func (b *backendTypeCheck) BlockByNumber(_ BlockNumber) *types.Block              { return nil }
func (b *backendTypeCheck) BlockByHash(_ types.Hash) *types.Block                { return nil }
func (b *backendTypeCheck) CurrentHeader() *types.Header                          { return nil }
func (b *backendTypeCheck) ChainID() *big.Int                                     { return nil }
func (b *backendTypeCheck) StateAt(_ types.Hash) (state.StateDB, error)           { return nil, nil }
func (b *backendTypeCheck) SendTransaction(_ *types.Transaction) error            { return nil }
func (b *backendTypeCheck) GetTransaction(_ types.Hash) (*types.Transaction, uint64, uint64) {
	return nil, 0, 0
}
func (b *backendTypeCheck) SuggestGasPrice() *big.Int                     { return nil }
func (b *backendTypeCheck) GetReceipts(_ types.Hash) []*types.Receipt     { return nil }
func (b *backendTypeCheck) GetLogs(_ types.Hash) []*types.Log             { return nil }
func (b *backendTypeCheck) GetBlockReceipts(_ uint64) []*types.Receipt    { return nil }
func (b *backendTypeCheck) GetProof(_ types.Address, _ []types.Hash, _ BlockNumber) (*trie.AccountProof, error) {
	return nil, nil
}
func (b *backendTypeCheck) EVMCall(_ types.Address, _ *types.Address, _ []byte, _ uint64, _ *big.Int, _ BlockNumber) ([]byte, uint64, error) {
	return nil, 0, nil
}
func (b *backendTypeCheck) TraceTransaction(_ types.Hash) (*vm.StructLogTracer, error) {
	return nil, nil
}
func (b *backendTypeCheck) HistoryOldestBlock() uint64 { return 0 }
