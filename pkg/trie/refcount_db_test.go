package trie

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

func TestRefCountDB_InsertAndRetrieve(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	data := []byte("node data")
	hash := crypto.Keccak256Hash(data)

	db.InsertNode(hash, data)

	got, err := db.Node(hash)
	if err != nil {
		t.Fatalf("Node() error: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("data mismatch: got %x, want %x", got, data)
	}
}

func TestRefCountDB_ReferenceAndDereference(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	hash := types.Hash{0x01}
	db.InsertNode(hash, []byte("data"))

	if db.RefCount(hash) != 0 {
		t.Fatalf("initial ref count = %d, want 0", db.RefCount(hash))
	}

	db.Reference(hash)
	if db.RefCount(hash) != 1 {
		t.Fatalf("ref count after Reference = %d, want 1", db.RefCount(hash))
	}

	db.Reference(hash)
	if db.RefCount(hash) != 2 {
		t.Fatalf("ref count after 2x Reference = %d, want 2", db.RefCount(hash))
	}

	zeroed, err := db.Dereference(hash)
	if err != nil {
		t.Fatalf("Dereference error: %v", err)
	}
	if zeroed {
		t.Fatal("should not be zeroed yet")
	}
	if db.RefCount(hash) != 1 {
		t.Fatalf("ref count after Dereference = %d, want 1", db.RefCount(hash))
	}

	zeroed, err = db.Dereference(hash)
	if err != nil {
		t.Fatalf("Dereference error: %v", err)
	}
	if !zeroed {
		t.Fatal("should be zeroed now")
	}
}

func TestRefCountDB_DereferenceNegative(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	hash := types.Hash{0x02}
	db.InsertNode(hash, []byte("data"))

	// Dereference without reference should go negative.
	_, err := db.Dereference(hash)
	if err != ErrRefCountNegative {
		t.Fatalf("expected ErrRefCountNegative, got %v", err)
	}
}

func TestRefCountDB_DeleteNode(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	hash := types.Hash{0x03}
	db.InsertNode(hash, []byte("data"))
	db.DeleteNode(hash)

	if db.RefCount(hash) != 0 {
		t.Fatal("ref count should be 0 after delete")
	}
	if db.NodeCount() != 0 {
		t.Fatalf("node count = %d, want 0", db.NodeCount())
	}
}

func TestRefCountDB_UnreferencedNodes(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	h1 := types.Hash{0x10}
	h2 := types.Hash{0x20}
	h3 := types.Hash{0x30}

	db.InsertNode(h1, []byte("a"))
	db.InsertNode(h2, []byte("b"))
	db.InsertNode(h3, []byte("c"))

	// Reference h1 only.
	db.Reference(h1)

	unreferenced := db.UnreferencedNodes()
	if len(unreferenced) != 2 {
		t.Fatalf("expected 2 unreferenced, got %d", len(unreferenced))
	}

	found := make(map[types.Hash]bool)
	for _, h := range unreferenced {
		found[h] = true
	}
	if !found[h2] || !found[h3] {
		t.Fatal("expected h2 and h3 to be unreferenced")
	}
}

func TestRefCountDB_CollectGarbage(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	h1 := types.Hash{0x10}
	h2 := types.Hash{0x20}
	h3 := types.Hash{0x30}

	db.InsertNode(h1, []byte("aaa"))   // 3 bytes
	db.InsertNode(h2, []byte("bbbbb")) // 5 bytes
	db.InsertNode(h3, []byte("cc"))    // 2 bytes

	db.Reference(h1) // only h1 referenced

	removed, freed := db.CollectGarbage()
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}
	if freed != 7 { // 5 + 2
		t.Fatalf("expected 7 freed bytes, got %d", freed)
	}
	if db.NodeCount() != 1 {
		t.Fatalf("expected 1 node remaining, got %d", db.NodeCount())
	}
}

func TestRefCountDB_Size(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	db.InsertNode(types.Hash{1}, []byte("aaa"))
	db.InsertNode(types.Hash{2}, []byte("bbbbb"))

	if db.Size() != 8 {
		t.Fatalf("Size() = %d, want 8", db.Size())
	}
}

func TestRefCountDB_ReferenceMany(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	hashes := []types.Hash{{0x01}, {0x02}, {0x03}}
	for _, h := range hashes {
		db.InsertNode(h, []byte("x"))
	}

	db.ReferenceMany(hashes)

	for _, h := range hashes {
		if db.RefCount(h) != 1 {
			t.Fatalf("ref count for %x = %d, want 1", h, db.RefCount(h))
		}
	}
}

func TestRefCountDB_DereferenceMany(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	hashes := []types.Hash{{0x01}, {0x02}, {0x03}}
	for _, h := range hashes {
		db.InsertNode(h, []byte("x"))
		db.Reference(h)
	}
	// Reference h1 twice so it doesn't reach zero.
	db.Reference(hashes[0])

	zeroed := db.DereferenceMany(hashes)

	if len(zeroed) != 2 {
		t.Fatalf("expected 2 zeroed, got %d", len(zeroed))
	}
}

func TestRefCountDB_Close(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	db.InsertNode(types.Hash{1}, []byte("data"))
	db.Close()

	_, err := db.Node(types.Hash{1})
	if err != ErrDatabaseClosed {
		t.Fatalf("expected ErrDatabaseClosed, got %v", err)
	}
}

func TestRefCountDB_Stats(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	db.InsertNode(types.Hash{1}, []byte("aaa"))
	db.InsertNode(types.Hash{2}, []byte("bb"))
	db.InsertNode(types.Hash{3}, []byte("c"))

	db.Reference(types.Hash{1})
	db.Reference(types.Hash{1})
	db.Reference(types.Hash{2})

	stats := db.Stats()
	if stats.TotalNodes != 3 {
		t.Fatalf("TotalNodes = %d, want 3", stats.TotalNodes)
	}
	if stats.ReferencedNodes != 2 {
		t.Fatalf("ReferencedNodes = %d, want 2", stats.ReferencedNodes)
	}
	if stats.UnreferencedCnt != 1 {
		t.Fatalf("UnreferencedCnt = %d, want 1", stats.UnreferencedCnt)
	}
	if stats.MaxRefCount != 2 {
		t.Fatalf("MaxRefCount = %d, want 2", stats.MaxRefCount)
	}
	if stats.TotalSize != 6 {
		t.Fatalf("TotalSize = %d, want 6", stats.TotalSize)
	}
}

func TestRefCountDB_InsertDuplicatePreservesRef(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	hash := types.Hash{0xAA}
	db.InsertNode(hash, []byte("data"))
	db.Reference(hash)

	// Insert same hash again should not reset ref count.
	db.InsertNode(hash, []byte("data"))
	if db.RefCount(hash) != 1 {
		t.Fatalf("ref count after re-insert = %d, want 1", db.RefCount(hash))
	}
}

func TestRefCountDB_Concurrent(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	var wg sync.WaitGroup
	for i := byte(0); i < 100; i++ {
		wg.Add(1)
		go func(b byte) {
			defer wg.Done()
			h := types.Hash{b}
			db.InsertNode(h, []byte{b})
			db.Reference(h)
			db.RefCount(h)
			db.Node(h)
		}(i)
	}
	wg.Wait()

	if db.NodeCount() != 100 {
		t.Fatalf("node count = %d, want 100", db.NodeCount())
	}
}

func TestRefCountDB_DereferenceNonExistent(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)

	zeroed, err := db.Dereference(types.Hash{0xFF})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if zeroed {
		t.Fatal("should not report zeroed for non-existent hash")
	}
}

func TestRefCountDB_Inner(t *testing.T) {
	inner := NewNodeDatabase(nil)
	db := NewRefCountDB(inner)
	if db.Inner() != inner {
		t.Fatal("Inner() should return the same NodeDatabase")
	}
}
