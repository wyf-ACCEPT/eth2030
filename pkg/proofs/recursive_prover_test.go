package proofs

import (
	"testing"
)

func makeRecursiveTestProofs(n int) []AggregateableProof {
	proofs := make([]AggregateableProof, n)
	for i := 0; i < n; i++ {
		data := make([]byte, 32)
		data[0] = byte(i)
		data[31] = byte(i + 1)
		proofs[i] = &SimpleAggregateable{
			Data: data,
			Kind: ProofType(i % 4), // rotate through SNARK, STARK, IPA, KZG
		}
	}
	return proofs
}

func TestRecursiveProverComposeAndVerify(t *testing.T) {
	prover := NewRecursiveProver(0)
	proofs := makeRecursiveTestProofs(4)

	composed, err := prover.ComposeProofs(proofs)
	if err != nil {
		t.Fatalf("ComposeProofs: %v", err)
	}

	if composed.TotalProofs != 4 {
		t.Errorf("TotalProofs: expected 4, got %d", composed.TotalProofs)
	}
	if composed.Depth < 1 {
		t.Errorf("Depth should be >= 1, got %d", composed.Depth)
	}
	if composed.Root == [32]byte{} {
		t.Error("Root should be non-zero")
	}

	valid, err := prover.VerifyRecursive(composed)
	if err != nil {
		t.Fatalf("VerifyRecursive: %v", err)
	}
	if !valid {
		t.Error("expected valid proof")
	}
}

func TestRecursiveProverSingleProof(t *testing.T) {
	prover := NewRecursiveProver(0)
	proofs := makeRecursiveTestProofs(1)

	composed, err := prover.ComposeProofs(proofs)
	if err != nil {
		t.Fatalf("ComposeProofs: %v", err)
	}

	if composed.TotalProofs != 1 {
		t.Errorf("expected 1 proof, got %d", composed.TotalProofs)
	}
	if composed.Depth != 0 {
		t.Errorf("single proof depth should be 0, got %d", composed.Depth)
	}

	valid, err := prover.VerifyRecursive(composed)
	if err != nil {
		t.Fatalf("VerifyRecursive: %v", err)
	}
	if !valid {
		t.Error("expected valid proof")
	}
}

func TestRecursiveProverNoProofs(t *testing.T) {
	prover := NewRecursiveProver(0)
	_, err := prover.ComposeProofs(nil)
	if err != ErrRecNoProofs {
		t.Errorf("expected ErrRecNoProofs, got %v", err)
	}
}

func TestRecursiveProverEmptyProofData(t *testing.T) {
	prover := NewRecursiveProver(0)
	proofs := []AggregateableProof{
		&SimpleAggregateable{Data: nil, Kind: ZKSNARK},
	}
	_, err := prover.ComposeProofs(proofs)
	if err != ErrRecNoProofData {
		t.Errorf("expected ErrRecNoProofData, got %v", err)
	}
}

func TestRecursiveProverVerifyNil(t *testing.T) {
	prover := NewRecursiveProver(0)
	_, err := prover.VerifyRecursive(nil)
	if err != ErrRecNilProof {
		t.Errorf("expected ErrRecNilProof, got %v", err)
	}
}

func TestRecursiveProverVerifyEmptyTree(t *testing.T) {
	prover := NewRecursiveProver(0)
	_, err := prover.VerifyRecursive(&RecursiveProof{})
	if err != ErrRecEmptyTree {
		t.Errorf("expected ErrRecEmptyTree, got %v", err)
	}
}

func TestRecursiveProverTamperedRoot(t *testing.T) {
	prover := NewRecursiveProver(0)
	proofs := makeRecursiveTestProofs(4)

	composed, err := prover.ComposeProofs(proofs)
	if err != nil {
		t.Fatalf("ComposeProofs: %v", err)
	}

	// Tamper with the root.
	composed.Root[0] ^= 0xff

	valid, err := prover.VerifyRecursive(composed)
	if err == nil && valid {
		t.Error("expected verification to fail with tampered root")
	}
}

func TestRecursiveProverManyProofs(t *testing.T) {
	prover := NewRecursiveProver(0)
	proofs := makeRecursiveTestProofs(16)

	composed, err := prover.ComposeProofs(proofs)
	if err != nil {
		t.Fatalf("ComposeProofs: %v", err)
	}

	if composed.TotalProofs != 16 {
		t.Errorf("expected 16 proofs, got %d", composed.TotalProofs)
	}
	if composed.Depth != 4 {
		t.Errorf("expected depth 4 for 16 proofs, got %d", composed.Depth)
	}

	valid, err := prover.VerifyRecursive(composed)
	if err != nil {
		t.Fatalf("VerifyRecursive: %v", err)
	}
	if !valid {
		t.Error("expected valid proof")
	}
}

func TestRecursiveProverNonPowerOfTwo(t *testing.T) {
	prover := NewRecursiveProver(0)
	proofs := makeRecursiveTestProofs(5) // not power of 2

	composed, err := prover.ComposeProofs(proofs)
	if err != nil {
		t.Fatalf("ComposeProofs: %v", err)
	}

	if composed.TotalProofs != 5 {
		t.Errorf("expected 5 proofs, got %d", composed.TotalProofs)
	}

	valid, err := prover.VerifyRecursive(composed)
	if err != nil {
		t.Fatalf("VerifyRecursive: %v", err)
	}
	if !valid {
		t.Error("expected valid proof")
	}
}

func TestRecursiveBuildProofTree(t *testing.T) {
	rawProofs := make([][]byte, 8)
	for i := range rawProofs {
		rawProofs[i] = make([]byte, 32)
		rawProofs[i][0] = byte(i)
	}

	tree, err := BuildProofTree(rawProofs)
	if err != nil {
		t.Fatalf("BuildProofTree: %v", err)
	}

	if tree.LeafCount != 8 {
		t.Errorf("expected 8 leaves, got %d", tree.LeafCount)
	}
	if tree.Depth != 3 {
		t.Errorf("expected depth 3 for 8 leaves, got %d", tree.Depth)
	}
	if tree.Root == nil {
		t.Fatal("root should not be nil")
	}
}

func TestRecursiveBuildProofTreeEmpty(t *testing.T) {
	_, err := BuildProofTree(nil)
	if err != ErrRecNoProofs {
		t.Errorf("expected ErrRecNoProofs, got %v", err)
	}
}

func TestRecursiveBuildProofTreeEmptyData(t *testing.T) {
	_, err := BuildProofTree([][]byte{nil})
	if err != ErrRecNoProofData {
		t.Errorf("expected ErrRecNoProofData, got %v", err)
	}
}

func TestRecursiveOptimizeTree(t *testing.T) {
	rawProofs := make([][]byte, 5)
	for i := range rawProofs {
		rawProofs[i] = make([]byte, 16)
		rawProofs[i][0] = byte(i + 1)
	}

	tree, err := BuildProofTree(rawProofs)
	if err != nil {
		t.Fatalf("BuildProofTree: %v", err)
	}

	optimized := OptimizeTree(tree)
	if optimized.LeafCount != tree.LeafCount {
		t.Errorf("optimized should preserve leaf count: got %d", optimized.LeafCount)
	}
	if optimized.Root.Commitment != tree.Root.Commitment {
		t.Error("optimized root should have same commitment")
	}
}

func TestRecursiveOptimizeTreeNil(t *testing.T) {
	result := OptimizeTree(nil)
	if result != nil {
		t.Error("OptimizeTree(nil) should return nil")
	}
}

func TestRecursiveEstimateVerificationGas(t *testing.T) {
	rawProofs := make([][]byte, 4)
	for i := range rawProofs {
		rawProofs[i] = make([]byte, 32)
		rawProofs[i][0] = byte(i)
	}

	tree, err := BuildProofTree(rawProofs)
	if err != nil {
		t.Fatalf("BuildProofTree: %v", err)
	}

	gas := EstimateVerificationGas(tree)
	// 4 * 50000 + 3 * 100 = 200300
	expected := uint64(4*50000 + 3*100)
	if gas != expected {
		t.Errorf("expected gas %d, got %d", expected, gas)
	}
}

func TestRecursiveEstimateVerificationGasNil(t *testing.T) {
	gas := EstimateVerificationGas(nil)
	if gas != 0 {
		t.Errorf("expected 0 for nil tree, got %d", gas)
	}
}

func TestRecursiveCollectLeaves(t *testing.T) {
	rawProofs := [][]byte{
		{0x01, 0x02},
		{0x03, 0x04},
		{0x05, 0x06},
	}

	tree, err := BuildProofTree(rawProofs)
	if err != nil {
		t.Fatalf("BuildProofTree: %v", err)
	}

	collected := CollectLeaves(tree)
	if len(collected) != 3 {
		t.Fatalf("expected 3 leaves, got %d", len(collected))
	}
	for i, data := range collected {
		if data[0] != rawProofs[i][0] {
			t.Errorf("leaf %d: expected first byte %d, got %d", i, rawProofs[i][0], data[0])
		}
	}
}

func TestRecursiveComputeTreeStats(t *testing.T) {
	rawProofs := make([][]byte, 4)
	for i := range rawProofs {
		rawProofs[i] = make([]byte, 16)
		rawProofs[i][0] = byte(i + 1)
	}

	tree, err := BuildProofTree(rawProofs)
	if err != nil {
		t.Fatalf("BuildProofTree: %v", err)
	}

	stats := ComputeTreeStats(tree)
	if stats.LeafNodes != 4 {
		t.Errorf("expected 4 leaf nodes, got %d", stats.LeafNodes)
	}
	if stats.InternalNodes != 3 {
		t.Errorf("expected 3 internal nodes, got %d", stats.InternalNodes)
	}
	if stats.TotalNodes != 7 {
		t.Errorf("expected 7 total nodes, got %d", stats.TotalNodes)
	}
	if stats.MaxDepth != 2 {
		t.Errorf("expected max depth 2, got %d", stats.MaxDepth)
	}
	if stats.TotalBytes != 4*16 {
		t.Errorf("expected %d total bytes, got %d", 4*16, stats.TotalBytes)
	}
}

func TestRecursiveComputeTreeStatsNil(t *testing.T) {
	stats := ComputeTreeStats(nil)
	if stats.TotalNodes != 0 {
		t.Errorf("expected 0 nodes for nil tree, got %d", stats.TotalNodes)
	}
}

func TestRecursiveSimpleAggregateableInterface(t *testing.T) {
	sa := &SimpleAggregateable{
		Data: []byte{1, 2, 3},
		Kind: KZG,
	}

	var _ AggregateableProof = sa // compile-time check

	if len(sa.ProofBytes()) != 3 {
		t.Errorf("expected 3 bytes, got %d", len(sa.ProofBytes()))
	}
	if sa.ProofKind() != KZG {
		t.Errorf("expected KZG, got %v", sa.ProofKind())
	}
}
