package core

import (
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// GigagasConfig configures the gigagas (1 Ggas/sec) execution infrastructure.
type GigagasConfig struct {
	TargetGasPerSecond     uint64 // target gas throughput (1 Ggas/s = 1_000_000_000)
	MaxBlockGas            uint64 // maximum gas per block
	ParallelExecutionSlots uint32 // number of parallel execution slots
}

// DefaultGigagasConfig returns the default gigagas configuration targeting
// 1 billion gas per second with 16 parallel execution slots.
var DefaultGigagasConfig = GigagasConfig{
	TargetGasPerSecond:     1_000_000_000,
	MaxBlockGas:            500_000_000,
	ParallelExecutionSlots: 16,
}

// blockGasRecord is a single block's gas measurement.
type blockGasRecord struct {
	BlockNum  uint64
	GasUsed   uint64
	Timestamp uint64
}

// GasRateTracker tracks gas throughput over a sliding window of blocks.
type GasRateTracker struct {
	mu      sync.Mutex
	records []blockGasRecord
	window  int // max number of records to keep
}

// NewGasRateTracker creates a new gas rate tracker with the given window size.
func NewGasRateTracker(windowSize int) *GasRateTracker {
	if windowSize <= 0 {
		windowSize = 100
	}
	return &GasRateTracker{
		records: make([]blockGasRecord, 0, windowSize),
		window:  windowSize,
	}
}

// RecordBlockGas records gas usage for a block.
func (t *GasRateTracker) RecordBlockGas(blockNum, gasUsed, timestamp uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.records = append(t.records, blockGasRecord{
		BlockNum:  blockNum,
		GasUsed:   gasUsed,
		Timestamp: timestamp,
	})
	// Trim to window size.
	if len(t.records) > t.window {
		t.records = t.records[len(t.records)-t.window:]
	}
}

// CurrentGasRate returns the current gas per second over the recorded window.
// Returns 0 if fewer than 2 records exist.
func (t *GasRateTracker) CurrentGasRate() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.records) < 2 {
		return 0
	}
	first := t.records[0]
	last := t.records[len(t.records)-1]

	timeDelta := last.Timestamp - first.Timestamp
	if timeDelta == 0 {
		return 0
	}

	var totalGas uint64
	for _, r := range t.records {
		totalGas += r.GasUsed
	}
	return float64(totalGas) / float64(timeDelta)
}

// IsGigagasEnabled returns true if the gigagas upgrade is active at the given timestamp.
// Gigagas activates at the K+ fork stage (~2028), which is beyond Hogota.
// For now, this checks if Hogota is active as a prerequisite placeholder.
func IsGigagasEnabled(config *ChainConfig, time uint64) bool {
	// Gigagas requires Hogota as a baseline (multidimensional pricing must be active).
	return config.IsHogota(time)
}

// ParallelExecutionHints analyzes transactions and returns groups of
// independent transaction indices that can be executed in parallel.
// Transactions touching the same sender or recipient are placed in the
// same group (conflict set).
func ParallelExecutionHints(txs []*types.Transaction) [][]int {
	if len(txs) == 0 {
		return nil
	}

	// Build a mapping from address -> list of tx indices that touch that address.
	addrToTxs := make(map[types.Address][]int)
	for i, tx := range txs {
		// Sender address.
		if from := tx.Sender(); from != nil {
			addrToTxs[*from] = append(addrToTxs[*from], i)
		}
		// Recipient address.
		if to := tx.To(); to != nil {
			addrToTxs[*to] = append(addrToTxs[*to], i)
		}
	}

	// Union-find to group conflicting transactions.
	parent := make([]int, len(txs))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	// Merge all tx indices that share an address.
	for _, indices := range addrToTxs {
		for j := 1; j < len(indices); j++ {
			union(indices[0], indices[j])
		}
	}

	// Collect groups.
	groups := make(map[int][]int)
	for i := range txs {
		root := find(i)
		groups[root] = append(groups[root], i)
	}

	result := make([][]int, 0, len(groups))
	for _, group := range groups {
		result = append(result, group)
	}
	return result
}

// EstimateParallelSpeedup estimates the theoretical speedup from parallel
// execution given the transaction groups. Returns 1.0 for no speedup.
// The speedup is estimated as totalTxs / maxGroupSize (Amdahl's law approximation).
func EstimateParallelSpeedup(txGroups [][]int) float64 {
	if len(txGroups) == 0 {
		return 1.0
	}
	total := 0
	maxGroup := 0
	for _, g := range txGroups {
		total += len(g)
		if len(g) > maxGroup {
			maxGroup = len(g)
		}
	}
	if maxGroup == 0 {
		return 1.0
	}
	return float64(total) / float64(maxGroup)
}
