package consensus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// SSF vote aggregation, committee assignment, quorum tracking, slot timeline,
// and finality decision logic for single-slot finality within 6-second slots.

// SSF vote aggregator errors.
var (
	ErrAggNilVote          = errors.New("ssf-agg: nil vote")
	ErrAggSlotFinalized    = errors.New("ssf-agg: slot already finalized")
	ErrAggDuplicateVote    = errors.New("ssf-agg: duplicate vote from validator")
	ErrAggUnknownValidator = errors.New("ssf-agg: unknown validator")
	ErrAggZeroTotalStake   = errors.New("ssf-agg: total stake is zero")
	ErrAggInvalidPhase     = errors.New("ssf-agg: invalid slot phase for this operation")
	ErrAggNoQuorum         = errors.New("ssf-agg: quorum not reached")
)

// SlotPhaseKind represents a phase within a 6-second SSF slot.
type SlotPhaseKind uint8

const (
	// PhaseSlotPropose is the initial 2-second proposal window.
	PhaseSlotPropose SlotPhaseKind = iota
	// PhaseSlotAttest is the 2-second attestation window.
	PhaseSlotAttest
	// PhaseSlotFinalize is the final 2-second finalization window.
	PhaseSlotFinalize
)

// String returns a human-readable name for the slot phase.
func (p SlotPhaseKind) String() string {
	switch p {
	case PhaseSlotPropose:
		return "propose"
	case PhaseSlotAttest:
		return "attest"
	case PhaseSlotFinalize:
		return "finalize"
	default:
		return "unknown"
	}
}

// SlotTimeline represents the 6-second slot divided into 3 phases of 2 seconds.
type SlotTimeline struct {
	SlotNumber       uint64
	SlotDurationMs   uint64 // total slot duration in milliseconds
	PhaseDurationMs  uint64 // each phase duration in milliseconds
	CurrentPhase     SlotPhaseKind
	PhaseStartTimeMs uint64 // when the current phase started (epoch ms)
}

// DefaultSlotTimeline creates a timeline for a 6-second slot.
func DefaultSlotTimeline(slot uint64) *SlotTimeline {
	return &SlotTimeline{
		SlotNumber:      slot,
		SlotDurationMs:  6000,
		PhaseDurationMs: 2000,
		CurrentPhase:    PhaseSlotPropose,
	}
}

// AdvancePhase moves the timeline to the next phase. Returns false if
// already at the finalize phase.
func (st *SlotTimeline) AdvancePhase() bool {
	switch st.CurrentPhase {
	case PhaseSlotPropose:
		st.CurrentPhase = PhaseSlotAttest
		st.PhaseStartTimeMs += st.PhaseDurationMs
		return true
	case PhaseSlotAttest:
		st.CurrentPhase = PhaseSlotFinalize
		st.PhaseStartTimeMs += st.PhaseDurationMs
		return true
	default:
		return false
	}
}

// PhaseAtOffset returns which phase corresponds to the given millisecond offset
// from the start of the slot.
func (st *SlotTimeline) PhaseAtOffset(offsetMs uint64) SlotPhaseKind {
	if st.PhaseDurationMs == 0 {
		return PhaseSlotPropose
	}
	phaseIdx := offsetMs / st.PhaseDurationMs
	if phaseIdx >= 3 {
		return PhaseSlotFinalize
	}
	return SlotPhaseKind(phaseIdx)
}

// SSFCommitteeSlot assigns validators to an SSF committee for a single slot.
type SSFCommitteeSlot struct {
	Slot       uint64
	Epoch      uint64
	Members    []uint64 // validator indices
	TotalStake uint64
}

// AssignSSFCommittee creates a committee for the given slot from active
// validator indices. In production, a RANDAO-based shuffle is used; here
// we deterministically assign by modular rotation per epoch.
func AssignSSFCommittee(slot, epoch uint64, validators []uint64, stakes map[uint64]uint64) *SSFCommitteeSlot {
	if len(validators) == 0 {
		return &SSFCommitteeSlot{Slot: slot, Epoch: epoch}
	}

	// Rotate by slot offset within epoch for simple determinism.
	n := len(validators)
	offset := int(slot % uint64(n))
	members := make([]uint64, n)
	for i := 0; i < n; i++ {
		members[i] = validators[(i+offset)%n]
	}

	var totalStake uint64
	for _, idx := range members {
		totalStake += stakes[idx]
	}

	return &SSFCommitteeSlot{
		Slot:       slot,
		Epoch:      epoch,
		Members:    members,
		TotalStake: totalStake,
	}
}

// AggregatedVote is a single validator's vote collected by the aggregator.
type AggregatedVote struct {
	ValidatorIndex uint64
	Slot           uint64
	BlockRoot      types.Hash
	Stake          uint64
	Signature      [96]byte
}

// FinalityDecision records the finality outcome for a slot.
type FinalityDecision struct {
	Slot           uint64
	BlockRoot      types.Hash
	IsFinalized    bool
	VoteCount      int
	TotalVoteStake uint64
	RequiredStake  uint64
	Participation  float64 // 0.0 to 100.0
}

// QuorumTracker tracks real-time quorum progress within a slot.
type QuorumTracker struct {
	Slot           uint64
	TotalStake     uint64
	RequiredStake  uint64
	AccruedStake   uint64
	VoteCount      int
	StakeByRoot    map[types.Hash]uint64
	LeadingRoot    types.Hash
	LeadingStake   uint64
	QuorumReached  bool
}

// NewQuorumTracker creates a quorum tracker for the given slot.
func NewQuorumTracker(slot, totalStake uint64, thresholdNum, thresholdDen uint64) *QuorumTracker {
	required := uint64(0)
	if thresholdDen > 0 {
		required = (totalStake*thresholdNum + thresholdDen - 1) / thresholdDen
	}
	return &QuorumTracker{
		Slot:          slot,
		TotalStake:    totalStake,
		RequiredStake: required,
		StakeByRoot:   make(map[types.Hash]uint64),
	}
}

// RecordVote updates quorum progress with a new vote. Returns true if this
// vote caused quorum to be reached.
func (qt *QuorumTracker) RecordVote(root types.Hash, stake uint64) bool {
	qt.AccruedStake += stake
	qt.VoteCount++
	qt.StakeByRoot[root] += stake

	rootStake := qt.StakeByRoot[root]
	if rootStake > qt.LeadingStake {
		qt.LeadingRoot = root
		qt.LeadingStake = rootStake
	}

	if !qt.QuorumReached && qt.LeadingStake >= qt.RequiredStake {
		qt.QuorumReached = true
		return true
	}
	return false
}

// Progress returns the percentage of required stake that has been reached
// for the leading root. Range: 0.0 to 100.0+.
func (qt *QuorumTracker) Progress() float64 {
	if qt.RequiredStake == 0 {
		return 0
	}
	return float64(qt.LeadingStake) / float64(qt.RequiredStake) * 100.0
}

// VoteAggregator collects and aggregates validator votes within a 6-second
// slot window. Thread-safe.
type VoteAggregator struct {
	mu sync.RWMutex

	// Configuration.
	thresholdNum uint64
	thresholdDen uint64
	totalStake   uint64

	// Validator stakes for weight lookup.
	validatorStakes map[uint64]uint64

	// Per-slot vote tracking.
	slotVotes    map[uint64]map[uint64]*AggregatedVote // slot -> validator -> vote
	slotQuorums  map[uint64]*QuorumTracker
	slotTimelines map[uint64]*SlotTimeline
	finalized    map[uint64]*FinalityDecision
}

// NewVoteAggregator creates a new vote aggregator.
func NewVoteAggregator(thresholdNum, thresholdDen, totalStake uint64) *VoteAggregator {
	return &VoteAggregator{
		thresholdNum:    thresholdNum,
		thresholdDen:    thresholdDen,
		totalStake:      totalStake,
		validatorStakes: make(map[uint64]uint64),
		slotVotes:       make(map[uint64]map[uint64]*AggregatedVote),
		slotQuorums:     make(map[uint64]*QuorumTracker),
		slotTimelines:   make(map[uint64]*SlotTimeline),
		finalized:       make(map[uint64]*FinalityDecision),
	}
}

// SetValidatorStakes sets the validator stake weights.
func (va *VoteAggregator) SetValidatorStakes(stakes map[uint64]uint64) {
	va.mu.Lock()
	defer va.mu.Unlock()
	va.validatorStakes = make(map[uint64]uint64, len(stakes))
	for k, v := range stakes {
		va.validatorStakes[k] = v
	}
}

// InitSlot initializes tracking for a new slot.
func (va *VoteAggregator) InitSlot(slot uint64) {
	va.mu.Lock()
	defer va.mu.Unlock()
	if _, ok := va.slotVotes[slot]; !ok {
		va.slotVotes[slot] = make(map[uint64]*AggregatedVote)
		va.slotQuorums[slot] = NewQuorumTracker(slot, va.totalStake, va.thresholdNum, va.thresholdDen)
		va.slotTimelines[slot] = DefaultSlotTimeline(slot)
	}
}

// SubmitVote adds a validator vote to the aggregator.
func (va *VoteAggregator) SubmitVote(vote *AggregatedVote) error {
	if vote == nil {
		return ErrAggNilVote
	}
	va.mu.Lock()
	defer va.mu.Unlock()

	// Check if slot is finalized.
	if _, ok := va.finalized[vote.Slot]; ok {
		return fmt.Errorf("%w: slot %d", ErrAggSlotFinalized, vote.Slot)
	}

	// Look up validator stake.
	stake, ok := va.validatorStakes[vote.ValidatorIndex]
	if !ok {
		return fmt.Errorf("%w: validator %d", ErrAggUnknownValidator, vote.ValidatorIndex)
	}

	// Ensure slot is initialized.
	if _, ok := va.slotVotes[vote.Slot]; !ok {
		va.slotVotes[vote.Slot] = make(map[uint64]*AggregatedVote)
		va.slotQuorums[vote.Slot] = NewQuorumTracker(vote.Slot, va.totalStake, va.thresholdNum, va.thresholdDen)
		va.slotTimelines[vote.Slot] = DefaultSlotTimeline(vote.Slot)
	}

	// Check for duplicate.
	if _, exists := va.slotVotes[vote.Slot][vote.ValidatorIndex]; exists {
		return fmt.Errorf("%w: validator %d slot %d", ErrAggDuplicateVote, vote.ValidatorIndex, vote.Slot)
	}

	// Store vote with correct stake weight.
	voteCopy := *vote
	voteCopy.Stake = stake
	va.slotVotes[vote.Slot][vote.ValidatorIndex] = &voteCopy

	// Update quorum tracker.
	va.slotQuorums[vote.Slot].RecordVote(vote.BlockRoot, stake)

	return nil
}

// CheckQuorum returns the quorum tracker state for a slot.
func (va *VoteAggregator) CheckQuorum(slot uint64) *QuorumTracker {
	va.mu.RLock()
	defer va.mu.RUnlock()
	if qt, ok := va.slotQuorums[slot]; ok {
		// Return a shallow copy.
		cp := *qt
		cp.StakeByRoot = make(map[types.Hash]uint64, len(qt.StakeByRoot))
		for k, v := range qt.StakeByRoot {
			cp.StakeByRoot[k] = v
		}
		return &cp
	}
	return NewQuorumTracker(slot, va.totalStake, va.thresholdNum, va.thresholdDen)
}

// DecideFinality checks if a slot has reached finality and produces a decision.
func (va *VoteAggregator) DecideFinality(slot uint64) (*FinalityDecision, error) {
	va.mu.Lock()
	defer va.mu.Unlock()

	if va.totalStake == 0 {
		return nil, ErrAggZeroTotalStake
	}

	// Already finalized?
	if fd, ok := va.finalized[slot]; ok {
		return fd, nil
	}

	qt, ok := va.slotQuorums[slot]
	if !ok {
		required := (va.totalStake*va.thresholdNum + va.thresholdDen - 1) / va.thresholdDen
		return &FinalityDecision{
			Slot:          slot,
			RequiredStake: required,
		}, nil
	}

	participation := float64(0)
	if va.totalStake > 0 {
		participation = float64(qt.AccruedStake) / float64(va.totalStake) * 100.0
	}

	decision := &FinalityDecision{
		Slot:           slot,
		BlockRoot:      qt.LeadingRoot,
		IsFinalized:    qt.QuorumReached,
		VoteCount:      qt.VoteCount,
		TotalVoteStake: qt.AccruedStake,
		RequiredStake:  qt.RequiredStake,
		Participation:  participation,
	}

	if decision.IsFinalized {
		va.finalized[slot] = decision
	}

	return decision, nil
}

// GetTimeline returns the slot timeline for the given slot.
func (va *VoteAggregator) GetTimeline(slot uint64) *SlotTimeline {
	va.mu.RLock()
	defer va.mu.RUnlock()
	if tl, ok := va.slotTimelines[slot]; ok {
		cp := *tl
		return &cp
	}
	return DefaultSlotTimeline(slot)
}

// AdvanceSlotPhase advances the slot timeline to the next phase.
func (va *VoteAggregator) AdvanceSlotPhase(slot uint64) bool {
	va.mu.Lock()
	defer va.mu.Unlock()
	if tl, ok := va.slotTimelines[slot]; ok {
		return tl.AdvancePhase()
	}
	return false
}

// VoteCount returns the number of votes for a slot.
func (va *VoteAggregator) VoteCount(slot uint64) int {
	va.mu.RLock()
	defer va.mu.RUnlock()
	if votes, ok := va.slotVotes[slot]; ok {
		return len(votes)
	}
	return 0
}

// IsSlotFinalized returns true if the slot has been finalized.
func (va *VoteAggregator) IsSlotFinalized(slot uint64) bool {
	va.mu.RLock()
	defer va.mu.RUnlock()
	_, ok := va.finalized[slot]
	return ok
}

// PurgeSlot removes all data for a slot.
func (va *VoteAggregator) PurgeSlot(slot uint64) {
	va.mu.Lock()
	defer va.mu.Unlock()
	delete(va.slotVotes, slot)
	delete(va.slotQuorums, slot)
	delete(va.slotTimelines, slot)
}
