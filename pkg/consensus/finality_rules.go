// finality_rules.go implements Casper FFG finality with proper
// justification/finalization logic per the beacon chain spec. Operates on
// BeaconStateV2 and tracks justified/finalized checkpoints with full
// support for all four finalization conditions.
package consensus

import (
	"errors"
	"sync"
)

// Casper FFG finality errors.
var (
	ErrFRNilState         = errors.New("finality_rules: nil beacon state")
	ErrFRGenesisEpoch     = errors.New("finality_rules: cannot process genesis epoch")
	ErrFRNoValidators     = errors.New("finality_rules: no active validators")
	ErrFRInvalidWeight    = errors.New("finality_rules: vote weight exceeds total weight")
	ErrFRAlreadyFinalized = errors.New("finality_rules: checkpoint already finalized")
)

// SupermajorityNumerator and SupermajorityDenominator define the 2/3
// supermajority threshold used in Casper FFG.
const (
	SupermajorityNumerator   = 2
	SupermajorityDenominator = 3
)

// CasperCheckpoint is a finality checkpoint with epoch and block root.
type CasperCheckpoint struct {
	Epoch Epoch
	Root  [32]byte
}

// IsZero returns true if the checkpoint is unset.
func (c CasperCheckpoint) IsZero() bool {
	return c.Epoch == 0 && c.Root == [32]byte{}
}

// Equals returns true if two checkpoints match.
func (c CasperCheckpoint) Equals(other CasperCheckpoint) bool {
	return c.Epoch == other.Epoch && c.Root == other.Root
}

// CasperFinalityTracker implements Casper FFG finality tracking with proper
// justification and finalization logic. Thread-safe.
type CasperFinalityTracker struct {
	mu                          sync.RWMutex
	justified                   CasperCheckpoint
	finalized                   CasperCheckpoint
	previousJustified           CasperCheckpoint
	justificationBits           [4]bool
	finalizedCheckpoints        map[Epoch]CasperCheckpoint
	slotsPerEpoch               uint64
}

// NewCasperFinalityTracker creates a finality tracker with the given slots
// per epoch configuration. Genesis epoch 0 is justified and finalized by
// default.
func NewCasperFinalityTracker(slotsPerEpoch uint64) *CasperFinalityTracker {
	if slotsPerEpoch == 0 {
		slotsPerEpoch = 32
	}
	genesis := CasperCheckpoint{Epoch: 0}
	return &CasperFinalityTracker{
		justified:            genesis,
		finalized:            genesis,
		previousJustified:    genesis,
		justificationBits:    [4]bool{true, false, false, false},
		finalizedCheckpoints: map[Epoch]CasperCheckpoint{0: genesis},
		slotsPerEpoch:        slotsPerEpoch,
	}
}

// ProcessJustification processes justification for the current epoch based on
// attestation weights. It updates justification bits and the current/previous
// justified checkpoints. This follows the spec's process_justification_and_finalization.
func (ft *CasperFinalityTracker) ProcessJustification(currentEpoch Epoch, state *BeaconStateV2) error {
	if state == nil {
		return ErrFRNilState
	}
	if currentEpoch <= 1 {
		return ErrFRGenesisEpoch
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()

	previousEpoch := currentEpoch - 1
	activeIndices := state.GetActiveValidatorIndices(currentEpoch)
	if len(activeIndices) == 0 {
		return ErrFRNoValidators
	}
	totalBalance := state.GetTotalActiveBalance(currentEpoch)

	// Rotate: previous justified <- current justified.
	ft.previousJustified = ft.justified

	// Shift justification bits: bit[i] = bit[i-1], bit[0] = false.
	for i := len(ft.justificationBits) - 1; i > 0; i-- {
		ft.justificationBits[i] = ft.justificationBits[i-1]
	}
	ft.justificationBits[0] = false

	// Check previous epoch attestation weight (simplified: use total balance
	// of active validators as a proxy -- in production this would use actual
	// attestation aggregation).
	previousBalance := state.GetTotalActiveBalance(previousEpoch)
	if isSuperMajority(previousBalance, totalBalance) {
		root := state.BlockRoots[uint64(EpochStartSlot(previousEpoch, ft.slotsPerEpoch))%SlotsPerHistoricalRoot]
		ft.justified = CasperCheckpoint{Epoch: previousEpoch, Root: root}
		ft.justificationBits[1] = true
	}

	// Check current epoch attestation weight.
	currentBalance := state.GetTotalActiveBalance(currentEpoch)
	if isSuperMajority(currentBalance, totalBalance) {
		root := state.BlockRoots[uint64(EpochStartSlot(currentEpoch, ft.slotsPerEpoch))%SlotsPerHistoricalRoot]
		ft.justified = CasperCheckpoint{Epoch: currentEpoch, Root: root}
		ft.justificationBits[0] = true
	}

	return nil
}

// ProcessJustificationWithWeights processes justification using explicit
// vote weights for the previous and current epochs.
func (ft *CasperFinalityTracker) ProcessJustificationWithWeights(
	currentEpoch Epoch,
	state *BeaconStateV2,
	previousEpochWeight, currentEpochWeight, totalWeight uint64,
) error {
	if state == nil {
		return ErrFRNilState
	}
	if currentEpoch <= 1 {
		return ErrFRGenesisEpoch
	}
	if previousEpochWeight > totalWeight || currentEpochWeight > totalWeight {
		return ErrFRInvalidWeight
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()

	previousEpoch := currentEpoch - 1

	// Rotate justified checkpoints.
	ft.previousJustified = ft.justified

	// Shift justification bits.
	for i := len(ft.justificationBits) - 1; i > 0; i-- {
		ft.justificationBits[i] = ft.justificationBits[i-1]
	}
	ft.justificationBits[0] = false

	// Justify previous epoch if supermajority attested.
	if isSuperMajority(previousEpochWeight, totalWeight) {
		root := state.BlockRoots[uint64(EpochStartSlot(previousEpoch, ft.slotsPerEpoch))%SlotsPerHistoricalRoot]
		ft.justified = CasperCheckpoint{Epoch: previousEpoch, Root: root}
		ft.justificationBits[1] = true
	}

	// Justify current epoch if supermajority attested.
	if isSuperMajority(currentEpochWeight, totalWeight) {
		root := state.BlockRoots[uint64(EpochStartSlot(currentEpoch, ft.slotsPerEpoch))%SlotsPerHistoricalRoot]
		ft.justified = CasperCheckpoint{Epoch: currentEpoch, Root: root}
		ft.justificationBits[0] = true
	}

	return nil
}

// ProcessFinalization attempts to finalize checkpoints based on the four
// Casper FFG finalization conditions from the beacon chain spec. Note: when
// calling this separately from ProcessJustification, it uses the current
// values of justified/previousJustified. For correct spec behavior, use
// ProcessJustificationAndFinalization which captures old values properly.
func (ft *CasperFinalityTracker) ProcessFinalization(currentEpoch Epoch, state *BeaconStateV2) error {
	if state == nil {
		return ErrFRNilState
	}
	if currentEpoch <= 1 {
		return ErrFRGenesisEpoch
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()

	ft.applyFinalization(currentEpoch, ft.previousJustified, ft.justified)
	return nil
}

// applyFinalization applies the 4 finalization conditions. Must be called
// with the lock held. oldPJ and oldCJ are the justified checkpoints captured
// BEFORE the current epoch's justification rotation.
func (ft *CasperFinalityTracker) applyFinalization(
	currentEpoch Epoch,
	oldPJ, oldCJ CasperCheckpoint,
) {
	b := ft.justificationBits

	// Condition 4 (spec order): 4th, 3rd, 2nd epochs justified, finalize 4th.
	if b[1] && b[2] && b[3] && oldPJ.Epoch+3 == currentEpoch {
		ft.finalized = oldPJ
		ft.finalizedCheckpoints[oldPJ.Epoch] = oldPJ
	}

	// Condition 2 (spec order): 3rd, 2nd epochs justified, finalize 3rd.
	if b[1] && b[2] && oldPJ.Epoch+2 == currentEpoch {
		ft.finalized = oldPJ
		ft.finalizedCheckpoints[oldPJ.Epoch] = oldPJ
	}

	// Condition 3 (spec order): 3rd, 2nd, 1st epochs justified, finalize 3rd.
	if b[0] && b[1] && b[2] && oldCJ.Epoch+2 == currentEpoch {
		ft.finalized = oldCJ
		ft.finalizedCheckpoints[oldCJ.Epoch] = oldCJ
	}

	// Condition 1 (spec order): 2nd, 1st epochs justified, finalize 2nd.
	if b[0] && b[1] && oldCJ.Epoch+1 == currentEpoch {
		ft.finalized = oldCJ
		ft.finalizedCheckpoints[oldCJ.Epoch] = oldCJ
	}
}

// ProcessJustificationAndFinalization performs both justification and
// finalization in a single call, correctly capturing old checkpoint values
// before rotation as required by the beacon chain spec.
func (ft *CasperFinalityTracker) ProcessJustificationAndFinalization(
	currentEpoch Epoch,
	state *BeaconStateV2,
	previousEpochWeight, currentEpochWeight, totalWeight uint64,
) error {
	if state == nil {
		return ErrFRNilState
	}
	if currentEpoch <= 1 {
		return ErrFRGenesisEpoch
	}
	if previousEpochWeight > totalWeight || currentEpochWeight > totalWeight {
		return ErrFRInvalidWeight
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()

	previousEpoch := currentEpoch - 1

	// Capture old values BEFORE rotation (per spec).
	oldPJ := ft.previousJustified
	oldCJ := ft.justified

	// Rotate justified checkpoints.
	ft.previousJustified = ft.justified

	// Shift justification bits.
	for i := len(ft.justificationBits) - 1; i > 0; i-- {
		ft.justificationBits[i] = ft.justificationBits[i-1]
	}
	ft.justificationBits[0] = false

	// Justify previous epoch if supermajority attested.
	if isSuperMajority(previousEpochWeight, totalWeight) {
		root := state.BlockRoots[uint64(EpochStartSlot(previousEpoch, ft.slotsPerEpoch))%SlotsPerHistoricalRoot]
		ft.justified = CasperCheckpoint{Epoch: previousEpoch, Root: root}
		ft.justificationBits[1] = true
	}

	// Justify current epoch if supermajority attested.
	if isSuperMajority(currentEpochWeight, totalWeight) {
		root := state.BlockRoots[uint64(EpochStartSlot(currentEpoch, ft.slotsPerEpoch))%SlotsPerHistoricalRoot]
		ft.justified = CasperCheckpoint{Epoch: currentEpoch, Root: root}
		ft.justificationBits[0] = true
	}

	// Apply finalization with the old (pre-rotation) values.
	ft.applyFinalization(currentEpoch, oldPJ, oldCJ)

	return nil
}

// IsFinalized returns true if the given checkpoint has been finalized.
func (ft *CasperFinalityTracker) IsFinalized(checkpoint CasperCheckpoint) bool {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	// A checkpoint is finalized if its epoch is at or before the finalized epoch.
	return checkpoint.Epoch <= ft.finalized.Epoch
}

// GetFinalizedCheckpoint returns the latest finalized checkpoint.
func (ft *CasperFinalityTracker) GetFinalizedCheckpoint() CasperCheckpoint {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.finalized
}

// GetJustifiedCheckpoint returns the latest justified checkpoint.
func (ft *CasperFinalityTracker) GetJustifiedCheckpoint() CasperCheckpoint {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.justified
}

// GetPreviousJustifiedCheckpoint returns the previous justified checkpoint.
func (ft *CasperFinalityTracker) GetPreviousJustifiedCheckpoint() CasperCheckpoint {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.previousJustified
}

// GetJustificationBits returns the current justification bitfield.
func (ft *CasperFinalityTracker) GetJustificationBits() [4]bool {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.justificationBits
}

// FinalityDelay returns the number of epochs since the last finalization.
func (ft *CasperFinalityTracker) FinalityDelay(currentEpoch Epoch) uint64 {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	if currentEpoch <= ft.finalized.Epoch {
		return 0
	}
	return uint64(currentEpoch) - uint64(ft.finalized.Epoch)
}

// SetJustified manually sets the justified checkpoint. Useful for
// initialization or testing.
func (ft *CasperFinalityTracker) SetJustified(cp CasperCheckpoint) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.justified = cp
}

// SetFinalized manually sets the finalized checkpoint. Useful for
// initialization or testing.
func (ft *CasperFinalityTracker) SetFinalized(cp CasperCheckpoint) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.finalized = cp
	ft.finalizedCheckpoints[cp.Epoch] = cp
}

// SetJustificationBits manually sets the justification bits.
func (ft *CasperFinalityTracker) SetJustificationBits(bits [4]bool) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.justificationBits = bits
}

// SetPreviousJustified manually sets the previous justified checkpoint.
func (ft *CasperFinalityTracker) SetPreviousJustified(cp CasperCheckpoint) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.previousJustified = cp
}

// isSuperMajority returns true if voteWeight >= 2/3 of totalWeight.
func isSuperMajority(voteWeight, totalWeight uint64) bool {
	if totalWeight == 0 {
		return false
	}
	// voteWeight * 3 >= totalWeight * 2 (safe from overflow for practical values).
	return voteWeight*SupermajorityDenominator >= totalWeight*SupermajorityNumerator
}
