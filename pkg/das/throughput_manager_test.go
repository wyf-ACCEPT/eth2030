package das

import (
	"sync"
	"testing"
)

func TestDefaultThroughputConfig(t *testing.T) {
	cfg := DefaultThroughputConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultThroughputConfig is invalid: %v", err)
	}
}

func TestThroughputConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*ThroughputConfig)
		wantErr bool
	}{
		{
			name:    "valid default",
			modify:  func(c *ThroughputConfig) {},
			wantErr: false,
		},
		{
			name:    "zero base",
			modify:  func(c *ThroughputConfig) { c.BaseBlobsPerBlock = 0 },
			wantErr: true,
		},
		{
			name:    "zero max",
			modify:  func(c *ThroughputConfig) { c.MaxBlobsPerBlock = 0 },
			wantErr: true,
		},
		{
			name:    "min > max",
			modify:  func(c *ThroughputConfig) { c.MinBlobsPerBlock = 100; c.MaxBlobsPerBlock = 10 },
			wantErr: true,
		},
		{
			name:    "base < min",
			modify:  func(c *ThroughputConfig) { c.MinBlobsPerBlock = 10; c.BaseBlobsPerBlock = 5 },
			wantErr: true,
		},
		{
			name:    "base > max",
			modify:  func(c *ThroughputConfig) { c.BaseBlobsPerBlock = 100 },
			wantErr: true,
		},
		{
			name:    "scale up > 1",
			modify:  func(c *ThroughputConfig) { c.ScaleUpThreshold = 1.5 },
			wantErr: true,
		},
		{
			name:    "scale up = 0",
			modify:  func(c *ThroughputConfig) { c.ScaleUpThreshold = 0 },
			wantErr: true,
		},
		{
			name:    "scale down negative",
			modify:  func(c *ThroughputConfig) { c.ScaleDownThreshold = -0.1 },
			wantErr: true,
		},
		{
			name:    "scale down >= scale up",
			modify:  func(c *ThroughputConfig) { c.ScaleDownThreshold = 0.9; c.ScaleUpThreshold = 0.8 },
			wantErr: true,
		},
		{
			name:    "zero epochs",
			modify:  func(c *ThroughputConfig) { c.EpochsPerAdjustment = 0 },
			wantErr: true,
		},
		{
			name:    "zero slots per epoch",
			modify:  func(c *ThroughputConfig) { c.SlotsPerEpoch = 0 },
			wantErr: true,
		},
		{
			name:    "zero step size",
			modify:  func(c *ThroughputConfig) { c.StepSize = 0 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultThroughputConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewThroughputManagerInvalidConfig(t *testing.T) {
	cfg := ThroughputConfig{} // Invalid: all zeros.
	_, err := NewThroughputManager(cfg)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestThroughputManagerCurrentLimit(t *testing.T) {
	cfg := DefaultThroughputConfig()
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	if limit := tm.CurrentLimit(); limit != cfg.BaseBlobsPerBlock {
		t.Errorf("CurrentLimit = %d, want %d", limit, cfg.BaseBlobsPerBlock)
	}
}

func TestThroughputManagerRecordUtilization(t *testing.T) {
	cfg := DefaultThroughputConfig()
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// Record first slot.
	if err := tm.RecordUtilization(5, 100); err != nil {
		t.Fatalf("RecordUtilization: %v", err)
	}

	// Record second slot with higher number.
	if err := tm.RecordUtilization(3, 101); err != nil {
		t.Fatalf("RecordUtilization: %v", err)
	}

	// Non-monotonic slot should fail.
	if err := tm.RecordUtilization(2, 100); err == nil {
		t.Error("expected error for non-monotonic slot")
	}

	// Same slot number should fail.
	if err := tm.RecordUtilization(2, 101); err == nil {
		t.Error("expected error for repeated slot number")
	}
}

func TestThroughputManagerUtilizationRate(t *testing.T) {
	cfg := DefaultThroughputConfig()
	cfg.BaseBlobsPerBlock = 10
	cfg.MaxBlobsPerBlock = 20
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// No records.
	if rate := tm.UtilizationRate(); rate != 0 {
		t.Errorf("UtilizationRate (empty) = %f, want 0", rate)
	}

	// Record 4 slots with blob counts 10, 10, 5, 5 = 30 total.
	// Max possible = 4 * 10 = 40. Rate = 0.75.
	tm.RecordUtilization(10, 1)
	tm.RecordUtilization(10, 2)
	tm.RecordUtilization(5, 3)
	tm.RecordUtilization(5, 4)

	rate := tm.UtilizationRate()
	if rate < 0.74 || rate > 0.76 {
		t.Errorf("UtilizationRate = %f, want ~0.75", rate)
	}
}

func TestThroughputManagerUtilizationRateCapped(t *testing.T) {
	cfg := DefaultThroughputConfig()
	cfg.BaseBlobsPerBlock = 6
	cfg.MaxBlobsPerBlock = 32
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// Record blob count higher than current limit.
	// Should be capped to currentLimit (6).
	tm.RecordUtilization(100, 1)

	rate := tm.UtilizationRate()
	if rate < 0.99 || rate > 1.01 {
		t.Errorf("UtilizationRate (capped) = %f, want 1.0", rate)
	}
}

func TestThroughputManagerAdjustLimitScaleUp(t *testing.T) {
	cfg := ThroughputConfig{
		BaseBlobsPerBlock:   6,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 1,
		SlotsPerEpoch:       4,
		StepSize:            2,
	}
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// Fill the window (4 slots) with high utilization (100%).
	for i := uint64(0); i < 4; i++ {
		tm.RecordUtilization(6, i+1)
	}

	adjusted := tm.AdjustLimit()
	if !adjusted {
		t.Error("expected adjustment")
	}
	if limit := tm.CurrentLimit(); limit != 8 {
		t.Errorf("CurrentLimit = %d, want 8 (6 + step 2)", limit)
	}

	status := tm.Status()
	if status.ScaleUpCount != 1 {
		t.Errorf("ScaleUpCount = %d, want 1", status.ScaleUpCount)
	}
}

func TestThroughputManagerAdjustLimitScaleDown(t *testing.T) {
	cfg := ThroughputConfig{
		BaseBlobsPerBlock:   10,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 1,
		SlotsPerEpoch:       4,
		StepSize:            2,
	}
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// Fill the window with low utilization (0 blobs).
	for i := uint64(0); i < 4; i++ {
		tm.RecordUtilization(0, i+1)
	}

	adjusted := tm.AdjustLimit()
	if !adjusted {
		t.Error("expected adjustment")
	}
	if limit := tm.CurrentLimit(); limit != 8 {
		t.Errorf("CurrentLimit = %d, want 8 (10 - step 2)", limit)
	}

	status := tm.Status()
	if status.ScaleDownCount != 1 {
		t.Errorf("ScaleDownCount = %d, want 1", status.ScaleDownCount)
	}
}

func TestThroughputManagerAdjustLimitNotEnoughHistory(t *testing.T) {
	cfg := ThroughputConfig{
		BaseBlobsPerBlock:   6,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 2,
		SlotsPerEpoch:       4,
		StepSize:            1,
	}
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// Add only 4 records (window = 8).
	for i := uint64(0); i < 4; i++ {
		tm.RecordUtilization(6, i+1)
	}

	adjusted := tm.AdjustLimit()
	if adjusted {
		t.Error("should not adjust with insufficient history")
	}
}

func TestThroughputManagerAdjustLimitNoChange(t *testing.T) {
	cfg := ThroughputConfig{
		BaseBlobsPerBlock:   10,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 1,
		SlotsPerEpoch:       4,
		StepSize:            1,
	}
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// Fill with moderate utilization (50%).
	for i := uint64(0); i < 4; i++ {
		tm.RecordUtilization(5, i+1)
	}

	adjusted := tm.AdjustLimit()
	if adjusted {
		t.Error("should not adjust with moderate utilization")
	}
	if limit := tm.CurrentLimit(); limit != 10 {
		t.Errorf("CurrentLimit = %d, want 10", limit)
	}
}

func TestThroughputManagerAdjustLimitMaxCap(t *testing.T) {
	cfg := ThroughputConfig{
		BaseBlobsPerBlock:   31,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 1,
		SlotsPerEpoch:       4,
		StepSize:            5,
	}
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// High utilization to trigger scale-up.
	for i := uint64(0); i < 4; i++ {
		tm.RecordUtilization(31, i+1)
	}

	tm.AdjustLimit()
	if limit := tm.CurrentLimit(); limit != 32 {
		t.Errorf("CurrentLimit = %d, want 32 (capped at max)", limit)
	}
}

func TestThroughputManagerAdjustLimitMinFloor(t *testing.T) {
	cfg := ThroughputConfig{
		BaseBlobsPerBlock:   4,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 1,
		SlotsPerEpoch:       4,
		StepSize:            5,
	}
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// Zero utilization to trigger scale-down.
	for i := uint64(0); i < 4; i++ {
		tm.RecordUtilization(0, i+1)
	}

	tm.AdjustLimit()
	if limit := tm.CurrentLimit(); limit != 3 {
		t.Errorf("CurrentLimit = %d, want 3 (min floor)", limit)
	}
}

func TestThroughputManagerAdjustLimitAlreadyAtMax(t *testing.T) {
	cfg := ThroughputConfig{
		BaseBlobsPerBlock:   32,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 1,
		SlotsPerEpoch:       4,
		StepSize:            1,
	}
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// High utilization but already at max.
	for i := uint64(0); i < 4; i++ {
		tm.RecordUtilization(32, i+1)
	}

	adjusted := tm.AdjustLimit()
	if adjusted {
		t.Error("should not adjust when already at max")
	}
}

func TestThroughputManagerAdjustLimitAlreadyAtMin(t *testing.T) {
	cfg := ThroughputConfig{
		BaseBlobsPerBlock:   3,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 1,
		SlotsPerEpoch:       4,
		StepSize:            1,
	}
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// Low utilization but already at min.
	for i := uint64(0); i < 4; i++ {
		tm.RecordUtilization(0, i+1)
	}

	adjusted := tm.AdjustLimit()
	if adjusted {
		t.Error("should not adjust when already at min")
	}
}

func TestThroughputManagerHistoryCleared(t *testing.T) {
	cfg := ThroughputConfig{
		BaseBlobsPerBlock:   6,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 1,
		SlotsPerEpoch:       4,
		StepSize:            1,
	}
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// Fill window.
	for i := uint64(0); i < 4; i++ {
		tm.RecordUtilization(3, i+1)
	}

	tm.AdjustLimit()

	// History should be cleared after adjustment.
	status := tm.Status()
	if status.HistorySize != 0 {
		t.Errorf("HistorySize = %d, want 0 after adjustment", status.HistorySize)
	}
}

func TestThroughputManagerMultipleAdjustments(t *testing.T) {
	cfg := ThroughputConfig{
		BaseBlobsPerBlock:   6,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 1,
		SlotsPerEpoch:       4,
		StepSize:            1,
	}
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	slot := uint64(1)

	// First window: high utilization -> scale up from 6 to 7.
	for i := 0; i < 4; i++ {
		tm.RecordUtilization(6, slot)
		slot++
	}
	tm.AdjustLimit()
	if limit := tm.CurrentLimit(); limit != 7 {
		t.Fatalf("after first adjust: limit = %d, want 7", limit)
	}

	// Second window: high utilization -> scale up from 7 to 8.
	for i := 0; i < 4; i++ {
		tm.RecordUtilization(7, slot)
		slot++
	}
	tm.AdjustLimit()
	if limit := tm.CurrentLimit(); limit != 8 {
		t.Fatalf("after second adjust: limit = %d, want 8", limit)
	}

	// Third window: low utilization -> scale down from 8 to 7.
	for i := 0; i < 4; i++ {
		tm.RecordUtilization(0, slot)
		slot++
	}
	tm.AdjustLimit()
	if limit := tm.CurrentLimit(); limit != 7 {
		t.Fatalf("after third adjust: limit = %d, want 7", limit)
	}

	status := tm.Status()
	if status.AdjustmentCount != 3 {
		t.Errorf("AdjustmentCount = %d, want 3", status.AdjustmentCount)
	}
	if status.ScaleUpCount != 2 {
		t.Errorf("ScaleUpCount = %d, want 2", status.ScaleUpCount)
	}
	if status.ScaleDownCount != 1 {
		t.Errorf("ScaleDownCount = %d, want 1", status.ScaleDownCount)
	}
}

func TestThroughputManagerSetLimit(t *testing.T) {
	cfg := DefaultThroughputConfig()
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	// Set within range.
	tm.SetLimit(15)
	if limit := tm.CurrentLimit(); limit != 15 {
		t.Errorf("CurrentLimit = %d, want 15", limit)
	}

	// Set below min (should clamp).
	tm.SetLimit(1)
	if limit := tm.CurrentLimit(); limit != cfg.MinBlobsPerBlock {
		t.Errorf("CurrentLimit = %d, want %d (min)", limit, cfg.MinBlobsPerBlock)
	}

	// Set above max (should clamp).
	tm.SetLimit(1000)
	if limit := tm.CurrentLimit(); limit != cfg.MaxBlobsPerBlock {
		t.Errorf("CurrentLimit = %d, want %d (max)", limit, cfg.MaxBlobsPerBlock)
	}
}

func TestThroughputManagerReset(t *testing.T) {
	cfg := DefaultThroughputConfig()
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	tm.SetLimit(20)
	tm.RecordUtilization(5, 1)
	tm.RecordUtilization(5, 2)

	tm.Reset()

	if limit := tm.CurrentLimit(); limit != cfg.BaseBlobsPerBlock {
		t.Errorf("CurrentLimit after reset = %d, want %d", limit, cfg.BaseBlobsPerBlock)
	}
	status := tm.Status()
	if status.HistorySize != 0 {
		t.Errorf("HistorySize after reset = %d, want 0", status.HistorySize)
	}
	if status.AdjustmentCount != 0 {
		t.Errorf("AdjustmentCount after reset = %d, want 0", status.AdjustmentCount)
	}

	// Should be able to record from slot 1 again after reset.
	if err := tm.RecordUtilization(3, 1); err != nil {
		t.Errorf("RecordUtilization after reset: %v", err)
	}
}

func TestThroughputManagerConfig(t *testing.T) {
	cfg := DefaultThroughputConfig()
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	got := tm.Config()
	if got.BaseBlobsPerBlock != cfg.BaseBlobsPerBlock {
		t.Errorf("Config().BaseBlobsPerBlock = %d, want %d",
			got.BaseBlobsPerBlock, cfg.BaseBlobsPerBlock)
	}
	if got.MaxBlobsPerBlock != cfg.MaxBlobsPerBlock {
		t.Errorf("Config().MaxBlobsPerBlock = %d, want %d",
			got.MaxBlobsPerBlock, cfg.MaxBlobsPerBlock)
	}
}

func TestThroughputManagerConcurrentAccess(t *testing.T) {
	cfg := ThroughputConfig{
		BaseBlobsPerBlock:   10,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 1,
		SlotsPerEpoch:       32,
		StepSize:            1,
	}
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	var wg sync.WaitGroup

	// Concurrent reads.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = tm.CurrentLimit()
				_ = tm.UtilizationRate()
				_ = tm.Status()
			}
		}()
	}

	// Concurrent writes (sequential slot numbers per goroutine, non-overlapping).
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				slot := uint64(base*100 + j + 1)
				tm.RecordUtilization(5, slot)
			}
		}(g)
	}

	wg.Wait()
}

func TestThroughputManagerStatus(t *testing.T) {
	cfg := ThroughputConfig{
		BaseBlobsPerBlock:   6,
		MaxBlobsPerBlock:    32,
		MinBlobsPerBlock:    3,
		ScaleUpThreshold:    0.80,
		ScaleDownThreshold:  0.20,
		EpochsPerAdjustment: 1,
		SlotsPerEpoch:       4,
		StepSize:            1,
	}
	tm, err := NewThroughputManager(cfg)
	if err != nil {
		t.Fatalf("NewThroughputManager: %v", err)
	}

	tm.RecordUtilization(3, 1)
	tm.RecordUtilization(3, 2)

	status := tm.Status()
	if status.CurrentLimit != 6 {
		t.Errorf("CurrentLimit = %d, want 6", status.CurrentLimit)
	}
	if status.HistorySize != 2 {
		t.Errorf("HistorySize = %d, want 2", status.HistorySize)
	}
	if status.WindowSize != 4 {
		t.Errorf("WindowSize = %d, want 4", status.WindowSize)
	}
}
