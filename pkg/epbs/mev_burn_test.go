package epbs

import (
	"errors"
	"math"
	"testing"
)

// --- MEVBurnConfig tests ---

func TestDefaultMEVBurnConfig(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	if cfg.BurnFraction != 0.50 {
		t.Errorf("BurnFraction = %f, want 0.50", cfg.BurnFraction)
	}
	if cfg.SmoothingFactor != 0.10 {
		t.Errorf("SmoothingFactor = %f, want 0.10", cfg.SmoothingFactor)
	}
	if cfg.MinBurnThreshold != 100 {
		t.Errorf("MinBurnThreshold = %d, want 100", cfg.MinBurnThreshold)
	}
	if cfg.Tolerance != 0.01 {
		t.Errorf("Tolerance = %f, want 0.01", cfg.Tolerance)
	}
}

func TestValidateMEVBurnConfigValid(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	if err := ValidateMEVBurnConfig(cfg); err != nil {
		t.Errorf("valid config: %v", err)
	}
}

func TestValidateMEVBurnConfigInvalidFraction(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	cfg.BurnFraction = 1.5
	err := ValidateMEVBurnConfig(cfg)
	if !errors.Is(err, ErrMEVBurnInvalidFraction) {
		t.Errorf("expected ErrMEVBurnInvalidFraction, got %v", err)
	}

	cfg.BurnFraction = -0.1
	err = ValidateMEVBurnConfig(cfg)
	if !errors.Is(err, ErrMEVBurnInvalidFraction) {
		t.Errorf("expected ErrMEVBurnInvalidFraction for negative, got %v", err)
	}
}

func TestValidateMEVBurnConfigInvalidSmoothing(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	cfg.SmoothingFactor = 0.0
	err := ValidateMEVBurnConfig(cfg)
	if !errors.Is(err, ErrMEVBurnInvalidSmoothing) {
		t.Errorf("expected ErrMEVBurnInvalidSmoothing, got %v", err)
	}

	cfg.SmoothingFactor = 1.5
	err = ValidateMEVBurnConfig(cfg)
	if !errors.Is(err, ErrMEVBurnInvalidSmoothing) {
		t.Errorf("expected ErrMEVBurnInvalidSmoothing for >1, got %v", err)
	}
}

func TestValidateMEVBurnConfigInvalidTolerance(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	cfg.Tolerance = -0.5
	err := ValidateMEVBurnConfig(cfg)
	if !errors.Is(err, ErrMEVBurnInvalidTolerance) {
		t.Errorf("expected ErrMEVBurnInvalidTolerance, got %v", err)
	}
}

// --- ComputeMEVBurn tests ---

func TestComputeMEVBurnStandard(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	result := ComputeMEVBurn(10000, cfg)

	if result.BidValue != 10000 {
		t.Errorf("BidValue = %d, want 10000", result.BidValue)
	}
	// 50% of 10000 = 5000
	if result.BurnAmount != 5000 {
		t.Errorf("BurnAmount = %d, want 5000", result.BurnAmount)
	}
	if result.ProposerPayment != 5000 {
		t.Errorf("ProposerPayment = %d, want 5000", result.ProposerPayment)
	}
}

func TestComputeMEVBurnSumsCorrectly(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	result := ComputeMEVBurn(12345, cfg)
	if result.BurnAmount+result.ProposerPayment != result.BidValue {
		t.Errorf("burn + proposer != bid: %d + %d != %d",
			result.BurnAmount, result.ProposerPayment, result.BidValue)
	}
}

func TestComputeMEVBurnBelowThreshold(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	cfg.MinBurnThreshold = 500
	result := ComputeMEVBurn(100, cfg)

	if result.BurnAmount != 0 {
		t.Errorf("BurnAmount below threshold = %d, want 0", result.BurnAmount)
	}
	if result.ProposerPayment != 100 {
		t.Errorf("ProposerPayment below threshold = %d, want 100", result.ProposerPayment)
	}
}

func TestComputeMEVBurnZeroFraction(t *testing.T) {
	cfg := MEVBurnConfig{BurnFraction: 0.0, MinBurnThreshold: 0}
	result := ComputeMEVBurn(10000, cfg)

	if result.BurnAmount != 0 {
		t.Errorf("BurnAmount with zero fraction = %d, want 0", result.BurnAmount)
	}
	if result.ProposerPayment != 10000 {
		t.Errorf("ProposerPayment with zero fraction = %d, want 10000", result.ProposerPayment)
	}
}

func TestComputeMEVBurnFullBurn(t *testing.T) {
	cfg := MEVBurnConfig{BurnFraction: 1.0, MinBurnThreshold: 0, SmoothingFactor: 0.1, Tolerance: 0.01}
	result := ComputeMEVBurn(10000, cfg)

	if result.BurnAmount != 10000 {
		t.Errorf("BurnAmount full burn = %d, want 10000", result.BurnAmount)
	}
	if result.ProposerPayment != 0 {
		t.Errorf("ProposerPayment full burn = %d, want 0", result.ProposerPayment)
	}
}

func TestComputeMEVBurnZeroBid(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	cfg.MinBurnThreshold = 0
	result := ComputeMEVBurn(0, cfg)

	if result.BurnAmount != 0 {
		t.Errorf("BurnAmount zero bid = %d, want 0", result.BurnAmount)
	}
}

// --- EpochBurnStats tests ---

func TestEpochBurnStatsBurnRate(t *testing.T) {
	stats := &EpochBurnStats{
		Epoch:         1,
		TotalBurned:   5000,
		TotalBidValue: 10000,
		BidCount:      2,
	}
	rate := stats.BurnRate()
	if rate != 0.5 {
		t.Errorf("burn rate = %f, want 0.5", rate)
	}
}

func TestEpochBurnStatsBurnRateZeroBids(t *testing.T) {
	stats := &EpochBurnStats{}
	rate := stats.BurnRate()
	if rate != 0.0 {
		t.Errorf("burn rate with zero bids = %f, want 0.0", rate)
	}
}

// --- MEVBurnTracker tests ---

func TestMEVBurnTrackerRecordBurn(t *testing.T) {
	tracker := NewMEVBurnTracker(DefaultMEVBurnConfig())
	result := MEVBurnResult{BidValue: 10000, BurnAmount: 5000, ProposerPayment: 5000}

	tracker.RecordBurn(1, result)

	if tracker.TotalBurned() != 5000 {
		t.Errorf("total burned = %d, want 5000", tracker.TotalBurned())
	}
	if tracker.TotalBids() != 1 {
		t.Errorf("total bids = %d, want 1", tracker.TotalBids())
	}
}

func TestMEVBurnTrackerMultipleEpochs(t *testing.T) {
	tracker := NewMEVBurnTracker(DefaultMEVBurnConfig())

	tracker.RecordBurn(1, MEVBurnResult{BidValue: 10000, BurnAmount: 5000, ProposerPayment: 5000})
	tracker.RecordBurn(1, MEVBurnResult{BidValue: 8000, BurnAmount: 4000, ProposerPayment: 4000})
	tracker.RecordBurn(2, MEVBurnResult{BidValue: 6000, BurnAmount: 3000, ProposerPayment: 3000})

	stats1 := tracker.GetEpochStats(1)
	if stats1 == nil {
		t.Fatal("epoch 1 stats nil")
	}
	if stats1.TotalBurned != 9000 {
		t.Errorf("epoch 1 burned = %d, want 9000", stats1.TotalBurned)
	}
	if stats1.BidCount != 2 {
		t.Errorf("epoch 1 bids = %d, want 2", stats1.BidCount)
	}

	stats2 := tracker.GetEpochStats(2)
	if stats2 == nil {
		t.Fatal("epoch 2 stats nil")
	}
	if stats2.TotalBurned != 3000 {
		t.Errorf("epoch 2 burned = %d, want 3000", stats2.TotalBurned)
	}

	if tracker.TotalBurned() != 12000 {
		t.Errorf("total burned = %d, want 12000", tracker.TotalBurned())
	}
}

func TestMEVBurnTrackerEMA(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	cfg.SmoothingFactor = 0.5 // 50% weight to make math easy
	tracker := NewMEVBurnTracker(cfg)

	// First bid: EMA = 10000
	tracker.RecordBurn(1, MEVBurnResult{BidValue: 10000, BurnAmount: 5000, ProposerPayment: 5000})
	ema := tracker.EMA()
	if ema != 10000 {
		t.Errorf("EMA after 1st bid = %f, want 10000", ema)
	}

	// Second bid: EMA = 0.5 * 6000 + 0.5 * 10000 = 8000
	tracker.RecordBurn(1, MEVBurnResult{BidValue: 6000, BurnAmount: 3000, ProposerPayment: 3000})
	ema = tracker.EMA()
	if ema != 8000 {
		t.Errorf("EMA after 2nd bid = %f, want 8000", ema)
	}
}

func TestMEVBurnTrackerGetEpochStatsNil(t *testing.T) {
	tracker := NewMEVBurnTracker(DefaultMEVBurnConfig())
	stats := tracker.GetEpochStats(999)
	if stats != nil {
		t.Error("expected nil stats for nonexistent epoch")
	}
}

func TestMEVBurnTrackerPrune(t *testing.T) {
	tracker := NewMEVBurnTracker(DefaultMEVBurnConfig())

	for epoch := uint64(1); epoch <= 10; epoch++ {
		tracker.RecordBurn(epoch, MEVBurnResult{BidValue: 1000, BurnAmount: 500, ProposerPayment: 500})
	}

	pruned := tracker.PruneEpochsBefore(5)
	if pruned != 4 {
		t.Errorf("pruned = %d, want 4", pruned)
	}
	if tracker.EpochCount() != 6 {
		t.Errorf("epoch count = %d, want 6", tracker.EpochCount())
	}

	// Epochs 1-4 should be gone.
	for epoch := uint64(1); epoch < 5; epoch++ {
		if tracker.GetEpochStats(epoch) != nil {
			t.Errorf("epoch %d should be pruned", epoch)
		}
	}
	// Epochs 5-10 should remain.
	for epoch := uint64(5); epoch <= 10; epoch++ {
		if tracker.GetEpochStats(epoch) == nil {
			t.Errorf("epoch %d should remain", epoch)
		}
	}
}

// --- EstimateSmoothedBurn tests ---

func TestEstimateSmoothedBurnSingleBid(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	ema, burn, err := EstimateSmoothedBurn([]uint64{10000}, cfg)
	if err != nil {
		t.Fatalf("EstimateSmoothedBurn: %v", err)
	}
	if ema != 10000 {
		t.Errorf("EMA = %f, want 10000", ema)
	}
	if burn != 5000 {
		t.Errorf("burn estimate = %d, want 5000", burn)
	}
}

func TestEstimateSmoothedBurnMultipleBids(t *testing.T) {
	cfg := MEVBurnConfig{
		BurnFraction:    0.50,
		SmoothingFactor: 0.50,
		Tolerance:       0.01,
	}
	bids := []uint64{10000, 6000}
	ema, _, err := EstimateSmoothedBurn(bids, cfg)
	if err != nil {
		t.Fatalf("EstimateSmoothedBurn: %v", err)
	}
	// EMA: start=10000, then 0.5*6000+0.5*10000=8000
	if ema != 8000 {
		t.Errorf("EMA = %f, want 8000", ema)
	}
}

func TestEstimateSmoothedBurnEmptyBids(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	_, _, err := EstimateSmoothedBurn(nil, cfg)
	if !errors.Is(err, ErrMEVBurnNoBids) {
		t.Errorf("expected ErrMEVBurnNoBids, got %v", err)
	}
}

func TestEstimateSmoothedBurnConvergence(t *testing.T) {
	cfg := MEVBurnConfig{
		BurnFraction:    0.50,
		SmoothingFactor: 0.20,
		Tolerance:       0.01,
	}
	// All bids are 5000: EMA should converge to 5000.
	bids := make([]uint64, 100)
	for i := range bids {
		bids[i] = 5000
	}
	ema, _, err := EstimateSmoothedBurn(bids, cfg)
	if err != nil {
		t.Fatalf("EstimateSmoothedBurn: %v", err)
	}
	if math.Abs(ema-5000) > 1 {
		t.Errorf("EMA should converge to 5000, got %f", ema)
	}
}

// --- ValidateBurnAmount tests ---

func TestValidateBurnAmountExact(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	// 50% of 10000 = 5000
	err := ValidateBurnAmount(5000, 10000, cfg)
	if err != nil {
		t.Errorf("exact burn amount: %v", err)
	}
}

func TestValidateBurnAmountWithinTolerance(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	cfg.Tolerance = 0.02 // 2% tolerance
	// 50% of 10000 = 5000, claim 5050 (1% off)
	err := ValidateBurnAmount(5050, 10000, cfg)
	if err != nil {
		t.Errorf("within tolerance: %v", err)
	}
}

func TestValidateBurnAmountExceedsTolerance(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	cfg.Tolerance = 0.01 // 1% tolerance
	// 50% of 10000 = 5000, claim 6000 (20% off)
	err := ValidateBurnAmount(6000, 10000, cfg)
	if !errors.Is(err, ErrMEVBurnValidationFailed) {
		t.Errorf("expected ErrMEVBurnValidationFailed, got %v", err)
	}
}

func TestValidateBurnAmountBothZero(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	cfg.BurnFraction = 0.0
	err := ValidateBurnAmount(0, 10000, cfg)
	if err != nil {
		t.Errorf("both zero: %v", err)
	}
}

func TestValidateBurnAmountClaimedNonZeroComputedZero(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	cfg.BurnFraction = 0.0
	err := ValidateBurnAmount(100, 10000, cfg)
	if !errors.Is(err, ErrMEVBurnValidationFailed) {
		t.Errorf("expected ErrMEVBurnValidationFailed, got %v", err)
	}
}

func TestValidateBurnAmountBelowThreshold(t *testing.T) {
	cfg := DefaultMEVBurnConfig()
	cfg.MinBurnThreshold = 500
	// Bid value 100 is below threshold, so computed burn = 0, claimed = 0.
	err := ValidateBurnAmount(0, 100, cfg)
	if err != nil {
		t.Errorf("below threshold: %v", err)
	}
}
