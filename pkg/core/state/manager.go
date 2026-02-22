// manager.go implements a state manager that coordinates state transitions,
// journal tracking, and snapshot management for the Ethereum execution layer.
package state

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Errors for state manager operations.
var (
	ErrSnapshotNotFound = errors.New("snapshot not found")
	ErrBlockNotFound    = errors.New("block not found in journal")
	ErrJournalEmpty     = errors.New("journal is empty")
)

// StateManagerConfig configures the state manager.
type StateManagerConfig struct {
	// CacheSize is the maximum number of state roots to cache.
	// Zero means no limit.
	CacheSize int

	// JournalLimit is the maximum number of journal entries to retain.
	// Zero means no limit.
	JournalLimit int

	// SnapshotInterval is the block interval at which automatic snapshots
	// are taken. Zero disables automatic snapshots.
	SnapshotInterval uint64
}

// journalRecord maps a block number to its post-execution state root.
type journalRecord struct {
	blockNumber uint64
	root        types.Hash
}

// snapshotRecord stores a point-in-time state root for restoration.
type snapshotRecord struct {
	id   types.Hash
	root types.Hash
}

// StateManager coordinates state root tracking, journal management, and
// snapshot operations. All public methods are safe for concurrent use.
type StateManager struct {
	config StateManagerConfig

	mu        sync.RWMutex
	root      types.Hash
	journal   []journalRecord
	snapshots map[types.Hash]snapshotRecord
	blockIdx  map[uint64]int // block number -> journal index
}

// NewStateManager creates a state manager with the given configuration.
func NewStateManager(config StateManagerConfig) *StateManager {
	return &StateManager{
		config:    config,
		snapshots: make(map[types.Hash]snapshotRecord),
		blockIdx:  make(map[uint64]int),
	}
}

// SetRoot sets the current state root.
func (m *StateManager) SetRoot(root types.Hash) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.root = root
}

// GetRoot returns the current state root.
func (m *StateManager) GetRoot() types.Hash {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.root
}

// AddJournalEntry records a block number and its resulting state root.
// If the journal exceeds JournalLimit, the oldest entry is pruned.
func (m *StateManager) AddJournalEntry(blockNumber uint64, root types.Hash) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := len(m.journal)
	m.journal = append(m.journal, journalRecord{
		blockNumber: blockNumber,
		root:        root,
	})
	m.blockIdx[blockNumber] = idx

	// Enforce journal limit.
	if m.config.JournalLimit > 0 && len(m.journal) > m.config.JournalLimit {
		m.pruneOldestLocked(len(m.journal) - m.config.JournalLimit)
	}
}

// GetJournalEntry looks up the state root for a given block number.
// Returns nil if the block is not in the journal.
func (m *StateManager) GetJournalEntry(blockNumber uint64) *types.Hash {
	m.mu.RLock()
	defer m.mu.RUnlock()

	idx, ok := m.blockIdx[blockNumber]
	if !ok {
		return nil
	}
	if idx >= len(m.journal) {
		return nil
	}
	root := m.journal[idx].root
	return &root
}

// JournalSize returns the number of entries in the journal.
func (m *StateManager) JournalSize() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.journal)
}

// TakeSnapshot captures the current state root and returns a unique
// snapshot ID derived from the root and snapshot count.
func (m *StateManager) TakeSnapshot() types.Hash {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Derive a unique ID from the current root + snapshot count.
	countBytes := []byte(fmt.Sprintf("%d", len(m.snapshots)))
	id := crypto.Keccak256Hash(m.root[:], countBytes)

	m.snapshots[id] = snapshotRecord{
		id:   id,
		root: m.root,
	}
	return id
}

// RestoreSnapshot restores the state root from a previously taken snapshot.
// Returns ErrSnapshotNotFound if the snapshot ID is unknown.
func (m *StateManager) RestoreSnapshot(id types.Hash) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap, ok := m.snapshots[id]
	if !ok {
		return ErrSnapshotNotFound
	}
	m.root = snap.root
	return nil
}

// PruneJournal removes old journal entries, keeping only the last keepLast
// entries. If keepLast >= current journal size, no pruning occurs.
func (m *StateManager) PruneJournal(keepLast int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if keepLast < 0 {
		keepLast = 0
	}
	if keepLast >= len(m.journal) {
		return
	}
	removeCount := len(m.journal) - keepLast
	m.pruneOldestLocked(removeCount)
}

// RevertToBlock restores the state root to the value recorded for the given
// block number. Returns the restored root or an error if the block is not
// found in the journal.
func (m *StateManager) RevertToBlock(blockNumber uint64) (*types.Hash, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx, ok := m.blockIdx[blockNumber]
	if !ok {
		return nil, fmt.Errorf("%w: block %d", ErrBlockNotFound, blockNumber)
	}
	if idx >= len(m.journal) {
		return nil, fmt.Errorf("%w: block %d (stale index)", ErrBlockNotFound, blockNumber)
	}

	root := m.journal[idx].root
	m.root = root

	// Remove all journal entries after this block.
	for i := idx + 1; i < len(m.journal); i++ {
		delete(m.blockIdx, m.journal[i].blockNumber)
	}
	m.journal = m.journal[:idx+1]

	return &root, nil
}

// LatestBlock returns the highest block number in the journal.
// Returns 0 if the journal is empty.
func (m *StateManager) LatestBlock() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.journal) == 0 {
		return 0
	}

	// Find max block number (journal is append-only but blocks may not
	// be strictly ordered if forks are involved).
	max := m.journal[0].blockNumber
	for _, entry := range m.journal[1:] {
		if entry.blockNumber > max {
			max = entry.blockNumber
		}
	}
	return max
}

// BlockNumbers returns all block numbers in the journal, sorted ascending.
func (m *StateManager) BlockNumbers() []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	blocks := make([]uint64, len(m.journal))
	for i, entry := range m.journal {
		blocks[i] = entry.blockNumber
	}
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i] < blocks[j]
	})
	return blocks
}

// pruneOldestLocked removes the first removeCount entries from the journal.
// Must be called with m.mu held.
func (m *StateManager) pruneOldestLocked(removeCount int) {
	if removeCount <= 0 || removeCount > len(m.journal) {
		return
	}

	// Remove block index entries for pruned records.
	for i := 0; i < removeCount; i++ {
		delete(m.blockIdx, m.journal[i].blockNumber)
	}

	// Shift journal.
	remaining := m.journal[removeCount:]
	newJournal := make([]journalRecord, len(remaining))
	copy(newJournal, remaining)
	m.journal = newJournal

	// Rebuild block index with new positions.
	m.blockIdx = make(map[uint64]int, len(m.journal))
	for i, entry := range m.journal {
		m.blockIdx[entry.blockNumber] = i
	}
}
