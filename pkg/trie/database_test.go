package trie

import (
	"bytes"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

func TestNodeDatabase_InsertAndRetrieve(t *testing.T) {
	db := NewNodeDatabase(nil)

	data := []byte("test node data")
	hash := crypto.Keccak256Hash(data)

	db.InsertNode(hash, data)

	got, err := db.Node(hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch: got %x, want %x", got, data)
	}
}

func TestNodeDatabase_NotFound(t *testing.T) {
	db := NewNodeDatabase(nil)
	_, err := db.Node(types.Hash{1, 2, 3})
	if err != ErrNodeNotFound {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestNodeDatabase_EmptyHashNotFound(t *testing.T) {
	db := NewNodeDatabase(nil)
	_, err := db.Node(types.Hash{})
	if err != ErrNodeNotFound {
		t.Fatalf("expected ErrNodeNotFound for empty hash, got %v", err)
	}
}

func TestNodeDatabase_DirtySizeAndCount(t *testing.T) {
	db := NewNodeDatabase(nil)

	if db.DirtySize() != 0 || db.DirtyCount() != 0 {
		t.Fatal("expected 0 dirty size/count for empty db")
	}

	db.InsertNode(types.Hash{1}, []byte("aaa"))
	db.InsertNode(types.Hash{2}, []byte("bbbbb"))

	if db.DirtyCount() != 2 {
		t.Fatalf("expected 2 dirty nodes, got %d", db.DirtyCount())
	}
	if db.DirtySize() != 8 { // 3 + 5
		t.Fatalf("expected 8 dirty bytes, got %d", db.DirtySize())
	}
}

func TestNodeDatabase_Commit(t *testing.T) {
	db := NewNodeDatabase(nil)

	db.InsertNode(types.Hash{1}, []byte("node1"))
	db.InsertNode(types.Hash{2}, []byte("node2"))

	store := make(map[types.Hash][]byte)
	writer := &mapNodeWriter{store: store}

	if err := db.Commit(writer); err != nil {
		t.Fatalf("commit error: %v", err)
	}

	if db.DirtyCount() != 0 {
		t.Fatalf("expected 0 dirty after commit, got %d", db.DirtyCount())
	}
	if len(store) != 2 {
		t.Fatalf("expected 2 committed nodes, got %d", len(store))
	}
}

func TestNodeDatabase_DiskFallback(t *testing.T) {
	diskData := map[types.Hash][]byte{
		{0xAA}: []byte("from disk"),
	}
	disk := &mapNodeReader{store: diskData}
	db := NewNodeDatabase(disk)

	// Insert one dirty node.
	db.InsertNode(types.Hash{0xBB}, []byte("from memory"))

	// Retrieve dirty node.
	got, err := db.Node(types.Hash{0xBB})
	if err != nil {
		t.Fatalf("dirty lookup failed: %v", err)
	}
	if !bytes.Equal(got, []byte("from memory")) {
		t.Fatalf("dirty data mismatch")
	}

	// Retrieve disk node.
	got, err = db.Node(types.Hash{0xAA})
	if err != nil {
		t.Fatalf("disk lookup failed: %v", err)
	}
	if !bytes.Equal(got, []byte("from disk")) {
		t.Fatalf("disk data mismatch")
	}
}

func TestNodeDatabase_Concurrent(t *testing.T) {
	db := NewNodeDatabase(nil)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h := types.Hash{byte(i)}
			db.InsertNode(h, []byte{byte(i)})
			db.Node(h)
		}(i)
	}
	wg.Wait()

	if db.DirtyCount() != 100 {
		t.Fatalf("expected 100 dirty nodes, got %d", db.DirtyCount())
	}
}

func TestCommitTrie_RoundTrip(t *testing.T) {
	// Build a trie, commit to DB, then reconstruct from DB.
	tr := New()
	entries := map[string]string{
		"doe":    "reindeer",
		"dog":    "puppy",
		"do":     "verb",
		"doge":   "coin",
		"horse":  "stallion",
		"abc":    "def",
		"abcdef": "ghij",
	}
	for k, v := range entries {
		tr.Put([]byte(k), []byte(v))
	}

	originalHash := tr.Hash()

	// Commit to node database.
	db := NewNodeDatabase(nil)
	root, err := CommitTrie(tr, db)
	if err != nil {
		t.Fatalf("CommitTrie error: %v", err)
	}
	if root != originalHash {
		t.Fatalf("root mismatch: commit=%v, hash=%v", root, originalHash)
	}

	// Verify nodes were stored.
	if db.DirtyCount() == 0 {
		t.Fatal("expected dirty nodes after commit")
	}

	// Reconstruct trie from the database.
	rt, err := NewResolvableTrie(root, db)
	if err != nil {
		t.Fatalf("NewResolvableTrie error: %v", err)
	}

	// Verify all entries can be retrieved.
	for k, want := range entries {
		got, err := rt.Get([]byte(k))
		if err != nil {
			t.Fatalf("Get(%q) error: %v", k, err)
		}
		if string(got) != want {
			t.Fatalf("Get(%q) = %q, want %q", k, got, want)
		}
	}
}

func TestCommitTrie_EmptyTrie(t *testing.T) {
	tr := New()
	db := NewNodeDatabase(nil)
	root, err := CommitTrie(tr, db)
	if err != nil {
		t.Fatalf("CommitTrie empty error: %v", err)
	}
	if root != emptyRoot {
		t.Fatalf("expected empty root, got %v", root)
	}
}

func TestResolvableTrie_PutAndGet(t *testing.T) {
	db := NewNodeDatabase(nil)
	rt, err := NewResolvableTrie(types.Hash{}, db)
	if err != nil {
		t.Fatalf("NewResolvableTrie error: %v", err)
	}

	rt.Put([]byte("hello"), []byte("world"))
	rt.Put([]byte("foo"), []byte("bar"))

	got, err := rt.Get([]byte("hello"))
	if err != nil || string(got) != "world" {
		t.Fatalf("Get(hello) = %q, %v; want world, nil", got, err)
	}

	got, err = rt.Get([]byte("foo"))
	if err != nil || string(got) != "bar" {
		t.Fatalf("Get(foo) = %q, %v; want bar, nil", got, err)
	}

	// Commit and reconstruct.
	root, err := rt.Commit()
	if err != nil {
		t.Fatalf("Commit error: %v", err)
	}

	rt2, err := NewResolvableTrie(root, db)
	if err != nil {
		t.Fatalf("NewResolvableTrie(root) error: %v", err)
	}

	got, err = rt2.Get([]byte("hello"))
	if err != nil || string(got) != "world" {
		t.Fatalf("reconstructed Get(hello) = %q, %v", got, err)
	}
}

func TestResolvableTrie_NotFound(t *testing.T) {
	db := NewNodeDatabase(nil)
	rt, _ := NewResolvableTrie(types.Hash{}, db)
	rt.Put([]byte("exists"), []byte("yes"))

	_, err := rt.Get([]byte("missing"))
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDecodeNode_LeafNode(t *testing.T) {
	// Create a shortNode (leaf), encode it, then decode.
	tr := New()
	tr.Put([]byte("abc"), []byte("value"))

	// Hash to force encoding.
	h := newHasher()
	collapsed, _ := h.hashChildren(tr.root)
	enc, err := encodeNode(collapsed)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}

	decoded, err := decodeNode(nil, enc)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if decoded == nil {
		t.Fatal("decoded node is nil")
	}
}

func TestRawDBAdapters(t *testing.T) {
	store := make(map[string][]byte)
	getter := func(key []byte) ([]byte, error) {
		v, ok := store[string(key)]
		if !ok {
			return nil, ErrNodeNotFound
		}
		return v, nil
	}
	putter := func(key, value []byte) error {
		store[string(key)] = value
		return nil
	}

	reader := NewRawDBNodeReader(getter)
	writer := NewRawDBNodeWriter(putter)

	hash := types.Hash{0xDE, 0xAD}
	data := []byte("node data")

	// Write.
	if err := writer.Put(hash, data); err != nil {
		t.Fatalf("write error: %v", err)
	}

	// Read back.
	got, err := reader.Node(hash)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch: got %x, want %x", got, data)
	}
}

// --- Helpers ---

type mapNodeReader struct {
	store map[types.Hash][]byte
}

func (r *mapNodeReader) Node(hash types.Hash) ([]byte, error) {
	data, ok := r.store[hash]
	if !ok {
		return nil, ErrNodeNotFound
	}
	return data, nil
}

type mapNodeWriter struct {
	store map[types.Hash][]byte
}

func (w *mapNodeWriter) Put(hash types.Hash, data []byte) error {
	w.store[hash] = data
	return nil
}
