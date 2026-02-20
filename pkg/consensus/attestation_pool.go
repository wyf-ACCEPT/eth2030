// Package consensus - attestation pool for managing pending attestations.
//
// Implements attestation collection, validation, aggregation, deduplication,
// pruning, and block-inclusion selection per phase0/Altair specs.
package consensus

import (
	"errors"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Attestation pool constants.
const (
	// MaxAttestationsPerBlock is the maximum attestations per block (phase0).
	MaxAttestationsPerBlock = 128

	// DefaultMaxPoolSize limits total attestations held in the pool.
	DefaultMaxPoolSize = 8192

	// DefaultPruneSlots is the number of slots after which old attestations
	// are pruned. Per spec: data.slot + SLOTS_PER_EPOCH >= state.slot.
	DefaultPruneSlots = 32

	// MinAttestationInclusionDelay is the minimum delay for attestation
	// inclusion (1 slot per phase0 spec).
	MinAttestationInclusionDelay = 1
)

// Attestation pool errors.
var (
	ErrPoolAttNil           = errors.New("attestation_pool: nil attestation")
	ErrPoolAttSlotTooOld    = errors.New("attestation_pool: attestation slot too old")
	ErrPoolAttFutureSlot    = errors.New("attestation_pool: attestation slot is in the future")
	ErrPoolAttSourceEpoch   = errors.New("attestation_pool: source epoch mismatch")
	ErrPoolAttTargetEpoch   = errors.New("attestation_pool: target epoch mismatch")
	ErrPoolAttNoBits        = errors.New("attestation_pool: empty aggregation bits")
	ErrPoolFull             = errors.New("attestation_pool: pool is full")
)

// PoolAttestation is the pool's internal attestation format, including
// phase0-style committee_index as a separate field alongside the EIP-7549
// data model.
type PoolAttestation struct {
	Slot            Slot
	CommitteeIndex  uint64
	AggregationBits []byte
	BeaconBlockRoot types.Hash
	Source          Checkpoint
	Target          Checkpoint
	Signature       types.Hash
}

// dataKey returns a key that uniquely identifies the attestation data
// (slot + block root + source + target). Attestations with the same key
// can be aggregated.
func (a *PoolAttestation) dataKey() attestationDataKey {
	return attestationDataKey{
		Slot:            a.Slot,
		CommitteeIndex:  a.CommitteeIndex,
		BeaconBlockRoot: a.BeaconBlockRoot,
		SourceEpoch:     a.Source.Epoch,
		SourceRoot:      a.Source.Root,
		TargetEpoch:     a.Target.Epoch,
		TargetRoot:      a.Target.Root,
	}
}

// bitCount returns the number of set bits in the aggregation bitfield.
func (a *PoolAttestation) bitCount() int {
	count := 0
	for _, b := range a.AggregationBits {
		count += popcount(b)
	}
	return count
}

// attestationDataKey is a hashable key for attestation data grouping.
type attestationDataKey struct {
	Slot            Slot
	CommitteeIndex  uint64
	BeaconBlockRoot types.Hash
	SourceEpoch     Epoch
	SourceRoot      types.Hash
	TargetEpoch     Epoch
	TargetRoot      types.Hash
}

// AttestationPoolConfig configures the attestation pool.
type AttestationPoolConfig struct {
	MaxPoolSize     int    // maximum number of attestations in the pool
	PruneSlots      uint64 // attestations older than currentSlot - PruneSlots are removed
	SlotsPerEpoch   uint64 // slots per epoch for epoch calculations
}

// DefaultAttestationPoolConfig returns the default pool configuration.
func DefaultAttestationPoolConfig() *AttestationPoolConfig {
	return &AttestationPoolConfig{
		MaxPoolSize:   DefaultMaxPoolSize,
		PruneSlots:    DefaultPruneSlots,
		SlotsPerEpoch: 32,
	}
}

// AttestationPool manages pending attestations for block inclusion.
// All public methods are thread-safe.
type AttestationPool struct {
	mu sync.RWMutex

	config *AttestationPoolConfig

	// attestations indexed by data key for fast aggregation lookup.
	byKey map[attestationDataKey][]*PoolAttestation

	// included tracks attestation data keys that have been included in blocks.
	included map[attestationDataKey]bool

	// total is the current count of attestations in the pool.
	total int

	// currentSlot tracks the pool's view of the current slot.
	currentSlot Slot

	// justifiedCheckpoint is the current justified checkpoint for validation.
	justifiedCheckpoint Checkpoint
}

// NewAttestationPool creates a new attestation pool.
func NewAttestationPool(cfg *AttestationPoolConfig) *AttestationPool {
	if cfg == nil {
		cfg = DefaultAttestationPoolConfig()
	}
	return &AttestationPool{
		config:   cfg,
		byKey:    make(map[attestationDataKey][]*PoolAttestation),
		included: make(map[attestationDataKey]bool),
	}
}

// SetCurrentSlot updates the pool's view of the current slot and triggers
// pruning of old attestations.
func (p *AttestationPool) SetCurrentSlot(slot Slot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.currentSlot = slot
	p.pruneLocked()
}

// SetJustifiedCheckpoint updates the justified checkpoint used for validation.
func (p *AttestationPool) SetJustifiedCheckpoint(cp Checkpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.justifiedCheckpoint = cp
}

// Add validates and inserts an attestation into the pool. If an attestation
// with the same data already exists, it attempts to aggregate (OR bits).
func (p *AttestationPool) Add(att *PoolAttestation) error {
	if att == nil {
		return ErrPoolAttNil
	}
	if len(att.AggregationBits) == 0 {
		return ErrPoolAttNoBits
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Validate slot bounds.
	if err := p.validateSlotLocked(att); err != nil {
		return err
	}

	// Validate source and target epochs.
	if err := p.validateEpochsLocked(att); err != nil {
		return err
	}

	// Skip if already included in a block.
	key := att.dataKey()
	if p.included[key] {
		return nil // silently drop duplicates
	}

	// Try to aggregate with existing attestations.
	existing := p.byKey[key]
	if aggregated := p.tryAggregateLocked(existing, att); aggregated {
		return nil
	}

	// Check pool size limit.
	if p.total >= p.config.MaxPoolSize {
		return ErrPoolFull
	}

	// Add as new entry.
	cp := copyPoolAttestation(att)
	p.byKey[key] = append(p.byKey[key], cp)
	p.total++

	return nil
}

// GetForBlock selects the best attestations for inclusion in a block at the
// given slot, up to MaxAttestationsPerBlock. Attestations are ranked by
// committee coverage (number of set bits) and must satisfy the inclusion
// delay requirement.
func (p *AttestationPool) GetForBlock(slot Slot) []*PoolAttestation {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var candidates []*PoolAttestation

	for key, atts := range p.byKey {
		// Skip already-included data.
		if p.included[key] {
			continue
		}
		// Attestation must satisfy inclusion delay: att.slot + 1 <= slot.
		for _, att := range atts {
			if uint64(att.Slot)+MinAttestationInclusionDelay <= uint64(slot) &&
				uint64(att.Slot)+p.config.PruneSlots >= uint64(slot) {
				candidates = append(candidates, att)
			}
		}
	}

	// Sort by coverage (descending), breaking ties by slot (newer first).
	sort.Slice(candidates, func(i, j int) bool {
		ci := candidates[i].bitCount()
		cj := candidates[j].bitCount()
		if ci != cj {
			return ci > cj
		}
		return candidates[i].Slot > candidates[j].Slot
	})

	// Limit to MaxAttestationsPerBlock.
	if len(candidates) > MaxAttestationsPerBlock {
		candidates = candidates[:MaxAttestationsPerBlock]
	}

	// Return copies so callers don't hold references to pool internals.
	result := make([]*PoolAttestation, len(candidates))
	for i, c := range candidates {
		result[i] = copyPoolAttestation(c)
	}
	return result
}

// MarkIncluded marks attestation data as included in a block, preventing
// future selection for block inclusion.
func (p *AttestationPool) MarkIncluded(att *PoolAttestation) {
	if att == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key := att.dataKey()
	p.included[key] = true

	// Remove from the active pool.
	if atts, ok := p.byKey[key]; ok {
		p.total -= len(atts)
		delete(p.byKey, key)
	}
}

// Size returns the current number of attestations in the pool.
func (p *AttestationPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.total
}

// KeyCount returns the number of distinct attestation data keys in the pool.
func (p *AttestationPool) KeyCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.byKey)
}

// --- Internal methods (must hold p.mu) ---

// validateSlotLocked checks that the attestation slot is within valid range.
func (p *AttestationPool) validateSlotLocked(att *PoolAttestation) error {
	if p.currentSlot > 0 {
		// Must not be too old.
		if uint64(p.currentSlot) > p.config.PruneSlots &&
			uint64(att.Slot) < uint64(p.currentSlot)-p.config.PruneSlots {
			return ErrPoolAttSlotTooOld
		}
		// Must not be in the future.
		if att.Slot > p.currentSlot {
			return ErrPoolAttFutureSlot
		}
	}
	return nil
}

// validateEpochsLocked checks source and target epoch consistency.
func (p *AttestationPool) validateEpochsLocked(att *PoolAttestation) error {
	if p.config.SlotsPerEpoch == 0 {
		return nil
	}

	// Source must match the justified checkpoint epoch.
	zeroCP := Checkpoint{}
	if p.justifiedCheckpoint != zeroCP {
		if att.Source.Epoch != p.justifiedCheckpoint.Epoch {
			return ErrPoolAttSourceEpoch
		}
	}

	// Target epoch must match the epoch of the attestation slot.
	attEpoch := Epoch(uint64(att.Slot) / p.config.SlotsPerEpoch)
	if att.Target.Epoch != attEpoch {
		return ErrPoolAttTargetEpoch
	}

	return nil
}

// tryAggregateLocked attempts to merge att into an existing attestation with
// non-overlapping bits. Returns true if aggregation succeeded.
func (p *AttestationPool) tryAggregateLocked(existing []*PoolAttestation, att *PoolAttestation) bool {
	for _, e := range existing {
		if canAggregate(e, att) {
			mergeAggregationBits(e, att)
			return true
		}
	}
	return false
}

// pruneLocked removes attestations older than currentSlot - PruneSlots.
func (p *AttestationPool) pruneLocked() {
	if p.currentSlot == 0 {
		return
	}
	var cutoff uint64
	if uint64(p.currentSlot) > p.config.PruneSlots {
		cutoff = uint64(p.currentSlot) - p.config.PruneSlots
	}

	for key, atts := range p.byKey {
		if uint64(key.Slot) < cutoff {
			p.total -= len(atts)
			delete(p.byKey, key)
		}
	}

	// Also prune old included markers.
	for key := range p.included {
		if uint64(key.Slot) < cutoff {
			delete(p.included, key)
		}
	}
}

// --- Utility functions ---

// canAggregate checks whether two attestations can be aggregated (same data,
// non-overlapping aggregation bits).
func canAggregate(a, b *PoolAttestation) bool {
	minLen := len(a.AggregationBits)
	if len(b.AggregationBits) < minLen {
		minLen = len(b.AggregationBits)
	}
	for i := 0; i < minLen; i++ {
		if a.AggregationBits[i]&b.AggregationBits[i] != 0 {
			return false // overlapping bits
		}
	}
	return true
}

// mergeAggregationBits ORs b's bits into a, extending a if necessary.
func mergeAggregationBits(a, b *PoolAttestation) {
	if len(b.AggregationBits) > len(a.AggregationBits) {
		extended := make([]byte, len(b.AggregationBits))
		copy(extended, a.AggregationBits)
		a.AggregationBits = extended
	}
	for i, v := range b.AggregationBits {
		a.AggregationBits[i] |= v
	}
}

// copyPoolAttestation returns a deep copy of a PoolAttestation.
func copyPoolAttestation(att *PoolAttestation) *PoolAttestation {
	cp := *att
	cp.AggregationBits = make([]byte, len(att.AggregationBits))
	copy(cp.AggregationBits, att.AggregationBits)
	return &cp
}

// popcount returns the number of set bits in a byte.
func popcount(b byte) int {
	count := 0
	for b != 0 {
		count += int(b & 1)
		b >>= 1
	}
	return count
}
