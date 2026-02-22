// builder_market.go implements a builder marketplace for ePBS with Vickrey
// (second-price) auction semantics, builder reputation scoring, and
// comprehensive bid validation.
//
// The marketplace collects bids from registered builders per slot, selects
// a winner using second-price auction rules (winner pays the second-highest
// bid), and maintains long-running builder scores based on delivery history.
package epbs

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Builder market errors.
var (
	ErrMarketNilBid          = errors.New("market: nil bid")
	ErrMarketZeroValue       = errors.New("market: bid value must be > 0")
	ErrMarketZeroSlot        = errors.New("market: bid slot must be > 0")
	ErrMarketEmptyBlockHash  = errors.New("market: empty block hash")
	ErrMarketEmptyParentHash = errors.New("market: empty parent block hash")
	ErrMarketNoBids          = errors.New("market: no bids for slot")
	ErrMarketSlotFinalized   = errors.New("market: slot already finalized")
	ErrMarketUnknownBuilder  = errors.New("market: unknown builder")
	ErrMarketBidTooLow       = errors.New("market: bid below reserve price")
	ErrMarketBuilderBanned   = errors.New("market: builder is banned")
)

// MarketBid represents a builder's bid in the marketplace. It wraps the
// core BuilderBid with marketplace-specific metadata.
type MarketBid struct {
	Bid         BuilderBid    `json:"bid"`
	BuilderAddr types.Address `json:"builderAddr"`
	ReceivedAt  time.Time     `json:"receivedAt"`
}

// BuilderProfile tracks a builder's reputation in the marketplace.
type BuilderProfile struct {
	Address           types.Address `json:"address"`
	TotalBids         uint64        `json:"totalBids"`
	TotalWins         uint64        `json:"totalWins"`
	TotalDeliveries   uint64        `json:"totalDeliveries"`
	TotalMisses       uint64        `json:"totalMisses"`
	ConsecutiveMisses uint64        `json:"consecutiveMisses"`
	Score             float64       `json:"score"`
	Banned            bool          `json:"banned"`
	LastActive        time.Time     `json:"lastActive"`
}

// BuilderMarketConfig configures the builder marketplace.
type BuilderMarketConfig struct {
	// ReservePrice is the minimum bid value (in Gwei) accepted.
	ReservePrice uint64

	// MaxBidsPerSlot is the maximum bids accepted per slot.
	MaxBidsPerSlot int

	// MaxConsecutiveMisses bans a builder after this many missed deliveries.
	MaxConsecutiveMisses uint64

	// ScoreDecayFactor controls how fast builder scores decay (0 to 1).
	// A value of 0.95 means 5% decay per scoring round.
	ScoreDecayFactor float64

	// DeliveryBonus is the score bonus for successful payload delivery.
	DeliveryBonus float64

	// MissPenalty is the score penalty for missed delivery.
	MissPenalty float64
}

// DefaultBuilderMarketConfig returns sensible production defaults.
func DefaultBuilderMarketConfig() BuilderMarketConfig {
	return BuilderMarketConfig{
		ReservePrice:         1,
		MaxBidsPerSlot:       256,
		MaxConsecutiveMisses: 3,
		ScoreDecayFactor:     0.95,
		DeliveryBonus:        10.0,
		MissPenalty:          25.0,
	}
}

// slotAuction holds the bids and state for a single slot's auction.
type slotAuction struct {
	bids      []*MarketBid
	winner    *MarketBid
	price     uint64 // second-price (Vickrey)
	finalized bool
}

// BuilderMarket manages the ePBS builder bid marketplace. All public
// methods are safe for concurrent use.
type BuilderMarket struct {
	mu       sync.RWMutex
	config   BuilderMarketConfig
	auctions map[uint64]*slotAuction           // slot -> auction
	builders map[types.Address]*BuilderProfile // builder address -> profile
}

// NewBuilderMarket creates a new builder marketplace with the given config.
func NewBuilderMarket(cfg BuilderMarketConfig) *BuilderMarket {
	if cfg.MaxBidsPerSlot <= 0 {
		cfg.MaxBidsPerSlot = 256
	}
	if cfg.ScoreDecayFactor <= 0 || cfg.ScoreDecayFactor > 1 {
		cfg.ScoreDecayFactor = 0.95
	}
	if cfg.DeliveryBonus <= 0 {
		cfg.DeliveryBonus = 10.0
	}
	if cfg.MissPenalty <= 0 {
		cfg.MissPenalty = 25.0
	}
	return &BuilderMarket{
		config:   cfg,
		auctions: make(map[uint64]*slotAuction),
		builders: make(map[types.Address]*BuilderProfile),
	}
}

// RegisterBuilder adds or resets a builder profile. Returns the profile.
func (bm *BuilderMarket) RegisterBuilder(addr types.Address) *BuilderProfile {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	profile, exists := bm.builders[addr]
	if !exists {
		profile = &BuilderProfile{
			Address:    addr,
			Score:      50.0, // start with neutral score
			LastActive: time.Now(),
		}
		bm.builders[addr] = profile
	}
	return profile
}

// ValidateBid performs marketplace-level validation of a bid.
func (bm *BuilderMarket) ValidateBid(bid *MarketBid) error {
	if bid == nil {
		return ErrMarketNilBid
	}
	if bid.Bid.Value == 0 {
		return ErrMarketZeroValue
	}
	if bid.Bid.Slot == 0 {
		return ErrMarketZeroSlot
	}
	if bid.Bid.BlockHash == (types.Hash{}) {
		return ErrMarketEmptyBlockHash
	}
	if bid.Bid.ParentBlockHash == (types.Hash{}) {
		return ErrMarketEmptyParentHash
	}

	bm.mu.RLock()
	defer bm.mu.RUnlock()

	if bid.Bid.Value < bm.config.ReservePrice {
		return fmt.Errorf("%w: bid %d < reserve %d",
			ErrMarketBidTooLow, bid.Bid.Value, bm.config.ReservePrice)
	}

	// Check if builder is banned.
	if profile, ok := bm.builders[bid.BuilderAddr]; ok && profile.Banned {
		return fmt.Errorf("%w: %s", ErrMarketBuilderBanned, bid.BuilderAddr.Hex())
	}

	return nil
}

// SubmitBid submits a bid to the marketplace for a given slot. The bid is
// validated and added to the slot's auction. Returns an error if validation
// fails or the slot is already finalized.
func (bm *BuilderMarket) SubmitBid(bid *MarketBid) error {
	if err := bm.ValidateBid(bid); err != nil {
		return err
	}

	if bid.ReceivedAt.IsZero() {
		bid.ReceivedAt = time.Now()
	}

	bm.mu.Lock()
	defer bm.mu.Unlock()

	auction, ok := bm.auctions[bid.Bid.Slot]
	if !ok {
		auction = &slotAuction{
			bids: make([]*MarketBid, 0, 16),
		}
		bm.auctions[bid.Bid.Slot] = auction
	}

	if auction.finalized {
		return fmt.Errorf("%w: slot %d", ErrMarketSlotFinalized, bid.Bid.Slot)
	}

	if len(auction.bids) >= bm.config.MaxBidsPerSlot {
		// Replace lowest bid if new bid is higher.
		lowest := auction.bids[len(auction.bids)-1]
		if bid.Bid.Value <= lowest.Bid.Value {
			return nil // silently reject
		}
		auction.bids[len(auction.bids)-1] = bid
	} else {
		auction.bids = append(auction.bids, bid)
	}

	// Keep bids sorted by value descending.
	bm.sortBids(auction)

	// Update builder profile.
	if profile, exists := bm.builders[bid.BuilderAddr]; exists {
		profile.TotalBids++
		profile.LastActive = time.Now()
	}

	return nil
}

// sortBids sorts the auction bids by value descending, with timestamp
// as tiebreaker (earlier wins).
func (bm *BuilderMarket) sortBids(auction *slotAuction) {
	bids := auction.bids
	for i := 1; i < len(bids); i++ {
		for j := i; j > 0; j-- {
			if bids[j].Bid.Value > bids[j-1].Bid.Value ||
				(bids[j].Bid.Value == bids[j-1].Bid.Value &&
					bids[j].ReceivedAt.Before(bids[j-1].ReceivedAt)) {
				bids[j], bids[j-1] = bids[j-1], bids[j]
			} else {
				break
			}
		}
	}
}

// SelectWinner runs a Vickrey (second-price) auction for the given slot.
// The highest bidder wins but pays the second-highest bid value.
// Returns the winning bid and the clearing price.
func (bm *BuilderMarket) SelectWinner(slot uint64) (*MarketBid, uint64, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	auction, ok := bm.auctions[slot]
	if !ok || len(auction.bids) == 0 {
		return nil, 0, fmt.Errorf("%w: slot %d", ErrMarketNoBids, slot)
	}

	if auction.finalized {
		return nil, 0, fmt.Errorf("%w: slot %d", ErrMarketSlotFinalized, slot)
	}

	winner := auction.bids[0]

	// Second-price: winner pays the second-highest bid value.
	// If only one bid, the price is the reserve price.
	price := bm.config.ReservePrice
	if len(auction.bids) > 1 {
		price = auction.bids[1].Bid.Value
	}

	auction.winner = winner
	auction.price = price
	auction.finalized = true

	// Update builder profile.
	if profile, exists := bm.builders[winner.BuilderAddr]; exists {
		profile.TotalWins++
	}

	return winner, price, nil
}

// ScoreBuilder computes the current reputation score for a builder.
// The score is a weighted combination of delivery rate and activity.
// Returns the score and an error if the builder is unknown.
func (bm *BuilderMarket) ScoreBuilder(addr types.Address) (float64, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	profile, ok := bm.builders[addr]
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMarketUnknownBuilder, addr.Hex())
	}

	return bm.calculateScore(profile), nil
}

// calculateScore computes the builder's score. Must be called with the
// lock held.
func (bm *BuilderMarket) calculateScore(profile *BuilderProfile) float64 {
	if profile.TotalWins == 0 {
		return profile.Score
	}

	// Delivery rate component (0 to 100).
	deliveryRate := float64(profile.TotalDeliveries) / float64(profile.TotalWins)
	deliveryScore := deliveryRate * 100.0

	// Consecutive miss penalty: exponential penalty for streaks.
	missPenalty := 0.0
	if profile.ConsecutiveMisses > 0 {
		missPenalty = bm.config.MissPenalty * math.Pow(1.5, float64(profile.ConsecutiveMisses-1))
	}

	// Apply decay to current score, then add delivery component.
	score := profile.Score*bm.config.ScoreDecayFactor +
		(1-bm.config.ScoreDecayFactor)*deliveryScore -
		missPenalty

	// Clamp to [0, 100].
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

// RecordDelivery records that a builder successfully delivered a payload
// for a won slot. Updates the builder's score and resets consecutive misses.
func (bm *BuilderMarket) RecordDelivery(addr types.Address) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	profile, ok := bm.builders[addr]
	if !ok {
		return fmt.Errorf("%w: %s", ErrMarketUnknownBuilder, addr.Hex())
	}

	profile.TotalDeliveries++
	profile.ConsecutiveMisses = 0
	profile.Score = bm.calculateScore(profile)
	return nil
}

// RecordMiss records that a builder failed to deliver a payload for a
// won slot. Updates score and may ban the builder.
func (bm *BuilderMarket) RecordMiss(addr types.Address) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	profile, ok := bm.builders[addr]
	if !ok {
		return fmt.Errorf("%w: %s", ErrMarketUnknownBuilder, addr.Hex())
	}

	profile.TotalMisses++
	profile.ConsecutiveMisses++
	profile.Score = bm.calculateScore(profile)

	if bm.config.MaxConsecutiveMisses > 0 &&
		profile.ConsecutiveMisses >= bm.config.MaxConsecutiveMisses {
		profile.Banned = true
	}
	return nil
}

// GetBuilderProfile returns a copy of the builder profile.
func (bm *BuilderMarket) GetBuilderProfile(addr types.Address) (*BuilderProfile, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	profile, ok := bm.builders[addr]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMarketUnknownBuilder, addr.Hex())
	}
	cp := *profile
	return &cp, nil
}

// GetBids returns all bids for a given slot.
func (bm *BuilderMarket) GetBids(slot uint64) []*MarketBid {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	auction, ok := bm.auctions[slot]
	if !ok {
		return nil
	}
	result := make([]*MarketBid, len(auction.bids))
	copy(result, auction.bids)
	return result
}

// BidCount returns the number of bids for a given slot.
func (bm *BuilderMarket) BidCount(slot uint64) int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	auction, ok := bm.auctions[slot]
	if !ok {
		return 0
	}
	return len(auction.bids)
}

// BuilderCount returns the number of registered builders.
func (bm *BuilderMarket) BuilderCount() int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return len(bm.builders)
}

// UnbanBuilder removes the ban from a builder.
func (bm *BuilderMarket) UnbanBuilder(addr types.Address) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	profile, ok := bm.builders[addr]
	if !ok {
		return fmt.Errorf("%w: %s", ErrMarketUnknownBuilder, addr.Hex())
	}
	profile.Banned = false
	profile.ConsecutiveMisses = 0
	return nil
}

// PruneBefore removes auction data for all slots before the given slot.
func (bm *BuilderMarket) PruneBefore(slot uint64) int {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	pruned := 0
	for s := range bm.auctions {
		if s < slot {
			delete(bm.auctions, s)
			pruned++
		}
	}
	return pruned
}
