package engine

import (
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeCommitment creates a 48-byte commitment with a unique pattern.
func makeCommitment(seed byte) []byte {
	c := make([]byte, KZGCommitmentSize)
	for i := range c {
		c[i] = seed + byte(i)
	}
	return c
}

func TestComputeVersionedHash_Valid(t *testing.T) {
	commitment := makeCommitment(0x10)
	h, err := ComputeVersionedHash(commitment, VersionKZG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify version byte.
	if h[0] != VersionKZG {
		t.Errorf("version byte: got 0x%02x, want 0x%02x", h[0], VersionKZG)
	}

	// Verify against manual computation.
	expected := sha256.Sum256(commitment)
	expected[0] = VersionKZG
	if h != types.Hash(expected) {
		t.Error("hash does not match manual SHA-256 computation")
	}
}

func TestComputeVersionedHash_NilCommitment(t *testing.T) {
	_, err := ComputeVersionedHash(nil, VersionKZG)
	if !errors.Is(err, ErrVHCommitmentNil) {
		t.Fatalf("expected ErrVHCommitmentNil, got %v", err)
	}
}

func TestComputeVersionedHash_BadSize(t *testing.T) {
	_, err := ComputeVersionedHash([]byte{1, 2, 3}, VersionKZG)
	if !errors.Is(err, ErrVHCommitmentSize) {
		t.Fatalf("expected ErrVHCommitmentSize, got %v", err)
	}
}

func TestComputeVersionedHash_DifferentVersions(t *testing.T) {
	commitment := makeCommitment(0x20)

	h1, _ := ComputeVersionedHash(commitment, VersionKZG)
	h2, _ := ComputeVersionedHash(commitment, VersionFuture)

	if h1 == h2 {
		t.Error("different version bytes should produce different hashes")
	}
	if h1[0] != VersionKZG {
		t.Errorf("h1 version byte: got 0x%02x", h1[0])
	}
	if h2[0] != VersionFuture {
		t.Errorf("h2 version byte: got 0x%02x", h2[0])
	}

	// Bytes 1..31 should be the same.
	for i := 1; i < 32; i++ {
		if h1[i] != h2[i] {
			t.Errorf("byte %d differs between versions", i)
			break
		}
	}
}

func TestComputeVersionedHashKZG(t *testing.T) {
	commitment := makeCommitment(0x30)
	h, err := ComputeVersionedHashKZG(commitment)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h[0] != VersionKZG {
		t.Errorf("version byte: got 0x%02x", h[0])
	}
}

func TestBatchComputeVersionedHashes(t *testing.T) {
	commitments := [][]byte{
		makeCommitment(0x01),
		makeCommitment(0x02),
		makeCommitment(0x03),
	}

	hashes, err := BatchComputeVersionedHashes(commitments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hashes) != 3 {
		t.Fatalf("expected 3 hashes, got %d", len(hashes))
	}

	for i, h := range hashes {
		if h[0] != VersionKZG {
			t.Errorf("hash %d: version byte 0x%02x", i, h[0])
		}
	}
}

func TestBatchComputeVersionedHashes_Empty(t *testing.T) {
	hashes, err := BatchComputeVersionedHashes(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hashes != nil {
		t.Error("expected nil for empty input")
	}
}

func TestBatchComputeVersionedHashes_InvalidCommitment(t *testing.T) {
	commitments := [][]byte{
		makeCommitment(0x01),
		{1, 2, 3}, // bad size
	}

	_, err := BatchComputeVersionedHashes(commitments)
	if err == nil {
		t.Fatal("expected error for invalid commitment")
	}
}

func TestVerifyVersionedHashesAgainstCommitments_Valid(t *testing.T) {
	commitments := [][]byte{
		makeCommitment(0x10),
		makeCommitment(0x20),
	}

	hashes, _ := BatchComputeVersionedHashes(commitments)
	err := VerifyVersionedHashesAgainstCommitments(hashes, commitments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyVersionedHashesAgainstCommitments_CountMismatch(t *testing.T) {
	commitments := [][]byte{makeCommitment(0x10)}
	hashes := []types.Hash{{}, {}} // 2 hashes, 1 commitment

	err := VerifyVersionedHashesAgainstCommitments(hashes, commitments)
	if !errors.Is(err, ErrVHCountMismatch) {
		t.Fatalf("expected ErrVHCountMismatch, got %v", err)
	}
}

func TestVerifyVersionedHashesAgainstCommitments_HashMismatch(t *testing.T) {
	commitments := [][]byte{makeCommitment(0x10)}
	wrongHash := types.Hash{0x01, 0xFF} // wrong hash
	hashes := []types.Hash{wrongHash}

	err := VerifyVersionedHashesAgainstCommitments(hashes, commitments)
	if !errors.Is(err, ErrVHHashMismatch) {
		t.Fatalf("expected ErrVHHashMismatch, got %v", err)
	}
}

func TestExtractVersionByte(t *testing.T) {
	h := types.Hash{0x01}
	if v := ExtractVersionByte(h); v != 0x01 {
		t.Errorf("expected 0x01, got 0x%02x", v)
	}
}

func TestIsKZGVersionedHash(t *testing.T) {
	kzgHash := types.Hash{VersionKZG}
	if !IsKZGVersionedHash(kzgHash) {
		t.Error("expected true for KZG version hash")
	}

	otherHash := types.Hash{0x00}
	if IsKZGVersionedHash(otherHash) {
		t.Error("expected false for non-KZG version hash")
	}
}

func TestValidateVersionByte(t *testing.T) {
	h := types.Hash{VersionKZG}
	if err := ValidateVersionByte(h, VersionKZG); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err := ValidateVersionByte(h, VersionFuture)
	if !errors.Is(err, ErrVHInvalidVersion) {
		t.Fatalf("expected ErrVHInvalidVersion, got %v", err)
	}
}

func TestBuildCommitmentHashMap(t *testing.T) {
	c1 := makeCommitment(0x10)
	c2 := makeCommitment(0x20)

	m, err := BuildCommitmentHashMap([][]byte{c1, c2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}

	h1, _ := ComputeVersionedHashKZG(c1)
	if _, ok := m[h1]; !ok {
		t.Error("expected commitment 1 in map")
	}
}

func TestBuildCommitmentHashMap_InvalidCommitment(t *testing.T) {
	_, err := BuildCommitmentHashMap([][]byte{
		makeCommitment(0x01),
		{1, 2}, // bad size
	})
	if err == nil {
		t.Fatal("expected error for invalid commitment")
	}
}

func TestCollectBlobHashesFromTransactions_Empty(t *testing.T) {
	hashes := CollectBlobHashesFromTransactions(nil)
	if len(hashes) != 0 {
		t.Errorf("expected 0 hashes from nil txs, got %d", len(hashes))
	}
}

func TestVerifyAllBlobVersionBytes_Valid(t *testing.T) {
	hashes := []types.Hash{
		{VersionKZG, 0xAA},
		{VersionKZG, 0xBB},
	}
	if err := VerifyAllBlobVersionBytes(hashes); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyAllBlobVersionBytes_Empty(t *testing.T) {
	if err := VerifyAllBlobVersionBytes(nil); err != nil {
		t.Fatalf("unexpected error for nil: %v", err)
	}
}

func TestVerifyAllBlobVersionBytes_Invalid(t *testing.T) {
	hashes := []types.Hash{
		{VersionKZG, 0xAA},
		{0x00, 0xBB}, // wrong version
	}
	err := VerifyAllBlobVersionBytes(hashes)
	if !errors.Is(err, ErrVHInvalidVersion) {
		t.Fatalf("expected ErrVHInvalidVersion, got %v", err)
	}
}

func TestComputeVersionedHash_Deterministic(t *testing.T) {
	commitment := makeCommitment(0x42)
	h1, _ := ComputeVersionedHashKZG(commitment)
	h2, _ := ComputeVersionedHashKZG(commitment)
	if h1 != h2 {
		t.Error("versioned hash should be deterministic")
	}
}

func TestVersionConstants(t *testing.T) {
	if VersionKZG != 0x01 {
		t.Errorf("VersionKZG: got 0x%02x, want 0x01", VersionKZG)
	}
	if VersionFuture != 0x02 {
		t.Errorf("VersionFuture: got 0x%02x, want 0x02", VersionFuture)
	}
	if VersionedHashLen != 32 {
		t.Errorf("VersionedHashLen: got %d, want 32", VersionedHashLen)
	}
}
