package sync

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestProgressTracker_InitialState(t *testing.T) {
	pt := NewProgressTracker()
	p := pt.GetProgress()

	if p.Stage != StageProgressIdle {
		t.Errorf("Stage = %v, want idle", p.Stage)
	}
	if p.CurrentBlock != 0 {
		t.Errorf("CurrentBlock = %d, want 0", p.CurrentBlock)
	}
	if p.HighestBlock != 0 {
		t.Errorf("HighestBlock = %d, want 0", p.HighestBlock)
	}
	if p.PercentComplete != 0 {
		t.Errorf("PercentComplete = %f, want 0", p.PercentComplete)
	}
	if pt.IsComplete() {
		t.Error("should not be complete")
	}
	if pt.BlocksPerSecond() != 0 {
		t.Errorf("BlocksPerSecond = %f, want 0", pt.BlocksPerSecond())
	}
}

func TestProgressTracker_Start(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(1000)

	p := pt.GetProgress()
	if p.Stage != StageProgressHeaders {
		t.Errorf("Stage = %v, want headers", p.Stage)
	}
	if p.HighestBlock != 1000 {
		t.Errorf("HighestBlock = %d, want 1000", p.HighestBlock)
	}
	if p.StartTime.IsZero() {
		t.Error("StartTime should be set after Start()")
	}
}

func TestProgressTracker_UpdateBlock(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(1000)
	pt.UpdateBlock(500)

	p := pt.GetProgress()
	if p.CurrentBlock != 500 {
		t.Errorf("CurrentBlock = %d, want 500", p.CurrentBlock)
	}
}

func TestProgressTracker_StageTransitions(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(1000)

	stages := []ProgressStage{
		StageProgressHeaders,
		StageProgressBodies,
		StageProgressReceipts,
		StageProgressState,
		StageProgressComplete,
	}

	for _, s := range stages {
		pt.SetStage(s)
		p := pt.GetProgress()
		if p.Stage != s {
			t.Errorf("Stage = %v, want %v", p.Stage, s)
		}
	}
}

func TestProgressTracker_PercentComplete(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(1000) // startBlock=0, highestBlock=1000

	pt.UpdateBlock(0)
	p := pt.GetProgress()
	if p.PercentComplete != 0.0 {
		t.Errorf("PercentComplete = %f, want 0.0", p.PercentComplete)
	}

	pt.UpdateBlock(250)
	p = pt.GetProgress()
	if p.PercentComplete != 25.0 {
		t.Errorf("PercentComplete = %f, want 25.0", p.PercentComplete)
	}

	pt.UpdateBlock(500)
	p = pt.GetProgress()
	if p.PercentComplete != 50.0 {
		t.Errorf("PercentComplete = %f, want 50.0", p.PercentComplete)
	}

	pt.UpdateBlock(1000)
	p = pt.GetProgress()
	if p.PercentComplete != 100.0 {
		t.Errorf("PercentComplete = %f, want 100.0", p.PercentComplete)
	}
}

func TestProgressTracker_PercentComplete_ZeroRange(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(0) // highestBlock = startBlock = 0

	p := pt.GetProgress()
	// Zero range, not complete stage => 0%.
	if p.PercentComplete != 0.0 {
		t.Errorf("PercentComplete = %f, want 0.0", p.PercentComplete)
	}

	// Mark complete to check 100%.
	pt.SetStage(StageProgressComplete)
	p = pt.GetProgress()
	if p.PercentComplete != 100.0 {
		t.Errorf("PercentComplete when complete = %f, want 100.0", p.PercentComplete)
	}
}

func TestProgressTracker_RecordCounters(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(1000)

	pt.RecordHeaders(50)
	pt.RecordHeaders(30)
	pt.RecordBodies(40)
	pt.RecordReceipts(20)
	pt.RecordStateNodes(100)
	pt.RecordBytes(5000)
	pt.RecordBytes(3000)

	p := pt.GetProgress()
	if p.HeadersProcessed != 80 {
		t.Errorf("HeadersProcessed = %d, want 80", p.HeadersProcessed)
	}
	if p.BodiesProcessed != 40 {
		t.Errorf("BodiesProcessed = %d, want 40", p.BodiesProcessed)
	}
	if p.ReceiptsProcessed != 20 {
		t.Errorf("ReceiptsProcessed = %d, want 20", p.ReceiptsProcessed)
	}
	if p.StateNodesProcessed != 100 {
		t.Errorf("StateNodesProcessed = %d, want 100", p.StateNodesProcessed)
	}
	if p.BytesDownloaded != 8000 {
		t.Errorf("BytesDownloaded = %d, want 8000", p.BytesDownloaded)
	}
}

func TestProgressTracker_SetPeerCount(t *testing.T) {
	pt := NewProgressTracker()
	pt.SetPeerCount(25)

	p := pt.GetProgress()
	if p.PeersConnected != 25 {
		t.Errorf("PeersConnected = %d, want 25", p.PeersConnected)
	}
}

func TestProgressTracker_BlocksPerSecond(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(10000)

	// No blocks processed yet.
	if bps := pt.BlocksPerSecond(); bps != 0 {
		t.Errorf("BlocksPerSecond = %f, want 0", bps)
	}

	// Simulate some elapsed time by directly setting startTime.
	pt.mu.Lock()
	pt.startTime = time.Now().Add(-10 * time.Second)
	pt.currentBlock = 1000
	pt.mu.Unlock()

	bps := pt.BlocksPerSecond()
	// Should be approximately 100 blocks/sec (1000 blocks / 10 seconds).
	if bps < 90 || bps > 110 {
		t.Errorf("BlocksPerSecond = %f, want ~100", bps)
	}
}

func TestProgressTracker_IsComplete(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(1000)

	if pt.IsComplete() {
		t.Error("should not be complete")
	}

	pt.SetStage(StageProgressComplete)
	if !pt.IsComplete() {
		t.Error("should be complete after setting StageProgressComplete")
	}
}

func TestProgressTracker_Reset(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(1000)
	pt.UpdateBlock(500)
	pt.RecordHeaders(100)
	pt.RecordBodies(50)
	pt.RecordReceipts(25)
	pt.RecordStateNodes(10)
	pt.RecordBytes(999)
	pt.SetPeerCount(5)
	pt.SetStage(StageProgressBodies)

	pt.Reset()

	p := pt.GetProgress()
	if p.Stage != StageProgressIdle {
		t.Errorf("Stage = %v, want idle", p.Stage)
	}
	if p.CurrentBlock != 0 {
		t.Errorf("CurrentBlock = %d, want 0", p.CurrentBlock)
	}
	if p.HighestBlock != 0 {
		t.Errorf("HighestBlock = %d, want 0", p.HighestBlock)
	}
	if p.HeadersProcessed != 0 {
		t.Errorf("HeadersProcessed = %d, want 0", p.HeadersProcessed)
	}
	if p.BodiesProcessed != 0 {
		t.Errorf("BodiesProcessed = %d, want 0", p.BodiesProcessed)
	}
	if p.ReceiptsProcessed != 0 {
		t.Errorf("ReceiptsProcessed = %d, want 0", p.ReceiptsProcessed)
	}
	if p.StateNodesProcessed != 0 {
		t.Errorf("StateNodesProcessed = %d, want 0", p.StateNodesProcessed)
	}
	if p.BytesDownloaded != 0 {
		t.Errorf("BytesDownloaded = %d, want 0", p.BytesDownloaded)
	}
	if p.PeersConnected != 0 {
		t.Errorf("PeersConnected = %d, want 0", p.PeersConnected)
	}
	if !p.StartTime.IsZero() {
		t.Error("StartTime should be zero after reset")
	}
}

func TestProgressTracker_EstimatedCompletion(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(10000)

	// Set start time 10 seconds ago and current block at 1000.
	pt.mu.Lock()
	pt.startTime = time.Now().Add(-10 * time.Second)
	pt.currentBlock = 1000
	pt.mu.Unlock()

	p := pt.GetProgress()
	if p.EstimatedCompletion.IsZero() {
		t.Error("EstimatedCompletion should be set when blocks are being processed")
	}

	// At 100 blocks/sec with 9000 remaining, ETA is ~90 seconds from now.
	eta := time.Until(p.EstimatedCompletion)
	if eta < 80*time.Second || eta > 100*time.Second {
		t.Errorf("EstimatedCompletion ETA = %v, want ~90s", eta)
	}
}

func TestProgressTracker_ConcurrentAccess(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(10000)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			pt.UpdateBlock(uint64(n * 10))
			pt.RecordHeaders(1)
			pt.RecordBodies(1)
			pt.RecordReceipts(1)
			pt.RecordStateNodes(1)
			pt.RecordBytes(100)
			pt.SetPeerCount(n % 50)
			pt.GetProgress()
			pt.BlocksPerSecond()
			pt.IsComplete()
		}(i)
	}
	wg.Wait()

	// Verify counters are consistent.
	p := pt.GetProgress()
	if p.HeadersProcessed != 100 {
		t.Errorf("HeadersProcessed = %d, want 100", p.HeadersProcessed)
	}
	if p.BodiesProcessed != 100 {
		t.Errorf("BodiesProcessed = %d, want 100", p.BodiesProcessed)
	}
	if p.BytesDownloaded != 10000 {
		t.Errorf("BytesDownloaded = %d, want 10000", p.BytesDownloaded)
	}
}

func TestProgressStage_String(t *testing.T) {
	tests := []struct {
		stage ProgressStage
		want  string
	}{
		{StageProgressIdle, "idle"},
		{StageProgressHeaders, "headers"},
		{StageProgressBodies, "bodies"},
		{StageProgressReceipts, "receipts"},
		{StageProgressState, "state"},
		{StageProgressBeacon, "beacon"},
		{StageProgressSnap, "snap"},
		{StageProgressComplete, "complete"},
		{ProgressStage(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.stage.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProgressTracker_NoEstimationWhenNoProgress(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(1000)

	// No UpdateBlock called - currentBlock is still 0 (= startBlock).
	p := pt.GetProgress()
	if !p.EstimatedCompletion.IsZero() {
		t.Error("EstimatedCompletion should be zero when no blocks processed")
	}
}

func TestProgressTracker_NoEstimationWhenComplete(t *testing.T) {
	pt := NewProgressTracker()
	pt.Start(1000)
	pt.UpdateBlock(1000)

	p := pt.GetProgress()
	// currentBlock == highestBlock, so no estimation is produced.
	if !p.EstimatedCompletion.IsZero() {
		t.Error("EstimatedCompletion should be zero when already at highest block")
	}
}

// Suppress unused import warning for fmt in concurrent test.
var _ = fmt.Sprint
