package state

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/trie"
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

// --- StorageRoot tests ---

func TestStorageRootNonExistentAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xdead")

	root := db.StorageRoot(addr)
	if root != types.EmptyRootHash {
		t.Errorf("StorageRoot of non-existent account = %s, want EmptyRootHash", root)
	}
}

func TestStorageRootEmptyAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111")
	db.CreateAccount(addr)

	root := db.StorageRoot(addr)
	if root != types.EmptyRootHash {
		t.Errorf("StorageRoot of empty account = %s, want EmptyRootHash", root)
	}
}

func TestStorageRootWithSingleSlot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111")
	db.CreateAccount(addr)
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	root := db.StorageRoot(addr)
	if root == types.EmptyRootHash {
		t.Error("StorageRoot should not be empty after setting storage")
	}
	if root == (types.Hash{}) {
		t.Error("StorageRoot should not be zero hash")
	}
}

func TestStorageRootChangesWithStorage(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111")
	db.CreateAccount(addr)

	rootEmpty := db.StorageRoot(addr)
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	rootWithSlot := db.StorageRoot(addr)

	if rootEmpty == rootWithSlot {
		t.Error("adding storage should change StorageRoot")
	}
}

func TestStorageRootDeterministic(t *testing.T) {
	db1 := NewMemoryStateDB()
	db2 := NewMemoryStateDB()

	addr := types.HexToAddress("0x1111")
	db1.CreateAccount(addr)
	db2.CreateAccount(addr)

	key1 := types.HexToHash("0x01")
	key2 := types.HexToHash("0x02")
	val1 := types.HexToHash("0xaa")
	val2 := types.HexToHash("0xbb")

	// Add in different order.
	db1.SetState(addr, key1, val1)
	db1.SetState(addr, key2, val2)

	db2.SetState(addr, key2, val2)
	db2.SetState(addr, key1, val1)

	root1 := db1.StorageRoot(addr)
	root2 := db2.StorageRoot(addr)

	if root1 != root2 {
		t.Errorf("StorageRoot should be deterministic: %s vs %s", root1, root2)
	}
}

func TestStorageRootDifferentValues(t *testing.T) {
	db1 := NewMemoryStateDB()
	db2 := NewMemoryStateDB()

	addr := types.HexToAddress("0x1111")
	key := types.HexToHash("0x01")

	db1.CreateAccount(addr)
	db2.CreateAccount(addr)

	db1.SetState(addr, key, types.HexToHash("0xaa"))
	db2.SetState(addr, key, types.HexToHash("0xbb"))

	root1 := db1.StorageRoot(addr)
	root2 := db2.StorageRoot(addr)

	if root1 == root2 {
		t.Error("different storage values should produce different StorageRoot")
	}
}

func TestStorageRootAffectsStateRoot(t *testing.T) {
	// Verify the chain: storage change -> StorageRoot change -> state root change.
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111")
	db.AddBalance(addr, big.NewInt(100))

	stateRootBefore := db.GetRoot()
	storageRootBefore := db.StorageRoot(addr)

	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0x42"))

	stateRootAfter := db.GetRoot()
	storageRootAfter := db.StorageRoot(addr)

	if storageRootBefore == storageRootAfter {
		t.Error("storage change should change StorageRoot")
	}
	if stateRootBefore == stateRootAfter {
		t.Error("StorageRoot change should propagate to state root")
	}
}

func TestStorageRootDeleteSlot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111")
	db.CreateAccount(addr)

	key := types.HexToHash("0x01")
	db.SetState(addr, key, types.HexToHash("0xaa"))

	rootWithSlot := db.StorageRoot(addr)

	// Delete the slot (set to zero).
	db.SetState(addr, key, types.Hash{})

	rootAfterDelete := db.StorageRoot(addr)
	if rootAfterDelete != types.EmptyRootHash {
		t.Errorf("StorageRoot after deleting all slots should be EmptyRootHash, got %s", rootAfterDelete)
	}
	if rootWithSlot == rootAfterDelete {
		t.Error("StorageRoot should change after deleting slot")
	}
}

func TestStorageRootMultipleSlots(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111")
	db.CreateAccount(addr)

	// Add multiple storage slots and verify root is not empty.
	for i := 0; i < 20; i++ {
		var key, val types.Hash
		key[31] = byte(i)
		val[31] = byte(i + 1)
		db.SetState(addr, key, val)
	}

	root := db.StorageRoot(addr)
	if root == types.EmptyRootHash {
		t.Error("StorageRoot should not be empty with 20 slots")
	}

	// Calling again should produce the same result.
	root2 := db.StorageRoot(addr)
	if root != root2 {
		t.Error("StorageRoot should be deterministic on repeated calls")
	}
}

func TestStorageRootIsolatedPerAccount(t *testing.T) {
	db := NewMemoryStateDB()
	addr1 := types.HexToAddress("0x1111")
	addr2 := types.HexToAddress("0x2222")
	db.CreateAccount(addr1)
	db.CreateAccount(addr2)

	// Only set storage on addr1.
	db.SetState(addr1, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	root1 := db.StorageRoot(addr1)
	root2 := db.StorageRoot(addr2)

	if root1 == root2 {
		t.Error("accounts with different storage should have different StorageRoots")
	}
	if root2 != types.EmptyRootHash {
		t.Errorf("account with no storage should have EmptyRootHash, got %s", root2)
	}
}

// --- Trie integration tests ---

func TestEmptyTrieHashMatchesEmptyRootHash(t *testing.T) {
	// Verify that an empty Merkle Patricia Trie produces the same hash as
	// types.EmptyRootHash.
	emptyTrie := trie.New()
	trieHash := emptyTrie.Hash()
	if trieHash != types.EmptyRootHash {
		t.Errorf("empty trie hash %s != EmptyRootHash %s", trieHash, types.EmptyRootHash)
	}
}

func TestStorageRootCommitConsistency(t *testing.T) {
	// Verify that StorageRoot before and after Commit returns the same value
	// when the underlying storage hasn't changed.
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111")
	db.CreateAccount(addr)
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	rootBefore := db.StorageRoot(addr)

	_, err := db.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rootAfter := db.StorageRoot(addr)
	if rootBefore != rootAfter {
		t.Errorf("StorageRoot should be same before and after Commit: %s vs %s", rootBefore, rootAfter)
	}
}

func TestStorageRootDirtyAndCommittedMerge(t *testing.T) {
	// Verify that StorageRoot correctly merges dirty and committed storage.
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111")
	db.CreateAccount(addr)

	// Set and commit a slot.
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	db.Commit()

	rootAfterCommit := db.StorageRoot(addr)

	// Now add a dirty slot (not yet committed).
	db.SetState(addr, types.HexToHash("0x02"), types.HexToHash("0xbb"))

	rootWithDirty := db.StorageRoot(addr)

	if rootAfterCommit == rootWithDirty {
		t.Error("adding dirty storage should change StorageRoot")
	}

	// Overwrite committed slot in dirty storage.
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xcc"))

	rootOverwritten := db.StorageRoot(addr)

	if rootOverwritten == rootWithDirty {
		t.Error("overwriting committed slot in dirty storage should change StorageRoot")
	}
}

// --- TrieBacked StorageRoot tests ---

func TestTrieBackedStorageRootEmpty(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x1111")
	db.CreateAccount(addr)

	root := db.StorageRoot(addr)
	if root != types.EmptyRootHash {
		t.Errorf("TrieBacked StorageRoot of empty account = %s, want EmptyRootHash", root)
	}
}

func TestTrieBackedStorageRootNonExistent(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0xdead")

	root := db.StorageRoot(addr)
	if root != types.EmptyRootHash {
		t.Errorf("TrieBacked StorageRoot of non-existent account = %s, want EmptyRootHash", root)
	}
}

func TestTrieBackedStorageRootWithSlots(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x1111")
	db.CreateAccount(addr)
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	root := db.StorageRoot(addr)
	if root == types.EmptyRootHash {
		t.Error("TrieBacked StorageRoot should not be empty with storage")
	}
}

func TestTrieBackedStorageRootDeterministic(t *testing.T) {
	db1 := NewTrieBackedStateDB()
	db2 := NewTrieBackedStateDB()

	addr := types.HexToAddress("0x1111")
	key1 := types.HexToHash("0x01")
	key2 := types.HexToHash("0x02")
	val1 := types.HexToHash("0xaa")
	val2 := types.HexToHash("0xbb")

	db1.CreateAccount(addr)
	db2.CreateAccount(addr)

	// Insert in different order.
	db1.SetState(addr, key1, val1)
	db1.SetState(addr, key2, val2)

	db2.SetState(addr, key2, val2)
	db2.SetState(addr, key1, val1)

	root1 := db1.StorageRoot(addr)
	root2 := db2.StorageRoot(addr)

	if root1 != root2 {
		t.Errorf("TrieBacked StorageRoot should be deterministic: %s vs %s", root1, root2)
	}
}

func TestTrieBackedStorageRootAffectsStateRoot(t *testing.T) {
	db := NewTrieBackedStateDB()
	addr := types.HexToAddress("0x1111")
	db.AddBalance(addr, big.NewInt(100))

	stateRootBefore := db.GetRoot()
	storageRootBefore := db.StorageRoot(addr)

	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0x42"))

	stateRootAfter := db.GetRoot()
	storageRootAfter := db.StorageRoot(addr)

	if storageRootBefore == storageRootAfter {
		t.Error("storage change should change TrieBacked StorageRoot")
	}
	if stateRootBefore == stateRootAfter {
		t.Error("StorageRoot change should propagate to TrieBacked state root")
	}
}

// --- Cross-implementation consistency ---

func TestMemoryAndTrieBackedStorageRootConsistency(t *testing.T) {
	// The MemoryStateDB and TrieBackedStateDB use different storage trie
	// encoding (MemoryStateDB uses raw bytes, TrieBackedStateDB uses
	// Keccak256(slot) keys and RLP-encoded values). We verify that both
	// produce non-empty roots for the same state and that each is internally
	// consistent.
	mem := NewMemoryStateDB()
	trieBacked := NewTrieBackedStateDB()

	addr := types.HexToAddress("0x1111")
	key := types.HexToHash("0x01")
	val := types.HexToHash("0x42")

	mem.CreateAccount(addr)
	trieBacked.CreateAccount(addr)

	mem.SetState(addr, key, val)
	trieBacked.SetState(addr, key, val)

	memRoot := mem.StorageRoot(addr)
	trieRoot := trieBacked.StorageRoot(addr)

	// Both should produce non-empty roots.
	if memRoot == types.EmptyRootHash {
		t.Error("MemoryStateDB StorageRoot should not be empty with storage")
	}
	if trieRoot == types.EmptyRootHash {
		t.Error("TrieBackedStateDB StorageRoot should not be empty with storage")
	}

	// Note: they will typically differ because of different key/value encoding
	// (raw vs Keccak256/RLP), which is expected. We just verify both work.
}

func TestConsistentHashingSameStateSameRoot(t *testing.T) {
	// Verify that creating the exact same state produces the exact same root,
	// even when rebuilt from scratch.
	makeState := func() *MemoryStateDB {
		db := NewMemoryStateDB()
		for i := 0; i < 5; i++ {
			var addr types.Address
			addr[19] = byte(i + 1)
			db.AddBalance(addr, big.NewInt(int64(i+1)*100))
			db.SetNonce(addr, uint64(i*10))
			for j := 0; j < 3; j++ {
				var key, val types.Hash
				key[31] = byte(j)
				val[31] = byte(j + 10)
				db.SetState(addr, key, val)
			}
			if i%2 == 0 {
				db.SetCode(addr, []byte{0x60, byte(i), 0xf3})
			}
		}
		return db
	}

	db1 := makeState()
	db2 := makeState()

	root1 := db1.GetRoot()
	root2 := db2.GetRoot()

	if root1 != root2 {
		t.Errorf("identical states should produce identical roots: %s vs %s", root1, root2)
	}

	// Also verify storage roots match.
	for i := 0; i < 5; i++ {
		var addr types.Address
		addr[19] = byte(i + 1)
		sr1 := db1.StorageRoot(addr)
		sr2 := db2.StorageRoot(addr)
		if sr1 != sr2 {
			t.Errorf("StorageRoot mismatch for account %d: %s vs %s", i, sr1, sr2)
		}
	}
}
