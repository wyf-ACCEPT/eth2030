package node

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// mockChecker is a test double for SubsystemChecker.
type mockChecker struct {
	status  string
	message string
}

func (mc *mockChecker) Check() *SubsystemHealth {
	return &SubsystemHealth{
		Status:  mc.status,
		Message: mc.message,
	}
}

func TestHealthCheckerNew(t *testing.T) {
	hc := NewHealthChecker()
	if hc == nil {
		t.Fatal("NewHealthChecker returned nil")
	}
	if hc.SubsystemCount() != 0 {
		t.Errorf("expected 0 subsystems, got %d", hc.SubsystemCount())
	}
}

func TestHealthCheckerRegisterSubsystem(t *testing.T) {
	hc := NewHealthChecker()
	hc.RegisterSubsystem("blockchain", &mockChecker{status: StatusHealthy})

	if hc.SubsystemCount() != 1 {
		t.Errorf("expected 1 subsystem, got %d", hc.SubsystemCount())
	}

	subs := hc.RegisteredSubsystems()
	if len(subs) != 1 || subs[0] != "blockchain" {
		t.Errorf("unexpected subsystems: %v", subs)
	}
}

func TestHealthCheckerRegisterReplace(t *testing.T) {
	hc := NewHealthChecker()
	hc.RegisterSubsystem("p2p", &mockChecker{status: StatusHealthy, message: "v1"})
	hc.RegisterSubsystem("p2p", &mockChecker{status: StatusDegraded, message: "v2"})

	if hc.SubsystemCount() != 1 {
		t.Errorf("expected 1 subsystem after replace, got %d", hc.SubsystemCount())
	}

	health, err := hc.CheckSubsystem("p2p")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != StatusDegraded {
		t.Errorf("expected degraded status after replace, got %s", health.Status)
	}
}

func TestHealthCheckerCheckAll(t *testing.T) {
	hc := NewHealthChecker()
	hc.RegisterSubsystem("blockchain", &mockChecker{status: StatusHealthy})
	hc.RegisterSubsystem("txpool", &mockChecker{status: StatusHealthy})

	report := hc.CheckAll()
	if report.OverallStatus != StatusHealthy {
		t.Errorf("expected healthy overall, got %s", report.OverallStatus)
	}
	if len(report.Subsystems) != 2 {
		t.Errorf("expected 2 subsystem results, got %d", len(report.Subsystems))
	}
	if report.CheckedAt == 0 {
		t.Error("expected non-zero CheckedAt")
	}
}

func TestHealthCheckerCheckSubsystem(t *testing.T) {
	hc := NewHealthChecker()
	hc.RegisterSubsystem("p2p", &mockChecker{
		status:  StatusHealthy,
		message: "50 peers connected",
	})

	health, err := hc.CheckSubsystem("p2p")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Name != "p2p" {
		t.Errorf("expected name=p2p, got %s", health.Name)
	}
	if health.Status != StatusHealthy {
		t.Errorf("expected healthy, got %s", health.Status)
	}
	if health.Message != "50 peers connected" {
		t.Errorf("expected message='50 peers connected', got %q", health.Message)
	}
	if health.LastCheck == 0 {
		t.Error("expected non-zero LastCheck")
	}
	if health.Latency < 0 {
		t.Error("expected non-negative latency")
	}
}

func TestHealthCheckerIsHealthy(t *testing.T) {
	hc := NewHealthChecker()

	// Empty checker is healthy.
	if !hc.IsHealthy() {
		t.Error("empty health checker should be healthy")
	}

	hc.RegisterSubsystem("db", &mockChecker{status: StatusHealthy})
	if !hc.IsHealthy() {
		t.Error("all-healthy subsystems should make IsHealthy true")
	}
}

func TestHealthCheckerDegraded(t *testing.T) {
	hc := NewHealthChecker()
	hc.RegisterSubsystem("blockchain", &mockChecker{status: StatusHealthy})
	hc.RegisterSubsystem("p2p", &mockChecker{
		status:  StatusDegraded,
		message: "low peer count",
	})

	report := hc.CheckAll()
	if report.OverallStatus != StatusDegraded {
		t.Errorf("expected degraded overall, got %s", report.OverallStatus)
	}
	if hc.IsHealthy() {
		t.Error("should not be healthy when degraded")
	}
}

func TestHealthCheckerUnhealthy(t *testing.T) {
	hc := NewHealthChecker()
	hc.RegisterSubsystem("blockchain", &mockChecker{status: StatusHealthy})
	hc.RegisterSubsystem("db", &mockChecker{
		status:  StatusUnhealthy,
		message: "disk full",
	})

	report := hc.CheckAll()
	if report.OverallStatus != StatusUnhealthy {
		t.Errorf("expected unhealthy overall, got %s", report.OverallStatus)
	}
}

func TestHealthCheckerUnhealthyOverridesDegraded(t *testing.T) {
	hc := NewHealthChecker()
	hc.RegisterSubsystem("a", &mockChecker{status: StatusDegraded})
	hc.RegisterSubsystem("b", &mockChecker{status: StatusUnhealthy})

	report := hc.CheckAll()
	if report.OverallStatus != StatusUnhealthy {
		t.Errorf("unhealthy should override degraded, got %s", report.OverallStatus)
	}
}

func TestHealthCheckerUptime(t *testing.T) {
	hc := NewHealthChecker()
	hc.SetStartTime(time.Now().Unix() - 100)

	uptime := hc.Uptime()
	if uptime < 99 || uptime > 102 {
		t.Errorf("expected uptime ~100s, got %d", uptime)
	}
}

func TestHealthCheckerRegisteredSubsystems(t *testing.T) {
	hc := NewHealthChecker()
	hc.RegisterSubsystem("c_engine", &mockChecker{status: StatusHealthy})
	hc.RegisterSubsystem("a_blockchain", &mockChecker{status: StatusHealthy})
	hc.RegisterSubsystem("b_txpool", &mockChecker{status: StatusHealthy})

	subs := hc.RegisteredSubsystems()
	if len(subs) != 3 {
		t.Fatalf("expected 3 subsystems, got %d", len(subs))
	}
	// Should be in registration order.
	if subs[0] != "c_engine" || subs[1] != "a_blockchain" || subs[2] != "b_txpool" {
		t.Errorf("unexpected order: %v", subs)
	}
}

func TestHealthCheckerSortedSubsystems(t *testing.T) {
	hc := NewHealthChecker()
	hc.RegisterSubsystem("c_engine", &mockChecker{status: StatusHealthy})
	hc.RegisterSubsystem("a_blockchain", &mockChecker{status: StatusHealthy})
	hc.RegisterSubsystem("b_txpool", &mockChecker{status: StatusHealthy})

	sorted := hc.SortedSubsystems()
	if sorted[0] != "a_blockchain" || sorted[1] != "b_txpool" || sorted[2] != "c_engine" {
		t.Errorf("expected alphabetical order, got %v", sorted)
	}
}

func TestHealthCheckerUnknownSubsystem(t *testing.T) {
	hc := NewHealthChecker()

	_, err := hc.CheckSubsystem("nonexistent")
	if err == nil {
		t.Error("expected error for unknown subsystem")
	}
}

func TestHealthCheckerMultipleSubsystems(t *testing.T) {
	hc := NewHealthChecker()
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("subsystem_%d", i)
		hc.RegisterSubsystem(name, &mockChecker{status: StatusHealthy})
	}

	if hc.SubsystemCount() != 10 {
		t.Errorf("expected 10 subsystems, got %d", hc.SubsystemCount())
	}

	report := hc.CheckAll()
	if len(report.Subsystems) != 10 {
		t.Errorf("expected 10 results, got %d", len(report.Subsystems))
	}
	if report.OverallStatus != StatusHealthy {
		t.Errorf("expected healthy, got %s", report.OverallStatus)
	}
}

func TestHealthCheckerConcurrentChecks(t *testing.T) {
	hc := NewHealthChecker()
	hc.RegisterSubsystem("blockchain", &mockChecker{status: StatusHealthy})
	hc.RegisterSubsystem("p2p", &mockChecker{status: StatusHealthy})
	hc.RegisterSubsystem("txpool", &mockChecker{status: StatusHealthy})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report := hc.CheckAll()
			if report == nil {
				t.Error("expected non-nil report")
			}
		}()
	}
	wg.Wait()
}

func TestHealthCheckerEmptyChecker(t *testing.T) {
	hc := NewHealthChecker()

	report := hc.CheckAll()
	if report.OverallStatus != StatusHealthy {
		t.Errorf("empty checker should report healthy, got %s", report.OverallStatus)
	}
	if len(report.Subsystems) != 0 {
		t.Errorf("expected 0 subsystems, got %d", len(report.Subsystems))
	}
	if report.CheckedAt == 0 {
		t.Error("expected non-zero CheckedAt")
	}
}

func TestHealthCheckerHealthReport(t *testing.T) {
	hc := NewHealthChecker()
	hc.SetStartTime(time.Now().Unix() - 300)
	hc.RegisterSubsystem("blockchain", &mockChecker{status: StatusHealthy, message: "synced"})

	report := hc.CheckAll()
	if report.NodeUptime < 299 || report.NodeUptime > 302 {
		t.Errorf("expected uptime ~300s, got %d", report.NodeUptime)
	}

	if len(report.Subsystems) != 1 {
		t.Fatalf("expected 1 subsystem, got %d", len(report.Subsystems))
	}
	sub := report.Subsystems[0]
	if sub.Name != "blockchain" {
		t.Errorf("expected name=blockchain, got %s", sub.Name)
	}
	if sub.Status != StatusHealthy {
		t.Errorf("expected healthy, got %s", sub.Status)
	}
	if sub.Message != "synced" {
		t.Errorf("expected message='synced', got %q", sub.Message)
	}
}

func TestHealthCheckerSetStartTime(t *testing.T) {
	hc := NewHealthChecker()

	past := time.Now().Unix() - 600
	hc.SetStartTime(past)

	uptime := hc.Uptime()
	if uptime < 599 || uptime > 602 {
		t.Errorf("expected uptime ~600s, got %d", uptime)
	}
}

func TestHealthCheckerConcurrentRegisterAndCheck(t *testing.T) {
	hc := NewHealthChecker()

	var wg sync.WaitGroup

	// Register concurrently.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("svc_%d", id)
			hc.RegisterSubsystem(name, &mockChecker{status: StatusHealthy})
		}(i)
	}

	// Check concurrently.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hc.CheckAll()
			hc.IsHealthy()
			hc.Uptime()
		}()
	}

	wg.Wait()

	if hc.SubsystemCount() != 10 {
		t.Errorf("expected 10 subsystems, got %d", hc.SubsystemCount())
	}
}
