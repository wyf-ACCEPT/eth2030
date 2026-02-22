// Package consensus implements Ethereum consensus-layer primitives.
// This file implements the slashing detection engine, which detects
// proposer slashings (double proposals) and attester slashings
// (double votes and surround votes) per the beacon chain spec (phase0).

package consensus

import (
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Slashing detection constants.
const (
	// DefaultAttestationWindow is the default number of epochs of attestation
	// history to retain for surround vote detection.
	DefaultAttestationWindow uint64 = 256
)

// BlockRecord stores a registered block proposal for a given slot.
type BlockRecord struct {
	ProposerIndex ValidatorIndex
	Slot          Slot
	Root          types.Hash
}

// AttestationRecord stores a registered attestation for surround/double vote checks.
type AttestationRecord struct {
	ValidatorIndex ValidatorIndex
	SourceEpoch    Epoch
	TargetEpoch    Epoch
	TargetRoot     types.Hash
}

// ProposerSlashingEvidence contains proof of a double proposal: two distinct
// block headers from the same proposer for the same slot.
type ProposerSlashingEvidence struct {
	ProposerIndex ValidatorIndex
	Slot          Slot
	Root1         types.Hash
	Root2         types.Hash
}

// AttesterSlashingEvidence contains proof of a slashable attestation pair:
// either a double vote or a surround vote.
type AttesterSlashingEvidence struct {
	ValidatorIndex ValidatorIndex
	// Type indicates whether this is a "double_vote" or "surround_vote".
	Type           string
	Attestation1   AttestationRecord
	Attestation2   AttestationRecord
}

// SlashingDetectorConfig holds configuration for the slashing detector.
type SlashingDetectorConfig struct {
	// AttestationWindow is the number of epochs of attestation history to keep.
	// Older attestations are pruned to bound memory usage.
	AttestationWindow uint64
}

// DefaultSlashingDetectorConfig returns a sensible default config.
func DefaultSlashingDetectorConfig() SlashingDetectorConfig {
	return SlashingDetectorConfig{
		AttestationWindow: DefaultAttestationWindow,
	}
}

// slotKey uniquely identifies a (proposer, slot) pair for block tracking.
type slotKey struct {
	proposer ValidatorIndex
	slot     Slot
}

// SlashingDetector detects proposer and attester slashing conditions.
// It maintains registries of observed block proposals and attestations,
// checking for violations as new entries are registered.
type SlashingDetector struct {
	mu     sync.RWMutex
	config SlashingDetectorConfig

	// blocks maps (proposer, slot) -> list of distinct roots seen.
	blocks map[slotKey][]types.Hash

	// attestations maps validatorIndex -> list of attestation records.
	attestations map[ValidatorIndex][]AttestationRecord

	// Accumulated evidence detected on RegisterBlock/RegisterAttestation.
	proposerEvidence []*ProposerSlashingEvidence
	attesterEvidence []*AttesterSlashingEvidence
}

// NewSlashingDetector creates a new slashing detector with the given config.
func NewSlashingDetector(config SlashingDetectorConfig) *SlashingDetector {
	if config.AttestationWindow == 0 {
		config.AttestationWindow = DefaultAttestationWindow
	}
	return &SlashingDetector{
		config:       config,
		blocks:       make(map[slotKey][]types.Hash),
		attestations: make(map[ValidatorIndex][]AttestationRecord),
	}
}

// RegisterBlock records a block proposal and checks for double proposals.
// If the same proposer has already proposed a different block for this slot,
// proposer slashing evidence is generated.
func (sd *SlashingDetector) RegisterBlock(proposerIndex ValidatorIndex, slot Slot, root types.Hash) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	key := slotKey{proposer: proposerIndex, slot: slot}
	existing := sd.blocks[key]

	// Check for duplicate root (same block, no slashing).
	for _, r := range existing {
		if r == root {
			return // Already registered this exact block.
		}
	}

	// If there are existing roots with different values, this is a double proposal.
	for _, r := range existing {
		sd.proposerEvidence = append(sd.proposerEvidence, &ProposerSlashingEvidence{
			ProposerIndex: proposerIndex,
			Slot:          slot,
			Root1:         r,
			Root2:         root,
		})
	}

	sd.blocks[key] = append(existing, root)
}

// RegisterAttestation records an attestation and checks for double votes
// and surround votes against existing attestation history.
func (sd *SlashingDetector) RegisterAttestation(
	validatorIndex ValidatorIndex,
	sourceEpoch Epoch,
	targetEpoch Epoch,
	targetRoot types.Hash,
) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	newAtt := AttestationRecord{
		ValidatorIndex: validatorIndex,
		SourceEpoch:    sourceEpoch,
		TargetEpoch:    targetEpoch,
		TargetRoot:     targetRoot,
	}

	history := sd.attestations[validatorIndex]

	for _, existing := range history {
		// Double vote: same target epoch, different target root.
		// Per spec: data_1 != data_2 and data_1.target.epoch == data_2.target.epoch
		if existing.TargetEpoch == targetEpoch && existing.TargetRoot != targetRoot {
			sd.attesterEvidence = append(sd.attesterEvidence, &AttesterSlashingEvidence{
				ValidatorIndex: validatorIndex,
				Type:           "double_vote",
				Attestation1:   existing,
				Attestation2:   newAtt,
			})
		}

		// Surround vote check (both directions):
		// Per spec: data_1.source.epoch < data_2.source.epoch AND
		//           data_2.target.epoch < data_1.target.epoch
		//
		// Case 1: existing surrounds new
		//   existing.source < new.source AND new.target < existing.target
		if existing.SourceEpoch < sourceEpoch && targetEpoch < existing.TargetEpoch {
			sd.attesterEvidence = append(sd.attesterEvidence, &AttesterSlashingEvidence{
				ValidatorIndex: validatorIndex,
				Type:           "surround_vote",
				Attestation1:   existing,
				Attestation2:   newAtt,
			})
		}

		// Case 2: new surrounds existing
		//   new.source < existing.source AND existing.target < new.target
		if sourceEpoch < existing.SourceEpoch && existing.TargetEpoch < targetEpoch {
			sd.attesterEvidence = append(sd.attesterEvidence, &AttesterSlashingEvidence{
				ValidatorIndex: validatorIndex,
				Type:           "surround_vote",
				Attestation1:   newAtt,
				Attestation2:   existing,
			})
		}
	}

	// Add to history.
	sd.attestations[validatorIndex] = append(history, newAtt)

	// Prune old attestations beyond the window.
	sd.pruneAttestations(validatorIndex, targetEpoch)
}

// pruneAttestations removes attestations that are older than the window.
// Must be called with the lock held.
func (sd *SlashingDetector) pruneAttestations(validatorIndex ValidatorIndex, currentTargetEpoch Epoch) {
	history := sd.attestations[validatorIndex]
	if len(history) == 0 {
		return
	}
	cutoff := uint64(0)
	if uint64(currentTargetEpoch) > sd.config.AttestationWindow {
		cutoff = uint64(currentTargetEpoch) - sd.config.AttestationWindow
	}

	// Keep only attestations with target epoch >= cutoff.
	n := 0
	for _, att := range history {
		if uint64(att.TargetEpoch) >= cutoff {
			history[n] = att
			n++
		}
	}
	sd.attestations[validatorIndex] = history[:n]
}

// DetectProposerSlashing returns all proposer slashing evidence detected so far
// and clears the internal evidence buffer.
func (sd *SlashingDetector) DetectProposerSlashing() []*ProposerSlashingEvidence {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	evidence := sd.proposerEvidence
	sd.proposerEvidence = nil
	return evidence
}

// DetectAttesterSlashing returns all attester slashing evidence detected so far
// and clears the internal evidence buffer.
func (sd *SlashingDetector) DetectAttesterSlashing() []*AttesterSlashingEvidence {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	evidence := sd.attesterEvidence
	sd.attesterEvidence = nil
	return evidence
}

// PeekProposerSlashing returns proposer slashing evidence without consuming it.
func (sd *SlashingDetector) PeekProposerSlashing() []*ProposerSlashingEvidence {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	result := make([]*ProposerSlashingEvidence, len(sd.proposerEvidence))
	copy(result, sd.proposerEvidence)
	return result
}

// PeekAttesterSlashing returns attester slashing evidence without consuming it.
func (sd *SlashingDetector) PeekAttesterSlashing() []*AttesterSlashingEvidence {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	result := make([]*AttesterSlashingEvidence, len(sd.attesterEvidence))
	copy(result, sd.attesterEvidence)
	return result
}

// BlockCount returns the total number of unique (proposer, slot, root) entries.
func (sd *SlashingDetector) BlockCount() int {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	count := 0
	for _, roots := range sd.blocks {
		count += len(roots)
	}
	return count
}

// AttestationCount returns the total number of attestation records being tracked.
func (sd *SlashingDetector) AttestationCount() int {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	count := 0
	for _, atts := range sd.attestations {
		count += len(atts)
	}
	return count
}

// ValidatorsWithAttestations returns a sorted list of validator indices that
// have at least one attestation record.
func (sd *SlashingDetector) ValidatorsWithAttestations() []ValidatorIndex {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	indices := make([]ValidatorIndex, 0, len(sd.attestations))
	for idx := range sd.attestations {
		if len(sd.attestations[idx]) > 0 {
			indices = append(indices, idx)
		}
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	return indices
}
