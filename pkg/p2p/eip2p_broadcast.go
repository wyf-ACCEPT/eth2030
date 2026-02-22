// Package p2p implements the Ethereum P2P networking layer.
//
// eip2p_broadcast.go provides an enhanced P2P broadcast protocol for efficient
// block and blob dissemination. Part of the Hogota era roadmap targeting
// improved blob throughput and local blob reconstruction.
//
// EIP2PBroadcaster manages fanout-based message broadcasting with topic
// subscriptions, configurable redundancy, and broadcast statistics tracking.
package p2p

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/crypto"
)

// EIP2P broadcast constants.
const (
	// DefaultFanout is the default number of peers to broadcast to.
	DefaultFanout = 8

	// MinFanout is the minimum allowed fanout value.
	MinFanout = 1

	// MaxFanout is the maximum allowed fanout value.
	MaxFanout = 64

	// DefaultMaxMessageSize is the default maximum broadcast message size (2 MiB).
	DefaultMaxMessageSize = 2 << 20

	// DefaultSubscriptionBuffer is the channel buffer size for subscriptions.
	DefaultSubscriptionBuffer = 128
)

// EIP2P broadcast errors.
var (
	ErrBroadcastClosed      = errors.New("eip2p: broadcaster is closed")
	ErrBroadcastNilData     = errors.New("eip2p: nil broadcast data")
	ErrBroadcastEmptyType   = errors.New("eip2p: empty message type")
	ErrBroadcastNoPeers     = errors.New("eip2p: no peers to broadcast to")
	ErrBroadcastTooLarge    = errors.New("eip2p: message exceeds max size")
	ErrBroadcastTopicEmpty  = errors.New("eip2p: empty topic name")
	ErrBroadcastNotSub      = errors.New("eip2p: not subscribed to topic")
	ErrBroadcastFanoutRange = errors.New("eip2p: fanout out of range")
)

// BroadcastResult records the outcome of broadcasting to a single peer.
type BroadcastResult struct {
	// PeerID identifies the peer the message was sent to.
	PeerID string

	// Success indicates whether the broadcast to this peer succeeded.
	Success bool

	// Latency is the time taken to deliver the message to this peer.
	Latency time.Duration

	// Error is set when Success is false.
	Error error
}

// BroadcastStats tracks aggregate broadcast statistics.
type BroadcastStats struct {
	// MessagesSent is the total number of messages sent across all peers.
	MessagesSent uint64

	// BytesSent is the total bytes sent across all broadcasts.
	BytesSent uint64

	// Failures is the total number of failed broadcast attempts.
	Failures uint64

	// AvgLatency is the average broadcast latency in nanoseconds.
	AvgLatency uint64
}

// TopicFilter provides selective relay filtering for broadcast messages.
// If Allow returns false, the message is not relayed to subscribers.
type TopicFilter func(msgType string, data []byte) bool

// Subscription represents an active topic subscription for receiving
// broadcast messages.
type Subscription struct {
	Topic    string
	Messages chan *BroadcastMessage
	active   int32 // atomic: 1 = active, 0 = inactive
}

// IsActive returns whether this subscription is still receiving messages.
func (s *Subscription) IsActive() bool {
	return atomic.LoadInt32(&s.active) == 1
}

// BroadcastMessage is a message received through a topic subscription.
type BroadcastMessage struct {
	Type      string
	Data      []byte
	Timestamp time.Time
}

// PeerSender is the interface for sending data to a peer. The broadcaster
// uses this to decouple from the actual network transport.
type PeerSender interface {
	// SendToPeer sends data to the specified peer and returns the latency.
	SendToPeer(peerID string, msgType string, data []byte) (time.Duration, error)
}

// simpleLocalSender is a PeerSender that simulates local delivery.
type simpleLocalSender struct{}

func (s *simpleLocalSender) SendToPeer(peerID string, msgType string, data []byte) (time.Duration, error) {
	start := time.Now()
	// Simulate a small processing delay.
	latency := time.Since(start)
	return latency, nil
}

// EIP2PBroadcaster manages enhanced P2P message broadcasting with
// configurable fanout, topic subscriptions, and statistics tracking.
type EIP2PBroadcaster struct {
	mu     sync.RWMutex
	closed bool

	// Configuration.
	fanout         int
	maxMessageSize int
	sender         PeerSender

	// Topic subscriptions.
	subscriptions map[string][]*Subscription

	// Topic filters for selective relay.
	filters map[string]TopicFilter

	// Statistics tracking (atomic).
	messagesSent   uint64
	bytesSent      uint64
	failures       uint64
	totalLatencyNs uint64
	latencyCount   uint64
}

// NewEIP2PBroadcaster creates a new broadcaster with default configuration.
func NewEIP2PBroadcaster() *EIP2PBroadcaster {
	return &EIP2PBroadcaster{
		fanout:         DefaultFanout,
		maxMessageSize: DefaultMaxMessageSize,
		sender:         &simpleLocalSender{},
		subscriptions:  make(map[string][]*Subscription),
		filters:        make(map[string]TopicFilter),
	}
}

// NewEIP2PBroadcasterWithSender creates a broadcaster with a custom sender.
func NewEIP2PBroadcasterWithSender(sender PeerSender) *EIP2PBroadcaster {
	b := NewEIP2PBroadcaster()
	if sender != nil {
		b.sender = sender
	}
	return b
}

// Broadcast sends a message to the specified peers. The message is sent to
// up to fanout peers from the provided list. Returns a BroadcastResult for
// each peer the message was sent to.
func (b *EIP2PBroadcaster) Broadcast(msgType string, data []byte, peers []string) []BroadcastResult {
	if err := b.validateBroadcast(msgType, data, peers); err != nil {
		return []BroadcastResult{{
			PeerID:  "",
			Success: false,
			Error:   err,
		}}
	}

	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return []BroadcastResult{{Success: false, Error: ErrBroadcastClosed}}
	}
	fanout := b.fanout
	b.mu.RUnlock()

	// Select peers up to fanout limit.
	selected := selectPeers(peers, fanout)

	// Broadcast to selected peers concurrently.
	results := make([]BroadcastResult, len(selected))
	var wg sync.WaitGroup

	for i, peerID := range selected {
		wg.Add(1)
		go func(idx int, pid string) {
			defer wg.Done()
			results[idx] = b.sendToPeer(pid, msgType, data)
		}(i, peerID)
	}

	wg.Wait()

	// Update statistics.
	b.updateStats(results, len(data))

	// Deliver to topic subscribers.
	b.deliverToSubscribers(msgType, data)

	return results
}

// SubscribeTopic creates a subscription to receive broadcast messages for
// the given topic. Messages are delivered to the Subscription's Messages
// channel.
func (b *EIP2PBroadcaster) SubscribeTopic(topic string) *Subscription {
	if topic == "" {
		return nil
	}

	sub := &Subscription{
		Topic:    topic,
		Messages: make(chan *BroadcastMessage, DefaultSubscriptionBuffer),
		active:   1,
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.subscriptions[topic] = append(b.subscriptions[topic], sub)
	return sub
}

// UnsubscribeTopic deactivates a subscription and removes it from the topic.
func (b *EIP2PBroadcaster) UnsubscribeTopic(topic string) {
	if topic == "" {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	subs, ok := b.subscriptions[topic]
	if !ok {
		return
	}

	for _, sub := range subs {
		if atomic.CompareAndSwapInt32(&sub.active, 1, 0) {
			close(sub.Messages)
		}
	}
	delete(b.subscriptions, topic)
}

// SetFanout updates the broadcast fanout (number of peers per broadcast).
// Fanout must be between MinFanout and MaxFanout.
func (b *EIP2PBroadcaster) SetFanout(fanout int) error {
	if fanout < MinFanout || fanout > MaxFanout {
		return ErrBroadcastFanoutRange
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.fanout = fanout
	return nil
}

// GetFanout returns the current fanout setting.
func (b *EIP2PBroadcaster) GetFanout() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.fanout
}

// SetTopicFilter registers a filter function for a topic. Messages are only
// delivered to subscribers if the filter returns true.
func (b *EIP2PBroadcaster) SetTopicFilter(topic string, filter TopicFilter) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if filter == nil {
		delete(b.filters, topic)
	} else {
		b.filters[topic] = filter
	}
}

// Stats returns a snapshot of the current broadcast statistics.
func (b *EIP2PBroadcaster) Stats() BroadcastStats {
	sent := atomic.LoadUint64(&b.messagesSent)
	bytesSent := atomic.LoadUint64(&b.bytesSent)
	failures := atomic.LoadUint64(&b.failures)
	totalNs := atomic.LoadUint64(&b.totalLatencyNs)
	count := atomic.LoadUint64(&b.latencyCount)

	avgLatency := uint64(0)
	if count > 0 {
		avgLatency = totalNs / count
	}

	return BroadcastStats{
		MessagesSent: sent,
		BytesSent:    bytesSent,
		Failures:     failures,
		AvgLatency:   avgLatency,
	}
}

// ResetStats resets all broadcast statistics to zero.
func (b *EIP2PBroadcaster) ResetStats() {
	atomic.StoreUint64(&b.messagesSent, 0)
	atomic.StoreUint64(&b.bytesSent, 0)
	atomic.StoreUint64(&b.failures, 0)
	atomic.StoreUint64(&b.totalLatencyNs, 0)
	atomic.StoreUint64(&b.latencyCount, 0)
}

// ActiveTopics returns the list of topics with active subscriptions.
func (b *EIP2PBroadcaster) ActiveTopics() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	topics := make([]string, 0, len(b.subscriptions))
	for t := range b.subscriptions {
		topics = append(topics, t)
	}
	return topics
}

// Close shuts down the broadcaster, closing all subscription channels.
func (b *EIP2PBroadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for _, subs := range b.subscriptions {
		for _, sub := range subs {
			if atomic.CompareAndSwapInt32(&sub.active, 1, 0) {
				close(sub.Messages)
			}
		}
	}
	b.subscriptions = make(map[string][]*Subscription)
}

// --- Internal helpers ---

func (b *EIP2PBroadcaster) validateBroadcast(msgType string, data []byte, peers []string) error {
	if msgType == "" {
		return ErrBroadcastEmptyType
	}
	if len(data) == 0 {
		return ErrBroadcastNilData
	}
	if len(peers) == 0 {
		return ErrBroadcastNoPeers
	}

	b.mu.RLock()
	maxSize := b.maxMessageSize
	b.mu.RUnlock()

	if len(data) > maxSize {
		return ErrBroadcastTooLarge
	}
	return nil
}

func (b *EIP2PBroadcaster) sendToPeer(peerID, msgType string, data []byte) BroadcastResult {
	latency, err := b.sender.SendToPeer(peerID, msgType, data)
	return BroadcastResult{
		PeerID:  peerID,
		Success: err == nil,
		Latency: latency,
		Error:   err,
	}
}

func (b *EIP2PBroadcaster) updateStats(results []BroadcastResult, dataSize int) {
	for _, r := range results {
		if r.Success {
			atomic.AddUint64(&b.messagesSent, 1)
			atomic.AddUint64(&b.bytesSent, uint64(dataSize))
			atomic.AddUint64(&b.totalLatencyNs, uint64(r.Latency.Nanoseconds()))
			atomic.AddUint64(&b.latencyCount, 1)
		} else {
			atomic.AddUint64(&b.failures, 1)
		}
	}
}

func (b *EIP2PBroadcaster) deliverToSubscribers(msgType string, data []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	subs, ok := b.subscriptions[msgType]
	if !ok {
		return
	}

	// Check topic filter if one is registered.
	if filter, hasFilter := b.filters[msgType]; hasFilter {
		if !filter(msgType, data) {
			return
		}
	}

	msg := &BroadcastMessage{
		Type:      msgType,
		Data:      data,
		Timestamp: time.Now(),
	}

	for _, sub := range subs {
		if atomic.LoadInt32(&sub.active) == 1 {
			select {
			case sub.Messages <- msg:
			default:
				// Channel full, drop message to avoid blocking.
			}
		}
	}
}

// selectPeers selects up to limit peers from the list using a deterministic
// hash-based selection. If len(peers) <= limit, all peers are returned.
func selectPeers(peers []string, limit int) []string {
	if len(peers) <= limit {
		result := make([]string, len(peers))
		copy(result, peers)
		return result
	}

	// Use hash-based selection for deterministic peer choice.
	type scoredPeer struct {
		id    string
		score [32]byte
	}

	scored := make([]scoredPeer, len(peers))
	for i, p := range peers {
		h := crypto.Keccak256Hash([]byte(p))
		scored[i] = scoredPeer{id: p, score: h}
	}

	// Sort by hash score and take top 'limit'.
	for i := 0; i < limit; i++ {
		minIdx := i
		for j := i + 1; j < len(scored); j++ {
			if comparHashes(scored[j].score, scored[minIdx].score) {
				minIdx = j
			}
		}
		scored[i], scored[minIdx] = scored[minIdx], scored[i]
	}

	result := make([]string, limit)
	for i := 0; i < limit; i++ {
		result[i] = scored[i].id
	}
	return result
}

// comparHashes returns true if a < b lexicographically.
func comparHashes(a, b [32]byte) bool {
	for i := range a {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}
