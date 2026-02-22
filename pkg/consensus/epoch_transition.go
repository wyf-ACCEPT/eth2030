// epoch_transition.go implements epoch boundary processing per the Ethereum
// beacon chain spec (Altair/Bellatrix). Processes full epoch transitions
// including justification/finalization, rewards/penalties, registry updates,
// effective balance recalculation, slashings, and inactivity leak tracking.
//
// This complements epoch_processor.go by providing a higher-level orchestrator
// that operates on FullBeaconState and integrates with the validator lifecycle,
// reward calculator, and committee systems.
package consensus

import (
	"errors"
	"math"
	"sync"
)

// Epoch transition constants.
const (
	// ETMinEpochsToInactivityPenalty is the finality delay threshold
	// before the chain enters inactivity leak mode.
	ETMinEpochsToInactivityPenalty uint64 = 4

	// ETInactivityPenaltyQuotientBellatrix is the Bellatrix inactivity
	// penalty denominator (3 * 2^24).
	ETInactivityPenaltyQuotientBellatrix uint64 = 3 * (1 << 24)

	// ETInactivityScoreRecoveryRate is how much the inactivity score
	// decreases per epoch when the chain is finalizing normally.
	ETInactivityScoreRecoveryRate uint64 = 1

	// ETInactivityScoreBias is the inactivity score increment per epoch
	// during an inactivity leak.
	ETInactivityScoreBias uint64 = 4

	// ETProportionalSlashingMultiplierBellatrix is the slashing
	// multiplier for Bellatrix.
	ETProportionalSlashingMultiplierBellatrix uint64 = 3
)

// Epoch transition errors.
var (
	ErrETNilState     = errors.New("epoch_transition: nil state")
	ErrETNoValidators = errors.New("epoch_transition: no validators")
	ErrETMismatch     = errors.New("epoch_transition: validator/balance length mismatch")
	ErrETEarlyEpoch   = errors.New("epoch_transition: cannot process epoch 0")
)

// EpochTransitionParticipation tracks per-validator attestation flags for
// the previous and current epoch. Uses bitflags for compact representation.
type EpochTransitionParticipation struct {
	// PrevSource, PrevTarget, PrevHead track previous epoch attestations.
	PrevSource map[ValidatorIndex]bool
	PrevTarget map[ValidatorIndex]bool
	PrevHead   map[ValidatorIndex]bool

	// CurrSource, CurrTarget, CurrHead track current epoch attestations.
	CurrSource map[ValidatorIndex]bool
	CurrTarget map[ValidatorIndex]bool
	CurrHead   map[ValidatorIndex]bool
}

// NewEpochTransitionParticipation creates a new empty participation tracker.
func NewEpochTransitionParticipation() *EpochTransitionParticipation {
	return &EpochTransitionParticipation{
		PrevSource: make(map[ValidatorIndex]bool),
		PrevTarget: make(map[ValidatorIndex]bool),
		PrevHead:   make(map[ValidatorIndex]bool),
		CurrSource: make(map[ValidatorIndex]bool),
		CurrTarget: make(map[ValidatorIndex]bool),
		CurrHead:   make(map[ValidatorIndex]bool),
	}
}

// RotateParticipation moves current epoch participation to previous and
// resets current epoch maps. Called at epoch boundaries.
func (p *EpochTransitionParticipation) RotateParticipation() {
	p.PrevSource = p.CurrSource
	p.PrevTarget = p.CurrTarget
	p.PrevHead = p.CurrHead
	p.CurrSource = make(map[ValidatorIndex]bool)
	p.CurrTarget = make(map[ValidatorIndex]bool)
	p.CurrHead = make(map[ValidatorIndex]bool)
}

// EpochTransitionState holds all beacon state fields required for a
// full epoch transition. Designed to work with FullBeaconState while
// allowing decoupled testing.
type EpochTransitionState struct {
	CurrentSlot      uint64
	SlotsPerEpoch    uint64
	Validators       []*ValidatorV2
	Balances         []uint64
	Slashings        []uint64 // indexed by epoch % EpochsPerSlashingsVector
	InactivityScores []uint64 // per-validator inactivity scores (Altair+)

	// Justification and finalization.
	JustBits    [4]bool
	PrevJustCP  Checkpoint
	CurrJustCP  Checkpoint
	FinalizedCP Checkpoint
	BlockRoots  []Checkpoint // epoch boundary block roots for justification

	// Participation tracking.
	Participation *EpochTransitionParticipation
}

// CurrentEpoch returns the current epoch for this state.
func (s *EpochTransitionState) CurrentEpoch() Epoch {
	if s.SlotsPerEpoch == 0 {
		return 0
	}
	return Epoch(s.CurrentSlot / s.SlotsPerEpoch)
}

// PreviousEpoch returns the previous epoch, clamped to 0.
func (s *EpochTransitionState) PreviousEpoch() Epoch {
	ce := s.CurrentEpoch()
	if ce > 0 {
		return ce - 1
	}
	return 0
}

// EpochTransitionResult holds the output of an epoch transition.
type EpochTransitionResult struct {
	Rewards        []int64 // per-validator net reward (positive) or penalty (negative)
	Activated      []ValidatorIndex
	Ejected        []ValidatorIndex
	Slashed        []ValidatorIndex // validators penalized by slashings processing
	FinalityDelay  uint64
	InLeakMode     bool
	NewFinalizedCP Checkpoint
	NewJustifiedCP Checkpoint
}

// EpochTransition orchestrates full epoch boundary state transitions.
// Thread-safe.
type EpochTransition struct {
	mu sync.Mutex
}

// NewEpochTransition creates a new epoch transition processor.
func NewEpochTransition() *EpochTransition {
	return &EpochTransition{}
}

// ProcessEpoch runs the full epoch transition sequence on the given state.
// Returns the result summary and modifies state in place.
func (et *EpochTransition) ProcessEpoch(state *EpochTransitionState) (*EpochTransitionResult, error) {
	et.mu.Lock()
	defer et.mu.Unlock()

	if state == nil {
		return nil, ErrETNilState
	}
	if len(state.Validators) == 0 {
		return nil, ErrETNoValidators
	}
	if len(state.Validators) != len(state.Balances) {
		return nil, ErrETMismatch
	}
	ce := state.CurrentEpoch()
	if ce == 0 {
		return nil, ErrETEarlyEpoch
	}

	result := &EpochTransitionResult{
		Rewards: make([]int64, len(state.Validators)),
	}

	// 1. Justification and finalization.
	et.processJustificationFinalization(state, result)

	// 2. Inactivity score updates.
	finalityDelay := et.computeFinalityDelay(state)
	result.FinalityDelay = finalityDelay
	result.InLeakMode = finalityDelay > ETMinEpochsToInactivityPenalty
	et.processInactivityUpdates(state, result.InLeakMode)

	// 3. Rewards and penalties.
	et.processRewardsAndPenalties(state, result)

	// 4. Registry updates (activation/ejection).
	et.processRegistryUpdates(state, result)

	// 5. Slashings processing.
	et.processSlashings(state, result)

	// 6. Effective balance updates.
	et.processEffectiveBalanceUpdates(state)

	// 7. Rotate participation.
	if state.Participation != nil {
		state.Participation.RotateParticipation()
	}

	result.NewFinalizedCP = state.FinalizedCP
	result.NewJustifiedCP = state.CurrJustCP
	return result, nil
}

// processJustificationFinalization implements Casper FFG justification
// and finalization bit tracking.
func (et *EpochTransition) processJustificationFinalization(
	state *EpochTransitionState, result *EpochTransitionResult,
) {
	ce := state.CurrentEpoch()
	if ce <= 1 {
		return
	}
	pe := state.PreviousEpoch()

	oldPJ := state.PrevJustCP
	oldCJ := state.CurrJustCP

	// Rotate justified checkpoints.
	state.PrevJustCP = state.CurrJustCP

	// Shift justification bits: bit[i] = bit[i-1], bit[0] = false.
	for i := 3; i > 0; i-- {
		state.JustBits[i] = state.JustBits[i-1]
	}
	state.JustBits[0] = false

	tab := et.totalActiveBalance(state, ce)
	if tab == 0 {
		return
	}

	// Check previous epoch target attesting balance.
	prevTargetBal := et.targetAttestingBalance(state, pe)
	if prevTargetBal*3 >= tab*2 {
		state.CurrJustCP = et.checkpointForEpoch(state, pe)
		state.JustBits[1] = true
	}

	// Check current epoch target attesting balance.
	currTargetBal := et.currentTargetAttestingBalance(state, ce)
	if currTargetBal*3 >= tab*2 {
		state.CurrJustCP = et.checkpointForEpoch(state, ce)
		state.JustBits[0] = true
	}

	// Apply the four finalization rules.
	b := state.JustBits
	if b[1] && b[2] && b[3] && oldPJ.Epoch+3 == ce {
		state.FinalizedCP = oldPJ
	}
	if b[1] && b[2] && oldPJ.Epoch+2 == ce {
		state.FinalizedCP = oldPJ
	}
	if b[0] && b[1] && b[2] && oldCJ.Epoch+2 == ce {
		state.FinalizedCP = oldCJ
	}
	if b[0] && b[1] && oldCJ.Epoch+1 == ce {
		state.FinalizedCP = oldCJ
	}
}

// processInactivityUpdates adjusts per-validator inactivity scores.
// During normal finalization, scores decrease. During a leak, scores
// increase for non-target attesters.
func (et *EpochTransition) processInactivityUpdates(
	state *EpochTransitionState, inLeak bool,
) {
	if state.InactivityScores == nil {
		state.InactivityScores = make([]uint64, len(state.Validators))
	}
	for len(state.InactivityScores) < len(state.Validators) {
		state.InactivityScores = append(state.InactivityScores, 0)
	}

	pe := state.PreviousEpoch()
	part := state.Participation
	if part == nil {
		part = NewEpochTransitionParticipation()
	}

	for i, v := range state.Validators {
		idx := ValidatorIndex(i)
		if !v.IsActiveV2(pe) {
			continue
		}
		targetAttested := part.PrevTarget[idx] && !v.Slashed

		if targetAttested {
			// Decrease inactivity score during normal operation.
			if state.InactivityScores[i] > ETInactivityScoreRecoveryRate {
				state.InactivityScores[i] -= ETInactivityScoreRecoveryRate
			} else {
				state.InactivityScores[i] = 0
			}
		} else if inLeak {
			// Increase inactivity score during leak.
			state.InactivityScores[i] += ETInactivityScoreBias
		}
	}
}

// processRewardsAndPenalties computes attestation rewards for source,
// target, and head votes, plus inactivity penalties with per-validator
// inactivity scores (Altair style).
func (et *EpochTransition) processRewardsAndPenalties(
	state *EpochTransitionState, result *EpochTransitionResult,
) {
	ce := state.CurrentEpoch()
	pe := state.PreviousEpoch()
	tab := et.totalActiveBalance(state, ce)
	sqrtTab := et.isqrt(tab)
	if sqrtTab == 0 {
		return
	}

	inLeak := result.InLeakMode
	part := state.Participation
	if part == nil {
		part = NewEpochTransitionParticipation()
	}

	srcBal := et.participatingBalance(state, part.PrevSource, pe)
	tgtBal := et.participatingBalance(state, part.PrevTarget, pe)
	hdBal := et.participatingBalance(state, part.PrevHead, pe)

	inc := EffectiveBalanceIncrement

	for i, v := range state.Validators {
		idx := ValidatorIndex(i)
		if !v.IsActiveV2(pe) {
			continue
		}
		baseReward := v.EffectiveBalance * RCBaseRewardFactor / sqrtTab
		var netDelta int64

		// Source component (weight 14/64 in Altair).
		srcR := baseReward * AltairSourceWeight / WeightDenominatorAltair
		if part.PrevSource[idx] && !v.Slashed {
			if inLeak {
				netDelta += int64(srcR)
			} else if tab > 0 {
				netDelta += int64(srcR * (srcBal / inc) / (tab / inc))
			}
		} else {
			netDelta -= int64(srcR)
		}

		// Target component (weight 26/64).
		tgtR := baseReward * AltairTargetWeight / WeightDenominatorAltair
		if part.PrevTarget[idx] && !v.Slashed {
			if inLeak {
				netDelta += int64(tgtR)
			} else if tab > 0 {
				netDelta += int64(tgtR * (tgtBal / inc) / (tab / inc))
			}
		} else {
			netDelta -= int64(tgtR)
		}

		// Head component (weight 14/64). No penalty for non-attestation.
		hdR := baseReward * AltairHeadWeight / WeightDenominatorAltair
		if part.PrevHead[idx] && !v.Slashed {
			if inLeak {
				netDelta += int64(hdR)
			} else if tab > 0 {
				netDelta += int64(hdR * (hdBal / inc) / (tab / inc))
			}
		}

		// Inactivity penalty using per-validator inactivity score.
		if inLeak && !(part.PrevTarget[idx] && !v.Slashed) {
			inactScore := uint64(0)
			if i < len(state.InactivityScores) {
				inactScore = state.InactivityScores[i]
			}
			penalty := v.EffectiveBalance * inactScore / ETInactivityPenaltyQuotientBellatrix
			netDelta -= int64(penalty)
		}

		result.Rewards[i] = netDelta
		et.applyDelta(&state.Balances[i], netDelta)
	}
}

// processRegistryUpdates handles activation eligibility, activation queue,
// and ejection of low-balance validators.
func (et *EpochTransition) processRegistryUpdates(
	state *EpochTransitionState, result *EpochTransitionResult,
) {
	ce := state.CurrentEpoch()

	// Mark activation eligibility and eject low-balance validators.
	for i, v := range state.Validators {
		if v.ActivationEligibilityEpoch == FarFutureEpoch &&
			v.EffectiveBalance >= MinActivationBalance {
			state.Validators[i].ActivationEligibilityEpoch = ce + 1
		}
		if v.IsActiveV2(ce) && v.EffectiveBalance <= EjectionBalance &&
			v.ExitEpoch == FarFutureEpoch {
			exitEp := ce + 1 + Epoch(MaxSeedLookahead)
			state.Validators[i].ExitEpoch = exitEp
			state.Validators[i].WithdrawableEpoch = exitEp + Epoch(MinValidatorWithdrawDelay)
			result.Ejected = append(result.Ejected, ValidatorIndex(i))
		}
	}

	// Process activation queue.
	churn := et.computeChurnLimit(state, ce)
	var activated uint64
	for i, v := range state.Validators {
		if activated >= churn {
			break
		}
		if v.ActivationEligibilityEpoch <= state.FinalizedCP.Epoch &&
			v.ActivationEpoch == FarFutureEpoch {
			state.Validators[i].ActivationEpoch = ce + 1 + Epoch(MaxSeedLookahead)
			result.Activated = append(result.Activated, ValidatorIndex(i))
			activated++
		}
	}
}

// processSlashings applies proportional slashing penalties. Validators
// slashed at (epoch + EPOCHS_PER_SLASHINGS_VECTOR / 2 == withdrawable)
// receive an additional penalty proportional to the total slashed amount.
func (et *EpochTransition) processSlashings(
	state *EpochTransitionState, result *EpochTransitionResult,
) {
	ce := state.CurrentEpoch()
	tab := et.totalActiveBalance(state, ce)
	if tab == 0 {
		return
	}

	var totalSlashed uint64
	for _, s := range state.Slashings {
		totalSlashed += s
	}

	adj := totalSlashed * ETProportionalSlashingMultiplierBellatrix
	if adj > tab {
		adj = tab
	}
	inc := EffectiveBalanceIncrement

	for i, v := range state.Validators {
		if !v.Slashed {
			continue
		}
		halfVector := Epoch(EpochsPerSlashingsVector / 2)
		if ce+halfVector != v.WithdrawableEpoch {
			continue
		}
		penalty := v.EffectiveBalance / inc * adj / tab * inc
		et.decreaseBalance(&state.Balances[i], penalty)
		result.Slashed = append(result.Slashed, ValidatorIndex(i))
	}
}

// processEffectiveBalanceUpdates recomputes effective balances with
// hysteresis to prevent oscillation.
func (et *EpochTransition) processEffectiveBalanceUpdates(state *EpochTransitionState) {
	halfInc := EffectiveBalanceIncrement / HysteresisQuotient
	downThresh := halfInc * HysteresisDownwardMultiplier
	upThresh := halfInc * HysteresisUpwardMultiplier

	for i, v := range state.Validators {
		bal := state.Balances[i]
		if bal+downThresh < v.EffectiveBalance || v.EffectiveBalance+upThresh < bal {
			eb := bal - (bal % EffectiveBalanceIncrement)
			if eb > MaxEffectiveBalance {
				eb = MaxEffectiveBalance
			}
			state.Validators[i].EffectiveBalance = eb
		}
	}
}

// totalActiveBalance returns the total effective balance of active validators.
func (et *EpochTransition) totalActiveBalance(state *EpochTransitionState, epoch Epoch) uint64 {
	var total uint64
	for _, v := range state.Validators {
		if v.IsActiveV2(epoch) {
			total += v.EffectiveBalance
		}
	}
	if total < EffectiveBalanceIncrement {
		return EffectiveBalanceIncrement
	}
	return total
}

// targetAttestingBalance returns the total effective balance of unslashed
// validators that attested to the target in the previous epoch.
func (et *EpochTransition) targetAttestingBalance(
	state *EpochTransitionState, epoch Epoch,
) uint64 {
	part := state.Participation
	if part == nil {
		return 0
	}
	var total uint64
	for i, v := range state.Validators {
		if v.IsActiveV2(epoch) && !v.Slashed && part.PrevTarget[ValidatorIndex(i)] {
			total += v.EffectiveBalance
		}
	}
	return total
}

// currentTargetAttestingBalance returns the total effective balance of
// unslashed validators that attested to the target in the current epoch.
func (et *EpochTransition) currentTargetAttestingBalance(
	state *EpochTransitionState, epoch Epoch,
) uint64 {
	part := state.Participation
	if part == nil {
		return 0
	}
	var total uint64
	for i, v := range state.Validators {
		if v.IsActiveV2(epoch) && !v.Slashed && part.CurrTarget[ValidatorIndex(i)] {
			total += v.EffectiveBalance
		}
	}
	return total
}

// participatingBalance returns total effective balance of unslashed active
// validators flagged in the given map.
func (et *EpochTransition) participatingBalance(
	state *EpochTransitionState, flags map[ValidatorIndex]bool, epoch Epoch,
) uint64 {
	var total uint64
	for i, v := range state.Validators {
		if v.IsActiveV2(epoch) && !v.Slashed && flags[ValidatorIndex(i)] {
			total += v.EffectiveBalance
		}
	}
	return total
}

// checkpointForEpoch builds a Checkpoint for the given epoch, using
// BlockRoots if available, otherwise a zero root.
func (et *EpochTransition) checkpointForEpoch(
	state *EpochTransitionState, epoch Epoch,
) Checkpoint {
	idx := int(epoch) % len(state.BlockRoots)
	if idx >= 0 && idx < len(state.BlockRoots) {
		return Checkpoint{Epoch: epoch, Root: state.BlockRoots[idx].Root}
	}
	return Checkpoint{Epoch: epoch}
}

// computeFinalityDelay returns the number of epochs since last finalization.
func (et *EpochTransition) computeFinalityDelay(state *EpochTransitionState) uint64 {
	pe := uint64(state.PreviousEpoch())
	fe := uint64(state.FinalizedCP.Epoch)
	if pe <= fe {
		return 0
	}
	return pe - fe
}

// computeChurnLimit returns the per-epoch activation/exit churn limit.
func (et *EpochTransition) computeChurnLimit(state *EpochTransitionState, epoch Epoch) uint64 {
	var active uint64
	for _, v := range state.Validators {
		if v.IsActiveV2(epoch) {
			active++
		}
	}
	churn := active / ChurnLimitQuotient
	if churn < MinPerEpochChurnLimit {
		return MinPerEpochChurnLimit
	}
	return churn
}

func (et *EpochTransition) applyDelta(balance *uint64, delta int64) {
	if delta >= 0 {
		*balance += uint64(delta)
	} else {
		d := uint64(-delta)
		if d > *balance {
			*balance = 0
		} else {
			*balance -= d
		}
	}
}

func (et *EpochTransition) decreaseBalance(balance *uint64, amount uint64) {
	if amount >= *balance {
		*balance = 0
	} else {
		*balance -= amount
	}
}

// isqrt computes the integer square root using Newton's method.
func (et *EpochTransition) isqrt(n uint64) uint64 {
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
