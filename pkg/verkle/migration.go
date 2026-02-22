package verkle

import (
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// StateMigrationSource provides read access to the MPT-based state for migration.
type StateMigrationSource interface {
	GetBalance(addr types.Address) *big.Int
	GetNonce(addr types.Address) uint64
	GetCodeHash(addr types.Address) types.Hash
	GetCode(addr types.Address) []byte
	Exist(addr types.Address) bool
}

// MigrationStats tracks the progress of a state migration.
type MigrationStats struct {
	AccountsMigrated     uint64
	StorageSlotsMigrated uint64
}

// MigrationState tracks which accounts have been migrated.
type MigrationState struct {
	mu       sync.Mutex
	migrated map[types.Address]bool
	stats    MigrationStats
	vdb      *VerkleStateDB
}

// NewMigrationState creates a new migration state tracker.
func NewMigrationState(vdb *VerkleStateDB) *MigrationState {
	return &MigrationState{
		migrated: make(map[types.Address]bool),
		vdb:      vdb,
	}
}

// MigrateAccounts migrates a list of accounts from the source state DB
// into the Verkle state DB. Already-migrated accounts are skipped.
func (ms *MigrationState) MigrateAccounts(source StateMigrationSource, accounts []types.Address) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	for _, addr := range accounts {
		if ms.migrated[addr] {
			continue
		}
		if !source.Exist(addr) {
			continue
		}

		acct := &AccountState{
			Nonce:   source.GetNonce(addr),
			Balance: new(big.Int).Set(source.GetBalance(addr)),
		}
		codeHash := source.GetCodeHash(addr)
		copy(acct.CodeHash[:], codeHash[:])

		ms.vdb.SetAccount(addr, acct)
		ms.migrated[addr] = true
		ms.stats.AccountsMigrated++
	}
	return nil
}

// MigrateOnAccess performs progressive migration: if an account has not
// yet been migrated, it is migrated from source on first access.
// Returns the account state from the Verkle DB.
func (ms *MigrationState) MigrateOnAccess(source StateMigrationSource, addr types.Address) *AccountState {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if !ms.migrated[addr] && source.Exist(addr) {
		acct := &AccountState{
			Nonce:   source.GetNonce(addr),
			Balance: new(big.Int).Set(source.GetBalance(addr)),
		}
		codeHash := source.GetCodeHash(addr)
		copy(acct.CodeHash[:], codeHash[:])

		ms.vdb.SetAccount(addr, acct)
		ms.migrated[addr] = true
		ms.stats.AccountsMigrated++
	}

	return ms.vdb.GetAccount(addr)
}

// IsMigrated reports whether an account has been migrated.
func (ms *MigrationState) IsMigrated(addr types.Address) bool {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.migrated[addr]
}

// Stats returns the current migration statistics.
func (ms *MigrationState) Stats() MigrationStats {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.stats
}

// MigrateAccountBatch converts a batch of MPT accounts to Verkle format with
// progress tracking. It returns the number of accounts successfully migrated
// and any non-fatal errors encountered. Unlike MigrateAccounts, this function
// reports per-account progress via the callback (if non-nil) and continues
// past individual account failures rather than aborting the entire batch.
func (ms *MigrationState) MigrateAccountBatch(
	source StateMigrationSource,
	accounts []types.Address,
	progressFn func(migrated, total int),
) (migrated int, errs []error) {
	total := len(accounts)
	for i, addr := range accounts {
		ms.mu.Lock()
		if ms.migrated[addr] {
			ms.mu.Unlock()
			if progressFn != nil {
				progressFn(i+1, total)
			}
			continue
		}
		ms.mu.Unlock()

		if !source.Exist(addr) {
			if progressFn != nil {
				progressFn(i+1, total)
			}
			continue
		}

		acct := &AccountState{
			Nonce:   source.GetNonce(addr),
			Balance: new(big.Int).Set(source.GetBalance(addr)),
		}
		codeHash := source.GetCodeHash(addr)
		copy(acct.CodeHash[:], codeHash[:])

		ms.mu.Lock()
		ms.vdb.SetAccount(addr, acct)
		ms.migrated[addr] = true
		ms.stats.AccountsMigrated++
		ms.mu.Unlock()

		migrated++
		if progressFn != nil {
			progressFn(i+1, total)
		}
	}
	return migrated, errs
}

// MigrateStorageSlot migrates a single storage slot for an already-migrated account.
func (ms *MigrationState) MigrateStorageSlot(addr types.Address, key, value types.Hash) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.vdb.SetStorage(addr, key, value)
	ms.stats.StorageSlotsMigrated++
}
