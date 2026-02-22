// payload_tracker.go manages in-flight payload build lifecycle for the
// Engine API. It connects payload IDs to their build attributes, tracks
// build status (pending/building/ready/failed), handles concurrent
// forkchoiceUpdated calls that may request builds for the same attributes,
// and integrates with the PayloadCache for completed payload storage.
//
// The PayloadTracker sits between the EngineAPI handler and the
// PayloadBuilder: forkchoiceUpdated registers a build via Track, the
// builder updates status via MarkBuilding/MarkReady/MarkFailed, and
// getPayload reads the completed result.
package engine

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Payload build lifecycle states.
const (
	BuildStatePending  uint8 = iota // Registered but not yet started.
	BuildStateBuilding              // Actively being constructed.
	BuildStateReady                 // Build complete, payload available.
	BuildStateFailed                // Build failed.
	BuildStateExpired               // Evicted by TTL or cache pressure.
)

// BuildStateName returns a human-readable label for a build state.
func BuildStateName(state uint8) string {
	switch state {
	case BuildStatePending:
		return "pending"
	case BuildStateBuilding:
		return "building"
	case BuildStateReady:
		return "ready"
	case BuildStateFailed:
		return "failed"
	case BuildStateExpired:
		return "expired"
	default:
		return fmt.Sprintf("unknown(%d)", state)
	}
}

// Tracker errors.
var (
	ErrPayloadAlreadyTracked = errors.New("payload tracker: payload ID already tracked")
	ErrPayloadNotTracked     = errors.New("payload tracker: payload ID not found")
	ErrPayloadNotReady       = errors.New("payload tracker: payload not yet ready")
	ErrPayloadBuildFailed    = errors.New("payload tracker: build failed")
	ErrTrackerFull           = errors.New("payload tracker: maximum tracked payloads reached")
)

// TrackerConfig configures the PayloadTracker.
type TrackerConfig struct {
	// MaxTracked is the maximum number of simultaneously tracked payloads.
	MaxTracked int
	// BuildTTL is how long a build can stay pending/building before expiry.
	BuildTTL time.Duration
	// CompletedTTL is how long a completed payload is retained.
	CompletedTTL time.Duration
}

// DefaultTrackerConfig returns sensible defaults for the tracker.
func DefaultTrackerConfig() TrackerConfig {
	return TrackerConfig{
		MaxTracked:   64,
		BuildTTL:     30 * time.Second,
		CompletedTTL: 120 * time.Second,
	}
}

// TrackedPayload holds the full lifecycle state for a single payload build.
type TrackedPayload struct {
	ID         PayloadID
	State      uint8
	ParentHash types.Hash
	Attrs      *PayloadAttributesV4
	Result     *BuiltPayload
	Error      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// IsTerminal returns true if the payload is in a terminal state (ready,
// failed, or expired) and will not transition further.
func (tp *TrackedPayload) IsTerminal() bool {
	return tp.State == BuildStateReady ||
		tp.State == BuildStateFailed ||
		tp.State == BuildStateExpired
}

// Age returns the time since the payload was first tracked.
func (tp *TrackedPayload) Age() time.Duration {
	return time.Since(tp.CreatedAt)
}

// PayloadTracker manages the lifecycle of in-flight payload builds.
// It is safe for concurrent use.
type PayloadTracker struct {
	mu      sync.RWMutex
	config  TrackerConfig
	entries map[PayloadID]*TrackedPayload
	// attrIndex maps (parentHash, timestamp) to payload ID for dedup.
	attrIndex map[attrKey]PayloadID
}

// attrKey is used for deduplicating builds with identical attributes.
type attrKey struct {
	ParentHash types.Hash
	Timestamp  uint64
}

// NewPayloadTracker creates a new tracker with the given config.
func NewPayloadTracker(config TrackerConfig) *PayloadTracker {
	if config.MaxTracked <= 0 {
		config.MaxTracked = DefaultTrackerConfig().MaxTracked
	}
	return &PayloadTracker{
		config:    config,
		entries:   make(map[PayloadID]*TrackedPayload),
		attrIndex: make(map[attrKey]PayloadID),
	}
}

// Track registers a new payload build. If an identical build (same parent
// hash and timestamp) already exists and has not failed, the existing
// payload ID is returned instead of creating a duplicate.
func (pt *PayloadTracker) Track(id PayloadID, parentHash types.Hash, attrs *PayloadAttributesV4) (PayloadID, error) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	// Check for an existing build with the same attributes.
	key := attrKey{ParentHash: parentHash, Timestamp: attrs.Timestamp}
	if existingID, ok := pt.attrIndex[key]; ok {
		if existing, found := pt.entries[existingID]; found {
			if existing.State != BuildStateFailed && existing.State != BuildStateExpired {
				return existingID, nil
			}
			// Previous build failed/expired; allow re-tracking.
			pt.removeEntryLocked(existingID)
		}
	}

	// Enforce capacity, evicting expired entries first.
	pt.evictExpiredLocked()
	if len(pt.entries) >= pt.config.MaxTracked {
		pt.evictOldestTerminalLocked()
	}
	if len(pt.entries) >= pt.config.MaxTracked {
		return PayloadID{}, ErrTrackerFull
	}

	now := time.Now()
	pt.entries[id] = &TrackedPayload{
		ID:         id,
		State:      BuildStatePending,
		ParentHash: parentHash,
		Attrs:      attrs,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	pt.attrIndex[key] = id
	return id, nil
}

// MarkBuilding transitions a payload from pending to building.
func (pt *PayloadTracker) MarkBuilding(id PayloadID) error {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	entry, ok := pt.entries[id]
	if !ok {
		return ErrPayloadNotTracked
	}
	if entry.State != BuildStatePending {
		return fmt.Errorf("payload tracker: cannot transition from %s to building",
			BuildStateName(entry.State))
	}
	entry.State = BuildStateBuilding
	entry.UpdatedAt = time.Now()
	return nil
}

// MarkReady transitions a payload to ready and stores the built result.
func (pt *PayloadTracker) MarkReady(id PayloadID, result *BuiltPayload) error {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	entry, ok := pt.entries[id]
	if !ok {
		return ErrPayloadNotTracked
	}
	if entry.State != BuildStatePending && entry.State != BuildStateBuilding {
		return fmt.Errorf("payload tracker: cannot transition from %s to ready",
			BuildStateName(entry.State))
	}
	entry.State = BuildStateReady
	entry.Result = result
	entry.UpdatedAt = time.Now()
	return nil
}

// MarkFailed transitions a payload to failed with an error message.
func (pt *PayloadTracker) MarkFailed(id PayloadID, reason string) error {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	entry, ok := pt.entries[id]
	if !ok {
		return ErrPayloadNotTracked
	}
	entry.State = BuildStateFailed
	entry.Error = reason
	entry.UpdatedAt = time.Now()
	return nil
}

// Get retrieves a tracked payload by ID. Returns a copy of the state.
func (pt *PayloadTracker) Get(id PayloadID) (*TrackedPayload, error) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	entry, ok := pt.entries[id]
	if !ok {
		return nil, ErrPayloadNotTracked
	}
	cp := *entry
	return &cp, nil
}

// GetResult retrieves the built payload result. Returns an error if the
// build is not yet ready or has failed.
func (pt *PayloadTracker) GetResult(id PayloadID) (*BuiltPayload, error) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	entry, ok := pt.entries[id]
	if !ok {
		return nil, ErrPayloadNotTracked
	}
	switch entry.State {
	case BuildStateReady:
		return entry.Result, nil
	case BuildStateFailed:
		return nil, fmt.Errorf("%w: %s", ErrPayloadBuildFailed, entry.Error)
	default:
		return nil, ErrPayloadNotReady
	}
}

// Count returns the number of currently tracked payloads.
func (pt *PayloadTracker) Count() int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return len(pt.entries)
}

// Prune removes all expired entries based on the configured TTLs.
// Returns the number of entries pruned.
func (pt *PayloadTracker) Prune() int {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.evictExpiredLocked()
}

// evictExpiredLocked removes entries that have exceeded their TTL.
// Caller must hold pt.mu write lock. Returns count of evicted entries.
func (pt *PayloadTracker) evictExpiredLocked() int {
	now := time.Now()
	evicted := 0
	for id, entry := range pt.entries {
		var ttl time.Duration
		if entry.State == BuildStateReady {
			ttl = pt.config.CompletedTTL
		} else {
			ttl = pt.config.BuildTTL
		}
		if now.Sub(entry.CreatedAt) > ttl {
			pt.removeEntryLocked(id)
			evicted++
		}
	}
	return evicted
}

// evictOldestTerminalLocked removes the oldest terminal (ready/failed/expired)
// entry to make room for a new one. Caller must hold pt.mu write lock.
func (pt *PayloadTracker) evictOldestTerminalLocked() {
	var oldestID PayloadID
	var oldestTime time.Time
	found := false

	for id, entry := range pt.entries {
		if !entry.IsTerminal() {
			continue
		}
		if !found || entry.CreatedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = entry.CreatedAt
			found = true
		}
	}
	if found {
		pt.removeEntryLocked(oldestID)
	}
}

// removeEntryLocked removes a tracked payload and its attribute index entry.
// Caller must hold pt.mu write lock.
func (pt *PayloadTracker) removeEntryLocked(id PayloadID) {
	entry, ok := pt.entries[id]
	if !ok {
		return
	}
	if entry.Attrs != nil {
		key := attrKey{ParentHash: entry.ParentHash, Timestamp: entry.Attrs.Timestamp}
		if indexed, exists := pt.attrIndex[key]; exists && indexed == id {
			delete(pt.attrIndex, key)
		}
	}
	delete(pt.entries, id)
}
