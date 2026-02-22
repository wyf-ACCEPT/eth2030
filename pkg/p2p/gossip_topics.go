// Package p2p implements gossip topic management per the Ethereum consensus
// layer P2P specification. TopicManager tracks subscribed gossip topics,
// handles message validation, deduplication, and per-topic scoring.
package p2p

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"
)

// GossipTopic enumerates the consensus-layer gossip sub topics.
type GossipTopic int

const (
	// BeaconBlock is the global topic for propagating signed beacon blocks.
	BeaconBlock GossipTopic = iota
	// BeaconAggregateAndProof is the global topic for aggregated attestations.
	BeaconAggregateAndProof
	// VoluntaryExit propagates signed voluntary validator exits.
	VoluntaryExit
	// ProposerSlashing propagates proposer slashings.
	ProposerSlashing
	// AttesterSlashing propagates attester slashings.
	AttesterSlashing
	// BlobSidecar propagates blob sidecars (EIP-4844+).
	BlobSidecar
	// SyncCommitteeContribution propagates sync committee contributions.
	SyncCommitteeContribution
)

// gossipTopicNames maps each GossipTopic to its canonical string name
// per the consensus P2P spec topic naming convention.
var gossipTopicNames = map[GossipTopic]string{
	BeaconBlock:               "beacon_block",
	BeaconAggregateAndProof:   "beacon_aggregate_and_proof",
	VoluntaryExit:             "voluntary_exit",
	ProposerSlashing:          "proposer_slashing",
	AttesterSlashing:          "attester_slashing",
	BlobSidecar:               "blob_sidecar",
	SyncCommitteeContribution: "sync_committee_contribution",
}

// String returns the spec-defined name of the gossip topic.
func (t GossipTopic) String() string {
	if name, ok := gossipTopicNames[t]; ok {
		return name
	}
	return fmt.Sprintf("unknown_topic(%d)", int(t))
}

// TopicString builds the full topic string in the form:
// /eth2/<fork_digest>/<name>/ssz_snappy
func (t GossipTopic) TopicString(forkDigest string) string {
	return fmt.Sprintf("/eth2/%s/%s/ssz_snappy", forkDigest, t.String())
}

// ParseGossipTopic converts a topic name string to a GossipTopic.
// Returns an error if the name is not recognized.
func ParseGossipTopic(name string) (GossipTopic, error) {
	for topic, n := range gossipTopicNames {
		if n == name {
			return topic, nil
		}
	}
	return 0, fmt.Errorf("gossip: unknown topic name %q", name)
}

// Message domains for gossip message-id computation per the spec.
var (
	MessageDomainValidSnappy   = [4]byte{0x01, 0x00, 0x00, 0x00}
	MessageDomainInvalidSnappy = [4]byte{0x00, 0x00, 0x00, 0x00}
)

// MessageIDSize is the size of a gossip message ID (20 bytes per spec).
const MessageIDSize = 20

// MessageID is a 20-byte gossip message identifier.
type MessageID [MessageIDSize]byte

// ComputeMessageID computes the gossip message ID per the consensus spec:
// SHA256(MESSAGE_DOMAIN_VALID_SNAPPY + snappy_decompressed_data)[:20]
// For simplicity, the caller passes the decompressed data directly.
// If decompression failed, use ComputeInvalidMessageID instead.
func ComputeMessageID(decompressedData []byte) MessageID {
	h := sha256.New()
	h.Write(MessageDomainValidSnappy[:])
	h.Write(decompressedData)
	sum := h.Sum(nil)
	var id MessageID
	copy(id[:], sum[:MessageIDSize])
	return id
}

// ComputeInvalidMessageID computes the message ID for data that failed
// snappy decompression: SHA256(MESSAGE_DOMAIN_INVALID_SNAPPY + raw_data)[:20]
func ComputeInvalidMessageID(rawData []byte) MessageID {
	h := sha256.New()
	h.Write(MessageDomainInvalidSnappy[:])
	h.Write(rawData)
	sum := h.Sum(nil)
	var id MessageID
	copy(id[:], sum[:MessageIDSize])
	return id
}

// TopicParams holds the gossipsub mesh parameters per the spec.
type TopicParams struct {
	// MeshD is the target number of peers in the mesh (D=8 per spec).
	MeshD int
	// MeshDlo is the low watermark for mesh peers (D_low=6).
	MeshDlo int
	// MeshDhi is the high watermark for mesh peers (D_high=12).
	MeshDhi int
	// HeartbeatInterval is the gossipsub heartbeat frequency.
	HeartbeatInterval time.Duration
	// HistoryLength is the number of heartbeat windows to retain message IDs.
	HistoryLength int
	// HistoryGossip is the number of windows to gossip about (mcache_gossip=3).
	HistoryGossip int
	// FanoutTTL is the TTL for fanout maps (60s per spec).
	FanoutTTL time.Duration
	// SeenTTL is the expiry time for the seen message cache.
	SeenTTL time.Duration
}

// DefaultTopicParams returns the gossipsub parameters from the consensus spec.
func DefaultTopicParams() TopicParams {
	return TopicParams{
		MeshD:             8,
		MeshDlo:           6,
		MeshDhi:           12,
		HeartbeatInterval: 700 * time.Millisecond,
		HistoryLength:     6,
		HistoryGossip:     3,
		FanoutTTL:         60 * time.Second,
		SeenTTL:           384 * time.Second, // SECONDS_PER_SLOT(12) * SLOTS_PER_EPOCH(32) = 384
	}
}

// TopicHandler is a callback invoked when a validated message is received
// on a subscribed topic.
type TopicHandler func(topic GossipTopic, msgID MessageID, data []byte)

// TopicScoreSnapshot holds per-topic scoring metrics.
type TopicScoreSnapshot struct {
	// MessagesReceived is the total number of messages received on this topic.
	MessagesReceived uint64
	// InvalidMessages is the count of messages that failed validation.
	InvalidMessages uint64
	// MeshDeliveries is the count of messages delivered via the mesh.
	MeshDeliveries uint64
	// FirstDeliveries is the count of messages received for the first time.
	FirstDeliveries uint64
}

// Errors for the TopicManager.
var (
	ErrTopicNotSubscribed     = errors.New("gossip_topics: topic not subscribed")
	ErrTopicAlreadySubscribed = errors.New("gossip_topics: topic already subscribed")
	ErrTopicManagerClosed     = errors.New("gossip_topics: manager is closed")
	ErrTopicNilHandler        = errors.New("gossip_topics: nil handler")
	ErrTopicEmptyData         = errors.New("gossip_topics: empty data")
	ErrTopicDuplicateMessage  = errors.New("gossip_topics: duplicate message")
	ErrTopicDataTooLarge      = errors.New("gossip_topics: data exceeds max payload size")
)

// MaxPayloadSize is the maximum uncompressed payload size (10 MiB per spec).
const MaxPayloadSize = 10 * 1024 * 1024

// topicState tracks per-topic subscription state and scoring.
type topicState struct {
	handler TopicHandler
	score   TopicScoreSnapshot
	peers   map[string]float64 // peer ID -> per-topic score
}

// TopicManager manages gossip sub topics per the consensus P2P spec.
// It tracks subscribed topics, message handlers, message deduplication,
// and per-topic scoring. All methods are safe for concurrent use.
type TopicManager struct {
	mu     sync.RWMutex
	params TopicParams
	closed bool

	// Subscribed topics with their handlers and state.
	topics map[GossipTopic]*topicState

	// Global message deduplication: messageID -> receive time.
	seen   map[MessageID]time.Time
	seenMu sync.Mutex
}

// NewTopicManager creates a new TopicManager with the given parameters.
func NewTopicManager(params TopicParams) *TopicManager {
	return &TopicManager{
		params: params,
		topics: make(map[GossipTopic]*topicState),
		seen:   make(map[MessageID]time.Time),
	}
}

// Subscribe registers a handler for the given gossip topic.
// Returns an error if the topic is already subscribed or the handler is nil.
func (tm *TopicManager) Subscribe(topic GossipTopic, handler TopicHandler) error {
	if handler == nil {
		return ErrTopicNilHandler
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.closed {
		return ErrTopicManagerClosed
	}

	if _, exists := tm.topics[topic]; exists {
		return ErrTopicAlreadySubscribed
	}

	tm.topics[topic] = &topicState{
		handler: handler,
		peers:   make(map[string]float64),
	}
	return nil
}

// Unsubscribe removes the handler for the given gossip topic.
func (tm *TopicManager) Unsubscribe(topic GossipTopic) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.closed {
		return ErrTopicManagerClosed
	}

	if _, exists := tm.topics[topic]; !exists {
		return ErrTopicNotSubscribed
	}

	delete(tm.topics, topic)
	return nil
}

// IsSubscribed returns whether the given topic is currently subscribed.
func (tm *TopicManager) IsSubscribed(topic GossipTopic) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	_, exists := tm.topics[topic]
	return exists
}

// SubscribedTopics returns a list of all currently subscribed topics.
func (tm *TopicManager) SubscribedTopics() []GossipTopic {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	topics := make([]GossipTopic, 0, len(tm.topics))
	for t := range tm.topics {
		topics = append(topics, t)
	}
	return topics
}

// Publish validates and dispatches data on the given topic.
// It computes the message ID, checks for duplicates, and invokes
// the registered handler. Returns an error if the topic is not
// subscribed, the data is empty/too large, or it is a duplicate.
func (tm *TopicManager) Publish(topic GossipTopic, data []byte) error {
	if len(data) == 0 {
		return ErrTopicEmptyData
	}
	if len(data) > MaxPayloadSize {
		return ErrTopicDataTooLarge
	}

	msgID := ComputeMessageID(data)

	// Check and mark as seen (deduplication).
	tm.seenMu.Lock()
	if _, dup := tm.seen[msgID]; dup {
		tm.seenMu.Unlock()
		return ErrTopicDuplicateMessage
	}
	tm.seen[msgID] = time.Now()
	tm.seenMu.Unlock()

	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.closed {
		return ErrTopicManagerClosed
	}

	state, exists := tm.topics[topic]
	if !exists {
		return ErrTopicNotSubscribed
	}

	state.score.MessagesReceived++
	state.score.FirstDeliveries++
	state.score.MeshDeliveries++

	state.handler(topic, msgID, data)
	return nil
}

// Deliver processes an incoming message from another peer. It validates
// the message, checks deduplication, and dispatches to the handler.
// isValid indicates whether the data successfully decompressed from snappy.
func (tm *TopicManager) Deliver(topic GossipTopic, data []byte, isValid bool) error {
	if len(data) == 0 {
		return ErrTopicEmptyData
	}
	if len(data) > MaxPayloadSize {
		return ErrTopicDataTooLarge
	}

	var msgID MessageID
	if isValid {
		msgID = ComputeMessageID(data)
	} else {
		msgID = ComputeInvalidMessageID(data)
	}

	// Deduplication.
	tm.seenMu.Lock()
	if _, dup := tm.seen[msgID]; dup {
		tm.seenMu.Unlock()
		return ErrTopicDuplicateMessage
	}
	tm.seen[msgID] = time.Now()
	tm.seenMu.Unlock()

	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.closed {
		return ErrTopicManagerClosed
	}

	state, exists := tm.topics[topic]
	if !exists {
		return ErrTopicNotSubscribed
	}

	state.score.MessagesReceived++

	if !isValid {
		state.score.InvalidMessages++
		return nil
	}

	state.score.FirstDeliveries++
	state.score.MeshDeliveries++
	state.handler(topic, msgID, data)
	return nil
}

// RecordInvalidMessage increments the invalid message counter for a topic.
func (tm *TopicManager) RecordInvalidMessage(topic GossipTopic) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if state, exists := tm.topics[topic]; exists {
		state.score.InvalidMessages++
	}
}

// TopicScore returns the scoring snapshot for a subscribed topic.
func (tm *TopicManager) TopicScore(topic GossipTopic) (TopicScoreSnapshot, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	state, exists := tm.topics[topic]
	if !exists {
		return TopicScoreSnapshot{}, false
	}
	return state.score, true
}

// UpdatePeerTopicScore adjusts the per-topic score for a peer.
func (tm *TopicManager) UpdatePeerTopicScore(topic GossipTopic, peerID string, delta float64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	state, exists := tm.topics[topic]
	if !exists {
		return
	}
	state.peers[peerID] += delta
}

// PeerTopicScore returns the per-topic score for a peer on a given topic.
func (tm *TopicManager) PeerTopicScore(topic GossipTopic, peerID string) float64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	state, exists := tm.topics[topic]
	if !exists {
		return 0
	}
	return state.peers[peerID]
}

// PruneSeenMessages removes seen message entries older than the SeenTTL.
// This should be called periodically to prevent unbounded memory growth.
func (tm *TopicManager) PruneSeenMessages() int {
	cutoff := time.Now().Add(-tm.params.SeenTTL)
	pruned := 0

	tm.seenMu.Lock()
	defer tm.seenMu.Unlock()

	for id, t := range tm.seen {
		if t.Before(cutoff) {
			delete(tm.seen, id)
			pruned++
		}
	}
	return pruned
}

// SeenCount returns the number of message IDs in the seen cache.
func (tm *TopicManager) SeenCount() int {
	tm.seenMu.Lock()
	defer tm.seenMu.Unlock()
	return len(tm.seen)
}

// Params returns the current topic parameters.
func (tm *TopicManager) Params() TopicParams {
	return tm.params
}

// Close shuts down the topic manager. After closing, all methods
// that modify state return ErrTopicManagerClosed.
func (tm *TopicManager) Close() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.closed = true
	tm.topics = make(map[GossipTopic]*topicState)
}
