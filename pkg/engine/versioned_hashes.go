// versioned_hashes.go implements versioned hash computation and validation
// for EIP-4844 blob transactions.
//
// A versioned hash is computed from a KZG commitment by taking SHA-256 of
// the commitment and replacing the first byte with a version byte (0x01 for
// KZG). This allows the EL to verify blob transaction hashes against the
// actual KZG commitments provided by the CL.
//
// This file provides utilities for:
//   - Computing versioned hashes from raw KZG commitments
//   - Batch computation and validation against blob transactions
//   - Version byte extraction and checking
//   - Commitment-to-hash mapping construction
//
// The core VersionedHash function and ValidateVersionedHashes are in
// blobs_bundle.go. This file adds higher-level utilities.
package engine

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
)

// Version byte constants for versioned hashes.
const (
	// VersionKZG is the version byte for KZG-committed blobs (EIP-4844).
	VersionKZG byte = 0x01

	// VersionFuture is reserved for future hash version schemes.
	VersionFuture byte = 0x02

	// VersionedHashLen is the byte length of a versioned hash (same as Hash).
	VersionedHashLen = 32
)

// Versioned hash errors.
var (
	ErrVHCommitmentNil    = errors.New("versioned_hashes: nil commitment")
	ErrVHCommitmentSize   = errors.New("versioned_hashes: invalid commitment size")
	ErrVHInvalidVersion   = errors.New("versioned_hashes: invalid version byte")
	ErrVHCountMismatch    = errors.New("versioned_hashes: hash count mismatch")
	ErrVHHashMismatch     = errors.New("versioned_hashes: hash mismatch at index")
	ErrVHEmptyCommitments = errors.New("versioned_hashes: empty commitments list")
	ErrVHTxNotBlob        = errors.New("versioned_hashes: transaction is not a blob transaction")
	ErrVHTxNoBlobHashes   = errors.New("versioned_hashes: blob transaction has no versioned hashes")
)

// ComputeVersionedHash computes an EIP-4844 versioned hash from a raw KZG
// commitment. The result is SHA-256(commitment) with byte[0] replaced by the
// version byte (0x01).
//
// This is equivalent to the VersionedHash function in blobs_bundle.go but
// accepts a version byte parameter for extensibility.
func ComputeVersionedHash(commitment []byte, version byte) (types.Hash, error) {
	if commitment == nil {
		return types.Hash{}, ErrVHCommitmentNil
	}
	if len(commitment) != KZGCommitmentSize {
		return types.Hash{}, fmt.Errorf("%w: got %d, want %d",
			ErrVHCommitmentSize, len(commitment), KZGCommitmentSize)
	}

	h := sha256.Sum256(commitment)
	h[0] = version
	return types.Hash(h), nil
}

// ComputeVersionedHashKZG is a convenience wrapper that computes a versioned
// hash using the KZG version byte (0x01).
func ComputeVersionedHashKZG(commitment []byte) (types.Hash, error) {
	return ComputeVersionedHash(commitment, VersionKZG)
}

// BatchComputeVersionedHashes computes versioned hashes for a list of KZG
// commitments. Returns an error if any commitment is invalid.
func BatchComputeVersionedHashes(commitments [][]byte) ([]types.Hash, error) {
	if len(commitments) == 0 {
		return nil, nil
	}

	hashes := make([]types.Hash, len(commitments))
	for i, c := range commitments {
		h, err := ComputeVersionedHashKZG(c)
		if err != nil {
			return nil, fmt.Errorf("commitment %d: %w", i, err)
		}
		hashes[i] = h
	}
	return hashes, nil
}

// VerifyVersionedHashesAgainstCommitments verifies that a list of expected
// versioned hashes matches hashes derived from the provided commitments.
// Both lists must have the same length and each hash must match exactly.
func VerifyVersionedHashesAgainstCommitments(
	expectedHashes []types.Hash,
	commitments [][]byte,
) error {
	if len(expectedHashes) != len(commitments) {
		return fmt.Errorf("%w: expected %d, got %d commitments",
			ErrVHCountMismatch, len(expectedHashes), len(commitments))
	}

	for i, commitment := range commitments {
		computed, err := ComputeVersionedHashKZG(commitment)
		if err != nil {
			return fmt.Errorf("commitment %d: %w", i, err)
		}
		if computed != expectedHashes[i] {
			return fmt.Errorf("%w %d: computed %s, expected %s",
				ErrVHHashMismatch, i, computed.Hex(), expectedHashes[i].Hex())
		}
	}
	return nil
}

// ExtractVersionByte returns the version byte from a versioned hash.
func ExtractVersionByte(hash types.Hash) byte {
	return hash[0]
}

// IsKZGVersionedHash checks whether a hash has the KZG version byte (0x01).
func IsKZGVersionedHash(hash types.Hash) bool {
	return hash[0] == VersionKZG
}

// ValidateVersionByte checks that a versioned hash has the expected version byte.
func ValidateVersionByte(hash types.Hash, expectedVersion byte) error {
	if hash[0] != expectedVersion {
		return fmt.Errorf("%w: got 0x%02x, want 0x%02x",
			ErrVHInvalidVersion, hash[0], expectedVersion)
	}
	return nil
}

// ValidateBlobTxVersionedHashes validates that a blob transaction's versioned
// hashes all have the correct KZG version byte and match the provided
// commitments. This combines version byte validation with commitment verification.
func ValidateBlobTxVersionedHashes(tx *types.Transaction, commitments [][]byte) error {
	if tx == nil {
		return ErrVHTxNotBlob
	}
	if tx.Type() != types.BlobTxType {
		return fmt.Errorf("%w: type 0x%02x", ErrVHTxNotBlob, tx.Type())
	}

	hashes := tx.BlobHashes()
	if len(hashes) == 0 {
		return ErrVHTxNoBlobHashes
	}

	// Verify version bytes.
	for i, h := range hashes {
		if err := ValidateVersionByte(h, VersionKZG); err != nil {
			return fmt.Errorf("blob hash %d: %w", i, err)
		}
	}

	// Verify against commitments.
	return VerifyVersionedHashesAgainstCommitments(hashes, commitments)
}

// BuildCommitmentHashMap creates a mapping from versioned hash to the
// commitment that produced it. Useful for looking up the commitment
// corresponding to a versioned hash in a blob transaction.
func BuildCommitmentHashMap(commitments [][]byte) (map[types.Hash][]byte, error) {
	result := make(map[types.Hash][]byte, len(commitments))
	for i, c := range commitments {
		h, err := ComputeVersionedHashKZG(c)
		if err != nil {
			return nil, fmt.Errorf("commitment %d: %w", i, err)
		}
		result[h] = c
	}
	return result, nil
}

// CollectBlobHashesFromTransactions extracts all versioned blob hashes from
// a list of transactions, returning them in order. Non-blob transactions
// are skipped.
func CollectBlobHashesFromTransactions(txs []*types.Transaction) []types.Hash {
	var hashes []types.Hash
	for _, tx := range txs {
		if tx != nil && tx.Type() == types.BlobTxType {
			hashes = append(hashes, tx.BlobHashes()...)
		}
	}
	return hashes
}

// VerifyAllBlobVersionBytes checks that all versioned hashes in the list
// have the KZG version byte. Returns an error at the first mismatch.
func VerifyAllBlobVersionBytes(hashes []types.Hash) error {
	for i, h := range hashes {
		if h[0] != VersionKZG {
			return fmt.Errorf("%w at index %d: got 0x%02x",
				ErrVHInvalidVersion, i, h[0])
		}
	}
	return nil
}
