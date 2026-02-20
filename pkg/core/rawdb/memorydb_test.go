package rawdb

import (
	"bytes"
	"sync"
	"testing"
)

// Tests in this file extend the basic coverage in rawdb_test.go with
// additional edge cases, concurrency tests, and iterator corner cases.

func TestMemoryDB_DeleteNonExistent(t *testing.T) {
	db := NewMemoryDB()
	// Deleting a non-existent key should not return an error.
	if err := db.Delete([]byte("nonexistent")); err != nil {
		t.Fatalf("Delete of non-existent key should not error: %v", err)
	}
}

func TestMemoryDB_Close(t *testing.T) {
	db := NewMemoryDB()
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestMemoryDB_Len(t *testing.T) {
	db := NewMemoryDB()
	if db.Len() != 0 {
		t.Fatal("expected length 0")
	}

	db.Put([]byte("a"), []byte("1"))
	db.Put([]byte("b"), []byte("2"))
	if db.Len() != 2 {
		t.Fatalf("expected length 2, got %d", db.Len())
	}

	db.Delete([]byte("a"))
	if db.Len() != 1 {
		t.Fatalf("expected length 1, got %d", db.Len())
	}
}

func TestMemoryDB_Overwrite(t *testing.T) {
	db := NewMemoryDB()
	key := []byte("key-ow")

	db.Put(key, []byte("first"))
	db.Put(key, []byte("second"))

	got, _ := db.Get(key)
	if !bytes.Equal(got, []byte("second")) {
		t.Fatalf("expected overwritten value 'second', got %q", got)
	}
	if db.Len() != 1 {
		t.Fatal("overwrite should not increase length")
	}
}

func TestMemoryDB_BatchDeleteMixed(t *testing.T) {
	db := NewMemoryDB()
	db.Put([]byte("dk1"), []byte("dv1"))
	db.Put([]byte("dk2"), []byte("dv2"))

	batch := db.NewBatch()
	batch.Delete([]byte("dk1"))
	batch.Put([]byte("dk3"), []byte("dv3"))
	batch.Write()

	ok, _ := db.Has([]byte("dk1"))
	if ok {
		t.Fatal("dk1 should be deleted by batch")
	}

	ok, _ = db.Has([]byte("dk2"))
	if !ok {
		t.Fatal("dk2 should still exist")
	}

	ok, _ = db.Has([]byte("dk3"))
	if !ok {
		t.Fatal("dk3 should be created by batch")
	}
}

func TestMemoryDB_BatchValueSize(t *testing.T) {
	db := NewMemoryDB()
	batch := db.NewBatch()

	if batch.ValueSize() != 0 {
		t.Fatal("expected initial batch size 0")
	}

	batch.Put([]byte("k"), []byte("v"))
	if batch.ValueSize() != 2 { // len("k") + len("v")
		t.Fatalf("expected batch size 2, got %d", batch.ValueSize())
	}
}

func TestMemoryDB_BatchMultipleWrites(t *testing.T) {
	db := NewMemoryDB()
	batch := db.NewBatch()

	batch.Put([]byte("mk1"), []byte("mv1"))
	batch.Write()

	// Add more and write again.
	batch.Put([]byte("mk2"), []byte("mv2"))
	batch.Write()

	// Both keys from both writes should exist.
	ok1, _ := db.Has([]byte("mk1"))
	ok2, _ := db.Has([]byte("mk2"))
	if !ok1 || !ok2 {
		t.Fatal("both keys should exist after multiple batch writes")
	}
}

func TestMemoryDB_BatchDeleteSize(t *testing.T) {
	db := NewMemoryDB()
	batch := db.NewBatch()
	batch.Delete([]byte("abc"))
	// Delete should also add to size (key length only).
	if batch.ValueSize() != 3 {
		t.Fatalf("expected batch size 3 after delete, got %d", batch.ValueSize())
	}
}

func TestMemoryDB_IteratorEmpty(t *testing.T) {
	db := NewMemoryDB()
	it := db.NewIterator([]byte("prefix-"))
	defer it.Release()

	if it.Next() {
		t.Fatal("expected no items for empty prefix")
	}
}

func TestMemoryDB_IteratorEmptyPrefix(t *testing.T) {
	db := NewMemoryDB()
	db.Put([]byte("a"), []byte("1"))
	db.Put([]byte("b"), []byte("2"))

	it := db.NewIterator([]byte{})
	defer it.Release()

	count := 0
	for it.Next() {
		count++
	}
	if count != 2 {
		t.Fatalf("expected 2 items with empty prefix, got %d", count)
	}
}

func TestMemoryDB_IteratorKeyValueBoundary(t *testing.T) {
	db := NewMemoryDB()
	db.Put([]byte("x-1"), []byte("val1"))

	it := db.NewIterator([]byte("x-"))
	defer it.Release()

	// Before first Next, Key and Value should return nil.
	if it.Key() != nil {
		t.Fatal("Key should be nil before first Next")
	}
	if it.Value() != nil {
		t.Fatal("Value should be nil before first Next")
	}

	if !it.Next() {
		t.Fatal("expected at least one item")
	}

	if !bytes.Equal(it.Key(), []byte("x-1")) {
		t.Fatalf("expected key 'x-1', got %q", it.Key())
	}
	if !bytes.Equal(it.Value(), []byte("val1")) {
		t.Fatalf("expected value 'val1', got %q", it.Value())
	}

	// After exhausting, Next returns false.
	if it.Next() {
		t.Fatal("expected no more items")
	}

	// After exhausting, Key/Value should return nil.
	if it.Key() != nil {
		t.Fatal("Key should be nil after exhaustion")
	}
}

func TestMemoryDB_IteratorDataIsolation(t *testing.T) {
	db := NewMemoryDB()
	db.Put([]byte("z-1"), []byte("val"))

	it := db.NewIterator([]byte("z-"))
	defer it.Release()

	// Modify db after creating iterator.
	db.Put([]byte("z-2"), []byte("val2"))

	// Iterator should have been snapshotted at creation time.
	count := 0
	for it.Next() {
		count++
	}
	if count != 1 {
		t.Fatalf("iterator should only see snapshot at creation, got %d items", count)
	}
}

// --- Concurrent access ---

func TestMemoryDB_ConcurrentAccess(t *testing.T) {
	db := NewMemoryDB()
	var wg sync.WaitGroup

	n := 100
	wg.Add(n * 2)

	// Concurrent writes.
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			key := []byte{byte(i)}
			db.Put(key, key)
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			key := []byte{byte(i)}
			db.Has(key)
			db.Get(key) // may or may not find it
		}(i)
	}

	wg.Wait()

	// All keys should be present after writes complete.
	for i := 0; i < n; i++ {
		ok, _ := db.Has([]byte{byte(i)})
		if !ok {
			t.Fatalf("key %d missing after concurrent writes", i)
		}
	}
}

// --- Empty key and value ---

func TestMemoryDB_EmptyKeyAndValue(t *testing.T) {
	db := NewMemoryDB()

	// Empty key with non-empty value.
	if err := db.Put([]byte{}, []byte("val")); err != nil {
		t.Fatal(err)
	}
	got, err := db.Get([]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("val")) {
		t.Fatal("empty key value mismatch")
	}

	// Non-empty key with empty value.
	if err := db.Put([]byte("k"), []byte{}); err != nil {
		t.Fatal(err)
	}
	got, err = db.Get([]byte("k"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("expected empty value")
	}

	// Has should still return true for empty value.
	ok, _ := db.Has([]byte("k"))
	if !ok {
		t.Fatal("empty value should still register as existing")
	}
}

// --- Large values ---

func TestMemoryDB_LargeValue(t *testing.T) {
	db := NewMemoryDB()
	key := []byte("large-key")
	val := make([]byte, 1<<16) // 64 KB
	for i := range val {
		val[i] = byte(i % 256)
	}

	if err := db.Put(key, val); err != nil {
		t.Fatal(err)
	}

	got, err := db.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, val) {
		t.Fatal("large value data mismatch")
	}
}
