// blob_fee_tracker.go implements a specialized blob gas price oracle for
// EIP-4844 blob transactions. It tracks recent block blob base fees,
// blob gas usage ratios, and detects fee spikes. It provides percentile-based
// blob fee suggestions with configurable lookback windows, price floor
// enforcement, and separate spike detection logic for blob gas markets.
package txpool

import (
	"math/big"
	"sort"
	"sync"
)

// Blob fee tracker constants.
const (
	// BlobFeeDefaultWindow is the default number of blocks tracked.
	BlobFeeDefaultWindow = 64

	// BlobFeeDefaultFloor is the minimum blob base fee (1 wei per EIP-4844).
	BlobFeeDefaultFloor = 1

	// BlobFeeSpikeThresholdPct is the percentage increase over the moving
	// average that triggers a spike detection. A value of 200 means a fee
	// that is 2x the moving average is flagged as a spike.
	BlobFeeSpikeThresholdPct = 200

	// BlobFeeSlowPercentile is the percentile for conservative blob fee estimates.
	BlobFeeSlowPercentile = 25

	// BlobFeeMedPercentile is the percentile for standard blob fee estimates.
	BlobFeeMedPercentile = 50

	// BlobFeeFastPercentile is the percentile for aggressive blob fee estimates.
	BlobFeeFastPercentile = 90

	// BlobFeeBufferNum / BlobFeeBufferDenom gives the headroom multiplier.
	// 9/8 = 12.5% headroom, matching the max blob base fee increase per block.
	BlobFeeBufferNum   = 9
	BlobFeeBufferDenom = 8

	// BlobTargetGasPerBlock is the target blob gas per block (3 blobs * 131072).
	BlobTargetGasPerBlock = 393216

	// BlobMaxGasPerBlock is the max blob gas per block (6 blobs * 131072).
	BlobMaxGasPerBlock = 786432

	// BlobBaseFeeUpdateFractionTracker mirrors the EIP-4844 update fraction.
	BlobBaseFeeUpdateFractionTracker = 3338477
)

// BlobFeeTrackerConfig configures the BlobFeeTracker.
type BlobFeeTrackerConfig struct {
	WindowSize        int      // Number of blocks to maintain in the history.
	MinBlobFee        *big.Int // Minimum blob base fee floor.
	SpikeThresholdPct int      // Percentage above moving average for spike detection.
	BufferNum         int      // Headroom numerator (e.g. 9).
	BufferDenom       int      // Headroom denominator (e.g. 8).
}

// DefaultBlobFeeTrackerConfig returns sensible defaults.
func DefaultBlobFeeTrackerConfig() BlobFeeTrackerConfig {
	return BlobFeeTrackerConfig{
		WindowSize:        BlobFeeDefaultWindow,
		MinBlobFee:        big.NewInt(BlobFeeDefaultFloor),
		SpikeThresholdPct: BlobFeeSpikeThresholdPct,
		BufferNum:         BlobFeeBufferNum,
		BufferDenom:       BlobFeeBufferDenom,
	}
}

// BlobFeeRecord holds blob fee data from a single block.
type BlobFeeRecord struct {
	Number       uint64   // Block number.
	BlobBaseFee  *big.Int // Blob base fee for this block.
	BlobGasUsed  uint64   // Total blob gas used in the block.
	BlobGasLimit uint64   // Maximum blob gas allowed (typically 786432).
	BlobCount    int      // Number of blobs in the block.
}

// BlobFeeSuggestion holds blob fee suggestions at different urgency levels.
type BlobFeeSuggestion struct {
	SlowFee      *big.Int // Conservative blob maxFeePerBlobGas.
	MediumFee    *big.Int // Standard blob maxFeePerBlobGas.
	FastFee      *big.Int // Aggressive blob maxFeePerBlobGas.
	CurrentFee   *big.Int // Latest observed blob base fee.
	EstimatedFee *big.Int // Estimated next block blob base fee.
	IsSpike      bool     // Whether the current fee is considered a spike.
}

// BlobFeeSpike describes a detected blob fee spike event.
type BlobFeeSpike struct {
	BlockNumber uint64   // Block where the spike was detected.
	CurrentFee  *big.Int // Blob base fee at the spike.
	MovingAvg   *big.Int // Moving average at the time of the spike.
	Ratio       float64  // CurrentFee / MovingAvg ratio.
}

// BlobFeeTracker tracks recent block blob fees and provides blob gas price
// recommendations. All methods are safe for concurrent use.
type BlobFeeTracker struct {
	mu      sync.RWMutex
	config  BlobFeeTrackerConfig
	records []BlobFeeRecord // Circular buffer of block records.
	head    int             // Next write position.
	count   int             // Valid entries count.
	spikes  []BlobFeeSpike  // Recent spike events (capped at window size).
}

// NewBlobFeeTracker creates a BlobFeeTracker with the given config.
func NewBlobFeeTracker(config BlobFeeTrackerConfig) *BlobFeeTracker {
	if config.WindowSize <= 0 {
		config.WindowSize = BlobFeeDefaultWindow
	}
	if config.MinBlobFee == nil {
		config.MinBlobFee = big.NewInt(BlobFeeDefaultFloor)
	}
	if config.SpikeThresholdPct <= 0 {
		config.SpikeThresholdPct = BlobFeeSpikeThresholdPct
	}
	if config.BufferNum <= 0 {
		config.BufferNum = BlobFeeBufferNum
	}
	if config.BufferDenom <= 0 {
		config.BufferDenom = BlobFeeBufferDenom
	}
	return &BlobFeeTracker{
		config:  config,
		records: make([]BlobFeeRecord, config.WindowSize),
	}
}

// AddBlock records blob fee data from a new block. If a spike is detected,
// it is appended to the internal spike log.
func (bt *BlobFeeTracker) AddBlock(rec BlobFeeRecord) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	bt.records[bt.head] = rec
	bt.head = (bt.head + 1) % bt.config.WindowSize
	if bt.count < bt.config.WindowSize {
		bt.count++
	}

	// Detect spikes.
	if rec.BlobBaseFee != nil && bt.count > 1 {
		avg := bt.movingAverageLocked()
		if avg.Sign() > 0 {
			// ratio = (currentFee * 100) / avg
			ratio := new(big.Int).Mul(rec.BlobBaseFee, big.NewInt(100))
			ratio.Div(ratio, avg)
			if ratio.Int64() > int64(bt.config.SpikeThresholdPct) {
				spike := BlobFeeSpike{
					BlockNumber: rec.Number,
					CurrentFee:  new(big.Int).Set(rec.BlobBaseFee),
					MovingAvg:   new(big.Int).Set(avg),
					Ratio:       float64(ratio.Int64()) / 100.0,
				}
				bt.spikes = append(bt.spikes, spike)
				// Cap spike log to window size.
				if len(bt.spikes) > bt.config.WindowSize {
					bt.spikes = bt.spikes[1:]
				}
			}
		}
	}
}

// BlockCount returns the number of blocks in the tracker window.
func (bt *BlobFeeTracker) BlockCount() int {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.count
}

// LatestBlobBaseFee returns the most recently observed blob base fee.
func (bt *BlobFeeTracker) LatestBlobBaseFee() *big.Int {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	if bt.count == 0 {
		return nil
	}
	idx := (bt.head - 1 + bt.config.WindowSize) % bt.config.WindowSize
	if bt.records[idx].BlobBaseFee == nil {
		return nil
	}
	return new(big.Int).Set(bt.records[idx].BlobBaseFee)
}

// MovingAverage returns the moving average of blob base fees over the window.
func (bt *BlobFeeTracker) MovingAverage() *big.Int {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.movingAverageLocked()
}

func (bt *BlobFeeTracker) movingAverageLocked() *big.Int {
	if bt.count == 0 {
		return new(big.Int).Set(bt.config.MinBlobFee)
	}
	sum := new(big.Int)
	n := 0
	for i := 0; i < bt.count; i++ {
		idx := (bt.head - bt.count + i + bt.config.WindowSize) % bt.config.WindowSize
		if bt.records[idx].BlobBaseFee != nil {
			sum.Add(sum, bt.records[idx].BlobBaseFee)
			n++
		}
	}
	if n == 0 {
		return new(big.Int).Set(bt.config.MinBlobFee)
	}
	return sum.Div(sum, big.NewInt(int64(n)))
}

// EstimateNextBlobFee estimates the next block's blob base fee based on
// the latest block's blob gas usage ratio relative to the target.
func (bt *BlobFeeTracker) EstimateNextBlobFee() *big.Int {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.estimateNextLocked()
}

// SuggestBlobFee returns a blob fee suggestion at the medium percentile
// with a buffer for potential increases. The result is clamped to the
// configured minimum.
func (bt *BlobFeeTracker) SuggestBlobFee() *big.Int {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	fees := bt.collectFeesLocked()
	if len(fees) == 0 {
		return new(big.Int).Set(bt.config.MinBlobFee)
	}

	med := blobFeePercentile(fees, BlobFeeMedPercentile)

	// Apply buffer: med * BufferNum / BufferDenom.
	buffered := new(big.Int).Mul(med, big.NewInt(int64(bt.config.BufferNum)))
	buffered.Div(buffered, big.NewInt(int64(bt.config.BufferDenom)))

	if buffered.Cmp(bt.config.MinBlobFee) < 0 {
		return new(big.Int).Set(bt.config.MinBlobFee)
	}
	return buffered
}

// Suggest returns multi-level blob fee suggestions.
func (bt *BlobFeeTracker) Suggest() BlobFeeSuggestion {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	fees := bt.collectFeesLocked()
	currentFee := bt.latestFeeLocked()
	estimatedFee := bt.estimateNextLocked()

	var slowFee, medFee, fastFee *big.Int
	if len(fees) == 0 {
		slowFee = new(big.Int).Set(bt.config.MinBlobFee)
		medFee = new(big.Int).Set(bt.config.MinBlobFee)
		fastFee = new(big.Int).Set(bt.config.MinBlobFee)
	} else {
		slowFee = blobFeePercentile(fees, BlobFeeSlowPercentile)
		// Re-collect because percentile sorts in place.
		fees2 := bt.collectFeesLocked()
		medFee = blobFeePercentile(fees2, BlobFeeMedPercentile)
		fees3 := bt.collectFeesLocked()
		fastFee = blobFeePercentile(fees3, BlobFeeFastPercentile)
	}

	// Apply buffer to each level.
	slowFee = bt.applyBuffer(slowFee)
	medFee = bt.applyBuffer(medFee)
	fastFee = bt.applyBuffer(fastFee)

	// Enforce floor.
	if slowFee.Cmp(bt.config.MinBlobFee) < 0 {
		slowFee.Set(bt.config.MinBlobFee)
	}
	if medFee.Cmp(bt.config.MinBlobFee) < 0 {
		medFee.Set(bt.config.MinBlobFee)
	}
	if fastFee.Cmp(bt.config.MinBlobFee) < 0 {
		fastFee.Set(bt.config.MinBlobFee)
	}

	// Detect current spike.
	isSpike := false
	if currentFee != nil && currentFee.Sign() > 0 {
		avg := bt.movingAverageLocked()
		if avg.Sign() > 0 {
			ratio := new(big.Int).Mul(currentFee, big.NewInt(100))
			ratio.Div(ratio, avg)
			isSpike = ratio.Int64() > int64(bt.config.SpikeThresholdPct)
		}
	}

	return BlobFeeSuggestion{
		SlowFee:      slowFee,
		MediumFee:    medFee,
		FastFee:      fastFee,
		CurrentFee:   currentFee,
		EstimatedFee: estimatedFee,
		IsSpike:      isSpike,
	}
}

// BlobGasUtilization returns the average blob gas utilization as a
// percentage (0-100) over the tracked window.
func (bt *BlobFeeTracker) BlobGasUtilization() float64 {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	if bt.count == 0 {
		return 0
	}
	var totalUsed, totalLimit uint64
	for i := 0; i < bt.count; i++ {
		idx := (bt.head - bt.count + i + bt.config.WindowSize) % bt.config.WindowSize
		totalUsed += bt.records[idx].BlobGasUsed
		totalLimit += bt.records[idx].BlobGasLimit
	}
	if totalLimit == 0 {
		return 0
	}
	return float64(totalUsed) / float64(totalLimit) * 100.0
}

// FeeHistory returns blob base fees for the last n blocks, oldest first.
func (bt *BlobFeeTracker) FeeHistory(n int) []*big.Int {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	if n > bt.count {
		n = bt.count
	}
	result := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		idx := (bt.head - n + i + bt.config.WindowSize) % bt.config.WindowSize
		if bt.records[idx].BlobBaseFee != nil {
			result[i] = new(big.Int).Set(bt.records[idx].BlobBaseFee)
		} else {
			result[i] = new(big.Int)
		}
	}
	return result
}

// Spikes returns all detected spike events.
func (bt *BlobFeeTracker) Spikes() []BlobFeeSpike {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	result := make([]BlobFeeSpike, len(bt.spikes))
	copy(result, bt.spikes)
	return result
}

// IsCurrentSpike returns whether the latest blob fee qualifies as a spike.
func (bt *BlobFeeTracker) IsCurrentSpike() bool {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	if bt.count < 2 {
		return false
	}
	idx := (bt.head - 1 + bt.config.WindowSize) % bt.config.WindowSize
	fee := bt.records[idx].BlobBaseFee
	if fee == nil || fee.Sign() == 0 {
		return false
	}
	avg := bt.movingAverageLocked()
	if avg.Sign() == 0 {
		return false
	}
	ratio := new(big.Int).Mul(fee, big.NewInt(100))
	ratio.Div(ratio, avg)
	return ratio.Int64() > int64(bt.config.SpikeThresholdPct)
}

// TotalBlobCount returns the total number of blobs across the tracked window.
func (bt *BlobFeeTracker) TotalBlobCount() int {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	total := 0
	for i := 0; i < bt.count; i++ {
		idx := (bt.head - bt.count + i + bt.config.WindowSize) % bt.config.WindowSize
		total += bt.records[idx].BlobCount
	}
	return total
}

// --- internal helpers (caller must hold lock) ---

func (bt *BlobFeeTracker) collectFeesLocked() []*big.Int {
	var fees []*big.Int
	for i := 0; i < bt.count; i++ {
		idx := (bt.head - bt.count + i + bt.config.WindowSize) % bt.config.WindowSize
		if bt.records[idx].BlobBaseFee != nil && bt.records[idx].BlobBaseFee.Sign() > 0 {
			fees = append(fees, new(big.Int).Set(bt.records[idx].BlobBaseFee))
		}
	}
	return fees
}

func (bt *BlobFeeTracker) latestFeeLocked() *big.Int {
	if bt.count == 0 {
		return new(big.Int).Set(bt.config.MinBlobFee)
	}
	idx := (bt.head - 1 + bt.config.WindowSize) % bt.config.WindowSize
	if bt.records[idx].BlobBaseFee != nil {
		return new(big.Int).Set(bt.records[idx].BlobBaseFee)
	}
	return new(big.Int).Set(bt.config.MinBlobFee)
}

func (bt *BlobFeeTracker) estimateNextLocked() *big.Int {
	if bt.count == 0 {
		return new(big.Int).Set(bt.config.MinBlobFee)
	}
	idx := (bt.head - 1 + bt.config.WindowSize) % bt.config.WindowSize
	rec := bt.records[idx]
	if rec.BlobBaseFee == nil {
		return new(big.Int).Set(bt.config.MinBlobFee)
	}
	fee := new(big.Int).Set(rec.BlobBaseFee)
	target := uint64(BlobTargetGasPerBlock)
	if rec.BlobGasUsed > target {
		excess := rec.BlobGasUsed - target
		delta := new(big.Int).SetUint64(excess)
		delta.Mul(delta, fee)
		delta.Div(delta, new(big.Int).SetUint64(target))
		delta.Div(delta, big.NewInt(8))
		if delta.Sign() == 0 {
			delta.SetInt64(1)
		}
		fee.Add(fee, delta)
	} else if rec.BlobGasUsed < target {
		deficit := target - rec.BlobGasUsed
		delta := new(big.Int).SetUint64(deficit)
		delta.Mul(delta, fee)
		delta.Div(delta, new(big.Int).SetUint64(target))
		delta.Div(delta, big.NewInt(8))
		fee.Sub(fee, delta)
	}
	if fee.Cmp(bt.config.MinBlobFee) < 0 {
		fee.Set(bt.config.MinBlobFee)
	}
	return fee
}

func (bt *BlobFeeTracker) applyBuffer(fee *big.Int) *big.Int {
	buffered := new(big.Int).Mul(fee, big.NewInt(int64(bt.config.BufferNum)))
	buffered.Div(buffered, big.NewInt(int64(bt.config.BufferDenom)))
	return buffered
}

// blobFeePercentile computes the p-th percentile (0-100) of big.Int values.
// The slice is sorted in place.
func blobFeePercentile(vals []*big.Int, p int) *big.Int {
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
