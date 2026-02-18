package rawdb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

// newTestFileDB creates a FileDB in a temporary directory. Returns the
// database and a cleanup function.
func newTestFileDB(t *testing.T) (*FileDB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, dir
}

// --- Basic CRUD ---

func TestFileDB_PutGet(t *testing.T) {
	db, _ := newTestFileDB(t)

	key := []byte("testkey")
	val := []byte("testvalue")

	if err := db.Put(key, val); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, val) {
		t.Fatalf("want %q, got %q", val, got)
	}
}

func TestFileDB_Has(t *testing.T) {
	db, _ := newTestFileDB(t)
	key := []byte("key")

	has, err := db.Has(key)
	if err != nil {
		t.Fatalf("Has error: %v", err)
	}
	if has {
		t.Fatal("empty db should not have key")
	}

	db.Put(key, []byte("val"))
	has, err = db.Has(key)
	if err != nil {
		t.Fatalf("Has error: %v", err)
	}
	if !has {
		t.Fatal("should have key after Put")
	}
}

func TestFileDB_Delete(t *testing.T) {
	db, _ := newTestFileDB(t)
	key := []byte("key")
	db.Put(key, []byte("val"))

	if err := db.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	has, _ := db.Has(key)
	if has {
		t.Fatal("should not have key after Delete")
	}
}

func TestFileDB_GetNotFound(t *testing.T) {
	db, _ := newTestFileDB(t)
	_, err := db.Get([]byte("missing"))
	if err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFileDB_DeleteNonExistent(t *testing.T) {
	db, _ := newTestFileDB(t)
	// Deleting a key that doesn't exist should not error.
	if err := db.Delete([]byte("nonexistent")); err != nil {
		t.Fatalf("Delete nonexistent: %v", err)
	}
}

func TestFileDB_ValueIsolation(t *testing.T) {
	db, _ := newTestFileDB(t)
	key := []byte("key")
	val := []byte("original")
	db.Put(key, val)

	// Mutate original slice -- should not affect stored value.
	val[0] = 'X'

	got, _ := db.Get(key)
	if got[0] == 'X' {
		t.Fatal("Put should copy value, not reference it")
	}

	// Mutate returned slice -- should not affect stored value.
	got[0] = 'Y'
	got2, _ := db.Get(key)
	if got2[0] == 'Y' {
		t.Fatal("Get should return a copy")
	}
}

func TestFileDB_Overwrite(t *testing.T) {
	db, _ := newTestFileDB(t)
	key := []byte("key")

	db.Put(key, []byte("first"))
	db.Put(key, []byte("second"))

	got, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("want 'second', got %q", got)
	}
}

func TestFileDB_EmptyValue(t *testing.T) {
	db, _ := newTestFileDB(t)
	key := []byte("empty")

	if err := db.Put(key, []byte{}); err != nil {
		t.Fatalf("Put empty: %v", err)
	}

	got, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %q", got)
	}

	has, _ := db.Has(key)
	if !has {
		t.Fatal("should have key with empty value")
	}
}

func TestFileDB_BinaryKeys(t *testing.T) {
	db, _ := newTestFileDB(t)
	// Test with binary keys containing null bytes.
	key := []byte{0x00, 0x01, 0xff, 0xfe}
	val := []byte("binary-key-value")

	if err := db.Put(key, val); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, val) {
		t.Fatalf("binary key mismatch")
	}
}

func TestFileDB_LargeValue(t *testing.T) {
	db, _ := newTestFileDB(t)
	key := []byte("large")
	val := make([]byte, 1<<16) // 64KB
	for i := range val {
		val[i] = byte(i % 256)
	}

	if err := db.Put(key, val); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, val) {
		t.Fatal("large value mismatch")
	}
}

// --- Persistence ---

func TestFileDB_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Open, write, close.
	db, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB: %v", err)
	}
	db.Put([]byte("persist-key"), []byte("persist-value"))
	db.Put([]byte("another"), []byte("data"))
	db.Close()

	// Reopen and verify data survived.
	db2, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB reopen: %v", err)
	}
	defer db2.Close()

	got, err := db2.Get([]byte("persist-key"))
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if string(got) != "persist-value" {
		t.Fatalf("want 'persist-value', got %q", got)
	}

	got2, err := db2.Get([]byte("another"))
	if err != nil {
		t.Fatalf("Get 'another': %v", err)
	}
	if string(got2) != "data" {
		t.Fatalf("want 'data', got %q", got2)
	}
}

func TestFileDB_PersistenceAfterDelete(t *testing.T) {
	dir := t.TempDir()

	db, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB: %v", err)
	}
	db.Put([]byte("a"), []byte("1"))
	db.Put([]byte("b"), []byte("2"))
	db.Delete([]byte("a"))
	db.Close()

	db2, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB reopen: %v", err)
	}
	defer db2.Close()

	has, _ := db2.Has([]byte("a"))
	if has {
		t.Fatal("deleted key should not persist")
	}
	got, err := db2.Get([]byte("b"))
	if err != nil {
		t.Fatalf("Get 'b': %v", err)
	}
	if string(got) != "2" {
		t.Fatalf("want '2', got %q", got)
	}
}

func TestFileDB_PersistenceOverwrite(t *testing.T) {
	dir := t.TempDir()

	db, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB: %v", err)
	}
	db.Put([]byte("key"), []byte("v1"))
	db.Put([]byte("key"), []byte("v2"))
	db.Close()

	db2, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB reopen: %v", err)
	}
	defer db2.Close()

	got, _ := db2.Get([]byte("key"))
	if string(got) != "v2" {
		t.Fatalf("want 'v2', got %q", got)
	}
}

// --- Batch ---

func TestFileDB_BatchWrite(t *testing.T) {
	db, _ := newTestFileDB(t)
	batch := db.NewBatch()

	batch.Put([]byte("a"), []byte("1"))
	batch.Put([]byte("b"), []byte("2"))
	batch.Put([]byte("c"), []byte("3"))

	// Before Write, DB should be empty.
	if db.Len() != 0 {
		t.Fatal("batch should not write until Write() called")
	}

	if err := batch.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if db.Len() != 3 {
		t.Fatalf("want 3 entries, got %d", db.Len())
	}

	got, _ := db.Get([]byte("b"))
	if string(got) != "2" {
		t.Fatalf("want '2', got %q", got)
	}
}

func TestFileDB_BatchDelete(t *testing.T) {
	db, _ := newTestFileDB(t)
	db.Put([]byte("key"), []byte("val"))

	batch := db.NewBatch()
	batch.Delete([]byte("key"))
	batch.Write()

	has, _ := db.Has([]byte("key"))
	if has {
		t.Fatal("key should be deleted after batch Write")
	}
}

func TestFileDB_BatchMixed(t *testing.T) {
	db, _ := newTestFileDB(t)
	db.Put([]byte("existing"), []byte("old"))

	batch := db.NewBatch()
	batch.Put([]byte("new-key"), []byte("new-val"))
	batch.Delete([]byte("existing"))
	batch.Put([]byte("another"), []byte("val"))

	if err := batch.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	has, _ := db.Has([]byte("existing"))
	if has {
		t.Fatal("'existing' should be deleted")
	}
	got, _ := db.Get([]byte("new-key"))
	if string(got) != "new-val" {
		t.Fatalf("want 'new-val', got %q", got)
	}
	got2, _ := db.Get([]byte("another"))
	if string(got2) != "val" {
		t.Fatalf("want 'val', got %q", got2)
	}
}

func TestFileDB_BatchReset(t *testing.T) {
	db, _ := newTestFileDB(t)
	batch := db.NewBatch()
	batch.Put([]byte("a"), []byte("1"))
	batch.Reset()
	batch.Write()

	if db.Len() != 0 {
		t.Fatal("reset batch should write nothing")
	}
}

func TestFileDB_BatchValueSize(t *testing.T) {
	db, _ := newTestFileDB(t)
	batch := db.NewBatch()

	if batch.ValueSize() != 0 {
		t.Fatalf("empty batch size: want 0, got %d", batch.ValueSize())
	}

	batch.Put([]byte("abc"), []byte("12345"))
	// Size = len("abc") + len("12345") = 3 + 5 = 8
	if batch.ValueSize() != 8 {
		t.Fatalf("batch size: want 8, got %d", batch.ValueSize())
	}

	batch.Delete([]byte("xy"))
	// Size = 8 + len("xy") = 8 + 2 = 10
	if batch.ValueSize() != 10 {
		t.Fatalf("batch size after delete: want 10, got %d", batch.ValueSize())
	}

	batch.Reset()
	if batch.ValueSize() != 0 {
		t.Fatalf("batch size after reset: want 0, got %d", batch.ValueSize())
	}
}

func TestFileDB_BatchPersistence(t *testing.T) {
	dir := t.TempDir()

	db, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB: %v", err)
	}

	batch := db.NewBatch()
	batch.Put([]byte("batch-a"), []byte("val-a"))
	batch.Put([]byte("batch-b"), []byte("val-b"))
	batch.Write()
	db.Close()

	// Reopen and verify batch data persisted.
	db2, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB reopen: %v", err)
	}
	defer db2.Close()

	got, err := db2.Get([]byte("batch-a"))
	if err != nil {
		t.Fatalf("Get batch-a: %v", err)
	}
	if string(got) != "val-a" {
		t.Fatalf("want 'val-a', got %q", got)
	}
}

// --- Iterator ---

func TestFileDB_Iterator(t *testing.T) {
	db, _ := newTestFileDB(t)
	db.Put([]byte("pa"), []byte("1"))
	db.Put([]byte("pb"), []byte("2"))
	db.Put([]byte("pc"), []byte("3"))
	db.Put([]byte("xa"), []byte("4")) // different prefix

	iter := db.NewIterator([]byte("p"))
	var keys []string
	for iter.Next() {
		keys = append(keys, string(iter.Key()))
	}
	iter.Release()

	if len(keys) != 3 {
		t.Fatalf("want 3 keys with prefix 'p', got %d: %v", len(keys), keys)
	}
	if keys[0] != "pa" || keys[1] != "pb" || keys[2] != "pc" {
		t.Fatalf("keys not sorted: %v", keys)
	}
}

func TestFileDB_IteratorEmpty(t *testing.T) {
	db, _ := newTestFileDB(t)
	iter := db.NewIterator([]byte("p"))
	if iter.Next() {
		t.Fatal("empty iterator should return false")
	}
	if iter.Key() != nil {
		t.Fatal("Key() on exhausted iterator should return nil")
	}
	if iter.Value() != nil {
		t.Fatal("Value() on exhausted iterator should return nil")
	}
	iter.Release()
}

func TestFileDB_IteratorNilPrefix(t *testing.T) {
	db, _ := newTestFileDB(t)
	db.Put([]byte("a"), []byte("1"))
	db.Put([]byte("b"), []byte("2"))
	db.Put([]byte("c"), []byte("3"))

	// Nil prefix should iterate over all keys.
	iter := db.NewIterator(nil)
	count := 0
	for iter.Next() {
		count++
	}
	iter.Release()

	if count != 3 {
		t.Fatalf("want 3, got %d", count)
	}
}

func TestFileDB_IteratorValues(t *testing.T) {
	db, _ := newTestFileDB(t)
	db.Put([]byte("k1"), []byte("v1"))
	db.Put([]byte("k2"), []byte("v2"))

	iter := db.NewIterator([]byte("k"))
	var pairs []string
	for iter.Next() {
		pairs = append(pairs, fmt.Sprintf("%s=%s", iter.Key(), iter.Value()))
	}
	iter.Release()

	if len(pairs) != 2 {
		t.Fatalf("want 2 pairs, got %d", len(pairs))
	}
	if pairs[0] != "k1=v1" || pairs[1] != "k2=v2" {
		t.Fatalf("unexpected pairs: %v", pairs)
	}
}

func TestFileDB_IteratorSnapshot(t *testing.T) {
	db, _ := newTestFileDB(t)
	db.Put([]byte("k1"), []byte("v1"))
	db.Put([]byte("k2"), []byte("v2"))

	// Create iterator -- it should capture a snapshot.
	iter := db.NewIterator(nil)

	// Modify the database while iterating.
	db.Put([]byte("k3"), []byte("v3"))
	db.Delete([]byte("k1"))

	// Iterator should see the original data.
	count := 0
	for iter.Next() {
		count++
	}
	iter.Release()

	if count != 2 {
		t.Fatalf("iterator should see snapshot, want 2, got %d", count)
	}
}

// --- WAL crash recovery ---

func TestFileDB_WALRecovery(t *testing.T) {
	dir := t.TempDir()

	// Write some data normally.
	db, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB: %v", err)
	}
	db.Put([]byte("safe"), []byte("committed"))
	db.Close()

	// Simulate a crash by writing a partial WAL entry (no commit marker).
	walPath := filepath.Join(dir, "wal")
	f, err := os.OpenFile(walPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	// Write a put record without a commit marker.
	f.Write([]byte{walPut})
	kl := make([]byte, 4)
	binary.BigEndian.PutUint32(kl, 6)
	f.Write(kl) // key length
	f.Write([]byte("unsafe"))
	vl := make([]byte, 4)
	binary.BigEndian.PutUint32(vl, 4)
	f.Write(vl) // value length
	f.Write([]byte("data"))
	// Intentionally NOT writing walCommit.
	f.Close()

	// Reopen -- the uncommitted entry should be discarded.
	db2, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB after crash: %v", err)
	}
	defer db2.Close()

	// Committed data should still be there.
	got, err := db2.Get([]byte("safe"))
	if err != nil {
		t.Fatalf("Get 'safe': %v", err)
	}
	if string(got) != "committed" {
		t.Fatalf("want 'committed', got %q", got)
	}

	// Uncommitted data should not be there.
	has, _ := db2.Has([]byte("unsafe"))
	if has {
		t.Fatal("uncommitted WAL entry should be discarded")
	}
}

func TestFileDB_WALRecoveryCommitted(t *testing.T) {
	dir := t.TempDir()

	// Create empty db and close it.
	db, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB: %v", err)
	}
	db.Close()

	// Manually write a fully committed WAL entry.
	walPath := filepath.Join(dir, "wal")
	f, err := os.OpenFile(walPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}

	// Put record: type + keyLen + key + valLen + val
	f.Write([]byte{walPut})
	kl := make([]byte, 4)
	binary.BigEndian.PutUint32(kl, 8)
	f.Write(kl)
	f.Write([]byte("wal-key1"))
	vl := make([]byte, 4)
	binary.BigEndian.PutUint32(vl, 8)
	f.Write(vl)
	f.Write([]byte("wal-val1"))
	// Commit marker.
	f.Write([]byte{walCommit})
	f.Close()

	// Reopen -- the committed entry should be replayed.
	db2, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB: %v", err)
	}
	defer db2.Close()

	got, err := db2.Get([]byte("wal-key1"))
	if err != nil {
		t.Fatalf("Get wal-key1: %v", err)
	}
	if string(got) != "wal-val1" {
		t.Fatalf("want 'wal-val1', got %q", got)
	}
}

// --- Concurrency ---

func TestFileDB_ConcurrentReadWrite(t *testing.T) {
	db, _ := newTestFileDB(t)

	const goroutines = 10
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := []byte(fmt.Sprintf("g%d-k%d", id, i))
				val := []byte(fmt.Sprintf("v%d-%d", id, i))
				if err := db.Put(key, val); err != nil {
					t.Errorf("Put: %v", err)
					return
				}
				got, err := db.Get(key)
				if err != nil {
					t.Errorf("Get: %v", err)
					return
				}
				if !bytes.Equal(got, val) {
					t.Errorf("want %q, got %q", val, got)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	// All keys should be present.
	if db.Len() != goroutines*opsPerGoroutine {
		t.Fatalf("want %d entries, got %d", goroutines*opsPerGoroutine, db.Len())
	}
}

func TestFileDB_ConcurrentBatch(t *testing.T) {
	db, _ := newTestFileDB(t)

	const goroutines = 5
	const keysPerBatch = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			batch := db.NewBatch()
			for i := 0; i < keysPerBatch; i++ {
				key := []byte(fmt.Sprintf("b%d-k%d", id, i))
				val := []byte(fmt.Sprintf("v%d-%d", id, i))
				batch.Put(key, val)
			}
			if err := batch.Write(); err != nil {
				t.Errorf("batch Write: %v", err)
			}
		}(g)
	}
	wg.Wait()

	if db.Len() != goroutines*keysPerBatch {
		t.Fatalf("want %d entries, got %d", goroutines*keysPerBatch, db.Len())
	}
}

// --- File locking ---

func TestFileDB_FileLock(t *testing.T) {
	dir := t.TempDir()

	db, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB: %v", err)
	}
	defer db.Close()

	// Attempting to open a second instance in the same directory should fail
	// because the file lock is held.
	_, err = NewFileDB(dir)
	if err == nil {
		t.Fatal("expected error opening second instance, got nil")
	}
}

// --- Close behavior ---

func TestFileDB_CloseThenOps(t *testing.T) {
	db, _ := newTestFileDB(t)
	db.Close()

	if err := db.Put([]byte("k"), []byte("v")); err == nil {
		t.Fatal("Put on closed db should error")
	}
	_, err := db.Get([]byte("k"))
	if err == nil {
		t.Fatal("Get on closed db should error")
	}
	_, err = db.Has([]byte("k"))
	if err == nil {
		t.Fatal("Has on closed db should error")
	}
	if err := db.Delete([]byte("k")); err == nil {
		t.Fatal("Delete on closed db should error")
	}
}

func TestFileDB_DoubleClose(t *testing.T) {
	db, _ := newTestFileDB(t)
	if err := db.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second close should not panic or error.
	if err := db.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// --- Len ---

func TestFileDB_Len(t *testing.T) {
	db, _ := newTestFileDB(t)

	if db.Len() != 0 {
		t.Fatalf("empty db: want 0, got %d", db.Len())
	}

	db.Put([]byte("a"), []byte("1"))
	db.Put([]byte("b"), []byte("2"))
	if db.Len() != 2 {
		t.Fatalf("after 2 puts: want 2, got %d", db.Len())
	}

	db.Delete([]byte("a"))
	if db.Len() != 1 {
		t.Fatalf("after delete: want 1, got %d", db.Len())
	}
}

// --- Temporary file cleanup ---

func TestFileDB_TmpFileCleanup(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	os.MkdirAll(dataDir, 0o755)

	// Place a stale .tmp file.
	tmpPath := filepath.Join(dataDir, "deadbeef.tmp")
	os.WriteFile(tmpPath, []byte("stale"), 0o644)

	db, err := NewFileDB(dir)
	if err != nil {
		t.Fatalf("NewFileDB: %v", err)
	}
	defer db.Close()

	// The .tmp file should have been cleaned up.
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatal("stale .tmp file should be cleaned up on open")
	}
}

// --- Many keys ---

func TestFileDB_ManyKeys(t *testing.T) {
	db, _ := newTestFileDB(t)
	const n = 500

	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("key-%04d", i))
		val := []byte(fmt.Sprintf("val-%04d", i))
		if err := db.Put(key, val); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	if db.Len() != n {
		t.Fatalf("want %d, got %d", n, db.Len())
	}

	// Verify all keys via iteration with prefix.
	iter := db.NewIterator([]byte("key-"))
	count := 0
	var prevKey string
	for iter.Next() {
		k := string(iter.Key())
		if k <= prevKey && prevKey != "" {
			t.Fatalf("keys not sorted: %q after %q", k, prevKey)
		}
		prevKey = k
		count++
	}
	iter.Release()

	if count != n {
		t.Fatalf("iterator count: want %d, got %d", n, count)
	}
}

// --- Accessor integration ---

func TestFileDB_HeaderAccessors(t *testing.T) {
	db, _ := newTestFileDB(t)
	hash := [32]byte{0x01, 0x02, 0x03}
	num := uint64(42)
	data := []byte("header-rlp-data")

	if HasHeader(db, num, hash) {
		t.Fatal("should not have header before write")
	}

	if err := WriteHeader(db, num, hash, data); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}

	if !HasHeader(db, num, hash) {
		t.Fatal("should have header after write")
	}

	got, err := ReadHeader(db, num, hash)
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("header data mismatch")
	}

	gotNum, err := ReadHeaderNumber(db, hash)
	if err != nil {
		t.Fatalf("ReadHeaderNumber: %v", err)
	}
	if gotNum != num {
		t.Fatalf("want number %d, got %d", num, gotNum)
	}

	if err := DeleteHeader(db, num, hash); err != nil {
		t.Fatalf("DeleteHeader: %v", err)
	}
	if HasHeader(db, num, hash) {
		t.Fatal("should not have header after delete")
	}
}

func TestFileDB_CodeAccessors(t *testing.T) {
	db, _ := newTestFileDB(t)
	codeHash := [32]byte{0x11, 0x22}
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}

	WriteCode(db, codeHash, code)
	if !HasCode(db, codeHash) {
		t.Fatal("should have code")
	}
	got, err := ReadCode(db, codeHash)
	if err != nil {
		t.Fatalf("ReadCode: %v", err)
	}
	if !bytes.Equal(got, code) {
		t.Fatal("code mismatch")
	}

	DeleteCode(db, codeHash)
	if HasCode(db, codeHash) {
		t.Fatal("should not have code after delete")
	}
}

// --- Database interface compliance ---

func TestFileDB_ImplementsDatabase(t *testing.T) {
	var _ Database = (*FileDB)(nil)
}

// --- Iterator sorted order with many prefixes ---

func TestFileDB_IteratorSorted(t *testing.T) {
	db, _ := newTestFileDB(t)

	// Insert keys in reverse order.
	keys := []string{"zz", "yy", "xx", "ww", "vv"}
	for _, k := range keys {
		db.Put([]byte(k), []byte("v"))
	}

	iter := db.NewIterator(nil)
	var got []string
	for iter.Next() {
		got = append(got, string(iter.Key()))
	}
	iter.Release()

	sorted := make([]string, len(keys))
	copy(sorted, keys)
	sort.Strings(sorted)

	if len(got) != len(sorted) {
		t.Fatalf("want %d, got %d", len(sorted), len(got))
	}
	for i := range got {
		if got[i] != sorted[i] {
			t.Fatalf("position %d: want %q, got %q", i, sorted[i], got[i])
		}
	}
}
