// Package consensus - equivocation detection for the beacon chain.
//
// Detects DoubleProposal, DoubleVote, and SurroundVote violations.
// Maintains in-memory tracking and generates evidence for slashing.
// Complements the existing SlashingDetector with typed evidence.
package consensus

import (
	"errors"
	"sort"
	"sync"
)

// EquivocationType categorizes the kind of equivocation detected.
type EquivocationType uint8

const (
	// DoubleProposal indicates two distinct block proposals for the same slot
	// by the same validator.
	DoubleProposal EquivocationType = iota

	// DoubleVote indicates two attestations for different targets in the
	// same epoch by the same validator.
	DoubleVote7

	// SurroundVote indicates one attestation's source-target range encloses
	// or is enclosed by another attestation from the same validator.
	SurroundVote7
)

// String returns a human-readable name for the equivocation type.
func (et EquivocationType) String() string {
	switch et {
	case DoubleProposal:
		return "double_proposal"
	case DoubleVote7:
		return "double_vote"
	case SurroundVote7:
		return "surround_vote"
	default:
		return "unknown"
	}
}

// Equivocation detector constants.
const (
	// DefaultEquivocationWindow is the number of epochs of attestation
	// history retained for surround vote detection.
	DefaultEquivocationWindow uint64 = 256

	// DefaultMaxPendingSlashings is the max number of pending slashing
	// evidence entries before the oldest are dropped.
	DefaultMaxPendingSlashings = 1024

	// DefaultProposalRetentionSlots is the number of slots of proposal
	// history retained for double-proposal detection.
	DefaultProposalRetentionSlots uint64 = 4096
)

// Equivocation detector errors.
var (
	ErrEquivNilAttestation = errors.New("equivocation: nil attestation data")
	ErrEquivNilDetector    = errors.New("equivocation: detector not initialized")
)

// EquivocationEvidence contains proof of an equivocation violation,
// including the type, the offending validator, and hashes of the two
// conflicting pieces of evidence.
type EquivocationEvidence struct {
	Type           EquivocationType
	ValidatorIndex ValidatorIndex
	Slot           Slot   // slot where the equivocation was detected
	Evidence1Hash  [32]byte
	Evidence2Hash  [32]byte
	// For attestation equivocations, store source/target info.
	Source1 Epoch
	Target1 Epoch
	Source2 Epoch
	Target2 Epoch
}

// proposalRecord tracks a block proposal by a validator at a slot.
type proposalRecord struct {
	validatorIndex ValidatorIndex
	slot           Slot
	blockHash      [32]byte
}

// attestationHistoryRecord tracks a single attestation's source/target
// epochs for a validator.
type attestationHistoryRecord struct {
	sourceEpoch Epoch
	targetEpoch Epoch
	dataHash    [32]byte // hash of the full attestation data
}

// EquivocationDetectorConfig configures the equivocation detector.
type EquivocationDetectorConfig struct {
	AttestationWindow      uint64 // epochs of attestation history to retain
	MaxPendingSlashings    int    // max pending evidence entries
	ProposalRetentionSlots uint64 // slots of proposal history to retain
}

// DefaultEquivocationDetectorConfig returns default configuration.
func DefaultEquivocationDetectorConfig() *EquivocationDetectorConfig {
	return &EquivocationDetectorConfig{
		AttestationWindow:      DefaultEquivocationWindow,
		MaxPendingSlashings:    DefaultMaxPendingSlashings,
		ProposalRetentionSlots: DefaultProposalRetentionSlots,
	}
}

// EquivocationDetector detects equivocation violations (double proposals,
// double votes, and surround votes) by maintaining a history of observed
// proposals and attestations per validator. All public methods are
// thread-safe.
type EquivocationDetector struct {
	mu sync.RWMutex

	config *EquivocationDetectorConfig

	// seenProposals maps (slot, validatorIndex) -> list of block hashes.
	seenProposals map[equivProposalKey][]proposalRecord

	// attestationHistory maps validatorIndex -> list of attestation records.
	attestationHistory map[ValidatorIndex][]attestationHistoryRecord

	// pendingSlashings is the ordered list of detected equivocations.
	pendingSlashings []*EquivocationEvidence

	// processedCount tracks total equivocations detected (lifetime).
	processedCount uint64
}

// equivProposalKey uniquely identifies a (slot, validator) pair.
type equivProposalKey struct {
	slot           Slot
	validatorIndex ValidatorIndex
}

// NewEquivocationDetector creates a new equivocation detector.
func NewEquivocationDetector(cfg *EquivocationDetectorConfig) *EquivocationDetector {
	if cfg == nil {
		cfg = DefaultEquivocationDetectorConfig()
	}
	return &EquivocationDetector{
		config:             cfg,
		seenProposals:      make(map[equivProposalKey][]proposalRecord),
		attestationHistory: make(map[ValidatorIndex][]attestationHistoryRecord),
		pendingSlashings:   make([]*EquivocationEvidence, 0),
	}
}

// CheckProposal checks a block proposal for double-proposal equivocation.
// If the same validator has already proposed a different block at the same
// slot, evidence is generated and returned. Returns nil if no equivocation.
func (d *EquivocationDetector) CheckProposal(
	slot Slot,
	proposerIdx ValidatorIndex,
	blockHash [32]byte,
) *EquivocationEvidence {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := equivProposalKey{slot: slot, validatorIndex: proposerIdx}
	existing := d.seenProposals[key]

	// Check for duplicate (same block hash).
	for _, rec := range existing {
		if rec.blockHash == blockHash {
			return nil // already seen this exact proposal
		}
	}

	// Record the new proposal.
	newRec := proposalRecord{
		validatorIndex: proposerIdx,
		slot:           slot,
		blockHash:      blockHash,
	}
	d.seenProposals[key] = append(existing, newRec)

	// If there were previous proposals with different hashes, this is a
	// double proposal.
	if len(existing) > 0 {
		evidence := &EquivocationEvidence{
			Type:           DoubleProposal,
			ValidatorIndex: proposerIdx,
			Slot:           slot,
			Evidence1Hash:  existing[0].blockHash,
			Evidence2Hash:  blockHash,
		}
		d.addEvidenceLocked(evidence)
		return evidence
	}

	return nil
}

// CheckAttestation checks an attestation for double-vote and surround-vote
// equivocations. Compares the new attestation against the validator's
// attestation history. Returns the first equivocation found, or nil.
func (d *EquivocationDetector) CheckAttestation(
	att *AttestationData,
	validatorIdx ValidatorIndex,
) *EquivocationEvidence {
	if att == nil {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Compute a hash of the attestation data for evidence tracking.
	dataHash := hashAttestationData(att)

	history := d.attestationHistory[validatorIdx]

	for _, prev := range history {
		// Skip if identical attestation.
		if prev.dataHash == dataHash {
			continue
		}

		// Double vote: same target epoch, different data.
		if prev.targetEpoch == att.Target.Epoch && prev.dataHash != dataHash {
			evidence := &EquivocationEvidence{
				Type:           DoubleVote7,
				ValidatorIndex: validatorIdx,
				Slot:           att.Slot,
				Evidence1Hash:  prev.dataHash,
				Evidence2Hash:  dataHash,
				Source1:        prev.sourceEpoch,
				Target1:        prev.targetEpoch,
				Source2:        att.Source.Epoch,
				Target2:        att.Target.Epoch,
			}
			d.addEvidenceLocked(evidence)

			// Still add to history before returning.
			d.addAttestationToHistoryLocked(validatorIdx, att.Source.Epoch, att.Target.Epoch, dataHash)
			return evidence
		}

		// Surround vote check.
		if IsSurroundVoteCheck(prev.sourceEpoch, prev.targetEpoch, att.Source.Epoch, att.Target.Epoch) {
			evidence := &EquivocationEvidence{
				Type:           SurroundVote7,
				ValidatorIndex: validatorIdx,
				Slot:           att.Slot,
				Evidence1Hash:  prev.dataHash,
				Evidence2Hash:  dataHash,
				Source1:        prev.sourceEpoch,
				Target1:        prev.targetEpoch,
				Source2:        att.Source.Epoch,
				Target2:        att.Target.Epoch,
			}
			d.addEvidenceLocked(evidence)

			d.addAttestationToHistoryLocked(validatorIdx, att.Source.Epoch, att.Target.Epoch, dataHash)
			return evidence
		}
	}

	// No equivocation found; add to history.
	d.addAttestationToHistoryLocked(validatorIdx, att.Source.Epoch, att.Target.Epoch, dataHash)
	return nil
}

// IsSurroundVoteCheck checks whether two attestations form a surround vote.
// A surround vote occurs when:
//   - source1 < source2 AND target2 < target1 (att1 surrounds att2), or
//   - source2 < source1 AND target1 < target2 (att2 surrounds att1)
func IsSurroundVoteCheck(source1, target1, source2, target2 Epoch) bool {
	// Case 1: first surrounds second.
	if source1 < source2 && target2 < target1 {
		return true
	}
	// Case 2: second surrounds first.
	if source2 < source1 && target1 < target2 {
		return true
	}
	return false
}

// ProcessSlashableEvidence manually adds an evidence entry. Used when
// evidence is received from the network.
func (d *EquivocationDetector) ProcessSlashableEvidence(evidence *EquivocationEvidence) {
	if evidence == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.addEvidenceLocked(evidence)
}

// GetPendingSlashings returns all pending equivocation evidence and
// clears the pending buffer.
func (d *EquivocationDetector) GetPendingSlashings() []*EquivocationEvidence {
	d.mu.Lock()
	defer d.mu.Unlock()

	result := make([]*EquivocationEvidence, len(d.pendingSlashings))
	copy(result, d.pendingSlashings)
	d.pendingSlashings = d.pendingSlashings[:0]
	return result
}

// PeekPendingSlashings returns pending evidence without consuming it.
func (d *EquivocationDetector) PeekPendingSlashings() []*EquivocationEvidence {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]*EquivocationEvidence, len(d.pendingSlashings))
	copy(result, d.pendingSlashings)
	return result
}

// PendingCount returns the number of pending equivocation evidence entries.
func (d *EquivocationDetector) PendingCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.pendingSlashings)
}

// ProcessedCount returns the lifetime count of detected equivocations.
func (d *EquivocationDetector) ProcessedCount() uint64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.processedCount
}

// TrackedValidatorCount returns the number of validators with attestation
// history being tracked.
func (d *EquivocationDetector) TrackedValidatorCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.attestationHistory)
}

// TrackedProposalCount returns the number of (slot, validator) pairs
// being tracked for double-proposal detection.
func (d *EquivocationDetector) TrackedProposalCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.seenProposals)
}

// PruneOld removes old tracking data to bound memory usage.
// For proposals: removes entries for slots older than
// (currentSlot - ProposalRetentionSlots).
// For attestations: removes entries for target epochs older than
// (currentEpoch - AttestationWindow).
func (d *EquivocationDetector) PruneOld(currentSlot Slot) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Prune proposals.
	var proposalCutoff uint64
	if uint64(currentSlot) > d.config.ProposalRetentionSlots {
		proposalCutoff = uint64(currentSlot) - d.config.ProposalRetentionSlots
	}
	for key := range d.seenProposals {
		if uint64(key.slot) < proposalCutoff {
			delete(d.seenProposals, key)
		}
	}

	// Prune attestation history.
	// Derive the current epoch assuming 32 slots/epoch (reasonable default).
	currentEpoch := uint64(currentSlot) / 32
	var attCutoff uint64
	if currentEpoch > d.config.AttestationWindow {
		attCutoff = currentEpoch - d.config.AttestationWindow
	}

	for idx, history := range d.attestationHistory {
		n := 0
		for _, rec := range history {
			if uint64(rec.targetEpoch) >= attCutoff {
				history[n] = rec
				n++
			}
		}
		if n == 0 {
			delete(d.attestationHistory, idx)
		} else {
			d.attestationHistory[idx] = history[:n]
		}
	}
}

// GetSlashingsByType returns all pending evidence filtered by type.
func (d *EquivocationDetector) GetSlashingsByType(eqType EquivocationType) []*EquivocationEvidence {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var result []*EquivocationEvidence
	for _, ev := range d.pendingSlashings {
		if ev.Type == eqType {
			result = append(result, ev)
		}
	}
	return result
}

// GetSlashedValidators returns a sorted list of unique validator indices
// that have pending equivocation evidence.
func (d *EquivocationDetector) GetSlashedValidators() []ValidatorIndex {
	d.mu.RLock()
	defer d.mu.RUnlock()

	seen := make(map[ValidatorIndex]bool)
	for _, ev := range d.pendingSlashings {
		seen[ev.ValidatorIndex] = true
	}

	result := make([]ValidatorIndex, 0, len(seen))
	for idx := range seen {
		result = append(result, idx)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// --- Internal helpers ---

// addEvidenceLocked adds evidence to the pending buffer, evicting the
// oldest if at capacity. Must be called with d.mu held.
func (d *EquivocationDetector) addEvidenceLocked(ev *EquivocationEvidence) {
	if len(d.pendingSlashings) >= d.config.MaxPendingSlashings {
		// Evict the oldest entry.
		d.pendingSlashings = d.pendingSlashings[1:]
	}
	d.pendingSlashings = append(d.pendingSlashings, ev)
	d.processedCount++
}

// addAttestationToHistoryLocked appends an attestation record and prunes
// old entries beyond the window. Must be called with d.mu held.
func (d *EquivocationDetector) addAttestationToHistoryLocked(
	idx ValidatorIndex,
	sourceEpoch, targetEpoch Epoch,
	dataHash [32]byte,
) {
	rec := attestationHistoryRecord{
		sourceEpoch: sourceEpoch,
		targetEpoch: targetEpoch,
		dataHash:    dataHash,
	}
	d.attestationHistory[idx] = append(d.attestationHistory[idx], rec)

	// Prune entries beyond the window for this validator.
	var cutoff uint64
	if uint64(targetEpoch) > d.config.AttestationWindow {
		cutoff = uint64(targetEpoch) - d.config.AttestationWindow
	}
	history := d.attestationHistory[idx]
	n := 0
	for _, h := range history {
		if uint64(h.targetEpoch) >= cutoff {
			history[n] = h
			n++
		}
	}
	d.attestationHistory[idx] = history[:n]
}

// hashAttestationData computes a deterministic hash of attestation data.
func hashAttestationData(att *AttestationData) [32]byte {
	var buf []byte
	s := uint64(att.Slot)
	buf = append(buf, byte(s), byte(s>>8), byte(s>>16), byte(s>>24),
		byte(s>>32), byte(s>>40), byte(s>>48), byte(s>>56))
	buf = append(buf, att.BeaconBlockRoot[:]...)
	se := uint64(att.Source.Epoch)
	buf = append(buf, byte(se), byte(se>>8), byte(se>>16), byte(se>>24),
		byte(se>>32), byte(se>>40), byte(se>>48), byte(se>>56))
	buf = append(buf, att.Source.Root[:]...)
	te := uint64(att.Target.Epoch)
	buf = append(buf, byte(te), byte(te>>8), byte(te>>16), byte(te>>24),
		byte(te>>32), byte(te>>40), byte(te>>48), byte(te>>56))
	buf = append(buf, att.Target.Root[:]...)

	// Use a simple hash for the data.
	h := [32]byte{}
	copy(h[:], buf)
	if len(buf) > 32 {
		// XOR fold for determinism.
		for i := 32; i < len(buf); i++ {
			h[i%32] ^= buf[i]
		}
	}
	return h
}
