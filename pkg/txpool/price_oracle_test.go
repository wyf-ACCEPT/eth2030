package txpool

import (
	"math/big"
	"sync"
	"testing"
)

func newTestPriceOracle() *PriceOracle {
	return NewPriceOracle(DefaultPriceOracleConfig())
}

func makeRecord(num uint64, baseFee int64, gasUsed, gasLimit uint64, tips []int64) BlockFeeRecord {
	tipBigs := make([]*big.Int, len(tips))
	for i, t := range tips {
		tipBigs[i] = big.NewInt(t)
	}
	gasPrices := make([]*big.Int, len(tips))
	for i, t := range tips {
		gasPrices[i] = big.NewInt(baseFee + t)
	}
	return BlockFeeRecord{
		Number:    num,
		BaseFee:   big.NewInt(baseFee),
		GasUsed:   gasUsed,
		GasLimit:  gasLimit,
		Tips:      tipBigs,
		GasPrices: gasPrices,
	}
}

func TestPriceOracle_EmptyOracle(t *testing.T) {
	po := newTestPriceOracle()
	if po.BlockCount() != 0 {
		t.Errorf("BlockCount = %d, want 0", po.BlockCount())
	}
	if po.LatestBaseFee() != nil {
		t.Errorf("LatestBaseFee = %v, want nil", po.LatestBaseFee())
	}
}

func TestPriceOracle_SuggestTipCapEmpty(t *testing.T) {
	po := newTestPriceOracle()
	tip := po.SuggestTipCap()
	if tip.Cmp(big.NewInt(PriceOracleMinTip)) != 0 {
		t.Errorf("SuggestTipCap = %s, want %d", tip, PriceOracleMinTip)
	}
}

func TestPriceOracle_SuggestGasPriceEmpty(t *testing.T) {
	po := newTestPriceOracle()
	price := po.SuggestGasPrice()
	if price.Cmp(big.NewInt(PriceOracleMinBaseFee)) != 0 {
		t.Errorf("SuggestGasPrice = %s, want %d", price, PriceOracleMinBaseFee)
	}
}

func TestPriceOracle_AddBlock(t *testing.T) {
	po := newTestPriceOracle()
	rec := makeRecord(100, 10_000_000_000, 15_000_000, 30_000_000,
		[]int64{2_000_000_000, 3_000_000_000, 5_000_000_000})
	po.AddBlock(rec)
	if po.BlockCount() != 1 {
		t.Errorf("BlockCount = %d, want 1", po.BlockCount())
	}
	bf := po.LatestBaseFee()
	if bf == nil || bf.Cmp(big.NewInt(10_000_000_000)) != 0 {
		t.Errorf("LatestBaseFee = %v, want 10000000000", bf)
	}
}

func TestPriceOracle_SuggestTipCap(t *testing.T) {
	po := newTestPriceOracle()
	// Add blocks with known tips.
	for i := 0; i < 5; i++ {
		tips := []int64{1_000_000_000, 2_000_000_000, 3_000_000_000, 4_000_000_000, 5_000_000_000}
		rec := makeRecord(uint64(100+i), 10_000_000_000, 15_000_000, 30_000_000, tips)
		po.AddBlock(rec)
	}
	tip := po.SuggestTipCap()
	// Median of [1,2,3,4,5] Gwei repeated 5 times = 3 Gwei.
	expected := big.NewInt(3_000_000_000)
	if tip.Cmp(expected) != 0 {
		t.Errorf("SuggestTipCap = %s, want %s", tip, expected)
	}
}

func TestPriceOracle_SuggestGasPrice(t *testing.T) {
	po := newTestPriceOracle()
	baseFee := int64(10_000_000_000)
	tips := []int64{1_000_000_000, 2_000_000_000, 3_000_000_000}
	rec := makeRecord(100, baseFee, 15_000_000, 30_000_000, tips)
	po.AddBlock(rec)

	price := po.SuggestGasPrice()
	// Gas prices are baseFee+tip: 11, 12, 13 Gwei. Median = 12 Gwei.
	expected := big.NewInt(12_000_000_000)
	if price.Cmp(expected) != 0 {
		t.Errorf("SuggestGasPrice = %s, want %s", price, expected)
	}
}

func TestPriceOracle_EstimateNextBaseFeeFull(t *testing.T) {
	po := newTestPriceOracle()
	// Block at exactly 100% gas used (above target).
	rec := makeRecord(100, 10_000_000_000, 30_000_000, 30_000_000,
		[]int64{1_000_000_000})
	po.AddBlock(rec)
	next := po.EstimateNextBaseFee()
	// target=15M, gasUsed=30M, delta = baseFee*(30M-15M)/15M/8 = baseFee*1/8 = 1.25G
	// next = 10G + 1.25G = 11.25G
	expected := big.NewInt(11_250_000_000)
	if next.Cmp(expected) != 0 {
		t.Errorf("EstimateNextBaseFee = %s, want %s", next, expected)
	}
}

func TestPriceOracle_EstimateNextBaseFeeEmpty(t *testing.T) {
	po := newTestPriceOracle()
	// Block at 0% gas used.
	rec := makeRecord(100, 10_000_000_000, 0, 30_000_000,
		[]int64{1_000_000_000})
	po.AddBlock(rec)
	next := po.EstimateNextBaseFee()
	// target=15M, gasUsed=0, delta = baseFee*15M/15M/8 = baseFee/8 = 1.25G
	// next = 10G - 1.25G = 8.75G
	expected := big.NewInt(8_750_000_000)
	if next.Cmp(expected) != 0 {
		t.Errorf("EstimateNextBaseFee = %s, want %s", next, expected)
	}
}

func TestPriceOracle_EstimateNextBaseFeeAtTarget(t *testing.T) {
	po := newTestPriceOracle()
	// Block at exactly 50% (target).
	rec := makeRecord(100, 10_000_000_000, 15_000_000, 30_000_000,
		[]int64{1_000_000_000})
	po.AddBlock(rec)
	next := po.EstimateNextBaseFee()
	// Should remain unchanged.
	expected := big.NewInt(10_000_000_000)
	if next.Cmp(expected) != 0 {
		t.Errorf("EstimateNextBaseFee at target = %s, want %s", next, expected)
	}
}

func TestPriceOracle_Recommend(t *testing.T) {
	po := newTestPriceOracle()
	for i := 0; i < 10; i++ {
		tips := []int64{1_000_000_000, 2_000_000_000, 3_000_000_000, 4_000_000_000, 5_000_000_000}
		rec := makeRecord(uint64(100+i), 10_000_000_000, 15_000_000, 30_000_000, tips)
		po.AddBlock(rec)
	}
	rec := po.Recommend()
	if rec.BaseFee == nil || rec.BaseFee.Sign() <= 0 {
		t.Error("BaseFee should be positive")
	}
	if rec.NextBaseFee == nil || rec.NextBaseFee.Sign() <= 0 {
		t.Error("NextBaseFee should be positive")
	}
	// Slow tip <= medium tip <= fast tip.
	if rec.SlowTip.Cmp(rec.MediumTip) > 0 {
		t.Errorf("SlowTip %s > MediumTip %s", rec.SlowTip, rec.MediumTip)
	}
	if rec.MediumTip.Cmp(rec.FastTip) > 0 {
		t.Errorf("MediumTip %s > FastTip %s", rec.MediumTip, rec.FastTip)
	}
	// Slow fee <= medium fee <= fast fee.
	if rec.SlowFee.Cmp(rec.MediumFee) > 0 {
		t.Errorf("SlowFee %s > MediumFee %s", rec.SlowFee, rec.MediumFee)
	}
	if rec.MediumFee.Cmp(rec.FastFee) > 0 {
		t.Errorf("MediumFee %s > FastFee %s", rec.MediumFee, rec.FastFee)
	}
}

func TestPriceOracle_FeeHistory(t *testing.T) {
	po := newTestPriceOracle()
	for i := 0; i < 5; i++ {
		tips := []int64{1_000_000_000, 3_000_000_000, 5_000_000_000}
		rec := makeRecord(uint64(100+i), 10_000_000_000, 15_000_000, 30_000_000, tips)
		po.AddBlock(rec)
	}
	history := po.FeeHistory(3)
	if len(history) != 3 {
		t.Fatalf("FeeHistory(3) = %d entries, want 3", len(history))
	}
	// Check ordering: oldest first.
	if history[0].Number >= history[2].Number {
		t.Errorf("history not in order: %d >= %d", history[0].Number, history[2].Number)
	}
	for _, h := range history {
		if h.BaseFee.Sign() <= 0 {
			t.Error("BaseFee should be positive in history")
		}
		if h.GasUsedPct != 50.0 {
			t.Errorf("GasUsedPct = %f, want 50.0", h.GasUsedPct)
		}
	}
}

func TestPriceOracle_FeeHistoryMoreThanAvailable(t *testing.T) {
	po := newTestPriceOracle()
	rec := makeRecord(100, 10_000_000_000, 15_000_000, 30_000_000,
		[]int64{1_000_000_000})
	po.AddBlock(rec)
	history := po.FeeHistory(100)
	if len(history) != 1 {
		t.Errorf("FeeHistory(100) = %d, want 1", len(history))
	}
}

func TestPriceOracle_CircularOverflow(t *testing.T) {
	cfg := DefaultPriceOracleConfig()
	cfg.WindowSize = 3
	po := NewPriceOracle(cfg)

	for i := 0; i < 5; i++ {
		rec := makeRecord(uint64(100+i), int64((10+i)*1_000_000_000),
			15_000_000, 30_000_000, []int64{1_000_000_000})
		po.AddBlock(rec)
	}
	if po.BlockCount() != 3 {
		t.Errorf("BlockCount = %d, want 3 (window size)", po.BlockCount())
	}
	// Latest should be block 104 with base fee 14 Gwei.
	bf := po.LatestBaseFee()
	expected := big.NewInt(14_000_000_000)
	if bf.Cmp(expected) != 0 {
		t.Errorf("LatestBaseFee = %s, want %s", bf, expected)
	}
}

func TestPriceOracle_BaseFeeHistory(t *testing.T) {
	po := newTestPriceOracle()
	for i := 0; i < 3; i++ {
		rec := makeRecord(uint64(100+i), int64((10+i)*1_000_000_000),
			15_000_000, 30_000_000, []int64{1_000_000_000})
		po.AddBlock(rec)
	}
	history := po.BaseFeeHistory(3)
	if len(history) != 3 {
		t.Fatalf("BaseFeeHistory = %d, want 3", len(history))
	}
	// Should be ordered oldest first.
	if history[0].Cmp(history[2]) >= 0 {
		t.Errorf("BaseFeeHistory not ascending: %s >= %s", history[0], history[2])
	}
}

func TestPriceOracle_AverageBaseFee(t *testing.T) {
	po := newTestPriceOracle()
	po.AddBlock(makeRecord(100, 10_000_000_000, 15_000_000, 30_000_000, nil))
	po.AddBlock(makeRecord(101, 20_000_000_000, 15_000_000, 30_000_000, nil))
	avg := po.AverageBaseFee()
	expected := big.NewInt(15_000_000_000)
	if avg.Cmp(expected) != 0 {
		t.Errorf("AverageBaseFee = %s, want %s", avg, expected)
	}
}

func TestPriceOracle_AverageBaseFeeEmpty(t *testing.T) {
	po := newTestPriceOracle()
	avg := po.AverageBaseFee()
	if avg.Cmp(big.NewInt(PriceOracleMinBaseFee)) != 0 {
		t.Errorf("AverageBaseFee(empty) = %s, want %d", avg, PriceOracleMinBaseFee)
	}
}

func TestPriceOracle_MinTipEnforced(t *testing.T) {
	po := newTestPriceOracle()
	// Add block with very low tips.
	rec := makeRecord(100, 10_000_000_000, 15_000_000, 30_000_000,
		[]int64{1, 2, 3}) // Sub-Gwei tips.
	po.AddBlock(rec)
	tip := po.SuggestTipCap()
	if tip.Cmp(big.NewInt(PriceOracleMinTip)) != 0 {
		t.Errorf("SuggestTipCap = %s, want min %d", tip, PriceOracleMinTip)
	}
}

func TestPriceOracle_ConcurrentAccess(t *testing.T) {
	po := newTestPriceOracle()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rec := makeRecord(uint64(n), 10_000_000_000, 15_000_000, 30_000_000,
				[]int64{1_000_000_000, 2_000_000_000})
			po.AddBlock(rec)
		}(i)
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			po.SuggestTipCap()
			po.SuggestGasPrice()
			po.Recommend()
			po.LatestBaseFee()
			po.EstimateNextBaseFee()
		}()
	}
	wg.Wait()
	// Just verify no panic/deadlock.
	if po.BlockCount() == 0 {
		t.Error("BlockCount should be > 0")
	}
}

func TestPriceOracle_BigPercentile(t *testing.T) {
	vals := []*big.Int{
		big.NewInt(10), big.NewInt(20), big.NewInt(30),
		big.NewInt(40), big.NewInt(50),
	}
	p0 := bigPercentile(copyBigs(vals), 0)
	if p0.Cmp(big.NewInt(10)) != 0 {
		t.Errorf("p0 = %s, want 10", p0)
	}
	p100 := bigPercentile(copyBigs(vals), 100)
	if p100.Cmp(big.NewInt(50)) != 0 {
		t.Errorf("p100 = %s, want 50", p100)
	}
	p50 := bigPercentile(copyBigs(vals), 50)
	if p50.Cmp(big.NewInt(30)) != 0 {
		t.Errorf("p50 = %s, want 30", p50)
	}
}

func TestPriceOracle_BigPercentileEmpty(t *testing.T) {
	result := bigPercentile(nil, 50)
	if result.Sign() != 0 {
		t.Errorf("bigPercentile(nil, 50) = %s, want 0", result)
	}
}

func TestPriceOracle_IgnoreLowPriceTxs(t *testing.T) {
	cfg := DefaultPriceOracleConfig()
	cfg.IgnorePrice = big.NewInt(500_000_000) // 0.5 Gwei
	po := NewPriceOracle(cfg)
	// Tips of 100 wei (below ignore) and 2 Gwei.
	rec := makeRecord(100, 10_000_000_000, 15_000_000, 30_000_000,
		[]int64{100, 2_000_000_000})
	po.AddBlock(rec)
	tip := po.SuggestTipCap()
	// Should only consider the 2 Gwei tip; median of [2G] = 2G.
	expected := big.NewInt(2_000_000_000)
	if tip.Cmp(expected) != 0 {
		t.Errorf("SuggestTipCap = %s, want %s", tip, expected)
	}
}

func TestPriceOracle_RecommendWithNoHistory(t *testing.T) {
	po := newTestPriceOracle()
	rec := po.Recommend()
	// All tips should be at minimum.
	if rec.SlowTip.Cmp(big.NewInt(PriceOracleMinTip)) != 0 {
		t.Errorf("SlowTip = %s, want min", rec.SlowTip)
	}
	if rec.MediumTip.Cmp(big.NewInt(PriceOracleMinTip)) != 0 {
		t.Errorf("MediumTip = %s, want min", rec.MediumTip)
	}
	if rec.FastTip.Cmp(big.NewInt(PriceOracleMinTip)) != 0 {
		t.Errorf("FastTip = %s, want min", rec.FastTip)
	}
}

// copyBigs creates a deep copy of a big.Int slice for independent percentile tests.
func copyBigs(vals []*big.Int) []*big.Int {
	cp := make([]*big.Int, len(vals))
	for i, v := range vals {
		cp[i] = new(big.Int).Set(v)
	}
	return cp
}
