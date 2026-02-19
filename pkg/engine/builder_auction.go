// Package engine implements the distributed block builder auction system.
//
// BuilderAuction manages a second-price sealed-bid auction for block construction
// rights in the Ethereum 2028 roadmap. Builders register with stake, submit bids
// for specific slots, and the auction selects the highest bidder while charging
// the second-highest price (Vickrey auction). Misbehaving builders can be slashed.
package engine

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Auction-specific errors.
var (
	ErrAuctionNilBid          = errors.New("auction: nil bid")
	ErrAuctionBidTooLow       = errors.New("auction: bid below minimum")
	ErrAuctionBidTooHigh      = errors.New("auction: bid exceeds maximum")
	ErrAuctionBidZeroSlot     = errors.New("auction: bid slot must be > 0")
	ErrAuctionBidEmptyBuilder = errors.New("auction: bid builder ID must not be zero")
	ErrAuctionBidZeroGas      = errors.New("auction: bid gas limit must be > 0")
	ErrAuctionBidNoPayload    = errors.New("auction: bid payload must not be empty")
	ErrAuctionBidNoSignature  = errors.New("auction: bid signature must not be empty")
	ErrAuctionBuilderNotReg   = errors.New("auction: builder not registered")
	ErrAuctionBuilderSlashed  = errors.New("auction: builder is slashed")
	ErrAuctionBuilderExists   = errors.New("auction: builder already registered")
	ErrAuctionInsufficientStk = errors.New("auction: insufficient stake")
	ErrAuctionNoBids          = errors.New("auction: no bids for slot")
	ErrAuctionSlashZeroID     = errors.New("auction: cannot slash zero builder ID")
)

// AuctionConfig configures the distributed block builder auction.
type AuctionConfig struct {
	MinBid          uint64 // minimum accepted bid value
	MaxBid          uint64 // maximum accepted bid value (0 = unlimited)
	AuctionDeadline uint64 // slot deadline for bid submission (unused placeholder)
	MinStake        uint64 // minimum stake to register as a builder
}

// DefaultAuctionConfig returns sensible default auction configuration.
func DefaultAuctionConfig() AuctionConfig {
	return AuctionConfig{
		MinBid:          1,
		MaxBid:          0, // no upper limit
		AuctionDeadline: 0,
		MinStake:        100,
	}
}

// AuctionBid represents a sealed bid from a builder for a specific slot.
type AuctionBid struct {
	BuilderID types.Hash // unique builder identifier
	Slot      uint64     // slot number for which the bid is made
	Value     uint64     // bid value in Gwei
	GasLimit  uint64     // proposed block gas limit
	Payload   []byte     // opaque payload commitment
	Signature []byte     // cryptographic signature
}

// Hash returns a deterministic hash of the bid for deduplication.
func (b *AuctionBid) Hash() types.Hash {
	var data []byte
	data = append(data, b.BuilderID[:]...)
	data = append(data, byte(b.Slot>>56), byte(b.Slot>>48), byte(b.Slot>>40), byte(b.Slot>>32))
	data = append(data, byte(b.Slot>>24), byte(b.Slot>>16), byte(b.Slot>>8), byte(b.Slot))
	data = append(data, byte(b.Value>>56), byte(b.Value>>48), byte(b.Value>>40), byte(b.Value>>32))
	data = append(data, byte(b.Value>>24), byte(b.Value>>16), byte(b.Value>>8), byte(b.Value))
	data = append(data, b.Payload...)
	return crypto.Keccak256Hash(data)
}

// AuctionResult contains the outcome of a slot auction.
type AuctionResult struct {
	Slot         uint64     // slot that was auctioned
	WinnerID     types.Hash // builder ID of the winner
	WinningValue uint64     // the winner's bid value
	TotalBids    int        // total number of bids in the auction
	SecondPrice  uint64     // second-highest bid (Vickrey price)
}

// registeredBuilder tracks a builder's registration state.
type registeredBuilder struct {
	ID      types.Hash
	Stake   uint64
	Slashed bool
	Reason  string // slash reason, if any
}

// BuilderAuction manages the distributed block builder auction protocol.
// All methods are safe for concurrent use.
type BuilderAuction struct {
	mu       sync.RWMutex
	config   AuctionConfig
	builders map[types.Hash]*registeredBuilder  // builder ID -> registration
	bids     map[uint64][]*AuctionBid           // slot -> ordered bids
}

// NewBuilderAuction creates a new builder auction with the given configuration.
func NewBuilderAuction(config AuctionConfig) *BuilderAuction {
	return &BuilderAuction{
		config:   config,
		builders: make(map[types.Hash]*registeredBuilder),
		bids:     make(map[uint64][]*AuctionBid),
	}
}

// RegisterBuilder registers a new builder with the given ID and stake.
// The stake must meet the minimum configured in AuctionConfig.
func (ba *BuilderAuction) RegisterBuilder(builderID types.Hash, stake uint64) error {
	if builderID.IsZero() {
		return ErrAuctionBidEmptyBuilder
	}

	ba.mu.Lock()
	defer ba.mu.Unlock()

	if _, exists := ba.builders[builderID]; exists {
		return ErrAuctionBuilderExists
	}
	if stake < ba.config.MinStake {
		return fmt.Errorf("%w: need %d, got %d", ErrAuctionInsufficientStk, ba.config.MinStake, stake)
	}

	ba.builders[builderID] = &registeredBuilder{
		ID:    builderID,
		Stake: stake,
	}
	return nil
}

// SlashBuilder marks a builder as slashed for the given reason.
// Slashed builders cannot submit bids.
func (ba *BuilderAuction) SlashBuilder(builderID types.Hash, reason string) error {
	if builderID.IsZero() {
		return ErrAuctionSlashZeroID
	}

	ba.mu.Lock()
	defer ba.mu.Unlock()

	b, exists := ba.builders[builderID]
	if !exists {
		return ErrAuctionBuilderNotReg
	}
	b.Slashed = true
	b.Reason = reason
	return nil
}

// ValidateBid checks that a bid meets all structural and policy requirements.
// It does not acquire the auction lock and can be used externally.
func (ba *BuilderAuction) ValidateBid(bid *AuctionBid) error {
	if bid == nil {
		return ErrAuctionNilBid
	}
	if bid.BuilderID.IsZero() {
		return ErrAuctionBidEmptyBuilder
	}
	if bid.Slot == 0 {
		return ErrAuctionBidZeroSlot
	}
	if bid.Value < ba.config.MinBid {
		return fmt.Errorf("%w: need >= %d, got %d", ErrAuctionBidTooLow, ba.config.MinBid, bid.Value)
	}
	if ba.config.MaxBid > 0 && bid.Value > ba.config.MaxBid {
		return fmt.Errorf("%w: max %d, got %d", ErrAuctionBidTooHigh, ba.config.MaxBid, bid.Value)
	}
	if bid.GasLimit == 0 {
		return ErrAuctionBidZeroGas
	}
	if len(bid.Payload) == 0 {
		return ErrAuctionBidNoPayload
	}
	if len(bid.Signature) == 0 {
		return ErrAuctionBidNoSignature
	}
	return nil
}

// SubmitBid validates and stores a bid for the specified slot.
// The builder must be registered and not slashed.
func (ba *BuilderAuction) SubmitBid(bid *AuctionBid) error {
	if err := ba.ValidateBid(bid); err != nil {
		return err
	}

	ba.mu.Lock()
	defer ba.mu.Unlock()

	b, exists := ba.builders[bid.BuilderID]
	if !exists {
		return ErrAuctionBuilderNotReg
	}
	if b.Slashed {
		return ErrAuctionBuilderSlashed
	}

	ba.bids[bid.Slot] = append(ba.bids[bid.Slot], bid)
	return nil
}

// GetWinningBid returns the highest-value bid for the given slot.
func (ba *BuilderAuction) GetWinningBid(slot uint64) (*AuctionBid, error) {
	ba.mu.RLock()
	defer ba.mu.RUnlock()

	bids := ba.bids[slot]
	if len(bids) == 0 {
		return nil, ErrAuctionNoBids
	}

	var best *AuctionBid
	for _, bid := range bids {
		if best == nil || bid.Value > best.Value {
			best = bid
		}
	}
	return best, nil
}

// RunAuction executes the Vickrey (second-price) auction for a slot.
// The winner pays the second-highest bid price.
func (ba *BuilderAuction) RunAuction(slot uint64) (*AuctionResult, error) {
	ba.mu.RLock()
	defer ba.mu.RUnlock()

	bids := ba.bids[slot]
	if len(bids) == 0 {
		return nil, ErrAuctionNoBids
	}

	// Sort descending by value.
	sorted := make([]*AuctionBid, len(bids))
	copy(sorted, bids)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})

	winner := sorted[0]
	secondPrice := winner.Value // if only one bid, pay own price
	if len(sorted) > 1 {
		secondPrice = sorted[1].Value
	}

	return &AuctionResult{
		Slot:         slot,
		WinnerID:     winner.BuilderID,
		WinningValue: winner.Value,
		TotalBids:    len(bids),
		SecondPrice:  secondPrice,
	}, nil
}

// GetBidHistory returns all bids submitted for a given slot.
func (ba *BuilderAuction) GetBidHistory(slot uint64) []*AuctionBid {
	ba.mu.RLock()
	defer ba.mu.RUnlock()

	bids := ba.bids[slot]
	result := make([]*AuctionBid, len(bids))
	copy(result, bids)
	return result
}
