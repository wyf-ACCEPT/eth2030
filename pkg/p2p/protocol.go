// Package p2p implements the devp2p eth protocol types for peer-to-peer networking.
package p2p

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// Protocol version constants.
const (
	ETH68 = 68
)

// eth/68 protocol message codes.
const (
	StatusMsg                    = 0x00
	NewBlockHashesMsg            = 0x01
	TransactionsMsg              = 0x02
	GetBlockHeadersMsg           = 0x03
	BlockHeadersMsg              = 0x04
	GetBlockBodiesMsg            = 0x05
	BlockBodiesMsg               = 0x06
	NewBlockMsg                  = 0x07
	NewPooledTransactionHashesMsg = 0x08
	GetPooledTransactionsMsg     = 0x09
	PooledTransactionsMsg        = 0x0a
	GetReceiptsMsg               = 0x0f
	ReceiptsMsg                  = 0x10
)

// StatusData represents the status message exchanged during the eth handshake.
type StatusData struct {
	ProtocolVersion uint32
	NetworkID       uint64
	TD              *big.Int
	Head            types.Hash
	Genesis         types.Hash
	ForkID          ForkID
}

// NewBlockHashesEntry is a single block hash announcement.
type NewBlockHashesEntry struct {
	Hash   types.Hash
	Number uint64
}

// HashOrNumber is a combined field for requesting a block header either by
// hash or by number. Exactly one must be set.
type HashOrNumber struct {
	Hash   types.Hash // If non-zero, look up by hash.
	Number uint64     // If Hash is zero, look up by number.
}

// IsHash returns true if the request specifies a hash rather than a number.
func (hon *HashOrNumber) IsHash() bool {
	return !hon.Hash.IsZero()
}

// GetBlockHeadersRequest represents a request for block headers.
type GetBlockHeadersRequest struct {
	Origin  HashOrNumber // Block from which to retrieve headers.
	Amount  uint64       // Maximum number of headers to retrieve.
	Skip    uint64       // Blocks to skip between consecutive headers.
	Reverse bool         // Whether to query in reverse direction.
}

// GetBlockHeadersPacket wraps a GetBlockHeadersRequest with a request ID.
type GetBlockHeadersPacket struct {
	RequestID uint64
	Request   GetBlockHeadersRequest
}

// BlockHeadersPacket is the response to GetBlockHeadersRequest.
type BlockHeadersPacket struct {
	RequestID uint64
	Headers   []*types.Header
}

// GetBlockBodiesRequest is a list of block hashes for which to retrieve bodies.
type GetBlockBodiesRequest []types.Hash

// GetBlockBodiesPacket wraps a GetBlockBodiesRequest with a request ID.
type GetBlockBodiesPacket struct {
	RequestID uint64
	Hashes    GetBlockBodiesRequest
}

// BlockBody represents the body of a single block in a response.
type BlockBody struct {
	Transactions []*types.Transaction
	Uncles       []*types.Header
	Withdrawals  []*types.Withdrawal
}

// BlockBodiesPacket is the response to GetBlockBodiesRequest.
type BlockBodiesPacket struct {
	RequestID uint64
	Bodies    []*BlockBody
}

// NewBlockData is the data propagated when a new block is announced.
type NewBlockData struct {
	Block *types.Block
	TD    *big.Int
}

// GetReceiptsRequest is a list of block hashes for which to retrieve receipts.
type GetReceiptsRequest []types.Hash

// GetReceiptsPacket wraps a GetReceiptsRequest with a request ID.
type GetReceiptsPacket struct {
	RequestID uint64
	Hashes    GetReceiptsRequest
}

// ReceiptsPacket is the response to GetReceiptsRequest.
type ReceiptsPacket struct {
	RequestID uint64
	Receipts  [][]*types.Receipt
}

// NewPooledTransactionHashesPacket68 represents the eth/68 announcement of
// transaction hashes along with their types and sizes.
type NewPooledTransactionHashesPacket68 struct {
	Types  []byte
	Sizes  []uint32
	Hashes []types.Hash
}

// GetPooledTransactionsRequest is a list of transaction hashes to retrieve
// from the remote peer's transaction pool.
type GetPooledTransactionsRequest []types.Hash

// GetPooledTransactionsPacket wraps a GetPooledTransactionsRequest with a request ID.
type GetPooledTransactionsPacket struct {
	RequestID uint64
	Hashes    GetPooledTransactionsRequest
}

// PooledTransactionsPacket is the response to GetPooledTransactionsRequest.
type PooledTransactionsPacket struct {
	RequestID    uint64
	Transactions []*types.Transaction
}
