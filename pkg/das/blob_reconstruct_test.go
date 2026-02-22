package das

import (
	"sync"
	"testing"
)

func TestValidateSample(t *testing.T) {
	// Nil sample.
	if err := ValidateSample(nil, 1); err != ErrNilSample {
		t.Errorf("expected ErrNilSample, got %v", err)
	}

	// Valid sample.
	s := &Sample{BlobIndex: 0, CellIndex: 0}
	if err := ValidateSample(s, 1); err != nil {
		t.Errorf("valid sample failed: %v", err)
	}

	// Cell index out of range.
	s = &Sample{BlobIndex: 0, CellIndex: CellsPerExtBlob}
	if err := ValidateSample(s, 1); err == nil {
		t.Error("expected error for cell index out of range")
	}

	// Blob index out of range.
	s = &Sample{BlobIndex: 5, CellIndex: 0}
	if err := ValidateSample(s, 3); err == nil {
		t.Error("expected error for blob index out of range")
	}

	// Blob count 0 means no blob index validation.
	s = &Sample{BlobIndex: 100, CellIndex: 0}
	if err := ValidateSample(s, 0); err != nil {
		t.Errorf("expected no error with blobCount=0, got %v", err)
	}
}

func TestBlobReconstructorAddSample(t *testing.T) {
	br := NewBlobReconstructor(9)

	// Add a valid sample.
	s := Sample{BlobIndex: 0, CellIndex: 10, Data: Cell{}}
	if err := br.AddSample(s); err != nil {
		t.Fatalf("AddSample: %v", err)
	}
	if br.SampleCount(0) != 1 {
		t.Fatalf("SampleCount = %d, want 1", br.SampleCount(0))
	}

	// Add duplicate cell index (should be silently ignored).
	if err := br.AddSample(s); err != nil {
		t.Fatalf("AddSample (dup): %v", err)
	}
	if br.SampleCount(0) != 1 {
		t.Fatalf("SampleCount after dup = %d, want 1", br.SampleCount(0))
	}

	// Add sample for different cell index.
	s2 := Sample{BlobIndex: 0, CellIndex: 20, Data: Cell{}}
	if err := br.AddSample(s2); err != nil {
		t.Fatalf("AddSample s2: %v", err)
	}
	if br.SampleCount(0) != 2 {
		t.Fatalf("SampleCount = %d, want 2", br.SampleCount(0))
	}

	// Invalid sample (cell index out of range).
	bad := Sample{BlobIndex: 0, CellIndex: CellsPerExtBlob}
	if err := br.AddSample(bad); err == nil {
		t.Error("expected error for invalid cell index")
	}

	// Invalid sample (blob index out of range).
	bad = Sample{BlobIndex: 10, CellIndex: 0}
	if err := br.AddSample(bad); err == nil {
		t.Error("expected error for invalid blob index")
	}
}

func TestBlobReconstructorAddSamples(t *testing.T) {
	br := NewBlobReconstructor(9)

	samples := []Sample{
		{BlobIndex: 0, CellIndex: 0},
		{BlobIndex: 0, CellIndex: 1},
		{BlobIndex: 1, CellIndex: 0},
	}

	if err := br.AddSamples(samples); err != nil {
		t.Fatalf("AddSamples: %v", err)
	}
	if br.SampleCount(0) != 2 {
		t.Errorf("blob 0 SampleCount = %d, want 2", br.SampleCount(0))
	}
	if br.SampleCount(1) != 1 {
		t.Errorf("blob 1 SampleCount = %d, want 1", br.SampleCount(1))
	}
}

func TestBlobReconstructorCanReconstruct(t *testing.T) {
	br := NewBlobReconstructor(9)

	// Not enough samples.
	if br.CanReconstructBlob(0) {
		t.Error("should not be able to reconstruct with 0 samples")
	}

	// Add exactly threshold samples.
	for i := 0; i < ReconstructionThreshold; i++ {
		s := Sample{BlobIndex: 0, CellIndex: uint64(i)}
		if err := br.AddSample(s); err != nil {
			t.Fatalf("AddSample %d: %v", i, err)
		}
	}

	if !br.CanReconstructBlob(0) {
		t.Error("should be able to reconstruct with threshold samples")
	}

	// Blob 1 still not ready.
	if br.CanReconstructBlob(1) {
		t.Error("blob 1 should not be ready")
	}
}

func TestBlobReconstructorReadyBlobs(t *testing.T) {
	br := NewBlobReconstructor(9)

	// No ready blobs initially.
	ready := br.ReadyBlobs()
	if len(ready) != 0 {
		t.Fatalf("expected 0 ready blobs, got %d", len(ready))
	}

	// Fill blob 0 to threshold.
	for i := 0; i < ReconstructionThreshold; i++ {
		br.AddSample(Sample{BlobIndex: 0, CellIndex: uint64(i)})
	}
	// Partially fill blob 1.
	for i := 0; i < 10; i++ {
		br.AddSample(Sample{BlobIndex: 1, CellIndex: uint64(i)})
	}

	ready = br.ReadyBlobs()
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready blob, got %d", len(ready))
	}
	if ready[0] != 0 {
		t.Errorf("ready blob = %d, want 0", ready[0])
	}
}

func TestBlobReconstructorReconstructZeroBlob(t *testing.T) {
	br := NewBlobReconstructor(9)

	// Create zero-filled samples for the first 64 cell indices.
	samples := make([]Sample, ReconstructionThreshold)
	for i := range samples {
		samples[i] = Sample{
			BlobIndex: 0,
			CellIndex: uint64(i),
			Data:      Cell{}, // Zero-filled.
		}
	}

	result, err := br.Reconstruct(samples, CellsPerExtBlob)
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}

	expectedSize := FieldElementsPerBlob * BytesPerFieldElement
	if len(result) != expectedSize {
		t.Fatalf("result size = %d, want %d", len(result), expectedSize)
	}

	// All-zero input produces all-zero output.
	for i, b := range result {
		if b != 0 {
			t.Fatalf("result[%d] = %d, want 0", i, b)
		}
	}

	// Check metrics.
	if br.Metrics.SuccessCount.Load() != 1 {
		t.Errorf("SuccessCount = %d, want 1", br.Metrics.SuccessCount.Load())
	}
	if br.Metrics.BlobsReconstructed.Load() != 1 {
		t.Errorf("BlobsReconstructed = %d, want 1", br.Metrics.BlobsReconstructed.Load())
	}
	if br.Metrics.FailureCount.Load() != 0 {
		t.Errorf("FailureCount = %d, want 0", br.Metrics.FailureCount.Load())
	}
	if br.Metrics.LastLatencyNs.Load() <= 0 {
		t.Error("LastLatencyNs should be positive")
	}
}

func TestBlobReconstructorReconstructInsufficientSamples(t *testing.T) {
	br := NewBlobReconstructor(9)

	samples := make([]Sample, ReconstructionThreshold-1)
	for i := range samples {
		samples[i] = Sample{BlobIndex: 0, CellIndex: uint64(i)}
	}

	_, err := br.Reconstruct(samples, CellsPerExtBlob)
	if err == nil {
		t.Fatal("expected error for insufficient samples")
	}

	if br.Metrics.FailureCount.Load() != 1 {
		t.Errorf("FailureCount = %d, want 1", br.Metrics.FailureCount.Load())
	}
	if br.Metrics.InsufficientSamples.Load() != 1 {
		t.Errorf("InsufficientSamples = %d, want 1", br.Metrics.InsufficientSamples.Load())
	}
}

func TestBlobReconstructorReconstructWithDuplicates(t *testing.T) {
	br := NewBlobReconstructor(9)

	// Create samples with some duplicates. Total unique = ReconstructionThreshold.
	samples := make([]Sample, ReconstructionThreshold+10)
	for i := 0; i < ReconstructionThreshold; i++ {
		samples[i] = Sample{BlobIndex: 0, CellIndex: uint64(i)}
	}
	// Last 10 are duplicates of the first 10.
	for i := 0; i < 10; i++ {
		samples[ReconstructionThreshold+i] = Sample{BlobIndex: 0, CellIndex: uint64(i)}
	}

	result, err := br.Reconstruct(samples, CellsPerExtBlob)
	if err != nil {
		t.Fatalf("Reconstruct with duplicates: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
}

func TestBlobReconstructorReconstructInvalidCellIndex(t *testing.T) {
	br := NewBlobReconstructor(9)

	samples := []Sample{
		{BlobIndex: 0, CellIndex: CellsPerExtBlob + 1}, // Out of range.
	}

	_, err := br.Reconstruct(samples, CellsPerExtBlob)
	if err == nil {
		t.Error("expected error for invalid cell index")
	}
}

func TestBlobReconstructorReconstructBlobs(t *testing.T) {
	br := NewBlobReconstructor(9)

	// Fill blob 0 to threshold with zero cells.
	for i := 0; i < ReconstructionThreshold; i++ {
		br.AddSample(Sample{BlobIndex: 0, CellIndex: uint64(i)})
	}

	// Blob 1 has insufficient samples.
	for i := 0; i < 10; i++ {
		br.AddSample(Sample{BlobIndex: 1, CellIndex: uint64(i)})
	}

	blobs, _ := br.ReconstructBlobs(2)
	if len(blobs) != 1 {
		t.Fatalf("expected 1 reconstructed blob, got %d", len(blobs))
	}
	if _, ok := blobs[0]; !ok {
		t.Error("blob 0 should be present in results")
	}
	if _, ok := blobs[1]; ok {
		t.Error("blob 1 should not be present (insufficient samples)")
	}
}

func TestBlobReconstructorReconstructBlobsInvalidCount(t *testing.T) {
	br := NewBlobReconstructor(9)

	_, err := br.ReconstructBlobs(0)
	if err == nil {
		t.Error("expected error for blob count 0")
	}

	_, err = br.ReconstructBlobs(-1)
	if err == nil {
		t.Error("expected error for negative blob count")
	}
}

func TestBlobReconstructorReconstructPending(t *testing.T) {
	br := NewBlobReconstructor(9)

	// No pending samples.
	blobs, err := br.ReconstructPending()
	if err != nil {
		t.Fatalf("ReconstructPending (empty): %v", err)
	}
	if blobs != nil {
		t.Errorf("expected nil, got %v", blobs)
	}

	// Add enough samples for blob 0.
	for i := 0; i < ReconstructionThreshold; i++ {
		br.AddSample(Sample{BlobIndex: 0, CellIndex: uint64(i)})
	}

	blobs, err = br.ReconstructPending()
	if err != nil {
		t.Fatalf("ReconstructPending: %v", err)
	}
	if len(blobs) != 1 {
		t.Fatalf("expected 1 blob, got %d", len(blobs))
	}

	// After reconstruction, pending should be cleared for blob 0.
	if br.SampleCount(0) != 0 {
		t.Errorf("blob 0 still has %d pending samples", br.SampleCount(0))
	}
}

func TestBlobReconstructorReset(t *testing.T) {
	br := NewBlobReconstructor(9)

	br.AddSample(Sample{BlobIndex: 0, CellIndex: 0})
	br.AddSample(Sample{BlobIndex: 1, CellIndex: 0})

	if br.PendingBlobCount() != 2 {
		t.Fatalf("PendingBlobCount = %d, want 2", br.PendingBlobCount())
	}

	br.Reset()

	if br.PendingBlobCount() != 0 {
		t.Errorf("PendingBlobCount after reset = %d, want 0", br.PendingBlobCount())
	}
}

func TestBlobReconstructorStatus(t *testing.T) {
	br := NewBlobReconstructor(9)

	// Add samples for two blobs.
	for i := 0; i < ReconstructionThreshold; i++ {
		br.AddSample(Sample{BlobIndex: 0, CellIndex: uint64(i)})
	}
	for i := 0; i < 10; i++ {
		br.AddSample(Sample{BlobIndex: 1, CellIndex: uint64(i)})
	}

	status := br.Status()
	if status.PendingBlobs != 2 {
		t.Errorf("PendingBlobs = %d, want 2", status.PendingBlobs)
	}
	if status.ReadyBlobs != 1 {
		t.Errorf("ReadyBlobs = %d, want 1", status.ReadyBlobs)
	}
	if status.TotalSamples != ReconstructionThreshold+10 {
		t.Errorf("TotalSamples = %d, want %d", status.TotalSamples, ReconstructionThreshold+10)
	}
}

func TestBlobReconstructorConcurrentAdd(t *testing.T) {
	br := NewBlobReconstructor(9)

	var wg sync.WaitGroup
	// Concurrently add samples from multiple goroutines.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(blobIdx uint64) {
			defer wg.Done()
			for i := 0; i < 32; i++ {
				br.AddSample(Sample{
					BlobIndex: blobIdx,
					CellIndex: uint64(i),
				})
			}
		}(uint64(g))
	}
	wg.Wait()

	// Each blob should have 32 samples.
	for g := 0; g < 4; g++ {
		count := br.SampleCount(uint64(g))
		if count != 32 {
			t.Errorf("blob %d: SampleCount = %d, want 32", g, count)
		}
	}
}

func TestReconstructionMetricsAvgLatency(t *testing.T) {
	m := &ReconstructionMetrics{}

	// No operations yet.
	if avg := m.AvgLatencyMs(); avg != 0 {
		t.Errorf("AvgLatencyMs = %f, want 0", avg)
	}

	// Simulate some latencies.
	m.SuccessCount.Add(2)
	m.TotalLatencyNs.Add(10_000_000) // 10ms total for 2 ops.
	if avg := m.AvgLatencyMs(); avg != 5.0 {
		t.Errorf("AvgLatencyMs = %f, want 5.0", avg)
	}
}

func TestNewBlobReconstructorDefaults(t *testing.T) {
	// maxBlobs = 0 should default to MaxBlobCommitmentsPerBlock.
	br := NewBlobReconstructor(0)
	if br.maxBlobs != MaxBlobCommitmentsPerBlock {
		t.Errorf("maxBlobs = %d, want %d", br.maxBlobs, MaxBlobCommitmentsPerBlock)
	}

	// Negative value also defaults.
	br = NewBlobReconstructor(-5)
	if br.maxBlobs != MaxBlobCommitmentsPerBlock {
		t.Errorf("maxBlobs = %d, want %d", br.maxBlobs, MaxBlobCommitmentsPerBlock)
	}
}

func TestValidateReconstructionInput(t *testing.T) {
	// Valid input with enough samples.
	samples := make([]Sample, ReconstructionThreshold)
	for i := range samples {
		samples[i] = Sample{BlobIndex: 0, CellIndex: uint64(i), Data: Cell{}}
	}
	if err := ValidateReconstructionInput(samples, CellsPerExtBlob); err != nil {
		t.Errorf("valid input: %v", err)
	}

	// Empty.
	if err := ValidateReconstructionInput(nil, CellsPerExtBlob); err == nil {
		t.Error("expected error for empty samples")
	}

	// Too few unique cells.
	fewSamples := make([]Sample, ReconstructionThreshold-1)
	for i := range fewSamples {
		fewSamples[i] = Sample{CellIndex: uint64(i), Data: Cell{}}
	}
	if err := ValidateReconstructionInput(fewSamples, CellsPerExtBlob); err == nil {
		t.Error("expected error for too few samples")
	}

	// Cell out of range.
	oob := []Sample{{CellIndex: uint64(CellsPerExtBlob + 1), Data: Cell{}}}
	for len(oob) < ReconstructionThreshold {
		oob = append(oob, Sample{CellIndex: uint64(len(oob)), Data: Cell{}})
	}
	if err := ValidateReconstructionInput(oob, CellsPerExtBlob); err == nil {
		t.Error("expected error for cell out of range")
	}
}
