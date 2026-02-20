// state_history.go implements historical state reading and pruning for
// EIP-4444 compatible history management. It provides a way to read account
// and storage state at past block heights, define retention windows, and
// prune state that falls outside the retention period.
package state

import (
	"errors"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Errors for state history operations.
var (
	ErrBlockNotInRange    = errors.New("state_history: block number not in available range")
	ErrNoHistoryAvailable = errors.New("state_history: no history data available")
	ErrHistoryPruned      = errors.New("state_history: requested state has been pruned")
	ErrInvalidPruneRange  = errors.New("state_history: invalid prune range")
)

// AccountHistoryEntry represents the state of an account at a specific block.
type AccountHistoryEntry struct {
	BlockNumber uint64
	Address     types.Address
	Nonce       uint64
	Balance     []byte // RLP-encoded or raw big.Int bytes
	CodeHash    types.Hash
	StorageRoot types.Hash
	Proof       []byte // optional state proof for the entry
}

// StorageHistoryEntry represents a storage value at a specific block.
type StorageHistoryEntry struct {
	BlockNumber uint64
	Address     types.Address
	Slot        types.Hash
	Value       types.Hash
}

// HistoryRange defines the min/max block numbers for available history.
type HistoryRange struct {
	MinBlock uint64
	MaxBlock uint64
}

// Contains returns true if blockNum is within the history range (inclusive).
func (r HistoryRange) Contains(blockNum uint64) bool {
	return blockNum >= r.MinBlock && blockNum <= r.MaxBlock
}

// Width returns the number of blocks in the range.
func (r HistoryRange) Width() uint64 {
	if r.MaxBlock < r.MinBlock {
		return 0
	}
	return r.MaxBlock - r.MinBlock + 1
}

// StateHistoryReader provides read access to historical state. It stores
// account and storage snapshots at past block heights and supports range
// queries and pruning.
type StateHistoryReader struct {
	mu              sync.RWMutex
	accountHistory  map[types.Address][]AccountHistoryEntry
	storageHistory  map[storageHistoryKey][]StorageHistoryEntry
	historyRange    HistoryRange
	retentionWindow uint64 // number of blocks to retain
}

// storageHistoryKey uniquely identifies an address/slot pair.
type storageHistoryKey struct {
	addr types.Address
	slot types.Hash
}

// NewStateHistoryReader creates a new reader with the given retention window.
// The retention window defines how many blocks of history to keep.
func NewStateHistoryReader(retentionWindow uint64) *StateHistoryReader {
	return &StateHistoryReader{
		accountHistory:  make(map[types.Address][]AccountHistoryEntry),
		storageHistory:  make(map[storageHistoryKey][]StorageHistoryEntry),
		retentionWindow: retentionWindow,
	}
}

// Range returns the current history range.
func (r *StateHistoryReader) Range() HistoryRange {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.historyRange
}

// RetentionWindow returns the configured retention window.
func (r *StateHistoryReader) RetentionWindow() uint64 {
	return r.retentionWindow
}

// AddAccountEntry records an account state at a block height.
func (r *StateHistoryReader) AddAccountEntry(entry AccountHistoryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := r.accountHistory[entry.Address]
	entries = append(entries, entry)
	r.accountHistory[entry.Address] = entries

	r.updateRange(entry.BlockNumber)
}

// AddStorageEntry records a storage value at a block height.
func (r *StateHistoryReader) AddStorageEntry(entry StorageHistoryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := storageHistoryKey{addr: entry.Address, slot: entry.Slot}
	entries := r.storageHistory[key]
	entries = append(entries, entry)
	r.storageHistory[key] = entries

	r.updateRange(entry.BlockNumber)
}

// updateRange expands the history range to include the given block.
// Must be called with mu held.
func (r *StateHistoryReader) updateRange(block uint64) {
	if r.historyRange.MinBlock == 0 && r.historyRange.MaxBlock == 0 {
		r.historyRange.MinBlock = block
		r.historyRange.MaxBlock = block
		return
	}
	if block < r.historyRange.MinBlock {
		r.historyRange.MinBlock = block
	}
	if block > r.historyRange.MaxBlock {
		r.historyRange.MaxBlock = block
	}
}

// GetAccountAt returns the account state at the given block number.
// It finds the entry with the highest block number that is <= the target.
func (r *StateHistoryReader) GetAccountAt(addr types.Address, blockNum uint64) (*AccountHistoryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.historyRange.Contains(blockNum) {
		return nil, ErrBlockNotInRange
	}

	entries, ok := r.accountHistory[addr]
	if !ok || len(entries) == 0 {
		return nil, ErrNoHistoryAvailable
	}

	// Find the latest entry at or before blockNum.
	var best *AccountHistoryEntry
	for i := range entries {
		if entries[i].BlockNumber <= blockNum {
			if best == nil || entries[i].BlockNumber > best.BlockNumber {
				e := entries[i]
				best = &e
			}
		}
	}

	if best == nil {
		return nil, ErrNoHistoryAvailable
	}
	return best, nil
}

// GetStorageAt returns the storage value at the given block number.
func (r *StateHistoryReader) GetStorageAt(addr types.Address, slot types.Hash, blockNum uint64) (*StorageHistoryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.historyRange.Contains(blockNum) {
		return nil, ErrBlockNotInRange
	}

	key := storageHistoryKey{addr: addr, slot: slot}
	entries, ok := r.storageHistory[key]
	if !ok || len(entries) == 0 {
		return nil, ErrNoHistoryAvailable
	}

	var best *StorageHistoryEntry
	for i := range entries {
		if entries[i].BlockNumber <= blockNum {
			if best == nil || entries[i].BlockNumber > best.BlockNumber {
				e := entries[i]
				best = &e
			}
		}
	}

	if best == nil {
		return nil, ErrNoHistoryAvailable
	}
	return best, nil
}

// GetAccountHistory returns all account entries for a given address, sorted
// by block number.
func (r *StateHistoryReader) GetAccountHistory(addr types.Address) []AccountHistoryEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := r.accountHistory[addr]
	if len(entries) == 0 {
		return nil
	}

	result := make([]AccountHistoryEntry, len(entries))
	copy(result, entries)
	sort.Slice(result, func(i, j int) bool {
		return result[i].BlockNumber < result[j].BlockNumber
	})
	return result
}

// GetStorageHistory returns all storage entries for an address/slot, sorted
// by block number.
func (r *StateHistoryReader) GetStorageHistory(addr types.Address, slot types.Hash) []StorageHistoryEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := storageHistoryKey{addr: addr, slot: slot}
	entries := r.storageHistory[key]
	if len(entries) == 0 {
		return nil
	}

	result := make([]StorageHistoryEntry, len(entries))
	copy(result, entries)
	sort.Slice(result, func(i, j int) bool {
		return result[i].BlockNumber < result[j].BlockNumber
	})
	return result
}

// PruneHistory removes all entries with block numbers strictly less than
// beforeBlock. Returns the number of entries pruned.
func (r *StateHistoryReader) PruneHistory(beforeBlock uint64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if beforeBlock == 0 {
		return 0, ErrInvalidPruneRange
	}

	pruned := 0

	// Prune account history.
	for addr, entries := range r.accountHistory {
		kept := entries[:0]
		for _, e := range entries {
			if e.BlockNumber >= beforeBlock {
				kept = append(kept, e)
			} else {
				pruned++
			}
		}
		if len(kept) == 0 {
			delete(r.accountHistory, addr)
		} else {
			r.accountHistory[addr] = kept
		}
	}

	// Prune storage history.
	for key, entries := range r.storageHistory {
		kept := entries[:0]
		for _, e := range entries {
			if e.BlockNumber >= beforeBlock {
				kept = append(kept, e)
			} else {
				pruned++
			}
		}
		if len(kept) == 0 {
			delete(r.storageHistory, key)
		} else {
			r.storageHistory[key] = kept
		}
	}

	// Update min range.
	if beforeBlock > r.historyRange.MinBlock {
		r.historyRange.MinBlock = beforeBlock
	}

	return pruned, nil
}

// AccountEntryCount returns the total number of account history entries.
func (r *StateHistoryReader) AccountEntryCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, entries := range r.accountHistory {
		count += len(entries)
	}
	return count
}

// StorageEntryCount returns the total number of storage history entries.
func (r *StateHistoryReader) StorageEntryCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, entries := range r.storageHistory {
		count += len(entries)
	}
	return count
}

// UniqueAddressCount returns the number of unique addresses in history.
func (r *StateHistoryReader) UniqueAddressCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.accountHistory)
}
