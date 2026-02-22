package p2p

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// Errors returned by ReputationTracker operations.
var (
	ErrReputationPeerNotFound  = errors.New("p2p: peer not found in reputation tracker")
	ErrReputationAlreadyBanned = errors.New("p2p: peer is already banned")
)

// Event type constants for reputation tracking.
const (
	EventGoodBlock       = "good_block"
	EventBadBlock        = "bad_block"
	EventTimeout         = "timeout"
	EventDisconnect      = "disconnect"
	EventGoodAttestation = "good_attestation"
	EventBadAttestation  = "bad_attestation"
)

// eventDeltas maps event types to their default score adjustments.
var eventDeltas = map[string]float64{
	EventGoodBlock:       10.0,
	EventBadBlock:        -25.0,
	EventTimeout:         -15.0,
	EventDisconnect:      -10.0,
	EventGoodAttestation: 5.0,
	EventBadAttestation:  -20.0,
}

// ReputationConfig configures the ReputationTracker.
type ReputationConfig struct {
	InitialScore  float64 // Starting score for new peers.
	MaxScore      float64 // Upper bound for peer scores.
	MinScore      float64 // Lower bound (ban threshold).
	DecayInterval int64   // Interval in seconds between decay applications.
	DecayRate     float64 // Multiplicative factor per decay (e.g., 0.95).
}

// DefaultReputationConfig returns a sensible default configuration.
func DefaultReputationConfig() ReputationConfig {
	return ReputationConfig{
		InitialScore:  100.0,
		MaxScore:      200.0,
		MinScore:      -100.0,
		DecayInterval: 60,
		DecayRate:     0.95,
	}
}

// PeerReputation holds reputation state for a single peer.
type PeerReputation struct {
	PeerID    string
	Score     float64
	Events    int
	LastEvent time.Time
	Banned    bool
	BanUntil  time.Time
}

// ReputationEvent records a single reputation-affecting event.
type ReputationEvent struct {
	PeerID    string
	EventType string
	Delta     float64
	Timestamp time.Time
	Reason    string
}

// ReputationTracker tracks peer behavior scores for quality-of-service
// decisions. All methods are safe for concurrent use.
type ReputationTracker struct {
	mu       sync.RWMutex
	config   ReputationConfig
	peers    map[string]*PeerReputation
	eventLog []ReputationEvent
}

// NewReputationTracker creates a new ReputationTracker with the given config.
func NewReputationTracker(config ReputationConfig) *ReputationTracker {
	if config.MaxScore <= config.MinScore {
		config.MaxScore = 200.0
		config.MinScore = -100.0
	}
	if config.DecayRate <= 0 || config.DecayRate > 1 {
		config.DecayRate = 0.95
	}
	return &ReputationTracker{
		config:   config,
		peers:    make(map[string]*PeerReputation),
		eventLog: make([]ReputationEvent, 0, 256),
	}
}

// RecordEvent records a behavior event for a peer. If the peer is not yet
// tracked, it is created with the initial score. The event type determines
// the score delta applied.
func (rt *ReputationTracker) RecordEvent(peerID string, eventType string, reason string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	peer := rt.getOrCreate(peerID)
	now := time.Now()

	delta, ok := eventDeltas[eventType]
	if !ok {
		delta = 0
	}

	peer.Score += delta
	peer.Events++
	peer.LastEvent = now

	// Clamp score.
	peer.Score = rt.clamp(peer.Score)

	// Auto-ban if score drops to or below min threshold.
	if !peer.Banned && peer.Score <= rt.config.MinScore {
		peer.Banned = true
	}

	// Record the event.
	rt.eventLog = append(rt.eventLog, ReputationEvent{
		PeerID:    peerID,
		EventType: eventType,
		Delta:     delta,
		Timestamp: now,
		Reason:    reason,
	})
}

// GetScore returns the current score for a peer. Returns an error if the
// peer is not tracked.
func (rt *ReputationTracker) GetScore(peerID string) (float64, error) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	peer, ok := rt.peers[peerID]
	if !ok {
		return 0, ErrReputationPeerNotFound
	}
	return peer.Score, nil
}

// IsBanned returns whether the given peer is currently banned. A peer that
// was given a timed ban is automatically unbanned once the ban expires.
func (rt *ReputationTracker) IsBanned(peerID string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	peer, ok := rt.peers[peerID]
	if !ok {
		return false
	}
	if peer.Banned && !peer.BanUntil.IsZero() && time.Now().After(peer.BanUntil) {
		peer.Banned = false
		peer.BanUntil = time.Time{}
		return false
	}
	return peer.Banned
}

// BanPeer manually bans a peer for the given duration in seconds.
// Returns an error if the peer is already banned.
func (rt *ReputationTracker) BanPeer(peerID string, duration int64) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	peer := rt.getOrCreate(peerID)

	if peer.Banned {
		return ErrReputationAlreadyBanned
	}

	peer.Banned = true
	if duration > 0 {
		peer.BanUntil = time.Now().Add(time.Duration(duration) * time.Second)
	}
	return nil
}

// UnbanPeer removes the ban from a peer and resets their score to the
// initial score. Returns an error if the peer is not tracked.
func (rt *ReputationTracker) UnbanPeer(peerID string) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	peer, ok := rt.peers[peerID]
	if !ok {
		return ErrReputationPeerNotFound
	}

	peer.Banned = false
	peer.BanUntil = time.Time{}
	peer.Score = rt.config.InitialScore
	return nil
}

// DecayScores applies the configured decay rate to all peer scores,
// moving them toward zero. Scores are clamped after decay.
func (rt *ReputationTracker) DecayScores() {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	for _, peer := range rt.peers {
		peer.Score *= rt.config.DecayRate
		peer.Score = rt.clamp(peer.Score)
	}
}

// TopPeers returns the n highest-scored peers, sorted by score descending.
// Banned peers are excluded.
func (rt *ReputationTracker) TopPeers(n int) []PeerReputation {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	return rt.sortedPeers(n, true)
}

// BottomPeers returns the n lowest-scored peers, sorted by score ascending.
// Banned peers are included.
func (rt *ReputationTracker) BottomPeers(n int) []PeerReputation {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	return rt.sortedPeers(n, false)
}

// PeerCount returns the total number of tracked peers (including banned).
func (rt *ReputationTracker) PeerCount() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return len(rt.peers)
}

// BannedCount returns the number of currently banned peers.
func (rt *ReputationTracker) BannedCount() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	count := 0
	for _, peer := range rt.peers {
		if peer.Banned {
			count++
		}
	}
	return count
}

// getOrCreate returns the reputation for a peer, creating it if it does
// not exist. Caller must hold rt.mu (write lock).
func (rt *ReputationTracker) getOrCreate(peerID string) *PeerReputation {
	peer, ok := rt.peers[peerID]
	if !ok {
		peer = &PeerReputation{
			PeerID: peerID,
			Score:  rt.config.InitialScore,
		}
		rt.peers[peerID] = peer
	}
	return peer
}

// clamp restricts score to [MinScore, MaxScore].
func (rt *ReputationTracker) clamp(score float64) float64 {
	if score > rt.config.MaxScore {
		return rt.config.MaxScore
	}
	if score < rt.config.MinScore {
		return rt.config.MinScore
	}
	return score
}

// sortedPeers returns n peers sorted by score. If topFirst is true, returns
// highest scores first (excluding banned); otherwise returns lowest first
// (including all). Caller must hold rt.mu.RLock.
func (rt *ReputationTracker) sortedPeers(n int, topFirst bool) []PeerReputation {
	list := make([]PeerReputation, 0, len(rt.peers))
	for _, peer := range rt.peers {
		if topFirst && peer.Banned {
			continue
		}
		list = append(list, *peer)
	}

	if topFirst {
		sort.Slice(list, func(i, j int) bool {
			return list[i].Score > list[j].Score
		})
	} else {
		sort.Slice(list, func(i, j int) bool {
			return list[i].Score < list[j].Score
		})
	}

	if n > len(list) {
		n = len(list)
	}
	if n < 0 {
		n = 0
	}
	return list[:n]
}
