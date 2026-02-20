package trie

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func rootHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestStatePruner_NewDefaults(t *testing.T) {
	sp := NewStatePruner(0) // should default to 128
	if sp.MaxRecent() != 128 {
		t.Fatalf("default maxRecent = %d, want 128", sp.MaxRecent())
	}
	if sp.WindowSize() != 0 {
		t.Fatalf("initial window = %d, want 0", sp.WindowSize())
	}
}

func TestStatePruner_AddRoot(t *testing.T) {
	sp := NewStatePruner(5)

	for i := uint64(1); i <= 5; i++ {
		evicted := sp.AddRoot(i, rootHash(byte(i)))
		if len(evicted) != 0 {
			t.Fatalf("block %d: unexpected eviction", i)
		}
	}

	if sp.WindowSize() != 5 {
		t.Fatalf("window = %d, want 5", sp.WindowSize())
	}

	// Adding 6th should evict the oldest.
	evicted := sp.AddRoot(6, rootHash(6))
	if len(evicted) != 1 {
		t.Fatalf("expected 1 evicted, got %d", len(evicted))
	}
	if evicted[0].Block != 1 {
		t.Fatalf("evicted block = %d, want 1", evicted[0].Block)
	}
}

func TestStatePruner_MarkAlive(t *testing.T) {
	sp := NewStatePruner(3)

	root := rootHash(0xAA)
	sp.MarkAlive(root)

	if !sp.IsAlive(root) {
		t.Fatal("marked root should be alive")
	}

	sp.UnmarkAlive(root)
	if sp.IsAlive(root) {
		t.Fatal("unmarked root should not be alive")
	}
}

func TestStatePruner_IsAliveFromWindow(t *testing.T) {
	sp := NewStatePruner(10)
	root := rootHash(0x01)
	sp.AddRoot(1, root)

	if !sp.IsAlive(root) {
		t.Fatal("root in recent window should be alive")
	}

	unknownRoot := rootHash(0xFF)
	if sp.IsAlive(unknownRoot) {
		t.Fatal("unknown root should not be alive")
	}
}

func TestStatePruner_Prune(t *testing.T) {
	sp := NewStatePruner(100)

	// Add 10 roots.
	for i := uint64(1); i <= 10; i++ {
		sp.AddRoot(i, rootHash(byte(i)))
	}

	// Prune keeping only the last 3.
	pruned := sp.Prune(3)
	if len(pruned) != 7 {
		t.Fatalf("pruned %d, want 7", len(pruned))
	}

	if sp.WindowSize() != 3 {
		t.Fatalf("window after prune = %d, want 3", sp.WindowSize())
	}

	// Verify remaining roots are blocks 8, 9, 10.
	recent := sp.RecentRoots()
	for i, sr := range recent {
		expected := uint64(8 + i)
		if sr.Block != expected {
			t.Errorf("recent[%d].Block = %d, want %d", i, sr.Block, expected)
		}
	}
}

func TestStatePruner_PrunePreservesAlive(t *testing.T) {
	sp := NewStatePruner(100)

	// Add 10 roots.
	for i := uint64(1); i <= 10; i++ {
		sp.AddRoot(i, rootHash(byte(i)))
	}

	// Mark root at block 2 as alive.
	sp.MarkAlive(rootHash(2))

	// Prune keeping only last 3 (blocks 8, 9, 10).
	pruned := sp.Prune(3)

	// Should prune 6 (not 7), because block 2 is alive.
	if len(pruned) != 6 {
		t.Fatalf("pruned %d, want 6", len(pruned))
	}

	// Block 2 should still be in the window.
	if !sp.IsAlive(rootHash(2)) {
		t.Fatal("alive root should survive pruning")
	}

	// Window should be 4: block 2 (alive) + blocks 8, 9, 10.
	if sp.WindowSize() != 4 {
		t.Fatalf("window = %d, want 4", sp.WindowSize())
	}
}

func TestStatePruner_PruneEmptyNoop(t *testing.T) {
	sp := NewStatePruner(10)
	pruned := sp.Prune(5)
	if len(pruned) != 0 {
		t.Fatalf("prune empty: got %d, want 0", len(pruned))
	}
}

func TestStatePruner_PruneKeepAll(t *testing.T) {
	sp := NewStatePruner(10)
	sp.AddRoot(1, rootHash(1))
	sp.AddRoot(2, rootHash(2))

	// keepRecent >= window size should prune nothing.
	pruned := sp.Prune(10)
	if len(pruned) != 0 {
		t.Fatalf("prune keep all: got %d, want 0", len(pruned))
	}
}

func TestStatePruner_RetainedRoots(t *testing.T) {
	sp := NewStatePruner(10)

	sp.AddRoot(1, rootHash(1))
	sp.AddRoot(2, rootHash(2))
	sp.MarkAlive(rootHash(0xBB))

	retained := sp.RetainedRoots()
	if len(retained) != 3 {
		t.Fatalf("retained = %d, want 3", len(retained))
	}

	found := make(map[types.Hash]bool)
	for _, h := range retained {
		found[h] = true
	}
	if !found[rootHash(1)] || !found[rootHash(2)] || !found[rootHash(0xBB)] {
		t.Fatal("retained roots missing expected hashes")
	}
}

func TestStatePruner_RetainedRootsDedup(t *testing.T) {
	sp := NewStatePruner(10)

	root := rootHash(0x01)
	sp.AddRoot(1, root)
	sp.MarkAlive(root) // same root in both sets

	retained := sp.RetainedRoots()
	if len(retained) != 1 {
		t.Fatalf("retained = %d, want 1 (deduped)", len(retained))
	}
}

func TestStatePruner_HeadRoot(t *testing.T) {
	sp := NewStatePruner(10)

	root, block := sp.HeadRoot()
	if root != (types.Hash{}) || block != 0 {
		t.Fatal("empty pruner should return zero head")
	}

	sp.AddRoot(5, rootHash(5))
	sp.AddRoot(10, rootHash(10))

	root, block = sp.HeadRoot()
	if root != rootHash(10) || block != 10 {
		t.Fatalf("head root = %s block %d, want %s block 10", root.Hex(), block, rootHash(10).Hex())
	}
}

func TestStatePruner_PrunedTotal(t *testing.T) {
	sp := NewStatePruner(3)

	for i := uint64(1); i <= 6; i++ {
		sp.AddRoot(i, rootHash(byte(i)))
	}
	// Adding 6 roots with window of 3 evicts 3.
	if sp.PrunedTotal() != 3 {
		t.Fatalf("pruned total = %d, want 3", sp.PrunedTotal())
	}

	sp.Prune(1)
	// Prune(1) keeps 1 of the 3, pruning 2 more.
	if sp.PrunedTotal() != 5 {
		t.Fatalf("pruned total after Prune = %d, want 5", sp.PrunedTotal())
	}
}

func TestStatePruner_Stop(t *testing.T) {
	sp := NewStatePruner(10)
	sp.AddRoot(1, rootHash(1))
	sp.Stop()

	// Operations should be no-ops after stop.
	evicted := sp.AddRoot(2, rootHash(2))
	if evicted != nil {
		t.Fatal("AddRoot after stop should return nil")
	}
	if sp.WindowSize() != 1 {
		t.Fatalf("window should not change after stop: %d", sp.WindowSize())
	}
}

func TestStatePruner_Reset(t *testing.T) {
	sp := NewStatePruner(5)
	sp.AddRoot(1, rootHash(1))
	sp.MarkAlive(rootHash(0xCC))
	sp.Stop()

	sp.Reset()

	if sp.WindowSize() != 0 {
		t.Fatal("window should be 0 after reset")
	}
	if len(sp.AliveRoots()) != 0 {
		t.Fatal("alive roots should be empty after reset")
	}
	if sp.PrunedTotal() != 0 {
		t.Fatal("pruned total should be 0 after reset")
	}

	// Should be usable again.
	evicted := sp.AddRoot(1, rootHash(1))
	if evicted != nil {
		t.Fatal("should be able to add after reset")
	}
}

func TestStatePruner_Stats(t *testing.T) {
	sp := NewStatePruner(5)
	sp.AddRoot(1, rootHash(1))
	sp.AddRoot(2, rootHash(2))
	sp.MarkAlive(rootHash(0xDD))

	stats := sp.Stats()
	if stats.WindowSize != 2 {
		t.Fatalf("WindowSize = %d, want 2", stats.WindowSize)
	}
	if stats.AliveCount != 1 {
		t.Fatalf("AliveCount = %d, want 1", stats.AliveCount)
	}
	if stats.HeadBlock != 2 {
		t.Fatalf("HeadBlock = %d, want 2", stats.HeadBlock)
	}
}

func TestStatePruner_SortedInsertion(t *testing.T) {
	sp := NewStatePruner(10)

	// Insert out of block order; should be sorted internally.
	sp.AddRoot(5, rootHash(5))
	sp.AddRoot(1, rootHash(1))
	sp.AddRoot(3, rootHash(3))

	recent := sp.RecentRoots()
	if len(recent) != 3 {
		t.Fatalf("window = %d, want 3", len(recent))
	}
	if recent[0].Block != 1 || recent[1].Block != 3 || recent[2].Block != 5 {
		t.Fatalf("not sorted: %v", recent)
	}
}

func TestStatePruner_Concurrent(t *testing.T) {
	sp := NewStatePruner(1000)

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				block := uint64(offset*100 + i)
				sp.AddRoot(block, rootHash(byte(block%256)))
			}
		}(g)
	}
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				sp.IsAlive(rootHash(byte(i)))
				sp.RetainedRoots()
				sp.Stats()
			}
		}()
	}
	wg.Wait()

	if sp.WindowSize() != 400 {
		t.Fatalf("window = %d, want 400", sp.WindowSize())
	}
}

func TestStatePruner_AliveRoots(t *testing.T) {
	sp := NewStatePruner(10)
	sp.MarkAlive(rootHash(1))
	sp.MarkAlive(rootHash(2))
	sp.MarkAlive(rootHash(3))

	alive := sp.AliveRoots()
	if len(alive) != 3 {
		t.Fatalf("alive = %d, want 3", len(alive))
	}
}
