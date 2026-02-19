package core

import (
	"errors"
	"math"
	"sync"
)

// NaturalGasConfig holds configuration for the natural gas limit adjustment.
type NaturalGasConfig struct {
	// TargetUtilization is the target block gas utilization ratio (default 0.5).
	TargetUtilization float64
	// AdjustmentSpeed controls how fast the gas limit adjusts (units per block).
	// A higher value means faster convergence toward the target utilization.
	AdjustmentSpeed uint64
	// MinGasLimit is the absolute minimum gas limit.
	MinGasLimit uint64
	// MaxGasLimit is the absolute maximum gas limit (0 means no max).
	MaxGasLimit uint64
}

// DefaultNaturalGasConfig returns the default natural gas configuration
// following EIP-1559 style elastic gas limit adjustment.
func DefaultNaturalGasConfig() NaturalGasConfig {
	return NaturalGasConfig{
		TargetUtilization: 0.5,
		AdjustmentSpeed:   8,
		MinGasLimit:       5000,
		MaxGasLimit:       0, // no upper bound by default
	}
}

// GasLimitHistoryEntry records gas data for a single block.
type GasLimitHistoryEntry struct {
	BlockNumber uint64
	GasLimit    uint64
	GasUsed     uint64
	Utilization float64
}

// GasLimitHistory tracks recent gas limit changes.
type GasLimitHistory struct {
	mu      sync.RWMutex
	entries []*GasLimitHistoryEntry
	maxSize int
}

// NewGasLimitHistory creates a history buffer that retains at most maxSize entries.
func NewGasLimitHistory(maxSize int) *GasLimitHistory {
	if maxSize <= 0 {
		maxSize = 1024
	}
	return &GasLimitHistory{
		entries: make([]*GasLimitHistoryEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// RecordBlock records a block's gas data into the history.
func (h *GasLimitHistory) RecordBlock(blockNum, gasLimit, gasUsed uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var util float64
	if gasLimit > 0 {
		util = float64(gasUsed) / float64(gasLimit)
	}
	entry := &GasLimitHistoryEntry{
		BlockNumber: blockNum,
		GasLimit:    gasLimit,
		GasUsed:     gasUsed,
		Utilization: util,
	}
	if len(h.entries) >= h.maxSize {
		// Evict oldest entry.
		copy(h.entries, h.entries[1:])
		h.entries[len(h.entries)-1] = entry
	} else {
		h.entries = append(h.entries, entry)
	}
}

// GetHistory returns the most recent N entries. If fewer than N entries exist,
// all available entries are returned.
func (h *GasLimitHistory) GetHistory(blocks uint64) []*GasLimitHistoryEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	n := int(blocks)
	if n > len(h.entries) {
		n = len(h.entries)
	}
	if n == 0 {
		return nil
	}
	start := len(h.entries) - n
	out := make([]*GasLimitHistoryEntry, n)
	copy(out, h.entries[start:])
	return out
}

// AverageUtilization returns the average gas utilization over the last N blocks.
// Returns 0 if no history is available.
func (h *GasLimitHistory) AverageUtilization(blocks uint64) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	n := int(blocks)
	if n > len(h.entries) {
		n = len(h.entries)
	}
	if n == 0 {
		return 0
	}
	start := len(h.entries) - n
	var sum float64
	for _, e := range h.entries[start:] {
		sum += e.Utilization
	}
	return sum / float64(n)
}

// Len returns the number of entries in history.
func (h *GasLimitHistory) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.entries)
}

// NaturalGasManager manages natural gas limit adjustments.
// It implements EIP-1559 style elastic gas limits that naturally adjust
// based on network utilization.
type NaturalGasManager struct {
	mu      sync.RWMutex
	config  NaturalGasConfig
	history *GasLimitHistory
}

// NewNaturalGasManager creates a new manager with the given configuration.
func NewNaturalGasManager(config NaturalGasConfig) *NaturalGasManager {
	// Apply defaults for zero-valued fields.
	if config.TargetUtilization <= 0 || config.TargetUtilization > 1.0 {
		config.TargetUtilization = 0.5
	}
	if config.AdjustmentSpeed == 0 {
		config.AdjustmentSpeed = 8
	}
	if config.MinGasLimit == 0 {
		config.MinGasLimit = 5000
	}
	return &NaturalGasManager{
		config:  config,
		history: NewGasLimitHistory(4096),
	}
}

// CalculateTargetGasLimit calculates the target gas limit for the next block
// based on the parent block's gas limit and gas used.
//
// The adjustment follows EIP-1559 style elasticity:
//   - If gas used > target (limit * targetUtilization): increase gas limit
//   - If gas used < target: decrease gas limit
//   - The rate of change is bounded by parentGasLimit / (adjustmentSpeed * 1024)
//
// The result is clamped to [MinGasLimit, MaxGasLimit].
func (m *NaturalGasManager) CalculateTargetGasLimit(parentGasLimit, parentGasUsed uint64) uint64 {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	target := uint64(float64(parentGasLimit) * cfg.TargetUtilization)
	if target == 0 {
		target = 1
	}

	// Maximum per-block change: parentGasLimit / (adjustmentSpeed * 1024).
	denominator := uint64(cfg.AdjustmentSpeed) * 1024
	if denominator == 0 {
		denominator = 8192
	}
	maxDelta := parentGasLimit / denominator
	if maxDelta == 0 {
		maxDelta = 1
	}

	var newLimit uint64
	if parentGasUsed > target {
		// Over-utilized: increase gas limit.
		excess := parentGasUsed - target
		// Scale delta proportionally: delta = maxDelta * excess / target.
		delta := maxDelta * excess / target
		if delta > maxDelta {
			delta = maxDelta
		}
		if delta == 0 {
			delta = 1
		}
		newLimit = parentGasLimit + delta
	} else if parentGasUsed < target {
		// Under-utilized: decrease gas limit.
		deficit := target - parentGasUsed
		delta := maxDelta * deficit / target
		if delta > maxDelta {
			delta = maxDelta
		}
		if delta == 0 {
			delta = 1
		}
		if parentGasLimit > delta {
			newLimit = parentGasLimit - delta
		} else {
			newLimit = cfg.MinGasLimit
		}
	} else {
		// Exactly at target.
		newLimit = parentGasLimit
	}

	// Clamp to bounds.
	if newLimit < cfg.MinGasLimit {
		newLimit = cfg.MinGasLimit
	}
	if cfg.MaxGasLimit > 0 && newLimit > cfg.MaxGasLimit {
		newLimit = cfg.MaxGasLimit
	}

	return newLimit
}

// Sentinel errors for gas limit validation.
var (
	ErrGasLimitTooLow    = errors.New("gas limit below minimum")
	ErrGasLimitTooHigh   = errors.New("gas limit above maximum")
	ErrGasLimitDeltaHigh = errors.New("gas limit change exceeds maximum allowed delta")
)

// ValidateGasLimit validates that a proposed new gas limit is a valid
// transition from the parent gas limit. The change must not exceed the
// maximum allowed delta of parentGasLimit/1024, and the new limit must
// be within [MinGasLimit, MaxGasLimit].
func (m *NaturalGasManager) ValidateGasLimit(parentGasLimit, newGasLimit uint64) error {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	if newGasLimit < cfg.MinGasLimit {
		return ErrGasLimitTooLow
	}
	if cfg.MaxGasLimit > 0 && newGasLimit > cfg.MaxGasLimit {
		return ErrGasLimitTooHigh
	}

	// Maximum delta: parentGasLimit / 1024 (the protocol bound).
	maxDelta := parentGasLimit / 1024
	if maxDelta == 0 {
		maxDelta = 1
	}

	var diff uint64
	if newGasLimit > parentGasLimit {
		diff = newGasLimit - parentGasLimit
	} else {
		diff = parentGasLimit - newGasLimit
	}

	if diff > maxDelta {
		return ErrGasLimitDeltaHigh
	}
	return nil
}

// AdjustmentFactor computes the adjustment factor for a block based on its
// gas utilization. Returns a value centered around 0:
//   - positive when utilization > target (signal to increase)
//   - negative when utilization < target (signal to decrease)
//   - 0 when utilization == target
//
// The magnitude is clamped to [-1.0, 1.0].
func (m *NaturalGasManager) AdjustmentFactor(gasUsed, gasLimit uint64) float64 {
	m.mu.RLock()
	targetUtil := m.config.TargetUtilization
	m.mu.RUnlock()

	if gasLimit == 0 {
		return 0
	}
	utilization := float64(gasUsed) / float64(gasLimit)
	factor := (utilization - targetUtil) / targetUtil

	// Clamp to [-1.0, 1.0].
	if factor > 1.0 {
		factor = 1.0
	} else if factor < -1.0 {
		factor = -1.0
	}
	return factor
}

// ProjectGasLimit projects the gas limit N blocks into the future, assuming
// every block uses exactly target utilization (i.e., the gas limit stays stable).
// If utilization deviates, the projection shows the convergence path.
// This uses the current gas limit as the starting point.
func (m *NaturalGasManager) ProjectGasLimit(currentLimit uint64, blocks uint64) uint64 {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	limit := currentLimit
	// Simulate N blocks at exactly target utilization.
	// At target utilization, the gas limit should remain stable.
	// We model a slight drift toward equilibrium for non-target states.
	targetGasUsed := uint64(float64(limit) * cfg.TargetUtilization)
	for i := uint64(0); i < blocks; i++ {
		limit = m.calculateTargetInternal(limit, targetGasUsed, cfg)
		// Recalculate target gas used for the new limit.
		targetGasUsed = uint64(float64(limit) * cfg.TargetUtilization)
	}
	return limit
}

// calculateTargetInternal is the non-locking core calculation.
func (m *NaturalGasManager) calculateTargetInternal(parentGasLimit, parentGasUsed uint64, cfg NaturalGasConfig) uint64 {
	target := uint64(float64(parentGasLimit) * cfg.TargetUtilization)
	if target == 0 {
		target = 1
	}

	denominator := uint64(cfg.AdjustmentSpeed) * 1024
	if denominator == 0 {
		denominator = 8192
	}
	maxDelta := parentGasLimit / denominator
	if maxDelta == 0 {
		maxDelta = 1
	}

	var newLimit uint64
	if parentGasUsed > target {
		excess := parentGasUsed - target
		delta := maxDelta * excess / target
		if delta > maxDelta {
			delta = maxDelta
		}
		if delta == 0 {
			delta = 1
		}
		newLimit = parentGasLimit + delta
	} else if parentGasUsed < target {
		deficit := target - parentGasUsed
		delta := maxDelta * deficit / target
		if delta > maxDelta {
			delta = maxDelta
		}
		if delta == 0 {
			delta = 1
		}
		if parentGasLimit > delta {
			newLimit = parentGasLimit - delta
		} else {
			newLimit = cfg.MinGasLimit
		}
	} else {
		newLimit = parentGasLimit
	}

	if newLimit < cfg.MinGasLimit {
		newLimit = cfg.MinGasLimit
	}
	if cfg.MaxGasLimit > 0 && newLimit > cfg.MaxGasLimit {
		newLimit = cfg.MaxGasLimit
	}
	return newLimit
}

// RecordBlock records a block's gas data in the manager's internal history.
func (m *NaturalGasManager) RecordBlock(blockNum, gasLimit, gasUsed uint64) {
	m.history.RecordBlock(blockNum, gasLimit, gasUsed)
}

// GetHistory returns the most recent N entries from the gas limit history.
func (m *NaturalGasManager) GetHistory(blocks uint64) []*GasLimitHistoryEntry {
	return m.history.GetHistory(blocks)
}

// AverageUtilization returns the average gas utilization over the last N blocks.
func (m *NaturalGasManager) AverageUtilization(blocks uint64) float64 {
	return m.history.AverageUtilization(blocks)
}

// Config returns the current configuration (copy).
func (m *NaturalGasManager) Config() NaturalGasConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// clampFloat64 clamps v to [lo, hi].
func clampFloat64(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}
