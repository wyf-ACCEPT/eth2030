// peer_scorer.go implements a behavior-based peer scoring system with
// composite score computation, score decay, banning thresholds, peer
// ranking for connection prioritization, and IP-based colocation penalties.
package p2p

import (
	"math"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// BehaviorScorerConfig configures the BehaviorScorer.
type BehaviorScorerConfig struct {
	InitialScore       float64       // Starting score for new peers (default 50).
	MaxScore           float64       // Upper bound for scores (default 150).
	MinScore           float64       // Lower bound for scores (default -100).
	BanThreshold       float64       // Score at/below which a peer is banned (default -50).
	DecayInterval      time.Duration // Interval between decay applications (default 1m).
	DecayFactor        float64       // Multiplicative factor per decay toward zero (default 0.97).
	ColocationPenalty  float64       // Penalty per extra peer sharing the same /24 subnet (default -5).
	MaxColocationPeers int           // Above this count, penalty is applied (default 2).
	LatencyBonusCap    float64       // Maximum latency bonus (default 10).
	LatencyTargetMs    float64       // Target latency for full bonus (default 100ms).
	LatencyPenaltyMs   float64       // Latency above this incurs penalty (default 5000ms).
}

// DefaultBehaviorScorerConfig returns a BehaviorScorerConfig with sensible defaults.
func DefaultBehaviorScorerConfig() BehaviorScorerConfig {
	return BehaviorScorerConfig{
		InitialScore:       50.0,
		MaxScore:           150.0,
		MinScore:           -100.0,
		BanThreshold:       -50.0,
		DecayInterval:      time.Minute,
		DecayFactor:        0.97,
		ColocationPenalty:  -5.0,
		MaxColocationPeers: 2,
		LatencyBonusCap:    10.0,
		LatencyTargetMs:    100.0,
		LatencyPenaltyMs:   5000.0,
	}
}

// BehaviorMetrics holds tracked metrics for a single peer.
type BehaviorMetrics struct {
	PeerID         string
	IP             string    // Peer's IP address for colocation checks.
	Score          float64   // Composite score.
	ValidBlocks    int       // Count of valid blocks delivered.
	InvalidMsgs    int       // Count of invalid messages received.
	TimedOut       int       // Count of request timeouts.
	AvgLatencyMs   float64   // Exponentially weighted average latency.
	LatencySamples int       // Number of latency samples recorded.
	BanCount       int       // Number of times this peer has been banned.
	Banned         bool      // Whether the peer is currently banned.
	BannedAt       time.Time // When the peer was last banned.
	FirstSeen      time.Time // When the peer was first tracked.
	LastActivity   time.Time // When the last event was recorded.
}

// BehaviorScorer computes composite peer reputation scores from message
// latency, block delivery, invalid messages, and IP colocation. All
// methods are safe for concurrent use.
type BehaviorScorer struct {
	mu     sync.RWMutex
	config BehaviorScorerConfig
	peers  map[string]*BehaviorMetrics
}

// NewBehaviorScorer creates a BehaviorScorer with the given config.
func NewBehaviorScorer(config BehaviorScorerConfig) *BehaviorScorer {
	return &BehaviorScorer{
		config: config,
		peers:  make(map[string]*BehaviorMetrics),
	}
}

// getOrCreate returns the metrics for a peer, creating with defaults if needed.
// Caller must hold bs.mu write lock.
func (bs *BehaviorScorer) getOrCreate(peerID string) *BehaviorMetrics {
	m, ok := bs.peers[peerID]
	if !ok {
		now := time.Now()
		m = &BehaviorMetrics{
			PeerID:       peerID,
			Score:        bs.config.InitialScore,
			FirstSeen:    now,
			LastActivity: now,
		}
		bs.peers[peerID] = m
	}
	return m
}

// RegisterPeer creates or updates a peer's IP address for colocation checks.
func (bs *BehaviorScorer) RegisterPeer(peerID, ipAddr string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	m := bs.getOrCreate(peerID)
	m.IP = normalizeIP(ipAddr)
}

// RecordValidBlock records a valid block delivery from a peer.
func (bs *BehaviorScorer) RecordValidBlock(peerID string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	m := bs.getOrCreate(peerID)
	m.ValidBlocks++
	m.Score += 5.0
	m.LastActivity = time.Now()
	bs.clampLocked(m)
}

// RecordInvalidMessage records receipt of an invalid message from a peer.
func (bs *BehaviorScorer) RecordInvalidMessage(peerID string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	m := bs.getOrCreate(peerID)
	m.InvalidMsgs++
	m.Score -= 15.0
	m.LastActivity = time.Now()
	bs.clampLocked(m)
	bs.checkBanLocked(m)
}

// RecordTimeout records a request timeout for a peer.
func (bs *BehaviorScorer) RecordTimeout(peerID string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	m := bs.getOrCreate(peerID)
	m.TimedOut++
	m.Score -= 8.0
	m.LastActivity = time.Now()
	bs.clampLocked(m)
	bs.checkBanLocked(m)
}

// RecordLatency records a response latency sample for a peer using exponential
// weighted moving average with alpha=0.3.
func (bs *BehaviorScorer) RecordLatency(peerID string, latency time.Duration) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	m := bs.getOrCreate(peerID)
	ms := float64(latency.Milliseconds())
	if ms < 0 {
		ms = 0
	}
	const alpha = 0.3
	if m.LatencySamples == 0 {
		m.AvgLatencyMs = ms
	} else {
		m.AvgLatencyMs = alpha*ms + (1-alpha)*m.AvgLatencyMs
	}
	m.LatencySamples++
	m.LastActivity = time.Now()
}

// CompositeScore returns the final composite score for a peer including
// latency bonus/penalty and IP colocation penalty.
func (bs *BehaviorScorer) CompositeScore(peerID string) float64 {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	m, ok := bs.peers[peerID]
	if !ok {
		return bs.config.InitialScore
	}
	return bs.compositeLocked(m)
}

// compositeLocked computes the composite score. Caller must hold bs.mu (read).
func (bs *BehaviorScorer) compositeLocked(m *BehaviorMetrics) float64 {
	score := m.Score

	// Latency component.
	score += bs.latencyBonusLocked(m)

	// Colocation penalty.
	score += bs.colocationPenaltyLocked(m)

	// Clamp final composite.
	if score > bs.config.MaxScore {
		score = bs.config.MaxScore
	}
	if score < bs.config.MinScore {
		score = bs.config.MinScore
	}
	return score
}

// latencyBonusLocked computes a latency-based bonus or penalty.
// Low latency earns up to LatencyBonusCap; high latency earns a penalty.
func (bs *BehaviorScorer) latencyBonusLocked(m *BehaviorMetrics) float64 {
	if m.LatencySamples == 0 {
		return 0.0
	}
	target := bs.config.LatencyTargetMs
	penalty := bs.config.LatencyPenaltyMs
	if penalty <= target {
		penalty = target + 1
	}
	cap := bs.config.LatencyBonusCap

	if m.AvgLatencyMs <= target {
		return cap
	}
	if m.AvgLatencyMs >= penalty {
		return -cap
	}
	// Linear interpolation: target -> +cap, penalty -> -cap.
	frac := (m.AvgLatencyMs - target) / (penalty - target)
	return cap * (1.0 - 2.0*frac)
}

// colocationPenaltyLocked returns the IP colocation penalty for a peer.
func (bs *BehaviorScorer) colocationPenaltyLocked(m *BehaviorMetrics) float64 {
	if m.IP == "" {
		return 0.0
	}
	subnet := subnetOf(m.IP)
	if subnet == "" {
		return 0.0
	}
	count := 0
	for _, other := range bs.peers {
		if other.PeerID == m.PeerID || other.Banned {
			continue
		}
		if subnetOf(other.IP) == subnet {
			count++
		}
	}
	excess := count - bs.config.MaxColocationPeers
	if excess <= 0 {
		return 0.0
	}
	return float64(excess) * bs.config.ColocationPenalty
}

// IsBanned returns whether the peer is currently banned.
func (bs *BehaviorScorer) IsBanned(peerID string) bool {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	m, ok := bs.peers[peerID]
	if !ok {
		return false
	}
	return m.Banned
}

// BanPeer forces a peer into the banned state.
func (bs *BehaviorScorer) BanPeer(peerID string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	m := bs.getOrCreate(peerID)
	if !m.Banned {
		m.Banned = true
		m.BanCount++
		m.BannedAt = time.Now()
	}
}

// UnbanPeer removes the ban and resets the score to initial.
func (bs *BehaviorScorer) UnbanPeer(peerID string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	m, ok := bs.peers[peerID]
	if !ok {
		return
	}
	m.Banned = false
	m.Score = bs.config.InitialScore
	m.LastActivity = time.Now()
}

// GetMetrics returns a copy of the metrics for a peer.
func (bs *BehaviorScorer) GetMetrics(peerID string) BehaviorMetrics {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	m, ok := bs.peers[peerID]
	if !ok {
		return BehaviorMetrics{
			PeerID: peerID,
			Score:  bs.config.InitialScore,
		}
	}
	return *m
}

// RankedPeers returns up to n non-banned peers sorted by composite score (desc).
func (bs *BehaviorScorer) RankedPeers(n int) []BehaviorMetrics {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	type ranked struct {
		metrics BehaviorMetrics
		score   float64
	}
	var list []ranked
	for _, m := range bs.peers {
		if m.Banned {
			continue
		}
		cs := bs.compositeLocked(m)
		cp := *m
		list = append(list, ranked{metrics: cp, score: cs})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].score > list[j].score
	})
	if n > len(list) {
		n = len(list)
	}
	result := make([]BehaviorMetrics, n)
	for i := 0; i < n; i++ {
		result[i] = list[i].metrics
	}
	return result
}

// DecayScores applies score decay to all peers, moving scores toward zero.
func (bs *BehaviorScorer) DecayScores() {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	for _, m := range bs.peers {
		m.Score *= bs.config.DecayFactor
		bs.clampLocked(m)
	}
}

// RemovePeer removes a peer from the scorer entirely.
func (bs *BehaviorScorer) RemovePeer(peerID string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	delete(bs.peers, peerID)
}

// PeerCount returns the number of tracked peers.
func (bs *BehaviorScorer) PeerCount() int {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return len(bs.peers)
}

// BannedPeers returns the IDs of all currently banned peers.
func (bs *BehaviorScorer) BannedPeers() []string {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	var result []string
	for _, m := range bs.peers {
		if m.Banned {
			result = append(result, m.PeerID)
		}
	}
	return result
}

// SubnetPeerCount returns the number of active (non-banned) peers in
// the same /24 subnet as the given IP.
func (bs *BehaviorScorer) SubnetPeerCount(ipAddr string) int {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	subnet := subnetOf(normalizeIP(ipAddr))
	if subnet == "" {
		return 0
	}
	count := 0
	for _, m := range bs.peers {
		if m.Banned {
			continue
		}
		if subnetOf(m.IP) == subnet {
			count++
		}
	}
	return count
}

// clampLocked clamps the score to [MinScore, MaxScore].
func (bs *BehaviorScorer) clampLocked(m *BehaviorMetrics) {
	if m.Score > bs.config.MaxScore {
		m.Score = bs.config.MaxScore
	}
	if m.Score < bs.config.MinScore {
		m.Score = bs.config.MinScore
	}
}

// checkBanLocked checks if a peer should be auto-banned based on threshold.
func (bs *BehaviorScorer) checkBanLocked(m *BehaviorMetrics) {
	if !m.Banned && m.Score <= bs.config.BanThreshold {
		m.Banned = true
		m.BanCount++
		m.BannedAt = time.Now()
	}
}

// normalizeIP extracts the IP address from an addr string (ip:port or plain IP).
func normalizeIP(addr string) string {
	// Try host:port format first.
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(addr)
}

// subnetOf returns the /24 subnet prefix for an IPv4 address, or the /48
// prefix for IPv6. Returns empty string if the IP is invalid.
func subnetOf(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	if v4 := parsed.To4(); v4 != nil {
		// /24 subnet: first 3 octets.
		return net.IP(v4[:3]).String()
	}
	// IPv6: /48 subnet (first 6 bytes).
	if len(parsed) >= 6 {
		mask := net.CIDRMask(48, 128)
		masked := parsed.Mask(mask)
		return masked.String()
	}
	return ""
}

// StaleInactivePeers returns peer IDs that have not had activity for the
// given duration, useful for periodic cleanup.
func (bs *BehaviorScorer) StaleInactivePeers(inactiveThreshold time.Duration) []string {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	cutoff := time.Now().Add(-inactiveThreshold)
	var stale []string
	for _, m := range bs.peers {
		if m.LastActivity.Before(cutoff) {
			stale = append(stale, m.PeerID)
		}
	}
	return stale
}

// ScoreDistribution returns min, max, mean, and median composite scores
// for all non-banned peers.
func (bs *BehaviorScorer) ScoreDistribution() (min, max, mean, median float64) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	var scores []float64
	for _, m := range bs.peers {
		if m.Banned {
			continue
		}
		scores = append(scores, bs.compositeLocked(m))
	}
	if len(scores) == 0 {
		return 0, 0, 0, 0
	}
	sort.Float64s(scores)
	min = scores[0]
	max = scores[len(scores)-1]
	var sum float64
	for _, s := range scores {
		sum += s
	}
	mean = sum / float64(len(scores))
	mid := len(scores) / 2
	if len(scores)%2 == 0 {
		median = (scores[mid-1] + scores[mid]) / 2.0
	} else {
		median = scores[mid]
	}
	// Round to avoid floating point noise.
	mean = math.Round(mean*100) / 100
	median = math.Round(median*100) / 100
	return min, max, mean, median
}
