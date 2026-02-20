package bal

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestAccessModeString(t *testing.T) {
	tests := []struct {
		mode AccessMode
		want string
	}{
		{AccessRead, "read"},
		{AccessWrite, "write"},
		{AccessReadWrite, "read-write"},
		{AccessMode(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("AccessMode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestBuildDetailedEntriesEmpty(t *testing.T) {
	result := BuildDetailedEntries(nil)
	if result != nil {
		t.Fatal("nil BAL should return nil")
	}

	bal := NewBlockAccessList()
	result = BuildDetailedEntries(bal)
	if result != nil {
		t.Fatal("empty BAL should return nil")
	}
}

func TestBuildDetailedEntriesReadOnly(t *testing.T) {
	bal := NewBlockAccessList()
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x01"),
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x10"), Value: types.HexToHash("0xff")},
		},
	})

	entries := BuildDetailedEntries(bal)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].TxIndex != 0 {
		t.Fatalf("expected TxIndex=0, got %d", entries[0].TxIndex)
	}
	if len(entries[0].Accesses) != 1 {
		t.Fatalf("expected 1 access, got %d", len(entries[0].Accesses))
	}
	if entries[0].Accesses[0].Mode != AccessRead {
		t.Fatalf("expected AccessRead, got %v", entries[0].Accesses[0].Mode)
	}
}

func TestBuildDetailedEntriesReadWrite(t *testing.T) {
	bal := NewBlockAccessList()
	slot := types.HexToHash("0x10")
	addr := types.HexToAddress("0x01")

	// Same slot is both read and written in the same tx.
	bal.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: slot, Value: types.HexToHash("0xff")},
		},
		StorageChanges: []StorageChange{
			{Slot: slot, OldValue: types.HexToHash("0xff"), NewValue: types.HexToHash("0x00")},
		},
	})

	entries := BuildDetailedEntries(bal)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Accesses[0].Mode != AccessReadWrite {
		t.Fatalf("expected AccessReadWrite, got %v", entries[0].Accesses[0].Mode)
	}
}

func TestBuildDetailedEntriesAccountLevel(t *testing.T) {
	bal := NewBlockAccessList()
	bal.AddEntry(AccessEntry{
		Address:       types.HexToAddress("0x01"),
		AccessIndex:   1,
		BalanceChange: &BalanceChange{},
	})

	entries := BuildDetailedEntries(bal)
	if len(entries) != 1 || len(entries[0].Accesses) != 1 {
		t.Fatal("expected one account-level access")
	}
	if entries[0].Accesses[0].Mode != AccessWrite {
		t.Fatalf("expected AccessWrite for account-level, got %v", entries[0].Accesses[0].Mode)
	}
}

func TestConflictMatrixBasic(t *testing.T) {
	cm := NewConflictMatrix(3)
	if cm.Size() != 3 {
		t.Fatalf("expected size 3, got %d", cm.Size())
	}

	cm.Set(0, 1)
	if !cm.Get(0, 1) {
		t.Fatal("expected conflict at (0,1)")
	}
	if !cm.Get(1, 0) {
		t.Fatal("expected symmetric conflict at (1,0)")
	}
	if cm.Get(0, 2) {
		t.Fatal("no conflict expected at (0,2)")
	}
}

func TestConflictMatrixOutOfBounds(t *testing.T) {
	cm := NewConflictMatrix(2)
	cm.Set(-1, 0)  // should not panic
	cm.Set(5, 0)   // should not panic
	if cm.Get(-1, 0) {
		t.Fatal("out-of-bounds should return false")
	}
	if cm.Get(5, 0) {
		t.Fatal("out-of-bounds should return false")
	}
}

func TestConflictMatrixConflictCount(t *testing.T) {
	cm := NewConflictMatrix(4)
	cm.Set(0, 1)
	cm.Set(0, 2)
	cm.Set(0, 3)

	if cnt := cm.ConflictCount(0); cnt != 3 {
		t.Fatalf("expected 3 conflicts for tx 0, got %d", cnt)
	}
	if cnt := cm.ConflictCount(1); cnt != 1 {
		t.Fatalf("expected 1 conflict for tx 1, got %d", cnt)
	}
	if cnt := cm.ConflictCount(-1); cnt != 0 {
		t.Fatal("out-of-bounds should return 0")
	}
}

func TestConflictMatrixTotalConflicts(t *testing.T) {
	cm := NewConflictMatrix(4)
	cm.Set(0, 1)
	cm.Set(2, 3)
	if total := cm.TotalConflicts(); total != 2 {
		t.Fatalf("expected 2 total conflicts, got %d", total)
	}
}

func TestBuildConflictMatrix(t *testing.T) {
	conflicts := []Conflict{
		{TxA: 0, TxB: 1, Type: ConflictWriteWrite},
		{TxA: 1, TxB: 2, Type: ConflictReadWrite},
	}
	cm := BuildConflictMatrix(conflicts, 3)
	if !cm.Get(0, 1) {
		t.Fatal("expected conflict at (0,1)")
	}
	if !cm.Get(1, 2) {
		t.Fatal("expected conflict at (1,2)")
	}
	if cm.Get(0, 2) {
		t.Fatal("no conflict expected at (0,2)")
	}
}

func TestDependencyGraphBasic(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNode(0)
	g.AddNode(1)
	g.AddEdge(1, 0)

	nodes := g.Nodes()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	deps := g.Dependencies(1)
	if len(deps) != 1 || deps[0] != 0 {
		t.Fatal("tx 1 should depend on tx 0")
	}

	independent := g.IndependentNodes()
	if len(independent) != 1 || independent[0] != 0 {
		t.Fatal("only tx 0 should be independent")
	}
}

func TestBuildDependencyGraphFromConflicts(t *testing.T) {
	conflicts := []Conflict{
		{TxA: 0, TxB: 2, Type: ConflictWriteWrite},
		{TxA: 1, TxB: 2, Type: ConflictReadWrite},
	}
	g := BuildDependencyGraphFromConflicts(conflicts, 3)

	deps := g.Dependencies(2)
	if len(deps) != 2 {
		t.Fatalf("tx 2 should have 2 deps, got %d", len(deps))
	}
	independent := g.IndependentNodes()
	if len(independent) != 2 {
		t.Fatalf("expected 2 independent nodes, got %d", len(independent))
	}
}

func TestBuildDependencyGraphDeduplication(t *testing.T) {
	// Two conflicts for same pair should produce one edge.
	conflicts := []Conflict{
		{TxA: 0, TxB: 1, Type: ConflictWriteWrite},
		{TxA: 0, TxB: 1, Type: ConflictReadWrite},
	}
	g := BuildDependencyGraphFromConflicts(conflicts, 2)
	deps := g.Dependencies(1)
	if len(deps) != 1 {
		t.Fatalf("expected 1 dep after dedup, got %d", len(deps))
	}
}

func TestScheduleFromGraphNil(t *testing.T) {
	slots := ScheduleFromGraph(nil)
	if slots != nil {
		t.Fatal("nil graph should return nil schedule")
	}
}

func TestScheduleFromGraphNoConflicts(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNode(0)
	g.AddNode(1)
	g.AddNode(2)

	slots := ScheduleFromGraph(g)
	if len(slots) != 3 {
		t.Fatalf("expected 3 slots, got %d", len(slots))
	}
	// All should be in wave 0 (no dependencies).
	for _, s := range slots {
		if s.WaveID != 0 {
			t.Fatalf("tx %d should be in wave 0, got %d", s.TxIndex, s.WaveID)
		}
	}
}

func TestScheduleFromGraphChain(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNode(0)
	g.AddEdge(1, 0) // tx 1 depends on tx 0
	g.AddEdge(2, 1) // tx 2 depends on tx 1

	slots := ScheduleFromGraph(g)
	if len(slots) != 3 {
		t.Fatalf("expected 3 slots, got %d", len(slots))
	}

	waves := WaveCount(slots)
	if waves != 3 {
		t.Fatalf("expected 3 waves, got %d", waves)
	}
}

func TestWaveCountEmpty(t *testing.T) {
	if WaveCount(nil) != 0 {
		t.Fatal("empty slots should have 0 waves")
	}
}

func TestWaveCountSingleWave(t *testing.T) {
	slots := []ScheduleSlot{{TxIndex: 0, WaveID: 0}, {TxIndex: 1, WaveID: 0}}
	if WaveCount(slots) != 1 {
		t.Fatal("expected 1 wave")
	}
}
