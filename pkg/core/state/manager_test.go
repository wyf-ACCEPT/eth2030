package state

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewStateManager(t *testing.T) {
	m := NewStateManager(StateManagerConfig{
		CacheSize:        100,
		JournalLimit:     50,
		SnapshotInterval: 10,
	})
	if m == nil {
		t.Fatal("expected non-nil state manager")
	}
	if m.config.CacheSize != 100 {
		t.Fatalf("expected cache size 100, got %d", m.config.CacheSize)
	}
	if m.config.JournalLimit != 50 {
		t.Fatalf("expected journal limit 50, got %d", m.config.JournalLimit)
	}
	if m.config.SnapshotInterval != 10 {
		t.Fatalf("expected snapshot interval 10, got %d", m.config.SnapshotInterval)
	}
}

func TestSetAndGetRoot(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})
	if !m.GetRoot().IsZero() {
		t.Fatal("expected initial root to be zero")
	}

	root := types.HexToHash("0xdeadbeef")
	m.SetRoot(root)
	if m.GetRoot() != root {
		t.Fatalf("expected root %s, got %s", root, m.GetRoot())
	}
}

func TestAddAndGetJournalEntry(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})

	root1 := types.HexToHash("0x01")
	root2 := types.HexToHash("0x02")

	m.AddJournalEntry(100, root1)
	m.AddJournalEntry(101, root2)

	got := m.GetJournalEntry(100)
	if got == nil {
		t.Fatal("expected journal entry for block 100")
	}
	if *got != root1 {
		t.Fatalf("expected root %s, got %s", root1, *got)
	}

	got = m.GetJournalEntry(101)
	if got == nil {
		t.Fatal("expected journal entry for block 101")
	}
	if *got != root2 {
		t.Fatalf("expected root %s, got %s", root2, *got)
	}

	// Non-existent block.
	if m.GetJournalEntry(999) != nil {
		t.Fatal("expected nil for non-existent block")
	}
}

func TestJournalSize(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})
	if m.JournalSize() != 0 {
		t.Fatal("expected empty journal")
	}

	m.AddJournalEntry(1, types.HexToHash("0x01"))
	m.AddJournalEntry(2, types.HexToHash("0x02"))
	m.AddJournalEntry(3, types.HexToHash("0x03"))

	if m.JournalSize() != 3 {
		t.Fatalf("expected journal size 3, got %d", m.JournalSize())
	}
}

func TestJournalLimit(t *testing.T) {
	m := NewStateManager(StateManagerConfig{JournalLimit: 3})

	m.AddJournalEntry(1, types.HexToHash("0x01"))
	m.AddJournalEntry(2, types.HexToHash("0x02"))
	m.AddJournalEntry(3, types.HexToHash("0x03"))

	if m.JournalSize() != 3 {
		t.Fatalf("expected journal size 3, got %d", m.JournalSize())
	}

	// Adding a 4th entry should prune the oldest.
	m.AddJournalEntry(4, types.HexToHash("0x04"))
	if m.JournalSize() != 3 {
		t.Fatalf("expected journal size 3 after limit, got %d", m.JournalSize())
	}

	// Block 1 should be pruned.
	if m.GetJournalEntry(1) != nil {
		t.Fatal("expected block 1 to be pruned")
	}

	// Blocks 2, 3, 4 should still exist.
	for _, bn := range []uint64{2, 3, 4} {
		if m.GetJournalEntry(bn) == nil {
			t.Fatalf("expected block %d to exist", bn)
		}
	}
}

func TestTakeAndRestoreSnapshot(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})

	root1 := types.HexToHash("0xaaaa")
	m.SetRoot(root1)
	snapID := m.TakeSnapshot()
	if snapID.IsZero() {
		t.Fatal("expected non-zero snapshot ID")
	}

	// Change root.
	root2 := types.HexToHash("0xbbbb")
	m.SetRoot(root2)
	if m.GetRoot() != root2 {
		t.Fatalf("expected root %s, got %s", root2, m.GetRoot())
	}

	// Restore snapshot.
	err := m.RestoreSnapshot(snapID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.GetRoot() != root1 {
		t.Fatalf("expected restored root %s, got %s", root1, m.GetRoot())
	}
}

func TestRestoreSnapshot_NotFound(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})
	err := m.RestoreSnapshot(types.HexToHash("0xdeadbeef"))
	if err != ErrSnapshotNotFound {
		t.Fatalf("expected ErrSnapshotNotFound, got %v", err)
	}
}

func TestMultipleSnapshots(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})

	root1 := types.HexToHash("0x01")
	root2 := types.HexToHash("0x02")
	root3 := types.HexToHash("0x03")

	m.SetRoot(root1)
	snap1 := m.TakeSnapshot()

	m.SetRoot(root2)
	snap2 := m.TakeSnapshot()

	m.SetRoot(root3)

	// Restore to snap1.
	if err := m.RestoreSnapshot(snap1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.GetRoot() != root1 {
		t.Fatalf("expected root1 %s, got %s", root1, m.GetRoot())
	}

	// Restore to snap2.
	if err := m.RestoreSnapshot(snap2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.GetRoot() != root2 {
		t.Fatalf("expected root2 %s, got %s", root2, m.GetRoot())
	}
}

func TestPruneJournal(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})

	for i := uint64(1); i <= 10; i++ {
		m.AddJournalEntry(i, types.HexToHash("0x01"))
	}

	m.PruneJournal(5)
	if m.JournalSize() != 5 {
		t.Fatalf("expected journal size 5, got %d", m.JournalSize())
	}

	// Blocks 1-5 should be pruned.
	for i := uint64(1); i <= 5; i++ {
		if m.GetJournalEntry(i) != nil {
			t.Fatalf("expected block %d to be pruned", i)
		}
	}

	// Blocks 6-10 should remain.
	for i := uint64(6); i <= 10; i++ {
		if m.GetJournalEntry(i) == nil {
			t.Fatalf("expected block %d to exist", i)
		}
	}
}

func TestPruneJournal_KeepMoreThanExists(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})
	m.AddJournalEntry(1, types.HexToHash("0x01"))
	m.AddJournalEntry(2, types.HexToHash("0x02"))

	m.PruneJournal(100) // keep 100 but only have 2
	if m.JournalSize() != 2 {
		t.Fatalf("expected journal size 2, got %d", m.JournalSize())
	}
}

func TestPruneJournal_NegativeKeepLast(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})
	m.AddJournalEntry(1, types.HexToHash("0x01"))
	m.AddJournalEntry(2, types.HexToHash("0x02"))

	m.PruneJournal(-1) // negative means keep 0
	if m.JournalSize() != 0 {
		t.Fatalf("expected journal size 0, got %d", m.JournalSize())
	}
}

func TestRevertToBlock(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})

	root1 := types.HexToHash("0x01")
	root2 := types.HexToHash("0x02")
	root3 := types.HexToHash("0x03")

	m.AddJournalEntry(100, root1)
	m.AddJournalEntry(101, root2)
	m.AddJournalEntry(102, root3)
	m.SetRoot(root3)

	// Revert to block 101.
	reverted, err := m.RevertToBlock(101)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *reverted != root2 {
		t.Fatalf("expected reverted root %s, got %s", root2, *reverted)
	}
	if m.GetRoot() != root2 {
		t.Fatalf("expected current root %s, got %s", root2, m.GetRoot())
	}

	// Block 102 should be pruned from journal.
	if m.GetJournalEntry(102) != nil {
		t.Fatal("expected block 102 to be pruned after revert")
	}

	// Block 100 and 101 should remain.
	if m.GetJournalEntry(100) == nil {
		t.Fatal("expected block 100 to remain")
	}
	if m.GetJournalEntry(101) == nil {
		t.Fatal("expected block 101 to remain")
	}

	if m.JournalSize() != 2 {
		t.Fatalf("expected journal size 2, got %d", m.JournalSize())
	}
}

func TestRevertToBlock_NotFound(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})
	m.AddJournalEntry(100, types.HexToHash("0x01"))

	_, err := m.RevertToBlock(999)
	if err == nil {
		t.Fatal("expected error for non-existent block")
	}
}

func TestLatestBlock(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})
	if m.LatestBlock() != 0 {
		t.Fatal("expected 0 for empty journal")
	}

	m.AddJournalEntry(50, types.HexToHash("0x01"))
	m.AddJournalEntry(100, types.HexToHash("0x02"))
	m.AddJournalEntry(75, types.HexToHash("0x03"))

	if m.LatestBlock() != 100 {
		t.Fatalf("expected latest block 100, got %d", m.LatestBlock())
	}
}

func TestBlockNumbers(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})

	m.AddJournalEntry(50, types.HexToHash("0x01"))
	m.AddJournalEntry(100, types.HexToHash("0x02"))
	m.AddJournalEntry(75, types.HexToHash("0x03"))

	blocks := m.BlockNumbers()
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if blocks[0] != 50 || blocks[1] != 75 || blocks[2] != 100 {
		t.Fatalf("expected [50, 75, 100], got %v", blocks)
	}
}

func TestManagerConcurrentAccess(t *testing.T) {
	m := NewStateManager(StateManagerConfig{JournalLimit: 100})

	var wg sync.WaitGroup
	// Writers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(base uint64) {
			defer wg.Done()
			for j := uint64(0); j < 5; j++ {
				bn := base*5 + j
				root := types.HexToHash("0x01")
				root[0] = byte(bn)
				m.AddJournalEntry(bn, root)
			}
		}(uint64(i))
	}

	// Readers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.GetRoot()
			m.JournalSize()
			m.LatestBlock()
			m.BlockNumbers()
		}()
	}

	// Snapshot takers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			root := types.HexToHash("0xff")
			root[1] = byte(n)
			m.SetRoot(root)
			id := m.TakeSnapshot()
			_ = m.RestoreSnapshot(id)
		}(i)
	}

	wg.Wait()

	// Verify no panics and journal is within limits.
	if m.JournalSize() > 100 {
		t.Fatalf("journal exceeded limit: %d", m.JournalSize())
	}
}

func TestRevertToBlock_ThenAddMore(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})

	m.AddJournalEntry(1, types.HexToHash("0x01"))
	m.AddJournalEntry(2, types.HexToHash("0x02"))
	m.AddJournalEntry(3, types.HexToHash("0x03"))

	// Revert to block 1.
	_, err := m.RevertToBlock(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.JournalSize() != 1 {
		t.Fatalf("expected journal size 1, got %d", m.JournalSize())
	}

	// Add new entries after revert.
	m.AddJournalEntry(2, types.HexToHash("0x22"))
	m.AddJournalEntry(3, types.HexToHash("0x33"))

	if m.JournalSize() != 3 {
		t.Fatalf("expected journal size 3, got %d", m.JournalSize())
	}

	got := m.GetJournalEntry(2)
	if got == nil || *got != types.HexToHash("0x22") {
		t.Fatal("expected new root for block 2")
	}
}

func TestSnapshotIdsAreUnique(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})
	m.SetRoot(types.HexToHash("0xaa"))

	ids := make(map[types.Hash]bool)
	for i := 0; i < 20; i++ {
		id := m.TakeSnapshot()
		if ids[id] {
			t.Fatalf("duplicate snapshot ID: %s", id)
		}
		ids[id] = true
	}
}

func TestPruneJournal_ZeroKeep(t *testing.T) {
	m := NewStateManager(StateManagerConfig{})
	m.AddJournalEntry(1, types.HexToHash("0x01"))
	m.AddJournalEntry(2, types.HexToHash("0x02"))

	m.PruneJournal(0)
	if m.JournalSize() != 0 {
		t.Fatalf("expected journal size 0, got %d", m.JournalSize())
	}
	if m.LatestBlock() != 0 {
		t.Fatalf("expected latest block 0, got %d", m.LatestBlock())
	}
}
