package vm

// access_list_tracker.go implements EIP-2929 warm/cold access tracking with
// journaling support for state reverts. It provides a standalone tracker that
// manages address and storage slot warm sets, computes gas costs based on
// warm/cold status, and supports pre-population from a transaction access list.

import (
	"github.com/eth2030/eth2030/core/types"
)

// AccessListTracker manages EIP-2929 warm/cold access tracking for addresses
// and storage slots during EVM execution. It maintains a warm set and provides
// journaling for revert support via snapshots.
type AccessListTracker struct {
	addresses   map[types.Address]int                // address -> journal index of insertion (-1 if pre-populated)
	slots       map[types.Address]map[types.Hash]int // address -> slot -> journal index
	journal     []accessListChange                   // ordered changes for revert
	snapshotIDs []int                                // journal length at each snapshot
}

// accessListChangeKind identifies the type of change recorded in the journal.
type accessListChangeKind uint8

const (
	changeAddAddress accessListChangeKind = iota
	changeAddSlot
)

// accessListChange records a single modification to the access list for
// journal-based revert.
type accessListChange struct {
	kind    accessListChangeKind
	address types.Address
	slot    types.Hash // only used for changeAddSlot
}

// NewAccessListTracker creates an empty AccessListTracker.
func NewAccessListTracker() *AccessListTracker {
	return &AccessListTracker{
		addresses: make(map[types.Address]int),
		slots:     make(map[types.Address]map[types.Hash]int),
	}
}

// PrePopulate warms the sender, recipient, all precompile addresses (0x01-0x13),
// and the entries from the transaction's access list per EIP-2929. Pre-populated
// entries use journal index -1 so they survive all reverts.
func (alt *AccessListTracker) PrePopulate(
	sender types.Address,
	to *types.Address,
	accessList types.AccessList,
) {
	// Warm the sender.
	alt.addAddressNoJournal(sender)

	// Warm the recipient (if not a contract creation).
	if to != nil {
		alt.addAddressNoJournal(*to)
	}

	// Warm all precompile addresses (0x01 through 0x13 = 19 addresses).
	for i := 1; i <= 0x13; i++ {
		alt.addAddressNoJournal(types.BytesToAddress([]byte{byte(i)}))
	}

	// Warm entries from the transaction access list.
	for _, tuple := range accessList {
		alt.addAddressNoJournal(tuple.Address)
		for _, key := range tuple.StorageKeys {
			alt.addSlotNoJournal(tuple.Address, key)
		}
	}
}

// addAddressNoJournal adds an address to the warm set without journaling.
// Used during pre-population to create entries that persist across reverts.
func (alt *AccessListTracker) addAddressNoJournal(addr types.Address) {
	if _, ok := alt.addresses[addr]; !ok {
		alt.addresses[addr] = -1
	}
}

// addSlotNoJournal adds a storage slot to the warm set without journaling.
func (alt *AccessListTracker) addSlotNoJournal(addr types.Address, slot types.Hash) {
	// Ensure the address is also tracked.
	if _, ok := alt.addresses[addr]; !ok {
		alt.addresses[addr] = -1
	}
	slots, ok := alt.slots[addr]
	if !ok {
		slots = make(map[types.Hash]int)
		alt.slots[addr] = slots
	}
	if _, ok := slots[slot]; !ok {
		slots[slot] = -1
	}
}

// ContainsAddress returns true if the address is in the warm set.
func (alt *AccessListTracker) ContainsAddress(addr types.Address) bool {
	_, ok := alt.addresses[addr]
	return ok
}

// ContainsSlot returns (addressWarm, slotWarm) indicating whether the address
// and the specific storage slot are in the warm set.
func (alt *AccessListTracker) ContainsSlot(addr types.Address, slot types.Hash) (bool, bool) {
	_, addrOk := alt.addresses[addr]
	if !addrOk {
		return false, false
	}
	slots, ok := alt.slots[addr]
	if !ok {
		return true, false
	}
	_, slotOk := slots[slot]
	return true, slotOk
}

// TouchAddress warms an address if cold. Returns true if the address was
// already warm (no state change), false if it was cold (now warmed).
func (alt *AccessListTracker) TouchAddress(addr types.Address) bool {
	if _, ok := alt.addresses[addr]; ok {
		return true // already warm
	}
	idx := len(alt.journal)
	alt.addresses[addr] = idx
	alt.journal = append(alt.journal, accessListChange{
		kind:    changeAddAddress,
		address: addr,
	})
	return false // was cold
}

// TouchSlot warms a storage slot if cold. Returns (addressWarm, slotWarm)
// reflecting the state before this call.
func (alt *AccessListTracker) TouchSlot(addr types.Address, slot types.Hash) (bool, bool) {
	addrWarm := alt.TouchAddress(addr)

	slots, ok := alt.slots[addr]
	if !ok {
		slots = make(map[types.Hash]int)
		alt.slots[addr] = slots
	}

	if _, slotOk := slots[slot]; slotOk {
		return addrWarm, true // slot already warm
	}

	idx := len(alt.journal)
	slots[slot] = idx
	alt.journal = append(alt.journal, accessListChange{
		kind:    changeAddSlot,
		address: addr,
		slot:    slot,
	})
	return addrWarm, false // slot was cold
}

// Snapshot takes a snapshot of the current journal state. Returns a snapshot
// ID that can be used with RevertToSnapshot.
func (alt *AccessListTracker) Snapshot() int {
	id := len(alt.snapshotIDs)
	alt.snapshotIDs = append(alt.snapshotIDs, len(alt.journal))
	return id
}

// RevertToSnapshot reverts all access list changes made after the given
// snapshot. Pre-populated entries (journal index -1) are never reverted.
func (alt *AccessListTracker) RevertToSnapshot(id int) {
	if id < 0 || id >= len(alt.snapshotIDs) {
		return
	}
	journalLen := alt.snapshotIDs[id]

	// Walk journal backwards, undoing each change.
	for i := len(alt.journal) - 1; i >= journalLen; i-- {
		change := alt.journal[i]
		switch change.kind {
		case changeAddSlot:
			slots := alt.slots[change.address]
			if slots != nil {
				if idx, ok := slots[change.slot]; ok && idx >= journalLen {
					delete(slots, change.slot)
				}
			}
		case changeAddAddress:
			if idx, ok := alt.addresses[change.address]; ok && idx >= journalLen {
				delete(alt.addresses, change.address)
			}
		}
	}

	// Truncate journal and snapshots.
	alt.journal = alt.journal[:journalLen]
	alt.snapshotIDs = alt.snapshotIDs[:id]
}

// AddressGasCost returns the gas cost for accessing an address, warming it
// if cold. For EIP-2929: cold=2600, warm=0 (the warm cost is the opcode's
// constant gas, typically WarmStorageReadCost=100).
func (alt *AccessListTracker) AddressGasCost(addr types.Address) uint64 {
	if alt.TouchAddress(addr) {
		return 0 // already warm, no extra gas
	}
	// Cold access: return (ColdAccountAccessCost - WarmStorageReadCost)
	// because the opcode's constant gas already covers WarmStorageReadCost.
	return ColdAccountAccessCost - WarmStorageReadCost
}

// SlotGasCost returns the gas cost for accessing a storage slot, warming it
// if cold. For EIP-2929: cold=2100, warm=0 (the warm cost is the opcode's
// constant gas, WarmStorageReadCost=100).
func (alt *AccessListTracker) SlotGasCost(addr types.Address, slot types.Hash) uint64 {
	_, slotWarm := alt.TouchSlot(addr, slot)
	if slotWarm {
		return 0 // already warm, no extra gas
	}
	// Cold access: return (ColdSloadCost - WarmStorageReadCost).
	return ColdSloadCost - WarmStorageReadCost
}

// Copy returns a deep copy of the tracker. The copy shares no mutable state
// with the original, making it safe for speculative execution paths.
func (alt *AccessListTracker) Copy() *AccessListTracker {
	cpy := &AccessListTracker{
		addresses: make(map[types.Address]int, len(alt.addresses)),
		slots:     make(map[types.Address]map[types.Hash]int, len(alt.slots)),
	}
	for addr, idx := range alt.addresses {
		cpy.addresses[addr] = idx
	}
	for addr, slots := range alt.slots {
		slotCopy := make(map[types.Hash]int, len(slots))
		for slot, idx := range slots {
			slotCopy[slot] = idx
		}
		cpy.slots[addr] = slotCopy
	}
	// Copy journal.
	if len(alt.journal) > 0 {
		cpy.journal = make([]accessListChange, len(alt.journal))
		copy(cpy.journal, alt.journal)
	}
	// Copy snapshot IDs.
	if len(alt.snapshotIDs) > 0 {
		cpy.snapshotIDs = make([]int, len(alt.snapshotIDs))
		copy(cpy.snapshotIDs, alt.snapshotIDs)
	}
	return cpy
}

// WarmAddresses returns a list of all addresses currently in the warm set.
func (alt *AccessListTracker) WarmAddresses() []types.Address {
	addrs := make([]types.Address, 0, len(alt.addresses))
	for addr := range alt.addresses {
		addrs = append(addrs, addr)
	}
	return addrs
}

// WarmSlots returns all warm storage slots for a given address.
func (alt *AccessListTracker) WarmSlots(addr types.Address) []types.Hash {
	slots, ok := alt.slots[addr]
	if !ok {
		return nil
	}
	result := make([]types.Hash, 0, len(slots))
	for slot := range slots {
		result = append(result, slot)
	}
	return result
}

// Reset clears the tracker, removing all warm entries and journal state.
func (alt *AccessListTracker) Reset() {
	alt.addresses = make(map[types.Address]int)
	alt.slots = make(map[types.Address]map[types.Hash]int)
	alt.journal = alt.journal[:0]
	alt.snapshotIDs = alt.snapshotIDs[:0]
}
