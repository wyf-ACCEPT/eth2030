package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func auctionTestRoot(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

// --- OpenAuction ---

func TestAPS_OpenAuction(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	if err := aps.OpenAuction(10); err != nil {
		t.Fatalf("OpenAuction failed: %v", err)
	}
}

func TestAPS_OpenAuction_Duplicate(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	aps.OpenAuction(10)
	err := aps.OpenAuction(10)
	if err == nil {
		t.Error("expected error for duplicate auction open")
	}
}

// --- SubmitBid ---

func TestAPS_SubmitBid(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	aps.OpenAuction(10)

	bid := &AuctionBid{
		Bidder:          1,
		Slot:            10,
		Amount:          100 * GweiPerETH,
		BlockCommitment: auctionTestRoot(0xAA),
	}
	if err := aps.SubmitBid(bid); err != nil {
		t.Fatalf("SubmitBid failed: %v", err)
	}
	if aps.BidCount(10) != 1 {
		t.Errorf("BidCount = %d, want 1", aps.BidCount(10))
	}
}

func TestAPS_SubmitBid_ZeroAmount(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	aps.OpenAuction(10)

	bid := &AuctionBid{Bidder: 1, Slot: 10, Amount: 0}
	err := aps.SubmitBid(bid)
	if err != ErrAuctionZeroBid {
		t.Errorf("got %v, want ErrAuctionZeroBid", err)
	}
}

func TestAPS_SubmitBid_NoAuction(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	bid := &AuctionBid{Bidder: 1, Slot: 99, Amount: 100}
	err := aps.SubmitBid(bid)
	if err == nil {
		t.Error("expected error when no auction open")
	}
}

func TestAPS_SubmitBid_DuplicateBidder(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	aps.OpenAuction(10)

	bid1 := &AuctionBid{Bidder: 1, Slot: 10, Amount: 100}
	bid2 := &AuctionBid{Bidder: 1, Slot: 10, Amount: 200}
	aps.SubmitBid(bid1)
	err := aps.SubmitBid(bid2)
	if err == nil {
		t.Error("expected error for duplicate bidder")
	}
}

// --- CloseAuction / Vickrey clearing ---

func TestAPS_CloseAuction_SingleBid(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	aps.OpenAuction(10)
	aps.SubmitBid(&AuctionBid{Bidder: 1, Slot: 10, Amount: 500, BlockCommitment: auctionTestRoot(0xAA)})

	clearing, err := aps.CloseAuction(10)
	if err != nil {
		t.Fatalf("CloseAuction failed: %v", err)
	}
	if clearing.Winner != 1 {
		t.Errorf("Winner = %d, want 1", clearing.Winner)
	}
	// Single bidder pays own bid.
	if clearing.ClearingPrice != 500 {
		t.Errorf("ClearingPrice = %d, want 500", clearing.ClearingPrice)
	}
	if clearing.BidCount != 1 {
		t.Errorf("BidCount = %d, want 1", clearing.BidCount)
	}
}

func TestAPS_CloseAuction_VickreyPrice(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	aps.OpenAuction(10)
	aps.SubmitBid(&AuctionBid{Bidder: 1, Slot: 10, Amount: 500, BlockCommitment: auctionTestRoot(0xAA)})
	aps.SubmitBid(&AuctionBid{Bidder: 2, Slot: 10, Amount: 300, BlockCommitment: auctionTestRoot(0xBB)})
	aps.SubmitBid(&AuctionBid{Bidder: 3, Slot: 10, Amount: 100, BlockCommitment: auctionTestRoot(0xCC)})

	clearing, err := aps.CloseAuction(10)
	if err != nil {
		t.Fatalf("CloseAuction failed: %v", err)
	}
	// Highest bid: 500, winner: bidder 1.
	if clearing.Winner != 1 {
		t.Errorf("Winner = %d, want 1", clearing.Winner)
	}
	// Vickrey: pay second-highest = 300.
	if clearing.ClearingPrice != 300 {
		t.Errorf("ClearingPrice = %d, want 300 (second-price Vickrey)", clearing.ClearingPrice)
	}
	if clearing.WinningBid != 500 {
		t.Errorf("WinningBid = %d, want 500", clearing.WinningBid)
	}
	if clearing.BidCount != 3 {
		t.Errorf("BidCount = %d, want 3", clearing.BidCount)
	}
}

func TestAPS_CloseAuction_NoBids(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	aps.OpenAuction(10)

	_, err := aps.CloseAuction(10)
	if err != ErrAuctionNoBids {
		t.Errorf("got %v, want ErrAuctionNoBids", err)
	}
}

func TestAPS_CloseAuction_NoAuction(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	_, err := aps.CloseAuction(99)
	if err == nil {
		t.Error("expected error closing non-existent auction")
	}
}

func TestAPS_CloseAuction_RecordsSchedule(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	aps.OpenAuction(10)
	aps.SubmitBid(&AuctionBid{Bidder: 5, Slot: 10, Amount: 1000})
	aps.CloseAuction(10)

	entry, ok := aps.GetScheduleEntry(10)
	if !ok {
		t.Fatal("schedule entry not found for slot 10")
	}
	if entry.ProposerIndex != 5 {
		t.Errorf("ProposerIndex = %d, want 5", entry.ProposerIndex)
	}
	if !entry.IsAuctioned {
		t.Error("should be marked as auctioned")
	}
}

func TestAPS_CloseAuction_Idempotent(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	aps.OpenAuction(10)
	aps.SubmitBid(&AuctionBid{Bidder: 1, Slot: 10, Amount: 100})

	c1, _ := aps.CloseAuction(10)
	c2, _ := aps.CloseAuction(10) // second close returns cached result

	if c1.Winner != c2.Winner || c1.ClearingPrice != c2.ClearingPrice {
		t.Error("second close should return same result")
	}
}

// --- FallbackProposer ---

func TestAPS_FallbackProposer(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	validators := []uint64{10, 20, 30, 40, 50}
	seed := auctionTestRoot(0xDD)

	proposer := aps.FallbackProposer(10, validators, seed)
	// Verify it's one of the validators.
	found := false
	for _, v := range validators {
		if proposer == v {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FallbackProposer = %d, not in validator set", proposer)
	}

	// Should be recorded in schedule.
	entry, ok := aps.GetScheduleEntry(10)
	if !ok {
		t.Fatal("schedule entry not found")
	}
	if entry.IsAuctioned {
		t.Error("fallback should not be marked as auctioned")
	}
	if entry.ProposerIndex != proposer {
		t.Errorf("schedule ProposerIndex = %d, want %d", entry.ProposerIndex, proposer)
	}
}

func TestAPS_FallbackProposer_Deterministic(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	validators := []uint64{10, 20, 30, 40, 50}
	seed := auctionTestRoot(0xEE)

	p1 := aps.FallbackProposer(10, validators, seed)
	p2 := aps.FallbackProposer(10, validators, seed)
	if p1 != p2 {
		t.Errorf("fallback not deterministic: %d vs %d", p1, p2)
	}
}

func TestAPS_FallbackProposer_DifferentSlots(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	validators := make([]uint64, 100)
	for i := range validators {
		validators[i] = uint64(i)
	}
	seed := auctionTestRoot(0xFF)

	// Different slots should produce different proposers (with high probability).
	results := make(map[uint64]bool)
	for slot := uint64(0); slot < 20; slot++ {
		p := aps.FallbackProposer(slot, validators, seed)
		results[p] = true
	}
	// At least 2 distinct proposers across 20 slots with 100 validators.
	if len(results) < 2 {
		t.Errorf("expected multiple distinct proposers, got %d", len(results))
	}
}

func TestAPS_FallbackProposer_EmptyValidators(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	p := aps.FallbackProposer(10, nil, auctionTestRoot(0x01))
	if p != 0 {
		t.Errorf("FallbackProposer with empty validators = %d, want 0", p)
	}
}

// --- Committee Rotation ---

func TestAPS_RotateCommittee(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	validators := []uint64{0, 1, 2, 3, 4, 5, 6, 7}
	seed := auctionTestRoot(0xAB)

	entry := aps.RotateCommittee(5, validators, seed)
	if entry.Epoch != 5 {
		t.Errorf("Epoch = %d, want 5", entry.Epoch)
	}
	if len(entry.Committee) != len(validators) {
		t.Errorf("Committee size = %d, want %d", len(entry.Committee), len(validators))
	}

	// Verify all validators are present.
	seen := make(map[uint64]bool)
	for _, v := range entry.Committee {
		seen[v] = true
	}
	for _, v := range validators {
		if !seen[v] {
			t.Errorf("validator %d missing from committee", v)
		}
	}
}

func TestAPS_RotateCommittee_Deterministic(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	validators := []uint64{0, 1, 2, 3, 4}
	seed := auctionTestRoot(0xCD)

	e1 := aps.RotateCommittee(10, validators, seed)
	e2 := aps.RotateCommittee(10, validators, seed)

	for i := range e1.Committee {
		if e1.Committee[i] != e2.Committee[i] {
			t.Errorf("committee[%d] = %d vs %d, not deterministic", i, e1.Committee[i], e2.Committee[i])
		}
	}
}

func TestAPS_RotateCommittee_DifferentEpochs(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	validators := make([]uint64, 50)
	for i := range validators {
		validators[i] = uint64(i)
	}
	seed := auctionTestRoot(0xEF)

	e1 := aps.RotateCommittee(1, validators, seed)
	e2 := aps.RotateCommittee(2, validators, seed)

	// Different epochs should produce different orderings (with high prob).
	differ := false
	for i := range e1.Committee {
		if e1.Committee[i] != e2.Committee[i] {
			differ = true
			break
		}
	}
	if !differ {
		t.Error("different epochs should produce different committee orderings")
	}
}

func TestAPS_GetCommittee(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	validators := []uint64{10, 20, 30}
	seed := auctionTestRoot(0x01)

	aps.RotateCommittee(5, validators, seed)

	committee, ok := aps.GetCommittee(5)
	if !ok {
		t.Fatal("committee not found for epoch 5")
	}
	if len(committee) != 3 {
		t.Errorf("committee size = %d, want 3", len(committee))
	}
}

func TestAPS_GetCommittee_NotFound(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	_, ok := aps.GetCommittee(99)
	if ok {
		t.Error("expected not found for non-existent epoch")
	}
}

func TestAPS_GetClearing(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	aps.OpenAuction(10)
	aps.SubmitBid(&AuctionBid{Bidder: 1, Slot: 10, Amount: 500})
	aps.SubmitBid(&AuctionBid{Bidder: 2, Slot: 10, Amount: 300})
	aps.CloseAuction(10)

	clearing, ok := aps.GetClearing(10)
	if !ok {
		t.Fatal("clearing not found")
	}
	if clearing.Winner != 1 {
		t.Errorf("Winner = %d, want 1", clearing.Winner)
	}
}

func TestAPS_GetClearing_NotFound(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	_, ok := aps.GetClearing(99)
	if ok {
		t.Error("expected not found for non-existent clearing")
	}
}

func TestAPS_SubmitBid_AfterClose(t *testing.T) {
	aps := NewAuctionedProposerSelection(DefaultAuctionedProposerConfig())
	aps.OpenAuction(10)
	aps.SubmitBid(&AuctionBid{Bidder: 1, Slot: 10, Amount: 100})
	aps.CloseAuction(10)

	err := aps.SubmitBid(&AuctionBid{Bidder: 2, Slot: 10, Amount: 200})
	if err == nil {
		t.Error("expected error submitting bid after auction closed")
	}
}
