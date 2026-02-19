package metrics

import (
	"math"
	"testing"
)

func TestEWMAUpdate(t *testing.T) {
	e := NewEWMA1()

	// Before any tick, rate should be 0.
	if r := e.Rate(); r != 0 {
		t.Errorf("initial rate = %f, want 0", r)
	}

	// Add 100 samples, then tick.
	e.Update(100)
	e.Tick()

	// After first tick, rate should be 100/5 = 20 per second.
	r := e.Rate()
	if math.Abs(r-20.0) > 0.001 {
		t.Errorf("rate after first tick = %f, want 20", r)
	}
}

func TestEWMADecay(t *testing.T) {
	e := NewEWMA1()

	// Add samples and tick.
	e.Update(100)
	e.Tick()

	initialRate := e.Rate()

	// Tick again without new samples - rate should decay.
	e.Tick()
	decayedRate := e.Rate()

	if decayedRate >= initialRate {
		t.Errorf("rate should decay: initial=%f, decayed=%f", initialRate, decayedRate)
	}
	if decayedRate <= 0 {
		t.Errorf("rate should still be positive after one decay: %f", decayedRate)
	}
}

func TestEWMA5(t *testing.T) {
	e := NewEWMA5()
	e.Update(50)
	e.Tick()

	r := e.Rate()
	expected := 10.0 // 50/5
	if math.Abs(r-expected) > 0.001 {
		t.Errorf("EWMA5 rate = %f, want %f", r, expected)
	}
}

func TestEWMA15(t *testing.T) {
	e := NewEWMA15()
	e.Update(50)
	e.Tick()

	r := e.Rate()
	expected := 10.0 // 50/5
	if math.Abs(r-expected) > 0.001 {
		t.Errorf("EWMA15 rate = %f, want %f", r, expected)
	}
}

func TestEWMAMultipleTicks(t *testing.T) {
	e := NewEWMA1()

	// Simulate steady 100 events per 5-second interval.
	for i := 0; i < 12; i++ {
		e.Update(100)
		e.Tick()
	}

	// Should converge to ~20 events/sec.
	r := e.Rate()
	if math.Abs(r-20.0) > 1.0 {
		t.Errorf("rate after 12 ticks = %f, want ~20", r)
	}
}

func TestStandardEWMA(t *testing.T) {
	// Custom alpha.
	e := StandardEWMA(0.5)
	e.Update(10)
	e.Tick()

	r := e.Rate()
	if r != 2.0 { // 10/5 = 2
		t.Errorf("custom EWMA rate = %f, want 2", r)
	}
}
