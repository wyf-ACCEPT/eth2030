// blobs_bundle.go implements blob bundle management for the Engine API,
// including BlobsBundle construction, versioned hash derivation, blob
// sidecar preparation, and KZG commitment verification.
//
// Per EIP-4844, each blob is accompanied by a KZG commitment and proof.
// The versioned hash of a commitment is SHA-256(commitment) with the first
// byte replaced by VersionedHashVersionKZG (0x01). The engine_getPayload
// response includes a BlobsBundle containing commitments, proofs, and
// raw blobs in parallel arrays.
//
// Per EIP-4895 (Deneb), blob sidecars are prepared for gossip propagation
// by pairing each blob with its commitment and proof, along with inclusion
// proofs against the beacon block body.
package engine

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Blob bundle constants.
const (
	// BlobSize is the byte length of a single blob (128 KiB).
	BlobSize = 131072

	// KZGCommitmentSize is the byte length of a compressed KZG commitment (G1).
	KZGCommitmentSize = 48

	// KZGProofSize is the byte length of a compressed KZG proof (G1).
	KZGProofSize = 48

	// MaxBlobsPerBundle is the maximum number of blobs in a single bundle.
	MaxBlobsPerBundle = 6

	// VersionedHashVersion is the version byte for KZG versioned hashes (EIP-4844).
	VersionedHashVersion byte = 0x01
)

// Blob bundle errors.
var (
	ErrBlobBundleEmpty         = errors.New("blobs bundle: empty bundle")
	ErrBlobBundleMismatch      = errors.New("blobs bundle: commitments/proofs/blobs length mismatch")
	ErrBlobBundleTooMany       = errors.New("blobs bundle: too many blobs")
	ErrBlobInvalidSize         = errors.New("blobs bundle: invalid blob size")
	ErrCommitmentInvalidSize   = errors.New("blobs bundle: invalid commitment size")
	ErrProofInvalidSize        = errors.New("blobs bundle: invalid proof size")
	ErrVersionedHashMismatch   = errors.New("blobs bundle: versioned hash does not match commitment")
	ErrBlobBundleSidecarIndex  = errors.New("blobs bundle: sidecar index out of range")
)

// KZGVerifier defines the interface for verifying KZG commitments against
// blobs. Implementations may use real cryptographic verification or a
// trusted setup stub.
type KZGVerifier interface {
	// VerifyBlobCommitment checks that the KZG commitment matches the blob data.
	// Returns nil if the commitment is valid.
	VerifyBlobCommitment(blob []byte, commitment []byte) error

	// VerifyBlobProof checks that the KZG proof is valid for the given
	// commitment and blob data. Returns nil if valid.
	VerifyBlobProof(blob []byte, commitment []byte, proof []byte) error
}

// BlobSidecar pairs a single blob with its KZG commitment, proof, and
// metadata needed for propagation on the gossip network.
type BlobSidecar struct {
	Index            uint64     `json:"index"`
	Blob             []byte     `json:"blob"`
	KZGCommitment    []byte     `json:"kzgCommitment"`
	KZGProof         []byte     `json:"kzgProof"`
	SignedBlockHeader types.Hash `json:"signedBlockHeader"` // block hash
	CommitmentInclusionProof []types.Hash `json:"kzgCommitmentInclusionProof"`
}

// BlobsBundleBuilder constructs a BlobsBundleV1 incrementally, validating
// each blob/commitment/proof triple as it is added. Thread-safe.
type BlobsBundleBuilder struct {
	mu          sync.Mutex
	commitments [][]byte
	proofs      [][]byte
	blobs       [][]byte
	verifier    KZGVerifier // optional; nil skips verification
}

// NewBlobsBundleBuilder creates a new builder. If verifier is nil, KZG
// commitment/proof verification is skipped.
func NewBlobsBundleBuilder(verifier KZGVerifier) *BlobsBundleBuilder {
	return &BlobsBundleBuilder{
		verifier: verifier,
	}
}

// AddBlob adds a blob with its commitment and proof to the bundle.
// Returns an error if the sizes are invalid or the bundle is full.
func (b *BlobsBundleBuilder) AddBlob(blob, commitment, proof []byte) error {
	if len(blob) != BlobSize {
		return fmt.Errorf("%w: got %d, want %d", ErrBlobInvalidSize, len(blob), BlobSize)
	}
	if len(commitment) != KZGCommitmentSize {
		return fmt.Errorf("%w: got %d, want %d", ErrCommitmentInvalidSize, len(commitment), KZGCommitmentSize)
	}
	if len(proof) != KZGProofSize {
		return fmt.Errorf("%w: got %d, want %d", ErrProofInvalidSize, len(proof), KZGProofSize)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.blobs) >= MaxBlobsPerBundle {
		return ErrBlobBundleTooMany
	}

	// Optionally verify the commitment against the blob.
	if b.verifier != nil {
		if err := b.verifier.VerifyBlobCommitment(blob, commitment); err != nil {
			return fmt.Errorf("commitment verification failed: %w", err)
		}
		if err := b.verifier.VerifyBlobProof(blob, commitment, proof); err != nil {
			return fmt.Errorf("proof verification failed: %w", err)
		}
	}

	// Copy inputs to avoid external mutation.
	blobCopy := make([]byte, BlobSize)
	copy(blobCopy, blob)
	commitCopy := make([]byte, KZGCommitmentSize)
	copy(commitCopy, commitment)
	proofCopy := make([]byte, KZGProofSize)
	copy(proofCopy, proof)

	b.commitments = append(b.commitments, commitCopy)
	b.proofs = append(b.proofs, proofCopy)
	b.blobs = append(b.blobs, blobCopy)

	return nil
}

// Build constructs the final BlobsBundleV1. Returns an error if the
// bundle is empty.
func (b *BlobsBundleBuilder) Build() (*BlobsBundleV1, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.blobs) == 0 {
		return nil, ErrBlobBundleEmpty
	}

	bundle := &BlobsBundleV1{
		Commitments: make([][]byte, len(b.commitments)),
		Proofs:      make([][]byte, len(b.proofs)),
		Blobs:       make([][]byte, len(b.blobs)),
	}
	copy(bundle.Commitments, b.commitments)
	copy(bundle.Proofs, b.proofs)
	copy(bundle.Blobs, b.blobs)

	return bundle, nil
}

// Count returns the number of blobs currently in the builder.
func (b *BlobsBundleBuilder) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.blobs)
}

// Reset clears the builder for reuse.
func (b *BlobsBundleBuilder) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.commitments = nil
	b.proofs = nil
	b.blobs = nil
}

// ValidateBundle checks a BlobsBundleV1 for structural correctness:
// parallel arrays must have equal length, sizes must be correct, and
// the number of blobs must not exceed the maximum.
func ValidateBundle(bundle *BlobsBundleV1) error {
	if bundle == nil {
		return ErrBlobBundleEmpty
	}
	if len(bundle.Commitments) != len(bundle.Proofs) ||
		len(bundle.Proofs) != len(bundle.Blobs) {
		return fmt.Errorf("%w: commitments=%d proofs=%d blobs=%d",
			ErrBlobBundleMismatch,
			len(bundle.Commitments), len(bundle.Proofs), len(bundle.Blobs))
	}
	if len(bundle.Blobs) > MaxBlobsPerBundle {
		return fmt.Errorf("%w: got %d, max %d",
			ErrBlobBundleTooMany, len(bundle.Blobs), MaxBlobsPerBundle)
	}
	for i, blob := range bundle.Blobs {
		if len(blob) != BlobSize {
			return fmt.Errorf("%w at index %d: got %d",
				ErrBlobInvalidSize, i, len(blob))
		}
	}
	for i, c := range bundle.Commitments {
		if len(c) != KZGCommitmentSize {
			return fmt.Errorf("%w at index %d: got %d",
				ErrCommitmentInvalidSize, i, len(c))
		}
	}
	for i, p := range bundle.Proofs {
		if len(p) != KZGProofSize {
			return fmt.Errorf("%w at index %d: got %d",
				ErrProofInvalidSize, i, len(p))
		}
	}
	return nil
}

// VersionedHash computes the EIP-4844 versioned hash from a KZG commitment.
// The versioned hash is SHA-256(commitment) with byte 0 replaced by the
// version byte (0x01).
func VersionedHash(commitment []byte) types.Hash {
	h := sha256.Sum256(commitment)
	h[0] = VersionedHashVersion
	return types.Hash(h)
}

// DeriveVersionedHashes computes versioned hashes for all commitments in
// a BlobsBundleV1. Returns hashes in the same order as the commitments.
func DeriveVersionedHashes(bundle *BlobsBundleV1) []types.Hash {
	if bundle == nil || len(bundle.Commitments) == 0 {
		return nil
	}
	hashes := make([]types.Hash, len(bundle.Commitments))
	for i, c := range bundle.Commitments {
		hashes[i] = VersionedHash(c)
	}
	return hashes
}

// ValidateVersionedHashes checks that the versioned hashes derived from
// the bundle's commitments match the expected hashes exactly (same order
// and count). This implements the CL-side validation per EIP-4844.
func ValidateVersionedHashes(bundle *BlobsBundleV1, expected []types.Hash) error {
	derived := DeriveVersionedHashes(bundle)
	if len(derived) != len(expected) {
		return fmt.Errorf("%w: expected %d hashes, got %d",
			ErrVersionedHashMismatch, len(expected), len(derived))
	}
	for i := range expected {
		if derived[i] != expected[i] {
			return fmt.Errorf("%w at index %d: expected %s, got %s",
				ErrVersionedHashMismatch, i, expected[i].Hex(), derived[i].Hex())
		}
	}
	return nil
}

// PrepareSidecars extracts individual BlobSidecars from a BlobsBundleV1,
// suitable for gossip propagation on the beacon network. The blockHash
// is included in each sidecar for block association.
func PrepareSidecars(bundle *BlobsBundleV1, blockHash types.Hash) ([]*BlobSidecar, error) {
	if err := ValidateBundle(bundle); err != nil {
		return nil, err
	}

	sidecars := make([]*BlobSidecar, len(bundle.Blobs))
	for i := range bundle.Blobs {
		// Build a commitment inclusion proof. In a full implementation,
		// this would be a Merkle proof against the beacon block body's
		// blob_kzg_commitments field. Here we derive a placeholder proof
		// by hashing the commitment index with the block hash.
		inclusionProof := deriveInclusionProof(bundle.Commitments[i], blockHash, uint64(i))

		sidecars[i] = &BlobSidecar{
			Index:                    uint64(i),
			Blob:                     bundle.Blobs[i],
			KZGCommitment:            bundle.Commitments[i],
			KZGProof:                 bundle.Proofs[i],
			SignedBlockHeader:        blockHash,
			CommitmentInclusionProof: inclusionProof,
		}
	}
	return sidecars, nil
}

// GetSidecar retrieves a single blob sidecar by index from a bundle.
func GetSidecar(bundle *BlobsBundleV1, index uint64, blockHash types.Hash) (*BlobSidecar, error) {
	if bundle == nil || int(index) >= len(bundle.Blobs) {
		return nil, fmt.Errorf("%w: index %d, bundle has %d blobs",
			ErrBlobBundleSidecarIndex, index, len(bundle.Blobs))
	}

	inclusionProof := deriveInclusionProof(bundle.Commitments[index], blockHash, index)
	return &BlobSidecar{
		Index:                    index,
		Blob:                     bundle.Blobs[index],
		KZGCommitment:            bundle.Commitments[index],
		KZGProof:                 bundle.Proofs[index],
		SignedBlockHeader:        blockHash,
		CommitmentInclusionProof: inclusionProof,
	}, nil
}

// deriveInclusionProof builds a placeholder inclusion proof for a blob
// commitment at the given index. In production this would be a proper
// Merkle branch against the SSZ-serialized beacon block body.
func deriveInclusionProof(commitment []byte, blockHash types.Hash, index uint64) []types.Hash {
	// The Deneb spec defines the inclusion proof as having depth
	// log2(MAX_BLOB_COMMITMENTS_PER_BLOCK) + 1 elements. For our
	// placeholder, we derive deterministic proof nodes.
	const proofDepth = 17 // ceil(log2(4096)) + 1

	proof := make([]types.Hash, proofDepth)
	for i := 0; i < proofDepth; i++ {
		var data []byte
		data = append(data, commitment...)
		data = append(data, blockHash[:]...)
		indexBytes := make([]byte, 8)
		indexBytes[0] = byte(index)
		indexBytes[1] = byte(i)
		data = append(data, indexBytes...)
		proof[i] = crypto.Keccak256Hash(data)
	}
	return proof
}
