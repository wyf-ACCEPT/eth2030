package state

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func newTestEndgameDB(t *testing.T) *EndgameStateDB {
	t.Helper()
	memDB := NewMemoryStateDB()
	edb, err := NewEndgameStateDB(memDB)
	if err != nil {
		t.Fatalf("NewEndgameStateDB: %v", err)
	}
	return edb
}

func testRoot(n byte) types.Hash {
	var h types.Hash
	h[0] = n
	h[31] = n
	return h
}

func TestNewEndgameStateDB(t *testing.T) {
	memDB := NewMemoryStateDB()
	edb, err := NewEndgameStateDB(memDB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edb.Underlying() != memDB {
		t.Error("Underlying() does not match")
	}
}

func TestNewEndgameStateDBNilUnderlying(t *testing.T) {
	_, err := NewEndgameStateDB(nil)
	if err != ErrEndgameNilStateDB {
		t.Errorf("error = %v, want %v", err, ErrEndgameNilStateDB)
	}
}

func TestMarkFinalized(t *testing.T) {
	edb := newTestEndgameDB(t)
	root := testRoot(1)

	err := edb.MarkFinalized(root, 100)
	if err != nil {
		t.Fatalf("MarkFinalized: %v", err)
	}

	if !edb.IsFinalized(root) {
		t.Error("root should be finalized")
	}
	if edb.GetFinalizedRoot() != root {
		t.Error("GetFinalizedRoot mismatch")
	}
	if edb.FinalizedSlot() != 100 {
		t.Errorf("FinalizedSlot = %d, want 100", edb.FinalizedSlot())
	}
}

func TestMarkFinalizedZeroRoot(t *testing.T) {
	edb := newTestEndgameDB(t)
	err := edb.MarkFinalized(types.Hash{}, 100)
	if err != ErrEndgameZeroRoot {
		t.Errorf("error = %v, want %v", err, ErrEndgameZeroRoot)
	}
}

func TestMarkFinalizedAlreadyFinalized(t *testing.T) {
	edb := newTestEndgameDB(t)
	root := testRoot(2)

	_ = edb.MarkFinalized(root, 100)
	err := edb.MarkFinalized(root, 101)
	if err != ErrEndgameAlreadyFinalized {
		t.Errorf("error = %v, want %v", err, ErrEndgameAlreadyFinalized)
	}
}

func TestMarkFinalizedSlotRegression(t *testing.T) {
	edb := newTestEndgameDB(t)

	_ = edb.MarkFinalized(testRoot(1), 200)
	err := edb.MarkFinalized(testRoot(2), 100)
	if err != ErrEndgameSlotRegression {
		t.Errorf("error = %v, want %v", err, ErrEndgameSlotRegression)
	}
}

func TestMarkFinalizedMonotonic(t *testing.T) {
	edb := newTestEndgameDB(t)

	for i := byte(1); i <= 5; i++ {
		err := edb.MarkFinalized(testRoot(i), uint64(i)*10)
		if err != nil {
			t.Fatalf("MarkFinalized(%d): %v", i, err)
		}
	}

	if edb.GetFinalizedRoot() != testRoot(5) {
		t.Error("GetFinalizedRoot should be the last finalized root")
	}
	if edb.FinalizedSlot() != 50 {
		t.Errorf("FinalizedSlot = %d, want 50", edb.FinalizedSlot())
	}

	for i := byte(1); i <= 5; i++ {
		if !edb.IsFinalized(testRoot(i)) {
			t.Errorf("root %d should be finalized", i)
		}
	}
}

func TestMarkFinalizedSameSlot(t *testing.T) {
	edb := newTestEndgameDB(t)

	// Same slot for different roots should be allowed.
	err := edb.MarkFinalized(testRoot(1), 100)
	if err != nil {
		t.Fatal(err)
	}
	err = edb.MarkFinalized(testRoot(2), 100)
	if err != nil {
		t.Fatalf("same slot should be allowed: %v", err)
	}
}

func TestIsFinalized(t *testing.T) {
	edb := newTestEndgameDB(t)

	if edb.IsFinalized(testRoot(1)) {
		t.Error("should not be finalized before MarkFinalized")
	}

	_ = edb.MarkFinalized(testRoot(1), 10)
	if !edb.IsFinalized(testRoot(1)) {
		t.Error("should be finalized after MarkFinalized")
	}

	if edb.IsFinalized(testRoot(99)) {
		t.Error("unknown root should not be finalized")
	}
}

func TestGetFinalizedRootEmpty(t *testing.T) {
	edb := newTestEndgameDB(t)
	root := edb.GetFinalizedRoot()
	if !root.IsZero() {
		t.Errorf("GetFinalizedRoot on empty should be zero, got %s", root.Hex())
	}
}

func TestFinalizedSlotEmpty(t *testing.T) {
	edb := newTestEndgameDB(t)
	if edb.FinalizedSlot() != 0 {
		t.Errorf("FinalizedSlot on empty = %d, want 0", edb.FinalizedSlot())
	}
}

func TestRevertToFinalized(t *testing.T) {
	edb := newTestEndgameDB(t)

	// Finalize a state.
	_ = edb.MarkFinalized(testRoot(1), 10)

	// Add some pending roots.
	edb.AddPendingRoot(testRoot(10), 11)
	edb.AddPendingRoot(testRoot(11), 12)

	if edb.PendingCount() != 2 {
		t.Fatalf("PendingCount = %d, want 2", edb.PendingCount())
	}

	// Revert to finalized.
	err := edb.RevertToFinalized()
	if err != nil {
		t.Fatalf("RevertToFinalized: %v", err)
	}

	// Pending should be cleared.
	if edb.PendingCount() != 0 {
		t.Errorf("PendingCount after revert = %d, want 0", edb.PendingCount())
	}
	roots := edb.PendingStateRoots()
	if len(roots) != 0 {
		t.Errorf("PendingStateRoots after revert has %d entries", len(roots))
	}
}

func TestRevertToFinalizedEmpty(t *testing.T) {
	edb := newTestEndgameDB(t)
	err := edb.RevertToFinalized()
	if err != ErrEndgameNoFinalized {
		t.Errorf("error = %v, want %v", err, ErrEndgameNoFinalized)
	}
}

func TestAddPendingRoot(t *testing.T) {
	edb := newTestEndgameDB(t)

	edb.AddPendingRoot(testRoot(10), 100)
	edb.AddPendingRoot(testRoot(11), 101)
	edb.AddPendingRoot(testRoot(12), 102)

	roots := edb.PendingStateRoots()
	if len(roots) != 3 {
		t.Fatalf("PendingStateRoots len = %d, want 3", len(roots))
	}
	if roots[0] != testRoot(10) || roots[1] != testRoot(11) || roots[2] != testRoot(12) {
		t.Error("PendingStateRoots order mismatch")
	}
}

func TestAddPendingRootDuplicate(t *testing.T) {
	edb := newTestEndgameDB(t)

	edb.AddPendingRoot(testRoot(10), 100)
	edb.AddPendingRoot(testRoot(10), 100) // duplicate, should be no-op.

	if edb.PendingCount() != 1 {
		t.Errorf("PendingCount = %d, want 1 after duplicate add", edb.PendingCount())
	}
}

func TestAddPendingRootZero(t *testing.T) {
	edb := newTestEndgameDB(t)
	edb.AddPendingRoot(types.Hash{}, 100)

	if edb.PendingCount() != 0 {
		t.Error("zero root should not be added to pending")
	}
}

func TestAddPendingRootAlreadyFinalized(t *testing.T) {
	edb := newTestEndgameDB(t)
	root := testRoot(1)

	_ = edb.MarkFinalized(root, 10)
	edb.AddPendingRoot(root, 11) // already finalized, should be no-op.

	if edb.PendingCount() != 0 {
		t.Error("finalized root should not be added to pending")
	}
}

func TestMarkFinalizedRemovesPending(t *testing.T) {
	edb := newTestEndgameDB(t)

	edb.AddPendingRoot(testRoot(10), 100)
	edb.AddPendingRoot(testRoot(11), 101)
	edb.AddPendingRoot(testRoot(12), 102)

	if edb.PendingCount() != 3 {
		t.Fatalf("PendingCount = %d, want 3", edb.PendingCount())
	}

	// Finalize the middle one.
	_ = edb.MarkFinalized(testRoot(11), 101)

	if edb.PendingCount() != 2 {
		t.Errorf("PendingCount after finalize = %d, want 2", edb.PendingCount())
	}

	roots := edb.PendingStateRoots()
	for _, r := range roots {
		if r == testRoot(11) {
			t.Error("finalized root should have been removed from pending")
		}
	}
}

func TestGarbageCollectPreFinality(t *testing.T) {
	edb := newTestEndgameDB(t)

	// Finalize several roots at different slots.
	for i := byte(1); i <= 10; i++ {
		_ = edb.MarkFinalized(testRoot(i), uint64(i)*10)
	}

	// Keep only the last 30 slots. Current finalized is at slot 100.
	// Cutoff = 100 - 30 = 70. Roots at slots 10-60 should be removed.
	removed := edb.GarbageCollectPreFinality(30)
	if removed != 6 { // slots 10,20,30,40,50,60
		t.Errorf("removed = %d, want 6", removed)
	}

	// The current finalized root should still be accessible.
	if !edb.IsFinalized(testRoot(10)) {
		t.Error("current finalized root should still exist")
	}

	// Old roots should have been removed from finalized list but we only
	// check that the current root is preserved.
	history := edb.FinalizedHistory()
	for _, h := range history {
		if h.Slot < 70 && h.Root != testRoot(10) {
			t.Errorf("slot %d should have been garbage collected", h.Slot)
		}
	}
}

func TestGarbageCollectPreFinalityEmpty(t *testing.T) {
	edb := newTestEndgameDB(t)
	removed := edb.GarbageCollectPreFinality(100)
	if removed != 0 {
		t.Errorf("removed = %d, want 0 for empty state", removed)
	}
}

func TestGarbageCollectPreFinalityKeepAll(t *testing.T) {
	edb := newTestEndgameDB(t)

	for i := byte(1); i <= 5; i++ {
		_ = edb.MarkFinalized(testRoot(i), uint64(i))
	}

	// keepSlots >= currentFinalizedSlot means keep everything.
	removed := edb.GarbageCollectPreFinality(1000)
	if removed != 0 {
		t.Errorf("removed = %d, want 0 when keeping all", removed)
	}
}

func TestFinalizedHistory(t *testing.T) {
	edb := newTestEndgameDB(t)

	_ = edb.MarkFinalized(testRoot(3), 300)
	_ = edb.MarkFinalized(testRoot(1), 300) // same slot
	_ = edb.MarkFinalized(testRoot(2), 400)

	history := edb.FinalizedHistory()
	if len(history) != 3 {
		t.Fatalf("history len = %d, want 3", len(history))
	}

	// Should be sorted by slot.
	for i := 1; i < len(history); i++ {
		if history[i].Slot < history[i-1].Slot {
			t.Error("history is not sorted by slot")
		}
	}
}

func TestComputeFinalityDigest(t *testing.T) {
	edb := newTestEndgameDB(t)

	// Empty state: zero hash.
	digest := edb.ComputeFinalityDigest()
	if !digest.IsZero() {
		t.Error("digest should be zero for empty state")
	}

	_ = edb.MarkFinalized(testRoot(1), 10)
	digest1 := edb.ComputeFinalityDigest()
	if digest1.IsZero() {
		t.Error("digest should not be zero after finalization")
	}

	_ = edb.MarkFinalized(testRoot(2), 20)
	digest2 := edb.ComputeFinalityDigest()
	if digest2.IsZero() {
		t.Error("digest should not be zero")
	}
	if digest1 == digest2 {
		t.Error("digest should change after new finalization")
	}

	// Determinism: same state should produce same digest.
	digest2b := edb.ComputeFinalityDigest()
	if digest2 != digest2b {
		t.Error("digest is not deterministic")
	}
}

func TestPendingStateRootsOrder(t *testing.T) {
	edb := newTestEndgameDB(t)

	for i := byte(1); i <= 10; i++ {
		edb.AddPendingRoot(testRoot(i), uint64(i))
	}

	roots := edb.PendingStateRoots()
	if len(roots) != 10 {
		t.Fatalf("len = %d, want 10", len(roots))
	}

	for i := byte(1); i <= 10; i++ {
		if roots[i-1] != testRoot(i) {
			t.Errorf("roots[%d] = %s, want testRoot(%d)", i-1, roots[i-1].Hex(), i)
		}
	}
}

func TestEndgameFullLifecycle(t *testing.T) {
	edb := newTestEndgameDB(t)

	// Phase 1: Add pending roots.
	for i := byte(1); i <= 5; i++ {
		edb.AddPendingRoot(testRoot(i), uint64(i)*10)
	}
	if edb.PendingCount() != 5 {
		t.Fatalf("PendingCount = %d, want 5", edb.PendingCount())
	}

	// Phase 2: Finalize some.
	_ = edb.MarkFinalized(testRoot(1), 10)
	_ = edb.MarkFinalized(testRoot(2), 20)
	if edb.PendingCount() != 3 {
		t.Errorf("PendingCount = %d, want 3 after finalizing 2", edb.PendingCount())
	}

	// Phase 3: Check state.
	if !edb.IsFinalized(testRoot(1)) {
		t.Error("root 1 should be finalized")
	}
	if !edb.IsFinalized(testRoot(2)) {
		t.Error("root 2 should be finalized")
	}
	if edb.IsFinalized(testRoot(3)) {
		t.Error("root 3 should not be finalized yet")
	}

	// Phase 4: Finalize remaining.
	_ = edb.MarkFinalized(testRoot(3), 30)
	_ = edb.MarkFinalized(testRoot(4), 40)
	_ = edb.MarkFinalized(testRoot(5), 50)
	if edb.PendingCount() != 0 {
		t.Errorf("PendingCount = %d, want 0", edb.PendingCount())
	}

	// Phase 5: GC old entries.
	removed := edb.GarbageCollectPreFinality(20)
	if removed < 1 {
		t.Errorf("GC should have removed at least 1 entry, removed %d", removed)
	}

	// Current finalized should still be accessible.
	if edb.GetFinalizedRoot() != testRoot(5) {
		t.Error("current finalized root mismatch after GC")
	}
	if edb.FinalizedSlot() != 50 {
		t.Errorf("FinalizedSlot = %d, want 50", edb.FinalizedSlot())
	}
}

func TestEndgameConcurrentAccess(t *testing.T) {
	edb := newTestEndgameDB(t)
	done := make(chan struct{})

	// Writer: finalize roots.
	go func() {
		for i := byte(1); i <= 50; i++ {
			_ = edb.MarkFinalized(testRoot(i), uint64(i))
		}
		close(done)
	}()

	// Reader: check finalization status concurrently.
	for i := 0; i < 100; i++ {
		_ = edb.IsFinalized(testRoot(byte(i % 50)))
		_ = edb.GetFinalizedRoot()
		_ = edb.FinalizedSlot()
		_ = edb.PendingStateRoots()
	}

	<-done
}
