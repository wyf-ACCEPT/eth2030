// gossip_mesh_scoring.go implements per-topic peer scoring for the GossipSub
// v1.1 mesh protocol. It tracks message delivery rates, applies exponential
// decay to historical scores, manages threshold-based banning with cooldown,
// and supports score-based mesh pruning and grafting decisions.
package p2p

import (
	"math"
	"sync"
	"time"
)

// MeshScoreParams configures per-topic scoring weights and penalties per
// the GossipSub v1.1 scoring specification.
type MeshScoreParams struct {
	// MeshDeliveryWeight rewards peers that deliver messages while in the mesh.
	MeshDeliveryWeight float64
	// FirstMessageWeight rewards the first peer that delivers a new message.
	FirstMessageWeight float64
	// InvalidMessagePenalty is the penalty for delivering an invalid message.
	InvalidMessagePenalty float64
	// MeshTimeWeight rewards peers based on how long they stay in the mesh.
	MeshTimeWeight float64
	// MeshTimeActivation is the minimum time in mesh before time score accrues.
	MeshTimeActivation time.Duration
}

// DefaultMeshScoreParams returns scoring parameters aligned with the
// GossipSub v1.1 specification defaults.
func DefaultMeshScoreParams() MeshScoreParams {
	return MeshScoreParams{
		MeshDeliveryWeight:    1.0,
		FirstMessageWeight:    5.0,
		InvalidMessagePenalty: -10.0,
		MeshTimeWeight:        0.1,
		MeshTimeActivation:    2 * time.Second,
	}
}

// TopicPeerScore tracks per-topic delivery statistics for a single peer.
type TopicPeerScore struct {
	MeshDeliveries  uint64    // Messages delivered while in mesh.
	FirstDeliveries uint64    // First-seen messages from this peer.
	InvalidMessages uint64    // Invalid messages from this peer.
	MeshJoinTime    time.Time // When this peer joined the topic mesh.
	InMesh          bool      // Whether the peer is currently in mesh.
	DecayedScore    float64   // Accumulated score after decay.
	LastDecayTime   time.Time // Last time decay was applied.
}

// MeshDecayConfig configures exponential decay for historical scores.
type MeshDecayConfig struct {
	// DecayInterval is the period between score decay applications.
	DecayInterval time.Duration
	// DecayFactor is the exponential decay multiplier (0..1) per interval.
	DecayFactor float64
	// CounterDecayFactor is the decay applied to message counters.
	CounterDecayFactor float64
}

// DefaultMeshDecayConfig returns sensible decay defaults.
func DefaultMeshDecayConfig() MeshDecayConfig {
	return MeshDecayConfig{
		DecayInterval:      time.Minute,
		DecayFactor:        0.9,
		CounterDecayFactor: 0.5,
	}
}

// MeshBanConfig configures threshold-based peer banning and cooldown.
type MeshBanConfig struct {
	// BanThreshold is the score below which a peer gets mesh-banned.
	BanThreshold float64
	// BanCooldown is the minimum time a peer must wait before rejoining.
	BanCooldown time.Duration
	// GraylistThreshold is the score below which a peer is graylisted
	// (deprioritized but not fully banned).
	GraylistThreshold float64
}

// DefaultMeshBanConfig returns sensible ban defaults.
func DefaultMeshBanConfig() MeshBanConfig {
	return MeshBanConfig{
		BanThreshold:      -100.0,
		BanCooldown:       5 * time.Minute,
		GraylistThreshold: -20.0,
	}
}

// meshBanEntry records a mesh ban with its expiry.
type meshBanEntry struct {
	BannedAt  time.Time
	ExpiresAt time.Time
	Score     float64
}

// meshPeerState holds all scoring state for a single peer across topics.
type meshPeerState struct {
	topics     map[string]*TopicPeerScore
	banned     *meshBanEntry
	graylisted bool
}

// GossipMeshScoreManager manages per-topic peer scoring for the gossip
// mesh protocol. It implements GossipSub v1.1 scoring with exponential
// decay, threshold-based banning with cooldown, and mesh management.
// All methods are safe for concurrent use.
type GossipMeshScoreManager struct {
	mu        sync.RWMutex
	params    MeshScoreParams
	decay     MeshDecayConfig
	ban       MeshBanConfig
	peers     map[string]*meshPeerState
	meshPeers map[string]map[string]bool // topic -> set of peer IDs in mesh
}

// NewGossipMeshScoreManager creates a mesh score manager with the given configs.
func NewGossipMeshScoreManager(params MeshScoreParams, decay MeshDecayConfig, ban MeshBanConfig) *GossipMeshScoreManager {
	return &GossipMeshScoreManager{
		params:    params,
		decay:     decay,
		ban:       ban,
		peers:     make(map[string]*meshPeerState),
		meshPeers: make(map[string]map[string]bool),
	}
}

// getOrCreatePeer returns (or initializes) a peer's state. Caller must hold mu.
func (m *GossipMeshScoreManager) getOrCreatePeer(peerID string) *meshPeerState {
	ps, ok := m.peers[peerID]
	if !ok {
		ps = &meshPeerState{
			topics: make(map[string]*TopicPeerScore),
		}
		m.peers[peerID] = ps
	}
	return ps
}

// getOrCreateTopicScore returns (or initializes) a topic score. Caller must hold mu.
func (m *GossipMeshScoreManager) getOrCreateTopicScore(ps *meshPeerState, topic string) *TopicPeerScore {
	ts, ok := ps.topics[topic]
	if !ok {
		ts = &TopicPeerScore{
			LastDecayTime: time.Now(),
		}
		ps.topics[topic] = ts
	}
	return ts
}

// RecordMeshDelivery records a valid message delivery in the mesh.
func (m *GossipMeshScoreManager) RecordMeshDelivery(peerID, topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ps := m.getOrCreatePeer(peerID)
	ts := m.getOrCreateTopicScore(ps, topic)
	if ts.InMesh {
		ts.MeshDeliveries++
	}
}

// RecordFirstMessage records that a peer was the first to deliver a message.
func (m *GossipMeshScoreManager) RecordFirstMessage(peerID, topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ps := m.getOrCreatePeer(peerID)
	ts := m.getOrCreateTopicScore(ps, topic)
	ts.FirstDeliveries++
}

// RecordInvalidMessage records that a peer sent an invalid message.
func (m *GossipMeshScoreManager) RecordInvalidMessage(peerID, topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ps := m.getOrCreatePeer(peerID)
	ts := m.getOrCreateTopicScore(ps, topic)
	ts.InvalidMessages++
}

// GraftPeer adds a peer to the mesh for a topic.
func (m *GossipMeshScoreManager) GraftPeer(peerID, topic string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	ps := m.getOrCreatePeer(peerID)

	// Reject grafts from banned peers.
	if ps.banned != nil && time.Now().Before(ps.banned.ExpiresAt) {
		return false
	}
	// Clear expired ban.
	if ps.banned != nil {
		ps.banned = nil
	}

	ts := m.getOrCreateTopicScore(ps, topic)
	ts.InMesh = true
	ts.MeshJoinTime = time.Now()

	if m.meshPeers[topic] == nil {
		m.meshPeers[topic] = make(map[string]bool)
	}
	m.meshPeers[topic][peerID] = true
	return true
}

// PrunePeer removes a peer from the mesh for a topic.
func (m *GossipMeshScoreManager) PrunePeer(peerID, topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ps, ok := m.peers[peerID]
	if !ok {
		return
	}
	ts, ok := ps.topics[topic]
	if ok {
		ts.InMesh = false
		ts.MeshJoinTime = time.Time{}
	}
	if mp, ok := m.meshPeers[topic]; ok {
		delete(mp, peerID)
	}
}

// TopicScore computes the aggregate score for a peer on a specific topic.
func (m *GossipMeshScoreManager) TopicScore(peerID, topic string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.topicScoreLocked(peerID, topic)
}

func (m *GossipMeshScoreManager) topicScoreLocked(peerID, topic string) float64 {
	ps, ok := m.peers[peerID]
	if !ok {
		return 0
	}
	ts, ok := ps.topics[topic]
	if !ok {
		return 0
	}

	score := ts.DecayedScore
	score += float64(ts.MeshDeliveries) * m.params.MeshDeliveryWeight
	score += float64(ts.FirstDeliveries) * m.params.FirstMessageWeight
	score += float64(ts.InvalidMessages) * m.params.InvalidMessagePenalty

	// Add mesh time contribution.
	if ts.InMesh && !ts.MeshJoinTime.IsZero() {
		elapsed := time.Since(ts.MeshJoinTime)
		if elapsed >= m.params.MeshTimeActivation {
			score += elapsed.Seconds() * m.params.MeshTimeWeight
		}
	}
	return score
}

// PeerScore computes the aggregate score across all topics for a peer.
func (m *GossipMeshScoreManager) PeerScore(peerID string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ps, ok := m.peers[peerID]
	if !ok {
		return 0
	}
	var total float64
	for topic := range ps.topics {
		total += m.topicScoreLocked(peerID, topic)
	}
	return total
}

// ApplyDecay applies exponential decay to all peers' historical scores.
// This should be called periodically (e.g., every DecayInterval).
func (m *GossipMeshScoreManager) ApplyDecay() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for _, ps := range m.peers {
		for _, ts := range ps.topics {
			elapsed := now.Sub(ts.LastDecayTime)
			if elapsed < m.decay.DecayInterval {
				continue
			}
			intervals := float64(elapsed) / float64(m.decay.DecayInterval)
			factor := math.Pow(m.decay.DecayFactor, intervals)

			// Decay the accumulated score.
			ts.DecayedScore *= factor

			// Decay the counters.
			counterFactor := math.Pow(m.decay.CounterDecayFactor, intervals)
			ts.MeshDeliveries = uint64(float64(ts.MeshDeliveries) * counterFactor)
			ts.FirstDeliveries = uint64(float64(ts.FirstDeliveries) * counterFactor)
			// Invalid messages decay slower (retain penalty longer).
			ts.InvalidMessages = uint64(float64(ts.InvalidMessages) * math.Sqrt(counterFactor))

			ts.LastDecayTime = now
		}
	}
}

// CheckBans evaluates all peers and bans those below the threshold.
// Returns the list of newly banned peer IDs.
func (m *GossipMeshScoreManager) CheckBans() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var newlyBanned []string
	now := time.Now()

	for peerID, ps := range m.peers {
		// Skip already banned peers.
		if ps.banned != nil && now.Before(ps.banned.ExpiresAt) {
			continue
		}
		// Clear expired bans.
		if ps.banned != nil {
			ps.banned = nil
		}

		var total float64
		for topic := range ps.topics {
			total += m.topicScoreLocked(peerID, topic)
		}

		if total <= m.ban.BanThreshold {
			ps.banned = &meshBanEntry{
				BannedAt:  now,
				ExpiresAt: now.Add(m.ban.BanCooldown),
				Score:     total,
			}
			// Prune from all meshes.
			for topic := range ps.topics {
				if ts, ok := ps.topics[topic]; ok {
					ts.InMesh = false
				}
				if mp, ok := m.meshPeers[topic]; ok {
					delete(mp, peerID)
				}
			}
			newlyBanned = append(newlyBanned, peerID)
		}

		// Graylist check.
		ps.graylisted = total <= m.ban.GraylistThreshold && ps.banned == nil
	}
	return newlyBanned
}

// IsBanned returns whether a peer is currently mesh-banned.
func (m *GossipMeshScoreManager) IsBanned(peerID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ps, ok := m.peers[peerID]
	if !ok {
		return false
	}
	return ps.banned != nil && time.Now().Before(ps.banned.ExpiresAt)
}

// IsGraylisted returns whether a peer is currently graylisted.
func (m *GossipMeshScoreManager) IsGraylisted(peerID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ps, ok := m.peers[peerID]
	if !ok {
		return false
	}
	return ps.graylisted
}

// BanCooldownRemaining returns the remaining ban cooldown for a peer.
// Returns zero if not banned.
func (m *GossipMeshScoreManager) BanCooldownRemaining(peerID string) time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ps, ok := m.peers[peerID]
	if !ok || ps.banned == nil {
		return 0
	}
	remaining := time.Until(ps.banned.ExpiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// MeshPeers returns the set of peer IDs currently in the mesh for a topic.
func (m *GossipMeshScoreManager) MeshPeers(topic string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mp, ok := m.meshPeers[topic]
	if !ok {
		return nil
	}
	peers := make([]string, 0, len(mp))
	for id := range mp {
		peers = append(peers, id)
	}
	return peers
}

// PruneByScore removes the lowest-scoring peers from a topic mesh until the
// mesh size is at or below the target count. Returns pruned peer IDs.
func (m *GossipMeshScoreManager) PruneByScore(topic string, targetSize int) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	mp, ok := m.meshPeers[topic]
	if !ok || len(mp) <= targetSize {
		return nil
	}

	// Collect scored peers.
	type scoredPeer struct {
		id    string
		score float64
	}
	peers := make([]scoredPeer, 0, len(mp))
	for id := range mp {
		peers = append(peers, scoredPeer{id: id, score: m.topicScoreLocked(id, topic)})
	}

	// Sort ascending by score (worst first).
	for i := 0; i < len(peers)-1; i++ {
		for j := i + 1; j < len(peers); j++ {
			if peers[j].score < peers[i].score {
				peers[i], peers[j] = peers[j], peers[i]
			}
		}
	}

	pruneCount := len(mp) - targetSize
	pruned := make([]string, 0, pruneCount)
	for i := 0; i < pruneCount && i < len(peers); i++ {
		id := peers[i].id
		delete(mp, id)
		if ps, ok := m.peers[id]; ok {
			if ts, ok := ps.topics[topic]; ok {
				ts.InMesh = false
			}
		}
		pruned = append(pruned, id)
	}
	return pruned
}

// RemovePeer removes all scoring state for a peer.
func (m *GossipMeshScoreManager) RemovePeer(peerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.peers, peerID)
	for _, mp := range m.meshPeers {
		delete(mp, peerID)
	}
}

// PeerCount returns the number of tracked peers.
func (m *GossipMeshScoreManager) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}
