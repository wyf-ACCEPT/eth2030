// journal_manager.go implements a high-level journal manager for tracking state
// modifications during block processing. It builds on the existing journal
// infrastructure (journal.go, state_journal.go, journal_extended.go) by adding
// block-level checkpoint management, per-transaction journal segmentation,
// modification categorization, and comprehensive metrics for journal monitoring.
//
// The JournalManager is designed for use by block processors that need to:
// - Track modifications per transaction within a block
// - Create named checkpoints at transaction boundaries
// - Roll back to any transaction boundary on failure
// - Collect detailed metrics on journal usage patterns
package state

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
)

// Errors returned by the JournalManager.
var (
	ErrJournalMgrCheckpointNotFound = errors.New("journal_manager: checkpoint not found")
	ErrJournalMgrAlreadyFinalized   = errors.New("journal_manager: block already finalized")
	ErrJournalMgrNoActiveTx         = errors.New("journal_manager: no active transaction")
	ErrJournalMgrTxActive           = errors.New("journal_manager: transaction already active")
)

// ModKind classifies the type of a tracked modification.
type ModKind uint8

const (
	ModAccountCreate ModKind = iota // New account created
	ModBalanceChange                // Balance modified
	ModNonceChange                  // Nonce modified
	ModStorageWrite                 // Storage slot written
	ModCodeDeploy                   // Code set on account
	ModSelfDestruct                 // Account self-destructed
)

// String returns a human-readable label for the modification kind.
func (k ModKind) String() string {
	switch k {
	case ModAccountCreate:
		return "account_create"
	case ModBalanceChange:
		return "balance_change"
	case ModNonceChange:
		return "nonce_change"
	case ModStorageWrite:
		return "storage_write"
	case ModCodeDeploy:
		return "code_deploy"
	case ModSelfDestruct:
		return "self_destruct"
	default:
		return "unknown"
	}
}

// Modification describes a single tracked state change with metadata.
type Modification struct {
	Kind    ModKind
	Address types.Address
	Key     types.Hash // storage key (zero for non-storage mods)
	TxIndex int        // transaction index within the block
}

// Checkpoint represents a named journal position that can be reverted to.
type Checkpoint struct {
	Name         string
	TxIndex      int // transaction index at which checkpoint was created
	JournalIndex int // journal entry index at checkpoint time
	SnapshotID   int // underlying journal snapshot ID
}

// JournalManagerMetrics contains metrics for journal manager monitoring.
type JournalManagerMetrics struct {
	TotalModifications atomic.Int64
	AccountCreates     atomic.Int64
	BalanceChanges     atomic.Int64
	NonceChanges       atomic.Int64
	StorageWrites      atomic.Int64
	CodeDeploys        atomic.Int64
	SelfDestructs      atomic.Int64
	CheckpointsCreated atomic.Int64
	Rollbacks          atomic.Int64
	TransactionsCount  atomic.Int64
	PeakJournalEntries atomic.Int64
}

// JournalManager provides block-level journal management with per-transaction
// segmentation, named checkpoints, and modification tracking.
type JournalManager struct {
	mu            sync.Mutex
	journal       *Journal
	statedb       *MemoryStateDB
	modifications []Modification
	checkpoints   map[string]*Checkpoint
	cpOrder       []string // checkpoint creation order
	txIndex       int      // current transaction index
	txActive      bool     // whether a transaction is in progress
	finalized     bool     // whether the block has been finalized
	metrics       JournalManagerMetrics

	// Per-transaction modification counts for the current tx.
	txModCount int
}

// NewJournalManager creates a new journal manager bound to the given state.
func NewJournalManager(statedb *MemoryStateDB) *JournalManager {
	return &JournalManager{
		journal:     NewJournal(),
		statedb:     statedb,
		checkpoints: make(map[string]*Checkpoint),
		txIndex:     -1,
	}
}

// BeginTransaction signals the start of a new transaction. A checkpoint is
// automatically created at the transaction boundary.
func (jm *JournalManager) BeginTransaction() error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	if jm.finalized {
		return ErrJournalMgrAlreadyFinalized
	}
	if jm.txActive {
		return ErrJournalMgrTxActive
	}

	jm.txIndex++
	jm.txActive = true
	jm.txModCount = 0
	jm.metrics.TransactionsCount.Add(1)

	// Create an automatic checkpoint at the transaction boundary.
	name := fmt.Sprintf("tx_%d", jm.txIndex)
	snapID := jm.journal.Snapshot()
	cp := &Checkpoint{
		Name:         name,
		TxIndex:      jm.txIndex,
		JournalIndex: jm.journal.Length(),
		SnapshotID:   snapID,
	}
	jm.checkpoints[name] = cp
	jm.cpOrder = append(jm.cpOrder, name)
	jm.metrics.CheckpointsCreated.Add(1)
	return nil
}

// EndTransaction signals the end of the current transaction.
func (jm *JournalManager) EndTransaction() error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	if !jm.txActive {
		return ErrJournalMgrNoActiveTx
	}
	jm.txActive = false
	return nil
}

// CreateCheckpoint creates a named checkpoint at the current journal position.
func (jm *JournalManager) CreateCheckpoint(name string) (*Checkpoint, error) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	if jm.finalized {
		return nil, ErrJournalMgrAlreadyFinalized
	}

	snapID := jm.journal.Snapshot()
	cp := &Checkpoint{
		Name:         name,
		TxIndex:      jm.txIndex,
		JournalIndex: jm.journal.Length(),
		SnapshotID:   snapID,
	}
	jm.checkpoints[name] = cp
	jm.cpOrder = append(jm.cpOrder, name)
	jm.metrics.CheckpointsCreated.Add(1)
	return cp, nil
}

// RollbackToCheckpoint reverts all state changes back to the named checkpoint.
func (jm *JournalManager) RollbackToCheckpoint(name string) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	cp, ok := jm.checkpoints[name]
	if !ok {
		return ErrJournalMgrCheckpointNotFound
	}

	err := jm.journal.RevertTo(cp.SnapshotID, jm.statedb)
	if err != nil {
		return err
	}

	// Trim modifications back to the checkpoint journal index.
	trimmed := jm.modifications[:0]
	for _, mod := range jm.modifications {
		if mod.TxIndex < cp.TxIndex || (mod.TxIndex == cp.TxIndex && len(trimmed) < cp.JournalIndex) {
			trimmed = append(trimmed, mod)
		}
	}
	jm.modifications = trimmed

	// Invalidate checkpoints created after this one.
	cpIdx := -1
	for i, n := range jm.cpOrder {
		if n == name {
			cpIdx = i
			break
		}
	}
	if cpIdx >= 0 {
		for _, n := range jm.cpOrder[cpIdx+1:] {
			delete(jm.checkpoints, n)
		}
		jm.cpOrder = jm.cpOrder[:cpIdx+1]
	}

	jm.metrics.Rollbacks.Add(1)
	return nil
}

// TrackAccountCreate records a new account creation.
func (jm *JournalManager) TrackAccountCreate(addr types.Address) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	jm.journal.Append(AccountCreated{Address: addr})
	jm.addModification(ModAccountCreate, addr, types.Hash{})
	jm.metrics.AccountCreates.Add(1)
}

// TrackBalanceChange records a balance modification. The previous balance
// is captured from the current state for revert support.
func (jm *JournalManager) TrackBalanceChange(addr types.Address, prevBalance *big.Int) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	jm.journal.Append(JrnlBalanceChange{
		Address:     addr,
		PrevBalance: new(big.Int).Set(prevBalance),
	})
	jm.addModification(ModBalanceChange, addr, types.Hash{})
	jm.metrics.BalanceChanges.Add(1)
}

// TrackNonceChange records a nonce modification.
func (jm *JournalManager) TrackNonceChange(addr types.Address, prevNonce uint64) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	jm.journal.Append(JrnlNonceChange{
		Address:   addr,
		PrevNonce: prevNonce,
	})
	jm.addModification(ModNonceChange, addr, types.Hash{})
	jm.metrics.NonceChanges.Add(1)
}

// TrackStorageWrite records a storage write operation.
func (jm *JournalManager) TrackStorageWrite(addr types.Address, key types.Hash, prevValue types.Hash) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	jm.journal.Append(JrnlStorageChange{
		Address:   addr,
		Key:       key,
		PrevValue: prevValue,
	})
	jm.addModification(ModStorageWrite, addr, key)
	jm.metrics.StorageWrites.Add(1)
}

// TrackCodeDeploy records a code deployment.
func (jm *JournalManager) TrackCodeDeploy(addr types.Address, prevCode []byte, prevCodeHash []byte) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	jm.journal.Append(JrnlCodeChange{
		Address:      addr,
		PrevCode:     prevCode,
		PrevCodeHash: prevCodeHash,
	})
	jm.addModification(ModCodeDeploy, addr, types.Hash{})
	jm.metrics.CodeDeploys.Add(1)
}

// TrackSelfDestruct records a self-destruct operation.
func (jm *JournalManager) TrackSelfDestruct(addr types.Address, prevBalance *big.Int, prevNonce uint64) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	jm.journal.Append(AccountSuicided{
		Address:     addr,
		PrevBalance: new(big.Int).Set(prevBalance),
		PrevNonce:   prevNonce,
	})
	jm.addModification(ModSelfDestruct, addr, types.Hash{})
	jm.metrics.SelfDestructs.Add(1)
}

// addModification appends a modification record (must be called with lock held).
func (jm *JournalManager) addModification(kind ModKind, addr types.Address, key types.Hash) {
	jm.modifications = append(jm.modifications, Modification{
		Kind:    kind,
		Address: addr,
		Key:     key,
		TxIndex: jm.txIndex,
	})
	jm.txModCount++
	jm.metrics.TotalModifications.Add(1)

	// Track peak journal entries.
	entryCount := int64(jm.journal.Length())
	if entryCount > jm.metrics.PeakJournalEntries.Load() {
		jm.metrics.PeakJournalEntries.Store(entryCount)
	}
}

// Modifications returns a copy of all tracked modifications.
func (jm *JournalManager) Modifications() []Modification {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	cp := make([]Modification, len(jm.modifications))
	copy(cp, jm.modifications)
	return cp
}

// ModificationsForTx returns modifications for a specific transaction index.
func (jm *JournalManager) ModificationsForTx(txIdx int) []Modification {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	var result []Modification
	for _, m := range jm.modifications {
		if m.TxIndex == txIdx {
			result = append(result, m)
		}
	}
	return result
}

// ModificationCountByKind returns a map of modification kind to count.
func (jm *JournalManager) ModificationCountByKind() map[ModKind]int {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	counts := make(map[ModKind]int)
	for _, m := range jm.modifications {
		counts[m.Kind]++
	}
	return counts
}

// TouchedAddresses returns the set of addresses that have been modified.
func (jm *JournalManager) TouchedAddresses() map[types.Address]struct{} {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	addrs := make(map[types.Address]struct{})
	for _, m := range jm.modifications {
		addrs[m.Address] = struct{}{}
	}
	return addrs
}

// JournalLength returns the current number of journal entries.
func (jm *JournalManager) JournalLength() int {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return jm.journal.Length()
}

// CheckpointCount returns the number of active checkpoints.
func (jm *JournalManager) CheckpointCount() int {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return len(jm.checkpoints)
}

// TxIndex returns the current transaction index.
func (jm *JournalManager) TxIndex() int {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return jm.txIndex
}

// TxModCount returns the modification count for the current transaction.
func (jm *JournalManager) TxModCount() int {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return jm.txModCount
}

// IsFinalized returns true if the block has been finalized.
func (jm *JournalManager) IsFinalized() bool {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return jm.finalized
}

// Finalize marks the block as finalized, preventing further modifications.
func (jm *JournalManager) Finalize() {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.finalized = true
	jm.txActive = false
}

// Metrics returns a snapshot of the current journal manager metrics.
func (jm *JournalManager) Metrics() *JournalManagerMetrics {
	m := new(JournalManagerMetrics)
	m.TotalModifications.Store(jm.metrics.TotalModifications.Load())
	m.AccountCreates.Store(jm.metrics.AccountCreates.Load())
	m.BalanceChanges.Store(jm.metrics.BalanceChanges.Load())
	m.NonceChanges.Store(jm.metrics.NonceChanges.Load())
	m.StorageWrites.Store(jm.metrics.StorageWrites.Load())
	m.CodeDeploys.Store(jm.metrics.CodeDeploys.Load())
	m.SelfDestructs.Store(jm.metrics.SelfDestructs.Load())
	m.CheckpointsCreated.Store(jm.metrics.CheckpointsCreated.Load())
	m.Rollbacks.Store(jm.metrics.Rollbacks.Load())
	m.TransactionsCount.Store(jm.metrics.TransactionsCount.Load())
	m.PeakJournalEntries.Store(jm.metrics.PeakJournalEntries.Load())
	return m
}

// GetCheckpoint returns the named checkpoint, or nil if not found.
func (jm *JournalManager) GetCheckpoint(name string) *Checkpoint {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return jm.checkpoints[name]
}

// Reset clears all journal state, modifications, and checkpoints.
func (jm *JournalManager) Reset() {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	jm.journal.Reset()
	jm.modifications = jm.modifications[:0]
	jm.checkpoints = make(map[string]*Checkpoint)
	jm.cpOrder = jm.cpOrder[:0]
	jm.txIndex = -1
	jm.txActive = false
	jm.finalized = false
	jm.txModCount = 0
}
