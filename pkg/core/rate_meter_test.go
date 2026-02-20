package core

import (
	"math"
	"testing"
)

func TestRateMeter_DefaultConfig(t *testing.T) {
	cfg := DefaultRateMeterConfig()
	if cfg.WindowSize != 64 {
		t.Errorf("WindowSize = %d, want 64", cfg.WindowSize)
	}
	if cfg.TargetGasPerSec != 1_000_000_000 {
		t.Errorf("TargetGasPerSec = %f, want 1e9", cfg.TargetGasPerSec)
	}
	if cfg.EMAAlpha != 0.1 {
		t.Errorf("EMAAlpha = %f, want 0.1", cfg.EMAAlpha)
	}
}

func TestRateMeter_NewRateMeter(t *testing.T) {
	rm := NewRateMeter(DefaultRateMeterConfig())
	if rm.WindowSize() != 64 {
		t.Errorf("WindowSize() = %d, want 64", rm.WindowSize())
	}
	if rm.RecordCount() != 0 {
		t.Errorf("RecordCount() = %d, want 0", rm.RecordCount())
	}
}

func TestRateMeter_InvalidConfig(t *testing.T) {
	// Zero values should be corrected.
	rm := NewRateMeter(RateMeterConfig{})
	if rm.WindowSize() != 64 {
		t.Errorf("WindowSize() = %d, want 64 (default)", rm.WindowSize())
	}
	if rm.TargetGasPerSec() != 1_000_000_000 {
		t.Errorf("TargetGasPerSec() = %f, want 1e9", rm.TargetGasPerSec())
	}
}

func TestRateMeter_RecordBlockAndRate(t *testing.T) {
	rm := NewRateMeter(RateMeterConfig{
		WindowSize:      10,
		TargetGasPerSec: 1_000_000_000,
		EMAAlpha:        0.5,
		MinWorkers:      2,
		MaxWorkers:      32,
	})

	// Record blocks at 12-second intervals, each using 100M gas.
	for i := 0; i < 5; i++ {
		rm.RecordBlock(uint64(i), 100_000_000, uint64(i*12))
	}

	if rm.RecordCount() != 5 {
		t.Errorf("RecordCount() = %d, want 5", rm.RecordCount())
	}

	// Current rate should be > 0.
	rate := rm.CurrentRate()
	if rate <= 0 {
		t.Errorf("CurrentRate() = %f, want > 0", rate)
	}
}

func TestRateMeter_RollingAverage(t *testing.T) {
	rm := NewRateMeter(RateMeterConfig{
		WindowSize:      10,
		TargetGasPerSec: 1_000_000_000,
		EMAAlpha:        0.1,
		MinWorkers:      2,
		MaxWorkers:      32,
	})

	// Single record: rolling average should be 0.
	rm.RecordBlock(0, 100_000_000, 0)
	if avg := rm.RollingAverageRate(); avg != 0 {
		t.Errorf("RollingAverageRate() with 1 record = %f, want 0", avg)
	}

	// Two records, 12 seconds apart, 120M gas each.
	rm.RecordBlock(1, 120_000_000, 12)

	avg := rm.RollingAverageRate()
	// Total gas = 220M, time span = 12s, rate = 220M/12 ~ 18.33M gas/sec.
	expected := 220_000_000.0 / 12.0
	if math.Abs(avg-expected) > 1000 {
		t.Errorf("RollingAverageRate() = %f, want ~%f", avg, expected)
	}
}

func TestRateMeter_WindowTrimming(t *testing.T) {
	rm := NewRateMeter(RateMeterConfig{
		WindowSize:      5,
		TargetGasPerSec: 1_000_000_000,
		EMAAlpha:        0.1,
		MinWorkers:      2,
		MaxWorkers:      32,
	})

	for i := 0; i < 10; i++ {
		rm.RecordBlock(uint64(i), 100_000_000, uint64(i*12))
	}

	if rm.RecordCount() != 5 {
		t.Errorf("RecordCount() = %d, want 5 (window trimmed)", rm.RecordCount())
	}
}

func TestRateMeter_AdaptiveParallelism_BelowTarget(t *testing.T) {
	rm := NewRateMeter(RateMeterConfig{
		WindowSize:      10,
		TargetGasPerSec: 1_000_000_000,
		EMAAlpha:        1.0, // Use instant rate for testing.
		MinWorkers:      2,
		MaxWorkers:      128,
	})

	// Record very low gas usage: 1M gas at 12s interval = ~83K gas/sec.
	// Far below 1 Ggas target.
	rm.RecordBlock(0, 1_000_000, 0)
	rm.RecordBlock(1, 1_000_000, 12)

	workers := rm.RecommendedWorkers()
	// Should have increased from minimum.
	if workers < 2 {
		t.Errorf("workers = %d, expected >= 2", workers)
	}
}

func TestRateMeter_AdaptiveParallelism_AboveTarget(t *testing.T) {
	rm := NewRateMeter(RateMeterConfig{
		WindowSize:      10,
		TargetGasPerSec: 1_000_000,
		EMAAlpha:        1.0,
		MinWorkers:      2,
		MaxWorkers:      128,
	})

	// Record high gas: 100M gas at 1s interval = 100M gas/sec.
	// Way above 1M target.
	rm.RecordBlock(0, 100_000_000, 0)
	rm.RecordBlock(1, 100_000_000, 1)

	workers := rm.RecommendedWorkers()
	// Workers should be at minimum since rate is way above target.
	if workers > 128 {
		t.Errorf("workers = %d, expected <= 128", workers)
	}
}

func TestRateMeter_UtilizationRatio(t *testing.T) {
	rm := NewRateMeter(RateMeterConfig{
		WindowSize:      10,
		TargetGasPerSec: 1_000_000,
		EMAAlpha:        1.0,
		MinWorkers:      2,
		MaxWorkers:      32,
	})

	// No records: utilization should be 0.
	if r := rm.UtilizationRatio(); r != 0 {
		t.Errorf("UtilizationRatio() = %f, want 0", r)
	}

	// 1M gas over 1 second = 1M gas/sec = 100% of target.
	rm.RecordBlock(0, 1_000_000, 0)
	rm.RecordBlock(1, 1_000_000, 1)

	ratio := rm.UtilizationRatio()
	if math.Abs(ratio-1.0) > 0.01 {
		t.Errorf("UtilizationRatio() = %f, want ~1.0", ratio)
	}
}

func TestRateMeter_IsAtTarget(t *testing.T) {
	rm := NewRateMeter(RateMeterConfig{
		WindowSize:      10,
		TargetGasPerSec: 1_000_000,
		EMAAlpha:        1.0,
		MinWorkers:      2,
		MaxWorkers:      32,
	})

	// No data: not at target.
	if rm.IsAtTarget() {
		t.Error("IsAtTarget() should be false with no data")
	}

	// Exactly at target.
	rm.RecordBlock(0, 1_000_000, 0)
	rm.RecordBlock(1, 1_000_000, 1)

	if !rm.IsAtTarget() {
		t.Errorf("IsAtTarget() = false, want true (ratio=%f)", rm.UtilizationRatio())
	}
}

func TestRateMeter_Reset(t *testing.T) {
	rm := NewRateMeter(DefaultRateMeterConfig())
	rm.RecordBlock(0, 100_000_000, 0)
	rm.RecordBlock(1, 100_000_000, 12)

	rm.Reset()

	if rm.RecordCount() != 0 {
		t.Errorf("RecordCount() after reset = %d, want 0", rm.RecordCount())
	}
	if rm.CurrentRate() != 0 {
		t.Errorf("CurrentRate() after reset = %f, want 0", rm.CurrentRate())
	}
}

func TestRateMeter_ZeroTimeDelta(t *testing.T) {
	rm := NewRateMeter(RateMeterConfig{
		WindowSize:      10,
		TargetGasPerSec: 1_000_000_000,
		EMAAlpha:        0.5,
		MinWorkers:      2,
		MaxWorkers:      32,
	})

	// Two records at the same timestamp.
	rm.RecordBlock(0, 100_000_000, 100)
	rm.RecordBlock(1, 200_000_000, 100)

	// Rate should remain 0 (no time passed).
	if rate := rm.RollingAverageRate(); rate != 0 {
		t.Errorf("RollingAverageRate() with same timestamp = %f, want 0", rate)
	}
}
