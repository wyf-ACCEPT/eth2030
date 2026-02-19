package metrics

import (
	"testing"
	"time"
)

func TestMeterCount(t *testing.T) {
	m := NewMeter()
	m.Mark(5)
	m.Mark(3)

	if c := m.Count(); c != 8 {
		t.Errorf("count = %d, want 8", c)
	}
}

func TestMeterRates(t *testing.T) {
	m := NewMeter()

	// Mark events.
	m.Mark(100)

	// Force ticks by manipulating lastTick.
	m.mu.Lock()
	m.lastTick = m.lastTick.Add(-10 * time.Second)
	m.mu.Unlock()

	// Now Rate1 should trigger ticks and return a non-zero value.
	r1 := m.Rate1()
	r5 := m.Rate5()
	r15 := m.Rate15()

	if r1 == 0 {
		t.Error("Rate1 should be non-zero after marking events and ticking")
	}
	if r5 == 0 {
		t.Error("Rate5 should be non-zero after marking events and ticking")
	}
	if r15 == 0 {
		t.Error("Rate15 should be non-zero after marking events and ticking")
	}
}

func TestMeterRateMean(t *testing.T) {
	m := NewMeter()
	// Set start time to 1 second ago.
	m.startTime = time.Now().Add(-1 * time.Second)
	m.Mark(100)

	mean := m.RateMean()
	// Mean should be approximately 100 events/sec.
	if mean < 50 || mean > 200 {
		t.Errorf("RateMean = %f, want roughly 100", mean)
	}
}

func TestMeterZero(t *testing.T) {
	m := NewMeter()
	if c := m.Count(); c != 0 {
		t.Errorf("initial count = %d, want 0", c)
	}
	// RateMean with near-zero elapsed time should return 0 or near 0.
	// Just ensure it doesn't panic.
	_ = m.RateMean()
}
