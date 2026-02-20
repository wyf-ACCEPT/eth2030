package das

import (
	"errors"
	"sync"
	"testing"
)

func TestTeraEnforce_DefaultConfig(t *testing.T) {
	cfg := DefaultBandwidthConfig()
	if cfg.GlobalCapBytesPerSec != 1<<30 {
		t.Errorf("GlobalCapBytesPerSec = %d, want %d", cfg.GlobalCapBytesPerSec, uint64(1<<30))
	}
	if cfg.CongestionThreshold != 0.80 {
		t.Errorf("CongestionThreshold = %f, want 0.80", cfg.CongestionThreshold)
	}
	if cfg.BackpressureThreshold != 0.95 {
		t.Errorf("BackpressureThreshold = %f, want 0.95", cfg.BackpressureThreshold)
	}
}

func TestTeraEnforce_NewBandwidthEnforcer(t *testing.T) {
	be, err := NewBandwidthEnforcer(DefaultBandwidthConfig())
	if err != nil {
		t.Fatalf("NewBandwidthEnforcer: %v", err)
	}
	if be.RegisteredChains() != 0 {
		t.Errorf("RegisteredChains() = %d, want 0", be.RegisteredChains())
	}
}

func TestTeraEnforce_ZeroCapError(t *testing.T) {
	_, err := NewBandwidthEnforcer(BandwidthConfig{})
	if !errors.Is(err, ErrZeroBandwidthCap) {
		t.Errorf("expected ErrZeroBandwidthCap, got %v", err)
	}
}

func TestTeraEnforce_RegisterChain(t *testing.T) {
	be, _ := NewBandwidthEnforcer(DefaultBandwidthConfig())
	be.RegisterChain(100, 0) // default quota
	be.RegisterChain(200, 1<<20)

	if be.RegisteredChains() != 2 {
		t.Errorf("RegisteredChains() = %d, want 2", be.RegisteredChains())
	}
}

func TestTeraEnforce_BasicBandwidthRequest(t *testing.T) {
	be, _ := NewBandwidthEnforcer(BandwidthConfig{
		GlobalCapBytesPerSec:  1 << 30,
		DefaultChainQuota:     1 << 20,
		CongestionThreshold:   0.80,
		CongestionMultiplier:  2.0,
		BackpressureThreshold: 0.95,
	})

	be.RegisterChain(1, 1<<20)

	// Small request should succeed.
	err := be.RequestBandwidth(1, 1024)
	if err != nil {
		t.Errorf("RequestBandwidth(1024): %v", err)
	}

	allowed, dropped, _, _ := be.Metrics()
	if allowed != 1024 {
		t.Errorf("allowed = %d, want 1024", allowed)
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0", dropped)
	}
}

func TestTeraEnforce_UnregisteredChain(t *testing.T) {
	be, _ := NewBandwidthEnforcer(DefaultBandwidthConfig())

	err := be.RequestBandwidth(999, 1024)
	if !errors.Is(err, ErrChainNotRegistered) {
		t.Errorf("expected ErrChainNotRegistered, got %v", err)
	}
}

func TestTeraEnforce_ChainQuotaExhaustion(t *testing.T) {
	be, _ := NewBandwidthEnforcer(BandwidthConfig{
		GlobalCapBytesPerSec:  1 << 30,
		DefaultChainQuota:     1000, // very small quota
		CongestionThreshold:   0.80,
		CongestionMultiplier:  2.0,
		BackpressureThreshold: 0.95,
	})

	be.RegisterChain(1, 1000) // 1000 bytes/sec, starts with 1000 tokens

	// First request for 800 bytes should succeed (we start with 1000 tokens).
	err := be.RequestBandwidth(1, 800)
	if err != nil {
		t.Errorf("first request: %v", err)
	}

	// Second request for 800 should fail (only ~200 tokens remaining).
	err = be.RequestBandwidth(1, 800)
	if err == nil {
		t.Error("expected bandwidth exhaustion error")
	}
}

func TestTeraEnforce_GlobalUtilization(t *testing.T) {
	be, _ := NewBandwidthEnforcer(DefaultBandwidthConfig())
	be.RegisterChain(1, 0)

	util := be.GlobalUtilization()
	// Fresh bucket should have low utilization (close to 0).
	if util > 0.5 {
		t.Errorf("initial GlobalUtilization() = %f, want < 0.5", util)
	}
}

func TestTeraEnforce_ChainUtilization(t *testing.T) {
	be, _ := NewBandwidthEnforcer(DefaultBandwidthConfig())

	// Non-existent chain.
	util := be.ChainUtilization(999)
	if util != 0 {
		t.Errorf("ChainUtilization(999) = %f, want 0", util)
	}

	be.RegisterChain(1, 0)
	util = be.ChainUtilization(1)
	if util > 0.5 {
		t.Errorf("initial ChainUtilization(1) = %f, want < 0.5", util)
	}
}

func TestTeraEnforce_ConcurrentRequests(t *testing.T) {
	be, _ := NewBandwidthEnforcer(BandwidthConfig{
		GlobalCapBytesPerSec:  1 << 30,
		DefaultChainQuota:     1 << 28,
		CongestionThreshold:   0.80,
		CongestionMultiplier:  2.0,
		BackpressureThreshold: 0.95,
	})

	for i := uint64(1); i <= 4; i++ {
		be.RegisterChain(i, 0)
	}

	var wg sync.WaitGroup
	for i := uint64(1); i <= 4; i++ {
		wg.Add(1)
		go func(chainID uint64) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				be.RequestBandwidth(chainID, 1024)
			}
		}(i)
	}
	wg.Wait()

	allowed, _, _, _ := be.Metrics()
	if allowed == 0 {
		t.Error("expected some allowed bytes after concurrent requests")
	}
}

func TestTeraEnforce_CongestionDetection(t *testing.T) {
	be, _ := NewBandwidthEnforcer(BandwidthConfig{
		GlobalCapBytesPerSec:  10000,
		DefaultChainQuota:     10000,
		CongestionThreshold:   0.80,
		CongestionMultiplier:  2.0,
		BackpressureThreshold: 0.95,
	})

	be.RegisterChain(1, 10000)

	// Should not be congested initially.
	if be.IsCongested() {
		t.Error("should not be congested initially")
	}
}

func TestTeraEnforce_BackpressureDetection(t *testing.T) {
	be, _ := NewBandwidthEnforcer(BandwidthConfig{
		GlobalCapBytesPerSec:  10000,
		DefaultChainQuota:     10000,
		CongestionThreshold:   0.80,
		CongestionMultiplier:  2.0,
		BackpressureThreshold: 0.95,
	})

	be.RegisterChain(1, 10000)

	// Should not have backpressure initially.
	if be.IsBackpressureActive() {
		t.Error("should not have backpressure initially")
	}
}

func TestTeraEnforce_MetricsSnapshot(t *testing.T) {
	be, _ := NewBandwidthEnforcer(DefaultBandwidthConfig())
	be.RegisterChain(1, 0)

	be.RequestBandwidth(1, 2048)
	be.RequestBandwidth(1, 4096)

	allowed, dropped, congestion, backpressure := be.Metrics()
	if allowed != 2048+4096 {
		t.Errorf("allowed = %d, want %d", allowed, 2048+4096)
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0", dropped)
	}
	// No congestion expected with default config and small requests.
	_ = congestion
	_ = backpressure
}

func TestTeraEnforce_ConfigAccessor(t *testing.T) {
	cfg := DefaultBandwidthConfig()
	be, _ := NewBandwidthEnforcer(cfg)

	got := be.Config()
	if got.GlobalCapBytesPerSec != cfg.GlobalCapBytesPerSec {
		t.Errorf("Config().GlobalCapBytesPerSec = %d, want %d",
			got.GlobalCapBytesPerSec, cfg.GlobalCapBytesPerSec)
	}
}
