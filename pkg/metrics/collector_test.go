package metrics

import (
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestNewMetricsCollector(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{})
	if mc.config.MaxMetrics != 10000 {
		t.Fatalf("default MaxMetrics = %d, want 10000", mc.config.MaxMetrics)
	}
	if mc.config.FlushInterval != 60 {
		t.Fatalf("default FlushInterval = %d, want 60", mc.config.FlushInterval)
	}
	if mc.MetricCount() != 0 {
		t.Fatalf("MetricCount() = %d, want 0", mc.MetricCount())
	}
}

func TestNewMetricsCollectorCustomConfig(t *testing.T) {
	cfg := CollectorConfig{
		FlushInterval:    10,
		MaxMetrics:       50,
		EnableHistograms: true,
	}
	mc := NewMetricsCollector(cfg)
	if mc.config.MaxMetrics != 50 {
		t.Fatalf("MaxMetrics = %d, want 50", mc.config.MaxMetrics)
	}
	if mc.config.FlushInterval != 10 {
		t.Fatalf("FlushInterval = %d, want 10", mc.config.FlushInterval)
	}
	if !mc.config.EnableHistograms {
		t.Fatal("EnableHistograms should be true")
	}
}

func TestRecord(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{MaxMetrics: 100})
	mc.Record("chain.height", 42, map[string]string{"network": "mainnet"})

	if mc.MetricCount() != 1 {
		t.Fatalf("MetricCount() = %d, want 1", mc.MetricCount())
	}
	e := mc.Get("chain.height")
	if e == nil {
		t.Fatal("Get returned nil")
	}
	if e.Name != "chain.height" {
		t.Fatalf("Name = %q, want %q", e.Name, "chain.height")
	}
	if e.Value != 42 {
		t.Fatalf("Value = %f, want 42", e.Value)
	}
	if e.Type != "gauge" {
		t.Fatalf("Type = %q, want %q", e.Type, "gauge")
	}
	if e.Tags["network"] != "mainnet" {
		t.Fatalf("Tag network = %q, want %q", e.Tags["network"], "mainnet")
	}
	if e.Timestamp <= 0 {
		t.Fatalf("Timestamp = %d, want > 0", e.Timestamp)
	}
}

func TestRecordNilTags(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{MaxMetrics: 100})
	mc.Record("p2p.peers", 10, nil)

	e := mc.Get("p2p.peers")
	if e == nil {
		t.Fatal("Get returned nil")
	}
	if e.Tags != nil {
		t.Fatalf("Tags = %v, want nil", e.Tags)
	}
}

func TestRecordOverwrite(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{MaxMetrics: 100})
	mc.Record("val", 1, nil)
	mc.Record("val", 2, nil)

	e := mc.Get("val")
	if e.Value != 2 {
		t.Fatalf("Value = %f after overwrite, want 2", e.Value)
	}
	// Both entries should exist in the log.
	if mc.MetricCount() != 2 {
		t.Fatalf("MetricCount() = %d, want 2", mc.MetricCount())
	}
}

func TestRecordMaxMetrics(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{MaxMetrics: 3})
	mc.Record("a", 1, nil)
	mc.Record("b", 2, nil)
	mc.Record("c", 3, nil)
	mc.Record("d", 4, nil) // exceeds max

	if mc.MetricCount() != 3 {
		t.Fatalf("MetricCount() = %d, want 3 (capped)", mc.MetricCount())
	}
	// Latest should still be updated even if log is full.
	e := mc.Get("d")
	if e == nil {
		t.Fatal("Get(d) returned nil; latest should still be updated")
	}
}

func TestRecordHistogram(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{
		MaxMetrics:       100,
		EnableHistograms: true,
	})
	mc.RecordHistogram("latency", 10)
	mc.RecordHistogram("latency", 20)
	mc.RecordHistogram("latency", 30)

	if mc.MetricCount() != 3 {
		t.Fatalf("MetricCount() = %d, want 3", mc.MetricCount())
	}
	e := mc.Get("latency")
	if e == nil {
		t.Fatal("Get returned nil")
	}
	if e.Type != "histogram" {
		t.Fatalf("Type = %q, want %q", e.Type, "histogram")
	}
}

func TestRecordHistogramDisabled(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{
		MaxMetrics:       100,
		EnableHistograms: false,
	})
	mc.RecordHistogram("latency", 10)

	if mc.MetricCount() != 0 {
		t.Fatalf("MetricCount() = %d, want 0 (histograms disabled)", mc.MetricCount())
	}
}

func TestGetNotFound(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{})
	if mc.Get("nonexistent") != nil {
		t.Fatal("expected nil for nonexistent metric")
	}
}

func TestGetReturnsCopy(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{MaxMetrics: 100})
	mc.Record("x", 1, map[string]string{"k": "v"})
	e := mc.Get("x")
	e.Value = 999
	e.Tags["k"] = "mutated"

	original := mc.Get("x")
	if original.Value == 999 {
		t.Fatal("Get did not return a copy (value mutated)")
	}
	if original.Tags["k"] == "mutated" {
		t.Fatal("Get did not return a copy (tags mutated)")
	}
}

func TestGetAll(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{MaxMetrics: 100})
	mc.Record("a", 1, nil)
	mc.Record("b", 2, nil)
	mc.Record("c", 3, nil)

	all := mc.GetAll()
	if len(all) != 3 {
		t.Fatalf("GetAll() len = %d, want 3", len(all))
	}
}

func TestGetAllEmpty(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{})
	all := mc.GetAll()
	if len(all) != 0 {
		t.Fatalf("GetAll() len = %d, want 0", len(all))
	}
}

func TestGetByTag(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{MaxMetrics: 100})
	mc.Record("a", 1, map[string]string{"env": "prod"})
	mc.Record("b", 2, map[string]string{"env": "dev"})
	mc.Record("c", 3, map[string]string{"env": "prod"})
	mc.Record("d", 4, nil)

	prod := mc.GetByTag("env", "prod")
	if len(prod) != 2 {
		t.Fatalf("GetByTag(env, prod) = %d, want 2", len(prod))
	}
	dev := mc.GetByTag("env", "dev")
	if len(dev) != 1 {
		t.Fatalf("GetByTag(env, dev) = %d, want 1", len(dev))
	}
	none := mc.GetByTag("env", "staging")
	if len(none) != 0 {
		t.Fatalf("GetByTag(env, staging) = %d, want 0", len(none))
	}
}

func TestFlush(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{
		MaxMetrics:       100,
		EnableHistograms: true,
	})
	mc.Record("a", 1, nil)
	mc.Record("b", 2, nil)
	mc.RecordHistogram("h", 10)

	flushed := mc.Flush()
	if len(flushed) != 3 {
		t.Fatalf("Flush() returned %d entries, want 3", len(flushed))
	}
	if mc.MetricCount() != 0 {
		t.Fatalf("MetricCount() = %d after flush, want 0", mc.MetricCount())
	}
	if mc.Get("a") != nil {
		t.Fatal("Get(a) should return nil after flush")
	}
}

func TestFlushEmpty(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{})
	flushed := mc.Flush()
	if len(flushed) != 0 {
		t.Fatalf("Flush() returned %d entries on empty collector", len(flushed))
	}
}

func TestSummary(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{MaxMetrics: 100})
	mc.Record("chain.height", 100, nil)
	mc.Record("p2p.peers", 25, nil)
	mc.Record("chain.height", 101, nil) // overwrite

	s := mc.Summary()
	if len(s) != 2 {
		t.Fatalf("Summary() len = %d, want 2", len(s))
	}
	if s["chain.height"] != 101 {
		t.Fatalf("Summary[chain.height] = %f, want 101", s["chain.height"])
	}
	if s["p2p.peers"] != 25 {
		t.Fatalf("Summary[p2p.peers] = %f, want 25", s["p2p.peers"])
	}
}

func TestSummaryEmpty(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{})
	s := mc.Summary()
	if len(s) != 0 {
		t.Fatalf("Summary() len = %d, want 0", len(s))
	}
}

func TestHistogramPercentile(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{
		MaxMetrics:       100,
		EnableHistograms: true,
	})
	// Record 1..100.
	for i := 1; i <= 100; i++ {
		mc.RecordHistogram("latency", float64(i))
	}

	// p0 -> minimum.
	p0 := mc.HistogramPercentile("latency", 0)
	if p0 != 1 {
		t.Fatalf("p0 = %f, want 1", p0)
	}

	// p100 -> maximum.
	p100 := mc.HistogramPercentile("latency", 100)
	if p100 != 100 {
		t.Fatalf("p100 = %f, want 100", p100)
	}

	// p50 -> median (should be ~50.5 for 1..100).
	p50 := mc.HistogramPercentile("latency", 50)
	if math.Abs(p50-50.5) > 0.01 {
		t.Fatalf("p50 = %f, want ~50.5", p50)
	}

	// p99.
	p99 := mc.HistogramPercentile("latency", 99)
	if p99 < 98 {
		t.Fatalf("p99 = %f, want >= 98", p99)
	}
}

func TestHistogramPercentileNotFound(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{EnableHistograms: true})
	p := mc.HistogramPercentile("missing", 50)
	if p != 0 {
		t.Fatalf("HistogramPercentile(missing) = %f, want 0", p)
	}
}

func TestHistogramPercentileSingleValue(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{
		MaxMetrics:       100,
		EnableHistograms: true,
	})
	mc.RecordHistogram("x", 42)

	p50 := mc.HistogramPercentile("x", 50)
	if p50 != 42 {
		t.Fatalf("p50 = %f, want 42 (single value)", p50)
	}
}

func TestHistogramPercentileTwoValues(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{
		MaxMetrics:       100,
		EnableHistograms: true,
	})
	mc.RecordHistogram("x", 10)
	mc.RecordHistogram("x", 20)

	p50 := mc.HistogramPercentile("x", 50)
	if p50 != 15 {
		t.Fatalf("p50 = %f, want 15 (interpolation)", p50)
	}
}

func TestMetricCount(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{
		MaxMetrics:       100,
		EnableHistograms: true,
	})
	mc.Record("a", 1, nil)
	mc.RecordHistogram("h", 10)
	mc.Record("b", 2, map[string]string{"k": "v"})

	if mc.MetricCount() != 3 {
		t.Fatalf("MetricCount() = %d, want 3", mc.MetricCount())
	}
}

func TestConcurrentCollector(t *testing.T) {
	mc := NewMetricsCollector(CollectorConfig{
		MaxMetrics:       100000,
		EnableHistograms: true,
	})

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 4)

	// Concurrent Record.
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				mc.Record(fmt.Sprintf("metric-%d", g), float64(i), nil)
			}
		}(g)
	}

	// Concurrent RecordHistogram.
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				mc.RecordHistogram("shared.hist", float64(i))
			}
		}()
	}

	// Concurrent reads.
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				mc.Get("metric-0")
				mc.MetricCount()
				mc.Summary()
			}
		}()
	}

	// Concurrent GetByTag.
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				mc.GetByTag("env", "prod")
				mc.HistogramPercentile("shared.hist", 50)
			}
		}()
	}

	wg.Wait()

	if mc.MetricCount() == 0 {
		t.Fatal("expected some metrics after concurrent recording")
	}
}

func TestCopyTagsNil(t *testing.T) {
	cp := copyTags(nil)
	if cp != nil {
		t.Fatal("copyTags(nil) should return nil")
	}
}

func TestCopyTagsIsolation(t *testing.T) {
	orig := map[string]string{"a": "1", "b": "2"}
	cp := copyTags(orig)
	cp["a"] = "mutated"
	if orig["a"] == "mutated" {
		t.Fatal("copyTags did not produce an independent copy")
	}
}
