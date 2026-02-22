package state

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestDefaultTechDebtConfig(t *testing.T) {
	cfg := DefaultTechDebtConfig()
	if !cfg.RemoveEmptyAccounts {
		t.Error("RemoveEmptyAccounts should default to true")
	}
	if !cfg.RemoveZeroStorageSlots {
		t.Error("RemoveZeroStorageSlots should default to true")
	}
	if !cfg.RemoveSelfDestructed {
		t.Error("RemoveSelfDestructed should default to true")
	}
	if cfg.DryRun {
		t.Error("DryRun should default to false")
	}
}

func TestNewDryRunConfig(t *testing.T) {
	cfg := NewDryRunConfig()
	if !cfg.DryRun {
		t.Error("NewDryRunConfig should have DryRun=true")
	}
}

func TestResetLegacyStorageRemovesEmptyAccounts(t *testing.T) {
	state := NewMemoryStateDB()
	addr1 := types.HexToAddress("0x01")
	addr2 := types.HexToAddress("0x02")

	// addr1: empty account (zero balance, zero nonce, empty code).
	state.CreateAccount(addr1)

	// addr2: non-empty (has balance).
	state.CreateAccount(addr2)
	state.AddBalance(addr2, big.NewInt(1000))

	resetter := NewTechDebtResetter(nil)
	cleaned, _, err := resetter.ResetLegacyStorage(state)
	if err != nil {
		t.Fatalf("ResetLegacyStorage: %v", err)
	}
	if cleaned < 1 {
		t.Errorf("expected at least 1 cleaned entry, got %d", cleaned)
	}

	// addr1 should be removed.
	if state.Exist(addr1) {
		t.Error("empty account addr1 should be removed")
	}
	// addr2 should still exist.
	if !state.Exist(addr2) {
		t.Error("non-empty account addr2 should still exist")
	}
}

func TestResetLegacyStorageRemovesSelfDestructed(t *testing.T) {
	state := NewMemoryStateDB()
	addr := types.HexToAddress("0xdead")

	state.CreateAccount(addr)
	state.AddBalance(addr, big.NewInt(1000))
	state.SelfDestruct(addr)

	resetter := NewTechDebtResetter(nil)
	cleaned, _, err := resetter.ResetLegacyStorage(state)
	if err != nil {
		t.Fatalf("ResetLegacyStorage: %v", err)
	}
	if cleaned != 1 {
		t.Errorf("expected 1 cleaned, got %d", cleaned)
	}
	if state.Exist(addr) {
		t.Error("self-destructed account should be removed")
	}
}

func TestResetLegacyStorageRemovesZeroSlots(t *testing.T) {
	state := NewMemoryStateDB()
	addr := types.HexToAddress("0xaa")

	state.CreateAccount(addr)
	state.AddBalance(addr, big.NewInt(1))

	// Set a slot to a non-zero value, then zero it out.
	key := types.HexToHash("0x01")
	state.SetState(addr, key, types.HexToHash("0xff"))
	state.SetState(addr, key, types.Hash{}) // zero it

	resetter := NewTechDebtResetter(nil)
	cleaned, _, err := resetter.ResetLegacyStorage(state)
	if err != nil {
		t.Fatalf("ResetLegacyStorage: %v", err)
	}
	if cleaned < 1 {
		t.Errorf("expected at least 1 zero slot removed, got %d", cleaned)
	}

	stats := resetter.Stats()
	if stats.SlotsRemoved < 1 {
		t.Errorf("SlotsRemoved: got %d, want >= 1", stats.SlotsRemoved)
	}
}

func TestResetLegacyStorageNilState(t *testing.T) {
	resetter := NewTechDebtResetter(nil)
	_, _, err := resetter.ResetLegacyStorage(nil)
	if err != ErrTDRNilState {
		t.Errorf("expected ErrTDRNilState, got %v", err)
	}
}

func TestResetLegacyStorageDryRun(t *testing.T) {
	state := NewMemoryStateDB()
	addr := types.HexToAddress("0x01")
	state.CreateAccount(addr) // empty account

	cfg := DefaultTechDebtConfig()
	cfg.DryRun = true
	resetter := NewTechDebtResetter(cfg)

	cleaned, _, err := resetter.ResetLegacyStorage(state)
	if err != nil {
		t.Fatalf("DryRun reset: %v", err)
	}
	if cleaned < 1 {
		t.Errorf("dry run should still count entries, got %d", cleaned)
	}

	// In dry-run mode, the account should NOT be removed.
	if !state.Exist(addr) {
		t.Error("dry-run should not mutate state")
	}

	stats := resetter.Stats()
	if !stats.DryRun {
		t.Error("stats should reflect dry-run mode")
	}
}

func TestRemoveDeadCode(t *testing.T) {
	state := NewMemoryStateDB()
	dead := types.HexToAddress("0xdead")
	alive := types.HexToAddress("0xbeef")

	// Dead contract: no code, no balance, no nonce, no storage.
	state.CreateAccount(dead)

	// Alive contract: has code.
	state.CreateAccount(alive)
	state.SetCode(alive, []byte{0x60, 0x00, 0x60, 0x00, 0xfd})

	resetter := NewTechDebtResetter(nil)
	removed, err := resetter.RemoveDeadCode(state, []types.Address{dead, alive})
	if err != nil {
		t.Fatalf("RemoveDeadCode: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	if state.Exist(dead) {
		t.Error("dead contract should be removed")
	}
	if !state.Exist(alive) {
		t.Error("alive contract should still exist")
	}
}

func TestRemoveDeadCodeWithBalance(t *testing.T) {
	state := NewMemoryStateDB()
	addr := types.HexToAddress("0x01")

	state.CreateAccount(addr)
	state.AddBalance(addr, big.NewInt(1)) // has balance, not dead

	resetter := NewTechDebtResetter(nil)
	removed, _ := resetter.RemoveDeadCode(state, []types.Address{addr})
	if removed != 0 {
		t.Errorf("account with balance should not be removed, got %d", removed)
	}
}

func TestRemoveDeadCodeNilState(t *testing.T) {
	resetter := NewTechDebtResetter(nil)
	_, err := resetter.RemoveDeadCode(nil, []types.Address{{0x01}})
	if err != ErrTDRNilState {
		t.Errorf("expected ErrTDRNilState, got %v", err)
	}
}

func TestRemoveDeadCodeEmptyContracts(t *testing.T) {
	state := NewMemoryStateDB()
	resetter := NewTechDebtResetter(nil)
	_, err := resetter.RemoveDeadCode(state, nil)
	if err != ErrTDREmptyContracts {
		t.Errorf("expected ErrTDREmptyContracts, got %v", err)
	}
}

func TestMigrateStorageLayout(t *testing.T) {
	state := NewMemoryStateDB()
	contract := types.HexToAddress("0xcc")

	state.CreateAccount(contract)
	state.AddBalance(contract, big.NewInt(1))

	oldSlot := types.HexToHash("0x01")
	newSlot := types.HexToHash("0xaa")
	val := types.HexToHash("0xdeadbeef")

	state.SetState(contract, oldSlot, val)

	oldLayout := &StorageLayout{Slots: []types.Hash{oldSlot}}
	newLayout := &StorageLayout{Slots: []types.Hash{newSlot}}

	resetter := NewTechDebtResetter(nil)
	err := resetter.MigrateStorageLayout(state, contract, oldLayout, newLayout)
	if err != nil {
		t.Fatalf("MigrateStorageLayout: %v", err)
	}

	// New slot should have the value.
	got := state.GetState(contract, newSlot)
	if got != val {
		t.Errorf("new slot value: got %s, want %s", got.Hex(), val.Hex())
	}

	// Old slot should be zeroed.
	gotOld := state.GetState(contract, oldSlot)
	if gotOld != (types.Hash{}) {
		t.Errorf("old slot should be zeroed, got %s", gotOld.Hex())
	}

	stats := resetter.Stats()
	if stats.ContractsMigrated != 1 {
		t.Errorf("ContractsMigrated: got %d, want 1", stats.ContractsMigrated)
	}
}

func TestMigrateStorageLayoutSizeMismatch(t *testing.T) {
	state := NewMemoryStateDB()
	contract := types.HexToAddress("0xcc")
	state.CreateAccount(contract)

	old := &StorageLayout{Slots: []types.Hash{{0x01}, {0x02}}}
	new := &StorageLayout{Slots: []types.Hash{{0x0a}}}

	resetter := NewTechDebtResetter(nil)
	err := resetter.MigrateStorageLayout(state, contract, old, new)
	if err != ErrTDRLayoutMismatch {
		t.Errorf("expected ErrTDRLayoutMismatch, got %v", err)
	}
}

func TestMigrateStorageLayoutContractNotFound(t *testing.T) {
	state := NewMemoryStateDB()
	resetter := NewTechDebtResetter(nil)

	old := &StorageLayout{Slots: []types.Hash{{0x01}}}
	new := &StorageLayout{Slots: []types.Hash{{0x02}}}

	err := resetter.MigrateStorageLayout(state, types.HexToAddress("0xdead"), old, new)
	if err != ErrTDRContractNotFound {
		t.Errorf("expected ErrTDRContractNotFound, got %v", err)
	}
}

func TestMigrateStorageLayoutNilState(t *testing.T) {
	resetter := NewTechDebtResetter(nil)
	err := resetter.MigrateStorageLayout(nil, types.Address{}, nil, nil)
	if err != ErrTDRNilState {
		t.Errorf("expected ErrTDRNilState, got %v", err)
	}
}

func TestMigrateStorageLayoutNilLayouts(t *testing.T) {
	state := NewMemoryStateDB()
	resetter := NewTechDebtResetter(nil)
	err := resetter.MigrateStorageLayout(state, types.HexToAddress("0x01"), nil, nil)
	if err != ErrTDRNilConfig {
		t.Errorf("expected ErrTDRNilConfig, got %v", err)
	}
}

func TestCompactState(t *testing.T) {
	state := populateTestState(5, 3)

	resetter := NewTechDebtResetter(nil)
	before, after, err := resetter.CompactState(state)
	if err != nil {
		t.Fatalf("CompactState: %v", err)
	}
	if before == 0 {
		t.Error("before size should be non-zero")
	}
	// After should be <= before (in practice equal here since there
	// are no empty accounts or zero slots).
	if after > before {
		t.Errorf("after (%d) should be <= before (%d)", after, before)
	}
}

func TestCompactStateWithEmptyAccounts(t *testing.T) {
	state := NewMemoryStateDB()

	// Create some non-empty accounts.
	addr1 := types.HexToAddress("0x01")
	state.CreateAccount(addr1)
	state.AddBalance(addr1, big.NewInt(1000))

	// Create an empty account.
	addr2 := types.HexToAddress("0x02")
	state.CreateAccount(addr2)

	resetter := NewTechDebtResetter(nil)
	before, after, err := resetter.CompactState(state)
	if err != nil {
		t.Fatalf("CompactState: %v", err)
	}
	if after >= before {
		t.Errorf("after compaction (%d) should be < before (%d) with empty accounts", after, before)
	}
}

func TestCompactStateNilState(t *testing.T) {
	resetter := NewTechDebtResetter(nil)
	_, _, err := resetter.CompactState(nil)
	if err != ErrTDRNilState {
		t.Errorf("expected ErrTDRNilState, got %v", err)
	}
}

func TestResetAndCommit(t *testing.T) {
	state := NewMemoryStateDB()

	// Create an empty account to be cleaned.
	addr := types.HexToAddress("0x01")
	state.CreateAccount(addr)

	// Create a non-empty account.
	addr2 := types.HexToAddress("0x02")
	state.CreateAccount(addr2)
	state.AddBalance(addr2, big.NewInt(999))

	resetter := NewTechDebtResetter(nil)
	stats, root, err := resetter.ResetAndCommit(state)
	if err != nil {
		t.Fatalf("ResetAndCommit: %v", err)
	}
	if root.IsZero() {
		t.Error("root should not be zero after commit")
	}
	if stats.AccountsRemoved < 1 {
		t.Errorf("expected at least 1 account removed, got %d", stats.AccountsRemoved)
	}
}

func TestResetAndCommitDryRun(t *testing.T) {
	state := NewMemoryStateDB()
	addr := types.HexToAddress("0x01")
	state.CreateAccount(addr)

	cfg := DefaultTechDebtConfig()
	cfg.DryRun = true
	resetter := NewTechDebtResetter(cfg)

	stats, _, err := resetter.ResetAndCommit(state)
	if err != nil {
		t.Fatalf("ResetAndCommit DryRun: %v", err)
	}
	if !stats.DryRun {
		t.Error("stats should reflect dry-run mode")
	}
	// State should not be mutated.
	if !state.Exist(addr) {
		t.Error("dry-run should not remove accounts")
	}
}

func TestStateSize(t *testing.T) {
	state := populateTestState(3, 2)
	size := StateSize(state)
	if size == 0 {
		t.Error("StateSize should be non-zero for populated state")
	}
}

func TestStateSizeNil(t *testing.T) {
	if StateSize(nil) != 0 {
		t.Error("StateSize(nil) should return 0")
	}
}

func TestDirtyAccountCount(t *testing.T) {
	state := NewMemoryStateDB()
	addr := types.HexToAddress("0x01")
	state.CreateAccount(addr)
	state.SetState(addr, types.Hash{0x01}, types.Hash{0xff})

	count := DirtyAccountCount(state)
	if count != 1 {
		t.Errorf("DirtyAccountCount: got %d, want 1", count)
	}
}

func TestDirtyAccountCountNil(t *testing.T) {
	if DirtyAccountCount(nil) != 0 {
		t.Error("DirtyAccountCount(nil) should return 0")
	}
}

func TestZeroBalanceCount(t *testing.T) {
	state := NewMemoryStateDB()
	addr1 := types.HexToAddress("0x01")
	addr2 := types.HexToAddress("0x02")

	state.CreateAccount(addr1) // zero balance
	state.CreateAccount(addr2)
	state.AddBalance(addr2, big.NewInt(1))

	count := ZeroBalanceCount(state)
	if count != 1 {
		t.Errorf("ZeroBalanceCount: got %d, want 1", count)
	}
}

func TestZeroBalanceCountNil(t *testing.T) {
	if ZeroBalanceCount(nil) != 0 {
		t.Error("ZeroBalanceCount(nil) should return 0")
	}
}

func TestPopulateTestState(t *testing.T) {
	state := populateTestState(5, 3)

	count := 0
	for range state.stateObjects {
		count++
	}
	if count != 5 {
		t.Errorf("expected 5 accounts, got %d", count)
	}
}

func TestMigrateStorageLayoutDryRun(t *testing.T) {
	state := NewMemoryStateDB()
	contract := types.HexToAddress("0xcc")
	state.CreateAccount(contract)
	state.AddBalance(contract, big.NewInt(1))

	oldSlot := types.HexToHash("0x01")
	newSlot := types.HexToHash("0xaa")
	val := types.HexToHash("0xbeef")
	state.SetState(contract, oldSlot, val)

	cfg := DefaultTechDebtConfig()
	cfg.DryRun = true
	resetter := NewTechDebtResetter(cfg)

	old := &StorageLayout{Slots: []types.Hash{oldSlot}}
	new := &StorageLayout{Slots: []types.Hash{newSlot}}

	err := resetter.MigrateStorageLayout(state, contract, old, new)
	if err != nil {
		t.Fatalf("DryRun migrate: %v", err)
	}

	// Old slot should still have value (dry run).
	if state.GetState(contract, oldSlot) != val {
		t.Error("dry-run should not move storage")
	}
	// New slot should be empty.
	if state.GetState(contract, newSlot) != (types.Hash{}) {
		t.Error("dry-run should not write new slot")
	}

	stats := resetter.Stats()
	if stats.ContractsMigrated != 1 {
		t.Errorf("ContractsMigrated in dry-run: got %d, want 1", stats.ContractsMigrated)
	}
}
