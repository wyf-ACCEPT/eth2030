package vm

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- CreateLongFuture tests ---

func TestCreateLongFuture(t *testing.T) {
	market := NewFuturesMarketLong()

	buyer := types.Address{0x01}
	seller := types.Address{0x02}
	strike := big.NewInt(50_000_000_000)

	future, err := market.CreateLongFuture(buyer, seller, strike, 100, 200, 500_000)
	if err != nil {
		t.Fatalf("CreateLongFuture failed: %v", err)
	}
	if future == nil {
		t.Fatal("expected non-nil future")
	}
	if future.Buyer != buyer {
		t.Error("buyer mismatch")
	}
	if future.Seller != seller {
		t.Error("seller mismatch")
	}
	if future.StrikePrice.Cmp(strike) != 0 {
		t.Error("strike price mismatch")
	}
	if future.ExpiryEpoch != 200 {
		t.Errorf("expected expiry epoch 200, got %d", future.ExpiryEpoch)
	}
	if future.Amount != 500_000 {
		t.Errorf("expected amount 500000, got %d", future.Amount)
	}
	if future.Settled {
		t.Error("new future should not be settled")
	}
	if future.CreatedEpoch != 100 {
		t.Errorf("expected created epoch 100, got %d", future.CreatedEpoch)
	}
	if market.OpenCount() != 1 {
		t.Errorf("expected 1 open, got %d", market.OpenCount())
	}
}

func TestCreateLongFutureZeroAmount(t *testing.T) {
	market := NewFuturesMarketLong()
	_, err := market.CreateLongFuture(
		types.Address{0x01}, types.Address{0x02},
		big.NewInt(100), 10, 20, 0,
	)
	if err != ErrLongFutureZeroAmount {
		t.Errorf("expected ErrLongFutureZeroAmount, got %v", err)
	}
}

func TestCreateLongFutureZeroStrike(t *testing.T) {
	market := NewFuturesMarketLong()
	_, err := market.CreateLongFuture(
		types.Address{0x01}, types.Address{0x02},
		big.NewInt(0), 10, 20, 1000,
	)
	if err != ErrLongFutureZeroStrike {
		t.Errorf("expected ErrLongFutureZeroStrike, got %v", err)
	}

	_, err = market.CreateLongFuture(
		types.Address{0x01}, types.Address{0x02},
		nil, 10, 20, 1000,
	)
	if err != ErrLongFutureZeroStrike {
		t.Errorf("expected ErrLongFutureZeroStrike for nil, got %v", err)
	}
}

func TestCreateLongFutureInvalidExpiry(t *testing.T) {
	market := NewFuturesMarketLong()

	// Expiry equal to created.
	_, err := market.CreateLongFuture(
		types.Address{0x01}, types.Address{0x02},
		big.NewInt(100), 10, 10, 1000,
	)
	if err != ErrLongFutureInvalidExpiry {
		t.Errorf("expected ErrLongFutureInvalidExpiry, got %v", err)
	}

	// Expiry before created.
	_, err = market.CreateLongFuture(
		types.Address{0x01}, types.Address{0x02},
		big.NewInt(100), 10, 5, 1000,
	)
	if err != ErrLongFutureInvalidExpiry {
		t.Errorf("expected ErrLongFutureInvalidExpiry, got %v", err)
	}
}

func TestCreateLongFutureTooLong(t *testing.T) {
	market := NewFuturesMarketLong()
	_, err := market.CreateLongFuture(
		types.Address{0x01}, types.Address{0x02},
		big.NewInt(100), 0, MaxLongFutureEpochs+1, 1000,
	)
	if err != ErrLongFutureTooLong {
		t.Errorf("expected ErrLongFutureTooLong, got %v", err)
	}
}

func TestCreateLongFutureMaxEpochs(t *testing.T) {
	market := NewFuturesMarketLong()
	// Exactly MaxLongFutureEpochs should work.
	_, err := market.CreateLongFuture(
		types.Address{0x01}, types.Address{0x02},
		big.NewInt(100), 0, MaxLongFutureEpochs, 1000,
	)
	if err != nil {
		t.Fatalf("expected success for exactly max epochs, got %v", err)
	}
}

func TestCreateLongFutureUniqueIDs(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	f1, _ := market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)
	f2, _ := market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)

	if f1.ID == f2.ID {
		t.Error("expected unique IDs for different futures")
	}
}

// --- SettleLongFuture tests ---

func TestSettleLongFutureBuyerWins(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	future, _ := market.CreateLongFuture(buyer, seller, big.NewInt(50_000_000_000), 10, 100, 100_000)

	// Actual price above strike -> buyer wins.
	settlement, err := market.SettleLongFuture(future.ID, 100, big.NewInt(60_000_000_000))
	if err != nil {
		t.Fatalf("SettleLongFuture failed: %v", err)
	}
	if settlement.Winner != buyer {
		t.Error("expected buyer to win when actual > strike")
	}
	// Payout = |60G - 50G| * 100_000 = 10G * 100_000
	expectedPayout := new(big.Int).Mul(big.NewInt(10_000_000_000), big.NewInt(100_000))
	if settlement.Payout.Cmp(expectedPayout) != 0 {
		t.Errorf("expected payout %s, got %s", expectedPayout, settlement.Payout)
	}
	if settlement.SettledEpoch != 100 {
		t.Errorf("expected settled epoch 100, got %d", settlement.SettledEpoch)
	}
}

func TestSettleLongFutureSellerWins(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	future, _ := market.CreateLongFuture(buyer, seller, big.NewInt(50_000_000_000), 10, 100, 100_000)

	// Actual price below strike -> seller wins.
	settlement, err := market.SettleLongFuture(future.ID, 100, big.NewInt(40_000_000_000))
	if err != nil {
		t.Fatalf("SettleLongFuture failed: %v", err)
	}
	if settlement.Winner != seller {
		t.Error("expected seller to win when actual < strike")
	}
}

func TestSettleLongFutureEqualPrice(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	future, _ := market.CreateLongFuture(buyer, seller, big.NewInt(50_000_000_000), 10, 100, 100_000)

	// Actual equals strike -> payout is 0, seller wins by convention.
	settlement, err := market.SettleLongFuture(future.ID, 100, big.NewInt(50_000_000_000))
	if err != nil {
		t.Fatalf("SettleLongFuture failed: %v", err)
	}
	if settlement.Winner != seller {
		t.Error("expected seller to win when actual == strike")
	}
	if settlement.Payout.Sign() != 0 {
		t.Errorf("expected 0 payout, got %s", settlement.Payout)
	}
}

func TestSettleLongFutureNotFound(t *testing.T) {
	market := NewFuturesMarketLong()
	_, err := market.SettleLongFuture(types.Hash{0xFF}, 100, big.NewInt(100))
	if err != ErrLongFutureNotFound {
		t.Errorf("expected ErrLongFutureNotFound, got %v", err)
	}
}

func TestSettleLongFutureAlreadySettled(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	future, _ := market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)
	market.SettleLongFuture(future.ID, 100, big.NewInt(200))

	_, err := market.SettleLongFuture(future.ID, 101, big.NewInt(300))
	if err != ErrLongFutureAlreadySettled {
		t.Errorf("expected ErrLongFutureAlreadySettled, got %v", err)
	}
}

func TestSettleLongFutureNotExpired(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	future, _ := market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)

	// Current epoch 50 < expiry 100 -> not expired yet.
	_, err := market.SettleLongFuture(future.ID, 50, big.NewInt(200))
	if err != ErrLongFutureNotExpired {
		t.Errorf("expected ErrLongFutureNotExpired, got %v", err)
	}
}

// --- ExpireLongFutures tests ---

func TestExpireLongFutures(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 50, 1000)
	market.CreateLongFuture(buyer, seller, big.NewInt(200), 10, 100, 2000)
	market.CreateLongFuture(buyer, seller, big.NewInt(300), 10, 150, 3000)

	// Expire at epoch 75: first future expires.
	count := market.ExpireLongFutures(75)
	if count != 1 {
		t.Errorf("expected 1 expired, got %d", count)
	}
	if market.OpenCount() != 2 {
		t.Errorf("expected 2 open, got %d", market.OpenCount())
	}
	if market.SettledCount() != 1 {
		t.Errorf("expected 1 settled, got %d", market.SettledCount())
	}

	// Expire at epoch 100: second future also expires.
	count = market.ExpireLongFutures(100)
	if count != 1 {
		t.Errorf("expected 1 more expired, got %d", count)
	}
	if market.OpenCount() != 1 {
		t.Errorf("expected 1 open, got %d", market.OpenCount())
	}

	// Expire at epoch 200: third future also expires.
	count = market.ExpireLongFutures(200)
	if count != 1 {
		t.Errorf("expected 1 more expired, got %d", count)
	}
	if market.OpenCount() != 0 {
		t.Errorf("expected 0 open, got %d", market.OpenCount())
	}
	if market.SettledCount() != 3 {
		t.Errorf("expected 3 settled, got %d", market.SettledCount())
	}
}

func TestExpireLongFuturesSellerWins(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	future, _ := market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 50, 1000)
	market.ExpireLongFutures(50)

	settlement := market.GetSettlement(future.ID)
	if settlement == nil {
		t.Fatal("expected settlement after expiry")
	}
	if settlement.Winner != seller {
		t.Error("expected seller to win on expiry")
	}
	// Payout = strikePrice * amount = 100 * 1000 = 100000
	expectedPayout := new(big.Int).Mul(big.NewInt(100), big.NewInt(1000))
	if settlement.Payout.Cmp(expectedPayout) != 0 {
		t.Errorf("expected payout %s, got %s", expectedPayout, settlement.Payout)
	}
}

func TestExpireLongFuturesNoneExpired(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)

	count := market.ExpireLongFutures(50)
	if count != 0 {
		t.Errorf("expected 0 expired, got %d", count)
	}
}

// --- GetOpenInterest tests ---

func TestGetOpenInterest(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)
	market.CreateLongFuture(buyer, seller, big.NewInt(200), 10, 200, 2000)

	oi := market.GetOpenInterest()
	expected := big.NewInt(3000)
	if oi.Cmp(expected) != 0 {
		t.Errorf("expected open interest %s, got %s", expected, oi)
	}
}

func TestGetOpenInterestExcludesSettled(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	f1, _ := market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)
	market.CreateLongFuture(buyer, seller, big.NewInt(200), 10, 200, 2000)

	// Settle first future.
	market.SettleLongFuture(f1.ID, 100, big.NewInt(150))

	oi := market.GetOpenInterest()
	expected := big.NewInt(2000)
	if oi.Cmp(expected) != 0 {
		t.Errorf("expected open interest %s, got %s", expected, oi)
	}
}

func TestGetOpenInterestEmpty(t *testing.T) {
	market := NewFuturesMarketLong()
	oi := market.GetOpenInterest()
	if oi.Sign() != 0 {
		t.Errorf("expected 0 open interest, got %s", oi)
	}
}

// --- GetMarketDepth tests ---

func TestGetMarketDepthEmpty(t *testing.T) {
	market := NewFuturesMarketLong()
	depth := market.GetMarketDepth()
	if depth.BuyOrders != 0 || depth.SellOrders != 0 {
		t.Errorf("expected 0 orders, got buy=%d sell=%d", depth.BuyOrders, depth.SellOrders)
	}
	if depth.SpreadBps != 0 {
		t.Errorf("expected 0 spread, got %d", depth.SpreadBps)
	}
	if depth.MidPrice.Sign() != 0 {
		t.Errorf("expected 0 mid price, got %s", depth.MidPrice)
	}
}

func TestGetMarketDepthSingleFuture(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)

	depth := market.GetMarketDepth()
	// With a single price, min == max, spread = 0, midPrice = 100.
	if depth.SpreadBps != 0 {
		t.Errorf("expected 0 spread for single future, got %d", depth.SpreadBps)
	}
	if depth.MidPrice.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("expected mid price 100, got %s", depth.MidPrice)
	}
	// Single price <= midPrice -> counts as buy order.
	if depth.BuyOrders != 1 {
		t.Errorf("expected 1 buy order, got %d", depth.BuyOrders)
	}
}

func TestGetMarketDepthMultipleFutures(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)
	market.CreateLongFuture(buyer, seller, big.NewInt(200), 10, 100, 2000)
	market.CreateLongFuture(buyer, seller, big.NewInt(300), 10, 100, 3000)

	depth := market.GetMarketDepth()
	// min=100, max=300, mid=200
	if depth.MidPrice.Cmp(big.NewInt(200)) != 0 {
		t.Errorf("expected mid price 200, got %s", depth.MidPrice)
	}
	// Spread = (300-100)*10000/200 = 10000
	if depth.SpreadBps != 10000 {
		t.Errorf("expected spread 10000 bps, got %d", depth.SpreadBps)
	}
	// Buy orders (strike <= mid): 100, 200 -> 2
	// Sell orders (strike > mid): 300 -> 1
	if depth.BuyOrders != 2 {
		t.Errorf("expected 2 buy orders, got %d", depth.BuyOrders)
	}
	if depth.SellOrders != 1 {
		t.Errorf("expected 1 sell order, got %d", depth.SellOrders)
	}
}

func TestGetMarketDepthExcludesSettled(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	f1, _ := market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)
	market.CreateLongFuture(buyer, seller, big.NewInt(200), 10, 200, 2000)

	// Settle f1.
	market.SettleLongFuture(f1.ID, 100, big.NewInt(150))

	depth := market.GetMarketDepth()
	// Only f2 remains open.
	totalOrders := depth.BuyOrders + depth.SellOrders
	if totalOrders != 1 {
		t.Errorf("expected 1 total order, got %d", totalOrders)
	}
}

// --- Getter tests ---

func TestGetFuture(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	future, _ := market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)
	got := market.GetFuture(future.ID)
	if got == nil {
		t.Fatal("expected non-nil future")
	}
	if got.ID != future.ID {
		t.Error("future ID mismatch")
	}
}

func TestGetFutureNotFound(t *testing.T) {
	market := NewFuturesMarketLong()
	got := market.GetFuture(types.Hash{0xFF})
	if got != nil {
		t.Error("expected nil for unknown ID")
	}
}

func TestGetSettlement(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	future, _ := market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)
	market.SettleLongFuture(future.ID, 100, big.NewInt(200))

	settlement := market.GetSettlement(future.ID)
	if settlement == nil {
		t.Fatal("expected non-nil settlement")
	}
	if settlement.FutureID != future.ID {
		t.Error("settlement ID mismatch")
	}
}

func TestGetSettlementNotFound(t *testing.T) {
	market := NewFuturesMarketLong()
	settlement := market.GetSettlement(types.Hash{0xFF})
	if settlement != nil {
		t.Error("expected nil for unknown settlement")
	}
}

// --- Count tests ---

func TestOpenAndSettledCounts(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	if market.OpenCount() != 0 {
		t.Errorf("expected 0 open, got %d", market.OpenCount())
	}
	if market.SettledCount() != 0 {
		t.Errorf("expected 0 settled, got %d", market.SettledCount())
	}

	f1, _ := market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 100, 1000)
	market.CreateLongFuture(buyer, seller, big.NewInt(200), 10, 200, 2000)

	if market.OpenCount() != 2 {
		t.Errorf("expected 2 open, got %d", market.OpenCount())
	}

	market.SettleLongFuture(f1.ID, 100, big.NewInt(150))
	if market.OpenCount() != 1 {
		t.Errorf("expected 1 open after settlement, got %d", market.OpenCount())
	}
	if market.SettledCount() != 1 {
		t.Errorf("expected 1 settled, got %d", market.SettledCount())
	}
}

// --- Thread safety test ---

func TestFuturesMarketLongThreadSafety(t *testing.T) {
	market := NewFuturesMarketLong()
	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			buyer := types.Address{byte(n)}
			seller := types.Address{byte(n + 100)}
			_, err := market.CreateLongFuture(
				buyer, seller,
				big.NewInt(int64(100+n)),
				uint64(n), uint64(n+100), uint64(1000+n),
			)
			if err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	if market.OpenCount() != 10 {
		t.Errorf("expected 10 open, got %d", market.OpenCount())
	}
}

// --- Multiple operations test ---

func TestFullLifecycle(t *testing.T) {
	market := NewFuturesMarketLong()
	buyer := types.Address{0x01}
	seller := types.Address{0x02}

	// Create 3 futures.
	f1, _ := market.CreateLongFuture(buyer, seller, big.NewInt(100), 10, 50, 1000)
	f2, _ := market.CreateLongFuture(buyer, seller, big.NewInt(200), 10, 100, 2000)
	f3, _ := market.CreateLongFuture(buyer, seller, big.NewInt(300), 10, 150, 3000)

	if market.OpenCount() != 3 {
		t.Fatalf("expected 3 open, got %d", market.OpenCount())
	}

	// Settle f1 (buyer wins).
	s1, err := market.SettleLongFuture(f1.ID, 50, big.NewInt(150))
	if err != nil {
		t.Fatalf("settle f1: %v", err)
	}
	if s1.Winner != buyer {
		t.Error("expected buyer to win f1")
	}

	// Expire f2.
	market.ExpireLongFutures(100)
	s2 := market.GetSettlement(f2.ID)
	if s2 == nil {
		t.Fatal("expected f2 settlement after expiry")
	}
	if s2.Winner != seller {
		t.Error("expected seller to win expired f2")
	}

	// f3 still open.
	if market.OpenCount() != 1 {
		t.Errorf("expected 1 open, got %d", market.OpenCount())
	}
	if market.SettledCount() != 2 {
		t.Errorf("expected 2 settled, got %d", market.SettledCount())
	}

	// Verify open interest is only f3.
	oi := market.GetOpenInterest()
	if oi.Cmp(big.NewInt(3000)) != 0 {
		t.Errorf("expected OI 3000, got %s", oi)
	}

	// Check f3 is retrievable.
	got := market.GetFuture(f3.ID)
	if got == nil {
		t.Fatal("expected to find f3")
	}
	if got.Settled {
		t.Error("f3 should not be settled yet")
	}
}

// --- MaxLongFutureEpochs constant ---

func TestMaxLongFutureEpochs(t *testing.T) {
	if MaxLongFutureEpochs != 365 {
		t.Errorf("expected MaxLongFutureEpochs=365, got %d", MaxLongFutureEpochs)
	}
}
