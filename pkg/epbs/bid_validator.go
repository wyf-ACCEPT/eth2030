// bid_validator.go implements advanced bid validation for ePBS with
// cryptographic commitment verification, collateral checks, value range
// validation, reputation-weighted bid scoring, and deterministic winner
// selection.
//
// The BidValidator performs multi-layer validation beyond the basic
// structural checks in validation.go:
//   - Signature/commitment verification via Keccak256 hash matching
//   - Collateral sufficiency checks against minimum stake requirements
//   - Bid value range enforcement (floor and ceiling)
//   - Composite scoring that weights value, reputation, and reliability
//   - Deterministic tie-breaking using bid hash ordering
package epbs

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Bid validation errors.
var (
	ErrBidValidatorNilBid          = errors.New("bid_validator: nil bid")
	ErrBidCommitmentMismatch       = errors.New("bid_validator: commitment hash mismatch")
	ErrBidInsufficientCollateral   = errors.New("bid_validator: insufficient collateral")
	ErrBidValueTooLow              = errors.New("bid_validator: bid value below minimum")
	ErrBidValueTooHigh             = errors.New("bid_validator: bid value above maximum")
	ErrBidNoBids                   = errors.New("bid_validator: no bids to select from")
	ErrBidScorerNilConfig          = errors.New("bid_validator: nil scorer config")
)

// BidValidatorConfig configures the bid validation parameters.
type BidValidatorConfig struct {
	// MinCollateral is the minimum collateral (in Gwei) a builder must have
	// staked in order to submit bids.
	MinCollateral uint64

	// MinBidValue is the absolute floor for bid values (in Gwei).
	MinBidValue uint64

	// MaxBidValue is the absolute ceiling for bid values (in Gwei).
	// Zero means no ceiling.
	MaxBidValue uint64
}

// DefaultBidValidatorConfig returns sensible production defaults.
func DefaultBidValidatorConfig() BidValidatorConfig {
	return BidValidatorConfig{
		MinCollateral: 32_000_000_000, // 32 ETH in Gwei
		MinBidValue:   1,              // 1 Gwei minimum
		MaxBidValue:   1_000_000_000_000_000, // ~1M ETH in Gwei (sanity ceiling)
	}
}

// ValidateBidSignature verifies that the bid's commitment hash matches a
// recomputed Keccak256 hash of the bid's canonical fields. This ensures
// the bid has not been tampered with since the builder signed it.
//
// The commitment is computed as: keccak256(bidHash || builderAddr || slot || value).
func ValidateBidSignature(bid *BuilderBid, builderAddr types.Address, commitment types.Hash) error {
	if bid == nil {
		return ErrBidValidatorNilBid
	}

	computed := ComputeBidCommitment(bid, builderAddr)
	if computed != commitment {
		return fmt.Errorf("%w: expected %s, got %s",
			ErrBidCommitmentMismatch, commitment.Hex(), computed.Hex())
	}
	return nil
}

// ComputeBidCommitment computes the canonical commitment hash for a bid.
// commitment = keccak256(bidHash || builderAddr || slotBytes || valueBytes)
func ComputeBidCommitment(bid *BuilderBid, builderAddr types.Address) types.Hash {
	bidHash := bid.BidHash()

	slotBytes := new(big.Int).SetUint64(bid.Slot).Bytes()
	valueBytes := new(big.Int).SetUint64(bid.Value).Bytes()

	return crypto.Keccak256Hash(
		bidHash[:],
		builderAddr[:],
		slotBytes,
		valueBytes,
	)
}

// ValidateBidCollateral checks that the builder has sufficient collateral
// staked to cover the minimum stake requirement. The collateralGwei parameter
// represents the builder's current staked balance in Gwei.
func ValidateBidCollateral(collateralGwei uint64, config BidValidatorConfig) error {
	if collateralGwei < config.MinCollateral {
		return fmt.Errorf("%w: have %d Gwei, need %d Gwei",
			ErrBidInsufficientCollateral, collateralGwei, config.MinCollateral)
	}
	return nil
}

// ValidateBidValue ensures the bid value falls within the acceptable range
// defined by the config. The bid value must be at least MinBidValue and,
// if MaxBidValue is set, must not exceed it.
func ValidateBidValue(value uint64, config BidValidatorConfig) error {
	if value < config.MinBidValue {
		return fmt.Errorf("%w: bid %d Gwei, min %d Gwei",
			ErrBidValueTooLow, value, config.MinBidValue)
	}
	if config.MaxBidValue > 0 && value > config.MaxBidValue {
		return fmt.Errorf("%w: bid %d Gwei, max %d Gwei",
			ErrBidValueTooHigh, value, config.MaxBidValue)
	}
	return nil
}

// ScorerConfig controls how bids are scored for winner selection.
type ScorerConfig struct {
	// ValueWeight is the weight applied to the normalized bid value.
	// Range: 0.0 to 1.0. The three weights should sum to 1.0.
	ValueWeight float64

	// ReputationWeight is the weight applied to builder reputation score.
	ReputationWeight float64

	// ReliabilityWeight is the weight applied to delivery reliability.
	ReliabilityWeight float64

	// MaxBidValueForNorm is the maximum bid value used for normalization.
	// Bids at or above this value score 1.0 on the value component.
	MaxBidValueForNorm uint64
}

// DefaultScorerConfig returns balanced scoring weights.
func DefaultScorerConfig() ScorerConfig {
	return ScorerConfig{
		ValueWeight:        0.60,
		ReputationWeight:   0.25,
		ReliabilityWeight:  0.15,
		MaxBidValueForNorm: 100_000_000, // 0.1 ETH in Gwei
	}
}

// BuilderReputation holds reputation data used for bid scoring.
type BuilderReputation struct {
	// Score is the builder's reputation score (0 to 100).
	Score float64
	// TotalWins is the number of auctions the builder has won.
	TotalWins uint64
	// TotalDeliveries is the number of payloads successfully delivered.
	TotalDeliveries uint64
}

// DeliveryRate returns the fraction of won auctions where the builder
// delivered the payload, or 1.0 if the builder has no wins yet.
func (r *BuilderReputation) DeliveryRate() float64 {
	if r.TotalWins == 0 {
		return 1.0
	}
	return float64(r.TotalDeliveries) / float64(r.TotalWins)
}

// BidScorer scores bids using a composite formula that accounts for
// bid value, builder reputation, and historical reliability.
type BidScorer struct {
	config ScorerConfig
}

// NewBidScorer creates a new BidScorer with the given config.
func NewBidScorer(config ScorerConfig) (*BidScorer, error) {
	if config.MaxBidValueForNorm == 0 {
		return nil, fmt.Errorf("%w: MaxBidValueForNorm must be > 0", ErrBidScorerNilConfig)
	}
	return &BidScorer{config: config}, nil
}

// ScoredBid pairs a bid with its computed score and builder metadata.
type ScoredBid struct {
	Bid         *BuilderBid
	BuilderAddr types.Address
	Score       float64
	BidHash     types.Hash
}

// ScoreBid computes a composite score for a single bid.
// The score is in the range [0.0, 1.0] and is computed as:
//
//	score = valueWeight * normalizedValue +
//	        reputationWeight * normalizedReputation +
//	        reliabilityWeight * deliveryRate
func (s *BidScorer) ScoreBid(bid *BuilderBid, reputation *BuilderReputation) float64 {
	if bid == nil || reputation == nil {
		return 0.0
	}

	// Normalize bid value to [0, 1].
	normValue := float64(bid.Value) / float64(s.config.MaxBidValueForNorm)
	if normValue > 1.0 {
		normValue = 1.0
	}

	// Normalize reputation to [0, 1].
	normReputation := reputation.Score / 100.0
	if normReputation > 1.0 {
		normReputation = 1.0
	}
	if normReputation < 0.0 {
		normReputation = 0.0
	}

	// Delivery rate is already [0, 1].
	reliability := reputation.DeliveryRate()

	score := s.config.ValueWeight*normValue +
		s.config.ReputationWeight*normReputation +
		s.config.ReliabilityWeight*reliability

	// Clamp to [0, 1].
	if score > 1.0 {
		score = 1.0
	}
	if score < 0.0 {
		score = 0.0
	}

	// Round to 12 decimal places to avoid floating point noise.
	return math.Round(score*1e12) / 1e12
}

// SelectWinningBid selects the winner from a set of scored bids using
// deterministic rules: highest score wins; ties are broken by the
// lexicographically smallest bid hash (ensuring determinism across nodes).
//
// Returns the winning ScoredBid and its index, or an error if no bids.
func SelectWinningBid(bids []ScoredBid) (*ScoredBid, int, error) {
	if len(bids) == 0 {
		return nil, -1, ErrBidNoBids
	}

	// Sort by: score descending, then bid hash ascending (deterministic tiebreak).
	sorted := make([]int, len(bids))
	for i := range sorted {
		sorted[i] = i
	}

	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := bids[sorted[i]], bids[sorted[j]]

		// Higher score wins.
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		// Tiebreak: smaller bid hash wins (deterministic).
		return bytes.Compare(a.BidHash[:], b.BidHash[:]) < 0
	})

	winnerIdx := sorted[0]
	winner := bids[winnerIdx]
	return &winner, winnerIdx, nil
}

// FullBidValidation performs all validation steps on a bid: structural
// validation, commitment verification, collateral check, and value range.
// It returns nil if the bid passes all checks.
func FullBidValidation(
	bid *BuilderBid,
	builderAddr types.Address,
	commitment types.Hash,
	collateralGwei uint64,
	config BidValidatorConfig,
) error {
	if bid == nil {
		return ErrBidValidatorNilBid
	}

	// 1. Structural validation (reuse existing).
	signed := &SignedBuilderBid{Message: *bid}
	if err := ValidateBuilderBid(signed); err != nil {
		return fmt.Errorf("structural: %w", err)
	}

	// 2. Commitment verification.
	if err := ValidateBidSignature(bid, builderAddr, commitment); err != nil {
		return err
	}

	// 3. Collateral check.
	if err := ValidateBidCollateral(collateralGwei, config); err != nil {
		return err
	}

	// 4. Value range check.
	if err := ValidateBidValue(bid.Value, config); err != nil {
		return err
	}

	return nil
}
