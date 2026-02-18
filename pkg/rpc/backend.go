package rpc

import (
	"math/big"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/core/vm"
	"github.com/eth2028/eth2028/trie"
)

// Backend provides access to chain data for the JSON-RPC API.
// This interface decouples the RPC layer from the chain implementation,
// following go-ethereum's ethapi.Backend pattern.
type Backend interface {
	// Chain data
	HeaderByNumber(number BlockNumber) *types.Header
	HeaderByHash(hash types.Hash) *types.Header
	BlockByNumber(number BlockNumber) *types.Block
	BlockByHash(hash types.Hash) *types.Block
	CurrentHeader() *types.Header
	ChainID() *big.Int

	// State access
	StateAt(root types.Hash) (state.StateDB, error)

	// Transaction pool
	SendTransaction(tx *types.Transaction) error
	GetTransaction(hash types.Hash) (*types.Transaction, uint64, uint64) // tx, blockNum, index

	// Gas estimation
	SuggestGasPrice() *big.Int

	// Receipts and logs
	GetReceipts(blockHash types.Hash) []*types.Receipt
	GetLogs(blockHash types.Hash) []*types.Log
	GetBlockReceipts(number uint64) []*types.Receipt

	// Proofs
	GetProof(addr types.Address, storageKeys []types.Hash, blockNumber BlockNumber) (*trie.AccountProof, error)

	// EVM execution
	EVMCall(from types.Address, to *types.Address, data []byte, gas uint64, value *big.Int, blockNumber BlockNumber) ([]byte, uint64, error)

	// Tracing
	TraceTransaction(txHash types.Hash) (*vm.StructLogTracer, error)
}
