// state_bridge_sync.go implements the cross-layer state synchronization engine
// for native rollups (EIP-8079). It manages state root synchronization between
// L1 and L2, maintains a synchronization journal for auditing, detects state
// divergence, and provides finalization tracking for cross-layer state proofs.
//
// This extends the base state_bridge.go deposit/withdrawal messaging with
// higher-level synchronization protocols: the SyncEngine tracks verified state
// roots on both layers, reconciles them at configurable intervals, and
// produces SyncCheckpoints that anchor L2 state to finalized L1 blocks.
package rollup

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Sync engine errors.
var (
	ErrSyncNilCheckpoint   = errors.New("state_bridge_sync: nil checkpoint")
	ErrSyncBlockRegression = errors.New("state_bridge_sync: L2 block does not advance")
	ErrSyncL1Regression    = errors.New("state_bridge_sync: L1 block does not advance")
	ErrSyncRootMismatch    = errors.New("state_bridge_sync: state root mismatch at checkpoint")
	ErrSyncNotFinalized    = errors.New("state_bridge_sync: checkpoint not yet finalized")
	ErrSyncDuplicateEntry  = errors.New("state_bridge_sync: duplicate journal entry")
	ErrSyncJournalEmpty    = errors.New("state_bridge_sync: journal is empty")
	ErrSyncInvalidProof    = errors.New("state_bridge_sync: synchronization proof invalid")
)

// SyncCheckpoint records a point where L1 and L2 state roots are verified
// to be consistent. It includes the block numbers, roots, and a binding
// commitment over both layers.
type SyncCheckpoint struct {
	// L1Block is the finalized L1 block number this checkpoint is anchored to.
	L1Block uint64

	// L2Block is the L2 block number at this checkpoint.
	L2Block uint64

	// L1StateRoot is the L1 state root at L1Block.
	L1StateRoot types.Hash

	// L2StateRoot is the L2 state root at L2Block.
	L2StateRoot types.Hash

	// Commitment is a binding hash over all checkpoint fields.
	Commitment types.Hash

	// Finalized indicates whether the L1 block has been finalized.
	Finalized bool
}

// SyncJournalEntry records a single synchronization event for auditing.
type SyncJournalEntry struct {
	// Sequence is the monotonic sequence number of this entry.
	Sequence uint64

	// L1Block is the L1 block at the time of this event.
	L1Block uint64

	// L2Block is the L2 block at the time of this event.
	L2Block uint64

	// EventType classifies the synchronization event.
	EventType SyncEventType

	// DataHash is a hash summarizing the event-specific data.
	DataHash types.Hash
}

// SyncEventType classifies synchronization events.
type SyncEventType uint8

const (
	// SyncEventCheckpoint indicates a new checkpoint was created.
	SyncEventCheckpoint SyncEventType = iota + 1

	// SyncEventFinalization indicates a checkpoint was finalized.
	SyncEventFinalization

	// SyncEventDivergence indicates a detected state divergence.
	SyncEventDivergence

	// SyncEventReconciliation indicates a divergence was reconciled.
	SyncEventReconciliation
)

// String returns a human-readable name for SyncEventType.
func (e SyncEventType) String() string {
	switch e {
	case SyncEventCheckpoint:
		return "checkpoint"
	case SyncEventFinalization:
		return "finalization"
	case SyncEventDivergence:
		return "divergence"
	case SyncEventReconciliation:
		return "reconciliation"
	default:
		return "unknown"
	}
}

// SyncEngineConfig configures the synchronization engine.
type SyncEngineConfig struct {
	// FinalizationDepth is how many L1 blocks must pass before a
	// checkpoint is considered finalized. Default: 64.
	FinalizationDepth uint64

	// MaxJournalEntries is the maximum number of journal entries to retain.
	MaxJournalEntries int
}

// DefaultSyncEngineConfig returns production defaults.
func DefaultSyncEngineConfig() SyncEngineConfig {
	return SyncEngineConfig{
		FinalizationDepth: 64,
		MaxJournalEntries: 1024,
	}
}

// SyncEngine manages cross-layer state root synchronization for a native
// rollup. It creates checkpoints, tracks finalization, and records events
// in an append-only journal. Thread-safe.
type SyncEngine struct {
	mu     sync.RWMutex
	config SyncEngineConfig

	// checkpoints stores all checkpoints indexed by L2 block number.
	checkpoints map[uint64]*SyncCheckpoint

	// latestL1Block tracks the most recently observed L1 block number.
	latestL1Block uint64

	// latestL2Block tracks the most recently checkpointed L2 block.
	latestL2Block uint64

	// journal is the append-only synchronization event log.
	journal []SyncJournalEntry

	// nextSeq is the next journal sequence number.
	nextSeq uint64
}

// NewSyncEngine creates a new synchronization engine.
func NewSyncEngine(config SyncEngineConfig) *SyncEngine {
	if config.FinalizationDepth == 0 {
		config.FinalizationDepth = 64
	}
	if config.MaxJournalEntries <= 0 {
		config.MaxJournalEntries = 1024
	}
	return &SyncEngine{
		config:      config,
		checkpoints: make(map[uint64]*SyncCheckpoint),
		journal:     make([]SyncJournalEntry, 0, 64),
	}
}

// CreateCheckpoint creates a new synchronization checkpoint binding the
// L1 and L2 state roots at the given block numbers. The checkpoint is
// initially not finalized.
func (se *SyncEngine) CreateCheckpoint(l1Block, l2Block uint64, l1Root, l2Root types.Hash) (*SyncCheckpoint, error) {
	se.mu.Lock()
	defer se.mu.Unlock()

	// L2 block must advance beyond the last checkpoint.
	if l2Block <= se.latestL2Block && se.latestL2Block > 0 {
		return nil, fmt.Errorf("%w: l2Block=%d, latest=%d", ErrSyncBlockRegression, l2Block, se.latestL2Block)
	}
	// L1 block must not regress.
	if l1Block < se.latestL1Block {
		return nil, fmt.Errorf("%w: l1Block=%d, latest=%d", ErrSyncL1Regression, l1Block, se.latestL1Block)
	}

	commitment := computeSyncCommitment(l1Block, l2Block, l1Root, l2Root)

	cp := &SyncCheckpoint{
		L1Block:     l1Block,
		L2Block:     l2Block,
		L1StateRoot: l1Root,
		L2StateRoot: l2Root,
		Commitment:  commitment,
		Finalized:   false,
	}

	se.checkpoints[l2Block] = cp
	se.latestL1Block = l1Block
	se.latestL2Block = l2Block

	se.appendJournalLocked(SyncJournalEntry{
		L1Block:   l1Block,
		L2Block:   l2Block,
		EventType: SyncEventCheckpoint,
		DataHash:  commitment,
	})

	return cp, nil
}

// FinalizeCheckpoints marks all checkpoints whose L1 block is at least
// FinalizationDepth behind the current L1 head as finalized. Returns
// the count of newly finalized checkpoints.
func (se *SyncEngine) FinalizeCheckpoints(currentL1Block uint64) int {
	se.mu.Lock()
	defer se.mu.Unlock()

	finalized := 0
	for _, cp := range se.checkpoints {
		if cp.Finalized {
			continue
		}
		if currentL1Block >= cp.L1Block+se.config.FinalizationDepth {
			cp.Finalized = true
			finalized++
			se.appendJournalLocked(SyncJournalEntry{
				L1Block:   cp.L1Block,
				L2Block:   cp.L2Block,
				EventType: SyncEventFinalization,
				DataHash:  cp.Commitment,
			})
		}
	}
	return finalized
}

// GetCheckpoint returns the checkpoint for a given L2 block, or nil if none.
func (se *SyncEngine) GetCheckpoint(l2Block uint64) *SyncCheckpoint {
	se.mu.RLock()
	defer se.mu.RUnlock()

	cp, ok := se.checkpoints[l2Block]
	if !ok {
		return nil
	}
	cpCopy := *cp
	return &cpCopy
}

// LatestFinalizedCheckpoint returns the most recent finalized checkpoint.
// Returns nil if no checkpoints are finalized.
func (se *SyncEngine) LatestFinalizedCheckpoint() *SyncCheckpoint {
	se.mu.RLock()
	defer se.mu.RUnlock()

	var best *SyncCheckpoint
	for _, cp := range se.checkpoints {
		if cp.Finalized {
			if best == nil || cp.L2Block > best.L2Block {
				best = cp
			}
		}
	}
	if best == nil {
		return nil
	}
	cpCopy := *best
	return &cpCopy
}

// VerifyCheckpoint verifies that a checkpoint's commitment is internally
// consistent with its field values.
func VerifySyncCheckpoint(cp *SyncCheckpoint) (bool, error) {
	if cp == nil {
		return false, ErrSyncNilCheckpoint
	}
	expected := computeSyncCommitment(cp.L1Block, cp.L2Block, cp.L1StateRoot, cp.L2StateRoot)
	if expected != cp.Commitment {
		return false, ErrSyncRootMismatch
	}
	return true, nil
}

// DetectDivergence checks whether two checkpoints for the same L2 block
// have different L2 state roots, indicating a state divergence. Returns
// true if divergence is detected.
func (se *SyncEngine) DetectDivergence(l2Block uint64, claimedL2Root types.Hash) bool {
	se.mu.RLock()
	cp, ok := se.checkpoints[l2Block]
	se.mu.RUnlock()

	if !ok {
		return false
	}
	diverged := cp.L2StateRoot != claimedL2Root

	if diverged {
		se.mu.Lock()
		se.appendJournalLocked(SyncJournalEntry{
			L1Block:   cp.L1Block,
			L2Block:   l2Block,
			EventType: SyncEventDivergence,
			DataHash:  crypto.Keccak256Hash(cp.L2StateRoot[:], claimedL2Root[:]),
		})
		se.mu.Unlock()
	}
	return diverged
}

// CheckpointCount returns the total number of checkpoints tracked.
func (se *SyncEngine) CheckpointCount() int {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return len(se.checkpoints)
}

// FinalizedCount returns the number of finalized checkpoints.
func (se *SyncEngine) FinalizedCount() int {
	se.mu.RLock()
	defer se.mu.RUnlock()

	count := 0
	for _, cp := range se.checkpoints {
		if cp.Finalized {
			count++
		}
	}
	return count
}

// JournalEntries returns a copy of the journal entries.
func (se *SyncEngine) JournalEntries() []SyncJournalEntry {
	se.mu.RLock()
	defer se.mu.RUnlock()

	result := make([]SyncJournalEntry, len(se.journal))
	copy(result, se.journal)
	return result
}

// JournalLength returns the number of journal entries.
func (se *SyncEngine) JournalLength() int {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return len(se.journal)
}

// PruneBefore removes checkpoints and journal entries for L2 blocks
// before the given block number. Returns the number of checkpoints pruned.
func (se *SyncEngine) PruneBefore(l2Block uint64) int {
	se.mu.Lock()
	defer se.mu.Unlock()

	pruned := 0
	for blk := range se.checkpoints {
		if blk < l2Block {
			delete(se.checkpoints, blk)
			pruned++
		}
	}

	// Prune journal entries.
	kept := make([]SyncJournalEntry, 0, len(se.journal))
	for _, entry := range se.journal {
		if entry.L2Block >= l2Block {
			kept = append(kept, entry)
		}
	}
	se.journal = kept

	return pruned
}

// appendJournalLocked appends an entry to the journal. Caller must hold the
// write lock. Automatically prunes old entries if the journal exceeds max size.
func (se *SyncEngine) appendJournalLocked(entry SyncJournalEntry) {
	entry.Sequence = se.nextSeq
	se.nextSeq++
	se.journal = append(se.journal, entry)

	// Prune oldest entries if over limit.
	if len(se.journal) > se.config.MaxJournalEntries {
		excess := len(se.journal) - se.config.MaxJournalEntries
		se.journal = se.journal[excess:]
	}
}

// computeSyncCommitment computes a binding commitment over checkpoint fields.
func computeSyncCommitment(l1Block, l2Block uint64, l1Root, l2Root types.Hash) types.Hash {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], l1Block)
	binary.BigEndian.PutUint64(buf[8:], l2Block)
	return crypto.Keccak256Hash(l1Root[:], l2Root[:], buf[:])
}
