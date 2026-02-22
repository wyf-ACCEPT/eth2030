package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestCheckpointStoreBasic(t *testing.T) {
	store := NewCheckpointStore()
	if store.Count() != 0 {
		t.Errorf("expected 0 count, got %d", store.Count())
	}

	cp := &FinalizedCheckpoint{
		Epoch:          10,
		Root:           types.Hash{0x01},
		JustifiedEpoch: 9,
	}
	store.Put(cp)

	if store.Count() != 1 {
		t.Errorf("expected 1 count, got %d", store.Count())
	}

	got, err := store.Get(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Epoch != 10 {
		t.Errorf("expected epoch 10, got %d", got.Epoch)
	}
	if got.Root != (types.Hash{0x01}) {
		t.Errorf("expected root 0x01, got %x", got.Root)
	}

	// Not found.
	_, err = store.Get(99)
	if err == nil {
		t.Error("expected error for missing epoch")
	}

	// Latest.
	latest := store.Latest()
	if latest == nil {
		t.Fatal("expected non-nil latest")
	}
	if latest.Epoch != 10 {
		t.Errorf("expected latest epoch 10, got %d", latest.Epoch)
	}
}

func TestCheckpointStoreNilPut(t *testing.T) {
	store := NewCheckpointStore()
	store.Put(nil)
	if store.Count() != 0 {
		t.Error("putting nil should not add to store")
	}
	if store.Latest() != nil {
		t.Error("latest should be nil after nil put")
	}
}

func TestCheckpointManagerCreate(t *testing.T) {
	mgr := NewCheckpointManager(nil)
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	if mgr.FinalizedEpoch() != 0 {
		t.Errorf("expected finalized epoch 0, got %d", mgr.FinalizedEpoch())
	}
}

func TestCheckpointManagerProcessJustification(t *testing.T) {
	mgr := NewCheckpointManager(nil)

	root := types.Hash{0xAA}

	// Sufficient voting stake: 700 / 900 >= 2/3.
	result := mgr.ProcessJustification(1, 900, 700, root)
	if !result.Justified {
		t.Error("expected justified with 700/900")
	}
	if result.Participation < 0.77 || result.Participation > 0.78 {
		t.Errorf("expected ~0.778 participation, got %f", result.Participation)
	}

	if mgr.JustifiedEpoch() != 1 {
		t.Errorf("expected justified epoch 1, got %d", mgr.JustifiedEpoch())
	}

	// Insufficient: 200/900 < 2/3.
	result2 := mgr.ProcessJustification(2, 900, 200, types.Hash{0xBB})
	if result2.Justified {
		t.Error("expected not justified with 200/900")
	}
}

func TestCheckpointManagerProcessJustificationZeroStake(t *testing.T) {
	mgr := NewCheckpointManager(nil)
	result := mgr.ProcessJustification(1, 0, 0, types.Hash{})
	if result.Justified {
		t.Error("expected not justified with zero stake")
	}
}

func TestCheckpointManagerProcessFinalization(t *testing.T) {
	mgr := NewCheckpointManager(nil)

	root1 := types.Hash{0x01}
	root2 := types.Hash{0x02}

	// Justify epochs 1 and 2.
	mgr.ProcessJustification(1, 100, 80, root1)
	mgr.ProcessJustification(2, 100, 80, root2)

	// Attempt finalization at epoch 2.
	// Condition 2: bits[0] and bits[1] set, justified.Epoch == 2,
	// prevJustified.Epoch + 1 == 2 -> finalize epoch 1.
	cp := mgr.ProcessFinalization(2)
	if cp == nil {
		t.Fatal("expected finalization at epoch 2")
	}
	if cp.Epoch != 1 {
		t.Errorf("expected finalized epoch 1, got %d", cp.Epoch)
	}

	if mgr.FinalizedEpoch() != 1 {
		t.Errorf("expected finalized epoch 1 in manager, got %d", mgr.FinalizedEpoch())
	}
}

func TestCheckpointManagerNoFinalization(t *testing.T) {
	mgr := NewCheckpointManager(nil)

	// Only justify epoch 1, not epoch 2.
	mgr.ProcessJustification(1, 100, 80, types.Hash{0x01})

	cp := mgr.ProcessFinalization(1)
	if cp != nil {
		t.Error("expected no finalization with only one justified epoch")
	}
}

func TestCheckpointManagerSetJustifiedFinalized(t *testing.T) {
	mgr := NewCheckpointManager(nil)

	mgr.SetJustified(5, types.Hash{0x55})
	if mgr.JustifiedEpoch() != 5 {
		t.Errorf("expected justified epoch 5, got %d", mgr.JustifiedEpoch())
	}

	mgr.SetFinalized(3, types.Hash{0x33})
	if mgr.FinalizedEpoch() != 3 {
		t.Errorf("expected finalized epoch 3, got %d", mgr.FinalizedEpoch())
	}
	if mgr.FinalizedRoot() != (types.Hash{0x33}) {
		t.Errorf("expected root 0x33, got %x", mgr.FinalizedRoot())
	}
}

func TestCheckpointManagerIsFinalizedAt(t *testing.T) {
	mgr := NewCheckpointManager(nil)
	mgr.SetFinalized(10, types.Hash{0xAA})

	if !mgr.IsFinalizedAt(5) {
		t.Error("epoch 5 should be finalized (before epoch 10)")
	}
	if !mgr.IsFinalizedAt(10) {
		t.Error("epoch 10 should be finalized (at epoch 10)")
	}
	if mgr.IsFinalizedAt(11) {
		t.Error("epoch 11 should not be finalized (after epoch 10)")
	}
}

func TestCheckpointManagerStats(t *testing.T) {
	mgr := NewCheckpointManager(nil)
	mgr.ProcessJustification(1, 100, 80, types.Hash{0x01})
	mgr.ProcessJustification(2, 100, 80, types.Hash{0x02})
	mgr.ProcessFinalization(2)

	stats := mgr.GetStats()
	if stats.Justifications != 2 {
		t.Errorf("expected 2 justifications, got %d", stats.Justifications)
	}
	if stats.Finalizations < 1 {
		t.Errorf("expected at least 1 finalization, got %d", stats.Finalizations)
	}
}

func TestCheckpointManagerPrune(t *testing.T) {
	mgr := NewCheckpointManager(nil)
	mgr.SetFinalized(5, types.Hash{0x05})
	mgr.SetFinalized(10, types.Hash{0x0A})
	mgr.SetFinalized(15, types.Hash{0x0F})

	if mgr.Store().Count() != 3 {
		t.Fatalf("expected 3 stored, got %d", mgr.Store().Count())
	}

	pruned := mgr.Prune(10)
	if pruned != 1 {
		t.Errorf("expected 1 pruned (epoch 5), got %d", pruned)
	}
	if mgr.Store().Count() != 2 {
		t.Errorf("expected 2 remaining, got %d", mgr.Store().Count())
	}
}

func TestCheckpointManagerJustificationBitsSnapshot(t *testing.T) {
	mgr := NewCheckpointManager(nil)
	mgr.ProcessJustification(1, 100, 80, types.Hash{0x01})

	bits := mgr.JustificationBitsSnapshot()
	if !bits.IsJustified(0) {
		t.Error("expected bit 0 justified after epoch 1 justification")
	}

	mgr.ProcessJustification(2, 100, 80, types.Hash{0x02})
	bits = mgr.JustificationBitsSnapshot()
	if !bits.IsJustified(0) {
		t.Error("expected bit 0 justified after epoch 2 justification")
	}
	if !bits.IsJustified(1) {
		t.Error("expected bit 1 justified (shifted from epoch 1)")
	}
}
