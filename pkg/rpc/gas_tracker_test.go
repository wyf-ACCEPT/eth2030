package rpc

import (
	"sync"
	"testing"
)

func defaultTrackerConfig() GasTrackerConfig {
	return GasTrackerConfig{
		HistoryBlocks: 10,
		Percentiles:   []float64{10, 50, 90},
		MaxCacheSize:  64,
	}
}

func makeRecord(num, baseFee, gasUsed, gasLimit uint64, prices []uint64) *GasBlockRecord {
	return &GasBlockRecord{
		BlockNumber: num,
		BaseFee:     baseFee,
		GasUsed:     gasUsed,
		GasLimit:    gasLimit,
		TxGasPrices: prices,
	}
}

func TestNewGasTracker(t *testing.T) {
	gt := NewGasTracker(GasTrackerConfig{})
	if gt == nil {
		t.Fatal("NewGasTracker returned nil")
	}
	if gt.config.HistoryBlocks != 128 {
		t.Errorf("expected default HistoryBlocks 128, got %d", gt.config.HistoryBlocks)
	}
	if gt.config.MaxCacheSize != 256 {
		t.Errorf("expected default MaxCacheSize 256, got %d", gt.config.MaxCacheSize)
	}
	if len(gt.config.Percentiles) != 3 {
		t.Errorf("expected 3 default percentiles, got %d", len(gt.config.Percentiles))
	}
}

func TestRecordBlock(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())

	err := gt.RecordBlock(makeRecord(1, 1000, 5000, 10000, []uint64{10, 20, 30}))
	if err != nil {
		t.Fatalf("RecordBlock failed: %v", err)
	}

	if gt.BlockCount() != 1 {
		t.Errorf("expected 1 block, got %d", gt.BlockCount())
	}
}

func TestRecordBlockNil(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	err := gt.RecordBlock(nil)
	if err == nil {
		t.Fatal("expected error for nil record")
	}
}

func TestRecordBlockTrimsHistory(t *testing.T) {
	gt := NewGasTracker(GasTrackerConfig{HistoryBlocks: 3, MaxCacheSize: 8})

	for i := uint64(0); i < 5; i++ {
		gt.RecordBlock(makeRecord(i, 1000+i*100, 5000, 10000, []uint64{10}))
	}

	if gt.BlockCount() != 3 {
		t.Errorf("expected 3 blocks after trimming, got %d", gt.BlockCount())
	}

	// The oldest blocks should have been trimmed; check base fee history.
	fees := gt.BaseFeeHistory(10)
	if len(fees) != 3 {
		t.Fatalf("expected 3 base fees, got %d", len(fees))
	}
	if fees[0] != 1200 {
		t.Errorf("expected first base fee 1200, got %d", fees[0])
	}
}

func TestRecordBlockSortsPrices(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 1000, 5000, 10000, []uint64{30, 10, 20}))

	// The 50th percentile of [10, 20, 30] should be 20.
	p, err := gt.Percentile(50)
	if err != nil {
		t.Fatalf("Percentile failed: %v", err)
	}
	if p != 20 {
		t.Errorf("expected percentile 50 = 20, got %d", p)
	}
}

func TestEstimateFeesEmpty(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	_, err := gt.EstimateFees()
	if err != ErrGasTrackerEmpty {
		t.Errorf("expected ErrGasTrackerEmpty, got %v", err)
	}
}

func TestEstimateFees(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())

	// Add blocks with known price distributions.
	for i := uint64(1); i <= 5; i++ {
		prices := make([]uint64, 100)
		for j := range prices {
			prices[j] = uint64(j+1) * i
		}
		gt.RecordBlock(makeRecord(i, 1000*i, 5000*i, 15000, prices))
	}

	est, err := gt.EstimateFees()
	if err != nil {
		t.Fatalf("EstimateFees failed: %v", err)
	}

	if est.BaseFeeEstimate == 0 {
		t.Error("base fee estimate should not be 0")
	}
	if est.PriorityFeeLow >= est.PriorityFeeMedium {
		t.Errorf("low (%d) should be < medium (%d)", est.PriorityFeeLow, est.PriorityFeeMedium)
	}
	if est.PriorityFeeMedium >= est.PriorityFeeHigh {
		t.Errorf("medium (%d) should be < high (%d)", est.PriorityFeeMedium, est.PriorityFeeHigh)
	}
	if est.NextBlockBaseFee == 0 {
		t.Error("next block base fee should not be 0")
	}
}

func TestEstimateFeesNoTxPrices(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 1000, 5000, 10000, nil))

	est, err := gt.EstimateFees()
	if err != nil {
		t.Fatalf("EstimateFees failed: %v", err)
	}
	if est.PriorityFeeLow != 0 {
		t.Errorf("expected 0 priority fee low with no txs, got %d", est.PriorityFeeLow)
	}
}

func TestPercentileInvalid(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 1000, 5000, 10000, []uint64{10, 20, 30}))

	_, err := gt.Percentile(-1)
	if err != ErrGasTrackerInvalidPercentile {
		t.Errorf("expected ErrGasTrackerInvalidPercentile for -1, got %v", err)
	}

	_, err = gt.Percentile(101)
	if err != ErrGasTrackerInvalidPercentile {
		t.Errorf("expected ErrGasTrackerInvalidPercentile for 101, got %v", err)
	}
}

func TestPercentileEmpty(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	_, err := gt.Percentile(50)
	if err != ErrGasTrackerEmpty {
		t.Errorf("expected ErrGasTrackerEmpty, got %v", err)
	}
}

func TestPercentileBoundaries(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 1000, 5000, 10000, []uint64{10, 20, 30, 40, 50}))

	p0, _ := gt.Percentile(0)
	if p0 != 10 {
		t.Errorf("percentile 0: expected 10, got %d", p0)
	}

	p100, _ := gt.Percentile(100)
	if p100 != 50 {
		t.Errorf("percentile 100: expected 50, got %d", p100)
	}
}

func TestPercentileCache(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 1000, 5000, 10000, []uint64{10, 20, 30}))

	// First call populates cache.
	v1, _ := gt.Percentile(50)
	// Second call should use cache.
	v2, _ := gt.Percentile(50)
	if v1 != v2 {
		t.Errorf("cached value mismatch: %d vs %d", v1, v2)
	}

	// Recording a new block should invalidate cache.
	gt.RecordBlock(makeRecord(2, 2000, 5000, 10000, []uint64{100, 200, 300}))
	v3, _ := gt.Percentile(50)
	if v3 == v1 {
		t.Error("cache should have been invalidated after RecordBlock")
	}
}

func TestAverageGasUsedEmpty(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	if avg := gt.AverageGasUsed(); avg != 0 {
		t.Errorf("expected 0 average for empty tracker, got %f", avg)
	}
}

func TestAverageGasUsed(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 1000, 5000, 10000, nil))  // 50%
	gt.RecordBlock(makeRecord(2, 1000, 10000, 10000, nil)) // 100%

	avg := gt.AverageGasUsed()
	if avg < 0.74 || avg > 0.76 {
		t.Errorf("expected ~0.75 average gas usage, got %f", avg)
	}
}

func TestAverageGasUsedZeroLimit(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 1000, 5000, 0, nil)) // gasLimit=0, should be skipped

	avg := gt.AverageGasUsed()
	if avg != 0 {
		t.Errorf("expected 0 for blocks with zero gas limit, got %f", avg)
	}
}

func TestBaseFeeHistory(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	for i := uint64(1); i <= 5; i++ {
		gt.RecordBlock(makeRecord(i, i*100, 5000, 10000, nil))
	}

	fees := gt.BaseFeeHistory(3)
	if len(fees) != 3 {
		t.Fatalf("expected 3 fees, got %d", len(fees))
	}
	if fees[0] != 300 || fees[1] != 400 || fees[2] != 500 {
		t.Errorf("unexpected fees: %v", fees)
	}

	// Requesting more than available returns all.
	fees = gt.BaseFeeHistory(100)
	if len(fees) != 5 {
		t.Errorf("expected 5 fees, got %d", len(fees))
	}
}

func TestBaseFeeHistoryZeroOrNegative(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 100, 5000, 10000, nil))

	if fees := gt.BaseFeeHistory(0); fees != nil {
		t.Error("expected nil for n=0")
	}
	if fees := gt.BaseFeeHistory(-1); fees != nil {
		t.Error("expected nil for n=-1")
	}
}

func TestPriorityFeeHistory(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 1000, 5000, 10000, []uint64{10, 20, 30}))
	gt.RecordBlock(makeRecord(2, 2000, 5000, 10000, []uint64{40, 50, 60}))
	gt.RecordBlock(makeRecord(3, 3000, 5000, 10000, nil))

	fees := gt.PriorityFeeHistory(3)
	if len(fees) != 3 {
		t.Fatalf("expected 3 priority fees, got %d", len(fees))
	}
	// median of [10,20,30] = 20 (index 1), median of [40,50,60] = 50, empty = 0
	if fees[0] != 20 {
		t.Errorf("expected priority fee 20, got %d", fees[0])
	}
	if fees[1] != 50 {
		t.Errorf("expected priority fee 50, got %d", fees[1])
	}
	if fees[2] != 0 {
		t.Errorf("expected priority fee 0 for empty block, got %d", fees[2])
	}
}

func TestTrendDirectionStable(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	for i := uint64(1); i <= 4; i++ {
		gt.RecordBlock(makeRecord(i, 1000, 5000, 10000, nil))
	}
	if d := gt.TrendDirection(); d != "stable" {
		t.Errorf("expected stable, got %s", d)
	}
}

func TestTrendDirectionRising(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	// Older blocks: low base fees.
	gt.RecordBlock(makeRecord(1, 100, 5000, 10000, nil))
	gt.RecordBlock(makeRecord(2, 100, 5000, 10000, nil))
	// Newer blocks: high base fees.
	gt.RecordBlock(makeRecord(3, 200, 5000, 10000, nil))
	gt.RecordBlock(makeRecord(4, 200, 5000, 10000, nil))

	if d := gt.TrendDirection(); d != "rising" {
		t.Errorf("expected rising, got %s", d)
	}
}

func TestTrendDirectionFalling(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 200, 5000, 10000, nil))
	gt.RecordBlock(makeRecord(2, 200, 5000, 10000, nil))
	gt.RecordBlock(makeRecord(3, 100, 5000, 10000, nil))
	gt.RecordBlock(makeRecord(4, 100, 5000, 10000, nil))

	if d := gt.TrendDirection(); d != "falling" {
		t.Errorf("expected falling, got %s", d)
	}
}

func TestTrendDirectionSingleBlock(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 1000, 5000, 10000, nil))

	if d := gt.TrendDirection(); d != "stable" {
		t.Errorf("expected stable for single block, got %s", d)
	}
}

func TestTrendDirectionEmpty(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	if d := gt.TrendDirection(); d != "stable" {
		t.Errorf("expected stable for empty tracker, got %s", d)
	}
}

func TestBlockCount(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	if gt.BlockCount() != 0 {
		t.Error("expected 0 blocks initially")
	}

	gt.RecordBlock(makeRecord(1, 1000, 5000, 10000, nil))
	gt.RecordBlock(makeRecord(2, 2000, 5000, 10000, nil))

	if gt.BlockCount() != 2 {
		t.Errorf("expected 2 blocks, got %d", gt.BlockCount())
	}
}

func TestNextBlockBaseFeePrediction(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())

	// Block at exactly the target (gasUsed = gasLimit/2) => no change.
	gt.RecordBlock(makeRecord(1, 1000, 5000, 10000, []uint64{10}))
	est, _ := gt.EstimateFees()
	if est.NextBlockBaseFee != 1000 {
		t.Errorf("expected next base fee 1000 at target, got %d", est.NextBlockBaseFee)
	}

	// Block above target => base fee should increase.
	gt2 := NewGasTracker(defaultTrackerConfig())
	gt2.RecordBlock(makeRecord(1, 1000, 10000, 10000, []uint64{10}))
	est2, _ := gt2.EstimateFees()
	if est2.NextBlockBaseFee <= 1000 {
		t.Errorf("expected increased base fee, got %d", est2.NextBlockBaseFee)
	}

	// Block below target => base fee should decrease.
	gt3 := NewGasTracker(defaultTrackerConfig())
	gt3.RecordBlock(makeRecord(1, 1000, 0, 10000, []uint64{10}))
	est3, _ := gt3.EstimateFees()
	if est3.NextBlockBaseFee >= 1000 {
		t.Errorf("expected decreased base fee, got %d", est3.NextBlockBaseFee)
	}
}

func TestGasTrackerConcurrentAccess(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	var wg sync.WaitGroup

	// Writer goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := uint64(0); i < 50; i++ {
			gt.RecordBlock(makeRecord(i, 1000+i, 5000, 10000, []uint64{10, 20, 30}))
		}
	}()

	// Reader goroutines.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				gt.BlockCount()
				gt.AverageGasUsed()
				gt.BaseFeeHistory(5)
				gt.PriorityFeeHistory(5)
				gt.TrendDirection()
				gt.EstimateFees()
			}
		}()
	}

	wg.Wait()
}

func TestEstimateFeesWithZeroBaseFee(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 0, 5000, 10000, []uint64{10, 20}))

	est, err := gt.EstimateFees()
	if err != nil {
		t.Fatalf("EstimateFees failed: %v", err)
	}
	if est.BaseFeeEstimate != 0 {
		t.Errorf("expected 0 base fee estimate, got %d", est.BaseFeeEstimate)
	}
}

func TestPercentileSinglePrice(t *testing.T) {
	gt := NewGasTracker(defaultTrackerConfig())
	gt.RecordBlock(makeRecord(1, 1000, 5000, 10000, []uint64{42}))

	p, _ := gt.Percentile(50)
	if p != 42 {
		t.Errorf("expected 42 for single-element percentile, got %d", p)
	}
}
