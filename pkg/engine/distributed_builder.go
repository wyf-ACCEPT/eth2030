package engine

import (
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Distributed builder network errors.
var (
	ErrDistBuilderNotFound    = errors.New("distributed builder: builder not found")
	ErrDistBuilderExists      = errors.New("distributed builder: builder already registered")
	ErrDistBuilderInactive    = errors.New("distributed builder: builder is inactive")
	ErrDistBuilderMaxReached  = errors.New("distributed builder: maximum builders reached")
	ErrDistBidInvalid         = errors.New("distributed builder: invalid bid")
	ErrDistBidZeroValue       = errors.New("distributed builder: bid value must be > 0")
	ErrDistBidBuilderNotFound = errors.New("distributed builder: bid from unregistered builder")
)

// BuilderConfig configures the distributed builder network.
type BuilderConfig struct {
	// MaxBuilders is the maximum number of registered builders.
	MaxBuilders int

	// BuilderTimeout is how long to wait for builder responses.
	BuilderTimeout time.Duration

	// MinBid is the minimum bid value accepted from builders.
	MinBid *big.Int

	// SlotAuctionDuration is the time window for accepting bids per slot.
	SlotAuctionDuration time.Duration
}

// DefaultBuilderConfig returns the default distributed builder config.
func DefaultBuilderConfig() *BuilderConfig {
	return &BuilderConfig{
		MaxBuilders:         32,
		BuilderTimeout:      2 * time.Second,
		MinBid:              big.NewInt(0),
		SlotAuctionDuration: 4 * time.Second,
	}
}

// DistBuilder represents a builder in the distributed builder network.
type DistBuilder struct {
	ID       types.Hash
	Address  types.Address
	Stake    *big.Int
	Active   bool
	LastSeen time.Time
}

// BuilderBid represents a block construction bid from a builder.
type BuilderBid struct {
	BuilderID types.Hash
	Slot      uint64
	BlockHash types.Hash
	Value     *big.Int
	Payload   []byte
	Timestamp time.Time
}

// BuilderNetwork manages distributed builders and their bids.
type BuilderNetwork struct {
	mu       sync.RWMutex
	config   *BuilderConfig
	builders map[types.Hash]*DistBuilder // id -> builder
	bids     map[uint64][]*BuilderBid    // slot -> bids
}

// NewBuilderNetwork creates a new distributed builder network.
func NewBuilderNetwork(config *BuilderConfig) *BuilderNetwork {
	if config == nil {
		config = DefaultBuilderConfig()
	}
	return &BuilderNetwork{
		config:   config,
		builders: make(map[types.Hash]*DistBuilder),
		bids:     make(map[uint64][]*BuilderBid),
	}
}

// RegisterBuilder registers a new builder in the network.
func (bn *BuilderNetwork) RegisterBuilder(id types.Hash, address types.Address, stake *big.Int) error {
	bn.mu.Lock()
	defer bn.mu.Unlock()

	if _, exists := bn.builders[id]; exists {
		return ErrDistBuilderExists
	}
	if len(bn.builders) >= bn.config.MaxBuilders {
		return ErrDistBuilderMaxReached
	}

	bn.builders[id] = &DistBuilder{
		ID:       id,
		Address:  address,
		Stake:    new(big.Int).Set(stake),
		Active:   true,
		LastSeen: time.Now(),
	}
	return nil
}

// UnregisterBuilder removes a builder from the network.
func (bn *BuilderNetwork) UnregisterBuilder(id types.Hash) error {
	bn.mu.Lock()
	defer bn.mu.Unlock()

	if _, exists := bn.builders[id]; !exists {
		return ErrDistBuilderNotFound
	}
	delete(bn.builders, id)
	return nil
}

// SubmitBid submits a block construction bid from a builder.
func (bn *BuilderNetwork) SubmitBid(bid *BuilderBid) error {
	if bid == nil {
		return ErrDistBidInvalid
	}
	if bid.Value == nil || bid.Value.Sign() <= 0 {
		return ErrDistBidZeroValue
	}

	bn.mu.Lock()
	defer bn.mu.Unlock()

	builder, exists := bn.builders[bid.BuilderID]
	if !exists {
		return ErrDistBidBuilderNotFound
	}
	if !builder.Active {
		return ErrDistBuilderInactive
	}

	// Update builder last seen.
	builder.LastSeen = time.Now()

	bn.bids[bid.Slot] = append(bn.bids[bid.Slot], bid)
	return nil
}

// GetWinningBid returns the highest-value bid for a given slot.
// Returns nil if no bids exist for the slot.
func (bn *BuilderNetwork) GetWinningBid(slot uint64) *BuilderBid {
	bn.mu.RLock()
	defer bn.mu.RUnlock()

	bids := bn.bids[slot]
	if len(bids) == 0 {
		return nil
	}

	var best *BuilderBid
	for _, bid := range bids {
		if best == nil || bid.Value.Cmp(best.Value) > 0 {
			best = bid
		}
	}
	return best
}

// ActiveBuilders returns the count of active builders.
func (bn *BuilderNetwork) ActiveBuilders() int {
	bn.mu.RLock()
	defer bn.mu.RUnlock()

	count := 0
	for _, b := range bn.builders {
		if b.Active {
			count++
		}
	}
	return count
}

// PruneStaleBids removes all bids for slots before beforeSlot.
func (bn *BuilderNetwork) PruneStaleBids(beforeSlot uint64) {
	bn.mu.Lock()
	defer bn.mu.Unlock()

	for slot := range bn.bids {
		if slot < beforeSlot {
			delete(bn.bids, slot)
		}
	}
}
