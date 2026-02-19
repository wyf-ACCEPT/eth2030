package core

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestChunkPayloadEmpty(t *testing.T) {
	chunks := ChunkPayload(nil, DefaultMaxChunkSize)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for empty payload, got %d", len(chunks))
	}
	if chunks[0].ChunkIndex != 0 || chunks[0].TotalChunks != 1 {
		t.Fatalf("unexpected chunk metadata: index=%d total=%d", chunks[0].ChunkIndex, chunks[0].TotalChunks)
	}
}

func TestChunkPayloadSingleByte(t *testing.T) {
	payload := []byte{0x42}
	chunks := ChunkPayload(payload, DefaultMaxChunkSize)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !bytes.Equal(chunks[0].Data, payload) {
		t.Fatalf("data mismatch")
	}
}

func TestChunkPayloadRoundtrip(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		maxSize int
	}{
		{"empty", 0, DefaultMaxChunkSize},
		{"1byte", 1, DefaultMaxChunkSize},
		{"exact-128KB", 128 * 1024, DefaultMaxChunkSize},
		{"128KB+1", 128*1024 + 1, DefaultMaxChunkSize},
		{"1MB", 1024 * 1024, DefaultMaxChunkSize},
		{"small-chunks", 1000, 100},
		{"tiny-chunks", 256, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := make([]byte, tt.size)
			if tt.size > 0 {
				rand.Read(payload)
			}

			chunks := ChunkPayload(payload, tt.maxSize)

			// Verify chunk metadata.
			for i, c := range chunks {
				if c.ChunkIndex != uint16(i) {
					t.Errorf("chunk %d: expected index %d, got %d", i, i, c.ChunkIndex)
				}
				if c.TotalChunks != uint16(len(chunks)) {
					t.Errorf("chunk %d: expected total %d, got %d", i, len(chunks), c.TotalChunks)
				}
			}

			// Reassemble.
			got, err := ReassemblePayload(chunks)
			if err != nil {
				t.Fatalf("ReassemblePayload: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("roundtrip mismatch: got %d bytes, want %d bytes", len(got), len(payload))
			}
		})
	}
}

func TestChunkPayloadShuffledOrder(t *testing.T) {
	payload := make([]byte, 500)
	rand.Read(payload)

	chunks := ChunkPayload(payload, 100)
	if len(chunks) < 2 {
		t.Fatal("expected multiple chunks")
	}

	// Reverse chunk order.
	reversed := make([]PayloadChunk, len(chunks))
	for i, c := range chunks {
		reversed[len(chunks)-1-i] = c
	}

	got, err := ReassemblePayload(reversed)
	if err != nil {
		t.Fatalf("ReassemblePayload with reversed chunks: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("roundtrip mismatch with reversed chunks")
	}
}

func TestReassemblePayloadErrors(t *testing.T) {
	payload := make([]byte, 300)
	rand.Read(payload)
	chunks := ChunkPayload(payload, 100)

	// Missing chunk.
	_, err := ReassemblePayload(chunks[:len(chunks)-1])
	if err == nil {
		t.Fatal("expected error for missing chunk")
	}

	// Duplicate chunk.
	dup := make([]PayloadChunk, len(chunks))
	copy(dup, chunks)
	dup[len(dup)-1] = dup[0]
	_, err = ReassemblePayload(dup)
	if err == nil {
		t.Fatal("expected error for duplicate chunk")
	}

	// Empty input.
	_, err = ReassemblePayload(nil)
	if err == nil {
		t.Fatal("expected error for nil chunks")
	}

	// Mismatched PayloadID.
	mismatch := make([]PayloadChunk, len(chunks))
	copy(mismatch, chunks)
	mismatch[1].PayloadID = [32]byte{0xff}
	_, err = ReassemblePayload(mismatch)
	if err == nil {
		t.Fatal("expected error for mismatched PayloadID")
	}
}

func TestValidateChunk(t *testing.T) {
	good := PayloadChunk{
		ChunkIndex:  0,
		TotalChunks: 2,
		PayloadID:   [32]byte{1},
		Data:        []byte{0x01},
	}
	if err := ValidateChunk(good); err != nil {
		t.Fatalf("valid chunk rejected: %v", err)
	}

	// Index >= Total.
	bad := good
	bad.ChunkIndex = 2
	if err := ValidateChunk(bad); err == nil {
		t.Fatal("expected error for index >= total")
	}

	// Zero total.
	bad = good
	bad.TotalChunks = 0
	bad.ChunkIndex = 0
	if err := ValidateChunk(bad); err == nil {
		t.Fatal("expected error for zero total")
	}

	// Empty data.
	bad = good
	bad.Data = nil
	if err := ValidateChunk(bad); err == nil {
		t.Fatal("expected error for empty data")
	}

	// Zero PayloadID.
	bad = good
	bad.PayloadID = [32]byte{}
	if err := ValidateChunk(bad); err == nil {
		t.Fatal("expected error for zero PayloadID")
	}
}

func TestEncodeDecodeChunk(t *testing.T) {
	original := PayloadChunk{
		ChunkIndex:  3,
		TotalChunks: 10,
		PayloadID:   [32]byte{0xAA, 0xBB},
		Data:        []byte{0x01, 0x02, 0x03, 0x04, 0x05},
		ProofHash:   [32]byte{0xCC, 0xDD},
	}

	encoded := EncodeChunk(&original)
	decoded, err := DecodeChunk(encoded)
	if err != nil {
		t.Fatalf("DecodeChunk: %v", err)
	}

	if decoded.ChunkIndex != original.ChunkIndex {
		t.Errorf("ChunkIndex: got %d, want %d", decoded.ChunkIndex, original.ChunkIndex)
	}
	if decoded.TotalChunks != original.TotalChunks {
		t.Errorf("TotalChunks: got %d, want %d", decoded.TotalChunks, original.TotalChunks)
	}
	if decoded.PayloadID != original.PayloadID {
		t.Error("PayloadID mismatch")
	}
	if decoded.ProofHash != original.ProofHash {
		t.Error("ProofHash mismatch")
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Error("Data mismatch")
	}
}

func TestDecodeChunkErrors(t *testing.T) {
	// Too short.
	_, err := DecodeChunk(make([]byte, 10))
	if err == nil {
		t.Fatal("expected error for short data")
	}

	// Truncated data.
	c := PayloadChunk{TotalChunks: 1, Data: []byte{0x01, 0x02, 0x03}}
	encoded := EncodeChunk(&c)
	_, err = DecodeChunk(encoded[:len(encoded)-2])
	if err == nil {
		t.Fatal("expected error for truncated data")
	}
}
