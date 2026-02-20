package rpc

import (
	"strings"
	"sync"
	"testing"
)

func TestLifecycleStateString(t *testing.T) {
	tests := []struct {
		state LifecycleState
		want  string
	}{
		{LCStateIdle, "idle"},
		{LCStateStarting, "starting"},
		{LCStateRunning, "running"},
		{LCStateStopping, "stopping"},
		{LCStateStopped, "stopped"},
		{LifecycleState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("LifecycleState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestDefaultLifecycleManagerConfig(t *testing.T) {
	cfg := DefaultLifecycleManagerConfig()
	if cfg.MaxEvents != 1000 {
		t.Errorf("MaxEvents = %d, want 1000", cfg.MaxEvents)
	}
}

func TestNewLifecycleManager_Defaults(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	if lm == nil {
		t.Fatal("nil")
	}
	if lm.EndpointCount() != 0 || lm.RunningCount() != 0 {
		t.Error("should start empty")
	}

	// Zero/negative MaxEvents gets default.
	for _, v := range []int{0, -5} {
		lm2 := NewLifecycleManager(LifecycleManagerConfig{MaxEvents: v})
		if lm2.maxEvents != 1000 {
			t.Errorf("maxEvents(%d) = %d, want 1000", v, lm2.maxEvents)
		}
	}
}

func TestLCRegisterEndpoint(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	ep := RPCEndpoint{Name: "eth", Enabled: true, Namespace: "eth", Version: "1.0"}

	if err := lm.RegisterEndpoint(ep); err != nil {
		t.Fatalf("register: %v", err)
	}
	if lm.EndpointCount() != 1 {
		t.Errorf("count = %d, want 1", lm.EndpointCount())
	}
	if lm.GetState("eth") != LCStateIdle {
		t.Errorf("state = %s, want idle", lm.GetState("eth"))
	}

	// Duplicate.
	if err := lm.RegisterEndpoint(ep); err != errLCEndpointExists {
		t.Errorf("duplicate: err = %v, want errLCEndpointExists", err)
	}
	// Empty name.
	if err := lm.RegisterEndpoint(RPCEndpoint{}); err != errLCEmptyName {
		t.Errorf("empty: err = %v, want errLCEmptyName", err)
	}
}

func TestLCStartEndpoint(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "eth", Enabled: true, Namespace: "eth", Version: "1.0"})

	if err := lm.StartEndpoint("eth"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if lm.GetState("eth") != LCStateRunning {
		t.Errorf("state = %s, want running", lm.GetState("eth"))
	}
	if lm.RunningCount() != 1 {
		t.Errorf("running = %d, want 1", lm.RunningCount())
	}

	// Not found.
	if err := lm.StartEndpoint("xxx"); err != errLCEndpointNotFound {
		t.Errorf("not found: err = %v", err)
	}
	// Disabled.
	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "off", Enabled: false, Namespace: "off", Version: "1.0"})
	if err := lm.StartEndpoint("off"); err != errLCEndpointDisabled {
		t.Errorf("disabled: err = %v", err)
	}
	// Already running.
	if err := lm.StartEndpoint("eth"); err == nil || !strings.Contains(err.Error(), "invalid state") {
		t.Errorf("already running: err = %v", err)
	}
}

func TestLCStopEndpoint(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "eth", Enabled: true, Namespace: "eth", Version: "1.0"})
	_ = lm.StartEndpoint("eth")

	if err := lm.StopEndpoint("eth"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if lm.GetState("eth") != LCStateStopped {
		t.Errorf("state = %s, want stopped", lm.GetState("eth"))
	}

	// Not found.
	if err := lm.StopEndpoint("xxx"); err != errLCEndpointNotFound {
		t.Errorf("not found: err = %v", err)
	}
	// Not running (idle).
	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "idle", Enabled: true, Namespace: "idle", Version: "1.0"})
	if err := lm.StopEndpoint("idle"); err == nil || !strings.Contains(err.Error(), "invalid state") {
		t.Errorf("not running: err = %v", err)
	}
}

func TestLCGetState_NotFound(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	if lm.GetState("unknown") != LCStateStopped {
		t.Error("unknown should return stopped")
	}
}

func TestLCListEndpoints(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	if len(lm.ListEndpoints()) != 0 {
		t.Error("should be empty initially")
	}

	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "eth", Enabled: true, Namespace: "eth", Version: "1.0"})
	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "net", Enabled: false, Namespace: "net", Version: "1.0"})

	eps := lm.ListEndpoints()
	if len(eps) != 2 {
		t.Fatalf("len = %d, want 2", len(eps))
	}

	// Snapshot: mutation should not affect manager.
	eps[0].Name = "MUTATED"
	for _, ep := range lm.ListEndpoints() {
		if ep.Name == "MUTATED" {
			t.Error("ListEndpoints should return snapshot")
		}
	}
}

func TestLCEnableEndpoint(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "eth", Enabled: false, Namespace: "eth", Version: "1.0"})

	if err := lm.EnableEndpoint("eth"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// Can now start.
	if err := lm.StartEndpoint("eth"); err != nil {
		t.Errorf("start after enable: %v", err)
	}
	// Not found.
	if err := lm.EnableEndpoint("xxx"); err != errLCEndpointNotFound {
		t.Errorf("not found: err = %v", err)
	}
}

func TestLCDisableEndpoint(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "eth", Enabled: true, Namespace: "eth", Version: "1.0"})

	// Disable idle endpoint.
	if err := lm.DisableEndpoint("eth"); err != nil {
		t.Fatalf("disable: %v", err)
	}

	// Disable running endpoint should stop it.
	_ = lm.EnableEndpoint("eth")
	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "debug", Enabled: true, Namespace: "debug", Version: "1.0"})
	_ = lm.StartEndpoint("debug")
	if err := lm.DisableEndpoint("debug"); err != nil {
		t.Fatalf("disable running: %v", err)
	}
	if lm.GetState("debug") != LCStateStopped {
		t.Errorf("state = %s, want stopped", lm.GetState("debug"))
	}

	// Not found.
	if err := lm.DisableEndpoint("xxx"); err != errLCEndpointNotFound {
		t.Errorf("not found: err = %v", err)
	}
}

func TestLCResetEndpoint(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "eth", Enabled: true, Namespace: "eth", Version: "1.0"})
	_ = lm.StartEndpoint("eth")
	_ = lm.StopEndpoint("eth")

	if err := lm.ResetEndpoint("eth"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if lm.GetState("eth") != LCStateIdle {
		t.Errorf("state = %s, want idle", lm.GetState("eth"))
	}
	// Can restart.
	if err := lm.StartEndpoint("eth"); err != nil {
		t.Errorf("restart: %v", err)
	}

	// Reset not-stopped.
	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "idle", Enabled: true, Namespace: "idle", Version: "1.0"})
	if err := lm.ResetEndpoint("idle"); err == nil || !strings.Contains(err.Error(), "invalid state") {
		t.Errorf("reset idle: err = %v", err)
	}
	// Not found.
	if err := lm.ResetEndpoint("xxx"); err != errLCEndpointNotFound {
		t.Errorf("not found: err = %v", err)
	}
}

func TestLCGetEvents(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	if len(lm.GetEvents()) != 0 {
		t.Error("should start empty")
	}

	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "eth", Enabled: true, Namespace: "eth", Version: "1.0"})
	_ = lm.StartEndpoint("eth")
	_ = lm.StopEndpoint("eth")

	events := lm.GetEvents()
	// Register(1) + Start(2) + Stop(2) = 5
	if len(events) != 5 {
		t.Fatalf("events = %d, want 5", len(events))
	}
	last := events[len(events)-1]
	if last.ToState != LCStateStopped {
		t.Errorf("last ToState = %s, want stopped", last.ToState)
	}
	for i, ev := range events {
		if ev.Timestamp.IsZero() {
			t.Errorf("event[%d] zero timestamp", i)
		}
	}

	// Snapshot.
	events[0].EndpointName = "MUTATED"
	if lm.GetEvents()[0].EndpointName == "MUTATED" {
		t.Error("GetEvents should return copy")
	}
}

func TestLCGetEvents_MaxTrimming(t *testing.T) {
	lm := NewLifecycleManager(LifecycleManagerConfig{MaxEvents: 5})
	for i := 0; i < 10; i++ {
		name := string(rune('a' + i))
		_ = lm.RegisterEndpoint(RPCEndpoint{Name: name, Enabled: true, Namespace: name, Version: "1.0"})
	}
	if len(lm.GetEvents()) > 5 {
		t.Errorf("events = %d, should be <= 5", len(lm.GetEvents()))
	}
}

func TestLCDisableEndpoint_RecordsEvents(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	_ = lm.RegisterEndpoint(RPCEndpoint{Name: "eth", Enabled: true, Namespace: "eth", Version: "1.0"})
	_ = lm.StartEndpoint("eth")

	before := len(lm.GetEvents())
	_ = lm.DisableEndpoint("eth")

	events := lm.GetEvents()
	if len(events) != before+2 {
		t.Errorf("events = %d, want %d", len(events), before+2)
	}
	for _, ev := range events[before:] {
		if ev.Error != "disabled" {
			t.Errorf("error = %q, want 'disabled'", ev.Error)
		}
	}
}

func TestLCFullLifecycle(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	ep := RPCEndpoint{Name: "debug", Enabled: false, Namespace: "debug", Version: "1.0"}
	_ = lm.RegisterEndpoint(ep)

	// Cannot start disabled.
	if err := lm.StartEndpoint("debug"); err != errLCEndpointDisabled {
		t.Errorf("start disabled: %v", err)
	}
	_ = lm.EnableEndpoint("debug")
	_ = lm.StartEndpoint("debug")
	if lm.GetState("debug") != LCStateRunning {
		t.Error("should be running")
	}
	_ = lm.StopEndpoint("debug")
	_ = lm.ResetEndpoint("debug")
	_ = lm.StartEndpoint("debug")
	if lm.GetState("debug") != LCStateRunning {
		t.Error("should be running after restart")
	}
	_ = lm.DisableEndpoint("debug")
	if lm.GetState("debug") != LCStateStopped {
		t.Error("should be stopped after disable")
	}
}

func TestLCConcurrentOperations(t *testing.T) {
	lm := NewLifecycleManager(DefaultLifecycleManagerConfig())
	var wg sync.WaitGroup
	n := 50

	// Concurrent register.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			name := string(rune('A'+idx%26)) + string(rune('0'+idx/26))
			_ = lm.RegisterEndpoint(RPCEndpoint{Name: name, Enabled: true, Namespace: name, Version: "1.0"})
		}(i)
	}
	wg.Wait()

	if lm.EndpointCount() != n {
		t.Errorf("count = %d, want %d", lm.EndpointCount(), n)
	}

	// Concurrent reads.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = lm.ListEndpoints()
			_ = lm.GetEvents()
		}()
	}
	wg.Wait()
}
