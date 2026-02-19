package pqc

import (
	"bytes"
	"testing"
)

// --- PQCommitmentScheme construction ---

func TestNewPQCommitmentScheme_ValidLevels(t *testing.T) {
	for _, level := range []int{128, 192, 256} {
		s := NewPQCommitmentScheme(level)
		if s == nil {
			t.Fatalf("NewPQCommitmentScheme(%d) returned nil", level)
		}
		if s.SecurityLevel() != level {
			t.Errorf("SecurityLevel() = %d, want %d", s.SecurityLevel(), level)
		}
	}
}

func TestNewPQCommitmentScheme_InvalidLevel(t *testing.T) {
	for _, level := range []int{0, 64, 100, 160, 384, -1} {
		s := NewPQCommitmentScheme(level)
		if s != nil {
			t.Errorf("NewPQCommitmentScheme(%d) should return nil", level)
		}
	}
}

// --- Commit ---

func TestCommit_Basic(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob := []byte("hello world, this is a test blob for PQ commitment")

	c, err := s.Commit(blob)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if c.Scheme != SchemeMerklePQCommit {
		t.Errorf("Scheme = %q, want %q", c.Scheme, SchemeMerklePQCommit)
	}
	if len(c.Commitment) != 32 {
		t.Errorf("Commitment length = %d, want 32", len(c.Commitment))
	}
	if len(c.Proof) != 32 {
		t.Errorf("Proof length = %d, want 32", len(c.Proof))
	}
}

func TestCommit_NilBlob(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	_, err := s.Commit(nil)
	if err != ErrBlobNil {
		t.Errorf("expected ErrBlobNil, got %v", err)
	}
}

func TestCommit_EmptyBlob(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	_, err := s.Commit([]byte{})
	if err != ErrBlobEmpty {
		t.Errorf("expected ErrBlobEmpty, got %v", err)
	}
}

func TestCommit_Deterministic(t *testing.T) {
	s := NewPQCommitmentScheme(256)
	blob := []byte("deterministic blob test data 12345")

	c1, _ := s.Commit(blob)
	c2, _ := s.Commit(blob)

	if !bytes.Equal(c1.Commitment, c2.Commitment) {
		t.Error("commitments for same blob should be identical")
	}
	if !bytes.Equal(c1.Proof, c2.Proof) {
		t.Error("proofs for same blob should be identical")
	}
}

func TestCommit_DifferentData(t *testing.T) {
	s := NewPQCommitmentScheme(128)

	c1, _ := s.Commit([]byte("blob A"))
	c2, _ := s.Commit([]byte("blob B"))

	if bytes.Equal(c1.Commitment, c2.Commitment) {
		t.Error("different blobs should produce different commitments")
	}
}

func TestCommit_DifferentSecurityLevels(t *testing.T) {
	blob := []byte("same blob for all security levels, padded to be long enough")

	s128 := NewPQCommitmentScheme(128)
	s192 := NewPQCommitmentScheme(192)
	s256 := NewPQCommitmentScheme(256)

	c128, _ := s128.Commit(blob)
	c192, _ := s192.Commit(blob)
	c256, _ := s256.Commit(blob)

	// Different security levels should produce different commitments because
	// the number of hash rounds differs.
	if bytes.Equal(c128.Commitment, c192.Commitment) {
		t.Error("128 and 192 should differ")
	}
	if bytes.Equal(c192.Commitment, c256.Commitment) {
		t.Error("192 and 256 should differ")
	}
	if bytes.Equal(c128.Commitment, c256.Commitment) {
		t.Error("128 and 256 should differ")
	}
}

// --- Verify ---

func TestVerify_Valid(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob := []byte("verify this blob data")

	c, err := s.Commit(blob)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if !s.Verify(c, blob) {
		t.Error("valid commitment should verify")
	}
}

func TestVerify_WrongData(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	c, _ := s.Commit([]byte("original"))

	if s.Verify(c, []byte("tampered")) {
		t.Error("tampered data should not verify")
	}
}

func TestVerify_NilCommitment(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	if s.Verify(nil, []byte("data")) {
		t.Error("nil commitment should not verify")
	}
}

func TestVerify_NilData(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	c, _ := s.Commit([]byte("data"))
	if s.Verify(c, nil) {
		t.Error("nil data should not verify")
	}
}

func TestVerify_EmptyData(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	c, _ := s.Commit([]byte("data"))
	if s.Verify(c, []byte{}) {
		t.Error("empty data should not verify")
	}
}

func TestVerify_WrongScheme(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	c, _ := s.Commit([]byte("data"))
	c.Scheme = "wrong-scheme"
	if s.Verify(c, []byte("data")) {
		t.Error("wrong scheme should not verify")
	}
}

func TestVerify_TamperedCommitment(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob := []byte("tamper test")
	c, _ := s.Commit(blob)

	tampered := &PQBlobCommitment{
		Scheme:     c.Scheme,
		Commitment: make([]byte, 32),
		Proof:      c.Proof,
	}
	copy(tampered.Commitment, c.Commitment)
	tampered.Commitment[0] ^= 0xFF

	if s.Verify(tampered, blob) {
		t.Error("tampered commitment bytes should not verify")
	}
}

func TestVerify_TamperedProof(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob := []byte("tamper proof test")
	c, _ := s.Commit(blob)

	tampered := &PQBlobCommitment{
		Scheme:     c.Scheme,
		Commitment: c.Commitment,
		Proof:      make([]byte, 32),
	}
	copy(tampered.Proof, c.Proof)
	tampered.Proof[0] ^= 0xFF

	if s.Verify(tampered, blob) {
		t.Error("tampered proof should not verify")
	}
}

func TestVerify_AllSecurityLevels(t *testing.T) {
	blob := []byte("verify across all security levels with enough data padding")
	for _, level := range []int{128, 192, 256} {
		s := NewPQCommitmentScheme(level)
		c, err := s.Commit(blob)
		if err != nil {
			t.Fatalf("level %d: commit failed: %v", level, err)
		}
		if !s.Verify(c, blob) {
			t.Errorf("level %d: verification failed", level)
		}
	}
}

// --- Batch Commit/Verify ---

func TestBatchCommit(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blobs := [][]byte{
		[]byte("blob zero"),
		[]byte("blob one with more data"),
		[]byte("blob two"),
	}

	commitments, err := s.BatchCommit(blobs)
	if err != nil {
		t.Fatalf("BatchCommit failed: %v", err)
	}
	if len(commitments) != 3 {
		t.Fatalf("expected 3 commitments, got %d", len(commitments))
	}

	// Each commitment should verify individually.
	for i, c := range commitments {
		if !s.Verify(c, blobs[i]) {
			t.Errorf("blob %d: verify failed", i)
		}
	}
}

func TestBatchCommit_Empty(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	_, err := s.BatchCommit(nil)
	if err != ErrBatchEmpty {
		t.Errorf("expected ErrBatchEmpty, got %v", err)
	}
	_, err = s.BatchCommit([][]byte{})
	if err != ErrBatchEmpty {
		t.Errorf("expected ErrBatchEmpty for empty slice, got %v", err)
	}
}

func TestBatchCommit_PropagatesError(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blobs := [][]byte{
		[]byte("valid"),
		nil, // triggers ErrBlobNil
	}
	_, err := s.BatchCommit(blobs)
	if err != ErrBlobNil {
		t.Errorf("expected ErrBlobNil, got %v", err)
	}
}

func TestBatchVerify(t *testing.T) {
	s := NewPQCommitmentScheme(192)
	blobs := [][]byte{
		[]byte("batch verify blob 1"),
		[]byte("batch verify blob 2 with extra data"),
		[]byte("batch verify blob 3"),
	}

	commitments, _ := s.BatchCommit(blobs)
	if !s.BatchVerify(commitments, blobs) {
		t.Error("batch verify should pass for matching commitments/blobs")
	}
}

func TestBatchVerify_LengthMismatch(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blobs := [][]byte{[]byte("a"), []byte("b")}
	commitments, _ := s.BatchCommit(blobs)

	if s.BatchVerify(commitments, [][]byte{[]byte("a")}) {
		t.Error("batch verify should fail with mismatched lengths")
	}
}

func TestBatchVerify_Empty(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	if s.BatchVerify(nil, nil) {
		t.Error("batch verify should fail for nil slices")
	}
	if s.BatchVerify([]*PQBlobCommitment{}, [][]byte{}) {
		t.Error("batch verify should fail for empty slices")
	}
}

func TestBatchVerify_OneWrong(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blobs := [][]byte{
		[]byte("batch 1"),
		[]byte("batch 2"),
		[]byte("batch 3"),
	}
	commitments, _ := s.BatchCommit(blobs)

	// Tamper with one blob.
	tampered := make([][]byte, 3)
	copy(tampered, blobs)
	tampered[1] = []byte("TAMPERED")

	if s.BatchVerify(commitments, tampered) {
		t.Error("batch verify should fail when one blob is tampered")
	}
}

// --- OpenAt / VerifyOpening ---

func TestOpenAt_Basic(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob := bytes.Repeat([]byte{0xAB}, 128) // 4 chunks of 32 bytes

	for idx := uint64(0); idx < 4; idx++ {
		opening, err := s.OpenAt(blob, idx)
		if err != nil {
			t.Fatalf("OpenAt(%d) failed: %v", idx, err)
		}
		if opening.Index != idx {
			t.Errorf("opening.Index = %d, want %d", opening.Index, idx)
		}
		if len(opening.Value) != ChunkSize {
			t.Errorf("opening.Value length = %d, want %d", len(opening.Value), ChunkSize)
		}
		if len(opening.MerklePath) == 0 {
			t.Errorf("MerklePath should not be empty for multi-chunk blob")
		}
		if len(opening.Root) != 32 {
			t.Errorf("Root length = %d, want 32", len(opening.Root))
		}
	}
}

func TestOpenAt_NilBlob(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	_, err := s.OpenAt(nil, 0)
	if err != ErrBlobNil {
		t.Errorf("expected ErrBlobNil, got %v", err)
	}
}

func TestOpenAt_EmptyBlob(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	_, err := s.OpenAt([]byte{}, 0)
	if err != ErrBlobEmpty {
		t.Errorf("expected ErrBlobEmpty, got %v", err)
	}
}

func TestOpenAt_IndexOutOfRange(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob := []byte("short") // 1 chunk
	_, err := s.OpenAt(blob, 1)
	if err != ErrIndexOutOfRange {
		t.Errorf("expected ErrIndexOutOfRange, got %v", err)
	}
}

func TestVerifyOpening_Valid(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob := bytes.Repeat([]byte{0xCD}, 96) // 3 chunks -> padded to 4

	c, _ := s.Commit(blob)

	for idx := uint64(0); idx < 3; idx++ {
		opening, err := s.OpenAt(blob, idx)
		if err != nil {
			t.Fatalf("OpenAt(%d) failed: %v", idx, err)
		}
		if !s.VerifyOpening(c, opening, idx) {
			t.Errorf("VerifyOpening(%d) should succeed", idx)
		}
	}
}

func TestVerifyOpening_WrongIndex(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob := bytes.Repeat([]byte{0xEF}, 64) // 2 chunks

	c, _ := s.Commit(blob)
	opening, _ := s.OpenAt(blob, 0)

	if s.VerifyOpening(c, opening, 1) {
		t.Error("VerifyOpening with wrong index should fail")
	}
}

func TestVerifyOpening_NilCommitment(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob := []byte("data for nil test")
	opening, _ := s.OpenAt(blob, 0)

	if s.VerifyOpening(nil, opening, 0) {
		t.Error("VerifyOpening with nil commitment should fail")
	}
}

func TestVerifyOpening_NilOpening(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob := []byte("data for nil opening test")
	c, _ := s.Commit(blob)

	if s.VerifyOpening(c, nil, 0) {
		t.Error("VerifyOpening with nil opening should fail")
	}
}

func TestVerifyOpening_TamperedValue(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob := bytes.Repeat([]byte{0x11}, 64) // 2 chunks

	c, _ := s.Commit(blob)
	opening, _ := s.OpenAt(blob, 0)

	// Tamper with the value.
	opening.Value[0] ^= 0xFF

	if s.VerifyOpening(c, opening, 0) {
		t.Error("VerifyOpening with tampered value should fail")
	}
}

func TestVerifyOpening_WrongCommitment(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob1 := bytes.Repeat([]byte{0x22}, 64)
	blob2 := bytes.Repeat([]byte{0x33}, 64)

	c1, _ := s.Commit(blob1)
	opening2, _ := s.OpenAt(blob2, 0)

	if s.VerifyOpening(c1, opening2, 0) {
		t.Error("VerifyOpening with wrong commitment should fail")
	}
}

// --- Large blob ---

func TestCommitVerify_LargeBlob(t *testing.T) {
	s := NewPQCommitmentScheme(256)

	// 128 KiB blob (4096 chunks).
	blob := make([]byte, 128*1024)
	for i := range blob {
		blob[i] = byte(i % 256)
	}

	c, err := s.Commit(blob)
	if err != nil {
		t.Fatalf("Commit on large blob failed: %v", err)
	}
	if !s.Verify(c, blob) {
		t.Error("verification of large blob commitment failed")
	}

	// Open at a few positions.
	for _, idx := range []uint64{0, 100, 2048, 4095} {
		opening, err := s.OpenAt(blob, idx)
		if err != nil {
			t.Fatalf("OpenAt(%d) failed: %v", idx, err)
		}
		if !s.VerifyOpening(c, opening, idx) {
			t.Errorf("VerifyOpening(%d) failed on large blob", idx)
		}
	}
}

// --- Single-byte blob ---

func TestCommitVerify_SingleByte(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	blob := []byte{0x42}

	c, err := s.Commit(blob)
	if err != nil {
		t.Fatalf("Commit on single-byte blob failed: %v", err)
	}
	if !s.Verify(c, blob) {
		t.Error("single-byte blob should verify")
	}

	opening, err := s.OpenAt(blob, 0)
	if err != nil {
		t.Fatalf("OpenAt(0) failed: %v", err)
	}
	if !s.VerifyOpening(c, opening, 0) {
		t.Error("opening for single-byte blob should verify")
	}
}

// --- Exact chunk boundary ---

func TestCommitVerify_ExactChunkBoundary(t *testing.T) {
	s := NewPQCommitmentScheme(128)
	// Exactly 2 chunks (64 bytes), no padding.
	blob := bytes.Repeat([]byte{0x77}, 64)

	c, err := s.Commit(blob)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if !s.Verify(c, blob) {
		t.Error("exact chunk boundary blob should verify")
	}
}

// --- splitChunks helper ---

func TestSplitChunks(t *testing.T) {
	blob := make([]byte, 65) // 2 full chunks + 1 partial
	for i := range blob {
		blob[i] = byte(i)
	}
	chunks := splitChunks(blob)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	// First chunk: bytes 0..31.
	for i := 0; i < 32; i++ {
		if chunks[0][i] != byte(i) {
			t.Errorf("chunk[0][%d] = %d, want %d", i, chunks[0][i], byte(i))
			break
		}
	}
	// Third chunk: byte 64, rest zero-padded.
	if chunks[2][0] != 64 {
		t.Errorf("chunk[2][0] = %d, want 64", chunks[2][0])
	}
	for i := 1; i < 32; i++ {
		if chunks[2][i] != 0 {
			t.Errorf("chunk[2][%d] = %d, want 0 (zero padding)", i, chunks[2][i])
			break
		}
	}
}

// --- nextPow2 ---

func TestNextPow2(t *testing.T) {
	tests := []struct {
		n    int
		want int
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 4},
		{4, 4},
		{5, 8},
		{7, 8},
		{8, 8},
		{9, 16},
		{1023, 1024},
		{1024, 1024},
		{1025, 2048},
	}
	for _, tt := range tests {
		got := nextPow2(tt.n)
		if got != tt.want {
			t.Errorf("nextPow2(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}
