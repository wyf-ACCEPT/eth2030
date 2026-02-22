// committee_selection.go implements the FOCIL committee selection algorithm
// using beacon chain randomness for deterministic inclusion list builder
// assignment. It extends the base committee_tracker.go with advanced
// rotation, fallback selection, epoch-based reseeding, and look-ahead
// computation for pre-fetching committee assignments.
//
// Per EIP-7805, IL committee members are selected pseudo-randomly each slot.
// This file adds:
//   - Epoch-based RANDAO reseeding for forward-secure selection
//   - Rotation tracking across consecutive slots
//   - Fallback selection when primary members are unavailable
//   - Look-ahead computation for the next N slots
//   - Selection proof generation and verification
package focil

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"golang.org/x/crypto/sha3"
)

// Committee selection errors.
var (
	ErrSelectionSlotZero       = errors.New("committee-selection: slot must be > 0")
	ErrSelectionNoValidators   = errors.New("committee-selection: no active validators")
	ErrSelectionSeedZero       = errors.New("committee-selection: seed is all zeros")
	ErrSelectionLookAheadZero  = errors.New("committee-selection: look-ahead count must be > 0")
	ErrSelectionNotInCommittee = errors.New("committee-selection: validator not in committee")
	ErrSelectionProofInvalid   = errors.New("committee-selection: selection proof verification failed")
	ErrSelectionFallbackNone   = errors.New("committee-selection: no fallback validators available")
)

// SelectionProof is a proof that a validator was correctly selected for an
// IL committee at a given slot. It binds the validator index, slot, and
// selection seed together with a hash commitment.
type SelectionProof struct {
	// ValidatorIndex is the selected validator.
	ValidatorIndex uint64

	// Slot is the slot for which the validator was selected.
	Slot uint64

	// CommitteePosition is the validator's position in the committee.
	CommitteePosition int

	// Commitment is keccak256(seed || slot || validatorIndex || position).
	Commitment types.Hash
}

// RotationRecord tracks how frequently a validator is assigned to committees
// across a range of slots. Used for fairness monitoring.
type RotationRecord struct {
	// ValidatorIndex is the validator being tracked.
	ValidatorIndex uint64

	// AssignmentCount is how many times the validator was selected.
	AssignmentCount int

	// LastAssignedSlot is the most recent slot the validator was assigned.
	LastAssignedSlot uint64

	// Slots lists all slots where the validator was assigned.
	Slots []uint64
}

// CommitteeSelectionConfig configures the committee selection algorithm.
type CommitteeSelectionConfig struct {
	// CommitteeSize is the number of validators per IL committee.
	CommitteeSize int

	// SlotsPerEpoch is the number of slots per epoch. Default: 32.
	SlotsPerEpoch uint64

	// FallbackSize is the number of fallback validators to select when
	// primary members are unavailable. Default: 4.
	FallbackSize int

	// MaxLookAhead is the maximum number of future slots for look-ahead
	// committee computation. Default: 32.
	MaxLookAhead int
}

// DefaultCommitteeSelectionConfig returns sensible defaults.
func DefaultCommitteeSelectionConfig() CommitteeSelectionConfig {
	return CommitteeSelectionConfig{
		CommitteeSize: IL_COMMITTEE_SIZE,
		SlotsPerEpoch: SlotsPerEpoch,
		FallbackSize:  4,
		MaxLookAhead:  32,
	}
}

// CommitteeSelector performs deterministic IL committee selection using
// beacon randomness. Thread-safe.
type CommitteeSelector struct {
	mu     sync.RWMutex
	config CommitteeSelectionConfig

	// validators is the sorted list of active validator indices.
	validators []uint64

	// epochSeeds maps epoch number -> 32-byte RANDAO seed.
	epochSeeds map[uint64][32]byte

	// cache maps slot -> selected committee (sorted validator indices).
	cache map[uint64][]uint64
}

// NewCommitteeSelector creates a new committee selector.
func NewCommitteeSelector(config CommitteeSelectionConfig, validators []uint64) *CommitteeSelector {
	if config.CommitteeSize <= 0 {
		config.CommitteeSize = IL_COMMITTEE_SIZE
	}
	if config.SlotsPerEpoch == 0 {
		config.SlotsPerEpoch = SlotsPerEpoch
	}
	if config.FallbackSize <= 0 {
		config.FallbackSize = 4
	}
	if config.MaxLookAhead <= 0 {
		config.MaxLookAhead = 32
	}
	vals := make([]uint64, len(validators))
	copy(vals, validators)
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })

	return &CommitteeSelector{
		config:     config,
		validators: vals,
		epochSeeds: make(map[uint64][32]byte),
		cache:      make(map[uint64][]uint64),
	}
}

// SetEpochSeed registers the RANDAO seed for a given epoch. The seed must
// be non-zero. Subsequent committee selections for slots in this epoch will
// use this seed.
func (cs *CommitteeSelector) SetEpochSeed(epoch uint64, seed [32]byte) error {
	if seed == ([32]byte{}) {
		return ErrSelectionSeedZero
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.epochSeeds[epoch] = seed

	// Invalidate cache for slots in this epoch.
	startSlot := epoch * cs.config.SlotsPerEpoch
	endSlot := startSlot + cs.config.SlotsPerEpoch
	for s := startSlot; s < endSlot; s++ {
		delete(cs.cache, s)
	}
	return nil
}

// SelectCommittee deterministically selects the IL committee for a slot.
// It uses the epoch's RANDAO seed combined with the slot number to produce
// a unique committee. Results are cached.
func (cs *CommitteeSelector) SelectCommittee(slot uint64) ([]uint64, error) {
	if slot == 0 {
		return nil, ErrSelectionSlotZero
	}

	cs.mu.RLock()
	if cached, ok := cs.cache[slot]; ok {
		cs.mu.RUnlock()
		return cached, nil
	}
	cs.mu.RUnlock()

	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Double-check after acquiring write lock.
	if cached, ok := cs.cache[slot]; ok {
		return cached, nil
	}

	if len(cs.validators) == 0 {
		return nil, ErrSelectionNoValidators
	}

	epoch := slot / cs.config.SlotsPerEpoch
	seed := cs.getEpochSeedLocked(epoch)

	committee := cs.selectFromSeed(seed, slot)
	cs.cache[slot] = committee
	return committee, nil
}

// SelectFallback selects additional fallback validators for a slot. Fallback
// validators are selected from the validator set but not from the primary
// committee. They serve as replacements when primary members are unavailable.
func (cs *CommitteeSelector) SelectFallback(slot uint64) ([]uint64, error) {
	if slot == 0 {
		return nil, ErrSelectionSlotZero
	}

	committee, err := cs.SelectCommittee(slot)
	if err != nil {
		return nil, err
	}

	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if len(cs.validators) == 0 {
		return nil, ErrSelectionNoValidators
	}

	// Exclude primary committee members.
	committeeSet := make(map[uint64]bool, len(committee))
	for _, v := range committee {
		committeeSet[v] = true
	}

	available := make([]uint64, 0)
	for _, v := range cs.validators {
		if !committeeSet[v] {
			available = append(available, v)
		}
	}

	if len(available) == 0 {
		return nil, ErrSelectionFallbackNone
	}

	// Select fallback members using a different domain seed.
	epoch := slot / cs.config.SlotsPerEpoch
	seed := cs.getEpochSeedLocked(epoch)
	fallbackSeed := computeSelectionDomainSeed(seed, slot, 1) // domain=1 for fallback

	size := cs.config.FallbackSize
	if size > len(available) {
		size = len(available)
	}

	selected := selectIndicesFromSeed(fallbackSeed, available, size)
	sort.Slice(selected, func(i, j int) bool { return selected[i] < selected[j] })
	return selected, nil
}

// ComputeLookAhead computes committees for the next N slots starting from
// startSlot. Returns a map from slot to committee.
func (cs *CommitteeSelector) ComputeLookAhead(startSlot uint64, count int) (map[uint64][]uint64, error) {
	if startSlot == 0 {
		return nil, ErrSelectionSlotZero
	}
	if count <= 0 {
		return nil, ErrSelectionLookAheadZero
	}
	if count > cs.config.MaxLookAhead {
		count = cs.config.MaxLookAhead
	}

	result := make(map[uint64][]uint64, count)
	for i := 0; i < count; i++ {
		slot := startSlot + uint64(i)
		committee, err := cs.SelectCommittee(slot)
		if err != nil {
			return nil, fmt.Errorf("slot %d: %w", slot, err)
		}
		result[slot] = committee
	}
	return result, nil
}

// GenerateSelectionProof creates a proof that a validator was selected for the
// committee at a given slot. Returns an error if the validator is not in the
// committee.
func (cs *CommitteeSelector) GenerateSelectionProof(validatorIndex, slot uint64) (*SelectionProof, error) {
	committee, err := cs.SelectCommittee(slot)
	if err != nil {
		return nil, err
	}

	position := -1
	for i, v := range committee {
		if v == validatorIndex {
			position = i
			break
		}
	}
	if position < 0 {
		return nil, fmt.Errorf("%w: validator %d at slot %d",
			ErrSelectionNotInCommittee, validatorIndex, slot)
	}

	cs.mu.RLock()
	epoch := slot / cs.config.SlotsPerEpoch
	seed := cs.getEpochSeedLocked(epoch)
	cs.mu.RUnlock()

	commitment := computeSelectionCommitment(seed, slot, validatorIndex, position)

	return &SelectionProof{
		ValidatorIndex:    validatorIndex,
		Slot:              slot,
		CommitteePosition: position,
		Commitment:        commitment,
	}, nil
}

// VerifySelectionProof verifies a selection proof for the given epoch seed.
func VerifySelectionProof(proof *SelectionProof, seed [32]byte) bool {
	if proof == nil {
		return false
	}
	expected := computeSelectionCommitment(seed, proof.Slot, proof.ValidatorIndex, proof.CommitteePosition)
	return expected == proof.Commitment
}

// GetRotationRecords returns rotation statistics for all validators across
// the given slot range [startSlot, endSlot].
func (cs *CommitteeSelector) GetRotationRecords(startSlot, endSlot uint64) []RotationRecord {
	records := make(map[uint64]*RotationRecord)

	for slot := startSlot; slot <= endSlot; slot++ {
		committee, err := cs.SelectCommittee(slot)
		if err != nil {
			continue
		}
		for _, v := range committee {
			rec, ok := records[v]
			if !ok {
				rec = &RotationRecord{ValidatorIndex: v}
				records[v] = rec
			}
			rec.AssignmentCount++
			rec.LastAssignedSlot = slot
			rec.Slots = append(rec.Slots, slot)
		}
	}

	result := make([]RotationRecord, 0, len(records))
	for _, r := range records {
		result = append(result, *r)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ValidatorIndex < result[j].ValidatorIndex
	})
	return result
}

// ValidatorCount returns the number of active validators.
func (cs *CommitteeSelector) ValidatorCount() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.validators)
}

// CachedSlots returns the number of cached committee selections.
func (cs *CommitteeSelector) CachedSlots() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.cache)
}

// PruneCacheBefore removes cached entries for slots before the given slot.
func (cs *CommitteeSelector) PruneCacheBefore(slot uint64) int {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	pruned := 0
	for s := range cs.cache {
		if s < slot {
			delete(cs.cache, s)
			pruned++
		}
	}
	return pruned
}

// --- Internal helpers ---

// getEpochSeedLocked returns the seed for an epoch, deriving a default if
// none was set. Caller must hold at least a read lock.
func (cs *CommitteeSelector) getEpochSeedLocked(epoch uint64) [32]byte {
	if seed, ok := cs.epochSeeds[epoch]; ok {
		return seed
	}
	// Derive a default seed from the epoch number.
	h := sha3.NewLegacyKeccak256()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], epoch)
	h.Write([]byte("focil-committee-seed-v1"))
	h.Write(buf[:])
	var seed [32]byte
	h.Sum(seed[:0])
	return seed
}

// selectFromSeed selects CommitteeSize unique validators for a slot.
func (cs *CommitteeSelector) selectFromSeed(seed [32]byte, slot uint64) []uint64 {
	n := len(cs.validators)
	size := cs.config.CommitteeSize
	if size > n {
		size = n
	}

	domainSeed := computeSelectionDomainSeed(seed, slot, 0) // domain=0 for primary

	selected := selectIndicesFromSeed(domainSeed, cs.validators, size)
	sort.Slice(selected, func(i, j int) bool { return selected[i] < selected[j] })
	return selected
}

// computeSelectionDomainSeed derives a domain-separated seed.
// domainSeed = keccak256(baseSeed || slot || domain)
func computeSelectionDomainSeed(baseSeed [32]byte, slot uint64, domain uint64) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(baseSeed[:])
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], slot)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], domain)
	h.Write(buf[:])
	var result [32]byte
	h.Sum(result[:0])
	return result
}

// selectIndicesFromSeed selects `count` unique indices from the pool using
// a hash chain starting from seed.
func selectIndicesFromSeed(seed [32]byte, pool []uint64, count int) []uint64 {
	n := len(pool)
	if count > n {
		count = n
	}

	selected := make([]uint64, 0, count)
	used := make(map[int]bool, count)

	for counter := uint64(0); len(selected) < count; counter++ {
		h := sha3.NewLegacyKeccak256()
		h.Write(seed[:])
		var cBuf [8]byte
		binary.LittleEndian.PutUint64(cBuf[:], counter)
		h.Write(cBuf[:])
		digest := h.Sum(nil)

		idx := int(binary.LittleEndian.Uint64(digest[:8]) % uint64(n))
		if !used[idx] {
			used[idx] = true
			selected = append(selected, pool[idx])
		}
	}
	return selected
}

// computeSelectionCommitment computes the commitment for a selection proof.
func computeSelectionCommitment(seed [32]byte, slot, validatorIndex uint64, position int) types.Hash {
	h := sha3.NewLegacyKeccak256()
	h.Write(seed[:])
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], slot)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], validatorIndex)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(position))
	h.Write(buf[:])
	var result types.Hash
	h.Sum(result[:0])
	return result
}
