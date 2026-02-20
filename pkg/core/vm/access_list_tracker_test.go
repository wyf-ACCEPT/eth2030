package vm

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewAccessListTracker(t *testing.T) {
	alt := NewAccessListTracker()
	if alt == nil {
		t.Fatal("NewAccessListTracker returned nil")
	}
	if len(alt.WarmAddresses()) != 0 {
		t.Errorf("new tracker should have 0 warm addresses, got %d", len(alt.WarmAddresses()))
	}
}

func TestAccessListTracker_TouchAddress(t *testing.T) {
	alt := NewAccessListTracker()
	addr := types.HexToAddress("0xdeadbeef")

	// First touch: cold -> warm, returns false (was cold).
	warm := alt.TouchAddress(addr)
	if warm {
		t.Error("expected false (cold) on first touch")
	}
	if !alt.ContainsAddress(addr) {
		t.Error("address should be warm after touch")
	}

	// Second touch: already warm, returns true.
	warm = alt.TouchAddress(addr)
	if !warm {
		t.Error("expected true (warm) on second touch")
	}
}

func TestAccessListTracker_TouchSlot(t *testing.T) {
	alt := NewAccessListTracker()
	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")

	// First touch: both address and slot are cold.
	addrWarm, slotWarm := alt.TouchSlot(addr, slot)
	if addrWarm {
		t.Error("address should have been cold")
	}
	if slotWarm {
		t.Error("slot should have been cold")
	}

	// Second touch: both should be warm now.
	addrWarm, slotWarm = alt.TouchSlot(addr, slot)
	if !addrWarm {
		t.Error("address should be warm on second touch")
	}
	if !slotWarm {
		t.Error("slot should be warm on second touch")
	}

	// New slot for the same address: address warm, slot cold.
	slot2 := types.HexToHash("0x02")
	addrWarm, slotWarm = alt.TouchSlot(addr, slot2)
	if !addrWarm {
		t.Error("address should still be warm")
	}
	if slotWarm {
		t.Error("new slot should be cold")
	}
}

func TestAccessListTracker_ContainsSlot(t *testing.T) {
	alt := NewAccessListTracker()
	addr := types.HexToAddress("0xbbbb")
	slot := types.HexToHash("0x10")

	// Neither address nor slot is warm.
	addrOk, slotOk := alt.ContainsSlot(addr, slot)
	if addrOk || slotOk {
		t.Error("neither should be warm initially")
	}

	// Warm the address only via TouchAddress.
	alt.TouchAddress(addr)
	addrOk, slotOk = alt.ContainsSlot(addr, slot)
	if !addrOk {
		t.Error("address should be warm")
	}
	if slotOk {
		t.Error("slot should still be cold")
	}

	// Warm the slot.
	alt.TouchSlot(addr, slot)
	addrOk, slotOk = alt.ContainsSlot(addr, slot)
	if !addrOk || !slotOk {
		t.Error("both address and slot should be warm")
	}
}

func TestAccessListTracker_AddressGasCost(t *testing.T) {
	alt := NewAccessListTracker()
	addr := types.HexToAddress("0xcccc")

	expectedCold := ColdAccountAccessCost - WarmStorageReadCost
	got := alt.AddressGasCost(addr)
	if got != expectedCold {
		t.Errorf("cold address gas = %d, want %d", got, expectedCold)
	}

	// Second access: warm, no extra gas.
	got = alt.AddressGasCost(addr)
	if got != 0 {
		t.Errorf("warm address gas = %d, want 0", got)
	}
}

func TestAccessListTracker_SlotGasCost(t *testing.T) {
	alt := NewAccessListTracker()
	addr := types.HexToAddress("0xdddd")
	slot := types.HexToHash("0x20")

	expectedCold := ColdSloadCost - WarmStorageReadCost
	got := alt.SlotGasCost(addr, slot)
	if got != expectedCold {
		t.Errorf("cold slot gas = %d, want %d", got, expectedCold)
	}

	// Second access: warm, no extra gas.
	got = alt.SlotGasCost(addr, slot)
	if got != 0 {
		t.Errorf("warm slot gas = %d, want 0", got)
	}
}

func TestAccessListTracker_PrePopulate(t *testing.T) {
	alt := NewAccessListTracker()
	sender := types.HexToAddress("0x01")
	to := types.HexToAddress("0xff")
	slot1 := types.HexToHash("0xaa")
	slot2 := types.HexToHash("0xbb")
	accessAddr := types.HexToAddress("0xee")

	accessList := types.AccessList{
		{Address: accessAddr, StorageKeys: []types.Hash{slot1, slot2}},
	}

	alt.PrePopulate(sender, &to, accessList)

	// Sender should be warm.
	if !alt.ContainsAddress(sender) {
		t.Error("sender should be warm after pre-populate")
	}
	// Recipient should be warm.
	if !alt.ContainsAddress(to) {
		t.Error("recipient should be warm after pre-populate")
	}
	// Precompile addresses 0x01..0x13 should be warm (19 addresses).
	for i := 1; i <= 0x13; i++ {
		pc := types.BytesToAddress([]byte{byte(i)})
		if !alt.ContainsAddress(pc) {
			t.Errorf("precompile %d should be warm", i)
		}
	}
	// Access list address and slots should be warm.
	if !alt.ContainsAddress(accessAddr) {
		t.Error("access list address should be warm")
	}
	_, slotOk := alt.ContainsSlot(accessAddr, slot1)
	if !slotOk {
		t.Error("access list slot1 should be warm")
	}
	_, slotOk = alt.ContainsSlot(accessAddr, slot2)
	if !slotOk {
		t.Error("access list slot2 should be warm")
	}

	// Pre-populated entries should cost 0 extra gas.
	if gas := alt.AddressGasCost(sender); gas != 0 {
		t.Errorf("pre-populated sender gas = %d, want 0", gas)
	}
	if gas := alt.SlotGasCost(accessAddr, slot1); gas != 0 {
		t.Errorf("pre-populated slot gas = %d, want 0", gas)
	}
}

func TestAccessListTracker_PrePopulateNilTo(t *testing.T) {
	alt := NewAccessListTracker()
	sender := types.HexToAddress("0x01")
	alt.PrePopulate(sender, nil, nil)

	if !alt.ContainsAddress(sender) {
		t.Error("sender should be warm")
	}
}

func TestAccessListTracker_SnapshotAndRevert(t *testing.T) {
	alt := NewAccessListTracker()
	addr1 := types.HexToAddress("0xaa")
	addr2 := types.HexToAddress("0xbb")
	slot1 := types.HexToHash("0x01")

	// Warm addr1.
	alt.TouchAddress(addr1)
	snap := alt.Snapshot()

	// After snapshot: warm addr2 and a slot.
	alt.TouchAddress(addr2)
	alt.TouchSlot(addr2, slot1)

	if !alt.ContainsAddress(addr2) {
		t.Error("addr2 should be warm before revert")
	}

	// Revert to snapshot.
	alt.RevertToSnapshot(snap)

	// addr1 should still be warm (added before snapshot).
	if !alt.ContainsAddress(addr1) {
		t.Error("addr1 should survive revert")
	}
	// addr2 should be reverted.
	if alt.ContainsAddress(addr2) {
		t.Error("addr2 should be reverted")
	}
	// slot1 under addr2 should be reverted.
	_, slotOk := alt.ContainsSlot(addr2, slot1)
	if slotOk {
		t.Error("slot should be reverted")
	}
}

func TestAccessListTracker_PrePopulatedSurvivesRevert(t *testing.T) {
	alt := NewAccessListTracker()
	sender := types.HexToAddress("0x01")
	alt.PrePopulate(sender, nil, nil)

	snap := alt.Snapshot()
	newAddr := types.HexToAddress("0x99")
	alt.TouchAddress(newAddr)

	alt.RevertToSnapshot(snap)

	// Pre-populated address survives revert.
	if !alt.ContainsAddress(sender) {
		t.Error("pre-populated sender should survive revert")
	}
	// Newly touched address should be reverted.
	if alt.ContainsAddress(newAddr) {
		t.Error("newAddr should be reverted")
	}
}

func TestAccessListTracker_NestedSnapshots(t *testing.T) {
	alt := NewAccessListTracker()
	addr1 := types.HexToAddress("0x01")
	addr2 := types.HexToAddress("0x02")
	addr3 := types.HexToAddress("0x03")

	alt.TouchAddress(addr1)
	snap1 := alt.Snapshot()

	alt.TouchAddress(addr2)
	snap2 := alt.Snapshot()

	alt.TouchAddress(addr3)

	// Revert to snap2: addr3 reverted, addr1 and addr2 remain.
	alt.RevertToSnapshot(snap2)
	if alt.ContainsAddress(addr3) {
		t.Error("addr3 should be reverted at snap2")
	}
	if !alt.ContainsAddress(addr2) {
		t.Error("addr2 should remain after snap2 revert")
	}

	// Revert to snap1: addr2 also reverted.
	alt.RevertToSnapshot(snap1)
	if alt.ContainsAddress(addr2) {
		t.Error("addr2 should be reverted at snap1")
	}
	if !alt.ContainsAddress(addr1) {
		t.Error("addr1 should remain after snap1 revert")
	}
}

func TestAccessListTracker_InvalidSnapshotRevert(t *testing.T) {
	alt := NewAccessListTracker()
	addr := types.HexToAddress("0xaa")
	alt.TouchAddress(addr)

	// Out-of-bounds snapshot IDs should be no-ops.
	alt.RevertToSnapshot(-1)
	alt.RevertToSnapshot(999)

	if !alt.ContainsAddress(addr) {
		t.Error("address should still be warm after invalid revert")
	}
}

func TestAccessListTracker_Copy(t *testing.T) {
	alt := NewAccessListTracker()
	addr := types.HexToAddress("0xaa")
	slot := types.HexToHash("0x11")
	alt.TouchAddress(addr)
	alt.TouchSlot(addr, slot)
	_ = alt.Snapshot()

	cpy := alt.Copy()

	// Copy should have the same data.
	if !cpy.ContainsAddress(addr) {
		t.Error("copy should contain address")
	}
	_, slotOk := cpy.ContainsSlot(addr, slot)
	if !slotOk {
		t.Error("copy should contain slot")
	}

	// Modifying copy should not affect original.
	newAddr := types.HexToAddress("0xbb")
	cpy.TouchAddress(newAddr)
	if alt.ContainsAddress(newAddr) {
		t.Error("modifying copy should not affect original")
	}
}

func TestAccessListTracker_Reset(t *testing.T) {
	alt := NewAccessListTracker()
	addr := types.HexToAddress("0xaa")
	alt.TouchAddress(addr)
	_ = alt.Snapshot()

	alt.Reset()

	if alt.ContainsAddress(addr) {
		t.Error("address should be cleared after reset")
	}
	if len(alt.WarmAddresses()) != 0 {
		t.Error("warm addresses should be empty after reset")
	}
}

func TestAccessListTracker_WarmAddresses(t *testing.T) {
	alt := NewAccessListTracker()
	a1 := types.HexToAddress("0x01")
	a2 := types.HexToAddress("0x02")
	alt.TouchAddress(a1)
	alt.TouchAddress(a2)

	addrs := alt.WarmAddresses()
	if len(addrs) != 2 {
		t.Fatalf("expected 2 warm addresses, got %d", len(addrs))
	}
}

func TestAccessListTracker_WarmSlots(t *testing.T) {
	alt := NewAccessListTracker()
	addr := types.HexToAddress("0xaa")
	s1 := types.HexToHash("0x01")
	s2 := types.HexToHash("0x02")

	alt.TouchSlot(addr, s1)
	alt.TouchSlot(addr, s2)

	slots := alt.WarmSlots(addr)
	if len(slots) != 2 {
		t.Fatalf("expected 2 warm slots, got %d", len(slots))
	}

	// No slots for unknown address.
	none := alt.WarmSlots(types.HexToAddress("0xff"))
	if none != nil {
		t.Error("expected nil for unknown address")
	}
}
