package sync

import (
	"errors"
	"fmt"
	gosync "sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
)

// Block downloader errors.
var (
	ErrInvalidRange    = errors.New("sync: invalid block range (start > end)")
	ErrOverlapping     = errors.New("sync: overlapping range already queued")
	ErrNoTaskAvailable = errors.New("sync: no pending task available")
	ErrTaskNotFound    = errors.New("sync: task not found")
	ErrTaskNotActive   = errors.New("sync: task is not in progress")
	ErrRetryExhausted  = errors.New("sync: retry limit reached")
)

// Task status constants.
const (
	TaskStatusPending    = "pending"
	TaskStatusActive     = "active"
	TaskStatusCompleted  = "completed"
	TaskStatusFailed     = "failed"
)

// BlockDownloaderConfig configures the BlockDownloader.
type BlockDownloaderConfig struct {
	MaxConcurrent  int   // Maximum concurrent download tasks.
	BlockBatchSize int   // Number of blocks per task.
	RetryLimit     int   // Maximum retry attempts per task.
	Timeout        int64 // Timeout in seconds for each download (informational).
}

// DownloadTask represents a single block range download task.
type DownloadTask struct {
	ID         string       // Unique task identifier.
	StartBlock uint64       // First block in the range (inclusive).
	EndBlock   uint64       // Last block in the range (inclusive).
	PeerID     string       // Assigned peer ID (empty if pending).
	Status     string       // Current status: pending, active, completed, failed.
	Attempts   int          // Number of attempts made.
	LastError  string       // Most recent error message.
	Blocks     []types.Hash // Downloaded block hashes (set on completion).
}

// BlockDownloader manages parallel block download tasks with retry logic,
// peer assignment tracking, and completion accounting. Thread-safe.
type BlockDownloader struct {
	mu     gosync.RWMutex
	config BlockDownloaderConfig

	nextID uint64 // Monotonic task ID counter (atomic).
	tasks  map[string]*DownloadTask

	completedBlocks atomic.Uint64
}

// NewBlockDownloader creates a new BlockDownloader with the given config.
func NewBlockDownloader(config BlockDownloaderConfig) *BlockDownloader {
	return &BlockDownloader{
		config: config,
		tasks:  make(map[string]*DownloadTask),
	}
}

// QueueRange queues a block range [start, end] for download. The range is
// split into batches according to BlockBatchSize. Returns an error if
// start > end.
func (d *BlockDownloader) QueueRange(start, end uint64) error {
	if start > end {
		return ErrInvalidRange
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	batchSize := uint64(d.config.BlockBatchSize)
	if batchSize == 0 {
		batchSize = 1
	}

	for s := start; s <= end; {
		e := s + batchSize - 1
		if e > end {
			e = end
		}

		id := d.generateID()
		d.tasks[id] = &DownloadTask{
			ID:         id,
			StartBlock: s,
			EndBlock:   e,
			Status:     TaskStatusPending,
		}

		// Guard against overflow when s is near max uint64.
		if e == end {
			break
		}
		s = e + 1
	}

	return nil
}

// AssignTask assigns the next available pending task to the specified peer.
// Returns the task, or nil if no task is available or the max concurrent
// limit has been reached.
func (d *BlockDownloader) AssignTask(peerID string) *DownloadTask {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check concurrent limit.
	if d.config.MaxConcurrent > 0 && d.activeCountLocked() >= d.config.MaxConcurrent {
		return nil
	}

	// Find the first pending task (lowest start block).
	var best *DownloadTask
	for _, t := range d.tasks {
		if t.Status != TaskStatusPending {
			continue
		}
		if best == nil || t.StartBlock < best.StartBlock {
			best = t
		}
	}

	if best == nil {
		return nil
	}

	best.Status = TaskStatusActive
	best.PeerID = peerID
	best.Attempts++

	// Return a copy.
	cp := *best
	return &cp
}

// CompleteTask marks a task as completed and records the downloaded block hashes.
func (d *BlockDownloader) CompleteTask(taskID string, blocks []types.Hash) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	t, ok := d.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	if t.Status != TaskStatusActive {
		return ErrTaskNotActive
	}

	t.Status = TaskStatusCompleted
	t.Blocks = make([]types.Hash, len(blocks))
	copy(t.Blocks, blocks)

	// Count completed blocks.
	count := t.EndBlock - t.StartBlock + 1
	d.completedBlocks.Add(count)

	return nil
}

// FailTask marks a task as failed. If the task has not exceeded the retry
// limit, it is reset to pending for reassignment. Otherwise it is marked
// as permanently failed.
func (d *BlockDownloader) FailTask(taskID string, reason string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	t, ok := d.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	if t.Status != TaskStatusActive {
		return ErrTaskNotActive
	}

	t.LastError = reason

	if t.Attempts < d.config.RetryLimit {
		// Reset to pending for retry.
		t.Status = TaskStatusPending
		t.PeerID = ""
	} else {
		t.Status = TaskStatusFailed
	}

	return nil
}

// PendingTasks returns the number of tasks in pending status.
func (d *BlockDownloader) PendingTasks() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	count := 0
	for _, t := range d.tasks {
		if t.Status == TaskStatusPending {
			count++
		}
	}
	return count
}

// ActiveTasks returns the number of tasks currently in progress.
func (d *BlockDownloader) ActiveTasks() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.activeCountLocked()
}

// CompletedBlocks returns the total number of blocks downloaded.
func (d *BlockDownloader) CompletedBlocks() uint64 {
	return d.completedBlocks.Load()
}

// PeerAssignments returns a map of peer ID to the number of active tasks
// assigned to each peer.
func (d *BlockDownloader) PeerAssignments() map[string]int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make(map[string]int)
	for _, t := range d.tasks {
		if t.Status == TaskStatusActive && t.PeerID != "" {
			result[t.PeerID]++
		}
	}
	return result
}

// Reset clears all tasks and resets the completed blocks counter.
func (d *BlockDownloader) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.tasks = make(map[string]*DownloadTask)
	d.completedBlocks.Store(0)
}

// activeCountLocked returns the number of active tasks.
// Caller must hold at least a read lock.
func (d *BlockDownloader) activeCountLocked() int {
	count := 0
	for _, t := range d.tasks {
		if t.Status == TaskStatusActive {
			count++
		}
	}
	return count
}

// generateID returns a new unique task ID.
// Caller must hold the write lock.
func (d *BlockDownloader) generateID() string {
	d.nextID++
	return fmt.Sprintf("task-%d", d.nextID)
}
