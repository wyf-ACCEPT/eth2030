// price_bumper.go implements PriceBumper, a dynamic fee management system
// that suggests optimal gas prices based on recent block history. It
// provides EIP-1559-aware fee suggestions, percentile-based gas price
// estimation, blob fee estimation from excess blob gas, and tiered fee
// recommendations (urgent/fast/standard/slow).
package txpool

import (
	"math/big"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Fee tier constants define the percentile targets for each speed tier.
const (
	TierUrgent   = "urgent"
	TierFast     = "fast"
	TierStandard = "standard"
	TierSlow     = "slow"

	// Default percentile targets for each tier.
	urgentPercentile   = 90
	fastPercentile     = 75
	standardPercentile = 50
	slowPercentile     = 25

	// DefaultFeeHistoryDepth is the number of recent blocks to track.
	DefaultFeeHistoryDepth = 20

	// BumperMinSuggestedTip is the minimum suggested priority fee (1 Gwei).
	BumperMinSuggestedTip = 1_000_000_000

	// DefaultBaseFeeMultiplier scales the base fee in urgent suggestions.
	DefaultBaseFeeMultiplier = 2
)

// BumperConfig configures the PriceBumper behaviour.
type BumperConfig struct {
	// HistoryDepth is the number of recent blocks to use for fee estimation.
	HistoryDepth int

	// MinSuggestedTip is the floor for suggested priority fees in wei.
	MinSuggestedTip *big.Int

	// BaseFeeMultiplier is used for the urgent fee cap: baseFee * multiplier + tip.
	BaseFeeMultiplier int

	// IgnorePrice is the minimum gas price below which transactions are
	// excluded from fee history sampling (filters spam/zero-fee txs).
	IgnorePrice *big.Int
}

// DefaultBumperConfig returns sensible defaults for fee estimation.
func DefaultBumperConfig() BumperConfig {
	return BumperConfig{
		HistoryDepth:      DefaultFeeHistoryDepth,
		MinSuggestedTip:   big.NewInt(BumperMinSuggestedTip),
		BaseFeeMultiplier: DefaultBaseFeeMultiplier,
		IgnorePrice:       big.NewInt(1), // 1 wei minimum
	}
}

// BumperBlockFeeData captures the fee-relevant data from a single block needed
// for gas price estimation.
type BumperBlockFeeData struct {
	BaseFee       *big.Int // EIP-1559 base fee, nil for pre-London
	GasUsedRatio  float64  // gasUsed / gasLimit
	Tips          []*big.Int // effective priority fees of all transactions
	BlobBaseFee   *big.Int // EIP-4844 blob base fee, nil if not applicable
	ExcessBlobGas uint64   // excess blob gas from header
	BlockNumber   uint64
}

// FeeSuggestion holds a complete fee recommendation for a transaction.
type FeeSuggestion struct {
	// MaxFeePerGas is the suggested maxFeePerGas (EIP-1559 fee cap).
	MaxFeePerGas *big.Int

	// MaxPriorityFeePerGas is the suggested maxPriorityFeePerGas (tip).
	MaxPriorityFeePerGas *big.Int

	// GasPrice is the suggested gas price for legacy transactions.
	GasPrice *big.Int

	// MaxFeePerBlobGas is the suggested blob fee cap for type-3 transactions.
	MaxFeePerBlobGas *big.Int
}

// TieredSuggestion holds fee suggestions for all speed tiers.
type TieredSuggestion struct {
	Urgent   FeeSuggestion
	Fast     FeeSuggestion
	Standard FeeSuggestion
	Slow     FeeSuggestion
	BaseFee  *big.Int // current base fee for reference
}

// PriceBumper tracks recent block fee data and computes gas price
// suggestions for different confirmation speed targets. It is safe for
// concurrent use.
type PriceBumper struct {
	mu      sync.RWMutex
	config  BumperConfig
	history []BumperBlockFeeData // circular buffer of recent blocks
	head    int            // next write position in history
	count   int            // number of entries in history

	// latestBaseFee caches the most recent block's base fee.
	latestBaseFee *big.Int

	// latestBlobBaseFee caches the most recent blob base fee.
	latestBlobBaseFee *big.Int
}

// NewPriceBumper creates a new PriceBumper with the given configuration.
func NewPriceBumper(config BumperConfig) *PriceBumper {
	if config.HistoryDepth <= 0 {
		config.HistoryDepth = DefaultFeeHistoryDepth
	}
	if config.MinSuggestedTip == nil {
		config.MinSuggestedTip = big.NewInt(BumperMinSuggestedTip)
	}
	if config.BaseFeeMultiplier <= 0 {
		config.BaseFeeMultiplier = DefaultBaseFeeMultiplier
	}
	return &PriceBumper{
		config:  config,
		history: make([]BumperBlockFeeData, config.HistoryDepth),
	}
}

// RecordBlock feeds fee data from a new block into the history buffer.
// This should be called for each new block header processed.
func (pb *PriceBumper) RecordBlock(data BumperBlockFeeData) {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	pb.history[pb.head] = data
	pb.head = (pb.head + 1) % len(pb.history)
	if pb.count < len(pb.history) {
		pb.count++
	}

	if data.BaseFee != nil {
		pb.latestBaseFee = new(big.Int).Set(data.BaseFee)
	}
	if data.BlobBaseFee != nil {
		pb.latestBlobBaseFee = new(big.Int).Set(data.BlobBaseFee)
	}
}

// RecordBlockFromHeader is a convenience method that extracts fee data
// from a block header and its transactions, then feeds it into the buffer.
func (pb *PriceBumper) RecordBlockFromHeader(header *types.Header, txs []*types.Transaction) {
	data := BumperBlockFeeData{
		BaseFee:     header.BaseFee,
		BlockNumber: header.Number.Uint64(),
	}
	if header.GasLimit > 0 {
		data.GasUsedRatio = float64(header.GasUsed) / float64(header.GasLimit)
	}
	if header.ExcessBlobGas != nil {
		data.ExcessBlobGas = *header.ExcessBlobGas
		data.BlobBaseFee = types.CalcBlobFee(*header.ExcessBlobGas)
	}

	// Collect effective tips from transactions.
	for _, tx := range txs {
		tip := EffectiveTip(tx, header.BaseFee)
		// Skip zero/negative tips and txs below ignore threshold.
		if tip.Sign() <= 0 {
			continue
		}
		if pb.config.IgnorePrice != nil {
			price := EffectiveGasPrice(tx, header.BaseFee)
			if price.Cmp(pb.config.IgnorePrice) < 0 {
				continue
			}
		}
		data.Tips = append(data.Tips, tip)
	}

	pb.RecordBlock(data)
}

// SuggestFee returns a fee suggestion for the desired speed tier. Valid
// tiers are TierUrgent, TierFast, TierStandard, and TierSlow.
func (pb *PriceBumper) SuggestFee(tier string) FeeSuggestion {
	percentile := tierToPercentile(tier)
	return pb.suggestAtPercentile(percentile)
}

// SuggestAllTiers returns fee suggestions for all four speed tiers at once.
func (pb *PriceBumper) SuggestAllTiers() TieredSuggestion {
	pb.mu.RLock()
	baseFee := cloneBigInt(pb.latestBaseFee)
	pb.mu.RUnlock()

	return TieredSuggestion{
		Urgent:   pb.suggestAtPercentile(urgentPercentile),
		Fast:     pb.suggestAtPercentile(fastPercentile),
		Standard: pb.suggestAtPercentile(standardPercentile),
		Slow:     pb.suggestAtPercentile(slowPercentile),
		BaseFee:  baseFee,
	}
}

// suggestAtPercentile computes a fee suggestion targeting the given tip
// percentile across recent block history.
func (pb *PriceBumper) suggestAtPercentile(percentile int) FeeSuggestion {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	baseFee := cloneBigInt(pb.latestBaseFee)
	if baseFee == nil {
		baseFee = new(big.Int)
	}

	tip := pb.tipAtPercentile(percentile)
	if tip.Cmp(pb.config.MinSuggestedTip) < 0 {
		tip = new(big.Int).Set(pb.config.MinSuggestedTip)
	}

	// EIP-1559 fee cap: baseFee * multiplier + tip.
	// For urgent, we use a higher multiplier to buffer against base fee rises.
	mult := 1
	if percentile >= urgentPercentile {
		mult = pb.config.BaseFeeMultiplier
	} else if percentile >= fastPercentile {
		mult = pb.config.BaseFeeMultiplier
	}
	// Always use at least 1x base fee plus tip.
	if mult < 1 {
		mult = 1
	}

	feeCap := new(big.Int).Mul(baseFee, big.NewInt(int64(mult)))
	feeCap.Add(feeCap, tip)

	// Legacy gas price: use the fee cap.
	gasPrice := new(big.Int).Set(feeCap)

	suggestion := FeeSuggestion{
		MaxFeePerGas:         feeCap,
		MaxPriorityFeePerGas: tip,
		GasPrice:             gasPrice,
	}

	// Blob fee suggestion.
	if pb.latestBlobBaseFee != nil {
		blobFee := pb.suggestBlobFee()
		suggestion.MaxFeePerBlobGas = blobFee
	}

	return suggestion
}

// tipAtPercentile computes the tip at the given percentile across the
// combined tip samples from all blocks in the history buffer.
// Caller must hold pb.mu (at least RLock).
func (pb *PriceBumper) tipAtPercentile(percentile int) *big.Int {
	var allTips []*big.Int

	for i := 0; i < pb.count; i++ {
		idx := (pb.head - pb.count + i + len(pb.history)) % len(pb.history)
		entry := pb.history[idx]
		for _, tip := range entry.Tips {
			if tip != nil && tip.Sign() > 0 {
				allTips = append(allTips, tip)
			}
		}
	}

	if len(allTips) == 0 {
		return new(big.Int).Set(pb.config.MinSuggestedTip)
	}

	sort.Slice(allTips, func(i, j int) bool {
		return allTips[i].Cmp(allTips[j]) < 0
	})

	idx := (len(allTips) - 1) * percentile / 100
	if idx >= len(allTips) {
		idx = len(allTips) - 1
	}
	return new(big.Int).Set(allTips[idx])
}

// suggestBlobFee suggests a blob fee cap based on the latest blob base fee.
// We suggest 2x the current blob base fee to account for volatility.
// Caller must hold pb.mu (at least RLock).
func (pb *PriceBumper) suggestBlobFee() *big.Int {
	if pb.latestBlobBaseFee == nil {
		return big.NewInt(int64(types.BlobTxMinBlobGasprice))
	}
	// 2x the current blob base fee as a safety margin.
	suggested := new(big.Int).Mul(pb.latestBlobBaseFee, big.NewInt(2))
	minFee := big.NewInt(int64(types.BlobTxMinBlobGasprice))
	if suggested.Cmp(minFee) < 0 {
		return minFee
	}
	return suggested
}

// SuggestReplacementFee computes the minimum fees needed for a replacement
// transaction that will pass the pool's price bump threshold. The bump
// percentage is applied to both the fee cap and the priority fee.
func (pb *PriceBumper) SuggestReplacementFee(tx *types.Transaction, bumpPercent int) FeeSuggestion {
	if bumpPercent <= 0 {
		bumpPercent = PriceBump // use pool default (10%)
	}

	multiplier := big.NewInt(int64(100 + bumpPercent))
	divisor := big.NewInt(100)

	// Compute bumped fee cap.
	feeCap := tx.GasFeeCap()
	if feeCap == nil {
		feeCap = tx.GasPrice()
	}
	if feeCap == nil {
		feeCap = new(big.Int)
	}
	newFeeCap := new(big.Int).Mul(feeCap, multiplier)
	newFeeCap.Div(newFeeCap, divisor)

	// Compute bumped tip.
	tip := tx.GasTipCap()
	if tip == nil {
		tip = tx.GasPrice()
	}
	if tip == nil {
		tip = new(big.Int)
	}
	newTip := new(big.Int).Mul(tip, multiplier)
	newTip.Div(newTip, divisor)

	suggestion := FeeSuggestion{
		MaxFeePerGas:         newFeeCap,
		MaxPriorityFeePerGas: newTip,
		GasPrice:             new(big.Int).Set(newFeeCap),
	}

	// Bump blob fee if present.
	if blobFeeCap := tx.BlobGasFeeCap(); blobFeeCap != nil {
		newBlobFee := new(big.Int).Mul(blobFeeCap, multiplier)
		newBlobFee.Div(newBlobFee, divisor)
		suggestion.MaxFeePerBlobGas = newBlobFee
	}

	return suggestion
}

// GasPriceAtPercentile computes the effective gas price at the given
// percentile (0-100) across all transactions in the fee history buffer.
func (pb *PriceBumper) GasPriceAtPercentile(percentile int) *big.Int {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	if percentile < 0 {
		percentile = 0
	}
	if percentile > 100 {
		percentile = 100
	}

	var allPrices []*big.Int
	for i := 0; i < pb.count; i++ {
		idx := (pb.head - pb.count + i + len(pb.history)) % len(pb.history)
		entry := pb.history[idx]
		baseFee := entry.BaseFee
		for _, tip := range entry.Tips {
			if tip == nil {
				continue
			}
			if baseFee != nil {
				price := new(big.Int).Add(baseFee, tip)
				allPrices = append(allPrices, price)
			} else {
				allPrices = append(allPrices, new(big.Int).Set(tip))
			}
		}
	}

	if len(allPrices) == 0 {
		return new(big.Int)
	}

	sort.Slice(allPrices, func(i, j int) bool {
		return allPrices[i].Cmp(allPrices[j]) < 0
	})

	idx := (len(allPrices) - 1) * percentile / 100
	return new(big.Int).Set(allPrices[idx])
}

// LatestBaseFee returns the most recently recorded base fee.
func (pb *PriceBumper) LatestBaseFee() *big.Int {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	return cloneBigInt(pb.latestBaseFee)
}

// LatestBlobBaseFee returns the most recently recorded blob base fee.
func (pb *PriceBumper) LatestBlobBaseFee() *big.Int {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	return cloneBigInt(pb.latestBlobBaseFee)
}

// HistoryLen returns the number of blocks currently in the history buffer.
func (pb *PriceBumper) HistoryLen() int {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	return pb.count
}

// FeeHistory returns the base fees and gas used ratios for the last n
// blocks in the history buffer (most recent first).
func (pb *PriceBumper) FeeHistory(n int) (baseFees []*big.Int, gasUsedRatios []float64) {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	if n <= 0 || pb.count == 0 {
		return nil, nil
	}
	if n > pb.count {
		n = pb.count
	}

	baseFees = make([]*big.Int, n)
	gasUsedRatios = make([]float64, n)

	for i := 0; i < n; i++ {
		// Walk backwards from the most recent entry.
		idx := (pb.head - 1 - i + len(pb.history)) % len(pb.history)
		entry := pb.history[idx]
		baseFees[i] = cloneBigInt(entry.BaseFee)
		gasUsedRatios[i] = entry.GasUsedRatio
	}
	return baseFees, gasUsedRatios
}

// tierToPercentile maps a tier name to its percentile target.
func tierToPercentile(tier string) int {
	switch tier {
	case TierUrgent:
		return urgentPercentile
	case TierFast:
		return fastPercentile
	case TierStandard:
		return standardPercentile
	case TierSlow:
		return slowPercentile
	default:
		return standardPercentile
	}
}

// cloneBigInt returns a copy of v, or nil if v is nil.
func cloneBigInt(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}
