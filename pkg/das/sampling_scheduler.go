// sampling_scheduler.go implements a DAS sampling scheduler that orchestrates
// sampling rounds per the PeerDAS specification (EIP-7594). It manages
// sampling quotas per slot, tracks which columns have been sampled, implements
// adaptive sampling rates based on network conditions, and coordinates with
// custody requirements. Supports both regular and extended sampling modes.
//
// Reference: consensus-specs/specs/fulu/das-core.md
package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"golang.org/x/crypto/sha3"
)

// Sampling scheduler errors.
var (
	ErrSchedClosed          = errors.New("das/sched: scheduler is closed")
	ErrSchedSlotZero        = errors.New("das/sched: slot must be > 0")
	ErrSchedRoundComplete   = errors.New("das/sched: sampling round already complete")
	ErrSchedColumnOOB       = errors.New("das/sched: column index out of range")
	ErrSchedQuotaExhausted  = errors.New("das/sched: sampling quota exhausted for slot")
	ErrSchedNoActiveRound   = errors.New("das/sched: no active sampling round")
	ErrSchedInvalidMode     = errors.New("das/sched: invalid sampling mode")
)

// SamplingMode determines the sampling intensity.
type SamplingMode int

const (
	// RegularSampling uses the standard SamplesPerSlot quota.
	RegularSampling SamplingMode = iota

	// ExtendedSampling doubles the sample count for higher assurance.
	ExtendedSampling
)

// SchedulerConfig configures the sampling scheduler.
type SchedulerConfig struct {
	// BaseSamplesPerSlot is the base number of columns to sample per slot.
	BaseSamplesPerSlot int

	// NumberOfColumns is the total columns in the extended data matrix.
	NumberOfColumns int

	// MaxConcurrentSlots is the maximum number of slots tracked simultaneously.
	MaxConcurrentSlots int

	// AdaptiveMinRate is the minimum sampling rate multiplier (0.5 = 50% of base).
	AdaptiveMinRate float64

	// AdaptiveMaxRate is the maximum sampling rate multiplier (2.0 = 200% of base).
	AdaptiveMaxRate float64

	// SuccessRateThreshold: if success rate drops below this, increase sampling.
	SuccessRateThreshold float64

	// HighSuccessThreshold: if success rate is above this, decrease sampling.
	HighSuccessThreshold float64
}

// DefaultSchedulerConfig returns production defaults.
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		BaseSamplesPerSlot: SamplesPerSlot,
		NumberOfColumns:    NumberOfColumns,
		MaxConcurrentSlots: 32,
		AdaptiveMinRate:    0.5,
		AdaptiveMaxRate:    3.0,
		SuccessRateThreshold: 0.7,
		HighSuccessThreshold: 0.95,
	}
}

// SamplingRound represents the state of a single slot's sampling round.
type SamplingRound struct {
	// Slot is the beacon slot for this round.
	Slot uint64

	// Mode is the sampling intensity mode.
	Mode SamplingMode

	// TargetColumns are the columns to sample.
	TargetColumns []ColumnIndex

	// SampledColumns tracks which columns have been sampled.
	SampledColumns map[ColumnIndex]bool

	// SuccessColumns tracks which sampled columns passed verification.
	SuccessColumns map[ColumnIndex]bool

	// FailedColumns tracks which sampled columns failed verification.
	FailedColumns map[ColumnIndex]bool

	// Quota is the remaining number of samples allowed.
	Quota int

	// Complete is true when the round has finished.
	Complete bool

	// StartedAt is when this round began.
	StartedAt time.Time

	// CompletedAt is when this round finished.
	CompletedAt time.Time
}

// SamplingStats reports aggregated statistics from the scheduler.
type SamplingStats struct {
	// TotalRounds is the total number of sampling rounds tracked.
	TotalRounds int

	// CompletedRounds is the number of completed rounds.
	CompletedRounds int

	// TotalSamples is the total number of individual column samples taken.
	TotalSamples int

	// TotalSuccesses is the total number of successful samples.
	TotalSuccesses int

	// TotalFailures is the total number of failed samples.
	TotalFailures int

	// SuccessRate is the overall success rate (0.0 to 1.0).
	SuccessRate float64

	// CurrentAdaptiveRate is the current adaptive rate multiplier.
	CurrentAdaptiveRate float64
}

// SamplingScheduler orchestrates DAS sampling rounds, managing quotas,
// tracking progress, and adapting rates based on network conditions.
// All public methods are safe for concurrent use.
type SamplingScheduler struct {
	mu     sync.RWMutex
	config SchedulerConfig
	nodeID [32]byte

	// rounds maps slot numbers to their sampling round state.
	rounds map[uint64]*SamplingRound

	// adaptiveRate is the current sampling rate multiplier.
	adaptiveRate float64

	// recentSuccessRate tracks the rolling success rate.
	recentSuccessRate float64

	// custodyColumns is the set of columns this node custodies.
	custodyColumns map[ColumnIndex]bool

	// closed indicates the scheduler has been shut down.
	closed bool
}

// NewSamplingScheduler creates a new sampling scheduler for the given node.
func NewSamplingScheduler(config SchedulerConfig, nodeID [32]byte) *SamplingScheduler {
	if config.BaseSamplesPerSlot <= 0 {
		config.BaseSamplesPerSlot = SamplesPerSlot
	}
	if config.NumberOfColumns <= 0 {
		config.NumberOfColumns = NumberOfColumns
	}
	if config.MaxConcurrentSlots <= 0 {
		config.MaxConcurrentSlots = 32
	}
	if config.AdaptiveMinRate <= 0 {
		config.AdaptiveMinRate = 0.5
	}
	if config.AdaptiveMaxRate <= 0 {
		config.AdaptiveMaxRate = 3.0
	}
	if config.SuccessRateThreshold <= 0 {
		config.SuccessRateThreshold = 0.7
	}
	if config.HighSuccessThreshold <= 0 {
		config.HighSuccessThreshold = 0.95
	}

	// Pre-compute custody columns.
	custodyCols := make(map[ColumnIndex]bool)
	cols, err := GetCustodyColumns(nodeID, CustodyRequirement)
	if err == nil {
		for _, c := range cols {
			custodyCols[c] = true
		}
	}

	return &SamplingScheduler{
		config:         config,
		nodeID:         nodeID,
		rounds:         make(map[uint64]*SamplingRound),
		adaptiveRate:   1.0,
		custodyColumns: custodyCols,
	}
}

// StartRound begins a new sampling round for the given slot and mode.
// It deterministically selects which columns to sample and sets the quota.
func (ss *SamplingScheduler) StartRound(slot uint64, mode SamplingMode) (*SamplingRound, error) {
	if slot == 0 {
		return nil, ErrSchedSlotZero
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.closed {
		return nil, ErrSchedClosed
	}

	// If round already exists, return it.
	if existing, ok := ss.rounds[slot]; ok {
		return existing, nil
	}

	// Compute effective sample count based on mode and adaptive rate.
	baseSamples := ss.config.BaseSamplesPerSlot
	if mode == ExtendedSampling {
		baseSamples *= 2
	}
	effectiveSamples := int(float64(baseSamples) * ss.adaptiveRate)
	if effectiveSamples < 1 {
		effectiveSamples = 1
	}
	if effectiveSamples > ss.config.NumberOfColumns {
		effectiveSamples = ss.config.NumberOfColumns
	}

	// Select target columns deterministically.
	targets := selectSchedulerColumns(ss.nodeID, slot, effectiveSamples, ss.config.NumberOfColumns)

	round := &SamplingRound{
		Slot:           slot,
		Mode:           mode,
		TargetColumns:  targets,
		SampledColumns: make(map[ColumnIndex]bool),
		SuccessColumns: make(map[ColumnIndex]bool),
		FailedColumns:  make(map[ColumnIndex]bool),
		Quota:          effectiveSamples,
		StartedAt:      time.Now(),
	}
	ss.rounds[slot] = round

	// Evict old slots if needed.
	ss.evictOldRoundsLocked()

	return round, nil
}

// RecordSample records a successful or failed sample for a slot.
func (ss *SamplingScheduler) RecordSample(slot uint64, col ColumnIndex, success bool) error {
	if uint64(col) >= uint64(ss.config.NumberOfColumns) {
		return fmt.Errorf("%w: %d >= %d", ErrSchedColumnOOB, col, ss.config.NumberOfColumns)
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.closed {
		return ErrSchedClosed
	}

	round, ok := ss.rounds[slot]
	if !ok {
		return ErrSchedNoActiveRound
	}
	if round.Complete {
		return ErrSchedRoundComplete
	}

	round.SampledColumns[col] = true
	if success {
		round.SuccessColumns[col] = true
	} else {
		round.FailedColumns[col] = true
	}

	round.Quota--
	if round.Quota <= 0 {
		round.Quota = 0
	}

	// Check if round is complete (all target columns sampled).
	allSampled := true
	for _, target := range round.TargetColumns {
		if !round.SampledColumns[target] {
			allSampled = false
			break
		}
	}
	if allSampled {
		round.Complete = true
		round.CompletedAt = time.Now()
		ss.updateAdaptiveRateLocked()
	}

	return nil
}

// CompleteRound forces a round to complete.
func (ss *SamplingScheduler) CompleteRound(slot uint64) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.closed {
		return ErrSchedClosed
	}

	round, ok := ss.rounds[slot]
	if !ok {
		return ErrSchedNoActiveRound
	}
	if round.Complete {
		return nil
	}

	round.Complete = true
	round.CompletedAt = time.Now()
	ss.updateAdaptiveRateLocked()
	return nil
}

// GetRound returns the sampling round for a slot.
func (ss *SamplingScheduler) GetRound(slot uint64) *SamplingRound {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.rounds[slot]
}

// IsRoundComplete returns true if the round for the given slot is complete.
func (ss *SamplingScheduler) IsRoundComplete(slot uint64) bool {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	round, ok := ss.rounds[slot]
	if !ok {
		return false
	}
	return round.Complete
}

// RemainingQuota returns the remaining quota for a slot.
func (ss *SamplingScheduler) RemainingQuota(slot uint64) int {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	round, ok := ss.rounds[slot]
	if !ok {
		return 0
	}
	return round.Quota
}

// RoundSuccessRate returns the success rate for a specific round.
func (ss *SamplingScheduler) RoundSuccessRate(slot uint64) float64 {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	round, ok := ss.rounds[slot]
	if !ok {
		return 0
	}
	total := len(round.SampledColumns)
	if total == 0 {
		return 0
	}
	return float64(len(round.SuccessColumns)) / float64(total)
}

// UnsampledColumns returns the columns that haven't been sampled yet for a slot.
func (ss *SamplingScheduler) UnsampledColumns(slot uint64) []ColumnIndex {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	round, ok := ss.rounds[slot]
	if !ok {
		return nil
	}

	var unsampled []ColumnIndex
	for _, col := range round.TargetColumns {
		if !round.SampledColumns[col] {
			unsampled = append(unsampled, col)
		}
	}
	return unsampled
}

// GetStats returns aggregated sampling statistics.
func (ss *SamplingScheduler) GetStats() SamplingStats {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	stats := SamplingStats{
		TotalRounds:         len(ss.rounds),
		CurrentAdaptiveRate: ss.adaptiveRate,
	}

	for _, round := range ss.rounds {
		if round.Complete {
			stats.CompletedRounds++
		}
		stats.TotalSamples += len(round.SampledColumns)
		stats.TotalSuccesses += len(round.SuccessColumns)
		stats.TotalFailures += len(round.FailedColumns)
	}

	if stats.TotalSamples > 0 {
		stats.SuccessRate = float64(stats.TotalSuccesses) / float64(stats.TotalSamples)
	}

	return stats
}

// AdaptiveRate returns the current adaptive rate multiplier.
func (ss *SamplingScheduler) AdaptiveRate() float64 {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.adaptiveRate
}

// SetAdaptiveRate manually sets the adaptive rate. Useful for testing.
func (ss *SamplingScheduler) SetAdaptiveRate(rate float64) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if rate < ss.config.AdaptiveMinRate {
		rate = ss.config.AdaptiveMinRate
	}
	if rate > ss.config.AdaptiveMaxRate {
		rate = ss.config.AdaptiveMaxRate
	}
	ss.adaptiveRate = rate
}

// IsCustodyColumn returns true if the given column is in this node's custody.
func (ss *SamplingScheduler) IsCustodyColumn(col ColumnIndex) bool {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.custodyColumns[col]
}

// ActiveRoundCount returns the number of active (non-complete) rounds.
func (ss *SamplingScheduler) ActiveRoundCount() int {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	count := 0
	for _, r := range ss.rounds {
		if !r.Complete {
			count++
		}
	}
	return count
}

// PruneCompleted removes all completed rounds from the scheduler.
func (ss *SamplingScheduler) PruneCompleted() int {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	pruned := 0
	for slot, round := range ss.rounds {
		if round.Complete {
			delete(ss.rounds, slot)
			pruned++
		}
	}
	return pruned
}

// Close shuts down the scheduler.
func (ss *SamplingScheduler) Close() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.closed = true
}

// --- internal helpers ---

// updateAdaptiveRateLocked adjusts the adaptive rate based on recent
// success rates. Caller must hold ss.mu.
func (ss *SamplingScheduler) updateAdaptiveRateLocked() {
	// Compute recent success rate from completed rounds.
	var totalSamples, totalSuccesses int
	for _, round := range ss.rounds {
		if round.Complete {
			totalSamples += len(round.SampledColumns)
			totalSuccesses += len(round.SuccessColumns)
		}
	}

	if totalSamples == 0 {
		return
	}

	rate := float64(totalSuccesses) / float64(totalSamples)
	ss.recentSuccessRate = rate

	// Adjust adaptive rate:
	// - Low success: increase sampling (up to max).
	// - High success: decrease sampling (down to min).
	if rate < ss.config.SuccessRateThreshold {
		// Increase by 20%.
		ss.adaptiveRate *= 1.2
	} else if rate > ss.config.HighSuccessThreshold {
		// Decrease by 10%.
		ss.adaptiveRate *= 0.9
	}

	// Clamp to bounds.
	if ss.adaptiveRate < ss.config.AdaptiveMinRate {
		ss.adaptiveRate = ss.config.AdaptiveMinRate
	}
	if ss.adaptiveRate > ss.config.AdaptiveMaxRate {
		ss.adaptiveRate = ss.config.AdaptiveMaxRate
	}
}

// evictOldRoundsLocked removes oldest rounds if we exceed MaxConcurrentSlots.
// Caller must hold ss.mu.
func (ss *SamplingScheduler) evictOldRoundsLocked() {
	if len(ss.rounds) <= ss.config.MaxConcurrentSlots {
		return
	}

	// Collect slots and sort, remove oldest.
	slots := make([]uint64, 0, len(ss.rounds))
	for s := range ss.rounds {
		slots = append(slots, s)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i] < slots[j] })

	for len(slots) > ss.config.MaxConcurrentSlots {
		delete(ss.rounds, slots[0])
		slots = slots[1:]
	}
}

// selectSchedulerColumns selects columns for a sampling round using a
// deterministic hash chain.
func selectSchedulerColumns(nodeID [32]byte, slot uint64, count int, totalColumns int) []ColumnIndex {
	if count <= 0 || totalColumns <= 0 {
		return nil
	}
	if count > totalColumns {
		count = totalColumns
	}

	h := sha3.NewLegacyKeccak256()
	h.Write(nodeID[:])
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], slot)
	h.Write(buf[:])
	// Add a domain separator for the scheduler.
	h.Write([]byte("das/scheduler"))
	seed := h.Sum(nil)

	seen := make(map[ColumnIndex]bool, count)
	result := make([]ColumnIndex, 0, count)

	for counter := uint64(0); len(result) < count; counter++ {
		sh := sha3.NewLegacyKeccak256()
		sh.Write(seed)
		var cBuf [8]byte
		binary.LittleEndian.PutUint64(cBuf[:], counter)
		sh.Write(cBuf[:])
		digest := sh.Sum(nil)

		val := binary.LittleEndian.Uint64(digest[:8])
		col := ColumnIndex(val % uint64(totalColumns))

		if !seen[col] {
			seen[col] = true
			result = append(result, col)
		}
	}

	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
