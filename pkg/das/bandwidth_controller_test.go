package das

import (
	"sync"
	"testing"
	"time"
)

func TestTokenBucketAllocate(t *testing.T) {
	bucket, err := NewTokenBucket(1000, 2000) // 1000 bytes/sec, 2000 capacity
	if err != nil {
		t.Fatalf("NewTokenBucket: %v", err)
	}

	// Bucket starts full at capacity=2000, so allocating 1500 should succeed.
	ok, wait := bucket.Allocate(1500)
	if !ok {
		t.Fatalf("expected allocation to succeed, wait=%v", wait)
	}
	if wait != 0 {
		t.Fatalf("expected zero wait, got %v", wait)
	}

	// 500 tokens remaining; allocating 600 should fail.
	ok, wait = bucket.Allocate(600)
	if ok {
		t.Fatal("expected allocation to fail with insufficient tokens")
	}
	if wait <= 0 {
		t.Fatal("expected positive wait duration")
	}

	// Allocating 400 should still succeed (500 remaining).
	ok, _ = bucket.Allocate(400)
	if !ok {
		t.Fatal("expected 400-byte allocation to succeed")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	bucket, err := NewTokenBucket(10000, 20000) // 10KB/sec
	if err != nil {
		t.Fatalf("NewTokenBucket: %v", err)
	}

	// Drain the bucket.
	bucket.Allocate(20000)

	avail := bucket.Available()
	if avail > 1000 {
		t.Fatalf("expected near-zero tokens after drain, got %d", avail)
	}

	// Simulate time passing by adjusting LastRefill.
	bucket.mu.Lock()
	bucket.LastRefill = time.Now().Add(-1 * time.Second)
	bucket.mu.Unlock()

	avail = bucket.Available()
	// After 1 second at 10000/sec, should have ~10000 tokens.
	if avail < 9000 || avail > 20000 {
		t.Fatalf("expected ~10000 tokens after 1s refill, got %d", avail)
	}
}

func TestTokenBucketUtilization(t *testing.T) {
	bucket, err := NewTokenBucket(1000, 2000)
	if err != nil {
		t.Fatalf("NewTokenBucket: %v", err)
	}

	// Full bucket = 0 utilization.
	util := bucket.Utilization()
	if util > 0.01 {
		t.Fatalf("expected ~0 utilization when full, got %f", util)
	}

	// Drain half.
	bucket.Allocate(1000)
	util = bucket.Utilization()
	if util < 0.4 || util > 0.6 {
		t.Fatalf("expected ~0.5 utilization, got %f", util)
	}
}

func TestNewTokenBucketErrors(t *testing.T) {
	_, err := NewTokenBucket(0, 100)
	if err == nil {
		t.Fatal("expected error for zero rate")
	}

	_, err = NewTokenBucket(-1, 100)
	if err == nil {
		t.Fatal("expected error for negative rate")
	}
}

func TestBandwidthControllerReserve(t *testing.T) {
	policy := DefaultBandwidthPolicy()
	bc, err := NewBandwidthController(policy)
	if err != nil {
		t.Fatalf("NewBandwidthController: %v", err)
	}

	// Reserve within limits.
	deadline := time.Now().Add(5 * time.Second)
	res, err := bc.Reserve(1024, deadline, "peer-1")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if res.Size != 1024 {
		t.Fatalf("expected size 1024, got %d", res.Size)
	}
	if res.PeerID != "peer-1" {
		t.Fatalf("expected peer-1, got %s", res.PeerID)
	}

	// Consume the reservation.
	if err := res.Consume(); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if !res.IsConsumed() {
		t.Fatal("expected reservation to be consumed")
	}

	// Double-consume should fail.
	if err := res.Consume(); err == nil {
		t.Fatal("expected error on double consume")
	}

	// Expired deadline should fail.
	_, err = bc.Reserve(1024, time.Now().Add(-1*time.Second), "peer-2")
	if err == nil {
		t.Fatal("expected error for expired deadline")
	}
}

func TestBandwidthControllerReserveTooLarge(t *testing.T) {
	policy := DefaultBandwidthPolicy()
	bc, err := NewBandwidthController(policy)
	if err != nil {
		t.Fatalf("NewBandwidthController: %v", err)
	}

	// Exceed max allocation size.
	deadline := time.Now().Add(5 * time.Second)
	_, err = bc.Reserve(policy.MaxAllocationSize+1, deadline, "peer-x")
	if err == nil {
		t.Fatal("expected error for oversized reservation")
	}
}

func TestBandwidthControllerMonitor(t *testing.T) {
	policy := DefaultBandwidthPolicy()
	bc, err := NewBandwidthController(policy)
	if err != nil {
		t.Fatalf("NewBandwidthController: %v", err)
	}

	// Perform some allocations.
	for i := 0; i < 10; i++ {
		_ = bc.AllocatePeer("peer-monitor", 1024)
	}

	report := bc.MonitorThroughput()
	if report.WindowSize != 10*time.Second {
		t.Fatalf("expected 10s window, got %v", report.WindowSize)
	}
	// We allocated 10*1024 = 10240 bytes, so average should be positive.
	if report.AverageBps <= 0 {
		t.Fatalf("expected positive average bps, got %f", report.AverageBps)
	}
}

func TestAdaptiveRateLimiter(t *testing.T) {
	ar, err := NewAdaptiveRateLimiter(100, 10000)
	if err != nil {
		t.Fatalf("NewAdaptiveRateLimiter: %v", err)
	}

	initial := ar.CurrentRate()
	if initial != 100 {
		t.Fatalf("expected initial rate 100, got %f", initial)
	}

	// Perform successful allocations to trigger increase.
	for i := 0; i < 20; i++ {
		ar.TryAllocate(1)
	}
	afterIncrease := ar.CurrentRate()
	if afterIncrease <= initial {
		t.Fatalf("expected rate to increase after successes, got %f", afterIncrease)
	}
}

func TestAdaptiveRateLimiterDecrease(t *testing.T) {
	ar, err := NewAdaptiveRateLimiter(10, 1000)
	if err != nil {
		t.Fatalf("NewAdaptiveRateLimiter: %v", err)
	}

	// Simulate successes to increase rate.
	for i := 0; i < 20; i++ {
		ar.TryAllocate(1)
	}
	rateAfterIncrease := ar.CurrentRate()

	// Trigger a decrease by requesting too much.
	ar.TryAllocate(999999999)
	rateAfterDecrease := ar.CurrentRate()
	if rateAfterDecrease >= rateAfterIncrease {
		t.Fatalf("expected rate decrease, before=%f after=%f", rateAfterIncrease, rateAfterDecrease)
	}
}

func TestAdaptiveRateLimiterInvalidParams(t *testing.T) {
	_, err := NewAdaptiveRateLimiter(0, 1000)
	if err == nil {
		t.Fatal("expected error for zero minRate")
	}
	_, err = NewAdaptiveRateLimiter(100, 50)
	if err == nil {
		t.Fatal("expected error for minRate > maxRate")
	}
}

func TestPeerBandwidthTracker(t *testing.T) {
	pt := NewPeerBandwidthTracker(10000)

	// Track allocations.
	ok, _ := pt.TrackAllocation("peer-A", 1000)
	if !ok {
		t.Fatal("expected allocation to succeed")
	}
	ok, _ = pt.TrackAllocation("peer-B", 2000)
	if !ok {
		t.Fatal("expected allocation to succeed")
	}

	if pt.PeerCount() != 2 {
		t.Fatalf("expected 2 peers, got %d", pt.PeerCount())
	}

	totalA, allocsA, err := pt.PeerStats("peer-A")
	if err != nil {
		t.Fatalf("PeerStats: %v", err)
	}
	if totalA != 1000 || allocsA != 1 {
		t.Fatalf("peer-A: expected 1000/1, got %d/%d", totalA, allocsA)
	}

	_, _, err = pt.PeerStats("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown peer")
	}
}

func TestPeerBandwidthTrackerPrune(t *testing.T) {
	pt := NewPeerBandwidthTracker(10000)

	pt.TrackAllocation("stale-peer", 100)
	pt.TrackAllocation("active-peer", 100)

	// Make stale-peer appear old.
	pt.mu.Lock()
	pt.peers["stale-peer"].lastActive = time.Now().Add(-2 * time.Hour)
	pt.mu.Unlock()

	pruned := pt.PruneStalePeers(1 * time.Hour)
	if pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", pruned)
	}
	if pt.PeerCount() != 1 {
		t.Fatalf("expected 1 remaining peer, got %d", pt.PeerCount())
	}
}

func TestBandwidthPolicyEnforcement(t *testing.T) {
	policy := DefaultBandwidthPolicy()
	policy.MinAllocationSize = 100
	policy.MaxAllocationSize = 5000

	bc, err := NewBandwidthController(policy)
	if err != nil {
		t.Fatalf("NewBandwidthController: %v", err)
	}

	// Too small.
	err = bc.AllocatePeer("peer-1", 50)
	if err == nil {
		t.Fatal("expected error for allocation below minimum")
	}

	// Too large.
	err = bc.AllocatePeer("peer-1", 10000)
	if err == nil {
		t.Fatal("expected error for allocation above maximum")
	}

	// Within range.
	err = bc.AllocatePeer("peer-1", 1000)
	if err != nil {
		t.Fatalf("expected valid allocation to succeed: %v", err)
	}
}

func TestBandwidthControllerStop(t *testing.T) {
	policy := DefaultBandwidthPolicy()
	bc, err := NewBandwidthController(policy)
	if err != nil {
		t.Fatalf("NewBandwidthController: %v", err)
	}

	bc.Stop()
	if !bc.IsStopped() {
		t.Fatal("expected controller to be stopped")
	}

	err = bc.AllocatePeer("peer-1", 1024)
	if err == nil {
		t.Fatal("expected error after stop")
	}

	_, err = bc.Reserve(1024, time.Now().Add(5*time.Second), "peer-1")
	if err == nil {
		t.Fatal("expected error after stop")
	}
}

func TestBandwidthControllerConcurrency(t *testing.T) {
	policy := DefaultBandwidthPolicy()
	bc, err := NewBandwidthController(policy)
	if err != nil {
		t.Fatalf("NewBandwidthController: %v", err)
	}

	var wg sync.WaitGroup
	goroutines := 16
	allocsPerGoroutine := 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			peerID := "peer-" + string(rune('A'+idx))
			for j := 0; j < allocsPerGoroutine; j++ {
				_ = bc.AllocatePeer(peerID, 512)
			}
		}(i)
	}

	wg.Wait()

	if bc.PeerCount() != goroutines {
		t.Fatalf("expected %d peers, got %d", goroutines, bc.PeerCount())
	}

	// Verify monitoring works after concurrent access.
	report := bc.MonitorThroughput()
	if report.DroppedBytes < 0 {
		t.Fatal("dropped bytes should not be negative")
	}
}

func TestComputeOptimalChunkSize(t *testing.T) {
	// Normal case.
	chunk := ComputeOptimalChunkSize(TeragasBandwidthTarget, 100, 512, 4*1024*1024)
	if chunk < 512 || chunk > 4*1024*1024 {
		t.Fatalf("chunk size %d out of bounds", chunk)
	}

	// Edge: zero target -> min.
	chunk = ComputeOptimalChunkSize(0, 100, 512, 4*1024*1024)
	if chunk != 512 {
		t.Fatalf("expected min chunk size 512, got %d", chunk)
	}

	// Edge: zero latency -> min.
	chunk = ComputeOptimalChunkSize(TeragasBandwidthTarget, 0, 512, 4*1024*1024)
	if chunk != 512 {
		t.Fatalf("expected min chunk size 512, got %d", chunk)
	}
}

func TestReservationExpiry(t *testing.T) {
	r := &Reservation{
		Size:     1024,
		Deadline: time.Now().Add(-1 * time.Second), // already expired
		Granted:  time.Now().Add(-2 * time.Second),
		PeerID:   "peer-1",
	}
	err := r.Consume()
	if err == nil {
		t.Fatal("expected error consuming expired reservation")
	}
}
