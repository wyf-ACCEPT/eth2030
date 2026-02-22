package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func makeRoot(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestWeighJustification(t *testing.T) {
	tests := []struct {
		total, vote uint64
		want        bool
	}{
		{0, 0, false},      // no weight
		{100, 66, false},   // 66/100 < 2/3
		{100, 67, true},    // 67/100 >= 2/3
		{3, 2, true},       // exactly 2/3
		{3, 1, false},      // 1/3
		{1000, 667, true},  // 66.7% >= 66.67%
		{1000, 666, false}, // 66.6% < 66.67%
		{99, 66, true},     // 66/99 = 2/3 exactly
		{99, 65, false},    // 65/99 < 2/3
	}
	for _, tt := range tests {
		got := WeighJustification(tt.total, tt.vote)
		if got != tt.want {
			t.Errorf("WeighJustification(%d, %d) = %v, want %v", tt.total, tt.vote, got, tt.want)
		}
	}
}

func TestFinalityTracker_Justify(t *testing.T) {
	ft := NewFinalityTracker(DefaultConfig())
	root := makeRoot(0xaa)

	ft.Justify(5, root)
	if ft.JustifiedEpoch() != 5 {
		t.Errorf("justified epoch = %d, want 5", ft.JustifiedEpoch())
	}
	if ft.State().JustifiedCheckpoint.Root != root {
		t.Error("justified root mismatch")
	}
	if !ft.State().JustificationBits.IsJustified(0) {
		t.Error("bit 0 should be set after Justify")
	}
}

func TestFinalityTracker_Finalize(t *testing.T) {
	ft := NewFinalityTracker(DefaultConfig())
	root := makeRoot(0xbb)

	ft.Finalize(3, root)
	if ft.FinalizedEpoch() != 3 {
		t.Errorf("finalized epoch = %d, want 3", ft.FinalizedEpoch())
	}
	if !ft.IsFinalizedAt(3) {
		t.Error("epoch 3 should be finalized")
	}
	if !ft.IsFinalizedAt(2) {
		t.Error("epoch 2 should be finalized (before finalized epoch)")
	}
	if ft.IsFinalizedAt(4) {
		t.Error("epoch 4 should not be finalized")
	}
}

func TestFinalityTracker_FinalityDelay(t *testing.T) {
	ft := NewFinalityTracker(DefaultConfig())
	ft.state.Epoch = 10
	ft.state.FinalizedCheckpoint.Epoch = 8
	if ft.FinalityDelay() != 2 {
		t.Errorf("finality delay = %d, want 2", ft.FinalityDelay())
	}

	ft.state.FinalizedCheckpoint.Epoch = 10
	if ft.FinalityDelay() != 0 {
		t.Errorf("finality delay = %d, want 0", ft.FinalityDelay())
	}
}

func TestSingleEpochFinality_Basic(t *testing.T) {
	cfg := QuickSlotsConfig() // 1-epoch finality
	ft := NewFinalityTracker(cfg)

	// Process epoch 0: justify with supermajority.
	ft.ProcessEpoch(0, makeRoot(0x01), 100, 70)

	if ft.JustifiedEpoch() != 0 {
		t.Errorf("justified epoch = %d, want 0", ft.JustifiedEpoch())
	}
	if ft.FinalizedEpoch() != 0 {
		t.Errorf("finalized epoch = %d, want 0 (single-epoch finality)", ft.FinalizedEpoch())
	}
}

func TestSingleEpochFinality_NoSupermajority(t *testing.T) {
	cfg := QuickSlotsConfig()
	ft := NewFinalityTracker(cfg)

	// Process epoch 0: no supermajority (only 50%).
	ft.ProcessEpoch(0, makeRoot(0x01), 100, 50)

	if ft.JustifiedEpoch() != 0 {
		// Not justified because 50/100 < 2/3.
		t.Errorf("should not be justified at epoch 0, but JustifiedEpoch=%d", ft.JustifiedEpoch())
	}
	if ft.FinalizedEpoch() != 0 {
		// Finalized epoch stays at 0 (genesis).
		t.Errorf("finalized epoch should be 0, got %d", ft.FinalizedEpoch())
	}
}

func TestSingleEpochFinality_Progressive(t *testing.T) {
	cfg := QuickSlotsConfig()
	ft := NewFinalityTracker(cfg)

	// Epoch 1 justified.
	ft.ProcessEpoch(1, makeRoot(0x01), 100, 70)
	if ft.FinalizedEpoch() != 1 {
		t.Errorf("after epoch 1, finalized = %d, want 1", ft.FinalizedEpoch())
	}

	// Epoch 2 justified.
	ft.ProcessEpoch(2, makeRoot(0x02), 100, 80)
	if ft.FinalizedEpoch() != 2 {
		t.Errorf("after epoch 2, finalized = %d, want 2", ft.FinalizedEpoch())
	}

	// Epoch 3 not justified (insufficient votes).
	ft.ProcessEpoch(3, makeRoot(0x03), 100, 60)
	if ft.FinalizedEpoch() != 2 {
		t.Errorf("after epoch 3 (no supermajority), finalized = %d, want 2", ft.FinalizedEpoch())
	}
}

func TestDualEpochFinality_JustifyThenFinalize(t *testing.T) {
	cfg := DefaultConfig() // 2-epoch finality
	ft := NewFinalityTracker(cfg)

	// Epoch 1: justify.
	ft.ProcessEpoch(1, makeRoot(0x01), 100, 70)
	if ft.JustifiedEpoch() != 1 {
		t.Errorf("justified = %d, want 1", ft.JustifiedEpoch())
	}

	// Epoch 2: justify again => finalize epoch 1.
	ft.ProcessEpoch(2, makeRoot(0x02), 100, 70)
	if ft.JustifiedEpoch() != 2 {
		t.Errorf("justified = %d, want 2", ft.JustifiedEpoch())
	}
	if ft.FinalizedEpoch() != 1 {
		t.Errorf("finalized = %d, want 1", ft.FinalizedEpoch())
	}
}

func TestDualEpochFinality_GapBreaksFinality(t *testing.T) {
	cfg := DefaultConfig()
	ft := NewFinalityTracker(cfg)

	// Epoch 1: justify.
	ft.ProcessEpoch(1, makeRoot(0x01), 100, 70)

	// Epoch 2: NOT justified (gap).
	ft.ProcessEpoch(2, makeRoot(0x02), 100, 50)

	// Epoch 1 justified but not finalized because epoch 2 wasn't justified.
	if ft.FinalizedEpoch() != 0 {
		t.Errorf("finalized = %d, want 0 (gap breaks chain)", ft.FinalizedEpoch())
	}
}

func TestFinalityTracker_SetState(t *testing.T) {
	ft := NewFinalityTracker(DefaultConfig())
	s := BeaconState{
		Slot:  100,
		Epoch: 3,
		FinalizedCheckpoint: Checkpoint{Epoch: 1, Root: makeRoot(0x01)},
		JustifiedCheckpoint: Checkpoint{Epoch: 2, Root: makeRoot(0x02)},
	}
	ft.SetState(s)
	got := ft.State()
	if got.Slot != 100 || got.Epoch != 3 {
		t.Error("SetState/State roundtrip failed")
	}
	if ft.FinalizedEpoch() != 1 {
		t.Errorf("finalized = %d, want 1", ft.FinalizedEpoch())
	}
}

func TestFinalityTracker_IsFinalizedAt_Zero(t *testing.T) {
	ft := NewFinalityTracker(DefaultConfig())
	// Genesis epoch 0 is always finalized by default.
	if !ft.IsFinalizedAt(0) {
		t.Error("epoch 0 should be finalized")
	}
	if ft.IsFinalizedAt(1) {
		t.Error("epoch 1 should not be finalized at start")
	}
}
