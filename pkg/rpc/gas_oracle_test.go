package rpc

import (
	"math/big"
	"sync"
	"testing"
)

func gwei(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1e9))
}

func TestNewGasOracle(t *testing.T) {
	cfg := DefaultGasOracleConfig()
	o := NewGasOracle(cfg)

	if o == nil {
		t.Fatal("NewGasOracle returned nil")
	}
	if bf := o.BaseFee(); bf.Sign() != 0 {
		t.Errorf("initial BaseFee = %s, want 0", bf)
	}
}

func TestNewGasOracleDefaults(t *testing.T) {
	// Zero/nil config values should be replaced with defaults.
	o := NewGasOracle(GasOracleConfig{})

	if bf := o.BaseFee(); bf.Sign() != 0 {
		t.Errorf("initial BaseFee = %s, want 0", bf)
	}

	// Should produce a reasonable suggestion even with no data.
	tip := o.SuggestGasTipCap()
	if tip.Sign() <= 0 {
		t.Errorf("SuggestGasTipCap() = %s, want > 0 (default fallback)", tip)
	}
}

func TestRecordBlockAndBaseFee(t *testing.T) {
	o := NewGasOracle(DefaultGasOracleConfig())

	baseFee := gwei(30)
	o.RecordBlock(100, baseFee, nil)

	got := o.BaseFee()
	if got.Cmp(baseFee) != 0 {
		t.Errorf("BaseFee() = %s, want %s", got, baseFee)
	}

	// Recording another block updates base fee.
	baseFee2 := gwei(35)
	o.RecordBlock(101, baseFee2, nil)

	got = o.BaseFee()
	if got.Cmp(baseFee2) != 0 {
		t.Errorf("BaseFee() = %s, want %s", got, baseFee2)
	}
}

func TestBaseFeeIsolation(t *testing.T) {
	o := NewGasOracle(DefaultGasOracleConfig())

	baseFee := gwei(30)
	o.RecordBlock(100, baseFee, nil)

	// Mutating the returned value should not affect the oracle.
	got := o.BaseFee()
	got.SetInt64(0)

	got2 := o.BaseFee()
	if got2.Cmp(gwei(30)) != 0 {
		t.Errorf("BaseFee() was mutated externally: got %s", got2)
	}
}

func TestSuggestGasTipCap(t *testing.T) {
	cfg := DefaultGasOracleConfig()
	cfg.Percentile = 50 // median
	cfg.Blocks = 5
	o := NewGasOracle(cfg)

	// Record 5 blocks with known tips.
	for i := uint64(0); i < 5; i++ {
		tips := []*big.Int{
			gwei(1),
			gwei(2),
			gwei(3),
			gwei(4),
			gwei(5),
		}
		o.RecordBlock(i, gwei(30), tips)
	}

	tip := o.SuggestGasTipCap()
	// With 25 tips (5 blocks * 5 tips each) sorted, median (50th percentile)
	// index = 25 * 50 / 100 = 12. All blocks have the same distribution so
	// the 13th element (0-indexed 12) is 3 Gwei.
	expected := gwei(3)
	if tip.Cmp(expected) != 0 {
		t.Errorf("SuggestGasTipCap() = %s, want %s", tip, expected)
	}
}

func TestSuggestGasTipCapIgnoresLowFees(t *testing.T) {
	cfg := DefaultGasOracleConfig()
	cfg.IgnorePrice = gwei(2) // ignore tips below 2 Gwei
	cfg.Percentile = 0        // lowest
	cfg.Blocks = 1
	o := NewGasOracle(cfg)

	tips := []*big.Int{
		big.NewInt(1),  // below IgnorePrice, should be filtered
		gwei(5),
		gwei(10),
	}
	o.RecordBlock(1, gwei(30), tips)

	tip := o.SuggestGasTipCap()
	// Only 5 Gwei and 10 Gwei remain after filtering. Percentile 0 -> index 0 -> 5 Gwei.
	if tip.Cmp(gwei(5)) != 0 {
		t.Errorf("SuggestGasTipCap() = %s, want %s", tip, gwei(5))
	}
}

func TestSuggestGasPrice(t *testing.T) {
	cfg := DefaultGasOracleConfig()
	cfg.Percentile = 50
	cfg.Blocks = 1
	o := NewGasOracle(cfg)

	baseFee := gwei(30)
	tips := []*big.Int{gwei(2), gwei(4), gwei(6)}
	o.RecordBlock(1, baseFee, tips)

	price := o.SuggestGasPrice()
	// tipCap: median of [2, 4, 6] Gwei -> index 1 -> 4 Gwei
	// price = baseFee(30) + tip(4) = 34 Gwei
	expected := gwei(34)
	if price.Cmp(expected) != 0 {
		t.Errorf("SuggestGasPrice() = %s, want %s", price, expected)
	}
}

func TestSuggestGasPriceCap(t *testing.T) {
	cfg := DefaultGasOracleConfig()
	cfg.MaxPrice = gwei(10) // very low cap
	cfg.Percentile = 100
	cfg.Blocks = 1
	o := NewGasOracle(cfg)

	o.RecordBlock(1, gwei(30), []*big.Int{gwei(50)})

	price := o.SuggestGasPrice()
	// baseFee(30) + tip(50) = 80 Gwei, but capped to 10 Gwei.
	if price.Cmp(gwei(10)) != 0 {
		t.Errorf("SuggestGasPrice() = %s, want %s (capped)", price, gwei(10))
	}
}

func TestGasOracleMaxPriorityFeePerGas(t *testing.T) {
	cfg := DefaultGasOracleConfig()
	o := NewGasOracle(cfg)

	max := o.MaxPriorityFeePerGas()
	if max.Cmp(cfg.MaxPrice) != 0 {
		t.Errorf("MaxPriorityFeePerGas() = %s, want %s", max, cfg.MaxPrice)
	}

	// Should return a copy.
	max.SetInt64(0)
	max2 := o.MaxPriorityFeePerGas()
	if max2.Cmp(cfg.MaxPrice) != 0 {
		t.Error("MaxPriorityFeePerGas() returned mutable reference")
	}
}

func TestGasOracleFeeHistory(t *testing.T) {
	cfg := DefaultGasOracleConfig()
	cfg.Percentile = 50
	o := NewGasOracle(cfg)

	for i := uint64(1); i <= 5; i++ {
		o.RecordBlock(i, gwei(int64(10+i)), []*big.Int{gwei(int64(i))})
	}

	history := o.FeeHistory(3)
	if len(history) != 3 {
		t.Fatalf("FeeHistory(3) len = %d, want 3", len(history))
	}

	// Should return the 3 most recent blocks.
	if history[0].Number != 3 {
		t.Errorf("history[0].Number = %d, want 3", history[0].Number)
	}
	if history[2].Number != 5 {
		t.Errorf("history[2].Number = %d, want 5", history[2].Number)
	}

	// Check base fee values.
	if history[0].BaseFee.Cmp(gwei(13)) != 0 {
		t.Errorf("history[0].BaseFee = %s, want %s", history[0].BaseFee, gwei(13))
	}
}

func TestFeeHistoryExceedsRecords(t *testing.T) {
	o := NewGasOracle(DefaultGasOracleConfig())

	o.RecordBlock(1, gwei(10), nil)
	o.RecordBlock(2, gwei(20), nil)

	history := o.FeeHistory(10)
	if len(history) != 2 {
		t.Fatalf("FeeHistory(10) len = %d, want 2 (only 2 blocks recorded)", len(history))
	}
}

func TestFeeHistoryZero(t *testing.T) {
	o := NewGasOracle(DefaultGasOracleConfig())

	history := o.FeeHistory(0)
	if history != nil {
		t.Errorf("FeeHistory(0) = %v, want nil", history)
	}
}

func TestEstimateL1DataFee(t *testing.T) {
	o := NewGasOracle(DefaultGasOracleConfig())

	// No blocks recorded -> base fee is 0 -> L1 fee is 0.
	fee := o.EstimateL1DataFee(1000)
	if fee.Sign() != 0 {
		t.Errorf("EstimateL1DataFee() = %s, want 0 (no base fee)", fee)
	}

	// Record a block with known base fee.
	baseFee := gwei(30)
	o.RecordBlock(1, baseFee, nil)

	fee = o.EstimateL1DataFee(1000)
	// Expected: 1000 * 16 * 30e9 = 480_000_000_000_000
	expected := new(big.Int).Mul(big.NewInt(1000*16), baseFee)
	if fee.Cmp(expected) != 0 {
		t.Errorf("EstimateL1DataFee(1000) = %s, want %s", fee, expected)
	}
}

func TestEstimateL1DataFeeZeroSize(t *testing.T) {
	o := NewGasOracle(DefaultGasOracleConfig())
	o.RecordBlock(1, gwei(30), nil)

	fee := o.EstimateL1DataFee(0)
	if fee.Sign() != 0 {
		t.Errorf("EstimateL1DataFee(0) = %s, want 0", fee)
	}
}

func TestMaxHeaderHistoryTrimming(t *testing.T) {
	cfg := DefaultGasOracleConfig()
	cfg.MaxHeaderHistory = 5
	o := NewGasOracle(cfg)

	// Record 10 blocks; only the last 5 should be retained.
	for i := uint64(1); i <= 10; i++ {
		o.RecordBlock(i, gwei(int64(i)), nil)
	}

	history := o.FeeHistory(100)
	if len(history) != 5 {
		t.Fatalf("history len = %d, want 5 (trimmed)", len(history))
	}
	if history[0].Number != 6 {
		t.Errorf("oldest block = %d, want 6", history[0].Number)
	}
	if history[4].Number != 10 {
		t.Errorf("newest block = %d, want 10", history[4].Number)
	}
}

func TestConcurrentAccess(t *testing.T) {
	o := NewGasOracle(DefaultGasOracleConfig())

	var wg sync.WaitGroup

	// Writers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			o.RecordBlock(uint64(n), gwei(int64(10+n)), []*big.Int{gwei(int64(n + 1))})
		}(i)
	}

	// Readers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.BaseFee()
			o.SuggestGasTipCap()
			o.SuggestGasPrice()
			o.FeeHistory(5)
			o.EstimateL1DataFee(100)
			o.MaxPriorityFeePerGas()
		}()
	}

	wg.Wait()
}

func TestSuggestGasTipCapNoTips(t *testing.T) {
	o := NewGasOracle(DefaultGasOracleConfig())

	// Record blocks with no transactions (empty tips).
	o.RecordBlock(1, gwei(30), nil)
	o.RecordBlock(2, gwei(31), []*big.Int{})

	tip := o.SuggestGasTipCap()
	// Should fall back to 1 Gwei default.
	if tip.Cmp(gwei(1)) != 0 {
		t.Errorf("SuggestGasTipCap() = %s, want %s (default fallback)", tip, gwei(1))
	}
}

func TestSuggestGasTipCapHighPercentile(t *testing.T) {
	cfg := DefaultGasOracleConfig()
	cfg.Percentile = 100
	cfg.Blocks = 1
	o := NewGasOracle(cfg)

	tips := []*big.Int{gwei(1), gwei(2), gwei(10)}
	o.RecordBlock(1, gwei(30), tips)

	tip := o.SuggestGasTipCap()
	// 100th percentile -> last element -> 10 Gwei
	if tip.Cmp(gwei(10)) != 0 {
		t.Errorf("SuggestGasTipCap() = %s, want %s", tip, gwei(10))
	}
}

func TestNilBaseFee(t *testing.T) {
	o := NewGasOracle(DefaultGasOracleConfig())

	// Recording with nil base fee should not panic.
	o.RecordBlock(1, nil, []*big.Int{gwei(5)})

	bf := o.BaseFee()
	if bf.Sign() != 0 {
		t.Errorf("BaseFee() = %s, want 0 (nil recorded)", bf)
	}
}

func TestFeeHistoryRewardPercentile(t *testing.T) {
	cfg := DefaultGasOracleConfig()
	cfg.Percentile = 50
	o := NewGasOracle(cfg)

	tips := []*big.Int{gwei(1), gwei(3), gwei(5)}
	o.RecordBlock(1, gwei(30), tips)

	history := o.FeeHistory(1)
	if len(history) != 1 {
		t.Fatalf("FeeHistory len = %d, want 1", len(history))
	}
	// Percentile 50 of [1, 3, 5] -> index 1 -> 3 Gwei
	if history[0].RewardPercentile.Cmp(gwei(3)) != 0 {
		t.Errorf("RewardPercentile = %s, want %s", history[0].RewardPercentile, gwei(3))
	}
}
