// stream_enforcer.go implements a streaming pipeline that integrates
// bandwidth enforcement with blob processing for teragas L2 throughput.
package das

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// StreamingPipeline errors.
var (
	ErrStreamBandwidthDenied = errors.New("stream: bandwidth denied")
	ErrStreamNilEnforcer     = errors.New("stream: nil bandwidth enforcer")
)

// ThroughputStats holds current throughput statistics for the streaming
// pipeline.
type ThroughputStats struct {
	// CurrentBytesPerSec is the throughput averaged over the last measurement
	// window.
	CurrentBytesPerSec float64

	// PeakBytesPerSec is the highest throughput observed since the pipeline
	// was created.
	PeakBytesPerSec float64

	// Utilization is the ratio of current throughput to the global bandwidth
	// cap (0.0-1.0).
	Utilization float64
}

// StreamingPipeline enforces bandwidth limits on blob data flowing through
// the gossip/streaming layer. It tracks throughput in real-time and provides
// backpressure signaling when utilization is high.
type StreamingPipeline struct {
	mu       sync.RWMutex
	enforcer *BandwidthEnforcer

	// Throughput tracking (atomic for lock-free reads).
	totalProcessed atomic.Uint64

	// Window-based throughput measurement.
	windowStart time.Time
	windowBytes uint64
	currentRate float64
	peakRate    float64
}

// NewStreamingPipeline creates a new streaming pipeline backed by the given
// bandwidth enforcer.
func NewStreamingPipeline(enforcer *BandwidthEnforcer) (*StreamingPipeline, error) {
	if enforcer == nil {
		return nil, ErrStreamNilEnforcer
	}
	return &StreamingPipeline{
		enforcer:    enforcer,
		windowStart: time.Now(),
	}, nil
}

// ProcessBlob enforces bandwidth before forwarding a blob for a given L2
// chain. Returns nil if the blob was accepted, or an error if bandwidth
// limits prevent processing.
func (sp *StreamingPipeline) ProcessBlob(chainID uint64, blob []byte) error {
	if len(blob) == 0 {
		return nil
	}

	blobSize := uint64(len(blob))

	// Use per-chain enforcement if the chain is registered, otherwise
	// fall back to global gossip enforcement.
	err := sp.enforcer.RequestBandwidth(chainID, blobSize)
	if err != nil {
		// If the chain is not registered, try global-only enforcement.
		if errors.Is(err, ErrChainNotRegistered) {
			err = sp.enforcer.EnforceOnGossip(blobSize)
		}
		if err != nil {
			return err
		}
	}

	// Update throughput tracking.
	sp.totalProcessed.Add(blobSize)
	sp.updateThroughput(blobSize)

	return nil
}

// BackpressureActive returns true when global utilization exceeds 95%,
// signaling that producers should slow down.
func (sp *StreamingPipeline) BackpressureActive() bool {
	return sp.enforcer.IsBackpressureActive()
}

// GetThroughputStats returns current throughput statistics.
func (sp *StreamingPipeline) GetThroughputStats() ThroughputStats {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	globalCap := float64(sp.enforcer.config.GlobalCapBytesPerSec)
	utilization := 0.0
	if globalCap > 0 {
		utilization = sp.currentRate / globalCap
	}
	if utilization > 1.0 {
		utilization = 1.0
	}

	return ThroughputStats{
		CurrentBytesPerSec: sp.currentRate,
		PeakBytesPerSec:    sp.peakRate,
		Utilization:        utilization,
	}
}

// updateThroughput recalculates the rolling throughput rate. Caller should
// invoke this after each successful blob processing.
func (sp *StreamingPipeline) updateThroughput(blobSize uint64) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	sp.windowBytes += blobSize
	elapsed := time.Since(sp.windowStart).Seconds()

	// Recalculate rate every 100ms or more.
	if elapsed >= 0.1 {
		sp.currentRate = float64(sp.windowBytes) / elapsed
		if sp.currentRate > sp.peakRate {
			sp.peakRate = sp.currentRate
		}
		// Reset window.
		sp.windowStart = time.Now()
		sp.windowBytes = 0
	}
}
