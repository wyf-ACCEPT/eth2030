package state

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// --- Nested snapshot tests ---

// TestNestedSnapshotBasic verifies that nested snapshots work: inner revert
// undoes inner changes while preserving outer changes.
func TestNestedSnapshotBasic(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x01")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))

	outer := db.Snapshot()

	db.AddBalance(addr, big.NewInt(50)) // balance = 150
	db.SetNonce(addr, 10)

	inner := db.Snapshot()

	db.AddBalance(addr, big.NewInt(25)) // balance = 175
	db.SetNonce(addr, 20)

	// Verify current state.
	if db.GetBalance(addr).Cmp(big.NewInt(175)) != 0 {
		t.Fatalf("expected 175 before inner revert, got %s", db.GetBalance(addr))
	}
	if db.GetNonce(addr) != 20 {
		t.Fatalf("expected nonce 20, got %d", db.GetNonce(addr))
	}

	// Revert inner snapshot: should undo inner changes only.
	db.RevertToSnapshot(inner)

	if db.GetBalance(addr).Cmp(big.NewInt(150)) != 0 {
		t.Fatalf("expected 150 after inner revert, got %s", db.GetBalance(addr))
	}
	if db.GetNonce(addr) != 10 {
		t.Fatalf("expected nonce 10 after inner revert, got %d", db.GetNonce(addr))
	}

	// Revert outer snapshot: should undo all changes since outer snapshot.
	db.RevertToSnapshot(outer)

	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected 100 after outer revert, got %s", db.GetBalance(addr))
	}
	if db.GetNonce(addr) != 0 {
		t.Fatalf("expected nonce 0 after outer revert, got %d", db.GetNonce(addr))
	}
}

// TestNestedSnapshotThreeLevels verifies three levels of nested snapshots.
func TestNestedSnapshotThreeLevels(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x02")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))

	snap1 := db.Snapshot()
	db.AddBalance(addr, big.NewInt(10)) // 110

	snap2 := db.Snapshot()
	db.AddBalance(addr, big.NewInt(20)) // 130

	snap3 := db.Snapshot()
	db.AddBalance(addr, big.NewInt(30)) // 160

	if db.GetBalance(addr).Cmp(big.NewInt(160)) != 0 {
		t.Fatalf("expected 160, got %s", db.GetBalance(addr))
	}

	// Revert to snap3: undo the +30.
	db.RevertToSnapshot(snap3)
	if db.GetBalance(addr).Cmp(big.NewInt(130)) != 0 {
		t.Fatalf("expected 130 after snap3 revert, got %s", db.GetBalance(addr))
	}

	// Revert to snap2: undo the +20.
	db.RevertToSnapshot(snap2)
	if db.GetBalance(addr).Cmp(big.NewInt(110)) != 0 {
		t.Fatalf("expected 110 after snap2 revert, got %s", db.GetBalance(addr))
	}

	// Revert to snap1: undo the +10.
	db.RevertToSnapshot(snap1)
	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected 100 after snap1 revert, got %s", db.GetBalance(addr))
	}
}

// TestNestedSnapshotSkipMiddle verifies that reverting directly to an outer
// snapshot correctly undoes all inner changes (skipping over inner snapshots).
func TestNestedSnapshotSkipMiddle(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x03")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))

	snap1 := db.Snapshot()
	db.AddBalance(addr, big.NewInt(10)) // 110

	_ = db.Snapshot() // snap2 -- not used for revert
	db.AddBalance(addr, big.NewInt(20)) // 130

	_ = db.Snapshot() // snap3 -- not used for revert
	db.AddBalance(addr, big.NewInt(30)) // 160

	// Revert directly to snap1, skipping snap2 and snap3.
	db.RevertToSnapshot(snap1)
	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected 100 after skipping to snap1, got %s", db.GetBalance(addr))
	}
}

// TestNestedSnapshotStorage verifies that storage changes are properly
// reverted in nested snapshots.
func TestNestedSnapshotStorage(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x04")
	key1 := types.HexToHash("0x01")
	key2 := types.HexToHash("0x02")
	val1 := types.HexToHash("0xaa")
	val2 := types.HexToHash("0xbb")
	val3 := types.HexToHash("0xcc")

	db.CreateAccount(addr)
	db.SetState(addr, key1, val1)

	snap1 := db.Snapshot()
	db.SetState(addr, key1, val2) // overwrite key1
	db.SetState(addr, key2, val2) // new key2

	snap2 := db.Snapshot()
	db.SetState(addr, key1, val3) // overwrite key1 again
	db.SetState(addr, key2, val3) // overwrite key2

	// Revert inner.
	db.RevertToSnapshot(snap2)
	if db.GetState(addr, key1) != val2 {
		t.Fatalf("key1 should be val2 after inner revert, got %s", db.GetState(addr, key1))
	}
	if db.GetState(addr, key2) != val2 {
		t.Fatalf("key2 should be val2 after inner revert, got %s", db.GetState(addr, key2))
	}

	// Revert outer.
	db.RevertToSnapshot(snap1)
	if db.GetState(addr, key1) != val1 {
		t.Fatalf("key1 should be val1 after outer revert, got %s", db.GetState(addr, key1))
	}
	if db.GetState(addr, key2) != (types.Hash{}) {
		t.Fatalf("key2 should be empty after outer revert, got %s", db.GetState(addr, key2))
	}
}

// TestNestedSnapshotStorageWithCommittedState verifies that reverting storage
// changes correctly falls back to committed state when the dirty slot is
// removed (as opposed to setting it to zero hash).
func TestNestedSnapshotStorageWithCommittedState(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x05")
	key := types.HexToHash("0x01")
	committedVal := types.HexToHash("0xaa")
	dirtyVal := types.HexToHash("0xbb")

	db.CreateAccount(addr)
	db.SetState(addr, key, committedVal)
	db.Commit() // committedVal is now in committedStorage

	if db.GetCommittedState(addr, key) != committedVal {
		t.Fatalf("committed state should be %s", committedVal)
	}

	snap := db.Snapshot()
	db.SetState(addr, key, dirtyVal)

	if db.GetState(addr, key) != dirtyVal {
		t.Fatalf("expected dirty val %s, got %s", dirtyVal, db.GetState(addr, key))
	}

	db.RevertToSnapshot(snap)

	// After revert, dirty slot should be removed, so GetState should return
	// the committed value.
	if db.GetState(addr, key) != committedVal {
		t.Fatalf("after revert, expected committed val %s, got %s", committedVal, db.GetState(addr, key))
	}
}

// TestNestedSnapshotCode verifies code changes revert correctly in nested
// snapshots.
func TestNestedSnapshotCode(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x06")

	db.CreateAccount(addr)
	code1 := []byte{0x60, 0x00} // PUSH1 0
	db.SetCode(addr, code1)
	hash1 := crypto.Keccak256Hash(code1)

	snap1 := db.Snapshot()
	code2 := []byte{0x60, 0x01, 0x60, 0x00} // PUSH1 1 PUSH1 0
	db.SetCode(addr, code2)
	hash2 := crypto.Keccak256Hash(code2)

	snap2 := db.Snapshot()
	code3 := []byte{0x60, 0x02, 0x60, 0x01, 0x60, 0x00} // PUSH1 2 PUSH1 1 PUSH1 0
	db.SetCode(addr, code3)

	if db.GetCodeSize(addr) != len(code3) {
		t.Fatalf("expected code3 length %d, got %d", len(code3), db.GetCodeSize(addr))
	}

	db.RevertToSnapshot(snap2)
	if db.GetCodeSize(addr) != len(code2) {
		t.Fatalf("expected code2 length %d, got %d", len(code2), db.GetCodeSize(addr))
	}
	if db.GetCodeHash(addr) != hash2 {
		t.Fatalf("expected hash2 after snap2 revert")
	}

	db.RevertToSnapshot(snap1)
	if db.GetCodeSize(addr) != len(code1) {
		t.Fatalf("expected code1 length %d, got %d", len(code1), db.GetCodeSize(addr))
	}
	if db.GetCodeHash(addr) != hash1 {
		t.Fatalf("expected hash1 after snap1 revert")
	}
}

// TestNestedSnapshotSelfDestruct verifies self-destruct reverts correctly in
// nested snapshots.
func TestNestedSnapshotSelfDestruct(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x07")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(1000))

	snap1 := db.Snapshot()
	db.SelfDestruct(addr)

	if !db.HasSelfDestructed(addr) {
		t.Fatal("should be self-destructed")
	}
	if db.GetBalance(addr).Sign() != 0 {
		t.Fatal("self-destructed account should have zero balance")
	}

	snap2 := db.Snapshot()
	// After self-destruct, do more operations (simulating a call that
	// receives value after self-destruct).
	db.AddBalance(addr, big.NewInt(500))

	db.RevertToSnapshot(snap2)
	// Should still be self-destructed with zero balance.
	if !db.HasSelfDestructed(addr) {
		t.Fatal("should still be self-destructed after inner revert")
	}
	if db.GetBalance(addr).Sign() != 0 {
		t.Fatal("balance should still be zero after inner revert")
	}

	db.RevertToSnapshot(snap1)
	// Should be fully restored.
	if db.HasSelfDestructed(addr) {
		t.Fatal("self-destruct should be reverted")
	}
	if db.GetBalance(addr).Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("expected balance 1000, got %s", db.GetBalance(addr))
	}
}

// TestNestedSnapshotAccessList verifies access list changes revert correctly
// in nested snapshots.
func TestNestedSnapshotAccessList(t *testing.T) {
	db := NewMemoryStateDB()
	addr1 := types.HexToAddress("0x08")
	addr2 := types.HexToAddress("0x09")
	slot1 := types.HexToHash("0x01")
	slot2 := types.HexToHash("0x02")

	db.AddAddressToAccessList(addr1)

	snap1 := db.Snapshot()
	db.AddSlotToAccessList(addr1, slot1)
	db.AddAddressToAccessList(addr2)

	snap2 := db.Snapshot()
	db.AddSlotToAccessList(addr2, slot2)

	// Verify current state.
	if !db.AddressInAccessList(addr2) {
		t.Fatal("addr2 should be in access list")
	}
	_, slotOk := db.SlotInAccessList(addr2, slot2)
	if !slotOk {
		t.Fatal("slot2 should be in access list for addr2")
	}

	// Revert inner.
	db.RevertToSnapshot(snap2)
	if !db.AddressInAccessList(addr2) {
		t.Fatal("addr2 should still be in access list after inner revert")
	}
	_, slotOk = db.SlotInAccessList(addr2, slot2)
	if slotOk {
		t.Fatal("slot2 should not be in access list after inner revert")
	}

	// Revert outer.
	db.RevertToSnapshot(snap1)
	if db.AddressInAccessList(addr2) {
		t.Fatal("addr2 should not be in access list after outer revert")
	}
	_, slotOk = db.SlotInAccessList(addr1, slot1)
	if slotOk {
		t.Fatal("slot1 should not be in access list after outer revert")
	}
	if !db.AddressInAccessList(addr1) {
		t.Fatal("addr1 should still be in access list (added before snap1)")
	}
}

// TestNestedSnapshotTransientStorage verifies transient storage changes
// revert correctly in nested snapshots.
func TestNestedSnapshotTransientStorage(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x0a")
	key := types.HexToHash("0x01")
	val1 := types.HexToHash("0xaa")
	val2 := types.HexToHash("0xbb")
	val3 := types.HexToHash("0xcc")

	db.SetTransientState(addr, key, val1)

	snap1 := db.Snapshot()
	db.SetTransientState(addr, key, val2)

	snap2 := db.Snapshot()
	db.SetTransientState(addr, key, val3)

	db.RevertToSnapshot(snap2)
	if db.GetTransientState(addr, key) != val2 {
		t.Fatalf("expected val2 after inner revert, got %s", db.GetTransientState(addr, key))
	}

	db.RevertToSnapshot(snap1)
	if db.GetTransientState(addr, key) != val1 {
		t.Fatalf("expected val1 after outer revert, got %s", db.GetTransientState(addr, key))
	}
}

// TestNestedSnapshotTransientStorageNewKey verifies that reverting transient
// storage correctly removes keys that didn't exist before.
func TestNestedSnapshotTransientStorageNewKey(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x0b")
	key := types.HexToHash("0x01")
	val := types.HexToHash("0xdd")

	snap := db.Snapshot()
	db.SetTransientState(addr, key, val)

	if db.GetTransientState(addr, key) != val {
		t.Fatalf("expected %s, got %s", val, db.GetTransientState(addr, key))
	}

	db.RevertToSnapshot(snap)
	if db.GetTransientState(addr, key) != (types.Hash{}) {
		t.Fatalf("transient storage should be empty after revert, got %s",
			db.GetTransientState(addr, key))
	}
}

// TestNestedSnapshotRefund verifies refund counter reverts correctly in
// nested snapshots.
func TestNestedSnapshotRefund(t *testing.T) {
	db := NewMemoryStateDB()

	db.AddRefund(100)

	snap1 := db.Snapshot()
	db.AddRefund(50) // 150

	snap2 := db.Snapshot()
	db.AddRefund(25) // 175

	if db.GetRefund() != 175 {
		t.Fatalf("expected refund 175, got %d", db.GetRefund())
	}

	db.RevertToSnapshot(snap2)
	if db.GetRefund() != 150 {
		t.Fatalf("expected refund 150 after inner revert, got %d", db.GetRefund())
	}

	db.RevertToSnapshot(snap1)
	if db.GetRefund() != 100 {
		t.Fatalf("expected refund 100 after outer revert, got %d", db.GetRefund())
	}
}

// TestNestedSnapshotLogs verifies logs revert correctly in nested snapshots.
func TestNestedSnapshotLogs(t *testing.T) {
	db := NewMemoryStateDB()
	txHash := types.HexToHash("0xaa")
	db.SetTxContext(txHash, 0)

	db.AddLog(&types.Log{Data: []byte{1}})

	snap1 := db.Snapshot()
	db.AddLog(&types.Log{Data: []byte{2}})

	snap2 := db.Snapshot()
	db.AddLog(&types.Log{Data: []byte{3}})

	if len(db.GetLogs(txHash)) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(db.GetLogs(txHash)))
	}

	db.RevertToSnapshot(snap2)
	if len(db.GetLogs(txHash)) != 2 {
		t.Fatalf("expected 2 logs after inner revert, got %d", len(db.GetLogs(txHash)))
	}

	db.RevertToSnapshot(snap1)
	if len(db.GetLogs(txHash)) != 1 {
		t.Fatalf("expected 1 log after outer revert, got %d", len(db.GetLogs(txHash)))
	}
}

// TestCreateAccountOverwrite verifies that CreateAccount on an existing
// address properly saves and restores the old state on revert.
func TestCreateAccountOverwrite(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x0c")

	// Create initial account with state.
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(500))
	db.SetNonce(addr, 7)
	db.SetCode(addr, []byte{0x60, 0x00})
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	snap := db.Snapshot()

	// Overwrite the account (this happens e.g. in CREATE2 to the same address).
	db.CreateAccount(addr)

	// The account should be fresh.
	if db.GetBalance(addr).Sign() != 0 {
		t.Fatal("overwritten account should have zero balance")
	}
	if db.GetNonce(addr) != 0 {
		t.Fatal("overwritten account should have zero nonce")
	}
	if db.GetCodeSize(addr) != 0 {
		t.Fatal("overwritten account should have no code")
	}
	if db.GetState(addr, types.HexToHash("0x01")) != (types.Hash{}) {
		t.Fatal("overwritten account should have no storage")
	}

	// Revert should restore the original account.
	db.RevertToSnapshot(snap)

	if db.GetBalance(addr).Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("expected balance 500 after revert, got %s", db.GetBalance(addr))
	}
	if db.GetNonce(addr) != 7 {
		t.Fatalf("expected nonce 7 after revert, got %d", db.GetNonce(addr))
	}
	if db.GetCodeSize(addr) != 2 {
		t.Fatalf("expected code size 2 after revert, got %d", db.GetCodeSize(addr))
	}
	if db.GetState(addr, types.HexToHash("0x01")) != types.HexToHash("0xaa") {
		t.Fatalf("expected storage 0xaa after revert, got %s",
			db.GetState(addr, types.HexToHash("0x01")))
	}
}

// TestCreateAccountOnNonExistent verifies CreateAccount on a non-existent
// address creates a new account that gets deleted on revert.
func TestCreateAccountOnNonExistent(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x0d")

	snap := db.Snapshot()
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))

	if !db.Exist(addr) {
		t.Fatal("account should exist after creation")
	}

	db.RevertToSnapshot(snap)

	if db.Exist(addr) {
		t.Fatal("account should not exist after revert")
	}
}

// TestSnapshotAfterRevert verifies that taking a new snapshot after a revert
// works correctly and doesn't interfere with the reverted state.
func TestSnapshotAfterRevert(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x0e")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))

	snap1 := db.Snapshot()
	db.AddBalance(addr, big.NewInt(50)) // 150

	// Revert to snap1.
	db.RevertToSnapshot(snap1)
	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected 100 after revert, got %s", db.GetBalance(addr))
	}

	// Take a new snapshot and make more changes.
	snap2 := db.Snapshot()
	db.AddBalance(addr, big.NewInt(200)) // 300

	if db.GetBalance(addr).Cmp(big.NewInt(300)) != 0 {
		t.Fatalf("expected 300, got %s", db.GetBalance(addr))
	}

	// Revert to snap2.
	db.RevertToSnapshot(snap2)
	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected 100 after second revert, got %s", db.GetBalance(addr))
	}
}

// TestSnapshotIDsAreStrictlyIncreasing verifies that snapshot IDs increase
// monotonically even after reverts.
func TestSnapshotIDsAreStrictlyIncreasing(t *testing.T) {
	db := NewMemoryStateDB()

	snap1 := db.Snapshot()
	snap2 := db.Snapshot()
	snap3 := db.Snapshot()

	if snap2 <= snap1 || snap3 <= snap2 {
		t.Fatalf("snapshot IDs should be strictly increasing: %d, %d, %d",
			snap1, snap2, snap3)
	}

	db.RevertToSnapshot(snap1)

	snap4 := db.Snapshot()
	if snap4 <= snap3 {
		t.Fatalf("snapshot ID after revert should still be increasing: %d > %d",
			snap4, snap3)
	}
}

// TestRevertInvalidSnapshotIsNoop verifies that reverting to a non-existent
// snapshot ID is a no-op and doesn't corrupt state.
func TestRevertInvalidSnapshotIsNoop(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x0f")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))

	// Revert to a snapshot that was never taken.
	db.RevertToSnapshot(999)

	// State should be unchanged.
	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected 100 after invalid revert, got %s", db.GetBalance(addr))
	}
}

// TestNestedSnapshotMixedOperations verifies a realistic scenario where
// multiple types of state changes are interspersed with nested snapshots.
func TestNestedSnapshotMixedOperations(t *testing.T) {
	db := NewMemoryStateDB()
	addr1 := types.HexToAddress("0x10")
	addr2 := types.HexToAddress("0x11")
	storageKey := types.HexToHash("0x01")
	txHash := types.HexToHash("0xaa")

	// Initial setup.
	db.CreateAccount(addr1)
	db.AddBalance(addr1, big.NewInt(1000))
	db.SetTxContext(txHash, 0)

	// --- Outer snapshot (simulating a CALL) ---
	snapOuter := db.Snapshot()

	db.AddBalance(addr1, big.NewInt(100)) // 1100
	db.SetNonce(addr1, 1)
	db.SetState(addr1, storageKey, types.HexToHash("0xaa"))
	db.AddRefund(200)
	db.AddLog(&types.Log{Data: []byte{1}})
	db.AddAddressToAccessList(addr1)
	db.SetTransientState(addr1, storageKey, types.HexToHash("0xbb"))

	// --- Inner snapshot (simulating a nested CALL) ---
	snapInner := db.Snapshot()

	db.CreateAccount(addr2)
	db.AddBalance(addr2, big.NewInt(500))
	db.SubBalance(addr1, big.NewInt(500)) // 600
	db.SetState(addr1, storageKey, types.HexToHash("0xcc"))
	db.AddRefund(100) // 300
	db.AddLog(&types.Log{Data: []byte{2}})
	db.AddSlotToAccessList(addr1, storageKey)
	db.SetTransientState(addr1, storageKey, types.HexToHash("0xdd"))

	// Verify state after inner changes.
	if db.GetBalance(addr1).Cmp(big.NewInt(600)) != 0 {
		t.Fatalf("expected addr1 balance 600, got %s", db.GetBalance(addr1))
	}
	if db.GetBalance(addr2).Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("expected addr2 balance 500, got %s", db.GetBalance(addr2))
	}

	// --- Revert inner (nested CALL failed) ---
	db.RevertToSnapshot(snapInner)

	if db.GetBalance(addr1).Cmp(big.NewInt(1100)) != 0 {
		t.Fatalf("expected addr1 balance 1100 after inner revert, got %s", db.GetBalance(addr1))
	}
	if db.Exist(addr2) {
		t.Fatal("addr2 should not exist after inner revert")
	}
	if db.GetState(addr1, storageKey) != types.HexToHash("0xaa") {
		t.Fatalf("storage should revert to 0xaa, got %s", db.GetState(addr1, storageKey))
	}
	if db.GetRefund() != 200 {
		t.Fatalf("refund should be 200, got %d", db.GetRefund())
	}
	if len(db.GetLogs(txHash)) != 1 {
		t.Fatalf("expected 1 log after inner revert, got %d", len(db.GetLogs(txHash)))
	}
	_, slotOk := db.SlotInAccessList(addr1, storageKey)
	if slotOk {
		t.Fatal("slot should not be in access list after inner revert")
	}
	if db.GetTransientState(addr1, storageKey) != types.HexToHash("0xbb") {
		t.Fatalf("transient storage should revert to 0xbb, got %s",
			db.GetTransientState(addr1, storageKey))
	}

	// --- Revert outer (CALL failed entirely) ---
	db.RevertToSnapshot(snapOuter)

	if db.GetBalance(addr1).Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("expected addr1 balance 1000 after outer revert, got %s", db.GetBalance(addr1))
	}
	if db.GetNonce(addr1) != 0 {
		t.Fatalf("expected nonce 0 after outer revert, got %d", db.GetNonce(addr1))
	}
	if db.GetState(addr1, storageKey) != (types.Hash{}) {
		t.Fatal("storage should be empty after outer revert")
	}
	if db.GetRefund() != 0 {
		t.Fatalf("refund should be 0, got %d", db.GetRefund())
	}
	if len(db.GetLogs(txHash)) != 0 {
		t.Fatalf("expected 0 logs after outer revert, got %d", len(db.GetLogs(txHash)))
	}
	if db.GetTransientState(addr1, storageKey) != (types.Hash{}) {
		t.Fatal("transient storage should be empty after outer revert")
	}
}

// TestJournalLength verifies that journal entries are accumulated and
// properly truncated on revert.
func TestJournalLength(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x12")

	initial := db.journal.length()
	if initial != 0 {
		t.Fatalf("expected journal length 0, got %d", initial)
	}

	db.CreateAccount(addr)          // +1
	db.AddBalance(addr, big.NewInt(10)) // +1
	db.SetNonce(addr, 1)            // +1

	if db.journal.length() != 3 {
		t.Fatalf("expected journal length 3, got %d", db.journal.length())
	}

	snap := db.Snapshot()
	db.AddBalance(addr, big.NewInt(5)) // +1
	db.SetNonce(addr, 2)           // +1

	if db.journal.length() != 5 {
		t.Fatalf("expected journal length 5, got %d", db.journal.length())
	}

	db.RevertToSnapshot(snap)
	if db.journal.length() != 3 {
		t.Fatalf("expected journal length 3 after revert, got %d", db.journal.length())
	}
}

// TestSnapshotWithMultipleAccounts tests snapshots across multiple accounts
// to verify changes to different accounts are all correctly reverted.
func TestSnapshotWithMultipleAccounts(t *testing.T) {
	db := NewMemoryStateDB()
	addr1 := types.HexToAddress("0x13")
	addr2 := types.HexToAddress("0x14")
	addr3 := types.HexToAddress("0x15")

	db.CreateAccount(addr1)
	db.CreateAccount(addr2)
	db.AddBalance(addr1, big.NewInt(100))
	db.AddBalance(addr2, big.NewInt(200))

	snap := db.Snapshot()

	db.AddBalance(addr1, big.NewInt(50))  // 150
	db.SubBalance(addr2, big.NewInt(100)) // 100
	db.CreateAccount(addr3)
	db.AddBalance(addr3, big.NewInt(300))
	db.SetNonce(addr1, 5)
	db.SetNonce(addr2, 10)

	db.RevertToSnapshot(snap)

	if db.GetBalance(addr1).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("addr1 balance should be 100, got %s", db.GetBalance(addr1))
	}
	if db.GetBalance(addr2).Cmp(big.NewInt(200)) != 0 {
		t.Fatalf("addr2 balance should be 200, got %s", db.GetBalance(addr2))
	}
	if db.Exist(addr3) {
		t.Fatal("addr3 should not exist after revert")
	}
	if db.GetNonce(addr1) != 0 {
		t.Fatalf("addr1 nonce should be 0, got %d", db.GetNonce(addr1))
	}
	if db.GetNonce(addr2) != 0 {
		t.Fatalf("addr2 nonce should be 0, got %d", db.GetNonce(addr2))
	}
}

// TestRevertAfterRevert verifies that multiple sequential reverts to the same
// snapshot work (the second revert is a no-op since the snapshot was consumed).
func TestRevertAfterRevert(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x16")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))

	snap := db.Snapshot()
	db.AddBalance(addr, big.NewInt(50)) // 150

	db.RevertToSnapshot(snap)
	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected 100 after first revert, got %s", db.GetBalance(addr))
	}

	// Second revert to the same snapshot should be a no-op.
	db.RevertToSnapshot(snap)
	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected 100 after second revert, got %s", db.GetBalance(addr))
	}
}

// TestNestedSnapshotStorageOverwrite tests the specific scenario where storage
// is written in an outer scope, then overwritten in an inner scope. The revert
// should restore the outer value correctly.
func TestNestedSnapshotStorageOverwrite(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x17")
	key := types.HexToHash("0x01")
	val1 := types.HexToHash("0x10")
	val2 := types.HexToHash("0x20")
	val3 := types.HexToHash("0x30")

	db.CreateAccount(addr)

	// Set initial storage in outer scope.
	db.SetState(addr, key, val1)

	snapOuter := db.Snapshot()

	// Overwrite in middle scope.
	db.SetState(addr, key, val2)

	snapInner := db.Snapshot()

	// Overwrite again in inner scope.
	db.SetState(addr, key, val3)

	if db.GetState(addr, key) != val3 {
		t.Fatalf("expected val3, got %s", db.GetState(addr, key))
	}

	// Revert inner: should restore val2.
	db.RevertToSnapshot(snapInner)
	if db.GetState(addr, key) != val2 {
		t.Fatalf("expected val2 after inner revert, got %s", db.GetState(addr, key))
	}

	// Revert outer: should restore val1.
	db.RevertToSnapshot(snapOuter)
	if db.GetState(addr, key) != val1 {
		t.Fatalf("expected val1 after outer revert, got %s", db.GetState(addr, key))
	}
}

// TestNestedSnapshotMultipleStorageSlots verifies that reverting correctly
// handles different slots independently.
func TestNestedSnapshotMultipleStorageSlots(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x18")

	db.CreateAccount(addr)

	// Set up multiple slots.
	for i := 0; i < 5; i++ {
		var key, val types.Hash
		key[31] = byte(i)
		val[31] = byte(i * 10)
		db.SetState(addr, key, val)
	}

	snap := db.Snapshot()

	// Modify even slots, leave odd slots alone.
	for i := 0; i < 5; i += 2 {
		var key, val types.Hash
		key[31] = byte(i)
		val[31] = byte(i*10 + 1) // different value
		db.SetState(addr, key, val)
	}

	db.RevertToSnapshot(snap)

	// All slots should be back to original values.
	for i := 0; i < 5; i++ {
		var key types.Hash
		key[31] = byte(i)
		got := db.GetState(addr, key)
		var expected types.Hash
		expected[31] = byte(i * 10)
		if got != expected {
			t.Fatalf("slot %d: expected %s, got %s", i, expected, got)
		}
	}
}

// TestSnapshotDoesNotAffectPreSnapshotState verifies that taking a snapshot
// has no side effects on state.
func TestSnapshotDoesNotAffectPreSnapshotState(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x19")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))
	db.SetNonce(addr, 5)
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xff"))

	// Take snapshot -- should not change any state.
	_ = db.Snapshot()
	_ = db.Snapshot()
	_ = db.Snapshot()

	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatal("taking snapshots should not affect balance")
	}
	if db.GetNonce(addr) != 5 {
		t.Fatal("taking snapshots should not affect nonce")
	}
	if db.GetState(addr, types.HexToHash("0x01")) != types.HexToHash("0xff") {
		t.Fatal("taking snapshots should not affect storage")
	}
}

// TestDeepNesting tests 10 levels of nested snapshots to ensure the journal
// handles arbitrary depth correctly.
func TestDeepNesting(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1a")
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(0))

	const depth = 10
	snaps := make([]int, depth)

	for i := 0; i < depth; i++ {
		snaps[i] = db.Snapshot()
		db.AddBalance(addr, big.NewInt(1))
	}

	// Balance should be depth.
	if db.GetBalance(addr).Cmp(big.NewInt(int64(depth))) != 0 {
		t.Fatalf("expected balance %d, got %s", depth, db.GetBalance(addr))
	}

	// Unwind one level at a time.
	for i := depth - 1; i >= 0; i-- {
		db.RevertToSnapshot(snaps[i])
		expected := big.NewInt(int64(i))
		if db.GetBalance(addr).Cmp(expected) != 0 {
			t.Fatalf("at level %d: expected balance %s, got %s",
				i, expected, db.GetBalance(addr))
		}
	}
}

// TestRevertToOuterAfterInnerRevert tests the pattern where inner snapshot
// is reverted, more changes are made, then outer is reverted.
func TestRevertToOuterAfterInnerRevert(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1b")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))

	snapOuter := db.Snapshot()

	db.AddBalance(addr, big.NewInt(10)) // 110

	snapInner := db.Snapshot()
	db.AddBalance(addr, big.NewInt(20)) // 130

	// Revert inner.
	db.RevertToSnapshot(snapInner)
	// Balance = 110

	// Make more changes after inner revert.
	db.AddBalance(addr, big.NewInt(5)) // 115
	db.SetNonce(addr, 42)

	if db.GetBalance(addr).Cmp(big.NewInt(115)) != 0 {
		t.Fatalf("expected 115, got %s", db.GetBalance(addr))
	}

	// Now revert outer -- should undo everything since outer snapshot.
	db.RevertToSnapshot(snapOuter)
	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected 100 after outer revert, got %s", db.GetBalance(addr))
	}
	if db.GetNonce(addr) != 0 {
		t.Fatalf("expected nonce 0 after outer revert, got %d", db.GetNonce(addr))
	}
}
