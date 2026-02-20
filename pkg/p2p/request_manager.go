// request_manager.go implements request/response lifecycle management for P2P
// protocols with timeout tracking, retry logic, and exponential backoff.
package p2p

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Errors returned by RequestManager operations.
var (
	ErrReqMgrClosed      = errors.New("p2p: request manager closed")
	ErrReqMgrMaxPending  = errors.New("p2p: max pending requests reached")
	ErrReqMgrNotFound    = errors.New("p2p: request not found")
	ErrReqMgrMaxRetries  = errors.New("p2p: max retries exceeded")
	ErrReqMgrDupRequest  = errors.New("p2p: duplicate request ID")
)

// RequestManagerConfig configures the RequestManager.
type RequestManagerConfig struct {
	DefaultTimeout time.Duration // Default timeout for outgoing requests.
	MaxPending     int           // Maximum number of concurrent pending requests.
	MaxRetries     int           // Maximum retry attempts per request (0 = no retries).
	BaseBackoff    time.Duration // Base duration for exponential backoff on retries.
	MaxBackoff     time.Duration // Maximum backoff duration cap.
}

// DefaultRequestManagerConfig returns sensible defaults.
func DefaultRequestManagerConfig() RequestManagerConfig {
	return RequestManagerConfig{
		DefaultTimeout: 15 * time.Second,
		MaxPending:     256,
		MaxRetries:     3,
		BaseBackoff:    500 * time.Millisecond,
		MaxBackoff:     30 * time.Second,
	}
}

// OutboundRequest represents an in-flight outgoing request with retry state.
type OutboundRequest struct {
	ID        uint64
	PeerID    string
	MsgCode   uint64
	Data      []byte
	CreatedAt time.Time
	Deadline  time.Time
	Retries   int
	RespCh    chan []byte // Receives response data when delivered.
}

// RequestManager tracks outgoing requests with timeouts, retry logic with
// exponential backoff, and response correlation. All methods are thread-safe.
type RequestManager struct {
	mu       sync.Mutex
	config   RequestManagerConfig
	pending  map[uint64]*OutboundRequest
	nextID   atomic.Uint64
	closed   bool
	stopOnce sync.Once
	stop     chan struct{}
}

// NewRequestManager creates a request manager with the given config.
func NewRequestManager(cfg RequestManagerConfig) *RequestManager {
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 15 * time.Second
	}
	if cfg.MaxPending <= 0 {
		cfg.MaxPending = 256
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 500 * time.Millisecond
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 30 * time.Second
	}

	rm := &RequestManager{
		config:  cfg,
		pending: make(map[uint64]*OutboundRequest),
		stop:    make(chan struct{}),
	}
	go rm.expireLoop()
	return rm
}

// SendRequest creates a tracked outgoing request. Returns a response channel
// that receives the response data when delivered (or is closed on timeout).
// The caller is responsible for actually writing the message to the transport;
// this method only tracks the request's lifecycle.
func (rm *RequestManager) SendRequest(peerID string, msgCode uint64, data []byte) (uint64, <-chan []byte, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.closed {
		return 0, nil, ErrReqMgrClosed
	}
	if len(rm.pending) >= rm.config.MaxPending {
		return 0, nil, ErrReqMgrMaxPending
	}

	id := rm.nextID.Add(1)
	now := time.Now()
	respCh := make(chan []byte, 1)

	req := &OutboundRequest{
		ID:        id,
		PeerID:    peerID,
		MsgCode:   msgCode,
		Data:      data,
		CreatedAt: now,
		Deadline:  now.Add(rm.config.DefaultTimeout),
		Retries:   0,
		RespCh:    respCh,
	}
	rm.pending[id] = req

	return id, respCh, nil
}

// DeliverResponse delivers a response for a tracked request. The response
// data is sent to the request's channel. Returns ErrReqMgrNotFound if the
// request ID is not pending.
func (rm *RequestManager) DeliverResponse(requestID uint64, data []byte) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	req, ok := rm.pending[requestID]
	if !ok {
		return ErrReqMgrNotFound
	}
	delete(rm.pending, requestID)
	req.RespCh <- data
	close(req.RespCh)
	return nil
}

// CancelRequest removes a tracked request without delivering a response.
// The request's channel is closed to unblock any waiters.
func (rm *RequestManager) CancelRequest(requestID uint64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if req, ok := rm.pending[requestID]; ok {
		delete(rm.pending, requestID)
		close(req.RespCh)
	}
}

// RetryRequest re-issues a request with exponential backoff. Returns the new
// deadline and the backoff duration that should be waited before retransmitting.
// Returns ErrReqMgrMaxRetries if the maximum retries are exceeded.
func (rm *RequestManager) RetryRequest(requestID uint64) (time.Duration, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	req, ok := rm.pending[requestID]
	if !ok {
		return 0, ErrReqMgrNotFound
	}
	if req.Retries >= rm.config.MaxRetries {
		delete(rm.pending, requestID)
		close(req.RespCh)
		return 0, ErrReqMgrMaxRetries
	}

	req.Retries++
	backoff := rm.calculateBackoff(req.Retries)
	req.Deadline = time.Now().Add(rm.config.DefaultTimeout + backoff)

	return backoff, nil
}

// calculateBackoff computes exponential backoff: base * 2^(attempt-1), capped at max.
func (rm *RequestManager) calculateBackoff(attempt int) time.Duration {
	// Cap the exponent to avoid overflow before multiplication.
	exp := attempt - 1
	if exp < 0 {
		exp = 0
	}
	if exp > 62 {
		return rm.config.MaxBackoff
	}
	factor := math.Pow(2, float64(exp))
	backoff := time.Duration(float64(rm.config.BaseBackoff) * factor)
	if backoff > rm.config.MaxBackoff || backoff < 0 {
		backoff = rm.config.MaxBackoff
	}
	return backoff
}

// ExpireTimeouts removes all pending requests whose deadline has passed.
// Their response channels are closed. Returns the number of expired requests.
func (rm *RequestManager) ExpireTimeouts() int {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	return rm.expireTimeoutsLocked()
}

// expireTimeoutsLocked removes expired requests. Caller must hold rm.mu.
func (rm *RequestManager) expireTimeoutsLocked() int {
	now := time.Now()
	expired := 0
	for id, req := range rm.pending {
		if now.After(req.Deadline) {
			delete(rm.pending, id)
			close(req.RespCh)
			expired++
		}
	}
	return expired
}

// PendingCount returns the number of in-flight requests.
func (rm *RequestManager) PendingCount() int {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return len(rm.pending)
}

// PendingForPeer returns the number of pending requests to a specific peer.
func (rm *RequestManager) PendingForPeer(peerID string) int {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	count := 0
	for _, req := range rm.pending {
		if req.PeerID == peerID {
			count++
		}
	}
	return count
}

// GetRequest returns a copy of a pending request's metadata. Returns nil if
// the request is not found.
func (rm *RequestManager) GetRequest(requestID uint64) *OutboundRequest {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	req, ok := rm.pending[requestID]
	if !ok {
		return nil
	}
	// Return a copy (excluding channel).
	cp := *req
	cp.RespCh = nil
	cp.Data = make([]byte, len(req.Data))
	copy(cp.Data, req.Data)
	return &cp
}

// Close stops the expire loop and cancels all pending requests.
func (rm *RequestManager) Close() {
	rm.stopOnce.Do(func() {
		close(rm.stop)
		rm.mu.Lock()
		rm.closed = true
		for id, req := range rm.pending {
			delete(rm.pending, id)
			close(req.RespCh)
		}
		rm.mu.Unlock()
	})
}

// expireLoop periodically checks for timed-out requests.
func (rm *RequestManager) expireLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stop:
			return
		case <-ticker.C:
			rm.mu.Lock()
			rm.expireTimeoutsLocked()
			rm.mu.Unlock()
		}
	}
}

// BackoffDuration returns the calculated backoff for a given attempt number.
// Exported for testing and informational use.
func (rm *RequestManager) BackoffDuration(attempt int) time.Duration {
	return rm.calculateBackoff(attempt)
}
