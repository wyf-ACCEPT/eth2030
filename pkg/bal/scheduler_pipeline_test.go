package bal

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// pipeTestBAL creates a BAL with:
// tx0 writes addr1/slot1, tx1 reads addr1/slot1 (conflict 0-1),
// tx2 independent, tx3 independent.
func pipeTestBAL() *BlockAccessList {
	bal := NewBlockAccessList()
	addr1 := types.HexToAddress("0xcc")
	slot1 := types.HexToHash("0x11")

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
		Address:     types.HexToAddress("0xdd"),
		AccessIndex: 3,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x22"), Value: types.HexToHash("0xee")},
		},
	})
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0xee"),
		AccessIndex: 4,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x33"), Value: types.HexToHash("0xff")},
		},
	})
	return bal
}

func TestNewPipelineScheduler(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	_, err := NewPipelineScheduler(0, detector)
	if err != ErrPipelineWorkers {
		t.Errorf("expected ErrPipelineWorkers, got %v", err)
	}

	sched, err := NewPipelineScheduler(4, detector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sched == nil {
		t.Fatal("scheduler should not be nil")
	}
}

func TestPipelineBuildPlan(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	sched, _ := NewPipelineScheduler(2, detector)

	gasLimits := map[int]uint64{0: 50000, 1: 30000, 2: 40000, 3: 35000}
	plan, err := sched.BuildPlan(pipeTestBAL(), gasLimits)
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}
	if plan == nil {
		t.Fatal("plan should not be nil")
	}
	if plan.TotalTx != 4 {
		t.Errorf("TotalTx = %d, want 4", plan.TotalTx)
	}
	if plan.WorkerCount != 2 {
		t.Errorf("WorkerCount = %d, want 2", plan.WorkerCount)
	}
	if len(plan.Stages) == 0 {
		t.Fatal("expected at least one stage")
	}
	// tx0 and tx1 conflict, so we need at least 2 stages.
	if len(plan.Stages) < 2 {
		t.Errorf("stages = %d, expected >= 2 due to conflict", len(plan.Stages))
	}

	// Verify all transactions appear exactly once across all stages.
	seen := make(map[int]bool)
	for _, stage := range plan.Stages {
		for _, batch := range stage.Batches {
			for _, task := range batch.Tasks {
				if seen[task.TxIndex] {
					t.Errorf("tx %d appears in multiple stages", task.TxIndex)
				}
				seen[task.TxIndex] = true
			}
		}
	}
	for i := 0; i < 4; i++ {
		if !seen[i] {
			t.Errorf("tx %d not found in any stage", i)
		}
	}
}

func TestPipelineBuildPlanNilBAL(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	sched, _ := NewPipelineScheduler(2, detector)
	_, err := sched.BuildPlan(nil, nil)
	if err != ErrPipelineEmpty {
		t.Errorf("expected ErrPipelineEmpty, got %v", err)
	}
}

func TestPipelineBuildPlanDefaultGas(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	sched, _ := NewPipelineScheduler(2, detector)

	// Pass nil gasLimits; should use default 21000.
	plan, err := sched.BuildPlan(pipeTestBAL(), nil)
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}
	if plan == nil {
		t.Fatal("plan should not be nil")
	}
	// Verify tasks use default gas.
	for _, stage := range plan.Stages {
		for _, batch := range stage.Batches {
			for _, task := range batch.Tasks {
				if task.GasLimit != 21000 {
					t.Errorf("tx %d gas = %d, expected 21000 default", task.TxIndex, task.GasLimit)
				}
			}
		}
	}
}

func TestPipelineBuildPlanSingleWorker(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	sched, _ := NewPipelineScheduler(1, detector)

	plan, err := sched.BuildPlan(pipeTestBAL(), nil)
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}
	// With one worker, each stage should have exactly one batch.
	for _, stage := range plan.Stages {
		if len(stage.Batches) != 1 {
			t.Errorf("stage %d has %d batches, want 1 with single worker",
				stage.StageID, len(stage.Batches))
		}
	}
}

func TestPipelineBuildPlanNoConflicts(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	sched, _ := NewPipelineScheduler(4, detector)

	bal := NewBlockAccessList()
	for i := uint64(1); i <= 4; i++ {
		bal.AddEntry(AccessEntry{
			Address:     types.BytesToAddress([]byte{byte(i)}),
			AccessIndex: i,
			StorageReads: []StorageAccess{
				{Slot: types.BytesToHash([]byte{byte(i)}), Value: types.HexToHash("0x01")},
			},
		})
	}

	plan, err := sched.BuildPlan(bal, nil)
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}
	// No conflicts, so all txs should be in one stage.
	if len(plan.Stages) != 1 {
		t.Errorf("stages = %d, want 1 for conflict-free block", len(plan.Stages))
	}
}

func TestBuildConflictFreeBatches(t *testing.T) {
	batches := BuildConflictFreeBatches(pipeTestBAL())
	if len(batches) == 0 {
		t.Fatal("expected at least one batch")
	}

	// Verify all txs are present.
	seen := make(map[int]bool)
	for _, batch := range batches {
		for _, idx := range batch {
			seen[idx] = true
		}
	}
	for i := 0; i < 4; i++ {
		if !seen[i] {
			t.Errorf("tx %d not found in batches", i)
		}
	}

	// Verify no conflicts within any batch.
	detector := NewBALConflictDetector(StrategySerialize)
	conflicts := detector.DetectConflicts(pipeTestBAL())
	conflictSet := make(map[[2]int]struct{})
	for _, c := range conflicts {
		conflictSet[[2]int{c.TxA, c.TxB}] = struct{}{}
		conflictSet[[2]int{c.TxB, c.TxA}] = struct{}{}
	}
	for _, batch := range batches {
		for i := 0; i < len(batch); i++ {
			for j := i + 1; j < len(batch); j++ {
				pair := [2]int{batch[i], batch[j]}
				if _, ok := conflictSet[pair]; ok {
					t.Errorf("batch contains conflicting txs %d and %d", batch[i], batch[j])
				}
			}
		}
	}
}

func TestBuildConflictFreeBatchesNil(t *testing.T) {
	if BuildConflictFreeBatches(nil) != nil {
		t.Error("nil BAL should produce nil batches")
	}
}

func TestBuildConflictFreeBatchesEmpty(t *testing.T) {
	bal := NewBlockAccessList()
	if BuildConflictFreeBatches(bal) != nil {
		t.Error("empty BAL should produce nil batches")
	}
}

func TestGasBalanceRatio(t *testing.T) {
	stage := PipelineStage{
		Batches: []PipelineBatch{
			{WorkerID: 0, GasSum: 100000},
			{WorkerID: 1, GasSum: 50000},
		},
	}
	ratio := GasBalanceRatio(stage)
	expected := 0.5
	if ratio != expected {
		t.Errorf("GasBalanceRatio = %f, want %f", ratio, expected)
	}
}

func TestGasBalanceRatioSingleBatch(t *testing.T) {
	stage := PipelineStage{
		Batches: []PipelineBatch{
			{WorkerID: 0, GasSum: 100000},
		},
	}
	if GasBalanceRatio(stage) != 1.0 {
		t.Error("single batch should have ratio 1.0")
	}
}

func TestGasBalanceRatioEmpty(t *testing.T) {
	stage := PipelineStage{}
	if GasBalanceRatio(stage) != 1.0 {
		t.Error("empty stage should have ratio 1.0")
	}
}

func TestGasBalanceRatioZeroGas(t *testing.T) {
	stage := PipelineStage{
		Batches: []PipelineBatch{
			{WorkerID: 0, GasSum: 0},
			{WorkerID: 1, GasSum: 0},
		},
	}
	if GasBalanceRatio(stage) != 1.0 {
		t.Error("zero gas should have ratio 1.0")
	}
}

func TestPipelineEfficiency(t *testing.T) {
	plan := &PipelinePlan{
		Stages:  make([]PipelineStage, 2),
		TotalTx: 8,
	}
	eff := PipelineEfficiency(plan)
	if eff != 4.0 {
		t.Errorf("PipelineEfficiency = %f, want 4.0", eff)
	}
}

func TestPipelineEfficiencyNil(t *testing.T) {
	if PipelineEfficiency(nil) != 0.0 {
		t.Error("nil plan should have 0.0 efficiency")
	}
}

func TestPipelineEfficiencyNoTx(t *testing.T) {
	plan := &PipelinePlan{TotalTx: 0}
	if PipelineEfficiency(plan) != 0.0 {
		t.Error("empty plan should have 0.0 efficiency")
	}
}
