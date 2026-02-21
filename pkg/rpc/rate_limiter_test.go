package rpc

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRateLimiterNewLimiter(t *testing.T) {
	rl := NewRPCRateLimiter(nil)
	if rl == nil {
		t.Fatal("NewRPCRateLimiter returned nil")
	}
	if rl.config.GlobalRPS != 1000 {
		t.Errorf("expected default GlobalRPS=1000, got %d", rl.config.GlobalRPS)
	}
	if rl.config.PerClientRPS != 100 {
		t.Errorf("expected default PerClientRPS=100, got %d", rl.config.PerClientRPS)
	}
	if rl.config.PerMethodRPS != 50 {
		t.Errorf("expected default PerMethodRPS=50, got %d", rl.config.PerMethodRPS)
	}
	if rl.config.BurstMultiplier != 3 {
		t.Errorf("expected default BurstMultiplier=3, got %d", rl.config.BurstMultiplier)
	}
	if rl.config.BanDurationSecs != 60 {
		t.Errorf("expected default BanDurationSecs=60, got %d", rl.config.BanDurationSecs)
	}
}

func TestRateLimiterAllowBasic(t *testing.T) {
	rl := NewRPCRateLimiter(&RPCRateLimitConfig{
		GlobalRPS:       1000,
		PerClientRPS:    100,
		PerMethodRPS:    50,
		BurstMultiplier: 3,
		BanDurationSecs: 60,
	})

	if !rl.Allow("10.0.0.1", "eth_blockNumber") {
		t.Error("first request should be allowed")
	}
}

func TestRateLimiterAllowBurst(t *testing.T) {
	cfg := &RPCRateLimitConfig{
		GlobalRPS:       1000,
		PerClientRPS:    10,
		PerMethodRPS:    10,
		BurstMultiplier: 3,
		BanDurationSecs: 60,
	}
	rl := NewRPCRateLimiter(cfg)

	// Should allow up to burst capacity (10 * 3 = 30 tokens initially).
	allowed := 0
	for i := 0; i < 30; i++ {
		if rl.Allow("10.0.0.1", "eth_blockNumber") {
			allowed++
		}
	}
	if allowed < 25 {
		t.Errorf("expected at least 25 allowed in burst, got %d", allowed)
	}
}

func TestRateLimiterDenyExceedRate(t *testing.T) {
	cfg := &RPCRateLimitConfig{
		GlobalRPS:       1000,
		PerClientRPS:    5,
		PerMethodRPS:    5,
		BurstMultiplier: 1,
		BanDurationSecs: 60,
	}
	rl := NewRPCRateLimiter(cfg)

	// Exhaust the bucket (5 tokens with burst=1).
	for i := 0; i < 5; i++ {
		rl.Allow("10.0.0.1", "eth_blockNumber")
	}

	// Next request should be denied.
	if rl.Allow("10.0.0.1", "eth_blockNumber") {
		t.Error("request should be denied after exceeding rate")
	}
}

func TestRateLimiterBan(t *testing.T) {
	rl := NewRPCRateLimiter(nil)

	rl.Ban("10.0.0.1", 60)
	if !rl.IsBanned("10.0.0.1") {
		t.Error("client should be banned after Ban()")
	}
	if rl.Allow("10.0.0.1", "eth_blockNumber") {
		t.Error("banned client request should be denied")
	}
}

func TestRateLimiterUnban(t *testing.T) {
	rl := NewRPCRateLimiter(nil)

	rl.Ban("10.0.0.1", 60)
	rl.Unban("10.0.0.1")
	if rl.IsBanned("10.0.0.1") {
		t.Error("client should not be banned after Unban()")
	}
	if !rl.Allow("10.0.0.1", "eth_blockNumber") {
		t.Error("unbanned client request should be allowed")
	}
}

func TestRateLimiterIsBanned(t *testing.T) {
	rl := NewRPCRateLimiter(nil)

	if rl.IsBanned("10.0.0.99") {
		t.Error("unknown client should not be banned")
	}

	rl.Ban("10.0.0.1", 60)
	if !rl.IsBanned("10.0.0.1") {
		t.Error("banned client should return true")
	}
}

func TestRateLimiterClientStats(t *testing.T) {
	rl := NewRPCRateLimiter(nil)

	// Unknown client returns nil.
	if stats := rl.ClientStats("unknown"); stats != nil {
		t.Error("expected nil stats for unknown client")
	}

	rl.Allow("10.0.0.1", "eth_blockNumber")
	rl.Allow("10.0.0.1", "eth_getBalance")

	stats := rl.ClientStats("10.0.0.1")
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.TotalRequests != 2 {
		t.Errorf("expected 2 total requests, got %d", stats.TotalRequests)
	}
	if stats.AllowedRequests != 2 {
		t.Errorf("expected 2 allowed requests, got %d", stats.AllowedRequests)
	}
	if stats.DeniedRequests != 0 {
		t.Errorf("expected 0 denied requests, got %d", stats.DeniedRequests)
	}
}

func TestRateLimiterMethodStats(t *testing.T) {
	rl := NewRPCRateLimiter(nil)

	// Unknown method returns nil.
	if stats := rl.MethodStats("unknown"); stats != nil {
		t.Error("expected nil stats for unknown method")
	}

	rl.Allow("10.0.0.1", "eth_blockNumber")
	rl.Allow("10.0.0.2", "eth_blockNumber")

	stats := rl.MethodStats("eth_blockNumber")
	if stats == nil {
		t.Fatal("expected non-nil method stats")
	}
	if stats.TotalRequests != 2 {
		t.Errorf("expected 2 total requests, got %d", stats.TotalRequests)
	}
}

func TestRateLimiterMethodStatsLatency(t *testing.T) {
	rl := NewRPCRateLimiter(nil)

	rl.Allow("10.0.0.1", "eth_call")
	rl.RecordLatency("eth_call", 5_000_000)  // 5ms
	rl.RecordLatency("eth_call", 15_000_000) // 15ms

	stats := rl.MethodStats("eth_call")
	if stats == nil {
		t.Fatal("expected non-nil method stats")
	}
	// avg = (5+15)/1 = 20ms total over 1 request that counted
	// But we have 1 totalRequest and 20_000_000 ns total latency
	// avg = 20_000_000 / 1 / 1e6 = 20.0ms
	if stats.AvgLatencyMs < 1.0 {
		t.Errorf("expected positive avg latency, got %.2f", stats.AvgLatencyMs)
	}
}

func TestRateLimiterGlobalStats(t *testing.T) {
	rl := NewRPCRateLimiter(nil)

	rl.Allow("10.0.0.1", "eth_blockNumber")
	rl.Allow("10.0.0.2", "eth_getBalance")
	rl.Ban("10.0.0.3", 60)

	stats := rl.GlobalStats()
	if stats.TotalRequests != 2 {
		t.Errorf("expected 2 total requests, got %d", stats.TotalRequests)
	}
	if stats.ActiveClients < 2 {
		t.Errorf("expected at least 2 active clients, got %d", stats.ActiveClients)
	}
	if stats.BannedClients != 1 {
		t.Errorf("expected 1 banned client, got %d", stats.BannedClients)
	}
}

func TestRateLimiterPruneInactive(t *testing.T) {
	rl := NewRPCRateLimiter(nil)

	rl.Allow("10.0.0.1", "eth_blockNumber")
	rl.Allow("10.0.0.2", "eth_blockNumber")

	// Prune with a future timestamp to remove all entries.
	futureTS := time.Now().Unix() + 3600
	removed := rl.PruneInactive(futureTS)
	if removed != 2 {
		t.Errorf("expected 2 pruned, got %d", removed)
	}

	// Stats should now be nil.
	if rl.ClientStats("10.0.0.1") != nil {
		t.Error("client should be pruned")
	}
}

func TestRateLimiterPruneInactiveSelective(t *testing.T) {
	rl := NewRPCRateLimiter(nil)

	rl.Allow("10.0.0.1", "eth_blockNumber")

	// Set a prune threshold of 0 (should not prune anything active now).
	removed := rl.PruneInactive(0)
	if removed != 0 {
		t.Errorf("expected 0 pruned with timestamp=0, got %d", removed)
	}
}

func TestRateLimiterConcurrentAccess(t *testing.T) {
	rl := NewRPCRateLimiter(&RPCRateLimitConfig{
		GlobalRPS:       10000,
		PerClientRPS:    1000,
		PerMethodRPS:    500,
		BurstMultiplier: 3,
		BanDurationSecs: 60,
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ip := fmt.Sprintf("10.0.0.%d", id%5)
			for j := 0; j < 50; j++ {
				rl.Allow(ip, "eth_blockNumber")
			}
		}(i)
	}
	wg.Wait()

	stats := rl.GlobalStats()
	if stats.TotalRequests != 1000 {
		t.Errorf("expected 1000 total requests, got %d", stats.TotalRequests)
	}
}

func TestRateLimiterPerMethodLimit(t *testing.T) {
	cfg := &RPCRateLimitConfig{
		GlobalRPS:       10000,
		PerClientRPS:    10000,
		PerMethodRPS:    5,
		BurstMultiplier: 1,
		BanDurationSecs: 60,
	}
	rl := NewRPCRateLimiter(cfg)

	// Exhaust method limit.
	for i := 0; i < 5; i++ {
		rl.Allow("10.0.0.1", "eth_call")
	}

	// Method limit exceeded but different method still works.
	if !rl.Allow("10.0.0.1", "eth_blockNumber") {
		t.Error("different method should still be allowed")
	}
}

func TestRateLimiterConfig(t *testing.T) {
	cfg := &RPCRateLimitConfig{
		GlobalRPS:       500,
		PerClientRPS:    50,
		PerMethodRPS:    25,
		BurstMultiplier: 2,
		BanDurationSecs: 120,
	}
	rl := NewRPCRateLimiter(cfg)

	if rl.config.GlobalRPS != 500 {
		t.Errorf("expected GlobalRPS=500, got %d", rl.config.GlobalRPS)
	}
	if rl.config.PerClientRPS != 50 {
		t.Errorf("expected PerClientRPS=50, got %d", rl.config.PerClientRPS)
	}
	if rl.config.BurstMultiplier != 2 {
		t.Errorf("expected BurstMultiplier=2, got %d", rl.config.BurstMultiplier)
	}
}

func TestRateLimiterAutoBanViaExceed(t *testing.T) {
	cfg := &RPCRateLimitConfig{
		GlobalRPS:       1000,
		PerClientRPS:    5,
		PerMethodRPS:    50,
		BurstMultiplier: 1,
		BanDurationSecs: 60,
	}
	rl := NewRPCRateLimiter(cfg)

	// Exhaust tokens.
	for i := 0; i < 5; i++ {
		rl.Allow("10.0.0.1", "eth_blockNumber")
	}

	// Now requests are denied.
	denied := 0
	for i := 0; i < 5; i++ {
		if !rl.Allow("10.0.0.1", "eth_blockNumber") {
			denied++
		}
	}
	if denied == 0 {
		t.Error("expected some denied requests after exhausting rate limit")
	}

	// Check stats reflect denials.
	stats := rl.ClientStats("10.0.0.1")
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.DeniedRequests == 0 {
		t.Error("expected non-zero denied requests in stats")
	}
}

func TestRateLimiterBanDefaultDuration(t *testing.T) {
	rl := NewRPCRateLimiter(nil)

	// Ban with duration 0 should use default.
	rl.Ban("10.0.0.1", 0)
	if !rl.IsBanned("10.0.0.1") {
		t.Error("client should be banned with default duration")
	}

	stats := rl.ClientStats("10.0.0.1")
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.BannedUntil == 0 {
		t.Error("expected non-zero BannedUntil")
	}
}

func TestRateLimiterGlobalRateLimit(t *testing.T) {
	cfg := &RPCRateLimitConfig{
		GlobalRPS:       5,
		PerClientRPS:    1000,
		PerMethodRPS:    1000,
		BurstMultiplier: 1,
		BanDurationSecs: 60,
	}
	rl := NewRPCRateLimiter(cfg)

	// Exhaust global bucket (5 tokens).
	for i := 0; i < 5; i++ {
		rl.Allow(fmt.Sprintf("10.0.0.%d", i), "eth_blockNumber")
	}

	// Global rate exceeded, even from a new client.
	if rl.Allow("10.0.0.99", "eth_blockNumber") {
		t.Error("request should be denied when global rate is exceeded")
	}
}

func TestRateLimiterBurstMultiplierZero(t *testing.T) {
	cfg := &RPCRateLimitConfig{
		GlobalRPS:       100,
		PerClientRPS:    10,
		PerMethodRPS:    10,
		BurstMultiplier: 0, // should be clamped to 1
		BanDurationSecs: 60,
	}
	rl := NewRPCRateLimiter(cfg)

	// Should still work (burst clamped to 1).
	if !rl.Allow("10.0.0.1", "eth_blockNumber") {
		t.Error("first request should be allowed even with zero burst multiplier")
	}
}
