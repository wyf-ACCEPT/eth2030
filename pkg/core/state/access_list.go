package state

import "github.com/eth2028/eth2028/core/types"

// accessList tracks warm addresses and storage slots per EIP-2929.
type accessList struct {
	addresses map[types.Address]int              // address -> index into slots, or -1 if no slots
	slots     []map[types.Hash]struct{}           // slot sets indexed by address entry
}

func newAccessList() *accessList {
	return &accessList{
		addresses: make(map[types.Address]int),
	}
}

// AddAddress adds an address to the access list. Returns true if the address
// was already present.
func (al *accessList) AddAddress(addr types.Address) bool {
	if _, ok := al.addresses[addr]; ok {
		return true
	}
	al.addresses[addr] = -1
	return false
}

// AddSlot adds a (address, slot) pair to the access list. Returns whether
// the address and slot were already present.
func (al *accessList) AddSlot(addr types.Address, slot types.Hash) (addrPresent bool, slotPresent bool) {
	idx, addrPresent := al.addresses[addr]
	if addrPresent && idx != -1 {
		// Address exists and has slot storage — check if this slot is present.
		if _, ok := al.slots[idx][slot]; ok {
			return true, true
		}
		al.slots[idx][slot] = struct{}{}
		return true, false
	}
	if !addrPresent {
		// New address entirely.
		al.addresses[addr] = len(al.slots)
		al.slots = append(al.slots, map[types.Hash]struct{}{slot: {}})
		return false, false
	}
	// Address existed but had no slots yet (idx == -1).
	al.addresses[addr] = len(al.slots)
	al.slots = append(al.slots, map[types.Hash]struct{}{slot: {}})
	return true, false
}

// ContainsAddress returns whether the address is in the access list.
func (al *accessList) ContainsAddress(addr types.Address) bool {
	_, ok := al.addresses[addr]
	return ok
}

// ContainsSlot returns whether the address and slot are in the access list.
func (al *accessList) ContainsSlot(addr types.Address, slot types.Hash) (addressOk bool, slotOk bool) {
	idx, ok := al.addresses[addr]
	if !ok {
		return false, false
	}
	if idx == -1 {
		return true, false
	}
	_, slotOk = al.slots[idx][slot]
	return true, slotOk
}

// Copy returns a deep copy of the access list.
func (al *accessList) Copy() *accessList {
	cp := &accessList{
		addresses: make(map[types.Address]int, len(al.addresses)),
		slots:     make([]map[types.Hash]struct{}, len(al.slots)),
	}
	for k, v := range al.addresses {
		cp.addresses[k] = v
	}
	for i, slotMap := range al.slots {
		cp.slots[i] = make(map[types.Hash]struct{}, len(slotMap))
		for k := range slotMap {
			cp.slots[i][k] = struct{}{}
		}
	}
	return cp
}

// DeleteAddress removes an address from the access list. Used during revert.
func (al *accessList) DeleteAddress(addr types.Address) {
	delete(al.addresses, addr)
}

// DeleteSlot removes a slot from an address in the access list. Used during revert.
func (al *accessList) DeleteSlot(addr types.Address, slot types.Hash) {
	idx, ok := al.addresses[addr]
	if !ok || idx == -1 {
		return
	}
	delete(al.slots[idx], slot)
}
