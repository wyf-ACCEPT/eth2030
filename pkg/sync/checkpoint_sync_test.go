package sync

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeCheckpoint creates a test checkpoint with deterministic values.
func makeCheckpoint(epoch, blockNum uint64) Checkpoint {
	return Checkpoint{
		Epoch:       epoch,
		BlockNumber: blockNum,
		BlockHash:   types.HexToHash("0xaa11bb22cc33dd44ee55ff6677889900aabbccdd11223344556677889900aabb"),
		StateRoot:   types.HexToHash("0x1122334455667788990011223344556677889900aabbccddeeff001122334455"),
	}
}

func TestNewCheckpointSyncer(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		cs := NewCheckpointSyncer(DefaultCheckpointConfig())
		if cs == nil {
			t.Fatal("expected non-nil syncer")
		}
		if cs.config.MaxHeaderBatch != 64 {
			t.Errorf("expected MaxHeaderBatch=64, got %d", cs.config.MaxHeaderBatch)
		}
		if cs.GetCheckpoint() != nil {
			t.Error("expected nil checkpoint initially")
		}
	})

	t.Run("with trusted checkpoint", func(t *testing.T) {
		cp := makeCheckpoint(100, 3200)
		config := CheckpointConfig{
			TrustedCheckpoint: &cp,
			VerifyHeaders:     true,
			MaxHeaderBatch:    128,
		}
		cs := NewCheckpointSyncer(config)
		got := cs.GetCheckpoint()
		if got == nil {
			t.Fatal("expected non-nil checkpoint")
		}
		if got.Epoch != 100 || got.BlockNumber != 3200 {
			t.Errorf("checkpoint mismatch: epoch=%d block=%d", got.Epoch, got.BlockNumber)
		}
	})

	t.Run("zero max header batch defaults to 64", func(t *testing.T) {
		config := CheckpointConfig{MaxHeaderBatch: 0}
		cs := NewCheckpointSyncer(config)
		if cs.config.MaxHeaderBatch != 64 {
			t.Errorf("expected MaxHeaderBatch=64, got %d", cs.config.MaxHeaderBatch)
		}
	})
}

func TestSetCheckpoint(t *testing.T) {
	cs := NewCheckpointSyncer(DefaultCheckpointConfig())

	t.Run("valid checkpoint", func(t *testing.T) {
		cp := makeCheckpoint(10, 320)
		if err := cs.SetCheckpoint(cp); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := cs.GetCheckpoint()
		if got == nil || got.BlockNumber != 320 {
			t.Error("checkpoint not stored correctly")
		}
	})

	t.Run("zero block hash", func(t *testing.T) {
		cp := Checkpoint{
			Epoch:       1,
			BlockNumber: 32,
			BlockHash:   types.Hash{},
			StateRoot:   types.HexToHash("0x1234"),
		}
		if err := cs.SetCheckpoint(cp); err != ErrCheckpointZeroHash {
			t.Errorf("expected ErrCheckpointZeroHash, got %v", err)
		}
	})

	t.Run("zero state root", func(t *testing.T) {
		cp := Checkpoint{
			Epoch:       1,
			BlockNumber: 32,
			BlockHash:   types.HexToHash("0xabcd"),
			StateRoot:   types.Hash{},
		}
		if err := cs.SetCheckpoint(cp); err != ErrCheckpointZeroState {
			t.Errorf("expected ErrCheckpointZeroState, got %v", err)
		}
	})

	t.Run("zero block number", func(t *testing.T) {
		cp := Checkpoint{
			Epoch:       1,
			BlockNumber: 0,
			BlockHash:   types.HexToHash("0xabcd"),
			StateRoot:   types.HexToHash("0x1234"),
		}
		if err := cs.SetCheckpoint(cp); err != ErrCheckpointZeroBlock {
			t.Errorf("expected ErrCheckpointZeroBlock, got %v", err)
		}
	})
}

func TestCheckpointHash(t *testing.T) {
	cp1 := makeCheckpoint(10, 320)
	cp2 := makeCheckpoint(10, 320)
	cp3 := makeCheckpoint(11, 352)

	h1 := cp1.Hash()
	h2 := cp2.Hash()
	h3 := cp3.Hash()

	if h1 != h2 {
		t.Error("same checkpoints should produce same hash")
	}
	if h1 == h3 {
		t.Error("different checkpoints should produce different hashes")
	}
	if h1.IsZero() {
		t.Error("checkpoint hash should not be zero")
	}
}

func TestVerifyCheckpoint(t *testing.T) {
	cs := NewCheckpointSyncer(DefaultCheckpointConfig())

	t.Run("valid checkpoint", func(t *testing.T) {
		cp := makeCheckpoint(10, 320)
		valid, err := cs.VerifyCheckpoint(cp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !valid {
			t.Error("expected valid checkpoint")
		}
		if cs.VerifiedCheckpoints() != 1 {
			t.Errorf("expected 1 verified checkpoint, got %d", cs.VerifiedCheckpoints())
		}
		if !cs.IsVerified(cp) {
			t.Error("checkpoint should be verified")
		}
	})

	t.Run("inconsistent epoch/block", func(t *testing.T) {
		cp := Checkpoint{
			Epoch:       100,
			BlockNumber: 10, // should be >= 100*32 = 3200
			BlockHash:   types.HexToHash("0xabcd"),
			StateRoot:   types.HexToHash("0x1234"),
		}
		valid, err := cs.VerifyCheckpoint(cp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if valid {
			t.Error("expected invalid checkpoint due to epoch/block mismatch")
		}
	})

	t.Run("zero epoch is valid", func(t *testing.T) {
		cp := Checkpoint{
			Epoch:       0,
			BlockNumber: 100,
			BlockHash:   types.HexToHash("0xabcd"),
			StateRoot:   types.HexToHash("0x1234"),
		}
		valid, err := cs.VerifyCheckpoint(cp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !valid {
			t.Error("expected valid checkpoint with zero epoch")
		}
	})

	t.Run("zero block hash error", func(t *testing.T) {
		cp := Checkpoint{BlockNumber: 100, StateRoot: types.HexToHash("0x1234")}
		_, err := cs.VerifyCheckpoint(cp)
		if err != ErrCheckpointZeroHash {
			t.Errorf("expected ErrCheckpointZeroHash, got %v", err)
		}
	})

	t.Run("zero state root error", func(t *testing.T) {
		cp := Checkpoint{BlockNumber: 100, BlockHash: types.HexToHash("0xabcd")}
		_, err := cs.VerifyCheckpoint(cp)
		if err != ErrCheckpointZeroState {
			t.Errorf("expected ErrCheckpointZeroState, got %v", err)
		}
	})

	t.Run("zero block number error", func(t *testing.T) {
		cp := Checkpoint{BlockHash: types.HexToHash("0xabcd"), StateRoot: types.HexToHash("0x1234")}
		_, err := cs.VerifyCheckpoint(cp)
		if err != ErrCheckpointZeroBlock {
			t.Errorf("expected ErrCheckpointZeroBlock, got %v", err)
		}
	})
}

func TestSyncFromCheckpoint(t *testing.T) {
	t.Run("no checkpoint set", func(t *testing.T) {
		cs := NewCheckpointSyncer(DefaultCheckpointConfig())
		if err := cs.SyncFromCheckpoint(); err != ErrCheckpointNotSet {
			t.Errorf("expected ErrCheckpointNotSet, got %v", err)
		}
	})

	t.Run("sync starts successfully", func(t *testing.T) {
		cp := makeCheckpoint(10, 320)
		cs := NewCheckpointSyncer(CheckpointConfig{
			TrustedCheckpoint: &cp,
			MaxHeaderBatch:    64,
		})
		cs.SetTarget(1000)
		if err := cs.SyncFromCheckpoint(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p := cs.Progress()
		if p.CurrentBlock != 320 {
			t.Errorf("expected CurrentBlock=320, got %d", p.CurrentBlock)
		}
		if p.TargetBlock != 1000 {
			t.Errorf("expected TargetBlock=1000, got %d", p.TargetBlock)
		}
		if p.StartTime.IsZero() {
			t.Error("expected non-zero start time")
		}
		if cs.IsComplete() {
			t.Error("sync should not be complete yet")
		}
	})

	t.Run("already syncing", func(t *testing.T) {
		cp := makeCheckpoint(10, 320)
		cs := NewCheckpointSyncer(CheckpointConfig{
			TrustedCheckpoint: &cp,
			MaxHeaderBatch:    64,
		})
		cs.SetTarget(1000)
		_ = cs.SyncFromCheckpoint()
		if err := cs.SyncFromCheckpoint(); err != ErrCheckpointSyncing {
			t.Errorf("expected ErrCheckpointSyncing, got %v", err)
		}
	})

	t.Run("target at or below checkpoint completes immediately", func(t *testing.T) {
		cp := makeCheckpoint(10, 320)
		cs := NewCheckpointSyncer(CheckpointConfig{
			TrustedCheckpoint: &cp,
			MaxHeaderBatch:    64,
		})
		cs.SetTarget(320)
		if err := cs.SyncFromCheckpoint(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cs.IsComplete() {
			t.Error("expected sync to be complete")
		}
		p := cs.Progress()
		if p.Percentage != 100.0 {
			t.Errorf("expected 100%% progress, got %.2f", p.Percentage)
		}
	})

	t.Run("sync after complete returns error", func(t *testing.T) {
		cp := makeCheckpoint(10, 320)
		cs := NewCheckpointSyncer(CheckpointConfig{
			TrustedCheckpoint: &cp,
			MaxHeaderBatch:    64,
		})
		cs.SetTarget(320)
		_ = cs.SyncFromCheckpoint()
		if err := cs.SyncFromCheckpoint(); err != ErrCheckpointComplete {
			t.Errorf("expected ErrCheckpointComplete, got %v", err)
		}
	})
}

func TestUpdateProgress(t *testing.T) {
	cp := makeCheckpoint(10, 320)
	cs := NewCheckpointSyncer(CheckpointConfig{
		TrustedCheckpoint: &cp,
		MaxHeaderBatch:    64,
	})
	cs.SetTarget(1320) // total range: 1000 blocks
	_ = cs.SyncFromCheckpoint()

	// Update to 50%.
	cs.UpdateProgress(820)
	p := cs.Progress()
	if p.CurrentBlock != 820 {
		t.Errorf("expected CurrentBlock=820, got %d", p.CurrentBlock)
	}
	if p.Percentage < 49.9 || p.Percentage > 50.1 {
		t.Errorf("expected ~50%%, got %.2f", p.Percentage)
	}

	// Update to 100%.
	cs.UpdateProgress(1320)
	p = cs.Progress()
	if p.Percentage != 100.0 {
		t.Errorf("expected 100%%, got %.2f", p.Percentage)
	}
	if !cs.IsComplete() {
		t.Error("expected sync to be complete")
	}
}

func TestReset(t *testing.T) {
	cp := makeCheckpoint(10, 320)
	cs := NewCheckpointSyncer(CheckpointConfig{
		TrustedCheckpoint: &cp,
		MaxHeaderBatch:    64,
	})
	cs.SetTarget(1000)
	_ = cs.SyncFromCheckpoint()

	cs.Reset()
	if cs.IsComplete() {
		t.Error("expected not complete after reset")
	}
	p := cs.Progress()
	if p.CurrentBlock != 0 || p.TargetBlock != 0 {
		t.Errorf("expected zero progress after reset, got current=%d target=%d",
			p.CurrentBlock, p.TargetBlock)
	}
}

func TestCheckpointSyncer_ConcurrentAccess(t *testing.T) {
	cp := makeCheckpoint(10, 320)
	cs := NewCheckpointSyncer(CheckpointConfig{
		TrustedCheckpoint: &cp,
		MaxHeaderBatch:    64,
	})
	cs.SetTarget(10000)
	_ = cs.SyncFromCheckpoint()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		blockNum := uint64(320 + i*10)
		go func(bn uint64) {
			defer wg.Done()
			cs.UpdateProgress(bn)
		}(blockNum)
		go func() {
			defer wg.Done()
			_ = cs.Progress()
		}()
		go func() {
			defer wg.Done()
			_ = cs.IsComplete()
		}()
	}
	wg.Wait()
}

func TestVerifyMultipleCheckpoints(t *testing.T) {
	cs := NewCheckpointSyncer(DefaultCheckpointConfig())

	for i := uint64(1); i <= 10; i++ {
		cp := makeCheckpoint(i, i*32)
		// Override hashes to make them unique.
		cp.BlockHash = types.BytesToHash([]byte{byte(i), 0xaa, 0xbb})
		cp.StateRoot = types.BytesToHash([]byte{byte(i), 0xcc, 0xdd})

		valid, err := cs.VerifyCheckpoint(cp)
		if err != nil {
			t.Fatalf("checkpoint %d: unexpected error: %v", i, err)
		}
		if !valid {
			t.Fatalf("checkpoint %d: expected valid", i)
		}
	}

	if cs.VerifiedCheckpoints() != 10 {
		t.Errorf("expected 10 verified checkpoints, got %d", cs.VerifiedCheckpoints())
	}
}

func TestSetTarget(t *testing.T) {
	cs := NewCheckpointSyncer(DefaultCheckpointConfig())
	cs.SetTarget(5000)
	cp := makeCheckpoint(10, 320)
	_ = cs.SetCheckpoint(cp)
	_ = cs.SyncFromCheckpoint()

	p := cs.Progress()
	if p.TargetBlock != 5000 {
		t.Errorf("expected TargetBlock=5000, got %d", p.TargetBlock)
	}
}
