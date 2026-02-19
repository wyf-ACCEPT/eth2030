package core

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/eth2028/eth2028/crypto"
)

const (
	// DefaultMaxChunkSize is the default maximum size of a payload chunk (128 KB).
	DefaultMaxChunkSize = 128 * 1024
)

// PayloadChunk represents a single chunk of a larger payload, with merkle proof data.
type PayloadChunk struct {
	ChunkIndex  uint16
	TotalChunks uint16
	PayloadID   [32]byte // hash of the complete payload
	Data        []byte
	ProofHash   [32]byte // merkle proof hash for verification
}

// ChunkPayload splits a payload into chunks of at most maxChunkSize bytes.
// If maxChunkSize <= 0, DefaultMaxChunkSize is used.
func ChunkPayload(payload []byte, maxChunkSize int) []PayloadChunk {
	if maxChunkSize <= 0 {
		maxChunkSize = DefaultMaxChunkSize
	}

	// Compute payload ID as keccak256 of the entire payload.
	var payloadID [32]byte
	copy(payloadID[:], crypto.Keccak256(payload))

	// Determine chunk count.
	n := len(payload)
	total := (n + maxChunkSize - 1) / maxChunkSize
	if total == 0 {
		total = 1 // at least one chunk, even for empty payload
	}

	// Split payload into chunk data slices.
	chunkData := make([][]byte, total)
	for i := range total {
		start := i * maxChunkSize
		end := start + maxChunkSize
		if end > n {
			end = n
		}
		chunkData[i] = payload[start:end]
	}

	// Build merkle tree over chunk hashes for proof verification.
	proofHashes := computeMerkleProofs(chunkData)

	chunks := make([]PayloadChunk, total)
	for i := range total {
		chunks[i] = PayloadChunk{
			ChunkIndex:  uint16(i),
			TotalChunks: uint16(total),
			PayloadID:   payloadID,
			Data:        chunkData[i],
			ProofHash:   proofHashes[i],
		}
	}
	return chunks
}

// ReassemblePayload reconstructs a payload from its chunks.
func ReassemblePayload(chunks []PayloadChunk) ([]byte, error) {
	if len(chunks) == 0 {
		return nil, errors.New("no chunks provided")
	}

	totalChunks := chunks[0].TotalChunks
	payloadID := chunks[0].PayloadID

	// Validate all chunks share the same metadata.
	for i, c := range chunks {
		if c.TotalChunks != totalChunks {
			return nil, fmt.Errorf("chunk %d: TotalChunks mismatch (got %d, want %d)", i, c.TotalChunks, totalChunks)
		}
		if c.PayloadID != payloadID {
			return nil, fmt.Errorf("chunk %d: PayloadID mismatch", i)
		}
	}

	if uint16(len(chunks)) != totalChunks {
		return nil, fmt.Errorf("expected %d chunks, got %d", totalChunks, len(chunks))
	}

	// Sort by index and verify completeness.
	ordered := make([][]byte, totalChunks)
	seen := make([]bool, totalChunks)
	for _, c := range chunks {
		if c.ChunkIndex >= totalChunks {
			return nil, fmt.Errorf("chunk index %d out of range [0, %d)", c.ChunkIndex, totalChunks)
		}
		if seen[c.ChunkIndex] {
			return nil, fmt.Errorf("duplicate chunk index %d", c.ChunkIndex)
		}
		seen[c.ChunkIndex] = true
		ordered[c.ChunkIndex] = c.Data
	}

	for i, s := range seen {
		if !s {
			return nil, fmt.Errorf("missing chunk index %d", i)
		}
	}

	// Compute total size and reassemble.
	totalSize := 0
	for _, d := range ordered {
		totalSize += len(d)
	}
	payload := make([]byte, 0, totalSize)
	for _, d := range ordered {
		payload = append(payload, d...)
	}
	return payload, nil
}

// ValidateChunk performs basic validation on a single chunk.
func ValidateChunk(chunk PayloadChunk) error {
	if chunk.ChunkIndex >= chunk.TotalChunks {
		return fmt.Errorf("chunk index %d >= total chunks %d", chunk.ChunkIndex, chunk.TotalChunks)
	}
	if chunk.TotalChunks == 0 {
		return errors.New("total chunks is zero")
	}
	if len(chunk.Data) == 0 {
		return errors.New("chunk data is empty")
	}
	if chunk.PayloadID == [32]byte{} {
		return errors.New("payload ID is zero")
	}
	return nil
}

// computeMerkleProofs computes a per-chunk proof hash from a merkle tree over chunk hashes.
// For each leaf, the proof hash is the hash of all sibling nodes needed for verification.
func computeMerkleProofs(chunks [][]byte) [][32]byte {
	n := len(chunks)
	if n == 0 {
		return nil
	}

	// Hash each chunk leaf.
	leaves := make([][32]byte, n)
	for i, c := range chunks {
		copy(leaves[i][:], crypto.Keccak256(c))
	}

	if n == 1 {
		// Single chunk: proof is the leaf hash itself.
		return leaves
	}

	// Pad to next power of 2.
	size := nextPow2(n)
	padded := make([][32]byte, size)
	copy(padded, leaves)

	// Build the merkle tree bottom-up, collecting sibling proof hashes.
	proofs := make([][32]byte, n)
	for i := range n {
		proofs[i] = computeSiblingProof(padded, size, i)
	}
	return proofs
}

// computeSiblingProof hashes together all sibling nodes for a leaf at idx.
func computeSiblingProof(leaves [][32]byte, size int, idx int) [32]byte {
	// Walk up the tree, hashing sibling at each level.
	level := make([][32]byte, len(leaves))
	copy(level, leaves)

	buf := make([]byte, 0, 64)
	var proofAccum [32]byte
	first := true

	for width := size; width > 1; width /= 2 {
		sibIdx := idx ^ 1
		var sibling [32]byte
		if sibIdx < width {
			sibling = level[sibIdx]
		}
		if first {
			proofAccum = sibling
			first = false
		} else {
			buf = buf[:0]
			buf = append(buf, proofAccum[:]...)
			buf = append(buf, sibling[:]...)
			copy(proofAccum[:], crypto.Keccak256(buf))
		}

		// Compute next level.
		next := make([][32]byte, width/2)
		for i := 0; i < width; i += 2 {
			buf = buf[:0]
			buf = append(buf, level[i][:]...)
			if i+1 < width {
				buf = append(buf, level[i+1][:]...)
			} else {
				buf = append(buf, make([]byte, 32)...)
			}
			copy(next[i/2][:], crypto.Keccak256(buf))
		}
		level = next
		idx /= 2
	}
	return proofAccum
}

func nextPow2(n int) int {
	v := 1
	for v < n {
		v <<= 1
	}
	return v
}

// EncodeChunk serializes a PayloadChunk to bytes for network transport.
func EncodeChunk(c *PayloadChunk) []byte {
	// Format: ChunkIndex(2) | TotalChunks(2) | PayloadID(32) | ProofHash(32) | DataLen(4) | Data
	buf := make([]byte, 2+2+32+32+4+len(c.Data))
	binary.BigEndian.PutUint16(buf[0:2], c.ChunkIndex)
	binary.BigEndian.PutUint16(buf[2:4], c.TotalChunks)
	copy(buf[4:36], c.PayloadID[:])
	copy(buf[36:68], c.ProofHash[:])
	binary.BigEndian.PutUint32(buf[68:72], uint32(len(c.Data)))
	copy(buf[72:], c.Data)
	return buf
}

// DecodeChunk deserializes a PayloadChunk from bytes.
func DecodeChunk(data []byte) (*PayloadChunk, error) {
	if len(data) < 72 {
		return nil, errors.New("chunk data too short")
	}
	c := &PayloadChunk{
		ChunkIndex:  binary.BigEndian.Uint16(data[0:2]),
		TotalChunks: binary.BigEndian.Uint16(data[2:4]),
	}
	copy(c.PayloadID[:], data[4:36])
	copy(c.ProofHash[:], data[36:68])
	dataLen := binary.BigEndian.Uint32(data[68:72])
	if uint32(len(data)-72) < dataLen {
		return nil, errors.New("chunk data truncated")
	}
	c.Data = make([]byte, dataLen)
	copy(c.Data, data[72:72+dataLen])
	return c, nil
}
