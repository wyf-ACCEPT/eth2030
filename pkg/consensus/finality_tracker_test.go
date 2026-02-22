package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func trackerRoot(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestNewEpochFinalityTracker(t *testing.T) {
	cfg := DefaultFinalityConfig()
	ft := NewEpochFinalityTracker(cfg)

	if ft.HeadEpoch() != 0 {
		t.Errorf("head epoch = %d, want 0", ft.HeadEpoch())
	}
	if got := ft.Config(); got.EpochLength != 32 {
		t.Errorf("config EpochLength = %d, want 32", got.EpochLength)
	}

	// Genesis is both justified and finalized.
	j := ft.LatestJustified()
	if j == nil || j.Epoch != 0 || !j.Justified {
		t.Error("genesis should be justified")
	}
	f := ft.LatestFinalized()
	if f == nil || f.Epoch != 0 || !f.Finalized {
		t.Error("genesis should be finalized")
	}
}

func TestEpochFinalityTracker_ProcessEpoch(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	if err := ft.ProcessEpoch(1, trackerRoot(0x01)); err != nil {
		t.Fatalf("ProcessEpoch(1): %v", err)
	}
	if err := ft.ProcessEpoch(2, trackerRoot(0x02)); err != nil {
		t.Fatalf("ProcessEpoch(2): %v", err)
	}
	if ft.HeadEpoch() != 2 {
		t.Errorf("head epoch = %d, want 2", ft.HeadEpoch())
	}
}

func TestEpochFinalityTracker_ProcessEpoch_Duplicate(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	if err := ft.ProcessEpoch(1, trackerRoot(0x01)); err != nil {
		t.Fatal(err)
	}
	if err := ft.ProcessEpoch(1, trackerRoot(0x01)); err == nil {
		t.Error("expected error for duplicate epoch")
	}
}

func TestEpochFinalityTracker_ProcessEpoch_Decreasing(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	if err := ft.ProcessEpoch(5, trackerRoot(0x05)); err != nil {
		t.Fatal(err)
	}
	if err := ft.ProcessEpoch(3, trackerRoot(0x03)); err == nil {
		t.Error("expected error for decreasing epoch")
	}
}

func TestEpochFinalityTracker_ProcessEpoch_Slot(t *testing.T) {
	cfg := FinalityConfig{EpochLength: 32, FinalityDelay: 2, SafetyThreshold: 0.667}
	ft := NewEpochFinalityTracker(cfg)

	if err := ft.ProcessEpoch(3, trackerRoot(0x03)); err != nil {
		t.Fatal(err)
	}
	history := ft.CheckpointHistory(1)
	if len(history) == 0 {
		t.Fatal("no history")
	}
	// Slot should be epoch * EpochLength = 3 * 32 = 96.
	if history[0].Slot != 96 {
		t.Errorf("slot = %d, want 96", history[0].Slot)
	}
}

func TestEpochFinalityTracker_Justify(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	if err := ft.ProcessEpoch(1, trackerRoot(0x01)); err != nil {
		t.Fatal(err)
	}
	if err := ft.Justify(1); err != nil {
		t.Fatalf("Justify(1): %v", err)
	}

	j := ft.LatestJustified()
	if j == nil || j.Epoch != 1 {
		t.Errorf("latest justified epoch = %v, want 1", j)
	}
	if !j.Justified {
		t.Error("checkpoint should be marked justified")
	}
}

func TestEpochFinalityTracker_Justify_Unknown(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	if err := ft.Justify(99); err == nil {
		t.Error("expected error for unknown epoch")
	}
}

func TestEpochFinalityTracker_Justify_Idempotent(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	if err := ft.ProcessEpoch(1, trackerRoot(0x01)); err != nil {
		t.Fatal(err)
	}
	if err := ft.Justify(1); err != nil {
		t.Fatal(err)
	}
	// Justify again should be a no-op.
	if err := ft.Justify(1); err != nil {
		t.Fatalf("double justify should not error: %v", err)
	}
}

func TestEpochFinalityTracker_Finalize(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	if err := ft.ProcessEpoch(1, trackerRoot(0x01)); err != nil {
		t.Fatal(err)
	}
	if err := ft.Justify(1); err != nil {
		t.Fatal(err)
	}
	if err := ft.Finalize(1); err != nil {
		t.Fatalf("Finalize(1): %v", err)
	}

	f := ft.LatestFinalized()
	if f == nil || f.Epoch != 1 {
		t.Errorf("latest finalized epoch = %v, want 1", f)
	}
	if !f.Finalized {
		t.Error("checkpoint should be marked finalized")
	}
}

func TestEpochFinalityTracker_Finalize_RequiresJustified(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	if err := ft.ProcessEpoch(1, trackerRoot(0x01)); err != nil {
		t.Fatal(err)
	}
	if err := ft.Finalize(1); err == nil {
		t.Error("expected error finalizing unjustified epoch")
	}
}

func TestEpochFinalityTracker_Finalize_Unknown(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	if err := ft.Finalize(99); err == nil {
		t.Error("expected error for unknown epoch")
	}
}

func TestEpochFinalityTracker_Finalize_Idempotent(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	if err := ft.ProcessEpoch(1, trackerRoot(0x01)); err != nil {
		t.Fatal(err)
	}
	if err := ft.Justify(1); err != nil {
		t.Fatal(err)
	}
	if err := ft.Finalize(1); err != nil {
		t.Fatal(err)
	}
	// Double finalize should be a no-op.
	if err := ft.Finalize(1); err != nil {
		t.Fatalf("double finalize should not error: %v", err)
	}
}

func TestEpochFinalityTracker_IsFinalized(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	// Genesis (epoch 0) is finalized.
	if !ft.IsFinalized(0) {
		t.Error("epoch 0 should be finalized")
	}

	for i := uint64(1); i <= 5; i++ {
		if err := ft.ProcessEpoch(i, trackerRoot(byte(i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := ft.Justify(3); err != nil {
		t.Fatal(err)
	}
	if err := ft.Finalize(3); err != nil {
		t.Fatal(err)
	}

	// Epochs 0-3 should be finalized, 4-5 should not.
	for epoch := uint64(0); epoch <= 3; epoch++ {
		if !ft.IsFinalized(epoch) {
			t.Errorf("epoch %d should be finalized", epoch)
		}
	}
	for epoch := uint64(4); epoch <= 5; epoch++ {
		if ft.IsFinalized(epoch) {
			t.Errorf("epoch %d should not be finalized", epoch)
		}
	}
}

func TestEpochFinalityTracker_FinalityGap(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	// At genesis, gap is 0.
	if ft.FinalityGap() != 0 {
		t.Errorf("gap = %d, want 0", ft.FinalityGap())
	}

	// Process epochs 1-5 without finalizing beyond genesis.
	for i := uint64(1); i <= 5; i++ {
		if err := ft.ProcessEpoch(i, trackerRoot(byte(i))); err != nil {
			t.Fatal(err)
		}
	}
	// Head is 5, finalized is 0 => gap = 5.
	if ft.FinalityGap() != 5 {
		t.Errorf("gap = %d, want 5", ft.FinalityGap())
	}

	// Finalize epoch 3.
	if err := ft.Justify(3); err != nil {
		t.Fatal(err)
	}
	if err := ft.Finalize(3); err != nil {
		t.Fatal(err)
	}
	// Head is 5, finalized is 3 => gap = 2.
	if ft.FinalityGap() != 2 {
		t.Errorf("gap = %d, want 2", ft.FinalityGap())
	}
}

func TestEpochFinalityTracker_CheckpointHistory(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	for i := uint64(1); i <= 5; i++ {
		if err := ft.ProcessEpoch(i, trackerRoot(byte(i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := ft.Justify(2); err != nil {
		t.Fatal(err)
	}
	if err := ft.Finalize(2); err != nil {
		t.Fatal(err)
	}

	// Get last 3 checkpoints (newest first).
	history := ft.CheckpointHistory(3)
	if len(history) != 3 {
		t.Fatalf("history length = %d, want 3", len(history))
	}
	if history[0].Epoch != 5 {
		t.Errorf("history[0].Epoch = %d, want 5", history[0].Epoch)
	}
	if history[1].Epoch != 4 {
		t.Errorf("history[1].Epoch = %d, want 4", history[1].Epoch)
	}
	if history[2].Epoch != 3 {
		t.Errorf("history[2].Epoch = %d, want 3", history[2].Epoch)
	}
}

func TestEpochFinalityTracker_CheckpointHistory_LargeLimit(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	if err := ft.ProcessEpoch(1, trackerRoot(0x01)); err != nil {
		t.Fatal(err)
	}

	// Request more than available: should return all (genesis + epoch 1).
	history := ft.CheckpointHistory(100)
	if len(history) != 2 {
		t.Errorf("history length = %d, want 2", len(history))
	}
}

func TestEpochFinalityTracker_CheckpointHistory_ZeroLimit(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	history := ft.CheckpointHistory(0)
	if history != nil {
		t.Error("expected nil for zero limit")
	}

	history = ft.CheckpointHistory(-1)
	if history != nil {
		t.Error("expected nil for negative limit")
	}
}

func TestEpochFinalityTracker_LatestJustified_Progression(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	for i := uint64(1); i <= 3; i++ {
		if err := ft.ProcessEpoch(i, trackerRoot(byte(i))); err != nil {
			t.Fatal(err)
		}
	}

	// Justify epoch 1.
	if err := ft.Justify(1); err != nil {
		t.Fatal(err)
	}
	j := ft.LatestJustified()
	if j.Epoch != 1 {
		t.Errorf("latest justified = %d, want 1", j.Epoch)
	}

	// Justify epoch 3 (skipping 2).
	if err := ft.Justify(3); err != nil {
		t.Fatal(err)
	}
	j = ft.LatestJustified()
	if j.Epoch != 3 {
		t.Errorf("latest justified = %d, want 3", j.Epoch)
	}
}

func TestEpochFinalityTracker_LatestFinalized_Progression(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	for i := uint64(1); i <= 4; i++ {
		if err := ft.ProcessEpoch(i, trackerRoot(byte(i))); err != nil {
			t.Fatal(err)
		}
		if err := ft.Justify(i); err != nil {
			t.Fatal(err)
		}
	}

	if err := ft.Finalize(2); err != nil {
		t.Fatal(err)
	}
	if ft.LatestFinalized().Epoch != 2 {
		t.Errorf("latest finalized = %d, want 2", ft.LatestFinalized().Epoch)
	}

	if err := ft.Finalize(4); err != nil {
		t.Fatal(err)
	}
	if ft.LatestFinalized().Epoch != 4 {
		t.Errorf("latest finalized = %d, want 4", ft.LatestFinalized().Epoch)
	}
}

func TestEpochFinalityTracker_Concurrent(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	// Pre-load epochs.
	for i := uint64(1); i <= 100; i++ {
		if err := ft.ProcessEpoch(i, trackerRoot(byte(i%256))); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup

	// Concurrent justification.
	for i := uint64(1); i <= 50; i++ {
		wg.Add(1)
		go func(epoch uint64) {
			defer wg.Done()
			_ = ft.Justify(epoch)
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ft.LatestJustified()
			_ = ft.LatestFinalized()
			_ = ft.IsFinalized(10)
			_ = ft.FinalityGap()
			_ = ft.CheckpointHistory(5)
		}()
	}

	wg.Wait()

	// All epochs 1-50 should be justified.
	for i := uint64(1); i <= 50; i++ {
		history := ft.CheckpointHistory(int(ft.HeadEpoch()) + 1)
		found := false
		for _, cp := range history {
			if cp.Epoch == i && cp.Justified {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("epoch %d should be justified", i)
		}
	}
}

func TestEpochFinalityTracker_CheckpointRoot(t *testing.T) {
	ft := NewEpochFinalityTracker(DefaultFinalityConfig())

	root := trackerRoot(0xab)
	if err := ft.ProcessEpoch(1, root); err != nil {
		t.Fatal(err)
	}
	if err := ft.Justify(1); err != nil {
		t.Fatal(err)
	}

	j := ft.LatestJustified()
	if j.Root != root {
		t.Errorf("root = %v, want %v", j.Root, root)
	}
}

func TestEpochFinalityTracker_FullLifecycle(t *testing.T) {
	cfg := FinalityConfig{EpochLength: 4, FinalityDelay: 2, SafetyThreshold: 0.667}
	ft := NewEpochFinalityTracker(cfg)

	// Process 10 epochs, justify all, finalize up to epoch 8.
	for i := uint64(1); i <= 10; i++ {
		if err := ft.ProcessEpoch(i, trackerRoot(byte(i))); err != nil {
			t.Fatalf("ProcessEpoch(%d): %v", i, err)
		}
		if err := ft.Justify(i); err != nil {
			t.Fatalf("Justify(%d): %v", i, err)
		}
	}
	for i := uint64(1); i <= 8; i++ {
		if err := ft.Finalize(i); err != nil {
			t.Fatalf("Finalize(%d): %v", i, err)
		}
	}

	if ft.LatestFinalized().Epoch != 8 {
		t.Errorf("finalized = %d, want 8", ft.LatestFinalized().Epoch)
	}
	if ft.LatestJustified().Epoch != 10 {
		t.Errorf("justified = %d, want 10", ft.LatestJustified().Epoch)
	}
	if ft.FinalityGap() != 2 {
		t.Errorf("gap = %d, want 2", ft.FinalityGap())
	}

	// Epochs 0-8 finalized.
	for i := uint64(0); i <= 8; i++ {
		if !ft.IsFinalized(i) {
			t.Errorf("epoch %d should be finalized", i)
		}
	}
	// Epochs 9-10 not finalized.
	for i := uint64(9); i <= 10; i++ {
		if ft.IsFinalized(i) {
			t.Errorf("epoch %d should not be finalized", i)
		}
	}

	// History: 11 entries (genesis + epochs 1-10).
	history := ft.CheckpointHistory(11)
	if len(history) != 11 {
		t.Fatalf("history length = %d, want 11", len(history))
	}
	// Newest first.
	if history[0].Epoch != 10 {
		t.Errorf("newest = %d, want 10", history[0].Epoch)
	}
	if history[10].Epoch != 0 {
		t.Errorf("oldest = %d, want 0", history[10].Epoch)
	}
}
