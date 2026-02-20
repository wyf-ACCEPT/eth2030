package metrics

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewSystemMetrics(t *testing.T) {
	sm := NewSystemMetrics()
	if sm == nil {
		t.Fatal("NewSystemMetrics returned nil")
	}
	if sm.startTime.IsZero() {
		t.Error("startTime should be set")
	}
}

func TestCollect(t *testing.T) {
	sm := NewSystemMetrics()

	// Before Collect(), lastCollect should be zero.
	if !sm.LastCollectTime().IsZero() {
		t.Error("expected zero LastCollectTime before Collect()")
	}

	sm.Collect()

	if sm.LastCollectTime().IsZero() {
		t.Error("expected non-zero LastCollectTime after Collect()")
	}
}

func TestGoRoutineCount(t *testing.T) {
	sm := NewSystemMetrics()

	// Before Collect(), should read live goroutine count.
	count := sm.GoRoutineCount()
	if count <= 0 {
		t.Errorf("GoRoutineCount = %d, want > 0", count)
	}

	// After Collect(), should return cached value.
	sm.Collect()
	count2 := sm.GoRoutineCount()
	if count2 <= 0 {
		t.Errorf("GoRoutineCount after Collect = %d, want > 0", count2)
	}
}

func TestMemoryUsage_BeforeCollect(t *testing.T) {
	sm := NewSystemMetrics()

	// Should do a live read before Collect().
	mem := sm.MemoryUsage()
	if mem.HeapAlloc == 0 {
		t.Error("HeapAlloc should be > 0")
	}
	if mem.Sys == 0 {
		t.Error("Sys should be > 0")
	}
}

func TestMemoryUsage_AfterCollect(t *testing.T) {
	sm := NewSystemMetrics()
	sm.Collect()

	mem := sm.MemoryUsage()
	if mem.HeapAlloc == 0 {
		t.Error("HeapAlloc should be > 0 after Collect()")
	}
	if mem.TotalAlloc == 0 {
		t.Error("TotalAlloc should be > 0 after Collect()")
	}
	if mem.Sys == 0 {
		t.Error("Sys should be > 0 after Collect()")
	}
}

func TestDiskUsage_Default(t *testing.T) {
	sm := NewSystemMetrics()

	// Default callback returns zero values.
	ds := sm.DiskUsage("/")
	if ds.Total != 0 || ds.Used != 0 || ds.Free != 0 {
		t.Errorf("default DiskUsage = %+v, want zeros", ds)
	}
}

func TestDiskUsage_Custom(t *testing.T) {
	sm := NewSystemMetrics()
	sm.SetDiskUsageFunc(func(path string) DiskStats {
		return DiskStats{
			Total: 1000000,
			Used:  500000,
			Free:  500000,
		}
	})

	ds := sm.DiskUsage("/data")
	if ds.Total != 1000000 {
		t.Errorf("Total = %d, want 1000000", ds.Total)
	}
	if ds.Used != 500000 {
		t.Errorf("Used = %d, want 500000", ds.Used)
	}
	if ds.Free != 500000 {
		t.Errorf("Free = %d, want 500000", ds.Free)
	}
}

func TestUptimeSeconds(t *testing.T) {
	sm := NewSystemMetrics()
	time.Sleep(10 * time.Millisecond)

	uptime := sm.UptimeSeconds()
	if uptime < 0.005 {
		t.Errorf("UptimeSeconds = %f, want >= 0.005", uptime)
	}
}

func TestPeerCount_Default(t *testing.T) {
	sm := NewSystemMetrics()
	if sm.PeerCount() != 0 {
		t.Errorf("default PeerCount = %d, want 0", sm.PeerCount())
	}
}

func TestPeerCount_Custom(t *testing.T) {
	sm := NewSystemMetrics()
	sm.SetPeerCountFunc(func() int { return 25 })

	if sm.PeerCount() != 25 {
		t.Errorf("PeerCount = %d, want 25", sm.PeerCount())
	}
}

func TestBlockHeight_Default(t *testing.T) {
	sm := NewSystemMetrics()
	if sm.BlockHeight() != 0 {
		t.Errorf("default BlockHeight = %d, want 0", sm.BlockHeight())
	}
}

func TestBlockHeight_Custom(t *testing.T) {
	sm := NewSystemMetrics()
	sm.SetBlockHeightFunc(func() uint64 { return 12345 })

	if sm.BlockHeight() != 12345 {
		t.Errorf("BlockHeight = %d, want 12345", sm.BlockHeight())
	}
}

func TestChainSyncProgress_Default(t *testing.T) {
	sm := NewSystemMetrics()
	if sm.ChainSyncProgress() != 0.0 {
		t.Errorf("default ChainSyncProgress = %f, want 0.0", sm.ChainSyncProgress())
	}
}

func TestChainSyncProgress_Custom(t *testing.T) {
	sm := NewSystemMetrics()
	sm.SetSyncProgressFunc(func() float64 { return 0.75 })

	if sm.ChainSyncProgress() != 0.75 {
		t.Errorf("ChainSyncProgress = %f, want 0.75", sm.ChainSyncProgress())
	}
}

func TestChainSyncProgress_Clamped(t *testing.T) {
	sm := NewSystemMetrics()

	// Test clamping above 1.0.
	sm.SetSyncProgressFunc(func() float64 { return 1.5 })
	if sm.ChainSyncProgress() != 1.0 {
		t.Errorf("ChainSyncProgress (>1) = %f, want 1.0", sm.ChainSyncProgress())
	}

	// Test clamping below 0.0.
	sm.SetSyncProgressFunc(func() float64 { return -0.5 })
	if sm.ChainSyncProgress() != 0.0 {
		t.Errorf("ChainSyncProgress (<0) = %f, want 0.0", sm.ChainSyncProgress())
	}
}

func TestExportJSON(t *testing.T) {
	sm := NewSystemMetrics()
	sm.SetPeerCountFunc(func() int { return 10 })
	sm.SetBlockHeightFunc(func() uint64 { return 42 })
	sm.SetSyncProgressFunc(func() float64 { return 0.99 })

	data, err := sm.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}

	// Verify top-level fields exist.
	requiredFields := []string{
		"goroutines", "memory", "uptimeSeconds",
		"peerCount", "blockHeight", "syncProgress", "collectedAt",
	}
	for _, field := range requiredFields {
		if _, ok := result[field]; !ok {
			t.Errorf("missing field: %q", field)
		}
	}

	// Verify peer count.
	if pc, ok := result["peerCount"].(float64); !ok || int(pc) != 10 {
		t.Errorf("peerCount = %v, want 10", result["peerCount"])
	}

	// Verify block height.
	if bh, ok := result["blockHeight"].(float64); !ok || uint64(bh) != 42 {
		t.Errorf("blockHeight = %v, want 42", result["blockHeight"])
	}

	// Verify sync progress.
	if sp, ok := result["syncProgress"].(float64); !ok || sp != 0.99 {
		t.Errorf("syncProgress = %v, want 0.99", result["syncProgress"])
	}

	// Verify memory sub-fields.
	memMap, ok := result["memory"].(map[string]interface{})
	if !ok {
		t.Fatal("memory field is not a map")
	}
	memFields := []string{"heapAlloc", "totalAlloc", "sys", "numGC"}
	for _, field := range memFields {
		if _, ok := memMap[field]; !ok {
			t.Errorf("missing memory field: %q", field)
		}
	}
}

func TestSetNilFuncsIgnored(t *testing.T) {
	sm := NewSystemMetrics()
	sm.SetPeerCountFunc(func() int { return 5 })

	// Setting nil should not override the existing function.
	sm.SetPeerCountFunc(nil)
	if sm.PeerCount() != 5 {
		t.Errorf("PeerCount after nil set = %d, want 5", sm.PeerCount())
	}

	sm.SetBlockHeightFunc(nil)
	sm.SetSyncProgressFunc(nil)
	sm.SetDiskUsageFunc(nil)
}

func TestGoVersion(t *testing.T) {
	v := GoVersion()
	if v == "" {
		t.Error("GoVersion returned empty string")
	}
}

func TestNumCPU(t *testing.T) {
	n := NumCPU()
	if n <= 0 {
		t.Errorf("NumCPU = %d, want > 0", n)
	}
}

func TestGOARCH(t *testing.T) {
	arch := GOARCH()
	if arch == "" {
		t.Error("GOARCH returned empty string")
	}
}

func TestGOOS(t *testing.T) {
	os := GOOS()
	if os == "" {
		t.Error("GOOS returned empty string")
	}
}
