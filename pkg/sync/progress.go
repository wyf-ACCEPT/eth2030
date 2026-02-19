package sync

import (
	"sync"
	"time"
)

// ProgressStage represents a named stage in the sync pipeline.
type ProgressStage uint8

// Sync pipeline stages.
const (
	StageProgressIdle     ProgressStage = iota // Not syncing.
	StageProgressHeaders                       // Downloading headers.
	StageProgressBodies                        // Downloading block bodies.
	StageProgressReceipts                      // Downloading receipts.
	StageProgressState                         // Downloading state.
	StageProgressBeacon                        // Beacon sync.
	StageProgressSnap                          // Snap sync.
	StageProgressComplete                      // Sync complete.
)

// String returns a human-readable stage name.
func (s ProgressStage) String() string {
	switch s {
	case StageProgressIdle:
		return "idle"
	case StageProgressHeaders:
		return "headers"
	case StageProgressBodies:
		return "bodies"
	case StageProgressReceipts:
		return "receipts"
	case StageProgressState:
		return "state"
	case StageProgressBeacon:
		return "beacon"
	case StageProgressSnap:
		return "snap"
	case StageProgressComplete:
		return "complete"
	default:
		return "unknown"
	}
}

// ProgressInfo holds a snapshot of the current sync progress.
type ProgressInfo struct {
	Stage               ProgressStage
	StartBlock          uint64
	CurrentBlock        uint64
	HighestBlock        uint64
	StartTime           time.Time
	PeersConnected      int
	BytesDownloaded     uint64
	HeadersProcessed    uint64
	BodiesProcessed     uint64
	ReceiptsProcessed   uint64
	StateNodesProcessed uint64
	EstimatedCompletion time.Time
	PercentComplete     float64
}

// ProgressTracker monitors synchronization state. It is safe for
// concurrent use from multiple goroutines.
type ProgressTracker struct {
	mu                  sync.RWMutex
	stage               ProgressStage
	startBlock          uint64
	currentBlock        uint64
	highestBlock        uint64
	startTime           time.Time
	peersConnected      int
	bytesDownloaded     uint64
	headersProcessed    uint64
	bodiesProcessed     uint64
	receiptsProcessed   uint64
	stateNodesProcessed uint64
}

// NewProgressTracker creates a new idle progress tracker.
func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{
		stage: StageProgressIdle,
	}
}

// Start begins tracking sync progress toward the given highest block.
func (pt *ProgressTracker) Start(highestBlock uint64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.highestBlock = highestBlock
	pt.startTime = time.Now()
	pt.stage = StageProgressHeaders
}

// SetStage updates the current sync stage.
func (pt *ProgressTracker) SetStage(stage ProgressStage) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.stage = stage
}

// UpdateBlock sets the current block number.
func (pt *ProgressTracker) UpdateBlock(current uint64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.currentBlock = current
}

// RecordBytes adds n downloaded bytes to the counter.
func (pt *ProgressTracker) RecordBytes(n uint64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.bytesDownloaded += n
}

// RecordHeaders adds n processed headers to the counter.
func (pt *ProgressTracker) RecordHeaders(n uint64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.headersProcessed += n
}

// RecordBodies adds n processed bodies to the counter.
func (pt *ProgressTracker) RecordBodies(n uint64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.bodiesProcessed += n
}

// RecordReceipts adds n processed receipts to the counter.
func (pt *ProgressTracker) RecordReceipts(n uint64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.receiptsProcessed += n
}

// RecordStateNodes adds n processed state nodes to the counter.
func (pt *ProgressTracker) RecordStateNodes(n uint64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.stateNodesProcessed += n
}

// SetPeerCount updates the connected peer count.
func (pt *ProgressTracker) SetPeerCount(n int) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.peersConnected = n
}

// GetProgress returns a snapshot of the current sync progress with
// computed fields (PercentComplete, EstimatedCompletion).
func (pt *ProgressTracker) GetProgress() ProgressInfo {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	info := ProgressInfo{
		Stage:               pt.stage,
		StartBlock:          pt.startBlock,
		CurrentBlock:        pt.currentBlock,
		HighestBlock:        pt.highestBlock,
		StartTime:           pt.startTime,
		PeersConnected:      pt.peersConnected,
		BytesDownloaded:     pt.bytesDownloaded,
		HeadersProcessed:    pt.headersProcessed,
		BodiesProcessed:     pt.bodiesProcessed,
		ReceiptsProcessed:   pt.receiptsProcessed,
		StateNodesProcessed: pt.stateNodesProcessed,
	}

	// Compute percent complete.
	total := pt.highestBlock - pt.startBlock
	if total > 0 {
		done := pt.currentBlock - pt.startBlock
		if done > total {
			done = total
		}
		info.PercentComplete = float64(done) / float64(total) * 100.0
	} else if pt.stage == StageProgressComplete {
		info.PercentComplete = 100.0
	}

	// Estimate completion time based on blocks per second.
	if !pt.startTime.IsZero() && pt.currentBlock > pt.startBlock && pt.currentBlock < pt.highestBlock {
		elapsed := time.Since(pt.startTime).Seconds()
		done := float64(pt.currentBlock - pt.startBlock)
		remaining := float64(pt.highestBlock - pt.currentBlock)
		if done > 0 && elapsed > 0 {
			bps := done / elapsed
			secsLeft := remaining / bps
			info.EstimatedCompletion = time.Now().Add(time.Duration(secsLeft * float64(time.Second)))
		}
	}

	return info
}

// IsComplete returns whether sync has reached the complete stage.
func (pt *ProgressTracker) IsComplete() bool {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.stage == StageProgressComplete
}

// BlocksPerSecond returns the throughput in blocks per second since
// Start was called. Returns 0 if no blocks have been processed or
// tracking has not started.
func (pt *ProgressTracker) BlocksPerSecond() float64 {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	if pt.startTime.IsZero() || pt.currentBlock <= pt.startBlock {
		return 0
	}
	elapsed := time.Since(pt.startTime).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(pt.currentBlock-pt.startBlock) / elapsed
}

// Reset clears all tracking state and returns to the idle stage.
func (pt *ProgressTracker) Reset() {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.stage = StageProgressIdle
	pt.startBlock = 0
	pt.currentBlock = 0
	pt.highestBlock = 0
	pt.startTime = time.Time{}
	pt.peersConnected = 0
	pt.bytesDownloaded = 0
	pt.headersProcessed = 0
	pt.bodiesProcessed = 0
	pt.receiptsProcessed = 0
	pt.stateNodesProcessed = 0
}
