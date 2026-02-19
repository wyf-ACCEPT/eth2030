package das

import (
	"sync"
	"testing"
)

func TestStreamConfig(t *testing.T) {
	cfg := DefaultStreamConfig()
	if cfg.ChunkSize != BytesPerCell {
		t.Errorf("ChunkSize = %d, want %d", cfg.ChunkSize, BytesPerCell)
	}
	if cfg.MaxConcurrentStreams != 16 {
		t.Errorf("MaxConcurrentStreams = %d, want 16", cfg.MaxConcurrentStreams)
	}
}

func TestBlobStreamAddChunk(t *testing.T) {
	commitment := [32]byte{0x01}
	totalSize := uint32(4096) // 2 chunks at 2048 each
	stream := newBlobStream(commitment, totalSize, BytesPerCell, nil)

	if stream.IsComplete() {
		t.Fatal("stream should not be complete initially")
	}

	// Add first chunk.
	chunk0 := &BlobChunk{Index: 0, Data: make([]byte, BytesPerCell)}
	if err := stream.AddChunk(chunk0); err != nil {
		t.Fatalf("AddChunk(0): %v", err)
	}

	if stream.IsComplete() {
		t.Fatal("stream should not be complete after 1 of 2 chunks")
	}

	// Add second chunk.
	chunk1 := &BlobChunk{Index: 1, Data: make([]byte, BytesPerCell)}
	if err := stream.AddChunk(chunk1); err != nil {
		t.Fatalf("AddChunk(1): %v", err)
	}

	if !stream.IsComplete() {
		t.Fatal("stream should be complete after 2 of 2 chunks")
	}
}

func TestBlobStreamDuplicateChunk(t *testing.T) {
	commitment := [32]byte{0x02}
	stream := newBlobStream(commitment, 4096, BytesPerCell, nil)

	chunk := &BlobChunk{Index: 0, Data: make([]byte, BytesPerCell)}
	if err := stream.AddChunk(chunk); err != nil {
		t.Fatalf("first AddChunk: %v", err)
	}

	err := stream.AddChunk(chunk)
	if err == nil {
		t.Fatal("expected error for duplicate chunk")
	}
}

func TestBlobStreamOutOfRange(t *testing.T) {
	commitment := [32]byte{0x03}
	stream := newBlobStream(commitment, BytesPerCell, BytesPerCell, nil) // 1 chunk

	chunk := &BlobChunk{Index: 5, Data: make([]byte, BytesPerCell)}
	err := stream.AddChunk(chunk)
	if err == nil {
		t.Fatal("expected error for chunk index out of range")
	}
}

func TestBlobStreamClosed(t *testing.T) {
	commitment := [32]byte{0x04}
	stream := newBlobStream(commitment, 4096, BytesPerCell, nil)
	stream.Close()

	chunk := &BlobChunk{Index: 0, Data: make([]byte, BytesPerCell)}
	err := stream.AddChunk(chunk)
	if err != ErrStreamClosed {
		t.Fatalf("expected ErrStreamClosed, got %v", err)
	}
}

func TestBlobStreamAssemble(t *testing.T) {
	commitment := [32]byte{0x05}
	totalSize := uint32(3000) // Less than 2 full chunks
	stream := newBlobStream(commitment, totalSize, BytesPerCell, nil)

	// Fill first chunk with 0xAA.
	data0 := make([]byte, BytesPerCell)
	for i := range data0 {
		data0[i] = 0xAA
	}
	stream.AddChunk(&BlobChunk{Index: 0, Data: data0})

	// Fill second chunk with 0xBB.
	data1 := make([]byte, BytesPerCell)
	for i := range data1 {
		data1[i] = 0xBB
	}
	stream.AddChunk(&BlobChunk{Index: 1, Data: data1})

	result, err := stream.Assemble()
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if uint32(len(result)) != totalSize {
		t.Fatalf("assembled size = %d, want %d", len(result), totalSize)
	}

	// First BytesPerCell bytes should be 0xAA.
	for i := 0; i < BytesPerCell; i++ {
		if result[i] != 0xAA {
			t.Fatalf("result[%d] = %02x, want 0xAA", i, result[i])
		}
	}

	// Remaining bytes should be 0xBB (up to totalSize).
	for i := BytesPerCell; i < int(totalSize); i++ {
		if result[i] != 0xBB {
			t.Fatalf("result[%d] = %02x, want 0xBB", i, result[i])
		}
	}
}

func TestBlobStreamAssembleIncomplete(t *testing.T) {
	commitment := [32]byte{0x06}
	stream := newBlobStream(commitment, 4096, BytesPerCell, nil)

	_, err := stream.Assemble()
	if err == nil {
		t.Fatal("expected error for incomplete stream")
	}
}

func TestBlobStreamProgress(t *testing.T) {
	commitment := [32]byte{0x07}
	stream := newBlobStream(commitment, uint32(BytesPerCell*4), BytesPerCell, nil)

	if got := stream.Progress(); got != 0.0 {
		t.Errorf("initial progress = %f, want 0.0", got)
	}

	stream.AddChunk(&BlobChunk{Index: 0, Data: make([]byte, BytesPerCell)})
	if got := stream.Progress(); got != 0.25 {
		t.Errorf("after 1/4 chunks progress = %f, want 0.25", got)
	}

	stream.AddChunk(&BlobChunk{Index: 1, Data: make([]byte, BytesPerCell)})
	stream.AddChunk(&BlobChunk{Index: 2, Data: make([]byte, BytesPerCell)})
	stream.AddChunk(&BlobChunk{Index: 3, Data: make([]byte, BytesPerCell)})
	if got := stream.Progress(); got != 1.0 {
		t.Errorf("after 4/4 chunks progress = %f, want 1.0", got)
	}
}

func TestBlobStreamProofVerification(t *testing.T) {
	commitment := [32]byte{0x08}
	stream := newBlobStream(commitment, BytesPerCell, BytesPerCell, nil)

	data := make([]byte, BytesPerCell)
	data[0] = 0x42

	// Compute correct proof.
	proof := computeChunkProof(commitment, 0, data)
	chunk := &BlobChunk{Index: 0, Data: data, Proof: proof}

	if err := stream.AddChunk(chunk); err != nil {
		t.Fatalf("valid proof rejected: %v", err)
	}
}

func TestBlobStreamBadProof(t *testing.T) {
	commitment := [32]byte{0x09}
	stream := newBlobStream(commitment, uint32(BytesPerCell*2), BytesPerCell, nil)

	data := make([]byte, BytesPerCell)
	badProof := make([]byte, 32)
	badProof[0] = 0xFF
	chunk := &BlobChunk{Index: 0, Data: data, Proof: badProof}

	err := stream.AddChunk(chunk)
	if err != ErrChunkVerification {
		t.Fatalf("expected ErrChunkVerification, got %v", err)
	}
}

func TestBlobStreamCallback(t *testing.T) {
	commitment := [32]byte{0x0A}
	var callbackCount int
	cb := func(chunk *BlobChunk) {
		callbackCount++
	}

	stream := newBlobStream(commitment, uint32(BytesPerCell*2), BytesPerCell, cb)
	stream.AddChunk(&BlobChunk{Index: 0, Data: make([]byte, BytesPerCell)})
	stream.AddChunk(&BlobChunk{Index: 1, Data: make([]byte, BytesPerCell)})

	if callbackCount != 2 {
		t.Errorf("callback count = %d, want 2", callbackCount)
	}
}

func TestBlobStreamerStartStream(t *testing.T) {
	cfg := DefaultStreamConfig()
	bs := NewBlobStreamer(cfg)

	hash := [32]byte{0x01}
	stream, err := bs.StartStream(hash, 4096, nil)
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	if stream == nil {
		t.Fatal("stream is nil")
	}
	if bs.ActiveStreams() != 1 {
		t.Errorf("ActiveStreams = %d, want 1", bs.ActiveStreams())
	}

	// Starting same hash again returns existing stream.
	stream2, err := bs.StartStream(hash, 4096, nil)
	if err != nil {
		t.Fatalf("StartStream (same): %v", err)
	}
	if stream2 != stream {
		t.Error("expected same stream for same hash")
	}
	if bs.ActiveStreams() != 1 {
		t.Errorf("ActiveStreams = %d, want 1 (no duplicate)", bs.ActiveStreams())
	}
}

func TestBlobStreamerMaxStreams(t *testing.T) {
	cfg := StreamConfig{
		ChunkSize:            BytesPerCell,
		MaxConcurrentStreams:  2,
	}
	bs := NewBlobStreamer(cfg)

	bs.StartStream([32]byte{0x01}, 4096, nil)
	bs.StartStream([32]byte{0x02}, 4096, nil)

	_, err := bs.StartStream([32]byte{0x03}, 4096, nil)
	if err != ErrMaxStreams {
		t.Fatalf("expected ErrMaxStreams, got %v", err)
	}
}

func TestBlobStreamerCloseStream(t *testing.T) {
	cfg := DefaultStreamConfig()
	bs := NewBlobStreamer(cfg)

	hash := [32]byte{0x01}
	bs.StartStream(hash, 4096, nil)
	bs.CloseStream(hash)

	if bs.ActiveStreams() != 0 {
		t.Errorf("ActiveStreams = %d, want 0 after close", bs.ActiveStreams())
	}

	_, err := bs.GetStream(hash)
	if err != ErrStreamNotFound {
		t.Fatalf("expected ErrStreamNotFound, got %v", err)
	}
}

func TestBlobStreamerGetStream(t *testing.T) {
	cfg := DefaultStreamConfig()
	bs := NewBlobStreamer(cfg)

	_, err := bs.GetStream([32]byte{0x99})
	if err != ErrStreamNotFound {
		t.Fatalf("expected ErrStreamNotFound, got %v", err)
	}

	hash := [32]byte{0x01}
	bs.StartStream(hash, 4096, nil)

	stream, err := bs.GetStream(hash)
	if err != nil {
		t.Fatalf("GetStream: %v", err)
	}
	if stream == nil {
		t.Fatal("GetStream returned nil")
	}
}

func TestBlobStreamConcurrentChunks(t *testing.T) {
	commitment := [32]byte{0x0B}
	numChunks := 8
	stream := newBlobStream(commitment, uint32(BytesPerCell*numChunks), BytesPerCell, nil)

	var wg sync.WaitGroup
	for i := 0; i < numChunks; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			chunk := &BlobChunk{Index: uint32(idx), Data: make([]byte, BytesPerCell)}
			stream.AddChunk(chunk)
		}(i)
	}
	wg.Wait()

	if !stream.IsComplete() {
		t.Error("stream should be complete after all chunks added concurrently")
	}
}
