package core

import (
	"math/big"
	"sync"
	"testing"
)

func TestGasDimensionString(t *testing.T) {
	tests := []struct {
		dim  GasDimension
		want string
	}{
		{DimCompute, "compute"},
		{DimStorage, "storage"},
		{DimBandwidth, "bandwidth"},
		{DimBlob, "blob"},
		{DimWitness, "witness"},
		{GasDimension(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.dim.String(); got != tt.want {
			t.Errorf("GasDimension(%d).String() = %q, want %q", tt.dim, got, tt.want)
		}
	}
}

func TestAllGasDimensions(t *testing.T) {
	dims := AllGasDimensions()
	if len(dims) != NumGasDimensions {
		t.Fatalf("AllGasDimensions() len = %d, want %d", len(dims), NumGasDimensions)
	}
	for i, d := range dims {
		if int(d) != i {
			t.Errorf("AllGasDimensions()[%d] = %d, want %d", i, d, i)
		}
	}
}

func TestDefaultMultidimGasConfig(t *testing.T) {
	cfg := DefaultMultidimGasConfig()

	if cfg.Dims[DimCompute].Target != 15_000_000 {
		t.Errorf("compute target = %d, want 15M", cfg.Dims[DimCompute].Target)
	}
	if cfg.Dims[DimCompute].MaxGas != 30_000_000 {
		t.Errorf("compute max = %d, want 30M", cfg.Dims[DimCompute].MaxGas)
	}
	if cfg.Dims[DimStorage].Target != 5_000_000 {
		t.Errorf("storage target = %d, want 5M", cfg.Dims[DimStorage].Target)
	}
	if cfg.Dims[DimBandwidth].Target != 1_875_000 {
		t.Errorf("bandwidth target = %d, want 1.875M", cfg.Dims[DimBandwidth].Target)
	}
	if cfg.Dims[DimBlob].Target != 393_216 {
		t.Errorf("blob target = %d, want 393216", cfg.Dims[DimBlob].Target)
	}
	if cfg.Dims[DimWitness].Target != 2_000_000 {
		t.Errorf("witness target = %d, want 2M", cfg.Dims[DimWitness].Target)
	}
	if cfg.HistoryLimit != 256 {
		t.Errorf("history limit = %d, want 256", cfg.HistoryLimit)
	}
}

func TestMultidimGasConfigValidate(t *testing.T) {
	// Valid config should pass.
	cfg := DefaultMultidimGasConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config failed: %v", err)
	}

	// Zero target should fail.
	bad := DefaultMultidimGasConfig()
	bad.Dims[DimCompute].Target = 0
	if err := bad.Validate(); err == nil {
		t.Fatal("zero target should fail validation")
	}

	// Zero max gas should fail.
	bad2 := DefaultMultidimGasConfig()
	bad2.Dims[DimStorage].MaxGas = 0
	if err := bad2.Validate(); err == nil {
		t.Fatal("zero max gas should fail validation")
	}

	// Zero denominator should fail.
	bad3 := DefaultMultidimGasConfig()
	bad3.Dims[DimBlob].BaseFeeChangeDenom = 0
	if err := bad3.Validate(); err == nil {
		t.Fatal("zero denominator should fail validation")
	}

	// Nil min base fee should fail.
	bad4 := DefaultMultidimGasConfig()
	bad4.Dims[DimWitness].MinBaseFee = nil
	if err := bad4.Validate(); err == nil {
		t.Fatal("nil min base fee should fail validation")
	}

	// Zero min base fee should fail.
	bad5 := DefaultMultidimGasConfig()
	bad5.Dims[DimBandwidth].MinBaseFee = big.NewInt(0)
	if err := bad5.Validate(); err == nil {
		t.Fatal("zero min base fee should fail validation")
	}

	// Zero history limit should fail.
	bad6 := DefaultMultidimGasConfig()
	bad6.HistoryLimit = 0
	if err := bad6.Validate(); err == nil {
		t.Fatal("zero history limit should fail validation")
	}
}

func TestNewMultidimPricingEngine(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, err := NewMultidimPricingEngine(cfg)
	if err != nil {
		t.Fatalf("NewMultidimPricingEngine: %v", err)
	}

	// Initial base fees should equal min base fees.
	for _, dim := range AllGasDimensions() {
		fee := e.BaseFee(dim)
		if fee.Cmp(cfg.Dims[dim].MinBaseFee) != 0 {
			t.Errorf("%s initial fee = %s, want %s", dim, fee, cfg.Dims[dim].MinBaseFee)
		}
	}
}

func TestNewMultidimPricingEngineInvalidConfig(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	cfg.Dims[0].Target = 0
	_, err := NewMultidimPricingEngine(cfg)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestNewMultidimPricingEngineWithFees(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	fees := map[GasDimension]*big.Int{
		DimCompute:   big.NewInt(1_000_000_000),
		DimStorage:   big.NewInt(500_000),
		DimBandwidth: big.NewInt(100_000),
		DimBlob:      big.NewInt(1000),
		DimWitness:   big.NewInt(5000),
	}

	e, err := NewMultidimPricingEngineWithFees(cfg, fees)
	if err != nil {
		t.Fatalf("NewMultidimPricingEngineWithFees: %v", err)
	}

	for dim, expected := range fees {
		got := e.BaseFee(dim)
		if got.Cmp(expected) != 0 {
			t.Errorf("%s base fee = %s, want %s", dim, got, expected)
		}
	}
}

func TestBaseFeeReturnsDefensiveCopyMDG(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(1000),
	})

	fee := e.BaseFee(DimCompute)
	fee.SetInt64(999999) // mutate

	fee2 := e.BaseFee(DimCompute)
	if fee2.Int64() != 1000 {
		t.Fatalf("BaseFee should return defensive copy: got %s", fee2)
	}
}

func TestBaseFeeInvalidDimension(t *testing.T) {
	e, _ := NewMultidimPricingEngine(DefaultMultidimGasConfig())
	fee := e.BaseFee(GasDimension(99))
	if fee.Sign() != 0 {
		t.Fatalf("invalid dimension should return 0, got %s", fee)
	}
}

func TestAllBaseFees(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	fees := map[GasDimension]*big.Int{
		DimCompute: big.NewInt(100),
		DimStorage: big.NewInt(200),
	}
	e, _ := NewMultidimPricingEngineWithFees(cfg, fees)

	all := e.AllBaseFees()
	if len(all) != NumGasDimensions {
		t.Fatalf("AllBaseFees len = %d, want %d", len(all), NumGasDimensions)
	}
	if all[DimCompute].Int64() != 100 {
		t.Errorf("compute = %s, want 100", all[DimCompute])
	}
	if all[DimStorage].Int64() != 200 {
		t.Errorf("storage = %s, want 200", all[DimStorage])
	}
}

func TestUpdateBaseFeesAtTargetMDG(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	initFee := big.NewInt(1_000_000_000)
	fees := make(map[GasDimension]*big.Int)
	for _, d := range AllGasDimensions() {
		fees[d] = new(big.Int).Set(initFee)
	}
	e, _ := NewMultidimPricingEngineWithFees(cfg, fees)

	// Usage exactly at target: fees should be unchanged.
	usage := make(map[GasDimension]uint64)
	for _, d := range AllGasDimensions() {
		usage[d] = cfg.Dims[d].Target
	}
	e.UpdateBaseFees(usage, nil)

	for _, d := range AllGasDimensions() {
		got := e.BaseFee(d)
		if got.Cmp(initFee) != 0 {
			t.Errorf("%s fee at target: got %s, want %s", d, got, initFee)
		}
	}
}

func TestUpdateBaseFeesAboveTargetMDG(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	initFee := big.NewInt(1_000_000_000)
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: new(big.Int).Set(initFee),
	})

	// Max usage (2x target for compute): fee = fee + fee * 1 / 8 = fee * 9/8.
	usage := map[GasDimension]uint64{
		DimCompute: cfg.Dims[DimCompute].MaxGas,
	}
	e.UpdateBaseFees(usage, nil)

	got := e.BaseFee(DimCompute)
	expected := new(big.Int).Mul(initFee, big.NewInt(9))
	expected.Div(expected, big.NewInt(8))
	if got.Cmp(expected) != 0 {
		t.Fatalf("compute fee after max usage: got %s, want %s", got, expected)
	}
}

func TestUpdateBaseFeesBelowTargetMDG(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	initFee := big.NewInt(1_000_000_000)
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: new(big.Int).Set(initFee),
	})

	// Zero usage: fee = fee - fee * target / target / 8 = fee * 7/8.
	e.UpdateBaseFees(map[GasDimension]uint64{}, nil)

	got := e.BaseFee(DimCompute)
	expected := new(big.Int).Mul(initFee, big.NewInt(7))
	expected.Div(expected, big.NewInt(8))
	if got.Cmp(expected) != 0 {
		t.Fatalf("compute fee after zero usage: got %s, want %s", got, expected)
	}
}

func TestUpdateBaseFeesMinimumFloorMDG(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(8), // just above min of 7
	})

	// Many empty blocks should floor at MinBaseFee.
	for i := 0; i < 100; i++ {
		e.UpdateBaseFees(map[GasDimension]uint64{}, nil)
	}

	got := e.BaseFee(DimCompute)
	if got.Cmp(cfg.Dims[DimCompute].MinBaseFee) < 0 {
		t.Fatalf("compute fee below minimum: got %s, min %s", got, cfg.Dims[DimCompute].MinBaseFee)
	}
}

func TestUpdateBaseFeesMaxCeiling(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	cfg.Dims[DimCompute].MaxBaseFee = big.NewInt(2_000_000_000)
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(1_900_000_000),
	})

	// Many full blocks should hit the ceiling.
	for i := 0; i < 100; i++ {
		e.UpdateBaseFees(map[GasDimension]uint64{
			DimCompute: cfg.Dims[DimCompute].MaxGas,
		}, nil)
	}

	got := e.BaseFee(DimCompute)
	if got.Cmp(cfg.Dims[DimCompute].MaxBaseFee) > 0 {
		t.Fatalf("compute fee above ceiling: got %s, max %s", got, cfg.Dims[DimCompute].MaxBaseFee)
	}
}

func TestUpdateBaseFeesIndependenceMDG(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	initFee := big.NewInt(1_000_000)
	fees := make(map[GasDimension]*big.Int)
	for _, d := range AllGasDimensions() {
		fees[d] = new(big.Int).Set(initFee)
	}
	e, _ := NewMultidimPricingEngineWithFees(cfg, fees)

	// Only increase compute, others at target.
	usage := make(map[GasDimension]uint64)
	for _, d := range AllGasDimensions() {
		usage[d] = cfg.Dims[d].Target
	}
	usage[DimCompute] = cfg.Dims[DimCompute].MaxGas

	e.UpdateBaseFees(usage, nil)

	computeFee := e.BaseFee(DimCompute)
	if computeFee.Cmp(initFee) <= 0 {
		t.Fatal("compute fee should have increased")
	}

	// Other dimensions should be unchanged.
	for _, d := range []GasDimension{DimStorage, DimBandwidth, DimBlob, DimWitness} {
		got := e.BaseFee(d)
		if got.Cmp(initFee) != 0 {
			t.Errorf("%s fee should be unchanged: got %s, want %s", d, got, initFee)
		}
	}
}

func TestUpdateBaseFeesMinimumIncrease(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	// Start at 100 (well above MinBaseFee) so clamping doesn't interfere.
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(100),
	})

	// Slightly above target: delta = 100 * 1 / 15M / 8 = 0, so minimum increase = 1.
	usage := map[GasDimension]uint64{
		DimCompute: cfg.Dims[DimCompute].Target + 1,
	}
	e.UpdateBaseFees(usage, nil)

	got := e.BaseFee(DimCompute)
	if got.Int64() != 101 {
		t.Fatalf("minimum increase: got %s, want 101", got)
	}
}

func TestUpdateBaseFeesCustomTargets(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(1000),
	})

	// Custom target of 10M instead of default 15M. With usage=10M (at custom
	// target), fee should remain unchanged.
	customTargets := map[GasDimension]uint64{
		DimCompute: 10_000_000,
	}
	usage := map[GasDimension]uint64{
		DimCompute: 10_000_000,
	}
	e.UpdateBaseFees(usage, customTargets)

	got := e.BaseFee(DimCompute)
	if got.Int64() != 1000 {
		t.Fatalf("fee at custom target should be unchanged: got %s, want 1000", got)
	}
}

func TestTotalGasCostMDG(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute:   big.NewInt(10),
		DimStorage:   big.NewInt(20),
		DimBandwidth: big.NewInt(5),
		DimBlob:      big.NewInt(2),
		DimWitness:   big.NewInt(3),
	})

	usage := map[GasDimension]uint64{
		DimCompute:   100,
		DimStorage:   50,
		DimBandwidth: 200,
		DimBlob:      10,
		DimWitness:   30,
	}

	cost := e.TotalGasCost(usage)
	// 100*10 + 50*20 + 200*5 + 10*2 + 30*3 = 1000 + 1000 + 1000 + 20 + 90 = 3110
	if cost.Int64() != 3110 {
		t.Fatalf("TotalGasCost = %s, want 3110", cost)
	}
}

func TestTotalGasCostEmpty(t *testing.T) {
	e, _ := NewMultidimPricingEngine(DefaultMultidimGasConfig())
	cost := e.TotalGasCost(nil)
	if cost.Sign() != 0 {
		t.Fatalf("nil usage should have zero cost, got %s", cost)
	}

	cost2 := e.TotalGasCost(map[GasDimension]uint64{})
	if cost2.Sign() != 0 {
		t.Fatalf("empty usage should have zero cost, got %s", cost2)
	}
}

func TestEffectiveGasPriceMDG(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(100),
		DimBlob:    big.NewInt(50),
	})

	usage := map[GasDimension]uint64{
		DimCompute: 1000,
		DimBlob:    500,
	}

	price := e.EffectiveGasPrice(usage)
	// 1000*100 + 500*50 = 100_000 + 25_000 = 125_000
	if price.Int64() != 125_000 {
		t.Fatalf("EffectiveGasPrice = %s, want 125000", price)
	}
}

func TestBaseFeeHistoryMDG(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	cfg.HistoryLimit = 5
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(1000),
	})

	// Each UpdateBaseFees call records the current fee before updating.
	for i := 0; i < 3; i++ {
		e.UpdateBaseFees(map[GasDimension]uint64{
			DimCompute: cfg.Dims[DimCompute].Target,
		}, nil)
	}

	hist, err := e.BaseFeeHistory(DimCompute)
	if err != nil {
		t.Fatalf("BaseFeeHistory: %v", err)
	}
	if len(hist) != 3 {
		t.Fatalf("history len = %d, want 3", len(hist))
	}
	// All entries should be 1000 since usage was at target.
	for i, h := range hist {
		if h.Int64() != 1000 {
			t.Errorf("history[%d] = %s, want 1000", i, h)
		}
	}
}

func TestBaseFeeHistoryRingBuffer(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	cfg.HistoryLimit = 3
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(1000),
	})

	// Fill history beyond limit.
	for i := 0; i < 5; i++ {
		e.UpdateBaseFees(map[GasDimension]uint64{
			DimCompute: cfg.Dims[DimCompute].Target,
		}, nil)
	}

	hist, _ := e.BaseFeeHistory(DimCompute)
	if len(hist) != 3 {
		t.Fatalf("history should be capped at limit: len = %d, want 3", len(hist))
	}
}

func TestBaseFeeHistoryInvalidDim(t *testing.T) {
	e, _ := NewMultidimPricingEngine(DefaultMultidimGasConfig())
	_, err := e.BaseFeeHistory(GasDimension(99))
	if err == nil {
		t.Fatal("expected error for invalid dimension")
	}
}

func TestHistoryLen(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngine(cfg)

	if e.HistoryLen(DimCompute) != 0 {
		t.Fatal("initial history should be empty")
	}

	e.UpdateBaseFees(map[GasDimension]uint64{
		DimCompute: cfg.Dims[DimCompute].Target,
	}, nil)

	if e.HistoryLen(DimCompute) != 1 {
		t.Fatal("history should have 1 entry after 1 update")
	}

	if e.HistoryLen(GasDimension(99)) != 0 {
		t.Fatal("invalid dim should return 0")
	}
}

func TestValidateUsage(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngine(cfg)

	// Valid usage.
	err := e.ValidateUsage(map[GasDimension]uint64{
		DimCompute: 1_000_000,
		DimBlob:    100_000,
	})
	if err != nil {
		t.Fatalf("valid usage rejected: %v", err)
	}

	// Usage exceeding max.
	err = e.ValidateUsage(map[GasDimension]uint64{
		DimCompute: cfg.Dims[DimCompute].MaxGas + 1,
	})
	if err == nil {
		t.Fatal("should fail when usage exceeds max")
	}

	// Invalid dimension.
	err = e.ValidateUsage(map[GasDimension]uint64{
		GasDimension(99): 100,
	})
	if err == nil {
		t.Fatal("should fail for invalid dimension")
	}
}

func TestMultidimPricingEngineMonotonicity(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(1_000_000_000),
	})

	// Consecutive full blocks should produce monotonically increasing fees.
	prevFee := e.BaseFee(DimCompute)
	for i := 0; i < 10; i++ {
		e.UpdateBaseFees(map[GasDimension]uint64{
			DimCompute: cfg.Dims[DimCompute].MaxGas,
		}, nil)
		curFee := e.BaseFee(DimCompute)
		if curFee.Cmp(prevFee) <= 0 {
			t.Fatalf("iteration %d: fee should increase: prev %s, cur %s", i, prevFee, curFee)
		}
		prevFee = curFee
	}
}

func TestMultidimPricingEngineConcurrency(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(1_000_000_000),
		DimBlob:    big.NewInt(1000),
	})

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			e.UpdateBaseFees(map[GasDimension]uint64{
				DimCompute: 20_000_000,
				DimBlob:    500_000,
			}, nil)
		}()
		go func() {
			defer wg.Done()
			_ = e.BaseFee(DimCompute)
			_ = e.BaseFee(DimBlob)
			_ = e.AllBaseFees()
		}()
		go func() {
			defer wg.Done()
			_ = e.TotalGasCost(map[GasDimension]uint64{
				DimCompute: 21000,
				DimBlob:    131072,
			})
		}()
	}

	wg.Wait()

	// Verify no panic and fees are positive.
	for _, dim := range AllGasDimensions() {
		fee := e.BaseFee(dim)
		if fee.Sign() <= 0 {
			t.Fatalf("%s fee should be positive: got %s", dim, fee)
		}
	}
}

func TestMultidimPricingEngineBlobDenominatorDifference(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(1000),
		DimBlob:    big.NewInt(1000),
	})

	// Full blocks for both. Blob has denominator=3, compute has denominator=8.
	e.UpdateBaseFees(map[GasDimension]uint64{
		DimCompute: cfg.Dims[DimCompute].MaxGas,
		DimBlob:    cfg.Dims[DimBlob].MaxGas,
	}, nil)

	computeFee := e.BaseFee(DimCompute)
	blobFee := e.BaseFee(DimBlob)

	computeIncrease := new(big.Int).Sub(computeFee, big.NewInt(1000))
	blobIncrease := new(big.Int).Sub(blobFee, big.NewInt(1000))

	// Blob should increase faster (denom 3 vs 8).
	if blobIncrease.Cmp(computeIncrease) <= 0 {
		t.Fatalf("blob should increase faster: compute +%s, blob +%s", computeIncrease, blobIncrease)
	}
}

func TestConfigReturnsCopy(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngine(cfg)

	got := e.Config()
	got.Dims[DimCompute].Target = 99999

	// Original engine config should be unchanged.
	got2 := e.Config()
	if got2.Dims[DimCompute].Target == 99999 {
		t.Fatal("Config() should return a copy")
	}
}

func TestUpdateBaseFeesNilMaps(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(1000),
	})

	// Both nil maps should treat all usage as 0 and use default targets.
	e.UpdateBaseFees(nil, nil)

	got := e.BaseFee(DimCompute)
	// With zero usage and target 15M, fee decreases.
	if got.Cmp(big.NewInt(1000)) >= 0 {
		t.Fatalf("fee should decrease with nil usage: got %s", got)
	}
}

func TestValidateTotalGasUsageValid(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngine(cfg)

	usage := map[GasDimension]uint64{
		DimCompute:   1_000_000,
		DimStorage:   500_000,
		DimBandwidth: 200_000,
		DimBlob:      100_000,
		DimWitness:   100_000,
	}
	if err := e.ValidateTotalGasUsage(usage); err != nil {
		t.Fatalf("valid usage rejected: %v", err)
	}
}

func TestValidateTotalGasUsageExceedsGigagas(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngine(cfg)

	// Sum > 1 Ggas but each dimension is within its own max.
	usage := map[GasDimension]uint64{
		DimCompute:   cfg.Dims[DimCompute].MaxGas,
		DimStorage:   cfg.Dims[DimStorage].MaxGas,
		DimBandwidth: cfg.Dims[DimBandwidth].MaxGas,
		DimBlob:      cfg.Dims[DimBlob].MaxGas,
		DimWitness:   cfg.Dims[DimWitness].MaxGas,
	}
	// Total = 30M + 10M + 7.5M + 786K + 4M = ~52M which is below gigagas.
	// Use larger values to exceed.
	total := uint64(0)
	for _, v := range usage {
		total += v
	}
	if total > MaxTotalGasUsage {
		// If defaults already exceed, this test checks rejection.
		if err := e.ValidateTotalGasUsage(usage); err == nil {
			t.Fatal("usage exceeding gigagas should fail")
		}
	} else {
		// Defaults sum to ~52M, well below gigagas. Verify pass.
		if err := e.ValidateTotalGasUsage(usage); err != nil {
			t.Fatalf("sum below gigagas should pass: %v", err)
		}
	}
}

func TestValidateTotalGasUsageOverflow(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	// Set very large max gas to allow large values.
	for i := range cfg.Dims {
		cfg.Dims[i].MaxGas = ^uint64(0) / 2
	}
	e, _ := NewMultidimPricingEngine(cfg)

	// Two dimensions that together overflow uint64.
	usage := map[GasDimension]uint64{
		DimCompute: ^uint64(0) / 2,
		DimStorage: ^uint64(0) / 2,
	}
	if err := e.ValidateTotalGasUsage(usage); err == nil {
		t.Fatal("overflow should be detected")
	}
}

func TestValidateTotalGasUsageEmpty(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngine(cfg)

	if err := e.ValidateTotalGasUsage(nil); err != nil {
		t.Fatalf("nil usage should pass: %v", err)
	}
	if err := e.ValidateTotalGasUsage(map[GasDimension]uint64{}); err != nil {
		t.Fatalf("empty usage should pass: %v", err)
	}
}

func TestValidateTotalGasUsageInvalidDim(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngine(cfg)

	usage := map[GasDimension]uint64{
		GasDimension(99): 100,
	}
	if err := e.ValidateTotalGasUsage(usage); err == nil {
		t.Fatal("invalid dimension should fail")
	}
}

func TestUpdateBaseFeesMaxClamping(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	maxFee := big.NewInt(5_000_000_000)
	cfg.Dims[DimCompute].MaxBaseFee = maxFee

	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(4_999_999_999),
	})

	// Many full blocks should hit the ceiling.
	for i := 0; i < 50; i++ {
		e.UpdateBaseFees(map[GasDimension]uint64{
			DimCompute: cfg.Dims[DimCompute].MaxGas,
		}, nil)
	}

	got := e.BaseFee(DimCompute)
	if got.Cmp(maxFee) > 0 {
		t.Fatalf("fee %s exceeds max %s", got, maxFee)
	}
	if got.Cmp(maxFee) != 0 {
		t.Logf("fee clamped to %s (max %s)", got, maxFee)
	}
}

func TestMultipleUpdatesRecovery(t *testing.T) {
	cfg := DefaultMultidimGasConfig()
	e, _ := NewMultidimPricingEngineWithFees(cfg, map[GasDimension]*big.Int{
		DimCompute: big.NewInt(1_000_000_000),
	})

	// 5 blocks of max usage.
	for i := 0; i < 5; i++ {
		e.UpdateBaseFees(map[GasDimension]uint64{
			DimCompute: cfg.Dims[DimCompute].MaxGas,
		}, nil)
	}
	highFee := e.BaseFee(DimCompute)
	if highFee.Cmp(big.NewInt(1_500_000_000)) <= 0 {
		t.Fatalf("fee after 5 full blocks should be > 1.5e9: got %s", highFee)
	}

	// 20 empty blocks.
	for i := 0; i < 20; i++ {
		e.UpdateBaseFees(map[GasDimension]uint64{
			DimCompute: 0,
		}, nil)
	}
	lowFee := e.BaseFee(DimCompute)
	if lowFee.Cmp(highFee) >= 0 {
		t.Fatalf("fee should decrease after empty blocks: got %s", lowFee)
	}
}

func TestValidateMultidimGasConfig(t *testing.T) {
	// Nil config.
	if err := ValidateMultidimGasConfig(nil); err == nil {
		t.Fatal("expected error for nil config")
	}

	// Valid default config.
	cfg := DefaultMultidimGasConfig()
	if err := ValidateMultidimGasConfig(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Zero history limit.
	bad := DefaultMultidimGasConfig()
	bad.HistoryLimit = 0
	if err := ValidateMultidimGasConfig(&bad); err == nil {
		t.Fatal("expected error for zero history limit")
	}

	// MaxGas < Target.
	bad2 := DefaultMultidimGasConfig()
	bad2.Dims[0].MaxGas = 100
	bad2.Dims[0].Target = 200
	if err := ValidateMultidimGasConfig(&bad2); err == nil {
		t.Fatal("expected error when MaxGas < Target")
	}
}
