// reward_calculator_v2.go implements Altair/Bellatrix reward computation for
// the Ethereum beacon chain. Extends the existing reward_calculator.go with
// per-validator inactivity score tracking, sync committee rewards, proposer
// rewards, and quadratic inactivity leak penalties.
//
// This integrates with EpochTransitionState and provides a standalone reward
// engine that can be used independently of the epoch transition pipeline.
package consensus

import (
	"errors"
	"math"
	"sync"
)

// Reward calculator v2 constants.
const (
	// RCV2BaseRewardFactor is the Altair base reward factor.
	RCV2BaseRewardFactor uint64 = 64

	// Altair reward weight constants (sum to 64).
	RCV2SourceWeight    uint64 = 14
	RCV2TargetWeight    uint64 = 26
	RCV2HeadWeight      uint64 = 14
	RCV2SyncWeight      uint64 = 2
	RCV2ProposerWeight  uint64 = 8
	RCV2WeightDenom     uint64 = 64

	// RCV2SyncCommitteeSize is the sync committee size.
	RCV2SyncCommitteeSize uint64 = 512

	// RCV2InactivityPenaltyQuotient is the Bellatrix inactivity quotient.
	RCV2InactivityPenaltyQuotient uint64 = 3 * (1 << 24)

	// RCV2MinEpochsToInactivityPen is the threshold for inactivity penalties.
	RCV2MinEpochsToInactivityPen uint64 = 4

	// RCV2ProposerRewardQuotient is the divisor for proposer rewards.
	RCV2ProposerRewardQuotient uint64 = 8

	// RCV2MaxSlashingsPerEpoch bounds slashing penalty processing.
	RCV2MaxSlashingsPerEpoch = 16
)

// Reward calculator v2 errors.
var (
	ErrRCV2NilInput       = errors.New("reward_calc_v2: nil input")
	ErrRCV2NoValidators   = errors.New("reward_calc_v2: no validators")
	ErrRCV2LenMismatch    = errors.New("reward_calc_v2: length mismatch")
	ErrRCV2ZeroBalance    = errors.New("reward_calc_v2: zero total active balance")
	ErrRCV2InvalidIndex   = errors.New("reward_calc_v2: validator index out of range")
)

// RewardInputV2 holds the inputs needed for v2 reward computation.
type RewardInputV2 struct {
	Validators       []*ValidatorV2
	Balances         []uint64
	InactivityScores []uint64 // per-validator inactivity scores
	CurrentEpoch     Epoch
	FinalizedEpoch   Epoch
	SlotsPerEpoch    uint64
	SyncCommitteeSize uint64

	// Participation flags for the previous epoch.
	PrevSource map[ValidatorIndex]bool
	PrevTarget map[ValidatorIndex]bool
	PrevHead   map[ValidatorIndex]bool

	// SyncParticipants tracks which validators participated in sync
	// committee duties during the epoch.
	SyncParticipants map[ValidatorIndex]uint64 // validator -> count of sync duties
}

// RewardBreakdownV2 holds the per-validator reward breakdown.
type RewardBreakdownV2 struct {
	Index           ValidatorIndex
	SourceDelta     int64
	TargetDelta     int64
	HeadDelta       int64
	SyncDelta       int64
	ProposerDelta   int64
	InactivityDelta int64
	NetDelta        int64
}

// RewardOutputV2 holds the aggregate reward computation output.
type RewardOutputV2 struct {
	Breakdowns     []RewardBreakdownV2
	TotalRewards   int64
	TotalPenalties int64
	FinalityDelay  uint64
	InLeakMode     bool
}

// RewardCalculatorV2 computes Altair/Bellatrix-style rewards with per-validator
// inactivity scores, sync committee rewards, and proposer rewards. Thread-safe.
type RewardCalculatorV2 struct {
	mu sync.Mutex
}

// NewRewardCalculatorV2 creates a new v2 reward calculator.
func NewRewardCalculatorV2() *RewardCalculatorV2 {
	return &RewardCalculatorV2{}
}

// ComputeRewardsV2 calculates all reward components for the given input.
func (rc *RewardCalculatorV2) ComputeRewardsV2(input *RewardInputV2) (*RewardOutputV2, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if input == nil {
		return nil, ErrRCV2NilInput
	}
	if len(input.Validators) == 0 {
		return nil, ErrRCV2NoValidators
	}
	if len(input.Validators) != len(input.Balances) {
		return nil, ErrRCV2LenMismatch
	}

	tab := rc.totalActiveBalance(input)
	if tab == 0 {
		return nil, ErrRCV2ZeroBalance
	}

	sqrtTab := rc.isqrt(tab)
	if sqrtTab == 0 {
		sqrtTab = 1
	}

	finalityDelay := rc.finalityDelay(input)
	inLeak := finalityDelay > RCV2MinEpochsToInactivityPen

	srcBal := rc.compBalance(input, input.PrevSource)
	tgtBal := rc.compBalance(input, input.PrevTarget)
	hdBal := rc.compBalance(input, input.PrevHead)

	output := &RewardOutputV2{
		Breakdowns:    make([]RewardBreakdownV2, len(input.Validators)),
		FinalityDelay: finalityDelay,
		InLeakMode:    inLeak,
	}

	pe := input.CurrentEpoch
	if pe > 0 {
		pe = pe - 1
	}

	for i, v := range input.Validators {
		bd := RewardBreakdownV2{Index: ValidatorIndex(i)}
		idx := ValidatorIndex(i)

		if !v.IsActiveV2(pe) {
			output.Breakdowns[i] = bd
			continue
		}

		baseReward := rc.baseReward(v.EffectiveBalance, sqrtTab)

		// Source component.
		bd.SourceDelta = rc.computeComponentReward(
			baseReward, RCV2SourceWeight, input.PrevSource[idx] && !v.Slashed,
			srcBal, tab, inLeak,
		)

		// Target component.
		bd.TargetDelta = rc.computeComponentReward(
			baseReward, RCV2TargetWeight, input.PrevTarget[idx] && !v.Slashed,
			tgtBal, tab, inLeak,
		)

		// Head component (no penalty for non-attestation in Altair).
		if input.PrevHead[idx] && !v.Slashed {
			hdR := baseReward * RCV2HeadWeight / RCV2WeightDenom
			if inLeak {
				bd.HeadDelta = int64(hdR)
			} else if tab > 0 {
				inc := EffectiveBalanceIncrement
				bd.HeadDelta = int64(hdR * (hdBal / inc) / (tab / inc))
			}
		}

		// Sync committee reward.
		if input.SyncParticipants != nil {
			if count, ok := input.SyncParticipants[idx]; ok && count > 0 {
				spe := input.SlotsPerEpoch
				if spe == 0 {
					spe = 32
				}
				syncSize := input.SyncCommitteeSize
				if syncSize == 0 {
					syncSize = RCV2SyncCommitteeSize
				}
				// Per-slot sync reward = total_active_balance * SYNC_REWARD_WEIGHT /
				// (WEIGHT_DENOMINATOR * sync_committee_size * slots_per_epoch)
				denom := RCV2WeightDenom * syncSize * spe
				if denom > 0 {
					perSlot := tab * RCV2SyncWeight / denom
					bd.SyncDelta = int64(perSlot * count)
				}
			}
		}

		// Proposer reward: simplified as base_reward * PROPOSER_WEIGHT / WEIGHT_DENOM
		// for each attestation included. Counted as a single aggregate.
		if input.PrevSource[idx] && !v.Slashed {
			bd.ProposerDelta = int64(baseReward * RCV2ProposerWeight / RCV2WeightDenom / RCV2ProposerRewardQuotient)
		}

		// Inactivity penalty using per-validator inactivity score.
		if inLeak && !(input.PrevTarget[idx] && !v.Slashed) {
			inactScore := uint64(0)
			if i < len(input.InactivityScores) {
				inactScore = input.InactivityScores[i]
			}
			penalty := v.EffectiveBalance * inactScore / RCV2InactivityPenaltyQuotient
			bd.InactivityDelta = -int64(penalty)
		}

		bd.NetDelta = bd.SourceDelta + bd.TargetDelta + bd.HeadDelta +
			bd.SyncDelta + bd.ProposerDelta + bd.InactivityDelta

		if bd.NetDelta > 0 {
			output.TotalRewards += bd.NetDelta
		} else {
			output.TotalPenalties += -bd.NetDelta
		}

		output.Breakdowns[i] = bd
	}

	return output, nil
}

// ComputeBaseRewardV2 calculates the Altair base reward for a single validator.
// base_reward = effective_balance * BASE_REWARD_FACTOR / sqrt(total_active_balance)
func (rc *RewardCalculatorV2) ComputeBaseRewardV2(
	effectiveBalance, totalActiveBalance uint64,
) uint64 {
	sqrtTab := rc.isqrt(totalActiveBalance)
	if sqrtTab == 0 {
		return 0
	}
	return effectiveBalance * RCV2BaseRewardFactor / sqrtTab
}

// ComputeSyncReward calculates the per-slot sync committee reward.
func (rc *RewardCalculatorV2) ComputeSyncReward(
	totalActiveBalance, slotsPerEpoch, syncCommitteeSize uint64,
) uint64 {
	if slotsPerEpoch == 0 {
		slotsPerEpoch = 32
	}
	if syncCommitteeSize == 0 {
		syncCommitteeSize = RCV2SyncCommitteeSize
	}
	denom := RCV2WeightDenom * syncCommitteeSize * slotsPerEpoch
	if denom == 0 {
		return 0
	}
	return totalActiveBalance * RCV2SyncWeight / denom
}

// ComputeProposerReward calculates the proposer reward for including an
// attestation with the given base reward.
func (rc *RewardCalculatorV2) ComputeProposerReward(baseReward uint64) uint64 {
	return baseReward * RCV2ProposerWeight / (RCV2WeightDenom * (RCV2WeightDenom - RCV2ProposerWeight))
}

// ComputeInactivityPenalty calculates the quadratic inactivity penalty.
// penalty = effective_balance * inactivity_score / INACTIVITY_PENALTY_QUOTIENT
func (rc *RewardCalculatorV2) ComputeInactivityPenalty(
	effectiveBalance, inactivityScore uint64,
) uint64 {
	return effectiveBalance * inactivityScore / RCV2InactivityPenaltyQuotient
}

// ComputeSlashingPenalty calculates the slashing penalty for a validator.
// penalty = effective_balance * adjusted_total_slashed / total_balance * increment
func (rc *RewardCalculatorV2) ComputeSlashingPenalty(
	effectiveBalance, totalSlashedBalance, totalActiveBalance uint64,
	proportionalMultiplier uint64,
) uint64 {
	if totalActiveBalance == 0 {
		return 0
	}
	adj := totalSlashedBalance * proportionalMultiplier
	if adj > totalActiveBalance {
		adj = totalActiveBalance
	}
	inc := EffectiveBalanceIncrement
	return effectiveBalance / inc * adj / totalActiveBalance * inc
}

// computeComponentReward calculates the reward or penalty for a single
// attestation component (source, target) using Altair weights.
func (rc *RewardCalculatorV2) computeComponentReward(
	baseReward, weight uint64,
	attested bool,
	compBal, totalBal uint64,
	inLeak bool,
) int64 {
	wReward := baseReward * weight / RCV2WeightDenom
	if attested {
		if inLeak {
			return int64(wReward)
		}
		inc := EffectiveBalanceIncrement
		if totalBal > 0 {
			return int64(wReward * (compBal / inc) / (totalBal / inc))
		}
		return 0
	}
	return -int64(wReward)
}

// baseReward computes the Altair base reward (no per-epoch division).
func (rc *RewardCalculatorV2) baseReward(effectiveBalance, sqrtTotal uint64) uint64 {
	if sqrtTotal == 0 {
		return 0
	}
	return effectiveBalance * RCV2BaseRewardFactor / sqrtTotal
}

// totalActiveBalance computes total effective balance of active validators.
func (rc *RewardCalculatorV2) totalActiveBalance(input *RewardInputV2) uint64 {
	pe := input.CurrentEpoch
	if pe > 0 {
		pe = pe - 1
	}
	var total uint64
	for _, v := range input.Validators {
		if v.IsActiveV2(pe) {
			total += v.EffectiveBalance
		}
	}
	if total < EffectiveBalanceIncrement {
		return EffectiveBalanceIncrement
	}
	return total
}

// compBalance computes the total effective balance of unslashed active
// validators flagged in the given map.
func (rc *RewardCalculatorV2) compBalance(
	input *RewardInputV2, flags map[ValidatorIndex]bool,
) uint64 {
	pe := input.CurrentEpoch
	if pe > 0 {
		pe = pe - 1
	}
	var total uint64
	for i, v := range input.Validators {
		if v.IsActiveV2(pe) && !v.Slashed && flags[ValidatorIndex(i)] {
			total += v.EffectiveBalance
		}
	}
	return total
}

// finalityDelay returns the epochs since last finalization.
func (rc *RewardCalculatorV2) finalityDelay(input *RewardInputV2) uint64 {
	pe := input.CurrentEpoch
	if pe > 0 {
		pe = pe - 1
	}
	if uint64(pe) <= uint64(input.FinalizedEpoch) {
		return 0
	}
	return uint64(pe) - uint64(input.FinalizedEpoch)
}

func (rc *RewardCalculatorV2) isqrt(n uint64) uint64 {
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
