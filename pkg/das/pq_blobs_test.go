package das

import (
	"bytes"
	"testing"
)

func TestCommitBlob(t *testing.T) {
	data := []byte("hello post-quantum blob world, this is test data for commitments")

	commitment, err := CommitBlob(data)
	if err != nil {
		t.Fatalf("CommitBlob: unexpected error: %v", err)
	}
	if commitment == nil {
		t.Fatal("CommitBlob: returned nil commitment")
	}
	if commitment.DataSize != uint32(len(data)) {
		t.Errorf("DataSize = %d, want %d", commitment.DataSize, len(data))
	}
	expectedChunks := chunkCount(len(data))
	if commitment.NumChunks != uint32(expectedChunks) {
		t.Errorf("NumChunks = %d, want %d", commitment.NumChunks, expectedChunks)
	}

	// Digest should not be all zeros.
	allZero := true
	for _, b := range commitment.Digest {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("Digest is all zeros")
	}
}

func TestCommitBlobEmpty(t *testing.T) {
	_, err := CommitBlob(nil)
	if err != ErrPQBlobEmpty {
		t.Errorf("CommitBlob(nil) error = %v, want %v", err, ErrPQBlobEmpty)
	}
	_, err = CommitBlob([]byte{})
	if err != ErrPQBlobEmpty {
		t.Errorf("CommitBlob(empty) error = %v, want %v", err, ErrPQBlobEmpty)
	}
}

func TestCommitBlobTooLarge(t *testing.T) {
	data := make([]byte, MaxBlobSize+1)
	_, err := CommitBlob(data)
	if err != ErrPQBlobTooLarge {
		t.Errorf("CommitBlob(too large) error = %v, want %v", err, ErrPQBlobTooLarge)
	}
}

func TestCommitBlobDeterministic(t *testing.T) {
	data := []byte("determinism test data for lattice commitments")
	c1, err := CommitBlob(data)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := CommitBlob(data)
	if err != nil {
		t.Fatal(err)
	}
	if c1.Digest != c2.Digest {
		t.Error("CommitBlob is not deterministic: digests differ")
	}
}

func TestCommitBlobDifferentData(t *testing.T) {
	c1, _ := CommitBlob([]byte("data one"))
	c2, _ := CommitBlob([]byte("data two"))
	if c1.Digest == c2.Digest {
		t.Error("Different data produced same commitment digest")
	}
}

func TestVerifyBlobCommitment(t *testing.T) {
	data := []byte("verification test data for post-quantum blobs")
	commitment, err := CommitBlob(data)
	if err != nil {
		t.Fatal(err)
	}

	if !VerifyBlobCommitment(commitment, data) {
		t.Error("VerifyBlobCommitment returned false for valid commitment")
	}

	// Wrong data.
	if VerifyBlobCommitment(commitment, []byte("wrong data")) {
		t.Error("VerifyBlobCommitment returned true for wrong data")
	}

	// Nil commitment.
	if VerifyBlobCommitment(nil, data) {
		t.Error("VerifyBlobCommitment returned true for nil commitment")
	}

	// Empty data.
	if VerifyBlobCommitment(commitment, nil) {
		t.Error("VerifyBlobCommitment returned true for nil data")
	}
}

func TestGenerateBlobProof(t *testing.T) {
	data := bytes.Repeat([]byte("proof test data!"), 10) // 160 bytes, 5 chunks

	proof, err := GenerateBlobProof(data, 0)
	if err != nil {
		t.Fatalf("GenerateBlobProof: %v", err)
	}
	if proof == nil {
		t.Fatal("GenerateBlobProof returned nil proof")
	}
	if proof.ChunkIndex != 0 {
		t.Errorf("ChunkIndex = %d, want 0", proof.ChunkIndex)
	}

	// ChunkHash should not be zero.
	zeroHash := [32]byte{}
	if proof.ChunkHash == zeroHash {
		t.Error("ChunkHash is all zeros")
	}

	// LatticeWitness should not be zero.
	zeroWitness := [PQProofSize]byte{}
	if proof.LatticeWitness == zeroWitness {
		t.Error("LatticeWitness is all zeros")
	}
}

func TestGenerateBlobProofLastChunk(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 100) // 100 bytes, 4 chunks (ceil(100/32))
	numChunks := uint32(chunkCount(len(data)))

	proof, err := GenerateBlobProof(data, numChunks-1)
	if err != nil {
		t.Fatalf("GenerateBlobProof last chunk: %v", err)
	}
	if proof.ChunkIndex != numChunks-1 {
		t.Errorf("ChunkIndex = %d, want %d", proof.ChunkIndex, numChunks-1)
	}
}

func TestGenerateBlobProofOutOfBounds(t *testing.T) {
	data := []byte("short blob data")
	numChunks := uint32(chunkCount(len(data)))

	_, err := GenerateBlobProof(data, numChunks)
	if err != ErrPQBlobIndexOOB {
		t.Errorf("GenerateBlobProof(OOB) error = %v, want %v", err, ErrPQBlobIndexOOB)
	}

	_, err = GenerateBlobProof(data, 999)
	if err != ErrPQBlobIndexOOB {
		t.Errorf("GenerateBlobProof(999) error = %v, want %v", err, ErrPQBlobIndexOOB)
	}
}

func TestGenerateBlobProofEmpty(t *testing.T) {
	_, err := GenerateBlobProof(nil, 0)
	if err != ErrPQBlobEmpty {
		t.Errorf("GenerateBlobProof(nil) error = %v, want %v", err, ErrPQBlobEmpty)
	}
}

func TestVerifyBlobProof(t *testing.T) {
	data := bytes.Repeat([]byte("verify proof "), 8)
	commitment, _ := CommitBlob(data)

	proof, err := GenerateBlobProof(data, 1)
	if err != nil {
		t.Fatal(err)
	}

	if !VerifyBlobProof(proof, commitment) {
		t.Error("VerifyBlobProof returned false for valid proof")
	}
}

func TestVerifyBlobProofInvalid(t *testing.T) {
	data := bytes.Repeat([]byte("invalid proof test "), 5)
	commitment, _ := CommitBlob(data)

	proof, _ := GenerateBlobProof(data, 0)

	// Tamper with witness.
	tampered := *proof
	tampered.LatticeWitness[0] ^= 0xff
	if VerifyBlobProof(&tampered, commitment) {
		t.Error("VerifyBlobProof returned true for tampered witness")
	}

	// Tamper with chunk hash.
	tampered2 := *proof
	tampered2.ChunkHash[0] ^= 0xff
	if VerifyBlobProof(&tampered2, commitment) {
		t.Error("VerifyBlobProof returned true for tampered chunk hash")
	}

	// Wrong commitment.
	otherData := []byte("other blob data for wrong commitment")
	otherCommitment, _ := CommitBlob(otherData)
	if VerifyBlobProof(proof, otherCommitment) {
		t.Error("VerifyBlobProof returned true for wrong commitment")
	}

	// Nil cases.
	if VerifyBlobProof(nil, commitment) {
		t.Error("VerifyBlobProof(nil proof)")
	}
	if VerifyBlobProof(proof, nil) {
		t.Error("VerifyBlobProof(nil commitment)")
	}
}

func TestVerifyBlobProofChunkIndexOOB(t *testing.T) {
	data := []byte("small blob")
	commitment, _ := CommitBlob(data)
	proof, _ := GenerateBlobProof(data, 0)

	// Force invalid chunk index in proof.
	tampered := *proof
	tampered.ChunkIndex = commitment.NumChunks + 10
	if VerifyBlobProof(&tampered, commitment) {
		t.Error("VerifyBlobProof returned true for out-of-bounds chunk index")
	}
}

func TestBatchVerifyProofs(t *testing.T) {
	data := bytes.Repeat([]byte("batch verify test!!! "), 10) // 210 bytes
	commitment, _ := CommitBlob(data)
	numChunks := chunkCount(len(data))

	proofs := make([]*PQBlobProof, numChunks)
	commitments := make([]*PQBlobCommitment, numChunks)
	for i := 0; i < numChunks; i++ {
		p, err := GenerateBlobProof(data, uint32(i))
		if err != nil {
			t.Fatalf("GenerateBlobProof(%d): %v", i, err)
		}
		proofs[i] = p
		commitments[i] = commitment
	}

	if !BatchVerifyProofs(proofs, commitments) {
		t.Error("BatchVerifyProofs returned false for all valid proofs")
	}
}

func TestBatchVerifyProofsOneBad(t *testing.T) {
	data := bytes.Repeat([]byte("batch one bad test!!"), 6)
	commitment, _ := CommitBlob(data)
	numChunks := chunkCount(len(data))

	proofs := make([]*PQBlobProof, numChunks)
	commitments := make([]*PQBlobCommitment, numChunks)
	for i := 0; i < numChunks; i++ {
		p, _ := GenerateBlobProof(data, uint32(i))
		proofs[i] = p
		commitments[i] = commitment
	}

	// Tamper with one proof.
	proofs[numChunks/2].LatticeWitness[10] ^= 0xff
	if BatchVerifyProofs(proofs, commitments) {
		t.Error("BatchVerifyProofs returned true with one tampered proof")
	}
}

func TestBatchVerifyProofsEmpty(t *testing.T) {
	if BatchVerifyProofs(nil, nil) {
		t.Error("BatchVerifyProofs(nil, nil) should return false")
	}
	if BatchVerifyProofs([]*PQBlobProof{}, []*PQBlobCommitment{}) {
		t.Error("BatchVerifyProofs(empty, empty) should return false")
	}
}

func TestBatchVerifyProofsLengthMismatch(t *testing.T) {
	data := []byte("mismatch test data for batch verification")
	commitment, _ := CommitBlob(data)
	proof, _ := GenerateBlobProof(data, 0)

	if BatchVerifyProofs([]*PQBlobProof{proof}, []*PQBlobCommitment{commitment, commitment}) {
		t.Error("BatchVerifyProofs should return false for length mismatch")
	}
}

func TestBatchVerifyProofsParallel(t *testing.T) {
	// Use > 4 proofs to trigger parallel verification.
	data := bytes.Repeat([]byte("parallel batch verification data!"), 20) // 640 bytes, 20 chunks
	commitment, _ := CommitBlob(data)
	numChunks := chunkCount(len(data))

	proofs := make([]*PQBlobProof, numChunks)
	commitments := make([]*PQBlobCommitment, numChunks)
	for i := 0; i < numChunks; i++ {
		p, err := GenerateBlobProof(data, uint32(i))
		if err != nil {
			t.Fatalf("GenerateBlobProof(%d): %v", i, err)
		}
		proofs[i] = p
		commitments[i] = commitment
	}

	if !BatchVerifyProofs(proofs, commitments) {
		t.Error("BatchVerifyProofs (parallel path) returned false for valid proofs")
	}
}

func TestChunkCount(t *testing.T) {
	tests := []struct {
		dataLen int
		want    int
	}{
		{0, 0},
		{1, 1},
		{31, 1},
		{32, 1},
		{33, 2},
		{64, 2},
		{65, 3},
		{100, 4},
		{128, 4},
	}
	for _, tt := range tests {
		got := chunkCount(tt.dataLen)
		if got != tt.want {
			t.Errorf("chunkCount(%d) = %d, want %d", tt.dataLen, got, tt.want)
		}
	}
}

func TestExtractChunk(t *testing.T) {
	data := []byte("0123456789abcdef0123456789ABCDEFEXTRA") // 37 bytes

	// First chunk: first 32 bytes.
	c0 := extractChunk(data, 0)
	if len(c0) != ChunkSize {
		t.Errorf("chunk 0 len = %d, want %d", len(c0), ChunkSize)
	}
	if !bytes.Equal(c0, data[:32]) {
		t.Error("chunk 0 content mismatch")
	}

	// Second chunk: 5 bytes + padding.
	c1 := extractChunk(data, 1)
	if len(c1) != ChunkSize {
		t.Errorf("chunk 1 len = %d, want %d", len(c1), ChunkSize)
	}
	if !bytes.Equal(c1[:5], data[32:]) {
		t.Error("chunk 1 first 5 bytes mismatch")
	}
	for i := 5; i < ChunkSize; i++ {
		if c1[i] != 0 {
			t.Errorf("chunk 1 byte %d = %d, want 0 (padding)", i, c1[i])
			break
		}
	}

	// Out of range chunk: all zeros.
	c99 := extractChunk(data, 99)
	for i := range c99 {
		if c99[i] != 0 {
			t.Error("out-of-range chunk should be all zeros")
			break
		}
	}
}

func TestCommitBlobMaxSize(t *testing.T) {
	data := make([]byte, MaxBlobSize)
	for i := range data {
		data[i] = byte(i % 256)
	}

	commitment, err := CommitBlob(data)
	if err != nil {
		t.Fatalf("CommitBlob(MaxBlobSize): %v", err)
	}
	if commitment.DataSize != uint32(MaxBlobSize) {
		t.Errorf("DataSize = %d, want %d", commitment.DataSize, MaxBlobSize)
	}
	if !VerifyBlobCommitment(commitment, data) {
		t.Error("VerifyBlobCommitment failed for max-size blob")
	}
}

func TestLatticeHashCombine(t *testing.T) {
	a := make([]byte, 32)
	b := make([]byte, 32)
	for i := range a {
		a[i] = byte(i)
		b[i] = byte(32 - i)
	}

	result := latticeHashCombine(a, b)
	if len(result) != 32 {
		t.Errorf("latticeHashCombine result len = %d, want 32", len(result))
	}

	// Verify determinism.
	result2 := latticeHashCombine(a, b)
	if !bytes.Equal(result, result2) {
		t.Error("latticeHashCombine is not deterministic")
	}

	// Different inputs -> different output.
	c := make([]byte, 32)
	c[0] = 0xff
	result3 := latticeHashCombine(a, c)
	if bytes.Equal(result, result3) {
		t.Error("latticeHashCombine produced same result for different inputs")
	}
}

func TestProofDifferentChunks(t *testing.T) {
	data := bytes.Repeat([]byte("different chunk proof data "), 4) // multiple chunks

	p0, _ := GenerateBlobProof(data, 0)
	p1, _ := GenerateBlobProof(data, 1)

	if p0.ChunkHash == p1.ChunkHash {
		// Only fail if the underlying data is actually different.
		c0 := extractChunk(data, 0)
		c1 := extractChunk(data, 1)
		if !bytes.Equal(c0, c1) {
			t.Error("Different chunks produced same ChunkHash")
		}
	}

	if p0.LatticeWitness == p1.LatticeWitness {
		t.Error("Different chunk indices produced same LatticeWitness")
	}
}
