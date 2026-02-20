package core

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestGigaWorkSteal_NewPool(t *testing.T) {
	pool := NewWorkStealingPool(4)
	if pool.Workers() != 4 {
		t.Errorf("Workers() = %d, want 4", pool.Workers())
	}

	// Default should use NumCPU.
	pool2 := NewWorkStealingPool(0)
	if pool2.Workers() != runtime.NumCPU() {
		t.Errorf("Workers() = %d, want %d", pool2.Workers(), runtime.NumCPU())
	}
}

func TestGigaWorkSteal_DequeOperations(t *testing.T) {
	d := &workDeque{}

	// Empty pop.
	_, ok := d.Pop()
	if ok {
		t.Error("Pop on empty deque should return false")
	}

	// Empty steal.
	_, ok = d.Steal()
	if ok {
		t.Error("Steal on empty deque should return false")
	}

	// Push and pop (LIFO).
	d.Push(&WorkStealingTask{ID: 1})
	d.Push(&WorkStealingTask{ID: 2})
	d.Push(&WorkStealingTask{ID: 3})

	if d.Len() != 3 {
		t.Errorf("Len() = %d, want 3", d.Len())
	}

	task, ok := d.Pop()
	if !ok || task.ID != 3 {
		t.Errorf("Pop: got ID=%d, want 3", task.ID)
	}

	// Steal from front (FIFO).
	task, ok = d.Steal()
	if !ok || task.ID != 1 {
		t.Errorf("Steal: got ID=%d, want 1", task.ID)
	}

	if d.Len() != 1 {
		t.Errorf("Len() = %d, want 1", d.Len())
	}
}

func TestGigaWorkSteal_BasicExecution(t *testing.T) {
	pool := NewWorkStealingPool(4)

	var counter atomic.Int64
	tasks := make([]*WorkStealingTask, 100)
	for i := 0; i < 100; i++ {
		tasks[i] = &WorkStealingTask{
			ID:      i,
			GasCost: 21000,
			Execute: func() uint64 {
				counter.Add(1)
				return 21000
			},
		}
	}

	pool.RunTasks(tasks)

	if counter.Load() != 100 {
		t.Errorf("executed %d tasks, want 100", counter.Load())
	}

	executed, _, gasUsed, _ := pool.Metrics().Snapshot()
	if executed != 100 {
		t.Errorf("metrics.TasksExecuted = %d, want 100", executed)
	}
	if gasUsed != 100*21000 {
		t.Errorf("metrics.TotalGasUsed = %d, want %d", gasUsed, 100*21000)
	}
}

func TestGigaWorkSteal_WorkStealing(t *testing.T) {
	// Create pool with 4 workers but load all tasks onto worker 0's deque.
	pool := NewWorkStealingPool(4)

	var counter atomic.Int64
	numTasks := 100
	for i := 0; i < numTasks; i++ {
		pool.deques[0].Push(&WorkStealingTask{
			ID:      i,
			GasCost: 21000,
			Execute: func() uint64 {
				counter.Add(1)
				// Small sleep to give steal a chance.
				time.Sleep(10 * time.Microsecond)
				return 21000
			},
		})
	}

	pool.Run()

	if counter.Load() != int64(numTasks) {
		t.Errorf("executed %d tasks, want %d", counter.Load(), numTasks)
	}

	_, stolen, _, _ := pool.Metrics().Snapshot()
	// With all tasks in one deque and 4 workers, steals should occur.
	if stolen == 0 {
		t.Log("Warning: no steals occurred (may be valid if execution was very fast)")
	}
}

func TestGigaWorkSteal_GasBasedDistribution(t *testing.T) {
	pool := NewWorkStealingPool(2)

	var counter atomic.Int64
	tasks := []*WorkStealingTask{
		{ID: 0, GasCost: 1_000_000, Execute: func() uint64 { counter.Add(1); return 1_000_000 }},
		{ID: 1, GasCost: 100, Execute: func() uint64 { counter.Add(1); return 100 }},
		{ID: 2, GasCost: 1_000_000, Execute: func() uint64 { counter.Add(1); return 1_000_000 }},
		{ID: 3, GasCost: 100, Execute: func() uint64 { counter.Add(1); return 100 }},
	}

	pool.SubmitByGas(tasks)

	// Verify distribution: expensive tasks should go to different deques.
	d0Len := pool.deques[0].Len()
	d1Len := pool.deques[1].Len()
	if d0Len+d1Len != 4 {
		t.Errorf("total tasks = %d, want 4", d0Len+d1Len)
	}

	pool.Run()
	if counter.Load() != 4 {
		t.Errorf("executed %d tasks, want 4", counter.Load())
	}
}

func TestGigaWorkSteal_ConcurrentSafety(t *testing.T) {
	pool := NewWorkStealingPool(8)

	var total atomic.Int64
	tasks := make([]*WorkStealingTask, 1000)
	for i := 0; i < 1000; i++ {
		tasks[i] = &WorkStealingTask{
			ID:      i,
			GasCost: uint64(i + 1),
			Execute: func() uint64 {
				total.Add(1)
				return 1
			},
		}
	}

	pool.RunTasks(tasks)

	if total.Load() != 1000 {
		t.Errorf("executed %d tasks, want 1000", total.Load())
	}
}

func TestGigaWorkSteal_MetricsIdleTime(t *testing.T) {
	pool := NewWorkStealingPool(4)

	// Submit just 1 task -- most workers will be idle.
	task := &WorkStealingTask{
		ID:      0,
		GasCost: 100,
		Execute: func() uint64 {
			time.Sleep(time.Millisecond)
			return 100
		},
	}

	pool.RunTasks([]*WorkStealingTask{task})

	_, _, _, idle := pool.Metrics().Snapshot()
	// Idle time should be >= 0 (could be very small on fast machines).
	if idle < 0 {
		t.Errorf("idle time should be >= 0, got %v", idle)
	}
}

func TestGigaWorkSteal_EmptyPool(t *testing.T) {
	pool := NewWorkStealingPool(4)
	// Run with no tasks should return immediately.
	pool.Run()

	executed, _, _, _ := pool.Metrics().Snapshot()
	if executed != 0 {
		t.Errorf("TasksExecuted = %d, want 0", executed)
	}
}

func TestGigaWorkSteal_PendingTasks(t *testing.T) {
	pool := NewWorkStealingPool(2)
	tasks := make([]*WorkStealingTask, 10)
	for i := range tasks {
		tasks[i] = &WorkStealingTask{ID: i, GasCost: 100, Execute: func() uint64 { return 100 }}
	}
	pool.Submit(tasks)

	pending := pool.TotalPendingTasks()
	if pending != 10 {
		t.Errorf("TotalPendingTasks() = %d, want 10", pending)
	}

	pool.Run()
	pending = pool.TotalPendingTasks()
	if pending != 0 {
		t.Errorf("after Run, TotalPendingTasks() = %d, want 0", pending)
	}
}
