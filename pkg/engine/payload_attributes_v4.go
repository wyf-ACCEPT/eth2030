// payload_attributes_v4.go implements payload attribute extensions for Prague+.
//
// PayloadAttributesV4Prague extends V3 with target_blob_count and max_blob_count
// fields that the consensus layer passes to the execution layer for blob gas
// pricing calibration. These fields were introduced alongside EIP-7742 which
// allows the CL to signal dynamic blob parameters to the EL.
//
// Note: The existing PayloadAttributesV4 in types.go is the Amsterdam/FOCIL
// variant with slot number and inclusion list. This file adds the Prague blob
// count extension as a separate type to avoid conflicts.
package engine

import (
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
)

// Blob count constants for Prague payload attributes.
const (
	// DefaultTargetBlobCount is the default target number of blobs per block.
	DefaultTargetBlobCount uint64 = 3

	// DefaultMaxBlobCount is the default maximum number of blobs per block.
	DefaultMaxBlobCount uint64 = 6

	// AbsoluteMaxBlobCount is the absolute maximum blob count the EL supports.
	AbsoluteMaxBlobCount uint64 = 16

	// MinTargetBlobCount is the minimum allowed target blob count.
	MinTargetBlobCount uint64 = 1
)

// Prague payload attributes errors.
var (
	ErrPragueAttrNil             = errors.New("payload_attributes_v4: nil attributes")
	ErrPragueAttrTargetZero      = errors.New("payload_attributes_v4: target blob count is zero")
	ErrPragueAttrMaxZero         = errors.New("payload_attributes_v4: max blob count is zero")
	ErrPragueAttrTargetExceedsMax = errors.New("payload_attributes_v4: target blob count exceeds max blob count")
	ErrPragueAttrMaxTooLarge     = errors.New("payload_attributes_v4: max blob count exceeds absolute maximum")
	ErrPragueAttrTargetTooSmall  = errors.New("payload_attributes_v4: target blob count below minimum")
	ErrPragueAttrBeaconRoot      = errors.New("payload_attributes_v4: parent beacon block root is zero")
	ErrPragueAttrTimestampZero   = errors.New("payload_attributes_v4: timestamp is zero")
	ErrPragueAttrTimestampOld    = errors.New("payload_attributes_v4: timestamp not after parent")
)

// PayloadAttributesV4Prague extends V3 payload attributes with blob count
// parameters per EIP-7742. The consensus layer signals these values to let
// the execution layer adjust blob gas pricing dynamically.
type PayloadAttributesV4Prague struct {
	PayloadAttributesV3

	// TargetBlobCount is the target number of blobs per block for gas pricing.
	// The blob base fee adjusts to drive the average blob count toward this target.
	TargetBlobCount uint64 `json:"targetBlobCount"`

	// MaxBlobCount is the maximum number of blobs allowed in a single block.
	// Blob transactions that would exceed this limit are not included.
	MaxBlobCount uint64 `json:"maxBlobCount"`
}

// ValidatePraguePayloadAttributes performs full validation of Prague payload
// attributes. It checks:
//   - Attributes are non-nil.
//   - Timestamp is non-zero and after parent.
//   - Beacon block root is present.
//   - Target blob count is within [MinTargetBlobCount, maxBlobCount].
//   - Max blob count is within (0, AbsoluteMaxBlobCount].
//   - Withdrawals are structurally valid.
func ValidatePraguePayloadAttributes(
	attrs *PayloadAttributesV4Prague,
	parentTimestamp uint64,
) error {
	if attrs == nil {
		return ErrPragueAttrNil
	}

	// Validate timestamp.
	if attrs.Timestamp == 0 {
		return ErrPragueAttrTimestampZero
	}
	if parentTimestamp > 0 && attrs.Timestamp <= parentTimestamp {
		return fmt.Errorf("%w: %d <= parent %d",
			ErrPragueAttrTimestampOld, attrs.Timestamp, parentTimestamp)
	}

	// Validate beacon root.
	if attrs.ParentBeaconBlockRoot.IsZero() {
		return ErrPragueAttrBeaconRoot
	}

	// Validate blob counts.
	if attrs.MaxBlobCount == 0 {
		return ErrPragueAttrMaxZero
	}
	if attrs.MaxBlobCount > AbsoluteMaxBlobCount {
		return fmt.Errorf("%w: %d > %d",
			ErrPragueAttrMaxTooLarge, attrs.MaxBlobCount, AbsoluteMaxBlobCount)
	}
	if attrs.TargetBlobCount == 0 {
		return ErrPragueAttrTargetZero
	}
	if attrs.TargetBlobCount < MinTargetBlobCount {
		return fmt.Errorf("%w: %d < %d",
			ErrPragueAttrTargetTooSmall, attrs.TargetBlobCount, MinTargetBlobCount)
	}
	if attrs.TargetBlobCount > attrs.MaxBlobCount {
		return fmt.Errorf("%w: target %d > max %d",
			ErrPragueAttrTargetExceedsMax, attrs.TargetBlobCount, attrs.MaxBlobCount)
	}

	// Validate withdrawals via the shared helper.
	if err := validateWithdrawals(attrs.Withdrawals); err != nil {
		return err
	}

	return nil
}

// ComputeBlobGasTarget returns the per-block blob gas target based on the
// target blob count. This is target_blob_count * blob_gas_per_blob.
func ComputeBlobGasTarget(targetBlobCount uint64) uint64 {
	return targetBlobCount * types.BlobTxBlobGasPerBlob
}

// ComputeMaxBlobGas returns the per-block maximum blob gas based on the
// max blob count. This is max_blob_count * blob_gas_per_blob.
func ComputeMaxBlobGas(maxBlobCount uint64) uint64 {
	return maxBlobCount * types.BlobTxBlobGasPerBlob
}

// NewDefaultPragueAttributes creates a PayloadAttributesV4Prague with default
// blob count parameters. Callers must still fill in timestamp, prevRandao,
// fee recipient, withdrawals, and beacon root.
func NewDefaultPragueAttributes() *PayloadAttributesV4Prague {
	return &PayloadAttributesV4Prague{
		TargetBlobCount: DefaultTargetBlobCount,
		MaxBlobCount:    DefaultMaxBlobCount,
	}
}

// BlobCountsEqual checks whether two sets of blob count parameters are equal.
func BlobCountsEqual(a, b *PayloadAttributesV4Prague) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.TargetBlobCount == b.TargetBlobCount && a.MaxBlobCount == b.MaxBlobCount
}

// DerivePayloadIDPrague computes a deterministic PayloadID from Prague
// attributes. It extends the V3 derivation by mixing in blob count parameters.
func DerivePayloadIDPrague(parentHash types.Hash, attrs *PayloadAttributesV4Prague) PayloadID {
	if attrs == nil {
		return PayloadID{}
	}

	// Use the V3 derivation as the base, then mix in blob counts.
	baseID := DerivePayloadIDV3(parentHash, &attrs.PayloadAttributesV3)

	// XOR the blob counts into the last 4 bytes for differentiation.
	baseID[4] ^= byte(attrs.TargetBlobCount)
	baseID[5] ^= byte(attrs.TargetBlobCount >> 8)
	baseID[6] ^= byte(attrs.MaxBlobCount)
	baseID[7] ^= byte(attrs.MaxBlobCount >> 8)

	return baseID
}
