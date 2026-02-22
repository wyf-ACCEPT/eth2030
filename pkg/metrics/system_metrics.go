// system_metrics.go provides collection and export of runtime system metrics
// including goroutine count, memory usage, GC statistics, disk usage, and
// configurable chain-level metrics (peer count, block height, sync progress).
package metrics

import (
	"encoding/json"
	"runtime"
	"sync"
	"time"
)

// MemStats holds key memory statistics from the Go runtime.
type MemStats struct {
	// HeapAlloc is the number of bytes of allocated heap objects.
	HeapAlloc uint64 `json:"heapAlloc"`

	// TotalAlloc is the cumulative bytes allocated for heap objects.
	TotalAlloc uint64 `json:"totalAlloc"`

	// Sys is the total bytes of memory obtained from the OS.
	Sys uint64 `json:"sys"`

	// NumGC is the number of completed GC cycles.
	NumGC uint64 `json:"numGC"`
}

// DiskStats holds disk usage information.
type DiskStats struct {
	// Total is the total capacity of the disk in bytes.
	Total uint64 `json:"total"`

	// Used is the number of bytes in use on the disk.
	Used uint64 `json:"used"`

	// Free is the number of bytes available on the disk.
	Free uint64 `json:"free"`
}

// PeerCountFunc is a callback that returns the current peer count.
type PeerCountFunc func() int

// BlockHeightFunc is a callback that returns the current block height.
type BlockHeightFunc func() uint64

// SyncProgressFunc is a callback that returns the chain sync progress
// as a float64 between 0.0 (not synced) and 1.0 (fully synced).
type SyncProgressFunc func() float64

// DiskUsageFunc is a callback that returns disk usage for a given path.
type DiskUsageFunc func(path string) DiskStats

// SystemMetrics tracks key system-level metrics for the Ethereum client.
type SystemMetrics struct {
	mu        sync.RWMutex
	startTime time.Time

	// Cached snapshot from the last Collect() call.
	memStats    MemStats
	goroutines  int
	lastCollect time.Time

	// Configurable callbacks for chain-level metrics.
	peerCountFn    PeerCountFunc
	blockHeightFn  BlockHeightFunc
	syncProgressFn SyncProgressFunc
	diskUsageFn    DiskUsageFunc
}

// NewSystemMetrics creates a new SystemMetrics instance. Callbacks default
// to no-op functions returning zero values; use Set*Func methods to override.
func NewSystemMetrics() *SystemMetrics {
	return &SystemMetrics{
		startTime:      time.Now(),
		peerCountFn:    func() int { return 0 },
		blockHeightFn:  func() uint64 { return 0 },
		syncProgressFn: func() float64 { return 0.0 },
		diskUsageFn:    func(path string) DiskStats { return DiskStats{} },
	}
}

// SetPeerCountFunc sets the callback for retrieving the current peer count.
func (sm *SystemMetrics) SetPeerCountFunc(fn PeerCountFunc) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if fn != nil {
		sm.peerCountFn = fn
	}
}

// SetBlockHeightFunc sets the callback for retrieving the current block height.
func (sm *SystemMetrics) SetBlockHeightFunc(fn BlockHeightFunc) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if fn != nil {
		sm.blockHeightFn = fn
	}
}

// SetSyncProgressFunc sets the callback for retrieving the sync progress.
func (sm *SystemMetrics) SetSyncProgressFunc(fn SyncProgressFunc) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if fn != nil {
		sm.syncProgressFn = fn
	}
}

// SetDiskUsageFunc sets the callback for retrieving disk usage.
func (sm *SystemMetrics) SetDiskUsageFunc(fn DiskUsageFunc) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if fn != nil {
		sm.diskUsageFn = fn
	}
}

// Collect takes a snapshot of the current system metrics from the Go runtime.
// Call this periodically (e.g. every few seconds) to update cached values.
func (sm *SystemMetrics) Collect() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.memStats = MemStats{
		HeapAlloc:  ms.HeapAlloc,
		TotalAlloc: ms.TotalAlloc,
		Sys:        ms.Sys,
		NumGC:      uint64(ms.NumGC),
	}
	sm.goroutines = runtime.NumGoroutine()
	sm.lastCollect = time.Now()
}

// GoRoutineCount returns the number of goroutines at the last Collect() call.
// If Collect() has not been called, reads the current goroutine count directly.
func (sm *SystemMetrics) GoRoutineCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.goroutines == 0 {
		return runtime.NumGoroutine()
	}
	return sm.goroutines
}

// MemoryUsage returns the memory statistics from the last Collect() call.
// If Collect() has not been called, performs a live read.
func (sm *SystemMetrics) MemoryUsage() MemStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.lastCollect.IsZero() {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return MemStats{
			HeapAlloc:  ms.HeapAlloc,
			TotalAlloc: ms.TotalAlloc,
			Sys:        ms.Sys,
			NumGC:      uint64(ms.NumGC),
		}
	}
	return sm.memStats
}

// DiskUsage returns disk usage statistics for the given path by invoking
// the configured disk usage callback.
func (sm *SystemMetrics) DiskUsage(path string) DiskStats {
	sm.mu.RLock()
	fn := sm.diskUsageFn
	sm.mu.RUnlock()
	return fn(path)
}

// UptimeSeconds returns the number of seconds since the SystemMetrics
// instance was created.
func (sm *SystemMetrics) UptimeSeconds() float64 {
	return time.Since(sm.startTime).Seconds()
}

// PeerCount returns the current peer count by invoking the callback.
func (sm *SystemMetrics) PeerCount() int {
	sm.mu.RLock()
	fn := sm.peerCountFn
	sm.mu.RUnlock()
	return fn()
}

// BlockHeight returns the current block height by invoking the callback.
func (sm *SystemMetrics) BlockHeight() uint64 {
	sm.mu.RLock()
	fn := sm.blockHeightFn
	sm.mu.RUnlock()
	return fn()
}

// ChainSyncProgress returns the chain sync progress as a float64 between
// 0.0 (not synced) and 1.0 (fully synced).
func (sm *SystemMetrics) ChainSyncProgress() float64 {
	sm.mu.RLock()
	fn := sm.syncProgressFn
	sm.mu.RUnlock()

	p := fn()
	// Clamp to [0.0, 1.0].
	if p < 0.0 {
		return 0.0
	}
	if p > 1.0 {
		return 1.0
	}
	return p
}

// metricsSnapshot is the internal type used for JSON serialization of all
// system metrics.
type metricsSnapshot struct {
	Goroutines   int      `json:"goroutines"`
	Memory       MemStats `json:"memory"`
	UptimeSec    float64  `json:"uptimeSeconds"`
	PeerCount    int      `json:"peerCount"`
	BlockHeight  uint64   `json:"blockHeight"`
	SyncProgress float64  `json:"syncProgress"`
	CollectedAt  string   `json:"collectedAt"`
}

// ExportJSON serializes all current metrics as a JSON object. It performs
// a fresh Collect() before exporting to ensure up-to-date values.
func (sm *SystemMetrics) ExportJSON() ([]byte, error) {
	sm.Collect()

	sm.mu.RLock()
	memSnap := sm.memStats
	goroutineSnap := sm.goroutines
	sm.mu.RUnlock()

	snapshot := metricsSnapshot{
		Goroutines:   goroutineSnap,
		Memory:       memSnap,
		UptimeSec:    sm.UptimeSeconds(),
		PeerCount:    sm.PeerCount(),
		BlockHeight:  sm.BlockHeight(),
		SyncProgress: sm.ChainSyncProgress(),
		CollectedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	return json.Marshal(snapshot)
}

// LastCollectTime returns the time of the last Collect() call, or zero
// if Collect() has never been called.
func (sm *SystemMetrics) LastCollectTime() time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.lastCollect
}

// GoVersion returns the Go runtime version string.
func GoVersion() string {
	return runtime.Version()
}

// NumCPU returns the number of logical CPUs available.
func NumCPU() int {
	return runtime.NumCPU()
}

// GOARCH returns the target architecture.
func GOARCH() string {
	return runtime.GOARCH
}

// GOOS returns the target operating system.
func GOOS() string {
	return runtime.GOOS
}
