// Package metrics provides lightweight, zero-dependency metrics primitives
// for the ETH2030 Ethereum execution client. Counter and Gauge use atomic
// operations for lock-free concurrent access; Histogram uses a mutex.
package metrics

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Counter
// ---------------------------------------------------------------------------

// Counter is a monotonically incrementing counter.
type Counter struct {
	name  string
	value atomic.Int64
}

// NewCounter returns a new Counter with the given name.
func NewCounter(name string) *Counter {
	return &Counter{name: name}
}

// Inc increments the counter by 1.
func (c *Counter) Inc() { c.value.Add(1) }

// Add increments the counter by n. Negative values are silently ignored
// because counters are monotonically increasing.
func (c *Counter) Add(n int64) {
	if n > 0 {
		c.value.Add(n)
	}
}

// Value returns the current counter value.
func (c *Counter) Value() int64 { return c.value.Load() }

// Name returns the metric name.
func (c *Counter) Name() string { return c.name }

// ---------------------------------------------------------------------------
// Gauge
// ---------------------------------------------------------------------------

// Gauge is a value that can go up and down.
type Gauge struct {
	name  string
	value atomic.Int64
}

// NewGauge returns a new Gauge with the given name.
func NewGauge(name string) *Gauge {
	return &Gauge{name: name}
}

// Set sets the gauge to the given value.
func (g *Gauge) Set(v int64) { g.value.Store(v) }

// Inc increments the gauge by 1.
func (g *Gauge) Inc() { g.value.Add(1) }

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() { g.value.Add(-1) }

// Value returns the current gauge value.
func (g *Gauge) Value() int64 { return g.value.Load() }

// Name returns the metric name.
func (g *Gauge) Name() string { return g.name }

// ---------------------------------------------------------------------------
// Histogram
// ---------------------------------------------------------------------------

// Histogram tracks the distribution of observed values. It records count,
// sum, min, and max. For a full-featured histogram with quantiles consider
// using an external library; this implementation intentionally stays minimal.
type Histogram struct {
	name  string
	mu    sync.Mutex
	count int64
	sum   float64
	min   float64
	max   float64
}

// NewHistogram returns a new Histogram with the given name.
func NewHistogram(name string) *Histogram {
	return &Histogram{
		name: name,
		min:  math.MaxFloat64,
		max:  -math.MaxFloat64,
	}
}

// Observe records a value.
func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	h.count++
	h.sum += v
	if v < h.min {
		h.min = v
	}
	if v > h.max {
		h.max = v
	}
	h.mu.Unlock()
}

// Count returns the number of observations.
func (h *Histogram) Count() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

// Sum returns the sum of all observed values.
func (h *Histogram) Sum() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sum
}

// Min returns the smallest observed value. If no values have been observed
// it returns 0.
func (h *Histogram) Min() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 {
		return 0
	}
	return h.min
}

// Max returns the largest observed value. If no values have been observed
// it returns 0.
func (h *Histogram) Max() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 {
		return 0
	}
	return h.max
}

// Mean returns the arithmetic mean of all observations. Returns 0 when no
// values have been observed.
func (h *Histogram) Mean() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 {
		return 0
	}
	return h.sum / float64(h.count)
}

// Name returns the metric name.
func (h *Histogram) Name() string { return h.name }

// ---------------------------------------------------------------------------
// Timer
// ---------------------------------------------------------------------------

// Timer is a convenience helper for timing operations. It records the
// elapsed duration (in milliseconds) into an associated Histogram when
// Stop is called.
type Timer struct {
	start time.Time
	hist  *Histogram
}

// NewTimer starts a new timer that will record into h when stopped.
func NewTimer(h *Histogram) *Timer {
	return &Timer{
		start: time.Now(),
		hist:  h,
	}
}

// Stop records the elapsed time in milliseconds into the associated
// histogram and returns the duration.
func (t *Timer) Stop() time.Duration {
	d := time.Since(t.start)
	if t.hist != nil {
		t.hist.Observe(float64(d.Milliseconds()))
	}
	return d
}
