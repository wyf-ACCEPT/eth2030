package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Variable-size blob errors.
var (
	ErrVarBlobTooLarge      = errors.New("das: blob data exceeds MaxBlobSize")
	ErrVarBlobInvalidChunk  = errors.New("das: chunk size must be a power of 2 within [MinChunkSize, MaxChunkSize]")
	ErrVarBlobDecodeShort   = errors.New("das: encoded varblob too short")
	ErrVarBlobDecodeLen     = errors.New("das: encoded varblob length mismatch")
	ErrVarBlobEmptyData     = errors.New("das: blob data must not be empty")
)

// VarBlobConfig holds configuration for variable-size blobs (J+ upgrade).
type VarBlobConfig struct {
	// MinChunkSize is the minimum chunk size in bytes. Must be a power of 2.
	MinChunkSize int
	// MaxChunkSize is the maximum chunk size in bytes. Must be a power of 2.
	MaxChunkSize int
	// MaxBlobSize is the maximum total blob size in bytes.
	MaxBlobSize int
}

// DefaultVarBlobConfig returns the default variable blob configuration.
func DefaultVarBlobConfig() VarBlobConfig {
	return VarBlobConfig{
		MinChunkSize: 128,
		MaxChunkSize: 4096,
		MaxBlobSize:  128 * 1024,
	}
}

// VarBlob represents a variable-length blob payload that is split into
// fixed-size chunks for data availability sampling.
type VarBlob struct {
	Data      []byte
	ChunkSize int
	NumChunks int
	BlobHash  types.Hash
}

// isPowerOfTwo reports whether n is a positive power of two.
func isPowerOfTwo(n int) bool {
	return n > 0 && (n&(n-1)) == 0
}

// NewVarBlob creates a variable-size blob from the given data and chunk size.
// The data is padded with zeros to fill the last chunk if necessary.
// ChunkSize must be a power of 2 within [MinChunkSize, MaxChunkSize].
func NewVarBlob(data []byte, chunkSize int) (*VarBlob, error) {
	cfg := DefaultVarBlobConfig()

	if len(data) == 0 {
		return nil, ErrVarBlobEmptyData
	}
	if len(data) > cfg.MaxBlobSize {
		return nil, fmt.Errorf("%w: size %d, max %d", ErrVarBlobTooLarge, len(data), cfg.MaxBlobSize)
	}
	if !isPowerOfTwo(chunkSize) || chunkSize < cfg.MinChunkSize || chunkSize > cfg.MaxChunkSize {
		return nil, fmt.Errorf("%w: got %d, range [%d, %d]", ErrVarBlobInvalidChunk, chunkSize, cfg.MinChunkSize, cfg.MaxChunkSize)
	}

	// Compute number of chunks (ceiling division).
	numChunks := (len(data) + chunkSize - 1) / chunkSize

	// Pad data to fill the last chunk.
	paddedSize := numChunks * chunkSize
	padded := make([]byte, paddedSize)
	copy(padded, data)

	// BlobHash = Keccak256(original data before padding).
	blobHash := crypto.Keccak256Hash(data)

	return &VarBlob{
		Data:      padded,
		ChunkSize: chunkSize,
		NumChunks: numChunks,
		BlobHash:  blobHash,
	}, nil
}

// Chunks splits the blob data into fixed-size chunks.
func (vb *VarBlob) Chunks() [][]byte {
	chunks := make([][]byte, vb.NumChunks)
	for i := 0; i < vb.NumChunks; i++ {
		start := i * vb.ChunkSize
		end := start + vb.ChunkSize
		chunk := make([]byte, vb.ChunkSize)
		copy(chunk, vb.Data[start:end])
		chunks[i] = chunk
	}
	return chunks
}

// Encode serializes the VarBlob as: chunkSize[4] || numChunks[4] || data.
func (vb *VarBlob) Encode() []byte {
	buf := make([]byte, 8+len(vb.Data))
	binary.BigEndian.PutUint32(buf[0:4], uint32(vb.ChunkSize))
	binary.BigEndian.PutUint32(buf[4:8], uint32(vb.NumChunks))
	copy(buf[8:], vb.Data)
	return buf
}

// DecodeVarBlob deserializes a VarBlob from bytes produced by Encode.
func DecodeVarBlob(data []byte) (*VarBlob, error) {
	if len(data) < 8 {
		return nil, ErrVarBlobDecodeShort
	}

	chunkSize := int(binary.BigEndian.Uint32(data[0:4]))
	numChunks := int(binary.BigEndian.Uint32(data[4:8]))
	blobData := data[8:]

	expectedLen := chunkSize * numChunks
	if len(blobData) != expectedLen {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrVarBlobDecodeLen, expectedLen, len(blobData))
	}

	// Recompute blob hash from the stored (padded) data.
	blobHash := crypto.Keccak256Hash(blobData)

	return &VarBlob{
		Data:      blobData,
		ChunkSize: chunkSize,
		NumChunks: numChunks,
		BlobHash:  blobHash,
	}, nil
}

// Verify checks that the blob's hash matches the expected hash.
func (vb *VarBlob) Verify(expectedHash types.Hash) bool {
	computed := crypto.Keccak256Hash(vb.Data)
	return computed == expectedHash
}

// VarBlobTx wraps a VarBlob with transaction-level fields for blob transactions.
type VarBlobTx struct {
	Blob  *VarBlob
	To    types.Address
	Value *big.Int
	Data  []byte
}

// Gas cost constants for variable blob transactions.
const (
	// varBlobBaseGas is the base gas for submitting a variable-size blob.
	varBlobBaseGas = 21000
	// varBlobPerChunkGas is the per-chunk gas cost.
	varBlobPerChunkGas = 512
)

// EstimateVarBlobGas estimates the gas cost for a variable-size blob transaction.
// The cost is baseGas + perChunkGas * numChunks.
func EstimateVarBlobGas(blobSize, chunkSize int) uint64 {
	if chunkSize <= 0 {
		return varBlobBaseGas
	}
	numChunks := uint64((blobSize + chunkSize - 1) / chunkSize)
	return varBlobBaseGas + varBlobPerChunkGas*numChunks
}
