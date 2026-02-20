package pqc

import (
	"testing"
)

func newBatchTestScheme() (*LatticeCommitScheme, *LatticeBatchVerifier) {
	var seed [32]byte
	seed[0] = 0xBE
	seed[1] = 0xEF
	scheme := NewLatticeCommitScheme(seed)
	verifier := NewLatticeBatchVerifier(scheme)
	return scheme, verifier
}

func TestBatchBlobVerify_SingleValid(t *testing.T) {
	scheme, verifier := newBatchTestScheme()
	data := []byte("single blob for batch verify")

	commit, opening, err := scheme.Commit(data)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	valid, err := verifier.BatchVerify(
		[]*LatticeCommitment{commit},
		[]*LatticeOpening{opening},
		[][]byte{data},
	)
	if err != nil {
		t.Fatalf("BatchVerify error: %v", err)
	}
	if !valid {
		t.Fatal("single valid commitment should pass batch verify")
	}
}

func TestBatchBlobVerify_MultipleValid(t *testing.T) {
	scheme, verifier := newBatchTestScheme()
	blobs := [][]byte{
		[]byte("blob number one"),
		[]byte("blob number two"),
		[]byte("blob number three"),
	}

	commitments := make([]*LatticeCommitment, len(blobs))
	openings := make([]*LatticeOpening, len(blobs))
	for i, b := range blobs {
		c, o, err := scheme.Commit(b)
		if err != nil {
			t.Fatalf("Commit %d failed: %v", i, err)
		}
		commitments[i] = c
		openings[i] = o
	}

	valid, err := verifier.BatchVerify(commitments, openings, blobs)
	if err != nil {
		t.Fatalf("BatchVerify error: %v", err)
	}
	if !valid {
		t.Fatal("all valid commitments should pass batch verify")
	}
}

func TestBatchBlobVerify_EmptyBatch(t *testing.T) {
	_, verifier := newBatchTestScheme()
	_, err := verifier.BatchVerify(nil, nil, nil)
	if err != ErrBatchVerifyEmpty {
		t.Fatalf("expected ErrBatchVerifyEmpty, got %v", err)
	}
}

func TestBatchBlobVerify_LengthMismatch(t *testing.T) {
	scheme, verifier := newBatchTestScheme()
	data := []byte("test")
	c, o, _ := scheme.Commit(data)

	_, err := verifier.BatchVerify(
		[]*LatticeCommitment{c, c},
		[]*LatticeOpening{o},
		[][]byte{data},
	)
	if err != ErrBatchVerifyMismatch {
		t.Fatalf("expected ErrBatchVerifyMismatch, got %v", err)
	}
}

func TestBatchBlobVerify_NilEntry(t *testing.T) {
	_, verifier := newBatchTestScheme()
	_, err := verifier.BatchVerify(
		[]*LatticeCommitment{nil},
		[]*LatticeOpening{nil},
		[][]byte{[]byte("data")},
	)
	if err != ErrBatchVerifyNilEntry {
		t.Fatalf("expected ErrBatchVerifyNilEntry, got %v", err)
	}
}

func TestBatchBlobVerify_WithIsolationAllValid(t *testing.T) {
	scheme, verifier := newBatchTestScheme()
	blobs := [][]byte{
		[]byte("isolation test blob 1"),
		[]byte("isolation test blob 2"),
	}

	commitments := make([]*LatticeCommitment, len(blobs))
	openings := make([]*LatticeOpening, len(blobs))
	for i, b := range blobs {
		c, o, _ := scheme.Commit(b)
		commitments[i] = c
		openings[i] = o
	}

	result, err := verifier.BatchVerifyWithIsolation(commitments, openings, blobs)
	if err != nil {
		t.Fatalf("BatchVerifyWithIsolation error: %v", err)
	}
	if !result.Valid {
		t.Fatal("all valid should pass")
	}
	if len(result.FailedIndices) != 0 {
		t.Fatal("no failures expected")
	}
}

func TestBatchBlobVerify_WithIsolationOneInvalid(t *testing.T) {
	scheme, verifier := newBatchTestScheme()
	data1 := []byte("valid blob data")
	data2 := []byte("another valid blob")

	c1, o1, _ := scheme.Commit(data1)
	c2, o2, _ := scheme.Commit(data2)

	// Tamper with the second commitment's vector to make it invalid.
	c2Tampered := &LatticeCommitment{
		Scheme:    c2.Scheme,
		CommitVec: c2.CommitVec,
		Hash:      c2.Hash,
	}
	// Modify a coefficient to break the commitment.
	c2Tampered.CommitVec[0] = c2.CommitVec[0].Add(NewPolyFromCoeffs([]int16{1}))

	result, err := verifier.BatchVerifyWithIsolation(
		[]*LatticeCommitment{c1, c2Tampered},
		[]*LatticeOpening{o1, o2},
		[][]byte{data1, data2},
	)
	if err != nil {
		t.Fatalf("BatchVerifyWithIsolation error: %v", err)
	}
	if result.Valid {
		t.Fatal("tampered batch should fail")
	}
	if len(result.FailedIndices) == 0 {
		t.Fatal("should identify at least one failure")
	}

	// The tampered index (1) should be in the failed list.
	found := false
	for _, idx := range result.FailedIndices {
		if idx == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected index 1 in failed indices, got %v", result.FailedIndices)
	}
}

func TestBatchBlobVerify_BatchVerifyBlobs(t *testing.T) {
	scheme, verifier := newBatchTestScheme()
	blobs := [][]byte{
		[]byte("pq blob 1"),
		[]byte("pq blob 2"),
	}

	pqCommits := make([]*PQBlobCommitment, len(blobs))
	for i, b := range blobs {
		c, err := scheme.CommitToBlob(b)
		if err != nil {
			t.Fatalf("CommitToBlob %d failed: %v", i, err)
		}
		pqCommits[i] = c
	}

	valid, err := verifier.BatchVerifyBlobs(pqCommits, blobs)
	if err != nil {
		t.Fatalf("BatchVerifyBlobs error: %v", err)
	}
	if !valid {
		t.Fatal("valid PQ blob batch should pass")
	}
}

func TestBatchBlobVerify_BatchVerifyBlobsWrongData(t *testing.T) {
	scheme, verifier := newBatchTestScheme()
	data := []byte("correct data")
	pqCommit, _ := scheme.CommitToBlob(data)

	valid, err := verifier.BatchVerifyBlobs(
		[]*PQBlobCommitment{pqCommit},
		[][]byte{[]byte("wrong data")},
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if valid {
		t.Fatal("wrong data should fail")
	}
}

func TestBatchBlobVerify_BatchVerifyBlobsEmpty(t *testing.T) {
	_, verifier := newBatchTestScheme()
	_, err := verifier.BatchVerifyBlobs(nil, nil)
	if err != ErrBatchVerifyEmpty {
		t.Fatalf("expected ErrBatchVerifyEmpty, got %v", err)
	}
}

func TestBatchBlobVerify_BatchVerifyBlobsMismatch(t *testing.T) {
	scheme, verifier := newBatchTestScheme()
	pqc, _ := scheme.CommitToBlob([]byte("test"))

	_, err := verifier.BatchVerifyBlobs(
		[]*PQBlobCommitment{pqc, pqc},
		[][]byte{[]byte("test")},
	)
	if err != ErrBatchVerifyMismatch {
		t.Fatalf("expected ErrBatchVerifyMismatch, got %v", err)
	}
}

func TestBatchBlobVerify_BatchVerifyBlobsNilEntry(t *testing.T) {
	_, verifier := newBatchTestScheme()
	_, err := verifier.BatchVerifyBlobs(
		[]*PQBlobCommitment{nil},
		[][]byte{[]byte("data")},
	)
	if err != ErrBatchVerifyNilEntry {
		t.Fatalf("expected ErrBatchVerifyNilEntry, got %v", err)
	}
}

func TestBatchBlobVerify_BlobsWithIsolation(t *testing.T) {
	scheme, verifier := newBatchTestScheme()
	blob1 := []byte("blob iso 1")
	blob2 := []byte("blob iso 2")

	pqc1, _ := scheme.CommitToBlob(blob1)
	pqc2, _ := scheme.CommitToBlob(blob2)

	// Tamper with pqc2.
	pqc2.Commitment[0] ^= 0xFF

	result, err := verifier.BatchVerifyBlobsWithIsolation(
		[]*PQBlobCommitment{pqc1, pqc2},
		[][]byte{blob1, blob2},
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Valid {
		t.Fatal("tampered batch should fail")
	}

	found := false
	for _, idx := range result.FailedIndices {
		if idx == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected index 1 in failed list, got %v", result.FailedIndices)
	}
}

func TestBatchBlobVerify_DeriveRandomScalars(t *testing.T) {
	scheme, _ := newBatchTestScheme()
	data := []byte("scalar test")
	c, _, _ := scheme.Commit(data)

	scalars := deriveRandomScalars([]*LatticeCommitment{c, c})
	if len(scalars) != 2 {
		t.Fatalf("expected 2 scalars, got %d", len(scalars))
	}
	// Scalars should be in [1, q).
	for i, s := range scalars {
		if s < 1 || s >= PolyQ {
			t.Fatalf("scalar %d out of range: %d", i, s)
		}
	}
}
