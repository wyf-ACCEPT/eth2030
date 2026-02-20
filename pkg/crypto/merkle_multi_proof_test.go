package crypto

import (
	"testing"
)

func TestGeneralizedIndex(t *testing.T) {
	// depth=3: leaves at [8..15], leaf 0 -> GI 8, leaf 7 -> GI 15.
	if gi := GeneralizedIndex(3, 0); gi != 8 {
		t.Fatalf("expected 8, got %d", gi)
	}
	if gi := GeneralizedIndex(3, 7); gi != 15 {
		t.Fatalf("expected 15, got %d", gi)
	}
	// depth=1: leaves at [2, 3].
	if gi := GeneralizedIndex(1, 0); gi != 2 {
		t.Fatalf("expected 2, got %d", gi)
	}
	if gi := GeneralizedIndex(1, 1); gi != 3 {
		t.Fatalf("expected 3, got %d", gi)
	}
}

func TestParent(t *testing.T) {
	if p := Parent(8); p != 4 {
		t.Fatalf("expected 4, got %d", p)
	}
	if p := Parent(9); p != 4 {
		t.Fatalf("expected 4, got %d", p)
	}
	if p := Parent(2); p != 1 {
		t.Fatalf("expected 1, got %d", p)
	}
}

func TestSibling(t *testing.T) {
	if s := Sibling(8); s != 9 {
		t.Fatalf("expected 9, got %d", s)
	}
	if s := Sibling(9); s != 8 {
		t.Fatalf("expected 8, got %d", s)
	}
	if s := Sibling(2); s != 3 {
		t.Fatalf("expected 3, got %d", s)
	}
	if s := Sibling(3); s != 2 {
		t.Fatalf("expected 2, got %d", s)
	}
}

func TestIsLeft(t *testing.T) {
	if !IsLeft(8) {
		t.Fatal("8 should be left")
	}
	if IsLeft(9) {
		t.Fatal("9 should be right")
	}
	if !IsLeft(2) {
		t.Fatal("2 should be left")
	}
	if IsLeft(3) {
		t.Fatal("3 should be right")
	}
}

func TestDepthOfGI(t *testing.T) {
	if d := DepthOfGI(1); d != 0 {
		t.Fatalf("root depth: expected 0, got %d", d)
	}
	if d := DepthOfGI(2); d != 1 {
		t.Fatalf("depth of 2: expected 1, got %d", d)
	}
	if d := DepthOfGI(8); d != 3 {
		t.Fatalf("depth of 8: expected 3, got %d", d)
	}
	if d := DepthOfGI(15); d != 3 {
		t.Fatalf("depth of 15: expected 3, got %d", d)
	}
	if d := DepthOfGI(0); d != 0 {
		t.Fatalf("depth of 0: expected 0, got %d", d)
	}
}

func TestPathToRoot(t *testing.T) {
	// From GI 8 (depth 3): path = [4, 2, 1].
	path := PathToRoot(8)
	expected := []uint64{4, 2, 1}
	if len(path) != len(expected) {
		t.Fatalf("expected %d elements, got %d", len(expected), len(path))
	}
	for i, gi := range expected {
		if path[i] != gi {
			t.Fatalf("path[%d]: expected %d, got %d", i, gi, path[i])
		}
	}

	// Root has empty path.
	path = PathToRoot(1)
	if len(path) != 0 {
		t.Fatalf("expected empty path for root, got %d", len(path))
	}
}

func makeLeafHash(b byte) [32]byte {
	var h [32]byte
	h[0] = b
	return h
}

func TestBuildMerkleTree(t *testing.T) {
	leaves := [][32]byte{
		makeLeafHash(1),
		makeLeafHash(2),
		makeLeafHash(3),
		makeLeafHash(4),
	}

	tree, depth := BuildMerkleTree(leaves)
	if depth != 2 {
		t.Fatalf("expected depth 2, got %d", depth)
	}

	// Check leaves are at positions 4..7.
	for i, leaf := range leaves {
		if tree[4+i] != leaf {
			t.Fatalf("leaf %d mismatch", i)
		}
	}

	// Internal nodes should be non-zero.
	for i := 1; i <= 3; i++ {
		allZero := true
		for _, b := range tree[i] {
			if b != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Fatalf("internal node %d is all zeros", i)
		}
	}
}

func TestBuildMerkleTreeNonPowerOfTwo(t *testing.T) {
	// 3 leaves should get padded to 4.
	leaves := [][32]byte{
		makeLeafHash(10),
		makeLeafHash(20),
		makeLeafHash(30),
	}

	tree, depth := BuildMerkleTree(leaves)
	if depth != 2 {
		t.Fatalf("expected depth 2, got %d", depth)
	}

	// Leaf 4 (index 7) should be zeroed.
	var zero [32]byte
	if tree[7] != zero {
		t.Fatal("padding leaf should be zero")
	}

	// Root should be non-zero.
	allZero := true
	for _, b := range tree[1] {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("root should be non-zero")
	}
}

func TestMerkleRoot(t *testing.T) {
	leaves := [][32]byte{
		makeLeafHash(1),
		makeLeafHash(2),
	}
	root := MerkleRoot(leaves)

	// Root should be non-zero.
	var zero [32]byte
	if root == zero {
		t.Fatal("root should not be zero")
	}

	// Same leaves should produce same root (deterministic).
	root2 := MerkleRoot(leaves)
	if root != root2 {
		t.Fatal("non-deterministic root")
	}

	// Different leaves should produce different root.
	leaves2 := [][32]byte{
		makeLeafHash(3),
		makeLeafHash(4),
	}
	root3 := MerkleRoot(leaves2)
	if root == root3 {
		t.Fatal("different leaves produced same root")
	}
}

func TestMerkleRootEmpty(t *testing.T) {
	// Empty leaves should produce a valid (zero-leaf-based) root.
	root := MerkleRoot(nil)
	var zero [32]byte
	// The root is hash of two zero children, so non-zero.
	if root == zero {
		t.Fatal("root of empty tree should not be the zero hash")
	}
}

func TestGenerateMultiProofSingleLeaf(t *testing.T) {
	leaves := [][32]byte{
		makeLeafHash(0xAA),
		makeLeafHash(0xBB),
		makeLeafHash(0xCC),
		makeLeafHash(0xDD),
	}
	tree, depth := BuildMerkleTree(leaves)
	root := tree[1]

	// Prove leaf 0.
	proof, err := GenerateMultiProof(tree, depth, []uint64{0})
	if err != nil {
		t.Fatal(err)
	}

	if len(proof.Leaves) != 1 {
		t.Fatalf("expected 1 leaf, got %d", len(proof.Leaves))
	}

	// Verify.
	if !VerifyMultiProof(root, proof) {
		t.Fatal("valid proof failed verification")
	}
}

func TestGenerateMultiProofMultipleLeaves(t *testing.T) {
	leaves := [][32]byte{
		makeLeafHash(1),
		makeLeafHash(2),
		makeLeafHash(3),
		makeLeafHash(4),
	}
	tree, depth := BuildMerkleTree(leaves)
	root := tree[1]

	// Prove leaves 0 and 2.
	proof, err := GenerateMultiProof(tree, depth, []uint64{0, 2})
	if err != nil {
		t.Fatal(err)
	}

	if len(proof.Leaves) != 2 {
		t.Fatalf("expected 2 leaves, got %d", len(proof.Leaves))
	}

	if !VerifyMultiProof(root, proof) {
		t.Fatal("valid multi-proof failed verification")
	}
}

func TestGenerateMultiProofAllLeaves(t *testing.T) {
	leaves := [][32]byte{
		makeLeafHash(10),
		makeLeafHash(20),
		makeLeafHash(30),
		makeLeafHash(40),
	}
	tree, depth := BuildMerkleTree(leaves)
	root := tree[1]

	// Prove all leaves.
	proof, err := GenerateMultiProof(tree, depth, []uint64{0, 1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}

	if len(proof.Leaves) != 4 {
		t.Fatalf("expected 4 leaves, got %d", len(proof.Leaves))
	}

	// With all leaves the proof should be very small (at most 1 internal node
	// due to bottom-up walk ordering in the proof generator).
	if len(proof.Proof) > 1 {
		t.Fatalf("expected at most 1 proof node for all leaves, got %d", len(proof.Proof))
	}

	if !VerifyMultiProof(root, proof) {
		t.Fatal("valid all-leaf proof failed verification")
	}
}

func TestGenerateMultiProofAdjacentLeaves(t *testing.T) {
	leaves := make([][32]byte, 8)
	for i := range leaves {
		leaves[i] = makeLeafHash(byte(i + 1))
	}
	tree, depth := BuildMerkleTree(leaves)
	root := tree[1]

	// Prove adjacent leaves 2 and 3 (siblings).
	proof, err := GenerateMultiProof(tree, depth, []uint64{2, 3})
	if err != nil {
		t.Fatal(err)
	}

	if !VerifyMultiProof(root, proof) {
		t.Fatal("adjacent leaf proof failed verification")
	}

	// Siblings should share their parent, so fewer proof nodes needed.
	// For a depth-3 tree with 2 sibling leaves: need 2 proof nodes (sibling-pair and uncle).
	if len(proof.Proof) > 2 {
		t.Fatalf("expected at most 2 proof nodes for siblings, got %d", len(proof.Proof))
	}
}

func TestVerifyMultiProofWrongRoot(t *testing.T) {
	leaves := [][32]byte{
		makeLeafHash(1),
		makeLeafHash(2),
		makeLeafHash(3),
		makeLeafHash(4),
	}
	tree, depth := BuildMerkleTree(leaves)

	proof, err := GenerateMultiProof(tree, depth, []uint64{0})
	if err != nil {
		t.Fatal(err)
	}

	// Verify against wrong root.
	wrongRoot := makeLeafHash(0xFF)
	if VerifyMultiProof(wrongRoot, proof) {
		t.Fatal("proof verified against wrong root")
	}
}

func TestVerifyMultiProofTamperedLeaf(t *testing.T) {
	leaves := [][32]byte{
		makeLeafHash(1),
		makeLeafHash(2),
		makeLeafHash(3),
		makeLeafHash(4),
	}
	tree, depth := BuildMerkleTree(leaves)
	root := tree[1]

	proof, err := GenerateMultiProof(tree, depth, []uint64{0})
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with the leaf.
	proof.Leaves[0].Hash[0] ^= 0xFF
	if VerifyMultiProof(root, proof) {
		t.Fatal("tampered proof should not verify")
	}
}

func TestVerifyMultiProofTamperedNode(t *testing.T) {
	leaves := [][32]byte{
		makeLeafHash(1),
		makeLeafHash(2),
		makeLeafHash(3),
		makeLeafHash(4),
	}
	tree, depth := BuildMerkleTree(leaves)
	root := tree[1]

	proof, err := GenerateMultiProof(tree, depth, []uint64{0})
	if err != nil {
		t.Fatal(err)
	}

	if len(proof.Proof) > 0 {
		proof.Proof[0].Hash[0] ^= 0xFF
		if VerifyMultiProof(root, proof) {
			t.Fatal("tampered proof node should not verify")
		}
	}
}

func TestVerifyMultiProofNil(t *testing.T) {
	var root [32]byte
	if VerifyMultiProof(root, nil) {
		t.Fatal("nil proof should not verify")
	}
}

func TestVerifyMultiProofEmptyLeaves(t *testing.T) {
	var root [32]byte
	proof := &MerkleMultiProof{}
	if VerifyMultiProof(root, proof) {
		t.Fatal("empty proof should not verify")
	}
}

func TestCompactMultiProof(t *testing.T) {
	leaves := make([][32]byte, 8)
	for i := range leaves {
		leaves[i] = makeLeafHash(byte(i + 1))
	}
	tree, depth := BuildMerkleTree(leaves)
	root := tree[1]

	proof, err := GenerateMultiProof(tree, depth, []uint64{0, 1})
	if err != nil {
		t.Fatal(err)
	}

	compacted := CompactMultiProof(proof)
	// Compacted should still verify.
	if !VerifyMultiProof(root, compacted) {
		t.Fatal("compacted proof failed verification")
	}

	// Compacted should have <= same number of proof nodes.
	if len(compacted.Proof) > len(proof.Proof) {
		t.Fatal("compaction increased proof size")
	}
}

func TestCompactMultiProofNil(t *testing.T) {
	result := CompactMultiProof(nil)
	if result != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestCompactMultiProofSingleLeaf(t *testing.T) {
	leaves := [][32]byte{makeLeafHash(1), makeLeafHash(2)}
	tree, depth := BuildMerkleTree(leaves)

	proof, err := GenerateMultiProof(tree, depth, []uint64{0})
	if err != nil {
		t.Fatal(err)
	}

	compacted := CompactMultiProof(proof)
	// Single leaf: no compaction possible.
	if len(compacted.Proof) != len(proof.Proof) {
		t.Fatal("single leaf proof should not change after compaction")
	}
}

func TestGenerateMultiProofLargeTree(t *testing.T) {
	// 16-leaf tree.
	leaves := make([][32]byte, 16)
	for i := range leaves {
		leaves[i] = makeLeafHash(byte(i))
	}
	tree, depth := BuildMerkleTree(leaves)
	root := tree[1]

	// Prove leaves 0, 5, 10, 15.
	proof, err := GenerateMultiProof(tree, depth, []uint64{0, 5, 10, 15})
	if err != nil {
		t.Fatal(err)
	}

	if !VerifyMultiProof(root, proof) {
		t.Fatal("large tree proof failed verification")
	}
}

func TestGenerateMultiProofDedup(t *testing.T) {
	leaves := [][32]byte{
		makeLeafHash(1),
		makeLeafHash(2),
		makeLeafHash(3),
		makeLeafHash(4),
	}
	tree, depth := BuildMerkleTree(leaves)
	root := tree[1]

	// Duplicate leaf index.
	proof, err := GenerateMultiProof(tree, depth, []uint64{0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}

	// Should have 2 unique leaves (0 and 1), not 3.
	if len(proof.Leaves) != 2 {
		t.Fatalf("expected 2 unique leaves, got %d", len(proof.Leaves))
	}

	if !VerifyMultiProof(root, proof) {
		t.Fatal("deduped proof failed verification")
	}
}

func TestGenerateMultiProofNoLeaves(t *testing.T) {
	leaves := [][32]byte{makeLeafHash(1), makeLeafHash(2)}
	tree, depth := BuildMerkleTree(leaves)

	_, err := GenerateMultiProof(tree, depth, []uint64{})
	if err == nil {
		t.Fatal("expected error for empty leaf indices")
	}
}

func TestGenerateMultiProofOutOfRange(t *testing.T) {
	leaves := [][32]byte{makeLeafHash(1), makeLeafHash(2)}
	tree, depth := BuildMerkleTree(leaves)

	_, err := GenerateMultiProof(tree, depth, []uint64{99})
	if err == nil {
		t.Fatal("expected error for out-of-range leaf index")
	}
}

func TestMerkleHashPairDeterministic(t *testing.T) {
	a := makeLeafHash(0xAA)
	b := makeLeafHash(0xBB)

	h1 := merkleHashPair(a, b)
	h2 := merkleHashPair(a, b)
	if h1 != h2 {
		t.Fatal("merkleHashPair is non-deterministic")
	}

	// Order matters.
	h3 := merkleHashPair(b, a)
	if h1 == h3 {
		t.Fatal("merkleHashPair should be order-dependent")
	}
}

func TestProofSize(t *testing.T) {
	if s := ProofSize(3, 0); s != 0 {
		t.Fatalf("expected 0, got %d", s)
	}
	if s := ProofSize(3, 1); s != 3 {
		t.Fatalf("expected 3, got %d", s)
	}
	if s := ProofSize(4, 2); s != 8 {
		t.Fatalf("expected 8, got %d", s)
	}
}

func TestMerkleRootDuplicateLeaves(t *testing.T) {
	leaf := makeLeafHash(0x42)
	leaves := [][32]byte{leaf, leaf, leaf, leaf}
	root := MerkleRoot(leaves)

	// All leaves same -> unique root.
	var zero [32]byte
	if root == zero {
		t.Fatal("root of duplicate leaves should not be zero")
	}

	// Single different leaf should change root.
	leaves2 := [][32]byte{leaf, leaf, leaf, makeLeafHash(0x43)}
	root2 := MerkleRoot(leaves2)
	if root == root2 {
		t.Fatal("different leaves should produce different root")
	}
}

func TestBuildMerkleTreeSingleLeaf(t *testing.T) {
	leaves := [][32]byte{makeLeafHash(0x01)}
	tree, depth := BuildMerkleTree(leaves)

	if depth != 1 {
		t.Fatalf("expected depth 1, got %d", depth)
	}

	// tree[2] should be our leaf, tree[3] should be zero.
	if tree[2] != makeLeafHash(0x01) {
		t.Fatal("leaf not placed correctly")
	}
	var zero [32]byte
	if tree[3] != zero {
		t.Fatal("padding should be zero")
	}
}
