package core

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// EIP-4844 blob transaction constants.
const (
	// MaxBlobGasPerBlock is the maximum blob gas allowed in a single block.
	MaxBlobGasPerBlock = 786432

	// TargetBlobGasPerBlock is the target blob gas per block for the
	// EIP-4844 blob base fee adjustment mechanism.
	TargetBlobGasPerBlock = 393216

	// GasPerBlob is the gas consumed by each blob (2^17).
	GasPerBlob = 131072

	// BlobTxHashVersion is the required first byte of each versioned blob hash.
	BlobTxHashVersion = 0x01

	// MaxBlobsPerBlock is the maximum number of blobs per block.
	MaxBlobsPerBlock = 6
)

var (
	ErrBlobTxNoBlobHashes     = errors.New("blob transaction must have at least one blob hash")
	ErrBlobTxTooManyBlobs     = errors.New("blob transaction exceeds maximum blobs per block")
	ErrBlobTxInvalidHashVersion = errors.New("blob hash has invalid version byte")
	ErrBlobFeeCapTooLow       = errors.New("max fee per blob gas too low")
	ErrBlobGasUsedNil         = errors.New("post-Cancun block missing BlobGasUsed")
	ErrBlobGasUsedExceeded    = errors.New("block blob gas used exceeds maximum")
	ErrExcessBlobGasNil       = errors.New("post-Cancun block missing ExcessBlobGas")
	ErrExcessBlobGasMismatch  = errors.New("block excess blob gas does not match calculated value")
)

// ValidateBlobTx validates an EIP-4844 blob transaction against protocol rules.
// It checks that the transaction has a valid number of blob hashes, each hash
// starts with the correct version byte, and the max fee per blob gas is
// sufficient to cover the current blob base fee.
func ValidateBlobTx(tx *types.Transaction, excessBlobGas uint64) error {
	hashes := tx.BlobHashes()
	if len(hashes) == 0 {
		return ErrBlobTxNoBlobHashes
	}
	if len(hashes) > MaxBlobsPerBlock {
		return fmt.Errorf("%w: have %d, max %d", ErrBlobTxTooManyBlobs, len(hashes), MaxBlobsPerBlock)
	}

	for i, h := range hashes {
		if h[0] != BlobTxHashVersion {
			return fmt.Errorf("%w: hash %d has version 0x%02x, want 0x%02x", ErrBlobTxInvalidHashVersion, i, h[0], BlobTxHashVersion)
		}
	}

	blobBaseFee := calcBlobBaseFee(excessBlobGas)
	maxFeePerBlobGas := tx.BlobGasFeeCap()
	if maxFeePerBlobGas == nil || maxFeePerBlobGas.Cmp(blobBaseFee) < 0 {
		return fmt.Errorf("%w: have %v, want at least %v", ErrBlobFeeCapTooLow, maxFeePerBlobGas, blobBaseFee)
	}

	return nil
}

// CalcExcessBlobGas computes the excess blob gas for a block given the
// parent block's excess blob gas and blob gas used. Per EIP-4844, excess
// is carried forward and adjusted by the target each block.
func CalcExcessBlobGas(parentExcessBlobGas, parentBlobGasUsed uint64) uint64 {
	sum := parentExcessBlobGas + parentBlobGasUsed
	if sum < TargetBlobGasPerBlock {
		return 0
	}
	return sum - TargetBlobGasPerBlock
}

// CountBlobGas returns the total blob gas consumed by a transaction.
// Non-blob transactions return 0.
func CountBlobGas(tx *types.Transaction) uint64 {
	return GasPerBlob * uint64(len(tx.BlobHashes()))
}

// ValidateBlockBlobGas validates blob gas fields in a post-Cancun block header.
// It checks that BlobGasUsed and ExcessBlobGas are present, that BlobGasUsed
// does not exceed the per-block maximum, and that ExcessBlobGas matches the
// value calculated from the parent header.
func ValidateBlockBlobGas(header *types.Header, parentHeader *types.Header) error {
	if header.BlobGasUsed == nil {
		return ErrBlobGasUsedNil
	}
	if *header.BlobGasUsed > MaxBlobGasPerBlock {
		return fmt.Errorf("%w: have %d, max %d", ErrBlobGasUsedExceeded, *header.BlobGasUsed, MaxBlobGasPerBlock)
	}

	if header.ExcessBlobGas == nil {
		return ErrExcessBlobGasNil
	}

	// Calculate the expected excess blob gas from parent.
	var parentExcess, parentUsed uint64
	if parentHeader.ExcessBlobGas != nil {
		parentExcess = *parentHeader.ExcessBlobGas
	}
	if parentHeader.BlobGasUsed != nil {
		parentUsed = *parentHeader.BlobGasUsed
	}
	expectedExcess := CalcExcessBlobGas(parentExcess, parentUsed)

	if *header.ExcessBlobGas != expectedExcess {
		return fmt.Errorf("%w: have %d, want %d", ErrExcessBlobGasMismatch, *header.ExcessBlobGas, expectedExcess)
	}

	return nil
}

// CalcBlobBaseFee returns the blob base fee given the excess blob gas.
// This is a convenience wrapper for use by external callers.
func CalcBlobBaseFee(excessBlobGas uint64) *big.Int {
	return calcBlobBaseFee(excessBlobGas)
}
