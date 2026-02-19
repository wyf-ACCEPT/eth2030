package core

import (
	"errors"
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Gas futures errors.
var (
	ErrFutureExpired     = errors.New("gas future has expired")
	ErrFutureNotFound    = errors.New("gas future not found")
	ErrFutureAlreadyOpen = errors.New("gas future already exists")
)

// GasFuture represents a gas price futures contract between two parties.
// The Long party benefits when actual gas price exceeds StrikePrice;
// the Short party benefits when it is below.
type GasFuture struct {
	ID          types.Hash   // unique contract identifier
	ExpiryBlock uint64       // block number at which the future settles
	StrikePrice *big.Int     // agreed gas price (in wei)
	Volume      uint64       // amount of gas covered by the contract
	Long        types.Address // party betting gas price goes up
	Short       types.Address // party betting gas price goes down
}

// Settlement records the outcome of a settled gas future.
type Settlement struct {
	FutureID       types.Hash
	ActualGasPrice *big.Int
	Payout         *big.Int     // amount transferred from loser to winner
	Winner         types.Address // party receiving the payout
}

// GasFuturesMarket manages open and settled gas futures contracts.
type GasFuturesMarket struct {
	mu               sync.RWMutex
	openContracts    map[types.Hash]*GasFuture
	settledContracts map[types.Hash]*Settlement
	nextID           uint64
}

// NewGasFuturesMarket creates a new gas futures market.
func NewGasFuturesMarket() *GasFuturesMarket {
	return &GasFuturesMarket{
		openContracts:    make(map[types.Hash]*GasFuture),
		settledContracts: make(map[types.Hash]*Settlement),
	}
}

// CreateGasFuture creates a new gas futures contract.
func (m *GasFuturesMarket) CreateGasFuture(expiryBlock uint64, strikePrice *big.Int, volume uint64, long, short types.Address) *GasFuture {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextID++
	// Generate a deterministic ID from the counter.
	var id types.Hash
	idBytes := new(big.Int).SetUint64(m.nextID).Bytes()
	copy(id[types.HashLength-len(idBytes):], idBytes)

	future := &GasFuture{
		ID:          id,
		ExpiryBlock: expiryBlock,
		StrikePrice: new(big.Int).Set(strikePrice),
		Volume:      volume,
		Long:        long,
		Short:       short,
	}
	m.openContracts[id] = future
	return future
}

// SettleGasFuture settles a gas future against the actual gas price.
// The payout is: |actualGasPrice - strikePrice| * volume.
// The Long wins if actualGasPrice > strikePrice; the Short wins otherwise.
func (m *GasFuturesMarket) SettleGasFuture(futureID types.Hash, actualGasPrice *big.Int) (*Settlement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	future, ok := m.openContracts[futureID]
	if !ok {
		return nil, ErrFutureNotFound
	}

	diff := new(big.Int).Sub(actualGasPrice, future.StrikePrice)
	payout := new(big.Int).Mul(diff.Abs(diff), new(big.Int).SetUint64(future.Volume))

	var winner types.Address
	if actualGasPrice.Cmp(future.StrikePrice) > 0 {
		winner = future.Long
	} else {
		winner = future.Short
	}

	settlement := &Settlement{
		FutureID:       futureID,
		ActualGasPrice: new(big.Int).Set(actualGasPrice),
		Payout:         payout,
		Winner:         winner,
	}

	m.settledContracts[futureID] = settlement
	delete(m.openContracts, futureID)
	return settlement, nil
}

// GetOpenInterest returns the total gas volume covered by all open contracts.
func (m *GasFuturesMarket) GetOpenInterest() *big.Int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := new(big.Int)
	for _, f := range m.openContracts {
		total.Add(total, new(big.Int).SetUint64(f.Volume))
	}
	return total
}

// PriceGasFuture estimates the fair value of a gas future using a simplified
// model: price = strikePrice * (expiryBlock - currentBlock) * volatility / 10000.
// This is a placeholder for a more sophisticated pricing model.
func PriceGasFuture(currentBlock, expiryBlock uint64, strikePrice *big.Int, volatility uint64) *big.Int {
	if currentBlock >= expiryBlock {
		return new(big.Int)
	}
	blocksToExpiry := expiryBlock - currentBlock
	// price = strikePrice * blocksToExpiry * volatility / 10_000_000
	price := new(big.Int).Set(strikePrice)
	price.Mul(price, new(big.Int).SetUint64(blocksToExpiry))
	price.Mul(price, new(big.Int).SetUint64(volatility))
	price.Div(price, big.NewInt(10_000_000))
	return price
}

// ExpiryCleanup settles expired futures with a zero gas price (forfeiture)
// and removes them from the open set.
func (m *GasFuturesMarket) ExpiryCleanup(currentBlock uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var expired []types.Hash
	for id, f := range m.openContracts {
		if f.ExpiryBlock <= currentBlock {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		future := m.openContracts[id]
		// Expired without settlement: Short wins by default (gas price = 0).
		settlement := &Settlement{
			FutureID:       id,
			ActualGasPrice: new(big.Int),
			Payout:         new(big.Int).Mul(future.StrikePrice, new(big.Int).SetUint64(future.Volume)),
			Winner:         future.Short,
		}
		m.settledContracts[id] = settlement
		delete(m.openContracts, id)
	}
}

// OpenContractCount returns the number of open futures contracts.
func (m *GasFuturesMarket) OpenContractCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.openContracts)
}

// SettledContractCount returns the number of settled futures contracts.
func (m *GasFuturesMarket) SettledContractCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.settledContracts)
}
