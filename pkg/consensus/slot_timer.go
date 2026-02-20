package consensus

import "time"

// SlotTimerConfig configures the slot timer.
type SlotTimerConfig struct {
	GenesisTime    uint64 // unix timestamp of chain genesis
	SecondsPerSlot uint64 // duration of each slot in seconds
	SlotsPerEpoch  uint64 // number of slots per epoch
}

// DefaultSlotTimerConfig returns mainnet-like slot timing parameters.
func DefaultSlotTimerConfig() SlotTimerConfig {
	return SlotTimerConfig{
		GenesisTime:    0,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	}
}

// SlotTimer provides slot and epoch timing calculations relative to genesis.
// All methods are pure computations based on config and wall clock time.
type SlotTimer struct {
	genesisTime    uint64
	secondsPerSlot uint64
	slotsPerEpoch  uint64
}

// NewSlotTimer creates a new SlotTimer from the given config.
// Panics if SecondsPerSlot or SlotsPerEpoch is zero.
func NewSlotTimer(config SlotTimerConfig) *SlotTimer {
	if config.SecondsPerSlot == 0 {
		panic("consensus: SecondsPerSlot must be > 0")
	}
	if config.SlotsPerEpoch == 0 {
		panic("consensus: SlotsPerEpoch must be > 0")
	}
	return &SlotTimer{
		genesisTime:    config.GenesisTime,
		secondsPerSlot: config.SecondsPerSlot,
		slotsPerEpoch:  config.SlotsPerEpoch,
	}
}

// CurrentSlot returns the current slot number based on the wall clock.
// Returns 0 if the current time is before genesis.
func (st *SlotTimer) CurrentSlot() uint64 {
	now := uint64(time.Now().Unix())
	if now < st.genesisTime {
		return 0
	}
	return (now - st.genesisTime) / st.secondsPerSlot
}

// CurrentEpoch returns the current epoch number based on the wall clock.
func (st *SlotTimer) CurrentEpoch() uint64 {
	return st.CurrentSlot() / st.slotsPerEpoch
}

// SlotStartTime returns the unix timestamp when the given slot begins.
func (st *SlotTimer) SlotStartTime(slot uint64) uint64 {
	return st.genesisTime + slot*st.secondsPerSlot
}

// EpochStartSlot returns the first slot of the given epoch.
func (st *SlotTimer) EpochStartSlot(epoch uint64) uint64 {
	return epoch * st.slotsPerEpoch
}

// SlotToEpoch converts a slot number to its containing epoch.
func (st *SlotTimer) SlotToEpoch(slot uint64) uint64 {
	return slot / st.slotsPerEpoch
}

// TimeUntilSlot returns the number of seconds until the given slot starts.
// Returns a negative value if the slot is in the past.
func (st *SlotTimer) TimeUntilSlot(slot uint64) int64 {
	slotStart := int64(st.SlotStartTime(slot))
	now := time.Now().Unix()
	return slotStart - now
}

// IsFirstSlotOfEpoch returns true if the given slot is the first slot of
// its epoch (an epoch boundary).
func (st *SlotTimer) IsFirstSlotOfEpoch(slot uint64) bool {
	return slot%st.slotsPerEpoch == 0
}

// SlotsSinceGenesis returns the total number of elapsed slots since genesis.
// This is equivalent to CurrentSlot() but named for clarity.
func (st *SlotTimer) SlotsSinceGenesis() uint64 {
	return st.CurrentSlot()
}

// EpochProgress returns the fractional progress through the current epoch,
// as a value in [0.0, 1.0). A value of 0.0 means the epoch just started;
// values approaching 1.0 mean the epoch is nearly complete.
func (st *SlotTimer) EpochProgress() float64 {
	slot := st.CurrentSlot()
	slotInEpoch := slot % st.slotsPerEpoch
	return float64(slotInEpoch) / float64(st.slotsPerEpoch)
}

// WithGenesisTime returns a new SlotTimer with a different genesis time,
// keeping all other configuration the same.
func (st *SlotTimer) WithGenesisTime(t uint64) *SlotTimer {
	return &SlotTimer{
		genesisTime:    t,
		secondsPerSlot: st.secondsPerSlot,
		slotsPerEpoch:  st.slotsPerEpoch,
	}
}

// GenesisTimeValue returns the configured genesis timestamp.
func (st *SlotTimer) GenesisTimeValue() uint64 {
	return st.genesisTime
}

// slotAt computes the slot at a given unix timestamp. Used internally
// for testing with deterministic timestamps.
func (st *SlotTimer) slotAt(unixTime uint64) uint64 {
	if unixTime < st.genesisTime {
		return 0
	}
	return (unixTime - st.genesisTime) / st.secondsPerSlot
}

// epochAt computes the epoch at a given unix timestamp.
func (st *SlotTimer) epochAt(unixTime uint64) uint64 {
	return st.slotAt(unixTime) / st.slotsPerEpoch
}

// epochProgressAt computes epoch progress at a given unix timestamp.
func (st *SlotTimer) epochProgressAt(unixTime uint64) float64 {
	slot := st.slotAt(unixTime)
	slotInEpoch := slot % st.slotsPerEpoch
	return float64(slotInEpoch) / float64(st.slotsPerEpoch)
}
