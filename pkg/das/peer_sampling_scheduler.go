// peer_sampling_scheduler.go implements a peer-aware PeerDAS sampling scheduler
// that orchestrates data availability sample requests across peers. It tracks
// per-peer latency, custody overlap, and success rate to make intelligent peer
// selection decisions for column sampling, with adaptive retry logic for failed
// samples.
//
// Reference: consensus-specs/specs/fulu/das-core.md
package das

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// Peer sampling scheduler errors.
var (
	ErrPeerSchedClosed      = errors.New("das/peersched: scheduler is closed")
	ErrPeerSchedNoColumns   = errors.New("das/peersched: no columns to sample")
	ErrPeerSchedNoPeers     = errors.New("das/peersched: no peers available")
	ErrPeerSchedSlotUnknown = errors.New("das/peersched: slot not tracked")
)

// DAVerdict indicates the overall data availability verdict for a slot.
type DAVerdict int

const (
	// VerdictPending means sampling is still in progress.
	VerdictPending DAVerdict = iota
	// VerdictAvailable means all required columns are verified.
	VerdictAvailable
	// VerdictUnavailable means too many columns failed to be retrieved.
	VerdictUnavailable
)

// String returns a human-readable verdict name.
func (v DAVerdict) String() string {
	switch v {
	case VerdictPending:
		return "pending"
	case VerdictAvailable:
		return "available"
	case VerdictUnavailable:
		return "unavailable"
	default:
		return "unknown"
	}
}

// PeerSamplingConfig configures the peer-aware sampling scheduler.
type PeerSamplingConfig struct {
	// SamplesPerSlot is the number of column samples to request per slot.
	SamplesPerSlot int
	// CustodyRequirement is the min custody groups an honest node custodies.
	CustodyRequirement int
	// SampleTimeout is the maximum time to wait for a single sample response.
	SampleTimeout time.Duration
	// MaxRetries is the maximum number of retry attempts per column.
	MaxRetries int
	// FailureThreshold is the fraction of columns that can fail before
	// declaring the slot unavailable (e.g. 0.5 means >50% failures = unavailable).
	FailureThreshold float64
}

// DefaultPeerSamplingConfig returns production defaults.
func DefaultPeerSamplingConfig() PeerSamplingConfig {
	return PeerSamplingConfig{
		SamplesPerSlot:     SamplesPerSlot,
		CustodyRequirement: int(CustodyRequirement),
		SampleTimeout:      2 * time.Second,
		MaxRetries:         3,
		FailureThreshold:   0.5,
	}
}

// SamplingPeerInfo holds metadata about a peer for intelligent selection.
type SamplingPeerInfo struct {
	// ID is a unique peer identifier.
	ID string
	// Latency is the peer's observed average response latency.
	Latency time.Duration
	// CustodyColumns is the set of columns this peer custodies.
	CustodyColumns map[uint64]bool
	// SuccessRate is the peer's historical success rate in [0, 1].
	SuccessRate float64
}

// PeerColumnAssignment represents a single column-to-peer assignment.
type PeerColumnAssignment struct {
	// Column is the column index to sample.
	Column uint64
	// Peer is the assigned peer's ID.
	Peer string
	// Priority is the assignment priority (lower = higher priority).
	Priority int
	// Deadline is the absolute deadline for this sample request.
	Deadline time.Time
}

// PeerSamplingPlan describes the full sampling plan for a slot, distributing
// column sample requests across available peers.
type PeerSamplingPlan struct {
	// Slot is the beacon slot being sampled.
	Slot uint64
	// Assignments maps column indices to their peer assignments.
	Assignments []PeerColumnAssignment
	// CreatedAt is when this plan was created.
	CreatedAt time.Time
}

// SlotSamplingStatus reports the sampling progress for a slot.
type SlotSamplingStatus struct {
	// Slot is the beacon slot.
	Slot uint64
	// Completed is the number of columns successfully sampled.
	Completed int
	// Pending is the number of columns still awaiting response.
	Pending int
	// Failed is the number of columns that failed all retry attempts.
	Failed int
	// Total is the total number of columns being sampled.
	Total int
	// Verdict is the overall data availability verdict.
	Verdict DAVerdict
}

// columnState tracks the sampling state of a single column within a slot.
type columnState struct {
	column     uint64
	peer       string
	success    bool
	failed     bool
	retries    int
	latency    time.Duration
	triedPeers map[string]bool
}

// slotState tracks the sampling state of an entire slot.
type slotState struct {
	slot      uint64
	columns   map[uint64]*columnState
	createdAt time.Time
}

// PeerSamplingScheduler orchestrates DAS sampling by selecting optimal peers
// for each column based on latency, custody match, and success rate. It
// supports adaptive retry logic for failed samples. All methods are safe
// for concurrent use.
type PeerSamplingScheduler struct {
	mu     sync.RWMutex
	config PeerSamplingConfig
	closed bool

	// slots tracks per-slot sampling state.
	slots map[uint64]*slotState

	// peerStats tracks accumulated peer performance metrics.
	peerStats map[string]*peerStatEntry
}

// peerStatEntry accumulates a peer's performance over time.
type peerStatEntry struct {
	totalRequests   int
	successRequests int
	totalLatency    time.Duration
}

// NewPeerSamplingScheduler creates a new peer-aware sampling scheduler.
func NewPeerSamplingScheduler(config PeerSamplingConfig) *PeerSamplingScheduler {
	if config.SamplesPerSlot <= 0 {
		config.SamplesPerSlot = SamplesPerSlot
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if config.SampleTimeout <= 0 {
		config.SampleTimeout = 2 * time.Second
	}
	if config.FailureThreshold <= 0 || config.FailureThreshold > 1.0 {
		config.FailureThreshold = 0.5
	}
	return &PeerSamplingScheduler{
		config:    config,
		slots:     make(map[uint64]*slotState),
		peerStats: make(map[string]*peerStatEntry),
	}
}

// ScheduleSampling creates a sampling plan distributing column sample requests
// across the provided peers. It assigns each column to the best available peer
// considering latency, custody match, and success rate.
func (ps *PeerSamplingScheduler) ScheduleSampling(slot uint64, columns []uint64, peers []SamplingPeerInfo) (*PeerSamplingPlan, error) {
	if len(columns) == 0 {
		return nil, ErrPeerSchedNoColumns
	}
	if len(peers) == 0 {
		return nil, ErrPeerSchedNoPeers
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.closed {
		return nil, ErrPeerSchedClosed
	}

	now := time.Now()
	plan := &PeerSamplingPlan{
		Slot:        slot,
		Assignments: make([]PeerColumnAssignment, 0, len(columns)),
		CreatedAt:   now,
	}

	// Initialize slot state.
	ss := &slotState{
		slot:      slot,
		columns:   make(map[uint64]*columnState, len(columns)),
		createdAt: now,
	}

	for priority, col := range columns {
		bestPeer := ps.assignPeerLocked(col, peers)
		deadline := now.Add(ps.config.SampleTimeout)

		assignment := PeerColumnAssignment{
			Column:   col,
			Peer:     bestPeer.ID,
			Priority: priority,
			Deadline: deadline,
		}
		plan.Assignments = append(plan.Assignments, assignment)

		ss.columns[col] = &columnState{
			column:     col,
			peer:       bestPeer.ID,
			triedPeers: map[string]bool{bestPeer.ID: true},
		}
	}

	ps.slots[slot] = ss
	return plan, nil
}

// AssignPeer selects the best peer for a given column based on latency and
// custody match. Peers that custody the column are preferred. Among those,
// the peer with the lowest latency is chosen. If no peer custodies the column,
// the peer with the highest success rate and lowest latency is chosen.
func (ps *PeerSamplingScheduler) AssignPeer(column uint64, peers []SamplingPeerInfo) SamplingPeerInfo {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.assignPeerLocked(column, peers)
}

// assignPeerLocked selects the best peer without locking. Caller must hold
// at least a read lock on ps.mu.
func (ps *PeerSamplingScheduler) assignPeerLocked(column uint64, peers []SamplingPeerInfo) SamplingPeerInfo {
	if len(peers) == 0 {
		return SamplingPeerInfo{}
	}

	// Separate peers into those that custody the column and those that don't.
	var custodyPeers, otherPeers []SamplingPeerInfo
	for _, p := range peers {
		if p.CustodyColumns[column] {
			custodyPeers = append(custodyPeers, p)
		} else {
			otherPeers = append(otherPeers, p)
		}
	}

	// Pick from custody peers first (prefer lowest latency).
	if len(custodyPeers) > 0 {
		return pickBestPeer(custodyPeers)
	}
	// Fall back to other peers (prefer highest success rate, then lowest latency).
	return pickBestPeer(otherPeers)
}

// pickBestPeer selects the peer with the best combined score.
// Score = successRate / max(latency_ms, 1). Higher is better.
func pickBestPeer(peers []SamplingPeerInfo) SamplingPeerInfo {
	if len(peers) == 0 {
		return SamplingPeerInfo{}
	}
	best := peers[0]
	bestScore := samplingPeerScore(best)
	for _, p := range peers[1:] {
		s := samplingPeerScore(p)
		if s > bestScore {
			bestScore = s
			best = p
		}
	}
	return best
}

// peerScore computes a selection score for a peer.
func samplingPeerScore(p SamplingPeerInfo) float64 {
	latMs := float64(p.Latency.Milliseconds())
	if latMs < 1 {
		latMs = 1
	}
	sr := p.SuccessRate
	if sr <= 0 {
		sr = 0.01 // avoid zero score
	}
	return sr / latMs
}

// TrackResult records the result of a sampling request for a specific column
// and peer. If the sample failed and retries remain, the column is marked
// for retry with a different peer.
func (ps *PeerSamplingScheduler) TrackResult(slot uint64, column uint64, peer string, success bool, latency time.Duration) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.closed {
		return ErrPeerSchedClosed
	}

	ss, ok := ps.slots[slot]
	if !ok {
		return ErrPeerSchedSlotUnknown
	}

	cs, ok := ss.columns[column]
	if !ok {
		return ErrPeerSchedSlotUnknown
	}

	// Update peer stats.
	stat, exists := ps.peerStats[peer]
	if !exists {
		stat = &peerStatEntry{}
		ps.peerStats[peer] = stat
	}
	stat.totalRequests++
	stat.totalLatency += latency
	if success {
		stat.successRequests++
	}

	// Update column state.
	cs.latency = latency
	cs.peer = peer
	if success {
		cs.success = true
		cs.failed = false
	} else {
		cs.retries++
		if cs.retries >= ps.config.MaxRetries {
			cs.failed = true
		}
	}

	return nil
}

// RetryFailed returns column assignments for failed-but-retriable columns,
// selecting different peers than previously tried. Returns nil if no retries
// are needed or possible.
func (ps *PeerSamplingScheduler) RetryFailed(slot uint64, peers []SamplingPeerInfo) []PeerColumnAssignment {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ss, ok := ps.slots[slot]
	if !ok {
		return nil
	}

	now := time.Now()
	var retries []PeerColumnAssignment

	// Collect retriable columns sorted by column index for determinism.
	var retriable []*columnState
	for _, cs := range ss.columns {
		if !cs.success && !cs.failed && cs.retries > 0 && cs.retries < ps.config.MaxRetries {
			retriable = append(retriable, cs)
		}
	}
	sort.Slice(retriable, func(i, j int) bool {
		return retriable[i].column < retriable[j].column
	})

	for _, cs := range retriable {
		// Filter out already-tried peers.
		var available []SamplingPeerInfo
		for _, p := range peers {
			if !cs.triedPeers[p.ID] {
				available = append(available, p)
			}
		}
		if len(available) == 0 {
			// No untried peers, mark as permanently failed.
			cs.failed = true
			continue
		}

		best := ps.assignPeerLocked(cs.column, available)
		cs.triedPeers[best.ID] = true
		cs.peer = best.ID

		retries = append(retries, PeerColumnAssignment{
			Column:   cs.column,
			Peer:     best.ID,
			Priority: cs.retries,
			Deadline: now.Add(ps.config.SampleTimeout),
		})
	}

	return retries
}

// GetSlotStatus returns the current sampling status for a slot.
func (ps *PeerSamplingScheduler) GetSlotStatus(slot uint64) (SlotSamplingStatus, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	ss, ok := ps.slots[slot]
	if !ok {
		return SlotSamplingStatus{}, ErrPeerSchedSlotUnknown
	}

	status := SlotSamplingStatus{
		Slot:  slot,
		Total: len(ss.columns),
	}

	for _, cs := range ss.columns {
		if cs.success {
			status.Completed++
		} else if cs.failed {
			status.Failed++
		} else {
			status.Pending++
		}
	}

	// Determine verdict.
	if status.Completed == status.Total {
		status.Verdict = VerdictAvailable
	} else if status.Total > 0 && float64(status.Failed)/float64(status.Total) > ps.config.FailureThreshold {
		status.Verdict = VerdictUnavailable
	} else {
		status.Verdict = VerdictPending
	}

	return status, nil
}

// GetPeerSuccessRate returns the historical success rate for a peer based
// on accumulated tracking data.
func (ps *PeerSamplingScheduler) GetPeerSuccessRate(peerID string) float64 {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	stat, ok := ps.peerStats[peerID]
	if !ok || stat.totalRequests == 0 {
		return 0
	}
	return float64(stat.successRequests) / float64(stat.totalRequests)
}

// GetPeerAvgLatency returns the average response latency for a peer.
func (ps *PeerSamplingScheduler) GetPeerAvgLatency(peerID string) time.Duration {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	stat, ok := ps.peerStats[peerID]
	if !ok || stat.totalRequests == 0 {
		return 0
	}
	return stat.totalLatency / time.Duration(stat.totalRequests)
}

// SlotCount returns the number of slots currently being tracked.
func (ps *PeerSamplingScheduler) SlotCount() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.slots)
}

// PurgeSlot removes all tracking state for a slot.
func (ps *PeerSamplingScheduler) PurgeSlot(slot uint64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.slots, slot)
}

// Close shuts down the scheduler.
func (ps *PeerSamplingScheduler) Close() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.closed = true
}
