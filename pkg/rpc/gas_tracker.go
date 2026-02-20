package rpc

import (
	"errors"
	"math"
	"sort"
	"sync"
)

// Errors returned by GasTracker operations.
var (
	ErrGasTrackerEmpty             = errors.New("rpc: gas tracker has no recorded blocks")
	ErrGasTrackerInvalidPercentile = errors.New("rpc: percentile must be between 0 and 100")
)

// GasTrackerConfig configures the GasTracker.
type GasTrackerConfig struct {
	// HistoryBlocks is the number of recent blocks to retain.
	HistoryBlocks int
	// Percentiles is a list of percentile values (0-100) used for fee estimation.
	Percentiles []float64
	// MaxCacheSize is the maximum number of entries in the percentile cache.
	MaxCacheSize int
}

// GasBlockRecord holds gas data for a single block.
type GasBlockRecord struct {
	BlockNumber uint64
	BaseFee     uint64
	GasUsed     uint64
	GasLimit    uint64
	TxGasPrices []uint64 // sorted ascending
}

// GasFeeEstimate contains fee estimates derived from recent block history.
type GasFeeEstimate struct {
	BaseFeeEstimate  uint64
	PriorityFeeLow   uint64
	PriorityFeeMedium uint64
	PriorityFeeHigh  uint64
	NextBlockBaseFee uint64
}

// GasTracker tracks gas usage patterns for EIP-1559 fee estimation and
// gas price analytics. All methods are safe for concurrent use.
type GasTracker struct {
	mu             sync.RWMutex
	config         GasTrackerConfig
	blocks         []*GasBlockRecord
	percentileCache map[float64]uint64
}

// NewGasTracker creates a new GasTracker with the given configuration.
func NewGasTracker(config GasTrackerConfig) *GasTracker {
	if config.HistoryBlocks <= 0 {
		config.HistoryBlocks = 128
	}
	if config.MaxCacheSize <= 0 {
		config.MaxCacheSize = 256
	}
	if len(config.Percentiles) == 0 {
		config.Percentiles = []float64{10, 50, 90}
	}
	return &GasTracker{
		config:          config,
		blocks:          make([]*GasBlockRecord, 0, config.HistoryBlocks),
		percentileCache: make(map[float64]uint64),
	}
}

// RecordBlock records a block's gas data into the tracker history.
// The TxGasPrices slice is copied and sorted internally.
func (gt *GasTracker) RecordBlock(record *GasBlockRecord) error {
	if record == nil {
		return errors.New("rpc: nil block record")
	}

	gt.mu.Lock()
	defer gt.mu.Unlock()

	// Copy the record to avoid external mutation.
	rec := &GasBlockRecord{
		BlockNumber: record.BlockNumber,
		BaseFee:     record.BaseFee,
		GasUsed:     record.GasUsed,
		GasLimit:    record.GasLimit,
	}
	if len(record.TxGasPrices) > 0 {
		rec.TxGasPrices = make([]uint64, len(record.TxGasPrices))
		copy(rec.TxGasPrices, record.TxGasPrices)
		sort.Slice(rec.TxGasPrices, func(i, j int) bool {
			return rec.TxGasPrices[i] < rec.TxGasPrices[j]
		})
	}

	gt.blocks = append(gt.blocks, rec)

	// Trim history to HistoryBlocks.
	if len(gt.blocks) > gt.config.HistoryBlocks {
		excess := len(gt.blocks) - gt.config.HistoryBlocks
		gt.blocks = gt.blocks[excess:]
	}

	// Invalidate percentile cache on new data.
	gt.percentileCache = make(map[float64]uint64)

	return nil
}

// EstimateFees produces fee estimates from recent block history.
// Uses the 10th, 50th, and 90th percentiles for low/medium/high priority fees.
func (gt *GasTracker) EstimateFees() (*GasFeeEstimate, error) {
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	if len(gt.blocks) == 0 {
		return nil, ErrGasTrackerEmpty
	}

	// Compute base fee estimate from the latest block.
	latest := gt.blocks[len(gt.blocks)-1]
	baseFeeEstimate := latest.BaseFee

	// Estimate next block base fee using EIP-1559 formula.
	nextBaseFee := gt.calcNextBaseFee(latest)

	// Collect all gas prices across blocks.
	allPrices := gt.collectAllPrices()
	if len(allPrices) == 0 {
		return &GasFeeEstimate{
			BaseFeeEstimate:  baseFeeEstimate,
			PriorityFeeLow:   0,
			PriorityFeeMedium: 0,
			PriorityFeeHigh:  0,
			NextBlockBaseFee: nextBaseFee,
		}, nil
	}

	low := gt.percentileFromSorted(allPrices, 10)
	medium := gt.percentileFromSorted(allPrices, 50)
	high := gt.percentileFromSorted(allPrices, 90)

	return &GasFeeEstimate{
		BaseFeeEstimate:  baseFeeEstimate,
		PriorityFeeLow:   low,
		PriorityFeeMedium: medium,
		PriorityFeeHigh:  high,
		NextBlockBaseFee: nextBaseFee,
	}, nil
}

// Percentile returns the gas price at the given percentile (0-100)
// across all recorded blocks.
func (gt *GasTracker) Percentile(p float64) (uint64, error) {
	if p < 0 || p > 100 {
		return 0, ErrGasTrackerInvalidPercentile
	}

	gt.mu.Lock()
	defer gt.mu.Unlock()

	if len(gt.blocks) == 0 {
		return 0, ErrGasTrackerEmpty
	}

	// Check cache.
	if cached, ok := gt.percentileCache[p]; ok {
		return cached, nil
	}

	allPrices := gt.collectAllPrices()
	if len(allPrices) == 0 {
		return 0, nil
	}

	result := gt.percentileFromSorted(allPrices, p)

	// Store in cache if under limit.
	if len(gt.percentileCache) < gt.config.MaxCacheSize {
		gt.percentileCache[p] = result
	}

	return result, nil
}

// AverageGasUsed returns the average gas utilization ratio (gasUsed/gasLimit)
// across all recorded blocks. Returns 0 if no blocks are recorded.
func (gt *GasTracker) AverageGasUsed() float64 {
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	if len(gt.blocks) == 0 {
		return 0
	}

	var total float64
	var count int
	for _, b := range gt.blocks {
		if b.GasLimit > 0 {
			total += float64(b.GasUsed) / float64(b.GasLimit)
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

// BaseFeeHistory returns the last n base fees from recorded blocks,
// ordered oldest to newest.
func (gt *GasTracker) BaseFeeHistory(n int) []uint64 {
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	if n <= 0 || len(gt.blocks) == 0 {
		return nil
	}

	start := len(gt.blocks) - n
	if start < 0 {
		start = 0
	}

	result := make([]uint64, 0, len(gt.blocks)-start)
	for _, b := range gt.blocks[start:] {
		result = append(result, b.BaseFee)
	}
	return result
}

// PriorityFeeHistory returns the last n median priority fees (one per block),
// ordered oldest to newest.
func (gt *GasTracker) PriorityFeeHistory(n int) []uint64 {
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	if n <= 0 || len(gt.blocks) == 0 {
		return nil
	}

	start := len(gt.blocks) - n
	if start < 0 {
		start = 0
	}

	result := make([]uint64, 0, len(gt.blocks)-start)
	for _, b := range gt.blocks[start:] {
		if len(b.TxGasPrices) == 0 {
			result = append(result, 0)
		} else {
			mid := len(b.TxGasPrices) / 2
			result = append(result, b.TxGasPrices[mid])
		}
	}
	return result
}

// TrendDirection compares the average base fee of the recent half of
// history to the older half and returns "rising", "falling", or "stable".
func (gt *GasTracker) TrendDirection() string {
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	if len(gt.blocks) < 2 {
		return "stable"
	}

	mid := len(gt.blocks) / 2
	olderBlocks := gt.blocks[:mid]
	newerBlocks := gt.blocks[mid:]

	olderAvg := avgBaseFee(olderBlocks)
	newerAvg := avgBaseFee(newerBlocks)

	if olderAvg == 0 {
		if newerAvg > 0 {
			return "rising"
		}
		return "stable"
	}

	// A change of more than 5% in either direction is a trend.
	ratio := float64(newerAvg) / float64(olderAvg)
	if ratio > 1.05 {
		return "rising"
	}
	if ratio < 0.95 {
		return "falling"
	}
	return "stable"
}

// BlockCount returns the number of blocks currently recorded.
func (gt *GasTracker) BlockCount() int {
	gt.mu.RLock()
	defer gt.mu.RUnlock()
	return len(gt.blocks)
}

// collectAllPrices gathers and sorts all tx gas prices from recorded blocks.
// Caller must hold at least gt.mu.RLock.
func (gt *GasTracker) collectAllPrices() []uint64 {
	var total int
	for _, b := range gt.blocks {
		total += len(b.TxGasPrices)
	}
	if total == 0 {
		return nil
	}

	all := make([]uint64, 0, total)
	for _, b := range gt.blocks {
		all = append(all, b.TxGasPrices...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	return all
}

// percentileFromSorted returns the value at percentile p from a sorted slice.
// Caller must ensure prices is sorted ascending and non-empty.
func (gt *GasTracker) percentileFromSorted(prices []uint64, p float64) uint64 {
	if len(prices) == 0 {
		return 0
	}
	if p <= 0 {
		return prices[0]
	}
	if p >= 100 {
		return prices[len(prices)-1]
	}

	idx := int(math.Ceil(float64(len(prices))*p/100)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(prices) {
		idx = len(prices) - 1
	}
	return prices[idx]
}

// calcNextBaseFee estimates the next block's base fee using a simplified
// EIP-1559 adjustment formula. Caller must hold at least gt.mu.RLock.
func (gt *GasTracker) calcNextBaseFee(block *GasBlockRecord) uint64 {
	if block.GasLimit == 0 || block.BaseFee == 0 {
		return block.BaseFee
	}

	target := block.GasLimit / 2
	baseFee := block.BaseFee

	if block.GasUsed == target {
		return baseFee
	}

	if block.GasUsed > target {
		// Gas used above target: base fee increases.
		delta := block.GasUsed - target
		adjustment := baseFee * delta / target / 8
		if adjustment == 0 {
			adjustment = 1
		}
		return baseFee + adjustment
	}

	// Gas used below target: base fee decreases.
	delta := target - block.GasUsed
	adjustment := baseFee * delta / target / 8
	if adjustment >= baseFee {
		return 0
	}
	return baseFee - adjustment
}

// avgBaseFee computes the average base fee for a slice of block records.
func avgBaseFee(blocks []*GasBlockRecord) uint64 {
	if len(blocks) == 0 {
		return 0
	}
	var sum uint64
	for _, b := range blocks {
		sum += b.BaseFee
	}
	return sum / uint64(len(blocks))
}
