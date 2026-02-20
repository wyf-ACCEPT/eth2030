// epoch_processor.go implements epoch boundary processing per the Ethereum
// beacon chain spec (phase0).
package consensus

import (
	"errors"
	"math"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Epoch processor spec constants.
const (
	epBaseRewardFactor          uint64 = 64
	epBaseRewardsPerEpoch       uint64 = 4
	epMinEpochsToInactivityPen  uint64 = 4
	epInactivityPenaltyQuotient uint64 = 1 << 26
	epPropSlashingMultiplier    uint64 = 1
	epEpochsPerSlashingsVector  uint64 = 8192
	epEjectionBalance           uint64 = 16_000_000_000
	epMinPerEpochChurnLimit     uint64 = 4
	epChurnLimitQuotient        uint64 = 65536
	epMaxSeedLookahead          uint64 = 4
	epMinValWithdrawDelay       uint64 = 256
	epEpochsPerHistoricalVector uint64 = 65536
	epSlotsPerHistoricalRoot    uint64 = 8192
)

var (
	ErrEPNilState        = errors.New("epoch_processor: nil state")
	ErrEPNoValidators    = errors.New("epoch_processor: no validators in state")
	ErrEPEarlyEpoch      = errors.New("epoch_processor: too early for epoch processing")
	ErrEPBalanceMismatch = errors.New("epoch_processor: validator/balance length mismatch")
)

// EpochParticipation tracks which validators attested to source, target, and
// head during an epoch.
type EpochParticipation struct {
	SourceAttested map[uint64]bool
	TargetAttested map[uint64]bool
	HeadAttested   map[uint64]bool
}

// NewEpochParticipation creates an empty participation tracker.
func NewEpochParticipation() *EpochParticipation {
	return &EpochParticipation{
		SourceAttested: make(map[uint64]bool),
		TargetAttested: make(map[uint64]bool),
		HeadAttested:   make(map[uint64]bool),
	}
}

// EpochProcessorState holds all fields needed for epoch transition processing.
type EpochProcessorState struct {
	Slot          uint64
	SlotsPerEpoch uint64
	Validators    []*ValidatorV2
	Balances      []uint64
	Slashings     [epEpochsPerSlashingsVector]uint64
	RandaoMixes   [epEpochsPerHistoricalVector]types.Hash

	JustificationBits           [4]bool
	PreviousJustifiedCheckpoint Checkpoint
	CurrentJustifiedCheckpoint  Checkpoint
	FinalizedCheckpoint         Checkpoint

	BlockRoots      [epSlotsPerHistoricalRoot]types.Hash
	HistoricalRoots []types.Hash

	PreviousEpochParticipation *EpochParticipation
	CurrentEpochParticipation  *EpochParticipation
}

// CurrentEpoch returns the epoch for the current slot.
func (s *EpochProcessorState) CurrentEpoch() Epoch {
	if s.SlotsPerEpoch == 0 {
		return 0
	}
	return Epoch(s.Slot / s.SlotsPerEpoch)
}

// PreviousEpoch returns the previous epoch, clamped to 0.
func (s *EpochProcessorState) PreviousEpoch() Epoch {
	if ce := s.CurrentEpoch(); ce > 0 {
		return ce - 1
	}
	return 0
}

// EpochProcessor performs epoch boundary state transitions. Thread-safe.
type EpochProcessor struct{ mu sync.Mutex }

// NewEpochProcessor creates a new epoch processor.
func NewEpochProcessor() *EpochProcessor { return &EpochProcessor{} }

// ProcessEpochTransition runs the full epoch transition on the given state.
func (ep *EpochProcessor) ProcessEpochTransition(state *EpochProcessorState) error {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	if state == nil {
		return ErrEPNilState
	}
	if len(state.Validators) == 0 {
		return ErrEPNoValidators
	}
	if len(state.Validators) != len(state.Balances) {
		return ErrEPBalanceMismatch
	}
	ep.processJustificationAndFinalization(state)
	ep.processRewardsAndPenalties(state)
	ep.processRegistryUpdates(state)
	ep.processSlashings(state)
	ep.processEffectiveBalanceUpdates(state)
	ep.processSlashingsReset(state)
	ep.processRandaoMixesReset(state)
	ep.processHistoricalRootsUpdate(state)
	ep.processParticipationRotation(state)
	return nil
}

// processJustificationAndFinalization implements Casper FFG justification and
// finalization rules.
func (ep *EpochProcessor) processJustificationAndFinalization(state *EpochProcessorState) {
	ce := state.CurrentEpoch()
	if ce <= 1 {
		return
	}
	pe := state.PreviousEpoch()
	oldPJ, oldCJ := state.PreviousJustifiedCheckpoint, state.CurrentJustifiedCheckpoint

	state.PreviousJustifiedCheckpoint = state.CurrentJustifiedCheckpoint
	for i := 3; i > 0; i-- {
		state.JustificationBits[i] = state.JustificationBits[i-1]
	}
	state.JustificationBits[0] = false

	tab := ep.totalActiveBalance(state, ce)

	if ep.partBalance(state, state.PreviousEpochParticipation, 1)*3 >= tab*2 {
		slot := uint64(pe) * state.SlotsPerEpoch
		state.CurrentJustifiedCheckpoint = Checkpoint{pe, state.BlockRoots[slot%epSlotsPerHistoricalRoot]}
		state.JustificationBits[1] = true
	}
	if ep.partBalance(state, state.CurrentEpochParticipation, 1)*3 >= tab*2 {
		slot := uint64(ce) * state.SlotsPerEpoch
		state.CurrentJustifiedCheckpoint = Checkpoint{ce, state.BlockRoots[slot%epSlotsPerHistoricalRoot]}
		state.JustificationBits[0] = true
	}

	b := state.JustificationBits
	if b[1] && b[2] && b[3] && oldPJ.Epoch+3 == ce {
		state.FinalizedCheckpoint = oldPJ
	}
	if b[1] && b[2] && oldPJ.Epoch+2 == ce {
		state.FinalizedCheckpoint = oldPJ
	}
	if b[0] && b[1] && b[2] && oldCJ.Epoch+2 == ce {
		state.FinalizedCheckpoint = oldCJ
	}
	if b[0] && b[1] && oldCJ.Epoch+1 == ce {
		state.FinalizedCheckpoint = oldCJ
	}
}

// decreaseBal safely subtracts delta from balance, flooring at zero.
func decreaseBal(bal *uint64, delta uint64) {
	if *bal >= delta {
		*bal -= delta
	} else {
		*bal = 0
	}
}

// processRewardsAndPenalties computes attestation rewards/penalties for source,
// target, and head votes, plus inactivity penalties.
func (ep *EpochProcessor) processRewardsAndPenalties(state *EpochProcessorState) {
	ce := state.CurrentEpoch()
	if ce == 0 {
		return
	}
	pe := state.PreviousEpoch()
	tab := ep.totalActiveBalance(state, ce)
	sq := ep.intSqrt(tab)
	if sq == 0 {
		return
	}
	finDelay := ep.finalityDelay(state)
	inLeak := finDelay > epMinEpochsToInactivityPen

	pp := state.PreviousEpochParticipation
	if pp == nil {
		pp = NewEpochParticipation()
	}
	srcBal := ep.partBalance(state, pp, 0)
	tgtBal := ep.partBalance(state, pp, 1)
	hdBal := ep.partBalance(state, pp, 2)
	inc := EffectiveBalanceIncrement

	for i, v := range state.Validators {
		idx := uint64(i)
		if !v.IsActiveV2(pe) {
			continue
		}
		br := v.EffectiveBalance * epBaseRewardFactor / sq / epBaseRewardsPerEpoch

		// Process source, target, head components.
		for _, comp := range []struct {
			attested bool
			compBal  uint64
		}{
			{pp.SourceAttested[idx] && !v.Slashed, srcBal},
			{pp.TargetAttested[idx] && !v.Slashed, tgtBal},
			{pp.HeadAttested[idx] && !v.Slashed, hdBal},
		} {
			if comp.attested {
				if inLeak {
					state.Balances[i] += br
				} else {
					state.Balances[i] += br * (comp.compBal / inc) / (tab / inc)
				}
			} else {
				decreaseBal(&state.Balances[i], br)
			}
		}

		// Inactivity penalty for non-target attesters during finality leak.
		if inLeak && !(pp.TargetAttested[idx] && !v.Slashed) {
			decreaseBal(&state.Balances[i], v.EffectiveBalance*finDelay/epInactivityPenaltyQuotient)
		}
	}
}

// processRegistryUpdates handles activation eligibility, ejection, and
// activation queue processing.
func (ep *EpochProcessor) processRegistryUpdates(state *EpochProcessorState) {
	ce := state.CurrentEpoch()
	for i, v := range state.Validators {
		if v.ActivationEligibilityEpoch == FarFutureEpoch && v.EffectiveBalance >= MinActivationBalance {
			state.Validators[i].ActivationEligibilityEpoch = ce + 1
		}
		if v.IsActiveV2(ce) && v.EffectiveBalance <= epEjectionBalance && v.ExitEpoch == FarFutureEpoch {
			ex := ce + 1 + Epoch(epMaxSeedLookahead)
			state.Validators[i].ExitEpoch = ex
			state.Validators[i].WithdrawableEpoch = ex + Epoch(epMinValWithdrawDelay)
		}
	}
	churn := ep.churnLimit(state, ce)
	var activated uint64
	for i, v := range state.Validators {
		if activated >= churn {
			break
		}
		if v.ActivationEligibilityEpoch <= state.FinalizedCheckpoint.Epoch &&
			v.ActivationEpoch == FarFutureEpoch {
			state.Validators[i].ActivationEpoch = ce + 1 + Epoch(epMaxSeedLookahead)
			activated++
		}
	}
}

// processSlashings applies effective-balance-proportional slashing penalties.
func (ep *EpochProcessor) processSlashings(state *EpochProcessorState) {
	ce := state.CurrentEpoch()
	tb := ep.totalActiveBalance(state, ce)
	var ts uint64
	for _, s := range state.Slashings {
		ts += s
	}
	adj := ts * epPropSlashingMultiplier
	if adj > tb {
		adj = tb
	}
	inc := EffectiveBalanceIncrement
	for i, v := range state.Validators {
		if v.Slashed && ce+Epoch(epEpochsPerSlashingsVector/2) == v.WithdrawableEpoch {
			pen := v.EffectiveBalance / inc * adj / tb * inc
			decreaseBal(&state.Balances[i], pen)
		}
	}
}

// processEffectiveBalanceUpdates recomputes effective balances with hysteresis.
func (ep *EpochProcessor) processEffectiveBalanceUpdates(state *EpochProcessorState) {
	hi := EffectiveBalanceIncrement / HysteresisQuotient
	down, up := hi*HysteresisDownwardMultiplier, hi*HysteresisUpwardMultiplier
	for i, v := range state.Validators {
		bal := state.Balances[i]
		if bal+down < v.EffectiveBalance || v.EffectiveBalance+up < bal {
			eb := bal - (bal % EffectiveBalanceIncrement)
			if eb > MaxEffectiveBalance {
				eb = MaxEffectiveBalance
			}
			state.Validators[i].EffectiveBalance = eb
		}
	}
}

// processSlashingsReset zeroes the slashings entry for the next epoch.
func (ep *EpochProcessor) processSlashingsReset(state *EpochProcessorState) {
	state.Slashings[uint64(state.CurrentEpoch()+1)%epEpochsPerSlashingsVector] = 0
}

// processRandaoMixesReset copies current RANDAO mix to next epoch slot.
func (ep *EpochProcessor) processRandaoMixesReset(state *EpochProcessorState) {
	ce := uint64(state.CurrentEpoch())
	state.RandaoMixes[(ce+1)%epEpochsPerHistoricalVector] = state.RandaoMixes[ce%epEpochsPerHistoricalVector]
}

// processHistoricalRootsUpdate accumulates a historical root at boundaries.
func (ep *EpochProcessor) processHistoricalRootsUpdate(state *EpochProcessorState) {
	if state.SlotsPerEpoch == 0 {
		return
	}
	epPerHist := epSlotsPerHistoricalRoot / state.SlotsPerEpoch
	if epPerHist == 0 {
		return
	}
	if uint64(state.CurrentEpoch()+1)%epPerHist == 0 {
		var combined types.Hash
		for i := range state.BlockRoots {
			for j := range combined {
				combined[j] ^= state.BlockRoots[i][j]
			}
		}
		state.HistoricalRoots = append(state.HistoricalRoots, combined)
	}
}

// processParticipationRotation rotates participation records.
func (ep *EpochProcessor) processParticipationRotation(state *EpochProcessorState) {
	state.PreviousEpochParticipation = state.CurrentEpochParticipation
	state.CurrentEpochParticipation = NewEpochParticipation()
}

// totalActiveBalance returns total effective balance of active validators.
// Returns at least EffectiveBalanceIncrement to avoid division by zero.
func (ep *EpochProcessor) totalActiveBalance(state *EpochProcessorState, epoch Epoch) uint64 {
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

// partBalance returns the total effective balance of unslashed validators
// that participated. kind: 0=source, 1=target, 2=head.
func (ep *EpochProcessor) partBalance(state *EpochProcessorState, part *EpochParticipation, kind int) uint64 {
	if part == nil {
		return 0
	}
	var m map[uint64]bool
	switch kind {
	case 0:
		m = part.SourceAttested
	case 1:
		m = part.TargetAttested
	default:
		m = part.HeadAttested
	}
	var total uint64
	for i, v := range state.Validators {
		if m[uint64(i)] && !v.Slashed {
			total += v.EffectiveBalance
		}
	}
	return total
}

func (ep *EpochProcessor) finalityDelay(state *EpochProcessorState) uint64 {
	pe, fe := uint64(state.PreviousEpoch()), uint64(state.FinalizedCheckpoint.Epoch)
	if pe <= fe {
		return 0
	}
	return pe - fe
}

func (ep *EpochProcessor) churnLimit(state *EpochProcessorState, epoch Epoch) uint64 {
	var n uint64
	for _, v := range state.Validators {
		if v.IsActiveV2(epoch) {
			n++
		}
	}
	if c := n / epChurnLimitQuotient; c >= epMinPerEpochChurnLimit {
		return c
	}
	return epMinPerEpochChurnLimit
}

// intSqrt computes the integer square root using Newton's method.
func (ep *EpochProcessor) intSqrt(n uint64) uint64 {
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
