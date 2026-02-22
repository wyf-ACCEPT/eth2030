package proofs

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makeSingleProof creates a SingleProof with the given type and a seed byte.
func makeSingleProof(pt SingleProofType, seed byte) SingleProof {
	return SingleProof{
		Type:         pt,
		Data:         []byte{seed, 0xAB, 0xCD, 0xEF},
		PublicInputs: [][]byte{{seed, 0x01}, {seed, 0x02}},
		BlockHash:    types.Hash{seed},
		ProverID:     "test-prover",
	}
}

// makeSingleProofs creates n SingleProofs of the given type.
func makeSingleProofs(pt SingleProofType, n int) []SingleProof {
	proofs := make([]SingleProof, n)
	for i := 0; i < n; i++ {
		proofs[i] = makeSingleProof(pt, byte(i))
	}
	return proofs
}

func TestSingleProofTypeString(t *testing.T) {
	tests := []struct {
		pt   SingleProofType
		want string
	}{
		{MerkleProof, "Merkle"},
		{KZGProof, "KZG"},
		{STARKProof, "STARK"},
		{SingleProofType(99), "unknown"},
	}
	for _, tt := range tests {
		got := tt.pt.String()
		if got != tt.want {
			t.Errorf("SingleProofType(%d).String() = %q, want %q", tt.pt, got, tt.want)
		}
	}
}

func TestSingleProofTypeIsValid(t *testing.T) {
	if !MerkleProof.IsValid() {
		t.Error("MerkleProof should be valid")
	}
	if !KZGProof.IsValid() {
		t.Error("KZGProof should be valid")
	}
	if !STARKProof.IsValid() {
		t.Error("STARKProof should be valid")
	}
	if SingleProofType(99).IsValid() {
		t.Error("type 99 should not be valid")
	}
}

func TestSingleProofHash(t *testing.T) {
	p1 := makeSingleProof(MerkleProof, 0x01)
	p2 := makeSingleProof(MerkleProof, 0x02)

	h1 := p1.Hash()
	h2 := p2.Hash()

	if h1 == h2 {
		t.Error("different proofs should have different hashes")
	}

	// Same proof should produce the same hash.
	h1b := p1.Hash()
	if h1 != h1b {
		t.Error("same proof should produce the same hash")
	}
}

func TestAddProof(t *testing.T) {
	agg := NewMultiProofAggregator()
	proof := makeSingleProof(KZGProof, 0x01)

	err := agg.AddProof(proof)
	if err != nil {
		t.Fatalf("AddProof failed: %v", err)
	}
	if agg.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", agg.PendingCount())
	}
}

func TestAddProofEmptyData(t *testing.T) {
	agg := NewMultiProofAggregator()
	proof := SingleProof{
		Type: MerkleProof,
		Data: nil,
	}

	err := agg.AddProof(proof)
	if err != ErrSingleProofNoData {
		t.Errorf("expected ErrSingleProofNoData, got %v", err)
	}
}

func TestAddProofInvalidType(t *testing.T) {
	agg := NewMultiProofAggregator()
	proof := SingleProof{
		Type: SingleProofType(99),
		Data: []byte{0x01},
	}

	err := agg.AddProof(proof)
	if err != ErrSingleProofBadType {
		t.Errorf("expected ErrSingleProofBadType, got %v", err)
	}
}

func TestAggregateEmpty(t *testing.T) {
	agg := NewMultiProofAggregator()

	_, err := agg.Aggregate()
	if err != ErrAggNothingToAggregate {
		t.Errorf("expected ErrAggNothingToAggregate, got %v", err)
	}
}

func TestAggregateBasic(t *testing.T) {
	agg := NewMultiProofAggregator()
	for _, p := range makeSingleProofs(MerkleProof, 5) {
		agg.AddProof(p)
	}

	result, err := agg.Aggregate()
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if result.BatchSize != 5 {
		t.Errorf("expected batch size 5, got %d", result.BatchSize)
	}
	if len(result.Proofs) != 5 {
		t.Errorf("expected 5 proofs, got %d", len(result.Proofs))
	}
	if result.MergedCommitment == (types.Hash{}) {
		t.Error("merged commitment should be non-zero")
	}

	// Pending should be cleared after aggregation.
	if agg.PendingCount() != 0 {
		t.Errorf("expected 0 pending after aggregate, got %d", agg.PendingCount())
	}
}

func TestAggregateDeterministic(t *testing.T) {
	proofs := makeSingleProofs(KZGProof, 3)

	agg1 := NewMultiProofAggregator()
	for _, p := range proofs {
		agg1.AddProof(p)
	}
	r1, _ := agg1.Aggregate()

	agg2 := NewMultiProofAggregator()
	for _, p := range proofs {
		agg2.AddProof(p)
	}
	r2, _ := agg2.Aggregate()

	if r1.MergedCommitment != r2.MergedCommitment {
		t.Error("aggregation should be deterministic")
	}
}

func TestVerifyAggregatedValid(t *testing.T) {
	agg := NewMultiProofAggregator()
	for _, p := range makeSingleProofs(STARKProof, 4) {
		agg.AddProof(p)
	}

	result, _ := agg.Aggregate()
	valid, err := VerifyAggregated(result)
	if err != nil {
		t.Fatalf("VerifyAggregated error: %v", err)
	}
	if !valid {
		t.Error("expected valid aggregated proof")
	}
}

func TestVerifyAggregatedTampered(t *testing.T) {
	agg := NewMultiProofAggregator()
	for _, p := range makeSingleProofs(MerkleProof, 3) {
		agg.AddProof(p)
	}

	result, _ := agg.Aggregate()
	// Tamper with the commitment.
	result.MergedCommitment[0] ^= 0xFF

	valid, err := VerifyAggregated(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("expected invalid for tampered commitment")
	}
}

func TestVerifyAggregatedBatchSizeMismatch(t *testing.T) {
	agg := NewMultiProofAggregator()
	for _, p := range makeSingleProofs(KZGProof, 3) {
		agg.AddProof(p)
	}

	result, _ := agg.Aggregate()
	result.BatchSize = 999

	_, err := VerifyAggregated(result)
	if err != ErrAggProofTampered {
		t.Errorf("expected ErrAggProofTampered, got %v", err)
	}
}

func TestVerifyAggregatedNil(t *testing.T) {
	_, err := VerifyAggregated(nil)
	if err != ErrAggProofNil {
		t.Errorf("expected ErrAggProofNil, got %v", err)
	}
}

func TestVerifyAggregatedEmpty(t *testing.T) {
	_, err := VerifyAggregated(&AggregatedSingleProof{})
	if err != ErrAggProofEmpty {
		t.Errorf("expected ErrAggProofEmpty, got %v", err)
	}
}

func TestEstimateGasSavingsMerkle(t *testing.T) {
	est, err := EstimateGasSavings(MerkleProof, 100)
	if err != nil {
		t.Fatalf("EstimateGasSavings error: %v", err)
	}
	if est.IndividualGas != 100*MerkleVerifyGas {
		t.Errorf("expected individual gas %d, got %d", 100*MerkleVerifyGas, est.IndividualGas)
	}
	expectedAgg := AggregateVerifyBaseGas + 100*AggregateVerifyPerProofGas
	if est.AggregatedGas != expectedAgg {
		t.Errorf("expected aggregated gas %d, got %d", expectedAgg, est.AggregatedGas)
	}
	if est.Savings != est.IndividualGas-est.AggregatedGas {
		t.Error("savings calculation mismatch")
	}
	if est.SavingsPercent <= 0 {
		t.Error("expected positive savings percent")
	}
}

func TestEstimateGasSavingsSTARK(t *testing.T) {
	est, err := EstimateGasSavings(STARKProof, 10)
	if err != nil {
		t.Fatalf("EstimateGasSavings error: %v", err)
	}
	if est.IndividualGas != 10*STARKVerifyGas {
		t.Errorf("expected %d, got %d", 10*STARKVerifyGas, est.IndividualGas)
	}
	if est.Savings == 0 {
		t.Error("expected non-zero savings for STARK proofs")
	}
}

func TestEstimateGasSavingsZero(t *testing.T) {
	_, err := EstimateGasSavings(MerkleProof, 0)
	if err != ErrAggGasNumZero {
		t.Errorf("expected ErrAggGasNumZero, got %v", err)
	}
}

func TestEstimateGasSavingsInvalidType(t *testing.T) {
	_, err := EstimateGasSavings(SingleProofType(99), 10)
	if err != ErrSingleProofBadType {
		t.Errorf("expected ErrSingleProofBadType, got %v", err)
	}
}

func TestSplitBatch(t *testing.T) {
	agg := NewMultiProofAggregator()
	for _, p := range makeSingleProofs(KZGProof, 10) {
		agg.AddProof(p)
	}

	result, _ := agg.Aggregate()
	batches, err := SplitBatch(result, 3)
	if err != nil {
		t.Fatalf("SplitBatch error: %v", err)
	}

	// 10 proofs / 3 per batch = 4 batches (3+3+3+1).
	if len(batches) != 4 {
		t.Fatalf("expected 4 batches, got %d", len(batches))
	}

	// First three batches should have 3 proofs each.
	for i := 0; i < 3; i++ {
		if batches[i].BatchSize != 3 {
			t.Errorf("batch %d: expected size 3, got %d", i, batches[i].BatchSize)
		}
	}
	// Last batch should have 1 proof.
	if batches[3].BatchSize != 1 {
		t.Errorf("last batch: expected size 1, got %d", batches[3].BatchSize)
	}

	// Each sub-batch should be independently verifiable.
	for i, b := range batches {
		valid, err := VerifyAggregated(b)
		if err != nil {
			t.Fatalf("batch %d verify error: %v", i, err)
		}
		if !valid {
			t.Errorf("batch %d should be valid", i)
		}
	}
}

func TestSplitBatchNil(t *testing.T) {
	_, err := SplitBatch(nil, 5)
	if err != ErrAggProofNil {
		t.Errorf("expected ErrAggProofNil, got %v", err)
	}
}

func TestSplitBatchZeroSize(t *testing.T) {
	agg := NewMultiProofAggregator()
	agg.AddProof(makeSingleProof(MerkleProof, 1))
	result, _ := agg.Aggregate()

	_, err := SplitBatch(result, 0)
	if err != ErrAggBatchSizeZero {
		t.Errorf("expected ErrAggBatchSizeZero, got %v", err)
	}
}

func TestSplitBatchSingleChunk(t *testing.T) {
	agg := NewMultiProofAggregator()
	for _, p := range makeSingleProofs(STARKProof, 5) {
		agg.AddProof(p)
	}

	result, _ := agg.Aggregate()
	batches, err := SplitBatch(result, 100)
	if err != nil {
		t.Fatalf("SplitBatch error: %v", err)
	}
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch when maxBatchSize > total, got %d", len(batches))
	}
	if batches[0].BatchSize != 5 {
		t.Errorf("expected batch size 5, got %d", batches[0].BatchSize)
	}
}

func TestProofTypeCounts(t *testing.T) {
	agg := NewMultiProofAggregator()
	agg.AddProof(makeSingleProof(MerkleProof, 1))
	agg.AddProof(makeSingleProof(MerkleProof, 2))
	agg.AddProof(makeSingleProof(KZGProof, 3))
	agg.AddProof(makeSingleProof(STARKProof, 4))

	result, _ := agg.Aggregate()
	counts := ProofTypeCounts(result)

	if counts[MerkleProof] != 2 {
		t.Errorf("expected 2 Merkle, got %d", counts[MerkleProof])
	}
	if counts[KZGProof] != 1 {
		t.Errorf("expected 1 KZG, got %d", counts[KZGProof])
	}
	if counts[STARKProof] != 1 {
		t.Errorf("expected 1 STARK, got %d", counts[STARKProof])
	}
}

func TestProofTypeCountsNil(t *testing.T) {
	counts := ProofTypeCounts(nil)
	if len(counts) != 0 {
		t.Errorf("expected empty counts for nil, got %v", counts)
	}
}

func TestEstimateMixedGasSavings(t *testing.T) {
	agg := NewMultiProofAggregator()
	agg.AddProof(makeSingleProof(MerkleProof, 1))
	agg.AddProof(makeSingleProof(KZGProof, 2))
	agg.AddProof(makeSingleProof(STARKProof, 3))

	result, _ := agg.Aggregate()
	est, err := EstimateMixedGasSavings(result)
	if err != nil {
		t.Fatalf("EstimateMixedGasSavings error: %v", err)
	}

	expectedIndividual := MerkleVerifyGas + KZGVerifyGas + STARKVerifyGas
	if est.IndividualGas != expectedIndividual {
		t.Errorf("expected individual gas %d, got %d", expectedIndividual, est.IndividualGas)
	}
	if est.Savings == 0 {
		t.Error("expected non-zero savings for mixed proofs")
	}
}

func TestConcurrentAddProof(t *testing.T) {
	agg := NewMultiProofAggregator()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(seed byte) {
			defer wg.Done()
			agg.AddProof(makeSingleProof(MerkleProof, seed))
		}(byte(i))
	}
	wg.Wait()

	if agg.PendingCount() != 50 {
		t.Errorf("expected 50 pending, got %d", agg.PendingCount())
	}
}

func TestAddProofDataIsolation(t *testing.T) {
	agg := NewMultiProofAggregator()

	data := []byte{0x01, 0x02, 0x03}
	proof := SingleProof{
		Type: MerkleProof,
		Data: data,
	}
	agg.AddProof(proof)

	// Mutate the original data.
	data[0] = 0xFF

	result, _ := agg.Aggregate()
	// The aggregated proof should have the original data.
	if result.Proofs[0].Data[0] != 0x01 {
		t.Error("proof data should be isolated from external mutation")
	}
}

func TestEstimateGasSavingsKZG(t *testing.T) {
	est, err := EstimateGasSavings(KZGProof, 20)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if est.IndividualGas != 20*KZGVerifyGas {
		t.Errorf("expected %d, got %d", 20*KZGVerifyGas, est.IndividualGas)
	}
	if est.SavingsPercent <= 0 {
		t.Error("expected positive savings percent for KZG")
	}
}
