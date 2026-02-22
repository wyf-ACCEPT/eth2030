package witness

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// setupState creates a MemoryStateDB with some pre-populated accounts for testing.
func setupState() *state.MemoryStateDB {
	sdb := state.NewMemoryStateDB()

	// Account A: an EOA with balance and nonce.
	addrA := types.HexToAddress("0xaaaa")
	sdb.CreateAccount(addrA)
	sdb.AddBalance(addrA, big.NewInt(1_000_000))
	sdb.SetNonce(addrA, 5)

	// Account B: a contract with code and storage.
	addrB := types.HexToAddress("0xbbbb")
	sdb.CreateAccount(addrB)
	sdb.AddBalance(addrB, big.NewInt(500))
	sdb.SetNonce(addrB, 1)
	sdb.SetCode(addrB, []byte{0x60, 0x00, 0x60, 0x00, 0xf3}) // PUSH0 PUSH0 RETURN
	sdb.SetState(addrB, types.HexToHash("0x01"), types.HexToHash("0xff"))
	sdb.SetState(addrB, types.HexToHash("0x02"), types.HexToHash("0xee"))

	return sdb
}

func TestCollectorRecordsBalance(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0xaaaa")
	bal := collector.GetBalance(addr)
	if bal.Cmp(big.NewInt(1_000_000)) != 0 {
		t.Fatalf("expected balance 1000000, got %s", bal)
	}

	aw, ok := w.State[addr]
	if !ok {
		t.Fatal("account not recorded in witness")
	}
	if aw.Balance.Cmp(big.NewInt(1_000_000)) != 0 {
		t.Fatalf("witness balance = %s, want 1000000", aw.Balance)
	}
	if aw.Nonce != 5 {
		t.Fatalf("witness nonce = %d, want 5", aw.Nonce)
	}
	if !aw.Exists {
		t.Fatal("witness should mark account as existing")
	}
}

func TestCollectorRecordsNonce(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0xaaaa")
	nonce := collector.GetNonce(addr)
	if nonce != 5 {
		t.Fatalf("expected nonce 5, got %d", nonce)
	}

	aw := w.State[addr]
	if aw == nil {
		t.Fatal("account not recorded in witness")
	}
	if aw.Nonce != 5 {
		t.Fatalf("witness nonce = %d, want 5", aw.Nonce)
	}
}

func TestCollectorRecordsCode(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0xbbbb")
	code := collector.GetCode(addr)
	if len(code) != 5 {
		t.Fatalf("expected 5 bytes of code, got %d", len(code))
	}

	codeHash := collector.GetCodeHash(addr)
	if codeHash == (types.Hash{}) {
		t.Fatal("code hash should not be zero")
	}

	// Check that the code is in the witness.
	if _, ok := w.Codes[codeHash]; !ok {
		t.Fatal("code not recorded in witness")
	}
}

func TestCollectorRecordsStorage(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0xbbbb")
	slot := types.HexToHash("0x01")

	val := collector.GetState(addr, slot)
	if val != types.HexToHash("0xff") {
		t.Fatalf("expected storage value 0xff, got %s", val.Hex())
	}

	aw := w.State[addr]
	if aw == nil {
		t.Fatal("account not recorded in witness")
	}
	if storedVal, ok := aw.Storage[slot]; !ok {
		t.Fatal("storage slot not recorded in witness")
	} else if storedVal != types.HexToHash("0xff") {
		t.Fatalf("witness storage = %s, want 0xff", storedVal.Hex())
	}
}

func TestCollectorRecordsPreStateOnWrite(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0xbbbb")
	slot := types.HexToHash("0x01")

	// Write without a prior read -- collector should still capture pre-state.
	collector.SetState(addr, slot, types.HexToHash("0xdd"))

	aw := w.State[addr]
	if aw == nil {
		t.Fatal("account not recorded in witness")
	}
	if storedVal, ok := aw.Storage[slot]; !ok {
		t.Fatal("storage slot not recorded in witness on write")
	} else if storedVal != types.HexToHash("0xff") {
		t.Fatalf("witness should record PRE-state value 0xff, got %s", storedVal.Hex())
	}
}

func TestCollectorPreservesFirstReadOnly(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0xbbbb")
	slot := types.HexToHash("0x01")

	// First read captures the pre-state.
	collector.GetState(addr, slot)

	// Write a new value (changes inner state).
	collector.SetState(addr, slot, types.HexToHash("0xdd"))

	// Second read should return the new value from inner state.
	val := collector.GetState(addr, slot)
	if val != types.HexToHash("0xdd") {
		t.Fatalf("expected new value 0xdd from inner state, got %s", val.Hex())
	}

	// But witness should still hold the original pre-state value.
	aw := w.State[addr]
	if aw.Storage[slot] != types.HexToHash("0xff") {
		t.Fatalf("witness should still hold pre-state 0xff, got %s", aw.Storage[slot].Hex())
	}
}

func TestCollectorExistAndEmpty(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	// Existing account.
	if !collector.Exist(types.HexToAddress("0xaaaa")) {
		t.Fatal("expected account 0xaaaa to exist")
	}
	if collector.Empty(types.HexToAddress("0xaaaa")) {
		t.Fatal("expected account 0xaaaa to not be empty")
	}

	// Non-existing account.
	noAddr := types.HexToAddress("0x9999")
	if collector.Exist(noAddr) {
		t.Fatal("expected account 0x9999 to not exist")
	}

	// The non-existing address should also be recorded in the witness.
	aw := w.State[noAddr]
	if aw == nil {
		t.Fatal("non-existing account should still be in witness")
	}
	if aw.Exists {
		t.Fatal("witness should mark 0x9999 as non-existing")
	}
}

func TestCollectorSnapshotRevert(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0xaaaa")

	snap := collector.Snapshot()
	collector.AddBalance(addr, big.NewInt(100))

	// After revert, the inner balance should be restored.
	collector.RevertToSnapshot(snap)
	bal := collector.GetBalance(addr)
	if bal.Cmp(big.NewInt(1_000_000)) != 0 {
		t.Fatalf("expected balance 1000000 after revert, got %s", bal)
	}

	// But witness data should still be present (witness is not reverted).
	if _, ok := w.State[addr]; !ok {
		t.Fatal("witness data should persist after snapshot revert")
	}
}

func TestCollectorAccessList(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")

	collector.AddAddressToAccessList(addr)
	if !collector.AddressInAccessList(addr) {
		t.Fatal("expected address in access list")
	}

	collector.AddSlotToAccessList(addr, slot)
	addrOk, slotOk := collector.SlotInAccessList(addr, slot)
	if !addrOk || !slotOk {
		t.Fatal("expected address and slot in access list")
	}
}

func TestCollectorTransientStorage(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0xaaaa")
	key := types.HexToHash("0x01")

	collector.SetTransientState(addr, key, types.HexToHash("0xab"))
	val := collector.GetTransientState(addr, key)
	if val != types.HexToHash("0xab") {
		t.Fatalf("transient storage = %s, want 0xab", val.Hex())
	}
}

func TestCollectorRefund(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	collector.AddRefund(100)
	if collector.GetRefund() != 100 {
		t.Fatalf("expected refund 100, got %d", collector.GetRefund())
	}
	collector.SubRefund(30)
	if collector.GetRefund() != 70 {
		t.Fatalf("expected refund 70, got %d", collector.GetRefund())
	}
}

func TestCollectorLogs(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	log := &types.Log{
		Address: types.HexToAddress("0xbbbb"),
		Topics:  []types.Hash{types.HexToHash("0x01")},
		Data:    []byte{0x01},
	}
	collector.AddLog(log)
	// Logs are delegated to inner -- no assertion on witness.
}

func TestCollectorCreateAccount(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0xcccc")

	// Pre-state: account does not exist.
	collector.CreateAccount(addr)

	aw := w.State[addr]
	if aw == nil {
		t.Fatal("newly created account should be in witness")
	}
	// The witness records the pre-state: the account did NOT exist.
	if aw.Exists {
		t.Fatal("witness should record that account did not exist before creation")
	}

	// But inner state should now have the account.
	if !collector.Exist(addr) {
		t.Fatal("account should exist in inner state after creation")
	}
}

func TestCollectorSelfDestruct(t *testing.T) {
	sdb := setupState()
	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0xbbbb")
	collector.SelfDestruct(addr)

	if !collector.HasSelfDestructed(addr) {
		t.Fatal("expected account to be self-destructed")
	}
	// Witness should have recorded pre-state.
	if _, ok := w.State[addr]; !ok {
		t.Fatal("self-destructed account should be in witness")
	}
}
