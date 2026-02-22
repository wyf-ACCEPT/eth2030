package verkle

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// mockStateSource implements StateMigrationSource for testing.
type mockStateSource struct {
	accounts map[types.Address]*mockAccount
}

type mockAccount struct {
	balance  *big.Int
	nonce    uint64
	codeHash types.Hash
	code     []byte
}

func newMockStateSource() *mockStateSource {
	return &mockStateSource{
		accounts: make(map[types.Address]*mockAccount),
	}
}

func (m *mockStateSource) GetBalance(addr types.Address) *big.Int {
	if a, ok := m.accounts[addr]; ok {
		return a.balance
	}
	return new(big.Int)
}

func (m *mockStateSource) GetNonce(addr types.Address) uint64 {
	if a, ok := m.accounts[addr]; ok {
		return a.nonce
	}
	return 0
}

func (m *mockStateSource) GetCodeHash(addr types.Address) types.Hash {
	if a, ok := m.accounts[addr]; ok {
		return a.codeHash
	}
	return types.EmptyCodeHash
}

func (m *mockStateSource) GetCode(addr types.Address) []byte {
	if a, ok := m.accounts[addr]; ok {
		return a.code
	}
	return nil
}

func (m *mockStateSource) Exist(addr types.Address) bool {
	_, ok := m.accounts[addr]
	return ok
}

func TestMigrateAccounts(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	vdb := NewVerkleStateDB(tree)
	ms := NewMigrationState(vdb)

	source := newMockStateSource()
	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})
	source.accounts[addr1] = &mockAccount{
		balance:  big.NewInt(1000),
		nonce:    5,
		codeHash: types.EmptyCodeHash,
	}
	source.accounts[addr2] = &mockAccount{
		balance:  big.NewInt(2000),
		nonce:    10,
		codeHash: types.EmptyCodeHash,
	}

	err := ms.MigrateAccounts(source, []types.Address{addr1, addr2})
	if err != nil {
		t.Fatalf("MigrateAccounts: %v", err)
	}

	stats := ms.Stats()
	if stats.AccountsMigrated != 2 {
		t.Errorf("AccountsMigrated = %d, want 2", stats.AccountsMigrated)
	}

	// Verify in Verkle state.
	a1 := vdb.GetAccount(addr1)
	if a1 == nil {
		t.Fatal("addr1 not found in Verkle state")
	}
	if a1.Balance.Int64() != 1000 {
		t.Errorf("addr1 balance = %d, want 1000", a1.Balance.Int64())
	}
	if a1.Nonce != 5 {
		t.Errorf("addr1 nonce = %d, want 5", a1.Nonce)
	}
}

func TestMigrateAccountsSkipsDuplicate(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	vdb := NewVerkleStateDB(tree)
	ms := NewMigrationState(vdb)

	source := newMockStateSource()
	addr := types.BytesToAddress([]byte{0x01})
	source.accounts[addr] = &mockAccount{
		balance: big.NewInt(1000),
		nonce:   5,
	}

	ms.MigrateAccounts(source, []types.Address{addr})
	ms.MigrateAccounts(source, []types.Address{addr}) // duplicate

	stats := ms.Stats()
	if stats.AccountsMigrated != 1 {
		t.Errorf("AccountsMigrated = %d, want 1 (should skip duplicate)", stats.AccountsMigrated)
	}
}

func TestMigrateAccountsSkipsNonExistent(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	vdb := NewVerkleStateDB(tree)
	ms := NewMigrationState(vdb)

	source := newMockStateSource()
	nonExistent := types.BytesToAddress([]byte{0xff})

	ms.MigrateAccounts(source, []types.Address{nonExistent})

	stats := ms.Stats()
	if stats.AccountsMigrated != 0 {
		t.Errorf("AccountsMigrated = %d, want 0", stats.AccountsMigrated)
	}
}

func TestMigrateOnAccess(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	vdb := NewVerkleStateDB(tree)
	ms := NewMigrationState(vdb)

	source := newMockStateSource()
	addr := types.BytesToAddress([]byte{0x01})
	source.accounts[addr] = &mockAccount{
		balance: big.NewInt(5000),
		nonce:   3,
	}

	// First access triggers migration.
	acct := ms.MigrateOnAccess(source, addr)
	if acct == nil {
		t.Fatal("MigrateOnAccess returned nil")
	}

	if !ms.IsMigrated(addr) {
		t.Error("account should be marked as migrated")
	}

	stats := ms.Stats()
	if stats.AccountsMigrated != 1 {
		t.Errorf("AccountsMigrated = %d, want 1", stats.AccountsMigrated)
	}

	// Second access should not re-migrate.
	acct2 := ms.MigrateOnAccess(source, addr)
	if acct2 == nil {
		t.Fatal("second MigrateOnAccess returned nil")
	}

	stats = ms.Stats()
	if stats.AccountsMigrated != 1 {
		t.Errorf("AccountsMigrated = %d, want 1 (no re-migration)", stats.AccountsMigrated)
	}
}

func TestMigrateOnAccessNonExistent(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	vdb := NewVerkleStateDB(tree)
	ms := NewMigrationState(vdb)

	source := newMockStateSource()
	addr := types.BytesToAddress([]byte{0xff})

	acct := ms.MigrateOnAccess(source, addr)
	// Non-existent in source, should return nil from Verkle DB too.
	if acct != nil {
		t.Error("expected nil for non-existent account")
	}
	if ms.IsMigrated(addr) {
		t.Error("non-existent account should not be marked migrated")
	}
}

func TestMigrateStorageSlot(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	vdb := NewVerkleStateDB(tree)
	ms := NewMigrationState(vdb)

	addr := types.BytesToAddress([]byte{0x01})
	key := types.HexToHash("0x01")
	val := types.HexToHash("0xaa")

	ms.MigrateStorageSlot(addr, key, val)

	stats := ms.Stats()
	if stats.StorageSlotsMigrated != 1 {
		t.Errorf("StorageSlotsMigrated = %d, want 1", stats.StorageSlotsMigrated)
	}

	// Verify the slot in Verkle state.
	got := vdb.GetStorage(addr, key)
	if got != val {
		t.Errorf("storage = %x, want %x", got, val)
	}
}

func TestMigrateAccountBatch(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	vdb := NewVerkleStateDB(tree)
	ms := NewMigrationState(vdb)

	source := newMockStateSource()
	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})
	addr3 := types.BytesToAddress([]byte{0x03})
	source.accounts[addr1] = &mockAccount{balance: big.NewInt(1000), nonce: 1}
	source.accounts[addr2] = &mockAccount{balance: big.NewInt(2000), nonce: 2}
	// addr3 does not exist in source

	t.Run("basic batch migration", func(t *testing.T) {
		var lastMigrated, lastTotal int
		migrated, errs := ms.MigrateAccountBatch(source, []types.Address{addr1, addr2, addr3}, func(m, total int) {
			lastMigrated = m
			lastTotal = total
		})
		if migrated != 2 {
			t.Errorf("migrated = %d, want 2", migrated)
		}
		if len(errs) != 0 {
			t.Errorf("errs = %v, want none", errs)
		}
		if lastMigrated != 3 || lastTotal != 3 {
			t.Errorf("progress callback: migrated=%d total=%d, want 3/3", lastMigrated, lastTotal)
		}
		if a := vdb.GetAccount(addr1); a == nil || a.Balance.Int64() != 1000 {
			t.Error("addr1 not correctly migrated")
		}
	})

	t.Run("skip already migrated", func(t *testing.T) {
		migrated, _ := ms.MigrateAccountBatch(source, []types.Address{addr1, addr2}, nil)
		if migrated != 0 {
			t.Errorf("migrated = %d, want 0 (already migrated)", migrated)
		}
	})

	t.Run("nil progress callback", func(t *testing.T) {
		newAddr := types.BytesToAddress([]byte{0x04})
		source.accounts[newAddr] = &mockAccount{balance: big.NewInt(500), nonce: 4}
		migrated, _ := ms.MigrateAccountBatch(source, []types.Address{newAddr}, nil)
		if migrated != 1 {
			t.Errorf("migrated = %d, want 1", migrated)
		}
	})
}

func TestIsMigrated(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	vdb := NewVerkleStateDB(tree)
	ms := NewMigrationState(vdb)

	addr := types.BytesToAddress([]byte{0x01})
	if ms.IsMigrated(addr) {
		t.Error("should not be migrated initially")
	}

	source := newMockStateSource()
	source.accounts[addr] = &mockAccount{balance: big.NewInt(100)}
	ms.MigrateAccounts(source, []types.Address{addr})

	if !ms.IsMigrated(addr) {
		t.Error("should be migrated after MigrateAccounts")
	}
}
