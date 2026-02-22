package light

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// makeLeaf creates a deterministic leaf hash from an index.
func makeLeaf(i int) types.Hash {
	return crypto.Keccak256Hash([]byte{byte(i), byte(i >> 8)})
}

// makeLeaves creates n deterministic leaf hashes.
func makeLeaves(n int) []types.Hash {
	leaves := make([]types.Hash, n)
	for i := range leaves {
		leaves[i] = makeLeaf(i)
	}
	return leaves
}

func TestNewProofVerifier(t *testing.T) {
	pv := NewProofVerifier(DefaultProofVerifierConfig())
	if pv == nil {
		t.Fatal("expected non-nil verifier")
	}
	if pv.config.MaxProofDepth != 64 {
		t.Errorf("expected MaxProofDepth=64, got %d", pv.config.MaxProofDepth)
	}
	if pv.ProofsVerified() != 0 {
		t.Errorf("expected 0 proofs verified, got %d", pv.ProofsVerified())
	}
}

func TestNewProofVerifier_Defaults(t *testing.T) {
	pv := NewProofVerifier(ProofVerifierConfig{})
	if pv.config.MaxProofDepth != 64 {
		t.Errorf("expected default MaxProofDepth=64, got %d", pv.config.MaxProofDepth)
	}
	if pv.config.CacheSize != 256 {
		t.Errorf("expected default CacheSize=256, got %d", pv.config.CacheSize)
	}
}

func TestComputeMerkleRoot(t *testing.T) {
	pv := NewProofVerifier(DefaultProofVerifierConfig())

	t.Run("empty leaves", func(t *testing.T) {
		root := pv.ComputeMerkleRoot(nil)
		if !root.IsZero() {
			t.Error("expected zero root for empty leaves")
		}
	})

	t.Run("single leaf", func(t *testing.T) {
		leaf := makeLeaf(0)
		root := pv.ComputeMerkleRoot([]types.Hash{leaf})
		if root != leaf {
			t.Error("single leaf root should equal the leaf")
		}
	})

	t.Run("two leaves", func(t *testing.T) {
		leaves := makeLeaves(2)
		root := pv.ComputeMerkleRoot(leaves)
		// Manually compute: H(leaf0 || leaf1)
		expected := crypto.Keccak256Hash(leaves[0][:], leaves[1][:])
		if root != expected {
			t.Errorf("root mismatch: got %s, want %s", root.Hex(), expected.Hex())
		}
	})

	t.Run("four leaves", func(t *testing.T) {
		leaves := makeLeaves(4)
		root := pv.ComputeMerkleRoot(leaves)
		if root.IsZero() {
			t.Error("expected non-zero root for four leaves")
		}
		// Compute again and verify determinism.
		root2 := pv.ComputeMerkleRoot(leaves)
		if root != root2 {
			t.Error("merkle root should be deterministic")
		}
	})

	t.Run("non-power-of-two padded", func(t *testing.T) {
		leaves := makeLeaves(3)
		root := pv.ComputeMerkleRoot(leaves)
		if root.IsZero() {
			t.Error("expected non-zero root for three leaves (padded)")
		}
	})
}

func TestCreateAndVerifyMerkleProof(t *testing.T) {
	pv := NewProofVerifier(DefaultProofVerifierConfig())

	t.Run("two leaves index 0", func(t *testing.T) {
		leaves := makeLeaves(2)
		proof, err := pv.CreateMerkleProof(leaves, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if proof.Leaf != leaves[0] {
			t.Error("proof leaf should match leaves[0]")
		}
		valid, err := pv.VerifyMerkleProof(*proof)
		if err != nil {
			t.Fatalf("verification error: %v", err)
		}
		if !valid {
			t.Error("expected valid proof")
		}
	})

	t.Run("two leaves index 1", func(t *testing.T) {
		leaves := makeLeaves(2)
		proof, err := pv.CreateMerkleProof(leaves, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		valid, err := pv.VerifyMerkleProof(*proof)
		if err != nil {
			t.Fatalf("verification error: %v", err)
		}
		if !valid {
			t.Error("expected valid proof")
		}
	})

	t.Run("four leaves all indices", func(t *testing.T) {
		leaves := makeLeaves(4)
		for i := uint64(0); i < 4; i++ {
			proof, err := pv.CreateMerkleProof(leaves, i)
			if err != nil {
				t.Fatalf("index %d: unexpected error: %v", i, err)
			}
			valid, err := pv.VerifyMerkleProof(*proof)
			if err != nil {
				t.Fatalf("index %d: verification error: %v", i, err)
			}
			if !valid {
				t.Errorf("index %d: expected valid proof", i)
			}
		}
	})

	t.Run("eight leaves all indices", func(t *testing.T) {
		leaves := makeLeaves(8)
		for i := uint64(0); i < 8; i++ {
			proof, err := pv.CreateMerkleProof(leaves, i)
			if err != nil {
				t.Fatalf("index %d: unexpected error: %v", i, err)
			}
			valid, err := pv.VerifyMerkleProof(*proof)
			if err != nil {
				t.Fatalf("index %d: verification error: %v", i, err)
			}
			if !valid {
				t.Errorf("index %d: expected valid proof", i)
			}
		}
	})

	t.Run("proof root matches tree root", func(t *testing.T) {
		leaves := makeLeaves(4)
		root := pv.ComputeMerkleRoot(leaves)
		proof, _ := pv.CreateMerkleProof(leaves, 2)
		if proof.Root != root {
			t.Errorf("proof root %s should match tree root %s",
				proof.Root.Hex(), root.Hex())
		}
	})
}

func TestCreateMerkleProof_Errors(t *testing.T) {
	pv := NewProofVerifier(DefaultProofVerifierConfig())

	t.Run("empty leaves", func(t *testing.T) {
		_, err := pv.CreateMerkleProof(nil, 0)
		if err != ErrProofNoLeaves {
			t.Errorf("expected ErrProofNoLeaves, got %v", err)
		}
	})

	t.Run("non-power-of-two", func(t *testing.T) {
		_, err := pv.CreateMerkleProof(makeLeaves(3), 0)
		if err != ErrProofNotPowerOfTwo {
			t.Errorf("expected ErrProofNotPowerOfTwo, got %v", err)
		}
	})

	t.Run("index out of range", func(t *testing.T) {
		_, err := pv.CreateMerkleProof(makeLeaves(4), 4)
		if err != ErrProofIndexOutOfRange {
			t.Errorf("expected ErrProofIndexOutOfRange, got %v", err)
		}
	})

	t.Run("depth exceeded", func(t *testing.T) {
		pv2 := NewProofVerifier(ProofVerifierConfig{MaxProofDepth: 1, CacheSize: 10})
		// 4 leaves = depth 2, but max is 1.
		_, err := pv2.CreateMerkleProof(makeLeaves(4), 0)
		if err != ErrProofDepthExceeded {
			t.Errorf("expected ErrProofDepthExceeded, got %v", err)
		}
	})
}

func TestVerifyMerkleProof_Errors(t *testing.T) {
	pv := NewProofVerifier(DefaultProofVerifierConfig())

	t.Run("zero root", func(t *testing.T) {
		proof := MerkleProof{
			Leaf: makeLeaf(0),
			Path: []types.Hash{makeLeaf(1)},
		}
		_, err := pv.VerifyMerkleProof(proof)
		if err != ErrProofNilRoot {
			t.Errorf("expected ErrProofNilRoot, got %v", err)
		}
	})

	t.Run("zero leaf", func(t *testing.T) {
		proof := MerkleProof{
			Root: makeLeaf(0),
			Path: []types.Hash{makeLeaf(1)},
		}
		_, err := pv.VerifyMerkleProof(proof)
		if err != ErrProofNilLeaf {
			t.Errorf("expected ErrProofNilLeaf, got %v", err)
		}
	})

	t.Run("empty path", func(t *testing.T) {
		proof := MerkleProof{
			Root: makeLeaf(0),
			Leaf: makeLeaf(1),
		}
		_, err := pv.VerifyMerkleProof(proof)
		if err != ErrProofEmptyPath {
			t.Errorf("expected ErrProofEmptyPath, got %v", err)
		}
	})

	t.Run("depth exceeded", func(t *testing.T) {
		pv2 := NewProofVerifier(ProofVerifierConfig{MaxProofDepth: 2, CacheSize: 10})
		proof := MerkleProof{
			Root:  makeLeaf(0),
			Leaf:  makeLeaf(1),
			Path:  makeLeaves(3),
			Index: 0,
		}
		_, err := pv2.VerifyMerkleProof(proof)
		if err != ErrProofDepthExceeded {
			t.Errorf("expected ErrProofDepthExceeded, got %v", err)
		}
	})

	t.Run("invalid proof returns false", func(t *testing.T) {
		proof := MerkleProof{
			Root:  makeLeaf(99),
			Leaf:  makeLeaf(1),
			Path:  []types.Hash{makeLeaf(2)},
			Index: 0,
		}
		valid, err := pv.VerifyMerkleProof(proof)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if valid {
			t.Error("expected invalid proof")
		}
	})
}

func TestVerifyMultiProof(t *testing.T) {
	pv := NewProofVerifier(DefaultProofVerifierConfig())

	t.Run("all valid", func(t *testing.T) {
		leaves := makeLeaves(4)
		proofs := make([]MerkleProof, 4)
		for i := uint64(0); i < 4; i++ {
			p, _ := pv.CreateMerkleProof(leaves, i)
			proofs[i] = *p
		}
		valid, err := pv.VerifyMultiProof(proofs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !valid {
			t.Error("expected all proofs valid")
		}
	})

	t.Run("one invalid fails batch", func(t *testing.T) {
		leaves := makeLeaves(4)
		proofs := make([]MerkleProof, 4)
		for i := uint64(0); i < 4; i++ {
			p, _ := pv.CreateMerkleProof(leaves, i)
			proofs[i] = *p
		}
		// Corrupt one proof's root.
		proofs[2].Root = makeLeaf(99)
		valid, err := pv.VerifyMultiProof(proofs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if valid {
			t.Error("expected batch to fail with invalid proof")
		}
	})

	t.Run("empty batch", func(t *testing.T) {
		_, err := pv.VerifyMultiProof(nil)
		if err != ErrProofEmptyBatch {
			t.Errorf("expected ErrProofEmptyBatch, got %v", err)
		}
	})
}

func TestVerifyBranch(t *testing.T) {
	pv := NewProofVerifier(DefaultProofVerifierConfig())

	t.Run("simple two-leaf tree", func(t *testing.T) {
		leaves := makeLeaves(2)
		root := pv.ComputeMerkleRoot(leaves)

		// Branch for index 0: sibling is leaf[1].
		valid := pv.VerifyBranch(root, leaves[0], []types.Hash{leaves[1]}, 0)
		if !valid {
			t.Error("expected valid branch for index 0")
		}

		// Branch for index 1: sibling is leaf[0].
		valid = pv.VerifyBranch(root, leaves[1], []types.Hash{leaves[0]}, 1)
		if !valid {
			t.Error("expected valid branch for index 1")
		}
	})

	t.Run("wrong root fails", func(t *testing.T) {
		leaves := makeLeaves(2)
		wrongRoot := makeLeaf(99)
		valid := pv.VerifyBranch(wrongRoot, leaves[0], []types.Hash{leaves[1]}, 0)
		if valid {
			t.Error("expected invalid branch with wrong root")
		}
	})
}

func TestProofsVerifiedCounter(t *testing.T) {
	pv := NewProofVerifier(DefaultProofVerifierConfig())
	leaves := makeLeaves(4)

	for i := uint64(0); i < 4; i++ {
		proof, _ := pv.CreateMerkleProof(leaves, i)
		pv.VerifyMerkleProof(*proof)
	}

	if pv.ProofsVerified() != 4 {
		t.Errorf("expected 4 proofs verified, got %d", pv.ProofsVerified())
	}
}

func TestProofVerifier_ConcurrentAccess(t *testing.T) {
	pv := NewProofVerifier(DefaultProofVerifierConfig())
	leaves := makeLeaves(8)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			leafIdx := uint64(idx % 8)
			proof, err := pv.CreateMerkleProof(leaves, leafIdx)
			if err != nil {
				return
			}
			pv.VerifyMerkleProof(*proof)
		}(i)
	}
	wg.Wait()

	if pv.ProofsVerified() == 0 {
		t.Error("expected some proofs to be verified concurrently")
	}
}

func TestProofVerifier_CacheEviction(t *testing.T) {
	pv := NewProofVerifier(ProofVerifierConfig{
		MaxProofDepth: 64,
		CacheSize:     4,
	})
	leaves := makeLeaves(8)

	// Verify 8 proofs to trigger cache eviction (cache size is 4).
	for i := uint64(0); i < 8; i++ {
		proof, err := pv.CreateMerkleProof(leaves, i)
		if err != nil {
			t.Fatalf("unexpected error creating proof %d: %v", i, err)
		}
		valid, err := pv.VerifyMerkleProof(*proof)
		if err != nil {
			t.Fatalf("verification error for proof %d: %v", i, err)
		}
		if !valid {
			t.Errorf("expected valid proof %d", i)
		}
	}

	if pv.ProofsVerified() != 8 {
		t.Errorf("expected 8 proofs verified, got %d", pv.ProofsVerified())
	}
}

func TestIsPowerOfTwo(t *testing.T) {
	tests := []struct {
		n    uint64
		want bool
	}{
		{0, false},
		{1, true},
		{2, true},
		{3, false},
		{4, true},
		{5, false},
		{8, true},
		{16, true},
		{17, false},
		{256, true},
		{1024, true},
	}
	for _, tt := range tests {
		got := isPowerOfTwo(tt.n)
		if got != tt.want {
			t.Errorf("isPowerOfTwo(%d) = %v, want %v", tt.n, got, tt.want)
		}
	}
}

func TestLog2(t *testing.T) {
	tests := []struct {
		n    uint64
		want int
	}{
		{1, 0},
		{2, 1},
		{4, 2},
		{8, 3},
		{16, 4},
		{256, 8},
	}
	for _, tt := range tests {
		got := log2(tt.n)
		if got != tt.want {
			t.Errorf("log2(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}

func TestPadToPowerOfTwo(t *testing.T) {
	t.Run("already power of two", func(t *testing.T) {
		leaves := makeLeaves(4)
		padded := padToPowerOfTwo(leaves)
		if len(padded) != 4 {
			t.Errorf("expected len 4, got %d", len(padded))
		}
	})

	t.Run("pad 3 to 4", func(t *testing.T) {
		leaves := makeLeaves(3)
		padded := padToPowerOfTwo(leaves)
		if len(padded) != 4 {
			t.Errorf("expected len 4, got %d", len(padded))
		}
		// Fourth element should be zero hash.
		if !padded[3].IsZero() {
			t.Error("padded element should be zero hash")
		}
	})

	t.Run("pad 5 to 8", func(t *testing.T) {
		leaves := makeLeaves(5)
		padded := padToPowerOfTwo(leaves)
		if len(padded) != 8 {
			t.Errorf("expected len 8, got %d", len(padded))
		}
	})
}
