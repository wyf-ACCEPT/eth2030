// ePBS auction engine with full bid lifecycle, winner selection, finalization,
// history tracking, and slashing condition monitoring.
//
// The AuctionEngine manages individual AuctionRounds through four states:
// Open -> BiddingClosed -> WinnerSelected -> Finalized. It supports
// highest-value winner selection with earliest-timestamp tiebreaking,
// history retention for auditing, and violation tracking for builders
// who win but fail to deliver payloads.
package epbs

import (
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Auction engine errors.
var (
	ErrAuctionNoRound         = errors.New("auction: no active round")
	ErrAuctionAlreadyOpen     = errors.New("auction: round already open")
	ErrAuctionNotOpen         = errors.New("auction: round is not open for bidding")
	ErrAuctionNotClosed       = errors.New("auction: bidding has not been closed")
	ErrAuctionAlreadyFinalized = errors.New("auction: round already finalized")
	ErrAuctionNoWinner        = errors.New("auction: no winner selected")
	ErrAuctionNoBids          = errors.New("auction: no bids submitted")
	ErrAuctionNilBid          = errors.New("auction: nil bid")
	ErrAuctionZeroValue       = errors.New("auction: bid value must be > 0")
	ErrAuctionInvalidSlot     = errors.New("auction: invalid slot")
	ErrAuctionSlotMismatch    = errors.New("auction: bid slot does not match round")
	ErrAuctionEmptyPubkey     = errors.New("auction: builder pubkey is empty")
	ErrAuctionEmptyPayload    = errors.New("auction: payload hash is empty")
	ErrAuctionWinnerNotSet    = errors.New("auction: winner not yet selected")
)

// AuctionState represents the lifecycle state of an auction round.
type AuctionState int

const (
	AuctionOpen           AuctionState = iota // accepting bids
	AuctionBiddingClosed                      // no more bids, ready for selection
	AuctionWinnerSelected                     // winner chosen, awaiting finalization
	AuctionFinalized                          // round complete
)

// String returns a human-readable state name.
func (s AuctionState) String() string {
	switch s {
	case AuctionOpen:
		return "Open"
	case AuctionBiddingClosed:
		return "BiddingClosed"
	case AuctionWinnerSelected:
		return "WinnerSelected"
	case AuctionFinalized:
		return "Finalized"
	default:
		return "Unknown"
	}
}

// AuctionBid represents a builder's bid in an auction round.
type AuctionBid struct {
	BuilderPubkey [48]byte   // BLS public key of the builder
	Slot          uint64     // target slot
	Value         *big.Int   // bid value in wei
	PayloadHash   types.Hash // commitment to the payload
	Timestamp     time.Time  // when the bid was submitted
	Signature     [96]byte   // BLS signature placeholder
}

// AuctionRound tracks the state of a single slot's auction.
type AuctionRound struct {
	OpeningSlot  uint64
	ClosingSlot  uint64 // slot when bidding closes (may equal opening)
	State        AuctionState
	Bids         []*AuctionBid
	WinningBid   *AuctionBid
	OpenedAt     time.Time
	ClosedAt     time.Time
	FinalizedAt  time.Time
}

// AuctionResult stores the outcome of a finalized auction.
type AuctionResult struct {
	Slot          uint64
	WinningBid    *AuctionBid
	TotalBids     int
	FinalizedAt   time.Time
	PayloadDelivered bool
}

// SlashingViolation records a builder who won but failed to deliver.
type SlashingViolation struct {
	BuilderPubkey [48]byte
	Slot          uint64
	BidValue      *big.Int
	RecordedAt    time.Time
}

// AuctionEngineConfig configures the auction engine.
type AuctionEngineConfig struct {
	MaxBidsPerRound int // max bids accepted per round
	MaxHistory      int // max past results to retain
}

// DefaultAuctionEngineConfig returns sensible defaults.
func DefaultAuctionEngineConfig() *AuctionEngineConfig {
	return &AuctionEngineConfig{
		MaxBidsPerRound: 256,
		MaxHistory:      128,
	}
}

// AuctionEngine manages the ePBS bid lifecycle. Thread-safe.
type AuctionEngine struct {
	mu         sync.RWMutex
	config     *AuctionEngineConfig
	round      *AuctionRound        // current active round
	history    []*AuctionResult     // past auction results
	violations []*SlashingViolation // recorded violations
}

// NewAuctionEngine creates a new auction engine.
func NewAuctionEngine(cfg *AuctionEngineConfig) *AuctionEngine {
	if cfg == nil {
		cfg = DefaultAuctionEngineConfig()
	}
	return &AuctionEngine{
		config: cfg,
	}
}

// OpenAuction starts a new auction round for the given slot.
func (ae *AuctionEngine) OpenAuction(slot uint64) error {
	if slot == 0 {
		return ErrAuctionInvalidSlot
	}

	ae.mu.Lock()
	defer ae.mu.Unlock()

	if ae.round != nil && ae.round.State != AuctionFinalized {
		return ErrAuctionAlreadyOpen
	}

	ae.round = &AuctionRound{
		OpeningSlot: slot,
		ClosingSlot: slot,
		State:       AuctionOpen,
		OpenedAt:    time.Now(),
	}
	return nil
}

// SubmitBid adds a bid to the current open auction round.
func (ae *AuctionEngine) SubmitBid(bid *AuctionBid) error {
	if bid == nil {
		return ErrAuctionNilBid
	}
	if bid.Value == nil || bid.Value.Sign() <= 0 {
		return ErrAuctionZeroValue
	}
	if bid.Slot == 0 {
		return ErrAuctionInvalidSlot
	}
	emptyPk := [48]byte{}
	if bid.BuilderPubkey == emptyPk {
		return ErrAuctionEmptyPubkey
	}
	emptyHash := types.Hash{}
	if bid.PayloadHash == emptyHash {
		return ErrAuctionEmptyPayload
	}

	ae.mu.Lock()
	defer ae.mu.Unlock()

	if ae.round == nil {
		return ErrAuctionNoRound
	}
	if ae.round.State != AuctionOpen {
		return ErrAuctionNotOpen
	}
	if bid.Slot != ae.round.OpeningSlot {
		return ErrAuctionSlotMismatch
	}
	if len(ae.round.Bids) >= ae.config.MaxBidsPerRound {
		return ErrAuctionNoBids // reuse; the round is full
	}

	if bid.Timestamp.IsZero() {
		bid.Timestamp = time.Now()
	}
	ae.round.Bids = append(ae.round.Bids, bid)
	return nil
}

// CloseBidding transitions the auction from Open to BiddingClosed.
func (ae *AuctionEngine) CloseBidding() error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	if ae.round == nil {
		return ErrAuctionNoRound
	}
	if ae.round.State != AuctionOpen {
		return ErrAuctionNotOpen
	}

	ae.round.State = AuctionBiddingClosed
	ae.round.ClosedAt = time.Now()
	return nil
}

// SelectWinner picks the highest-value bid. Ties broken by earliest timestamp.
func (ae *AuctionEngine) SelectWinner() (*AuctionBid, error) {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	if ae.round == nil {
		return nil, ErrAuctionNoRound
	}
	if ae.round.State != AuctionBiddingClosed {
		return nil, ErrAuctionNotClosed
	}
	if len(ae.round.Bids) == 0 {
		return nil, ErrAuctionNoBids
	}

	var winner *AuctionBid
	for _, b := range ae.round.Bids {
		if winner == nil {
			winner = b
			continue
		}
		cmp := b.Value.Cmp(winner.Value)
		if cmp > 0 {
			winner = b
		} else if cmp == 0 && b.Timestamp.Before(winner.Timestamp) {
			// Tiebreak: earliest timestamp wins.
			winner = b
		}
	}

	ae.round.WinningBid = winner
	ae.round.State = AuctionWinnerSelected
	return winner, nil
}

// FinalizeAuction finalizes the round and archives the result.
func (ae *AuctionEngine) FinalizeAuction() error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	if ae.round == nil {
		return ErrAuctionNoRound
	}
	if ae.round.State == AuctionFinalized {
		return ErrAuctionAlreadyFinalized
	}
	if ae.round.State != AuctionWinnerSelected {
		return ErrAuctionWinnerNotSet
	}

	now := time.Now()
	ae.round.State = AuctionFinalized
	ae.round.FinalizedAt = now

	result := &AuctionResult{
		Slot:       ae.round.OpeningSlot,
		WinningBid: ae.round.WinningBid,
		TotalBids:  len(ae.round.Bids),
		FinalizedAt: now,
		PayloadDelivered: true, // assumed until violation recorded
	}
	ae.history = append(ae.history, result)
	if len(ae.history) > ae.config.MaxHistory {
		ae.history = ae.history[len(ae.history)-ae.config.MaxHistory:]
	}

	return nil
}

// GetWinner returns the winning bid of the current round, if selected.
func (ae *AuctionEngine) GetWinner() (*AuctionBid, error) {
	ae.mu.RLock()
	defer ae.mu.RUnlock()

	if ae.round == nil {
		return nil, ErrAuctionNoRound
	}
	if ae.round.WinningBid == nil {
		return nil, ErrAuctionNoWinner
	}
	return ae.round.WinningBid, nil
}

// GetState returns the current auction round state.
func (ae *AuctionEngine) GetState() AuctionState {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	if ae.round == nil {
		return AuctionFinalized // no round is effectively "done"
	}
	return ae.round.State
}

// BidCount returns the number of bids in the current round.
func (ae *AuctionEngine) BidCount() int {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	if ae.round == nil {
		return 0
	}
	return len(ae.round.Bids)
}

// CurrentSlot returns the slot of the current round, or 0 if none.
func (ae *AuctionEngine) CurrentSlot() uint64 {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	if ae.round == nil {
		return 0
	}
	return ae.round.OpeningSlot
}

// RecordViolation records a slashing condition when a winning builder
// fails to deliver the payload for a slot.
func (ae *AuctionEngine) RecordViolation(builderPubkey [48]byte, slot uint64, bidValue *big.Int) {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	v := &SlashingViolation{
		BuilderPubkey: builderPubkey,
		Slot:          slot,
		BidValue:      new(big.Int).Set(bidValue),
		RecordedAt:    time.Now(),
	}
	ae.violations = append(ae.violations, v)

	// Mark the corresponding history entry if it exists.
	for _, r := range ae.history {
		if r.Slot == slot {
			r.PayloadDelivered = false
		}
	}
}

// Violations returns all recorded slashing violations.
func (ae *AuctionEngine) Violations() []*SlashingViolation {
	ae.mu.RLock()
	defer ae.mu.RUnlock()

	result := make([]*SlashingViolation, len(ae.violations))
	copy(result, ae.violations)
	return result
}

// ViolationCount returns the number of recorded violations.
func (ae *AuctionEngine) ViolationCount() int {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	return len(ae.violations)
}

// History returns the last n auction results.
func (ae *AuctionEngine) History(n int) []*AuctionResult {
	ae.mu.RLock()
	defer ae.mu.RUnlock()

	if n <= 0 || len(ae.history) == 0 {
		return nil
	}
	if n > len(ae.history) {
		n = len(ae.history)
	}
	result := make([]*AuctionResult, n)
	copy(result, ae.history[len(ae.history)-n:])
	return result
}

// HistoryCount returns the number of archived auction results.
func (ae *AuctionEngine) HistoryCount() int {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	return len(ae.history)
}
