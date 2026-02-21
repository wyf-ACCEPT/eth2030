package trie

import (
	"sync"
	"testing"
)

func TestMigrationPlannerNew(t *testing.T) {
	mp := NewMigrationPlanner(nil)
	if mp == nil {
		t.Fatal("expected non-nil planner")
	}
	if mp.PlanCount() != 0 {
		t.Errorf("expected 0 plans, got %d", mp.PlanCount())
	}
	// Also test with explicit config.
	cfg := &PlannerConfig{SourceType: "mpt", TargetType: "binary", BatchSize: 500, MaxDepth: 128, EstimatedAccounts: 5000}
	mp2 := NewMigrationPlanner(cfg)
	if mp2 == nil {
		t.Fatal("expected non-nil planner with config")
	}
}

func TestMigrationPlannerCreatePlan(t *testing.T) {
	cfg := &PlannerConfig{SourceType: "mpt", TargetType: "binary", BatchSize: 1000, MaxDepth: 256, EstimatedAccounts: 3000}
	mp := NewMigrationPlanner(cfg)
	var root [32]byte
	root[0] = 0xAA
	plan, err := mp.CreatePlan(root)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan == nil {
		t.Fatal("expected non-nil plan")
	}
	if len(plan.Phases) != 3 {
		t.Errorf("expected 3 phases, got %d", len(plan.Phases))
	}
	if plan.TotalAccounts != 3000 {
		t.Errorf("expected 3000 accounts, got %d", plan.TotalAccounts)
	}
	if plan.EstimatedGas == 0 || plan.EstimatedBlocks == 0 || plan.CreatedAt == 0 {
		t.Error("expected non-zero gas, blocks, and timestamp")
	}
	if mp.PlanCount() != 1 {
		t.Errorf("expected 1 plan, got %d", mp.PlanCount())
	}
}

func TestMigrationPlannerExecutePhase(t *testing.T) {
	mp := NewMigrationPlanner(&PlannerConfig{BatchSize: 1000, EstimatedAccounts: 2000})
	var root [32]byte
	plan, _ := mp.CreatePlan(root)
	result, err := mp.ExecutePhase(plan.ID, 0)
	if err != nil {
		t.Fatalf("ExecutePhase: %v", err)
	}
	if result.AccountsMigrated != 1000 {
		t.Errorf("expected 1000 accounts migrated, got %d", result.AccountsMigrated)
	}
	if result.GasUsed == 0 || len(result.Errors) != 0 {
		t.Error("expected non-zero gas and no errors")
	}
	if plan.Phases[0].Status != "done" {
		t.Errorf("expected status 'done', got %q", plan.Phases[0].Status)
	}
}

func TestMigrationPlannerExecutePhaseInvalidPlan(t *testing.T) {
	mp := NewMigrationPlanner(nil)
	_, err := mp.ExecutePhase(99, 0)
	if err != ErrPlannerPlanNotFound {
		t.Errorf("expected ErrPlannerPlanNotFound, got %v", err)
	}
}

func TestMigrationPlannerExecutePhaseInvalidPhase(t *testing.T) {
	mp := NewMigrationPlanner(nil)
	var root [32]byte
	plan, _ := mp.CreatePlan(root)
	_, err := mp.ExecutePhase(plan.ID, 99)
	if err != ErrPlannerPhaseInvalid {
		t.Errorf("expected ErrPlannerPhaseInvalid, got %v", err)
	}
}

func TestMigrationPlannerValidatePhase(t *testing.T) {
	mp := NewMigrationPlanner(&PlannerConfig{BatchSize: 500, EstimatedAccounts: 1000})
	var root [32]byte
	plan, _ := mp.CreatePlan(root)
	// Validate before execution should fail.
	if err := mp.ValidatePhase(plan.ID, 0); err != ErrPlannerPhaseNotDone {
		t.Errorf("expected ErrPlannerPhaseNotDone, got %v", err)
	}
	// Execute and validate.
	mp.ExecutePhase(plan.ID, 0)
	if err := mp.ValidatePhase(plan.ID, 0); err != nil {
		t.Fatalf("ValidatePhase after execute: %v", err)
	}
}

func TestMigrationPlannerValidatePhaseInvalidPlan(t *testing.T) {
	mp := NewMigrationPlanner(nil)
	err := mp.ValidatePhase(99, 0)
	if err != ErrPlannerPlanNotFound {
		t.Errorf("expected ErrPlannerPlanNotFound, got %v", err)
	}
}

func TestMigrationPlannerProgress(t *testing.T) {
	mp := NewMigrationPlanner(&PlannerConfig{BatchSize: 1000, EstimatedAccounts: 3000})
	var root [32]byte
	plan, _ := mp.CreatePlan(root)
	progress := mp.Progress(plan.ID)
	if progress.CompletedPhases != 0 || progress.TotalPhases != 3 || progress.Percentage != 0.0 {
		t.Errorf("initial progress wrong: %+v", progress)
	}
	mp.ExecutePhase(plan.ID, 0)
	progress = mp.Progress(plan.ID)
	if progress.CompletedPhases != 1 {
		t.Errorf("expected 1 completed, got %d", progress.CompletedPhases)
	}
	// Execute all remaining phases.
	mp.ExecutePhase(plan.ID, 1)
	mp.ExecutePhase(plan.ID, 2)
	progress = mp.Progress(plan.ID)
	if progress.CompletedPhases != 3 || progress.Percentage != 100.0 {
		t.Errorf("expected 3/100%%, got %d/%.1f%%", progress.CompletedPhases, progress.Percentage)
	}
}

func TestMigrationPlannerProgressInvalidPlan(t *testing.T) {
	mp := NewMigrationPlanner(nil)
	progress := mp.Progress(99)
	if progress.TotalPhases != 0 {
		t.Errorf("expected 0 total phases for invalid plan, got %d", progress.TotalPhases)
	}
}

func TestMigrationPlannerActivePlans(t *testing.T) {
	mp := NewMigrationPlanner(&PlannerConfig{BatchSize: 1000, EstimatedAccounts: 1000})
	var root [32]byte
	mp.CreatePlan(root)
	root[0] = 0x01
	mp.CreatePlan(root)
	if active := mp.ActivePlans(); len(active) != 2 {
		t.Errorf("expected 2 active plans, got %d", len(active))
	}
	mp.ExecutePhase(0, 0) // complete plan 0
	if active := mp.ActivePlans(); len(active) != 1 {
		t.Errorf("expected 1 active plan, got %d", len(active))
	}
}

func TestMigrationPlannerPlanConfig(t *testing.T) {
	cfg := &PlannerConfig{
		SourceType: "mpt",
		TargetType: "binary",
		BatchSize:  2000,
		MaxDepth:   128,
	}
	mp := NewMigrationPlanner(cfg)

	var root [32]byte
	plan, _ := mp.CreatePlan(root)
	if plan.Config.SourceType != "mpt" {
		t.Errorf("expected source 'mpt', got %q", plan.Config.SourceType)
	}
	if plan.Config.TargetType != "binary" {
		t.Errorf("expected target 'binary', got %q", plan.Config.TargetType)
	}
	if plan.Config.BatchSize != 2000 {
		t.Errorf("expected batch 2000, got %d", plan.Config.BatchSize)
	}
}

func TestMigrationPlannerEmptyState(t *testing.T) {
	mp := NewMigrationPlanner(&PlannerConfig{BatchSize: 1000, EstimatedAccounts: 0})
	var root [32]byte
	plan, err := mp.CreatePlan(root)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if len(plan.Phases) != 1 {
		t.Errorf("expected 1 phase for empty state, got %d", len(plan.Phases))
	}
}

func TestMigrationPlannerLargeState(t *testing.T) {
	mp := NewMigrationPlanner(&PlannerConfig{BatchSize: 1000, EstimatedAccounts: 100000})
	var root [32]byte
	plan, _ := mp.CreatePlan(root)
	if len(plan.Phases) != 100 {
		t.Errorf("expected 100 phases, got %d", len(plan.Phases))
	}
	if plan.TotalAccounts != 100000 {
		t.Errorf("expected 100000 accounts, got %d", plan.TotalAccounts)
	}
}

func TestMigrationPlannerBatchSize(t *testing.T) {
	mp := NewMigrationPlanner(&PlannerConfig{BatchSize: 500, EstimatedAccounts: 1500})
	var root [32]byte
	plan, _ := mp.CreatePlan(root)
	if len(plan.Phases) != 3 {
		t.Errorf("expected 3 phases for 1500/500, got %d", len(plan.Phases))
	}
}

func TestMigrationPlannerPhaseStatus(t *testing.T) {
	mp := NewMigrationPlanner(&PlannerConfig{BatchSize: 1000, EstimatedAccounts: 2000})
	var root [32]byte
	plan, _ := mp.CreatePlan(root)
	for i, p := range plan.Phases {
		if p.Status != "pending" {
			t.Errorf("phase %d: expected 'pending', got %q", i, p.Status)
		}
	}
	mp.ExecutePhase(plan.ID, 0)
	if plan.Phases[0].Status != "done" || plan.Phases[1].Status != "pending" {
		t.Error("phase status mismatch after executing phase 0")
	}
}

func TestMigrationPlannerMultiPlan(t *testing.T) {
	mp := NewMigrationPlanner(&PlannerConfig{BatchSize: 1000, EstimatedAccounts: 2000})
	var root1, root2 [32]byte
	root1[0] = 0x11
	root2[0] = 0x22
	plan1, _ := mp.CreatePlan(root1)
	plan2, _ := mp.CreatePlan(root2)
	if plan1.ID == plan2.ID {
		t.Error("plans should have different IDs")
	}
	if mp.PlanCount() != 2 {
		t.Errorf("expected 2 plans, got %d", mp.PlanCount())
	}
	mp.ExecutePhase(plan1.ID, 0)
	if p := mp.Progress(plan1.ID); p.CompletedPhases != 1 {
		t.Errorf("plan1: expected 1 completed, got %d", p.CompletedPhases)
	}
	if p := mp.Progress(plan2.ID); p.CompletedPhases != 0 {
		t.Errorf("plan2: expected 0 completed, got %d", p.CompletedPhases)
	}
}

func TestMigrationPlannerConcurrentPhases(t *testing.T) {
	mp := NewMigrationPlanner(&PlannerConfig{BatchSize: 500, EstimatedAccounts: 5000})
	var root [32]byte
	plan, _ := mp.CreatePlan(root)
	var wg sync.WaitGroup
	for i := 0; i < len(plan.Phases); i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if _, err := mp.ExecutePhase(plan.ID, idx); err != nil {
				t.Errorf("concurrent ExecutePhase(%d): %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
	progress := mp.Progress(plan.ID)
	if progress.CompletedPhases != len(plan.Phases) || progress.Percentage != 100.0 {
		t.Errorf("expected all done/100%%, got %d/%.1f%%", progress.CompletedPhases, progress.Percentage)
	}
}

func TestMigrationPlannerErrorHandling(t *testing.T) {
	mp := NewMigrationPlanner(nil)
	if _, err := mp.ExecutePhase(-1, 0); err != ErrPlannerPlanNotFound {
		t.Errorf("expected ErrPlannerPlanNotFound, got %v", err)
	}
	if err := mp.ValidatePhase(-1, 0); err != ErrPlannerPlanNotFound {
		t.Errorf("expected ErrPlannerPlanNotFound, got %v", err)
	}
	var root [32]byte
	plan, _ := mp.CreatePlan(root)
	if _, err := mp.ExecutePhase(plan.ID, -1); err != ErrPlannerPhaseInvalid {
		t.Errorf("expected ErrPlannerPhaseInvalid, got %v", err)
	}
	if err := mp.ValidatePhase(plan.ID, -1); err != ErrPlannerPhaseInvalid {
		t.Errorf("expected ErrPlannerPhaseInvalid, got %v", err)
	}
}

func TestMigrationPlannerPlanStructure(t *testing.T) {
	mp := NewMigrationPlanner(&PlannerConfig{BatchSize: 1000, EstimatedAccounts: 2000})
	var root [32]byte
	root[0] = 0xDE
	plan, _ := mp.CreatePlan(root)
	for i, p := range plan.Phases {
		if p.Name == "" {
			t.Errorf("phase %d has empty name", i)
		}
	}
	// Verify last phase ends at 0xFF.
	lastPhase := plan.Phases[len(plan.Phases)-1]
	for j := 0; j < 32; j++ {
		if lastPhase.EndKey[j] != 0xFF {
			t.Errorf("last phase end key byte %d should be 0xFF, got 0x%02x", j, lastPhase.EndKey[j])
			break
		}
	}
	if plan.EstimatedGas != uint64(2000*5200) {
		t.Errorf("expected gas %d, got %d", uint64(2000*5200), plan.EstimatedGas)
	}
}

func TestMigrationPlannerGetPlan(t *testing.T) {
	mp := NewMigrationPlanner(nil)
	var root [32]byte
	plan, _ := mp.CreatePlan(root)

	got := mp.GetPlan(plan.ID)
	if got == nil {
		t.Fatal("GetPlan returned nil for valid ID")
	}
	if got.ID != plan.ID {
		t.Errorf("GetPlan ID mismatch: want %d, got %d", plan.ID, got.ID)
	}

	got = mp.GetPlan(99)
	if got != nil {
		t.Error("GetPlan should return nil for invalid ID")
	}
}

func TestMigrationPlannerGetPhaseResult(t *testing.T) {
	mp := NewMigrationPlanner(&PlannerConfig{
		BatchSize:         1000,
		EstimatedAccounts: 1000,
	})

	var root [32]byte
	plan, _ := mp.CreatePlan(root)

	// No result before execution.
	result := mp.GetPhaseResult(plan.ID, 0)
	if result != nil {
		t.Error("expected nil result before execution")
	}

	mp.ExecutePhase(plan.ID, 0)
	result = mp.GetPhaseResult(plan.ID, 0)
	if result == nil {
		t.Fatal("expected non-nil result after execution")
	}
	if result.AccountsMigrated == 0 {
		t.Error("expected non-zero accounts migrated")
	}
}

func TestMigrationPlannerDefaultConfig(t *testing.T) {
	cfg := DefaultPlannerConfig()
	if cfg.SourceType != "mpt" {
		t.Errorf("default source type: want 'mpt', got %q", cfg.SourceType)
	}
	if cfg.TargetType != "binary" {
		t.Errorf("default target type: want 'binary', got %q", cfg.TargetType)
	}
	if cfg.BatchSize != 1000 {
		t.Errorf("default batch size: want 1000, got %d", cfg.BatchSize)
	}
	if cfg.MaxDepth != 256 {
		t.Errorf("default max depth: want 256, got %d", cfg.MaxDepth)
	}
}
