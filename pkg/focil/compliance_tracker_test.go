package focil

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestComplianceTrackerRegisterAndFulfill(t *testing.T) {
	ct := NewComplianceTracker(DefaultComplianceTrackerConfig())

	ct.RegisterValidator(1)
	ct.RegisterValidator(2)

	if ct.ValidatorCount() != 2 {
		t.Errorf("ValidatorCount = %d, want 2", ct.ValidatorCount())
	}

	err := ct.RecordDutyFulfilled(1, 100)
	if err != nil {
		t.Fatalf("RecordDutyFulfilled: %v", err)
	}

	state, err := ct.GetValidatorState(1)
	if err != nil {
		t.Fatalf("GetValidatorState: %v", err)
	}
	if state.TotalDuties != 1 {
		t.Errorf("TotalDuties = %d, want 1", state.TotalDuties)
	}
	if state.DutiesFulfilled != 1 {
		t.Errorf("DutiesFulfilled = %d, want 1", state.DutiesFulfilled)
	}
	if state.ConsecutiveMisses != 0 {
		t.Errorf("ConsecutiveMisses = %d, want 0", state.ConsecutiveMisses)
	}
}

func TestComplianceTrackerDuplicateRegister(t *testing.T) {
	ct := NewComplianceTracker(DefaultComplianceTrackerConfig())

	ct.RegisterValidator(1)
	ct.RegisterValidator(1) // should be idempotent

	if ct.ValidatorCount() != 1 {
		t.Errorf("ValidatorCount = %d, want 1", ct.ValidatorCount())
	}
}

func TestComplianceTrackerUnregisteredValidator(t *testing.T) {
	ct := NewComplianceTracker(DefaultComplianceTrackerConfig())

	err := ct.RecordDutyFulfilled(999, 1)
	if err == nil {
		t.Fatal("expected error for unregistered validator")
	}

	err = ct.RecordViolation(999, 1, ViolationMissedSubmission, types.Hash{})
	if err == nil {
		t.Fatal("expected error for unregistered validator violation")
	}

	_, err = ct.GetValidatorState(999)
	if err == nil {
		t.Fatal("expected error for unregistered validator state")
	}
}

func TestComplianceTrackerSlotZero(t *testing.T) {
	ct := NewComplianceTracker(DefaultComplianceTrackerConfig())
	ct.RegisterValidator(1)

	err := ct.RecordDutyFulfilled(1, 0)
	if err == nil {
		t.Fatal("expected error for slot 0")
	}

	err = ct.RecordViolation(1, 0, ViolationMissedSubmission, types.Hash{})
	if err == nil {
		t.Fatal("expected error for slot 0 violation")
	}
}

func TestComplianceTrackerViolationScoreDeduction(t *testing.T) {
	config := DefaultComplianceTrackerConfig()
	config.InitialScore = 100
	config.MaxScore = 100
	config.MissedSubmissionWeight = 2
	ct := NewComplianceTracker(config)

	ct.RegisterValidator(1)

	err := ct.RecordViolation(1, 10, ViolationMissedSubmission, types.HexToHash("0xaa"))
	if err != nil {
		t.Fatalf("RecordViolation: %v", err)
	}

	score, err := ct.GetComplianceScore(1)
	if err != nil {
		t.Fatalf("GetComplianceScore: %v", err)
	}
	// InitialScore(100) - weight(2)*10 = 80
	if score != 80 {
		t.Errorf("score = %d, want 80", score)
	}

	state, _ := ct.GetValidatorState(1)
	if state.ConsecutiveMisses != 1 {
		t.Errorf("ConsecutiveMisses = %d, want 1", state.ConsecutiveMisses)
	}
	if len(state.Violations) != 1 {
		t.Errorf("Violations count = %d, want 1", len(state.Violations))
	}
}

func TestComplianceTrackerScoreFloor(t *testing.T) {
	config := DefaultComplianceTrackerConfig()
	config.InitialScore = 10
	config.MaxScore = 10
	config.ConflictingWeight = 5
	ct := NewComplianceTracker(config)

	ct.RegisterValidator(1)

	// Two violations should bring score below zero, clamped to 0.
	_ = ct.RecordViolation(1, 10, ViolationConflicting, types.Hash{})

	score, _ := ct.GetComplianceScore(1)
	if score < 0 {
		t.Errorf("score = %d, should not be negative", score)
	}
}

func TestComplianceTrackerScoreRecovery(t *testing.T) {
	config := DefaultComplianceTrackerConfig()
	config.InitialScore = 100
	config.MaxScore = 100
	config.MissedSubmissionWeight = 2
	config.ScoreRecoveryPerDuty = 5
	ct := NewComplianceTracker(config)

	ct.RegisterValidator(1)

	// Record a violation: score = 100 - 20 = 80.
	_ = ct.RecordViolation(1, 10, ViolationMissedSubmission, types.Hash{})

	// Fulfill a duty: score = 80 + 5 = 85.
	_ = ct.RecordDutyFulfilled(1, 11)

	score, _ := ct.GetComplianceScore(1)
	if score != 85 {
		t.Errorf("score = %d, want 85", score)
	}
}

func TestComplianceTrackerScoreCap(t *testing.T) {
	config := DefaultComplianceTrackerConfig()
	config.InitialScore = 100
	config.MaxScore = 100
	config.ScoreRecoveryPerDuty = 50
	ct := NewComplianceTracker(config)

	ct.RegisterValidator(1)

	// Score is already at max; recovery should not exceed max.
	_ = ct.RecordDutyFulfilled(1, 10)

	score, _ := ct.GetComplianceScore(1)
	if score != 100 {
		t.Errorf("score = %d, want 100 (capped)", score)
	}
}

func TestComplianceTrackerGracePeriod(t *testing.T) {
	config := DefaultComplianceTrackerConfig()
	config.GracePeriodSlots = 3
	config.SlashThresholdMisses = 2
	ct := NewComplianceTracker(config)

	ct.RegisterValidator(1)

	inGrace, _ := ct.IsInGracePeriod(1)
	if !inGrace {
		t.Error("validator should be in grace period initially")
	}

	// Record violations during grace period: should not recommend slash.
	_ = ct.RecordViolation(1, 10, ViolationMissedSubmission, types.Hash{})
	_ = ct.RecordViolation(1, 11, ViolationMissedSubmission, types.Hash{})

	state, _ := ct.GetValidatorState(1)
	if state.SlashRecommended {
		t.Error("should not recommend slash during grace period")
	}

	// Grace period consumed: 3 - 2 violations = 1 remaining.
	inGrace, _ = ct.IsInGracePeriod(1)
	if !inGrace {
		t.Error("should still have grace remaining after 2 violations out of 3")
	}

	// Third violation: grace is now 0, consecutive misses = 3 >= threshold(2)
	// -> slash recommended.
	_ = ct.RecordViolation(1, 12, ViolationMissedSubmission, types.Hash{})

	state, _ = ct.GetValidatorState(1)
	if !state.SlashRecommended {
		t.Error("should recommend slash after grace period expires and threshold met")
	}
}

func TestComplianceTrackerSlashCandidates(t *testing.T) {
	config := DefaultComplianceTrackerConfig()
	config.GracePeriodSlots = 0 // no grace
	config.SlashThresholdMisses = 2
	ct := NewComplianceTracker(config)

	ct.RegisterValidator(1)
	ct.RegisterValidator(2)
	ct.RegisterValidator(3)

	// Validator 1: 2 consecutive misses -> slash.
	_ = ct.RecordViolation(1, 10, ViolationMissedSubmission, types.Hash{})
	_ = ct.RecordViolation(1, 11, ViolationMissedSubmission, types.Hash{})

	// Validator 2: 1 miss, then fulfilled -> no slash.
	_ = ct.RecordViolation(2, 10, ViolationMissedSubmission, types.Hash{})
	_ = ct.RecordDutyFulfilled(2, 11)

	// Validator 3: fulfilled -> no slash.
	_ = ct.RecordDutyFulfilled(3, 10)

	candidates := ct.SlashCandidates()
	if len(candidates) != 1 {
		t.Fatalf("SlashCandidates count = %d, want 1", len(candidates))
	}
	if candidates[0] != 1 {
		t.Errorf("SlashCandidates[0] = %d, want 1", candidates[0])
	}
}

func TestComplianceTrackerResetSlashRecommendation(t *testing.T) {
	config := DefaultComplianceTrackerConfig()
	config.GracePeriodSlots = 0
	config.SlashThresholdMisses = 1
	ct := NewComplianceTracker(config)

	ct.RegisterValidator(1)
	_ = ct.RecordViolation(1, 10, ViolationMissedSubmission, types.Hash{})

	if len(ct.SlashCandidates()) != 1 {
		t.Fatal("expected 1 slash candidate")
	}

	err := ct.ResetSlashRecommendation(1)
	if err != nil {
		t.Fatalf("ResetSlashRecommendation: %v", err)
	}

	if len(ct.SlashCandidates()) != 0 {
		t.Error("expected 0 slash candidates after reset")
	}
}

func TestComplianceTrackerResetSlashUnknownValidator(t *testing.T) {
	ct := NewComplianceTracker(DefaultComplianceTrackerConfig())

	err := ct.ResetSlashRecommendation(999)
	if err == nil {
		t.Fatal("expected error for unknown validator")
	}
}

func TestComplianceTrackerGenerateReport(t *testing.T) {
	config := DefaultComplianceTrackerConfig()
	config.GracePeriodSlots = 0
	config.SlashThresholdMisses = 2
	ct := NewComplianceTracker(config)

	ct.RegisterValidator(1)
	ct.RegisterValidator(2)

	// Validator 1: 2 misses -> slash.
	_ = ct.RecordViolation(1, 100, ViolationMissedSubmission, types.HexToHash("0x01"))
	_ = ct.RecordViolation(1, 101, ViolationMissedSubmission, types.HexToHash("0x02"))

	// Validator 2: 1 conflicting.
	_ = ct.RecordViolation(2, 100, ViolationConflicting, types.HexToHash("0x03"))

	report, err := ct.GenerateReport(100, 101)
	if err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}

	if report.StartSlot != 100 || report.EndSlot != 101 {
		t.Errorf("slot range = %d-%d, want 100-101", report.StartSlot, report.EndSlot)
	}
	if report.TotalViolations != 3 {
		t.Errorf("TotalViolations = %d, want 3", report.TotalViolations)
	}
	if report.TotalPenaltyWeight == 0 {
		t.Error("TotalPenaltyWeight should be > 0")
	}
	if len(report.SlashCandidates) != 1 {
		t.Errorf("SlashCandidates = %d, want 1", len(report.SlashCandidates))
	}
	if report.SlashCandidates[0] != 1 {
		t.Errorf("SlashCandidates[0] = %d, want 1", report.SlashCandidates[0])
	}
	if report.ReportHash.IsZero() {
		t.Error("ReportHash should not be zero")
	}

	// Check violation breakdown.
	if report.ViolationsByKind[ViolationMissedSubmission] != 2 {
		t.Errorf("missed submissions = %d, want 2", report.ViolationsByKind[ViolationMissedSubmission])
	}
	if report.ViolationsByKind[ViolationConflicting] != 1 {
		t.Errorf("conflicting = %d, want 1", report.ViolationsByKind[ViolationConflicting])
	}
}

func TestComplianceTrackerGenerateReportSlotFiltering(t *testing.T) {
	ct := NewComplianceTracker(DefaultComplianceTrackerConfig())

	ct.RegisterValidator(1)
	_ = ct.RecordViolation(1, 50, ViolationMissedSubmission, types.Hash{})
	_ = ct.RecordViolation(1, 100, ViolationMissedSubmission, types.Hash{})
	_ = ct.RecordViolation(1, 150, ViolationMissedSubmission, types.Hash{})

	// Only slot 100 should be in the report.
	report, err := ct.GenerateReport(100, 100)
	if err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}
	if report.TotalViolations != 1 {
		t.Errorf("TotalViolations = %d, want 1", report.TotalViolations)
	}
}

func TestComplianceTrackerGenerateReportErrors(t *testing.T) {
	ct := NewComplianceTracker(DefaultComplianceTrackerConfig())

	_, err := ct.GenerateReport(0, 100)
	if err == nil {
		t.Fatal("expected error for slot 0")
	}

	_, err = ct.GenerateReport(100, 0)
	if err == nil {
		t.Fatal("expected error for slot 0 endSlot")
	}

	_, err = ct.GenerateReport(200, 100)
	if err == nil {
		t.Fatal("expected error for invalid range")
	}
}

func TestComplianceTrackerConsecutiveMissesResetOnFulfill(t *testing.T) {
	config := DefaultComplianceTrackerConfig()
	config.GracePeriodSlots = 0
	config.SlashThresholdMisses = 3
	ct := NewComplianceTracker(config)

	ct.RegisterValidator(1)

	// 2 misses.
	_ = ct.RecordViolation(1, 10, ViolationMissedSubmission, types.Hash{})
	_ = ct.RecordViolation(1, 11, ViolationMissedSubmission, types.Hash{})

	state, _ := ct.GetValidatorState(1)
	if state.ConsecutiveMisses != 2 {
		t.Errorf("ConsecutiveMisses = %d, want 2", state.ConsecutiveMisses)
	}

	// Fulfill resets streak.
	_ = ct.RecordDutyFulfilled(1, 12)

	state, _ = ct.GetValidatorState(1)
	if state.ConsecutiveMisses != 0 {
		t.Errorf("ConsecutiveMisses after fulfill = %d, want 0", state.ConsecutiveMisses)
	}
}

func TestComplianceTrackerNonMissViolationResetsStreak(t *testing.T) {
	config := DefaultComplianceTrackerConfig()
	config.GracePeriodSlots = 0
	config.SlashThresholdMisses = 5
	ct := NewComplianceTracker(config)

	ct.RegisterValidator(1)

	// 2 misses.
	_ = ct.RecordViolation(1, 10, ViolationMissedSubmission, types.Hash{})
	_ = ct.RecordViolation(1, 11, ViolationMissedSubmission, types.Hash{})

	// A non-miss violation resets the consecutive miss counter.
	_ = ct.RecordViolation(1, 12, ViolationLateSubmission, types.Hash{})

	state, _ := ct.GetValidatorState(1)
	if state.ConsecutiveMisses != 0 {
		t.Errorf("ConsecutiveMisses = %d, want 0 after non-miss violation", state.ConsecutiveMisses)
	}
}

func TestComplianceViolationKindString(t *testing.T) {
	cases := []struct {
		k    ComplianceViolationKind
		want string
	}{
		{ViolationMissedSubmission, "missed_submission"},
		{ViolationLateSubmission, "late_submission"},
		{ViolationConflicting, "conflicting"},
		{ViolationInvalidContent, "invalid_content"},
		{ComplianceViolationKind(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("%d.String() = %q, want %q", c.k, got, c.want)
		}
	}
}

func TestComplianceTrackerIsInGracePeriodError(t *testing.T) {
	ct := NewComplianceTracker(DefaultComplianceTrackerConfig())

	_, err := ct.IsInGracePeriod(999)
	if err == nil {
		t.Fatal("expected error for unregistered validator")
	}
}

func TestComplianceTrackerGetComplianceScoreError(t *testing.T) {
	ct := NewComplianceTracker(DefaultComplianceTrackerConfig())

	_, err := ct.GetComplianceScore(999)
	if err == nil {
		t.Fatal("expected error for unregistered validator")
	}
}
