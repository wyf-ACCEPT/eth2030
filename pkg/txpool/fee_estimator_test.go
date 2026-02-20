package txpool

import (
	"math/big"
	"testing"
)

func makeBlockFeeData(blockNum uint64, baseFee int64, gasPrices []int64, tips []int64, blobBaseFee int64) BlockFeeData {
	data := BlockFeeData{
		BlockNumber: blockNum,
		BaseFee:     big.NewInt(baseFee),
	}
	for _, p := range gasPrices {
		data.GasPrices = append(data.GasPrices, big.NewInt(p))
	}
	for _, t := range tips {
		data.Tips = append(data.Tips, big.NewInt(t))
	}
	if blobBaseFee > 0 {
		data.BlobBaseFee = big.NewInt(blobBaseFee)
	}
	return data
}

func TestFeeEstimatorSuggestGasPriceEmpty(t *testing.T) {
	fe := NewFeeEstimator(DefaultFeeEstimatorConfig())

	price := fe.SuggestGasPrice()
	if price.Cmp(big.NewInt(DefaultMinSuggestedGasPrice)) != 0 {
		t.Fatalf("expected min gas price %d, got %s", DefaultMinSuggestedGasPrice, price)
	}
}

func TestFeeEstimatorSuggestGasPrice(t *testing.T) {
	fe := NewFeeEstimator(DefaultFeeEstimatorConfig())

	// Add blocks with increasing gas prices.
	for i := uint64(0); i < 5; i++ {
		prices := []int64{
			int64(10_000_000_000 + i*1_000_000_000),
			int64(12_000_000_000 + i*1_000_000_000),
			int64(15_000_000_000 + i*1_000_000_000),
		}
		fe.AddBlock(makeBlockFeeData(i, 8_000_000_000, prices, nil, 0))
	}

	price := fe.SuggestGasPrice()
	// Should be above minimum.
	if price.Cmp(big.NewInt(DefaultMinSuggestedGasPrice)) <= 0 {
		t.Fatalf("expected price > min, got %s", price)
	}
}

func TestFeeEstimatorSuggestGasTipCapEmpty(t *testing.T) {
	fe := NewFeeEstimator(DefaultFeeEstimatorConfig())

	tip := fe.SuggestGasTipCap()
	if tip.Cmp(big.NewInt(DefaultMinSuggestedTip)) != 0 {
		t.Fatalf("expected min tip %d, got %s", DefaultMinSuggestedTip, tip)
	}
}

func TestFeeEstimatorSuggestGasTipCap(t *testing.T) {
	fe := NewFeeEstimator(DefaultFeeEstimatorConfig())

	// Add blocks with tip data.
	for i := uint64(0); i < 5; i++ {
		tips := []int64{
			int64(2_000_000_000 + i*500_000_000),
			int64(3_000_000_000 + i*500_000_000),
		}
		fe.AddBlock(makeBlockFeeData(i, 8_000_000_000, nil, tips, 0))
	}

	tip := fe.SuggestGasTipCap()
	if tip.Cmp(big.NewInt(DefaultMinSuggestedTip)) < 0 {
		t.Fatalf("expected tip >= min, got %s", tip)
	}
}

func TestFeeEstimatorEstimateBlobFeeEmpty(t *testing.T) {
	fe := NewFeeEstimator(DefaultFeeEstimatorConfig())

	blobFee := fe.EstimateBlobFee()
	if blobFee.Cmp(big.NewInt(MinBlobBaseFee)) != 0 {
		t.Fatalf("expected min blob base fee %d, got %s", MinBlobBaseFee, blobFee)
	}
}

func TestFeeEstimatorEstimateBlobFee(t *testing.T) {
	fe := NewFeeEstimator(DefaultFeeEstimatorConfig())

	// Add a block with blob base fee.
	fe.AddBlock(makeBlockFeeData(1, 8_000_000_000, nil, nil, 1000))

	blobFee := fe.EstimateBlobFee()
	// Should be > 1000 due to the 12.5% buffer.
	if blobFee.Cmp(big.NewInt(1000)) <= 0 {
		t.Fatalf("expected blob fee > 1000, got %s", blobFee)
	}
	// And should be <= 1000 * 1.125 = 1125.
	if blobFee.Cmp(big.NewInt(1125)) > 0 {
		t.Fatalf("expected blob fee <= 1125, got %s", blobFee)
	}
}

func TestFeeEstimatorSuggestGasFeeCap(t *testing.T) {
	fe := NewFeeEstimator(DefaultFeeEstimatorConfig())

	baseFee := int64(10_000_000_000)
	fe.AddBlock(makeBlockFeeData(1, baseFee, nil, []int64{2_000_000_000}, 0))

	feeCap := fe.SuggestGasFeeCap()
	// feeCap should be >= baseFee * 2 + tip.
	minExpected := new(big.Int).Mul(big.NewInt(baseFee), big.NewInt(2))
	if feeCap.Cmp(minExpected) < 0 {
		t.Fatalf("expected feeCap >= %s, got %s", minExpected, feeCap)
	}
}

func TestFeeEstimatorHistoryCircularBuffer(t *testing.T) {
	config := DefaultFeeEstimatorConfig()
	config.HistorySize = 3
	fe := NewFeeEstimator(config)

	// Add 5 blocks to a buffer of size 3.
	for i := uint64(0); i < 5; i++ {
		prices := []int64{int64(1_000_000_000 * (i + 1))}
		fe.AddBlock(makeBlockFeeData(i, 8_000_000_000, prices, nil, 0))
	}

	if fe.HistoryLen() != 3 {
		t.Fatalf("expected history len 3, got %d", fe.HistoryLen())
	}
}

func TestFeeEstimatorLatestBaseFee(t *testing.T) {
	fe := NewFeeEstimator(DefaultFeeEstimatorConfig())

	if fe.LatestBaseFee() != nil {
		t.Fatal("expected nil base fee before any blocks")
	}

	fe.AddBlock(makeBlockFeeData(1, 12_000_000_000, nil, nil, 0))
	baseFee := fe.LatestBaseFee()
	if baseFee == nil {
		t.Fatal("expected non-nil base fee after adding block")
	}
	if baseFee.Cmp(big.NewInt(12_000_000_000)) != 0 {
		t.Fatalf("expected 12000000000, got %s", baseFee)
	}
}

func TestFeeEstimatorByPercentile(t *testing.T) {
	fe := NewFeeEstimator(DefaultFeeEstimatorConfig())

	// Add blocks with a range of gas prices.
	for i := uint64(0); i < 10; i++ {
		var prices []int64
		for j := int64(1); j <= 10; j++ {
			prices = append(prices, j*1_000_000_000+int64(i)*100_000_000)
		}
		fe.AddBlock(makeBlockFeeData(i, 8_000_000_000, prices, nil, 0))
	}

	low, med, high := fe.FeeEstByPercentile()

	// low <= med <= high.
	if low.Cmp(med) > 0 {
		t.Fatalf("low (%s) > med (%s)", low, med)
	}
	if med.Cmp(high) > 0 {
		t.Fatalf("med (%s) > high (%s)", med, high)
	}
}

func TestPercentile(t *testing.T) {
	values := []*big.Int{
		big.NewInt(10),
		big.NewInt(20),
		big.NewInt(30),
		big.NewInt(40),
		big.NewInt(50),
	}

	p0 := percentile(values, 0)
	if p0.Cmp(big.NewInt(10)) != 0 {
		t.Fatalf("p0: expected 10, got %s", p0)
	}

	p50 := percentile(values, 50)
	if p50.Cmp(big.NewInt(30)) != 0 {
		t.Fatalf("p50: expected 30, got %s", p50)
	}

	p100 := percentile(values, 100)
	if p100.Cmp(big.NewInt(50)) != 0 {
		t.Fatalf("p100: expected 50, got %s", p100)
	}
}

func TestPercentileEmpty(t *testing.T) {
	result := percentile(nil, 50)
	if result.Sign() != 0 {
		t.Fatalf("expected 0 for empty, got %s", result)
	}
}

func TestFeeEstimatorMinGasPriceFloor(t *testing.T) {
	config := DefaultFeeEstimatorConfig()
	config.MinGasPrice = big.NewInt(5_000_000_000) // 5 Gwei floor
	fe := NewFeeEstimator(config)

	// Add a block with very low gas prices.
	fe.AddBlock(makeBlockFeeData(1, 1_000_000_000, []int64{100, 200, 300}, nil, 0))

	price := fe.SuggestGasPrice()
	if price.Cmp(big.NewInt(5_000_000_000)) != 0 {
		t.Fatalf("expected floor of 5 Gwei, got %s", price)
	}
}

func TestFeeEstimatorMultipleTipUpdates(t *testing.T) {
	fe := NewFeeEstimator(DefaultFeeEstimatorConfig())

	// Simulate several blocks with growing tips.
	for i := uint64(1); i <= 10; i++ {
		tips := []int64{int64(i) * 1_000_000_000}
		fe.AddBlock(makeBlockFeeData(i, 8_000_000_000, nil, tips, 0))
	}

	tip := fe.SuggestGasTipCap()
	// Median of 1G..10G tips should be around 5G.
	if tip.Cmp(big.NewInt(1_000_000_000)) < 0 {
		t.Fatalf("tip too low: %s", tip)
	}
}
