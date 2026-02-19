package metrics

import (
	"math"
	"sync"
	"sync/atomic"
)

// EWMA implements an exponentially weighted moving average.
// It is safe for concurrent use.
type EWMA struct {
	alpha     float64
	uncounted atomic.Int64
	mu        sync.Mutex
	rate      float64
	init      bool
	interval  float64 // tick interval in seconds
}

// StandardEWMA creates a new EWMA with the given alpha decay factor.
// The tick interval is 5 seconds by default.
func StandardEWMA(alpha float64) *EWMA {
	return &EWMA{
		alpha:    alpha,
		interval: 5.0,
	}
}

// NewEWMA1 creates a 1-minute EWMA (alpha = 1 - exp(-5s/60s)).
func NewEWMA1() *EWMA {
	return StandardEWMA(1 - math.Exp(-5.0/60.0))
}

// NewEWMA5 creates a 5-minute EWMA (alpha = 1 - exp(-5s/300s)).
func NewEWMA5() *EWMA {
	return StandardEWMA(1 - math.Exp(-5.0/300.0))
}

// NewEWMA15 creates a 15-minute EWMA (alpha = 1 - exp(-5s/900s)).
func NewEWMA15() *EWMA {
	return StandardEWMA(1 - math.Exp(-5.0/900.0))
}

// Update adds n samples to the uncounted total.
func (e *EWMA) Update(n int64) {
	e.uncounted.Add(n)
}

// Tick decays the rate and incorporates uncounted samples.
// It should be called at regular intervals (every 5 seconds by default).
func (e *EWMA) Tick() {
	count := e.uncounted.Swap(0)
	instantRate := float64(count) / e.interval

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.init {
		e.rate += e.alpha * (instantRate - e.rate)
	} else {
		e.rate = instantRate
		e.init = true
	}
}

// Rate returns the current rate per second.
func (e *EWMA) Rate() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rate
}
