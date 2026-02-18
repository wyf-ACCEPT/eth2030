package state

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// =============================================================================
// 1. State journaling: nested snapshots with reverts at different levels
// =============================================================================

// TestNestedSnapshotRevertOutOfOrder reverts to snap3, then directly to snap1,
// skipping snap2 entirely. This verifies that the journal correctly unwinds
// all entries back to the specified snapshot level.
func TestNestedSnapshotRevertOutOfOrder(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xaa01")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))

	snap1 := db.Snapshot()
	db.AddBalance(addr, big.NewInt(10)) // 110

	_ = db.Snapshot() // snap2, taken but not reverted directly
	db.AddBalance(addr, big.NewInt(20)) // 130

	snap3 := db.Snapshot()
	db.AddBalance(addr, big.NewInt(40)) // 170

	if db.GetBalance(addr).Cmp(big.NewInt(170)) != 0 {
		t.Fatalf("expected 170, got %s", db.GetBalance(addr))
	}

	// Revert to snap3 first (innermost).
	db.RevertToSnapshot(snap3)
	if db.GetBalance(addr).Cmp(big.NewInt(130)) != 0 {
		t.Fatalf("expected 130 after snap3 revert, got %s", db.GetBalance(addr))
	}

	// Now skip snap2 and revert directly to snap1.
	db.RevertToSnapshot(snap1)
	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected 100 after snap1 revert, got %s", db.GetBalance(addr))
	}
}

// TestNestedSnapshotRevertMiddleOnly takes three snapshots, reverts only the
// middle one, then makes more changes. Verifies the outer snapshot still works.
func TestNestedSnapshotRevertMiddleOnly(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xaa02")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))

	snapOuter := db.Snapshot()
	db.AddBalance(addr, big.NewInt(10)) // 110

	snapMiddle := db.Snapshot()
	db.AddBalance(addr, big.NewInt(20)) // 130
	db.SetNonce(addr, 5)

	_ = db.Snapshot() // snapInner
	db.AddBalance(addr, big.NewInt(30)) // 160
	db.SetNonce(addr, 10)

	// Revert to middle, unwinding inner and middle changes after middle.
	db.RevertToSnapshot(snapMiddle)
	if db.GetBalance(addr).Cmp(big.NewInt(110)) != 0 {
		t.Fatalf("expected 110, got %s", db.GetBalance(addr))
	}
	if db.GetNonce(addr) != 0 {
		t.Fatalf("expected nonce 0, got %d", db.GetNonce(addr))
	}

	// Add new changes after middle revert.
	db.AddBalance(addr, big.NewInt(5)) // 115
	db.SetNonce(addr, 2)

	// Revert to outer, which should undo everything since outer snapshot.
	db.RevertToSnapshot(snapOuter)
	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected 100 after outer revert, got %s", db.GetBalance(addr))
	}
	if db.GetNonce(addr) != 0 {
		t.Fatalf("expected nonce 0 after outer revert, got %d", db.GetNonce(addr))
	}
}

// TestSnapshotRevertWithCommittedStorage tests the interaction between
// committed storage and snapshot reverts at multiple levels.
func TestSnapshotRevertWithCommittedStorage(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xaa03")
	key := types.HexToHash("0x01")
	committedVal := types.HexToHash("0xcc")

	db.CreateAccount(addr)
	db.SetState(addr, key, committedVal)
	db.Commit() // Move to committed storage

	// Verify committed state.
	if db.GetCommittedState(addr, key) != committedVal {
		t.Fatal("committed state not set")
	}

	snap1 := db.Snapshot()
	dirtyVal1 := types.HexToHash("0xd1")
	db.SetState(addr, key, dirtyVal1)

	snap2 := db.Snapshot()
	dirtyVal2 := types.HexToHash("0xd2")
	db.SetState(addr, key, dirtyVal2)

	// Verify current state is dirtyVal2.
	if db.GetState(addr, key) != dirtyVal2 {
		t.Fatalf("expected %s, got %s", dirtyVal2, db.GetState(addr, key))
	}

	// Revert to snap2: should restore dirtyVal1.
	db.RevertToSnapshot(snap2)
	if db.GetState(addr, key) != dirtyVal1 {
		t.Fatalf("expected %s after snap2 revert, got %s", dirtyVal1, db.GetState(addr, key))
	}
	// Committed state should remain unchanged.
	if db.GetCommittedState(addr, key) != committedVal {
		t.Fatal("committed state should not change on revert")
	}

	// Revert to snap1: should fall back to committed state.
	db.RevertToSnapshot(snap1)
	if db.GetState(addr, key) != committedVal {
		t.Fatalf("expected committed val %s after snap1 revert, got %s",
			committedVal, db.GetState(addr, key))
	}
}

// TestSnapshotRevertMultipleAccountsMultipleLevels verifies that reverting
// correctly handles changes across multiple accounts at multiple snapshot
// levels.
func TestSnapshotRevertMultipleAccountsMultipleLevels(t *testing.T) {
	db := NewMemoryStateDB()
	a1 := types.HexToAddress("0xaa04")
	a2 := types.HexToAddress("0xaa05")

	db.CreateAccount(a1)
	db.CreateAccount(a2)
	db.AddBalance(a1, big.NewInt(100))
	db.AddBalance(a2, big.NewInt(200))

	snap1 := db.Snapshot()
	db.AddBalance(a1, big.NewInt(10)) // a1: 110
	db.SubBalance(a2, big.NewInt(50)) // a2: 150

	snap2 := db.Snapshot()
	db.SubBalance(a1, big.NewInt(60)) // a1: 50
	db.AddBalance(a2, big.NewInt(300)) // a2: 450

	// Revert snap2: undo inner changes.
	db.RevertToSnapshot(snap2)
	if db.GetBalance(a1).Cmp(big.NewInt(110)) != 0 {
		t.Fatalf("a1: expected 110, got %s", db.GetBalance(a1))
	}
	if db.GetBalance(a2).Cmp(big.NewInt(150)) != 0 {
		t.Fatalf("a2: expected 150, got %s", db.GetBalance(a2))
	}

	// Revert snap1: undo outer changes.
	db.RevertToSnapshot(snap1)
	if db.GetBalance(a1).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("a1: expected 100, got %s", db.GetBalance(a1))
	}
	if db.GetBalance(a2).Cmp(big.NewInt(200)) != 0 {
		t.Fatalf("a2: expected 200, got %s", db.GetBalance(a2))
	}
}

// =============================================================================
// 2. Account lifecycle: create, balance, code, code hash, self-destruct, empty
// =============================================================================

// TestAccountLifecycleComplete tests the full lifecycle of an account:
// creation, setting balance/nonce/code, verifying code hash, self-destruct.
func TestAccountLifecycleComplete(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xbb01")

	// Create account and set properties.
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(1_000_000))
	db.SetNonce(addr, 1)
	code := []byte{0x60, 0x0a, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}
	db.SetCode(addr, code)

	// Verify code hash is computed correctly.
	expectedHash := crypto.Keccak256Hash(code)
	gotHash := db.GetCodeHash(addr)
	if gotHash != expectedHash {
		t.Fatalf("code hash mismatch: expected %s, got %s", expectedHash, gotHash)
	}

	// Verify code size.
	if db.GetCodeSize(addr) != len(code) {
		t.Fatalf("code size: expected %d, got %d", len(code), db.GetCodeSize(addr))
	}

	// Verify account is not empty (has balance, nonce, code).
	if db.Empty(addr) {
		t.Fatal("account with balance/nonce/code should not be empty")
	}

	// Self-destruct the account.
	db.SelfDestruct(addr)

	// Balance should be zeroed.
	if db.GetBalance(addr).Sign() != 0 {
		t.Fatalf("self-destructed balance should be 0, got %s", db.GetBalance(addr))
	}
	// Self-destruct flag should be set.
	if !db.HasSelfDestructed(addr) {
		t.Fatal("account should be marked as self-destructed")
	}
	// Code and nonce should still be accessible (self-destruct doesn't
	// immediately remove them, they're removed at end of transaction).
	if db.GetCodeSize(addr) != len(code) {
		t.Fatal("code should still be accessible after self-destruct")
	}
}

// TestSelfDestructNonExistentAccount verifies self-destruct is a no-op for
// accounts that don't exist.
func TestSelfDestructNonExistentAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xbb02")

	db.SelfDestruct(addr) // Should not panic.
	if db.HasSelfDestructed(addr) {
		t.Fatal("non-existent account should not be self-destructed")
	}
}

// TestEmptyAccountDetectionEIP161 tests empty account detection per EIP-161.
// An account is empty if it has zero nonce, zero balance, and empty code hash.
func TestEmptyAccountDetectionEIP161(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xbb03")

	// Non-existent account is empty.
	if !db.Empty(addr) {
		t.Fatal("non-existent account should be empty")
	}

	// Freshly created account is empty.
	db.CreateAccount(addr)
	if !db.Empty(addr) {
		t.Fatal("freshly created account should be empty")
	}

	// With balance only -> not empty.
	db.AddBalance(addr, big.NewInt(1))
	if db.Empty(addr) {
		t.Fatal("account with balance should not be empty")
	}
	db.SubBalance(addr, big.NewInt(1))
	if !db.Empty(addr) {
		t.Fatal("account with zeroed balance should be empty again")
	}

	// With nonce only -> not empty.
	db.SetNonce(addr, 1)
	if db.Empty(addr) {
		t.Fatal("account with nonce should not be empty")
	}
	db.SetNonce(addr, 0)
	if !db.Empty(addr) {
		t.Fatal("account with zeroed nonce should be empty again")
	}

	// With code only -> not empty.
	db.SetCode(addr, []byte{0x00})
	if db.Empty(addr) {
		t.Fatal("account with code should not be empty")
	}
}

// TestCodeHashEmptyCode verifies that the code hash of an account with no
// code set returns the zero hash (not EmptyCodeHash), while a newly created
// account with NewAccount() semantics returns EmptyCodeHash through Empty().
func TestCodeHashEmptyCode(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xbb04")

	// Non-existent account returns zero hash for code hash.
	zeroHash := db.GetCodeHash(addr)
	if zeroHash != (types.Hash{}) {
		t.Fatalf("non-existent account code hash should be zero, got %s", zeroHash)
	}

	// Created account with NewAccount() gets EmptyCodeHash via the Account
	// constructor.
	db.CreateAccount(addr)
	codeHash := db.GetCodeHash(addr)
	if codeHash != types.EmptyCodeHash {
		t.Fatalf("new account code hash should be EmptyCodeHash, got %s", codeHash)
	}
}

// TestSetCodeUpdatesHash verifies that SetCode properly updates the code hash.
func TestSetCodeUpdatesHash(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xbb05")
	db.CreateAccount(addr)

	code1 := []byte{0x60, 0x00}
	code2 := []byte{0x60, 0x01}

	db.SetCode(addr, code1)
	hash1 := db.GetCodeHash(addr)

	db.SetCode(addr, code2)
	hash2 := db.GetCodeHash(addr)

	if hash1 == hash2 {
		t.Fatal("different code should produce different hashes")
	}
	if hash1 != crypto.Keccak256Hash(code1) {
		t.Fatal("hash1 should be keccak256 of code1")
	}
	if hash2 != crypto.Keccak256Hash(code2) {
		t.Fatal("hash2 should be keccak256 of code2")
	}
}

// =============================================================================
// 3. Storage operations
// =============================================================================

// TestStorageDirtyVsCommitted verifies the separation between dirty and
// committed storage layers.
func TestStorageDirtyVsCommitted(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xcc01")
	key := types.HexToHash("0x01")
	val1 := types.HexToHash("0xaa")
	val2 := types.HexToHash("0xbb")

	db.CreateAccount(addr)

	// Set initial value and commit.
	db.SetState(addr, key, val1)
	if db.GetCommittedState(addr, key) != (types.Hash{}) {
		t.Fatal("committed state should be empty before commit")
	}
	if db.GetState(addr, key) != val1 {
		t.Fatal("GetState should return dirty value")
	}

	db.Commit()
	if db.GetCommittedState(addr, key) != val1 {
		t.Fatal("committed state should be val1 after commit")
	}

	// Overwrite with new dirty value.
	db.SetState(addr, key, val2)
	if db.GetState(addr, key) != val2 {
		t.Fatal("GetState should return new dirty value")
	}
	if db.GetCommittedState(addr, key) != val1 {
		t.Fatal("committed state should still be val1")
	}

	// Commit again to flush.
	db.Commit()
	if db.GetCommittedState(addr, key) != val2 {
		t.Fatal("committed state should be val2 after second commit")
	}
}

// TestStorageAcrossMultipleAccounts verifies that storage is properly
// isolated between accounts.
func TestStorageAcrossMultipleAccounts(t *testing.T) {
	db := NewMemoryStateDB()
	addr1 := types.HexToAddress("0xcc02")
	addr2 := types.HexToAddress("0xcc03")
	key := types.HexToHash("0x01")
	val1 := types.HexToHash("0xaa")
	val2 := types.HexToHash("0xbb")

	db.CreateAccount(addr1)
	db.CreateAccount(addr2)

	// Set same key with different values on different accounts.
	db.SetState(addr1, key, val1)
	db.SetState(addr2, key, val2)

	if db.GetState(addr1, key) != val1 {
		t.Fatal("addr1 storage should be val1")
	}
	if db.GetState(addr2, key) != val2 {
		t.Fatal("addr2 storage should be val2")
	}

	// Modifying addr1 should not affect addr2.
	db.SetState(addr1, key, types.Hash{})
	if db.GetState(addr2, key) != val2 {
		t.Fatal("addr2 storage should not be affected by addr1 changes")
	}
}

// TestStorageSlotDeletion verifies that setting a storage slot to zero
// effectively deletes it.
func TestStorageSlotDeletion(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xcc04")
	key := types.HexToHash("0x01")
	val := types.HexToHash("0xaa")

	db.CreateAccount(addr)
	db.SetState(addr, key, val)
	db.Commit()

	// Delete by setting to zero.
	db.SetState(addr, key, types.Hash{})
	db.Commit()

	// Both dirty and committed should be gone.
	if db.GetState(addr, key) != (types.Hash{}) {
		t.Fatal("deleted storage should return zero")
	}
	if db.GetCommittedState(addr, key) != (types.Hash{}) {
		t.Fatal("deleted committed storage should return zero")
	}
}

// TestStorageRevertRestoresState verifies that reverting a snapshot properly
// restores storage state, including handling the dirty->committed fallback.
func TestStorageRevertRestoresState(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xcc05")
	key1 := types.HexToHash("0x01")
	key2 := types.HexToHash("0x02")
	val1 := types.HexToHash("0xaa")
	val2 := types.HexToHash("0xbb")

	db.CreateAccount(addr)
	db.SetState(addr, key1, val1)

	snap := db.Snapshot()

	// Overwrite key1, add key2.
	db.SetState(addr, key1, val2)
	db.SetState(addr, key2, val2)

	// Verify changes.
	if db.GetState(addr, key1) != val2 {
		t.Fatal("key1 should be val2")
	}
	if db.GetState(addr, key2) != val2 {
		t.Fatal("key2 should be val2")
	}

	// Revert.
	db.RevertToSnapshot(snap)

	// key1 should be back to val1, key2 should be gone.
	if db.GetState(addr, key1) != val1 {
		t.Fatalf("key1 should revert to val1, got %s", db.GetState(addr, key1))
	}
	if db.GetState(addr, key2) != (types.Hash{}) {
		t.Fatalf("key2 should be gone after revert, got %s", db.GetState(addr, key2))
	}
}

// TestStorageMultipleSlotsMultipleAccounts verifies storage isolation and
// correctness across many slots on multiple accounts.
func TestStorageMultipleSlotsMultipleAccounts(t *testing.T) {
	db := NewMemoryStateDB()
	const numAccounts = 5
	const numSlots = 10

	addrs := make([]types.Address, numAccounts)
	for i := range addrs {
		addrs[i] = types.HexToAddress("0xcc10")
		addrs[i][19] = byte(i + 1)
		db.CreateAccount(addrs[i])
	}

	// Set storage.
	for i, addr := range addrs {
		for j := 0; j < numSlots; j++ {
			var key, val types.Hash
			key[31] = byte(j)
			val[31] = byte(i*numSlots + j)
			db.SetState(addr, key, val)
		}
	}

	// Verify storage.
	for i, addr := range addrs {
		for j := 0; j < numSlots; j++ {
			var key types.Hash
			key[31] = byte(j)
			got := db.GetState(addr, key)
			var expected types.Hash
			expected[31] = byte(i*numSlots + j)
			if got != expected {
				t.Fatalf("account %d, slot %d: expected %s, got %s", i, j, expected, got)
			}
		}
	}
}

// =============================================================================
// 4. Trie-backed state
// =============================================================================

// TestTrieBackedIntermediateRootDifferentStates verifies that different
// states produce different IntermediateRoot hashes.
func TestTrieBackedIntermediateRootDifferentStates(t *testing.T) {
	addr := types.HexToAddress("0xdd01")

	db1 := NewTrieBackedStateDB()
	db1.CreateAccount(addr)
	db1.AddBalance(addr, big.NewInt(100))
	db1.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	db2 := NewTrieBackedStateDB()
	db2.CreateAccount(addr)
	db2.AddBalance(addr, big.NewInt(100))
	db2.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xbb"))

	root1 := db1.IntermediateRoot(false)
	root2 := db2.IntermediateRoot(false)

	if root1 == root2 {
		t.Fatal("different storage values should produce different roots")
	}
}

// TestTrieBackedCommitDeterministic verifies that Commit() produces the
// same root as IntermediateRoot().
func TestTrieBackedCommitDeterministic(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0xdd02")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(500))
	db.SetNonce(addr, 3)
	db.SetCode(addr, []byte{0x60, 0x00, 0xf3})
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	db.SetState(addr, types.HexToHash("0x02"), types.HexToHash("0xbb"))

	intermediateRoot := db.IntermediateRoot(false)
	commitRoot, err := db.Commit()
	if err != nil {
		t.Fatalf("Commit error: %v", err)
	}

	if intermediateRoot != commitRoot {
		t.Fatalf("IntermediateRoot %s != Commit %s", intermediateRoot, commitRoot)
	}
}

// TestTrieBackedCommutativity verifies that the same state changes produce
// the same root hash regardless of the order operations are performed. This
// tests commutativity of state changes.
func TestTrieBackedCommutativity(t *testing.T) {
	addr1 := types.HexToAddress("0xdd03")
	addr2 := types.HexToAddress("0xdd04")
	key1 := types.HexToHash("0x01")
	key2 := types.HexToHash("0x02")

	// Build state in order: addr1 first, then addr2.
	db1 := NewTrieBackedStateDB()
	db1.CreateAccount(addr1)
	db1.AddBalance(addr1, big.NewInt(100))
	db1.SetState(addr1, key1, types.HexToHash("0xaa"))
	db1.SetCode(addr1, []byte{0x60, 0x00})
	db1.CreateAccount(addr2)
	db1.AddBalance(addr2, big.NewInt(200))
	db1.SetState(addr2, key2, types.HexToHash("0xbb"))
	db1.SetNonce(addr2, 5)

	// Build state in reverse order: addr2 first, then addr1.
	db2 := NewTrieBackedStateDB()
	db2.CreateAccount(addr2)
	db2.AddBalance(addr2, big.NewInt(200))
	db2.SetState(addr2, key2, types.HexToHash("0xbb"))
	db2.SetNonce(addr2, 5)
	db2.CreateAccount(addr1)
	db2.AddBalance(addr1, big.NewInt(100))
	db2.SetState(addr1, key1, types.HexToHash("0xaa"))
	db2.SetCode(addr1, []byte{0x60, 0x00})

	root1 := db1.IntermediateRoot(false)
	root2 := db2.IntermediateRoot(false)

	if root1 != root2 {
		t.Fatalf("roots should be equal regardless of order: %s vs %s", root1, root2)
	}
}

// TestTrieBackedCommutativityStorage tests that storage slots within the
// same account produce the same root regardless of insertion order.
func TestTrieBackedCommutativityStorage(t *testing.T) {
	addr := types.HexToAddress("0xdd05")

	db1 := NewTrieBackedStateDB()
	db1.CreateAccount(addr)
	db1.AddBalance(addr, big.NewInt(100))
	for i := 0; i < 10; i++ {
		var key, val types.Hash
		key[31] = byte(i)
		val[31] = byte(i + 1)
		db1.SetState(addr, key, val)
	}

	db2 := NewTrieBackedStateDB()
	db2.CreateAccount(addr)
	db2.AddBalance(addr, big.NewInt(100))
	// Insert in reverse order.
	for i := 9; i >= 0; i-- {
		var key, val types.Hash
		key[31] = byte(i)
		val[31] = byte(i + 1)
		db2.SetState(addr, key, val)
	}

	root1 := db1.IntermediateRoot(false)
	root2 := db2.IntermediateRoot(false)

	if root1 != root2 {
		t.Fatalf("storage insertion order should not affect root: %s vs %s", root1, root2)
	}
}

// TestTrieBackedDeleteEmptyWithStorage verifies that EIP-161 deleteEmpty does
// not remove accounts that have storage but are otherwise "empty" (zero
// balance, zero nonce, empty code). Storage alone doesn't prevent deletion
// because the account is considered empty by EIP-161 rules.
func TestTrieBackedDeleteEmptyWithStorage(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0xdd06")

	db.CreateAccount(addr)
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	// Without deleteEmpty, account should be present.
	rootBefore := db.IntermediateRoot(false)
	if rootBefore == types.EmptyRootHash {
		t.Fatal("root should not be empty with account")
	}

	// With deleteEmpty, the account is "empty" per EIP-161 (zero balance/nonce,
	// no code), so it gets deleted even though it has storage.
	db2 := NewTrieBackedStateDB()
	db2.CreateAccount(addr)
	db2.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	rootDeleteEmpty := db2.IntermediateRoot(true)

	if rootDeleteEmpty != types.EmptyRootHash {
		t.Fatalf("empty account (by EIP-161) should be deleted even with storage, got %s",
			rootDeleteEmpty)
	}
}

// TestTrieBackedCommitThenModify verifies that modifying state after Commit
// produces a different root.
func TestTrieBackedCommitThenModify(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0xdd07")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	root1, _ := db.Commit()

	// Modify state.
	db.AddBalance(addr, big.NewInt(1))
	root2, _ := db.Commit()

	if root1 == root2 {
		t.Fatal("modifying state after commit should change root")
	}
}

// TestTrieBackedCopyProducesSameRoot verifies that Copy() produces a state
// with the same root.
func TestTrieBackedCopyProducesSameRoot(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0xdd08")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(1000))
	db.SetNonce(addr, 7)
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	db.SetCode(addr, []byte{0x60, 0x00, 0xf3})

	original := db.IntermediateRoot(false)
	cp := db.Copy()
	copied := cp.IntermediateRoot(false)

	if original != copied {
		t.Fatalf("copy should produce same root: %s vs %s", original, copied)
	}

	// Modify original, verify copy is independent.
	db.AddBalance(addr, big.NewInt(1))
	newOriginal := db.IntermediateRoot(false)

	if newOriginal == original {
		t.Fatal("original should have changed")
	}
	if cp.IntermediateRoot(false) != copied {
		t.Fatal("copy should not be affected by original changes")
	}
}

// =============================================================================
// 5. Access list operations
// =============================================================================

// TestAccessListAddAddressTwice verifies that adding the same address twice
// is idempotent.
func TestAccessListAddAddressTwice(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xee01")

	db.AddAddressToAccessList(addr)
	db.AddAddressToAccessList(addr)

	if !db.AddressInAccessList(addr) {
		t.Fatal("address should be in access list")
	}

	// Taking a snapshot and reverting should still work correctly even
	// though address was added twice. The journal should only record
	// the first add (when the address was not already present).
	snap := db.Snapshot()
	addr2 := types.HexToAddress("0xee02")
	db.AddAddressToAccessList(addr2)
	db.RevertToSnapshot(snap)

	// Original address should still be present, new one should not.
	if !db.AddressInAccessList(addr) {
		t.Fatal("original address should still be in access list")
	}
	if db.AddressInAccessList(addr2) {
		t.Fatal("new address should not be in access list after revert")
	}
}

// TestAccessListAddSlotCreatesAddress verifies that AddSlotToAccessList
// also adds the address to the access list.
func TestAccessListAddSlotCreatesAddress(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xee03")
	slot := types.HexToHash("0x01")

	// Address should not be in access list before.
	if db.AddressInAccessList(addr) {
		t.Fatal("address should not be in access list initially")
	}

	db.AddSlotToAccessList(addr, slot)

	// Both address and slot should now be present.
	addrOk, slotOk := db.SlotInAccessList(addr, slot)
	if !addrOk {
		t.Fatal("address should be in access list after AddSlotToAccessList")
	}
	if !slotOk {
		t.Fatal("slot should be in access list after AddSlotToAccessList")
	}
}

// TestAccessListSlotRevertIndependent verifies that reverting a slot
// addition does not remove the address if it was added before the snapshot.
func TestAccessListSlotRevertIndependent(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xee04")
	slot1 := types.HexToHash("0x01")
	slot2 := types.HexToHash("0x02")

	db.AddAddressToAccessList(addr)
	db.AddSlotToAccessList(addr, slot1)

	snap := db.Snapshot()
	db.AddSlotToAccessList(addr, slot2)

	// Verify slot2 is present.
	_, slotOk := db.SlotInAccessList(addr, slot2)
	if !slotOk {
		t.Fatal("slot2 should be in access list")
	}

	db.RevertToSnapshot(snap)

	// Address and slot1 should still be present, slot2 should not.
	if !db.AddressInAccessList(addr) {
		t.Fatal("address should still be in access list after revert")
	}
	_, slotOk = db.SlotInAccessList(addr, slot1)
	if !slotOk {
		t.Fatal("slot1 should still be in access list after revert")
	}
	_, slotOk = db.SlotInAccessList(addr, slot2)
	if slotOk {
		t.Fatal("slot2 should not be in access list after revert")
	}
}

// TestAccessListMultipleSlotsSameAddress verifies that multiple slots can
// be tracked for the same address.
func TestAccessListMultipleSlotsSameAddress(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xee05")
	slots := []types.Hash{
		types.HexToHash("0x01"),
		types.HexToHash("0x02"),
		types.HexToHash("0x03"),
	}

	for _, slot := range slots {
		db.AddSlotToAccessList(addr, slot)
	}

	for _, slot := range slots {
		_, slotOk := db.SlotInAccessList(addr, slot)
		if !slotOk {
			t.Fatalf("slot %s should be in access list", slot)
		}
	}

	// Slot that was never added should not be present.
	_, slotOk := db.SlotInAccessList(addr, types.HexToHash("0x04"))
	if slotOk {
		t.Fatal("non-added slot should not be in access list")
	}
}

// TestAccessListRevertUndoesBothAddressAndSlot verifies that reverting
// undoes both address and slot additions when they were added in the same
// scope via AddSlotToAccessList (which adds the address implicitly).
func TestAccessListRevertUndoesBothAddressAndSlot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xee06")
	slot := types.HexToHash("0x01")

	snap := db.Snapshot()

	// AddSlotToAccessList adds both the address and the slot.
	db.AddSlotToAccessList(addr, slot)

	if !db.AddressInAccessList(addr) {
		t.Fatal("address should be in access list")
	}
	_, slotOk := db.SlotInAccessList(addr, slot)
	if !slotOk {
		t.Fatal("slot should be in access list")
	}

	db.RevertToSnapshot(snap)

	if db.AddressInAccessList(addr) {
		t.Fatal("address should be removed after revert")
	}
	_, slotOk = db.SlotInAccessList(addr, slot)
	if slotOk {
		t.Fatal("slot should be removed after revert")
	}
}

// TestAccessListNestedSnapshotsMultipleAddresses tests access list behavior
// with multiple addresses across nested snapshots.
func TestAccessListNestedSnapshotsMultipleAddresses(t *testing.T) {
	db := NewMemoryStateDB()
	a1 := types.HexToAddress("0xee07")
	a2 := types.HexToAddress("0xee08")
	a3 := types.HexToAddress("0xee09")
	s1 := types.HexToHash("0x01")
	s2 := types.HexToHash("0x02")

	db.AddAddressToAccessList(a1)

	snap1 := db.Snapshot()
	db.AddSlotToAccessList(a1, s1) // addr already present, just add slot
	db.AddAddressToAccessList(a2)

	snap2 := db.Snapshot()
	db.AddSlotToAccessList(a2, s2) // addr already present, just add slot
	db.AddAddressToAccessList(a3)

	// Revert snap2: undo a3 and a2/s2.
	db.RevertToSnapshot(snap2)
	if db.AddressInAccessList(a3) {
		t.Fatal("a3 should be gone")
	}
	_, slotOk := db.SlotInAccessList(a2, s2)
	if slotOk {
		t.Fatal("s2 should be gone from a2")
	}
	if !db.AddressInAccessList(a2) {
		t.Fatal("a2 should still be present (added before snap2)")
	}

	// Revert snap1: undo a2 and a1/s1.
	db.RevertToSnapshot(snap1)
	if db.AddressInAccessList(a2) {
		t.Fatal("a2 should be gone")
	}
	_, slotOk = db.SlotInAccessList(a1, s1)
	if slotOk {
		t.Fatal("s1 should be gone from a1")
	}
	if !db.AddressInAccessList(a1) {
		t.Fatal("a1 should still be present (added before snap1)")
	}
}

// =============================================================================
// 6. Transient storage
// =============================================================================

// TestClearTransientStorageEmptiesAll verifies that ClearTransientStorage
// removes all entries across all addresses and keys.
func TestClearTransientStorageEmptiesAll(t *testing.T) {
	db := NewMemoryStateDB()
	addrs := []types.Address{
		types.HexToAddress("0xff01"),
		types.HexToAddress("0xff02"),
		types.HexToAddress("0xff03"),
	}
	keys := []types.Hash{
		types.HexToHash("0x01"),
		types.HexToHash("0x02"),
		types.HexToHash("0x03"),
	}

	// Set transient storage for all combinations.
	for _, addr := range addrs {
		for _, key := range keys {
			var val types.Hash
			val[0] = addr[19]
			val[31] = key[31]
			db.SetTransientState(addr, key, val)
		}
	}

	// Verify at least one is set.
	got := db.GetTransientState(addrs[0], keys[0])
	if got == (types.Hash{}) {
		t.Fatal("transient state should be set")
	}

	// Clear all.
	db.ClearTransientStorage()

	// Verify all are cleared.
	for _, addr := range addrs {
		for _, key := range keys {
			got := db.GetTransientState(addr, key)
			if got != (types.Hash{}) {
				t.Fatalf("transient storage should be empty after clear, got %s", got)
			}
		}
	}
}

// TestTransientStorageDoesNotPersistAcrossClears verifies the per-transaction
// semantics of transient storage.
func TestTransientStorageDoesNotPersistAcrossClears(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xff04")
	key := types.HexToHash("0x01")
	val := types.HexToHash("0xaa")

	db.SetTransientState(addr, key, val)
	if db.GetTransientState(addr, key) != val {
		t.Fatal("transient state should be set")
	}

	// Simulate end-of-transaction clear.
	db.ClearTransientStorage()

	// Should be gone.
	if db.GetTransientState(addr, key) != (types.Hash{}) {
		t.Fatal("transient state should be empty after clear")
	}

	// Set a different value in the "next transaction."
	newVal := types.HexToHash("0xbb")
	db.SetTransientState(addr, key, newVal)
	if db.GetTransientState(addr, key) != newVal {
		t.Fatalf("expected %s, got %s", newVal, db.GetTransientState(addr, key))
	}
}

// TestTransientStorageRevertToZero verifies that reverting transient storage
// to its initial zero state works correctly.
func TestTransientStorageRevertToZero(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xff05")
	key := types.HexToHash("0x01")

	// No transient storage set yet.
	snap := db.Snapshot()

	db.SetTransientState(addr, key, types.HexToHash("0xaa"))
	if db.GetTransientState(addr, key) != types.HexToHash("0xaa") {
		t.Fatal("expected 0xaa")
	}

	db.RevertToSnapshot(snap)

	// Should be back to zero.
	if db.GetTransientState(addr, key) != (types.Hash{}) {
		t.Fatalf("expected zero after revert, got %s", db.GetTransientState(addr, key))
	}
}

// TestTransientStorageIsolation verifies that transient storage does not
// affect regular storage and vice versa.
func TestTransientStorageIsolation(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xff06")
	key := types.HexToHash("0x01")

	db.CreateAccount(addr)

	// Set both regular and transient storage on the same address/key.
	db.SetState(addr, key, types.HexToHash("0xaa"))
	db.SetTransientState(addr, key, types.HexToHash("0xbb"))

	// They should be independent.
	if db.GetState(addr, key) != types.HexToHash("0xaa") {
		t.Fatal("regular state should be 0xaa")
	}
	if db.GetTransientState(addr, key) != types.HexToHash("0xbb") {
		t.Fatal("transient state should be 0xbb")
	}

	// Clearing transient should not affect regular.
	db.ClearTransientStorage()
	if db.GetState(addr, key) != types.HexToHash("0xaa") {
		t.Fatal("regular state should not be affected by ClearTransientStorage")
	}
	if db.GetTransientState(addr, key) != (types.Hash{}) {
		t.Fatal("transient state should be empty after clear")
	}
}

// =============================================================================
// Additional edge case tests
// =============================================================================

// TestCopyIndependence verifies that a Copy() of MemoryStateDB is fully
// independent from the original.
func TestCopyIndependence(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xfa01")
	key := types.HexToHash("0x01")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))
	db.SetState(addr, key, types.HexToHash("0xaa"))
	db.SetTransientState(addr, key, types.HexToHash("0xbb"))
	db.AddAddressToAccessList(addr)
	db.AddRefund(50)

	cp := db.Copy()

	// Modify original.
	db.AddBalance(addr, big.NewInt(900))
	db.SetState(addr, key, types.HexToHash("0xff"))
	db.SetTransientState(addr, key, types.HexToHash("0xff"))
	db.AddRefund(1000)

	// Copy should be unaffected.
	if cp.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("copy balance should be 100, got %s", cp.GetBalance(addr))
	}
	if cp.GetState(addr, key) != types.HexToHash("0xaa") {
		t.Fatal("copy storage should be 0xaa")
	}
	if cp.GetTransientState(addr, key) != types.HexToHash("0xbb") {
		t.Fatal("copy transient should be 0xbb")
	}
	if cp.GetRefund() != 50 {
		t.Fatalf("copy refund should be 50, got %d", cp.GetRefund())
	}
}

// TestMergeAppliesChanges verifies that Merge() correctly applies state
// changes from one MemoryStateDB to another.
func TestMergeAppliesChanges(t *testing.T) {
	db1 := NewMemoryStateDB()
	db2 := NewMemoryStateDB()
	addr := types.HexToAddress("0xfa02")

	db1.CreateAccount(addr)
	db1.AddBalance(addr, big.NewInt(100))

	db2.CreateAccount(addr)
	db2.AddBalance(addr, big.NewInt(500))
	db2.SetNonce(addr, 10)
	db2.SetCode(addr, []byte{0x60, 0x00})
	db2.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	db1.Merge(db2)

	// db1 should now have db2's state.
	if db1.GetBalance(addr).Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("merged balance should be 500, got %s", db1.GetBalance(addr))
	}
	if db1.GetNonce(addr) != 10 {
		t.Fatalf("merged nonce should be 10, got %d", db1.GetNonce(addr))
	}
	if db1.GetCodeSize(addr) != 2 {
		t.Fatalf("merged code size should be 2, got %d", db1.GetCodeSize(addr))
	}
	if db1.GetState(addr, types.HexToHash("0x01")) != types.HexToHash("0xaa") {
		t.Fatal("merged storage should be 0xaa")
	}
}

// TestGetCodeSizeNonExistentAccount verifies GetCodeSize returns 0 for
// non-existent accounts.
func TestGetCodeSizeNonExistentAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xfa03")

	if db.GetCodeSize(addr) != 0 {
		t.Fatalf("expected 0, got %d", db.GetCodeSize(addr))
	}
}

// TestGetCodeNonExistentAccount verifies GetCode returns nil for
// non-existent accounts.
func TestGetCodeNonExistentAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xfa04")

	if db.GetCode(addr) != nil {
		t.Fatal("expected nil code for non-existent account")
	}
}

// TestGetNonceNonExistentAccount verifies GetNonce returns 0 for
// non-existent accounts.
func TestGetNonceNonExistentAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xfa05")

	if db.GetNonce(addr) != 0 {
		t.Fatalf("expected 0, got %d", db.GetNonce(addr))
	}
}

// TestGetBalanceNonExistentAccount verifies GetBalance returns zero for
// non-existent accounts.
func TestGetBalanceNonExistentAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xfa06")

	bal := db.GetBalance(addr)
	if bal.Sign() != 0 {
		t.Fatalf("expected zero balance, got %s", bal)
	}
}

// TestExistAfterCreateAndRevert verifies that Exist() returns false after
// reverting a CreateAccount.
func TestExistAfterCreateAndRevert(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xfa07")

	if db.Exist(addr) {
		t.Fatal("account should not exist initially")
	}

	snap := db.Snapshot()
	db.CreateAccount(addr)
	if !db.Exist(addr) {
		t.Fatal("account should exist after creation")
	}

	db.RevertToSnapshot(snap)
	if db.Exist(addr) {
		t.Fatal("account should not exist after revert")
	}
}

// TestGetStateNonExistentAccount verifies that GetState returns zero for
// accounts that don't exist.
func TestGetStateNonExistentAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xfa08")

	got := db.GetState(addr, types.HexToHash("0x01"))
	if got != (types.Hash{}) {
		t.Fatalf("expected zero for non-existent account, got %s", got)
	}
}

// TestGetCommittedStateNonExistentAccount verifies GetCommittedState returns
// zero for non-existent accounts.
func TestGetCommittedStateNonExistentAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xfa09")

	got := db.GetCommittedState(addr, types.HexToHash("0x01"))
	if got != (types.Hash{}) {
		t.Fatalf("expected zero for non-existent account, got %s", got)
	}
}

// TestLogTxContextAttribution verifies that logs are attributed to the
// correct transaction via SetTxContext.
func TestLogTxContextAttribution(t *testing.T) {
	db := NewMemoryStateDB()
	tx1 := types.HexToHash("0x01")
	tx2 := types.HexToHash("0x02")

	db.SetTxContext(tx1, 0)
	db.AddLog(&types.Log{Data: []byte{1}})
	db.AddLog(&types.Log{Data: []byte{2}})

	db.SetTxContext(tx2, 1)
	db.AddLog(&types.Log{Data: []byte{3}})

	logs1 := db.GetLogs(tx1)
	logs2 := db.GetLogs(tx2)

	if len(logs1) != 2 {
		t.Fatalf("expected 2 logs for tx1, got %d", len(logs1))
	}
	if len(logs2) != 1 {
		t.Fatalf("expected 1 log for tx2, got %d", len(logs2))
	}

	// Verify TxHash is set correctly on the log.
	if logs1[0].TxHash != tx1 {
		t.Fatalf("log tx hash should be tx1, got %s", logs1[0].TxHash)
	}
	if logs2[0].TxHash != tx2 {
		t.Fatalf("log tx hash should be tx2, got %s", logs2[0].TxHash)
	}

	// Verify TxIndex is set correctly.
	if logs1[0].TxIndex != 0 {
		t.Fatalf("log tx index should be 0, got %d", logs1[0].TxIndex)
	}
	if logs2[0].TxIndex != 1 {
		t.Fatalf("log tx index should be 1, got %d", logs2[0].TxIndex)
	}
}

// TestSubBalanceCreatesAccount verifies that SubBalance on a non-existent
// account creates it with a negative balance (matching geth behavior where
// the caller must ensure sufficient funds).
func TestSubBalanceCreatesAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xfa0a")

	// SubBalance on non-existent account triggers getOrNewStateObject.
	db.SubBalance(addr, big.NewInt(10))

	if !db.Exist(addr) {
		t.Fatal("account should exist after SubBalance")
	}
	// Balance will be negative (the caller is responsible for checking).
	if db.GetBalance(addr).Cmp(big.NewInt(-10)) != 0 {
		t.Fatalf("expected -10, got %s", db.GetBalance(addr))
	}
}

// TestSetNonceCreatesAccount verifies that SetNonce on a non-existent
// account auto-creates it.
func TestSetNonceCreatesAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xfa0b")

	db.SetNonce(addr, 42)

	if !db.Exist(addr) {
		t.Fatal("account should exist after SetNonce")
	}
	if db.GetNonce(addr) != 42 {
		t.Fatalf("expected nonce 42, got %d", db.GetNonce(addr))
	}
}

// TestSetStateCreatesAccount verifies that SetState on a non-existent
// account auto-creates it.
func TestSetStateCreatesAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xfa0c")

	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	if !db.Exist(addr) {
		t.Fatal("account should exist after SetState")
	}
	if db.GetState(addr, types.HexToHash("0x01")) != types.HexToHash("0xaa") {
		t.Fatal("storage should be set")
	}
}

// TestSnapshotRevertRootConsistency verifies that the state root before
// a snapshot is taken matches the root after reverting to that snapshot,
// even with complex state changes in between.
func TestSnapshotRevertRootConsistency(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr1 := types.HexToAddress("0xfa0d")
	addr2 := types.HexToAddress("0xfa0e")

	db.CreateAccount(addr1)
	db.AddBalance(addr1, big.NewInt(1000))
	db.SetNonce(addr1, 5)
	db.SetCode(addr1, []byte{0x60, 0x00, 0xf3})
	db.SetState(addr1, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	rootBefore := db.IntermediateRoot(false)

	snap := db.Snapshot()

	// Make a bunch of changes.
	db.AddBalance(addr1, big.NewInt(500))
	db.SetNonce(addr1, 10)
	db.SetCode(addr1, []byte{0x60, 0x01, 0xf3})
	db.SetState(addr1, types.HexToHash("0x01"), types.HexToHash("0xbb"))
	db.SetState(addr1, types.HexToHash("0x02"), types.HexToHash("0xcc"))
	db.CreateAccount(addr2)
	db.AddBalance(addr2, big.NewInt(2000))

	// Root should have changed.
	rootChanged := db.IntermediateRoot(false)
	if rootBefore == rootChanged {
		t.Fatal("root should change after modifications")
	}

	// Revert and verify root is restored.
	db.RevertToSnapshot(snap)
	rootAfter := db.IntermediateRoot(false)
	if rootBefore != rootAfter {
		t.Fatalf("root should be restored: before=%s, after=%s", rootBefore, rootAfter)
	}
}
