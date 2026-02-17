package eth

import (
	"fmt"
	"math/big"
	"sync/atomic"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/p2p"
)

// EthPeer wraps a p2p.Peer with eth protocol-specific send/request methods.
type EthPeer struct {
	peer      *p2p.Peer
	transport p2p.Transport
	reqID     atomic.Uint64
}

// NewEthPeer creates a new EthPeer wrapping the given p2p peer and transport.
func NewEthPeer(peer *p2p.Peer, t p2p.Transport) *EthPeer {
	return &EthPeer{
		peer:      peer,
		transport: t,
	}
}

// Peer returns the underlying p2p.Peer.
func (ep *EthPeer) Peer() *p2p.Peer { return ep.peer }

// ID returns the peer's unique identifier.
func (ep *EthPeer) ID() string { return ep.peer.ID() }

// nextRequestID returns a monotonically increasing request ID.
func (ep *EthPeer) nextRequestID() uint64 {
	return ep.reqID.Add(1)
}

// sendMessage encodes val and sends it with the given message code.
func (ep *EthPeer) sendMessage(code uint64, val interface{}) error {
	msg, err := p2p.EncodeMessage(code, val)
	if err != nil {
		return fmt.Errorf("eth: encode %s: %w", p2p.MessageName(code), err)
	}
	return ep.transport.WriteMsg(p2p.Msg{
		Code:    msg.Code,
		Size:    msg.Size,
		Payload: msg.Payload,
	})
}

// SendStatus sends a status message to the remote peer.
func (ep *EthPeer) SendStatus(status *p2p.StatusData) error {
	return ep.sendMessage(p2p.StatusMsg, status)
}

// SendBlockHeaders sends block headers as a response to a headers request.
func (ep *EthPeer) SendBlockHeaders(requestID uint64, headers []*types.Header) error {
	return ep.sendMessage(p2p.BlockHeadersMsg, &p2p.BlockHeadersPacket{
		RequestID: requestID,
		Headers:   headers,
	})
}

// SendBlockBodies sends block bodies as a response to a bodies request.
func (ep *EthPeer) SendBlockBodies(requestID uint64, bodies []*p2p.BlockBody) error {
	return ep.sendMessage(p2p.BlockBodiesMsg, &p2p.BlockBodiesPacket{
		RequestID: requestID,
		Bodies:    bodies,
	})
}

// RequestBlockHeaders sends a request for block headers to the peer.
func (ep *EthPeer) RequestBlockHeaders(origin p2p.HashOrNumber, amount, skip uint64, reverse bool) (uint64, error) {
	reqID := ep.nextRequestID()
	err := ep.sendMessage(p2p.GetBlockHeadersMsg, &p2p.GetBlockHeadersPacket{
		RequestID: reqID,
		Request: p2p.GetBlockHeadersRequest{
			Origin:  origin,
			Amount:  amount,
			Skip:    skip,
			Reverse: reverse,
		},
	})
	return reqID, err
}

// RequestBlockBodies sends a request for block bodies to the peer.
func (ep *EthPeer) RequestBlockBodies(hashes []types.Hash) (uint64, error) {
	reqID := ep.nextRequestID()
	err := ep.sendMessage(p2p.GetBlockBodiesMsg, &p2p.GetBlockBodiesPacket{
		RequestID: reqID,
		Hashes:    p2p.GetBlockBodiesRequest(hashes),
	})
	return reqID, err
}

// SendTransactions sends a batch of transactions to the peer.
func (ep *EthPeer) SendTransactions(txs []*types.Transaction) error {
	msg, err := encodeTransactions(txs)
	if err != nil {
		return fmt.Errorf("eth: encode transactions: %w", err)
	}
	return ep.transport.WriteMsg(msg)
}

// SendNewBlockHashes announces new block hashes to the peer.
func (ep *EthPeer) SendNewBlockHashes(entries []p2p.NewBlockHashesEntry) error {
	return ep.sendMessage(p2p.NewBlockHashesMsg, entries)
}

// SendNewBlock sends a full new block announcement to the peer.
func (ep *EthPeer) SendNewBlock(block *types.Block, td *big.Int) error {
	msg, err := encodeNewBlock(&p2p.NewBlockData{Block: block, TD: td})
	if err != nil {
		return fmt.Errorf("eth: encode new block: %w", err)
	}
	return ep.transport.WriteMsg(msg)
}

// Handshake performs the eth protocol handshake by exchanging status messages.
// It sends our status and reads the remote status, updating the peer's head.
func (ep *EthPeer) Handshake(local *p2p.StatusData) (*p2p.StatusData, error) {
	// Send our status.
	if err := ep.SendStatus(local); err != nil {
		return nil, fmt.Errorf("eth: send status: %w", err)
	}

	// Read remote status.
	msg, err := ep.transport.ReadMsg()
	if err != nil {
		return nil, fmt.Errorf("eth: read status: %w", err)
	}
	if msg.Code != p2p.StatusMsg {
		return nil, fmt.Errorf("eth: expected status (0x%02x), got 0x%02x", p2p.StatusMsg, msg.Code)
	}

	var remote p2p.StatusData
	if err := p2p.DecodeMessage(p2p.Message{Code: msg.Code, Size: msg.Size, Payload: msg.Payload}, &remote); err != nil {
		return nil, fmt.Errorf("eth: decode remote status: %w", err)
	}

	// Validate compatibility.
	if remote.NetworkID != local.NetworkID {
		return nil, fmt.Errorf("eth: network ID mismatch: local %d, remote %d", local.NetworkID, remote.NetworkID)
	}
	if remote.Genesis != local.Genesis {
		return nil, fmt.Errorf("eth: genesis mismatch: local %s, remote %s", local.Genesis.Hex(), remote.Genesis.Hex())
	}

	// Update peer head info.
	ep.peer.SetHead(remote.Head, remote.TD)
	ep.peer.SetVersion(remote.ProtocolVersion)

	return &remote, nil
}
