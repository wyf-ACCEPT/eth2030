package rpc

import (
	"math/big"
	"sort"
	"sync"
)

// GasOracleConfig holds configuration for the gas price oracle.
type GasOracleConfig struct {
	// Blocks is the number of recent blocks to consider for fee estimates.
	Blocks int
	// Percentile is the percentile (0-100) to use when sampling priority fees.
	Percentile int
	// MaxPrice is the absolute maximum gas price the oracle will suggest.
	MaxPrice *big.Int
	// IgnorePrice is the minimum priority fee threshold; tips below this
	// are excluded from the sample.
	IgnorePrice *big.Int
	// MaxHeaderHistory is the maximum number of blocks to retain in history.
	MaxHeaderHistory int
}

// DefaultGasOracleConfig returns a sensible default configuration.
func DefaultGasOracleConfig() GasOracleConfig {
	return GasOracleConfig{
		Blocks:           20,
		Percentile:       60,
		MaxPrice:         new(big.Int).Mul(big.NewInt(500), big.NewInt(1e9)), // 500 Gwei
		IgnorePrice:      big.NewInt(2),                                      // 2 wei
		MaxHeaderHistory: 1024,
	}
}

// BlockFeeData holds fee information for a single block.
type BlockFeeData struct {
	Number           uint64
	BaseFee          *big.Int
	RewardPercentile *big.Int
	GasUsedRatio     float64
}

// blockRecord is internal storage for a block's fee data.
type blockRecord struct {
	number  uint64
	baseFee *big.Int
	tips    []*big.Int
}

// GasOracle tracks recent block base fees and priority fees to produce
// EIP-1559-aware gas price suggestions.
type GasOracle struct {
	mu     sync.RWMutex
	config GasOracleConfig
	blocks []blockRecord
}

// NewGasOracle creates a new gas price oracle with the given configuration.
func NewGasOracle(config GasOracleConfig) *GasOracle {
	if config.Blocks <= 0 {
		config.Blocks = 20
	}
	if config.Percentile < 0 || config.Percentile > 100 {
		config.Percentile = 60
	}
	if config.MaxHeaderHistory <= 0 {
		config.MaxHeaderHistory = 1024
	}
	if config.MaxPrice == nil {
		config.MaxPrice = new(big.Int).Mul(big.NewInt(500), big.NewInt(1e9))
	}
	if config.IgnorePrice == nil {
		config.IgnorePrice = big.NewInt(2)
	}
	return &GasOracle{
		config: config,
		blocks: make([]blockRecord, 0, config.MaxHeaderHistory),
	}
}

// RecordBlock records fee data from a new block. The tips slice contains
// the effective priority fees (tips) paid by each transaction in the block.
func (o *GasOracle) RecordBlock(number uint64, baseFee *big.Int, tips []*big.Int) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Copy baseFee to avoid external mutation.
	bf := new(big.Int)
	if baseFee != nil {
		bf.Set(baseFee)
	}

	// Copy and filter tips.
	filtered := make([]*big.Int, 0, len(tips))
	for _, tip := range tips {
		if tip != nil && tip.Cmp(o.config.IgnorePrice) >= 0 {
			filtered = append(filtered, new(big.Int).Set(tip))
		}
	}

	rec := blockRecord{
		number:  number,
		baseFee: bf,
		tips:    filtered,
	}

	o.blocks = append(o.blocks, rec)

	// Trim history to MaxHeaderHistory.
	if len(o.blocks) > o.config.MaxHeaderHistory {
		excess := len(o.blocks) - o.config.MaxHeaderHistory
		o.blocks = o.blocks[excess:]
	}
}

// BaseFee returns the latest known base fee, or zero if no blocks recorded.
func (o *GasOracle) BaseFee() *big.Int {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if len(o.blocks) == 0 {
		return new(big.Int)
	}
	return new(big.Int).Set(o.blocks[len(o.blocks)-1].baseFee)
}

// SuggestGasTipCap returns a suggested priority fee based on the configured
// percentile of recent tips across the last N blocks.
func (o *GasOracle) SuggestGasTipCap() *big.Int {
	o.mu.RLock()
	defer o.mu.RUnlock()

	// Collect tips from the most recent blocks.
	var allTips []*big.Int
	start := len(o.blocks) - o.config.Blocks
	if start < 0 {
		start = 0
	}
	for _, rec := range o.blocks[start:] {
		allTips = append(allTips, rec.tips...)
	}

	if len(allTips) == 0 {
		// No tips recorded; return a minimal default.
		return big.NewInt(1e9) // 1 Gwei
	}

	// Sort tips ascending.
	sort.Slice(allTips, func(i, j int) bool {
		return allTips[i].Cmp(allTips[j]) < 0
	})

	idx := len(allTips) * o.config.Percentile / 100
	if idx >= len(allTips) {
		idx = len(allTips) - 1
	}

	tip := new(big.Int).Set(allTips[idx])
	return o.cap(tip)
}

// SuggestGasPrice returns a suggested legacy gas price, computed as
// latestBaseFee + suggestedTip. This is suitable for legacy transactions
// that use a single gasPrice field.
func (o *GasOracle) SuggestGasPrice() *big.Int {
	baseFee := o.BaseFee()
	tip := o.SuggestGasTipCap()

	price := new(big.Int).Add(baseFee, tip)
	return o.capLocked(price)
}

// MaxPriorityFeePerGas returns the configured maximum priority fee.
func (o *GasOracle) MaxPriorityFeePerGas() *big.Int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return new(big.Int).Set(o.config.MaxPrice)
}

// FeeHistory returns fee data for the most recent blockCount blocks.
func (o *GasOracle) FeeHistory(blockCount int) []BlockFeeData {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if blockCount <= 0 {
		return nil
	}

	start := len(o.blocks) - blockCount
	if start < 0 {
		start = 0
	}

	result := make([]BlockFeeData, 0, len(o.blocks)-start)
	for _, rec := range o.blocks[start:] {
		reward := o.percentileTip(rec.tips)
		result = append(result, BlockFeeData{
			Number:           rec.number,
			BaseFee:          new(big.Int).Set(rec.baseFee),
			RewardPercentile: reward,
			GasUsedRatio:     0.5, // placeholder; real ratio requires gas data
		})
	}
	return result
}

// EstimateL1DataFee estimates the L1 data posting cost for rollups based on
// the current base fee and data size. Uses the formula:
//
//	l1Fee = baseFee * dataSize * 16 (16 gas per non-zero calldata byte)
func (o *GasOracle) EstimateL1DataFee(dataSize uint64) *big.Int {
	baseFee := o.BaseFee()
	if baseFee.Sign() == 0 {
		return new(big.Int)
	}

	// Cost model: 16 gas per byte of data * baseFee.
	gasPerByte := big.NewInt(16)
	dataSizeBig := new(big.Int).SetUint64(dataSize)

	fee := new(big.Int).Mul(dataSizeBig, gasPerByte)
	fee.Mul(fee, baseFee)
	return fee
}

// percentileTip computes the percentile tip from a slice of tips.
func (o *GasOracle) percentileTip(tips []*big.Int) *big.Int {
	if len(tips) == 0 {
		return new(big.Int)
	}

	sorted := make([]*big.Int, len(tips))
	copy(sorted, tips)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Cmp(sorted[j]) < 0
	})

	idx := len(sorted) * o.config.Percentile / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return new(big.Int).Set(sorted[idx])
}

// cap clamps a value to MaxPrice (caller must NOT hold mu).
func (o *GasOracle) capLocked(val *big.Int) *big.Int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.cap(val)
}

// cap clamps a value to MaxPrice (caller must hold mu.RLock or mu.Lock).
func (o *GasOracle) cap(val *big.Int) *big.Int {
	if o.config.MaxPrice != nil && val.Cmp(o.config.MaxPrice) > 0 {
		return new(big.Int).Set(o.config.MaxPrice)
	}
	return val
}
