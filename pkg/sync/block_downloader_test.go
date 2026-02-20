package sync

import (
	"fmt"
	gosync "sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func testBlockDownloaderConfig() BlockDownloaderConfig {
	return BlockDownloaderConfig{
		MaxConcurrent:  4,
		BlockBatchSize: 10,
		RetryLimit:     3,
		Timeout:        30,
	}
}

func TestBlockDownloader_QueueRange(t *testing.T) {
	dl := NewBlockDownloader(testBlockDownloaderConfig())

	if err := dl.QueueRange(1, 25); err != nil {
		t.Fatalf("QueueRange: %v", err)
	}

	// With batch size 10: tasks for [1,10], [11,20], [21,25] = 3 tasks.
	if got := dl.PendingTasks(); got != 3 {
		t.Errorf("PendingTasks = %d, want 3", got)
	}
}

func TestBlockDownloader_QueueRange_SingleBlock(t *testing.T) {
	dl := NewBlockDownloader(testBlockDownloaderConfig())

	if err := dl.QueueRange(100, 100); err != nil {
		t.Fatalf("QueueRange single block: %v", err)
	}

	if got := dl.PendingTasks(); got != 1 {
		t.Errorf("PendingTasks = %d, want 1", got)
	}
}

func TestBlockDownloader_QueueRange_InvalidRange(t *testing.T) {
	dl := NewBlockDownloader(testBlockDownloaderConfig())

	if err := dl.QueueRange(100, 50); err != ErrInvalidRange {
		t.Errorf("QueueRange(100,50): got %v, want ErrInvalidRange", err)
	}
}

func TestBlockDownloader_QueueRange_ExactBatch(t *testing.T) {
	dl := NewBlockDownloader(testBlockDownloaderConfig())

	// Exactly one batch: [1, 10].
	if err := dl.QueueRange(1, 10); err != nil {
		t.Fatalf("QueueRange: %v", err)
	}
	if got := dl.PendingTasks(); got != 1 {
		t.Errorf("PendingTasks = %d, want 1", got)
	}
}

func TestBlockDownloader_AssignTask(t *testing.T) {
	dl := NewBlockDownloader(testBlockDownloaderConfig())
	dl.QueueRange(1, 10)

	task := dl.AssignTask("peer-A")
	if task == nil {
		t.Fatal("AssignTask returned nil")
	}
	if task.PeerID != "peer-A" {
		t.Errorf("task.PeerID = %q, want %q", task.PeerID, "peer-A")
	}
	if task.Status != TaskStatusActive {
		t.Errorf("task.Status = %q, want %q", task.Status, TaskStatusActive)
	}
	if task.StartBlock != 1 || task.EndBlock != 10 {
		t.Errorf("task range = [%d,%d], want [1,10]", task.StartBlock, task.EndBlock)
	}
	if task.Attempts != 1 {
		t.Errorf("task.Attempts = %d, want 1", task.Attempts)
	}

	// No more pending tasks.
	if dl.PendingTasks() != 0 {
		t.Errorf("PendingTasks = %d, want 0", dl.PendingTasks())
	}
	if dl.ActiveTasks() != 1 {
		t.Errorf("ActiveTasks = %d, want 1", dl.ActiveTasks())
	}
}

func TestBlockDownloader_AssignTask_NoPending(t *testing.T) {
	dl := NewBlockDownloader(testBlockDownloaderConfig())

	task := dl.AssignTask("peer-A")
	if task != nil {
		t.Errorf("AssignTask on empty: got %v, want nil", task)
	}
}

func TestBlockDownloader_AssignTask_ConcurrentLimit(t *testing.T) {
	cfg := testBlockDownloaderConfig()
	cfg.MaxConcurrent = 2
	cfg.BlockBatchSize = 5
	dl := NewBlockDownloader(cfg)

	dl.QueueRange(1, 20) // 4 tasks

	dl.AssignTask("peer-A")
	dl.AssignTask("peer-B")

	// Third assignment should be blocked by MaxConcurrent.
	task := dl.AssignTask("peer-C")
	if task != nil {
		t.Error("expected nil when MaxConcurrent reached")
	}
	if dl.ActiveTasks() != 2 {
		t.Errorf("ActiveTasks = %d, want 2", dl.ActiveTasks())
	}
}

func TestBlockDownloader_CompleteTask(t *testing.T) {
	dl := NewBlockDownloader(testBlockDownloaderConfig())
	dl.QueueRange(1, 10)

	task := dl.AssignTask("peer-A")

	hashes := []types.Hash{
		types.HexToHash("aabb"),
		types.HexToHash("ccdd"),
	}
	if err := dl.CompleteTask(task.ID, hashes); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	if dl.ActiveTasks() != 0 {
		t.Errorf("ActiveTasks = %d, want 0", dl.ActiveTasks())
	}
	// 10 blocks completed (1 through 10 inclusive).
	if got := dl.CompletedBlocks(); got != 10 {
		t.Errorf("CompletedBlocks = %d, want 10", got)
	}
}

func TestBlockDownloader_CompleteTask_NotFound(t *testing.T) {
	dl := NewBlockDownloader(testBlockDownloaderConfig())

	if err := dl.CompleteTask("nonexistent", nil); err != ErrTaskNotFound {
		t.Errorf("CompleteTask(unknown): got %v, want ErrTaskNotFound", err)
	}
}

func TestBlockDownloader_CompleteTask_NotActive(t *testing.T) {
	dl := NewBlockDownloader(testBlockDownloaderConfig())
	dl.QueueRange(1, 10)

	// Task is pending, not active.
	dl.mu.RLock()
	var taskID string
	for id := range dl.tasks {
		taskID = id
		break
	}
	dl.mu.RUnlock()

	if err := dl.CompleteTask(taskID, nil); err != ErrTaskNotActive {
		t.Errorf("CompleteTask(pending): got %v, want ErrTaskNotActive", err)
	}
}

func TestBlockDownloader_FailTask_Retry(t *testing.T) {
	cfg := testBlockDownloaderConfig()
	cfg.RetryLimit = 3
	dl := NewBlockDownloader(cfg)
	dl.QueueRange(1, 10)

	// First attempt.
	task := dl.AssignTask("peer-A")
	if err := dl.FailTask(task.ID, "timeout"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	// Task should be back to pending.
	if dl.PendingTasks() != 1 {
		t.Errorf("PendingTasks after fail = %d, want 1", dl.PendingTasks())
	}
	if dl.ActiveTasks() != 0 {
		t.Errorf("ActiveTasks after fail = %d, want 0", dl.ActiveTasks())
	}

	// Second attempt by a different peer.
	task2 := dl.AssignTask("peer-B")
	if task2 == nil {
		t.Fatal("AssignTask returned nil on retry")
	}
	if task2.Attempts != 2 {
		t.Errorf("task.Attempts = %d, want 2", task2.Attempts)
	}
}

func TestBlockDownloader_FailTask_ExhaustedRetries(t *testing.T) {
	cfg := testBlockDownloaderConfig()
	cfg.RetryLimit = 2
	dl := NewBlockDownloader(cfg)
	dl.QueueRange(1, 10)

	// Exhaust retries.
	for i := 0; i < cfg.RetryLimit; i++ {
		task := dl.AssignTask("peer-A")
		if task == nil {
			t.Fatalf("AssignTask returned nil on attempt %d", i+1)
		}
		dl.FailTask(task.ID, fmt.Sprintf("error-%d", i+1))
	}

	// Task should now be permanently failed, not pending.
	if dl.PendingTasks() != 0 {
		t.Errorf("PendingTasks = %d, want 0 (retries exhausted)", dl.PendingTasks())
	}
	if dl.ActiveTasks() != 0 {
		t.Errorf("ActiveTasks = %d, want 0", dl.ActiveTasks())
	}

	// No task to assign.
	if task := dl.AssignTask("peer-B"); task != nil {
		t.Error("expected nil after retries exhausted")
	}
}

func TestBlockDownloader_FailTask_NotFound(t *testing.T) {
	dl := NewBlockDownloader(testBlockDownloaderConfig())

	if err := dl.FailTask("nonexistent", "err"); err != ErrTaskNotFound {
		t.Errorf("FailTask(unknown): got %v, want ErrTaskNotFound", err)
	}
}

func TestBlockDownloader_FailTask_NotActive(t *testing.T) {
	dl := NewBlockDownloader(testBlockDownloaderConfig())
	dl.QueueRange(1, 10)

	dl.mu.RLock()
	var taskID string
	for id := range dl.tasks {
		taskID = id
		break
	}
	dl.mu.RUnlock()

	if err := dl.FailTask(taskID, "err"); err != ErrTaskNotActive {
		t.Errorf("FailTask(pending): got %v, want ErrTaskNotActive", err)
	}
}

func TestBlockDownloader_PeerAssignments(t *testing.T) {
	cfg := testBlockDownloaderConfig()
	cfg.BlockBatchSize = 5
	cfg.MaxConcurrent = 10
	dl := NewBlockDownloader(cfg)
	dl.QueueRange(1, 15) // 3 tasks

	dl.AssignTask("peer-A")
	dl.AssignTask("peer-A")
	dl.AssignTask("peer-B")

	assignments := dl.PeerAssignments()
	if assignments["peer-A"] != 2 {
		t.Errorf("peer-A assignments = %d, want 2", assignments["peer-A"])
	}
	if assignments["peer-B"] != 1 {
		t.Errorf("peer-B assignments = %d, want 1", assignments["peer-B"])
	}
}

func TestBlockDownloader_Reset(t *testing.T) {
	dl := NewBlockDownloader(testBlockDownloaderConfig())
	dl.QueueRange(1, 20)

	task := dl.AssignTask("peer-A")
	dl.CompleteTask(task.ID, nil)

	if dl.CompletedBlocks() == 0 {
		t.Error("CompletedBlocks should be > 0 before reset")
	}

	dl.Reset()

	if dl.PendingTasks() != 0 {
		t.Errorf("PendingTasks after reset = %d, want 0", dl.PendingTasks())
	}
	if dl.ActiveTasks() != 0 {
		t.Errorf("ActiveTasks after reset = %d, want 0", dl.ActiveTasks())
	}
	if dl.CompletedBlocks() != 0 {
		t.Errorf("CompletedBlocks after reset = %d, want 0", dl.CompletedBlocks())
	}
}

func TestBlockDownloader_AssignTaskPicksLowestBlock(t *testing.T) {
	cfg := testBlockDownloaderConfig()
	cfg.BlockBatchSize = 5
	dl := NewBlockDownloader(cfg)

	// Queue two ranges to ensure ordering.
	dl.QueueRange(50, 54)
	dl.QueueRange(10, 14)

	task := dl.AssignTask("peer-A")
	if task == nil {
		t.Fatal("AssignTask returned nil")
	}
	if task.StartBlock != 10 {
		t.Errorf("first assigned task starts at %d, want 10", task.StartBlock)
	}
}

func TestBlockDownloader_CompletedBlocksAccumulates(t *testing.T) {
	cfg := testBlockDownloaderConfig()
	cfg.BlockBatchSize = 5
	cfg.MaxConcurrent = 10
	dl := NewBlockDownloader(cfg)
	dl.QueueRange(1, 10) // 2 tasks: [1,5] and [6,10]

	t1 := dl.AssignTask("peer-A")
	t2 := dl.AssignTask("peer-B")

	dl.CompleteTask(t1.ID, nil)
	if got := dl.CompletedBlocks(); got != 5 {
		t.Errorf("after first complete: CompletedBlocks = %d, want 5", got)
	}

	dl.CompleteTask(t2.ID, nil)
	if got := dl.CompletedBlocks(); got != 10 {
		t.Errorf("after second complete: CompletedBlocks = %d, want 10", got)
	}
}

func TestBlockDownloader_Concurrency(t *testing.T) {
	cfg := BlockDownloaderConfig{
		MaxConcurrent:  100,
		BlockBatchSize: 5,
		RetryLimit:     3,
		Timeout:        10,
	}
	dl := NewBlockDownloader(cfg)
	dl.QueueRange(1, 250) // 50 tasks

	const workers = 20
	var wg gosync.WaitGroup

	// Concurrent assigns and completes.
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			peerID := fmt.Sprintf("peer-%d", i)
			for {
				task := dl.AssignTask(peerID)
				if task == nil {
					return
				}
				dl.CompleteTask(task.ID, nil)
			}
		}(i)
	}
	wg.Wait()

	if got := dl.CompletedBlocks(); got != 250 {
		t.Errorf("CompletedBlocks = %d, want 250", got)
	}
	if dl.PendingTasks() != 0 {
		t.Errorf("PendingTasks = %d, want 0", dl.PendingTasks())
	}
	if dl.ActiveTasks() != 0 {
		t.Errorf("ActiveTasks = %d, want 0", dl.ActiveTasks())
	}
}

func TestBlockDownloader_ConcurrentQueueAndAssign(t *testing.T) {
	cfg := BlockDownloaderConfig{
		MaxConcurrent:  50,
		BlockBatchSize: 10,
		RetryLimit:     3,
		Timeout:        10,
	}
	dl := NewBlockDownloader(cfg)

	var wg gosync.WaitGroup

	// Concurrent queuing.
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			defer wg.Done()
			start := uint64(i*100 + 1)
			end := uint64(i*100 + 50)
			dl.QueueRange(start, end)
		}(i)
	}

	// Concurrent reads while queuing.
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			dl.PendingTasks()
			dl.ActiveTasks()
			dl.CompletedBlocks()
			dl.PeerAssignments()
		}()
	}
	wg.Wait()
}

func TestBlockDownloader_ZeroBatchSize(t *testing.T) {
	cfg := testBlockDownloaderConfig()
	cfg.BlockBatchSize = 0
	dl := NewBlockDownloader(cfg)

	// With batch size 0, should default to 1 per task.
	if err := dl.QueueRange(1, 3); err != nil {
		t.Fatalf("QueueRange: %v", err)
	}
	if got := dl.PendingTasks(); got != 3 {
		t.Errorf("PendingTasks = %d, want 3 (one per block)", got)
	}
}
