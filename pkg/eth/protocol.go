// Package eth implements the eth/68 wire protocol handler connecting P2P
// networking to the blockchain and transaction pool.
package eth

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/p2p"
)

// Protocol version.
const ETH68 = 68

// MaxHeaders is the maximum number of headers returned in a single response.
const MaxHeaders = 1024

// MaxBodies is the maximum number of block bodies returned in a single response.
const MaxBodies = 512

// Blockchain defines the interface for blockchain operations needed by the handler.
type Blockchain interface {
	CurrentBlock() *types.Block
	GetBlock(hash types.Hash) *types.Block
	GetBlockByNumber(number uint64) *types.Block
	HasBlock(hash types.Hash) bool
	InsertBlock(block *types.Block) error
	Genesis() *types.Block
}

// TxPool defines the interface for transaction pool operations needed by the handler.
type TxPool interface {
	AddRemote(tx *types.Transaction) error
	Get(hash types.Hash) *types.Transaction
	Pending() map[types.Address][]*types.Transaction
}

// StatusInfo holds the local chain status for handshake exchange.
type StatusInfo struct {
	ProtocolVersion uint32
	NetworkID       uint64
	TD              *big.Int
	Head            types.Hash
	Genesis         types.Hash
	ForkID          p2p.ForkID

	// EIP-4444: advertise available history range so peers know what data
	// this node can serve. OldestBlock is the lowest block number for which
	// bodies and receipts are available. Zero means no pruning has occurred.
	OldestBlock uint64
}
