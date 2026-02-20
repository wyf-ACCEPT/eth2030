package state

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func alAddr(b byte) types.Address {
	var a types.Address
	a[0] = b
	return a
}

func alHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

// --- AddAddress ---

func TestAccessList_AddAddress(t *testing.T) {
	al := newAccessList()
	addr := alAddr(1)

	// First add returns false (not previously present).
	if al.AddAddress(addr) {
		t.Fatal("expected false for first AddAddress")
	}

	// Second add returns true (already present).
	if !al.AddAddress(addr) {
		t.Fatal("expected true for duplicate AddAddress")
	}
}

func TestAccessList_ContainsAddress(t *testing.T) {
	al := newAccessList()
	addr := alAddr(2)

	if al.ContainsAddress(addr) {
		t.Fatal("address should not be present initially")
	}

	al.AddAddress(addr)
	if !al.ContainsAddress(addr) {
		t.Fatal("address should be present after adding")
	}
}

// --- AddSlot ---

func TestAccessList_AddSlot_NewAddress(t *testing.T) {
	al := newAccessList()
	addr := alAddr(3)
	slot := alHash(1)

	addrPresent, slotPresent := al.AddSlot(addr, slot)
	if addrPresent {
		t.Fatal("address should not be present initially")
	}
	if slotPresent {
		t.Fatal("slot should not be present initially")
	}

	// Address should now be present.
	if !al.ContainsAddress(addr) {
		t.Fatal("address should be present after AddSlot")
	}
}

func TestAccessList_AddSlot_ExistingAddressNoSlots(t *testing.T) {
	al := newAccessList()
	addr := alAddr(4)
	slot := alHash(2)

	// Add address without any slots first.
	al.AddAddress(addr)

	addrPresent, slotPresent := al.AddSlot(addr, slot)
	if !addrPresent {
		t.Fatal("address should already be present")
	}
	if slotPresent {
		t.Fatal("slot should not be present yet")
	}
}

func TestAccessList_AddSlot_ExistingAddressWithSlots(t *testing.T) {
	al := newAccessList()
	addr := alAddr(5)
	slot1 := alHash(3)
	slot2 := alHash(4)

	al.AddSlot(addr, slot1)

	// Add a different slot.
	addrPresent, slotPresent := al.AddSlot(addr, slot2)
	if !addrPresent {
		t.Fatal("address should be present")
	}
	if slotPresent {
		t.Fatal("slot2 should not be present yet")
	}

	// Add the same slot again.
	addrPresent, slotPresent = al.AddSlot(addr, slot2)
	if !addrPresent {
		t.Fatal("address should be present")
	}
	if !slotPresent {
		t.Fatal("slot2 should already be present")
	}
}

func TestAccessList_AddSlot_DuplicateSlot(t *testing.T) {
	al := newAccessList()
	addr := alAddr(6)
	slot := alHash(5)

	al.AddSlot(addr, slot)
	addrPresent, slotPresent := al.AddSlot(addr, slot)
	if !addrPresent {
		t.Fatal("address should be present")
	}
	if !slotPresent {
		t.Fatal("slot should already be present")
	}
}

// --- ContainsSlot ---

func TestAccessList_ContainsSlot(t *testing.T) {
	al := newAccessList()
	addr := alAddr(7)
	slot := alHash(6)

	// Nothing present.
	addrOk, slotOk := al.ContainsSlot(addr, slot)
	if addrOk || slotOk {
		t.Fatal("neither address nor slot should be present initially")
	}

	// Add address only.
	al.AddAddress(addr)
	addrOk, slotOk = al.ContainsSlot(addr, slot)
	if !addrOk {
		t.Fatal("address should be present")
	}
	if slotOk {
		t.Fatal("slot should not be present")
	}

	// Add slot.
	al.AddSlot(addr, slot)
	addrOk, slotOk = al.ContainsSlot(addr, slot)
	if !addrOk || !slotOk {
		t.Fatal("both address and slot should be present")
	}
}

// --- DeleteAddress ---

func TestAccessList_DeleteAddress(t *testing.T) {
	al := newAccessList()
	addr := alAddr(8)

	al.AddAddress(addr)
	al.DeleteAddress(addr)

	if al.ContainsAddress(addr) {
		t.Fatal("address should be removed after delete")
	}
}

func TestAccessList_DeleteAddress_NonExistent(t *testing.T) {
	al := newAccessList()
	// Should not panic.
	al.DeleteAddress(alAddr(99))
}

// --- DeleteSlot ---

func TestAccessList_DeleteSlot(t *testing.T) {
	al := newAccessList()
	addr := alAddr(9)
	slot := alHash(7)

	al.AddSlot(addr, slot)
	al.DeleteSlot(addr, slot)

	addrOk, slotOk := al.ContainsSlot(addr, slot)
	if !addrOk {
		t.Fatal("address should still be present after deleting slot")
	}
	if slotOk {
		t.Fatal("slot should be removed after delete")
	}
}

func TestAccessList_DeleteSlot_NonExistent(t *testing.T) {
	al := newAccessList()
	// Should not panic.
	al.DeleteSlot(alAddr(99), alHash(99))
}

func TestAccessList_DeleteSlot_AddressNoSlots(t *testing.T) {
	al := newAccessList()
	addr := alAddr(10)

	al.AddAddress(addr) // address with idx == -1
	// Should not panic.
	al.DeleteSlot(addr, alHash(10))
}

// --- Copy ---

func TestAccessList_Copy(t *testing.T) {
	al := newAccessList()
	addr := alAddr(11)
	slot := alHash(8)

	al.AddSlot(addr, slot)
	cp := al.Copy()

	// Copy should contain same entries.
	if !cp.ContainsAddress(addr) {
		t.Fatal("copy should contain address")
	}
	addrOk, slotOk := cp.ContainsSlot(addr, slot)
	if !addrOk || !slotOk {
		t.Fatal("copy should contain slot")
	}

	// Mutating original should not affect copy.
	newSlot := alHash(9)
	al.AddSlot(addr, newSlot)
	_, slotOk = cp.ContainsSlot(addr, newSlot)
	if slotOk {
		t.Fatal("mutation of original should not affect copy")
	}

	// Mutating copy should not affect original.
	addr2 := alAddr(12)
	cp.AddAddress(addr2)
	if al.ContainsAddress(addr2) {
		t.Fatal("mutation of copy should not affect original")
	}
}

func TestAccessList_CopyEmpty(t *testing.T) {
	al := newAccessList()
	cp := al.Copy()
	if cp == nil {
		t.Fatal("copy should not be nil")
	}
	if len(cp.addresses) != 0 {
		t.Fatal("copy should have no addresses")
	}
}

// --- Multiple addresses ---

func TestAccessList_MultipleAddresses(t *testing.T) {
	al := newAccessList()
	addr1 := alAddr(20)
	addr2 := alAddr(21)
	slot1 := alHash(30)
	slot2 := alHash(31)

	al.AddSlot(addr1, slot1)
	al.AddSlot(addr2, slot2)

	// Each address should only contain its own slot.
	_, ok1 := al.ContainsSlot(addr1, slot1)
	if !ok1 {
		t.Fatal("addr1 should contain slot1")
	}
	_, ok2 := al.ContainsSlot(addr1, slot2)
	if ok2 {
		t.Fatal("addr1 should not contain slot2")
	}
	_, ok3 := al.ContainsSlot(addr2, slot2)
	if !ok3 {
		t.Fatal("addr2 should contain slot2")
	}
	_, ok4 := al.ContainsSlot(addr2, slot1)
	if ok4 {
		t.Fatal("addr2 should not contain slot1")
	}
}
