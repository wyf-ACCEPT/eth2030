package state

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestAccessEventTrackerNew(t *testing.T) {
	tracker := NewAccessEventTracker()
	if tracker == nil {
		t.Fatal("NewAccessEventTracker returned nil")
	}
	if tracker.AddressCount() != 0 {
		t.Fatalf("new tracker should have 0 addresses, got %d", tracker.AddressCount())
	}
	w := tracker.Witness()
	if w.TotalGas != 0 {
		t.Fatalf("new tracker should have 0 total gas, got %d", w.TotalGas)
	}
}

func TestAccessEventTrackerTouchAddressCold(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	gas := tracker.TouchAddress(addr, false)
	if gas != TrackerColdAccountAccessCost {
		t.Fatalf("cold address access: got %d, want %d", gas, TrackerColdAccountAccessCost)
	}

	w := tracker.Witness()
	if w.ColdReads != 1 {
		t.Fatalf("cold reads: got %d, want 1", w.ColdReads)
	}
}

func TestAccessEventTrackerTouchAddressWarm(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0x2222222222222222222222222222222222222222")

	// First access is cold.
	tracker.TouchAddress(addr, false)

	// Second access should be warm.
	gas := tracker.TouchAddress(addr, false)
	if gas != TrackerWarmStorageReadCost {
		t.Fatalf("warm address access: got %d, want %d", gas, TrackerWarmStorageReadCost)
	}

	w := tracker.Witness()
	if w.WarmReads != 1 {
		t.Fatalf("warm reads: got %d, want 1", w.WarmReads)
	}
}

func TestAccessEventTrackerTouchAddressWrite(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0x3333333333333333333333333333333333333333")

	gas := tracker.TouchAddress(addr, true)
	if gas != TrackerColdAccountAccessCost {
		t.Fatalf("cold write address: got %d, want %d", gas, TrackerColdAccountAccessCost)
	}

	w := tracker.Witness()
	if w.ColdWrites != 1 {
		t.Fatalf("cold writes after write: got %d, want 1", w.ColdWrites)
	}

	// Second write should be warm.
	tracker.TouchAddress(addr, true)
	w = tracker.Witness()
	if w.WarmWrites != 1 {
		t.Fatalf("warm writes after second write: got %d, want 1", w.WarmWrites)
	}
}

func TestAccessEventTrackerTouchStorageSlotCold(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0x4444444444444444444444444444444444444444")
	slot := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")

	gas := tracker.TouchStorageSlot(addr, slot, false)
	if gas != TrackerColdSloadCost {
		t.Fatalf("cold slot access: got %d, want %d", gas, TrackerColdSloadCost)
	}
}

func TestAccessEventTrackerTouchStorageSlotWarm(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0x5555555555555555555555555555555555555555")
	slot := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002")

	tracker.TouchStorageSlot(addr, slot, false)

	gas := tracker.TouchStorageSlot(addr, slot, false)
	if gas != TrackerWarmStorageReadCost {
		t.Fatalf("warm slot access: got %d, want %d", gas, TrackerWarmStorageReadCost)
	}
}

func TestAccessEventTrackerTouchStorageSlotWrite(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0x6666666666666666666666666666666666666666")
	slot := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000003")

	gas := tracker.TouchStorageSlot(addr, slot, true)
	if gas != TrackerColdSloadCost {
		t.Fatalf("cold slot write: got %d, want %d", gas, TrackerColdSloadCost)
	}

	w := tracker.Witness()
	if w.ColdWrites != 1 {
		t.Fatalf("cold writes after slot write: got %d, want 1", w.ColdWrites)
	}
}

func TestAccessEventTrackerTouchCodeCold(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0x7777777777777777777777777777777777777777")

	gas := tracker.TouchCode(addr, false)
	if gas != TrackerColdCodeAccessCost {
		t.Fatalf("cold code access: got %d, want %d", gas, TrackerColdCodeAccessCost)
	}
}

func TestAccessEventTrackerTouchCodeWarm(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0x8888888888888888888888888888888888888888")

	tracker.TouchCode(addr, false)
	gas := tracker.TouchCode(addr, false)
	if gas != TrackerWarmStorageReadCost {
		t.Fatalf("warm code access: got %d, want %d", gas, TrackerWarmStorageReadCost)
	}
}

func TestAccessEventTrackerIsAddressWarm(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0x9999999999999999999999999999999999999999")

	if tracker.IsAddressWarm(addr) {
		t.Fatal("address should not be warm before access")
	}

	tracker.TouchAddress(addr, false)

	if !tracker.IsAddressWarm(addr) {
		t.Fatal("address should be warm after access")
	}
}

func TestAccessEventTrackerIsSlotWarm(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	slot := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000005")

	if tracker.IsSlotWarm(addr, slot) {
		t.Fatal("slot should not be warm before access")
	}

	tracker.TouchStorageSlot(addr, slot, false)

	if !tracker.IsSlotWarm(addr, slot) {
		t.Fatal("slot should be warm after access")
	}
}

func TestAccessEventTrackerIsCodeWarm(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	if tracker.IsCodeWarm(addr) {
		t.Fatal("code should not be warm before access")
	}

	tracker.TouchCode(addr, false)

	if !tracker.IsCodeWarm(addr) {
		t.Fatal("code should be warm after access")
	}
}

func TestAccessEventTrackerMultipleAddresses(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")
	addr3 := types.HexToAddress("0x3333333333333333333333333333333333333333")

	tracker.TouchAddress(addr1, false)
	tracker.TouchAddress(addr2, false)
	tracker.TouchAddress(addr3, false)

	if tracker.AddressCount() != 3 {
		t.Fatalf("address count: got %d, want 3", tracker.AddressCount())
	}

	// Total gas should be 3 cold accesses.
	w := tracker.Witness()
	expectedGas := uint64(3) * TrackerColdAccountAccessCost
	if w.TotalGas != expectedGas {
		t.Fatalf("total gas: got %d, want %d", w.TotalGas, expectedGas)
	}
}

func TestAccessEventTrackerMultipleSlots(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	slot1 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	slot2 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002")

	tracker.TouchStorageSlot(addr, slot1, false)
	tracker.TouchStorageSlot(addr, slot2, false)

	if tracker.SlotCount() != 2 {
		t.Fatalf("slot count: got %d, want 2", tracker.SlotCount())
	}
}

func TestAccessEventTrackerTouchBranch(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0xdddddddddddddddddddddddddddddddddddddd")

	// Cold branch access.
	gas := tracker.TouchBranch(addr, 0, false)
	if gas != WitnessBranchReadCost {
		t.Fatalf("cold branch: got %d, want %d", gas, WitnessBranchReadCost)
	}

	// Warm branch access.
	gas = tracker.TouchBranch(addr, 0, false)
	if gas != 0 {
		t.Fatalf("warm branch: got %d, want 0", gas)
	}

	bc := tracker.GetBranchCounter(addr, 0)
	if bc == nil {
		t.Fatal("branch counter should not be nil")
	}
	if bc.ReadCount != 2 {
		t.Fatalf("branch read count: got %d, want 2", bc.ReadCount)
	}
}

func TestAccessEventTrackerTouchBranchWrite(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")

	tracker.TouchBranch(addr, 1, true)

	bc := tracker.GetBranchCounter(addr, 1)
	if bc == nil {
		t.Fatal("branch counter should not be nil after write")
	}
	if bc.WriteCount != 1 {
		t.Fatalf("branch write count: got %d, want 1", bc.WriteCount)
	}
}

func TestAccessEventTrackerReset(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	tracker.TouchAddress(addr, false)
	tracker.TouchStorageSlot(addr, types.Hash{}, false)
	tracker.TouchBranch(addr, 0, false)

	tracker.Reset()

	if tracker.AddressCount() != 0 {
		t.Fatalf("after reset, address count: got %d, want 0", tracker.AddressCount())
	}
	if tracker.SlotCount() != 0 {
		t.Fatalf("after reset, slot count: got %d, want 0", tracker.SlotCount())
	}
	if tracker.BranchCount() != 0 {
		t.Fatalf("after reset, branch count: got %d, want 0", tracker.BranchCount())
	}
	w := tracker.Witness()
	if w.TotalGas != 0 {
		t.Fatalf("after reset, total gas: got %d, want 0", w.TotalGas)
	}
}

func TestAccessEventTrackerGasAccumulation(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	slot := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")

	// Cold address access: 2600.
	tracker.TouchAddress(addr, false)
	// Cold slot access: 2100.
	tracker.TouchStorageSlot(addr, slot, false)
	// Cold code access: 2600.
	tracker.TouchCode(addr, false)
	// Warm address: 100.
	tracker.TouchAddress(addr, false)
	// Warm slot: 100.
	tracker.TouchStorageSlot(addr, slot, false)

	w := tracker.Witness()
	expectedTotal := TrackerColdAccountAccessCost + TrackerColdSloadCost +
		TrackerColdCodeAccessCost + TrackerWarmStorageReadCost + TrackerWarmStorageReadCost
	if w.TotalGas != expectedTotal {
		t.Fatalf("accumulated gas: got %d, want %d", w.TotalGas, expectedTotal)
	}
}

func TestAccessEventTrackerDifferentAddressesIndependent(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")
	slot := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")

	// Access slot on addr1.
	gas1 := tracker.TouchStorageSlot(addr1, slot, false)
	// Same slot on addr2 should still be cold.
	gas2 := tracker.TouchStorageSlot(addr2, slot, false)

	if gas1 != gas2 {
		t.Fatalf("same slot different addresses should both be cold: gas1=%d, gas2=%d", gas1, gas2)
	}
	if gas1 != TrackerColdSloadCost {
		t.Fatalf("expected cold sload cost %d, got %d", TrackerColdSloadCost, gas1)
	}
}

func TestAccessEventTrackerBranchCountMultiple(t *testing.T) {
	tracker := NewAccessEventTracker()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	tracker.TouchBranch(addr, 0, false)
	tracker.TouchBranch(addr, 1, false)
	tracker.TouchBranch(addr, 2, false)

	if tracker.BranchCount() != 3 {
		t.Fatalf("branch count: got %d, want 3", tracker.BranchCount())
	}
}
