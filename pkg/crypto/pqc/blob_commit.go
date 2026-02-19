package pqc

import (
	"bytes"
	"errors"

	"golang.org/x/crypto/sha3"
)

// Post-Quantum Blob Commitments (M+ roadmap)
//
// Provides a post-quantum secure blob commitment scheme to replace KZG
// commitments in the long term. This stub uses a hash-based commitment
// as a placeholder until lattice-based polynomial commitment schemes
// are standardized.
//
// Migration path: KZG (current) -> Hybrid KZG+PQ (transition) -> PQ-only (M+)

// Commitment scheme identifiers.
const (
	SchemeLatticeBlobCommit = "lattice-blob-commit-v0"
	SchemeLegacyKZG        = "kzg-v1"
)

// PQ blob commitment errors.
var (
	ErrPQCommitNilData       = errors.New("pqc: nil blob data")
	ErrPQCommitEmptyData     = errors.New("pqc: empty blob data")
	ErrPQCommitInvalid       = errors.New("pqc: invalid commitment")
	ErrPQCommitSchemeMismatch = errors.New("pqc: commitment scheme mismatch")
)

// PQBlobCommitment represents a post-quantum blob commitment.
type PQBlobCommitment struct {
	Scheme     string
	Commitment []byte
	Proof      []byte
}

// PQCommitScheme is the interface for post-quantum commitment schemes.
type PQCommitScheme interface {
	// Name returns the scheme identifier.
	Name() string

	// Commit generates a commitment to the given data.
	Commit(data []byte) (*PQBlobCommitment, error)

	// Verify checks a commitment against the original data.
	Verify(commitment *PQBlobCommitment, data []byte) bool
}

// LatticeBlobCommit is a stub lattice-based blob commitment scheme.
// It uses Keccak-256 as a placeholder for the actual lattice-based
// polynomial commitment that will be used in production.
type LatticeBlobCommit struct{}

// NewLatticeBlobCommit creates a new lattice blob commitment scheme.
func NewLatticeBlobCommit() *LatticeBlobCommit {
	return &LatticeBlobCommit{}
}

// Name returns the scheme identifier.
func (l *LatticeBlobCommit) Name() string {
	return SchemeLatticeBlobCommit
}

// Commit generates a commitment to blob data.
// The stub uses a two-layer hash: commitment = H(data), proof = H(H(data) || data).
func (l *LatticeBlobCommit) Commit(data []byte) (*PQBlobCommitment, error) {
	if data == nil {
		return nil, ErrPQCommitNilData
	}
	if len(data) == 0 {
		return nil, ErrPQCommitEmptyData
	}

	// Commitment: Keccak256(data).
	commitment := keccak256(data)

	// Proof: Keccak256(commitment || data).
	proof := keccak256(append(commitment, data...))

	return &PQBlobCommitment{
		Scheme:     SchemeLatticeBlobCommit,
		Commitment: commitment,
		Proof:      proof,
	}, nil
}

// Verify checks that a commitment is valid for the given data.
func (l *LatticeBlobCommit) Verify(commitment *PQBlobCommitment, data []byte) bool {
	if commitment == nil || data == nil || len(data) == 0 {
		return false
	}
	if commitment.Scheme != SchemeLatticeBlobCommit {
		return false
	}

	// Recompute commitment.
	expectedCommitment := keccak256(data)
	if !bytes.Equal(commitment.Commitment, expectedCommitment) {
		return false
	}

	// Recompute proof.
	expectedProof := keccak256(append(expectedCommitment, data...))
	return bytes.Equal(commitment.Proof, expectedProof)
}

// MigrationPath documents the transition plan from KZG to PQ commitments.
type MigrationPath struct {
	// CurrentScheme is the active commitment scheme.
	CurrentScheme string

	// TargetScheme is the target commitment scheme after migration.
	TargetScheme string

	// HybridMode indicates whether both schemes are active during transition.
	HybridMode bool
}

// DefaultMigrationPath returns the default migration configuration.
func DefaultMigrationPath() *MigrationPath {
	return &MigrationPath{
		CurrentScheme: SchemeLegacyKZG,
		TargetScheme:  SchemeLatticeBlobCommit,
		HybridMode:    false,
	}
}

// keccak256 computes Keccak-256 of data.
func keccak256(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	return h.Sum(nil)
}
