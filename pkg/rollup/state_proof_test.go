package rollup

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func TestGenerateAndVerifyStateTransition(t *testing.T) {
	preState := crypto.Keccak256Hash([]byte("pre-state"))
	txs := [][]byte{
		[]byte("tx1-transfer-100-eth"),
		[]byte("tx2-deploy-contract"),
	}

	// Generate a proof.
	proof := GenerateStateTransitionProof(1, preState, types.Hash{}, txs, 42)

	// The generated proof's post-state should be the deterministic derivation.
	if proof.PreStateRoot != preState {
		t.Fatalf("pre-state mismatch")
	}
	if proof.RollupID != 1 {
		t.Fatalf("rollup ID mismatch")
	}
	if proof.BlockNumber != 42 {
		t.Fatalf("block number mismatch")
	}
	if len(proof.Witness) == 0 {
		t.Fatal("witness should not be empty")
	}
	if len(proof.Transactions) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(proof.Transactions))
	}

	// Compute the expected post-state root.
	postState := computeTransitionRoot(preState, txs, proof.Witness)
	proof.PostStateRoot = postState

	// Verify the proof.
	valid, err := VerifyStateTransition(proof)
	if err != nil {
		t.Fatalf("verification error: %v", err)
	}
	if !valid {
		t.Fatal("proof should be valid")
	}
}

func TestVerifyStateTransition_Errors(t *testing.T) {
	tests := []struct {
		name  string
		proof StateTransitionProof
		err   error
	}{
		{
			name:  "zero pre-state",
			proof: StateTransitionProof{PostStateRoot: types.BytesToHash([]byte{1}), Transactions: [][]byte{{1}}, Witness: []byte{1}},
			err:   ErrProofPreStateZero,
		},
		{
			name:  "zero post-state",
			proof: StateTransitionProof{PreStateRoot: types.BytesToHash([]byte{1}), Transactions: [][]byte{{1}}, Witness: []byte{1}},
			err:   ErrProofPostStateZero,
		},
		{
			name:  "empty transactions",
			proof: StateTransitionProof{PreStateRoot: types.BytesToHash([]byte{1}), PostStateRoot: types.BytesToHash([]byte{2}), Witness: []byte{1}},
			err:   ErrProofEmptyTransactions,
		},
		{
			name:  "nil witness",
			proof: StateTransitionProof{PreStateRoot: types.BytesToHash([]byte{1}), PostStateRoot: types.BytesToHash([]byte{2}), Transactions: [][]byte{{1}}},
			err:   ErrProofNilWitness,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := VerifyStateTransition(tt.proof)
			if err == nil {
				t.Fatal("expected error")
			}
			if err != tt.err {
				t.Fatalf("expected %v, got %v", tt.err, err)
			}
		})
	}
}

func TestVerifyStateTransition_MismatchedPostState(t *testing.T) {
	preState := crypto.Keccak256Hash([]byte("pre"))
	txs := [][]byte{[]byte("tx")}

	proof := GenerateStateTransitionProof(1, preState, types.Hash{}, txs, 1)
	// Set an incorrect post-state root.
	proof.PostStateRoot = crypto.Keccak256Hash([]byte("wrong-post-state"))

	valid, err := VerifyStateTransition(proof)
	if err == nil {
		t.Fatal("expected error for mismatched post-state")
	}
	if valid {
		t.Fatal("proof should be invalid")
	}
}

func TestVerifyMerkleInclusion(t *testing.T) {
	key := []byte("account-0x1234")
	value := []byte("balance-1000")

	// Build a simple merkle tree with 2 leaves.
	leaf0 := crypto.Keccak256Hash(key, value)
	leaf1 := crypto.Keccak256Hash([]byte("other-key"), []byte("other-val"))

	// Compute root with canonical ordering.
	var root types.Hash
	if hashLessBytes(leaf0[:], leaf1[:]) {
		root = crypto.Keccak256Hash(leaf0[:], leaf1[:])
	} else {
		root = crypto.Keccak256Hash(leaf1[:], leaf0[:])
	}

	// Proof for leaf0 is just leaf1.
	proof := [][]byte{leaf1[:]}
	valid := VerifyMerkleInclusion([32]byte(root), key, value, proof)
	if !valid {
		t.Fatal("merkle inclusion should be valid")
	}
}

func TestVerifyMerkleInclusion_InvalidCases(t *testing.T) {
	key := []byte("key")
	value := []byte("value")

	t.Run("zero root", func(t *testing.T) {
		if VerifyMerkleInclusion([32]byte{}, key, value, [][]byte{{1}}) {
			t.Fatal("should reject zero root")
		}
	})

	t.Run("empty key", func(t *testing.T) {
		root := [32]byte{1}
		if VerifyMerkleInclusion(root, nil, value, [][]byte{{1}}) {
			t.Fatal("should reject empty key")
		}
	})

	t.Run("empty value", func(t *testing.T) {
		root := [32]byte{1}
		if VerifyMerkleInclusion(root, key, nil, [][]byte{{1}}) {
			t.Fatal("should reject empty value")
		}
	})

	t.Run("empty proof", func(t *testing.T) {
		root := [32]byte{1}
		if VerifyMerkleInclusion(root, key, value, nil) {
			t.Fatal("should reject empty proof")
		}
	})

	t.Run("invalid sibling length", func(t *testing.T) {
		root := [32]byte{1}
		if VerifyMerkleInclusion(root, key, value, [][]byte{{1, 2, 3}}) {
			t.Fatal("should reject invalid sibling length")
		}
	})
}

func TestBatchProofAggregator(t *testing.T) {
	preState := crypto.Keccak256Hash([]byte("genesis"))
	agg := NewBatchProofAggregator(1, 16)

	if agg.Count() != 0 {
		t.Fatal("aggregator should start empty")
	}

	// Build a chain of proofs.
	currentState := preState
	for i := 0; i < 3; i++ {
		txs := [][]byte{[]byte{byte(i)}}
		proof := GenerateStateTransitionProof(1, currentState, types.Hash{}, txs, uint64(i+1))
		proof.PostStateRoot = computeTransitionRoot(currentState, txs, proof.Witness)
		currentState = proof.PostStateRoot

		if err := agg.Add(proof); err != nil {
			t.Fatalf("add proof %d: %v", i, err)
		}
	}

	if agg.Count() != 3 {
		t.Fatalf("expected 3 proofs, got %d", agg.Count())
	}

	result, err := agg.Aggregate()
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}

	if result.ProofCount != 3 {
		t.Fatalf("expected 3 proofs in result, got %d", result.ProofCount)
	}
	if result.RollupID != 1 {
		t.Fatalf("expected rollup ID 1, got %d", result.RollupID)
	}
	if result.FirstBlock != 1 {
		t.Fatalf("expected first block 1, got %d", result.FirstBlock)
	}
	if result.LastBlock != 3 {
		t.Fatalf("expected last block 3, got %d", result.LastBlock)
	}
	if result.CommitmentRoot.IsZero() {
		t.Fatal("commitment root should not be zero")
	}
	if len(result.CompressedData) == 0 {
		t.Fatal("compressed data should not be empty")
	}
}

func TestBatchProofAggregator_Empty(t *testing.T) {
	agg := NewBatchProofAggregator(1, 16)
	_, err := agg.Aggregate()
	if err != ErrBatchAggEmpty {
		t.Fatalf("expected ErrBatchAggEmpty, got %v", err)
	}
}

func TestBatchProofAggregator_MaxSize(t *testing.T) {
	agg := NewBatchProofAggregator(1, 2)
	preState := crypto.Keccak256Hash([]byte("s1"))

	for i := 0; i < 2; i++ {
		txs := [][]byte{[]byte{byte(i)}}
		proof := GenerateStateTransitionProof(1, preState, types.Hash{}, txs, uint64(i))
		proof.PostStateRoot = computeTransitionRoot(preState, txs, proof.Witness)
		preState = proof.PostStateRoot

		if err := agg.Add(proof); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}

	// Third add should fail.
	proof := GenerateStateTransitionProof(1, preState, types.Hash{}, [][]byte{{0}}, 3)
	err := agg.Add(proof)
	if err != ErrBatchAggTooLarge {
		t.Fatalf("expected ErrBatchAggTooLarge, got %v", err)
	}
}

func TestBatchProofAggregator_ChainBreak(t *testing.T) {
	agg := NewBatchProofAggregator(1, 16)

	// Create two proofs that don't chain.
	proof1 := GenerateStateTransitionProof(1, crypto.Keccak256Hash([]byte("a")), types.Hash{}, [][]byte{{1}}, 1)
	proof1.PostStateRoot = crypto.Keccak256Hash([]byte("post-a"))

	proof2 := GenerateStateTransitionProof(1, crypto.Keccak256Hash([]byte("b")), types.Hash{}, [][]byte{{2}}, 2)
	proof2.PostStateRoot = crypto.Keccak256Hash([]byte("post-b"))

	_ = agg.Add(proof1)
	_ = agg.Add(proof2)

	_, err := agg.Aggregate()
	if err != ErrProofMismatch {
		t.Fatalf("expected ErrProofMismatch for chain break, got %v", err)
	}
}

func TestVerifyAggregatedProof(t *testing.T) {
	preState := crypto.Keccak256Hash([]byte("genesis"))
	agg := NewBatchProofAggregator(1, 16)

	currentState := preState
	for i := 0; i < 2; i++ {
		txs := [][]byte{[]byte{byte(i)}}
		proof := GenerateStateTransitionProof(1, currentState, types.Hash{}, txs, uint64(i+1))
		proof.PostStateRoot = computeTransitionRoot(currentState, txs, proof.Witness)
		currentState = proof.PostStateRoot
		_ = agg.Add(proof)
	}

	result, err := agg.Aggregate()
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}

	valid, err := VerifyAggregatedProof(result, preState, currentState)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !valid {
		t.Fatal("aggregated proof should be valid")
	}
}

func TestVerifyAggregatedProof_WrongStates(t *testing.T) {
	preState := crypto.Keccak256Hash([]byte("genesis"))
	agg := NewBatchProofAggregator(1, 16)

	txs := [][]byte{[]byte{0}}
	proof := GenerateStateTransitionProof(1, preState, types.Hash{}, txs, 1)
	proof.PostStateRoot = computeTransitionRoot(preState, txs, proof.Witness)
	_ = agg.Add(proof)

	result, _ := agg.Aggregate()

	// Pass wrong expected post-state.
	valid, err := VerifyAggregatedProof(result, preState, crypto.Keccak256Hash([]byte("wrong")))
	if err == nil {
		t.Fatal("expected error")
	}
	if valid {
		t.Fatal("should not be valid with wrong post-state")
	}
}

func TestCompressProof(t *testing.T) {
	preState := crypto.Keccak256Hash([]byte("pre"))
	txs := [][]byte{[]byte("tx-data-for-compress-test")}
	proof := GenerateStateTransitionProof(1, preState, types.Hash{}, txs, 1)
	proof.PostStateRoot = computeTransitionRoot(preState, txs, proof.Witness)

	cp, err := CompressProof(proof)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}

	if cp.RollupID != 1 {
		t.Fatalf("expected rollup ID 1, got %d", cp.RollupID)
	}
	if len(cp.Data) == 0 {
		t.Fatal("compressed data should not be empty")
	}
	if cp.OriginalSize == 0 {
		t.Fatal("original size should not be zero")
	}
	if cp.Checksum.IsZero() {
		t.Fatal("checksum should not be zero")
	}

	// Verify the compressed proof.
	if !VerifyCompressedProof(cp) {
		t.Fatal("compressed proof should verify")
	}
}

func TestVerifyCompressedProof_Nil(t *testing.T) {
	if VerifyCompressedProof(nil) {
		t.Fatal("nil compressed proof should not verify")
	}
	if VerifyCompressedProof(&CompressedProof{}) {
		t.Fatal("empty compressed proof should not verify")
	}
}

func TestDeduplicateAndExpand(t *testing.T) {
	// Create data with repeated 32-byte chunks.
	chunk := crypto.Keccak256([]byte("repeated-chunk"))
	var data []byte
	data = append(data, chunk...)
	data = append(data, chunk...) // duplicate
	data = append(data, crypto.Keccak256([]byte("unique"))...)

	compressed := deduplicateChunks(data)
	expanded := expandChunks(compressed)

	if !bytes.Equal(data, expanded) {
		t.Fatalf("round-trip failed: got %d bytes, want %d bytes", len(expanded), len(data))
	}
}

func TestDeduplicateAndExpand_WithRemainder(t *testing.T) {
	// Data that is not a multiple of 32.
	data := make([]byte, 70) // 2 full chunks + 6 bytes
	for i := range data {
		data[i] = byte(i)
	}

	compressed := deduplicateChunks(data)
	expanded := expandChunks(compressed)

	if !bytes.Equal(data, expanded) {
		t.Fatalf("round-trip with remainder failed")
	}
}

func TestDeduplicateAndExpand_Short(t *testing.T) {
	data := []byte("short")
	compressed := deduplicateChunks(data)
	expanded := expandChunks(compressed)

	if !bytes.Equal(data, expanded) {
		t.Fatalf("short data round-trip failed")
	}
}

func TestComputeMerkleRoot(t *testing.T) {
	leaves := [][]byte{
		crypto.Keccak256([]byte("a")),
		crypto.Keccak256([]byte("b")),
		crypto.Keccak256([]byte("c")),
	}

	root := computeMerkleRoot(leaves)
	if root.IsZero() {
		t.Fatal("merkle root should not be zero")
	}

	// Same leaves should produce same root.
	root2 := computeMerkleRoot(leaves)
	if root != root2 {
		t.Fatal("merkle root should be deterministic")
	}

	// Different leaves should produce different root.
	leaves[0] = crypto.Keccak256([]byte("x"))
	root3 := computeMerkleRoot(leaves)
	if root == root3 {
		t.Fatal("different leaves should produce different root")
	}
}

func TestComputeMerkleRoot_Empty(t *testing.T) {
	root := computeMerkleRoot(nil)
	if !root.IsZero() {
		t.Fatal("empty leaves should produce zero root")
	}
}

func TestComputeMerkleRoot_Single(t *testing.T) {
	leaf := crypto.Keccak256([]byte("only"))
	root := computeMerkleRoot([][]byte{leaf})

	// Single leaf: returned directly as the root.
	var expected types.Hash
	copy(expected[:], leaf)
	if root != expected {
		t.Fatal("single leaf root mismatch")
	}
}

func TestNewBatchProofAggregator_DefaultMaxSize(t *testing.T) {
	// Zero max size should default to MaxBatchProofs.
	agg := NewBatchProofAggregator(1, 0)
	if agg.maxSize != MaxBatchProofs {
		t.Fatalf("expected default max size %d, got %d", MaxBatchProofs, agg.maxSize)
	}

	// Negative max size should also default.
	agg2 := NewBatchProofAggregator(1, -5)
	if agg2.maxSize != MaxBatchProofs {
		t.Fatalf("expected default max size %d, got %d", MaxBatchProofs, agg2.maxSize)
	}
}
