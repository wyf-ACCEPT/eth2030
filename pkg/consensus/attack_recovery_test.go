package consensus

import (
	"testing"
)

// TestSeverityLevel verifies that reorg depths map to the correct severity.
func TestSeverityLevel(t *testing.T) {
	tests := []struct {
		depth    uint64
		expected string
	}{
		{0, SeverityNone},
		{1, SeverityNone},
		{2, SeverityLow},
		{3, SeverityLow},
		{4, SeverityMedium},
		{7, SeverityMedium},
		{8, SeverityHigh},
		{15, SeverityHigh},
		{16, SeverityCritical},
		{100, SeverityCritical},
	}
	for _, tt := range tests {
		got := SeverityLevel(tt.depth)
		if got != tt.expected {
			t.Errorf("SeverityLevel(%d) = %q, want %q", tt.depth, got, tt.expected)
		}
	}
}

// TestDetectAttackNoAttack verifies that shallow reorgs do not trigger detection.
func TestDetectAttackNoAttack(t *testing.T) {
	ad := NewAttackDetector()
	report := ad.DetectAttack(1, 10, 20)
	if report.Detected {
		t.Error("expected no attack for reorg depth 1")
	}
	if report.Severity != SeverityNone {
		t.Errorf("expected severity none, got %q", report.Severity)
	}
	if ad.IsUnderAttack() {
		t.Error("detector should not be under attack")
	}
}

// TestDetectAttackLow verifies low-severity attack detection.
func TestDetectAttackLow(t *testing.T) {
	ad := NewAttackDetector()
	report := ad.DetectAttack(3, 10, 20)
	if !report.Detected {
		t.Error("expected attack detected for reorg depth 3")
	}
	if report.Severity != SeverityLow {
		t.Errorf("expected low severity, got %q", report.Severity)
	}
	if report.RecommendedAction != ActionMonitor {
		t.Errorf("expected action %q, got %q", ActionMonitor, report.RecommendedAction)
	}
	if !ad.IsUnderAttack() {
		t.Error("detector should be under attack")
	}
}

// TestDetectAttackCritical verifies critical severity detection.
func TestDetectAttackCritical(t *testing.T) {
	ad := NewAttackDetector()
	report := ad.DetectAttack(20, 5, 25)
	if !report.Detected {
		t.Error("expected attack detected for reorg depth 20")
	}
	if report.Severity != SeverityCritical {
		t.Errorf("expected critical severity, got %q", report.Severity)
	}
	if report.RecommendedAction != ActionSocialOverride {
		t.Errorf("expected action %q, got %q", ActionSocialOverride, report.RecommendedAction)
	}
}

// TestDetectAttackReachesFinalizedEpoch verifies that a reorg reaching
// the finalized checkpoint is treated as at least high severity.
func TestDetectAttackReachesFinalizedEpoch(t *testing.T) {
	ad := NewAttackDetector()
	// Current epoch 15, finalized at 10, reorg depth 5 reaches finalized boundary.
	report := ad.DetectAttack(5, 10, 15)
	if !report.Detected {
		t.Error("expected attack detected when reorg reaches finalized epoch")
	}
	// Depth 5 would normally be medium, but reaching finalized upgrades to high.
	if report.Severity != SeverityHigh {
		t.Errorf("expected high severity when reaching finalized, got %q", report.Severity)
	}
}

// TestAffectedEpochs verifies the affected epoch range computation.
func TestAffectedEpochs(t *testing.T) {
	epochs := affectedEpochs(5, 10, 20)
	if len(epochs) != 5 {
		t.Fatalf("expected 5 affected epochs, got %d", len(epochs))
	}
	for i, want := range []uint64{11, 12, 13, 14, 15} {
		if epochs[i] != want {
			t.Errorf("affected epoch [%d] = %d, want %d", i, epochs[i], want)
		}
	}

	// Reorg depth larger than gap to current: cap at currentEpoch.
	epochs = affectedEpochs(100, 10, 15)
	if len(epochs) != 5 {
		t.Fatalf("expected 5 capped affected epochs, got %d", len(epochs))
	}
	if epochs[len(epochs)-1] != 15 {
		t.Errorf("last affected epoch = %d, want 15", epochs[len(epochs)-1])
	}

	// Zero reorg depth: no affected epochs.
	epochs = affectedEpochs(0, 10, 20)
	if len(epochs) != 0 {
		t.Errorf("expected 0 affected epochs for zero depth, got %d", len(epochs))
	}
}

// TestBuildRecoveryPlan verifies plan construction for various severities.
func TestBuildRecoveryPlan(t *testing.T) {
	// No attack: should error.
	_, err := BuildRecoveryPlan(&AttackReport{Detected: false})
	if err != ErrNoAttackDetected {
		t.Errorf("expected ErrNoAttackDetected, got %v", err)
	}

	// Nil report: should error.
	_, err = BuildRecoveryPlan(nil)
	if err != ErrNoAttackDetected {
		t.Errorf("expected ErrNoAttackDetected for nil, got %v", err)
	}

	// Critical plan.
	plan, err := BuildRecoveryPlan(&AttackReport{
		Detected:       true,
		Severity:       SeverityCritical,
		FinalizedEpoch: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !plan.IsolationMode || !plan.FallbackToFinalized || !plan.SocialConsensusOverride {
		t.Error("critical plan should enable all recovery actions")
	}
	if plan.FinalizedCheckpointEpoch != 100 {
		t.Errorf("expected finalized epoch 100, got %d", plan.FinalizedCheckpointEpoch)
	}

	// Medium plan: isolation only.
	plan, err = BuildRecoveryPlan(&AttackReport{
		Detected: true,
		Severity: SeverityMedium,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !plan.IsolationMode {
		t.Error("medium plan should enable isolation")
	}
	if plan.FallbackToFinalized || plan.SocialConsensusOverride {
		t.Error("medium plan should not enable fallback or social override")
	}

	// Low plan: no automated actions.
	plan, err = BuildRecoveryPlan(&AttackReport{
		Detected: true,
		Severity: SeverityLow,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.IsolationMode || plan.FallbackToFinalized || plan.SocialConsensusOverride {
		t.Error("low plan should not enable any automated actions")
	}
}

// TestExecuteRecovery verifies full recovery execution.
func TestExecuteRecovery(t *testing.T) {
	ad := NewAttackDetector()

	// Nil plan: error.
	if err := ad.ExecuteRecovery(nil); err != ErrNilRecoveryPlan {
		t.Errorf("expected ErrNilRecoveryPlan, got %v", err)
	}

	// Empty plan (no actions): error.
	if err := ad.ExecuteRecovery(&RecoveryPlan{}); err != ErrInvalidPlan {
		t.Errorf("expected ErrInvalidPlan, got %v", err)
	}

	// Valid critical recovery.
	plan := &RecoveryPlan{
		IsolationMode:            true,
		FallbackToFinalized:      true,
		SocialConsensusOverride:  true,
		FinalizedCheckpointEpoch: 50,
		Severity:                 SeverityCritical,
	}
	if err := ad.ExecuteRecovery(plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	status := ad.GetRecoveryStatus()
	if !status.Active {
		t.Error("recovery should be active")
	}
	if !status.PeersIsolated {
		t.Error("peers should be isolated")
	}
	if !status.FellBackToFinalized {
		t.Error("should have fallen back to finalized")
	}
	if !status.SocialOverrideSet {
		t.Error("social override should be set")
	}

	// Double recovery: error.
	if err := ad.ExecuteRecovery(plan); err != ErrAlreadyRecovering {
		t.Errorf("expected ErrAlreadyRecovering, got %v", err)
	}
}

// TestClearRecovery verifies that state is properly reset.
func TestClearRecovery(t *testing.T) {
	ad := NewAttackDetector()

	// Detect an attack and execute recovery.
	ad.DetectAttack(20, 5, 25)
	plan := &RecoveryPlan{
		IsolationMode:       true,
		FallbackToFinalized: true,
	}
	if err := ad.ExecuteRecovery(plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ad.IsUnderAttack() {
		t.Error("should be under attack before clear")
	}

	ad.ClearRecovery()

	if ad.IsUnderAttack() {
		t.Error("should not be under attack after clear")
	}
	status := ad.GetRecoveryStatus()
	if status.Active {
		t.Error("recovery should not be active after clear")
	}
}

// TestAttackReportString verifies the string representation.
func TestAttackReportString(t *testing.T) {
	noAttack := &AttackReport{Detected: false}
	if s := noAttack.String(); s != "no attack detected" {
		t.Errorf("unexpected string for no attack: %q", s)
	}

	attack := &AttackReport{
		Detected:          true,
		Severity:          SeverityHigh,
		ReorgDepth:        10,
		FinalizedEpoch:    5,
		CurrentEpoch:      20,
		AffectedEpochs:    []uint64{6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		RecommendedAction: ActionFallback,
	}
	s := attack.String()
	if s == "" || s == "no attack detected" {
		t.Error("expected non-empty attack description")
	}
}

// TestDetectAttackMediumSeverity verifies medium-severity detection.
func TestDetectAttackMediumSeverity(t *testing.T) {
	ad := NewAttackDetector()
	report := ad.DetectAttack(6, 50, 100)
	if !report.Detected {
		t.Error("expected attack detected for reorg depth 6")
	}
	if report.Severity != SeverityMedium {
		t.Errorf("expected medium severity, got %q", report.Severity)
	}
	if report.RecommendedAction != ActionIsolate {
		t.Errorf("expected action %q, got %q", ActionIsolate, report.RecommendedAction)
	}
}

// TestLastReport verifies that the detector stores the most recent report.
func TestLastReport(t *testing.T) {
	ad := NewAttackDetector()
	if ad.LastReport() != nil {
		t.Error("expected nil last report initially")
	}

	ad.DetectAttack(0, 10, 20)
	r := ad.LastReport()
	if r == nil {
		t.Fatal("expected non-nil last report")
	}
	if r.Detected {
		t.Error("expected no attack for depth 0")
	}

	// Second detection overwrites.
	ad.DetectAttack(10, 10, 20)
	r = ad.LastReport()
	if r == nil || !r.Detected {
		t.Error("expected attack in updated last report")
	}
}

func TestValidateRecoveryPlan(t *testing.T) {
	// Valid critical plan.
	plan := &RecoveryPlan{
		Severity: SeverityCritical, IsolationMode: true,
		FallbackToFinalized: true, SocialConsensusOverride: true,
	}
	if err := ValidateRecoveryPlan(plan); err != nil {
		t.Errorf("valid plan: %v", err)
	}

	// Nil plan.
	if err := ValidateRecoveryPlan(nil); err == nil {
		t.Error("expected error for nil plan")
	}

	// No actions.
	if err := ValidateRecoveryPlan(&RecoveryPlan{Severity: SeverityMedium}); err == nil {
		t.Error("expected error for plan with no actions")
	}
}

func TestValidateAttackReport(t *testing.T) {
	// Valid report.
	report := &AttackReport{Detected: true, Severity: SeverityHigh, CurrentEpoch: 10, FinalizedEpoch: 8}
	if err := ValidateAttackReport(report); err != nil {
		t.Errorf("valid report: %v", err)
	}

	// Nil.
	if err := ValidateAttackReport(nil); err == nil {
		t.Error("expected error for nil report")
	}

	// Detected but no severity.
	bad := &AttackReport{Detected: true, Severity: SeverityNone}
	if err := ValidateAttackReport(bad); err == nil {
		t.Error("expected error for detected with no severity")
	}
}
