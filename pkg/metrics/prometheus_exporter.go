package metrics

import (
	"fmt"
	"math"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// PrometheusExporter serves metrics in Prometheus text exposition format at
// the /metrics HTTP endpoint. It collects metrics from a Registry and
// supports custom collector registration and runtime metrics (goroutines,
// memory, GC stats).

// PrometheusConfig configures the Prometheus exporter.
type PrometheusConfig struct {
	// Namespace is an optional prefix prepended to all metric names
	// (e.g. "ETH2030" produces "ETH2030_chain_height").
	Namespace string
	// EnableRuntime controls whether Go runtime metrics (goroutines,
	// memory, GC) are included in the output.
	EnableRuntime bool
	// Path is the HTTP path to serve metrics on (default "/metrics").
	Path string
}

// DefaultPrometheusConfig returns a config with sensible defaults.
func DefaultPrometheusConfig() PrometheusConfig {
	return PrometheusConfig{
		Namespace:     "ETH2030",
		EnableRuntime: true,
		Path:          "/metrics",
	}
}

// CustomCollector is an interface for registering arbitrary metric producers
// that are called during each scrape.
type CustomCollector interface {
	// Collect returns a set of metric lines in Prometheus text format.
	// Each entry should be a complete metric line (name, labels, value).
	Collect() []MetricLine
}

// MetricLine represents a single Prometheus metric data point with optional labels.
type MetricLine struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// PrometheusExporter formats and serves metrics over HTTP.
type PrometheusExporter struct {
	mu         sync.RWMutex
	config     PrometheusConfig
	registry   *Registry
	collectors map[string]CustomCollector
}

// NewPrometheusExporter creates a new exporter that reads from the given registry.
func NewPrometheusExporter(registry *Registry, config PrometheusConfig) *PrometheusExporter {
	if config.Path == "" {
		config.Path = "/metrics"
	}
	return &PrometheusExporter{
		config:     config,
		registry:   registry,
		collectors: make(map[string]CustomCollector),
	}
}

// RegisterCollector adds a named custom collector. If a collector with the
// same name exists, it is replaced.
func (pe *PrometheusExporter) RegisterCollector(name string, c CustomCollector) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	pe.collectors[name] = c
}

// UnregisterCollector removes a previously registered custom collector.
func (pe *PrometheusExporter) UnregisterCollector(name string) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	delete(pe.collectors, name)
}

// Handler returns an http.Handler that serves the /metrics endpoint.
func (pe *PrometheusExporter) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(pe.config.Path, pe.handleMetrics)
	return mux
}

// handleMetrics generates the Prometheus exposition format response.
func (pe *PrometheusExporter) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	var b strings.Builder
	pe.writeRegistryMetrics(&b)

	if pe.config.EnableRuntime {
		pe.writeRuntimeMetrics(&b)
	}

	pe.writeCustomCollectors(&b)

	w.Write([]byte(b.String()))
}

// writeRegistryMetrics formats all metrics from the registry.
func (pe *PrometheusExporter) writeRegistryMetrics(b *strings.Builder) {
	pe.registry.mu.RLock()
	defer pe.registry.mu.RUnlock()

	// Sort counter names for deterministic output.
	counterNames := sortedKeys(pe.registry.counters)
	for _, name := range counterNames {
		c := pe.registry.counters[name]
		promName := pe.promName(name)
		writeHelp(b, promName, "counter", name)
		writeType(b, promName, "counter")
		fmt.Fprintf(b, "%s %d\n", promName, c.Value())
	}

	// Gauges.
	gaugeNames := sortedKeys(pe.registry.gauges)
	for _, name := range gaugeNames {
		g := pe.registry.gauges[name]
		promName := pe.promName(name)
		writeHelp(b, promName, "gauge", name)
		writeType(b, promName, "gauge")
		fmt.Fprintf(b, "%s %d\n", promName, g.Value())
	}

	// Histograms: emit _count, _sum, _min, _max, _mean as gauges.
	histNames := sortedKeys(pe.registry.histograms)
	for _, name := range histNames {
		h := pe.registry.histograms[name]
		promName := pe.promName(name)
		writeHelp(b, promName, "summary", name)
		writeType(b, promName, "summary")
		fmt.Fprintf(b, "%s_count %d\n", promName, h.Count())
		fmt.Fprintf(b, "%s_sum %s\n", promName, formatFloat(h.Sum()))
		if h.Count() > 0 {
			fmt.Fprintf(b, "%s_min %s\n", promName, formatFloat(h.Min()))
			fmt.Fprintf(b, "%s_max %s\n", promName, formatFloat(h.Max()))
			fmt.Fprintf(b, "%s_mean %s\n", promName, formatFloat(h.Mean()))
		}
	}
}

// writeRuntimeMetrics emits Go runtime metrics: goroutines, memory, GC.
func (pe *PrometheusExporter) writeRuntimeMetrics(b *strings.Builder) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	prefix := pe.config.Namespace
	if prefix != "" {
		prefix += "_"
	}

	// Goroutines.
	goroutineName := prefix + "go_goroutines"
	writeHelp(b, goroutineName, "gauge", "Number of active goroutines")
	writeType(b, goroutineName, "gauge")
	fmt.Fprintf(b, "%s %d\n", goroutineName, runtime.NumGoroutine())

	// Threads.
	threadName := prefix + "go_threads"
	writeHelp(b, threadName, "gauge", "Number of OS threads")
	writeType(b, threadName, "gauge")
	numCPU := runtime.GOMAXPROCS(0)
	fmt.Fprintf(b, "%s %d\n", threadName, numCPU)

	// Memory: alloc, total alloc, sys, heap.
	writeMemMetric(b, prefix+"go_memstats_alloc_bytes", "gauge",
		"Bytes of allocated heap objects", m.Alloc)
	writeMemMetric(b, prefix+"go_memstats_alloc_bytes_total", "counter",
		"Total bytes allocated", m.TotalAlloc)
	writeMemMetric(b, prefix+"go_memstats_sys_bytes", "gauge",
		"Bytes of memory obtained from the OS", m.Sys)
	writeMemMetric(b, prefix+"go_memstats_heap_alloc_bytes", "gauge",
		"Bytes of allocated heap objects", m.HeapAlloc)
	writeMemMetric(b, prefix+"go_memstats_heap_inuse_bytes", "gauge",
		"Bytes in in-use heap spans", m.HeapInuse)
	writeMemMetric(b, prefix+"go_memstats_heap_objects", "gauge",
		"Number of allocated heap objects", m.HeapObjects)
	writeMemMetric(b, prefix+"go_memstats_stack_inuse_bytes", "gauge",
		"Bytes in stack spans", m.StackInuse)

	// GC metrics.
	gcName := prefix + "go_gc_duration_seconds_count"
	writeHelp(b, gcName, "counter", "Total number of GC cycles")
	writeType(b, gcName, "counter")
	fmt.Fprintf(b, "%s %d\n", gcName, m.NumGC)

	gcPauseName := prefix + "go_gc_pause_total_seconds"
	writeHelp(b, gcPauseName, "counter", "Total GC pause time in seconds")
	writeType(b, gcPauseName, "counter")
	fmt.Fprintf(b, "%s %s\n", gcPauseName,
		formatFloat(float64(m.PauseTotalNs)/1e9))

	// Last GC time.
	lastGCName := prefix + "go_gc_last_seconds"
	writeHelp(b, lastGCName, "gauge", "Timestamp of last GC in seconds since epoch")
	writeType(b, lastGCName, "gauge")
	if m.LastGC > 0 {
		fmt.Fprintf(b, "%s %s\n", lastGCName,
			formatFloat(float64(m.LastGC)/1e9))
	} else {
		fmt.Fprintf(b, "%s 0\n", lastGCName)
	}

	// Process start time.
	startName := prefix + "process_start_time_seconds"
	writeHelp(b, startName, "gauge", "Process start time in seconds since epoch")
	writeType(b, startName, "gauge")
	fmt.Fprintf(b, "%s %s\n", startName,
		formatFloat(float64(processStartTime.Unix())))
}

// writeCustomCollectors invokes each registered custom collector.
func (pe *PrometheusExporter) writeCustomCollectors(b *strings.Builder) {
	pe.mu.RLock()
	collectors := make(map[string]CustomCollector, len(pe.collectors))
	for k, v := range pe.collectors {
		collectors[k] = v
	}
	pe.mu.RUnlock()

	for _, c := range collectors {
		lines := c.Collect()
		for _, line := range lines {
			promName := pe.promName(line.Name)
			if len(line.Labels) > 0 {
				fmt.Fprintf(b, "%s{%s} %s\n", promName,
					formatLabels(line.Labels), formatFloat(line.Value))
			} else {
				fmt.Fprintf(b, "%s %s\n", promName, formatFloat(line.Value))
			}
		}
	}
}

// promName converts a dot-separated metric name to Prometheus format:
// dots become underscores, and the namespace prefix is prepended.
func (pe *PrometheusExporter) promName(name string) string {
	sanitized := strings.ReplaceAll(name, ".", "_")
	sanitized = strings.ReplaceAll(sanitized, "-", "_")
	if pe.config.Namespace != "" {
		return pe.config.Namespace + "_" + sanitized
	}
	return sanitized
}

// formatLabels converts a label map to Prometheus label format: key="value",...
func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	// Sort for deterministic output.
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, labels[k]))
	}
	return strings.Join(parts, ",")
}

// formatFloat formats a float64 for Prometheus output, handling special values.
func formatFloat(v float64) string {
	if math.IsInf(v, 1) {
		return "+Inf"
	}
	if math.IsInf(v, -1) {
		return "-Inf"
	}
	if math.IsNaN(v) {
		return "NaN"
	}
	return fmt.Sprintf("%g", v)
}

// writeHelp writes a HELP line for a metric.
func writeHelp(b *strings.Builder, name, metricType, description string) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, description)
}

// writeType writes a TYPE line for a metric.
func writeType(b *strings.Builder, name, metricType string) {
	fmt.Fprintf(b, "# TYPE %s %s\n", name, metricType)
}

// writeMemMetric writes a memory metric line.
func writeMemMetric(b *strings.Builder, name, metricType, help string, value uint64) {
	writeHelp(b, name, metricType, help)
	writeType(b, name, metricType)
	fmt.Fprintf(b, "%s %d\n", name, value)
}

// sortedKeys returns a sorted list of keys from a map of any metric type.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// processStartTime is recorded at init for process_start_time_seconds.
var processStartTime = time.Now()
