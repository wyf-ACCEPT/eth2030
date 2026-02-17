package rpc

import (
	"math/big"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
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
}
