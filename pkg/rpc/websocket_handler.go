package rpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// WebSocket configuration constants.
const (
	// WSMaxMessageSize is the maximum size of a single WebSocket message (1 MB).
	WSMaxMessageSize = 1 << 20
	// WSPingInterval is the interval between ping frames sent to the client.
	WSPingInterval = 30 * time.Second
	// WSPongTimeout is the deadline for a pong response after a ping.
	WSPongTimeout = 60 * time.Second
	// WSWriteTimeout is the deadline for a write operation.
	WSWriteTimeout = 10 * time.Second
	// WSRateLimit is the maximum number of requests per second per connection.
	WSRateLimit = 100
	// WSRateWindow is the time window for rate limiting.
	WSRateWindow = time.Second
	// WSMaxSubscriptionsPerConn is the maximum subscriptions per connection.
	WSMaxSubscriptionsPerConn = 32
)

// WSMessage represents a parsed WebSocket frame containing a JSON-RPC request.
type WSMessage struct {
	Data []byte
	Err  error
}

// WSConn represents a single WebSocket connection with its state.
type WSConn struct {
	mu            sync.Mutex
	id            uint64
	api           *EthAPI
	batchHandler  *BatchHandler
	subscriptions map[string]bool
	closed        atomic.Bool
	rateLimiter   *rateBucket
	sendCh        chan []byte
	closeCh       chan struct{}
	createdAt     time.Time
}

// rateBucket implements a simple token bucket rate limiter for per-connection
// request throttling.
type rateBucket struct {
	mu       sync.Mutex
	tokens   int
	max      int
	lastFill time.Time
	window   time.Duration
}

// newRateBucket creates a rate limiter that allows max requests per window.
func newRateBucket(max int, window time.Duration) *rateBucket {
	return &rateBucket{
		tokens:   max,
		max:      max,
		lastFill: time.Now(),
		window:   window,
	}
}

// Allow checks if a request is allowed under the rate limit. Returns true
// if the request can proceed, false if the rate limit is exceeded.
func (rb *rateBucket) Allow() bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rb.lastFill)
	if elapsed >= rb.window {
		// Refill tokens.
		rb.tokens = rb.max
		rb.lastFill = now
	}

	if rb.tokens <= 0 {
		return false
	}
	rb.tokens--
	return true
}

// Remaining returns the number of tokens left in the current window.
func (rb *rateBucket) Remaining() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.tokens
}

// WSHandler manages WebSocket connections and their lifecycle.
type WSHandler struct {
	mu           sync.RWMutex
	api          *EthAPI
	batchHandler *BatchHandler
	connections  map[uint64]*WSConn
	nextID       atomic.Uint64
	maxConns     int
}

// NewWSHandler creates a WebSocket handler that dispatches JSON-RPC requests.
func NewWSHandler(api *EthAPI, maxConns int) *WSHandler {
	if maxConns <= 0 {
		maxConns = 100
	}
	return &WSHandler{
		api:          api,
		batchHandler: NewBatchHandler(api),
		connections:  make(map[uint64]*WSConn),
		maxConns:     maxConns,
	}
}

// newWSConn creates a new WebSocket connection state object.
func (h *WSHandler) newWSConn() *WSConn {
	id := h.nextID.Add(1)
	return &WSConn{
		id:            id,
		api:           h.api,
		batchHandler:  h.batchHandler,
		subscriptions: make(map[string]bool),
		rateLimiter:   newRateBucket(WSRateLimit, WSRateWindow),
		sendCh:        make(chan []byte, 256),
		closeCh:       make(chan struct{}),
		createdAt:     time.Now(),
	}
}

// addConnection registers a new connection. Returns error if max connections
// has been reached.
func (h *WSHandler) addConnection(conn *WSConn) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.connections) >= h.maxConns {
		return fmt.Errorf("maximum WebSocket connections (%d) reached", h.maxConns)
	}
	h.connections[conn.id] = conn
	return nil
}

// removeConnection unregisters a connection and cleans up its subscriptions.
func (h *WSHandler) removeConnection(conn *WSConn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Clean up subscriptions for this connection.
	conn.mu.Lock()
	for subID := range conn.subscriptions {
		h.api.subs.Unsubscribe(subID)
	}
	conn.subscriptions = nil
	conn.mu.Unlock()

	delete(h.connections, conn.id)
}

// ConnectionCount returns the number of active WebSocket connections.
func (h *WSHandler) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections)
}

// ServeHTTP implements http.Handler for WebSocket upgrade requests.
// In a full implementation, this would perform the WebSocket upgrade
// handshake. Here we validate the upgrade request and set up the
// connection state.
func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Validate the upgrade request headers.
	if r.Header.Get("Upgrade") != "websocket" {
		http.Error(w, "expected WebSocket upgrade", http.StatusBadRequest)
		return
	}

	conn := h.newWSConn()
	if err := h.addConnection(conn); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// In a production implementation, we would perform the full WebSocket
	// handshake here (using gorilla/websocket or nhooyr.io/websocket).
	// For this implementation, we acknowledge the upgrade and set up the
	// response to indicate the connection was accepted.
	w.Header().Set("X-WS-Connection-ID", encodeUint64(conn.id))
	w.WriteHeader(http.StatusSwitchingProtocols)
}

// HandleMessage processes a single JSON-RPC message from a WebSocket connection.
// It handles both single requests and batch requests, with rate limiting.
func (conn *WSConn) HandleMessage(data []byte) ([]byte, error) {
	if conn.closed.Load() {
		return nil, fmt.Errorf("connection closed")
	}

	// Rate limit check.
	if !conn.rateLimiter.Allow() {
		errResp := &Response{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: -32005, Message: "rate limit exceeded"},
			ID:      json.RawMessage("null"),
		}
		return json.Marshal(errResp)
	}

	// Check if this is a batch request.
	if IsBatchRequest(data) {
		responses, err := conn.batchHandler.HandleBatch(data)
		if err != nil {
			errResp := &Response{
				JSONRPC: "2.0",
				Error:   &RPCError{Code: ErrCodeInvalidRequest, Message: err.Error()},
				ID:      json.RawMessage("null"),
			}
			return json.Marshal(errResp)
		}
		return MarshalBatchResponse(responses)
	}

	// Parse as a single request.
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		errResp := &Response{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeParse, Message: "invalid JSON"},
			ID:      json.RawMessage("null"),
		}
		return json.Marshal(errResp)
	}

	// Handle subscription methods specially for connection-scoped tracking.
	switch req.Method {
	case "eth_subscribe":
		return conn.handleSubscribe(&req)
	case "eth_unsubscribe":
		return conn.handleUnsubscribe(&req)
	default:
		resp := conn.api.HandleRequest(&req)
		return json.Marshal(resp)
	}
}

// handleSubscribe creates a subscription scoped to this connection.
func (conn *WSConn) handleSubscribe(req *Request) ([]byte, error) {
	conn.mu.Lock()
	if len(conn.subscriptions) >= WSMaxSubscriptionsPerConn {
		conn.mu.Unlock()
		errResp := errorResponse(req.ID, ErrCodeInvalidRequest,
			fmt.Sprintf("maximum subscriptions per connection (%d) reached", WSMaxSubscriptionsPerConn))
		return json.Marshal(errResp)
	}
	conn.mu.Unlock()

	resp := conn.api.HandleRequest(req)

	// Track the subscription ID if the call succeeded.
	if resp.Error == nil && resp.Result != nil {
		if subID, ok := resp.Result.(string); ok {
			conn.mu.Lock()
			conn.subscriptions[subID] = true
			conn.mu.Unlock()
		}
	}

	return json.Marshal(resp)
}

// handleUnsubscribe removes a subscription scoped to this connection.
func (conn *WSConn) handleUnsubscribe(req *Request) ([]byte, error) {
	resp := conn.api.HandleRequest(req)

	// Remove tracking if the unsubscribe succeeded.
	if resp.Error == nil {
		if len(req.Params) > 0 {
			var subID string
			if json.Unmarshal(req.Params[0], &subID) == nil {
				conn.mu.Lock()
				delete(conn.subscriptions, subID)
				conn.mu.Unlock()
			}
		}
	}

	return json.Marshal(resp)
}

// Close marks the connection as closed and cleans up resources.
func (conn *WSConn) Close() {
	if conn.closed.CompareAndSwap(false, true) {
		close(conn.closeCh)
	}
}

// IsClosed returns true if the connection has been closed.
func (conn *WSConn) IsClosed() bool {
	return conn.closed.Load()
}

// SubscriptionCount returns the number of active subscriptions on this connection.
func (conn *WSConn) SubscriptionCount() int {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return len(conn.subscriptions)
}

// ID returns the connection's unique identifier.
func (conn *WSConn) ID() uint64 {
	return conn.id
}

// FormatWSError creates a JSON-RPC error response suitable for WebSocket delivery.
func FormatWSError(id json.RawMessage, code int, message string) []byte {
	resp := &Response{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: code, Message: message},
		ID:      id,
	}
	data, _ := json.Marshal(resp)
	return data
}

// PingMessage generates a WebSocket ping payload with a timestamp.
func PingMessage() []byte {
	return []byte(fmt.Sprintf("ping-%d", time.Now().UnixNano()))
}

// ValidatePong checks if a pong payload matches the expected format.
func ValidatePong(data []byte) bool {
	return len(data) > 5 && string(data[:5]) == "ping-"
}

// WSConnectionInfo provides diagnostic information about a WebSocket connection.
type WSConnectionInfo struct {
	ID              uint64 `json:"id"`
	Subscriptions   int    `json:"subscriptions"`
	RateRemaining   int    `json:"rateRemaining"`
	ConnectedSince  int64  `json:"connectedSince"`
}

// Info returns diagnostic information about this connection.
func (conn *WSConn) Info() WSConnectionInfo {
	conn.mu.Lock()
	subCount := len(conn.subscriptions)
	conn.mu.Unlock()

	return WSConnectionInfo{
		ID:             conn.id,
		Subscriptions:  subCount,
		RateRemaining:  conn.rateLimiter.Remaining(),
		ConnectedSince: conn.createdAt.Unix(),
	}
}

// BroadcastToSubscribers sends a notification to all connections that have
// a matching subscription. This is called when new chain events occur.
func (h *WSHandler) BroadcastToSubscribers(subType string, data interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	notification, _ := json.Marshal(data)

	for _, conn := range h.connections {
		if conn.closed.Load() {
			continue
		}
		conn.mu.Lock()
		for subID := range conn.subscriptions {
			// Format as eth_subscription notification.
			notif := FormatWSNotification(subID, json.RawMessage(notification))
			msg, _ := json.Marshal(notif)
			select {
			case conn.sendCh <- msg:
			default:
				// Drop if send buffer is full; connection is too slow.
			}
		}
		conn.mu.Unlock()
	}
}
