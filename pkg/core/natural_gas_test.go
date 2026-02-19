package core

import (
	"math"
	"sync"
	"testing"
)

func TestDefaultNaturalGasConfig(t *testing.T) {
	cfg := DefaultNaturalGasConfig()
	if cfg.TargetUtilization != 0.5 {
		t.Errorf("TargetUtilization: got %f, want 0.5", cfg.TargetUtilization)
	}
	if cfg.AdjustmentSpeed != 8 {
		t.Errorf("AdjustmentSpeed: got %d, want 8", cfg.AdjustmentSpeed)
	}
	if cfg.MinGasLimit != 5000 {
		t.Errorf("MinGasLimit: got %d, want 5000", cfg.MinGasLimit)
	}
	if cfg.MaxGasLimit != 0 {
		t.Errorf("MaxGasLimit: got %d, want 0 (no max)", cfg.MaxGasLimit)
	}
}

func TestNewNaturalGasManager_DefaultsForInvalid(t *testing.T) {
	// Zero config should get defaults.
	m := NewNaturalGasManager(NaturalGasConfig{})
	cfg := m.Config()
	if cfg.TargetUtilization != 0.5 {
		t.Errorf("TargetUtilization: got %f, want 0.5", cfg.TargetUtilization)
	}
	if cfg.AdjustmentSpeed != 8 {
		t.Errorf("AdjustmentSpeed: got %d, want 8", cfg.AdjustmentSpeed)
	}
	if cfg.MinGasLimit != 5000 {
		t.Errorf("MinGasLimit: got %d, want 5000", cfg.MinGasLimit)
	}

	// Negative target utilization should default to 0.5.
	m = NewNaturalGasManager(NaturalGasConfig{TargetUtilization: -0.1})
	if m.Config().TargetUtilization != 0.5 {
		t.Errorf("negative TargetUtilization should default to 0.5")
	}

	// Target > 1.0 should default to 0.5.
	m = NewNaturalGasManager(NaturalGasConfig{TargetUtilization: 1.5})
	if m.Config().TargetUtilization != 0.5 {
		t.Errorf("TargetUtilization >1 should default to 0.5")
	}
}

func TestNGCalculateTarget_AtTarget(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	// When gas used == target (50% of limit), gas limit should remain the same.
	parentLimit := uint64(30_000_000)
	parentUsed := uint64(15_000_000) // exactly 50%
	result := m.CalculateTargetGasLimit(parentLimit, parentUsed)
	if result != parentLimit {
		t.Errorf("at target: got %d, want %d", result, parentLimit)
	}
}

func TestNGCalculateTarget_OverUtilized(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	parentLimit := uint64(30_000_000)
	parentUsed := uint64(25_000_000) // 83%, well over 50% target

	result := m.CalculateTargetGasLimit(parentLimit, parentUsed)
	if result <= parentLimit {
		t.Errorf("over-utilized: expected increase, got %d (parent %d)", result, parentLimit)
	}
}

func TestNGCalculateTarget_UnderUtilized(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	parentLimit := uint64(30_000_000)
	parentUsed := uint64(5_000_000) // 16.7%, under 50% target

	result := m.CalculateTargetGasLimit(parentLimit, parentUsed)
	if result >= parentLimit {
		t.Errorf("under-utilized: expected decrease, got %d (parent %d)", result, parentLimit)
	}
}

func TestNGCalculateTarget_FullBlock(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	parentLimit := uint64(30_000_000)
	parentUsed := parentLimit // 100% utilization

	result := m.CalculateTargetGasLimit(parentLimit, parentUsed)
	if result <= parentLimit {
		t.Errorf("full block: expected increase, got %d (parent %d)", result, parentLimit)
	}
}

func TestNGCalculateTarget_EmptyBlock(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	parentLimit := uint64(30_000_000)
	parentUsed := uint64(0) // empty block

	result := m.CalculateTargetGasLimit(parentLimit, parentUsed)
	if result >= parentLimit {
		t.Errorf("empty block: expected decrease, got %d (parent %d)", result, parentLimit)
	}
}

func TestNGCalculateTarget_MinBound(t *testing.T) {
	cfg := DefaultNaturalGasConfig()
	cfg.MinGasLimit = 10_000_000
	m := NewNaturalGasManager(cfg)

	// Very low gas limit with empty blocks should hit minimum.
	result := m.CalculateTargetGasLimit(10_000_000, 0)
	if result < cfg.MinGasLimit {
		t.Errorf("below min: got %d, want >= %d", result, cfg.MinGasLimit)
	}
}

func TestNGCalculateTarget_MaxBound(t *testing.T) {
	cfg := DefaultNaturalGasConfig()
	cfg.MaxGasLimit = 50_000_000
	m := NewNaturalGasManager(cfg)

	// High utilization trying to push above max.
	result := m.CalculateTargetGasLimit(50_000_000, 50_000_000)
	if result > cfg.MaxGasLimit {
		t.Errorf("above max: got %d, want <= %d", result, cfg.MaxGasLimit)
	}
}

func TestNGValidate_Valid(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	parentLimit := uint64(30_000_000)
	delta := parentLimit / 1024 // max allowed change

	// Increase within bounds.
	err := m.ValidateGasLimit(parentLimit, parentLimit+delta)
	if err != nil {
		t.Errorf("valid increase: unexpected error: %v", err)
	}

	// Decrease within bounds.
	err = m.ValidateGasLimit(parentLimit, parentLimit-delta)
	if err != nil {
		t.Errorf("valid decrease: unexpected error: %v", err)
	}

	// No change.
	err = m.ValidateGasLimit(parentLimit, parentLimit)
	if err != nil {
		t.Errorf("no change: unexpected error: %v", err)
	}
}

func TestNGValidate_TooLargeIncrease(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	parentLimit := uint64(30_000_000)
	delta := parentLimit/1024 + 1

	err := m.ValidateGasLimit(parentLimit, parentLimit+delta)
	if err != ErrGasLimitDeltaHigh {
		t.Errorf("too large increase: got %v, want ErrGasLimitDeltaHigh", err)
	}
}

func TestNGValidate_TooLargeDecrease(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	parentLimit := uint64(30_000_000)
	delta := parentLimit/1024 + 1

	err := m.ValidateGasLimit(parentLimit, parentLimit-delta)
	if err != ErrGasLimitDeltaHigh {
		t.Errorf("too large decrease: got %v, want ErrGasLimitDeltaHigh", err)
	}
}

func TestNGValidate_BelowMin(t *testing.T) {
	cfg := DefaultNaturalGasConfig()
	cfg.MinGasLimit = 10_000
	m := NewNaturalGasManager(cfg)

	err := m.ValidateGasLimit(10_000, 9_999)
	if err != ErrGasLimitTooLow {
		t.Errorf("below min: got %v, want ErrGasLimitTooLow", err)
	}
}

func TestNGValidate_AboveMax(t *testing.T) {
	cfg := DefaultNaturalGasConfig()
	cfg.MaxGasLimit = 50_000_000
	m := NewNaturalGasManager(cfg)

	err := m.ValidateGasLimit(50_000_000, 50_000_001)
	if err != ErrGasLimitTooHigh {
		t.Errorf("above max: got %v, want ErrGasLimitTooHigh", err)
	}
}

func TestNGAdjustmentFactor_AtTarget(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	factor := m.AdjustmentFactor(15_000_000, 30_000_000) // 50% utilization, 50% target
	if factor != 0 {
		t.Errorf("at target: got %f, want 0", factor)
	}
}

func TestNGAdjustmentFactor_OverTarget(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	factor := m.AdjustmentFactor(24_000_000, 30_000_000) // 80% utilization, 50% target
	if factor <= 0 {
		t.Errorf("over target: got %f, want positive", factor)
	}
	if factor > 1.0 {
		t.Errorf("over target: got %f, want <= 1.0 (clamped)", factor)
	}
}

func TestNGAdjustmentFactor_UnderTarget(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	factor := m.AdjustmentFactor(6_000_000, 30_000_000) // 20% utilization, 50% target
	if factor >= 0 {
		t.Errorf("under target: got %f, want negative", factor)
	}
	if factor < -1.0 {
		t.Errorf("under target: got %f, want >= -1.0 (clamped)", factor)
	}
}

func TestNGAdjustmentFactor_ZeroGasLimit(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())
	factor := m.AdjustmentFactor(0, 0)
	if factor != 0 {
		t.Errorf("zero gas limit: got %f, want 0", factor)
	}
}

func TestNGAdjustmentFactor_FullBlock(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	// 100% utilization with 50% target: factor = (1.0 - 0.5) / 0.5 = 1.0
	factor := m.AdjustmentFactor(30_000_000, 30_000_000)
	if math.Abs(factor-1.0) > 0.001 {
		t.Errorf("full block: got %f, want 1.0", factor)
	}
}

func TestNGAdjustmentFactor_Clamped(t *testing.T) {
	cfg := DefaultNaturalGasConfig()
	cfg.TargetUtilization = 0.1 // very low target
	m := NewNaturalGasManager(cfg)

	// 100% utilization with 10% target: factor = (1.0 - 0.1) / 0.1 = 9.0 -> clamped to 1.0
	factor := m.AdjustmentFactor(30_000_000, 30_000_000)
	if factor != 1.0 {
		t.Errorf("clamped high: got %f, want 1.0", factor)
	}
}

func TestNGProjectGasLimit_StableAtTarget(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	// Project at target utilization: limit should stay approximately the same.
	start := uint64(30_000_000)
	result := m.ProjectGasLimit(start, 100)

	// Allow tiny drift due to integer rounding.
	diff := int64(result) - int64(start)
	if diff < 0 {
		diff = -diff
	}
	// Over 100 blocks at target, drift should be negligible.
	if uint64(diff) > start/100 {
		t.Errorf("stable at target: got %d (diff %d), want ~%d", result, diff, start)
	}
}

func TestNGProjectGasLimit_ZeroBlocks(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())
	result := m.ProjectGasLimit(30_000_000, 0)
	if result != 30_000_000 {
		t.Errorf("zero blocks: got %d, want 30000000", result)
	}
}

func TestNGHistory_RecordAndGet(t *testing.T) {
	h := NewGasLimitHistory(10)

	for i := uint64(1); i <= 5; i++ {
		h.RecordBlock(i, 30_000_000, i*5_000_000)
	}

	entries := h.GetHistory(3)
	if len(entries) != 3 {
		t.Fatalf("GetHistory(3): got %d entries, want 3", len(entries))
	}

	// Should be the last 3 entries.
	if entries[0].BlockNumber != 3 {
		t.Errorf("first entry block: got %d, want 3", entries[0].BlockNumber)
	}
	if entries[2].BlockNumber != 5 {
		t.Errorf("last entry block: got %d, want 5", entries[2].BlockNumber)
	}
}

func TestNGHistory_Utilization(t *testing.T) {
	h := NewGasLimitHistory(10)

	h.RecordBlock(1, 30_000_000, 15_000_000) // 50%
	h.RecordBlock(2, 30_000_000, 30_000_000) // 100%

	entries := h.GetHistory(2)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	if math.Abs(entries[0].Utilization-0.5) > 0.001 {
		t.Errorf("entry 1 utilization: got %f, want 0.5", entries[0].Utilization)
	}
	if math.Abs(entries[1].Utilization-1.0) > 0.001 {
		t.Errorf("entry 2 utilization: got %f, want 1.0", entries[1].Utilization)
	}
}

func TestNGHistory_Eviction(t *testing.T) {
	h := NewGasLimitHistory(3) // small buffer

	for i := uint64(1); i <= 5; i++ {
		h.RecordBlock(i, 30_000_000, 15_000_000)
	}

	if h.Len() != 3 {
		t.Errorf("Len: got %d, want 3", h.Len())
	}

	entries := h.GetHistory(10) // request more than available
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	// Should have blocks 3, 4, 5 (evicted 1 and 2).
	if entries[0].BlockNumber != 3 {
		t.Errorf("oldest entry: got block %d, want 3", entries[0].BlockNumber)
	}
	if entries[2].BlockNumber != 5 {
		t.Errorf("newest entry: got block %d, want 5", entries[2].BlockNumber)
	}
}

func TestNGHistory_EmptyHistory(t *testing.T) {
	h := NewGasLimitHistory(10)

	entries := h.GetHistory(5)
	if entries != nil {
		t.Errorf("empty history: got %v, want nil", entries)
	}

	avg := h.AverageUtilization(5)
	if avg != 0 {
		t.Errorf("empty avg: got %f, want 0", avg)
	}
}

func TestNGAverageUtilization(t *testing.T) {
	h := NewGasLimitHistory(10)

	h.RecordBlock(1, 100, 50)  // 50%
	h.RecordBlock(2, 100, 75)  // 75%
	h.RecordBlock(3, 100, 100) // 100%

	avg := h.AverageUtilization(3)
	expected := (0.5 + 0.75 + 1.0) / 3.0
	if math.Abs(avg-expected) > 0.001 {
		t.Errorf("avg utilization: got %f, want %f", avg, expected)
	}

	// Only last 2 blocks.
	avg2 := h.AverageUtilization(2)
	expected2 := (0.75 + 1.0) / 2.0
	if math.Abs(avg2-expected2) > 0.001 {
		t.Errorf("avg utilization (2): got %f, want %f", avg2, expected2)
	}
}

func TestNGManager_RecordAndHistory(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	m.RecordBlock(1, 30_000_000, 15_000_000)
	m.RecordBlock(2, 30_000_000, 20_000_000)

	entries := m.GetHistory(2)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	avg := m.AverageUtilization(2)
	expected := (0.5 + 20.0/30.0) / 2.0
	if math.Abs(avg-expected) > 0.001 {
		t.Errorf("avg utilization: got %f, want %f", avg, expected)
	}
}

func TestNGManager_ConcurrentAccess(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	var wg sync.WaitGroup
	errCh := make(chan string, 100)

	// Concurrent writers.
	for i := uint64(0); i < 50; i++ {
		wg.Add(1)
		go func(block uint64) {
			defer wg.Done()
			m.RecordBlock(block, 30_000_000, block*500_000)
		}(i)
	}

	// Concurrent readers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.GetHistory(10)
			_ = m.AverageUtilization(10)
			_ = m.CalculateTargetGasLimit(30_000_000, 15_000_000)
			err := m.ValidateGasLimit(30_000_000, 30_000_000)
			if err != nil {
				errCh <- "ValidateGasLimit with no change failed"
			}
			_ = m.AdjustmentFactor(15_000_000, 30_000_000)
			_ = m.ProjectGasLimit(30_000_000, 10)
		}()
	}

	wg.Wait()
	close(errCh)
	for msg := range errCh {
		t.Error(msg)
	}
}

func TestNGHistory_ZeroGasLimit(t *testing.T) {
	h := NewGasLimitHistory(10)
	h.RecordBlock(1, 0, 0)
	entries := h.GetHistory(1)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Utilization != 0 {
		t.Errorf("zero gas limit utilization: got %f, want 0", entries[0].Utilization)
	}
}

func TestNGHistory_InvalidMaxSize(t *testing.T) {
	h := NewGasLimitHistory(0)
	if h.maxSize != 1024 {
		t.Errorf("zero maxSize: got %d, want 1024", h.maxSize)
	}

	h = NewGasLimitHistory(-5)
	if h.maxSize != 1024 {
		t.Errorf("negative maxSize: got %d, want 1024", h.maxSize)
	}
}

func TestNGCalculateTarget_SmallGasLimit(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	// Very small gas limit to test edge cases.
	result := m.CalculateTargetGasLimit(5000, 5000)
	if result < 5000 {
		t.Errorf("small limit full block: got %d, want >= 5000", result)
	}

	result = m.CalculateTargetGasLimit(5000, 0)
	if result < 5000 {
		t.Errorf("small limit empty: got %d, want >= 5000 (min)", result)
	}
}

func TestNGCalculateTarget_GradualConvergence(t *testing.T) {
	m := NewNaturalGasManager(DefaultNaturalGasConfig())

	// Simulate a series of full blocks: gas limit should steadily increase.
	limit := uint64(30_000_000)
	prev := limit
	for i := 0; i < 10; i++ {
		limit = m.CalculateTargetGasLimit(limit, limit) // 100% utilization
		if limit <= prev {
			t.Errorf("block %d: limit did not increase (%d -> %d)", i, prev, limit)
		}
		prev = limit
	}

	// Simulate a series of empty blocks: gas limit should steadily decrease.
	limit = uint64(30_000_000)
	prev = limit
	for i := 0; i < 10; i++ {
		limit = m.CalculateTargetGasLimit(limit, 0) // 0% utilization
		if limit >= prev {
			t.Errorf("block %d: limit did not decrease (%d -> %d)", i, prev, limit)
		}
		prev = limit
	}
}

func TestNGClampFloat64(t *testing.T) {
	if v := clampFloat64(0.5, 0.0, 1.0); v != 0.5 {
		t.Errorf("in range: got %f, want 0.5", v)
	}
	if v := clampFloat64(-2.0, -1.0, 1.0); v != -1.0 {
		t.Errorf("below lo: got %f, want -1.0", v)
	}
	if v := clampFloat64(5.0, 0.0, 1.0); v != 1.0 {
		t.Errorf("above hi: got %f, want 1.0", v)
	}
}

func TestNGValidate_SmallParent(t *testing.T) {
	cfg := DefaultNaturalGasConfig()
	cfg.MinGasLimit = 5000
	m := NewNaturalGasManager(cfg)

	// With a small parent (e.g., 5000), delta = 5000/1024 = 4.
	err := m.ValidateGasLimit(5000, 5004)
	if err != nil {
		t.Errorf("small parent valid: unexpected error: %v", err)
	}

	err = m.ValidateGasLimit(5000, 5005)
	if err != ErrGasLimitDeltaHigh {
		t.Errorf("small parent invalid: got %v, want ErrGasLimitDeltaHigh", err)
	}
}
