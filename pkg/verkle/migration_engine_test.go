package verkle

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestMigrationDefaultConfig(t *testing.T) {
	cfg := DefaultMigrationConfig()
	if cfg.BatchSize != 1000 {
		t.Errorf("BatchSize = %d, want 1000", cfg.BatchSize)
	}
	if cfg.WorkerCount != 4 {
		t.Errorf("WorkerCount = %d, want 4", cfg.WorkerCount)
	}
	if cfg.CheckpointInterval != 10000 {
		t.Errorf("CheckpointInterval = %d, want 10000", cfg.CheckpointInterval)
	}
	if cfg.DryRun {
		t.Error("DryRun should be false by default")
	}
}

func TestMigrateAccountEngine(t *testing.T) {
	engine := NewMigrationEngine(DefaultMigrationConfig())

	addr := types.BytesToAddress([]byte{0x01})
	balance := big.NewInt(1_000_000)
	nonce := uint64(42)
	codeHash := types.EmptyCodeHash

	err := engine.MigrateAccount(addr, balance, nonce, codeHash)
	if err != nil {
		t.Fatalf("MigrateAccount: %v", err)
	}

	p := engine.Progress()
	if p.MigratedAccounts != 1 {
		t.Errorf("MigratedAccounts = %d, want 1", p.MigratedAccounts)
	}
}

func TestMigrateStorageEngine(t *testing.T) {
	engine := NewMigrationEngine(DefaultMigrationConfig())

	addr := types.BytesToAddress([]byte{0x01})
	slots := map[types.Hash]types.Hash{
		types.HexToHash("0x01"): types.HexToHash("0xaa"),
		types.HexToHash("0x02"): types.HexToHash("0xbb"),
	}

	err := engine.MigrateStorage(addr, slots)
	if err != nil {
		t.Fatalf("MigrateStorage: %v", err)
	}

	p := engine.Progress()
	if p.MigratedStorage != 2 {
		t.Errorf("MigratedStorage = %d, want 2", p.MigratedStorage)
	}
}

func TestMigrationProgressEngine(t *testing.T) {
	engine := NewMigrationEngine(DefaultMigrationConfig())

	p := engine.Progress()
	if p.MigratedAccounts != 0 {
		t.Errorf("initial MigratedAccounts = %d, want 0", p.MigratedAccounts)
	}
	if p.MigratedStorage != 0 {
		t.Errorf("initial MigratedStorage = %d, want 0", p.MigratedStorage)
	}
	if p.Complete {
		t.Error("initial Complete should be false")
	}
	if p.StartTime.IsZero() {
		t.Error("StartTime should be set")
	}

	// Migrate an account and check progress updates.
	addr := types.BytesToAddress([]byte{0x01})
	engine.MigrateAccount(addr, big.NewInt(100), 1, types.EmptyCodeHash)

	p = engine.Progress()
	if p.MigratedAccounts != 1 {
		t.Errorf("MigratedAccounts = %d, want 1", p.MigratedAccounts)
	}
}

func TestCheckpointEngine(t *testing.T) {
	engine := NewMigrationEngine(DefaultMigrationConfig())

	addr := types.BytesToAddress([]byte{0x01})
	engine.MigrateAccount(addr, big.NewInt(500), 3, types.EmptyCodeHash)

	err := engine.Checkpoint()
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	p := engine.Progress()
	if p.LastCheckpoint.IsZero() {
		t.Error("LastCheckpoint should be set after Checkpoint()")
	}

	// Verify checkpoint captured the account.
	if engine.checkpointAccounts == nil {
		t.Fatal("checkpointAccounts should not be nil after Checkpoint")
	}
	if _, ok := engine.checkpointAccounts[addr]; !ok {
		t.Error("checkpointed account not found")
	}
}

func TestVerifyMigrationEngine(t *testing.T) {
	engine := NewMigrationEngine(DefaultMigrationConfig())

	addr := types.BytesToAddress([]byte{0x01})
	engine.MigrateAccount(addr, big.NewInt(1000), 5, types.EmptyCodeHash)

	ok, err := engine.VerifyMigration(addr)
	if err != nil {
		t.Fatalf("VerifyMigration: %v", err)
	}
	if !ok {
		t.Error("VerifyMigration should return true for migrated account")
	}
}

func TestVerifyMigrationMissing(t *testing.T) {
	engine := NewMigrationEngine(DefaultMigrationConfig())

	addr := types.BytesToAddress([]byte{0xff})
	ok, err := engine.VerifyMigration(addr)
	if err != nil {
		t.Fatalf("VerifyMigration: %v", err)
	}
	if ok {
		t.Error("VerifyMigration should return false for unmigrated account")
	}
}

func TestMigrateMultipleAccountsEngine(t *testing.T) {
	engine := NewMigrationEngine(DefaultMigrationConfig())

	addrs := make([]types.Address, 10)
	for i := range addrs {
		addrs[i] = types.BytesToAddress([]byte{byte(i + 1)})
		err := engine.MigrateAccount(addrs[i], big.NewInt(int64(i*100)), uint64(i), types.EmptyCodeHash)
		if err != nil {
			t.Fatalf("MigrateAccount[%d]: %v", i, err)
		}
	}

	p := engine.Progress()
	if p.MigratedAccounts != 10 {
		t.Errorf("MigratedAccounts = %d, want 10", p.MigratedAccounts)
	}

	// Verify each account.
	for _, addr := range addrs {
		ok, err := engine.VerifyMigration(addr)
		if err != nil {
			t.Fatalf("VerifyMigration(%x): %v", addr, err)
		}
		if !ok {
			t.Errorf("VerifyMigration(%x) = false, want true", addr)
		}
	}
}

func TestMigrationResetEngine(t *testing.T) {
	engine := NewMigrationEngine(DefaultMigrationConfig())

	addr := types.BytesToAddress([]byte{0x01})
	engine.MigrateAccount(addr, big.NewInt(1000), 5, types.EmptyCodeHash)
	engine.MigrateStorage(addr, map[types.Hash]types.Hash{
		types.HexToHash("0x01"): types.HexToHash("0xaa"),
	})
	engine.Checkpoint()

	engine.Reset()

	p := engine.Progress()
	if p.MigratedAccounts != 0 {
		t.Errorf("MigratedAccounts after reset = %d, want 0", p.MigratedAccounts)
	}
	if p.MigratedStorage != 0 {
		t.Errorf("MigratedStorage after reset = %d, want 0", p.MigratedStorage)
	}

	ok, err := engine.VerifyMigration(addr)
	if err != nil {
		t.Fatalf("VerifyMigration after reset: %v", err)
	}
	if ok {
		t.Error("VerifyMigration should return false after reset")
	}
}
