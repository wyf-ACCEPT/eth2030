// peer_reputation.go implements a comprehensive peer reputation system with
// time-based score decay, temporary banning, and sorted peer ranking.
package p2p

import (
	"sort"
	"sync"
	"time"
)

// ReputationRecord stores behavioral metrics and ban state for a single peer.
type ReputationRecord struct {
	PeerID      string
	Score       float64
	Successes   int
	Failures    int
	LastUpdated time.Time
	BannedUntil time.Time // Zero value means not banned.
}

// IsBanned returns true if the peer is currently banned.
func (r *ReputationRecord) IsBanned() bool {
	if r.BannedUntil.IsZero() {
		return false
	}
	return time.Now().Before(r.BannedUntil)
}

// ReputationSystemConfig configures the PeerReputationSystem.
type ReputationSystemConfig struct {
	InitialScore float64 // Starting score for newly tracked peers.
	MaxScore     float64 // Upper bound for peer scores.
	MinScore     float64 // Lower bound for peer scores.
	SuccessDelta float64 // Score increase per successful interaction.
	FailureDelta float64 // Score decrease per failed interaction.
	DecayFactor  float64 // Multiplicative decay applied each cycle (0 < x < 1).
	BanThreshold float64 // Score at or below which auto-ban triggers.
}

// DefaultReputationSystemConfig returns sensible defaults for the reputation system.
func DefaultReputationSystemConfig() ReputationSystemConfig {
	return ReputationSystemConfig{
		InitialScore: 50.0,
		MaxScore:     100.0,
		MinScore:     -100.0,
		SuccessDelta: 5.0,
		FailureDelta: -10.0,
		DecayFactor:  0.95,
		BanThreshold: -50.0,
	}
}

// PeerReputationSystem manages per-peer behavior scores with decay, banning,
// and ranked retrieval. All methods are safe for concurrent use.
type PeerReputationSystem struct {
	mu      sync.RWMutex
	config  ReputationSystemConfig
	records map[string]*ReputationRecord
}

// NewPeerReputationSystem creates a reputation system with the given config.
func NewPeerReputationSystem(cfg ReputationSystemConfig) *PeerReputationSystem {
	if cfg.MaxScore <= cfg.MinScore {
		cfg.MaxScore = 100.0
		cfg.MinScore = -100.0
	}
	if cfg.DecayFactor <= 0 || cfg.DecayFactor >= 1 {
		cfg.DecayFactor = 0.95
	}
	return &PeerReputationSystem{
		config:  cfg,
		records: make(map[string]*ReputationRecord),
	}
}

// getOrCreate returns the record for a peer, creating one with the initial
// score if it does not exist. Caller must hold prs.mu (write lock).
func (prs *PeerReputationSystem) getOrCreate(peerID string) *ReputationRecord {
	rec, ok := prs.records[peerID]
	if !ok {
		rec = &ReputationRecord{
			PeerID:      peerID,
			Score:       prs.config.InitialScore,
			LastUpdated: time.Now(),
		}
		prs.records[peerID] = rec
	}
	return rec
}

// clamp restricts a score to [MinScore, MaxScore].
func (prs *PeerReputationSystem) clamp(score float64) float64 {
	if score > prs.config.MaxScore {
		return prs.config.MaxScore
	}
	if score < prs.config.MinScore {
		return prs.config.MinScore
	}
	return score
}

// RecordSuccess adjusts a peer's score upward by SuccessDelta. If the peer
// is not yet tracked, it is created with InitialScore first.
func (prs *PeerReputationSystem) RecordSuccess(peerID string) {
	prs.mu.Lock()
	defer prs.mu.Unlock()

	rec := prs.getOrCreate(peerID)
	rec.Score += prs.config.SuccessDelta
	rec.Score = prs.clamp(rec.Score)
	rec.Successes++
	rec.LastUpdated = time.Now()
}

// RecordFailure adjusts a peer's score downward by FailureDelta and triggers
// an auto-ban if the score drops to or below BanThreshold.
func (prs *PeerReputationSystem) RecordFailure(peerID string) {
	prs.mu.Lock()
	defer prs.mu.Unlock()

	rec := prs.getOrCreate(peerID)
	rec.Score += prs.config.FailureDelta
	rec.Score = prs.clamp(rec.Score)
	rec.Failures++
	rec.LastUpdated = time.Now()

	// Auto-ban check.
	if rec.BannedUntil.IsZero() && rec.Score <= prs.config.BanThreshold {
		rec.BannedUntil = time.Now().Add(10 * time.Minute)
	}
}

// AdjustScore applies an arbitrary delta to a peer's score with clamping.
func (prs *PeerReputationSystem) AdjustScore(peerID string, delta float64) {
	prs.mu.Lock()
	defer prs.mu.Unlock()

	rec := prs.getOrCreate(peerID)
	rec.Score += delta
	rec.Score = prs.clamp(rec.Score)
	rec.LastUpdated = time.Now()
}

// GetScore returns the current score for a peer. Returns InitialScore
// if the peer is not tracked (without creating a record).
func (prs *PeerReputationSystem) GetScore(peerID string) float64 {
	prs.mu.RLock()
	defer prs.mu.RUnlock()

	if rec, ok := prs.records[peerID]; ok {
		return rec.Score
	}
	return prs.config.InitialScore
}

// GetRecord returns a copy of the full reputation record for a peer.
// Returns nil if the peer is not tracked.
func (prs *PeerReputationSystem) GetRecord(peerID string) *ReputationRecord {
	prs.mu.RLock()
	defer prs.mu.RUnlock()

	rec, ok := prs.records[peerID]
	if !ok {
		return nil
	}
	cp := *rec
	return &cp
}

// Ban temporarily bans a peer for the given duration. The peer is created
// with InitialScore if not already tracked.
func (prs *PeerReputationSystem) Ban(peerID string, duration time.Duration) {
	prs.mu.Lock()
	defer prs.mu.Unlock()

	rec := prs.getOrCreate(peerID)
	rec.BannedUntil = time.Now().Add(duration)
	rec.LastUpdated = time.Now()
}

// Unban removes the ban from a peer. No-op if the peer is not tracked.
func (prs *PeerReputationSystem) Unban(peerID string) {
	prs.mu.Lock()
	defer prs.mu.Unlock()

	if rec, ok := prs.records[peerID]; ok {
		rec.BannedUntil = time.Time{}
		rec.LastUpdated = time.Now()
	}
}

// IsAllowed returns true if the peer is not currently banned. An untracked
// peer is considered allowed.
func (prs *PeerReputationSystem) IsAllowed(peerID string) bool {
	prs.mu.RLock()
	defer prs.mu.RUnlock()

	rec, ok := prs.records[peerID]
	if !ok {
		return true
	}
	return !rec.IsBanned()
}

// DecayScores applies the configured decay factor to all peer scores,
// moving them toward zero. Scores are clamped after decay.
func (prs *PeerReputationSystem) DecayScores() {
	prs.mu.Lock()
	defer prs.mu.Unlock()

	now := time.Now()
	for _, rec := range prs.records {
		rec.Score *= prs.config.DecayFactor
		rec.Score = prs.clamp(rec.Score)
		rec.LastUpdated = now
	}
}

// SortedPeers returns all non-banned peers sorted by score descending.
func (prs *PeerReputationSystem) SortedPeers() []ReputationRecord {
	prs.mu.RLock()
	defer prs.mu.RUnlock()

	list := make([]ReputationRecord, 0, len(prs.records))
	for _, rec := range prs.records {
		if !rec.IsBanned() {
			list = append(list, *rec)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Score > list[j].Score
	})
	return list
}

// TopN returns the top N non-banned peers by score, sorted descending.
func (prs *PeerReputationSystem) TopN(n int) []ReputationRecord {
	all := prs.SortedPeers()
	if n > len(all) {
		n = len(all)
	}
	return all[:n]
}

// PeerCount returns the total number of tracked peers (including banned).
func (prs *PeerReputationSystem) PeerCount() int {
	prs.mu.RLock()
	defer prs.mu.RUnlock()
	return len(prs.records)
}

// BannedCount returns the number of currently banned peers.
func (prs *PeerReputationSystem) BannedCount() int {
	prs.mu.RLock()
	defer prs.mu.RUnlock()

	count := 0
	for _, rec := range prs.records {
		if rec.IsBanned() {
			count++
		}
	}
	return count
}

// RemovePeer deletes a peer's record from the system entirely.
func (prs *PeerReputationSystem) RemovePeer(peerID string) {
	prs.mu.Lock()
	defer prs.mu.Unlock()
	delete(prs.records, peerID)
}

// CleanExpiredBans removes ban entries for peers whose bans have expired,
// returning the list of peer IDs that were unbanned.
func (prs *PeerReputationSystem) CleanExpiredBans() []string {
	prs.mu.Lock()
	defer prs.mu.Unlock()

	now := time.Now()
	var cleaned []string
	for id, rec := range prs.records {
		if !rec.BannedUntil.IsZero() && now.After(rec.BannedUntil) {
			rec.BannedUntil = time.Time{}
			cleaned = append(cleaned, id)
		}
	}
	return cleaned
}
