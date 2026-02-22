package das

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func defaultProver() *BlockBlobProver {
	return NewBlockBlobProver(DefaultBlockBlobProverConfig())
}

func TestBlockBlobEncodeBasic(t *testing.T) {
	prover := defaultProver()
	data := bytes.Repeat([]byte{0xAB}, 100)
	enc, err := prover.EncodeBlock(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc.OriginalSize != 100 {
		t.Fatalf("expected OriginalSize=100, got %d", enc.OriginalSize)
	}
	if enc.ChunkCount == 0 {
		t.Fatal("expected ChunkCount > 0")
	}
	if len(enc.EncodedChunks) != int(enc.ChunkCount) {
		t.Fatalf("chunk count mismatch: %d vs %d", len(enc.EncodedChunks), enc.ChunkCount)
	}
}

func TestBlockBlobEncodeEmpty(t *testing.T) {
	prover := defaultProver()
	_, err := prover.EncodeBlock(nil)
	if !errors.Is(err, ErrBlockBlobEncodingFailed) {
		t.Fatalf("expected ErrBlockBlobEncodingFailed, got %v", err)
	}
	_, err = prover.EncodeBlock([]byte{})
	if !errors.Is(err, ErrBlockBlobEncodingFailed) {
		t.Fatalf("expected ErrBlockBlobEncodingFailed for empty slice, got %v", err)
	}
}

func TestBlockBlobEncodeTooLarge(t *testing.T) {
	cfg := BlockBlobProverConfig{MaxBlockSize: 100, BlobFieldElementSize: 31, MaxBlobsPerBlock: 16}
	prover := NewBlockBlobProver(cfg)
	data := make([]byte, 200)
	_, err := prover.EncodeBlock(data)
	if !errors.Is(err, ErrBlockBlobTooLarge) {
		t.Fatalf("expected ErrBlockBlobTooLarge, got %v", err)
	}
}

func TestBlockBlobEncodeChunkSize(t *testing.T) {
	cfg := BlockBlobProverConfig{MaxBlockSize: 1024, BlobFieldElementSize: 10, MaxBlobsPerBlock: 16}
	prover := NewBlockBlobProver(cfg)
	data := make([]byte, 25)
	for i := range data {
		data[i] = byte(i)
	}
	enc, err := prover.EncodeBlock(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 25 bytes / 10 per chunk = 3 chunks (with padding on last).
	if enc.ChunkCount != 3 {
		t.Fatalf("expected 3 chunks, got %d", enc.ChunkCount)
	}
	if enc.PaddingBytes != 5 {
		t.Fatalf("expected 5 padding bytes, got %d", enc.PaddingBytes)
	}
}

func TestBlockBlobCreateProof(t *testing.T) {
	prover := defaultProver()
	data := bytes.Repeat([]byte{0xCD}, 200)
	enc, err := prover.EncodeBlock(data)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	proof, err := prover.CreateProof(enc)
	if err != nil {
		t.Fatalf("proof error: %v", err)
	}
	if proof.BlockHash == (types.Hash{}) {
		t.Fatal("expected non-zero block hash")
	}
	if proof.MerkleRoot == (types.Hash{}) {
		t.Fatal("expected non-zero merkle root")
	}
	if len(proof.ProofPath) == 0 {
		t.Fatal("expected non-empty proof path")
	}
	if proof.EncodingMetadata.OriginalSize != 200 {
		t.Fatalf("expected metadata original size 200, got %d", proof.EncodingMetadata.OriginalSize)
	}
}

func TestBlockBlobCreateProofNilEncoding(t *testing.T) {
	prover := defaultProver()
	_, err := prover.CreateProof(nil)
	if !errors.Is(err, ErrBlockBlobEncodingFailed) {
		t.Fatalf("expected ErrBlockBlobEncodingFailed, got %v", err)
	}
}

func TestBlockBlobVerifyProofValid(t *testing.T) {
	prover := defaultProver()
	data := bytes.Repeat([]byte{0xEF}, 150)
	enc, err := prover.EncodeBlock(data)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	proof, err := prover.CreateProof(enc)
	if err != nil {
		t.Fatalf("proof error: %v", err)
	}
	valid, err := prover.VerifyProof(proof, data)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if !valid {
		t.Fatal("expected proof to be valid")
	}
}

func TestBlockBlobVerifyProofTampered(t *testing.T) {
	prover := defaultProver()
	data := bytes.Repeat([]byte{0xEF}, 150)
	enc, err := prover.EncodeBlock(data)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	proof, err := prover.CreateProof(enc)
	if err != nil {
		t.Fatalf("proof error: %v", err)
	}
	// Tamper with data.
	tampered := make([]byte, len(data))
	copy(tampered, data)
	tampered[0] = 0x00
	valid, err := prover.VerifyProof(proof, tampered)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if valid {
		t.Fatal("expected proof to be invalid for tampered data")
	}
}

func TestBlockBlobVerifyProofNil(t *testing.T) {
	prover := defaultProver()
	_, err := prover.VerifyProof(nil, []byte{1})
	if !errors.Is(err, ErrBlockBlobInvalidProof) {
		t.Fatalf("expected ErrBlockBlobInvalidProof, got %v", err)
	}
}

func TestBlockBlobVerifyProofEmptyData(t *testing.T) {
	prover := defaultProver()
	proof := &BlockBlobProof{}
	_, err := prover.VerifyProof(proof, nil)
	if !errors.Is(err, ErrBlockBlobInvalidProof) {
		t.Fatalf("expected ErrBlockBlobInvalidProof, got %v", err)
	}
}

func TestBlockBlobEstimateBlobCount(t *testing.T) {
	prover := defaultProver()

	// Zero size.
	if n := prover.EstimateBlobCount(0); n != 0 {
		t.Fatalf("expected 0 for size 0, got %d", n)
	}
	// Negative size.
	if n := prover.EstimateBlobCount(-1); n != 0 {
		t.Fatalf("expected 0 for negative size, got %d", n)
	}
	// Small block.
	n := prover.EstimateBlobCount(1000)
	if n < 1 {
		t.Fatalf("expected at least 1 blob, got %d", n)
	}
	// Larger block requires more blobs.
	n2 := prover.EstimateBlobCount(500_000)
	if n2 <= n {
		t.Fatalf("expected more blobs for larger block: %d vs %d", n2, n)
	}
}

func TestBlockBlobRoundTrip(t *testing.T) {
	prover := defaultProver()
	// Test multiple sizes.
	sizes := []int{1, 30, 31, 62, 100, 500, 1024}
	for _, size := range sizes {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}
		enc, err := prover.EncodeBlock(data)
		if err != nil {
			t.Fatalf("size %d: encode error: %v", size, err)
		}
		proof, err := prover.CreateProof(enc)
		if err != nil {
			t.Fatalf("size %d: proof error: %v", size, err)
		}
		valid, err := prover.VerifyProof(proof, data)
		if err != nil {
			t.Fatalf("size %d: verify error: %v", size, err)
		}
		if !valid {
			t.Fatalf("size %d: proof should be valid", size)
		}
	}
}

func TestBlockBlobEncodingReassembly(t *testing.T) {
	prover := defaultProver()
	data := []byte("hello world, this is block data for blob encoding test")
	enc, err := prover.EncodeBlock(data)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	reassembled := reassembleFromEncoding(enc)
	if !bytes.Equal(reassembled, data) {
		t.Fatalf("reassembled data mismatch: got %q, want %q", reassembled, data)
	}
}

func TestBlockBlobMerkleRootDeterministic(t *testing.T) {
	prover := defaultProver()
	data := bytes.Repeat([]byte{0x42}, 200)
	enc, err := prover.EncodeBlock(data)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	proof1, err := prover.CreateProof(enc)
	if err != nil {
		t.Fatalf("proof1 error: %v", err)
	}
	proof2, err := prover.CreateProof(enc)
	if err != nil {
		t.Fatalf("proof2 error: %v", err)
	}
	if proof1.MerkleRoot != proof2.MerkleRoot {
		t.Fatal("merkle root should be deterministic")
	}
	if proof1.BlockHash != proof2.BlockHash {
		t.Fatal("block hash should be deterministic")
	}
}

func TestBlockBlobProofBlobIndices(t *testing.T) {
	prover := defaultProver()
	data := bytes.Repeat([]byte{0xFF}, 500)
	enc, err := prover.EncodeBlock(data)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	proof, err := prover.CreateProof(enc)
	if err != nil {
		t.Fatalf("proof error: %v", err)
	}
	if len(proof.BlobIndices) == 0 {
		t.Fatal("expected non-empty blob indices")
	}
	// Indices should be sequential from 0.
	for i, idx := range proof.BlobIndices {
		if idx != uint64(i) {
			t.Fatalf("expected index %d, got %d", i, idx)
		}
	}
}

func TestBlockBlobThreadSafety(t *testing.T) {
	prover := defaultProver()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			data := bytes.Repeat([]byte{byte(id)}, 100+id*10)
			enc, err := prover.EncodeBlock(data)
			if err != nil {
				t.Errorf("goroutine %d encode: %v", id, err)
				return
			}
			proof, err := prover.CreateProof(enc)
			if err != nil {
				t.Errorf("goroutine %d proof: %v", id, err)
				return
			}
			valid, err := prover.VerifyProof(proof, data)
			if err != nil {
				t.Errorf("goroutine %d verify: %v", id, err)
				return
			}
			if !valid {
				t.Errorf("goroutine %d: proof should be valid", id)
			}
		}(i)
	}
	wg.Wait()
}

func TestBlockBlobNextPowerOfTwo(t *testing.T) {
	tests := []struct {
		input    int
		expected int
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
	}
	for _, tc := range tests {
		got := nextPowerOfTwo(tc.input)
		if got != tc.expected {
			t.Errorf("nextPowerOfTwo(%d) = %d, want %d", tc.input, got, tc.expected)
		}
	}
}

func TestBlockBlobSingleChunk(t *testing.T) {
	cfg := BlockBlobProverConfig{MaxBlockSize: 1024, BlobFieldElementSize: 64, MaxBlobsPerBlock: 16}
	prover := NewBlockBlobProver(cfg)
	data := []byte("small")
	enc, err := prover.EncodeBlock(data)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if enc.ChunkCount != 1 {
		t.Fatalf("expected 1 chunk, got %d", enc.ChunkCount)
	}
	proof, err := prover.CreateProof(enc)
	if err != nil {
		t.Fatalf("proof error: %v", err)
	}
	valid, err := prover.VerifyProof(proof, data)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if !valid {
		t.Fatal("single chunk proof should be valid")
	}
}

func TestBlockBlobExactChunkSize(t *testing.T) {
	cfg := BlockBlobProverConfig{MaxBlockSize: 1024, BlobFieldElementSize: 10, MaxBlobsPerBlock: 16}
	prover := NewBlockBlobProver(cfg)
	// Exactly 30 bytes = 3 chunks of 10 with no padding.
	data := bytes.Repeat([]byte{0xAA}, 30)
	enc, err := prover.EncodeBlock(data)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if enc.ChunkCount != 3 {
		t.Fatalf("expected 3 chunks, got %d", enc.ChunkCount)
	}
	if enc.PaddingBytes != 0 {
		t.Fatalf("expected 0 padding, got %d", enc.PaddingBytes)
	}
}

func TestBlockBlobDefaultConfig(t *testing.T) {
	cfg := DefaultBlockBlobProverConfig()
	if cfg.MaxBlockSize != 2*1024*1024 {
		t.Fatalf("expected MaxBlockSize=2MiB, got %d", cfg.MaxBlockSize)
	}
	if cfg.BlobFieldElementSize != 31 {
		t.Fatalf("expected BlobFieldElementSize=31, got %d", cfg.BlobFieldElementSize)
	}
	if cfg.MaxBlobsPerBlock != 16 {
		t.Fatalf("expected MaxBlobsPerBlock=16, got %d", cfg.MaxBlobsPerBlock)
	}
}

func TestBlockBlobEstimateBlobCountConsistency(t *testing.T) {
	prover := defaultProver()
	// Verify that EstimateBlobCount returns at least 1 for any positive size.
	for _, size := range []int{1, 100, 1000, 10000, 100000} {
		n := prover.EstimateBlobCount(size)
		if n < 1 {
			t.Fatalf("EstimateBlobCount(%d) = %d, expected >= 1", size, n)
		}
	}
}

func TestBlockBlobOneByte(t *testing.T) {
	prover := defaultProver()
	data := []byte{0xFF}
	enc, err := prover.EncodeBlock(data)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if enc.ChunkCount != 1 {
		t.Fatalf("expected 1 chunk for 1 byte, got %d", enc.ChunkCount)
	}
	proof, err := prover.CreateProof(enc)
	if err != nil {
		t.Fatalf("proof error: %v", err)
	}
	valid, err := prover.VerifyProof(proof, data)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if !valid {
		t.Fatal("1-byte proof should be valid")
	}
}
