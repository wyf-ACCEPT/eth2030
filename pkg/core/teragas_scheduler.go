// Package core - teragas_scheduler.go implements scheduling for teragas L2
// throughput targeting 1 Gbyte/sec. It manages a priority queue of blob
// requests, enforces bandwidth limits, and produces scheduling decisions
// with estimated delivery times and allocated bandwidth.
package core

import (
	"container/heap"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Teragas scheduler errors.
var (
	ErrTeragasQueueFull        = errors.New("core/teragas: scheduling queue is full")
	ErrTeragasDeadlineExpired  = errors.New("core/teragas: blob request deadline has expired")
	ErrTeragasInvalidPriority  = errors.New("core/teragas: priority must be >= 0")
	ErrTeragasEmptyData        = errors.New("core/teragas: blob request data is empty")
	ErrTeragasBandwidthZero    = errors.New("core/teragas: max bandwidth must be > 0")
	ErrTeragasSchedulerStopped = errors.New("core/teragas: scheduler is stopped")
)

// TeragasTarget is the L2 bandwidth target: 1 GiB/sec.
const TeragasTarget int64 = 1 << 30

// SchedulerConfig configures the teragas scheduler.
type SchedulerConfig struct {
	// MaxQueueSize is the maximum number of pending blob requests.
	MaxQueueSize int

	// TargetBps is the target throughput in bytes/sec (default TeragasTarget).
	TargetBps int64

	// DefaultPriority is the priority for requests without an explicit one.
	DefaultPriority int

	// SlotDuration is the duration of a consensus slot.
	SlotDuration time.Duration

	// MaxBlobSize is the maximum size of a single blob.
	MaxBlobSize int64
}

// DefaultSchedulerConfig returns a configuration targeting teragas L2 throughput.
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		MaxQueueSize:    4096,
		TargetBps:       TeragasTarget,
		DefaultPriority: 5,
		SlotDuration:    12 * time.Second,
		MaxBlobSize:     4 * 1024 * 1024, // 4 MiB
	}
}

// BlobRequest represents a request to schedule a blob for processing.
type BlobRequest struct {
	// Data is the blob payload.
	Data []byte

	// Priority determines processing order (higher = more urgent).
	Priority int

	// Deadline is the latest time by which this blob must be processed.
	Deadline time.Time

	// MaxBandwidth is the maximum bytes/sec the requester can accept.
	// 0 means no per-request limit (use scheduler default).
	MaxBandwidth int64

	// ID is an opaque identifier for tracking.
	ID string

	// SubmitTime is when the request was submitted.
	SubmitTime time.Time
}

// ScheduleResult contains the scheduling decision for a blob.
type ScheduleResult struct {
	// Slot is the assigned slot number for processing.
	Slot uint64

	// EstimatedDelivery is the estimated time when processing completes.
	EstimatedDelivery time.Time

	// AllocatedBps is the bandwidth allocated to this blob in bytes/sec.
	AllocatedBps int64

	// QueuePosition is the position in the queue when scheduled.
	QueuePosition int

	// WaitEstimate is the estimated wait time before processing starts.
	WaitEstimate time.Duration

	// RequestID mirrors the BlobRequest.ID for correlation.
	RequestID string
}

// TeragasMetrics tracks aggregate scheduling metrics.
type TeragasMetrics struct {
	TotalBlobs     atomic.Int64
	TotalBytes     atomic.Int64
	ProcessedBlobs atomic.Int64
	ProcessedBytes atomic.Int64
	DroppedBlobs   atomic.Int64
	PeakQueueDepth atomic.Int64

	// Latency tracking uses a simple sum/count for average.
	latencySum   atomic.Int64 // nanoseconds
	latencyCount atomic.Int64
}

// AvgLatency returns the average scheduling latency.
func (m *TeragasMetrics) AvgLatency() time.Duration {
	count := m.latencyCount.Load()
	if count == 0 {
		return 0
	}
	return time.Duration(m.latencySum.Load() / count)
}

// PeakThroughput returns the peak throughput seen.
func (m *TeragasMetrics) PeakThroughput() int64 {
	return m.ProcessedBytes.Load()
}

// queueItem wraps a BlobRequest for the priority queue.
type queueItem struct {
	request   *BlobRequest
	index     int
	seqNumber int64 // tie-breaking for same priority
}

// blobPriorityQueue implements heap.Interface for priority-based scheduling.
type blobPriorityQueue []*queueItem

func (pq blobPriorityQueue) Len() int { return len(pq) }

func (pq blobPriorityQueue) Less(i, j int) bool {
	// Higher priority first; break ties by earlier deadline, then sequence.
	if pq[i].request.Priority != pq[j].request.Priority {
		return pq[i].request.Priority > pq[j].request.Priority
	}
	if !pq[i].request.Deadline.Equal(pq[j].request.Deadline) {
		return pq[i].request.Deadline.Before(pq[j].request.Deadline)
	}
	return pq[i].seqNumber < pq[j].seqNumber
}

func (pq blobPriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *blobPriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*queueItem)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *blobPriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[:n-1]
	return item
}

// TeragasScheduler manages blob scheduling with priority queuing and
// bandwidth-aware allocation for teragas L2 throughput.
type TeragasScheduler struct {
	mu       sync.Mutex
	config   SchedulerConfig
	queue    blobPriorityQueue
	metrics  TeragasMetrics
	stopped  atomic.Bool
	nextSeq  int64
	nextSlot uint64

	// allocatedBps tracks the current bandwidth allocation.
	allocatedBps int64
}

// NewTeragasScheduler creates a new teragas scheduler.
func NewTeragasScheduler(config SchedulerConfig) *TeragasScheduler {
	if config.TargetBps <= 0 {
		config.TargetBps = TeragasTarget
	}
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = 4096
	}
	if config.SlotDuration <= 0 {
		config.SlotDuration = 12 * time.Second
	}
	if config.MaxBlobSize <= 0 {
		config.MaxBlobSize = 4 * 1024 * 1024
	}

	ts := &TeragasScheduler{
		config:   config,
		queue:    make(blobPriorityQueue, 0, config.MaxQueueSize),
		nextSlot: 1,
	}
	heap.Init(&ts.queue)
	return ts
}

// ScheduleBlob enqueues a blob request and returns a scheduling decision.
func (ts *TeragasScheduler) ScheduleBlob(req BlobRequest) (ScheduleResult, error) {
	if ts.stopped.Load() {
		return ScheduleResult{}, ErrTeragasSchedulerStopped
	}
	if len(req.Data) == 0 {
		return ScheduleResult{}, ErrTeragasEmptyData
	}
	if req.Priority < 0 {
		return ScheduleResult{}, ErrTeragasInvalidPriority
	}
	if !req.Deadline.IsZero() && time.Now().After(req.Deadline) {
		return ScheduleResult{}, ErrTeragasDeadlineExpired
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	if len(ts.queue) >= ts.config.MaxQueueSize {
		ts.metrics.DroppedBlobs.Add(1)
		return ScheduleResult{}, ErrTeragasQueueFull
	}

	if req.SubmitTime.IsZero() {
		req.SubmitTime = time.Now()
	}

	ts.nextSeq++
	item := &queueItem{
		request:   &req,
		seqNumber: ts.nextSeq,
	}
	heap.Push(&ts.queue, item)

	ts.metrics.TotalBlobs.Add(1)
	ts.metrics.TotalBytes.Add(int64(len(req.Data)))

	queueDepth := int64(len(ts.queue))
	if queueDepth > ts.metrics.PeakQueueDepth.Load() {
		ts.metrics.PeakQueueDepth.Store(queueDepth)
	}

	// Compute allocation.
	allocBps := ts.computeAllocation(int64(len(req.Data)), req.MaxBandwidth)
	deliveryTime := ts.estimateDelivery(int64(len(req.Data)), allocBps)
	waitEstimate := ts.estimateWait(item.index)

	result := ScheduleResult{
		Slot:              ts.nextSlot,
		EstimatedDelivery: deliveryTime,
		AllocatedBps:      allocBps,
		QueuePosition:     item.index,
		WaitEstimate:      waitEstimate,
		RequestID:         req.ID,
	}

	return result, nil
}

// computeAllocation determines the bandwidth allocation for a blob.
func (ts *TeragasScheduler) computeAllocation(blobSize, maxBandwidth int64) int64 {
	// Fair share: total bandwidth / (queue size + 1).
	queueSize := int64(len(ts.queue))
	if queueSize <= 0 {
		queueSize = 1
	}
	fairShare := ts.config.TargetBps / queueSize

	// Apply per-request cap if specified.
	if maxBandwidth > 0 && maxBandwidth < fairShare {
		fairShare = maxBandwidth
	}

	// Ensure at least enough bandwidth to deliver within one slot.
	minBps := blobSize / int64(ts.config.SlotDuration.Seconds())
	if minBps <= 0 {
		minBps = 1
	}
	if fairShare < minBps {
		fairShare = minBps
	}

	return fairShare
}

// estimateDelivery computes the estimated delivery time based on blob size
// and allocated bandwidth.
func (ts *TeragasScheduler) estimateDelivery(blobSize, allocBps int64) time.Time {
	if allocBps <= 0 {
		allocBps = 1
	}
	transferSec := float64(blobSize) / float64(allocBps)
	return time.Now().Add(time.Duration(transferSec * float64(time.Second)))
}

// estimateWait estimates how long a request will wait at the given queue position.
func (ts *TeragasScheduler) estimateWait(position int) time.Duration {
	if position <= 0 {
		return 0
	}
	// Estimate: each position adds ~1ms per MiB of average queue item.
	// Simplified: position * slotDuration / maxQueueSize.
	slotSec := ts.config.SlotDuration.Seconds()
	waitSec := float64(position) * slotSec / float64(ts.config.MaxQueueSize)
	return time.Duration(waitSec * float64(time.Second))
}

// ProcessQueue dequeues and "processes" blob requests up to the bandwidth
// limit for one slot. Returns the number of blobs processed and total bytes.
func (ts *TeragasScheduler) ProcessQueue() (processedCount int, processedBytes int64) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Bandwidth budget for one slot.
	budget := ts.config.TargetBps * int64(ts.config.SlotDuration.Seconds())
	var totalBytes int64

	for ts.queue.Len() > 0 {
		// Peek at highest priority item.
		item := ts.queue[0]
		blobSize := int64(len(item.request.Data))

		// Check if adding this blob exceeds the budget.
		if totalBytes+blobSize > budget && processedCount > 0 {
			break
		}

		// Check deadline.
		if !item.request.Deadline.IsZero() && time.Now().After(item.request.Deadline) {
			// Expired; drop it.
			heap.Pop(&ts.queue)
			ts.metrics.DroppedBlobs.Add(1)
			continue
		}

		heap.Pop(&ts.queue)
		totalBytes += blobSize
		processedCount++

		// Record latency.
		latency := time.Since(item.request.SubmitTime)
		ts.metrics.latencySum.Add(latency.Nanoseconds())
		ts.metrics.latencyCount.Add(1)
	}

	ts.metrics.ProcessedBlobs.Add(int64(processedCount))
	ts.metrics.ProcessedBytes.Add(totalBytes)

	if processedCount > 0 {
		ts.nextSlot++
	}

	return processedCount, totalBytes
}

// QueueLength returns the current queue depth.
func (ts *TeragasScheduler) QueueLength() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.queue.Len()
}

// Metrics returns a snapshot of scheduler metrics.
func (ts *TeragasScheduler) Metrics() (total, processed, dropped int64, avgLatency time.Duration) {
	return ts.metrics.TotalBlobs.Load(),
		ts.metrics.ProcessedBlobs.Load(),
		ts.metrics.DroppedBlobs.Load(),
		ts.metrics.AvgLatency()
}

// Stop stops the scheduler, rejecting new requests.
func (ts *TeragasScheduler) Stop() {
	ts.stopped.Store(true)
}

// IsStopped returns whether the scheduler is stopped.
func (ts *TeragasScheduler) IsStopped() bool {
	return ts.stopped.Load()
}

// Config returns the scheduler configuration.
func (ts *TeragasScheduler) Config() SchedulerConfig {
	return ts.config
}
