package bal

import (
	"math/big"
	"sort"

	"github.com/eth2030/eth2030/core/types"
)

// addrKey is used as a map key for addresses.
type addrKey = types.Address

// slotKey uniquely identifies an (address, slot) pair.
type slotKey struct {
	Addr types.Address
	Slot types.Hash
}

// AccessTracker records state accesses during block execution and
// produces a BlockAccessList from the recorded data.
type AccessTracker struct {
	reads          map[slotKey]types.Hash    // slot -> value read
	changes        map[slotKey][2]types.Hash // slot -> [old, new]
	balanceChanges map[addrKey]*BalanceChange
	nonceChanges   map[addrKey]*NonceChange
	codeChanges    map[addrKey]*CodeChange
	touchedAddrs   map[addrKey]struct{}
}

// NewTracker creates a new AccessTracker with initialized maps.
func NewTracker() *AccessTracker {
	return &AccessTracker{
		reads:          make(map[slotKey]types.Hash),
		changes:        make(map[slotKey][2]types.Hash),
		balanceChanges: make(map[addrKey]*BalanceChange),
		nonceChanges:   make(map[addrKey]*NonceChange),
		codeChanges:    make(map[addrKey]*CodeChange),
		touchedAddrs:   make(map[addrKey]struct{}),
	}
}

// RecordStorageRead records a storage slot read.
func (t *AccessTracker) RecordStorageRead(addr types.Address, slot types.Hash, value types.Hash) {
	key := slotKey{Addr: addr, Slot: slot}
	t.reads[key] = value
	t.touchedAddrs[addr] = struct{}{}
}

// RecordStorageChange records a storage slot modification.
func (t *AccessTracker) RecordStorageChange(addr types.Address, slot, oldVal, newVal types.Hash) {
	key := slotKey{Addr: addr, Slot: slot}
	t.changes[key] = [2]types.Hash{oldVal, newVal}
	t.touchedAddrs[addr] = struct{}{}
}

// RecordBalanceChange records a balance modification.
func (t *AccessTracker) RecordBalanceChange(addr types.Address, oldBal, newBal *big.Int) {
	t.balanceChanges[addr] = &BalanceChange{
		OldValue: new(big.Int).Set(oldBal),
		NewValue: new(big.Int).Set(newBal),
	}
	t.touchedAddrs[addr] = struct{}{}
}

// RecordNonceChange records a nonce modification.
func (t *AccessTracker) RecordNonceChange(addr types.Address, oldNonce, newNonce uint64) {
	t.nonceChanges[addr] = &NonceChange{OldValue: oldNonce, NewValue: newNonce}
	t.touchedAddrs[addr] = struct{}{}
}

// RecordCodeChange records a code modification.
func (t *AccessTracker) RecordCodeChange(addr types.Address, oldCode, newCode []byte) {
	cc := &CodeChange{}
	if oldCode != nil {
		cc.OldCode = make([]byte, len(oldCode))
		copy(cc.OldCode, oldCode)
	}
	if newCode != nil {
		cc.NewCode = make([]byte, len(newCode))
		copy(cc.NewCode, newCode)
	}
	t.codeChanges[addr] = cc
	t.touchedAddrs[addr] = struct{}{}
}

// Build produces a BlockAccessList from the recorded accesses, tagging
// all entries with the given txIndex.
func (t *AccessTracker) Build(txIndex uint64) *BlockAccessList {
	bal := NewBlockAccessList()

	// Sort addresses for deterministic output.
	addrs := make([]types.Address, 0, len(t.touchedAddrs))
	for addr := range t.touchedAddrs {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return addrLess(addrs[i], addrs[j])
	})

	for _, addr := range addrs {
		entry := AccessEntry{
			Address:     addr,
			AccessIndex: txIndex,
		}

		// Collect storage reads for this address.
		for key, val := range t.reads {
			if key.Addr == addr {
				entry.StorageReads = append(entry.StorageReads, StorageAccess{
					Slot:  key.Slot,
					Value: val,
				})
			}
		}
		sort.Slice(entry.StorageReads, func(i, j int) bool {
			return hashLess(entry.StorageReads[i].Slot, entry.StorageReads[j].Slot)
		})

		// Collect storage changes for this address.
		for key, vals := range t.changes {
			if key.Addr == addr {
				entry.StorageChanges = append(entry.StorageChanges, StorageChange{
					Slot:     key.Slot,
					OldValue: vals[0],
					NewValue: vals[1],
				})
			}
		}
		sort.Slice(entry.StorageChanges, func(i, j int) bool {
			return hashLess(entry.StorageChanges[i].Slot, entry.StorageChanges[j].Slot)
		})

		if bc, ok := t.balanceChanges[addr]; ok {
			entry.BalanceChange = bc
		}
		if nc, ok := t.nonceChanges[addr]; ok {
			entry.NonceChange = nc
		}
		if cc, ok := t.codeChanges[addr]; ok {
			entry.CodeChange = cc
		}

		bal.AddEntry(entry)
	}

	return bal
}

// Reset clears all recorded accesses so the tracker can be reused.
func (t *AccessTracker) Reset() {
	t.reads = make(map[slotKey]types.Hash)
	t.changes = make(map[slotKey][2]types.Hash)
	t.balanceChanges = make(map[addrKey]*BalanceChange)
	t.nonceChanges = make(map[addrKey]*NonceChange)
	t.codeChanges = make(map[addrKey]*CodeChange)
	t.touchedAddrs = make(map[addrKey]struct{})
}

// addrLess compares two addresses lexicographically.
func addrLess(a, b types.Address) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// hashLess compares two hashes lexicographically.
func hashLess(a, b types.Hash) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
