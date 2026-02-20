package rawdb

import (
	"bytes"
	"testing"
)

// Tests in this file extend the accessor coverage in rawdb_test.go with
// additional edge cases like not-found errors, data isolation, and key collisions.

func makeHash2(b byte) [32]byte {
	var h [32]byte
	h[31] = b
	return h
}

// --- Header accessors (extended) ---

func TestReadHeader_NotFound(t *testing.T) {
	db := NewMemoryDB()
	_, err := ReadHeader(db, 1, makeHash2(99))
	if err == nil {
		t.Fatal("expected error for non-existent header")
	}
}

func TestReadHeaderNumber_NotFound(t *testing.T) {
	db := NewMemoryDB()
	_, err := ReadHeaderNumber(db, makeHash2(99))
	if err == nil {
		t.Fatal("expected error for non-existent header number")
	}
}

func TestDeleteHeader_RemovesBothEntries(t *testing.T) {
	db := NewMemoryDB()
	num := uint64(10)
	hash := makeHash2(4)

	WriteHeader(db, num, hash, []byte("data"))
	if err := DeleteHeader(db, num, hash); err != nil {
		t.Fatalf("DeleteHeader failed: %v", err)
	}

	if HasHeader(db, num, hash) {
		t.Fatal("header should not exist after delete")
	}

	// Hash -> number mapping should also be deleted.
	_, err := ReadHeaderNumber(db, hash)
	if err == nil {
		t.Fatal("expected error for deleted header number mapping")
	}
}

// --- Body accessors (extended) ---

func TestReadBody_NotFound(t *testing.T) {
	db := NewMemoryDB()
	_, err := ReadBody(db, 1, makeHash2(99))
	if err == nil {
		t.Fatal("expected error for non-existent body")
	}
}

func TestDeleteBody_Roundtrip(t *testing.T) {
	db := NewMemoryDB()
	num := uint64(20)
	hash := makeHash2(7)

	WriteBody(db, num, hash, []byte("data"))
	if err := DeleteBody(db, num, hash); err != nil {
		t.Fatalf("DeleteBody failed: %v", err)
	}
	if HasBody(db, num, hash) {
		t.Fatal("body should not exist after delete")
	}
}

// --- Receipt accessors (extended) ---

func TestReadReceipts_NotFound(t *testing.T) {
	db := NewMemoryDB()
	_, err := ReadReceipts(db, 1, makeHash2(99))
	if err == nil {
		t.Fatal("expected error for non-existent receipts")
	}
}

func TestDeleteReceipts_Roundtrip(t *testing.T) {
	db := NewMemoryDB()
	num := uint64(300)
	hash := makeHash2(9)

	WriteReceipts(db, num, hash, []byte("data"))
	if err := DeleteReceipts(db, num, hash); err != nil {
		t.Fatalf("DeleteReceipts failed: %v", err)
	}
	_, err := ReadReceipts(db, num, hash)
	if err == nil {
		t.Fatal("expected error after deleting receipts")
	}
}

// --- Transaction lookup (extended) ---

func TestReadTxLookup_NotFound(t *testing.T) {
	db := NewMemoryDB()
	_, err := ReadTxLookup(db, makeHash2(99))
	if err == nil {
		t.Fatal("expected error for non-existent tx lookup")
	}
}

// --- Canonical hash (extended) ---

func TestReadCanonicalHash_NotFound(t *testing.T) {
	db := NewMemoryDB()
	_, err := ReadCanonicalHash(db, 999)
	if err == nil {
		t.Fatal("expected error for non-existent canonical hash")
	}
}

func TestDeleteCanonicalHash_Roundtrip(t *testing.T) {
	db := NewMemoryDB()
	num := uint64(500)
	hash := makeHash2(13)

	WriteCanonicalHash(db, num, hash)
	if err := DeleteCanonicalHash(db, num); err != nil {
		t.Fatalf("DeleteCanonicalHash failed: %v", err)
	}
	_, err := ReadCanonicalHash(db, num)
	if err == nil {
		t.Fatal("expected error after deleting canonical hash")
	}
}

// --- Head pointers (extended) ---

func TestReadHeadHeaderHash_NotFound(t *testing.T) {
	db := NewMemoryDB()
	_, err := ReadHeadHeaderHash(db)
	if err == nil {
		t.Fatal("expected error for non-existent head header hash")
	}
}

func TestReadHeadBlockHash_NotFound(t *testing.T) {
	db := NewMemoryDB()
	_, err := ReadHeadBlockHash(db)
	if err == nil {
		t.Fatal("expected error for non-existent head block hash")
	}
}

func TestHeadHashOverwrite(t *testing.T) {
	db := NewMemoryDB()
	hash1 := makeHash2(16)
	hash2 := makeHash2(17)

	WriteHeadHeaderHash(db, hash1)
	WriteHeadHeaderHash(db, hash2)

	got, err := ReadHeadHeaderHash(db)
	if err != nil {
		t.Fatal(err)
	}
	if got != hash2 {
		t.Fatal("head header hash should be overwritten")
	}
}

// --- Code (extended) ---

func TestReadCode_NotFound(t *testing.T) {
	db := NewMemoryDB()
	_, err := ReadCode(db, makeHash2(99))
	if err == nil {
		t.Fatal("expected error for non-existent code")
	}
}

// --- No key collision between data types ---

func TestAccessors_NoKeyCollision(t *testing.T) {
	db := NewMemoryDB()
	hash := makeHash2(21)

	// Write header and body at same number+hash; they should not collide.
	headerData := []byte("header-data")
	bodyData := []byte("body-data")
	receiptData := []byte("receipt-data")

	WriteHeader(db, 1, hash, headerData)
	WriteBody(db, 1, hash, bodyData)
	WriteReceipts(db, 1, hash, receiptData)

	gotHeader, err := ReadHeader(db, 1, hash)
	if err != nil {
		t.Fatal(err)
	}
	gotBody, err := ReadBody(db, 1, hash)
	if err != nil {
		t.Fatal(err)
	}
	gotReceipt, err := ReadReceipts(db, 1, hash)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(gotHeader, headerData) {
		t.Fatal("header data corrupted")
	}
	if !bytes.Equal(gotBody, bodyData) {
		t.Fatal("body data corrupted")
	}
	if !bytes.Equal(gotReceipt, receiptData) {
		t.Fatal("receipt data corrupted")
	}
}

func TestAccessors_DifferentBlockNumbers(t *testing.T) {
	db := NewMemoryDB()
	hash1 := makeHash2(22)
	hash2 := makeHash2(23)

	WriteHeader(db, 1, hash1, []byte("block1"))
	WriteHeader(db, 2, hash2, []byte("block2"))

	got1, _ := ReadHeader(db, 1, hash1)
	got2, _ := ReadHeader(db, 2, hash2)

	if !bytes.Equal(got1, []byte("block1")) {
		t.Fatal("block 1 header corrupted")
	}
	if !bytes.Equal(got2, []byte("block2")) {
		t.Fatal("block 2 header corrupted")
	}
}

// --- Edge case: large block numbers ---

func TestAccessors_LargeBlockNumber(t *testing.T) {
	db := NewMemoryDB()
	num := uint64(1<<63 - 1)
	hash := makeHash2(24)

	WriteHeader(db, num, hash, []byte("big-block"))
	got, err := ReadHeader(db, num, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("big-block")) {
		t.Fatal("large block number header data mismatch")
	}

	gotNum, err := ReadHeaderNumber(db, hash)
	if err != nil {
		t.Fatal(err)
	}
	if gotNum != num {
		t.Fatalf("expected %d, got %d", num, gotNum)
	}
}

// --- Overwrite semantics ---

func TestAccessors_HeaderOverwrite(t *testing.T) {
	db := NewMemoryDB()
	num := uint64(10)
	hash := makeHash2(30)

	WriteHeader(db, num, hash, []byte("v1"))
	WriteHeader(db, num, hash, []byte("v2"))

	got, err := ReadHeader(db, num, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("v2")) {
		t.Fatal("header should be overwritten with latest value")
	}
}

func TestAccessors_TxLookupOverwrite(t *testing.T) {
	db := NewMemoryDB()
	txHash := makeHash2(31)

	WriteTxLookup(db, txHash, 100)
	WriteTxLookup(db, txHash, 200)

	got, err := ReadTxLookup(db, txHash)
	if err != nil {
		t.Fatal(err)
	}
	if got != 200 {
		t.Fatalf("expected block number 200 after overwrite, got %d", got)
	}
}

// --- Block number zero ---

func TestAccessors_BlockNumberZero(t *testing.T) {
	db := NewMemoryDB()
	hash := makeHash2(32)

	WriteHeader(db, 0, hash, []byte("genesis"))
	got, err := ReadHeader(db, 0, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("genesis")) {
		t.Fatal("block 0 header data mismatch")
	}

	gotNum, err := ReadHeaderNumber(db, hash)
	if err != nil {
		t.Fatal(err)
	}
	if gotNum != 0 {
		t.Fatalf("expected block number 0, got %d", gotNum)
	}
}
