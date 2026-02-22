package trie

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// --- MPT proof verifier tests ---

func TestVerifyMPTProof_SimpleInclusion(t *testing.T) {
	tr := New()
	tr.Put([]byte("alpha"), []byte("one"))
	tr.Put([]byte("bravo"), []byte("two"))
	tr.Put([]byte("charlie"), []byte("three"))
	root := tr.Hash()

	for _, key := range []string{"alpha", "bravo", "charlie"} {
		proof, err := tr.Prove([]byte(key))
		if err != nil {
			t.Fatalf("Prove(%q): %v", key, err)
		}
		r, err := VerifyMPTProof(root, []byte(key), proof)
		if err != nil {
			t.Fatalf("VerifyMPTProof(%q): %v", key, err)
		}
		if !r.Exists {
			t.Errorf("expected key %q to exist", key)
		}
		if r.Value == nil {
			t.Errorf("expected non-nil value for %q", key)
		}
	}
}

func TestVerifyMPTProof_EmptyRoot(t *testing.T) {
	r, err := VerifyMPTProof(emptyRoot, []byte("missing"), nil)
	if err != nil {
		t.Fatalf("unexpected error for empty trie: %v", err)
	}
	if r.Exists {
		t.Error("key should not exist in empty trie")
	}
}

func TestVerifyMPTProof_WrongRoot(t *testing.T) {
	tr := New()
	tr.Put([]byte("key"), []byte("val"))
	root := tr.Hash()

	proof, _ := tr.Prove([]byte("key"))

	// Tamper with root.
	wrongRoot := root
	wrongRoot[0] ^= 0xff

	_, err := VerifyMPTProof(wrongRoot, []byte("key"), proof)
	if err == nil {
		t.Error("expected error for wrong root")
	}
}

func TestVerifyMPTProof_NilKey(t *testing.T) {
	_, err := VerifyMPTProof(types.Hash{}, nil, nil)
	if err != ErrProofNilInput {
		t.Errorf("expected ErrProofNilInput, got %v", err)
	}
}

func TestVerifyMPTProof_EmptyProofNonEmptyRoot(t *testing.T) {
	tr := New()
	tr.Put([]byte("x"), []byte("y"))
	root := tr.Hash()

	_, err := VerifyMPTProof(root, []byte("x"), nil)
	if err == nil {
		t.Error("expected error for empty proof on non-empty root")
	}
}

func TestVerifyMPTProof_AbsenceProof(t *testing.T) {
	tr := New()
	tr.Put([]byte("alpha"), []byte("one"))
	tr.Put([]byte("bravo"), []byte("two"))
	root := tr.Hash()

	// Prove absence of a key that does not exist.
	proof, err := tr.ProveAbsence([]byte("charlie"))
	if err != nil {
		t.Fatalf("ProveAbsence: %v", err)
	}

	r, err := VerifyMPTProof(root, []byte("charlie"), proof)
	if err != nil {
		t.Fatalf("VerifyMPTProof absence: %v", err)
	}
	if r.Exists {
		t.Error("key should not exist")
	}
}

func TestVerifyMPTProof_TamperedProofNode(t *testing.T) {
	tr := New()
	tr.Put([]byte("foo"), []byte("bar"))
	root := tr.Hash()

	proof, _ := tr.Prove([]byte("foo"))

	// Tamper with the last proof node.
	tampered := make([][]byte, len(proof))
	copy(tampered, proof)
	last := make([]byte, len(proof[len(proof)-1]))
	copy(last, proof[len(proof)-1])
	last[0] ^= 0xff
	tampered[len(tampered)-1] = last

	_, err := VerifyMPTProof(root, []byte("foo"), tampered)
	if err == nil {
		t.Error("expected error for tampered proof")
	}
}

func TestVerifyMPTProof_MultipleKeys(t *testing.T) {
	tr := New()
	keys := []string{"a", "ab", "abc", "abcd", "b", "bc", "bcd", "c"}
	for _, k := range keys {
		tr.Put([]byte(k), []byte(k+"_val"))
	}
	root := tr.Hash()

	for _, k := range keys {
		proof, err := tr.Prove([]byte(k))
		if err != nil {
			t.Fatalf("Prove(%q): %v", k, err)
		}
		r, err := VerifyMPTProof(root, []byte(k), proof)
		if err != nil {
			t.Fatalf("VerifyMPTProof(%q): %v", k, err)
		}
		if !r.Exists {
			t.Errorf("%q should exist", k)
		}
		if string(r.Value) != k+"_val" {
			t.Errorf("value for %q: got %q, want %q", k, r.Value, k+"_val")
		}
	}
}

func TestVerifyMPTAbsence_Success(t *testing.T) {
	tr := New()
	tr.Put([]byte("exist"), []byte("yes"))
	root := tr.Hash()

	proof, _ := tr.ProveAbsence([]byte("nope"))
	if err := VerifyMPTAbsence(root, []byte("nope"), proof); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyMPTAbsence_KeyExists(t *testing.T) {
	tr := New()
	tr.Put([]byte("exist"), []byte("yes"))
	root := tr.Hash()

	proof, _ := tr.Prove([]byte("exist"))
	err := VerifyMPTAbsence(root, []byte("exist"), proof)
	if err == nil {
		t.Error("expected error when key exists")
	}
}

// --- Binary proof verifier tests ---

func TestVerifyBinaryProof_SimpleInclusion(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("hello"), []byte("world"))
	root := bt.Hash()

	proof, err := bt.Prove([]byte("hello"))
	if err != nil {
		t.Fatalf("BinaryTrie.Prove: %v", err)
	}

	r, err := VerifyBinaryProof(root, proof)
	if err != nil {
		t.Fatalf("VerifyBinaryProof: %v", err)
	}
	if !r.Exists {
		t.Error("key should exist")
	}
	if !bytes.Equal(r.Value, []byte("world")) {
		t.Errorf("value = %q, want %q", r.Value, "world")
	}
}

func TestVerifyBinaryProof_NilProof(t *testing.T) {
	_, err := VerifyBinaryProof(types.Hash{}, nil)
	if err != ErrProofNilInput {
		t.Errorf("expected ErrProofNilInput, got %v", err)
	}
}

func TestVerifyBinaryProof_WrongRoot(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("val"))
	root := bt.Hash()

	proof, _ := bt.Prove([]byte("key"))

	wrongRoot := root
	wrongRoot[0] ^= 0xff

	_, err := VerifyBinaryProof(wrongRoot, proof)
	if err == nil {
		t.Error("expected root mismatch error")
	}
}

func TestVerifyBinaryProof_TamperedSibling(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("a"), []byte("1"))
	bt.Put([]byte("b"), []byte("2"))
	root := bt.Hash()

	proof, _ := bt.Prove([]byte("a"))
	if len(proof.Siblings) > 0 {
		proof.Siblings[0][0] ^= 0xff // tamper
	}

	_, err := VerifyBinaryProof(root, proof)
	if err == nil {
		t.Error("expected error for tampered sibling")
	}
}

func TestVerifyBinaryProof_MultipleEntries(t *testing.T) {
	bt := NewBinaryTrie()
	entries := map[string]string{
		"alpha": "one", "bravo": "two", "charlie": "three",
		"delta": "four", "echo": "five",
	}
	for k, v := range entries {
		bt.Put([]byte(k), []byte(v))
	}
	root := bt.Hash()

	for k, v := range entries {
		proof, err := bt.Prove([]byte(k))
		if err != nil {
			t.Fatalf("Prove(%q): %v", k, err)
		}
		r, err := VerifyBinaryProof(root, proof)
		if err != nil {
			t.Fatalf("VerifyBinaryProof(%q): %v", k, err)
		}
		if !bytes.Equal(r.Value, []byte(v)) {
			t.Errorf("value for %q: got %q, want %q", k, r.Value, v)
		}
	}
}

func TestVerifyBinaryAbsence_EmptyTrie(t *testing.T) {
	absentKey := crypto.Keccak256Hash([]byte("anything"))
	err := VerifyBinaryAbsence(types.Hash{}, absentKey, nil)
	if err != nil {
		t.Fatalf("expected success for empty trie absence: %v", err)
	}
}

func TestVerifyBinaryAbsence_NonEmptyNoProof(t *testing.T) {
	root := crypto.Keccak256Hash([]byte("nonempty"))
	absentKey := crypto.Keccak256Hash([]byte("key"))
	err := VerifyBinaryAbsence(root, absentKey, nil)
	if err == nil {
		t.Error("expected error for non-empty root without proof")
	}
}

// --- Verkle proof verifier tests ---

func TestVerifyVerkleProof_NilProof(t *testing.T) {
	_, err := VerifyVerkleProof([32]byte{}, nil)
	if err != ErrProofNilInput {
		t.Errorf("expected ErrProofNilInput, got %v", err)
	}
}

func TestVerifyVerkleProof_NoCommitments(t *testing.T) {
	proof := &VerkleProofData{
		IPAProof: []byte{0x01},
	}
	_, err := VerifyVerkleProof([32]byte{}, proof)
	if err == nil {
		t.Error("expected error for empty commitments")
	}
}

func TestVerifyVerkleProof_InvalidDepth(t *testing.T) {
	root := [32]byte{1}
	proof := &VerkleProofData{
		CommitmentsByPath: [][32]byte{root},
		IPAProof:          []byte{0x01},
		Depth:             99, // > 31
	}
	_, err := VerifyVerkleProof(root, proof)
	if err == nil {
		t.Error("expected error for invalid depth")
	}
}

func TestVerifyVerkleProof_EmptyIPAProof(t *testing.T) {
	root := [32]byte{1}
	proof := &VerkleProofData{
		CommitmentsByPath: [][32]byte{root},
		Depth:             0,
	}
	_, err := VerifyVerkleProof(root, proof)
	if err == nil {
		t.Error("expected error for empty IPA proof")
	}
}

func TestVerifyVerkleProof_RootMismatch(t *testing.T) {
	root := [32]byte{1}
	proof := &VerkleProofData{
		CommitmentsByPath: [][32]byte{{2}}, // different from root
		IPAProof:          []byte{0x01},
		Depth:             0,
	}
	_, err := VerifyVerkleProof(root, proof)
	if err == nil {
		t.Error("expected error for root commitment mismatch")
	}
}

func TestVerifyVerkleProof_InclusionProof(t *testing.T) {
	root := [32]byte{1, 2, 3}
	val := [32]byte{0xaa, 0xbb}
	proof := &VerkleProofData{
		CommitmentsByPath: [][32]byte{root, {4, 5, 6}},
		D:                 [32]byte{7, 8, 9},
		IPAProof:          []byte{0x01, 0x02, 0x03},
		Depth:             1,
		ExtensionPresent:  true,
		Key:               [32]byte{0x10},
		Value:             &val,
	}

	r, err := VerifyVerkleProof(root, proof)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Exists {
		t.Error("key should exist")
	}
	if r.Value == nil || *r.Value != val {
		t.Error("value mismatch")
	}
}

func TestVerifyVerkleProof_AbsenceProof(t *testing.T) {
	root := [32]byte{1, 2, 3}
	proof := &VerkleProofData{
		CommitmentsByPath: [][32]byte{root},
		IPAProof:          []byte{0x01},
		Depth:             0,
		ExtensionPresent:  false,
		Key:               [32]byte{0x10},
		Value:             nil,
	}

	r, err := VerifyVerkleProof(root, proof)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Exists {
		t.Error("key should not exist in absence proof")
	}
}

func TestVerifyVerkleProof_ZeroCommitmentInPath(t *testing.T) {
	root := [32]byte{1, 2, 3}
	proof := &VerkleProofData{
		CommitmentsByPath: [][32]byte{root, {}}, // second is zero
		IPAProof:          []byte{0x01},
		Depth:             1,
	}
	_, err := VerifyVerkleProof(root, proof)
	if err == nil {
		t.Error("expected error for zero commitment in path")
	}
}

// --- Multi-proof tests ---

func TestVerifyMultiProof_EmptyItems(t *testing.T) {
	_, err := VerifyMultiProof(types.Hash{}, nil)
	if err != ErrProofEmpty {
		t.Errorf("expected ErrProofEmpty, got %v", err)
	}
}

func TestVerifyMultiProof_NilKeyItem(t *testing.T) {
	items := []MultiProofItem{{Key: nil}}
	_, err := VerifyMultiProof(types.Hash{}, items)
	if err == nil {
		t.Error("expected error for nil key")
	}
}

func TestVerifyMultiProof_AllValid(t *testing.T) {
	tr := New()
	tr.Put([]byte("a"), []byte("1"))
	tr.Put([]byte("b"), []byte("2"))
	tr.Put([]byte("c"), []byte("3"))
	root := tr.Hash()

	items := make([]MultiProofItem, 3)
	for i, k := range []string{"a", "b", "c"} {
		proof, err := tr.Prove([]byte(k))
		if err != nil {
			t.Fatalf("Prove(%q): %v", k, err)
		}
		items[i] = MultiProofItem{Key: []byte(k), Proof: proof}
	}

	r, err := VerifyMultiProof(root, items)
	if err != nil {
		t.Fatalf("VerifyMultiProof: %v", err)
	}
	if len(r.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(r.Results))
	}
	for i, res := range r.Results {
		if !res.Exists {
			t.Errorf("result %d should exist", i)
		}
	}
}

func TestVerifyMultiProof_OneBadProof(t *testing.T) {
	tr := New()
	tr.Put([]byte("x"), []byte("y"))
	root := tr.Hash()

	proof, _ := tr.Prove([]byte("x"))

	// Tamper with proof.
	tampered := make([][]byte, len(proof))
	copy(tampered, proof)
	tampered[0] = []byte{0xff, 0xfe, 0xfd}

	items := []MultiProofItem{
		{Key: []byte("x"), Proof: tampered},
	}
	_, err := VerifyMultiProof(root, items)
	if err == nil {
		t.Error("expected error for tampered proof item")
	}
}

func TestVerifyMultiProof_ValueMismatch(t *testing.T) {
	tr := New()
	tr.Put([]byte("k"), []byte("real"))
	root := tr.Hash()

	proof, _ := tr.Prove([]byte("k"))
	items := []MultiProofItem{
		{Key: []byte("k"), Value: []byte("wrong"), Proof: proof},
	}
	_, err := VerifyMultiProof(root, items)
	if err == nil {
		t.Error("expected error for value mismatch")
	}
}

func TestVerifyMultiProof_MixedExistAndAbsent(t *testing.T) {
	tr := New()
	tr.Put([]byte("exist"), []byte("val"))
	root := tr.Hash()

	proofExist, _ := tr.Prove([]byte("exist"))
	proofAbsent, _ := tr.ProveAbsence([]byte("absent"))

	items := []MultiProofItem{
		{Key: []byte("exist"), Proof: proofExist},
		{Key: []byte("absent"), Proof: proofAbsent},
	}
	r, err := VerifyMultiProof(root, items)
	if err != nil {
		t.Fatalf("VerifyMultiProof: %v", err)
	}
	if !r.Results[0].Exists {
		t.Error("first item should exist")
	}
	if r.Results[1].Exists {
		t.Error("second item should not exist")
	}
}
