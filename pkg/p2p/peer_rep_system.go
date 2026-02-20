// peer_rep_system.go implements a multi-dimensional peer reputation system with
// per-category behavior scoring, exponential moving average decay, temporary and
// permanent banning, reputation-weighted peer selection, and metrics export.
package p2p

import (
	"errors"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/eth2028/eth2028/metrics"
)

// Errors returned by PeerRep operations.
var (
	ErrRepPeerNotTracked  = errors.New("p2p: peer not tracked in reputation system")
	ErrRepPeerBanned      = errors.New("p2p: peer is currently banned")
	ErrRepInvalidCategory = errors.New("p2p: invalid score category")
)

// RepCategory identifies a dimension of peer behavior scoring.
type RepCategory int

const (
	RepCatProtocol     RepCategory = iota // Protocol compliance score.
	RepCatLatency                         // Responsiveness score.
	RepCatBandwidth                       // Throughput and data quality score.
	RepCatAvailability                    // Uptime and connection stability score.
	repCatCount                           // sentinel, must be last
)

// String returns the category name.
func (c RepCategory) String() string {
	switch c {
	case RepCatProtocol:
		return "protocol"
	case RepCatLatency:
		return "latency"
	case RepCatBandwidth:
		return "bandwidth"
	case RepCatAvailability:
		return "availability"
	default:
		return "unknown"
	}
}

// BanReason classifies why a peer was banned.
type BanReason int

const (
	BanReasonNone              BanReason = iota
	BanReasonProtocolViolation           // Wire-protocol breach.
	BanReasonSpam                        // Excessive message flooding.
	BanReasonDoS                         // Denial-of-service behavior.
	BanReasonInvalidBlocks               // Serving invalid block data.
)

// String returns a human-readable ban reason.
func (r BanReason) String() string {
	switch r {
	case BanReasonProtocolViolation:
		return "protocol_violation"
	case BanReasonSpam:
		return "spam"
	case BanReasonDoS:
		return "dos"
	case BanReasonInvalidBlocks:
		return "invalid_blocks"
	default:
		return "none"
	}
}

// Category score weights for computing the composite reputation score.
// Normalized so sum = 1.0.
var defaultCategoryWeights = [repCatCount]float64{
	RepCatProtocol:     0.35,
	RepCatLatency:      0.20,
	RepCatBandwidth:    0.20,
	RepCatAvailability: 0.25,
}

// RepConfig configures the PeerRep reputation system.
type RepConfig struct {
	// InitialScore is the starting score for each category (default 100.0).
	InitialScore float64
	// MaxCategoryScore caps each individual category score (default 200.0).
	MaxCategoryScore float64
	// MinCategoryScore floors each individual category score (default -100.0).
	MinCategoryScore float64
	// DecayAlpha is the EMA smoothing factor (0 < alpha < 1). Lower values
	// decay faster toward the mean. Default: 0.95.
	DecayAlpha float64
	// CategoryWeights overrides the default category weights. Must sum to 1.0.
	CategoryWeights [repCatCount]float64
	// BanThreshold: composite score at or below which auto-ban triggers.
	BanThreshold float64
	// DefaultBanDuration is the temporary ban duration (default 1 hour).
	DefaultBanDuration time.Duration
	// HighRepThreshold: composite score above which a peer is high-priority.
	HighRepThreshold float64
}

// DefaultRepConfig returns a RepConfig with production defaults.
func DefaultRepConfig() RepConfig {
	return RepConfig{
		InitialScore:       100.0,
		MaxCategoryScore:   200.0,
		MinCategoryScore:   -100.0,
		DecayAlpha:         0.95,
		CategoryWeights:    defaultCategoryWeights,
		BanThreshold:       -20.0,
		DefaultBanDuration: time.Hour,
		HighRepThreshold:   120.0,
	}
}

// PeerRepEntry holds multi-dimensional reputation state for a single peer.
type PeerRepEntry struct {
	PeerID     string
	Scores     [repCatCount]float64 // Per-category scores.
	Composite  float64              // Weighted composite score.
	EventCount int                  // Total events recorded.
	LastUpdate time.Time
	Banned     bool
	Permanent  bool      // True if the ban is permanent.
	BanUntil   time.Time // Zero if permanent or not banned.
	BanRsn     BanReason
}

// PeerRep is the multi-dimensional peer reputation system. It tracks per-peer
// behavior across protocol compliance, latency, bandwidth, and availability
// categories. Scores decay toward the initial value using an exponential moving
// average. Peers whose composite score falls below BanThreshold are auto-banned.
// All methods are safe for concurrent use.
type PeerRep struct {
	mu      sync.RWMutex
	config  RepConfig
	peers   map[string]*PeerRepEntry
	banLog  []RepBanRecord
	nowFunc func() time.Time // injectable clock for testing

	// Metrics.
	metricAvgScore  *metrics.Gauge
	metricBanCount  *metrics.Counter
	metricHighCount *metrics.Gauge
	metricLowCount  *metrics.Gauge
	metricScoreDist *metrics.Histogram
}

// RepBanRecord logs a ban event.
type RepBanRecord struct {
	PeerID    string
	Reason    BanReason
	Permanent bool
	BanUntil  time.Time
	Timestamp time.Time
}

// NewPeerRep creates a new PeerRep with the given config.
func NewPeerRep(cfg RepConfig) *PeerRep {
	if cfg.InitialScore == 0 {
		cfg.InitialScore = 100.0
	}
	if cfg.MaxCategoryScore <= cfg.MinCategoryScore {
		cfg.MaxCategoryScore = 200.0
		cfg.MinCategoryScore = -100.0
	}
	if cfg.DecayAlpha <= 0 || cfg.DecayAlpha >= 1 {
		cfg.DecayAlpha = 0.95
	}
	if cfg.DefaultBanDuration <= 0 {
		cfg.DefaultBanDuration = time.Hour
	}
	// Validate weights sum to 1.0 (within epsilon).
	var wsum float64
	for _, w := range cfg.CategoryWeights {
		wsum += w
	}
	if math.Abs(wsum-1.0) > 0.01 {
		cfg.CategoryWeights = defaultCategoryWeights
	}

	return &PeerRep{
		config:          cfg,
		peers:           make(map[string]*PeerRepEntry),
		banLog:          make([]RepBanRecord, 0, 128),
		nowFunc:         time.Now,
		metricAvgScore:  metrics.NewGauge("p2p_rep_avg_score"),
		metricBanCount:  metrics.NewCounter("p2p_rep_ban_total"),
		metricHighCount: metrics.NewGauge("p2p_rep_high_peers"),
		metricLowCount:  metrics.NewGauge("p2p_rep_low_peers"),
		metricScoreDist: metrics.NewHistogram("p2p_rep_score_dist"),
	}
}

// RepAdjustScore adjusts the score for peerID in the given category by delta.
// Positive delta improves score, negative worsens it. The EMA is applied:
//
//	new = alpha * old + (1 - alpha) * (old + delta)
//
// which simplifies to: new = old + (1 - alpha) * delta.
// Returns the new composite score.
func (pr *PeerRep) RepAdjustScore(peerID string, cat RepCategory, delta float64) (float64, error) {
	if cat < 0 || cat >= repCatCount {
		return 0, ErrRepInvalidCategory
	}

	pr.mu.Lock()
	defer pr.mu.Unlock()

	entry := pr.repGetOrCreate(peerID)
	if entry.Banned {
		return entry.Composite, ErrRepPeerBanned
	}

	now := pr.nowFunc()
	alpha := pr.config.DecayAlpha

	// Apply EMA-weighted delta.
	old := entry.Scores[cat]
	entry.Scores[cat] = alpha*old + (1-alpha)*(old+delta)
	entry.Scores[cat] = pr.repClampCategory(entry.Scores[cat])
	entry.EventCount++
	entry.LastUpdate = now

	// Recompute composite.
	entry.Composite = pr.repComputeComposite(entry)

	// Auto-ban check.
	if entry.Composite <= pr.config.BanThreshold {
		pr.repBanLocked(entry, BanReasonProtocolViolation, false)
	}

	return entry.Composite, nil
}

// RepGetComposite returns the composite score for a peer, or an error if untracked.
func (pr *PeerRep) RepGetComposite(peerID string) (float64, error) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	entry, ok := pr.peers[peerID]
	if !ok {
		return 0, ErrRepPeerNotTracked
	}
	return entry.Composite, nil
}

// RepGetCategoryScore returns the score in a specific category for a peer.
func (pr *PeerRep) RepGetCategoryScore(peerID string, cat RepCategory) (float64, error) {
	if cat < 0 || cat >= repCatCount {
		return 0, ErrRepInvalidCategory
	}
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	entry, ok := pr.peers[peerID]
	if !ok {
		return 0, ErrRepPeerNotTracked
	}
	return entry.Scores[cat], nil
}

// RepGetEntry returns a copy of the full reputation entry for a peer.
func (pr *PeerRep) RepGetEntry(peerID string) (PeerRepEntry, error) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	entry, ok := pr.peers[peerID]
	if !ok {
		return PeerRepEntry{}, ErrRepPeerNotTracked
	}
	return *entry, nil
}

// RepBanTemp temporarily bans a peer for the given duration with the specified reason.
func (pr *PeerRep) RepBanTemp(peerID string, reason BanReason, duration time.Duration) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	entry := pr.repGetOrCreate(peerID)
	now := pr.nowFunc()
	entry.Banned = true
	entry.Permanent = false
	entry.BanUntil = now.Add(duration)
	entry.BanRsn = reason

	pr.banLog = append(pr.banLog, RepBanRecord{
		PeerID:    peerID,
		Reason:    reason,
		Permanent: false,
		BanUntil:  entry.BanUntil,
		Timestamp: now,
	})
	pr.metricBanCount.Inc()
}

// RepBanPermanent permanently bans a peer.
func (pr *PeerRep) RepBanPermanent(peerID string, reason BanReason) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	entry := pr.repGetOrCreate(peerID)
	now := pr.nowFunc()
	entry.Banned = true
	entry.Permanent = true
	entry.BanUntil = time.Time{} // zero = no expiry
	entry.BanRsn = reason

	pr.banLog = append(pr.banLog, RepBanRecord{
		PeerID:    peerID,
		Reason:    reason,
		Permanent: true,
		Timestamp: now,
	})
	pr.metricBanCount.Inc()
}

// RepIsBanned returns whether the peer is currently banned. Expired temporary
// bans are automatically lifted.
func (pr *PeerRep) RepIsBanned(peerID string) bool {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	entry, ok := pr.peers[peerID]
	if !ok {
		return false
	}
	if !entry.Banned {
		return false
	}
	// Check temporary ban expiry.
	if !entry.Permanent && !entry.BanUntil.IsZero() && pr.nowFunc().After(entry.BanUntil) {
		entry.Banned = false
		entry.BanRsn = BanReasonNone
		entry.BanUntil = time.Time{}
		for i := range entry.Scores {
			entry.Scores[i] = pr.config.InitialScore
		}
		entry.Composite = pr.repComputeComposite(entry)
		return false
	}
	return true
}

// RepUnban removes a ban from a peer and resets scores to initial values.
func (pr *PeerRep) RepUnban(peerID string) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	entry, ok := pr.peers[peerID]
	if !ok {
		return ErrRepPeerNotTracked
	}
	entry.Banned = false
	entry.Permanent = false
	entry.BanRsn = BanReasonNone
	entry.BanUntil = time.Time{}
	for i := range entry.Scores {
		entry.Scores[i] = pr.config.InitialScore
	}
	entry.Composite = pr.repComputeComposite(entry)
	return nil
}

// RepDecayAll applies exponential decay to all tracked (non-banned) peers,
// moving category scores toward the initial score.
//
//	new = alpha * current + (1 - alpha) * initial
func (pr *PeerRep) RepDecayAll() {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	alpha := pr.config.DecayAlpha
	initial := pr.config.InitialScore

	for _, entry := range pr.peers {
		if entry.Banned {
			continue
		}
		for i := range entry.Scores {
			entry.Scores[i] = alpha*entry.Scores[i] + (1-alpha)*initial
			entry.Scores[i] = pr.repClampCategory(entry.Scores[i])
		}
		entry.Composite = pr.repComputeComposite(entry)
	}
}

// RepSelectWeighted returns up to n peers selected randomly with probability
// proportional to their composite reputation score. Banned peers are excluded.
func (pr *PeerRep) RepSelectWeighted(n int) []string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	if n <= 0 {
		return nil
	}

	type candidate struct {
		id     string
		weight float64
	}
	var candidates []candidate
	var totalWeight float64

	for id, entry := range pr.peers {
		if entry.Banned {
			continue
		}
		// Shift composite so minimum possible maps to a small positive weight.
		w := entry.Composite - pr.config.MinCategoryScore + 1.0
		if w < 1.0 {
			w = 1.0
		}
		candidates = append(candidates, candidate{id: id, weight: w})
		totalWeight += w
	}

	if len(candidates) == 0 {
		return nil
	}
	if n > len(candidates) {
		n = len(candidates)
	}

	// Weighted sampling without replacement.
	selected := make(map[string]bool)
	result := make([]string, 0, n)

	for len(result) < n && totalWeight > 0 {
		r := rand.Float64() * totalWeight
		var cumulative float64
		for i, c := range candidates {
			if selected[c.id] {
				continue
			}
			cumulative += c.weight
			if r <= cumulative {
				result = append(result, c.id)
				selected[c.id] = true
				totalWeight -= c.weight
				candidates[i].weight = 0
				break
			}
		}
	}
	return result
}

// RepHighPeers returns peers whose composite score exceeds HighRepThreshold,
// sorted by composite score descending.
func (pr *PeerRep) RepHighPeers() []PeerRepEntry {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	var list []PeerRepEntry
	for _, entry := range pr.peers {
		if !entry.Banned && entry.Composite >= pr.config.HighRepThreshold {
			list = append(list, *entry)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Composite > list[j].Composite
	})
	return list
}

// RepTrackedCount returns the total number of tracked peers (including banned).
func (pr *PeerRep) RepTrackedCount() int {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return len(pr.peers)
}

// RepBannedCount returns the number of currently banned peers.
func (pr *PeerRep) RepBannedCount() int {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	count := 0
	for _, entry := range pr.peers {
		if entry.Banned {
			count++
		}
	}
	return count
}

// RepBanLog returns a copy of all ban records.
func (pr *PeerRep) RepBanLog() []RepBanRecord {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	log := make([]RepBanRecord, len(pr.banLog))
	copy(log, pr.banLog)
	return log
}

// RepRemovePeer removes a peer from tracking entirely.
func (pr *PeerRep) RepRemovePeer(peerID string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	delete(pr.peers, peerID)
}

// RepUpdateMetrics computes and exports current reputation metrics.
func (pr *PeerRep) RepUpdateMetrics() {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	if len(pr.peers) == 0 {
		pr.metricAvgScore.Set(0)
		pr.metricHighCount.Set(0)
		pr.metricLowCount.Set(0)
		return
	}

	var total float64
	var highCount, lowCount int64
	nonBanned := 0
	for _, entry := range pr.peers {
		if entry.Banned {
			continue
		}
		nonBanned++
		total += entry.Composite
		pr.metricScoreDist.Observe(entry.Composite)
		if entry.Composite >= pr.config.HighRepThreshold {
			highCount++
		}
		if entry.Composite <= pr.config.BanThreshold+20 {
			lowCount++
		}
	}

	if nonBanned > 0 {
		pr.metricAvgScore.Set(int64(total / float64(nonBanned)))
	}
	pr.metricHighCount.Set(highCount)
	pr.metricLowCount.Set(lowCount)
}

// --- internal helpers ---

// repGetOrCreate returns the entry for peerID, creating one if absent.
// Caller must hold pr.mu (write lock).
func (pr *PeerRep) repGetOrCreate(peerID string) *PeerRepEntry {
	entry, ok := pr.peers[peerID]
	if !ok {
		entry = &PeerRepEntry{
			PeerID:     peerID,
			LastUpdate: pr.nowFunc(),
		}
		for i := range entry.Scores {
			entry.Scores[i] = pr.config.InitialScore
		}
		entry.Composite = pr.repComputeComposite(entry)
		pr.peers[peerID] = entry
	}
	return entry
}

// repComputeComposite returns the weighted sum of category scores.
func (pr *PeerRep) repComputeComposite(entry *PeerRepEntry) float64 {
	var sum float64
	for i := 0; i < int(repCatCount); i++ {
		sum += entry.Scores[i] * pr.config.CategoryWeights[i]
	}
	return sum
}

// repClampCategory restricts a category score to the configured range.
func (pr *PeerRep) repClampCategory(v float64) float64 {
	if v > pr.config.MaxCategoryScore {
		return pr.config.MaxCategoryScore
	}
	if v < pr.config.MinCategoryScore {
		return pr.config.MinCategoryScore
	}
	return v
}

// repBanLocked bans the entry. Caller must hold pr.mu.
func (pr *PeerRep) repBanLocked(entry *PeerRepEntry, reason BanReason, permanent bool) {
	now := pr.nowFunc()
	entry.Banned = true
	entry.BanRsn = reason
	entry.Permanent = permanent
	if !permanent {
		entry.BanUntil = now.Add(pr.config.DefaultBanDuration)
	}
	pr.banLog = append(pr.banLog, RepBanRecord{
		PeerID:    entry.PeerID,
		Reason:    reason,
		Permanent: permanent,
		BanUntil:  entry.BanUntil,
		Timestamp: now,
	})
	pr.metricBanCount.Inc()
}
