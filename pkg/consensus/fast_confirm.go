// Package consensus - fast confirmation protocol for the CL latency track.
//
// Fast confirmation provides optimistic confirmation of blocks before finality
// is reached. Validators attest to blocks, and once a quorum threshold is met,
// the block is considered "fast confirmed" -- not finalized, but highly likely
// to be included in the canonical chain. This reduces perceived latency for
// users and applications without weakening finality guarantees.
package consensus

import (
	"errors"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Fast confirmation errors.
var (
	ErrFCSlotZero          = errors.New("fast confirm: slot must be > 0")
	ErrFCBlockRootEmpty    = errors.New("fast confirm: block root must not be empty")
	ErrFCDuplicateAttester = errors.New("fast confirm: duplicate attester for slot")
	ErrFCSlotExpired       = errors.New("fast confirm: slot has expired")
	ErrFCNotFound          = errors.New("fast confirm: no confirmation for slot")
)

// FastConfirmConfig configures the fast confirmation protocol.
type FastConfirmConfig struct {
	// QuorumThreshold is the fraction of attesters needed for confirmation
	// (0.0 to 1.0). Typically 0.67 for 2/3 supermajority.
	QuorumThreshold float64

	// MinAttesters is the minimum number of unique attesters required
	// before quorum logic is even checked.
	MinAttesters int

	// ConfirmTimeout is how long after slot start to wait for attestations
	// before giving up on fast confirmation.
	ConfirmTimeout time.Duration

	// MaxTrackedSlots limits how many slots are tracked simultaneously.
	// Older slots are automatically pruned when this limit is exceeded.
	MaxTrackedSlots int

	// TotalValidators is the total validator set size used to compute
	// whether the quorum threshold is met.
	TotalValidators int
}

// DefaultFastConfirmConfig returns a sensible default configuration.
func DefaultFastConfirmConfig() *FastConfirmConfig {
	return &FastConfirmConfig{
		QuorumThreshold: 0.67,
		MinAttesters:    64,
		ConfirmTimeout:  4 * time.Second,
		MaxTrackedSlots: 64,
		TotalValidators: 1024,
	}
}

// FastConfirmation represents the fast confirmation state for a single slot.
type FastConfirmation struct {
	Slot             Slot
	BlockRoot        types.Hash
	AttestationCount int
	Confirmed        bool
	Timestamp        time.Time // when confirmation was achieved (zero if not confirmed)
}

// slotAttestation tracks per-slot attestation state internally.
type slotAttestation struct {
	blockRoot types.Hash
	attesters map[ValidatorIndex]struct{} // set of attester indices
	confirmed bool
	confirmedAt time.Time
	createdAt time.Time
}

// FastConfirmTracker collects attestations and confirms blocks when quorum is met.
// All methods are safe for concurrent use.
type FastConfirmTracker struct {
	mu     sync.RWMutex
	config *FastConfirmConfig
	slots  map[Slot]*slotAttestation
	// slotOrder tracks insertion order for pruning oldest slots.
	slotOrder []Slot
}

// NewFastConfirmTracker creates a new tracker with the given config.
func NewFastConfirmTracker(cfg *FastConfirmConfig) *FastConfirmTracker {
	if cfg == nil {
		cfg = DefaultFastConfirmConfig()
	}
	return &FastConfirmTracker{
		config: cfg,
		slots:  make(map[Slot]*slotAttestation),
	}
}

// AddAttestation records an attestation for the given slot and block root
// from the specified attester. Returns an error if the attester has already
// attested to this slot, if the slot is zero, or if the block root is empty.
// If the quorum threshold is met, the slot is marked as confirmed.
func (ft *FastConfirmTracker) AddAttestation(slot Slot, blockRoot types.Hash, attesterIndex ValidatorIndex) error {
	if slot == 0 {
		return ErrFCSlotZero
	}
	emptyHash := types.Hash{}
	if blockRoot == emptyHash {
		return ErrFCBlockRootEmpty
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()

	sa, exists := ft.slots[slot]
	if !exists {
		// Create a new slot attestation entry.
		sa = &slotAttestation{
			blockRoot: blockRoot,
			attesters: make(map[ValidatorIndex]struct{}),
			createdAt: time.Now(),
		}
		ft.slots[slot] = sa
		ft.slotOrder = append(ft.slotOrder, slot)
		ft.pruneOldestLocked()
	}

	// Check for duplicate attester.
	if _, dup := sa.attesters[attesterIndex]; dup {
		return ErrFCDuplicateAttester
	}

	sa.attesters[attesterIndex] = struct{}{}

	// Check if quorum is met.
	if !sa.confirmed && ft.isQuorumMet(len(sa.attesters)) {
		sa.confirmed = true
		sa.confirmedAt = time.Now()
	}

	return nil
}

// GetConfirmation returns the fast confirmation state for a slot.
// Returns ErrFCNotFound if the slot is not being tracked.
func (ft *FastConfirmTracker) GetConfirmation(slot Slot) (*FastConfirmation, error) {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	sa, exists := ft.slots[slot]
	if !exists {
		return nil, ErrFCNotFound
	}

	return &FastConfirmation{
		Slot:             slot,
		BlockRoot:        sa.blockRoot,
		AttestationCount: len(sa.attesters),
		Confirmed:        sa.confirmed,
		Timestamp:        sa.confirmedAt,
	}, nil
}

// IsConfirmed returns true if the given slot+blockRoot pair has been fast
// confirmed. Returns false if the slot is not tracked, not yet confirmed,
// or if the block root does not match.
func (ft *FastConfirmTracker) IsConfirmed(slot Slot, blockRoot types.Hash) bool {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	sa, exists := ft.slots[slot]
	if !exists {
		return false
	}
	return sa.confirmed && sa.blockRoot == blockRoot
}

// AttestationCount returns the number of attestations for a slot, or 0
// if the slot is not tracked.
func (ft *FastConfirmTracker) AttestationCount(slot Slot) int {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	sa, exists := ft.slots[slot]
	if !exists {
		return 0
	}
	return len(sa.attesters)
}

// TrackedSlots returns the number of slots currently being tracked.
func (ft *FastConfirmTracker) TrackedSlots() int {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return len(ft.slots)
}

// PruneExpired removes all slot entries older than the configured timeout
// relative to the given reference time. This can be called periodically
// to free memory for old, unconfirmed slots.
func (ft *FastConfirmTracker) PruneExpired(now time.Time) int {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	pruned := 0
	remaining := ft.slotOrder[:0]
	for _, slot := range ft.slotOrder {
		sa, exists := ft.slots[slot]
		if !exists {
			continue
		}
		if now.Sub(sa.createdAt) > ft.config.ConfirmTimeout {
			delete(ft.slots, slot)
			pruned++
		} else {
			remaining = append(remaining, slot)
		}
	}
	ft.slotOrder = remaining
	return pruned
}

// Config returns the tracker's configuration (read-only copy).
func (ft *FastConfirmTracker) Config() FastConfirmConfig {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return *ft.config
}

// isQuorumMet checks whether the attestation count meets the quorum threshold.
// Must be called with ft.mu held.
func (ft *FastConfirmTracker) isQuorumMet(count int) bool {
	if count < ft.config.MinAttesters {
		return false
	}
	if ft.config.TotalValidators <= 0 {
		return false
	}
	ratio := float64(count) / float64(ft.config.TotalValidators)
	return ratio >= ft.config.QuorumThreshold
}

// pruneOldestLocked removes the oldest tracked slots if the max is exceeded.
// Must be called with ft.mu held.
func (ft *FastConfirmTracker) pruneOldestLocked() {
	for len(ft.slots) > ft.config.MaxTrackedSlots && len(ft.slotOrder) > 0 {
		oldest := ft.slotOrder[0]
		ft.slotOrder = ft.slotOrder[1:]
		delete(ft.slots, oldest)
	}
}
