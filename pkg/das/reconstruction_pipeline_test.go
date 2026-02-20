package das

import (
	"testing"
)

func TestCellCollectorInitAndAddCell(t *testing.T) {
	cc := NewCellCollector()
	var commitment KZGCommitment
	cc.InitBlob(100, 0, commitment, PriorityNormal)

	var cell Cell
	copy(cell[:], []byte("test-cell-data"))

	err := cc.AddCell(100, 0, 5, cell)
	if err != nil {
		t.Fatalf("AddCell failed: %v", err)
	}
	if cc.CellCount(100, 0) != 1 {
		t.Errorf("expected 1 cell, got %d", cc.CellCount(100, 0))
	}
}

func TestCellCollectorDuplicateCell(t *testing.T) {
	cc := NewCellCollector()
	var commitment KZGCommitment
	cc.InitBlob(100, 0, commitment, PriorityNormal)

	var cell Cell
	if err := cc.AddCell(100, 0, 5, cell); err != nil {
		t.Fatalf("first AddCell: %v", err)
	}
	err := cc.AddCell(100, 0, 5, cell)
	if err != ErrPipelineDuplicateCell {
		t.Errorf("expected ErrPipelineDuplicateCell, got %v", err)
	}
}

func TestCellCollectorAddToUninitializedBlob(t *testing.T) {
	cc := NewCellCollector()
	var cell Cell
	err := cc.AddCell(100, 99, 0, cell)
	if err == nil {
		t.Error("expected error for uninitialized blob")
	}
}

func TestCellCollectorIsReady(t *testing.T) {
	cc := NewCellCollector()
	var commitment KZGCommitment
	cc.InitBlob(100, 0, commitment, PriorityNormal)

	// Not ready with 0 cells.
	if cc.IsReady(100, 0) {
		t.Error("should not be ready with 0 cells")
	}

	// Add ReconstructionThreshold cells.
	for i := uint64(0); i < uint64(ReconstructionThreshold); i++ {
		var cell Cell
		cell[0] = byte(i)
		if err := cc.AddCell(100, 0, i, cell); err != nil {
			t.Fatalf("AddCell %d: %v", i, err)
		}
	}

	if !cc.IsReady(100, 0) {
		t.Errorf("should be ready with %d cells", ReconstructionThreshold)
	}
}

func TestCellCollectorGetState(t *testing.T) {
	cc := NewCellCollector()
	var commitment KZGCommitment
	commitment[0] = 0xAB
	cc.InitBlob(50, 3, commitment, PriorityHigh)

	state := cc.GetState(50, 3)
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state.BlobIndex != 3 {
		t.Errorf("expected blob index 3, got %d", state.BlobIndex)
	}
	if state.Slot != 50 {
		t.Errorf("expected slot 50, got %d", state.Slot)
	}
	if state.Priority != PriorityHigh {
		t.Errorf("expected priority high, got %d", state.Priority)
	}
	if state.Commitment[0] != 0xAB {
		t.Errorf("expected commitment[0]=0xAB, got 0x%X", state.Commitment[0])
	}
}

func TestCellCollectorMarkReconstructed(t *testing.T) {
	cc := NewCellCollector()
	var commitment KZGCommitment
	cc.InitBlob(100, 0, commitment, PriorityNormal)

	cc.MarkReconstructed(100, 0, []byte("blob-data"))
	state := cc.GetState(100, 0)
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if !state.Reconstructed {
		t.Error("expected reconstructed=true")
	}
	if string(state.ReconData) != "blob-data" {
		t.Errorf("expected recon data 'blob-data', got %q", state.ReconData)
	}
}

func TestCellCollectorMarkFailed(t *testing.T) {
	cc := NewCellCollector()
	var commitment KZGCommitment
	cc.InitBlob(100, 0, commitment, PriorityNormal)

	cc.MarkFailed(100, 0, ErrPipelineInsufficient)
	state := cc.GetState(100, 0)
	if state.Error != ErrPipelineInsufficient {
		t.Errorf("expected ErrPipelineInsufficient, got %v", state.Error)
	}
}

func TestReconstructionSchedulerEmpty(t *testing.T) {
	cc := NewCellCollector()
	sched := NewReconstructionScheduler(cc)

	entries := sched.Schedule()
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestReconstructionSchedulerPriorityOrdering(t *testing.T) {
	cc := NewCellCollector()
	sched := NewReconstructionScheduler(cc)

	var commitment KZGCommitment

	// Create two blobs: one low priority, one high priority.
	cc.InitBlob(100, 0, commitment, PriorityLow)
	cc.InitBlob(100, 1, commitment, PriorityCritical)

	// Add enough cells to both for reconstruction.
	for i := uint64(0); i < uint64(ReconstructionThreshold); i++ {
		var cell Cell
		cell[0] = byte(i)
		cc.AddCell(100, 0, i, cell)
		cc.AddCell(100, 1, i, cell)
	}

	entries := sched.Schedule()
	if len(entries) != 2 {
		t.Fatalf("expected 2 scheduled entries, got %d", len(entries))
	}
	// Critical should come first.
	if entries[0].Priority != PriorityCritical {
		t.Errorf("expected critical first, got priority %d", entries[0].Priority)
	}
	if entries[1].Priority != PriorityLow {
		t.Errorf("expected low second, got priority %d", entries[1].Priority)
	}
}

func TestReconstructionSchedulerSkipsReconstructed(t *testing.T) {
	cc := NewCellCollector()
	sched := NewReconstructionScheduler(cc)

	var commitment KZGCommitment
	cc.InitBlob(100, 0, commitment, PriorityNormal)

	for i := uint64(0); i < uint64(ReconstructionThreshold); i++ {
		var cell Cell
		cc.AddCell(100, 0, i, cell)
	}

	cc.MarkReconstructed(100, 0, []byte("done"))

	entries := sched.Schedule()
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for reconstructed blobs, got %d", len(entries))
	}
}

func TestReconstructionSchedulerSkipsInsufficientCells(t *testing.T) {
	cc := NewCellCollector()
	sched := NewReconstructionScheduler(cc)

	var commitment KZGCommitment
	cc.InitBlob(100, 0, commitment, PriorityNormal)

	// Add only 2 cells (well below threshold).
	for i := uint64(0); i < 2; i++ {
		var cell Cell
		cc.AddCell(100, 0, i, cell)
	}

	entries := sched.Schedule()
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for insufficient cells, got %d", len(entries))
	}
}

func TestValidationStepCorrectSize(t *testing.T) {
	vs := NewValidationStep()

	// Correct size: FieldElementsPerBlob * BytesPerFieldElement = 4096 * 32 = 131072
	correctData := make([]byte, FieldElementsPerBlob*BytesPerFieldElement)
	var commitment KZGCommitment

	if !vs.Validate(correctData, commitment) {
		t.Error("expected validation to pass for correct-size data")
	}

	passed, failed := vs.Stats()
	if passed != 1 || failed != 0 {
		t.Errorf("expected 1 passed 0 failed, got %d passed %d failed", passed, failed)
	}
}

func TestValidationStepWrongSize(t *testing.T) {
	vs := NewValidationStep()

	wrongData := make([]byte, 100)
	var commitment KZGCommitment

	if vs.Validate(wrongData, commitment) {
		t.Error("expected validation to fail for wrong-size data")
	}

	passed, failed := vs.Stats()
	if passed != 0 || failed != 1 {
		t.Errorf("expected 0 passed 1 failed, got %d passed %d failed", passed, failed)
	}
}

func TestPipelineMetricsSuccessRate(t *testing.T) {
	pm := &ReconPipelineMetrics{}

	// Empty: 0.0.
	if pm.SuccessRate() != 0.0 {
		t.Errorf("expected 0.0 success rate, got %f", pm.SuccessRate())
	}

	pm.TotalAttempts.Add(10)
	pm.TotalSuccesses.Add(7)

	rate := pm.SuccessRate()
	if rate < 0.69 || rate > 0.71 {
		t.Errorf("expected ~0.70 success rate, got %f", rate)
	}
}

func TestPipelineMetricsAverageLatency(t *testing.T) {
	pm := &ReconPipelineMetrics{}

	if pm.AverageLatencyMs() != 0.0 {
		t.Errorf("expected 0.0 avg latency, got %f", pm.AverageLatencyMs())
	}

	pm.TotalSuccesses.Add(4)
	pm.TotalLatencyMs.Add(200)

	avg := pm.AverageLatencyMs()
	if avg < 49.0 || avg > 51.0 {
		t.Errorf("expected ~50.0 avg latency, got %f", avg)
	}
}

func TestReconstructionPipelineClosedErrors(t *testing.T) {
	p := NewReconstructionPipeline()
	p.Close()

	var commitment KZGCommitment
	err := p.InitBlob(100, 0, commitment, PriorityNormal)
	if err != ErrPipelineClosed {
		t.Errorf("expected ErrPipelineClosed from InitBlob, got %v", err)
	}

	var cell Cell
	err = p.AddCell(100, 0, 0, cell)
	if err != ErrPipelineClosed {
		t.Errorf("expected ErrPipelineClosed from AddCell, got %v", err)
	}

	_, err = p.Reconstruct(100, 0)
	if err != ErrPipelineClosed {
		t.Errorf("expected ErrPipelineClosed from Reconstruct, got %v", err)
	}
}

func TestReconstructionPipelineInsufficientCells(t *testing.T) {
	p := NewReconstructionPipeline()

	var commitment KZGCommitment
	if err := p.InitBlob(100, 0, commitment, PriorityNormal); err != nil {
		t.Fatalf("InitBlob: %v", err)
	}

	// Add only 2 cells.
	for i := uint64(0); i < 2; i++ {
		var cell Cell
		cell[0] = byte(i)
		if err := p.AddCell(100, 0, i, cell); err != nil {
			t.Fatalf("AddCell %d: %v", i, err)
		}
	}

	_, err := p.Reconstruct(100, 0)
	if err == nil {
		t.Error("expected error for insufficient cells")
	}

	metrics := p.Metrics()
	if metrics.TotalAttempts.Load() != 1 {
		t.Errorf("expected 1 attempt, got %d", metrics.TotalAttempts.Load())
	}
	if metrics.TotalFailures.Load() != 1 {
		t.Errorf("expected 1 failure, got %d", metrics.TotalFailures.Load())
	}
}

func TestReconstructionPipelineCellTracking(t *testing.T) {
	p := NewReconstructionPipeline()

	var commitment KZGCommitment
	if err := p.InitBlob(100, 0, commitment, PriorityNormal); err != nil {
		t.Fatalf("InitBlob: %v", err)
	}

	for i := uint64(0); i < 10; i++ {
		var cell Cell
		cell[0] = byte(i)
		if err := p.AddCell(100, 0, i, cell); err != nil {
			t.Fatalf("AddCell %d: %v", i, err)
		}
	}

	metrics := p.Metrics()
	if metrics.CellsCollected.Load() != 10 {
		t.Errorf("expected 10 cells collected, got %d", metrics.CellsCollected.Load())
	}
}

func TestReconstructionPipelineRunScheduledNoReady(t *testing.T) {
	p := NewReconstructionPipeline()

	results := p.RunScheduled(100)
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestReconstructionPipelineReconstructNonexistent(t *testing.T) {
	p := NewReconstructionPipeline()

	_, err := p.Reconstruct(999, 999)
	if err == nil {
		t.Error("expected error for nonexistent blob")
	}
}

func TestReconstructionPipelineReconstructNoCells(t *testing.T) {
	p := NewReconstructionPipeline()

	var commitment KZGCommitment
	p.InitBlob(100, 0, commitment, PriorityNormal)

	_, err := p.Reconstruct(100, 0)
	if err != ErrPipelineNoCells {
		t.Errorf("expected ErrPipelineNoCells, got %v", err)
	}
}
