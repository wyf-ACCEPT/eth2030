package epbs

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func testPubkey(b byte) [48]byte {
	var pk [48]byte
	pk[0] = b
	pk[1] = 0xFF
	return pk
}

func testPayloadHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	h[1] = 0xAA
	return h
}

func testBid(slot uint64, value int64, pk byte) *AuctionBid {
	return &AuctionBid{
		BuilderPubkey: testPubkey(pk),
		Slot:          slot,
		Value:         big.NewInt(value),
		PayloadHash:   testPayloadHash(pk),
		Timestamp:     time.Now(),
	}
}

func TestDefaultAuctionEngineConfig(t *testing.T) {
	cfg := DefaultAuctionEngineConfig()
	if cfg.MaxBidsPerRound != 256 {
		t.Errorf("MaxBidsPerRound = %d, want 256", cfg.MaxBidsPerRound)
	}
	if cfg.MaxHistory != 128 {
		t.Errorf("MaxHistory = %d, want 128", cfg.MaxHistory)
	}
}

func TestNewAuctionEngine_NilConfig(t *testing.T) {
	ae := NewAuctionEngine(nil)
	if ae.config.MaxBidsPerRound != 256 {
		t.Error("nil config should use defaults")
	}
}

func TestAuctionState_String(t *testing.T) {
	tests := []struct {
		state AuctionState
		want  string
	}{
		{AuctionOpen, "Open"},
		{AuctionBiddingClosed, "BiddingClosed"},
		{AuctionWinnerSelected, "WinnerSelected"},
		{AuctionFinalized, "Finalized"},
		{AuctionState(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("AuctionState(%d).String() = %s, want %s", tt.state, got, tt.want)
		}
	}
}

func TestOpenAuction_Basic(t *testing.T) {
	ae := NewAuctionEngine(nil)
	if err := ae.OpenAuction(100); err != nil {
		t.Fatalf("OpenAuction: %v", err)
	}
	if ae.GetState() != AuctionOpen {
		t.Errorf("state = %v, want Open", ae.GetState())
	}
	if ae.CurrentSlot() != 100 {
		t.Errorf("CurrentSlot = %d, want 100", ae.CurrentSlot())
	}
}

func TestOpenAuction_InvalidSlot(t *testing.T) {
	ae := NewAuctionEngine(nil)
	if err := ae.OpenAuction(0); err != ErrAuctionInvalidSlot {
		t.Errorf("slot 0: got %v, want ErrAuctionInvalidSlot", err)
	}
}

func TestOpenAuction_AlreadyOpen(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	if err := ae.OpenAuction(101); err != ErrAuctionAlreadyOpen {
		t.Errorf("double open: got %v, want ErrAuctionAlreadyOpen", err)
	}
}

func TestSubmitBid_Basic(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)

	bid := testBid(100, 5000, 1)
	if err := ae.SubmitBid(bid); err != nil {
		t.Fatalf("SubmitBid: %v", err)
	}
	if ae.BidCount() != 1 {
		t.Errorf("BidCount = %d, want 1", ae.BidCount())
	}
}

func TestSubmitBid_NilBid(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	if err := ae.SubmitBid(nil); err != ErrAuctionNilBid {
		t.Errorf("nil bid: got %v, want ErrAuctionNilBid", err)
	}
}

func TestSubmitBid_ZeroValue(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)

	bid := testBid(100, 0, 1)
	bid.Value = big.NewInt(0)
	if err := ae.SubmitBid(bid); err != ErrAuctionZeroValue {
		t.Errorf("zero value: got %v, want ErrAuctionZeroValue", err)
	}
}

func TestSubmitBid_NilValue(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)

	bid := testBid(100, 1, 1)
	bid.Value = nil
	if err := ae.SubmitBid(bid); err != ErrAuctionZeroValue {
		t.Errorf("nil value: got %v, want ErrAuctionZeroValue", err)
	}
}

func TestSubmitBid_NegativeValue(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)

	bid := testBid(100, -5, 1)
	if err := ae.SubmitBid(bid); err != ErrAuctionZeroValue {
		t.Errorf("negative value: got %v, want ErrAuctionZeroValue", err)
	}
}

func TestSubmitBid_InvalidSlot(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)

	bid := testBid(0, 5000, 1)
	if err := ae.SubmitBid(bid); err != ErrAuctionInvalidSlot {
		t.Errorf("slot 0: got %v, want ErrAuctionInvalidSlot", err)
	}
}

func TestSubmitBid_EmptyPubkey(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)

	bid := testBid(100, 5000, 1)
	bid.BuilderPubkey = [48]byte{}
	if err := ae.SubmitBid(bid); err != ErrAuctionEmptyPubkey {
		t.Errorf("empty pubkey: got %v, want ErrAuctionEmptyPubkey", err)
	}
}

func TestSubmitBid_EmptyPayloadHash(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)

	bid := testBid(100, 5000, 1)
	bid.PayloadHash = types.Hash{}
	if err := ae.SubmitBid(bid); err != ErrAuctionEmptyPayload {
		t.Errorf("empty payload: got %v, want ErrAuctionEmptyPayload", err)
	}
}

func TestSubmitBid_SlotMismatch(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)

	bid := testBid(200, 5000, 1)
	if err := ae.SubmitBid(bid); err != ErrAuctionSlotMismatch {
		t.Errorf("slot mismatch: got %v, want ErrAuctionSlotMismatch", err)
	}
}

func TestSubmitBid_NoRound(t *testing.T) {
	ae := NewAuctionEngine(nil)
	bid := testBid(100, 5000, 1)
	if err := ae.SubmitBid(bid); err != ErrAuctionNoRound {
		t.Errorf("no round: got %v, want ErrAuctionNoRound", err)
	}
}

func TestSubmitBid_NotOpen(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	ae.CloseBidding()

	bid := testBid(100, 5000, 1)
	if err := ae.SubmitBid(bid); err != ErrAuctionNotOpen {
		t.Errorf("not open: got %v, want ErrAuctionNotOpen", err)
	}
}

func TestCloseBidding_Basic(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	ae.SubmitBid(testBid(100, 5000, 1))

	if err := ae.CloseBidding(); err != nil {
		t.Fatalf("CloseBidding: %v", err)
	}
	if ae.GetState() != AuctionBiddingClosed {
		t.Errorf("state = %v, want BiddingClosed", ae.GetState())
	}
}

func TestCloseBidding_NoRound(t *testing.T) {
	ae := NewAuctionEngine(nil)
	if err := ae.CloseBidding(); err != ErrAuctionNoRound {
		t.Errorf("no round: got %v, want ErrAuctionNoRound", err)
	}
}

func TestCloseBidding_NotOpen(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	ae.CloseBidding()

	if err := ae.CloseBidding(); err != ErrAuctionNotOpen {
		t.Errorf("double close: got %v, want ErrAuctionNotOpen", err)
	}
}

func TestSelectWinner_HighestValue(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)

	ae.SubmitBid(testBid(100, 1000, 1))
	ae.SubmitBid(testBid(100, 5000, 2))
	ae.SubmitBid(testBid(100, 3000, 3))

	ae.CloseBidding()

	winner, err := ae.SelectWinner()
	if err != nil {
		t.Fatalf("SelectWinner: %v", err)
	}
	if winner.Value.Cmp(big.NewInt(5000)) != 0 {
		t.Errorf("winner value = %s, want 5000", winner.Value)
	}
	if winner.BuilderPubkey != testPubkey(2) {
		t.Errorf("winner pubkey mismatch")
	}
	if ae.GetState() != AuctionWinnerSelected {
		t.Errorf("state = %v, want WinnerSelected", ae.GetState())
	}
}

func TestSelectWinner_TiebreakByTimestamp(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)

	early := time.Now().Add(-1 * time.Second)
	late := time.Now()

	bid1 := testBid(100, 5000, 1)
	bid1.Timestamp = late

	bid2 := testBid(100, 5000, 2)
	bid2.Timestamp = early

	ae.SubmitBid(bid1)
	ae.SubmitBid(bid2)
	ae.CloseBidding()

	winner, err := ae.SelectWinner()
	if err != nil {
		t.Fatalf("SelectWinner: %v", err)
	}
	// bid2 has earlier timestamp and same value, so it wins.
	if winner.BuilderPubkey != testPubkey(2) {
		t.Error("tiebreak: earlier timestamp should win")
	}
}

func TestSelectWinner_NoBids(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	ae.CloseBidding()

	_, err := ae.SelectWinner()
	if err != ErrAuctionNoBids {
		t.Errorf("no bids: got %v, want ErrAuctionNoBids", err)
	}
}

func TestSelectWinner_NoRound(t *testing.T) {
	ae := NewAuctionEngine(nil)
	_, err := ae.SelectWinner()
	if err != ErrAuctionNoRound {
		t.Errorf("no round: got %v, want ErrAuctionNoRound", err)
	}
}

func TestSelectWinner_NotClosed(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	ae.SubmitBid(testBid(100, 5000, 1))

	_, err := ae.SelectWinner()
	if err != ErrAuctionNotClosed {
		t.Errorf("not closed: got %v, want ErrAuctionNotClosed", err)
	}
}

func TestFinalizeAuction_Basic(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	ae.SubmitBid(testBid(100, 5000, 1))
	ae.CloseBidding()
	ae.SelectWinner()

	if err := ae.FinalizeAuction(); err != nil {
		t.Fatalf("FinalizeAuction: %v", err)
	}
	if ae.GetState() != AuctionFinalized {
		t.Errorf("state = %v, want Finalized", ae.GetState())
	}
}

func TestFinalizeAuction_NoRound(t *testing.T) {
	ae := NewAuctionEngine(nil)
	if err := ae.FinalizeAuction(); err != ErrAuctionNoRound {
		t.Errorf("no round: got %v, want ErrAuctionNoRound", err)
	}
}

func TestFinalizeAuction_AlreadyFinalized(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	ae.SubmitBid(testBid(100, 5000, 1))
	ae.CloseBidding()
	ae.SelectWinner()
	ae.FinalizeAuction()

	if err := ae.FinalizeAuction(); err != ErrAuctionAlreadyFinalized {
		t.Errorf("double finalize: got %v, want ErrAuctionAlreadyFinalized", err)
	}
}

func TestFinalizeAuction_WinnerNotSelected(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	ae.SubmitBid(testBid(100, 5000, 1))
	ae.CloseBidding()

	if err := ae.FinalizeAuction(); err != ErrAuctionWinnerNotSet {
		t.Errorf("no winner: got %v, want ErrAuctionWinnerNotSet", err)
	}
}

func TestGetWinner_Basic(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	ae.SubmitBid(testBid(100, 5000, 1))
	ae.CloseBidding()
	ae.SelectWinner()

	winner, err := ae.GetWinner()
	if err != nil {
		t.Fatalf("GetWinner: %v", err)
	}
	if winner.Value.Cmp(big.NewInt(5000)) != 0 {
		t.Errorf("winner value = %s, want 5000", winner.Value)
	}
}

func TestGetWinner_NoRound(t *testing.T) {
	ae := NewAuctionEngine(nil)
	_, err := ae.GetWinner()
	if err != ErrAuctionNoRound {
		t.Errorf("no round: got %v, want ErrAuctionNoRound", err)
	}
}

func TestGetWinner_NoWinner(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	_, err := ae.GetWinner()
	if err != ErrAuctionNoWinner {
		t.Errorf("no winner: got %v, want ErrAuctionNoWinner", err)
	}
}

func TestHistory_Basic(t *testing.T) {
	ae := NewAuctionEngine(nil)

	for slot := uint64(1); slot <= 3; slot++ {
		ae.OpenAuction(slot)
		ae.SubmitBid(testBid(slot, int64(slot*1000), byte(slot)))
		ae.CloseBidding()
		ae.SelectWinner()
		ae.FinalizeAuction()
	}

	hist := ae.History(2)
	if len(hist) != 2 {
		t.Fatalf("History(2) = %d, want 2", len(hist))
	}
	if hist[0].Slot != 2 {
		t.Errorf("hist[0].Slot = %d, want 2", hist[0].Slot)
	}
	if hist[1].Slot != 3 {
		t.Errorf("hist[1].Slot = %d, want 3", hist[1].Slot)
	}
}

func TestHistory_Empty(t *testing.T) {
	ae := NewAuctionEngine(nil)
	if ae.History(5) != nil {
		t.Error("expected nil for empty history")
	}
}

func TestHistory_RequestMoreThanAvailable(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(1)
	ae.SubmitBid(testBid(1, 5000, 1))
	ae.CloseBidding()
	ae.SelectWinner()
	ae.FinalizeAuction()

	hist := ae.History(100)
	if len(hist) != 1 {
		t.Errorf("History(100) = %d, want 1", len(hist))
	}
}

func TestHistoryCount(t *testing.T) {
	ae := NewAuctionEngine(nil)
	if ae.HistoryCount() != 0 {
		t.Errorf("initial HistoryCount = %d, want 0", ae.HistoryCount())
	}

	ae.OpenAuction(1)
	ae.SubmitBid(testBid(1, 5000, 1))
	ae.CloseBidding()
	ae.SelectWinner()
	ae.FinalizeAuction()

	if ae.HistoryCount() != 1 {
		t.Errorf("HistoryCount = %d, want 1", ae.HistoryCount())
	}
}

func TestRecordViolation_Basic(t *testing.T) {
	ae := NewAuctionEngine(nil)

	// Run a full auction cycle first.
	ae.OpenAuction(100)
	ae.SubmitBid(testBid(100, 5000, 1))
	ae.CloseBidding()
	ae.SelectWinner()
	ae.FinalizeAuction()

	ae.RecordViolation(testPubkey(1), 100, big.NewInt(5000))

	if ae.ViolationCount() != 1 {
		t.Errorf("ViolationCount = %d, want 1", ae.ViolationCount())
	}

	vs := ae.Violations()
	if len(vs) != 1 {
		t.Fatalf("Violations = %d, want 1", len(vs))
	}
	if vs[0].Slot != 100 {
		t.Errorf("violation slot = %d, want 100", vs[0].Slot)
	}
	if vs[0].BidValue.Cmp(big.NewInt(5000)) != 0 {
		t.Errorf("violation value = %s, want 5000", vs[0].BidValue)
	}
}

func TestRecordViolation_MarksHistoryUndelivered(t *testing.T) {
	ae := NewAuctionEngine(nil)

	ae.OpenAuction(100)
	ae.SubmitBid(testBid(100, 5000, 1))
	ae.CloseBidding()
	ae.SelectWinner()
	ae.FinalizeAuction()

	ae.RecordViolation(testPubkey(1), 100, big.NewInt(5000))

	hist := ae.History(1)
	if len(hist) != 1 {
		t.Fatal("expected 1 history entry")
	}
	if hist[0].PayloadDelivered {
		t.Error("payload should be marked as not delivered")
	}
}

func TestOpenAuction_AfterFinalize(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	ae.SubmitBid(testBid(100, 5000, 1))
	ae.CloseBidding()
	ae.SelectWinner()
	ae.FinalizeAuction()

	// Should be able to open a new round after finalization.
	if err := ae.OpenAuction(101); err != nil {
		t.Fatalf("OpenAuction after finalize: %v", err)
	}
	if ae.CurrentSlot() != 101 {
		t.Errorf("CurrentSlot = %d, want 101", ae.CurrentSlot())
	}
}

func TestBidCount_NoRound(t *testing.T) {
	ae := NewAuctionEngine(nil)
	if ae.BidCount() != 0 {
		t.Errorf("BidCount = %d, want 0", ae.BidCount())
	}
}

func TestCurrentSlot_NoRound(t *testing.T) {
	ae := NewAuctionEngine(nil)
	if ae.CurrentSlot() != 0 {
		t.Errorf("CurrentSlot = %d, want 0", ae.CurrentSlot())
	}
}

func TestGetState_NoRound(t *testing.T) {
	ae := NewAuctionEngine(nil)
	if ae.GetState() != AuctionFinalized {
		t.Errorf("state = %v, want Finalized (no round)", ae.GetState())
	}
}

func TestFullLifecycle(t *testing.T) {
	ae := NewAuctionEngine(nil)

	// Phase 1: Open
	if err := ae.OpenAuction(42); err != nil {
		t.Fatalf("OpenAuction: %v", err)
	}
	if ae.GetState() != AuctionOpen {
		t.Fatalf("expected Open state")
	}

	// Phase 2: Submit bids
	for i := byte(1); i <= 5; i++ {
		ae.SubmitBid(testBid(42, int64(i)*1000, i))
	}
	if ae.BidCount() != 5 {
		t.Fatalf("BidCount = %d, want 5", ae.BidCount())
	}

	// Phase 3: Close bidding
	ae.CloseBidding()
	if ae.GetState() != AuctionBiddingClosed {
		t.Fatalf("expected BiddingClosed state")
	}

	// Phase 4: Select winner
	winner, err := ae.SelectWinner()
	if err != nil {
		t.Fatalf("SelectWinner: %v", err)
	}
	if winner.Value.Cmp(big.NewInt(5000)) != 0 {
		t.Errorf("winner value = %s, want 5000", winner.Value)
	}
	if ae.GetState() != AuctionWinnerSelected {
		t.Fatalf("expected WinnerSelected state")
	}

	// Phase 5: Finalize
	ae.FinalizeAuction()
	if ae.GetState() != AuctionFinalized {
		t.Fatalf("expected Finalized state")
	}

	// Verify history
	if ae.HistoryCount() != 1 {
		t.Errorf("HistoryCount = %d, want 1", ae.HistoryCount())
	}
	hist := ae.History(1)
	if hist[0].TotalBids != 5 {
		t.Errorf("TotalBids = %d, want 5", hist[0].TotalBids)
	}
}

func TestConcurrentBidSubmission(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)

	var wg sync.WaitGroup
	for i := byte(1); i <= 20; i++ {
		wg.Add(1)
		go func(idx byte) {
			defer wg.Done()
			ae.SubmitBid(testBid(100, int64(idx)*100, idx))
		}(i)
	}
	wg.Wait()

	if ae.BidCount() != 20 {
		t.Errorf("BidCount = %d, want 20", ae.BidCount())
	}
}

func TestHistoryLimit(t *testing.T) {
	cfg := &AuctionEngineConfig{
		MaxBidsPerRound: 256,
		MaxHistory:      3,
	}
	ae := NewAuctionEngine(cfg)

	for slot := uint64(1); slot <= 5; slot++ {
		ae.OpenAuction(slot)
		ae.SubmitBid(testBid(slot, int64(slot*1000), byte(slot)))
		ae.CloseBidding()
		ae.SelectWinner()
		ae.FinalizeAuction()
	}

	if ae.HistoryCount() != 3 {
		t.Errorf("HistoryCount = %d, want 3 (limited)", ae.HistoryCount())
	}
	// Should retain slots 3, 4, 5.
	hist := ae.History(3)
	if hist[0].Slot != 3 {
		t.Errorf("oldest slot = %d, want 3", hist[0].Slot)
	}
	if hist[2].Slot != 5 {
		t.Errorf("newest slot = %d, want 5", hist[2].Slot)
	}
}

func TestSelectWinner_SingleBid(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)
	ae.SubmitBid(testBid(100, 7777, 1))
	ae.CloseBidding()

	winner, err := ae.SelectWinner()
	if err != nil {
		t.Fatalf("SelectWinner: %v", err)
	}
	if winner.Value.Cmp(big.NewInt(7777)) != 0 {
		t.Errorf("winner value = %s, want 7777", winner.Value)
	}
}

func TestSubmitBid_ZeroTimestampSet(t *testing.T) {
	ae := NewAuctionEngine(nil)
	ae.OpenAuction(100)

	bid := &AuctionBid{
		BuilderPubkey: testPubkey(1),
		Slot:          100,
		Value:         big.NewInt(1000),
		PayloadHash:   testPayloadHash(1),
		// Timestamp is zero
	}
	if err := ae.SubmitBid(bid); err != nil {
		t.Fatalf("SubmitBid: %v", err)
	}
	if bid.Timestamp.IsZero() {
		t.Error("timestamp should be set when submitted as zero")
	}
}
