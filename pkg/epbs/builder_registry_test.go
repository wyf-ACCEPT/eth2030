package epbs

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// registryAddr generates a unique address for registry tests.
func registryAddr(b byte) types.Address {
	var a types.Address
	a[19] = b
	return a
}

// registryHash generates a unique hash for registry tests.
func registryHash(b byte) types.Hash {
	var h types.Hash
	h[31] = b
	return h
}

// registryBid creates a MarketBid suitable for builder registry tests.
func registryBid(slot, value uint64, builder types.Address) *MarketBid {
	return &MarketBid{
		Bid: BuilderBid{
			Slot:            slot,
			Value:           value,
			BlockHash:       registryHash(0xCC),
			ParentBlockHash: registryHash(0xDD),
			GasLimit:        30_000_000,
		},
		BuilderAddr: builder,
		ReceivedAt:  time.Now(),
	}
}

// --- Builder Registration ---

func TestRegistryRegisterNewBuilder(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	addr := registryAddr(0x01)
	profile := bm.RegisterBuilder(addr)

	if profile == nil {
		t.Fatal("RegisterBuilder returned nil")
	}
	if profile.Address != addr {
		t.Errorf("address = %s, want %s", profile.Address.Hex(), addr.Hex())
	}
	if profile.Score != 50.0 {
		t.Errorf("initial score = %f, want 50.0", profile.Score)
	}
	if profile.Banned {
		t.Error("new builder should not be banned")
	}
	if bm.BuilderCount() != 1 {
		t.Errorf("BuilderCount = %d, want 1", bm.BuilderCount())
	}
}

func TestRegistryRegisterIdempotent(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	addr := registryAddr(0x01)

	p1 := bm.RegisterBuilder(addr)
	p2 := bm.RegisterBuilder(addr)

	// Should return the same profile, not create a duplicate.
	if p1.Address != p2.Address {
		t.Error("repeated RegisterBuilder should return same profile")
	}
	if bm.BuilderCount() != 1 {
		t.Errorf("BuilderCount = %d, want 1 after double register", bm.BuilderCount())
	}
}

func TestRegistryRegisterMultipleBuilders(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())

	for i := byte(1); i <= 5; i++ {
		bm.RegisterBuilder(registryAddr(i))
	}

	if bm.BuilderCount() != 5 {
		t.Errorf("BuilderCount = %d, want 5", bm.BuilderCount())
	}
}

// --- Deregistration / Banning ---

func TestRegistryBanAndUnban(t *testing.T) {
	cfg := DefaultBuilderMarketConfig()
	cfg.MaxConsecutiveMisses = 2
	bm := NewBuilderMarket(cfg)
	addr := registryAddr(0x01)
	bm.RegisterBuilder(addr)

	// Two consecutive misses should ban.
	bm.RecordMiss(addr)
	bm.RecordMiss(addr)

	profile, _ := bm.GetBuilderProfile(addr)
	if !profile.Banned {
		t.Fatal("builder should be banned after 2 misses")
	}

	// Banned builder's bids should be rejected.
	bid := registryBid(100, 5000, addr)
	err := bm.ValidateBid(bid)
	if !errors.Is(err, ErrMarketBuilderBanned) {
		t.Errorf("expected ErrMarketBuilderBanned, got %v", err)
	}

	// Unban.
	if err := bm.UnbanBuilder(addr); err != nil {
		t.Fatalf("UnbanBuilder: %v", err)
	}
	profile, _ = bm.GetBuilderProfile(addr)
	if profile.Banned {
		t.Error("builder should be unbanned")
	}
	if profile.ConsecutiveMisses != 0 {
		t.Errorf("consecutive misses = %d, want 0 after unban", profile.ConsecutiveMisses)
	}

	// After unban, bids should be accepted again.
	if err := bm.ValidateBid(bid); err != nil {
		t.Errorf("bid after unban should be valid: %v", err)
	}
}

func TestRegistryDeliveryResetsConsecutiveMisses(t *testing.T) {
	cfg := DefaultBuilderMarketConfig()
	cfg.MaxConsecutiveMisses = 5 // high to avoid banning
	bm := NewBuilderMarket(cfg)
	addr := registryAddr(0x01)
	bm.RegisterBuilder(addr)

	bm.RecordMiss(addr)
	bm.RecordMiss(addr)

	profile, _ := bm.GetBuilderProfile(addr)
	if profile.ConsecutiveMisses != 2 {
		t.Fatalf("consecutive misses = %d, want 2", profile.ConsecutiveMisses)
	}

	// Delivery should reset streak.
	bm.RecordDelivery(addr)
	profile, _ = bm.GetBuilderProfile(addr)
	if profile.ConsecutiveMisses != 0 {
		t.Errorf("consecutive misses after delivery = %d, want 0", profile.ConsecutiveMisses)
	}
	if profile.TotalDeliveries != 1 {
		t.Errorf("TotalDeliveries = %d, want 1", profile.TotalDeliveries)
	}
}

// --- Collateral tracking (score as proxy for reputation) ---

func TestRegistryScoreDecayOnMiss(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	addr := registryAddr(0x01)
	bm.RegisterBuilder(addr)

	initialScore, _ := bm.ScoreBuilder(addr)

	// Simulate wins so the score calculation is active.
	bm.mu.Lock()
	bm.builders[addr].TotalWins = 5
	bm.builders[addr].TotalDeliveries = 3
	bm.mu.Unlock()

	bm.RecordMiss(addr)

	afterMiss, _ := bm.ScoreBuilder(addr)
	if afterMiss >= initialScore {
		t.Errorf("score should decrease after miss: before=%f, after=%f", initialScore, afterMiss)
	}
}

func TestRegistryScoreImprovesOnDelivery(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	addr := registryAddr(0x01)
	bm.RegisterBuilder(addr)

	// Give a low initial score by recording misses.
	bm.mu.Lock()
	bm.builders[addr].TotalWins = 5
	bm.builders[addr].TotalDeliveries = 1
	bm.builders[addr].TotalMisses = 3
	bm.builders[addr].Score = 20.0
	bm.mu.Unlock()

	scoreBefore, _ := bm.ScoreBuilder(addr)

	bm.RecordDelivery(addr)

	scoreAfter, _ := bm.ScoreBuilder(addr)
	if scoreAfter < scoreBefore {
		t.Errorf("score should improve after delivery: before=%f, after=%f", scoreBefore, scoreAfter)
	}
}

// --- Builder selection: Vickrey auction ---

func TestRegistryVickreyAuctionSecondPrice(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())

	addrs := []types.Address{registryAddr(0x01), registryAddr(0x02), registryAddr(0x03)}
	for _, a := range addrs {
		bm.RegisterBuilder(a)
	}

	bm.SubmitBid(registryBid(100, 1000, addrs[0]))
	bm.SubmitBid(registryBid(100, 3000, addrs[1]))
	bm.SubmitBid(registryBid(100, 2000, addrs[2]))

	winner, price, err := bm.SelectWinner(100)
	if err != nil {
		t.Fatalf("SelectWinner: %v", err)
	}
	if winner.Bid.Value != 3000 {
		t.Errorf("winner value = %d, want 3000", winner.Bid.Value)
	}
	// Vickrey: winner pays second-highest price.
	if price != 2000 {
		t.Errorf("clearing price = %d, want 2000 (second-price)", price)
	}
}

func TestRegistryAuctionSingleBidPaysReserve(t *testing.T) {
	cfg := DefaultBuilderMarketConfig()
	cfg.ReservePrice = 500
	bm := NewBuilderMarket(cfg)
	addr := registryAddr(0x01)
	bm.RegisterBuilder(addr)
	bm.SubmitBid(registryBid(100, 5000, addr))

	_, price, err := bm.SelectWinner(100)
	if err != nil {
		t.Fatalf("SelectWinner: %v", err)
	}
	if price != 500 {
		t.Errorf("single-bid price = %d, want reserve 500", price)
	}
}

func TestRegistryWinnerUpdatesProfile(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	addr := registryAddr(0x01)
	bm.RegisterBuilder(addr)
	bm.SubmitBid(registryBid(100, 5000, addr))

	bm.SelectWinner(100)

	profile, _ := bm.GetBuilderProfile(addr)
	if profile.TotalWins != 1 {
		t.Errorf("TotalWins = %d, want 1", profile.TotalWins)
	}
}

// --- Bid validation table-driven ---

func TestRegistryBidValidationTable(t *testing.T) {
	cfg := DefaultBuilderMarketConfig()
	cfg.ReservePrice = 100
	bm := NewBuilderMarket(cfg)

	tests := []struct {
		name    string
		bid     *MarketBid
		wantErr error
	}{
		{
			name:    "nil bid",
			bid:     nil,
			wantErr: ErrMarketNilBid,
		},
		{
			name: "zero value",
			bid: &MarketBid{
				Bid: BuilderBid{Slot: 1, Value: 0, BlockHash: registryHash(0xAA), ParentBlockHash: registryHash(0xBB)},
			},
			wantErr: ErrMarketZeroValue,
		},
		{
			name: "zero slot",
			bid: &MarketBid{
				Bid: BuilderBid{Slot: 0, Value: 500, BlockHash: registryHash(0xAA), ParentBlockHash: registryHash(0xBB)},
			},
			wantErr: ErrMarketZeroSlot,
		},
		{
			name: "empty block hash",
			bid: &MarketBid{
				Bid: BuilderBid{Slot: 1, Value: 500, BlockHash: types.Hash{}, ParentBlockHash: registryHash(0xBB)},
			},
			wantErr: ErrMarketEmptyBlockHash,
		},
		{
			name: "empty parent hash",
			bid: &MarketBid{
				Bid: BuilderBid{Slot: 1, Value: 500, BlockHash: registryHash(0xAA), ParentBlockHash: types.Hash{}},
			},
			wantErr: ErrMarketEmptyParentHash,
		},
		{
			name: "below reserve price",
			bid: &MarketBid{
				Bid: BuilderBid{Slot: 1, Value: 50, BlockHash: registryHash(0xAA), ParentBlockHash: registryHash(0xBB)},
			},
			wantErr: ErrMarketBidTooLow,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := bm.ValidateBid(tc.bid)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// --- Concurrent registration and bidding ---

func TestRegistryConcurrentRegistrationAndBidding(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())

	var wg sync.WaitGroup
	for i := byte(0); i < 20; i++ {
		wg.Add(1)
		go func(b byte) {
			defer wg.Done()
			addr := registryAddr(b)
			bm.RegisterBuilder(addr)
			bm.SubmitBid(registryBid(100, uint64(b+1)*100, addr))
		}(i)
	}
	wg.Wait()

	if bm.BuilderCount() != 20 {
		t.Errorf("BuilderCount = %d, want 20", bm.BuilderCount())
	}
	if bm.BidCount(100) != 20 {
		t.Errorf("BidCount(100) = %d, want 20", bm.BidCount(100))
	}
}

// --- Pruning ---

func TestRegistryPruneRemovesOldSlots(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	addr := registryAddr(0x01)
	bm.RegisterBuilder(addr)

	for slot := uint64(10); slot <= 20; slot++ {
		bm.SubmitBid(registryBid(slot, 1000, addr))
	}

	pruned := bm.PruneBefore(15)
	if pruned != 5 {
		t.Errorf("pruned = %d, want 5", pruned)
	}

	// Slots 10-14 should be gone.
	for slot := uint64(10); slot < 15; slot++ {
		if bm.BidCount(slot) != 0 {
			t.Errorf("slot %d should be pruned", slot)
		}
	}
	// Slots 15-20 should remain.
	for slot := uint64(15); slot <= 20; slot++ {
		if bm.BidCount(slot) != 1 {
			t.Errorf("slot %d should have 1 bid", slot)
		}
	}
}

// --- GetBids returns a defensive copy ---

func TestRegistryGetBidsDefensiveCopy(t *testing.T) {
	bm := NewBuilderMarket(DefaultBuilderMarketConfig())
	addr := registryAddr(0x01)
	bm.RegisterBuilder(addr)
	bm.SubmitBid(registryBid(100, 1000, addr))

	bids := bm.GetBids(100)
	if len(bids) != 1 {
		t.Fatalf("expected 1 bid, got %d", len(bids))
	}

	// Mutate the returned slice.
	bids[0] = nil

	// Original should be unchanged.
	bids2 := bm.GetBids(100)
	if bids2[0] == nil {
		t.Error("GetBids should return a defensive copy")
	}
}
