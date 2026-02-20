// Package consensus - distributed block building for the CL.
//
// Distributed block building allows multiple builders to collaborate on
// constructing blocks. Each builder submits bids (for complete blocks)
// and fragments (partial transaction lists). The protocol merges bids
// by selecting the highest-value bid, and merges fragments by priority
// ordering with gas limit enforcement. This design supports the 2028
// roadmap goal of distributed block building for censorship resistance.
package consensus

import (
	"errors"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Distributed block builder errors.
var (
	ErrDBNilBid          = errors.New("dist builder: nil bid")
	ErrDBZeroBidValue    = errors.New("dist builder: bid value must be > 0")
	ErrDBSlotZero        = errors.New("dist builder: slot must be > 0")
	ErrDBNilFragment     = errors.New("dist builder: nil fragment")
	ErrDBEmptyFragment   = errors.New("dist builder: fragment has no transactions")
	ErrDBMaxBuilders     = errors.New("dist builder: maximum builders reached")
	ErrDBBidTimeout      = errors.New("dist builder: bid submission timed out")
	ErrDBNoBids          = errors.New("dist builder: no bids for slot")
	ErrDBNoFragments     = errors.New("dist builder: no fragments for slot")
	ErrDBGasExceeded     = errors.New("dist builder: merged gas exceeds limit")
)

// MergeStrategy defines how fragments are combined into a block.
type MergeStrategy int

const (
	// MergeByPriority sorts fragments by priority descending and packs
	// them greedily up to the gas limit.
	MergeByPriority MergeStrategy = iota

	// MergeByGasPrice sorts fragments by effective gas price (value/gas)
	// descending, maximizing revenue per gas unit.
	MergeByGasPrice
)

// DistBuilderConfig configures the distributed block building protocol.
type DistBuilderConfig struct {
	// MaxBuilders is the max number of builders whose bids/fragments
	// are accepted per slot.
	MaxBuilders int

	// BidTimeout is the deadline for bid submission after slot start.
	BidTimeout time.Duration

	// Strategy determines how fragments are merged.
	Strategy MergeStrategy

	// GasLimit is the max total gas for merged fragments.
	GasLimit uint64

	// MaxFragmentsPerSlot limits the fragments accepted per slot.
	MaxFragmentsPerSlot int
}

// DefaultDistBuilderConfig returns sensible defaults.
func DefaultDistBuilderConfig() *DistBuilderConfig {
	return &DistBuilderConfig{
		MaxBuilders:         16,
		BidTimeout:          4 * time.Second,
		Strategy:            MergeByPriority,
		GasLimit:            30_000_000,
		MaxFragmentsPerSlot: 64,
	}
}

// ConsensusBuilderBid represents a complete block bid from a builder.
// Named differently from engine.BuilderBid to avoid cross-package confusion.
type ConsensusBuilderBid struct {
	BuilderID string
	Slot      Slot
	Value     *big.Int   // bid value in wei
	GasUsed   uint64
	TxCount   int
	BlockRoot types.Hash
	Timestamp time.Time
}

// BlockFragment represents a partial block contribution from a builder.
// Multiple fragments are merged to form a complete block.
type BlockFragment struct {
	BuilderID string
	TxList    [][]byte // serialized transactions
	GasUsed   uint64
	Priority  int // higher = more important
}

// slotBids holds bids and fragments for a single slot.
type slotBids struct {
	bids      []*ConsensusBuilderBid
	fragments []*BlockFragment
	builders  map[string]struct{} // set of builder IDs that have bid
	createdAt time.Time
}

// MergedBlock is the result of merging fragments for a slot.
type MergedBlock struct {
	Slot        Slot
	Fragments   []*BlockFragment // included fragments, in merge order
	TotalGas    uint64
	TotalTxs    int
	BuilderIDs  []string
}

// DistBlockBuilder collects bids and fragments from multiple builders
// and merges them into blocks. All methods are safe for concurrent use.
type DistBlockBuilder struct {
	mu     sync.RWMutex
	config *DistBuilderConfig
	slots  map[Slot]*slotBids
}

// NewDistBlockBuilder creates a new distributed block builder.
func NewDistBlockBuilder(cfg *DistBuilderConfig) *DistBlockBuilder {
	if cfg == nil {
		cfg = DefaultDistBuilderConfig()
	}
	return &DistBlockBuilder{
		config: cfg,
		slots:  make(map[Slot]*slotBids),
	}
}

// SubmitBid submits a complete block bid for a slot.
func (db *DistBlockBuilder) SubmitBid(bid *ConsensusBuilderBid) error {
	if bid == nil {
		return ErrDBNilBid
	}
	if bid.Slot == 0 {
		return ErrDBSlotZero
	}
	if bid.Value == nil || bid.Value.Sign() <= 0 {
		return ErrDBZeroBidValue
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	sb := db.getOrCreateSlotLocked(bid.Slot)

	if len(sb.builders) >= db.config.MaxBuilders {
		if _, exists := sb.builders[bid.BuilderID]; !exists {
			return ErrDBMaxBuilders
		}
	}

	sb.builders[bid.BuilderID] = struct{}{}
	sb.bids = append(sb.bids, bid)
	return nil
}

// SubmitFragment submits a partial block fragment for a slot.
func (db *DistBlockBuilder) SubmitFragment(slot Slot, frag *BlockFragment) error {
	if frag == nil {
		return ErrDBNilFragment
	}
	if slot == 0 {
		return ErrDBSlotZero
	}
	if len(frag.TxList) == 0 {
		return ErrDBEmptyFragment
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	sb := db.getOrCreateSlotLocked(slot)

	if len(sb.fragments) >= db.config.MaxFragmentsPerSlot {
		return ErrDBMaxBuilders
	}

	sb.fragments = append(sb.fragments, frag)
	return nil
}

// MergeBids selects the highest-value bid for the given slot.
// Returns nil and ErrDBNoBids if no bids exist.
func (db *DistBlockBuilder) MergeBids(slot Slot) (*ConsensusBuilderBid, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	sb, exists := db.slots[slot]
	if !exists || len(sb.bids) == 0 {
		return nil, ErrDBNoBids
	}

	var best *ConsensusBuilderBid
	for _, bid := range sb.bids {
		if best == nil || bid.Value.Cmp(best.Value) > 0 {
			best = bid
		}
	}
	return best, nil
}

// MergeFragments combines fragments for a slot using the configured strategy.
// Fragments are packed greedily until the gas limit is reached.
func (db *DistBlockBuilder) MergeFragments(slot Slot) (*MergedBlock, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	sb, exists := db.slots[slot]
	if !exists || len(sb.fragments) == 0 {
		return nil, ErrDBNoFragments
	}

	// Copy and sort fragments based on strategy.
	sorted := make([]*BlockFragment, len(sb.fragments))
	copy(sorted, sb.fragments)

	switch db.config.Strategy {
	case MergeByGasPrice:
		sort.Slice(sorted, func(i, j int) bool {
			// Higher gas efficiency first; if gas is 0, sort by priority.
			if sorted[i].GasUsed == 0 && sorted[j].GasUsed == 0 {
				return sorted[i].Priority > sorted[j].Priority
			}
			if sorted[i].GasUsed == 0 {
				return false
			}
			if sorted[j].GasUsed == 0 {
				return true
			}
			// Compare tx count per gas as a proxy for gas price.
			iEff := float64(len(sorted[i].TxList)) / float64(sorted[i].GasUsed)
			jEff := float64(len(sorted[j].TxList)) / float64(sorted[j].GasUsed)
			return iEff > jEff
		})
	default: // MergeByPriority
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Priority > sorted[j].Priority
		})
	}

	// Greedily pack fragments up to gas limit.
	result := &MergedBlock{Slot: slot}
	gasLeft := db.config.GasLimit
	builderSet := make(map[string]struct{})

	for _, frag := range sorted {
		if frag.GasUsed > gasLeft {
			continue // skip fragment that exceeds remaining gas
		}
		result.Fragments = append(result.Fragments, frag)
		result.TotalGas += frag.GasUsed
		result.TotalTxs += len(frag.TxList)
		gasLeft -= frag.GasUsed
		if _, seen := builderSet[frag.BuilderID]; !seen {
			builderSet[frag.BuilderID] = struct{}{}
			result.BuilderIDs = append(result.BuilderIDs, frag.BuilderID)
		}
	}

	if len(result.Fragments) == 0 {
		return nil, ErrDBNoFragments
	}

	return result, nil
}

// GetWinningBid returns the highest-value bid for a slot, or nil if none.
func (db *DistBlockBuilder) GetWinningBid(slot Slot) *ConsensusBuilderBid {
	bid, err := db.MergeBids(slot)
	if err != nil {
		return nil
	}
	return bid
}

// BidCount returns the number of bids for a slot.
func (db *DistBlockBuilder) BidCount(slot Slot) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	sb, exists := db.slots[slot]
	if !exists {
		return 0
	}
	return len(sb.bids)
}

// FragmentCount returns the number of fragments for a slot.
func (db *DistBlockBuilder) FragmentCount(slot Slot) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	sb, exists := db.slots[slot]
	if !exists {
		return 0
	}
	return len(sb.fragments)
}

// PruneSlot removes all bids and fragments for a slot.
func (db *DistBlockBuilder) PruneSlot(slot Slot) {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.slots, slot)
}

// PruneBefore removes all data for slots before the given slot.
func (db *DistBlockBuilder) PruneBefore(slot Slot) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	pruned := 0
	for s := range db.slots {
		if s < slot {
			delete(db.slots, s)
			pruned++
		}
	}
	return pruned
}

// Config returns the builder's configuration (read-only copy).
func (db *DistBlockBuilder) Config() DistBuilderConfig {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return *db.config
}

// getOrCreateSlotLocked returns the slotBids for the given slot, creating
// it if it does not exist. Must be called with db.mu held for writing.
func (db *DistBlockBuilder) getOrCreateSlotLocked(slot Slot) *slotBids {
	sb, exists := db.slots[slot]
	if !exists {
		sb = &slotBids{
			builders:  make(map[string]struct{}),
			createdAt: time.Now(),
		}
		db.slots[slot] = sb
	}
	return sb
}
