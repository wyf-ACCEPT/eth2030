package rawdb

import (
	"bytes"
	"fmt"
	"testing"
)

func TestBatchWriter_PutAndFlush(t *testing.T) {
	db := NewMemoryDB()
	bw := NewBatchWriter(db)

	if err := bw.Put([]byte("key1"), []byte("val1")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := bw.Put([]byte("key2"), []byte("val2")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Before flush, DB should be empty.
	if db.Len() != 0 {
		t.Fatal("DB should be empty before flush")
	}

	if err := bw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// After flush, both keys should be in DB.
	got, err := db.Get([]byte("key1"))
	if err != nil {
		t.Fatalf("Get key1: %v", err)
	}
	if string(got) != "val1" {
		t.Fatalf("key1 = %q, want %q", got, "val1")
	}

	got, err = db.Get([]byte("key2"))
	if err != nil {
		t.Fatalf("Get key2: %v", err)
	}
	if string(got) != "val2" {
		t.Fatalf("key2 = %q, want %q", got, "val2")
	}
}

func TestBatchWriter_DeleteAndFlush(t *testing.T) {
	db := NewMemoryDB()
	db.Put([]byte("existing"), []byte("value"))

	bw := NewBatchWriter(db)
	if err := bw.Delete([]byte("existing")); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Before flush, key should still exist.
	has, _ := db.Has([]byte("existing"))
	if !has {
		t.Fatal("key should exist before flush")
	}

	if err := bw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// After flush, key should be gone.
	has, _ = db.Has([]byte("existing"))
	if has {
		t.Fatal("key should be deleted after flush")
	}
}

func TestBatchWriter_Size(t *testing.T) {
	db := NewMemoryDB()
	bw := NewBatchWriter(db)

	if bw.Size() != 0 {
		t.Fatalf("initial size = %d, want 0", bw.Size())
	}

	bw.Put([]byte("abc"), []byte("12345"))
	// Size should be len("abc") + len("12345") = 3 + 5 = 8.
	if bw.Size() != 8 {
		t.Fatalf("size = %d, want 8", bw.Size())
	}

	bw.Delete([]byte("xy"))
	// Size increases by len("xy") = 2.
	if bw.Size() != 10 {
		t.Fatalf("size = %d, want 10", bw.Size())
	}
}

func TestBatchWriter_Reset(t *testing.T) {
	db := NewMemoryDB()
	bw := NewBatchWriter(db)

	bw.Put([]byte("key"), []byte("val"))

	if bw.Size() == 0 {
		t.Fatal("size should be > 0 after Put")
	}
	if bw.Len() != 1 {
		t.Fatalf("Len = %d, want 1", bw.Len())
	}

	bw.Reset()

	if bw.Size() != 0 {
		t.Fatalf("size after Reset = %d, want 0", bw.Size())
	}
	if bw.Len() != 0 {
		t.Fatalf("Len after Reset = %d, want 0", bw.Len())
	}

	// Flush after reset should be a no-op.
	bw.Flush()
	if db.Len() != 0 {
		t.Fatal("flush after reset should write nothing")
	}
}

func TestBatchWriter_FlushEmpty(t *testing.T) {
	db := NewMemoryDB()
	bw := NewBatchWriter(db)

	// Flushing an empty batch should succeed without errors.
	if err := bw.Flush(); err != nil {
		t.Fatalf("Flush empty: %v", err)
	}
}

func TestBatchWriter_AutoFlush(t *testing.T) {
	db := NewMemoryDB()
	bw := NewBatchWriter(db)
	// Set a very small max batch size to trigger auto-flush.
	bw.MaxBatchSize = 20

	// Write enough data to exceed the 20-byte threshold.
	bw.Put([]byte("key1"), []byte("value1"))      // 4 + 6 = 10
	bw.Put([]byte("key2"), []byte("value2value2")) // 4 + 11 = 15, total = 25

	// The auto-flush should have been triggered, so data should be in DB.
	if db.Len() == 0 {
		t.Fatal("auto-flush should have written data to DB")
	}

	got, err := db.Get([]byte("key1"))
	if err != nil {
		t.Fatalf("Get key1 after auto-flush: %v", err)
	}
	if string(got) != "value1" {
		t.Fatalf("key1 = %q, want %q", got, "value1")
	}
}

func TestBatchWriter_MaxBatchSizeDefault(t *testing.T) {
	db := NewMemoryDB()
	bw := NewBatchWriter(db)

	if bw.MaxBatchSize != DefaultMaxBatchSize {
		t.Fatalf("default MaxBatchSize = %d, want %d", bw.MaxBatchSize, DefaultMaxBatchSize)
	}
	if DefaultMaxBatchSize != 4*1024*1024 {
		t.Fatalf("DefaultMaxBatchSize = %d, want %d", DefaultMaxBatchSize, 4*1024*1024)
	}
}

func TestBatchWriter_MixedOperations(t *testing.T) {
	db := NewMemoryDB()
	db.Put([]byte("a"), []byte("old_a"))
	db.Put([]byte("b"), []byte("old_b"))

	bw := NewBatchWriter(db)
	bw.Put([]byte("a"), []byte("new_a"))     // overwrite
	bw.Delete([]byte("b"))                    // delete
	bw.Put([]byte("c"), []byte("new_c"))      // insert new
	bw.Flush()

	// Verify results.
	got, _ := db.Get([]byte("a"))
	if string(got) != "new_a" {
		t.Fatalf("a = %q, want %q", got, "new_a")
	}

	has, _ := db.Has([]byte("b"))
	if has {
		t.Fatal("b should be deleted")
	}

	got, _ = db.Get([]byte("c"))
	if string(got) != "new_c" {
		t.Fatalf("c = %q, want %q", got, "new_c")
	}
}

func TestBatchWriter_CloseFlushesRemaining(t *testing.T) {
	db := NewMemoryDB()
	bw := NewBatchWriter(db)

	bw.Put([]byte("closing"), []byte("data"))

	if err := bw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Data should be flushed by Close.
	got, err := db.Get([]byte("closing"))
	if err != nil {
		t.Fatalf("Get after Close: %v", err)
	}
	if string(got) != "data" {
		t.Fatalf("closing = %q, want %q", got, "data")
	}
}

func TestBatchWriter_OperationsAfterClose(t *testing.T) {
	db := NewMemoryDB()
	bw := NewBatchWriter(db)
	bw.Close()

	if err := bw.Put([]byte("k"), []byte("v")); err != ErrBatchClosed {
		t.Fatalf("Put after close: err = %v, want ErrBatchClosed", err)
	}
	if err := bw.Delete([]byte("k")); err != ErrBatchClosed {
		t.Fatalf("Delete after close: err = %v, want ErrBatchClosed", err)
	}
	if err := bw.Flush(); err != ErrBatchClosed {
		t.Fatalf("Flush after close: err = %v, want ErrBatchClosed", err)
	}
}

func TestBatchWriter_ValueIsolation(t *testing.T) {
	db := NewMemoryDB()
	bw := NewBatchWriter(db)

	key := []byte("key")
	val := []byte("original")
	bw.Put(key, val)

	// Mutate the original slices.
	key[0] = 'X'
	val[0] = 'Y'

	bw.Flush()

	// The original key should have been used, not the mutated one.
	got, err := db.Get([]byte("key"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, []byte("original")) {
		t.Fatalf("value = %q, want %q (value isolation failed)", got, "original")
	}
}

func TestBatchWriter_MultipleFlushes(t *testing.T) {
	db := NewMemoryDB()
	bw := NewBatchWriter(db)

	// First batch.
	bw.Put([]byte("a"), []byte("1"))
	bw.Flush()

	// Second batch.
	bw.Put([]byte("b"), []byte("2"))
	bw.Flush()

	// Third batch.
	bw.Put([]byte("c"), []byte("3"))
	bw.Flush()

	if db.Len() != 3 {
		t.Fatalf("DB should have 3 entries, got %d", db.Len())
	}
}

func TestBatchWriter_LargeDataFlush(t *testing.T) {
	db := NewMemoryDB()
	bw := NewBatchWriter(db)

	// Insert 100 key-value pairs.
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key_%04d", i))
		val := bytes.Repeat([]byte{byte(i)}, 100)
		if err := bw.Put(key, val); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	if err := bw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if db.Len() != 100 {
		t.Fatalf("DB should have 100 entries, got %d", db.Len())
	}

	// Spot-check a value.
	got, _ := db.Get([]byte("key_0042"))
	expected := bytes.Repeat([]byte{42}, 100)
	if !bytes.Equal(got, expected) {
		t.Fatalf("key_0042 value mismatch")
	}
}

func TestBatchWriter_AutoFlushDelete(t *testing.T) {
	db := NewMemoryDB()
	// Pre-populate many keys.
	for i := 0; i < 50; i++ {
		db.Put([]byte(fmt.Sprintf("k%d", i)), []byte("v"))
	}

	bw := NewBatchWriter(db)
	bw.MaxBatchSize = 50 // Very small to trigger auto-flush on deletes.

	for i := 0; i < 50; i++ {
		bw.Delete([]byte(fmt.Sprintf("k%d", i)))
	}

	// Some or all deletes should have been auto-flushed.
	// Flush remaining.
	bw.Flush()

	if db.Len() != 0 {
		t.Fatalf("DB should be empty after deleting all keys, got %d", db.Len())
	}
}

func TestBatchWriter_FlushResetsSize(t *testing.T) {
	db := NewMemoryDB()
	bw := NewBatchWriter(db)

	bw.Put([]byte("key"), []byte("value"))
	if bw.Size() == 0 {
		t.Fatal("size should be > 0 before flush")
	}

	bw.Flush()
	if bw.Size() != 0 {
		t.Fatalf("size after flush = %d, want 0", bw.Size())
	}
	if bw.Len() != 0 {
		t.Fatalf("Len after flush = %d, want 0", bw.Len())
	}
}
