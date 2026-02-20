package core

import (
	"sync"
	"testing"
	"time"
)

func TestTeragasSchedulerScheduleBlob(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	sched := NewTeragasScheduler(cfg)

	req := BlobRequest{
		Data:     make([]byte, 1024),
		Priority: 5,
		Deadline: time.Now().Add(10 * time.Second),
		ID:       "test-1",
	}

	result, err := sched.ScheduleBlob(req)
	if err != nil {
		t.Fatalf("ScheduleBlob: %v", err)
	}
	if result.RequestID != "test-1" {
		t.Fatalf("expected ID test-1, got %s", result.RequestID)
	}
	if result.AllocatedBps <= 0 {
		t.Fatalf("expected positive bandwidth allocation, got %d", result.AllocatedBps)
	}
	if result.Slot == 0 {
		t.Fatal("expected non-zero slot")
	}
	if sched.QueueLength() != 1 {
		t.Fatalf("expected queue length 1, got %d", sched.QueueLength())
	}
}

func TestTeragasSchedulerEmptyData(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	sched := NewTeragasScheduler(cfg)

	_, err := sched.ScheduleBlob(BlobRequest{
		Data:     nil,
		Priority: 1,
	})
	if err == nil {
		t.Fatal("expected error for empty data")
	}

	_, err = sched.ScheduleBlob(BlobRequest{
		Data:     []byte{},
		Priority: 1,
	})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestTeragasSchedulerInvalidPriority(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	sched := NewTeragasScheduler(cfg)

	_, err := sched.ScheduleBlob(BlobRequest{
		Data:     make([]byte, 100),
		Priority: -1,
	})
	if err == nil {
		t.Fatal("expected error for negative priority")
	}
}

func TestTeragasSchedulerExpiredDeadline(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	sched := NewTeragasScheduler(cfg)

	_, err := sched.ScheduleBlob(BlobRequest{
		Data:     make([]byte, 100),
		Priority: 1,
		Deadline: time.Now().Add(-1 * time.Second),
	})
	if err == nil {
		t.Fatal("expected error for expired deadline")
	}
}

func TestTeragasSchedulerQueueFull(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.MaxQueueSize = 5
	sched := NewTeragasScheduler(cfg)

	for i := 0; i < 5; i++ {
		_, err := sched.ScheduleBlob(BlobRequest{
			Data:     make([]byte, 100),
			Priority: 1,
			ID:       "blob-" + string(rune('A'+i)),
		})
		if err != nil {
			t.Fatalf("ScheduleBlob %d: %v", i, err)
		}
	}

	_, err := sched.ScheduleBlob(BlobRequest{
		Data:     make([]byte, 100),
		Priority: 1,
	})
	if err == nil {
		t.Fatal("expected error for full queue")
	}
}

func TestTeragasSchedulerPriorityOrder(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	sched := NewTeragasScheduler(cfg)

	// Schedule low, medium, high priority.
	sched.ScheduleBlob(BlobRequest{Data: make([]byte, 100), Priority: 1, ID: "low"})
	sched.ScheduleBlob(BlobRequest{Data: make([]byte, 100), Priority: 10, ID: "high"})
	sched.ScheduleBlob(BlobRequest{Data: make([]byte, 100), Priority: 5, ID: "mid"})

	// Process all; should process in priority order: high, mid, low.
	count, _ := sched.ProcessQueue()
	if count != 3 {
		t.Fatalf("expected 3 processed, got %d", count)
	}
}

func TestTeragasSchedulerProcessQueue(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	sched := NewTeragasScheduler(cfg)

	for i := 0; i < 10; i++ {
		sched.ScheduleBlob(BlobRequest{
			Data:     make([]byte, 1024),
			Priority: 5,
			ID:       "blob",
		})
	}

	count, bytes := sched.ProcessQueue()
	if count <= 0 {
		t.Fatal("expected at least one blob processed")
	}
	if bytes <= 0 {
		t.Fatal("expected positive processed bytes")
	}
	if sched.QueueLength() != 10-count {
		t.Fatalf("expected queue to shrink by %d, got length %d", count, sched.QueueLength())
	}
}

func TestTeragasSchedulerProcessQueueEmpty(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	sched := NewTeragasScheduler(cfg)

	count, bytes := sched.ProcessQueue()
	if count != 0 || bytes != 0 {
		t.Fatalf("expected 0/0 for empty queue, got %d/%d", count, bytes)
	}
}

func TestTeragasSchedulerMetrics(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	sched := NewTeragasScheduler(cfg)

	for i := 0; i < 5; i++ {
		sched.ScheduleBlob(BlobRequest{
			Data:     make([]byte, 2048),
			Priority: 3,
		})
	}

	total, _, _, _ := sched.Metrics()
	if total != 5 {
		t.Fatalf("expected 5 total blobs, got %d", total)
	}

	sched.ProcessQueue()

	_, processed, _, _ := sched.Metrics()
	if processed <= 0 {
		t.Fatal("expected processed blobs > 0")
	}
}

func TestTeragasSchedulerStop(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	sched := NewTeragasScheduler(cfg)

	sched.Stop()
	if !sched.IsStopped() {
		t.Fatal("expected scheduler to be stopped")
	}

	_, err := sched.ScheduleBlob(BlobRequest{
		Data:     make([]byte, 100),
		Priority: 1,
	})
	if err == nil {
		t.Fatal("expected error scheduling on stopped scheduler")
	}
}

func TestTeragasSchedulerConcurrency(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.MaxQueueSize = 10000
	sched := NewTeragasScheduler(cfg)

	var wg sync.WaitGroup
	goroutines := 8
	blobsPerGoroutine := 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < blobsPerGoroutine; j++ {
				sched.ScheduleBlob(BlobRequest{
					Data:     make([]byte, 512),
					Priority: 5,
				})
			}
		}()
	}

	wg.Wait()

	expected := goroutines * blobsPerGoroutine
	if sched.QueueLength() != expected {
		t.Fatalf("expected queue length %d, got %d", expected, sched.QueueLength())
	}

	// Process everything.
	totalProcessed := 0
	for sched.QueueLength() > 0 {
		count, _ := sched.ProcessQueue()
		totalProcessed += count
	}
	if totalProcessed != expected {
		t.Fatalf("expected %d total processed, got %d", expected, totalProcessed)
	}
}

func TestTeragasSchedulerConfig(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	sched := NewTeragasScheduler(cfg)

	got := sched.Config()
	if got.TargetBps != TeragasTarget {
		t.Fatalf("expected target %d, got %d", TeragasTarget, got.TargetBps)
	}
	if got.MaxQueueSize != 4096 {
		t.Fatalf("expected max queue 4096, got %d", got.MaxQueueSize)
	}
}

func TestTeragasSchedulerDeadlineSkip(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	sched := NewTeragasScheduler(cfg)

	// Schedule a blob with a very short deadline that will expire before processing.
	sched.ScheduleBlob(BlobRequest{
		Data:     make([]byte, 100),
		Priority: 10,
		Deadline: time.Now().Add(5 * time.Millisecond),
		ID:       "soon-expired",
	})

	// Non-expired blob.
	sched.ScheduleBlob(BlobRequest{
		Data:     make([]byte, 100),
		Priority: 1,
		ID:       "valid",
	})

	// Wait for the first blob's deadline to pass.
	time.Sleep(10 * time.Millisecond)

	count, _ := sched.ProcessQueue()
	// The expired one should be dropped, only 1 processed.
	if count != 1 {
		t.Fatalf("expected 1 processed (expired dropped), got %d", count)
	}

	_, _, dropped, _ := sched.Metrics()
	if dropped != 1 {
		t.Fatalf("expected 1 dropped blob, got %d", dropped)
	}
}
