// rate_meter.go implements cross-block gas rate metering for the gigagas
// execution target (1 Ggas/sec). It maintains a sliding window of recent
// blocks, computes a rolling average gas rate with EMA smoothing, and
// provides adaptive parallelism recommendations.
package core

import (
	"math"
	"sync"
)

// RateMeterConfig configures the cross-block rate meter.
type RateMeterConfig struct {
	// WindowSize is the number of blocks in the sliding window.
	WindowSize int
	// TargetGasPerSec is the target gas throughput (default 1 Ggas/sec).
	TargetGasPerSec float64
	// EMAAlpha is the smoothing factor for EMA (0 < alpha <= 1).
	// Higher = more weight on recent observations.
	EMAAlpha float64
	// MinWorkers is the minimum number of parallel workers.
	MinWorkers int
	// MaxWorkers is the maximum number of parallel workers.
	MaxWorkers int
}

// DefaultRateMeterConfig returns the default rate meter configuration.
func DefaultRateMeterConfig() RateMeterConfig {
	return RateMeterConfig{
		WindowSize:      64,
		TargetGasPerSec: 1_000_000_000, // 1 Ggas/sec
		EMAAlpha:        0.1,
		MinWorkers:      2,
		MaxWorkers:      128,
	}
}

// blockRecord stores gas measurement for a single block.
type blockRecord struct {
	blockNumber uint64
	gasUsed     uint64
	timestamp   uint64
}

// RateMeter tracks gas rates across blocks with EMA smoothing and
// adaptive parallelism recommendations.
type RateMeter struct {
	mu      sync.Mutex
	config  RateMeterConfig
	records []blockRecord
	emaRate float64 // EMA-smoothed gas rate (gas/sec)
	workers int     // current recommended worker count
}

// NewRateMeter creates a new rate meter with the given configuration.
func NewRateMeter(config RateMeterConfig) *RateMeter {
	if config.WindowSize <= 0 {
		config.WindowSize = 64
	}
	if config.TargetGasPerSec <= 0 {
		config.TargetGasPerSec = 1_000_000_000
	}
	if config.EMAAlpha <= 0 || config.EMAAlpha > 1 {
		config.EMAAlpha = 0.1
	}
	if config.MinWorkers <= 0 {
		config.MinWorkers = 2
	}
	if config.MaxWorkers <= 0 {
		config.MaxWorkers = 128
	}
	if config.MaxWorkers < config.MinWorkers {
		config.MaxWorkers = config.MinWorkers
	}
	return &RateMeter{
		config:  config,
		records: make([]blockRecord, 0, config.WindowSize),
		workers: config.MinWorkers,
	}
}

// RecordBlock records gas usage for a block. Timestamps are in seconds.
func (rm *RateMeter) RecordBlock(blockNumber, gasUsed, timestamp uint64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.records = append(rm.records, blockRecord{
		blockNumber: blockNumber,
		gasUsed:     gasUsed,
		timestamp:   timestamp,
	})

	// Trim to window size.
	if len(rm.records) > rm.config.WindowSize {
		rm.records = rm.records[len(rm.records)-rm.config.WindowSize:]
	}

	// Update EMA with the instantaneous rate from the newest pair of blocks.
	if len(rm.records) >= 2 {
		prev := rm.records[len(rm.records)-2]
		curr := rm.records[len(rm.records)-1]
		dt := curr.timestamp - prev.timestamp
		if dt > 0 {
			instantRate := float64(curr.gasUsed) / float64(dt)
			if rm.emaRate == 0 {
				rm.emaRate = instantRate
			} else {
				rm.emaRate = rm.config.EMAAlpha*instantRate +
					(1-rm.config.EMAAlpha)*rm.emaRate
			}
		}
	}

	// Adapt worker count based on current vs target rate.
	rm.adaptWorkers()
}

// CurrentRate returns the EMA-smoothed gas rate in gas/sec.
func (rm *RateMeter) CurrentRate() float64 {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.emaRate
}

// RollingAverageRate computes the simple rolling average gas rate over the
// entire sliding window, in gas/sec. Returns 0 if fewer than 2 records.
func (rm *RateMeter) RollingAverageRate() float64 {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if len(rm.records) < 2 {
		return 0
	}

	first := rm.records[0]
	last := rm.records[len(rm.records)-1]
	dt := last.timestamp - first.timestamp
	if dt == 0 {
		return 0
	}

	var totalGas uint64
	for _, r := range rm.records {
		totalGas += r.gasUsed
	}
	return float64(totalGas) / float64(dt)
}

// RecommendedWorkers returns the current adaptive worker count.
func (rm *RateMeter) RecommendedWorkers() int {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.workers
}

// adaptWorkers adjusts the recommended worker count based on the ratio
// of current rate to target rate. If we are below target, increase workers;
// if above, decrease. Clamped to [MinWorkers, MaxWorkers].
func (rm *RateMeter) adaptWorkers() {
	if rm.emaRate <= 0 {
		return
	}

	ratio := rm.emaRate / rm.config.TargetGasPerSec

	switch {
	case ratio < 0.5:
		// Far below target: double workers.
		rm.workers = rm.workers * 2
	case ratio < 0.8:
		// Below target: increase by 25%.
		rm.workers = rm.workers + rm.workers/4
		if rm.workers == 0 {
			rm.workers = rm.config.MinWorkers
		}
	case ratio > 1.5:
		// Far above target: halve workers.
		rm.workers = rm.workers / 2
	case ratio > 1.2:
		// Above target: decrease by 25%.
		rm.workers = rm.workers - rm.workers/4
	}

	// Clamp.
	if rm.workers < rm.config.MinWorkers {
		rm.workers = rm.config.MinWorkers
	}
	if rm.workers > rm.config.MaxWorkers {
		rm.workers = rm.config.MaxWorkers
	}
}

// UtilizationRatio returns the ratio of current EMA rate to the target.
// A value of 1.0 means the target is exactly met.
func (rm *RateMeter) UtilizationRatio() float64 {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.config.TargetGasPerSec == 0 {
		return 0
	}
	return rm.emaRate / rm.config.TargetGasPerSec
}

// IsAtTarget returns true if the current rate is within 20% of the target.
func (rm *RateMeter) IsAtTarget() bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.config.TargetGasPerSec == 0 || rm.emaRate == 0 {
		return false
	}
	ratio := rm.emaRate / rm.config.TargetGasPerSec
	return math.Abs(ratio-1.0) <= 0.2
}

// Reset clears all recorded blocks and resets the rate and workers.
func (rm *RateMeter) Reset() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.records = rm.records[:0]
	rm.emaRate = 0
	rm.workers = rm.config.MinWorkers
}

// WindowSize returns the configured sliding window size.
func (rm *RateMeter) WindowSize() int {
	return rm.config.WindowSize
}

// RecordCount returns how many block records are currently stored.
func (rm *RateMeter) RecordCount() int {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return len(rm.records)
}

// TargetGasPerSec returns the configured target gas rate.
func (rm *RateMeter) TargetGasPerSec() float64 {
	return rm.config.TargetGasPerSec
}
