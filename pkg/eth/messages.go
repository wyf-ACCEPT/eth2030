package eth

import (
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/p2p"
	"github.com/eth2028/eth2028/rlp"
)

// ETH/68 message code constants. These mirror the canonical definitions in
// the p2p package but are re-exported here for ergonomic use by callers
// that only depend on the eth package.
const (
	MsgStatus                    uint64 = 0x00
	MsgNewBlockHashes            uint64 = 0x01
	MsgTransactions              uint64 = 0x02
	MsgGetBlockHeaders           uint64 = 0x03
	MsgBlockHeaders              uint64 = 0x04
	MsgGetBlockBodies            uint64 = 0x05
	MsgBlockBodies               uint64 = 0x06
	MsgNewBlock                  uint64 = 0x07
	MsgNewPooledTransactionHashes uint64 = 0x08
	MsgGetPooledTransactions     uint64 = 0x09
	MsgPooledTransactions        uint64 = 0x0a
)

// StatusMessage is the eth/68 status handshake message. It is exchanged once
// on connection establishment to verify protocol compatibility.
type StatusMessage struct {
	ProtocolVersion uint32
	NetworkID       uint64
	TD              *big.Int
	BestHash        types.Hash
	Genesis         types.Hash
	ForkID          p2p.ForkID
}

// NewBlockHashesMessage announces new block hashes available on a peer.
type NewBlockHashesMessage struct {
	Entries []BlockHashEntry
}

// BlockHashEntry pairs a block hash with its number.
type BlockHashEntry struct {
	Hash   types.Hash
	Number uint64
}

// TransactionsMessage carries a list of full transactions propagated between peers.
type TransactionsMessage struct {
	Transactions []*types.Transaction
}

// GetBlockHeadersMessage requests block headers by origin, amount, skip, and direction.
type GetBlockHeadersMessage struct {
	Origin  p2p.HashOrNumber
	Amount  uint64
	Skip    uint64
	Reverse bool
}

// BlockHeadersMessage is a response containing requested block headers.
type BlockHeadersMessage struct {
	Headers []*types.Header
}

// GetBlockBodiesMessage requests block bodies for the specified hashes.
type GetBlockBodiesMessage struct {
	Hashes []types.Hash
}

// BlockBodyData holds the transactions and uncles of a single block.
type BlockBodyData struct {
	Transactions []*types.Transaction
	Uncles       []*types.Header
}

// BlockBodiesMessage is a response containing requested block bodies.
type BlockBodiesMessage struct {
	Bodies []BlockBodyData
}

// NewBlockMessage announces a newly mined block along with the total difficulty.
type NewBlockMessage struct {
	Block *types.Block
	TD    *big.Int
}

// NewPooledTransactionHashesMsg68 announces new transaction hashes along with
// their types and sizes, as defined in the eth/68 protocol.
type NewPooledTxHashesMsg68 struct {
	Types  []byte
	Sizes  []uint32
	Hashes []types.Hash
}

// GetPooledTransactionsMessage requests specific transactions from a peer's pool.
type GetPooledTransactionsMessage struct {
	Hashes []types.Hash
}

// PooledTransactionsMessage is a response containing pooled transactions.
type PooledTransactionsMessage struct {
	Transactions []*types.Transaction
}

// GetReceiptsMessage requests receipts for the specified block hashes.
type GetReceiptsMessage struct {
	Hashes []types.Hash
}

// ReceiptsMessage is a response containing receipts grouped by block.
type ReceiptsMessage struct {
	Receipts [][]*types.Receipt
}

// EncodeMsg encodes a message struct for the given code into RLP bytes.
// The caller should provide the correct message type for the given code.
func EncodeMsg(code uint64, msg interface{}) ([]byte, error) {
	switch code {
	case MsgStatus:
		sm, ok := msg.(*StatusMessage)
		if !ok {
			return nil, fmt.Errorf("eth: EncodeMsg: expected *StatusMessage for code 0x%02x", code)
		}
		return rlp.EncodeToBytes(sm)

	case MsgNewBlockHashes:
		nm, ok := msg.(*NewBlockHashesMessage)
		if !ok {
			return nil, fmt.Errorf("eth: EncodeMsg: expected *NewBlockHashesMessage for code 0x%02x", code)
		}
		return rlp.EncodeToBytes(nm.Entries)

	case MsgTransactions:
		tm, ok := msg.(*TransactionsMessage)
		if !ok {
			return nil, fmt.Errorf("eth: EncodeMsg: expected *TransactionsMessage for code 0x%02x", code)
		}
		return rlp.EncodeToBytes(tm.Transactions)

	case MsgGetBlockHeaders:
		gm, ok := msg.(*GetBlockHeadersMessage)
		if !ok {
			return nil, fmt.Errorf("eth: EncodeMsg: expected *GetBlockHeadersMessage for code 0x%02x", code)
		}
		return rlp.EncodeToBytes(gm)

	case MsgBlockHeaders:
		bm, ok := msg.(*BlockHeadersMessage)
		if !ok {
			return nil, fmt.Errorf("eth: EncodeMsg: expected *BlockHeadersMessage for code 0x%02x", code)
		}
		return rlp.EncodeToBytes(bm.Headers)

	case MsgGetBlockBodies:
		gm, ok := msg.(*GetBlockBodiesMessage)
		if !ok {
			return nil, fmt.Errorf("eth: EncodeMsg: expected *GetBlockBodiesMessage for code 0x%02x", code)
		}
		return rlp.EncodeToBytes(gm.Hashes)

	case MsgBlockBodies:
		bm, ok := msg.(*BlockBodiesMessage)
		if !ok {
			return nil, fmt.Errorf("eth: EncodeMsg: expected *BlockBodiesMessage for code 0x%02x", code)
		}
		return rlp.EncodeToBytes(bm.Bodies)

	case MsgNewBlock:
		nm, ok := msg.(*NewBlockMessage)
		if !ok {
			return nil, fmt.Errorf("eth: EncodeMsg: expected *NewBlockMessage for code 0x%02x", code)
		}
		return encodeNewBlockMsg(nm)

	case MsgNewPooledTransactionHashes:
		pm, ok := msg.(*NewPooledTxHashesMsg68)
		if !ok {
			return nil, fmt.Errorf("eth: EncodeMsg: expected *NewPooledTxHashesMsg68 for code 0x%02x", code)
		}
		return rlp.EncodeToBytes(pm)

	case MsgGetPooledTransactions:
		gm, ok := msg.(*GetPooledTransactionsMessage)
		if !ok {
			return nil, fmt.Errorf("eth: EncodeMsg: expected *GetPooledTransactionsMessage for code 0x%02x", code)
		}
		return rlp.EncodeToBytes(gm.Hashes)

	case MsgPooledTransactions:
		pm, ok := msg.(*PooledTransactionsMessage)
		if !ok {
			return nil, fmt.Errorf("eth: EncodeMsg: expected *PooledTransactionsMessage for code 0x%02x", code)
		}
		return rlp.EncodeToBytes(pm.Transactions)

	default:
		return nil, fmt.Errorf("eth: EncodeMsg: unknown message code 0x%02x", code)
	}
}

// DecodeMsg decodes RLP bytes into the appropriate message struct for the
// given code. It returns the decoded message or an error.
func DecodeMsg(code uint64, data []byte) (interface{}, error) {
	switch code {
	case MsgStatus:
		var m StatusMessage
		if err := rlp.DecodeBytes(data, &m); err != nil {
			return nil, fmt.Errorf("eth: DecodeMsg Status: %w", err)
		}
		return &m, nil

	case MsgNewBlockHashes:
		var entries []BlockHashEntry
		if err := rlp.DecodeBytes(data, &entries); err != nil {
			return nil, fmt.Errorf("eth: DecodeMsg NewBlockHashes: %w", err)
		}
		return &NewBlockHashesMessage{Entries: entries}, nil

	case MsgTransactions:
		var txs []*types.Transaction
		if err := rlp.DecodeBytes(data, &txs); err != nil {
			return nil, fmt.Errorf("eth: DecodeMsg Transactions: %w", err)
		}
		return &TransactionsMessage{Transactions: txs}, nil

	case MsgGetBlockHeaders:
		var m GetBlockHeadersMessage
		if err := rlp.DecodeBytes(data, &m); err != nil {
			return nil, fmt.Errorf("eth: DecodeMsg GetBlockHeaders: %w", err)
		}
		return &m, nil

	case MsgBlockHeaders:
		var headers []*types.Header
		if err := rlp.DecodeBytes(data, &headers); err != nil {
			return nil, fmt.Errorf("eth: DecodeMsg BlockHeaders: %w", err)
		}
		return &BlockHeadersMessage{Headers: headers}, nil

	case MsgGetBlockBodies:
		var hashes []types.Hash
		if err := rlp.DecodeBytes(data, &hashes); err != nil {
			return nil, fmt.Errorf("eth: DecodeMsg GetBlockBodies: %w", err)
		}
		return &GetBlockBodiesMessage{Hashes: hashes}, nil

	case MsgBlockBodies:
		var bodies []BlockBodyData
		if err := rlp.DecodeBytes(data, &bodies); err != nil {
			return nil, fmt.Errorf("eth: DecodeMsg BlockBodies: %w", err)
		}
		return &BlockBodiesMessage{Bodies: bodies}, nil

	case MsgNewBlock:
		return nil, fmt.Errorf("eth: DecodeMsg NewBlock requires special handling; use decodeNewBlock")

	case MsgNewPooledTransactionHashes:
		var m NewPooledTxHashesMsg68
		if err := rlp.DecodeBytes(data, &m); err != nil {
			return nil, fmt.Errorf("eth: DecodeMsg NewPooledTransactionHashes: %w", err)
		}
		return &m, nil

	case MsgGetPooledTransactions:
		var hashes []types.Hash
		if err := rlp.DecodeBytes(data, &hashes); err != nil {
			return nil, fmt.Errorf("eth: DecodeMsg GetPooledTransactions: %w", err)
		}
		return &GetPooledTransactionsMessage{Hashes: hashes}, nil

	case MsgPooledTransactions:
		var txs []*types.Transaction
		if err := rlp.DecodeBytes(data, &txs); err != nil {
			return nil, fmt.Errorf("eth: DecodeMsg PooledTransactions: %w", err)
		}
		return &PooledTransactionsMessage{Transactions: txs}, nil

	default:
		return nil, fmt.Errorf("eth: DecodeMsg: unknown message code 0x%02x", code)
	}
}

// encodeNewBlockMsg encodes a NewBlockMessage by encoding the block and TD
// as a two-element RLP list.
func encodeNewBlockMsg(msg *NewBlockMessage) ([]byte, error) {
	blockEnc, err := msg.Block.EncodeRLP()
	if err != nil {
		return nil, fmt.Errorf("eth: encode block: %w", err)
	}
	td := msg.TD
	if td == nil {
		td = new(big.Int)
	}
	tdEnc, err := rlp.EncodeToBytes(td)
	if err != nil {
		return nil, fmt.Errorf("eth: encode td: %w", err)
	}
	var payload []byte
	payload = append(payload, blockEnc...)
	payload = append(payload, tdEnc...)
	return rlp.WrapList(payload), nil
}

// MsgCodeName returns a human-readable name for an ETH/68 message code.
func MsgCodeName(code uint64) string {
	switch code {
	case MsgStatus:
		return "Status"
	case MsgNewBlockHashes:
		return "NewBlockHashes"
	case MsgTransactions:
		return "Transactions"
	case MsgGetBlockHeaders:
		return "GetBlockHeaders"
	case MsgBlockHeaders:
		return "BlockHeaders"
	case MsgGetBlockBodies:
		return "GetBlockBodies"
	case MsgBlockBodies:
		return "BlockBodies"
	case MsgNewBlock:
		return "NewBlock"
	case MsgNewPooledTransactionHashes:
		return "NewPooledTransactionHashes"
	case MsgGetPooledTransactions:
		return "GetPooledTransactions"
	case MsgPooledTransactions:
		return "PooledTransactions"
	default:
		return fmt.Sprintf("Unknown(0x%02x)", code)
	}
}
