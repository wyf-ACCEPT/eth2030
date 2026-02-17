package metrics

import (
	"sync"
	"testing"
	"time"
)

func TestCounter_IncAndAdd(t *testing.T) {
	c := NewCounter("test.counter")
	if c.Value() != 0 {
		t.Fatalf("initial value = %d, want 0", c.Value())
	}
	c.Inc()
	if c.Value() != 1 {
		t.Fatalf("after Inc() value = %d, want 1", c.Value())
	}
	c.Add(9)
	if c.Value() != 10 {
		t.Fatalf("after Add(9) value = %d, want 10", c.Value())
	}
	// Negative adds must be ignored (counters are monotonic).
	c.Add(-5)
	if c.Value() != 10 {
		t.Fatalf("after Add(-5) value = %d, want 10 (negatives ignored)", c.Value())
	}
	if c.Name() != "test.counter" {
		t.Fatalf("name = %q, want %q", c.Name(), "test.counter")
	}
}

func TestGauge_SetIncDec(t *testing.T) {
	g := NewGauge("test.gauge")
	if g.Value() != 0 {
		t.Fatalf("initial value = %d, want 0", g.Value())
	}
	g.Set(42)
	if g.Value() != 42 {
		t.Fatalf("after Set(42) value = %d, want 42", g.Value())
	}
	g.Inc()
	if g.Value() != 43 {
		t.Fatalf("after Inc() value = %d, want 43", g.Value())
	}
	g.Dec()
	g.Dec()
	if g.Value() != 41 {
		t.Fatalf("after two Dec() value = %d, want 41", g.Value())
	}
	// Gauges can go negative.
	g.Set(-10)
	if g.Value() != -10 {
		t.Fatalf("after Set(-10) value = %d, want -10", g.Value())
	}
	if g.Name() != "test.gauge" {
		t.Fatalf("name = %q, want %q", g.Name(), "test.gauge")
	}
}

func TestHistogram_Observe(t *testing.T) {
	h := NewHistogram("test.hist")
	// No observations yet -- all accessors return 0.
	if h.Count() != 0 {
		t.Fatalf("initial count = %d, want 0", h.Count())
	}
	if h.Min() != 0 || h.Max() != 0 || h.Mean() != 0 {
		t.Fatalf("empty histogram: min=%f max=%f mean=%f, want all 0", h.Min(), h.Max(), h.Mean())
	}
	h.Observe(10)
	h.Observe(20)
	h.Observe(30)
	if h.Count() != 3 {
		t.Fatalf("count = %d, want 3", h.Count())
	}
	if h.Sum() != 60 {
		t.Fatalf("sum = %f, want 60", h.Sum())
	}
	if h.Min() != 10 {
		t.Fatalf("min = %f, want 10", h.Min())
	}
	if h.Max() != 30 {
		t.Fatalf("max = %f, want 30", h.Max())
	}
	if h.Mean() != 20 {
		t.Fatalf("mean = %f, want 20", h.Mean())
	}
	if h.Name() != "test.hist" {
		t.Fatalf("name = %q, want %q", h.Name(), "test.hist")
	}
}

func TestTimer_Stop(t *testing.T) {
	h := NewHistogram("test.timer")
	timer := NewTimer(h)
	time.Sleep(1 * time.Millisecond)
	d := timer.Stop()
	if d <= 0 {
		t.Fatalf("duration = %v, want > 0", d)
	}
	if h.Count() != 1 {
		t.Fatalf("histogram count = %d, want 1", h.Count())
	}
	if h.Min() < 1 {
		t.Fatalf("histogram min = %f, want >= 1 ms", h.Min())
	}

	// A timer with a nil histogram should not panic.
	timer2 := NewTimer(nil)
	d2 := timer2.Stop()
	if d2 < 0 {
		t.Fatalf("nil-hist duration = %v, want >= 0", d2)
	}
}

func TestRegistry_GetOrCreate(t *testing.T) {
	r := NewRegistry()
	c1 := r.Counter("ops")
	c2 := r.Counter("ops")
	if c1 != c2 {
		t.Fatal("Counter: second call returned a different instance")
	}
	g1 := r.Gauge("peers")
	g2 := r.Gauge("peers")
	if g1 != g2 {
		t.Fatal("Gauge: second call returned a different instance")
	}
	h1 := r.Histogram("latency")
	h2 := r.Histogram("latency")
	if h1 != h2 {
		t.Fatal("Histogram: second call returned a different instance")
	}
}

func TestRegistry_Snapshot(t *testing.T) {
	r := NewRegistry()
	r.Counter("c").Add(5)
	r.Gauge("g").Set(42)
	h := r.Histogram("h")
	h.Observe(10)
	h.Observe(20)

	snap := r.Snapshot()

	if v, ok := snap["c"]; !ok {
		t.Fatal("snapshot missing counter 'c'")
	} else if v.(int64) != 5 {
		t.Fatalf("counter c = %v, want 5", v)
	}
	if v, ok := snap["g"]; !ok {
		t.Fatal("snapshot missing gauge 'g'")
	} else if v.(int64) != 42 {
		t.Fatalf("gauge g = %v, want 42", v)
	}
	hv, ok := snap["h"]
	if !ok {
		t.Fatal("snapshot missing histogram 'h'")
	}
	hm := hv.(map[string]interface{})
	if hm["count"].(int64) != 2 {
		t.Fatalf("histogram count = %v, want 2", hm["count"])
	}
	if hm["sum"].(float64) != 30 {
		t.Fatalf("histogram sum = %v, want 30", hm["sum"])
	}
	if hm["min"].(float64) != 10 {
		t.Fatalf("histogram min = %v, want 10", hm["min"])
	}
	if hm["max"].(float64) != 20 {
		t.Fatalf("histogram max = %v, want 20", hm["max"])
	}
	if hm["mean"].(float64) != 15 {
		t.Fatalf("histogram mean = %v, want 15", hm["mean"])
	}
}

func TestConcurrency(t *testing.T) {
	c := NewCounter("concurrent.counter")
	g := NewGauge("concurrent.gauge")
	h := NewHistogram("concurrent.hist")

	const goroutines = 100
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				c.Inc()
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				g.Inc()
				g.Dec()
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				h.Observe(float64(j))
			}
		}()
	}
	wg.Wait()

	want := int64(goroutines * iterations)
	if c.Value() != want {
		t.Fatalf("counter = %d, want %d", c.Value(), want)
	}
	if g.Value() != 0 {
		t.Fatalf("gauge = %d, want 0", g.Value())
	}
	if h.Count() != want {
		t.Fatalf("histogram count = %d, want %d", h.Count(), want)
	}
}

func TestStandardMetrics(t *testing.T) {
	// Verify standard metrics are non-nil and usable.
	ChainHeight.Set(100)
	if ChainHeight.Value() != 100 {
		t.Fatalf("ChainHeight = %d, want 100", ChainHeight.Value())
	}
	BlocksInserted.Inc()
	if BlocksInserted.Value() != 1 {
		t.Fatalf("BlocksInserted = %d, want 1", BlocksInserted.Value())
	}
	BlockProcessTime.Observe(42.5)
	if BlockProcessTime.Count() != 1 {
		t.Fatalf("BlockProcessTime count = %d, want 1", BlockProcessTime.Count())
	}
}
