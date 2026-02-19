package epbs

import (
	"errors"
	"fmt"
	"sync"
)

// Auction errors.
var (
	ErrNoBidsForSlot    = errors.New("no bids available for slot")
	ErrBidValidation    = errors.New("bid failed validation")
)

// PayloadAuction tracks builder bids per slot and selects winners.
// It is the EL-side mechanism for ePBS bid management.
type PayloadAuction struct {
	mu   sync.RWMutex
	bids map[uint64][]*SignedBuilderBid // slot -> bids sorted by value desc
}

// NewPayloadAuction creates a new payload auction tracker.
func NewPayloadAuction() *PayloadAuction {
	return &PayloadAuction{
		bids: make(map[uint64][]*SignedBuilderBid),
	}
}

// SubmitBid adds a validated bid to the auction for its slot.
// Bids are stored sorted by value descending (highest first).
func (a *PayloadAuction) SubmitBid(signed *SignedBuilderBid) error {
	if err := ValidateBuilderBid(signed); err != nil {
		return fmt.Errorf("%w: %v", ErrBidValidation, err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	slot := signed.Message.Slot
	bids := a.bids[slot]

	// Insert sorted by value descending.
	inserted := false
	for i, existing := range bids {
		if signed.Message.Value > existing.Message.Value {
			// Insert before this element.
			bids = append(bids[:i+1], bids[i:]...)
			bids[i] = signed
			inserted = true
			break
		}
	}
	if !inserted {
		bids = append(bids, signed)
	}
	a.bids[slot] = bids

	return nil
}

// GetWinningBid returns the highest-value bid for the given slot.
func (a *PayloadAuction) GetWinningBid(slot uint64) (*SignedBuilderBid, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	bids, ok := a.bids[slot]
	if !ok || len(bids) == 0 {
		return nil, ErrNoBidsForSlot
	}
	return bids[0], nil
}

// GetBidsForSlot returns all bids for a slot, ordered by value descending.
func (a *PayloadAuction) GetBidsForSlot(slot uint64) []*SignedBuilderBid {
	a.mu.RLock()
	defer a.mu.RUnlock()

	bids := a.bids[slot]
	result := make([]*SignedBuilderBid, len(bids))
	copy(result, bids)
	return result
}

// BidCount returns the number of bids for a given slot.
func (a *PayloadAuction) BidCount(slot uint64) int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.bids[slot])
}

// PruneSlot removes all bids for a given slot to reclaim memory.
func (a *PayloadAuction) PruneSlot(slot uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.bids, slot)
}

// PruneBefore removes all bids for slots before the given slot.
func (a *PayloadAuction) PruneBefore(slot uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for s := range a.bids {
		if s < slot {
			delete(a.bids, s)
		}
	}
}
