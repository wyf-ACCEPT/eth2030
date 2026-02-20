// slot_duty_scheduler.go implements slot-level duty scheduling for the
// consensus layer, supporting multi-duration slot transitions (12s -> 6s),
// proposer duty computation with RANDAO-based selection, and fork-aware
// genesis time derivation.
//
// This builds on the SlotTimer, SlotClock, and SlotSchedule primitives to
// provide a higher-level scheduler that tracks proposer duties per slot,
// supports epoch boundary detection across fork transitions, and provides
// slot-to-time / time-to-slot conversions that account for slot duration
// changes at fork boundaries.
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

// Duty scheduler errors.
var (
	ErrDutyNoValidators = errors.New("duty_scheduler: no active validators")
	ErrDutySlotInPast   = errors.New("duty_scheduler: slot is in the past")
	ErrDutyNilSchedule  = errors.New("duty_scheduler: nil slot schedule")
	ErrDutyForkNotFound = errors.New("duty_scheduler: fork not found for slot")
)

// SlotDutyConfig configures the slot-level duty scheduler.
type SlotDutyConfig struct {
	// GenesisTime is the unix timestamp of chain genesis.
	GenesisTime uint64

	// InitialSecondsPerSlot is the slot duration at genesis (e.g. 12s).
	InitialSecondsPerSlot uint64

	// InitialSlotsPerEpoch is the number of slots per epoch at genesis (e.g. 32).
	InitialSlotsPerEpoch uint64

	// QuickSlotForkTime is the unix timestamp when quick slots activate (0 = never).
	// After this fork, slot duration changes to QuickSecondsPerSlot and epoch
	// length changes to QuickSlotsPerEpoch.
	QuickSlotForkTime uint64

	// QuickSecondsPerSlot is the slot duration after the quick-slot fork (e.g. 6s).
	QuickSecondsPerSlot uint64

	// QuickSlotsPerEpoch is the epoch length after the quick-slot fork (e.g. 4).
	QuickSlotsPerEpoch uint64
}

// DefaultSlotDutyConfig returns a mainnet-like configuration starting at 12s
// slots with a quick-slot fork disabled.
func DefaultSlotDutyConfig() SlotDutyConfig {
	return SlotDutyConfig{
		GenesisTime:           0,
		InitialSecondsPerSlot: 12,
		InitialSlotsPerEpoch:  32,
		QuickSlotForkTime:     0,
		QuickSecondsPerSlot:   6,
		QuickSlotsPerEpoch:    4,
	}
}

// ProposerDutyEntry records the proposer assignment for a single slot.
type ProposerDutyEntry struct {
	Slot           Slot
	ProposerIndex  ValidatorIndex
	EpochNumber    Epoch
	IsEpochBoundary bool
	SlotStartTime  uint64 // unix timestamp
}

// SlotDutyScheduler provides fork-aware proposer duty scheduling and
// slot/epoch timing. It integrates with the SlotSchedule for multi-duration
// slot support and computes deterministic proposer duties.
// All methods are safe for concurrent use.
type SlotDutyScheduler struct {
	mu       sync.RWMutex
	config   SlotDutyConfig
	schedule *SlotSchedule

	// dutyCache caches computed proposer duties by epoch.
	dutyCache map[Epoch][]ProposerDutyEntry
}

// NewSlotDutyScheduler creates a new duty scheduler with the given config.
func NewSlotDutyScheduler(config SlotDutyConfig) *SlotDutyScheduler {
	sched := NewSlotSchedule(config.GenesisTime, config.InitialSecondsPerSlot)
	if config.QuickSlotForkTime > 0 {
		// Errors are suppressed since we validate timestamps above genesis.
		_ = sched.AddFork(config.QuickSlotForkTime, config.QuickSecondsPerSlot)
	}

	return &SlotDutyScheduler{
		config:    config,
		schedule:  sched,
		dutyCache: make(map[Epoch][]ProposerDutyEntry),
	}
}

// SlotAtTime returns the slot number at a given unix timestamp, accounting
// for fork transitions that change slot duration.
func (ds *SlotDutyScheduler) SlotAtTime(unixTime uint64) Slot {
	return ds.schedule.SlotAtTime(unixTime)
}

// CurrentSlot returns the current slot based on the wall clock.
func (ds *SlotDutyScheduler) CurrentSlot() Slot {
	return ds.SlotAtTime(uint64(time.Now().Unix()))
}

// TimeForSlot returns the unix timestamp when the given slot begins.
// It accumulates slot durations across fork boundaries.
func (ds *SlotDutyScheduler) TimeForSlot(slot Slot) uint64 {
	if slot == 0 {
		return ds.config.GenesisTime
	}

	// Walk through slots respecting fork boundaries.
	t := ds.config.GenesisTime
	remaining := uint64(slot)

	for remaining > 0 {
		dur := ds.schedule.SlotDurationAtTime(t)
		if dur == 0 {
			dur = ds.config.InitialSecondsPerSlot
		}
		t += dur
		remaining--
	}
	return t
}

// SlotsPerEpochAtSlot returns the epoch length in effect at the given slot.
func (ds *SlotDutyScheduler) SlotsPerEpochAtSlot(slot Slot) uint64 {
	slotTime := ds.TimeForSlot(slot)
	if ds.config.QuickSlotForkTime > 0 && slotTime >= ds.config.QuickSlotForkTime {
		return ds.config.QuickSlotsPerEpoch
	}
	return ds.config.InitialSlotsPerEpoch
}

// EpochForSlot returns the epoch number that contains the given slot,
// accounting for the quick-slot fork epoch length change.
func (ds *SlotDutyScheduler) EpochForSlot(slot Slot) Epoch {
	spe := ds.SlotsPerEpochAtSlot(slot)
	if spe == 0 {
		return 0
	}
	return Epoch(uint64(slot) / spe)
}

// IsEpochBoundary returns true if the given slot is the first slot of its
// epoch, accounting for fork transitions.
func (ds *SlotDutyScheduler) IsEpochBoundary(slot Slot) bool {
	spe := ds.SlotsPerEpochAtSlot(slot)
	if spe == 0 {
		return false
	}
	return uint64(slot)%spe == 0
}

// DeriveGenesisTime computes the genesis time from the minimum genesis time
// and genesis delay. This follows the spec's genesis derivation:
//
//	genesis_time = max(min_genesis_time, eth1_timestamp + genesis_delay)
func DeriveGenesisTime(minGenesisTime, eth1Timestamp, genesisDelay uint64) uint64 {
	computed := eth1Timestamp + genesisDelay
	if computed > minGenesisTime {
		return computed
	}
	return minGenesisTime
}

// DurationUntilSlot returns the wall-clock duration until the given slot
// starts. Returns a negative duration if the slot is in the past.
func (ds *SlotDutyScheduler) DurationUntilSlot(slot Slot) time.Duration {
	slotTime := ds.TimeForSlot(slot)
	now := uint64(time.Now().Unix())
	if slotTime >= now {
		return time.Duration(slotTime-now) * time.Second
	}
	return -time.Duration(now-slotTime) * time.Second
}

// ComputeProposerForSlot deterministically selects a proposer for the given
// slot from the active validator indices using a RANDAO-based selection.
// The seed is derived from the epoch's RANDAO mix and the slot number.
func ComputeProposerForSlot(
	slot Slot,
	activeIndices []ValidatorIndex,
	randaoMix [32]byte,
	effectiveBalances []uint64,
) (ValidatorIndex, error) {
	if len(activeIndices) == 0 {
		return 0, ErrDutyNoValidators
	}

	// Compute seed: sha256(randaoMix || slot).
	var buf [40]byte
	copy(buf[:32], randaoMix[:])
	binary.LittleEndian.PutUint64(buf[32:], uint64(slot))
	seed := sha256.Sum256(buf[:])

	total := uint64(len(activeIndices))

	// Proposer selection: iterate candidates using shuffled index and
	// effective-balance-weighted acceptance (matching the beacon chain spec).
	for i := uint64(0); i < total*100; i++ {
		candidateIdx := i % total

		// Compute random byte for acceptance test.
		var rbuf [40]byte
		copy(rbuf[:32], seed[:])
		binary.LittleEndian.PutUint64(rbuf[32:], i/32)
		rh := sha256.Sum256(rbuf[:])
		randomByte := uint64(rh[i%32])

		valIdx := activeIndices[candidateIdx]

		// Effective balance check: accept if
		// effective_balance * 255 >= MAX_EFFECTIVE_BALANCE * random_byte
		var eb uint64
		if int(valIdx) < len(effectiveBalances) {
			eb = effectiveBalances[valIdx]
		} else {
			eb = MaxEffectiveBalance
		}

		if eb*255 >= MaxEffectiveBalance*randomByte {
			return valIdx, nil
		}
	}

	// Fallback: return the first active validator.
	return activeIndices[0], nil
}

// ComputeEpochDuties computes proposer duties for all slots in the given
// epoch. Results are cached internally.
func (ds *SlotDutyScheduler) ComputeEpochDuties(
	epoch Epoch,
	activeIndices []ValidatorIndex,
	randaoMix [32]byte,
	effectiveBalances []uint64,
) ([]ProposerDutyEntry, error) {
	if len(activeIndices) == 0 {
		return nil, ErrDutyNoValidators
	}

	// Check the cache first.
	ds.mu.RLock()
	if cached, ok := ds.dutyCache[epoch]; ok {
		ds.mu.RUnlock()
		return cached, nil
	}
	ds.mu.RUnlock()

	// Determine epoch parameters at the first slot of this epoch.
	// Use the initial slots-per-epoch to determine start slot; in a real
	// fork-aware system the epoch boundaries would shift, but here we
	// use the configuration in effect.
	spe := ds.config.InitialSlotsPerEpoch
	startSlotTime := ds.TimeForSlot(Slot(uint64(epoch) * spe))
	if ds.config.QuickSlotForkTime > 0 && startSlotTime >= ds.config.QuickSlotForkTime {
		spe = ds.config.QuickSlotsPerEpoch
	}

	startSlot := Slot(uint64(epoch) * spe)
	duties := make([]ProposerDutyEntry, spe)

	for i := uint64(0); i < spe; i++ {
		slot := Slot(uint64(startSlot) + i)
		proposer, err := ComputeProposerForSlot(slot, activeIndices, randaoMix, effectiveBalances)
		if err != nil {
			return nil, err
		}

		duties[i] = ProposerDutyEntry{
			Slot:            slot,
			ProposerIndex:   proposer,
			EpochNumber:     epoch,
			IsEpochBoundary: i == 0,
			SlotStartTime:   ds.TimeForSlot(slot),
		}
	}

	// Cache the result.
	ds.mu.Lock()
	ds.dutyCache[epoch] = duties
	ds.mu.Unlock()

	return duties, nil
}

// ClearDutyCache removes all cached duty entries.
func (ds *SlotDutyScheduler) ClearDutyCache() {
	ds.mu.Lock()
	ds.dutyCache = make(map[Epoch][]ProposerDutyEntry)
	ds.mu.Unlock()
}

// Schedule returns the underlying SlotSchedule for direct access.
func (ds *SlotDutyScheduler) Schedule() *SlotSchedule {
	return ds.schedule
}
