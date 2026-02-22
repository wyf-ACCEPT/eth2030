// Package consensus - aggregate attestation handling for the beacon chain.
//
// Implements the AggregateAttestation container, an attestation aggregation
// pool with greedy aggregation, bitfield operations, and pruning. This
// complements the existing AttestationPool and EIP-7549 attestation
// handling with a dedicated aggregation pipeline that combines individual
// attestations into compact aggregate forms for efficient on-chain inclusion.
package consensus

import (
	"errors"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Aggregate attestation constants.
const (
	// DefaultMaxPerSlot is the default maximum number of aggregate
	// attestations tracked per slot.
	DefaultMaxPerSlot = 256

	// DefaultAggPoolMaxAge is the default maximum age (in slots) before
	// attestations are pruned from the aggregation pool.
	DefaultAggPoolMaxAge uint64 = 64

	// DefaultAggPoolMaxSlots is the default number of slots the aggregation
	// pool tracks simultaneously.
	DefaultAggPoolMaxSlots = 128
)

// Aggregate attestation errors.
var (
	ErrAggAttNil          = errors.New("agg_attestation: nil attestation")
	ErrAggAttNilData      = errors.New("agg_attestation: nil attestation data")
	ErrAggAttEmptyBits    = errors.New("agg_attestation: empty aggregation bits")
	ErrAggAttEmptySig     = errors.New("agg_attestation: empty signature")
	ErrAggAttDataMismatch = errors.New("agg_attestation: data mismatch for aggregation")
	ErrAggAttOverlapping  = errors.New("agg_attestation: overlapping aggregation bits")
	ErrAggAttSlotFull     = errors.New("agg_attestation: slot at capacity")
	ErrAggAttDuplicate    = errors.New("agg_attestation: duplicate attestation")
	ErrAggAttBitfieldLen  = errors.New("agg_attestation: bitfield length mismatch")
)

// AggregateAttestation represents an attestation that may aggregate
// signatures and participation bits from multiple individual attestations.
// The AggregationBits bitfield tracks which validators have contributed.
type AggregateAttestation struct {
	Data            AttestationData
	AggregationBits []byte   // bitfield of participating validators
	Signature       [96]byte // aggregate BLS signature
}

// aggAttDataKey is a hashable key for grouping attestations by data.
type aggAttDataKey struct {
	Slot            Slot
	BeaconBlockRoot types.Hash
	SourceEpoch     Epoch
	SourceRoot      types.Hash
	TargetEpoch     Epoch
	TargetRoot      types.Hash
}

// makeAggAttDataKey builds a hashable key from AttestationData.
func makeAggAttDataKey(data *AttestationData) aggAttDataKey {
	return aggAttDataKey{
		Slot:            data.Slot,
		BeaconBlockRoot: data.BeaconBlockRoot,
		SourceEpoch:     data.Source.Epoch,
		SourceRoot:      data.Source.Root,
		TargetEpoch:     data.Target.Epoch,
		TargetRoot:      data.Target.Root,
	}
}

// HashAggregateAttestation computes a hash of the aggregate attestation
// covering data, bits, and signature. Used for deduplication.
func HashAggregateAttestation(agg *AggregateAttestation) types.Hash {
	var buf []byte
	s := uint64(agg.Data.Slot)
	buf = append(buf, byte(s), byte(s>>8), byte(s>>16), byte(s>>24),
		byte(s>>32), byte(s>>40), byte(s>>48), byte(s>>56))
	buf = append(buf, agg.Data.BeaconBlockRoot[:]...)
	buf = append(buf, agg.Data.Source.Root[:]...)
	buf = append(buf, agg.Data.Target.Root[:]...)
	buf = append(buf, agg.AggregationBits...)
	buf = append(buf, agg.Signature[:]...)
	return crypto.Keccak256Hash(buf)
}

// AggregationPoolConfig configures the attestation aggregation pool.
type AggregationPoolConfig struct {
	MaxPerSlot int    // max aggregates tracked per slot
	MaxAge     uint64 // max slot age before pruning
}

// DefaultAggregationPoolConfig returns default configuration.
func DefaultAggregationPoolConfig() *AggregationPoolConfig {
	return &AggregationPoolConfig{
		MaxPerSlot: DefaultMaxPerSlot,
		MaxAge:     DefaultAggPoolMaxAge,
	}
}

// AggregationPool manages pending aggregate attestations organized by slot
// and attestation data. It supports adding individual attestations,
// pairwise aggregation, greedy aggregation, and pruning.
// All public methods are thread-safe.
type AggregationPool struct {
	mu sync.RWMutex

	config *AggregationPoolConfig

	// pendingBySlot maps slot -> (data key -> list of aggregates with that data).
	pendingBySlot map[Slot]map[aggAttDataKey][]*AggregateAttestation

	// slotCounts tracks total attestations per slot for capacity enforcement.
	slotCounts map[Slot]int
}

// NewAggregationPool creates a new attestation aggregation pool.
func NewAggregationPool(cfg *AggregationPoolConfig) *AggregationPool {
	if cfg == nil {
		cfg = DefaultAggregationPoolConfig()
	}
	return &AggregationPool{
		config:        cfg,
		pendingBySlot: make(map[Slot]map[aggAttDataKey][]*AggregateAttestation),
		slotCounts:    make(map[Slot]int),
	}
}

// AddAttestation adds an individual attestation to the pool. The attestation
// is wrapped into an AggregateAttestation (with a single participant) and
// either merged into an existing aggregate or stored as a new entry.
func (ap *AggregationPool) AddAttestation(att *Attestation) error {
	if att == nil {
		return ErrAggAttNil
	}
	if len(att.AggregationBits) == 0 {
		return ErrAggAttEmptyBits
	}
	emptySig := [96]byte{}
	if att.Signature == emptySig {
		return ErrAggAttEmptySig
	}

	agg := &AggregateAttestation{
		Data:            att.Data,
		AggregationBits: make([]byte, len(att.AggregationBits)),
		Signature:       att.Signature,
	}
	copy(agg.AggregationBits, att.AggregationBits)

	return ap.addAggregate(agg)
}

// addAggregate inserts an aggregate attestation into the pool, attempting
// to merge with existing aggregates first.
func (ap *AggregationPool) addAggregate(agg *AggregateAttestation) error {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	slot := agg.Data.Slot
	key := makeAggAttDataKey(&agg.Data)

	// Initialize slot maps if needed.
	if _, ok := ap.pendingBySlot[slot]; !ok {
		ap.pendingBySlot[slot] = make(map[aggAttDataKey][]*AggregateAttestation)
	}

	existing := ap.pendingBySlot[slot][key]

	// Try to merge with an existing aggregate with non-overlapping bits.
	for i, e := range existing {
		if !BitfieldOverlaps(e.AggregationBits, agg.AggregationBits) {
			merged := mergeAggregates(e, agg)
			existing[i] = merged
			ap.pendingBySlot[slot][key] = existing
			return nil
		}
	}

	// Check slot capacity.
	if ap.slotCounts[slot] >= ap.config.MaxPerSlot {
		return ErrAggAttSlotFull
	}

	// Store as a new entry.
	cp := copyAggregateAttestation(agg)
	ap.pendingBySlot[slot][key] = append(existing, cp)
	ap.slotCounts[slot]++
	return nil
}

// TryAggregate attempts to combine two aggregate attestations. They must
// have the same attestation data and non-overlapping aggregation bits.
// Returns the combined aggregate and true, or nil and false if not possible.
func TryAggregate(att1, att2 *AggregateAttestation) (*AggregateAttestation, bool) {
	if att1 == nil || att2 == nil {
		return nil, false
	}
	if !IsEqualAttestationData(&att1.Data, &att2.Data) {
		return nil, false
	}
	if BitfieldOverlaps(att1.AggregationBits, att2.AggregationBits) {
		return nil, false
	}
	return mergeAggregates(att1, att2), true
}

// mergeAggregates combines two aggregate attestations with compatible data.
// The resulting aggregate has OR'd bits and the first attestation's signature
// placeholder (in production, this would be an aggregated BLS signature).
func mergeAggregates(a, b *AggregateAttestation) *AggregateAttestation {
	bits := BitfieldOR(a.AggregationBits, b.AggregationBits)
	return &AggregateAttestation{
		Data:            a.Data,
		AggregationBits: bits,
		Signature:       a.Signature, // placeholder; real impl aggregates BLS sigs
	}
}

// AggregateAll performs greedy aggregation of all attestations for a given
// slot. For each distinct attestation data key, it sorts aggregates by
// participation count (descending) and greedily merges non-overlapping
// aggregates. Returns the resulting aggregate attestations.
func (ap *AggregationPool) AggregateAll(slot Slot) []*AggregateAttestation {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	slotMap, ok := ap.pendingBySlot[slot]
	if !ok {
		return nil
	}

	var result []*AggregateAttestation

	for key, atts := range slotMap {
		if len(atts) == 0 {
			continue
		}

		// Sort by participation count descending for greedy approach.
		sorted := make([]*AggregateAttestation, len(atts))
		copy(sorted, atts)
		sort.Slice(sorted, func(i, j int) bool {
			return CountBits(sorted[i].AggregationBits) > CountBits(sorted[j].AggregationBits)
		})

		// Greedy aggregation: start with the highest-participation aggregate
		// and merge in non-overlapping ones.
		aggregated := make([]*AggregateAttestation, 0, len(sorted))
		used := make([]bool, len(sorted))

		for i := 0; i < len(sorted); i++ {
			if used[i] {
				continue
			}
			current := copyAggregateAttestation(sorted[i])
			used[i] = true

			// Try to merge remaining non-overlapping aggregates.
			for j := i + 1; j < len(sorted); j++ {
				if used[j] {
					continue
				}
				if !BitfieldOverlaps(current.AggregationBits, sorted[j].AggregationBits) {
					current = mergeAggregates(current, sorted[j])
					used[j] = true
				}
			}
			aggregated = append(aggregated, current)
		}

		// Replace the slot entries with the aggregated results.
		slotMap[key] = aggregated
		result = append(result, aggregated...)
	}

	// Update the count for the slot.
	count := 0
	for _, atts := range slotMap {
		count += len(atts)
	}
	ap.slotCounts[slot] = count

	return result
}

// GetAggregates returns all current aggregate attestations for a slot.
func (ap *AggregationPool) GetAggregates(slot Slot) []*AggregateAttestation {
	ap.mu.RLock()
	defer ap.mu.RUnlock()

	slotMap, ok := ap.pendingBySlot[slot]
	if !ok {
		return nil
	}

	var result []*AggregateAttestation
	for _, atts := range slotMap {
		for _, agg := range atts {
			result = append(result, copyAggregateAttestation(agg))
		}
	}
	return result
}

// SlotCount returns the number of aggregate attestations tracked for a slot.
func (ap *AggregationPool) SlotCount(slot Slot) int {
	ap.mu.RLock()
	defer ap.mu.RUnlock()
	return ap.slotCounts[slot]
}

// TotalCount returns the total number of aggregate attestations in the pool.
func (ap *AggregationPool) TotalCount() int {
	ap.mu.RLock()
	defer ap.mu.RUnlock()
	total := 0
	for _, c := range ap.slotCounts {
		total += c
	}
	return total
}

// PruneOld removes all attestations for slots older than
// (currentSlot - maxAge). Thread-safe.
func (ap *AggregationPool) PruneOld(currentSlot Slot, maxAge uint64) {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	var cutoff uint64
	if uint64(currentSlot) > maxAge {
		cutoff = uint64(currentSlot) - maxAge
	}

	for slot := range ap.pendingBySlot {
		if uint64(slot) < cutoff {
			delete(ap.pendingBySlot, slot)
			delete(ap.slotCounts, slot)
		}
	}
}

// --- Bitfield operations ---

// BitfieldOR computes the bitwise OR of two bitfields.
// The result has the length of the longer input.
func BitfieldOR(a, b []byte) []byte {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	result := make([]byte, maxLen)
	copy(result, a)
	for i, v := range b {
		result[i] |= v
	}
	return result
}

// BitfieldOverlaps returns true if any bit position is set in both
// bitfields. Returns false if either bitfield is nil or empty.
func BitfieldOverlaps(a, b []byte) bool {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if a[i]&b[i] != 0 {
			return true
		}
	}
	return false
}

// BitfieldAND computes the bitwise AND of two bitfields.
// The result has the length of the shorter input.
func BitfieldAND(a, b []byte) []byte {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	result := make([]byte, minLen)
	for i := 0; i < minLen; i++ {
		result[i] = a[i] & b[i]
	}
	return result
}

// CountBits returns the number of set bits (population count) in the
// bitfield. This represents the number of participating validators.
func CountBits(bitfield []byte) int {
	count := 0
	for _, b := range bitfield {
		// Brian Kernighan's bit counting algorithm.
		v := b
		for v != 0 {
			count++
			v &= v - 1
		}
	}
	return count
}

// BitfieldEqual returns true if two bitfields are identical.
func BitfieldEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SetBit sets the bit at the given index in the bitfield.
// Grows the bitfield if necessary.
func SetBit(bitfield []byte, index int) []byte {
	byteIdx := index / 8
	bitIdx := uint(index % 8)
	for byteIdx >= len(bitfield) {
		bitfield = append(bitfield, 0)
	}
	bitfield[byteIdx] |= 1 << bitIdx
	return bitfield
}

// GetBit returns true if the bit at the given index is set.
func GetBit(bitfield []byte, index int) bool {
	byteIdx := index / 8
	if byteIdx >= len(bitfield) {
		return false
	}
	bitIdx := uint(index % 8)
	return bitfield[byteIdx]&(1<<bitIdx) != 0
}

// --- Helpers ---

// copyAggregateAttestation returns a deep copy of an AggregateAttestation.
func copyAggregateAttestation(agg *AggregateAttestation) *AggregateAttestation {
	cp := *agg
	cp.AggregationBits = make([]byte, len(agg.AggregationBits))
	copy(cp.AggregationBits, agg.AggregationBits)
	return &cp
}
