// gossip_scoring.go implements a composite peer scoring system that
// integrates gossip topic scores with protocol-level reputation. It
// provides weighted score aggregation, response latency tracking, and
// gossip-aware ban management for peer selection decisions.
package p2p

import (
	"math"
	"sort"
	"sync"
	"time"
)

// GossipScoreWeights controls the relative importance of each scoring
// component when computing a peer's composite score.
type GossipScoreWeights struct {
	ProtocolWeight float64 // Weight for protocol-level score (peer_scoring.go).
	GossipWeight   float64 // Weight for gossip topic aggregate score.
	LatencyWeight  float64 // Weight for response latency component.
}

// DefaultGossipScoreWeights returns balanced defaults.
func DefaultGossipScoreWeights() GossipScoreWeights {
	return GossipScoreWeights{
		ProtocolWeight: 0.40,
		GossipWeight:   0.35,
		LatencyWeight:  0.25,
	}
}

// GossipScoreConfig configures the GossipScoreManager.
type GossipScoreConfig struct {
	Weights            GossipScoreWeights
	GossipBanThreshold float64       // Composite score at which a peer gets gossip-banned.
	LatencyTarget      time.Duration // Ideal response latency; below this earns full score.
	LatencyPenalty     time.Duration // Latency above this earns the worst latency score.
	DecayInterval      time.Duration // How often latency samples decay.
	DecayAlpha         float64       // Exponential moving average alpha for latency (0..1).
	MaxTopics          int           // Maximum number of topics tracked per peer.
}

// DefaultGossipScoreConfig returns sensible defaults.
func DefaultGossipScoreConfig() GossipScoreConfig {
	return GossipScoreConfig{
		Weights:            DefaultGossipScoreWeights(),
		GossipBanThreshold: -60.0,
		LatencyTarget:      200 * time.Millisecond,
		LatencyPenalty:     5 * time.Second,
		DecayInterval:      30 * time.Second,
		DecayAlpha:         0.3,
		MaxTopics:          16,
	}
}

// peerLatencyState holds the exponentially weighted average latency for a peer.
type peerLatencyState struct {
	avgMs      float64   // Exponentially weighted average latency in ms.
	samples    int       // Total number of recorded samples.
	lastSample time.Time // When the last sample was recorded.
}

// peerGossipState holds per-peer gossip topic scores.
type peerGossipState struct {
	topicScores map[string]float64 // topic name -> score
	totalMsgs   uint64             // Total messages delivered by this peer.
	invalidMsgs uint64             // Invalid messages from this peer.
}

// GossipScorerSnapshot is a read-only view of a peer's composite score state.
type GossipScorerSnapshot struct {
	PeerID          string
	CompositeScore  float64
	ProtocolScore   float64
	GossipScore     float64
	LatencyScore    float64
	AvgLatencyMs    float64
	LatencySamples  int
	TotalMessages   uint64
	InvalidMessages uint64
	GossipBanned    bool
}

// GossipScoreManager computes composite peer scores by combining protocol
// reputation (from PeerScorer), gossip topic contributions, and measured
// response latency. All methods are safe for concurrent use.
type GossipScoreManager struct {
	mu     sync.RWMutex
	config GossipScoreConfig

	// Protocol scorer provides the base peer reputation.
	scorer *PeerScorer

	// Per-peer gossip and latency state.
	gossipStates  map[string]*peerGossipState
	latencyStates map[string]*peerLatencyState

	// Peers that have been gossip-banned (separate from protocol bans).
	gossipBanned map[string]time.Time
}

// NewGossipScoreManager creates a GossipScoreManager backed by the given PeerScorer.
func NewGossipScoreManager(config GossipScoreConfig, scorer *PeerScorer) *GossipScoreManager {
	return &GossipScoreManager{
		config:        config,
		scorer:        scorer,
		gossipStates:  make(map[string]*peerGossipState),
		latencyStates: make(map[string]*peerLatencyState),
		gossipBanned:  make(map[string]time.Time),
	}
}

// RecordLatency records a response latency sample for a peer. The latency
// is integrated into the peer's exponentially weighted moving average.
func (gsm *GossipScoreManager) RecordLatency(peerID string, latency time.Duration) {
	gsm.mu.Lock()
	defer gsm.mu.Unlock()

	state := gsm.getOrCreateLatency(peerID)
	ms := float64(latency.Milliseconds())
	if ms < 0 {
		ms = 0
	}

	alpha := gsm.config.DecayAlpha
	if state.samples == 0 {
		state.avgMs = ms
	} else {
		state.avgMs = alpha*ms + (1-alpha)*state.avgMs
	}
	state.samples++
	state.lastSample = time.Now()
}

// RecordTopicDelivery records that a peer delivered a valid message on
// the given gossip topic, increasing the peer's gossip score.
func (gsm *GossipScoreManager) RecordTopicDelivery(peerID, topic string) {
	gsm.mu.Lock()
	defer gsm.mu.Unlock()

	gs := gsm.getOrCreateGossip(peerID)
	gs.totalMsgs++

	// Increment the topic score. Limit tracked topics per peer.
	if len(gs.topicScores) < gsm.config.MaxTopics || gs.topicScores[topic] != 0 {
		gs.topicScores[topic] += 1.0
	}
}

// RecordTopicInvalid records that a peer sent an invalid message on a topic,
// penalizing the peer's gossip score.
func (gsm *GossipScoreManager) RecordTopicInvalid(peerID, topic string) {
	gsm.mu.Lock()
	defer gsm.mu.Unlock()

	gs := gsm.getOrCreateGossip(peerID)
	gs.invalidMsgs++
	gs.topicScores[topic] -= 5.0
}

// CompositeScore computes the weighted composite score for a peer. Components
// are: (1) protocol reputation from PeerScorer, (2) aggregate gossip topic
// score, and (3) latency-derived score.
func (gsm *GossipScoreManager) CompositeScore(peerID string) float64 {
	gsm.mu.RLock()
	defer gsm.mu.RUnlock()

	w := gsm.config.Weights
	protocol := gsm.protocolComponent(peerID)
	gossip := gsm.gossipComponent(peerID)
	latency := gsm.latencyComponent(peerID)

	return w.ProtocolWeight*protocol + w.GossipWeight*gossip + w.LatencyWeight*latency
}

// Snapshot returns a full scoring snapshot for a peer.
func (gsm *GossipScoreManager) Snapshot(peerID string) GossipScorerSnapshot {
	gsm.mu.RLock()
	defer gsm.mu.RUnlock()

	protocol := gsm.protocolComponent(peerID)
	gossip := gsm.gossipComponent(peerID)
	latency := gsm.latencyComponent(peerID)

	w := gsm.config.Weights
	composite := w.ProtocolWeight*protocol + w.GossipWeight*gossip + w.LatencyWeight*latency

	snap := GossipScorerSnapshot{
		PeerID:         peerID,
		CompositeScore: composite,
		ProtocolScore:  protocol,
		GossipScore:    gossip,
		LatencyScore:   latency,
		GossipBanned:   gsm.isGossipBannedLocked(peerID),
	}

	if ls, ok := gsm.latencyStates[peerID]; ok {
		snap.AvgLatencyMs = ls.avgMs
		snap.LatencySamples = ls.samples
	}
	if gs, ok := gsm.gossipStates[peerID]; ok {
		snap.TotalMessages = gs.totalMsgs
		snap.InvalidMessages = gs.invalidMsgs
	}

	return snap
}

// TopComposite returns the n peers with the highest composite scores,
// sorted descending. Gossip-banned peers are excluded.
func (gsm *GossipScoreManager) TopComposite(n int) []GossipScorerSnapshot {
	gsm.mu.RLock()
	defer gsm.mu.RUnlock()

	// Collect all tracked peer IDs.
	peerSet := make(map[string]struct{})
	for id := range gsm.gossipStates {
		peerSet[id] = struct{}{}
	}
	for id := range gsm.latencyStates {
		peerSet[id] = struct{}{}
	}

	var results []GossipScorerSnapshot
	w := gsm.config.Weights
	for id := range peerSet {
		if gsm.isGossipBannedLocked(id) {
			continue
		}
		protocol := gsm.protocolComponent(id)
		gossip := gsm.gossipComponent(id)
		latency := gsm.latencyComponent(id)
		composite := w.ProtocolWeight*protocol + w.GossipWeight*gossip + w.LatencyWeight*latency

		results = append(results, GossipScorerSnapshot{
			PeerID:         id,
			CompositeScore: composite,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].CompositeScore > results[j].CompositeScore
	})

	if n > len(results) {
		n = len(results)
	}
	return results[:n]
}

// CheckGossipBans evaluates all tracked peers and gossip-bans those whose
// composite score falls at or below the configured threshold. Returns the
// list of newly banned peer IDs.
func (gsm *GossipScoreManager) CheckGossipBans() []string {
	gsm.mu.Lock()
	defer gsm.mu.Unlock()

	peerSet := make(map[string]struct{})
	for id := range gsm.gossipStates {
		peerSet[id] = struct{}{}
	}
	for id := range gsm.latencyStates {
		peerSet[id] = struct{}{}
	}

	w := gsm.config.Weights
	var banned []string
	for id := range peerSet {
		if gsm.isGossipBannedLocked(id) {
			continue
		}
		protocol := gsm.protocolComponent(id)
		gossip := gsm.gossipComponent(id)
		latency := gsm.latencyComponent(id)
		composite := w.ProtocolWeight*protocol + w.GossipWeight*gossip + w.LatencyWeight*latency

		if composite <= gsm.config.GossipBanThreshold {
			gsm.gossipBanned[id] = time.Now()
			banned = append(banned, id)
		}
	}
	return banned
}

// IsGossipBanned returns whether the peer is currently gossip-banned.
func (gsm *GossipScoreManager) IsGossipBanned(peerID string) bool {
	gsm.mu.RLock()
	defer gsm.mu.RUnlock()
	return gsm.isGossipBannedLocked(peerID)
}

// RemoveGossipBan lifts the gossip ban on a peer.
func (gsm *GossipScoreManager) RemoveGossipBan(peerID string) {
	gsm.mu.Lock()
	defer gsm.mu.Unlock()
	delete(gsm.gossipBanned, peerID)
}

// PurgePeer removes all scoring state for a peer.
func (gsm *GossipScoreManager) PurgePeer(peerID string) {
	gsm.mu.Lock()
	defer gsm.mu.Unlock()
	delete(gsm.gossipStates, peerID)
	delete(gsm.latencyStates, peerID)
	delete(gsm.gossipBanned, peerID)
}

// PeerCount returns the number of peers with any scoring state.
func (gsm *GossipScoreManager) PeerCount() int {
	gsm.mu.RLock()
	defer gsm.mu.RUnlock()

	peerSet := make(map[string]struct{})
	for id := range gsm.gossipStates {
		peerSet[id] = struct{}{}
	}
	for id := range gsm.latencyStates {
		peerSet[id] = struct{}{}
	}
	return len(peerSet)
}

// --- internal helpers (caller must hold appropriate lock) ---

func (gsm *GossipScoreManager) getOrCreateGossip(peerID string) *peerGossipState {
	gs, ok := gsm.gossipStates[peerID]
	if !ok {
		gs = &peerGossipState{topicScores: make(map[string]float64)}
		gsm.gossipStates[peerID] = gs
	}
	return gs
}

func (gsm *GossipScoreManager) getOrCreateLatency(peerID string) *peerLatencyState {
	ls, ok := gsm.latencyStates[peerID]
	if !ok {
		ls = &peerLatencyState{}
		gsm.latencyStates[peerID] = ls
	}
	return ls
}

// protocolComponent returns the normalized protocol score [-100, 100] for use
// as a composite component. The caller must hold at least gsm.mu.RLock.
func (gsm *GossipScoreManager) protocolComponent(peerID string) float64 {
	return gsm.scorer.GetScore(peerID)
}

// gossipComponent aggregates all per-topic scores for a peer and normalizes
// to roughly [-100, 100]. The caller must hold gsm.mu.RLock.
func (gsm *GossipScoreManager) gossipComponent(peerID string) float64 {
	gs, ok := gsm.gossipStates[peerID]
	if !ok {
		return 0
	}
	var total float64
	for _, score := range gs.topicScores {
		total += score
	}
	// Clamp to [-100, 100].
	if total > 100 {
		total = 100
	}
	if total < -100 {
		total = -100
	}
	return total
}

// latencyComponent returns a score in [0, 100] based on the peer's average
// response latency. Lower latency earns a higher score.
func (gsm *GossipScoreManager) latencyComponent(peerID string) float64 {
	ls, ok := gsm.latencyStates[peerID]
	if !ok || ls.samples == 0 {
		return 50.0 // neutral when no data
	}

	targetMs := float64(gsm.config.LatencyTarget.Milliseconds())
	penaltyMs := float64(gsm.config.LatencyPenalty.Milliseconds())
	if penaltyMs <= targetMs {
		penaltyMs = targetMs + 1
	}

	if ls.avgMs <= targetMs {
		return 100.0
	}
	if ls.avgMs >= penaltyMs {
		return 0.0
	}

	// Linear interpolation between target (100) and penalty (0).
	frac := (ls.avgMs - targetMs) / (penaltyMs - targetMs)
	frac = math.Min(math.Max(frac, 0), 1)
	return 100.0 * (1.0 - frac)
}

func (gsm *GossipScoreManager) isGossipBannedLocked(peerID string) bool {
	_, ok := gsm.gossipBanned[peerID]
	return ok
}
