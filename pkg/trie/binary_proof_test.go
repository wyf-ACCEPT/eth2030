package trie

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// --- BinaryProof structure ---

func TestBinaryProof_ProofSize_Nil(t *testing.T) {
	var p *BinaryProof
	if p.ProofSize() != 0 {
		t.Fatalf("nil proof size = %d, want 0", p.ProofSize())
	}
}

func TestBinaryProof_ProofSize_NoSiblings(t *testing.T) {
	p := &BinaryProof{
		Key:   crypto.Keccak256Hash([]byte("key")),
		Value: []byte("value"),
	}
	// 0*32 + 32 + 5
	expected := 32 + 5
	if p.ProofSize() != expected {
		t.Fatalf("ProofSize = %d, want %d", p.ProofSize(), expected)
	}
}

func TestBinaryProof_ProofSize_WithSiblings(t *testing.T) {
	p := &BinaryProof{
		Siblings: make([]types.Hash, 10),
		Key:      types.Hash{},
		Value:    []byte("hello"),
	}
	// 10*32 + 32 + 5 = 357
	expected := 10*32 + 32 + 5
	if p.ProofSize() != expected {
		t.Fatalf("ProofSize = %d, want %d", p.ProofSize(), expected)
	}
}

// --- Prove ---

func TestBinaryTrie_Prove_SingleEntry(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("single"), []byte("entry"))
	bt.Hash()

	proof, err := bt.Prove([]byte("single"))
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if proof == nil {
		t.Fatal("proof should not be nil")
	}
	if !bytes.Equal(proof.Value, []byte("entry")) {
		t.Fatalf("proof.Value = %q, want %q", proof.Value, "entry")
	}
	// Single entry: no siblings.
	if len(proof.Siblings) != 0 {
		t.Fatalf("single entry should have 0 siblings, got %d", len(proof.Siblings))
	}
}

func TestBinaryTrie_Prove_TwoEntries(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("alpha"), []byte("one"))
	bt.Put([]byte("beta"), []byte("two"))
	bt.Hash()

	proof, err := bt.Prove([]byte("alpha"))
	if err != nil {
		t.Fatalf("Prove(alpha): %v", err)
	}
	if !bytes.Equal(proof.Value, []byte("one")) {
		t.Fatalf("proof.Value = %q, want %q", proof.Value, "one")
	}
	// With two entries that diverge at some bit, we should have at least one sibling.
	if len(proof.Siblings) == 0 {
		t.Fatal("two-entry proof should have at least 1 sibling")
	}
}

func TestBinaryTrie_Prove_NonExistent(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("exists"), []byte("yes"))
	bt.Hash()

	_, err := bt.Prove([]byte("missing"))
	if err != ErrNotFound {
		t.Fatalf("Prove(missing): err = %v, want ErrNotFound", err)
	}
}

func TestBinaryTrie_Prove_EmptyTrie(t *testing.T) {
	bt := NewBinaryTrie()
	_, err := bt.Prove([]byte("anything"))
	if err != ErrNotFound {
		t.Fatalf("Prove on empty trie: err = %v, want ErrNotFound", err)
	}
}

// --- ProveHashed ---

func TestBinaryTrie_ProveHashed(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("value"))
	bt.Hash()

	hk := crypto.Keccak256Hash([]byte("key"))
	proof, err := bt.ProveHashed(hk)
	if err != nil {
		t.Fatalf("ProveHashed: %v", err)
	}
	if proof == nil {
		t.Fatal("proof should not be nil")
	}
	if proof.Key != hk {
		t.Fatal("proof key should match hashed key")
	}
}

func TestBinaryTrie_ProveHashed_NotFound(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("exists"), []byte("yes"))
	bt.Hash()

	hk := crypto.Keccak256Hash([]byte("nope"))
	_, err := bt.ProveHashed(hk)
	if err != ErrNotFound {
		t.Fatalf("ProveHashed(missing): err = %v, want ErrNotFound", err)
	}
}

// --- verifyBinaryProofSimple (renamed from the original VerifyBinaryProof in binary_proof.go) ---

func TestVerifyBinaryProofSimple_Valid(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("test"), []byte("proof"))
	root := bt.Hash()

	proof, err := bt.Prove([]byte("test"))
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}

	if err := verifyBinaryProofSimple(root, proof); err != nil {
		t.Fatalf("verifyBinaryProofSimple: %v", err)
	}
}

func TestVerifyBinaryProofSimple_NilProof(t *testing.T) {
	if err := verifyBinaryProofSimple(types.Hash{}, nil); err == nil {
		t.Fatal("nil proof should fail verification")
	}
}

func TestVerifyBinaryProofSimple_TamperedValue(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("value"))
	root := bt.Hash()

	proof, err := bt.Prove([]byte("key"))
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}

	proof.Value = []byte("tampered")
	if err := verifyBinaryProofSimple(root, proof); err == nil {
		t.Fatal("tampered proof should fail verification")
	}
}

func TestVerifyBinaryProofSimple_WrongRoot(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("value"))
	bt.Hash()

	proof, err := bt.Prove([]byte("key"))
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}

	wrongRoot := types.Hash{0xff, 0xfe, 0xfd}
	if err := verifyBinaryProofSimple(wrongRoot, proof); err == nil {
		t.Fatal("wrong root should fail verification")
	}
}

func TestVerifyBinaryProofSimple_TamperedSibling(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("a"), []byte("1"))
	bt.Put([]byte("b"), []byte("2"))
	root := bt.Hash()

	proof, err := bt.Prove([]byte("a"))
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if len(proof.Siblings) > 0 {
		proof.Siblings[0][0] ^= 0xff
	}
	if err := verifyBinaryProofSimple(root, proof); err == nil {
		t.Fatal("tampered sibling should fail verification")
	}
}

// --- Proof on many entries ---

func TestBinaryProof_ManyEntries(t *testing.T) {
	bt := NewBinaryTrie()
	entries := make(map[string]string)
	for i := 0; i < 50; i++ {
		k := []byte{byte(i), byte(i + 1), byte(i + 2)}
		v := []byte{byte(i * 2)}
		bt.Put(k, v)
		entries[string(k)] = string(v)
	}
	root := bt.Hash()

	for k, v := range entries {
		proof, err := bt.Prove([]byte(k))
		if err != nil {
			t.Fatalf("Prove(%x): %v", k, err)
		}
		if !bytes.Equal(proof.Value, []byte(v)) {
			t.Fatalf("proof value for %x = %x, want %x", k, proof.Value, v)
		}
		// Verify the proof against the root using the verifier from proof_verifier.go
		_, verifyErr := VerifyBinaryProof(root, proof)
		if verifyErr != nil {
			t.Fatalf("VerifyBinaryProof(%x): %v", k, verifyErr)
		}
	}
}

// --- Proof key matches ---

func TestBinaryProof_KeyMatchesHash(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("mykey"), []byte("myval"))
	bt.Hash()

	proof, err := bt.Prove([]byte("mykey"))
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}

	expected := crypto.Keccak256Hash([]byte("mykey"))
	if proof.Key != expected {
		t.Fatalf("proof.Key = %x, want %x", proof.Key, expected)
	}
}

// --- ErrBinaryProofInvalid ---

func TestErrBinaryProofInvalid(t *testing.T) {
	if ErrBinaryProofInvalid == nil {
		t.Fatal("ErrBinaryProofInvalid should not be nil")
	}
	if ErrBinaryProofInvalid.Error() == "" {
		t.Fatal("ErrBinaryProofInvalid should have a message")
	}
}

// --- getBit helper used in proof construction ---

func TestGetBit_MSB(t *testing.T) {
	// 0x80 = 10000000
	h := types.Hash{0x80}
	if getBit(h, 0) != 1 {
		t.Fatal("bit 0 of 0x80 should be 1")
	}
	for i := 1; i < 8; i++ {
		if getBit(h, i) != 0 {
			t.Fatalf("bit %d of 0x80 should be 0", i)
		}
	}
}

func TestGetBit_LSB(t *testing.T) {
	// 0x01 = 00000001
	h := types.Hash{0x01}
	for i := 0; i < 7; i++ {
		if getBit(h, i) != 0 {
			t.Fatalf("bit %d of 0x01 should be 0", i)
		}
	}
	if getBit(h, 7) != 1 {
		t.Fatal("bit 7 of 0x01 should be 1")
	}
}

func TestGetBit_OutOfRange(t *testing.T) {
	h := types.Hash{0xff}
	// pos >= 256 should return 0.
	if getBit(h, 256) != 0 {
		t.Fatal("out of range bit should be 0")
	}
}
