package das

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func newTestMarket() *BlobFuturesMarket {
	return NewBlobFuturesMarket(100)
}

func TestBlobFuturesMarketCreate(t *testing.T) {
	m := newTestMarket()
	buyer := types.Address{0xAA}
	seller := types.Address{0xBB}
	committed := types.Hash{0x01}
	price := big.NewInt(1_000_000_000)

	f, err := m.CreateFuture(200, 0, committed, 250, price, buyer, seller)
	if err != nil {
		t.Fatalf("CreateFuture: %v", err)
	}
	if f.Slot != 200 {
		t.Errorf("Slot = %d, want 200", f.Slot)
	}
	if f.BlobIndex != 0 {
		t.Errorf("BlobIndex = %d, want 0", f.BlobIndex)
	}
	if f.CommittedHash != committed {
		t.Error("CommittedHash mismatch")
	}
	if f.Expiry != 250 {
		t.Errorf("Expiry = %d, want 250", f.Expiry)
	}
	if f.Price.Cmp(price) != 0 {
		t.Errorf("Price = %v, want %v", f.Price, price)
	}
	if f.Buyer != buyer {
		t.Error("Buyer mismatch")
	}
	if f.Seller != seller {
		t.Error("Seller mismatch")
	}
	if f.Status != FutureActive {
		t.Errorf("Status = %d, want FutureActive", f.Status)
	}
	if f.FType != ShortDatedFuture {
		t.Errorf("FType = %d, want ShortDatedFuture", f.FType)
	}
}

func TestBlobFuturesMarketCreateLongDated(t *testing.T) {
	m := newTestMarket()
	// Expiry at slot 100 + 1000 = slot 1100, which is > ShortDatedMaxSlots from current.
	f, err := m.CreateFuture(500, 1, types.Hash{0x02}, 1100, big.NewInt(5000), types.Address{0xAA}, types.Address{0xBB})
	if err != nil {
		t.Fatalf("CreateFuture: %v", err)
	}
	if f.FType != LongDatedFuture {
		t.Errorf("FType = %d, want LongDatedFuture", f.FType)
	}
}

func TestBlobFuturesMarketCreateInvalidSlot(t *testing.T) {
	m := newTestMarket()
	// Slot in the past.
	_, err := m.CreateFuture(50, 0, types.Hash{}, 200, big.NewInt(100), types.Address{}, types.Address{})
	if err != ErrBlobFutureInvalidSlot {
		t.Fatalf("expected ErrBlobFutureInvalidSlot, got %v", err)
	}
	// Slot at current.
	_, err = m.CreateFuture(100, 0, types.Hash{}, 200, big.NewInt(100), types.Address{}, types.Address{})
	if err != ErrBlobFutureInvalidSlot {
		t.Fatalf("expected ErrBlobFutureInvalidSlot for current slot, got %v", err)
	}
}

func TestBlobFuturesMarketCreateBadBlobIndex(t *testing.T) {
	m := newTestMarket()
	_, err := m.CreateFuture(200, MaxBlobCommitmentsPerBlock, types.Hash{}, 300, big.NewInt(100), types.Address{}, types.Address{})
	if err != ErrBlobFutureBadIndex {
		t.Fatalf("expected ErrBlobFutureBadIndex, got %v", err)
	}
}

func TestBlobFuturesMarketCreateBadPrice(t *testing.T) {
	m := newTestMarket()

	tests := []struct {
		name  string
		price *big.Int
	}{
		{"nil", nil},
		{"zero", big.NewInt(0)},
		{"negative", big.NewInt(-1)},
	}
	for _, tt := range tests {
		_, err := m.CreateFuture(200, 0, types.Hash{}, 300, tt.price, types.Address{}, types.Address{})
		if err != ErrBlobFutureBadPrice {
			t.Errorf("%s: expected ErrBlobFutureBadPrice, got %v", tt.name, err)
		}
	}
}

func TestBlobFuturesMarketCreateBadExpiry(t *testing.T) {
	m := newTestMarket()
	// Expiry before slot.
	_, err := m.CreateFuture(200, 0, types.Hash{}, 150, big.NewInt(100), types.Address{}, types.Address{})
	if err != ErrBlobFutureBadExpiry {
		t.Fatalf("expected ErrBlobFutureBadExpiry, got %v", err)
	}
	// Expiry exceeds long-dated max.
	_, err = m.CreateFuture(200, 0, types.Hash{}, 100+LongDatedMaxSlots+1, big.NewInt(100), types.Address{}, types.Address{})
	if err != ErrBlobFutureBadExpiry {
		t.Fatalf("expected ErrBlobFutureBadExpiry for too far out, got %v", err)
	}
}

func TestBlobFuturesSettleFullMatch(t *testing.T) {
	m := newTestMarket()
	committed := types.Hash{0x42, 0x43}
	price := big.NewInt(1_000_000_000)

	f, _ := m.CreateFuture(200, 0, committed, 250, price, types.Address{0xAA}, types.Address{0xBB})

	// Settle with matching hash.
	payout, err := m.SettleFuture(f.ID, committed)
	if err != nil {
		t.Fatalf("SettleFuture: %v", err)
	}

	expected := new(big.Int).Mul(price, big.NewInt(2))
	if payout.Cmp(expected) != 0 {
		t.Errorf("payout = %v, want %v", payout, expected)
	}
	if f.Status != FutureSettled {
		t.Errorf("Status = %d, want FutureSettled", f.Status)
	}
}

func TestBlobFuturesSettleNoMatch(t *testing.T) {
	m := newTestMarket()
	committed := types.Hash{0x42}
	actual := types.Hash{0xFF}
	price := big.NewInt(1_000_000_000)

	f, _ := m.CreateFuture(200, 0, committed, 250, price, types.Address{0xAA}, types.Address{0xBB})

	payout, err := m.SettleFuture(f.ID, actual)
	if err != nil {
		t.Fatalf("SettleFuture: %v", err)
	}

	if payout.Sign() != 0 {
		t.Errorf("payout = %v, want 0 (no match)", payout)
	}
}

func TestBlobFuturesSettlePartialMatch(t *testing.T) {
	m := newTestMarket()
	// Create hashes that match in first 16 bytes but differ after.
	var committed, actual types.Hash
	for i := 0; i < 16; i++ {
		committed[i] = byte(i + 1)
		actual[i] = byte(i + 1)
	}
	committed[16] = 0xAA
	actual[16] = 0xBB

	price := big.NewInt(1_000_000_000)
	f, _ := m.CreateFuture(200, 0, committed, 250, price, types.Address{0xAA}, types.Address{0xBB})

	payout, err := m.SettleFuture(f.ID, actual)
	if err != nil {
		t.Fatalf("SettleFuture: %v", err)
	}

	// Partial match: 1.5x price.
	expected := new(big.Int).Mul(price, big.NewInt(3))
	expected.Div(expected, big.NewInt(2))
	if payout.Cmp(expected) != 0 {
		t.Errorf("payout = %v, want %v (1.5x)", payout, expected)
	}
}

func TestBlobFuturesSettleNotFound(t *testing.T) {
	m := newTestMarket()
	_, err := m.SettleFuture(types.Hash{0xFF}, types.Hash{})
	if err != ErrBlobFutureNotFound {
		t.Fatalf("expected ErrBlobFutureNotFound, got %v", err)
	}
}

func TestBlobFuturesSettleAlreadySettled(t *testing.T) {
	m := newTestMarket()
	f, _ := m.CreateFuture(200, 0, types.Hash{0x01}, 250, big.NewInt(100), types.Address{}, types.Address{})
	m.SettleFuture(f.ID, types.Hash{0x01})

	_, err := m.SettleFuture(f.ID, types.Hash{0x01})
	if err != ErrBlobFutureNotActive {
		t.Fatalf("expected ErrBlobFutureNotActive, got %v", err)
	}
}

func TestBlobFuturesExpire(t *testing.T) {
	m := newTestMarket()

	m.CreateFuture(150, 0, types.Hash{0x01}, 200, big.NewInt(100), types.Address{0x01}, types.Address{0x02})
	m.CreateFuture(200, 1, types.Hash{0x02}, 250, big.NewInt(200), types.Address{0x03}, types.Address{0x04})
	m.CreateFuture(300, 2, types.Hash{0x03}, 350, big.NewInt(300), types.Address{0x05}, types.Address{0x06})

	if m.ActiveFutureCount() != 3 {
		t.Fatalf("ActiveFutureCount = %d, want 3", m.ActiveFutureCount())
	}

	// Expire up to slot 250 -> futures at 200 and 250 expired.
	expired := m.ExpireFutures(250)
	if expired != 2 {
		t.Errorf("expired = %d, want 2", expired)
	}
	if m.ActiveFutureCount() != 1 {
		t.Errorf("ActiveFutureCount = %d, want 1", m.ActiveFutureCount())
	}

	// Expire remaining.
	expired = m.ExpireFutures(400)
	if expired != 1 {
		t.Errorf("expired = %d, want 1", expired)
	}
	if m.ActiveFutureCount() != 0 {
		t.Errorf("ActiveFutureCount = %d, want 0", m.ActiveFutureCount())
	}
}

func TestBlobFuturesExpireAlreadySettled(t *testing.T) {
	m := newTestMarket()

	f, _ := m.CreateFuture(200, 0, types.Hash{0x01}, 250, big.NewInt(100), types.Address{}, types.Address{})
	m.SettleFuture(f.ID, types.Hash{0x01})

	// Expiring should not count already-settled futures.
	expired := m.ExpireFutures(300)
	if expired != 0 {
		t.Errorf("expired = %d, want 0 (already settled)", expired)
	}
}

func TestBlobFuturesListActive(t *testing.T) {
	m := newTestMarket()

	m.CreateFuture(200, 0, types.Hash{0x01}, 300, big.NewInt(100), types.Address{0x01}, types.Address{0x02})
	m.CreateFuture(150, 1, types.Hash{0x02}, 200, big.NewInt(200), types.Address{0x03}, types.Address{0x04})
	f3, _ := m.CreateFuture(250, 2, types.Hash{0x03}, 350, big.NewInt(300), types.Address{0x05}, types.Address{0x06})

	// Settle one.
	m.SettleFuture(f3.ID, types.Hash{0x03})

	active := m.ListActiveFutures()
	if len(active) != 2 {
		t.Fatalf("ListActiveFutures returned %d, want 2", len(active))
	}

	// Should be sorted by expiry ascending.
	if active[0].Expiry > active[1].Expiry {
		t.Error("ListActiveFutures not sorted by expiry")
	}
}

func TestBlobFuturesCancelFuture(t *testing.T) {
	m := newTestMarket()
	f, _ := m.CreateFuture(200, 0, types.Hash{0x01}, 250, big.NewInt(100), types.Address{}, types.Address{})

	err := m.CancelFuture(f.ID)
	if err != nil {
		t.Fatalf("CancelFuture: %v", err)
	}
	if f.Status != FutureCancelled {
		t.Errorf("Status = %d, want FutureCancelled", f.Status)
	}

	// Cannot cancel again.
	err = m.CancelFuture(f.ID)
	if err != ErrBlobFutureNotActive {
		t.Fatalf("expected ErrBlobFutureNotActive, got %v", err)
	}
}

func TestBlobFuturesCancelNotFound(t *testing.T) {
	m := newTestMarket()
	err := m.CancelFuture(types.Hash{0xFF})
	if err != ErrBlobFutureNotFound {
		t.Fatalf("expected ErrBlobFutureNotFound, got %v", err)
	}
}

func TestBlobFuturesGetFuture(t *testing.T) {
	m := newTestMarket()
	f, _ := m.CreateFuture(200, 0, types.Hash{0x01}, 250, big.NewInt(100), types.Address{0xAA}, types.Address{0xBB})

	got, err := m.GetFuture(f.ID)
	if err != nil {
		t.Fatalf("GetFuture: %v", err)
	}
	if got.ID != f.ID {
		t.Error("ID mismatch")
	}

	_, err = m.GetFuture(types.Hash{0xFF})
	if err != ErrBlobFutureNotFound {
		t.Fatalf("expected ErrBlobFutureNotFound, got %v", err)
	}
}

func TestBlobFuturesFutureCount(t *testing.T) {
	m := newTestMarket()

	m.CreateFuture(200, 0, types.Hash{0x01}, 250, big.NewInt(100), types.Address{0x01}, types.Address{0x02})
	m.CreateFuture(300, 1, types.Hash{0x02}, 350, big.NewInt(200), types.Address{0x03}, types.Address{0x04})

	if m.FutureCount() != 2 {
		t.Errorf("FutureCount = %d, want 2", m.FutureCount())
	}
}

func TestComputeSettlementPriceDirectly(t *testing.T) {
	price := big.NewInt(1_000_000_000)

	// Full match.
	h := types.Hash{0x01, 0x02}
	payout := ComputeSettlementPrice(h, h, price)
	if payout.Cmp(big.NewInt(2_000_000_000)) != 0 {
		t.Errorf("full match payout = %v, want 2000000000", payout)
	}

	// No match at all.
	payout = ComputeSettlementPrice(types.Hash{0xAA}, types.Hash{0xBB}, price)
	if payout.Sign() != 0 {
		t.Errorf("no match payout = %v, want 0", payout)
	}

	// Partial match (first 16 bytes same).
	var c, a types.Hash
	for i := 0; i < 16; i++ {
		c[i] = 0x42
		a[i] = 0x42
	}
	c[16] = 0x01
	a[16] = 0x02
	payout = ComputeSettlementPrice(c, a, price)
	expected := big.NewInt(1_500_000_000) // 1.5x
	if payout.Cmp(expected) != 0 {
		t.Errorf("partial match payout = %v, want %v", payout, expected)
	}
}

func TestBlobFuturesConcurrency(t *testing.T) {
	m := newTestMarket()
	var wg sync.WaitGroup

	// Concurrent creation.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			slot := uint64(200 + idx)
			m.CreateFuture(slot, uint8(idx%MaxBlobCommitmentsPerBlock), types.Hash{byte(idx)}, slot+100, big.NewInt(int64(idx+1)*100), types.Address{byte(idx)}, types.Address{byte(idx + 50)})
		}(i)
	}
	wg.Wait()

	if m.FutureCount() != 50 {
		t.Errorf("FutureCount = %d, want 50", m.FutureCount())
	}

	// Concurrent reads.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.ListActiveFutures()
			m.ActiveFutureCount()
			m.FutureCount()
		}()
	}
	wg.Wait()
}

func TestSettleBlobFutureMatch(t *testing.T) {
	m := newTestMarket()
	committed := types.Hash{0x42, 0x43}
	price := big.NewInt(1_000_000_000)

	f, _ := m.CreateFuture(200, 0, committed, 250, price, types.Address{0xAA}, types.Address{0xBB})

	payout, winner, err := m.SettleBlobFuture(f.ID, committed)
	if err != nil {
		t.Fatalf("SettleBlobFuture: %v", err)
	}
	if winner != "buyer" {
		t.Errorf("winner = %s, want buyer", winner)
	}
	expected := new(big.Int).Mul(price, big.NewInt(2))
	if payout.Cmp(expected) != 0 {
		t.Errorf("payout = %v, want %v", payout, expected)
	}
}

func TestSettleBlobFutureMismatch(t *testing.T) {
	m := newTestMarket()
	committed := types.Hash{0x42}
	actual := types.Hash{0xFF}
	price := big.NewInt(1_000_000_000)

	f, _ := m.CreateFuture(200, 0, committed, 250, price, types.Address{0xAA}, types.Address{0xBB})

	payout, winner, err := m.SettleBlobFuture(f.ID, actual)
	if err != nil {
		t.Fatalf("SettleBlobFuture: %v", err)
	}
	if winner != "seller" {
		t.Errorf("winner = %s, want seller", winner)
	}
	if payout.Sign() != 0 {
		t.Errorf("payout = %v, want 0", payout)
	}
}

func TestSettleBlobFutureNotFound(t *testing.T) {
	m := newTestMarket()
	_, _, err := m.SettleBlobFuture(types.Hash{0xFF}, types.Hash{})
	if err != ErrBlobFutureNotFound {
		t.Fatalf("expected ErrBlobFutureNotFound, got %v", err)
	}
}

func TestExpireOldFutures(t *testing.T) {
	m := newTestMarket()

	m.CreateFuture(150, 0, types.Hash{0x01}, 200, big.NewInt(100), types.Address{0x01}, types.Address{0x02})
	m.CreateFuture(200, 1, types.Hash{0x02}, 250, big.NewInt(200), types.Address{0x03}, types.Address{0x04})
	m.CreateFuture(300, 2, types.Hash{0x03}, 350, big.NewInt(300), types.Address{0x05}, types.Address{0x06})

	// Expire up to slot 250 -> futures at 200 and 250 expired.
	count, ids := m.ExpireOldFutures(250)
	if count != 2 {
		t.Errorf("expired = %d, want 2", count)
	}
	if len(ids) != 2 {
		t.Errorf("expired IDs len = %d, want 2", len(ids))
	}
	if m.ActiveFutureCount() != 1 {
		t.Errorf("ActiveFutureCount = %d, want 1", m.ActiveFutureCount())
	}

	// Expire remaining.
	count, ids = m.ExpireOldFutures(400)
	if count != 1 {
		t.Errorf("expired = %d, want 1", count)
	}
	if len(ids) != 1 {
		t.Errorf("expired IDs len = %d, want 1", len(ids))
	}
	if m.ActiveFutureCount() != 0 {
		t.Errorf("ActiveFutureCount = %d, want 0", m.ActiveFutureCount())
	}
}

func TestExpireOldFuturesNoActiveToExpire(t *testing.T) {
	m := newTestMarket()
	count, ids := m.ExpireOldFutures(500)
	if count != 0 {
		t.Errorf("empty market: expired = %d, want 0", count)
	}
	if len(ids) != 0 {
		t.Errorf("empty market: expired IDs len = %d, want 0", len(ids))
	}
}

func TestBlobFuturesPriceIsolation(t *testing.T) {
	m := newTestMarket()
	price := big.NewInt(1000)

	f, _ := m.CreateFuture(200, 0, types.Hash{}, 250, price, types.Address{}, types.Address{})

	// Mutating the original price should not affect the stored future.
	price.SetInt64(9999)
	if f.Price.Int64() == 9999 {
		t.Error("future price should be a copy, not reference")
	}
}

func TestValidateFutureContract(t *testing.T) {
	// Valid.
	f := &BlobFutureContract{
		Slot: 200, BlobIndex: 0, Price: big.NewInt(1000),
		Expiry: 300,
	}
	if err := ValidateFutureContract(f, 100); err != nil {
		t.Errorf("valid contract: %v", err)
	}

	// Nil.
	if err := ValidateFutureContract(nil, 100); err == nil {
		t.Error("expected error for nil contract")
	}

	// Slot in the past.
	past := &BlobFutureContract{Slot: 50, Price: big.NewInt(1000), Expiry: 60}
	if err := ValidateFutureContract(past, 100); err == nil {
		t.Error("expected error for past slot")
	}

	// Expiry before slot.
	badExpiry := &BlobFutureContract{Slot: 200, Price: big.NewInt(1000), Expiry: 100}
	if err := ValidateFutureContract(badExpiry, 100); err == nil {
		t.Error("expected error for expiry before slot")
	}
}

func TestFuturesPoolSettleFuture(t *testing.T) {
	t.Run("settles filled orders", func(t *testing.T) {
		pool, err := NewFuturesPool(100, 300)
		if err != nil {
			t.Fatalf("NewFuturesPool: %v", err)
		}

		// Deposit margin and create matching orders.
		buyer := types.Address{0xAA}
		seller := types.Address{0xBB}
		pool.DepositMargin(buyer, big.NewInt(1_000_000))
		pool.DepositMargin(seller, big.NewInt(1_000_000))

		book := pool.GetOrCreateBook(200)
		book.PlaceOrder(OrderSideBuy, buyer, 0, big.NewInt(500), 1, 100)
		book.PlaceOrder(OrderSideSell, seller, 0, big.NewInt(400), 1, 100)

		// Match the orders.
		matched := book.MatchOrders()
		if matched != 1 {
			t.Fatalf("matched = %d, want 1", matched)
		}

		// Settle with an actual blob hash. Both buy and sell orders are
		// filled, so both get settled.
		actualHash := types.Hash{0x01, 0x02}
		settled, payout := pool.SettleFuture(200, actualHash)
		if settled != 2 {
			t.Errorf("settled = %d, want 2 (both buy and sell orders)", settled)
		}
		if payout == nil || payout.Sign() < 0 {
			t.Error("payout should be non-negative")
		}
	})

	t.Run("no book for slot", func(t *testing.T) {
		pool, _ := NewFuturesPool(100, 300)
		settled, payout := pool.SettleFuture(999, types.Hash{})
		if settled != 0 {
			t.Errorf("settled = %d, want 0", settled)
		}
		if payout.Sign() != 0 {
			t.Error("payout should be zero for no book")
		}
	})
}
