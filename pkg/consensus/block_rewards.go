// block_rewards.go implements block reward computation per the Ethereum
// beacon chain spec (Altair/Bellatrix).
//
// Reward components:
//   - AttestationRewards: source/target/head weighted rewards
//   - ProposerReward: fraction of attester reward given to block proposer
//   - SyncCommitteeReward: per-slot reward for sync committee participants
//   - InactivityPenalty: quadratic penalty during finality leak
//
// Per Altair, total reward weights sum to 64:
//   source=14, target=26, head=14, sync=2, proposer=8
package consensus

import (
	"errors"
	"math"
)

// Block reward constants per Altair spec.
const (
	// BRBaseRewardFactor is the Altair base reward factor.
	BRBaseRewardFactor uint64 = 64

	// Altair reward weights (sum to 64).
	BRSourceWeight   uint64 = 14
	BRTargetWeight   uint64 = 26
	BRHeadWeight     uint64 = 14
	BRSyncWeight     uint64 = 2
	BRProposerWeight uint64 = 8
	BRWeightDenom    uint64 = 64

	// BRSyncCommitteeSize is the sync committee size for reward computation.
	BRSyncCommitteeSize uint64 = 512

	// BRDefaultSlotsPerEpoch is the default slots per epoch.
	BRDefaultSlotsPerEpoch uint64 = 32

	// BRInactivityPenaltyQuotient is the Bellatrix inactivity quotient.
	BRInactivityPenaltyQuotient uint64 = 3 * (1 << 24)

	// BRMinEpochsToInactivityPenalty is the finality delay threshold.
	BRMinEpochsToInactivityPenalty uint64 = 4
)

// Block reward errors.
var (
	ErrBRNilInput       = errors.New("block_rewards: nil input")
	ErrBRNoValidators   = errors.New("block_rewards: no validators")
	ErrBRLenMismatch    = errors.New("block_rewards: validator/balance length mismatch")
	ErrBRZeroBalance    = errors.New("block_rewards: zero total active balance")
	ErrBRInvalidIndex   = errors.New("block_rewards: validator index out of range")
)

// BlockRewardInput holds all inputs for block reward computation.
type BlockRewardInput struct {
	// Validators and their balances.
	Validators []*BlockRewardValidator
	Balances   []uint64

	// Epoch information.
	CurrentEpoch   Epoch
	FinalizedEpoch Epoch
	SlotsPerEpoch  uint64

	// Attestation participation flags for previous epoch.
	SourceAttested map[ValidatorIndex]bool
	TargetAttested map[ValidatorIndex]bool
	HeadAttested   map[ValidatorIndex]bool

	// Sync committee participation: validator -> number of sync duties.
	SyncParticipants    map[ValidatorIndex]uint64
	SyncCommitteeSize   uint64

	// Inactivity scores per validator (Altair+).
	InactivityScores []uint64

	// ProposerIndex is the block proposer for reward calculation.
	ProposerIndex ValidatorIndex
}

// BlockRewardValidator holds per-validator fields needed for rewards.
type BlockRewardValidator struct {
	EffectiveBalance uint64
	Slashed          bool
	Active           bool
}

// AttestationRewardBreakdown holds the attestation reward components
// for a single validator.
type AttestationRewardBreakdown struct {
	SourceReward int64
	TargetReward int64
	HeadReward   int64
}

// BlockRewardBreakdown holds the full reward breakdown for a validator.
type BlockRewardBreakdown struct {
	ValidatorIdx   ValidatorIndex
	Attestation    AttestationRewardBreakdown
	ProposerReward int64
	SyncReward     int64
	InactivityPen  int64
	NetReward      int64
}

// BlockRewardOutput holds aggregate block reward computation results.
type BlockRewardOutput struct {
	Breakdowns       []BlockRewardBreakdown
	ProposerTotal    int64  // total reward earned by the block proposer
	TotalRewards     int64
	TotalPenalties   int64
	FinalityDelay    uint64
	InLeakMode       bool
}

// BlockRewardEngine computes beacon chain block rewards per Altair spec.
type BlockRewardEngine struct {
	// No mutable state; all computation is pure.
}

// NewBlockRewardEngine creates a new block reward engine.
func NewBlockRewardEngine() *BlockRewardEngine {
	return &BlockRewardEngine{}
}

// ComputeBlockRewards calculates rewards and penalties for all validators
// based on their participation in the previous epoch.
func (bre *BlockRewardEngine) ComputeBlockRewards(
	input *BlockRewardInput,
) (*BlockRewardOutput, error) {
	if input == nil {
		return nil, ErrBRNilInput
	}
	if len(input.Validators) == 0 {
		return nil, ErrBRNoValidators
	}
	if len(input.Validators) != len(input.Balances) {
		return nil, ErrBRLenMismatch
	}

	tab := brTotalActiveBalance(input)
	if tab == 0 {
		return nil, ErrBRZeroBalance
	}

	sqrtTab := brIsqrt(tab)
	if sqrtTab == 0 {
		sqrtTab = 1
	}

	finalityDelay := brFinalityDelay(input)
	inLeak := finalityDelay > BRMinEpochsToInactivityPenalty

	// Compute component balances.
	srcBal := brComponentBalance(input, input.SourceAttested)
	tgtBal := brComponentBalance(input, input.TargetAttested)
	hdBal := brComponentBalance(input, input.HeadAttested)

	output := &BlockRewardOutput{
		Breakdowns:    make([]BlockRewardBreakdown, len(input.Validators)),
		FinalityDelay: finalityDelay,
		InLeakMode:    inLeak,
	}

	var proposerTotal int64
	inc := EffectiveBalanceIncrement

	for i, v := range input.Validators {
		bd := BlockRewardBreakdown{
			ValidatorIdx: ValidatorIndex(i),
		}

		if !v.Active {
			output.Breakdowns[i] = bd
			continue
		}

		baseReward := brBaseReward(v.EffectiveBalance, sqrtTab)
		idx := ValidatorIndex(i)

		// Attestation rewards.
		bd.Attestation = bre.computeAttestationRewards(
			baseReward, idx, v, input, tab, srcBal, tgtBal, hdBal, inc, inLeak,
		)

		// Proposer reward: attester_reward * PROPOSER_WEIGHT / (WEIGHT_DENOM - PROPOSER_WEIGHT)
		if input.SourceAttested[idx] && !v.Slashed {
			attesterReward := baseReward * (BRWeightDenom - BRProposerWeight) / BRWeightDenom
			propReward := attesterReward * BRProposerWeight / (BRWeightDenom - BRProposerWeight)
			bd.ProposerReward = int64(propReward)
			proposerTotal += bd.ProposerReward
		}

		// Sync committee reward.
		bd.SyncReward = bre.computeSyncReward(idx, input, tab)

		// Inactivity penalty.
		bd.InactivityPen = bre.computeInactivityPenalty(i, idx, v, input, inLeak)

		bd.NetReward = bd.Attestation.SourceReward + bd.Attestation.TargetReward +
			bd.Attestation.HeadReward + bd.ProposerReward + bd.SyncReward + bd.InactivityPen

		if bd.NetReward > 0 {
			output.TotalRewards += bd.NetReward
		} else {
			output.TotalPenalties += -bd.NetReward
		}

		output.Breakdowns[i] = bd
	}

	output.ProposerTotal = proposerTotal
	return output, nil
}

// AttestationRewards computes just the attestation reward components for
// a single validator. Useful for per-validator reward queries.
func (bre *BlockRewardEngine) AttestationRewards(
	effectiveBalance uint64,
	totalActiveBalance uint64,
	sourceBal, targetBal, headBal uint64,
	sourceAttested, targetAttested, headAttested bool,
	slashed, inLeak bool,
) AttestationRewardBreakdown {
	sqrtTab := brIsqrt(totalActiveBalance)
	if sqrtTab == 0 {
		return AttestationRewardBreakdown{}
	}
	baseReward := brBaseReward(effectiveBalance, sqrtTab)
	inc := EffectiveBalanceIncrement

	return AttestationRewardBreakdown{
		SourceReward: brWeightedReward(baseReward, BRSourceWeight, sourceAttested && !slashed, sourceBal, totalActiveBalance, inc, inLeak),
		TargetReward: brWeightedReward(baseReward, BRTargetWeight, targetAttested && !slashed, targetBal, totalActiveBalance, inc, inLeak),
		HeadReward:   brHeadReward(baseReward, headAttested && !slashed, headBal, totalActiveBalance, inc, inLeak),
	}
}

// ProposerReward computes the proposer reward for including an attestation
// with the given attester base reward.
//
// proposer_reward = base_reward * PROPOSER_WEIGHT / (WEIGHT_DENOM - PROPOSER_WEIGHT)
func ProposerReward(baseReward uint64) uint64 {
	attesterReward := baseReward * (BRWeightDenom - BRProposerWeight) / BRWeightDenom
	return attesterReward * BRProposerWeight / (BRWeightDenom - BRProposerWeight)
}

// SyncCommitteeRewardPerSlot computes the per-slot sync committee reward
// for a participating member.
//
// reward = total_active_balance * SYNC_WEIGHT / (WEIGHT_DENOM * sync_committee_size * slots_per_epoch)
func SyncCommitteeRewardPerSlot(
	totalActiveBalance uint64,
	syncCommitteeSize uint64,
	slotsPerEpoch uint64,
) uint64 {
	if syncCommitteeSize == 0 {
		syncCommitteeSize = BRSyncCommitteeSize
	}
	if slotsPerEpoch == 0 {
		slotsPerEpoch = BRDefaultSlotsPerEpoch
	}
	denom := BRWeightDenom * syncCommitteeSize * slotsPerEpoch
	if denom == 0 {
		return 0
	}
	return totalActiveBalance * BRSyncWeight / denom
}

// BaseRewardForValidator computes the Altair base reward for a validator.
// base_reward = effective_balance * BASE_REWARD_FACTOR / sqrt(total_active_balance)
func BaseRewardForValidator(effectiveBalance, totalActiveBalance uint64) uint64 {
	sqrtTab := brIsqrt(totalActiveBalance)
	if sqrtTab == 0 {
		return 0
	}
	return effectiveBalance * BRBaseRewardFactor / sqrtTab
}

// --- Internal computation methods ---

func (bre *BlockRewardEngine) computeAttestationRewards(
	baseReward uint64,
	idx ValidatorIndex,
	v *BlockRewardValidator,
	input *BlockRewardInput,
	tab, srcBal, tgtBal, hdBal, inc uint64,
	inLeak bool,
) AttestationRewardBreakdown {
	return AttestationRewardBreakdown{
		SourceReward: brWeightedReward(baseReward, BRSourceWeight,
			input.SourceAttested[idx] && !v.Slashed, srcBal, tab, inc, inLeak),
		TargetReward: brWeightedReward(baseReward, BRTargetWeight,
			input.TargetAttested[idx] && !v.Slashed, tgtBal, tab, inc, inLeak),
		HeadReward: brHeadReward(baseReward,
			input.HeadAttested[idx] && !v.Slashed, hdBal, tab, inc, inLeak),
	}
}

func (bre *BlockRewardEngine) computeSyncReward(
	idx ValidatorIndex,
	input *BlockRewardInput,
	tab uint64,
) int64 {
	if input.SyncParticipants == nil {
		return 0
	}
	count, ok := input.SyncParticipants[idx]
	if !ok || count == 0 {
		return 0
	}

	perSlot := SyncCommitteeRewardPerSlot(
		tab,
		input.SyncCommitteeSize,
		input.SlotsPerEpoch,
	)
	return int64(perSlot * count)
}

func (bre *BlockRewardEngine) computeInactivityPenalty(
	i int,
	idx ValidatorIndex,
	v *BlockRewardValidator,
	input *BlockRewardInput,
	inLeak bool,
) int64 {
	if !inLeak {
		return 0
	}
	if input.TargetAttested[idx] && !v.Slashed {
		return 0
	}

	inactScore := uint64(0)
	if i < len(input.InactivityScores) {
		inactScore = input.InactivityScores[i]
	}
	penalty := v.EffectiveBalance * inactScore / BRInactivityPenaltyQuotient
	return -int64(penalty)
}

// --- Pure helper functions ---

// brWeightedReward computes a weighted attestation reward/penalty.
func brWeightedReward(
	baseReward, weight uint64,
	attested bool,
	compBal, totalBal, inc uint64,
	inLeak bool,
) int64 {
	wReward := baseReward * weight / BRWeightDenom
	if attested {
		if inLeak {
			return int64(wReward)
		}
		if totalBal > 0 {
			return int64(wReward * (compBal / inc) / (totalBal / inc))
		}
		return 0
	}
	return -int64(wReward)
}

// brHeadReward computes the head attestation reward. Per Altair, there is
// no penalty for failing to attest to the correct head (only for source/target).
func brHeadReward(
	baseReward uint64,
	attested bool,
	hdBal, totalBal, inc uint64,
	inLeak bool,
) int64 {
	if !attested {
		return 0 // No penalty for head in Altair.
	}
	hdR := baseReward * BRHeadWeight / BRWeightDenom
	if inLeak {
		return int64(hdR)
	}
	if totalBal > 0 {
		return int64(hdR * (hdBal / inc) / (totalBal / inc))
	}
	return 0
}

// brBaseReward computes the Altair base reward.
func brBaseReward(effectiveBalance, sqrtTotal uint64) uint64 {
	if sqrtTotal == 0 {
		return 0
	}
	return effectiveBalance * BRBaseRewardFactor / sqrtTotal
}

// brTotalActiveBalance sums effective balances of active validators.
func brTotalActiveBalance(input *BlockRewardInput) uint64 {
	var total uint64
	for _, v := range input.Validators {
		if v.Active {
			total += v.EffectiveBalance
		}
	}
	if total < EffectiveBalanceIncrement {
		return EffectiveBalanceIncrement
	}
	return total
}

// brComponentBalance sums effective balances of active unslashed attesters.
func brComponentBalance(input *BlockRewardInput, flags map[ValidatorIndex]bool) uint64 {
	var total uint64
	for i, v := range input.Validators {
		if v.Active && !v.Slashed && flags[ValidatorIndex(i)] {
			total += v.EffectiveBalance
		}
	}
	return total
}

// brFinalityDelay computes epochs since last finalization.
func brFinalityDelay(input *BlockRewardInput) uint64 {
	pe := input.CurrentEpoch
	if pe > 0 {
		pe = pe - 1
	}
	if uint64(pe) <= uint64(input.FinalizedEpoch) {
		return 0
	}
	return uint64(pe) - uint64(input.FinalizedEpoch)
}

// brIsqrt computes the integer square root using Newton's method.
func brIsqrt(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	if n == math.MaxUint64 {
		return 4294967295
	}
	x, y := n, (n+1)/2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}
