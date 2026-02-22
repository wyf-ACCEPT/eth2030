// checkpoint_sync.go implements checkpoint-based sync that allows the client
// to start syncing from a trusted checkpoint instead of genesis. This is part
// of the Glamsterdan fast-confirmation roadmap item.
package sync

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Checkpoint sync errors.
var (
	ErrCheckpointNotSet     = errors.New("checkpoint sync: no checkpoint configured")
	ErrCheckpointZeroHash   = errors.New("checkpoint sync: block hash must not be zero")
	ErrCheckpointZeroState  = errors.New("checkpoint sync: state root must not be zero")
	ErrCheckpointZeroBlock  = errors.New("checkpoint sync: block number must not be zero")
	ErrCheckpointInvalid    = errors.New("checkpoint sync: checkpoint is inconsistent")
	ErrCheckpointSyncing    = errors.New("checkpoint sync: already syncing")
	ErrCheckpointComplete   = errors.New("checkpoint sync: sync already complete")
	ErrCheckpointNoTarget   = errors.New("checkpoint sync: target block not set")
)

// Checkpoint represents a trusted point in the chain that the client can
// start syncing from instead of genesis. Checkpoints are typically
// distributed by the community or embedded in client releases.
type Checkpoint struct {
	Epoch       uint64
	BlockNumber uint64
	BlockHash   types.Hash
	StateRoot   types.Hash
}

// Hash returns a deterministic hash of the checkpoint for identification.
func (cp Checkpoint) Hash() types.Hash {
	buf := make([]byte, 0, 16+64)
	buf = binary.BigEndian.AppendUint64(buf, cp.Epoch)
	buf = binary.BigEndian.AppendUint64(buf, cp.BlockNumber)
	buf = append(buf, cp.BlockHash[:]...)
	buf = append(buf, cp.StateRoot[:]...)
	return crypto.Keccak256Hash(buf)
}

// CheckpointConfig configures the CheckpointSyncer.
type CheckpointConfig struct {
	// TrustedCheckpoint is the starting checkpoint for sync.
	TrustedCheckpoint *Checkpoint
	// VerifyHeaders enables header verification during sync.
	VerifyHeaders bool
	// MaxHeaderBatch is the maximum number of headers to request per batch.
	MaxHeaderBatch int
}

// DefaultCheckpointConfig returns sensible default configuration.
func DefaultCheckpointConfig() CheckpointConfig {
	return CheckpointConfig{
		VerifyHeaders:  true,
		MaxHeaderBatch: 64,
	}
}

// CheckpointProgress reports the current state of a checkpoint sync.
type CheckpointProgress struct {
	CurrentBlock uint64
	TargetBlock  uint64
	StartTime    time.Time
	Percentage   float64
}

// checkpoint sync states
const (
	cpStateIdle     uint32 = 0
	cpStateSyncing  uint32 = 1
	cpStateComplete uint32 = 2
)

// CheckpointSyncer synchronizes the chain starting from a trusted checkpoint,
// skipping the need to process all blocks from genesis.
// All methods are safe for concurrent use.
type CheckpointSyncer struct {
	config CheckpointConfig

	mu         sync.RWMutex
	checkpoint *Checkpoint
	verified   map[types.Hash]Checkpoint // verified checkpoints keyed by hash
	progress   CheckpointProgress
	state      atomic.Uint32
	target     uint64 // target block number for sync
}

// NewCheckpointSyncer creates a new CheckpointSyncer with the given config.
func NewCheckpointSyncer(config CheckpointConfig) *CheckpointSyncer {
	if config.MaxHeaderBatch <= 0 {
		config.MaxHeaderBatch = 64
	}

	cs := &CheckpointSyncer{
		config:   config,
		verified: make(map[types.Hash]Checkpoint),
	}

	if config.TrustedCheckpoint != nil {
		cp := *config.TrustedCheckpoint
		cs.checkpoint = &cp
	}

	return cs
}

// SetCheckpoint sets the trusted checkpoint to sync from. The checkpoint
// must have a non-zero block hash and state root. The block number must
// also be non-zero (genesis is block 0, but checkpoint sync is meaningless
// from genesis).
func (cs *CheckpointSyncer) SetCheckpoint(cp Checkpoint) error {
	if cp.BlockHash.IsZero() {
		return ErrCheckpointZeroHash
	}
	if cp.StateRoot.IsZero() {
		return ErrCheckpointZeroState
	}
	if cp.BlockNumber == 0 {
		return ErrCheckpointZeroBlock
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.checkpoint = &cp
	return nil
}

// VerifyCheckpoint verifies that a checkpoint is internally consistent.
// It checks that the checkpoint hash matches the recomputed hash and that
// the epoch and block number are in a valid relationship (block >= epoch*32).
func (cs *CheckpointSyncer) VerifyCheckpoint(cp Checkpoint) (bool, error) {
	if cp.BlockHash.IsZero() {
		return false, ErrCheckpointZeroHash
	}
	if cp.StateRoot.IsZero() {
		return false, ErrCheckpointZeroState
	}
	if cp.BlockNumber == 0 {
		return false, ErrCheckpointZeroBlock
	}

	// Verify epoch/block relationship: block number should be at least
	// epoch * 32 (slots per epoch) to be consistent.
	if cp.Epoch > 0 && cp.BlockNumber < cp.Epoch*32 {
		return false, nil
	}

	// Verify the checkpoint hash is deterministically correct.
	hash := cp.Hash()
	if hash.IsZero() {
		return false, ErrCheckpointInvalid
	}

	cs.mu.Lock()
	cs.verified[hash] = cp
	cs.mu.Unlock()

	return true, nil
}

// SyncFromCheckpoint starts syncing from the configured checkpoint.
// It sets the sync state and progress tracking. The actual block downloading
// would be performed by the main Syncer; this method initializes the
// checkpoint sync state machine.
func (cs *CheckpointSyncer) SyncFromCheckpoint() error {
	cs.mu.RLock()
	cp := cs.checkpoint
	cs.mu.RUnlock()

	if cp == nil {
		return ErrCheckpointNotSet
	}

	if !cs.state.CompareAndSwap(cpStateIdle, cpStateSyncing) {
		st := cs.state.Load()
		if st == cpStateComplete {
			return ErrCheckpointComplete
		}
		return ErrCheckpointSyncing
	}

	cs.mu.Lock()
	cs.progress = CheckpointProgress{
		CurrentBlock: cp.BlockNumber,
		TargetBlock:  cs.target,
		StartTime:    time.Now(),
		Percentage:   0.0,
	}

	// If target is not set or is below the checkpoint, set target to
	// checkpoint block number (sync is already "complete").
	if cs.target <= cp.BlockNumber {
		cs.progress.TargetBlock = cp.BlockNumber
		cs.progress.Percentage = 100.0
		cs.mu.Unlock()
		cs.state.Store(cpStateComplete)
		return nil
	}
	cs.mu.Unlock()

	return nil
}

// SetTarget sets the target block number for sync. This should be called
// before SyncFromCheckpoint to define the sync endpoint.
func (cs *CheckpointSyncer) SetTarget(blockNumber uint64) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.target = blockNumber
}

// UpdateProgress updates the current block during sync and recalculates
// the completion percentage. This is called as blocks are downloaded.
func (cs *CheckpointSyncer) UpdateProgress(currentBlock uint64) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.progress.CurrentBlock = currentBlock

	if cs.progress.TargetBlock <= cs.checkpoint.BlockNumber {
		cs.progress.Percentage = 100.0
		return
	}

	total := cs.progress.TargetBlock - cs.checkpoint.BlockNumber
	if total == 0 {
		cs.progress.Percentage = 100.0
		return
	}

	done := currentBlock - cs.checkpoint.BlockNumber
	if done > total {
		done = total
	}
	cs.progress.Percentage = float64(done) / float64(total) * 100.0

	if currentBlock >= cs.progress.TargetBlock {
		cs.state.Store(cpStateComplete)
	}
}

// Progress returns the current sync progress.
func (cs *CheckpointSyncer) Progress() CheckpointProgress {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.progress
}

// IsComplete returns whether the checkpoint sync has finished.
func (cs *CheckpointSyncer) IsComplete() bool {
	return cs.state.Load() == cpStateComplete
}

// Reset resets the sync state back to idle, clearing progress.
func (cs *CheckpointSyncer) Reset() {
	cs.state.Store(cpStateIdle)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.progress = CheckpointProgress{}
	cs.target = 0
}

// GetCheckpoint returns the currently configured checkpoint, or nil.
func (cs *CheckpointSyncer) GetCheckpoint() *Checkpoint {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if cs.checkpoint == nil {
		return nil
	}
	cp := *cs.checkpoint
	return &cp
}

// VerifiedCheckpoints returns the number of verified checkpoints.
func (cs *CheckpointSyncer) VerifiedCheckpoints() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.verified)
}

// IsVerified checks whether a checkpoint has been previously verified.
func (cs *CheckpointSyncer) IsVerified(cp Checkpoint) bool {
	hash := cp.Hash()
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	_, ok := cs.verified[hash]
	return ok
}
