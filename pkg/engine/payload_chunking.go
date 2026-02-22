// payload_chunking.go splits large execution payloads into chunks for efficient
// network propagation. Part of the J+ era roadmap: as block sizes grow with
// increased gas limits, chunked propagation enables progressive validation and
// reduces peak bandwidth requirements.
package engine

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/crypto"
)

// Default chunking parameters.
const (
	// DefaultChunkSize is the default maximum chunk size in bytes (128 KiB).
	DefaultChunkSize = 128 * 1024

	// MinChunkSize is the minimum allowed chunk size (1 KiB).
	MinChunkSize = 1024

	// MaxChunkSize is the maximum allowed chunk size (1 MiB).
	MaxChunkSize = 1024 * 1024

	// MaxChunksPerPayload prevents excessive fragmentation.
	MaxChunksPerPayload = 4096
)

// Chunking errors.
var (
	ErrChunkEmpty        = errors.New("chunk: empty payload")
	ErrChunkSizeInvalid  = errors.New("chunk: invalid chunk size")
	ErrChunkMissing      = errors.New("chunk: missing chunks for reassembly")
	ErrChunkDuplicate    = errors.New("chunk: duplicate chunk index")
	ErrChunkIndexInvalid = errors.New("chunk: index out of range")
	ErrChunkIntegrity    = errors.New("chunk: integrity check failed")
	ErrChunkCountExceed  = errors.New("chunk: too many chunks")
	ErrChunkTotalMismatch = errors.New("chunk: total count mismatch")
	ErrChunkParentHash   = errors.New("chunk: parent hash mismatch")
)

// PayloadChunk represents a single chunk of a split execution payload.
type PayloadChunk struct {
	// Index is the zero-based position of this chunk in the sequence.
	Index uint32
	// Total is the total number of chunks in the payload.
	Total uint32
	// Data is the raw chunk data.
	Data []byte
	// Hash is the Keccak-256 hash of this chunk's Data.
	Hash [32]byte
	// ParentHash is the hash of the complete (unchunked) payload.
	ParentHash [32]byte
}

// PayloadChunker handles splitting and reassembling execution payloads.
type PayloadChunker struct {
	chunkSize int
}

// NewPayloadChunker creates a chunker with the specified chunk size.
// If size is 0, DefaultChunkSize is used. Panics if size is out of valid range.
func NewPayloadChunker(chunkSize int) *PayloadChunker {
	if chunkSize == 0 {
		chunkSize = DefaultChunkSize
	}
	if chunkSize < MinChunkSize || chunkSize > MaxChunkSize {
		panic(fmt.Sprintf("chunk size %d outside valid range [%d, %d]",
			chunkSize, MinChunkSize, MaxChunkSize))
	}
	return &PayloadChunker{chunkSize: chunkSize}
}

// ChunkSize returns the configured chunk size.
func (pc *PayloadChunker) ChunkSize() int {
	return pc.chunkSize
}

// ChunkPayload splits a payload into ordered chunks. Each chunk contains a
// slice of the payload data, a hash of its data, and the hash of the full
// payload for identity linking.
func (pc *PayloadChunker) ChunkPayload(payload []byte) ([]*PayloadChunk, error) {
	if len(payload) == 0 {
		return nil, ErrChunkEmpty
	}

	numChunks := (len(payload) + pc.chunkSize - 1) / pc.chunkSize
	if numChunks > MaxChunksPerPayload {
		return nil, fmt.Errorf("%w: %d chunks exceeds maximum %d",
			ErrChunkCountExceed, numChunks, MaxChunksPerPayload)
	}

	// Hash the full payload for identity.
	parentHash := crypto.Keccak256Hash(payload)

	chunks := make([]*PayloadChunk, 0, numChunks)
	for i := 0; i < numChunks; i++ {
		start := i * pc.chunkSize
		end := start + pc.chunkSize
		if end > len(payload) {
			end = len(payload)
		}

		data := make([]byte, end-start)
		copy(data, payload[start:end])

		chunkHash := crypto.Keccak256Hash(data)

		chunks = append(chunks, &PayloadChunk{
			Index:      uint32(i),
			Total:      uint32(numChunks),
			Data:       data,
			Hash:       chunkHash,
			ParentHash: parentHash,
		})
	}

	return chunks, nil
}

// ReassemblePayload reconstructs the original payload from an ordered set of
// chunks. Verifies chunk integrity and completeness.
func (pc *PayloadChunker) ReassemblePayload(chunks []*PayloadChunk) ([]byte, error) {
	if len(chunks) == 0 {
		return nil, ErrChunkMissing
	}

	total := chunks[0].Total
	parentHash := chunks[0].ParentHash

	if uint32(len(chunks)) != total {
		return nil, fmt.Errorf("%w: have %d chunks, expected %d",
			ErrChunkMissing, len(chunks), total)
	}

	// Build an indexed map and verify consistency.
	indexed := make(map[uint32]*PayloadChunk, len(chunks))
	for _, chunk := range chunks {
		if chunk.Total != total {
			return nil, fmt.Errorf("%w: chunk %d says total=%d, expected %d",
				ErrChunkTotalMismatch, chunk.Index, chunk.Total, total)
		}
		if chunk.ParentHash != parentHash {
			return nil, ErrChunkParentHash
		}
		if chunk.Index >= total {
			return nil, fmt.Errorf("%w: index %d >= total %d",
				ErrChunkIndexInvalid, chunk.Index, total)
		}
		if _, dup := indexed[chunk.Index]; dup {
			return nil, fmt.Errorf("%w: index %d", ErrChunkDuplicate, chunk.Index)
		}

		// Verify chunk data integrity.
		if !VerifyChunkIntegrity(chunk) {
			return nil, fmt.Errorf("%w: chunk %d", ErrChunkIntegrity, chunk.Index)
		}

		indexed[chunk.Index] = chunk
	}

	// Reassemble in order.
	totalSize := 0
	for i := uint32(0); i < total; i++ {
		c, ok := indexed[i]
		if !ok {
			return nil, fmt.Errorf("%w: missing chunk %d", ErrChunkMissing, i)
		}
		totalSize += len(c.Data)
	}

	result := make([]byte, 0, totalSize)
	for i := uint32(0); i < total; i++ {
		result = append(result, indexed[i].Data...)
	}

	// Verify reassembled payload hash matches the parent hash.
	reassembledHash := crypto.Keccak256Hash(result)
	if reassembledHash != parentHash {
		return nil, fmt.Errorf("%w: reassembled payload hash mismatch", ErrChunkIntegrity)
	}

	return result, nil
}

// VerifyChunkIntegrity verifies that a chunk's data hash matches its declared Hash.
func VerifyChunkIntegrity(chunk *PayloadChunk) bool {
	if chunk == nil || len(chunk.Data) == 0 {
		return false
	}
	computed := crypto.Keccak256Hash(chunk.Data)
	return computed == chunk.Hash
}

// ChunkSet collects chunks for a given payload and tracks completion.
// It is safe for concurrent use.
type ChunkSet struct {
	mu         sync.RWMutex
	parentHash [32]byte
	total      uint32
	chunks     map[uint32]*PayloadChunk
	complete   bool
}

// NewChunkSet creates a new chunk set expecting chunks with the given parent hash.
func NewChunkSet(parentHash [32]byte, total uint32) *ChunkSet {
	return &ChunkSet{
		parentHash: parentHash,
		total:      total,
		chunks:     make(map[uint32]*PayloadChunk, total),
	}
}

// AddChunk adds a chunk to the set. Returns an error if the chunk is invalid
// or a duplicate. Returns true if the set is now complete.
func (cs *ChunkSet) AddChunk(chunk *PayloadChunk) (bool, error) {
	if chunk == nil {
		return false, ErrChunkEmpty
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.complete {
		return true, nil
	}

	if chunk.ParentHash != cs.parentHash {
		return false, ErrChunkParentHash
	}
	if chunk.Total != cs.total {
		return false, fmt.Errorf("%w: chunk says total=%d, set expects %d",
			ErrChunkTotalMismatch, chunk.Total, cs.total)
	}
	if chunk.Index >= cs.total {
		return false, fmt.Errorf("%w: index %d >= total %d",
			ErrChunkIndexInvalid, chunk.Index, cs.total)
	}
	if _, dup := cs.chunks[chunk.Index]; dup {
		return false, fmt.Errorf("%w: index %d", ErrChunkDuplicate, chunk.Index)
	}

	if !VerifyChunkIntegrity(chunk) {
		return false, fmt.Errorf("%w: chunk %d", ErrChunkIntegrity, chunk.Index)
	}

	cs.chunks[chunk.Index] = chunk

	if uint32(len(cs.chunks)) == cs.total {
		cs.complete = true
		return true, nil
	}

	return false, nil
}

// IsComplete returns true if all chunks have been received.
func (cs *ChunkSet) IsComplete() bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.complete
}

// Received returns the number of chunks received so far.
func (cs *ChunkSet) Received() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.chunks)
}

// Total returns the expected total number of chunks.
func (cs *ChunkSet) Total() uint32 {
	return cs.total
}

// Missing returns the indices of chunks not yet received.
func (cs *ChunkSet) Missing() []uint32 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	var missing []uint32
	for i := uint32(0); i < cs.total; i++ {
		if _, ok := cs.chunks[i]; !ok {
			missing = append(missing, i)
		}
	}
	return missing
}

// Chunks returns all received chunks in order. Returns nil if not complete.
func (cs *ChunkSet) Chunks() []*PayloadChunk {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if !cs.complete {
		return nil
	}

	ordered := make([]*PayloadChunk, cs.total)
	for i := uint32(0); i < cs.total; i++ {
		ordered[i] = cs.chunks[i]
	}
	return ordered
}

// ParentHash returns the expected parent hash for this chunk set.
func (cs *ChunkSet) ParentHash() [32]byte {
	return cs.parentHash
}

// Progress returns the completion percentage (0.0 to 1.0).
func (cs *ChunkSet) Progress() float64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if cs.total == 0 {
		return 0
	}
	return float64(len(cs.chunks)) / float64(cs.total)
}
