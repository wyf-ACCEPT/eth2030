// state_journal.go provides a standalone, exported state change journal for
// tracking and reverting modifications to a MemoryStateDB. It complements
// the unexported journal in journal.go by exposing a public API with typed
// entry structs suitable for use by external packages (e.g., transaction
// processors and block builders).
//
// Entry types are prefixed with "Jrnl" to avoid collision with the diff types
// in state_diff.go (BalanceChange, NonceChange, etc.).
package state

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// Errors returned by the Journal.
var (
	ErrInvalidSnapshot = errors.New("journal: invalid snapshot ID")
	ErrSnapshotBehind  = errors.New("journal: snapshot is behind current position")
)

// JournalEntry is a revertible state change. Each implementation knows how
// to undo its change on a MemoryStateDB.
type JournalEntry interface {
	// Revert undoes the state change recorded by this entry.
	Revert(statedb *MemoryStateDB)
}

// --- Concrete journal entry types ---

// JrnlBalanceChange records a balance modification.
type JrnlBalanceChange struct {
	Address     types.Address
	PrevBalance *big.Int
}

// Revert restores the previous balance.
func (e JrnlBalanceChange) Revert(s *MemoryStateDB) {
	if obj := s.getStateObject(e.Address); obj != nil {
		obj.account.Balance = new(big.Int).Set(e.PrevBalance)
	}
}

// JrnlNonceChange records a nonce modification.
type JrnlNonceChange struct {
	Address   types.Address
	PrevNonce uint64
}

// Revert restores the previous nonce.
func (e JrnlNonceChange) Revert(s *MemoryStateDB) {
	if obj := s.getStateObject(e.Address); obj != nil {
		obj.account.Nonce = e.PrevNonce
	}
}

// JrnlStorageChange records a storage slot modification.
type JrnlStorageChange struct {
	Address   types.Address
	Key       types.Hash
	PrevValue types.Hash
}

// Revert restores the previous storage value.
func (e JrnlStorageChange) Revert(s *MemoryStateDB) {
	if obj := s.getStateObject(e.Address); obj != nil {
		if e.PrevValue == (types.Hash{}) {
			delete(obj.dirtyStorage, e.Key)
		} else {
			obj.dirtyStorage[e.Key] = e.PrevValue
		}
	}
}

// JrnlCodeChange records a code modification.
type JrnlCodeChange struct {
	Address      types.Address
	PrevCode     []byte
	PrevCodeHash []byte
}

// Revert restores the previous code and code hash.
func (e JrnlCodeChange) Revert(s *MemoryStateDB) {
	if obj := s.getStateObject(e.Address); obj != nil {
		obj.code = e.PrevCode
		obj.account.CodeHash = e.PrevCodeHash
	}
}

// AccountCreated records the creation of a new account. On revert, the
// account is removed from the state.
type AccountCreated struct {
	Address types.Address
}

// Revert deletes the created account.
func (e AccountCreated) Revert(s *MemoryStateDB) {
	delete(s.stateObjects, e.Address)
}

// AccountSuicided records a SELFDESTRUCT operation, capturing the full
// account state needed for rollback.
type AccountSuicided struct {
	Address     types.Address
	PrevBalance *big.Int
	PrevNonce   uint64
	PrevCode    []byte
}

// Revert restores the self-destructed account to its previous state.
func (e AccountSuicided) Revert(s *MemoryStateDB) {
	obj := s.getStateObject(e.Address)
	if obj == nil {
		obj = newStateObject()
		s.stateObjects[e.Address] = obj
	}
	obj.selfDestructed = false
	obj.account.Balance = new(big.Int).Set(e.PrevBalance)
	obj.account.Nonce = e.PrevNonce
	if e.PrevCode != nil {
		obj.code = make([]byte, len(e.PrevCode))
		copy(obj.code, e.PrevCode)
	}
}

// JrnlLogEntry records the addition of a log entry. On revert, logs are
// truncated back to the previous length.
type JrnlLogEntry struct {
	TxHash  [32]byte
	PrevLen int // length of log slice before this entry was added
}

// Revert truncates the log list back to the previous length.
func (e JrnlLogEntry) Revert(s *MemoryStateDB) {
	txHash := types.BytesToHash(e.TxHash[:])
	if logs, ok := s.logs[txHash]; ok {
		s.logs[txHash] = logs[:e.PrevLen]
		if e.PrevLen == 0 {
			delete(s.logs, txHash)
		}
	}
}

// JrnlRefundChange records a modification to the gas refund counter.
type JrnlRefundChange struct {
	PrevRefund uint64
}

// Revert restores the previous refund value.
func (e JrnlRefundChange) Revert(s *MemoryStateDB) {
	s.refund = e.PrevRefund
}

// --- Journal ---

// Journal tracks state modifications and supports snapshot/revert for
// transaction-level atomicity. It maintains an append-only list of entries
// and a stack of snapshot positions.
type Journal struct {
	entries   []JournalEntry
	snapshots []int // each element is the entry index at snapshot time
}

// NewJournal creates a new empty journal.
func NewJournal() *Journal {
	return &Journal{}
}

// Append adds a new journal entry.
func (j *Journal) Append(entry JournalEntry) {
	j.entries = append(j.entries, entry)
}

// Snapshot records the current journal position and returns a snapshot ID.
// The ID is the index into the snapshots slice (0-based).
func (j *Journal) Snapshot() int {
	id := len(j.snapshots)
	j.snapshots = append(j.snapshots, len(j.entries))
	return id
}

// RevertTo undoes all journal entries back to the given snapshot, applying
// each entry's Revert method in reverse order. Entries after the snapshot
// point are removed, and any snapshots taken after the given one are
// invalidated.
func (j *Journal) RevertTo(snapshot int, statedb *MemoryStateDB) error {
	if snapshot < 0 || snapshot >= len(j.snapshots) {
		return ErrInvalidSnapshot
	}

	idx := j.snapshots[snapshot]
	if idx > len(j.entries) {
		return ErrSnapshotBehind
	}

	// Revert entries in reverse order.
	for i := len(j.entries) - 1; i >= idx; i-- {
		j.entries[i].Revert(statedb)
	}

	// Truncate entries and invalidate later snapshots.
	j.entries = j.entries[:idx]
	j.snapshots = j.snapshots[:snapshot]
	return nil
}

// Length returns the current number of journal entries.
func (j *Journal) Length() int {
	return len(j.entries)
}

// SnapshotCount returns the number of active snapshots.
func (j *Journal) SnapshotCount() int {
	return len(j.snapshots)
}

// Reset clears all entries and snapshots, returning the journal to its
// initial empty state.
func (j *Journal) Reset() {
	j.entries = j.entries[:0]
	j.snapshots = j.snapshots[:0]
}

// Entries returns a copy of the current journal entries for inspection.
func (j *Journal) Entries() []JournalEntry {
	cp := make([]JournalEntry, len(j.entries))
	copy(cp, j.entries)
	return cp
}

// --- Convenience constructors ---

// NewJrnlBalanceChange creates a JrnlBalanceChange entry from the current state.
func NewJrnlBalanceChange(s *MemoryStateDB, addr types.Address) JrnlBalanceChange {
	bal := s.GetBalance(addr)
	return JrnlBalanceChange{Address: addr, PrevBalance: bal}
}

// NewJrnlNonceChange creates a JrnlNonceChange entry from the current state.
func NewJrnlNonceChange(s *MemoryStateDB, addr types.Address) JrnlNonceChange {
	return JrnlNonceChange{Address: addr, PrevNonce: s.GetNonce(addr)}
}

// NewJrnlStorageChange creates a JrnlStorageChange entry from the current state.
func NewJrnlStorageChange(s *MemoryStateDB, addr types.Address, key types.Hash) JrnlStorageChange {
	return JrnlStorageChange{
		Address:   addr,
		Key:       key,
		PrevValue: s.GetState(addr, key),
	}
}

// NewJrnlCodeChange creates a JrnlCodeChange entry from the current state.
func NewJrnlCodeChange(s *MemoryStateDB, addr types.Address) JrnlCodeChange {
	code := s.GetCode(addr)
	codeCopy := make([]byte, len(code))
	copy(codeCopy, code)

	codeHash := s.GetCodeHash(addr)
	hashBytes := make([]byte, len(codeHash))
	copy(hashBytes, codeHash[:])

	return JrnlCodeChange{
		Address:      addr,
		PrevCode:     codeCopy,
		PrevCodeHash: hashBytes,
	}
}

// NewAccountSuicided creates an AccountSuicided entry from the current state.
func NewAccountSuicided(s *MemoryStateDB, addr types.Address) AccountSuicided {
	code := s.GetCode(addr)
	codeCopy := make([]byte, len(code))
	copy(codeCopy, code)

	return AccountSuicided{
		Address:     addr,
		PrevBalance: s.GetBalance(addr),
		PrevNonce:   s.GetNonce(addr),
		PrevCode:    codeCopy,
	}
}

// NewJrnlRefundChange creates a JrnlRefundChange entry from the current state.
func NewJrnlRefundChange(s *MemoryStateDB) JrnlRefundChange {
	return JrnlRefundChange{PrevRefund: s.GetRefund()}
}
