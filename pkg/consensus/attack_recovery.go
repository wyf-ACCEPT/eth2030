// Package consensus implements Ethereum consensus-layer primitives.
// This file implements 51% attack detection and auto-recovery per the
// CL Accessibility track of the Ethereum 2028 roadmap.
package consensus

import (
	"errors"
	"fmt"
	"sync"
)

// Severity levels for detected attacks.
const (
	SeverityNone     = "none"
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

// Reorg depth thresholds that determine attack severity.
const (
	// ReorgThresholdLow: reorgs deeper than 2 epochs are suspicious.
	ReorgThresholdLow uint64 = 2
	// ReorgThresholdMedium: reorgs deeper than 4 epochs indicate a likely attack.
	ReorgThresholdMedium uint64 = 4
	// ReorgThresholdHigh: reorgs deeper than 8 epochs are a serious attack.
	ReorgThresholdHigh uint64 = 8
	// ReorgThresholdCritical: reorgs deeper than 16 epochs threaten finality.
	ReorgThresholdCritical uint64 = 16
)

// Recommended recovery actions for each severity level.
const (
	ActionNone           = "none"
	ActionMonitor        = "monitor"
	ActionIsolate        = "isolate_attacker_peers"
	ActionFallback       = "fallback_to_finalized_checkpoint"
	ActionSocialOverride = "social_consensus_override"
)

// Errors returned by attack recovery operations.
var (
	ErrNoAttackDetected  = errors.New("consensus: no attack detected, recovery not needed")
	ErrNilRecoveryPlan   = errors.New("consensus: recovery plan is nil")
	ErrAlreadyRecovering = errors.New("consensus: already executing a recovery plan")
	ErrInvalidPlan       = errors.New("consensus: recovery plan has no actions enabled")
)

// AttackReport describes a detected (or non-detected) 51% attack.
type AttackReport struct {
	Detected          bool
	Severity          string
	ReorgDepth        uint64
	FinalizedEpoch    uint64
	CurrentEpoch      uint64
	AffectedEpochs    []uint64
	RecommendedAction string
}

// RecoveryPlan describes the steps to recover from a detected attack.
type RecoveryPlan struct {
	// IsolationMode stops accepting blocks from suspected attacker peers.
	IsolationMode bool
	// FallbackToFinalized reverts the chain head to the last finalized checkpoint.
	FallbackToFinalized bool
	// SocialConsensusOverride flags that manual community intervention is needed.
	SocialConsensusOverride bool
	// FinalizedCheckpointEpoch is the epoch to fall back to.
	FinalizedCheckpointEpoch uint64
	// Severity is the severity that triggered this plan.
	Severity string
}

// RecoveryStatus tracks the state of an ongoing recovery operation.
type RecoveryStatus struct {
	Active              bool
	PeersIsolated       bool
	FellBackToFinalized bool
	SocialOverrideSet   bool
	Plan                *RecoveryPlan
}

// AttackDetector monitors for chain reorganization attacks and coordinates
// auto-recovery. It is safe for concurrent use.
type AttackDetector struct {
	mu             sync.RWMutex
	underAttack    bool
	lastReport     *AttackReport
	recoveryStatus RecoveryStatus
}

// NewAttackDetector creates a new AttackDetector.
func NewAttackDetector() *AttackDetector {
	return &AttackDetector{}
}

// SeverityLevel classifies the attack severity based on reorg depth measured
// in epochs. Deeper reorgs imply a more powerful attacker.
func SeverityLevel(reorgDepth uint64) string {
	switch {
	case reorgDepth >= ReorgThresholdCritical:
		return SeverityCritical
	case reorgDepth >= ReorgThresholdHigh:
		return SeverityHigh
	case reorgDepth >= ReorgThresholdMedium:
		return SeverityMedium
	case reorgDepth >= ReorgThresholdLow:
		return SeverityLow
	default:
		return SeverityNone
	}
}

// recommendedAction returns the recovery action for the given severity.
func recommendedAction(severity string) string {
	switch severity {
	case SeverityCritical:
		return ActionSocialOverride
	case SeverityHigh:
		return ActionFallback
	case SeverityMedium:
		return ActionIsolate
	case SeverityLow:
		return ActionMonitor
	default:
		return ActionNone
	}
}

// affectedEpochs computes the list of epoch numbers affected by a reorg.
// The affected range is [finalizedEpoch+1 .. finalizedEpoch+reorgDepth],
// capped at currentEpoch.
func affectedEpochs(reorgDepth, finalizedEpoch, currentEpoch uint64) []uint64 {
	if reorgDepth == 0 {
		return nil
	}
	start := finalizedEpoch + 1
	end := finalizedEpoch + reorgDepth
	if end > currentEpoch {
		end = currentEpoch
	}
	if start > end {
		return nil
	}
	epochs := make([]uint64, 0, end-start+1)
	for e := start; e <= end; e++ {
		epochs = append(epochs, e)
	}
	return epochs
}

// DetectAttack analyzes a chain reorganization to determine whether it
// constitutes a 51% attack. The reorgDepth is measured in epochs.
// If the reorg goes beyond the finalized checkpoint, it is always suspicious.
func (ad *AttackDetector) DetectAttack(reorgDepth, finalizedEpoch, currentEpoch uint64) *AttackReport {
	severity := SeverityLevel(reorgDepth)
	detected := severity != SeverityNone

	// A reorg that reaches into finalized territory is always critical,
	// regardless of depth thresholds.
	if reorgDepth > 0 && finalizedEpoch > 0 && currentEpoch > finalizedEpoch {
		distToFinalized := currentEpoch - finalizedEpoch
		if reorgDepth >= distToFinalized {
			detected = true
			if severity != SeverityCritical {
				severity = SeverityHigh
			}
		}
	}

	report := &AttackReport{
		Detected:          detected,
		Severity:          severity,
		ReorgDepth:        reorgDepth,
		FinalizedEpoch:    finalizedEpoch,
		CurrentEpoch:      currentEpoch,
		AffectedEpochs:    affectedEpochs(reorgDepth, finalizedEpoch, currentEpoch),
		RecommendedAction: recommendedAction(severity),
	}

	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.underAttack = detected
	ad.lastReport = report
	return report
}

// IsUnderAttack returns true if the detector has identified an ongoing attack.
func (ad *AttackDetector) IsUnderAttack() bool {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	return ad.underAttack
}

// LastReport returns the most recent attack report, or nil if none.
func (ad *AttackDetector) LastReport() *AttackReport {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	return ad.lastReport
}

// BuildRecoveryPlan creates a RecoveryPlan from the given AttackReport.
// Returns an error if no attack was detected.
func BuildRecoveryPlan(report *AttackReport) (*RecoveryPlan, error) {
	if report == nil || !report.Detected {
		return nil, ErrNoAttackDetected
	}
	plan := &RecoveryPlan{
		FinalizedCheckpointEpoch: report.FinalizedEpoch,
		Severity:                 report.Severity,
	}
	switch report.Severity {
	case SeverityCritical:
		plan.IsolationMode = true
		plan.FallbackToFinalized = true
		plan.SocialConsensusOverride = true
	case SeverityHigh:
		plan.IsolationMode = true
		plan.FallbackToFinalized = true
	case SeverityMedium:
		plan.IsolationMode = true
	case SeverityLow:
		// Low severity: monitor only, no automated actions.
	}
	return plan, nil
}

// ExecuteRecovery executes the recovery steps described in the plan.
// In a real implementation, each step would interact with the p2p layer,
// fork-choice store, and operator alerting. This implementation updates
// the detector's internal recovery status.
func (ad *AttackDetector) ExecuteRecovery(plan *RecoveryPlan) error {
	if plan == nil {
		return ErrNilRecoveryPlan
	}
	if !plan.IsolationMode && !plan.FallbackToFinalized && !plan.SocialConsensusOverride {
		return ErrInvalidPlan
	}

	ad.mu.Lock()
	defer ad.mu.Unlock()

	if ad.recoveryStatus.Active {
		return ErrAlreadyRecovering
	}

	ad.recoveryStatus = RecoveryStatus{
		Active: true,
		Plan:   plan,
	}

	// Step 1: Isolate attacker peers if requested.
	if plan.IsolationMode {
		ad.recoveryStatus.PeersIsolated = true
	}

	// Step 2: Revert to finalized checkpoint if requested.
	if plan.FallbackToFinalized {
		ad.recoveryStatus.FellBackToFinalized = true
	}

	// Step 3: Flag social consensus override if requested.
	if plan.SocialConsensusOverride {
		ad.recoveryStatus.SocialOverrideSet = true
	}

	return nil
}

// GetRecoveryStatus returns the current recovery status.
func (ad *AttackDetector) GetRecoveryStatus() RecoveryStatus {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	return ad.recoveryStatus
}

// ClearRecovery resets the attack and recovery state, typically called
// after the network has stabilized.
func (ad *AttackDetector) ClearRecovery() {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.underAttack = false
	ad.recoveryStatus = RecoveryStatus{}
}

// String returns a human-readable summary of the attack report.
func (r *AttackReport) String() string {
	if !r.Detected {
		return "no attack detected"
	}
	return fmt.Sprintf(
		"attack detected: severity=%s reorg_depth=%d finalized=%d current=%d affected_epochs=%d action=%s",
		r.Severity, r.ReorgDepth, r.FinalizedEpoch, r.CurrentEpoch,
		len(r.AffectedEpochs), r.RecommendedAction,
	)
}
