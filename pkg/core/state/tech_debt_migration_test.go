package state

import (
	"errors"
	"testing"
)

// makeTestStep creates a simple migration step for testing.
func makeTestStep(name string, version int, mtype MigrationType, deps ...string) *MigrationStep {
	return &MigrationStep{
		Name:        name,
		Version:     version,
		Type:        mtype,
		Description: "test step: " + name,
		DependsOn:   deps,
		Apply: func(db StateDB) error {
			return nil
		},
		Rollback: func(db StateDB) error {
			return nil
		},
	}
}

func TestMigrationPlanCreate(t *testing.T) {
	steps := []*MigrationStep{
		makeTestStep("step1", 1, MigrationRemoveField),
		makeTestStep("step2", 2, MigrationConvertType),
	}
	plan, err := NewMigrationPlan(2, steps, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Version != 2 {
		t.Errorf("expected version 2, got %d", plan.Version)
	}
	if len(plan.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(plan.Steps))
	}
	if plan.DryRun {
		t.Error("expected DryRun=false")
	}
	if plan.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestMigrationPlanValidateErrors(t *testing.T) {
	// Nil step.
	_, err := NewMigrationPlan(1, []*MigrationStep{nil}, false)
	if err != ErrMigrationNilStep {
		t.Errorf("expected ErrMigrationNilStep, got %v", err)
	}

	// Empty name.
	badStep := makeTestStep("", 1, MigrationPurge)
	_, err = NewMigrationPlan(1, []*MigrationStep{badStep}, false)
	if err != ErrMigrationEmptyName {
		t.Errorf("expected ErrMigrationEmptyName, got %v", err)
	}

	// Nil apply.
	noApply := &MigrationStep{Name: "no_apply", Version: 1, Type: MigrationPurge}
	_, err = NewMigrationPlan(1, []*MigrationStep{noApply}, false)
	if err == nil {
		t.Error("expected error for nil apply function")
	}

	// Duplicate name.
	dup := []*MigrationStep{
		makeTestStep("dup", 1, MigrationPurge),
		makeTestStep("dup", 2, MigrationPurge),
	}
	_, err = NewMigrationPlan(2, dup, false)
	if !errors.Is(err, ErrMigrationDuplicate) {
		t.Errorf("expected ErrMigrationDuplicate, got %v", err)
	}

	// Missing dependency.
	missing := []*MigrationStep{
		makeTestStep("s1", 1, MigrationPurge, "nonexistent"),
	}
	_, err = NewMigrationPlan(1, missing, false)
	if !errors.Is(err, ErrMigrationDepNotFound) {
		t.Errorf("expected ErrMigrationDepNotFound, got %v", err)
	}
}

func TestMigrationEngineExecute(t *testing.T) {
	engine := NewMigrationEngine()
	state := NewMemoryStateDB()

	steps := []*MigrationStep{
		makeTestStep("s1", 1, MigrationRemoveField),
		makeTestStep("s2", 2, MigrationConvertType, "s1"),
	}
	plan, err := NewMigrationPlan(2, steps, false)
	if err != nil {
		t.Fatalf("plan creation failed: %v", err)
	}

	report, err := engine.Execute(plan, state)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if report.Applied != 2 {
		t.Errorf("expected 2 applied, got %d", report.Applied)
	}
	if report.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", report.Failed)
	}
	if report.Duration <= 0 {
		t.Error("expected positive duration")
	}
	if engine.AppliedCount() != 2 {
		t.Errorf("expected 2 applied steps tracked, got %d", engine.AppliedCount())
	}
}

func TestMigrationEngineExecuteSkipApplied(t *testing.T) {
	engine := NewMigrationEngine()
	state := NewMemoryStateDB()

	steps := []*MigrationStep{
		makeTestStep("s1", 1, MigrationPurge),
	}
	plan, _ := NewMigrationPlan(1, steps, false)

	// First execution.
	_, _ = engine.Execute(plan, state)
	if !engine.IsApplied("s1") {
		t.Fatal("s1 should be applied")
	}

	// Second execution should skip.
	report, _ := engine.Execute(plan, state)
	if report.Applied != 0 {
		t.Errorf("expected 0 applied on re-run, got %d", report.Applied)
	}
	if report.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", report.Skipped)
	}
}

func TestMigrationEngineValidate(t *testing.T) {
	engine := NewMigrationEngine()

	if err := engine.Validate(nil); err != ErrMigrationNilPlan {
		t.Errorf("expected ErrMigrationNilPlan, got %v", err)
	}

	steps := []*MigrationStep{
		makeTestStep("ok", 1, MigrationPurge),
	}
	plan, _ := NewMigrationPlan(1, steps, false)
	if err := engine.Validate(plan); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestMigrationEngineRollback(t *testing.T) {
	engine := NewMigrationEngine()
	state := NewMemoryStateDB()

	steps := []*MigrationStep{
		makeTestStep("r1", 1, MigrationRemoveField),
		makeTestStep("r2", 2, MigrationConvertType),
	}
	plan, _ := NewMigrationPlan(2, steps, false)
	_, _ = engine.Execute(plan, state)

	if engine.AppliedCount() != 2 {
		t.Fatalf("expected 2 applied before rollback, got %d", engine.AppliedCount())
	}

	report, err := engine.Rollback(state)
	if err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	if report.Applied != 2 {
		t.Errorf("expected 2 rolled back, got %d", report.Applied)
	}
	if engine.AppliedCount() != 0 {
		t.Errorf("expected 0 applied after rollback, got %d", engine.AppliedCount())
	}
}

func TestMigrationEngineRollbackNoRollbackFunc(t *testing.T) {
	engine := NewMigrationEngine()
	state := NewMemoryStateDB()

	step := &MigrationStep{
		Name:    "no_rollback",
		Version: 1,
		Type:    MigrationPurge,
		Apply:   func(db StateDB) error { return nil },
		// No Rollback function.
	}
	plan, _ := NewMigrationPlan(1, []*MigrationStep{step}, false)
	_, _ = engine.Execute(plan, state)

	report, _ := engine.Rollback(state)
	if report.Skipped != 1 {
		t.Errorf("expected 1 skipped (no rollback func), got %d", report.Skipped)
	}
}

func TestMigrationEngineDryRun(t *testing.T) {
	engine := NewMigrationEngine()
	state := NewMemoryStateDB()

	steps := []*MigrationStep{
		makeTestStep("dry1", 1, MigrationPurge),
		makeTestStep("dry2", 2, MigrationPurge),
	}
	plan, _ := NewMigrationPlan(2, steps, true)

	report, err := engine.Execute(plan, state)
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}
	if report.Applied != 0 {
		t.Errorf("expected 0 applied in dry run, got %d", report.Applied)
	}
	if report.Skipped != 2 {
		t.Errorf("expected 2 skipped in dry run, got %d", report.Skipped)
	}
	if !report.DryRun {
		t.Error("expected DryRun flag in report")
	}
	if engine.AppliedCount() != 0 {
		t.Errorf("expected 0 applied after dry run, got %d", engine.AppliedCount())
	}
}

func TestMigrationVersioning(t *testing.T) {
	steps := DefaultMigrationSteps()
	plan, err := NewMigrationPlan(3, steps, false)
	if err != nil {
		t.Fatalf("default steps plan failed: %v", err)
	}
	if plan.Version != 3 {
		t.Errorf("expected version 3, got %d", plan.Version)
	}
	if len(plan.Steps) != 3 {
		t.Errorf("expected 3 default steps, got %d", len(plan.Steps))
	}

	// Verify step versions are monotonically assigned.
	for i, step := range plan.Steps {
		if step.Version != i+1 {
			t.Errorf("step %d: expected version %d, got %d", i, i+1, step.Version)
		}
	}
}

func TestMigrationDependencies(t *testing.T) {
	// Steps with dependency chain: A -> B -> C.
	steps := []*MigrationStep{
		makeTestStep("C", 3, MigrationPurge, "B"),
		makeTestStep("B", 2, MigrationConvertType, "A"),
		makeTestStep("A", 1, MigrationRemoveField),
	}

	plan, err := NewMigrationPlan(3, steps, false)
	if err != nil {
		t.Fatalf("dependency plan failed: %v", err)
	}

	engine := NewMigrationEngine()
	state := NewMemoryStateDB()
	report, err := engine.Execute(plan, state)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if report.Applied != 3 {
		t.Errorf("expected 3 applied, got %d", report.Applied)
	}
}

func TestMigrationDependencyCycle(t *testing.T) {
	steps := []*MigrationStep{
		makeTestStep("X", 1, MigrationPurge, "Y"),
		makeTestStep("Y", 2, MigrationPurge, "X"),
	}
	_, err := NewMigrationPlan(2, steps, false)
	if !errors.Is(err, ErrMigrationCycle) {
		t.Errorf("expected ErrMigrationCycle, got %v", err)
	}
}

func TestMigrationReport(t *testing.T) {
	failStep := &MigrationStep{
		Name:    "fail_step",
		Version: 1,
		Type:    MigrationPurge,
		Apply: func(db StateDB) error {
			return errors.New("deliberate failure")
		},
	}

	engine := NewMigrationEngine()
	state := NewMemoryStateDB()
	plan, _ := NewMigrationPlan(1, []*MigrationStep{failStep}, false)

	report, _ := engine.Execute(plan, state)
	if report.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", report.Failed)
	}
	if report.Applied != 0 {
		t.Errorf("expected 0 applied, got %d", report.Applied)
	}
	if len(report.Errors) != 1 {
		t.Errorf("expected 1 error in report, got %d", len(report.Errors))
	}
	if _, ok := report.Errors["fail_step"]; !ok {
		t.Error("expected error for fail_step in report")
	}
}

func TestMigrationTypeString(t *testing.T) {
	tests := []struct {
		mt   MigrationType
		want string
	}{
		{MigrationRemoveField, "RemoveField"},
		{MigrationRenameField, "RenameField"},
		{MigrationConvertType, "ConvertType"},
		{MigrationPurge, "Purge"},
		{MigrationType(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.mt.String(); got != tt.want {
			t.Errorf("MigrationType(%d).String() = %q, want %q", tt.mt, got, tt.want)
		}
	}
}

func TestMigrationEngineNilState(t *testing.T) {
	engine := NewMigrationEngine()
	steps := []*MigrationStep{makeTestStep("s", 1, MigrationPurge)}
	plan, _ := NewMigrationPlan(1, steps, false)

	_, err := engine.Execute(plan, nil)
	if err != ErrMigrationNilState {
		t.Errorf("expected ErrMigrationNilState, got %v", err)
	}

	_, err = engine.Rollback(nil)
	if err != ErrMigrationNilState {
		t.Errorf("expected ErrMigrationNilState for rollback, got %v", err)
	}
}
