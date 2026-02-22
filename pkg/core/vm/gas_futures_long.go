// gas_futures_long.go implements long-dated gas futures contracts (M+ upgrade).
// Unlike short-dated futures (settled per-slot or per-block), long-dated futures
// use epochs as time units, supporting contracts up to 365 epochs for long-term
// gas price hedging. This is part of the EL throughput roadmap for predictable
// gas economics.
package vm

import (
	"encoding/binary"
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// MaxLongFutureEpochs is the maximum number of epochs a long-dated future can span.
const MaxLongFutureEpochs = 365

// Long-dated futures errors.
var (
	ErrLongFutureNotFound     = errors.New("long_futures: future not found")
	ErrLongFutureAlreadySettled = errors.New("long_futures: future already settled")
	ErrLongFutureExpired      = errors.New("long_futures: future has expired")
	ErrLongFutureNotExpired   = errors.New("long_futures: future has not expired yet")
	ErrLongFutureTooLong      = errors.New("long_futures: exceeds max epoch duration")
	ErrLongFutureZeroAmount   = errors.New("long_futures: amount must be > 0")
	ErrLongFutureZeroStrike   = errors.New("long_futures: strike price must be > 0")
	ErrLongFutureInvalidExpiry = errors.New("long_futures: expiry must be > created epoch")
)

// LongDatedFuture represents a gas futures contract denominated in epochs.
type LongDatedFuture struct {
	ID           types.Hash    // unique contract identifier
	Buyer        types.Address // party buying gas capacity
	Seller       types.Address // party selling gas capacity
	StrikePrice  *big.Int      // agreed gas price per unit (in wei)
	ExpiryEpoch  uint64        // epoch at which the future settles
	Amount       uint64        // gas units covered
	Settled      bool          // whether the contract has been settled
	CreatedEpoch uint64        // epoch when the contract was created
}

// LongSettlement records the outcome of settling a long-dated future.
type LongSettlement struct {
	FutureID       types.Hash
	ActualGasPrice *big.Int
	Payout         *big.Int
	Winner         types.Address
	SettledEpoch   uint64
}

// MarketDepth represents the depth of the long-dated futures market.
type MarketDepth struct {
	BuyOrders  int      // number of open buy orders
	SellOrders int      // number of open sell orders
	SpreadBps  uint64   // spread in basis points (0.01%)
	MidPrice   *big.Int // midpoint price between best buy and sell
}

// FuturesMarketLong manages long-dated gas futures contracts.
// All operations are thread-safe.
type FuturesMarketLong struct {
	mu         sync.RWMutex
	futures    map[types.Hash]*LongDatedFuture
	settlements map[types.Hash]*LongSettlement
	nonce      uint64
}

// NewFuturesMarketLong creates a new long-dated futures market.
func NewFuturesMarketLong() *FuturesMarketLong {
	return &FuturesMarketLong{
		futures:     make(map[types.Hash]*LongDatedFuture),
		settlements: make(map[types.Hash]*LongSettlement),
	}
}

// CreateLongFuture creates a new long-dated gas futures contract.
func (m *FuturesMarketLong) CreateLongFuture(
	buyer, seller types.Address,
	strikePrice *big.Int,
	createdEpoch, expiryEpoch uint64,
	amount uint64,
) (*LongDatedFuture, error) {
	if amount == 0 {
		return nil, ErrLongFutureZeroAmount
	}
	if strikePrice == nil || strikePrice.Sign() <= 0 {
		return nil, ErrLongFutureZeroStrike
	}
	if expiryEpoch <= createdEpoch {
		return nil, ErrLongFutureInvalidExpiry
	}
	if expiryEpoch-createdEpoch > MaxLongFutureEpochs {
		return nil, ErrLongFutureTooLong
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	id := deriveLongFutureID(buyer, seller, createdEpoch, m.nonce)
	m.nonce++

	future := &LongDatedFuture{
		ID:           id,
		Buyer:        buyer,
		Seller:       seller,
		StrikePrice:  new(big.Int).Set(strikePrice),
		ExpiryEpoch:  expiryEpoch,
		Amount:       amount,
		Settled:      false,
		CreatedEpoch: createdEpoch,
	}
	m.futures[id] = future
	return future, nil
}

// SettleLongFuture settles a long-dated future against the actual gas price.
// The payout is |actualGasPrice - strikePrice| * amount.
// The buyer wins if actualGasPrice > strikePrice; the seller wins otherwise.
func (m *FuturesMarketLong) SettleLongFuture(
	futureID types.Hash,
	currentEpoch uint64,
	actualGasPrice *big.Int,
) (*LongSettlement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	future, ok := m.futures[futureID]
	if !ok {
		return nil, ErrLongFutureNotFound
	}
	if future.Settled {
		return nil, ErrLongFutureAlreadySettled
	}
	if currentEpoch < future.ExpiryEpoch {
		return nil, ErrLongFutureNotExpired
	}

	diff := new(big.Int).Sub(actualGasPrice, future.StrikePrice)
	payout := new(big.Int).Mul(new(big.Int).Abs(diff), new(big.Int).SetUint64(future.Amount))

	var winner types.Address
	if actualGasPrice.Cmp(future.StrikePrice) > 0 {
		winner = future.Buyer
	} else {
		winner = future.Seller
	}

	future.Settled = true

	settlement := &LongSettlement{
		FutureID:       futureID,
		ActualGasPrice: new(big.Int).Set(actualGasPrice),
		Payout:         payout,
		Winner:         winner,
		SettledEpoch:   currentEpoch,
	}
	m.settlements[futureID] = settlement
	return settlement, nil
}

// ExpireLongFutures marks all unsettled futures whose expiry epoch has passed
// as settled with a zero gas price (seller wins by default). Returns the count
// of expired futures.
func (m *FuturesMarketLong) ExpireLongFutures(currentEpoch uint64) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for id, f := range m.futures {
		if !f.Settled && currentEpoch >= f.ExpiryEpoch {
			f.Settled = true
			payout := new(big.Int).Mul(f.StrikePrice, new(big.Int).SetUint64(f.Amount))
			m.settlements[id] = &LongSettlement{
				FutureID:       id,
				ActualGasPrice: new(big.Int),
				Payout:         payout,
				Winner:         f.Seller,
				SettledEpoch:   currentEpoch,
			}
			count++
		}
	}
	return count
}

// GetOpenInterest returns the total gas amount covered by unsettled futures.
func (m *FuturesMarketLong) GetOpenInterest() *big.Int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := new(big.Int)
	for _, f := range m.futures {
		if !f.Settled {
			total.Add(total, new(big.Int).SetUint64(f.Amount))
		}
	}
	return total
}

// GetMarketDepth computes the market depth from open (unsettled) futures.
// Buy orders are futures where the buyer's strike is below the average price,
// sell orders are where the strike is above average. SpreadBps and MidPrice
// are derived from the minimum and maximum strike prices.
func (m *FuturesMarketLong) GetMarketDepth() *MarketDepth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var strikes []*big.Int
	for _, f := range m.futures {
		if !f.Settled {
			strikes = append(strikes, new(big.Int).Set(f.StrikePrice))
		}
	}

	if len(strikes) == 0 {
		return &MarketDepth{
			MidPrice: new(big.Int),
		}
	}

	// Sort strike prices to find min/max.
	sort.Slice(strikes, func(i, j int) bool {
		return strikes[i].Cmp(strikes[j]) < 0
	})

	minStrike := strikes[0]
	maxStrike := strikes[len(strikes)-1]

	// MidPrice = (min + max) / 2
	midPrice := new(big.Int).Add(minStrike, maxStrike)
	midPrice.Div(midPrice, big.NewInt(2))

	// Count buy/sell orders relative to mid price.
	buyOrders := 0
	sellOrders := 0
	for _, s := range strikes {
		if s.Cmp(midPrice) <= 0 {
			buyOrders++
		} else {
			sellOrders++
		}
	}

	// SpreadBps = (max - min) * 10000 / midPrice
	var spreadBps uint64
	if midPrice.Sign() > 0 {
		spread := new(big.Int).Sub(maxStrike, minStrike)
		spread.Mul(spread, big.NewInt(10000))
		spread.Div(spread, midPrice)
		spreadBps = spread.Uint64()
	}

	return &MarketDepth{
		BuyOrders:  buyOrders,
		SellOrders: sellOrders,
		SpreadBps:  spreadBps,
		MidPrice:   midPrice,
	}
}

// GetFuture returns a future by ID, or nil if not found.
func (m *FuturesMarketLong) GetFuture(id types.Hash) *LongDatedFuture {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.futures[id]
}

// GetSettlement returns a settlement by future ID, or nil if not found.
func (m *FuturesMarketLong) GetSettlement(id types.Hash) *LongSettlement {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settlements[id]
}

// OpenCount returns the number of unsettled futures.
func (m *FuturesMarketLong) OpenCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, f := range m.futures {
		if !f.Settled {
			count++
		}
	}
	return count
}

// SettledCount returns the number of settled futures.
func (m *FuturesMarketLong) SettledCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.settlements)
}

// deriveLongFutureID computes a unique ID from buyer, seller, epoch, and nonce.
func deriveLongFutureID(buyer, seller types.Address, epoch, nonce uint64) types.Hash {
	// 20 (buyer) + 20 (seller) + 8 (epoch) + 8 (nonce) = 56 bytes
	data := make([]byte, 0, 56)
	data = append(data, buyer[:]...)
	data = append(data, seller[:]...)
	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], epoch)
	data = append(data, tmp[:]...)
	binary.BigEndian.PutUint64(tmp[:], nonce)
	data = append(data, tmp[:]...)
	return crypto.Keccak256Hash(data)
}
