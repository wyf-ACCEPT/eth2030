package consensus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// SSFEngine implements the single-slot finality consensus engine.
// It tracks attestations per slot and determines finality based on
// a configurable stake-weight threshold (default 2/3 of total stake).
//
// Thread-safe: all public methods are protected by sync.RWMutex.

// SSFEngine errors.
var (
	ErrSSFEngineNilAttestation  = errors.New("ssf-engine: nil attestation")
	ErrSSFEngineSlotFinalized   = errors.New("ssf-engine: slot already finalized")
	ErrSSFEngineDuplicateVote   = errors.New("ssf-engine: duplicate attestation from validator")
	ErrSSFEngineUnknownValidator = errors.New("ssf-engine: unknown validator (no weight)")
	ErrSSFEngineInvalidConfig   = errors.New("ssf-engine: invalid configuration")
	ErrSSFEngineZeroStake       = errors.New("ssf-engine: total stake is zero")
)

// Default finality threshold: 2/3 supermajority.
const (
	DefaultFinalityThresholdNum uint64 = 2
	DefaultFinalityThresholdDen uint64 = 3
	DefaultMaxSlotHistory       int    = 256
)

// SSFEngineConfig holds configuration for the SSF engine.
type SSFEngineConfig struct {
	// FinalityThreshold expressed as Num/Den (default 2/3).
	FinalityThresholdNum uint64
	FinalityThresholdDen uint64

	// MaxSlotHistory is the number of finalized slot decisions to retain.
	MaxSlotHistory int

	// TotalStake is the total active stake in the system.
	TotalStake uint64
}

// DefaultSSFEngineConfig returns a sensible default configuration.
func DefaultSSFEngineConfig() SSFEngineConfig {
	return SSFEngineConfig{
		FinalityThresholdNum: DefaultFinalityThresholdNum,
		FinalityThresholdDen: DefaultFinalityThresholdDen,
		MaxSlotHistory:       DefaultMaxSlotHistory,
		TotalStake:           32_000_000 * GweiPerETH,
	}
}

// SSFAttestation represents a validator's attestation for SSF.
type SSFAttestation struct {
	Slot           uint64
	ValidatorIndex uint64
	SourceEpoch    uint64
	TargetRoot     types.Hash
	Signature      [96]byte
}

// FinalityResult reports whether a slot has achieved finality.
type FinalityResult struct {
	Slot        uint64
	IsFinalized bool
	VoteCount   int
	StakeWeight uint64
	Threshold   uint64 // stake required for finality
}

// VotingStatus reports the current voting state for a slot.
type VotingStatus struct {
	TotalVotes    int
	TotalStake    uint64
	RequiredStake uint64
	Participation float64 // 0.0 - 100.0
}

// slotRecord tracks attestations and finality for a single slot.
type slotRecord struct {
	attestations map[uint64]*SSFAttestation // validator index -> attestation
	stakeByRoot  map[types.Hash]uint64
	totalStake   uint64
	finalized    bool
	finalRoot    types.Hash
}

func newSlotRecord() *slotRecord {
	return &slotRecord{
		attestations: make(map[uint64]*SSFAttestation),
		stakeByRoot:  make(map[types.Hash]uint64),
	}
}

// SSFEngine is the main SSF consensus engine.
type SSFEngine struct {
	mu sync.RWMutex

	config           SSFEngineConfig
	validatorWeights map[uint64]uint64 // validator index -> stake weight

	// Active slot tracking.
	slots map[uint64]*slotRecord

	// Finality history (LRU-style bounded cache).
	historySlots []uint64              // ordered list of finalized slots
	historyMap   map[uint64]*slotRecord // slot -> finality record
}

// NewSSFEngine creates a new SSF engine with the given configuration.
// Returns nil if the configuration is invalid.
func NewSSFEngine(config SSFEngineConfig) *SSFEngine {
	if config.FinalityThresholdDen == 0 || config.FinalityThresholdNum == 0 {
		return nil
	}
	if config.MaxSlotHistory <= 0 {
		config.MaxSlotHistory = DefaultMaxSlotHistory
	}

	return &SSFEngine{
		config:           config,
		validatorWeights: make(map[uint64]uint64),
		slots:            make(map[uint64]*slotRecord),
		historySlots:     make([]uint64, 0),
		historyMap:       make(map[uint64]*slotRecord),
	}
}

// SetValidatorWeights replaces the validator weight map.
func (e *SSFEngine) SetValidatorWeights(weights map[uint64]uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.validatorWeights = make(map[uint64]uint64, len(weights))
	for k, v := range weights {
		e.validatorWeights[k] = v
	}
}

// ProcessAttestation processes an SSF attestation from a validator.
func (e *SSFEngine) ProcessAttestation(att *SSFAttestation) error {
	if att == nil {
		return ErrSSFEngineNilAttestation
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if this slot is already in finalized history.
	if rec, ok := e.historyMap[att.Slot]; ok && rec.finalized {
		return fmt.Errorf("%w: slot %d", ErrSSFEngineSlotFinalized, att.Slot)
	}

	// Check if slot is finalized in active slots.
	if rec, ok := e.slots[att.Slot]; ok && rec.finalized {
		return fmt.Errorf("%w: slot %d", ErrSSFEngineSlotFinalized, att.Slot)
	}

	// Look up validator weight.
	weight, ok := e.validatorWeights[att.ValidatorIndex]
	if !ok {
		return fmt.Errorf("%w: validator %d", ErrSSFEngineUnknownValidator, att.ValidatorIndex)
	}

	// Get or create slot record.
	rec, ok := e.slots[att.Slot]
	if !ok {
		rec = newSlotRecord()
		e.slots[att.Slot] = rec
	}

	// Check for duplicate attestation.
	if _, exists := rec.attestations[att.ValidatorIndex]; exists {
		return fmt.Errorf("%w: validator %d at slot %d",
			ErrSSFEngineDuplicateVote, att.ValidatorIndex, att.Slot)
	}

	// Record the attestation.
	rec.attestations[att.ValidatorIndex] = att
	rec.stakeByRoot[att.TargetRoot] += weight
	rec.totalStake += weight

	return nil
}

// CheckFinality checks whether a slot has reached finality.
func (e *SSFEngine) CheckFinality(slot uint64) (*FinalityResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.config.TotalStake == 0 {
		return nil, ErrSSFEngineZeroStake
	}

	threshold := e.requiredStake()

	// Check finality history first.
	if rec, ok := e.historyMap[slot]; ok {
		return &FinalityResult{
			Slot:        slot,
			IsFinalized: rec.finalized,
			VoteCount:   len(rec.attestations),
			StakeWeight: rec.totalStake,
			Threshold:   threshold,
		}, nil
	}

	rec, ok := e.slots[slot]
	if !ok {
		return &FinalityResult{
			Slot:        slot,
			IsFinalized: false,
			VoteCount:   0,
			StakeWeight: 0,
			Threshold:   threshold,
		}, nil
	}

	// Check if any root has reached the threshold.
	isFinalized := false
	for _, rootStake := range rec.stakeByRoot {
		if e.meetsThreshold(rootStake) {
			isFinalized = true
			break
		}
	}

	return &FinalityResult{
		Slot:        slot,
		IsFinalized: isFinalized,
		VoteCount:   len(rec.attestations),
		StakeWeight: rec.totalStake,
		Threshold:   threshold,
	}, nil
}

// Finalize explicitly marks a slot as finalized with the given root.
// This moves the slot from active tracking into the history cache.
func (e *SSFEngine) Finalize(slot uint64, root types.Hash) {
	e.mu.Lock()
	defer e.mu.Unlock()

	rec, ok := e.slots[slot]
	if !ok {
		rec = newSlotRecord()
	}
	rec.finalized = true
	rec.finalRoot = root

	// Move to history.
	e.historyMap[slot] = rec
	e.historySlots = append(e.historySlots, slot)
	delete(e.slots, slot)

	// Evict oldest entries if history exceeds max.
	for len(e.historySlots) > e.config.MaxSlotHistory {
		oldest := e.historySlots[0]
		e.historySlots = e.historySlots[1:]
		delete(e.historyMap, oldest)
	}
}

// GetVotingStatus returns the current voting status for a slot.
func (e *SSFEngine) GetVotingStatus(slot uint64) *VotingStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	required := e.requiredStake()

	rec, ok := e.slots[slot]
	if !ok {
		// Check history.
		if hrec, hok := e.historyMap[slot]; hok {
			participation := float64(0)
			if e.config.TotalStake > 0 {
				participation = float64(hrec.totalStake) / float64(e.config.TotalStake) * 100.0
			}
			return &VotingStatus{
				TotalVotes:    len(hrec.attestations),
				TotalStake:    hrec.totalStake,
				RequiredStake: required,
				Participation: participation,
			}
		}
		return &VotingStatus{
			TotalVotes:    0,
			TotalStake:    0,
			RequiredStake: required,
			Participation: 0,
		}
	}

	participation := float64(0)
	if e.config.TotalStake > 0 {
		participation = float64(rec.totalStake) / float64(e.config.TotalStake) * 100.0
	}

	return &VotingStatus{
		TotalVotes:    len(rec.attestations),
		TotalStake:    rec.totalStake,
		RequiredStake: required,
		Participation: participation,
	}
}

// SlotHistory returns a copy of the finalized slot numbers in the history cache.
func (e *SSFEngine) SlotHistory() []uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]uint64, len(e.historySlots))
	copy(result, e.historySlots)
	return result
}

// IsFinalized returns true if the given slot is in the finality history.
func (e *SSFEngine) IsFinalized(slot uint64) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if rec, ok := e.historyMap[slot]; ok && rec.finalized {
		return true
	}
	return false
}

// --- internal helpers ---

// meetsThreshold returns true if stake meets the finality threshold.
func (e *SSFEngine) meetsThreshold(stake uint64) bool {
	if e.config.TotalStake == 0 {
		return false
	}
	return stake*e.config.FinalityThresholdDen >= e.config.TotalStake*e.config.FinalityThresholdNum
}

// requiredStake returns the minimum stake needed for finality.
// This is ceil(TotalStake * Num / Den).
func (e *SSFEngine) requiredStake() uint64 {
	num := e.config.TotalStake * e.config.FinalityThresholdNum
	den := e.config.FinalityThresholdDen
	if den == 0 {
		return 0
	}
	// Ceiling division.
	return (num + den - 1) / den
}
