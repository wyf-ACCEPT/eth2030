// Dynamic scaling for attestation processing at 1M attestations per slot.
//
// Monitors attestation arrival rate and dynamically adjusts the worker
// pool size. Provides a memory-bounded priority queue (current slot first),
// garbage collection for stale attestations, and real-time statistics.
package consensus

import (
	"errors"
	"sync"
	"sync/atomic"
)

// Attestation scaler constants.
const (
	// DefaultMaxBufferSize is the maximum attestation buffer entries.
	DefaultMaxBufferSize = 2_000_000

	// DefaultMinWorkers is the minimum worker pool size.
	DefaultMinWorkers = 4

	// DefaultMaxWorkers is the maximum worker pool size.
	DefaultMaxWorkers = 64

	// DefaultPruneEpochs is the number of epochs before pruning.
	DefaultPruneEpochs = 2

	// DefaultSlotsPerEpochScaler is the slots per epoch for scaler calculations.
	DefaultSlotsPerEpochScaler uint64 = 32

	// ScaleUpThreshold triggers upscaling when queue exceeds this fraction.
	ScaleUpThreshold = 0.75

	// ScaleDownThreshold triggers downscaling when queue is below this fraction.
	ScaleDownThreshold = 0.25
)

// Attestation scaler errors.
var (
	ErrScalerBufferFull = errors.New("attestation_scaler: buffer full, attestation dropped")
	ErrScalerNilAtt     = errors.New("attestation_scaler: nil attestation")
	ErrScalerStopped    = errors.New("attestation_scaler: scaler is stopped")
)

// ScalerStats holds real-time statistics for the attestation scaler.
type ScalerStats struct {
	AttestationsPerSlot float64 // moving average
	QueueDepth          int
	ActiveWorkers       int
	DroppedCount        int64
	ProcessedCount      int64
	CurrentSlot         uint64
	BufferUtilization   float64 // fraction of buffer used
}

// ScalerConfig configures the attestation scaler.
type ScalerConfig struct {
	MaxBufferSize int    // maximum attestations in the buffer
	MinWorkers    int    // minimum worker count
	MaxWorkers    int    // maximum worker count
	PruneEpochs   int    // epochs before attestation expiry
	SlotsPerEpoch uint64 // slots per epoch
}

// DefaultScalerConfig returns the default scaler configuration.
func DefaultScalerConfig() *ScalerConfig {
	return &ScalerConfig{
		MaxBufferSize: DefaultMaxBufferSize,
		MinWorkers:    DefaultMinWorkers,
		MaxWorkers:    DefaultMaxWorkers,
		PruneEpochs:   DefaultPruneEpochs,
		SlotsPerEpoch: DefaultSlotsPerEpochScaler,
	}
}

// priorityEntry wraps an attestation with priority metadata.
type priorityEntry struct {
	att      *AggregateAttestation
	priority int // lower number = higher priority
}

// AttestationScaler dynamically scales attestation processing.
// Maintains a priority queue where current-slot attestations have
// highest priority, and manages a pool of workers. Thread-safe.
type AttestationScaler struct {
	config *ScalerConfig
	mu     sync.Mutex

	// buffer is the priority queue, organized by slot.
	bufferBySlot map[Slot][]*AggregateAttestation

	// bufferSize is the total number of entries across all slots.
	bufferSize int

	// currentSlot is the current slot for priority determination.
	currentSlot Slot

	// activeWorkers is the current worker pool size.
	activeWorkers int

	// Atomic metrics.
	dropped   atomic.Int64
	processed atomic.Int64

	// slotRates tracks attestations received per slot for rate calculation.
	slotRates map[Slot]int

	// moving average of attestations per slot.
	rateAvg float64

	// stopped indicates the scaler is shut down.
	stopped bool
}

// NewAttestationScaler creates a new attestation scaler.
func NewAttestationScaler(cfg *ScalerConfig) *AttestationScaler {
	if cfg == nil {
		cfg = DefaultScalerConfig()
	}
	if cfg.MinWorkers < 1 {
		cfg.MinWorkers = 1
	}
	if cfg.MaxWorkers < cfg.MinWorkers {
		cfg.MaxWorkers = cfg.MinWorkers
	}
	if cfg.MaxBufferSize < 1 {
		cfg.MaxBufferSize = DefaultMaxBufferSize
	}
	if cfg.SlotsPerEpoch == 0 {
		cfg.SlotsPerEpoch = DefaultSlotsPerEpochScaler
	}
	if cfg.PruneEpochs < 1 {
		cfg.PruneEpochs = DefaultPruneEpochs
	}

	return &AttestationScaler{
		config:        cfg,
		bufferBySlot:  make(map[Slot][]*AggregateAttestation),
		slotRates:     make(map[Slot]int),
		activeWorkers: cfg.MinWorkers,
	}
}

// Submit adds an attestation to the scaler's priority queue.
// Attestations for the current slot get highest priority.
func (as *AttestationScaler) Submit(att *AggregateAttestation) error {
	if att == nil {
		return ErrScalerNilAtt
	}

	as.mu.Lock()
	defer as.mu.Unlock()

	if as.stopped {
		return ErrScalerStopped
	}

	// Check buffer capacity.
	if as.bufferSize >= as.config.MaxBufferSize {
		as.dropped.Add(1)
		return ErrScalerBufferFull
	}

	slot := att.Data.Slot
	as.bufferBySlot[slot] = append(as.bufferBySlot[slot], copyAggregateAttestation(att))
	as.bufferSize++

	// Track slot rate.
	as.slotRates[slot]++

	return nil
}

// Dequeue retrieves the highest priority attestations (current slot first),
// up to the given count. Returns the dequeued attestations.
func (as *AttestationScaler) Dequeue(count int) []*AggregateAttestation {
	as.mu.Lock()
	defer as.mu.Unlock()

	if count <= 0 || as.bufferSize == 0 {
		return nil
	}

	var result []*AggregateAttestation

	// Priority 1: current slot attestations.
	if atts, ok := as.bufferBySlot[as.currentSlot]; ok && len(atts) > 0 {
		take := count
		if take > len(atts) {
			take = len(atts)
		}
		result = append(result, atts[:take]...)
		as.bufferBySlot[as.currentSlot] = atts[take:]
		if len(as.bufferBySlot[as.currentSlot]) == 0 {
			delete(as.bufferBySlot, as.currentSlot)
		}
		as.bufferSize -= take
		count -= take
	}

	// Priority 2: recent slots (current-1, current-2, ...).
	if count > 0 {
		for offset := uint64(1); offset <= 32 && count > 0; offset++ {
			slot := Slot(0)
			if uint64(as.currentSlot) >= offset {
				slot = Slot(uint64(as.currentSlot) - offset)
			} else {
				continue
			}

			atts, ok := as.bufferBySlot[slot]
			if !ok || len(atts) == 0 {
				continue
			}
			take := count
			if take > len(atts) {
				take = len(atts)
			}
			result = append(result, atts[:take]...)
			as.bufferBySlot[slot] = atts[take:]
			if len(as.bufferBySlot[slot]) == 0 {
				delete(as.bufferBySlot, slot)
			}
			as.bufferSize -= take
			count -= take
		}
	}

	as.processed.Add(int64(len(result)))
	return result
}

// UpdateSlot updates the current slot and triggers scaling and pruning.
func (as *AttestationScaler) UpdateSlot(slot Slot) {
	as.mu.Lock()
	defer as.mu.Unlock()

	as.currentSlot = slot
	as.adjustWorkersLocked()
	as.pruneLocked()
	as.updateRateLocked(slot)
}

// adjustWorkersLocked scales the worker pool based on buffer utilization.
func (as *AttestationScaler) adjustWorkersLocked() {
	utilization := float64(as.bufferSize) / float64(as.config.MaxBufferSize)

	if utilization > ScaleUpThreshold && as.activeWorkers < as.config.MaxWorkers {
		// Scale up: double workers, capped at max.
		newWorkers := as.activeWorkers * 2
		if newWorkers > as.config.MaxWorkers {
			newWorkers = as.config.MaxWorkers
		}
		as.activeWorkers = newWorkers
	} else if utilization < ScaleDownThreshold && as.activeWorkers > as.config.MinWorkers {
		// Scale down: halve workers, floored at min.
		newWorkers := as.activeWorkers / 2
		if newWorkers < as.config.MinWorkers {
			newWorkers = as.config.MinWorkers
		}
		as.activeWorkers = newWorkers
	}
}

// pruneLocked removes attestations older than PruneEpochs.
func (as *AttestationScaler) pruneLocked() {
	if as.currentSlot == 0 {
		return
	}

	maxAge := uint64(as.config.PruneEpochs) * as.config.SlotsPerEpoch
	var cutoff uint64
	if uint64(as.currentSlot) > maxAge {
		cutoff = uint64(as.currentSlot) - maxAge
	}

	for slot, atts := range as.bufferBySlot {
		if uint64(slot) < cutoff {
			as.bufferSize -= len(atts)
			delete(as.bufferBySlot, slot)
			delete(as.slotRates, slot)
		}
	}
}

// updateRateLocked updates the attestation rate moving average.
func (as *AttestationScaler) updateRateLocked(slot Slot) {
	rate := float64(as.slotRates[slot])
	// Exponential moving average with alpha = 0.2.
	as.rateAvg = as.rateAvg*0.8 + rate*0.2
}

// Stats returns current scaler statistics.
func (as *AttestationScaler) Stats() ScalerStats {
	as.mu.Lock()
	defer as.mu.Unlock()

	return ScalerStats{
		AttestationsPerSlot: as.rateAvg,
		QueueDepth:          as.bufferSize,
		ActiveWorkers:       as.activeWorkers,
		DroppedCount:        as.dropped.Load(),
		ProcessedCount:      as.processed.Load(),
		CurrentSlot:         uint64(as.currentSlot),
		BufferUtilization:   float64(as.bufferSize) / float64(as.config.MaxBufferSize),
	}
}

// ActiveWorkers returns the current worker pool size.
func (as *AttestationScaler) ActiveWorkers() int {
	as.mu.Lock()
	defer as.mu.Unlock()
	return as.activeWorkers
}

// BufferSize returns the total number of buffered attestations.
func (as *AttestationScaler) BufferSize() int {
	as.mu.Lock()
	defer as.mu.Unlock()
	return as.bufferSize
}

// Stop stops the scaler, rejecting new submissions.
func (as *AttestationScaler) Stop() {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.stopped = true
}
