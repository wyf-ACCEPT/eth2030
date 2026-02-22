// compliance_tracker.go implements FOCIL compliance enforcement: tracks
// validator violations, computes compliance scores, manages grace periods,
// and generates per-epoch reports for slashing per EIP-7805.
package focil

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"golang.org/x/crypto/sha3"
)

// Compliance tracker errors.
var (
	ErrTrackerComplianceSlotZero     = errors.New("compliance-tracker: slot must be > 0")
	ErrTrackerComplianceNoValidator  = errors.New("compliance-tracker: validator not registered")
	ErrTrackerComplianceDuplicate    = errors.New("compliance-tracker: violation already recorded")
	ErrTrackerComplianceRangeInvalid = errors.New("compliance-tracker: invalid slot range")
	ErrTrackerComplianceGraceActive  = errors.New("compliance-tracker: validator in grace period")
)

// ComplianceViolationKind classifies the type of compliance failure.
type ComplianceViolationKind uint8

const (
	// ViolationMissedSubmission means the validator did not submit an IL.
	ViolationMissedSubmission ComplianceViolationKind = iota + 1

	// ViolationLateSubmission means the IL was submitted after the deadline.
	ViolationLateSubmission

	// ViolationConflicting means the validator submitted conflicting ILs.
	ViolationConflicting

	// ViolationInvalidContent means the IL failed structural validation.
	ViolationInvalidContent
)

// String returns a human-readable name for ComplianceViolationKind.
func (k ComplianceViolationKind) String() string {
	switch k {
	case ViolationMissedSubmission:
		return "missed_submission"
	case ViolationLateSubmission:
		return "late_submission"
	case ViolationConflicting:
		return "conflicting"
	case ViolationInvalidContent:
		return "invalid_content"
	default:
		return "unknown"
	}
}

// ComplianceViolation records a single compliance failure by a validator.
type ComplianceViolation struct {
	// ValidatorIndex is the offending validator.
	ValidatorIndex uint64

	// Slot is the slot where the violation occurred.
	Slot uint64

	// Kind classifies the violation.
	Kind ComplianceViolationKind

	// Evidence is an optional hash summarizing the violation details.
	Evidence types.Hash

	// PenaltyWeight is the severity weight of this violation (1-10).
	PenaltyWeight uint64
}

// ValidatorComplianceState tracks the compliance state for a single validator.
type ValidatorComplianceState struct {
	// ValidatorIndex is the validator being tracked.
	ValidatorIndex uint64

	// Score is the current compliance score (0 to 1000).
	Score int64

	// TotalDuties is the number of slots the validator was on committee.
	TotalDuties uint64

	// DutiesFulfilled is the number of slots with timely, valid submissions.
	DutiesFulfilled uint64

	// Violations lists all recorded violations.
	Violations []ComplianceViolation

	// ConsecutiveMisses tracks the current streak of missed submissions.
	ConsecutiveMisses uint64

	GraceRemaining   uint64
	SlashRecommended bool
}

// ComplianceReport aggregates compliance data for an epoch-level report
// suitable for submission to the slashing pipeline.
type ComplianceReport struct {
	// StartSlot is the first slot in the report range.
	StartSlot uint64

	// EndSlot is the last slot (inclusive) in the report range.
	EndSlot uint64

	// TotalViolations is the total number of violations in this period.
	TotalViolations int

	// TotalPenaltyWeight is the sum of penalty weights for all violations.
	TotalPenaltyWeight uint64

	// SlashCandidates lists validators recommended for slashing.
	SlashCandidates []uint64

	// Violations is the full list of violations in this report.
	Violations []ComplianceViolation

	// ViolationsByKind breaks down violation counts by kind.
	ViolationsByKind map[ComplianceViolationKind]int

	// ReportHash is a binding commitment over the report contents.
	ReportHash types.Hash
}

// ComplianceTrackerConfig configures the compliance tracker.
type ComplianceTrackerConfig struct {
	// InitialScore is the starting compliance score for new validators.
	InitialScore int64

	// MaxScore is the maximum compliance score. Default: 1000.
	MaxScore int64

	// MissedSubmissionWeight is the penalty weight for missing an IL.
	MissedSubmissionWeight uint64

	// LateSubmissionWeight is the penalty weight for a late IL.
	LateSubmissionWeight uint64

	// ConflictingWeight is the penalty weight for conflicting ILs.
	ConflictingWeight uint64

	// InvalidContentWeight is the penalty weight for invalid IL content.
	InvalidContentWeight uint64

	// GracePeriodSlots is the number of initial slots during which
	// violations are recorded but do not trigger slashing. Default: 32.
	GracePeriodSlots uint64

	// SlashThresholdMisses is the consecutive miss count that triggers a
	// slashing recommendation. Default: 5.
	SlashThresholdMisses uint64

	// ScoreRecoveryPerDuty is the score recovered per fulfilled duty.
	ScoreRecoveryPerDuty int64
}

// DefaultComplianceTrackerConfig returns production defaults.
func DefaultComplianceTrackerConfig() ComplianceTrackerConfig {
	return ComplianceTrackerConfig{
		InitialScore:         1000,
		MaxScore:             1000,
		MissedSubmissionWeight: 3,
		LateSubmissionWeight:   1,
		ConflictingWeight:      5,
		InvalidContentWeight:   2,
		GracePeriodSlots:       32,
		SlashThresholdMisses:   5,
		ScoreRecoveryPerDuty:   10,
	}
}

// ComplianceTracker tracks per-validator compliance state and generates
// reports for the slashing pipeline. Thread-safe.
type ComplianceTracker struct {
	mu     sync.RWMutex
	config ComplianceTrackerConfig

	// validators maps validator index -> compliance state.
	validators map[uint64]*ValidatorComplianceState
}

// NewComplianceTracker creates a new compliance tracker.
// A GracePeriodSlots of 0 is valid and disables the grace period.
func NewComplianceTracker(config ComplianceTrackerConfig) *ComplianceTracker {
	if config.InitialScore <= 0 {
		config.InitialScore = 1000
	}
	if config.MaxScore <= 0 {
		config.MaxScore = 1000
	}
	if config.MissedSubmissionWeight == 0 {
		config.MissedSubmissionWeight = 3
	}
	if config.SlashThresholdMisses == 0 {
		config.SlashThresholdMisses = 5
	}
	// GracePeriodSlots == 0 is a valid value (no grace period); do not override.
	if config.ScoreRecoveryPerDuty <= 0 {
		config.ScoreRecoveryPerDuty = 10
	}
	return &ComplianceTracker{
		config:     config,
		validators: make(map[uint64]*ValidatorComplianceState),
	}
}

// RegisterValidator initializes compliance tracking for a validator with
// the configured grace period.
func (ct *ComplianceTracker) RegisterValidator(validatorIndex uint64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if _, exists := ct.validators[validatorIndex]; exists {
		return
	}
	ct.validators[validatorIndex] = &ValidatorComplianceState{
		ValidatorIndex: validatorIndex,
		Score:          ct.config.InitialScore,
		GraceRemaining: ct.config.GracePeriodSlots,
	}
}

// RecordDutyFulfilled records that a validator successfully submitted a
// valid IL for their assigned slot. It resets the consecutive miss counter
// and recovers score.
func (ct *ComplianceTracker) RecordDutyFulfilled(validatorIndex, slot uint64) error {
	if slot == 0 {
		return ErrTrackerComplianceSlotZero
	}

	ct.mu.Lock()
	defer ct.mu.Unlock()

	state, ok := ct.validators[validatorIndex]
	if !ok {
		return fmt.Errorf("%w: validator %d", ErrTrackerComplianceNoValidator, validatorIndex)
	}

	state.TotalDuties++
	state.DutiesFulfilled++
	state.ConsecutiveMisses = 0

	// Recover score.
	state.Score += ct.config.ScoreRecoveryPerDuty
	if state.Score > ct.config.MaxScore {
		state.Score = ct.config.MaxScore
	}

	// Decrement grace period.
	if state.GraceRemaining > 0 {
		state.GraceRemaining--
	}

	return nil
}

// RecordViolation records a compliance violation for a validator. The
// penalty weight is determined by the violation kind and the tracker config.
func (ct *ComplianceTracker) RecordViolation(validatorIndex, slot uint64, kind ComplianceViolationKind, evidence types.Hash) error {
	if slot == 0 {
		return ErrTrackerComplianceSlotZero
	}

	ct.mu.Lock()
	defer ct.mu.Unlock()

	state, ok := ct.validators[validatorIndex]
	if !ok {
		return fmt.Errorf("%w: validator %d", ErrTrackerComplianceNoValidator, validatorIndex)
	}

	weight := ct.violationWeight(kind)

	violation := ComplianceViolation{
		ValidatorIndex: validatorIndex,
		Slot:           slot,
		Kind:           kind,
		Evidence:       evidence,
		PenaltyWeight:  weight,
	}

	state.Violations = append(state.Violations, violation)
	state.TotalDuties++

	// Apply score penalty.
	state.Score -= int64(weight) * 10
	if state.Score < 0 {
		state.Score = 0
	}

	// Track consecutive misses.
	if kind == ViolationMissedSubmission {
		state.ConsecutiveMisses++
	} else {
		state.ConsecutiveMisses = 0
	}

	// Decrement grace period before checking slash threshold.
	if state.GraceRemaining > 0 {
		state.GraceRemaining--
	}

	// Check slashing threshold (only outside grace period).
	if state.GraceRemaining == 0 &&
		state.ConsecutiveMisses >= ct.config.SlashThresholdMisses {
		state.SlashRecommended = true
	}

	return nil
}

// GetValidatorState returns a copy of the compliance state for a validator.
func (ct *ComplianceTracker) GetValidatorState(validatorIndex uint64) (*ValidatorComplianceState, error) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	state, ok := ct.validators[validatorIndex]
	if !ok {
		return nil, fmt.Errorf("%w: validator %d", ErrTrackerComplianceNoValidator, validatorIndex)
	}

	cp := *state
	cp.Violations = make([]ComplianceViolation, len(state.Violations))
	copy(cp.Violations, state.Violations)
	return &cp, nil
}

// GetComplianceScore returns the current compliance score for a validator.
func (ct *ComplianceTracker) GetComplianceScore(validatorIndex uint64) (int64, error) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	state, ok := ct.validators[validatorIndex]
	if !ok {
		return 0, fmt.Errorf("%w: validator %d", ErrTrackerComplianceNoValidator, validatorIndex)
	}
	return state.Score, nil
}

// IsInGracePeriod returns whether a validator is still in their grace period.
func (ct *ComplianceTracker) IsInGracePeriod(validatorIndex uint64) (bool, error) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	state, ok := ct.validators[validatorIndex]
	if !ok {
		return false, fmt.Errorf("%w: validator %d", ErrTrackerComplianceNoValidator, validatorIndex)
	}
	return state.GraceRemaining > 0, nil
}

// GenerateReport creates a compliance report for the given slot range.
// The report includes all violations, slash candidates, and a binding
// commitment hash.
func (ct *ComplianceTracker) GenerateReport(startSlot, endSlot uint64) (*ComplianceReport, error) {
	if startSlot == 0 || endSlot == 0 {
		return nil, ErrTrackerComplianceSlotZero
	}
	if endSlot < startSlot {
		return nil, fmt.Errorf("%w: startSlot=%d > endSlot=%d",
			ErrTrackerComplianceRangeInvalid, startSlot, endSlot)
	}

	ct.mu.RLock()
	defer ct.mu.RUnlock()

	report := &ComplianceReport{
		StartSlot:      startSlot,
		EndSlot:        endSlot,
		ViolationsByKind: make(map[ComplianceViolationKind]int),
	}

	slashSet := make(map[uint64]bool)

	for _, state := range ct.validators {
		for _, v := range state.Violations {
			if v.Slot >= startSlot && v.Slot <= endSlot {
				report.Violations = append(report.Violations, v)
				report.TotalViolations++
				report.TotalPenaltyWeight += v.PenaltyWeight
				report.ViolationsByKind[v.Kind]++
			}
		}
		if state.SlashRecommended {
			slashSet[state.ValidatorIndex] = true
		}
	}

	// Sort violations by slot then validator for determinism.
	sort.Slice(report.Violations, func(i, j int) bool {
		if report.Violations[i].Slot != report.Violations[j].Slot {
			return report.Violations[i].Slot < report.Violations[j].Slot
		}
		return report.Violations[i].ValidatorIndex < report.Violations[j].ValidatorIndex
	})

	// Collect slash candidates.
	for vi := range slashSet {
		report.SlashCandidates = append(report.SlashCandidates, vi)
	}
	sort.Slice(report.SlashCandidates, func(i, j int) bool {
		return report.SlashCandidates[i] < report.SlashCandidates[j]
	})

	report.ReportHash = computeComplianceReportHash(report)
	return report, nil
}

// SlashCandidates returns all validators currently recommended for slashing.
func (ct *ComplianceTracker) SlashCandidates() []uint64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	var candidates []uint64
	for _, state := range ct.validators {
		if state.SlashRecommended {
			candidates = append(candidates, state.ValidatorIndex)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	return candidates
}

// ValidatorCount returns the number of tracked validators.
func (ct *ComplianceTracker) ValidatorCount() int {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return len(ct.validators)
}

// ResetSlashRecommendation clears the slashing recommendation for a
// validator (e.g., after a slash has been processed).
func (ct *ComplianceTracker) ResetSlashRecommendation(validatorIndex uint64) error {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	state, ok := ct.validators[validatorIndex]
	if !ok {
		return fmt.Errorf("%w: validator %d", ErrTrackerComplianceNoValidator, validatorIndex)
	}
	state.SlashRecommended = false
	state.ConsecutiveMisses = 0
	return nil
}

// --- Internal helpers ---

// violationWeight returns the penalty weight for a violation kind.
func (ct *ComplianceTracker) violationWeight(kind ComplianceViolationKind) uint64 {
	switch kind {
	case ViolationMissedSubmission:
		return ct.config.MissedSubmissionWeight
	case ViolationLateSubmission:
		return ct.config.LateSubmissionWeight
	case ViolationConflicting:
		return ct.config.ConflictingWeight
	case ViolationInvalidContent:
		return ct.config.InvalidContentWeight
	default:
		return 1
	}
}

// computeComplianceReportHash computes a binding hash over report contents.
func computeComplianceReportHash(report *ComplianceReport) types.Hash {
	h := sha3.NewLegacyKeccak256()
	var buf [8]byte

	binary.LittleEndian.PutUint64(buf[:], report.StartSlot)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], report.EndSlot)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(report.TotalViolations))
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], report.TotalPenaltyWeight)
	h.Write(buf[:])

	for _, v := range report.Violations {
		binary.LittleEndian.PutUint64(buf[:], v.ValidatorIndex)
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], v.Slot)
		h.Write(buf[:])
		h.Write([]byte{byte(v.Kind)})
		h.Write(v.Evidence[:])
	}

	for _, c := range report.SlashCandidates {
		binary.LittleEndian.PutUint64(buf[:], c)
		h.Write(buf[:])
	}

	var result types.Hash
	h.Sum(result[:0])
	return result
}
