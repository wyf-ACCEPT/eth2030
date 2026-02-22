// blob_validator.go implements EIP-4844 blob sidecar validation for the Engine API.
//
// It validates blob sidecars against block transactions, verifies KZG commitment
// consistency, computes blob gas usage, and calculates the blob base fee using
// the fake exponential function from EIP-4844.
//
// FixedBlobSidecar uses fixed-size arrays for blob data, commitments, and proofs,
// complementing the existing BlobSidecar (which uses dynamic slices).
//
// Reference: EIPs/EIPS/eip-4844.md, consensus-specs/specs/deneb
package engine

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// FixedBlobSidecar represents a blob sidecar with fixed-size arrays.
// This is the consensus-layer representation where blob, commitment, and proof
// sizes are known at compile time. See also BlobSidecar in blobs_bundle.go
// which uses dynamic slices for the engine API wire format.
type FixedBlobSidecar struct {
	// BlobIndex is the index of this blob within the block.
	BlobIndex uint64

	// Blob is the raw blob data (128 KiB = 4096 field elements * 32 bytes).
	Blob [131072]byte

	// KZGCommitment is the KZG commitment to the blob polynomial.
	KZGCommitment [48]byte

	// KZGProof is the KZG proof for the commitment.
	KZGProof [48]byte
}

// Blob validation errors.
var (
	ErrBlobSidecarsNil          = errors.New("blob_validator: sidecars list is nil")
	ErrBlobSidecarNil           = errors.New("blob_validator: nil sidecar entry")
	ErrBlobTooManySidecars      = errors.New("blob_validator: too many sidecars")
	ErrBlobIndexOutOfOrder      = errors.New("blob_validator: blob index out of order")
	ErrBlobIndexDuplicate       = errors.New("blob_validator: duplicate blob index")
	ErrBlobIndexTooLarge        = errors.New("blob_validator: blob index exceeds expected count")
	ErrBlobCountMismatch        = errors.New("blob_validator: sidecar count does not match transaction blobs")
	ErrBlobCommitmentMismatch   = errors.New("blob_validator: KZG commitment does not match versioned hash")
	ErrBlobCommitmentsNil       = errors.New("blob_validator: commitments list is nil")
	ErrBlobBlobsNil             = errors.New("blob_validator: blobs list is nil")
	ErrBlobCommitmentCountWrong = errors.New("blob_validator: commitments count does not match blobs count")
	ErrBlobCommitmentEmpty      = errors.New("blob_validator: empty KZG commitment")
	ErrBlobTxSidecarNil         = errors.New("blob_validator: sidecar is nil")
	ErrBlobTxNotBlobType        = errors.New("blob_validator: transaction is not a blob transaction")
	ErrBlobTxHashMismatch       = errors.New("blob_validator: versioned hash does not match commitment")
)

// BlobValidator validates EIP-4844 blob sidecars and blob gas computations.
type BlobValidator struct {
	// maxBlobsPerBlock is the maximum number of blobs per block.
	maxBlobsPerBlock int

	// targetBlobsPerBlock is the target number of blobs per block for gas pricing.
	targetBlobsPerBlock int

	// blobGasPerBlob is the gas consumed per blob.
	blobGasPerBlob uint64
}

// NewBlobValidator creates a new BlobValidator with Cancun/Deneb defaults.
func NewBlobValidator() *BlobValidator {
	return &BlobValidator{
		maxBlobsPerBlock:    int(types.MaxBlobGasPerBlock / types.BlobTxBlobGasPerBlob),
		targetBlobsPerBlock: int(types.TargetBlobGasPerBlock / types.BlobTxBlobGasPerBlob),
		blobGasPerBlob:      types.BlobTxBlobGasPerBlob,
	}
}

// NewBlobValidatorWithConfig creates a BlobValidator with custom parameters.
func NewBlobValidatorWithConfig(maxBlobs, targetBlobs int, gasPerBlob uint64) *BlobValidator {
	return &BlobValidator{
		maxBlobsPerBlock:    maxBlobs,
		targetBlobsPerBlock: targetBlobs,
		blobGasPerBlob:      gasPerBlob,
	}
}

// ValidateBlobSidecars validates a list of fixed blob sidecars against the given
// block hash. It checks that:
//   - The sidecars list is non-nil.
//   - The number of sidecars does not exceed the maximum.
//   - Blob indices are sequential starting from 0.
//   - No duplicate blob indices exist.
//   - KZG commitments are non-zero.
func (v *BlobValidator) ValidateBlobSidecars(sidecars []FixedBlobSidecar, blockHash [32]byte) error {
	if sidecars == nil {
		return ErrBlobSidecarsNil
	}
	if len(sidecars) > v.maxBlobsPerBlock {
		return fmt.Errorf("%w: got %d, max %d",
			ErrBlobTooManySidecars, len(sidecars), v.maxBlobsPerBlock)
	}

	seen := make(map[uint64]bool, len(sidecars))
	for i, sc := range sidecars {
		// Check sequential ordering.
		if sc.BlobIndex != uint64(i) {
			return fmt.Errorf("%w: expected index %d, got %d",
				ErrBlobIndexOutOfOrder, i, sc.BlobIndex)
		}
		if seen[sc.BlobIndex] {
			return fmt.Errorf("%w: index %d", ErrBlobIndexDuplicate, sc.BlobIndex)
		}
		seen[sc.BlobIndex] = true

		if sc.BlobIndex >= uint64(v.maxBlobsPerBlock) {
			return fmt.Errorf("%w: index %d >= max %d",
				ErrBlobIndexTooLarge, sc.BlobIndex, v.maxBlobsPerBlock)
		}

		// Verify the KZG commitment is not empty.
		if sc.KZGCommitment == ([48]byte{}) {
			return fmt.Errorf("%w at index %d", ErrBlobCommitmentEmpty, i)
		}
	}
	return nil
}

// ValidateKZGCommitments checks that each KZG commitment corresponds to a blob.
// It verifies structural consistency: commitments and blobs must be the same length,
// neither may be nil, and each commitment must be non-zero.
//
// Note: Full cryptographic verification of KZG proofs requires the trusted setup
// and is handled by the crypto/kzg package. This method only checks structural validity.
func (v *BlobValidator) ValidateKZGCommitments(commitments [][48]byte, blobs [][131072]byte) error {
	if commitments == nil {
		return ErrBlobCommitmentsNil
	}
	if blobs == nil {
		return ErrBlobBlobsNil
	}
	if len(commitments) != len(blobs) {
		return fmt.Errorf("%w: %d commitments, %d blobs",
			ErrBlobCommitmentCountWrong, len(commitments), len(blobs))
	}

	for i, c := range commitments {
		if c == ([48]byte{}) {
			return fmt.Errorf("%w at index %d", ErrBlobCommitmentEmpty, i)
		}
	}
	return nil
}

// ComputeBlobGas returns the total blob gas for a given number of blobs.
func ComputeBlobGas(numBlobs int) uint64 {
	return uint64(numBlobs) * types.BlobTxBlobGasPerBlob
}

// ComputeExcessBlobGas calculates the excess blob gas for the current block
// given the parent's excess and used blob gas. Per EIP-4844:
//
//	excess = max(0, parent_excess + parent_used - target)
func ComputeExcessBlobGas(parentExcess, parentUsed uint64) uint64 {
	return types.CalcExcessBlobGas(parentExcess, parentUsed)
}

// ComputeBlobBaseFee calculates the blob base fee (in wei) from the excess
// blob gas using the EIP-4844 fake exponential formula:
//
//	fee = fake_exponential(MIN_BLOB_GASPRICE, excess_blob_gas, BLOB_BASE_FEE_UPDATE_FRACTION)
func ComputeBlobBaseFee(excessBlobGas uint64) *big.Int {
	return types.CalcBlobFee(excessBlobGas)
}

// ValidateBlobTransactionSidecar validates that a blob transaction's versioned
// hashes are consistent with the given sidecar. It computes the versioned hash
// from the KZG commitment (keccak256(commitment) with version byte 0x01) and
// compares it against the transaction's blob hashes.
func ValidateBlobTransactionSidecar(tx *types.Transaction, sidecar *FixedBlobSidecar) error {
	if sidecar == nil {
		return ErrBlobTxSidecarNil
	}
	if tx == nil {
		return fmt.Errorf("%w: transaction is nil", ErrBlobTxNotBlobType)
	}
	if tx.Type() != types.BlobTxType {
		return fmt.Errorf("%w: type 0x%02x", ErrBlobTxNotBlobType, tx.Type())
	}

	hashes := tx.BlobHashes()
	if int(sidecar.BlobIndex) >= len(hashes) {
		return fmt.Errorf("%w: blob index %d >= blob hash count %d",
			ErrBlobIndexTooLarge, sidecar.BlobIndex, len(hashes))
	}

	// Compute the versioned hash from the KZG commitment.
	// versioned_hash = 0x01 || keccak256(commitment)[1:]
	commitHash := crypto.Keccak256(sidecar.KZGCommitment[:])
	var versionedHash types.Hash
	versionedHash[0] = types.VersionedHashVersionKZG
	copy(versionedHash[1:], commitHash[1:])

	expectedHash := hashes[sidecar.BlobIndex]
	if versionedHash != expectedHash {
		return fmt.Errorf("%w at blob index %d: computed %s, expected %s",
			ErrBlobTxHashMismatch, sidecar.BlobIndex,
			versionedHash.Hex(), expectedHash.Hex())
	}
	return nil
}

// MaxBlobsPerBlock returns the configured maximum blobs per block.
func (v *BlobValidator) MaxBlobsPerBlock() int {
	return v.maxBlobsPerBlock
}

// TargetBlobsPerBlock returns the configured target blobs per block.
func (v *BlobValidator) TargetBlobsPerBlock() int {
	return v.targetBlobsPerBlock
}

// BlobGasPerBlob returns the configured gas per blob.
func (v *BlobValidator) BlobGasPerBlob() uint64 {
	return v.blobGasPerBlob
}

// CountBlobsInTransactions counts the total number of blobs referenced by
// blob transactions in the given list.
func CountBlobsInTransactions(txs []*types.Transaction) int {
	count := 0
	for _, tx := range txs {
		if tx != nil && tx.Type() == types.BlobTxType {
			count += len(tx.BlobHashes())
		}
	}
	return count
}

// VerifySidecarCount checks that the number of fixed blob sidecars matches the
// total number of blobs referenced by transactions.
func (v *BlobValidator) VerifySidecarCount(sidecars []FixedBlobSidecar, txs []*types.Transaction) error {
	expectedCount := CountBlobsInTransactions(txs)
	if len(sidecars) != expectedCount {
		return fmt.Errorf("%w: got %d sidecars, expected %d from transactions",
			ErrBlobCountMismatch, len(sidecars), expectedCount)
	}
	return nil
}
