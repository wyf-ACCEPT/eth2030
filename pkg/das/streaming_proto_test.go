package das

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// --- StreamSession tests ---

func TestNewStreamSession(t *testing.T) {
	s := NewStreamSession(0, 4096, 1024)
	if s == nil {
		t.Fatal("expected non-nil session")
	}
	if s.BlobIndex != 0 {
		t.Errorf("BlobIndex = %d, want 0", s.BlobIndex)
	}
	if s.NumChunks() != 4 {
		t.Errorf("NumChunks = %d, want 4", s.NumChunks())
	}
	if s.ID.IsZero() {
		t.Error("expected non-zero session ID")
	}
}

func TestNewStreamSessionRoundsUp(t *testing.T) {
	// 5000 bytes / 1024 per chunk = 5 chunks (rounds up)
	s := NewStreamSession(1, 5000, 1024)
	if s.NumChunks() != 5 {
		t.Errorf("NumChunks = %d, want 5", s.NumChunks())
	}
}

func TestNewStreamSessionZeroChunkSize(t *testing.T) {
	s := NewStreamSession(0, 4096, 0)
	if s != nil {
		t.Error("expected nil for zero chunk size")
	}
}

func TestNewStreamSessionZeroTotalSize(t *testing.T) {
	s := NewStreamSession(0, 0, 1024)
	if s != nil {
		t.Error("expected nil for zero total size")
	}
}

func TestStreamSessionAddChunk(t *testing.T) {
	s := NewStreamSession(0, 2048, 1024)

	err := s.AddChunk(0, make([]byte, 1024))
	if err != nil {
		t.Fatalf("AddChunk(0): %v", err)
	}

	if s.IsComplete() {
		t.Error("should not be complete after 1/2 chunks")
	}

	err = s.AddChunk(1, make([]byte, 1024))
	if err != nil {
		t.Fatalf("AddChunk(1): %v", err)
	}

	if !s.IsComplete() {
		t.Error("should be complete after 2/2 chunks")
	}
}

func TestStreamSessionAddChunkDuplicate(t *testing.T) {
	s := NewStreamSession(0, 2048, 1024)
	s.AddChunk(0, make([]byte, 1024))

	err := s.AddChunk(0, make([]byte, 1024))
	if !errors.Is(err, ErrDuplicateChunk) {
		t.Errorf("expected ErrDuplicateChunk, got %v", err)
	}
}

func TestStreamSessionAddChunkOutOfRange(t *testing.T) {
	s := NewStreamSession(0, 1024, 1024) // 1 chunk
	err := s.AddChunk(5, make([]byte, 100))
	if !errors.Is(err, ErrChunkOutOfRange) {
		t.Errorf("expected ErrChunkOutOfRange, got %v", err)
	}
}

func TestStreamSessionAddChunkTooLarge(t *testing.T) {
	s := NewStreamSession(0, 2048, 1024)
	err := s.AddChunk(0, make([]byte, 2000)) // > chunkSize
	if !errors.Is(err, ErrChunkSizeMismatch) {
		t.Errorf("expected ErrChunkSizeMismatch, got %v", err)
	}
}

func TestStreamSessionAddChunkCancelled(t *testing.T) {
	s := NewStreamSession(0, 2048, 1024)
	s.cancelled = true

	err := s.AddChunk(0, make([]byte, 100))
	if err != ErrSessionCancelled {
		t.Errorf("expected ErrSessionCancelled, got %v", err)
	}
}

func TestStreamSessionMissingChunks(t *testing.T) {
	s := NewStreamSession(0, 4096, 1024) // 4 chunks
	s.AddChunk(1, make([]byte, 1024))

	missing := s.MissingChunks()
	if len(missing) != 3 {
		t.Fatalf("expected 3 missing chunks, got %d", len(missing))
	}
	// Should be [0, 2, 3]
	expected := []uint64{0, 2, 3}
	for i, v := range expected {
		if missing[i] != v {
			t.Errorf("missing[%d] = %d, want %d", i, missing[i], v)
		}
	}
}

func TestStreamSessionMissingChunksNone(t *testing.T) {
	s := NewStreamSession(0, 1024, 1024)
	s.AddChunk(0, make([]byte, 1024))

	missing := s.MissingChunks()
	if len(missing) != 0 {
		t.Errorf("expected 0 missing, got %d", len(missing))
	}
}

func TestStreamSessionProgress(t *testing.T) {
	s := NewStreamSession(0, 4096, 1024) // 4 chunks

	if p := s.Progress(); p != 0.0 {
		t.Errorf("initial progress = %f, want 0.0", p)
	}

	s.AddChunk(0, make([]byte, 1024))
	if p := s.Progress(); p != 0.25 {
		t.Errorf("after 1/4: progress = %f, want 0.25", p)
	}

	s.AddChunk(1, make([]byte, 1024))
	s.AddChunk(2, make([]byte, 1024))
	if p := s.Progress(); p != 0.75 {
		t.Errorf("after 3/4: progress = %f, want 0.75", p)
	}

	s.AddChunk(3, make([]byte, 1024))
	if p := s.Progress(); p != 1.0 {
		t.Errorf("after 4/4: progress = %f, want 1.0", p)
	}
}

func TestStreamSessionReceivedCount(t *testing.T) {
	s := NewStreamSession(0, 3072, 1024) // 3 chunks

	if s.ReceivedCount() != 0 {
		t.Errorf("initial received = %d, want 0", s.ReceivedCount())
	}
	s.AddChunk(0, make([]byte, 1024))
	s.AddChunk(2, make([]byte, 1024))
	if s.ReceivedCount() != 2 {
		t.Errorf("received = %d, want 2", s.ReceivedCount())
	}
}

func TestStreamSessionAssemble(t *testing.T) {
	s := NewStreamSession(0, 3000, 2048) // 2 chunks (2048 + 952)

	data0 := make([]byte, 2048)
	for i := range data0 {
		data0[i] = 0xAA
	}
	s.AddChunk(0, data0)

	data1 := make([]byte, 952) // last chunk, smaller
	for i := range data1 {
		data1[i] = 0xBB
	}
	s.AddChunk(1, data1)

	result, err := s.Assemble()
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if len(result) != 3000 {
		t.Fatalf("assembled size = %d, want 3000", len(result))
	}

	for i := 0; i < 2048; i++ {
		if result[i] != 0xAA {
			t.Fatalf("result[%d] = 0x%02x, want 0xAA", i, result[i])
		}
	}
	for i := 2048; i < 3000; i++ {
		if result[i] != 0xBB {
			t.Fatalf("result[%d] = 0x%02x, want 0xBB", i, result[i])
		}
	}
}

func TestStreamSessionAssembleIncomplete(t *testing.T) {
	s := NewStreamSession(0, 2048, 1024)
	s.AddChunk(0, make([]byte, 1024))

	_, err := s.Assemble()
	if !errors.Is(err, ErrIncompleteStream) {
		t.Errorf("expected ErrIncompleteStream, got %v", err)
	}
}

func TestStreamSessionAssembleCancelled(t *testing.T) {
	s := NewStreamSession(0, 1024, 1024)
	s.AddChunk(0, make([]byte, 1024))
	s.cancelled = true

	_, err := s.Assemble()
	if err != ErrSessionCancelled {
		t.Errorf("expected ErrSessionCancelled, got %v", err)
	}
}

func TestStreamSessionAssembleTrimming(t *testing.T) {
	// Total size not evenly divisible: 1500 / 1024 = 2 chunks
	s := NewStreamSession(0, 1500, 1024)

	// Chunk 0: full 1024 bytes
	chunk0 := bytes.Repeat([]byte{0x11}, 1024)
	s.AddChunk(0, chunk0)

	// Chunk 1: 1024 bytes but only 476 are "real"
	chunk1 := bytes.Repeat([]byte{0x22}, 1024)
	s.AddChunk(1, chunk1)

	result, err := s.Assemble()
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(result) != 1500 {
		t.Fatalf("len = %d, want 1500", len(result))
	}
}

func TestStreamSessionCopiesData(t *testing.T) {
	s := NewStreamSession(0, 1024, 1024)

	data := make([]byte, 1024)
	data[0] = 0x42
	s.AddChunk(0, data)

	// Mutate original data.
	data[0] = 0xFF

	result, err := s.Assemble()
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	// Should still have original value (defensive copy).
	if result[0] != 0x42 {
		t.Errorf("expected 0x42 (copy), got 0x%02x", result[0])
	}
}

func TestStreamSessionConcurrent(t *testing.T) {
	s := NewStreamSession(0, uint64(1024*8), 1024) // 8 chunks

	var wg sync.WaitGroup
	for i := uint64(0); i < 8; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			s.AddChunk(idx, make([]byte, 1024))
		}(i)
	}
	wg.Wait()

	if !s.IsComplete() {
		t.Error("expected complete after concurrent adds")
	}
	if s.ReceivedCount() != 8 {
		t.Errorf("received = %d, want 8", s.ReceivedCount())
	}
}

func TestStreamSessionUniqueIDs(t *testing.T) {
	s1 := NewStreamSession(0, 1024, 512)
	s2 := NewStreamSession(0, 1024, 512)
	if s1.ID == s2.ID {
		t.Error("expected unique session IDs, got identical")
	}
}

// --- StreamManager tests ---

func TestNewStreamManager(t *testing.T) {
	cfg := DefaultSessionConfig()
	m := NewStreamManager(cfg)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.ActiveStreams() != 0 {
		t.Errorf("initial active streams = %d, want 0", m.ActiveStreams())
	}
}

func TestStreamManagerStartStream(t *testing.T) {
	cfg := DefaultSessionConfig()
	m := NewStreamManager(cfg)

	session, err := m.StartStream(0, 4096)
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if session.BlobIndex != 0 {
		t.Errorf("BlobIndex = %d, want 0", session.BlobIndex)
	}
	if m.ActiveStreams() != 1 {
		t.Errorf("ActiveStreams = %d, want 1", m.ActiveStreams())
	}
}

func TestStreamManagerDuplicateBlob(t *testing.T) {
	cfg := DefaultSessionConfig()
	m := NewStreamManager(cfg)

	_, err := m.StartStream(0, 4096)
	if err != nil {
		t.Fatalf("first StartStream: %v", err)
	}

	_, err = m.StartStream(0, 4096) // same blob index
	if !errors.Is(err, ErrDuplicateSession) {
		t.Errorf("expected ErrDuplicateSession, got %v", err)
	}
}

func TestStreamManagerMaxSessions(t *testing.T) {
	cfg := StreamSessionConfig{
		MaxConcurrentStreams: 2,
		DefaultChunkSize:    1024,
		StreamTimeout:       time.Minute,
		MaxBlobSize:         100000,
	}
	m := NewStreamManager(cfg)

	m.StartStream(0, 1024)
	m.StartStream(1, 1024)

	_, err := m.StartStream(2, 1024)
	if err != ErrMaxSessionsReached {
		t.Errorf("expected ErrMaxSessionsReached, got %v", err)
	}
}

func TestStreamManagerZeroTotalSize(t *testing.T) {
	cfg := DefaultSessionConfig()
	m := NewStreamManager(cfg)

	_, err := m.StartStream(0, 0)
	if err != ErrZeroTotalSize {
		t.Errorf("expected ErrZeroTotalSize, got %v", err)
	}
}

func TestStreamManagerBlobTooLarge(t *testing.T) {
	cfg := DefaultSessionConfig()
	cfg.MaxBlobSize = 1024
	m := NewStreamManager(cfg)

	_, err := m.StartStream(0, 2048)
	if !errors.Is(err, ErrBlobTooLarge) {
		t.Errorf("expected ErrBlobTooLarge, got %v", err)
	}
}

func TestStreamManagerReceiveChunk(t *testing.T) {
	cfg := DefaultSessionConfig()
	m := NewStreamManager(cfg)

	session, _ := m.StartStream(0, 4096)

	err := m.ReceiveChunk(session.ID, 0, make([]byte, cfg.DefaultChunkSize))
	if err != nil {
		t.Fatalf("ReceiveChunk: %v", err)
	}
}

func TestStreamManagerReceiveChunkNotFound(t *testing.T) {
	cfg := DefaultSessionConfig()
	m := NewStreamManager(cfg)

	err := m.ReceiveChunk(types.Hash{0x99}, 0, nil)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestStreamManagerGetSession(t *testing.T) {
	cfg := DefaultSessionConfig()
	m := NewStreamManager(cfg)

	session, _ := m.StartStream(0, 4096)

	got, err := m.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != session.ID {
		t.Error("session ID mismatch")
	}
}

func TestStreamManagerGetSessionNotFound(t *testing.T) {
	cfg := DefaultSessionConfig()
	m := NewStreamManager(cfg)

	_, err := m.GetSession(types.Hash{0x01})
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestStreamManagerGetCompletedBlobs(t *testing.T) {
	cfg := DefaultSessionConfig()
	cfg.DefaultChunkSize = 1024
	m := NewStreamManager(cfg)

	s1, _ := m.StartStream(0, 1024)
	s2, _ := m.StartStream(1, 1024)

	// Complete only session 1.
	m.ReceiveChunk(s1.ID, 0, make([]byte, 1024))

	completed := m.GetCompletedBlobs()
	if len(completed) != 1 {
		t.Fatalf("expected 1 completed, got %d", len(completed))
	}
	if completed[0] != 0 {
		t.Errorf("completed[0] = %d, want 0", completed[0])
	}

	// Complete session 2.
	m.ReceiveChunk(s2.ID, 0, make([]byte, 1024))
	completed = m.GetCompletedBlobs()
	if len(completed) != 2 {
		t.Fatalf("expected 2 completed, got %d", len(completed))
	}
}

func TestStreamManagerCancelStream(t *testing.T) {
	cfg := DefaultSessionConfig()
	m := NewStreamManager(cfg)

	session, _ := m.StartStream(0, 4096)

	err := m.CancelStream(session.ID)
	if err != nil {
		t.Fatalf("CancelStream: %v", err)
	}
	if m.ActiveStreams() != 0 {
		t.Errorf("ActiveStreams = %d, want 0", m.ActiveStreams())
	}

	// Further operations on cancelled session should fail.
	err = session.AddChunk(0, make([]byte, 100))
	if err != ErrSessionCancelled {
		t.Errorf("expected ErrSessionCancelled, got %v", err)
	}
}

func TestStreamManagerCancelStreamNotFound(t *testing.T) {
	cfg := DefaultSessionConfig()
	m := NewStreamManager(cfg)

	err := m.CancelStream(types.Hash{0x01})
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestStreamManagerCancelFreesSlot(t *testing.T) {
	cfg := StreamSessionConfig{
		MaxConcurrentStreams: 1,
		DefaultChunkSize:    1024,
		StreamTimeout:       time.Minute,
		MaxBlobSize:         100000,
	}
	m := NewStreamManager(cfg)

	s, _ := m.StartStream(0, 1024)

	// Can't start another, at max.
	_, err := m.StartStream(1, 1024)
	if err != ErrMaxSessionsReached {
		t.Fatalf("expected ErrMaxSessionsReached, got %v", err)
	}

	// Cancel frees the slot.
	m.CancelStream(s.ID)

	_, err = m.StartStream(1, 1024)
	if err != nil {
		t.Fatalf("StartStream after cancel: %v", err)
	}
}

func TestStreamManagerFullWorkflow(t *testing.T) {
	cfg := StreamSessionConfig{
		MaxConcurrentStreams: 4,
		DefaultChunkSize:    512,
		StreamTimeout:       time.Minute,
		MaxBlobSize:         100000,
	}
	m := NewStreamManager(cfg)

	// Start a stream for a 1536-byte blob (3 chunks of 512).
	session, err := m.StartStream(42, 1536)
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}

	// Check initial state.
	if session.Progress() != 0.0 {
		t.Errorf("initial progress = %f", session.Progress())
	}
	if session.IsComplete() {
		t.Error("should not be complete initially")
	}

	missing := session.MissingChunks()
	if len(missing) != 3 {
		t.Fatalf("expected 3 missing, got %d", len(missing))
	}

	// Deliver chunks out of order.
	chunk2 := bytes.Repeat([]byte{0xCC}, 512)
	m.ReceiveChunk(session.ID, 2, chunk2)

	chunk0 := bytes.Repeat([]byte{0xAA}, 512)
	m.ReceiveChunk(session.ID, 0, chunk0)

	chunk1 := bytes.Repeat([]byte{0xBB}, 512)
	m.ReceiveChunk(session.ID, 1, chunk1)

	// Should be complete.
	if !session.IsComplete() {
		t.Error("should be complete")
	}
	if session.Progress() != 1.0 {
		t.Errorf("progress = %f, want 1.0", session.Progress())
	}

	// Assemble and verify.
	data, err := session.Assemble()
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(data) != 1536 {
		t.Fatalf("assembled len = %d, want 1536", len(data))
	}

	// Verify content order.
	for i := 0; i < 512; i++ {
		if data[i] != 0xAA {
			t.Fatalf("data[%d] = 0x%02x, want 0xAA", i, data[i])
		}
	}
	for i := 512; i < 1024; i++ {
		if data[i] != 0xBB {
			t.Fatalf("data[%d] = 0x%02x, want 0xBB", i, data[i])
		}
	}
	for i := 1024; i < 1536; i++ {
		if data[i] != 0xCC {
			t.Fatalf("data[%d] = 0x%02x, want 0xCC", i, data[i])
		}
	}

	// Check completed blobs.
	completed := m.GetCompletedBlobs()
	if len(completed) != 1 || completed[0] != 42 {
		t.Errorf("completed = %v, want [42]", completed)
	}
}

func TestStreamManagerConcurrentStartAndReceive(t *testing.T) {
	cfg := StreamSessionConfig{
		MaxConcurrentStreams: 100,
		DefaultChunkSize:    256,
		StreamTimeout:       time.Minute,
		MaxBlobSize:         100000,
	}
	m := NewStreamManager(cfg)

	var wg sync.WaitGroup

	// Start 10 streams concurrently.
	sessions := make([]*StreamSession, 10)
	var mu sync.Mutex
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s, err := m.StartStream(uint64(idx), 512)
			if err != nil {
				t.Errorf("StartStream(%d): %v", idx, err)
				return
			}
			mu.Lock()
			sessions[idx] = s
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if m.ActiveStreams() != 10 {
		t.Errorf("ActiveStreams = %d, want 10", m.ActiveStreams())
	}

	// Deliver chunks concurrently.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			mu.Lock()
			s := sessions[idx]
			mu.Unlock()
			if s == nil {
				return
			}
			m.ReceiveChunk(s.ID, 0, make([]byte, 256))
			m.ReceiveChunk(s.ID, 1, make([]byte, 256))
		}(i)
	}
	wg.Wait()

	completed := m.GetCompletedBlobs()
	if len(completed) != 10 {
		t.Errorf("completed = %d, want 10", len(completed))
	}
}

func TestStreamManagerCleanupExpired(t *testing.T) {
	cfg := StreamSessionConfig{
		MaxConcurrentStreams: 10,
		DefaultChunkSize:    1024,
		StreamTimeout:       1 * time.Millisecond, // very short timeout
		MaxBlobSize:         100000,
	}
	m := NewStreamManager(cfg)

	m.StartStream(0, 1024)
	m.StartStream(1, 1024)

	// Wait for timeout.
	time.Sleep(5 * time.Millisecond)

	cleaned := m.CleanupExpired()
	if cleaned != 2 {
		t.Errorf("cleaned = %d, want 2", cleaned)
	}
	if m.ActiveStreams() != 0 {
		t.Errorf("ActiveStreams = %d, want 0", m.ActiveStreams())
	}
}

func TestDefaultSessionConfig(t *testing.T) {
	cfg := DefaultSessionConfig()
	if cfg.MaxConcurrentStreams != 16 {
		t.Errorf("MaxConcurrentStreams = %d, want 16", cfg.MaxConcurrentStreams)
	}
	if cfg.DefaultChunkSize != uint64(BytesPerCell) {
		t.Errorf("DefaultChunkSize = %d, want %d", cfg.DefaultChunkSize, BytesPerCell)
	}
	if cfg.StreamTimeout != 30*time.Second {
		t.Errorf("StreamTimeout = %v, want 30s", cfg.StreamTimeout)
	}
	if cfg.MaxBlobSize != 128*1024 {
		t.Errorf("MaxBlobSize = %d, want %d", cfg.MaxBlobSize, 128*1024)
	}
}
