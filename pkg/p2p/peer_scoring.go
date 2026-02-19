package p2p

import (
	"sort"
	"sync"
	"time"
)

// PeerScoreConfig configures the peer scoring system.
type PeerScoreConfig struct {
	InitialScore  float64       // Starting score for new peers (default 100.0).
	MaxScore      float64       // Upper score clamp (default 200.0).
	MinScore      float64       // Lower score clamp (default -100.0).
	BanThreshold  float64       // Score at or below which a peer is banned (default -50.0).
	DecayInterval time.Duration // How often scores decay toward neutral (default 1 minute).
	DecayFactor   float64       // Multiplicative decay per interval (default 0.95).
}

// DefaultPeerScoreConfig returns a PeerScoreConfig with sensible defaults.
func DefaultPeerScoreConfig() PeerScoreConfig {
	return PeerScoreConfig{
		InitialScore:  100.0,
		MaxScore:      200.0,
		MinScore:      -100.0,
		BanThreshold:  -50.0,
		DecayInterval: time.Minute,
		DecayFactor:   0.95,
	}
}

// ScoreEvent represents a discrete scoring event with a fixed delta.
type ScoreEvent float64

// Score event constants applied when peers perform various actions.
const (
	ScoreValidBlock   ScoreEvent = 5
	ScoreInvalidBlock ScoreEvent = -20
	ScoreValidTx      ScoreEvent = 1
	ScoreInvalidTx    ScoreEvent = -5
	ScoreTimedOut     ScoreEvent = -10
	ScoreReconnect    ScoreEvent = 2
	ScoreDuplicateMsg ScoreEvent = -1
	ScoreUsefulData   ScoreEvent = 3
)

// PeerScoreInfo holds the scoring state for a single peer.
type PeerScoreInfo struct {
	PeerID     string
	Score      float64
	Events     int
	BanCount   int
	LastUpdate time.Time
	Banned     bool
}

// PeerScorer manages per-peer reputation scores. It is safe for concurrent use.
type PeerScorer struct {
	mu     sync.RWMutex
	config PeerScoreConfig
	peers  map[string]*PeerScoreInfo
}

// NewPeerScorer creates a new PeerScorer with the given configuration.
func NewPeerScorer(config PeerScoreConfig) *PeerScorer {
	return &PeerScorer{
		config: config,
		peers:  make(map[string]*PeerScoreInfo),
	}
}

// getOrCreate returns the score info for a peer, creating one with the
// initial score if it doesn't exist. Caller must hold ps.mu (write lock).
func (ps *PeerScorer) getOrCreate(peerID string) *PeerScoreInfo {
	info, ok := ps.peers[peerID]
	if !ok {
		info = &PeerScoreInfo{
			PeerID:     peerID,
			Score:      ps.config.InitialScore,
			LastUpdate: time.Now(),
		}
		ps.peers[peerID] = info
	}
	return info
}

// RecordEvent applies a score event to the specified peer. If the peer
// is not yet tracked it is created with the initial score first.
// After adjusting, the score is clamped and ban status is re-evaluated.
func (ps *PeerScorer) RecordEvent(peerID string, event ScoreEvent) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	info := ps.getOrCreate(peerID)
	info.Score += float64(event)
	info.Events++
	info.LastUpdate = time.Now()

	// Clamp.
	if info.Score > ps.config.MaxScore {
		info.Score = ps.config.MaxScore
	}
	if info.Score < ps.config.MinScore {
		info.Score = ps.config.MinScore
	}

	// Auto-ban check.
	if !info.Banned && info.Score <= ps.config.BanThreshold {
		info.Banned = true
		info.BanCount++
	}
}

// GetScore returns the current score for a peer. If the peer is unknown
// the configured initial score is returned without registering the peer.
func (ps *PeerScorer) GetScore(peerID string) float64 {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if info, ok := ps.peers[peerID]; ok {
		return info.Score
	}
	return ps.config.InitialScore
}

// GetPeerInfo returns a copy of the full scoring info for a peer.
// If the peer is not tracked a zero-value info with the initial score is returned.
func (ps *PeerScorer) GetPeerInfo(peerID string) PeerScoreInfo {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if info, ok := ps.peers[peerID]; ok {
		return *info
	}
	return PeerScoreInfo{
		PeerID: peerID,
		Score:  ps.config.InitialScore,
	}
}

// IsBanned returns whether the peer is currently banned.
func (ps *PeerScorer) IsBanned(peerID string) bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if info, ok := ps.peers[peerID]; ok {
		return info.Banned
	}
	return false
}

// BanPeer forces a peer into the banned state regardless of score.
func (ps *PeerScorer) BanPeer(peerID string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	info := ps.getOrCreate(peerID)
	if !info.Banned {
		info.Banned = true
		info.BanCount++
	}
}

// UnbanPeer removes the ban on a peer, resetting its score to the initial value.
func (ps *PeerScorer) UnbanPeer(peerID string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if info, ok := ps.peers[peerID]; ok {
		info.Banned = false
		info.Score = ps.config.InitialScore
		info.LastUpdate = time.Now()
	}
}

// TopPeers returns the n highest-scoring non-banned peers, sorted descending.
func (ps *PeerScorer) TopPeers(n int) []PeerScoreInfo {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	list := make([]PeerScoreInfo, 0, len(ps.peers))
	for _, info := range ps.peers {
		if !info.Banned {
			list = append(list, *info)
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Score > list[j].Score
	})

	if n > len(list) {
		n = len(list)
	}
	return list[:n]
}

// PruneLowScoring removes all peers whose score is at or below the given
// threshold and returns their peer IDs. Banned peers are always pruned.
func (ps *PeerScorer) PruneLowScoring(threshold float64) []string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	var pruned []string
	for id, info := range ps.peers {
		if info.Score <= threshold || info.Banned {
			pruned = append(pruned, id)
			delete(ps.peers, id)
		}
	}
	return pruned
}

// DecayScores applies the configured decay factor to all peer scores,
// moving them toward zero. Scores are clamped after decay.
func (ps *PeerScorer) DecayScores() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	for _, info := range ps.peers {
		info.Score *= ps.config.DecayFactor

		if info.Score > ps.config.MaxScore {
			info.Score = ps.config.MaxScore
		}
		if info.Score < ps.config.MinScore {
			info.Score = ps.config.MinScore
		}
		info.LastUpdate = time.Now()
	}
}

// PeerCount returns the number of tracked peers (including banned).
func (ps *PeerScorer) PeerCount() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.peers)
}
