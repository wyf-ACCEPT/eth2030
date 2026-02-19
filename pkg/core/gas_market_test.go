package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestCreateContract(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})

	c, err := fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Buyer != buyer {
		t.Errorf("buyer = %v, want %v", c.Buyer, buyer)
	}
	if c.GasAmount != 1000 {
		t.Errorf("gas amount = %d, want 1000", c.GasAmount)
	}
	if c.PricePerGas.Cmp(big.NewInt(50)) != 0 {
		t.Errorf("price per gas = %v, want 50", c.PricePerGas)
	}
	if c.SettlementSlot != 100 {
		t.Errorf("settlement slot = %d, want 100", c.SettlementSlot)
	}
	if c.ExpirySlot != 200 {
		t.Errorf("expiry slot = %d, want 200", c.ExpirySlot)
	}
	if c.Status != ContractOpen {
		t.Errorf("status = %d, want Open (%d)", c.Status, ContractOpen)
	}
	if c.ID.IsZero() {
		t.Error("contract ID should not be zero")
	}
}

func TestCreateContractInvalidGas(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})

	_, err := fm.CreateContract(buyer, 0, big.NewInt(50), 100, 200)
	if err != ErrZeroGasAmount {
		t.Errorf("expected ErrZeroGasAmount, got %v", err)
	}
}

func TestCreateContractInvalidPrice(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})

	// Nil price
	_, err := fm.CreateContract(buyer, 1000, nil, 100, 200)
	if err != ErrZeroPricePerGas {
		t.Errorf("expected ErrZeroPricePerGas for nil, got %v", err)
	}

	// Zero price
	_, err = fm.CreateContract(buyer, 1000, big.NewInt(0), 100, 200)
	if err != ErrZeroPricePerGas {
		t.Errorf("expected ErrZeroPricePerGas for zero, got %v", err)
	}

	// Negative price
	_, err = fm.CreateContract(buyer, 1000, big.NewInt(-1), 100, 200)
	if err != ErrZeroPricePerGas {
		t.Errorf("expected ErrZeroPricePerGas for negative, got %v", err)
	}
}

func TestCreateContractInvalidExpiry(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})

	// Expiry equal to settlement
	_, err := fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 100)
	if err != ErrExpiryBeforeSettle {
		t.Errorf("expected ErrExpiryBeforeSettle, got %v", err)
	}

	// Expiry before settlement
	_, err = fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 50)
	if err != ErrExpiryBeforeSettle {
		t.Errorf("expected ErrExpiryBeforeSettle, got %v", err)
	}
}

func TestCreateContractInvalidSettlementSlot(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})

	_, err := fm.CreateContract(buyer, 1000, big.NewInt(50), 0, 200)
	if err != ErrZeroSettlementSlot {
		t.Errorf("expected ErrZeroSettlementSlot, got %v", err)
	}
}

func TestFillContract(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})
	seller := types.BytesToAddress([]byte{0x02})

	c, _ := fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 200)

	err := fm.FillContract(c.ID, seller)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := fm.GetContract(c.ID)
	if got.Status != ContractFilled {
		t.Errorf("status = %d, want Filled (%d)", got.Status, ContractFilled)
	}
	if got.Seller != seller {
		t.Errorf("seller = %v, want %v", got.Seller, seller)
	}
}

func TestFillContractNotFound(t *testing.T) {
	fm := NewFuturesMarket()
	seller := types.BytesToAddress([]byte{0x02})
	fakeID := types.HexToHash("0xdead")

	err := fm.FillContract(fakeID, seller)
	if err != ErrContractNotFound {
		t.Errorf("expected ErrContractNotFound, got %v", err)
	}
}

func TestFillContractNotOpen(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})
	seller1 := types.BytesToAddress([]byte{0x02})
	seller2 := types.BytesToAddress([]byte{0x03})

	c, _ := fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 200)

	// Fill it once.
	_ = fm.FillContract(c.ID, seller1)

	// Try to fill again.
	err := fm.FillContract(c.ID, seller2)
	if err != ErrContractNotOpen {
		t.Errorf("expected ErrContractNotOpen, got %v", err)
	}
}

func TestSettleContract(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})
	seller := types.BytesToAddress([]byte{0x02})

	c, _ := fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 200)
	_ = fm.FillContract(c.ID, seller)

	// Actual gas price is 80, above the contract price of 50.
	// Settlement = (80 - 50) * 1000 = 30000 (seller pays buyer).
	settlement, err := fm.SettleContract(c.ID, 100, big.NewInt(80))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settlement.Cmp(big.NewInt(30000)) != 0 {
		t.Errorf("settlement = %v, want 30000", settlement)
	}

	got := fm.GetContract(c.ID)
	if got.Status != ContractSettled {
		t.Errorf("status = %d, want Settled (%d)", got.Status, ContractSettled)
	}
}

func TestSettleContractBelowPrice(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})
	seller := types.BytesToAddress([]byte{0x02})

	c, _ := fm.CreateContract(buyer, 500, big.NewInt(100), 100, 200)
	_ = fm.FillContract(c.ID, seller)

	// Actual gas price is 60, below the contract price of 100.
	// Settlement = (60 - 100) * 500 = -20000 (buyer pays seller).
	settlement, err := fm.SettleContract(c.ID, 100, big.NewInt(60))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := big.NewInt(-20000)
	if settlement.Cmp(expected) != 0 {
		t.Errorf("settlement = %v, want %v", settlement, expected)
	}
}

func TestSettleContractNotFilled(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})

	c, _ := fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 200)

	_, err := fm.SettleContract(c.ID, 100, big.NewInt(80))
	if err != ErrContractNotFilled {
		t.Errorf("expected ErrContractNotFilled, got %v", err)
	}
}

func TestSettleContractTooEarly(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})
	seller := types.BytesToAddress([]byte{0x02})

	c, _ := fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 200)
	_ = fm.FillContract(c.ID, seller)

	_, err := fm.SettleContract(c.ID, 50, big.NewInt(80))
	if err != ErrSettlementTooEarly {
		t.Errorf("expected ErrSettlementTooEarly, got %v", err)
	}
}

func TestSettleContractNotFound(t *testing.T) {
	fm := NewFuturesMarket()
	fakeID := types.HexToHash("0xdead")

	_, err := fm.SettleContract(fakeID, 100, big.NewInt(80))
	if err != ErrContractNotFound {
		t.Errorf("expected ErrContractNotFound, got %v", err)
	}
}

func TestExpireContracts(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})
	seller := types.BytesToAddress([]byte{0x02})

	// Create 3 contracts: one filled, two open with different expiry.
	c1, _ := fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 150)
	c2, _ := fm.CreateContract(buyer, 2000, big.NewInt(60), 100, 200)
	c3, _ := fm.CreateContract(buyer, 3000, big.NewInt(70), 100, 250)

	// Fill c1 so it should NOT expire.
	_ = fm.FillContract(c1.ID, seller)

	// Expire at slot 200: c2 should expire (expiry=200), c3 should not (expiry=250).
	count := fm.ExpireContracts(200)
	if count != 1 {
		t.Errorf("expired count = %d, want 1", count)
	}

	if fm.GetContract(c1.ID).Status != ContractFilled {
		t.Error("filled contract should not expire")
	}
	if fm.GetContract(c2.ID).Status != ContractExpired {
		t.Error("c2 should be expired")
	}
	if fm.GetContract(c3.ID).Status != ContractOpen {
		t.Error("c3 should still be open")
	}
}

func TestOpenContracts(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})
	seller := types.BytesToAddress([]byte{0x02})

	// Create 3 contracts, fill one.
	fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 200)
	c2, _ := fm.CreateContract(buyer, 2000, big.NewInt(60), 100, 200)
	fm.CreateContract(buyer, 3000, big.NewInt(70), 100, 200)

	_ = fm.FillContract(c2.ID, seller)

	open := fm.OpenContracts()
	if len(open) != 2 {
		t.Errorf("open contracts = %d, want 2", len(open))
	}
	for _, c := range open {
		if c.Status != ContractOpen {
			t.Errorf("expected open status, got %d", c.Status)
		}
	}
}

func TestMarketStats(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})
	seller := types.BytesToAddress([]byte{0x02})

	// Create 4 contracts.
	c1, _ := fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 200)
	c2, _ := fm.CreateContract(buyer, 2000, big.NewInt(60), 100, 200)
	_, _ = fm.CreateContract(buyer, 3000, big.NewInt(70), 100, 150)
	fm.CreateContract(buyer, 4000, big.NewInt(80), 100, 200) // stays open

	// Fill c1 and c2.
	_ = fm.FillContract(c1.ID, seller)
	_ = fm.FillContract(c2.ID, seller)

	// Settle c1.
	_, _ = fm.SettleContract(c1.ID, 100, big.NewInt(80))

	// Expire c3.
	_ = fm.ExpireContracts(150)

	open, filled, settled, expired := fm.MarketStats()
	if open != 1 {
		t.Errorf("open = %d, want 1", open)
	}
	if filled != 1 {
		t.Errorf("filled = %d, want 1", filled)
	}
	if settled != 1 {
		t.Errorf("settled = %d, want 1", settled)
	}
	if expired != 1 {
		t.Errorf("expired = %d, want 1", expired)
	}
}

func TestGetContractNil(t *testing.T) {
	fm := NewFuturesMarket()
	got := fm.GetContract(types.HexToHash("0xdead"))
	if got != nil {
		t.Error("expected nil for non-existent contract")
	}
}

func TestUniqueContractIDs(t *testing.T) {
	fm := NewFuturesMarket()
	buyer := types.BytesToAddress([]byte{0x01})

	c1, _ := fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 200)
	c2, _ := fm.CreateContract(buyer, 1000, big.NewInt(50), 100, 200)

	if c1.ID == c2.ID {
		t.Error("two contracts with same params should have different IDs (nonce differs)")
	}
}
