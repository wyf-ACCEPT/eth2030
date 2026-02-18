package witness

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- Initialization ---

func TestNewWitnessStateDB_EmptyWitness(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)
	if sdb == nil {
		t.Fatal("NewWitnessStateDB returned nil")
	}
	if len(sdb.accounts) != 0 {
		t.Fatalf("expected 0 accounts, got %d", len(sdb.accounts))
	}
}

func TestNewWitnessStateDB_PopulatesFromWitness(t *testing.T) {
	w := NewBlockWitness()

	addr1 := types.HexToAddress("0x1111")
	addr2 := types.HexToAddress("0x2222")
	codeHash := types.HexToHash("0xdeadbeef")
	code := []byte{0x60, 0x00, 0xf3}
	slot := types.HexToHash("0x01")
	slotVal := types.HexToHash("0xaa")

	w.State[addr1] = &AccountWitness{
		Balance:  big.NewInt(1000),
		Nonce:    5,
		CodeHash: codeHash,
		Storage:  map[types.Hash]types.Hash{slot: slotVal},
		Exists:   true,
	}
	w.Codes[codeHash] = code

	w.State[addr2] = &AccountWitness{
		Balance:  big.NewInt(200),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)

	if len(sdb.accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(sdb.accounts))
	}

	// Verify addr1 state was copied.
	if sdb.GetBalance(addr1).Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("addr1 balance = %s, want 1000", sdb.GetBalance(addr1))
	}
	if sdb.GetNonce(addr1) != 5 {
		t.Fatalf("addr1 nonce = %d, want 5", sdb.GetNonce(addr1))
	}
	if sdb.GetCodeHash(addr1) != codeHash {
		t.Fatalf("addr1 code hash mismatch")
	}
	gotCode := sdb.GetCode(addr1)
	if len(gotCode) != len(code) {
		t.Fatalf("addr1 code length = %d, want %d", len(gotCode), len(code))
	}
	if sdb.GetState(addr1, slot) != slotVal {
		t.Fatalf("addr1 storage[slot] = %s, want %s", sdb.GetState(addr1, slot).Hex(), slotVal.Hex())
	}
}

func TestNewWitnessStateDB_CopiesBalanceIndependently(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	origBal := big.NewInt(500)
	w.State[addr] = &AccountWitness{
		Balance:  origBal,
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	sdb.AddBalance(addr, big.NewInt(100))

	// Mutating sdb should not change the witness.
	if origBal.Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("witness balance was mutated: %s", origBal)
	}
}

func TestNewWitnessStateDB_CopiesStorageIndependently(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  map[types.Hash]types.Hash{slot: types.HexToHash("0x10")},
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	sdb.SetState(addr, slot, types.HexToHash("0x20"))

	// Witness pre-state should be unchanged.
	if w.State[addr].Storage[slot] != types.HexToHash("0x10") {
		t.Fatal("witness storage was mutated")
	}
}

// --- Account operations ---

func TestCreateAccount_NewAddress(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xcccc")
	sdb.CreateAccount(addr)

	if !sdb.Exist(addr) {
		t.Fatal("expected account to exist after CreateAccount")
	}
	if !sdb.Empty(addr) {
		t.Fatal("expected newly created account to be empty")
	}
	if sdb.GetBalance(addr).Sign() != 0 {
		t.Fatal("expected zero balance")
	}
	if sdb.GetNonce(addr) != 0 {
		t.Fatal("expected zero nonce")
	}
	if sdb.GetCodeHash(addr) != types.EmptyCodeHash {
		t.Fatalf("expected EmptyCodeHash, got %s", sdb.GetCodeHash(addr).Hex())
	}
}

func TestCreateAccount_OverwritesExisting(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(1000),
		Nonce:    10,
		CodeHash: types.HexToHash("0xbeef"),
		Storage:  map[types.Hash]types.Hash{types.HexToHash("0x01"): types.HexToHash("0xff")},
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	sdb.CreateAccount(addr)

	// After CreateAccount, the account is reset.
	if sdb.GetBalance(addr).Sign() != 0 {
		t.Fatalf("balance after CreateAccount = %s, want 0", sdb.GetBalance(addr))
	}
	if sdb.GetNonce(addr) != 0 {
		t.Fatalf("nonce after CreateAccount = %d, want 0", sdb.GetNonce(addr))
	}
	if sdb.GetCodeHash(addr) != types.EmptyCodeHash {
		t.Fatal("code hash should be EmptyCodeHash after CreateAccount")
	}
	// Storage is cleared.
	if sdb.GetState(addr, types.HexToHash("0x01")) != (types.Hash{}) {
		t.Fatal("storage should be cleared after CreateAccount")
	}
}

func TestGetBalance_ExistingAccount(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(42),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	bal := sdb.GetBalance(addr)
	if bal.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("balance = %s, want 42", bal)
	}

	// GetBalance should return a copy, not a reference.
	bal.Add(bal, big.NewInt(100))
	if sdb.GetBalance(addr).Cmp(big.NewInt(42)) != 0 {
		t.Fatal("GetBalance returned a reference instead of a copy")
	}
}

func TestAddBalance_ExistingAccount(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(100),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	sdb.AddBalance(addr, big.NewInt(50))
	if sdb.GetBalance(addr).Cmp(big.NewInt(150)) != 0 {
		t.Fatalf("balance = %s, want 150", sdb.GetBalance(addr))
	}
}

func TestAddBalance_NonExistentAccountCreatesIt(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xdddd")
	sdb.AddBalance(addr, big.NewInt(42))

	if !sdb.Exist(addr) {
		t.Fatal("expected account to exist after AddBalance")
	}
	if sdb.GetBalance(addr).Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("balance = %s, want 42", sdb.GetBalance(addr))
	}
}

func TestSubBalance(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(1000),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	sdb.SubBalance(addr, big.NewInt(300))
	if sdb.GetBalance(addr).Cmp(big.NewInt(700)) != 0 {
		t.Fatalf("balance = %s, want 700", sdb.GetBalance(addr))
	}
}

func TestBalanceTransfer(t *testing.T) {
	w := NewBlockWitness()
	from := types.HexToAddress("0xaaaa")
	to := types.HexToAddress("0xbbbb")
	w.State[from] = &AccountWitness{
		Balance:  big.NewInt(1000),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}
	w.State[to] = &AccountWitness{
		Balance:  big.NewInt(100),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	sdb.SubBalance(from, big.NewInt(200))
	sdb.AddBalance(to, big.NewInt(200))

	if sdb.GetBalance(from).Cmp(big.NewInt(800)) != 0 {
		t.Fatalf("from balance = %s, want 800", sdb.GetBalance(from))
	}
	if sdb.GetBalance(to).Cmp(big.NewInt(300)) != 0 {
		t.Fatalf("to balance = %s, want 300", sdb.GetBalance(to))
	}
}

func TestGetNonce_SetNonce(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    7,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	if sdb.GetNonce(addr) != 7 {
		t.Fatalf("nonce = %d, want 7", sdb.GetNonce(addr))
	}
	sdb.SetNonce(addr, 8)
	if sdb.GetNonce(addr) != 8 {
		t.Fatalf("nonce after set = %d, want 8", sdb.GetNonce(addr))
	}
}

func TestSetNonce_NonExistentAccount(t *testing.T) {
	// SetNonce on a non-existent account creates an implicit account entry,
	// but does not set exists=true. GetNonce checks exists, so it returns 0.
	// To read the nonce back, the account must exist (via CreateAccount or AddBalance).
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xeeee")
	sdb.SetNonce(addr, 42)
	// GetNonce returns 0 because the implicit account has exists=false.
	if sdb.GetNonce(addr) != 0 {
		t.Fatalf("nonce = %d, want 0 (account does not 'exist')", sdb.GetNonce(addr))
	}

	// If we explicitly create the account first, then SetNonce works as expected.
	addr2 := types.HexToAddress("0xffff")
	sdb.CreateAccount(addr2)
	sdb.SetNonce(addr2, 99)
	if sdb.GetNonce(addr2) != 99 {
		t.Fatalf("nonce = %d, want 99", sdb.GetNonce(addr2))
	}
}

// --- Code operations ---

func TestGetCode_WithWitnessCode(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xbbbb")
	codeHash := types.HexToHash("0xdeadbeef")
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: codeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}
	w.Codes[codeHash] = code

	sdb := NewWitnessStateDB(w)

	gotCode := sdb.GetCode(addr)
	if len(gotCode) != len(code) {
		t.Fatalf("code length = %d, want %d", len(gotCode), len(code))
	}
	for i := range code {
		if gotCode[i] != code[i] {
			t.Fatalf("code[%d] = %x, want %x", i, gotCode[i], code[i])
		}
	}
}

func TestGetCode_NoCode(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	if code := sdb.GetCode(addr); len(code) != 0 {
		t.Fatalf("expected empty code, got %d bytes", len(code))
	}
}

func TestGetCode_NonExistentAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xdead")
	if code := sdb.GetCode(addr); code != nil {
		t.Fatalf("expected nil code for non-existent account, got %d bytes", len(code))
	}
}

func TestSetCode(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xcccc")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	newCode := []byte{0x01, 0x02, 0x03}
	sdb.SetCode(addr, newCode)

	if sdb.GetCodeSize(addr) != 3 {
		t.Fatalf("code size = %d, want 3", sdb.GetCodeSize(addr))
	}

	// Verify SetCode copies the input.
	newCode[0] = 0xff
	got := sdb.GetCode(addr)
	if got[0] != 0x01 {
		t.Fatal("SetCode did not copy input: mutation affected stored code")
	}
}

func TestSetCode_NonExistentAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xeeee")
	sdb.SetCode(addr, []byte{0xaa, 0xbb})
	if sdb.GetCodeSize(addr) != 2 {
		t.Fatalf("code size = %d, want 2", sdb.GetCodeSize(addr))
	}
}

func TestGetCodeHash(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	codeHash := types.HexToHash("0xabcdef")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: codeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	if sdb.GetCodeHash(addr) != codeHash {
		t.Fatalf("code hash = %s, want %s", sdb.GetCodeHash(addr).Hex(), codeHash.Hex())
	}
}

func TestGetCodeHash_NonExistentAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xdead")
	if sdb.GetCodeHash(addr) != (types.Hash{}) {
		t.Fatal("expected zero hash for non-existent account")
	}
}

func TestGetCodeHash_ExistsButFalse(t *testing.T) {
	// Account in witness but Exists=false returns zero hash.
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.HexToHash("0xbeef"),
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   false,
	}

	sdb := NewWitnessStateDB(w)
	if sdb.GetCodeHash(addr) != (types.Hash{}) {
		t.Fatal("expected zero hash for account with Exists=false")
	}
}

func TestGetCodeSize(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xbbbb")
	codeHash := types.HexToHash("0xdeadbeef")
	code := []byte{0x60, 0x00, 0xf3}
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: codeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}
	w.Codes[codeHash] = code

	sdb := NewWitnessStateDB(w)
	if sdb.GetCodeSize(addr) != 3 {
		t.Fatalf("code size = %d, want 3", sdb.GetCodeSize(addr))
	}
}

func TestGetCodeSize_NonExistentAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xdead")
	if sdb.GetCodeSize(addr) != 0 {
		t.Fatal("expected code size 0 for non-existent account")
	}
}

// --- Storage operations ---

func TestGetState_FromWitnessPreState(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xbbbb")
	slot := types.HexToHash("0x01")
	val := types.HexToHash("0xff")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  map[types.Hash]types.Hash{slot: val},
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	if sdb.GetState(addr, slot) != val {
		t.Fatalf("storage = %s, want %s", sdb.GetState(addr, slot).Hex(), val.Hex())
	}
}

func TestGetState_NonExistentSlot(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xbbbb")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	if sdb.GetState(addr, types.HexToHash("0x99")) != (types.Hash{}) {
		t.Fatal("expected zero hash for non-existent slot")
	}
}

func TestGetState_NonExistentAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xdead")
	if sdb.GetState(addr, types.HexToHash("0x01")) != (types.Hash{}) {
		t.Fatal("expected zero hash for non-existent account")
	}
}

func TestSetState_OverwriteExisting(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xbbbb")
	slot := types.HexToHash("0x01")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  map[types.Hash]types.Hash{slot: types.HexToHash("0xff")},
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	sdb.SetState(addr, slot, types.HexToHash("0xdd"))
	if sdb.GetState(addr, slot) != types.HexToHash("0xdd") {
		t.Fatalf("storage = %s, want 0xdd", sdb.GetState(addr, slot).Hex())
	}
}

func TestSetState_NewSlot(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xbbbb")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	newSlot := types.HexToHash("0x55")
	sdb.SetState(addr, newSlot, types.HexToHash("0xcc"))
	if sdb.GetState(addr, newSlot) != types.HexToHash("0xcc") {
		t.Fatal("expected newly set storage value")
	}
}

func TestSetState_NonExistentAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xdead")
	slot := types.HexToHash("0x01")
	sdb.SetState(addr, slot, types.HexToHash("0xab"))
	if sdb.GetState(addr, slot) != types.HexToHash("0xab") {
		t.Fatal("expected storage to be set on auto-created account")
	}
}

func TestGetCommittedState_ReturnsWitnessPreState(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xbbbb")
	slot := types.HexToHash("0x01")
	preVal := types.HexToHash("0xff")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  map[types.Hash]types.Hash{slot: preVal},
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)

	// Modify current state.
	sdb.SetState(addr, slot, types.HexToHash("0xdd"))

	// GetCommittedState still returns pre-state.
	committed := sdb.GetCommittedState(addr, slot)
	if committed != preVal {
		t.Fatalf("committed = %s, want %s", committed.Hex(), preVal.Hex())
	}
}

func TestGetCommittedState_NonExistentSlot(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xbbbb")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	if sdb.GetCommittedState(addr, types.HexToHash("0x99")) != (types.Hash{}) {
		t.Fatal("expected zero for non-existent committed slot")
	}
}

func TestGetCommittedState_NonExistentAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xdead")
	if sdb.GetCommittedState(addr, types.HexToHash("0x01")) != (types.Hash{}) {
		t.Fatal("expected zero for non-existent account committed state")
	}
}

// --- Transient storage (EIP-1153) ---

func TestTransientStorage_GetSetBasic(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	key := types.HexToHash("0x01")

	// Initially zero.
	if sdb.GetTransientState(addr, key) != (types.Hash{}) {
		t.Fatal("expected zero transient state initially")
	}

	sdb.SetTransientState(addr, key, types.HexToHash("0xab"))
	if sdb.GetTransientState(addr, key) != types.HexToHash("0xab") {
		t.Fatal("expected transient state 0xab")
	}
}

func TestTransientStorage_MultipleAddresses(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr1 := types.HexToAddress("0xaaaa")
	addr2 := types.HexToAddress("0xbbbb")
	key := types.HexToHash("0x01")

	sdb.SetTransientState(addr1, key, types.HexToHash("0x11"))
	sdb.SetTransientState(addr2, key, types.HexToHash("0x22"))

	if sdb.GetTransientState(addr1, key) != types.HexToHash("0x11") {
		t.Fatal("addr1 transient state mismatch")
	}
	if sdb.GetTransientState(addr2, key) != types.HexToHash("0x22") {
		t.Fatal("addr2 transient state mismatch")
	}
}

func TestTransientStorage_MultipleKeys(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	key1 := types.HexToHash("0x01")
	key2 := types.HexToHash("0x02")

	sdb.SetTransientState(addr, key1, types.HexToHash("0xaa"))
	sdb.SetTransientState(addr, key2, types.HexToHash("0xbb"))

	if sdb.GetTransientState(addr, key1) != types.HexToHash("0xaa") {
		t.Fatal("key1 transient mismatch")
	}
	if sdb.GetTransientState(addr, key2) != types.HexToHash("0xbb") {
		t.Fatal("key2 transient mismatch")
	}
}

func TestTransientStorage_Overwrite(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	key := types.HexToHash("0x01")

	sdb.SetTransientState(addr, key, types.HexToHash("0x11"))
	sdb.SetTransientState(addr, key, types.HexToHash("0x22"))

	if sdb.GetTransientState(addr, key) != types.HexToHash("0x22") {
		t.Fatal("expected overwritten transient value")
	}
}

func TestClearTransientStorage(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	key := types.HexToHash("0x01")
	sdb.SetTransientState(addr, key, types.HexToHash("0xab"))

	sdb.ClearTransientStorage()

	if sdb.GetTransientState(addr, key) != (types.Hash{}) {
		t.Fatal("expected zero transient state after clear")
	}
}

func TestTransientStorage_IndependentOfPersistentStorage(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  map[types.Hash]types.Hash{slot: types.HexToHash("0xff")},
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)

	// Set transient with same key as persistent.
	sdb.SetTransientState(addr, slot, types.HexToHash("0x01"))

	// Persistent remains unchanged.
	if sdb.GetState(addr, slot) != types.HexToHash("0xff") {
		t.Fatal("persistent storage was affected by transient set")
	}
	if sdb.GetTransientState(addr, slot) != types.HexToHash("0x01") {
		t.Fatal("transient storage mismatch")
	}
}

// --- Self-destruct ---

func TestSelfDestruct(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xbbbb")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(500),
		Nonce:    1,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	sdb.SelfDestruct(addr)

	if !sdb.HasSelfDestructed(addr) {
		t.Fatal("expected account to be self-destructed")
	}
	if sdb.GetBalance(addr).Sign() != 0 {
		t.Fatal("expected zero balance after self-destruct")
	}
}

func TestSelfDestruct_NonExistentAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xdead")
	// SelfDestruct on non-existent account should not panic.
	sdb.SelfDestruct(addr)
	if sdb.HasSelfDestructed(addr) {
		t.Fatal("non-existent account should not be marked as self-destructed")
	}
}

func TestHasSelfDestructed_DefaultFalse(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	if sdb.HasSelfDestructed(addr) {
		t.Fatal("account should not be self-destructed by default")
	}
}

func TestHasSelfDestructed_NonExistentAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	if sdb.HasSelfDestructed(types.HexToAddress("0xdead")) {
		t.Fatal("non-existent account should not be self-destructed")
	}
}

// --- Account existence ---

func TestExist(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	if !sdb.Exist(addr) {
		t.Fatal("expected account to exist")
	}
}

func TestExist_NotInWitness(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	if sdb.Exist(types.HexToAddress("0xdead")) {
		t.Fatal("expected account to not exist")
	}
}

func TestExist_InWitnessButNotExists(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   false,
	}

	sdb := NewWitnessStateDB(w)
	if sdb.Exist(addr) {
		t.Fatal("expected account with Exists=false to not exist")
	}
}

func TestEmpty_TrueForEmptyAccount(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	if !sdb.Empty(addr) {
		t.Fatal("expected empty account to be empty")
	}
}

func TestEmpty_FalseWithBalance(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(1),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	if sdb.Empty(addr) {
		t.Fatal("account with balance should not be empty")
	}
}

func TestEmpty_FalseWithNonce(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    1,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	if sdb.Empty(addr) {
		t.Fatal("account with nonce should not be empty")
	}
}

func TestEmpty_FalseWithCodeHash(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.HexToHash("0xdeadbeef"),
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	if sdb.Empty(addr) {
		t.Fatal("account with non-empty code hash should not be empty")
	}
}

func TestEmpty_TrueForNonExistentAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	if !sdb.Empty(types.HexToAddress("0xdead")) {
		t.Fatal("non-existent account should be empty")
	}
}

// --- Snapshot / Revert ---

func TestSnapshot_ReturnsIncrementingIDs(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	id0 := sdb.Snapshot()
	id1 := sdb.Snapshot()
	id2 := sdb.Snapshot()

	if id0 != 0 || id1 != 1 || id2 != 2 {
		t.Fatalf("snapshot IDs = %d, %d, %d; want 0, 1, 2", id0, id1, id2)
	}
}

func TestRevertToSnapshot_RevertsBalance(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(1000),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	snap := sdb.Snapshot()
	sdb.AddBalance(addr, big.NewInt(500))

	if sdb.GetBalance(addr).Cmp(big.NewInt(1500)) != 0 {
		t.Fatalf("balance before revert = %s, want 1500", sdb.GetBalance(addr))
	}

	sdb.RevertToSnapshot(snap)
	if sdb.GetBalance(addr).Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("balance after revert = %s, want 1000", sdb.GetBalance(addr))
	}
}

func TestRevertToSnapshot_RevertsNonce(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    5,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	snap := sdb.Snapshot()
	sdb.SetNonce(addr, 10)

	sdb.RevertToSnapshot(snap)
	if sdb.GetNonce(addr) != 5 {
		t.Fatalf("nonce after revert = %d, want 5", sdb.GetNonce(addr))
	}
}

func TestRevertToSnapshot_RevertsStorage(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  map[types.Hash]types.Hash{slot: types.HexToHash("0xff")},
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	snap := sdb.Snapshot()
	sdb.SetState(addr, slot, types.HexToHash("0xdd"))

	sdb.RevertToSnapshot(snap)
	if sdb.GetState(addr, slot) != types.HexToHash("0xff") {
		t.Fatalf("storage after revert = %s, want 0xff", sdb.GetState(addr, slot).Hex())
	}
}

func TestRevertToSnapshot_RevertsRefund(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	sdb.AddRefund(100)
	snap := sdb.Snapshot()
	sdb.AddRefund(200)

	if sdb.GetRefund() != 300 {
		t.Fatalf("refund before revert = %d, want 300", sdb.GetRefund())
	}

	sdb.RevertToSnapshot(snap)
	if sdb.GetRefund() != 100 {
		t.Fatalf("refund after revert = %d, want 100", sdb.GetRefund())
	}
}

func TestRevertToSnapshot_RevertsLogs(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	sdb.AddLog(&types.Log{Address: types.HexToAddress("0x01")})
	snap := sdb.Snapshot()
	sdb.AddLog(&types.Log{Address: types.HexToAddress("0x02")})
	sdb.AddLog(&types.Log{Address: types.HexToAddress("0x03")})

	sdb.RevertToSnapshot(snap)
	if len(sdb.logs) != 1 {
		t.Fatalf("logs after revert = %d, want 1", len(sdb.logs))
	}
}

func TestRevertToSnapshot_RevertsAccessList(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr1 := types.HexToAddress("0xaaaa")
	addr2 := types.HexToAddress("0xbbbb")
	slot := types.HexToHash("0x01")

	sdb.AddAddressToAccessList(addr1)
	snap := sdb.Snapshot()
	sdb.AddAddressToAccessList(addr2)
	sdb.AddSlotToAccessList(addr2, slot)

	sdb.RevertToSnapshot(snap)

	if !sdb.AddressInAccessList(addr1) {
		t.Fatal("addr1 should still be in access list after revert")
	}
	if sdb.AddressInAccessList(addr2) {
		t.Fatal("addr2 should not be in access list after revert")
	}
}

func TestRevertToSnapshot_RevertsTransientStorage(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	key := types.HexToHash("0x01")

	sdb.SetTransientState(addr, key, types.HexToHash("0x11"))
	snap := sdb.Snapshot()
	sdb.SetTransientState(addr, key, types.HexToHash("0x22"))

	sdb.RevertToSnapshot(snap)
	if sdb.GetTransientState(addr, key) != types.HexToHash("0x11") {
		t.Fatalf("transient after revert = %s, want 0x11", sdb.GetTransientState(addr, key).Hex())
	}
}

func TestRevertToSnapshot_RevertsCreateAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xcccc")
	snap := sdb.Snapshot()
	sdb.CreateAccount(addr)

	if !sdb.Exist(addr) {
		t.Fatal("expected account to exist before revert")
	}

	sdb.RevertToSnapshot(snap)
	if sdb.Exist(addr) {
		t.Fatal("expected account to not exist after revert")
	}
}

func TestRevertToSnapshot_RevertsSelfDestruct(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xbbbb")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(500),
		Nonce:    1,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	snap := sdb.Snapshot()
	sdb.SelfDestruct(addr)

	if !sdb.HasSelfDestructed(addr) {
		t.Fatal("expected self-destructed before revert")
	}

	sdb.RevertToSnapshot(snap)
	if sdb.HasSelfDestructed(addr) {
		t.Fatal("expected not self-destructed after revert")
	}
	if sdb.GetBalance(addr).Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("balance after revert = %s, want 500", sdb.GetBalance(addr))
	}
}

func TestRevertToSnapshot_InvalidIDIsNoOp(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(100),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	sdb.AddBalance(addr, big.NewInt(50))

	// Revert to an invalid snapshot ID should be a no-op.
	sdb.RevertToSnapshot(999)

	if sdb.GetBalance(addr).Cmp(big.NewInt(150)) != 0 {
		t.Fatalf("balance = %s, want 150 (revert should be no-op)", sdb.GetBalance(addr))
	}
}

func TestRevertToSnapshot_InvalidatesNewerSnapshots(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(100),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	snap0 := sdb.Snapshot()
	sdb.AddBalance(addr, big.NewInt(10))

	snap1 := sdb.Snapshot()
	sdb.AddBalance(addr, big.NewInt(20))

	// Revert to snap0.
	sdb.RevertToSnapshot(snap0)
	if sdb.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("balance = %s, want 100", sdb.GetBalance(addr))
	}

	// snap1 should be invalidated; reverting to it should be a no-op.
	sdb.AddBalance(addr, big.NewInt(50))
	sdb.RevertToSnapshot(snap1)
	// Since snap1 was deleted, this is a no-op.
	if sdb.GetBalance(addr).Cmp(big.NewInt(150)) != 0 {
		t.Fatalf("balance = %s, want 150 (snap1 should be invalidated)", sdb.GetBalance(addr))
	}
}

func TestNestedSnapshots(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(100),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)

	// Snapshot 0: balance = 100
	snap0 := sdb.Snapshot()
	sdb.AddBalance(addr, big.NewInt(10)) // balance = 110

	// Snapshot 1: balance = 110
	snap1 := sdb.Snapshot()
	sdb.AddBalance(addr, big.NewInt(20)) // balance = 130

	// Snapshot 2: balance = 130
	snap2 := sdb.Snapshot()
	sdb.AddBalance(addr, big.NewInt(30)) // balance = 160

	if sdb.GetBalance(addr).Cmp(big.NewInt(160)) != 0 {
		t.Fatalf("balance = %s, want 160", sdb.GetBalance(addr))
	}

	// Revert to snap2: balance = 130
	sdb.RevertToSnapshot(snap2)
	if sdb.GetBalance(addr).Cmp(big.NewInt(130)) != 0 {
		t.Fatalf("balance after revert to snap2 = %s, want 130", sdb.GetBalance(addr))
	}

	// Revert to snap1: balance = 110
	sdb.RevertToSnapshot(snap1)
	if sdb.GetBalance(addr).Cmp(big.NewInt(110)) != 0 {
		t.Fatalf("balance after revert to snap1 = %s, want 110", sdb.GetBalance(addr))
	}

	// Revert to snap0: balance = 100
	sdb.RevertToSnapshot(snap0)
	if sdb.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("balance after revert to snap0 = %s, want 100", sdb.GetBalance(addr))
	}
}

// --- Logs ---

func TestAddLog(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	log1 := &types.Log{
		Address: types.HexToAddress("0xbbbb"),
		Topics:  []types.Hash{types.HexToHash("0x01")},
		Data:    []byte{0x42},
	}
	log2 := &types.Log{
		Address: types.HexToAddress("0xcccc"),
		Topics:  []types.Hash{types.HexToHash("0x02"), types.HexToHash("0x03")},
		Data:    []byte{0x99, 0xaa},
	}
	sdb.AddLog(log1)
	sdb.AddLog(log2)

	if len(sdb.logs) != 2 {
		t.Fatalf("logs count = %d, want 2", len(sdb.logs))
	}
	if sdb.logs[0].Address != log1.Address {
		t.Fatal("first log address mismatch")
	}
	if sdb.logs[1].Address != log2.Address {
		t.Fatal("second log address mismatch")
	}
}

func TestAddLog_RevertTruncates(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	sdb.AddLog(&types.Log{Address: types.HexToAddress("0x01")})
	snap := sdb.Snapshot()
	sdb.AddLog(&types.Log{Address: types.HexToAddress("0x02")})
	sdb.AddLog(&types.Log{Address: types.HexToAddress("0x03")})

	if len(sdb.logs) != 3 {
		t.Fatalf("logs before revert = %d, want 3", len(sdb.logs))
	}

	sdb.RevertToSnapshot(snap)
	if len(sdb.logs) != 1 {
		t.Fatalf("logs after revert = %d, want 1", len(sdb.logs))
	}
}

// --- Refund counter ---

func TestAddRefund(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	sdb.AddRefund(100)
	if sdb.GetRefund() != 100 {
		t.Fatalf("refund = %d, want 100", sdb.GetRefund())
	}
	sdb.AddRefund(200)
	if sdb.GetRefund() != 300 {
		t.Fatalf("refund = %d, want 300", sdb.GetRefund())
	}
}

func TestSubRefund(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	sdb.AddRefund(200)
	sdb.SubRefund(50)
	if sdb.GetRefund() != 150 {
		t.Fatalf("refund = %d, want 150", sdb.GetRefund())
	}
}

func TestGetRefund_InitiallyZero(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	if sdb.GetRefund() != 0 {
		t.Fatalf("initial refund = %d, want 0", sdb.GetRefund())
	}
}

// --- Access list (EIP-2929) ---

func TestAccessList_AddressOnly(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	if sdb.AddressInAccessList(addr) {
		t.Fatal("address should not be in access list initially")
	}

	sdb.AddAddressToAccessList(addr)
	if !sdb.AddressInAccessList(addr) {
		t.Fatal("address should be in access list after add")
	}

	// Verify slot is not automatically added.
	_, slotOk := sdb.SlotInAccessList(addr, types.HexToHash("0x01"))
	if slotOk {
		t.Fatal("slot should not be in access list when only address was added")
	}
}

func TestAccessList_AddressAndSlot(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")

	sdb.AddSlotToAccessList(addr, slot)
	addrOk, slotOk := sdb.SlotInAccessList(addr, slot)
	if !addrOk {
		t.Fatal("address should be in access list after adding slot")
	}
	if !slotOk {
		t.Fatal("slot should be in access list after add")
	}
}

func TestAccessList_AddSlotAlsoAddsAddress(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")

	sdb.AddSlotToAccessList(addr, slot)
	if !sdb.AddressInAccessList(addr) {
		t.Fatal("address should be in access list after adding slot")
	}
}

func TestAccessList_MultipleSlots(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	slot1 := types.HexToHash("0x01")
	slot2 := types.HexToHash("0x02")

	sdb.AddSlotToAccessList(addr, slot1)
	sdb.AddSlotToAccessList(addr, slot2)

	_, s1ok := sdb.SlotInAccessList(addr, slot1)
	_, s2ok := sdb.SlotInAccessList(addr, slot2)
	if !s1ok || !s2ok {
		t.Fatal("both slots should be in access list")
	}
}

func TestAccessList_DifferentAddresses(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr1 := types.HexToAddress("0xaaaa")
	addr2 := types.HexToAddress("0xbbbb")
	slot := types.HexToHash("0x01")

	sdb.AddSlotToAccessList(addr1, slot)

	if sdb.AddressInAccessList(addr2) {
		t.Fatal("addr2 should not be in access list")
	}
	_, slotOk := sdb.SlotInAccessList(addr2, slot)
	if slotOk {
		t.Fatal("slot for addr2 should not be in access list")
	}
}

func TestAccessList_AddAddressThenSlot(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")

	// First add address alone, then add a slot for the same address.
	sdb.AddAddressToAccessList(addr)
	sdb.AddSlotToAccessList(addr, slot)

	addrOk, slotOk := sdb.SlotInAccessList(addr, slot)
	if !addrOk || !slotOk {
		t.Fatal("expected address and slot in access list")
	}
}

func TestAccessList_DuplicateAddressIsIdempotent(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	sdb.AddAddressToAccessList(addr)
	sdb.AddAddressToAccessList(addr)

	if !sdb.AddressInAccessList(addr) {
		t.Fatal("address should still be in access list")
	}
}

func TestAccessList_DuplicateSlotIsIdempotent(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")
	sdb.AddSlotToAccessList(addr, slot)
	sdb.AddSlotToAccessList(addr, slot)

	_, slotOk := sdb.SlotInAccessList(addr, slot)
	if !slotOk {
		t.Fatal("slot should still be in access list")
	}
}

func TestAccessList_SlotInAccessList_NonExistentSlot(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	sdb.AddAddressToAccessList(addr)

	addrOk, slotOk := sdb.SlotInAccessList(addr, types.HexToHash("0x99"))
	if !addrOk {
		t.Fatal("address should be present")
	}
	if slotOk {
		t.Fatal("non-existent slot should not be present")
	}
}

func TestAccessList_SlotInAccessList_NonExistentAddress(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addrOk, slotOk := sdb.SlotInAccessList(types.HexToAddress("0xdead"), types.HexToHash("0x01"))
	if addrOk || slotOk {
		t.Fatal("neither address nor slot should be present")
	}
}

// --- Edge cases ---

func TestNonExistentAccount_AllOperations(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xdead")

	// All reads should return zero/nil/false.
	if sdb.Exist(addr) {
		t.Fatal("Exist should return false")
	}
	if !sdb.Empty(addr) {
		t.Fatal("Empty should return true")
	}
	if sdb.GetBalance(addr).Sign() != 0 {
		t.Fatal("GetBalance should return 0")
	}
	if sdb.GetNonce(addr) != 0 {
		t.Fatal("GetNonce should return 0")
	}
	if sdb.GetCode(addr) != nil {
		t.Fatal("GetCode should return nil")
	}
	if sdb.GetCodeHash(addr) != (types.Hash{}) {
		t.Fatal("GetCodeHash should return zero hash")
	}
	if sdb.GetCodeSize(addr) != 0 {
		t.Fatal("GetCodeSize should return 0")
	}
	if sdb.GetState(addr, types.HexToHash("0x01")) != (types.Hash{}) {
		t.Fatal("GetState should return zero hash")
	}
	if sdb.GetCommittedState(addr, types.HexToHash("0x01")) != (types.Hash{}) {
		t.Fatal("GetCommittedState should return zero hash")
	}
	if sdb.HasSelfDestructed(addr) {
		t.Fatal("HasSelfDestructed should return false")
	}
	if sdb.GetTransientState(addr, types.HexToHash("0x01")) != (types.Hash{}) {
		t.Fatal("GetTransientState should return zero hash")
	}
}

func TestAccountNotInWitness_SetOperationsCreateImplicitly(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	// SetState on non-existent account creates an implicit account entry.
	// GetState works because it does not check exists.
	addr := types.HexToAddress("0xeeee")
	sdb.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xab"))
	if sdb.GetState(addr, types.HexToHash("0x01")) != types.HexToHash("0xab") {
		t.Fatal("SetState should work on non-existent account")
	}

	// SetNonce on non-existent account creates an implicit entry,
	// but GetNonce checks exists (which is false), so returns 0.
	// This is by design: reads check existence, writes create implicitly.
	addr2 := types.HexToAddress("0xffff")
	sdb.SetNonce(addr2, 99)
	if sdb.GetNonce(addr2) != 0 {
		t.Fatal("GetNonce should return 0 for implicit (non-existing) account")
	}

	// SetCode on non-existent account creates an implicit entry.
	// GetCodeSize works because it does not check exists.
	addr3 := types.HexToAddress("0x1234")
	sdb.SetCode(addr3, []byte{0xfe})
	if sdb.GetCodeSize(addr3) != 1 {
		t.Fatal("SetCode should work on non-existent account")
	}
}

func TestWitnessAccountExistsFalse_Behavior(t *testing.T) {
	// When witness has an account with Exists=false, it represents an account
	// that was accessed but did not exist at the pre-state.
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(0),
		Nonce:    0,
		CodeHash: types.Hash{},
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   false,
	}

	sdb := NewWitnessStateDB(w)

	if sdb.Exist(addr) {
		t.Fatal("expected Exist to return false for Exists=false account")
	}
	if !sdb.Empty(addr) {
		t.Fatal("expected Empty to return true for Exists=false account")
	}
	// GetBalance returns 0 because exists is false.
	if sdb.GetBalance(addr).Sign() != 0 {
		t.Fatal("expected zero balance for Exists=false account")
	}
	// GetNonce returns 0 because exists is false.
	if sdb.GetNonce(addr) != 0 {
		t.Fatal("expected zero nonce for Exists=false account")
	}
	// GetCodeHash returns zero hash because exists is false.
	if sdb.GetCodeHash(addr) != (types.Hash{}) {
		t.Fatal("expected zero code hash for Exists=false account")
	}
}

func TestMultipleAccountsInWitness(t *testing.T) {
	w := NewBlockWitness()

	// Set up multiple accounts in the witness.
	addrs := make([]types.Address, 5)
	for i := 0; i < 5; i++ {
		addrs[i] = types.BytesToAddress([]byte{byte(i + 1)})
		w.State[addrs[i]] = &AccountWitness{
			Balance:  big.NewInt(int64(i * 100)),
			Nonce:    uint64(i),
			CodeHash: types.EmptyCodeHash,
			Storage:  make(map[types.Hash]types.Hash),
			Exists:   true,
		}
	}

	sdb := NewWitnessStateDB(w)

	for i, addr := range addrs {
		if sdb.GetBalance(addr).Int64() != int64(i*100) {
			t.Fatalf("addr[%d] balance = %s, want %d", i, sdb.GetBalance(addr), i*100)
		}
		if sdb.GetNonce(addr) != uint64(i) {
			t.Fatalf("addr[%d] nonce = %d, want %d", i, sdb.GetNonce(addr), i)
		}
	}
}

func TestSnapshotWithMultipleAccounts(t *testing.T) {
	w := NewBlockWitness()
	addr1 := types.HexToAddress("0xaaaa")
	addr2 := types.HexToAddress("0xbbbb")
	w.State[addr1] = &AccountWitness{
		Balance:  big.NewInt(100),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}
	w.State[addr2] = &AccountWitness{
		Balance:  big.NewInt(200),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)
	snap := sdb.Snapshot()

	sdb.AddBalance(addr1, big.NewInt(50))
	sdb.SubBalance(addr2, big.NewInt(50))

	sdb.RevertToSnapshot(snap)

	if sdb.GetBalance(addr1).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("addr1 balance = %s, want 100", sdb.GetBalance(addr1))
	}
	if sdb.GetBalance(addr2).Cmp(big.NewInt(200)) != 0 {
		t.Fatalf("addr2 balance = %s, want 200", sdb.GetBalance(addr2))
	}
}

func TestSnapshotDoesNotAffectWitnessPreState(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")
	preVal := types.HexToHash("0xaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(100),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  map[types.Hash]types.Hash{slot: preVal},
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)

	snap := sdb.Snapshot()
	sdb.SetState(addr, slot, types.HexToHash("0xbb"))
	sdb.RevertToSnapshot(snap)

	// After revert, current state should be pre-state value.
	if sdb.GetState(addr, slot) != preVal {
		t.Fatal("storage should revert to pre-state value")
	}

	// GetCommittedState always returns witness pre-state.
	if sdb.GetCommittedState(addr, slot) != preVal {
		t.Fatal("committed state should always be witness pre-state")
	}
}

// --- vm.StateDB interface compliance ---

func TestWitnessStateDB_ImplementsVMStateDB(t *testing.T) {
	// This test simply verifies the compile-time check in verifier.go.
	// If this file compiles, WitnessStateDB implements vm.StateDB.
	w := NewBlockWitness()
	_ = NewWitnessStateDB(w)
}
