package state

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- Test helpers ---

// setupSchedulerWithState creates a scheduler and a populated MemoryStateDB.
func setupSchedulerWithState(t *testing.T, numAccounts int) (*StateMigrationScheduler, *MemoryStateDB) {
	t.Helper()

	config := DefaultSchedulerConfig()
	config.BatchSize = 10
	config.CurrentVersion = 1

	sched, err := NewStateMigrationScheduler(config)
	if err != nil {
		t.Fatalf("NewStateMigrationScheduler: %v", err)
	}

	// Register migrations v1->v2->v3.
	for _, m := range DefaultVersionedMigrations()[:2] {
		if err := sched.RegisterMigration(m); err != nil {
			t.Fatalf("RegisterMigration: %v", err)
		}
	}

	db := populateTestState(numAccounts, 3)

	// Set the legacy gas slot on some accounts so v1->v2 has work.
	legacySlot := types.BytesToHash([]byte{0x01})
	for addr := range db.stateObjects {
		val := types.BytesToHash([]byte{0xAB})
		db.SetState(addr, legacySlot, val)
		break // just one account
	}

	return sched, db
}

// --- Tests ---

func TestSchedulerCreation(t *testing.T) {
	config := DefaultSchedulerConfig()
	sched, err := NewStateMigrationScheduler(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sched == nil {
		t.Fatal("expected non-nil scheduler")
	}
	if sched.CurrentVersion() != 1 {
		t.Errorf("expected version 1, got %d", sched.CurrentVersion())
	}
}

func TestSchedulerNilConfig(t *testing.T) {
	_, err := NewStateMigrationScheduler(nil)
	if !errors.Is(err, ErrSchedNilConfig) {
		t.Fatalf("expected ErrSchedNilConfig, got %v", err)
	}
}

func TestSchedulerRegisterMigration(t *testing.T) {
	sched, _ := NewStateMigrationScheduler(DefaultSchedulerConfig())

	m := V1ToV2Migration()
	err := sched.RegisterMigration(m)
	if err != nil {
		t.Fatalf("RegisterMigration: %v", err)
	}
	if sched.MigrationCount() != 1 {
		t.Errorf("expected 1 migration, got %d", sched.MigrationCount())
	}

	// Duplicate registration.
	err = sched.RegisterMigration(m)
	if !errors.Is(err, ErrSchedVersionExists) {
		t.Fatalf("expected ErrSchedVersionExists, got %v", err)
	}
}

func TestSchedulerRegisterInvalidVersion(t *testing.T) {
	sched, _ := NewStateMigrationScheduler(DefaultSchedulerConfig())

	m := &VersionedMigration{FromVersion: 0, ToVersion: 1}
	err := sched.RegisterMigration(m)
	if !errors.Is(err, ErrSchedInvalidVersion) {
		t.Fatalf("expected ErrSchedInvalidVersion, got %v", err)
	}
}

func TestSchedulerPlanMigration(t *testing.T) {
	sched, db := setupSchedulerWithState(t, 20)

	estimate, err := sched.PlanMigration(3, db)
	if err != nil {
		t.Fatalf("PlanMigration: %v", err)
	}
	if estimate.FromVersion != 1 {
		t.Errorf("expected from=1, got %d", estimate.FromVersion)
	}
	if estimate.ToVersion != 3 {
		t.Errorf("expected to=3, got %d", estimate.ToVersion)
	}
	if estimate.Steps != 2 {
		t.Errorf("expected 2 steps, got %d", estimate.Steps)
	}
	if estimate.TotalAccounts != 20 {
		t.Errorf("expected 20 accounts, got %d", estimate.TotalAccounts)
	}
	if estimate.EstimatedBatches < 1 {
		t.Errorf("expected at least 1 batch, got %d", estimate.EstimatedBatches)
	}
	if !estimate.IsReady {
		t.Error("expected migration to be ready (fork epoch 0)")
	}
}

func TestSchedulerPlanMigrationNilState(t *testing.T) {
	sched, _ := NewStateMigrationScheduler(DefaultSchedulerConfig())
	sched.RegisterMigration(V1ToV2Migration())

	_, err := sched.PlanMigration(2, nil)
	if !errors.Is(err, ErrSchedNilState) {
		t.Fatalf("expected ErrSchedNilState, got %v", err)
	}
}

func TestSchedulerV1ToV2Migration(t *testing.T) {
	sched, db := setupSchedulerWithState(t, 5)

	err := sched.StartMigration(2, db)
	if err != nil {
		t.Fatalf("StartMigration: %v", err)
	}

	if !sched.IsRunning() {
		t.Fatal("expected migration to be running")
	}

	// Process batches until done.
	for sched.IsRunning() {
		progress, err := sched.ProcessNextBatch(db)
		if err != nil {
			t.Fatalf("ProcessNextBatch: %v", err)
		}
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
	}

	if sched.CurrentVersion() != 2 {
		t.Errorf("expected version 2, got %d", sched.CurrentVersion())
	}

	progress := sched.GetProgress()
	if progress == nil {
		t.Fatal("expected progress after completion")
	}
	if progress.PercentComplete != 100.0 {
		t.Errorf("expected 100%%, got %.1f%%", progress.PercentComplete)
	}
}

func TestSchedulerBatchProcessingLimits(t *testing.T) {
	config := DefaultSchedulerConfig()
	config.BatchSize = 3
	config.CurrentVersion = 1

	sched, err := NewStateMigrationScheduler(config)
	if err != nil {
		t.Fatal(err)
	}
	sched.RegisterMigration(V1ToV2Migration())

	db := populateTestState(10, 1)

	err = sched.StartMigration(2, db)
	if err != nil {
		t.Fatal(err)
	}

	batchCount := 0
	for sched.IsRunning() {
		_, err := sched.ProcessNextBatch(db)
		if err != nil {
			t.Fatal(err)
		}
		batchCount++
	}

	// 10 accounts / 3 per batch = 4 batches (ceil).
	if batchCount < 3 || batchCount > 5 {
		t.Errorf("expected 3-5 batches for 10 accounts, got %d", batchCount)
	}
}

func TestSchedulerProgressTracking(t *testing.T) {
	config := DefaultSchedulerConfig()
	config.BatchSize = 2
	config.CurrentVersion = 1

	sched, _ := NewStateMigrationScheduler(config)
	sched.RegisterMigration(V1ToV2Migration())

	db := populateTestState(6, 1)
	sched.StartMigration(2, db)

	progress, _ := sched.ProcessNextBatch(db)
	if progress.MigratedAccounts != 2 {
		t.Errorf("expected 2 migrated after first batch, got %d", progress.MigratedAccounts)
	}
	if progress.PercentComplete < 30.0 || progress.PercentComplete > 40.0 {
		t.Errorf("expected ~33%%, got %.1f%%", progress.PercentComplete)
	}
	if progress.BatchesRun != 1 {
		t.Errorf("expected 1 batch run, got %d", progress.BatchesRun)
	}
}

func TestSchedulerRollbackAfterFailure(t *testing.T) {
	config := DefaultSchedulerConfig()
	config.BatchSize = 100
	config.CurrentVersion = 1

	sched, _ := NewStateMigrationScheduler(config)

	// Register a migration that modifies storage.
	sched.RegisterMigration(&VersionedMigration{
		FromVersion: 1,
		ToVersion:   2,
		Fork:        ForkBoundary{Name: "Test", ActivateAt: 0},
		TransformAccount: func(addr types.Address, db StateDB) (bool, error) {
			// Write a new storage value.
			slot := types.BytesToHash([]byte{0xFF})
			db.SetState(addr, slot, types.BytesToHash([]byte{0x42}))
			return true, nil
		},
	})

	db := populateTestState(5, 2)

	// Capture pre-migration state.
	preSlot := types.BytesToHash([]byte{0xFF})
	var firstAddr types.Address
	for addr := range db.stateObjects {
		firstAddr = addr
		break
	}
	preMigValue := db.GetState(firstAddr, preSlot)

	// Run migration.
	sched.StartMigration(2, db)
	for sched.IsRunning() {
		sched.ProcessNextBatch(db)
	}

	// Verify migration happened.
	postMigValue := db.GetState(firstAddr, preSlot)
	if postMigValue == preMigValue {
		t.Error("expected storage change after migration")
	}

	// Rollback.
	err := sched.Rollback(db)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// Verify rollback restored the original value.
	afterRollback := db.GetState(firstAddr, preSlot)
	if afterRollback != preMigValue {
		t.Errorf("expected restored value %v, got %v", preMigValue, afterRollback)
	}
	if sched.CurrentVersion() != 1 {
		t.Errorf("expected version 1 after rollback, got %d", sched.CurrentVersion())
	}
}

func TestSchedulerForkBoundaryActivation(t *testing.T) {
	config := DefaultSchedulerConfig()
	config.CurrentVersion = 3
	config.CurrentForkEpoch = 50 // before I+ activation (epoch 100)

	sched, _ := NewStateMigrationScheduler(config)
	sched.RegisterMigration(V3ToV4Migration())

	db := populateTestState(5, 1)

	// Should fail: fork not active.
	err := sched.StartMigration(4, db)
	if !errors.Is(err, ErrSchedForkNotActive) {
		t.Fatalf("expected ErrSchedForkNotActive, got %v", err)
	}

	// Activate the fork.
	sched.SetForkEpoch(100)
	err = sched.StartMigration(4, db)
	if err != nil {
		t.Fatalf("should succeed after fork activation: %v", err)
	}
}

func TestSchedulerConcurrentSafety(t *testing.T) {
	config := DefaultSchedulerConfig()
	config.BatchSize = 2
	config.CurrentVersion = 1

	sched, _ := NewStateMigrationScheduler(config)
	sched.RegisterMigration(V1ToV2Migration())

	db := populateTestState(20, 1)
	sched.StartMigration(2, db)

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Concurrent batch processing.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sched.IsRunning() {
				_, err := sched.ProcessNextBatch(db)
				if err != nil && !errors.Is(err, ErrSchedNotStarted) {
					errCh <- err
				}
			}
		}()
	}

	// Concurrent progress reads.
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				sched.GetProgress()
				sched.IsRunning()
				sched.CurrentVersion()
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}
}

func TestSchedulerErrorRecovery(t *testing.T) {
	config := DefaultSchedulerConfig()
	config.BatchSize = 100
	config.CurrentVersion = 1

	sched, _ := NewStateMigrationScheduler(config)

	callCount := 0
	sched.RegisterMigration(&VersionedMigration{
		FromVersion: 1,
		ToVersion:   2,
		Fork:        ForkBoundary{Name: "Test", ActivateAt: 0},
		TransformAccount: func(addr types.Address, db StateDB) (bool, error) {
			callCount++
			if callCount == 3 {
				return false, fmt.Errorf("simulated error on account %d", callCount)
			}
			return true, nil
		},
	})

	db := populateTestState(5, 1)
	sched.StartMigration(2, db)

	progress, err := sched.ProcessNextBatch(db)
	if err != nil {
		t.Fatalf("ProcessNextBatch: %v", err)
	}

	if progress.ErrorCount != 1 {
		t.Errorf("expected 1 error, got %d", progress.ErrorCount)
	}
	if len(progress.Errors) != 1 {
		t.Errorf("expected 1 error detail, got %d", len(progress.Errors))
	}
}

func TestSchedulerMigrationChainV1ToV3(t *testing.T) {
	sched, db := setupSchedulerWithState(t, 8)

	err := sched.StartMigration(3, db)
	if err != nil {
		t.Fatalf("StartMigration v1->v3: %v", err)
	}

	for sched.IsRunning() {
		_, err := sched.ProcessNextBatch(db)
		if err != nil {
			t.Fatalf("ProcessNextBatch: %v", err)
		}
	}

	if sched.CurrentVersion() != 3 {
		t.Errorf("expected version 3, got %d", sched.CurrentVersion())
	}
}

func TestSchedulerEmptyStateMigration(t *testing.T) {
	config := DefaultSchedulerConfig()
	config.CurrentVersion = 1

	sched, _ := NewStateMigrationScheduler(config)
	sched.RegisterMigration(V1ToV2Migration())

	db := NewMemoryStateDB()
	err := sched.StartMigration(2, db)
	if err != nil {
		t.Fatalf("StartMigration on empty state: %v", err)
	}

	progress, err := sched.ProcessNextBatch(db)
	if err != nil {
		t.Fatalf("ProcessNextBatch: %v", err)
	}
	if progress.TotalAccounts != 0 {
		t.Errorf("expected 0 total accounts, got %d", progress.TotalAccounts)
	}
	if sched.CurrentVersion() != 2 {
		t.Errorf("expected version 2, got %d", sched.CurrentVersion())
	}
}

func TestSchedulerLargeBatchEstimation(t *testing.T) {
	config := DefaultSchedulerConfig()
	config.BatchSize = 50
	config.CurrentVersion = 1

	sched, _ := NewStateMigrationScheduler(config)
	sched.RegisterMigration(V1ToV2Migration())

	db := populateTestState(200, 2)

	estimate, err := sched.PlanMigration(2, db)
	if err != nil {
		t.Fatalf("PlanMigration: %v", err)
	}

	if estimate.TotalAccounts != 200 {
		t.Errorf("expected 200 accounts, got %d", estimate.TotalAccounts)
	}
	if estimate.EstimatedBatches != 4 { // 200/50
		t.Errorf("expected 4 batches, got %d", estimate.EstimatedBatches)
	}
	if estimate.Steps != 1 {
		t.Errorf("expected 1 step, got %d", estimate.Steps)
	}
}

func TestSchedulerDryRun(t *testing.T) {
	config := DefaultSchedulerConfig()
	config.BatchSize = 100
	config.CurrentVersion = 1
	config.DryRun = true

	sched, _ := NewStateMigrationScheduler(config)
	sched.RegisterMigration(&VersionedMigration{
		FromVersion: 1,
		ToVersion:   2,
		Fork:        ForkBoundary{Name: "Test", ActivateAt: 0},
		TransformAccount: func(addr types.Address, db StateDB) (bool, error) {
			// This would modify state, but DryRun prevents it.
			slot := types.BytesToHash([]byte{0xEE})
			db.SetState(addr, slot, types.BytesToHash([]byte{0x99}))
			return true, nil
		},
	})

	db := populateTestState(5, 1)

	// Capture pre-migration state.
	slot := types.BytesToHash([]byte{0xEE})
	var firstAddr types.Address
	for addr := range db.stateObjects {
		firstAddr = addr
		break
	}
	preval := db.GetState(firstAddr, slot)

	sched.StartMigration(2, db)
	for sched.IsRunning() {
		sched.ProcessNextBatch(db)
	}

	// In dry run, transform is NOT called, so state should be unchanged.
	postval := db.GetState(firstAddr, slot)
	if postval != preval {
		t.Error("dry run should not modify state")
	}
}

func TestSchedulerRollbackWithNoSnapshot(t *testing.T) {
	sched, _ := NewStateMigrationScheduler(DefaultSchedulerConfig())
	db := NewMemoryStateDB()

	err := sched.Rollback(db)
	if !errors.Is(err, ErrSchedRollbackFailed) {
		t.Fatalf("expected ErrSchedRollbackFailed, got %v", err)
	}
}

func TestSchedulerAlreadyRunning(t *testing.T) {
	sched, db := setupSchedulerWithState(t, 5)

	err := sched.StartMigration(2, db)
	if err != nil {
		t.Fatal(err)
	}

	err = sched.StartMigration(2, db)
	if !errors.Is(err, ErrSchedAlreadyRunning) {
		t.Fatalf("expected ErrSchedAlreadyRunning, got %v", err)
	}
}

func TestSchedulerProcessBatchNoMigration(t *testing.T) {
	sched, _ := NewStateMigrationScheduler(DefaultSchedulerConfig())
	db := NewMemoryStateDB()

	_, err := sched.ProcessNextBatch(db)
	if !errors.Is(err, ErrSchedNotStarted) {
		t.Fatalf("expected ErrSchedNotStarted, got %v", err)
	}
}

func TestSchedulerHasRollback(t *testing.T) {
	sched, db := setupSchedulerWithState(t, 3)

	if sched.HasRollback() {
		t.Error("should not have rollback before migration")
	}

	sched.StartMigration(2, db)
	if !sched.HasRollback() {
		t.Error("should have rollback after starting migration")
	}
}

func TestSchedulerGetMigration(t *testing.T) {
	sched, _ := NewStateMigrationScheduler(DefaultSchedulerConfig())
	sched.RegisterMigration(V1ToV2Migration())

	m := sched.GetMigration(1)
	if m == nil {
		t.Fatal("expected migration for version 1")
	}
	if m.FromVersion != 1 || m.ToVersion != 2 {
		t.Errorf("wrong migration: %d->%d", m.FromVersion, m.ToVersion)
	}

	m2 := sched.GetMigration(99)
	if m2 != nil {
		t.Error("should return nil for unregistered version")
	}
}

func TestSchedulerVerifyStep(t *testing.T) {
	config := DefaultSchedulerConfig()
	config.BatchSize = 100
	config.CurrentVersion = 1

	sched, _ := NewStateMigrationScheduler(config)
	sched.RegisterMigration(&VersionedMigration{
		FromVersion: 1,
		ToVersion:   2,
		Fork:        ForkBoundary{Name: "Test", ActivateAt: 0},
		TransformAccount: func(addr types.Address, db StateDB) (bool, error) {
			return true, nil
		},
		Verify: func(addr types.Address, db StateDB) error {
			// Simulate a verification failure for one specific address.
			if addr[19] == 1 {
				return fmt.Errorf("verification failed for first account")
			}
			return nil
		},
	})

	db := populateTestState(3, 1)
	sched.StartMigration(2, db)

	progress, _ := sched.ProcessNextBatch(db)
	if progress.ErrorCount == 0 {
		t.Error("expected at least one verification error")
	}
}

func TestSchedulerRollbackRestoresBalance(t *testing.T) {
	config := DefaultSchedulerConfig()
	config.BatchSize = 100
	config.CurrentVersion = 1

	sched, _ := NewStateMigrationScheduler(config)
	sched.RegisterMigration(&VersionedMigration{
		FromVersion: 1,
		ToVersion:   2,
		Fork:        ForkBoundary{Name: "Test", ActivateAt: 0},
		TransformAccount: func(addr types.Address, db StateDB) (bool, error) {
			db.AddBalance(addr, big.NewInt(999))
			return true, nil
		},
	})

	db := populateTestState(3, 0)
	var firstAddr types.Address
	for addr := range db.stateObjects {
		firstAddr = addr
		break
	}
	originalBalance := new(big.Int).Set(db.GetBalance(firstAddr))

	sched.StartMigration(2, db)
	for sched.IsRunning() {
		sched.ProcessNextBatch(db)
	}

	modifiedBalance := db.GetBalance(firstAddr)
	expectedModified := new(big.Int).Add(originalBalance, big.NewInt(999))
	if modifiedBalance.Cmp(expectedModified) != 0 {
		t.Errorf("expected modified balance %s, got %s", expectedModified, modifiedBalance)
	}

	sched.Rollback(db)
	restoredBalance := db.GetBalance(firstAddr)
	if restoredBalance.Cmp(originalBalance) != 0 {
		t.Errorf("expected restored balance %s, got %s", originalBalance, restoredBalance)
	}
}
