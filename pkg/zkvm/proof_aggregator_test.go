package zkvm

import (
	"crypto/sha256"
	"sync"
	"testing"
	"time"
)

// makeTestProof creates a ZKExecutionProof with a unique ID based on the seed.
func makeTestProof(seed byte) *ZKExecutionProof {
	var id, prog, input, output [32]byte
	id[0] = seed
	prog[0] = seed + 1
	input[0] = seed + 2
	output[0] = seed + 3

	return &ZKExecutionProof{
		ProofID:     id,
		ProgramHash: prog,
		InputHash:   input,
		OutputHash:  output,
		ProofBytes:  []byte{seed, seed + 1, seed + 2, seed + 3},
		GasUsed:     uint64(seed) * 10000,
		Timestamp:   time.Now().Unix(),
	}
}

func TestAggregatorNewAggregator(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	if agg == nil {
		t.Fatal("NewZKProofAggregator returned nil with nil config")
	}
	if agg.config.MaxBatchSize != 16 {
		t.Errorf("default MaxBatchSize = %d, want 16", agg.config.MaxBatchSize)
	}
	if agg.config.ProofSystem != "groth16" {
		t.Errorf("default ProofSystem = %s, want groth16", agg.config.ProofSystem)
	}
	if agg.config.AggregationDepth != 2 {
		t.Errorf("default AggregationDepth = %d, want 2", agg.config.AggregationDepth)
	}
	if agg.config.GasPerProof != 100000 {
		t.Errorf("default GasPerProof = %d, want 100000", agg.config.GasPerProof)
	}
}

func TestAggregatorCustomConfig(t *testing.T) {
	cfg := &AggregatorConfig{
		MaxBatchSize:     8,
		ProofSystem:      "plonk",
		AggregationDepth: 3,
		GasPerProof:      50000,
	}
	agg := NewZKProofAggregator(cfg)
	if agg.config.MaxBatchSize != 8 {
		t.Errorf("MaxBatchSize = %d, want 8", agg.config.MaxBatchSize)
	}
	if agg.config.ProofSystem != "plonk" {
		t.Errorf("ProofSystem = %s, want plonk", agg.config.ProofSystem)
	}
	if agg.config.AggregationDepth != 3 {
		t.Errorf("AggregationDepth = %d, want 3", agg.config.AggregationDepth)
	}
}

func TestAggregatorAddProof(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	proof := makeTestProof(1)
	if err := agg.AddProof(proof); err != nil {
		t.Fatalf("AddProof: %v", err)
	}
	if agg.PendingCount() != 1 {
		t.Errorf("PendingCount = %d, want 1", agg.PendingCount())
	}
}

func TestAggregatorAddNilProof(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	if err := agg.AddProof(nil); err != ErrAggregatorNilProof {
		t.Errorf("expected ErrAggregatorNilProof, got %v", err)
	}
}

func TestAggregatorAddEmptyProofBytes(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	p := makeTestProof(1)
	p.ProofBytes = nil
	if err := agg.AddProof(p); err != ErrAggregatorEmptyProof {
		t.Errorf("expected ErrAggregatorEmptyProof, got %v", err)
	}
}

func TestAggregatorDuplicateProof(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	proof := makeTestProof(1)
	if err := agg.AddProof(proof); err != nil {
		t.Fatalf("first AddProof: %v", err)
	}

	// Same proof ID should be rejected.
	dup := makeTestProof(1)
	if err := agg.AddProof(dup); err != ErrAggregatorDuplicateID {
		t.Errorf("expected ErrAggregatorDuplicateID, got %v", err)
	}
}

func TestAggregatorMaxBatch(t *testing.T) {
	cfg := &AggregatorConfig{MaxBatchSize: 3, ProofSystem: "groth16", AggregationDepth: 2, GasPerProof: 100000}
	agg := NewZKProofAggregator(cfg)

	for i := byte(1); i <= 3; i++ {
		if err := agg.AddProof(makeTestProof(i)); err != nil {
			t.Fatalf("AddProof(%d): %v", i, err)
		}
	}

	if err := agg.AddProof(makeTestProof(4)); err != ErrAggregatorBatchFull {
		t.Errorf("expected ErrAggregatorBatchFull, got %v", err)
	}
}

func TestAggregatorAggregate(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	for i := byte(1); i <= 5; i++ {
		if err := agg.AddProof(makeTestProof(i)); err != nil {
			t.Fatalf("AddProof: %v", err)
		}
	}

	result, err := agg.Aggregate()
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if result.ProofCount != 5 {
		t.Errorf("ProofCount = %d, want 5", result.ProofCount)
	}
	if len(result.ProgramHashes) != 5 {
		t.Errorf("ProgramHashes len = %d, want 5", len(result.ProgramHashes))
	}
	if result.MerkleRoot == [32]byte{} {
		t.Error("MerkleRoot should not be zero")
	}
	if len(result.RootProof) == 0 {
		t.Error("RootProof should not be empty")
	}
	if result.AggregatedAt == 0 {
		t.Error("AggregatedAt should be set")
	}

	// After aggregation, pending should be empty.
	if agg.PendingCount() != 0 {
		t.Errorf("PendingCount after aggregate = %d, want 0", agg.PendingCount())
	}
}

func TestAggregatorEmptyAggregate(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	_, err := agg.Aggregate()
	if err != ErrAggregatorEmptyBatch {
		t.Errorf("expected ErrAggregatorEmptyBatch, got %v", err)
	}
}

func TestAggregatorVerifyAggregated(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	for i := byte(1); i <= 4; i++ {
		if err := agg.AddProof(makeTestProof(i)); err != nil {
			t.Fatalf("AddProof: %v", err)
		}
	}

	result, err := agg.Aggregate()
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	if !agg.VerifyAggregated(result) {
		t.Error("VerifyAggregated should return true for valid proof")
	}
}

func TestAggregatorVerifyAggregatedNil(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	if agg.VerifyAggregated(nil) {
		t.Error("VerifyAggregated(nil) should return false")
	}
}

func TestAggregatorVerifyAggregatedTampered(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	for i := byte(1); i <= 3; i++ {
		agg.AddProof(makeTestProof(i))
	}
	result, _ := agg.Aggregate()

	// Tamper with the root proof.
	result.RootProof[0] ^= 0xFF
	if agg.VerifyAggregated(result) {
		t.Error("VerifyAggregated should return false for tampered proof")
	}
}

func TestAggregatorPendingCount(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	if agg.PendingCount() != 0 {
		t.Errorf("initial PendingCount = %d, want 0", agg.PendingCount())
	}
	agg.AddProof(makeTestProof(1))
	agg.AddProof(makeTestProof(2))
	if agg.PendingCount() != 2 {
		t.Errorf("PendingCount = %d, want 2", agg.PendingCount())
	}
}

func TestAggregatorEstimateGas(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	if agg.EstimateGas() != 0 {
		t.Errorf("EstimateGas with 0 proofs = %d, want 0", agg.EstimateGas())
	}

	for i := byte(1); i <= 4; i++ {
		agg.AddProof(makeTestProof(i))
	}

	gas := agg.EstimateGas()
	// base (100000) + 4 * 10000 = 140000
	expected := uint64(100000 + 4*10000)
	if gas != expected {
		t.Errorf("EstimateGas = %d, want %d", gas, expected)
	}
}

func TestAggregatorReset(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	for i := byte(1); i <= 5; i++ {
		agg.AddProof(makeTestProof(i))
	}
	if agg.PendingCount() != 5 {
		t.Fatalf("PendingCount before reset = %d, want 5", agg.PendingCount())
	}

	agg.Reset()
	if agg.PendingCount() != 0 {
		t.Errorf("PendingCount after reset = %d, want 0", agg.PendingCount())
	}

	// Should be able to add the same proof IDs again after reset.
	if err := agg.AddProof(makeTestProof(1)); err != nil {
		t.Errorf("AddProof after reset: %v", err)
	}
}

func TestAggregatorStats(t *testing.T) {
	agg := NewZKProofAggregator(nil)

	stats := agg.Stats()
	if stats.TotalAggregated != 0 {
		t.Errorf("initial TotalAggregated = %d, want 0", stats.TotalAggregated)
	}

	// Do two aggregations with different batch sizes.
	for i := byte(1); i <= 4; i++ {
		agg.AddProof(makeTestProof(i))
	}
	agg.Aggregate()

	for i := byte(10); i <= 15; i++ {
		agg.AddProof(makeTestProof(i))
	}
	agg.Aggregate()

	stats = agg.Stats()
	if stats.TotalAggregated != 2 {
		t.Errorf("TotalAggregated = %d, want 2", stats.TotalAggregated)
	}
	if stats.TotalProofs != 10 {
		t.Errorf("TotalProofs = %d, want 10", stats.TotalProofs)
	}
	if stats.AvgBatchSize != 5 {
		t.Errorf("AvgBatchSize = %d, want 5", stats.AvgBatchSize)
	}
}

func TestAggregatorTotalGas(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	p1 := makeTestProof(1)
	p1.GasUsed = 50000
	p2 := makeTestProof(2)
	p2.GasUsed = 75000

	agg.AddProof(p1)
	agg.AddProof(p2)

	result, err := agg.Aggregate()
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if result.TotalGas != 125000 {
		t.Errorf("TotalGas = %d, want 125000", result.TotalGas)
	}
}

func TestAggregatorAggregatedProofFields(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	p := makeTestProof(42)
	agg.AddProof(p)

	result, err := agg.Aggregate()
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	// Single proof aggregation: Merkle root should equal the leaf.
	expectedLeaf := computeProofLeaf(p)
	if result.MerkleRoot != expectedLeaf {
		t.Error("single proof MerkleRoot should equal the leaf commitment")
	}
	if result.ProgramHashes[0] != p.ProgramHash {
		t.Error("ProgramHashes[0] should match the proof's program hash")
	}
}

func TestAggregatorLargeProof(t *testing.T) {
	agg := NewZKProofAggregator(nil)
	p := makeTestProof(1)
	// Large proof data (1 KiB).
	p.ProofBytes = make([]byte, 1024)
	for i := range p.ProofBytes {
		p.ProofBytes[i] = byte(i % 256)
	}

	if err := agg.AddProof(p); err != nil {
		t.Fatalf("AddProof large proof: %v", err)
	}
	result, err := agg.Aggregate()
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if !agg.VerifyAggregated(result) {
		t.Error("large proof should verify after aggregation")
	}
}

func TestAggregatorConcurrentAccess(t *testing.T) {
	cfg := &AggregatorConfig{MaxBatchSize: 100, ProofSystem: "groth16", AggregationDepth: 2, GasPerProof: 100000}
	agg := NewZKProofAggregator(cfg)

	var wg sync.WaitGroup
	errCh := make(chan error, 50)

	for i := byte(0); i < 50; i++ {
		wg.Add(1)
		go func(seed byte) {
			defer wg.Done()
			if err := agg.AddProof(makeTestProof(seed)); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent AddProof error: %v", err)
	}

	if agg.PendingCount() != 50 {
		t.Errorf("PendingCount = %d, want 50", agg.PendingCount())
	}

	result, err := agg.Aggregate()
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if result.ProofCount != 50 {
		t.Errorf("ProofCount = %d, want 50", result.ProofCount)
	}
}

func TestAggregatorMerkleRootDeterministic(t *testing.T) {
	// Verify that the same set of proofs always produces the same Merkle root.
	makeProofs := func() []*ZKExecutionProof {
		proofs := make([]*ZKExecutionProof, 4)
		for i := byte(0); i < 4; i++ {
			proofs[i] = makeTestProof(i + 10)
		}
		return proofs
	}

	agg1 := NewZKProofAggregator(nil)
	for _, p := range makeProofs() {
		agg1.AddProof(p)
	}
	r1, _ := agg1.Aggregate()

	agg2 := NewZKProofAggregator(nil)
	for _, p := range makeProofs() {
		agg2.AddProof(p)
	}
	r2, _ := agg2.Aggregate()

	if r1.MerkleRoot != r2.MerkleRoot {
		t.Error("same proofs should produce identical Merkle roots")
	}
}

func TestAggregatorComputeProofLeaf(t *testing.T) {
	p := makeTestProof(1)
	leaf := computeProofLeaf(p)

	// Verify leaf matches manual computation.
	h := sha256.New()
	h.Write(p.ProofID[:])
	h.Write(p.ProgramHash[:])
	h.Write(p.InputHash[:])
	h.Write(p.OutputHash[:])
	h.Write(p.ProofBytes)
	var expected [32]byte
	copy(expected[:], h.Sum(nil))

	if leaf != expected {
		t.Error("computeProofLeaf does not match manual hash")
	}
}
