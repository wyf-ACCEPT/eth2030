// subscription_manager.go implements a real-time subscription manager for
// WebSocket connections. It supports newHeads, logs, pendingTransactions,
// and syncing subscriptions with per-connection rate limiting and registry
// cleanup on disconnect.
package rpc

import (
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Subscription manager errors.
var (
	ErrSubManagerClosed     = errors.New("subscription manager: closed")
	ErrSubManagerCapacity   = errors.New("subscription manager: capacity reached")
	ErrSubManagerNotFound   = errors.New("subscription manager: subscription not found")
	ErrSubManagerRateLimit  = errors.New("subscription manager: rate limit exceeded")
	ErrSubManagerInvalidTyp = errors.New("subscription manager: invalid subscription type")
)

// SubKind identifies the type of real-time subscription.
type SubKind int

const (
	SubKindNewHeads  SubKind = iota // New block header notifications.
	SubKindLogs                     // Matching log event notifications.
	SubKindPendingTx                // Pending transaction notifications.
	SubKindSyncStatus               // Sync progress notifications.
)

// subKindNames maps SubKind to its eth_subscribe parameter name.
var subKindNames = map[string]SubKind{
	"newHeads":                SubKindNewHeads,
	"logs":                    SubKindLogs,
	"newPendingTransactions":  SubKindPendingTx,
	"syncing":                 SubKindSyncStatus,
}

// ParseSubKind converts a subscription type name to a SubKind.
// Returns ErrSubManagerInvalidTyp if the name is not recognized.
func ParseSubKind(name string) (SubKind, error) {
	kind, ok := subKindNames[name]
	if !ok {
		return 0, ErrSubManagerInvalidTyp
	}
	return kind, nil
}

// SubEntry is a single registered subscription.
type SubEntry struct {
	ID         string
	Kind       SubKind
	ConnID     string      // connection identifier for grouping
	Query      FilterQuery // only used for logs subscriptions
	CreatedAt  time.Time
	ch         chan interface{}
}

// Channel returns the notification channel.
func (s *SubEntry) Channel() <-chan interface{} {
	return s.ch
}

// SubRateLimitConfig configures per-connection subscription rate limiting.
type SubRateLimitConfig struct {
	MaxSubsPerConn  int           // max subscriptions per connection
	WindowDuration  time.Duration // rate limit window
	MaxEventsPerSec int           // max events per second per subscription
}

// DefaultSubRateLimitConfig returns sensible defaults.
func DefaultSubRateLimitConfig() SubRateLimitConfig {
	return SubRateLimitConfig{
		MaxSubsPerConn:  32,
		WindowDuration:  time.Second,
		MaxEventsPerSec: 1000,
	}
}

// connTracker tracks subscriptions per connection for rate limiting.
type connTracker struct {
	subCount    int
	lastEventAt time.Time
	eventCount  int
}

// SubRegistry manages active subscriptions across multiple connections.
type SubRegistry struct {
	mu          sync.Mutex
	subs        map[string]*SubEntry
	connTrackers map[string]*connTracker
	rateConfig  SubRateLimitConfig
	bufferSize  int
	nextSeq     uint64
	closed      bool
}

// NewSubRegistry creates a new subscription registry.
func NewSubRegistry(rateConfig SubRateLimitConfig, bufferSize int) *SubRegistry {
	if bufferSize <= 0 {
		bufferSize = 128
	}
	return &SubRegistry{
		subs:         make(map[string]*SubEntry),
		connTrackers: make(map[string]*connTracker),
		rateConfig:   rateConfig,
		bufferSize:   bufferSize,
	}
}

// generateSubID creates a unique hex subscription ID.
func (r *SubRegistry) generateSubID() string {
	r.nextSeq++
	buf := make([]byte, 16)
	seq := r.nextSeq
	ts := uint64(time.Now().UnixNano())
	for i := 0; i < 8; i++ {
		buf[i] = byte(seq >> (8 * i))
		buf[8+i] = byte(ts >> (8 * i))
	}
	h := crypto.Keccak256(buf)
	return "0x" + hex.EncodeToString(h[:16])
}

// NewHeadsSub creates a new heads subscription for the given connection.
func (r *SubRegistry) NewHeadsSub(connID string) (string, error) {
	return r.addSub(connID, SubKindNewHeads, FilterQuery{})
}

// LogsSub creates a logs subscription with the given filter for a connection.
func (r *SubRegistry) LogsSub(connID string, query FilterQuery) (string, error) {
	return r.addSub(connID, SubKindLogs, query)
}

// PendingTxSub creates a pending transaction subscription for a connection.
func (r *SubRegistry) PendingTxSub(connID string) (string, error) {
	return r.addSub(connID, SubKindPendingTx, FilterQuery{})
}

// SyncStatusSub creates a sync status subscription for a connection.
func (r *SubRegistry) SyncStatusSub(connID string) (string, error) {
	return r.addSub(connID, SubKindSyncStatus, FilterQuery{})
}

// addSub adds a subscription after checking rate limits.
func (r *SubRegistry) addSub(connID string, kind SubKind, query FilterQuery) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return "", ErrSubManagerClosed
	}

	// Check per-connection limit.
	tracker := r.connTrackers[connID]
	if tracker == nil {
		tracker = &connTracker{}
		r.connTrackers[connID] = tracker
	}
	if tracker.subCount >= r.rateConfig.MaxSubsPerConn {
		return "", ErrSubManagerRateLimit
	}

	id := r.generateSubID()
	entry := &SubEntry{
		ID:        id,
		Kind:      kind,
		ConnID:    connID,
		Query:     query,
		CreatedAt: time.Now(),
		ch:        make(chan interface{}, r.bufferSize),
	}
	r.subs[id] = entry
	tracker.subCount++

	return id, nil
}

// Unsubscribe removes a subscription by ID.
func (r *SubRegistry) Unsubscribe(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.subs[id]
	if !ok {
		return ErrSubManagerNotFound
	}

	close(entry.ch)
	delete(r.subs, id)

	if tracker := r.connTrackers[entry.ConnID]; tracker != nil {
		tracker.subCount--
		if tracker.subCount <= 0 {
			delete(r.connTrackers, entry.ConnID)
		}
	}
	return nil
}

// GetSub returns a subscription by ID, or nil if not found.
func (r *SubRegistry) GetSub(id string) *SubEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.subs[id]
}

// DisconnectConn removes all subscriptions for a given connection,
// cleaning up channels and tracker state.
func (r *SubRegistry) DisconnectConn(connID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	removed := 0
	for id, entry := range r.subs {
		if entry.ConnID == connID {
			close(entry.ch)
			delete(r.subs, id)
			removed++
		}
	}
	delete(r.connTrackers, connID)
	return removed
}

// Count returns the total number of active subscriptions.
func (r *SubRegistry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subs)
}

// CountByKind returns the number of subscriptions of a given kind.
func (r *SubRegistry) CountByKind(kind SubKind) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, entry := range r.subs {
		if entry.Kind == kind {
			count++
		}
	}
	return count
}

// ConnSubCount returns the number of subscriptions for a connection.
func (r *SubRegistry) ConnSubCount(connID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tracker := r.connTrackers[connID]; tracker != nil {
		return tracker.subCount
	}
	return 0
}

// NotifyNewHead sends a new header to all newHeads subscribers.
func (r *SubRegistry) NotifyNewHead(header *types.Header) {
	r.mu.Lock()
	defer r.mu.Unlock()

	formatted := FormatHeader(header)
	for _, entry := range r.subs {
		if entry.Kind == SubKindNewHeads {
			select {
			case entry.ch <- formatted:
			default:
				// Drop if full.
			}
		}
	}
}

// NotifyLogEvents sends matching logs to all logs subscribers.
func (r *SubRegistry) NotifyLogEvents(logs []*types.Log) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, entry := range r.subs {
		if entry.Kind != SubKindLogs {
			continue
		}
		for _, log := range logs {
			if MatchFilter(log, entry.Query) {
				formatted := FormatLog(log)
				select {
				case entry.ch <- formatted:
				default:
				}
			}
		}
	}
}

// NotifyPendingTxHash sends a pending transaction hash to all pendingTx subs.
func (r *SubRegistry) NotifyPendingTxHash(txHash types.Hash) {
	r.mu.Lock()
	defer r.mu.Unlock()

	hashStr := encodeHash(txHash)
	for _, entry := range r.subs {
		if entry.Kind == SubKindPendingTx {
			select {
			case entry.ch <- hashStr:
			default:
			}
		}
	}
}

// NotifySyncStatus sends sync status to all syncing subscribers.
func (r *SubRegistry) NotifySyncStatus(syncing bool, currentBlock, highestBlock uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var result interface{}
	if !syncing {
		result = false
	} else {
		result = map[string]string{
			"currentBlock": encodeUint64(currentBlock),
			"highestBlock": encodeUint64(highestBlock),
		}
	}

	for _, entry := range r.subs {
		if entry.Kind == SubKindSyncStatus {
			select {
			case entry.ch <- result:
			default:
			}
		}
	}
}

// Close shuts down all subscriptions and prevents new ones.
func (r *SubRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true
	for id, entry := range r.subs {
		close(entry.ch)
		delete(r.subs, id)
	}
	r.connTrackers = make(map[string]*connTracker)
}

// IsClosed returns whether the registry is closed.
func (r *SubRegistry) IsClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// CheckRateLimit checks whether a connection has exceeded its event rate.
// Returns true if the event should be allowed, false if rate limited.
func (r *SubRegistry) CheckRateLimit(connID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	tracker := r.connTrackers[connID]
	if tracker == nil {
		return true
	}

	now := time.Now()
	if now.Sub(tracker.lastEventAt) >= r.rateConfig.WindowDuration {
		tracker.eventCount = 0
		tracker.lastEventAt = now
	}
	tracker.eventCount++
	return tracker.eventCount <= r.rateConfig.MaxEventsPerSec
}
