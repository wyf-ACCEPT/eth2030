package epbs

import (
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func testAddr(b byte) types.Address {
	var a types.Address
	a[0] = b
	return a
}

func testHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func validMarketBid(slot uint64, value uint64, builder types.Address) *MarketBid {
	return &MarketBid{
		Bid: BuilderBid{
			Slot:            slot,
			Value:           value,
			BlockHash:       testHash(0xAA),
			ParentBlockHash: testHash(0xBB),
			GasLimit:        30_000_000,
		},
		BuilderAddr: builder,
		ReceivedAt:  time.Now(),
	}
}

// --- ValidateBid tests ---

func TestMarketValidateBidValid(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	bid := validMarketBid(100, 1000, testAddr(0x01))
	if err := bm.ValidateBid(bid); err != nil {
		t.Errorf("valid bid: %v", err)
	}
}

func TestMarketValidateBidNil(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	if err := bm.ValidateBid(nil); err != ErrMarketNilBid {
		t.Errorf("nil bid: got %v, want ErrMarketNilBid", err)
	}
}

func TestMarketValidateBidZeroValue(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	bid := validMarketBid(100, 0, testAddr(0x01))
	if err := bm.ValidateBid(bid); err != ErrMarketZeroValue {
		t.Errorf("zero value: got %v, want ErrMarketZeroValue", err)
	}
}

func TestMarketValidateBidZeroSlot(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	bid := validMarketBid(0, 1000, testAddr(0x01))
	if err := bm.ValidateBid(bid); err != ErrMarketZeroSlot {
		t.Errorf("zero slot: got %v, want ErrMarketZeroSlot", err)
	}
}

func TestMarketValidateBidEmptyBlockHash(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	bid := validMarketBid(100, 1000, testAddr(0x01))
	bid.Bid.BlockHash = types.Hash{}
	if err := bm.ValidateBid(bid); err != ErrMarketEmptyBlockHash {
		t.Errorf("empty block hash: got %v, want ErrMarketEmptyBlockHash", err)
	}
}

func TestMarketValidateBidEmptyParentHash(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	bid := validMarketBid(100, 1000, testAddr(0x01))
	bid.Bid.ParentBlockHash = types.Hash{}
	if err := bm.ValidateBid(bid); err != ErrMarketEmptyParentHash {
		t.Errorf("empty parent hash: got %v, want ErrMarketEmptyParentHash", err)
	}
}

func TestMarketValidateBidBelowReserve(t *testing.T) {
	cfg := DefaultBuilderMarketConfig()
	cfg.ReservePrice = 500
	bm := NewBuilderMarket(cfg)
	bid := validMarketBid(100, 100, testAddr(0x01))
	err := bm.ValidateBid(bid)
	if err == nil {
		t.Error("expected error for bid below reserve price")
	}
}

func TestMarketValidateBidBannedBuilder(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	addr := testAddr(0x01)
	bm.RegisterBuilder(addr)

	// Ban the builder.
	bm.mu.Lock()
	bm.builders[addr].Banned = true
	bm.mu.Unlock()

	bid := validMarketBid(100, 1000, addr)
	err := bm.ValidateBid(bid)
	if err == nil {
		t.Error("expected error for banned builder")
	}
}

// --- SubmitBid tests ---

func TestMarketSubmitBid(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	bm.RegisterBuilder(testAddr(0x01))

	bid := validMarketBid(100, 1000, testAddr(0x01))
	if err := bm.SubmitBid(bid); err != nil {
		t.Fatalf("SubmitBid: %v", err)
	}
	if bm.BidCount(100) != 1 {
		t.Errorf("BidCount = %d, want 1", bm.BidCount(100))
	}
}

func TestMarketSubmitBidFinalized(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	bm.RegisterBuilder(testAddr(0x01))

	bid := validMarketBid(100, 1000, testAddr(0x01))
	bm.SubmitBid(bid)
	bm.SelectWinner(100)

	bid2 := validMarketBid(100, 2000, testAddr(0x01))
	err := bm.SubmitBid(bid2)
	if err == nil {
		t.Error("expected error for bid on finalized slot")
	}
}

func TestMarketSubmitBidSorted(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())

	for i := byte(1); i <= 3; i++ {
		bm.RegisterBuilder(testAddr(i))
	}

	bm.SubmitBid(validMarketBid(100, 3000, testAddr(0x01)))
	bm.SubmitBid(validMarketBid(100, 7000, testAddr(0x02)))
	bm.SubmitBid(validMarketBid(100, 5000, testAddr(0x03)))

	bids := bm.GetBids(100)
	if len(bids) != 3 {
		t.Fatalf("bid count = %d, want 3", len(bids))
	}
	if bids[0].Bid.Value != 7000 {
		t.Errorf("bids[0] value = %d, want 7000", bids[0].Bid.Value)
	}
	if bids[1].Bid.Value != 5000 {
		t.Errorf("bids[1] value = %d, want 5000", bids[1].Bid.Value)
	}
	if bids[2].Bid.Value != 3000 {
		t.Errorf("bids[2] value = %d, want 3000", bids[2].Bid.Value)
	}
}

// --- SelectWinner tests ---

func TestMarketSelectWinnerVickrey(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())

	bm.RegisterBuilder(testAddr(0x01))
	bm.RegisterBuilder(testAddr(0x02))
	bm.RegisterBuilder(testAddr(0x03))

	bm.SubmitBid(validMarketBid(100, 3000, testAddr(0x01)))
	bm.SubmitBid(validMarketBid(100, 7000, testAddr(0x02)))
	bm.SubmitBid(validMarketBid(100, 5000, testAddr(0x03)))

	winner, price, err := bm.SelectWinner(100)
	if err != nil {
		t.Fatalf("SelectWinner: %v", err)
	}
	// Highest bidder wins.
	if winner.Bid.Value != 7000 {
		t.Errorf("winner value = %d, want 7000", winner.Bid.Value)
	}
	// Vickrey: pays second-highest price.
	if price != 5000 {
		t.Errorf("clearing price = %d, want 5000 (second-price)", price)
	}
}

func TestMarketSelectWinnerSingleBid(t *testing.T) {
	cfg := DefaultBuilderMarketConfig()
	cfg.ReservePrice = 100
	bm := NewBuilderMarket(cfg)

	bm.RegisterBuilder(testAddr(0x01))
	bm.SubmitBid(validMarketBid(100, 5000, testAddr(0x01)))

	winner, price, err := bm.SelectWinner(100)
	if err != nil {
		t.Fatalf("SelectWinner: %v", err)
	}
	if winner.Bid.Value != 5000 {
		t.Errorf("winner value = %d, want 5000", winner.Bid.Value)
	}
	// With one bid, price = reserve.
	if price != 100 {
		t.Errorf("single bid price = %d, want reserve 100", price)
	}
}

func TestMarketSelectWinnerNoBids(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	_, _, err := bm.SelectWinner(100)
	if err == nil {
		t.Error("expected error for no bids")
	}
}

func TestMarketSelectWinnerAlreadyFinalized(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	bm.RegisterBuilder(testAddr(0x01))
	bm.SubmitBid(validMarketBid(100, 1000, testAddr(0x01)))
	bm.SelectWinner(100)

	_, _, err := bm.SelectWinner(100)
	if err == nil {
		t.Error("expected error for already finalized slot")
	}
}

// --- ScoreBuilder tests ---

func TestMarketScoreBuilderUnknown(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	_, err := bm.ScoreBuilder(testAddr(0x99))
	if err == nil {
		t.Error("expected error for unknown builder")
	}
}

func TestMarketScoreBuilderInitial(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	bm.RegisterBuilder(testAddr(0x01))

	score, err := bm.ScoreBuilder(testAddr(0x01))
	if err != nil {
		t.Fatalf("ScoreBuilder: %v", err)
	}
	if score != 50.0 {
		t.Errorf("initial score = %f, want 50.0", score)
	}
}

func TestMarketScoreAfterDeliveries(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	addr := testAddr(0x01)
	bm.RegisterBuilder(addr)

	// Simulate some wins and deliveries.
	bm.mu.Lock()
	bm.builders[addr].TotalWins = 10
	bm.builders[addr].TotalDeliveries = 10
	bm.mu.Unlock()

	score, err := bm.ScoreBuilder(addr)
	if err != nil {
		t.Fatalf("ScoreBuilder: %v", err)
	}
	// With 100% delivery rate and no misses, score should be high.
	if score < 50 {
		t.Errorf("good builder score = %f, want > 50", score)
	}
}

// --- RecordDelivery / RecordMiss tests ---

func TestMarketRecordDelivery(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	addr := testAddr(0x01)
	bm.RegisterBuilder(addr)

	// Record a miss first.
	bm.RecordMiss(addr)
	profile, _ := bm.GetBuilderProfile(addr)
	if profile.ConsecutiveMisses != 1 {
		t.Errorf("consecutive misses = %d, want 1", profile.ConsecutiveMisses)
	}

	// Record a delivery: should reset consecutive misses.
	bm.RecordDelivery(addr)
	profile, _ = bm.GetBuilderProfile(addr)
	if profile.ConsecutiveMisses != 0 {
		t.Errorf("consecutive misses after delivery = %d, want 0", profile.ConsecutiveMisses)
	}
	if profile.TotalDeliveries != 1 {
		t.Errorf("total deliveries = %d, want 1", profile.TotalDeliveries)
	}
}

func TestMarketRecordMissBan(t *testing.T) {
	cfg := DefaultBuilderMarketConfig()
	cfg.MaxConsecutiveMisses = 3
	bm := NewBuilderMarket(cfg)
	addr := testAddr(0x01)
	bm.RegisterBuilder(addr)

	for i := uint64(0); i < 3; i++ {
		bm.RecordMiss(addr)
	}

	profile, _ := bm.GetBuilderProfile(addr)
	if !profile.Banned {
		t.Error("builder should be banned after 3 consecutive misses")
	}
}

func TestMarketRecordDeliveryUnknown(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	err := bm.RecordDelivery(testAddr(0x99))
	if err == nil {
		t.Error("expected error for unknown builder delivery")
	}
}

func TestMarketRecordMissUnknown(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	err := bm.RecordMiss(testAddr(0x99))
	if err == nil {
		t.Error("expected error for unknown builder miss")
	}
}

// --- RegisterBuilder / GetBuilderProfile ---

func TestMarketRegisterBuilder(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	addr := testAddr(0x01)
	profile := bm.RegisterBuilder(addr)
	if profile.Score != 50.0 {
		t.Errorf("initial score = %f, want 50.0", profile.Score)
	}
	if bm.BuilderCount() != 1 {
		t.Errorf("BuilderCount = %d, want 1", bm.BuilderCount())
	}
}

func TestMarketGetBuilderProfileUnknown(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	_, err := bm.GetBuilderProfile(testAddr(0x99))
	if err == nil {
		t.Error("expected error for unknown builder profile")
	}
}

// --- UnbanBuilder ---

func TestMarketUnbanBuilder(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	addr := testAddr(0x01)
	bm.RegisterBuilder(addr)

	// Ban.
	bm.mu.Lock()
	bm.builders[addr].Banned = true
	bm.mu.Unlock()

	bm.UnbanBuilder(addr)
	profile, _ := bm.GetBuilderProfile(addr)
	if profile.Banned {
		t.Error("builder should be unbanned")
	}
}

func TestMarketUnbanBuilderUnknown(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	err := bm.UnbanBuilder(testAddr(0x99))
	if err == nil {
		t.Error("expected error for unknown builder unban")
	}
}

// --- PruneBefore ---

func TestMarketPruneBefore(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	bm.RegisterBuilder(testAddr(0x01))

	for slot := uint64(95); slot <= 105; slot++ {
		bm.SubmitBid(validMarketBid(slot, 1000, testAddr(0x01)))
	}

	pruned := bm.PruneBefore(100)
	if pruned != 5 {
		t.Errorf("pruned = %d, want 5", pruned)
	}
	if bm.BidCount(99) != 0 {
		t.Error("slot 99 should be pruned")
	}
	if bm.BidCount(100) != 1 {
		t.Error("slot 100 should remain")
	}
}

// --- Concurrent access ---

func TestMarketConcurrentSubmit(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	for i := byte(0); i < 10; i++ {
		bm.RegisterBuilder(testAddr(i))
	}

	var wg sync.WaitGroup
	for i := byte(0); i < 10; i++ {
		wg.Add(1)
		go func(b byte) {
			defer wg.Done()
			bm.SubmitBid(validMarketBid(100, uint64(b+1)*100, testAddr(b)))
		}(i)
	}
	wg.Wait()

	if bm.BidCount(100) != 10 {
		t.Errorf("BidCount = %d, want 10", bm.BidCount(100))
	}
}

// --- GetBids ---

func TestMarketGetBidsEmpty(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	bids := bm.GetBids(999)
	if bids != nil {
		t.Errorf("expected nil bids for unknown slot, got %d", len(bids))
	}
}

// --- Default config ---

func TestMarketDefaultConfig(t *testing.T) {
	cfg := DefaultBuilderMarketConfig()
	if cfg.ReservePrice != 1 {
		t.Errorf("ReservePrice = %d, want 1", cfg.ReservePrice)
	}
	if cfg.MaxBidsPerSlot != 256 {
		t.Errorf("MaxBidsPerSlot = %d, want 256", cfg.MaxBidsPerSlot)
	}
	if cfg.MaxConsecutiveMisses != 3 {
		t.Errorf("MaxConsecutiveMisses = %d, want 3", cfg.MaxConsecutiveMisses)
	}
}
