package bal

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// graphTestBAL builds a BAL with 5 transactions:
// tx0: writes addr1/slot1
// tx1: reads addr1/slot1 (depends on tx0)
// tx2: writes addr2/slot2 (independent)
// tx3: writes addr3/slot3 (independent)
// tx4: reads addr2/slot2 and writes addr1/slot1 (depends on tx0, tx2)
func graphTestBAL() *BlockAccessList {
	bal := NewBlockAccessList()
	addr1 := types.HexToAddress("0xaa")
	addr2 := types.HexToAddress("0xbb")
	addr3 := types.HexToAddress("0xcc")
	slot1 := types.HexToHash("0x01")
	slot2 := types.HexToHash("0x02")
	slot3 := types.HexToHash("0x03")

	bal.AddEntry(AccessEntry{
		Address:     addr1,
		AccessIndex: 1,
		StorageChanges: []StorageChange{
			{Slot: slot1, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x10")},
		},
	})
	bal.AddEntry(AccessEntry{
		Address:     addr1,
		AccessIndex: 2,
		StorageReads: []StorageAccess{
			{Slot: slot1, Value: types.HexToHash("0x10")},
		},
	})
	bal.AddEntry(AccessEntry{
		Address:     addr2,
		AccessIndex: 3,
		StorageChanges: []StorageChange{
			{Slot: slot2, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x20")},
		},
	})
	bal.AddEntry(AccessEntry{
		Address:     addr3,
		AccessIndex: 4,
		StorageChanges: []StorageChange{
			{Slot: slot3, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x30")},
		},
	})
	// tx4: reads addr2/slot2, writes addr1/slot1
	bal.AddEntry(AccessEntry{
		Address:     addr2,
		AccessIndex: 5,
		StorageReads: []StorageAccess{
			{Slot: slot2, Value: types.HexToHash("0x20")},
		},
	})
	bal.AddEntry(AccessEntry{
		Address:     addr1,
		AccessIndex: 5,
		StorageChanges: []StorageChange{
			{Slot: slot1, OldValue: types.HexToHash("0x10"), NewValue: types.HexToHash("0x40")},
		},
	})

	return bal
}

func TestGraphConflictAnalyzerNew(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)
	if analyzer.Detector() != det {
		t.Fatal("Detector() should return the wrapped detector")
	}
}

func TestGraphConflictAnalyzerFindParallelGroups(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)
	bal := graphTestBAL()

	groups := analyzer.FindParallelGroupsFromBAL(bal)
	if len(groups) == 0 {
		t.Fatal("expected at least one parallel group")
	}

	// Wave 0 should contain independent txs (tx0, tx2, tx3).
	// Wave 1 should contain tx1 (depends on tx0).
	// Wave 2 should contain tx4 (depends on tx0 and tx2).
	if len(groups) < 2 {
		t.Fatalf("expected at least 2 waves, got %d", len(groups))
	}

	// Verify total tx count.
	total := 0
	for _, g := range groups {
		total += len(g)
	}
	if total != 5 {
		t.Fatalf("expected 5 total txs, got %d", total)
	}
}

func TestGraphConflictAnalyzerFindParallelGroupsNil(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	if groups := analyzer.FindParallelGroupsFromBAL(nil); groups != nil {
		t.Fatal("expected nil for nil BAL")
	}

	if groups := analyzer.FindParallelGroups(nil); groups != nil {
		t.Fatal("expected nil for nil graph")
	}
}

func TestGraphConflictAnalyzerCanParallelize(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	addr := types.HexToAddress("0xaa")
	slot := types.HexToHash("0x01")

	// Two txs writing different slots: should be parallelizable.
	bal1 := NewBlockAccessList()
	bal1.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageChanges: []StorageChange{
			{Slot: slot, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x01")},
		},
	})
	bal2 := NewBlockAccessList()
	bal2.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0xbb"),
		AccessIndex: 1,
		StorageChanges: []StorageChange{
			{Slot: types.HexToHash("0x02"), OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x02")},
		},
	})
	if !analyzer.CanParallelize(bal1, bal2) {
		t.Fatal("different addresses should be parallelizable")
	}
}

func TestGraphConflictAnalyzerCanParallelizeConflict(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	addr := types.HexToAddress("0xaa")
	slot := types.HexToHash("0x01")

	// Two txs writing same slot: not parallelizable.
	bal1 := NewBlockAccessList()
	bal1.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageChanges: []StorageChange{
			{Slot: slot, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x01")},
		},
	})
	bal2 := NewBlockAccessList()
	bal2.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageChanges: []StorageChange{
			{Slot: slot, OldValue: types.HexToHash("0x01"), NewValue: types.HexToHash("0x02")},
		},
	})
	if analyzer.CanParallelize(bal1, bal2) {
		t.Fatal("same address/slot writes should NOT be parallelizable")
	}
}

func TestGraphConflictAnalyzerCanParallelizeReadWrite(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	addr := types.HexToAddress("0xaa")
	slot := types.HexToHash("0x01")

	bal1 := NewBlockAccessList()
	bal1.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: slot, Value: types.HexToHash("0x10")},
		},
	})
	bal2 := NewBlockAccessList()
	bal2.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageChanges: []StorageChange{
			{Slot: slot, OldValue: types.HexToHash("0x10"), NewValue: types.HexToHash("0x20")},
		},
	})
	if analyzer.CanParallelize(bal1, bal2) {
		t.Fatal("read-write on same slot should NOT be parallelizable")
	}
}

func TestGraphConflictAnalyzerCanParallelizeNil(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	if !analyzer.CanParallelize(nil, nil) {
		t.Fatal("nil BALs should be parallelizable")
	}
	bal := NewBlockAccessList()
	if !analyzer.CanParallelize(bal, nil) {
		t.Fatal("nil BAL should be parallelizable with anything")
	}
}

func TestGraphConflictAnalyzerCanParallelizeAccountLevel(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	addr := types.HexToAddress("0xaa")

	bal1 := NewBlockAccessList()
	bal1.AddEntry(AccessEntry{
		Address:       addr,
		AccessIndex:   1,
		BalanceChange: &BalanceChange{OldValue: big.NewInt(100), NewValue: big.NewInt(50)},
	})
	bal2 := NewBlockAccessList()
	bal2.AddEntry(AccessEntry{
		Address:       addr,
		AccessIndex:   1,
		BalanceChange: &BalanceChange{OldValue: big.NewInt(50), NewValue: big.NewInt(30)},
	})
	if analyzer.CanParallelize(bal1, bal2) {
		t.Fatal("two txs changing the same account balance should NOT be parallelizable")
	}
}

func TestGraphConflictAnalyzerEstimateSpeedup(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	// 5 txs in 2 groups: speedup = 5/2 = 2.5
	groups := [][]int{{0, 1, 2}, {3, 4}}
	speedup := analyzer.EstimateSpeedup(groups)
	if speedup < 2.49 || speedup > 2.51 {
		t.Fatalf("expected ~2.5 speedup, got %f", speedup)
	}
}

func TestGraphConflictAnalyzerEstimateSpeedupEmpty(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	if speedup := analyzer.EstimateSpeedup(nil); speedup != 1.0 {
		t.Fatalf("expected 1.0 for nil groups, got %f", speedup)
	}
	if speedup := analyzer.EstimateSpeedup([][]int{}); speedup != 1.0 {
		t.Fatalf("expected 1.0 for empty groups, got %f", speedup)
	}
}

func TestGraphConflictAnalyzerEstimateSpeedupFullParallel(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	// All txs in one group: speedup = 5/1 = 5.0
	groups := [][]int{{0, 1, 2, 3, 4}}
	speedup := analyzer.EstimateSpeedup(groups)
	if speedup < 4.99 || speedup > 5.01 {
		t.Fatalf("expected ~5.0 speedup, got %f", speedup)
	}
}

func TestGraphConflictAnalyzerEstimateSpeedupWeighted(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	groups := [][]int{{0, 1}, {2}}
	gasCosts := map[int]uint64{
		0: 100,
		1: 200,
		2: 50,
	}
	// Total gas: 350. Critical path: max(100,200) + 50 = 250. Speedup: 350/250 = 1.4
	speedup := analyzer.EstimateSpeedupWeighted(groups, gasCosts)
	if speedup < 1.39 || speedup > 1.41 {
		t.Fatalf("expected ~1.4 weighted speedup, got %f", speedup)
	}
}

func TestGraphConflictAnalyzerEstimateSpeedupWeightedEmpty(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	speedup := analyzer.EstimateSpeedupWeighted(nil, nil)
	if speedup != 1.0 {
		t.Fatalf("expected 1.0 for nil, got %f", speedup)
	}
}

func TestGraphConflictAnalyzerCriticalPath(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)
	bal := graphTestBAL()

	conflicts := det.DetectConflicts(bal)
	sets := extractRWSets(bal)
	numTx := 0
	for idx := range sets {
		if idx+1 > numTx {
			numTx = idx + 1
		}
	}
	graph := BuildDependencyGraphFromConflicts(conflicts, numTx)
	path := analyzer.CriticalPath(graph)

	if len(path) == 0 {
		t.Fatal("expected non-empty critical path")
	}
	// The critical path should end at tx4 which depends on both tx0 and tx2.
	if path[len(path)-1] != 4 {
		t.Fatalf("expected critical path to end at tx4, got tx%d", path[len(path)-1])
	}
}

func TestGraphConflictAnalyzerCriticalPathNil(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	if path := analyzer.CriticalPath(nil); path != nil {
		t.Fatal("expected nil for nil graph")
	}
}

func TestGraphConflictAnalyzerSummarize(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)
	bal := graphTestBAL()

	conflicts := det.DetectConflicts(bal)
	sets := extractRWSets(bal)
	numTx := 0
	for idx := range sets {
		if idx+1 > numTx {
			numTx = idx + 1
		}
	}
	graph := BuildDependencyGraphFromConflicts(conflicts, numTx)
	summary := analyzer.Summarize(graph)

	if summary.TotalNodes != 5 {
		t.Fatalf("expected 5 nodes, got %d", summary.TotalNodes)
	}
	if summary.ParallelGroups < 2 {
		t.Fatalf("expected at least 2 parallel groups, got %d", summary.ParallelGroups)
	}
	if summary.Speedup <= 1.0 {
		t.Fatalf("expected speedup > 1.0, got %f", summary.Speedup)
	}
}

func TestGraphConflictAnalyzerSummarizeNil(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	summary := analyzer.Summarize(nil)
	if summary.TotalNodes != 0 {
		t.Fatal("expected 0 nodes for nil graph")
	}
}

func TestGraphConflictAnalyzerTransitiveDeps(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	// Build a chain: tx0 -> tx1 -> tx2
	graph := NewDependencyGraph()
	graph.AddNode(0)
	graph.AddEdge(1, 0)
	graph.AddEdge(2, 1)

	deps := analyzer.TransitiveDeps(graph, 2)
	if len(deps) != 2 {
		t.Fatalf("expected 2 transitive deps for tx2, got %d", len(deps))
	}
	// Should include tx0 and tx1.
	if deps[0] != 0 || deps[1] != 1 {
		t.Fatalf("expected deps [0, 1], got %v", deps)
	}
}

func TestGraphConflictAnalyzerTransitiveDepsRoot(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	graph := NewDependencyGraph()
	graph.AddNode(0)
	graph.AddEdge(1, 0)

	deps := analyzer.TransitiveDeps(graph, 0)
	if len(deps) != 0 {
		t.Fatalf("root node should have 0 transitive deps, got %d", len(deps))
	}
}

func TestGraphConflictAnalyzerTransitiveDepsNil(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)
	if deps := analyzer.TransitiveDeps(nil, 0); deps != nil {
		t.Fatal("expected nil for nil graph")
	}
}

func TestGraphConflictAnalyzerTransitiveDependents(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	// tx0 -> tx1 -> tx2, tx0 -> tx3
	graph := NewDependencyGraph()
	graph.AddNode(0)
	graph.AddEdge(1, 0)
	graph.AddEdge(2, 1)
	graph.AddEdge(3, 0)

	dependents := analyzer.TransitiveDependents(graph, 0)
	if len(dependents) != 3 {
		t.Fatalf("expected 3 transitive dependents of tx0, got %d: %v", len(dependents), dependents)
	}
}

func TestGraphConflictAnalyzerTransitiveDependentsLeaf(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	graph := NewDependencyGraph()
	graph.AddNode(0)
	graph.AddEdge(1, 0)

	dependents := analyzer.TransitiveDependents(graph, 1)
	if len(dependents) != 0 {
		t.Fatalf("leaf node should have 0 dependents, got %d", len(dependents))
	}
}

func TestGraphConflictAnalyzerIndependentPairCount(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	// 3 nodes, 1 edge (0->1). Total pairs = 3. Conflicting = 1.
	graph := NewDependencyGraph()
	graph.AddNode(0)
	graph.AddNode(2)
	graph.AddEdge(1, 0)

	count := analyzer.IndependentPairCount(graph)
	if count != 2 {
		t.Fatalf("expected 2 independent pairs, got %d", count)
	}
}

func TestGraphConflictAnalyzerIndependentPairCountNil(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	if count := analyzer.IndependentPairCount(nil); count != 0 {
		t.Fatalf("expected 0 for nil graph, got %d", count)
	}
}

func TestGraphConflictAnalyzerDetectConflictsFromBALs(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	addr := types.HexToAddress("0xaa")
	slot := types.HexToHash("0x01")

	bal0 := NewBlockAccessList()
	bal0.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageChanges: []StorageChange{
			{Slot: slot, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x10")},
		},
	})
	bal1 := NewBlockAccessList()
	bal1.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: slot, Value: types.HexToHash("0x10")},
		},
	})
	bal2 := NewBlockAccessList()
	bal2.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0xbb"),
		AccessIndex: 1,
		StorageChanges: []StorageChange{
			{Slot: types.HexToHash("0x02"), OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x20")},
		},
	})

	conflicts := analyzer.DetectConflictsFromBALs([]*BlockAccessList{bal0, bal1, bal2})
	if len(conflicts) == 0 {
		t.Fatal("expected at least one conflict between bal0 and bal1")
	}

	// bal2 should not conflict with either.
	for _, c := range conflicts {
		if c.TxA == 2 || c.TxB == 2 {
			t.Fatalf("bal2 should not be involved in conflicts, got %+v", c)
		}
	}
}

func TestGraphConflictAnalyzerBuildDependencyGraphFromBALs(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	addr := types.HexToAddress("0xaa")
	slot := types.HexToHash("0x01")

	bal0 := NewBlockAccessList()
	bal0.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageChanges: []StorageChange{
			{Slot: slot, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x10")},
		},
	})
	bal1 := NewBlockAccessList()
	bal1.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: slot, Value: types.HexToHash("0x10")},
		},
	})

	graph := analyzer.BuildDependencyGraphFromBALs([]*BlockAccessList{bal0, bal1})
	if graph == nil {
		t.Fatal("expected non-nil graph")
	}

	// tx1 should depend on tx0.
	deps := graph.Dependencies(1)
	if len(deps) != 1 || deps[0] != 0 {
		t.Fatalf("expected tx1 to depend on tx0, got deps %v", deps)
	}
}

func TestGraphConflictAnalyzerFullParallelBlock(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	// 4 txs all writing different addresses: fully parallel.
	bal := NewBlockAccessList()
	for i := 0; i < 4; i++ {
		addr := types.HexToAddress("0x" + string(rune('a'+i)) + string(rune('a'+i)))
		slot := types.HexToHash("0x01")
		bal.AddEntry(AccessEntry{
			Address:     addr,
			AccessIndex: uint64(i + 1),
			StorageChanges: []StorageChange{
				{Slot: slot, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x10")},
			},
		})
	}

	groups := analyzer.FindParallelGroupsFromBAL(bal)
	if len(groups) != 1 {
		t.Fatalf("expected 1 wave for fully parallel block, got %d", len(groups))
	}
	if len(groups[0]) != 4 {
		t.Fatalf("expected 4 txs in wave, got %d", len(groups[0]))
	}

	speedup := analyzer.EstimateSpeedup(groups)
	if speedup < 3.99 || speedup > 4.01 {
		t.Fatalf("expected ~4.0 speedup, got %f", speedup)
	}
}

func TestGraphConflictAnalyzerFullSerialBlock(t *testing.T) {
	det := NewBALConflictDetector(StrategySerialize)
	analyzer := NewGraphConflictAnalyzer(det)

	addr := types.HexToAddress("0xaa")
	slot := types.HexToHash("0x01")

	// 3 txs all writing same slot: fully serial chain.
	bal := NewBlockAccessList()
	for i := 0; i < 3; i++ {
		bal.AddEntry(AccessEntry{
			Address:     addr,
			AccessIndex: uint64(i + 1),
			StorageChanges: []StorageChange{
				{Slot: slot, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x10")},
			},
		})
	}

	groups := analyzer.FindParallelGroupsFromBAL(bal)
	// Each wave should have exactly 1 tx.
	for _, group := range groups {
		if len(group) != 1 {
			t.Fatalf("expected 1 tx per wave in serial block, got %d", len(group))
		}
	}

	speedup := analyzer.EstimateSpeedup(groups)
	if speedup < 0.99 || speedup > 1.01 {
		t.Fatalf("expected ~1.0 speedup for serial block, got %f", speedup)
	}
}
