// attproc_attestation_processing.go implements attestation processing per the
// Ethereum beacon chain spec (Altair). Provides validation of attestation data,
// timeliness checks, participation flag updates, indexed attestation verification
// placeholders, and attesting indices extraction from committee bits.
//
// This complements attestation.go (EIP-7549 types and aggregation) and
// epoch_processor.go by providing the block-level attestation processing
// that updates participation flags on BsnBeaconState.
package consensus

import (
	"errors"
	"fmt"

	"github.com/eth2028/eth2028/core/types"
)

// Attestation processing constants.
const (
	// AttProcMinInclusionDelay is the minimum slots between attestation and inclusion (1 slot).
	AttProcMinInclusionDelay uint64 = 1

	// AttProcSlotsPerEpoch is the default slots per epoch.
	AttProcSlotsPerEpoch uint64 = 32

	// AttProcMaxAttestations is the maximum attestations per block.
	AttProcMaxAttestations = 128

	// AttProcParticipationFlagCount is the number of participation flags.
	AttProcParticipationFlagCount = 3
)

// Participation flag weights (Altair spec) used for reward computation.
var attProcFlagWeights = [AttProcParticipationFlagCount]uint64{
	BsnTimelySourceWeight, // source = 14
	BsnTimelyTargetWeight, // target = 26
	BsnTimelyHeadWeight,   // head = 14
}

// Attestation processing errors.
var (
	ErrAttProcNilState         = errors.New("attproc: nil beacon state")
	ErrAttProcNilAttestation   = errors.New("attproc: nil attestation")
	ErrAttProcFutureTarget     = errors.New("attproc: target epoch is in the future")
	ErrAttProcOldTarget        = errors.New("attproc: target epoch is too old")
	ErrAttProcTargetMismatch   = errors.New("attproc: target epoch does not match slot")
	ErrAttProcFutureSlot       = errors.New("attproc: attestation slot is in the future")
	ErrAttProcTooOld           = errors.New("attproc: attestation is too old for inclusion")
	ErrAttProcBadSource        = errors.New("attproc: source checkpoint does not match justified")
	ErrAttProcEmptyCommittee   = errors.New("attproc: empty committee bits")
	ErrAttProcBadCommittee     = errors.New("attproc: invalid committee index")
	ErrAttProcAggBitsMismatch  = errors.New("attproc: aggregation bits length mismatch")
	ErrAttProcNoNewFlags       = errors.New("attproc: attestation adds no new participation flags")
	ErrAttProcInvalidSignature = errors.New("attproc: invalid indexed attestation signature")
)

// AttProcAttestationData mirrors AttestationData but uses [32]byte roots
// for processing against BsnBeaconState.
type AttProcAttestationData struct {
	Slot            uint64
	CommitteeIndex  uint64
	BeaconBlockRoot [32]byte
	Source          CheckpointV2
	Target          CheckpointV2
}

// AttProcIndexedAttestation is an indexed attestation with explicit validator
// indices for verification.
type AttProcIndexedAttestation struct {
	AttestingIndices []uint64
	Data             AttProcAttestationData
	Signature        [96]byte
}

// AttProcAttestation is an attestation for processing against BsnBeaconState.
type AttProcAttestation struct {
	AggregationBits []byte
	Data            AttProcAttestationData
	CommitteeBits   []byte   // bitfield, which committees are included
	Signature       [96]byte
}

// AttProcValidateAttestationData validates attestation data against the beacon
// state per the spec:
//  1. Target epoch must be current or previous epoch
//  2. Target epoch must match attestation slot's epoch
//  3. Attestation slot must not be in the future
//  4. Attestation must not be too old (within SLOTS_PER_EPOCH)
//  5. Source checkpoint must match the justified checkpoint for the target epoch
func AttProcValidateAttestationData(
	state *BsnBeaconState,
	data *AttProcAttestationData,
) error {
	if state == nil {
		return ErrAttProcNilState
	}
	spe := state.SlotsPerEpoch
	if spe == 0 {
		spe = AttProcSlotsPerEpoch
	}
	currentEpoch := state.BsnGetCurrentEpoch()
	previousEpoch := state.BsnGetPreviousEpoch()

	// Target epoch must be current or previous.
	if data.Target.Epoch > currentEpoch {
		return ErrAttProcFutureTarget
	}
	if data.Target.Epoch < previousEpoch {
		return ErrAttProcOldTarget
	}

	// Target epoch must match the epoch of the attestation slot.
	attEpoch := Epoch(data.Slot / spe)
	if attEpoch != data.Target.Epoch {
		return ErrAttProcTargetMismatch
	}

	// Attestation slot must not be in the future.
	if data.Slot > state.Slot {
		return ErrAttProcFutureSlot
	}

	// Check inclusion timeliness: att.slot + MIN_INCLUSION_DELAY <= state.slot
	// and state.slot <= att.slot + SLOTS_PER_EPOCH.
	if data.Slot+AttProcMinInclusionDelay > state.Slot {
		return ErrAttProcFutureSlot
	}
	if state.Slot > data.Slot+spe {
		return ErrAttProcTooOld
	}

	// Source checkpoint validation: must match the justified checkpoint for
	// the target epoch.
	if data.Target.Epoch == currentEpoch {
		if data.Source.Epoch != state.CurrentJustifiedCheckpoint.Epoch ||
			data.Source.Root != state.CurrentJustifiedCheckpoint.Root {
			return ErrAttProcBadSource
		}
	} else {
		// Previous epoch attestation.
		if data.Source.Epoch != state.PreviousJustifiedCheckpoint.Epoch ||
			data.Source.Root != state.PreviousJustifiedCheckpoint.Root {
			return ErrAttProcBadSource
		}
	}

	return nil
}

// AttProcCheckTimeliness determines which participation flags are applicable
// based on the inclusion delay.
//
// Returns a [3]bool for [source, target, head]:
//   - Source is timely if included within sqrt(SLOTS_PER_EPOCH) slots (~5 for 32)
//   - Target is timely if included within SLOTS_PER_EPOCH
//   - Head is timely if included in the very next slot (delay == 1)
func AttProcCheckTimeliness(
	stateSlot, attSlot, slotsPerEpoch uint64,
) [AttProcParticipationFlagCount]bool {
	if slotsPerEpoch == 0 {
		slotsPerEpoch = AttProcSlotsPerEpoch
	}
	inclusionDelay := stateSlot - attSlot

	var flags [AttProcParticipationFlagCount]bool

	// Source: timely if delay <= integer_sqrt(SLOTS_PER_EPOCH).
	sqrtEpoch := attProcIntSqrt(slotsPerEpoch)
	if inclusionDelay <= sqrtEpoch {
		flags[BsnTimelySourceFlagIndex] = true
	}

	// Target: timely if delay <= SLOTS_PER_EPOCH.
	if inclusionDelay <= slotsPerEpoch {
		flags[BsnTimelyTargetFlagIndex] = true
	}

	// Head: timely if delay == MIN_INCLUSION_DELAY (1 slot).
	if inclusionDelay == AttProcMinInclusionDelay {
		flags[BsnTimelyHeadFlagIndex] = true
	}

	return flags
}

// attProcIntSqrt computes the integer square root of n.
func attProcIntSqrt(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	x := n
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}

// AttProcGetAttestingIndices extracts the validator indices that are
// attesting based on the committee bits and aggregation bits.
//
// For each committee indicated in committeeBits, it looks up the committee
// members from the state and uses the aggregation bits to determine which
// members participated.
func AttProcGetAttestingIndices(
	state *BsnBeaconState,
	data *AttProcAttestationData,
	aggregationBits []byte,
	committeeBits []byte,
) ([]uint64, error) {
	if state == nil {
		return nil, ErrAttProcNilState
	}
	if len(committeeBits) == 0 {
		return nil, ErrAttProcEmptyCommittee
	}

	// Extract committee indices from committee bits.
	committeeIndices := attProcGetCommitteeIndices(committeeBits)
	if len(committeeIndices) == 0 {
		return nil, ErrAttProcEmptyCommittee
	}

	var allIndices []uint64
	aggBitOffset := 0

	for _, commIdx := range committeeIndices {
		// Get committee members for this committee.
		committee, err := ShufGetBeaconCommittee(state, data.Slot, commIdx)
		if err != nil {
			return nil, fmt.Errorf("attproc: failed to get committee %d: %w", commIdx, err)
		}

		// Extract the members indicated by aggregation bits.
		for memberIdx, valIdx := range committee {
			bitPos := aggBitOffset + memberIdx
			bytePos := bitPos / 8
			bitOffset := uint(bitPos % 8)
			if bytePos < len(aggregationBits) && (aggregationBits[bytePos]>>bitOffset)&1 != 0 {
				allIndices = append(allIndices, valIdx)
			}
		}
		aggBitOffset += len(committee)
	}

	return allIndices, nil
}

// attProcGetCommitteeIndices extracts committee indices from a bitfield.
func attProcGetCommitteeIndices(bits []byte) []uint64 {
	var indices []uint64
	for byteIdx, b := range bits {
		for bitIdx := uint64(0); bitIdx < 8; bitIdx++ {
			if b&(1<<bitIdx) != 0 {
				indices = append(indices, uint64(byteIdx)*8+bitIdx)
			}
		}
	}
	return indices
}

// AttProcVerifyIndexedAttestation is a placeholder for BLS signature
// verification of an indexed attestation. In production, this would
// verify the aggregate BLS signature against all attesting validator
// pubkeys and the signing root of the attestation data.
func AttProcVerifyIndexedAttestation(
	_ *BsnBeaconState,
	att *AttProcIndexedAttestation,
) error {
	if att == nil {
		return ErrAttProcNilAttestation
	}
	if len(att.AttestingIndices) == 0 {
		return ErrAttProcEmptyCommittee
	}

	// Verify indices are sorted and unique.
	for i := 1; i < len(att.AttestingIndices); i++ {
		if att.AttestingIndices[i] <= att.AttestingIndices[i-1] {
			return fmt.Errorf("attproc: attesting indices not sorted: %d >= %d",
				att.AttestingIndices[i-1], att.AttestingIndices[i])
		}
	}

	// Signature verification placeholder: in production, aggregate the BLS
	// pubkeys for all attesting indices and verify against att.Signature.
	var emptySig [96]byte
	if att.Signature == emptySig {
		return ErrAttProcInvalidSignature
	}

	return nil
}

// AttProcUpdateParticipationFlags updates the participation flags for all
// attesting validators based on the timeliness of their attestation.
// Returns true if any new flag was set (i.e., the attestation is useful).
func AttProcUpdateParticipationFlags(
	state *BsnBeaconState,
	attestingIndices []uint64,
	timelyFlags [AttProcParticipationFlagCount]bool,
	targetEpoch Epoch,
) (bool, error) {
	if state == nil {
		return false, ErrAttProcNilState
	}
	currentEpoch := state.BsnGetCurrentEpoch()

	state.mu.Lock()
	defer state.mu.Unlock()

	// Determine which participation array to update.
	var participation []uint8
	if targetEpoch == currentEpoch {
		participation = state.CurrentEpochParticipation
	} else {
		participation = state.PreviousEpochParticipation
	}

	anyNewFlag := false

	for _, idx := range attestingIndices {
		if idx >= uint64(len(participation)) {
			continue
		}
		if idx >= uint64(len(state.Validators)) {
			continue
		}
		// Only update flags for active validators.
		if !state.Validators[idx].IsActiveV2(targetEpoch) {
			continue
		}

		oldFlags := participation[idx]
		newFlags := oldFlags

		for flagIdx := uint8(0); flagIdx < AttProcParticipationFlagCount; flagIdx++ {
			if timelyFlags[flagIdx] && !BsnHasParticipationFlag(oldFlags, flagIdx) {
				newFlags |= 1 << flagIdx
			}
		}

		if newFlags != oldFlags {
			participation[idx] = newFlags
			anyNewFlag = true
		}
	}

	// Write back the updated participation.
	if targetEpoch == currentEpoch {
		state.CurrentEpochParticipation = participation
	} else {
		state.PreviousEpochParticipation = participation
	}

	return anyNewFlag, nil
}

// AttProcProcessAttestation is the main entry point for processing a single
// attestation against the beacon state. It validates the attestation data,
// extracts attesting indices, checks timeliness, and updates participation flags.
func AttProcProcessAttestation(
	state *BsnBeaconState,
	att *AttProcAttestation,
) error {
	if state == nil {
		return ErrAttProcNilState
	}
	if att == nil {
		return ErrAttProcNilAttestation
	}

	// Step 1: Validate attestation data.
	if err := AttProcValidateAttestationData(state, &att.Data); err != nil {
		return err
	}

	// Step 2: Extract attesting indices.
	attestingIndices, err := AttProcGetAttestingIndices(
		state, &att.Data, att.AggregationBits, att.CommitteeBits,
	)
	if err != nil {
		return err
	}
	if len(attestingIndices) == 0 {
		return ErrAttProcEmptyCommittee
	}

	// Step 3: Verify indexed attestation (signature placeholder).
	indexedAtt := &AttProcIndexedAttestation{
		AttestingIndices: attestingIndices,
		Data:             att.Data,
		Signature:        att.Signature,
	}
	if err := AttProcVerifyIndexedAttestation(state, indexedAtt); err != nil {
		return err
	}

	// Step 4: Check timeliness and determine which flags are applicable.
	spe := state.SlotsPerEpoch
	if spe == 0 {
		spe = AttProcSlotsPerEpoch
	}
	timelyFlags := AttProcCheckTimeliness(state.Slot, att.Data.Slot, spe)

	// Step 5: Update participation flags.
	anyNew, err := AttProcUpdateParticipationFlags(
		state, attestingIndices, timelyFlags, att.Data.Target.Epoch,
	)
	if err != nil {
		return err
	}
	if !anyNew {
		return ErrAttProcNoNewFlags
	}

	return nil
}

// AttProcProcessPendingAttestations processes multiple attestations at epoch
// boundary. This is used for batch processing of pending attestations that
// were deferred during the epoch.
func AttProcProcessPendingAttestations(
	state *BsnBeaconState,
	attestations []*AttProcAttestation,
) (processed int, errList []error) {
	if state == nil {
		return 0, []error{ErrAttProcNilState}
	}

	for _, att := range attestations {
		if err := AttProcProcessAttestation(state, att); err != nil {
			errList = append(errList, err)
		} else {
			processed++
		}
	}
	return processed, errList
}

// AttProcComputeParticipationReward computes the base reward for a single
// participation flag for a validator with the given effective balance.
//
// base_reward = effective_balance * BASE_REWARD_FACTOR / sqrt(total_balance) / BASE_REWARDS_PER_EPOCH
// flag_reward = base_reward * flag_weight / WEIGHT_DENOMINATOR
func AttProcComputeParticipationReward(
	effectiveBalance uint64,
	totalActiveBalance uint64,
	flagIndex uint8,
) uint64 {
	if totalActiveBalance == 0 || flagIndex >= AttProcParticipationFlagCount {
		return 0
	}
	sqrtBalance := attProcIntSqrt(totalActiveBalance)
	if sqrtBalance == 0 {
		return 0
	}
	baseReward := effectiveBalance * BaseRewardFactor / sqrtBalance / BaseRewardsPerEpoch
	weight := attProcFlagWeights[flagIndex]
	return baseReward * weight / BsnWeightDenominator
}

// AttProcCountParticipation counts the number of validators that have each
// participation flag set in the given participation array.
func AttProcCountParticipation(
	participation []uint8,
	validators []*ValidatorV2,
	epoch Epoch,
) [AttProcParticipationFlagCount]uint64 {
	var counts [AttProcParticipationFlagCount]uint64
	for i, p := range participation {
		if i >= len(validators) {
			break
		}
		if !validators[i].IsActiveV2(epoch) {
			continue
		}
		for flagIdx := uint8(0); flagIdx < AttProcParticipationFlagCount; flagIdx++ {
			if BsnHasParticipationFlag(p, flagIdx) {
				counts[flagIdx]++
			}
		}
	}
	return counts
}

// AttProcConvertToAttProcData converts consensus.AttestationData to
// AttProcAttestationData for processing against BsnBeaconState.
func AttProcConvertToAttProcData(data *AttestationData) AttProcAttestationData {
	var sourceRoot, targetRoot, blockRoot [32]byte
	copy(sourceRoot[:], data.Source.Root[:])
	copy(targetRoot[:], data.Target.Root[:])
	copy(blockRoot[:], data.BeaconBlockRoot[:])
	return AttProcAttestationData{
		Slot:            uint64(data.Slot),
		BeaconBlockRoot: blockRoot,
		Source: CheckpointV2{
			Epoch: data.Source.Epoch,
			Root:  sourceRoot,
		},
		Target: CheckpointV2{
			Epoch: data.Target.Epoch,
			Root:  targetRoot,
		},
	}
}

// AttProcConvertFromTypesHash converts a types.Hash to a [32]byte for
// processing.
func AttProcConvertFromTypesHash(h types.Hash) [32]byte {
	var out [32]byte
	copy(out[:], h[:])
	return out
}
