package rawdb

import (
	"bytes"
	"testing"
)

func TestMemoryDB_PutGet(t *testing.T) {
	db := NewMemoryDB()
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

func TestMemoryDB_Has(t *testing.T) {
	db := NewMemoryDB()
	key := []byte("key")

	has, _ := db.Has(key)
	if has {
		t.Fatal("empty db should not have key")
	}

	db.Put(key, []byte("val"))
	has, _ = db.Has(key)
	if !has {
		t.Fatal("should have key after Put")
	}
}

func TestMemoryDB_Delete(t *testing.T) {
	db := NewMemoryDB()
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

func TestMemoryDB_GetNotFound(t *testing.T) {
	db := NewMemoryDB()
	_, err := db.Get([]byte("missing"))
	if err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestMemoryDB_ValueIsolation(t *testing.T) {
	db := NewMemoryDB()
	key := []byte("key")
	val := []byte("original")
	db.Put(key, val)

	// Mutate original slice.
	val[0] = 'X'

	got, _ := db.Get(key)
	if got[0] == 'X' {
		t.Fatal("Put should copy value, not reference it")
	}

	// Mutate returned slice.
	got[0] = 'Y'
	got2, _ := db.Get(key)
	if got2[0] == 'Y' {
		t.Fatal("Get should return a copy")
	}
}

func TestBatch_Write(t *testing.T) {
	db := NewMemoryDB()
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

func TestBatch_DeleteInBatch(t *testing.T) {
	db := NewMemoryDB()
	db.Put([]byte("key"), []byte("val"))

	batch := db.NewBatch()
	batch.Delete([]byte("key"))
	batch.Write()

	has, _ := db.Has([]byte("key"))
	if has {
		t.Fatal("key should be deleted after batch Write")
	}
}

func TestBatch_Reset(t *testing.T) {
	db := NewMemoryDB()
	batch := db.NewBatch()
	batch.Put([]byte("a"), []byte("1"))
	batch.Reset()
	batch.Write()

	if db.Len() != 0 {
		t.Fatal("reset batch should write nothing")
	}
}

func TestIterator(t *testing.T) {
	db := NewMemoryDB()
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
	// Should be sorted.
	if keys[0] != "pa" || keys[1] != "pb" || keys[2] != "pc" {
		t.Fatalf("keys not sorted: %v", keys)
	}
}

// --- Accessor Tests ---

func TestHeaderAccessors(t *testing.T) {
	db := NewMemoryDB()
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

	// Reverse lookup: hash -> number.
	gotNum, err := ReadHeaderNumber(db, hash)
	if err != nil {
		t.Fatalf("ReadHeaderNumber: %v", err)
	}
	if gotNum != num {
		t.Fatalf("want number %d, got %d", num, gotNum)
	}

	// Delete.
	if err := DeleteHeader(db, num, hash); err != nil {
		t.Fatalf("DeleteHeader: %v", err)
	}
	if HasHeader(db, num, hash) {
		t.Fatal("should not have header after delete")
	}
}

func TestBodyAccessors(t *testing.T) {
	db := NewMemoryDB()
	hash := [32]byte{0xaa}
	num := uint64(100)
	data := []byte("body-rlp")

	if err := WriteBody(db, num, hash, data); err != nil {
		t.Fatalf("WriteBody: %v", err)
	}

	got, err := ReadBody(db, num, hash)
	if err != nil {
		t.Fatalf("ReadBody: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("body data mismatch")
	}

	if !HasBody(db, num, hash) {
		t.Fatal("HasBody: should exist")
	}
}

func TestReceiptAccessors(t *testing.T) {
	db := NewMemoryDB()
	hash := [32]byte{0xbb}
	num := uint64(200)
	data := []byte("receipts-rlp")

	WriteReceipts(db, num, hash, data)
	got, err := ReadReceipts(db, num, hash)
	if err != nil {
		t.Fatalf("ReadReceipts: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("receipt data mismatch")
	}
}

func TestTxLookup(t *testing.T) {
	db := NewMemoryDB()
	txHash := [32]byte{0xcc}
	blockNum := uint64(999)

	WriteTxLookup(db, txHash, blockNum)
	got, err := ReadTxLookup(db, txHash)
	if err != nil {
		t.Fatalf("ReadTxLookup: %v", err)
	}
	if got != blockNum {
		t.Fatalf("want block %d, got %d", blockNum, got)
	}

	DeleteTxLookup(db, txHash)
	_, err = ReadTxLookup(db, txHash)
	if err == nil {
		t.Fatal("should fail after delete")
	}
}

func TestCanonicalHash(t *testing.T) {
	db := NewMemoryDB()
	num := uint64(50)
	hash := [32]byte{0xdd}

	WriteCanonicalHash(db, num, hash)
	got, err := ReadCanonicalHash(db, num)
	if err != nil {
		t.Fatalf("ReadCanonicalHash: %v", err)
	}
	if got != hash {
		t.Fatalf("canonical hash mismatch")
	}

	DeleteCanonicalHash(db, num)
	_, err = ReadCanonicalHash(db, num)
	if err == nil {
		t.Fatal("should fail after delete")
	}
}

func TestHeadPointers(t *testing.T) {
	db := NewMemoryDB()
	hash := [32]byte{0xee}

	WriteHeadHeaderHash(db, hash)
	got, err := ReadHeadHeaderHash(db)
	if err != nil {
		t.Fatalf("ReadHeadHeaderHash: %v", err)
	}
	if got != hash {
		t.Fatal("head header hash mismatch")
	}

	blockHash := [32]byte{0xff}
	WriteHeadBlockHash(db, blockHash)
	got2, err := ReadHeadBlockHash(db)
	if err != nil {
		t.Fatalf("ReadHeadBlockHash: %v", err)
	}
	if got2 != blockHash {
		t.Fatal("head block hash mismatch")
	}
}

func TestCodeAccessors(t *testing.T) {
	db := NewMemoryDB()
	codeHash := [32]byte{0x11, 0x22}
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3} // PUSH1 0, PUSH1 0, RETURN

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
