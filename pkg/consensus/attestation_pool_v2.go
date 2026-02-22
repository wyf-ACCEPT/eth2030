// Package consensus - enhanced attestation pool with inclusion delay tracking,
// per-committee best attestation selection, scored ranking, and advanced
// aggregation strategies. Builds on the base AttestationPool with deeper
// spec-aligned features for block production optimization.
package consensus

import (
	"errors"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Enhanced attestation pool constants.
const (
	// MaxInclusionDelay is the maximum number of slots an attestation can
	// be delayed and still be valid for inclusion (1 epoch = 32 slots).
	MaxInclusionDelay uint64 = 32

	// OptimalInclusionDelay is the ideal inclusion delay (1 slot).
	OptimalInclusionDelay uint64 = 1

	// MaxCommitteesPerSlotV2 is the max committees per slot for scoring.
	MaxCommitteesPerSlotV2 uint64 = 64

	// DefaultMaxPoolSizeV2 is the enhanced pool's default max capacity.
	DefaultMaxPoolSizeV2 = 16384

	// AttestationScoreDecayPerSlot is the score reduction per slot of
	// inclusion delay. Attestations included sooner are more valuable.
	AttestationScoreDecayPerSlot uint64 = 10
)

// Enhanced attestation pool errors.
var (
	ErrPoolV2AttNil          = errors.New("attestation_pool_v2: nil attestation")
	ErrPoolV2AttNoBits       = errors.New("attestation_pool_v2: empty aggregation bits")
	ErrPoolV2AttSlotTooOld   = errors.New("attestation_pool_v2: attestation slot too old")
	ErrPoolV2AttFutureSlot   = errors.New("attestation_pool_v2: attestation slot in future")
	ErrPoolV2AttDuplicate    = errors.New("attestation_pool_v2: duplicate attestation")
	ErrPoolV2Full            = errors.New("attestation_pool_v2: pool capacity exceeded")
	ErrPoolV2InvalidCommittee = errors.New("attestation_pool_v2: committee index out of range")
)

// ScoredAttestation wraps a PoolAttestation with scoring metadata used
// for ranked block inclusion selection.
type ScoredAttestation struct {
	Att            *PoolAttestation
	Score          uint64 // higher is better
	InclusionDelay uint64 // slots since attestation was created
	BitCount       int    // number of set aggregation bits
	IsAggregated   bool   // true if this was produced by aggregation
}

// committeeKey uniquely identifies a committee at a specific slot.
type committeeKey struct {
	Slot           Slot
	CommitteeIndex uint64
}

// InclusionDelayStats tracks attestation inclusion delay statistics for
// monitoring network health and proposer efficiency.
type InclusionDelayStats struct {
	TotalAttestations uint64
	TotalDelay        uint64  // sum of all inclusion delays
	MinDelay          uint64
	MaxDelay          uint64
	OptimalCount      uint64  // attestations included at delay=1
}

// AverageDelay returns the mean inclusion delay across all tracked attestations.
func (s *InclusionDelayStats) AverageDelay() float64 {
	if s.TotalAttestations == 0 {
		return 0
	}
	return float64(s.TotalDelay) / float64(s.TotalAttestations)
}

// OptimalRate returns the fraction of attestations included at the optimal
// delay of 1 slot.
func (s *InclusionDelayStats) OptimalRate() float64 {
	if s.TotalAttestations == 0 {
		return 0
	}
	return float64(s.OptimalCount) / float64(s.TotalAttestations)
}

// AttestationPoolV2Config configures the enhanced attestation pool.
type AttestationPoolV2Config struct {
	MaxPoolSize       int
	MaxInclusionDelay uint64
	SlotsPerEpoch     uint64
}

// DefaultAttestationPoolV2Config returns the default enhanced pool config.
func DefaultAttestationPoolV2Config() *AttestationPoolV2Config {
	return &AttestationPoolV2Config{
		MaxPoolSize:       DefaultMaxPoolSizeV2,
		MaxInclusionDelay: MaxInclusionDelay,
		SlotsPerEpoch:     32,
	}
}

// AttestationPoolV2 is an enhanced attestation pool that tracks inclusion
// delay, supports per-committee best attestation selection, and provides
// scored ranking for optimal block production. Thread-safe.
type AttestationPoolV2 struct {
	mu sync.RWMutex

	config *AttestationPoolV2Config

	// byCommittee indexes attestations by (slot, committee) for fast
	// per-committee best attestation retrieval.
	byCommittee map[committeeKey][]*PoolAttestation

	// byDataKey indexes by attestation data for aggregation, same as
	// the base pool.
	byDataKey map[attestationDataKey][]*PoolAttestation

	// seen tracks unique attestation fingerprints for deduplication.
	// Key is derived from data key + aggregation bits hash.
	seen map[types.Hash]bool

	// included tracks data keys that have been included in finalized blocks.
	included map[attestationDataKey]Slot // value = inclusion slot

	// delayStats tracks inclusion delay statistics.
	delayStats InclusionDelayStats

	// total is the current attestation count.
	total int

	// currentSlot is the pool's view of the head slot.
	currentSlot Slot
}

// NewAttestationPoolV2 creates a new enhanced attestation pool.
func NewAttestationPoolV2(cfg *AttestationPoolV2Config) *AttestationPoolV2 {
	if cfg == nil {
		cfg = DefaultAttestationPoolV2Config()
	}
	return &AttestationPoolV2{
		config:      cfg,
		byCommittee: make(map[committeeKey][]*PoolAttestation),
		byDataKey:   make(map[attestationDataKey][]*PoolAttestation),
		seen:        make(map[types.Hash]bool),
		included:    make(map[attestationDataKey]Slot),
		delayStats:  InclusionDelayStats{MinDelay: ^uint64(0)},
	}
}

// SetCurrentSlot updates the pool's head slot and prunes stale data.
func (p *AttestationPoolV2) SetCurrentSlot(slot Slot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.currentSlot = slot
	p.pruneLocked()
}

// Add inserts an attestation into the pool with deduplication and
// aggregation. Returns an error if the attestation is invalid or the pool
// is full.
func (p *AttestationPoolV2) Add(att *PoolAttestation) error {
	if att == nil {
		return ErrPoolV2AttNil
	}
	if len(att.AggregationBits) == 0 {
		return ErrPoolV2AttNoBits
	}
	if att.CommitteeIndex >= MaxCommitteesPerSlotV2 {
		return ErrPoolV2InvalidCommittee
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Validate slot bounds.
	if p.currentSlot > 0 {
		if uint64(att.Slot)+p.config.MaxInclusionDelay < uint64(p.currentSlot) {
			return ErrPoolV2AttSlotTooOld
		}
		if att.Slot > p.currentSlot {
			return ErrPoolV2AttFutureSlot
		}
	}

	// Deduplication: compute a fingerprint from the data key and bits.
	fp := p.fingerprintLocked(att)
	if p.seen[fp] {
		return ErrPoolV2AttDuplicate
	}

	// Skip if already included in a block.
	dk := att.dataKey()
	if _, included := p.included[dk]; included {
		return nil
	}

	// Try to aggregate with existing attestations sharing the same data key.
	existing := p.byDataKey[dk]
	for _, e := range existing {
		if canAggregate(e, att) {
			mergeAggregationBits(e, att)
			p.seen[fp] = true
			return nil
		}
	}

	// Check capacity.
	if p.total >= p.config.MaxPoolSize {
		return ErrPoolV2Full
	}

	// Deep copy and insert.
	cp := copyPoolAttestation(att)
	p.byDataKey[dk] = append(p.byDataKey[dk], cp)

	ck := committeeKey{Slot: att.Slot, CommitteeIndex: att.CommitteeIndex}
	p.byCommittee[ck] = append(p.byCommittee[ck], cp)

	p.seen[fp] = true
	p.total++
	return nil
}

// GetBestForCommittee returns the best (highest-coverage) attestation
// for a specific committee at a specific slot. Returns nil if none found.
func (p *AttestationPoolV2) GetBestForCommittee(slot Slot, committee uint64) *PoolAttestation {
	p.mu.RLock()
	defer p.mu.RUnlock()

	ck := committeeKey{Slot: slot, CommitteeIndex: committee}
	atts := p.byCommittee[ck]
	if len(atts) == 0 {
		return nil
	}

	best := atts[0]
	bestCount := best.bitCount()
	for _, att := range atts[1:] {
		c := att.bitCount()
		if c > bestCount {
			best = att
			bestCount = c
		}
	}
	return copyPoolAttestation(best)
}

// GetScoredForBlock returns attestations scored and ranked for block
// inclusion at the given slot. Attestations are scored based on committee
// coverage (bit count) and inclusion delay (fresher is better).
func (p *AttestationPoolV2) GetScoredForBlock(slot Slot, maxCount int) []*ScoredAttestation {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if maxCount <= 0 {
		maxCount = MaxAttestationsPerBlock
	}

	var scored []*ScoredAttestation
	for dk, atts := range p.byDataKey {
		if _, incl := p.included[dk]; incl {
			continue
		}
		for _, att := range atts {
			delay := p.inclusionDelayLocked(att, slot)
			if delay < OptimalInclusionDelay || delay > p.config.MaxInclusionDelay {
				continue
			}
			bc := att.bitCount()
			s := p.scoreAttestationLocked(att, slot)
			scored = append(scored, &ScoredAttestation{
				Att:            copyPoolAttestation(att),
				Score:          s,
				InclusionDelay: delay,
				BitCount:       bc,
				IsAggregated:   bc > 1,
			})
		}
	}

	// Sort by score descending, break ties by inclusion delay ascending.
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].InclusionDelay < scored[j].InclusionDelay
	})

	if len(scored) > maxCount {
		scored = scored[:maxCount]
	}
	return scored
}

// MarkIncludedV2 marks attestation data as included at the given slot and
// records inclusion delay statistics.
func (p *AttestationPoolV2) MarkIncludedV2(att *PoolAttestation, inclusionSlot Slot) {
	if att == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	dk := att.dataKey()
	p.included[dk] = inclusionSlot

	// Compute and record inclusion delay.
	delay := uint64(0)
	if uint64(inclusionSlot) > uint64(att.Slot) {
		delay = uint64(inclusionSlot) - uint64(att.Slot)
	}
	p.delayStats.TotalAttestations++
	p.delayStats.TotalDelay += delay
	if delay < p.delayStats.MinDelay {
		p.delayStats.MinDelay = delay
	}
	if delay > p.delayStats.MaxDelay {
		p.delayStats.MaxDelay = delay
	}
	if delay == OptimalInclusionDelay {
		p.delayStats.OptimalCount++
	}

	// Remove from active indexes.
	if atts, ok := p.byDataKey[dk]; ok {
		p.total -= len(atts)
		delete(p.byDataKey, dk)
	}
	ck := committeeKey{Slot: att.Slot, CommitteeIndex: att.CommitteeIndex}
	delete(p.byCommittee, ck)
}

// GetDelayStats returns a copy of the current inclusion delay statistics.
func (p *AttestationPoolV2) GetDelayStats() InclusionDelayStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	stats := p.delayStats
	if stats.TotalAttestations == 0 {
		stats.MinDelay = 0
	}
	return stats
}

// Size returns the current number of attestations in the pool.
func (p *AttestationPoolV2) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.total
}

// CommitteeCount returns the number of distinct (slot, committee) pairs
// with pending attestations.
func (p *AttestationPoolV2) CommitteeCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.byCommittee)
}

// --- Internal methods (caller must hold p.mu) ---

// fingerprintLocked computes a unique fingerprint for deduplication.
// Combines the data key fields with the aggregation bits.
func (p *AttestationPoolV2) fingerprintLocked(att *PoolAttestation) types.Hash {
	// Build a deterministic byte sequence from the attestation identity.
	var buf []byte
	slotBytes := uint64ToBytes(uint64(att.Slot))
	buf = append(buf, slotBytes[:]...)
	ciBytes := uint64ToBytes(att.CommitteeIndex)
	buf = append(buf, ciBytes[:]...)
	buf = append(buf, att.BeaconBlockRoot[:]...)
	seBytes := uint64ToBytes(uint64(att.Source.Epoch))
	buf = append(buf, seBytes[:]...)
	buf = append(buf, att.Source.Root[:]...)
	teBytes := uint64ToBytes(uint64(att.Target.Epoch))
	buf = append(buf, teBytes[:]...)
	buf = append(buf, att.Target.Root[:]...)
	buf = append(buf, att.AggregationBits...)
	// Simple hash from XOR folding for fingerprint.
	var h types.Hash
	for i, b := range buf {
		h[i%32] ^= b
	}
	return h
}

// inclusionDelayLocked returns the inclusion delay for an attestation at
// the given block slot.
func (p *AttestationPoolV2) inclusionDelayLocked(att *PoolAttestation, blockSlot Slot) uint64 {
	if uint64(blockSlot) <= uint64(att.Slot) {
		return 0
	}
	return uint64(blockSlot) - uint64(att.Slot)
}

// scoreAttestationLocked computes a score for block-inclusion ranking.
// Score = bitCount * 100 - inclusionDelay * decay.
// Higher coverage and lower delay produce higher scores.
func (p *AttestationPoolV2) scoreAttestationLocked(att *PoolAttestation, blockSlot Slot) uint64 {
	bc := uint64(att.bitCount())
	delay := p.inclusionDelayLocked(att, blockSlot)
	score := bc * 100
	penalty := delay * AttestationScoreDecayPerSlot
	if penalty >= score {
		return 0
	}
	return score - penalty
}

// pruneLocked removes attestations beyond the inclusion window and stale
// inclusion records.
func (p *AttestationPoolV2) pruneLocked() {
	if p.currentSlot == 0 {
		return
	}
	var cutoff uint64
	if uint64(p.currentSlot) > p.config.MaxInclusionDelay {
		cutoff = uint64(p.currentSlot) - p.config.MaxInclusionDelay
	}

	// Prune byDataKey and byCommittee.
	for dk, atts := range p.byDataKey {
		if uint64(dk.Slot) < cutoff {
			p.total -= len(atts)
			delete(p.byDataKey, dk)
			ck := committeeKey{Slot: dk.Slot, CommitteeIndex: dk.CommitteeIndex}
			delete(p.byCommittee, ck)
		}
	}

	// Prune old inclusion records.
	for dk := range p.included {
		if uint64(dk.Slot) < cutoff {
			delete(p.included, dk)
		}
	}

	// Prune old seen fingerprints (keep pool bounded).
	// Since fingerprints are not indexed by slot, we clear them when the
	// pool is getting large. This is a trade-off: we may re-accept some
	// previously-seen attestations, but deduplication at the data-key
	// level still prevents true duplicates.
	if len(p.seen) > p.config.MaxPoolSize*2 {
		p.seen = make(map[types.Hash]bool, p.total)
	}
}

// uint64ToBytes converts a uint64 to an 8-byte little-endian representation.
func uint64ToBytes(v uint64) [8]byte {
	var b [8]byte
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
	b[4] = byte(v >> 32)
	b[5] = byte(v >> 40)
	b[6] = byte(v >> 48)
	b[7] = byte(v >> 56)
	return b
}
