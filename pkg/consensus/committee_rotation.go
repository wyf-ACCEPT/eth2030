// committee_rotation.go implements a committee rotation manager for SSF
// (single-slot finality). Manages validator committee assignments across
// epochs, uses deterministic swap-or-not shuffling from the Eth2 spec,
// supports a 128K attester cap, tracks committee indices, and handles
// committee assignment queries by slot and epoch.
//
// Complements committee_assignment.go and committee_selection.go by
// providing an epoch-aware rotation manager that pre-computes and caches
// full epoch committee maps with support for the SSF 4-slot epoch model.
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
)

// Committee rotation constants.
const (
	// CRShuffleRounds is the number of swap-or-not rounds (per spec).
	CRShuffleRounds = 90

	// CRMaxAttesters is the 128K attester cap from the roadmap.
	CRMaxAttesters = 128_000

	// CRTargetCommitteeSize is the ideal committee size.
	CRTargetCommitteeSize = 128

	// CRMaxCommitteesPerSlot limits committees per slot.
	CRMaxCommitteesPerSlot = 64

	// CRDefaultSlotsPerEpoch is the standard slots per epoch.
	CRDefaultSlotsPerEpoch uint64 = 32

	// CRSSFSlotsPerEpoch is the SSF 4-slot epoch model.
	CRSSFSlotsPerEpoch uint64 = 4

	// CRDomainAttester is the domain separator for attester seed derivation.
	CRDomainAttester uint32 = 0x01000000
)

// Committee rotation errors.
var (
	ErrCRNoValidators     = errors.New("committee_rotation: no active validators")
	ErrCRInvalidSlot      = errors.New("committee_rotation: slot not in epoch")
	ErrCRInvalidCommittee = errors.New("committee_rotation: committee index out of range")
	ErrCRCapExceeded      = errors.New("committee_rotation: attester cap exceeded")
	ErrCREpochNotComputed = errors.New("committee_rotation: epoch not yet computed")
	ErrCRZeroSlotsPerEp   = errors.New("committee_rotation: zero slots per epoch")
)

// CRCommitteeAssignment represents a single committee assignment for a
// validator at a specific slot and committee index.
type CRCommitteeAssignment struct {
	ValidatorIndex ValidatorIndex
	Slot           Slot
	CommitteeIndex uint64
	CommitteeSize  int
	Position       int // position within the committee
}

// CREpochCommittees holds all committee assignments for one epoch.
type CREpochCommittees struct {
	Epoch            Epoch
	SlotsPerEpoch    uint64
	CommitteesPerSlot uint64
	TotalValidators  int

	// committees is indexed as [slotOffset][committeeIdx] -> members.
	committees [][][]ValidatorIndex

	// validatorMap maps validator index -> assignment info.
	validatorMap map[ValidatorIndex]*CRCommitteeAssignment
}

// GetCommittee returns the committee members for the given slot offset
// and committee index within this epoch.
func (ec *CREpochCommittees) GetCommittee(slotOffset uint64, committeeIdx uint64) ([]ValidatorIndex, error) {
	if slotOffset >= ec.SlotsPerEpoch {
		return nil, ErrCRInvalidSlot
	}
	if committeeIdx >= ec.CommitteesPerSlot {
		return nil, ErrCRInvalidCommittee
	}
	members := ec.committees[slotOffset][committeeIdx]
	out := make([]ValidatorIndex, len(members))
	copy(out, members)
	return out, nil
}

// GetAssignment returns the committee assignment for a specific validator.
func (ec *CREpochCommittees) GetAssignment(idx ValidatorIndex) (*CRCommitteeAssignment, bool) {
	a, ok := ec.validatorMap[idx]
	if !ok {
		return nil, false
	}
	cp := *a
	return &cp, true
}

// CommitteeRotationConfig configures the rotation manager.
type CommitteeRotationConfig struct {
	SlotsPerEpoch        uint64
	TargetCommitteeSize  uint64
	MaxCommitteesPerSlot uint64
	MaxAttesters         int
	EnableSSFMode        bool // when true, uses 4-slot epochs
}

// DefaultCommitteeRotationConfig returns mainnet-compatible defaults.
func DefaultCommitteeRotationConfig() CommitteeRotationConfig {
	return CommitteeRotationConfig{
		SlotsPerEpoch:        CRDefaultSlotsPerEpoch,
		TargetCommitteeSize:  CRTargetCommitteeSize,
		MaxCommitteesPerSlot: CRMaxCommitteesPerSlot,
		MaxAttesters:         CRMaxAttesters,
		EnableSSFMode:        false,
	}
}

// SSFCommitteeRotationConfig returns config for SSF 4-slot epoch mode.
func SSFCommitteeRotationConfig() CommitteeRotationConfig {
	return CommitteeRotationConfig{
		SlotsPerEpoch:        CRSSFSlotsPerEpoch,
		TargetCommitteeSize:  CRTargetCommitteeSize,
		MaxCommitteesPerSlot: CRMaxCommitteesPerSlot,
		MaxAttesters:         CRMaxAttesters,
		EnableSSFMode:        true,
	}
}

// CommitteeRotationManager manages committee assignments across epochs.
// Thread-safe. Caches computed epoch committees for fast lookups.
type CommitteeRotationManager struct {
	mu     sync.RWMutex
	config CommitteeRotationConfig

	// epochCache holds precomputed committees per epoch.
	epochCache map[Epoch]*CREpochCommittees

	// lastComputedEpoch tracks the most recent epoch we've computed.
	lastComputedEpoch Epoch
	hasComputed       bool
}

// NewCommitteeRotationManager creates a new rotation manager.
func NewCommitteeRotationManager(cfg CommitteeRotationConfig) *CommitteeRotationManager {
	if cfg.SlotsPerEpoch == 0 {
		cfg.SlotsPerEpoch = CRDefaultSlotsPerEpoch
	}
	if cfg.TargetCommitteeSize == 0 {
		cfg.TargetCommitteeSize = CRTargetCommitteeSize
	}
	if cfg.MaxCommitteesPerSlot == 0 {
		cfg.MaxCommitteesPerSlot = CRMaxCommitteesPerSlot
	}
	if cfg.MaxAttesters == 0 {
		cfg.MaxAttesters = CRMaxAttesters
	}
	if cfg.EnableSSFMode {
		cfg.SlotsPerEpoch = CRSSFSlotsPerEpoch
	}
	return &CommitteeRotationManager{
		config:     cfg,
		epochCache: make(map[Epoch]*CREpochCommittees),
	}
}

// EffectiveSlotsPerEpoch returns the configured slots per epoch.
func (m *CommitteeRotationManager) EffectiveSlotsPerEpoch() uint64 {
	return m.config.SlotsPerEpoch
}

// ComputeEpochCommittees computes all committee assignments for the given
// epoch using the provided active validator indices and a 32-byte seed
// (typically derived from RANDAO). Enforces the 128K attester cap.
func (m *CommitteeRotationManager) ComputeEpochCommittees(
	epoch Epoch,
	activeIndices []ValidatorIndex,
	seed [32]byte,
) (*CREpochCommittees, error) {
	if len(activeIndices) == 0 {
		return nil, ErrCRNoValidators
	}

	// Enforce 128K attester cap.
	capped := activeIndices
	if len(capped) > m.config.MaxAttesters {
		capped = capped[:m.config.MaxAttesters]
	}

	// Check cache.
	m.mu.RLock()
	if cached, ok := m.epochCache[epoch]; ok {
		m.mu.RUnlock()
		return cached, nil
	}
	m.mu.RUnlock()

	spe := m.config.SlotsPerEpoch
	committeesPerSlot := crComputeCommitteeCount(
		uint64(len(capped)), spe,
		m.config.TargetCommitteeSize, m.config.MaxCommitteesPerSlot,
	)
	totalCommittees := spe * committeesPerSlot
	count := uint64(len(capped))

	// Pre-compute shuffled list for the full epoch.
	shuffled, err := crShuffleList(capped, seed)
	if err != nil {
		return nil, err
	}

	// Build the committee structure.
	committees := make([][][]ValidatorIndex, spe)
	validatorMap := make(map[ValidatorIndex]*CRCommitteeAssignment, len(capped))
	startSlot := Slot(uint64(epoch) * spe)

	for s := uint64(0); s < spe; s++ {
		slot := Slot(uint64(startSlot) + s)
		committees[s] = make([][]ValidatorIndex, committeesPerSlot)

		for c := uint64(0); c < committeesPerSlot; c++ {
			globalIdx := s*committeesPerSlot + c
			start := count * globalIdx / totalCommittees
			end := count * (globalIdx + 1) / totalCommittees

			members := make([]ValidatorIndex, 0, end-start)
			for i := start; i < end; i++ {
				members = append(members, shuffled[i])
			}
			committees[s][c] = members

			// Record assignments for each validator.
			for pos, valIdx := range members {
				validatorMap[valIdx] = &CRCommitteeAssignment{
					ValidatorIndex: valIdx,
					Slot:           slot,
					CommitteeIndex: c,
					CommitteeSize:  len(members),
					Position:       pos,
				}
			}
		}
	}

	result := &CREpochCommittees{
		Epoch:             epoch,
		SlotsPerEpoch:     spe,
		CommitteesPerSlot: committeesPerSlot,
		TotalValidators:   len(capped),
		committees:        committees,
		validatorMap:      validatorMap,
	}

	m.mu.Lock()
	m.epochCache[epoch] = result
	m.lastComputedEpoch = epoch
	m.hasComputed = true
	m.mu.Unlock()

	return result, nil
}

// GetCommitteeBySlot returns the committee members for an absolute slot
// and committee index. The epoch must have been previously computed.
func (m *CommitteeRotationManager) GetCommitteeBySlot(
	slot Slot, committeeIdx uint64,
) ([]ValidatorIndex, error) {
	spe := m.config.SlotsPerEpoch
	epoch := Epoch(uint64(slot) / spe)

	m.mu.RLock()
	ec, ok := m.epochCache[epoch]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrCREpochNotComputed
	}

	slotOffset := uint64(slot) % spe
	return ec.GetCommittee(slotOffset, committeeIdx)
}

// GetValidatorAssignment returns the assignment for a specific validator
// in the given epoch. The epoch must have been previously computed.
func (m *CommitteeRotationManager) GetValidatorAssignment(
	epoch Epoch, validatorIdx ValidatorIndex,
) (*CRCommitteeAssignment, error) {
	m.mu.RLock()
	ec, ok := m.epochCache[epoch]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrCREpochNotComputed
	}

	a, found := ec.GetAssignment(validatorIdx)
	if !found {
		return nil, ErrCRNoValidators
	}
	return a, nil
}

// RotateEpoch computes committees for the next epoch after the last
// computed epoch. Returns the new epoch committees.
func (m *CommitteeRotationManager) RotateEpoch(
	activeIndices []ValidatorIndex,
	seed [32]byte,
) (*CREpochCommittees, error) {
	m.mu.RLock()
	nextEpoch := Epoch(0)
	if m.hasComputed {
		nextEpoch = m.lastComputedEpoch + 1
	}
	m.mu.RUnlock()

	return m.ComputeEpochCommittees(nextEpoch, activeIndices, seed)
}

// PruneBeforeEpoch removes cached epochs older than the given epoch.
// Returns the number of pruned entries.
func (m *CommitteeRotationManager) PruneBeforeEpoch(beforeEpoch Epoch) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	pruned := 0
	for ep := range m.epochCache {
		if ep < beforeEpoch {
			delete(m.epochCache, ep)
			pruned++
		}
	}
	return pruned
}

// CachedEpochCount returns the number of cached epoch committee sets.
func (m *CommitteeRotationManager) CachedEpochCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.epochCache)
}

// LastComputedEpoch returns the most recently computed epoch. Returns 0
// and false if no epochs have been computed.
func (m *CommitteeRotationManager) LastComputedEpoch() (Epoch, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastComputedEpoch, m.hasComputed
}

// ComputeEpochSeed derives a deterministic seed for committee shuffling
// from the epoch number and a 32-byte RANDAO mix.
func ComputeEpochSeed(epoch Epoch, randaoMix [32]byte) [32]byte {
	var buf [44]byte
	binary.LittleEndian.PutUint32(buf[:4], CRDomainAttester)
	binary.LittleEndian.PutUint64(buf[4:12], uint64(epoch))
	copy(buf[12:44], randaoMix[:])
	return sha256.Sum256(buf[:])
}

// crComputeCommitteeCount returns the number of committees per slot.
func crComputeCommitteeCount(activeCount, slotsPerEpoch, targetSize, maxPerSlot uint64) uint64 {
	if slotsPerEpoch == 0 || targetSize == 0 {
		return 1
	}
	count := activeCount / slotsPerEpoch / targetSize
	if count == 0 {
		count = 1
	}
	if count > maxPerSlot {
		count = maxPerSlot
	}
	return count
}

// crShuffleList applies the swap-or-not shuffle to produce a complete
// permutation of the input indices. Uses the per-index SwapOrNotShuffle
// from the Eth2 spec (same algorithm as committee_assignment.go).
func crShuffleList(indices []ValidatorIndex, seed [32]byte) ([]ValidatorIndex, error) {
	n := uint64(len(indices))
	if n == 0 {
		return nil, ErrCRNoValidators
	}

	result := make([]ValidatorIndex, n)
	for i := uint64(0); i < n; i++ {
		shuffled := crComputeShuffledIndex(i, n, seed)
		result[i] = indices[shuffled]
	}
	return result, nil
}

// crComputeShuffledIndex computes the shuffled position for a given index
// using the swap-or-not network with CRShuffleRounds rounds of SHA-256.
func crComputeShuffledIndex(index, indexCount uint64, seed [32]byte) uint64 {
	if indexCount <= 1 {
		return 0
	}

	cur := index
	for round := uint64(0); round < CRShuffleRounds; round++ {
		var pivotInput [33]byte
		copy(pivotInput[:32], seed[:])
		pivotInput[32] = byte(round)
		pivotHash := sha256.Sum256(pivotInput[:])
		pivot := binary.LittleEndian.Uint64(pivotHash[:8]) % indexCount

		flip := (pivot + indexCount - cur) % indexCount

		position := flip
		if cur > flip {
			position = cur
		}

		var srcInput [37]byte
		copy(srcInput[:32], seed[:])
		srcInput[32] = byte(round)
		binary.LittleEndian.PutUint32(srcInput[33:], uint32(position/256))
		source := sha256.Sum256(srcInput[:])

		byteIdx := (position % 256) / 8
		bitIdx := position % 8
		if (source[byteIdx]>>bitIdx)&1 != 0 {
			cur = flip
		}
	}
	return cur
}
