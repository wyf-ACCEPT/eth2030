package pqc

import (
	"bytes"
	"testing"
)

func TestLatticeBlobCommit_Commit(t *testing.T) {
	scheme := NewLatticeBlobCommit()
	data := []byte("test blob data for commitment")

	commitment, err := scheme.Commit(data)
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	if commitment.Scheme != SchemeLatticeBlobCommit {
		t.Errorf("wrong scheme: got %s, want %s", commitment.Scheme, SchemeLatticeBlobCommit)
	}
	if len(commitment.Commitment) != 32 { // Keccak256 output
		t.Errorf("wrong commitment length: got %d, want 32", len(commitment.Commitment))
	}
	if len(commitment.Proof) != 32 {
		t.Errorf("wrong proof length: got %d, want 32", len(commitment.Proof))
	}
}

func TestLatticeBlobCommit_Verify(t *testing.T) {
	scheme := NewLatticeBlobCommit()
	data := []byte("test blob data for verification")

	commitment, err := scheme.Commit(data)
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	if !scheme.Verify(commitment, data) {
		t.Error("valid commitment should verify")
	}
}

func TestLatticeBlobCommit_Verify_WrongData(t *testing.T) {
	scheme := NewLatticeBlobCommit()
	data := []byte("original data")

	commitment, err := scheme.Commit(data)
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	if scheme.Verify(commitment, []byte("tampered data")) {
		t.Error("tampered data should not verify")
	}
}

func TestLatticeBlobCommit_Verify_NilCommitment(t *testing.T) {
	scheme := NewLatticeBlobCommit()
	if scheme.Verify(nil, []byte("data")) {
		t.Error("nil commitment should not verify")
	}
}

func TestLatticeBlobCommit_Verify_EmptyData(t *testing.T) {
	scheme := NewLatticeBlobCommit()
	commitment := &PQBlobCommitment{
		Scheme:     SchemeLatticeBlobCommit,
		Commitment: []byte{1, 2, 3},
		Proof:      []byte{4, 5, 6},
	}
	if scheme.Verify(commitment, nil) {
		t.Error("nil data should not verify")
	}
	if scheme.Verify(commitment, []byte{}) {
		t.Error("empty data should not verify")
	}
}

func TestLatticeBlobCommit_Verify_WrongScheme(t *testing.T) {
	scheme := NewLatticeBlobCommit()
	data := []byte("test data")

	commitment, err := scheme.Commit(data)
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	// Change the scheme.
	commitment.Scheme = "wrong-scheme"
	if scheme.Verify(commitment, data) {
		t.Error("wrong scheme should not verify")
	}
}

func TestLatticeBlobCommit_CommitNilData(t *testing.T) {
	scheme := NewLatticeBlobCommit()
	_, err := scheme.Commit(nil)
	if err != ErrPQCommitNilData {
		t.Fatalf("expected ErrPQCommitNilData, got %v", err)
	}
}

func TestLatticeBlobCommit_CommitEmptyData(t *testing.T) {
	scheme := NewLatticeBlobCommit()
	_, err := scheme.Commit([]byte{})
	if err != ErrPQCommitEmptyData {
		t.Fatalf("expected ErrPQCommitEmptyData, got %v", err)
	}
}

func TestLatticeBlobCommit_Deterministic(t *testing.T) {
	scheme := NewLatticeBlobCommit()
	data := []byte("deterministic test data")

	c1, err := scheme.Commit(data)
	if err != nil {
		t.Fatalf("first commit failed: %v", err)
	}

	c2, err := scheme.Commit(data)
	if err != nil {
		t.Fatalf("second commit failed: %v", err)
	}

	if !bytes.Equal(c1.Commitment, c2.Commitment) {
		t.Error("commitments should be deterministic")
	}
	if !bytes.Equal(c1.Proof, c2.Proof) {
		t.Error("proofs should be deterministic")
	}
}

func TestLatticeBlobCommit_DifferentData(t *testing.T) {
	scheme := NewLatticeBlobCommit()

	c1, _ := scheme.Commit([]byte("data1"))
	c2, _ := scheme.Commit([]byte("data2"))

	if bytes.Equal(c1.Commitment, c2.Commitment) {
		t.Error("different data should produce different commitments")
	}
}

func TestLatticeBlobCommit_Name(t *testing.T) {
	scheme := NewLatticeBlobCommit()
	if scheme.Name() != SchemeLatticeBlobCommit {
		t.Errorf("wrong name: got %s, want %s", scheme.Name(), SchemeLatticeBlobCommit)
	}
}

func TestLatticeBlobCommit_Interface(t *testing.T) {
	// Ensure LatticeBlobCommit implements PQCommitScheme.
	var _ PQCommitScheme = (*LatticeBlobCommit)(nil)
}

func TestDefaultMigrationPath(t *testing.T) {
	path := DefaultMigrationPath()
	if path.CurrentScheme != SchemeLegacyKZG {
		t.Errorf("wrong current scheme: got %s, want %s", path.CurrentScheme, SchemeLegacyKZG)
	}
	if path.TargetScheme != SchemeLatticeBlobCommit {
		t.Errorf("wrong target scheme: got %s, want %s", path.TargetScheme, SchemeLatticeBlobCommit)
	}
	if path.HybridMode {
		t.Error("hybrid mode should be false by default")
	}
}

func TestLatticeBlobCommit_LargeData(t *testing.T) {
	scheme := NewLatticeBlobCommit()

	// Simulate blob-sized data (128 KiB).
	data := make([]byte, 128*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	commitment, err := scheme.Commit(data)
	if err != nil {
		t.Fatalf("commit on large data failed: %v", err)
	}

	if !scheme.Verify(commitment, data) {
		t.Error("verification of large data commitment failed")
	}
}

func TestLatticeBlobCommit_TamperedCommitment(t *testing.T) {
	scheme := NewLatticeBlobCommit()
	data := []byte("test data")

	commitment, err := scheme.Commit(data)
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	// Tamper with the commitment bytes.
	tampered := &PQBlobCommitment{
		Scheme:     commitment.Scheme,
		Commitment: make([]byte, len(commitment.Commitment)),
		Proof:      commitment.Proof,
	}
	copy(tampered.Commitment, commitment.Commitment)
	tampered.Commitment[0] ^= 0xFF

	if scheme.Verify(tampered, data) {
		t.Error("tampered commitment should not verify")
	}
}

func TestLatticeBlobCommit_TamperedProof(t *testing.T) {
	scheme := NewLatticeBlobCommit()
	data := []byte("test data")

	commitment, err := scheme.Commit(data)
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	// Tamper with the proof.
	tampered := &PQBlobCommitment{
		Scheme:     commitment.Scheme,
		Commitment: commitment.Commitment,
		Proof:      make([]byte, len(commitment.Proof)),
	}
	copy(tampered.Proof, commitment.Proof)
	tampered.Proof[0] ^= 0xFF

	if scheme.Verify(tampered, data) {
		t.Error("tampered proof should not verify")
	}
}
