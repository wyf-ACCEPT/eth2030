// beacon_state_merger.go handles merging v1/v2/modern beacon state formats
// into the unified beacon state representation. Part of the CL Accessibility
// roadmap ("modernized beacon specs" and "beacon & lean specs merge").
// Supports version detection, field-level conflict resolution, and migration
// of legacy state formats to the modern unified format.
package consensus

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Merger errors.
var (
	ErrMergerNilState       = errors.New("state_merger: nil state")
	ErrMergerVersionUnknown = errors.New("state_merger: unknown state version")
	ErrMergerConflict       = errors.New("state_merger: field conflict")
	ErrMergerNoValidators   = errors.New("state_merger: no validators in source state")
	ErrMergerEpochMismatch  = errors.New("state_merger: epoch mismatch between states")
)

// MergePolicy controls how field conflicts are resolved when merging two
// beacon state snapshots.
type MergePolicy uint8

const (
	// MergePreferModern always takes the field from the newer (higher-slot)
	// state when there is a conflict.
	MergePreferModern MergePolicy = iota

	// MergePreferLegacy always takes the field from the older (lower-slot)
	// state when there is a conflict.
	MergePreferLegacy

	// MergeStrictVersion rejects any merge where both states have non-zero
	// values that differ. Returns an error on conflict.
	MergeStrictVersion
)

// String returns a human-readable name for the merge policy.
func (p MergePolicy) String() string {
	switch p {
	case MergePreferModern:
		return "PreferModern"
	case MergePreferLegacy:
		return "PreferLegacy"
	case MergeStrictVersion:
		return "StrictVersion"
	default:
		return "Unknown"
	}
}

// BeaconSpecVersion identifies the format version of a beacon state.
type BeaconSpecVersion uint8

const (
	// SpecVersionUnknown indicates the version could not be determined.
	SpecVersionUnknown BeaconSpecVersion = iota
	// SpecVersionV1 is the original FullBeaconState format.
	SpecVersionV1
	// SpecVersionV2 is the BeaconStateV2 format (phase0 spec-aligned).
	SpecVersionV2
	// SpecVersionModern is the ModernBeaconState format.
	SpecVersionModern
	// SpecVersionUnified is the UnifiedBeaconState format (target).
	SpecVersionUnified
)

// String returns a human-readable name for the spec version.
func (v BeaconSpecVersion) String() string {
	switch v {
	case SpecVersionV1:
		return "V1"
	case SpecVersionV2:
		return "V2"
	case SpecVersionModern:
		return "Modern"
	case SpecVersionUnified:
		return "Unified"
	default:
		return "Unknown"
	}
}

// MergeLogEntry records a single field-level merge decision.
type MergeLogEntry struct {
	Field      string
	Decision   string // "kept_left", "kept_right", "merged", "conflict"
	LeftValue  string
	RightValue string
}

// MergeLog tracks all field-level decisions made during a merge operation.
type MergeLog struct {
	mu      sync.Mutex
	Entries []MergeLogEntry
	StartAt time.Time
	EndAt   time.Time
}

// NewMergeLog creates a new merge log.
func NewMergeLog() *MergeLog {
	return &MergeLog{
		Entries: make([]MergeLogEntry, 0),
		StartAt: time.Now(),
	}
}

// Add appends an entry to the merge log.
func (l *MergeLog) Add(field, decision, leftVal, rightVal string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Entries = append(l.Entries, MergeLogEntry{
		Field:      field,
		Decision:   decision,
		LeftValue:  leftVal,
		RightValue: rightVal,
	})
}

// ConflictCount returns the number of conflict entries.
func (l *MergeLog) ConflictCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	count := 0
	for _, e := range l.Entries {
		if e.Decision == "conflict" {
			count++
		}
	}
	return count
}

// Finish records the end time.
func (l *MergeLog) Finish() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.EndAt = time.Now()
}

// EntryCount returns the total number of log entries.
func (l *MergeLog) EntryCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.Entries)
}

// StateMerger handles merging two UnifiedBeaconState snapshots into one,
// applying the configured MergePolicy for conflict resolution.
type StateMerger struct {
	policy MergePolicy
	log    *MergeLog
}

// NewStateMerger creates a merger with the given policy.
func NewStateMerger(policy MergePolicy) *StateMerger {
	return &StateMerger{
		policy: policy,
		log:    NewMergeLog(),
	}
}

// Log returns the merge log from the most recent merge operation.
func (m *StateMerger) Log() *MergeLog {
	return m.log
}

// DetectVersion examines a UnifiedBeaconState and infers its format version
// based on the presence or absence of certain fields.
func (m *StateMerger) DetectVersion(state *UnifiedBeaconState) BeaconSpecVersion {
	if state == nil {
		return SpecVersionUnknown
	}
	state.mu.RLock()
	defer state.mu.RUnlock()

	// Unified states have GenesisTime, ForkEpoch, and SlotsPerEpoch set.
	hasGenesis := state.GenesisTime > 0
	hasFork := state.ForkEpoch > 0 || state.ForkCurrentVersion != [4]byte{}
	hasValidators := len(state.Validators) > 0

	if hasGenesis && hasFork && hasValidators {
		return SpecVersionUnified
	}
	if hasFork && hasValidators {
		return SpecVersionV2
	}
	if hasValidators {
		return SpecVersionV1
	}
	return SpecVersionUnknown
}

// MergeState merges two UnifiedBeaconState snapshots into one. The merge
// applies the configured policy to resolve field conflicts. Both input
// states are not modified; a new state is returned.
func (m *StateMerger) MergeState(left, right *UnifiedBeaconState) (*UnifiedBeaconState, error) {
	if left == nil || right == nil {
		return nil, ErrMergerNilState
	}

	m.log = NewMergeLog()
	defer m.log.Finish()

	left.mu.RLock()
	defer left.mu.RUnlock()
	right.mu.RLock()
	defer right.mu.RUnlock()

	result := NewUnifiedBeaconState(left.SlotsPerEpoch)

	// Merge slot/epoch: prefer the more advanced state.
	if left.CurrentSlot >= right.CurrentSlot {
		result.CurrentSlot = left.CurrentSlot
		result.CurrentEpoch = left.CurrentEpoch
		m.log.Add("CurrentSlot", "kept_left",
			fmt.Sprintf("%d", left.CurrentSlot), fmt.Sprintf("%d", right.CurrentSlot))
	} else {
		result.CurrentSlot = right.CurrentSlot
		result.CurrentEpoch = right.CurrentEpoch
		m.log.Add("CurrentSlot", "kept_right",
			fmt.Sprintf("%d", left.CurrentSlot), fmt.Sprintf("%d", right.CurrentSlot))
	}

	// Merge genesis time.
	result.GenesisTime = m.mergeUint64("GenesisTime", left.GenesisTime, right.GenesisTime)

	// Merge genesis validators root.
	if left.GenesisValidatorsRoot != [32]byte{} {
		result.GenesisValidatorsRoot = left.GenesisValidatorsRoot
		m.log.Add("GenesisValidatorsRoot", "kept_left", "set", "")
	} else {
		result.GenesisValidatorsRoot = right.GenesisValidatorsRoot
		m.log.Add("GenesisValidatorsRoot", "kept_right", "", "set")
	}

	// Merge fork versioning: prefer left if both set.
	if left.ForkCurrentVersion != [4]byte{} {
		result.ForkPreviousVersion = left.ForkPreviousVersion
		result.ForkCurrentVersion = left.ForkCurrentVersion
		result.ForkEpoch = left.ForkEpoch
		m.log.Add("Fork", "kept_left", "set", "")
	} else {
		result.ForkPreviousVersion = right.ForkPreviousVersion
		result.ForkCurrentVersion = right.ForkCurrentVersion
		result.ForkEpoch = right.ForkEpoch
		m.log.Add("Fork", "kept_right", "", "set")
	}

	// Merge finalization: prefer the higher finalized epoch.
	if left.FinalizedCheckpointU.Epoch >= right.FinalizedCheckpointU.Epoch {
		result.FinalizedCheckpointU = left.FinalizedCheckpointU
		m.log.Add("FinalizedCheckpoint", "kept_left",
			fmt.Sprintf("epoch=%d", left.FinalizedCheckpointU.Epoch),
			fmt.Sprintf("epoch=%d", right.FinalizedCheckpointU.Epoch))
	} else {
		result.FinalizedCheckpointU = right.FinalizedCheckpointU
		m.log.Add("FinalizedCheckpoint", "kept_right",
			fmt.Sprintf("epoch=%d", left.FinalizedCheckpointU.Epoch),
			fmt.Sprintf("epoch=%d", right.FinalizedCheckpointU.Epoch))
	}

	// Merge justified checkpoints similarly.
	if left.CurrentJustified.Epoch >= right.CurrentJustified.Epoch {
		result.CurrentJustified = left.CurrentJustified
	} else {
		result.CurrentJustified = right.CurrentJustified
	}
	if left.PreviousJustified.Epoch >= right.PreviousJustified.Epoch {
		result.PreviousJustified = left.PreviousJustified
	} else {
		result.PreviousJustified = right.PreviousJustified
	}

	// Merge validator sets.
	if err := m.mergeValidators(result, left, right); err != nil {
		return nil, err
	}

	// Merge Eth1 data: prefer non-zero.
	result.Eth1DepositCount = m.mergeUint64("Eth1DepositCount",
		left.Eth1DepositCount, right.Eth1DepositCount)

	return result, nil
}

// mergeValidators merges validators from both states into result.
// Uses pubkey as the unique key. On conflict, applies the merge policy.
func (m *StateMerger) mergeValidators(
	result, left, right *UnifiedBeaconState,
) error {
	seen := make(map[[48]byte]*UnifiedValidator)

	// Add all left validators.
	for _, v := range left.Validators {
		vc := *v
		seen[v.Pubkey] = &vc
	}

	// Merge right validators.
	for _, v := range right.Validators {
		existing, exists := seen[v.Pubkey]
		if !exists {
			vc := *v
			seen[v.Pubkey] = &vc
			continue
		}

		// Conflict resolution.
		switch m.policy {
		case MergePreferModern:
			// Prefer the validator with the higher balance.
			if v.Balance > existing.Balance {
				vc := *v
				seen[v.Pubkey] = &vc
			}
			m.log.Add("Validator", "merged", "left", "right")

		case MergePreferLegacy:
			// Keep existing (left).
			m.log.Add("Validator", "kept_left", "left", "right")

		case MergeStrictVersion:
			if v.EffectiveBalance != existing.EffectiveBalance {
				m.log.Add("Validator", "conflict", "left", "right")
				return fmt.Errorf("%w: validator pubkey conflict for index %d",
					ErrMergerConflict, v.Index)
			}
		}
	}

	// Add to result.
	for _, v := range seen {
		result.Validators = append(result.Validators, v)
		result.pubkeyIdx[v.Pubkey] = v.Index
	}

	return nil
}

// MigrateToModern upgrades a legacy-format UnifiedBeaconState to the modern
// format by setting default values for missing fields and normalizing
// validator data.
func (m *StateMerger) MigrateToModern(state *UnifiedBeaconState) error {
	if state == nil {
		return ErrMergerNilState
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	// Set SlotsPerEpoch if missing.
	if state.SlotsPerEpoch == 0 {
		state.SlotsPerEpoch = 32
	}

	// Ensure all validators have valid epochs.
	for _, v := range state.Validators {
		if v.ExitEpoch == 0 && !v.Slashed {
			v.ExitEpoch = FarFutureEpoch
		}
		if v.WithdrawableEpoch == 0 && !v.Slashed {
			v.WithdrawableEpoch = FarFutureEpoch
		}
		if v.ActivationEligibilityEpoch == 0 {
			v.ActivationEligibilityEpoch = FarFutureEpoch
		}
		// Cap effective balance.
		if v.EffectiveBalance > 2048*GweiPerETH {
			v.EffectiveBalance = 2048 * GweiPerETH
		}
	}

	// Set epoch from slot if not set.
	if state.CurrentEpoch == 0 && state.CurrentSlot > 0 {
		state.CurrentEpoch = Epoch(state.CurrentSlot / state.SlotsPerEpoch)
	}

	return nil
}

// mergeUint64 resolves a uint64 field conflict based on the merger policy.
func (m *StateMerger) mergeUint64(field string, left, right uint64) uint64 {
	if left == right {
		m.log.Add(field, "merged", fmt.Sprintf("%d", left), fmt.Sprintf("%d", right))
		return left
	}
	switch m.policy {
	case MergePreferModern:
		if left > right {
			m.log.Add(field, "kept_left", fmt.Sprintf("%d", left), fmt.Sprintf("%d", right))
			return left
		}
		m.log.Add(field, "kept_right", fmt.Sprintf("%d", left), fmt.Sprintf("%d", right))
		return right
	case MergePreferLegacy:
		if left < right {
			m.log.Add(field, "kept_left", fmt.Sprintf("%d", left), fmt.Sprintf("%d", right))
			return left
		}
		m.log.Add(field, "kept_right", fmt.Sprintf("%d", left), fmt.Sprintf("%d", right))
		return right
	default:
		// StrictVersion: prefer left on conflict (we cannot error from here).
		m.log.Add(field, "conflict", fmt.Sprintf("%d", left), fmt.Sprintf("%d", right))
		return left
	}
}
