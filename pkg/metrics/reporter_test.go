package metrics

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// mockBackend records every Report call for test inspection.
type mockBackend struct {
	mu      sync.Mutex
	calls   []map[string]float64
	failErr error // if non-nil, Report returns this error
}

func (m *mockBackend) Report(metrics map[string]float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]float64, len(metrics))
	for k, v := range metrics {
		cp[k] = v
	}
	m.calls = append(m.calls, cp)
	return m.failErr
}

func (m *mockBackend) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockBackend) lastCall() map[string]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return nil
	}
	return m.calls[len(m.calls)-1]
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestNewMetricsReporter(t *testing.T) {
	r := NewMetricsReporter(time.Second)
	if r == nil {
		t.Fatal("NewMetricsReporter returned nil")
	}
	if r.interval != time.Second {
		t.Fatalf("interval = %v, want %v", r.interval, time.Second)
	}
	if r.Running() {
		t.Fatal("reporter should not be running immediately after creation")
	}
}

func TestRecordMetricAndSnapshot(t *testing.T) {
	r := NewMetricsReporter(time.Minute)
	r.RecordMetric("cpu.usage", 42.5)
	r.RecordMetric("mem.used", 1024.0)

	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot length = %d, want 2", len(snap))
	}
	if snap["cpu.usage"] != 42.5 {
		t.Fatalf("cpu.usage = %f, want 42.5", snap["cpu.usage"])
	}
	if snap["mem.used"] != 1024.0 {
		t.Fatalf("mem.used = %f, want 1024", snap["mem.used"])
	}

	// Overwrite existing metric.
	r.RecordMetric("cpu.usage", 99.9)
	snap = r.Snapshot()
	if snap["cpu.usage"] != 99.9 {
		t.Fatalf("overwritten cpu.usage = %f, want 99.9", snap["cpu.usage"])
	}
}

func TestRecordTimer(t *testing.T) {
	r := NewMetricsReporter(time.Minute)
	r.RecordTimer("request.latency", 150*time.Millisecond)

	snap := r.Snapshot()
	if snap["request.latency"] != 150 {
		t.Fatalf("request.latency = %f, want 150", snap["request.latency"])
	}
}

func TestRegisterAndUnregisterBackend(t *testing.T) {
	r := NewMetricsReporter(time.Minute)
	b := &mockBackend{}

	r.RegisterBackend("test", b)
	r.mu.RLock()
	count := len(r.backends)
	r.mu.RUnlock()
	if count != 1 {
		t.Fatalf("backend count = %d, want 1", count)
	}

	r.UnregisterBackend("test")
	r.mu.RLock()
	count = len(r.backends)
	r.mu.RUnlock()
	if count != 0 {
		t.Fatalf("backend count after unregister = %d, want 0", count)
	}
}

func TestStartStop(t *testing.T) {
	r := NewMetricsReporter(time.Minute)
	if r.Running() {
		t.Fatal("should not be running before Start()")
	}

	r.Start()
	if !r.Running() {
		t.Fatal("should be running after Start()")
	}

	// Double start is a no-op.
	r.Start()
	if !r.Running() {
		t.Fatal("should still be running after double Start()")
	}

	r.Stop()
	if r.Running() {
		t.Fatal("should not be running after Stop()")
	}

	// Double stop is a no-op.
	r.Stop()
}

func TestPeriodicReporting(t *testing.T) {
	b := &mockBackend{}
	r := NewMetricsReporter(20 * time.Millisecond)
	r.RegisterBackend("mock", b)
	r.RecordMetric("ops", 100)

	r.Start()

	// Wait long enough for at least two ticks.
	time.Sleep(80 * time.Millisecond)
	r.Stop()

	calls := b.callCount()
	if calls < 2 {
		t.Fatalf("expected at least 2 backend calls, got %d", calls)
	}

	last := b.lastCall()
	if last["ops"] != 100 {
		t.Fatalf("last reported ops = %f, want 100", last["ops"])
	}
}

func TestMultipleBackends(t *testing.T) {
	b1 := &mockBackend{}
	b2 := &mockBackend{}
	r := NewMetricsReporter(20 * time.Millisecond)
	r.RegisterBackend("one", b1)
	r.RegisterBackend("two", b2)
	r.RecordMetric("x", 1)

	r.Start()
	time.Sleep(50 * time.Millisecond)
	r.Stop()

	if b1.callCount() < 1 {
		t.Fatal("backend 'one' was never called")
	}
	if b2.callCount() < 1 {
		t.Fatal("backend 'two' was never called")
	}
}

func TestBackendErrorDoesNotPanic(t *testing.T) {
	b := &mockBackend{failErr: errors.New("write failed")}
	r := NewMetricsReporter(20 * time.Millisecond)
	r.RegisterBackend("fail", b)
	r.RecordMetric("x", 1)

	r.Start()
	time.Sleep(50 * time.Millisecond)
	r.Stop()

	// The reporter should have called the backend at least once despite errors.
	if b.callCount() < 1 {
		t.Fatal("expected at least 1 backend call even on error")
	}
}

func TestSnapshotIsIsolated(t *testing.T) {
	r := NewMetricsReporter(time.Minute)
	r.RecordMetric("a", 1)

	snap := r.Snapshot()
	snap["a"] = 999 // mutate the snapshot

	snap2 := r.Snapshot()
	if snap2["a"] != 1 {
		t.Fatalf("snapshot mutation leaked: a = %f, want 1", snap2["a"])
	}
}

func TestConcurrentRecordAndSnapshot(t *testing.T) {
	r := NewMetricsReporter(time.Minute)

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 100

	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				r.RecordMetric("counter", float64(id*iterations+j))
			}
		}(i)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = r.Snapshot()
			}
		}()
	}
	wg.Wait()

	snap := r.Snapshot()
	if _, ok := snap["counter"]; !ok {
		t.Fatal("expected 'counter' in snapshot after concurrent writes")
	}
}
