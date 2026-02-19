// Package p2p implements an enhanced gossip protocol for block distribution.
//
// GossipManager provides topic-based publish/subscribe messaging with peer
// scoring, banning, and message validation. It supports the Ethereum 2028
// roadmap's distributed block building and fast confirmation requirements.
package p2p

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Gossip protocol errors.
var (
	ErrGossipClosed         = errors.New("gossip: manager is closed")
	ErrGossipNilMsg         = errors.New("gossip: nil message")
	ErrGossipEmptyTopic     = errors.New("gossip: empty topic")
	ErrGossipEmptyData      = errors.New("gossip: empty data")
	ErrGossipMsgTooLarge    = errors.New("gossip: message exceeds max size")
	ErrGossipZeroSender     = errors.New("gossip: sender ID must not be zero")
	ErrGossipZeroTimestamp  = errors.New("gossip: timestamp must not be zero")
	ErrGossipZeroMessageID  = errors.New("gossip: message ID must not be zero")
	ErrGossipSubNotFound    = errors.New("gossip: subscription not found")
	ErrGossipSubInactive    = errors.New("gossip: subscription already inactive")
	ErrGossipPeerBanned     = errors.New("gossip: peer is banned")
	ErrGossipPeerNotFound   = errors.New("gossip: peer not found")
	ErrGossipTopicMismatch  = errors.New("gossip: message topic does not match")
)

// GossipConfig configures the gossip protocol manager.
type GossipConfig struct {
	MaxMessageSize     uint64  // maximum message size in bytes
	FanoutSize         int     // number of peers to relay each message to
	HeartbeatInterval  uint64  // heartbeat interval in milliseconds
	PeerScoreThreshold float64 // minimum score to participate in gossip
}

// DefaultGossipConfig returns sensible default gossip configuration.
func DefaultGossipConfig() GossipConfig {
	return GossipConfig{
		MaxMessageSize:     1 << 20, // 1 MiB
		FanoutSize:         6,
		HeartbeatInterval:  1000, // 1 second
		PeerScoreThreshold: -50.0,
	}
}

// GossipMessage represents a message propagated through the gossip network.
type GossipMessage struct {
	Topic     string     // topic the message belongs to
	Data      []byte     // message payload
	SenderID  types.Hash // sender peer identifier
	Timestamp uint64     // unix timestamp in seconds
	MessageID types.Hash // unique message identifier
}

// GossipSubscription represents an active subscription to a gossip topic.
type GossipSubscription struct {
	Topic    string
	Messages chan *GossipMessage
	active   bool
	mu       sync.Mutex
}

// IsActive returns whether the subscription is still active.
func (gs *GossipSubscription) IsActive() bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	return gs.active
}

// bannedPeer tracks a banned peer and the ban expiry.
type bannedPeer struct {
	PeerID types.Hash
	Reason string
	Expiry uint64 // unix timestamp when the ban expires
}

// GossipManager manages gossip-based message propagation across topics.
// All methods are safe for concurrent use.
type GossipManager struct {
	mu     sync.RWMutex
	config GossipConfig
	closed bool

	// Topic management.
	subscriptions map[string][]*GossipSubscription // topic -> subscriptions
	topicPeers    map[string]map[types.Hash]bool   // topic -> set of peer IDs

	// Peer scoring and banning.
	peerScores map[types.Hash]float64 // peer ID -> score
	banned     map[types.Hash]*bannedPeer

	// Message deduplication.
	seen map[types.Hash]bool
}

// NewGossipManager creates a new gossip manager with the given configuration.
func NewGossipManager(config GossipConfig) *GossipManager {
	return &GossipManager{
		config:        config,
		subscriptions: make(map[string][]*GossipSubscription),
		topicPeers:    make(map[string]map[types.Hash]bool),
		peerScores:    make(map[types.Hash]float64),
		banned:        make(map[types.Hash]*bannedPeer),
		seen:          make(map[types.Hash]bool),
	}
}

// PublishMessage creates and delivers a gossip message to all subscribers
// of the specified topic.
func (gm *GossipManager) PublishMessage(topic string, data []byte) error {
	if topic == "" {
		return ErrGossipEmptyTopic
	}
	if len(data) == 0 {
		return ErrGossipEmptyData
	}
	if gm.config.MaxMessageSize > 0 && uint64(len(data)) > gm.config.MaxMessageSize {
		return fmt.Errorf("%w: size %d > max %d", ErrGossipMsgTooLarge, len(data), gm.config.MaxMessageSize)
	}

	// Build the message with a deterministic ID.
	now := uint64(time.Now().Unix())
	msgID := computeMessageID(topic, data, now)

	msg := &GossipMessage{
		Topic:     topic,
		Data:      data,
		Timestamp: now,
		MessageID: msgID,
	}

	gm.mu.Lock()
	defer gm.mu.Unlock()

	if gm.closed {
		return ErrGossipClosed
	}

	// Mark as seen for deduplication.
	gm.seen[msgID] = true

	// Deliver to all active subscribers for this topic.
	subs := gm.subscriptions[topic]
	for _, sub := range subs {
		sub.mu.Lock()
		if sub.active {
			select {
			case sub.Messages <- msg:
			default:
				// Channel full; drop message to avoid blocking.
			}
		}
		sub.mu.Unlock()
	}

	return nil
}

// Subscribe creates a new subscription to the specified topic.
// Messages are delivered to the returned GossipSubscription's Messages channel.
func (gm *GossipManager) Subscribe(topic string) *GossipSubscription {
	sub := &GossipSubscription{
		Topic:    topic,
		Messages: make(chan *GossipMessage, 64),
		active:   true,
	}

	gm.mu.Lock()
	defer gm.mu.Unlock()

	gm.subscriptions[topic] = append(gm.subscriptions[topic], sub)
	return sub
}

// Unsubscribe deactivates a subscription and removes it from the topic.
func (gm *GossipManager) Unsubscribe(sub *GossipSubscription) error {
	if sub == nil {
		return ErrGossipSubNotFound
	}

	sub.mu.Lock()
	if !sub.active {
		sub.mu.Unlock()
		return ErrGossipSubInactive
	}
	sub.active = false
	close(sub.Messages)
	sub.mu.Unlock()

	gm.mu.Lock()
	defer gm.mu.Unlock()

	subs := gm.subscriptions[sub.Topic]
	for i, s := range subs {
		if s == sub {
			gm.subscriptions[sub.Topic] = append(subs[:i], subs[i+1:]...)
			break
		}
	}

	// Clean up empty topic.
	if len(gm.subscriptions[sub.Topic]) == 0 {
		delete(gm.subscriptions, sub.Topic)
	}

	return nil
}

// ValidateMessage checks that a gossip message is well-formed.
func (gm *GossipManager) ValidateMessage(msg *GossipMessage) error {
	if msg == nil {
		return ErrGossipNilMsg
	}
	if msg.Topic == "" {
		return ErrGossipEmptyTopic
	}
	if len(msg.Data) == 0 {
		return ErrGossipEmptyData
	}
	if gm.config.MaxMessageSize > 0 && uint64(len(msg.Data)) > gm.config.MaxMessageSize {
		return fmt.Errorf("%w: size %d > max %d", ErrGossipMsgTooLarge, len(msg.Data), gm.config.MaxMessageSize)
	}
	if msg.SenderID.IsZero() {
		return ErrGossipZeroSender
	}
	if msg.Timestamp == 0 {
		return ErrGossipZeroTimestamp
	}
	if msg.MessageID.IsZero() {
		return ErrGossipZeroMessageID
	}

	// Check if sender is banned.
	gm.mu.RLock()
	if bp, ok := gm.banned[msg.SenderID]; ok {
		now := uint64(time.Now().Unix())
		if now < bp.Expiry {
			gm.mu.RUnlock()
			return fmt.Errorf("%w: %s", ErrGossipPeerBanned, bp.Reason)
		}
	}
	gm.mu.RUnlock()

	return nil
}

// PeerScore returns the current reputation score for a peer.
// Returns 0.0 if the peer is unknown.
func (gm *GossipManager) PeerScore(peerID types.Hash) float64 {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return gm.peerScores[peerID]
}

// UpdatePeerScore adjusts a peer's reputation score by delta.
// The score is clamped to [-100, 100].
func (gm *GossipManager) UpdatePeerScore(peerID types.Hash, delta float64) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	score := gm.peerScores[peerID] + delta
	if score > MaxScore {
		score = MaxScore
	}
	if score < MinScore {
		score = MinScore
	}
	gm.peerScores[peerID] = score
}

// BanPeer bans a peer for the specified duration (in seconds).
// Banned peers fail message validation.
func (gm *GossipManager) BanPeer(peerID types.Hash, reason string, duration uint64) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	expiry := uint64(time.Now().Unix()) + duration
	gm.banned[peerID] = &bannedPeer{
		PeerID: peerID,
		Reason: reason,
		Expiry: expiry,
	}

	// Also set score to minimum.
	gm.peerScores[peerID] = MinScore
}

// GetTopicPeers returns the list of peer IDs subscribed to a topic.
func (gm *GossipManager) GetTopicPeers(topic string) []types.Hash {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	peers := gm.topicPeers[topic]
	result := make([]types.Hash, 0, len(peers))
	for id := range peers {
		result = append(result, id)
	}
	return result
}

// AddTopicPeer registers a peer as subscribed to a topic.
func (gm *GossipManager) AddTopicPeer(topic string, peerID types.Hash) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	if gm.topicPeers[topic] == nil {
		gm.topicPeers[topic] = make(map[types.Hash]bool)
	}
	gm.topicPeers[topic][peerID] = true
}

// RemoveTopicPeer unregisters a peer from a topic.
func (gm *GossipManager) RemoveTopicPeer(topic string, peerID types.Hash) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	if peers, ok := gm.topicPeers[topic]; ok {
		delete(peers, peerID)
		if len(peers) == 0 {
			delete(gm.topicPeers, topic)
		}
	}
}

// ActiveTopics returns a sorted list of topics with active subscriptions.
func (gm *GossipManager) ActiveTopics() []string {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	topics := make([]string, 0, len(gm.subscriptions))
	for topic := range gm.subscriptions {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	return topics
}

// IsSeen returns whether a message ID has already been seen.
func (gm *GossipManager) IsSeen(msgID types.Hash) bool {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return gm.seen[msgID]
}

// Close shuts down the gossip manager and closes all subscription channels.
func (gm *GossipManager) Close() {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	if gm.closed {
		return
	}
	gm.closed = true

	for _, subs := range gm.subscriptions {
		for _, sub := range subs {
			sub.mu.Lock()
			if sub.active {
				sub.active = false
				close(sub.Messages)
			}
			sub.mu.Unlock()
		}
	}
}

// computeMessageID derives a deterministic message ID from topic, data, and timestamp.
func computeMessageID(topic string, data []byte, timestamp uint64) types.Hash {
	var buf []byte
	buf = append(buf, []byte(topic)...)
	buf = append(buf, data...)
	buf = append(buf, byte(timestamp>>56), byte(timestamp>>48), byte(timestamp>>40), byte(timestamp>>32))
	buf = append(buf, byte(timestamp>>24), byte(timestamp>>16), byte(timestamp>>8), byte(timestamp))
	return crypto.Keccak256Hash(buf)
}
