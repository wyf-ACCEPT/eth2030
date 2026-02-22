package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func csTestHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	h[31] = b
	return h
}

func csStoreWithChain(t *testing.T, n int) *CheckpointPersistenceStore {
	t.Helper()
	store := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())
	for i := 0; i < n; i++ {
		parent := Epoch(0)
		if i > 0 {
			parent = Epoch(i - 1)
		}
		cp := &StoredCheckpoint{
			Epoch:       Epoch(i),
			Root:        csTestHash(byte(i)),
			Justified:   true,
			Finalized:   true,
			ParentEpoch: parent,
		}
		if err := store.StoreCheckpoint(cp, false); err != nil {
			t.Fatalf("StoreCheckpoint(%d): %v", i, err)
		}
	}
	return store
}

// TestCSStoreAndRetrieve verifies basic store and get operations.
func TestCSStoreAndRetrieve(t *testing.T) {
	store := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())

	cp := &StoredCheckpoint{
		Epoch:     10,
		Root:      csTestHash(0xAA),
		Justified: true,
		Finalized: false,
	}
	if err := store.StoreCheckpoint(cp, false); err != nil {
		t.Fatalf("StoreCheckpoint: %v", err)
	}

	got, err := store.GetCheckpoint(10)
	if err != nil {
		t.Fatalf("GetCheckpoint: %v", err)
	}
	if got.Epoch != 10 {
		t.Errorf("epoch=%d, want 10", got.Epoch)
	}
	if got.Root != csTestHash(0xAA) {
		t.Errorf("root mismatch")
	}
	if !got.Justified {
		t.Error("expected justified=true")
	}
	if got.Finalized {
		t.Error("expected finalized=false")
	}
}

// TestCSStoreEdgeCases verifies defensive copy, nil, duplicate, and not-found.
func TestCSStoreEdgeCases(t *testing.T) {
	store := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())

	// Nil rejected.
	if err := store.StoreCheckpoint(nil, false); err != ErrCSNilCheckpoint {
		t.Fatalf("expected ErrCSNilCheckpoint, got %v", err)
	}

	// Store and defensive copy.
	store.StoreCheckpoint(&StoredCheckpoint{Epoch: 5, Root: csTestHash(0x01)}, false)
	got, _ := store.GetCheckpoint(5)
	got.Epoch = 999
	got2, _ := store.GetCheckpoint(5)
	if got2.Epoch != 5 {
		t.Errorf("store was mutated: epoch=%d, want 5", got2.Epoch)
	}

	// Duplicate rejected.
	if err := store.StoreCheckpoint(&StoredCheckpoint{Epoch: 5, Root: csTestHash(0x01)}, false); err != ErrCSEpochExists {
		t.Fatalf("expected ErrCSEpochExists, got %v", err)
	}

	// Overwrite succeeds.
	store.StoreCheckpoint(&StoredCheckpoint{Epoch: 5, Root: csTestHash(0x02)}, true)
	got3, _ := store.GetCheckpoint(5)
	if got3.Root != csTestHash(0x02) {
		t.Error("overwrite did not update root")
	}

	// Not found.
	if _, err := store.GetCheckpoint(99); err == nil {
		t.Fatal("expected error for missing checkpoint")
	}
}

// TestCSLatestFinalizedAndJustified verifies latest tracking.
func TestCSLatestFinalizedAndJustified(t *testing.T) {
	store := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())

	if store.LatestFinalized() != nil {
		t.Fatal("expected nil finalized initially")
	}
	if store.LatestJustified() != nil {
		t.Fatal("expected nil justified initially")
	}

	store.StoreCheckpoint(&StoredCheckpoint{
		Epoch: 5, Root: csTestHash(0x05), Justified: true, Finalized: true,
	}, false)
	store.StoreCheckpoint(&StoredCheckpoint{
		Epoch: 10, Root: csTestHash(0x0A), Justified: true, Finalized: false,
	}, false)
	store.StoreCheckpoint(&StoredCheckpoint{
		Epoch: 8, Root: csTestHash(0x08), Justified: false, Finalized: true,
	}, false)

	lf := store.LatestFinalized()
	if lf == nil || lf.Epoch != 8 {
		t.Errorf("latest finalized=%v, want epoch 8", lf)
	}

	lj := store.LatestJustified()
	if lj == nil || lj.Epoch != 10 {
		t.Errorf("latest justified=%v, want epoch 10", lj)
	}
}

// TestCSChainValidation verifies chain validation with valid and broken chains.
func TestCSChainValidation(t *testing.T) {
	// Valid chain.
	store := csStoreWithChain(t, 5)
	status := store.ValidateChain()
	if !status.Valid {
		t.Errorf("expected valid chain, got gaps: %v", status.Gaps)
	}
	if status.Length != 5 {
		t.Errorf("length=%d, want 5", status.Length)
	}
	if status.EarliestEpoch != 0 {
		t.Errorf("earliest=%d, want 0", status.EarliestEpoch)
	}
	if status.LatestEpoch != 4 {
		t.Errorf("latest=%d, want 4", status.LatestEpoch)
	}

	// Broken chain: add checkpoint referencing non-existent parent.
	store.StoreCheckpoint(&StoredCheckpoint{
		Epoch:       10,
		Root:        csTestHash(0x10),
		Finalized:   true,
		ParentEpoch: 7, // epoch 7 does not exist
	}, false)

	status = store.ValidateChain()
	if status.Valid {
		t.Error("expected invalid chain with gap")
	}
	if len(status.Gaps) != 1 || status.Gaps[0] != 7 {
		t.Errorf("gaps=%v, want [7]", status.Gaps)
	}
}

// TestCSChainValidationEmpty verifies validation of empty store.
func TestCSChainValidationEmpty(t *testing.T) {
	store := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())
	status := store.ValidateChain()
	if !status.Valid {
		t.Error("empty store should be valid")
	}
	if status.Length != 0 {
		t.Errorf("length=%d, want 0", status.Length)
	}
}

// TestCSWeakSubjectivitySafe verifies WS check passes within window.
func TestCSWeakSubjectivitySafe(t *testing.T) {
	store := NewCheckpointPersistenceStore(CheckpointPersistenceConfig{
		WeakSubjectivityPeriod: 256,
	})
	store.StoreCheckpoint(&StoredCheckpoint{
		Epoch: 100, Root: csTestHash(0x64), Finalized: true,
	}, false)

	ws := store.CheckWeakSubjectivity(200)
	if !ws.Safe {
		t.Error("expected WS safe at epoch 200 with finalized at 100")
	}
	if ws.Distance != 100 {
		t.Errorf("distance=%d, want 100", ws.Distance)
	}
	if ws.MaxAllowed != 256 {
		t.Errorf("maxAllowed=%d, want 256", ws.MaxAllowed)
	}
}

// TestCSWeakSubjectivityUnsafe verifies WS check fails outside window.
func TestCSWeakSubjectivityUnsafe(t *testing.T) {
	store := NewCheckpointPersistenceStore(CheckpointPersistenceConfig{
		WeakSubjectivityPeriod: 100,
	})
	store.StoreCheckpoint(&StoredCheckpoint{
		Epoch: 50, Root: csTestHash(0x32), Finalized: true,
	}, false)

	ws := store.CheckWeakSubjectivity(200)
	if ws.Safe {
		t.Error("expected WS unsafe at epoch 200 with finalized at 50, period 100")
	}
	if ws.Distance != 150 {
		t.Errorf("distance=%d, want 150", ws.Distance)
	}
}

// TestCSWeakSubjectivityNoFinalized verifies WS with no finalized checkpoint.
func TestCSWeakSubjectivityNoFinalized(t *testing.T) {
	store := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())
	ws := store.CheckWeakSubjectivity(100)
	if !ws.Safe {
		t.Error("expected safe when no finalized checkpoint exists (genesis)")
	}
}

// TestCSBootstrapFromCheckpoint verifies checkpoint sync bootstrapping.
func TestCSBootstrapFromCheckpoint(t *testing.T) {
	store := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())
	root := csTestHash(0xBB)

	err := store.BootstrapFromCheckpoint(1000, root)
	if err != nil {
		t.Fatalf("BootstrapFromCheckpoint: %v", err)
	}

	cp, err := store.GetCheckpoint(1000)
	if err != nil {
		t.Fatalf("GetCheckpoint: %v", err)
	}
	if cp.Root != root {
		t.Error("root mismatch after bootstrap")
	}
	if !cp.Finalized || !cp.Justified {
		t.Error("bootstrap checkpoint should be both finalized and justified")
	}

	// Finalized should be updated.
	lf := store.LatestFinalized()
	if lf == nil || lf.Epoch != 1000 {
		t.Errorf("latest finalized=%v, want epoch 1000", lf)
	}

	// Re-bootstrap with same root should be idempotent.
	if err := store.BootstrapFromCheckpoint(1000, root); err != nil {
		t.Fatalf("idempotent bootstrap: %v", err)
	}

	// Bootstrap with conflicting root should fail.
	conflictRoot := csTestHash(0xCC)
	err = store.BootstrapFromCheckpoint(1000, conflictRoot)
	if err == nil {
		t.Fatal("expected error for conflicting bootstrap")
	}
}

// TestCSPruneBeforeEpoch verifies pruning behavior.
func TestCSPruneBeforeEpoch(t *testing.T) {
	store := csStoreWithChain(t, 10)

	if store.Count() != 10 {
		t.Fatalf("count=%d, want 10", store.Count())
	}

	pruned := store.PruneBeforeEpoch(5)
	// Epoch 0 is the finalized latest (epoch 9 actually), but epochs 0-4 are
	// candidates for pruning. The finalized checkpoint is epoch 9, justified
	// is epoch 9 as well, so 0-4 should be pruned.
	if pruned != 5 {
		t.Errorf("pruned=%d, want 5", pruned)
	}

	// Epochs 5-9 should remain.
	for ep := Epoch(5); ep < 10; ep++ {
		if !store.HasCheckpoint(ep) {
			t.Errorf("epoch %d missing after prune", ep)
		}
	}
	for ep := Epoch(0); ep < 5; ep++ {
		if store.HasCheckpoint(ep) {
			t.Errorf("epoch %d should be pruned", ep)
		}
	}
}

// TestCSPruneProtectsFinalized verifies that finalized checkpoints survive pruning.
func TestCSPruneProtectsFinalized(t *testing.T) {
	store := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())

	// Store a finalized checkpoint at epoch 5, justified at 5.
	store.StoreCheckpoint(&StoredCheckpoint{
		Epoch: 5, Root: csTestHash(0x05), Finalized: true, Justified: true,
	}, false)
	// Store non-finalized at 3.
	store.StoreCheckpoint(&StoredCheckpoint{
		Epoch: 3, Root: csTestHash(0x03), Finalized: false, Justified: false,
	}, false)
	// Store non-finalized at 8.
	store.StoreCheckpoint(&StoredCheckpoint{
		Epoch: 8, Root: csTestHash(0x08), Finalized: false, Justified: false,
	}, false)

	pruned := store.PruneBeforeEpoch(7)
	// Epoch 3 should be pruned, epoch 5 protected (finalized).
	if pruned != 1 {
		t.Errorf("pruned=%d, want 1", pruned)
	}
	if !store.HasCheckpoint(5) {
		t.Error("finalized checkpoint at epoch 5 should be protected from pruning")
	}
}

// TestCSAllEpochs verifies epoch listing in sorted order.
func TestCSAllEpochs(t *testing.T) {
	store := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())

	for _, ep := range []Epoch{10, 3, 7, 1, 5} {
		store.StoreCheckpoint(&StoredCheckpoint{Epoch: ep, Root: csTestHash(byte(ep))}, false)
	}

	epochs := store.AllEpochs()
	if len(epochs) != 5 {
		t.Fatalf("len=%d, want 5", len(epochs))
	}
	expected := []Epoch{1, 3, 5, 7, 10}
	for i, ep := range epochs {
		if ep != expected[i] {
			t.Errorf("epochs[%d]=%d, want %d", i, ep, expected[i])
		}
	}
}

// TestCSFinalizedAndJustifiedCheckpoints verifies listing both types.
func TestCSFinalizedAndJustifiedCheckpoints(t *testing.T) {
	store := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())
	store.StoreCheckpoint(&StoredCheckpoint{Epoch: 1, Root: csTestHash(0x01), Finalized: true, Justified: true}, false)
	store.StoreCheckpoint(&StoredCheckpoint{Epoch: 2, Root: csTestHash(0x02), Finalized: false, Justified: false}, false)
	store.StoreCheckpoint(&StoredCheckpoint{Epoch: 3, Root: csTestHash(0x03), Finalized: true, Justified: true}, false)

	finalized := store.FinalizedCheckpoints()
	if len(finalized) != 2 || finalized[0].Epoch != 1 || finalized[1].Epoch != 3 {
		t.Errorf("finalized: got %d entries, want epochs 1,3", len(finalized))
	}
	justified := store.JustifiedCheckpoints()
	if len(justified) != 2 || justified[0].Epoch != 1 || justified[1].Epoch != 3 {
		t.Errorf("justified: got %d entries, want epochs 1,3", len(justified))
	}
}

// TestCSGetCheckpointRange verifies range queries.
func TestCSGetCheckpointRange(t *testing.T) {
	store := csStoreWithChain(t, 10)

	// Range [3, 6] should return 4 checkpoints.
	cps := store.GetCheckpointRange(3, 6)
	if len(cps) != 4 {
		t.Fatalf("range [3,6] count=%d, want 4", len(cps))
	}
	for i, cp := range cps {
		expected := Epoch(3 + i)
		if cp.Epoch != expected {
			t.Errorf("range[%d].Epoch=%d, want %d", i, cp.Epoch, expected)
		}
	}

	// Range with no matches.
	cps = store.GetCheckpointRange(100, 200)
	if len(cps) != 0 {
		t.Errorf("empty range count=%d, want 0", len(cps))
	}
}

// TestCSHasCheckpoint verifies existence checks.
func TestCSHasCheckpoint(t *testing.T) {
	store := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())

	store.StoreCheckpoint(&StoredCheckpoint{Epoch: 42, Root: csTestHash(0x42)}, false)

	if !store.HasCheckpoint(42) {
		t.Error("expected HasCheckpoint(42) = true")
	}
	if store.HasCheckpoint(99) {
		t.Error("expected HasCheckpoint(99) = false")
	}
}

// TestCSConcurrentAccess verifies thread safety.
func TestCSConcurrentAccess(t *testing.T) {
	store := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())
	var wg sync.WaitGroup
	errCh := make(chan error, 200)

	// Concurrent writes.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(ep int) {
			defer wg.Done()
			cp := &StoredCheckpoint{
				Epoch:     Epoch(ep),
				Root:      csTestHash(byte(ep)),
				Finalized: ep%2 == 0,
				Justified: true,
			}
			// Use overwrite=true since concurrent writes may collide.
			if err := store.StoreCheckpoint(cp, true); err != nil {
				errCh <- err
			}
		}(i)
	}

	// Concurrent reads alongside writes.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(ep int) {
			defer wg.Done()
			store.GetCheckpoint(Epoch(ep))
			store.LatestFinalized()
			store.LatestJustified()
			store.Count()
			store.ValidateChain()
			store.CheckWeakSubjectivity(Epoch(ep + 100))
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}

	// Store should have checkpoints.
	if store.Count() == 0 {
		t.Error("expected non-zero checkpoint count after concurrent writes")
	}
}

// TestCSAutoMaxStored verifies automatic pruning when maxStored is set.
func TestCSAutoMaxStored(t *testing.T) {
	store := NewCheckpointPersistenceStore(CheckpointPersistenceConfig{
		WeakSubjectivityPeriod: CSWeakSubjectivityPeriod,
		MaxStoredCheckpoints:   5,
	})

	for i := 0; i < 10; i++ {
		cp := &StoredCheckpoint{
			Epoch: Epoch(i),
			Root:  csTestHash(byte(i)),
		}
		store.StoreCheckpoint(cp, false)
	}

	// Should not exceed maxStored significantly.
	count := store.Count()
	if count > 6 { // some tolerance for finalized/justified protection
		t.Errorf("count=%d, expected <= 6 with maxStored=5", count)
	}
}

// TestCSWeakSubjectivityBoundaryAndEdge verifies exact boundary and edge cases.
func TestCSWeakSubjectivityBoundaryAndEdge(t *testing.T) {
	store := NewCheckpointPersistenceStore(CheckpointPersistenceConfig{WeakSubjectivityPeriod: 100})
	store.StoreCheckpoint(&StoredCheckpoint{Epoch: 50, Root: csTestHash(0x32), Finalized: true}, false)

	// Exactly at boundary (distance=100): safe.
	if ws := store.CheckWeakSubjectivity(150); !ws.Safe {
		t.Error("expected safe at exact WS boundary")
	}
	// One past boundary (distance=101): unsafe.
	if ws := store.CheckWeakSubjectivity(151); ws.Safe {
		t.Error("expected unsafe one past WS boundary")
	}

	// Current epoch before finalized: safe.
	store2 := NewCheckpointPersistenceStore(DefaultCheckpointPersistenceConfig())
	store2.StoreCheckpoint(&StoredCheckpoint{Epoch: 100, Root: csTestHash(0x64), Finalized: true}, false)
	ws := store2.CheckWeakSubjectivity(50)
	if !ws.Safe || ws.Distance != 0 {
		t.Errorf("expected safe with distance=0, got safe=%v dist=%d", ws.Safe, ws.Distance)
	}
}
