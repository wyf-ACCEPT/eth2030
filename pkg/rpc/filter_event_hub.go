// filter_event_hub.go implements an event-driven filter dispatch system that
// bridges chain events (new blocks, logs, pending transactions) to the
// various filter subsystems (FilterSystem, LogFilterEngine, SubscriptionManager,
// WSSubscriptionManager). It provides a unified event hub with automatic
// expiry, per-filter rate limiting, and an event replay mechanism for
// reorgs.
//
// The EventHub decouples event producers (chain importers, txpool) from
// event consumers (RPC filters, WebSocket subscriptions), enabling
// fan-out delivery without tight coupling.
package rpc

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// EventHub errors.
var (
	ErrHubClosed          = errors.New("event hub is closed")
	ErrHubListenerFull    = errors.New("listener buffer full")
	ErrHubInvalidListener = errors.New("invalid listener ID")
	ErrHubDuplicateID     = errors.New("duplicate listener ID")
)

// ChainEventType distinguishes the kind of chain event dispatched through
// the hub. Each type maps to a concrete payload in ChainEvent.
type ChainEventType int

const (
	// EventNewBlock signals a new block has been imported.
	EventNewBlock ChainEventType = iota
	// EventNewLogs signals new log entries from a block.
	EventNewLogs
	// EventPendingTx signals a new pending transaction.
	EventPendingTx
	// EventReorg signals a chain reorganization.
	EventReorg
	// EventFilterExpired signals a filter was expired by the hub.
	EventFilterExpired
)

// String returns a human-readable name for the event type.
func (t ChainEventType) String() string {
	switch t {
	case EventNewBlock:
		return "NewBlock"
	case EventNewLogs:
		return "NewLogs"
	case EventPendingTx:
		return "PendingTx"
	case EventReorg:
		return "Reorg"
	case EventFilterExpired:
		return "FilterExpired"
	default:
		return "Unknown"
	}
}

// ChainEvent is a polymorphic event dispatched through the EventHub.
// Exactly one payload field is populated depending on the Type.
type ChainEvent struct {
	Type      ChainEventType
	Timestamp time.Time

	// EventNewBlock payload.
	BlockHash   types.Hash
	BlockNumber uint64
	Header      *types.Header

	// EventNewLogs payload.
	Logs []*types.Log

	// EventPendingTx payload.
	TxHash types.Hash

	// EventReorg payload.
	OldHead    types.Hash
	NewHead    types.Hash
	ReorgDepth uint64
}

// EventListener receives events from the hub. Listeners are identified
// by a unique string ID and receive events on a buffered channel.
type EventListener struct {
	ID       string
	Types    map[ChainEventType]bool // event types this listener cares about
	Ch       chan ChainEvent
	Created  time.Time
	LastRecv time.Time
}

// EventHubConfig configures the event hub.
type EventHubConfig struct {
	// MaxListeners is the maximum number of concurrent listeners.
	MaxListeners int
	// ListenerBuffer is the channel buffer size per listener.
	ListenerBuffer int
	// ExpiryInterval is how often the hub runs expiry checks.
	ExpiryInterval time.Duration
	// ListenerTimeout removes listeners that have not consumed events
	// within this duration (zero disables timeout).
	ListenerTimeout time.Duration
	// MaxReplayDepth is the maximum number of recent events kept for replay.
	MaxReplayDepth int
}

// DefaultEventHubConfig returns sensible defaults for the event hub.
func DefaultEventHubConfig() EventHubConfig {
	return EventHubConfig{
		MaxListeners:    256,
		ListenerBuffer:  64,
		ExpiryInterval:  30 * time.Second,
		ListenerTimeout: 5 * time.Minute,
		MaxReplayDepth:  128,
	}
}

// EventHub dispatches chain events to registered listeners with fan-out
// delivery. Thread-safe: all public methods are safe for concurrent use.
type EventHub struct {
	mu        sync.RWMutex
	config    EventHubConfig
	listeners map[string]*EventListener
	replay    []ChainEvent // ring buffer of recent events
	nextSeq   uint64
	closed    int32 // atomic: 1 if closed
	stats     EventHubStats
}

// EventHubStats tracks dispatch statistics.
type EventHubStats struct {
	EventsDispatched uint64
	EventsDropped    uint64
	ListenersAdded   uint64
	ListenersRemoved uint64
	ListenersExpired uint64
}

// NewEventHub creates a new event hub with the given configuration.
func NewEventHub(config EventHubConfig) *EventHub {
	if config.MaxListeners <= 0 {
		config.MaxListeners = 256
	}
	if config.ListenerBuffer <= 0 {
		config.ListenerBuffer = 64
	}
	if config.MaxReplayDepth <= 0 {
		config.MaxReplayDepth = 128
	}
	return &EventHub{
		config:    config,
		listeners: make(map[string]*EventListener),
		replay:    make([]ChainEvent, 0, config.MaxReplayDepth),
	}
}

// Register adds a new listener to the hub. The listener will receive
// events matching any of the specified types. Returns the listener or
// an error if the hub is closed or full.
func (hub *EventHub) Register(eventTypes []ChainEventType) (*EventListener, error) {
	if atomic.LoadInt32(&hub.closed) == 1 {
		return nil, ErrHubClosed
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()

	if len(hub.listeners) >= hub.config.MaxListeners {
		return nil, ErrHubListenerFull
	}

	id := hub.generateID()
	typeMap := make(map[ChainEventType]bool, len(eventTypes))
	for _, t := range eventTypes {
		typeMap[t] = true
	}

	now := time.Now()
	listener := &EventListener{
		ID:       id,
		Types:    typeMap,
		Ch:       make(chan ChainEvent, hub.config.ListenerBuffer),
		Created:  now,
		LastRecv: now,
	}
	hub.listeners[id] = listener
	hub.stats.ListenersAdded++

	return listener, nil
}

// Unregister removes a listener by ID. Returns true if the listener
// existed and was removed.
func (hub *EventHub) Unregister(id string) bool {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	listener, ok := hub.listeners[id]
	if !ok {
		return false
	}
	close(listener.Ch)
	delete(hub.listeners, id)
	hub.stats.ListenersRemoved++
	return true
}

// Dispatch sends an event to all matching listeners. Events are dropped
// (not blocking) if a listener's buffer is full.
func (hub *EventHub) Dispatch(event ChainEvent) error {
	if atomic.LoadInt32(&hub.closed) == 1 {
		return ErrHubClosed
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()

	// Append to replay buffer.
	if len(hub.replay) >= hub.config.MaxReplayDepth {
		hub.replay = hub.replay[1:]
	}
	hub.replay = append(hub.replay, event)

	// Fan-out to matching listeners.
	for _, listener := range hub.listeners {
		if !listener.Types[event.Type] {
			continue
		}
		select {
		case listener.Ch <- event:
			listener.LastRecv = time.Now()
			hub.stats.EventsDispatched++
		default:
			hub.stats.EventsDropped++
		}
	}

	return nil
}

// DispatchBlock is a convenience method that dispatches both a NewBlock event
// and a NewLogs event (if logs are provided) for a newly imported block.
func (hub *EventHub) DispatchBlock(header *types.Header, logs []*types.Log) error {
	if header == nil {
		return nil
	}

	blockHash := header.Hash()
	blockNum := header.Number.Uint64()

	err := hub.Dispatch(ChainEvent{
		Type:        EventNewBlock,
		BlockHash:   blockHash,
		BlockNumber: blockNum,
		Header:      header,
	})
	if err != nil {
		return err
	}

	if len(logs) > 0 {
		return hub.Dispatch(ChainEvent{
			Type:        EventNewLogs,
			BlockHash:   blockHash,
			BlockNumber: blockNum,
			Logs:        logs,
		})
	}
	return nil
}

// DispatchPendingTx dispatches a pending transaction event.
func (hub *EventHub) DispatchPendingTx(txHash types.Hash) error {
	return hub.Dispatch(ChainEvent{
		Type:   EventPendingTx,
		TxHash: txHash,
	})
}

// DispatchReorg dispatches a chain reorganization event.
func (hub *EventHub) DispatchReorg(oldHead, newHead types.Hash, depth uint64) error {
	return hub.Dispatch(ChainEvent{
		Type:       EventReorg,
		OldHead:    oldHead,
		NewHead:    newHead,
		ReorgDepth: depth,
	})
}

// Replay returns the most recent events from the replay buffer, filtered
// by the specified event types. If types is nil, all events are returned.
func (hub *EventHub) Replay(eventTypes []ChainEventType, maxEvents int) []ChainEvent {
	hub.mu.RLock()
	defer hub.mu.RUnlock()

	typeFilter := make(map[ChainEventType]bool, len(eventTypes))
	for _, t := range eventTypes {
		typeFilter[t] = true
	}

	var result []ChainEvent
	for i := len(hub.replay) - 1; i >= 0 && len(result) < maxEvents; i-- {
		ev := hub.replay[i]
		if len(typeFilter) == 0 || typeFilter[ev.Type] {
			result = append(result, ev)
		}
	}

	// Reverse to chronological order.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

// ExpireStale removes listeners that have not received events within the
// configured timeout. Returns the number of expired listeners.
func (hub *EventHub) ExpireStale() int {
	if hub.config.ListenerTimeout == 0 {
		return 0
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()

	now := time.Now()
	expired := 0
	for id, listener := range hub.listeners {
		if now.Sub(listener.LastRecv) > hub.config.ListenerTimeout {
			close(listener.Ch)
			delete(hub.listeners, id)
			expired++
		}
	}
	hub.stats.ListenersExpired += uint64(expired)
	return expired
}

// Close shuts down the event hub and all listeners.
func (hub *EventHub) Close() {
	if !atomic.CompareAndSwapInt32(&hub.closed, 0, 1) {
		return
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()

	for id, listener := range hub.listeners {
		close(listener.Ch)
		delete(hub.listeners, id)
	}
}

// ListenerCount returns the number of active listeners.
func (hub *EventHub) ListenerCount() int {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	return len(hub.listeners)
}

// Stats returns a snapshot of dispatch statistics.
func (hub *EventHub) Stats() EventHubStats {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	return hub.stats
}

// ReplaySize returns the current number of events in the replay buffer.
func (hub *EventHub) ReplaySize() int {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	return len(hub.replay)
}

// generateID produces a unique listener ID. Caller must hold hub.mu.
func (hub *EventHub) generateID() string {
	hub.nextSeq++
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], hub.nextSeq)
	binary.LittleEndian.PutUint64(buf[8:], uint64(time.Now().UnixNano()))
	h := crypto.Keccak256Hash(buf[:])
	return h.Hex()
}
