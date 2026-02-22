package state

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// createPurgeableState builds a MemoryStateDB with a mix of empty,
// self-destructed, and storage-holding accounts for purge testing.
func createPurgeableState(numEmpty, numSelfDestructed, numWithStorage int) *MemoryStateDB {
	db := NewMemoryStateDB()

	for i := 0; i < numEmpty; i++ {
		var addr types.Address
		addr[0] = 0x01
		addr[18] = byte(i >> 8)
		addr[19] = byte(i)
		db.CreateAccount(addr)
	}

	for i := 0; i < numSelfDestructed; i++ {
		var addr types.Address
		addr[0] = 0x02
		addr[18] = byte(i >> 8)
		addr[19] = byte(i)
		db.CreateAccount(addr)
		db.AddBalance(addr, big.NewInt(100))
		db.SelfDestruct(addr)
	}

	for i := 0; i < numWithStorage; i++ {
		var addr types.Address
		addr[0] = 0x03
		addr[18] = byte(i >> 8)
		addr[19] = byte(i)
		db.CreateAccount(addr)
		db.SetNonce(addr, 1)
		db.AddBalance(addr, big.NewInt(1000))
		var slot types.Hash
		slot[31] = byte(i)
		db.SetState(addr, slot, types.BytesToHash([]byte{0xFF}))
	}

	return db
}

func makeTestAddr(prefix byte, idx int) types.Address {
	var addr types.Address
	addr[0] = prefix
	addr[18] = byte(idx >> 8)
	addr[19] = byte(idx)
	return addr
}

func TestPurgeEmptyAccounts(t *testing.T) {
	db := createPurgeableState(5, 0, 3)

	config := DefaultPurgeConfig()
	purger := NewStatePurger(config)

	count, newRoot, err := purger.PurgeEmptyAccounts(db)
	if err != nil {
		t.Fatalf("purge error: %v", err)
	}

	if count != 5 {
		t.Fatalf("expected 5 empty accounts purged, got %d", count)
	}

	if newRoot.IsZero() {
		t.Fatal("new root should not be zero (storage accounts remain)")
	}

	// Verify empty accounts are gone.
	for i := 0; i < 5; i++ {
		addr := makeTestAddr(0x01, i)
		if db.Exist(addr) {
			t.Fatalf("empty account %d should have been purged", i)
		}
	}

	// Verify storage accounts still exist.
	for i := 0; i < 3; i++ {
		addr := makeTestAddr(0x03, i)
		if !db.Exist(addr) {
			t.Fatalf("storage account %d should still exist", i)
		}
	}
}

func TestPurgeSelfDestructed(t *testing.T) {
	db := createPurgeableState(0, 4, 2)

	config := DefaultPurgeConfig()
	purger := NewStatePurger(config)

	count, _, err := purger.PurgeSelfDestructed(db)
	if err != nil {
		t.Fatalf("purge error: %v", err)
	}

	if count != 4 {
		t.Fatalf("expected 4 self-destructed purged, got %d", count)
	}

	// Verify self-destructed accounts are gone.
	for i := 0; i < 4; i++ {
		addr := makeTestAddr(0x02, i)
		if db.Exist(addr) {
			t.Fatalf("self-destructed account %d should have been purged", i)
		}
	}
}

func TestPurgeExpiredStorage(t *testing.T) {
	db := NewMemoryStateDB()

	// Create account with nonce = 5 (below cutoff of 10) and storage.
	addr := makeTestAddr(0x10, 0)
	db.CreateAccount(addr)
	db.SetNonce(addr, 5)
	db.AddBalance(addr, big.NewInt(100))

	slot1 := types.BytesToHash([]byte{1})
	slot2 := types.BytesToHash([]byte{2})
	db.SetState(addr, slot1, types.BytesToHash([]byte{0xAA}))
	db.SetState(addr, slot2, types.BytesToHash([]byte{0xBB}))

	// Create account with nonce = 20 (above cutoff).
	addr2 := makeTestAddr(0x10, 1)
	db.CreateAccount(addr2)
	db.SetNonce(addr2, 20)
	db.AddBalance(addr2, big.NewInt(100))
	db.SetState(addr2, slot1, types.BytesToHash([]byte{0xCC}))

	config := DefaultPurgeConfig()
	purger := NewStatePurger(config)

	count, err := purger.PurgeExpiredStorage(db, 10)
	if err != nil {
		t.Fatalf("purge expired storage error: %v", err)
	}

	if count != 2 {
		t.Fatalf("expected 2 expired slots purged, got %d", count)
	}

	// Account with nonce < cutoff should have empty storage.
	if db.GetState(addr, slot1) != (types.Hash{}) {
		t.Fatal("expired slot1 should be cleared")
	}
	if db.GetState(addr, slot2) != (types.Hash{}) {
		t.Fatal("expired slot2 should be cleared")
	}

	// Account with nonce >= cutoff should be untouched.
	if db.GetState(addr2, slot1) == (types.Hash{}) {
		t.Fatal("non-expired slot should remain")
	}
}

func TestPurgeExpiredStorage_CutoffZero(t *testing.T) {
	db := NewMemoryStateDB()
	config := DefaultPurgeConfig()
	purger := NewStatePurger(config)

	_, err := purger.PurgeExpiredStorage(db, 0)
	if !errors.Is(err, ErrPurgeCutoffZero) {
		t.Fatalf("expected ErrPurgeCutoffZero, got: %v", err)
	}
}

func TestPurgeNilState(t *testing.T) {
	config := DefaultPurgeConfig()
	purger := NewStatePurger(config)

	_, _, err := purger.PurgeEmptyAccounts(nil)
	if !errors.Is(err, ErrPurgeNilState) {
		t.Fatalf("expected ErrPurgeNilState, got: %v", err)
	}

	_, _, err = purger.PurgeSelfDestructed(nil)
	if !errors.Is(err, ErrPurgeNilState) {
		t.Fatalf("expected ErrPurgeNilState, got: %v", err)
	}

	_, err = purger.PurgeExpiredStorage(nil, 10)
	if !errors.Is(err, ErrPurgeNilState) {
		t.Fatalf("expected ErrPurgeNilState, got: %v", err)
	}
}

func TestDryRunPurge(t *testing.T) {
	db := createPurgeableState(3, 2, 4)

	config := DefaultPurgeConfig()
	config.DryRun = true
	purger := NewStatePurger(config)

	stats, err := purger.DryRunPurge(db, 10)
	if err != nil {
		t.Fatalf("dry run error: %v", err)
	}

	if !stats.DryRun {
		t.Fatal("stats should indicate dry run")
	}

	if stats.EmptyAccountsPurged != 3 {
		t.Fatalf("expected 3 empty in dry run, got %d", stats.EmptyAccountsPurged)
	}
	if stats.SelfDestructedPurged != 2 {
		t.Fatalf("expected 2 self-destructed in dry run, got %d", stats.SelfDestructedPurged)
	}

	// State should be unmodified after dry run.
	for i := 0; i < 3; i++ {
		addr := makeTestAddr(0x01, i)
		if !db.Exist(addr) {
			t.Fatalf("dry run should not modify state: empty account %d missing", i)
		}
	}
	for i := 0; i < 2; i++ {
		addr := makeTestAddr(0x02, i)
		if !db.Exist(addr) {
			t.Fatalf("dry run should not modify state: self-destructed account %d missing", i)
		}
	}
}

func TestDryRunPurge_EmptyAccountsOnly(t *testing.T) {
	db := createPurgeableState(5, 3, 0)

	config := PurgeConfig{
		Targets:           PurgeTargetEmptyAccounts,
		DryRun:            true,
		PreserveAddresses: make(map[types.Address]bool),
	}
	purger := NewStatePurger(config)

	stats, err := purger.DryRunPurge(db, 0)
	if err != nil {
		t.Fatalf("dry run error: %v", err)
	}

	if stats.EmptyAccountsPurged != 5 {
		t.Fatalf("expected 5 empty, got %d", stats.EmptyAccountsPurged)
	}
	if stats.SelfDestructedPurged != 0 {
		t.Fatalf("expected 0 self-destructed (not targeted), got %d", stats.SelfDestructedPurged)
	}
}

func TestFullPurge(t *testing.T) {
	db := createPurgeableState(3, 2, 4)

	config := DefaultPurgeConfig()
	purger := NewStatePurger(config)

	stats, err := purger.FullPurge(db, 10)
	if err != nil {
		t.Fatalf("full purge error: %v", err)
	}

	if stats.EmptyAccountsPurged != 3 {
		t.Fatalf("expected 3 empty purged, got %d", stats.EmptyAccountsPurged)
	}
	if stats.SelfDestructedPurged != 2 {
		t.Fatalf("expected 2 self-destructed purged, got %d", stats.SelfDestructedPurged)
	}
	if stats.AccountsBefore != 9 {
		t.Fatalf("expected 9 accounts before, got %d", stats.AccountsBefore)
	}
	if stats.AccountsAfter != 4 {
		t.Fatalf("expected 4 accounts after, got %d", stats.AccountsAfter)
	}
	if stats.Duration <= 0 {
		t.Fatal("duration should be positive")
	}
}

func TestFullPurge_NoTargets(t *testing.T) {
	db := NewMemoryStateDB()
	config := PurgeConfig{Targets: 0}
	purger := NewStatePurger(config)

	_, err := purger.FullPurge(db, 0)
	if !errors.Is(err, ErrPurgeNoTargets) {
		t.Fatalf("expected ErrPurgeNoTargets, got: %v", err)
	}
}

func TestFullPurge_DryRunMode(t *testing.T) {
	db := createPurgeableState(2, 1, 0)

	config := DefaultPurgeConfig()
	config.DryRun = true
	purger := NewStatePurger(config)

	stats, err := purger.FullPurge(db, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !stats.DryRun {
		t.Fatal("should be dry run")
	}

	// State should be unmodified.
	if len(db.stateObjects) != 3 {
		t.Fatalf("expected 3 accounts (unmodified), got %d", len(db.stateObjects))
	}
}

func TestPreserveAddresses(t *testing.T) {
	db := createPurgeableState(3, 0, 0)

	// Preserve the first empty account.
	preserveAddr := makeTestAddr(0x01, 0)

	config := DefaultPurgeConfig()
	config.PreserveAddresses = map[types.Address]bool{
		preserveAddr: true,
	}
	purger := NewStatePurger(config)

	count, _, err := purger.PurgeEmptyAccounts(db)
	if err != nil {
		t.Fatalf("purge error: %v", err)
	}

	if count != 2 {
		t.Fatalf("expected 2 purged (1 preserved), got %d", count)
	}

	if !db.Exist(preserveAddr) {
		t.Fatal("preserved address should still exist")
	}
}

func TestPreserveSystemContracts(t *testing.T) {
	config := DefaultPurgeConfig()
	PreserveSystemContracts(&config)

	// Precompile addresses 1-9 should be preserved.
	for i := byte(1); i <= 9; i++ {
		var addr types.Address
		addr[19] = i
		if !config.PreserveAddresses[addr] {
			t.Fatalf("precompile %d should be preserved", i)
		}
	}

	// Beacon deposit contract should be preserved.
	beacon := types.HexToAddress("0x00000000219ab540356cBB839Cbe05303d7705Fa")
	if !config.PreserveAddresses[beacon] {
		t.Fatal("beacon deposit contract should be preserved")
	}
}

func TestPurgeStats_TotalPurged(t *testing.T) {
	stats := PurgeStats{
		EmptyAccountsPurged:  3,
		SelfDestructedPurged: 2,
		ExpiredSlotsPurged:   10,
	}
	if stats.TotalPurged() != 15 {
		t.Fatalf("expected 15 total purged, got %d", stats.TotalPurged())
	}
}

func TestPurgeStats_Summary(t *testing.T) {
	stats := PurgeStats{
		EmptyAccountsPurged:  3,
		SelfDestructedPurged: 2,
		DryRun:               true,
	}

	s := stats.Summary()
	if len(s) == 0 {
		t.Fatal("summary should not be empty")
	}
}

func TestEstimateGasSavings(t *testing.T) {
	stats := PurgeStats{
		EmptyAccountsPurged:  10,
		SelfDestructedPurged: 5,
		ExpiredSlotsPurged:   20,
	}

	savings := EstimateGasSavings(stats)
	expected := uint64(10+5)*2100 + uint64(20)*2100
	if savings != expected {
		t.Fatalf("expected savings %d, got %d", expected, savings)
	}
}

func TestPurgeConfig_HasTarget(t *testing.T) {
	config := PurgeConfig{Targets: PurgeTargetEmptyAccounts | PurgeTargetSelfDestructed}

	if !config.HasTarget(PurgeTargetEmptyAccounts) {
		t.Fatal("should have empty accounts target")
	}
	if !config.HasTarget(PurgeTargetSelfDestructed) {
		t.Fatal("should have self-destructed target")
	}
	if config.HasTarget(PurgeTargetExpiredStorage) {
		t.Fatal("should not have expired storage target")
	}
}

func TestPurgeConfig_All(t *testing.T) {
	config := PurgeConfig{Targets: PurgeTargetAll}

	if !config.HasTarget(PurgeTargetEmptyAccounts) {
		t.Fatal("all should include empty accounts")
	}
	if !config.HasTarget(PurgeTargetSelfDestructed) {
		t.Fatal("all should include self-destructed")
	}
	if !config.HasTarget(PurgeTargetExpiredStorage) {
		t.Fatal("all should include expired storage")
	}
}

func TestCreatePurgeableState(t *testing.T) {
	db := createPurgeableState(3, 2, 4)

	totalAccounts := 0
	emptyCount := 0
	selfDestructedCount := 0
	storageCount := 0

	for addr, obj := range db.stateObjects {
		totalAccounts++
		if isPurgeableEmpty(obj) {
			emptyCount++
		}
		if obj.selfDestructed {
			selfDestructedCount++
		}
		_ = addr
		if obj.account.Nonce > 0 && obj.account.Balance.Sign() > 0 {
			storageCount++
		}
	}

	if totalAccounts != 9 {
		t.Fatalf("expected 9 total accounts, got %d", totalAccounts)
	}
	if emptyCount != 3 {
		t.Fatalf("expected 3 empty, got %d", emptyCount)
	}
	if selfDestructedCount != 2 {
		t.Fatalf("expected 2 self-destructed, got %d", selfDestructedCount)
	}
	if storageCount != 4 {
		t.Fatalf("expected 4 with storage, got %d", storageCount)
	}
}

func TestVerifyPurgeEligibility_Empty(t *testing.T) {
	db := createPurgeableState(3, 0, 0)
	config := DefaultPurgeConfig()

	addr := makeTestAddr(0x01, 0) // empty account
	elig := VerifyPurgeEligibility(db, addr, config)

	if !elig.Eligible {
		t.Fatal("empty account should be eligible for purging")
	}
	if !elig.IsEmpty {
		t.Fatal("empty account should be flagged as empty")
	}
	if elig.HasBalance || elig.HasNonce || elig.HasCode {
		t.Fatal("empty account should have no balance, nonce, or code")
	}
}

func TestVerifyPurgeEligibility_NonEmpty(t *testing.T) {
	db := createPurgeableState(0, 0, 3)
	config := DefaultPurgeConfig()

	addr := makeTestAddr(0x03, 0) // account with storage, nonce, balance
	elig := VerifyPurgeEligibility(db, addr, config)

	if elig.Eligible {
		t.Fatal("non-empty account should not be eligible for purging")
	}
	if elig.IsEmpty {
		t.Fatal("non-empty account should not be flagged as empty")
	}
	if !elig.HasBalance {
		t.Fatal("account should have balance")
	}
	if !elig.HasNonce {
		t.Fatal("account should have nonce")
	}
}

func TestVerifyPurgeEligibility_SelfDestructed(t *testing.T) {
	db := createPurgeableState(0, 2, 0)
	config := DefaultPurgeConfig()

	addr := makeTestAddr(0x02, 0) // self-destructed account
	elig := VerifyPurgeEligibility(db, addr, config)

	if !elig.Eligible {
		t.Fatal("self-destructed account should be eligible for purging")
	}
	if !elig.IsSelfDestructed {
		t.Fatal("should be flagged as self-destructed")
	}
}

func TestVerifyPurgeEligibility_Preserved(t *testing.T) {
	db := createPurgeableState(3, 0, 0)
	addr := makeTestAddr(0x01, 0)

	config := DefaultPurgeConfig()
	config.PreserveAddresses = map[types.Address]bool{addr: true}

	elig := VerifyPurgeEligibility(db, addr, config)

	if elig.Eligible {
		t.Fatal("preserved address should not be eligible for purging")
	}
	if !elig.IsPreserved {
		t.Fatal("should be flagged as preserved")
	}
}

func TestVerifyPurgeEligibility_NonExistent(t *testing.T) {
	db := NewMemoryStateDB()
	config := DefaultPurgeConfig()

	addr := makeTestAddr(0xFF, 0) // does not exist
	elig := VerifyPurgeEligibility(db, addr, config)

	if elig.Eligible {
		t.Fatal("non-existent account should not be eligible")
	}
}

func TestValidatePreserveAddresses(t *testing.T) {
	db := createPurgeableState(3, 0, 0)

	addr1 := makeTestAddr(0x01, 0)  // exists
	addr2 := makeTestAddr(0xFF, 99) // does not exist

	config := DefaultPurgeConfig()
	config.PreserveAddresses = map[types.Address]bool{
		addr1: true,
		addr2: true,
	}

	missing := ValidatePreserveAddresses(db, config)

	if len(missing) != 1 {
		t.Fatalf("expected 1 missing preserved address, got %d", len(missing))
	}
	if missing[0] != addr2 {
		t.Fatalf("expected missing address %s, got %s", addr2.Hex(), missing[0].Hex())
	}
}

func TestValidatePreserveAddresses_AllExist(t *testing.T) {
	db := createPurgeableState(3, 0, 0)
	addr := makeTestAddr(0x01, 0)

	config := DefaultPurgeConfig()
	config.PreserveAddresses = map[types.Address]bool{addr: true}

	missing := ValidatePreserveAddresses(db, config)
	if len(missing) != 0 {
		t.Fatalf("expected no missing addresses, got %d", len(missing))
	}
}

func TestFullPurge_DetailedStats(t *testing.T) {
	db := createPurgeableState(3, 2, 4)

	config := DefaultPurgeConfig()
	purger := NewStatePurger(config)

	stats, err := purger.FullPurge(db, 10)
	if err != nil {
		t.Fatalf("full purge error: %v", err)
	}

	// Empty accounts have zero balance, zero nonce, and empty code hash.
	if stats.ZeroBalancePurged != 3 {
		t.Errorf("ZeroBalancePurged: got %d, want 3", stats.ZeroBalancePurged)
	}
	if stats.ZeroNoncePurged != 3 {
		t.Errorf("ZeroNoncePurged: got %d, want 3", stats.ZeroNoncePurged)
	}
	if stats.EmptyCodeHashPurged != 3 {
		t.Errorf("EmptyCodeHashPurged: got %d, want 3", stats.EmptyCodeHashPurged)
	}
}

func TestFullPurge_PreservedCount(t *testing.T) {
	db := createPurgeableState(3, 0, 2)
	preserveAddr := makeTestAddr(0x01, 0)

	config := DefaultPurgeConfig()
	config.PreserveAddresses = map[types.Address]bool{preserveAddr: true}
	purger := NewStatePurger(config)

	stats, err := purger.FullPurge(db, 10)
	if err != nil {
		t.Fatalf("full purge error: %v", err)
	}

	if stats.PreservedCount != 1 {
		t.Errorf("PreservedCount: got %d, want 1", stats.PreservedCount)
	}
	// 3 empty accounts minus 1 preserved = 2 purged.
	if stats.EmptyAccountsPurged != 2 {
		t.Errorf("EmptyAccountsPurged: got %d, want 2", stats.EmptyAccountsPurged)
	}
}

func TestStatePurger_SetConfig(t *testing.T) {
	config := DefaultPurgeConfig()
	purger := NewStatePurger(config)

	newConfig := PurgeConfig{
		Targets: PurgeTargetEmptyAccounts,
		DryRun:  true,
	}
	purger.SetConfig(newConfig)

	got := purger.Config()
	if got.Targets != PurgeTargetEmptyAccounts {
		t.Fatal("config update failed")
	}
	if !got.DryRun {
		t.Fatal("dry run should be true")
	}
}

func TestPurgeEmptyAccounts_DryRun(t *testing.T) {
	db := createPurgeableState(3, 0, 0)

	config := DefaultPurgeConfig()
	config.DryRun = true
	purger := NewStatePurger(config)

	count, _, err := purger.PurgeEmptyAccounts(db)
	if !errors.Is(err, ErrPurgeDryRun) {
		t.Fatalf("expected ErrPurgeDryRun, got: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 in dry run count, got %d", count)
	}

	// Verify state unchanged.
	if len(db.stateObjects) != 3 {
		t.Fatalf("state should be unchanged, got %d accounts", len(db.stateObjects))
	}
}

func TestPurgeSelfDestructed_DryRun(t *testing.T) {
	db := createPurgeableState(0, 4, 0)

	config := DefaultPurgeConfig()
	config.DryRun = true
	purger := NewStatePurger(config)

	count, _, err := purger.PurgeSelfDestructed(db)
	if !errors.Is(err, ErrPurgeDryRun) {
		t.Fatalf("expected ErrPurgeDryRun, got: %v", err)
	}
	if count != 4 {
		t.Fatalf("expected 4 in dry run count, got %d", count)
	}
}

func TestDryRunPurge_NilState(t *testing.T) {
	config := DefaultPurgeConfig()
	purger := NewStatePurger(config)

	_, err := purger.DryRunPurge(nil, 10)
	if !errors.Is(err, ErrPurgeNilState) {
		t.Fatalf("expected ErrPurgeNilState, got: %v", err)
	}
}

func TestFullPurge_NilState(t *testing.T) {
	config := DefaultPurgeConfig()
	purger := NewStatePurger(config)

	_, err := purger.FullPurge(nil, 10)
	if !errors.Is(err, ErrPurgeNilState) {
		t.Fatalf("expected ErrPurgeNilState, got: %v", err)
	}
}

func TestPurgeExpiredStorage_PreserveAddresses(t *testing.T) {
	db := NewMemoryStateDB()

	// Create two accounts with low nonce and storage.
	addr1 := makeTestAddr(0x20, 0)
	addr2 := makeTestAddr(0x20, 1)

	db.CreateAccount(addr1)
	db.SetNonce(addr1, 3)
	db.SetState(addr1, types.BytesToHash([]byte{1}), types.BytesToHash([]byte{0xFF}))

	db.CreateAccount(addr2)
	db.SetNonce(addr2, 3)
	db.SetState(addr2, types.BytesToHash([]byte{2}), types.BytesToHash([]byte{0xEE}))

	config := DefaultPurgeConfig()
	config.PreserveAddresses = map[types.Address]bool{addr1: true}
	purger := NewStatePurger(config)

	count, err := purger.PurgeExpiredStorage(db, 10)
	if err != nil {
		t.Fatalf("purge error: %v", err)
	}

	// Only addr2's slot should be purged (addr1 is preserved).
	if count != 1 {
		t.Fatalf("expected 1 slot purged (preserved addr1), got %d", count)
	}

	// addr1 storage should remain.
	if db.GetState(addr1, types.BytesToHash([]byte{1})) == (types.Hash{}) {
		t.Fatal("preserved addr1 storage should remain")
	}
}

func TestValidatePurgeConfig(t *testing.T) {
	// Nil config.
	if err := ValidatePurgeConfig(nil); err == nil {
		t.Fatal("expected error for nil config")
	}

	// No targets.
	cfg := &PurgeConfig{}
	if err := ValidatePurgeConfig(cfg); err == nil {
		t.Fatal("expected error for no targets")
	}

	// Valid config.
	dflt := DefaultPurgeConfig()
	if err := ValidatePurgeConfig(&dflt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Unknown bits.
	bad := &PurgeConfig{Targets: 0xFF}
	if err := ValidatePurgeConfig(bad); err == nil {
		t.Fatal("expected error for unknown target bits")
	}
}
