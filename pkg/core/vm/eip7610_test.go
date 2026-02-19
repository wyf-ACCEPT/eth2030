package vm

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// --- HasNonEmptyStorage ---

func TestHasNonEmptyStorage_EmptyAccount(t *testing.T) {
	db := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	db.CreateAccount(addr)

	if HasNonEmptyStorage(db, addr) {
		t.Fatal("expected empty storage for fresh account")
	}
}

func TestHasNonEmptyStorage_WithSlotZero(t *testing.T) {
	db := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x02})
	db.CreateAccount(addr)
	db.SetState(addr, types.BytesToHash([]byte{0}), types.BytesToHash([]byte{0x42}))

	if !HasNonEmptyStorage(db, addr) {
		t.Fatal("expected non-empty storage when slot 0 is set")
	}
}

func TestHasNonEmptyStorage_WithHighSlot(t *testing.T) {
	db := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x03})
	db.CreateAccount(addr)
	// Slot 5 is within the probed set.
	db.SetState(addr, types.BytesToHash([]byte{5}), types.BytesToHash([]byte{0xff}))

	if !HasNonEmptyStorage(db, addr) {
		t.Fatal("expected non-empty storage when slot 5 is set")
	}
}

func TestHasNonEmptyStorage_UnprobedSlot(t *testing.T) {
	db := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x04})
	db.CreateAccount(addr)
	// Slot 100 is outside CommonStorageSlots; the probe will miss it.
	db.SetState(addr, types.BytesToHash([]byte{100}), types.BytesToHash([]byte{0xaa}))

	if HasNonEmptyStorage(db, addr) {
		t.Fatal("slot 100 is outside the probed range; should report empty")
	}
}

// --- CollisionCheck7610 ---

func TestCheckCreateCollision_EmptyAddress(t *testing.T) {
	db := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x10})
	db.CreateAccount(addr)

	chk := NewCollisionCheck7610(true)
	if err := chk.CheckCreateCollision(db, addr); err != nil {
		t.Fatalf("empty address should not collide, got: %v", err)
	}
}

func TestCheckCreateCollision_NonZeroNonce(t *testing.T) {
	db := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x11})
	db.CreateAccount(addr)
	db.SetNonce(addr, 1)

	chk := NewCollisionCheck7610(true)
	if err := chk.CheckCreateCollision(db, addr); err != ErrContractCreationCollision {
		t.Fatalf("expected collision for nonzero nonce, got: %v", err)
	}
}

func TestCheckCreateCollision_NonEmptyCode(t *testing.T) {
	db := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x12})
	db.CreateAccount(addr)
	db.SetCode(addr, []byte{0x60, 0x00, 0x60, 0x00, 0xfd}) // PUSH1 0 PUSH1 0 REVERT

	chk := NewCollisionCheck7610(true)
	if err := chk.CheckCreateCollision(db, addr); err != ErrContractCreationCollision {
		t.Fatalf("expected collision for non-empty code, got: %v", err)
	}
}

func TestCheckCreateCollision_NonEmptyStorage(t *testing.T) {
	db := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x13})
	db.CreateAccount(addr)
	db.SetState(addr, types.BytesToHash([]byte{0}), types.BytesToHash([]byte{0x01}))

	chk := NewCollisionCheck7610(true)
	if err := chk.CheckCreateCollision(db, addr); err != ErrContractCreationCollision {
		t.Fatalf("expected collision for non-empty storage (EIP-7610), got: %v", err)
	}
}

func TestCheckCreateCollision_StorageCheckDisabled(t *testing.T) {
	db := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x14})
	db.CreateAccount(addr)
	db.SetState(addr, types.BytesToHash([]byte{0}), types.BytesToHash([]byte{0x01}))

	// With EIP-7610 disabled, storage alone should not trigger collision.
	chk := NewCollisionCheck7610(false)
	if err := chk.CheckCreateCollision(db, addr); err != nil {
		t.Fatalf("storage-only address should pass when EIP-7610 is disabled, got: %v", err)
	}
}

func TestCheckCreateCollision_BalanceOnly(t *testing.T) {
	db := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x15})
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(1000000))

	chk := NewCollisionCheck7610(true)
	if err := chk.CheckCreateCollision(db, addr); err != nil {
		t.Fatalf("balance-only address should not collide per EIP-7610, got: %v", err)
	}
}

func TestCheckCreateCollision_NoncePlusStorage(t *testing.T) {
	db := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x16})
	db.CreateAccount(addr)
	db.SetNonce(addr, 5)
	db.SetState(addr, types.BytesToHash([]byte{0}), types.BytesToHash([]byte{0x01}))

	chk := NewCollisionCheck7610(true)
	if err := chk.CheckCreateCollision(db, addr); err != ErrContractCreationCollision {
		t.Fatalf("expected collision for nonce+storage, got: %v", err)
	}
}

func TestCheckCreateCollision_CodePlusStorage(t *testing.T) {
	db := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x17})
	db.CreateAccount(addr)
	db.SetCode(addr, []byte{0x00})
	db.SetState(addr, types.BytesToHash([]byte{1}), types.BytesToHash([]byte{0xab}))

	chk := NewCollisionCheck7610(true)
	if err := chk.CheckCreateCollision(db, addr); err != ErrContractCreationCollision {
		t.Fatalf("expected collision for code+storage, got: %v", err)
	}
}

func TestCheckCreateCollision_NonexistentAccount(t *testing.T) {
	db := state.NewMemoryStateDB()
	// Address that was never created in the state.
	addr := types.BytesToAddress([]byte{0x99})

	chk := NewCollisionCheck7610(true)
	if err := chk.CheckCreateCollision(db, addr); err != nil {
		t.Fatalf("nonexistent address should not collide, got: %v", err)
	}
}
