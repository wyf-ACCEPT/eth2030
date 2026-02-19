package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestGasFuturesMarket_CreateAndSettle(t *testing.T) {
	market := NewGasFuturesMarket()

	long := types.Address{0x01}
	short := types.Address{0x02}

	future := market.CreateGasFuture(1000, big.NewInt(50_000_000_000), 100_000, long, short)
	if future == nil {
		t.Fatal("expected non-nil future")
	}
	if market.OpenContractCount() != 1 {
		t.Fatalf("expected 1 open contract, got %d", market.OpenContractCount())
	}

	// Settle with actual price above strike -> Long wins.
	settlement, err := market.SettleGasFuture(future.ID, big.NewInt(60_000_000_000))
	if err != nil {
		t.Fatalf("SettleGasFuture failed: %v", err)
	}
	if settlement.Winner != long {
		t.Fatal("expected Long to win when actual > strike")
	}
	// Payout = |60G - 50G| * 100_000 = 10G * 100_000 = 1_000_000G.
	expectedPayout := new(big.Int).Mul(big.NewInt(10_000_000_000), big.NewInt(100_000))
	if settlement.Payout.Cmp(expectedPayout) != 0 {
		t.Fatalf("expected payout %s, got %s", expectedPayout, settlement.Payout)
	}
	if market.OpenContractCount() != 0 {
		t.Fatalf("expected 0 open contracts after settlement, got %d", market.OpenContractCount())
	}
	if market.SettledContractCount() != 1 {
		t.Fatalf("expected 1 settled contract, got %d", market.SettledContractCount())
	}
}

func TestGasFuturesMarket_ShortWins(t *testing.T) {
	market := NewGasFuturesMarket()

	long := types.Address{0x01}
	short := types.Address{0x02}

	future := market.CreateGasFuture(1000, big.NewInt(50_000_000_000), 100_000, long, short)

	// Settle with actual price below strike -> Short wins.
	settlement, err := market.SettleGasFuture(future.ID, big.NewInt(40_000_000_000))
	if err != nil {
		t.Fatalf("SettleGasFuture failed: %v", err)
	}
	if settlement.Winner != short {
		t.Fatal("expected Short to win when actual < strike")
	}
}

func TestGasFuturesMarket_SettleNotFound(t *testing.T) {
	market := NewGasFuturesMarket()

	_, err := market.SettleGasFuture(types.Hash{0xFF}, big.NewInt(100))
	if err != ErrFutureNotFound {
		t.Fatalf("expected ErrFutureNotFound, got %v", err)
	}
}

func TestGasFuturesMarket_OpenInterest(t *testing.T) {
	market := NewGasFuturesMarket()

	long := types.Address{0x01}
	short := types.Address{0x02}

	market.CreateGasFuture(1000, big.NewInt(50_000_000_000), 100_000, long, short)
	market.CreateGasFuture(2000, big.NewInt(60_000_000_000), 200_000, long, short)

	oi := market.GetOpenInterest()
	expected := big.NewInt(300_000)
	if oi.Cmp(expected) != 0 {
		t.Fatalf("expected open interest %s, got %s", expected, oi)
	}
}

func TestGasFuturesMarket_ExpiryCleanup(t *testing.T) {
	market := NewGasFuturesMarket()

	long := types.Address{0x01}
	short := types.Address{0x02}

	market.CreateGasFuture(100, big.NewInt(50_000_000_000), 100_000, long, short)
	market.CreateGasFuture(200, big.NewInt(60_000_000_000), 200_000, long, short)

	// Cleanup at block 150: first future expires, second stays.
	market.ExpiryCleanup(150)
	if market.OpenContractCount() != 1 {
		t.Fatalf("expected 1 open contract after cleanup, got %d", market.OpenContractCount())
	}
	if market.SettledContractCount() != 1 {
		t.Fatalf("expected 1 settled contract after cleanup, got %d", market.SettledContractCount())
	}

	// Cleanup at block 200: second future also expires.
	market.ExpiryCleanup(200)
	if market.OpenContractCount() != 0 {
		t.Fatalf("expected 0 open contracts, got %d", market.OpenContractCount())
	}
	if market.SettledContractCount() != 2 {
		t.Fatalf("expected 2 settled contracts, got %d", market.SettledContractCount())
	}
}

func TestPriceGasFuture(t *testing.T) {
	// Past expiry -> price is 0.
	price := PriceGasFuture(1000, 500, big.NewInt(50_000_000_000), 100)
	if price.Sign() != 0 {
		t.Fatalf("expected 0 price past expiry, got %s", price)
	}

	// Active future.
	price = PriceGasFuture(100, 1100, big.NewInt(50_000_000_000), 100)
	// = 50_000_000_000 * 1000 * 100 / 10_000_000 = 500_000_000
	expected := big.NewInt(500_000_000)
	if price.Cmp(expected) != 0 {
		t.Fatalf("expected price %s, got %s", expected, price)
	}
}

func TestGasFuturesMarket_MultipleContracts(t *testing.T) {
	market := NewGasFuturesMarket()

	long := types.Address{0x01}
	short := types.Address{0x02}

	// Create multiple contracts.
	futures := make([]*GasFuture, 5)
	for i := 0; i < 5; i++ {
		futures[i] = market.CreateGasFuture(
			uint64(1000+i*100),
			big.NewInt(int64(50_000_000_000+i*1_000_000_000)),
			uint64(100_000+i*10_000),
			long,
			short,
		)
	}

	if market.OpenContractCount() != 5 {
		t.Fatalf("expected 5 open contracts, got %d", market.OpenContractCount())
	}

	// Settle the first one.
	_, err := market.SettleGasFuture(futures[0].ID, big.NewInt(55_000_000_000))
	if err != nil {
		t.Fatalf("SettleGasFuture failed: %v", err)
	}
	if market.OpenContractCount() != 4 {
		t.Fatalf("expected 4 open contracts, got %d", market.OpenContractCount())
	}
}
