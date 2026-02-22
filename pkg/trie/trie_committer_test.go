package trie

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestTrieCommitter_BasicCommit(t *testing.T) {
	db := NewNodeDatabase(nil)
	tc := NewTrieCommitter(db)

	tr := New()
	tr.Put([]byte("hello"), []byte("world"))
	tr.Put([]byte("foo"), []byte("bar"))

	root, metrics, err := tc.Commit(tr)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if root.IsZero() {
		t.Error("root should not be zero")
	}
	if metrics.NodesWritten == 0 {
		t.Error("expected nodes to be written")
	}
	if metrics.BytesFlushed == 0 {
		t.Error("expected bytes to be flushed")
	}
	if metrics.HashTimeNs == 0 {
		t.Error("expected non-zero hash time")
	}
	if metrics.CommitTimeNs == 0 {
		t.Error("expected non-zero commit time")
	}
}

func TestTrieCommitter_EmptyTrie(t *testing.T) {
	db := NewNodeDatabase(nil)
	tc := NewTrieCommitter(db)

	tr := New()
	root, metrics, err := tc.Commit(tr)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if root != emptyRoot {
		t.Errorf("expected emptyRoot, got %s", root)
	}
	if metrics.NodesWritten != 0 {
		t.Errorf("expected 0 nodes written, got %d", metrics.NodesWritten)
	}
}

func TestTrieCommitter_MultipleCommits(t *testing.T) {
	db := NewNodeDatabase(nil)
	tc := NewTrieCommitter(db)

	// Use enough keys to ensure nodes are large enough (>= 32 bytes RLP)
	// so they are collected by commitRecursive.
	tr := New()
	tr.Put([]byte("key_alpha"), []byte("value_one"))
	tr.Put([]byte("key_bravo"), []byte("value_two"))
	tr.Put([]byte("key_charlie"), []byte("value_three"))
	root1, _, err := tc.Commit(tr)
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}

	tr.Put([]byte("key_delta"), []byte("value_four"))
	root2, _, err := tc.Commit(tr)
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}

	if root1 == root2 {
		t.Error("roots should differ after modification")
	}

	nodes, bytes, commits := tc.TotalMetrics()
	if nodes == 0 || bytes == 0 || commits != 2 {
		t.Errorf("unexpected total metrics: nodes=%d, bytes=%d, commits=%d", nodes, bytes, commits)
	}
}

func TestTrieCommitter_DirtyTracking(t *testing.T) {
	db := NewNodeDatabase(nil)
	tc := NewTrieCommitter(db)

	tr := New()
	tr.Put([]byte("key1"), []byte("value1"))
	tr.Put([]byte("key2"), []byte("value2"))
	tr.Put([]byte("key3"), []byte("value3"))

	_, metrics, err := tc.Commit(tr)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if metrics.DirtyBefore != 0 {
		t.Errorf("expected 0 dirty before first commit, got %d", metrics.DirtyBefore)
	}
	if tc.DirtyCount() == 0 {
		t.Error("expected dirty nodes after commit")
	}
	if tc.DirtySize() == 0 {
		t.Error("expected positive dirty size after commit")
	}
}

func TestTrieCommitter_RefCounting(t *testing.T) {
	db := NewNodeDatabase(nil)
	tc := NewTrieCommitter(db)

	tr := New()
	tr.Put([]byte("test"), []byte("data"))
	root, _, err := tc.Commit(tr)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Root should have a reference.
	if rc := tc.RefCount(root); rc < 1 {
		t.Errorf("expected refcount >= 1, got %d", rc)
	}

	// Dereference.
	freed := tc.Dereference(root)
	if len(freed) == 0 {
		t.Error("expected at least 1 freed hash")
	}

	// After dereference, refcount should be 0.
	if rc := tc.RefCount(root); rc != 0 {
		t.Errorf("expected refcount 0, got %d", rc)
	}
}

func TestTrieCommitter_DereferenceEmpty(t *testing.T) {
	db := NewNodeDatabase(nil)
	tc := NewTrieCommitter(db)

	freed := tc.Dereference(emptyRoot)
	if len(freed) != 0 {
		t.Error("dereferencing emptyRoot should free nothing")
	}

	freed = tc.Dereference(types.Hash{})
	if len(freed) != 0 {
		t.Error("dereferencing zero hash should free nothing")
	}
}

func TestTrieCommitter_FlushToWriter(t *testing.T) {
	db := NewNodeDatabase(nil)
	tc := NewTrieCommitter(db)

	tr := New()
	tr.Put([]byte("data"), []byte("stuff"))
	_, _, err := tc.Commit(tr)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if tc.DirtyCount() == 0 {
		t.Fatal("expected dirty nodes before flush")
	}

	// Flush to a batch writer.
	bw := NewBatchWriter(0)
	count, err := tc.Flush(bw)
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if count == 0 {
		t.Error("expected non-zero flush count")
	}
	if tc.DirtyCount() != 0 {
		t.Error("expected 0 dirty nodes after flush")
	}
}

func TestBatchWriter_Basic(t *testing.T) {
	bw := NewBatchWriter(1024)

	hash1 := types.BytesToHash([]byte{1, 2, 3})
	hash2 := types.BytesToHash([]byte{4, 5, 6})

	bw.Put(hash1, []byte("data1"))
	bw.Put(hash2, []byte("data2"))

	if bw.Count() != 2 {
		t.Errorf("expected 2 nodes, got %d", bw.Count())
	}
	if bw.Size() == 0 {
		t.Error("expected positive size")
	}
	if bw.NeedFlush() {
		t.Error("should not need flush yet")
	}
}

func TestBatchWriter_FlushTo(t *testing.T) {
	bw := NewBatchWriter(1024)
	// Use a nodeDBWriter adapter to bridge NodeDatabase to NodeWriter.
	target := NewNodeDatabase(nil)
	writer := &nodeDBWriter{db: target}

	hash := types.BytesToHash([]byte{0xab})
	bw.Put(hash, []byte("nodedata"))

	count, err := bw.FlushTo(writer)
	if err != nil {
		t.Fatalf("FlushTo: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
	if bw.Count() != 0 {
		t.Error("expected empty after flush")
	}

	// Verify data was written to target.
	data, err := target.Node(hash)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if string(data) != "nodedata" {
		t.Errorf("expected 'nodedata', got %q", data)
	}
}

// nodeDBWriter adapts NodeDatabase to the NodeWriter interface.
type nodeDBWriter struct {
	db *NodeDatabase
}

func (w *nodeDBWriter) Put(hash types.Hash, data []byte) error {
	w.db.InsertNode(hash, data)
	return nil
}

func TestBatchWriter_NeedFlush(t *testing.T) {
	bw := NewBatchWriter(100) // small buffer

	hash := types.BytesToHash([]byte{1})
	// Write enough data to exceed the limit.
	bw.Put(hash, make([]byte, 200))

	if !bw.NeedFlush() {
		t.Error("should need flush after exceeding max size")
	}
}

func TestTrieCommitter_CommitResolvable(t *testing.T) {
	// Build and commit a trie via the normal path.
	db := NewNodeDatabase(nil)
	tr := New()
	tr.Put([]byte("x"), []byte("y"))
	tr.Put([]byte("a"), []byte("b"))
	root, err := CommitTrie(tr, db)
	if err != nil {
		t.Fatalf("CommitTrie: %v", err)
	}

	// Open as resolvable trie.
	resTrie, err := NewResolvableTrie(root, db)
	if err != nil {
		t.Fatalf("NewResolvableTrie: %v", err)
	}

	tc := NewTrieCommitter(db)
	resTrie.Put([]byte("c"), []byte("d"))
	root2, metrics, err := tc.CommitResolvable(resTrie)
	if err != nil {
		t.Fatalf("CommitResolvable: %v", err)
	}
	if root2 == root {
		t.Error("root should change after modification")
	}
	if metrics.NodesWritten == 0 {
		t.Error("expected nodes written")
	}
}

func TestTrieCommitter_CleanNodeSkip(t *testing.T) {
	db := NewNodeDatabase(nil)
	tc := NewTrieCommitter(db)

	// Use enough keys to ensure the trie has nodes >= 32 bytes.
	tr := New()
	tr.Put([]byte("stable_key_one"), []byte("stable_data_value"))
	tr.Put([]byte("stable_key_two"), []byte("another_data_value"))

	// First commit.
	root1, _, err := tc.Commit(tr)
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}

	// Second commit without changes should produce same root and write zero nodes.
	root2, m2, err := tc.Commit(tr)
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if root1 != root2 {
		t.Error("root should be the same without changes")
	}
	if m2.NodesWritten != 0 {
		t.Errorf("expected 0 nodes written on clean commit, got %d", m2.NodesWritten)
	}
}

func TestBatchWriter_DuplicatePut(t *testing.T) {
	bw := NewBatchWriter(1024)

	hash := types.BytesToHash([]byte{0x01})
	bw.Put(hash, []byte("first"))
	bw.Put(hash, []byte("second"))

	// Duplicate puts should not increase count.
	if bw.Count() != 1 {
		t.Errorf("expected 1 node after duplicate put, got %d", bw.Count())
	}
}

func TestTrieCommitter_LargeTrie(t *testing.T) {
	db := NewNodeDatabase(nil)
	tc := NewTrieCommitter(db)

	tr := New()
	// Insert enough keys to create a multi-level trie with fullNodes.
	for i := 0; i < 20; i++ {
		key := []byte{byte(i), byte(i + 1), byte(i + 2), byte(i + 3)}
		val := []byte{byte(i * 3), byte(i*3 + 1)}
		tr.Put(key, val)
	}

	root, metrics, err := tc.Commit(tr)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if root.IsZero() {
		t.Error("root should not be zero")
	}
	if metrics.NodesWritten == 0 {
		t.Error("expected nodes to be written for a large trie")
	}
	if metrics.BytesFlushed == 0 {
		t.Error("expected bytes flushed for a large trie")
	}
	if tc.DirtyCount() == 0 {
		t.Error("expected dirty nodes after commit")
	}
}

func TestTrieCommitter_CommitThenModifyAndRecommit(t *testing.T) {
	db := NewNodeDatabase(nil)
	tc := NewTrieCommitter(db)

	tr := New()
	tr.Put([]byte("key_one"), []byte("val_one"))
	tr.Put([]byte("key_two"), []byte("val_two"))
	tr.Put([]byte("key_three"), []byte("val_three"))

	root1, m1, err := tc.Commit(tr)
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if m1.NodesWritten == 0 {
		t.Error("first commit should write nodes")
	}

	// Modify the trie and recommit.
	tr.Put([]byte("key_four"), []byte("val_four"))
	root2, m2, err := tc.Commit(tr)
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if root1 == root2 {
		t.Error("roots should differ after modification")
	}
	// The second commit should write some nodes (the modified path).
	if m2.NodesWritten == 0 {
		t.Error("second commit should write modified nodes")
	}

	_, _, commits := tc.TotalMetrics()
	if commits != 2 {
		t.Errorf("expected 2 total commits, got %d", commits)
	}
}
