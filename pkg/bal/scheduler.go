// scheduler.go implements a BAL-guided parallel transaction scheduler.
// It performs topological sorting of non-conflicting transactions, assigns
// workers, forms execution groups, and supports speculative execution with
// rollback and re-execution on detected conflicts.
//
// The scheduler operates on a dependency graph produced by BALConflictDetector
// and schedules transactions into execution waves where each wave contains
// only non-conflicting transactions that can safely run in parallel.
package bal

import (
	"errors"
	"sort"
	"sync"
	"sync/atomic"
)

// Scheduler errors.
var (
	ErrNoTransactions     = errors.New("scheduler: no transactions to schedule")
	ErrCyclicDependency   = errors.New("scheduler: dependency graph contains a cycle")
	ErrWorkerCountInvalid = errors.New("scheduler: worker count must be positive")
)

// TxTask represents a single transaction to be scheduled for execution.
type TxTask struct {
	Index    int
	GasLimit uint64
}

// Wave represents a group of transactions that can execute in parallel.
// All transactions in a wave are independent of each other according to
// the BAL conflict analysis.
type Wave struct {
	Tasks []TxTask
}

// WorkerAssignment maps a transaction index to the worker that should
// execute it.
type WorkerAssignment struct {
	TxIndex  int
	WorkerID int
}

// SpeculativeResult holds the outcome of a speculatively executed transaction.
type SpeculativeResult struct {
	TxIndex    int
	GasUsed    uint64
	Success    bool
	Rolled     bool // true if this result was rolled back due to conflict
	ReExecuted bool // true if this was a re-execution after rollback
}

// SchedulerMetrics collects runtime statistics for the scheduler.
type SchedulerMetrics struct {
	WavesFormed    atomic.Uint64
	TxsScheduled   atomic.Uint64
	Rollbacks      atomic.Uint64
	ReExecutions   atomic.Uint64
	MaxWaveSize    atomic.Uint64
}

// BALScheduler uses BAL conflict information to schedule transactions
// into parallel execution waves and manage speculative execution.
type BALScheduler struct {
	mu         sync.Mutex
	workers    int
	detector   *BALConflictDetector
	metrics    SchedulerMetrics
}

// NewBALScheduler creates a scheduler with the given number of workers and
// conflict detector. The detector is used to analyze the BAL and build
// dependency information.
func NewBALScheduler(workers int, detector *BALConflictDetector) (*BALScheduler, error) {
	if workers < 1 {
		return nil, ErrWorkerCountInvalid
	}
	return &BALScheduler{
		workers:  workers,
		detector: detector,
	}, nil
}

// Workers returns the configured worker count.
func (s *BALScheduler) Workers() int {
	return s.workers
}

// SchedulerMetricsSnapshot returns a copy of the current metrics.
func (s *BALScheduler) SchedulerMetricsSnapshot() SchedulerMetrics {
	return s.metrics
}

// Schedule analyzes a BlockAccessList and produces an ordered sequence of
// execution waves. Each wave contains non-conflicting transactions that
// can run in parallel. Waves must execute sequentially.
func (s *BALScheduler) Schedule(bal *BlockAccessList) ([]Wave, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if bal == nil || len(bal.Entries) == 0 {
		return nil, ErrNoTransactions
	}

	graph := s.detector.BuildDependencyGraph(bal)
	if len(graph) == 0 {
		return nil, ErrNoTransactions
	}

	order, err := topoSort(graph)
	if err != nil {
		return nil, err
	}

	waves := buildWaves(order, graph)

	// Record metrics.
	s.metrics.WavesFormed.Add(uint64(len(waves)))
	for _, w := range waves {
		s.metrics.TxsScheduled.Add(uint64(len(w.Tasks)))
		sz := uint64(len(w.Tasks))
		// Update max wave size (relaxed CAS loop).
		for {
			cur := s.metrics.MaxWaveSize.Load()
			if sz <= cur {
				break
			}
			if s.metrics.MaxWaveSize.CompareAndSwap(cur, sz) {
				break
			}
		}
	}

	return waves, nil
}

// AssignWorkers distributes tasks in a wave across available workers
// using round-robin assignment. Returns a slice of assignments.
func (s *BALScheduler) AssignWorkers(wave Wave) []WorkerAssignment {
	assignments := make([]WorkerAssignment, len(wave.Tasks))
	for i, task := range wave.Tasks {
		assignments[i] = WorkerAssignment{
			TxIndex:  task.Index,
			WorkerID: i % s.workers,
		}
	}
	return assignments
}

// ExecuteSpeculative simulates speculative parallel execution of a wave.
// Each task runs optimistically. If the provided conflictSet contains
// a task's index, that task is marked as rolled back and scheduled for
// re-execution. Returns results including rollback/re-execution status.
func (s *BALScheduler) ExecuteSpeculative(
	wave Wave,
	conflictSet map[int]struct{},
) []SpeculativeResult {
	results := make([]SpeculativeResult, len(wave.Tasks))

	var wg sync.WaitGroup
	for i, task := range wave.Tasks {
		wg.Add(1)
		go func(idx int, t TxTask) {
			defer wg.Done()

			// Simulate execution: base gas cost.
			gasUsed := uint64(21_000)
			if gasUsed > t.GasLimit {
				gasUsed = t.GasLimit
			}

			result := SpeculativeResult{
				TxIndex: t.Index,
				GasUsed: gasUsed,
				Success: gasUsed <= t.GasLimit,
			}

			// Check if this tx is in the conflict set.
			if _, conflict := conflictSet[t.Index]; conflict {
				result.Rolled = true
				result.Success = false
				s.metrics.Rollbacks.Add(1)
			}

			results[idx] = result
		}(i, task)
	}
	wg.Wait()

	return results
}

// ReExecute re-runs rolled-back transactions serially and returns updated
// results. Only tasks that were rolled back are re-executed.
func (s *BALScheduler) ReExecute(results []SpeculativeResult) []SpeculativeResult {
	updated := make([]SpeculativeResult, len(results))
	copy(updated, results)

	for i, r := range updated {
		if !r.Rolled {
			continue
		}
		// Re-execute serially with guaranteed correctness.
		gasUsed := uint64(21_000)
		updated[i] = SpeculativeResult{
			TxIndex:    r.TxIndex,
			GasUsed:    gasUsed,
			Success:    true,
			Rolled:     false,
			ReExecuted: true,
		}
		s.metrics.ReExecutions.Add(1)
	}

	return updated
}

// topoSort performs Kahn's algorithm on the dependency graph and returns
// a topologically sorted order. Returns ErrCyclicDependency if the graph
// has a cycle.
func topoSort(graph map[int][]int) ([]int, error) {
	inDegree := make(map[int]int)
	forward := make(map[int][]int)

	for node := range graph {
		if _, ok := inDegree[node]; !ok {
			inDegree[node] = 0
		}
	}
	for node, deps := range graph {
		for _, dep := range deps {
			forward[dep] = append(forward[dep], node)
			inDegree[node]++
			if _, ok := inDegree[dep]; !ok {
				inDegree[dep] = 0
			}
		}
	}

	var queue []int
	for node, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, node)
		}
	}
	sort.Ints(queue)

	var order []int
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		order = append(order, node)

		neighbors := forward[node]
		sort.Ints(neighbors)
		for _, next := range neighbors {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
		sort.Ints(queue)
	}

	if len(order) != len(inDegree) {
		return nil, ErrCyclicDependency
	}

	return order, nil
}

// buildWaves partitions a topological order into execution waves.
// Each wave contains transactions whose dependencies are all in earlier waves.
func buildWaves(order []int, graph map[int][]int) []Wave {
	if len(order) == 0 {
		return nil
	}

	level := make(map[int]int)
	for _, node := range order {
		maxDepLevel := -1
		for _, dep := range graph[node] {
			if l, ok := level[dep]; ok && l > maxDepLevel {
				maxDepLevel = l
			}
		}
		level[node] = maxDepLevel + 1
	}

	maxLevel := 0
	for _, l := range level {
		if l > maxLevel {
			maxLevel = l
		}
	}

	waves := make([]Wave, maxLevel+1)
	for _, node := range order {
		l := level[node]
		waves[l].Tasks = append(waves[l].Tasks, TxTask{Index: node})
	}

	return waves
}

// ParallelismRatio returns the ratio of total transactions to the number
// of waves. A higher ratio means more parallelism. Returns 1.0 for a
// single wave or if scheduling has not occurred.
func (s *BALScheduler) ParallelismRatio() float64 {
	waves := s.metrics.WavesFormed.Load()
	txs := s.metrics.TxsScheduled.Load()
	if waves == 0 || txs == 0 {
		return 1.0
	}
	return float64(txs) / float64(waves)
}
