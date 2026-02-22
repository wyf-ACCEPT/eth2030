package state

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// buildTestWitness creates a StatelessWitness with one account that has
// known balance, nonce, storage, and code. It computes the correct state
// root by building a MemoryStateDB with the same data and committing.
func buildTestWitness(t *testing.T) (*StatelessWitness, types.Address, types.Hash) {
	t.Helper()

	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	storageKey := types.HexToHash("0x01")
	storageVal := types.HexToHash("0x1234")
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3} // PUSH0 PUSH0 RETURN

	// Build a MemoryStateDB to compute the canonical state root.
	mem := NewMemoryStateDB()
	mem.CreateAccount(addr)
	mem.AddBalance(addr, big.NewInt(5000))
	mem.SetNonce(addr, 7)
	mem.SetCode(addr, code)
	mem.SetState(addr, storageKey, storageVal)
	root, err := mem.Commit()
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	codeHash := mem.GetCodeHash(addr)

	witness := NewStatelessWitness(root)
	witness.AddAccount(addr, &WitnessAccount{
		Nonce:    7,
		Balance:  big.NewInt(5000),
		CodeHash: codeHash,
		Storage:  map[types.Hash]types.Hash{storageKey: storageVal},
		Exists:   true,
	})
	witness.AddCode(codeHash, code)
	return witness, addr, root
}

func TestStatelessStateDB_CreateFromWitness(t *testing.T) {
	witness, addr, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	if sdb == nil {
		t.Fatal("NewStatelessStateDB returned nil")
	}
	if !sdb.Exist(addr) {
		t.Fatal("account from witness should exist")
	}
}

func TestStatelessStateDB_ReadAccountData(t *testing.T) {
	witness, addr, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	// Balance
	bal := sdb.GetBalance(addr)
	if bal.Cmp(big.NewInt(5000)) != 0 {
		t.Fatalf("balance: want 5000, got %s", bal)
	}

	// Nonce
	if nonce := sdb.GetNonce(addr); nonce != 7 {
		t.Fatalf("nonce: want 7, got %d", nonce)
	}

	// Code
	code := sdb.GetCode(addr)
	if len(code) != 5 {
		t.Fatalf("code length: want 5, got %d", len(code))
	}
	if code[0] != 0x60 {
		t.Fatalf("code[0]: want 0x60, got 0x%02x", code[0])
	}

	// CodeSize
	if sdb.GetCodeSize(addr) != 5 {
		t.Fatalf("code size: want 5, got %d", sdb.GetCodeSize(addr))
	}

	// CodeHash should be non-empty.
	if sdb.GetCodeHash(addr) == (types.Hash{}) {
		t.Fatal("code hash should be non-zero")
	}
	if sdb.GetCodeHash(addr) == types.EmptyCodeHash {
		t.Fatal("code hash should not be empty code hash for a contract")
	}
}

func TestStatelessStateDB_ReadStorage(t *testing.T) {
	witness, addr, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	storageKey := types.HexToHash("0x01")
	storageVal := types.HexToHash("0x1234")

	val := sdb.GetState(addr, storageKey)
	if val != storageVal {
		t.Fatalf("storage: want %s, got %s", storageVal.Hex(), val.Hex())
	}

	// Reading a key not in witness should return zero.
	missingKey := types.HexToHash("0xff")
	if v := sdb.GetState(addr, missingKey); v != (types.Hash{}) {
		t.Fatalf("missing storage: want zero, got %s", v.Hex())
	}

	// CommittedState should also read from witness.
	committed := sdb.GetCommittedState(addr, storageKey)
	if committed != storageVal {
		t.Fatalf("committed storage: want %s, got %s", storageVal.Hex(), committed.Hex())
	}
}

func TestStatelessStateDB_AccumulateChanges(t *testing.T) {
	witness, addr, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	// Modify balance.
	sdb.AddBalance(addr, big.NewInt(100))
	if bal := sdb.GetBalance(addr); bal.Cmp(big.NewInt(5100)) != 0 {
		t.Fatalf("balance after add: want 5100, got %s", bal)
	}

	sdb.SubBalance(addr, big.NewInt(50))
	if bal := sdb.GetBalance(addr); bal.Cmp(big.NewInt(5050)) != 0 {
		t.Fatalf("balance after sub: want 5050, got %s", bal)
	}

	// Modify nonce.
	sdb.SetNonce(addr, 8)
	if sdb.GetNonce(addr) != 8 {
		t.Fatalf("nonce: want 8, got %d", sdb.GetNonce(addr))
	}

	// Modify storage.
	newKey := types.HexToHash("0x02")
	newVal := types.HexToHash("0xdead")
	sdb.SetState(addr, newKey, newVal)
	if v := sdb.GetState(addr, newKey); v != newVal {
		t.Fatalf("new storage: want %s, got %s", newVal.Hex(), v.Hex())
	}

	// Dirty storage should overlay committed.
	existingKey := types.HexToHash("0x01")
	updatedVal := types.HexToHash("0x9999")
	sdb.SetState(addr, existingKey, updatedVal)
	if v := sdb.GetState(addr, existingKey); v != updatedVal {
		t.Fatalf("updated storage: want %s, got %s", updatedVal.Hex(), v.Hex())
	}
	// Committed state should remain original.
	if v := sdb.GetCommittedState(addr, existingKey); v != types.HexToHash("0x1234") {
		t.Fatalf("committed should be original: want 0x1234, got %s", v.Hex())
	}
}

func TestStatelessStateDB_SnapshotRevert(t *testing.T) {
	witness, addr, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	snap := sdb.Snapshot()

	sdb.AddBalance(addr, big.NewInt(9999))
	sdb.SetNonce(addr, 100)

	if bal := sdb.GetBalance(addr); bal.Cmp(big.NewInt(14999)) != 0 {
		t.Fatalf("balance before revert: want 14999, got %s", bal)
	}

	sdb.RevertToSnapshot(snap)

	if bal := sdb.GetBalance(addr); bal.Cmp(big.NewInt(5000)) != 0 {
		t.Fatalf("balance after revert: want 5000, got %s", bal)
	}
	if sdb.GetNonce(addr) != 7 {
		t.Fatalf("nonce after revert: want 7, got %d", sdb.GetNonce(addr))
	}
}

func TestStatelessWitness_Verify(t *testing.T) {
	witness, _, root := buildTestWitness(t)

	// Verification against the correct root should succeed.
	if err := witness.Verify(root); err != nil {
		t.Fatalf("verify correct root: %v", err)
	}

	// Verification against a wrong root should fail.
	badRoot := types.HexToHash("0xdeadbeef")
	if err := witness.Verify(badRoot); err == nil {
		t.Fatal("verify bad root: expected error, got nil")
	}
}

func TestStatelessStateDB_NonExistentAccount(t *testing.T) {
	witness := NewStatelessWitness(types.EmptyRootHash)
	sdb := NewStatelessStateDB(witness)

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	if sdb.Exist(addr) {
		t.Fatal("non-existent account should not exist")
	}
	if !sdb.Empty(addr) {
		t.Fatal("non-existent account should be empty")
	}
	if sdb.GetBalance(addr).Sign() != 0 {
		t.Fatal("non-existent account should have zero balance")
	}
	if sdb.GetNonce(addr) != 0 {
		t.Fatal("non-existent account should have zero nonce")
	}
}

func TestStatelessStateDB_SelfDestruct(t *testing.T) {
	witness, addr, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	if sdb.HasSelfDestructed(addr) {
		t.Fatal("should not be self-destructed initially")
	}

	sdb.SelfDestruct(addr)

	if !sdb.HasSelfDestructed(addr) {
		t.Fatal("should be self-destructed after SelfDestruct")
	}
	if sdb.GetBalance(addr).Sign() != 0 {
		t.Fatal("balance should be zero after self-destruct")
	}
}

func TestStatelessStateDB_SetCode(t *testing.T) {
	witness, addr, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	newCode := []byte{0x00, 0x01, 0x02}
	sdb.SetCode(addr, newCode)

	if len(sdb.GetCode(addr)) != 3 {
		t.Fatalf("code length: want 3, got %d", len(sdb.GetCode(addr)))
	}
	if sdb.GetCodeSize(addr) != 3 {
		t.Fatalf("code size: want 3, got %d", sdb.GetCodeSize(addr))
	}
}

func TestStatelessStateDB_Logs(t *testing.T) {
	witness, _, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	txHash := types.HexToHash("0xabcd")
	sdb.SetTxContext(txHash, 0)

	sdb.AddLog(&types.Log{
		Address: types.HexToAddress("0x01"),
		Data:    []byte{0x42},
	})

	logs := sdb.GetLogs(txHash)
	if len(logs) != 1 {
		t.Fatalf("logs: want 1, got %d", len(logs))
	}
	if logs[0].Data[0] != 0x42 {
		t.Fatalf("log data: want 0x42, got 0x%02x", logs[0].Data[0])
	}
}

func TestStatelessStateDB_Refund(t *testing.T) {
	witness, _, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	sdb.AddRefund(100)
	if sdb.GetRefund() != 100 {
		t.Fatalf("refund: want 100, got %d", sdb.GetRefund())
	}
	sdb.SubRefund(30)
	if sdb.GetRefund() != 70 {
		t.Fatalf("refund: want 70, got %d", sdb.GetRefund())
	}
}

func TestStatelessStateDB_AccessList(t *testing.T) {
	witness, _, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	addr := types.HexToAddress("0x01")
	slot := types.HexToHash("0x02")

	if sdb.AddressInAccessList(addr) {
		t.Fatal("address should not be in access list initially")
	}

	sdb.AddAddressToAccessList(addr)
	if !sdb.AddressInAccessList(addr) {
		t.Fatal("address should be in access list after add")
	}

	sdb.AddSlotToAccessList(addr, slot)
	addrOk, slotOk := sdb.SlotInAccessList(addr, slot)
	if !addrOk || !slotOk {
		t.Fatalf("slot should be in access list: addr=%v slot=%v", addrOk, slotOk)
	}
}

func TestStatelessStateDB_TransientStorage(t *testing.T) {
	witness, _, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	addr := types.HexToAddress("0x01")
	key := types.HexToHash("0x02")
	val := types.HexToHash("0x03")

	if sdb.GetTransientState(addr, key) != (types.Hash{}) {
		t.Fatal("transient storage should be empty initially")
	}

	sdb.SetTransientState(addr, key, val)
	if sdb.GetTransientState(addr, key) != val {
		t.Fatal("transient storage should return set value")
	}

	sdb.ClearTransientStorage()
	if sdb.GetTransientState(addr, key) != (types.Hash{}) {
		t.Fatal("transient storage should be empty after clear")
	}
}

func TestStatelessStateDB_CreateNewAccount(t *testing.T) {
	witness, _, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	newAddr := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	sdb.CreateAccount(newAddr)
	if !sdb.Exist(newAddr) {
		t.Fatal("newly created account should exist")
	}
	sdb.AddBalance(newAddr, big.NewInt(42))
	if bal := sdb.GetBalance(newAddr); bal.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("new account balance: want 42, got %s", bal)
	}
}

func TestStatelessStateDB_Commit(t *testing.T) {
	witness, addr, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	// Modify state.
	sdb.AddBalance(addr, big.NewInt(100))
	sdb.SetState(addr, types.HexToHash("0x05"), types.HexToHash("0xff"))

	root, err := sdb.Commit()
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if root == (types.Hash{}) {
		t.Fatal("committed root should not be zero")
	}

	// After commit, dirty storage should be flushed.
	committed := sdb.GetCommittedState(addr, types.HexToHash("0x05"))
	if committed != types.HexToHash("0xff") {
		t.Fatalf("committed storage after commit: want 0xff, got %s", committed.Hex())
	}
}

func TestStatelessWitness_VerifyEmpty(t *testing.T) {
	witness := NewStatelessWitness(types.EmptyRootHash)
	if err := witness.Verify(types.EmptyRootHash); err != nil {
		t.Fatalf("verify empty witness: %v", err)
	}
	if err := witness.Verify(types.HexToHash("0x01")); err == nil {
		t.Fatal("verify empty witness against non-empty root should fail")
	}
}

func TestStatelessStateDB_DirtyAccounts(t *testing.T) {
	witness, addr, _ := buildTestWitness(t)
	sdb := NewStatelessStateDB(witness)

	// Touch the witness account.
	sdb.GetBalance(addr)

	dirty := sdb.DirtyAccounts()
	if len(dirty) != 1 {
		t.Fatalf("dirty accounts: want 1, got %d", len(dirty))
	}
	if dirty[0] != addr {
		t.Fatalf("dirty account: want %s, got %s", addr.Hex(), dirty[0].Hex())
	}
}
