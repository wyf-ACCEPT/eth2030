// Package das - throughput_manager.go implements blob throughput scaling per
// the Ethereum 2030 roadmap. It dynamically adjusts the blob limit per block
// based on network utilization, scaling up when demand is high and scaling
// down when utilization drops.
package das

import (
	"errors"
	"fmt"
	"sync"
)

// Throughput manager errors.
var (
	ErrInvalidThroughputConfig = errors.New("das: invalid throughput config")
	ErrSlotNotMonotonic        = errors.New("das: slot number must be monotonically increasing")
)

// ThroughputConfig defines the parameters for dynamic blob throughput scaling.
type ThroughputConfig struct {
	// BaseBlobsPerBlock is the starting blob limit per block.
	BaseBlobsPerBlock uint64
	// MaxBlobsPerBlock is the absolute maximum blob limit.
	MaxBlobsPerBlock uint64
	// MinBlobsPerBlock is the absolute minimum blob limit.
	MinBlobsPerBlock uint64
	// ScaleUpThreshold is the utilization ratio (0.0-1.0) above which
	// the limit is increased after sustained high usage.
	ScaleUpThreshold float64
	// ScaleDownThreshold is the utilization ratio (0.0-1.0) below which
	// the limit is decreased after sustained low usage.
	ScaleDownThreshold float64
	// EpochsPerAdjustment is the number of epochs that must elapse
	// before a throughput adjustment is considered.
	EpochsPerAdjustment uint64
	// SlotsPerEpoch is the number of slots per epoch.
	SlotsPerEpoch uint64
	// StepSize is how many blobs to add/remove per adjustment.
	StepSize uint64
}

// DefaultThroughputConfig returns a sensible default configuration
// aligned with the current PeerDAS parameters.
func DefaultThroughputConfig() ThroughputConfig {
	return ThroughputConfig{
		BaseBlobsPerBlock:   6,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 4,
		SlotsPerEpoch:       32,
		StepSize:            1,
	}
}

// Validate checks that the ThroughputConfig has consistent parameters.
func (c *ThroughputConfig) Validate() error {
	if c.BaseBlobsPerBlock == 0 {
		return fmt.Errorf("%w: base blobs must be > 0", ErrInvalidThroughputConfig)
	}
	if c.MaxBlobsPerBlock == 0 {
		return fmt.Errorf("%w: max blobs must be > 0", ErrInvalidThroughputConfig)
	}
	if c.MinBlobsPerBlock > c.MaxBlobsPerBlock {
		return fmt.Errorf("%w: min %d > max %d", ErrInvalidThroughputConfig,
			c.MinBlobsPerBlock, c.MaxBlobsPerBlock)
	}
	if c.BaseBlobsPerBlock < c.MinBlobsPerBlock || c.BaseBlobsPerBlock > c.MaxBlobsPerBlock {
		return fmt.Errorf("%w: base %d not in [%d, %d]", ErrInvalidThroughputConfig,
			c.BaseBlobsPerBlock, c.MinBlobsPerBlock, c.MaxBlobsPerBlock)
	}
	if c.ScaleUpThreshold <= 0 || c.ScaleUpThreshold > 1.0 {
		return fmt.Errorf("%w: scale up threshold must be in (0, 1]", ErrInvalidThroughputConfig)
	}
	if c.ScaleDownThreshold < 0 || c.ScaleDownThreshold >= 1.0 {
		return fmt.Errorf("%w: scale down threshold must be in [0, 1)", ErrInvalidThroughputConfig)
	}
	if c.ScaleDownThreshold >= c.ScaleUpThreshold {
		return fmt.Errorf("%w: scale down threshold %f >= scale up threshold %f",
			ErrInvalidThroughputConfig, c.ScaleDownThreshold, c.ScaleUpThreshold)
	}
	if c.EpochsPerAdjustment == 0 {
		return fmt.Errorf("%w: epochs per adjustment must be > 0", ErrInvalidThroughputConfig)
	}
	if c.SlotsPerEpoch == 0 {
		return fmt.Errorf("%w: slots per epoch must be > 0", ErrInvalidThroughputConfig)
	}
	if c.StepSize == 0 {
		return fmt.Errorf("%w: step size must be > 0", ErrInvalidThroughputConfig)
	}
	return nil
}

// slotRecord stores blob utilization data for a single slot.
type slotRecord struct {
	slotNumber uint64
	blobCount  uint64
}

// ThroughputManager tracks blob throughput and dynamically adjusts the
// per-block blob limit based on observed utilization patterns.
// It is safe for concurrent use.
type ThroughputManager struct {
	mu     sync.RWMutex
	config ThroughputConfig

	// currentLimit is the current blob limit per block.
	currentLimit uint64

	// history stores utilization records for the current adjustment window.
	history []slotRecord

	// lastSlot tracks the most recently recorded slot for monotonicity.
	lastSlot uint64
	// hasRecords indicates if any records have been added.
	hasRecords bool

	// adjustmentCount tracks total number of adjustments made.
	adjustmentCount uint64
	// scaleUpCount is the number of upward adjustments.
	scaleUpCount uint64
	// scaleDownCount is the number of downward adjustments.
	scaleDownCount uint64
}

// NewThroughputManager creates a new ThroughputManager with the given config.
// Returns an error if the config is invalid.
func NewThroughputManager(config ThroughputConfig) (*ThroughputManager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &ThroughputManager{
		config:       config,
		currentLimit: config.BaseBlobsPerBlock,
		history:      make([]slotRecord, 0, config.SlotsPerEpoch*config.EpochsPerAdjustment),
	}, nil
}

// CurrentLimit returns the current blob limit per block.
func (tm *ThroughputManager) CurrentLimit() uint64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.currentLimit
}

// RecordUtilization records the number of blobs included in a slot.
// The slotNumber must be monotonically increasing.
func (tm *ThroughputManager) RecordUtilization(blobCount, slotNumber uint64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.hasRecords && slotNumber <= tm.lastSlot {
		return fmt.Errorf("%w: slot %d <= last slot %d",
			ErrSlotNotMonotonic, slotNumber, tm.lastSlot)
	}

	// Cap blobCount to current limit for utilization calculation.
	if blobCount > tm.currentLimit {
		blobCount = tm.currentLimit
	}

	tm.history = append(tm.history, slotRecord{
		slotNumber: slotNumber,
		blobCount:  blobCount,
	})
	tm.lastSlot = slotNumber
	tm.hasRecords = true

	return nil
}

// UtilizationRate returns the average blob utilization rate over the
// current history window as a value in [0.0, 1.0].
// Returns 0 if no records exist.
func (tm *ThroughputManager) UtilizationRate() float64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.utilizationRateLocked()
}

// utilizationRateLocked computes utilization rate (caller holds lock).
func (tm *ThroughputManager) utilizationRateLocked() float64 {
	if len(tm.history) == 0 || tm.currentLimit == 0 {
		return 0
	}

	var totalBlobs uint64
	for _, r := range tm.history {
		totalBlobs += r.blobCount
	}

	maxPossible := uint64(len(tm.history)) * tm.currentLimit
	if maxPossible == 0 {
		return 0
	}
	return float64(totalBlobs) / float64(maxPossible)
}

// windowSize returns the number of slots in the adjustment window.
func (tm *ThroughputManager) windowSize() uint64 {
	return tm.config.SlotsPerEpoch * tm.config.EpochsPerAdjustment
}

// AdjustLimit evaluates the current utilization history and adjusts the
// blob limit if the adjustment window is full. Returns true if an
// adjustment was made.
func (tm *ThroughputManager) AdjustLimit() bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	window := tm.windowSize()
	if uint64(len(tm.history)) < window {
		return false
	}

	rate := tm.utilizationRateLocked()
	adjusted := false

	if rate >= tm.config.ScaleUpThreshold {
		newLimit := tm.currentLimit + tm.config.StepSize
		if newLimit > tm.config.MaxBlobsPerBlock {
			newLimit = tm.config.MaxBlobsPerBlock
		}
		if newLimit != tm.currentLimit {
			tm.currentLimit = newLimit
			tm.scaleUpCount++
			tm.adjustmentCount++
			adjusted = true
		}
	} else if rate <= tm.config.ScaleDownThreshold {
		if tm.currentLimit > tm.config.MinBlobsPerBlock {
			var newLimit uint64
			if tm.currentLimit >= tm.config.StepSize {
				newLimit = tm.currentLimit - tm.config.StepSize
			} else {
				newLimit = 0
			}
			if newLimit < tm.config.MinBlobsPerBlock {
				newLimit = tm.config.MinBlobsPerBlock
			}
			if newLimit != tm.currentLimit {
				tm.currentLimit = newLimit
				tm.scaleDownCount++
				tm.adjustmentCount++
				adjusted = true
			}
		}
	}

	// Clear history after evaluation to start a fresh window.
	tm.history = tm.history[:0]

	return adjusted
}

// ThroughputStatus contains the current state of the throughput manager.
type ThroughputStatus struct {
	CurrentLimit    uint64
	UtilizationRate float64
	HistorySize     int
	WindowSize      uint64
	AdjustmentCount uint64
	ScaleUpCount    uint64
	ScaleDownCount  uint64
}

// Status returns the current throughput manager state.
func (tm *ThroughputManager) Status() ThroughputStatus {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return ThroughputStatus{
		CurrentLimit:    tm.currentLimit,
		UtilizationRate: tm.utilizationRateLocked(),
		HistorySize:     len(tm.history),
		WindowSize:      tm.windowSize(),
		AdjustmentCount: tm.adjustmentCount,
		ScaleUpCount:    tm.scaleUpCount,
		ScaleDownCount:  tm.scaleDownCount,
	}
}

// Config returns a copy of the throughput configuration.
func (tm *ThroughputManager) Config() ThroughputConfig {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.config
}

// Reset restores the manager to its initial state with the base blob limit.
func (tm *ThroughputManager) Reset() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.currentLimit = tm.config.BaseBlobsPerBlock
	tm.history = tm.history[:0]
	tm.lastSlot = 0
	tm.hasRecords = false
	tm.adjustmentCount = 0
	tm.scaleUpCount = 0
	tm.scaleDownCount = 0
}

// SetLimit manually overrides the current blob limit. The value is clamped
// to [MinBlobsPerBlock, MaxBlobsPerBlock].
func (tm *ThroughputManager) SetLimit(limit uint64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if limit < tm.config.MinBlobsPerBlock {
		limit = tm.config.MinBlobsPerBlock
	}
	if limit > tm.config.MaxBlobsPerBlock {
		limit = tm.config.MaxBlobsPerBlock
	}
	tm.currentLimit = limit
}
