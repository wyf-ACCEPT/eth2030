package rpc

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// LifecycleState represents the current state of an RPC endpoint.
type LifecycleState int

const (
	// LCStateIdle means the endpoint is registered but not started.
	LCStateIdle LifecycleState = iota
	// LCStateStarting means the endpoint is in the process of starting.
	LCStateStarting
	// LCStateRunning means the endpoint is active and serving requests.
	LCStateRunning
	// LCStateStopping means the endpoint is in the process of stopping.
	LCStateStopping
	// LCStateStopped means the endpoint has been stopped.
	LCStateStopped
)

// String returns a human-readable name for the lifecycle state.
func (s LifecycleState) String() string {
	switch s {
	case LCStateIdle:
		return "idle"
	case LCStateStarting:
		return "starting"
	case LCStateRunning:
		return "running"
	case LCStateStopping:
		return "stopping"
	case LCStateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// RPCEndpoint describes a registered RPC endpoint.
type RPCEndpoint struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Namespace string `json:"namespace"`
	Version   string `json:"version"`
}

// LifecycleEvent records a state transition for an endpoint.
type LifecycleEvent struct {
	EndpointName string         `json:"endpointName"`
	FromState    LifecycleState `json:"fromState"`
	ToState      LifecycleState `json:"toState"`
	Timestamp    time.Time      `json:"timestamp"`
	Error        string         `json:"error,omitempty"`
}

// endpointEntry holds runtime state for a registered endpoint.
type endpointEntry struct {
	Endpoint RPCEndpoint
	State    LifecycleState
}

// LifecycleManager manages registration, start, stop, enable, and disable
// of RPC server endpoint components. All operations are thread-safe.
type LifecycleManager struct {
	mu        sync.RWMutex
	endpoints map[string]*endpointEntry
	events    []LifecycleEvent
	maxEvents int
}

// LifecycleManagerConfig configures the lifecycle manager.
type LifecycleManagerConfig struct {
	// MaxEvents is the maximum number of lifecycle events to retain.
	MaxEvents int
}

// DefaultLifecycleManagerConfig returns a config with sensible defaults.
func DefaultLifecycleManagerConfig() LifecycleManagerConfig {
	return LifecycleManagerConfig{
		MaxEvents: 1000,
	}
}

// NewLifecycleManager creates a new LifecycleManager.
func NewLifecycleManager(config LifecycleManagerConfig) *LifecycleManager {
	if config.MaxEvents <= 0 {
		config.MaxEvents = 1000
	}
	return &LifecycleManager{
		endpoints: make(map[string]*endpointEntry),
		maxEvents: config.MaxEvents,
	}
}

// Errors returned by LifecycleManager methods.
var (
	errLCEndpointExists    = errors.New("lifecycle: endpoint already registered")
	errLCEndpointNotFound  = errors.New("lifecycle: endpoint not found")
	errLCEndpointDisabled  = errors.New("lifecycle: endpoint is disabled")
	errLCInvalidTransition = errors.New("lifecycle: invalid state transition")
	errLCEmptyName         = errors.New("lifecycle: endpoint name must not be empty")
)

// RegisterEndpoint registers a new RPC endpoint. The endpoint starts in
// the idle state. Returns an error if the name is empty or already taken.
func (lm *LifecycleManager) RegisterEndpoint(ep RPCEndpoint) error {
	if ep.Name == "" {
		return errLCEmptyName
	}

	lm.mu.Lock()
	defer lm.mu.Unlock()

	if _, exists := lm.endpoints[ep.Name]; exists {
		return errLCEndpointExists
	}

	lm.endpoints[ep.Name] = &endpointEntry{
		Endpoint: ep,
		State:    LCStateIdle,
	}

	lm.recordEvent(ep.Name, LCStateIdle, LCStateIdle, "")
	return nil
}

// StartEndpoint transitions an endpoint from idle to running. Returns an
// error if the endpoint is not found, disabled, or not in the idle state.
func (lm *LifecycleManager) StartEndpoint(name string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	entry, ok := lm.endpoints[name]
	if !ok {
		return errLCEndpointNotFound
	}
	if !entry.Endpoint.Enabled {
		return errLCEndpointDisabled
	}
	if entry.State != LCStateIdle {
		return fmt.Errorf("%w: cannot start from state %s", errLCInvalidTransition, entry.State)
	}

	prev := entry.State
	entry.State = LCStateStarting
	lm.recordEvent(name, prev, LCStateStarting, "")

	entry.State = LCStateRunning
	lm.recordEvent(name, LCStateStarting, LCStateRunning, "")

	return nil
}

// StopEndpoint transitions an endpoint from running to stopped. Returns an
// error if the endpoint is not found or not in the running state.
func (lm *LifecycleManager) StopEndpoint(name string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	entry, ok := lm.endpoints[name]
	if !ok {
		return errLCEndpointNotFound
	}
	if entry.State != LCStateRunning {
		return fmt.Errorf("%w: cannot stop from state %s", errLCInvalidTransition, entry.State)
	}

	prev := entry.State
	entry.State = LCStateStopping
	lm.recordEvent(name, prev, LCStateStopping, "")

	entry.State = LCStateStopped
	lm.recordEvent(name, LCStateStopping, LCStateStopped, "")

	return nil
}

// GetState returns the current lifecycle state of an endpoint.
// Returns LCStateStopped if the endpoint is not found.
func (lm *LifecycleManager) GetState(name string) LifecycleState {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	entry, ok := lm.endpoints[name]
	if !ok {
		return LCStateStopped
	}
	return entry.State
}

// ListEndpoints returns a snapshot of all registered endpoints.
func (lm *LifecycleManager) ListEndpoints() []RPCEndpoint {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	result := make([]RPCEndpoint, 0, len(lm.endpoints))
	for _, entry := range lm.endpoints {
		result = append(result, entry.Endpoint)
	}
	return result
}

// EnableEndpoint sets the Enabled flag on an endpoint to true.
// Returns an error if the endpoint is not found.
func (lm *LifecycleManager) EnableEndpoint(name string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	entry, ok := lm.endpoints[name]
	if !ok {
		return errLCEndpointNotFound
	}
	entry.Endpoint.Enabled = true
	return nil
}

// DisableEndpoint sets the Enabled flag on an endpoint to false.
// If the endpoint is currently running, it is stopped first.
// Returns an error if the endpoint is not found.
func (lm *LifecycleManager) DisableEndpoint(name string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	entry, ok := lm.endpoints[name]
	if !ok {
		return errLCEndpointNotFound
	}

	// If running, stop it first.
	if entry.State == LCStateRunning {
		entry.State = LCStateStopping
		lm.recordEvent(name, LCStateRunning, LCStateStopping, "disabled")
		entry.State = LCStateStopped
		lm.recordEvent(name, LCStateStopping, LCStateStopped, "disabled")
	}

	entry.Endpoint.Enabled = false
	return nil
}

// GetEvents returns a copy of the event history.
func (lm *LifecycleManager) GetEvents() []LifecycleEvent {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	cp := make([]LifecycleEvent, len(lm.events))
	copy(cp, lm.events)
	return cp
}

// EndpointCount returns the number of registered endpoints.
func (lm *LifecycleManager) EndpointCount() int {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return len(lm.endpoints)
}

// RunningCount returns the number of endpoints currently in the running state.
func (lm *LifecycleManager) RunningCount() int {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	count := 0
	for _, entry := range lm.endpoints {
		if entry.State == LCStateRunning {
			count++
		}
	}
	return count
}

// ResetEndpoint transitions a stopped endpoint back to idle so it can be
// started again. Returns an error if the endpoint is not found or not
// in the stopped state.
func (lm *LifecycleManager) ResetEndpoint(name string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	entry, ok := lm.endpoints[name]
	if !ok {
		return errLCEndpointNotFound
	}
	if entry.State != LCStateStopped {
		return fmt.Errorf("%w: cannot reset from state %s", errLCInvalidTransition, entry.State)
	}

	prev := entry.State
	entry.State = LCStateIdle
	lm.recordEvent(name, prev, LCStateIdle, "reset")
	return nil
}

// recordEvent appends a lifecycle event. Caller must hold lm.mu.
func (lm *LifecycleManager) recordEvent(name string, from, to LifecycleState, errMsg string) {
	ev := LifecycleEvent{
		EndpointName: name,
		FromState:    from,
		ToState:      to,
		Timestamp:    time.Now(),
		Error:        errMsg,
	}
	lm.events = append(lm.events, ev)

	// Trim events if over capacity.
	if len(lm.events) > lm.maxEvents {
		excess := len(lm.events) - lm.maxEvents
		lm.events = lm.events[excess:]
	}
}
