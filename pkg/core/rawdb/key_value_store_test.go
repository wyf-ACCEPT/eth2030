package rawdb

import (
	"bytes"
	"errors"
	"testing"
)

func TestMemoryKVStoreBasic(t *testing.T) {
	store := NewMemoryKVStore()

	// Put and Get.
	if err := store.Put([]byte("key1"), []byte("val1")); err != nil {
		t.Fatal(err)
	}
	val, err := store.Get([]byte("key1"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(val, []byte("val1")) {
		t.Errorf("Get = %s, want val1", val)
	}

	// Has.
	ok, err := store.Has([]byte("key1"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("Has(key1) = false, want true")
	}
	ok, err = store.Has([]byte("missing"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("Has(missing) = true, want false")
	}
}

func TestMemoryKVStoreNotFound(t *testing.T) {
	store := NewMemoryKVStore()

	_, err := store.Get([]byte("nope"))
	if !errors.Is(err, ErrKVNotFound) {
		t.Errorf("expected ErrKVNotFound, got %v", err)
	}
}

func TestMemoryKVStoreDelete(t *testing.T) {
	store := NewMemoryKVStore()
	store.Put([]byte("k"), []byte("v"))
	store.Delete([]byte("k"))

	_, err := store.Get([]byte("k"))
	if !errors.Is(err, ErrKVNotFound) {
		t.Errorf("expected ErrKVNotFound after delete, got %v", err)
	}

	if store.Len() != 0 {
		t.Errorf("Len = %d, want 0", store.Len())
	}
}

func TestMemoryKVStoreLen(t *testing.T) {
	store := NewMemoryKVStore()
	store.Put([]byte("a"), []byte("1"))
	store.Put([]byte("b"), []byte("2"))
	store.Put([]byte("c"), []byte("3"))

	if store.Len() != 3 {
		t.Errorf("Len = %d, want 3", store.Len())
	}
}

func TestMemoryKVStoreDataIsolation(t *testing.T) {
	store := NewMemoryKVStore()

	original := []byte("original")
	store.Put([]byte("key"), original)

	// Mutate the original slice after Put.
	original[0] = 0xff

	val, _ := store.Get([]byte("key"))
	if val[0] == 0xff {
		t.Error("store should copy data, not reference original")
	}

	// Mutate the returned value.
	val[0] = 0xee
	val2, _ := store.Get([]byte("key"))
	if val2[0] == 0xee {
		t.Error("store should return copies, not references")
	}
}

func TestWriteBatch(t *testing.T) {
	store := NewMemoryKVStore()
	batch := store.NewBatch()

	batch.Put([]byte("a"), []byte("1"))
	batch.Put([]byte("b"), []byte("2"))
	batch.Delete([]byte("a"))
	batch.Put([]byte("c"), []byte("3"))

	if batch.Len() != 4 {
		t.Errorf("batch Len = %d, want 4", batch.Len())
	}

	// Before Write, store should be empty.
	if store.Len() != 0 {
		t.Error("store should be empty before batch Write")
	}

	// Write the batch.
	if err := batch.Write(); err != nil {
		t.Fatal(err)
	}

	// "a" was put then deleted, so it should not exist.
	if ok, _ := store.Has([]byte("a")); ok {
		t.Error("key 'a' should not exist (deleted in batch)")
	}

	// "b" and "c" should exist.
	if val, err := store.Get([]byte("b")); err != nil || !bytes.Equal(val, []byte("2")) {
		t.Errorf("key 'b': err=%v val=%s", err, val)
	}
	if val, err := store.Get([]byte("c")); err != nil || !bytes.Equal(val, []byte("3")) {
		t.Errorf("key 'c': err=%v val=%s", err, val)
	}
}

func TestWriteBatchDoubleWrite(t *testing.T) {
	store := NewMemoryKVStore()
	batch := store.NewBatch()
	batch.Put([]byte("x"), []byte("y"))

	if err := batch.Write(); err != nil {
		t.Fatal(err)
	}

	// Second Write should return error.
	err := batch.Write()
	if !errors.Is(err, ErrKVBatchApplied) {
		t.Errorf("expected ErrKVBatchApplied, got %v", err)
	}
}

func TestWriteBatchReset(t *testing.T) {
	store := NewMemoryKVStore()
	batch := store.NewBatch()

	batch.Put([]byte("a"), []byte("1"))
	batch.Reset()

	if batch.Len() != 0 {
		t.Errorf("batch Len after Reset = %d, want 0", batch.Len())
	}
	if batch.Size() != 0 {
		t.Errorf("batch Size after Reset = %d, want 0", batch.Size())
	}

	// Reset should allow writing again even after a previous Write.
	batch.Put([]byte("b"), []byte("2"))
	if err := batch.Write(); err != nil {
		t.Fatal(err)
	}
	if val, err := store.Get([]byte("b")); err != nil || !bytes.Equal(val, []byte("2")) {
		t.Errorf("key 'b' after reset+write: err=%v val=%s", err, val)
	}
}

func TestKVIterator(t *testing.T) {
	store := NewMemoryKVStore()
	store.Put([]byte("aa"), []byte("1"))
	store.Put([]byte("ab"), []byte("2"))
	store.Put([]byte("ba"), []byte("3"))
	store.Put([]byte("bb"), []byte("4"))

	// Iterate with prefix "a".
	it := store.NewKVIterator([]byte("a"), nil)
	defer it.Release()

	var keys []string
	for it.Next() {
		keys = append(keys, string(it.Key()))
	}
	if len(keys) != 2 {
		t.Fatalf("iterator returned %d items, want 2", len(keys))
	}
	if keys[0] != "aa" || keys[1] != "ab" {
		t.Errorf("keys = %v, want [aa ab]", keys)
	}
}

func TestKVIteratorWithStart(t *testing.T) {
	store := NewMemoryKVStore()
	store.Put([]byte("a1"), []byte("1"))
	store.Put([]byte("a2"), []byte("2"))
	store.Put([]byte("a3"), []byte("3"))
	store.Put([]byte("b1"), []byte("4"))

	// Prefix "a", start at "a2".
	it := store.NewKVIterator([]byte("a"), []byte("a2"))
	defer it.Release()

	var keys []string
	for it.Next() {
		keys = append(keys, string(it.Key()))
	}
	if len(keys) != 2 {
		t.Fatalf("iterator returned %d items, want 2 (a2, a3)", len(keys))
	}
	if keys[0] != "a2" || keys[1] != "a3" {
		t.Errorf("keys = %v, want [a2 a3]", keys)
	}
}

func TestPrefixedStore(t *testing.T) {
	inner := NewMemoryKVStore()
	ps := NewPrefixedStore(inner, []byte("ns/"))

	ps.Put([]byte("key1"), []byte("val1"))
	ps.Put([]byte("key2"), []byte("val2"))

	// Read through prefixed store.
	val, err := ps.Get([]byte("key1"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(val, []byte("val1")) {
		t.Errorf("Get = %s, want val1", val)
	}

	// Has.
	ok, _ := ps.Has([]byte("key1"))
	if !ok {
		t.Error("Has(key1) = false, want true")
	}

	// Delete.
	ps.Delete([]byte("key1"))
	if ok, _ := ps.Has([]byte("key1")); ok {
		t.Error("key1 should be deleted")
	}

	// The inner store should have the prefixed key.
	if _, err := inner.Get([]byte("ns/key2")); err != nil {
		t.Errorf("inner store missing prefixed key: %v", err)
	}
}

func TestPrefixedStoreIterator(t *testing.T) {
	inner := NewMemoryKVStore()
	ps := NewPrefixedStore(inner, []byte("pfx/"))

	ps.Put([]byte("a"), []byte("1"))
	ps.Put([]byte("b"), []byte("2"))
	ps.Put([]byte("c"), []byte("3"))

	// Also put something directly in inner with different prefix.
	inner.Put([]byte("other/x"), []byte("9"))

	it := ps.NewKVIterator(nil, nil)
	defer it.Release()

	var keys []string
	for it.Next() {
		keys = append(keys, string(it.Key()))
	}
	if len(keys) != 3 {
		t.Fatalf("iterator returned %d items, want 3", len(keys))
	}
	// Keys should have prefix stripped.
	if keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Errorf("keys = %v, want [a b c]", keys)
	}
}

func TestPrefixedStorePrefix(t *testing.T) {
	inner := NewMemoryKVStore()
	ps := NewPrefixedStore(inner, []byte("test/"))

	pfx := ps.Prefix()
	if !bytes.Equal(pfx, []byte("test/")) {
		t.Errorf("Prefix = %s, want test/", pfx)
	}
}

func TestMemoryKVStoreClose(t *testing.T) {
	store := NewMemoryKVStore()
	if err := store.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestPrefixedStoreClose(t *testing.T) {
	inner := NewMemoryKVStore()
	ps := NewPrefixedStore(inner, []byte("p/"))
	if err := ps.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
