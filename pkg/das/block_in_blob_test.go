package das

import (
	"bytes"
	"crypto/rand"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func TestEncodeDecodeBlockSmall(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	data := []byte("hello, block-in-blob world")

	blobs, err := enc.EncodeBlock(data)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}
	if len(blobs) != 1 {
		t.Fatalf("expected 1 blob, got %d", len(blobs))
	}
	if !blobs[0].IsLast {
		t.Error("single blob should be marked as last")
	}
	if blobs[0].TotalChunks != 1 {
		t.Errorf("TotalChunks = %d, want 1", blobs[0].TotalChunks)
	}

	decoded, err := enc.DecodeBlock(blobs)
	if err != nil {
		t.Fatalf("DecodeBlock: %v", err)
	}
	if !bytes.Equal(decoded, data) {
		t.Errorf("decoded data mismatch:\n got: %q\nwant: %q", decoded, data)
	}
}

func TestEncodeDecodeBlockMultiBlob(t *testing.T) {
	// Use a small blob size to force multiple blobs.
	cfg := BlobBlockConfig{
		MaxBlobSize:      128, // 128 bytes total per blob
		MaxBlobsPerBlock: 64,
	}
	enc := NewBlobBlockEncoder(cfg)

	// 128 - 53 (header) = 75 usable bytes per blob.
	// 300 bytes of data -> ceil(300/75) = 4 blobs.
	data := make([]byte, 300)
	for i := range data {
		data[i] = byte(i % 256)
	}

	blobs, err := enc.EncodeBlock(data)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}
	if len(blobs) != 4 {
		t.Fatalf("expected 4 blobs, got %d", len(blobs))
	}

	// Only the last blob should have IsLast=true.
	for i, b := range blobs {
		if b.Index != uint64(i) {
			t.Errorf("blob %d: Index = %d", i, b.Index)
		}
		if b.TotalChunks != 4 {
			t.Errorf("blob %d: TotalChunks = %d, want 4", i, b.TotalChunks)
		}
		if i < 3 && b.IsLast {
			t.Errorf("blob %d: IsLast should be false", i)
		}
		if i == 3 && !b.IsLast {
			t.Error("last blob should have IsLast=true")
		}
	}

	decoded, err := enc.DecodeBlock(blobs)
	if err != nil {
		t.Fatalf("DecodeBlock: %v", err)
	}
	if !bytes.Equal(decoded, data) {
		t.Error("decoded data mismatch after multi-blob roundtrip")
	}
}

func TestEncodeBlockEmpty(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	_, err := enc.EncodeBlock(nil)
	if err != ErrBlockDataEmpty {
		t.Fatalf("expected ErrBlockDataEmpty, got %v", err)
	}
	_, err = enc.EncodeBlock([]byte{})
	if err != ErrBlockDataEmpty {
		t.Fatalf("expected ErrBlockDataEmpty for empty slice, got %v", err)
	}
}

func TestEncodeBlockTooLarge(t *testing.T) {
	cfg := BlobBlockConfig{
		MaxBlobSize:      128,
		MaxBlobsPerBlock: 2,
	}
	enc := NewBlobBlockEncoder(cfg)

	// 75 usable per blob * 2 = 150 bytes max. Use 200 bytes.
	data := make([]byte, 200)
	_, err := enc.EncodeBlock(data)
	if err == nil {
		t.Fatal("expected error for oversized block")
	}
}

func TestDecodeBlockNil(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	_, err := enc.DecodeBlock(nil)
	if err != ErrNoBlobsProvided {
		t.Fatalf("expected ErrNoBlobsProvided, got %v", err)
	}
}

func TestDecodeBlockHashMismatch(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	data := []byte("test data for hash mismatch")

	blobs, err := enc.EncodeBlock(data)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}

	// Tamper with the block hash on one blob.
	blobs[0].BlockHash = types.Hash{0xFF}
	blobs = append(blobs, blobs[0]) // need at least 2 with different hashes
	_, err = enc.DecodeBlock(blobs)
	if err == nil {
		t.Fatal("expected error for hash mismatch")
	}
}

func TestDecodeBlockMissingLastMarker(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	data := []byte("test for missing last marker")

	blobs, err := enc.EncodeBlock(data)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}

	blobs[len(blobs)-1].IsLast = false
	_, err = enc.DecodeBlock(blobs)
	if err != ErrMissingLastBlob {
		t.Fatalf("expected ErrMissingLastBlob, got %v", err)
	}
}

func TestVerifyBlockBlobs(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	data := []byte("verify block blobs test data")

	blobs, err := enc.EncodeBlock(data)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}

	if err := enc.VerifyBlockBlobs(blobs); err != nil {
		t.Fatalf("VerifyBlockBlobs: %v", err)
	}
}

func TestVerifyBlockBlobsEmpty(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	if err := enc.VerifyBlockBlobs(nil); err != ErrNoBlobsProvided {
		t.Fatalf("expected ErrNoBlobsProvided, got %v", err)
	}
}

func TestVerifyBlockBlobsCorruptData(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	data := []byte("verify corrupt test")

	blobs, err := enc.EncodeBlock(data)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}

	// Corrupt the block hash inside the blob data payload.
	blobs[0].Data[25] ^= 0xFF
	if err := enc.VerifyBlockBlobs(blobs); err == nil {
		t.Fatal("expected error for corrupt blob data")
	}
}

func TestEstimateBlobCount(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())

	if enc.EstimateBlobCount(0) != 0 {
		t.Error("estimate for 0 should be 0")
	}

	// Default: 131072 - 53 = 131019 usable bytes per blob.
	if enc.EstimateBlobCount(100) != 1 {
		t.Error("100 bytes should fit in 1 blob")
	}

	payload := enc.usablePayload()
	// Exactly one payload -> 1 blob.
	if enc.EstimateBlobCount(payload) != 1 {
		t.Errorf("payload size %d should need 1 blob", payload)
	}
	// payload+1 -> 2 blobs.
	if enc.EstimateBlobCount(payload+1) != 2 {
		t.Errorf("payload+1 should need 2 blobs")
	}
}

func TestGenerateAndVerifyCommitment(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	data := []byte("commitment verification test data")

	blobs, err := enc.EncodeBlock(data)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}

	commitment, err := enc.GenerateCommitment(blobs)
	if err != nil {
		t.Fatalf("GenerateCommitment: %v", err)
	}

	if commitment.BlockHash != blobs[0].BlockHash {
		t.Error("commitment block hash mismatch")
	}
	if len(commitment.BlobCommitments) != len(blobs) {
		t.Errorf("commitment count = %d, want %d", len(commitment.BlobCommitments), len(blobs))
	}
	if commitment.TotalSize == 0 {
		t.Error("total size should be > 0")
	}

	// Verify should pass.
	if !enc.VerifyCommitment(commitment, blobs) {
		t.Error("VerifyCommitment should return true for valid commitment")
	}
}

func TestVerifyCommitmentFails(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	data := []byte("bad commitment test")

	blobs, err := enc.EncodeBlock(data)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}

	commitment, _ := enc.GenerateCommitment(blobs)

	// Nil commitment.
	if enc.VerifyCommitment(nil, blobs) {
		t.Error("should fail for nil commitment")
	}

	// Empty blobs.
	if enc.VerifyCommitment(commitment, nil) {
		t.Error("should fail for nil blobs")
	}

	// Wrong block hash.
	badCommitment := &BlockBlobCommitment{
		BlockHash:       types.Hash{0xFF},
		BlobCommitments: commitment.BlobCommitments,
		TotalSize:       commitment.TotalSize,
	}
	if enc.VerifyCommitment(badCommitment, blobs) {
		t.Error("should fail for wrong block hash")
	}

	// Tampered blob data.
	blobs[0].Data[blockBlobHeaderSize] ^= 0xFF
	if enc.VerifyCommitment(commitment, blobs) {
		t.Error("should fail for tampered blob data")
	}
}

func TestBlockBlobBlockHash(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	data := []byte("block hash consistency")

	blobs, err := enc.EncodeBlock(data)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}

	expected := crypto.Keccak256Hash(data)
	for i, b := range blobs {
		if b.BlockHash != expected {
			t.Errorf("blob %d: BlockHash mismatch", i)
		}
	}
}

func TestEncodeDecodeLargeBlock(t *testing.T) {
	cfg := BlobBlockConfig{
		MaxBlobSize:      256,
		MaxBlobsPerBlock: 100,
	}
	enc := NewBlobBlockEncoder(cfg)

	// Generate random data large enough for many blobs.
	data := make([]byte, 5000)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}

	blobs, err := enc.EncodeBlock(data)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}
	if len(blobs) < 2 {
		t.Fatalf("expected multiple blobs, got %d", len(blobs))
	}

	decoded, err := enc.DecodeBlock(blobs)
	if err != nil {
		t.Fatalf("DecodeBlock: %v", err)
	}
	if !bytes.Equal(decoded, data) {
		t.Error("large block roundtrip mismatch")
	}
}

func TestBlockInBlobConcurrency(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			data := make([]byte, 100+n*10)
			for j := range data {
				data[j] = byte(n + j)
			}
			blobs, err := enc.EncodeBlock(data)
			if err != nil {
				t.Errorf("goroutine %d EncodeBlock: %v", n, err)
				return
			}
			decoded, err := enc.DecodeBlock(blobs)
			if err != nil {
				t.Errorf("goroutine %d DecodeBlock: %v", n, err)
				return
			}
			if !bytes.Equal(decoded, data) {
				t.Errorf("goroutine %d: data mismatch", n)
			}
			_ = enc.EstimateBlobCount(uint64(len(data)))
		}(i)
	}

	wg.Wait()
}

func TestDecodeBlockDuplicateIndex(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	data := []byte("duplicate index test")

	blobs, err := enc.EncodeBlock(data)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}

	// Duplicate the first blob (creates 2 blobs with index 0, TotalChunks=1 vs len=2).
	dup := *blobs[0]
	duped := []*BlockBlob{blobs[0], &dup}
	_, err = enc.DecodeBlock(duped)
	if err == nil {
		t.Fatal("expected error for duplicate blob index")
	}
}

func TestNewBlobBlockEncoderDefaults(t *testing.T) {
	// Zero config should get defaults.
	enc := NewBlobBlockEncoder(BlobBlockConfig{})
	if enc.config.MaxBlobSize != 131072 {
		t.Errorf("MaxBlobSize = %d, want 131072", enc.config.MaxBlobSize)
	}
	if enc.config.MaxBlobsPerBlock != 16 {
		t.Errorf("MaxBlobsPerBlock = %d, want 16", enc.config.MaxBlobsPerBlock)
	}
}

func TestGenerateCommitmentEmpty(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	_, err := enc.GenerateCommitment(nil)
	if err != ErrNoBlobsProvided {
		t.Fatalf("expected ErrNoBlobsProvided, got %v", err)
	}
}

func TestVerifyBlockBlobsCountMismatch(t *testing.T) {
	enc := NewBlobBlockEncoder(DefaultBlobBlockConfig())
	data := []byte("count mismatch test")

	blobs, err := enc.EncodeBlock(data)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}

	// Modify TotalChunks to create a mismatch.
	blobs[0].TotalChunks = 5
	if err := enc.VerifyBlockBlobs(blobs); err == nil {
		t.Fatal("expected error for count mismatch")
	}
}
