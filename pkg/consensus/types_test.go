package consensus

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestSlotToEpoch(t *testing.T) {
	tests := []struct {
		slot          Slot
		slotsPerEpoch uint64
		want          Epoch
	}{
		{0, 32, 0},
		{31, 32, 0},
		{32, 32, 1},
		{63, 32, 1},
		{64, 32, 2},
		{0, 4, 0},
		{3, 4, 0},
		{4, 4, 1},
		{7, 4, 1},
		{8, 4, 2},
		{100, 1, 100},
	}
	for _, tt := range tests {
		got := SlotToEpoch(tt.slot, tt.slotsPerEpoch)
		if got != tt.want {
			t.Errorf("SlotToEpoch(%d, %d) = %d, want %d", tt.slot, tt.slotsPerEpoch, got, tt.want)
		}
	}
}

func TestSlotToEpoch_ZeroSlotsPerEpoch(t *testing.T) {
	got := SlotToEpoch(10, 0)
	if got != 0 {
		t.Errorf("SlotToEpoch with 0 slotsPerEpoch should return 0, got %d", got)
	}
}

func TestEpochStartSlot(t *testing.T) {
	tests := []struct {
		epoch         Epoch
		slotsPerEpoch uint64
		want          Slot
	}{
		{0, 32, 0},
		{1, 32, 32},
		{2, 32, 64},
		{0, 4, 0},
		{1, 4, 4},
		{2, 4, 8},
		{10, 4, 40},
	}
	for _, tt := range tests {
		got := EpochStartSlot(tt.epoch, tt.slotsPerEpoch)
		if got != tt.want {
			t.Errorf("EpochStartSlot(%d, %d) = %d, want %d", tt.epoch, tt.slotsPerEpoch, got, tt.want)
		}
	}
}

func TestJustificationBits(t *testing.T) {
	var bits JustificationBits

	// Initially all zero.
	for i := uint(0); i < 8; i++ {
		if bits.IsJustified(i) {
			t.Errorf("bit %d should not be set initially", i)
		}
	}

	// Set bit 0.
	bits.Set(0)
	if !bits.IsJustified(0) {
		t.Error("bit 0 should be set")
	}
	if bits.IsJustified(1) {
		t.Error("bit 1 should not be set")
	}

	// Set bit 2.
	bits.Set(2)
	if !bits.IsJustified(2) {
		t.Error("bit 2 should be set")
	}

	// Shift left by 1 (age bits: bit 0->1, bit 2->3).
	bits.Shift(1)
	if bits.IsJustified(0) {
		t.Error("after shift, bit 0 should be cleared")
	}
	if !bits.IsJustified(1) {
		t.Error("after shift, bit 1 should be the old bit 0 (set)")
	}
	if !bits.IsJustified(3) {
		t.Error("after shift, bit 3 should be the old bit 2 (set)")
	}
}

func TestJustificationBits_OutOfRange(t *testing.T) {
	var bits JustificationBits
	bits.Set(8) // out of range, should be no-op
	if bits != 0 {
		t.Error("setting bit 8 should be a no-op")
	}
	if bits.IsJustified(8) {
		t.Error("IsJustified(8) should return false")
	}
}

func TestCheckpoint(t *testing.T) {
	cp := Checkpoint{
		Epoch: 5,
		Root:  types.HexToHash("0xdead"),
	}
	if cp.Epoch != 5 {
		t.Errorf("expected epoch 5, got %d", cp.Epoch)
	}
	if cp.Root.IsZero() {
		t.Error("root should not be zero")
	}
}

func TestBeaconState_Defaults(t *testing.T) {
	var s BeaconState
	if s.Slot != 0 || s.Epoch != 0 {
		t.Error("default state should have zero slot and epoch")
	}
	if s.FinalizedCheckpoint.Epoch != 0 {
		t.Error("default finalized checkpoint should be epoch 0")
	}
	if s.JustifiedCheckpoint.Epoch != 0 {
		t.Error("default justified checkpoint should be epoch 0")
	}
}
