// migration_scheduler.go provides a fork-aware, batch-processing migration
// scheduler. It manages versioned migration campaigns that activate at fork
// boundaries, process accounts in batches, track progress, and support
// rollback. Addresses gap #12 (Tech debt reset).
package state

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Scheduler errors.
var (
	ErrSchedNilState        = errors.New("migration_scheduler: nil state")
	ErrSchedNilConfig       = errors.New("migration_scheduler: nil config")
	ErrSchedNoMigrations    = errors.New("migration_scheduler: no migrations registered")
	ErrSchedVersionExists   = errors.New("migration_scheduler: version already registered")
	ErrSchedVersionNotFound = errors.New("migration_scheduler: version not found")
	ErrSchedInvalidVersion  = errors.New("migration_scheduler: invalid version (must be > 0)")
	ErrSchedAlreadyRunning  = errors.New("migration_scheduler: migration already in progress")
	ErrSchedNotStarted      = errors.New("migration_scheduler: no migration in progress")
	ErrSchedRollbackFailed  = errors.New("migration_scheduler: rollback failed")
	ErrSchedForkNotActive   = errors.New("migration_scheduler: fork not yet active")
)

// ForkBoundary identifies a protocol fork at which migrations activate.
type ForkBoundary struct {
	Name       string
	ActivateAt uint64 // epoch at which the fork activates
}

// VersionedMigration describes a state schema migration bound to a fork.
type VersionedMigration struct {
	FromVersion      int
	ToVersion        int
	Fork             ForkBoundary
	Description      string
	TransformAccount func(addr types.Address, db StateDB) (modified bool, err error)
	Verify           func(addr types.Address, db StateDB) error
}

// SchedulerConfig configures the migration scheduler.
type SchedulerConfig struct {
	BatchSize        int
	CurrentForkEpoch uint64
	CurrentVersion   int
	DryRun           bool
}

// DefaultSchedulerConfig returns sensible defaults.
func DefaultSchedulerConfig() *SchedulerConfig {
	return &SchedulerConfig{BatchSize: 1000, CurrentVersion: 1}
}

// MigrationProgress tracks the progress of an active migration.
type MigrationProgress struct {
	FromVersion, ToVersion         int
	TotalAccounts, MigratedAccounts int
	ErrorCount, BatchesRun         int
	PercentComplete                float64
	StartedAt, LastBatchAt         time.Time
	EstimatedDoneAt                time.Time
	Errors                         []MigrationError
}

// MigrationError records an error for a specific account.
type MigrationError struct {
	Address types.Address
	Message string
}

// MigrationCostEstimate provides pre-migration cost analysis.
type MigrationCostEstimate struct {
	FromVersion, ToVersion int
	Steps, TotalAccounts   int
	EstimatedBatches       int
	EstimatedTimeMs        int64
	ForkRequired           string
	IsReady                bool
}

// RollbackSnapshot stores pre-migration state for undo support.
type RollbackSnapshot struct {
	Version       int
	AccountStates map[types.Address]*accountSnapshot
	CreatedAt     time.Time
}

type accountSnapshot struct {
	Balance *big.Int
	Nonce   uint64
	Code    []byte
	Storage map[types.Hash]types.Hash
}

// StateMigrationScheduler manages fork-aware state migrations. Thread-safe.
type StateMigrationScheduler struct {
	mu          sync.Mutex
	config      *SchedulerConfig
	migrations  map[int]*VersionedMigration
	active      *MigrationProgress
	running     bool
	rollback    *RollbackSnapshot
	accountList []types.Address
	batchCursor int
}

// NewStateMigrationScheduler creates a new scheduler with the given config.
func NewStateMigrationScheduler(config *SchedulerConfig) (*StateMigrationScheduler, error) {
	if config == nil {
		return nil, ErrSchedNilConfig
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 1000
	}
	return &StateMigrationScheduler{
		config:     config,
		migrations: make(map[int]*VersionedMigration),
	}, nil
}

// RegisterMigration adds a versioned migration to the scheduler.
func (s *StateMigrationScheduler) RegisterMigration(m *VersionedMigration) error {
	if m == nil {
		return ErrSchedNilConfig
	}
	if m.FromVersion <= 0 || m.ToVersion <= 0 {
		return ErrSchedInvalidVersion
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.migrations[m.FromVersion]; exists {
		return ErrSchedVersionExists
	}
	s.migrations[m.FromVersion] = m
	return nil
}

// PlanMigration returns a cost estimate without executing anything.
func (s *StateMigrationScheduler) PlanMigration(targetVersion int, db *MemoryStateDB) (*MigrationCostEstimate, error) {
	if db == nil {
		return nil, ErrSchedNilState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	chain, err := s.buildChain(s.config.CurrentVersion, targetVersion)
	if err != nil {
		return nil, err
	}
	total := len(db.stateObjects)
	batches := (total + s.config.BatchSize - 1) / s.config.BatchSize
	if batches == 0 {
		batches = 1
	}
	allReady := true
	forkReq := ""
	for _, m := range chain {
		if m.Fork.ActivateAt > s.config.CurrentForkEpoch {
			allReady = false
			forkReq = m.Fork.Name
			break
		}
	}
	return &MigrationCostEstimate{
		FromVersion: s.config.CurrentVersion, ToVersion: targetVersion,
		Steps: len(chain), TotalAccounts: total, EstimatedBatches: batches,
		EstimatedTimeMs: int64(total * len(chain)), ForkRequired: forkReq, IsReady: allReady,
	}, nil
}

// StartMigration begins a migration from current version to target.
func (s *StateMigrationScheduler) StartMigration(targetVersion int, db *MemoryStateDB) error {
	if db == nil {
		return ErrSchedNilState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return ErrSchedAlreadyRunning
	}
	chain, err := s.buildChain(s.config.CurrentVersion, targetVersion)
	if err != nil {
		return err
	}
	if len(chain) > 0 && chain[0].Fork.ActivateAt > s.config.CurrentForkEpoch {
		return fmt.Errorf("%w: need %s (epoch %d)", ErrSchedForkNotActive,
			chain[0].Fork.Name, chain[0].Fork.ActivateAt)
	}
	accounts := make([]types.Address, 0, len(db.stateObjects))
	for addr := range db.stateObjects {
		accounts = append(accounts, addr)
	}
	snap := &RollbackSnapshot{
		Version:       s.config.CurrentVersion,
		AccountStates: make(map[types.Address]*accountSnapshot, len(accounts)),
		CreatedAt:     time.Now(),
	}
	for _, addr := range accounts {
		snap.AccountStates[addr] = captureSnap(db, addr)
	}
	s.rollback = snap
	s.accountList = accounts
	s.batchCursor = 0
	s.running = true
	s.active = &MigrationProgress{
		FromVersion: s.config.CurrentVersion, ToVersion: targetVersion,
		TotalAccounts: len(accounts), StartedAt: time.Now(),
	}
	return nil
}

// ProcessNextBatch migrates the next batch of accounts.
func (s *StateMigrationScheduler) ProcessNextBatch(db *MemoryStateDB) (*MigrationProgress, error) {
	if db == nil {
		return nil, ErrSchedNilState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.active == nil {
		return nil, ErrSchedNotStarted
	}
	chain, err := s.buildChain(s.active.FromVersion, s.active.ToVersion)
	if err != nil {
		return nil, err
	}
	step := 0
	if len(chain) > 1 {
		maxAcc := s.active.TotalAccounts
		if maxAcc == 0 {
			maxAcc = 1
		}
		step = (s.active.BatchesRun * s.config.BatchSize / maxAcc) % len(chain)
	}
	if step >= len(chain) {
		step = len(chain) - 1
	}
	migration := chain[step]
	end := s.batchCursor + s.config.BatchSize
	if end > len(s.accountList) {
		end = len(s.accountList)
	}
	batchErrs := 0
	for _, addr := range s.accountList[s.batchCursor:end] {
		if s.config.DryRun {
			s.active.MigratedAccounts++
			continue
		}
		if migration.TransformAccount != nil {
			_, tErr := migration.TransformAccount(addr, db)
			if tErr != nil {
				batchErrs++
				s.active.Errors = append(s.active.Errors, MigrationError{addr, tErr.Error()})
				continue
			}
		}
		s.active.MigratedAccounts++
		if migration.Verify != nil && !s.config.DryRun {
			if vErr := migration.Verify(addr, db); vErr != nil {
				batchErrs++
				s.active.Errors = append(s.active.Errors, MigrationError{addr, "verify: " + vErr.Error()})
			}
		}
	}
	s.active.ErrorCount += batchErrs
	s.active.BatchesRun++
	s.active.LastBatchAt = time.Now()
	s.batchCursor = end
	if s.active.TotalAccounts > 0 {
		s.active.PercentComplete = float64(s.active.MigratedAccounts) / float64(s.active.TotalAccounts) * 100.0
	}
	if s.active.BatchesRun > 0 && s.active.MigratedAccounts < s.active.TotalAccounts {
		elapsed := time.Since(s.active.StartedAt).Seconds()
		if elapsed > 0 {
			rate := float64(s.active.MigratedAccounts) / elapsed
			if rate > 0 {
				rem := float64(s.active.TotalAccounts-s.active.MigratedAccounts) / rate
				s.active.EstimatedDoneAt = time.Now().Add(time.Duration(rem * float64(time.Second)))
			}
		}
	}
	if s.batchCursor >= len(s.accountList) {
		s.running = false
		s.config.CurrentVersion = s.active.ToVersion
		s.active.PercentComplete = 100.0
	}
	return s.copyProgress(), nil
}

// Rollback undoes the last migration by restoring the snapshot.
func (s *StateMigrationScheduler) Rollback(db *MemoryStateDB) error {
	if db == nil {
		return ErrSchedNilState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rollback == nil {
		return ErrSchedRollbackFailed
	}
	for addr, snap := range s.rollback.AccountStates {
		restoreSnap(db, addr, snap)
	}
	s.config.CurrentVersion = s.rollback.Version
	s.running, s.active, s.rollback, s.accountList, s.batchCursor = false, nil, nil, nil, 0
	return nil
}

// GetProgress returns the current migration progress, or nil.
func (s *StateMigrationScheduler) GetProgress() *MigrationProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.copyProgress()
}

// IsRunning returns whether a migration is in progress.
func (s *StateMigrationScheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// CurrentVersion returns the current state schema version.
func (s *StateMigrationScheduler) CurrentVersion() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config.CurrentVersion
}

// MigrationCount returns the number of registered migrations.
func (s *StateMigrationScheduler) MigrationCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.migrations)
}

// HasRollback returns whether a rollback snapshot is available.
func (s *StateMigrationScheduler) HasRollback() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rollback != nil
}

// SetForkEpoch updates the current fork epoch for activation checks.
func (s *StateMigrationScheduler) SetForkEpoch(epoch uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.CurrentForkEpoch = epoch
}

// GetMigration returns the migration registered for a source version.
func (s *StateMigrationScheduler) GetMigration(fromVersion int) *VersionedMigration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.migrations[fromVersion]
}

// --- Internal helpers ---

func (s *StateMigrationScheduler) buildChain(from, to int) ([]*VersionedMigration, error) {
	if from >= to {
		return nil, ErrSchedNoMigrations
	}
	var chain []*VersionedMigration
	cur := from
	for cur < to {
		m, ok := s.migrations[cur]
		if !ok {
			return nil, fmt.Errorf("%w: no migration from version %d", ErrSchedVersionNotFound, cur)
		}
		chain = append(chain, m)
		cur = m.ToVersion
	}
	return chain, nil
}

func (s *StateMigrationScheduler) copyProgress() *MigrationProgress {
	if s.active == nil {
		return nil
	}
	cp := *s.active
	if len(s.active.Errors) > 0 {
		cp.Errors = make([]MigrationError, len(s.active.Errors))
		copy(cp.Errors, s.active.Errors)
	}
	return &cp
}

func captureSnap(db *MemoryStateDB, addr types.Address) *accountSnapshot {
	snap := &accountSnapshot{
		Balance: new(big.Int).Set(db.GetBalance(addr)),
		Nonce:   db.GetNonce(addr),
		Code:    append([]byte(nil), db.GetCode(addr)...),
		Storage: make(map[types.Hash]types.Hash),
	}
	if obj, ok := db.stateObjects[addr]; ok {
		for k, v := range mergeStorage(obj) {
			snap.Storage[k] = v
		}
	}
	return snap
}

func restoreSnap(db *MemoryStateDB, addr types.Address, snap *accountSnapshot) {
	if snap == nil {
		return
	}
	if !db.Exist(addr) {
		db.CreateAccount(addr)
	}
	cur := db.GetBalance(addr)
	if cur.Sign() > 0 {
		db.SubBalance(addr, cur)
	}
	if snap.Balance.Sign() > 0 {
		db.AddBalance(addr, snap.Balance)
	}
	db.SetNonce(addr, snap.Nonce)
	db.SetCode(addr, snap.Code)
	// Clear dirty slots that were added during migration.
	if obj, ok := db.stateObjects[addr]; ok {
		for k := range obj.dirtyStorage {
			if _, inSnap := snap.Storage[k]; !inSnap {
				db.SetState(addr, k, types.Hash{})
			}
		}
	}
	for k, v := range snap.Storage {
		db.SetState(addr, k, v)
	}
}

// --- Built-in migration factories ---

// V1ToV2Migration converts legacy gas tracking to multidimensional gas (EIP-7706).
func V1ToV2Migration() *VersionedMigration {
	return &VersionedMigration{
		FromVersion: 1, ToVersion: 2,
		Fork: ForkBoundary{Name: "Glamsterdam", ActivateAt: 0},
		Description: "Convert legacy gas to multidimensional gas model",
		TransformAccount: func(addr types.Address, db StateDB) (bool, error) {
			legacy := types.BytesToHash([]byte{0x01})
			val := db.GetState(addr, legacy)
			if val == (types.Hash{}) {
				return false, nil
			}
			db.SetState(addr, types.BytesToHash([]byte{0x10}), val)
			db.SetState(addr, legacy, types.Hash{})
			return true, nil
		},
		Verify: func(addr types.Address, db StateDB) error {
			if db.GetState(addr, types.BytesToHash([]byte{0x01})) != (types.Hash{}) {
				return fmt.Errorf("legacy slot not cleared for %v", addr)
			}
			return nil
		},
	}
}

// V2ToV3Migration removes deprecated self-destruct markers. Activates at Hogota.
func V2ToV3Migration() *VersionedMigration {
	return &VersionedMigration{
		FromVersion: 2, ToVersion: 3,
		Fork: ForkBoundary{Name: "Hogota", ActivateAt: 0},
		Description: "Remove deprecated self-destruct markers",
		TransformAccount: func(addr types.Address, db StateDB) (bool, error) {
			return db.HasSelfDestructed(addr) || db.Empty(addr), nil
		},
	}
}

// V3ToV4Migration converts nonce to announce-nonce format (EIP-8077). Activates at I+.
func V3ToV4Migration() *VersionedMigration {
	return &VersionedMigration{
		FromVersion: 3, ToVersion: 4,
		Fork: ForkBoundary{Name: "I+", ActivateAt: 100},
		Description: "Convert nonce to announce-nonce format",
		TransformAccount: func(addr types.Address, db StateDB) (bool, error) {
			nonce := db.GetNonce(addr)
			if nonce == 0 {
				return false, nil
			}
			var h types.Hash
			h[31] = byte(nonce & 0xFF)
			h[30] = byte((nonce >> 8) & 0xFF)
			db.SetState(addr, types.BytesToHash([]byte{0x20}), h)
			return true, nil
		},
	}
}

// DefaultVersionedMigrations returns the built-in v1->v2->v3->v4 chain.
func DefaultVersionedMigrations() []*VersionedMigration {
	return []*VersionedMigration{V1ToV2Migration(), V2ToV3Migration(), V3ToV4Migration()}
}
