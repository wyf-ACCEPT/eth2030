package txpool

import (
	"math/big"
	"sync"
	"testing"
)

func newTestBlobTracker() *BlobFeeTracker {
	return NewBlobFeeTracker(DefaultBlobFeeTrackerConfig())
}

func makeBlobRecord(num uint64, blobFee int64, blobGasUsed, blobGasLimit uint64, blobs int) BlobFeeRecord {
	return BlobFeeRecord{
		Number:       num,
		BlobBaseFee:  big.NewInt(blobFee),
		BlobGasUsed:  blobGasUsed,
		BlobGasLimit: blobGasLimit,
		BlobCount:    blobs,
	}
}

func TestBlobFeeTracker_EmptyTracker(t *testing.T) {
	bt := newTestBlobTracker()
	if bt.BlockCount() != 0 {
		t.Errorf("BlockCount = %d, want 0", bt.BlockCount())
	}
	if bt.LatestBlobBaseFee() != nil {
		t.Errorf("LatestBlobBaseFee = %v, want nil", bt.LatestBlobBaseFee())
	}
}

func TestBlobFeeTracker_AddBlock(t *testing.T) {
	bt := newTestBlobTracker()
	rec := makeBlobRecord(100, 1000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3)
	bt.AddBlock(rec)
	if bt.BlockCount() != 1 {
		t.Errorf("BlockCount = %d, want 1", bt.BlockCount())
	}
	fee := bt.LatestBlobBaseFee()
	if fee == nil || fee.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("LatestBlobBaseFee = %v, want 1000", fee)
	}
}

func TestBlobFeeTracker_MovingAverageEmpty(t *testing.T) {
	bt := newTestBlobTracker()
	avg := bt.MovingAverage()
	if avg.Cmp(big.NewInt(BlobFeeDefaultFloor)) != 0 {
		t.Errorf("MovingAverage(empty) = %s, want %d", avg, BlobFeeDefaultFloor)
	}
}

func TestBlobFeeTracker_MovingAverage(t *testing.T) {
	bt := newTestBlobTracker()
	bt.AddBlock(makeBlobRecord(100, 1000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	bt.AddBlock(makeBlobRecord(101, 2000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	bt.AddBlock(makeBlobRecord(102, 3000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	avg := bt.MovingAverage()
	// (1000 + 2000 + 3000) / 3 = 2000
	expected := big.NewInt(2000)
	if avg.Cmp(expected) != 0 {
		t.Errorf("MovingAverage = %s, want %s", avg, expected)
	}
}

func TestBlobFeeTracker_EstimateNextBlobFeeEmpty(t *testing.T) {
	bt := newTestBlobTracker()
	next := bt.EstimateNextBlobFee()
	if next.Cmp(big.NewInt(BlobFeeDefaultFloor)) != 0 {
		t.Errorf("EstimateNextBlobFee(empty) = %s, want %d", next, BlobFeeDefaultFloor)
	}
}

func TestBlobFeeTracker_EstimateNextBlobFeeAboveTarget(t *testing.T) {
	bt := newTestBlobTracker()
	// Blob gas used at maximum (above target).
	rec := makeBlobRecord(100, 10000, BlobMaxGasPerBlock, BlobMaxGasPerBlock, 6)
	bt.AddBlock(rec)
	next := bt.EstimateNextBlobFee()
	// With blobGasUsed > target, fee should increase.
	if next.Cmp(big.NewInt(10000)) <= 0 {
		t.Errorf("EstimateNextBlobFee(above target) = %s, should be > 10000", next)
	}
}

func TestBlobFeeTracker_EstimateNextBlobFeeBelowTarget(t *testing.T) {
	bt := newTestBlobTracker()
	// No blob gas used (below target).
	rec := makeBlobRecord(100, 10000, 0, BlobMaxGasPerBlock, 0)
	bt.AddBlock(rec)
	next := bt.EstimateNextBlobFee()
	// With blobGasUsed < target, fee should decrease.
	if next.Cmp(big.NewInt(10000)) >= 0 {
		t.Errorf("EstimateNextBlobFee(below target) = %s, should be < 10000", next)
	}
}

func TestBlobFeeTracker_EstimateNextBlobFeeAtTarget(t *testing.T) {
	bt := newTestBlobTracker()
	rec := makeBlobRecord(100, 10000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3)
	bt.AddBlock(rec)
	next := bt.EstimateNextBlobFee()
	// At target, fee should remain unchanged.
	if next.Cmp(big.NewInt(10000)) != 0 {
		t.Errorf("EstimateNextBlobFee(at target) = %s, want 10000", next)
	}
}

func TestBlobFeeTracker_SuggestBlobFeeEmpty(t *testing.T) {
	bt := newTestBlobTracker()
	fee := bt.SuggestBlobFee()
	if fee.Cmp(big.NewInt(BlobFeeDefaultFloor)) != 0 {
		t.Errorf("SuggestBlobFee(empty) = %s, want %d", fee, BlobFeeDefaultFloor)
	}
}

func TestBlobFeeTracker_SuggestBlobFee(t *testing.T) {
	bt := newTestBlobTracker()
	for i := 0; i < 10; i++ {
		bt.AddBlock(makeBlobRecord(uint64(100+i), 1000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	}
	fee := bt.SuggestBlobFee()
	// Should be median (1000) * 9/8 = 1125.
	expected := big.NewInt(1125)
	if fee.Cmp(expected) != 0 {
		t.Errorf("SuggestBlobFee = %s, want %s", fee, expected)
	}
}

func TestBlobFeeTracker_SuggestMultiLevel(t *testing.T) {
	bt := newTestBlobTracker()
	for i := 0; i < 20; i++ {
		fee := int64(1000 + i*100)
		bt.AddBlock(makeBlobRecord(uint64(100+i), fee, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	}
	suggestion := bt.Suggest()

	// Slow <= Medium <= Fast.
	if suggestion.SlowFee.Cmp(suggestion.MediumFee) > 0 {
		t.Errorf("SlowFee %s > MediumFee %s", suggestion.SlowFee, suggestion.MediumFee)
	}
	if suggestion.MediumFee.Cmp(suggestion.FastFee) > 0 {
		t.Errorf("MediumFee %s > FastFee %s", suggestion.MediumFee, suggestion.FastFee)
	}
	if suggestion.CurrentFee == nil || suggestion.CurrentFee.Sign() <= 0 {
		t.Error("CurrentFee should be positive")
	}
	if suggestion.EstimatedFee == nil || suggestion.EstimatedFee.Sign() <= 0 {
		t.Error("EstimatedFee should be positive")
	}
}

func TestBlobFeeTracker_SpikeDetection(t *testing.T) {
	cfg := DefaultBlobFeeTrackerConfig()
	cfg.SpikeThresholdPct = 200 // 2x average = spike
	bt := NewBlobFeeTracker(cfg)

	// Add blocks with steady fees of 1000.
	for i := 0; i < 10; i++ {
		bt.AddBlock(makeBlobRecord(uint64(100+i), 1000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	}

	// Add a block with a big spike (10000, which is 10x average).
	bt.AddBlock(makeBlobRecord(110, 10000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))

	spikes := bt.Spikes()
	if len(spikes) == 0 {
		t.Fatal("expected at least one spike to be detected")
	}
	lastSpike := spikes[len(spikes)-1]
	if lastSpike.BlockNumber != 110 {
		t.Errorf("spike block = %d, want 110", lastSpike.BlockNumber)
	}
	if lastSpike.Ratio < 2.0 {
		t.Errorf("spike ratio = %f, want >= 2.0", lastSpike.Ratio)
	}
}

func TestBlobFeeTracker_IsCurrentSpike(t *testing.T) {
	cfg := DefaultBlobFeeTrackerConfig()
	cfg.SpikeThresholdPct = 200
	bt := NewBlobFeeTracker(cfg)

	// Add steady fees.
	for i := 0; i < 10; i++ {
		bt.AddBlock(makeBlobRecord(uint64(100+i), 1000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	}
	if bt.IsCurrentSpike() {
		t.Error("should not be a spike with steady fees")
	}

	// Add spike.
	bt.AddBlock(makeBlobRecord(110, 10000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	if !bt.IsCurrentSpike() {
		t.Error("should detect spike at 10x the average")
	}
}

func TestBlobFeeTracker_FeeHistory(t *testing.T) {
	bt := newTestBlobTracker()
	for i := 0; i < 5; i++ {
		bt.AddBlock(makeBlobRecord(uint64(100+i), int64(1000+i*100), BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	}
	history := bt.FeeHistory(3)
	if len(history) != 3 {
		t.Fatalf("FeeHistory(3) = %d, want 3", len(history))
	}
	// Oldest first: 1200, 1300, 1400.
	if history[0].Cmp(history[2]) >= 0 {
		t.Errorf("history not ascending: %s >= %s", history[0], history[2])
	}
}

func TestBlobFeeTracker_FeeHistoryMoreThanAvailable(t *testing.T) {
	bt := newTestBlobTracker()
	bt.AddBlock(makeBlobRecord(100, 1000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	history := bt.FeeHistory(100)
	if len(history) != 1 {
		t.Errorf("FeeHistory(100) = %d, want 1", len(history))
	}
}

func TestBlobFeeTracker_CircularOverflow(t *testing.T) {
	cfg := DefaultBlobFeeTrackerConfig()
	cfg.WindowSize = 3
	bt := NewBlobFeeTracker(cfg)

	for i := 0; i < 5; i++ {
		bt.AddBlock(makeBlobRecord(uint64(100+i), int64(1000+i*100), BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	}
	if bt.BlockCount() != 3 {
		t.Errorf("BlockCount = %d, want 3", bt.BlockCount())
	}
	// Latest should be block 104 with fee 1400.
	fee := bt.LatestBlobBaseFee()
	if fee.Cmp(big.NewInt(1400)) != 0 {
		t.Errorf("LatestBlobBaseFee = %s, want 1400", fee)
	}
}

func TestBlobFeeTracker_BlobGasUtilization(t *testing.T) {
	bt := newTestBlobTracker()
	// Blocks at 50% utilization (target = max/2).
	for i := 0; i < 5; i++ {
		bt.AddBlock(makeBlobRecord(uint64(100+i), 1000, BlobMaxGasPerBlock/2, BlobMaxGasPerBlock, 3))
	}
	util := bt.BlobGasUtilization()
	if util < 49.9 || util > 50.1 {
		t.Errorf("BlobGasUtilization = %f, want ~50.0", util)
	}
}

func TestBlobFeeTracker_BlobGasUtilizationEmpty(t *testing.T) {
	bt := newTestBlobTracker()
	if bt.BlobGasUtilization() != 0 {
		t.Errorf("BlobGasUtilization(empty) = %f, want 0", bt.BlobGasUtilization())
	}
}

func TestBlobFeeTracker_TotalBlobCount(t *testing.T) {
	bt := newTestBlobTracker()
	bt.AddBlock(makeBlobRecord(100, 1000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	bt.AddBlock(makeBlobRecord(101, 1000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 6))
	bt.AddBlock(makeBlobRecord(102, 1000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 1))
	if bt.TotalBlobCount() != 10 {
		t.Errorf("TotalBlobCount = %d, want 10", bt.TotalBlobCount())
	}
}

func TestBlobFeeTracker_MinFeeFloor(t *testing.T) {
	cfg := DefaultBlobFeeTrackerConfig()
	cfg.MinBlobFee = big.NewInt(100)
	bt := NewBlobFeeTracker(cfg)

	// Add block with fee below the configured floor.
	bt.AddBlock(makeBlobRecord(100, 10, 0, BlobMaxGasPerBlock, 0))
	next := bt.EstimateNextBlobFee()
	if next.Cmp(big.NewInt(100)) < 0 {
		t.Errorf("EstimateNextBlobFee = %s, should not be below floor 100", next)
	}
}

func TestBlobFeeTracker_ConcurrentAccess(t *testing.T) {
	bt := newTestBlobTracker()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			bt.AddBlock(makeBlobRecord(uint64(n), int64(1000+n*10), BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
		}(i)
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bt.SuggestBlobFee()
			bt.MovingAverage()
			bt.EstimateNextBlobFee()
			bt.Suggest()
			bt.IsCurrentSpike()
			bt.BlobGasUtilization()
		}()
	}
	wg.Wait()
	if bt.BlockCount() == 0 {
		t.Error("BlockCount should be > 0 after concurrent writes")
	}
}

func TestBlobFeeTracker_SuggestNoSpike(t *testing.T) {
	bt := newTestBlobTracker()
	for i := 0; i < 10; i++ {
		bt.AddBlock(makeBlobRecord(uint64(100+i), 1000, BlobTargetGasPerBlock, BlobMaxGasPerBlock, 3))
	}
	suggestion := bt.Suggest()
	if suggestion.IsSpike {
		t.Error("should not detect spike with steady fees")
	}
}

func TestBlobFeeTracker_BlobFeePercentile(t *testing.T) {
	vals := []*big.Int{
		big.NewInt(100), big.NewInt(200), big.NewInt(300),
		big.NewInt(400), big.NewInt(500),
	}
	p0 := blobFeePercentile(copyBlobBigs(vals), 0)
	if p0.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("p0 = %s, want 100", p0)
	}
	p100 := blobFeePercentile(copyBlobBigs(vals), 100)
	if p100.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("p100 = %s, want 500", p100)
	}
	p50 := blobFeePercentile(copyBlobBigs(vals), 50)
	if p50.Cmp(big.NewInt(300)) != 0 {
		t.Errorf("p50 = %s, want 300", p50)
	}
}

func TestBlobFeeTracker_DefaultConfig(t *testing.T) {
	cfg := DefaultBlobFeeTrackerConfig()
	if cfg.WindowSize != BlobFeeDefaultWindow {
		t.Errorf("WindowSize = %d, want %d", cfg.WindowSize, BlobFeeDefaultWindow)
	}
	if cfg.MinBlobFee.Cmp(big.NewInt(BlobFeeDefaultFloor)) != 0 {
		t.Errorf("MinBlobFee = %s, want %d", cfg.MinBlobFee, BlobFeeDefaultFloor)
	}
	if cfg.SpikeThresholdPct != BlobFeeSpikeThresholdPct {
		t.Errorf("SpikeThresholdPct = %d, want %d", cfg.SpikeThresholdPct, BlobFeeSpikeThresholdPct)
	}
}

func copyBlobBigs(vals []*big.Int) []*big.Int {
	cp := make([]*big.Int, len(vals))
	for i, v := range vals {
		cp[i] = new(big.Int).Set(v)
	}
	return cp
}
