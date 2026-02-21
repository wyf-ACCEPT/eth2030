// Package consensus implements the Ethereum consensus layer.
// This file adds PhaseTimer: precise sub-slot phase tracking for 6-second
// quick slots, dividing each slot into Proposal, Attestation, and Aggregation
// phases. It complements SlotTimer and QuickSlotScheduler by providing
// millisecond-resolution phase boundaries and event subscriptions.
package consensus

import (
	"sync"
	"time"
)

// PhaseTimerConfig configures the phase timer with per-phase durations.
type PhaseTimerConfig struct {
	// SlotDurationMs is the total slot duration in milliseconds. Default: 6000.
	SlotDurationMs uint64

	// ProposalPhaseMs is the proposal phase duration. Default: 2000.
	ProposalPhaseMs uint64

	// AttestationPhaseMs is the attestation phase duration. Default: 2000.
	AttestationPhaseMs uint64

	// AggregationPhaseMs is the aggregation phase duration. Default: 2000.
	AggregationPhaseMs uint64

	// GenesisTime is the unix timestamp (seconds) of chain genesis.
	GenesisTime int64

	// SlotsPerEpoch is the number of slots in one epoch. Default: 4.
	SlotsPerEpoch uint64
}

// DefaultPhaseTimerConfig returns the standard 6-second quick-slot config
// with equal 2-second phases and 4-slot epochs.
func DefaultPhaseTimerConfig() *PhaseTimerConfig {
	return &PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        0,
		SlotsPerEpoch:      4,
	}
}

// SlotPhase represents a sub-slot phase.
type SlotPhase uint8

const (
	// PhaseProposal is the block proposal window.
	PhaseProposal SlotPhase = iota
	// PhaseAttestation is the attestation collection window.
	PhaseAttestation
	// PhaseAggregation is the attestation aggregation window.
	PhaseAggregation
)

// String returns a human-readable phase name.
func (p SlotPhase) String() string {
	switch p {
	case PhaseProposal:
		return "proposal"
	case PhaseAttestation:
		return "attestation"
	case PhaseAggregation:
		return "aggregation"
	default:
		return "unknown"
	}
}

// SlotEvent is emitted when a slot or phase boundary is crossed.
type SlotEvent struct {
	Slot      uint64
	Phase     SlotPhase
	Timestamp time.Time
}

// PhaseTimer provides millisecond-resolution slot and phase tracking for
// 6-second quick slots. It is safe for concurrent use.
type PhaseTimer struct {
	mu       sync.RWMutex
	config   PhaseTimerConfig
	timeFunc func() time.Time

	// subscribers receive slot/phase boundary events.
	subMu   sync.Mutex
	nextID  int
	subs    map[int]chan SlotEvent
	stopCh  chan struct{}
	running bool
}

// NewPhaseTimer creates a phase timer from the given config. If config is nil,
// DefaultPhaseTimerConfig is used. The timer uses real wall-clock time.
func NewPhaseTimer(config *PhaseTimerConfig) *PhaseTimer {
	if config == nil {
		config = DefaultPhaseTimerConfig()
	}
	cfg := *config
	if cfg.SlotDurationMs == 0 {
		cfg.SlotDurationMs = 6000
	}
	if cfg.SlotsPerEpoch == 0 {
		cfg.SlotsPerEpoch = 4
	}
	// Ensure phase durations sum to slot duration. If not configured,
	// split evenly.
	total := cfg.ProposalPhaseMs + cfg.AttestationPhaseMs + cfg.AggregationPhaseMs
	if total == 0 {
		third := cfg.SlotDurationMs / 3
		cfg.ProposalPhaseMs = third
		cfg.AttestationPhaseMs = third
		cfg.AggregationPhaseMs = cfg.SlotDurationMs - 2*third
		total = cfg.SlotDurationMs
	}
	// Adjust slot duration to match phase sum if mismatched.
	if total != cfg.SlotDurationMs {
		cfg.SlotDurationMs = total
	}

	return &PhaseTimer{
		config:   cfg,
		timeFunc: time.Now,
		subs:     make(map[int]chan SlotEvent),
		stopCh:   make(chan struct{}),
	}
}

// CurrentSlot returns the current slot number based on wall-clock time.
// Returns 0 if before genesis.
func (pt *PhaseTimer) CurrentSlot() uint64 {
	now := pt.timeFunc()
	return pt.slotAtTime(now)
}

// slotAtTime computes the slot at a given time.
func (pt *PhaseTimer) slotAtTime(t time.Time) uint64 {
	nowMs := t.UnixMilli()
	genesisMs := pt.config.GenesisTime * 1000
	if nowMs < genesisMs {
		return 0
	}
	elapsedMs := uint64(nowMs - genesisMs)
	return elapsedMs / pt.config.SlotDurationMs
}

// CurrentPhase returns the current sub-slot phase.
func (pt *PhaseTimer) CurrentPhase() SlotPhase {
	now := pt.timeFunc()
	return pt.phaseAtTime(now)
}

// phaseAtTime computes the phase at a given time.
func (pt *PhaseTimer) phaseAtTime(t time.Time) SlotPhase {
	nowMs := t.UnixMilli()
	genesisMs := pt.config.GenesisTime * 1000
	if nowMs < genesisMs {
		return PhaseProposal
	}
	elapsedMs := uint64(nowMs - genesisMs)
	offsetInSlot := elapsedMs % pt.config.SlotDurationMs

	if offsetInSlot < pt.config.ProposalPhaseMs {
		return PhaseProposal
	}
	if offsetInSlot < pt.config.ProposalPhaseMs+pt.config.AttestationPhaseMs {
		return PhaseAttestation
	}
	return PhaseAggregation
}

// TimeToNextSlot returns the duration until the next slot boundary.
func (pt *PhaseTimer) TimeToNextSlot() time.Duration {
	now := pt.timeFunc()
	slot := pt.slotAtTime(now)
	nextStart := pt.slotStartTimeInternal(slot + 1)
	return nextStart.Sub(now)
}

// TimeToNextPhase returns the duration until the next phase boundary
// within the current slot, or to the next slot if in the last phase.
func (pt *PhaseTimer) TimeToNextPhase() time.Duration {
	now := pt.timeFunc()
	slot := pt.slotAtTime(now)
	phase := pt.phaseAtTime(now)

	var nextPhaseStart time.Time
	switch phase {
	case PhaseProposal:
		nextPhaseStart = pt.PhaseStartTime(slot, PhaseAttestation)
	case PhaseAttestation:
		nextPhaseStart = pt.PhaseStartTime(slot, PhaseAggregation)
	default:
		// In aggregation: next boundary is start of next slot.
		nextPhaseStart = pt.slotStartTimeInternal(slot + 1)
	}
	return nextPhaseStart.Sub(now)
}

// SlotStartTime returns the time when the given slot begins.
func (pt *PhaseTimer) SlotStartTime(slot uint64) time.Time {
	return pt.slotStartTimeInternal(slot)
}

func (pt *PhaseTimer) slotStartTimeInternal(slot uint64) time.Time {
	genesisMs := pt.config.GenesisTime * 1000
	slotMs := int64(slot * pt.config.SlotDurationMs)
	return time.UnixMilli(genesisMs + slotMs)
}

// PhaseStartTime returns the time when a specific phase begins within a slot.
func (pt *PhaseTimer) PhaseStartTime(slot uint64, phase SlotPhase) time.Time {
	start := pt.slotStartTimeInternal(slot)
	switch phase {
	case PhaseProposal:
		return start
	case PhaseAttestation:
		return start.Add(time.Duration(pt.config.ProposalPhaseMs) * time.Millisecond)
	case PhaseAggregation:
		offset := pt.config.ProposalPhaseMs + pt.config.AttestationPhaseMs
		return start.Add(time.Duration(offset) * time.Millisecond)
	default:
		return start
	}
}

// IsInSlot returns true if the current time falls within the given slot.
func (pt *PhaseTimer) IsInSlot(slot uint64) bool {
	return pt.CurrentSlot() == slot
}

// EpochForSlot returns the epoch number for a given slot.
func (pt *PhaseTimer) EpochForSlot(slot uint64) uint64 {
	return slot / pt.config.SlotsPerEpoch
}

// IsEpochBoundary returns true if the given slot is the first slot of its epoch.
func (pt *PhaseTimer) IsEpochBoundary(slot uint64) bool {
	return slot%pt.config.SlotsPerEpoch == 0
}

// Subscribe returns a channel that receives SlotEvent notifications on
// slot and phase boundaries. The returned channel is buffered (size 8).
// Call Unsubscribe with the same channel to stop receiving events.
func (pt *PhaseTimer) Subscribe() <-chan SlotEvent {
	pt.subMu.Lock()
	defer pt.subMu.Unlock()

	ch := make(chan SlotEvent, 8)
	pt.nextID++
	pt.subs[pt.nextID] = ch
	return ch
}

// Unsubscribe removes a previously subscribed channel.
func (pt *PhaseTimer) Unsubscribe(ch <-chan SlotEvent) {
	pt.subMu.Lock()
	defer pt.subMu.Unlock()

	for id, sub := range pt.subs {
		if sub == ch {
			close(sub)
			delete(pt.subs, id)
			return
		}
	}
}

// notify sends an event to all subscribers (non-blocking).
func (pt *PhaseTimer) notify(evt SlotEvent) {
	pt.subMu.Lock()
	defer pt.subMu.Unlock()

	for _, ch := range pt.subs {
		select {
		case ch <- evt:
		default:
			// Drop if subscriber is slow.
		}
	}
}

// Config returns a copy of the timer configuration.
func (pt *PhaseTimer) Config() PhaseTimerConfig {
	return pt.config
}

// GenesisTime returns the configured genesis time as time.Time.
func (pt *PhaseTimer) GenesisTime() time.Time {
	return time.Unix(pt.config.GenesisTime, 0)
}

// SlotDuration returns the total slot duration.
func (pt *PhaseTimer) SlotDuration() time.Duration {
	return time.Duration(pt.config.SlotDurationMs) * time.Millisecond
}

// PhaseDuration returns the duration of a specific phase.
func (pt *PhaseTimer) PhaseDuration(phase SlotPhase) time.Duration {
	switch phase {
	case PhaseProposal:
		return time.Duration(pt.config.ProposalPhaseMs) * time.Millisecond
	case PhaseAttestation:
		return time.Duration(pt.config.AttestationPhaseMs) * time.Millisecond
	case PhaseAggregation:
		return time.Duration(pt.config.AggregationPhaseMs) * time.Millisecond
	default:
		return 0
	}
}

// ProgressInSlot returns the fractional progress through the current slot
// as a value in [0.0, 1.0).
func (pt *PhaseTimer) ProgressInSlot() float64 {
	now := pt.timeFunc()
	nowMs := now.UnixMilli()
	genesisMs := pt.config.GenesisTime * 1000
	if nowMs < genesisMs {
		return 0
	}
	elapsedMs := uint64(nowMs - genesisMs)
	offsetInSlot := elapsedMs % pt.config.SlotDurationMs
	return float64(offsetInSlot) / float64(pt.config.SlotDurationMs)
}
