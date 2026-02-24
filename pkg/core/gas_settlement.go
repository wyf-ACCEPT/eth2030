// gas_settlement.go implements gas futures settlement logic for the Ethereum
// 2030 roadmap's long-dated gas futures track (M+ upgrade). It manages the
// full contract lifecycle: position tracking, mark-to-market valuations,
// collateral/margin management, liquidation of undercollateralized positions,
// and batch settlement at expiry.
//
// This builds on the existing gas_futures.go (GasFuturesMarket) and
// gas_market.go (FuturesMarket) by adding the settlement and risk
// management layer that those modules lack.
package core

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
)

// Settlement direction.
const (
	DirectionLong  uint8 = 0
	DirectionShort uint8 = 1
)

// Default collateral parameters.
const (
	// DefaultMaintenanceMarginBPS is the maintenance margin in basis points
	// (e.g., 1000 = 10%). Positions below this are liquidated.
	DefaultMaintenanceMarginBPS uint64 = 1000

	// DefaultInitialMarginBPS is the initial margin required in basis points
	// (e.g., 2000 = 20%).
	DefaultInitialMarginBPS uint64 = 2000
)

// Gas settlement errors.
var (
	ErrSettlementContractNotFound   = errors.New("settlement: contract not found")
	ErrSettlementNotExpired         = errors.New("settlement: contract has not reached expiry")
	ErrSettlementAlreadySettled     = errors.New("settlement: contract already settled")
	ErrSettlementInvalidDirection   = errors.New("settlement: invalid direction")
	ErrSettlementInsufficientMargin = errors.New("settlement: insufficient collateral for margin")
	ErrSettlementZeroNotional       = errors.New("settlement: notional value must be > 0")
	ErrSettlementZeroStrike         = errors.New("settlement: strike price must be > 0")
)

// Position represents a single futures position with collateral tracking.
type Position struct {
	ContractID       string
	Direction        uint8    // DirectionLong or DirectionShort
	StrikePrice      *big.Int // agreed gas price (wei)
	NotionalGas      uint64   // amount of gas covered
	Collateral       *big.Int // posted collateral (wei)
	LiquidationPrice *big.Int // price at which position gets liquidated
	ExpirySlot       uint64   // slot at which settlement occurs
	Settled          bool
}

// SettlementResult records the outcome of settling a single contract.
type SettlementResult struct {
	ContractID      string
	SettlementPrice *big.Int // actual gas price at settlement
	StrikePrice     *big.Int // the agreed strike price
	PnL             *big.Int // profit/loss (positive = profit for the position holder)
	Direction       uint8
	Settled         bool
}

// SettlementConfig holds parameters for the settlement engine.
type SettlementConfig struct {
	InitialMarginBPS     uint64 // required initial margin in basis points
	MaintenanceMarginBPS uint64 // maintenance margin in basis points
}

// DefaultSettlementConfig returns production-like settlement config.
func DefaultSettlementConfig() SettlementConfig {
	return SettlementConfig{
		InitialMarginBPS:     DefaultInitialMarginBPS,
		MaintenanceMarginBPS: DefaultMaintenanceMarginBPS,
	}
}

// GasFuturesSettlement manages the lifecycle of gas futures contracts
// including position tracking, mark-to-market, collateral management,
// and settlement. Thread-safe.
type GasFuturesSettlement struct {
	mu        sync.RWMutex
	config    SettlementConfig
	positions map[string]*Position // contractID -> Position
	results   map[string]*SettlementResult
}

// NewGasFuturesSettlement creates a new settlement engine.
func NewGasFuturesSettlement(cfg SettlementConfig) *GasFuturesSettlement {
	return &GasFuturesSettlement{
		config:    cfg,
		positions: make(map[string]*Position),
		results:   make(map[string]*SettlementResult),
	}
}

// RegisterPosition registers a new futures position with the settlement
// engine. The collateral must meet the initial margin requirement:
//
//	requiredMargin = strikePrice * notionalGas * initialMarginBPS / 10000
func (s *GasFuturesSettlement) RegisterPosition(
	contractID string,
	direction uint8,
	strikePrice *big.Int,
	notionalGas uint64,
	collateral *big.Int,
	expirySlot uint64,
) error {
	if direction != DirectionLong && direction != DirectionShort {
		return ErrSettlementInvalidDirection
	}
	if strikePrice == nil || strikePrice.Sign() <= 0 {
		return ErrSettlementZeroStrike
	}
	if notionalGas == 0 {
		return ErrSettlementZeroNotional
	}

	required := calcMarginRequired(strikePrice, notionalGas, s.config.InitialMarginBPS)
	if collateral == nil || collateral.Cmp(required) < 0 {
		return fmt.Errorf("%w: need %s, have %v",
			ErrSettlementInsufficientMargin, required, collateral)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	liqPrice := calcLiquidationPrice(strikePrice, direction, notionalGas,
		collateral, s.config.MaintenanceMarginBPS)

	s.positions[contractID] = &Position{
		ContractID:       contractID,
		Direction:        direction,
		StrikePrice:      new(big.Int).Set(strikePrice),
		NotionalGas:      notionalGas,
		Collateral:       new(big.Int).Set(collateral),
		LiquidationPrice: liqPrice,
		ExpirySlot:       expirySlot,
		Settled:          false,
	}
	return nil
}

// GetPosition returns a copy of the position for the given contract ID.
func (s *GasFuturesSettlement) GetPosition(contractID string) *Position {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.positions[contractID]
	if !ok {
		return nil
	}
	return copyPosition(p)
}

// SettleContract settles a single contract at the given current slot using the
// actual gas price. The contract must have reached its expiry slot.
//
// PnL for long: (settlementPrice - strikePrice) * notionalGas
// PnL for short: (strikePrice - settlementPrice) * notionalGas
func (s *GasFuturesSettlement) SettleContract(
	contractID string,
	currentSlot uint64,
	settlementPrice *big.Int,
) (*SettlementResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.settleContractLocked(contractID, currentSlot, settlementPrice)
}

// settleContractLocked performs settlement while the lock is held.
func (s *GasFuturesSettlement) settleContractLocked(
	contractID string,
	currentSlot uint64,
	settlementPrice *big.Int,
) (*SettlementResult, error) {
	pos, ok := s.positions[contractID]
	if !ok {
		return nil, ErrSettlementContractNotFound
	}
	if pos.Settled {
		return nil, ErrSettlementAlreadySettled
	}
	if currentSlot < pos.ExpirySlot {
		return nil, ErrSettlementNotExpired
	}

	pnl := calcPnL(pos.Direction, pos.StrikePrice, settlementPrice, pos.NotionalGas)

	result := &SettlementResult{
		ContractID:      contractID,
		SettlementPrice: new(big.Int).Set(settlementPrice),
		StrikePrice:     new(big.Int).Set(pos.StrikePrice),
		PnL:             pnl,
		Direction:       pos.Direction,
		Settled:         true,
	}

	pos.Settled = true
	s.results[contractID] = result
	return result, nil
}

// BatchSettle settles multiple contracts in a single call. Returns results for
// each contract; if any individual settlement fails, the error is recorded
// and processing continues with remaining contracts.
func (s *GasFuturesSettlement) BatchSettle(
	contractIDs []string,
	currentSlot uint64,
	settlementPrice *big.Int,
) ([]*SettlementResult, []error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	results := make([]*SettlementResult, 0, len(contractIDs))
	errs := make([]error, 0)

	for _, id := range contractIDs {
		result, err := s.settleContractLocked(id, currentSlot, settlementPrice)
		if err != nil {
			errs = append(errs, fmt.Errorf("contract %s: %w", id, err))
			continue
		}
		results = append(results, result)
	}
	return results, errs
}

// MarkToMarket computes the unrealized PnL for a position at the given
// current market price, without settling it.
func (s *GasFuturesSettlement) MarkToMarket(
	contractID string,
	currentPrice *big.Int,
) (*big.Int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pos, ok := s.positions[contractID]
	if !ok {
		return nil, ErrSettlementContractNotFound
	}
	if pos.Settled {
		// Return the final PnL from the settlement result.
		if result, exists := s.results[contractID]; exists {
			return new(big.Int).Set(result.PnL), nil
		}
		return nil, ErrSettlementAlreadySettled
	}

	return calcPnL(pos.Direction, pos.StrikePrice, currentPrice, pos.NotionalGas), nil
}

// ExpiryCheck settles all contracts that have reached their expiry slot.
// Returns the settlement results and any errors.
func (s *GasFuturesSettlement) ExpiryCheck(
	currentSlot uint64,
	settlementPrice *big.Int,
) ([]*SettlementResult, []error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var expired []string
	for id, pos := range s.positions {
		if !pos.Settled && currentSlot >= pos.ExpirySlot {
			expired = append(expired, id)
		}
	}

	results := make([]*SettlementResult, 0, len(expired))
	errs := make([]error, 0)
	for _, id := range expired {
		result, err := s.settleContractLocked(id, currentSlot, settlementPrice)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		results = append(results, result)
	}
	return results, errs
}

// CheckLiquidations identifies positions that are undercollateralized at
// the given current gas price and liquidates them by settling immediately.
// Returns the liquidated settlement results.
func (s *GasFuturesSettlement) CheckLiquidations(
	currentPrice *big.Int,
	currentSlot uint64,
) []*SettlementResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	var liquidated []*SettlementResult

	for id, pos := range s.positions {
		if pos.Settled {
			continue
		}
		if isUndercollateralized(pos, currentPrice, s.config.MaintenanceMarginBPS) {
			// Force-settle at current price.
			pnl := calcPnL(pos.Direction, pos.StrikePrice, currentPrice, pos.NotionalGas)
			result := &SettlementResult{
				ContractID:      id,
				SettlementPrice: new(big.Int).Set(currentPrice),
				StrikePrice:     new(big.Int).Set(pos.StrikePrice),
				PnL:             pnl,
				Direction:       pos.Direction,
				Settled:         true,
			}
			pos.Settled = true
			s.results[id] = result
			liquidated = append(liquidated, result)
		}
	}
	return liquidated
}

// AddCollateral adds collateral to an existing position and recalculates
// the liquidation price.
func (s *GasFuturesSettlement) AddCollateral(contractID string, amount *big.Int) error {
	if amount == nil || amount.Sign() <= 0 {
		return fmt.Errorf("%w: amount must be > 0", ErrSettlementInsufficientMargin)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pos, ok := s.positions[contractID]
	if !ok {
		return ErrSettlementContractNotFound
	}
	if pos.Settled {
		return ErrSettlementAlreadySettled
	}

	pos.Collateral.Add(pos.Collateral, amount)
	pos.LiquidationPrice = calcLiquidationPrice(
		pos.StrikePrice, pos.Direction, pos.NotionalGas,
		pos.Collateral, s.config.MaintenanceMarginBPS,
	)
	return nil
}

// PositionCount returns the total number of positions (settled and unsettled).
func (s *GasFuturesSettlement) PositionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.positions)
}

// OpenPositionCount returns the number of unsettled positions.
func (s *GasFuturesSettlement) OpenPositionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, p := range s.positions {
		if !p.Settled {
			count++
		}
	}
	return count
}

// SettledCount returns the number of settled positions.
func (s *GasFuturesSettlement) SettledCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.results)
}

// GetSettlementResult returns the result for a settled contract, or nil.
func (s *GasFuturesSettlement) GetSettlementResult(contractID string) *SettlementResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.results[contractID]
	if !ok {
		return nil
	}
	return &SettlementResult{
		ContractID:      r.ContractID,
		SettlementPrice: new(big.Int).Set(r.SettlementPrice),
		StrikePrice:     new(big.Int).Set(r.StrikePrice),
		PnL:             new(big.Int).Set(r.PnL),
		Direction:       r.Direction,
		Settled:         r.Settled,
	}
}

// --- Internal helpers ---

// calcPnL computes profit/loss based on direction.
//
//	Long PnL:  (settlement - strike) * notional
//	Short PnL: (strike - settlement) * notional
func calcPnL(direction uint8, strike, settlement *big.Int, notional uint64) *big.Int {
	diff := new(big.Int)
	if direction == DirectionLong {
		diff.Sub(settlement, strike)
	} else {
		diff.Sub(strike, settlement)
	}
	return diff.Mul(diff, new(big.Int).SetUint64(notional))
}

// calcMarginRequired computes the required margin:
//
//	strikePrice * notionalGas * marginBPS / 10000
func calcMarginRequired(strike *big.Int, notional uint64, marginBPS uint64) *big.Int {
	margin := new(big.Int).Mul(strike, new(big.Int).SetUint64(notional))
	margin.Mul(margin, new(big.Int).SetUint64(marginBPS))
	margin.Div(margin, big.NewInt(10000))
	return margin
}

// calcLiquidationPrice determines the price at which the position becomes
// undercollateralized.
//
// For long: liqPrice = strike - (collateral - maintenanceMargin) / notional
// For short: liqPrice = strike + (collateral - maintenanceMargin) / notional
func calcLiquidationPrice(
	strike *big.Int,
	direction uint8,
	notional uint64,
	collateral *big.Int,
	maintenanceBPS uint64,
) *big.Int {
	maintenanceReq := calcMarginRequired(strike, notional, maintenanceBPS)

	// buffer = collateral - maintenanceRequirement
	buffer := new(big.Int).Sub(collateral, maintenanceReq)

	// perUnit = buffer / notional
	perUnit := new(big.Int).Div(buffer, new(big.Int).SetUint64(notional))

	liqPrice := new(big.Int)
	if direction == DirectionLong {
		// Long is liquidated when price drops: liq = strike - perUnit
		liqPrice.Sub(strike, perUnit)
	} else {
		// Short is liquidated when price rises: liq = strike + perUnit
		liqPrice.Add(strike, perUnit)
	}

	// Floor at zero.
	if liqPrice.Sign() < 0 {
		liqPrice.SetInt64(0)
	}
	return liqPrice
}

// isUndercollateralized checks whether the position's unrealized loss exceeds
// the available collateral minus the maintenance margin.
func isUndercollateralized(pos *Position, currentPrice *big.Int, maintenanceBPS uint64) bool {
	pnl := calcPnL(pos.Direction, pos.StrikePrice, currentPrice, pos.NotionalGas)

	// If PnL is non-negative, position is not at risk.
	if pnl.Sign() >= 0 {
		return false
	}

	// Absolute loss.
	loss := new(big.Int).Neg(pnl)

	// maintenanceRequired = strike * notional * maintenanceBPS / 10000
	maintenanceReq := calcMarginRequired(pos.StrikePrice, pos.NotionalGas, maintenanceBPS)

	// Position is undercollateralized if: collateral - loss < maintenanceReq
	remaining := new(big.Int).Sub(pos.Collateral, loss)
	return remaining.Cmp(maintenanceReq) < 0
}

// copyPosition returns a deep copy of a Position.
func copyPosition(p *Position) *Position {
	return &Position{
		ContractID:       p.ContractID,
		Direction:        p.Direction,
		StrikePrice:      new(big.Int).Set(p.StrikePrice),
		NotionalGas:      p.NotionalGas,
		Collateral:       new(big.Int).Set(p.Collateral),
		LiquidationPrice: new(big.Int).Set(p.LiquidationPrice),
		ExpirySlot:       p.ExpirySlot,
		Settled:          p.Settled,
	}
}
