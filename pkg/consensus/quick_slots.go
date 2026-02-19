package consensus

import (
	"time"
)

// Quick Slots implements the K+ upgrade (2028) with 4-slot epochs and
// 6-second slots, reducing epoch duration from 6.4 minutes (32 * 12s)
// to 24 seconds (4 * 6s). This enables faster finality and tighter
// consensus feedback loops.

// QuickSlotConfig holds parameters for quick-slot timing.
type QuickSlotConfig struct {
	// SlotDuration is the duration of each slot (default: 6 seconds).
	SlotDuration time.Duration

	// SlotsPerEpoch is the number of slots per epoch (default: 4).
	SlotsPerEpoch uint64
}

// DefaultQuickSlotConfig returns production defaults: 6s slots, 4 slots/epoch.
func DefaultQuickSlotConfig() *QuickSlotConfig {
	return &QuickSlotConfig{
		SlotDuration:  6 * time.Second,
		SlotsPerEpoch: 4,
	}
}

// EpochDuration returns the total duration of one epoch.
func (c *QuickSlotConfig) EpochDuration() time.Duration {
	return c.SlotDuration * time.Duration(c.SlotsPerEpoch)
}

// QuickSlotScheduler manages slot timing and epoch transitions for
// the quick-slot regime. It provides wall-clock-based slot/epoch
// calculations from a genesis time reference.
type QuickSlotScheduler struct {
	config      *QuickSlotConfig
	genesisTime time.Time
}

// NewQuickSlotScheduler creates a scheduler with the given config and genesis time.
func NewQuickSlotScheduler(config *QuickSlotConfig, genesisTime time.Time) *QuickSlotScheduler {
	if config == nil {
		config = DefaultQuickSlotConfig()
	}
	return &QuickSlotScheduler{
		config:      config,
		genesisTime: genesisTime,
	}
}

// CurrentSlot returns the current slot based on the wall clock.
// Returns 0 if the current time is before genesis.
func (s *QuickSlotScheduler) CurrentSlot() uint64 {
	return s.SlotAt(time.Now())
}

// SlotAt returns the slot number at the given time.
// Returns 0 if t is before genesis.
func (s *QuickSlotScheduler) SlotAt(t time.Time) uint64 {
	if t.Before(s.genesisTime) {
		return 0
	}
	elapsed := t.Sub(s.genesisTime)
	return uint64(elapsed / s.config.SlotDuration)
}

// CurrentEpoch returns the current epoch based on the wall clock.
func (s *QuickSlotScheduler) CurrentEpoch() uint64 {
	return s.SlotToEpoch(s.CurrentSlot())
}

// SlotToEpoch converts a slot number to its epoch number.
func (s *QuickSlotScheduler) SlotToEpoch(slot uint64) uint64 {
	if s.config.SlotsPerEpoch == 0 {
		return 0
	}
	return slot / s.config.SlotsPerEpoch
}

// EpochStartSlot returns the first slot of the given epoch.
func (s *QuickSlotScheduler) EpochStartSlot(epoch uint64) uint64 {
	return epoch * s.config.SlotsPerEpoch
}

// SlotStartTime returns the absolute time when a slot starts.
func (s *QuickSlotScheduler) SlotStartTime(slot uint64) time.Time {
	offset := time.Duration(slot) * s.config.SlotDuration
	return s.genesisTime.Add(offset)
}

// IsFirstSlotOfEpoch returns true if the given slot is the first slot
// in its epoch (i.e., slot % SlotsPerEpoch == 0).
func (s *QuickSlotScheduler) IsFirstSlotOfEpoch(slot uint64) bool {
	if s.config.SlotsPerEpoch == 0 {
		return false
	}
	return slot%s.config.SlotsPerEpoch == 0
}

// NextSlotTime returns the time when the next slot starts, relative
// to the current wall clock.
func (s *QuickSlotScheduler) NextSlotTime() time.Time {
	now := time.Now()
	if now.Before(s.genesisTime) {
		return s.genesisTime
	}
	currentSlot := s.SlotAt(now)
	return s.SlotStartTime(currentSlot + 1)
}

// TimeUntilNextSlot returns the duration until the next slot boundary.
func (s *QuickSlotScheduler) TimeUntilNextSlot() time.Duration {
	return time.Until(s.NextSlotTime())
}

// GenesisTime returns the scheduler's genesis time.
func (s *QuickSlotScheduler) GenesisTime() time.Time {
	return s.genesisTime
}

// Config returns the scheduler's quick-slot config.
func (s *QuickSlotScheduler) Config() *QuickSlotConfig {
	return s.config
}

// ValidatorDuties describes the duties for a single slot: who proposes
// and which validators are on the attesting committee.
type ValidatorDuties struct {
	// ProposerIndex is the validator selected to propose the block.
	ProposerIndex uint64

	// CommitteeIndices lists validators assigned to the attesting
	// committee for this slot.
	CommitteeIndices []uint64
}

// GetDuties computes deterministic duty assignments for a given slot.
// With only 4 slots per epoch, each slot gets approximately 1/4 of the
// validator set as committee members. The proposer is selected by
// slot mod validatorCount.
//
// In production this would use a RANDAO-based shuffle; here we use
// a simple deterministic assignment for correctness testing.
func (s *QuickSlotScheduler) GetDuties(slot uint64, validatorCount int) *ValidatorDuties {
	if validatorCount == 0 {
		return &ValidatorDuties{}
	}

	// Proposer: simple deterministic selection.
	proposer := slot % uint64(validatorCount)

	// Committee: assign ~1/SlotsPerEpoch of validators to each slot.
	// Slot offset within epoch determines which segment of validators
	// is assigned.
	slotsPerEpoch := s.config.SlotsPerEpoch
	if slotsPerEpoch == 0 {
		slotsPerEpoch = 1
	}
	slotInEpoch := slot % slotsPerEpoch

	// Divide validators into SlotsPerEpoch segments.
	segmentSize := validatorCount / int(slotsPerEpoch)
	remainder := validatorCount % int(slotsPerEpoch)

	// Calculate start and end indices for this slot's segment.
	start := int(slotInEpoch) * segmentSize
	if int(slotInEpoch) < remainder {
		start += int(slotInEpoch)
	} else {
		start += remainder
	}

	size := segmentSize
	if int(slotInEpoch) < remainder {
		size++
	}

	committee := make([]uint64, size)
	for i := 0; i < size; i++ {
		committee[i] = uint64(start + i)
	}

	return &ValidatorDuties{
		ProposerIndex:   proposer,
		CommitteeIndices: committee,
	}
}
