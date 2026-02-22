package consensus

// tech_debt_reset.go implements tracking of deprecated beacon state fields
// and automated migration as part of the CL Accessibility roadmap ("tech
// debt reset"). The TechDebtTracker manages a registry of deprecated fields,
// their replacements, and removal epochs, enabling clean state migrations
// across protocol upgrades.

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Errors returned by tech debt tracking operations.
var (
	ErrTechDebtNilField      = errors.New("tech_debt: nil deprecated field")
	ErrTechDebtEmptyName     = errors.New("tech_debt: empty field name")
	ErrTechDebtDuplicate     = errors.New("tech_debt: field already registered")
	ErrTechDebtInvalidEpochs = errors.New("tech_debt: removal epoch must be >= deprecation epoch")
)

// DeprecatedField describes a beacon state field that has been deprecated.
// It records when the field was deprecated, what replaces it, and when it
// will be fully removed.
type DeprecatedField struct {
	FieldName            string   // canonical field name being deprecated
	DeprecatedSinceEpoch uint64   // epoch at which the field became deprecated
	ReplacedBy           []string // zero or more replacement field names
	RemovalEpoch         uint64   // epoch at which the field will be removed (0 = no planned removal)
}

// TechDebtConfig holds configuration for the tech debt reset system,
// including a list of well-known deprecated fields and their replacements.
type TechDebtConfig struct {
	// KnownDeprecations is a set of pre-configured deprecations that
	// are loaded at initialization.
	KnownDeprecations []*DeprecatedField

	// AutoMigrate controls whether MigrateState automatically removes
	// fields past their removal epoch. If false, only replacements are
	// applied; removed fields must be cleaned up explicitly.
	AutoMigrate bool
}

// Altair upgrade epoch (mainnet epoch 74240). Used as the deprecation epoch
// for fields replaced by the Altair participation flag scheme.
const AltairEpoch = 74240

// Phase1RemovalEpoch is a synthetic epoch representing the removal of Phase 1
// shard-related fields that were never activated on mainnet.
const Phase1RemovalEpoch = 74240

// DefaultTechDebtConfig returns a config pre-populated with well-known
// deprecated beacon state fields from real Ethereum spec upgrades and
// auto-migration enabled.
func DefaultTechDebtConfig() *TechDebtConfig {
	return &TechDebtConfig{
		AutoMigrate: true,
		KnownDeprecations: []*DeprecatedField{
			{
				// Altair replaced per-epoch PendingAttestation lists with
				// per-validator participation flag bytes.
				FieldName:            "previous_epoch_attestations",
				DeprecatedSinceEpoch: AltairEpoch,
				ReplacedBy:           []string{"previous_epoch_participation"},
				RemovalEpoch:         AltairEpoch + 8192,
			},
			{
				FieldName:            "current_epoch_attestations",
				DeprecatedSinceEpoch: AltairEpoch,
				ReplacedBy:           []string{"current_epoch_participation"},
				RemovalEpoch:         AltairEpoch + 8192,
			},
			{
				// compact_committees_root was proposed in early Phase 1 designs
				// but never activated. Removed from the spec entirely.
				FieldName:            "compact_committees_root",
				DeprecatedSinceEpoch: Phase1RemovalEpoch,
				RemovalEpoch:         Phase1RemovalEpoch,
			},
			{
				// shard_states tracked per-shard beacon state in Phase 1
				// crosslink designs. Removed when Phase 1 was abandoned.
				FieldName:            "shard_states",
				DeprecatedSinceEpoch: Phase1RemovalEpoch,
				RemovalEpoch:         Phase1RemovalEpoch,
			},
			{
				// latest_crosslinks stored the most recent crosslink per shard.
				// Removed when the crosslink mechanism was replaced by the
				// data availability sampling approach (EIP-4844+).
				FieldName:            "latest_crosslinks",
				DeprecatedSinceEpoch: Phase1RemovalEpoch,
				RemovalEpoch:         Phase1RemovalEpoch,
			},
		},
	}
}

// MigrationReport tracks the outcome of a MigrateState operation.
type MigrationReport struct {
	// FieldsMigrated counts how many deprecated field values were copied
	// to their replacement fields.
	FieldsMigrated int

	// FieldsRemoved counts how many deprecated fields were deleted from
	// the state because they passed their removal epoch.
	FieldsRemoved int

	// Errors collects any non-fatal issues encountered during migration
	// (e.g., a replacement field already existing).
	Errors []string
}

// TechDebtTracker tracks deprecated fields in the beacon state and provides
// methods for checking deprecation status, getting replacements, and
// migrating state data. Thread-safe.
type TechDebtTracker struct {
	mu     sync.RWMutex
	fields map[string]*DeprecatedField
	config *TechDebtConfig
}

// NewTechDebtTracker creates a new tracker with the given config. If config
// is nil, DefaultTechDebtConfig is used. Any known deprecations in the
// config are registered automatically.
func NewTechDebtTracker(config *TechDebtConfig) *TechDebtTracker {
	if config == nil {
		config = DefaultTechDebtConfig()
	}
	t := &TechDebtTracker{
		fields: make(map[string]*DeprecatedField),
		config: config,
	}
	for _, d := range config.KnownDeprecations {
		if d != nil && d.FieldName != "" {
			// Pre-load known deprecations; ignore errors for robustness.
			t.fields[d.FieldName] = copyDeprecatedField(d)
		}
	}
	return t
}

// RegisterDeprecation registers a new deprecated field. Returns an error
// if the field is nil, has an empty name, is already registered, or has
// an invalid epoch configuration.
func (t *TechDebtTracker) RegisterDeprecation(field *DeprecatedField) error {
	if field == nil {
		return ErrTechDebtNilField
	}
	if field.FieldName == "" {
		return ErrTechDebtEmptyName
	}
	if field.RemovalEpoch != 0 && field.RemovalEpoch < field.DeprecatedSinceEpoch {
		return ErrTechDebtInvalidEpochs
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.fields[field.FieldName]; exists {
		return ErrTechDebtDuplicate
	}

	t.fields[field.FieldName] = copyDeprecatedField(field)
	return nil
}

// IsDeprecated checks whether a field is deprecated at the given epoch.
// A field is deprecated if it was registered and the current epoch is
// at or past its deprecation epoch.
func (t *TechDebtTracker) IsDeprecated(fieldName string, currentEpoch uint64) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	f, ok := t.fields[fieldName]
	if !ok {
		return false
	}
	return currentEpoch >= f.DeprecatedSinceEpoch
}

// GetReplacements returns the replacement field names for the given
// deprecated field. Returns nil if the field is not registered or has
// no replacements.
func (t *TechDebtTracker) GetReplacements(fieldName string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	f, ok := t.fields[fieldName]
	if !ok {
		return nil
	}
	if len(f.ReplacedBy) == 0 {
		return nil
	}
	// Return a copy to prevent external mutation.
	cp := make([]string, len(f.ReplacedBy))
	copy(cp, f.ReplacedBy)
	return cp
}

// MigrateState migrates a state map by applying replacements for deprecated
// fields. For each deprecated field present in the state whose deprecation
// epoch has passed, its value is copied to each replacement field (if the
// replacement does not already exist). If AutoMigrate is enabled, fields
// past their removal epoch are also deleted.
//
// Returns the migrated state, a MigrationReport with statistics, and any
// fatal error encountered.
func (t *TechDebtTracker) MigrateState(state map[string]interface{}, currentEpoch uint64) (map[string]interface{}, *MigrationReport, error) {
	if state == nil {
		return nil, nil, errors.New("tech_debt: nil state map")
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	report := &MigrationReport{}

	// Build a new state map so we do not mutate the input.
	result := make(map[string]interface{}, len(state))
	for k, v := range state {
		result[k] = v
	}

	// Apply migrations: copy deprecated field values to replacements.
	for name, field := range t.fields {
		if currentEpoch < field.DeprecatedSinceEpoch {
			continue
		}
		val, exists := result[name]
		if !exists {
			continue
		}
		for _, repl := range field.ReplacedBy {
			if _, replExists := result[repl]; !replExists {
				result[repl] = val
				report.FieldsMigrated++
			} else {
				report.Errors = append(report.Errors,
					"replacement field "+repl+" already exists, skipping migration from "+name)
			}
		}
	}

	// Auto-remove fields past removal epoch if configured.
	if t.config.AutoMigrate {
		for name, field := range t.fields {
			if field.RemovalEpoch == 0 {
				continue
			}
			if currentEpoch >= field.RemovalEpoch {
				if _, exists := result[name]; exists {
					delete(result, name)
					report.FieldsRemoved++
				}
			}
		}
	}

	return result, report, nil
}

// DeprecationReport returns all deprecations that are active at the given
// epoch (i.e., deprecated but not yet removed). Results are sorted by
// deprecation epoch.
func (t *TechDebtTracker) DeprecationReport(currentEpoch uint64) []DeprecatedField {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var report []DeprecatedField
	for _, f := range t.fields {
		if currentEpoch < f.DeprecatedSinceEpoch {
			continue // not yet deprecated
		}
		if f.RemovalEpoch != 0 && currentEpoch >= f.RemovalEpoch {
			continue // already removed
		}
		report = append(report, *f)
	}

	sort.Slice(report, func(i, j int) bool {
		if report[i].DeprecatedSinceEpoch != report[j].DeprecatedSinceEpoch {
			return report[i].DeprecatedSinceEpoch < report[j].DeprecatedSinceEpoch
		}
		return report[i].FieldName < report[j].FieldName
	})

	return report
}

// CleanupRemovedFields removes all fields from the state map that are past
// their removal epoch. Returns the number of fields removed.
func (t *TechDebtTracker) CleanupRemovedFields(state map[string]interface{}, currentEpoch uint64) int {
	if state == nil {
		return 0
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	removed := 0
	for name, field := range t.fields {
		if field.RemovalEpoch == 0 {
			continue
		}
		if currentEpoch < field.RemovalEpoch {
			continue
		}
		if _, exists := state[name]; exists {
			delete(state, name)
			removed++
		}
	}
	return removed
}

// FieldCount returns the total number of registered deprecations.
func (t *TechDebtTracker) FieldCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.fields)
}

// IsRemoved checks whether a field has been fully removed (past its
// removal epoch) at the given epoch.
func (t *TechDebtTracker) IsRemoved(fieldName string, currentEpoch uint64) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	f, ok := t.fields[fieldName]
	if !ok {
		return false
	}
	if f.RemovalEpoch == 0 {
		return false // no removal planned
	}
	return currentEpoch >= f.RemovalEpoch
}

// ValidateMigrationReadiness checks that a state map is ready for migration
// at the given epoch: all deprecated fields have replacements configured,
// and the state contains the deprecated fields that need migration.
func (t *TechDebtTracker) ValidateMigrationReadiness(state map[string]interface{}, currentEpoch uint64) error {
	if state == nil {
		return errors.New("tech_debt: nil state map")
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	for name, field := range t.fields {
		if currentEpoch < field.DeprecatedSinceEpoch {
			continue
		}
		if _, exists := state[name]; !exists {
			continue
		}
		// Field is deprecated and present - check it has replacements.
		if len(field.ReplacedBy) == 0 && field.RemovalEpoch == 0 {
			return fmt.Errorf("tech_debt: field %q deprecated but has no replacement or removal epoch", name)
		}
	}
	return nil
}

// ValidateRollbackCapability checks that a migration can be rolled back:
// for every replacement field in the state, the original deprecated field
// must still be present (not yet removed).
func (t *TechDebtTracker) ValidateRollbackCapability(state map[string]interface{}, currentEpoch uint64) error {
	if state == nil {
		return errors.New("tech_debt: nil state map")
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	for name, field := range t.fields {
		if currentEpoch < field.DeprecatedSinceEpoch {
			continue
		}
		if field.RemovalEpoch != 0 && currentEpoch >= field.RemovalEpoch {
			// Field already removed; rollback not possible.
			for _, repl := range field.ReplacedBy {
				if _, replExists := state[repl]; replExists {
					if _, origExists := state[name]; !origExists {
						return fmt.Errorf("tech_debt: rollback not possible for %q (removed at epoch %d)", name, field.RemovalEpoch)
					}
				}
			}
		}
	}
	return nil
}

// copyDeprecatedField returns a deep copy of a DeprecatedField.
func copyDeprecatedField(f *DeprecatedField) *DeprecatedField {
	cp := &DeprecatedField{
		FieldName:            f.FieldName,
		DeprecatedSinceEpoch: f.DeprecatedSinceEpoch,
		RemovalEpoch:         f.RemovalEpoch,
	}
	if len(f.ReplacedBy) > 0 {
		cp.ReplacedBy = make([]string, len(f.ReplacedBy))
		copy(cp.ReplacedBy, f.ReplacedBy)
	}
	return cp
}
