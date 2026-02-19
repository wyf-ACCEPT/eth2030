package node

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ServiceState represents the lifecycle state of a service.
type ServiceState int

const (
	StateCreated  ServiceState = iota // registered but not started
	StateStarting                     // start in progress
	StateRunning                      // running normally
	StateStopping                     // stop in progress
	StateStopped                      // stopped cleanly
	StateFailed                       // failed to start or crashed
)

// String returns a human-readable name for the service state.
func (s ServiceState) String() string {
	switch s {
	case StateCreated:
		return "created"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Service is a subsystem that can be started and stopped by the
// lifecycle manager.
type Service interface {
	Start() error
	Stop() error
	Name() string
}

// LifecycleConfig holds configuration for the lifecycle manager.
type LifecycleConfig struct {
	// ShutdownTimeout is the maximum time to wait for all services to stop.
	ShutdownTimeout time.Duration
	// GracePeriod is extra time allowed after shutdown timeout before force kill.
	GracePeriod time.Duration
	// MaxServices is the maximum number of services that can be registered.
	MaxServices int
}

// DefaultLifecycleConfig returns a LifecycleConfig with sensible defaults.
func DefaultLifecycleConfig() LifecycleConfig {
	return LifecycleConfig{
		ShutdownTimeout: 30 * time.Second,
		GracePeriod:     5 * time.Second,
		MaxServices:     32,
	}
}

// ServiceEntry tracks a registered service and its state.
type ServiceEntry struct {
	Svc       Service
	State     ServiceState
	StartedAt time.Time
	Error     error
	Priority  int // lower value = start first
}

// LifecycleManager manages the startup and shutdown of multiple services
// with priority ordering and state tracking.
type LifecycleManager struct {
	mu       sync.Mutex
	config   LifecycleConfig
	services []*ServiceEntry
	byName   map[string]*ServiceEntry
}

// NewLifecycleManager creates a new LifecycleManager with the given configuration.
func NewLifecycleManager(config LifecycleConfig) *LifecycleManager {
	return &LifecycleManager{
		config: config,
		byName: make(map[string]*ServiceEntry),
	}
}

// Register adds a service to the manager. Priority determines start order:
// lower values start first. Returns an error if the maximum number of
// services has been reached or the service name is already registered.
func (lm *LifecycleManager) Register(svc Service, priority int) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if len(lm.services) >= lm.config.MaxServices {
		return errors.New("maximum number of services reached")
	}
	if _, exists := lm.byName[svc.Name()]; exists {
		return fmt.Errorf("service %q already registered", svc.Name())
	}

	entry := &ServiceEntry{
		Svc:      svc,
		State:    StateCreated,
		Priority: priority,
	}
	lm.services = append(lm.services, entry)
	lm.byName[svc.Name()] = entry
	return nil
}

// StartAll starts all registered services in priority order (ascending).
// Returns a slice of errors for services that failed to start.
func (lm *LifecycleManager) StartAll() []error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	ordered := lm.sortedServices()
	var errs []error

	for _, entry := range ordered {
		entry.State = StateStarting
		if err := entry.Svc.Start(); err != nil {
			entry.State = StateFailed
			entry.Error = err
			errs = append(errs, fmt.Errorf("start %s: %w", entry.Svc.Name(), err))
			continue
		}
		entry.State = StateRunning
		entry.StartedAt = time.Now()
	}
	return errs
}

// StopAll stops all running services in reverse priority order (descending).
// Returns a slice of errors for services that failed to stop cleanly.
func (lm *LifecycleManager) StopAll() []error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	ordered := lm.sortedServices()
	var errs []error

	// Stop in reverse priority order.
	for i := len(ordered) - 1; i >= 0; i-- {
		entry := ordered[i]
		if entry.State != StateRunning {
			continue
		}
		entry.State = StateStopping
		if err := entry.Svc.Stop(); err != nil {
			entry.State = StateFailed
			entry.Error = err
			errs = append(errs, fmt.Errorf("stop %s: %w", entry.Svc.Name(), err))
			continue
		}
		entry.State = StateStopped
	}
	return errs
}

// GetState returns the current state of a service by name. Returns
// StateFailed if the service is not found.
func (lm *LifecycleManager) GetState(name string) ServiceState {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	entry, ok := lm.byName[name]
	if !ok {
		return StateFailed
	}
	return entry.State
}

// ServiceCount returns the total number of registered services.
func (lm *LifecycleManager) ServiceCount() int {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return len(lm.services)
}

// RunningCount returns the number of services currently in the running state.
func (lm *LifecycleManager) RunningCount() int {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	count := 0
	for _, entry := range lm.services {
		if entry.State == StateRunning {
			count++
		}
	}
	return count
}

// HealthCheck returns a map of service name to health status.
// A service is healthy if it is in the running state.
func (lm *LifecycleManager) HealthCheck() map[string]bool {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	result := make(map[string]bool, len(lm.services))
	for _, entry := range lm.services {
		result[entry.Svc.Name()] = entry.State == StateRunning
	}
	return result
}

// sortedServices returns a copy of the services slice sorted by priority
// (ascending). Caller must hold lm.mu.
func (lm *LifecycleManager) sortedServices() []*ServiceEntry {
	sorted := make([]*ServiceEntry, len(lm.services))
	copy(sorted, lm.services)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	return sorted
}
