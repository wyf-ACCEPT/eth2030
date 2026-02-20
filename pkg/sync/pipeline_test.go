package sync

import (
	"errors"
	"fmt"
	gosync "sync"
	"testing"
)

// 1. NewSyncPipeline creates a valid pipeline.
func TestNewSyncPipeline(t *testing.T) {
	cfg := DefaultPipelineConfig()
	p := NewSyncPipeline(cfg)
	if p == nil {
		t.Fatal("expected non-nil SyncPipeline")
	}
	if p.StageCount() != 0 {
		t.Fatalf("expected 0 stages, got %d", p.StageCount())
	}
}

// 2. AddStage registers stages correctly.
func TestAddStage(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	if err := p.AddStage("HeaderSync", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.StageCount() != 1 {
		t.Fatalf("expected 1 stage, got %d", p.StageCount())
	}

	stage, err := p.GetStage("HeaderSync")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stage.Status != StageStatusPending {
		t.Fatalf("expected pending, got %s", stage.Status)
	}
}

// 3. AddStage rejects duplicate names.
func TestAddStage_Duplicate(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)
	err := p.AddStage("A", nil)
	if err == nil {
		t.Fatal("expected error for duplicate stage name")
	}
}

// 4. StartStage transitions from pending to running.
func TestStartStage(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)

	if err := p.StartStage("A"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stage, _ := p.GetStage("A")
	if stage.Status != StageStatusRunning {
		t.Fatalf("expected running, got %s", stage.Status)
	}
	if stage.Attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", stage.Attempts)
	}
}

// 5. StartStage fails for unknown stage.
func TestStartStage_NotFound(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	err := p.StartStage("nonexistent")
	if !errors.Is(err, ErrPipelineStageNotFound) {
		t.Fatalf("expected ErrPipelineStageNotFound, got %v", err)
	}
}

// 6. StartStage fails when dependencies are not completed.
func TestStartStage_DependenciesNotMet(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)
	_ = p.AddStage("B", []string{"A"})

	err := p.StartStage("B")
	if !errors.Is(err, ErrPipelineStageDeps) {
		t.Fatalf("expected ErrPipelineStageDeps, got %v", err)
	}
}

// 7. StartStage succeeds when dependencies are completed.
func TestStartStage_DependenciesMet(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.MaxConcurrentStages = 0 // no limit
	p := NewSyncPipeline(cfg)
	_ = p.AddStage("A", nil)
	_ = p.AddStage("B", []string{"A"})

	_ = p.StartStage("A")
	_ = p.CompleteStage("A")

	if err := p.StartStage("B"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 8. CompleteStage marks a running stage as completed.
func TestCompleteStage(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)
	_ = p.StartStage("A")

	if err := p.CompleteStage("A"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stage, _ := p.GetStage("A")
	if stage.Status != StageStatusCompleted {
		t.Fatalf("expected completed, got %s", stage.Status)
	}
	if stage.Progress != 100.0 {
		t.Fatalf("expected 100%% progress, got %f", stage.Progress)
	}
}

// 9. CompleteStage fails for non-running stage.
func TestCompleteStage_NotRunning(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)

	err := p.CompleteStage("A")
	if !errors.Is(err, ErrPipelineStageActive) {
		t.Fatalf("expected ErrPipelineStageActive, got %v", err)
	}
}

// 10. FailStage marks a running stage as failed.
func TestFailStage(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)
	_ = p.StartStage("A")

	if err := p.FailStage("A", "timeout"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stage, _ := p.GetStage("A")
	if stage.Status != StageStatusFailed {
		t.Fatalf("expected failed, got %s", stage.Status)
	}
	if stage.Error != "timeout" {
		t.Fatalf("expected 'timeout', got %q", stage.Error)
	}
}

// 11. FailStage fails for unknown stage.
func TestFailStage_NotFound(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	err := p.FailStage("nonexistent", "reason")
	if !errors.Is(err, ErrPipelineStageNotFound) {
		t.Fatalf("expected ErrPipelineStageNotFound, got %v", err)
	}
}

// 12. RetryStage transitions from failed to pending.
func TestRetryStage(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)
	_ = p.StartStage("A")
	_ = p.FailStage("A", "error")

	if err := p.RetryStage("A"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stage, _ := p.GetStage("A")
	if stage.Status != StageStatusPending {
		t.Fatalf("expected pending after retry, got %s", stage.Status)
	}
	if stage.Error != "" {
		t.Fatalf("expected empty error after retry, got %q", stage.Error)
	}
}

// 13. RetryStage fails when retry limit exhausted.
func TestRetryStage_Exhausted(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.RetryLimit = 1
	p := NewSyncPipeline(cfg)
	_ = p.AddStage("A", nil)
	_ = p.StartStage("A") // attempt 1
	_ = p.FailStage("A", "error")

	err := p.RetryStage("A")
	if !errors.Is(err, ErrPipelineRetryExhausted) {
		t.Fatalf("expected ErrPipelineRetryExhausted, got %v", err)
	}
}

// 14. RetryStage fails when stage is not in failed state.
func TestRetryStage_NotFailed(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)

	err := p.RetryStage("A")
	if !errors.Is(err, ErrPipelineStageActive) {
		t.Fatalf("expected ErrPipelineStageActive, got %v", err)
	}
}

// 15. Progress returns a snapshot of all stages.
func TestProgress(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)
	_ = p.AddStage("B", []string{"A"})

	progress := p.Progress()
	if len(progress) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(progress))
	}
	if progress["A"] == nil || progress["B"] == nil {
		t.Fatal("expected both stages in progress map")
	}
}

// 16. OverallProgress calculates correctly.
func TestOverallProgress(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.MaxConcurrentStages = 0
	p := NewSyncPipeline(cfg)
	_ = p.AddStage("A", nil)
	_ = p.AddStage("B", nil)

	// Both at 0%.
	if prog := p.OverallProgress(); prog != 0 {
		t.Fatalf("expected 0%%, got %f", prog)
	}

	// Complete one stage (100% / 2 stages = 50%).
	_ = p.StartStage("A")
	_ = p.CompleteStage("A")

	prog := p.OverallProgress()
	if prog != 50.0 {
		t.Fatalf("expected 50%%, got %f", prog)
	}

	// Complete both (100%).
	_ = p.StartStage("B")
	_ = p.CompleteStage("B")

	prog = p.OverallProgress()
	if prog != 100.0 {
		t.Fatalf("expected 100%%, got %f", prog)
	}
}

// 17. IsComplete returns true only when all stages completed.
func TestIsComplete(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)
	_ = p.AddStage("B", []string{"A"})

	if p.IsComplete() {
		t.Fatal("expected not complete initially")
	}

	_ = p.StartStage("A")
	_ = p.CompleteStage("A")

	if p.IsComplete() {
		t.Fatal("expected not complete with B still pending")
	}

	_ = p.StartStage("B")
	_ = p.CompleteStage("B")

	if !p.IsComplete() {
		t.Fatal("expected complete after all stages done")
	}
}

// 18. IsComplete returns false for empty pipeline.
func TestIsComplete_Empty(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	if p.IsComplete() {
		t.Fatal("expected not complete for empty pipeline")
	}
}

// 19. Reset clears all stages.
func TestPipelineReset(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)
	_ = p.AddStage("B", nil)

	p.Reset()

	if p.StageCount() != 0 {
		t.Fatalf("expected 0 stages after reset, got %d", p.StageCount())
	}
	if len(p.StageNames()) != 0 {
		t.Fatal("expected empty stage names after reset")
	}
}

// 20. StageNames returns insertion order.
func TestStageNames(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("C", nil)
	_ = p.AddStage("A", nil)
	_ = p.AddStage("B", nil)

	names := p.StageNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "C" || names[1] != "A" || names[2] != "B" {
		t.Fatalf("unexpected order: %v", names)
	}
}

// 21. Concurrent stage limit enforced.
func TestMaxConcurrentStages(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.MaxConcurrentStages = 1
	p := NewSyncPipeline(cfg)
	_ = p.AddStage("A", nil)
	_ = p.AddStage("B", nil)

	_ = p.StartStage("A")

	err := p.StartStage("B")
	if !errors.Is(err, ErrPipelineStageActive) {
		t.Fatalf("expected ErrPipelineStageActive due to concurrency limit, got %v", err)
	}

	// After completing A, B should be startable.
	_ = p.CompleteStage("A")
	if err := p.StartStage("B"); err != nil {
		t.Fatalf("unexpected error after completing A: %v", err)
	}
}

// 22. Full pipeline lifecycle: HeaderSync -> BodySync -> Verification.
func TestFullPipelineLifecycle(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	stages := []string{PipelineStageHeaderSync, PipelineStageBodySync,
		PipelineStageReceiptSync, PipelineStageStateSync, PipelineStageVerification}
	for i, name := range stages {
		var deps []string
		if i > 0 {
			deps = []string{stages[i-1]}
		}
		_ = p.AddStage(name, deps)
	}
	for _, name := range stages {
		if err := p.StartStage(name); err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
		if err := p.CompleteStage(name); err != nil {
			t.Fatalf("complete %s: %v", name, err)
		}
	}
	if !p.IsComplete() {
		t.Fatal("expected pipeline to be complete")
	}
	if p.OverallProgress() != 100.0 {
		t.Fatalf("expected 100%%, got %f", p.OverallProgress())
	}
}

// 23. HasFailed detects failed stages.
func TestHasFailed(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)
	if p.HasFailed() {
		t.Fatal("expected no failures initially")
	}
	_ = p.StartStage("A")
	_ = p.FailStage("A", "broke")
	if !p.HasFailed() {
		t.Fatal("expected HasFailed to be true")
	}
}

// 24. UpdateStageProgress sets progress correctly.
func TestUpdateStageProgress(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)
	_ = p.StartStage("A")
	_ = p.UpdateStageProgress("A", 55.5)
	stage, _ := p.GetStage("A")
	if stage.Progress != 55.5 {
		t.Fatalf("expected 55.5, got %f", stage.Progress)
	}
}

// 25. UpdateStageProgress clamps values.
func TestUpdateStageProgress_Clamp(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	_ = p.AddStage("A", nil)
	_ = p.UpdateStageProgress("A", -10.0)
	s, _ := p.GetStage("A")
	if s.Progress != 0 {
		t.Fatalf("expected 0, got %f", s.Progress)
	}
	_ = p.UpdateStageProgress("A", 200.0)
	s, _ = p.GetStage("A")
	if s.Progress != 100 {
		t.Fatalf("expected 100, got %f", s.Progress)
	}
}

// 26. Thread safety: concurrent operations do not race.
func TestConcurrency(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.MaxConcurrentStages = 0 // no limit
	cfg.RetryLimit = 100
	p := NewSyncPipeline(cfg)

	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("stage-%d", i)
		_ = p.AddStage(name, nil)
	}

	var wg gosync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("stage-%d", idx)
			_ = p.StartStage(name)
			_ = p.UpdateStageProgress(name, 50.0)
			_ = p.Progress()
			_ = p.OverallProgress()
			_ = p.CompleteStage(name)
		}(i)
	}
	wg.Wait()

	if !p.IsComplete() {
		t.Fatal("expected all stages complete after concurrent execution")
	}
}

// 27. Retry-then-succeed lifecycle.
func TestRetryThenSucceed(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.RetryLimit = 3
	p := NewSyncPipeline(cfg)
	_ = p.AddStage("A", nil)

	// First attempt: fail.
	_ = p.StartStage("A")
	_ = p.FailStage("A", "err1")
	_ = p.RetryStage("A")

	// Second attempt: fail.
	_ = p.StartStage("A")
	_ = p.FailStage("A", "err2")
	_ = p.RetryStage("A")

	// Third attempt: succeed.
	_ = p.StartStage("A")
	_ = p.CompleteStage("A")

	stage, _ := p.GetStage("A")
	if stage.Status != StageStatusCompleted {
		t.Fatalf("expected completed, got %s", stage.Status)
	}
	if stage.Attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", stage.Attempts)
	}
}

// 28. OverallProgress with empty pipeline returns 0; GetStage fails for unknown.
func TestPipelineEdgeCases(t *testing.T) {
	p := NewSyncPipeline(DefaultPipelineConfig())
	if prog := p.OverallProgress(); prog != 0 {
		t.Fatalf("expected 0 for empty pipeline, got %f", prog)
	}
	_, err := p.GetStage("missing")
	if !errors.Is(err, ErrPipelineStageNotFound) {
		t.Fatalf("expected ErrPipelineStageNotFound, got %v", err)
	}
	// StageStatus.String() covers all values.
	for _, tc := range []struct {
		s    StageStatus
		want string
	}{
		{StageStatusPending, "pending"}, {StageStatusRunning, "running"},
		{StageStatusCompleted, "completed"}, {StageStatusFailed, "failed"},
		{StageStatus(99), "unknown(99)"},
	} {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("StageStatus(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}
