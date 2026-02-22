package verkle

import (
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

// --- Multi-point proof tests ---

func TestVerkleProofMulti_SingleKey(t *testing.T) {
	tree := NewTree()

	var key [KeySize]byte
	key[0] = 0x01
	var val [ValueSize]byte
	val[0] = 0xAA
	tree.Put(key, val)

	cfg := DefaultIPAConfig()
	keys := [][KeySize]byte{key}

	proof, values, err := VerkleProofMulti(cfg, tree, keys)
	if err != nil {
		t.Fatalf("VerkleProofMulti error: %v", err)
	}
	if proof == nil {
		t.Fatal("proof is nil")
	}
	if len(values) != 1 {
		t.Fatalf("expected 1 value, got %d", len(values))
	}
	if values[0][0] != 0xAA {
		t.Errorf("value[0][0] = %d, want 0xAA", values[0][0])
	}
	if proof.IPAData == nil {
		t.Error("IPAData should not be nil")
	}
	if len(proof.Commitments) != 1 {
		t.Errorf("expected 1 commitment, got %d", len(proof.Commitments))
	}
}

func TestVerkleProofMulti_MultipleKeys(t *testing.T) {
	tree := NewTree()

	// Insert several keys with different stems.
	for i := 0; i < 3; i++ {
		var key [KeySize]byte
		key[0] = byte(i + 1)
		var val [ValueSize]byte
		val[0] = byte(i + 10)
		tree.Put(key, val)
	}

	cfg := DefaultIPAConfig()
	keys := make([][KeySize]byte, 3)
	for i := 0; i < 3; i++ {
		keys[i][0] = byte(i + 1)
	}

	proof, values, err := VerkleProofMulti(cfg, tree, keys)
	if err != nil {
		t.Fatalf("VerkleProofMulti error: %v", err)
	}
	if len(values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(values))
	}
	for i := 0; i < 3; i++ {
		if values[i][0] != byte(i+10) {
			t.Errorf("values[%d][0] = %d, want %d", i, values[i][0], i+10)
		}
	}
	if len(proof.EvalPoints) != 3 {
		t.Errorf("expected 3 eval points, got %d", len(proof.EvalPoints))
	}
}

func TestVerkleProofMulti_NonExistentKey(t *testing.T) {
	tree := NewTree()

	// Insert one key.
	var existKey [KeySize]byte
	existKey[0] = 0x01
	var val [ValueSize]byte
	val[0] = 0xAA
	tree.Put(existKey, val)

	cfg := DefaultIPAConfig()

	// Prove a non-existent key.
	var missingKey [KeySize]byte
	missingKey[0] = 0xFF
	keys := [][KeySize]byte{missingKey}

	proof, values, err := VerkleProofMulti(cfg, tree, keys)
	if err != nil {
		t.Fatalf("VerkleProofMulti error: %v", err)
	}
	if proof == nil {
		t.Fatal("proof should not be nil even for absent keys")
	}
	// Value should be all zeros for non-existent key.
	allZero := true
	for _, b := range values[0] {
		if b != 0 {
			allZero = false
			break
		}
	}
	if !allZero {
		t.Error("non-existent key should return zero value")
	}
}

func TestVerkleProofMulti_EmptyKeys(t *testing.T) {
	tree := NewTree()
	cfg := DefaultIPAConfig()

	_, _, err := VerkleProofMulti(cfg, tree, nil)
	if err == nil {
		t.Error("empty keys should error")
	}
}

func TestVerkleVerifyMulti_StructuralChecks(t *testing.T) {
	cfg := DefaultIPAConfig()
	root := Commitment{1}

	// Nil proof.
	if VerkleVerifyMulti(cfg, root, nil, nil, nil) {
		t.Error("nil proof should fail")
	}

	// Mismatched key/value counts.
	proof := &VerkleMultiProof{
		IPAData:     &crypto.IPAProofData{},
		Commitments: []Commitment{{1}},
		EvalPoints:  []FieldElement{Zero()},
		EvalResults: []FieldElement{Zero()},
	}
	keys := [][KeySize]byte{{}}
	values := [][ValueSize]byte{{}, {}} // mismatch
	if VerkleVerifyMulti(cfg, root, proof, keys, values) {
		t.Error("mismatched key/value counts should fail")
	}
}

// --- IPACommitter interface tests ---

func TestIPACommitter_Interface(t *testing.T) {
	committer := NewIPACommitter()
	if committer == nil {
		t.Fatal("NewIPACommitter returned nil")
	}

	cfg := committer.Config()
	if cfg == nil {
		t.Fatal("Config() returned nil")
	}
	if cfg.DomainSize != NodeWidth {
		t.Errorf("DomainSize = %d, want %d", cfg.DomainSize, NodeWidth)
	}
}

func TestIPACommitter_CommitToNode(t *testing.T) {
	committer := NewIPACommitter()

	var children [NodeWidth]Commitment
	children[0] = Commitment{1}

	c := committer.CommitToNode(children)
	if c.IsZero() {
		t.Error("commitment should not be zero")
	}
}

func TestIPACommitter_CommitToLeaf(t *testing.T) {
	committer := NewIPACommitter()

	var stem [StemSize]byte
	stem[0] = 0x01
	var values [NodeWidth]FieldElement
	for i := range values {
		values[i] = Zero()
	}
	values[0] = FieldElementFromUint64(42)

	c := committer.CommitToLeaf(stem, values)
	if c.IsZero() {
		t.Error("leaf commitment should not be zero")
	}
}

func TestIPACommitter_ProveAndVerify(t *testing.T) {
	committer := NewIPACommitter()
	cfg := committer.Config()

	// Use a small polynomial for efficiency (pad to 256).
	poly := make([]FieldElement, NodeWidth)
	for i := range poly {
		poly[i] = Zero()
	}
	poly[0] = FieldElementFromUint64(10)
	poly[1] = FieldElementFromUint64(20)

	evalPoint := FieldElementFromUint64(0)
	evalResult := evaluatePolynomial(poly, evalPoint)

	proof, err := committer.ProveEvaluation(poly, evalPoint, evalResult)
	if err != nil {
		t.Fatalf("ProveEvaluation error: %v", err)
	}

	commitPt := cfg.PedersenCommitPoint(poly)
	ok, err := committer.VerifyEvaluation(commitPt, proof)
	if err != nil {
		t.Fatalf("VerifyEvaluation error: %v", err)
	}
	if !ok {
		t.Error("valid evaluation proof should verify")
	}
}
