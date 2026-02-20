package consensus

import (
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// FinalityConfig configures the finality tracker's behavior.
type FinalityConfig struct {
	EpochLength     uint64  // slots per epoch
	FinalityDelay   uint64  // epochs behind head before considering stale
	SafetyThreshold float64 // fraction of validators required (0.0-1.0)
}

// DefaultFinalityConfig returns a mainnet-like finality configuration.
func DefaultFinalityConfig() FinalityConfig {
	return FinalityConfig{
		EpochLength:     32,
		FinalityDelay:   2,
		SafetyThreshold: 0.667,
	}
}

// TrackedCheckpoint is an extended checkpoint with justification/finalization status.
type TrackedCheckpoint struct {
	Epoch     uint64
	Root      types.Hash
	Slot      uint64
	Justified bool
	Finalized bool
}

// EpochFinalityTracker manages finality checkpoints for the beacon chain.
// It tracks justified and finalized epochs and maintains a checkpoint history.
// All methods are safe for concurrent use.
type EpochFinalityTracker struct {
	mu     sync.RWMutex
	config FinalityConfig

	// Current state.
	headEpoch       uint64
	latestJustified *TrackedCheckpoint
	latestFinalized *TrackedCheckpoint

	// Checkpoint history in order of creation (oldest first).
	// Uses pointers so map and slice share the same objects.
	history []*TrackedCheckpoint

	// Fast lookup by epoch.
	byEpoch map[uint64]*TrackedCheckpoint
}

// NewEpochFinalityTracker creates a new finality tracker with the given config.
func NewEpochFinalityTracker(config FinalityConfig) *EpochFinalityTracker {
	genesis := &TrackedCheckpoint{
		Epoch:     0,
		Root:      types.Hash{},
		Slot:      0,
		Justified: true,
		Finalized: true,
	}
	ft := &EpochFinalityTracker{
		config:          config,
		latestJustified: genesis,
		latestFinalized: genesis,
		history:         []*TrackedCheckpoint{genesis},
		byEpoch:         make(map[uint64]*TrackedCheckpoint),
	}
	ft.byEpoch[0] = genesis
	return ft
}

// ProcessEpoch records a new epoch boundary checkpoint.
// Returns an error if the epoch is not monotonically increasing.
func (ft *EpochFinalityTracker) ProcessEpoch(epoch uint64, root types.Hash) error {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	if epoch < ft.headEpoch && epoch != 0 {
		return errors.New("consensus: epoch must not decrease")
	}
	if _, exists := ft.byEpoch[epoch]; exists && epoch != 0 {
		return errors.New("consensus: epoch already processed")
	}

	slot := epoch * ft.config.EpochLength
	cp := &TrackedCheckpoint{
		Epoch: epoch,
		Root:  root,
		Slot:  slot,
	}
	ft.history = append(ft.history, cp)
	ft.byEpoch[epoch] = cp
	ft.headEpoch = epoch
	return nil
}

// Justify marks the given epoch as justified. The epoch must have been
// previously processed via ProcessEpoch.
func (ft *EpochFinalityTracker) Justify(epoch uint64) error {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	cp, ok := ft.byEpoch[epoch]
	if !ok {
		return errors.New("consensus: unknown epoch")
	}
	if cp.Justified {
		return nil // already justified
	}

	cp.Justified = true
	if ft.latestJustified == nil || epoch > ft.latestJustified.Epoch {
		ft.latestJustified = cp
	}
	return nil
}

// Finalize marks the given epoch as finalized. The epoch must have been
// previously justified. Once finalized, all prior epochs are implicitly
// finalized as well.
func (ft *EpochFinalityTracker) Finalize(epoch uint64) error {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	cp, ok := ft.byEpoch[epoch]
	if !ok {
		return errors.New("consensus: unknown epoch")
	}
	if !cp.Justified {
		return errors.New("consensus: epoch not justified")
	}
	if cp.Finalized {
		return nil // already finalized
	}

	cp.Finalized = true
	if ft.latestFinalized == nil || epoch > ft.latestFinalized.Epoch {
		ft.latestFinalized = cp
	}
	return nil
}

// LatestJustified returns the most recently justified checkpoint, or nil.
func (ft *EpochFinalityTracker) LatestJustified() *TrackedCheckpoint {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	if ft.latestJustified == nil {
		return nil
	}
	cp := *ft.latestJustified
	return &cp
}

// LatestFinalized returns the most recently finalized checkpoint, or nil.
func (ft *EpochFinalityTracker) LatestFinalized() *TrackedCheckpoint {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	if ft.latestFinalized == nil {
		return nil
	}
	cp := *ft.latestFinalized
	return &cp
}

// IsFinalized returns true if the given epoch has been finalized (either
// directly or because a later epoch was finalized).
func (ft *EpochFinalityTracker) IsFinalized(epoch uint64) bool {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	if ft.latestFinalized == nil {
		return false
	}
	return epoch <= ft.latestFinalized.Epoch
}

// FinalityGap returns the number of epochs between the head epoch and the
// latest finalized epoch. A larger gap indicates the chain is struggling
// to finalize.
func (ft *EpochFinalityTracker) FinalityGap() uint64 {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	if ft.latestFinalized == nil {
		return ft.headEpoch
	}
	if ft.headEpoch <= ft.latestFinalized.Epoch {
		return 0
	}
	return ft.headEpoch - ft.latestFinalized.Epoch
}

// CheckpointHistory returns the most recent checkpoints, up to limit.
// Results are ordered newest first.
func (ft *EpochFinalityTracker) CheckpointHistory(limit int) []TrackedCheckpoint {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	if limit <= 0 || len(ft.history) == 0 {
		return nil
	}

	n := len(ft.history)
	if limit > n {
		limit = n
	}

	// Return newest-first, copying values out.
	result := make([]TrackedCheckpoint, limit)
	for i := 0; i < limit; i++ {
		result[i] = *ft.history[n-1-i]
	}
	return result
}

// HeadEpoch returns the current head epoch.
func (ft *EpochFinalityTracker) HeadEpoch() uint64 {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.headEpoch
}

// Config returns the tracker's configuration.
func (ft *EpochFinalityTracker) Config() FinalityConfig {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.config
}
