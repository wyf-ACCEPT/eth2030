package state

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestEmptyStateRoot(t *testing.T) {
	db := NewMemoryStateDB()
	root := db.GetRoot()
	if root != types.EmptyRootHash {
		t.Errorf("empty state root = %x, want EmptyRootHash", root)
	}
}

func TestSingleAccountRoot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111")
	db.AddBalance(addr, big.NewInt(1000))

	root := db.GetRoot()
	if root == types.EmptyRootHash {
		t.Error("state root should not be empty after adding account")
	}
	if root == (types.Hash{}) {
		t.Error("state root should not be zero hash")
	}
}

func TestDeterministicRoot(t *testing.T) {
	// Same state should always produce the same root.
	db1 := NewMemoryStateDB()
	db2 := NewMemoryStateDB()

	addr1 := types.HexToAddress("0xaaaa")
	addr2 := types.HexToAddress("0xbbbb")

	// Add same state in different order.
	db1.AddBalance(addr1, big.NewInt(100))
	db1.AddBalance(addr2, big.NewInt(200))

	db2.AddBalance(addr2, big.NewInt(200))
	db2.AddBalance(addr1, big.NewInt(100))

	root1 := db1.GetRoot()
	root2 := db2.GetRoot()

	if root1 != root2 {
		t.Errorf("roots should be equal: %x vs %x", root1, root2)
	}
}

func TestDifferentStatesProduceDifferentRoots(t *testing.T) {
	db1 := NewMemoryStateDB()
	db2 := NewMemoryStateDB()

	addr := types.HexToAddress("0x1111")

	db1.AddBalance(addr, big.NewInt(100))
	db2.AddBalance(addr, big.NewInt(200))

	root1 := db1.GetRoot()
	root2 := db2.GetRoot()

	if root1 == root2 {
		t.Error("different balances should produce different roots")
	}
}

func TestStorageAffectsRoot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111")
	db.AddBalance(addr, big.NewInt(100))

	rootBefore := db.GetRoot()

	key := types.HexToHash("0x01")
	val := types.HexToHash("0x42")
	db.SetState(addr, key, val)

	rootAfter := db.GetRoot()

	if rootBefore == rootAfter {
		t.Error("storage change should affect state root")
	}
}

func TestNonceAffectsRoot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111")
	db.AddBalance(addr, big.NewInt(100))

	rootBefore := db.GetRoot()

	db.SetNonce(addr, 1)

	rootAfter := db.GetRoot()

	if rootBefore == rootAfter {
		t.Error("nonce change should affect state root")
	}
}

func TestCodeAffectsRoot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x3333")
	db.CreateAccount(addr)

	rootBefore := db.GetRoot()

	db.SetCode(addr, []byte{0x60, 0x00, 0x60, 0x00, 0xf3})

	rootAfter := db.GetRoot()

	if rootBefore == rootAfter {
		t.Error("code change should affect state root")
	}
}

func TestCommitProducesSameRoot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111")
	db.AddBalance(addr, big.NewInt(100))
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0x42"))

	rootBefore := db.GetRoot()
	committedRoot, err := db.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if rootBefore != committedRoot {
		t.Errorf("GetRoot %x != Commit root %x", rootBefore, committedRoot)
	}

	// After commit, root should stay the same.
	rootAfter := db.GetRoot()
	if rootAfter != committedRoot {
		t.Errorf("root after commit %x != committed root %x", rootAfter, committedRoot)
	}
}

func TestSelfDestructedAccountExcluded(t *testing.T) {
	db := NewMemoryStateDB()
	addr1 := types.HexToAddress("0x1111")
	addr2 := types.HexToAddress("0x2222")
	db.AddBalance(addr1, big.NewInt(100))
	db.AddBalance(addr2, big.NewInt(200))

	rootBefore := db.GetRoot()

	db.SelfDestruct(addr2)

	rootAfter := db.GetRoot()

	if rootBefore == rootAfter {
		t.Error("self-destructing account should change root")
	}

	// Root should now equal a state with only addr1.
	dbSingle := NewMemoryStateDB()
	dbSingle.AddBalance(addr1, big.NewInt(100))
	singleRoot := dbSingle.GetRoot()

	if rootAfter != singleRoot {
		t.Errorf("root after self-destruct %x != single account root %x", rootAfter, singleRoot)
	}
}

func TestMultipleAccountsWithStorage(t *testing.T) {
	db := NewMemoryStateDB()

	for i := 0; i < 10; i++ {
		var addr types.Address
		addr[19] = byte(i + 1)
		db.AddBalance(addr, big.NewInt(int64(i+1)*1000))
		db.SetNonce(addr, uint64(i))

		for j := 0; j < 5; j++ {
			var key, val types.Hash
			key[31] = byte(j)
			val[31] = byte(j + 1)
			db.SetState(addr, key, val)
		}
	}

	root := db.GetRoot()
	if root == types.EmptyRootHash {
		t.Error("root should not be empty with 10 accounts")
	}

	// Verify determinism.
	root2 := db.GetRoot()
	if root != root2 {
		t.Error("calling GetRoot twice should return same result")
	}
}
