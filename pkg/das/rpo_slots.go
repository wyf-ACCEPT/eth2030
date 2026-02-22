package das

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// RPO management errors.
var (
	ErrRPOBelowMin       = errors.New("das/rpo: value below minimum RPO")
	ErrRPOAboveMax       = errors.New("das/rpo: value above maximum RPO")
	ErrRPOStepTooLarge   = errors.New("das/rpo: transition exceeds maximum step size")
	ErrRPONotIncreasing  = errors.New("das/rpo: new RPO must be greater than current")
	ErrRPOScheduleEmpty  = errors.New("das/rpo: schedule must not be empty")
	ErrRPOScheduleOrder  = errors.New("das/rpo: schedule epochs must be strictly increasing")
	ErrRPOScheduleValues = errors.New("das/rpo: schedule RPO values must be non-decreasing")
)

// RPOConfig configures the RPO manager.
type RPOConfig struct {
	// InitialRPO is the starting rows-per-operation value.
	InitialRPO uint64

	// MaxRPO is the upper bound on RPO.
	MaxRPO uint64

	// MinRPO is the lower bound on RPO.
	MinRPO uint64

	// RPOStepSize is the maximum allowed increase per transition.
	RPOStepSize uint64
}

// DefaultRPOConfig returns sensible defaults for the current PeerDAS spec.
func DefaultRPOConfig() RPOConfig {
	return RPOConfig{
		InitialRPO:  4,
		MaxRPO:      64,
		MinRPO:      1,
		RPOStepSize: 8,
	}
}

// ThroughputEstimate describes the expected throughput at a given RPO.
type ThroughputEstimate struct {
	// BlobsPerSlot is the number of blobs processed per slot.
	BlobsPerSlot uint64

	// DataRateKBps is the estimated data rate in kilobytes per second.
	DataRateKBps uint64

	// SamplesNeeded is the number of DAS samples needed at this throughput.
	SamplesNeeded uint64

	// ValidationTimeMs is the estimated validation time in milliseconds.
	ValidationTimeMs uint64
}

// RPOSchedule describes a planned RPO transition at a given epoch.
type RPOSchedule struct {
	// Epoch at which the RPO value takes effect.
	Epoch uint64

	// TargetRPO is the RPO value starting from Epoch.
	TargetRPO uint64

	// Description is a human-readable note about this transition.
	Description string
}

// RPOHistoryEntry records a single RPO change event.
type RPOHistoryEntry struct {
	// Epoch at which the change occurred.
	Epoch uint64

	// OldRPO is the RPO value before the change.
	OldRPO uint64

	// NewRPO is the RPO value after the change.
	NewRPO uint64
}

// RPOManager manages rows-per-operation values for DAS blob throughput.
// It tracks the current RPO, enforces transition rules, maintains an
// upgrade schedule, and records a history of changes. All methods are
// safe for concurrent use.
type RPOManager struct {
	mu       sync.RWMutex
	config   RPOConfig
	current  uint64
	schedule []*RPOSchedule
	history  []*RPOHistoryEntry
}

// NewRPOManager creates a new RPO manager with the given configuration.
// Zero-value fields in config are replaced with defaults.
func NewRPOManager(config RPOConfig) *RPOManager {
	if config.MinRPO == 0 {
		config.MinRPO = 1
	}
	if config.MaxRPO == 0 {
		config.MaxRPO = 64
	}
	if config.MaxRPO < config.MinRPO {
		config.MaxRPO = config.MinRPO
	}
	if config.InitialRPO < config.MinRPO {
		config.InitialRPO = config.MinRPO
	}
	if config.InitialRPO > config.MaxRPO {
		config.InitialRPO = config.MaxRPO
	}
	if config.RPOStepSize == 0 {
		config.RPOStepSize = 8
	}
	return &RPOManager{
		config:  config,
		current: config.InitialRPO,
	}
}

// CurrentRPO returns the current RPO value.
func (rm *RPOManager) CurrentRPO() uint64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.current
}

// IncreaseRPO attempts to increase the RPO to newRPO. It validates the
// transition and records it in history. The epoch recorded is derived from
// the history length (callers should use SetSchedule for epoch-aware changes).
func (rm *RPOManager) IncreaseRPO(newRPO uint64) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if err := rm.validateTransitionLocked(rm.current, newRPO); err != nil {
		return err
	}

	oldRPO := rm.current
	rm.current = newRPO
	rm.history = append(rm.history, &RPOHistoryEntry{
		Epoch:  uint64(len(rm.history)),
		OldRPO: oldRPO,
		NewRPO: newRPO,
	})
	return nil
}

// ValidateRPOTransition checks whether transitioning from currentRPO to
// targetRPO is valid under the current config. It does not modify state.
func (rm *RPOManager) ValidateRPOTransition(currentRPO, targetRPO uint64) error {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.validateTransitionLocked(currentRPO, targetRPO)
}

// validateTransitionLocked performs transition validation. Caller must hold
// at least a read lock.
func (rm *RPOManager) validateTransitionLocked(currentRPO, targetRPO uint64) error {
	if targetRPO < rm.config.MinRPO {
		return fmt.Errorf("%w: %d < %d", ErrRPOBelowMin, targetRPO, rm.config.MinRPO)
	}
	if targetRPO > rm.config.MaxRPO {
		return fmt.Errorf("%w: %d > %d", ErrRPOAboveMax, targetRPO, rm.config.MaxRPO)
	}
	if targetRPO <= currentRPO {
		return fmt.Errorf("%w: %d <= %d", ErrRPONotIncreasing, targetRPO, currentRPO)
	}
	step := targetRPO - currentRPO
	if step > rm.config.RPOStepSize {
		return fmt.Errorf("%w: step %d > max %d", ErrRPOStepTooLarge, step, rm.config.RPOStepSize)
	}
	return nil
}

// CalculateThroughput estimates network throughput at the given RPO value.
// The model assumes each row adds one blob worth of data per slot, and
// sampling requirements scale linearly with RPO.
func (rm *RPOManager) CalculateThroughput(rpo uint64) *ThroughputEstimate {
	if rpo == 0 {
		rpo = 1
	}
	// Each RPO unit contributes one blob per slot.
	blobsPerSlot := rpo
	if blobsPerSlot > uint64(MaxBlobCommitmentsPerBlock) {
		blobsPerSlot = uint64(MaxBlobCommitmentsPerBlock)
	}

	// Data rate: blobs * bytes per blob / slot time.
	// bytes per blob = FieldElementsPerBlob * BytesPerFieldElement = 131072 (128 KiB).
	bytesPerBlob := uint64(FieldElementsPerBlob * BytesPerFieldElement)
	slotTimeSec := uint64(12) // 12 second slots
	dataRateKBps := (blobsPerSlot * bytesPerBlob) / (slotTimeSec * 1024)

	// Samples scale: base SamplesPerSlot * sqrt(rpo) to account for
	// diminishing marginal security gain per additional row.
	baseSamples := uint64(SamplesPerSlot)
	samplesNeeded := baseSamples * isqrt(rpo)
	if samplesNeeded > NumberOfColumns {
		samplesNeeded = NumberOfColumns
	}

	// Validation time: linear in blobs, ~5ms per blob for KZG checks.
	validationTimeMs := blobsPerSlot * 5

	return &ThroughputEstimate{
		BlobsPerSlot:     blobsPerSlot,
		DataRateKBps:     dataRateKBps,
		SamplesNeeded:    samplesNeeded,
		ValidationTimeMs: validationTimeMs,
	}
}

// isqrt returns the integer square root (floor) of n.
func isqrt(n uint64) uint64 {
	if n <= 1 {
		return n
	}
	x := n
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}

// SetSchedule installs a sequence of planned RPO transitions. Epochs must
// be strictly increasing and RPO values must be non-decreasing.
func (rm *RPOManager) SetSchedule(schedule []*RPOSchedule) error {
	if len(schedule) == 0 {
		return ErrRPOScheduleEmpty
	}

	// Validate ordering.
	for i := 1; i < len(schedule); i++ {
		if schedule[i].Epoch <= schedule[i-1].Epoch {
			return fmt.Errorf("%w: epoch %d <= %d",
				ErrRPOScheduleOrder, schedule[i].Epoch, schedule[i-1].Epoch)
		}
		if schedule[i].TargetRPO < schedule[i-1].TargetRPO {
			return fmt.Errorf("%w: RPO %d < %d at epoch %d",
				ErrRPOScheduleValues, schedule[i].TargetRPO,
				schedule[i-1].TargetRPO, schedule[i].Epoch)
		}
	}

	// Validate each target RPO against config bounds.
	for _, s := range schedule {
		if s.TargetRPO < rm.config.MinRPO {
			return fmt.Errorf("%w: %d < %d at epoch %d",
				ErrRPOBelowMin, s.TargetRPO, rm.config.MinRPO, s.Epoch)
		}
		if s.TargetRPO > rm.config.MaxRPO {
			return fmt.Errorf("%w: %d > %d at epoch %d",
				ErrRPOAboveMax, s.TargetRPO, rm.config.MaxRPO, s.Epoch)
		}
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Copy to avoid external mutation.
	rm.schedule = make([]*RPOSchedule, len(schedule))
	for i, s := range schedule {
		cp := *s
		rm.schedule[i] = &cp
	}
	return nil
}

// GetScheduledRPO returns the RPO that should be active at the given epoch
// by looking up the schedule. If the epoch precedes all scheduled entries,
// the current RPO is returned.
func (rm *RPOManager) GetScheduledRPO(epoch uint64) uint64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if len(rm.schedule) == 0 {
		return rm.current
	}

	// Find the last schedule entry whose Epoch <= the query epoch.
	idx := sort.Search(len(rm.schedule), func(i int) bool {
		return rm.schedule[i].Epoch > epoch
	})
	if idx == 0 {
		return rm.current
	}
	return rm.schedule[idx-1].TargetRPO
}

// ValidateBlobSchedule checks that an RPO schedule has monotonically increasing
// epochs and non-decreasing RPO values within bounds.
func ValidateBlobSchedule(schedule []*RPOSchedule, config RPOConfig) error {
	if len(schedule) == 0 {
		return ErrRPOScheduleEmpty
	}
	for i, s := range schedule {
		if s.TargetRPO < config.MinRPO {
			return fmt.Errorf("%w: %d < %d at epoch %d", ErrRPOBelowMin, s.TargetRPO, config.MinRPO, s.Epoch)
		}
		if s.TargetRPO > config.MaxRPO {
			return fmt.Errorf("%w: %d > %d at epoch %d", ErrRPOAboveMax, s.TargetRPO, config.MaxRPO, s.Epoch)
		}
		if i > 0 {
			if schedule[i].Epoch <= schedule[i-1].Epoch {
				return ErrRPOScheduleOrder
			}
			if schedule[i].TargetRPO < schedule[i-1].TargetRPO {
				return ErrRPOScheduleValues
			}
		}
	}
	return nil
}

// BPO3Schedule returns the J+ phase BPO blob schedule. J+ (2027-2028)
// increases blob targets beyond BPO2, enabling higher data throughput
// for variable-size blobs, Reed-Solomon blob reconstruction, and
// block-in-blobs encoding. Target blobs rise to 48 (max 64).
func BPO3Schedule() []*RPOSchedule {
	return []*RPOSchedule{
		{Epoch: 300000, TargetRPO: 32, Description: "J+ BPO3: target 48 blobs (32 RPO)"},
		{Epoch: 350000, TargetRPO: 48, Description: "J+ BPO3: target 64 blobs (48 RPO)"},
	}
}

// BPO4Schedule returns the L+ phase BPO blob schedule. L+ (2029) further
// increases blob targets toward teragas L2 throughput, with BPO blobs
// increase enabling higher sustained data rates. Target blobs rise to 64+.
func BPO4Schedule() []*RPOSchedule {
	return []*RPOSchedule{
		{Epoch: 500000, TargetRPO: 48, Description: "L+ BPO4: target 96 blobs (48 RPO)"},
		{Epoch: 600000, TargetRPO: 64, Description: "L+ BPO4: target 128 blobs (64 RPO)"},
	}
}

// MergeBPOSchedules combines multiple BPO schedule phases into a single
// schedule, verifying epoch monotonicity and RPO non-decreasing invariants.
func MergeBPOSchedules(phases ...[]*RPOSchedule) ([]*RPOSchedule, error) {
	var merged []*RPOSchedule
	for _, phase := range phases {
		merged = append(merged, phase...)
	}
	if len(merged) == 0 {
		return nil, ErrRPOScheduleEmpty
	}
	// Validate epoch ordering and RPO progression.
	for i := 1; i < len(merged); i++ {
		if merged[i].Epoch <= merged[i-1].Epoch {
			return nil, ErrRPOScheduleOrder
		}
		if merged[i].TargetRPO < merged[i-1].TargetRPO {
			return nil, ErrRPOScheduleValues
		}
	}
	return merged, nil
}

// GetHistory returns a copy of the RPO change history.
func (rm *RPOManager) GetHistory() []*RPOHistoryEntry {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make([]*RPOHistoryEntry, len(rm.history))
	for i, h := range rm.history {
		cp := *h
		result[i] = &cp
	}
	return result
}
