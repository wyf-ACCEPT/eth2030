// Package metrics reporter provides periodic export of metric values to
// pluggable backends (e.g. Prometheus push-gateway, StatsD, log file).
package metrics

import (
	"sync"
	"time"
)

// ReportBackend is the interface that export backends must implement.
// Report is called periodically with a snapshot of all current metric values.
type ReportBackend interface {
	Report(metrics map[string]float64) error
}

// MetricsReporter periodically collects metric values and pushes them to
// one or more registered backends.
type MetricsReporter struct {
	mu       sync.RWMutex
	interval time.Duration
	backends map[string]ReportBackend
	metrics  map[string]float64

	stopCh chan struct{}
	doneCh chan struct{}
	running bool
}

// NewMetricsReporter creates a reporter that exports metrics every interval.
func NewMetricsReporter(interval time.Duration) *MetricsReporter {
	return &MetricsReporter{
		interval: interval,
		backends: make(map[string]ReportBackend),
		metrics:  make(map[string]float64),
	}
}

// RegisterBackend adds a named export backend. If a backend with the same
// name already exists it is replaced.
func (r *MetricsReporter) RegisterBackend(name string, backend ReportBackend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[name] = backend
}

// UnregisterBackend removes a previously registered backend by name.
func (r *MetricsReporter) UnregisterBackend(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.backends, name)
}

// RecordMetric stores a metric value that will be included in subsequent
// reports. Concurrent-safe.
func (r *MetricsReporter) RecordMetric(name string, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics[name] = value
}

// RecordTimer records a duration metric in milliseconds.
func (r *MetricsReporter) RecordTimer(name string, duration time.Duration) {
	r.RecordMetric(name, float64(duration.Milliseconds()))
}

// Snapshot returns a point-in-time copy of all recorded metric values.
func (r *MetricsReporter) Snapshot() map[string]float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snap := make(map[string]float64, len(r.metrics))
	for k, v := range r.metrics {
		snap[k] = v
	}
	return snap
}

// Start begins periodic reporting. It is safe to call Start on an already
// running reporter (it is a no-op).
func (r *MetricsReporter) Start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.stopCh = make(chan struct{})
	r.doneCh = make(chan struct{})
	r.mu.Unlock()

	go r.loop()
}

// Stop halts periodic reporting and blocks until the reporting goroutine
// exits. Safe to call on a stopped reporter (no-op).
func (r *MetricsReporter) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.running = false
	close(r.stopCh)
	r.mu.Unlock()

	<-r.doneCh
}

// Running returns true if the reporter is actively exporting.
func (r *MetricsReporter) Running() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

// loop is the main export goroutine. It ticks at the configured interval
// and calls every registered backend with the current snapshot.
func (r *MetricsReporter) loop() {
	defer close(r.doneCh)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.reportOnce()
		}
	}
}

// reportOnce takes a snapshot and sends it to all backends. Errors from
// individual backends are currently silently ignored; production code would
// log them.
func (r *MetricsReporter) reportOnce() {
	snap := r.Snapshot()

	r.mu.RLock()
	backends := make([]ReportBackend, 0, len(r.backends))
	for _, b := range r.backends {
		backends = append(backends, b)
	}
	r.mu.RUnlock()

	for _, b := range backends {
		_ = b.Report(snap)
	}
}
