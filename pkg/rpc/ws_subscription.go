package rpc

import (
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/eth2028/eth2028/crypto"
)

// SubscriptionConfig configures the WebSocket subscription manager.
type SubscriptionConfig struct {
	// MaxSubscriptions is the maximum number of concurrent subscriptions.
	MaxSubscriptions int
	// BufferSize is the channel buffer size for each subscription.
	BufferSize int
	// CleanupInterval is the number of seconds between automatic cleanup runs.
	CleanupInterval int64
}

// DefaultSubscriptionConfig returns a SubscriptionConfig with sensible defaults.
func DefaultSubscriptionConfig() SubscriptionConfig {
	return SubscriptionConfig{
		MaxSubscriptions: 256,
		BufferSize:       128,
		CleanupInterval:  300, // 5 minutes
	}
}

// supportedSubTypes lists the valid subscription types.
var supportedSubTypes = map[string]bool{
	"newHeads":                true,
	"logs":                   true,
	"newPendingTransactions": true,
	"syncing":                true,
}

// WSSubscription represents an active WebSocket subscription with
// string-based type and flexible filter criteria.
type WSSubscription struct {
	ID             string
	Type           string
	FilterCriteria map[string]interface{}
	CreatedAt      int64
	Active         bool
	ch             chan interface{}
}

// Channel returns the notification channel for this subscription.
func (s *WSSubscription) Channel() <-chan interface{} {
	return s.ch
}

// WSSubscriptionManager manages WebSocket subscriptions for real-time
// event streaming. It is thread-safe.
type WSSubscriptionManager struct {
	mu      sync.Mutex
	config  SubscriptionConfig
	subs    map[string]*WSSubscription
	nextSeq uint64
	closed  bool
}

// NewWSSubscriptionManager creates a new subscription manager with
// the given configuration.
func NewWSSubscriptionManager(config SubscriptionConfig) *WSSubscriptionManager {
	if config.MaxSubscriptions <= 0 {
		config.MaxSubscriptions = 256
	}
	if config.BufferSize <= 0 {
		config.BufferSize = 128
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 300
	}
	return &WSSubscriptionManager{
		config: config,
		subs:   make(map[string]*WSSubscription),
	}
}

// generateSubID produces a unique hex subscription ID.
func (m *WSSubscriptionManager) generateSubID() string {
	m.nextSeq++
	buf := make([]byte, 16)
	seq := m.nextSeq
	ts := uint64(time.Now().UnixNano())
	for i := 0; i < 8; i++ {
		buf[i] = byte(seq >> (8 * i))
		buf[8+i] = byte(ts >> (8 * i))
	}
	h := crypto.Keccak256(buf)
	return "0x" + hex.EncodeToString(h[:16])
}

// Subscribe creates a new subscription of the given type with optional
// filter criteria. Returns the subscription ID or an error if the type
// is unsupported or the maximum subscription count has been reached.
func (m *WSSubscriptionManager) Subscribe(subType string, criteria map[string]interface{}) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return "", errors.New("subscription manager is closed")
	}
	if !supportedSubTypes[subType] {
		return "", errors.New("unsupported subscription type: " + subType)
	}
	if len(m.subs) >= m.config.MaxSubscriptions {
		return "", errors.New("maximum subscription count reached")
	}

	id := m.generateSubID()
	sub := &WSSubscription{
		ID:             id,
		Type:           subType,
		FilterCriteria: criteria,
		CreatedAt:      time.Now().Unix(),
		Active:         true,
		ch:             make(chan interface{}, m.config.BufferSize),
	}
	m.subs[id] = sub
	return id, nil
}

// Unsubscribe removes a subscription by ID and closes its channel.
// Returns an error if the subscription does not exist.
func (m *WSSubscriptionManager) Unsubscribe(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sub, ok := m.subs[id]
	if !ok {
		return errors.New("subscription not found: " + id)
	}
	sub.Active = false
	close(sub.ch)
	delete(m.subs, id)
	return nil
}

// GetSubscription returns subscription details by ID, or nil if not found.
func (m *WSSubscriptionManager) GetSubscription(id string) *WSSubscription {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.subs[id]
}

// ActiveSubscriptions returns a snapshot of all active subscriptions.
func (m *WSSubscriptionManager) ActiveSubscriptions() []WSSubscription {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]WSSubscription, 0, len(m.subs))
	for _, sub := range m.subs {
		if sub.Active {
			result = append(result, WSSubscription{
				ID:             sub.ID,
				Type:           sub.Type,
				FilterCriteria: sub.FilterCriteria,
				CreatedAt:      sub.CreatedAt,
				Active:         sub.Active,
			})
		}
	}
	return result
}

// PublishEvent sends an event to all subscriptions matching the given type.
// Events are dropped (not blocking) if a subscriber's buffer is full.
func (m *WSSubscriptionManager) PublishEvent(eventType string, data interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, sub := range m.subs {
		if !sub.Active || sub.Type != eventType {
			continue
		}
		select {
		case sub.ch <- data:
		default:
			// Drop if buffer is full; subscriber is too slow.
		}
	}
}

// SubscriberCount returns the number of active subscribers for the
// given event type.
func (m *WSSubscriptionManager) SubscriberCount(eventType string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, sub := range m.subs {
		if sub.Active && sub.Type == eventType {
			count++
		}
	}
	return count
}

// Cleanup removes subscriptions that are no longer active. In a full
// implementation this would also remove subscriptions whose WebSocket
// connections have been closed. Currently marks inactive subscriptions
// older than CleanupInterval seconds as dead and removes them.
func (m *WSSubscriptionManager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().Unix()
	for id, sub := range m.subs {
		if !sub.Active {
			close(sub.ch)
			delete(m.subs, id)
			continue
		}
		// Remove subscriptions older than the cleanup interval that
		// have a full buffer (likely dead consumers).
		age := now - sub.CreatedAt
		if age > m.config.CleanupInterval && len(sub.ch) == cap(sub.ch) {
			sub.Active = false
			close(sub.ch)
			delete(m.subs, id)
		}
	}
}

// Close shuts down all active subscriptions and prevents new ones
// from being created.
func (m *WSSubscriptionManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	for id, sub := range m.subs {
		sub.Active = false
		close(sub.ch)
		delete(m.subs, id)
	}
}
