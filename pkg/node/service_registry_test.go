package node

import (
	"errors"
	"testing"
)

// registryTestSvc implements Service for service registry testing.
// Uses a different name to avoid conflict with mockService in lifecycle_test.go.
type registryTestSvc struct {
	svcName  string
	wasStart bool
	wasStop  bool
	startErr error
	stopErr  error
}

func (s *registryTestSvc) Start() error {
	if s.startErr != nil {
		return s.startErr
	}
	s.wasStart = true
	return nil
}

func (s *registryTestSvc) Stop() error {
	if s.stopErr != nil {
		return s.stopErr
	}
	s.wasStop = true
	return nil
}

func (s *registryTestSvc) Name() string { return s.svcName }

func TestNewServiceRegistry(t *testing.T) {
	r := NewServiceRegistry(10)
	if r.Count() != 0 {
		t.Errorf("Count() = %d, want 0", r.Count())
	}
}

func TestRegistryRegisterAndGetService(t *testing.T) {
	r := NewServiceRegistry(10)
	svc := &registryTestSvc{svcName: "db"}

	err := r.Register(&ServiceDescriptor{
		Name:     "db",
		Service:  svc,
		Priority: 1,
	})
	if err != nil {
		t.Fatalf("Register error: %v", err)
	}

	desc, err := r.GetService("db")
	if err != nil {
		t.Fatalf("GetService error: %v", err)
	}
	if desc.Name != "db" {
		t.Errorf("Name = %q, want db", desc.Name)
	}
	if desc.state != StateCreated {
		t.Errorf("state = %v, want created", desc.state)
	}
}

func TestRegistryRegisterDuplicate(t *testing.T) {
	r := NewServiceRegistry(10)
	svc := &registryTestSvc{svcName: "p2p"}

	r.Register(&ServiceDescriptor{Name: "p2p", Service: svc, Priority: 1})
	err := r.Register(&ServiceDescriptor{Name: "p2p", Service: svc, Priority: 2})
	if err != ErrServiceExists {
		t.Errorf("expected ErrServiceExists, got %v", err)
	}
}

func TestRegistryRegisterMaxCapacity(t *testing.T) {
	r := NewServiceRegistry(2)
	r.Register(&ServiceDescriptor{Name: "a", Service: &registryTestSvc{svcName: "a"}, Priority: 1})
	r.Register(&ServiceDescriptor{Name: "b", Service: &registryTestSvc{svcName: "b"}, Priority: 2})

	err := r.Register(&ServiceDescriptor{Name: "c", Service: &registryTestSvc{svcName: "c"}, Priority: 3})
	if err != ErrRegistryMaxReached {
		t.Errorf("expected ErrRegistryMaxReached, got %v", err)
	}
}

func TestRegistryGetServiceNotFound(t *testing.T) {
	r := NewServiceRegistry(10)
	_, err := r.GetService("nonexistent")
	if err != ErrServiceNotFound {
		t.Errorf("expected ErrServiceNotFound, got %v", err)
	}
}

func TestRegistryStartAndStop(t *testing.T) {
	r := NewServiceRegistry(10)

	db := &registryTestSvc{svcName: "db"}
	rpcSvc := &registryTestSvc{svcName: "rpc"}

	r.Register(&ServiceDescriptor{Name: "db", Service: db, Priority: 1})
	r.Register(&ServiceDescriptor{Name: "rpc", Service: rpcSvc, Priority: 2})

	errs := r.Start()
	if len(errs) != 0 {
		t.Fatalf("Start errors: %v", errs)
	}

	if !db.wasStart {
		t.Error("db should be started")
	}
	if !rpcSvc.wasStart {
		t.Error("rpc should be started")
	}
	if r.RunningCount() != 2 {
		t.Errorf("RunningCount() = %d, want 2", r.RunningCount())
	}

	errs = r.Stop()
	if len(errs) != 0 {
		t.Fatalf("Stop errors: %v", errs)
	}

	if !db.wasStop {
		t.Error("db should be stopped")
	}
	if !rpcSvc.wasStop {
		t.Error("rpc should be stopped")
	}
}

func TestRegistryStartWithDependencies(t *testing.T) {
	r := NewServiceRegistry(10)

	db := &registryTestSvc{svcName: "db"}
	chain := &registryTestSvc{svcName: "chain"}
	rpcSvc := &registryTestSvc{svcName: "rpc"}

	// chain depends on db, rpc depends on chain.
	r.Register(&ServiceDescriptor{Name: "db", Service: db, Priority: 1})
	r.Register(&ServiceDescriptor{Name: "chain", Service: chain, Priority: 2, Dependencies: []string{"db"}})
	r.Register(&ServiceDescriptor{Name: "rpc", Service: rpcSvc, Priority: 3, Dependencies: []string{"chain"}})

	errs := r.Start()
	if len(errs) != 0 {
		t.Fatalf("Start errors: %v", errs)
	}

	// All should be started.
	if !db.wasStart || !chain.wasStart || !rpcSvc.wasStart {
		t.Error("all services should be started")
	}
}

func TestRegistryStartFailedDependency(t *testing.T) {
	r := NewServiceRegistry(10)

	db := &registryTestSvc{svcName: "db", startErr: errors.New("db init failed")}
	chain := &registryTestSvc{svcName: "chain"}

	r.Register(&ServiceDescriptor{Name: "db", Service: db, Priority: 1})
	r.Register(&ServiceDescriptor{Name: "chain", Service: chain, Priority: 2, Dependencies: []string{"db"}})

	errs := r.Start()
	// Should get errors for both db (start failed) and chain (dep failed).
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}

	if r.GetState("db") != StateFailed {
		t.Errorf("db state = %v, want failed", r.GetState("db"))
	}
	if r.GetState("chain") != StateFailed {
		t.Errorf("chain state = %v, want failed", r.GetState("chain"))
	}
}

func TestRegistryStartFailure(t *testing.T) {
	r := NewServiceRegistry(10)
	svc := &registryTestSvc{svcName: "failing", startErr: errors.New("boom")}

	r.Register(&ServiceDescriptor{Name: "failing", Service: svc, Priority: 1})

	errs := r.Start()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if r.GetState("failing") != StateFailed {
		t.Errorf("state = %v, want failed", r.GetState("failing"))
	}
}

func TestRegistryStopFailure(t *testing.T) {
	r := NewServiceRegistry(10)
	svc := &registryTestSvc{svcName: "stubborn", stopErr: errors.New("won't stop")}

	r.Register(&ServiceDescriptor{Name: "stubborn", Service: svc, Priority: 1})
	r.Start()

	errs := r.Stop()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if r.GetState("stubborn") != StateFailed {
		t.Errorf("state = %v, want failed", r.GetState("stubborn"))
	}
}

func TestRegistryHealthCheck(t *testing.T) {
	r := NewServiceRegistry(10)

	healthy := &registryTestSvc{svcName: "healthy"}
	unhealthy := &registryTestSvc{svcName: "unhealthy"}

	r.Register(&ServiceDescriptor{Name: "healthy", Service: healthy, Priority: 1})
	r.Register(&ServiceDescriptor{
		Name:     "unhealthy",
		Service:  unhealthy,
		Priority: 2,
		HealthFn: func() bool { return false },
	})

	r.Start()

	health := r.HealthCheck()
	if !health["healthy"] {
		t.Error("healthy service should be healthy")
	}
	if health["unhealthy"] {
		t.Error("unhealthy service should report unhealthy via HealthFn")
	}
}

func TestRegistryHealthCheckCustomFn(t *testing.T) {
	r := NewServiceRegistry(10)

	counter := int32(0)
	svc := &registryTestSvc{svcName: "custom"}

	r.Register(&ServiceDescriptor{
		Name:     "custom",
		Service:  svc,
		Priority: 1,
		HealthFn: func() bool {
			counter++
			return counter < 3
		},
	})

	r.Start()

	// First two calls should be healthy.
	h1 := r.HealthCheck()
	if !h1["custom"] {
		t.Error("first check should be healthy")
	}
	h2 := r.HealthCheck()
	if !h2["custom"] {
		t.Error("second check should be healthy")
	}
	// Third call should be unhealthy.
	h3 := r.HealthCheck()
	if h3["custom"] {
		t.Error("third check should be unhealthy")
	}
}

func TestRegistryNames(t *testing.T) {
	r := NewServiceRegistry(10)
	r.Register(&ServiceDescriptor{Name: "alpha", Service: &registryTestSvc{svcName: "alpha"}, Priority: 1})
	r.Register(&ServiceDescriptor{Name: "beta", Service: &registryTestSvc{svcName: "beta"}, Priority: 2})
	r.Register(&ServiceDescriptor{Name: "gamma", Service: &registryTestSvc{svcName: "gamma"}, Priority: 3})

	names := r.Names()
	if len(names) != 3 {
		t.Fatalf("Names() len = %d, want 3", len(names))
	}
	// Should be in registration order.
	if names[0] != "alpha" || names[1] != "beta" || names[2] != "gamma" {
		t.Errorf("Names() = %v, want [alpha beta gamma]", names)
	}
}

func TestRegistryRegisterAfterStop(t *testing.T) {
	r := NewServiceRegistry(10)
	r.Register(&ServiceDescriptor{Name: "a", Service: &registryTestSvc{svcName: "a"}, Priority: 1})
	r.Start()
	r.Stop()

	err := r.Register(&ServiceDescriptor{Name: "b", Service: &registryTestSvc{svcName: "b"}, Priority: 2})
	if err != ErrRegistryClosed {
		t.Errorf("expected ErrRegistryClosed, got %v", err)
	}
}

func TestRegistryDependencyCycle(t *testing.T) {
	r := NewServiceRegistry(10)

	r.Register(&ServiceDescriptor{Name: "a", Service: &registryTestSvc{svcName: "a"}, Priority: 1, Dependencies: []string{"b"}})
	r.Register(&ServiceDescriptor{Name: "b", Service: &registryTestSvc{svcName: "b"}, Priority: 2, Dependencies: []string{"a"}})

	errs := r.Start()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if !errors.Is(errs[0], ErrDependencyCycle) {
		t.Errorf("expected ErrDependencyCycle, got %v", errs[0])
	}
}

func TestRegistryMissingDependency(t *testing.T) {
	r := NewServiceRegistry(10)

	r.Register(&ServiceDescriptor{Name: "a", Service: &registryTestSvc{svcName: "a"}, Priority: 1, Dependencies: []string{"missing"}})

	errs := r.Start()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if !errors.Is(errs[0], ErrDependencyMissing) {
		t.Errorf("expected ErrDependencyMissing, got %v", errs[0])
	}
}

func TestRegistryGetStateNotFound(t *testing.T) {
	r := NewServiceRegistry(10)
	state := r.GetState("nonexistent")
	if state != StateFailed {
		t.Errorf("GetState for unknown = %v, want StateFailed", state)
	}
}

func TestRegistryStopReverseOrder(t *testing.T) {
	r := NewServiceRegistry(10)

	db := &registryTestSvc{svcName: "db"}
	chain := &registryTestSvc{svcName: "chain"}
	rpcSvc := &registryTestSvc{svcName: "rpc"}

	r.Register(&ServiceDescriptor{Name: "db", Service: db, Priority: 1})
	r.Register(&ServiceDescriptor{Name: "chain", Service: chain, Priority: 2, Dependencies: []string{"db"}})
	r.Register(&ServiceDescriptor{Name: "rpc", Service: rpcSvc, Priority: 3, Dependencies: []string{"chain"}})

	r.Start()
	r.Stop()

	// After stop, verify all are stopped.
	for _, name := range []string{"db", "chain", "rpc"} {
		state := r.GetState(name)
		if state != StateStopped {
			t.Errorf("%s state = %v, want stopped", name, state)
		}
	}
}

func TestRegistryUnlimitedCapacity(t *testing.T) {
	r := NewServiceRegistry(0) // 0 = unlimited

	for i := 0; i < 100; i++ {
		name := string(rune('A'+i/26)) + string(rune('a'+i%26))
		r.Register(&ServiceDescriptor{
			Name:     name,
			Service:  &registryTestSvc{svcName: name},
			Priority: i,
		})
	}

	if r.Count() != 100 {
		t.Errorf("Count() = %d, want 100", r.Count())
	}
}

func TestRegistryHealthCheckBeforeStart(t *testing.T) {
	r := NewServiceRegistry(10)
	r.Register(&ServiceDescriptor{Name: "svc", Service: &registryTestSvc{svcName: "svc"}, Priority: 1})

	health := r.HealthCheck()
	if health["svc"] {
		t.Error("service should not be healthy before Start()")
	}
}
