package das

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestCreateFuture(t *testing.T) {
	fm := NewFuturesMarket(100)
	blobHash := types.Hash{0x01}
	creator := types.Address{0xAA}
	price := big.NewInt(1_000_000_000) // 1 Gwei

	future, err := fm.CreateFuture(blobHash, 200, price, creator)
	if err != nil {
		t.Fatalf("CreateFuture: %v", err)
	}

	if future.ExpirySlot != 200 {
		t.Errorf("ExpirySlot = %d, want 200", future.ExpirySlot)
	}
	if future.BlobHash != blobHash {
		t.Error("BlobHash mismatch")
	}
	if future.Price.Cmp(price) != 0 {
		t.Errorf("Price = %v, want %v", future.Price, price)
	}
	if future.Creator != creator {
		t.Error("Creator mismatch")
	}
	if future.Settled {
		t.Error("future should not be settled initially")
	}
	if fm.ActiveCount() != 1 {
		t.Errorf("ActiveCount = %d, want 1", fm.ActiveCount())
	}
}

func TestCreateFutureInvalidExpiry(t *testing.T) {
	fm := NewFuturesMarket(100)

	// Expiry in the past.
	_, err := fm.CreateFuture(types.Hash{}, 50, big.NewInt(1000), types.Address{})
	if err != ErrInvalidExpiry {
		t.Fatalf("expected ErrInvalidExpiry, got %v", err)
	}

	// Expiry at current slot.
	_, err = fm.CreateFuture(types.Hash{}, 100, big.NewInt(1000), types.Address{})
	if err != ErrInvalidExpiry {
		t.Fatalf("expected ErrInvalidExpiry for current slot, got %v", err)
	}
}

func TestCreateFutureInvalidPrice(t *testing.T) {
	fm := NewFuturesMarket(100)

	// Zero price.
	_, err := fm.CreateFuture(types.Hash{}, 200, big.NewInt(0), types.Address{})
	if err != ErrInvalidPrice {
		t.Fatalf("expected ErrInvalidPrice, got %v", err)
	}

	// Negative price.
	_, err = fm.CreateFuture(types.Hash{}, 200, big.NewInt(-1), types.Address{})
	if err != ErrInvalidPrice {
		t.Fatalf("expected ErrInvalidPrice for negative, got %v", err)
	}

	// Nil price.
	_, err = fm.CreateFuture(types.Hash{}, 200, nil, types.Address{})
	if err != ErrInvalidPrice {
		t.Fatalf("expected ErrInvalidPrice for nil, got %v", err)
	}
}

func TestSettleFutureAvailable(t *testing.T) {
	fm := NewFuturesMarket(100)
	price := big.NewInt(1_000_000_000)

	future, _ := fm.CreateFuture(types.Hash{0x01}, 200, price, types.Address{0xAA})

	payout, err := fm.SettleFuture(future.ID, true)
	if err != nil {
		t.Fatalf("SettleFuture: %v", err)
	}

	expected := new(big.Int).Mul(price, big.NewInt(2))
	if payout.Cmp(expected) != 0 {
		t.Errorf("payout = %v, want %v (2x stake)", payout, expected)
	}
}

func TestSettleFutureUnavailable(t *testing.T) {
	fm := NewFuturesMarket(100)
	price := big.NewInt(1_000_000_000)

	future, _ := fm.CreateFuture(types.Hash{0x02}, 200, price, types.Address{0xBB})

	payout, err := fm.SettleFuture(future.ID, false)
	if err != nil {
		t.Fatalf("SettleFuture: %v", err)
	}

	if payout.Sign() != 0 {
		t.Errorf("payout = %v, want 0 (blob unavailable)", payout)
	}
}

func TestSettleFutureDouble(t *testing.T) {
	fm := NewFuturesMarket(100)

	future, _ := fm.CreateFuture(types.Hash{0x03}, 200, big.NewInt(1000), types.Address{})
	fm.SettleFuture(future.ID, true)

	_, err := fm.SettleFuture(future.ID, true)
	if err != ErrFutureSettled {
		t.Fatalf("expected ErrFutureSettled, got %v", err)
	}
}

func TestSettleFutureNotFound(t *testing.T) {
	fm := NewFuturesMarket(100)

	_, err := fm.SettleFuture(types.Hash{0xFF}, true)
	if err != ErrFutureNotFound {
		t.Fatalf("expected ErrFutureNotFound, got %v", err)
	}
}

func TestExpireFutures(t *testing.T) {
	fm := NewFuturesMarket(100)

	// Create futures at different expiry slots.
	fm.CreateFuture(types.Hash{0x01}, 150, big.NewInt(1000), types.Address{0x01})
	fm.CreateFuture(types.Hash{0x02}, 200, big.NewInt(2000), types.Address{0x02})
	fm.CreateFuture(types.Hash{0x03}, 300, big.NewInt(3000), types.Address{0x03})

	if fm.ActiveCount() != 3 {
		t.Fatalf("ActiveCount = %d, want 3", fm.ActiveCount())
	}

	// Expire up to slot 200 (should expire futures at 150 and 200).
	expired := fm.ExpireFutures(200)
	if expired != 2 {
		t.Errorf("expired = %d, want 2", expired)
	}
	if fm.ActiveCount() != 1 {
		t.Errorf("ActiveCount = %d, want 1", fm.ActiveCount())
	}

	// Expire up to slot 300.
	expired = fm.ExpireFutures(300)
	if expired != 1 {
		t.Errorf("expired = %d, want 1", expired)
	}
	if fm.ActiveCount() != 0 {
		t.Errorf("ActiveCount = %d, want 0", fm.ActiveCount())
	}
}

func TestPriceFuture(t *testing.T) {
	// Price should be zero for expired slots.
	price := PriceFuture(200, 100, 1)
	if price.Sign() != 0 {
		t.Errorf("price for expired future = %v, want 0", price)
	}

	// Price should be positive for valid futures.
	price = PriceFuture(100, 200, 3)
	if price.Sign() <= 0 {
		t.Errorf("price = %v, want positive", price)
	}

	// Longer expiry should mean higher price.
	priceShort := PriceFuture(100, 110, 3)
	priceLong := PriceFuture(100, 1000, 3)
	if priceLong.Cmp(priceShort) <= 0 {
		t.Errorf("long-dated price %v should be > short-dated price %v", priceLong, priceShort)
	}

	// More blobs should mean higher price.
	priceFew := PriceFuture(100, 200, 1)
	priceMany := PriceFuture(100, 200, 9)
	if priceMany.Cmp(priceFew) <= 0 {
		t.Errorf("many-blob price %v should be > few-blob price %v", priceMany, priceFew)
	}
}

func TestTotalVolume(t *testing.T) {
	fm := NewFuturesMarket(100)

	fm.CreateFuture(types.Hash{0x01}, 200, big.NewInt(1000), types.Address{})
	fm.CreateFuture(types.Hash{0x02}, 200, big.NewInt(2000), types.Address{0x01})

	vol := fm.TotalVolume()
	expected := big.NewInt(3000)
	if vol.Cmp(expected) != 0 {
		t.Errorf("TotalVolume = %v, want %v", vol, expected)
	}
}

func TestFutureIDDeterministic(t *testing.T) {
	blobHash := types.Hash{0x42}
	creator := types.Address{0xAA}

	id1 := computeFutureID(blobHash, 100, creator)
	id2 := computeFutureID(blobHash, 100, creator)

	if id1 != id2 {
		t.Error("future ID not deterministic")
	}

	// Different params should produce different IDs.
	id3 := computeFutureID(blobHash, 101, creator)
	if id1 == id3 {
		t.Error("different expiry should produce different ID")
	}
}
