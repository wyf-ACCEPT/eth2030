// bid_scoring.go implements builder bid scoring for ePBS slot auctions.
// It provides a composite scoring system that weighs bid amount, builder
// reputation, inclusion quality, and latency to rank bids. This complements
// bid_validator.go (validation/selection) and auction.go (bid storage) by
// adding the scoring, ranking, reputation tracking, tiebreaking, and minimum
// bid enforcement needed for a production auction system.
//
// Components:
//   - BidScoreCalculator: computes composite bid scores
//   - ScoreComponents: individual score factors
//   - ReputationTracker: tracks builder reliability over time
//   - BidRanker: ranks bids by composite score
//   - TiebreakerRule: deterministic tiebreaking
//   - MinBidEnforcer: rejects bids below minimum
package epbs

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Bid scoring errors.
var (
	ErrBSNilBid          = errors.New("bid_scoring: nil bid")
	ErrBSNoBids          = errors.New("bid_scoring: no bids to rank")
	ErrBSBidBelowMinimum = errors.New("bid_scoring: bid value below minimum")
	ErrBSInvalidWeight   = errors.New("bid_scoring: invalid scoring weight")
	ErrBSBuilderNotFound = errors.New("bid_scoring: builder not found in reputation tracker")
)

// ScoreComponents holds the individual factors that contribute to a bid score.
type ScoreComponents struct {
	// BidAmount is the raw bid value in Gwei.
	BidAmount uint64

	// ReputationScore is the builder's reputation (0-100).
	ReputationScore float64

	// InclusionQuality is a measure of how many inclusion-list transactions
	// the builder includes (0.0 to 1.0).
	InclusionQuality float64

	// Latency is the bid submission latency in milliseconds.
	// Lower is better; used as a negative factor.
	LatencyMs uint64
}

// BidScoreConfig controls the weighting of each score component.
type BidScoreConfig struct {
	// AmountWeight is the weight for the normalized bid amount.
	AmountWeight float64

	// ReputationWeight is the weight for the reputation component.
	ReputationWeight float64

	// InclusionWeight is the weight for the inclusion quality component.
	InclusionWeight float64

	// LatencyWeight is the weight for the latency penalty component.
	LatencyWeight float64

	// MaxBidForNorm is the max bid value used for normalization.
	MaxBidForNorm uint64

	// MaxLatencyMs is the latency threshold above which the latency score is 0.
	MaxLatencyMs uint64
}

// DefaultBidScoreConfig returns balanced scoring defaults.
func DefaultBidScoreConfig() BidScoreConfig {
	return BidScoreConfig{
		AmountWeight:     0.50,
		ReputationWeight: 0.20,
		InclusionWeight:  0.15,
		LatencyWeight:    0.15,
		MaxBidForNorm:    100_000_000, // 0.1 ETH in Gwei
		MaxLatencyMs:     5000,        // 5 seconds
	}
}

// BidScoreCalculator computes composite scores for builder bids.
type BidScoreCalculator struct {
	config BidScoreConfig
}

// NewBidScoreCalculator creates a new calculator with the given config.
func NewBidScoreCalculator(config BidScoreConfig) (*BidScoreCalculator, error) {
	if config.MaxBidForNorm == 0 {
		return nil, fmt.Errorf("%w: MaxBidForNorm must be > 0", ErrBSInvalidWeight)
	}
	if config.MaxLatencyMs == 0 {
		return nil, fmt.Errorf("%w: MaxLatencyMs must be > 0", ErrBSInvalidWeight)
	}
	return &BidScoreCalculator{config: config}, nil
}

// ComputeScore calculates the composite score for a bid.
// Score is in the range [0.0, 1.0].
func (bsc *BidScoreCalculator) ComputeScore(components ScoreComponents) float64 {
	cfg := bsc.config

	// Normalize bid amount to [0, 1].
	normAmount := float64(components.BidAmount) / float64(cfg.MaxBidForNorm)
	if normAmount > 1.0 {
		normAmount = 1.0
	}

	// Normalize reputation to [0, 1].
	normRep := components.ReputationScore / 100.0
	normRep = clampFloat(normRep, 0.0, 1.0)

	// Inclusion quality is already [0, 1].
	normInc := clampFloat(components.InclusionQuality, 0.0, 1.0)

	// Latency: lower is better. Convert to score where 0ms=1.0, maxMs=0.0.
	var normLatency float64
	if components.LatencyMs >= cfg.MaxLatencyMs {
		normLatency = 0.0
	} else {
		normLatency = 1.0 - float64(components.LatencyMs)/float64(cfg.MaxLatencyMs)
	}

	score := cfg.AmountWeight*normAmount +
		cfg.ReputationWeight*normRep +
		cfg.InclusionWeight*normInc +
		cfg.LatencyWeight*normLatency

	score = clampFloat(score, 0.0, 1.0)

	// Round to 12 decimal places.
	return math.Round(score*1e12) / 1e12
}

// clampFloat clamps a value to [min, max].
func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// BuilderReputationEntry holds reputation data for a single builder.
type BuilderReputationEntry struct {
	BuilderAddr       types.Address
	Score             float64 // 0-100
	TotalBids         uint64
	SuccessfulReveals uint64
}

// Reliability returns the fraction of bids that resulted in successful reveals.
func (e *BuilderReputationEntry) Reliability() float64 {
	if e.TotalBids == 0 {
		return 1.0 // new builders get benefit of the doubt
	}
	return float64(e.SuccessfulReveals) / float64(e.TotalBids)
}

// ReputationTracker tracks builder reliability over time.
type ReputationTracker struct {
	mu       sync.RWMutex
	builders map[types.Address]*BuilderReputationEntry
}

// NewReputationTracker creates a new reputation tracker.
func NewReputationTracker() *ReputationTracker {
	return &ReputationTracker{
		builders: make(map[types.Address]*BuilderReputationEntry),
	}
}

// Register adds a builder to the tracker with an initial score.
func (rt *ReputationTracker) Register(addr types.Address, initialScore float64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.builders[addr] = &BuilderReputationEntry{
		BuilderAddr: addr,
		Score:       clampFloat(initialScore, 0.0, 100.0),
	}
}

// RecordBid records that a builder submitted a bid.
func (rt *ReputationTracker) RecordBid(addr types.Address) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if e, ok := rt.builders[addr]; ok {
		e.TotalBids++
	}
}

// RecordReveal records that a builder successfully revealed a payload.
func (rt *ReputationTracker) RecordReveal(addr types.Address) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if e, ok := rt.builders[addr]; ok {
		e.SuccessfulReveals++
		// Increase score by 1 per reveal, capped at 100.
		e.Score = clampFloat(e.Score+1.0, 0.0, 100.0)
	}
}

// RecordFailure records that a builder failed to reveal (penalty to score).
func (rt *ReputationTracker) RecordFailure(addr types.Address) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if e, ok := rt.builders[addr]; ok {
		// Decrease score by 5 per failure, floored at 0.
		e.Score = clampFloat(e.Score-5.0, 0.0, 100.0)
	}
}

// Get returns the reputation entry for a builder, or nil if not found.
func (rt *ReputationTracker) Get(addr types.Address) *BuilderReputationEntry {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	e, ok := rt.builders[addr]
	if !ok {
		return nil
	}
	cp := *e
	return &cp
}

// Count returns the number of tracked builders.
func (rt *ReputationTracker) Count() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return len(rt.builders)
}

// RankedBid pairs a bid with its computed score and metadata for ranking.
type RankedBid struct {
	Bid         *BuilderBid
	BuilderAddr types.Address
	Score       float64
	BidHash     types.Hash
}

// BidRanker ranks bids by composite score for selection.
type BidRanker struct {
	scorer *BidScoreCalculator
}

// NewBidRanker creates a new bid ranker with the given score calculator.
func NewBidRanker(scorer *BidScoreCalculator) *BidRanker {
	return &BidRanker{scorer: scorer}
}

// Rank sorts bids by score descending. Ties are broken deterministically
// by the lexicographically smallest bid hash.
func (br *BidRanker) Rank(bids []RankedBid) []RankedBid {
	if len(bids) == 0 {
		return bids
	}

	sorted := make([]RankedBid, len(bids))
	copy(sorted, bids)

	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Score != sorted[j].Score {
			return sorted[i].Score > sorted[j].Score
		}
		// Deterministic tiebreak: smaller bid hash wins.
		return bytes.Compare(sorted[i].BidHash[:], sorted[j].BidHash[:]) < 0
	})

	return sorted
}

// Winner returns the highest-ranked bid, or an error if no bids.
func (br *BidRanker) Winner(bids []RankedBid) (*RankedBid, error) {
	if len(bids) == 0 {
		return nil, ErrBSNoBids
	}
	ranked := br.Rank(bids)
	return &ranked[0], nil
}

// TiebreakerRule provides deterministic tiebreaking between two bids.
type TiebreakerRule struct{}

// NewTiebreakerRule creates a new tiebreaker rule.
func NewTiebreakerRule() *TiebreakerRule {
	return &TiebreakerRule{}
}

// Break resolves a tie between two bids. Returns the winner.
// Uses lexicographically smaller bid hash as the deterministic tiebreaker.
func (tbr *TiebreakerRule) Break(a, b RankedBid) RankedBid {
	if bytes.Compare(a.BidHash[:], b.BidHash[:]) <= 0 {
		return a
	}
	return b
}

// MinBidEnforcer rejects bids below a configurable minimum value.
type MinBidEnforcer struct {
	mu         sync.RWMutex
	minimumBid uint64 // in Gwei
}

// NewMinBidEnforcer creates an enforcer with the given minimum bid.
func NewMinBidEnforcer(minimumGwei uint64) *MinBidEnforcer {
	return &MinBidEnforcer{minimumBid: minimumGwei}
}

// Check validates that the bid value meets the minimum threshold.
func (mbe *MinBidEnforcer) Check(bid *BuilderBid) error {
	if bid == nil {
		return ErrBSNilBid
	}
	mbe.mu.RLock()
	min := mbe.minimumBid
	mbe.mu.RUnlock()

	if bid.Value < min {
		return fmt.Errorf("%w: bid %d Gwei, min %d Gwei",
			ErrBSBidBelowMinimum, bid.Value, min)
	}
	return nil
}

// SetMinimum updates the minimum bid threshold.
func (mbe *MinBidEnforcer) SetMinimum(minimumGwei uint64) {
	mbe.mu.Lock()
	defer mbe.mu.Unlock()
	mbe.minimumBid = minimumGwei
}

// Minimum returns the current minimum bid threshold.
func (mbe *MinBidEnforcer) Minimum() uint64 {
	mbe.mu.RLock()
	defer mbe.mu.RUnlock()
	return mbe.minimumBid
}

// FilterBids returns only bids that meet the minimum threshold.
func (mbe *MinBidEnforcer) FilterBids(bids []*BuilderBid) []*BuilderBid {
	mbe.mu.RLock()
	min := mbe.minimumBid
	mbe.mu.RUnlock()

	var result []*BuilderBid
	for _, bid := range bids {
		if bid != nil && bid.Value >= min {
			result = append(result, bid)
		}
	}
	return result
}
