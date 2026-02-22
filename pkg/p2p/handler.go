package p2p

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/rlp"
)

// Handler errors.
var (
	ErrRequestTimeout   = errors.New("p2p: request timed out")
	ErrDuplicateRequest = errors.New("p2p: duplicate request ID")
	ErrUnknownRequest   = errors.New("p2p: unknown request ID")
	ErrHandlerNotFound  = errors.New("p2p: no handler for message code")
	ErrNilPeer          = errors.New("p2p: nil peer")
	ErrNilBackend       = errors.New("p2p: nil backend")
)

// Default limits.
const (
	// MaxHeadersServe is the maximum number of headers returned in one response.
	MaxHeadersServe = 1024

	// MaxBodiesServe is the maximum number of block bodies returned in one response.
	MaxBodiesServe = 256

	// MaxReceiptsServe is the maximum number of receipt lists returned in one response.
	MaxReceiptsServe = 256

	// MaxPooledTxServe is the maximum number of pooled transactions returned in one response.
	MaxPooledTxServe = 256

	// DefaultRequestTimeout is the default duration before a pending request expires.
	DefaultRequestTimeout = 15 * time.Second
)

// HandlerFunc is a function that handles a single protocol message from a peer.
// The decoded payload is passed as msg.Payload. Returning an error signals that
// the peer misbehaved and should be penalized.
type HandlerFunc func(backend Backend, peer *Peer, msg Message) error

// Backend is the interface that the protocol handler uses to access chain data
// for serving requests. Implementations are provided by the sync/downloader layer.
type Backend interface {
	// GetHeaderByHash returns a header by its hash, or nil.
	GetHeaderByHash(hash types.Hash) *types.Header

	// GetHeaderByNumber returns a header by its block number, or nil.
	GetHeaderByNumber(number uint64) *types.Header

	// GetBlockBody returns the body (txs, uncles, withdrawals) for a block hash.
	GetBlockBody(hash types.Hash) *BlockBody

	// GetReceipts returns the receipts for a block hash.
	GetReceipts(hash types.Hash) []*types.Receipt

	// GetPooledTransaction returns a pooled transaction by hash, or nil.
	GetPooledTransaction(hash types.Hash) *types.Transaction

	// HandleNewBlock is called when a NewBlock broadcast is received.
	HandleNewBlock(peer *Peer, block *types.Block, td *big.Int)

	// HandleNewBlockHashes is called when a NewBlockHashes broadcast is received.
	HandleNewBlockHashes(peer *Peer, hashes []NewBlockHashesEntry)

	// HandleTransactions is called when a Transactions broadcast is received.
	HandleTransactions(peer *Peer, txs []*types.Transaction)

	// HandleNewPooledTransactionHashes is called when a NewPooledTransactionHashes
	// broadcast (eth/68 format) is received.
	HandleNewPooledTransactionHashes(peer *Peer, types []byte, sizes []uint32, hashes []types.Hash)
}

// HandlerRegistry maps eth/68 message codes to their handler functions.
type HandlerRegistry struct {
	mu       sync.RWMutex
	handlers map[uint64]HandlerFunc
}

// NewHandlerRegistry creates a registry pre-populated with all eth/68 handlers.
func NewHandlerRegistry() *HandlerRegistry {
	r := &HandlerRegistry{
		handlers: make(map[uint64]HandlerFunc),
	}
	r.registerDefaults()
	return r
}

// Register adds or replaces the handler for a message code.
func (r *HandlerRegistry) Register(code uint64, h HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[code] = h
}

// Handle dispatches a message to the registered handler for its code.
func (r *HandlerRegistry) Handle(backend Backend, peer *Peer, msg Message) error {
	r.mu.RLock()
	h, ok := r.handlers[msg.Code]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: 0x%02x", ErrHandlerNotFound, msg.Code)
	}
	return h(backend, peer, msg)
}

// Lookup returns the handler for a message code, or nil if not registered.
func (r *HandlerRegistry) Lookup(code uint64) HandlerFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.handlers[code]
}

// registerDefaults installs the default eth/68 handlers.
func (r *HandlerRegistry) registerDefaults() {
	// Request-response handlers.
	r.handlers[GetBlockHeadersMsg] = handleGetBlockHeaders
	r.handlers[BlockHeadersMsg] = handleBlockHeaders
	r.handlers[GetBlockBodiesMsg] = handleGetBlockBodies
	r.handlers[BlockBodiesMsg] = handleBlockBodies
	r.handlers[GetReceiptsMsg] = handleGetReceipts
	r.handlers[ReceiptsMsg] = handleReceipts
	r.handlers[GetPooledTransactionsMsg] = handleGetPooledTransactions
	r.handlers[PooledTransactionsMsg] = handlePooledTransactions

	// Broadcast handlers.
	r.handlers[NewBlockHashesMsg] = handleNewBlockHashes
	r.handlers[NewBlockMsg] = handleNewBlock
	r.handlers[TransactionsMsg] = handleTransactions
	r.handlers[NewPooledTransactionHashesMsg] = handleNewPooledTransactionHashes
}

// ---------------------------------------------------------------------------
// Request-response: GetBlockHeaders / BlockHeaders
// ---------------------------------------------------------------------------

func handleGetBlockHeaders(backend Backend, peer *Peer, msg Message) error {
	if backend == nil {
		return ErrNilBackend
	}
	var pkt GetBlockHeadersPacket
	if err := DecodeMessage(msg, &pkt); err != nil {
		return err
	}

	headers := resolveHeaders(backend, pkt.Request)

	resp := BlockHeadersPacket{
		RequestID: pkt.RequestID,
		Headers:   headers,
	}
	// The response would be sent via the peer's transport; here we store it
	// on the peer so it can be picked up by the caller / test.
	peer.SetLastResponse(BlockHeadersMsg, resp)
	return nil
}

// resolveHeaders collects headers from the backend according to the request params.
func resolveHeaders(backend Backend, req GetBlockHeadersRequest) []*types.Header {
	amount := req.Amount
	if amount > MaxHeadersServe {
		amount = MaxHeadersServe
	}

	headers := make([]*types.Header, 0, amount)

	// Resolve the origin header.
	var origin *types.Header
	if req.Origin.IsHash() {
		origin = backend.GetHeaderByHash(req.Origin.Hash)
	} else {
		origin = backend.GetHeaderByNumber(req.Origin.Number)
	}
	if origin == nil {
		return headers
	}

	headers = append(headers, origin)

	// Walk the chain collecting additional headers.
	for i := uint64(1); i < amount; i++ {
		var next *types.Header
		if req.Reverse {
			// Walk backwards.
			num := origin.Number.Uint64()
			step := (req.Skip + 1)
			if num < step*i {
				break
			}
			next = backend.GetHeaderByNumber(num - step*i)
		} else {
			// Walk forwards.
			num := origin.Number.Uint64()
			step := (req.Skip + 1)
			next = backend.GetHeaderByNumber(num + step*i)
		}
		if next == nil {
			break
		}
		headers = append(headers, next)
	}
	return headers
}

func handleBlockHeaders(_ Backend, peer *Peer, msg Message) error {
	var pkt BlockHeadersPacket
	if err := DecodeMessage(msg, &pkt); err != nil {
		return err
	}
	// Deliver the response to the request tracker.
	peer.DeliverResponse(pkt.RequestID, &pkt)
	return nil
}

// ---------------------------------------------------------------------------
// Request-response: GetBlockBodies / BlockBodies
// ---------------------------------------------------------------------------

func handleGetBlockBodies(backend Backend, peer *Peer, msg Message) error {
	if backend == nil {
		return ErrNilBackend
	}
	var pkt GetBlockBodiesPacket
	if err := DecodeMessage(msg, &pkt); err != nil {
		return err
	}

	hashes := pkt.Hashes
	if uint64(len(hashes)) > MaxBodiesServe {
		hashes = hashes[:MaxBodiesServe]
	}

	bodies := make([]*BlockBody, 0, len(hashes))
	for _, hash := range hashes {
		body := backend.GetBlockBody(hash)
		if body != nil {
			bodies = append(bodies, body)
		}
	}

	resp := BlockBodiesPacket{
		RequestID: pkt.RequestID,
		Bodies:    bodies,
	}
	peer.SetLastResponse(BlockBodiesMsg, resp)
	return nil
}

func handleBlockBodies(_ Backend, peer *Peer, msg Message) error {
	var pkt BlockBodiesPacket
	if err := DecodeMessage(msg, &pkt); err != nil {
		return err
	}
	peer.DeliverResponse(pkt.RequestID, &pkt)
	return nil
}

// ---------------------------------------------------------------------------
// Request-response: GetReceipts / Receipts
// ---------------------------------------------------------------------------

func handleGetReceipts(backend Backend, peer *Peer, msg Message) error {
	if backend == nil {
		return ErrNilBackend
	}
	var pkt GetReceiptsPacket
	if err := DecodeMessage(msg, &pkt); err != nil {
		return err
	}

	hashes := pkt.Hashes
	if uint64(len(hashes)) > MaxReceiptsServe {
		hashes = hashes[:MaxReceiptsServe]
	}

	result := make([][]*types.Receipt, 0, len(hashes))
	for _, hash := range hashes {
		receipts := backend.GetReceipts(hash)
		if receipts != nil {
			result = append(result, receipts)
		}
	}

	resp := ReceiptsPacket{
		RequestID: pkt.RequestID,
		Receipts:  result,
	}
	peer.SetLastResponse(ReceiptsMsg, resp)
	return nil
}

func handleReceipts(_ Backend, peer *Peer, msg Message) error {
	var pkt ReceiptsPacket
	if err := DecodeMessage(msg, &pkt); err != nil {
		return err
	}
	peer.DeliverResponse(pkt.RequestID, &pkt)
	return nil
}

// ---------------------------------------------------------------------------
// Request-response: GetPooledTransactions / PooledTransactions
// ---------------------------------------------------------------------------

func handleGetPooledTransactions(backend Backend, peer *Peer, msg Message) error {
	if backend == nil {
		return ErrNilBackend
	}
	var pkt GetPooledTransactionsPacket
	if err := DecodeMessage(msg, &pkt); err != nil {
		return err
	}

	hashes := pkt.Hashes
	if uint64(len(hashes)) > MaxPooledTxServe {
		hashes = hashes[:MaxPooledTxServe]
	}

	txs := make([]*types.Transaction, 0, len(hashes))
	for _, hash := range hashes {
		tx := backend.GetPooledTransaction(hash)
		if tx != nil {
			txs = append(txs, tx)
		}
	}

	resp := PooledTransactionsPacket{
		RequestID:    pkt.RequestID,
		Transactions: txs,
	}
	peer.SetLastResponse(PooledTransactionsMsg, resp)
	return nil
}

func handlePooledTransactions(_ Backend, peer *Peer, msg Message) error {
	var pkt PooledTransactionsPacket
	if err := DecodeMessage(msg, &pkt); err != nil {
		return err
	}
	peer.DeliverResponse(pkt.RequestID, &pkt)
	return nil
}

// ---------------------------------------------------------------------------
// Broadcast: NewBlockHashes
// ---------------------------------------------------------------------------

func handleNewBlockHashes(backend Backend, peer *Peer, msg Message) error {
	var entries []NewBlockHashesEntry
	if err := DecodeMessage(msg, &entries); err != nil {
		return err
	}
	if backend != nil {
		backend.HandleNewBlockHashes(peer, entries)
	}
	// Update peer's head to the highest announced block.
	for _, entry := range entries {
		if entry.Number > peer.HeadNumber() {
			peer.SetHead(entry.Hash, nil)
			peer.SetHeadNumber(entry.Number)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Broadcast: NewBlock
// ---------------------------------------------------------------------------

func handleNewBlock(backend Backend, peer *Peer, msg Message) error {
	// NewBlockData contains a *Block which requires custom RLP decoding
	// (the generic decoder cannot handle Block's unexported fields).
	block, td, err := decodeNewBlockData(msg.Payload)
	if err != nil {
		return fmt.Errorf("%w: code 0x%02x: %v", ErrDecode, msg.Code, err)
	}
	// Update the peer's head.
	peer.SetHead(block.Hash(), td)
	peer.SetHeadNumber(block.NumberU64())

	if backend != nil {
		backend.HandleNewBlock(peer, block, td)
	}
	return nil
}

// decodeNewBlockData decodes the RLP payload of a NewBlock message.
// The wire format is: [block_rlp, td] as a two-element RLP list.
func decodeNewBlockData(payload []byte) (*types.Block, *big.Int, error) {
	s := rlp.NewStreamFromBytes(payload)
	if _, err := s.List(); err != nil {
		return nil, nil, fmt.Errorf("opening NewBlock list: %w", err)
	}

	// First element: the block (itself an RLP list).
	blockRLP, err := s.RawItem()
	if err != nil {
		return nil, nil, fmt.Errorf("reading block RLP: %w", err)
	}
	block, err := types.DecodeBlockRLP(blockRLP)
	if err != nil {
		return nil, nil, fmt.Errorf("decoding block: %w", err)
	}

	// Second element: total difficulty (big.Int).
	td, err := s.BigInt()
	if err != nil {
		return nil, nil, fmt.Errorf("reading TD: %w", err)
	}

	if err := s.ListEnd(); err != nil {
		return nil, nil, fmt.Errorf("closing NewBlock list: %w", err)
	}
	return block, td, nil
}

// ---------------------------------------------------------------------------
// Broadcast: Transactions
// ---------------------------------------------------------------------------

func handleTransactions(backend Backend, peer *Peer, msg Message) error {
	var txs []*types.Transaction
	if err := DecodeMessage(msg, &txs); err != nil {
		return err
	}
	if backend != nil {
		backend.HandleTransactions(peer, txs)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Broadcast: NewPooledTransactionHashes (eth/68)
// ---------------------------------------------------------------------------

func handleNewPooledTransactionHashes(backend Backend, peer *Peer, msg Message) error {
	var pkt NewPooledTransactionHashesPacket68
	if err := DecodeMessage(msg, &pkt); err != nil {
		return err
	}
	// Validate that Types, Sizes, and Hashes have the same length.
	if len(pkt.Types) != len(pkt.Hashes) || len(pkt.Sizes) != len(pkt.Hashes) {
		return fmt.Errorf("p2p: mismatched NewPooledTransactionHashes lengths: types=%d sizes=%d hashes=%d",
			len(pkt.Types), len(pkt.Sizes), len(pkt.Hashes))
	}
	if backend != nil {
		backend.HandleNewPooledTransactionHashes(peer, pkt.Types, pkt.Sizes, pkt.Hashes)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Request tracker
// ---------------------------------------------------------------------------

// pendingRequest represents an in-flight request awaiting a response.
type pendingRequest struct {
	id       uint64
	deadline time.Time
	done     chan interface{} // closed or receives the response value
}

// RequestTracker manages outgoing request IDs and correlates them with
// incoming responses. It provides timeout-based expiry for stale requests.
type RequestTracker struct {
	mu       sync.Mutex
	pending  map[uint64]*pendingRequest
	nextID   atomic.Uint64
	timeout  time.Duration
	stopOnce sync.Once
	stop     chan struct{}
}

// NewRequestTracker creates a tracker with the given request timeout.
func NewRequestTracker(timeout time.Duration) *RequestTracker {
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	rt := &RequestTracker{
		pending: make(map[uint64]*pendingRequest),
		timeout: timeout,
		stop:    make(chan struct{}),
	}
	go rt.expireLoop()
	return rt
}

// NextRequestID returns a monotonically increasing request ID.
func (rt *RequestTracker) NextRequestID() uint64 {
	return rt.nextID.Add(1)
}

// Track registers a new outgoing request. Returns a channel that will receive
// the response (or be closed on timeout). Returns ErrDuplicateRequest if the
// ID is already tracked.
func (rt *RequestTracker) Track(id uint64) (<-chan interface{}, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if _, exists := rt.pending[id]; exists {
		return nil, ErrDuplicateRequest
	}
	pr := &pendingRequest{
		id:       id,
		deadline: time.Now().Add(rt.timeout),
		done:     make(chan interface{}, 1),
	}
	rt.pending[id] = pr
	return pr.done, nil
}

// Deliver provides a response for a tracked request ID. The value is sent to
// the waiting channel. Returns ErrUnknownRequest if the ID is not pending.
func (rt *RequestTracker) Deliver(id uint64, value interface{}) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	pr, ok := rt.pending[id]
	if !ok {
		return ErrUnknownRequest
	}
	delete(rt.pending, id)
	pr.done <- value
	close(pr.done)
	return nil
}

// Cancel removes a tracked request without delivering a response.
func (rt *RequestTracker) Cancel(id uint64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if pr, ok := rt.pending[id]; ok {
		delete(rt.pending, id)
		close(pr.done)
	}
}

// Pending returns the number of in-flight requests.
func (rt *RequestTracker) Pending() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.pending)
}

// Close stops the expiry goroutine and cancels all pending requests.
func (rt *RequestTracker) Close() {
	rt.stopOnce.Do(func() {
		close(rt.stop)
		rt.mu.Lock()
		for id, pr := range rt.pending {
			delete(rt.pending, id)
			close(pr.done)
		}
		rt.mu.Unlock()
	})
}

// expireLoop periodically removes requests that have exceeded their deadline.
func (rt *RequestTracker) expireLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rt.stop:
			return
		case now := <-ticker.C:
			rt.mu.Lock()
			for id, pr := range rt.pending {
				if now.After(pr.deadline) {
					delete(rt.pending, id)
					close(pr.done)
				}
			}
			rt.mu.Unlock()
		}
	}
}

// ---------------------------------------------------------------------------
// Peer protocol state extensions
// ---------------------------------------------------------------------------

// The Peer type is extended with protocol state fields for tracking the peer's
// head block number and last received responses. These methods are added here
// rather than in peer.go to keep handler-specific state grouped together.

// headNumber is the peer's best known block number.
// We store it separately because SetHead only tracks hash + TD.
// Using a sync map on the peer struct would be invasive; instead we
// add exported methods that access a new field via the mu lock.

// SetHeadNumber sets the peer's best known block number.
func (p *Peer) SetHeadNumber(num uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.headNumber = num
}

// HeadNumber returns the peer's best known block number.
func (p *Peer) HeadNumber() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.headNumber
}

// SetLastResponse stores the most recent response message for a given code.
// This is used by request-serving handlers so that the upper layer or test
// can inspect what was produced without needing a live transport.
func (p *Peer) SetLastResponse(code uint64, value interface{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lastResponses == nil {
		p.lastResponses = make(map[uint64]interface{})
	}
	p.lastResponses[code] = value
}

// LastResponse returns the most recently stored response for a message code.
func (p *Peer) LastResponse(code uint64) interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.lastResponses == nil {
		return nil
	}
	return p.lastResponses[code]
}

// DeliverResponse stores a response value indexed by request ID on the peer.
// This is used by response handlers (BlockHeaders, BlockBodies, etc.) to make
// the response accessible for correlation with pending requests.
func (p *Peer) DeliverResponse(requestID uint64, value interface{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deliveredResponses == nil {
		p.deliveredResponses = make(map[uint64]interface{})
	}
	p.deliveredResponses[requestID] = value
}

// GetDeliveredResponse returns and removes a delivered response by request ID.
func (p *Peer) GetDeliveredResponse(requestID uint64) (interface{}, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deliveredResponses == nil {
		return nil, false
	}
	v, ok := p.deliveredResponses[requestID]
	if ok {
		delete(p.deliveredResponses, requestID)
	}
	return v, ok
}
