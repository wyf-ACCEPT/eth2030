package node

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ServiceRegistry error codes.
var (
	ErrServiceNotFound    = errors.New("service not found")
	ErrServiceExists      = errors.New("service already registered")
	ErrRegistryClosed     = errors.New("registry is closed")
	ErrDependencyMissing  = errors.New("dependency not registered")
	ErrDependencyCycle    = errors.New("dependency cycle detected")
	ErrRegistryMaxReached = errors.New("maximum service count reached")
)

// ServiceDescriptor describes a registered service including its metadata,
// dependencies, start priority, and health check function.
type ServiceDescriptor struct {
	// Name is a unique identifier for the service.
	Name string

	// Service is the underlying service instance.
	Service Service

	// Dependencies lists the names of services that must be started
	// before this service.
	Dependencies []string

	// Priority controls start ordering when there are no dependency
	// constraints. Lower values start first.
	Priority int

	// HealthFn is an optional health check function. When set, it
	// overrides the default state-based health check.
	HealthFn func() bool

	// state tracks the runtime state of this service.
	state     ServiceState
	startedAt time.Time
	stoppedAt time.Time
	lastErr   error
}

// ServiceRegistry is a service container that manages registration, lifecycle
// (start/stop), dependency resolution, and health checking of node subsystems.
// It is safe for concurrent use.
type ServiceRegistry struct {
	mu         sync.RWMutex
	services   []*ServiceDescriptor
	byName     map[string]*ServiceDescriptor
	maxSize    int
	closed     bool
	startOrder []string // computed start ordering
}

// NewServiceRegistry creates a new ServiceRegistry with the given maximum
// capacity. Pass 0 for unlimited services.
func NewServiceRegistry(maxSize int) *ServiceRegistry {
	return &ServiceRegistry{
		byName:  make(map[string]*ServiceDescriptor),
		maxSize: maxSize,
	}
}

// Register adds a service descriptor to the registry. The descriptor's Name
// must be unique. Returns an error if the registry is closed, full, or the
// name is already taken.
func (r *ServiceRegistry) Register(desc *ServiceDescriptor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return ErrRegistryClosed
	}
	if r.maxSize > 0 && len(r.services) >= r.maxSize {
		return ErrRegistryMaxReached
	}
	if _, exists := r.byName[desc.Name]; exists {
		return ErrServiceExists
	}

	// Copy the descriptor to avoid external mutation.
	entry := *desc
	entry.state = StateCreated
	r.services = append(r.services, &entry)
	r.byName[entry.Name] = &entry
	r.startOrder = nil // invalidate cached order
	return nil
}

// Start starts all registered services in dependency-aware, priority-sorted
// order. Services whose dependencies have failed are marked as failed and
// skipped. Returns a slice of errors for services that failed to start.
func (r *ServiceRegistry) Start() []error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return []error{ErrRegistryClosed}
	}

	order, err := r.resolveOrder()
	if err != nil {
		return []error{err}
	}
	r.startOrder = make([]string, len(order))
	for i, desc := range order {
		r.startOrder[i] = desc.Name
	}

	var errs []error
	for _, desc := range order {
		// Check that all dependencies are running.
		depFailed := false
		for _, dep := range desc.Dependencies {
			if d, ok := r.byName[dep]; ok {
				if d.state != StateRunning {
					depFailed = true
					break
				}
			}
		}
		if depFailed {
			desc.state = StateFailed
			desc.lastErr = fmt.Errorf("dependency failed for %s", desc.Name)
			errs = append(errs, desc.lastErr)
			continue
		}

		desc.state = StateStarting
		if err := desc.Service.Start(); err != nil {
			desc.state = StateFailed
			desc.lastErr = err
			errs = append(errs, fmt.Errorf("start %s: %w", desc.Name, err))
			continue
		}
		desc.state = StateRunning
		desc.startedAt = time.Now()
	}
	return errs
}

// Stop stops all running services in reverse start order. Returns a slice
// of errors for services that failed to stop cleanly.
func (r *ServiceRegistry) Stop() []error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true

	// Build the stop order (reverse of start order, or reverse of registration).
	var order []*ServiceDescriptor
	if len(r.startOrder) > 0 {
		for i := len(r.startOrder) - 1; i >= 0; i-- {
			if desc, ok := r.byName[r.startOrder[i]]; ok {
				order = append(order, desc)
			}
		}
	} else {
		for i := len(r.services) - 1; i >= 0; i-- {
			order = append(order, r.services[i])
		}
	}

	var errs []error
	for _, desc := range order {
		if desc.state != StateRunning {
			continue
		}
		desc.state = StateStopping
		if err := desc.Service.Stop(); err != nil {
			desc.state = StateFailed
			desc.lastErr = err
			errs = append(errs, fmt.Errorf("stop %s: %w", desc.Name, err))
			continue
		}
		desc.state = StateStopped
		desc.stoppedAt = time.Now()
	}
	return errs
}

// HealthCheck returns a map of service name to health status. A service is
// healthy if it is running and its optional HealthFn returns true.
func (r *ServiceRegistry) HealthCheck() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]bool, len(r.services))
	for _, desc := range r.services {
		if desc.HealthFn != nil {
			result[desc.Name] = desc.state == StateRunning && desc.HealthFn()
		} else {
			result[desc.Name] = desc.state == StateRunning
		}
	}
	return result
}

// GetService returns the ServiceDescriptor for the given name, or an error
// if the service is not registered.
func (r *ServiceRegistry) GetService(name string) (*ServiceDescriptor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	desc, ok := r.byName[name]
	if !ok {
		return nil, ErrServiceNotFound
	}
	return desc, nil
}

// GetState returns the current state of a named service. Returns StateFailed
// if the service is not found.
func (r *ServiceRegistry) GetState(name string) ServiceState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	desc, ok := r.byName[name]
	if !ok {
		return StateFailed
	}
	return desc.state
}

// Count returns the number of registered services.
func (r *ServiceRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.services)
}

// RunningCount returns the number of services in the running state.
func (r *ServiceRegistry) RunningCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, desc := range r.services {
		if desc.state == StateRunning {
			count++
		}
	}
	return count
}

// Names returns the names of all registered services.
func (r *ServiceRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, len(r.services))
	for i, desc := range r.services {
		names[i] = desc.Name
	}
	return names
}

// resolveOrder computes a dependency-aware start ordering using topological
// sort with priority-based tiebreaking. Caller must hold r.mu.
func (r *ServiceRegistry) resolveOrder() ([]*ServiceDescriptor, error) {
	// Validate all dependencies exist.
	for _, desc := range r.services {
		for _, dep := range desc.Dependencies {
			if _, ok := r.byName[dep]; !ok {
				return nil, fmt.Errorf("%w: %s depends on %s", ErrDependencyMissing, desc.Name, dep)
			}
		}
	}

	// Kahn's algorithm for topological sort.
	inDegree := make(map[string]int, len(r.services))
	dependents := make(map[string][]string, len(r.services))

	for _, desc := range r.services {
		inDegree[desc.Name] = len(desc.Dependencies)
		for _, dep := range desc.Dependencies {
			dependents[dep] = append(dependents[dep], desc.Name)
		}
	}

	// Seed the queue with zero-indegree nodes, sorted by priority.
	var queue []*ServiceDescriptor
	for _, desc := range r.services {
		if inDegree[desc.Name] == 0 {
			queue = append(queue, desc)
		}
	}
	sortByPriority(queue)

	var order []*ServiceDescriptor
	for len(queue) > 0 {
		// Pick the highest-priority (lowest value) node.
		desc := queue[0]
		queue = queue[1:]
		order = append(order, desc)

		for _, depName := range dependents[desc.Name] {
			inDegree[depName]--
			if inDegree[depName] == 0 {
				queue = append(queue, r.byName[depName])
				sortByPriority(queue)
			}
		}
	}

	if len(order) != len(r.services) {
		return nil, ErrDependencyCycle
	}
	return order, nil
}

// sortByPriority sorts descriptors by priority (ascending). Stable sort to
// preserve registration order for equal priorities.
func sortByPriority(descs []*ServiceDescriptor) {
	for i := 1; i < len(descs); i++ {
		for j := i; j > 0 && descs[j].Priority < descs[j-1].Priority; j-- {
			descs[j], descs[j-1] = descs[j-1], descs[j]
		}
	}
}
