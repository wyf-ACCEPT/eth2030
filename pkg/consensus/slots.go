package consensus

import (
	"fmt"
	"sort"
	"time"
)

// SlotClock computes the current slot from genesis time and slot duration.
type SlotClock struct {
	genesisTime    uint64 // unix timestamp of genesis
	secondsPerSlot uint64 // slot duration in seconds
	slotsPerEpoch  uint64 // slots per epoch
}

// NewSlotClock creates a SlotClock with the given genesis time and config.
func NewSlotClock(genesisTime uint64, cfg *ConsensusConfig) *SlotClock {
	return &SlotClock{
		genesisTime:    genesisTime,
		secondsPerSlot: cfg.SecondsPerSlot,
		slotsPerEpoch:  cfg.SlotsPerEpoch,
	}
}

// CurrentSlot returns the current slot for the given timestamp.
// Returns 0 if the timestamp is before genesis.
func (sc *SlotClock) CurrentSlot(now uint64) Slot {
	if now < sc.genesisTime {
		return 0
	}
	elapsed := now - sc.genesisTime
	return Slot(elapsed / sc.secondsPerSlot)
}

// CurrentEpoch returns the current epoch for the given timestamp.
func (sc *SlotClock) CurrentEpoch(now uint64) Epoch {
	return SlotToEpoch(sc.CurrentSlot(now), sc.slotsPerEpoch)
}

// SlotStartTime returns the absolute timestamp when a slot begins.
func (sc *SlotClock) SlotStartTime(slot Slot) uint64 {
	return sc.genesisTime + uint64(slot)*sc.secondsPerSlot
}

// TimeInSlot returns how many seconds into the slot the given timestamp is.
// Returns 0 if the timestamp is before genesis.
func (sc *SlotClock) TimeInSlot(now uint64) uint64 {
	if now < sc.genesisTime {
		return 0
	}
	elapsed := now - sc.genesisTime
	return elapsed % sc.secondsPerSlot
}

// NextSlotIn returns the duration until the next slot boundary.
func (sc *SlotClock) NextSlotIn(now uint64) time.Duration {
	if now < sc.genesisTime {
		return time.Duration(sc.genesisTime-now) * time.Second
	}
	inSlot := sc.TimeInSlot(now)
	remaining := sc.secondsPerSlot - inSlot
	return time.Duration(remaining) * time.Second
}

// GenesisTime returns the genesis timestamp.
func (sc *SlotClock) GenesisTime() uint64 {
	return sc.genesisTime
}

// SecondsPerSlot returns the slot duration.
func (sc *SlotClock) SecondsPerSlot() uint64 {
	return sc.secondsPerSlot
}

// AttestationDeadline returns the time within a slot by which attestations
// must be received. Typically 1/3 of the slot duration.
func (sc *SlotClock) AttestationDeadline() time.Duration {
	return time.Duration(sc.secondsPerSlot/3) * time.Second
}

// ProposalDeadline returns the time within a slot by which the block proposal
// must be broadcast. Typically at slot start (0).
func (sc *SlotClock) ProposalDeadline() time.Duration {
	return 0
}

// forkEntry maps a fork activation timestamp to a slot duration.
type forkEntry struct {
	Timestamp      uint64 // activation timestamp
	SecondsPerSlot uint64 // new slot duration from this timestamp
}

// SlotSchedule maps fork timestamps to slot durations, supporting transitions
// from e.g. 12s slots to 6s slots at a given fork time.
type SlotSchedule struct {
	genesisTime uint64
	forks       []forkEntry // sorted by timestamp ascending
}

// NewSlotSchedule creates a schedule with a base slot duration from genesis.
func NewSlotSchedule(genesisTime, baseSecondsPerSlot uint64) *SlotSchedule {
	return &SlotSchedule{
		genesisTime: genesisTime,
		forks: []forkEntry{
			{Timestamp: genesisTime, SecondsPerSlot: baseSecondsPerSlot},
		},
	}
}

// AddFork registers a slot duration change at the given timestamp.
// Forks must be added with increasing timestamps.
func (ss *SlotSchedule) AddFork(timestamp, secondsPerSlot uint64) error {
	if secondsPerSlot == 0 {
		return fmt.Errorf("consensus: slot duration must be > 0")
	}
	if len(ss.forks) > 0 && timestamp <= ss.forks[len(ss.forks)-1].Timestamp {
		return fmt.Errorf("consensus: fork timestamp %d must be after previous fork %d",
			timestamp, ss.forks[len(ss.forks)-1].Timestamp)
	}
	ss.forks = append(ss.forks, forkEntry{
		Timestamp:      timestamp,
		SecondsPerSlot: secondsPerSlot,
	})
	return nil
}

// SlotDurationAtTime returns the slot duration in effect at the given timestamp.
func (ss *SlotSchedule) SlotDurationAtTime(t uint64) uint64 {
	// Find the last fork at or before t.
	idx := sort.Search(len(ss.forks), func(i int) bool {
		return ss.forks[i].Timestamp > t
	})
	if idx == 0 {
		return ss.forks[0].SecondsPerSlot
	}
	return ss.forks[idx-1].SecondsPerSlot
}

// SlotAtTime computes the slot number at a given timestamp, accounting for
// fork transitions that change slot duration.
func (ss *SlotSchedule) SlotAtTime(t uint64) Slot {
	if t < ss.genesisTime {
		return 0
	}

	var totalSlots uint64
	prevTime := ss.genesisTime

	for _, fork := range ss.forks {
		if fork.Timestamp <= ss.genesisTime {
			continue
		}
		if fork.Timestamp > t {
			break
		}
		// Count slots in the segment before this fork.
		dur := ss.SlotDurationAtTime(prevTime)
		segLen := fork.Timestamp - prevTime
		totalSlots += segLen / dur
		prevTime = fork.Timestamp
	}

	// Count remaining slots from last fork boundary to t.
	dur := ss.SlotDurationAtTime(prevTime)
	remaining := t - prevTime
	totalSlots += remaining / dur

	return Slot(totalSlots)
}
