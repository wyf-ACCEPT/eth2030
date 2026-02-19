// Package vm implements the Ethereum Virtual Machine.
//
// gigagas.go implements the Gigagas Executor for high-throughput transaction
// execution targeting 1 Ggas/sec, as outlined in the Ethereum 2028 roadmap
// (EL Throughput track, M+ milestone).
package vm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Gigagas executor errors.
var (
	ErrBatchTooLarge       = errors.New("gigagas: batch exceeds max batch size")
	ErrBatchEmpty          = errors.New("gigagas: batch is empty")
	ErrInvalidWorkerCount  = errors.New("gigagas: invalid worker count")
	ErrGasLimitZero        = errors.New("gigagas: transaction gas limit is zero")
	ErrNilTransaction      = errors.New("gigagas: nil transaction in batch")
	ErrMemoryLimitExceeded = errors.New("gigagas: memory limit exceeded")
)

// DefaultTargetGasPerSecond is 1 billion gas per second (1 Ggas/sec).
const DefaultTargetGasPerSecond = 1_000_000_000

// GigagasConfig configures the high-throughput executor.
type GigagasConfig struct {
	// MaxBatchSize is the maximum number of transactions per batch.
	MaxBatchSize uint64

	// MaxWorkers is the maximum number of parallel execution workers.
	MaxWorkers int

	// TargetGasPerSecond is the target throughput in gas per second.
	// Defaults to 1_000_000_000 (1 Ggas/sec).
	TargetGasPerSecond uint64

	// MemoryLimit is the maximum memory (bytes) for execution buffers.
	MemoryLimit uint64
}

// DefaultGigagasConfig returns a sensible default configuration.
func DefaultGigagasConfig() GigagasConfig {
	return GigagasConfig{
		MaxBatchSize:       10_000,
		MaxWorkers:         8,
		TargetGasPerSecond: DefaultTargetGasPerSecond,
		MemoryLimit:        1 << 30, // 1 GiB
	}
}

// GigagasTx represents a transaction for high-throughput execution.
type GigagasTx struct {
	From     types.Address
	To       types.Address
	Data     []byte
	GasLimit uint64
	Value    uint64
	Nonce    uint64
}

// GigagasLog represents a log entry produced during execution.
type GigagasLog struct {
	Address types.Address
	Topics  []types.Hash
	Data    []byte
}

// GigagasResult holds the outcome of executing a single transaction.
type GigagasResult struct {
	TxHash     types.Hash
	GasUsed    uint64
	Success    bool
	ReturnData []byte
	Logs       []GigagasLog
}

// ThroughputMetrics captures execution performance measurements.
type ThroughputMetrics struct {
	TotalGas     uint64
	Duration     uint64 // nanoseconds
	GasPerSecond uint64
	TxCount      uint64
	AvgGasPerTx  uint64
}

// GigagasExecutor provides high-throughput transaction execution with parallel
// processing and batching support. It is safe for concurrent use.
type GigagasExecutor struct {
	config GigagasConfig
	mu     sync.RWMutex

	// Counters for metrics (atomic for lock-free reads).
	totalExecuted atomic.Uint64
	totalGasUsed  atomic.Uint64
}

// NewGigagasExecutor creates a new high-throughput executor with the given config.
func NewGigagasExecutor(config GigagasConfig) *GigagasExecutor {
	if config.MaxBatchSize == 0 {
		config.MaxBatchSize = 10_000
	}
	if config.MaxWorkers == 0 {
		config.MaxWorkers = 8
	}
	if config.TargetGasPerSecond == 0 {
		config.TargetGasPerSecond = DefaultTargetGasPerSecond
	}
	if config.MemoryLimit == 0 {
		config.MemoryLimit = 1 << 30
	}
	return &GigagasExecutor{config: config}
}

// PrevalidateBatch validates all transactions before execution.
// Returns a slice of errors, one per transaction (nil if valid).
func (ge *GigagasExecutor) PrevalidateBatch(txs []*GigagasTx) []error {
	errs := make([]error, len(txs))
	for i, tx := range txs {
		if tx == nil {
			errs[i] = ErrNilTransaction
			continue
		}
		if tx.GasLimit == 0 {
			errs[i] = ErrGasLimitZero
			continue
		}
	}
	return errs
}

// ExecuteBatch executes a batch of transactions sequentially and returns
// results. This provides deterministic ordering guarantees.
func (ge *GigagasExecutor) ExecuteBatch(txs []*GigagasTx) ([]*GigagasResult, error) {
	if len(txs) == 0 {
		return nil, ErrBatchEmpty
	}
	if uint64(len(txs)) > ge.config.MaxBatchSize {
		return nil, fmt.Errorf("%w: %d > %d", ErrBatchTooLarge, len(txs), ge.config.MaxBatchSize)
	}

	// Check memory budget: rough estimate of per-tx overhead.
	memEstimate := uint64(len(txs)) * 1024
	for _, tx := range txs {
		if tx != nil {
			memEstimate += uint64(len(tx.Data))
		}
	}
	if memEstimate > ge.config.MemoryLimit {
		return nil, ErrMemoryLimitExceeded
	}

	results := make([]*GigagasResult, len(txs))
	for i, tx := range txs {
		results[i] = ge.executeSingle(tx, uint64(i))
	}

	ge.totalExecuted.Add(uint64(len(txs)))
	return results, nil
}

// ParallelExecute executes transactions in parallel across the given number
// of workers. Transactions are partitioned into conflict-free groups by sender
// address for safe parallel execution.
func (ge *GigagasExecutor) ParallelExecute(txs []*GigagasTx, workers int) ([]*GigagasResult, error) {
	if len(txs) == 0 {
		return nil, ErrBatchEmpty
	}
	if workers <= 0 {
		return nil, ErrInvalidWorkerCount
	}
	if uint64(len(txs)) > ge.config.MaxBatchSize {
		return nil, fmt.Errorf("%w: %d > %d", ErrBatchTooLarge, len(txs), ge.config.MaxBatchSize)
	}
	if workers > ge.config.MaxWorkers {
		workers = ge.config.MaxWorkers
	}

	results := make([]*GigagasResult, len(txs))

	// Partition transactions by sender for conflict-free parallel execution.
	// Transactions from the same sender must execute sequentially to
	// maintain nonce ordering.
	type indexedTx struct {
		index int
		tx    *GigagasTx
	}
	groups := make(map[types.Address][]indexedTx)
	for i, tx := range txs {
		if tx == nil {
			results[i] = &GigagasResult{Success: false}
			continue
		}
		groups[tx.From] = append(groups[tx.From], indexedTx{index: i, tx: tx})
	}

	// Fan out groups to workers via channel.
	type workGroup struct {
		items []indexedTx
	}
	ch := make(chan workGroup, len(groups))
	for _, items := range groups {
		ch <- workGroup{items: items}
	}
	close(ch)

	var wg sync.WaitGroup
	for w := 0; w < workers && w < len(groups); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for group := range ch {
				for _, it := range group.items {
					results[it.index] = ge.executeSingle(it.tx, uint64(it.index))
				}
			}
		}()
	}
	wg.Wait()

	ge.totalExecuted.Add(uint64(len(txs)))
	return results, nil
}

// MeasureThroughput executes the batch and measures throughput performance.
func (ge *GigagasExecutor) MeasureThroughput(txs []*GigagasTx) *ThroughputMetrics {
	if len(txs) == 0 {
		return &ThroughputMetrics{}
	}

	start := time.Now()
	results, err := ge.ExecuteBatch(txs)
	elapsed := time.Since(start)

	if err != nil {
		return &ThroughputMetrics{TxCount: uint64(len(txs))}
	}

	var totalGas uint64
	for _, r := range results {
		if r != nil {
			totalGas += r.GasUsed
		}
	}

	dur := uint64(elapsed.Nanoseconds())
	if dur == 0 {
		dur = 1 // avoid division by zero
	}

	gasPerSec := totalGas * 1_000_000_000 / dur

	var avgGas uint64
	if len(results) > 0 {
		avgGas = totalGas / uint64(len(results))
	}

	return &ThroughputMetrics{
		TotalGas:     totalGas,
		Duration:     dur,
		GasPerSecond: gasPerSec,
		TxCount:      uint64(len(txs)),
		AvgGasPerTx:  avgGas,
	}
}

// TotalExecuted returns the total number of transactions executed.
func (ge *GigagasExecutor) TotalExecuted() uint64 {
	return ge.totalExecuted.Load()
}

// TotalGasUsed returns the total gas consumed across all executions.
func (ge *GigagasExecutor) TotalGasUsed() uint64 {
	return ge.totalGasUsed.Load()
}

// executeSingle executes a single transaction and returns its result.
// Gas accounting: the base cost is 21000, plus 16 gas per non-zero data byte
// and 4 gas per zero byte (as per EIP-2028 calldata pricing).
func (ge *GigagasExecutor) executeSingle(tx *GigagasTx, batchIndex uint64) *GigagasResult {
	if tx == nil {
		return &GigagasResult{Success: false}
	}

	txHash := computeTxHash(tx, batchIndex)

	// Intrinsic gas: 21000 base + calldata cost.
	intrinsicGas := uint64(21_000)
	for _, b := range tx.Data {
		if b == 0 {
			intrinsicGas += 4
		} else {
			intrinsicGas += 16
		}
	}

	if intrinsicGas > tx.GasLimit {
		return &GigagasResult{
			TxHash:  txHash,
			GasUsed: tx.GasLimit,
			Success: false,
		}
	}

	gasUsed := intrinsicGas
	// If there is calldata, simulate execution gas (use remaining gas).
	if len(tx.Data) > 0 {
		execGas := (tx.GasLimit - intrinsicGas) / 2
		gasUsed += execGas
	}

	ge.totalGasUsed.Add(gasUsed)

	// Generate logs if the transaction has calldata (simulates contract interaction).
	var logs []GigagasLog
	if len(tx.Data) >= 4 {
		topicHash := crypto.Keccak256Hash(tx.Data[:4])
		logs = append(logs, GigagasLog{
			Address: tx.To,
			Topics:  []types.Hash{topicHash},
			Data:    tx.Data[4:],
		})
	}

	return &GigagasResult{
		TxHash:     txHash,
		GasUsed:    gasUsed,
		Success:    true,
		ReturnData: nil,
		Logs:       logs,
	}
}

// computeTxHash derives a deterministic hash for a transaction within a batch.
func computeTxHash(tx *GigagasTx, batchIndex uint64) types.Hash {
	var buf []byte
	buf = append(buf, tx.From[:]...)
	buf = append(buf, tx.To[:]...)

	var nonceBuf [8]byte
	binary.BigEndian.PutUint64(nonceBuf[:], tx.Nonce)
	buf = append(buf, nonceBuf[:]...)

	var idxBuf [8]byte
	binary.BigEndian.PutUint64(idxBuf[:], batchIndex)
	buf = append(buf, idxBuf[:]...)

	buf = append(buf, tx.Data...)

	return crypto.Keccak256Hash(buf)
}
