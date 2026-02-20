// collector.go implements a metrics collector that aggregates metrics from
// all subsystems. It stores tagged metric entries with timestamps and
// supports histograms with percentile computation.
package metrics

import (
	"math"
	"sort"
	"sync"
	"time"
)

// CollectorConfig configures the metrics collector.
type CollectorConfig struct {
	FlushInterval    int64 // seconds between automatic flushes
	MaxMetrics       int   // maximum number of stored metric entries
	EnableHistograms bool  // whether histogram recording is enabled
}

// MetricEntry represents a single recorded metric data point.
type MetricEntry struct {
	Name      string            // dot-separated metric name (e.g. "chain.height")
	Value     float64           // observed value
	Tags      map[string]string // key-value labels
	Timestamp int64             // unix timestamp of recording
	Type      string            // "gauge", "counter", "histogram"
}

// MetricsCollector aggregates metrics from subsystems. All methods are
// safe for concurrent use.
type MetricsCollector struct {
	mu         sync.RWMutex
	config     CollectorConfig
	metrics    []MetricEntry            // append-only log of all entries
	latest     map[string]*MetricEntry  // most recent value per name
	histValues map[string][]float64     // raw values per histogram name
}

// NewMetricsCollector creates a new collector with the given config.
func NewMetricsCollector(config CollectorConfig) *MetricsCollector {
	if config.MaxMetrics <= 0 {
		config.MaxMetrics = 10000
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 60
	}
	return &MetricsCollector{
		config:     config,
		metrics:    make([]MetricEntry, 0, 64),
		latest:     make(map[string]*MetricEntry),
		histValues: make(map[string][]float64),
	}
}

// Record stores a metric value with optional tags. The metric type is set
// to "gauge" by default. Tags may be nil.
func (mc *MetricsCollector) Record(name string, value float64, tags map[string]string) {
	entry := MetricEntry{
		Name:      name,
		Value:     value,
		Tags:      copyTags(tags),
		Timestamp: time.Now().Unix(),
		Type:      "gauge",
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()

	if len(mc.metrics) < mc.config.MaxMetrics {
		mc.metrics = append(mc.metrics, entry)
	}
	cp := entry
	mc.latest[name] = &cp
}

// RecordHistogram records a histogram observation. This is a no-op if
// EnableHistograms is false.
func (mc *MetricsCollector) RecordHistogram(name string, value float64) {
	if !mc.config.EnableHistograms {
		return
	}

	entry := MetricEntry{
		Name:      name,
		Value:     value,
		Timestamp: time.Now().Unix(),
		Type:      "histogram",
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()

	if len(mc.metrics) < mc.config.MaxMetrics {
		mc.metrics = append(mc.metrics, entry)
	}
	mc.histValues[name] = append(mc.histValues[name], value)

	// Update latest with the most recent observation.
	cp := entry
	mc.latest[name] = &cp
}

// Get returns the latest entry for the named metric. Returns nil if no
// entry has been recorded for that name.
func (mc *MetricsCollector) Get(name string) *MetricEntry {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	e, ok := mc.latest[name]
	if !ok {
		return nil
	}
	cp := *e
	cp.Tags = copyTags(e.Tags)
	return &cp
}

// GetAll returns a copy of every recorded metric entry.
func (mc *MetricsCollector) GetAll() []MetricEntry {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make([]MetricEntry, len(mc.metrics))
	for i, e := range mc.metrics {
		result[i] = e
		result[i].Tags = copyTags(e.Tags)
	}
	return result
}

// GetByTag returns all entries where Tags[key] == value.
func (mc *MetricsCollector) GetByTag(key, value string) []MetricEntry {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	var result []MetricEntry
	for _, e := range mc.metrics {
		if e.Tags != nil && e.Tags[key] == value {
			cp := e
			cp.Tags = copyTags(e.Tags)
			result = append(result, cp)
		}
	}
	return result
}

// Flush returns all stored metrics and resets the collector state.
func (mc *MetricsCollector) Flush() []MetricEntry {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	result := make([]MetricEntry, len(mc.metrics))
	for i, e := range mc.metrics {
		result[i] = e
		result[i].Tags = copyTags(e.Tags)
	}

	mc.metrics = make([]MetricEntry, 0, 64)
	mc.latest = make(map[string]*MetricEntry)
	mc.histValues = make(map[string][]float64)
	return result
}

// Summary returns a map of metric names to their latest values.
func (mc *MetricsCollector) Summary() map[string]float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make(map[string]float64, len(mc.latest))
	for name, e := range mc.latest {
		result[name] = e.Value
	}
	return result
}

// HistogramPercentile computes the given percentile (0-100) for the named
// histogram. Returns 0 if no observations exist or histograms are disabled.
func (mc *MetricsCollector) HistogramPercentile(name string, percentile float64) float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	vals, ok := mc.histValues[name]
	if !ok || len(vals) == 0 {
		return 0
	}

	// Work on a sorted copy so we don't mutate internal state.
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)

	if percentile <= 0 {
		return sorted[0]
	}
	if percentile >= 100 {
		return sorted[len(sorted)-1]
	}

	// Linear interpolation for the percentile rank.
	rank := (percentile / 100) * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := rank - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

// MetricCount returns the total number of stored metric entries.
func (mc *MetricsCollector) MetricCount() int {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return len(mc.metrics)
}

// copyTags returns a shallow copy of a tag map.
func copyTags(tags map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	cp := make(map[string]string, len(tags))
	for k, v := range tags {
		cp[k] = v
	}
	return cp
}
