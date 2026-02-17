package eth

import (
	"errors"
	"fmt"
	"log"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/p2p"
	ethsync "github.com/eth2028/eth2028/sync"
)

var (
	ErrIncompatibleVersion = errors.New("eth: incompatible protocol version")
	ErrNetworkIDMismatch   = errors.New("eth: network ID mismatch")
	ErrGenesisMismatch     = errors.New("eth: genesis block mismatch")
)

// SyncNotifier is an optional callback interface for triggering sync on
// new block announcements.
type SyncNotifier interface {
	OnNewBlock(peerID string, blockNum uint64)
}

// Handler implements the eth/68 protocol message handling. It connects
// incoming P2P messages to the blockchain and transaction pool.
type Handler struct {
	chain     Blockchain
	txPool    TxPool
	networkID uint64
	peers     *p2p.PeerSet

	// Optional sync notifier triggered by NewBlock messages.
	syncNotifier SyncNotifier

	// Optional downloader for responding to header/body requests
	// received as responses (from sync).
	downloader *ethsync.Downloader
}

// NewHandler creates a new eth protocol handler.
func NewHandler(chain Blockchain, txPool TxPool, networkID uint64) *Handler {
	return &Handler{
		chain:     chain,
		txPool:    txPool,
		networkID: networkID,
		peers:     p2p.NewPeerSet(),
	}
}

// SetSyncNotifier configures an optional callback for new block events.
func (h *Handler) SetSyncNotifier(sn SyncNotifier) {
	h.syncNotifier = sn
}

// SetDownloader configures the downloader for the handler.
func (h *Handler) SetDownloader(dl *ethsync.Downloader) {
	h.downloader = dl
}

// Peers returns the handler's peer set.
func (h *Handler) Peers() *p2p.PeerSet {
	return h.peers
}

// Chain returns the handler's blockchain.
func (h *Handler) Chain() Blockchain {
	return h.chain
}

// Protocol returns a p2p.Protocol that can be registered with the P2P server.
func (h *Handler) Protocol() p2p.Protocol {
	return p2p.Protocol{
		Name:    "eth",
		Version: ETH68,
		Length:  13,
		Run:     h.runPeer,
	}
}

// runPeer is called by the P2P server for each connected peer.
// It performs the handshake and then enters the message loop.
func (h *Handler) runPeer(peer *p2p.Peer, t p2p.Transport) error {
	ethPeer := NewEthPeer(peer, t)

	// Build local status.
	head := h.chain.CurrentBlock()
	genesis := h.chain.Genesis()
	status := &p2p.StatusData{
		ProtocolVersion: ETH68,
		NetworkID:       h.networkID,
		TD:              head.Difficulty(),
		Head:            head.Hash(),
		Genesis:         genesis.Hash(),
	}

	// Handshake.
	if _, err := ethPeer.Handshake(status); err != nil {
		return err
	}

	// Register peer.
	if err := h.peers.Register(peer); err != nil {
		return err
	}
	defer h.peers.Unregister(peer.ID())

	// Message loop.
	return h.handleMessages(ethPeer)
}

// handleMessages reads and dispatches messages from the peer.
func (h *Handler) handleMessages(ep *EthPeer) error {
	for {
		msg, err := ep.transport.ReadMsg()
		if err != nil {
			return err
		}
		if err := h.handleMsg(ep, msg); err != nil {
			return err
		}
	}
}

// HandleMsg dispatches a single message to the appropriate handler.
// Exported for testing.
func (h *Handler) HandleMsg(ep *EthPeer, msg p2p.Msg) error {
	return h.handleMsg(ep, msg)
}

func (h *Handler) handleMsg(ep *EthPeer, msg p2p.Msg) error {
	switch msg.Code {
	case p2p.StatusMsg:
		return fmt.Errorf("eth: unexpected status message after handshake")

	case p2p.GetBlockHeadersMsg:
		return h.handleGetBlockHeaders(ep, msg)

	case p2p.BlockHeadersMsg:
		return h.handleBlockHeaders(ep, msg)

	case p2p.GetBlockBodiesMsg:
		return h.handleGetBlockBodies(ep, msg)

	case p2p.BlockBodiesMsg:
		return h.handleBlockBodies(ep, msg)

	case p2p.NewBlockHashesMsg:
		return h.handleNewBlockHashes(ep, msg)

	case p2p.NewBlockMsg:
		return h.handleNewBlock(ep, msg)

	case p2p.TransactionsMsg:
		return h.handleTransactions(ep, msg)

	default:
		log.Printf("eth: ignoring unknown message code 0x%02x from %s", msg.Code, ep.ID())
		return nil
	}
}

// HandleGetBlockHeaders responds to a header request by looking up blocks
// in the local chain and returning their headers. Exported for use by
// sync adapters.
func (h *Handler) HandleGetBlockHeaders(origin p2p.HashOrNumber, amount, skip uint64, reverse bool) []*types.Header {
	return h.collectHeaders(p2p.GetBlockHeadersRequest{
		Origin:  origin,
		Amount:  amount,
		Skip:    skip,
		Reverse: reverse,
	})
}

// HandleGetBlockBodies retrieves block bodies for the given hashes from
// the local chain. Exported for use by sync adapters.
func (h *Handler) HandleGetBlockBodies(hashes []types.Hash) []*types.Body {
	var bodies []*types.Body
	count := len(hashes)
	if count > MaxBodies {
		count = MaxBodies
	}

	for _, hash := range hashes[:count] {
		block := h.chain.GetBlock(hash)
		if block == nil {
			// Return an empty body to keep index alignment.
			bodies = append(bodies, &types.Body{})
			continue
		}
		bodies = append(bodies, block.Body())
	}
	return bodies
}

// HandleNewBlock processes a new block announcement and optionally triggers
// a sync if the announced block is higher than our head. Exported for
// testing and external use.
func (h *Handler) HandleNewBlock(peerID string, block *types.Block, td uint64) {
	localHead := h.chain.CurrentBlock().NumberU64()
	remoteNum := block.NumberU64()

	if remoteNum > localHead && h.syncNotifier != nil {
		h.syncNotifier.OnNewBlock(peerID, remoteNum)
	}
}

// handleGetBlockHeaders responds to a block headers request.
func (h *Handler) handleGetBlockHeaders(ep *EthPeer, msg p2p.Msg) error {
	var req p2p.GetBlockHeadersPacket
	if err := decodeMsg(msg, &req); err != nil {
		return err
	}

	headers := h.collectHeaders(req.Request)
	return ep.SendBlockHeaders(req.RequestID, headers)
}

// collectHeaders gathers headers based on the request parameters.
func (h *Handler) collectHeaders(req p2p.GetBlockHeadersRequest) []*types.Header {
	var headers []*types.Header

	// Resolve the starting block.
	var origin *types.Block
	if req.Origin.IsHash() {
		origin = h.chain.GetBlock(req.Origin.Hash)
	} else {
		origin = h.chain.GetBlockByNumber(req.Origin.Number)
	}
	if origin == nil {
		return nil
	}

	headers = append(headers, origin.Header())

	// Collect subsequent headers.
	amount := req.Amount
	if amount > MaxHeaders {
		amount = MaxHeaders
	}

	num := origin.NumberU64()
	for i := uint64(1); i < amount; i++ {
		if req.Reverse {
			if num < 1+req.Skip {
				break
			}
			num -= 1 + req.Skip
		} else {
			num += 1 + req.Skip
		}

		block := h.chain.GetBlockByNumber(num)
		if block == nil {
			break
		}
		headers = append(headers, block.Header())
	}
	return headers
}

// handleBlockHeaders processes a received block headers response.
func (h *Handler) handleBlockHeaders(ep *EthPeer, msg p2p.Msg) error {
	var pkt p2p.BlockHeadersPacket
	if err := decodeMsg(msg, &pkt); err != nil {
		return err
	}
	// For now, just log receipt. A sync manager would process these.
	log.Printf("eth: received %d headers from %s (req %d)", len(pkt.Headers), ep.ID(), pkt.RequestID)
	return nil
}

// handleGetBlockBodies responds to a block bodies request.
func (h *Handler) handleGetBlockBodies(ep *EthPeer, msg p2p.Msg) error {
	var req p2p.GetBlockBodiesPacket
	if err := decodeMsg(msg, &req); err != nil {
		return err
	}

	var bodies []*p2p.BlockBody
	count := len(req.Hashes)
	if count > MaxBodies {
		count = MaxBodies
	}

	for _, hash := range req.Hashes[:count] {
		block := h.chain.GetBlock(hash)
		if block == nil {
			continue
		}
		bodies = append(bodies, &p2p.BlockBody{
			Transactions: block.Transactions(),
			Uncles:       block.Uncles(),
			Withdrawals:  block.Withdrawals(),
		})
	}
	return ep.SendBlockBodies(req.RequestID, bodies)
}

// handleBlockBodies processes a received block bodies response.
func (h *Handler) handleBlockBodies(ep *EthPeer, msg p2p.Msg) error {
	var pkt p2p.BlockBodiesPacket
	if err := decodeMsg(msg, &pkt); err != nil {
		return err
	}
	log.Printf("eth: received %d bodies from %s (req %d)", len(pkt.Bodies), ep.ID(), pkt.RequestID)
	return nil
}

// handleNewBlockHashes processes new block hash announcements.
func (h *Handler) handleNewBlockHashes(ep *EthPeer, msg p2p.Msg) error {
	var entries []p2p.NewBlockHashesEntry
	if err := decodeMsg(msg, &entries); err != nil {
		return err
	}

	// Mark blocks we don't have for fetching.
	for _, entry := range entries {
		if !h.chain.HasBlock(entry.Hash) {
			log.Printf("eth: new block hash %s (#%d) from %s", entry.Hash.Hex(), entry.Number, ep.ID())
		}
	}
	return nil
}

// handleNewBlock processes a full new block announcement.
func (h *Handler) handleNewBlock(ep *EthPeer, msg p2p.Msg) error {
	data, err := decodeNewBlock(msg)
	if err != nil {
		return err
	}

	if data.Block == nil {
		return fmt.Errorf("eth: nil block in NewBlock from %s", ep.ID())
	}

	// Update peer's head.
	ep.peer.SetHead(data.Block.Hash(), data.TD)

	// Notify sync manager if the block is higher than our head.
	h.HandleNewBlock(ep.ID(), data.Block, data.Block.NumberU64())

	// Try to insert the block.
	if err := h.chain.InsertBlock(data.Block); err != nil {
		log.Printf("eth: failed to insert block #%d from %s: %v", data.Block.NumberU64(), ep.ID(), err)
		return nil // don't disconnect on insert failure
	}

	log.Printf("eth: imported block #%d from %s", data.Block.NumberU64(), ep.ID())
	return nil
}

// handleTransactions processes received transactions and adds them to the pool.
func (h *Handler) handleTransactions(ep *EthPeer, msg p2p.Msg) error {
	txs, err := decodeTransactions(msg)
	if err != nil {
		return err
	}

	for _, tx := range txs {
		if err := h.txPool.AddRemote(tx); err != nil {
			// Non-fatal: log and continue with remaining transactions.
			log.Printf("eth: rejected tx %s from %s: %v", tx.Hash().Hex(), ep.ID(), err)
		}
	}
	return nil
}

// PeerFetcher adapts the eth handler to implement the sync.HeaderFetcher
// and sync.BodyFetcher interfaces, enabling the sync package to fetch
// data from the local chain (for testing) or remote peers.
type PeerFetcher struct {
	chain Blockchain
}

// NewPeerFetcher creates a PeerFetcher backed by the given blockchain.
func NewPeerFetcher(chain Blockchain) *PeerFetcher {
	return &PeerFetcher{chain: chain}
}

// FetchHeaders fetches headers from the local chain starting at `from`.
func (pf *PeerFetcher) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	var headers []*types.Header
	for i := 0; i < count; i++ {
		num := from + uint64(i)
		block := pf.chain.GetBlockByNumber(num)
		if block == nil {
			break
		}
		headers = append(headers, block.Header())
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("no headers found starting at %d", from)
	}
	return headers, nil
}

// FetchBodies fetches block bodies from the local chain by hash.
func (pf *PeerFetcher) FetchBodies(hashes []types.Hash) ([]*types.Body, error) {
	bodies := make([]*types.Body, 0, len(hashes))
	for _, hash := range hashes {
		block := pf.chain.GetBlock(hash)
		if block == nil {
			// Return empty body to keep alignment.
			bodies = append(bodies, &types.Body{})
			continue
		}
		bodies = append(bodies, block.Body())
	}
	return bodies, nil
}

// decodeMsg is a helper that converts a p2p.Msg to p2p.Message and decodes.
func decodeMsg(msg p2p.Msg, val interface{}) error {
	return p2p.DecodeMessage(p2p.Message{
		Code:    msg.Code,
		Size:    msg.Size,
		Payload: msg.Payload,
	}, val)
}
