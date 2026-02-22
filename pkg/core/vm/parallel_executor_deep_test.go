package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- DependencyAnalyzer tests ---

func TestDependencyAnalyzer_Empty(t *testing.T) {
	da := NewDependencyAnalyzer()
	_, err := da.BuildGraph()
	if err != ErrDepGraphEmpty {
		t.Fatalf("expected ErrDepGraphEmpty, got %v", err)
	}
}

func TestDependencyAnalyzer_NoConflicts(t *testing.T) {
	da := NewDependencyAnalyzer()
	// Two txs accessing different slots.
	da.AddProfile(TxAccessProfile{
		TxIndex: 0,
		Writes:  []AccessPair{{Address: types.HexToAddress("0x01"), Slot: types.HexToHash("0x10"), IsWrite: true}},
	})
	da.AddProfile(TxAccessProfile{
		TxIndex: 1,
		Writes:  []AccessPair{{Address: types.HexToAddress("0x02"), Slot: types.HexToHash("0x20"), IsWrite: true}},
	})

	graph, err := da.BuildGraph()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(graph.Edges))
	}
}

func TestDependencyAnalyzer_RAWDependency(t *testing.T) {
	da := NewDependencyAnalyzer()
	addr := types.HexToAddress("0x01")
	slot := types.HexToHash("0x10")

	da.AddProfile(TxAccessProfile{
		TxIndex: 0,
		Writes:  []AccessPair{{Address: addr, Slot: slot, IsWrite: true}},
	})
	da.AddProfile(TxAccessProfile{
		TxIndex: 1,
		Reads:   []AccessPair{{Address: addr, Slot: slot}},
	})

	graph, err := da.BuildGraph()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(graph.Edges))
	}
	if graph.Edges[0].Kind != "RAW" {
		t.Errorf("expected RAW dependency, got %s", graph.Edges[0].Kind)
	}
	if graph.Edges[0].From != 0 || graph.Edges[0].To != 1 {
		t.Errorf("expected edge 0->1, got %d->%d", graph.Edges[0].From, graph.Edges[0].To)
	}
}

func TestDependencyAnalyzer_WAWDependency(t *testing.T) {
	da := NewDependencyAnalyzer()
	addr := types.HexToAddress("0x01")
	slot := types.HexToHash("0x10")

	da.AddProfile(TxAccessProfile{
		TxIndex: 0,
		Writes:  []AccessPair{{Address: addr, Slot: slot, IsWrite: true}},
	})
	da.AddProfile(TxAccessProfile{
		TxIndex: 1,
		Writes:  []AccessPair{{Address: addr, Slot: slot, IsWrite: true}},
	})

	graph, err := da.BuildGraph()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(graph.Edges))
	}
	if graph.Edges[0].Kind != "WAW" {
		t.Errorf("expected WAW dependency, got %s", graph.Edges[0].Kind)
	}
}

func TestDependencyAnalyzer_WARDependency(t *testing.T) {
	da := NewDependencyAnalyzer()
	addr := types.HexToAddress("0x01")
	slot := types.HexToHash("0x10")

	da.AddProfile(TxAccessProfile{
		TxIndex: 0,
		Reads:   []AccessPair{{Address: addr, Slot: slot}},
	})
	da.AddProfile(TxAccessProfile{
		TxIndex: 1,
		Writes:  []AccessPair{{Address: addr, Slot: slot, IsWrite: true}},
	})

	graph, err := da.BuildGraph()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(graph.Edges))
	}
	if graph.Edges[0].Kind != "WAR" {
		t.Errorf("expected WAR dependency, got %s", graph.Edges[0].Kind)
	}
}

func TestDependencyAnalyzer_ReadReadNoDep(t *testing.T) {
	da := NewDependencyAnalyzer()
	addr := types.HexToAddress("0x01")
	slot := types.HexToHash("0x10")

	da.AddProfile(TxAccessProfile{
		TxIndex: 0,
		Reads:   []AccessPair{{Address: addr, Slot: slot}},
	})
	da.AddProfile(TxAccessProfile{
		TxIndex: 1,
		Reads:   []AccessPair{{Address: addr, Slot: slot}},
	})

	graph, err := da.BuildGraph()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Edges) != 0 {
		t.Errorf("expected 0 edges for read-read, got %d", len(graph.Edges))
	}
}

func TestDependencyAnalyzer_ProfileCount(t *testing.T) {
	da := NewDependencyAnalyzer()
	da.AddProfile(TxAccessProfile{TxIndex: 0})
	da.AddProfile(TxAccessProfile{TxIndex: 1})
	if da.ProfileCount() != 2 {
		t.Errorf("expected 2 profiles, got %d", da.ProfileCount())
	}
}

// --- ExecutionGroup tests ---

func TestBuildExecutionGroups_AllIndependent(t *testing.T) {
	graph := &DependencyGraph{
		NumTxs:     4,
		Successors: make(map[int][]int),
		InDegree:   make([]int, 4),
	}
	groups, err := BuildExecutionGroups(graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].TxIndices) != 4 {
		t.Errorf("expected 4 txs in group 0, got %d", len(groups[0].TxIndices))
	}
}

func TestBuildExecutionGroups_Chain(t *testing.T) {
	// Linear dependency chain: 0 -> 1 -> 2 -> 3
	graph := &DependencyGraph{
		NumTxs: 4,
		Edges: []DependencyEdge{
			{From: 0, To: 1, Kind: "RAW"},
			{From: 1, To: 2, Kind: "RAW"},
			{From: 2, To: 3, Kind: "RAW"},
		},
		Successors: map[int][]int{
			0: {1}, 1: {2}, 2: {3},
		},
		InDegree: []int{0, 1, 1, 1},
	}

	groups, err := BuildExecutionGroups(graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 4 {
		t.Fatalf("expected 4 groups for chain, got %d", len(groups))
	}
	for i, g := range groups {
		if len(g.TxIndices) != 1 || g.TxIndices[0] != i {
			t.Errorf("group %d: expected [%d], got %v", i, i, g.TxIndices)
		}
	}
}

func TestBuildExecutionGroups_Diamond(t *testing.T) {
	// Diamond: 0 -> 1, 0 -> 2, 1 -> 3, 2 -> 3
	graph := &DependencyGraph{
		NumTxs: 4,
		Successors: map[int][]int{
			0: {1, 2}, 1: {3}, 2: {3},
		},
		InDegree: []int{0, 1, 1, 2},
	}

	groups, err := BuildExecutionGroups(graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Level 0: [0], Level 1: [1, 2], Level 2: [3]
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups for diamond, got %d", len(groups))
	}
	if len(groups[1].TxIndices) != 2 {
		t.Errorf("expected 2 txs in group 1, got %d", len(groups[1].TxIndices))
	}
}

func TestBuildExecutionGroups_Nil(t *testing.T) {
	_, err := BuildExecutionGroups(nil)
	if err != ErrExecGroupEmpty {
		t.Fatalf("expected ErrExecGroupEmpty, got %v", err)
	}
}

// --- SpeculativeExecutor tests ---

func TestSpeculativeExecutor_BasicExecution(t *testing.T) {
	se := NewSpeculativeExecutor()
	state := newParallelMockStateDB()

	to := types.HexToAddress("0xdead")
	tx := types.NewTransaction(&types.LegacyTx{
		Gas: 21000, GasPrice: big.NewInt(1), To: &to,
	})

	result := se.Execute(0, tx, state)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.GasUsed != 21000 {
		t.Errorf("expected 21000 gas, got %d", result.GasUsed)
	}
	if result.RolledBack {
		t.Error("should not be rolled back")
	}
}

func TestSpeculativeExecutor_Rollback(t *testing.T) {
	se := NewSpeculativeExecutor()
	state := newParallelMockStateDB()

	to := types.HexToAddress("0xdead")
	tx := types.NewTransaction(&types.LegacyTx{
		Gas: 21000, GasPrice: big.NewInt(1), To: &to,
	})

	se.Execute(0, tx, state)
	err := se.Rollback(0, state)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	results := se.Results()
	if !results[0].RolledBack {
		t.Error("expected rolled back flag after rollback")
	}
}

func TestSpeculativeExecutor_RollbackNonexistent(t *testing.T) {
	se := NewSpeculativeExecutor()
	state := newParallelMockStateDB()

	err := se.Rollback(99, state)
	if err == nil {
		t.Error("expected error for nonexistent tx rollback")
	}
}

// --- StateDelta and MergeResults tests ---

func TestStateDelta_Basic(t *testing.T) {
	sd := NewStateDelta()
	sd.Record(types.HexToAddress("0x01"), types.HexToHash("0x10"), types.HexToHash("0xAA"))
	if sd.Len() != 1 {
		t.Errorf("expected 1 write, got %d", sd.Len())
	}
}

func TestMergeResults_Success(t *testing.T) {
	state := newParallelMockStateDB()

	d1 := NewStateDelta()
	d1.Record(types.HexToAddress("0x01"), types.HexToHash("0x10"), types.HexToHash("0xAA"))

	d2 := NewStateDelta()
	d2.Record(types.HexToAddress("0x02"), types.HexToHash("0x20"), types.HexToHash("0xBB"))

	err := MergeResults([]*StateDelta{d1, d2}, state)
	if err != nil {
		t.Fatalf("MergeResults failed: %v", err)
	}

	// Verify state was updated.
	val := state.GetState(types.HexToAddress("0x01"), types.HexToHash("0x10"))
	if val != types.HexToHash("0xAA") {
		t.Errorf("expected 0xAA, got %s", val.Hex())
	}
}

func TestMergeResults_Conflict(t *testing.T) {
	state := newParallelMockStateDB()

	d1 := NewStateDelta()
	d1.Record(types.HexToAddress("0x01"), types.HexToHash("0x10"), types.HexToHash("0xAA"))

	d2 := NewStateDelta()
	d2.Record(types.HexToAddress("0x01"), types.HexToHash("0x10"), types.HexToHash("0xBB"))

	err := MergeResults([]*StateDelta{d1, d2}, state)
	if err == nil {
		t.Fatal("expected ErrMergeConflict, got nil")
	}
}

func TestMergeResults_NilState(t *testing.T) {
	d1 := NewStateDelta()
	err := MergeResults([]*StateDelta{d1}, nil)
	if err != ErrMergeStateNil {
		t.Fatalf("expected ErrMergeStateNil, got %v", err)
	}
}

// --- AnalyzeParallelism tests ---

func TestAnalyzeParallelism_FullParallel(t *testing.T) {
	graph := &DependencyGraph{
		NumTxs:     5,
		Successors: make(map[int][]int),
		InDegree:   make([]int, 5),
	}
	maxPar, levels, speedup := AnalyzeParallelism(graph)
	if maxPar != 5 {
		t.Errorf("expected maxPar=5, got %d", maxPar)
	}
	if levels != 1 {
		t.Errorf("expected 1 level, got %d", levels)
	}
	if speedup != 5.0 {
		t.Errorf("expected speedup=5.0, got %f", speedup)
	}
}

func TestAnalyzeParallelism_Serial(t *testing.T) {
	// 0 -> 1 -> 2
	graph := &DependencyGraph{
		NumTxs:     3,
		Successors: map[int][]int{0: {1}, 1: {2}},
		InDegree:   []int{0, 1, 1},
	}
	maxPar, levels, speedup := AnalyzeParallelism(graph)
	if maxPar != 1 {
		t.Errorf("expected maxPar=1, got %d", maxPar)
	}
	if levels != 3 {
		t.Errorf("expected 3 levels, got %d", levels)
	}
	if speedup != 1.0 {
		t.Errorf("expected speedup=1.0, got %f", speedup)
	}
}

func TestAnalyzeParallelism_Nil(t *testing.T) {
	maxPar, levels, speedup := AnalyzeParallelism(nil)
	if maxPar != 0 || levels != 0 || speedup != 0 {
		t.Errorf("expected zeros for nil graph, got %d %d %f", maxPar, levels, speedup)
	}
}
