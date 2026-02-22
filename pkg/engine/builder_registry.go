package engine

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Builder registry errors.
var (
	ErrBuilderNotFound      = errors.New("builder not found")
	ErrBuilderAlreadyExists = errors.New("builder already registered")
	ErrBuilderNotActive     = errors.New("builder not active")
	ErrInsufficientStake    = errors.New("insufficient builder stake")
	ErrInvalidBuilderBid    = errors.New("invalid builder bid")
	ErrInvalidPayloadReveal = errors.New("payload reveal does not match committed hash")
	ErrNoBidsAvailable      = errors.New("no bids available for slot")
	ErrInvalidBidSignature  = errors.New("invalid bid signature")
)

// MinBuilderStake is the minimum stake required for a builder (1 ETH in wei).
var MinBuilderStake = new(big.Int).Mul(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))

// BuilderRegistry manages registered builders and their bids.
type BuilderRegistry struct {
	mu       sync.RWMutex
	builders map[BLSPubkey]*Builder                  // pubkey -> builder
	byIndex  map[BuilderIndex]*Builder               // index -> builder
	bids     map[uint64][]*SignedExecutionPayloadBid // slot -> sorted bids
	nextIdx  BuilderIndex
}

// NewBuilderRegistry creates an empty builder registry.
func NewBuilderRegistry() *BuilderRegistry {
	return &BuilderRegistry{
		builders: make(map[BLSPubkey]*Builder),
		byIndex:  make(map[BuilderIndex]*Builder),
		bids:     make(map[uint64][]*SignedExecutionPayloadBid),
	}
}

// RegisterBuilder adds a new builder to the registry.
// The builder must provide a valid registration with sufficient stake.
func (r *BuilderRegistry) RegisterBuilder(reg *BuilderRegistrationV1, stake *big.Int) (*Builder, error) {
	if stake == nil || stake.Cmp(MinBuilderStake) < 0 {
		return nil, fmt.Errorf("%w: need at least %s wei, got %s",
			ErrInsufficientStake, MinBuilderStake.String(), stake.String())
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.builders[reg.Pubkey]; exists {
		return nil, ErrBuilderAlreadyExists
	}

	idx := r.nextIdx
	r.nextIdx++

	builder := &Builder{
		Pubkey:           reg.Pubkey,
		Index:            idx,
		FeeRecipient:     reg.FeeRecipient,
		GasLimit:         reg.GasLimit,
		Balance:          new(big.Int).Set(stake),
		Status:           BuilderStatusActive,
		RegistrationTime: reg.Timestamp,
	}

	r.builders[reg.Pubkey] = builder
	r.byIndex[idx] = builder

	return builder, nil
}

// UnregisterBuilder marks a builder as exiting.
// The builder enters the exit cooldown period before withdrawal is allowed.
func (r *BuilderRegistry) UnregisterBuilder(pubkey BLSPubkey) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	builder, ok := r.builders[pubkey]
	if !ok {
		return ErrBuilderNotFound
	}
	if builder.Status != BuilderStatusActive {
		return ErrBuilderNotActive
	}

	builder.Status = BuilderStatusExiting
	return nil
}

// GetBuilder returns a builder by public key.
func (r *BuilderRegistry) GetBuilder(pubkey BLSPubkey) (*Builder, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	builder, ok := r.builders[pubkey]
	if !ok {
		return nil, ErrBuilderNotFound
	}
	return builder, nil
}

// GetBuilderByIndex returns a builder by its registry index.
func (r *BuilderRegistry) GetBuilderByIndex(idx BuilderIndex) (*Builder, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	builder, ok := r.byIndex[idx]
	if !ok {
		return nil, ErrBuilderNotFound
	}
	return builder, nil
}

// GetRegisteredBuilders returns all active builders.
func (r *BuilderRegistry) GetRegisteredBuilders() []*Builder {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*Builder
	for _, b := range r.builders {
		if b.Status == BuilderStatusActive {
			result = append(result, b)
		}
	}
	return result
}

// SubmitBid adds a builder bid for a given slot.
// The bid is validated for basic correctness before being stored.
func (r *BuilderRegistry) SubmitBid(signed *SignedExecutionPayloadBid) error {
	bid := &signed.Message

	r.mu.Lock()
	defer r.mu.Unlock()

	// Verify the builder is registered and active.
	builder, ok := r.byIndex[bid.BuilderIndex]
	if !ok {
		return fmt.Errorf("%w: builder index %d", ErrBuilderNotFound, bid.BuilderIndex)
	}
	if builder.Status != BuilderStatusActive {
		return ErrBuilderNotActive
	}

	// Verify the bid value is nonzero.
	if bid.Value == 0 {
		return fmt.Errorf("%w: bid value must be > 0", ErrInvalidBuilderBid)
	}

	// Verify the block hash is not empty.
	if bid.BlockHash == (types.Hash{}) {
		return fmt.Errorf("%w: block hash must not be empty", ErrInvalidBuilderBid)
	}

	// Verify the parent block hash is not empty.
	if bid.ParentBlockHash == (types.Hash{}) {
		return fmt.Errorf("%w: parent block hash must not be empty", ErrInvalidBuilderBid)
	}

	// Insert sorted by value descending (highest value first).
	slot := bid.Slot
	bids := r.bids[slot]
	inserted := false
	for i, existing := range bids {
		if bid.Value > existing.Message.Value {
			// Insert before this element.
			bids = append(bids[:i+1], bids[i:]...)
			bids[i] = signed
			inserted = true
			break
		}
	}
	if !inserted {
		bids = append(bids, signed)
	}
	r.bids[slot] = bids

	return nil
}

// GetBestBid returns the highest-value bid for the given slot.
func (r *BuilderRegistry) GetBestBid(slot uint64) (*SignedExecutionPayloadBid, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	bids, ok := r.bids[slot]
	if !ok || len(bids) == 0 {
		return nil, ErrNoBidsAvailable
	}

	return bids[0], nil
}

// GetBidsForSlot returns all bids for a given slot, ordered by value descending.
func (r *BuilderRegistry) GetBidsForSlot(slot uint64) []*SignedExecutionPayloadBid {
	r.mu.RLock()
	defer r.mu.RUnlock()

	bids := r.bids[slot]
	result := make([]*SignedExecutionPayloadBid, len(bids))
	copy(result, bids)
	return result
}

// ValidateBidPayload checks that a revealed payload matches the committed bid.
// This is the reveal phase: the builder reveals the full execution payload
// and we verify that the block hash matches what was committed in the bid.
func (r *BuilderRegistry) ValidateBidPayload(bid *ExecutionPayloadBid, payload *ExecutionPayloadV4) error {
	// The payload's block hash must match the committed block hash in the bid.
	if payload.BlockHash != bid.BlockHash {
		return fmt.Errorf("%w: committed %s, revealed %s",
			ErrInvalidPayloadReveal, bid.BlockHash.Hex(), payload.BlockHash.Hex())
	}

	// Verify parent hash consistency.
	if payload.ParentHash != bid.ParentBlockHash {
		return fmt.Errorf("%w: parent hash mismatch: bid %s, payload %s",
			ErrInvalidPayloadReveal, bid.ParentBlockHash.Hex(), payload.ParentHash.Hex())
	}

	// Verify gas limit consistency.
	if payload.GasLimit != bid.GasLimit {
		return fmt.Errorf("%w: gas limit mismatch: bid %d, payload %d",
			ErrInvalidPayloadReveal, bid.GasLimit, payload.GasLimit)
	}

	// Verify fee recipient consistency.
	if payload.FeeRecipient != bid.FeeRecipient {
		return fmt.Errorf("%w: fee recipient mismatch",
			ErrInvalidPayloadReveal)
	}

	return nil
}

// PruneSlot removes all bids for a given slot to reclaim memory.
func (r *BuilderRegistry) PruneSlot(slot uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bids, slot)
}

// BuilderCount returns the total number of registered builders (all statuses).
func (r *BuilderRegistry) BuilderCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.builders)
}
