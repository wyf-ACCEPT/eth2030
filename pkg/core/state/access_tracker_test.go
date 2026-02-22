package state

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func makeATAddr(b byte) types.Address {
	var a types.Address
	a[types.AddressLength-1] = b
	return a
}

func makeATHash(b byte) types.Hash {
	var h types.Hash
	h[types.HashLength-1] = b
	return h
}

func TestAccessTrackerNewEmpty(t *testing.T) {
	at := NewAccessTracker()
	if at.BlockAddressCount() != 0 {
		t.Fatal("expected 0 block addresses")
	}
	if at.BlockSlotCount() != 0 {
		t.Fatal("expected 0 block slots")
	}
	if at.TxCount() != 0 {
		t.Fatal("expected 0 transactions")
	}
}

func TestAccessTrackerColdAddressAccess(t *testing.T) {
	at := NewAccessTracker()
	addr := makeATAddr(0x01)

	gas := at.TouchAddress(addr)
	if gas != AccessTrackerColdAccountCost {
		t.Fatalf("expected cold cost %d, got %d", AccessTrackerColdAccountCost, gas)
	}
}

func TestAccessTrackerWarmAddressAccess(t *testing.T) {
	at := NewAccessTracker()
	addr := makeATAddr(0x02)

	at.TouchAddress(addr) // cold
	gas := at.TouchAddress(addr)
	if gas != AccessTrackerWarmAccessCost {
		t.Fatalf("expected warm cost %d, got %d", AccessTrackerWarmAccessCost, gas)
	}
}

func TestAccessTrackerColdSlotAccess(t *testing.T) {
	at := NewAccessTracker()
	addr := makeATAddr(0x03)
	slot := makeATHash(0x01)

	gas := at.TouchSlot(addr, slot)
	if gas != AccessTrackerColdSloadCost {
		t.Fatalf("expected cold sload cost %d, got %d", AccessTrackerColdSloadCost, gas)
	}
}

func TestAccessTrackerWarmSlotAccess(t *testing.T) {
	at := NewAccessTracker()
	addr := makeATAddr(0x04)
	slot := makeATHash(0x02)

	at.TouchSlot(addr, slot) // cold
	gas := at.TouchSlot(addr, slot)
	if gas != AccessTrackerWarmAccessCost {
		t.Fatalf("expected warm cost %d, got %d", AccessTrackerWarmAccessCost, gas)
	}
}

func TestAccessTrackerIsAddressWarm(t *testing.T) {
	at := NewAccessTracker()
	addr := makeATAddr(0x05)

	if at.IsAddressWarm(addr) {
		t.Fatal("expected cold address")
	}
	at.TouchAddress(addr)
	if !at.IsAddressWarm(addr) {
		t.Fatal("expected warm address")
	}
}

func TestAccessTrackerIsSlotWarm(t *testing.T) {
	at := NewAccessTracker()
	addr := makeATAddr(0x06)
	slot := makeATHash(0x03)

	if at.IsSlotWarm(addr, slot) {
		t.Fatal("expected cold slot")
	}
	at.TouchSlot(addr, slot)
	if !at.IsSlotWarm(addr, slot) {
		t.Fatal("expected warm slot")
	}
}

func TestAccessTrackerBeginEndTx(t *testing.T) {
	at := NewAccessTracker()
	addr := makeATAddr(0x07)

	txSet := at.BeginTx()
	if txSet == nil {
		t.Fatal("expected non-nil tx access set")
	}

	at.TouchAddress(addr)
	at.EndTx()

	if at.TxCount() != 1 {
		t.Fatalf("expected 1 tx, got %d", at.TxCount())
	}
	// Block-level should be warm now.
	if !at.IsAddressWarm(addr) {
		t.Fatal("expected warm address in block after tx")
	}
}

func TestAccessTrackerCrossTxWarmth(t *testing.T) {
	at := NewAccessTracker()
	addr := makeATAddr(0x08)

	// Tx 0: access addr (cold).
	at.BeginTx()
	gas1 := at.TouchAddress(addr)
	at.EndTx()

	// Tx 1: addr should be warm from block-level.
	at.BeginTx()
	gas2 := at.TouchAddress(addr)
	at.EndTx()

	if gas1 != AccessTrackerColdAccountCost {
		t.Fatalf("tx0: expected cold cost, got %d", gas1)
	}
	if gas2 != AccessTrackerWarmAccessCost {
		t.Fatalf("tx1: expected warm cost, got %d", gas2)
	}
}

func TestAccessTrackerCrossTxSlotWarmth(t *testing.T) {
	at := NewAccessTracker()
	addr := makeATAddr(0x09)
	slot := makeATHash(0x10)

	at.BeginTx()
	at.TouchSlot(addr, slot) // cold
	at.EndTx()

	at.BeginTx()
	gas := at.TouchSlot(addr, slot)
	at.EndTx()

	if gas != AccessTrackerWarmAccessCost {
		t.Fatalf("expected warm slot cost on second tx, got %d", gas)
	}
}

func TestAccessTrackerPreloadAccessList(t *testing.T) {
	at := NewAccessTracker()
	addr1 := makeATAddr(0x0A)
	addr2 := makeATAddr(0x0B)
	slot := makeATHash(0x20)

	at.BeginTx()
	at.PreloadAccessList(
		[]types.Address{addr1, addr2},
		map[types.Address][]types.Hash{
			addr1: {slot},
		},
	)

	// Both addresses should be warm.
	if !at.IsAddressWarm(addr1) {
		t.Fatal("expected addr1 warm after preload")
	}
	if !at.IsAddressWarm(addr2) {
		t.Fatal("expected addr2 warm after preload")
	}
	if !at.IsSlotWarm(addr1, slot) {
		t.Fatal("expected slot warm after preload")
	}

	// Access should cost warm price.
	gas := at.TouchAddress(addr1)
	if gas != AccessTrackerWarmAccessCost {
		t.Fatalf("expected warm cost after preload, got %d", gas)
	}
	at.EndTx()
}

func TestAccessTrackerPreloadAccessListAtBlockLevel(t *testing.T) {
	at := NewAccessTracker()
	addr := makeATAddr(0x0C)

	// Preload without an active tx => block-level.
	at.PreloadAccessList([]types.Address{addr}, nil)

	if !at.IsAddressWarm(addr) {
		t.Fatal("expected address warm at block level after preload")
	}
}

func TestAccessTrackerTxAccessSetAt(t *testing.T) {
	at := NewAccessTracker()
	addr := makeATAddr(0x0D)

	at.BeginTx()
	at.TouchAddress(addr)
	at.EndTx()

	txSet := at.TxAccessSetAt(0)
	if txSet == nil {
		t.Fatal("expected non-nil tx access set")
	}
	if !txSet.ContainsAddress(addr) {
		t.Fatal("expected addr in tx set")
	}

	// Out of range.
	if at.TxAccessSetAt(1) != nil {
		t.Fatal("expected nil for out-of-range index")
	}
	if at.TxAccessSetAt(-1) != nil {
		t.Fatal("expected nil for negative index")
	}
}

func TestAccessTrackerTxAccessSetCounters(t *testing.T) {
	tas := newTxAccessSet()
	addr := makeATAddr(0x0E)
	slot1 := makeATHash(0x30)
	slot2 := makeATHash(0x31)

	tas.TouchAddress(addr)     // cold
	tas.TouchAddress(addr)     // warm
	tas.TouchSlot(addr, slot1) // cold
	tas.TouchSlot(addr, slot2) // cold
	tas.TouchSlot(addr, slot1) // warm

	if tas.ColdAccountCount() != 1 {
		t.Fatalf("expected 1 cold account, got %d", tas.ColdAccountCount())
	}
	if tas.ColdSlotCount() != 2 {
		t.Fatalf("expected 2 cold slots, got %d", tas.ColdSlotCount())
	}
	if tas.WarmAccessCount() != 2 { // 1 warm addr + 1 warm slot
		t.Fatalf("expected 2 warm accesses, got %d", tas.WarmAccessCount())
	}
	if tas.AddressCount() != 1 {
		t.Fatalf("expected 1 address, got %d", tas.AddressCount())
	}
	if tas.SlotCount() != 2 {
		t.Fatalf("expected 2 slots, got %d", tas.SlotCount())
	}
}

func TestAccessTrackerTxAccessSetGasCharged(t *testing.T) {
	tas := newTxAccessSet()
	addr1 := makeATAddr(0x0F)
	addr2 := makeATAddr(0x10)
	slot := makeATHash(0x40)

	tas.TouchAddress(addr1)    // 2600
	tas.TouchAddress(addr2)    // 2600
	tas.TouchSlot(addr1, slot) // 2100

	expected := uint64(2600 + 2600 + 2100)
	if tas.GasCharged() != expected {
		t.Fatalf("expected gas %d, got %d", expected, tas.GasCharged())
	}
}

func TestAccessTrackerTxAccessSetEntries(t *testing.T) {
	tas := newTxAccessSet()
	addr := makeATAddr(0x11)
	slot := makeATHash(0x50)

	tas.TouchAddress(addr)
	tas.TouchSlot(addr, slot)

	entries := tas.Entries()
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(entries))
	}

	hasAddr := false
	hasSlot := false
	for _, e := range entries {
		if e.Address == addr && !e.IsSlot {
			hasAddr = true
		}
		if e.Address == addr && e.IsSlot && e.Slot == slot {
			hasSlot = true
		}
	}
	if !hasAddr {
		t.Fatal("expected address entry")
	}
	if !hasSlot {
		t.Fatal("expected slot entry")
	}
}

func TestAccessTrackerTxAccessSetContainsSlot(t *testing.T) {
	tas := newTxAccessSet()
	addr := makeATAddr(0x12)
	slot := makeATHash(0x60)

	addrWarm, slotWarm := tas.ContainsSlot(addr, slot)
	if addrWarm || slotWarm {
		t.Fatal("expected cold on empty set")
	}

	tas.TouchSlot(addr, slot)
	addrWarm, slotWarm = tas.ContainsSlot(addr, slot)
	if !addrWarm {
		t.Fatal("expected warm addr after touching slot")
	}
	if !slotWarm {
		t.Fatal("expected warm slot after touching slot")
	}
}

func TestAccessTrackerWithWitness(t *testing.T) {
	ae := NewAccessEvents()
	at := NewAccessTrackerWithWitness(ae)
	addr := makeATAddr(0x13)

	gas := at.TouchAddress(addr)
	if gas != AccessTrackerColdAccountCost {
		t.Fatalf("expected cold cost %d, got %d", AccessTrackerColdAccountCost, gas)
	}
	// Witness gas should be non-zero (from AccessEvents).
	if at.WitnessGas() == 0 {
		t.Fatal("expected non-zero witness gas")
	}
}

func TestAccessTrackerWithWitnessSlot(t *testing.T) {
	ae := NewAccessEvents()
	at := NewAccessTrackerWithWitness(ae)
	addr := makeATAddr(0x14)
	slot := makeATHash(0x70)

	at.TouchSlot(addr, slot)
	// After cold slot access, witness gas should be charged.
	if at.WitnessGas() == 0 {
		t.Fatal("expected non-zero witness gas for cold slot")
	}
}

func TestAccessTrackerReset(t *testing.T) {
	at := NewAccessTracker()
	addr := makeATAddr(0x15)

	at.BeginTx()
	at.TouchAddress(addr)
	at.EndTx()

	at.Reset()

	if at.BlockAddressCount() != 0 {
		t.Fatal("expected 0 addresses after reset")
	}
	if at.TxCount() != 0 {
		t.Fatal("expected 0 txs after reset")
	}
	if at.IsAddressWarm(addr) {
		t.Fatal("expected cold after reset")
	}
}

func TestAccessTrackerBlockCounts(t *testing.T) {
	at := NewAccessTracker()
	addr1 := makeATAddr(0x16)
	addr2 := makeATAddr(0x17)
	slot := makeATHash(0x80)

	at.BeginTx()
	at.TouchAddress(addr1)
	at.TouchSlot(addr2, slot)
	at.EndTx()

	if at.BlockAddressCount() != 2 {
		t.Fatalf("expected 2 block addresses, got %d", at.BlockAddressCount())
	}
	if at.BlockSlotCount() != 1 {
		t.Fatalf("expected 1 block slot, got %d", at.BlockSlotCount())
	}
}

func TestAccessTrackerMultipleTxMerge(t *testing.T) {
	at := NewAccessTracker()
	addr1 := makeATAddr(0x18)
	addr2 := makeATAddr(0x19)
	slot1 := makeATHash(0x90)
	slot2 := makeATHash(0x91)

	// Tx 0: access addr1 + slot1
	at.BeginTx()
	at.TouchAddress(addr1)
	at.TouchSlot(addr1, slot1)
	at.EndTx()

	// Tx 1: access addr2 + slot2
	at.BeginTx()
	at.TouchAddress(addr2)
	at.TouchSlot(addr2, slot2)
	at.EndTx()

	// Block should have both.
	if at.BlockAddressCount() != 2 {
		t.Fatalf("expected 2 block addresses, got %d", at.BlockAddressCount())
	}
	if at.BlockSlotCount() != 2 {
		t.Fatalf("expected 2 block slots, got %d", at.BlockSlotCount())
	}
}
