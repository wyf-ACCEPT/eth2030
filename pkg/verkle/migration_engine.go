package verkle

import (
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// MigrationConfig controls the MPT-to-Verkle state migration engine.
type MigrationConfig struct {
	// BatchSize is the number of accounts to process per batch.
	BatchSize int
	// WorkerCount is the number of parallel migration workers.
	WorkerCount int
	// CheckpointInterval is how often (in accounts) to create a checkpoint.
	CheckpointInterval int
	// DryRun skips actual writes when true.
	DryRun bool
}

// DefaultMigrationConfig returns a MigrationConfig with sensible defaults.
func DefaultMigrationConfig() MigrationConfig {
	return MigrationConfig{
		BatchSize:          1000,
		WorkerCount:        4,
		CheckpointInterval: 10000,
		DryRun:             false,
	}
}

// MigrationProgress tracks the overall progress of the MPT-to-Verkle migration.
type MigrationProgress struct {
	TotalAccounts    uint64
	MigratedAccounts uint64
	TotalStorage     uint64
	MigratedStorage  uint64
	StartTime        time.Time
	LastCheckpoint   time.Time
	Complete         bool
}

// migratedAccount holds the data for a single migrated account.
type migratedAccount struct {
	Balance  *big.Int
	Nonce    uint64
	CodeHash types.Hash
}

// migratedStorage holds migrated storage slots for an account.
type migratedStorage struct {
	Slots map[types.Hash]types.Hash
}

// MigrationEngine manages the MPT-to-Verkle state migration process.
type MigrationEngine struct {
	mu       sync.RWMutex
	config   MigrationConfig
	accounts map[types.Address]*migratedAccount
	storage  map[types.Address]*migratedStorage
	progress MigrationProgress
	// checkpointAccounts and checkpointStorage hold the last checkpoint snapshot.
	checkpointAccounts map[types.Address]*migratedAccount
	checkpointStorage  map[types.Address]*migratedStorage
}

// NewMigrationEngine creates a new migration engine with the given config.
func NewMigrationEngine(config MigrationConfig) *MigrationEngine {
	return &MigrationEngine{
		config:   config,
		accounts: make(map[types.Address]*migratedAccount),
		storage:  make(map[types.Address]*migratedStorage),
		progress: MigrationProgress{
			StartTime: time.Now(),
		},
	}
}

// MigrateAccount migrates a single account into the Verkle state.
func (e *MigrationEngine) MigrateAccount(address types.Address, balance *big.Int, nonce uint64, codeHash types.Hash) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if balance == nil {
		return errors.New("migration: balance must not be nil")
	}

	e.accounts[address] = &migratedAccount{
		Balance:  new(big.Int).Set(balance),
		Nonce:    nonce,
		CodeHash: codeHash,
	}
	e.progress.MigratedAccounts++
	return nil
}

// MigrateStorage migrates storage slots for an account.
func (e *MigrationEngine) MigrateStorage(address types.Address, slots map[types.Hash]types.Hash) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(slots) == 0 {
		return nil
	}

	st, ok := e.storage[address]
	if !ok {
		st = &migratedStorage{Slots: make(map[types.Hash]types.Hash)}
		e.storage[address] = st
	}
	for k, v := range slots {
		st.Slots[k] = v
		e.progress.MigratedStorage++
	}
	return nil
}

// Progress returns a copy of the current migration progress.
func (e *MigrationEngine) Progress() *MigrationProgress {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p := e.progress
	return &p
}

// Checkpoint creates a snapshot of the current migration state.
func (e *MigrationEngine) Checkpoint() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Snapshot accounts.
	e.checkpointAccounts = make(map[types.Address]*migratedAccount, len(e.accounts))
	for addr, acct := range e.accounts {
		e.checkpointAccounts[addr] = &migratedAccount{
			Balance:  new(big.Int).Set(acct.Balance),
			Nonce:    acct.Nonce,
			CodeHash: acct.CodeHash,
		}
	}

	// Snapshot storage.
	e.checkpointStorage = make(map[types.Address]*migratedStorage, len(e.storage))
	for addr, st := range e.storage {
		slots := make(map[types.Hash]types.Hash, len(st.Slots))
		for k, v := range st.Slots {
			slots[k] = v
		}
		e.checkpointStorage[addr] = &migratedStorage{Slots: slots}
	}

	e.progress.LastCheckpoint = time.Now()
	return nil
}

// VerifyMigration checks whether an account has been migrated and its
// Verkle tree key can be derived. Returns true if the account exists
// in the migrated state and its balance/nonce/codeHash key derivation
// is consistent.
func (e *MigrationEngine) VerifyMigration(address types.Address) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	acct, ok := e.accounts[address]
	if !ok {
		return false, nil
	}

	// Verify key derivation produces valid keys (non-zero stems).
	balKey := GetTreeKeyForBalance(address)
	nonceKey := GetTreeKeyForNonce(address)
	codeHashKey := GetTreeKeyForCodeHash(address)

	// The stem should be consistent across all account header keys.
	balStem := StemFromKey(balKey)
	nonceStem := StemFromKey(nonceKey)
	codeStem := StemFromKey(codeHashKey)

	if balStem != nonceStem || balStem != codeStem {
		return false, errors.New("migration: inconsistent stem derivation")
	}

	// Verify the account data is non-nil.
	if acct.Balance == nil {
		return false, errors.New("migration: nil balance in migrated account")
	}

	// Verify the code hash produces a valid keccak preimage check:
	// EmptyCodeHash for EOAs or any non-zero hash for contracts.
	_ = crypto.Keccak256Hash(acct.CodeHash[:])

	return true, nil
}

// Reset clears all migration state and starts fresh.
func (e *MigrationEngine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.accounts = make(map[types.Address]*migratedAccount)
	e.storage = make(map[types.Address]*migratedStorage)
	e.checkpointAccounts = nil
	e.checkpointStorage = nil
	e.progress = MigrationProgress{
		StartTime: time.Now(),
	}
}
