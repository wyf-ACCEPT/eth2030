package node

import (
	"sync"
	"sync/atomic"
	"time"
)

// EventType identifies the kind of event published on the bus.
type EventType string

// Common event types for subsystem communication.
const (
	EventNewBlock      EventType = "chain.newBlock"
	EventNewTx         EventType = "tx.new"
	EventChainHead     EventType = "chain.head"
	EventChainSideHead EventType = "chain.sideHead"
	EventNewPeer       EventType = "p2p.newPeer"
	EventDropPeer      EventType = "p2p.dropPeer"
	EventSyncStarted   EventType = "sync.started"
	EventSyncCompleted EventType = "sync.completed"
	EventTxPoolAdd     EventType = "txpool.add"
	EventTxPoolDrop    EventType = "txpool.drop"
)

// Event is a message published on the event bus.
type Event struct {
	Type      EventType
	Data      interface{}
	Timestamp time.Time
}

// Subscription represents a subscription to one or more event types
// on the EventBus.
type Subscription struct {
	id     uint64
	types  map[EventType]struct{}
	ch     chan Event
	bus    *EventBus
	closed atomic.Bool
}

// Chan returns a read-only channel that receives events matching the
// subscription's event types.
func (s *Subscription) Chan() <-chan Event {
	return s.ch
}

// Unsubscribe removes this subscription from the event bus and closes
// the underlying channel. It is safe to call multiple times.
func (s *Subscription) Unsubscribe() {
	if s.bus != nil {
		s.bus.Unsubscribe(s)
	}
}

// EventBus provides a publish/subscribe mechanism for loosely-coupled
// subsystem communication. All methods are safe for concurrent use.
type EventBus struct {
	mu         sync.RWMutex
	subs       map[uint64]*Subscription
	nextID     uint64
	bufferSize int
	closed     bool
}

// NewEventBus creates a new EventBus. bufferSize controls the channel
// buffer for each subscription; use 0 for unbuffered channels.
func NewEventBus(bufferSize int) *EventBus {
	if bufferSize < 0 {
		bufferSize = 0
	}
	return &EventBus{
		subs:       make(map[uint64]*Subscription),
		bufferSize: bufferSize,
	}
}

// Subscribe creates a subscription that receives events of the given type.
// Returns a Subscription whose Chan() delivers matching events.
func (eb *EventBus) Subscribe(eventType EventType) *Subscription {
	return eb.SubscribeMultiple(eventType)
}

// SubscribeMultiple creates a subscription that receives events matching
// any of the given types.
func (eb *EventBus) SubscribeMultiple(types ...EventType) *Subscription {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.closed {
		// Return a closed subscription.
		sub := &Subscription{
			ch:    make(chan Event),
			types: make(map[EventType]struct{}),
		}
		sub.closed.Store(true)
		close(sub.ch)
		return sub
	}

	eb.nextID++
	id := eb.nextID

	typeSet := make(map[EventType]struct{}, len(types))
	for _, t := range types {
		typeSet[t] = struct{}{}
	}

	sub := &Subscription{
		id:    id,
		types: typeSet,
		ch:    make(chan Event, eb.bufferSize),
		bus:   eb,
	}
	eb.subs[id] = sub
	return sub
}

// Unsubscribe removes the given subscription from the bus and closes
// its channel. Safe to call multiple times or with nil.
func (eb *EventBus) Unsubscribe(sub *Subscription) {
	if sub == nil {
		return
	}

	// Use atomic bool to ensure we only close the channel once,
	// even under concurrent calls.
	if !sub.closed.CompareAndSwap(false, true) {
		return
	}

	eb.mu.Lock()
	delete(eb.subs, sub.id)
	eb.mu.Unlock()

	close(sub.ch)
}

// Publish sends an event to all subscribers that match the given event type.
// It blocks if any subscriber's channel is full.
func (eb *EventBus) Publish(eventType EventType, data interface{}) {
	event := Event{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now(),
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if eb.closed {
		return
	}

	for _, sub := range eb.subs {
		if sub.closed.Load() {
			continue
		}
		if _, ok := sub.types[eventType]; ok {
			sub.ch <- event
		}
	}
}

// PublishAsync sends an event to all matching subscribers without blocking.
// If a subscriber's channel is full, the event is dropped for that subscriber.
func (eb *EventBus) PublishAsync(eventType EventType, data interface{}) {
	event := Event{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now(),
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if eb.closed {
		return
	}

	for _, sub := range eb.subs {
		if sub.closed.Load() {
			continue
		}
		if _, ok := sub.types[eventType]; ok {
			select {
			case sub.ch <- event:
			default:
				// Drop event for this subscriber (channel full).
			}
		}
	}
}

// SubscriberCount returns the number of active subscriptions for the
// given event type.
func (eb *EventBus) SubscriberCount(eventType EventType) int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	count := 0
	for _, sub := range eb.subs {
		if sub.closed.Load() {
			continue
		}
		if _, ok := sub.types[eventType]; ok {
			count++
		}
	}
	return count
}

// Close shuts down the event bus. All subscription channels are closed
// and no further events can be published.
func (eb *EventBus) Close() {
	eb.mu.Lock()
	if eb.closed {
		eb.mu.Unlock()
		return
	}
	eb.closed = true

	// Collect subscriptions to close outside the lock.
	toClose := make([]*Subscription, 0, len(eb.subs))
	for _, sub := range eb.subs {
		toClose = append(toClose, sub)
	}
	eb.subs = make(map[uint64]*Subscription)
	eb.mu.Unlock()

	for _, sub := range toClose {
		if sub.closed.CompareAndSwap(false, true) {
			close(sub.ch)
		}
	}
}
