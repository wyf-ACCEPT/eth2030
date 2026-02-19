package trie

import (
	"bytes"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// --- AnnounceBinaryTrie tests ---

func TestAnnounceBinaryTrie_NewEmpty(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	if tr.Len() != 0 {
		t.Fatalf("Len = %d, want 0", tr.Len())
	}
	root := tr.Root()
	if root != (types.Hash{}) {
		t.Fatalf("empty root = %s, want zero hash", root)
	}
}

func TestAnnounceBinaryTrie_InsertGet(t *testing.T) {
	tr := NewAnnounceBinaryTrie()

	if err := tr.Insert([]byte("key1"), []byte("val1")); err != nil {
		t.Fatalf("Insert error: %v", err)
	}

	val, ok := tr.Get([]byte("key1"))
	if !ok {
		t.Fatal("Get returned not found")
	}
	if string(val) != "val1" {
		t.Fatalf("Get = %q, want %q", val, "val1")
	}
}

func TestAnnounceBinaryTrie_GetNotFound(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("key1"), []byte("val1"))

	_, ok := tr.Get([]byte("nonexistent"))
	if ok {
		t.Fatal("expected not found for nonexistent key")
	}
}

func TestAnnounceBinaryTrie_GetNilKey(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	_, ok := tr.Get(nil)
	if ok {
		t.Fatal("expected not found for nil key")
	}
}

func TestAnnounceBinaryTrie_InsertNilKey(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	err := tr.Insert(nil, []byte("val"))
	if err == nil {
		t.Fatal("expected error for nil key")
	}
}

func TestAnnounceBinaryTrie_Update(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("key"), []byte("val1"))
	tr.Insert([]byte("key"), []byte("val2"))

	if tr.Len() != 1 {
		t.Fatalf("Len = %d after update, want 1", tr.Len())
	}

	val, ok := tr.Get([]byte("key"))
	if !ok {
		t.Fatal("key not found after update")
	}
	if string(val) != "val2" {
		t.Fatalf("Get = %q, want %q", val, "val2")
	}
}

func TestAnnounceBinaryTrie_Delete(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("a"), []byte("1"))
	tr.Insert([]byte("b"), []byte("2"))
	tr.Insert([]byte("c"), []byte("3"))

	if tr.Len() != 3 {
		t.Fatalf("Len = %d, want 3", tr.Len())
	}

	ok := tr.Delete([]byte("b"))
	if !ok {
		t.Fatal("Delete returned false for existing key")
	}
	if tr.Len() != 2 {
		t.Fatalf("Len = %d after delete, want 2", tr.Len())
	}

	_, found := tr.Get([]byte("b"))
	if found {
		t.Fatal("key b should not exist after delete")
	}

	// Verify remaining keys.
	v, ok := tr.Get([]byte("a"))
	if !ok || string(v) != "1" {
		t.Fatal("key a not found or wrong value")
	}
	v, ok = tr.Get([]byte("c"))
	if !ok || string(v) != "3" {
		t.Fatal("key c not found or wrong value")
	}
}

func TestAnnounceBinaryTrie_DeleteNonexistent(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("a"), []byte("1"))

	ok := tr.Delete([]byte("b"))
	if ok {
		t.Fatal("Delete returned true for nonexistent key")
	}
	if tr.Len() != 1 {
		t.Fatalf("Len changed after failed delete: %d", tr.Len())
	}
}

func TestAnnounceBinaryTrie_DeleteNilKey(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	ok := tr.Delete(nil)
	if ok {
		t.Fatal("Delete returned true for nil key")
	}
}

func TestAnnounceBinaryTrie_DeleteAll(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("x"), []byte("1"))
	tr.Insert([]byte("y"), []byte("2"))

	tr.Delete([]byte("x"))
	tr.Delete([]byte("y"))

	if tr.Len() != 0 {
		t.Fatalf("Len = %d after deleting all, want 0", tr.Len())
	}
	if tr.Root() != (types.Hash{}) {
		t.Fatal("root should be zero hash for empty trie")
	}
}

func TestAnnounceBinaryTrie_RootDeterministic(t *testing.T) {
	// Same keys inserted in different order should produce the same root.
	tr1 := NewAnnounceBinaryTrie()
	tr1.Insert([]byte("alpha"), []byte("one"))
	tr1.Insert([]byte("beta"), []byte("two"))
	tr1.Insert([]byte("gamma"), []byte("three"))

	tr2 := NewAnnounceBinaryTrie()
	tr2.Insert([]byte("gamma"), []byte("three"))
	tr2.Insert([]byte("alpha"), []byte("one"))
	tr2.Insert([]byte("beta"), []byte("two"))

	if tr1.Root() != tr2.Root() {
		t.Fatalf("roots differ: %s vs %s", tr1.Root(), tr2.Root())
	}
}

func TestAnnounceBinaryTrie_RootChangesOnMutation(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("key"), []byte("val1"))
	root1 := tr.Root()

	tr.Insert([]byte("key"), []byte("val2"))
	root2 := tr.Root()

	if root1 == root2 {
		t.Fatal("root should change after value update")
	}
}

func TestAnnounceBinaryTrie_ManyKeys(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	n := 200
	for i := range n {
		key := crypto.Keccak256([]byte{byte(i), byte(i >> 8)})
		val := []byte{byte(i)}
		tr.Insert(key, val)
	}

	if tr.Len() != n {
		t.Fatalf("Len = %d, want %d", tr.Len(), n)
	}

	// Verify all present.
	for i := range n {
		key := crypto.Keccak256([]byte{byte(i), byte(i >> 8)})
		val, ok := tr.Get(key)
		if !ok {
			t.Fatalf("key %d not found", i)
		}
		if len(val) != 1 || val[0] != byte(i) {
			t.Fatalf("key %d: wrong value", i)
		}
	}

	root := tr.Root()
	if root == (types.Hash{}) {
		t.Fatal("root should not be zero for non-empty trie")
	}
}

// --- Proof tests ---

func TestAnnounceBinaryTrie_ProveVerify(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("alice"), []byte("data_alice"))
	tr.Insert([]byte("bob"), []byte("data_bob"))
	tr.Insert([]byte("carol"), []byte("data_carol"))

	root := tr.Root()

	// Prove and verify each key.
	for _, key := range []string{"alice", "bob", "carol"} {
		proof, err := tr.Prove([]byte(key))
		if err != nil {
			t.Fatalf("Prove(%s) error: %v", key, err)
		}
		if !bytes.Equal(proof.Key, []byte(key)) {
			t.Errorf("proof.Key = %q, want %q", proof.Key, key)
		}
		if !VerifyAnnounceProof(root, []byte(key), proof) {
			t.Errorf("VerifyAnnounceProof(%s) failed", key)
		}
	}
}

func TestAnnounceBinaryTrie_ProveNotFound(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("exists"), []byte("val"))

	_, err := tr.Prove([]byte("missing"))
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestAnnounceBinaryTrie_ProveNilKey(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	_, err := tr.Prove(nil)
	if err == nil {
		t.Fatal("expected error for nil key")
	}
}

func TestAnnounceBinaryTrie_ProveEmptyTrie(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	_, err := tr.Prove([]byte("key"))
	if err == nil {
		t.Fatal("expected error for proof in empty trie")
	}
}

func TestVerifyAnnounceProof_InvalidRoot(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("key"), []byte("val"))

	proof, _ := tr.Prove([]byte("key"))
	wrongRoot := types.BytesToHash([]byte{0xff, 0xee, 0xdd})

	if VerifyAnnounceProof(wrongRoot, []byte("key"), proof) {
		t.Fatal("proof should fail against wrong root")
	}
}

func TestVerifyAnnounceProof_NilProof(t *testing.T) {
	if VerifyAnnounceProof(types.Hash{}, []byte("key"), nil) {
		t.Fatal("nil proof should not verify")
	}
}

func TestVerifyAnnounceProof_NilKey(t *testing.T) {
	proof := &BinaryProofAnnounce{Key: []byte("k"), Value: []byte("v")}
	if VerifyAnnounceProof(types.Hash{}, nil, proof) {
		t.Fatal("nil key should not verify")
	}
}

func TestVerifyAnnounceProof_TamperedValue(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("key"), []byte("original"))
	root := tr.Root()

	proof, _ := tr.Prove([]byte("key"))
	proof.Value = []byte("tampered")

	if VerifyAnnounceProof(root, []byte("key"), proof) {
		t.Fatal("tampered proof should not verify")
	}
}

func TestVerifyAnnounceProof_SingleKey(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("only"), []byte("one"))
	root := tr.Root()

	proof, err := tr.Prove([]byte("only"))
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}
	if len(proof.Path) != 0 {
		t.Fatalf("single-key trie proof should have empty path, got %d", len(proof.Path))
	}
	if !VerifyAnnounceProof(root, []byte("only"), proof) {
		t.Fatal("single-key proof should verify")
	}
}

// --- BinaryNode export tests ---

func TestAnnounceBinaryTrie_ExportBinaryNode(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("k1"), []byte("v1"))
	tr.Insert([]byte("k2"), []byte("v2"))

	bn := tr.ExportBinaryNode()
	if bn == nil {
		t.Fatal("ExportBinaryNode returned nil for non-empty trie")
	}
	if bn.Hash == (types.Hash{}) {
		t.Fatal("exported root hash should not be zero")
	}
}

func TestAnnounceBinaryTrie_ExportEmpty(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	bn := tr.ExportBinaryNode()
	if bn != nil {
		t.Fatal("ExportBinaryNode should return nil for empty trie")
	}
}

// --- AnnouncementSet tests ---

func TestAnnouncementSet_New(t *testing.T) {
	as := NewAnnouncementSet()
	if as.Size() != 0 {
		t.Fatalf("Size = %d, want 0", as.Size())
	}
}

func TestAnnouncementSet_AddStateChange(t *testing.T) {
	as := NewAnnouncementSet()

	addr := types.BytesToAddress([]byte{0x01, 0x02, 0x03})
	slot := types.BytesToHash([]byte{0x10})
	oldVal := types.BytesToHash([]byte{0x00})
	newVal := types.BytesToHash([]byte{0xff})

	as.AddStateChange(addr, slot, oldVal, newVal)
	if as.Size() != 1 {
		t.Fatalf("Size = %d, want 1", as.Size())
	}
}

func TestAnnouncementSet_MultipleChanges(t *testing.T) {
	as := NewAnnouncementSet()

	for i := range 10 {
		addr := types.BytesToAddress([]byte{byte(i)})
		slot := types.BytesToHash([]byte{byte(i + 1)})
		old := types.BytesToHash([]byte{byte(i)})
		new := types.BytesToHash([]byte{byte(i + 10)})
		as.AddStateChange(addr, slot, old, new)
	}

	if as.Size() != 10 {
		t.Fatalf("Size = %d, want 10", as.Size())
	}
}

func TestAnnouncementSet_BuildAnnouncementTree(t *testing.T) {
	as := NewAnnouncementSet()

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})
	slot := types.BytesToHash([]byte{0x10})

	as.AddStateChange(addr1, slot, types.Hash{}, types.BytesToHash([]byte{0xaa}))
	as.AddStateChange(addr2, slot, types.Hash{}, types.BytesToHash([]byte{0xbb}))

	tree := as.BuildAnnouncementTree()
	if tree == nil {
		t.Fatal("BuildAnnouncementTree returned nil")
	}
	if tree.Len() != 2 {
		t.Fatalf("tree Len = %d, want 2", tree.Len())
	}

	root := tree.Root()
	if root == (types.Hash{}) {
		t.Fatal("tree root should not be zero")
	}

	// Verify we can retrieve each entry.
	key1 := make([]byte, 52) // addr(20) + slot(32)
	copy(key1[:20], addr1[:])
	copy(key1[20:], slot[:])

	val, ok := tree.Get(key1)
	if !ok {
		t.Fatal("key1 not found in tree")
	}
	if len(val) != 64 {
		t.Fatalf("value length = %d, want 64", len(val))
	}
	// Check newVal portion (last 32 bytes).
	expectedNewVal := types.BytesToHash([]byte{0xaa})
	var gotNewVal types.Hash
	copy(gotNewVal[:], val[32:])
	if gotNewVal != expectedNewVal {
		t.Errorf("newVal mismatch: got %s, want %s", gotNewVal, expectedNewVal)
	}
}

func TestAnnouncementSet_BuildEmpty(t *testing.T) {
	as := NewAnnouncementSet()
	tree := as.BuildAnnouncementTree()
	if tree.Len() != 0 {
		t.Fatalf("empty tree Len = %d, want 0", tree.Len())
	}
	if tree.Root() != (types.Hash{}) {
		t.Fatal("empty tree root should be zero hash")
	}
}

func TestAnnouncementSet_DuplicateKeyOverwrites(t *testing.T) {
	as := NewAnnouncementSet()

	addr := types.BytesToAddress([]byte{0x01})
	slot := types.BytesToHash([]byte{0x10})

	// Two changes to the same addr+slot.
	as.AddStateChange(addr, slot, types.Hash{}, types.BytesToHash([]byte{0x11}))
	as.AddStateChange(addr, slot, types.BytesToHash([]byte{0x11}), types.BytesToHash([]byte{0x22}))

	tree := as.BuildAnnouncementTree()
	// The second insert overwrites the first.
	if tree.Len() != 1 {
		t.Fatalf("tree Len = %d, want 1 (overwrite)", tree.Len())
	}

	key := make([]byte, 52)
	copy(key[:20], addr[:])
	copy(key[20:], slot[:])

	val, ok := tree.Get(key)
	if !ok {
		t.Fatal("key not found")
	}
	var gotNew types.Hash
	copy(gotNew[:], val[32:])
	expected := types.BytesToHash([]byte{0x22})
	if gotNew != expected {
		t.Errorf("expected latest newVal %s, got %s", expected, gotNew)
	}
}

func TestAnnouncementSet_ProveFromTree(t *testing.T) {
	as := NewAnnouncementSet()

	addr := types.BytesToAddress([]byte{0xaa, 0xbb, 0xcc})
	slot := types.BytesToHash([]byte{0x01})
	as.AddStateChange(addr, slot, types.Hash{}, types.BytesToHash([]byte{0xff}))

	tree := as.BuildAnnouncementTree()
	root := tree.Root()

	key := make([]byte, 52)
	copy(key[:20], addr[:])
	copy(key[20:], slot[:])

	proof, err := tree.Prove(key)
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}

	if !VerifyAnnounceProof(root, key, proof) {
		t.Fatal("proof from announcement tree should verify")
	}
}

// --- Thread safety ---

func TestAnnounceBinaryTrie_ThreadSafety(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := []byte{byte(id), byte(id >> 8)}
			val := []byte{byte(id)}
			tr.Insert(key, val)
			tr.Get(key)
			tr.Root()
		}(i)
	}
	wg.Wait()

	if tr.Len() != 50 {
		t.Fatalf("Len = %d, want 50", tr.Len())
	}
}

func TestAnnouncementSet_ThreadSafety(t *testing.T) {
	as := NewAnnouncementSet()
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			addr := types.BytesToAddress([]byte{byte(id)})
			slot := types.BytesToHash([]byte{byte(id)})
			as.AddStateChange(addr, slot, types.Hash{}, types.BytesToHash([]byte{byte(id)}))
		}(i)
	}
	wg.Wait()

	if as.Size() != 50 {
		t.Fatalf("Size = %d, want 50", as.Size())
	}
}

func TestAnnounceBinaryTrie_DeleteAndReinsert(t *testing.T) {
	tr := NewAnnounceBinaryTrie()
	tr.Insert([]byte("key"), []byte("val1"))
	root1 := tr.Root()

	tr.Delete([]byte("key"))
	if tr.Len() != 0 {
		t.Fatal("should be empty after delete")
	}

	tr.Insert([]byte("key"), []byte("val1"))
	root2 := tr.Root()

	if root1 != root2 {
		t.Fatal("reinserting same key-value should produce same root")
	}
}
