package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Meter tracks the rate of events over time using 1-, 5-, and 15-minute
// exponentially weighted moving averages, similar to Unix load averages.
type Meter struct {
	count     atomic.Int64
	rate1     *EWMA
	rate5     *EWMA
	rate15    *EWMA
	startTime time.Time

	mu       sync.Mutex
	lastTick time.Time
}

// NewMeter creates a new Meter and initializes its start time.
func NewMeter() *Meter {
	now := time.Now()
	return &Meter{
		rate1:     NewEWMA1(),
		rate5:     NewEWMA5(),
		rate15:    NewEWMA15(),
		startTime: now,
		lastTick:  now,
	}
}

// Mark records n events.
func (m *Meter) Mark(n int64) {
	m.count.Add(n)
	m.rate1.Update(n)
	m.rate5.Update(n)
	m.rate15.Update(n)
	m.tickIfNeeded()
}

// tickIfNeeded ticks the EWMAs if 5 seconds have elapsed since the last tick.
func (m *Meter) tickIfNeeded() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(m.lastTick)
	// Tick every 5 seconds.
	for elapsed >= 5*time.Second {
		m.rate1.Tick()
		m.rate5.Tick()
		m.rate15.Tick()
		m.lastTick = m.lastTick.Add(5 * time.Second)
		elapsed = now.Sub(m.lastTick)
	}
}

// Count returns the total number of events recorded.
func (m *Meter) Count() int64 {
	return m.count.Load()
}

// Rate1 returns the 1-minute EWMA rate per second.
func (m *Meter) Rate1() float64 {
	m.tickIfNeeded()
	return m.rate1.Rate()
}

// Rate5 returns the 5-minute EWMA rate per second.
func (m *Meter) Rate5() float64 {
	m.tickIfNeeded()
	return m.rate5.Rate()
}

// Rate15 returns the 15-minute EWMA rate per second.
func (m *Meter) Rate15() float64 {
	m.tickIfNeeded()
	return m.rate15.Rate()
}

// RateMean returns the mean rate since the meter was created.
func (m *Meter) RateMean() float64 {
	elapsed := time.Since(m.startTime).Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(m.count.Load()) / elapsed
}
