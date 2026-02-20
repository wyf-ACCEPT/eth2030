package state

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestJournalAppendAndLength(t *testing.T) {
	j := NewJournal()
	if j.Length() != 0 {
		t.Fatalf("new journal length = %d, want 0", j.Length())
	}

	j.Append(JrnlBalanceChange{
		Address:     types.HexToAddress("0x01"),
		PrevBalance: big.NewInt(100),
	})
	j.Append(JrnlNonceChange{
		Address:   types.HexToAddress("0x01"),
		PrevNonce: 0,
	})

	if j.Length() != 2 {
		t.Fatalf("journal length = %d, want 2", j.Length())
	}
}

func TestJournalSnapshotAndRevert(t *testing.T) {
	s := NewMemoryStateDB()
	addr := types.HexToAddress("0xabc")
	s.CreateAccount(addr)
	s.AddBalance(addr, big.NewInt(1000))
	s.SetNonce(addr, 5)

	j := NewJournal()

	// Take snapshot before modifications.
	snap := j.Snapshot()
	if snap != 0 {
		t.Fatalf("first snapshot ID = %d, want 0", snap)
	}

	// Record a balance change, then modify state.
	j.Append(NewJrnlBalanceChange(s, addr))
	s.AddBalance(addr, big.NewInt(500))

	// Record a nonce change, then modify state.
	j.Append(NewJrnlNonceChange(s, addr))
	s.SetNonce(addr, 10)

	// Verify modified state.
	if got := s.GetBalance(addr); got.Cmp(big.NewInt(1500)) != 0 {
		t.Fatalf("balance after modify = %v, want 1500", got)
	}
	if got := s.GetNonce(addr); got != 10 {
		t.Fatalf("nonce after modify = %d, want 10", got)
	}

	// Revert to snapshot.
	err := j.RevertTo(snap, s)
	if err != nil {
		t.Fatalf("RevertTo: %v", err)
	}

	// State should be restored.
	if got := s.GetBalance(addr); got.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("balance after revert = %v, want 1000", got)
	}
	if got := s.GetNonce(addr); got != 5 {
		t.Fatalf("nonce after revert = %d, want 5", got)
	}
	if j.Length() != 0 {
		t.Fatalf("journal length after revert = %d, want 0", j.Length())
	}
}

func TestJournalStorageRevert(t *testing.T) {
	s := NewMemoryStateDB()
	addr := types.HexToAddress("0xdef")
	s.CreateAccount(addr)

	key := types.HexToHash("0x01")
	val := types.HexToHash("0xff")

	j := NewJournal()
	snap := j.Snapshot()

	// Record storage change then modify.
	j.Append(NewJrnlStorageChange(s, addr, key))
	s.SetState(addr, key, val)

	if got := s.GetState(addr, key); got != val {
		t.Fatalf("storage after set = %v, want %v", got, val)
	}

	err := j.RevertTo(snap, s)
	if err != nil {
		t.Fatalf("RevertTo: %v", err)
	}

	// Storage should be restored to zero (since the slot was newly written,
	// the revert deletes it from dirtyStorage, exposing the committed zero).
	if got := s.GetState(addr, key); got != (types.Hash{}) {
		t.Fatalf("storage after revert = %v, want zero", got)
	}
}

func TestJournalCodeRevert(t *testing.T) {
	s := NewMemoryStateDB()
	addr := types.HexToAddress("0xc0de")
	s.CreateAccount(addr)

	j := NewJournal()
	snap := j.Snapshot()

	j.Append(NewJrnlCodeChange(s, addr))
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xfd} // PUSH1 0 PUSH1 0 REVERT
	s.SetCode(addr, code)

	if len(s.GetCode(addr)) != 5 {
		t.Fatalf("code length after set = %d, want 5", len(s.GetCode(addr)))
	}

	err := j.RevertTo(snap, s)
	if err != nil {
		t.Fatalf("RevertTo: %v", err)
	}

	if len(s.GetCode(addr)) != 0 {
		t.Fatalf("code length after revert = %d, want 0", len(s.GetCode(addr)))
	}
}

func TestJournalAccountCreatedRevert(t *testing.T) {
	s := NewMemoryStateDB()
	addr := types.HexToAddress("0x0a0b")

	j := NewJournal()
	snap := j.Snapshot()

	j.Append(AccountCreated{Address: addr})
	s.CreateAccount(addr)
	s.AddBalance(addr, big.NewInt(100))

	if !s.Exist(addr) {
		t.Fatal("account should exist after create")
	}

	err := j.RevertTo(snap, s)
	if err != nil {
		t.Fatalf("RevertTo: %v", err)
	}

	if s.Exist(addr) {
		t.Fatal("account should not exist after revert")
	}
}

func TestJournalAccountSuicidedRevert(t *testing.T) {
	s := NewMemoryStateDB()
	addr := types.HexToAddress("0xdead")
	s.CreateAccount(addr)
	s.AddBalance(addr, big.NewInt(5000))
	s.SetNonce(addr, 42)
	s.SetCode(addr, []byte{0x00})

	j := NewJournal()
	snap := j.Snapshot()

	j.Append(NewAccountSuicided(s, addr))
	s.SelfDestruct(addr)

	if !s.HasSelfDestructed(addr) {
		t.Fatal("should be self-destructed")
	}

	err := j.RevertTo(snap, s)
	if err != nil {
		t.Fatalf("RevertTo: %v", err)
	}

	if s.HasSelfDestructed(addr) {
		t.Fatal("should not be self-destructed after revert")
	}
	if got := s.GetBalance(addr); got.Cmp(big.NewInt(5000)) != 0 {
		t.Fatalf("balance after revert = %v, want 5000", got)
	}
	if got := s.GetNonce(addr); got != 42 {
		t.Fatalf("nonce after revert = %d, want 42", got)
	}
}

func TestJournalRefundRevert(t *testing.T) {
	s := NewMemoryStateDB()

	j := NewJournal()
	snap := j.Snapshot()

	j.Append(NewJrnlRefundChange(s))
	s.AddRefund(4800)

	if s.GetRefund() != 4800 {
		t.Fatalf("refund = %d, want 4800", s.GetRefund())
	}

	err := j.RevertTo(snap, s)
	if err != nil {
		t.Fatalf("RevertTo: %v", err)
	}

	if s.GetRefund() != 0 {
		t.Fatalf("refund after revert = %d, want 0", s.GetRefund())
	}
}

func TestJournalLogEntryRevert(t *testing.T) {
	s := NewMemoryStateDB()
	txHash := types.HexToHash("0xaabbcc")
	s.SetTxContext(txHash, 0)

	j := NewJournal()
	snap := j.Snapshot()

	var txHashBytes [32]byte
	copy(txHashBytes[:], txHash[:])

	j.Append(JrnlLogEntry{TxHash: txHashBytes, PrevLen: 0})
	s.AddLog(&types.Log{Address: types.HexToAddress("0x01"), Data: []byte{0x01}})

	if len(s.GetLogs(txHash)) != 1 {
		t.Fatalf("logs after add = %d, want 1", len(s.GetLogs(txHash)))
	}

	err := j.RevertTo(snap, s)
	if err != nil {
		t.Fatalf("RevertTo: %v", err)
	}

	if len(s.GetLogs(txHash)) != 0 {
		t.Fatalf("logs after revert = %d, want 0", len(s.GetLogs(txHash)))
	}
}

func TestJournalNestedSnapshots(t *testing.T) {
	s := NewMemoryStateDB()
	addr := types.HexToAddress("0x01")
	s.CreateAccount(addr)
	s.AddBalance(addr, big.NewInt(100))

	j := NewJournal()

	// Snapshot 0: balance = 100
	snap0 := j.Snapshot()

	j.Append(NewJrnlBalanceChange(s, addr))
	s.AddBalance(addr, big.NewInt(50)) // balance = 150

	// Snapshot 1: balance = 150
	snap1 := j.Snapshot()

	j.Append(NewJrnlBalanceChange(s, addr))
	s.AddBalance(addr, big.NewInt(25)) // balance = 175

	// Revert to snap1 (balance should be 150).
	err := j.RevertTo(snap1, s)
	if err != nil {
		t.Fatalf("RevertTo snap1: %v", err)
	}
	if got := s.GetBalance(addr); got.Cmp(big.NewInt(150)) != 0 {
		t.Fatalf("balance after revert to snap1 = %v, want 150", got)
	}

	// Revert to snap0 (balance should be 100).
	err = j.RevertTo(snap0, s)
	if err != nil {
		t.Fatalf("RevertTo snap0: %v", err)
	}
	if got := s.GetBalance(addr); got.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("balance after revert to snap0 = %v, want 100", got)
	}
}

func TestJournalInvalidSnapshot(t *testing.T) {
	j := NewJournal()
	s := NewMemoryStateDB()

	err := j.RevertTo(-1, s)
	if err != ErrInvalidSnapshot {
		t.Fatalf("expected ErrInvalidSnapshot, got %v", err)
	}

	err = j.RevertTo(0, s)
	if err != ErrInvalidSnapshot {
		t.Fatalf("expected ErrInvalidSnapshot for empty snapshots, got %v", err)
	}

	j.Snapshot()
	err = j.RevertTo(1, s)
	if err != ErrInvalidSnapshot {
		t.Fatalf("expected ErrInvalidSnapshot for out-of-range, got %v", err)
	}
}

func TestJournalReset(t *testing.T) {
	j := NewJournal()
	j.Append(JrnlBalanceChange{Address: types.HexToAddress("0x01"), PrevBalance: big.NewInt(0)})
	j.Snapshot()
	j.Append(JrnlNonceChange{Address: types.HexToAddress("0x01"), PrevNonce: 0})

	j.Reset()

	if j.Length() != 0 {
		t.Fatalf("length after reset = %d, want 0", j.Length())
	}
	if j.SnapshotCount() != 0 {
		t.Fatalf("snapshot count after reset = %d, want 0", j.SnapshotCount())
	}
}

func TestJournalEntries(t *testing.T) {
	j := NewJournal()
	j.Append(JrnlBalanceChange{Address: types.HexToAddress("0x01"), PrevBalance: big.NewInt(10)})
	j.Append(JrnlNonceChange{Address: types.HexToAddress("0x02"), PrevNonce: 3})

	entries := j.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(entries))
	}

	// Verify the copy is independent.
	j.Append(JrnlRefundChange{PrevRefund: 100})
	if len(entries) != 2 {
		t.Fatal("entries copy was mutated")
	}
}
