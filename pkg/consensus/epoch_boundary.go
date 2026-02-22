// epoch_boundary.go implements epoch boundary processing for the consensus
// layer. Computes epoch transitions, processes pending attestations, updates
// effective balances, handles validator activations/exits, computes
// participation rates, and generates epoch summary reports. Complements
// epoch_transition.go and epoch_processor.go by providing a high-level
// orchestrator built on UnifiedBeaconState.
package consensus

import (
	"errors"
	"sync"
)

// Epoch boundary errors.
var (
	ErrEBNilState         = errors.New("epoch_boundary: nil state")
	ErrEBNoValidators     = errors.New("epoch_boundary: no validators")
	ErrEBNotAtBoundary    = errors.New("epoch_boundary: not at epoch boundary slot")
	ErrEBGenesisEpoch     = errors.New("epoch_boundary: cannot process genesis epoch")
	ErrEBAlreadyProcessed = errors.New("epoch_boundary: epoch already processed")
)

// Epoch boundary constants.
const (
	// EBActivationChurnLimit is the per-epoch activation queue limit.
	EBActivationChurnLimit uint64 = 8

	// EBEjectionThreshold is the balance below which validators are ejected.
	EBEjectionThreshold uint64 = 16 * GweiPerETH

	// EBEffBalHysteresisDown is the downward hysteresis threshold.
	EBEffBalHysteresisDown uint64 = EffectiveBalanceIncrement / 4

	// EBEffBalHysteresisUp is the upward hysteresis threshold.
	EBEffBalHysteresisUp uint64 = EffectiveBalanceIncrement * 5 / 4
)

// EpochBoundaryConfig configures the epoch boundary processor.
type EpochBoundaryConfig struct {
	SlotsPerEpoch       uint64
	ActivationChurn     uint64
	EjectionThreshold   uint64
	MaxEffectiveBalance uint64
}

// DefaultEpochBoundaryConfig returns default epoch boundary config.
func DefaultEpochBoundaryConfig() EpochBoundaryConfig {
	return EpochBoundaryConfig{
		SlotsPerEpoch:       32,
		ActivationChurn:     EBActivationChurnLimit,
		EjectionThreshold:   EBEjectionThreshold,
		MaxEffectiveBalance: MaxEffectiveBalance,
	}
}

// PendingAttestation is a simplified pending attestation for epoch processing.
type PendingAttestation struct {
	ValidatorIndex ValidatorIndex
	SourceEpoch    Epoch
	TargetEpoch    Epoch
	HeadCorrect    bool
}

// EpochSummary holds the results of epoch boundary processing.
type EpochSummary struct {
	Epoch              Epoch
	PreviousEpoch      Epoch
	TotalActiveBalance uint64
	TotalValidators    int
	ActiveValidators   int

	// Participation rates (0.0 to 1.0).
	SourceParticipation float64
	TargetParticipation float64
	HeadParticipation   float64

	// Validator lifecycle changes.
	Activated []ValidatorIndex
	Ejected   []ValidatorIndex
	Exited    []ValidatorIndex

	// Balance updates.
	EffBalUpdated int

	// Justification status.
	PrevJustified bool
	CurrJustified bool

	// Finalization.
	NewFinalizedEpoch Epoch
}

// EpochBoundaryProcessor handles epoch boundary state transitions.
// Thread-safe.
type EpochBoundaryProcessor struct {
	mu     sync.Mutex
	config EpochBoundaryConfig

	// Track which epochs have been processed to prevent double-processing.
	processedEpochs map[Epoch]bool
}

// NewEpochBoundaryProcessor creates a new epoch boundary processor.
func NewEpochBoundaryProcessor(cfg EpochBoundaryConfig) *EpochBoundaryProcessor {
	if cfg.SlotsPerEpoch == 0 {
		cfg.SlotsPerEpoch = 32
	}
	if cfg.ActivationChurn == 0 {
		cfg.ActivationChurn = EBActivationChurnLimit
	}
	if cfg.EjectionThreshold == 0 {
		cfg.EjectionThreshold = EBEjectionThreshold
	}
	if cfg.MaxEffectiveBalance == 0 {
		cfg.MaxEffectiveBalance = MaxEffectiveBalance
	}
	return &EpochBoundaryProcessor{
		config:          cfg,
		processedEpochs: make(map[Epoch]bool),
	}
}

// IsEpochBoundary returns true if the given slot is the last slot of an epoch.
func (ep *EpochBoundaryProcessor) IsEpochBoundary(slot uint64) bool {
	if ep.config.SlotsPerEpoch == 0 {
		return false
	}
	return (slot+1)%ep.config.SlotsPerEpoch == 0
}

// ProcessEpoch processes the epoch boundary for the given state and pending
// attestations. Returns a summary of changes applied.
func (ep *EpochBoundaryProcessor) ProcessEpoch(
	state *UnifiedBeaconState,
	pendingAttestations []*PendingAttestation,
) (*EpochSummary, error) {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	if state == nil {
		return nil, ErrEBNilState
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.Validators) == 0 {
		return nil, ErrEBNoValidators
	}

	currentEpoch := state.CurrentEpoch
	if currentEpoch == 0 {
		return nil, ErrEBGenesisEpoch
	}

	if ep.processedEpochs[currentEpoch] {
		return nil, ErrEBAlreadyProcessed
	}

	prevEpoch := currentEpoch - 1
	summary := &EpochSummary{
		Epoch:         currentEpoch,
		PreviousEpoch: prevEpoch,
	}

	// Count active validators and total active balance.
	for _, v := range state.Validators {
		summary.TotalValidators++
		if v.IsActiveAt(currentEpoch) {
			summary.ActiveValidators++
			summary.TotalActiveBalance += v.EffectiveBalance
		}
	}
	if summary.TotalActiveBalance < EffectiveBalanceIncrement {
		summary.TotalActiveBalance = EffectiveBalanceIncrement
	}

	// Process attestation participation.
	sourceCount, targetCount, headCount := ep.processAttestations(
		state, pendingAttestations, prevEpoch, currentEpoch,
	)
	if summary.ActiveValidators > 0 {
		summary.SourceParticipation = float64(sourceCount) / float64(summary.ActiveValidators)
		summary.TargetParticipation = float64(targetCount) / float64(summary.ActiveValidators)
		summary.HeadParticipation = float64(headCount) / float64(summary.ActiveValidators)
	}

	// Process justification.
	ep.processJustification(state, summary, targetCount)

	// Process finalization.
	ep.processFinalization(state, summary)

	// Process validator activations.
	summary.Activated = ep.processActivations(state, currentEpoch)

	// Process validator exits/ejections.
	summary.Ejected = ep.processEjections(state, currentEpoch)
	summary.Exited = ep.collectExited(state, currentEpoch)

	// Update effective balances.
	summary.EffBalUpdated = ep.updateEffectiveBalances(state)

	ep.processedEpochs[currentEpoch] = true
	return summary, nil
}

// processAttestations tallies source, target, and head attestation counts
// from pending attestations. Must be called with state lock held.
func (ep *EpochBoundaryProcessor) processAttestations(
	state *UnifiedBeaconState,
	atts []*PendingAttestation,
	prevEpoch, currentEpoch Epoch,
) (sourceCount, targetCount, headCount int) {
	sourceVoters := make(map[ValidatorIndex]bool)
	targetVoters := make(map[ValidatorIndex]bool)
	headVoters := make(map[ValidatorIndex]bool)

	for _, att := range atts {
		idx := att.ValidatorIndex
		if int(idx) >= len(state.Validators) {
			continue
		}
		v := state.Validators[idx]
		if v.Slashed || !v.IsActiveAt(prevEpoch) {
			continue
		}

		if att.TargetEpoch == prevEpoch || att.TargetEpoch == currentEpoch {
			if !sourceVoters[idx] {
				sourceVoters[idx] = true
				sourceCount++
			}
			if !targetVoters[idx] {
				targetVoters[idx] = true
				targetCount++
			}
			if att.HeadCorrect && !headVoters[idx] {
				headVoters[idx] = true
				headCount++
			}
		}
	}
	return
}

// processJustification checks if the target-attesting balance meets
// the 2/3 threshold and updates justification. Must hold state lock.
func (ep *EpochBoundaryProcessor) processJustification(
	state *UnifiedBeaconState,
	summary *EpochSummary,
	targetCount int,
) {
	// Simplified justification: if target participation >= 2/3,
	// mark the current epoch as justified.
	if summary.ActiveValidators > 0 {
		ratio := float64(targetCount) / float64(summary.ActiveValidators)
		if ratio >= 2.0/3.0 {
			summary.CurrJustified = true
			state.CurrentJustified = UnifiedCheckpoint{
				Epoch: summary.Epoch,
				Root:  state.BlockRoots[state.CurrentSlot%8192],
			}
		}
	}

	// Previous epoch justification.
	if summary.TargetParticipation >= 2.0/3.0 {
		summary.PrevJustified = true
		state.PreviousJustified = UnifiedCheckpoint{
			Epoch: summary.PreviousEpoch,
			Root:  state.BlockRoots[(state.CurrentSlot-1)%8192],
		}
	}
}

// processFinalization applies finalization rules. Must hold state lock.
func (ep *EpochBoundaryProcessor) processFinalization(
	state *UnifiedBeaconState,
	summary *EpochSummary,
) {
	ce := summary.Epoch
	// Rule: if both current and previous are justified, finalize previous.
	if summary.CurrJustified && summary.PrevJustified {
		if state.PreviousJustified.Epoch+1 == ce {
			state.FinalizedCheckpointU = state.PreviousJustified
			summary.NewFinalizedEpoch = state.PreviousJustified.Epoch
		}
	}
	// Also finalize if current justified is 2 epochs ahead of finalized.
	if summary.CurrJustified && state.CurrentJustified.Epoch > state.FinalizedCheckpointU.Epoch+1 {
		state.FinalizedCheckpointU = UnifiedCheckpoint{
			Epoch: state.CurrentJustified.Epoch - 1,
			Root:  state.CurrentJustified.Root,
		}
		if state.FinalizedCheckpointU.Epoch > summary.NewFinalizedEpoch {
			summary.NewFinalizedEpoch = state.FinalizedCheckpointU.Epoch
		}
	}
}

// processActivations activates eligible validators up to the churn limit.
// Must hold state lock.
func (ep *EpochBoundaryProcessor) processActivations(
	state *UnifiedBeaconState, epoch Epoch,
) []ValidatorIndex {
	var activated []ValidatorIndex
	var count uint64
	for i, v := range state.Validators {
		if count >= ep.config.ActivationChurn {
			break
		}
		if v.ActivationEpoch == FarFutureEpoch &&
			v.ActivationEligibilityEpoch != FarFutureEpoch &&
			v.ActivationEligibilityEpoch <= epoch &&
			v.EffectiveBalance >= MinActivationBalance &&
			!v.Slashed {
			state.Validators[i].ActivationEpoch = epoch + 1
			activated = append(activated, ValidatorIndex(i))
			count++
		}
	}
	return activated
}

// processEjections ejects validators with balance below the threshold.
// Must hold state lock.
func (ep *EpochBoundaryProcessor) processEjections(
	state *UnifiedBeaconState, epoch Epoch,
) []ValidatorIndex {
	var ejected []ValidatorIndex
	for i, v := range state.Validators {
		if v.IsActiveAt(epoch) && v.Balance <= ep.config.EjectionThreshold &&
			v.ExitEpoch == FarFutureEpoch {
			state.Validators[i].ExitEpoch = epoch + 1
			state.Validators[i].WithdrawableEpoch = epoch + 1 + 256
			ejected = append(ejected, ValidatorIndex(i))
		}
	}
	return ejected
}

// collectExited returns validators whose exit epoch has been reached.
func (ep *EpochBoundaryProcessor) collectExited(
	state *UnifiedBeaconState, epoch Epoch,
) []ValidatorIndex {
	var exited []ValidatorIndex
	for i, v := range state.Validators {
		if v.ExitEpoch != FarFutureEpoch && v.ExitEpoch <= epoch &&
			v.WithdrawableEpoch > epoch {
			exited = append(exited, ValidatorIndex(i))
		}
	}
	return exited
}

// updateEffectiveBalances recalculates effective balances with hysteresis.
// Must hold state lock. Returns the count of updated validators.
func (ep *EpochBoundaryProcessor) updateEffectiveBalances(
	state *UnifiedBeaconState,
) int {
	updated := 0
	for _, v := range state.Validators {
		bal := v.Balance
		effBal := v.EffectiveBalance

		if bal+EBEffBalHysteresisDown < effBal ||
			effBal+EBEffBalHysteresisUp < bal {
			newEff := (bal / EffectiveBalanceIncrement) * EffectiveBalanceIncrement
			if newEff > ep.config.MaxEffectiveBalance {
				newEff = ep.config.MaxEffectiveBalance
			}
			if newEff != effBal {
				v.EffectiveBalance = newEff
				updated++
			}
		}
	}
	return updated
}

// HasProcessed returns whether the given epoch has been processed.
func (ep *EpochBoundaryProcessor) HasProcessed(epoch Epoch) bool {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	return ep.processedEpochs[epoch]
}

// ProcessedCount returns the number of epochs that have been processed.
func (ep *EpochBoundaryProcessor) ProcessedCount() int {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	return len(ep.processedEpochs)
}

// Reset clears the processed epochs tracking.
func (ep *EpochBoundaryProcessor) Reset() {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	ep.processedEpochs = make(map[Epoch]bool)
}

// ComputeParticipationRate calculates overall participation from a summary.
func ComputeParticipationRate(summary *EpochSummary) float64 {
	if summary == nil || summary.ActiveValidators == 0 {
		return 0
	}
	return (summary.SourceParticipation +
		summary.TargetParticipation +
		summary.HeadParticipation) / 3.0
}
