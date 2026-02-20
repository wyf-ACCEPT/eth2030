package consensus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// AuctionedProposerSelection implements APS for block proposer selection
// using sealed-bid Vickrey auctions with VRF-based fallback.

// Auction errors.
var (
	ErrAuctionSlotPast       = errors.New("proposer-auction: slot already past")
	ErrAuctionAlreadyOpen    = errors.New("proposer-auction: auction already open for slot")
	ErrAuctionNotOpen        = errors.New("proposer-auction: no open auction for slot")
	ErrAuctionAlreadyClosed  = errors.New("proposer-auction: auction already closed")
	ErrAuctionDuplicateBid   = errors.New("proposer-auction: duplicate bid from bidder")
	ErrAuctionZeroBid        = errors.New("proposer-auction: bid amount must be > 0")
	ErrAuctionNoBids         = errors.New("proposer-auction: no bids submitted")
	ErrAuctionInvalidCommit  = errors.New("proposer-auction: invalid block root commitment")
)

// AuctionBid represents a sealed bid in a proposer auction.
type AuctionBid struct {
	Bidder          uint64     // validator index of the bidder
	Slot            uint64     // target slot
	Amount          uint64     // bid amount in Gwei
	BlockCommitment types.Hash // commitment to the block root
	Signature       [96]byte   // BLS signature over the bid
}

// AuctionClearing holds the clearing result for a Vickrey auction.
type AuctionClearing struct {
	Slot           uint64
	Winner         uint64     // winning bidder
	WinningBid     uint64     // highest bid
	ClearingPrice  uint64     // second-highest bid (Vickrey price)
	BlockCommitment types.Hash
	BidCount       int
}

// ProposerAuction manages a sealed-bid auction for a single slot.
type ProposerAuction struct {
	Slot    uint64
	Open    bool
	Closed  bool
	Bids    []*AuctionBid
	bidders map[uint64]bool // track unique bidders
}

// ProposerScheduleEntry is one entry in the deterministic proposer schedule.
type ProposerScheduleEntry struct {
	Slot           uint64
	ProposerIndex  uint64
	IsAuctioned    bool   // true if assigned via auction
	ClearingPrice  uint64 // 0 if via fallback
}

// CommitteeRotationEntry records committee composition for an epoch.
type CommitteeRotationEntry struct {
	Epoch      uint64
	Committee  []uint64 // validator indices eligible for proposing
	Seed       types.Hash
}

// AuctionedProposerConfig configures the auctioned proposer system.
type AuctionedProposerConfig struct {
	// MinBid is the minimum bid in Gwei.
	MinBid uint64
	// MaxAuctionSlots is how far ahead auctions can be opened.
	MaxAuctionSlots uint64
	// FallbackEnabled determines if VRF fallback is used when no bids.
	FallbackEnabled bool
}

// DefaultAuctionedProposerConfig returns production defaults.
func DefaultAuctionedProposerConfig() AuctionedProposerConfig {
	return AuctionedProposerConfig{
		MinBid:          1 * GweiPerETH,
		MaxAuctionSlots: 32,
		FallbackEnabled: true,
	}
}

// AuctionedProposerSelection manages proposer auctions across slots.
// Thread-safe.
type AuctionedProposerSelection struct {
	mu     sync.RWMutex
	config AuctionedProposerConfig

	// Open auctions keyed by slot.
	auctions map[uint64]*ProposerAuction

	// Completed clearings keyed by slot.
	clearings map[uint64]*AuctionClearing

	// Deterministic schedule for fallback.
	schedule map[uint64]*ProposerScheduleEntry

	// Committee rotations by epoch.
	rotations map[uint64]*CommitteeRotationEntry
}

// NewAuctionedProposerSelection creates a new APS manager.
func NewAuctionedProposerSelection(config AuctionedProposerConfig) *AuctionedProposerSelection {
	return &AuctionedProposerSelection{
		config:    config,
		auctions:  make(map[uint64]*ProposerAuction),
		clearings: make(map[uint64]*AuctionClearing),
		schedule:  make(map[uint64]*ProposerScheduleEntry),
		rotations: make(map[uint64]*CommitteeRotationEntry),
	}
}

// OpenAuction opens a sealed-bid auction for the given slot.
func (aps *AuctionedProposerSelection) OpenAuction(slot uint64) error {
	aps.mu.Lock()
	defer aps.mu.Unlock()

	if _, ok := aps.auctions[slot]; ok {
		return fmt.Errorf("%w: slot %d", ErrAuctionAlreadyOpen, slot)
	}
	if _, ok := aps.clearings[slot]; ok {
		return fmt.Errorf("%w: slot %d", ErrAuctionAlreadyClosed, slot)
	}

	aps.auctions[slot] = &ProposerAuction{
		Slot:    slot,
		Open:    true,
		bidders: make(map[uint64]bool),
	}
	return nil
}

// SubmitBid adds a sealed bid to an open auction.
func (aps *AuctionedProposerSelection) SubmitBid(bid *AuctionBid) error {
	if bid.Amount == 0 {
		return ErrAuctionZeroBid
	}

	aps.mu.Lock()
	defer aps.mu.Unlock()

	auction, ok := aps.auctions[bid.Slot]
	if !ok {
		return fmt.Errorf("%w: slot %d", ErrAuctionNotOpen, bid.Slot)
	}
	if !auction.Open {
		return fmt.Errorf("%w: slot %d", ErrAuctionAlreadyClosed, bid.Slot)
	}
	if auction.bidders[bid.Bidder] {
		return fmt.Errorf("%w: bidder %d slot %d", ErrAuctionDuplicateBid, bid.Bidder, bid.Slot)
	}

	bidCopy := *bid
	auction.Bids = append(auction.Bids, &bidCopy)
	auction.bidders[bid.Bidder] = true
	return nil
}

// CloseAuction closes the auction and computes Vickrey clearing.
// The winner pays the second-highest bid price.
func (aps *AuctionedProposerSelection) CloseAuction(slot uint64) (*AuctionClearing, error) {
	aps.mu.Lock()
	defer aps.mu.Unlock()

	auction, ok := aps.auctions[slot]
	if !ok {
		return nil, fmt.Errorf("%w: slot %d", ErrAuctionNotOpen, slot)
	}
	if auction.Closed {
		if clearing, ok := aps.clearings[slot]; ok {
			return clearing, nil
		}
		return nil, fmt.Errorf("%w: slot %d", ErrAuctionAlreadyClosed, slot)
	}

	if len(auction.Bids) == 0 {
		auction.Open = false
		auction.Closed = true
		return nil, ErrAuctionNoBids
	}

	// Sort bids by amount descending.
	bids := make([]*AuctionBid, len(auction.Bids))
	copy(bids, auction.Bids)
	sort.Slice(bids, func(i, j int) bool {
		return bids[i].Amount > bids[j].Amount
	})

	winner := bids[0]
	clearingPrice := winner.Amount // default: pay own bid if only 1 bidder
	if len(bids) > 1 {
		clearingPrice = bids[1].Amount // Vickrey: pay second price
	}

	clearing := &AuctionClearing{
		Slot:            slot,
		Winner:          winner.Bidder,
		WinningBid:      winner.Amount,
		ClearingPrice:   clearingPrice,
		BlockCommitment: winner.BlockCommitment,
		BidCount:        len(bids),
	}

	auction.Open = false
	auction.Closed = true
	aps.clearings[slot] = clearing

	// Record in schedule.
	aps.schedule[slot] = &ProposerScheduleEntry{
		Slot:          slot,
		ProposerIndex: winner.Bidder,
		IsAuctioned:   true,
		ClearingPrice: clearingPrice,
	}

	return clearing, nil
}

// FallbackProposer computes a deterministic proposer using VRF-based
// selection when no auction bids were placed.
func (aps *AuctionedProposerSelection) FallbackProposer(
	slot uint64,
	validators []uint64,
	seed types.Hash,
) uint64 {
	if len(validators) == 0 {
		return 0
	}

	// Derive deterministic index from seed + slot.
	var buf [40]byte
	copy(buf[:32], seed[:])
	binary.BigEndian.PutUint64(buf[32:], slot)
	h := crypto.Keccak256(buf[:])
	idx := binary.BigEndian.Uint64(h[:8]) % uint64(len(validators))

	proposer := validators[idx]

	aps.mu.Lock()
	aps.schedule[slot] = &ProposerScheduleEntry{
		Slot:          slot,
		ProposerIndex: proposer,
		IsAuctioned:   false,
	}
	aps.mu.Unlock()

	return proposer
}

// GetScheduleEntry returns the proposer schedule entry for a slot.
func (aps *AuctionedProposerSelection) GetScheduleEntry(slot uint64) (*ProposerScheduleEntry, bool) {
	aps.mu.RLock()
	defer aps.mu.RUnlock()
	entry, ok := aps.schedule[slot]
	if !ok {
		return nil, false
	}
	cp := *entry
	return &cp, true
}

// GetClearing returns the auction clearing for a slot.
func (aps *AuctionedProposerSelection) GetClearing(slot uint64) (*AuctionClearing, bool) {
	aps.mu.RLock()
	defer aps.mu.RUnlock()
	clearing, ok := aps.clearings[slot]
	if !ok {
		return nil, false
	}
	cp := *clearing
	return &cp, true
}

// BidCount returns the number of bids for an open or closed auction.
func (aps *AuctionedProposerSelection) BidCount(slot uint64) int {
	aps.mu.RLock()
	defer aps.mu.RUnlock()
	if auction, ok := aps.auctions[slot]; ok {
		return len(auction.Bids)
	}
	return 0
}

// RotateCommittee computes and stores the proposer committee for an epoch.
func (aps *AuctionedProposerSelection) RotateCommittee(
	epoch uint64,
	validators []uint64,
	seed types.Hash,
) *CommitteeRotationEntry {
	if len(validators) == 0 {
		return &CommitteeRotationEntry{Epoch: epoch, Seed: seed}
	}

	// Shuffle validators using seed + epoch for deterministic rotation.
	shuffled := make([]uint64, len(validators))
	copy(shuffled, validators)

	var buf [40]byte
	copy(buf[:32], seed[:])
	binary.BigEndian.PutUint64(buf[32:], epoch)

	for i := len(shuffled) - 1; i > 0; i-- {
		binary.BigEndian.PutUint64(buf[32:], epoch+uint64(i))
		h := crypto.Keccak256(buf[:])
		j := binary.BigEndian.Uint64(h[:8]) % uint64(i+1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	entry := &CommitteeRotationEntry{
		Epoch:     epoch,
		Committee: shuffled,
		Seed:      seed,
	}

	aps.mu.Lock()
	aps.rotations[epoch] = entry
	aps.mu.Unlock()

	return entry
}

// GetCommittee returns the proposer committee for an epoch.
func (aps *AuctionedProposerSelection) GetCommittee(epoch uint64) ([]uint64, bool) {
	aps.mu.RLock()
	defer aps.mu.RUnlock()
	entry, ok := aps.rotations[epoch]
	if !ok {
		return nil, false
	}
	result := make([]uint64, len(entry.Committee))
	copy(result, entry.Committee)
	return result, true
}
