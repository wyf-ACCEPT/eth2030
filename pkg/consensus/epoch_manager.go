// Package consensus - epoch manager for committee rotations and epoch-level state.
//
// EpochManager tracks epoch boundaries, assigns validators to committees,
// and maintains a bounded history of past epochs for lookback queries.
package consensus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Epoch manager errors.
var (
	ErrEpochNotFound         = errors.New("epoch: epoch not found")
	ErrEpochAlreadyFinalized = errors.New("epoch: epoch already finalized")
	ErrEpochNoValidators     = errors.New("epoch: no validators provided")
	ErrEpochAlreadyStarted   = errors.New("epoch: epoch already started")
)

// ManagedEpoch represents a single epoch with its committee and finality state.
type ManagedEpoch struct {
	Number    uint64
	StartSlot uint64
	EndSlot   uint64       // inclusive: last slot of the epoch
	Committee []string     // ordered list of validator IDs
	Finalized bool
	StateRoot types.Hash
}

// CommitteeAssignment maps a validator to a specific slot within an epoch.
type CommitteeAssignment struct {
	ValidatorID string
	Slot        uint64
	IsProposer  bool
}

// EpochManagerConfig configures the epoch manager.
type EpochManagerConfig struct {
	// SlotsPerEpoch is the number of slots in each epoch.
	SlotsPerEpoch uint64

	// CommitteeSize is the target committee size per epoch.
	CommitteeSize int

	// MaxHistoryEpochs is the maximum number of past epochs to retain.
	MaxHistoryEpochs int
}

// EpochManager manages epoch lifecycle, committee rotations, and history.
// All methods are safe for concurrent use.
type EpochManager struct {
	mu      sync.RWMutex
	config  EpochManagerConfig
	current *ManagedEpoch
	epochs  map[uint64]*ManagedEpoch // all tracked epochs keyed by number
	order   []uint64                 // epoch numbers in chronological order
}

// NewEpochManager creates a new epoch manager with the given config.
func NewEpochManager(config EpochManagerConfig) *EpochManager {
	return &EpochManager{
		config: config,
		epochs: make(map[uint64]*ManagedEpoch),
	}
}

// CurrentEpoch returns the current (most recently started) epoch, or nil
// if no epoch has been started.
func (em *EpochManager) CurrentEpoch() *ManagedEpoch {
	em.mu.RLock()
	defer em.mu.RUnlock()

	if em.current == nil {
		return nil
	}
	cp := *em.current
	return &cp
}

// EpochForSlot computes the epoch number that contains the given slot.
func (em *EpochManager) EpochForSlot(slot uint64) uint64 {
	if em.config.SlotsPerEpoch == 0 {
		return 0
	}
	return slot / em.config.SlotsPerEpoch
}

// StartEpoch begins a new epoch with the given validator committee.
// Returns an error if the epoch already exists or no validators are provided.
func (em *EpochManager) StartEpoch(epochNum uint64, validators []string) error {
	if len(validators) == 0 {
		return ErrEpochNoValidators
	}

	em.mu.Lock()
	defer em.mu.Unlock()

	if _, exists := em.epochs[epochNum]; exists {
		return fmt.Errorf("%w: epoch %d", ErrEpochAlreadyStarted, epochNum)
	}

	startSlot := epochNum * em.config.SlotsPerEpoch
	endSlot := startSlot + em.config.SlotsPerEpoch - 1

	// Copy validators to avoid external mutation.
	committee := make([]string, len(validators))
	copy(committee, validators)

	epoch := &ManagedEpoch{
		Number:    epochNum,
		StartSlot: startSlot,
		EndSlot:   endSlot,
		Committee: committee,
	}

	em.epochs[epochNum] = epoch
	em.order = append(em.order, epochNum)
	em.current = epoch

	// Prune old epochs if history limit exceeded.
	em.pruneHistoryLocked()

	return nil
}

// FinalizeEpoch marks the given epoch as finalized and records the state root.
func (em *EpochManager) FinalizeEpoch(epochNum uint64, stateRoot types.Hash) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	epoch, exists := em.epochs[epochNum]
	if !exists {
		return fmt.Errorf("%w: epoch %d", ErrEpochNotFound, epochNum)
	}

	if epoch.Finalized {
		return fmt.Errorf("%w: epoch %d", ErrEpochAlreadyFinalized, epochNum)
	}

	epoch.Finalized = true
	epoch.StateRoot = stateRoot
	return nil
}

// GetCommittee returns the validator committee for the given epoch.
func (em *EpochManager) GetCommittee(epochNum uint64) ([]string, error) {
	em.mu.RLock()
	defer em.mu.RUnlock()

	epoch, exists := em.epochs[epochNum]
	if !exists {
		return nil, fmt.Errorf("%w: epoch %d", ErrEpochNotFound, epochNum)
	}

	// Return a copy.
	result := make([]string, len(epoch.Committee))
	copy(result, epoch.Committee)
	return result, nil
}

// GetAssignment returns the committee assignment for a specific slot within an epoch.
// The assigned validator is determined by round-robin over the committee.
// The first validator in the committee for that slot position is the proposer.
func (em *EpochManager) GetAssignment(epochNum uint64, slot uint64) (*CommitteeAssignment, error) {
	em.mu.RLock()
	defer em.mu.RUnlock()

	epoch, exists := em.epochs[epochNum]
	if !exists {
		return nil, fmt.Errorf("%w: epoch %d", ErrEpochNotFound, epochNum)
	}

	if len(epoch.Committee) == 0 {
		return nil, fmt.Errorf("%w: epoch %d has empty committee", ErrEpochNotFound, epochNum)
	}

	// Compute position within epoch.
	slotPos := slot - epoch.StartSlot
	idx := slotPos % uint64(len(epoch.Committee))

	return &CommitteeAssignment{
		ValidatorID: epoch.Committee[idx],
		Slot:        slot,
		IsProposer:  true, // the assigned validator is the slot proposer
	}, nil
}

// IsEpochBoundary returns true if the given slot is the last slot of an epoch.
func (em *EpochManager) IsEpochBoundary(slot uint64) bool {
	if em.config.SlotsPerEpoch == 0 {
		return false
	}
	return (slot+1)%em.config.SlotsPerEpoch == 0
}

// History returns the last n epochs in chronological order (oldest first).
// If n exceeds the number of tracked epochs, all tracked epochs are returned.
func (em *EpochManager) History(n int) []*ManagedEpoch {
	em.mu.RLock()
	defer em.mu.RUnlock()

	if n <= 0 || len(em.order) == 0 {
		return nil
	}

	total := len(em.order)
	if n > total {
		n = total
	}

	result := make([]*ManagedEpoch, 0, n)
	for i := total - n; i < total; i++ {
		if epoch, ok := em.epochs[em.order[i]]; ok {
			cp := *epoch
			result = append(result, &cp)
		}
	}
	return result
}

// SlotInEpoch returns the 0-indexed position of a slot within its epoch.
func (em *EpochManager) SlotInEpoch(slot uint64) uint64 {
	if em.config.SlotsPerEpoch == 0 {
		return 0
	}
	return slot % em.config.SlotsPerEpoch
}

// pruneHistoryLocked removes the oldest epochs when history exceeds the limit.
// Must be called with em.mu held.
func (em *EpochManager) pruneHistoryLocked() {
	if em.config.MaxHistoryEpochs <= 0 {
		return
	}

	for len(em.order) > em.config.MaxHistoryEpochs {
		oldest := em.order[0]
		em.order = em.order[1:]
		delete(em.epochs, oldest)
	}
}
