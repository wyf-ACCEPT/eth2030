package core

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestCalcBlobsRequired(t *testing.T) {
	tests := []struct {
		blockSize int
		want      int
	}{
		{0, 1},                        // 4 bytes prefix fits in 1 blob
		{1, 1},                        // 5 bytes
		{UsableBytesPerBlob - 4, 1},   // exactly fills one blob
		{UsableBytesPerBlob - 3, 2},   // one byte over
		{UsableBytesPerBlob, 2},       // needs prefix + data
		{UsableBytesPerBlob*2 - 4, 2}, // exactly fills two blobs
		{UsableBytesPerBlob*2 - 3, 3}, // one byte over two blobs
		{1024 * 1024, 9},              // ~1MB block
	}

	for _, tt := range tests {
		got := CalcBlobsRequired(tt.blockSize)
		if got != tt.want {
			t.Errorf("CalcBlobsRequired(%d) = %d, want %d", tt.blockSize, got, tt.want)
		}
	}
}

func TestBlobEncoderRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{"empty", 0},
		{"1byte", 1},
		{"small", 100},
		{"1KB", 1024},
		{"exact-one-blob", UsableBytesPerBlob - BlockLengthPrefixSize},
		{"two-blobs", UsableBytesPerBlob},
		{"128KB", 128 * 1024},
		{"256KB", 256 * 1024},
	}

	enc := &BlobEncoder{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, tt.size)
			if tt.size > 0 {
				rand.Read(data)
			}

			blobs, err := enc.EncodeBlockToBlobs(data)
			if err != nil {
				t.Fatalf("EncodeBlockToBlobs: %v", err)
			}

			expectedBlobs := CalcBlobsRequired(tt.size)
			if len(blobs) != expectedBlobs {
				t.Fatalf("got %d blobs, want %d", len(blobs), expectedBlobs)
			}

			// Verify blob sizes.
			for i, blob := range blobs {
				if len(blob) != BlobSize {
					t.Fatalf("blob %d: got %d bytes, want %d", i, len(blob), BlobSize)
				}
			}

			// Verify high bytes are zero.
			for bi, blob := range blobs {
				for ei := range ElementsPerBlob {
					if blob[ei*FieldElementSize] != 0 {
						t.Fatalf("blob %d element %d: high byte is 0x%02x, want 0x00",
							bi, ei, blob[ei*FieldElementSize])
					}
				}
			}

			// Decode and compare.
			got, err := enc.DecodeBlobsToBlock(blobs)
			if err != nil {
				t.Fatalf("DecodeBlobsToBlock: %v", err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("roundtrip mismatch: got %d bytes, want %d", len(got), len(data))
			}
		})
	}
}

func TestBlobEncoderHighByteZero(t *testing.T) {
	// Encode a block and verify every field element's high byte is 0.
	data := make([]byte, 10000)
	for i := range data {
		data[i] = 0xFF // worst case: all high bits set
	}

	enc := &BlobEncoder{}
	blobs, err := enc.EncodeBlockToBlobs(data)
	if err != nil {
		t.Fatalf("EncodeBlockToBlobs: %v", err)
	}

	for bi, blob := range blobs {
		for ei := range ElementsPerBlob {
			if blob[ei*FieldElementSize] != 0 {
				t.Fatalf("blob %d element %d: high byte is non-zero", bi, ei)
			}
		}
	}
}

func TestDecodeBlobsErrors(t *testing.T) {
	enc := &BlobEncoder{}

	// No blobs.
	_, err := enc.DecodeBlobsToBlock(nil)
	if err == nil {
		t.Fatal("expected error for nil blobs")
	}

	// Wrong blob size.
	_, err = enc.DecodeBlobsToBlock([][]byte{make([]byte, 100)})
	if err == nil {
		t.Fatal("expected error for wrong blob size")
	}

	// Corrupted length prefix (claims more data than available).
	blob := make([]byte, BlobSize)
	// Write a huge length in the first field element's usable bytes.
	blob[1] = 0xFF
	blob[2] = 0xFF
	blob[3] = 0xFF
	blob[4] = 0xFF
	_, err = enc.DecodeBlobsToBlock([][]byte{blob})
	if err == nil {
		t.Fatal("expected error for corrupted length prefix")
	}
}

func TestBlobEncoderLargeBlock(t *testing.T) {
	// Test with 1 MB block.
	data := make([]byte, 1024*1024)
	rand.Read(data)

	enc := &BlobEncoder{}
	blobs, err := enc.EncodeBlockToBlobs(data)
	if err != nil {
		t.Fatalf("EncodeBlockToBlobs: %v", err)
	}

	got, err := enc.DecodeBlobsToBlock(blobs)
	if err != nil {
		t.Fatalf("DecodeBlobsToBlock: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("roundtrip mismatch for 1MB block")
	}
}

func TestValidateBlockInBlobs(t *testing.T) {
	// Empty blobs.
	if err := ValidateBlockInBlobs(nil); err == nil {
		t.Fatal("expected error for empty blobs")
	}

	// Wrong blob size.
	if err := ValidateBlockInBlobs([][]byte{make([]byte, 100)}); err == nil {
		t.Fatal("expected error for wrong blob size")
	}

	// Non-zero high byte.
	blob := make([]byte, BlobSize)
	blob[0] = 0xFF // high byte of first element
	if err := ValidateBlockInBlobs([][]byte{blob}); err == nil {
		t.Fatal("expected error for non-zero high byte")
	}

	// Valid blob (all zeros, high bytes are 0).
	validBlob := make([]byte, BlobSize)
	if err := ValidateBlockInBlobs([][]byte{validBlob}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
