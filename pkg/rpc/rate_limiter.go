// rate_limiter.go implements per-client, per-method rate limiting for JSON-RPC
// endpoints using a token bucket algorithm. It tracks request rates, supports
// manual and automatic banning, and exposes per-client and global statistics.
package rpc

import (
	"sync"
	"sync/atomic"
	"time"
)

// RPCRateLimitConfig holds the configuration for the RPCRateLimiter.
type RPCRateLimitConfig struct {
	// GlobalRPS is the maximum total requests per second across all clients.
	GlobalRPS int

	// PerClientRPS is the maximum requests per second for a single client IP.
	PerClientRPS int

	// PerMethodRPS is the maximum requests per second for a single method
	// from a single client.
	PerMethodRPS int

	// BurstMultiplier scales the token bucket capacity to allow short bursts.
	BurstMultiplier int

	// BanDurationSecs is the default ban duration in seconds when a client
	// is automatically banned for exceeding its rate limit repeatedly.
	BanDurationSecs int64
}

// DefaultRPCRateLimitConfig returns sensible defaults for RPC rate limiting.
func DefaultRPCRateLimitConfig() *RPCRateLimitConfig {
	return &RPCRateLimitConfig{
		GlobalRPS:       1000,
		PerClientRPS:    100,
		PerMethodRPS:    50,
		BurstMultiplier: 3,
		BanDurationSecs: 60,
	}
}

// tokenBucket implements a simple token bucket for rate limiting.
type tokenBucket struct {
	tokens     float64
	capacity   float64
	refillRate float64 // tokens per second
	lastRefill int64   // unix nanoseconds
}

// newTokenBucket creates a bucket with the given rate (tokens/sec) and burst multiplier.
func newTokenBucket(rate int, burstMult int) *tokenBucket {
	cap := float64(rate * burstMult)
	return &tokenBucket{
		tokens:     cap,
		capacity:   cap,
		refillRate: float64(rate),
		lastRefill: time.Now().UnixNano(),
	}
}

// allow tries to consume one token. Returns true if allowed.
func (tb *tokenBucket) allow(now int64) bool {
	elapsed := float64(now-tb.lastRefill) / float64(time.Second)
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
	tb.lastRefill = now
	if tb.tokens >= 1.0 {
		tb.tokens--
		return true
	}
	return false
}

// clientEntry tracks per-client rate limiting state.
type clientEntry struct {
	bucket     *tokenBucket
	methods    map[string]*tokenBucket
	bannedUtil int64 // unix seconds; 0 = not banned
	lastActive int64 // unix seconds

	totalRequests   uint64
	allowedRequests uint64
	deniedRequests  uint64
}

// methodEntry tracks per-method aggregate statistics.
type methodEntry struct {
	totalRequests  uint64
	deniedRequests uint64
	totalLatencyNs int64 // cumulative latency in nanoseconds
}

// ClientRateStats provides per-client rate limiting statistics.
type ClientRateStats struct {
	TotalRequests   uint64
	AllowedRequests uint64
	DeniedRequests  uint64
	BannedUntil     int64 // unix seconds; 0 = not banned
}

// MethodRateStats provides per-method rate limiting statistics.
type MethodRateStats struct {
	TotalRequests  uint64
	DeniedRequests uint64
	AvgLatencyMs   float64
}

// GlobalRateStats provides aggregate rate limiting statistics.
type GlobalRateStats struct {
	TotalRequests uint64
	TotalDenied   uint64
	ActiveClients uint64
	BannedClients uint64
}

// RPCRateLimiter enforces per-client and per-method rate limits for
// JSON-RPC endpoints. All methods are safe for concurrent use.
type RPCRateLimiter struct {
	config *RPCRateLimitConfig

	mu      sync.Mutex
	clients map[string]*clientEntry
	methods map[string]*methodEntry

	globalBucket *tokenBucket

	totalRequests atomic.Uint64
	totalDenied   atomic.Uint64
}

// NewRPCRateLimiter creates a new rate limiter with the given configuration.
// If config is nil, DefaultRPCRateLimitConfig is used.
func NewRPCRateLimiter(config *RPCRateLimitConfig) *RPCRateLimiter {
	if config == nil {
		config = DefaultRPCRateLimitConfig()
	}
	if config.BurstMultiplier <= 0 {
		config.BurstMultiplier = 1
	}
	return &RPCRateLimiter{
		config:       config,
		clients:      make(map[string]*clientEntry),
		methods:      make(map[string]*methodEntry),
		globalBucket: newTokenBucket(config.GlobalRPS, config.BurstMultiplier),
	}
}

// Allow checks whether a request from clientIP calling method should be
// allowed. It updates internal counters and returns false if any rate
// limit is exceeded or the client is banned.
func (rl *RPCRateLimiter) Allow(clientIP string, method string) bool {
	now := time.Now()
	nowNano := now.UnixNano()
	nowSec := now.Unix()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.totalRequests.Add(1)

	// Get or create client entry.
	ce := rl.clients[clientIP]
	if ce == nil {
		ce = &clientEntry{
			bucket:  newTokenBucket(rl.config.PerClientRPS, rl.config.BurstMultiplier),
			methods: make(map[string]*tokenBucket),
		}
		rl.clients[clientIP] = ce
	}
	ce.lastActive = nowSec
	ce.totalRequests++

	// Get or create method entry.
	me := rl.methods[method]
	if me == nil {
		me = &methodEntry{}
		rl.methods[method] = me
	}
	me.totalRequests++

	// Check ban status.
	if ce.bannedUtil > 0 && nowSec < ce.bannedUtil {
		ce.deniedRequests++
		me.deniedRequests++
		rl.totalDenied.Add(1)
		return false
	}
	// Clear expired ban.
	if ce.bannedUtil > 0 && nowSec >= ce.bannedUtil {
		ce.bannedUtil = 0
	}

	// Check global rate limit.
	if !rl.globalBucket.allow(nowNano) {
		ce.deniedRequests++
		me.deniedRequests++
		rl.totalDenied.Add(1)
		return false
	}

	// Check per-client rate limit.
	if !ce.bucket.allow(nowNano) {
		ce.deniedRequests++
		me.deniedRequests++
		rl.totalDenied.Add(1)
		return false
	}

	// Check per-method rate limit.
	mb := ce.methods[method]
	if mb == nil {
		mb = newTokenBucket(rl.config.PerMethodRPS, rl.config.BurstMultiplier)
		ce.methods[method] = mb
	}
	if !mb.allow(nowNano) {
		ce.deniedRequests++
		me.deniedRequests++
		rl.totalDenied.Add(1)
		return false
	}

	ce.allowedRequests++
	return true
}

// Ban manually bans a client IP for the given duration in seconds.
func (rl *RPCRateLimiter) Ban(clientIP string, durationSecs int64) {
	if durationSecs <= 0 {
		durationSecs = rl.config.BanDurationSecs
	}
	nowSec := time.Now().Unix()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	ce := rl.clients[clientIP]
	if ce == nil {
		ce = &clientEntry{
			bucket:  newTokenBucket(rl.config.PerClientRPS, rl.config.BurstMultiplier),
			methods: make(map[string]*tokenBucket),
		}
		rl.clients[clientIP] = ce
	}
	ce.bannedUtil = nowSec + durationSecs
}

// Unban removes a ban on the given client IP.
func (rl *RPCRateLimiter) Unban(clientIP string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if ce, ok := rl.clients[clientIP]; ok {
		ce.bannedUtil = 0
	}
}

// IsBanned returns true if the client IP is currently banned.
func (rl *RPCRateLimiter) IsBanned(clientIP string) bool {
	nowSec := time.Now().Unix()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	ce, ok := rl.clients[clientIP]
	if !ok {
		return false
	}
	return ce.bannedUtil > 0 && nowSec < ce.bannedUtil
}

// ClientStats returns rate limiting statistics for a specific client IP.
// Returns nil if the client has not been seen.
func (rl *RPCRateLimiter) ClientStats(clientIP string) *ClientRateStats {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	ce, ok := rl.clients[clientIP]
	if !ok {
		return nil
	}
	return &ClientRateStats{
		TotalRequests:   ce.totalRequests,
		AllowedRequests: ce.allowedRequests,
		DeniedRequests:  ce.deniedRequests,
		BannedUntil:     ce.bannedUtil,
	}
}

// MethodStats returns rate limiting statistics for a specific RPC method.
// Returns nil if the method has not been called.
func (rl *RPCRateLimiter) MethodStats(method string) *MethodRateStats {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	me, ok := rl.methods[method]
	if !ok {
		return nil
	}

	var avgLatency float64
	if me.totalRequests > 0 {
		avgLatency = float64(me.totalLatencyNs) / float64(me.totalRequests) / 1e6
	}

	return &MethodRateStats{
		TotalRequests:  me.totalRequests,
		DeniedRequests: me.deniedRequests,
		AvgLatencyMs:   avgLatency,
	}
}

// RecordLatency records the latency for a method call, used to compute
// AvgLatencyMs in MethodStats.
func (rl *RPCRateLimiter) RecordLatency(method string, latencyNs int64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	me := rl.methods[method]
	if me == nil {
		me = &methodEntry{}
		rl.methods[method] = me
	}
	me.totalLatencyNs += latencyNs
}

// PruneInactive removes client entries whose lastActive is before the
// given timestamp (unix seconds). Returns the number of entries removed.
func (rl *RPCRateLimiter) PruneInactive(beforeTimestamp int64) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	count := 0
	for ip, ce := range rl.clients {
		if ce.lastActive < beforeTimestamp {
			delete(rl.clients, ip)
			count++
		}
	}
	return count
}

// GlobalStats returns aggregate rate limiting statistics.
func (rl *RPCRateLimiter) GlobalStats() *GlobalRateStats {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	var active, banned uint64
	for _, ce := range rl.clients {
		active++
		if ce.bannedUtil > 0 && time.Now().Unix() < ce.bannedUtil {
			banned++
		}
	}

	return &GlobalRateStats{
		TotalRequests: rl.totalRequests.Load(),
		TotalDenied:   rl.totalDenied.Load(),
		ActiveClients: active,
		BannedClients: banned,
	}
}
