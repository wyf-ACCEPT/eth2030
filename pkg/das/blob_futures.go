// Package das implements blob futures contracts for the Data Layer roadmap.
// This file adds short-dated and long-dated blob futures as described in the
// DL track: "short-dated blob futures" and "long-dated gas futures".
//
// A blob future is a financial instrument where a buyer and seller agree on a
// price for blob data availability at a future slot. Settlement is based on
// whether the committed hash matches the actual blob hash at the target slot.
package das

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// FutureType distinguishes short-dated from long-dated blob futures.
type FutureType uint8

const (
	// ShortDatedFuture expires within 256 slots (~51 min at 12s slots).
	ShortDatedFuture FutureType = iota
	// LongDatedFuture can expire up to 32768 slots (~4.5 days) in the future.
	LongDatedFuture
)

// Short vs long dated boundary (in slots).
const (
	ShortDatedMaxSlots = 256
	LongDatedMaxSlots  = 32768
)

// FutureStatus tracks the lifecycle of a blob future.
type FutureStatus uint8

const (
	FutureActive    FutureStatus = iota // Created, not yet settled or expired.
	FutureSettled                       // Settled with actual blob hash.
	FutureExpired                       // Past expiry slot without settlement.
	FutureCancelled                     // Cancelled by the buyer before expiry.
)

// Errors specific to the blob futures market.
var (
	ErrBlobFutureNotFound    = errors.New("das: blob future not found")
	ErrBlobFutureNotActive   = errors.New("das: blob future is not active")
	ErrBlobFutureInvalidSlot = errors.New("das: target slot must be in the future")
	ErrBlobFutureBadExpiry   = errors.New("das: expiry exceeds maximum for future type")
	ErrBlobFutureBadPrice    = errors.New("das: price must be positive")
	ErrBlobFutureBadIndex    = errors.New("das: blob index out of range")
	ErrBlobFutureDuplicate   = errors.New("das: duplicate future ID")
)

// BlobFutureContract represents a futures contract on a specific blob at a
// specific slot. The buyer pays the price upfront. At settlement, payout
// depends on whether the committed hash matches the actual blob hash.
type BlobFutureContract struct {
	ID            types.Hash   // Deterministic identifier.
	FType         FutureType   // Short-dated or long-dated.
	Status        FutureStatus // Current lifecycle status.
	Slot          uint64       // Target slot for the blob.
	BlobIndex     uint8        // Index of the blob within the block (0..MaxBlobCommitmentsPerBlock-1).
	CommittedHash types.Hash   // Hash the buyer commits to (expected blob versioned hash).
	Expiry        uint64       // Slot at which this future expires if unsettled.
	Price         *big.Int     // Amount staked (in wei).
	Buyer         types.Address
	Seller        types.Address
	Payout        *big.Int // Computed at settlement; nil until settled.
}

// BlobFuturesMarket manages a set of blob futures with thread-safe access.
type BlobFuturesMarket struct {
	mu          sync.RWMutex
	futures     map[types.Hash]*BlobFutureContract // ID -> future
	byExpiry    map[uint64][]types.Hash            // expirySlot -> future IDs
	currentSlot uint64
	nextNonce   uint64 // For unique ID generation.
}

// NewBlobFuturesMarket creates a new blob futures market starting at the given slot.
func NewBlobFuturesMarket(currentSlot uint64) *BlobFuturesMarket {
	return &BlobFuturesMarket{
		futures:     make(map[types.Hash]*BlobFutureContract),
		byExpiry:    make(map[uint64][]types.Hash),
		currentSlot: currentSlot,
	}
}

// CreateFuture creates a new blob future. The slot must be in the future relative
// to currentSlot, and expiry must be >= slot but within the allowed range for
// the future type. Price must be positive.
func (m *BlobFuturesMarket) CreateFuture(
	slot uint64,
	blobIndex uint8,
	committedHash types.Hash,
	expiry uint64,
	price *big.Int,
	buyer, seller types.Address,
) (*BlobFutureContract, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if slot <= m.currentSlot {
		return nil, ErrBlobFutureInvalidSlot
	}
	if blobIndex >= MaxBlobCommitmentsPerBlock {
		return nil, ErrBlobFutureBadIndex
	}
	if price == nil || price.Sign() <= 0 {
		return nil, ErrBlobFutureBadPrice
	}
	if expiry < slot {
		return nil, ErrBlobFutureBadExpiry
	}

	// Determine future type based on distance to expiry.
	slotsToExpiry := expiry - m.currentSlot
	var ftype FutureType
	if slotsToExpiry <= ShortDatedMaxSlots {
		ftype = ShortDatedFuture
	} else if slotsToExpiry <= LongDatedMaxSlots {
		ftype = LongDatedFuture
	} else {
		return nil, ErrBlobFutureBadExpiry
	}

	id := m.computeID(slot, blobIndex, buyer, seller)
	if _, exists := m.futures[id]; exists {
		return nil, ErrBlobFutureDuplicate
	}

	future := &BlobFutureContract{
		ID:            id,
		FType:         ftype,
		Status:        FutureActive,
		Slot:          slot,
		BlobIndex:     blobIndex,
		CommittedHash: committedHash,
		Expiry:        expiry,
		Price:         new(big.Int).Set(price),
		Buyer:         buyer,
		Seller:        seller,
	}

	m.futures[id] = future
	m.byExpiry[expiry] = append(m.byExpiry[expiry], id)

	return future, nil
}

// SettleFuture settles an active future by comparing the committed hash to the
// actual blob hash. Returns the payout amount. If the hashes match, the buyer
// receives 2x the price (profit = price). If they don't match, the seller
// keeps the price (buyer payout = 0).
func (m *BlobFuturesMarket) SettleFuture(futureID types.Hash, actualBlobHash types.Hash) (*big.Int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	future, ok := m.futures[futureID]
	if !ok {
		return nil, ErrBlobFutureNotFound
	}
	if future.Status != FutureActive {
		return nil, ErrBlobFutureNotActive
	}

	future.Status = FutureSettled
	future.Payout = ComputeSettlementPrice(future.CommittedHash, actualBlobHash, future.Price)

	return new(big.Int).Set(future.Payout), nil
}

// ComputeSettlementPrice computes the payout based on whether the committed
// hash matches the actual blob hash. Full match returns 2x price. A partial
// match (first 16 bytes match) returns 1.5x price. No match returns 0.
func ComputeSettlementPrice(committed, actual types.Hash, price *big.Int) *big.Int {
	if committed == actual {
		// Full match: buyer gets 2x.
		return new(big.Int).Mul(price, big.NewInt(2))
	}

	// Check partial match: first 16 bytes.
	partial := true
	for i := 0; i < 16; i++ {
		if committed[i] != actual[i] {
			partial = false
			break
		}
	}
	if partial {
		// Partial match: buyer gets 1.5x (price * 3 / 2).
		payout := new(big.Int).Mul(price, big.NewInt(3))
		payout.Div(payout, big.NewInt(2))
		return payout
	}

	// No match: buyer gets nothing.
	return new(big.Int)
}

// ExpireFutures marks all futures at or before currentSlot as expired.
// Returns the number of futures expired.
func (m *BlobFuturesMarket) ExpireFutures(currentSlot uint64) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentSlot = currentSlot
	expired := 0

	for slot, ids := range m.byExpiry {
		if slot > currentSlot {
			continue
		}
		for _, id := range ids {
			f, ok := m.futures[id]
			if !ok {
				continue
			}
			if f.Status == FutureActive {
				f.Status = FutureExpired
				f.Payout = new(big.Int) // No payout on expiry.
				expired++
			}
		}
		delete(m.byExpiry, slot)
	}

	return expired
}

// ListActiveFutures returns all futures with FutureActive status, sorted by
// expiry slot ascending.
func (m *BlobFuturesMarket) ListActiveFutures() []*BlobFutureContract {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var active []*BlobFutureContract
	for _, f := range m.futures {
		if f.Status == FutureActive {
			active = append(active, f)
		}
	}

	sort.Slice(active, func(i, j int) bool {
		return active[i].Expiry < active[j].Expiry
	})

	return active
}

// GetFuture returns the future with the given ID, or an error if not found.
func (m *BlobFuturesMarket) GetFuture(id types.Hash) (*BlobFutureContract, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	f, ok := m.futures[id]
	if !ok {
		return nil, ErrBlobFutureNotFound
	}
	return f, nil
}

// CancelFuture cancels an active future. Only possible before settlement or expiry.
func (m *BlobFuturesMarket) CancelFuture(id types.Hash) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.futures[id]
	if !ok {
		return ErrBlobFutureNotFound
	}
	if f.Status != FutureActive {
		return ErrBlobFutureNotActive
	}

	f.Status = FutureCancelled
	f.Payout = new(big.Int) // No payout on cancel.
	return nil
}

// FutureCount returns total number of futures (all statuses).
func (m *BlobFuturesMarket) FutureCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.futures)
}

// ActiveCount returns the number of active (unsettled, unexpired) futures.
func (m *BlobFuturesMarket) ActiveFutureCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, f := range m.futures {
		if f.Status == FutureActive {
			count++
		}
	}
	return count
}

// computeID generates a unique deterministic ID for a future.
func (m *BlobFuturesMarket) computeID(slot uint64, blobIndex uint8, buyer, seller types.Address) types.Hash {
	var buf [8 + 1 + 20 + 20 + 8]byte
	buf[0] = byte(slot)
	buf[1] = byte(slot >> 8)
	buf[2] = byte(slot >> 16)
	buf[3] = byte(slot >> 24)
	buf[4] = byte(slot >> 32)
	buf[5] = byte(slot >> 40)
	buf[6] = byte(slot >> 48)
	buf[7] = byte(slot >> 56)
	buf[8] = blobIndex
	copy(buf[9:29], buyer[:])
	copy(buf[29:49], seller[:])
	// Add nonce to ensure uniqueness for repeated calls with same params.
	nonce := m.nextNonce
	m.nextNonce++
	buf[49] = byte(nonce)
	buf[50] = byte(nonce >> 8)
	buf[51] = byte(nonce >> 16)
	buf[52] = byte(nonce >> 24)
	buf[53] = byte(nonce >> 32)
	buf[54] = byte(nonce >> 40)
	buf[55] = byte(nonce >> 48)
	buf[56] = byte(nonce >> 56)

	return keccak256(buf[:])
}
