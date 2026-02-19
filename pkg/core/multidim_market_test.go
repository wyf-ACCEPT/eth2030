package core

import (
	"errors"
	"math/big"
	"sync"
	"testing"
)

func TestDimensionTypeString(t *testing.T) {
	tests := []struct {
		dim  DimensionType
		want string
	}{
		{ExecutionGas, "execution"},
		{BlobGas, "blob"},
		{AccessGas, "access"},
		{DimensionType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.dim.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.dim, got, tt.want)
		}
	}
}

func TestDefaultFeeMarketConfig(t *testing.T) {
	cfg := DefaultFeeMarketConfig()

	if cfg.Execution.TargetGas != 15_000_000 {
		t.Fatalf("exec target: got %d", cfg.Execution.TargetGas)
	}
	if cfg.Execution.MaxGas != 30_000_000 {
		t.Fatalf("exec max: got %d", cfg.Execution.MaxGas)
	}
	if cfg.Blob.TargetGas != 393_216 {
		t.Fatalf("blob target: got %d", cfg.Blob.TargetGas)
	}
	if cfg.Access.TargetGas != 3_750_000 {
		t.Fatalf("access target: got %d", cfg.Access.TargetGas)
	}
}

func TestNewFeeMarketInitialBaseFees(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarket(cfg)

	execFee := fm.BaseFee(ExecutionGas)
	if execFee.Int64() != cfg.Execution.MinBaseFee {
		t.Fatalf("initial exec fee: got %s, want %d", execFee, cfg.Execution.MinBaseFee)
	}

	blobFee := fm.BaseFee(BlobGas)
	if blobFee.Int64() != cfg.Blob.MinBaseFee {
		t.Fatalf("initial blob fee: got %s, want %d", blobFee, cfg.Blob.MinBaseFee)
	}

	accessFee := fm.BaseFee(AccessGas)
	if accessFee.Int64() != cfg.Access.MinBaseFee {
		t.Fatalf("initial access fee: got %s, want %d", accessFee, cfg.Access.MinBaseFee)
	}
}

func TestNewFeeMarketWithBaseFees(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	execFee := big.NewInt(1_000_000_000)
	blobFee := big.NewInt(1000)
	accessFee := big.NewInt(500)

	fm := NewFeeMarketWithBaseFees(cfg, execFee, blobFee, accessFee)

	if fm.BaseFee(ExecutionGas).Cmp(execFee) != 0 {
		t.Fatalf("exec fee: got %s, want %s", fm.BaseFee(ExecutionGas), execFee)
	}
	if fm.BaseFee(BlobGas).Cmp(blobFee) != 0 {
		t.Fatalf("blob fee: got %s, want %s", fm.BaseFee(BlobGas), blobFee)
	}
	if fm.BaseFee(AccessGas).Cmp(accessFee) != 0 {
		t.Fatalf("access fee: got %s, want %s", fm.BaseFee(AccessGas), accessFee)
	}
}

func TestNewFeeMarketInvalidConfig(t *testing.T) {
	// Invalid config should fall back to defaults.
	cfg := FeeMarketConfig{} // all zeros
	fm := NewFeeMarket(cfg)

	// Should use default config.
	execFee := fm.BaseFee(ExecutionGas)
	if execFee.Int64() != DefaultFeeMarketConfig().Execution.MinBaseFee {
		t.Fatalf("should use default config: got exec fee %s", execFee)
	}
}

func TestUpdateBaseFeesAtTarget(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	before := fm.BaseFee(ExecutionGas)

	// Usage exactly at target: fee unchanged.
	fm.UpdateBaseFees(cfg.Execution.TargetGas, cfg.Blob.TargetGas, cfg.Access.TargetGas)

	after := fm.BaseFee(ExecutionGas)
	if after.Cmp(before) != 0 {
		t.Fatalf("fee at target should be unchanged: before %s, after %s", before, after)
	}

	blobAfter := fm.BaseFee(BlobGas)
	if blobAfter.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("blob fee at target: got %s, want 1000", blobAfter)
	}

	accessAfter := fm.BaseFee(AccessGas)
	if accessAfter.Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("access fee at target: got %s, want 500", accessAfter)
	}
}

func TestUpdateBaseFeesAboveTarget(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	initialFee := big.NewInt(1_000_000_000)
	fm := NewFeeMarketWithBaseFees(cfg,
		new(big.Int).Set(initialFee),
		big.NewInt(1000),
		big.NewInt(500),
	)

	// Use 100% of limit (2x target): fee should increase.
	fm.UpdateBaseFees(cfg.Execution.MaxGas, cfg.Blob.MaxGas, cfg.Access.MaxGas)

	execFee := fm.BaseFee(ExecutionGas)
	if execFee.Cmp(initialFee) <= 0 {
		t.Fatalf("fee should increase above target: got %s, was %s", execFee, initialFee)
	}

	// With elasticity=2 and denominator=8, at max usage (2*target):
	// delta = fee * (used-target)/target / denominator = fee * 1 / 8 = fee/8
	// new fee = fee + fee/8 = fee * 9/8
	expected := new(big.Int).Mul(initialFee, big.NewInt(9))
	expected.Div(expected, big.NewInt(8))
	if execFee.Cmp(expected) != 0 {
		t.Fatalf("exec fee after max usage: got %s, want %s", execFee, expected)
	}
}

func TestUpdateBaseFeesBelowTarget(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	initialFee := big.NewInt(1_000_000_000)
	fm := NewFeeMarketWithBaseFees(cfg,
		new(big.Int).Set(initialFee),
		big.NewInt(1000),
		big.NewInt(500),
	)

	// Use 0 gas: fee should decrease.
	fm.UpdateBaseFees(0, 0, 0)

	execFee := fm.BaseFee(ExecutionGas)
	if execFee.Cmp(initialFee) >= 0 {
		t.Fatalf("fee should decrease below target: got %s, was %s", execFee, initialFee)
	}

	// At 0 usage: delta = fee * target/target / denominator = fee / 8
	// new fee = fee - fee/8 = fee * 7/8
	expected := new(big.Int).Mul(initialFee, big.NewInt(7))
	expected.Div(expected, big.NewInt(8))
	if execFee.Cmp(expected) != 0 {
		t.Fatalf("exec fee after zero usage: got %s, want %s", execFee, expected)
	}
}

func TestUpdateBaseFeesMinimumFloor(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	// Start with a very low fee.
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(8), // just above min of 7
		big.NewInt(2),
		big.NewInt(2),
	)

	// Zero usage should try to decrease, but floor at minimum.
	for i := 0; i < 100; i++ {
		fm.UpdateBaseFees(0, 0, 0)
	}

	execFee := fm.BaseFee(ExecutionGas)
	if execFee.Cmp(big.NewInt(cfg.Execution.MinBaseFee)) < 0 {
		t.Fatalf("exec fee below minimum: got %s, min %d", execFee, cfg.Execution.MinBaseFee)
	}

	blobFee := fm.BaseFee(BlobGas)
	if blobFee.Cmp(big.NewInt(cfg.Blob.MinBaseFee)) < 0 {
		t.Fatalf("blob fee below minimum: got %s, min %d", blobFee, cfg.Blob.MinBaseFee)
	}

	accessFee := fm.BaseFee(AccessGas)
	if accessFee.Cmp(big.NewInt(cfg.Access.MinBaseFee)) < 0 {
		t.Fatalf("access fee below minimum: got %s, min %d", accessFee, cfg.Access.MinBaseFee)
	}
}

func TestUpdateBaseFeesMonotonicity(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	// Consecutive full-block usage should produce monotonically increasing fees.
	prevFee := fm.BaseFee(ExecutionGas)
	for i := 0; i < 10; i++ {
		fm.UpdateBaseFees(cfg.Execution.MaxGas, cfg.Blob.TargetGas, cfg.Access.TargetGas)
		curFee := fm.BaseFee(ExecutionGas)
		if curFee.Cmp(prevFee) <= 0 {
			t.Fatalf("iteration %d: fee should increase: prev %s, cur %s", i, prevFee, curFee)
		}
		prevFee = curFee
	}
}

func TestUpdateBaseFeesIndependence(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	// Only execution gas above target; blob and access at target.
	fm.UpdateBaseFees(cfg.Execution.MaxGas, cfg.Blob.TargetGas, cfg.Access.TargetGas)

	execFee := fm.BaseFee(ExecutionGas)
	blobFee := fm.BaseFee(BlobGas)
	accessFee := fm.BaseFee(AccessGas)

	if execFee.Cmp(big.NewInt(1_000_000_000)) <= 0 {
		t.Fatal("exec fee should have increased")
	}
	if blobFee.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("blob fee should be unchanged: got %s", blobFee)
	}
	if accessFee.Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("access fee should be unchanged: got %s", accessFee)
	}
}

func TestValidateFeesSuccess(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	tx := &MultidimTx{
		GasLimit:           21000,
		BlobCount:          2,
		AccessListSize:     1000,
		MaxFeePerGas:       big.NewInt(2_000_000_000),
		MaxBlobFeePerGas:   big.NewInt(2000),
		MaxAccessFeePerGas: big.NewInt(1000),
	}

	if err := fm.ValidateFees(tx); err != nil {
		t.Fatalf("valid fees rejected: %v", err)
	}
}

func TestValidateFeesExactBaseFee(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	// Fees exactly at base fee should pass.
	tx := &MultidimTx{
		GasLimit:           21000,
		BlobCount:          1,
		AccessListSize:     500,
		MaxFeePerGas:       big.NewInt(1_000_000_000),
		MaxBlobFeePerGas:   big.NewInt(1000),
		MaxAccessFeePerGas: big.NewInt(500),
	}

	if err := fm.ValidateFees(tx); err != nil {
		t.Fatalf("exact base fee should pass: %v", err)
	}
}

func TestValidateFeesExecBelowBaseFee(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	tx := &MultidimTx{
		GasLimit:     21000,
		MaxFeePerGas: big.NewInt(999_999_999), // below base fee
	}

	err := fm.ValidateFees(tx)
	if err == nil {
		t.Fatal("should fail with exec fee below base")
	}
	if !errors.Is(err, ErrFeeBelowBaseFee) {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestValidateFeesBlobBelowBaseFee(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	tx := &MultidimTx{
		GasLimit:         21000,
		BlobCount:        1,
		MaxFeePerGas:     big.NewInt(2_000_000_000),
		MaxBlobFeePerGas: big.NewInt(999), // below blob base fee
	}

	err := fm.ValidateFees(tx)
	if err == nil {
		t.Fatal("should fail with blob fee below base")
	}
	if !errors.Is(err, ErrBlobFeeBelowBaseFee) {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestValidateFeesAccessBelowBaseFee(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	tx := &MultidimTx{
		GasLimit:           21000,
		AccessListSize:     100,
		MaxFeePerGas:       big.NewInt(2_000_000_000),
		MaxAccessFeePerGas: big.NewInt(499), // below access base fee
	}

	err := fm.ValidateFees(tx)
	if err == nil {
		t.Fatal("should fail with access fee below base")
	}
	if !errors.Is(err, ErrAccessFeeBelowBaseFee) {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestValidateFeesNilFeeFields(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	// Nil exec fee with non-zero gas limit should fail.
	tx := &MultidimTx{GasLimit: 21000, MaxFeePerGas: nil}
	if err := fm.ValidateFees(tx); err == nil {
		t.Fatal("nil exec fee should fail with non-zero gas limit")
	}

	// Nil blob fee with blobs should fail.
	tx2 := &MultidimTx{
		GasLimit:         21000,
		BlobCount:        1,
		MaxFeePerGas:     big.NewInt(2_000_000_000),
		MaxBlobFeePerGas: nil,
	}
	if err := fm.ValidateFees(tx2); err == nil {
		t.Fatal("nil blob fee should fail with blobs")
	}

	// Nil access fee with access list should fail.
	tx3 := &MultidimTx{
		GasLimit:           21000,
		AccessListSize:     100,
		MaxFeePerGas:       big.NewInt(2_000_000_000),
		MaxAccessFeePerGas: nil,
	}
	if err := fm.ValidateFees(tx3); err == nil {
		t.Fatal("nil access fee should fail with access list")
	}
}

func TestValidateFeesZeroDimensions(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	// Zero gas, zero blobs, zero access: all checks skipped.
	tx := &MultidimTx{
		GasLimit:       0,
		BlobCount:      0,
		AccessListSize: 0,
	}
	if err := fm.ValidateFees(tx); err != nil {
		t.Fatalf("zero-dimension tx should pass: %v", err)
	}
}

func TestEffectiveFee(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(10), // exec base fee
		big.NewInt(5),  // blob base fee
		big.NewInt(3),  // access base fee
	)

	tx := &MultidimTx{
		GasLimit:           1000,
		BlobCount:          1,        // 131072 blob gas
		AccessListSize:     500,
		MaxFeePerGas:       big.NewInt(10), // == base fee
		MaxBlobFeePerGas:   big.NewInt(5),  // == base fee
		MaxAccessFeePerGas: big.NewInt(3),  // == base fee
	}

	fee := fm.EffectiveFee(tx)
	// 1000*10 + 131072*5 + 500*3 = 10000 + 655360 + 1500 = 666860
	expected := int64(10000 + 655360 + 1500)
	if fee.Int64() != expected {
		t.Fatalf("effective fee: got %s, want %d", fee, expected)
	}
}

func TestEffectiveFeeUsesMinOfMaxAndBase(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(100), // exec base fee
		big.NewInt(50),  // blob base fee
		big.NewInt(30),  // access base fee
	)

	// Max fees are below base fees: effective uses max fees.
	tx := &MultidimTx{
		GasLimit:           1000,
		BlobCount:          0,
		AccessListSize:     0,
		MaxFeePerGas:       big.NewInt(80), // below base
		MaxBlobFeePerGas:   big.NewInt(40),
		MaxAccessFeePerGas: big.NewInt(20),
	}

	fee := fm.EffectiveFee(tx)
	// Only execution: 1000 * min(80, 100) = 1000 * 80 = 80000
	if fee.Int64() != 80000 {
		t.Fatalf("effective fee with low max: got %s, want 80000", fee)
	}
}

func TestEstimateTotalCost(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(10),
		big.NewInt(5),
		big.NewInt(3),
	)

	tx := &MultidimTx{
		GasLimit:       1000,
		BlobCount:      1,    // 131072 blob gas
		AccessListSize: 500,
	}

	cost := fm.EstimateTotalCost(tx)
	// 1000*10 + 131072*5 + 500*3 = 10000 + 655360 + 1500 = 666860
	expected := int64(10000 + 655360 + 1500)
	if cost.Int64() != expected {
		t.Fatalf("estimated cost: got %s, want %d", cost, expected)
	}
}

func TestEstimateTotalCostExecOnly(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	tx := &MultidimTx{
		GasLimit:       21000,
		BlobCount:      0,
		AccessListSize: 0,
	}

	cost := fm.EstimateTotalCost(tx)
	// 21000 * 1_000_000_000 = 21_000_000_000_000
	expected := new(big.Int).Mul(big.NewInt(21000), big.NewInt(1_000_000_000))
	if cost.Cmp(expected) != 0 {
		t.Fatalf("exec-only cost: got %s, want %s", cost, expected)
	}
}

func TestMultidimTxBlobGasUsed(t *testing.T) {
	tx := &MultidimTx{BlobCount: 3}
	if tx.BlobGasUsed() != 3*131072 {
		t.Fatalf("blob gas used: got %d, want %d", tx.BlobGasUsed(), 3*131072)
	}

	tx0 := &MultidimTx{BlobCount: 0}
	if tx0.BlobGasUsed() != 0 {
		t.Fatalf("zero blob gas: got %d", tx0.BlobGasUsed())
	}
}

func TestBaseFeeReturnsDefensiveCopy(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1000),
		big.NewInt(100),
		big.NewInt(50),
	)

	fee := fm.BaseFee(ExecutionGas)
	fee.SetInt64(999999) // mutate the returned value

	// Original should be unchanged.
	fee2 := fm.BaseFee(ExecutionGas)
	if fee2.Int64() != 1000 {
		t.Fatalf("BaseFee should return defensive copy: got %s after mutation", fee2)
	}
}

func TestFeeMarketConcurrency(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	var wg sync.WaitGroup

	// Concurrent reads and writes.
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			fm.UpdateBaseFees(20_000_000, 500_000, 4_000_000)
		}()
		go func() {
			defer wg.Done()
			_ = fm.BaseFee(ExecutionGas)
			_ = fm.BaseFee(BlobGas)
			_ = fm.BaseFee(AccessGas)
		}()
	}

	wg.Wait()

	// Just verify no panic and fees are positive.
	for _, dim := range []DimensionType{ExecutionGas, BlobGas, AccessGas} {
		fee := fm.BaseFee(dim)
		if fee.Sign() <= 0 {
			t.Fatalf("%s base fee should be positive after concurrent updates: got %s", dim, fee)
		}
	}
}

func TestFeeMarketBlobDenominatorDifference(t *testing.T) {
	// Blob dimension has denominator=3, so fees change faster than execution (denominator=8).
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1000),
		big.NewInt(1000),
		big.NewInt(1000),
	)

	// Full blocks for both dimensions.
	fm.UpdateBaseFees(cfg.Execution.MaxGas, cfg.Blob.MaxGas, cfg.Access.TargetGas)

	execFee := fm.BaseFee(ExecutionGas)
	blobFee := fm.BaseFee(BlobGas)

	// Blob should increase faster (1/3 = 33%) vs execution (1/8 = 12.5%).
	execIncrease := new(big.Int).Sub(execFee, big.NewInt(1000))
	blobIncrease := new(big.Int).Sub(blobFee, big.NewInt(1000))

	if blobIncrease.Cmp(execIncrease) <= 0 {
		t.Fatalf("blob should increase faster: exec +%s, blob +%s", execIncrease, blobIncrease)
	}
}

func TestAdjustBaseFeeMinimumIncrease(t *testing.T) {
	// When fee is very low and excess is small, delta should still be at least 1.
	cfg := DimensionConfig{
		TargetGas:                1000,
		MaxGas:                   2000,
		BaseFeeChangeDenominator: 8,
		MinBaseFee:               1,
	}

	// With fee=1 and usage just slightly above target:
	// delta = 1 * 1 / 1000 / 8 = 0, but minimum is 1.
	result := adjustBaseFee(big.NewInt(1), 1001, cfg)
	expected := big.NewInt(2) // 1 + min_increase(1) = 2
	if result.Cmp(expected) != 0 {
		t.Fatalf("minimum increase: got %s, want %s", result, expected)
	}
}

func TestValidateConfigErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  FeeMarketConfig
	}{
		{
			"zero exec target",
			FeeMarketConfig{
				Execution: DimensionConfig{TargetGas: 0, MaxGas: 1, BaseFeeChangeDenominator: 1},
				Blob:      DimensionConfig{TargetGas: 1, MaxGas: 1, BaseFeeChangeDenominator: 1},
				Access:    DimensionConfig{TargetGas: 1, MaxGas: 1, BaseFeeChangeDenominator: 1},
			},
		},
		{
			"zero blob max",
			FeeMarketConfig{
				Execution: DimensionConfig{TargetGas: 1, MaxGas: 1, BaseFeeChangeDenominator: 1},
				Blob:      DimensionConfig{TargetGas: 1, MaxGas: 0, BaseFeeChangeDenominator: 1},
				Access:    DimensionConfig{TargetGas: 1, MaxGas: 1, BaseFeeChangeDenominator: 1},
			},
		},
		{
			"zero access denominator",
			FeeMarketConfig{
				Execution: DimensionConfig{TargetGas: 1, MaxGas: 1, BaseFeeChangeDenominator: 1},
				Blob:      DimensionConfig{TargetGas: 1, MaxGas: 1, BaseFeeChangeDenominator: 1},
				Access:    DimensionConfig{TargetGas: 1, MaxGas: 1, BaseFeeChangeDenominator: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestBaseFeeUnknownDimension(t *testing.T) {
	fm := NewFeeMarket(DefaultFeeMarketConfig())
	fee := fm.BaseFee(DimensionType(99))
	if fee.Sign() != 0 {
		t.Fatalf("unknown dimension should return 0: got %s", fee)
	}
}

func TestEffectiveFeeZeroTx(t *testing.T) {
	fm := NewFeeMarket(DefaultFeeMarketConfig())
	tx := &MultidimTx{}
	fee := fm.EffectiveFee(tx)
	if fee.Sign() != 0 {
		t.Fatalf("zero tx should have zero effective fee: got %s", fee)
	}
}

func TestEstimateTotalCostZeroTx(t *testing.T) {
	fm := NewFeeMarket(DefaultFeeMarketConfig())
	tx := &MultidimTx{}
	cost := fm.EstimateTotalCost(tx)
	if cost.Sign() != 0 {
		t.Fatalf("zero tx should have zero cost: got %s", cost)
	}
}

func TestFeeMarketMultipleUpdates(t *testing.T) {
	cfg := DefaultFeeMarketConfig()
	fm := NewFeeMarketWithBaseFees(cfg,
		big.NewInt(1_000_000_000),
		big.NewInt(1000),
		big.NewInt(500),
	)

	// Several blocks of high usage.
	for i := 0; i < 5; i++ {
		fm.UpdateBaseFees(cfg.Execution.MaxGas, cfg.Blob.TargetGas, cfg.Access.TargetGas)
	}

	execFee := fm.BaseFee(ExecutionGas)
	// After 5 blocks at max: 1e9 * (9/8)^5 ~ 1.8e9
	if execFee.Cmp(big.NewInt(1_500_000_000)) <= 0 {
		t.Fatalf("exec fee after 5 full blocks should be > 1.5e9: got %s", execFee)
	}

	// Several blocks of zero usage to bring it back down.
	for i := 0; i < 20; i++ {
		fm.UpdateBaseFees(0, cfg.Blob.TargetGas, cfg.Access.TargetGas)
	}

	execFeeLow := fm.BaseFee(ExecutionGas)
	if execFeeLow.Cmp(execFee) >= 0 {
		t.Fatalf("fee should decrease after empty blocks: got %s", execFeeLow)
	}
}
