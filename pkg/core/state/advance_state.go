package state

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Errors for the slot-based state advancer.
var (
	ErrAdvanceTooFar          = errors.New("advance: slot exceeds lookahead limit")
	ErrBranchNotFound         = errors.New("advance: speculative branch not found")
	ErrBranchAlreadyConfirmed = errors.New("advance: branch already confirmed")
	ErrMaxBranches            = errors.New("advance: maximum speculative branches reached")
)

// BranchStatus tracks the lifecycle of a speculative branch.
type BranchStatus int

const (
	BranchPending   BranchStatus = iota // created, awaiting confirmation
	BranchConfirmed                     // confirmed as canonical
	BranchPruned                        // discarded
)

// String returns a human-readable branch status.
func (s BranchStatus) String() string {
	switch s {
	case BranchPending:
		return "pending"
	case BranchConfirmed:
		return "confirmed"
	case BranchPruned:
		return "pruned"
	default:
		return "unknown"
	}
}

// AdvanceConfig configures the slot-based StateAdvancement system.
type AdvanceConfig struct {
	LookaheadSlots  uint64 // how many slots ahead we can speculate
	MaxSpecBranches int    // maximum concurrent speculative branches
	PruneInterval   uint64 // prune branches older than this many slots
}

// DefaultAdvanceConfig returns sensible defaults for advance state.
func DefaultAdvanceConfig() AdvanceConfig {
	return AdvanceConfig{
		LookaheadSlots:  8,
		MaxSpecBranches: 32,
		PruneInterval:   16,
	}
}

// SpeculativeBranch represents a speculative state computed ahead of a block.
type SpeculativeBranch struct {
	ID            string       // unique branch identifier
	ParentHash    types.Hash   // parent block hash this branch extends
	Slot          uint64       // target slot number
	PredictedRoot types.Hash   // predicted state root after advance
	Status        BranchStatus // current lifecycle status
	CreatedAtSlot uint64       // slot at which this branch was created
}

// SlotAdvancer pre-computes state for upcoming slots before blocks arrive,
// reducing latency when the actual block is received.
type SlotAdvancer struct {
	mu       sync.RWMutex
	config   AdvanceConfig
	branches map[string]*SpeculativeBranch // branchID -> branch
	headSlot uint64                        // current head slot for reference
}

// NewSlotAdvancer creates a new SlotAdvancer with the given config.
func NewSlotAdvancer(config AdvanceConfig) *SlotAdvancer {
	if config.LookaheadSlots == 0 {
		config.LookaheadSlots = DefaultAdvanceConfig().LookaheadSlots
	}
	if config.MaxSpecBranches <= 0 {
		config.MaxSpecBranches = DefaultAdvanceConfig().MaxSpecBranches
	}
	if config.PruneInterval == 0 {
		config.PruneInterval = DefaultAdvanceConfig().PruneInterval
	}
	return &SlotAdvancer{
		config:   config,
		branches: make(map[string]*SpeculativeBranch),
	}
}

// SetHeadSlot updates the current head slot used for lookahead checks.
func (sa *SlotAdvancer) SetHeadSlot(slot uint64) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.headSlot = slot
}

// HeadSlot returns the current head slot.
func (sa *SlotAdvancer) HeadSlot() uint64 {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.headSlot
}

// Advance creates a speculative branch for the given parent hash and slot.
// It returns an error if the slot is too far ahead or max branches is reached.
func (sa *SlotAdvancer) Advance(parentHash types.Hash, slot uint64) (*SpeculativeBranch, error) {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	// Check lookahead limit.
	if slot > sa.headSlot+sa.config.LookaheadSlots {
		return nil, ErrAdvanceTooFar
	}

	// Check branch count (only count pending branches toward the limit).
	pending := 0
	for _, b := range sa.branches {
		if b.Status == BranchPending {
			pending++
		}
	}
	if pending >= sa.config.MaxSpecBranches {
		return nil, ErrMaxBranches
	}

	// Derive a deterministic branch ID and predicted root from inputs.
	branchID := deriveBranchID(parentHash, slot)
	predictedRoot := derivePredictedRoot(parentHash, slot)

	branch := &SpeculativeBranch{
		ID:            branchID,
		ParentHash:    parentHash,
		Slot:          slot,
		PredictedRoot: predictedRoot,
		Status:        BranchPending,
		CreatedAtSlot: sa.headSlot,
	}

	sa.branches[branchID] = branch
	return branch, nil
}

// Confirm marks a speculative branch as canonical.
func (sa *SlotAdvancer) Confirm(branchID string) error {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	branch, ok := sa.branches[branchID]
	if !ok {
		return ErrBranchNotFound
	}
	if branch.Status == BranchConfirmed {
		return ErrBranchAlreadyConfirmed
	}

	branch.Status = BranchConfirmed
	return nil
}

// Prune removes old or invalid speculative branches that are older than the
// prune interval relative to the current head slot. Returns count of pruned.
func (sa *SlotAdvancer) Prune() int {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	pruned := 0
	cutoff := uint64(0)
	if sa.headSlot > sa.config.PruneInterval {
		cutoff = sa.headSlot - sa.config.PruneInterval
	}

	for id, branch := range sa.branches {
		if branch.Status == BranchPruned {
			// Remove already-pruned entries that somehow remain.
			delete(sa.branches, id)
			pruned++
		} else if branch.Status == BranchPending && branch.Slot <= cutoff {
			// Prune pending branches whose slot is at or below the cutoff.
			branch.Status = BranchPruned
			delete(sa.branches, id)
			pruned++
		}
	}
	return pruned
}

// GetBranch returns the branch with the given ID, or nil if not found.
func (sa *SlotAdvancer) GetBranch(branchID string) *SpeculativeBranch {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.branches[branchID]
}

// ActiveBranches returns the number of pending (non-confirmed, non-pruned) branches.
func (sa *SlotAdvancer) ActiveBranches() int {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	count := 0
	for _, b := range sa.branches {
		if b.Status == BranchPending {
			count++
		}
	}
	return count
}

// ConfirmedBranches returns the number of confirmed branches.
func (sa *SlotAdvancer) ConfirmedBranches() int {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	count := 0
	for _, b := range sa.branches {
		if b.Status == BranchConfirmed {
			count++
		}
	}
	return count
}

// TotalBranches returns the total number of branches (all statuses).
func (sa *SlotAdvancer) TotalBranches() int {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return len(sa.branches)
}

// Config returns the advancer's configuration.
func (sa *SlotAdvancer) Config() AdvanceConfig {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.config
}

// deriveBranchID produces a unique, deterministic branch identifier from
// the parent hash and target slot.
func deriveBranchID(parentHash types.Hash, slot uint64) string {
	var buf [40]byte
	copy(buf[:32], parentHash.Bytes())
	binary.BigEndian.PutUint64(buf[32:], slot)
	h := crypto.Keccak256Hash(buf[:])
	return fmt.Sprintf("branch-%s", h.Hex()[:18])
}

// derivePredictedRoot simulates computing the predicted state root from
// the parent hash and slot. In production this would run actual EVM execution.
func derivePredictedRoot(parentHash types.Hash, slot uint64) types.Hash {
	var buf [40]byte
	copy(buf[:32], parentHash.Bytes())
	binary.BigEndian.PutUint64(buf[32:], slot)
	return crypto.Keccak256Hash(append([]byte("predicted-root:"), buf[:]...))
}
