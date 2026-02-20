package state

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// helpers for creating hashes.
func slotHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestDetectConflictsNoConflict(t *testing.T) {
	als := []BALSAccessList{
		{TxIndex: 0, ReadSet: []types.Hash{slotHash(1)}, WriteSet: []types.Hash{slotHash(2)}},
		{TxIndex: 1, ReadSet: []types.Hash{slotHash(3)}, WriteSet: []types.Hash{slotHash(4)}},
	}

	conflicts := DetectConflicts(als)
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts, got %d", len(conflicts))
	}
}

func TestDetectConflictsWriteWrite(t *testing.T) {
	slot := slotHash(0xAA)
	als := []BALSAccessList{
		{TxIndex: 0, WriteSet: []types.Hash{slot}},
		{TxIndex: 1, WriteSet: []types.Hash{slot}},
	}

	conflicts := DetectConflicts(als)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].Reason != "write-write" {
		t.Errorf("reason = %q, want write-write", conflicts[0].Reason)
	}
	if conflicts[0].TxA != 0 || conflicts[0].TxB != 1 {
		t.Errorf("conflict = (%d, %d), want (0, 1)", conflicts[0].TxA, conflicts[0].TxB)
	}
}

func TestDetectConflictsReadWrite(t *testing.T) {
	slot := slotHash(0xBB)
	als := []BALSAccessList{
		{TxIndex: 0, ReadSet: []types.Hash{slot}},
		{TxIndex: 1, WriteSet: []types.Hash{slot}},
	}

	conflicts := DetectConflicts(als)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].Reason != "read-write" {
		t.Errorf("reason = %q, want read-write", conflicts[0].Reason)
	}
}

func TestDetectConflictsWriteRead(t *testing.T) {
	slot := slotHash(0xCC)
	als := []BALSAccessList{
		{TxIndex: 0, WriteSet: []types.Hash{slot}},
		{TxIndex: 1, ReadSet: []types.Hash{slot}},
	}

	conflicts := DetectConflicts(als)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].Reason != "write-read" {
		t.Errorf("reason = %q, want write-read", conflicts[0].Reason)
	}
}

func TestDetectConflictsMultiple(t *testing.T) {
	slot := slotHash(0xDD)
	als := []BALSAccessList{
		{TxIndex: 0, WriteSet: []types.Hash{slot}},
		{TxIndex: 1, WriteSet: []types.Hash{slot}},
		{TxIndex: 2, WriteSet: []types.Hash{slot}},
	}

	conflicts := DetectConflicts(als)
	// 3 pairs: (0,1), (0,2), (1,2).
	if len(conflicts) != 3 {
		t.Errorf("expected 3 conflicts, got %d", len(conflicts))
	}
}

func TestDetectConflictsEmpty(t *testing.T) {
	conflicts := DetectConflicts(nil)
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts, got %d", len(conflicts))
	}
}

func TestBuildDependencyGraph(t *testing.T) {
	slot := slotHash(0x01)
	als := []BALSAccessList{
		{TxIndex: 0, WriteSet: []types.Hash{slot}},
		{TxIndex: 1, ReadSet: []types.Hash{slot}},
		{TxIndex: 2, ReadSet: []types.Hash{slotHash(0x99)}},
	}

	graph := BuildDependencyGraph(als)

	// Tx1 depends on Tx0 (write-read conflict).
	deps, ok := graph[1]
	if !ok {
		t.Fatal("tx 1 not in graph")
	}
	if len(deps) != 1 || deps[0] != 0 {
		t.Errorf("tx 1 deps = %v, want [0]", deps)
	}

	// Tx2 has no dependencies.
	deps2 := graph[2]
	if len(deps2) != 0 {
		t.Errorf("tx 2 deps = %v, want []", deps2)
	}
}

func TestBuildDependencyGraphNoDeps(t *testing.T) {
	als := []BALSAccessList{
		{TxIndex: 0, ReadSet: []types.Hash{slotHash(1)}},
		{TxIndex: 1, ReadSet: []types.Hash{slotHash(2)}},
	}

	graph := BuildDependencyGraph(als)
	for idx, deps := range graph {
		if len(deps) != 0 {
			t.Errorf("tx %d has unexpected deps: %v", idx, deps)
		}
	}
}

func TestTopologicalSortSimple(t *testing.T) {
	graph := DependencyGraph{
		0: nil,
		1: {0},
		2: {0},
		3: {1, 2},
	}

	order, err := TopologicalSort(graph, 4)
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("order len = %d, want 4", len(order))
	}

	// Verify ordering constraints.
	pos := make(map[int]int)
	for i, v := range order {
		pos[v] = i
	}
	if pos[0] >= pos[1] {
		t.Error("tx 0 must come before tx 1")
	}
	if pos[0] >= pos[2] {
		t.Error("tx 0 must come before tx 2")
	}
	if pos[1] >= pos[3] {
		t.Error("tx 1 must come before tx 3")
	}
	if pos[2] >= pos[3] {
		t.Error("tx 2 must come before tx 3")
	}
}

func TestTopologicalSortNoEdges(t *testing.T) {
	graph := DependencyGraph{
		0: nil,
		1: nil,
		2: nil,
	}

	order, err := TopologicalSort(graph, 3)
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	// Should be sorted by index for determinism.
	if order[0] != 0 || order[1] != 1 || order[2] != 2 {
		t.Errorf("order = %v, want [0 1 2]", order)
	}
}

func TestTopologicalSortLinearChain(t *testing.T) {
	graph := DependencyGraph{
		0: nil,
		1: {0},
		2: {1},
		3: {2},
	}

	order, err := TopologicalSort(graph, 4)
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	for i, v := range order {
		if v != i {
			t.Errorf("order[%d] = %d, want %d", i, v, i)
		}
	}
}

func TestTopologicalSortEmpty(t *testing.T) {
	order, err := TopologicalSort(DependencyGraph{}, 0)
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if len(order) != 0 {
		t.Errorf("order len = %d, want 0", len(order))
	}
}

func TestComputeParallelGainFullParallel(t *testing.T) {
	als := []BALSAccessList{
		{TxIndex: 0, ReadSet: []types.Hash{slotHash(1)}},
		{TxIndex: 1, ReadSet: []types.Hash{slotHash(2)}},
		{TxIndex: 2, ReadSet: []types.Hash{slotHash(3)}},
		{TxIndex: 3, ReadSet: []types.Hash{slotHash(4)}},
	}

	gain := ComputeParallelGain(als)
	// 4 transactions, 1 level -> gain = 4.0.
	if gain != 4.0 {
		t.Errorf("gain = %f, want 4.0", gain)
	}
}

func TestComputeParallelGainFullSerial(t *testing.T) {
	slot := slotHash(0xFF)
	als := []BALSAccessList{
		{TxIndex: 0, WriteSet: []types.Hash{slot}},
		{TxIndex: 1, WriteSet: []types.Hash{slot}},
		{TxIndex: 2, WriteSet: []types.Hash{slot}},
	}

	gain := ComputeParallelGain(als)
	// 3 transactions, 3 levels -> gain = 1.0.
	if gain != 1.0 {
		t.Errorf("gain = %f, want 1.0", gain)
	}
}

func TestComputeParallelGainSingle(t *testing.T) {
	als := []BALSAccessList{
		{TxIndex: 0, ReadSet: []types.Hash{slotHash(1)}},
	}

	gain := ComputeParallelGain(als)
	if gain != 1.0 {
		t.Errorf("gain = %f, want 1.0", gain)
	}
}

func TestComputeParallelGainPartial(t *testing.T) {
	slot := slotHash(0x01)
	als := []BALSAccessList{
		{TxIndex: 0, WriteSet: []types.Hash{slot}},
		{TxIndex: 1, ReadSet: []types.Hash{slot}},
		{TxIndex: 2, ReadSet: []types.Hash{slotHash(0x99)}},
		{TxIndex: 3, ReadSet: []types.Hash{slotHash(0x98)}},
	}

	gain := ComputeParallelGain(als)
	// Tx0 -> Tx1 chain, Tx2 and Tx3 independent.
	// Level 0: [0, 2, 3], Level 1: [1] -> 2 levels.
	// gain = 4/2 = 2.0.
	if gain != 2.0 {
		t.Errorf("gain = %f, want 2.0", gain)
	}
}

func TestExecuteParallelBasic(t *testing.T) {
	engine := NewBALSEngine(4)

	txs := []BALSTx{
		{Index: 0, GasLimit: 100000, Data: []byte{0x01}},
		{Index: 1, GasLimit: 100000, Data: []byte{0x02, 0x03}},
		{Index: 2, GasLimit: 100000, Data: nil},
	}

	als := []BALSAccessList{
		{TxIndex: 0, ReadSet: []types.Hash{slotHash(1)}},
		{TxIndex: 1, ReadSet: []types.Hash{slotHash(2)}},
		{TxIndex: 2, ReadSet: []types.Hash{slotHash(3)}},
	}

	results, err := engine.ExecuteParallel(txs, als)
	if err != nil {
		t.Fatalf("ExecuteParallel: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results len = %d, want 3", len(results))
	}
	for i, r := range results {
		if !r.Success {
			t.Errorf("tx %d failed", i)
		}
		if r.TxIndex != i {
			t.Errorf("tx %d: TxIndex = %d", i, r.TxIndex)
		}
	}
}

func TestExecuteParallelWithDeps(t *testing.T) {
	engine := NewBALSEngine(2)

	slot := slotHash(0xAA)
	txs := []BALSTx{
		{Index: 0, GasLimit: 100000, Data: []byte{0x01}},
		{Index: 1, GasLimit: 100000, Data: []byte{0x02}},
	}

	als := []BALSAccessList{
		{TxIndex: 0, WriteSet: []types.Hash{slot}},
		{TxIndex: 1, ReadSet: []types.Hash{slot}},
	}

	results, err := engine.ExecuteParallel(txs, als)
	if err != nil {
		t.Fatalf("ExecuteParallel: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	if !results[0].Success || !results[1].Success {
		t.Error("all transactions should succeed")
	}
}

func TestExecuteParallelEmpty(t *testing.T) {
	engine := NewBALSEngine(4)

	results, err := engine.ExecuteParallel(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Error("expected nil results for empty txs")
	}
}

func TestExecuteParallelNoAccessLists(t *testing.T) {
	engine := NewBALSEngine(4)
	txs := []BALSTx{{Index: 0, GasLimit: 100000}}

	_, err := engine.ExecuteParallel(txs, nil)
	if err != ErrEmptyAccessLists {
		t.Errorf("got %v, want ErrEmptyAccessLists", err)
	}
}

func TestExecuteParallelGasLimit(t *testing.T) {
	engine := NewBALSEngine(1)

	// Data of 100 bytes => gas = 21000 + 100*16 = 22600, which exceeds 22000.
	data := make([]byte, 100)
	txs := []BALSTx{
		{Index: 0, GasLimit: 22000, Data: data},
	}
	als := []BALSAccessList{
		{TxIndex: 0, ReadSet: []types.Hash{slotHash(1)}},
	}

	results, err := engine.ExecuteParallel(txs, als)
	if err != nil {
		t.Fatalf("ExecuteParallel: %v", err)
	}
	// Gas is capped at GasLimit.
	if results[0].GasUsed != 22000 {
		t.Errorf("GasUsed = %d, want 22000", results[0].GasUsed)
	}
}

func TestNewBALSEngineMinParallel(t *testing.T) {
	engine := NewBALSEngine(0)
	if engine.MaxParallel() != 1 {
		t.Errorf("MaxParallel() = %d, want 1 (minimum)", engine.MaxParallel())
	}
}

func TestHasOverlapEmpty(t *testing.T) {
	if hasOverlap(nil, []types.Hash{slotHash(1)}) {
		t.Error("nil set should not overlap")
	}
	if hasOverlap([]types.Hash{slotHash(1)}, nil) {
		t.Error("nil set should not overlap")
	}
}

func TestBuildExecutionLevelsDiamond(t *testing.T) {
	// Diamond DAG: 0 -> 1, 0 -> 2, 1 -> 3, 2 -> 3.
	slot1 := slotHash(0x01)
	slot2 := slotHash(0x02)
	als := []BALSAccessList{
		{TxIndex: 0, WriteSet: []types.Hash{slot1, slot2}},
		{TxIndex: 1, ReadSet: []types.Hash{slot1}, WriteSet: []types.Hash{slotHash(0x03)}},
		{TxIndex: 2, ReadSet: []types.Hash{slot2}, WriteSet: []types.Hash{slotHash(0x04)}},
		{TxIndex: 3, ReadSet: []types.Hash{slotHash(0x03), slotHash(0x04)}},
	}

	graph := BuildDependencyGraph(als)
	order, err := TopologicalSort(graph, 4)
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}

	levels := buildExecutionLevels(order, graph)
	// Level 0: [0], Level 1: [1, 2], Level 2: [3].
	if len(levels) != 3 {
		t.Fatalf("levels = %d, want 3", len(levels))
	}
	if len(levels[0]) != 1 || levels[0][0] != 0 {
		t.Errorf("level 0 = %v, want [0]", levels[0])
	}
	if len(levels[1]) != 2 {
		t.Errorf("level 1 = %v, want 2 items", levels[1])
	}
	if len(levels[2]) != 1 || levels[2][0] != 3 {
		t.Errorf("level 2 = %v, want [3]", levels[2])
	}
}

func TestConflictEdgeOrdering(t *testing.T) {
	// Verify that TxA < TxB in conflict edges even when access lists
	// are provided in reverse index order.
	slot := slotHash(0x01)
	als := []BALSAccessList{
		{TxIndex: 5, WriteSet: []types.Hash{slot}},
		{TxIndex: 2, WriteSet: []types.Hash{slot}},
	}

	conflicts := DetectConflicts(als)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].TxA != 2 || conflicts[0].TxB != 5 {
		t.Errorf("conflict = (%d, %d), want (2, 5)", conflicts[0].TxA, conflicts[0].TxB)
	}
}
