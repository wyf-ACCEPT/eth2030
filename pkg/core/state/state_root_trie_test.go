package state

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
	"github.com/eth2028/eth2028/rlp"
)

// --- State root determinism tests ---

// TestStateRootDeterminismDifferentCreationOrder verifies that the same state
// produces the same root regardless of the order accounts are created.
func TestStateRootDeterminismDifferentCreationOrder(t *testing.T) {
	addrs := make([]types.Address, 20)
	for i := range addrs {
		addrs[i][19] = byte(i + 1)
		addrs[i][0] = byte(i + 100)
	}

	// Create state in forward order.
	db1 := NewMemoryStateDB()
	for i, addr := range addrs {
		db1.AddBalance(addr, big.NewInt(int64(i+1)*100))
		db1.SetNonce(addr, uint64(i*5))
	}

	// Create state in reverse order.
	db2 := NewMemoryStateDB()
	for i := len(addrs) - 1; i >= 0; i-- {
		addr := addrs[i]
		db2.AddBalance(addr, big.NewInt(int64(i+1)*100))
		db2.SetNonce(addr, uint64(i*5))
	}

	root1 := db1.GetRoot()
	root2 := db2.GetRoot()

	if root1 != root2 {
		t.Errorf("roots differ for same state in different order: %s vs %s", root1, root2)
	}
}

// TestStateRootDeterminismRebuiltFromScratch verifies that completely
// rebuilding the same state from scratch produces the same root.
func TestStateRootDeterminismRebuiltFromScratch(t *testing.T) {
	buildState := func() types.Hash {
		db := NewMemoryStateDB()
		for i := 0; i < 10; i++ {
			var addr types.Address
			addr[19] = byte(i + 1)
			db.AddBalance(addr, big.NewInt(int64(i)*1000+500))
			db.SetNonce(addr, uint64(i*3))
			if i%2 == 0 {
				db.SetCode(addr, []byte{0x60, byte(i), 0xf3})
			}
			for j := 0; j < 5; j++ {
				var key, val types.Hash
				key[31] = byte(j)
				val[31] = byte(j + 42)
				db.SetState(addr, key, val)
			}
		}
		return db.GetRoot()
	}

	root1 := buildState()
	root2 := buildState()
	root3 := buildState()

	if root1 != root2 || root2 != root3 {
		t.Errorf("rebuilt states should produce same root: %s, %s, %s", root1, root2, root3)
	}
}

// TestStateRootChangesWithAnyModification checks that any change to any
// account field produces a different root.
func TestStateRootChangesWithAnyModification(t *testing.T) {
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	baseState := func() *MemoryStateDB {
		db := NewMemoryStateDB()
		db.CreateAccount(addr)
		db.AddBalance(addr, big.NewInt(1000))
		db.SetNonce(addr, 5)
		db.SetCode(addr, []byte{0x60, 0x00, 0xf3})
		db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))
		return db
	}

	baseRoot := baseState().GetRoot()

	// Change balance.
	db := baseState()
	db.AddBalance(addr, big.NewInt(1))
	if db.GetRoot() == baseRoot {
		t.Error("balance change should produce different root")
	}

	// Change nonce.
	db = baseState()
	db.SetNonce(addr, 6)
	if db.GetRoot() == baseRoot {
		t.Error("nonce change should produce different root")
	}

	// Change code.
	db = baseState()
	db.SetCode(addr, []byte{0x60, 0x01, 0xf3})
	if db.GetRoot() == baseRoot {
		t.Error("code change should produce different root")
	}

	// Change storage.
	db = baseState()
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xbb"))
	if db.GetRoot() == baseRoot {
		t.Error("storage value change should produce different root")
	}

	// Add storage slot.
	db = baseState()
	db.SetState(addr, types.HexToHash("0x02"), types.HexToHash("0xcc"))
	if db.GetRoot() == baseRoot {
		t.Error("new storage slot should produce different root")
	}

	// Remove storage slot.
	db = baseState()
	db.SetState(addr, types.HexToHash("0x01"), types.Hash{})
	if db.GetRoot() == baseRoot {
		t.Error("removing storage slot should produce different root")
	}
}

// --- Snapshot and revert preserve correct state root ---

func TestSnapshotRevertPreservesRoot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	db.AddBalance(addr, big.NewInt(1000))
	db.SetNonce(addr, 5)
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	rootBefore := db.GetRoot()

	snap := db.Snapshot()

	// Make various changes.
	db.AddBalance(addr, big.NewInt(500))
	db.SetNonce(addr, 10)
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xbb"))
	db.SetState(addr, types.HexToHash("0x02"), types.HexToHash("0xcc"))
	db.SetCode(addr, []byte{0x60, 0x01})

	rootChanged := db.GetRoot()
	if rootBefore == rootChanged {
		t.Error("root should change after modifications")
	}

	// Revert.
	db.RevertToSnapshot(snap)

	rootAfterRevert := db.GetRoot()
	if rootBefore != rootAfterRevert {
		t.Errorf("root should be restored after revert: before=%s, after=%s",
			rootBefore, rootAfterRevert)
	}
}

func TestNestedSnapshotRevertPreservesRoot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	db.AddBalance(addr, big.NewInt(1000))

	rootInitial := db.GetRoot()

	snap1 := db.Snapshot()
	db.AddBalance(addr, big.NewInt(100))
	rootAfterSnap1 := db.GetRoot()

	snap2 := db.Snapshot()
	db.AddBalance(addr, big.NewInt(200))
	rootAfterSnap2 := db.GetRoot()

	// All three roots should differ.
	if rootInitial == rootAfterSnap1 || rootAfterSnap1 == rootAfterSnap2 || rootInitial == rootAfterSnap2 {
		t.Error("each state change should produce a different root")
	}

	// Revert snap2: should restore to rootAfterSnap1.
	db.RevertToSnapshot(snap2)
	if db.GetRoot() != rootAfterSnap1 {
		t.Errorf("after revert snap2: expected %s, got %s", rootAfterSnap1, db.GetRoot())
	}

	// Revert snap1: should restore to rootInitial.
	db.RevertToSnapshot(snap1)
	if db.GetRoot() != rootInitial {
		t.Errorf("after revert snap1: expected %s, got %s", rootInitial, db.GetRoot())
	}
}

func TestSnapshotRevertWithAccountCreationAndDeletion(t *testing.T) {
	db := NewMemoryStateDB()
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	db.AddBalance(addr1, big.NewInt(100))
	rootBefore := db.GetRoot()

	snap := db.Snapshot()
	db.CreateAccount(addr2)
	db.AddBalance(addr2, big.NewInt(500))

	if db.GetRoot() == rootBefore {
		t.Error("creating new account should change root")
	}

	db.RevertToSnapshot(snap)
	if db.GetRoot() != rootBefore {
		t.Errorf("root not restored after reverting account creation: %s vs %s",
			rootBefore, db.GetRoot())
	}
}

// --- Account RLP encoding tests ---

func TestAccountRLPEncodingFormat(t *testing.T) {
	// Verify the RLP encoding of an account matches the expected format:
	// [nonce, balance, storageRoot, codeHash]
	acc := rlpAccount{
		Nonce:    0,
		Balance:  new(big.Int),
		Root:     types.EmptyRootHash.Bytes(),
		CodeHash: types.EmptyCodeHash.Bytes(),
	}

	encoded, err := rlp.EncodeToBytes(acc)
	if err != nil {
		t.Fatalf("RLP encode error: %v", err)
	}

	// Should be a valid RLP list.
	if len(encoded) == 0 {
		t.Fatal("encoded account should not be empty")
	}

	// First byte should indicate a list (>= 0xc0).
	if encoded[0] < 0xc0 {
		t.Fatalf("encoded account should be an RLP list, got first byte 0x%02x", encoded[0])
	}
}

func TestAccountRLPEncodingDeterministic(t *testing.T) {
	acc := rlpAccount{
		Nonce:    42,
		Balance:  big.NewInt(1000000),
		Root:     types.EmptyRootHash.Bytes(),
		CodeHash: types.EmptyCodeHash.Bytes(),
	}

	enc1, err1 := rlp.EncodeToBytes(acc)
	enc2, err2 := rlp.EncodeToBytes(acc)

	if err1 != nil || err2 != nil {
		t.Fatalf("RLP encode errors: %v, %v", err1, err2)
	}

	if len(enc1) != len(enc2) {
		t.Fatal("same account should produce same encoding length")
	}
	for i := range enc1 {
		if enc1[i] != enc2[i] {
			t.Fatalf("encoding mismatch at byte %d: 0x%02x vs 0x%02x", i, enc1[i], enc2[i])
		}
	}
}

func TestAccountRLPEncodingNonceAffectsEncoding(t *testing.T) {
	base := rlpAccount{
		Nonce:    0,
		Balance:  big.NewInt(100),
		Root:     types.EmptyRootHash.Bytes(),
		CodeHash: types.EmptyCodeHash.Bytes(),
	}
	modified := rlpAccount{
		Nonce:    1,
		Balance:  big.NewInt(100),
		Root:     types.EmptyRootHash.Bytes(),
		CodeHash: types.EmptyCodeHash.Bytes(),
	}

	enc1, _ := rlp.EncodeToBytes(base)
	enc2, _ := rlp.EncodeToBytes(modified)

	if bytesEqual(enc1, enc2) {
		t.Error("different nonces should produce different encodings")
	}
}

func TestAccountRLPEncodingBalanceAffectsEncoding(t *testing.T) {
	base := rlpAccount{
		Nonce:    0,
		Balance:  big.NewInt(100),
		Root:     types.EmptyRootHash.Bytes(),
		CodeHash: types.EmptyCodeHash.Bytes(),
	}
	modified := rlpAccount{
		Nonce:    0,
		Balance:  big.NewInt(200),
		Root:     types.EmptyRootHash.Bytes(),
		CodeHash: types.EmptyCodeHash.Bytes(),
	}

	enc1, _ := rlp.EncodeToBytes(base)
	enc2, _ := rlp.EncodeToBytes(modified)

	if bytesEqual(enc1, enc2) {
		t.Error("different balances should produce different encodings")
	}
}

func TestAccountRLPEncodingStorageRootAffectsEncoding(t *testing.T) {
	root1 := types.EmptyRootHash.Bytes()
	root2 := types.HexToHash("0xaabbccdd").Bytes()

	enc1, _ := rlp.EncodeToBytes(rlpAccount{
		Nonce: 0, Balance: new(big.Int), Root: root1,
		CodeHash: types.EmptyCodeHash.Bytes(),
	})
	enc2, _ := rlp.EncodeToBytes(rlpAccount{
		Nonce: 0, Balance: new(big.Int), Root: root2,
		CodeHash: types.EmptyCodeHash.Bytes(),
	})

	if bytesEqual(enc1, enc2) {
		t.Error("different storage roots should produce different encodings")
	}
}

func TestAccountRLPEncodingCodeHashAffectsEncoding(t *testing.T) {
	code1 := types.EmptyCodeHash.Bytes()
	code2 := crypto.Keccak256([]byte{0x60, 0x00})

	enc1, _ := rlp.EncodeToBytes(rlpAccount{
		Nonce: 0, Balance: new(big.Int),
		Root: types.EmptyRootHash.Bytes(), CodeHash: code1,
	})
	enc2, _ := rlp.EncodeToBytes(rlpAccount{
		Nonce: 0, Balance: new(big.Int),
		Root: types.EmptyRootHash.Bytes(), CodeHash: code2,
	})

	if bytesEqual(enc1, enc2) {
		t.Error("different code hashes should produce different encodings")
	}
}

func TestAccountRLPEncodingLargeBalance(t *testing.T) {
	// Test with a large balance (> 64 bits) to ensure proper big.Int encoding.
	largeBalance := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil) // 10^30

	acc := rlpAccount{
		Nonce:    1,
		Balance:  largeBalance,
		Root:     types.EmptyRootHash.Bytes(),
		CodeHash: types.EmptyCodeHash.Bytes(),
	}

	encoded, err := rlp.EncodeToBytes(acc)
	if err != nil {
		t.Fatalf("RLP encode error for large balance: %v", err)
	}

	if len(encoded) == 0 {
		t.Fatal("encoded account with large balance should not be empty")
	}

	// RLP of big.Int should be its big-endian bytes without leading zeros.
	// 10^30 in hex is about 13 bytes, so the total encoding should be substantial.
	if len(encoded) < 50 {
		t.Fatalf("encoding seems too short for large balance: %d bytes", len(encoded))
	}
}

// --- MemoryStateDB and TrieBackedStateDB root consistency ---

func TestMemoryAndTrieBackedRootsMatch(t *testing.T) {
	// Since we now use the same encoding in both, the roots should match.
	mem := NewMemoryStateDB()
	trieBacked := NewTrieBackedStateDB()

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	mem.AddBalance(addr, big.NewInt(1000))
	mem.SetNonce(addr, 5)
	mem.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	trieBacked.AddBalance(addr, big.NewInt(1000))
	trieBacked.SetNonce(addr, 5)
	trieBacked.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	memRoot := mem.GetRoot()
	trieRoot := trieBacked.GetRoot()

	if memRoot != trieRoot {
		t.Errorf("MemoryStateDB and TrieBackedStateDB roots should now match: %s vs %s",
			memRoot, trieRoot)
	}
}

func TestMemoryAndTrieBackedCommitRootsMatch(t *testing.T) {
	mem := NewMemoryStateDB()
	trieBacked := NewTrieBackedStateDB()

	for i := 0; i < 5; i++ {
		var addr types.Address
		addr[19] = byte(i + 1)

		mem.AddBalance(addr, big.NewInt(int64(i+1)*1000))
		mem.SetNonce(addr, uint64(i))
		trieBacked.AddBalance(addr, big.NewInt(int64(i+1)*1000))
		trieBacked.SetNonce(addr, uint64(i))

		for j := 0; j < 3; j++ {
			var key, val types.Hash
			key[31] = byte(j)
			val[31] = byte(j + 10)
			mem.SetState(addr, key, val)
			trieBacked.SetState(addr, key, val)
		}
	}

	memRoot, err1 := mem.Commit()
	trieRoot, err2 := trieBacked.Commit()

	if err1 != nil || err2 != nil {
		t.Fatalf("Commit errors: %v, %v", err1, err2)
	}

	if memRoot != trieRoot {
		t.Errorf("Commit roots should match: mem=%s, trie=%s", memRoot, trieRoot)
	}
}

func TestMemoryAndTrieBackedStorageRootsMatch(t *testing.T) {
	mem := NewMemoryStateDB()
	trieBacked := NewTrieBackedStateDB()

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	mem.CreateAccount(addr)
	trieBacked.CreateAccount(addr)

	for j := 0; j < 10; j++ {
		var key, val types.Hash
		key[31] = byte(j)
		val[31] = byte(j + 42)
		mem.SetState(addr, key, val)
		trieBacked.SetState(addr, key, val)
	}

	memStorageRoot := mem.StorageRoot(addr)
	trieStorageRoot := trieBacked.StorageRoot(addr)

	if memStorageRoot != trieStorageRoot {
		t.Errorf("storage roots should match: mem=%s, trie=%s", memStorageRoot, trieStorageRoot)
	}
}

// --- Commit finalization tests ---

func TestCommitFlushesAndReturnsDeterministicRoot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	db.AddBalance(addr, big.NewInt(500))
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	// GetRoot before commit should match Commit result.
	rootBefore := db.GetRoot()
	committedRoot, err := db.Commit()
	if err != nil {
		t.Fatalf("Commit error: %v", err)
	}
	if rootBefore != committedRoot {
		t.Errorf("GetRoot %s != Commit %s", rootBefore, committedRoot)
	}

	// After commit, dirty storage should be flushed to committed.
	if db.GetCommittedState(addr, types.HexToHash("0x01")) != types.HexToHash("0xaa") {
		t.Error("committed state should contain flushed value")
	}

	// GetRoot after commit should still be the same.
	if db.GetRoot() != committedRoot {
		t.Errorf("GetRoot after commit should match: %s vs %s", db.GetRoot(), committedRoot)
	}
}

func TestCommitTwiceProducesSameRoot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	db.AddBalance(addr, big.NewInt(100))
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0x42"))

	root1, _ := db.Commit()
	root2, _ := db.Commit()

	if root1 != root2 {
		t.Errorf("committing twice without changes should produce same root: %s vs %s",
			root1, root2)
	}
}

func TestCommitThenModifyChangesRoot(t *testing.T) {
	db := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	db.AddBalance(addr, big.NewInt(100))
	root1, _ := db.Commit()

	db.AddBalance(addr, big.NewInt(1))
	root2, _ := db.Commit()

	if root1 == root2 {
		t.Error("commit after modification should produce different root")
	}
}

// --- trimLeadingZeros tests ---

func TestTrimLeadingZeros(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{"no zeros", []byte{1, 2, 3}, []byte{1, 2, 3}},
		{"leading zeros", []byte{0, 0, 1, 2}, []byte{1, 2}},
		{"all zeros", []byte{0, 0, 0}, []byte{}},
		{"single non-zero", []byte{0, 0, 5}, []byte{5}},
		{"single zero", []byte{0}, []byte{}},
		{"empty", []byte{}, []byte{}},
		{"no leading zeros", []byte{0xff, 0xaa}, []byte{0xff, 0xaa}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := trimLeadingZeros(tt.input)
			if !bytesEqual(result, tt.expected) {
				t.Errorf("trimLeadingZeros(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// --- Storage trie encoding tests ---

func TestStorageTrieUsesKeccakHashedKeys(t *testing.T) {
	// Verify that two different slot addresses that hash to the same
	// position produce conflicting roots (they shouldn't since different
	// slot values hash differently).
	db1 := NewMemoryStateDB()
	db2 := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	db1.CreateAccount(addr)
	db2.CreateAccount(addr)

	// Two different keys should produce different storage roots.
	db1.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	db2.SetState(addr, types.HexToHash("0x02"), types.HexToHash("0xaa"))

	root1 := db1.StorageRoot(addr)
	root2 := db2.StorageRoot(addr)

	if root1 == root2 {
		t.Error("different storage keys should produce different storage roots")
	}
}

func TestStorageTrieRLPEncodesValues(t *testing.T) {
	// Verify that the storage value encoding is consistent: the same
	// value at the same key always produces the same root.
	db1 := NewMemoryStateDB()
	db2 := NewMemoryStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	key := types.HexToHash("0x01")
	val := types.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000ff")

	db1.CreateAccount(addr)
	db2.CreateAccount(addr)

	db1.SetState(addr, key, val)
	db2.SetState(addr, key, val)

	root1 := db1.StorageRoot(addr)
	root2 := db2.StorageRoot(addr)

	if root1 != root2 {
		t.Errorf("same storage should produce same root: %s vs %s", root1, root2)
	}
}

// --- State root with code ---

func TestStateRootWithCodeAndWithout(t *testing.T) {
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	db1 := NewMemoryStateDB()
	db1.CreateAccount(addr)
	db1.AddBalance(addr, big.NewInt(100))

	db2 := NewMemoryStateDB()
	db2.CreateAccount(addr)
	db2.AddBalance(addr, big.NewInt(100))
	db2.SetCode(addr, []byte{0x60, 0x00, 0xf3})

	root1 := db1.GetRoot()
	root2 := db2.GetRoot()

	if root1 == root2 {
		t.Error("account with code should have different root than without")
	}
}

// --- Helper ---

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
