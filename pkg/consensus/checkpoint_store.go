// checkpoint_store.go implements a persistent checkpoint store for finality
// tracking. Stores justified and finalized checkpoints with epoch+root,
// supports checkpoint chain validation, weak subjectivity checks, and
// checkpoint sync bootstrapping.
//
// Complements finality_checkpoint.go by providing a richer persistence
// layer with chain validation, weak subjectivity safety, and bootstrap
// capabilities for checkpoint sync.
package consensus

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Checkpoint store constants.
const (
	// CSWeakSubjectivityPeriod is the default weak subjectivity period
	// in epochs (~54 hours at 32 slots/epoch, 12 sec/slot).
	CSWeakSubjectivityPeriod Epoch = 256

	// CSMinCheckpointsForChain is the minimum number of checkpoints
	// required for chain validation.
	CSMinCheckpointsForChain = 2

	// CSMaxStoredCheckpoints is the default maximum stored checkpoints
	// before automatic pruning. Zero means unlimited.
	CSMaxStoredCheckpoints = 0
)

// Checkpoint store errors.
var (
	ErrCSNotFound            = errors.New("checkpoint_store: checkpoint not found")
	ErrCSEpochExists         = errors.New("checkpoint_store: epoch already stored")
	ErrCSInvalidEpoch        = errors.New("checkpoint_store: invalid epoch")
	ErrCSChainBroken         = errors.New("checkpoint_store: chain validation failed")
	ErrCSWeakSubjectivity    = errors.New("checkpoint_store: outside weak subjectivity period")
	ErrCSNoCheckpoints       = errors.New("checkpoint_store: no checkpoints stored")
	ErrCSNilCheckpoint       = errors.New("checkpoint_store: nil checkpoint")
	ErrCSBootstrapConflict   = errors.New("checkpoint_store: bootstrap conflicts with existing")
)

// StoredCheckpoint represents a checkpoint persisted in the store.
// Includes both the checkpoint data and metadata about its chain position.
type StoredCheckpoint struct {
	// Epoch is the checkpoint epoch.
	Epoch Epoch

	// Root is the block root at the epoch boundary.
	Root types.Hash

	// Justified indicates whether this checkpoint was justified.
	Justified bool

	// Finalized indicates whether this checkpoint was finalized.
	Finalized bool

	// ParentEpoch is the previous finalized epoch in the chain,
	// or 0 if this is the genesis/bootstrap checkpoint.
	ParentEpoch Epoch
}

// CheckpointChainStatus reports the result of a chain validation.
type CheckpointChainStatus struct {
	Valid         bool
	Length        int
	LatestEpoch   Epoch
	EarliestEpoch Epoch
	Gaps          []Epoch // epochs with missing intermediate checkpoints
}

// WeakSubjectivityCheck reports the result of a WS safety check.
type WeakSubjectivityCheck struct {
	Safe                bool
	CurrentEpoch        Epoch
	LatestFinalizedEpoch Epoch
	Distance            uint64 // epochs since last finalization
	MaxAllowed          uint64 // maximum allowed distance (WS period)
}

// CheckpointPersistenceStore provides persistence and retrieval of
// finality checkpoints with chain validation and weak subjectivity
// support. Thread-safe.
type CheckpointPersistenceStore struct {
	mu sync.RWMutex

	// checkpoints maps epoch -> stored checkpoint.
	checkpoints map[Epoch]*StoredCheckpoint

	// justified tracks the current justified checkpoint.
	justified *StoredCheckpoint

	// finalized tracks the current finalized checkpoint.
	finalized *StoredCheckpoint

	// wsperiod is the configured weak subjectivity period.
	wsperiod Epoch

	// maxStored is the max checkpoints to keep (0 = unlimited).
	maxStored int
}

// CheckpointPersistenceConfig configures the persistence store.
type CheckpointPersistenceConfig struct {
	WeakSubjectivityPeriod Epoch
	MaxStoredCheckpoints   int
}

// DefaultCheckpointPersistenceConfig returns default configuration.
func DefaultCheckpointPersistenceConfig() CheckpointPersistenceConfig {
	return CheckpointPersistenceConfig{
		WeakSubjectivityPeriod: CSWeakSubjectivityPeriod,
		MaxStoredCheckpoints:   CSMaxStoredCheckpoints,
	}
}

// NewCheckpointPersistenceStore creates a new checkpoint persistence store.
func NewCheckpointPersistenceStore(cfg CheckpointPersistenceConfig) *CheckpointPersistenceStore {
	if cfg.WeakSubjectivityPeriod == 0 {
		cfg.WeakSubjectivityPeriod = CSWeakSubjectivityPeriod
	}
	return &CheckpointPersistenceStore{
		checkpoints: make(map[Epoch]*StoredCheckpoint),
		wsperiod:    cfg.WeakSubjectivityPeriod,
		maxStored:   cfg.MaxStoredCheckpoints,
	}
}

// StoreCheckpoint persists a checkpoint. If allowOverwrite is false and
// the epoch already exists, returns ErrCSEpochExists.
func (s *CheckpointPersistenceStore) StoreCheckpoint(cp *StoredCheckpoint, allowOverwrite bool) error {
	if cp == nil {
		return ErrCSNilCheckpoint
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if !allowOverwrite {
		if _, exists := s.checkpoints[cp.Epoch]; exists {
			return ErrCSEpochExists
		}
	}

	stored := *cp
	s.checkpoints[cp.Epoch] = &stored

	// Update justified/finalized tracking.
	if cp.Justified {
		if s.justified == nil || cp.Epoch > s.justified.Epoch {
			j := stored
			s.justified = &j
		}
	}
	if cp.Finalized {
		if s.finalized == nil || cp.Epoch > s.finalized.Epoch {
			f := stored
			s.finalized = &f
		}
	}

	// Auto-prune if maxStored is set.
	if s.maxStored > 0 && len(s.checkpoints) > s.maxStored {
		s.pruneLocked(s.maxStored)
	}

	return nil
}

// GetCheckpoint retrieves the checkpoint at the given epoch.
func (s *CheckpointPersistenceStore) GetCheckpoint(epoch Epoch) (*StoredCheckpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp, ok := s.checkpoints[epoch]
	if !ok {
		return nil, fmt.Errorf("%w: epoch %d", ErrCSNotFound, epoch)
	}
	result := *cp
	return &result, nil
}

// LatestFinalized returns the most recent finalized checkpoint, or nil.
func (s *CheckpointPersistenceStore) LatestFinalized() *StoredCheckpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.finalized == nil {
		return nil
	}
	cp := *s.finalized
	return &cp
}

// LatestJustified returns the most recent justified checkpoint, or nil.
func (s *CheckpointPersistenceStore) LatestJustified() *StoredCheckpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.justified == nil {
		return nil
	}
	cp := *s.justified
	return &cp
}

// Count returns the total number of stored checkpoints.
func (s *CheckpointPersistenceStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.checkpoints)
}

// AllEpochs returns all stored checkpoint epochs in ascending order.
func (s *CheckpointPersistenceStore) AllEpochs() []Epoch {
	s.mu.RLock()
	defer s.mu.RUnlock()

	epochs := make([]Epoch, 0, len(s.checkpoints))
	for ep := range s.checkpoints {
		epochs = append(epochs, ep)
	}
	sort.Slice(epochs, func(i, j int) bool { return epochs[i] < epochs[j] })
	return epochs
}

// ValidateChain validates the stored checkpoint chain by checking that
// each checkpoint's parent epoch references a stored checkpoint (or 0
// for genesis). Returns the chain status.
func (s *CheckpointPersistenceStore) ValidateChain() CheckpointChainStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := CheckpointChainStatus{
		Valid:  true,
		Length: len(s.checkpoints),
	}

	if len(s.checkpoints) == 0 {
		return status
	}

	epochs := make([]Epoch, 0, len(s.checkpoints))
	for ep := range s.checkpoints {
		epochs = append(epochs, ep)
	}
	sort.Slice(epochs, func(i, j int) bool { return epochs[i] < epochs[j] })

	status.EarliestEpoch = epochs[0]
	status.LatestEpoch = epochs[len(epochs)-1]

	// Validate each checkpoint's parent reference.
	for _, ep := range epochs {
		cp := s.checkpoints[ep]
		if cp.ParentEpoch == 0 {
			continue // genesis or bootstrap checkpoint
		}
		if _, exists := s.checkpoints[cp.ParentEpoch]; !exists {
			status.Valid = false
			status.Gaps = append(status.Gaps, cp.ParentEpoch)
		}
	}

	return status
}

// CheckWeakSubjectivity performs a weak subjectivity safety check.
// Returns whether the chain is within the WS safety window relative
// to the given current epoch.
func (s *CheckpointPersistenceStore) CheckWeakSubjectivity(currentEpoch Epoch) WeakSubjectivityCheck {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := WeakSubjectivityCheck{
		CurrentEpoch: currentEpoch,
		MaxAllowed:   uint64(s.wsperiod),
	}

	if s.finalized == nil {
		// No finalized checkpoint: always considered safe at genesis.
		result.Safe = true
		return result
	}

	result.LatestFinalizedEpoch = s.finalized.Epoch

	if currentEpoch < s.finalized.Epoch {
		// Current epoch before finalized: this is an edge case, treat as safe.
		result.Safe = true
		result.Distance = 0
		return result
	}

	result.Distance = uint64(currentEpoch - s.finalized.Epoch)
	result.Safe = result.Distance <= result.MaxAllowed

	return result
}

// BootstrapFromCheckpoint initializes the store with a trusted bootstrap
// checkpoint for checkpoint sync. If the store already has checkpoints
// and the bootstrap conflicts with existing data, returns an error.
func (s *CheckpointPersistenceStore) BootstrapFromCheckpoint(
	epoch Epoch, root types.Hash,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for conflicts with existing data.
	if existing, ok := s.checkpoints[epoch]; ok {
		if existing.Root != root {
			return fmt.Errorf("%w: epoch %d has root %x, bootstrap has %x",
				ErrCSBootstrapConflict, epoch, existing.Root[:4], root[:4])
		}
		// Already matches -- no-op.
		return nil
	}

	cp := &StoredCheckpoint{
		Epoch:       epoch,
		Root:        root,
		Justified:   true,
		Finalized:   true,
		ParentEpoch: 0, // bootstrap has no parent
	}
	s.checkpoints[epoch] = cp

	// Update tracking.
	if s.finalized == nil || epoch > s.finalized.Epoch {
		f := *cp
		s.finalized = &f
	}
	if s.justified == nil || epoch > s.justified.Epoch {
		j := *cp
		s.justified = &j
	}

	return nil
}

// PruneBeforeEpoch removes all checkpoints with epoch < beforeEpoch.
// Returns the number of pruned entries. Never prunes the current finalized
// or justified checkpoint even if they're before the cutoff.
func (s *CheckpointPersistenceStore) PruneBeforeEpoch(beforeEpoch Epoch) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	pruned := 0
	for ep := range s.checkpoints {
		if ep < beforeEpoch {
			// Do not prune the current finalized or justified checkpoint.
			if s.finalized != nil && ep == s.finalized.Epoch {
				continue
			}
			if s.justified != nil && ep == s.justified.Epoch {
				continue
			}
			delete(s.checkpoints, ep)
			pruned++
		}
	}
	return pruned
}

// FinalizedCheckpoints returns all finalized checkpoints in epoch order.
func (s *CheckpointPersistenceStore) FinalizedCheckpoints() []*StoredCheckpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*StoredCheckpoint
	for _, cp := range s.checkpoints {
		if cp.Finalized {
			stored := *cp
			result = append(result, &stored)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Epoch < result[j].Epoch })
	return result
}

// JustifiedCheckpoints returns all justified checkpoints in epoch order.
func (s *CheckpointPersistenceStore) JustifiedCheckpoints() []*StoredCheckpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*StoredCheckpoint
	for _, cp := range s.checkpoints {
		if cp.Justified {
			stored := *cp
			result = append(result, &stored)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Epoch < result[j].Epoch })
	return result
}

// HasCheckpoint returns whether a checkpoint exists at the given epoch.
func (s *CheckpointPersistenceStore) HasCheckpoint(epoch Epoch) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.checkpoints[epoch]
	return ok
}

// GetCheckpointRange returns all checkpoints in [fromEpoch, toEpoch].
func (s *CheckpointPersistenceStore) GetCheckpointRange(
	fromEpoch, toEpoch Epoch,
) []*StoredCheckpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*StoredCheckpoint
	for ep, cp := range s.checkpoints {
		if ep >= fromEpoch && ep <= toEpoch {
			stored := *cp
			result = append(result, &stored)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Epoch < result[j].Epoch })
	return result
}

// pruneLocked removes the oldest checkpoints to keep only the specified
// count. Must be called with s.mu held.
func (s *CheckpointPersistenceStore) pruneLocked(keepCount int) {
	if len(s.checkpoints) <= keepCount {
		return
	}

	epochs := make([]Epoch, 0, len(s.checkpoints))
	for ep := range s.checkpoints {
		epochs = append(epochs, ep)
	}
	sort.Slice(epochs, func(i, j int) bool { return epochs[i] < epochs[j] })

	toRemove := len(epochs) - keepCount
	for i := 0; i < toRemove; i++ {
		ep := epochs[i]
		// Protect finalized/justified.
		if s.finalized != nil && ep == s.finalized.Epoch {
			continue
		}
		if s.justified != nil && ep == s.justified.Epoch {
			continue
		}
		delete(s.checkpoints, ep)
	}
}
