// subscription_dispatcher.go implements a subscription event dispatcher
// for WebSocket connections. It provides multi-topic broadcast with
// per-client rate limiting, stale subscription cleanup, and statistics
// tracking. This complements the existing SubRegistry and WSSubscriptionManager
// by adding a higher-level dispatch layer.
package rpc

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eth2030/eth2030/crypto"
)

// Dispatcher errors.
var (
	ErrDispatcherClosed       = errors.New("dispatcher: closed")
	ErrDispatcherClientLimit  = errors.New("dispatcher: client subscription limit exceeded")
	ErrDispatcherEventLimit   = errors.New("dispatcher: event rate limit exceeded")
	ErrDispatcherSubNotFound  = errors.New("dispatcher: subscription not found")
	ErrDispatcherInvalidTopic = errors.New("dispatcher: invalid subscription topic")
	ErrDispatcherDuplicate    = errors.New("dispatcher: duplicate subscription")
)

// SubscriptionTopic identifies a category of real-time events.
type SubscriptionTopic string

const (
	TopicNewHeads   SubscriptionTopic = "newHeads"
	TopicLogs       SubscriptionTopic = "logs"
	TopicPendingTxs SubscriptionTopic = "newPendingTransactions"
	TopicSyncing    SubscriptionTopic = "syncing"
)

// validTopics is the set of recognized subscription topics.
var validTopics = map[SubscriptionTopic]bool{
	TopicNewHeads:   true,
	TopicLogs:       true,
	TopicPendingTxs: true,
	TopicSyncing:    true,
}

// IsValidTopic returns whether the topic is recognized.
func IsValidTopic(topic SubscriptionTopic) bool {
	return validTopics[topic]
}

// DispatchSubscription represents a single active subscription managed
// by the dispatcher.
type DispatchSubscription struct {
	ID        string
	ClientID  string
	Topic     SubscriptionTopic
	Filter    interface{} // Topic-specific filter (e.g., log filter criteria).
	Created   time.Time
	LastEvent time.Time
	Events    uint64 // Total events delivered.
	ch        chan interface{}
}

// Channel returns the read-only notification channel.
func (ds *DispatchSubscription) Channel() <-chan interface{} {
	return ds.ch
}

// DispatcherConfig configures the subscription dispatcher.
type DispatcherConfig struct {
	MaxSubsPerClient int           // Max subscriptions per client ID.
	MaxEventsPerSec  int           // Max events per second per client.
	RateWindow       time.Duration // Rate limit window duration.
	BufferSize       int           // Channel buffer size per subscription.
	MaxTotalSubs     int           // Global maximum subscriptions (0 = unlimited).
}

// DefaultDispatcherConfig returns sensible defaults.
func DefaultDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		MaxSubsPerClient: 32,
		MaxEventsPerSec:  1000,
		RateWindow:       time.Second,
		BufferSize:       128,
		MaxTotalSubs:     4096,
	}
}

// clientState tracks per-client rate limiting and subscription counts.
type clientState struct {
	subCount    int
	eventCount  int
	windowStart time.Time
}

// SubStats holds per-topic counts and global totals.
type SubStats struct {
	NewHeads   int
	Logs       int
	PendingTxs int
	Syncing    int
	Total      int
	Clients    int
}

// SubscriptionDispatcher manages active subscriptions across multiple
// WebSocket clients with rate limiting and lifecycle tracking.
// All methods are safe for concurrent use.
type SubscriptionDispatcher struct {
	mu      sync.Mutex
	config  DispatcherConfig
	subs    map[string]*DispatchSubscription // Keyed by subscription ID.
	clients map[string]*clientState          // Keyed by client ID.
	nextSeq uint64
	closed  bool
}

// NewSubscriptionDispatcher creates a new dispatcher with the given config.
func NewSubscriptionDispatcher(config DispatcherConfig) *SubscriptionDispatcher {
	if config.BufferSize <= 0 {
		config.BufferSize = 128
	}
	if config.MaxSubsPerClient <= 0 {
		config.MaxSubsPerClient = 32
	}
	if config.RateWindow <= 0 {
		config.RateWindow = time.Second
	}
	return &SubscriptionDispatcher{
		config:  config,
		subs:    make(map[string]*DispatchSubscription),
		clients: make(map[string]*clientState),
	}
}

// generateID produces a unique hex subscription ID.
func (d *SubscriptionDispatcher) generateID() string {
	d.nextSeq++
	buf := make([]byte, 16)
	seq := d.nextSeq
	ts := uint64(time.Now().UnixNano())
	for i := 0; i < 8; i++ {
		buf[i] = byte(seq >> (8 * i))
		buf[8+i] = byte(ts >> (8 * i))
	}
	h := crypto.Keccak256(buf)
	return "0x" + hex.EncodeToString(h[:16])
}

// Subscribe creates a new subscription for the given client and topic.
// Returns the subscription or an error if limits are exceeded.
func (d *SubscriptionDispatcher) Subscribe(clientID string, topic SubscriptionTopic, filter interface{}) (*DispatchSubscription, error) {
	if !IsValidTopic(topic) {
		return nil, fmt.Errorf("%w: %s", ErrDispatcherInvalidTopic, topic)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil, ErrDispatcherClosed
	}

	// Check global limit.
	if d.config.MaxTotalSubs > 0 && len(d.subs) >= d.config.MaxTotalSubs {
		return nil, ErrDispatcherClientLimit
	}

	// Check per-client limit.
	cs := d.clients[clientID]
	if cs == nil {
		cs = &clientState{windowStart: time.Now()}
		d.clients[clientID] = cs
	}
	if cs.subCount >= d.config.MaxSubsPerClient {
		return nil, ErrDispatcherClientLimit
	}

	id := d.generateID()
	sub := &DispatchSubscription{
		ID:       id,
		ClientID: clientID,
		Topic:    topic,
		Filter:   filter,
		Created:  time.Now(),
		ch:       make(chan interface{}, d.config.BufferSize),
	}
	d.subs[id] = sub
	cs.subCount++

	return sub, nil
}

// Unsubscribe removes a subscription by ID and closes its channel.
func (d *SubscriptionDispatcher) Unsubscribe(subID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	sub, ok := d.subs[subID]
	if !ok {
		return ErrDispatcherSubNotFound
	}

	close(sub.ch)
	delete(d.subs, subID)

	if cs := d.clients[sub.ClientID]; cs != nil {
		cs.subCount--
		if cs.subCount <= 0 {
			delete(d.clients, sub.ClientID)
		}
	}
	return nil
}

// Broadcast sends data to all subscriptions matching the given topic.
// Events are dropped (not blocking) if a subscriber's buffer is full.
// Per-client rate limiting is enforced.
func (d *SubscriptionDispatcher) Broadcast(topic SubscriptionTopic, data interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return
	}

	now := time.Now()
	for _, sub := range d.subs {
		if sub.Topic != topic {
			continue
		}

		// Check client rate limit.
		cs := d.clients[sub.ClientID]
		if cs != nil && d.config.MaxEventsPerSec > 0 {
			if now.Sub(cs.windowStart) >= d.config.RateWindow {
				cs.eventCount = 0
				cs.windowStart = now
			}
			cs.eventCount++
			if cs.eventCount > d.config.MaxEventsPerSec {
				continue // Rate limited; skip this event.
			}
		}

		select {
		case sub.ch <- data:
			sub.LastEvent = now
			sub.Events++
		default:
			// Buffer full; drop event.
		}
	}
}

// GetSubscriptions returns all active subscriptions for the given client.
func (d *SubscriptionDispatcher) GetSubscriptions(clientID string) []*DispatchSubscription {
	d.mu.Lock()
	defer d.mu.Unlock()

	var result []*DispatchSubscription
	for _, sub := range d.subs {
		if sub.ClientID == clientID {
			// Return a shallow copy to avoid exposing the channel.
			cp := &DispatchSubscription{
				ID:        sub.ID,
				ClientID:  sub.ClientID,
				Topic:     sub.Topic,
				Filter:    sub.Filter,
				Created:   sub.Created,
				LastEvent: sub.LastEvent,
				Events:    sub.Events,
			}
			result = append(result, cp)
		}
	}
	return result
}

// GetSubscription returns a single subscription by ID, or nil.
func (d *SubscriptionDispatcher) GetSubscription(subID string) *DispatchSubscription {
	d.mu.Lock()
	defer d.mu.Unlock()

	sub, ok := d.subs[subID]
	if !ok {
		return nil
	}
	return sub
}

// CleanupStale removes subscriptions that have not received an event
// within the given maxAge since their creation. Returns the count removed.
func (d *SubscriptionDispatcher) CleanupStale(maxAge time.Duration) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	removed := 0
	for id, sub := range d.subs {
		// Subscription is stale if it was created more than maxAge ago
		// and has never received an event (or the last event was too old).
		age := now.Sub(sub.Created)
		if age < maxAge {
			continue
		}
		lastActivity := sub.LastEvent
		if lastActivity.IsZero() {
			lastActivity = sub.Created
		}
		if now.Sub(lastActivity) >= maxAge {
			close(sub.ch)
			delete(d.subs, id)
			if cs := d.clients[sub.ClientID]; cs != nil {
				cs.subCount--
				if cs.subCount <= 0 {
					delete(d.clients, sub.ClientID)
				}
			}
			removed++
		}
	}
	return removed
}

// DisconnectClient removes all subscriptions for the given client ID.
// Returns the number of subscriptions removed.
func (d *SubscriptionDispatcher) DisconnectClient(clientID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	removed := 0
	for id, sub := range d.subs {
		if sub.ClientID == clientID {
			close(sub.ch)
			delete(d.subs, id)
			removed++
		}
	}
	delete(d.clients, clientID)
	return removed
}

// SubscriptionStats returns per-topic counts and overall statistics.
func (d *SubscriptionDispatcher) SubscriptionStats() *SubStats {
	d.mu.Lock()
	defer d.mu.Unlock()

	stats := &SubStats{
		Total:   len(d.subs),
		Clients: len(d.clients),
	}
	for _, sub := range d.subs {
		switch sub.Topic {
		case TopicNewHeads:
			stats.NewHeads++
		case TopicLogs:
			stats.Logs++
		case TopicPendingTxs:
			stats.PendingTxs++
		case TopicSyncing:
			stats.Syncing++
		}
	}
	return stats
}

// TotalSubscriptions returns the total number of active subscriptions.
func (d *SubscriptionDispatcher) TotalSubscriptions() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.subs)
}

// ClientCount returns the number of distinct clients with subscriptions.
func (d *SubscriptionDispatcher) ClientCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.clients)
}

// Close shuts down all subscriptions and prevents new ones.
func (d *SubscriptionDispatcher) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.closed = true
	for id, sub := range d.subs {
		close(sub.ch)
		delete(d.subs, id)
	}
	d.clients = make(map[string]*clientState)
}

// IsClosed returns whether the dispatcher has been closed.
func (d *SubscriptionDispatcher) IsClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

// CheckClientRateLimit returns true if the client is within the event
// rate limit, false if the client has exceeded it.
func (d *SubscriptionDispatcher) CheckClientRateLimit(clientID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	cs := d.clients[clientID]
	if cs == nil {
		return true
	}

	now := time.Now()
	if now.Sub(cs.windowStart) >= d.config.RateWindow {
		cs.eventCount = 0
		cs.windowStart = now
	}
	return cs.eventCount < d.config.MaxEventsPerSec
}
