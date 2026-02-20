package verkle

import (
	"bytes"
	"sync"
	"testing"
)

// makeStem returns a 31-byte stem for testing.
func makeStem(b byte) []byte {
	stem := make([]byte, StemSize)
	stem[0] = b
	return stem
}

// makeValue returns a 32-byte value for testing.
func makeValue(b byte) []byte {
	val := make([]byte, ValueSize)
	val[0] = b
	return val
}

func TestWitnessBuilderAddRead(t *testing.T) {
	wb := NewWitnessBuilder()
	stem := makeStem(0x01)
	suffix := []byte{0x00}
	value := makeValue(0xaa)

	if err := wb.AddRead(stem, suffix, value); err != nil {
		t.Fatalf("AddRead: %v", err)
	}

	if wb.AccessCount() != 1 {
		t.Fatalf("AccessCount = %d, want 1", wb.AccessCount())
	}
	if wb.StemCount() != 1 {
		t.Fatalf("StemCount = %d, want 1", wb.StemCount())
	}
}

func TestWitnessBuilderAddWrite(t *testing.T) {
	wb := NewWitnessBuilder()
	stem := makeStem(0x02)
	suffix := []byte{0x01}
	oldVal := makeValue(0xaa)
	newVal := makeValue(0xbb)

	if err := wb.AddWrite(stem, suffix, oldVal, newVal); err != nil {
		t.Fatalf("AddWrite: %v", err)
	}

	if wb.AccessCount() != 1 {
		t.Fatalf("AccessCount = %d, want 1", wb.AccessCount())
	}
}

func TestWitnessBuilderDeduplicateReads(t *testing.T) {
	wb := NewWitnessBuilder()
	stem := makeStem(0x01)
	suffix := []byte{0x00}
	value := makeValue(0xaa)

	wb.AddRead(stem, suffix, value)
	wb.AddRead(stem, suffix, value) // duplicate

	if wb.AccessCount() != 1 {
		t.Fatalf("AccessCount = %d, want 1 (duplicate should be ignored)", wb.AccessCount())
	}
}

func TestWitnessBuilderUpgradeReadToWrite(t *testing.T) {
	wb := NewWitnessBuilder()
	stem := makeStem(0x01)
	suffix := []byte{0x00}
	oldVal := makeValue(0xaa)
	newVal := makeValue(0xbb)

	// First read.
	wb.AddRead(stem, suffix, oldVal)
	// Then write to the same location.
	wb.AddWrite(stem, suffix, oldVal, newVal)

	if wb.AccessCount() != 1 {
		t.Fatalf("AccessCount = %d, want 1 (upgrade, not new entry)", wb.AccessCount())
	}

	w := wb.Build()
	if w.NewValues[0] == nil {
		t.Fatal("write should have set NewValues")
	}
	if !bytes.Equal(w.NewValues[0], newVal) {
		t.Fatalf("NewValues[0] = %x, want %x", w.NewValues[0], newVal)
	}
}

func TestWitnessBuilderMultipleStems(t *testing.T) {
	wb := NewWitnessBuilder()

	for i := byte(0); i < 5; i++ {
		stem := makeStem(i)
		suffix := []byte{i}
		value := makeValue(i * 10)
		wb.AddRead(stem, suffix, value)
	}

	if wb.StemCount() != 5 {
		t.Fatalf("StemCount = %d, want 5", wb.StemCount())
	}
	if wb.AccessCount() != 5 {
		t.Fatalf("AccessCount = %d, want 5", wb.AccessCount())
	}
}

func TestWitnessBuilderSameStemDifferentSuffixes(t *testing.T) {
	wb := NewWitnessBuilder()
	stem := makeStem(0x01)

	for i := byte(0); i < 4; i++ {
		wb.AddRead(stem, []byte{i}, makeValue(i))
	}

	if wb.StemCount() != 1 {
		t.Fatalf("StemCount = %d, want 1 (same stem)", wb.StemCount())
	}
	if wb.AccessCount() != 4 {
		t.Fatalf("AccessCount = %d, want 4", wb.AccessCount())
	}
}

func TestWitnessBuilderBuild(t *testing.T) {
	wb := NewWitnessBuilder()
	stem := makeStem(0x01)
	wb.AddRead(stem, []byte{0x00}, makeValue(0xaa))
	wb.AddWrite(makeStem(0x02), []byte{0x01}, makeValue(0xbb), makeValue(0xcc))

	w := wb.Build()
	if w == nil {
		t.Fatal("Build returned nil")
	}
	if len(w.Stems) != 2 {
		t.Fatalf("Stems count = %d, want 2", len(w.Stems))
	}
	if len(w.Suffixes) != 2 {
		t.Fatalf("Suffixes count = %d, want 2", len(w.Suffixes))
	}
	if len(w.CurrentValues) != 2 {
		t.Fatalf("CurrentValues count = %d, want 2", len(w.CurrentValues))
	}
	if len(w.NewValues) != 2 {
		t.Fatalf("NewValues count = %d, want 2", len(w.NewValues))
	}
	if len(w.CommitmentProof) == 0 {
		t.Fatal("CommitmentProof should not be empty")
	}
}

func TestWitnessBuilderBuildFinalized(t *testing.T) {
	wb := NewWitnessBuilder()
	wb.AddRead(makeStem(0x01), []byte{0x00}, makeValue(0xaa))
	wb.Build()

	// After Build, further adds should fail.
	err := wb.AddRead(makeStem(0x02), []byte{0x01}, makeValue(0xbb))
	if err != ErrBuilderFinalized {
		t.Fatalf("expected ErrBuilderFinalized, got %v", err)
	}

	err = wb.AddWrite(makeStem(0x03), []byte{0x02}, makeValue(0xcc), makeValue(0xdd))
	if err != ErrBuilderFinalized {
		t.Fatalf("expected ErrBuilderFinalized for write, got %v", err)
	}
}

func TestWitnessBuilderReset(t *testing.T) {
	wb := NewWitnessBuilder()
	wb.AddRead(makeStem(0x01), []byte{0x00}, makeValue(0xaa))
	wb.Build()

	wb.Reset()

	if wb.AccessCount() != 0 {
		t.Fatalf("AccessCount after Reset = %d, want 0", wb.AccessCount())
	}
	if wb.StemCount() != 0 {
		t.Fatalf("StemCount after Reset = %d, want 0", wb.StemCount())
	}

	// Should be able to add after reset.
	err := wb.AddRead(makeStem(0x02), []byte{0x01}, makeValue(0xbb))
	if err != nil {
		t.Fatalf("AddRead after Reset: %v", err)
	}
}

func TestWitnessBuilderNilStem(t *testing.T) {
	wb := NewWitnessBuilder()
	err := wb.AddRead(nil, []byte{0x00}, makeValue(0xaa))
	if err != ErrNilStem {
		t.Fatalf("expected ErrNilStem, got %v", err)
	}
}

func TestWitnessBuilderInvalidStemSize(t *testing.T) {
	wb := NewWitnessBuilder()
	shortStem := make([]byte, 10) // not 31
	err := wb.AddRead(shortStem, []byte{0x00}, makeValue(0xaa))
	if err != ErrInvalidStemSize {
		t.Fatalf("expected ErrInvalidStemSize, got %v", err)
	}
}

func TestWitnessBuilderInvalidValueSize(t *testing.T) {
	wb := NewWitnessBuilder()
	stem := makeStem(0x01)
	shortVal := []byte{0x01, 0x02} // not 32

	err := wb.AddRead(stem, []byte{0x00}, shortVal)
	if err != ErrInvalidValueSize {
		t.Fatalf("expected ErrInvalidValueSize, got %v", err)
	}
}

func TestWitnessBuilderNilValueAllowed(t *testing.T) {
	wb := NewWitnessBuilder()
	stem := makeStem(0x01)

	// nil value is allowed (represents empty/absent slot).
	err := wb.AddRead(stem, []byte{0x00}, nil)
	if err != nil {
		t.Fatalf("AddRead with nil value: %v", err)
	}
}

func TestEstimateWitnessSize(t *testing.T) {
	wb := NewWitnessBuilder()
	wb.AddRead(makeStem(0x01), []byte{0x00}, makeValue(0xaa))
	wb.AddWrite(makeStem(0x02), []byte{0x01}, makeValue(0xbb), makeValue(0xcc))
	w := wb.Build()

	size := EstimateWitnessSize(w)
	if size <= 0 {
		t.Fatalf("EstimateWitnessSize = %d, want > 0", size)
	}

	// Minimum: 2 stems * 31 + 2 suffixes * 1 + values + proof + overhead.
	minExpected := 2*StemSize + 2*1 + 3*ValueSize + len(w.CommitmentProof) + 16
	if size < minExpected {
		t.Fatalf("EstimateWitnessSize = %d, want >= %d", size, minExpected)
	}
}

func TestEstimateWitnessSizeNil(t *testing.T) {
	if EstimateWitnessSize(nil) != 0 {
		t.Fatal("EstimateWitnessSize(nil) should be 0")
	}
}

func TestMergeWitnesses(t *testing.T) {
	wb1 := NewWitnessBuilder()
	wb1.AddRead(makeStem(0x01), []byte{0x00}, makeValue(0xaa))
	w1 := wb1.Build()

	wb2 := NewWitnessBuilder()
	wb2.AddRead(makeStem(0x02), []byte{0x01}, makeValue(0xbb))
	w2 := wb2.Build()

	merged, err := MergeWitnesses(w1, w2)
	if err != nil {
		t.Fatalf("MergeWitnesses: %v", err)
	}
	if len(merged.Stems) != 2 {
		t.Fatalf("merged stems = %d, want 2", len(merged.Stems))
	}
	if len(merged.Suffixes) != 2 {
		t.Fatalf("merged suffixes = %d, want 2", len(merged.Suffixes))
	}
}

func TestMergeWitnessesDeduplicateStems(t *testing.T) {
	stem := makeStem(0x01)

	wb1 := NewWitnessBuilder()
	wb1.AddRead(stem, []byte{0x00}, makeValue(0xaa))
	w1 := wb1.Build()

	wb2 := NewWitnessBuilder()
	wb2.AddRead(stem, []byte{0x01}, makeValue(0xbb))
	w2 := wb2.Build()

	merged, err := MergeWitnesses(w1, w2)
	if err != nil {
		t.Fatalf("MergeWitnesses: %v", err)
	}

	// Same stem should appear only once.
	if len(merged.Stems) != 1 {
		t.Fatalf("merged stems = %d, want 1 (deduplicated)", len(merged.Stems))
	}
	// But both accesses should be present.
	if len(merged.Suffixes) != 2 {
		t.Fatalf("merged suffixes = %d, want 2", len(merged.Suffixes))
	}
}

func TestMergeWitnessesNil(t *testing.T) {
	_, err := MergeWitnesses(nil, nil)
	if err != ErrNilWitness {
		t.Fatalf("expected ErrNilWitness, got %v", err)
	}

	wb := NewWitnessBuilder()
	wb.AddRead(makeStem(0x01), []byte{0x00}, makeValue(0xaa))
	w := wb.Build()

	// One nil, one valid.
	result, err := MergeWitnesses(w, nil)
	if err != nil {
		t.Fatalf("MergeWitnesses(w, nil): %v", err)
	}
	if result != w {
		t.Fatal("MergeWitnesses(w, nil) should return w")
	}

	result, err = MergeWitnesses(nil, w)
	if err != nil {
		t.Fatalf("MergeWitnesses(nil, w): %v", err)
	}
	if result != w {
		t.Fatal("MergeWitnesses(nil, w) should return w")
	}
}

func TestVerifyWitnessCompleteness(t *testing.T) {
	wb := NewWitnessBuilder()
	stem := makeStem(0x01)
	wb.AddRead(stem, []byte{0x00}, makeValue(0xaa))
	wb.AddRead(stem, []byte{0x01}, makeValue(0xbb))
	w := wb.Build()

	// Build accessed keys: stem(31) || suffix(1) = 32 bytes.
	key1 := make([]byte, KeySize)
	copy(key1[:StemSize], stem)
	key1[StemSize] = 0x00

	key2 := make([]byte, KeySize)
	copy(key2[:StemSize], stem)
	key2[StemSize] = 0x01

	complete, missing := VerifyWitnessCompleteness(w, [][]byte{key1, key2})
	if !complete {
		t.Fatalf("witness should be complete, missing: %d keys", len(missing))
	}
}

func TestVerifyWitnessCompletenessIncomplete(t *testing.T) {
	wb := NewWitnessBuilder()
	wb.AddRead(makeStem(0x01), []byte{0x00}, makeValue(0xaa))
	w := wb.Build()

	// Key with a different stem that is not in the witness.
	missingKey := make([]byte, KeySize)
	copy(missingKey[:StemSize], makeStem(0xFF))
	missingKey[StemSize] = 0x00

	complete, missing := VerifyWitnessCompleteness(w, [][]byte{missingKey})
	if complete {
		t.Fatal("witness should be incomplete")
	}
	if len(missing) != 1 {
		t.Fatalf("missing count = %d, want 1", len(missing))
	}
}

func TestVerifyWitnessCompletenessNilWitness(t *testing.T) {
	// Nil witness with no keys = complete.
	complete, _ := VerifyWitnessCompleteness(nil, nil)
	if !complete {
		t.Fatal("nil witness with no keys should be complete")
	}

	// Nil witness with keys = incomplete.
	key := make([]byte, KeySize)
	complete, missing := VerifyWitnessCompleteness(nil, [][]byte{key})
	if complete {
		t.Fatal("nil witness with keys should be incomplete")
	}
	if len(missing) != 1 {
		t.Fatalf("missing count = %d, want 1", len(missing))
	}
}

func TestWitnessBuilderConcurrency(t *testing.T) {
	wb := NewWitnessBuilder()

	var wg sync.WaitGroup
	for i := byte(0); i < 50; i++ {
		wg.Add(1)
		go func(idx byte) {
			defer wg.Done()
			stem := makeStem(idx)
			suffix := []byte{idx}
			value := makeValue(idx)
			_ = wb.AddRead(stem, suffix, value)
		}(i)
	}
	wg.Wait()

	if wb.AccessCount() != 50 {
		t.Fatalf("AccessCount = %d, want 50", wb.AccessCount())
	}
	if wb.StemCount() != 50 {
		t.Fatalf("StemCount = %d, want 50", wb.StemCount())
	}

	w := wb.Build()
	if len(w.Stems) != 50 {
		t.Fatalf("Stems = %d, want 50", len(w.Stems))
	}
}

func TestWitnessBuilderAddWriteNilNewValue(t *testing.T) {
	wb := NewWitnessBuilder()
	stem := makeStem(0x01)
	oldVal := makeValue(0xaa)

	// Write with nil new value (deletion).
	err := wb.AddWrite(stem, []byte{0x00}, oldVal, nil)
	if err != nil {
		t.Fatalf("AddWrite with nil newValue: %v", err)
	}

	w := wb.Build()
	if w.NewValues[0] != nil {
		t.Fatal("nil newValue should remain nil in witness")
	}
	if !bytes.Equal(w.CurrentValues[0], oldVal) {
		t.Fatal("CurrentValues should match oldValue")
	}
}

func TestWitnessBuilderCommitmentProof(t *testing.T) {
	wb := NewWitnessBuilder()
	wb.AddRead(makeStem(0x01), []byte{0x00}, makeValue(0xaa))
	wb.AddRead(makeStem(0x02), []byte{0x01}, makeValue(0xbb))
	w := wb.Build()

	// Verify the placeholder proof contains the marker.
	if !bytes.HasPrefix(w.CommitmentProof, []byte("VERKLE_PROOF")) {
		t.Fatal("CommitmentProof should start with VERKLE_PROOF marker")
	}
}

func TestEstimateWitnessSizeGrowsWithAccesses(t *testing.T) {
	wb1 := NewWitnessBuilder()
	wb1.AddRead(makeStem(0x01), []byte{0x00}, makeValue(0xaa))
	w1 := wb1.Build()

	wb2 := NewWitnessBuilder()
	for i := byte(0); i < 10; i++ {
		wb2.AddRead(makeStem(i), []byte{i}, makeValue(i))
	}
	w2 := wb2.Build()

	size1 := EstimateWitnessSize(w1)
	size2 := EstimateWitnessSize(w2)
	if size2 <= size1 {
		t.Fatalf("larger witness (%d) should have bigger size than smaller (%d)", size2, size1)
	}
}
