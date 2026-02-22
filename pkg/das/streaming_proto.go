package das

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Blob Streaming Protocol (DL roadmap: blob streaming -> short-dated blob futures)
//
// Manages progressive blob reception with chunk-level tracking, session
// management, and concurrent stream coordination. Builds on the lower-level
// BlobStreamer with session-based semantics and types.Hash-based IDs.

// Streaming protocol errors.
var (
	ErrSessionNotFound    = errors.New("das: session not found")
	ErrSessionComplete    = errors.New("das: session already complete")
	ErrSessionCancelled   = errors.New("das: session was cancelled")
	ErrChunkSizeMismatch  = errors.New("das: chunk data size exceeds chunk size")
	ErrBlobTooLarge       = errors.New("das: blob exceeds maximum size")
	ErrMaxSessionsReached = errors.New("das: max concurrent sessions reached")
	ErrDuplicateSession   = errors.New("das: session for blob index already exists")
	ErrZeroChunkSize      = errors.New("das: chunk size must be > 0")
	ErrZeroTotalSize      = errors.New("das: total size must be > 0")
)

// StreamSessionConfig holds per-session configuration.
type StreamSessionConfig struct {
	MaxConcurrentStreams int
	DefaultChunkSize     uint64
	StreamTimeout        time.Duration
	MaxBlobSize          uint64
}

// DefaultSessionConfig returns sensible defaults for session management.
func DefaultSessionConfig() StreamSessionConfig {
	return StreamSessionConfig{
		MaxConcurrentStreams: 16,
		DefaultChunkSize:     uint64(BytesPerCell), // 2048
		StreamTimeout:        30 * time.Second,
		MaxBlobSize:          128 * 1024, // 128 KiB
	}
}

// StreamSession manages a single blob streaming session, tracking chunks
// received, missing pieces, and assembly of the final blob.
type StreamSession struct {
	mu        sync.RWMutex
	ID        types.Hash
	BlobIndex uint64
	totalSize uint64
	chunkSize uint64
	numChunks uint64
	chunks    [][]byte
	received  map[uint64]bool
	cancelled bool
	createdAt time.Time
}

// NewStreamSession creates a new streaming session for a blob.
func NewStreamSession(blobIndex uint64, totalSize uint64, chunkSize uint64) *StreamSession {
	if chunkSize == 0 || totalSize == 0 {
		return nil
	}

	numChunks := totalSize / chunkSize
	if totalSize%chunkSize != 0 {
		numChunks++
	}

	// Derive session ID from blob index + total size + creation entropy.
	idData := make([]byte, 24)
	putLE64(idData[0:8], blobIndex)
	putLE64(idData[8:16], totalSize)
	putLE64(idData[16:24], uint64(time.Now().UnixNano()))
	id := crypto.Keccak256Hash(idData)

	return &StreamSession{
		ID:        id,
		BlobIndex: blobIndex,
		totalSize: totalSize,
		chunkSize: chunkSize,
		numChunks: numChunks,
		chunks:    make([][]byte, numChunks),
		received:  make(map[uint64]bool),
		createdAt: time.Now(),
	}
}

// AddChunk adds a received chunk at the given index. Returns an error
// if the index is out of range, the chunk is a duplicate, or the
// session is cancelled.
func (s *StreamSession) AddChunk(index uint64, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancelled {
		return ErrSessionCancelled
	}
	if index >= s.numChunks {
		return fmt.Errorf("%w: index %d >= %d", ErrChunkOutOfRange, index, s.numChunks)
	}
	if s.received[index] {
		return fmt.Errorf("%w: index %d", ErrDuplicateChunk, index)
	}
	if uint64(len(data)) > s.chunkSize {
		return fmt.Errorf("%w: got %d, max %d", ErrChunkSizeMismatch, len(data), s.chunkSize)
	}

	// Store a copy of the data.
	stored := make([]byte, len(data))
	copy(stored, data)
	s.chunks[index] = stored
	s.received[index] = true

	return nil
}

// MissingChunks returns the indices of chunks not yet received.
func (s *StreamSession) MissingChunks() []uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	missing := make([]uint64, 0, s.numChunks-uint64(len(s.received)))
	for i := uint64(0); i < s.numChunks; i++ {
		if !s.received[i] {
			missing = append(missing, i)
		}
	}
	return missing
}

// IsComplete returns true if all chunks have been received.
func (s *StreamSession) IsComplete() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return uint64(len(s.received)) == s.numChunks
}

// Assemble concatenates all received chunks into the full blob data.
// Returns an error if the session is incomplete or cancelled.
func (s *StreamSession) Assemble() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cancelled {
		return nil, ErrSessionCancelled
	}
	if uint64(len(s.received)) != s.numChunks {
		return nil, fmt.Errorf("%w: have %d/%d chunks",
			ErrIncompleteStream, len(s.received), s.numChunks)
	}

	data := make([]byte, 0, s.totalSize)
	for _, chunk := range s.chunks {
		data = append(data, chunk...)
	}

	// Trim to exact size (last chunk may be shorter).
	if uint64(len(data)) > s.totalSize {
		data = data[:s.totalSize]
	}

	return data, nil
}

// Progress returns the completion percentage as a float in [0.0, 1.0].
func (s *StreamSession) Progress() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.numChunks == 0 {
		return 1.0
	}
	return float64(len(s.received)) / float64(s.numChunks)
}

// NumChunks returns the total number of expected chunks.
func (s *StreamSession) NumChunks() uint64 {
	return s.numChunks
}

// ReceivedCount returns the number of chunks received so far.
func (s *StreamSession) ReceivedCount() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return uint64(len(s.received))
}

// StreamManager coordinates multiple concurrent streaming sessions.
type StreamManager struct {
	mu       sync.RWMutex
	sessions map[types.Hash]*StreamSession
	byBlob   map[uint64]types.Hash // blobIndex -> sessionID
	config   StreamSessionConfig
}

// NewStreamManager creates a new stream manager with the given config.
func NewStreamManager(config StreamSessionConfig) *StreamManager {
	return &StreamManager{
		sessions: make(map[types.Hash]*StreamSession),
		byBlob:   make(map[uint64]types.Hash),
		config:   config,
	}
}

// StartStream creates a new streaming session for the given blob index.
func (m *StreamManager) StartStream(blobIndex uint64, totalSize uint64) (*StreamSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if session already exists for this blob.
	if _, exists := m.byBlob[blobIndex]; exists {
		return nil, fmt.Errorf("%w: blob %d", ErrDuplicateSession, blobIndex)
	}

	// Check concurrent session limit.
	if len(m.sessions) >= m.config.MaxConcurrentStreams {
		return nil, ErrMaxSessionsReached
	}

	// Validate blob size.
	if totalSize == 0 {
		return nil, ErrZeroTotalSize
	}
	if totalSize > m.config.MaxBlobSize {
		return nil, fmt.Errorf("%w: %d > %d", ErrBlobTooLarge, totalSize, m.config.MaxBlobSize)
	}

	chunkSize := m.config.DefaultChunkSize
	if chunkSize == 0 {
		return nil, ErrZeroChunkSize
	}

	session := NewStreamSession(blobIndex, totalSize, chunkSize)
	if session == nil {
		return nil, ErrZeroChunkSize
	}

	m.sessions[session.ID] = session
	m.byBlob[blobIndex] = session.ID

	return session, nil
}

// ReceiveChunk delivers a chunk to the session identified by sessionID.
func (m *StreamManager) ReceiveChunk(sessionID types.Hash, chunkIndex uint64, data []byte) error {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID.Hex())
	}

	return session.AddChunk(chunkIndex, data)
}

// GetSession returns a session by ID.
func (m *StreamManager) GetSession(sessionID types.Hash) (*StreamSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID.Hex())
	}
	return session, nil
}

// GetCompletedBlobs returns the blob indices of all completed sessions.
func (m *StreamManager) GetCompletedBlobs() []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var completed []uint64
	for _, session := range m.sessions {
		if session.IsComplete() {
			completed = append(completed, session.BlobIndex)
		}
	}
	return completed
}

// CancelStream cancels a streaming session and removes it.
func (m *StreamManager) CancelStream(sessionID types.Hash) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID.Hex())
	}

	session.mu.Lock()
	session.cancelled = true
	session.mu.Unlock()

	delete(m.byBlob, session.BlobIndex)
	delete(m.sessions, sessionID)
	return nil
}

// ActiveStreams returns the number of active (non-cancelled) sessions.
func (m *StreamManager) ActiveStreams() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// CleanupExpired removes sessions that have exceeded the stream timeout.
// Returns the number of sessions cleaned up.
func (m *StreamManager) CleanupExpired() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	cleaned := 0

	for id, session := range m.sessions {
		if now.Sub(session.createdAt) > m.config.StreamTimeout {
			session.mu.Lock()
			session.cancelled = true
			session.mu.Unlock()
			delete(m.byBlob, session.BlobIndex)
			delete(m.sessions, id)
			cleaned++
		}
	}

	return cleaned
}

// putLE64 writes a uint64 as 8 little-endian bytes.
func putLE64(b []byte, v uint64) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
	b[4] = byte(v >> 32)
	b[5] = byte(v >> 40)
	b[6] = byte(v >> 48)
	b[7] = byte(v >> 56)
}
