// tech_debt_reset.go implements comprehensive state cleanup for removing
// legacy storage cruft (L+ era). This enables the Ethereum state to be
// compacted by identifying and removing dead code, migrating storage layouts,
// and pruning stale entries. Supports a dry-run mode for estimation before
// committing changes.
package state

import (
	"errors"
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Tech debt reset errors.
var (
	ErrTDRNilState         = errors.New("tech_debt_reset: nil state")
	ErrTDREmptyContracts   = errors.New("tech_debt_reset: no contracts specified")
	ErrTDRNilConfig        = errors.New("tech_debt_reset: nil config")
	ErrTDRLayoutMismatch   = errors.New("tech_debt_reset: old layout size does not match new layout")
	ErrTDRContractNotFound = errors.New("tech_debt_reset: contract not found in state")
	ErrTDRCompactFailed    = errors.New("tech_debt_reset: state compaction failed")
)

// TechDebtConfig specifies what to clean up during a state reset.
type TechDebtConfig struct {
	// RemoveEmptyAccounts removes accounts with zero balance, zero nonce,
	// and empty code hash.
	RemoveEmptyAccounts bool

	// RemoveZeroStorageSlots removes storage slots whose value is zero.
	RemoveZeroStorageSlots bool

	// RemoveSelfDestructed removes self-destructed accounts.
	RemoveSelfDestructed bool

	// DryRun if true means no state mutations are performed; only statistics
	// are collected.
	DryRun bool
}

// DefaultTechDebtConfig returns a config that cleans all types of cruft.
func DefaultTechDebtConfig() *TechDebtConfig {
	return &TechDebtConfig{
		RemoveEmptyAccounts:    true,
		RemoveZeroStorageSlots: true,
		RemoveSelfDestructed:   true,
		DryRun:                 false,
	}
}

// ResetStats records the outcome of a tech debt reset operation.
type ResetStats struct {
	EntriesRemoved    int
	BytesFreed        uint64
	ContractsMigrated int
	SlotsRemoved      int
	AccountsRemoved   int
	DryRun            bool
}

// StorageLayout describes the mapping of storage slot keys for a contract,
// used to migrate from one layout to another.
type StorageLayout struct {
	Slots []types.Hash
}

// TechDebtResetter performs state cleanup operations on a MemoryStateDB.
// Thread-safe for concurrent read access; writes are serialised.
type TechDebtResetter struct {
	mu     sync.Mutex
	config *TechDebtConfig
	stats  ResetStats
}

// NewTechDebtResetter creates a resetter with the given config.
func NewTechDebtResetter(config *TechDebtConfig) *TechDebtResetter {
	if config == nil {
		config = DefaultTechDebtConfig()
	}
	return &TechDebtResetter{
		config: config,
		stats:  ResetStats{DryRun: config.DryRun},
	}
}

// Stats returns a copy of the current reset statistics.
func (r *TechDebtResetter) Stats() ResetStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

// ResetLegacyStorage scans the state and removes stale entries according
// to the configured policy. Returns the number of cleaned entries and
// the new state root. In dry-run mode, the state is not mutated.
func (r *TechDebtResetter) ResetLegacyStorage(state *MemoryStateDB) (int, types.Hash, error) {
	if state == nil {
		return 0, types.Hash{}, ErrTDRNilState
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	cleaned := 0

	// Collect addresses to remove (cannot delete during iteration).
	var toRemove []types.Address

	for addr, obj := range state.stateObjects {
		if r.config.RemoveSelfDestructed && obj.selfDestructed {
			toRemove = append(toRemove, addr)
			cleaned++
			r.stats.AccountsRemoved++
			r.stats.BytesFreed += estimateAccountSize(obj)
			continue
		}

		if r.config.RemoveEmptyAccounts && isAccountEmptyTD(obj) {
			toRemove = append(toRemove, addr)
			cleaned++
			r.stats.AccountsRemoved++
			r.stats.BytesFreed += estimateAccountSize(obj)
			continue
		}

		if r.config.RemoveZeroStorageSlots {
			slotsRemoved := removeZeroSlots(obj, r.config.DryRun)
			cleaned += slotsRemoved
			r.stats.SlotsRemoved += slotsRemoved
			r.stats.BytesFreed += uint64(slotsRemoved) * 64 // 32-byte key + 32-byte value
		}
	}

	if !r.config.DryRun {
		for _, addr := range toRemove {
			delete(state.stateObjects, addr)
		}
	}

	r.stats.EntriesRemoved += cleaned

	newRoot := state.GetRoot()
	return cleaned, newRoot, nil
}

// RemoveDeadCode removes contracts from the state that have no code, no
// balance, no nonce, and no storage (i.e., they are effectively dead).
// Only the specified contract addresses are checked.
func (r *TechDebtResetter) RemoveDeadCode(state *MemoryStateDB, contracts []types.Address) (int, error) {
	if state == nil {
		return 0, ErrTDRNilState
	}
	if len(contracts) == 0 {
		return 0, ErrTDREmptyContracts
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	removed := 0
	for _, addr := range contracts {
		obj, exists := state.stateObjects[addr]
		if !exists {
			continue
		}

		// A contract is "dead" if it has no code, zero balance, zero nonce,
		// and no live storage.
		if len(obj.code) > 0 {
			continue
		}
		if obj.account.Nonce > 0 {
			continue
		}
		if obj.account.Balance != nil && obj.account.Balance.Sign() > 0 {
			continue
		}
		if len(obj.committedStorage) > 0 || len(obj.dirtyStorage) > 0 {
			continue
		}

		if !r.config.DryRun {
			delete(state.stateObjects, addr)
		}
		removed++
		r.stats.BytesFreed += estimateAccountSize(obj)
	}

	r.stats.EntriesRemoved += removed
	r.stats.AccountsRemoved += removed
	return removed, nil
}

// MigrateStorageLayout migrates storage slots from one layout to another
// for a given contract. The old layout's slot values are copied to the
// corresponding new layout slots (by index), then the old slots are zeroed.
func (r *TechDebtResetter) MigrateStorageLayout(state *MemoryStateDB, contract types.Address, oldLayout, newLayout *StorageLayout) error {
	if state == nil {
		return ErrTDRNilState
	}
	if oldLayout == nil || newLayout == nil {
		return ErrTDRNilConfig
	}
	if len(oldLayout.Slots) != len(newLayout.Slots) {
		return ErrTDRLayoutMismatch
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	obj, exists := state.stateObjects[contract]
	if !exists {
		return ErrTDRContractNotFound
	}

	if r.config.DryRun {
		r.stats.ContractsMigrated++
		return nil
	}

	for i, oldSlot := range oldLayout.Slots {
		newSlot := newLayout.Slots[i]

		// Read value from old slot (check dirty first, then committed).
		val := types.Hash{}
		if v, ok := obj.dirtyStorage[oldSlot]; ok {
			val = v
		} else if v, ok := obj.committedStorage[oldSlot]; ok {
			val = v
		}

		if val == (types.Hash{}) {
			continue
		}

		// Write value to new slot.
		obj.dirtyStorage[newSlot] = val

		// Zero the old slot.
		obj.dirtyStorage[oldSlot] = types.Hash{}
	}

	r.stats.ContractsMigrated++
	return nil
}

// CompactState estimates the size before and after removing empty accounts
// and zero-valued storage slots. This is equivalent to a dry-run reset
// that reports size metrics.
func (r *TechDebtResetter) CompactState(state *MemoryStateDB) (beforeSize, afterSize uint64, err error) {
	if state == nil {
		return 0, 0, ErrTDRNilState
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var before, after uint64

	for _, obj := range state.stateObjects {
		accountSize := estimateAccountSize(obj)
		before += accountSize

		if r.config.RemoveSelfDestructed && obj.selfDestructed {
			continue // excluded from after
		}
		if r.config.RemoveEmptyAccounts && isAccountEmptyTD(obj) {
			continue
		}

		// Base account size without zero-valued storage.
		after += estimateBaseAccountSize(obj)

		// Count non-zero storage slots.
		if r.config.RemoveZeroStorageSlots {
			for _, v := range obj.committedStorage {
				if v != (types.Hash{}) {
					after += 64
				}
			}
			for k, v := range obj.dirtyStorage {
				if v != (types.Hash{}) {
					// Only count if not already counted from committed.
					if _, inCommitted := obj.committedStorage[k]; !inCommitted {
						after += 64
					}
				}
			}
		} else {
			after += uint64(len(obj.committedStorage)+len(obj.dirtyStorage)) * 64
		}
	}

	return before, after, nil
}

// ResetAndCommit performs a full reset followed by a state commit, returning
// the stats and new root hash. Convenience wrapper combining ResetLegacyStorage
// and Commit.
func (r *TechDebtResetter) ResetAndCommit(state *MemoryStateDB) (ResetStats, types.Hash, error) {
	if state == nil {
		return ResetStats{}, types.Hash{}, ErrTDRNilState
	}

	_, _, err := r.ResetLegacyStorage(state)
	if err != nil {
		return r.Stats(), types.Hash{}, err
	}

	if r.config.DryRun {
		root := state.GetRoot()
		return r.Stats(), root, nil
	}

	root, err := state.Commit()
	if err != nil {
		return r.Stats(), types.Hash{}, err
	}

	return r.Stats(), root, nil
}

// --- Internal helpers ---

// isAccountEmptyTD returns true if the account has zero balance, zero nonce,
// and the empty code hash. Wraps isEmptyAccount with a nil check.
func isAccountEmptyTD(obj *stateObject) bool {
	if obj == nil {
		return true
	}
	return isEmptyAccount(obj)
}

// removeZeroSlots removes storage slots with zero values from an account.
// Returns the number of slots removed.
func removeZeroSlots(obj *stateObject, dryRun bool) int {
	removed := 0

	// Check committed storage.
	for k, v := range obj.committedStorage {
		if v == (types.Hash{}) {
			if !dryRun {
				delete(obj.committedStorage, k)
			}
			removed++
		}
	}

	// Check dirty storage.
	for k, v := range obj.dirtyStorage {
		if v == (types.Hash{}) {
			if !dryRun {
				delete(obj.dirtyStorage, k)
			}
			removed++
		}
	}

	return removed
}

// estimateAccountSize returns a rough byte-size estimate for an account
// including all storage.
func estimateAccountSize(obj *stateObject) uint64 {
	if obj == nil {
		return 0
	}
	size := estimateBaseAccountSize(obj)
	size += uint64(len(obj.committedStorage)) * 64
	size += uint64(len(obj.dirtyStorage)) * 64
	return size
}

// estimateBaseAccountSize returns the byte-size estimate for the account
// structure without storage.
func estimateBaseAccountSize(obj *stateObject) uint64 {
	if obj == nil {
		return 0
	}
	// nonce(8) + balance(32) + codeHash(32) + root(32) + code
	base := uint64(8 + 32 + 32 + 32)
	base += uint64(len(obj.code))
	return base
}

// computeStorageHash returns the keccak256 hash of all non-zero storage
// slot values for an account. Used to produce a compact fingerprint.
func computeStorageHash(obj *stateObject) types.Hash {
	if obj == nil {
		return types.Hash{}
	}
	merged := mergeStorage(obj)
	if len(merged) == 0 {
		return types.Hash{}
	}
	var buf []byte
	for k, v := range merged {
		buf = append(buf, k[:]...)
		buf = append(buf, v[:]...)
	}
	return crypto.Keccak256Hash(buf)
}

// NewDryRunConfig returns a config that collects stats without mutations.
func NewDryRunConfig() *TechDebtConfig {
	cfg := DefaultTechDebtConfig()
	cfg.DryRun = true
	return cfg
}

// StateSize computes the estimated size of the entire state in bytes.
func StateSize(state *MemoryStateDB) uint64 {
	if state == nil {
		return 0
	}
	var total uint64
	for _, obj := range state.stateObjects {
		total += estimateAccountSize(obj)
	}
	return total
}

// DirtyAccountCount returns the number of accounts with dirty storage.
func DirtyAccountCount(state *MemoryStateDB) int {
	if state == nil {
		return 0
	}
	count := 0
	for _, obj := range state.stateObjects {
		if len(obj.dirtyStorage) > 0 {
			count++
		}
	}
	return count
}

// ZeroBalanceCount returns the number of accounts with zero balance.
func ZeroBalanceCount(state *MemoryStateDB) int {
	if state == nil {
		return 0
	}
	count := 0
	for _, obj := range state.stateObjects {
		if obj.account.Balance == nil || obj.account.Balance.Sign() == 0 {
			count++
		}
	}
	return count
}

// populateTestState creates a MemoryStateDB with synthetic accounts
// for testing purposes. Exported for use by the test file.
func populateTestState(numAccounts, slotsPerAccount int) *MemoryStateDB {
	state := NewMemoryStateDB()
	for i := 0; i < numAccounts; i++ {
		var addr types.Address
		addr[0] = byte(i / 256)
		addr[1] = byte(i % 256)
		addr[19] = byte(i + 1)

		state.CreateAccount(addr)
		state.AddBalance(addr, big.NewInt(int64((i+1)*1000)))
		state.SetNonce(addr, uint64(i))

		for s := 0; s < slotsPerAccount; s++ {
			var key, val types.Hash
			key[0] = byte(s)
			key[31] = byte(i)
			val[0] = byte(s + 1)
			state.SetState(addr, key, val)
		}
	}
	return state
}
