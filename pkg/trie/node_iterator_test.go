package trie

import (
	"bytes"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// buildTrieAndCommit creates a trie from key-value pairs, commits it to a
// NodeDatabase, and returns the root hash plus a flat map of hash -> node data.
func buildTrieAndCommit(t *testing.T, entries map[string]string) (types.Hash, map[types.Hash][]byte) {
	t.Helper()
	tr := New()
	for k, v := range entries {
		tr.Put([]byte(k), []byte(v))
	}
	db := NewNodeDatabase(nil)
	root, err := CommitTrie(tr, db)
	if err != nil {
		t.Fatalf("CommitTrie: %v", err)
	}
	// Extract committed nodes from the dirty map.
	nodeMap := make(map[types.Hash][]byte)
	db.mu.RLock()
	for h, d := range db.dirty {
		cp := make([]byte, len(d))
		copy(cp, d)
		nodeMap[h] = cp
	}
	db.mu.RUnlock()
	return root, nodeMap
}

func TestTrieIterator_EmptyTrie(t *testing.T) {
	it := NewTrieIterator(types.Hash{}, nil)
	if it.Next() {
		t.Fatal("expected no entries from empty trie")
	}
	if err := it.Error(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTrieIterator_EmptyNodeMap(t *testing.T) {
	// Non-zero root but no nodes: should yield nothing.
	fakeRoot := types.HexToHash("0x1234")
	it := NewTrieIterator(fakeRoot, map[types.Hash][]byte{})
	if it.Next() {
		t.Fatal("expected no entries with empty node map")
	}
}

func TestTrieIterator_WithNodes(t *testing.T) {
	entries := map[string]string{
		"doe":  "reindeer",
		"dog":  "puppy",
		"doge": "coin",
	}
	root, nodeMap := buildTrieAndCommit(t, entries)

	it := NewTrieIterator(root, nodeMap)
	got := make(map[string]string)
	for it.Next() {
		got[string(it.Key())] = string(it.Value())
		// Verify leaf is always true.
		if !it.Leaf() {
			t.Error("Leaf() should be true during iteration")
		}
		// Hash should not be zero.
		if it.Hash() == (types.Hash{}) {
			t.Error("Hash() should not be zero")
		}
		// Path should not be nil.
		if it.Path() == nil {
			t.Error("Path() should not be nil")
		}
	}
	if err := it.Error(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}

	for k, want := range entries {
		if v, ok := got[k]; !ok {
			t.Errorf("missing key %q", k)
		} else if v != want {
			t.Errorf("key %q: got %q, want %q", k, v, want)
		}
	}
	if len(got) != len(entries) {
		t.Errorf("got %d entries, want %d", len(got), len(entries))
	}
}

func TestTrieIterator_ManyEntries(t *testing.T) {
	entries := map[string]string{
		"alpha":   "1",
		"bravo":   "2",
		"charlie": "3",
		"delta":   "4",
		"echo":    "5",
		"foxtrot": "6",
	}
	root, nodeMap := buildTrieAndCommit(t, entries)
	it := NewTrieIterator(root, nodeMap)

	got := make(map[string]string)
	for it.Next() {
		got[string(it.Key())] = string(it.Value())
	}
	if err := it.Error(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
	if len(got) != len(entries) {
		t.Fatalf("got %d entries, want %d", len(got), len(entries))
	}
	for k, want := range entries {
		if got[k] != want {
			t.Errorf("key %q: got %q, want %q", k, got[k], want)
		}
	}
}

func TestTrieIterator_BeforeNext(t *testing.T) {
	entries := map[string]string{"a": "1"}
	root, nodeMap := buildTrieAndCommit(t, entries)
	it := NewTrieIterator(root, nodeMap)

	// Before calling Next, accessors should return zero values.
	if it.Key() != nil {
		t.Error("Key() should be nil before Next()")
	}
	if it.Value() != nil {
		t.Error("Value() should be nil before Next()")
	}
	if it.Leaf() {
		t.Error("Leaf() should be false before Next()")
	}
}

func TestDiffIterator_FindsNewNodes(t *testing.T) {
	// Build two tries: trieA has {a, b}, trieB has {a, b, c}.
	entriesA := map[string]string{"alpha": "1", "bravo": "2"}
	entriesB := map[string]string{"alpha": "1", "bravo": "2", "charlie": "3"}

	rootA, nodesA := buildTrieAndCommit(t, entriesA)
	rootB, nodesB := buildTrieAndCommit(t, entriesB)

	iterA := NewTrieIterator(rootA, nodesA)
	iterB := NewTrieIterator(rootB, nodesB)

	diff := NewDiffIterator(iterA, iterB)
	var diffKeys []string
	for diff.Next() {
		diffKeys = append(diffKeys, string(diff.Key()))
		diff.Advance()
	}

	if len(diffKeys) != 1 {
		t.Fatalf("diff found %d entries, want 1: %v", len(diffKeys), diffKeys)
	}
	if diffKeys[0] != "charlie" {
		t.Fatalf("diff key = %q, want %q", diffKeys[0], "charlie")
	}
}

func TestDiffIterator_DetectsValueChange(t *testing.T) {
	// Same key, different value.
	entriesA := map[string]string{"alpha": "old"}
	entriesB := map[string]string{"alpha": "new"}

	rootA, nodesA := buildTrieAndCommit(t, entriesA)
	rootB, nodesB := buildTrieAndCommit(t, entriesB)

	iterA := NewTrieIterator(rootA, nodesA)
	iterB := NewTrieIterator(rootB, nodesB)

	diff := NewDiffIterator(iterA, iterB)
	found := false
	for diff.Next() {
		if string(diff.Key()) == "alpha" && string(diff.Value()) == "new" {
			found = true
		}
		diff.Advance()
	}
	if !found {
		t.Fatal("diff should detect changed value for alpha")
	}
}

func TestDiffIterator_EmptyA(t *testing.T) {
	// a is empty, b has entries => all b entries are diffs.
	entriesB := map[string]string{"x": "1", "y": "2"}
	rootB, nodesB := buildTrieAndCommit(t, entriesB)

	iterA := NewTrieIterator(types.Hash{}, nil)
	iterB := NewTrieIterator(rootB, nodesB)

	diff := NewDiffIterator(iterA, iterB)
	count := 0
	for diff.Next() {
		count++
		diff.Advance()
	}
	if count != 2 {
		t.Fatalf("diff count = %d, want 2", count)
	}
}

func TestDiffIterator_IdenticalTries(t *testing.T) {
	entries := map[string]string{"a": "1", "b": "2"}
	rootA, nodesA := buildTrieAndCommit(t, entries)
	rootB, nodesB := buildTrieAndCommit(t, entries)

	iterA := NewTrieIterator(rootA, nodesA)
	iterB := NewTrieIterator(rootB, nodesB)

	diff := NewDiffIterator(iterA, iterB)
	if diff.Next() {
		t.Fatal("identical tries should yield no diffs")
	}
}

func TestCollectLeaves(t *testing.T) {
	entries := map[string]string{
		"foo": "bar",
		"baz": "qux",
	}
	root, nodeMap := buildTrieAndCommit(t, entries)
	it := NewTrieIterator(root, nodeMap)

	keys, values, err := CollectLeaves(it)
	if err != nil {
		t.Fatalf("CollectLeaves error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d leaves, want 2", len(keys))
	}

	// Build a map for easier checking.
	got := make(map[string]string)
	for i, k := range keys {
		got[string(k)] = string(values[i])
	}
	for k, want := range entries {
		if got[k] != want {
			t.Errorf("key %q: got %q, want %q", k, got[k], want)
		}
	}
}

func TestCollectLeaves_Empty(t *testing.T) {
	it := NewTrieIterator(types.Hash{}, nil)
	keys, values, err := CollectLeaves(it)
	if err != nil {
		t.Fatalf("CollectLeaves error: %v", err)
	}
	if len(keys) != 0 || len(values) != 0 {
		t.Fatalf("expected empty results, got %d keys, %d values", len(keys), len(values))
	}
}

func TestIteratorStats(t *testing.T) {
	stats := IteratorStats{
		NodesVisited: 10,
		LeavesFound:  3,
		BytesRead:    1024,
		Duration:     100 * time.Millisecond,
	}

	if stats.NodesVisited != 10 {
		t.Fatalf("NodesVisited = %d, want 10", stats.NodesVisited)
	}
	if stats.LeavesFound != 3 {
		t.Fatalf("LeavesFound = %d, want 3", stats.LeavesFound)
	}
	if stats.BytesRead != 1024 {
		t.Fatalf("BytesRead = %d, want 1024", stats.BytesRead)
	}
	if stats.Duration != 100*time.Millisecond {
		t.Fatalf("Duration = %v, want 100ms", stats.Duration)
	}
}

func TestCollectLeavesWithStats(t *testing.T) {
	entries := map[string]string{
		"alpha": "1",
		"bravo": "2",
		"charlie": "3",
	}
	root, nodeMap := buildTrieAndCommit(t, entries)

	start := time.Now()
	it := NewTrieIterator(root, nodeMap)
	keys, values, err := CollectLeaves(it)
	duration := time.Since(start)
	if err != nil {
		t.Fatalf("CollectLeaves error: %v", err)
	}

	stats := IteratorStats{
		LeavesFound: len(keys),
		BytesRead:   totalBytes(keys, values),
		Duration:    duration,
	}

	if stats.LeavesFound != 3 {
		t.Fatalf("LeavesFound = %d, want 3", stats.LeavesFound)
	}
	if stats.BytesRead == 0 {
		t.Fatal("BytesRead should be > 0")
	}
	if stats.Duration == 0 {
		t.Fatal("Duration should be > 0")
	}
}

func TestTrieIterator_SortedOrder(t *testing.T) {
	entries := map[string]string{
		"dog":   "1",
		"cat":   "2",
		"apple": "3",
		"zebra": "4",
	}
	root, nodeMap := buildTrieAndCommit(t, entries)
	it := NewTrieIterator(root, nodeMap)

	var keys []string
	for it.Next() {
		keys = append(keys, string(it.Key()))
	}

	for i := 1; i < len(keys); i++ {
		if bytes.Compare([]byte(keys[i-1]), []byte(keys[i])) >= 0 {
			t.Fatalf("keys not in sorted order at index %d: %q >= %q", i, keys[i-1], keys[i])
		}
	}
}

func TestDiffIterator_ErrorPassthrough(t *testing.T) {
	iterA := NewTrieIterator(types.Hash{}, nil)
	iterB := NewTrieIterator(types.Hash{}, nil)
	diff := NewDiffIterator(iterA, iterB)
	if diff.Error() != nil {
		t.Fatal("expected nil error for clean iterators")
	}
}

// totalBytes sums the byte lengths of all keys and values.
func totalBytes(keys, values [][]byte) int {
	total := 0
	for i := range keys {
		total += len(keys[i]) + len(values[i])
	}
	return total
}
