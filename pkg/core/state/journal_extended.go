// journal_extended.go extends the core journal with size tracking, batch
// journal operations, compound entry types (account destruct with full
// storage rollback), and journal metrics. It complements journal.go by
// providing higher-level journal management without modifying the base
// journal implementation.
package state

import (
	"math/big"
	"sync/atomic"
	"unsafe"

	"github.com/eth2030/eth2030/core/types"
)

// JournalMetrics tracks journal statistics using atomic counters for
// lock-free concurrent access.
type JournalMetrics struct {
	TotalEntries   atomic.Int64 // Total entries ever appended.
	TotalReverts   atomic.Int64 // Total entries ever reverted.
	TotalSnapshots atomic.Int64 // Total snapshots ever created.
	PeakSize       atomic.Int64 // Peak journal size in estimated bytes.
}

// ExtendedJournal wraps a journal with size tracking, batch operations,
// and metrics. It provides a higher-level API over the base journal.
type ExtendedJournal struct {
	inner   *journal
	metrics JournalMetrics
	size    int64 // estimated current byte size of journal entries
}

// NewExtendedJournal creates a new extended journal wrapping a fresh
// base journal.
func NewExtendedJournal() *ExtendedJournal {
	return &ExtendedJournal{
		inner: newJournal(),
	}
}

// WrapJournal creates an extended journal wrapping an existing journal.
func WrapJournal(j *journal) *ExtendedJournal {
	return &ExtendedJournal{
		inner: j,
	}
}

// Inner returns the underlying base journal.
func (ej *ExtendedJournal) Inner() *journal {
	return ej.inner
}

// Append adds a journal entry and tracks its estimated size.
func (ej *ExtendedJournal) Append(entry journalEntry) {
	ej.inner.append(entry)
	entrySize := estimateEntrySize(entry)
	ej.size += entrySize
	ej.metrics.TotalEntries.Add(1)

	// Track peak size.
	if ej.size > ej.metrics.PeakSize.Load() {
		ej.metrics.PeakSize.Store(ej.size)
	}
}

// AppendBatch adds multiple journal entries atomically. This is more
// efficient than calling Append repeatedly for grouped changes.
func (ej *ExtendedJournal) AppendBatch(entries []journalEntry) {
	var batchSize int64
	for _, entry := range entries {
		ej.inner.append(entry)
		batchSize += estimateEntrySize(entry)
	}
	ej.size += batchSize
	ej.metrics.TotalEntries.Add(int64(len(entries)))

	if ej.size > ej.metrics.PeakSize.Load() {
		ej.metrics.PeakSize.Store(ej.size)
	}
}

// Snapshot creates a snapshot and returns its ID, tracking the event.
func (ej *ExtendedJournal) Snapshot() int {
	ej.metrics.TotalSnapshots.Add(1)
	return ej.inner.snapshot()
}

// RevertToSnapshot reverts all entries back to the given snapshot,
// tracking the number of reverted entries and updating size.
func (ej *ExtendedJournal) RevertToSnapshot(id int, s *MemoryStateDB) {
	prevLen := ej.inner.length()
	ej.inner.revertToSnapshot(id, s)
	newLen := ej.inner.length()
	reverted := int64(prevLen - newLen)
	ej.metrics.TotalReverts.Add(reverted)

	// Recalculate size after revert (approximate by scaling).
	if prevLen > 0 {
		ej.size = ej.size * int64(newLen) / int64(prevLen)
	}
	if ej.size < 0 {
		ej.size = 0
	}
}

// Length returns the current number of journal entries.
func (ej *ExtendedJournal) Length() int {
	return ej.inner.length()
}

// EstimatedSize returns the estimated byte size of the journal.
func (ej *ExtendedJournal) EstimatedSize() int64 {
	return ej.size
}

// Metrics returns a snapshot of the journal metrics.
func (ej *ExtendedJournal) Metrics() *JournalMetrics {
	m := new(JournalMetrics)
	m.TotalEntries.Store(ej.metrics.TotalEntries.Load())
	m.TotalReverts.Store(ej.metrics.TotalReverts.Load())
	m.TotalSnapshots.Store(ej.metrics.TotalSnapshots.Load())
	m.PeakSize.Store(ej.metrics.PeakSize.Load())
	return m
}

// Reset clears all journal entries and resets size tracking.
func (ej *ExtendedJournal) Reset() {
	ej.inner = newJournal()
	ej.size = 0
}

// --- Compound journal entry types ---

// accountDestructChange records the full destruction of an account,
// capturing all state needed for complete rollback: balance, nonce,
// code, and all dirty storage slots. This is used when an account
// is self-destructed and needs to be fully restored on revert.
type accountDestructChange struct {
	addr          types.Address
	prevAccount   types.Account
	prevCode      []byte
	prevCodeHash  []byte
	prevStorage   map[types.Hash]types.Hash // snapshot of dirty storage
	wasDestructed bool
}

func (ch accountDestructChange) revert(s *MemoryStateDB) {
	obj := s.getStateObject(ch.addr)
	if obj == nil {
		obj = newStateObject()
		s.stateObjects[ch.addr] = obj
	}
	obj.account.Nonce = ch.prevAccount.Nonce
	obj.account.Balance = new(big.Int).Set(ch.prevAccount.Balance)
	obj.account.Root = ch.prevAccount.Root
	obj.account.CodeHash = make([]byte, len(ch.prevCodeHash))
	copy(obj.account.CodeHash, ch.prevCodeHash)
	obj.code = make([]byte, len(ch.prevCode))
	copy(obj.code, ch.prevCode)
	obj.selfDestructed = ch.wasDestructed

	// Restore dirty storage.
	obj.dirtyStorage = make(map[types.Hash]types.Hash, len(ch.prevStorage))
	for k, v := range ch.prevStorage {
		obj.dirtyStorage[k] = v
	}
}

// touchChange records that an account was "touched" (accessed) without
// any actual state modification. Used for EIP-161 tracking where empty
// accounts need to be removed after being touched.
type touchChange struct {
	addr types.Address
}

func (ch touchChange) revert(s *MemoryStateDB) {
	// Touch is a no-op for revert; the account is not modified.
}

// --- Utility functions for building compound journal entries ---

// CaptureAccountState builds an accountDestructChange entry that captures
// the full current state of the given address. Call this before performing
// a self-destruct to enable complete rollback.
func CaptureAccountState(s *MemoryStateDB, addr types.Address) accountDestructChange {
	ch := accountDestructChange{
		addr:          addr,
		wasDestructed: false,
	}

	obj := s.getStateObject(addr)
	if obj == nil {
		ch.prevAccount = types.NewAccount()
		ch.prevCodeHash = types.EmptyCodeHash.Bytes()
		return ch
	}

	ch.wasDestructed = obj.selfDestructed
	ch.prevAccount = types.Account{
		Nonce:    obj.account.Nonce,
		Balance:  new(big.Int).Set(obj.account.Balance),
		Root:     obj.account.Root,
		CodeHash: make([]byte, len(obj.account.CodeHash)),
	}
	copy(ch.prevAccount.CodeHash, obj.account.CodeHash)

	ch.prevCode = make([]byte, len(obj.code))
	copy(ch.prevCode, obj.code)

	ch.prevCodeHash = make([]byte, len(obj.account.CodeHash))
	copy(ch.prevCodeHash, obj.account.CodeHash)

	// Capture dirty storage snapshot.
	ch.prevStorage = make(map[types.Hash]types.Hash, len(obj.dirtyStorage))
	for k, v := range obj.dirtyStorage {
		ch.prevStorage[k] = v
	}

	return ch
}

// --- Size estimation ---

// estimateEntrySize returns an approximate byte size for a journal entry.
// This is used for memory accounting, not for serialization.
func estimateEntrySize(entry journalEntry) int64 {
	const ptrSize = int64(unsafe.Sizeof(uintptr(0)))
	const hashSize = int64(types.HashLength)
	const addrSize = int64(types.AddressLength)

	switch e := entry.(type) {
	case createAccountChange:
		size := addrSize + ptrSize // addr + prev pointer
		if e.prev != nil {
			size += estimateStateObjectSize(e.prev)
		}
		return size

	case balanceChange:
		// addr + prev big.Int (approx 40 bytes)
		return addrSize + 40

	case nonceChange:
		// addr + prev uint64
		return addrSize + 8

	case codeChange:
		// addr + prevCode + prevHash
		return addrSize + int64(len(e.prevCode)) + int64(len(e.prevHash))

	case storageChange:
		// addr + key + prev + prevExists
		return addrSize + hashSize + hashSize + 1

	case selfDestructChange:
		// addr + prevDestructed + prevBalance
		return addrSize + 1 + 40

	case accessListAddAccountChange:
		return addrSize

	case accessListAddSlotChange:
		return addrSize + hashSize

	case transientStorageChange:
		return addrSize + hashSize + hashSize

	case logChange:
		return hashSize + 8

	case refundChange:
		return 8

	case accountDestructChange:
		size := addrSize + 40 + 8 + hashSize // account fields
		size += int64(len(e.prevCode))
		size += int64(len(e.prevCodeHash))
		size += int64(len(e.prevStorage)) * (hashSize + hashSize)
		return size

	case touchChange:
		return addrSize

	default:
		// Unknown entry type; use a conservative estimate.
		return 64
	}
}

// estimateStateObjectSize estimates memory for a stateObject.
func estimateStateObjectSize(obj *stateObject) int64 {
	var size int64
	size += 40                      // account balance big.Int
	size += 8                       // nonce
	size += int64(types.HashLength) // root
	size += int64(len(obj.account.CodeHash))
	size += int64(len(obj.code))
	size += int64(len(obj.dirtyStorage)) * int64(2*types.HashLength)
	size += int64(len(obj.committedStorage)) * int64(2*types.HashLength)
	return size
}

// --- Journal entry counters by type ---

// JournalStats contains a breakdown of journal entries by type.
type JournalStats struct {
	Creates         int
	Balances        int
	Nonces          int
	Codes           int
	Storages        int
	SelfDestructs   int
	AccessListAddrs int
	AccessListSlots int
	TransientStores int
	Logs            int
	Refunds         int
	Destructs       int
	Touches         int
	Other           int
}

// CountEntries scans the extended journal and returns a breakdown of
// entry types. This is useful for diagnostics and debugging.
func (ej *ExtendedJournal) CountEntries() JournalStats {
	var stats JournalStats
	for _, entry := range ej.inner.entries {
		switch entry.(type) {
		case createAccountChange:
			stats.Creates++
		case balanceChange:
			stats.Balances++
		case nonceChange:
			stats.Nonces++
		case codeChange:
			stats.Codes++
		case storageChange:
			stats.Storages++
		case selfDestructChange:
			stats.SelfDestructs++
		case accessListAddAccountChange:
			stats.AccessListAddrs++
		case accessListAddSlotChange:
			stats.AccessListSlots++
		case transientStorageChange:
			stats.TransientStores++
		case logChange:
			stats.Logs++
		case refundChange:
			stats.Refunds++
		case accountDestructChange:
			stats.Destructs++
		case touchChange:
			stats.Touches++
		default:
			stats.Other++
		}
	}
	return stats
}

// Total returns the total number of entries across all types.
func (js JournalStats) Total() int {
	return js.Creates + js.Balances + js.Nonces + js.Codes +
		js.Storages + js.SelfDestructs + js.AccessListAddrs +
		js.AccessListSlots + js.TransientStores + js.Logs +
		js.Refunds + js.Destructs + js.Touches + js.Other
}
