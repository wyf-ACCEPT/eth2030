package metrics

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CPUStats holds process CPU usage statistics.
type CPUStats struct {
	// GlobalTime is the total CPU time consumed by all processes (jiffies).
	GlobalTime int64
	// GlobalWait is the total I/O wait time (jiffies).
	GlobalWait int64
	// LocalTime is the CPU time consumed by this process (jiffies).
	LocalTime int64
}

// ReadCPUStats returns current process CPU usage by reading /proc.
// On non-Linux systems it falls back to runtime-based estimates.
func ReadCPUStats() *CPUStats {
	stats := &CPUStats{}

	// Try to read process CPU from /proc/self/stat (Linux).
	// The comm field (field 2) is in parentheses and may contain spaces,
	// so we find the closing ')' first and split the rest.
	if data, err := os.ReadFile("/proc/self/stat"); err == nil {
		s := string(data)
		if idx := strings.LastIndex(s, ")"); idx >= 0 {
			rest := strings.Fields(s[idx+1:])
			// rest[0] is state (field 3), utime is field 14 -> rest[11], stime is field 15 -> rest[12]
			if len(rest) > 12 {
				utime, _ := strconv.ParseInt(rest[11], 10, 64)
				stime, _ := strconv.ParseInt(rest[12], 10, 64)
				stats.LocalTime = utime + stime
			}
		}
	} else {
		// Fallback: use Go runtime goroutine count as rough proxy.
		stats.LocalTime = int64(runtime.NumGoroutine())
	}

	// Try to read global CPU from /proc/stat.
	if data, err := os.ReadFile("/proc/stat"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "cpu ") {
				fields := strings.Fields(line)
				if len(fields) >= 5 {
					for i := 1; i < len(fields); i++ {
						v, _ := strconv.ParseInt(fields[i], 10, 64)
						stats.GlobalTime += v
					}
				}
				if len(fields) >= 6 {
					stats.GlobalWait, _ = strconv.ParseInt(fields[5], 10, 64)
				}
				break
			}
		}
	}

	return stats
}

// CPUTracker tracks CPU usage over time by sampling at intervals.
type CPUTracker struct {
	mu       sync.Mutex
	prev     *CPUStats
	prevTime time.Time
	usage    float64 // current CPU utilization as a percentage
}

// NewCPUTracker creates a CPUTracker with an initial sample.
func NewCPUTracker() *CPUTracker {
	return &CPUTracker{
		prev:     ReadCPUStats(),
		prevTime: time.Now(),
	}
}

// RecordCPU takes a new CPU sample and computes utilization since the last sample.
func (t *CPUTracker) RecordCPU() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	current := ReadCPUStats()

	elapsed := now.Sub(t.prevTime).Seconds()
	if elapsed > 0 && t.prev != nil {
		localDelta := float64(current.LocalTime - t.prev.LocalTime)
		globalDelta := float64(current.GlobalTime - t.prev.GlobalTime)

		if globalDelta > 0 {
			// Percentage of total CPU used by this process.
			t.usage = (localDelta / globalDelta) * 100.0 * float64(runtime.NumCPU())
		} else {
			t.usage = 0
		}
	}

	t.prev = current
	t.prevTime = now
}

// Usage returns the current CPU utilization percentage (0-100*numCPU).
func (t *CPUTracker) Usage() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.usage
}
