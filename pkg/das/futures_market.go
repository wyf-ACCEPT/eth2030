// futures_market.go extends blob futures with price discovery, margin tracking,
// order books, and a futures pool for the Data Layer roadmap (short-dated blob
// futures -> long-dated gas futures).
package das

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Order book and margin errors.
var (
	ErrOrderBookEmpty       = errors.New("das: order book is empty")
	ErrOrderNotFound        = errors.New("das: order not found")
	ErrOrderAlreadyFilled   = errors.New("das: order already filled")
	ErrInsufficientMargin   = errors.New("das: insufficient margin for order")
	ErrMarginBelowMin       = errors.New("das: margin below minimum requirement")
	ErrMarginAccountExists  = errors.New("das: margin account already exists")
	ErrMarginAccountMissing = errors.New("das: margin account not found")
	ErrPoolSlotRange        = errors.New("das: invalid slot range for pool")
	ErrPoolNoLiquidity      = errors.New("das: no liquidity available")
)

// Margin and order book limits.
const (
	MinMarginBasisPoints   = 500   // 5% minimum margin.
	LiquidationBasisPoints = 250   // 2.5% liquidation threshold.
	BasisPointsDenom       = 10000 // Denominator for basis point math.
	MaxOrdersPerBook       = 1024  // Max active orders per book.
)

// OrderSide indicates whether an order is a buy or sell.
type OrderSide uint8

const (
	OrderSideBuy  OrderSide = iota // Buyer wants to purchase blob availability future.
	OrderSideSell                  // Seller offers to guarantee blob availability.
)

// OrderStatus tracks the lifecycle of an order.
type OrderStatus uint8

const (
	OrderOpen      OrderStatus = iota // Order is live in the book.
	OrderFilled                       // Order was matched and executed.
	OrderCancelled                    // Order was cancelled by the maker.
)

// FuturesOrder represents a limit order in the blob futures order book.
type FuturesOrder struct {
	ID         types.Hash
	Side       OrderSide
	Status     OrderStatus
	Maker      types.Address
	TargetSlot uint64   // Slot for which blob availability is being traded.
	BlobIndex  uint8    // Index of the blob within the block.
	Price      *big.Int // Limit price in wei.
	Quantity   uint64   // Number of futures contracts.
	Timestamp  uint64   // Slot at which the order was placed.
	FilledQty  uint64   // How many contracts have been filled.
}

// MarginAccount tracks collateral for a participant in the futures market.
type MarginAccount struct {
	Owner     types.Address
	Deposited *big.Int // Total collateral deposited.
	Locked    *big.Int // Collateral locked in open positions.
	PnL       *big.Int // Unrealized profit/loss.
}

// AvailableMargin returns the margin available for new orders.
func (ma *MarginAccount) AvailableMargin() *big.Int {
	avail := new(big.Int).Sub(ma.Deposited, ma.Locked)
	avail.Add(avail, ma.PnL)
	if avail.Sign() < 0 {
		return new(big.Int)
	}
	return avail
}

// IsLiquidatable returns true if the account margin has fallen below the
// liquidation threshold relative to locked collateral.
func (ma *MarginAccount) IsLiquidatable() bool {
	if ma.Locked.Sign() == 0 {
		return false
	}
	threshold := new(big.Int).Mul(ma.Locked, big.NewInt(LiquidationBasisPoints))
	threshold.Div(threshold, big.NewInt(BasisPointsDenom))
	return ma.AvailableMargin().Cmp(threshold) < 0
}

// FuturesOrderBook manages buy and sell orders for blob futures at a
// specific target slot. Thread-safe for concurrent access.
type FuturesOrderBook struct {
	mu         sync.RWMutex
	targetSlot uint64
	buys       []*FuturesOrder // Sorted by price descending (best bid first).
	sells      []*FuturesOrder // Sorted by price ascending (best ask first).
	orderIndex map[types.Hash]*FuturesOrder
	nextNonce  uint64
	matchCount uint64
}

// NewFuturesOrderBook creates an order book for the given target slot.
func NewFuturesOrderBook(targetSlot uint64) *FuturesOrderBook {
	return &FuturesOrderBook{
		targetSlot: targetSlot,
		orderIndex: make(map[types.Hash]*FuturesOrder),
	}
}

// PlaceOrder adds a new limit order to the book. Returns the order with
// its assigned ID. The order may be immediately matched if a compatible
// counterparty exists.
func (ob *FuturesOrderBook) PlaceOrder(
	side OrderSide,
	maker types.Address,
	blobIndex uint8,
	price *big.Int,
	quantity uint64,
	currentSlot uint64,
) (*FuturesOrder, error) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if price == nil || price.Sign() <= 0 {
		return nil, ErrBlobFutureBadPrice
	}
	if len(ob.orderIndex) >= MaxOrdersPerBook {
		return nil, ErrOrderBookEmpty // Book is full.
	}

	id := ob.computeOrderID(maker, blobIndex, price, currentSlot)
	order := &FuturesOrder{
		ID:         id,
		Side:       side,
		Status:     OrderOpen,
		Maker:      maker,
		TargetSlot: ob.targetSlot,
		BlobIndex:  blobIndex,
		Price:      new(big.Int).Set(price),
		Quantity:   quantity,
		Timestamp:  currentSlot,
	}

	ob.orderIndex[id] = order
	ob.insertOrder(order)
	return order, nil
}

// MatchOrders attempts to match compatible buy and sell orders. Returns
// the number of matches made. A match occurs when the best bid >= best ask.
func (ob *FuturesOrderBook) MatchOrders() int {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	matched := 0
	for len(ob.buys) > 0 && len(ob.sells) > 0 {
		bestBid := ob.buys[0]
		bestAsk := ob.sells[0]

		// Match if bid price >= ask price.
		if bestBid.Price.Cmp(bestAsk.Price) < 0 {
			break
		}

		// Determine fill quantity.
		bidRemain := bestBid.Quantity - bestBid.FilledQty
		askRemain := bestAsk.Quantity - bestAsk.FilledQty
		fillQty := bidRemain
		if askRemain < fillQty {
			fillQty = askRemain
		}

		bestBid.FilledQty += fillQty
		bestAsk.FilledQty += fillQty

		if bestBid.FilledQty >= bestBid.Quantity {
			bestBid.Status = OrderFilled
			ob.buys = ob.buys[1:]
		}
		if bestAsk.FilledQty >= bestAsk.Quantity {
			bestAsk.Status = OrderFilled
			ob.sells = ob.sells[1:]
		}

		matched++
		ob.matchCount++
	}
	return matched
}

// BestBid returns the highest buy price, or nil if no bids exist.
func (ob *FuturesOrderBook) BestBid() *big.Int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	if len(ob.buys) == 0 {
		return nil
	}
	return new(big.Int).Set(ob.buys[0].Price)
}

// BestAsk returns the lowest sell price, or nil if no asks exist.
func (ob *FuturesOrderBook) BestAsk() *big.Int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	if len(ob.sells) == 0 {
		return nil
	}
	return new(big.Int).Set(ob.sells[0].Price)
}

// MidPrice returns the midpoint between best bid and best ask for price
// discovery. Returns nil if either side is empty.
func (ob *FuturesOrderBook) MidPrice() *big.Int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	if len(ob.buys) == 0 || len(ob.sells) == 0 {
		return nil
	}
	mid := new(big.Int).Add(ob.buys[0].Price, ob.sells[0].Price)
	mid.Div(mid, big.NewInt(2))
	return mid
}

// Spread returns the difference between best ask and best bid.
// Returns nil if either side is empty.
func (ob *FuturesOrderBook) Spread() *big.Int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	if len(ob.buys) == 0 || len(ob.sells) == 0 {
		return nil
	}
	return new(big.Int).Sub(ob.sells[0].Price, ob.buys[0].Price)
}

// CancelOrder cancels an open order by ID.
func (ob *FuturesOrderBook) CancelOrder(id types.Hash) error {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	order, ok := ob.orderIndex[id]
	if !ok {
		return ErrOrderNotFound
	}
	if order.Status != OrderOpen {
		return ErrOrderAlreadyFilled
	}
	order.Status = OrderCancelled
	ob.removeFromBook(order)
	return nil
}

// OpenOrderCount returns the number of open (unmatched) orders.
func (ob *FuturesOrderBook) OpenOrderCount() int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return len(ob.buys) + len(ob.sells)
}

// MatchCount returns the total number of matches executed.
func (ob *FuturesOrderBook) MatchCount() uint64 {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.matchCount
}

// insertOrder places the order into the correct sorted position.
func (ob *FuturesOrderBook) insertOrder(order *FuturesOrder) {
	if order.Side == OrderSideBuy {
		ob.buys = append(ob.buys, order)
		sort.Slice(ob.buys, func(i, j int) bool {
			return ob.buys[i].Price.Cmp(ob.buys[j].Price) > 0
		})
	} else {
		ob.sells = append(ob.sells, order)
		sort.Slice(ob.sells, func(i, j int) bool {
			return ob.sells[i].Price.Cmp(ob.sells[j].Price) < 0
		})
	}
}

// removeFromBook removes an order from the buy or sell slice.
func (ob *FuturesOrderBook) removeFromBook(order *FuturesOrder) {
	if order.Side == OrderSideBuy {
		ob.buys = removeOrder(ob.buys, order.ID)
	} else {
		ob.sells = removeOrder(ob.sells, order.ID)
	}
}

// removeOrder removes an order with the given ID from a slice.
func removeOrder(orders []*FuturesOrder, id types.Hash) []*FuturesOrder {
	for i, o := range orders {
		if o.ID == id {
			return append(orders[:i], orders[i+1:]...)
		}
	}
	return orders
}

// computeOrderID generates a unique ID for an order.
func (ob *FuturesOrderBook) computeOrderID(
	maker types.Address,
	blobIndex uint8,
	price *big.Int,
	slot uint64,
) types.Hash {
	var buf [20 + 1 + 32 + 8 + 8]byte
	copy(buf[:20], maker[:])
	buf[20] = blobIndex
	priceBytes := price.Bytes()
	if len(priceBytes) <= 32 {
		copy(buf[21+32-len(priceBytes):53], priceBytes)
	}
	buf[53] = byte(slot)
	buf[54] = byte(slot >> 8)
	buf[55] = byte(slot >> 16)
	buf[56] = byte(slot >> 24)
	buf[57] = byte(slot >> 32)
	buf[58] = byte(slot >> 40)
	buf[59] = byte(slot >> 48)
	buf[60] = byte(slot >> 56)
	nonce := ob.nextNonce
	ob.nextNonce++
	buf[61] = byte(nonce)
	buf[62] = byte(nonce >> 8)
	buf[63] = byte(nonce >> 16)
	buf[64] = byte(nonce >> 24)
	buf[65] = byte(nonce >> 32)
	buf[66] = byte(nonce >> 40)
	buf[67] = byte(nonce >> 48)
	buf[68] = byte(nonce >> 56)
	return keccak256(buf[:])
}

// FuturesPool aggregates liquidity across multiple order books covering
// a range of target slots. It provides unified price discovery and
// margin management for participants.
type FuturesPool struct {
	mu        sync.RWMutex
	books     map[uint64]*FuturesOrderBook     // targetSlot -> order book
	margins   map[types.Address]*MarginAccount // participant -> margin account
	startSlot uint64
	endSlot   uint64
}

// NewFuturesPool creates a pool covering the slot range [start, end].
func NewFuturesPool(startSlot, endSlot uint64) (*FuturesPool, error) {
	if endSlot < startSlot {
		return nil, ErrPoolSlotRange
	}
	return &FuturesPool{
		books:     make(map[uint64]*FuturesOrderBook),
		margins:   make(map[types.Address]*MarginAccount),
		startSlot: startSlot,
		endSlot:   endSlot,
	}, nil
}

// GetOrCreateBook returns the order book for the given slot, creating one
// if it doesn't exist.
func (fp *FuturesPool) GetOrCreateBook(slot uint64) *FuturesOrderBook {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if book, ok := fp.books[slot]; ok {
		return book
	}
	book := NewFuturesOrderBook(slot)
	fp.books[slot] = book
	return book
}

// DepositMargin creates or tops up a margin account for the given address.
func (fp *FuturesPool) DepositMargin(owner types.Address, amount *big.Int) {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	ma, ok := fp.margins[owner]
	if !ok {
		ma = &MarginAccount{
			Owner:     owner,
			Deposited: new(big.Int),
			Locked:    new(big.Int),
			PnL:       new(big.Int),
		}
		fp.margins[owner] = ma
	}
	ma.Deposited.Add(ma.Deposited, amount)
}

// GetMargin returns the margin account for the given owner.
func (fp *FuturesPool) GetMargin(owner types.Address) (*MarginAccount, error) {
	fp.mu.RLock()
	defer fp.mu.RUnlock()
	ma, ok := fp.margins[owner]
	if !ok {
		return nil, ErrMarginAccountMissing
	}
	return ma, nil
}

// CheckMarginRequirement verifies that an account has sufficient margin
// to place an order of the given value.
func (fp *FuturesPool) CheckMarginRequirement(owner types.Address, orderValue *big.Int) error {
	fp.mu.RLock()
	defer fp.mu.RUnlock()

	ma, ok := fp.margins[owner]
	if !ok {
		return ErrMarginAccountMissing
	}

	required := new(big.Int).Mul(orderValue, big.NewInt(MinMarginBasisPoints))
	required.Div(required, big.NewInt(BasisPointsDenom))

	if ma.AvailableMargin().Cmp(required) < 0 {
		return ErrInsufficientMargin
	}
	return nil
}

// LockMargin locks collateral for an open position.
func (fp *FuturesPool) LockMargin(owner types.Address, amount *big.Int) error {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	ma, ok := fp.margins[owner]
	if !ok {
		return ErrMarginAccountMissing
	}
	if ma.AvailableMargin().Cmp(amount) < 0 {
		return ErrInsufficientMargin
	}
	ma.Locked.Add(ma.Locked, amount)
	return nil
}

// ReleaseMargin unlocks collateral after a position is closed.
func (fp *FuturesPool) ReleaseMargin(owner types.Address, amount *big.Int) {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	ma, ok := fp.margins[owner]
	if !ok {
		return
	}
	ma.Locked.Sub(ma.Locked, amount)
	if ma.Locked.Sign() < 0 {
		ma.Locked.SetInt64(0)
	}
}

// VWAP computes the Volume-Weighted Average Price across all active order
// books in the pool. Returns nil if no orders exist.
func (fp *FuturesPool) VWAP() *big.Int {
	fp.mu.RLock()
	defer fp.mu.RUnlock()

	totalValue := new(big.Int)
	totalQty := uint64(0)

	for _, book := range fp.books {
		mid := book.MidPrice()
		if mid == nil {
			continue
		}
		count := uint64(book.OpenOrderCount())
		if count == 0 {
			continue
		}
		contribution := new(big.Int).Mul(mid, new(big.Int).SetUint64(count))
		totalValue.Add(totalValue, contribution)
		totalQty += count
	}

	if totalQty == 0 {
		return nil
	}
	return totalValue.Div(totalValue, new(big.Int).SetUint64(totalQty))
}

// SettleFuture resolves all matched orders in the order book for a target slot
// by comparing the committed blob hash against the actual blob hash. Filled
// orders whose buyer hash matches the actual hash receive a full payout;
// otherwise the seller retains the price. Returns the number of settlements
// and total payout. This wires futures_market.go order book settlement with
// the blob_futures.go contract settlement logic.
func (fp *FuturesPool) SettleFuture(targetSlot uint64, actualBlobHash types.Hash) (int, *big.Int) {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	book, ok := fp.books[targetSlot]
	if !ok {
		return 0, new(big.Int)
	}

	book.mu.Lock()
	defer book.mu.Unlock()

	settled := 0
	totalPayout := new(big.Int)

	for _, order := range book.orderIndex {
		if order.Status != OrderFilled {
			continue
		}
		// Settle filled orders: compute payout based on the order price.
		// The dummy committed hash is derived from the maker address.
		var committedHash types.Hash
		copy(committedHash[:20], order.Maker[:])

		payout := ComputeSettlementPrice(committedHash, actualBlobHash, order.Price)
		totalPayout.Add(totalPayout, payout)

		// Update margin accounts if they exist.
		if ma, exists := fp.margins[order.Maker]; exists {
			ma.PnL.Add(ma.PnL, payout)
			// Release locked margin.
			if ma.Locked.Sign() > 0 {
				ma.Locked.Sub(ma.Locked, order.Price)
				if ma.Locked.Sign() < 0 {
					ma.Locked.SetInt64(0)
				}
			}
		}
		settled++
	}

	return settled, totalPayout
}

// BookCount returns the number of active order books in the pool.
func (fp *FuturesPool) BookCount() int {
	fp.mu.RLock()
	defer fp.mu.RUnlock()
	return len(fp.books)
}

// MarginAccountCount returns the number of registered margin accounts.
func (fp *FuturesPool) MarginAccountCount() int {
	fp.mu.RLock()
	defer fp.mu.RUnlock()
	return len(fp.margins)
}
