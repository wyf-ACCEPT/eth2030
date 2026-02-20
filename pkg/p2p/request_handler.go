package p2p

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Errors returned by RequestHandler.
var (
	ErrNoMessageHandler  = errors.New("p2p: no handler registered for message code")
	ErrHandlerTimeout    = errors.New("p2p: handler timed out")
	ErrNilMessageHandler = errors.New("p2p: nil message handler")
)

// Default request handler timeout.
const DefaultHandlerTimeout = 30 * time.Second

// MessageHandler is the interface for handling incoming P2P messages.
// Implementations process a message from a given peer and optionally
// return response bytes for request-response patterns.
type MessageHandler interface {
	Handle(peerID string, data []byte) ([]byte, error)
}

// MessageHandlerFunc is an adapter to allow use of ordinary functions
// as MessageHandler implementations.
type MessageHandlerFunc func(peerID string, data []byte) ([]byte, error)

// Handle calls f(peerID, data).
func (f MessageHandlerFunc) Handle(peerID string, data []byte) ([]byte, error) {
	return f(peerID, data)
}

// RequestHandlerStats tracks message handling statistics.
type RequestHandlerStats struct {
	MessagesReceived atomic.Uint64
	MessagesHandled  atomic.Uint64
	Errors           atomic.Uint64
}

// Snapshot returns a copy of the current stats values.
func (s *RequestHandlerStats) Snapshot() (received, handled, errors uint64) {
	return s.MessagesReceived.Load(), s.MessagesHandled.Load(), s.Errors.Load()
}

// RequestHandler dispatches incoming P2P messages to registered handlers
// based on message code. It supports request-response patterns where
// handlers return response bytes, configurable timeouts, and stats tracking.
type RequestHandler struct {
	mu       sync.RWMutex
	handlers map[uint64]MessageHandler
	timeout  time.Duration
	stats    RequestHandlerStats
}

// NewRequestHandler creates a new RequestHandler with the default timeout.
func NewRequestHandler() *RequestHandler {
	return &RequestHandler{
		handlers: make(map[uint64]MessageHandler),
		timeout:  DefaultHandlerTimeout,
	}
}

// RegisterHandler registers a MessageHandler for a specific message code.
// If a handler is already registered for the code, it is replaced.
// Returns an error if handler is nil.
func (rh *RequestHandler) RegisterHandler(msgCode uint64, handler MessageHandler) error {
	if handler == nil {
		return ErrNilMessageHandler
	}
	rh.mu.Lock()
	defer rh.mu.Unlock()
	rh.handlers[msgCode] = handler
	return nil
}

// UnregisterHandler removes the handler for a message code.
func (rh *RequestHandler) UnregisterHandler(msgCode uint64) {
	rh.mu.Lock()
	defer rh.mu.Unlock()
	delete(rh.handlers, msgCode)
}

// HasHandler returns true if a handler is registered for the message code.
func (rh *RequestHandler) HasHandler(msgCode uint64) bool {
	rh.mu.RLock()
	defer rh.mu.RUnlock()
	_, ok := rh.handlers[msgCode]
	return ok
}

// SetTimeout sets the timeout duration for handler execution.
// A non-positive duration disables the timeout.
func (rh *RequestHandler) SetTimeout(duration time.Duration) {
	rh.mu.Lock()
	defer rh.mu.Unlock()
	rh.timeout = duration
}

// HandleMessage dispatches a message to the handler registered for msgCode.
// It increments stats counters and enforces the configured timeout.
// Returns the handler's response bytes and any error.
func (rh *RequestHandler) HandleMessage(peerID string, msgCode uint64, data []byte) ([]byte, error) {
	rh.stats.MessagesReceived.Add(1)

	rh.mu.RLock()
	handler, ok := rh.handlers[msgCode]
	timeout := rh.timeout
	rh.mu.RUnlock()

	if !ok {
		rh.stats.Errors.Add(1)
		return nil, fmt.Errorf("%w: 0x%02x", ErrNoMessageHandler, msgCode)
	}

	// If timeout is non-positive, call handler directly without timeout.
	if timeout <= 0 {
		resp, err := handler.Handle(peerID, data)
		if err != nil {
			rh.stats.Errors.Add(1)
			return nil, err
		}
		rh.stats.MessagesHandled.Add(1)
		return resp, nil
	}

	// Execute handler with timeout.
	type result struct {
		resp []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := handler.Handle(peerID, data)
		ch <- result{resp, err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			rh.stats.Errors.Add(1)
			return nil, r.err
		}
		rh.stats.MessagesHandled.Add(1)
		return r.resp, nil
	case <-time.After(timeout):
		rh.stats.Errors.Add(1)
		return nil, fmt.Errorf("%w: code 0x%02x after %v", ErrHandlerTimeout, msgCode, timeout)
	}
}

// Stats returns a pointer to the handler's statistics. The counters are
// safe for concurrent reads via atomic operations.
func (rh *RequestHandler) Stats() *RequestHandlerStats {
	return &rh.stats
}

// HandlerCount returns the number of registered handlers.
func (rh *RequestHandler) HandlerCount() int {
	rh.mu.RLock()
	defer rh.mu.RUnlock()
	return len(rh.handlers)
}
