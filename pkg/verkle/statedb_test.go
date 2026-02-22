package verkle

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestVerkleStateDB_SetAndGetAccount(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	sdb := NewVerkleStateDB(tree)

	addr := types.BytesToAddress([]byte{0xde, 0xad})

	// Account should not exist initially.
	if sdb.GetAccount(addr) != nil {
		t.Error("expected nil for non-existent account")
	}

	acct := &AccountState{
		Nonce:    42,
		Balance:  big.NewInt(1_000_000),
		CodeHash: types.HexToHash("0xdeadbeef"),
	}
	sdb.SetAccount(addr, acct)

	got := sdb.GetAccount(addr)
	if got == nil {
		t.Fatal("expected account after set")
	}
	if got.Nonce != 42 {
		t.Errorf("nonce = %d, want 42", got.Nonce)
	}
	if got.Balance.Int64() != 1_000_000 {
		t.Errorf("balance = %d, want 1000000", got.Balance.Int64())
	}
	if got.CodeHash != types.HexToHash("0xdeadbeef") {
		t.Errorf("code hash mismatch")
	}
}

func TestVerkleStateDB_MultipleAccounts(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	sdb := NewVerkleStateDB(tree)

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})

	sdb.SetAccount(addr1, &AccountState{
		Nonce:   1,
		Balance: big.NewInt(100),
	})
	sdb.SetAccount(addr2, &AccountState{
		Nonce:   2,
		Balance: big.NewInt(200),
	})

	a1 := sdb.GetAccount(addr1)
	a2 := sdb.GetAccount(addr2)

	if a1.Nonce != 1 || a2.Nonce != 2 {
		t.Error("account nonces should be independent")
	}
	if a1.Balance.Int64() != 100 || a2.Balance.Int64() != 200 {
		t.Error("account balances should be independent")
	}
}

func TestVerkleStateDB_SetAndGetStorage(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	sdb := NewVerkleStateDB(tree)

	addr := types.BytesToAddress([]byte{0x01})
	key := types.HexToHash("0x01")
	val := types.HexToHash("0xaa")

	// Should be zero initially.
	got := sdb.GetStorage(addr, key)
	if !got.IsZero() {
		t.Error("expected zero for non-existent storage")
	}

	sdb.SetStorage(addr, key, val)
	got = sdb.GetStorage(addr, key)
	if got != val {
		t.Errorf("storage = %x, want %x", got, val)
	}
}

func TestVerkleStateDB_Commit(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	sdb := NewVerkleStateDB(tree)

	root1, err := sdb.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	sdb.SetAccount(types.BytesToAddress([]byte{0x01}), &AccountState{
		Nonce:   1,
		Balance: big.NewInt(100),
	})

	root2, err := sdb.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if root1 == root2 {
		t.Error("root should change after state modification")
	}
}

func TestVerkleStateDB_SetNilAccount(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	sdb := NewVerkleStateDB(tree)

	// Should not panic.
	sdb.SetAccount(types.BytesToAddress([]byte{0x01}), nil)
}

func TestVerkleStateDB_ZeroBalance(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	sdb := NewVerkleStateDB(tree)

	addr := types.BytesToAddress([]byte{0x01})
	sdb.SetAccount(addr, &AccountState{
		Nonce:   0,
		Balance: big.NewInt(0),
	})

	got := sdb.GetAccount(addr)
	if got == nil {
		t.Fatal("expected account with zero balance")
	}
	if got.Balance.Sign() != 0 {
		t.Errorf("balance = %s, want 0", got.Balance)
	}
}

func TestVerkleStateDB_Tree(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	sdb := NewVerkleStateDB(tree)
	if sdb.Tree() != tree {
		t.Error("Tree() should return the underlying tree")
	}
}
