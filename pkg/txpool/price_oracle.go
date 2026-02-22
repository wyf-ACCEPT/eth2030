// price_oracle.go provides a gas price oracle that tracks recent block base
// fees and computes percentile-based fee suggestions for different urgency
// levels. It supports EIP-1559 priority fee estimation, fee history windows,
// and max fee recommendations for slow/medium/fast transactions.
package txpool

import (
	"math/big"
	"sort"
	"sync"
)

// PriceOracle constants.
const (
	// PriceOracleDefaultWindow is the default number of blocks tracked.
	PriceOracleDefaultWindow = 50

	// Percentile boundaries for urgency levels.
	PriceOracleSlowPercentile   = 10
	PriceOracleMediumPercentile = 50
	PriceOracleFastPercentile   = 90

	// PriceOracleMinBaseFee is the minimum base fee floor (1 Gwei).
	PriceOracleMinBaseFee = 1_000_000_000

	// PriceOracleMinTip is the minimum priority fee floor (1 Gwei).
	PriceOracleMinTip = 1_000_000_000

	// PriceOracleBaseFeeMargin is the base fee headroom multiplier numerator/denom.
	// A margin of 125/100 means 25% headroom above current base fee.
	PriceOracleBaseFeeMarginNum   = 125
	PriceOracleBaseFeeMarginDenom = 100
)

// PriceOracleConfig configures the PriceOracle.
type PriceOracleConfig struct {
	WindowSize    int      // Number of blocks to maintain in the history.
	MinBaseFee    *big.Int // Minimum base fee floor.
	MinTip        *big.Int // Minimum priority fee floor.
	IgnorePrice   *big.Int // Ignore txs below this price (anti-spam filter).
	MaxHeaderSize int      // Max headers stored (caps memory usage).
}

// DefaultPriceOracleConfig returns sensible defaults.
func DefaultPriceOracleConfig() PriceOracleConfig {
	return PriceOracleConfig{
		WindowSize:    PriceOracleDefaultWindow,
		MinBaseFee:    big.NewInt(PriceOracleMinBaseFee),
		MinTip:        big.NewInt(PriceOracleMinTip),
		IgnorePrice:   big.NewInt(0),
		MaxHeaderSize: PriceOracleDefaultWindow * 2,
	}
}

// BlockFeeRecord holds fee data from a single block for the oracle.
type BlockFeeRecord struct {
	Number    uint64     // Block number.
	BaseFee   *big.Int   // Block base fee (EIP-1559).
	GasUsed   uint64     // Total gas used in the block.
	GasLimit  uint64     // Block gas limit.
	Tips      []*big.Int // Priority fees (tips) from transactions in the block.
	GasPrices []*big.Int // Effective gas prices from transactions.
}

// FeeRecommendation holds fee suggestions for different urgency levels.
type FeeRecommendation struct {
	SlowTip     *big.Int // Conservative priority fee (low percentile).
	MediumTip   *big.Int // Standard priority fee (median).
	FastTip     *big.Int // Aggressive priority fee (high percentile).
	SlowFee     *big.Int // Suggested maxFeePerGas for slow.
	MediumFee   *big.Int // Suggested maxFeePerGas for medium.
	FastFee     *big.Int // Suggested maxFeePerGas for fast.
	BaseFee     *big.Int // Latest known base fee.
	NextBaseFee *big.Int // Estimated next block base fee.
}

// FeeHistoryEntry holds fee data for a single block in the fee history.
type FeeHistoryEntry struct {
	Number     uint64
	BaseFee    *big.Int
	GasUsedPct float64  // Gas used as percentage of gas limit.
	Reward10th *big.Int // 10th percentile tip.
	Reward50th *big.Int // 50th percentile tip (median).
	Reward90th *big.Int // 90th percentile tip.
}

// PriceOracle tracks recent block fees and provides gas price recommendations.
// All methods are safe for concurrent use.
type PriceOracle struct {
	mu      sync.RWMutex
	config  PriceOracleConfig
	records []BlockFeeRecord // Circular buffer.
	head    int              // Next write position.
	count   int              // Valid entries.
}

// NewPriceOracle creates a PriceOracle with the given config.
func NewPriceOracle(config PriceOracleConfig) *PriceOracle {
	if config.WindowSize <= 0 {
		config.WindowSize = PriceOracleDefaultWindow
	}
	if config.MinBaseFee == nil {
		config.MinBaseFee = big.NewInt(PriceOracleMinBaseFee)
	}
	if config.MinTip == nil {
		config.MinTip = big.NewInt(PriceOracleMinTip)
	}
	if config.IgnorePrice == nil {
		config.IgnorePrice = big.NewInt(0)
	}
	return &PriceOracle{
		config:  config,
		records: make([]BlockFeeRecord, config.WindowSize),
	}
}

// AddBlock records fee data from a new block.
func (po *PriceOracle) AddBlock(rec BlockFeeRecord) {
	po.mu.Lock()
	defer po.mu.Unlock()

	po.records[po.head] = rec
	po.head = (po.head + 1) % po.config.WindowSize
	if po.count < po.config.WindowSize {
		po.count++
	}
}

// SuggestTipCap returns a recommended priority fee (tip) using the median
// of tips from recent blocks.
func (po *PriceOracle) SuggestTipCap() *big.Int {
	po.mu.RLock()
	defer po.mu.RUnlock()

	tips := po.collectTipsLocked()
	if len(tips) == 0 {
		return new(big.Int).Set(po.config.MinTip)
	}
	med := bigPercentile(tips, PriceOracleMediumPercentile)
	if med.Cmp(po.config.MinTip) < 0 {
		return new(big.Int).Set(po.config.MinTip)
	}
	return med
}

// SuggestGasPrice returns a recommended gas price for legacy transactions
// using the median effective gas price from recent blocks.
func (po *PriceOracle) SuggestGasPrice() *big.Int {
	po.mu.RLock()
	defer po.mu.RUnlock()

	prices := po.collectGasPricesLocked()
	if len(prices) == 0 {
		return new(big.Int).Set(po.config.MinBaseFee)
	}
	med := bigPercentile(prices, PriceOracleMediumPercentile)
	if med.Cmp(po.config.MinBaseFee) < 0 {
		return new(big.Int).Set(po.config.MinBaseFee)
	}
	return med
}

// LatestBaseFee returns the most recently observed base fee. Returns nil if
// no blocks have been recorded.
func (po *PriceOracle) LatestBaseFee() *big.Int {
	po.mu.RLock()
	defer po.mu.RUnlock()

	if po.count == 0 {
		return nil
	}
	idx := (po.head - 1 + po.config.WindowSize) % po.config.WindowSize
	bf := po.records[idx].BaseFee
	if bf == nil {
		return nil
	}
	return new(big.Int).Set(bf)
}

// EstimateNextBaseFee estimates the next block's base fee based on the
// latest block's gas usage ratio. If the latest block was more than 50%
// full, base fee increases; otherwise it decreases.
func (po *PriceOracle) EstimateNextBaseFee() *big.Int {
	po.mu.RLock()
	defer po.mu.RUnlock()

	if po.count == 0 {
		return new(big.Int).Set(po.config.MinBaseFee)
	}
	idx := (po.head - 1 + po.config.WindowSize) % po.config.WindowSize
	rec := po.records[idx]
	if rec.BaseFee == nil {
		return new(big.Int).Set(po.config.MinBaseFee)
	}

	baseFee := new(big.Int).Set(rec.BaseFee)
	if rec.GasLimit == 0 {
		return baseFee
	}

	target := rec.GasLimit / 2
	if rec.GasUsed > target {
		// Increase: baseFee + baseFee * (gasUsed - target) / target / 8
		delta := new(big.Int).SetUint64(rec.GasUsed - target)
		delta.Mul(delta, baseFee)
		delta.Div(delta, new(big.Int).SetUint64(target))
		delta.Div(delta, big.NewInt(8))
		if delta.Sign() == 0 {
			delta.SetInt64(1)
		}
		baseFee.Add(baseFee, delta)
	} else if rec.GasUsed < target {
		// Decrease: baseFee - baseFee * (target - gasUsed) / target / 8
		delta := new(big.Int).SetUint64(target - rec.GasUsed)
		delta.Mul(delta, baseFee)
		delta.Div(delta, new(big.Int).SetUint64(target))
		delta.Div(delta, big.NewInt(8))
		baseFee.Sub(baseFee, delta)
	}

	if baseFee.Cmp(po.config.MinBaseFee) < 0 {
		baseFee.Set(po.config.MinBaseFee)
	}
	return baseFee
}

// Recommend returns fee recommendations for slow, medium, and fast urgency.
func (po *PriceOracle) Recommend() FeeRecommendation {
	po.mu.RLock()
	defer po.mu.RUnlock()

	tips := po.collectTipsLocked()
	baseFee := po.latestBaseFeeLocked()
	nextBaseFee := po.estimateNextBaseFeeLocked()

	var slowTip, medTip, fastTip *big.Int
	if len(tips) == 0 {
		slowTip = new(big.Int).Set(po.config.MinTip)
		medTip = new(big.Int).Set(po.config.MinTip)
		fastTip = new(big.Int).Set(po.config.MinTip)
	} else {
		slowTip = bigPercentile(tips, PriceOracleSlowPercentile)
		medTip = bigPercentile(tips, PriceOracleMediumPercentile)
		fastTip = bigPercentile(tips, PriceOracleFastPercentile)
	}

	// Enforce minimums.
	if slowTip.Cmp(po.config.MinTip) < 0 {
		slowTip = new(big.Int).Set(po.config.MinTip)
	}
	if medTip.Cmp(po.config.MinTip) < 0 {
		medTip = new(big.Int).Set(po.config.MinTip)
	}
	if fastTip.Cmp(po.config.MinTip) < 0 {
		fastTip = new(big.Int).Set(po.config.MinTip)
	}

	// Compute maxFeePerGas suggestions:
	// slow  = nextBaseFee + slowTip
	// medium = nextBaseFee * 1.25 + medTip
	// fast   = nextBaseFee * 1.5 + fastTip
	slowFee := new(big.Int).Add(nextBaseFee, slowTip)

	medBase := new(big.Int).Mul(nextBaseFee, big.NewInt(PriceOracleBaseFeeMarginNum))
	medBase.Div(medBase, big.NewInt(PriceOracleBaseFeeMarginDenom))
	medFee := new(big.Int).Add(medBase, medTip)

	fastBase := new(big.Int).Mul(nextBaseFee, big.NewInt(150))
	fastBase.Div(fastBase, big.NewInt(100))
	fastFee := new(big.Int).Add(fastBase, fastTip)

	return FeeRecommendation{
		SlowTip:     slowTip,
		MediumTip:   medTip,
		FastTip:     fastTip,
		SlowFee:     slowFee,
		MediumFee:   medFee,
		FastFee:     fastFee,
		BaseFee:     baseFee,
		NextBaseFee: nextBaseFee,
	}
}

// FeeHistory returns per-block fee information for the last n blocks.
func (po *PriceOracle) FeeHistory(n int) []FeeHistoryEntry {
	po.mu.RLock()
	defer po.mu.RUnlock()

	if n > po.count {
		n = po.count
	}
	result := make([]FeeHistoryEntry, n)
	for i := 0; i < n; i++ {
		idx := (po.head - n + i + po.config.WindowSize) % po.config.WindowSize
		rec := po.records[idx]

		var gasUsedPct float64
		if rec.GasLimit > 0 {
			gasUsedPct = float64(rec.GasUsed) / float64(rec.GasLimit) * 100.0
		}

		var r10, r50, r90 *big.Int
		if len(rec.Tips) > 0 {
			tipsCopy := make([]*big.Int, len(rec.Tips))
			for j, t := range rec.Tips {
				tipsCopy[j] = new(big.Int).Set(t)
			}
			r10 = bigPercentile(tipsCopy, 10)
			tipsCopy2 := make([]*big.Int, len(rec.Tips))
			for j, t := range rec.Tips {
				tipsCopy2[j] = new(big.Int).Set(t)
			}
			r50 = bigPercentile(tipsCopy2, 50)
			tipsCopy3 := make([]*big.Int, len(rec.Tips))
			for j, t := range rec.Tips {
				tipsCopy3[j] = new(big.Int).Set(t)
			}
			r90 = bigPercentile(tipsCopy3, 90)
		} else {
			r10 = new(big.Int)
			r50 = new(big.Int)
			r90 = new(big.Int)
		}

		var baseFee *big.Int
		if rec.BaseFee != nil {
			baseFee = new(big.Int).Set(rec.BaseFee)
		} else {
			baseFee = new(big.Int)
		}

		result[i] = FeeHistoryEntry{
			Number:     rec.Number,
			BaseFee:    baseFee,
			GasUsedPct: gasUsedPct,
			Reward10th: r10,
			Reward50th: r50,
			Reward90th: r90,
		}
	}
	return result
}

// BlockCount returns the number of blocks currently in the oracle window.
func (po *PriceOracle) BlockCount() int {
	po.mu.RLock()
	defer po.mu.RUnlock()
	return po.count
}

// BaseFeeHistory returns base fees for the last n blocks, oldest first.
func (po *PriceOracle) BaseFeeHistory(n int) []*big.Int {
	po.mu.RLock()
	defer po.mu.RUnlock()

	if n > po.count {
		n = po.count
	}
	result := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		idx := (po.head - n + i + po.config.WindowSize) % po.config.WindowSize
		if po.records[idx].BaseFee != nil {
			result[i] = new(big.Int).Set(po.records[idx].BaseFee)
		} else {
			result[i] = new(big.Int)
		}
	}
	return result
}

// AverageBaseFee returns the mean base fee across the tracked window.
func (po *PriceOracle) AverageBaseFee() *big.Int {
	po.mu.RLock()
	defer po.mu.RUnlock()

	if po.count == 0 {
		return new(big.Int).Set(po.config.MinBaseFee)
	}
	sum := new(big.Int)
	n := 0
	for i := 0; i < po.count; i++ {
		idx := (po.head - po.count + i + po.config.WindowSize) % po.config.WindowSize
		if po.records[idx].BaseFee != nil {
			sum.Add(sum, po.records[idx].BaseFee)
			n++
		}
	}
	if n == 0 {
		return new(big.Int).Set(po.config.MinBaseFee)
	}
	return sum.Div(sum, big.NewInt(int64(n)))
}

// --- internal helpers (caller must hold lock) ---

func (po *PriceOracle) collectTipsLocked() []*big.Int {
	var tips []*big.Int
	for i := 0; i < po.count; i++ {
		idx := (po.head - po.count + i + po.config.WindowSize) % po.config.WindowSize
		for _, t := range po.records[idx].Tips {
			if t != nil && t.Sign() > 0 {
				if po.config.IgnorePrice.Sign() > 0 && t.Cmp(po.config.IgnorePrice) < 0 {
					continue
				}
				tips = append(tips, new(big.Int).Set(t))
			}
		}
	}
	return tips
}

func (po *PriceOracle) collectGasPricesLocked() []*big.Int {
	var prices []*big.Int
	for i := 0; i < po.count; i++ {
		idx := (po.head - po.count + i + po.config.WindowSize) % po.config.WindowSize
		for _, p := range po.records[idx].GasPrices {
			if p != nil && p.Sign() > 0 {
				if po.config.IgnorePrice.Sign() > 0 && p.Cmp(po.config.IgnorePrice) < 0 {
					continue
				}
				prices = append(prices, new(big.Int).Set(p))
			}
		}
	}
	return prices
}

func (po *PriceOracle) latestBaseFeeLocked() *big.Int {
	if po.count == 0 {
		return new(big.Int).Set(po.config.MinBaseFee)
	}
	idx := (po.head - 1 + po.config.WindowSize) % po.config.WindowSize
	if po.records[idx].BaseFee != nil {
		return new(big.Int).Set(po.records[idx].BaseFee)
	}
	return new(big.Int).Set(po.config.MinBaseFee)
}

func (po *PriceOracle) estimateNextBaseFeeLocked() *big.Int {
	if po.count == 0 {
		return new(big.Int).Set(po.config.MinBaseFee)
	}
	idx := (po.head - 1 + po.config.WindowSize) % po.config.WindowSize
	rec := po.records[idx]
	if rec.BaseFee == nil {
		return new(big.Int).Set(po.config.MinBaseFee)
	}
	baseFee := new(big.Int).Set(rec.BaseFee)
	if rec.GasLimit == 0 {
		return baseFee
	}
	target := rec.GasLimit / 2
	if rec.GasUsed > target {
		delta := new(big.Int).SetUint64(rec.GasUsed - target)
		delta.Mul(delta, baseFee)
		delta.Div(delta, new(big.Int).SetUint64(target))
		delta.Div(delta, big.NewInt(8))
		if delta.Sign() == 0 {
			delta.SetInt64(1)
		}
		baseFee.Add(baseFee, delta)
	} else if rec.GasUsed < target {
		delta := new(big.Int).SetUint64(target - rec.GasUsed)
		delta.Mul(delta, baseFee)
		delta.Div(delta, new(big.Int).SetUint64(target))
		delta.Div(delta, big.NewInt(8))
		baseFee.Sub(baseFee, delta)
	}
	if baseFee.Cmp(po.config.MinBaseFee) < 0 {
		baseFee.Set(po.config.MinBaseFee)
	}
	return baseFee
}

// bigPercentile computes the p-th percentile (0-100) of a slice of big.Int.
// The slice is sorted in place.
func bigPercentile(vals []*big.Int, p int) *big.Int {
	if len(vals) == 0 {
		return new(big.Int)
	}
	sort.Slice(vals, func(i, j int) bool {
		return vals[i].Cmp(vals[j]) < 0
	})
	if p <= 0 {
		return new(big.Int).Set(vals[0])
	}
	if p >= 100 {
		return new(big.Int).Set(vals[len(vals)-1])
	}
	idx := (len(vals) - 1) * p / 100
	return new(big.Int).Set(vals[idx])
}
