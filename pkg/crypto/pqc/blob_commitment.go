package pqc

import (
	"bytes"
	"errors"
	"math/bits"
)

// Post-Quantum Blob Commitment Scheme (M+ roadmap)
//
// Implements a Merkle-tree-based commitment scheme over blob data using
// Keccak-256 as the hash function. Merkle trees with hash-based commitments
// are quantum-resistant because their security relies on the pre-image and
// collision resistance of the hash function, not on hard number-theoretic
// problems broken by quantum computers.
//
// The blob is split into 32-byte chunks, forming the leaves of the Merkle
// tree. The commitment is the Merkle root. Openings provide an
// authentication path from a leaf to the root.

// Chunk size for splitting blob data into Merkle tree leaves.
const ChunkSize = 32

// Scheme identifier for the Merkle-based PQ commitment.
const SchemeMerklePQCommit = "merkle-pq-commit-v1"

// Security level constants.
const (
	SecurityLevel128 = 128
	SecurityLevel192 = 192
	SecurityLevel256 = 256
)

// Errors for the PQ commitment scheme.
var (
	ErrInvalidSecurityLevel = errors.New("pqc: invalid security level (must be 128, 192, or 256)")
	ErrBlobNil              = errors.New("pqc: blob is nil")
	ErrBlobEmpty            = errors.New("pqc: blob is empty")
	ErrCommitmentNil        = errors.New("pqc: commitment is nil")
	ErrIndexOutOfRange      = errors.New("pqc: index out of range")
	ErrBatchLengthMismatch  = errors.New("pqc: commitments and blobs length mismatch")
	ErrBatchEmpty           = errors.New("pqc: batch is empty")
	ErrOpeningNil           = errors.New("pqc: opening is nil")
)

// PQProofOpening represents an opening proof for a Merkle-tree commitment.
// It contains the authentication path from a leaf to the root.
type PQProofOpening struct {
	Index      uint64   // chunk index being opened
	Value      []byte   // the chunk value at that index
	MerklePath [][]byte // sibling hashes along the path from leaf to root
	Root       []byte   // the Merkle root (commitment)
}

// PQCommitmentScheme generates and verifies Merkle-tree-based blob commitments.
type PQCommitmentScheme struct {
	securityLevel int
	hashRounds    int // extra hash rounds for higher security levels
}

// NewPQCommitmentScheme creates a new commitment scheme at the given security
// level. Accepted levels are 128, 192, and 256.
func NewPQCommitmentScheme(securityLevel int) *PQCommitmentScheme {
	rounds := 1
	switch securityLevel {
	case SecurityLevel128:
		rounds = 1
	case SecurityLevel192:
		rounds = 2
	case SecurityLevel256:
		rounds = 3
	default:
		return nil
	}
	return &PQCommitmentScheme{
		securityLevel: securityLevel,
		hashRounds:    rounds,
	}
}

// SecurityLevel returns the scheme's security level.
func (s *PQCommitmentScheme) SecurityLevel() int {
	return s.securityLevel
}

// Commit generates a PQ blob commitment by building a Merkle tree over the
// blob's 32-byte chunks and returning the root as the commitment.
func (s *PQCommitmentScheme) Commit(blob []byte) (*PQBlobCommitment, error) {
	if blob == nil {
		return nil, ErrBlobNil
	}
	if len(blob) == 0 {
		return nil, ErrBlobEmpty
	}

	leaves := s.chunkAndHash(blob)
	root := s.buildMerkleRoot(leaves)

	return &PQBlobCommitment{
		Scheme:     SchemeMerklePQCommit,
		Commitment: root,
		Proof:      s.hashN(root), // domain-separated proof tag
	}, nil
}

// Verify checks whether a commitment is valid for the given blob by
// recomputing the Merkle root and comparing.
func (s *PQCommitmentScheme) Verify(commitment *PQBlobCommitment, blob []byte) bool {
	if commitment == nil || blob == nil || len(blob) == 0 {
		return false
	}
	if commitment.Scheme != SchemeMerklePQCommit {
		return false
	}

	leaves := s.chunkAndHash(blob)
	root := s.buildMerkleRoot(leaves)

	if !bytes.Equal(commitment.Commitment, root) {
		return false
	}
	expectedProof := s.hashN(root)
	return bytes.Equal(commitment.Proof, expectedProof)
}

// BatchCommit generates commitments for multiple blobs.
func (s *PQCommitmentScheme) BatchCommit(blobs [][]byte) ([]*PQBlobCommitment, error) {
	if len(blobs) == 0 {
		return nil, ErrBatchEmpty
	}
	commitments := make([]*PQBlobCommitment, len(blobs))
	for i, blob := range blobs {
		c, err := s.Commit(blob)
		if err != nil {
			return nil, err
		}
		commitments[i] = c
	}
	return commitments, nil
}

// BatchVerify verifies that each commitment matches its corresponding blob.
func (s *PQCommitmentScheme) BatchVerify(commitments []*PQBlobCommitment, blobs [][]byte) bool {
	if len(commitments) != len(blobs) {
		return false
	}
	if len(commitments) == 0 {
		return false
	}
	for i := range commitments {
		if !s.Verify(commitments[i], blobs[i]) {
			return false
		}
	}
	return true
}

// OpenAt creates a proof opening for a specific chunk index in the blob.
// The opening contains the Merkle path from the leaf to the root.
func (s *PQCommitmentScheme) OpenAt(blob []byte, index uint64) (*PQProofOpening, error) {
	if blob == nil {
		return nil, ErrBlobNil
	}
	if len(blob) == 0 {
		return nil, ErrBlobEmpty
	}

	chunks := splitChunks(blob)
	if index >= uint64(len(chunks)) {
		return nil, ErrIndexOutOfRange
	}

	leaves := make([][]byte, len(chunks))
	for i, chunk := range chunks {
		leaves[i] = s.hashN(chunk)
	}

	// Pad leaves to a power of two for a complete binary tree.
	paddedLeaves := padToPow2(leaves, s.hashN(nil))
	path := s.merkleProof(paddedLeaves, index)
	root := s.buildMerkleRoot(leaves)

	return &PQProofOpening{
		Index:      index,
		Value:      chunks[index],
		MerklePath: path,
		Root:       root,
	}, nil
}

// VerifyOpening verifies a proof opening against a commitment.
func (s *PQCommitmentScheme) VerifyOpening(
	commitment *PQBlobCommitment,
	opening *PQProofOpening,
	index uint64,
) bool {
	if commitment == nil || opening == nil {
		return false
	}
	if opening.Index != index {
		return false
	}
	if !bytes.Equal(commitment.Commitment, opening.Root) {
		return false
	}

	// Recompute the root from the leaf and Merkle path.
	leaf := s.hashN(opening.Value)
	computed := leaf
	idx := index
	for _, sibling := range opening.MerklePath {
		if idx%2 == 0 {
			computed = s.hashPair(computed, sibling)
		} else {
			computed = s.hashPair(sibling, computed)
		}
		idx /= 2
	}

	return bytes.Equal(computed, opening.Root)
}

// --- internal helpers ---

// splitChunks splits blob data into 32-byte chunks. The last chunk is
// zero-padded if the blob length is not a multiple of ChunkSize.
func splitChunks(blob []byte) [][]byte {
	n := (len(blob) + ChunkSize - 1) / ChunkSize
	chunks := make([][]byte, n)
	for i := 0; i < n; i++ {
		start := i * ChunkSize
		end := start + ChunkSize
		if end > len(blob) {
			end = len(blob)
		}
		chunk := make([]byte, ChunkSize)
		copy(chunk, blob[start:end])
		chunks[i] = chunk
	}
	return chunks
}

// chunkAndHash splits the blob into chunks and hashes each chunk.
func (s *PQCommitmentScheme) chunkAndHash(blob []byte) [][]byte {
	chunks := splitChunks(blob)
	leaves := make([][]byte, len(chunks))
	for i, chunk := range chunks {
		leaves[i] = s.hashN(chunk)
	}
	return leaves
}

// hashN applies Keccak-256 hashRounds times for the configured security level.
func (s *PQCommitmentScheme) hashN(data []byte) []byte {
	h := keccak256(data)
	for i := 1; i < s.hashRounds; i++ {
		h = keccak256(h)
	}
	return h
}

// hashPair hashes two 32-byte nodes together (internal Merkle tree node).
func (s *PQCommitmentScheme) hashPair(left, right []byte) []byte {
	combined := make([]byte, 0, len(left)+len(right))
	combined = append(combined, left...)
	combined = append(combined, right...)
	return s.hashN(combined)
}

// buildMerkleRoot constructs a Merkle tree from leaf hashes and returns the root.
func (s *PQCommitmentScheme) buildMerkleRoot(leaves [][]byte) []byte {
	if len(leaves) == 0 {
		return s.hashN(nil)
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	// Pad to power of two.
	emptyLeaf := s.hashN(nil)
	layer := padToPow2(leaves, emptyLeaf)

	for len(layer) > 1 {
		next := make([][]byte, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			next[i/2] = s.hashPair(layer[i], layer[i+1])
		}
		layer = next
	}
	return layer[0]
}

// merkleProof generates the sibling hashes along the path from leaf at index
// to the root. The leaves must already be padded to a power of two.
func (s *PQCommitmentScheme) merkleProof(leaves [][]byte, index uint64) [][]byte {
	layer := make([][]byte, len(leaves))
	copy(layer, leaves)

	var path [][]byte
	idx := index
	for len(layer) > 1 {
		// Collect sibling.
		if idx%2 == 0 {
			path = append(path, layer[idx+1])
		} else {
			path = append(path, layer[idx-1])
		}
		// Build next layer.
		next := make([][]byte, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			next[i/2] = s.hashPair(layer[i], layer[i+1])
		}
		layer = next
		idx /= 2
	}
	return path
}

// padToPow2 pads a slice of byte slices to the next power of two using pad.
func padToPow2(items [][]byte, pad []byte) [][]byte {
	n := len(items)
	if n == 0 {
		return [][]byte{pad}
	}
	target := nextPow2(n)
	if target == n {
		result := make([][]byte, n)
		copy(result, items)
		return result
	}
	result := make([][]byte, target)
	copy(result, items)
	for i := n; i < target; i++ {
		p := make([]byte, len(pad))
		copy(p, pad)
		result[i] = p
	}
	return result
}

// nextPow2 returns the smallest power of 2 >= n.
func nextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	return 1 << bits.Len(uint(n-1))
}
