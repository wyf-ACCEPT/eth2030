// Package das - bandwidth_controller.go implements bandwidth enforcement for
// teragas L2 throughput targeting 1 Gbyte/sec with token-bucket rate limiters,
// per-peer tracking, reservations, throughput monitoring, and adaptive rates.
package das

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Bandwidth controller errors.
var (
	ErrBWInsufficientTokens  = errors.New("das/bw: insufficient tokens for allocation")
	ErrBWReservationExpired  = errors.New("das/bw: reservation deadline has passed")
	ErrBWReservationTooLarge = errors.New("das/bw: reservation exceeds capacity")
	ErrBWPeerNotFound        = errors.New("das/bw: peer not found")
	ErrBWPeerLimitExceeded   = errors.New("das/bw: peer bandwidth limit exceeded")
	ErrBWPolicyViolation     = errors.New("das/bw: bandwidth policy violation")
	ErrBWControllerStopped   = errors.New("das/bw: controller is stopped")
	ErrBWZeroRate            = errors.New("das/bw: rate must be positive")
)

// TeragasBandwidthTarget is the teragas L2 target: 1 GiB/sec.
const TeragasBandwidthTarget float64 = 1 << 30 // 1,073,741,824 bytes/sec

// TokenBucket implements a token-bucket rate limiter.
type TokenBucket struct {
	mu         sync.Mutex
	Rate       float64   // bytes per second refill rate
	Capacity   int64     // maximum burst capacity in bytes
	Tokens     int64     // current available tokens
	LastRefill time.Time // last refill time
}

// NewTokenBucket creates a token bucket with the given rate and burst capacity.
func NewTokenBucket(rate float64, capacity int64) (*TokenBucket, error) {
	if rate <= 0 {
		return nil, fmt.Errorf("%w: got %f", ErrBWZeroRate, rate)
	}
	if capacity <= 0 {
		capacity = int64(rate * 2)
	}
	tokens := capacity
	return &TokenBucket{
		Rate:       rate,
		Capacity:   capacity,
		Tokens:     tokens,
		LastRefill: time.Now(),
	}, nil
}

// refillLocked adds tokens based on elapsed time. Must be called with lock held.
func (tb *TokenBucket) refillLocked(now time.Time) {
	elapsed := now.Sub(tb.LastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	tb.LastRefill = now
	added := int64(elapsed * tb.Rate)
	tb.Tokens += added
	if tb.Tokens > tb.Capacity {
		tb.Tokens = tb.Capacity
	}
}

// Allocate consumes size bytes. Returns (true,0) on success or (false,wait).
func (tb *TokenBucket) Allocate(size int64) (bool, time.Duration) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	tb.refillLocked(now)

	if tb.Tokens >= size {
		tb.Tokens -= size
		return true, 0
	}

	// Compute how long the caller would need to wait.
	deficit := float64(size - tb.Tokens)
	waitSec := deficit / tb.Rate
	return false, time.Duration(waitSec * float64(time.Second))
}

// Available returns the current number of available tokens.
func (tb *TokenBucket) Available() int64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refillLocked(time.Now())
	return tb.Tokens
}

// Utilization returns fraction consumed (0=full, 1=empty).
func (tb *TokenBucket) Utilization() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refillLocked(time.Now())
	if tb.Capacity <= 0 {
		return 1.0
	}
	return 1.0 - float64(tb.Tokens)/float64(tb.Capacity)
}

// Reservation represents a pre-allocated bandwidth reservation.
type Reservation struct {
	Size     int64
	Deadline time.Time
	Granted  time.Time
	PeerID   string
	consumed atomic.Bool
}

// Consume uses the reservation. Fails if already consumed or expired.
func (r *Reservation) Consume() error {
	if time.Now().After(r.Deadline) {
		return ErrBWReservationExpired
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return errors.New("das/bw: reservation already consumed")
	}
	return nil
}

// IsConsumed returns whether this reservation has been consumed.
func (r *Reservation) IsConsumed() bool {
	return r.consumed.Load()
}

// ThroughputReport contains real-time throughput monitoring data.
type ThroughputReport struct {
	CurrentBps, PeakBps, AverageBps, Utilization float64
	DroppedBytes                                 int64
	WindowSize                                   time.Duration
}

type throughputSample struct {
	bytes     int64
	timestamp time.Time
}

// BandwidthPolicy defines configurable bandwidth limits.
type BandwidthPolicy struct {
	MaxGlobalBps      float64       // overall bandwidth cap (bytes/sec)
	MaxPeerBps        float64       // per-peer cap (bytes/sec)
	MaxBlobBps        float64       // per-blob cap (bytes/sec)
	MinAllocationSize int64         // smallest allocation allowed
	MaxAllocationSize int64         // largest single allocation allowed
	ReservationTTL    time.Duration // how long reservations remain valid
}

// DefaultBandwidthPolicy returns a policy targeting teragas L2 throughput.
func DefaultBandwidthPolicy() BandwidthPolicy {
	return BandwidthPolicy{
		MaxGlobalBps:      TeragasBandwidthTarget,      // 1 GiB/sec
		MaxPeerBps:        TeragasBandwidthTarget / 8,  // 128 MiB/sec per peer
		MaxBlobBps:        TeragasBandwidthTarget / 16, // 64 MiB/sec per blob stream
		MinAllocationSize: 512,                         // 512 bytes
		MaxAllocationSize: 16 * 1024 * 1024,            // 16 MiB
		ReservationTTL:    30 * time.Second,
	}
}

// PeerBandwidthTracker tracks per-peer bandwidth usage.
type PeerBandwidthTracker struct {
	mu    sync.RWMutex
	peers map[string]*peerBWState
	limit float64
}

type peerBWState struct {
	bucket     *TokenBucket
	totalBytes atomic.Int64
	allocs     atomic.Int64
	lastActive time.Time
}

// NewPeerBandwidthTracker creates a tracker with the given per-peer limit.
func NewPeerBandwidthTracker(perPeerBps float64) *PeerBandwidthTracker {
	if perPeerBps <= 0 {
		perPeerBps = TeragasBandwidthTarget / 8
	}
	return &PeerBandwidthTracker{peers: make(map[string]*peerBWState), limit: perPeerBps}
}

// TrackAllocation records bandwidth usage for a peer, auto-registering new peers.
func (pt *PeerBandwidthTracker) TrackAllocation(peerID string, size int64) (bool, time.Duration) {
	pt.mu.Lock()
	ps, ok := pt.peers[peerID]
	if !ok {
		bucket, _ := NewTokenBucket(pt.limit, int64(pt.limit*2))
		ps = &peerBWState{bucket: bucket, lastActive: time.Now()}
		pt.peers[peerID] = ps
	}
	pt.mu.Unlock()
	ok, wait := ps.bucket.Allocate(size)
	if ok {
		ps.totalBytes.Add(size)
		ps.allocs.Add(1)
		ps.lastActive = time.Now()
	}
	return ok, wait
}

// PeerStats returns total bytes and allocation count for a peer.
func (pt *PeerBandwidthTracker) PeerStats(peerID string) (totalBytes, allocations int64, err error) {
	pt.mu.RLock()
	ps, ok := pt.peers[peerID]
	pt.mu.RUnlock()
	if !ok {
		return 0, 0, fmt.Errorf("%w: %s", ErrBWPeerNotFound, peerID)
	}
	return ps.totalBytes.Load(), ps.allocs.Load(), nil
}

// PeerCount returns the number of tracked peers.
func (pt *PeerBandwidthTracker) PeerCount() int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return len(pt.peers)
}

// PruneStalePeers removes peers inactive longer than maxIdle.
func (pt *PeerBandwidthTracker) PruneStalePeers(maxIdle time.Duration) int {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	cutoff := time.Now().Add(-maxIdle)
	pruned := 0
	for id, ps := range pt.peers {
		if ps.lastActive.Before(cutoff) {
			delete(pt.peers, id)
			pruned++
		}
	}
	return pruned
}

// AdaptiveRateLimiter uses AIMD (Additive Increase, Multiplicative Decrease)
// to adjust its rate based on observed network conditions.
type AdaptiveRateLimiter struct {
	mu                     sync.Mutex
	bucket                 *TokenBucket
	MinRate, MaxRate       float64
	current                float64
	AdditiveIncrease       float64 // bytes/sec added per successful window
	MultiplicativeDecrease float64 // multiplier on congestion (e.g. 0.5)
	successWindow          int
	successCount           int
}

// NewAdaptiveRateLimiter creates an adaptive rate limiter with AIMD control.
func NewAdaptiveRateLimiter(minRate, maxRate float64) (*AdaptiveRateLimiter, error) {
	if minRate <= 0 || maxRate <= 0 || minRate > maxRate {
		return nil, fmt.Errorf("%w: minRate=%f, maxRate=%f", ErrBWZeroRate, minRate, maxRate)
	}
	bucket, err := NewTokenBucket(minRate, int64(minRate*2))
	if err != nil {
		return nil, err
	}
	return &AdaptiveRateLimiter{
		bucket:                 bucket,
		MinRate:                minRate,
		MaxRate:                maxRate,
		current:                minRate,
		AdditiveIncrease:       maxRate / 100, // 1% of max per successful window
		MultiplicativeDecrease: 0.5,           // halve on congestion
		successWindow:          10,
	}, nil
}

// TryAllocate attempts to allocate size bytes with AIMD feedback.
func (ar *AdaptiveRateLimiter) TryAllocate(size int64) (bool, time.Duration) {
	ar.mu.Lock()
	defer ar.mu.Unlock()

	ok, wait := ar.bucket.Allocate(size)
	if ok {
		ar.successCount++
		if ar.successCount >= ar.successWindow {
			ar.successCount = 0
			ar.increaseRate()
		}
	} else {
		ar.decreaseRate()
		ar.successCount = 0
	}
	return ok, wait
}

// increaseRate applies additive increase (caller holds lock).
func (ar *AdaptiveRateLimiter) increaseRate() {
	ar.current += ar.AdditiveIncrease
	if ar.current > ar.MaxRate {
		ar.current = ar.MaxRate
	}
	ar.bucket.mu.Lock()
	ar.bucket.Rate = ar.current
	ar.bucket.mu.Unlock()
}

// decreaseRate applies multiplicative decrease (caller holds lock).
func (ar *AdaptiveRateLimiter) decreaseRate() {
	ar.current *= ar.MultiplicativeDecrease
	if ar.current < ar.MinRate {
		ar.current = ar.MinRate
	}
	ar.bucket.mu.Lock()
	ar.bucket.Rate = ar.current
	ar.bucket.mu.Unlock()
}

// CurrentRate returns the current adaptive rate.
func (ar *AdaptiveRateLimiter) CurrentRate() float64 {
	ar.mu.Lock()
	defer ar.mu.Unlock()
	return ar.current
}

// BandwidthController orchestrates bandwidth enforcement for teragas L2
// with token bucket limiting, per-peer tracking, adaptive rates, and
// real-time throughput monitoring.
type BandwidthController struct {
	mu           sync.RWMutex
	policy       BandwidthPolicy
	global       *TokenBucket
	peers        *PeerBandwidthTracker
	limiter      *AdaptiveRateLimiter
	stopped      atomic.Bool
	samples      []throughputSample
	sampleWindow time.Duration
	peakBps      float64
	droppedBytes atomic.Int64
}

// NewBandwidthController creates a new bandwidth controller with the given policy.
func NewBandwidthController(policy BandwidthPolicy) (*BandwidthController, error) {
	if policy.MaxGlobalBps <= 0 {
		policy.MaxGlobalBps = TeragasBandwidthTarget
	}

	global, err := NewTokenBucket(policy.MaxGlobalBps, int64(policy.MaxGlobalBps*2))
	if err != nil {
		return nil, fmt.Errorf("creating global bucket: %w", err)
	}

	peerTracker := NewPeerBandwidthTracker(policy.MaxPeerBps)

	limiter, err := NewAdaptiveRateLimiter(
		policy.MaxGlobalBps/10, // min = 10% of target
		policy.MaxGlobalBps,
	)
	if err != nil {
		return nil, fmt.Errorf("creating adaptive limiter: %w", err)
	}

	return &BandwidthController{
		policy:       policy,
		global:       global,
		peers:        peerTracker,
		limiter:      limiter,
		samples:      make([]throughputSample, 0, 1024),
		sampleWindow: 10 * time.Second,
	}, nil
}

// Reserve creates a bandwidth reservation that must be consumed before deadline.
func (bc *BandwidthController) Reserve(size int64, deadline time.Time, peerID string) (*Reservation, error) {
	if bc.stopped.Load() {
		return nil, ErrBWControllerStopped
	}
	if size > bc.policy.MaxAllocationSize && bc.policy.MaxAllocationSize > 0 {
		return nil, fmt.Errorf("%w: %d > max %d", ErrBWReservationTooLarge, size, bc.policy.MaxAllocationSize)
	}
	if time.Now().After(deadline) {
		return nil, ErrBWReservationExpired
	}

	// Pre-allocate from the global bucket.
	ok, _ := bc.global.Allocate(size)
	if !ok {
		return nil, fmt.Errorf("%w: global bucket exhausted", ErrBWInsufficientTokens)
	}

	return &Reservation{
		Size:     size,
		Deadline: deadline,
		Granted:  time.Now(),
		PeerID:   peerID,
	}, nil
}

// AllocatePeer allocates bandwidth for a peer, checking global and per-peer limits.
func (bc *BandwidthController) AllocatePeer(peerID string, size int64) error {
	if bc.stopped.Load() {
		return ErrBWControllerStopped
	}

	// Check policy constraints.
	if bc.policy.MinAllocationSize > 0 && size < bc.policy.MinAllocationSize {
		return fmt.Errorf("%w: size %d < min %d", ErrBWPolicyViolation, size, bc.policy.MinAllocationSize)
	}
	if bc.policy.MaxAllocationSize > 0 && size > bc.policy.MaxAllocationSize {
		return fmt.Errorf("%w: size %d > max %d", ErrBWPolicyViolation, size, bc.policy.MaxAllocationSize)
	}

	// Check global bucket via adaptive limiter.
	ok, _ := bc.limiter.TryAllocate(size)
	if !ok {
		bc.droppedBytes.Add(size)
		return fmt.Errorf("%w: adaptive limiter denied", ErrBWInsufficientTokens)
	}

	// Check per-peer limit.
	ok, _ = bc.peers.TrackAllocation(peerID, size)
	if !ok {
		bc.droppedBytes.Add(size)
		return fmt.Errorf("%w: peer %s", ErrBWPeerLimitExceeded, peerID)
	}

	// Record sample for throughput monitoring.
	bc.recordSample(size)
	return nil
}

func (bc *BandwidthController) recordSample(bytes int64) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	now := time.Now()
	bc.samples = append(bc.samples, throughputSample{bytes: bytes, timestamp: now})
	cutoff := now.Add(-bc.sampleWindow)
	firstValid := 0
	for i, s := range bc.samples {
		if !s.timestamp.Before(cutoff) {
			firstValid = i
			break
		}
		if i == len(bc.samples)-1 {
			firstValid = len(bc.samples)
		}
	}
	if firstValid > 0 {
		bc.samples = bc.samples[firstValid:]
	}
}

// MonitorThroughput returns a real-time throughput report based on
// samples within the monitoring window.
func (bc *BandwidthController) MonitorThroughput() ThroughputReport {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-bc.sampleWindow)
	recentCutoff := now.Add(-time.Second)
	var totalBytes, currentBytes int64

	for _, s := range bc.samples {
		if !s.timestamp.Before(cutoff) {
			totalBytes += s.bytes
		}
		if !s.timestamp.Before(recentCutoff) {
			currentBytes += s.bytes
		}
	}

	windowSec := bc.sampleWindow.Seconds()
	if windowSec <= 0 {
		windowSec = 1
	}
	avgBps := float64(totalBytes) / windowSec
	currentBps := float64(currentBytes)

	if currentBps > bc.peakBps {
		bc.peakBps = currentBps
	}

	return ThroughputReport{
		CurrentBps:   currentBps,
		PeakBps:      bc.peakBps,
		AverageBps:   avgBps,
		Utilization:  avgBps / bc.policy.MaxGlobalBps,
		DroppedBytes: bc.droppedBytes.Load(),
		WindowSize:   bc.sampleWindow,
	}
}

// GlobalUtilization returns the current global bucket utilization.
func (bc *BandwidthController) GlobalUtilization() float64 {
	return bc.global.Utilization()
}

// AdaptiveRate returns the current adaptive rate in bytes/sec.
func (bc *BandwidthController) AdaptiveRate() float64 {
	return bc.limiter.CurrentRate()
}

// PeerCount returns the number of currently tracked peers.
func (bc *BandwidthController) PeerCount() int {
	return bc.peers.PeerCount()
}

// Policy returns the current bandwidth policy.
func (bc *BandwidthController) Policy() BandwidthPolicy {
	return bc.policy
}

// Stop marks the controller as stopped, rejecting future allocations.
func (bc *BandwidthController) Stop() {
	bc.stopped.Store(true)
}

// IsStopped returns whether the controller is stopped.
func (bc *BandwidthController) IsStopped() bool {
	return bc.stopped.Load()
}

// ComputeOptimalChunkSize computes the optimal chunk size for blob streaming
// given a target throughput and latency budget, balancing throughput vs latency.
func ComputeOptimalChunkSize(targetBps float64, latencyBudgetMs int, minChunkSize, maxChunkSize int64) int64 {
	if targetBps <= 0 || latencyBudgetMs <= 0 {
		return minChunkSize
	}
	// chunk_size = targetBps * (latencyBudgetMs / 1000) / 2
	// The /2 provides margin for protocol overhead and retransmissions.
	optimalF := targetBps * (float64(latencyBudgetMs) / 1000.0) / 2.0
	optimal := int64(math.Min(math.Max(optimalF, float64(minChunkSize)), float64(maxChunkSize)))
	if optimal < minChunkSize {
		optimal = minChunkSize
	}
	if optimal > maxChunkSize {
		optimal = maxChunkSize
	}
	return optimal
}
