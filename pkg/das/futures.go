package das

import (
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"golang.org/x/crypto/sha3"
)

// Blob futures errors.
var (
	ErrFutureNotFound = errors.New("das: future not found")
	ErrFutureExpired  = errors.New("das: future already expired")
	ErrFutureSettled  = errors.New("das: future already settled")
	ErrInvalidExpiry  = errors.New("das: expiry slot must be in the future")
	ErrInvalidPrice   = errors.New("das: price must be positive")
)

// BlobFuture represents a short-dated futures contract on blob availability.
// The creator bets that a blob will be available by the expiry slot.
type BlobFuture struct {
	// ID is a unique identifier for this future (hash of creator + blobHash + expiry).
	ID types.Hash

	// ExpirySlot is the slot at which the future expires.
	ExpirySlot uint64

	// BlobHash is the versioned hash of the blob this future references.
	BlobHash types.Hash

	// Price is the amount staked on this future (in wei).
	Price *big.Int

	// Creator is the address that created the future.
	Creator types.Address

	// Settled indicates whether this future has been settled.
	Settled bool
}

// FuturesMarket manages blob availability futures.
type FuturesMarket struct {
	mu           sync.Mutex
	active       map[types.Hash]*BlobFuture // ID -> future
	byExpiry     map[uint64][]*BlobFuture   // expirySlot -> futures
	currentSlot  uint64
	totalVolume  *big.Int
	settledCount uint64
}

// NewFuturesMarket creates a new futures market starting at the given slot.
func NewFuturesMarket(currentSlot uint64) *FuturesMarket {
	return &FuturesMarket{
		active:      make(map[types.Hash]*BlobFuture),
		byExpiry:    make(map[uint64][]*BlobFuture),
		currentSlot: currentSlot,
		totalVolume: new(big.Int),
	}
}

// CreateFuture creates a new blob availability future.
func (fm *FuturesMarket) CreateFuture(blobHash types.Hash, expirySlot uint64, price *big.Int, creator types.Address) (*BlobFuture, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if expirySlot <= fm.currentSlot {
		return nil, ErrInvalidExpiry
	}
	if price == nil || price.Sign() <= 0 {
		return nil, ErrInvalidPrice
	}

	// Generate a deterministic ID.
	id := computeFutureID(blobHash, expirySlot, creator)

	future := &BlobFuture{
		ID:         id,
		ExpirySlot: expirySlot,
		BlobHash:   blobHash,
		Price:      new(big.Int).Set(price),
		Creator:    creator,
	}

	fm.active[id] = future
	fm.byExpiry[expirySlot] = append(fm.byExpiry[expirySlot], future)
	fm.totalVolume.Add(fm.totalVolume, price)

	return future, nil
}

// SettleFuture settles a future based on whether the blob was available.
// Returns the payout: if the blob was available, the creator gets 2x their stake;
// otherwise they get nothing (payout = 0).
func (fm *FuturesMarket) SettleFuture(futureID types.Hash, wasAvailable bool) (*big.Int, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	future, ok := fm.active[futureID]
	if !ok {
		return nil, ErrFutureNotFound
	}
	if future.Settled {
		return nil, ErrFutureSettled
	}

	future.Settled = true
	fm.settledCount++

	payout := new(big.Int)
	if wasAvailable {
		// Creator was correct: return 2x stake.
		payout.Mul(future.Price, big.NewInt(2))
	}
	// If not available, payout is zero.

	return payout, nil
}

// PriceFuture estimates the price of a blob availability future based on
// current network conditions. Uses a simple model: base price adjusted by
// time-to-expiry and current blob count.
func PriceFuture(currentSlot, expirySlot uint64, blobCount uint64) *big.Int {
	if expirySlot <= currentSlot {
		return new(big.Int)
	}

	// Base price: 1 Gwei per blob.
	gwei := big.NewInt(1_000_000_000)
	basePrice := new(big.Int).Mul(gwei, new(big.Int).SetUint64(blobCount))
	if basePrice.Sign() == 0 {
		basePrice.Set(gwei) // minimum 1 Gwei
	}

	// Time discount: longer expiry -> higher price (more uncertainty).
	slotsRemaining := expirySlot - currentSlot
	timeFactor := new(big.Int).SetUint64(slotsRemaining)

	// Price = basePrice * sqrt(slotsRemaining), approximated as
	// basePrice * slotsRemaining / 32 (with floor of basePrice).
	price := new(big.Int).Mul(basePrice, timeFactor)
	price.Div(price, big.NewInt(32))

	if price.Cmp(basePrice) < 0 {
		price.Set(basePrice)
	}

	return price
}

// ExpireFutures removes all futures that have expired at or before the given slot.
// Returns the number of futures expired.
func (fm *FuturesMarket) ExpireFutures(currentSlot uint64) int {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.currentSlot = currentSlot
	expired := 0

	for slot, futures := range fm.byExpiry {
		if slot > currentSlot {
			continue
		}
		for _, f := range futures {
			if !f.Settled {
				f.Settled = true
				fm.settledCount++
			}
			delete(fm.active, f.ID)
			expired++
		}
		delete(fm.byExpiry, slot)
	}

	return expired
}

// ActiveCount returns the number of active (unsettled) futures.
func (fm *FuturesMarket) ActiveCount() int {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	return len(fm.active)
}

// TotalVolume returns the total volume of all futures created.
func (fm *FuturesMarket) TotalVolume() *big.Int {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	return new(big.Int).Set(fm.totalVolume)
}

// computeFutureID generates a deterministic ID for a future.
func computeFutureID(blobHash types.Hash, expirySlot uint64, creator types.Address) types.Hash {
	// Simple hash: keccak256(blobHash || expirySlot || creator).
	var buf [32 + 8 + 20]byte
	copy(buf[:32], blobHash[:])
	buf[32] = byte(expirySlot)
	buf[33] = byte(expirySlot >> 8)
	buf[34] = byte(expirySlot >> 16)
	buf[35] = byte(expirySlot >> 24)
	buf[36] = byte(expirySlot >> 32)
	buf[37] = byte(expirySlot >> 40)
	buf[38] = byte(expirySlot >> 48)
	buf[39] = byte(expirySlot >> 56)
	copy(buf[40:], creator[:])

	return keccak256(buf[:])
}

// keccak256 computes the Keccak-256 hash of the input.
func keccak256(data []byte) types.Hash {
	h := newKeccak256()
	h.Write(data)
	var result types.Hash
	h.Sum(result[:0])
	return result
}

// newKeccak256 returns a new Keccak-256 hasher.
func newKeccak256() interface {
	Write([]byte) (int, error)
	Sum([]byte) []byte
} {
	return sha3.NewLegacyKeccak256()
}
