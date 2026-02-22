package consensus

import (
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
)

// EIP-7549: Move Committee Index Outside Attestation
//
// Moves the committee index from AttestationData (which is signed) to a
// separate field in the Attestation container. This enables aggregation
// of attestations across committees, significantly reducing the number
// of attestations that need to be included on-chain.

// Post-Electra constants.
const (
	// MaxAttestationsElectra is the maximum attestations per block after Electra.
	// Reduced from 128 to 8 because cross-committee aggregation is now possible.
	MaxAttestationsElectra = 8

	// MaxCommitteesPerSlot is the maximum number of committees in a single slot.
	MaxCommitteesPerSlot = 64
)

// Attestation errors.
var (
	ErrAttestationNilData        = errors.New("attestation: nil attestation data")
	ErrAttestationEmptyBits      = errors.New("attestation: empty aggregation bits")
	ErrAttestationEmptySig       = errors.New("attestation: empty signature")
	ErrAttestationSourceAfterTarget = errors.New("attestation: source epoch after target epoch")
	ErrAttestationFutureSlot     = errors.New("attestation: slot is in the future")
	ErrAttestationEmptyCommittee = errors.New("attestation: empty committee bits")
	ErrAttestationDataMismatch   = errors.New("attestation: data mismatch for aggregation")
	ErrAttestationOverlapping    = errors.New("attestation: overlapping aggregation bits")
)

// AttestationData represents the data that validators sign when attesting.
// Per EIP-7549, the committee index is removed from the signed data,
// enabling cross-committee aggregation.
type AttestationData struct {
	Slot            Slot
	BeaconBlockRoot types.Hash
	Source          Checkpoint
	Target          Checkpoint
}

// Attestation represents a validator attestation in the post-Electra format.
// CommitteeBits replaces the Index field in AttestationData per EIP-7549.
type Attestation struct {
	AggregationBits []byte
	Data            AttestationData
	CommitteeBits   []byte // bitfield indicating which committees are included
	Signature       [96]byte
}

// IsEqualAttestationData checks if two attestation data are equal.
// This is the key function for aggregation: attestations with equal data
// (ignoring committee index) can be aggregated.
func IsEqualAttestationData(a, b *AttestationData) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Slot == b.Slot &&
		a.BeaconBlockRoot == b.BeaconBlockRoot &&
		a.Source.Epoch == b.Source.Epoch &&
		a.Source.Root == b.Source.Root &&
		a.Target.Epoch == b.Target.Epoch &&
		a.Target.Root == b.Target.Root
}

// CreateAttestation creates a new attestation for the given parameters.
func CreateAttestation(
	slot Slot,
	committeeIndex uint64,
	beaconBlockRoot types.Hash,
	source, target Checkpoint,
) *Attestation {
	data := AttestationData{
		Slot:            slot,
		BeaconBlockRoot: beaconBlockRoot,
		Source:          source,
		Target:          target,
	}

	// Encode committee index into committee bits.
	committeeBits := make([]byte, (MaxCommitteesPerSlot+7)/8)
	if committeeIndex < MaxCommitteesPerSlot {
		committeeBits[committeeIndex/8] |= 1 << (committeeIndex % 8)
	}

	return &Attestation{
		AggregationBits: make([]byte, 0),
		Data:            data,
		CommitteeBits:   committeeBits,
	}
}

// GetCommitteeIndices extracts the committee indices from the committee bits.
func GetCommitteeIndices(committeeBits []byte) []uint64 {
	var indices []uint64
	for byteIdx, b := range committeeBits {
		for bitIdx := uint64(0); bitIdx < 8; bitIdx++ {
			if b&(1<<bitIdx) != 0 {
				indices = append(indices, uint64(byteIdx)*8+bitIdx)
			}
		}
	}
	return indices
}

// ValidateAttestation checks that an attestation is well-formed relative
// to the current beacon state.
func ValidateAttestation(att *Attestation, state *BeaconState) error {
	if att == nil {
		return ErrAttestationNilData
	}

	// Signature must not be all zeros.
	emptySig := [96]byte{}
	if att.Signature == emptySig {
		return ErrAttestationEmptySig
	}

	// Committee bits must not be empty (at least one committee).
	if len(att.CommitteeBits) == 0 {
		return ErrAttestationEmptyCommittee
	}
	indices := GetCommitteeIndices(att.CommitteeBits)
	if len(indices) == 0 {
		return ErrAttestationEmptyCommittee
	}

	// Validate each committee index is within bounds.
	for _, idx := range indices {
		if idx >= MaxCommitteesPerSlot {
			return fmt.Errorf("attestation: committee index %d exceeds max %d", idx, MaxCommitteesPerSlot-1)
		}
	}

	// Source epoch must not be after target epoch.
	if att.Data.Source.Epoch > att.Data.Target.Epoch {
		return ErrAttestationSourceAfterTarget
	}

	// Attestation slot must not be in the future relative to beacon state.
	if state != nil && att.Data.Slot > state.Slot {
		return ErrAttestationFutureSlot
	}

	return nil
}

// AggregateAttestations combines multiple attestations that share the same
// AttestationData. Per EIP-7549, attestations from different committees can
// be aggregated because the committee index is no longer part of the signed data.
func AggregateAttestations(atts []*Attestation) (*Attestation, error) {
	if len(atts) == 0 {
		return nil, errors.New("attestation: no attestations to aggregate")
	}
	if len(atts) == 1 {
		return atts[0], nil
	}

	// All attestations must have the same data.
	for i := 1; i < len(atts); i++ {
		if !IsEqualAttestationData(&atts[0].Data, &atts[i].Data) {
			return nil, ErrAttestationDataMismatch
		}
	}

	// Determine the maximum lengths for aggregation and committee bits.
	maxAggLen := 0
	maxCommLen := 0
	for _, att := range atts {
		if len(att.AggregationBits) > maxAggLen {
			maxAggLen = len(att.AggregationBits)
		}
		if len(att.CommitteeBits) > maxCommLen {
			maxCommLen = len(att.CommitteeBits)
		}
	}

	// OR together aggregation bits and committee bits.
	aggBits := make([]byte, maxAggLen)
	commBits := make([]byte, maxCommLen)

	for _, att := range atts {
		for i, b := range att.AggregationBits {
			// Check for overlapping aggregation bits.
			if aggBits[i]&b != 0 {
				return nil, ErrAttestationOverlapping
			}
			aggBits[i] |= b
		}
		for i, b := range att.CommitteeBits {
			commBits[i] |= b
		}
	}

	// Use first attestation's signature as placeholder.
	// In production, aggregate BLS signatures.
	return &Attestation{
		AggregationBits: aggBits,
		Data:            atts[0].Data,
		CommitteeBits:   commBits,
		Signature:       atts[0].Signature,
	}, nil
}
