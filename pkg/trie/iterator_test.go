package trie

import (
	"bytes"
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestIterator_EmptyTrie(t *testing.T) {
	tr := New()
	it := NewIterator(tr)
	if it.Next() {
		t.Fatal("expected no entries from empty trie")
	}
	if err := it.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIterator_SingleKey(t *testing.T) {
	tr := New()
	tr.Put([]byte("hello"), []byte("world"))

	it := NewIterator(tr)
	if !it.Next() {
		t.Fatal("expected one entry")
	}
	if string(it.Key) != "hello" {
		t.Fatalf("key = %q, want %q", it.Key, "hello")
	}
	if string(it.Value) != "world" {
		t.Fatalf("value = %q, want %q", it.Value, "world")
	}
	if it.Next() {
		t.Fatal("expected no more entries")
	}
}

func TestIterator_MultipleKeys(t *testing.T) {
	tr := New()
	entries := map[string]string{
		"doe":          "reindeer",
		"dog":          "puppy",
		"dogglesworth": "cat",
	}
	for k, v := range entries {
		tr.Put([]byte(k), []byte(v))
	}

	got := make(map[string]string)
	it := NewIterator(tr)
	for it.Next() {
		got[string(it.Key)] = string(it.Value)
	}
	if err := it.Err(); err != nil {
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

func TestIterator_LexicographicOrder(t *testing.T) {
	tr := New()
	keys := []string{"dog", "doe", "doge", "abc", "xyz", "hello", "world"}
	for _, k := range keys {
		tr.Put([]byte(k), []byte("v"))
	}

	sort.Strings(keys)

	var gotKeys []string
	it := NewIterator(tr)
	for it.Next() {
		gotKeys = append(gotKeys, string(it.Key))
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}

	if len(gotKeys) != len(keys) {
		t.Fatalf("got %d keys, want %d", len(gotKeys), len(keys))
	}
	for i, k := range keys {
		if gotKeys[i] != k {
			t.Errorf("key[%d] = %q, want %q", i, gotKeys[i], k)
		}
	}
}

func TestIterator_OverlappingPrefixes(t *testing.T) {
	// Keys like "do", "dog", "doge" that share common prefixes and
	// exercise branch node value slots (Children[16]).
	tr := New()
	entries := map[string]string{
		"do":   "verb",
		"dog":  "puppy",
		"doge": "coin",
	}
	for k, v := range entries {
		tr.Put([]byte(k), []byte(v))
	}

	got := make(map[string]string)
	it := NewIterator(tr)
	for it.Next() {
		got[string(it.Key)] = string(it.Value)
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

func TestIterator_BinaryKeys(t *testing.T) {
	tr := New()
	entries := map[string][]byte{}
	for i := 0; i < 16; i++ {
		key := []byte{byte(i << 4)}
		val := []byte{byte(i)}
		tr.Put(key, val)
		entries[string(key)] = val
	}

	got := map[string][]byte{}
	it := NewIterator(tr)
	for it.Next() {
		got[string(it.Key)] = it.Value
	}

	if len(got) != len(entries) {
		t.Fatalf("got %d entries, want %d", len(got), len(entries))
	}
	for k, want := range entries {
		if v, ok := got[k]; !ok {
			t.Errorf("missing key %x", k)
		} else if !bytes.Equal(v, want) {
			t.Errorf("key %x: got %x, want %x", k, v, want)
		}
	}
}

func TestIterator_LargeDataset(t *testing.T) {
	tr := New()
	reference := make(map[string]string)
	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("key-%04d", i)
		val := fmt.Sprintf("value-%04d", i)
		tr.Put([]byte(key), []byte(val))
		reference[key] = val
	}

	got := make(map[string]string)
	it := NewIterator(tr)
	count := 0
	for it.Next() {
		got[string(it.Key)] = string(it.Value)
		count++
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}

	if count != 200 {
		t.Fatalf("iterated %d entries, want 200", count)
	}
	for k, want := range reference {
		if v, ok := got[k]; !ok {
			t.Errorf("missing key %q", k)
		} else if v != want {
			t.Errorf("key %q: got %q, want %q", k, v, want)
		}
	}
}

func TestIterator_Random(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	tr := New()
	reference := make(map[string]string)

	for i := 0; i < 500; i++ {
		keyLen := rng.Intn(20) + 1
		key := make([]byte, keyLen)
		rng.Read(key)
		valLen := rng.Intn(50) + 1
		val := make([]byte, valLen)
		rng.Read(val)
		tr.Put(key, val)
		reference[string(key)] = string(val)
	}

	got := make(map[string]string)
	it := NewIterator(tr)
	for it.Next() {
		got[string(it.Key)] = string(it.Value)
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}

	if len(got) != len(reference) {
		t.Fatalf("iterated %d entries, want %d", len(got), len(reference))
	}
	for k, want := range reference {
		if v, ok := got[k]; !ok {
			t.Errorf("missing key %x", k)
		} else if v != want {
			t.Errorf("key %x: value mismatch", k)
		}
	}
}

func TestIterator_OrderConsistency(t *testing.T) {
	// Verify that two iterations over the same trie yield the same order.
	tr := New()
	for i := 0; i < 50; i++ {
		tr.Put([]byte(fmt.Sprintf("k%03d", i)), []byte(fmt.Sprintf("v%03d", i)))
	}

	var keys1, keys2 []string
	it1 := NewIterator(tr)
	for it1.Next() {
		keys1 = append(keys1, string(it1.Key))
	}
	it2 := NewIterator(tr)
	for it2.Next() {
		keys2 = append(keys2, string(it2.Key))
	}

	if len(keys1) != len(keys2) {
		t.Fatalf("iteration counts differ: %d vs %d", len(keys1), len(keys2))
	}
	for i := range keys1 {
		if keys1[i] != keys2[i] {
			t.Errorf("key[%d] differs: %q vs %q", i, keys1[i], keys2[i])
		}
	}
}

func TestIterator_AfterMutation(t *testing.T) {
	tr := New()
	tr.Put([]byte("a"), []byte("1"))
	tr.Put([]byte("b"), []byte("2"))
	tr.Put([]byte("c"), []byte("3"))

	// Delete "b" and add "d".
	tr.Delete([]byte("b"))
	tr.Put([]byte("d"), []byte("4"))

	expected := map[string]string{"a": "1", "c": "3", "d": "4"}
	got := make(map[string]string)
	it := NewIterator(tr)
	for it.Next() {
		got[string(it.Key)] = string(it.Value)
	}

	if len(got) != len(expected) {
		t.Fatalf("got %d entries, want %d", len(got), len(expected))
	}
	for k, want := range expected {
		if v := got[k]; v != want {
			t.Errorf("key %q: got %q, want %q", k, v, want)
		}
	}
}

func TestIterator_SingleByteKeys(t *testing.T) {
	tr := New()
	for i := 0; i < 256; i++ {
		tr.Put([]byte{byte(i)}, []byte{byte(i), byte(i)})
	}

	count := 0
	it := NewIterator(tr)
	for it.Next() {
		count++
	}
	if count != 256 {
		t.Fatalf("iterated %d entries, want 256", count)
	}
}

func TestIterator_LargeValues(t *testing.T) {
	tr := New()
	largeVal := bytes.Repeat([]byte{0x42}, 1024)
	tr.Put([]byte("big"), largeVal)
	tr.Put([]byte("small"), []byte("tiny"))

	got := make(map[string][]byte)
	it := NewIterator(tr)
	for it.Next() {
		v := make([]byte, len(it.Value))
		copy(v, it.Value)
		got[string(it.Key)] = v
	}

	if !bytes.Equal(got["big"], largeVal) {
		t.Fatal("large value mismatch")
	}
	if string(got["small"]) != "tiny" {
		t.Fatal("small value mismatch")
	}
}

// -- ResolvableIterator tests --

func TestResolvableIterator_BasicRoundTrip(t *testing.T) {
	// Build a trie, commit to DB, reconstruct, and iterate.
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

	db := NewNodeDatabase(nil)
	root, err := CommitTrie(tr, db)
	if err != nil {
		t.Fatalf("CommitTrie error: %v", err)
	}

	rt, err := NewResolvableTrie(root, db)
	if err != nil {
		t.Fatalf("NewResolvableTrie error: %v", err)
	}

	got := make(map[string]string)
	it := NewResolvableIterator(rt)
	for it.Next() {
		got[string(it.Key)] = string(it.Value)
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}

	if len(got) != len(entries) {
		t.Fatalf("iterated %d entries, want %d", len(got), len(entries))
	}
	for k, want := range entries {
		if v, ok := got[k]; !ok {
			t.Errorf("missing key %q", k)
		} else if v != want {
			t.Errorf("key %q: got %q, want %q", k, v, want)
		}
	}
}

func TestResolvableIterator_Empty(t *testing.T) {
	db := NewNodeDatabase(nil)
	rt, err := NewResolvableTrie(types.Hash{}, db)
	if err != nil {
		t.Fatalf("NewResolvableTrie error: %v", err)
	}

	it := NewResolvableIterator(rt)
	if it.Next() {
		t.Fatal("expected no entries from empty trie")
	}
}

func TestResolvableTrie_DeleteRoundTrip(t *testing.T) {
	// Build, commit, reconstruct, delete, verify.
	tr := New()
	entries := map[string]string{
		"alpha":   "1",
		"bravo":   "2",
		"charlie": "3",
		"delta":   "4",
	}
	for k, v := range entries {
		tr.Put([]byte(k), []byte(v))
	}

	db := NewNodeDatabase(nil)
	root, err := CommitTrie(tr, db)
	if err != nil {
		t.Fatalf("CommitTrie error: %v", err)
	}

	rt, err := NewResolvableTrie(root, db)
	if err != nil {
		t.Fatalf("NewResolvableTrie error: %v", err)
	}

	// Delete "bravo" and "delta".
	if err := rt.Delete([]byte("bravo")); err != nil {
		t.Fatalf("Delete(bravo) error: %v", err)
	}
	if err := rt.Delete([]byte("delta")); err != nil {
		t.Fatalf("Delete(delta) error: %v", err)
	}

	// Verify remaining keys.
	for _, tc := range []struct {
		key  string
		want string
	}{
		{"alpha", "1"},
		{"charlie", "3"},
	} {
		got, err := rt.Get([]byte(tc.key))
		if err != nil {
			t.Fatalf("Get(%q) error: %v", tc.key, err)
		}
		if string(got) != tc.want {
			t.Fatalf("Get(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}

	// Verify deleted keys are gone.
	for _, k := range []string{"bravo", "delta"} {
		_, err := rt.Get([]byte(k))
		if err != ErrNotFound {
			t.Fatalf("Get(%q) after delete: err = %v, want ErrNotFound", k, err)
		}
	}

	// Compare hash with a fresh trie containing only alpha and charlie.
	expected := New()
	expected.Put([]byte("alpha"), []byte("1"))
	expected.Put([]byte("charlie"), []byte("3"))
	if rt.Hash() != expected.Hash() {
		t.Fatalf("hash mismatch after delete: got %s, want %s", rt.Hash().Hex(), expected.Hash().Hex())
	}
}

func TestResolvableTrie_DeleteNonExistent(t *testing.T) {
	tr := New()
	tr.Put([]byte("key"), []byte("value"))

	db := NewNodeDatabase(nil)
	root, err := CommitTrie(tr, db)
	if err != nil {
		t.Fatalf("CommitTrie error: %v", err)
	}

	rt, err := NewResolvableTrie(root, db)
	if err != nil {
		t.Fatalf("NewResolvableTrie error: %v", err)
	}

	hashBefore := rt.Hash()
	if err := rt.Delete([]byte("nonexistent")); err != nil {
		t.Fatalf("Delete non-existent error: %v", err)
	}
	if rt.Hash() != hashBefore {
		t.Fatal("hash changed after deleting non-existent key")
	}
}

func TestResolvableTrie_PutEmptyValueDeletes(t *testing.T) {
	tr := New()
	tr.Put([]byte("a"), []byte("1"))
	tr.Put([]byte("b"), []byte("2"))

	db := NewNodeDatabase(nil)
	root, err := CommitTrie(tr, db)
	if err != nil {
		t.Fatalf("CommitTrie error: %v", err)
	}

	rt, err := NewResolvableTrie(root, db)
	if err != nil {
		t.Fatalf("NewResolvableTrie error: %v", err)
	}

	// Put with empty value should delete.
	if err := rt.Put([]byte("a"), nil); err != nil {
		t.Fatalf("Put(a, nil) error: %v", err)
	}

	_, err = rt.Get([]byte("a"))
	if err != ErrNotFound {
		t.Fatalf("Get(a) after Put(nil): err = %v, want ErrNotFound", err)
	}

	got, err := rt.Get([]byte("b"))
	if err != nil || string(got) != "2" {
		t.Fatalf("Get(b) = %q, %v; want 2, nil", got, err)
	}
}

func TestResolvableTrie_ProveAndVerify(t *testing.T) {
	tr := New()
	entries := map[string]string{
		"doe":    "reindeer",
		"dog":    "puppy",
		"doge":   "coin",
		"horse":  "stallion",
		"abc":    "def",
		"abcdef": "ghij",
	}
	for k, v := range entries {
		tr.Put([]byte(k), []byte(v))
	}

	db := NewNodeDatabase(nil)
	root, err := CommitTrie(tr, db)
	if err != nil {
		t.Fatalf("CommitTrie error: %v", err)
	}

	rt, err := NewResolvableTrie(root, db)
	if err != nil {
		t.Fatalf("NewResolvableTrie error: %v", err)
	}

	for key, want := range entries {
		proof, err := rt.Prove([]byte(key))
		if err != nil {
			t.Fatalf("Prove(%q) error: %v", key, err)
		}
		val, err := VerifyProof(root, []byte(key), proof)
		if err != nil {
			t.Fatalf("VerifyProof(%q) error: %v", key, err)
		}
		if string(val) != want {
			t.Fatalf("VerifyProof(%q) = %q, want %q", key, val, want)
		}
	}
}

func TestResolvableTrie_ProveAbsence(t *testing.T) {
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))
	tr.Put([]byte("dog"), []byte("puppy"))

	db := NewNodeDatabase(nil)
	root, err := CommitTrie(tr, db)
	if err != nil {
		t.Fatalf("CommitTrie error: %v", err)
	}

	rt, err := NewResolvableTrie(root, db)
	if err != nil {
		t.Fatalf("NewResolvableTrie error: %v", err)
	}

	// "cat" is not in the trie.
	proof, err := rt.ProveAbsence([]byte("cat"))
	if err != nil {
		t.Fatalf("ProveAbsence error: %v", err)
	}

	val, err := VerifyProof(root, []byte("cat"), proof)
	if err != nil {
		t.Fatalf("VerifyProof absence error: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil for absent key, got %x", val)
	}
}

func TestResolvableTrie_CommitAndRecommit(t *testing.T) {
	// Build, commit, modify, recommit, verify both roots.
	db := NewNodeDatabase(nil)

	rt, err := NewResolvableTrie(types.Hash{}, db)
	if err != nil {
		t.Fatalf("NewResolvableTrie error: %v", err)
	}

	rt.Put([]byte("a"), []byte("1"))
	rt.Put([]byte("b"), []byte("2"))
	root1, err := rt.Commit()
	if err != nil {
		t.Fatalf("first Commit error: %v", err)
	}

	// Modify and recommit.
	rt.Put([]byte("c"), []byte("3"))
	rt.Delete([]byte("a"))
	root2, err := rt.Commit()
	if err != nil {
		t.Fatalf("second Commit error: %v", err)
	}

	if root1 == root2 {
		t.Fatal("roots should differ after modification")
	}

	// Load first root and verify.
	rt1, err := NewResolvableTrie(root1, db)
	if err != nil {
		t.Fatalf("load root1 error: %v", err)
	}
	got, err := rt1.Get([]byte("a"))
	if err != nil || string(got) != "1" {
		t.Fatalf("root1 Get(a) = %q, %v", got, err)
	}

	// Load second root and verify.
	rt2, err := NewResolvableTrie(root2, db)
	if err != nil {
		t.Fatalf("load root2 error: %v", err)
	}
	_, err = rt2.Get([]byte("a"))
	if err != ErrNotFound {
		t.Fatalf("root2 Get(a) err = %v, want ErrNotFound", err)
	}
	got, err = rt2.Get([]byte("c"))
	if err != nil || string(got) != "3" {
		t.Fatalf("root2 Get(c) = %q, %v", got, err)
	}
}

func TestIterator_ValueAtBranchOrdering(t *testing.T) {
	// When a key "do" has a value AND there's a "dog" key, "do" is stored at
	// the branch value (Children[16]). The iterator must return "do" before "dog".
	tr := New()
	tr.Put([]byte("do"), []byte("verb"))
	tr.Put([]byte("dog"), []byte("puppy"))
	tr.Put([]byte("doge"), []byte("coin"))

	var keys []string
	it := NewIterator(tr)
	for it.Next() {
		keys = append(keys, string(it.Key))
	}

	expected := []string{"do", "dog", "doge"}
	if len(keys) != len(expected) {
		t.Fatalf("got %d keys, want %d: %v", len(keys), len(expected), keys)
	}
	for i, k := range expected {
		if keys[i] != k {
			t.Errorf("keys[%d] = %q, want %q (full: %v)", i, keys[i], k, keys)
		}
	}
}
