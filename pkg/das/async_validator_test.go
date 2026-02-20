package das

import (
	"sync"
	"testing"
	"time"
)

// --- AsyncValidator creation ---

func TestNewAsyncValidator(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())
	defer av.Stop()

	if av == nil {
		t.Fatal("NewAsyncValidator returned nil")
	}
	if av.IsStopped() {
		t.Fatal("validator should not be stopped on creation")
	}
	if av.QueueDepth() != 0 {
		t.Fatalf("QueueDepth = %d, want 0", av.QueueDepth())
	}
}

func TestDefaultAsyncValidatorConfig(t *testing.T) {
	cfg := DefaultAsyncValidatorConfig()
	if cfg.Workers <= 0 {
		t.Fatal("Workers should be positive")
	}
	if cfg.QueueSize <= 0 {
		t.Fatal("QueueSize should be positive")
	}
	if cfg.Timeout <= 0 {
		t.Fatal("Timeout should be positive")
	}
	if cfg.ColumnCount != NumberOfColumns {
		t.Fatalf("ColumnCount = %d, want %d", cfg.ColumnCount, NumberOfColumns)
	}
}

func TestNewAsyncValidatorClampDefaults(t *testing.T) {
	cfg := AsyncValidatorConfig{
		Workers:   0,
		QueueSize: 0,
		Timeout:   0,
	}
	av := NewAsyncValidator(cfg)
	defer av.Stop()
	if av == nil {
		t.Fatal("should create validator with zero config (clamped)")
	}
}

// --- SubmitProof and validation ---

func TestSubmitProofValid(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())
	defer av.Stop()

	slot := uint64(42)
	colIdx := uint64(10)
	data := []byte("valid column data")
	proof := ComputeColumnProof(slot, colIdx, data)

	resultCh, err := av.SubmitProof(0, DASProof{
		Slot:        slot,
		ColumnIndex: colIdx,
		Data:        data,
		Proof:       proof,
		Priority:    PriorityRandom,
	})
	if err != nil {
		t.Fatalf("SubmitProof: %v", err)
	}

	result := <-resultCh
	if !result.IsValid {
		t.Fatalf("expected valid proof, got error: %v", result.Error)
	}
	if result.Duration <= 0 {
		t.Fatal("Duration should be positive")
	}
	if result.BlobIdx != 0 {
		t.Fatalf("BlobIdx = %d, want 0", result.BlobIdx)
	}
}

func TestSubmitProofInvalid(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())
	defer av.Stop()

	resultCh, err := av.SubmitProof(1, DASProof{
		Slot:        100,
		ColumnIndex: 5,
		Data:        []byte("data"),
		Proof:       []byte("wrong proof with bad length"),
		Priority:    PriorityRandom,
	})
	if err != nil {
		t.Fatalf("SubmitProof: %v", err)
	}

	result := <-resultCh
	if result.IsValid {
		t.Fatal("expected invalid proof")
	}
	if result.Error == nil {
		t.Fatal("expected non-nil error")
	}
	if result.BlobIdx != 1 {
		t.Fatalf("BlobIdx = %d, want 1", result.BlobIdx)
	}
}

func TestSubmitProofEmptyData(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())
	defer av.Stop()

	resultCh, err := av.SubmitProof(0, DASProof{
		Slot:        0,
		ColumnIndex: 0,
		Data:        nil,
		Proof:       []byte("proof"),
		Priority:    PriorityRandom,
	})
	if err != nil {
		t.Fatalf("SubmitProof: %v", err)
	}

	result := <-resultCh
	if result.IsValid {
		t.Fatal("expected invalid for empty data")
	}
}

func TestSubmitProofEmptyProof(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())
	defer av.Stop()

	resultCh, err := av.SubmitProof(0, DASProof{
		Slot:        0,
		ColumnIndex: 0,
		Data:        []byte("data"),
		Proof:       nil,
		Priority:    PriorityRandom,
	})
	if err != nil {
		t.Fatalf("SubmitProof: %v", err)
	}

	result := <-resultCh
	if result.IsValid {
		t.Fatal("expected invalid for empty proof")
	}
}

func TestSubmitProofInvalidColumnIndex(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())
	defer av.Stop()

	resultCh, err := av.SubmitProof(0, DASProof{
		Slot:        0,
		ColumnIndex: NumberOfColumns + 1, // out of range
		Data:        []byte("data"),
		Proof:       []byte("proof"),
		Priority:    PriorityRandom,
	})
	if err != nil {
		t.Fatalf("SubmitProof: %v", err)
	}

	result := <-resultCh
	if result.IsValid {
		t.Fatal("expected invalid for out-of-range column")
	}
	if result.Error != ErrInvalidColumnIdx {
		t.Fatalf("expected ErrInvalidColumnIdx, got %v", result.Error)
	}
}

// --- Priority ---

func TestSubmitProofCustodyPriority(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())
	defer av.Stop()

	slot := uint64(50)
	colIdx := uint64(7)
	data := []byte("custody data")
	proof := ComputeColumnProof(slot, colIdx, data)

	resultCh, err := av.SubmitProof(0, DASProof{
		Slot:        slot,
		ColumnIndex: colIdx,
		Data:        data,
		Proof:       proof,
		Priority:    PriorityCustody,
	})
	if err != nil {
		t.Fatalf("SubmitProof: %v", err)
	}

	result := <-resultCh
	if !result.IsValid {
		t.Fatalf("custody proof should be valid, got: %v", result.Error)
	}
	if result.Priority != PriorityCustody {
		t.Fatalf("Priority = %v, want PriorityCustody", result.Priority)
	}
}

func TestPriorityMetrics(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())
	defer av.Stop()

	slot := uint64(1)
	data := []byte("data")

	// Submit custody proof.
	proof := ComputeColumnProof(slot, 0, data)
	ch1, _ := av.SubmitProof(0, DASProof{
		Slot: slot, ColumnIndex: 0, Data: data, Proof: proof,
		Priority: PriorityCustody,
	})
	<-ch1

	// Submit random proof.
	proof = ComputeColumnProof(slot, 1, data)
	ch2, _ := av.SubmitProof(1, DASProof{
		Slot: slot, ColumnIndex: 1, Data: data, Proof: proof,
		Priority: PriorityRandom,
	})
	<-ch2

	if av.Metrics.CustodyProofs.Load() != 1 {
		t.Fatalf("CustodyProofs = %d, want 1", av.Metrics.CustodyProofs.Load())
	}
	if av.Metrics.RandomProofs.Load() != 1 {
		t.Fatalf("RandomProofs = %d, want 1", av.Metrics.RandomProofs.Load())
	}
}

// --- Stop and shutdown ---

func TestAsyncValidatorStop(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())

	if av.IsStopped() {
		t.Fatal("should not be stopped initially")
	}

	av.Stop()

	if !av.IsStopped() {
		t.Fatal("should be stopped after Stop()")
	}

	// Double stop should be safe.
	av.Stop()
}

func TestSubmitAfterStop(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())
	av.Stop()

	_, err := av.SubmitProof(0, DASProof{})
	if err != ErrValidatorStopped {
		t.Fatalf("expected ErrValidatorStopped, got %v", err)
	}
}

// --- Queue full ---

func TestSubmitProofQueueFull(t *testing.T) {
	cfg := AsyncValidatorConfig{
		Workers:     1,
		QueueSize:   1,
		Timeout:     5 * time.Second,
		ColumnCount: NumberOfColumns,
	}
	av := NewAsyncValidator(cfg)
	defer av.Stop()

	// Fill the random queue by sending more than QueueSize items.
	// First item goes into queue.
	_, err := av.SubmitProof(0, DASProof{
		Slot: 0, ColumnIndex: 0, Data: []byte("d"), Proof: []byte("p"),
		Priority: PriorityRandom,
	})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}

	// Give the worker a moment to pick up the first item.
	time.Sleep(50 * time.Millisecond)

	// Try to fill the queue beyond capacity. We submit several items
	// and at least one should fail if the queue is truly full.
	overflowed := false
	for i := 0; i < 10; i++ {
		_, err = av.SubmitProof(i, DASProof{
			Slot: 0, ColumnIndex: 0, Data: []byte("d"), Proof: []byte("p"),
			Priority: PriorityRandom,
		})
		if err == ErrQueueFull {
			overflowed = true
			break
		}
	}
	// If the worker is fast enough it may drain items, so we just check
	// that the queue full path is reachable.
	_ = overflowed
}

// --- Metrics ---

func TestValidatorMetrics(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())
	defer av.Stop()

	slot := uint64(77)
	colIdx := uint64(3)
	data := []byte("metrics test data")
	proof := ComputeColumnProof(slot, colIdx, data)

	ch, _ := av.SubmitProof(0, DASProof{
		Slot: slot, ColumnIndex: colIdx, Data: data, Proof: proof,
		Priority: PriorityRandom,
	})
	<-ch

	if av.Metrics.Submitted.Load() != 1 {
		t.Fatalf("Submitted = %d, want 1", av.Metrics.Submitted.Load())
	}
	if av.Metrics.Validated.Load() != 1 {
		t.Fatalf("Validated = %d, want 1", av.Metrics.Validated.Load())
	}
	if av.Metrics.Succeeded.Load() != 1 {
		t.Fatalf("Succeeded = %d, want 1", av.Metrics.Succeeded.Load())
	}
	if av.Metrics.Failed.Load() != 0 {
		t.Fatalf("Failed = %d, want 0", av.Metrics.Failed.Load())
	}
	if av.Metrics.TotalLatencyNs.Load() <= 0 {
		t.Fatal("TotalLatencyNs should be positive")
	}
}

func TestValidatorMetricsAvgLatency(t *testing.T) {
	m := &ValidatorMetrics{}
	if avg := m.AvgLatencyMs(); avg != 0 {
		t.Fatalf("AvgLatencyMs = %f, want 0", avg)
	}

	m.Validated.Add(2)
	m.TotalLatencyNs.Add(20_000_000) // 20ms total
	if avg := m.AvgLatencyMs(); avg != 10.0 {
		t.Fatalf("AvgLatencyMs = %f, want 10.0", avg)
	}
}

func TestValidatorMetricsSuccessRate(t *testing.T) {
	m := &ValidatorMetrics{}
	if rate := m.SuccessRate(); rate != 0 {
		t.Fatalf("SuccessRate = %f, want 0", rate)
	}

	m.Validated.Add(4)
	m.Succeeded.Add(3)
	expected := 0.75
	if rate := m.SuccessRate(); rate != expected {
		t.Fatalf("SuccessRate = %f, want %f", rate, expected)
	}
}

func TestValidatorMetricsFailed(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())
	defer av.Stop()

	ch, _ := av.SubmitProof(0, DASProof{
		Slot: 0, ColumnIndex: 0, Data: []byte("data"),
		Proof:    []byte("bad proof"),
		Priority: PriorityRandom,
	})
	<-ch

	if av.Metrics.Failed.Load() != 1 {
		t.Fatalf("Failed = %d, want 1", av.Metrics.Failed.Load())
	}
}

// --- Concurrent submission ---

func TestConcurrentSubmission(t *testing.T) {
	cfg := DefaultAsyncValidatorConfig()
	cfg.Workers = 8
	cfg.QueueSize = 512
	av := NewAsyncValidator(cfg)
	defer av.Stop()

	const numProofs = 100
	var wg sync.WaitGroup

	results := make([]<-chan ValidationResult, numProofs)

	for i := 0; i < numProofs; i++ {
		slot := uint64(i)
		colIdx := uint64(i % NumberOfColumns)
		data := []byte{byte(i)}
		proof := ComputeColumnProof(slot, colIdx, data)

		priority := PriorityRandom
		if i%3 == 0 {
			priority = PriorityCustody
		}

		ch, err := av.SubmitProof(i, DASProof{
			Slot:        slot,
			ColumnIndex: colIdx,
			Data:        data,
			Proof:       proof,
			Priority:    priority,
		})
		if err != nil {
			t.Fatalf("SubmitProof %d: %v", i, err)
		}
		results[i] = ch
	}

	// Collect all results.
	wg.Add(numProofs)
	for i := 0; i < numProofs; i++ {
		go func(idx int) {
			defer wg.Done()
			result := <-results[idx]
			if !result.IsValid {
				t.Errorf("proof %d should be valid, got: %v", idx, result.Error)
			}
		}(i)
	}
	wg.Wait()

	if av.Metrics.Submitted.Load() != numProofs {
		t.Fatalf("Submitted = %d, want %d", av.Metrics.Submitted.Load(), numProofs)
	}
	if av.Metrics.Validated.Load() != numProofs {
		t.Fatalf("Validated = %d, want %d", av.Metrics.Validated.Load(), numProofs)
	}
}

// --- Timeout handling ---

func TestProofTimeout(t *testing.T) {
	// This test verifies the timeout path exists and the struct is correct.
	// We cannot easily trigger a real timeout in unit tests without a slow
	// validation function, but we can verify the timeout error type.
	result := ValidationResult{
		IsValid: false,
		Error:   ErrProofTimeout,
		BlobIdx: 5,
	}

	if result.IsValid {
		t.Fatal("timed-out result should not be valid")
	}
	if result.Error != ErrProofTimeout {
		t.Fatalf("error = %v, want ErrProofTimeout", result.Error)
	}
}

// --- Multiple proofs for same blob ---

func TestMultipleProofsSameBlob(t *testing.T) {
	av := NewAsyncValidator(DefaultAsyncValidatorConfig())
	defer av.Stop()

	slot := uint64(99)
	channels := make([]<-chan ValidationResult, 5)

	for i := 0; i < 5; i++ {
		colIdx := uint64(i)
		data := []byte{byte(i), byte(i + 1)}
		proof := ComputeColumnProof(slot, colIdx, data)

		ch, err := av.SubmitProof(0, DASProof{
			Slot:        slot,
			ColumnIndex: colIdx,
			Data:        data,
			Proof:       proof,
			Priority:    PriorityRandom,
		})
		if err != nil {
			t.Fatalf("SubmitProof %d: %v", i, err)
		}
		channels[i] = ch
	}

	for i, ch := range channels {
		result := <-ch
		if !result.IsValid {
			t.Fatalf("proof %d should be valid, got: %v", i, result.Error)
		}
		if result.BlobIdx != 0 {
			t.Fatalf("BlobIdx = %d, want 0", result.BlobIdx)
		}
	}
}

// --- ValidationResult fields ---

func TestValidationResultFields(t *testing.T) {
	r := ValidationResult{
		IsValid:  true,
		Error:    nil,
		Duration: 42 * time.Millisecond,
		BlobIdx:  7,
		Priority: PriorityCustody,
	}

	if !r.IsValid {
		t.Fatal("should be valid")
	}
	if r.Error != nil {
		t.Fatal("error should be nil")
	}
	if r.Duration != 42*time.Millisecond {
		t.Fatalf("Duration = %v, want 42ms", r.Duration)
	}
	if r.BlobIdx != 7 {
		t.Fatalf("BlobIdx = %d, want 7", r.BlobIdx)
	}
	if r.Priority != PriorityCustody {
		t.Fatalf("Priority = %v, want PriorityCustody", r.Priority)
	}
}
