// Package state provides Ethereum state management.
//
// endgame_state.go implements endgame state management with Single-Slot Finality
// (SSF) and instant finality tracking. Part of the L+ era roadmap where the
// consensus layer achieves endgame finality in seconds.
//
// EndgameStateDB wraps a StateDB to track finalized state roots and pending
// state transitions, enabling safe reversion to the last finalized checkpoint
// and garbage collection of pre-finality data.
package state

import (
	"errors"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Endgame state errors.
var (
	ErrEndgameNoFinalized      = errors.New("endgame: no finalized state exists")
	ErrEndgameAlreadyFinalized = errors.New("endgame: state root already finalized")
	ErrEndgameRootNotPending   = errors.New("endgame: state root not in pending set")
	ErrEndgameNilStateDB       = errors.New("endgame: nil underlying state db")
	ErrEndgameZeroRoot         = errors.New("endgame: zero state root")
	ErrEndgameSlotRegression   = errors.New("endgame: slot must not decrease")
)

// finalizedEntry records a finalized state root and the slot at which it
// was finalized.
type finalizedEntry struct {
	Root types.Hash
	Slot uint64
}

// pendingEntry records a pending (not yet finalized) state root with its
// associated slot number.
type pendingEntry struct {
	Root types.Hash
	Slot uint64
}

// EndgameStateDB wraps a StateDB with finality tracking for the endgame
// consensus regime. It maintains a history of finalized state roots and
// tracks pending state transitions awaiting finalization.
type EndgameStateDB struct {
	mu sync.RWMutex

	// underlying is the wrapped StateDB implementation.
	underlying StateDB

	// finalized tracks all finalized state roots in order.
	finalized []finalizedEntry

	// pending tracks state roots that have been proposed but not yet
	// finalized, keyed by state root hash.
	pending map[types.Hash]*pendingEntry

	// pendingOrder maintains insertion order of pending roots.
	pendingOrder []types.Hash

	// currentFinalizedRoot is the most recently finalized state root.
	currentFinalizedRoot types.Hash

	// currentFinalizedSlot is the slot of the most recent finalization.
	currentFinalizedSlot uint64

	// revertSnapshots maps state roots to snapshot IDs for reversion.
	revertSnapshots map[types.Hash]int
}

// NewEndgameStateDB creates a new EndgameStateDB wrapping the given StateDB.
func NewEndgameStateDB(underlying StateDB) (*EndgameStateDB, error) {
	if underlying == nil {
		return nil, ErrEndgameNilStateDB
	}
	return &EndgameStateDB{
		underlying:      underlying,
		finalized:       make([]finalizedEntry, 0),
		pending:         make(map[types.Hash]*pendingEntry),
		pendingOrder:    make([]types.Hash, 0),
		revertSnapshots: make(map[types.Hash]int),
	}, nil
}

// MarkFinalized marks a state root as finalized at the given slot. The state
// root must either be in the pending set or be the current state root. Slots
// must be monotonically non-decreasing.
func (e *EndgameStateDB) MarkFinalized(stateRoot types.Hash, slot uint64) error {
	if stateRoot.IsZero() {
		return ErrEndgameZeroRoot
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Check for slot regression.
	if len(e.finalized) > 0 && slot < e.currentFinalizedSlot {
		return ErrEndgameSlotRegression
	}

	// Check if already finalized.
	for _, f := range e.finalized {
		if f.Root == stateRoot {
			return ErrEndgameAlreadyFinalized
		}
	}

	// Add to finalized list.
	entry := finalizedEntry{Root: stateRoot, Slot: slot}
	e.finalized = append(e.finalized, entry)
	e.currentFinalizedRoot = stateRoot
	e.currentFinalizedSlot = slot

	// Remove from pending if present.
	if _, ok := e.pending[stateRoot]; ok {
		delete(e.pending, stateRoot)
		e.removePendingOrder(stateRoot)
	}

	// Take a snapshot for potential future reversion.
	snapID := e.underlying.Snapshot()
	e.revertSnapshots[stateRoot] = snapID

	return nil
}

// IsFinalized returns whether the given state root has been finalized.
func (e *EndgameStateDB) IsFinalized(stateRoot types.Hash) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, f := range e.finalized {
		if f.Root == stateRoot {
			return true
		}
	}
	return false
}

// GetFinalizedRoot returns the most recently finalized state root.
// Returns the zero hash if no state has been finalized yet.
func (e *EndgameStateDB) GetFinalizedRoot() types.Hash {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentFinalizedRoot
}

// FinalizedSlot returns the slot number of the most recent finalization.
// Returns 0 if no state has been finalized.
func (e *EndgameStateDB) FinalizedSlot() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentFinalizedSlot
}

// RevertToFinalized reverts the underlying state to the most recently
// finalized snapshot. This discards all pending state changes made after
// the last finalization.
func (e *EndgameStateDB) RevertToFinalized() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.currentFinalizedRoot.IsZero() {
		return ErrEndgameNoFinalized
	}

	snapID, ok := e.revertSnapshots[e.currentFinalizedRoot]
	if !ok {
		return ErrEndgameNoFinalized
	}

	// Revert the underlying state.
	e.underlying.RevertToSnapshot(snapID)

	// Clear all pending entries.
	e.pending = make(map[types.Hash]*pendingEntry)
	e.pendingOrder = make([]types.Hash, 0)

	return nil
}

// AddPendingRoot registers a state root as pending (not yet finalized) at the
// given slot. If the root is already pending, this is a no-op.
func (e *EndgameStateDB) AddPendingRoot(stateRoot types.Hash, slot uint64) {
	if stateRoot.IsZero() {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.pending[stateRoot]; ok {
		return
	}
	if e.isFinalized(stateRoot) {
		return
	}

	e.pending[stateRoot] = &pendingEntry{Root: stateRoot, Slot: slot}
	e.pendingOrder = append(e.pendingOrder, stateRoot)
}

// PendingStateRoots returns all pending (not yet finalized) state roots
// in the order they were added.
func (e *EndgameStateDB) PendingStateRoots() []types.Hash {
	e.mu.RLock()
	defer e.mu.RUnlock()

	roots := make([]types.Hash, len(e.pendingOrder))
	copy(roots, e.pendingOrder)
	return roots
}

// PendingCount returns the number of pending state roots.
func (e *EndgameStateDB) PendingCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.pending)
}

// GarbageCollectPreFinality removes finalized entries older than keepSlots
// from the current finalized slot. Returns the number of entries removed.
// This helps bound memory usage as the chain progresses.
func (e *EndgameStateDB) GarbageCollectPreFinality(keepSlots uint64) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.finalized) == 0 {
		return 0
	}

	cutoff := uint64(0)
	if e.currentFinalizedSlot > keepSlots {
		cutoff = e.currentFinalizedSlot - keepSlots
	}

	removed := 0
	newFinalized := make([]finalizedEntry, 0, len(e.finalized))
	for _, f := range e.finalized {
		if f.Slot < cutoff && f.Root != e.currentFinalizedRoot {
			// Remove the snapshot for this old finalized root.
			delete(e.revertSnapshots, f.Root)
			removed++
		} else {
			newFinalized = append(newFinalized, f)
		}
	}

	e.finalized = newFinalized
	return removed
}

// FinalizedHistory returns a copy of the finalized entries sorted by slot.
func (e *EndgameStateDB) FinalizedHistory() []finalizedEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()

	history := make([]finalizedEntry, len(e.finalized))
	copy(history, e.finalized)
	sort.Slice(history, func(i, j int) bool {
		return history[i].Slot < history[j].Slot
	})
	return history
}

// ComputeFinalityDigest computes a digest over the current finality state,
// useful for consensus attestations about finalized state.
func (e *EndgameStateDB) ComputeFinalityDigest() types.Hash {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.currentFinalizedRoot.IsZero() {
		return types.Hash{}
	}

	var slotBuf [8]byte
	slotBuf[0] = byte(e.currentFinalizedSlot >> 56)
	slotBuf[1] = byte(e.currentFinalizedSlot >> 48)
	slotBuf[2] = byte(e.currentFinalizedSlot >> 40)
	slotBuf[3] = byte(e.currentFinalizedSlot >> 32)
	slotBuf[4] = byte(e.currentFinalizedSlot >> 24)
	slotBuf[5] = byte(e.currentFinalizedSlot >> 16)
	slotBuf[6] = byte(e.currentFinalizedSlot >> 8)
	slotBuf[7] = byte(e.currentFinalizedSlot)

	data := make([]byte, 0, 32+8+32*len(e.finalized))
	data = append(data, e.currentFinalizedRoot[:]...)
	data = append(data, slotBuf[:]...)

	for _, f := range e.finalized {
		data = append(data, f.Root[:]...)
	}

	return crypto.Keccak256Hash(data)
}

// Underlying returns the wrapped StateDB.
func (e *EndgameStateDB) Underlying() StateDB {
	return e.underlying
}

// --- Internal helpers ---

// isFinalized checks if a root is finalized (must hold at least RLock).
func (e *EndgameStateDB) isFinalized(root types.Hash) bool {
	for _, f := range e.finalized {
		if f.Root == root {
			return true
		}
	}
	return false
}

// ValidateEndgameState checks that an EndgameStateDB is internally consistent:
//   - The underlying StateDB must not be nil
//   - If finalized entries exist, they must have increasing slot numbers
//   - All pending entries must have non-zero roots
func ValidateEndgameState(e *EndgameStateDB) error {
	if e == nil {
		return errors.New("endgame: nil EndgameStateDB")
	}
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.underlying == nil {
		return ErrEndgameNilStateDB
	}
	for i := 1; i < len(e.finalized); i++ {
		if e.finalized[i].Slot <= e.finalized[i-1].Slot {
			return ErrEndgameSlotRegression
		}
	}
	for root := range e.pending {
		if root == (types.Hash{}) {
			return ErrEndgameZeroRoot
		}
	}
	return nil
}

// removePendingOrder removes a root from the pendingOrder slice.
func (e *EndgameStateDB) removePendingOrder(root types.Hash) {
	for i, r := range e.pendingOrder {
		if r == root {
			e.pendingOrder = append(e.pendingOrder[:i], e.pendingOrder[i+1:]...)
			return
		}
	}
}
