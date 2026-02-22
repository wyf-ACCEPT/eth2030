package consensus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// EIP-7549: Move Committee Index Outside Attestation
//
// This file provides an alternative attestation model with the committee index
// moved to a top-level field in the attestation container (IndexedAttestation).
// This enables aggregation of attestations from the same slot/checkpoint data
// across different committees, reducing on-chain attestation volume.

// IndexedAttestation errors.
var (
	ErrIndexedAttNil            = errors.New("eip7549: nil indexed attestation")
	ErrIndexedAttEmptyBits      = errors.New("eip7549: empty aggregation bits")
	ErrIndexedAttSourceNil      = errors.New("eip7549: nil source checkpoint")
	ErrIndexedAttTargetNil      = errors.New("eip7549: nil target checkpoint")
	ErrIndexedAttSourceAfter    = errors.New("eip7549: source epoch after target epoch")
	ErrIndexedAttCommitteeRange = errors.New("eip7549: committee index out of range")
	ErrIndexedAttNotAggregatable = errors.New("eip7549: attestations are not aggregatable")
	ErrIndexedAttPoolDuplicate  = errors.New("eip7549: duplicate attestation in pool")
	ErrIndexedAttValidatorRange = errors.New("eip7549: aggregation bits exceed validator count")
)

// Checkpoint7549 represents a finality checkpoint used in indexed attestations.
type Checkpoint7549 struct {
	Epoch uint64
	Root  types.Hash
}

// IndexedAttestation is the EIP-7549 attestation format where the committee
// index is a top-level field rather than part of the signed attestation data.
type IndexedAttestation struct {
	Slot            uint64
	BeaconBlockRoot types.Hash
	Source          *Checkpoint7549
	Target          *Checkpoint7549
	AggregationBits []byte
	CommitteeIndex  uint64
}

// LegacyAttestation represents the pre-EIP-7549 attestation format where
// the committee index is embedded inside the attestation data.
type LegacyAttestation struct {
	Slot            uint64
	CommitteeIndex  uint64
	Data            []byte
	AggregationBits []byte
	Signature       []byte
}

// dataKey produces a comparable key for the attestation's core data (slot,
// beacon block root, source, target). Two attestations with the same key
// can be aggregated across committees.
func dataKey(att *IndexedAttestation) [128]byte {
	var key [128]byte
	key[0] = byte(att.Slot)
	key[1] = byte(att.Slot >> 8)
	key[2] = byte(att.Slot >> 16)
	key[3] = byte(att.Slot >> 24)
	key[4] = byte(att.Slot >> 32)
	key[5] = byte(att.Slot >> 40)
	key[6] = byte(att.Slot >> 48)
	key[7] = byte(att.Slot >> 56)
	copy(key[8:40], att.BeaconBlockRoot[:])
	if att.Source != nil {
		key[40] = byte(att.Source.Epoch)
		key[41] = byte(att.Source.Epoch >> 8)
		key[42] = byte(att.Source.Epoch >> 16)
		key[43] = byte(att.Source.Epoch >> 24)
		key[44] = byte(att.Source.Epoch >> 32)
		key[45] = byte(att.Source.Epoch >> 40)
		key[46] = byte(att.Source.Epoch >> 48)
		key[47] = byte(att.Source.Epoch >> 56)
		copy(key[48:80], att.Source.Root[:])
	}
	if att.Target != nil {
		key[80] = byte(att.Target.Epoch)
		key[81] = byte(att.Target.Epoch >> 8)
		key[82] = byte(att.Target.Epoch >> 16)
		key[83] = byte(att.Target.Epoch >> 24)
		key[84] = byte(att.Target.Epoch >> 32)
		key[85] = byte(att.Target.Epoch >> 40)
		key[86] = byte(att.Target.Epoch >> 48)
		key[87] = byte(att.Target.Epoch >> 56)
		copy(key[88:120], att.Target.Root[:])
	}
	return key
}

// ConvertAttestation converts a LegacyAttestation (with committee index inside
// data) to an IndexedAttestation (with committee index as a top-level field).
func ConvertAttestation(legacy *LegacyAttestation) *IndexedAttestation {
	if legacy == nil {
		return nil
	}
	aggBits := make([]byte, len(legacy.AggregationBits))
	copy(aggBits, legacy.AggregationBits)

	return &IndexedAttestation{
		Slot:            legacy.Slot,
		BeaconBlockRoot: types.Hash{}, // caller must populate from Data
		Source:          &Checkpoint7549{},
		Target:          &Checkpoint7549{},
		AggregationBits: aggBits,
		CommitteeIndex:  legacy.CommitteeIndex,
	}
}

// ValidateIndexedAttestation validates an IndexedAttestation against basic
// structural and logical rules.
func ValidateIndexedAttestation(att *IndexedAttestation, validatorCount uint64) error {
	if att == nil {
		return ErrIndexedAttNil
	}
	if len(att.AggregationBits) == 0 {
		return ErrIndexedAttEmptyBits
	}
	if att.Source == nil {
		return ErrIndexedAttSourceNil
	}
	if att.Target == nil {
		return ErrIndexedAttTargetNil
	}
	if att.Source.Epoch > att.Target.Epoch {
		return ErrIndexedAttSourceAfter
	}
	if att.CommitteeIndex >= MaxCommitteesPerSlot {
		return ErrIndexedAttCommitteeRange
	}
	// Check that aggregation bits do not reference validators beyond the count.
	if validatorCount > 0 {
		maxBit := uint64(len(att.AggregationBits)) * 8
		if maxBit > validatorCount {
			// Check no bits are set beyond validatorCount.
			for i := validatorCount; i < maxBit; i++ {
				byteIdx := i / 8
				bitIdx := i % 8
				if att.AggregationBits[byteIdx]&(1<<bitIdx) != 0 {
					return fmt.Errorf("%w: bit %d set but only %d validators",
						ErrIndexedAttValidatorRange, i, validatorCount)
				}
			}
		}
	}
	return nil
}

// IsAggregatable checks if two indexed attestations can be aggregated.
// They must share the same slot, beacon block root, source, and target.
func IsAggregatable(a, b *IndexedAttestation) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Slot != b.Slot {
		return false
	}
	if a.BeaconBlockRoot != b.BeaconBlockRoot {
		return false
	}
	if a.Source == nil || b.Source == nil {
		return false
	}
	if a.Source.Epoch != b.Source.Epoch || a.Source.Root != b.Source.Root {
		return false
	}
	if a.Target == nil || b.Target == nil {
		return false
	}
	if a.Target.Epoch != b.Target.Epoch || a.Target.Root != b.Target.Root {
		return false
	}
	return true
}

// AggregateIndexedAttestations aggregates multiple indexed attestations that
// share the same data (slot, beacon block root, source, target) across
// different committees. Returns an error if attestations are not compatible.
func AggregateIndexedAttestations(atts []*IndexedAttestation) (*IndexedAttestation, error) {
	if len(atts) == 0 {
		return nil, errors.New("eip7549: no attestations to aggregate")
	}
	if len(atts) == 1 {
		return atts[0], nil
	}

	// Verify all attestations are aggregatable with the first.
	for i := 1; i < len(atts); i++ {
		if !IsAggregatable(atts[0], atts[i]) {
			return nil, ErrIndexedAttNotAggregatable
		}
	}

	// Merge aggregation bits via OR across all attestations.
	maxLen := 0
	for _, att := range atts {
		if len(att.AggregationBits) > maxLen {
			maxLen = len(att.AggregationBits)
		}
	}

	merged := make([]byte, maxLen)
	for _, att := range atts {
		for i, b := range att.AggregationBits {
			merged[i] |= b
		}
	}

	// Use the first attestation's committee index. In a cross-committee
	// aggregate the committee index field is less meaningful; set to 0.
	return &IndexedAttestation{
		Slot:            atts[0].Slot,
		BeaconBlockRoot: atts[0].BeaconBlockRoot,
		Source: &Checkpoint7549{
			Epoch: atts[0].Source.Epoch,
			Root:  atts[0].Source.Root,
		},
		Target: &Checkpoint7549{
			Epoch: atts[0].Target.Epoch,
			Root:  atts[0].Target.Root,
		},
		AggregationBits: merged,
		CommitteeIndex:  0,
	}, nil
}

// AttestationPool7549 manages a pool of indexed attestations for aggregation.
// Thread-safe.
type AttestationPool7549 struct {
	mu   sync.RWMutex
	atts []*IndexedAttestation
	// seen tracks attestations by slot+committee to detect duplicates.
	seen map[uint64]map[uint64]bool // slot -> committee -> exists
}

// NewAttestationPool7549 creates a new empty attestation pool.
func NewAttestationPool7549() *AttestationPool7549 {
	return &AttestationPool7549{
		atts: make([]*IndexedAttestation, 0),
		seen: make(map[uint64]map[uint64]bool),
	}
}

// Add inserts an indexed attestation into the pool.
func (p *AttestationPool7549) Add(att *IndexedAttestation) error {
	if att == nil {
		return ErrIndexedAttNil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check for duplicates (same slot + committee).
	if comms, ok := p.seen[att.Slot]; ok {
		if comms[att.CommitteeIndex] {
			return ErrIndexedAttPoolDuplicate
		}
	}

	p.atts = append(p.atts, att)
	if _, ok := p.seen[att.Slot]; !ok {
		p.seen[att.Slot] = make(map[uint64]bool)
	}
	p.seen[att.Slot][att.CommitteeIndex] = true
	return nil
}

// GetBest returns all indexed attestations for the given slot.
func (p *AttestationPool7549) GetBest(slot uint64) []*IndexedAttestation {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []*IndexedAttestation
	for _, att := range p.atts {
		if att.Slot == slot {
			result = append(result, att)
		}
	}
	return result
}

// AggregateAll groups all attestations in the pool by their data key and
// aggregates each group. Returns one aggregated attestation per unique data key.
func (p *AttestationPool7549) AggregateAll() []*IndexedAttestation {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Group by data key (slot + beaconBlockRoot + source + target).
	groups := make(map[[128]byte][]*IndexedAttestation)
	for _, att := range p.atts {
		key := dataKey(att)
		groups[key] = append(groups[key], att)
	}

	var result []*IndexedAttestation
	for _, group := range groups {
		agg, err := AggregateIndexedAttestations(group)
		if err != nil {
			// If aggregation fails, include the first attestation as-is.
			result = append(result, group[0])
			continue
		}
		result = append(result, agg)
	}
	return result
}
