// finality_equivocation_detector.go detects validators who cast conflicting
// finality votes (double voting) within the same slot. Unlike the general
// EquivocationDetector which handles proposals and attestations, this
// detector is specialized for the block finalization pipeline and operates
// on BlockFinalityVote objects, providing evidence for slashing validators
// who vote for multiple conflicting block roots in the same finality round.
// This addresses Gap #6 (Fast L1 Finality) by adding equivocation detection
// to the finality vote pipeline.
package consensus

import (
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// FinalityEquivocationEvidence contains proof that a validator voted for
// two different block roots in the same slot.
type FinalityEquivocationEvidence struct {
	ValidatorIndex uint64
	Slot           uint64
	VoteRoot1      types.Hash
	VoteSig1       []byte
	VoteRoot2      types.Hash
	VoteSig2       []byte
	DetectedAt     int64 // unix ms
}

// finalityVoteRecord is an internal record of a validator's finality vote.
type finalityVoteRecord struct {
	blockRoot types.Hash
	signature []byte
}

// slotVoteKey uniquely identifies a (slot, validator) pair.
type slotVoteKey struct {
	slot           uint64
	validatorIndex uint64
}

// FinalityEquivocationDetector detects double voting in the finality
// pipeline. It maintains a history of votes per slot and generates
// evidence when a validator votes for conflicting block roots.
// All public methods are thread-safe.
type FinalityEquivocationDetector struct {
	mu sync.RWMutex

	maxSlotHistory uint64

	// votes maps (slot, validator) -> first recorded vote.
	votes map[slotVoteKey]*finalityVoteRecord

	// evidence maps validator -> list of equivocation evidence.
	evidence map[uint64][]*FinalityEquivocationEvidence

	// slotEvidence maps slot -> list of equivocation evidence.
	slotEvidence map[uint64][]*FinalityEquivocationEvidence

	// totalEvidence is the lifetime count of evidence generated.
	totalEvidence int
}

// NewFinalityEquivocationDetector creates a new detector with the given
// maximum slot history retention.
func NewFinalityEquivocationDetector(maxSlotHistory uint64) *FinalityEquivocationDetector {
	if maxSlotHistory == 0 {
		maxSlotHistory = 256
	}
	return &FinalityEquivocationDetector{
		maxSlotHistory: maxSlotHistory,
		votes:          make(map[slotVoteKey]*finalityVoteRecord),
		evidence:       make(map[uint64][]*FinalityEquivocationEvidence),
		slotEvidence:   make(map[uint64][]*FinalityEquivocationEvidence),
	}
}

// RecordVote records a finality vote and checks for equivocation. If the
// validator has already voted for a different block root in the same slot,
// evidence is generated and returned. Returns nil if no equivocation.
func (d *FinalityEquivocationDetector) RecordVote(vote *BlockFinalityVote) *FinalityEquivocationEvidence {
	if vote == nil {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	key := slotVoteKey{
		slot:           vote.Slot,
		validatorIndex: vote.ValidatorIndex,
	}

	existing, ok := d.votes[key]
	if !ok {
		// First vote from this validator for this slot.
		sig := make([]byte, len(vote.Signature))
		copy(sig, vote.Signature)
		d.votes[key] = &finalityVoteRecord{
			blockRoot: vote.BlockRoot,
			signature: sig,
		}
		return nil
	}

	// Same root is not equivocation.
	if existing.blockRoot == vote.BlockRoot {
		return nil
	}

	// Different root: equivocation detected.
	sig2 := make([]byte, len(vote.Signature))
	copy(sig2, vote.Signature)

	ev := &FinalityEquivocationEvidence{
		ValidatorIndex: vote.ValidatorIndex,
		Slot:           vote.Slot,
		VoteRoot1:      existing.blockRoot,
		VoteSig1:       existing.signature,
		VoteRoot2:      vote.BlockRoot,
		VoteSig2:       sig2,
		DetectedAt:     vote.Timestamp,
	}

	d.evidence[vote.ValidatorIndex] = append(d.evidence[vote.ValidatorIndex], ev)
	d.slotEvidence[vote.Slot] = append(d.slotEvidence[vote.Slot], ev)
	d.totalEvidence++

	return ev
}

// GetEvidence returns all equivocation evidence for a given validator.
func (d *FinalityEquivocationDetector) GetEvidence(validatorIndex uint64) []*FinalityEquivocationEvidence {
	d.mu.RLock()
	defer d.mu.RUnlock()

	evs := d.evidence[validatorIndex]
	if len(evs) == 0 {
		return nil
	}

	result := make([]*FinalityEquivocationEvidence, len(evs))
	copy(result, evs)
	return result
}

// GetSlotEvidence returns all equivocation evidence for a given slot.
func (d *FinalityEquivocationDetector) GetSlotEvidence(slot uint64) []*FinalityEquivocationEvidence {
	d.mu.RLock()
	defer d.mu.RUnlock()

	evs := d.slotEvidence[slot]
	if len(evs) == 0 {
		return nil
	}

	result := make([]*FinalityEquivocationEvidence, len(evs))
	copy(result, evs)
	return result
}

// PruneOldSlots removes votes and evidence for slots older than
// (currentSlot - maxSlotHistory).
func (d *FinalityEquivocationDetector) PruneOldSlots(currentSlot uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if currentSlot <= d.maxSlotHistory {
		return
	}
	cutoff := currentSlot - d.maxSlotHistory

	// Remove old votes.
	for key := range d.votes {
		if key.slot < cutoff {
			delete(d.votes, key)
		}
	}

	// Remove old slot evidence.
	for slot := range d.slotEvidence {
		if slot < cutoff {
			delete(d.slotEvidence, slot)
		}
	}

	// Rebuild per-validator evidence without pruned slots.
	for valIdx, evs := range d.evidence {
		n := 0
		for _, ev := range evs {
			if ev.Slot >= cutoff {
				evs[n] = ev
				n++
			}
		}
		if n == 0 {
			delete(d.evidence, valIdx)
		} else {
			d.evidence[valIdx] = evs[:n]
		}
	}
}

// EvidenceCount returns the total number of equivocation evidence entries
// generated over the detector's lifetime.
func (d *FinalityEquivocationDetector) EvidenceCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.totalEvidence
}

// SlashableValidators returns a sorted list of validator indices that have
// equivocation evidence at the given slot.
func (d *FinalityEquivocationDetector) SlashableValidators(slot uint64) []uint64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	evs := d.slotEvidence[slot]
	if len(evs) == 0 {
		return nil
	}

	seen := make(map[uint64]bool)
	for _, ev := range evs {
		seen[ev.ValidatorIndex] = true
	}

	result := make([]uint64, 0, len(seen))
	for idx := range seen {
		result = append(result, idx)
	}

	// Sort for determinism.
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j] < result[i] {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

// VerifyEvidence checks that a piece of equivocation evidence is internally
// consistent: the two vote roots must differ and the validator/slot must
// match.
func (d *FinalityEquivocationDetector) VerifyEvidence(ev *FinalityEquivocationEvidence) bool {
	if ev == nil {
		return false
	}
	// The two roots must be different for it to be a valid equivocation.
	if ev.VoteRoot1 == ev.VoteRoot2 {
		return false
	}
	// Must have non-zero slot (slot 0 is genesis, no finality votes).
	if ev.Slot == 0 {
		return false
	}
	return true
}

// TrackedVoteCount returns the number of (slot, validator) vote entries
// currently tracked.
func (d *FinalityEquivocationDetector) TrackedVoteCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.votes)
}
