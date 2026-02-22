package witness

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// extSetupState creates a MemoryStateDB with a richer set of accounts for
// extended testing, complementing setupState in collector_test.go.
func extSetupState() *state.MemoryStateDB {
	sdb := state.NewMemoryStateDB()

	// EOA with large balance.
	addrA := types.HexToAddress("0x1111")
	sdb.CreateAccount(addrA)
	sdb.AddBalance(addrA, big.NewInt(10_000_000))
	sdb.SetNonce(addrA, 42)

	// Contract with code and multiple storage slots.
	addrB := types.HexToAddress("0x2222")
	sdb.CreateAccount(addrB)
	sdb.AddBalance(addrB, big.NewInt(999))
	sdb.SetNonce(addrB, 3)
	sdb.SetCode(addrB, []byte{0x60, 0x01, 0x60, 0x02, 0x01, 0xf3}) // PUSH1 1 PUSH1 2 ADD RETURN
	for i := 0; i < 5; i++ {
		key := types.BytesToHash([]byte{byte(i + 1)})
		val := types.BytesToHash([]byte{byte(0xA0 + i)})
		sdb.SetState(addrB, key, val)
	}

	// Zero-balance account.
	addrC := types.HexToAddress("0x3333")
	sdb.CreateAccount(addrC)

	return sdb
}

// ---------- Account Witness Recording ----------

func TestExtCollector_MultipleAccountReadsRecordOnce(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x1111")

	// Read the same account's fields multiple times.
	_ = c.GetBalance(addr)
	_ = c.GetNonce(addr)
	_ = c.GetBalance(addr)
	_ = c.GetCodeHash(addr)

	// The witness should contain exactly one entry for the account.
	aw, ok := w.State[addr]
	if !ok {
		t.Fatal("account not recorded in witness")
	}
	if aw.Balance.Cmp(big.NewInt(10_000_000)) != 0 {
		t.Errorf("witness balance = %s, want 10000000", aw.Balance)
	}
	if aw.Nonce != 42 {
		t.Errorf("witness nonce = %d, want 42", aw.Nonce)
	}
	if !aw.Exists {
		t.Error("witness should mark account as existing")
	}
}

func TestExtCollector_ZeroBalanceAccountRecorded(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x3333")

	_ = c.GetBalance(addr)

	aw := w.State[addr]
	if aw == nil {
		t.Fatal("zero-balance account should be recorded")
	}
	if aw.Balance.Sign() != 0 {
		t.Errorf("expected zero balance, got %s", aw.Balance)
	}
	if !aw.Exists {
		t.Error("account should exist")
	}
}

// ---------- Storage Witness Recording ----------

func TestExtCollector_MultipleStorageSlotsRecorded(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x2222")

	// Read 3 distinct storage slots.
	for i := 1; i <= 3; i++ {
		key := types.BytesToHash([]byte{byte(i)})
		val := c.GetState(addr, key)
		expected := types.BytesToHash([]byte{byte(0xA0 + i - 1)})
		if val != expected {
			t.Errorf("slot %d: got %s, want %s", i, val.Hex(), expected.Hex())
		}
	}

	aw := w.State[addr]
	if aw == nil {
		t.Fatal("account not in witness")
	}
	if len(aw.Storage) != 3 {
		t.Errorf("expected 3 storage entries, got %d", len(aw.Storage))
	}
}

func TestExtCollector_GetCommittedStateRecordsPreState(t *testing.T) {
	sdb := extSetupState()
	// Commit to move dirty storage into committed storage.
	if _, err := sdb.Commit(); err != nil {
		t.Fatal(err)
	}
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x2222")
	key := types.BytesToHash([]byte{1})

	val := c.GetCommittedState(addr, key)
	expected := types.BytesToHash([]byte{0xA0})
	if val != expected {
		t.Errorf("GetCommittedState = %s, want %s", val.Hex(), expected.Hex())
	}

	aw := w.State[addr]
	if stored, ok := aw.Storage[key]; !ok || stored != expected {
		t.Errorf("witness storage[key] = %s, want %s", stored.Hex(), expected.Hex())
	}
}

func TestExtCollector_StorageReadThenWritePreservesPreState(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x2222")
	key := types.BytesToHash([]byte{1})
	preVal := types.BytesToHash([]byte{0xA0})

	// Read first, then overwrite.
	_ = c.GetState(addr, key)
	newVal := types.HexToHash("0xDEAD")
	c.SetState(addr, key, newVal)

	aw := w.State[addr]
	if aw.Storage[key] != preVal {
		t.Errorf("witness should hold pre-state value %s, got %s",
			preVal.Hex(), aw.Storage[key].Hex())
	}
}

func TestExtCollector_WriteWithoutPriorRead_CapturesPreState(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x2222")
	key := types.BytesToHash([]byte{2})
	preVal := types.BytesToHash([]byte{0xA1})

	// Write directly without any prior read.
	c.SetState(addr, key, types.HexToHash("0xBEEF"))

	aw := w.State[addr]
	if aw.Storage[key] != preVal {
		t.Errorf("witness should capture pre-state value %s, got %s",
			preVal.Hex(), aw.Storage[key].Hex())
	}
}

func TestExtCollector_UnsetStorageSlotReturnsZero(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x2222")
	key := types.HexToHash("0xFF") // slot not set in extSetupState

	val := c.GetState(addr, key)
	if val != (types.Hash{}) {
		t.Errorf("expected zero hash for unset slot, got %s", val.Hex())
	}

	aw := w.State[addr]
	if stored, ok := aw.Storage[key]; !ok {
		t.Error("unset slot should still be recorded in witness")
	} else if stored != (types.Hash{}) {
		t.Errorf("witness value for unset slot should be zero, got %s", stored.Hex())
	}
}

// ---------- Code Witness Recording ----------

func TestExtCollector_CodeRecordedInWitness(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x2222")

	code := c.GetCode(addr)
	if len(code) == 0 {
		t.Fatal("expected non-empty code")
	}

	codeHash := c.GetCodeHash(addr)
	if codeHash.IsZero() {
		t.Fatal("code hash should not be zero")
	}

	if _, ok := w.Codes[codeHash]; !ok {
		t.Fatal("code not in witness codes map")
	}
	if len(w.Codes[codeHash]) != len(code) {
		t.Errorf("witness code length = %d, want %d", len(w.Codes[codeHash]), len(code))
	}
}

func TestExtCollector_EOACodeNotRecorded(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x1111") // EOA

	code := c.GetCode(addr)
	if len(code) != 0 {
		t.Errorf("expected empty code for EOA, got %d bytes", len(code))
	}
	if len(w.Codes) != 0 {
		t.Error("no code should be recorded for EOA")
	}
}

func TestExtCollector_GetCodeSize(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0x2222")
	size := c.GetCodeSize(addr)
	if size != 6 {
		t.Errorf("expected code size 6, got %d", size)
	}
	// The account should be recorded even via GetCodeSize.
	if _, ok := w.State[addr]; !ok {
		t.Error("account should be recorded from GetCodeSize call")
	}
}

// ---------- Witness Size Tracking ----------

func TestExtCollector_WitnessSizeGrowsWithAccess(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)

	addr := types.HexToAddress("0x2222")
	initialSize := len(w.State)

	_ = c.GetBalance(addr)
	afterAccount := len(w.State)
	if afterAccount <= initialSize {
		t.Error("witness state count should grow after account access")
	}

	for i := 1; i <= 3; i++ {
		key := types.BytesToHash([]byte{byte(i)})
		_ = c.GetState(addr, key)
	}
	aw := w.State[addr]
	if len(aw.Storage) != 3 {
		t.Errorf("expected 3 storage slots, got %d", len(aw.Storage))
	}
}

// ---------- Witness Accessor Method ----------

func TestExtCollector_WitnessReturnsCorrectRef(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)

	if c.Witness() != w {
		t.Error("Witness() should return the same BlockWitness reference")
	}
}

// ---------- Write Operations Record Pre-State ----------

func TestExtCollector_AddBalanceRecordsPreState(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x1111")

	c.AddBalance(addr, big.NewInt(500))

	aw := w.State[addr]
	if aw == nil {
		t.Fatal("account not in witness after AddBalance")
	}
	// Witness should show pre-state balance (10_000_000), not post-state.
	if aw.Balance.Cmp(big.NewInt(10_000_000)) != 0 {
		t.Errorf("witness balance = %s, want 10000000", aw.Balance)
	}
}

func TestExtCollector_SubBalanceRecordsPreState(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x1111")

	c.SubBalance(addr, big.NewInt(100))

	aw := w.State[addr]
	if aw == nil {
		t.Fatal("account not in witness after SubBalance")
	}
	if aw.Balance.Cmp(big.NewInt(10_000_000)) != 0 {
		t.Errorf("witness balance = %s, want 10000000 (pre-state)", aw.Balance)
	}
}

func TestExtCollector_SetNonceRecordsPreState(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x1111")

	c.SetNonce(addr, 100)

	aw := w.State[addr]
	if aw == nil {
		t.Fatal("account not in witness after SetNonce")
	}
	if aw.Nonce != 42 {
		t.Errorf("witness nonce = %d, want 42 (pre-state)", aw.Nonce)
	}
}

func TestExtCollector_SetCodeRecordsPreState(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x2222")

	newCode := []byte{0xFF, 0xFE}
	c.SetCode(addr, newCode)

	aw := w.State[addr]
	if aw == nil {
		t.Fatal("account not in witness after SetCode")
	}
	// Pre-state nonce should be 3.
	if aw.Nonce != 3 {
		t.Errorf("witness nonce = %d, want 3", aw.Nonce)
	}
}

// ---------- Snapshot and Revert ----------

func TestExtCollector_SnapshotRevertPreservesWitness(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x2222")

	snap := c.Snapshot()

	// Access account and storage.
	_ = c.GetBalance(addr)
	key := types.BytesToHash([]byte{1})
	_ = c.GetState(addr, key)

	// Revert the snapshot.
	c.RevertToSnapshot(snap)

	// Witness data should still be present.
	aw, ok := w.State[addr]
	if !ok {
		t.Fatal("witness should retain account data after revert")
	}
	if _, ok := aw.Storage[key]; !ok {
		t.Fatal("witness should retain storage data after revert")
	}
}

// ---------- Table-Driven: Non-Existent Accounts ----------

func TestExtCollector_NonExistentAccountOperations(t *testing.T) {
	tests := []struct {
		name string
		op   func(c *WitnessCollector, addr types.Address)
	}{
		{"GetBalance", func(c *WitnessCollector, addr types.Address) { c.GetBalance(addr) }},
		{"GetNonce", func(c *WitnessCollector, addr types.Address) { c.GetNonce(addr) }},
		{"GetCodeHash", func(c *WitnessCollector, addr types.Address) { c.GetCodeHash(addr) }},
		{"Exist", func(c *WitnessCollector, addr types.Address) { c.Exist(addr) }},
		{"Empty", func(c *WitnessCollector, addr types.Address) { c.Empty(addr) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sdb := extSetupState()
			w := NewBlockWitness()
			c := NewWitnessCollector(sdb, w)
			addr := types.HexToAddress("0xDEAD")

			tc.op(c, addr)

			aw := w.State[addr]
			if aw == nil {
				t.Fatal("non-existent account should be in witness")
			}
			if aw.Exists {
				t.Fatal("account should not be marked as existing")
			}
			if aw.Balance.Sign() != 0 {
				t.Error("balance should be zero for non-existent account")
			}
			if aw.Nonce != 0 {
				t.Error("nonce should be zero for non-existent account")
			}
		})
	}
}

// ---------- HasSelfDestructed Delegation ----------

func TestExtCollector_HasSelfDestructedDelegation(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x1111")

	if c.HasSelfDestructed(addr) {
		t.Error("account should not be self-destructed initially")
	}
}

// ---------- ClearTransientStorage Delegation ----------

func TestExtCollector_ClearTransientStorage(t *testing.T) {
	sdb := extSetupState()
	w := NewBlockWitness()
	c := NewWitnessCollector(sdb, w)
	addr := types.HexToAddress("0x1111")
	key := types.HexToHash("0x01")

	c.SetTransientState(addr, key, types.HexToHash("0xAA"))
	c.ClearTransientStorage()

	val := c.GetTransientState(addr, key)
	if val != (types.Hash{}) {
		t.Errorf("transient state should be cleared, got %s", val.Hex())
	}
}
