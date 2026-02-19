package metrics

import (
	"testing"
)

func TestReadCPUStats(t *testing.T) {
	stats := ReadCPUStats()
	if stats == nil {
		t.Fatal("ReadCPUStats returned nil")
	}
	// LocalTime may be 0 if the test process has used negligible CPU.
	// Just verify it's non-negative and the function doesn't error.
	if stats.LocalTime < 0 {
		t.Errorf("LocalTime = %d, want >= 0", stats.LocalTime)
	}
	// GlobalTime should be non-negative; may be 0 in sandboxed environments.
	if stats.GlobalTime < 0 {
		t.Errorf("GlobalTime = %d, want >= 0", stats.GlobalTime)
	}
}

func TestCPUTracker(t *testing.T) {
	tracker := NewCPUTracker()
	if tracker == nil {
		t.Fatal("NewCPUTracker returned nil")
	}

	// Initial usage should be 0.
	if u := tracker.Usage(); u != 0 {
		t.Errorf("initial usage = %f, want 0", u)
	}

	// RecordCPU should not panic.
	tracker.RecordCPU()

	// After recording, usage should be >= 0 (could be 0 if no delta).
	u := tracker.Usage()
	if u < 0 {
		t.Errorf("usage = %f, want >= 0", u)
	}
}

func TestCPUTrackerMultipleSamples(t *testing.T) {
	tracker := NewCPUTracker()

	// Take a few samples to ensure stability.
	for i := 0; i < 3; i++ {
		tracker.RecordCPU()
	}

	u := tracker.Usage()
	if u < 0 {
		t.Errorf("usage after multiple samples = %f, want >= 0", u)
	}
}
