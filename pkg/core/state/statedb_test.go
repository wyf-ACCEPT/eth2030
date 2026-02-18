package state

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func TestBalanceOperations(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x01")

	db.CreateAccount(addr)
	if db.GetBalance(addr).Sign() != 0 {
		t.Fatal("new account should have zero balance")
	}

	db.AddBalance(addr, big.NewInt(100))
	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected balance 100, got %s", db.GetBalance(addr))
	}

	db.SubBalance(addr, big.NewInt(30))
	if db.GetBalance(addr).Cmp(big.NewInt(70)) != 0 {
		t.Fatalf("expected balance 70, got %s", db.GetBalance(addr))
	}
}

func TestNonceOperations(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x02")

	db.CreateAccount(addr)
	if db.GetNonce(addr) != 0 {
		t.Fatal("new account should have zero nonce")
	}

	db.SetNonce(addr, 42)
	if db.GetNonce(addr) != 42 {
		t.Fatalf("expected nonce 42, got %d", db.GetNonce(addr))
	}
}

func TestCodeOperations(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x03")

	db.CreateAccount(addr)

	code := []byte{0x60, 0x00, 0x60, 0x00, 0xfd} // PUSH0 PUSH0 REVERT
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

	expectedHash := crypto.Keccak256Hash(code)
	if db.GetCodeHash(addr) != expectedHash {
		t.Fatalf("code hash mismatch: expected %s, got %s", expectedHash, db.GetCodeHash(addr))
	}

	if db.GetCodeSize(addr) != len(code) {
		t.Fatalf("expected code size %d, got %d", len(code), db.GetCodeSize(addr))
	}
}

func TestStorageOperations(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x04")

	db.CreateAccount(addr)

	key := types.HexToHash("0x01")
	val := types.HexToHash("0xff")

	if db.GetState(addr, key) != (types.Hash{}) {
		t.Fatal("storage should be empty initially")
	}

	db.SetState(addr, key, val)
	if db.GetState(addr, key) != val {
		t.Fatalf("expected storage value %s, got %s", val, db.GetState(addr, key))
	}

	// Committed state should still be empty before commit.
	if db.GetCommittedState(addr, key) != (types.Hash{}) {
		t.Fatal("committed state should be empty before commit")
	}

	// After commit, committed state should reflect the value.
	db.Commit()
	if db.GetCommittedState(addr, key) != val {
		t.Fatalf("committed state should be %s after commit, got %s", val, db.GetCommittedState(addr, key))
	}
}

func TestSnapshotRevert(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x05")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(100))
	db.SetNonce(addr, 1)

	snap := db.Snapshot()

	db.AddBalance(addr, big.NewInt(200))
	db.SetNonce(addr, 5)
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	if db.GetBalance(addr).Cmp(big.NewInt(300)) != 0 {
		t.Fatalf("expected balance 300 before revert, got %s", db.GetBalance(addr))
	}

	db.RevertToSnapshot(snap)

	if db.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected balance 100 after revert, got %s", db.GetBalance(addr))
	}
	if db.GetNonce(addr) != 1 {
		t.Fatalf("expected nonce 1 after revert, got %d", db.GetNonce(addr))
	}
	if db.GetState(addr, types.HexToHash("0x01")) != (types.Hash{}) {
		t.Fatal("storage should be reverted")
	}
}

func TestAccessList(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x06")
	slot := types.HexToHash("0x01")

	if db.AddressInAccessList(addr) {
		t.Fatal("address should not be in access list initially")
	}

	db.AddAddressToAccessList(addr)
	if !db.AddressInAccessList(addr) {
		t.Fatal("address should be in access list after add")
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
		t.Fatal("both address and slot should be present after adding slot")
	}

	// Test revert of access list changes.
	snap := db.Snapshot()
	addr2 := types.HexToAddress("0x07")
	db.AddAddressToAccessList(addr2)
	if !db.AddressInAccessList(addr2) {
		t.Fatal("addr2 should be in access list")
	}
	db.RevertToSnapshot(snap)
	if db.AddressInAccessList(addr2) {
		t.Fatal("addr2 should not be in access list after revert")
	}
}

func TestTransientStorage(t *testing.T) {
	db := NewMemoryStateDB()
	addr1 := types.HexToAddress("0x08")
	addr2 := types.HexToAddress("0x09")
	key := types.HexToHash("0x01")
	val := types.HexToHash("0xab")

	if db.GetTransientState(addr1, key) != (types.Hash{}) {
		t.Fatal("transient storage should be empty initially")
	}

	db.SetTransientState(addr1, key, val)
	if db.GetTransientState(addr1, key) != val {
		t.Fatalf("expected transient value %s, got %s", val, db.GetTransientState(addr1, key))
	}

	// Different address should be isolated.
	if db.GetTransientState(addr2, key) != (types.Hash{}) {
		t.Fatal("transient storage should be isolated per address")
	}

	// Test revert.
	snap := db.Snapshot()
	db.SetTransientState(addr1, key, types.HexToHash("0xcc"))
	db.RevertToSnapshot(snap)
	if db.GetTransientState(addr1, key) != val {
		t.Fatalf("transient storage should revert to %s, got %s", val, db.GetTransientState(addr1, key))
	}
}

func TestClearTransientStorage(t *testing.T) {
	db := NewMemoryStateDB()
	addr1 := types.HexToAddress("0x08")
	addr2 := types.HexToAddress("0x09")
	key1 := types.HexToHash("0x01")
	key2 := types.HexToHash("0x02")

	// Set transient storage for multiple addresses and keys.
	db.SetTransientState(addr1, key1, types.HexToHash("0xaa"))
	db.SetTransientState(addr1, key2, types.HexToHash("0xbb"))
	db.SetTransientState(addr2, key1, types.HexToHash("0xcc"))

	// Verify values are set.
	if db.GetTransientState(addr1, key1) != types.HexToHash("0xaa") {
		t.Fatal("expected transient value 0xaa")
	}

	// Clear all transient storage.
	db.ClearTransientStorage()

	// All transient storage should be empty after clearing.
	if db.GetTransientState(addr1, key1) != (types.Hash{}) {
		t.Fatal("transient storage for addr1/key1 should be empty after clear")
	}
	if db.GetTransientState(addr1, key2) != (types.Hash{}) {
		t.Fatal("transient storage for addr1/key2 should be empty after clear")
	}
	if db.GetTransientState(addr2, key1) != (types.Hash{}) {
		t.Fatal("transient storage for addr2/key1 should be empty after clear")
	}

	// Should be able to set new values after clearing.
	db.SetTransientState(addr1, key1, types.HexToHash("0xdd"))
	if db.GetTransientState(addr1, key1) != types.HexToHash("0xdd") {
		t.Fatal("expected new transient value 0xdd after clear and re-set")
	}
}

func TestSelfDestruct(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x0a")

	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(500))

	if db.HasSelfDestructed(addr) {
		t.Fatal("account should not be self-destructed initially")
	}

	db.SelfDestruct(addr)
	if !db.HasSelfDestructed(addr) {
		t.Fatal("account should be self-destructed")
	}
	if db.GetBalance(addr).Sign() != 0 {
		t.Fatal("self-destructed account should have zero balance")
	}

	// Test revert.
	db2 := NewMemoryStateDB()
	addr2 := types.HexToAddress("0x0b")
	db2.CreateAccount(addr2)
	db2.AddBalance(addr2, big.NewInt(500))
	snap := db2.Snapshot()
	db2.SelfDestruct(addr2)
	db2.RevertToSnapshot(snap)
	if db2.HasSelfDestructed(addr2) {
		t.Fatal("self-destruct should be reverted")
	}
	if db2.GetBalance(addr2).Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("balance should be restored after revert, got %s", db2.GetBalance(addr2))
	}
}

func TestLogs(t *testing.T) {
	db := NewMemoryStateDB()
	txHash1 := types.HexToHash("0xaa")
	txHash2 := types.HexToHash("0xbb")

	log1 := &types.Log{Data: []byte{1}}
	log2 := &types.Log{Data: []byte{2}}
	log3 := &types.Log{Data: []byte{3}}

	db.SetTxContext(txHash1, 0)
	db.AddLog(log1)
	db.AddLog(log2)
	db.SetTxContext(txHash2, 1)
	db.AddLog(log3)

	logs1 := db.GetLogs(txHash1)
	if len(logs1) != 2 {
		t.Fatalf("expected 2 logs for tx1, got %d", len(logs1))
	}

	logs2 := db.GetLogs(txHash2)
	if len(logs2) != 1 {
		t.Fatalf("expected 1 log for tx2, got %d", len(logs2))
	}

	// Test revert of log.
	snap := db.Snapshot()
	db.SetTxContext(txHash1, 0)
	db.AddLog(&types.Log{Data: []byte{4}})
	if len(db.GetLogs(txHash1)) != 3 {
		t.Fatal("expected 3 logs before revert")
	}
	db.RevertToSnapshot(snap)
	if len(db.GetLogs(txHash1)) != 2 {
		t.Fatal("expected 2 logs after revert")
	}
}

func TestEmptyAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x0c")

	// Non-existent account is empty.
	if !db.Empty(addr) {
		t.Fatal("non-existent account should be empty")
	}

	db.CreateAccount(addr)
	if !db.Empty(addr) {
		t.Fatal("new account should be empty")
	}

	db.AddBalance(addr, big.NewInt(1))
	if db.Empty(addr) {
		t.Fatal("account with balance should not be empty")
	}

	db.SubBalance(addr, big.NewInt(1))
	if !db.Empty(addr) {
		t.Fatal("account with zero balance, no code, no nonce should be empty")
	}

	db.SetNonce(addr, 1)
	if db.Empty(addr) {
		t.Fatal("account with nonce should not be empty")
	}
}

func TestCommit(t *testing.T) {
	db := NewMemoryStateDB()

	// Empty state should return EmptyRootHash.
	root1, err := db.Commit()
	if err != nil {
		t.Fatalf("commit error: %v", err)
	}
	if root1 != types.EmptyRootHash {
		t.Fatalf("empty state should return EmptyRootHash, got %s", root1)
	}

	addr := types.HexToAddress("0x0d")
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(1000))

	root2, err := db.Commit()
	if err != nil {
		t.Fatalf("commit error: %v", err)
	}
	if root2 == types.EmptyRootHash {
		t.Fatal("non-empty state should not return EmptyRootHash")
	}

	// Committing again with same state should yield the same root.
	root3, err := db.Commit()
	if err != nil {
		t.Fatalf("commit error: %v", err)
	}
	if root2 != root3 {
		t.Fatalf("repeated commit should yield same root: %s vs %s", root2, root3)
	}

	// Changing state should change the root.
	db.AddBalance(addr, big.NewInt(1))
	root4, err := db.Commit()
	if err != nil {
		t.Fatalf("commit error: %v", err)
	}
	if root4 == root3 {
		t.Fatal("different state should produce different root")
	}
}

func TestRefund(t *testing.T) {
	db := NewMemoryStateDB()

	db.AddRefund(100)
	if db.GetRefund() != 100 {
		t.Fatalf("expected refund 100, got %d", db.GetRefund())
	}

	db.SubRefund(30)
	if db.GetRefund() != 70 {
		t.Fatalf("expected refund 70, got %d", db.GetRefund())
	}

	snap := db.Snapshot()
	db.AddRefund(50)
	db.RevertToSnapshot(snap)
	if db.GetRefund() != 70 {
		t.Fatalf("expected refund 70 after revert, got %d", db.GetRefund())
	}
}

// Ensure MemoryStateDB satisfies the StateDB interface.
func TestInterfaceCompliance(t *testing.T) {
	var _ StateDB = (*MemoryStateDB)(nil)
}
