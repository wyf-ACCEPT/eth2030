package metrics

import "sync"

// Registry holds all registered metrics, keyed by name. Metrics are created
// on first access (get-or-create semantics) so callers never need to check
// for nil.
type Registry struct {
	mu         sync.RWMutex
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
}

// DefaultRegistry is the process-wide global registry used by the
// pre-defined metrics in standard.go.
var DefaultRegistry = NewRegistry()

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
	}
}

// Counter returns the Counter registered under name, creating it if it does
// not exist yet.
func (r *Registry) Counter(name string) *Counter {
	// Fast path: read lock.
	r.mu.RLock()
	c, ok := r.counters[name]
	r.mu.RUnlock()
	if ok {
		return c
	}

	// Slow path: write lock + double-check.
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok = r.counters[name]; ok {
		return c
	}
	c = NewCounter(name)
	r.counters[name] = c
	return c
}

// Gauge returns the Gauge registered under name, creating it if it does not
// exist yet.
func (r *Registry) Gauge(name string) *Gauge {
	r.mu.RLock()
	g, ok := r.gauges[name]
	r.mu.RUnlock()
	if ok {
		return g
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok = r.gauges[name]; ok {
		return g
	}
	g = NewGauge(name)
	r.gauges[name] = g
	return g
}

// Histogram returns the Histogram registered under name, creating it if it
// does not exist yet.
func (r *Registry) Histogram(name string) *Histogram {
	r.mu.RLock()
	h, ok := r.histograms[name]
	r.mu.RUnlock()
	if ok {
		return h
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok = r.histograms[name]; ok {
		return h
	}
	h = NewHistogram(name)
	r.histograms[name] = h
	return h
}

// Snapshot returns a point-in-time copy of every metric value in the
// registry. The returned map is keyed by metric name; values are int64 for
// counters and gauges, and map[string]interface{} for histograms.
func (r *Registry) Snapshot() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snap := make(map[string]interface{}, len(r.counters)+len(r.gauges)+len(r.histograms))
	for name, c := range r.counters {
		snap[name] = c.Value()
	}
	for name, g := range r.gauges {
		snap[name] = g.Value()
	}
	for name, h := range r.histograms {
		snap[name] = map[string]interface{}{
			"count": h.Count(),
			"sum":   h.Sum(),
			"min":   h.Min(),
			"max":   h.Max(),
			"mean":  h.Mean(),
		}
	}
	return snap
}
