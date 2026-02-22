// bandwidth_enforcer.go implements bandwidth enforcement for teragas L2
// throughput targeting 1 Gbyte/sec. It uses token-bucket rate limiters per L2
// chain with a global bandwidth cap, congestion pricing when utilization
// exceeds 80%, and backpressure signaling when consumers lag.
package das

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Bandwidth enforcer errors.
var (
	ErrBandwidthExceeded  = errors.New("das: bandwidth limit exceeded")
	ErrChainNotRegistered = errors.New("das: L2 chain not registered for bandwidth")
	ErrGlobalCapExceeded  = errors.New("das: global bandwidth cap exceeded")
	ErrBackpressureActive = errors.New("das: backpressure active, slow down")
	ErrZeroBandwidthCap   = errors.New("das: bandwidth cap must be > 0")
)

// BandwidthConfig configures the bandwidth enforcer.
type BandwidthConfig struct {
	// GlobalCapBytesPerSec is the total bandwidth cap across all L2 chains.
	// Default: 1 Gbyte/sec = 1_073_741_824 bytes/sec.
	GlobalCapBytesPerSec uint64

	// DefaultChainQuota is the default per-chain bytes/sec quota.
	DefaultChainQuota uint64

	// CongestionThreshold is the utilization ratio (0.0-1.0) above which
	// congestion pricing kicks in. Default: 0.80.
	CongestionThreshold float64

	// CongestionMultiplier is applied to the cost when congestion is active.
	// Default: 2.0 (double the cost).
	CongestionMultiplier float64

	// BackpressureThreshold is the utilization ratio above which backpressure
	// signals are sent to producers. Default: 0.95.
	BackpressureThreshold float64

	// RefillIntervalMs is how often tokens are refilled (in milliseconds).
	RefillIntervalMs int
}

// DefaultBandwidthConfig returns a default configuration targeting 1 Gbyte/sec.
func DefaultBandwidthConfig() BandwidthConfig {
	return BandwidthConfig{
		GlobalCapBytesPerSec:  1 << 30,   // 1 GiB/sec
		DefaultChainQuota:     128 << 20, // 128 MiB/sec per chain
		CongestionThreshold:   0.80,
		CongestionMultiplier:  2.0,
		BackpressureThreshold: 0.95,
		RefillIntervalMs:      100,
	}
}

// tokenBucket implements a simple token bucket rate limiter.
type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64 // current tokens (bytes)
	capacity float64 // max tokens
	rate     float64 // tokens per second (refill rate)
	lastTime time.Time
}

// newTokenBucket creates a token bucket with the given rate (bytes/sec).
func newTokenBucket(rate uint64) *tokenBucket {
	return &tokenBucket{
		tokens:   float64(rate),     // start full for one second
		capacity: float64(rate) * 2, // allow a burst of 2 seconds
		rate:     float64(rate),
		lastTime: time.Now(),
	}
}

// tryConsume attempts to consume n tokens. Returns true if successful.
func (tb *tokenBucket) tryConsume(n uint64) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	if tb.tokens >= float64(n) {
		tb.tokens -= float64(n)
		return true
	}
	return false
}

// available returns the current number of available tokens.
func (tb *tokenBucket) available() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	return tb.tokens
}

// refill adds tokens based on elapsed time. Must be called with lock held.
func (tb *tokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	if elapsed <= 0 {
		return
	}
	tb.lastTime = now
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
}

// utilization returns the fraction of capacity consumed (0.0 = full bucket,
// 1.0 = empty bucket).
func (tb *tokenBucket) utilization() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	if tb.capacity == 0 {
		return 1.0
	}
	return 1.0 - tb.tokens/tb.capacity
}

// BandwidthMetrics tracks bandwidth enforcement statistics.
type BandwidthMetrics struct {
	TotalBytesAllowed atomic.Uint64
	TotalBytesDropped atomic.Uint64
	CongestionEvents  atomic.Uint64
	BackpressureCount atomic.Uint64
}

// BandwidthEnforcer manages bandwidth quotas for L2 chains.
type BandwidthEnforcer struct {
	mu      sync.RWMutex
	config  BandwidthConfig
	global  *tokenBucket
	chains  map[uint64]*tokenBucket
	metrics BandwidthMetrics
}

// NewBandwidthEnforcer creates a new bandwidth enforcer.
func NewBandwidthEnforcer(config BandwidthConfig) (*BandwidthEnforcer, error) {
	if config.GlobalCapBytesPerSec == 0 {
		return nil, ErrZeroBandwidthCap
	}
	if config.CongestionThreshold <= 0 || config.CongestionThreshold > 1.0 {
		config.CongestionThreshold = 0.80
	}
	if config.CongestionMultiplier <= 0 {
		config.CongestionMultiplier = 2.0
	}
	if config.BackpressureThreshold <= 0 || config.BackpressureThreshold > 1.0 {
		config.BackpressureThreshold = 0.95
	}
	if config.DefaultChainQuota == 0 {
		config.DefaultChainQuota = config.GlobalCapBytesPerSec / 8
	}

	return &BandwidthEnforcer{
		config: config,
		global: newTokenBucket(config.GlobalCapBytesPerSec),
		chains: make(map[uint64]*tokenBucket),
	}, nil
}

// RegisterChain registers an L2 chain with the given quota (bytes/sec).
// If quota is 0, the default chain quota is used.
func (be *BandwidthEnforcer) RegisterChain(chainID uint64, quotaBytesPerSec uint64) {
	be.mu.Lock()
	defer be.mu.Unlock()
	if quotaBytesPerSec == 0 {
		quotaBytesPerSec = be.config.DefaultChainQuota
	}
	be.chains[chainID] = newTokenBucket(quotaBytesPerSec)
}

// RequestBandwidth attempts to consume nBytes of bandwidth for the given chain.
// Returns nil if allowed, or an error indicating why the request was denied.
func (be *BandwidthEnforcer) RequestBandwidth(chainID uint64, nBytes uint64) error {
	be.mu.RLock()
	chainBucket, ok := be.chains[chainID]
	be.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: chain %d", ErrChainNotRegistered, chainID)
	}

	// Check backpressure first.
	globalUtil := be.global.utilization()
	if globalUtil >= be.config.BackpressureThreshold {
		be.metrics.BackpressureCount.Add(1)
		return ErrBackpressureActive
	}

	// Apply congestion pricing: if above threshold, the effective cost
	// is multiplied (the caller must request more tokens).
	effectiveBytes := nBytes
	if globalUtil >= be.config.CongestionThreshold {
		be.metrics.CongestionEvents.Add(1)
		effectiveBytes = uint64(float64(nBytes) * be.config.CongestionMultiplier)
	}

	// Check per-chain bucket.
	if !chainBucket.tryConsume(effectiveBytes) {
		be.metrics.TotalBytesDropped.Add(nBytes)
		return fmt.Errorf("%w: chain %d", ErrBandwidthExceeded, chainID)
	}

	// Check global bucket.
	if !be.global.tryConsume(effectiveBytes) {
		// Refund the chain bucket (approximate -- we re-add the consumed amount).
		chainBucket.mu.Lock()
		chainBucket.tokens += float64(effectiveBytes)
		if chainBucket.tokens > chainBucket.capacity {
			chainBucket.tokens = chainBucket.capacity
		}
		chainBucket.mu.Unlock()

		be.metrics.TotalBytesDropped.Add(nBytes)
		return ErrGlobalCapExceeded
	}

	be.metrics.TotalBytesAllowed.Add(nBytes)
	return nil
}

// GlobalUtilization returns the current global bandwidth utilization (0.0-1.0).
func (be *BandwidthEnforcer) GlobalUtilization() float64 {
	return be.global.utilization()
}

// ChainUtilization returns the bandwidth utilization for a specific chain.
func (be *BandwidthEnforcer) ChainUtilization(chainID uint64) float64 {
	be.mu.RLock()
	bucket, ok := be.chains[chainID]
	be.mu.RUnlock()
	if !ok {
		return 0
	}
	return bucket.utilization()
}

// IsCongested returns true if global utilization exceeds the congestion threshold.
func (be *BandwidthEnforcer) IsCongested() bool {
	return be.global.utilization() >= be.config.CongestionThreshold
}

// IsBackpressureActive returns true if backpressure should be applied.
func (be *BandwidthEnforcer) IsBackpressureActive() bool {
	return be.global.utilization() >= be.config.BackpressureThreshold
}

// Metrics returns a snapshot of bandwidth enforcement metrics.
func (be *BandwidthEnforcer) Metrics() (allowed, dropped, congestion, backpressure uint64) {
	return be.metrics.TotalBytesAllowed.Load(),
		be.metrics.TotalBytesDropped.Load(),
		be.metrics.CongestionEvents.Load(),
		be.metrics.BackpressureCount.Load()
}

// RegisteredChains returns the count of registered L2 chains.
func (be *BandwidthEnforcer) RegisteredChains() int {
	be.mu.RLock()
	defer be.mu.RUnlock()
	return len(be.chains)
}

// Config returns the enforcer configuration.
func (be *BandwidthEnforcer) Config() BandwidthConfig {
	return be.config
}
