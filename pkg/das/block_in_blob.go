// Block-in-Blobs: encoding execution blocks as blob space for the EL throughput
// roadmap track (block in blobs -> mandatory proofs -> canonical guest).
// This enables embedding full execution payloads within blob transactions,
// allowing L1 blocks to be reconstructed from blob data.
package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Block-in-blob errors.
var (
	ErrBlockDataEmpty       = errors.New("das: block data must not be empty")
	ErrBlockDataTooLarge    = errors.New("das: block data exceeds maximum encodable size")
	ErrNoBlobsProvided      = errors.New("das: no blobs provided for decoding")
	ErrBlobOrderMismatch    = errors.New("das: blob sequence has non-contiguous indices")
	ErrBlobHashMismatch     = errors.New("das: blob block hash mismatch across blobs")
	ErrBlobCountMismatch    = errors.New("das: blob total chunk count mismatch")
	ErrMissingLastBlob      = errors.New("das: last blob marker not found")
	ErrBlobDataCorrupt      = errors.New("das: blob data integrity check failed")
	ErrCommitmentMismatch   = errors.New("das: commitment verification failed")
	ErrMaxBlobsExceeded     = errors.New("das: block data requires more blobs than allowed")
)

// blockBlobHeaderSize is the byte overhead per blob for the header.
// Format: originalLen[4] + index[8] + totalChunks[8] + isLast[1] + blockHash[32] = 53 bytes.
const blockBlobHeaderSize = 4 + 8 + 8 + 1 + 32

// BlobBlockConfig configures the block-in-blob encoder.
type BlobBlockConfig struct {
	// MaxBlobSize is the maximum size of each blob payload in bytes.
	// Default: 131072 (128 KiB, matching standard blob size).
	MaxBlobSize uint64

	// MaxBlobsPerBlock is the maximum number of blobs that can encode a single block.
	MaxBlobsPerBlock uint64

	// CompressionEnabled indicates whether to compress block data before encoding.
	// (Stub: reserved for future snappy/zstd integration.)
	CompressionEnabled bool
}

// DefaultBlobBlockConfig returns sensible defaults for block-in-blob encoding.
func DefaultBlobBlockConfig() BlobBlockConfig {
	return BlobBlockConfig{
		MaxBlobSize:        131072, // 128 KiB
		MaxBlobsPerBlock:   16,
		CompressionEnabled: false,
	}
}

// BlockBlob is a single blob carrying a chunk of an encoded execution block.
type BlockBlob struct {
	// Index is the zero-based position of this blob in the sequence.
	Index uint64

	// Data is the blob payload (header + block data chunk).
	Data []byte

	// BlockHash is the Keccak-256 hash of the original block data.
	BlockHash types.Hash

	// IsLast indicates this is the final blob in the sequence.
	IsLast bool

	// TotalChunks is the total number of blobs encoding this block.
	TotalChunks uint64
}

// BlockBlobCommitment ties a set of block blobs to their block hash.
type BlockBlobCommitment struct {
	// BlockHash is the Keccak-256 hash of the original block data.
	BlockHash types.Hash

	// BlobCommitments contains one commitment hash per blob.
	BlobCommitments []types.Hash

	// TotalSize is the total original block data size.
	TotalSize uint64
}

// BlobBlockEncoder encodes and decodes execution blocks as blobs.
// All methods are safe for concurrent use.
type BlobBlockEncoder struct {
	mu     sync.RWMutex
	config BlobBlockConfig
}

// NewBlobBlockEncoder creates a new encoder with the given config.
func NewBlobBlockEncoder(config BlobBlockConfig) *BlobBlockEncoder {
	if config.MaxBlobSize == 0 {
		config.MaxBlobSize = 131072
	}
	if config.MaxBlobsPerBlock == 0 {
		config.MaxBlobsPerBlock = 16
	}
	return &BlobBlockEncoder{config: config}
}

// usablePayload returns the number of block-data bytes each blob can carry.
func (e *BlobBlockEncoder) usablePayload() uint64 {
	return e.config.MaxBlobSize - blockBlobHeaderSize
}

// EstimateBlobCount returns the number of blobs needed to encode blockSize bytes.
func (e *BlobBlockEncoder) EstimateBlobCount(blockSize uint64) uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if blockSize == 0 {
		return 0
	}
	payload := e.usablePayload()
	count := blockSize / payload
	if blockSize%payload != 0 {
		count++
	}
	return count
}

// EncodeBlock splits blockData across one or more BlockBlobs.
func (e *BlobBlockEncoder) EncodeBlock(blockData []byte) ([]*BlockBlob, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(blockData) == 0 {
		return nil, ErrBlockDataEmpty
	}

	payload := e.usablePayload()
	totalChunks := uint64(len(blockData)) / payload
	if uint64(len(blockData))%payload != 0 {
		totalChunks++
	}

	if totalChunks > e.config.MaxBlobsPerBlock {
		return nil, fmt.Errorf("%w: need %d blobs, max %d",
			ErrMaxBlobsExceeded, totalChunks, e.config.MaxBlobsPerBlock)
	}

	blockHash := crypto.Keccak256Hash(blockData)
	blobs := make([]*BlockBlob, totalChunks)

	for i := uint64(0); i < totalChunks; i++ {
		start := i * payload
		end := start + payload
		if end > uint64(len(blockData)) {
			end = uint64(len(blockData))
		}
		chunk := blockData[start:end]
		isLast := i == totalChunks-1

		blobData := encodeBlockBlobPayload(uint32(len(blockData)), i, totalChunks, isLast, blockHash, chunk)

		blobs[i] = &BlockBlob{
			Index:       i,
			Data:        blobData,
			BlockHash:   blockHash,
			IsLast:      isLast,
			TotalChunks: totalChunks,
		}
	}

	return blobs, nil
}

// DecodeBlock reassembles the original block data from a set of blobs.
func (e *BlobBlockEncoder) DecodeBlock(blobs []*BlockBlob) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(blobs) == 0 {
		return nil, ErrNoBlobsProvided
	}

	// Validate all blobs share the same block hash and total chunk count.
	expected := blobs[0]
	if expected.TotalChunks != uint64(len(blobs)) {
		return nil, fmt.Errorf("%w: expected %d blobs, got %d",
			ErrBlobCountMismatch, expected.TotalChunks, len(blobs))
	}

	hasLast := false
	for _, b := range blobs {
		if b.BlockHash != expected.BlockHash {
			return nil, ErrBlobHashMismatch
		}
		if b.TotalChunks != expected.TotalChunks {
			return nil, ErrBlobCountMismatch
		}
		if b.IsLast {
			hasLast = true
		}
	}
	if !hasLast {
		return nil, ErrMissingLastBlob
	}

	// Sort by index and verify contiguous.
	sorted := make([]*BlockBlob, expected.TotalChunks)
	for _, b := range blobs {
		if b.Index >= expected.TotalChunks {
			return nil, fmt.Errorf("%w: index %d >= total %d",
				ErrBlobOrderMismatch, b.Index, expected.TotalChunks)
		}
		if sorted[b.Index] != nil {
			return nil, fmt.Errorf("%w: duplicate index %d", ErrBlobOrderMismatch, b.Index)
		}
		sorted[b.Index] = b
	}
	for i, b := range sorted {
		if b == nil {
			return nil, fmt.Errorf("%w: missing index %d", ErrBlobOrderMismatch, i)
		}
	}

	// Extract block data from each blob payload.
	var result []byte
	var originalLen uint32
	for i, b := range sorted {
		origLen, chunk, err := decodeBlockBlobPayload(b.Data)
		if err != nil {
			return nil, fmt.Errorf("blob %d: %w", i, err)
		}
		if i == 0 {
			originalLen = origLen
			result = make([]byte, 0, originalLen)
		} else if origLen != originalLen {
			return nil, ErrBlobDataCorrupt
		}
		result = append(result, chunk...)
	}

	// Trim to original length.
	if uint32(len(result)) < originalLen {
		return nil, ErrBlobDataCorrupt
	}
	result = result[:originalLen]

	// Verify the block hash.
	computed := crypto.Keccak256Hash(result)
	if computed != expected.BlockHash {
		return nil, ErrBlobDataCorrupt
	}

	return result, nil
}

// VerifyBlockBlobs checks the integrity of a set of block blobs without decoding.
func (e *BlobBlockEncoder) VerifyBlockBlobs(blobs []*BlockBlob) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(blobs) == 0 {
		return ErrNoBlobsProvided
	}

	expected := blobs[0]
	if expected.TotalChunks != uint64(len(blobs)) {
		return fmt.Errorf("%w: expected %d blobs, got %d",
			ErrBlobCountMismatch, expected.TotalChunks, len(blobs))
	}

	seen := make(map[uint64]bool, len(blobs))
	hasLast := false
	for _, b := range blobs {
		if b.BlockHash != expected.BlockHash {
			return ErrBlobHashMismatch
		}
		if b.TotalChunks != expected.TotalChunks {
			return ErrBlobCountMismatch
		}
		if b.Index >= expected.TotalChunks {
			return fmt.Errorf("%w: index %d >= total %d",
				ErrBlobOrderMismatch, b.Index, expected.TotalChunks)
		}
		if seen[b.Index] {
			return fmt.Errorf("%w: duplicate index %d", ErrBlobOrderMismatch, b.Index)
		}
		seen[b.Index] = true
		if b.IsLast {
			hasLast = true
		}
		// Verify the blob data header is consistent.
		if len(b.Data) < blockBlobHeaderSize {
			return ErrBlobDataCorrupt
		}
		headerHash := types.BytesToHash(b.Data[4+8+8+1 : blockBlobHeaderSize])
		if headerHash != b.BlockHash {
			return ErrBlobDataCorrupt
		}
	}
	if !hasLast {
		return ErrMissingLastBlob
	}

	return nil
}

// GenerateCommitment creates a commitment covering all blobs for a block.
func (e *BlobBlockEncoder) GenerateCommitment(blobs []*BlockBlob) (*BlockBlobCommitment, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(blobs) == 0 {
		return nil, ErrNoBlobsProvided
	}

	commitments := make([]types.Hash, len(blobs))
	var totalSize uint64
	blockHash := blobs[0].BlockHash

	for i, b := range blobs {
		if b.BlockHash != blockHash {
			return nil, ErrBlobHashMismatch
		}
		commitments[i] = crypto.Keccak256Hash(b.Data)
		totalSize += uint64(len(b.Data))
	}

	return &BlockBlobCommitment{
		BlockHash:       blockHash,
		BlobCommitments: commitments,
		TotalSize:       totalSize,
	}, nil
}

// VerifyCommitment checks that a commitment matches the given blobs.
func (e *BlobBlockEncoder) VerifyCommitment(commitment *BlockBlobCommitment, blobs []*BlockBlob) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if commitment == nil || len(blobs) == 0 {
		return false
	}
	if len(commitment.BlobCommitments) != len(blobs) {
		return false
	}
	if commitment.BlockHash != blobs[0].BlockHash {
		return false
	}

	var totalSize uint64
	for i, b := range blobs {
		if b.BlockHash != commitment.BlockHash {
			return false
		}
		blobCommit := crypto.Keccak256Hash(b.Data)
		if blobCommit != commitment.BlobCommitments[i] {
			return false
		}
		totalSize += uint64(len(b.Data))
	}

	return totalSize == commitment.TotalSize
}

// encodeBlockBlobPayload builds the wire format for a single blob:
// originalLen[4] || index[8] || totalChunks[8] || isLast[1] || blockHash[32] || chunk
func encodeBlockBlobPayload(originalLen uint32, index, totalChunks uint64, isLast bool, blockHash types.Hash, chunk []byte) []byte {
	buf := make([]byte, blockBlobHeaderSize+len(chunk))
	binary.BigEndian.PutUint32(buf[0:4], originalLen)
	binary.BigEndian.PutUint64(buf[4:12], index)
	binary.BigEndian.PutUint64(buf[12:20], totalChunks)
	if isLast {
		buf[20] = 1
	}
	copy(buf[21:53], blockHash[:])
	copy(buf[53:], chunk)
	return buf
}

// decodeBlockBlobPayload parses a blob payload and returns the original length
// and the chunk data.
func decodeBlockBlobPayload(data []byte) (uint32, []byte, error) {
	if len(data) < blockBlobHeaderSize {
		return 0, nil, ErrBlobDataCorrupt
	}
	originalLen := binary.BigEndian.Uint32(data[0:4])
	chunk := data[blockBlobHeaderSize:]
	return originalLen, chunk, nil
}
