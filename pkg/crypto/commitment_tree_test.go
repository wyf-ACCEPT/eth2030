package crypto

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestCommitTree_NewTreeEmpty(t *testing.T) {
	ct := NewCommitmentTree()
	if ct.Size() != 0 {
		t.Fatalf("expected size 0, got %d", ct.Size())
	}
	root := ct.Root()
	if root.IsZero() {
		t.Fatal("empty tree should have non-zero default root")
	}
}

func TestCommitTree_AppendSingle(t *testing.T) {
	ct := NewCommitmentTree()
	c := types.HexToHash("0xaabb")

	idx, root, err := ct.Append(c)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if idx != 0 {
		t.Fatalf("expected index 0, got %d", idx)
	}
	if root.IsZero() {
		t.Fatal("root should be non-zero after append")
	}
	if ct.Size() != 1 {
		t.Fatalf("expected size 1, got %d", ct.Size())
	}
}

func TestCommitTree_AppendChangesRoot(t *testing.T) {
	ct := NewCommitmentTree()
	root0 := ct.Root()

	c := types.HexToHash("0xccdd")
	_, root1, _ := ct.Append(c)

	if root0 == root1 {
		t.Fatal("root should change after append")
	}
}

func TestCommitTree_AppendMultiple(t *testing.T) {
	ct := NewCommitmentTree()
	commitments := []types.Hash{
		types.HexToHash("0x1111"),
		types.HexToHash("0x2222"),
		types.HexToHash("0x3333"),
	}

	for i, c := range commitments {
		idx, _, err := ct.Append(c)
		if err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
		if idx != uint64(i) {
			t.Fatalf("expected index %d, got %d", i, idx)
		}
	}
	if ct.Size() != 3 {
		t.Fatalf("expected size 3, got %d", ct.Size())
	}
}

func TestCommitTree_AppendDifferentCommitmentsProduceDifferentRoots(t *testing.T) {
	ct1 := NewCommitmentTree()
	ct2 := NewCommitmentTree()

	ct1.Append(types.HexToHash("0xaaaa"))
	ct2.Append(types.HexToHash("0xbbbb"))

	if ct1.Root() == ct2.Root() {
		t.Fatal("different commitments should produce different roots")
	}
}

func TestCommitTree_MerkleProofSingle(t *testing.T) {
	ct := NewCommitmentTree()
	c := types.HexToHash("0xeeff")
	ct.Append(c)

	proof, err := ct.MerkleProof(0)
	if err != nil {
		t.Fatalf("MerkleProof failed: %v", err)
	}
	if proof.Index != 0 {
		t.Fatalf("expected index 0, got %d", proof.Index)
	}
}

func TestCommitTree_MerkleProofVerify(t *testing.T) {
	ct := NewCommitmentTree()
	c := types.HexToHash("0x4455")
	ct.Append(c)

	proof, err := ct.MerkleProof(0)
	if err != nil {
		t.Fatalf("MerkleProof failed: %v", err)
	}

	root := ct.Root()
	if !VerifyCommitmentProof(c, proof, root) {
		t.Fatal("valid proof should verify")
	}
}

func TestCommitTree_MerkleProofMultiple(t *testing.T) {
	ct := NewCommitmentTree()
	commitments := []types.Hash{
		types.HexToHash("0xaa01"),
		types.HexToHash("0xaa02"),
		types.HexToHash("0xaa03"),
		types.HexToHash("0xaa04"),
	}

	for _, c := range commitments {
		ct.Append(c)
	}

	root := ct.Root()
	for i, c := range commitments {
		proof, err := ct.MerkleProof(uint64(i))
		if err != nil {
			t.Fatalf("MerkleProof(%d) failed: %v", i, err)
		}
		if !VerifyCommitmentProof(c, proof, root) {
			t.Fatalf("proof for index %d failed verification", i)
		}
	}
}

func TestCommitTree_MerkleProofRejectsWrongCommitment(t *testing.T) {
	ct := NewCommitmentTree()
	c := types.HexToHash("0xbb01")
	ct.Append(c)

	proof, _ := ct.MerkleProof(0)
	root := ct.Root()

	wrong := types.HexToHash("0xbb02")
	if VerifyCommitmentProof(wrong, proof, root) {
		t.Fatal("wrong commitment should fail verification")
	}
}

func TestCommitTree_MerkleProofRejectsWrongRoot(t *testing.T) {
	ct := NewCommitmentTree()
	c := types.HexToHash("0xcc01")
	ct.Append(c)

	proof, _ := ct.MerkleProof(0)
	wrongRoot := types.HexToHash("0xdeadbeef")

	if VerifyCommitmentProof(c, proof, wrongRoot) {
		t.Fatal("proof against wrong root should fail")
	}
}

func TestCommitTree_MerkleProofRejectsNil(t *testing.T) {
	c := types.HexToHash("0xdd01")
	root := types.HexToHash("0xdd02")
	if VerifyCommitmentProof(c, nil, root) {
		t.Fatal("nil proof should be rejected")
	}
}

func TestCommitTree_MerkleProofOutOfRange(t *testing.T) {
	ct := NewCommitmentTree()
	ct.Append(types.HexToHash("0xee01"))

	_, err := ct.MerkleProof(1) // only index 0 exists
	if err != ErrCommitTreeBadIndex {
		t.Fatalf("expected ErrCommitTreeBadIndex, got %v", err)
	}
}

func TestCommitTree_MerkleProofEmptyTree(t *testing.T) {
	ct := NewCommitmentTree()
	_, err := ct.MerkleProof(0)
	if err != ErrCommitTreeBadIndex {
		t.Fatalf("expected ErrCommitTreeBadIndex, got %v", err)
	}
}

func TestCommitTree_BatchAppend(t *testing.T) {
	ct := NewCommitmentTree()
	commitments := []types.Hash{
		types.HexToHash("0xff01"),
		types.HexToHash("0xff02"),
		types.HexToHash("0xff03"),
	}

	startIdx, root, err := ct.BatchAppend(commitments)
	if err != nil {
		t.Fatalf("BatchAppend failed: %v", err)
	}
	if startIdx != 0 {
		t.Fatalf("expected start index 0, got %d", startIdx)
	}
	if root.IsZero() {
		t.Fatal("root should be non-zero")
	}
	if ct.Size() != 3 {
		t.Fatalf("expected size 3, got %d", ct.Size())
	}
}

func TestCommitTree_BatchAppendAfterSingle(t *testing.T) {
	ct := NewCommitmentTree()
	ct.Append(types.HexToHash("0x0001"))

	batch := []types.Hash{
		types.HexToHash("0x0002"),
		types.HexToHash("0x0003"),
	}

	startIdx, _, err := ct.BatchAppend(batch)
	if err != nil {
		t.Fatalf("BatchAppend failed: %v", err)
	}
	if startIdx != 1 {
		t.Fatalf("expected start index 1, got %d", startIdx)
	}
	if ct.Size() != 3 {
		t.Fatalf("expected size 3, got %d", ct.Size())
	}
}

func TestCommitTree_BatchAppendProofs(t *testing.T) {
	ct := NewCommitmentTree()
	commitments := []types.Hash{
		types.HexToHash("0xba01"),
		types.HexToHash("0xba02"),
		types.HexToHash("0xba03"),
		types.HexToHash("0xba04"),
		types.HexToHash("0xba05"),
	}
	ct.BatchAppend(commitments)

	root := ct.Root()
	for i, c := range commitments {
		proof, err := ct.MerkleProof(uint64(i))
		if err != nil {
			t.Fatalf("MerkleProof(%d) failed: %v", i, err)
		}
		if !VerifyCommitmentProof(c, proof, root) {
			t.Fatalf("proof for batch-appended index %d failed", i)
		}
	}
}

func TestCommitTree_LargerTree(t *testing.T) {
	ct := NewCommitmentTree()
	n := 64
	commitments := make([]types.Hash, n)
	for i := 0; i < n; i++ {
		var c types.Hash
		c[0] = byte(i)
		c[1] = byte(i >> 8)
		commitments[i] = c
		ct.Append(c)
	}

	root := ct.Root()
	// Verify a sampling of proofs.
	for _, idx := range []int{0, 1, n / 2, n - 1} {
		proof, err := ct.MerkleProof(uint64(idx))
		if err != nil {
			t.Fatalf("MerkleProof(%d) failed: %v", idx, err)
		}
		if !VerifyCommitmentProof(commitments[idx], proof, root) {
			t.Fatalf("proof for index %d failed in larger tree", idx)
		}
	}
}
