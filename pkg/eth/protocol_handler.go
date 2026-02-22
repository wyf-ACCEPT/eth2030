// protocol_handler.go implements ProtocolHandler, which manages ETH/68-72
// wire protocol message processing. It extends the core Handler with
// per-peer sync state tracking, pooled transaction handling, and structured
// message dispatch for all supported ETH protocol messages.
package eth

import (
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Protocol handler errors.
var (
	ErrProtoHandlerStopped    = errors.New("protocol handler: stopped")
	ErrProtoHandlerNoPeer     = errors.New("protocol handler: peer not found")
	ErrProtoHandlerDuplicate  = errors.New("protocol handler: duplicate block hash")
	ErrProtoHandlerInvalidMsg = errors.New("protocol handler: invalid message")
	ErrProtoHandlerTooMany    = errors.New("protocol handler: too many items")
)

// MaxNewBlockHashes is the maximum number of block hash announcements in a
// single NewBlockHashes message.
const MaxNewBlockHashes = 256

// MaxTransactionsPerMsg is the maximum number of transactions per message.
const MaxTransactionsPerMsg = 4096

// MaxPooledTxHashes is the maximum number of pooled transaction hashes
// in a request/response.
const MaxPooledTxHashes = 4096

// PeerSyncState tracks the sync state of a remote peer.
type PeerSyncState struct {
	PeerID      string
	HeadHash    types.Hash
	HeadNumber  uint64
	TD          *big.Int
	Version     uint32
	LastSeen    time.Time
	BlocksKnown map[types.Hash]bool
}

// NewPeerSyncState creates a new peer sync state.
func NewPeerSyncState(peerID string, version uint32) *PeerSyncState {
	return &PeerSyncState{
		PeerID:      peerID,
		Version:     version,
		TD:          new(big.Int),
		LastSeen:    time.Now(),
		BlocksKnown: make(map[types.Hash]bool),
	}
}

// SetHead updates the peer's head block information.
func (ps *PeerSyncState) SetHead(hash types.Hash, number uint64, td *big.Int) {
	ps.HeadHash = hash
	ps.HeadNumber = number
	if td != nil {
		ps.TD = new(big.Int).Set(td)
	}
	ps.LastSeen = time.Now()
}

// MarkBlockKnown records that this peer knows about a specific block.
func (ps *PeerSyncState) MarkBlockKnown(hash types.Hash) {
	ps.BlocksKnown[hash] = true
}

// IsBlockKnown returns true if the peer already knows about this block.
func (ps *PeerSyncState) IsBlockKnown(hash types.Hash) bool {
	return ps.BlocksKnown[hash]
}

// NewBlockHashResult holds the result of processing a NewBlockHashes message.
type NewBlockHashResult struct {
	Entries []BlockHashEntry
	Unknown []BlockHashEntry // entries for blocks we do not have
}

// TransactionResult holds the result of processing a Transactions message.
type TransactionResult struct {
	Added    int
	Rejected int
	Errors   []error
}

// PooledTxResult holds the result of processing pooled transactions.
type PooledTxResult struct {
	Requested int
	Received  int
	Missing   []types.Hash
}

// HeadersResult holds the result of responding to a GetBlockHeaders request.
type HeadersResult struct {
	Headers []*types.Header
	Count   int
}

// BodiesResult holds the result of responding to a GetBlockBodies request.
type BodiesResult struct {
	Bodies []*types.Body
	Count  int
}

// ProtocolHandler processes ETH wire protocol messages with per-peer
// sync state tracking.
type ProtocolHandler struct {
	mu         sync.RWMutex
	chain      Blockchain
	txPool     TxPool
	networkID  uint64
	peerStates map[string]*PeerSyncState
	stopped    bool
}

// NewProtocolHandler creates a new protocol handler.
func NewProtocolHandler(chain Blockchain, txPool TxPool, networkID uint64) *ProtocolHandler {
	return &ProtocolHandler{
		chain:      chain,
		txPool:     txPool,
		networkID:  networkID,
		peerStates: make(map[string]*PeerSyncState),
	}
}

// RegisterPeer adds a peer's sync state to the tracker.
func (ph *ProtocolHandler) RegisterPeer(peerID string, version uint32) error {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	if ph.stopped {
		return ErrProtoHandlerStopped
	}
	ph.peerStates[peerID] = NewPeerSyncState(peerID, version)
	return nil
}

// UnregisterPeer removes a peer's sync state.
func (ph *ProtocolHandler) UnregisterPeer(peerID string) {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	delete(ph.peerStates, peerID)
}

// GetPeerState returns the sync state for a peer, or nil if not found.
func (ph *ProtocolHandler) GetPeerState(peerID string) *PeerSyncState {
	ph.mu.RLock()
	defer ph.mu.RUnlock()
	return ph.peerStates[peerID]
}

// PeerCount returns the number of tracked peers.
func (ph *ProtocolHandler) PeerCount() int {
	ph.mu.RLock()
	defer ph.mu.RUnlock()
	return len(ph.peerStates)
}

// HandleNewBlockHashes processes incoming block hash announcements.
// Returns the set of unknown hashes that should be fetched.
func (ph *ProtocolHandler) HandleNewBlockHashes(peerID string, entries []BlockHashEntry) (*NewBlockHashResult, error) {
	if len(entries) > MaxNewBlockHashes {
		return nil, ErrProtoHandlerTooMany
	}

	ph.mu.Lock()
	state := ph.peerStates[peerID]
	ph.mu.Unlock()

	result := &NewBlockHashResult{
		Entries: entries,
	}

	for _, entry := range entries {
		if state != nil {
			state.MarkBlockKnown(entry.Hash)
		}
		if !ph.chain.HasBlock(entry.Hash) {
			result.Unknown = append(result.Unknown, entry)
		}
	}
	return result, nil
}

// HandleNewBlock processes a new block announcement from a peer.
// It updates the peer's sync state and attempts to insert the block.
func (ph *ProtocolHandler) HandleNewBlock(peerID string, block *types.Block, td *big.Int) error {
	if block == nil {
		return ErrProtoHandlerInvalidMsg
	}

	ph.mu.Lock()
	state := ph.peerStates[peerID]
	ph.mu.Unlock()

	if state != nil {
		state.SetHead(block.Hash(), block.NumberU64(), td)
		state.MarkBlockKnown(block.Hash())
	}

	return ph.chain.InsertBlock(block)
}

// HandleTransactions processes incoming transactions from a peer,
// adding them to the transaction pool.
func (ph *ProtocolHandler) HandleTransactions(peerID string, txs []*types.Transaction) *TransactionResult {
	if len(txs) > MaxTransactionsPerMsg {
		txs = txs[:MaxTransactionsPerMsg]
	}

	result := &TransactionResult{}
	for _, tx := range txs {
		err := ph.txPool.AddRemote(tx)
		if err != nil {
			result.Rejected++
			result.Errors = append(result.Errors, err)
		} else {
			result.Added++
		}
	}
	return result
}

// HandlePooledTransactions processes a response to GetPooledTransactions.
func (ph *ProtocolHandler) HandlePooledTransactions(peerID string, requested []types.Hash, received []*types.Transaction) *PooledTxResult {
	result := &PooledTxResult{
		Requested: len(requested),
		Received:  len(received),
	}

	receivedMap := make(map[types.Hash]bool, len(received))
	for _, tx := range received {
		receivedMap[tx.Hash()] = true
		ph.txPool.AddRemote(tx)
	}

	for _, hash := range requested {
		if !receivedMap[hash] {
			result.Missing = append(result.Missing, hash)
		}
	}
	return result
}

// HandleGetBlockHeaders responds to a block header request by collecting
// headers from the local chain.
func (ph *ProtocolHandler) HandleGetBlockHeaders(origin types.Hash, originNumber uint64, useHash bool, amount, skip uint64, reverse bool) *HeadersResult {
	if amount > MaxHeaders {
		amount = MaxHeaders
	}

	result := &HeadersResult{}
	var startBlock *types.Block

	if useHash {
		startBlock = ph.chain.GetBlock(origin)
	} else {
		startBlock = ph.chain.GetBlockByNumber(originNumber)
	}
	if startBlock == nil {
		return result
	}

	result.Headers = append(result.Headers, startBlock.Header())
	num := startBlock.NumberU64()

	for i := uint64(1); i < amount; i++ {
		if reverse {
			if num < 1+skip {
				break
			}
			num -= 1 + skip
		} else {
			num += 1 + skip
		}
		block := ph.chain.GetBlockByNumber(num)
		if block == nil {
			break
		}
		result.Headers = append(result.Headers, block.Header())
	}
	result.Count = len(result.Headers)
	return result
}

// HandleGetBlockBodies responds to a block bodies request.
func (ph *ProtocolHandler) HandleGetBlockBodies(hashes []types.Hash) *BodiesResult {
	count := len(hashes)
	if count > MaxBodies {
		count = MaxBodies
	}

	result := &BodiesResult{}
	for _, hash := range hashes[:count] {
		block := ph.chain.GetBlock(hash)
		if block == nil {
			result.Bodies = append(result.Bodies, &types.Body{})
		} else {
			result.Bodies = append(result.Bodies, block.Body())
		}
	}
	result.Count = len(result.Bodies)
	return result
}

// BestPeer returns the peer with the highest total difficulty.
func (ph *ProtocolHandler) BestPeer() *PeerSyncState {
	ph.mu.RLock()
	defer ph.mu.RUnlock()

	var best *PeerSyncState
	for _, ps := range ph.peerStates {
		if best == nil || ps.TD.Cmp(best.TD) > 0 {
			best = ps
		}
	}
	return best
}

// Stop marks the handler as stopped, preventing further operations.
func (ph *ProtocolHandler) Stop() {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	ph.stopped = true
}

// IsStopped returns whether the handler has been stopped.
func (ph *ProtocolHandler) IsStopped() bool {
	ph.mu.RLock()
	defer ph.mu.RUnlock()
	return ph.stopped
}
