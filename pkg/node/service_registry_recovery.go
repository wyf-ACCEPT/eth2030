// Service registry recovery and graceful shutdown extensions.
//
// Provides auto-restart with exponential backoff for failed services,
// ordered graceful shutdown with dependency awareness, and health
// monitoring with configurable intervals.
package node

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Recovery errors.
var (
	ErrRecoveryPolicyClosed   = errors.New("recovery: policy is closed")
	ErrRecoveryServiceUnknown = errors.New("recovery: unknown service")
	ErrRecoveryMaxRetries     = errors.New("recovery: max retries exceeded")
	ErrGracefulTimeout        = errors.New("graceful: shutdown timed out")
)

// RecoveryState tracks the recovery status of a service.
type RecoveryState int

const (
	// RecoveryIdle means no recovery action is needed.
	RecoveryIdle RecoveryState = iota
	// RecoveryPending means a restart is scheduled.
	RecoveryPending
	// RecoveryAttempting means a restart is in progress.
	RecoveryAttempting
	// RecoveryExhausted means max retries have been reached.
	RecoveryExhausted
)

// String returns a human-readable recovery state name.
func (s RecoveryState) String() string {
	switch s {
	case RecoveryIdle:
		return "idle"
	case RecoveryPending:
		return "pending"
	case RecoveryAttempting:
		return "attempting"
	case RecoveryExhausted:
		return "exhausted"
	default:
		return "unknown"
	}
}

// RecoveryConfig configures the auto-restart behavior for a service.
type RecoveryConfig struct {
	// MaxRetries is the maximum number of restart attempts. 0 = no restarts.
	MaxRetries int

	// InitialBackoff is the delay before the first restart attempt.
	InitialBackoff time.Duration

	// MaxBackoff caps the exponential backoff duration.
	MaxBackoff time.Duration

	// BackoffMultiplier scales the backoff between retries (typically 2.0).
	BackoffMultiplier float64
}

// DefaultRecoveryConfig returns a sensible default recovery configuration.
func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		MaxRetries:        3,
		InitialBackoff:    time.Second,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2.0,
	}
}

// ServiceRecoveryEntry tracks per-service recovery state.
type ServiceRecoveryEntry struct {
	Name           string
	Config         RecoveryConfig
	State          RecoveryState
	Retries        int
	LastAttempt    time.Time
	LastError      error
	CurrentBackoff time.Duration
}

// NextBackoff computes the next backoff duration using exponential backoff.
func (e *ServiceRecoveryEntry) NextBackoff() time.Duration {
	if e.CurrentBackoff == 0 {
		return e.Config.InitialBackoff
	}
	next := time.Duration(float64(e.CurrentBackoff) * e.Config.BackoffMultiplier)
	if next > e.Config.MaxBackoff {
		next = e.Config.MaxBackoff
	}
	return next
}

// RecoveryPolicy manages auto-restart policies for multiple services.
// It tracks failure counts, computes backoff delays, and determines
// whether a service should be restarted.
type RecoveryPolicy struct {
	mu      sync.Mutex
	entries map[string]*ServiceRecoveryEntry
	closed  bool
}

// NewRecoveryPolicy creates a new recovery policy manager.
func NewRecoveryPolicy() *RecoveryPolicy {
	return &RecoveryPolicy{
		entries: make(map[string]*ServiceRecoveryEntry),
	}
}

// Register adds a service to the recovery policy with the given config.
func (rp *RecoveryPolicy) Register(name string, config RecoveryConfig) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	if rp.closed {
		return ErrRecoveryPolicyClosed
	}

	rp.entries[name] = &ServiceRecoveryEntry{
		Name:   name,
		Config: config,
		State:  RecoveryIdle,
	}
	return nil
}

// RecordFailure records a service failure and updates recovery state.
// Returns the computed backoff duration, or an error if max retries exceeded.
func (rp *RecoveryPolicy) RecordFailure(name string, err error) (time.Duration, error) {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	entry, ok := rp.entries[name]
	if !ok {
		return 0, ErrRecoveryServiceUnknown
	}

	entry.Retries++
	entry.LastError = err
	entry.LastAttempt = time.Now()

	if entry.Retries > entry.Config.MaxRetries {
		entry.State = RecoveryExhausted
		return 0, fmt.Errorf("%w: %s after %d retries", ErrRecoveryMaxRetries, name, entry.Config.MaxRetries)
	}

	backoff := entry.NextBackoff()
	entry.CurrentBackoff = backoff
	entry.State = RecoveryPending
	return backoff, nil
}

// RecordSuccess resets the recovery state for a service after a successful restart.
func (rp *RecoveryPolicy) RecordSuccess(name string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	entry, ok := rp.entries[name]
	if !ok {
		return ErrRecoveryServiceUnknown
	}

	entry.State = RecoveryIdle
	entry.Retries = 0
	entry.CurrentBackoff = 0
	entry.LastError = nil
	return nil
}

// GetState returns the recovery state for a named service.
func (rp *RecoveryPolicy) GetState(name string) (RecoveryState, error) {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	entry, ok := rp.entries[name]
	if !ok {
		return RecoveryIdle, ErrRecoveryServiceUnknown
	}
	return entry.State, nil
}

// GetRetries returns the current retry count for a named service.
func (rp *RecoveryPolicy) GetRetries(name string) (int, error) {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	entry, ok := rp.entries[name]
	if !ok {
		return 0, ErrRecoveryServiceUnknown
	}
	return entry.Retries, nil
}

// ShouldRestart returns true if the service should be restarted.
func (rp *RecoveryPolicy) ShouldRestart(name string) bool {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	entry, ok := rp.entries[name]
	if !ok {
		return false
	}
	return entry.State == RecoveryPending
}

// Close prevents further recovery actions.
func (rp *RecoveryPolicy) Close() {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.closed = true
}

// --- Graceful Shutdown ---

// GracefulShutdown performs an ordered shutdown of services respecting
// reverse dependency order. It stops each service sequentially, skipping
// services not in the running state.
type GracefulShutdown struct {
	mu       sync.Mutex
	services []shutdownEntry
	timeout  time.Duration
}

type shutdownEntry struct {
	name    string
	svc     Service
	deps    []string
	running bool
}

// NewGracefulShutdown creates a new graceful shutdown manager.
func NewGracefulShutdown(timeout time.Duration) *GracefulShutdown {
	return &GracefulShutdown{
		timeout: timeout,
	}
}

// RegisterService adds a service to the shutdown manager.
func (gs *GracefulShutdown) RegisterService(name string, svc Service, deps []string, running bool) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	gs.services = append(gs.services, shutdownEntry{
		name:    name,
		svc:     svc,
		deps:    deps,
		running: running,
	})
}

// ServiceCount returns the number of registered services.
func (gs *GracefulShutdown) ServiceCount() int {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	return len(gs.services)
}

// Execute performs the shutdown in reverse registration order.
// Returns errors for services that failed to stop, or ErrGracefulTimeout
// if the overall timeout is exceeded.
func (gs *GracefulShutdown) Execute() []error {
	gs.mu.Lock()
	entries := make([]shutdownEntry, len(gs.services))
	copy(entries, gs.services)
	gs.mu.Unlock()

	done := make(chan []error, 1)
	go func() {
		var errs []error
		// Stop in reverse order.
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if !e.running {
				continue
			}
			if err := e.svc.Stop(); err != nil {
				errs = append(errs, fmt.Errorf("stop %s: %w", e.name, err))
			}
		}
		done <- errs
	}()

	select {
	case errs := <-done:
		return errs
	case <-time.After(gs.timeout):
		return []error{ErrGracefulTimeout}
	}
}

// --- Health Monitor ---

// HealthMonitor periodically checks service health and reports status.
type HealthMonitor struct {
	mu       sync.Mutex
	checks   map[string]HealthCheckFunc
	results  map[string]bool
	interval time.Duration
}

// HealthCheckFunc is a function that returns true if the service is healthy.
type HealthCheckFunc func() bool

// NewHealthMonitor creates a new health monitor with the given check interval.
func NewHealthMonitor(interval time.Duration) *HealthMonitor {
	return &HealthMonitor{
		checks:   make(map[string]HealthCheckFunc),
		results:  make(map[string]bool),
		interval: interval,
	}
}

// Register adds a health check function for a named service.
func (hm *HealthMonitor) Register(name string, check HealthCheckFunc) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.checks[name] = check
}

// CheckAll runs all registered health checks and updates results.
// Returns a map of service name to health status.
func (hm *HealthMonitor) CheckAll() map[string]bool {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	for name, check := range hm.checks {
		hm.results[name] = check()
	}

	// Return a copy.
	out := make(map[string]bool, len(hm.results))
	for k, v := range hm.results {
		out[k] = v
	}
	return out
}

// IsHealthy returns the last known health status for a service.
func (hm *HealthMonitor) IsHealthy(name string) bool {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	return hm.results[name]
}

// Interval returns the configured check interval.
func (hm *HealthMonitor) Interval() time.Duration {
	return hm.interval
}

// Count returns the number of registered health checks.
func (hm *HealthMonitor) Count() int {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	return len(hm.checks)
}

// HealthyCount returns how many services are currently healthy.
func (hm *HealthMonitor) HealthyCount() int {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	count := 0
	for _, v := range hm.results {
		if v {
			count++
		}
	}
	return count
}

// DependencyGraph tracks service dependencies for ordered operations.
type DependencyGraph struct {
	mu   sync.RWMutex
	deps map[string][]string // service -> list of dependencies
}

// NewDependencyGraph creates a new empty dependency graph.
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		deps: make(map[string][]string),
	}
}

// Add registers a service and its dependencies.
func (dg *DependencyGraph) Add(name string, deps []string) {
	dg.mu.Lock()
	defer dg.mu.Unlock()
	dg.deps[name] = deps
}

// Dependencies returns the direct dependencies of a service.
func (dg *DependencyGraph) Dependencies(name string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()
	return dg.deps[name]
}

// HasCycle detects whether the dependency graph contains a cycle using DFS.
func (dg *DependencyGraph) HasCycle() bool {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	const (
		white = 0 // unvisited
		gray  = 1 // in current DFS path
		black = 2 // fully processed
	)

	color := make(map[string]int)

	var visit func(string) bool
	visit = func(node string) bool {
		color[node] = gray
		for _, dep := range dg.deps[node] {
			switch color[dep] {
			case gray:
				return true // back edge = cycle
			case white:
				if visit(dep) {
					return true
				}
			}
		}
		color[node] = black
		return false
	}

	for name := range dg.deps {
		if color[name] == white {
			if visit(name) {
				return true
			}
		}
	}
	return false
}

// TopologicalOrder returns services in dependency-respecting order using
// Kahn's algorithm. Returns nil if a cycle is detected.
func (dg *DependencyGraph) TopologicalOrder() []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	inDegree := make(map[string]int)
	dependents := make(map[string][]string)

	// Initialize.
	for name, deps := range dg.deps {
		if _, ok := inDegree[name]; !ok {
			inDegree[name] = 0
		}
		for _, dep := range deps {
			inDegree[name]++
			dependents[dep] = append(dependents[dep], name)
			if _, ok := inDegree[dep]; !ok {
				inDegree[dep] = 0
			}
		}
	}

	// Seed with zero-indegree nodes.
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}

	var order []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		order = append(order, node)

		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(order) != len(inDegree) {
		return nil // cycle detected
	}
	return order
}

// Size returns the number of services in the graph.
func (dg *DependencyGraph) Size() int {
	dg.mu.RLock()
	defer dg.mu.RUnlock()
	return len(dg.deps)
}
