package core

import (
	"fmt"
	"math/big"
	"sync"
	"testing"
)

func TestRegisterPosition(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())

	// Strike=100, notional=1000, margin=20% -> need 100*1000*2000/10000 = 20000.
	err := s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(20000), 500)
	if err != nil {
		t.Fatalf("RegisterPosition: %v", err)
	}

	pos := s.GetPosition("c1")
	if pos == nil {
		t.Fatal("expected position to exist")
	}
	if pos.Direction != DirectionLong {
		t.Errorf("direction = %d, want Long", pos.Direction)
	}
	if pos.StrikePrice.Int64() != 100 {
		t.Errorf("strike = %s, want 100", pos.StrikePrice)
	}
	if pos.NotionalGas != 1000 {
		t.Errorf("notional = %d, want 1000", pos.NotionalGas)
	}
	if pos.Collateral.Int64() != 20000 {
		t.Errorf("collateral = %s, want 20000", pos.Collateral)
	}
	if pos.ExpirySlot != 500 {
		t.Errorf("expiry = %d, want 500", pos.ExpirySlot)
	}
	if pos.Settled {
		t.Error("new position should not be settled")
	}
	if pos.LiquidationPrice == nil {
		t.Error("liquidation price should be set")
	}
}

func TestRegisterPositionInsufficientMargin(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())

	// Need 20000 margin, only provide 19999.
	err := s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(19999), 500)
	if err == nil {
		t.Fatal("should fail with insufficient margin")
	}
}

func TestRegisterPositionInvalidDirection(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	err := s.RegisterPosition("c1", 99, big.NewInt(100), 1000,
		big.NewInt(100000), 500)
	if err != ErrSettlementInvalidDirection {
		t.Fatalf("expected ErrSettlementInvalidDirection, got %v", err)
	}
}

func TestRegisterPositionZeroStrike(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	err := s.RegisterPosition("c1", DirectionLong, big.NewInt(0), 1000,
		big.NewInt(100000), 500)
	if err != ErrSettlementZeroStrike {
		t.Fatalf("expected ErrSettlementZeroStrike, got %v", err)
	}
}

func TestRegisterPositionNilStrike(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	err := s.RegisterPosition("c1", DirectionLong, nil, 1000,
		big.NewInt(100000), 500)
	if err != ErrSettlementZeroStrike {
		t.Fatalf("expected ErrSettlementZeroStrike, got %v", err)
	}
}

func TestRegisterPositionZeroNotional(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	err := s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 0,
		big.NewInt(100000), 500)
	if err != ErrSettlementZeroNotional {
		t.Fatalf("expected ErrSettlementZeroNotional, got %v", err)
	}
}

func TestSettleContractLongProfit(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	// Settlement price 150 > strike 100: Long profits.
	// PnL = (150 - 100) * 1000 = 50000
	result, err := s.SettleContract("c1", 500, big.NewInt(150))
	if err != nil {
		t.Fatalf("SettleContract: %v", err)
	}
	if !result.Settled {
		t.Fatal("should be settled")
	}
	if result.PnL.Int64() != 50000 {
		t.Fatalf("PnL = %s, want 50000", result.PnL)
	}
	if result.Direction != DirectionLong {
		t.Fatalf("direction = %d, want Long", result.Direction)
	}
}

func TestSettleContractLongLoss(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	// Settlement price 80 < strike 100: Long loses.
	// PnL = (80 - 100) * 1000 = -20000
	result, err := s.SettleContract("c1", 500, big.NewInt(80))
	if err != nil {
		t.Fatalf("SettleContract: %v", err)
	}
	if result.PnL.Int64() != -20000 {
		t.Fatalf("PnL = %s, want -20000", result.PnL)
	}
}

func TestSettleContractShortProfit(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionShort, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	// Settlement price 80 < strike 100: Short profits.
	// PnL = (100 - 80) * 1000 = 20000
	result, err := s.SettleContract("c1", 500, big.NewInt(80))
	if err != nil {
		t.Fatalf("SettleContract: %v", err)
	}
	if result.PnL.Int64() != 20000 {
		t.Fatalf("PnL = %s, want 20000", result.PnL)
	}
}

func TestSettleContractShortLoss(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionShort, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	// Settlement price 150 > strike 100: Short loses.
	// PnL = (100 - 150) * 1000 = -50000
	result, err := s.SettleContract("c1", 500, big.NewInt(150))
	if err != nil {
		t.Fatalf("SettleContract: %v", err)
	}
	if result.PnL.Int64() != -50000 {
		t.Fatalf("PnL = %s, want -50000", result.PnL)
	}
}

func TestSettleContractNotFoundSettlement(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_, err := s.SettleContract("nonexistent", 500, big.NewInt(100))
	if err != ErrSettlementContractNotFound {
		t.Fatalf("expected ErrSettlementContractNotFound, got %v", err)
	}
}

func TestSettleContractNotExpired(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	// Current slot 499 < expiry 500.
	_, err := s.SettleContract("c1", 499, big.NewInt(150))
	if err != ErrSettlementNotExpired {
		t.Fatalf("expected ErrSettlementNotExpired, got %v", err)
	}
}

func TestSettleContractAlreadySettled(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	_, _ = s.SettleContract("c1", 500, big.NewInt(150))

	// Second settlement should fail.
	_, err := s.SettleContract("c1", 500, big.NewInt(150))
	if err != ErrSettlementAlreadySettled {
		t.Fatalf("expected ErrSettlementAlreadySettled, got %v", err)
	}
}

func TestBatchSettle(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)
	_ = s.RegisterPosition("c2", DirectionShort, big.NewInt(100), 500,
		big.NewInt(50000), 500)
	_ = s.RegisterPosition("c3", DirectionLong, big.NewInt(100), 200,
		big.NewInt(20000), 600) // not yet expired

	results, errs := s.BatchSettle(
		[]string{"c1", "c2", "c3", "nonexistent"},
		500,
		big.NewInt(120),
	)

	// c1 and c2 should settle; c3 is not expired; nonexistent is not found.
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(errs))
	}

	// c1 long: PnL = (120-100)*1000 = 20000
	if results[0].ContractID != "c1" || results[0].PnL.Int64() != 20000 {
		t.Errorf("c1 PnL = %s, want 20000", results[0].PnL)
	}
	// c2 short: PnL = (100-120)*500 = -10000
	if results[1].ContractID != "c2" || results[1].PnL.Int64() != -10000 {
		t.Errorf("c2 PnL = %s, want -10000", results[1].PnL)
	}
}

func TestMarkToMarket(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	// Current price 130: unrealized PnL = (130-100)*1000 = 30000
	pnl, err := s.MarkToMarket("c1", big.NewInt(130))
	if err != nil {
		t.Fatalf("MarkToMarket: %v", err)
	}
	if pnl.Int64() != 30000 {
		t.Fatalf("MTM PnL = %s, want 30000", pnl)
	}

	// Current price 80: unrealized PnL = (80-100)*1000 = -20000
	pnl2, err := s.MarkToMarket("c1", big.NewInt(80))
	if err != nil {
		t.Fatalf("MarkToMarket: %v", err)
	}
	if pnl2.Int64() != -20000 {
		t.Fatalf("MTM PnL = %s, want -20000", pnl2)
	}
}

func TestMarkToMarketNotFound(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_, err := s.MarkToMarket("nonexistent", big.NewInt(100))
	if err != ErrSettlementContractNotFound {
		t.Fatalf("expected ErrSettlementContractNotFound, got %v", err)
	}
}

func TestMarkToMarketSettledReturnsResultPnL(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	_, _ = s.SettleContract("c1", 500, big.NewInt(150))

	// After settlement, MTM should return the settled PnL.
	pnl, err := s.MarkToMarket("c1", big.NewInt(200)) // price doesn't matter
	if err != nil {
		t.Fatalf("MarkToMarket after settlement: %v", err)
	}
	// Settled PnL = (150-100)*1000 = 50000
	if pnl.Int64() != 50000 {
		t.Fatalf("settled MTM PnL = %s, want 50000", pnl)
	}
}

func TestExpiryCheck(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)
	_ = s.RegisterPosition("c2", DirectionShort, big.NewInt(100), 500,
		big.NewInt(50000), 600)
	_ = s.RegisterPosition("c3", DirectionLong, big.NewInt(100), 200,
		big.NewInt(20000), 400)

	// At slot 500: c1 and c3 (expiry 400 and 500) should settle.
	results, errs := s.ExpiryCheck(500, big.NewInt(120))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 settlements, got %d", len(results))
	}

	// c2 should still be open.
	if s.OpenPositionCount() != 1 {
		t.Fatalf("expected 1 open position, got %d", s.OpenPositionCount())
	}
}

func TestCheckLiquidations(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())

	// Long position: strike=100, notional=1000, collateral=20000 (exact min).
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(20000), 1000)

	// Current price drops significantly. Let's see when liquidation triggers.
	// maintenanceMargin = 100 * 1000 * 1000 / 10000 = 10000
	// For liquidation: collateral - |loss| < maintenanceMargin
	// loss = (100 - currentPrice) * 1000
	// 20000 - (100-price)*1000 < 10000
	// 20000 - 100000 + price*1000 < 10000
	// price*1000 < 90000
	// price < 90
	// So at price=89 the position should be liquidated.

	liquidated := s.CheckLiquidations(big.NewInt(89), 100)
	if len(liquidated) != 1 {
		t.Fatalf("expected 1 liquidation, got %d", len(liquidated))
	}
	if liquidated[0].ContractID != "c1" {
		t.Fatalf("liquidated contract = %s, want c1", liquidated[0].ContractID)
	}

	// PnL = (89 - 100) * 1000 = -11000
	if liquidated[0].PnL.Int64() != -11000 {
		t.Fatalf("liquidation PnL = %s, want -11000", liquidated[0].PnL)
	}
}

func TestCheckLiquidationsNonTriggered(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())

	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(50000), 1000) // plenty of collateral

	// Price drops a bit but not enough to trigger liquidation.
	liquidated := s.CheckLiquidations(big.NewInt(90), 100)
	if len(liquidated) != 0 {
		t.Fatalf("expected no liquidations, got %d", len(liquidated))
	}
}

func TestCheckLiquidationsShort(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())

	// Short position: strike=100, notional=1000, collateral=20000 (exact min).
	_ = s.RegisterPosition("c1", DirectionShort, big.NewInt(100), 1000,
		big.NewInt(20000), 1000)

	// maintenanceMargin = 10000
	// Short loss = (price - 100) * 1000
	// 20000 - (price-100)*1000 < 10000
	// 20000 - price*1000 + 100000 < 10000
	// 110000 < 10000 + price*1000
	// price > 110

	liquidated := s.CheckLiquidations(big.NewInt(111), 100)
	if len(liquidated) != 1 {
		t.Fatalf("expected 1 liquidation, got %d", len(liquidated))
	}
}

func TestAddCollateral(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(20000), 500)

	liqBefore := s.GetPosition("c1").LiquidationPrice

	err := s.AddCollateral("c1", big.NewInt(10000))
	if err != nil {
		t.Fatalf("AddCollateral: %v", err)
	}

	pos := s.GetPosition("c1")
	if pos.Collateral.Int64() != 30000 {
		t.Fatalf("collateral = %s, want 30000", pos.Collateral)
	}

	// Liquidation price should have improved (lower for long).
	if pos.LiquidationPrice.Cmp(liqBefore) >= 0 {
		t.Fatalf("liquidation price should improve: before %s, after %s",
			liqBefore, pos.LiquidationPrice)
	}
}

func TestAddCollateralNotFound(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	err := s.AddCollateral("nonexistent", big.NewInt(1000))
	if err != ErrSettlementContractNotFound {
		t.Fatalf("expected ErrSettlementContractNotFound, got %v", err)
	}
}

func TestAddCollateralSettled(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)
	_, _ = s.SettleContract("c1", 500, big.NewInt(150))

	err := s.AddCollateral("c1", big.NewInt(1000))
	if err != ErrSettlementAlreadySettled {
		t.Fatalf("expected ErrSettlementAlreadySettled, got %v", err)
	}
}

func TestAddCollateralInvalidAmount(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	err := s.AddCollateral("c1", big.NewInt(0))
	if err == nil {
		t.Fatal("should fail with zero amount")
	}

	err = s.AddCollateral("c1", big.NewInt(-1))
	if err == nil {
		t.Fatal("should fail with negative amount")
	}
}

func TestPositionCount(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())

	if s.PositionCount() != 0 {
		t.Fatalf("initial count = %d, want 0", s.PositionCount())
	}

	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)
	_ = s.RegisterPosition("c2", DirectionShort, big.NewInt(100), 500,
		big.NewInt(50000), 500)

	if s.PositionCount() != 2 {
		t.Fatalf("count = %d, want 2", s.PositionCount())
	}
	if s.OpenPositionCount() != 2 {
		t.Fatalf("open count = %d, want 2", s.OpenPositionCount())
	}

	_, _ = s.SettleContract("c1", 500, big.NewInt(150))

	if s.PositionCount() != 2 {
		t.Fatalf("total count should still be 2, got %d", s.PositionCount())
	}
	if s.OpenPositionCount() != 1 {
		t.Fatalf("open count = %d, want 1", s.OpenPositionCount())
	}
	if s.SettledCount() != 1 {
		t.Fatalf("settled count = %d, want 1", s.SettledCount())
	}
}

func TestGetSettlementResult(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	// Before settlement.
	r := s.GetSettlementResult("c1")
	if r != nil {
		t.Fatal("should return nil before settlement")
	}

	// After settlement.
	_, _ = s.SettleContract("c1", 500, big.NewInt(150))
	r = s.GetSettlementResult("c1")
	if r == nil {
		t.Fatal("should return result after settlement")
	}
	if r.PnL.Int64() != 50000 {
		t.Fatalf("result PnL = %s, want 50000", r.PnL)
	}
}

func TestGetPositionReturnsCopy(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	pos := s.GetPosition("c1")
	pos.StrikePrice.SetInt64(999)

	pos2 := s.GetPosition("c1")
	if pos2.StrikePrice.Int64() != 100 {
		t.Fatalf("GetPosition should return copy, strike changed to %s", pos2.StrikePrice)
	}
}

func TestGetPositionNotFound(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	pos := s.GetPosition("nonexistent")
	if pos != nil {
		t.Fatal("should return nil for nonexistent position")
	}
}

func TestSettlementAtExactStrike(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	result, err := s.SettleContract("c1", 500, big.NewInt(100))
	if err != nil {
		t.Fatalf("SettleContract: %v", err)
	}
	if result.PnL.Sign() != 0 {
		t.Fatalf("PnL at strike should be 0, got %s", result.PnL)
	}
}

func TestLiquidationPriceCalculation(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())

	// Long position: strike=100, notional=1000, collateral=20000.
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(20000), 500)

	pos := s.GetPosition("c1")

	// maintenance = 100*1000*1000/10000 = 10000
	// buffer = 20000 - 10000 = 10000
	// perUnit = 10000 / 1000 = 10
	// liqPrice = 100 - 10 = 90
	if pos.LiquidationPrice.Int64() != 90 {
		t.Fatalf("long liq price = %s, want 90", pos.LiquidationPrice)
	}
}

func TestLiquidationPriceShort(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())

	// Short position: strike=100, notional=1000, collateral=20000.
	_ = s.RegisterPosition("c1", DirectionShort, big.NewInt(100), 1000,
		big.NewInt(20000), 500)

	pos := s.GetPosition("c1")

	// maintenance = 10000, buffer = 10000, perUnit = 10
	// liqPrice = 100 + 10 = 110
	if pos.LiquidationPrice.Int64() != 110 {
		t.Fatalf("short liq price = %s, want 110", pos.LiquidationPrice)
	}
}

func TestConcurrentSettlement(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())

	// Register many positions.
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("c%d", i)
		_ = s.RegisterPosition(id, DirectionLong, big.NewInt(100), 1000,
			big.NewInt(100000), 500)
	}

	var wg sync.WaitGroup

	// Concurrent reads and settlements.
	for i := 0; i < 50; i++ {
		wg.Add(3)
		idx := i
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("c%d", idx)
			_, _ = s.SettleContract(id, 500, big.NewInt(150))
		}()
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("c%d", idx)
			_, _ = s.MarkToMarket(id, big.NewInt(120))
		}()
		go func() {
			defer wg.Done()
			_ = s.GetPosition(fmt.Sprintf("c%d", idx))
		}()
	}

	wg.Wait()

	// All should be settled.
	if s.OpenPositionCount() != 0 {
		t.Fatalf("expected 0 open, got %d", s.OpenPositionCount())
	}
	if s.SettledCount() != 50 {
		t.Fatalf("expected 50 settled, got %d", s.SettledCount())
	}
}

func TestBatchSettlePartialFailure(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 500)

	// c1 should settle, "missing" should fail.
	results, errs := s.BatchSettle([]string{"c1", "missing"}, 500, big.NewInt(120))
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestExpiryCheckDoesNotSettleFutureContracts(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())
	_ = s.RegisterPosition("c1", DirectionLong, big.NewInt(100), 1000,
		big.NewInt(100000), 1000)

	// Current slot 500 < expiry 1000.
	results, errs := s.ExpiryCheck(500, big.NewInt(120))
	if len(results) != 0 {
		t.Fatalf("should not settle future contracts, got %d results", len(results))
	}
	if len(errs) != 0 {
		t.Fatalf("should have no errors, got %d", len(errs))
	}
}

func TestRegisterShortPosition(t *testing.T) {
	s := NewGasFuturesSettlement(DefaultSettlementConfig())

	err := s.RegisterPosition("c1", DirectionShort, big.NewInt(100), 1000,
		big.NewInt(20000), 500)
	if err != nil {
		t.Fatalf("RegisterPosition short: %v", err)
	}

	pos := s.GetPosition("c1")
	if pos.Direction != DirectionShort {
		t.Errorf("direction = %d, want Short", pos.Direction)
	}
}
