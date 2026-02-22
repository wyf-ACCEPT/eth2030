// gigagas_integration.go wires the gigagas parallel executor, work-stealing
// pool, and sharded state into the live chain pipeline. GigagasBlockProcessor
// replaces sequential block execution with parallel execution, re-executing
// conflicting transactions sequentially. Parallelism is adaptive based on
// gas rate feedback. Fork activation: enabled only for Hogota+ blocks.
package core

import (
	"errors"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// GigagasBlockProcessor errors.
var (
	ErrGigagasNilBlock    = errors.New("gigagas_integration: nil block")
	ErrGigagasNilState    = errors.New("gigagas_integration: nil state db")
	ErrGigagasNotEnabled  = errors.New("gigagas_integration: gigagas not enabled for this block")
	ErrGigagasGasExceeded = errors.New("gigagas_integration: block gas limit exceeded")
)

// GigagasBlockConfig configures the block processor.
type GigagasBlockConfig struct {
	// MinWorkers is the floor for adaptive worker count.
	MinWorkers int
	// MaxWorkers is the ceiling for adaptive worker count.
	MaxWorkers int
	// TargetGasPerSec is the target gas throughput.
	TargetGasPerSec uint64
	// ConflictRetries is how many times conflicting txs are retried.
	ConflictRetries int
	// AdaptiveEnabled controls whether worker count adjusts based on rate.
	AdaptiveEnabled bool
}

// DefaultGigagasBlockConfig returns sensible defaults.
func DefaultGigagasBlockConfig() GigagasBlockConfig {
	cpus := runtime.NumCPU()
	return GigagasBlockConfig{
		MinWorkers:      2,
		MaxWorkers:      cpus,
		TargetGasPerSec: 1_000_000_000,
		ConflictRetries: 3,
		AdaptiveEnabled: true,
	}
}

// BlockProcessingResult holds the outcome of parallel block processing.
type BlockProcessingResult struct {
	Receipts     []*TxReceipt
	GasUsed      uint64
	Conflicts    int
	ReExecuted   int
	WorkerCount  int
	Duration     time.Duration
	GasPerSecond float64
}

// TxReceipt is a minimal receipt for block processing results.
type TxReceipt struct {
	TxHash  types.Hash
	GasUsed uint64
	Success bool
	Logs    []types.Hash
}

// txExecResult captures execution output for a single transaction during
// parallel block processing, including the access record for conflict detection.
type txExecResult struct {
	index   int
	receipt *TxReceipt
	access  *state.TxAccessRecord
}

// GigagasBlockProcessor replaces sequential block execution with parallel
// execution using work-stealing and sharded state.
type GigagasBlockProcessor struct {
	config      GigagasBlockConfig
	rateTracker *GasRateTracker
	chainConfig *ChainConfig

	currentWorkers atomic.Int32
	totalBlocks    atomic.Uint64
	totalConflicts atomic.Uint64

	execFunc TxExecuteFunc
}

// NewGigagasBlockProcessor creates a new parallel block processor.
// If execFunc is nil, a default no-op executor is used.
func NewGigagasBlockProcessor(
	config GigagasBlockConfig,
	chainConfig *ChainConfig,
	execFunc TxExecuteFunc,
) *GigagasBlockProcessor {
	if config.MinWorkers <= 0 {
		config.MinWorkers = 2
	}
	if config.MaxWorkers <= 0 {
		config.MaxWorkers = runtime.NumCPU()
	}
	if config.MaxWorkers < config.MinWorkers {
		config.MaxWorkers = config.MinWorkers
	}
	if config.TargetGasPerSec == 0 {
		config.TargetGasPerSec = 1_000_000_000
	}
	if config.ConflictRetries <= 0 {
		config.ConflictRetries = 3
	}
	if execFunc == nil {
		execFunc = defaultExecFunc
	}

	p := &GigagasBlockProcessor{
		config:      config,
		rateTracker: NewGasRateTracker(100),
		chainConfig: chainConfig,
		execFunc:    execFunc,
	}
	p.currentWorkers.Store(int32(config.MaxWorkers))
	return p
}

// IsEnabled returns true if gigagas parallel execution is active for the
// given block timestamp.
func (p *GigagasBlockProcessor) IsEnabled(blockTime uint64) bool {
	if p.chainConfig == nil {
		return false
	}
	return IsGigagasEnabled(p.chainConfig, blockTime)
}

// ProcessBlockParallel executes all transactions in a block using parallel
// execution with conflict detection and sequential re-execution of conflicts.
func (p *GigagasBlockProcessor) ProcessBlockParallel(
	block *types.Block,
	stateDB *state.ShardedStateDB,
	gasLimit uint64,
	blockTime uint64,
) (*BlockProcessingResult, error) {
	if block == nil {
		return nil, ErrGigagasNilBlock
	}
	if stateDB == nil {
		return nil, ErrGigagasNilState
	}
	if !p.IsEnabled(blockTime) {
		return nil, ErrGigagasNotEnabled
	}

	start := time.Now()
	txs := block.Transactions()
	if len(txs) == 0 {
		return &BlockProcessingResult{
			Receipts:    make([]*TxReceipt, 0),
			WorkerCount: int(p.currentWorkers.Load()),
			Duration:    time.Since(start),
		}, nil
	}

	workerCount := int(p.currentWorkers.Load())

	// Phase 1: Build independent tx groups using address overlap analysis.
	groups := ParallelExecutionHints(txs)

	// Phase 2: Execute groups in parallel via work-stealing pool.
	pool := NewWorkStealingPool(workerCount)

	var (
		resultsMu sync.Mutex
		results   = make(map[int]*txExecResult)
	)

	// Build tasks: one per transaction.
	tasks := make([]*WorkStealingTask, 0, len(txs))
	for i, tx := range txs {
		txIdx := i
		txCopy := tx
		task := &WorkStealingTask{
			ID:      txIdx,
			GasCost: txCopy.Gas(),
			Execute: func() uint64 {
				// Execute on the sharded state (concurrent-safe).
				gasUsed, logs, err := p.execFunc(txCopy, stateDB)

				// Track access record for conflict detection.
				access := state.NewTxAccessRecord(txIdx)
				if from := txCopy.Sender(); from != nil {
					access.AddWrite(*from, types.Hash{})
				}
				if to := txCopy.To(); to != nil {
					access.AddRead(*to, types.Hash{})
				}

				receipt := &TxReceipt{
					TxHash:  txCopy.Hash(),
					GasUsed: gasUsed,
					Success: err == nil,
					Logs:    logs,
				}

				resultsMu.Lock()
				results[txIdx] = &txExecResult{
					index:   txIdx,
					receipt: receipt,
					access:  access,
				}
				resultsMu.Unlock()

				return gasUsed
			},
		}
		tasks = append(tasks, task)
	}

	// Distribute by gas cost for better balance, then run.
	pool.SubmitByGas(tasks)
	pool.Run()

	// Phase 3: Conflict detection using TxAccessRecord.
	conflictingTxs := detectConflictingTxIndices(results, groups)
	conflicts := len(conflictingTxs)

	// Phase 4: Re-execute conflicting transactions sequentially.
	reExecuted := 0
	for _, txIdx := range conflictingTxs {
		if txIdx < 0 || txIdx >= len(txs) {
			continue
		}
		tx := txs[txIdx]
		gasUsed, logs, err := p.execFunc(tx, stateDB)
		results[txIdx] = &txExecResult{
			index: txIdx,
			receipt: &TxReceipt{
				TxHash:  tx.Hash(),
				GasUsed: gasUsed,
				Success: err == nil,
				Logs:    logs,
			},
		}
		reExecuted++
	}

	// Collect ordered receipts and total gas.
	receipts := make([]*TxReceipt, len(txs))
	var totalGas uint64
	for i := range txs {
		if r, ok := results[i]; ok {
			receipts[i] = r.receipt
			totalGas += r.receipt.GasUsed
		} else {
			receipts[i] = &TxReceipt{
				TxHash: txs[i].Hash(),
			}
		}
	}

	duration := time.Since(start)
	gasPerSec := float64(0)
	if duration > 0 {
		gasPerSec = float64(totalGas) / duration.Seconds()
	}

	// Record rate for adaptive adjustment.
	p.rateTracker.RecordBlockGas(
		block.NumberU64(),
		totalGas,
		blockTime,
	)

	// Adaptive parallelism adjustment.
	if p.config.AdaptiveEnabled {
		p.adaptWorkerCount()
	}

	p.totalBlocks.Add(1)
	p.totalConflicts.Add(uint64(conflicts))

	return &BlockProcessingResult{
		Receipts:     receipts,
		GasUsed:      totalGas,
		Conflicts:    conflicts,
		ReExecuted:   reExecuted,
		WorkerCount:  workerCount,
		Duration:     duration,
		GasPerSecond: gasPerSec,
	}, nil
}

// detectConflictingTxIndices finds transactions whose access records conflict
// with other transactions in different groups.
func detectConflictingTxIndices(
	results map[int]*txExecResult,
	groups [][]int,
) []int {
	var conflicting []int
	seen := make(map[int]bool)

	// Only check cross-group conflicts (within-group is sequential).
	for gi := 0; gi < len(groups); gi++ {
		for gj := gi + 1; gj < len(groups); gj++ {
			for _, ti := range groups[gi] {
				ri, okI := results[ti]
				if !okI || ri.access == nil {
					continue
				}
				for _, tj := range groups[gj] {
					rj, okJ := results[tj]
					if !okJ || rj.access == nil {
						continue
					}
					if ri.access.ConflictsWith(rj.access) {
						if !seen[tj] {
							conflicting = append(conflicting, tj)
							seen[tj] = true
						}
					}
				}
			}
		}
	}
	return conflicting
}

// adaptWorkerCount adjusts the worker count based on the current gas rate
// relative to the target.
func (p *GigagasBlockProcessor) adaptWorkerCount() {
	currentRate := p.rateTracker.CurrentGasRate()
	if currentRate <= 0 {
		return
	}
	target := float64(p.config.TargetGasPerSec)

	// If current rate is below 80% of target, increase workers.
	// If above 120%, decrease workers (over-provisioning wastes CPU).
	ratio := currentRate / target
	current := int(p.currentWorkers.Load())
	newWorkers := current

	if ratio < 0.8 {
		// Under-performing: add workers.
		newWorkers = int(math.Ceil(float64(current) * 1.25))
	} else if ratio > 1.2 && current > p.config.MinWorkers {
		// Over-provisioned: reduce workers.
		newWorkers = int(math.Floor(float64(current) * 0.9))
	}

	// Clamp to bounds.
	if newWorkers < p.config.MinWorkers {
		newWorkers = p.config.MinWorkers
	}
	if newWorkers > p.config.MaxWorkers {
		newWorkers = p.config.MaxWorkers
	}

	p.currentWorkers.Store(int32(newWorkers))
}

// CurrentWorkers returns the current adaptive worker count.
func (p *GigagasBlockProcessor) CurrentWorkers() int {
	return int(p.currentWorkers.Load())
}

// Metrics returns aggregate processing metrics.
func (p *GigagasBlockProcessor) Metrics() (blocks, conflicts uint64) {
	return p.totalBlocks.Load(), p.totalConflicts.Load()
}

// CurrentGasRate returns the current gas/sec from the rate tracker.
func (p *GigagasBlockProcessor) CurrentGasRate() float64 {
	return p.rateTracker.CurrentGasRate()
}
