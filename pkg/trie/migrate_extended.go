// migrate_extended.go implements full MPT-to-Binary-Trie migration with
// incremental migration, progress tracking, state root translation,
// rollback support, and parallel migration workers.
package trie

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// MigrationState represents the state of a migration process.
type MigrationState int

const (
	MigrationIdle       MigrationState = iota // not started
	MigrationRunning                          // actively migrating
	MigrationPaused                           // paused, can resume
	MigrationComplete                         // finished
	MigrationRolledBack                       // rolled back
)

// MigrationProgress tracks the progress of an MPT-to-binary-trie migration.
type MigrationProgress struct {
	KeysMigrated   uint64
	TotalKeys      uint64
	LastKey        []byte
	MPTRoot        types.Hash
	BinaryRoot     types.Hash
	State          MigrationState
}

// MigrationSnapshot captures a point-in-time state for rollback.
type MigrationSnapshot struct {
	BinaryTrie *BinaryTrie
	Progress   MigrationProgress
}

// MigrationConfig controls the behavior of incremental migration.
type MigrationConfig struct {
	// BatchSize is the number of keys to migrate per batch.
	BatchSize int
	// Workers is the number of parallel migration workers.
	Workers int
}

// DefaultMigrationConfig returns sensible defaults for migration.
func DefaultMigrationConfig() MigrationConfig {
	return MigrationConfig{
		BatchSize: 1000,
		Workers:   4,
	}
}

// IncrementalMigrator performs incremental MPT-to-BinaryTrie migration
// with progress tracking, rollback support, and parallel workers.
type IncrementalMigrator struct {
	mu       sync.Mutex
	config   MigrationConfig
	source   *Trie
	dest     *BinaryTrie
	progress MigrationProgress

	// Snapshots for rollback, keyed by migration step.
	snapshots []MigrationSnapshot

	// rootMap translates MPT state roots to binary trie roots.
	rootMap map[types.Hash]types.Hash

	// pending is the channel for key-value pairs awaiting migration.
	pending chan kvPair

	// stopCh signals workers to halt.
	stopCh chan struct{}
	wg     sync.WaitGroup
}

type kvPair struct {
	key   types.Hash
	value []byte
}

// NewIncrementalMigrator creates a migrator for the given source MPT.
func NewIncrementalMigrator(source *Trie, config MigrationConfig) *IncrementalMigrator {
	if config.BatchSize <= 0 {
		config.BatchSize = 1000
	}
	if config.Workers <= 0 {
		config.Workers = 1
	}
	return &IncrementalMigrator{
		config:  config,
		source:  source,
		dest:    NewBinaryTrie(),
		rootMap: make(map[types.Hash]types.Hash),
		progress: MigrationProgress{
			State: MigrationIdle,
		},
	}
}

// Progress returns a copy of the current migration progress.
func (m *IncrementalMigrator) Progress() MigrationProgress {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.progress
}

// Destination returns the destination binary trie.
func (m *IncrementalMigrator) Destination() *BinaryTrie {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dest
}

// TranslateRoot returns the binary trie root corresponding to an MPT root.
// Returns the zero hash if no translation exists.
func (m *IncrementalMigrator) TranslateRoot(mptRoot types.Hash) types.Hash {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.rootMap[mptRoot]; ok {
		return h
	}
	return types.Hash{}
}

// TakeSnapshot saves the current state for later rollback.
func (m *IncrementalMigrator) TakeSnapshot() {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := MigrationSnapshot{
		BinaryTrie: copyBinaryTrie(m.dest),
		Progress:   m.progress,
	}
	m.snapshots = append(m.snapshots, snap)
}

// Rollback reverts to the most recent snapshot. Returns an error if no
// snapshots exist.
func (m *IncrementalMigrator) Rollback() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.snapshots) == 0 {
		return errors.New("migrate: no snapshots to rollback to")
	}
	snap := m.snapshots[len(m.snapshots)-1]
	m.snapshots = m.snapshots[:len(m.snapshots)-1]
	m.dest = snap.BinaryTrie
	m.progress = snap.Progress
	m.progress.State = MigrationRolledBack
	return nil
}

// MigrateBatch migrates up to BatchSize keys from the source MPT,
// starting after the last migrated key. Returns the number of keys
// migrated and whether the migration is complete.
func (m *IncrementalMigrator) MigrateBatch() (int, bool) {
	m.mu.Lock()
	m.progress.State = MigrationRunning
	m.progress.MPTRoot = m.source.Hash()
	m.mu.Unlock()

	// Collect all key-value pairs from MPT.
	pairs := m.collectPairs()

	m.mu.Lock()
	m.progress.TotalKeys = uint64(len(pairs))
	start := int(m.progress.KeysMigrated)
	m.mu.Unlock()

	end := start + m.config.BatchSize
	if end > len(pairs) {
		end = len(pairs)
	}

	if start >= len(pairs) {
		m.mu.Lock()
		m.progress.State = MigrationComplete
		m.progress.BinaryRoot = m.dest.Hash()
		m.rootMap[m.progress.MPTRoot] = m.progress.BinaryRoot
		m.mu.Unlock()
		return 0, true
	}

	batch := pairs[start:end]
	m.migratePairs(batch)

	m.mu.Lock()
	m.progress.KeysMigrated += uint64(len(batch))
	if len(batch) > 0 {
		m.progress.LastKey = batch[len(batch)-1].key[:]
	}
	m.progress.BinaryRoot = m.dest.Hash()
	m.rootMap[m.progress.MPTRoot] = m.progress.BinaryRoot
	complete := int(m.progress.KeysMigrated) >= len(pairs)
	if complete {
		m.progress.State = MigrationComplete
	}
	m.mu.Unlock()

	return len(batch), complete
}

// MigrateAll runs the full migration in one call.
func (m *IncrementalMigrator) MigrateAll() {
	for {
		_, done := m.MigrateBatch()
		if done {
			return
		}
	}
}

// MigrateParallel uses parallel workers to migrate all keys. Each worker
// processes a portion of the key space concurrently.
func (m *IncrementalMigrator) MigrateParallel() {
	m.mu.Lock()
	m.progress.State = MigrationRunning
	m.progress.MPTRoot = m.source.Hash()
	m.mu.Unlock()

	pairs := m.collectPairs()

	m.mu.Lock()
	m.progress.TotalKeys = uint64(len(pairs))
	m.mu.Unlock()

	if len(pairs) == 0 {
		m.mu.Lock()
		m.progress.State = MigrationComplete
		m.progress.BinaryRoot = m.dest.Hash()
		m.rootMap[m.progress.MPTRoot] = m.progress.BinaryRoot
		m.mu.Unlock()
		return
	}

	// Split pairs among workers.
	workers := m.config.Workers
	if workers > len(pairs) {
		workers = len(pairs)
	}

	var counter atomic.Int64
	var destMu sync.Mutex
	var wg sync.WaitGroup

	chunkSize := (len(pairs) + workers - 1) / workers
	for w := 0; w < workers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if end > len(pairs) {
			end = len(pairs)
		}
		if start >= len(pairs) {
			break
		}

		wg.Add(1)
		go func(chunk []kvPair) {
			defer wg.Done()
			for _, kv := range chunk {
				destMu.Lock()
				m.dest.PutHashed(kv.key, kv.value)
				destMu.Unlock()
				counter.Add(1)
			}
		}(pairs[start:end])
	}

	wg.Wait()

	m.mu.Lock()
	m.progress.KeysMigrated = uint64(counter.Load())
	m.progress.State = MigrationComplete
	m.progress.BinaryRoot = m.dest.Hash()
	m.rootMap[m.progress.MPTRoot] = m.progress.BinaryRoot
	if len(pairs) > 0 {
		m.progress.LastKey = pairs[len(pairs)-1].key[:]
	}
	m.mu.Unlock()
}

// Pause pauses the migration at the current position.
func (m *IncrementalMigrator) Pause() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.progress.State == MigrationRunning {
		m.progress.State = MigrationPaused
	}
}

// SnapshotCount returns the number of rollback snapshots.
func (m *IncrementalMigrator) SnapshotCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.snapshots)
}

// collectPairs iterates over the source MPT and collects all key-value pairs.
func (m *IncrementalMigrator) collectPairs() []kvPair {
	it := NewIterator(m.source)
	var pairs []kvPair
	for it.Next() {
		hk := crypto.Keccak256Hash(it.Key)
		val := make([]byte, len(it.Value))
		copy(val, it.Value)
		pairs = append(pairs, kvPair{key: hk, value: val})
	}
	return pairs
}

// migratePairs inserts a batch of key-value pairs into the binary trie.
func (m *IncrementalMigrator) migratePairs(pairs []kvPair) {
	for _, kv := range pairs {
		m.dest.PutHashed(kv.key, kv.value)
	}
}

// copyBinaryTrie creates a deep copy of a BinaryTrie by re-inserting all
// key-value pairs.
func copyBinaryTrie(t *BinaryTrie) *BinaryTrie {
	cp := NewBinaryTrie()
	it := NewBinaryIterator(t)
	for it.Next() {
		var key types.Hash
		copy(key[:], it.Key())
		val := make([]byte, len(it.Value()))
		copy(val, it.Value())
		cp.PutHashed(key, val)
	}
	return cp
}

// VerifyMigration checks that the source MPT and destination binary trie
// contain the same set of key-value pairs. Returns an error describing the
// first mismatch, or nil if they match.
func VerifyMigration(mpt *Trie, bt *BinaryTrie) error {
	mptCount := 0
	it := NewIterator(mpt)
	for it.Next() {
		hk := crypto.Keccak256Hash(it.Key)
		val, err := bt.GetHashed(hk)
		if err != nil {
			return errors.New("migrate: key in MPT not found in binary trie")
		}
		if !migrateBytesEqual(val, it.Value) {
			return errors.New("migrate: value mismatch for key")
		}
		mptCount++
	}
	if mptCount != bt.Len() {
		return errors.New("migrate: binary trie has extra keys not in MPT")
	}
	return nil
}

func migrateBytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
