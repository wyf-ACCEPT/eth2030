package das

import (
	"math"
	"sync"
	"testing"
)

// --- SampleOptimizerConfig defaults ---

func TestDefaultSampleOptimizerConfig(t *testing.T) {
	cfg := DefaultSampleOptimizerConfig()
	if cfg.MinSamples != SamplesPerSlot {
		t.Fatalf("MinSamples = %d, want %d", cfg.MinSamples, SamplesPerSlot)
	}
	if cfg.MaxSamples != int(NumberOfColumns) {
		t.Fatalf("MaxSamples = %d, want %d", cfg.MaxSamples, NumberOfColumns)
	}
	if cfg.TargetConfidence <= 0 || cfg.TargetConfidence > 1 {
		t.Fatalf("TargetConfidence = %f, want (0, 1]", cfg.TargetConfidence)
	}
	if cfg.SecurityMargin < 0 {
		t.Fatalf("SecurityMargin = %d, want >= 0", cfg.SecurityMargin)
	}
}

// --- NewSampleOptimizer ---

func TestNewSampleOptimizerDefaults(t *testing.T) {
	so := NewSampleOptimizer(SampleOptimizerConfig{})
	// All zero-value fields should be replaced with defaults.
	if so.config.MinSamples != SamplesPerSlot {
		t.Fatalf("MinSamples = %d, want %d", so.config.MinSamples, SamplesPerSlot)
	}
	if so.config.MaxSamples != int(NumberOfColumns) {
		t.Fatalf("MaxSamples = %d, want %d", so.config.MaxSamples, NumberOfColumns)
	}
}

func TestNewSampleOptimizerMaxLessThanMin(t *testing.T) {
	so := NewSampleOptimizer(SampleOptimizerConfig{
		MinSamples: 20,
		MaxSamples: 10, // less than min
	})
	if so.config.MaxSamples < so.config.MinSamples {
		t.Fatalf("MaxSamples %d < MinSamples %d after normalization",
			so.config.MaxSamples, so.config.MinSamples)
	}
}

func TestNewSampleOptimizerInvalidConfidence(t *testing.T) {
	so := NewSampleOptimizer(SampleOptimizerConfig{
		TargetConfidence: -1,
	})
	if so.config.TargetConfidence != 0.999 {
		t.Fatalf("TargetConfidence = %f, want 0.999 default", so.config.TargetConfidence)
	}

	so2 := NewSampleOptimizer(SampleOptimizerConfig{
		TargetConfidence: 1.5,
	})
	if so2.config.TargetConfidence != 0.999 {
		t.Fatalf("TargetConfidence = %f, want 0.999 default", so2.config.TargetConfidence)
	}
}

// --- CalculateOptimalSamples ---

func TestCalculateOptimalSamplesBasic(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	// With security param 10 bits, we need enough samples.
	samples := so.CalculateOptimalSamples(6, 10)
	if samples < so.config.MinSamples {
		t.Fatalf("samples %d < MinSamples %d", samples, so.config.MinSamples)
	}
	if samples > so.config.MaxSamples {
		t.Fatalf("samples %d > MaxSamples %d", samples, so.config.MaxSamples)
	}
}

func TestCalculateOptimalSamplesHighSecurity(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	low := so.CalculateOptimalSamples(6, 10)
	high := so.CalculateOptimalSamples(6, 80)
	if high < low {
		t.Fatalf("higher security param should need more samples: %d vs %d", high, low)
	}
}

func TestCalculateOptimalSamplesZeroInputs(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	// Zero blob count returns min.
	samples := so.CalculateOptimalSamples(0, 10)
	if samples != so.config.MinSamples {
		t.Fatalf("zero blobs: samples %d, want %d", samples, so.config.MinSamples)
	}

	// Zero security param returns min.
	samples = so.CalculateOptimalSamples(6, 0)
	if samples != so.config.MinSamples {
		t.Fatalf("zero security: samples %d, want %d", samples, so.config.MinSamples)
	}

	// Negative inputs return min.
	samples = so.CalculateOptimalSamples(-1, -1)
	if samples != so.config.MinSamples {
		t.Fatalf("negative inputs: samples %d, want %d", samples, so.config.MinSamples)
	}
}

func TestCalculateOptimalSamplesClampedToMax(t *testing.T) {
	so := NewSampleOptimizer(SampleOptimizerConfig{
		MinSamples:       4,
		MaxSamples:       16,
		TargetConfidence: 0.99,
	})

	// Very high security param should clamp to max.
	samples := so.CalculateOptimalSamples(6, 1000)
	if samples > so.config.MaxSamples {
		t.Fatalf("samples %d > MaxSamples %d", samples, so.config.MaxSamples)
	}
}

func TestCalculateOptimalSamplesMonotonic(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	// Increasing security param should never decrease sample count.
	prev := 0
	for sec := 1; sec <= 100; sec++ {
		samples := so.CalculateOptimalSamples(6, sec)
		if samples < prev {
			t.Fatalf("non-monotonic at sec=%d: %d < %d", sec, samples, prev)
		}
		prev = samples
	}
}

// --- AdaptiveSampling ---

func TestAdaptiveSamplingPerfectHealth(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	plan := so.AdaptiveSampling(6, 1.0)

	if plan.SamplesPerBlob < so.config.MinSamples {
		t.Fatalf("SamplesPerBlob %d < MinSamples %d", plan.SamplesPerBlob, so.config.MinSamples)
	}
	if plan.TotalSamples != plan.SamplesPerBlob*6 {
		t.Fatalf("TotalSamples %d != %d*6", plan.TotalSamples, plan.SamplesPerBlob)
	}
	if plan.ConfidenceLevel <= 0 || plan.ConfidenceLevel > 1 {
		t.Fatalf("ConfidenceLevel %f out of range", plan.ConfidenceLevel)
	}
	if plan.SecurityLevel <= 0 {
		t.Fatalf("SecurityLevel %d should be positive", plan.SecurityLevel)
	}
}

func TestAdaptiveSamplingDegradedNetwork(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	healthy := so.AdaptiveSampling(6, 1.0)
	degraded := so.AdaptiveSampling(6, 0.3)

	if degraded.SamplesPerBlob < healthy.SamplesPerBlob {
		t.Fatalf("degraded network should need more samples: %d vs %d",
			degraded.SamplesPerBlob, healthy.SamplesPerBlob)
	}
}

func TestAdaptiveSamplingZeroHealth(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	plan := so.AdaptiveSampling(6, 0.0)

	// At zero health, factor is 2x so samples should be at least double base.
	if plan.SamplesPerBlob < so.config.MinSamples {
		t.Fatalf("SamplesPerBlob %d < MinSamples %d", plan.SamplesPerBlob, so.config.MinSamples)
	}
}

func TestAdaptiveSamplingClampedHealth(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	// Negative health clamped to 0.
	planNeg := so.AdaptiveSampling(6, -0.5)
	planZero := so.AdaptiveSampling(6, 0.0)
	if planNeg.SamplesPerBlob != planZero.SamplesPerBlob {
		t.Fatalf("negative health should clamp to 0: %d vs %d",
			planNeg.SamplesPerBlob, planZero.SamplesPerBlob)
	}

	// Health > 1 clamped to 1.
	planOver := so.AdaptiveSampling(6, 1.5)
	planOne := so.AdaptiveSampling(6, 1.0)
	if planOver.SamplesPerBlob != planOne.SamplesPerBlob {
		t.Fatalf("health > 1 should clamp to 1: %d vs %d",
			planOver.SamplesPerBlob, planOne.SamplesPerBlob)
	}
}

func TestAdaptiveSamplingZeroBlobCount(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	plan := so.AdaptiveSampling(0, 1.0)

	// Zero blob count is treated as 1.
	if plan.TotalSamples != plan.SamplesPerBlob {
		t.Fatalf("zero blobs: TotalSamples %d != SamplesPerBlob %d",
			plan.TotalSamples, plan.SamplesPerBlob)
	}
}

// --- ValidateSamplingResult ---

func TestValidateSamplingResultSufficient(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	plan := so.AdaptiveSampling(3, 1.0)

	verdict := so.ValidateSamplingResult(plan, plan.TotalSamples)
	if !verdict.Sufficient {
		t.Fatal("should be sufficient when all samples received")
	}
	if verdict.MissingSamples != 0 {
		t.Fatalf("MissingSamples %d, want 0", verdict.MissingSamples)
	}
	if verdict.Confidence <= 0 {
		t.Fatalf("Confidence %f should be positive", verdict.Confidence)
	}
}

func TestValidateSamplingResultInsufficient(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	plan := so.AdaptiveSampling(3, 1.0)

	verdict := so.ValidateSamplingResult(plan, plan.TotalSamples/2)
	if verdict.Sufficient {
		t.Fatal("should be insufficient when half received")
	}
	if verdict.MissingSamples <= 0 {
		t.Fatal("MissingSamples should be positive")
	}
}

func TestValidateSamplingResultExcessSamples(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	plan := so.AdaptiveSampling(3, 1.0)

	// More samples than needed.
	verdict := so.ValidateSamplingResult(plan, plan.TotalSamples+100)
	if !verdict.Sufficient {
		t.Fatal("excess samples should still be sufficient")
	}
	if verdict.MissingSamples != 0 {
		t.Fatalf("MissingSamples %d, want 0 with excess", verdict.MissingSamples)
	}
}

func TestValidateSamplingResultNilPlan(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	verdict := so.ValidateSamplingResult(nil, 10)
	if verdict.Sufficient {
		t.Fatal("nil plan should not be sufficient")
	}
}

func TestValidateSamplingResultNegativeSamples(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	plan := &SamplingPlan{TotalSamples: 10}
	verdict := so.ValidateSamplingResult(plan, -5)
	if verdict.Sufficient {
		t.Fatal("negative received should not be sufficient")
	}
	if verdict.MissingSamples != 10 {
		t.Fatalf("MissingSamples %d, want 10", verdict.MissingSamples)
	}
}

// --- AdjustSampleSize ---

func TestAdjustSampleSizeHighFailure(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	current := 16
	adjusted := so.AdjustSampleSize(current, 0.5)
	if adjusted <= current {
		t.Fatalf("high failure rate should increase: %d -> %d", current, adjusted)
	}
}

func TestAdjustSampleSizeLowFailure(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	current := 32
	adjusted := so.AdjustSampleSize(current, 0.01)
	if adjusted >= current {
		t.Fatalf("low failure rate should decrease: %d -> %d", current, adjusted)
	}
}

func TestAdjustSampleSizeStableRange(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	current := 16
	adjusted := so.AdjustSampleSize(current, 0.1)
	if adjusted != current {
		t.Fatalf("moderate failure should keep size stable: %d -> %d", current, adjusted)
	}
}

func TestAdjustSampleSizeClamp(t *testing.T) {
	so := NewSampleOptimizer(SampleOptimizerConfig{
		MinSamples:       8,
		MaxSamples:       32,
		TargetConfidence: 0.99,
	})

	// Low failure with small current: should clamp to min.
	adjusted := so.AdjustSampleSize(9, 0.01)
	if adjusted < 8 {
		t.Fatalf("adjusted %d below min 8", adjusted)
	}

	// High failure with large current: should clamp to max.
	adjusted = so.AdjustSampleSize(30, 1.0)
	if adjusted > 32 {
		t.Fatalf("adjusted %d above max 32", adjusted)
	}
}

func TestAdjustSampleSizeZeroCurrent(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	adjusted := so.AdjustSampleSize(0, 0.1)
	if adjusted < so.config.MinSamples {
		t.Fatalf("zero current: adjusted %d < MinSamples %d", adjusted, so.config.MinSamples)
	}
}

func TestAdjustSampleSizeBoundaryFailureRates(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	// Failure rate clamped to 0.
	a := so.AdjustSampleSize(16, -0.5)
	b := so.AdjustSampleSize(16, 0.0)
	if a != b {
		t.Fatalf("negative failure rate not clamped: %d vs %d", a, b)
	}

	// Failure rate clamped to 1.
	c := so.AdjustSampleSize(16, 1.5)
	d := so.AdjustSampleSize(16, 1.0)
	if c != d {
		t.Fatalf("failure rate > 1 not clamped: %d vs %d", c, d)
	}
}

// --- EstimateNetworkLoad ---

func TestEstimateNetworkLoadBasic(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	load := so.EstimateNetworkLoad(6, 16)
	expectedPerSample := uint64(BytesPerCell + 48)
	expected := 6 * 16 * expectedPerSample
	if load != expected {
		t.Fatalf("load = %d, want %d", load, expected)
	}
}

func TestEstimateNetworkLoadZeroInputs(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	if so.EstimateNetworkLoad(0, 16) != 0 {
		t.Fatal("zero blobs should give zero load")
	}
	if so.EstimateNetworkLoad(6, 0) != 0 {
		t.Fatal("zero samples should give zero load")
	}
	if so.EstimateNetworkLoad(-1, 16) != 0 {
		t.Fatal("negative blobs should give zero load")
	}
}

func TestEstimateNetworkLoadScales(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	load1 := so.EstimateNetworkLoad(1, 8)
	load2 := so.EstimateNetworkLoad(2, 8)
	if load2 != 2*load1 {
		t.Fatalf("doubling blobs should double load: %d vs %d", load2, load1)
	}

	load3 := so.EstimateNetworkLoad(1, 16)
	if load3 != 2*load1 {
		t.Fatalf("doubling samples should double load: %d vs %d", load3, load1)
	}
}

// --- Confidence math sanity ---

func TestConfidenceMath(t *testing.T) {
	// With k samples from N=128 columns, confidence = 1 - ((N-1)/N)^k.
	n := float64(NumberOfColumns)
	for _, k := range []int{8, 16, 32, 64, 128} {
		conf := 1.0 - math.Pow((n-1)/n, float64(k))
		if conf <= 0 || conf > 1 {
			t.Fatalf("k=%d: confidence %f out of range", k, conf)
		}
	}

	// More samples should give higher confidence.
	conf8 := 1.0 - math.Pow((n-1)/n, 8)
	conf64 := 1.0 - math.Pow((n-1)/n, 64)
	if conf64 <= conf8 {
		t.Fatalf("64 samples should give higher confidence: %f vs %f", conf64, conf8)
	}
}

// --- Concurrency ---

func TestSampleOptimizerConcurrency(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = so.CalculateOptimalSamples(6, idx%50+1)
			plan := so.AdaptiveSampling(idx%9+1, float64(idx%100)/100.0)
			_ = so.ValidateSamplingResult(plan, plan.TotalSamples/2)
			_ = so.AdjustSampleSize(idx%64+1, float64(idx%100)/100.0)
			_ = so.EstimateNetworkLoad(idx%9+1, idx%64+1)
		}(i)
	}
	wg.Wait()
}

// --- ValidateSamplingPlan ---

func TestValidateSamplingPlanValid(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	plan := so.AdaptiveSampling(6, 1.0)

	if err := so.ValidateSamplingPlan(plan); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
}

func TestValidateSamplingPlanNil(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	if err := so.ValidateSamplingPlan(nil); err == nil {
		t.Fatal("nil plan should fail")
	}
}

func TestValidateSamplingPlanBelowMin(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	plan := &SamplingPlan{
		SamplesPerBlob:  1, // below MinSamples (8)
		TotalSamples:    6,
		SecurityLevel:   5,
		ConfidenceLevel: 0.9,
	}
	if err := so.ValidateSamplingPlan(plan); err == nil {
		t.Fatal("plan below min samples should fail")
	}
}

func TestValidateSamplingPlanAboveMax(t *testing.T) {
	so := NewSampleOptimizer(SampleOptimizerConfig{
		MinSamples:       4,
		MaxSamples:       16,
		TargetConfidence: 0.99,
	})
	plan := &SamplingPlan{
		SamplesPerBlob:  20, // above MaxSamples (16)
		TotalSamples:    20,
		SecurityLevel:   10,
		ConfidenceLevel: 0.95,
	}
	if err := so.ValidateSamplingPlan(plan); err == nil {
		t.Fatal("plan above max samples should fail")
	}
}

func TestValidateSamplingPlanBadConfidence(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())
	plan := &SamplingPlan{
		SamplesPerBlob:  SamplesPerSlot,
		TotalSamples:    SamplesPerSlot,
		SecurityLevel:   5,
		ConfidenceLevel: 1.5, // invalid
	}
	if err := so.ValidateSamplingPlan(plan); err == nil {
		t.Fatal("confidence > 1 should fail")
	}
}

func TestValidateSamplingPlanSecurityMonotonic(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	// Higher sample count should produce higher security level.
	plan1 := so.AdaptiveSampling(6, 1.0)
	plan2 := so.AdaptiveSampling(6, 0.0) // worse health -> more samples

	if plan2.SamplesPerBlob > plan1.SamplesPerBlob && plan2.SecurityLevel < plan1.SecurityLevel {
		t.Fatal("more samples should not decrease security level")
	}

	// Both should validate.
	if err := so.ValidateSamplingPlan(plan1); err != nil {
		t.Fatalf("plan1 rejected: %v", err)
	}
	if err := so.ValidateSamplingPlan(plan2); err != nil {
		t.Fatalf("plan2 rejected: %v", err)
	}
}

func TestKnownSecurityValues(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	// With 128 columns, k=10 samples gives confidence ~1-((127/128)^10) ~= 0.0754.
	// Security bits ~= -log2(1-0.0754) ~= 0.113 bits. With margin, samples should
	// be higher than 10 for useful security.
	plan := so.AdaptiveSampling(1, 1.0)
	if plan.SecurityLevel < 1 {
		t.Fatalf("plan with default config should achieve at least 1-bit security, got %d", plan.SecurityLevel)
	}

	// Confidence with N=128 and k=8 (minimum).
	n := float64(NumberOfColumns)
	minConf := 1.0 - math.Pow((n-1)/n, float64(SamplesPerSlot))
	if minConf <= 0 {
		t.Fatal("minimum confidence should be positive")
	}
}

// --- Integration: full adaptive flow ---

func TestAdaptiveSamplingFullFlow(t *testing.T) {
	so := NewSampleOptimizer(DefaultSampleOptimizerConfig())

	// Simulate a slot with 6 blobs and healthy network.
	plan := so.AdaptiveSampling(6, 0.95)
	t.Logf("plan: %d samples/blob, %d total, confidence=%.4f, security=%d bits",
		plan.SamplesPerBlob, plan.TotalSamples, plan.ConfidenceLevel, plan.SecurityLevel)

	// Simulate receiving all samples.
	verdict := so.ValidateSamplingResult(plan, plan.TotalSamples)
	if !verdict.Sufficient {
		t.Fatal("full receipt should be sufficient")
	}

	// Estimate load.
	load := so.EstimateNetworkLoad(6, plan.SamplesPerBlob)
	if load == 0 {
		t.Fatal("load should be non-zero")
	}
	t.Logf("network load: %d bytes", load)

	// Simulate some failures and adjust.
	adjusted := so.AdjustSampleSize(plan.SamplesPerBlob, 0.3)
	if adjusted < plan.SamplesPerBlob {
		t.Fatal("30% failure should increase sample size")
	}
	t.Logf("adjusted sample size: %d -> %d", plan.SamplesPerBlob, adjusted)
}
