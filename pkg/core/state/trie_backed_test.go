package state

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// TestTrieBackedEmptyRoot verifies that an empty state returns the canonical
// empty trie root hash (Keccak256 of RLP-encoded empty string).
func TestTrieBackedEmptyRoot(t *testing.T) {
	db := NewTrieBackedStateDB()

	root := db.IntermediateRoot(false)
	if root != types.EmptyRootHash {
		t.Errorf("empty state root = %s, want EmptyRootHash %s", root, types.EmptyRootHash)
	}

	// With deleteEmpty=true, result should be the same.
	root2 := db.IntermediateRoot(true)
	if root2 != types.EmptyRootHash {
		t.Errorf("empty state root (deleteEmpty) = %s, want EmptyRootHash", root2)
	}
}

// TestTrieBackedAddAccountChangesRoot verifies that adding accounts
// changes the state root away from the empty root.
func TestTrieBackedAddAccountChangesRoot(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	db.AddBalance(addr, big.NewInt(1000))

	root := db.IntermediateRoot(false)
	if root == types.EmptyRootHash {
		t.Error("state root should not be empty after adding account")
	}
	if root == (types.Hash{}) {
		t.Error("state root should not be zero hash")
	}
}

// TestTrieBackedDeterministic verifies that the same state always produces
// the same root, regardless of the order accounts are created.
func TestTrieBackedDeterministic(t *testing.T) {
	db1 := NewTrieBackedStateDB()
	db2 := NewTrieBackedStateDB()

	addr1 := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	addr2 := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// Add same state in different order.
	db1.AddBalance(addr1, big.NewInt(100))
	db1.AddBalance(addr2, big.NewInt(200))

	db2.AddBalance(addr2, big.NewInt(200))
	db2.AddBalance(addr1, big.NewInt(100))

	root1 := db1.IntermediateRoot(false)
	root2 := db2.IntermediateRoot(false)

	if root1 != root2 {
		t.Errorf("roots should be equal: %s vs %s", root1, root2)
	}
}

// TestTrieBackedDeterministicRepeated verifies that calling IntermediateRoot
// multiple times on the same state yields the same result.
func TestTrieBackedDeterministicRepeated(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	db.AddBalance(addr, big.NewInt(42))
	db.SetNonce(addr, 7)

	root1 := db.IntermediateRoot(false)
	root2 := db.IntermediateRoot(false)
	root3 := db.IntermediateRoot(false)

	if root1 != root2 || root2 != root3 {
		t.Errorf("repeated calls should yield same root: %s, %s, %s", root1, root2, root3)
	}
}

// TestTrieBackedStorageIntegration verifies that storage changes affect the
// state root through the storage trie.
func TestTrieBackedStorageIntegration(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	db.AddBalance(addr, big.NewInt(100))

	rootBefore := db.IntermediateRoot(false)

	// Add storage to the account.
	key := types.HexToHash("0x01")
	val := types.HexToHash("0x42")
	db.SetState(addr, key, val)

	rootAfter := db.IntermediateRoot(false)

	if rootBefore == rootAfter {
		t.Error("storage change should affect state root")
	}
}

// TestTrieBackedMultipleStorageSlots verifies that multiple storage slots
// are correctly included in the storage trie.
func TestTrieBackedMultipleStorageSlots(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x2222222222222222222222222222222222222222")
	db.AddBalance(addr, big.NewInt(500))

	// Set multiple storage slots.
	for i := 0; i < 10; i++ {
		var key, val types.Hash
		key[31] = byte(i)
		val[31] = byte(i + 1)
		db.SetState(addr, key, val)
	}

	root := db.IntermediateRoot(false)
	if root == types.EmptyRootHash {
		t.Error("root should not be empty with storage")
	}

	// Same state should produce same root.
	root2 := db.IntermediateRoot(false)
	if root != root2 {
		t.Errorf("repeated root should match: %s vs %s", root, root2)
	}
}

// TestTrieBackedDeleteEmptyAccounts verifies that deleteEmpty=true removes
// accounts with zero nonce, zero balance, and empty code hash (EIP-161).
func TestTrieBackedDeleteEmptyAccounts(t *testing.T) {
	db := NewTrieBackedStateDB()

	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	// addr1 has balance, addr2 is empty (created but no balance/nonce/code).
	db.AddBalance(addr1, big.NewInt(100))
	db.CreateAccount(addr2) // empty account

	rootWithEmpty := db.IntermediateRoot(false)

	// Create a fresh DB with only addr1 for comparison.
	dbSingle := NewTrieBackedStateDB()
	dbSingle.AddBalance(addr1, big.NewInt(100))
	rootSingle := dbSingle.IntermediateRoot(false)

	// With deleteEmpty=false, the empty account should be included, so
	// roots should differ.
	if rootWithEmpty == rootSingle {
		t.Error("root with empty account should differ from root without it (deleteEmpty=false)")
	}

	// Now call with deleteEmpty=true on a fresh copy that has both accounts.
	db2 := NewTrieBackedStateDB()
	db2.AddBalance(addr1, big.NewInt(100))
	db2.CreateAccount(addr2)

	rootCleaned := db2.IntermediateRoot(true)

	if rootCleaned != rootSingle {
		t.Errorf("deleteEmpty root %s should equal single-account root %s", rootCleaned, rootSingle)
	}
}

// TestTrieBackedDeleteEmptyPreservesNonEmpty verifies that deleteEmpty only
// removes truly empty accounts and keeps those with nonce, balance, or code.
func TestTrieBackedDeleteEmptyPreservesNonEmpty(t *testing.T) {
	// Account with nonce only.
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x3333333333333333333333333333333333333333")
	db.CreateAccount(addr)
	db.SetNonce(addr, 1)

	root := db.IntermediateRoot(true)
	if root == types.EmptyRootHash {
		t.Error("account with nonce should not be deleted")
	}

	// Account with code only.
	db2 := NewTrieBackedStateDB()
	addr2 := types.HexToAddress("0x4444444444444444444444444444444444444444")
	db2.CreateAccount(addr2)
	db2.SetCode(addr2, []byte{0x60, 0x00})

	root2 := db2.IntermediateRoot(true)
	if root2 == types.EmptyRootHash {
		t.Error("account with code should not be deleted")
	}
}

// TestTrieBackedNonceAffectsRoot verifies that nonce changes affect the root.
func TestTrieBackedNonceAffectsRoot(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	db.AddBalance(addr, big.NewInt(100))

	rootBefore := db.IntermediateRoot(false)
	db.SetNonce(addr, 1)
	rootAfter := db.IntermediateRoot(false)

	if rootBefore == rootAfter {
		t.Error("nonce change should affect state root")
	}
}

// TestTrieBackedCodeAffectsRoot verifies that code changes affect the root.
func TestTrieBackedCodeAffectsRoot(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x5555555555555555555555555555555555555555")
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(1)) // non-empty so deleteEmpty doesn't remove

	rootBefore := db.IntermediateRoot(false)
	db.SetCode(addr, []byte{0x60, 0x00, 0x60, 0x00, 0xf3})
	rootAfter := db.IntermediateRoot(false)

	if rootBefore == rootAfter {
		t.Error("code change should affect state root")
	}
}

// TestTrieBackedDifferentBalanceDifferentRoot verifies that different
// balances produce different roots.
func TestTrieBackedDifferentBalanceDifferentRoot(t *testing.T) {
	db1 := NewTrieBackedStateDB()
	db2 := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	db1.AddBalance(addr, big.NewInt(100))
	db2.AddBalance(addr, big.NewInt(200))

	root1 := db1.IntermediateRoot(false)
	root2 := db2.IntermediateRoot(false)

	if root1 == root2 {
		t.Error("different balances should produce different roots")
	}
}

// TestTrieBackedSelfDestructedExcluded verifies that self-destructed
// accounts are excluded from root computation.
func TestTrieBackedSelfDestructedExcluded(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	db.AddBalance(addr1, big.NewInt(100))
	db.AddBalance(addr2, big.NewInt(200))

	db.SelfDestruct(addr2)
	rootAfter := db.IntermediateRoot(false)

	// Should match a state with only addr1.
	dbSingle := NewTrieBackedStateDB()
	dbSingle.AddBalance(addr1, big.NewInt(100))
	rootSingle := dbSingle.IntermediateRoot(false)

	if rootAfter != rootSingle {
		t.Errorf("root after self-destruct %s should equal single-account root %s", rootAfter, rootSingle)
	}
}

// TestTrieBackedCommit verifies that Commit produces the same root as
// IntermediateRoot and correctly flushes dirty storage.
func TestTrieBackedCommit(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	db.AddBalance(addr, big.NewInt(100))
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0x42"))

	rootBefore := db.IntermediateRoot(false)
	committedRoot, err := db.Commit()
	if err != nil {
		t.Fatalf("Commit error: %v", err)
	}

	if rootBefore != committedRoot {
		t.Errorf("IntermediateRoot %s != Commit root %s", rootBefore, committedRoot)
	}

	// After commit, root should remain the same.
	rootAfter := db.IntermediateRoot(false)
	if rootAfter != committedRoot {
		t.Errorf("root after commit %s != committed root %s", rootAfter, committedRoot)
	}
}

// TestTrieBackedGetRootMatchesIntermediateRoot verifies that GetRoot
// returns the same value as IntermediateRoot(false).
func TestTrieBackedGetRootMatchesIntermediateRoot(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	db.AddBalance(addr, big.NewInt(500))
	db.SetNonce(addr, 3)
	db.SetState(addr, types.HexToHash("0xaa"), types.HexToHash("0xbb"))

	getRoot := db.GetRoot()
	intermediateRoot := db.IntermediateRoot(false)

	if getRoot != intermediateRoot {
		t.Errorf("GetRoot %s != IntermediateRoot %s", getRoot, intermediateRoot)
	}
}

// TestTrieBackedMultipleAccountsWithStorage tests root computation with
// many accounts each having multiple storage slots.
func TestTrieBackedMultipleAccountsWithStorage(t *testing.T) {
	db := NewTrieBackedStateDB()

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

	root := db.IntermediateRoot(false)
	if root == types.EmptyRootHash {
		t.Error("root should not be empty with 10 accounts")
	}

	// Verify determinism.
	root2 := db.IntermediateRoot(false)
	if root != root2 {
		t.Errorf("calling IntermediateRoot twice should produce same result: %s vs %s", root, root2)
	}
}

// TestTrieBackedFromMemory verifies that wrapping an existing MemoryStateDB
// produces correct roots.
func TestTrieBackedFromMemory(t *testing.T) {
	mem := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	mem.AddBalance(addr, big.NewInt(999))

	trieBacked := NewTrieBackedFromMemory(mem)
	root := trieBacked.IntermediateRoot(false)

	if root == types.EmptyRootHash {
		t.Error("root from wrapped MemoryStateDB should not be empty")
	}
	if root == (types.Hash{}) {
		t.Error("root should not be zero hash")
	}
}

// TestTrieBackedStorageDeletion verifies that deleting a storage slot
// (setting to zero) changes the root.
func TestTrieBackedStorageDeletion(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	db.AddBalance(addr, big.NewInt(100))

	key := types.HexToHash("0x01")
	val := types.HexToHash("0x42")
	db.SetState(addr, key, val)

	rootWithStorage := db.IntermediateRoot(false)

	// Delete the storage slot by setting to zero.
	db.SetState(addr, key, types.Hash{})

	rootNoStorage := db.IntermediateRoot(false)

	if rootWithStorage == rootNoStorage {
		t.Error("deleting storage slot should change root")
	}

	// The root with deleted storage should match a state with no storage.
	dbNoStorage := NewTrieBackedStateDB()
	dbNoStorage.AddBalance(addr, big.NewInt(100))
	expected := dbNoStorage.IntermediateRoot(false)

	if rootNoStorage != expected {
		t.Errorf("root after storage deletion %s != root with no storage %s", rootNoStorage, expected)
	}
}

// TestTrieBackedAllAccountsSelfDestructed verifies that if all accounts
// are self-destructed, the root is the empty root hash.
func TestTrieBackedAllAccountsSelfDestructed(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	db.AddBalance(addr1, big.NewInt(100))
	db.AddBalance(addr2, big.NewInt(200))

	db.SelfDestruct(addr1)
	db.SelfDestruct(addr2)

	root := db.IntermediateRoot(false)
	if root != types.EmptyRootHash {
		t.Errorf("all accounts self-destructed should give EmptyRootHash, got %s", root)
	}
}

// TestTrieBackedInterfaceCompliance ensures TrieBackedStateDB satisfies
// the StateDB interface at compile time.
func TestTrieBackedInterfaceCompliance(t *testing.T) {
	var _ StateDB = (*TrieBackedStateDB)(nil)
}
