package trie

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// --- BinaryIterator: empty trie ---

func TestBinaryIterator_Empty(t *testing.T) {
	bt := NewBinaryTrie()
	it := NewBinaryIterator(bt)
	if it.Next() {
		t.Fatal("iterator on empty trie should return false")
	}
	if it.Key() != nil {
		t.Fatal("Key() on exhausted iterator should be nil")
	}
	if it.Value() != nil {
		t.Fatal("Value() on exhausted iterator should be nil")
	}
}

// --- BinaryIterator: single entry ---

func TestBinaryIterator_SingleEntry(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("only"), []byte("one"))

	it := NewBinaryIterator(bt)
	count := 0
	for it.Next() {
		count++
		if it.Key() == nil {
			t.Fatal("Key() should not be nil")
		}
		if it.Value() == nil {
			t.Fatal("Value() should not be nil")
		}
		// Verify the key matches the keccak hash of "only".
		hk := crypto.Keccak256Hash([]byte("only"))
		if !bytes.Equal(it.Key(), hk[:]) {
			t.Fatalf("Key = %x, want %x", it.Key(), hk[:])
		}
		if !bytes.Equal(it.Value(), []byte("one")) {
			t.Fatalf("Value = %q, want %q", it.Value(), "one")
		}
	}
	if count != 1 {
		t.Fatalf("iterated %d times, want 1", count)
	}
}

// --- BinaryIterator: multiple entries ---

func TestBinaryIterator_MultipleEntries(t *testing.T) {
	bt := NewBinaryTrie()
	entries := map[string]string{
		"alpha":   "1",
		"bravo":   "2",
		"charlie": "3",
		"delta":   "4",
		"echo":    "5",
	}
	for k, v := range entries {
		bt.Put([]byte(k), []byte(v))
	}

	found := make(map[types.Hash]string)
	it := NewBinaryIterator(bt)
	for it.Next() {
		hk := types.BytesToHash(it.Key())
		found[hk] = string(it.Value())
	}

	if len(found) != len(entries) {
		t.Fatalf("iterated %d entries, want %d", len(found), len(entries))
	}

	// Verify each entry was found.
	for k, v := range entries {
		hk := crypto.Keccak256Hash([]byte(k))
		got, ok := found[hk]
		if !ok {
			t.Fatalf("entry %q not found in iterator output", k)
		}
		if got != v {
			t.Fatalf("entry %q value = %q, want %q", k, got, v)
		}
	}
}

// --- BinaryIterator: large dataset ---

func TestBinaryIterator_LargeDataset(t *testing.T) {
	bt := NewBinaryTrie()
	n := 200
	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("key-%04d", i))
		val := []byte(fmt.Sprintf("val-%04d", i))
		bt.Put(key, val)
	}

	count := 0
	it := NewBinaryIterator(bt)
	for it.Next() {
		count++
		if it.Key() == nil {
			t.Fatalf("nil key at iteration %d", count)
		}
		if it.Value() == nil {
			t.Fatalf("nil value at iteration %d", count)
		}
	}
	if count != n {
		t.Fatalf("iterated %d entries, want %d", count, n)
	}
}

// --- BinaryIterator: no duplicate keys ---

func TestBinaryIterator_NoDuplicates(t *testing.T) {
	bt := NewBinaryTrie()
	for i := 0; i < 50; i++ {
		bt.Put([]byte(fmt.Sprintf("k%d", i)), []byte(fmt.Sprintf("v%d", i)))
	}

	seen := make(map[types.Hash]bool)
	it := NewBinaryIterator(bt)
	for it.Next() {
		hk := types.BytesToHash(it.Key())
		if seen[hk] {
			t.Fatalf("duplicate key %x", hk)
		}
		seen[hk] = true
	}
}

// --- BinaryIterator: iterator does not modify trie ---

func TestBinaryIterator_DoesNotModifyTrie(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("a"), []byte("1"))
	bt.Put([]byte("b"), []byte("2"))
	hashBefore := bt.Hash()

	it := NewBinaryIterator(bt)
	for it.Next() {
		// Just iterate.
	}

	hashAfter := bt.Hash()
	if hashBefore != hashAfter {
		t.Fatal("iterating should not change the trie hash")
	}
}

// --- BinaryIterator: Next returns false after exhaustion ---

func TestBinaryIterator_ExhaustedNextReturnsFalse(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("x"), []byte("y"))

	it := NewBinaryIterator(bt)
	for it.Next() {
		// Consume all.
	}
	// Calling Next() again should still return false.
	if it.Next() {
		t.Fatal("Next() after exhaustion should return false")
	}
	if it.Next() {
		t.Fatal("Next() after double exhaustion should return false")
	}
}

// --- BinaryIterator: Key and Value are 32-byte hash and original value ---

func TestBinaryIterator_KeyIsHashedKey(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("test-key"), []byte("test-value"))

	it := NewBinaryIterator(bt)
	if !it.Next() {
		t.Fatal("expected at least one iteration")
	}

	expectedKey := crypto.Keccak256Hash([]byte("test-key"))
	if !bytes.Equal(it.Key(), expectedKey[:]) {
		t.Fatalf("Key = %x, want %x", it.Key(), expectedKey[:])
	}
	if !bytes.Equal(it.Value(), []byte("test-value")) {
		t.Fatalf("Value = %q, want %q", it.Value(), "test-value")
	}
}

// --- BinaryIterator: after delete ---

func TestBinaryIterator_AfterDelete(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("a"), []byte("1"))
	bt.Put([]byte("b"), []byte("2"))
	bt.Put([]byte("c"), []byte("3"))

	bt.Delete([]byte("b"))

	count := 0
	it := NewBinaryIterator(bt)
	for it.Next() {
		count++
	}
	if count != 2 {
		t.Fatalf("iterated %d entries after delete, want 2", count)
	}
}

// --- BinaryIterator: depth-first traversal order ---

func TestBinaryIterator_TraversalOrder(t *testing.T) {
	// Verify that the iterator visits left branches before right branches.
	// Insert two keys that diverge at bit 0 (one starts with 0-bit, one with 1-bit).
	bt := NewBinaryTrie()

	// We'll use pre-hashed keys to control the bit pattern.
	// Key with first bit 0: 0x00...
	key0 := types.Hash{0x00, 0x01} // MSB bit 0 = 0
	// Key with first bit 1: 0x80...
	key1 := types.Hash{0x80, 0x01} // MSB bit 0 = 1

	bt.PutHashed(key0, []byte("left"))
	bt.PutHashed(key1, []byte("right"))

	it := NewBinaryIterator(bt)

	if !it.Next() {
		t.Fatal("expected first iteration")
	}
	firstKey := types.BytesToHash(it.Key())

	if !it.Next() {
		t.Fatal("expected second iteration")
	}
	secondKey := types.BytesToHash(it.Key())

	// Left (0-bit) should come before right (1-bit) in depth-first traversal.
	if firstKey != key0 {
		t.Fatalf("first key = %x, want %x (left branch first)", firstKey, key0)
	}
	if secondKey != key1 {
		t.Fatalf("second key = %x, want %x (right branch second)", secondKey, key1)
	}
}

// --- MigrateFromMPT test (related to binary iterator since it uses MPT iterator) ---

func TestMigrateFromMPT_Roundtrip(t *testing.T) {
	mpt := New()
	entries := map[string]string{
		"x": "10", "y": "20", "z": "30",
	}
	for k, v := range entries {
		mpt.Put([]byte(k), []byte(v))
	}

	bt := MigrateFromMPT(mpt)

	// Verify all entries via the binary iterator.
	found := make(map[types.Hash]string)
	it := NewBinaryIterator(bt)
	for it.Next() {
		hk := types.BytesToHash(it.Key())
		found[hk] = string(it.Value())
	}

	for k, v := range entries {
		hk := crypto.Keccak256Hash([]byte(k))
		got, ok := found[hk]
		if !ok {
			t.Fatalf("migrated entry %q not found via iterator", k)
		}
		if got != v {
			t.Fatalf("migrated entry %q value = %q, want %q", k, got, v)
		}
	}
}

func TestMigrateFromMPT_Empty(t *testing.T) {
	mpt := New()
	bt := MigrateFromMPT(mpt)

	it := NewBinaryIterator(bt)
	if it.Next() {
		t.Fatal("migrated empty MPT should produce no iterations")
	}
}
