package consensus

import (
	"github.com/eth2030/eth2030/core/types"
)

// Endgame finality targets sub-slot finality (finality delay of 1 slot or less),
// the ultimate goal of the Ethereum consensus roadmap (M+ era, 2029+).
// Instead of waiting multiple epochs, blocks can be finalized within a single
// slot by collecting sufficient attestation weight across sub-slot intervals.

// EndgameConfig holds the configuration for endgame finality.
type EndgameConfig struct {
	TargetFinalityDelay uint64 // target finality delay in slots (goal: 1)
	MaxFinalityDelay    uint64 // max allowed finality delay before degrading
	SubSlotCount        uint64 // number of sub-slot voting intervals per slot
}

// DefaultEndgameConfig returns the default endgame finality configuration.
func DefaultEndgameConfig() *EndgameConfig {
	return &EndgameConfig{
		TargetFinalityDelay: 1,
		MaxFinalityDelay:    4,
		SubSlotCount:        3, // 3 sub-slots: propose, attest, aggregate
	}
}

// SubSlotAttestation represents an attestation received during a sub-slot interval.
type SubSlotAttestation struct {
	ValidatorIndex uint64
	Slot           Slot
	SubSlotIndex   uint64
	Weight         uint64     // effective balance weight of this attestation
	BlockRoot      types.Hash // the block root being attested to
}

// SlotFinalityState tracks the finality progress within a single slot.
type SlotFinalityState struct {
	Slot            Slot
	TotalWeight     uint64
	AttestingWeight uint64
	SubSlotWeights  []uint64 // attestation weight per sub-slot
	Finalized       bool
	FinalizedRoot   types.Hash
}

// EndgameFinalityTracker extends FinalityTracker for sub-slot finality.
type EndgameFinalityTracker struct {
	*FinalityTracker
	endgameConfig *EndgameConfig
	slotStates    map[Slot]*SlotFinalityState
}

// NewEndgameFinalityTracker creates an endgame finality tracker.
func NewEndgameFinalityTracker(cfg *ConsensusConfig, endgameCfg *EndgameConfig) *EndgameFinalityTracker {
	return &EndgameFinalityTracker{
		FinalityTracker: NewFinalityTracker(cfg),
		endgameConfig:   endgameCfg,
		slotStates:      make(map[Slot]*SlotFinalityState),
	}
}

// getOrCreateSlotState returns the SlotFinalityState for a slot, creating it
// if it doesn't exist.
func (eft *EndgameFinalityTracker) getOrCreateSlotState(slot Slot, totalWeight uint64) *SlotFinalityState {
	state, exists := eft.slotStates[slot]
	if !exists {
		state = &SlotFinalityState{
			Slot:           slot,
			TotalWeight:    totalWeight,
			SubSlotWeights: make([]uint64, eft.endgameConfig.SubSlotCount),
		}
		eft.slotStates[slot] = state
	}
	return state
}

// ProcessSlot processes attestations for a slot and attempts finality.
// Returns true if the slot was finalized.
func (eft *EndgameFinalityTracker) ProcessSlot(
	slot Slot,
	totalWeight uint64,
	attestations []SubSlotAttestation,
) bool {
	state := eft.getOrCreateSlotState(slot, totalWeight)
	if state.Finalized {
		return true
	}

	// Accumulate attestation weights.
	for _, att := range attestations {
		if att.Slot != slot {
			continue
		}
		if att.SubSlotIndex < eft.endgameConfig.SubSlotCount {
			state.SubSlotWeights[att.SubSlotIndex] += att.Weight
		}
		state.AttestingWeight += att.Weight
	}

	// Check if 2/3 supermajority is reached for the slot.
	if WeighJustification(state.TotalWeight, state.AttestingWeight) {
		state.Finalized = true
		// Use the block root from the first attestation as the finalized root.
		if len(attestations) > 0 {
			state.FinalizedRoot = attestations[0].BlockRoot
		}

		// Update the underlying finality tracker.
		epoch := SlotToEpoch(slot, eft.config.SlotsPerEpoch)
		eft.Finalize(epoch, state.FinalizedRoot)
		return true
	}
	return false
}

// IsSubSlotFinalized returns true if the given sub-slot has accumulated
// sufficient weight for its portion of the supermajority threshold.
func (eft *EndgameFinalityTracker) IsSubSlotFinalized(slot Slot, subSlotIndex uint64) bool {
	state, exists := eft.slotStates[slot]
	if !exists {
		return false
	}
	if subSlotIndex >= eft.endgameConfig.SubSlotCount {
		return false
	}
	// A sub-slot is "finalized" if it has collected its proportional share
	// of the 2/3 supermajority.
	subSlotThreshold := (state.TotalWeight * 2) / (3 * eft.endgameConfig.SubSlotCount)
	return state.SubSlotWeights[subSlotIndex] >= subSlotThreshold
}

// FinalityScore returns a confidence score (0.0-1.0) for a slot based on
// the fraction of attestation weight relative to the 2/3 supermajority.
func (eft *EndgameFinalityTracker) FinalityScore(slot Slot) float64 {
	state, exists := eft.slotStates[slot]
	if !exists {
		return 0.0
	}
	if state.TotalWeight == 0 {
		return 0.0
	}

	// Target is 2/3 of total weight.
	target := (state.TotalWeight * 2) / 3
	if target == 0 {
		return 0.0
	}

	score := float64(state.AttestingWeight) / float64(target)
	if score > 1.0 {
		score = 1.0
	}
	return score
}

// SubSlotVoting collects and processes a batch of attestations for a specific
// sub-slot within a slot.
func (eft *EndgameFinalityTracker) SubSlotVoting(
	slot Slot,
	subSlotIndex uint64,
	totalWeight uint64,
	attestations []SubSlotAttestation,
) bool {
	// Filter attestations for this specific sub-slot.
	filtered := make([]SubSlotAttestation, 0, len(attestations))
	for _, att := range attestations {
		if att.Slot == slot && att.SubSlotIndex == subSlotIndex {
			filtered = append(filtered, att)
		}
	}
	// Process the filtered attestations through the main path.
	return eft.ProcessSlot(slot, totalWeight, filtered)
}

// SlotFinalized returns whether a slot has been finalized.
func (eft *EndgameFinalityTracker) SlotFinalized(slot Slot) bool {
	state, exists := eft.slotStates[slot]
	if !exists {
		return false
	}
	return state.Finalized
}

// PruneSlots removes finality state for slots older than the given slot.
// Keeps memory usage bounded.
func (eft *EndgameFinalityTracker) PruneSlots(beforeSlot Slot) {
	for s := range eft.slotStates {
		if s < beforeSlot {
			delete(eft.slotStates, s)
		}
	}
}
