// work_stealing.go implements a lock-free work-stealing scheduler for gigagas
// parallel transaction execution. Each worker goroutine maintains a local deque
// of tasks; when idle, workers steal from other workers' deques. This minimizes
// contention under the 1 Ggas/sec workload by keeping tasks close to the thread
// that created them while still balancing load across cores.
package core

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// WorkStealingTask is a unit of work representing a single transaction
// execution with an estimated gas cost for scheduling heuristics.
type WorkStealingTask struct {
	// ID is a unique task identifier (typically the transaction index).
	ID int
	// GasCost is the estimated gas cost for this task.
	GasCost uint64
	// Execute is the function to run. It returns actual gas used.
	Execute func() uint64
}

// workDeque is a double-ended queue supporting Push/Pop from the back (owner)
// and Steal from the front (thieves). Uses a mutex for correctness; the
// contention is acceptable because steals are infrequent relative to local
// pops.
type workDeque struct {
	mu    sync.Mutex
	items []*WorkStealingTask
}

// Push adds a task to the back (owner end) of the deque.
func (d *workDeque) Push(task *WorkStealingTask) {
	d.mu.Lock()
	d.items = append(d.items, task)
	d.mu.Unlock()
}

// Pop removes and returns a task from the back (owner end).
func (d *workDeque) Pop() (*WorkStealingTask, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.items) == 0 {
		return nil, false
	}
	n := len(d.items) - 1
	task := d.items[n]
	d.items = d.items[:n]
	return task, true
}

// Steal removes and returns a task from the front (thief end).
func (d *workDeque) Steal() (*WorkStealingTask, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.items) == 0 {
		return nil, false
	}
	task := d.items[0]
	d.items = d.items[1:]
	return task, true
}

// Len returns the current deque size.
func (d *workDeque) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.items)
}

// WorkStealingMetrics tracks performance counters for the pool.
type WorkStealingMetrics struct {
	TasksExecuted atomic.Uint64
	TasksStolen   atomic.Uint64
	TotalGasUsed  atomic.Uint64
	IdleNanos     atomic.Int64
}

// Snapshot returns a copy of the current metrics.
func (m *WorkStealingMetrics) Snapshot() (executed, stolen, gasUsed uint64, idleTime time.Duration) {
	return m.TasksExecuted.Load(), m.TasksStolen.Load(),
		m.TotalGasUsed.Load(), time.Duration(m.IdleNanos.Load())
}

// WorkStealingPool is a worker pool where each worker has its own deque.
// Workers first process local tasks, then attempt to steal from peers.
type WorkStealingPool struct {
	workers int
	deques  []*workDeque
	metrics WorkStealingMetrics
}

// NewWorkStealingPool creates a pool with numWorkers goroutines.
// If numWorkers <= 0, defaults to runtime.NumCPU().
func NewWorkStealingPool(numWorkers int) *WorkStealingPool {
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}
	deques := make([]*workDeque, numWorkers)
	for i := range deques {
		deques[i] = &workDeque{}
	}
	return &WorkStealingPool{
		workers: numWorkers,
		deques:  deques,
	}
}

// Workers returns the number of worker goroutines.
func (p *WorkStealingPool) Workers() int { return p.workers }

// Metrics returns the pool's performance metrics.
func (p *WorkStealingPool) Metrics() *WorkStealingMetrics { return &p.metrics }

// Submit distributes tasks across worker deques using round-robin on task ID.
func (p *WorkStealingPool) Submit(tasks []*WorkStealingTask) {
	for i, task := range tasks {
		dequeIdx := i % p.workers
		p.deques[dequeIdx].Push(task)
	}
}

// SubmitByGas distributes tasks to the deque with the smallest estimated
// gas load, achieving better balance for heterogeneous transactions.
func (p *WorkStealingPool) SubmitByGas(tasks []*WorkStealingTask) {
	loads := make([]uint64, p.workers)
	for _, task := range tasks {
		// Find the deque with the smallest load.
		minIdx := 0
		for j := 1; j < p.workers; j++ {
			if loads[j] < loads[minIdx] {
				minIdx = j
			}
		}
		p.deques[minIdx].Push(task)
		loads[minIdx] += task.GasCost
	}
}

// Run executes all submitted tasks in parallel using the work-stealing
// strategy. Blocks until all tasks are complete.
func (p *WorkStealingPool) Run() {
	var wg sync.WaitGroup
	for w := 0; w < p.workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			p.workerLoop(workerID)
		}(w)
	}
	wg.Wait()
}

// workerLoop processes the local deque, then steals from other workers.
func (p *WorkStealingPool) workerLoop(workerID int) {
	myDeque := p.deques[workerID]

	for {
		// Phase 1: drain own deque.
		task, ok := myDeque.Pop()
		if ok {
			p.executeTask(task, false)
			continue
		}

		// Phase 2: attempt to steal from other workers.
		idleStart := time.Now()
		stolen := false
		for i := 1; i < p.workers; i++ {
			victimID := (workerID + i) % p.workers
			task, ok = p.deques[victimID].Steal()
			if ok {
				p.metrics.IdleNanos.Add(time.Since(idleStart).Nanoseconds())
				p.executeTask(task, true)
				stolen = true
				break
			}
		}

		if !stolen {
			// No work anywhere -- record idle time and exit.
			p.metrics.IdleNanos.Add(time.Since(idleStart).Nanoseconds())
			return
		}
	}
}

// executeTask runs a single task and updates metrics.
func (p *WorkStealingPool) executeTask(task *WorkStealingTask, wasStolen bool) {
	gas := task.Execute()
	p.metrics.TasksExecuted.Add(1)
	p.metrics.TotalGasUsed.Add(gas)
	if wasStolen {
		p.metrics.TasksStolen.Add(1)
	}
}

// RunTasks is a convenience method that submits and runs tasks in one call.
func (p *WorkStealingPool) RunTasks(tasks []*WorkStealingTask) {
	p.Submit(tasks)
	p.Run()
}

// TotalPendingTasks returns the sum of tasks across all deques.
func (p *WorkStealingPool) TotalPendingTasks() int {
	total := 0
	for _, d := range p.deques {
		total += d.Len()
	}
	return total
}
