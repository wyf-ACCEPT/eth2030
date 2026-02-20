package consensus

import (
	"errors"
	"sort"
)

// Attester capacity constants for the 1MiB attestation cap.
const (
	// MaxAttestationSizeBytes is the 1 MiB hard cap on total attestation
	// data per slot, limiting bandwidth consumed by attestation gossip.
	MaxAttestationSizeBytes uint64 = 1 << 20 // 1,048,576 bytes

	// BaseAttestationSize is the base overhead per attestation in bytes
	// (committee bits, signature, attestation data without aggregation bits).
	BaseAttestationSize uint64 = 228

	// AggregationBitsPerValidator is the number of bits used per validator
	// in the aggregation bitfield (1 bit per validator, rounded up to bytes).
	AggregationBitsPerValidator uint64 = 1

	// MaxAggregateSignatureSize is the BLS signature size in bytes.
	MaxAggregateSignatureSize uint64 = 96

	// DefaultMaxAttestationsPerSlot is the baseline maximum attestations
	// per slot under the 1MiB cap.
	DefaultMaxAttestationsPerSlot uint64 = 128
)

// Attester capacity errors.
var (
	ErrAttestationCapExceeded  = errors.New("attestation: slot capacity exceeded")
	ErrAttestationTooLarge     = errors.New("attestation: single attestation exceeds size limit")
	ErrEmptyCommitteeList      = errors.New("attestation: empty committee list")
)

// AttestationSizeEstimate estimates the encoded size of a single attestation
// based on the committee size.
func AttestationSizeEstimate(committeeSize uint64) uint64 {
	// Size = base + ceil(committeeSize / 8) for aggregation bits.
	aggBytes := (committeeSize + 7) / 8
	return BaseAttestationSize + aggBytes
}

// MaxAttestationsForBudget computes how many attestations of the given
// committee size fit within the 1MiB slot budget.
func MaxAttestationsForBudget(committeeSize uint64) uint64 {
	perAttestation := AttestationSizeEstimate(committeeSize)
	if perAttestation == 0 {
		return 0
	}
	return MaxAttestationSizeBytes / perAttestation
}

// CommitteeAttestationPlan describes how attestations should be packed
// from multiple committees within a slot's bandwidth budget.
type CommitteeAttestationPlan struct {
	CommitteeIndex     uint64
	CommitteeSize      uint64
	MaxAttestations    uint64
	EstimatedSizeBytes uint64
}

// PlanCommitteeAttestations calculates an attestation packing plan for a
// set of committees within the 1MiB budget. Committees are sorted by size
// (smallest first) to maximize the number of distinct committees included.
func PlanCommitteeAttestations(committees []uint64) ([]CommitteeAttestationPlan, error) {
	if len(committees) == 0 {
		return nil, ErrEmptyCommitteeList
	}

	// Build index+size pairs and sort by size ascending.
	type indexedComm struct {
		index uint64
		size  uint64
	}
	sorted := make([]indexedComm, len(committees))
	for i, size := range committees {
		sorted[i] = indexedComm{index: uint64(i), size: size}
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].size < sorted[j].size
	})

	budgetRemaining := MaxAttestationSizeBytes
	plans := make([]CommitteeAttestationPlan, 0, len(committees))

	for _, c := range sorted {
		perAttestation := AttestationSizeEstimate(c.size)
		if perAttestation > budgetRemaining {
			continue
		}

		// Allocate as many attestations as fit for this committee.
		maxForCommittee := budgetRemaining / perAttestation
		if maxForCommittee == 0 {
			continue
		}

		// Cap at 1 aggregate per committee (cross-committee aggregation).
		if maxForCommittee > 1 {
			maxForCommittee = 1
		}

		totalSize := maxForCommittee * perAttestation
		budgetRemaining -= totalSize

		plans = append(plans, CommitteeAttestationPlan{
			CommitteeIndex:     c.index,
			CommitteeSize:      c.size,
			MaxAttestations:    maxForCommittee,
			EstimatedSizeBytes: totalSize,
		})
	}

	return plans, nil
}

// BandwidthEstimate estimates the total bandwidth in bytes consumed by
// attestations for a slot given committee sizes.
func BandwidthEstimate(committees []uint64) uint64 {
	var total uint64
	for _, size := range committees {
		total += AttestationSizeEstimate(size)
	}
	return total
}

// IsWithinBudget returns true if the total attestation bandwidth for
// the given committees fits within the 1MiB cap.
func IsWithinBudget(committees []uint64) bool {
	return BandwidthEstimate(committees) <= MaxAttestationSizeBytes
}

// AggregateSizeLimit returns the maximum size in bytes for an aggregated
// attestation covering the given number of validators.
func AggregateSizeLimit(validatorCount uint64) uint64 {
	aggBitsBytes := (validatorCount + 7) / 8
	return BaseAttestationSize + aggBitsBytes
}

// CapEffectiveBalances applies the 1MiB attester cap to a slice of
// effective balances. Validators exceeding the cap have their balance
// capped, and the function returns the capped balances and the total
// capped weight.
func CapEffectiveBalances(balances []uint64, maxCap uint64) ([]uint64, uint64) {
	capped := make([]uint64, len(balances))
	var total uint64
	for i, bal := range balances {
		if bal > maxCap {
			capped[i] = maxCap
		} else {
			capped[i] = bal
		}
		total += capped[i]
	}
	return capped, total
}

// OptimalCommitteeCount computes the number of committees for a slot
// that keeps the total attestation size within the 1MiB budget.
func OptimalCommitteeCount(totalValidators, maxCommitteesPerSlot uint64) uint64 {
	if totalValidators == 0 || maxCommitteesPerSlot == 0 {
		return 0
	}

	for n := maxCommitteesPerSlot; n > 0; n-- {
		committeeSize := totalValidators / n
		if committeeSize == 0 {
			continue
		}
		totalSize := n * AttestationSizeEstimate(committeeSize)
		if totalSize <= MaxAttestationSizeBytes {
			return n
		}
	}
	return 1
}

// VirtualAttesterCount computes the number of "virtual" attesters produced
// by applying the attester cap. With a cap of maxCap, a validator with
// balance > maxCap counts as ceil(balance / maxCap) virtual attesters.
func VirtualAttesterCount(balances []uint64, maxCap uint64) uint64 {
	if maxCap == 0 {
		return 0
	}
	var count uint64
	for _, bal := range balances {
		count += (bal + maxCap - 1) / maxCap
	}
	return count
}

// EffectiveSlotCapacity returns the effective number of attestations that
// can be included in a slot given the total active validators and the
// number of committees.
func EffectiveSlotCapacity(totalValidators, committees uint64) uint64 {
	if committees == 0 {
		return 0
	}
	committeeSize := totalValidators / committees
	perAttestation := AttestationSizeEstimate(committeeSize)
	if perAttestation == 0 {
		return 0
	}
	return MaxAttestationSizeBytes / perAttestation
}
