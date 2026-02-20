package node

import (
	"errors"
	"testing"
	"time"
)

// --- Recovery Policy Tests ---

func TestNewRecoveryPolicy(t *testing.T) {
	rp := NewRecoveryPolicy()
	if rp == nil {
		t.Fatal("NewRecoveryPolicy returned nil")
	}
}

func TestRecoveryPolicyRegister(t *testing.T) {
	rp := NewRecoveryPolicy()
	err := rp.Register("db", DefaultRecoveryConfig())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	state, err := rp.GetState("db")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != RecoveryIdle {
		t.Errorf("state = %v, want idle", state)
	}
}

func TestRecoveryPolicyRegisterClosed(t *testing.T) {
	rp := NewRecoveryPolicy()
	rp.Close()

	err := rp.Register("db", DefaultRecoveryConfig())
	if err != ErrRecoveryPolicyClosed {
		t.Errorf("expected ErrRecoveryPolicyClosed, got %v", err)
	}
}

func TestRecoveryPolicyGetStateUnknown(t *testing.T) {
	rp := NewRecoveryPolicy()
	_, err := rp.GetState("unknown")
	if err != ErrRecoveryServiceUnknown {
		t.Errorf("expected ErrRecoveryServiceUnknown, got %v", err)
	}
}

func TestRecoveryPolicyRecordFailure(t *testing.T) {
	rp := NewRecoveryPolicy()
	rp.Register("db", RecoveryConfig{
		MaxRetries:        3,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        5 * time.Second,
		BackoffMultiplier: 2.0,
	})

	backoff, err := rp.RecordFailure("db", errors.New("connection lost"))
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if backoff != 100*time.Millisecond {
		t.Errorf("backoff = %v, want 100ms", backoff)
	}

	state, _ := rp.GetState("db")
	if state != RecoveryPending {
		t.Errorf("state = %v, want pending", state)
	}

	retries, _ := rp.GetRetries("db")
	if retries != 1 {
		t.Errorf("retries = %d, want 1", retries)
	}
}

func TestRecoveryPolicyRecordFailureUnknown(t *testing.T) {
	rp := NewRecoveryPolicy()
	_, err := rp.RecordFailure("unknown", errors.New("fail"))
	if err != ErrRecoveryServiceUnknown {
		t.Errorf("expected ErrRecoveryServiceUnknown, got %v", err)
	}
}

func TestRecoveryPolicyExponentialBackoff(t *testing.T) {
	rp := NewRecoveryPolicy()
	rp.Register("svc", RecoveryConfig{
		MaxRetries:        5,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        2 * time.Second,
		BackoffMultiplier: 2.0,
	})

	expectedBackoffs := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
	}

	for i, expected := range expectedBackoffs {
		backoff, err := rp.RecordFailure("svc", errors.New("fail"))
		if err != nil {
			t.Fatalf("retry %d: %v", i, err)
		}
		if backoff != expected {
			t.Errorf("retry %d: backoff = %v, want %v", i, backoff, expected)
		}
	}
}

func TestRecoveryPolicyMaxBackoffCap(t *testing.T) {
	rp := NewRecoveryPolicy()
	rp.Register("svc", RecoveryConfig{
		MaxRetries:        10,
		InitialBackoff:    time.Second,
		MaxBackoff:        3 * time.Second,
		BackoffMultiplier: 4.0,
	})

	// First: 1s, second: 4s capped to 3s.
	rp.RecordFailure("svc", errors.New("fail"))
	backoff, _ := rp.RecordFailure("svc", errors.New("fail"))
	if backoff > 3*time.Second {
		t.Errorf("backoff %v exceeds max 3s", backoff)
	}
}

func TestRecoveryPolicyMaxRetriesExceeded(t *testing.T) {
	rp := NewRecoveryPolicy()
	rp.Register("svc", RecoveryConfig{
		MaxRetries:        2,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        time.Second,
		BackoffMultiplier: 2.0,
	})

	rp.RecordFailure("svc", errors.New("fail1"))
	rp.RecordFailure("svc", errors.New("fail2"))

	// Third failure exceeds max retries.
	_, err := rp.RecordFailure("svc", errors.New("fail3"))
	if !errors.Is(err, ErrRecoveryMaxRetries) {
		t.Errorf("expected ErrRecoveryMaxRetries, got %v", err)
	}

	state, _ := rp.GetState("svc")
	if state != RecoveryExhausted {
		t.Errorf("state = %v, want exhausted", state)
	}
}

func TestRecoveryPolicyRecordSuccess(t *testing.T) {
	rp := NewRecoveryPolicy()
	rp.Register("svc", DefaultRecoveryConfig())

	rp.RecordFailure("svc", errors.New("fail"))
	rp.RecordSuccess("svc")

	state, _ := rp.GetState("svc")
	if state != RecoveryIdle {
		t.Errorf("state = %v after success, want idle", state)
	}

	retries, _ := rp.GetRetries("svc")
	if retries != 0 {
		t.Errorf("retries = %d after success, want 0", retries)
	}
}

func TestRecoveryPolicyRecordSuccessUnknown(t *testing.T) {
	rp := NewRecoveryPolicy()
	err := rp.RecordSuccess("unknown")
	if err != ErrRecoveryServiceUnknown {
		t.Errorf("expected ErrRecoveryServiceUnknown, got %v", err)
	}
}

func TestRecoveryPolicyShouldRestart(t *testing.T) {
	rp := NewRecoveryPolicy()
	rp.Register("svc", DefaultRecoveryConfig())

	// Initially should not restart.
	if rp.ShouldRestart("svc") {
		t.Error("should not restart when idle")
	}

	rp.RecordFailure("svc", errors.New("fail"))

	if !rp.ShouldRestart("svc") {
		t.Error("should restart after failure")
	}

	// Unknown service.
	if rp.ShouldRestart("unknown") {
		t.Error("should not restart unknown service")
	}
}

func TestRecoveryStateString(t *testing.T) {
	tests := []struct {
		state RecoveryState
		want  string
	}{
		{RecoveryIdle, "idle"},
		{RecoveryPending, "pending"},
		{RecoveryAttempting, "attempting"},
		{RecoveryExhausted, "exhausted"},
		{RecoveryState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// --- Graceful Shutdown Tests ---

// recoverySvc implements Service for testing.
type recoverySvc struct {
	name    string
	stopErr error
	stopped bool
}

func (s *recoverySvc) Start() error    { return nil }
func (s *recoverySvc) Stop() error     { s.stopped = true; return s.stopErr }
func (s *recoverySvc) Name() string    { return s.name }

func TestGracefulShutdownBasic(t *testing.T) {
	gs := NewGracefulShutdown(5 * time.Second)

	svc1 := &recoverySvc{name: "db"}
	svc2 := &recoverySvc{name: "rpc"}

	gs.RegisterService("db", svc1, nil, true)
	gs.RegisterService("rpc", svc2, []string{"db"}, true)

	if gs.ServiceCount() != 2 {
		t.Errorf("ServiceCount() = %d, want 2", gs.ServiceCount())
	}

	errs := gs.Execute()
	if len(errs) != 0 {
		t.Fatalf("Execute errors: %v", errs)
	}

	if !svc1.stopped || !svc2.stopped {
		t.Error("both services should be stopped")
	}
}

func TestGracefulShutdownNotRunning(t *testing.T) {
	gs := NewGracefulShutdown(5 * time.Second)

	svc := &recoverySvc{name: "idle"}
	gs.RegisterService("idle", svc, nil, false)

	errs := gs.Execute()
	if len(errs) != 0 {
		t.Fatalf("Execute errors: %v", errs)
	}

	if svc.stopped {
		t.Error("non-running service should not be stopped")
	}
}

func TestGracefulShutdownStopError(t *testing.T) {
	gs := NewGracefulShutdown(5 * time.Second)

	svc := &recoverySvc{name: "stubborn", stopErr: errors.New("won't stop")}
	gs.RegisterService("stubborn", svc, nil, true)

	errs := gs.Execute()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestGracefulShutdownEmpty(t *testing.T) {
	gs := NewGracefulShutdown(5 * time.Second)
	errs := gs.Execute()
	if len(errs) != 0 {
		t.Fatalf("Execute on empty should return no errors, got %v", errs)
	}
}

// --- Health Monitor Tests ---

func TestHealthMonitorBasic(t *testing.T) {
	hm := NewHealthMonitor(5 * time.Second)

	hm.Register("db", func() bool { return true })
	hm.Register("rpc", func() bool { return false })

	if hm.Count() != 2 {
		t.Errorf("Count() = %d, want 2", hm.Count())
	}
	if hm.Interval() != 5*time.Second {
		t.Errorf("Interval() = %v, want 5s", hm.Interval())
	}

	results := hm.CheckAll()
	if !results["db"] {
		t.Error("db should be healthy")
	}
	if results["rpc"] {
		t.Error("rpc should be unhealthy")
	}

	if hm.HealthyCount() != 1 {
		t.Errorf("HealthyCount() = %d, want 1", hm.HealthyCount())
	}
}

func TestHealthMonitorIsHealthy(t *testing.T) {
	hm := NewHealthMonitor(time.Second)
	hm.Register("svc", func() bool { return true })
	hm.CheckAll()

	if !hm.IsHealthy("svc") {
		t.Error("svc should be healthy")
	}
	if hm.IsHealthy("unknown") {
		t.Error("unknown service should not be healthy")
	}
}

func TestHealthMonitorDynamic(t *testing.T) {
	counter := 0
	hm := NewHealthMonitor(time.Second)
	hm.Register("svc", func() bool {
		counter++
		return counter <= 2
	})

	// First two checks healthy.
	hm.CheckAll()
	if !hm.IsHealthy("svc") {
		t.Error("first check should be healthy")
	}
	hm.CheckAll()
	if !hm.IsHealthy("svc") {
		t.Error("second check should be healthy")
	}
	// Third check unhealthy.
	hm.CheckAll()
	if hm.IsHealthy("svc") {
		t.Error("third check should be unhealthy")
	}
}

// --- Dependency Graph Tests ---

func TestDependencyGraphBasic(t *testing.T) {
	dg := NewDependencyGraph()
	dg.Add("db", nil)
	dg.Add("chain", []string{"db"})
	dg.Add("rpc", []string{"chain"})

	if dg.Size() != 3 {
		t.Errorf("Size() = %d, want 3", dg.Size())
	}

	deps := dg.Dependencies("rpc")
	if len(deps) != 1 || deps[0] != "chain" {
		t.Errorf("rpc deps = %v, want [chain]", deps)
	}
}

func TestDependencyGraphNoCycle(t *testing.T) {
	dg := NewDependencyGraph()
	dg.Add("a", nil)
	dg.Add("b", []string{"a"})
	dg.Add("c", []string{"b"})

	if dg.HasCycle() {
		t.Error("should not detect cycle in DAG")
	}
}

func TestDependencyGraphWithCycle(t *testing.T) {
	dg := NewDependencyGraph()
	dg.Add("a", []string{"c"})
	dg.Add("b", []string{"a"})
	dg.Add("c", []string{"b"})

	if !dg.HasCycle() {
		t.Error("should detect cycle")
	}
}

func TestDependencyGraphTopologicalOrder(t *testing.T) {
	dg := NewDependencyGraph()
	dg.Add("db", nil)
	dg.Add("chain", []string{"db"})
	dg.Add("rpc", []string{"chain"})

	order := dg.TopologicalOrder()
	if order == nil {
		t.Fatal("TopologicalOrder returned nil (cycle detected)")
	}

	// db must come before chain, chain before rpc.
	indexOf := func(s string) int {
		for i, v := range order {
			if v == s {
				return i
			}
		}
		return -1
	}

	if indexOf("db") > indexOf("chain") {
		t.Error("db should come before chain")
	}
	if indexOf("chain") > indexOf("rpc") {
		t.Error("chain should come before rpc")
	}
}

func TestDependencyGraphTopologicalOrderCycle(t *testing.T) {
	dg := NewDependencyGraph()
	dg.Add("a", []string{"b"})
	dg.Add("b", []string{"a"})

	order := dg.TopologicalOrder()
	if order != nil {
		t.Error("TopologicalOrder should return nil for cyclic graph")
	}
}

func TestDefaultRecoveryConfig(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.InitialBackoff != time.Second {
		t.Errorf("InitialBackoff = %v, want 1s", cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != 30*time.Second {
		t.Errorf("MaxBackoff = %v, want 30s", cfg.MaxBackoff)
	}
	if cfg.BackoffMultiplier != 2.0 {
		t.Errorf("BackoffMultiplier = %f, want 2.0", cfg.BackoffMultiplier)
	}
}
