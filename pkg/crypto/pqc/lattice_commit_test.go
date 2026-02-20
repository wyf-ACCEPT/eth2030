package pqc

import (
	"bytes"
	"testing"
)

func newMLWETestScheme() *LatticeCommitScheme {
	var seed [32]byte
	seed[0] = 0xAA
	seed[1] = 0xBB
	return NewLatticeCommitScheme(seed)
}

func TestMLWECommit_Name(t *testing.T) {
	lcs := newMLWETestScheme()
	if lcs.Name() != SchemeMLWECommit {
		t.Fatalf("expected %s, got %s", SchemeMLWECommit, lcs.Name())
	}
}

func TestMLWECommit_CommitNilData(t *testing.T) {
	lcs := newMLWETestScheme()
	_, _, err := lcs.Commit(nil)
	if err != ErrLatticeNilData {
		t.Fatalf("expected ErrLatticeNilData, got %v", err)
	}
}

func TestMLWECommit_CommitEmptyData(t *testing.T) {
	lcs := newMLWETestScheme()
	_, _, err := lcs.Commit([]byte{})
	if err != ErrLatticeEmptyData {
		t.Fatalf("expected ErrLatticeEmptyData, got %v", err)
	}
}

func TestMLWECommit_CommitAndVerify(t *testing.T) {
	lcs := newMLWETestScheme()
	data := []byte("hello lattice blob commitment")

	commitment, opening, err := lcs.Commit(data)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if commitment == nil {
		t.Fatal("commitment is nil")
	}
	if opening == nil {
		t.Fatal("opening is nil")
	}
	if commitment.Scheme != SchemeMLWECommit {
		t.Fatalf("wrong scheme: %s", commitment.Scheme)
	}

	// Verify.
	if !lcs.Verify(commitment, opening, data) {
		t.Fatal("valid commitment failed verification")
	}
}

func TestMLWECommit_VerifyRejectsWrongData(t *testing.T) {
	lcs := newMLWETestScheme()
	data := []byte("correct data")
	commitment, opening, _ := lcs.Commit(data)

	wrongData := []byte("wrong data")
	if lcs.Verify(commitment, opening, wrongData) {
		t.Fatal("wrong data should fail verification")
	}
}

func TestMLWECommit_VerifyRejectsNil(t *testing.T) {
	lcs := newMLWETestScheme()
	data := []byte("test")
	_, opening, _ := lcs.Commit(data)

	if lcs.Verify(nil, opening, data) {
		t.Fatal("nil commitment should fail")
	}
}

func TestMLWECommit_VerifyRejectsNilOpening(t *testing.T) {
	lcs := newMLWETestScheme()
	data := []byte("test")
	commitment, _, _ := lcs.Commit(data)

	if lcs.Verify(commitment, nil, data) {
		t.Fatal("nil opening should fail")
	}
}

func TestMLWECommit_VerifyRejectsNilData(t *testing.T) {
	lcs := newMLWETestScheme()
	data := []byte("test")
	commitment, opening, _ := lcs.Commit(data)

	if lcs.Verify(commitment, opening, nil) {
		t.Fatal("nil data should fail")
	}
}

func TestMLWECommit_VerifyRejectsWrongScheme(t *testing.T) {
	lcs := newMLWETestScheme()
	data := []byte("test")
	commitment, opening, _ := lcs.Commit(data)
	commitment.Scheme = "wrong-scheme"

	if lcs.Verify(commitment, opening, data) {
		t.Fatal("wrong scheme should fail")
	}
}

func TestMLWECommit_DeterministicCommitment(t *testing.T) {
	lcs := newMLWETestScheme()
	data := []byte("deterministic test data")

	c1, _, _ := lcs.Commit(data)
	c2, _, _ := lcs.Commit(data)

	if !bytes.Equal(c1.Hash[:], c2.Hash[:]) {
		t.Fatal("same data should produce same commitment hash")
	}
}

func TestMLWECommit_DifferentDataDifferentCommitments(t *testing.T) {
	lcs := newMLWETestScheme()
	c1, _, _ := lcs.Commit([]byte("data1"))
	c2, _, _ := lcs.Commit([]byte("data2"))

	if bytes.Equal(c1.Hash[:], c2.Hash[:]) {
		t.Fatal("different data should produce different commitment hashes")
	}
}

func TestMLWECommit_OpenAndVerify(t *testing.T) {
	lcs := newMLWETestScheme()
	data := []byte("open-verify test")

	commitment, _, err := lcs.Commit(data)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	opening, err := lcs.Open(data, commitment)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if !lcs.Verify(commitment, opening, data) {
		t.Fatal("Open+Verify should succeed")
	}
}

func TestMLWECommit_OpenNilData(t *testing.T) {
	lcs := newMLWETestScheme()
	_, err := lcs.Open(nil, &LatticeCommitment{})
	if err != ErrLatticeNilData {
		t.Fatalf("expected ErrLatticeNilData, got %v", err)
	}
}

func TestMLWECommit_OpenEmptyData(t *testing.T) {
	lcs := newMLWETestScheme()
	_, err := lcs.Open([]byte{}, &LatticeCommitment{})
	if err != ErrLatticeEmptyData {
		t.Fatalf("expected ErrLatticeEmptyData, got %v", err)
	}
}

func TestMLWECommit_OpenNilCommitment(t *testing.T) {
	lcs := newMLWETestScheme()
	_, err := lcs.Open([]byte("test"), nil)
	if err != ErrLatticeNilCommit {
		t.Fatalf("expected ErrLatticeNilCommit, got %v", err)
	}
}

func TestMLWECommit_CommitToBlob(t *testing.T) {
	lcs := newMLWETestScheme()
	data := []byte("blob data for pq commit")

	pqc, err := lcs.CommitToBlob(data)
	if err != nil {
		t.Fatalf("CommitToBlob failed: %v", err)
	}
	if pqc.Scheme != SchemeMLWECommit {
		t.Fatalf("wrong scheme: %s", pqc.Scheme)
	}
	if len(pqc.Commitment) == 0 {
		t.Fatal("commitment should not be empty")
	}
	if len(pqc.Proof) == 0 {
		t.Fatal("proof should not be empty")
	}
}

func TestMLWECommit_VerifyBlob(t *testing.T) {
	lcs := newMLWETestScheme()
	data := []byte("verify blob test")

	pqc, _ := lcs.CommitToBlob(data)
	if !lcs.VerifyBlob(pqc, data) {
		t.Fatal("VerifyBlob should succeed")
	}
}

func TestMLWECommit_VerifyBlobRejectsWrongData(t *testing.T) {
	lcs := newMLWETestScheme()
	data := []byte("original blob")

	pqc, _ := lcs.CommitToBlob(data)
	if lcs.VerifyBlob(pqc, []byte("tampered blob")) {
		t.Fatal("wrong data should fail VerifyBlob")
	}
}

func TestMLWECommit_VerifyBlobRejectsNil(t *testing.T) {
	lcs := newMLWETestScheme()
	if lcs.VerifyBlob(nil, []byte("data")) {
		t.Fatal("nil commitment should fail")
	}
}

func TestMLWECommit_LargeBlob(t *testing.T) {
	lcs := newMLWETestScheme()
	data := make([]byte, 131072) // 128 KiB blob.
	for i := range data {
		data[i] = byte(i)
	}

	commitment, opening, err := lcs.Commit(data)
	if err != nil {
		t.Fatalf("Commit failed for large blob: %v", err)
	}
	if !lcs.Verify(commitment, opening, data) {
		t.Fatal("large blob verification failed")
	}
}

func TestMLWECommit_DifferentSeeds(t *testing.T) {
	var seed1, seed2 [32]byte
	seed1[0] = 0x01
	seed2[0] = 0x02

	lcs1 := NewLatticeCommitScheme(seed1)
	lcs2 := NewLatticeCommitScheme(seed2)

	data := []byte("same data")
	c1, _, _ := lcs1.Commit(data)
	c2, _, _ := lcs2.Commit(data)

	// Different schemes with different seeds produce different commitments.
	if bytes.Equal(c1.Hash[:], c2.Hash[:]) {
		t.Fatal("different seeds should produce different commitments")
	}
}

func TestMLWECommit_BindingProperty(t *testing.T) {
	// The binding property means that for a given commitment, there should
	// not be two different openings that verify. We test this by checking
	// that a commitment to data1 does not verify with data2's opening.
	lcs := newMLWETestScheme()
	data1 := []byte("data1 for binding test")
	data2 := []byte("data2 for binding test")

	commitment1, _, _ := lcs.Commit(data1)
	_, opening2, _ := lcs.Commit(data2)

	if lcs.Verify(commitment1, opening2, data2) {
		t.Fatal("binding violation: commitment to data1 verified with data2 opening")
	}
}
