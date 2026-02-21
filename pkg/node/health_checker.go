// health_checker.go provides a subsystem health monitoring framework for the
// eth2028 node. It aggregates health checks from registered subsystems and
// produces consolidated health reports.
package node

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// SubsystemChecker is the interface that subsystems implement to report
// their health status.
type SubsystemChecker interface {
	// Check performs a health check and returns the current status.
	Check() *SubsystemHealth
}

// SubsystemHealth describes the health of a single subsystem.
type SubsystemHealth struct {
	// Name is the subsystem identifier.
	Name string

	// Status is one of "healthy", "degraded", or "unhealthy".
	Status string

	// Message is an optional human-readable description of the status.
	Message string

	// LastCheck is the unix timestamp (seconds) of when this check ran.
	LastCheck int64

	// Latency is the time in nanoseconds the health check took to execute.
	Latency int64
}

// HealthReport is the aggregate result of checking all subsystems.
type HealthReport struct {
	// OverallStatus summarises all subsystems. It is "healthy" if all are
	// healthy, "degraded" if any are degraded but none unhealthy, and
	// "unhealthy" if any subsystem is unhealthy.
	OverallStatus string

	// Subsystems contains individual health results.
	Subsystems []*SubsystemHealth

	// CheckedAt is the unix timestamp (seconds) when the report was generated.
	CheckedAt int64

	// NodeUptime is the node uptime in seconds at the time of the report.
	NodeUptime int64
}

// Status constants.
const (
	StatusHealthy   = "healthy"
	StatusDegraded  = "degraded"
	StatusUnhealthy = "unhealthy"
)

// HealthChecker aggregates health from registered subsystem checkers.
// All methods are safe for concurrent use.
type HealthChecker struct {
	mu         sync.RWMutex
	checkers   map[string]SubsystemChecker
	order      []string // insertion order
	startTime  int64    // unix seconds
}

// NewHealthChecker creates a new HealthChecker with no registered subsystems.
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		checkers:  make(map[string]SubsystemChecker),
		startTime: time.Now().Unix(),
	}
}

// RegisterSubsystem registers a named subsystem health checker. If a
// checker with the same name already exists, it is replaced.
func (hc *HealthChecker) RegisterSubsystem(name string, checker SubsystemChecker) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if _, exists := hc.checkers[name]; !exists {
		hc.order = append(hc.order, name)
	}
	hc.checkers[name] = checker
}

// CheckAll runs all registered health checks and returns a consolidated
// HealthReport. Checks are executed sequentially in registration order.
func (hc *HealthChecker) CheckAll() *HealthReport {
	hc.mu.RLock()
	names := make([]string, len(hc.order))
	copy(names, hc.order)
	checkers := make(map[string]SubsystemChecker, len(hc.checkers))
	for k, v := range hc.checkers {
		checkers[k] = v
	}
	startTime := hc.startTime
	hc.mu.RUnlock()

	now := time.Now().Unix()
	report := &HealthReport{
		OverallStatus: StatusHealthy,
		CheckedAt:     now,
		NodeUptime:    now - startTime,
	}

	for _, name := range names {
		checker := checkers[name]
		start := time.Now()
		health := checker.Check()
		latency := time.Since(start).Nanoseconds()

		if health == nil {
			health = &SubsystemHealth{
				Name:   name,
				Status: StatusUnhealthy,
			}
		}
		health.Name = name
		health.LastCheck = now
		health.Latency = latency

		report.Subsystems = append(report.Subsystems, health)

		// Update overall status.
		switch health.Status {
		case StatusUnhealthy:
			report.OverallStatus = StatusUnhealthy
		case StatusDegraded:
			if report.OverallStatus != StatusUnhealthy {
				report.OverallStatus = StatusDegraded
			}
		}
	}

	return report
}

// CheckSubsystem runs the health check for a single named subsystem.
// Returns an error if the subsystem is not registered.
func (hc *HealthChecker) CheckSubsystem(name string) (*SubsystemHealth, error) {
	hc.mu.RLock()
	checker, ok := hc.checkers[name]
	hc.mu.RUnlock()

	if !ok {
		return nil, errors.New("subsystem not found: " + name)
	}

	start := time.Now()
	health := checker.Check()
	latency := time.Since(start).Nanoseconds()

	if health == nil {
		health = &SubsystemHealth{
			Name:   name,
			Status: StatusUnhealthy,
		}
	}
	health.Name = name
	health.LastCheck = time.Now().Unix()
	health.Latency = latency

	return health, nil
}

// IsHealthy returns true if all registered subsystems report a healthy
// status. Returns true if no subsystems are registered.
func (hc *HealthChecker) IsHealthy() bool {
	report := hc.CheckAll()
	return report.OverallStatus == StatusHealthy
}

// RegisteredSubsystems returns the names of all registered subsystems
// in registration order.
func (hc *HealthChecker) RegisteredSubsystems() []string {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make([]string, len(hc.order))
	copy(result, hc.order)
	return result
}

// SetStartTime records the node's start time (unix seconds) for uptime
// calculation.
func (hc *HealthChecker) SetStartTime(t int64) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.startTime = t
}

// Uptime returns the node uptime in seconds since the configured start time.
func (hc *HealthChecker) Uptime() int64 {
	hc.mu.RLock()
	startTime := hc.startTime
	hc.mu.RUnlock()

	return time.Now().Unix() - startTime
}

// SubsystemCount returns the number of registered subsystems.
func (hc *HealthChecker) SubsystemCount() int {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return len(hc.checkers)
}

// SortedSubsystems returns the names of all registered subsystems
// sorted alphabetically.
func (hc *HealthChecker) SortedSubsystems() []string {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	names := make([]string, len(hc.order))
	copy(names, hc.order)
	sort.Strings(names)
	return names
}
