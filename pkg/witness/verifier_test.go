package witness

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestVerifierBasicAccountOps(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(1000),
		Nonce:    3,
		CodeHash: types.EmptyCodeHash,
		Storage:  make(map[types.Hash]types.Hash),
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)

	// Balance
	if bal := sdb.GetBalance(addr); bal.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("balance = %s, want 1000", bal)
	}
	// Nonce
	if n := sdb.GetNonce(addr); n != 3 {
		t.Fatalf("nonce = %d, want 3", n)
	}
	// Exist
	if !sdb.Exist(addr) {
		t.Fatal("expected account to exist")
	}
	// Empty (has balance, so not empty)
	if sdb.Empty(addr) {
		t.Fatal("expected account to not be empty")
	}
	// Code hash
	if sdb.GetCodeHash(addr) != types.EmptyCodeHash {
		t.Fatal("expected empty code hash")
	}
	// Code size
	if sdb.GetCodeSize(addr) != 0 {
		t.Fatal("expected code size 0")
	}
}

func TestVerifierNonExistentAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xdead")
	if sdb.Exist(addr) {
		t.Fatal("expected account to not exist")
	}
	if !sdb.Empty(addr) {
		t.Fatal("expected account to be empty")
	}
	if sdb.GetBalance(addr).Sign() != 0 {
		t.Fatal("expected zero balance")
	}
	if sdb.GetNonce(addr) != 0 {
		t.Fatal("expected zero nonce")
	}
}

func TestVerifierStorage(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xbbbb")
	slot := types.HexToHash("0x01")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(500),
		Nonce:    1,
		CodeHash: types.EmptyCodeHash,
		Storage:  map[types.Hash]types.Hash{slot: types.HexToHash("0xff")},
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)

	// Read pre-state storage.
	val := sdb.GetState(addr, slot)
	if val != types.HexToHash("0xff") {
		t.Fatalf("storage = %s, want 0xff", val.Hex())
	}

	// Write new value.
	sdb.SetState(addr, slot, types.HexToHash("0xdd"))
	val = sdb.GetState(addr, slot)
	if val != types.HexToHash("0xdd") {
		t.Fatalf("storage after write = %s, want 0xdd", val.Hex())
	}

	// GetCommittedState returns the witness pre-state.
	committed := sdb.GetCommittedState(addr, slot)
	if committed != types.HexToHash("0xff") {
		t.Fatalf("committed state = %s, want 0xff", committed.Hex())
	}
}

func TestVerifierBalanceTransfer(t *testing.T) {
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

func TestVerifierCreateAccount(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xcccc")
	sdb.CreateAccount(addr)

	if !sdb.Exist(addr) {
		t.Fatal("expected newly created account to exist")
	}
	if !sdb.Empty(addr) {
		t.Fatal("expected newly created account to be empty")
	}
	if sdb.GetBalance(addr).Sign() != 0 {
		t.Fatal("expected zero balance for new account")
	}
}

func TestVerifierSnapshotRevert(t *testing.T) {
	w := NewBlockWitness()
	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")
	w.State[addr] = &AccountWitness{
		Balance:  big.NewInt(1000),
		Nonce:    0,
		CodeHash: types.EmptyCodeHash,
		Storage:  map[types.Hash]types.Hash{slot: types.HexToHash("0xff")},
		Exists:   true,
	}

	sdb := NewWitnessStateDB(w)

	snap := sdb.Snapshot()

	// Modify state.
	sdb.AddBalance(addr, big.NewInt(500))
	sdb.SetState(addr, slot, types.HexToHash("0xdd"))
	sdb.SetNonce(addr, 10)

	// Verify modifications.
	if sdb.GetBalance(addr).Cmp(big.NewInt(1500)) != 0 {
		t.Fatalf("balance after add = %s, want 1500", sdb.GetBalance(addr))
	}
	if sdb.GetState(addr, slot) != types.HexToHash("0xdd") {
		t.Fatalf("storage after set = %s, want 0xdd", sdb.GetState(addr, slot).Hex())
	}

	// Revert.
	sdb.RevertToSnapshot(snap)

	// Verify revert.
	if sdb.GetBalance(addr).Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("balance after revert = %s, want 1000", sdb.GetBalance(addr))
	}
	if sdb.GetState(addr, slot) != types.HexToHash("0xff") {
		t.Fatalf("storage after revert = %s, want 0xff", sdb.GetState(addr, slot).Hex())
	}
	if sdb.GetNonce(addr) != 0 {
		t.Fatalf("nonce after revert = %d, want 0", sdb.GetNonce(addr))
	}
}

func TestVerifierSelfDestruct(t *testing.T) {
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

func TestVerifierAccessList(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")

	if sdb.AddressInAccessList(addr) {
		t.Fatal("address should not be in access list initially")
	}

	sdb.AddAddressToAccessList(addr)
	if !sdb.AddressInAccessList(addr) {
		t.Fatal("address should be in access list")
	}

	sdb.AddSlotToAccessList(addr, slot)
	addrOk, slotOk := sdb.SlotInAccessList(addr, slot)
	if !addrOk || !slotOk {
		t.Fatal("expected address and slot in access list")
	}
}

func TestVerifierTransientStorage(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	addr := types.HexToAddress("0xaaaa")
	key := types.HexToHash("0x01")

	// Initially zero.
	if sdb.GetTransientState(addr, key) != (types.Hash{}) {
		t.Fatal("expected zero transient state")
	}

	sdb.SetTransientState(addr, key, types.HexToHash("0xab"))
	if sdb.GetTransientState(addr, key) != types.HexToHash("0xab") {
		t.Fatal("expected transient state 0xab")
	}
}

func TestVerifierRefund(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	sdb.AddRefund(200)
	if sdb.GetRefund() != 200 {
		t.Fatalf("refund = %d, want 200", sdb.GetRefund())
	}
	sdb.SubRefund(50)
	if sdb.GetRefund() != 150 {
		t.Fatalf("refund = %d, want 150", sdb.GetRefund())
	}
}

func TestVerifierLogs(t *testing.T) {
	w := NewBlockWitness()
	sdb := NewWitnessStateDB(w)

	log := &types.Log{
		Address: types.HexToAddress("0xbbbb"),
		Topics:  []types.Hash{types.HexToHash("0x01")},
		Data:    []byte{0x42},
	}
	sdb.AddLog(log)
	// Logs are stored internally -- no assertion failure means success.
}

func TestVerifierCode(t *testing.T) {
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
	if sdb.GetCodeSize(addr) != len(code) {
		t.Fatalf("code size = %d, want %d", sdb.GetCodeSize(addr), len(code))
	}
	if sdb.GetCodeHash(addr) != codeHash {
		t.Fatalf("code hash = %s, want %s", sdb.GetCodeHash(addr).Hex(), codeHash.Hex())
	}
}

func TestVerifierSetCode(t *testing.T) {
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
		t.Fatalf("code size after SetCode = %d, want 3", sdb.GetCodeSize(addr))
	}
}

func TestVerifierAddBalanceCreatesAccount(t *testing.T) {
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
