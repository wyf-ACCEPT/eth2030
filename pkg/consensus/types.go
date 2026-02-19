// Package consensus implements Ethereum consensus-layer primitives including
// quick slots, epoch timing, and finality tracking.
package consensus

import (
	"github.com/eth2028/eth2028/core/types"
)

// Epoch is a consensus-layer epoch number.
type Epoch uint64

// Slot is a consensus-layer slot number.
type Slot uint64

// ValidatorIndex is a beacon-chain validator index.
type ValidatorIndex uint64

// Checkpoint represents a finality checkpoint (epoch + block root).
type Checkpoint struct {
	Epoch Epoch
	Root  types.Hash
}

// JustificationBits is a bitfield tracking justification status of recent epochs.
// Bit 0 = current epoch, bit 1 = previous epoch, etc.
type JustificationBits uint8

// IsJustified returns whether the epoch at the given offset is justified.
// Offset 0 = current epoch, 1 = previous, 2 = two epochs ago, etc.
func (j JustificationBits) IsJustified(offset uint) bool {
	if offset > 7 {
		return false
	}
	return j&(1<<offset) != 0
}

// Set marks the epoch at the given offset as justified.
func (j *JustificationBits) Set(offset uint) {
	if offset > 7 {
		return
	}
	*j |= 1 << offset
}

// Shift ages the bitfield by shifting bits left by n positions.
// Bit 0 (current) moves to bit 1 (previous), etc. Bit 0 is cleared.
func (j *JustificationBits) Shift(n uint) {
	*j <<= n
}

// BeaconState holds minimal beacon state fields relevant for consensus timing
// and finality tracking.
type BeaconState struct {
	Slot                 Slot
	Epoch                Epoch
	FinalizedCheckpoint  Checkpoint
	JustifiedCheckpoint  Checkpoint
	PreviousJustified    Checkpoint
	JustificationBits    JustificationBits
}

// SlotToEpoch returns the epoch number for a given slot.
func SlotToEpoch(slot Slot, slotsPerEpoch uint64) Epoch {
	if slotsPerEpoch == 0 {
		return 0
	}
	return Epoch(uint64(slot) / slotsPerEpoch)
}

// EpochStartSlot returns the first slot of a given epoch.
func EpochStartSlot(epoch Epoch, slotsPerEpoch uint64) Slot {
	return Slot(uint64(epoch) * slotsPerEpoch)
}
