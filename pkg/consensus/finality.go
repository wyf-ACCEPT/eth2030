package consensus

import (
	"errors"

	"github.com/eth2030/eth2030/core/types"
)

// FinalityTracker tracks justification and finalization state across epochs.
type FinalityTracker struct {
	config          *ConsensusConfig
	state           BeaconState
	singleEpochMode bool // if true, finalize in 1 epoch instead of 2
}

// NewFinalityTracker creates a tracker with the given config.
func NewFinalityTracker(cfg *ConsensusConfig) *FinalityTracker {
	return &FinalityTracker{
		config:          cfg,
		singleEpochMode: cfg.IsSingleEpochFinality(),
	}
}

// State returns the current beacon state snapshot.
func (ft *FinalityTracker) State() BeaconState {
	return ft.state
}

// SetState replaces the tracker's internal state.
func (ft *FinalityTracker) SetState(s BeaconState) {
	ft.state = s
}

// FinalizedEpoch returns the finalized epoch.
func (ft *FinalityTracker) FinalizedEpoch() Epoch {
	return ft.state.FinalizedCheckpoint.Epoch
}

// JustifiedEpoch returns the justified epoch.
func (ft *FinalityTracker) JustifiedEpoch() Epoch {
	return ft.state.JustifiedCheckpoint.Epoch
}

// IsFinalizedAt returns true if the given epoch is finalized.
func (ft *FinalityTracker) IsFinalizedAt(epoch Epoch) bool {
	return epoch <= ft.state.FinalizedCheckpoint.Epoch
}

// FinalityDelay returns how many epochs behind finalization is.
func (ft *FinalityTracker) FinalityDelay() uint64 {
	if uint64(ft.state.Epoch) <= uint64(ft.state.FinalizedCheckpoint.Epoch) {
		return 0
	}
	return uint64(ft.state.Epoch) - uint64(ft.state.FinalizedCheckpoint.Epoch)
}

// Justify marks a checkpoint as justified (current epoch).
func (ft *FinalityTracker) Justify(epoch Epoch, root types.Hash) {
	ft.state.JustifiedCheckpoint = Checkpoint{Epoch: epoch, Root: root}
	ft.state.JustificationBits.Set(0)
}

// Finalize marks a checkpoint as finalized.
func (ft *FinalityTracker) Finalize(epoch Epoch, root types.Hash) {
	ft.state.FinalizedCheckpoint = Checkpoint{Epoch: epoch, Root: root}
}

// WeighJustification checks whether the given vote weight meets the
// supermajority threshold (2/3 of total).
func WeighJustification(totalWeight, voteWeight uint64) bool {
	// voteWeight * 3 >= totalWeight * 2 (avoiding overflow with division)
	if totalWeight == 0 {
		return false
	}
	return voteWeight*3 >= totalWeight*2
}

// ProcessEpoch runs justification and finalization logic at an epoch boundary.
// currentEpoch is the epoch that just ended. epochRoot is the block root at the
// epoch boundary. totalWeight and voteWeight are the total and attesting validator
// weights for the boundary attestations.
func (ft *FinalityTracker) ProcessEpoch(currentEpoch Epoch, epochRoot types.Hash, totalWeight, voteWeight uint64) {
	// Rotate justification bits: shift left by 1 to age (bit 0 -> bit 1).
	ft.state.PreviousJustified = ft.state.JustifiedCheckpoint
	ft.state.JustificationBits.Shift(1)

	// Update the current epoch.
	ft.state.Epoch = currentEpoch

	// Check supermajority for justification.
	if WeighJustification(totalWeight, voteWeight) {
		ft.Justify(currentEpoch, epochRoot)
	}

	// Try to finalize.
	if ft.singleEpochMode {
		ft.trySingleEpochFinality(currentEpoch)
	} else {
		ft.tryDualEpochFinality(currentEpoch)
	}
}

// trySingleEpochFinality finalizes in 1 epoch: if current epoch is justified,
// finalize the previous justified checkpoint immediately.
func (ft *FinalityTracker) trySingleEpochFinality(currentEpoch Epoch) {
	// If the current epoch was just justified, finalize it directly.
	if ft.state.JustifiedCheckpoint.Epoch == currentEpoch {
		ft.state.FinalizedCheckpoint = ft.state.JustifiedCheckpoint
	}
}

// ValidateEpochFinality checks that epoch finality state is internally consistent:
// epoch bounds, checkpoint ordering, and justification bits integrity.
func ValidateEpochFinality(state *BeaconState) error {
	if state == nil {
		return errors.New("finality: nil beacon state")
	}
	if state.FinalizedCheckpoint.Epoch > state.Epoch {
		return errors.New("finality: finalized epoch exceeds current epoch")
	}
	if state.JustifiedCheckpoint.Epoch > state.Epoch {
		return errors.New("finality: justified epoch exceeds current epoch")
	}
	if state.FinalizedCheckpoint.Epoch > state.JustifiedCheckpoint.Epoch {
		return errors.New("finality: finalized epoch exceeds justified epoch")
	}
	return nil
}

// tryDualEpochFinality implements standard Casper FFG 2-epoch finality.
// Checks the 4 Casper finality conditions from the Ethereum spec.
func (ft *FinalityTracker) tryDualEpochFinality(currentEpoch Epoch) {
	bits := ft.state.JustificationBits
	justified := ft.state.JustifiedCheckpoint
	prevJustified := ft.state.PreviousJustified

	// Condition 1: epochs k-2 and k-1 justified, finalize k-2
	// bits[1] = previous epoch justified, bits[2] = two epochs ago justified
	if currentEpoch >= 2 {
		if bits.IsJustified(1) && bits.IsJustified(2) {
			if prevJustified.Epoch+2 == currentEpoch {
				ft.state.FinalizedCheckpoint = prevJustified
			}
		}
	}

	// Condition 2: epoch k-1 justified, k justified, finalize k-1
	if currentEpoch >= 1 {
		if bits.IsJustified(0) && bits.IsJustified(1) {
			if justified.Epoch == currentEpoch && prevJustified.Epoch+1 == currentEpoch {
				ft.state.FinalizedCheckpoint = prevJustified
			}
		}
	}

	// Condition 3: k-3, k-2, k-1 justified, finalize k-3
	if currentEpoch >= 3 {
		if bits.IsJustified(1) && bits.IsJustified(2) && bits.IsJustified(3) {
			if prevJustified.Epoch+3 == currentEpoch {
				ft.state.FinalizedCheckpoint = prevJustified
			}
		}
	}

	// Condition 4: k-2, k-1, k justified, finalize k-2
	if currentEpoch >= 2 {
		if bits.IsJustified(0) && bits.IsJustified(1) && bits.IsJustified(2) {
			if prevJustified.Epoch+2 == currentEpoch {
				ft.state.FinalizedCheckpoint = prevJustified
			}
		}
	}
}
