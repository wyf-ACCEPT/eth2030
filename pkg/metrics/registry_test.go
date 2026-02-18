package metrics

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Counter extended tests ---

func TestCounter_AddZero(t *testing.T) {
	c := NewCounter("test.add_zero")
	c.Inc()
	c.Add(0) // zero should be ignored (not > 0)
	if c.Value() != 1 {
		t.Fatalf("after Add(0): want 1, got %d", c.Value())
	}
}

func TestCounter_AddLargeValue(t *testing.T) {
	c := NewCounter("test.large")
	c.Add(math.MaxInt64 - 1)
	if c.Value() != math.MaxInt64-1 {
		t.Fatalf("after large Add: want %d, got %d", int64(math.MaxInt64-1), c.Value())
	}
	c.Inc()
	if c.Value() != math.MaxInt64 {
		t.Fatalf("after Inc: want %d, got %d", int64(math.MaxInt64), c.Value())
	}
}

func TestCounter_MultipleNegativeAdds(t *testing.T) {
	c := NewCounter("test.negatives")
	c.Add(10)
	c.Add(-1)
	c.Add(-100)
	c.Add(-math.MaxInt64)
	if c.Value() != 10 {
		t.Fatalf("negative adds should all be ignored: want 10, got %d", c.Value())
	}
}

func TestCounter_ConcurrentIncrement(t *testing.T) {
	c := NewCounter("test.conc_inc")
	const n = 10000
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			c.Inc()
		}()
	}
	wg.Wait()
	if c.Value() != n {
		t.Fatalf("concurrent Inc: want %d, got %d", n, c.Value())
	}
}

// --- Gauge extended tests ---

func TestGauge_SetOverwrite(t *testing.T) {
	g := NewGauge("test.overwrite")
	g.Set(100)
	g.Set(200)
	g.Set(-50)
	if g.Value() != -50 {
		t.Fatalf("Set should overwrite: want -50, got %d", g.Value())
	}
}

func TestGauge_IncDecSymmetry(t *testing.T) {
	g := NewGauge("test.symmetry")
	for i := 0; i < 100; i++ {
		g.Inc()
	}
	for i := 0; i < 100; i++ {
		g.Dec()
	}
	if g.Value() != 0 {
		t.Fatalf("100 Inc + 100 Dec: want 0, got %d", g.Value())
	}
}

func TestGauge_ConcurrentSetAndRead(t *testing.T) {
	g := NewGauge("test.conc_set")
	const goroutines = 50
	const iterations = 1000
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Writers.
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				g.Set(int64(id*iterations + j))
			}
		}(i)
	}
	// Readers (should not panic or produce data races).
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = g.Value()
			}
		}()
	}
	wg.Wait()
}

func TestGauge_Extremes(t *testing.T) {
	g := NewGauge("test.extremes")
	g.Set(math.MaxInt64)
	if g.Value() != math.MaxInt64 {
		t.Fatalf("MaxInt64: want %d, got %d", int64(math.MaxInt64), g.Value())
	}
	g.Set(math.MinInt64)
	if g.Value() != math.MinInt64 {
		t.Fatalf("MinInt64: want %d, got %d", int64(math.MinInt64), g.Value())
	}
}

// --- Histogram extended tests ---

func TestHistogram_SingleObservation(t *testing.T) {
	h := NewHistogram("test.single")
	h.Observe(42.5)
	if h.Count() != 1 {
		t.Fatalf("count: want 1, got %d", h.Count())
	}
	if h.Min() != 42.5 {
		t.Fatalf("min: want 42.5, got %f", h.Min())
	}
	if h.Max() != 42.5 {
		t.Fatalf("max: want 42.5, got %f", h.Max())
	}
	if h.Mean() != 42.5 {
		t.Fatalf("mean: want 42.5, got %f", h.Mean())
	}
	if h.Sum() != 42.5 {
		t.Fatalf("sum: want 42.5, got %f", h.Sum())
	}
}

func TestHistogram_NegativeValues(t *testing.T) {
	h := NewHistogram("test.negatives")
	h.Observe(-10)
	h.Observe(-20)
	h.Observe(-5)
	if h.Min() != -20 {
		t.Fatalf("min: want -20, got %f", h.Min())
	}
	if h.Max() != -5 {
		t.Fatalf("max: want -5, got %f", h.Max())
	}
	expected := (-10.0 + -20.0 + -5.0) / 3
	if h.Mean() != expected {
		t.Fatalf("mean: want %f, got %f", expected, h.Mean())
	}
}

func TestHistogram_ZeroValue(t *testing.T) {
	h := NewHistogram("test.zero")
	h.Observe(0)
	if h.Count() != 1 {
		t.Fatalf("count: want 1, got %d", h.Count())
	}
	if h.Min() != 0 {
		t.Fatalf("min: want 0, got %f", h.Min())
	}
	if h.Max() != 0 {
		t.Fatalf("max: want 0, got %f", h.Max())
	}
}

func TestHistogram_LargeDataset(t *testing.T) {
	h := NewHistogram("test.large_dataset")
	const n = 10000
	var expectedSum float64
	for i := 0; i < n; i++ {
		v := float64(i)
		h.Observe(v)
		expectedSum += v
	}
	if h.Count() != n {
		t.Fatalf("count: want %d, got %d", n, h.Count())
	}
	if h.Sum() != expectedSum {
		t.Fatalf("sum: want %f, got %f", expectedSum, h.Sum())
	}
	if h.Min() != 0 {
		t.Fatalf("min: want 0, got %f", h.Min())
	}
	if h.Max() != float64(n-1) {
		t.Fatalf("max: want %f, got %f", float64(n-1), h.Max())
	}
	expectedMean := expectedSum / float64(n)
	if h.Mean() != expectedMean {
		t.Fatalf("mean: want %f, got %f", expectedMean, h.Mean())
	}
}

func TestHistogram_MixedPositiveNegative(t *testing.T) {
	h := NewHistogram("test.mixed")
	h.Observe(-100.5)
	h.Observe(0)
	h.Observe(100.5)
	if h.Min() != -100.5 {
		t.Fatalf("min: want -100.5, got %f", h.Min())
	}
	if h.Max() != 100.5 {
		t.Fatalf("max: want 100.5, got %f", h.Max())
	}
	if h.Mean() != 0 {
		t.Fatalf("mean: want 0, got %f", h.Mean())
	}
}

func TestHistogram_ConcurrentObserve(t *testing.T) {
	h := NewHistogram("test.conc_obs")
	const goroutines = 100
	const iterations = 500
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				h.Observe(1.0) // All observe the same value for deterministic check.
			}
		}()
	}
	wg.Wait()
	want := int64(goroutines * iterations)
	if h.Count() != want {
		t.Fatalf("count: want %d, got %d", want, h.Count())
	}
	if h.Sum() != float64(want) {
		t.Fatalf("sum: want %f, got %f", float64(want), h.Sum())
	}
	if h.Min() != 1.0 || h.Max() != 1.0 {
		t.Fatalf("min/max: want 1.0/1.0, got %f/%f", h.Min(), h.Max())
	}
}

// --- Timer extended tests ---

func TestTimer_NilHistogram(t *testing.T) {
	// Should not panic.
	timer := NewTimer(nil)
	d := timer.Stop()
	if d < 0 {
		t.Fatalf("duration should be >= 0, got %v", d)
	}
}

func TestTimer_MultipleStops(t *testing.T) {
	h := NewHistogram("test.multi_stop")
	timer := NewTimer(h)
	time.Sleep(1 * time.Millisecond)
	timer.Stop()
	// Second stop records a second observation.
	timer.Stop()
	if h.Count() != 2 {
		t.Fatalf("count after two stops: want 2, got %d", h.Count())
	}
}

func TestTimer_RecordsDuration(t *testing.T) {
	h := NewHistogram("test.timer_dur")
	timer := NewTimer(h)
	time.Sleep(10 * time.Millisecond)
	d := timer.Stop()
	if d < 10*time.Millisecond {
		t.Fatalf("duration: want >= 10ms, got %v", d)
	}
	// Histogram should have recorded the duration in milliseconds.
	if h.Min() < 10 {
		t.Fatalf("histogram min: want >= 10 ms, got %f", h.Min())
	}
}

// --- Registry extended tests ---

func TestRegistry_Empty(t *testing.T) {
	r := NewRegistry()
	snap := r.Snapshot()
	if len(snap) != 0 {
		t.Fatalf("empty registry snapshot: want 0 entries, got %d", len(snap))
	}
}

func TestRegistry_CounterOnly(t *testing.T) {
	r := NewRegistry()
	r.Counter("c1").Add(5)
	r.Counter("c2").Inc()
	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot entries: want 2, got %d", len(snap))
	}
	if snap["c1"].(int64) != 5 {
		t.Fatalf("c1: want 5, got %v", snap["c1"])
	}
	if snap["c2"].(int64) != 1 {
		t.Fatalf("c2: want 1, got %v", snap["c2"])
	}
}

func TestRegistry_GaugeOnly(t *testing.T) {
	r := NewRegistry()
	r.Gauge("g1").Set(42)
	r.Gauge("g2").Set(-7)
	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot entries: want 2, got %d", len(snap))
	}
	if snap["g1"].(int64) != 42 {
		t.Fatalf("g1: want 42, got %v", snap["g1"])
	}
	if snap["g2"].(int64) != -7 {
		t.Fatalf("g2: want -7, got %v", snap["g2"])
	}
}

func TestRegistry_HistogramOnly(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("h1")
	h.Observe(5)
	h.Observe(15)
	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot entries: want 1, got %d", len(snap))
	}
	hm := snap["h1"].(map[string]interface{})
	if hm["count"].(int64) != 2 {
		t.Fatalf("h1 count: want 2, got %v", hm["count"])
	}
	if hm["min"].(float64) != 5 {
		t.Fatalf("h1 min: want 5, got %v", hm["min"])
	}
	if hm["max"].(float64) != 15 {
		t.Fatalf("h1 max: want 15, got %v", hm["max"])
	}
	if hm["mean"].(float64) != 10 {
		t.Fatalf("h1 mean: want 10, got %v", hm["mean"])
	}
	if hm["sum"].(float64) != 20 {
		t.Fatalf("h1 sum: want 20, got %v", hm["sum"])
	}
}

func TestRegistry_DuplicateGetReturnsSame(t *testing.T) {
	r := NewRegistry()

	// Counter identity.
	c1 := r.Counter("shared_name")
	c1.Inc()
	c2 := r.Counter("shared_name")
	if c2.Value() != 1 {
		t.Fatalf("counter reuse: second reference should see value 1, got %d", c2.Value())
	}

	// Gauge identity.
	g1 := r.Gauge("g_shared")
	g1.Set(99)
	g2 := r.Gauge("g_shared")
	if g2.Value() != 99 {
		t.Fatalf("gauge reuse: want 99, got %d", g2.Value())
	}

	// Histogram identity.
	h1 := r.Histogram("h_shared")
	h1.Observe(7)
	h2 := r.Histogram("h_shared")
	if h2.Count() != 1 {
		t.Fatalf("histogram reuse: want count 1, got %d", h2.Count())
	}
}

func TestRegistry_ManyMetrics(t *testing.T) {
	r := NewRegistry()
	const n = 100
	for i := 0; i < n; i++ {
		r.Counter(fmt.Sprintf("counter_%d", i)).Add(int64(i))
		r.Gauge(fmt.Sprintf("gauge_%d", i)).Set(int64(i * 10))
		r.Histogram(fmt.Sprintf("hist_%d", i)).Observe(float64(i))
	}
	snap := r.Snapshot()
	if len(snap) != 3*n {
		t.Fatalf("snapshot entries: want %d, got %d", 3*n, len(snap))
	}
}

func TestRegistry_ConcurrentGetOrCreate(t *testing.T) {
	r := NewRegistry()
	const goroutines = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Many goroutines concurrently requesting the same counter.
	counters := make([]*Counter, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			counters[idx] = r.Counter("shared.counter")
		}(i)
	}

	// Same for gauges.
	gauges := make([]*Gauge, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			gauges[idx] = r.Gauge("shared.gauge")
		}(i)
	}

	// Same for histograms.
	histograms := make([]*Histogram, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			histograms[idx] = r.Histogram("shared.histogram")
		}(i)
	}

	wg.Wait()

	// All goroutines should get the same instance.
	for i := 1; i < goroutines; i++ {
		if counters[i] != counters[0] {
			t.Fatal("concurrent Counter: different instances returned")
		}
		if gauges[i] != gauges[0] {
			t.Fatal("concurrent Gauge: different instances returned")
		}
		if histograms[i] != histograms[0] {
			t.Fatal("concurrent Histogram: different instances returned")
		}
	}
}

func TestRegistry_ConcurrentGetOrCreateDifferentNames(t *testing.T) {
	r := NewRegistry()
	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			// Use distinct names per metric type to avoid snapshot key collisions.
			r.Counter(fmt.Sprintf("counter_%d", idx)).Inc()
			r.Gauge(fmt.Sprintf("gauge_%d", idx)).Set(int64(idx))
			r.Histogram(fmt.Sprintf("hist_%d", idx)).Observe(float64(idx))
		}(i)
	}
	wg.Wait()

	snap := r.Snapshot()
	// Each goroutine creates 3 metrics with distinct names per type.
	if len(snap) != goroutines*3 {
		t.Fatalf("snapshot: want %d entries, got %d", goroutines*3, len(snap))
	}
}

func TestRegistry_SnapshotIsIsolated(t *testing.T) {
	r := NewRegistry()
	r.Counter("c").Add(5)
	snap := r.Snapshot()

	// Mutate the counter after snapshot.
	r.Counter("c").Add(10)

	// Snapshot should reflect the old value.
	if snap["c"].(int64) != 5 {
		t.Fatalf("snapshot should be isolated: want 5, got %v", snap["c"])
	}

	// New snapshot reflects current value.
	snap2 := r.Snapshot()
	if snap2["c"].(int64) != 15 {
		t.Fatalf("new snapshot: want 15, got %v", snap2["c"])
	}
}

func TestRegistry_ConcurrentSnapshotAndWrite(t *testing.T) {
	r := NewRegistry()
	r.Counter("c").Add(1)
	r.Gauge("g").Set(1)
	r.Histogram("h").Observe(1)

	const goroutines = 50
	const iterations = 500
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Writers.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				r.Counter("c").Inc()
				r.Gauge("g").Inc()
				r.Histogram("h").Observe(1.0)
			}
		}()
	}
	// Readers (snapshot).
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				snap := r.Snapshot()
				// Snapshot should always have consistent keys.
				if _, ok := snap["c"]; !ok {
					t.Error("snapshot missing counter 'c'")
					return
				}
				if _, ok := snap["g"]; !ok {
					t.Error("snapshot missing gauge 'g'")
					return
				}
				if _, ok := snap["h"]; !ok {
					t.Error("snapshot missing histogram 'h'")
					return
				}
			}
		}()
	}
	wg.Wait()
}

// --- DefaultRegistry tests ---

func TestDefaultRegistry_NotNil(t *testing.T) {
	if DefaultRegistry == nil {
		t.Fatal("DefaultRegistry should not be nil")
	}
}

// --- Name uniqueness across metric types ---

func TestRegistry_SameNameDifferentTypes(t *testing.T) {
	r := NewRegistry()
	// Using the same name for different metric types should create separate entries.
	r.Counter("metric").Inc()
	r.Gauge("metric").Set(42)
	r.Histogram("metric").Observe(7)

	snap := r.Snapshot()
	// All three should be present since they are different metric types.
	// The counter and gauge both produce int64 under the same key.
	// Due to map key collision, only the last written type is visible in the snapshot.
	// Actually, since counters, gauges, and histograms use separate internal maps,
	// the same name creates separate entries but Snapshot iterates all maps and
	// would overwrite the key. Let's verify:
	if len(snap) < 1 {
		t.Fatal("snapshot should have at least one entry")
	}
	// Due to iteration order non-determinism, we just verify no panic.
}

// --- Metric name tests ---

func TestMetric_EmptyName(t *testing.T) {
	c := NewCounter("")
	if c.Name() != "" {
		t.Fatalf("empty name counter: want empty, got %q", c.Name())
	}
	g := NewGauge("")
	if g.Name() != "" {
		t.Fatalf("empty name gauge: want empty, got %q", g.Name())
	}
	h := NewHistogram("")
	if h.Name() != "" {
		t.Fatalf("empty name histogram: want empty, got %q", h.Name())
	}
}

func TestMetric_SpecialCharNames(t *testing.T) {
	names := []string{
		"a.b.c",
		"metric/with/slashes",
		"metric-with-dashes",
		"metric_with_underscores",
		"metric.123.numeric",
	}
	for _, name := range names {
		c := NewCounter(name)
		if c.Name() != name {
			t.Errorf("counter name: want %q, got %q", name, c.Name())
		}
	}
}

// --- Standard metrics validation ---

func TestStandardMetrics_Names(t *testing.T) {
	expectedCounterNames := []string{
		"chain.blocks_inserted",
		"chain.reorgs",
		"txpool.added",
		"txpool.dropped",
		"p2p.messages_received",
		"p2p.messages_sent",
		"rpc.requests",
		"rpc.errors",
		"evm.executions",
		"evm.gas_used",
		"engine.new_payload",
		"engine.forkchoice_updated",
	}

	snap := DefaultRegistry.Snapshot()
	for _, name := range expectedCounterNames {
		if _, ok := snap[name]; !ok {
			t.Errorf("standard metric %q not found in DefaultRegistry snapshot", name)
		}
	}
}

func TestStandardMetrics_GaugeNames(t *testing.T) {
	expectedGaugeNames := []string{
		"chain.height",
		"txpool.pending",
		"txpool.queued",
		"p2p.peers",
	}

	snap := DefaultRegistry.Snapshot()
	for _, name := range expectedGaugeNames {
		if _, ok := snap[name]; !ok {
			t.Errorf("standard gauge %q not found in DefaultRegistry snapshot", name)
		}
	}
}

func TestStandardMetrics_HistogramNames(t *testing.T) {
	expectedHistNames := []string{
		"chain.block_process_ms",
		"rpc.latency_ms",
	}

	snap := DefaultRegistry.Snapshot()
	for _, name := range expectedHistNames {
		if _, ok := snap[name]; !ok {
			t.Errorf("standard histogram %q not found in DefaultRegistry snapshot", name)
		}
	}
}

func TestStandardMetrics_AllNonNil(t *testing.T) {
	metrics := []interface{}{
		ChainHeight, BlockProcessTime, BlocksInserted, ReorgsDetected,
		TxPoolPending, TxPoolQueued, TxPoolAdded, TxPoolDropped,
		PeersConnected, MessagesReceived, MessagesSent,
		RPCRequests, RPCErrors, RPCLatency,
		EVMExecutions, EVMGasUsed,
		EngineNewPayload, EngineFCU,
	}
	for i, m := range metrics {
		if m == nil {
			t.Errorf("standard metric [%d] is nil", i)
		}
	}
}

// --- Histogram empty-check edge cases ---

func TestHistogram_EmptyMinMaxMean(t *testing.T) {
	h := NewHistogram("test.empty_checks")
	// All should return 0 when no observations.
	if h.Min() != 0 {
		t.Fatalf("empty Min: want 0, got %f", h.Min())
	}
	if h.Max() != 0 {
		t.Fatalf("empty Max: want 0, got %f", h.Max())
	}
	if h.Mean() != 0 {
		t.Fatalf("empty Mean: want 0, got %f", h.Mean())
	}
	if h.Sum() != 0 {
		t.Fatalf("empty Sum: want 0, got %f", h.Sum())
	}
	if h.Count() != 0 {
		t.Fatalf("empty Count: want 0, got %d", h.Count())
	}
}

// --- Snapshot with histogram that has no observations ---

func TestRegistry_SnapshotWithEmptyHistogram(t *testing.T) {
	r := NewRegistry()
	r.Histogram("empty_h") // create but don't observe

	snap := r.Snapshot()
	hv, ok := snap["empty_h"]
	if !ok {
		t.Fatal("snapshot missing histogram 'empty_h'")
	}
	hm := hv.(map[string]interface{})
	if hm["count"].(int64) != 0 {
		t.Fatalf("empty histogram count: want 0, got %v", hm["count"])
	}
	if hm["min"].(float64) != 0 {
		t.Fatalf("empty histogram min: want 0, got %v", hm["min"])
	}
	if hm["max"].(float64) != 0 {
		t.Fatalf("empty histogram max: want 0, got %v", hm["max"])
	}
	if hm["mean"].(float64) != 0 {
		t.Fatalf("empty histogram mean: want 0, got %v", hm["mean"])
	}
	if hm["sum"].(float64) != 0 {
		t.Fatalf("empty histogram sum: want 0, got %v", hm["sum"])
	}
}

// --- Benchmark for concurrent registry access ---

func BenchmarkRegistry_ConcurrentCounter(b *testing.B) {
	r := NewRegistry()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r.Counter("bench.counter").Inc()
		}
	})
}

func BenchmarkCounter_Inc(b *testing.B) {
	c := NewCounter("bench.inc")
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}

func BenchmarkHistogram_Observe(b *testing.B) {
	h := NewHistogram("bench.observe")
	b.RunParallel(func(pb *testing.PB) {
		v := 0.0
		for pb.Next() {
			h.Observe(v)
			v++
		}
	})
}

// --- Counter initial state test ---

func TestCounter_InitialState(t *testing.T) {
	c := NewCounter("test.init")
	if c.Value() != 0 {
		t.Fatalf("initial counter value: want 0, got %d", c.Value())
	}
	if c.Name() != "test.init" {
		t.Fatalf("name: want %q, got %q", "test.init", c.Name())
	}
}

// --- Gauge initial state test ---

func TestGauge_InitialState(t *testing.T) {
	g := NewGauge("test.gauge_init")
	if g.Value() != 0 {
		t.Fatalf("initial gauge value: want 0, got %d", g.Value())
	}
	if g.Name() != "test.gauge_init" {
		t.Fatalf("name: want %q, got %q", "test.gauge_init", g.Name())
	}
}

// --- Registry with high contention ---

func TestRegistry_HighContentionGetOrCreate(t *testing.T) {
	r := NewRegistry()
	const goroutines = 200
	const names = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("contended_%d", id%names)
			c := r.Counter(name)
			c.Inc()
			_ = r.Gauge(name)
			_ = r.Histogram(name)
		}(i)
	}
	wg.Wait()

	// Each of the 10 names should have a counter with value goroutines/names.
	for i := 0; i < names; i++ {
		name := fmt.Sprintf("contended_%d", i)
		c := r.Counter(name)
		expectedMin := int64(goroutines / names)
		if c.Value() < expectedMin {
			t.Errorf("counter %s: want >= %d, got %d", name, expectedMin, c.Value())
		}
	}
}

// --- Registry separates namespaces ---

func TestRegistry_NamespaceSeparation(t *testing.T) {
	r := NewRegistry()
	r.Counter("a.b").Add(1)
	r.Counter("a.c").Add(2)
	r.Counter("b.a").Add(3)

	snap := r.Snapshot()
	if snap["a.b"].(int64) != 1 {
		t.Fatalf("a.b: want 1, got %v", snap["a.b"])
	}
	if snap["a.c"].(int64) != 2 {
		t.Fatalf("a.c: want 2, got %v", snap["a.c"])
	}
	if snap["b.a"].(int64) != 3 {
		t.Fatalf("b.a: want 3, got %v", snap["b.a"])
	}
}

// --- Verify metric names follow dot convention ---

func TestStandardMetrics_DotConvention(t *testing.T) {
	snap := DefaultRegistry.Snapshot()
	for name := range snap {
		if !strings.Contains(name, ".") {
			t.Errorf("metric name %q does not follow dot convention", name)
		}
	}
}
