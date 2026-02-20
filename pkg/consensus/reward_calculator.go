// reward_calculator.go implements beacon chain validator reward computation
// per the Ethereum consensus spec. Supports Phase0 and Altair reward schemes.
package consensus

import (
	"errors"
	"math"
	"sync"
)

// Reward calculation constants per spec.
const (
	// RCBaseRewardFactor is the base reward factor (64).
	RCBaseRewardFactor uint64 = 64

	// RCBaseRewardsPerEpoch is the number of base reward components (4 in Phase0).
	RCBaseRewardsPerEpoch uint64 = 4

	// RCProposerRewardQuotient is the divisor for proposer rewards in Phase0.
	RCProposerRewardQuotient uint64 = 8

	// RCMinEpochsToInactivityPenalty is the minimum finality delay before
	// inactivity penalties apply.
	RCMinEpochsToInactivityPenalty uint64 = 4

	// RCInactivityPenaltyQuotient is the Phase0 inactivity penalty denominator.
	RCInactivityPenaltyQuotient uint64 = 1 << 26

	// RCInactivityPenaltyQuotientAltair is the Altair inactivity penalty denominator.
	RCInactivityPenaltyQuotientAltair uint64 = 3 * (1 << 24)

	// Altair reward weight constants (out of WeightDenominatorAltair = 64).
	AltairSourceWeight        uint64 = 14
	AltairTargetWeight        uint64 = 26
	AltairHeadWeight          uint64 = 14
	AltairSyncWeight          uint64 = 2
	AltairProposerWeight      uint64 = 8
	WeightDenominatorAltair   uint64 = 64

	// RCSyncCommitteeSize is the sync committee size for reward computation.
	RCSyncCommitteeSize uint64 = 512

	// RCSlotsPerEpoch is the default slots per epoch.
	RCSlotsPerEpoch uint64 = 32
)

// Reward calculator errors.
var (
	ErrRCNilState         = errors.New("reward_calculator: nil state")
	ErrRCNilParticipation = errors.New("reward_calculator: nil participation")
	ErrRCNoValidators     = errors.New("reward_calculator: no validators")
	ErrRCZeroBalance      = errors.New("reward_calculator: zero total active balance")
	ErrRCLengthMismatch   = errors.New("reward_calculator: validator/balance length mismatch")
	ErrRCUnknownFork      = errors.New("reward_calculator: unknown fork")
)

// ForkScheme identifies the reward calculation scheme.
type ForkScheme int

const (
	// ForkPhase0 uses Phase0 reward rules.
	ForkPhase0 ForkScheme = iota

	// ForkAltair uses Altair weight-based reward rules.
	ForkAltair
)

// RewardState holds the beacon state fields needed for reward computation.
type RewardState struct {
	Validators          []*RewardValidator
	Balances            []uint64
	CurrentEpoch        Epoch
	FinalizedEpoch      Epoch
	SlotsPerEpoch       uint64
	SyncCommitteeSize   uint64
}

// RewardValidator holds per-validator fields relevant to rewards.
type RewardValidator struct {
	EffectiveBalance uint64
	Slashed          bool
	Active           bool
}

// Participation tracks which validators attested to each component.
type Participation struct {
	Source map[int]bool
	Target map[int]bool
	Head   map[int]bool
}

// NewParticipation creates a participation tracker with empty maps.
func NewParticipation() *Participation {
	return &Participation{
		Source: make(map[int]bool),
		Target: make(map[int]bool),
		Head:   make(map[int]bool),
	}
}

// ValidatorReward holds the reward breakdown for a single validator.
type ValidatorReward struct {
	Index          int
	SourceReward   int64
	TargetReward   int64
	HeadReward     int64
	InclusionReward int64
	SyncReward     int64
	InactivityPen  int64
	NetReward      int64
}

// RewardSummary aggregates the reward results across all validators.
type RewardSummary struct {
	Validators    []ValidatorReward
	TotalRewards  int64
	TotalPenalties int64
	FinalityDelay uint64
	InLeakMode    bool
}

// RewardCalculatorConfig configures the reward calculator.
type RewardCalculatorConfig struct {
	Fork                  ForkScheme
	ProposerRewardQuotient uint64
	InactivityQuotient     uint64
}

// DefaultRewardConfig returns the default Phase0 reward config.
func DefaultRewardConfig() *RewardCalculatorConfig {
	return &RewardCalculatorConfig{
		Fork:                   ForkPhase0,
		ProposerRewardQuotient: RCProposerRewardQuotient,
		InactivityQuotient:     RCInactivityPenaltyQuotient,
	}
}

// AltairRewardConfig returns the Altair reward config.
func AltairRewardConfig() *RewardCalculatorConfig {
	return &RewardCalculatorConfig{
		Fork:                   ForkAltair,
		ProposerRewardQuotient: RCProposerRewardQuotient,
		InactivityQuotient:     RCInactivityPenaltyQuotientAltair,
	}
}

// RewardCalculator computes validator rewards per the beacon chain spec.
// Thread-safe.
type RewardCalculator struct {
	mu     sync.Mutex
	config *RewardCalculatorConfig
}

// NewRewardCalculator creates a reward calculator with the given config.
func NewRewardCalculator(cfg *RewardCalculatorConfig) *RewardCalculator {
	if cfg == nil {
		cfg = DefaultRewardConfig()
	}
	return &RewardCalculator{config: cfg}
}

// ComputeRewards calculates rewards and penalties for all validators based
// on their participation in the previous epoch.
func (rc *RewardCalculator) ComputeRewards(
	state *RewardState,
	participation *Participation,
) (*RewardSummary, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if state == nil {
		return nil, ErrRCNilState
	}
	if participation == nil {
		return nil, ErrRCNilParticipation
	}
	if len(state.Validators) == 0 {
		return nil, ErrRCNoValidators
	}
	if len(state.Validators) != len(state.Balances) {
		return nil, ErrRCLengthMismatch
	}

	totalActive := rc.totalActiveBalance(state)
	if totalActive == 0 {
		return nil, ErrRCZeroBalance
	}

	sqrtTotal := intSqrtReward(totalActive)
	if sqrtTotal == 0 {
		sqrtTotal = 1
	}

	// Compute finality delay.
	var finalityDelay uint64
	if uint64(state.CurrentEpoch) > uint64(state.FinalizedEpoch) {
		finalityDelay = uint64(state.CurrentEpoch) - uint64(state.FinalizedEpoch)
	}
	inLeak := finalityDelay > RCMinEpochsToInactivityPenalty

	// Compute component balances (total effective balance of unslashed attesters).
	sourceBalance := rc.componentBalance(state, participation.Source)
	targetBalance := rc.componentBalance(state, participation.Target)
	headBalance := rc.componentBalance(state, participation.Head)

	summary := &RewardSummary{
		Validators:    make([]ValidatorReward, len(state.Validators)),
		FinalityDelay: finalityDelay,
		InLeakMode:    inLeak,
	}

	for i, v := range state.Validators {
		vr := ValidatorReward{Index: i}

		if !v.Active {
			summary.Validators[i] = vr
			continue
		}

		switch rc.config.Fork {
		case ForkPhase0:
			rc.computePhase0Rewards(
				v, i, participation, totalActive, sqrtTotal,
				sourceBalance, targetBalance, headBalance,
				finalityDelay, inLeak, &vr,
			)
		case ForkAltair:
			rc.computeAltairRewards(
				v, i, participation, totalActive, sqrtTotal,
				sourceBalance, targetBalance, headBalance,
				finalityDelay, inLeak, state, &vr,
			)
		}

		vr.NetReward = vr.SourceReward + vr.TargetReward + vr.HeadReward +
			vr.InclusionReward + vr.SyncReward + vr.InactivityPen

		if vr.NetReward > 0 {
			summary.TotalRewards += vr.NetReward
		} else {
			summary.TotalPenalties += -vr.NetReward
		}

		summary.Validators[i] = vr
	}

	return summary, nil
}

// computePhase0Rewards computes Phase0-style rewards for a single validator.
func (rc *RewardCalculator) computePhase0Rewards(
	v *RewardValidator,
	idx int,
	part *Participation,
	totalActive, sqrtTotal uint64,
	srcBal, tgtBal, hdBal uint64,
	finalityDelay uint64,
	inLeak bool,
	vr *ValidatorReward,
) {
	baseReward := rc.baseReward(v.EffectiveBalance, sqrtTotal)

	// Source reward.
	if part.Source[idx] && !v.Slashed {
		if inLeak {
			vr.SourceReward = int64(baseReward)
		} else {
			vr.SourceReward = int64(baseReward * srcBal / totalActive)
		}
	} else {
		vr.SourceReward = -int64(baseReward)
	}

	// Target reward.
	if part.Target[idx] && !v.Slashed {
		if inLeak {
			vr.TargetReward = int64(baseReward)
		} else {
			vr.TargetReward = int64(baseReward * tgtBal / totalActive)
		}
	} else {
		vr.TargetReward = -int64(baseReward)
	}

	// Head reward.
	if part.Head[idx] && !v.Slashed {
		if inLeak {
			vr.HeadReward = int64(baseReward)
		} else {
			vr.HeadReward = int64(baseReward * hdBal / totalActive)
		}
	} else {
		vr.HeadReward = -int64(baseReward)
	}

	// Proposer reward: simplified as base_reward / PROPOSER_REWARD_QUOTIENT
	// for attestation includers.
	if part.Source[idx] && !v.Slashed {
		includerReward := baseReward / rc.config.ProposerRewardQuotient
		vr.InclusionReward = int64(includerReward)
	}

	// Inactivity penalty: applied to non-target attesters during leak.
	if inLeak && !(part.Target[idx] && !v.Slashed) {
		penalty := v.EffectiveBalance * finalityDelay / rc.config.InactivityQuotient
		vr.InactivityPen = -int64(penalty)
	}
}

// computeAltairRewards computes Altair weight-based rewards for a single validator.
func (rc *RewardCalculator) computeAltairRewards(
	v *RewardValidator,
	idx int,
	part *Participation,
	totalActive, sqrtTotal uint64,
	srcBal, tgtBal, hdBal uint64,
	finalityDelay uint64,
	inLeak bool,
	state *RewardState,
	vr *ValidatorReward,
) {
	baseReward := rc.baseRewardAltair(v.EffectiveBalance, sqrtTotal)

	// Source component with Altair weight.
	if part.Source[idx] && !v.Slashed {
		if inLeak {
			vr.SourceReward = int64(baseReward * AltairSourceWeight / WeightDenominatorAltair)
		} else {
			vr.SourceReward = int64(baseReward * AltairSourceWeight *
				srcBal / totalActive / WeightDenominatorAltair)
		}
	} else {
		vr.SourceReward = -int64(baseReward * AltairSourceWeight / WeightDenominatorAltair)
	}

	// Target component with Altair weight.
	if part.Target[idx] && !v.Slashed {
		if inLeak {
			vr.TargetReward = int64(baseReward * AltairTargetWeight / WeightDenominatorAltair)
		} else {
			vr.TargetReward = int64(baseReward * AltairTargetWeight *
				tgtBal / totalActive / WeightDenominatorAltair)
		}
	} else {
		vr.TargetReward = -int64(baseReward * AltairTargetWeight / WeightDenominatorAltair)
	}

	// Head component with Altair weight.
	if part.Head[idx] && !v.Slashed {
		if inLeak {
			vr.HeadReward = int64(baseReward * AltairHeadWeight / WeightDenominatorAltair)
		} else {
			vr.HeadReward = int64(baseReward * AltairHeadWeight *
				hdBal / totalActive / WeightDenominatorAltair)
		}
	} else {
		// In Altair, head non-attesters get zero penalty (not negative).
		vr.HeadReward = 0
	}

	// Sync committee reward.
	slotsPerEpoch := state.SlotsPerEpoch
	if slotsPerEpoch == 0 {
		slotsPerEpoch = RCSlotsPerEpoch
	}
	syncSize := state.SyncCommitteeSize
	if syncSize == 0 {
		syncSize = RCSyncCommitteeSize
	}
	if syncSize > 0 && slotsPerEpoch > 0 {
		vr.SyncReward = int64(totalActive / syncSize / slotsPerEpoch)
	}

	// Inactivity penalty.
	if inLeak && !(part.Target[idx] && !v.Slashed) {
		penalty := v.EffectiveBalance * finalityDelay / rc.config.InactivityQuotient
		vr.InactivityPen = -int64(penalty)
	}
}

// baseReward computes the Phase0 base reward for a validator.
// base_reward = effective_balance * BASE_REWARD_FACTOR / sqrt(total_active) / BASE_REWARDS_PER_EPOCH
func (rc *RewardCalculator) baseReward(effectiveBalance, sqrtTotal uint64) uint64 {
	if sqrtTotal == 0 || RCBaseRewardsPerEpoch == 0 {
		return 0
	}
	return effectiveBalance * RCBaseRewardFactor / sqrtTotal / RCBaseRewardsPerEpoch
}

// baseRewardAltair computes the Altair base reward (no division by BASE_REWARDS_PER_EPOCH).
// base_reward = effective_balance * BASE_REWARD_FACTOR / sqrt(total_active)
func (rc *RewardCalculator) baseRewardAltair(effectiveBalance, sqrtTotal uint64) uint64 {
	if sqrtTotal == 0 {
		return 0
	}
	return effectiveBalance * RCBaseRewardFactor / sqrtTotal
}

// totalActiveBalance computes the sum of effective balances of active validators.
func (rc *RewardCalculator) totalActiveBalance(state *RewardState) uint64 {
	var total uint64
	for _, v := range state.Validators {
		if v.Active {
			total += v.EffectiveBalance
		}
	}
	return total
}

// componentBalance computes the total effective balance of unslashed validators
// that participated in a given component (source, target, or head).
func (rc *RewardCalculator) componentBalance(state *RewardState, attested map[int]bool) uint64 {
	var total uint64
	for i, v := range state.Validators {
		if v.Active && !v.Slashed && attested[i] {
			total += v.EffectiveBalance
		}
	}
	return total
}

// intSqrtReward computes the integer square root using Newton's method.
func intSqrtReward(n uint64) uint64 {
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
