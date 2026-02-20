package trie

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// TestGenerateInclusionProof verifies that an inclusion proof can be generated
// and verified for keys that exist in the trie.
func TestGenerateInclusionProof(t *testing.T) {
	tr := New()
	keys := [][]byte{
		[]byte("alpha"),
		[]byte("bravo"),
		[]byte("charlie"),
		[]byte("delta"),
		[]byte("echo"),
	}
	for _, k := range keys {
		if err := tr.Put(k, append([]byte("val-"), k...)); err != nil {
			t.Fatalf("Put(%s): %v", k, err)
		}
	}

	pg := NewProofGenerator(tr)

	for _, k := range keys {
		proof, err := pg.GenerateProof(k)
		if err != nil {
			t.Fatalf("GenerateProof(%s): %v", k, err)
		}
		if proof.Key == nil {
			t.Fatalf("proof key is nil for %s", k)
		}
		expectedVal := append([]byte("val-"), k...)
		if !bytes.Equal(proof.Value, expectedVal) {
			t.Errorf("proof value mismatch for %s: got %x, want %x", k, proof.Value, expectedVal)
		}
		if len(proof.ProofNodes) == 0 {
			t.Errorf("no proof nodes for %s", k)
		}
	}
}

// TestGenerateExclusionProof verifies that an exclusion proof is generated
// for keys that do not exist in the trie.
func TestGenerateExclusionProof(t *testing.T) {
	tr := New()
	if err := tr.Put([]byte("abc"), []byte("val1")); err != nil {
		t.Fatal(err)
	}
	if err := tr.Put([]byte("xyz"), []byte("val2")); err != nil {
		t.Fatal(err)
	}

	pg := NewProofGenerator(tr)

	// Key "def" does not exist.
	proof, err := pg.GenerateExclusionProof([]byte("def"))
	if err != nil {
		t.Fatalf("GenerateExclusionProof: %v", err)
	}
	if proof.Value != nil {
		t.Error("exclusion proof should have nil value")
	}
}

// TestVerifyInclusionProof verifies inclusion proof verification roundtrip.
func TestVerifyInclusionProof(t *testing.T) {
	tr := New()
	testData := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}
	for k, v := range testData {
		if err := tr.Put([]byte(k), []byte(v)); err != nil {
			t.Fatal(err)
		}
	}

	pg := NewProofGenerator(tr)

	for k, v := range testData {
		proof, err := pg.GenerateProof([]byte(k))
		if err != nil {
			t.Fatalf("GenerateProof(%s): %v", k, err)
		}

		err = VerifyInclusionProof(proof)
		if err != nil {
			t.Errorf("VerifyInclusionProof(%s): %v", k, err)
		}

		if !bytes.Equal(proof.Value, []byte(v)) {
			t.Errorf("value mismatch for %s", k)
		}
	}
}

// TestVerifyExclusionProof verifies exclusion proof verification roundtrip.
func TestVerifyExclusionProof(t *testing.T) {
	tr := New()
	if err := tr.Put([]byte("exists"), []byte("yes")); err != nil {
		t.Fatal(err)
	}

	pg := NewProofGenerator(tr)

	proof, err := pg.GenerateExclusionProof([]byte("missing"))
	if err != nil {
		t.Fatalf("GenerateExclusionProof: %v", err)
	}

	err = VerifyExclusionProof(proof)
	if err != nil {
		t.Errorf("VerifyExclusionProof: %v", err)
	}
}

// TestMultiProof verifies batch proof generation and verification.
func TestMultiProof(t *testing.T) {
	tr := New()
	if err := tr.Put([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := tr.Put([]byte("b"), []byte("2")); err != nil {
		t.Fatal(err)
	}

	pg := NewProofGenerator(tr)

	keys := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	mp, err := pg.GenerateMultiProof(keys)
	if err != nil {
		t.Fatalf("GenerateMultiProof: %v", err)
	}

	if len(mp.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(mp.Items))
	}

	// "a" and "b" should exist, "c" should not.
	if !mp.Items[0].Exists {
		t.Error("expected 'a' to exist")
	}
	if !mp.Items[1].Exists {
		t.Error("expected 'b' to exist")
	}
	if mp.Items[2].Exists {
		t.Error("expected 'c' to not exist")
	}

	// Verify the entire multi-proof.
	err = VerifyMultiProofResult(mp)
	if err != nil {
		t.Errorf("VerifyMultiProofResult: %v", err)
	}
}

// TestSerializeDeserializeProof tests proof serialization roundtrip.
func TestSerializeDeserializeProof(t *testing.T) {
	tr := New()
	if err := tr.Put([]byte("hello"), []byte("world")); err != nil {
		t.Fatal(err)
	}

	pg := NewProofGenerator(tr)
	proof, err := pg.GenerateProof([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}

	serialized, err := SerializeInclusionProof(proof)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	rootHash, key, value, nodes, err := DeserializeProof(serialized)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	if rootHash != proof.RootHash {
		t.Error("root hash mismatch")
	}
	if !bytes.Equal(key, proof.Key) {
		t.Error("key mismatch")
	}
	if !bytes.Equal(value, proof.Value) {
		t.Error("value mismatch")
	}
	if len(nodes) != len(proof.ProofNodes) {
		t.Errorf("node count mismatch: got %d, want %d", len(nodes), len(proof.ProofNodes))
	}
}

// TestProofSize verifies the proof size calculation.
func TestProofSize(t *testing.T) {
	nodes := [][]byte{
		{0x01, 0x02},
		{0x03, 0x04, 0x05},
	}
	if got := ProofSize(nodes); got != 5 {
		t.Errorf("ProofSize = %d, want 5", got)
	}
}

// TestHashProofNodes verifies that proof node hashing produces deterministic results.
func TestHashProofNodes(t *testing.T) {
	nodes := [][]byte{{0xaa}, {0xbb}, {0xcc}}
	h1 := HashProofNodes(nodes)
	h2 := HashProofNodes(nodes)
	if h1 != h2 {
		t.Error("HashProofNodes not deterministic")
	}

	// Different input should produce different hash.
	h3 := HashProofNodes([][]byte{{0xdd}})
	if h1 == h3 {
		t.Error("different inputs produced same hash")
	}

	// Empty input.
	h4 := HashProofNodes(nil)
	if h4 != (types.Hash{}) {
		t.Error("empty input should produce zero hash")
	}
}

// TestNilKeyErrors verifies that nil/empty key inputs return errors.
func TestNilKeyErrors(t *testing.T) {
	tr := New()
	pg := NewProofGenerator(tr)

	_, err := pg.GenerateProof(nil)
	if err == nil {
		t.Error("expected error for nil key in GenerateProof")
	}

	_, err = pg.GenerateExclusionProof(nil)
	if err == nil {
		t.Error("expected error for nil key in GenerateExclusionProof")
	}
}

// TestEmptyTrieExclusionProof verifies exclusion proof for an empty trie.
func TestEmptyTrieExclusionProof(t *testing.T) {
	tr := New()
	pg := NewProofGenerator(tr)

	// Empty trie: any key is absent. GenerateProof returns error because
	// trie is empty. GenerateExclusionProof should succeed.
	proof, err := pg.GenerateExclusionProof([]byte("anything"))
	if err != nil {
		t.Fatalf("GenerateExclusionProof on empty trie: %v", err)
	}
	if len(proof.ProofNodes) != 0 {
		t.Errorf("expected 0 proof nodes for empty trie, got %d", len(proof.ProofNodes))
	}
}
