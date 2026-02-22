// Package das implements PeerDAS data availability sampling.
//
// block_blob_proof.go provides an enhanced block-in-blob proof system that
// validates block data encoded within blob commitments using Merkle proofs.
// Part of the K+ roadmap: "block in blobs -> mandatory proofs -> canonical guest".
package das

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Block-blob proof errors.
var (
	ErrBlockBlobTooLarge       = errors.New("das-proof: block data exceeds maximum size")
	ErrBlockBlobInvalidProof   = errors.New("das-proof: proof verification failed")
	ErrBlockBlobEncodingFailed = errors.New("das-proof: encoding failed")
)

// BlockBlobProverConfig configures the proof system.
type BlockBlobProverConfig struct {
	// MaxBlockSize is the maximum block data size in bytes.
	MaxBlockSize uint64

	// BlobFieldElementSize is the byte size of each blob field element chunk.
	BlobFieldElementSize uint64

	// MaxBlobsPerBlock is the maximum number of blobs allowed per block.
	MaxBlobsPerBlock uint64
}

// DefaultBlockBlobProverConfig returns sensible defaults.
func DefaultBlockBlobProverConfig() BlockBlobProverConfig {
	return BlockBlobProverConfig{
		MaxBlockSize:         2 * 1024 * 1024, // 2 MiB
		BlobFieldElementSize: 31,              // 31 usable bytes per field element
		MaxBlobsPerBlock:     16,
	}
}

// BlockBlobEncoding holds the result of encoding a block into blob-sized chunks.
type BlockBlobEncoding struct {
	OriginalSize  uint64
	EncodedChunks [][]byte
	PaddingBytes  uint64
	ChunkCount    uint64
}

// BlockBlobProof is a Merkle proof over the encoded block chunks.
type BlockBlobProof struct {
	BlockHash        types.Hash
	BlobIndices      []uint64
	MerkleRoot       types.Hash
	ProofPath        []types.Hash
	EncodingMetadata BlockBlobEncodingMeta
}

// BlockBlobEncodingMeta stores encoding parameters for proof verification.
type BlockBlobEncodingMeta struct {
	OriginalSize uint64
	ChunkSize    uint64
	ChunkCount   uint64
	PaddingBytes uint64
}

// BlockBlobProver creates and verifies Merkle proofs over block-in-blob encodings.
// All exported methods are safe for concurrent use.
type BlockBlobProver struct {
	mu         sync.RWMutex
	config     BlockBlobProverConfig
	proofCache map[types.Hash]*BlockBlobProof
}

// NewBlockBlobProver creates a new prover with the given config.
func NewBlockBlobProver(config BlockBlobProverConfig) *BlockBlobProver {
	if config.MaxBlockSize == 0 {
		config.MaxBlockSize = 2 * 1024 * 1024
	}
	if config.BlobFieldElementSize == 0 {
		config.BlobFieldElementSize = 31
	}
	if config.MaxBlobsPerBlock == 0 {
		config.MaxBlobsPerBlock = 16
	}
	return &BlockBlobProver{
		config:     config,
		proofCache: make(map[types.Hash]*BlockBlobProof),
	}
}

// EncodeBlock splits block data into blob-sized field element chunks.
func (p *BlockBlobProver) EncodeBlock(blockData []byte) (*BlockBlobEncoding, error) {
	p.mu.RLock()
	cfg := p.config
	p.mu.RUnlock()

	if len(blockData) == 0 {
		return nil, fmt.Errorf("%w: empty block data", ErrBlockBlobEncodingFailed)
	}
	if uint64(len(blockData)) > cfg.MaxBlockSize {
		return nil, fmt.Errorf("%w: size %d > max %d",
			ErrBlockBlobTooLarge, len(blockData), cfg.MaxBlockSize)
	}

	chunkSize := cfg.BlobFieldElementSize
	numChunks := uint64(len(blockData)) / chunkSize
	if uint64(len(blockData))%chunkSize != 0 {
		numChunks++
	}

	chunks := make([][]byte, numChunks)
	var paddingBytes uint64

	for i := uint64(0); i < numChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > uint64(len(blockData)) {
			end = uint64(len(blockData))
		}
		chunk := make([]byte, chunkSize)
		copy(chunk, blockData[start:end])
		padded := chunkSize - (end - start)
		paddingBytes += padded
		chunks[i] = chunk
	}

	return &BlockBlobEncoding{
		OriginalSize:  uint64(len(blockData)),
		EncodedChunks: chunks,
		PaddingBytes:  paddingBytes,
		ChunkCount:    numChunks,
	}, nil
}

// CreateProof creates a Merkle proof over the encoded chunks.
func (p *BlockBlobProver) CreateProof(encoding *BlockBlobEncoding) (*BlockBlobProof, error) {
	if encoding == nil || len(encoding.EncodedChunks) == 0 {
		return nil, fmt.Errorf("%w: nil or empty encoding", ErrBlockBlobEncodingFailed)
	}

	// Hash each chunk to form Merkle leaves.
	leaves := make([]types.Hash, len(encoding.EncodedChunks))
	for i, chunk := range encoding.EncodedChunks {
		leaves[i] = crypto.Keccak256Hash(chunk)
	}

	// Compute Merkle root and proof path.
	root, proofPath := computeMerkleRootAndPath(leaves)

	// Compute block hash from concatenated chunks (trimmed to original size).
	blockData := reassembleFromEncoding(encoding)
	blockHash := crypto.Keccak256Hash(blockData)

	// Build blob indices.
	p.mu.RLock()
	feSize := p.config.BlobFieldElementSize
	p.mu.RUnlock()

	blobIndices := computeBlobIndices(encoding.ChunkCount, feSize)

	proof := &BlockBlobProof{
		BlockHash:   blockHash,
		BlobIndices: blobIndices,
		MerkleRoot:  root,
		ProofPath:   proofPath,
		EncodingMetadata: BlockBlobEncodingMeta{
			OriginalSize: encoding.OriginalSize,
			ChunkSize:    feSize,
			ChunkCount:   encoding.ChunkCount,
			PaddingBytes: encoding.PaddingBytes,
		},
	}

	// Cache the proof.
	p.mu.Lock()
	p.proofCache[blockHash] = proof
	p.mu.Unlock()

	return proof, nil
}

// VerifyProof verifies a proof against the original block data.
func (p *BlockBlobProver) VerifyProof(proof *BlockBlobProof, blockData []byte) (bool, error) {
	if proof == nil {
		return false, ErrBlockBlobInvalidProof
	}
	if len(blockData) == 0 {
		return false, fmt.Errorf("%w: empty block data", ErrBlockBlobInvalidProof)
	}

	// Verify block hash matches.
	computedHash := crypto.Keccak256Hash(blockData)
	if computedHash != proof.BlockHash {
		return false, nil
	}

	// Re-encode the block data.
	p.mu.RLock()
	cfg := p.config
	p.mu.RUnlock()

	chunkSize := proof.EncodingMetadata.ChunkSize
	if chunkSize == 0 {
		chunkSize = cfg.BlobFieldElementSize
	}

	numChunks := uint64(len(blockData)) / chunkSize
	if uint64(len(blockData))%chunkSize != 0 {
		numChunks++
	}

	if numChunks != proof.EncodingMetadata.ChunkCount {
		return false, nil
	}

	// Recompute Merkle leaves.
	leaves := make([]types.Hash, numChunks)
	for i := uint64(0); i < numChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > uint64(len(blockData)) {
			end = uint64(len(blockData))
		}
		chunk := make([]byte, chunkSize)
		copy(chunk, blockData[start:end])
		leaves[i] = crypto.Keccak256Hash(chunk)
	}

	// Verify Merkle root.
	root, _ := computeMerkleRootAndPath(leaves)
	if root != proof.MerkleRoot {
		return false, nil
	}

	return true, nil
}

// EstimateBlobCount estimates the number of blobs needed for blockSize bytes.
func (p *BlockBlobProver) EstimateBlobCount(blockSize int) int {
	p.mu.RLock()
	cfg := p.config
	p.mu.RUnlock()

	if blockSize <= 0 {
		return 0
	}

	// Each blob holds FieldElementsPerBlob field elements.
	bytesPerBlob := FieldElementsPerBlob * cfg.BlobFieldElementSize
	count := uint64(blockSize) / bytesPerBlob
	if uint64(blockSize)%bytesPerBlob != 0 {
		count++
	}
	return int(count)
}

// reassembleFromEncoding reconstructs block data from an encoding.
func reassembleFromEncoding(enc *BlockBlobEncoding) []byte {
	var buf []byte
	for _, chunk := range enc.EncodedChunks {
		buf = append(buf, chunk...)
	}
	if uint64(len(buf)) > enc.OriginalSize {
		buf = buf[:enc.OriginalSize]
	}
	return buf
}

// computeBlobIndices returns blob indices for the given chunk count.
func computeBlobIndices(chunkCount, feSize uint64) []uint64 {
	chunksPerBlob := (FieldElementsPerBlob * feSize) / feSize
	if chunksPerBlob == 0 {
		chunksPerBlob = 1
	}
	blobCount := chunkCount / chunksPerBlob
	if chunkCount%chunksPerBlob != 0 {
		blobCount++
	}
	indices := make([]uint64, blobCount)
	for i := range indices {
		indices[i] = uint64(i)
	}
	return indices
}

// computeMerkleRootAndPath computes a Merkle root from leaves and returns the
// internal nodes as a proof path. The tree is padded to the next power of two.
func computeMerkleRootAndPath(leaves []types.Hash) (types.Hash, []types.Hash) {
	if len(leaves) == 0 {
		return types.Hash{}, nil
	}
	if len(leaves) == 1 {
		return leaves[0], []types.Hash{leaves[0]}
	}

	// Pad to next power of two.
	n := nextPowerOfTwo(len(leaves))
	padded := make([]types.Hash, n)
	copy(padded, leaves)
	// Remaining are zero hashes (default).

	var proofPath []types.Hash
	current := padded

	for len(current) > 1 {
		next := make([]types.Hash, len(current)/2)
		for i := 0; i < len(current); i += 2 {
			combined := make([]byte, 64)
			copy(combined[:32], current[i][:])
			copy(combined[32:], current[i+1][:])
			next[i/2] = crypto.Keccak256Hash(combined)
		}
		proofPath = append(proofPath, next...)
		current = next
	}

	return current[0], proofPath
}

// nextPowerOfTwo returns the smallest power of two >= n.
func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p *= 2
	}
	return p
}
