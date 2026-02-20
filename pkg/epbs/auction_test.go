package epbs

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func validSignedBid(slot, value uint64, builder BuilderIndex) *SignedBuilderBid {
	return &SignedBuilderBid{
		Message: BuilderBid{
			ParentBlockHash: types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			BlockHash:       types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			Slot:            slot,
			Value:           value,
			GasLimit:        30_000_000,
			BuilderIndex:    builder,
			FeeRecipient:    types.HexToAddress("0xdead"),
		},
	}
}

func TestPayloadAuctionNew(t *testing.T) {
	a := NewPayloadAuction()
	if a == nil {
		t.Fatal("NewPayloadAuction returned nil")
	}
	if a.BidCount(1) != 0 {
		t.Errorf("new auction should have 0 bids")
	}
}

func TestPayloadAuctionSubmitBidValid(t *testing.T) {
	a := NewPayloadAuction()
	bid := validSignedBid(10, 5000, 1)
	if err := a.SubmitBid(bid); err != nil {
		t.Fatalf("SubmitBid: %v", err)
	}
	if a.BidCount(10) != 1 {
		t.Errorf("expected 1 bid, got %d", a.BidCount(10))
	}
}

func TestPayloadAuctionSubmitInvalidBid(t *testing.T) {
	a := NewPayloadAuction()
	// Zero value is invalid.
	bid := validSignedBid(10, 0, 1)
	if err := a.SubmitBid(bid); err == nil {
		t.Error("expected error for zero-value bid")
	}
	if a.BidCount(10) != 0 {
		t.Error("invalid bid should not be stored")
	}
}

func TestPayloadAuctionGetWinningBidHighestValue(t *testing.T) {
	a := NewPayloadAuction()

	// Submit bids with different values.
	for i, v := range []uint64{3000, 7000, 5000, 1000} {
		bid := validSignedBid(100, v, BuilderIndex(i+1))
		if err := a.SubmitBid(bid); err != nil {
			t.Fatalf("SubmitBid: %v", err)
		}
	}

	winner, err := a.GetWinningBid(100)
	if err != nil {
		t.Fatalf("GetWinningBid: %v", err)
	}
	if winner.Message.Value != 7000 {
		t.Errorf("winning value = %d, want 7000", winner.Message.Value)
	}
}

func TestPayloadAuctionGetWinningBidEmpty(t *testing.T) {
	a := NewPayloadAuction()
	_, err := a.GetWinningBid(42)
	if err != ErrNoBidsForSlot {
		t.Errorf("expected ErrNoBidsForSlot, got %v", err)
	}
}

func TestPayloadAuctionGetBidsForSlotOrdering(t *testing.T) {
	a := NewPayloadAuction()

	values := []uint64{100, 500, 300, 200, 400}
	for i, v := range values {
		bid := validSignedBid(50, v, BuilderIndex(i))
		a.SubmitBid(bid)
	}

	bids := a.GetBidsForSlot(50)
	if len(bids) != 5 {
		t.Fatalf("expected 5 bids, got %d", len(bids))
	}
	// Verify descending order.
	for i := 1; i < len(bids); i++ {
		if bids[i].Message.Value > bids[i-1].Message.Value {
			t.Errorf("bids not sorted: bids[%d].Value=%d > bids[%d].Value=%d",
				i, bids[i].Message.Value, i-1, bids[i-1].Message.Value)
		}
	}
}

func TestPayloadAuctionGetBidsForSlotEmpty(t *testing.T) {
	a := NewPayloadAuction()
	bids := a.GetBidsForSlot(999)
	if len(bids) != 0 {
		t.Errorf("expected 0 bids, got %d", len(bids))
	}
}

func TestPayloadAuctionGetBidsReturnsDefensiveCopy(t *testing.T) {
	a := NewPayloadAuction()
	bid := validSignedBid(10, 5000, 1)
	a.SubmitBid(bid)

	bids := a.GetBidsForSlot(10)
	bids[0] = nil // Mutate the copy.

	// Original should be unchanged.
	bids2 := a.GetBidsForSlot(10)
	if bids2[0] == nil {
		t.Error("GetBidsForSlot should return a defensive copy")
	}
}

func TestPayloadAuctionBidCountMultipleSlots(t *testing.T) {
	a := NewPayloadAuction()

	a.SubmitBid(validSignedBid(10, 1000, 1))
	a.SubmitBid(validSignedBid(10, 2000, 2))
	a.SubmitBid(validSignedBid(20, 3000, 1))

	if a.BidCount(10) != 2 {
		t.Errorf("slot 10: expected 2 bids, got %d", a.BidCount(10))
	}
	if a.BidCount(20) != 1 {
		t.Errorf("slot 20: expected 1 bid, got %d", a.BidCount(20))
	}
	if a.BidCount(30) != 0 {
		t.Errorf("slot 30: expected 0 bids, got %d", a.BidCount(30))
	}
}

func TestPayloadAuctionPruneSlot(t *testing.T) {
	a := NewPayloadAuction()

	a.SubmitBid(validSignedBid(10, 1000, 1))
	a.SubmitBid(validSignedBid(10, 2000, 2))
	a.SubmitBid(validSignedBid(20, 3000, 1))

	a.PruneSlot(10)

	if a.BidCount(10) != 0 {
		t.Error("slot 10 should have 0 bids after prune")
	}
	if a.BidCount(20) != 1 {
		t.Error("slot 20 should be unaffected by prune")
	}
}

func TestPayloadAuctionPruneBefore(t *testing.T) {
	a := NewPayloadAuction()

	for slot := uint64(1); slot <= 10; slot++ {
		a.SubmitBid(validSignedBid(slot, slot*100, 1))
	}

	a.PruneBefore(5)

	for slot := uint64(1); slot < 5; slot++ {
		if a.BidCount(slot) != 0 {
			t.Errorf("slot %d should be pruned", slot)
		}
	}
	for slot := uint64(5); slot <= 10; slot++ {
		if a.BidCount(slot) != 1 {
			t.Errorf("slot %d should have 1 bid", slot)
		}
	}
}

func TestPayloadAuctionPruneBeforeZero(t *testing.T) {
	a := NewPayloadAuction()
	a.SubmitBid(validSignedBid(1, 100, 1))
	a.PruneBefore(0)
	if a.BidCount(1) != 1 {
		t.Error("PruneBefore(0) should not prune anything")
	}
}

func TestPayloadAuctionPruneNonexistentSlot(t *testing.T) {
	a := NewPayloadAuction()
	// Should not panic.
	a.PruneSlot(999)
}
