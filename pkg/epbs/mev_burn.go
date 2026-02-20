// mev_burn.go implements the MEV-burn mechanism for ePBS.
//
// MEV-burn redirects a portion of the MEV captured by builders from the
// proposer to be burned (removed from circulation), reducing the incentive
// for proposer-builder collusion and redistributing MEV value to all ETH
// holders through supply reduction.
//
// Key components:
//
//   - MEVBurnConfig: configurable burn fraction, smoothing factor, and
//     minimum burn threshold.
//   - ComputeMEVBurn: splits a bid value into burn and proposer portions.
//   - MEVBurnTracker: tracks cumulative burns per epoch and maintains an
//     exponential moving average (EMA) of recent bid values.
//   - EstimateSmoothedBurn: computes the EMA of MEV across recent bids.
//   - ValidateBurnAmount: verifies a claimed burn amount against the
//     computed value within a configurable tolerance.
package epbs

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

// MEV burn errors.
var (
	ErrMEVBurnInvalidFraction   = errors.New("mev_burn: burn fraction must be in [0.0, 1.0]")
	ErrMEVBurnInvalidSmoothing  = errors.New("mev_burn: smoothing factor must be in (0.0, 1.0]")
	ErrMEVBurnValidationFailed  = errors.New("mev_burn: burn amount validation failed")
	ErrMEVBurnNoBids            = errors.New("mev_burn: no bids for smoothed estimate")
	ErrMEVBurnInvalidTolerance  = errors.New("mev_burn: tolerance must be in [0.0, 1.0]")
)

// MEVBurnConfig configures the MEV-burn mechanism.
type MEVBurnConfig struct {
	// BurnFraction is the fraction of the bid value to burn (0.0 to 1.0).
	// For example, 0.5 means 50% of the bid value is burned.
	BurnFraction float64

	// SmoothingFactor controls the EMA smoothing for bid value estimation.
	// Higher values give more weight to recent bids (0.0 to 1.0).
	// A value of 0.1 means 10% weight on the latest bid.
	SmoothingFactor float64

	// MinBurnThreshold is the minimum bid value (in Gwei) below which
	// no burn is applied. This avoids burning negligible amounts.
	MinBurnThreshold uint64

	// Tolerance is the maximum relative deviation allowed when validating
	// a burn amount against the computed value (0.0 to 1.0).
	// For example, 0.01 means 1% tolerance.
	Tolerance float64
}

// DefaultMEVBurnConfig returns production defaults for MEV burn.
func DefaultMEVBurnConfig() MEVBurnConfig {
	return MEVBurnConfig{
		BurnFraction:     0.50, // burn 50% of MEV
		SmoothingFactor:  0.10, // 10% weight to latest bid in EMA
		MinBurnThreshold: 100,  // minimum 100 Gwei to trigger burn
		Tolerance:        0.01, // 1% validation tolerance
	}
}

// ValidateMEVBurnConfig checks that the config values are within valid ranges.
func ValidateMEVBurnConfig(config MEVBurnConfig) error {
	if config.BurnFraction < 0.0 || config.BurnFraction > 1.0 {
		return fmt.Errorf("%w: got %f", ErrMEVBurnInvalidFraction, config.BurnFraction)
	}
	if config.SmoothingFactor <= 0.0 || config.SmoothingFactor > 1.0 {
		return fmt.Errorf("%w: got %f", ErrMEVBurnInvalidSmoothing, config.SmoothingFactor)
	}
	if config.Tolerance < 0.0 || config.Tolerance > 1.0 {
		return fmt.Errorf("%w: got %f", ErrMEVBurnInvalidTolerance, config.Tolerance)
	}
	return nil
}

// MEVBurnResult holds the breakdown of a bid value into burn and proposer
// portions.
type MEVBurnResult struct {
	// BidValue is the original bid value in Gwei.
	BidValue uint64

	// BurnAmount is the portion burned (removed from circulation) in Gwei.
	BurnAmount uint64

	// ProposerPayment is the portion paid to the proposer in Gwei.
	ProposerPayment uint64
}

// ComputeMEVBurn calculates how much of a bid value to burn versus pay
// to the proposer based on the configured burn fraction.
//
// If the bid value is below the minimum burn threshold, no burn is applied
// and the full amount goes to the proposer.
//
// Returns the burn breakdown result.
func ComputeMEVBurn(bidValue uint64, config MEVBurnConfig) MEVBurnResult {
	if bidValue < config.MinBurnThreshold || config.BurnFraction == 0.0 {
		return MEVBurnResult{
			BidValue:        bidValue,
			BurnAmount:      0,
			ProposerPayment: bidValue,
		}
	}

	// Compute burn: floor(bidValue * burnFraction).
	burnAmount := uint64(math.Floor(float64(bidValue) * config.BurnFraction))

	// Ensure burn does not exceed bid value (safety clamp).
	if burnAmount > bidValue {
		burnAmount = bidValue
	}

	proposerPayment := bidValue - burnAmount

	return MEVBurnResult{
		BidValue:        bidValue,
		BurnAmount:      burnAmount,
		ProposerPayment: proposerPayment,
	}
}

// EpochBurnStats tracks cumulative burn statistics for an epoch.
type EpochBurnStats struct {
	Epoch          uint64 `json:"epoch"`
	TotalBurned    uint64 `json:"totalBurned"`
	TotalBidValue  uint64 `json:"totalBidValue"`
	BidCount       uint64 `json:"bidCount"`
}

// BurnRate returns the fraction of total bid value that was burned
// in this epoch, or 0.0 if no bids were processed.
func (s *EpochBurnStats) BurnRate() float64 {
	if s.TotalBidValue == 0 {
		return 0.0
	}
	return float64(s.TotalBurned) / float64(s.TotalBidValue)
}

// MEVBurnTracker tracks MEV burn statistics across epochs and maintains
// an exponential moving average of bid values. Thread-safe.
type MEVBurnTracker struct {
	mu            sync.RWMutex
	config        MEVBurnConfig
	epochs        map[uint64]*EpochBurnStats // epoch -> stats
	ema           float64                    // current EMA of bid values
	emaInitialized bool
	totalBurned   uint64 // lifetime cumulative burn
	totalBids     uint64 // lifetime bid count
}

// NewMEVBurnTracker creates a new MEV burn tracker.
func NewMEVBurnTracker(config MEVBurnConfig) *MEVBurnTracker {
	return &MEVBurnTracker{
		config: config,
		epochs: make(map[uint64]*EpochBurnStats),
	}
}

// RecordBurn records a burn event for a given epoch. It updates the
// epoch stats, cumulative totals, and the EMA.
func (t *MEVBurnTracker) RecordBurn(epoch uint64, result MEVBurnResult) {
	t.mu.Lock()
	defer t.mu.Unlock()

	stats, ok := t.epochs[epoch]
	if !ok {
		stats = &EpochBurnStats{Epoch: epoch}
		t.epochs[epoch] = stats
	}

	stats.TotalBurned += result.BurnAmount
	stats.TotalBidValue += result.BidValue
	stats.BidCount++

	t.totalBurned += result.BurnAmount
	t.totalBids++

	// Update EMA of bid values.
	alpha := t.config.SmoothingFactor
	if !t.emaInitialized {
		t.ema = float64(result.BidValue)
		t.emaInitialized = true
	} else {
		t.ema = alpha*float64(result.BidValue) + (1.0-alpha)*t.ema
	}
}

// GetEpochStats returns a copy of the burn stats for the given epoch.
// Returns nil if the epoch has no data.
func (t *MEVBurnTracker) GetEpochStats(epoch uint64) *EpochBurnStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats, ok := t.epochs[epoch]
	if !ok {
		return nil
	}
	cp := *stats
	return &cp
}

// EMA returns the current exponential moving average of bid values.
func (t *MEVBurnTracker) EMA() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.ema
}

// TotalBurned returns the lifetime cumulative burn amount in Gwei.
func (t *MEVBurnTracker) TotalBurned() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalBurned
}

// TotalBids returns the lifetime total number of bids processed.
func (t *MEVBurnTracker) TotalBids() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalBids
}

// EstimateSmoothedBurn computes the exponential moving average of MEV
// from a series of recent bid values. This provides a smoothed estimate
// of expected MEV that can be used for protocol-level decisions.
//
// The EMA is computed iteratively:
//
//	ema[0] = bids[0]
//	ema[i] = alpha * bids[i] + (1 - alpha) * ema[i-1]
//
// Returns the final EMA value and the estimated burn amount.
func EstimateSmoothedBurn(recentBids []uint64, config MEVBurnConfig) (float64, uint64, error) {
	if len(recentBids) == 0 {
		return 0, 0, ErrMEVBurnNoBids
	}

	alpha := config.SmoothingFactor
	ema := float64(recentBids[0])

	for i := 1; i < len(recentBids); i++ {
		ema = alpha*float64(recentBids[i]) + (1.0-alpha)*ema
	}

	// Estimated burn from the smoothed value.
	burnEstimate := uint64(math.Floor(ema * config.BurnFraction))

	return ema, burnEstimate, nil
}

// ValidateBurnAmount verifies that a claimed burn amount is correct
// within the configured tolerance. This is used by validators to check
// that a builder/proposer computed the burn correctly.
//
// The validation passes if:
//
//	|claimed - computed| / computed <= tolerance
//
// or if computed is zero and claimed is also zero.
func ValidateBurnAmount(claimedBurn uint64, bidValue uint64, config MEVBurnConfig) error {
	result := ComputeMEVBurn(bidValue, config)
	computed := result.BurnAmount

	// Both zero is valid.
	if computed == 0 && claimedBurn == 0 {
		return nil
	}

	// If computed is zero but claimed is not, that is invalid.
	if computed == 0 && claimedBurn != 0 {
		return fmt.Errorf("%w: computed 0, claimed %d",
			ErrMEVBurnValidationFailed, claimedBurn)
	}

	// Compute relative difference.
	var diff uint64
	if claimedBurn > computed {
		diff = claimedBurn - computed
	} else {
		diff = computed - claimedBurn
	}

	relDiff := float64(diff) / float64(computed)
	if relDiff > config.Tolerance {
		return fmt.Errorf("%w: claimed %d, computed %d, diff %.4f%% > tolerance %.4f%%",
			ErrMEVBurnValidationFailed, claimedBurn, computed,
			relDiff*100, config.Tolerance*100)
	}

	return nil
}

// PruneEpochsBefore removes all epoch stats for epochs before the given
// epoch to reclaim memory.
func (t *MEVBurnTracker) PruneEpochsBefore(epoch uint64) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	pruned := 0
	for e := range t.epochs {
		if e < epoch {
			delete(t.epochs, e)
			pruned++
		}
	}
	return pruned
}

// EpochCount returns the number of tracked epochs.
func (t *MEVBurnTracker) EpochCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.epochs)
}
