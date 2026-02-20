// Package p2p implements the Req/Resp protocol for consensus-layer P2P
// communication per the Ethereum consensus P2P specification.
// ReqRespProtocol manages request-response exchanges with method-based
// routing, rate limiting, response streaming, and timeout handling.
package p2p

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// MethodID identifies a Req/Resp method per the consensus spec.
type MethodID int

const (
	// StatusV1 exchanges chain status with a peer.
	// Protocol: /eth2/beacon_chain/req/status/1/ssz_snappy
	StatusV1 MethodID = iota
	// GoodbyeV1 is sent upon disconnection.
	// Protocol: /eth2/beacon_chain/req/goodbye/1/ssz_snappy
	GoodbyeV1
	// BeaconBlocksByRangeV2 requests blocks in a slot range.
	// Protocol: /eth2/beacon_chain/req/beacon_blocks_by_range/2/ssz_snappy
	BeaconBlocksByRangeV2
	// BeaconBlocksByRootV2 requests blocks by root hash.
	// Protocol: /eth2/beacon_chain/req/beacon_blocks_by_root/2/ssz_snappy
	BeaconBlocksByRootV2
	// BlobSidecarsByRangeV1 requests blob sidecars in a slot range.
	// Protocol: /eth2/beacon_chain/req/blob_sidecars_by_range/1/ssz_snappy
	BlobSidecarsByRangeV1
	// BlobSidecarsByRootV1 requests blob sidecars by root hash.
	// Protocol: /eth2/beacon_chain/req/blob_sidecars_by_root/1/ssz_snappy
	BlobSidecarsByRootV1
)

// methodProtocolIDs maps each MethodID to its full protocol identifier.
var methodProtocolIDs = map[MethodID]string{
	StatusV1:              "/eth2/beacon_chain/req/status/1/ssz_snappy",
	GoodbyeV1:             "/eth2/beacon_chain/req/goodbye/1/ssz_snappy",
	BeaconBlocksByRangeV2: "/eth2/beacon_chain/req/beacon_blocks_by_range/2/ssz_snappy",
	BeaconBlocksByRootV2:  "/eth2/beacon_chain/req/beacon_blocks_by_root/2/ssz_snappy",
	BlobSidecarsByRangeV1: "/eth2/beacon_chain/req/blob_sidecars_by_range/1/ssz_snappy",
	BlobSidecarsByRootV1:  "/eth2/beacon_chain/req/blob_sidecars_by_root/1/ssz_snappy",
}

// String returns the protocol ID string for the method.
func (m MethodID) String() string {
	if s, ok := methodProtocolIDs[m]; ok {
		return s
	}
	return fmt.Sprintf("unknown_method(%d)", int(m))
}

// ResponseCode represents the result code in a response chunk per the spec.
type ResponseCode byte

const (
	// RespSuccess indicates a normal, successful response.
	RespSuccess ResponseCode = 0
	// RespInvalidRequest means the request was semantically invalid.
	RespInvalidRequest ResponseCode = 1
	// RespServerError means the responder encountered an internal error.
	RespServerError ResponseCode = 2
	// RespResourceUnavailable means the requested resource is not available.
	RespResourceUnavailable ResponseCode = 3
)

// String returns a human-readable name for the response code.
func (c ResponseCode) String() string {
	switch c {
	case RespSuccess:
		return "Success"
	case RespInvalidRequest:
		return "InvalidRequest"
	case RespServerError:
		return "ServerError"
	case RespResourceUnavailable:
		return "ResourceUnavailable"
	default:
		return fmt.Sprintf("ResponseCode(%d)", c)
	}
}

// StatusMessage represents the Status handshake message per the spec.
type StatusMessage struct {
	ForkDigest     [4]byte  // The node's ForkDigest.
	FinalizedRoot  [32]byte // store.finalized_checkpoint.root
	FinalizedEpoch uint64   // store.finalized_checkpoint.epoch
	HeadRoot       [32]byte // hash_tree_root of the current head block.
	HeadSlot       uint64   // Slot of the head block.
}

// GoodbyeReason is the reason code sent in a Goodbye message.
type GoodbyeReason uint64

const (
	GoodbyeClientShutdown    GoodbyeReason = 1
	GoodbyeIrrelevantNetwork GoodbyeReason = 2
	GoodbyeFaultError        GoodbyeReason = 3
)

// ProtocolRequest represents an outgoing request to a peer.
type ProtocolRequest struct {
	ID      uint64
	Method  MethodID
	Peer    string
	Payload []byte
	Timeout time.Duration
}

// ProtocolResponse represents a response from a peer.
type ProtocolResponse struct {
	Code    ResponseCode
	Payload []byte
	Error   string
}

// ResponseChunk represents a single chunk in a streamed response
// (used for range queries that return multiple items).
type ResponseChunk struct {
	Code    ResponseCode
	Payload []byte
	Error   string
}

// StreamedResponse holds a complete streamed response (multiple chunks).
type StreamedResponse struct {
	Chunks []ResponseChunk
}

// MaxConcurrentRequestsPerProtocol is the spec-mandated limit per peer per method.
const MaxConcurrentRequestsPerProtocol = 2

// ReqHandler is a callback for handling an incoming request.
// It receives the peer ID, request payload, and returns a response.
type ReqHandler func(peer string, payload []byte) *ProtocolResponse

// StreamingRequestHandler handles requests that produce multiple response chunks
// (e.g., BeaconBlocksByRange). It writes chunks to the provided channel.
type StreamingRequestHandler func(peer string, payload []byte, chunks chan<- ResponseChunk)

// ProtocolConfig configures the ReqRespProtocol.
type ProtocolConfig struct {
	// DefaultTimeout is the default request timeout.
	DefaultTimeout time.Duration
	// MethodTimeouts allows per-method timeout overrides.
	MethodTimeouts map[MethodID]time.Duration
	// RateLimitWindow is the time window for rate limiting.
	RateLimitWindow time.Duration
	// RateLimitMaxRequests is the max requests per peer per method per window.
	RateLimitMaxRequests int
}

// DefaultProtocolConfig returns sensible defaults.
func DefaultProtocolConfig() ProtocolConfig {
	return ProtocolConfig{
		DefaultTimeout: 10 * time.Second,
		MethodTimeouts: map[MethodID]time.Duration{
			StatusV1:              5 * time.Second,
			GoodbyeV1:            3 * time.Second,
			BeaconBlocksByRangeV2: 30 * time.Second,
			BeaconBlocksByRootV2:  15 * time.Second,
			BlobSidecarsByRangeV1: 30 * time.Second,
			BlobSidecarsByRootV1:  15 * time.Second,
		},
		RateLimitWindow:      10 * time.Second,
		RateLimitMaxRequests: 20,
	}
}

// Errors for ReqRespProtocol.
var (
	ErrProtocolClosed       = errors.New("reqresp_protocol: protocol closed")
	ErrProtocolNoHandler    = errors.New("reqresp_protocol: no handler registered")
	ErrProtocolTimeout      = errors.New("reqresp_protocol: request timeout")
	ErrProtocolRateLimited  = errors.New("reqresp_protocol: rate limited")
	ErrProtocolConcurrency  = errors.New("reqresp_protocol: max concurrent requests exceeded")
	ErrProtocolNilPayload   = errors.New("reqresp_protocol: nil response payload")
	ErrProtocolInvalidResp  = errors.New("reqresp_protocol: invalid response")
)

// rateLimitEntry tracks request timestamps for rate limiting.
type rateLimitEntry struct {
	timestamps []time.Time
}

// pendingKey uniquely identifies a concurrent request slot: peer + method.
type pendingKey struct {
	peer   string
	method MethodID
}

// ReqRespProtocol manages request-response exchanges for CL P2P communication.
// It supports method-based routing, rate limiting, response streaming,
// and timeout handling. All methods are safe for concurrent use.
type ReqRespProtocol struct {
	mu     sync.RWMutex
	config ProtocolConfig
	closed bool
	nextID atomic.Uint64

	// Registered handlers for each method.
	handlers          map[MethodID]ReqHandler
	streamingHandlers map[MethodID]StreamingRequestHandler

	// Rate limiting: (peer, method) -> timestamps.
	rateLimits   map[pendingKey]*rateLimitEntry
	rateLimitsMu sync.Mutex

	// Concurrent request tracking: (peer, method) -> count.
	pending   map[pendingKey]int
	pendingMu sync.Mutex

	// Send function is injected for actual network delivery.
	// In production, this sends data over the wire; in tests, it's mocked.
	sendFunc func(peer string, method MethodID, payload []byte) (*ProtocolResponse, error)

	// Streaming send function for range queries.
	sendStreamFunc func(peer string, method MethodID, payload []byte) (*StreamedResponse, error)
}

// NewReqRespProtocol creates a new ReqRespProtocol with the given config.
func NewReqRespProtocol(config ProtocolConfig) *ReqRespProtocol {
	return &ReqRespProtocol{
		config:            config,
		handlers:          make(map[MethodID]ReqHandler),
		streamingHandlers: make(map[MethodID]StreamingRequestHandler),
		rateLimits:        make(map[pendingKey]*rateLimitEntry),
		pending:           make(map[pendingKey]int),
	}
}

// HandleRequest registers a handler for the given method.
// Only one handler (regular or streaming) can be registered per method.
func (p *ReqRespProtocol) HandleRequest(method MethodID, handler ReqHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers[method] = handler
}

// HandleStreamingRequest registers a streaming handler for range queries.
func (p *ReqRespProtocol) HandleStreamingRequest(method MethodID, handler StreamingRequestHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.streamingHandlers[method] = handler
}

// SetSendFunc sets the function used to send single-response requests.
func (p *ReqRespProtocol) SetSendFunc(fn func(peer string, method MethodID, payload []byte) (*ProtocolResponse, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sendFunc = fn
}

// SetStreamSendFunc sets the function used to send streaming requests.
func (p *ReqRespProtocol) SetStreamSendFunc(fn func(peer string, method MethodID, payload []byte) (*StreamedResponse, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sendStreamFunc = fn
}

// methodTimeout returns the timeout for the given method.
func (p *ReqRespProtocol) methodTimeout(method MethodID) time.Duration {
	if t, ok := p.config.MethodTimeouts[method]; ok {
		return t
	}
	return p.config.DefaultTimeout
}

// checkRateLimit verifies the request is within rate limits.
// Returns an error if rate limited. Must be called with rateLimitsMu held.
func (p *ReqRespProtocol) checkRateLimit(peer string, method MethodID) error {
	key := pendingKey{peer: peer, method: method}
	entry, exists := p.rateLimits[key]
	if !exists {
		entry = &rateLimitEntry{}
		p.rateLimits[key] = entry
	}

	now := time.Now()
	cutoff := now.Add(-p.config.RateLimitWindow)

	// Prune expired timestamps.
	valid := entry.timestamps[:0]
	for _, ts := range entry.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	entry.timestamps = valid

	if len(entry.timestamps) >= p.config.RateLimitMaxRequests {
		return ErrProtocolRateLimited
	}

	entry.timestamps = append(entry.timestamps, now)
	return nil
}

// acquireConcurrency checks and increments the concurrent request count.
func (p *ReqRespProtocol) acquireConcurrency(peer string, method MethodID) error {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()

	key := pendingKey{peer: peer, method: method}
	if p.pending[key] >= MaxConcurrentRequestsPerProtocol {
		return ErrProtocolConcurrency
	}
	p.pending[key]++
	return nil
}

// releaseConcurrency decrements the concurrent request count.
func (p *ReqRespProtocol) releaseConcurrency(peer string, method MethodID) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()

	key := pendingKey{peer: peer, method: method}
	if p.pending[key] > 0 {
		p.pending[key]--
	}
	if p.pending[key] == 0 {
		delete(p.pending, key)
	}
}

// SendRequest sends a request to a peer and waits for a single response.
// It enforces rate limits, concurrency limits, and timeouts.
func (p *ReqRespProtocol) SendRequest(peer string, method MethodID, payload []byte) (*ProtocolResponse, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, ErrProtocolClosed
	}
	sendFn := p.sendFunc
	p.mu.RUnlock()

	if sendFn == nil {
		return nil, ErrProtocolNoHandler
	}

	// Rate limit check.
	p.rateLimitsMu.Lock()
	if err := p.checkRateLimit(peer, method); err != nil {
		p.rateLimitsMu.Unlock()
		return nil, err
	}
	p.rateLimitsMu.Unlock()

	// Concurrency check.
	if err := p.acquireConcurrency(peer, method); err != nil {
		return nil, err
	}
	defer p.releaseConcurrency(peer, method)

	// Execute with timeout.
	timeout := p.methodTimeout(method)
	type result struct {
		resp *ProtocolResponse
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		resp, err := sendFn(peer, method, payload)
		ch <- result{resp, err}
	}()

	select {
	case r := <-ch:
		return r.resp, r.err
	case <-time.After(timeout):
		return nil, ErrProtocolTimeout
	}
}

// SendStreamingRequest sends a request that expects a streamed response
// (multiple chunks), such as BeaconBlocksByRange.
func (p *ReqRespProtocol) SendStreamingRequest(peer string, method MethodID, payload []byte) (*StreamedResponse, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, ErrProtocolClosed
	}
	streamFn := p.sendStreamFunc
	p.mu.RUnlock()

	if streamFn == nil {
		return nil, ErrProtocolNoHandler
	}

	// Rate limit check.
	p.rateLimitsMu.Lock()
	if err := p.checkRateLimit(peer, method); err != nil {
		p.rateLimitsMu.Unlock()
		return nil, err
	}
	p.rateLimitsMu.Unlock()

	// Concurrency check.
	if err := p.acquireConcurrency(peer, method); err != nil {
		return nil, err
	}
	defer p.releaseConcurrency(peer, method)

	// Execute with timeout.
	timeout := p.methodTimeout(method)
	type result struct {
		resp *StreamedResponse
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		resp, err := streamFn(peer, method, payload)
		ch <- result{resp, err}
	}()

	select {
	case r := <-ch:
		return r.resp, r.err
	case <-time.After(timeout):
		return nil, ErrProtocolTimeout
	}
}

// ProcessIncomingRequest dispatches an incoming request to the registered handler.
func (p *ReqRespProtocol) ProcessIncomingRequest(peer string, method MethodID, payload []byte) *ProtocolResponse {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return &ProtocolResponse{
			Code:  RespServerError,
			Error: "protocol closed",
		}
	}

	handler, ok := p.handlers[method]
	p.mu.RUnlock()

	if !ok {
		return &ProtocolResponse{
			Code:  RespInvalidRequest,
			Error: fmt.Sprintf("no handler for method %s", method),
		}
	}

	return handler(peer, payload)
}

// ProcessIncomingStreamingRequest dispatches an incoming range request
// to the registered streaming handler.
func (p *ReqRespProtocol) ProcessIncomingStreamingRequest(peer string, method MethodID, payload []byte) *StreamedResponse {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return &StreamedResponse{
			Chunks: []ResponseChunk{{
				Code:  RespServerError,
				Error: "protocol closed",
			}},
		}
	}

	handler, ok := p.streamingHandlers[method]
	p.mu.RUnlock()

	if !ok {
		return &StreamedResponse{
			Chunks: []ResponseChunk{{
				Code:  RespInvalidRequest,
				Error: fmt.Sprintf("no handler for method %s", method),
			}},
		}
	}

	chunks := make(chan ResponseChunk, 64)
	go func() {
		handler(peer, payload, chunks)
		close(chunks)
	}()

	var resp StreamedResponse
	for chunk := range chunks {
		resp.Chunks = append(resp.Chunks, chunk)
	}
	return &resp
}

// PendingRequestCount returns the number of concurrent requests for a peer+method.
func (p *ReqRespProtocol) PendingRequestCount(peer string, method MethodID) int {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	return p.pending[pendingKey{peer: peer, method: method}]
}

// HasHandler returns whether a handler is registered for the given method.
func (p *ReqRespProtocol) HasHandler(method MethodID) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.handlers[method]
	if !ok {
		_, ok = p.streamingHandlers[method]
	}
	return ok
}

// Config returns the current protocol configuration.
func (p *ReqRespProtocol) Config() ProtocolConfig {
	return p.config
}

// Close shuts down the protocol. After closing, SendRequest returns errors.
func (p *ReqRespProtocol) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
}
