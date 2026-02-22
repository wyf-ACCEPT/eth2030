package das

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/sha3"
)

// Blob streaming errors.
var (
	ErrStreamClosed      = errors.New("das: stream is closed")
	ErrDuplicateChunk    = errors.New("das: duplicate chunk index")
	ErrChunkOutOfRange   = errors.New("das: chunk index out of range")
	ErrChunkVerification = errors.New("das: chunk proof verification failed")
	ErrMaxStreams        = errors.New("das: max concurrent streams reached")
	ErrStreamNotFound    = errors.New("das: stream not found")
	ErrIncompleteStream  = errors.New("das: stream is not complete")
)

// StreamConfig configures the blob streaming protocol.
type StreamConfig struct {
	// ChunkSize is the size of each chunk in bytes.
	ChunkSize uint32

	// MaxConcurrentStreams is the maximum number of concurrent streams.
	MaxConcurrentStreams int

	// Timeout is the maximum duration to wait for a stream to complete.
	Timeout time.Duration
}

// DefaultStreamConfig returns a default streaming configuration.
func DefaultStreamConfig() StreamConfig {
	return StreamConfig{
		ChunkSize:            BytesPerCell, // 2048 bytes per chunk (one cell)
		MaxConcurrentStreams: 16,
		Timeout:              30 * time.Second,
	}
}

// BlobChunk represents a single chunk of a blob being streamed.
type BlobChunk struct {
	// Index is the position of this chunk in the blob.
	Index uint32

	// Data is the chunk payload.
	Data []byte

	// Proof is the KZG sub-proof for progressive verification.
	Proof []byte
}

// ChunkCallback is invoked when a chunk is received and verified.
type ChunkCallback func(chunk *BlobChunk)

// BlobStream tracks the progressive reception of a streaming blob.
type BlobStream struct {
	mu         sync.Mutex
	chunks     []*BlobChunk
	received   map[uint32]bool
	complete   bool
	closed     bool
	commitment [32]byte
	totalSize  uint32
	numChunks  uint32
	chunkSize  uint32
	onChunk    ChunkCallback
}

// newBlobStream creates a new blob stream.
func newBlobStream(commitment [32]byte, totalSize, chunkSize uint32, cb ChunkCallback) *BlobStream {
	numChunks := totalSize / chunkSize
	if totalSize%chunkSize != 0 {
		numChunks++
	}
	return &BlobStream{
		chunks:     make([]*BlobChunk, numChunks),
		received:   make(map[uint32]bool),
		commitment: commitment,
		totalSize:  totalSize,
		numChunks:  numChunks,
		chunkSize:  chunkSize,
		onChunk:    cb,
	}
}

// AddChunk adds a chunk to the stream. Returns an error if the chunk index is
// invalid, duplicated, or the stream is closed.
func (s *BlobStream) AddChunk(chunk *BlobChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStreamClosed
	}
	if chunk.Index >= s.numChunks {
		return fmt.Errorf("%w: index %d >= %d", ErrChunkOutOfRange, chunk.Index, s.numChunks)
	}
	if s.received[chunk.Index] {
		return fmt.Errorf("%w: index %d", ErrDuplicateChunk, chunk.Index)
	}

	// Progressive verification: hash(commitment || index || data) == proof.
	if len(chunk.Proof) > 0 && !verifyChunkProof(s.commitment, chunk) {
		return ErrChunkVerification
	}

	s.chunks[chunk.Index] = chunk
	s.received[chunk.Index] = true

	if uint32(len(s.received)) == s.numChunks {
		s.complete = true
	}

	if s.onChunk != nil {
		s.onChunk(chunk)
	}

	return nil
}

// IsComplete returns true if all chunks have been received.
func (s *BlobStream) IsComplete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.complete
}

// Progress returns the fraction of chunks received (0.0 to 1.0).
func (s *BlobStream) Progress() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.numChunks == 0 {
		return 1.0
	}
	return float64(len(s.received)) / float64(s.numChunks)
}

// Assemble concatenates all received chunks into the full blob data.
// Returns an error if the stream is not yet complete.
func (s *BlobStream) Assemble() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.complete {
		return nil, fmt.Errorf("%w: have %d/%d chunks",
			ErrIncompleteStream, len(s.received), s.numChunks)
	}

	data := make([]byte, 0, s.totalSize)
	for _, chunk := range s.chunks {
		data = append(data, chunk.Data...)
	}

	// Trim to exact total size (last chunk may be padded).
	if uint32(len(data)) > s.totalSize {
		data = data[:s.totalSize]
	}

	return data, nil
}

// Close marks the stream as closed, rejecting further chunks.
func (s *BlobStream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

// BlobStreamer manages progressive blob transmissions.
type BlobStreamer struct {
	mu      sync.Mutex
	streams map[[32]byte]*BlobStream
	config  StreamConfig
}

// NewBlobStreamer creates a new BlobStreamer with the given configuration.
func NewBlobStreamer(config StreamConfig) *BlobStreamer {
	return &BlobStreamer{
		streams: make(map[[32]byte]*BlobStream),
		config:  config,
	}
}

// StartStream begins a new stream for the given blob hash.
// totalSize is the expected total blob size in bytes.
func (bs *BlobStreamer) StartStream(blobHash [32]byte, totalSize uint32, cb ChunkCallback) (*BlobStream, error) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if len(bs.streams) >= bs.config.MaxConcurrentStreams {
		return nil, ErrMaxStreams
	}

	if existing, ok := bs.streams[blobHash]; ok {
		return existing, nil
	}

	stream := newBlobStream(blobHash, totalSize, bs.config.ChunkSize, cb)
	bs.streams[blobHash] = stream
	return stream, nil
}

// GetStream returns an active stream by blob hash.
func (bs *BlobStreamer) GetStream(blobHash [32]byte) (*BlobStream, error) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	stream, ok := bs.streams[blobHash]
	if !ok {
		return nil, ErrStreamNotFound
	}
	return stream, nil
}

// CloseStream closes and removes a stream.
func (bs *BlobStreamer) CloseStream(blobHash [32]byte) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if stream, ok := bs.streams[blobHash]; ok {
		stream.Close()
		delete(bs.streams, blobHash)
	}
}

// ActiveStreams returns the number of currently active streams.
func (bs *BlobStreamer) ActiveStreams() int {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return len(bs.streams)
}

// ValidateStreamConfig checks that a StreamConfig has valid parameters:
// non-zero chunk size, positive max streams, and positive timeout.
func ValidateStreamConfig(cfg StreamConfig) error {
	if cfg.ChunkSize == 0 {
		return errors.New("das: chunk size must be > 0")
	}
	if cfg.MaxConcurrentStreams <= 0 {
		return errors.New("das: max concurrent streams must be > 0")
	}
	if cfg.Timeout <= 0 {
		return errors.New("das: timeout must be > 0")
	}
	return nil
}

// ValidateBlobChunk checks that a blob chunk is well-formed.
func ValidateBlobChunk(chunk *BlobChunk, chunkSize uint32) error {
	if chunk == nil {
		return errors.New("das: nil chunk")
	}
	if len(chunk.Data) == 0 {
		return errors.New("das: empty chunk data")
	}
	if chunkSize > 0 && uint32(len(chunk.Data)) > chunkSize {
		return fmt.Errorf("das: chunk data %d exceeds chunk size %d", len(chunk.Data), chunkSize)
	}
	return nil
}

// verifyChunkProof verifies a chunk's proof using a simple hash-based scheme.
// In production this would use KZG sub-proofs; here we use
// hash(commitment || index || data) for testing.
func verifyChunkProof(commitment [32]byte, chunk *BlobChunk) bool {
	expected := computeChunkProof(commitment, chunk.Index, chunk.Data)
	if len(chunk.Proof) != len(expected) {
		return false
	}
	for i := range expected {
		if chunk.Proof[i] != expected[i] {
			return false
		}
	}
	return true
}

// computeChunkProof computes a proof for a chunk: hash(commitment || index || data).
func computeChunkProof(commitment [32]byte, index uint32, data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(commitment[:])
	idxBytes := [4]byte{
		byte(index), byte(index >> 8), byte(index >> 16), byte(index >> 24),
	}
	h.Write(idxBytes[:])
	h.Write(data)
	return h.Sum(nil)
}
