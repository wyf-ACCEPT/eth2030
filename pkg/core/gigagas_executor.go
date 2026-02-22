package core

// Gigagas Parallel Executor implements parallel transaction execution
// targeting 1 Ggas/sec throughput for the K+ era (~2028). Transactions
// are partitioned by sender address to avoid state conflicts, and a
// configurable worker pool executes independent groups concurrently.

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// GigagasExecutorConfig configures the parallel executor.
type GigagasExecutorConfig struct {
	// Workers is the number of parallel worker goroutines.
	// Defaults to runtime.NumCPU() if zero.
	Workers int

	// TargetGasPerSec is the target gas throughput in gas/sec.
	TargetGasPerSec uint64

	// MaxRetries is the number of times a conflicting tx may be retried.
	MaxRetries int
}

// DefaultGigagasExecutorConfig returns the default executor configuration.
func DefaultGigagasExecutorConfig() GigagasExecutorConfig {
	return GigagasExecutorConfig{
		Workers:         runtime.NumCPU(),
		TargetGasPerSec: 1_000_000_000,
		MaxRetries:      3,
	}
}

// TxResult captures the outcome of executing a single transaction.
type TxResult struct {
	TxHash  types.Hash
	GasUsed uint64
	Success bool
	Error   string
	Logs    []types.Hash
}

// GigagasExecutionResult captures the aggregate outcome of a batch.
type GigagasExecutionResult struct {
	GasUsed      uint64
	TxResults    []TxResult
	Duration     time.Duration
	GasPerSecond float64
	Parallelism  int
	Conflicts    int
}

// AccessSet tracks the read and write sets of a transaction for
// conflict detection. Addresses and storage slots are tracked.
type AccessSet struct {
	Reads  map[types.Address]bool
	Writes map[types.Address]bool
}

// NewAccessSet creates an empty access set.
func NewAccessSet() *AccessSet {
	return &AccessSet{
		Reads:  make(map[types.Address]bool),
		Writes: make(map[types.Address]bool),
	}
}

// AddRead marks an address as read.
func (as *AccessSet) AddRead(addr types.Address) {
	as.Reads[addr] = true
}

// AddWrite marks an address as written.
func (as *AccessSet) AddWrite(addr types.Address) {
	as.Writes[addr] = true
}

// ConflictsWith returns true if this access set conflicts with another.
// A conflict exists when one set writes to an address that the other
// reads or writes.
func (as *AccessSet) ConflictsWith(other *AccessSet) bool {
	for addr := range as.Writes {
		if other.Reads[addr] || other.Writes[addr] {
			return true
		}
	}
	for addr := range other.Writes {
		if as.Reads[addr] {
			return true
		}
	}
	return false
}

// TxExecuteFunc is a callback invoked to execute a single transaction.
// It receives the transaction and state handle, and returns gas used,
// generated log hashes, and any error.
type TxExecuteFunc func(tx *types.Transaction, stateDB interface{}) (gasUsed uint64, logs []types.Hash, err error)

// GigagasExecutor orchestrates parallel transaction execution.
type GigagasExecutor struct {
	mu       sync.RWMutex
	config   GigagasExecutorConfig
	execFunc TxExecuteFunc

	totalExecuted  atomic.Uint64
	totalGasUsed   atomic.Uint64
	totalConflicts atomic.Uint64
}

// NewGigagasExecutor creates a new parallel executor. If execFunc is nil,
// a default no-op executor is used that charges the tx gas limit.
func NewGigagasExecutor(config GigagasExecutorConfig, execFunc TxExecuteFunc) *GigagasExecutor {
	if config.Workers <= 0 {
		config.Workers = runtime.NumCPU()
	}
	if config.TargetGasPerSec == 0 {
		config.TargetGasPerSec = 1_000_000_000
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if execFunc == nil {
		execFunc = defaultExecFunc
	}
	return &GigagasExecutor{
		config:   config,
		execFunc: execFunc,
	}
}

// defaultExecFunc charges the tx gas limit as consumed gas.
func defaultExecFunc(tx *types.Transaction, _ interface{}) (uint64, []types.Hash, error) {
	return tx.Gas(), nil, nil
}

// ExecuteBatch executes transactions in parallel, respecting gasLimit.
// Transactions are partitioned by sender to avoid conflicts.
func (ge *GigagasExecutor) ExecuteBatch(
	txs []*types.Transaction,
	stateDB interface{},
	gasLimit uint64,
) (*GigagasExecutionResult, error) {
	start := time.Now()

	if len(txs) == 0 {
		return &GigagasExecutionResult{
			Duration:    time.Since(start),
			Parallelism: ge.config.Workers,
		}, nil
	}

	groups := partitionBySender(txs)

	parallelism := ge.config.Workers
	if len(groups) < parallelism {
		parallelism = len(groups)
	}

	type groupResult struct {
		results   []TxResult
		gasUsed   uint64
		conflicts int
	}

	groupCh := make(chan []*types.Transaction, len(groups))
	for _, group := range groups {
		groupCh <- group
	}
	close(groupCh)

	var (
		wg         sync.WaitGroup
		resultsMu  sync.Mutex
		allResults []groupResult
	)

	var gasCounter atomic.Uint64

	for w := 0; w < parallelism; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for group := range groupCh {
				gr := groupResult{}
				for _, tx := range group {
					if gasCounter.Load() >= gasLimit {
						gr.results = append(gr.results, TxResult{
							TxHash:  tx.Hash(),
							Success: false,
							Error:   "gas limit exceeded",
						})
						continue
					}

					gasUsed, logs, err := ge.execFunc(tx, stateDB)

					if gasCounter.Load()+gasUsed > gasLimit {
						gasUsed = gasLimit - gasCounter.Load()
					}
					gasCounter.Add(gasUsed)
					gr.gasUsed += gasUsed

					result := TxResult{
						TxHash:  tx.Hash(),
						GasUsed: gasUsed,
						Logs:    logs,
					}
					if err != nil {
						result.Success = false
						result.Error = err.Error()
						gr.conflicts++
					} else {
						result.Success = true
					}
					gr.results = append(gr.results, result)
				}
				resultsMu.Lock()
				allResults = append(allResults, gr)
				resultsMu.Unlock()
			}
		}()
	}
	wg.Wait()

	totalGas := uint64(0)
	totalConflicts := 0
	txResultMap := make(map[types.Hash]TxResult)
	for _, gr := range allResults {
		totalGas += gr.gasUsed
		totalConflicts += gr.conflicts
		for _, r := range gr.results {
			txResultMap[r.TxHash] = r
		}
	}

	orderedResults := make([]TxResult, len(txs))
	for i, tx := range txs {
		if r, ok := txResultMap[tx.Hash()]; ok {
			orderedResults[i] = r
		} else {
			orderedResults[i] = TxResult{
				TxHash:  tx.Hash(),
				Success: false,
				Error:   "not executed",
			}
		}
	}

	duration := time.Since(start)
	gasPerSec := float64(0)
	if duration > 0 {
		gasPerSec = float64(totalGas) / duration.Seconds()
	}

	ge.totalExecuted.Add(uint64(len(txs)))
	ge.totalGasUsed.Add(totalGas)
	ge.totalConflicts.Add(uint64(totalConflicts))

	return &GigagasExecutionResult{
		GasUsed:      totalGas,
		TxResults:    orderedResults,
		Duration:     duration,
		GasPerSecond: gasPerSec,
		Parallelism:  parallelism,
		Conflicts:    totalConflicts,
	}, nil
}

// partitionBySender groups transactions by sender address.
func partitionBySender(txs []*types.Transaction) [][]*types.Transaction {
	senderGroups := make(map[types.Address][]*types.Transaction)
	var noSender []*types.Transaction

	for _, tx := range txs {
		sender := tx.Sender()
		if sender == nil {
			noSender = append(noSender, tx)
			continue
		}
		senderGroups[*sender] = append(senderGroups[*sender], tx)
	}

	groups := make([][]*types.Transaction, 0, len(senderGroups)+1)
	for _, group := range senderGroups {
		groups = append(groups, group)
	}
	if len(noSender) > 0 {
		groups = append(groups, noSender)
	}
	return groups
}

// DetectConflicts checks all pairs of access sets for conflicts.
func DetectConflicts(sets []*AccessSet) int {
	conflicts := 0
	for i := 0; i < len(sets); i++ {
		for j := i + 1; j < len(sets); j++ {
			if sets[i].ConflictsWith(sets[j]) {
				conflicts++
			}
		}
	}
	return conflicts
}

// GigagasMetrics returns executor metrics: executed txs, gas used, conflicts.
func (ge *GigagasExecutor) GigagasMetrics() (executed, gasUsed, conflicts uint64) {
	return ge.totalExecuted.Load(), ge.totalGasUsed.Load(), ge.totalConflicts.Load()
}

// Workers returns the configured number of workers.
func (ge *GigagasExecutor) Workers() int {
	ge.mu.RLock()
	defer ge.mu.RUnlock()
	return ge.config.Workers
}

// SetWorkers updates the number of workers for future batch executions.
func (ge *GigagasExecutor) SetWorkers(n int) {
	ge.mu.Lock()
	defer ge.mu.Unlock()
	if n > 0 {
		ge.config.Workers = n
	}
}
