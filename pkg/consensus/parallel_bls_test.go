package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makeTestAgg creates a test AggregateAttestation with the given slot and
// a single bit set at the specified position.
func makeTestAgg(slot uint64, bitPos int) *AggregateAttestation {
	bits := make([]byte, (bitPos/8)+1)
	bits[bitPos/8] |= 1 << (bitPos % 8)

	var sig [96]byte
	sig[0] = byte(bitPos)
	sig[1] = byte(slot)

	return &AggregateAttestation{
		Data: AttestationData{
			Slot:            Slot(slot),
			BeaconBlockRoot: types.Hash{0x01},
			Source:          Checkpoint{Epoch: 1, Root: types.Hash{0x02}},
			Target:          Checkpoint{Epoch: 2, Root: types.Hash{0x03}},
		},
		AggregationBits: bits,
		Signature:       sig,
	}
}

func TestParallelBLS_NewParallelAggregator(t *testing.T) {
	pa := NewParallelAggregator(nil)
	if pa == nil {
		t.Fatal("expected non-nil aggregator")
	}
	if pa.config.Workers != DefaultParallelWorkers {
		t.Errorf("expected %d workers, got %d", DefaultParallelWorkers, pa.config.Workers)
	}
	if pa.config.BatchSize != DefaultBatchSize {
		t.Errorf("expected %d batch size, got %d", DefaultBatchSize, pa.config.BatchSize)
	}
}

func TestParallelBLS_AggregateEmpty(t *testing.T) {
	pa := NewParallelAggregator(nil)
	_, err := pa.Aggregate(nil)
	if err != ErrParallelBLSNoAtts {
		t.Errorf("expected ErrParallelBLSNoAtts, got %v", err)
	}
}

func TestParallelBLS_AggregateSingle(t *testing.T) {
	pa := NewParallelAggregator(nil)
	att := makeTestAgg(1, 0)
	result, err := pa.Aggregate([]*AggregateAttestation{att})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProcessedCount != 1 {
		t.Errorf("expected 1 processed, got %d", result.ProcessedCount)
	}
	if result.MergeDepth != 0 {
		t.Errorf("expected merge depth 0, got %d", result.MergeDepth)
	}
}

func TestParallelBLS_AggregateMultiple(t *testing.T) {
	pa := NewParallelAggregator(&ParallelAggregatorConfig{
		Workers:   4,
		BatchSize: 2,
	})

	atts := make([]*AggregateAttestation, 8)
	for i := 0; i < 8; i++ {
		atts[i] = makeTestAgg(1, i)
	}

	result, err := pa.Aggregate(atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProcessedCount != 8 {
		t.Errorf("expected 8 processed, got %d", result.ProcessedCount)
	}
	if result.WorkerBatches < 2 {
		t.Errorf("expected at least 2 worker batches, got %d", result.WorkerBatches)
	}

	// The aggregate should have all 8 bits set.
	totalBits := CountBits(result.Aggregate.AggregationBits)
	if totalBits != 8 {
		t.Errorf("expected 8 bits set, got %d", totalBits)
	}
}

func TestParallelBLS_AggregateDataMismatch(t *testing.T) {
	pa := NewParallelAggregator(nil)
	att1 := makeTestAgg(1, 0)
	att2 := makeTestAgg(2, 1) // different slot

	_, err := pa.Aggregate([]*AggregateAttestation{att1, att2})
	if err != ErrParallelBLSDataMismatch {
		t.Errorf("expected ErrParallelBLSDataMismatch, got %v", err)
	}
}

func TestParallelBLS_TreeReduce(t *testing.T) {
	pa := NewParallelAggregator(nil)

	// Create 7 non-overlapping attestations.
	atts := make([]*AggregateAttestation, 7)
	for i := 0; i < 7; i++ {
		atts[i] = makeTestAgg(1, i)
	}

	merged, depth := pa.treeReduce(atts)
	if merged == nil {
		t.Fatal("expected non-nil merged result")
	}
	if depth < 1 {
		t.Errorf("expected depth >= 1, got %d", depth)
	}

	// All 7 bits should be merged.
	bits := CountBits(merged.AggregationBits)
	if bits != 7 {
		t.Errorf("expected 7 bits, got %d", bits)
	}
}

func TestParallelBLS_IncrementalAggregate(t *testing.T) {
	pa := NewParallelAggregator(&ParallelAggregatorConfig{
		Workers:   2,
		BatchSize: 4,
	})

	existing := makeTestAgg(1, 0)
	newAtts := []*AggregateAttestation{
		makeTestAgg(1, 1),
		makeTestAgg(1, 2),
	}

	result, err := pa.IncrementalAggregate(existing, newAtts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bits := CountBits(result.Aggregate.AggregationBits)
	if bits != 3 {
		t.Errorf("expected 3 bits, got %d", bits)
	}
}

func TestParallelBLS_IncrementalAggregateNilExisting(t *testing.T) {
	pa := NewParallelAggregator(nil)
	atts := []*AggregateAttestation{makeTestAgg(1, 0)}

	result, err := pa.IncrementalAggregate(nil, atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProcessedCount != 1 {
		t.Errorf("expected 1 processed, got %d", result.ProcessedCount)
	}
}

func TestParallelBLS_IncrementalAggregateEmptyNew(t *testing.T) {
	pa := NewParallelAggregator(nil)
	existing := makeTestAgg(1, 0)

	result, err := pa.IncrementalAggregate(existing, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProcessedCount != 0 {
		t.Errorf("expected 0 processed, got %d", result.ProcessedCount)
	}
}

func TestParallelBLS_BloomFilter(t *testing.T) {
	pa := NewParallelAggregator(nil)
	att := makeTestAgg(1, 5)

	// Should not be a duplicate initially.
	if pa.CheckDuplicate(att) {
		t.Error("expected not duplicate initially")
	}

	// Mark as seen.
	pa.MarkSeen(att)

	// Now should be detected as duplicate.
	if !pa.CheckDuplicate(att) {
		t.Error("expected duplicate after MarkSeen")
	}

	// Reset bloom filter.
	pa.ResetBloom()

	// Should no longer be detected.
	if pa.CheckDuplicate(att) {
		t.Error("expected not duplicate after ResetBloom")
	}
}

func TestParallelBLS_BloomFilterNil(t *testing.T) {
	pa := NewParallelAggregator(nil)
	if pa.CheckDuplicate(nil) {
		t.Error("nil should not be a duplicate")
	}
}

func TestParallelBLS_Metrics(t *testing.T) {
	pa := NewParallelAggregator(&ParallelAggregatorConfig{
		Workers:   2,
		BatchSize: 4,
	})

	atts := make([]*AggregateAttestation, 4)
	for i := 0; i < 4; i++ {
		atts[i] = makeTestAgg(1, i)
	}

	_, err := pa.Aggregate(atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	agg, dups, merges := pa.Metrics()
	if agg != 4 {
		t.Errorf("expected 4 aggregated, got %d", agg)
	}
	if dups < 0 {
		t.Errorf("expected non-negative duplicates, got %d", dups)
	}
	_ = merges
}

func TestParallelBLS_ConcurrentAggregate(t *testing.T) {
	pa := NewParallelAggregator(&ParallelAggregatorConfig{
		Workers:   4,
		BatchSize: 8,
	})

	// Run multiple aggregations concurrently.
	var wg sync.WaitGroup
	errs := make([]error, 4)

	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(group int) {
			defer wg.Done()
			atts := make([]*AggregateAttestation, 4)
			for i := 0; i < 4; i++ {
				atts[i] = makeTestAgg(uint64(group), i)
			}
			_, errs[group] = pa.Aggregate(atts)
		}(g)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("group %d error: %v", i, err)
		}
	}
}

func TestParallelBLS_DuplicateDetection(t *testing.T) {
	pa := NewParallelAggregator(&ParallelAggregatorConfig{
		Workers:   2,
		BatchSize: 4,
	})

	// Create attestations where bits overlap (duplicates).
	atts := []*AggregateAttestation{
		makeTestAgg(1, 0),
		makeTestAgg(1, 0), // duplicate bit position
		makeTestAgg(1, 1),
	}

	result, err := pa.Aggregate(atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// At least 1 duplicate should be detected.
	if result.DuplicateCount < 1 {
		t.Errorf("expected at least 1 duplicate, got %d", result.DuplicateCount)
	}
}

func TestParallelBLS_LargeBatch(t *testing.T) {
	pa := NewParallelAggregator(&ParallelAggregatorConfig{
		Workers:   8,
		BatchSize: 32,
	})

	// Create 128 attestations with distinct bits.
	atts := make([]*AggregateAttestation, 128)
	for i := 0; i < 128; i++ {
		atts[i] = makeTestAgg(1, i)
	}

	result, err := pa.Aggregate(atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ProcessedCount != 128 {
		t.Errorf("expected 128 processed, got %d", result.ProcessedCount)
	}

	bits := CountBits(result.Aggregate.AggregationBits)
	expected := 128 - result.DuplicateCount
	if bits != expected {
		t.Errorf("expected %d bits, got %d", expected, bits)
	}
}
