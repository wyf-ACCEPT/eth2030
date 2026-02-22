package state

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func testAddr(b byte) types.Address {
	var a types.Address
	a[19] = b
	return a
}

func testHash(b byte) types.Hash {
	var h types.Hash
	h[31] = b
	return h
}

// --- Balance tests ---

func TestMemoryStateDB_Balance(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(1)

	// Non-existent account returns zero.
	if bal := db.GetBalance(addr); bal.Sign() != 0 {
		t.Fatalf("expected zero balance for non-existent account, got %s", bal)
	}

	// Add balance creates account implicitly.
	db.AddBalance(addr, big.NewInt(100))
	if bal := db.GetBalance(addr); bal.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected balance 100, got %s", bal)
	}

	// Add more.
	db.AddBalance(addr, big.NewInt(50))
	if bal := db.GetBalance(addr); bal.Cmp(big.NewInt(150)) != 0 {
		t.Fatalf("expected balance 150, got %s", bal)
	}

	// Sub balance.
	db.SubBalance(addr, big.NewInt(30))
	if bal := db.GetBalance(addr); bal.Cmp(big.NewInt(120)) != 0 {
		t.Fatalf("expected balance 120, got %s", bal)
	}
}

func TestMemoryStateDB_BalanceReturnsCopy(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(1)
	db.AddBalance(addr, big.NewInt(100))

	// Modifying the returned value should not change the state.
	bal := db.GetBalance(addr)
	bal.SetInt64(999)
	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatal("GetBalance returned a reference instead of a copy")
	}
}

// --- Nonce tests ---

func TestMemoryStateDB_Nonce(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(2)

	if n := db.GetNonce(addr); n != 0 {
		t.Fatalf("expected nonce 0 for non-existent account, got %d", n)
	}

	db.SetNonce(addr, 5)
	if n := db.GetNonce(addr); n != 5 {
		t.Fatalf("expected nonce 5, got %d", n)
	}

	db.SetNonce(addr, 42)
	if n := db.GetNonce(addr); n != 42 {
		t.Fatalf("expected nonce 42, got %d", n)
	}
}

// --- Code tests ---

func TestMemoryStateDB_Code(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(3)

	if code := db.GetCode(addr); code != nil {
		t.Fatal("expected nil code for non-existent account")
	}
	if size := db.GetCodeSize(addr); size != 0 {
		t.Fatalf("expected code size 0, got %d", size)
	}

	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}
	db.SetCode(addr, code)

	got := db.GetCode(addr)
	if len(got) != len(code) {
		t.Fatalf("expected code length %d, got %d", len(code), len(got))
	}
	for i := range code {
		if got[i] != code[i] {
			t.Fatalf("code mismatch at byte %d", i)
		}
	}
	if db.GetCodeSize(addr) != len(code) {
		t.Fatalf("expected code size %d, got %d", len(code), db.GetCodeSize(addr))
	}

	// CodeHash should be non-zero after setting code.
	hash := db.GetCodeHash(addr)
	if hash == (types.Hash{}) {
		t.Fatal("expected non-zero code hash after setting code")
	}

	// CodeHash for non-existent account should be zero.
	if db.GetCodeHash(testAddr(99)) != (types.Hash{}) {
		t.Fatal("expected zero hash for non-existent account")
	}
}

// --- CreateAccount tests ---

func TestMemoryStateDB_CreateAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(4)

	// Set up some state.
	db.AddBalance(addr, big.NewInt(500))
	db.SetNonce(addr, 10)

	// CreateAccount should reset the account.
	db.CreateAccount(addr)

	if bal := db.GetBalance(addr); bal.Sign() != 0 {
		t.Fatalf("expected zero balance after CreateAccount, got %s", bal)
	}
	if n := db.GetNonce(addr); n != 0 {
		t.Fatalf("expected nonce 0 after CreateAccount, got %d", n)
	}
}

// --- Exist and Empty tests ---

func TestMemoryStateDB_ExistAndEmpty(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(5)

	if db.Exist(addr) {
		t.Fatal("account should not exist yet")
	}
	if !db.Empty(addr) {
		t.Fatal("non-existent account should be empty")
	}

	// Creating a fresh account: it exists but is empty.
	db.CreateAccount(addr)
	if !db.Exist(addr) {
		t.Fatal("account should exist after creation")
	}
	if !db.Empty(addr) {
		t.Fatal("fresh account should be empty")
	}

	// Adding balance makes it non-empty.
	db.AddBalance(addr, big.NewInt(1))
	if db.Empty(addr) {
		t.Fatal("account with balance should not be empty")
	}
}

func TestMemoryStateDB_EmptyWithNonce(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(6)

	db.SetNonce(addr, 1)
	if db.Empty(addr) {
		t.Fatal("account with nonce should not be empty")
	}
}

func TestMemoryStateDB_EmptyWithCode(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(7)

	db.SetCode(addr, []byte{0x60, 0x00})
	if db.Empty(addr) {
		t.Fatal("account with code should not be empty")
	}
}

// --- Storage tests ---

func TestMemoryStateDB_Storage(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(8)
	key := testHash(1)
	val := testHash(0xAB)

	// Non-existent returns zero.
	if db.GetState(addr, key) != (types.Hash{}) {
		t.Fatal("expected zero for non-existent storage")
	}
	if db.GetCommittedState(addr, key) != (types.Hash{}) {
		t.Fatal("expected zero committed state for non-existent account")
	}

	db.SetState(addr, key, val)
	if db.GetState(addr, key) != val {
		t.Fatalf("expected state %v, got %v", val, db.GetState(addr, key))
	}

	// Committed state should still be zero (dirty only).
	if db.GetCommittedState(addr, key) != (types.Hash{}) {
		t.Fatal("expected zero committed state before commit")
	}
}

func TestMemoryStateDB_StorageCommit(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(9)
	key := testHash(2)
	val := testHash(0xCD)

	db.SetState(addr, key, val)
	_, err := db.Commit()
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	// After commit, committed state should match.
	if db.GetCommittedState(addr, key) != val {
		t.Fatal("expected committed state to match after commit")
	}
}

func TestMemoryStateDB_StorageDeleteOnCommit(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(10)
	key := testHash(3)
	val := testHash(0xEF)

	db.SetState(addr, key, val)
	db.Commit()

	// Delete by setting to zero.
	db.SetState(addr, key, types.Hash{})
	db.Commit()

	if db.GetCommittedState(addr, key) != (types.Hash{}) {
		t.Fatal("expected committed state to be cleared after zero-set and commit")
	}
}

// --- SelfDestruct tests ---

func TestMemoryStateDB_SelfDestruct(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(11)

	db.AddBalance(addr, big.NewInt(1000))

	if db.HasSelfDestructed(addr) {
		t.Fatal("should not be self-destructed before calling SelfDestruct")
	}

	db.SelfDestruct(addr)

	if !db.HasSelfDestructed(addr) {
		t.Fatal("should be self-destructed after calling SelfDestruct")
	}
	if db.GetBalance(addr).Sign() != 0 {
		t.Fatal("balance should be zero after SelfDestruct")
	}
}

func TestMemoryStateDB_SelfDestructNonExistent(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(12)

	// Should be a no-op; must not panic.
	db.SelfDestruct(addr)
	if db.HasSelfDestructed(addr) {
		t.Fatal("non-existent account should not be self-destructed")
	}
}

// --- Snapshot and Revert tests ---

func TestMemoryStateDB_SnapshotRevertBalance(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(13)

	db.AddBalance(addr, big.NewInt(100))
	snap := db.Snapshot()
	db.AddBalance(addr, big.NewInt(200))

	if db.GetBalance(addr).Cmp(big.NewInt(300)) != 0 {
		t.Fatal("balance should be 300 before revert")
	}

	db.RevertToSnapshot(snap)

	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected balance 100 after revert, got %s", db.GetBalance(addr))
	}
}

func TestMemoryStateDB_SnapshotRevertNonce(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(14)

	db.SetNonce(addr, 5)
	snap := db.Snapshot()
	db.SetNonce(addr, 10)
	db.RevertToSnapshot(snap)

	if db.GetNonce(addr) != 5 {
		t.Fatalf("expected nonce 5 after revert, got %d", db.GetNonce(addr))
	}
}

func TestMemoryStateDB_SnapshotRevertCode(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(15)

	db.SetCode(addr, []byte{0x01})
	snap := db.Snapshot()
	db.SetCode(addr, []byte{0x02, 0x03})
	db.RevertToSnapshot(snap)

	code := db.GetCode(addr)
	if len(code) != 1 || code[0] != 0x01 {
		t.Fatalf("expected code [0x01] after revert, got %v", code)
	}
}

func TestMemoryStateDB_SnapshotRevertStorage(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(16)
	key := testHash(1)

	db.SetState(addr, key, testHash(0xAA))
	snap := db.Snapshot()
	db.SetState(addr, key, testHash(0xBB))
	db.RevertToSnapshot(snap)

	if db.GetState(addr, key) != testHash(0xAA) {
		t.Fatal("expected storage to revert to 0xAA")
	}
}

func TestMemoryStateDB_SnapshotRevertCreateAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(17)

	snap := db.Snapshot()
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(50))
	db.RevertToSnapshot(snap)

	if db.Exist(addr) {
		t.Fatal("account should not exist after reverting creation")
	}
}

func TestMemoryStateDB_SnapshotRevertSelfDestruct(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(18)
	db.AddBalance(addr, big.NewInt(500))

	snap := db.Snapshot()
	db.SelfDestruct(addr)
	db.RevertToSnapshot(snap)

	if db.HasSelfDestructed(addr) {
		t.Fatal("self-destruct should be reverted")
	}
	if db.GetBalance(addr).Cmp(big.NewInt(500)) != 0 {
		t.Fatal("balance should be restored after revert of self-destruct")
	}
}

func TestMemoryStateDB_NestedSnapshots(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(19)

	db.AddBalance(addr, big.NewInt(10))
	snap1 := db.Snapshot()

	db.AddBalance(addr, big.NewInt(20))
	snap2 := db.Snapshot()

	db.AddBalance(addr, big.NewInt(30))

	// Revert to snap2: should have 10+20=30.
	db.RevertToSnapshot(snap2)
	if db.GetBalance(addr).Cmp(big.NewInt(30)) != 0 {
		t.Fatalf("expected 30 after reverting to snap2, got %s", db.GetBalance(addr))
	}

	// Revert to snap1: should have 10.
	db.RevertToSnapshot(snap1)
	if db.GetBalance(addr).Cmp(big.NewInt(10)) != 0 {
		t.Fatalf("expected 10 after reverting to snap1, got %s", db.GetBalance(addr))
	}
}

// --- Refund tests ---

func TestMemoryStateDB_Refund(t *testing.T) {
	db := NewMemoryStateDB()

	if db.GetRefund() != 0 {
		t.Fatal("expected initial refund 0")
	}

	db.AddRefund(100)
	if db.GetRefund() != 100 {
		t.Fatalf("expected refund 100, got %d", db.GetRefund())
	}

	db.AddRefund(50)
	if db.GetRefund() != 150 {
		t.Fatalf("expected refund 150, got %d", db.GetRefund())
	}

	db.SubRefund(30)
	if db.GetRefund() != 120 {
		t.Fatalf("expected refund 120, got %d", db.GetRefund())
	}
}

func TestMemoryStateDB_RefundRevert(t *testing.T) {
	db := NewMemoryStateDB()

	db.AddRefund(100)
	snap := db.Snapshot()
	db.AddRefund(200)
	db.RevertToSnapshot(snap)

	if db.GetRefund() != 100 {
		t.Fatalf("expected refund 100 after revert, got %d", db.GetRefund())
	}
}

// --- Log tests ---

func TestMemoryStateDB_Logs(t *testing.T) {
	db := NewMemoryStateDB()
	txHash := testHash(0x01)
	db.SetTxContext(txHash, 0)

	log1 := &types.Log{Address: testAddr(1), Data: []byte{0x01}}
	log2 := &types.Log{Address: testAddr(2), Data: []byte{0x02}}
	db.AddLog(log1)
	db.AddLog(log2)

	logs := db.GetLogs(txHash)
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
	if logs[0].TxHash != txHash || logs[1].TxHash != txHash {
		t.Fatal("log TxHash not set correctly")
	}
	if logs[0].TxIndex != 0 || logs[1].TxIndex != 0 {
		t.Fatal("log TxIndex not set correctly")
	}
}

func TestMemoryStateDB_LogsRevert(t *testing.T) {
	db := NewMemoryStateDB()
	txHash := testHash(0x02)
	db.SetTxContext(txHash, 1)

	db.AddLog(&types.Log{Address: testAddr(1)})
	snap := db.Snapshot()
	db.AddLog(&types.Log{Address: testAddr(2)})
	db.AddLog(&types.Log{Address: testAddr(3)})
	db.RevertToSnapshot(snap)

	logs := db.GetLogs(txHash)
	if len(logs) != 1 {
		t.Fatalf("expected 1 log after revert, got %d", len(logs))
	}
}

func TestMemoryStateDB_LogsEmpty(t *testing.T) {
	db := NewMemoryStateDB()
	txHash := testHash(0x03)
	if logs := db.GetLogs(txHash); len(logs) != 0 {
		t.Fatalf("expected 0 logs for unknown tx, got %d", len(logs))
	}
}

func TestMemoryStateDB_LogsMultipleTx(t *testing.T) {
	db := NewMemoryStateDB()

	tx1 := testHash(0x10)
	tx2 := testHash(0x20)

	db.SetTxContext(tx1, 0)
	db.AddLog(&types.Log{Address: testAddr(1)})

	db.SetTxContext(tx2, 1)
	db.AddLog(&types.Log{Address: testAddr(2)})
	db.AddLog(&types.Log{Address: testAddr(3)})

	if len(db.GetLogs(tx1)) != 1 {
		t.Fatal("expected 1 log for tx1")
	}
	if len(db.GetLogs(tx2)) != 2 {
		t.Fatal("expected 2 logs for tx2")
	}
}

// --- Access list tests (via StateDB interface) ---

func TestMemoryStateDB_AccessList(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(20)
	slot := testHash(5)

	if db.AddressInAccessList(addr) {
		t.Fatal("address should not be in access list initially")
	}

	db.AddAddressToAccessList(addr)
	if !db.AddressInAccessList(addr) {
		t.Fatal("address should be in access list after adding")
	}

	addrOk, slotOk := db.SlotInAccessList(addr, slot)
	if !addrOk {
		t.Fatal("address should be present")
	}
	if slotOk {
		t.Fatal("slot should not be present yet")
	}

	db.AddSlotToAccessList(addr, slot)
	addrOk, slotOk = db.SlotInAccessList(addr, slot)
	if !addrOk || !slotOk {
		t.Fatal("both address and slot should be present")
	}
}

func TestMemoryStateDB_AccessListRevert(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(21)
	slot := testHash(6)

	snap := db.Snapshot()
	db.AddAddressToAccessList(addr)
	db.AddSlotToAccessList(addr, slot)
	db.RevertToSnapshot(snap)

	if db.AddressInAccessList(addr) {
		t.Fatal("address should not be in access list after revert")
	}
}

func TestMemoryStateDB_AddSlotAddsAddress(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(22)
	slot := testHash(7)

	// Adding a slot for a new address should also add the address.
	db.AddSlotToAccessList(addr, slot)
	if !db.AddressInAccessList(addr) {
		t.Fatal("adding a slot should also add the address")
	}
	addrOk, slotOk := db.SlotInAccessList(addr, slot)
	if !addrOk || !slotOk {
		t.Fatal("both address and slot should be present")
	}
}

// --- Transient storage tests ---

func TestMemoryStateDB_TransientStorage(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(23)
	key := testHash(10)
	val := testHash(0xFF)

	// Non-existent returns zero.
	if db.GetTransientState(addr, key) != (types.Hash{}) {
		t.Fatal("expected zero for non-existent transient storage")
	}

	db.SetTransientState(addr, key, val)
	if db.GetTransientState(addr, key) != val {
		t.Fatal("transient storage not set correctly")
	}
}

func TestMemoryStateDB_TransientStorageClear(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(24)
	key := testHash(11)

	db.SetTransientState(addr, key, testHash(0xAA))
	db.ClearTransientStorage()

	if db.GetTransientState(addr, key) != (types.Hash{}) {
		t.Fatal("transient storage should be empty after clear")
	}
}

func TestMemoryStateDB_TransientStorageRevert(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(25)
	key := testHash(12)

	snap := db.Snapshot()
	db.SetTransientState(addr, key, testHash(0xBB))
	db.RevertToSnapshot(snap)

	if db.GetTransientState(addr, key) != (types.Hash{}) {
		t.Fatal("transient storage should revert to zero")
	}
}

func TestMemoryStateDB_TransientStorageRevertToValue(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(26)
	key := testHash(13)

	db.SetTransientState(addr, key, testHash(0xAA))
	snap := db.Snapshot()
	db.SetTransientState(addr, key, testHash(0xBB))
	db.RevertToSnapshot(snap)

	if db.GetTransientState(addr, key) != testHash(0xAA) {
		t.Fatal("transient storage should revert to previous value")
	}
}

// --- Commit and root tests ---

func TestMemoryStateDB_CommitEmpty(t *testing.T) {
	db := NewMemoryStateDB()
	root, err := db.Commit()
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}
	if root != types.EmptyRootHash {
		t.Fatalf("expected empty root hash, got %v", root)
	}
}

func TestMemoryStateDB_CommitDeterministic(t *testing.T) {
	// Two identical state DBs should produce the same root.
	makeDB := func() *MemoryStateDB {
		db := NewMemoryStateDB()
		db.AddBalance(testAddr(1), big.NewInt(100))
		db.SetNonce(testAddr(1), 5)
		db.AddBalance(testAddr(2), big.NewInt(200))
		db.SetState(testAddr(2), testHash(1), testHash(0xAA))
		return db
	}

	root1, err := makeDB().Commit()
	if err != nil {
		t.Fatal(err)
	}
	root2, err := makeDB().Commit()
	if err != nil {
		t.Fatal(err)
	}
	if root1 != root2 {
		t.Fatalf("expected identical roots, got %v and %v", root1, root2)
	}
}

func TestMemoryStateDB_CommitSelfDestructExcluded(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(1)
	db.AddBalance(addr, big.NewInt(100))

	// Self-destructed accounts should not be included in root.
	db.SelfDestruct(addr)
	root, err := db.Commit()
	if err != nil {
		t.Fatal(err)
	}
	if root != types.EmptyRootHash {
		t.Fatal("self-destructed account should not contribute to root")
	}
}

func TestMemoryStateDB_GetRoot(t *testing.T) {
	db := NewMemoryStateDB()
	if db.GetRoot() != types.EmptyRootHash {
		t.Fatal("empty db should return empty root hash")
	}

	db.AddBalance(testAddr(1), big.NewInt(100))
	root := db.GetRoot()
	if root == types.EmptyRootHash {
		t.Fatal("non-empty state should not have empty root")
	}
}

func TestMemoryStateDB_StorageRoot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(30)

	// Non-existent account has empty storage root.
	if db.StorageRoot(addr) != types.EmptyRootHash {
		t.Fatal("expected empty root for non-existent account")
	}

	// Account with no storage has empty storage root.
	db.CreateAccount(addr)
	if db.StorageRoot(addr) != types.EmptyRootHash {
		t.Fatal("expected empty root for account with no storage")
	}

	// Account with storage has non-empty root.
	db.SetState(addr, testHash(1), testHash(0xAB))
	root := db.StorageRoot(addr)
	if root == types.EmptyRootHash {
		t.Fatal("expected non-empty root for account with storage")
	}
}

// --- Copy tests ---

func TestMemoryStateDB_Copy(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(31)

	db.AddBalance(addr, big.NewInt(100))
	db.SetNonce(addr, 5)
	db.SetCode(addr, []byte{0x60, 0x00})
	db.SetState(addr, testHash(1), testHash(0xAA))
	db.SetTransientState(addr, testHash(2), testHash(0xBB))
	db.AddAddressToAccessList(addr)

	cp := db.Copy()

	// Copy should have same state.
	if cp.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatal("copy balance mismatch")
	}
	if cp.GetNonce(addr) != 5 {
		t.Fatal("copy nonce mismatch")
	}
	if len(cp.GetCode(addr)) != 2 {
		t.Fatal("copy code mismatch")
	}
	if cp.GetState(addr, testHash(1)) != testHash(0xAA) {
		t.Fatal("copy storage mismatch")
	}
	if cp.GetTransientState(addr, testHash(2)) != testHash(0xBB) {
		t.Fatal("copy transient storage mismatch")
	}

	// Mutating original should not affect copy.
	db.AddBalance(addr, big.NewInt(999))
	if cp.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatal("mutation of original affected the copy")
	}

	// Mutating copy should not affect original.
	cp.SetNonce(addr, 99)
	if db.GetNonce(addr) != 5 {
		t.Fatal("mutation of copy affected the original")
	}
}

// --- Merge tests ---

func TestMemoryStateDB_Merge(t *testing.T) {
	dst := NewMemoryStateDB()
	src := NewMemoryStateDB()
	addr := testAddr(32)

	dst.AddBalance(addr, big.NewInt(100))
	src.AddBalance(addr, big.NewInt(999))
	src.SetNonce(addr, 7)

	dst.Merge(src)

	if dst.GetBalance(addr).Cmp(big.NewInt(999)) != 0 {
		t.Fatalf("expected merged balance 999, got %s", dst.GetBalance(addr))
	}
	if dst.GetNonce(addr) != 7 {
		t.Fatalf("expected merged nonce 7, got %d", dst.GetNonce(addr))
	}
}

// --- Prefetch tests ---

func TestMemoryStateDB_Prefetch(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(33)

	if db.Exist(addr) {
		t.Fatal("address should not exist before prefetch")
	}

	db.Prefetch([]types.Address{addr})

	if !db.Exist(addr) {
		t.Fatal("address should exist after prefetch")
	}
}

func TestMemoryStateDB_PrefetchStorage(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(34)

	db.PrefetchStorage(addr, []types.Hash{testHash(1)})

	if !db.Exist(addr) {
		t.Fatal("address should exist after PrefetchStorage")
	}
}

// --- BuildStateTrie ---

func TestMemoryStateDB_BuildStateTrie(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(40)
	db.AddBalance(addr, big.NewInt(100))

	tr := db.BuildStateTrie()
	if tr == nil {
		t.Fatal("expected non-nil trie")
	}

	// The root should match GetRoot.
	if tr.Hash() != db.GetRoot() {
		t.Fatal("trie root should match GetRoot")
	}
}

func TestMemoryStateDB_BuildStorageTrie(t *testing.T) {
	db := NewMemoryStateDB()
	addr := testAddr(41)

	// Non-existent account returns nil.
	if db.BuildStorageTrie(addr) != nil {
		t.Fatal("expected nil trie for non-existent account")
	}

	db.CreateAccount(addr)
	// Account with no storage returns nil.
	if db.BuildStorageTrie(addr) != nil {
		t.Fatal("expected nil trie for account with no storage")
	}

	db.SetState(addr, testHash(1), testHash(0xAA))
	tr := db.BuildStorageTrie(addr)
	if tr == nil {
		t.Fatal("expected non-nil trie for account with storage")
	}
}

// --- Interface compliance ---

func TestMemoryStateDB_InterfaceCompliance(t *testing.T) {
	var _ StateDB = (*MemoryStateDB)(nil)
}
