package engine

import (
	"bytes"
	"crypto/rand"
	"errors"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/crypto"
)

func makeChunkTestPayload(size int) []byte {
	data := make([]byte, size)
	rand.Read(data)
	return data
}

func TestPayloadChunker_BasicChunkAndReassemble(t *testing.T) {
	pc := NewPayloadChunker(DefaultChunkSize)
	payload := makeChunkTestPayload(300 * 1024) // 300 KiB

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	expected := (len(payload) + DefaultChunkSize - 1) / DefaultChunkSize
	if len(chunks) != expected {
		t.Fatalf("expected %d chunks, got %d", expected, len(chunks))
	}

	// Verify each chunk.
	for i, chunk := range chunks {
		if chunk.Index != uint32(i) {
			t.Fatalf("chunk %d: expected index %d, got %d", i, i, chunk.Index)
		}
		if chunk.Total != uint32(expected) {
			t.Fatalf("chunk %d: expected total %d, got %d", i, expected, chunk.Total)
		}
		if !VerifyChunkIntegrity(chunk) {
			t.Fatalf("chunk %d: integrity check failed", i)
		}
	}

	// Reassemble.
	result, err := pc.ReassemblePayload(chunks)
	if err != nil {
		t.Fatalf("reassemble error: %v", err)
	}

	if !bytes.Equal(result, payload) {
		t.Fatal("reassembled payload does not match original")
	}
}

func TestPayloadChunker_SmallPayload(t *testing.T) {
	pc := NewPayloadChunker(DefaultChunkSize)
	payload := makeChunkTestPayload(100) // smaller than one chunk

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for small payload, got %d", len(chunks))
	}

	result, err := pc.ReassemblePayload(chunks)
	if err != nil {
		t.Fatalf("reassemble error: %v", err)
	}

	if !bytes.Equal(result, payload) {
		t.Fatal("reassembled payload does not match original")
	}
}

func TestPayloadChunker_ExactMultiple(t *testing.T) {
	chunkSize := 4096
	pc := NewPayloadChunker(chunkSize)
	payload := makeChunkTestPayload(chunkSize * 3) // exactly 3 chunks

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	result, err := pc.ReassemblePayload(chunks)
	if err != nil {
		t.Fatalf("reassemble error: %v", err)
	}

	if !bytes.Equal(result, payload) {
		t.Fatal("payload mismatch")
	}
}

func TestPayloadChunker_EmptyPayload(t *testing.T) {
	pc := NewPayloadChunker(DefaultChunkSize)

	_, err := pc.ChunkPayload(nil)
	if !errors.Is(err, ErrChunkEmpty) {
		t.Fatalf("expected ErrChunkEmpty, got: %v", err)
	}

	_, err = pc.ChunkPayload([]byte{})
	if !errors.Is(err, ErrChunkEmpty) {
		t.Fatalf("expected ErrChunkEmpty for empty slice, got: %v", err)
	}
}

func TestPayloadChunker_ReassembleMissingChunk(t *testing.T) {
	pc := NewPayloadChunker(4096)
	payload := makeChunkTestPayload(4096 * 3)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	// Remove middle chunk.
	incomplete := []*PayloadChunk{chunks[0], chunks[2]}

	_, err = pc.ReassemblePayload(incomplete)
	if err == nil {
		t.Fatal("expected error for missing chunk")
	}
}

func TestPayloadChunker_ReassembleEmpty(t *testing.T) {
	pc := NewPayloadChunker(DefaultChunkSize)

	_, err := pc.ReassemblePayload(nil)
	if !errors.Is(err, ErrChunkMissing) {
		t.Fatalf("expected ErrChunkMissing, got: %v", err)
	}
}

func TestPayloadChunker_ReassembleDuplicate(t *testing.T) {
	pc := NewPayloadChunker(4096)
	payload := makeChunkTestPayload(4096 * 2)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	// Duplicate index 0.
	duped := []*PayloadChunk{chunks[0], chunks[0]}

	_, err = pc.ReassemblePayload(duped)
	if err == nil {
		t.Fatal("expected error for duplicate chunks")
	}
}

func TestPayloadChunker_CorruptedChunkData(t *testing.T) {
	pc := NewPayloadChunker(4096)
	payload := makeChunkTestPayload(4096 * 2)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	// Corrupt data in first chunk without updating hash.
	chunks[0].Data[0] ^= 0xFF

	_, err = pc.ReassemblePayload(chunks)
	if err == nil {
		t.Fatal("expected integrity error for corrupted chunk")
	}
}

func TestVerifyChunkIntegrity(t *testing.T) {
	data := []byte("test data for integrity check")
	hash := crypto.Keccak256Hash(data)

	chunk := &PayloadChunk{
		Index:      0,
		Total:      1,
		Data:       data,
		Hash:       hash,
		ParentHash: [32]byte{1},
	}

	if !VerifyChunkIntegrity(chunk) {
		t.Fatal("valid chunk should pass integrity check")
	}

	// Tamper with data.
	chunk.Data[0] ^= 0xFF
	if VerifyChunkIntegrity(chunk) {
		t.Fatal("tampered chunk should fail integrity check")
	}

	// Nil chunk.
	if VerifyChunkIntegrity(nil) {
		t.Fatal("nil chunk should fail integrity check")
	}

	// Empty data.
	if VerifyChunkIntegrity(&PayloadChunk{}) {
		t.Fatal("empty chunk should fail integrity check")
	}
}

func TestChunkSet_BasicCompletion(t *testing.T) {
	pc := NewPayloadChunker(4096)
	payload := makeChunkTestPayload(4096 * 3)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	cs := NewChunkSet(chunks[0].ParentHash, chunks[0].Total)

	if cs.IsComplete() {
		t.Fatal("should not be complete initially")
	}

	// Add chunks in order.
	for i, chunk := range chunks {
		complete, err := cs.AddChunk(chunk)
		if err != nil {
			t.Fatalf("add chunk %d error: %v", i, err)
		}
		if i < len(chunks)-1 && complete {
			t.Fatalf("should not be complete after %d chunks", i+1)
		}
	}

	if !cs.IsComplete() {
		t.Fatal("should be complete after all chunks added")
	}

	if cs.Received() != 3 {
		t.Fatalf("expected 3 received, got %d", cs.Received())
	}
}

func TestChunkSet_OutOfOrder(t *testing.T) {
	pc := NewPayloadChunker(4096)
	payload := makeChunkTestPayload(4096 * 4)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	cs := NewChunkSet(chunks[0].ParentHash, chunks[0].Total)

	// Add in reverse order.
	for i := len(chunks) - 1; i >= 0; i-- {
		_, err := cs.AddChunk(chunks[i])
		if err != nil {
			t.Fatalf("add chunk %d error: %v", i, err)
		}
	}

	if !cs.IsComplete() {
		t.Fatal("should be complete after all chunks added (out of order)")
	}

	// Verify ordered chunks.
	ordered := cs.Chunks()
	if len(ordered) != len(chunks) {
		t.Fatalf("expected %d ordered chunks, got %d", len(chunks), len(ordered))
	}

	// Reassemble from the ordered chunks.
	result, err := pc.ReassemblePayload(ordered)
	if err != nil {
		t.Fatalf("reassemble error: %v", err)
	}
	if !bytes.Equal(result, payload) {
		t.Fatal("reassembled payload mismatch")
	}
}

func TestChunkSet_Duplicate(t *testing.T) {
	pc := NewPayloadChunker(4096)
	payload := makeChunkTestPayload(4096 * 2)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	cs := NewChunkSet(chunks[0].ParentHash, chunks[0].Total)

	_, err = cs.AddChunk(chunks[0])
	if err != nil {
		t.Fatalf("first add error: %v", err)
	}

	_, err = cs.AddChunk(chunks[0])
	if err == nil {
		t.Fatal("expected error for duplicate chunk")
	}
}

func TestChunkSet_WrongParentHash(t *testing.T) {
	pc := NewPayloadChunker(4096)
	payload := makeChunkTestPayload(4096)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	wrongHash := [32]byte{0xFF}
	cs := NewChunkSet(wrongHash, chunks[0].Total)

	_, err = cs.AddChunk(chunks[0])
	if !errors.Is(err, ErrChunkParentHash) {
		t.Fatalf("expected ErrChunkParentHash, got: %v", err)
	}
}

func TestChunkSet_Missing(t *testing.T) {
	pc := NewPayloadChunker(4096)
	payload := makeChunkTestPayload(4096 * 4)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	cs := NewChunkSet(chunks[0].ParentHash, chunks[0].Total)
	cs.AddChunk(chunks[0])
	cs.AddChunk(chunks[2])

	missing := cs.Missing()
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing, got %d", len(missing))
	}
	if missing[0] != 1 || missing[1] != 3 {
		t.Fatalf("wrong missing indices: %v", missing)
	}
}

func TestChunkSet_Progress(t *testing.T) {
	pc := NewPayloadChunker(4096)
	payload := makeChunkTestPayload(4096 * 4)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	cs := NewChunkSet(chunks[0].ParentHash, chunks[0].Total)

	if cs.Progress() != 0 {
		t.Fatalf("expected 0 progress, got %f", cs.Progress())
	}

	cs.AddChunk(chunks[0])
	if cs.Progress() != 0.25 {
		t.Fatalf("expected 0.25 progress, got %f", cs.Progress())
	}

	cs.AddChunk(chunks[1])
	if cs.Progress() != 0.5 {
		t.Fatalf("expected 0.5 progress, got %f", cs.Progress())
	}
}

func TestChunkSet_ChunksReturnsNilIfIncomplete(t *testing.T) {
	cs := NewChunkSet([32]byte{1}, 3)

	if cs.Chunks() != nil {
		t.Fatal("expected nil chunks when incomplete")
	}
}

func TestChunkSet_ConcurrentAddChunk(t *testing.T) {
	pc := NewPayloadChunker(4096)
	payload := makeChunkTestPayload(4096 * 20)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	cs := NewChunkSet(chunks[0].ParentHash, chunks[0].Total)

	var wg sync.WaitGroup
	for _, chunk := range chunks {
		wg.Add(1)
		go func(c *PayloadChunk) {
			defer wg.Done()
			cs.AddChunk(c)
		}(chunk)
	}
	wg.Wait()

	if !cs.IsComplete() {
		t.Fatal("should be complete after concurrent adds")
	}
}

func TestPayloadChunker_ChunkSize(t *testing.T) {
	pc := NewPayloadChunker(4096)
	if pc.ChunkSize() != 4096 {
		t.Fatalf("expected chunk size 4096, got %d", pc.ChunkSize())
	}
}

func TestPayloadChunker_DefaultChunkSize(t *testing.T) {
	pc := NewPayloadChunker(0)
	if pc.ChunkSize() != DefaultChunkSize {
		t.Fatalf("expected default chunk size %d, got %d", DefaultChunkSize, pc.ChunkSize())
	}
}

func TestPayloadChunker_InvalidChunkSize(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for invalid chunk size")
		}
	}()
	NewPayloadChunker(100) // below MinChunkSize
}

func TestPayloadChunker_LargePayload(t *testing.T) {
	pc := NewPayloadChunker(DefaultChunkSize)
	// 1 MiB payload.
	payload := makeChunkTestPayload(1024 * 1024)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	result, err := pc.ReassemblePayload(chunks)
	if err != nil {
		t.Fatalf("reassemble error: %v", err)
	}

	if !bytes.Equal(result, payload) {
		t.Fatal("large payload mismatch")
	}
}

func TestChunkSet_AddNilChunk(t *testing.T) {
	cs := NewChunkSet([32]byte{1}, 1)

	_, err := cs.AddChunk(nil)
	if !errors.Is(err, ErrChunkEmpty) {
		t.Fatalf("expected ErrChunkEmpty for nil, got: %v", err)
	}
}

func TestChunkSet_TotalMismatch(t *testing.T) {
	pc := NewPayloadChunker(4096)
	payload := makeChunkTestPayload(4096)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	// Create set with wrong total.
	cs := NewChunkSet(chunks[0].ParentHash, 5)

	_, err = cs.AddChunk(chunks[0])
	if err == nil {
		t.Fatal("expected error for total mismatch")
	}
}

func TestChunkSet_ParentHash(t *testing.T) {
	hash := [32]byte{0xAB, 0xCD}
	cs := NewChunkSet(hash, 2)

	if cs.ParentHash() != hash {
		t.Fatal("parent hash mismatch")
	}
}

func TestChunkSet_Total(t *testing.T) {
	cs := NewChunkSet([32]byte{1}, 7)
	if cs.Total() != 7 {
		t.Fatalf("expected total 7, got %d", cs.Total())
	}
}

func TestPayloadChunker_ReassembleTotalMismatch(t *testing.T) {
	pc := NewPayloadChunker(4096)
	payload := makeChunkTestPayload(4096 * 2)

	chunks, err := pc.ChunkPayload(payload)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}

	// Tamper with total on second chunk.
	chunks[1].Total = 5

	_, err = pc.ReassemblePayload(chunks)
	if err == nil {
		t.Fatal("expected error for total mismatch in reassembly")
	}
}
