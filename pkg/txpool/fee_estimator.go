// fee_estimator.go provides gas price and fee estimation based on recent
// block history. FeeEstimator tracks recent block gas prices and suggests
// appropriate fees for new transactions, including EIP-1559 priority fees
// and EIP-4844 blob base fees.
package txpool

import (
	"math/big"
	"sort"
	"sync"
)

// Fee estimator constants.
const (
	// FeeHistorySize is the number of recent blocks tracked for fee estimation.
	FeeHistorySize = 20

	// DefaultSuggestedTipMultiplier scales the median tip for the suggestion.
	DefaultSuggestedTipMultiplier = 1

	// DefaultMinSuggestedGasPrice is the floor for suggested gas prices (1 Gwei).
	DefaultMinSuggestedGasPrice = 1_000_000_000

	// DefaultMinSuggestedTip is the minimum suggested priority fee (1 Gwei).
	DefaultMinSuggestedTip = 1_000_000_000

	// FeeEstPercentileLow is the low percentile for conservative estimates.
	FeeEstPercentileLow = 10

	// FeeEstPercentileMed is the medium percentile for standard estimates.
	FeeEstPercentileMed = 50

	// FeeEstPercentileHigh is the high percentile for fast estimates.
	FeeEstPercentileHigh = 90
)

// BlockFeeData holds fee information from a single block used for estimation.
type BlockFeeData struct {
	BlockNumber uint64
	BaseFee     *big.Int   // EIP-1559 base fee of the block
	GasPrices   []*big.Int // effective gas prices of transactions in the block
	Tips        []*big.Int // effective priority fees (tips) of transactions
	BlobBaseFee *big.Int   // blob base fee (nil if pre-4844)
}

// FeeEstimatorConfig configures the FeeEstimator.
type FeeEstimatorConfig struct {
	HistorySize     int      // number of blocks to track
	MinGasPrice     *big.Int // minimum suggested gas price
	MinTip          *big.Int // minimum suggested priority fee
	TipMultiplier   int      // multiplier for median tip suggestion
}

// DefaultFeeEstimatorConfig returns sensible defaults.
func DefaultFeeEstimatorConfig() FeeEstimatorConfig {
	return FeeEstimatorConfig{
		HistorySize:   FeeHistorySize,
		MinGasPrice:   big.NewInt(DefaultMinSuggestedGasPrice),
		MinTip:        big.NewInt(DefaultMinSuggestedTip),
		TipMultiplier: DefaultSuggestedTipMultiplier,
	}
}

// FeeEstimator tracks recent block gas prices to suggest fees for new
// transactions. It maintains a sliding window of recent block fee data
// and computes percentile-based recommendations.
type FeeEstimator struct {
	config  FeeEstimatorConfig

	mu      sync.RWMutex
	history []BlockFeeData // circular buffer of recent blocks
	head    int            // index of next write position
	count   int            // number of valid entries

	// Latest known values.
	latestBaseFee     *big.Int
	latestBlobBaseFee *big.Int
}

// NewFeeEstimator creates a new FeeEstimator with the given configuration.
func NewFeeEstimator(config FeeEstimatorConfig) *FeeEstimator {
	if config.HistorySize <= 0 {
		config.HistorySize = FeeHistorySize
	}
	if config.MinGasPrice == nil {
		config.MinGasPrice = big.NewInt(DefaultMinSuggestedGasPrice)
	}
	if config.MinTip == nil {
		config.MinTip = big.NewInt(DefaultMinSuggestedTip)
	}
	if config.TipMultiplier <= 0 {
		config.TipMultiplier = DefaultSuggestedTipMultiplier
	}
	return &FeeEstimator{
		config:  config,
		history: make([]BlockFeeData, config.HistorySize),
	}
}

// AddBlock records fee data from a newly processed block. Old entries
// beyond HistorySize are overwritten in a circular fashion.
func (fe *FeeEstimator) AddBlock(data BlockFeeData) {
	fe.mu.Lock()
	defer fe.mu.Unlock()

	fe.history[fe.head] = data
	fe.head = (fe.head + 1) % fe.config.HistorySize
	if fe.count < fe.config.HistorySize {
		fe.count++
	}

	// Update latest known values.
	if data.BaseFee != nil {
		fe.latestBaseFee = new(big.Int).Set(data.BaseFee)
	}
	if data.BlobBaseFee != nil {
		fe.latestBlobBaseFee = new(big.Int).Set(data.BlobBaseFee)
	}
}

// SuggestGasPrice returns a recommended gas price for legacy transactions
// based on the median gas price of recent blocks. The result is clamped
// to the configured minimum.
func (fe *FeeEstimator) SuggestGasPrice() *big.Int {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	prices := fe.collectGasPrices()
	if len(prices) == 0 {
		return new(big.Int).Set(fe.config.MinGasPrice)
	}

	median := percentile(prices, FeeEstPercentileMed)
	if median.Cmp(fe.config.MinGasPrice) < 0 {
		return new(big.Int).Set(fe.config.MinGasPrice)
	}
	return median
}

// SuggestGasTipCap returns a recommended max priority fee per gas (tip)
// for EIP-1559 transactions. It uses the median tip from recent blocks
// scaled by the configured multiplier.
func (fe *FeeEstimator) SuggestGasTipCap() *big.Int {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	tips := fe.collectTips()
	if len(tips) == 0 {
		return new(big.Int).Set(fe.config.MinTip)
	}

	median := percentile(tips, FeeEstPercentileMed)
	suggested := new(big.Int).Mul(median, big.NewInt(int64(fe.config.TipMultiplier)))
	if suggested.Cmp(fe.config.MinTip) < 0 {
		return new(big.Int).Set(fe.config.MinTip)
	}
	return suggested
}

// EstimateBlobFee returns the estimated blob base fee for the next block
// based on the latest observed blob base fee. If no blob fee data is
// available, returns the minimum blob base fee.
func (fe *FeeEstimator) EstimateBlobFee() *big.Int {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	if fe.latestBlobBaseFee != nil {
		// Suggest slightly above current to account for possible increase.
		// Add 12.5% buffer (matching the max base fee increase per block).
		suggested := new(big.Int).Set(fe.latestBlobBaseFee)
		buffer := new(big.Int).Div(suggested, big.NewInt(8))
		suggested.Add(suggested, buffer)
		return suggested
	}
	return big.NewInt(MinBlobBaseFee)
}

// SuggestGasFeeCap returns a recommended max fee per gas for EIP-1559
// transactions. It is computed as: baseFee * 2 + suggestedTip. This
// provides headroom for base fee increases over the next few blocks.
func (fe *FeeEstimator) SuggestGasFeeCap() *big.Int {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	baseFee := fe.latestBaseFee
	if baseFee == nil {
		baseFee = fe.config.MinGasPrice
	}

	tips := fe.collectTips()
	var tip *big.Int
	if len(tips) == 0 {
		tip = new(big.Int).Set(fe.config.MinTip)
	} else {
		tip = percentile(tips, FeeEstPercentileMed)
		if tip.Cmp(fe.config.MinTip) < 0 {
			tip = new(big.Int).Set(fe.config.MinTip)
		}
	}

	// feeCap = baseFee * 2 + tip
	feeCap := new(big.Int).Mul(baseFee, big.NewInt(2))
	feeCap.Add(feeCap, tip)
	return feeCap
}

// FeeEstByPercentile returns gas price estimates at the low, medium,
// and high percentiles for more nuanced fee suggestions.
func (fe *FeeEstimator) FeeEstByPercentile() (low, med, high *big.Int) {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	prices := fe.collectGasPrices()
	if len(prices) == 0 {
		min := new(big.Int).Set(fe.config.MinGasPrice)
		return min, min, min
	}

	low = percentile(prices, FeeEstPercentileLow)
	med = percentile(prices, FeeEstPercentileMed)
	high = percentile(prices, FeeEstPercentileHigh)

	if low.Cmp(fe.config.MinGasPrice) < 0 {
		low = new(big.Int).Set(fe.config.MinGasPrice)
	}
	if med.Cmp(fe.config.MinGasPrice) < 0 {
		med = new(big.Int).Set(fe.config.MinGasPrice)
	}
	if high.Cmp(fe.config.MinGasPrice) < 0 {
		high = new(big.Int).Set(fe.config.MinGasPrice)
	}
	return low, med, high
}

// LatestBaseFee returns the latest observed base fee. Returns nil if
// no blocks have been added yet.
func (fe *FeeEstimator) LatestBaseFee() *big.Int {
	fe.mu.RLock()
	defer fe.mu.RUnlock()
	if fe.latestBaseFee == nil {
		return nil
	}
	return new(big.Int).Set(fe.latestBaseFee)
}

// HistoryLen returns the number of blocks currently in the history.
func (fe *FeeEstimator) HistoryLen() int {
	fe.mu.RLock()
	defer fe.mu.RUnlock()
	return fe.count
}

// collectGasPrices gathers all gas prices from the history. Caller must hold fe.mu.
func (fe *FeeEstimator) collectGasPrices() []*big.Int {
	var prices []*big.Int
	for i := 0; i < fe.count; i++ {
		idx := (fe.head - fe.count + i + fe.config.HistorySize) % fe.config.HistorySize
		for _, p := range fe.history[idx].GasPrices {
			if p != nil && p.Sign() > 0 {
				prices = append(prices, new(big.Int).Set(p))
			}
		}
	}
	return prices
}

// collectTips gathers all priority fees from the history. Caller must hold fe.mu.
func (fe *FeeEstimator) collectTips() []*big.Int {
	var tips []*big.Int
	for i := 0; i < fe.count; i++ {
		idx := (fe.head - fe.count + i + fe.config.HistorySize) % fe.config.HistorySize
		for _, t := range fe.history[idx].Tips {
			if t != nil && t.Sign() > 0 {
				tips = append(tips, new(big.Int).Set(t))
			}
		}
	}
	return tips
}

// percentile computes the p-th percentile (0-100) of a slice of big.Int values.
// The input slice is sorted in place.
func percentile(values []*big.Int, p int) *big.Int {
	if len(values) == 0 {
		return new(big.Int)
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].Cmp(values[j]) < 0
	})
	if p <= 0 {
		return new(big.Int).Set(values[0])
	}
	if p >= 100 {
		return new(big.Int).Set(values[len(values)-1])
	}
	idx := (len(values) - 1) * p / 100
	return new(big.Int).Set(values[idx])
}
