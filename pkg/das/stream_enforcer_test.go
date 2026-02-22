package das

import (
	"errors"
	"testing"
)

func TestNewStreamingPipeline_NilEnforcer(t *testing.T) {
	_, err := NewStreamingPipeline(nil)
	if err != ErrStreamNilEnforcer {
		t.Errorf("expected ErrStreamNilEnforcer, got %v", err)
	}
}

func TestStreamingPipeline_ProcessBlob_RegisteredChain(t *testing.T) {
	cfg := DefaultBandwidthConfig()
	cfg.GlobalCapBytesPerSec = 1 << 20 // 1 MiB/s
	cfg.DefaultChainQuota = 512 << 10  // 512 KiB/s
	be, err := NewBandwidthEnforcer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	be.RegisterChain(1, 0)

	sp, err := NewStreamingPipeline(be)
	if err != nil {
		t.Fatal(err)
	}

	// Process a small blob on a registered chain.
	blob := make([]byte, 1024)
	if err := sp.ProcessBlob(1, blob); err != nil {
		t.Errorf("ProcessBlob should succeed for registered chain, got: %v", err)
	}

	// Check throughput stats are non-negative.
	stats := sp.GetThroughputStats()
	if stats.Utilization < 0 {
		t.Error("utilization should be >= 0")
	}
}

func TestStreamingPipeline_ProcessBlob_UnregisteredChain_GlobalFallback(t *testing.T) {
	cfg := DefaultBandwidthConfig()
	cfg.GlobalCapBytesPerSec = 1 << 20
	be, err := NewBandwidthEnforcer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Do NOT register chain 99.

	sp, err := NewStreamingPipeline(be)
	if err != nil {
		t.Fatal(err)
	}

	// Should fall back to global-only enforcement.
	blob := make([]byte, 512)
	if err := sp.ProcessBlob(99, blob); err != nil {
		t.Errorf("ProcessBlob with unregistered chain should use global fallback, got: %v", err)
	}
}

func TestStreamingPipeline_ProcessBlob_EmptyBlob(t *testing.T) {
	cfg := DefaultBandwidthConfig()
	be, err := NewBandwidthEnforcer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	sp, err := NewStreamingPipeline(be)
	if err != nil {
		t.Fatal(err)
	}

	// Empty blob should succeed (no-op).
	if err := sp.ProcessBlob(1, nil); err != nil {
		t.Errorf("empty blob should succeed, got: %v", err)
	}
	if err := sp.ProcessBlob(1, []byte{}); err != nil {
		t.Errorf("zero-length blob should succeed, got: %v", err)
	}
}

func TestStreamingPipeline_BackpressureActive(t *testing.T) {
	cfg := DefaultBandwidthConfig()
	cfg.GlobalCapBytesPerSec = 100
	cfg.BackpressureThreshold = 0.95
	be, err := NewBandwidthEnforcer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	sp, err := NewStreamingPipeline(be)
	if err != nil {
		t.Fatal(err)
	}

	// Initially should not be under backpressure.
	if sp.BackpressureActive() {
		t.Error("backpressure should not be active initially")
	}
}

func TestStreamingPipeline_GetThroughputStats_Initial(t *testing.T) {
	cfg := DefaultBandwidthConfig()
	be, err := NewBandwidthEnforcer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	sp, err := NewStreamingPipeline(be)
	if err != nil {
		t.Fatal(err)
	}

	stats := sp.GetThroughputStats()
	if stats.CurrentBytesPerSec != 0 {
		t.Errorf("initial CurrentBytesPerSec = %f, want 0", stats.CurrentBytesPerSec)
	}
	if stats.PeakBytesPerSec != 0 {
		t.Errorf("initial PeakBytesPerSec = %f, want 0", stats.PeakBytesPerSec)
	}
	if stats.Utilization != 0 {
		t.Errorf("initial Utilization = %f, want 0", stats.Utilization)
	}
}

func TestTeradataManager_WithBandwidthEnforcer(t *testing.T) {
	cfg := DefaultBandwidthConfig()
	cfg.GlobalCapBytesPerSec = 1 << 20 // 1 MiB/s
	cfg.DefaultChainQuota = 512 << 10  // 512 KiB/s
	be, err := NewBandwidthEnforcer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	be.RegisterChain(1, 0)

	m := NewTeradataManager(DefaultTeradataConfig())
	m.SetBandwidthEnforcer(be)

	// Should succeed: small data on a registered chain.
	receipt, err := m.StoreL2Data(1, []byte("hello world"))
	if err != nil {
		t.Fatalf("StoreL2Data with enforcer failed: %v", err)
	}
	if receipt == nil {
		t.Fatal("receipt is nil")
	}
}

func TestTeradataManager_BandwidthDenied_UnregisteredChain(t *testing.T) {
	cfg := DefaultBandwidthConfig()
	cfg.GlobalCapBytesPerSec = 1 << 20
	be, err := NewBandwidthEnforcer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Do NOT register chain 42.

	m := NewTeradataManager(DefaultTeradataConfig())
	m.SetBandwidthEnforcer(be)

	// Should fail because chain 42 is not registered with the enforcer.
	_, err = m.StoreL2Data(42, []byte("data"))
	if err == nil {
		t.Fatal("expected error for unregistered chain")
	}
	if !errors.Is(err, ErrTeradataBandwidthDenied) {
		t.Errorf("expected ErrTeradataBandwidthDenied, got: %v", err)
	}
}

func TestTeradataManager_NoBandwidthEnforcer(t *testing.T) {
	// Without an enforcer, everything should work as before.
	m := NewTeradataManager(DefaultTeradataConfig())
	receipt, err := m.StoreL2Data(1, []byte("no enforcer"))
	if err != nil {
		t.Fatalf("StoreL2Data without enforcer failed: %v", err)
	}
	if receipt == nil {
		t.Fatal("receipt is nil")
	}
}

func TestBandwidthEnforcer_EnforceOnGossip(t *testing.T) {
	cfg := DefaultBandwidthConfig()
	cfg.GlobalCapBytesPerSec = 1 << 20 // 1 MiB/s
	be, err := NewBandwidthEnforcer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Small blob should pass global enforcement.
	if err := be.EnforceOnGossip(1024); err != nil {
		t.Errorf("EnforceOnGossip should succeed for small blob, got: %v", err)
	}

	// Metrics should be updated.
	allowed, _, _, _ := be.Metrics()
	if allowed == 0 {
		t.Error("allowed bytes should be > 0 after EnforceOnGossip")
	}
}

func TestBandwidthEnforcer_EnforceOnGossip_ExceedsGlobal(t *testing.T) {
	cfg := DefaultBandwidthConfig()
	cfg.GlobalCapBytesPerSec = 100 // Very small cap.
	be, err := NewBandwidthEnforcer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Consume the bucket.
	be.EnforceOnGossip(200)

	// Next request should fail (bucket empty).
	err = be.EnforceOnGossip(200)
	if err == nil {
		t.Error("expected error when global cap exceeded")
	}
}
