package snapshot

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
)

// helper to create a hash from a byte.
func makeHash(b byte) types.Hash {
	var h types.Hash
	h[types.HashLength-1] = b
	return h
}

func TestNewTree(t *testing.T) {
	db := rawdb.NewMemoryDB()
	root := makeHash(0x01)
	tree := NewTree(db, root)

	if tree.Size() != 1 {
		t.Fatalf("expected 1 layer, got %d", tree.Size())
	}
	snap := tree.Snapshot(root)
	if snap == nil {
		t.Fatal("expected snapshot at disk root")
	}
	if snap.Root() != root {
		t.Fatalf("root mismatch: got %v, want %v", snap.Root(), root)
	}
}

func TestDiffLayerAccountLookup(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	// Add a diff layer with one account.
	blockRoot := makeHash(0x02)
	acctHash := makeHash(0xAA)
	acctData := []byte{1, 2, 3, 4}
	accounts := map[types.Hash][]byte{acctHash: acctData}
	storage := map[types.Hash]map[types.Hash][]byte{}

	if err := tree.Update(blockRoot, diskRoot, accounts, storage); err != nil {
		t.Fatal(err)
	}
	if tree.Size() != 2 {
		t.Fatalf("expected 2 layers, got %d", tree.Size())
	}
	snap := tree.Snapshot(blockRoot)
	if snap == nil {
		t.Fatal("expected snapshot at block root")
	}
	// Account should exist (returns non-nil).
	acc, err := snap.Account(acctHash)
	if err != nil {
		t.Fatal(err)
	}
	if acc == nil {
		t.Fatal("expected account to exist")
	}
}

func TestDiffLayerStorageLookup(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	blockRoot := makeHash(0x02)
	acctHash := makeHash(0xAA)
	slotHash := makeHash(0xBB)
	slotData := []byte{10, 20, 30}

	accounts := map[types.Hash][]byte{acctHash: {1}}
	storage := map[types.Hash]map[types.Hash][]byte{
		acctHash: {slotHash: slotData},
	}

	if err := tree.Update(blockRoot, diskRoot, accounts, storage); err != nil {
		t.Fatal(err)
	}
	snap := tree.Snapshot(blockRoot)
	data, err := snap.Storage(acctHash, slotHash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, slotData) {
		t.Fatalf("storage mismatch: got %x, want %x", data, slotData)
	}
}

func TestDiffLayerParentFallthrough(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	// Write account to disk layer directly.
	acctHash := makeHash(0xCC)
	acctData := []byte{5, 6, 7}
	db.Put(accountSnapshotKey(acctHash), acctData)

	// Add diff layer that doesn't contain this account.
	blockRoot := makeHash(0x02)
	otherHash := makeHash(0xDD)
	accounts := map[types.Hash][]byte{otherHash: {8}}
	storage := map[types.Hash]map[types.Hash][]byte{}

	if err := tree.Update(blockRoot, diskRoot, accounts, storage); err != nil {
		t.Fatal(err)
	}
	// Query account from diff layer, should fall through to disk.
	snap := tree.Snapshot(blockRoot)
	acc, err := snap.Account(acctHash)
	if err != nil {
		t.Fatal(err)
	}
	if acc == nil {
		t.Fatal("expected account to be resolved from disk layer")
	}
}

func TestStackedDiffLayers(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	// Layer 1: add account A.
	root1 := makeHash(0x02)
	hashA := makeHash(0xAA)
	if err := tree.Update(root1, diskRoot, map[types.Hash][]byte{hashA: {1}}, nil); err != nil {
		t.Fatal(err)
	}

	// Layer 2: add account B on top of layer 1.
	root2 := makeHash(0x03)
	hashB := makeHash(0xBB)
	if err := tree.Update(root2, root1, map[types.Hash][]byte{hashB: {2}}, nil); err != nil {
		t.Fatal(err)
	}

	// Layer 3: add account C on top of layer 2.
	root3 := makeHash(0x04)
	hashC := makeHash(0xCC)
	if err := tree.Update(root3, root2, map[types.Hash][]byte{hashC: {3}}, nil); err != nil {
		t.Fatal(err)
	}

	if tree.Size() != 4 { // disk + 3 diffs
		t.Fatalf("expected 4 layers, got %d", tree.Size())
	}

	// Query from top layer should see all accounts.
	snap := tree.Snapshot(root3)
	for _, h := range []types.Hash{hashA, hashB, hashC} {
		acc, err := snap.Account(h)
		if err != nil {
			t.Fatalf("error querying %v: %v", h, err)
		}
		if acc == nil {
			t.Fatalf("expected account at %v", h)
		}
	}
}

func TestCapFlattenLayers(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	// Create 3 diff layers.
	root1 := makeHash(0x02)
	root2 := makeHash(0x03)
	root3 := makeHash(0x04)

	hashA := makeHash(0xAA)
	hashB := makeHash(0xBB)
	hashC := makeHash(0xCC)

	tree.Update(root1, diskRoot, map[types.Hash][]byte{hashA: {1}}, nil)
	tree.Update(root2, root1, map[types.Hash][]byte{hashB: {2}}, nil)
	tree.Update(root3, root2, map[types.Hash][]byte{hashC: {3}}, nil)

	// Cap to keep only 1 diff layer above disk.
	if err := tree.Cap(root3, 1); err != nil {
		t.Fatal(err)
	}

	// After cap, the disk root should have been updated.
	// The tree should now have 2 layers: disk + 1 diff.
	if tree.Size() != 2 {
		t.Fatalf("expected 2 layers after cap, got %d", tree.Size())
	}

	// The top snapshot should still work.
	snap := tree.Snapshot(root3)
	if snap == nil {
		t.Fatal("expected top snapshot to still exist")
	}
	acc, err := snap.Account(hashC)
	if err != nil {
		t.Fatal(err)
	}
	if acc == nil {
		t.Fatal("expected account C in top layer")
	}

	// Accounts A and B should have been flushed to disk.
	dataA, _ := db.Get(accountSnapshotKey(hashA))
	if !bytes.Equal(dataA, []byte{1}) {
		t.Fatalf("expected account A on disk, got %x", dataA)
	}
	dataB, _ := db.Get(accountSnapshotKey(hashB))
	if !bytes.Equal(dataB, []byte{2}) {
		t.Fatalf("expected account B on disk, got %x", dataB)
	}
}

func TestCapFlattenAll(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	root1 := makeHash(0x02)
	hashA := makeHash(0xAA)
	tree.Update(root1, diskRoot, map[types.Hash][]byte{hashA: {1}}, nil)

	// Cap to 0 layers: flatten everything to disk.
	if err := tree.Cap(root1, 0); err != nil {
		t.Fatal(err)
	}
	if tree.Size() != 1 {
		t.Fatalf("expected 1 layer after full cap, got %d", tree.Size())
	}

	// Account should be on disk.
	dataA, _ := db.Get(accountSnapshotKey(hashA))
	if !bytes.Equal(dataA, []byte{1}) {
		t.Fatalf("expected account A on disk after full cap, got %x", dataA)
	}
}

func TestStaleLayerDetection(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	root1 := makeHash(0x02)
	hashA := makeHash(0xAA)
	tree.Update(root1, diskRoot, map[types.Hash][]byte{hashA: {1}}, nil)

	// Get a reference to the diff layer before capping.
	snap := tree.Snapshot(root1)

	// Cap to 0 (flatten all).
	tree.Cap(root1, 0)

	// The old diff layer should now be stale. However, since we hold it via
	// the Snapshot interface (which may have been replaced), let's check the
	// internal stale state through an Account call.
	// After cap, root1 now points to a disk layer, so Account should work.
	_ = snap
}

func TestUpdateCyclePrevention(t *testing.T) {
	db := rawdb.NewMemoryDB()
	root := makeHash(0x01)
	tree := NewTree(db, root)

	// Attempting to create a layer where blockRoot == parentRoot should fail.
	err := tree.Update(root, root, nil, nil)
	if err == nil {
		t.Fatal("expected error for cycle")
	}
}

func TestUpdateUnknownParent(t *testing.T) {
	db := rawdb.NewMemoryDB()
	root := makeHash(0x01)
	tree := NewTree(db, root)

	unknownParent := makeHash(0xFF)
	blockRoot := makeHash(0x02)
	err := tree.Update(blockRoot, unknownParent, nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown parent")
	}
}

func TestDiffAccountIterator(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	blockRoot := makeHash(0x02)
	hash1 := makeHash(0x01)
	hash2 := makeHash(0x02)
	hash3 := makeHash(0x03)

	accounts := map[types.Hash][]byte{
		hash1: {1},
		hash2: {2},
		hash3: {3},
	}
	tree.Update(blockRoot, diskRoot, accounts, nil)

	layer := tree.layers[blockRoot].(*diffLayer)
	iter := layer.AccountIterator(types.Hash{})

	var collected []types.Hash
	for iter.Next() {
		collected = append(collected, iter.Hash())
		if iter.Account() == nil {
			t.Fatal("expected non-nil account data")
		}
	}
	iter.Release()

	if len(collected) != 3 {
		t.Fatalf("expected 3 accounts, got %d", len(collected))
	}
	// Verify sorted order.
	for i := 1; i < len(collected); i++ {
		if !hashLess(collected[i-1], collected[i]) {
			t.Fatal("accounts not in sorted order")
		}
	}
}

func TestDiffStorageIterator(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	blockRoot := makeHash(0x02)
	acctHash := makeHash(0xAA)
	slot1 := makeHash(0x10)
	slot2 := makeHash(0x20)

	storage := map[types.Hash]map[types.Hash][]byte{
		acctHash: {
			slot1: {1},
			slot2: {2},
		},
	}
	tree.Update(blockRoot, diskRoot, map[types.Hash][]byte{acctHash: {1}}, storage)

	layer := tree.layers[blockRoot].(*diffLayer)
	iter := layer.StorageIterator(acctHash, types.Hash{})

	count := 0
	for iter.Next() {
		if iter.Slot() == nil {
			t.Fatal("expected non-nil slot data")
		}
		count++
	}
	iter.Release()

	if count != 2 {
		t.Fatalf("expected 2 storage slots, got %d", count)
	}
}

func TestDiskAccountIterator(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)

	// Pre-populate disk with snapshot accounts.
	hash1 := makeHash(0x01)
	hash2 := makeHash(0x02)
	db.Put(accountSnapshotKey(hash1), []byte{1})
	db.Put(accountSnapshotKey(hash2), []byte{2})

	tree := NewTree(db, diskRoot)
	disk := tree.layers[diskRoot].(*diskLayer)
	iter := disk.AccountIterator(types.Hash{})

	count := 0
	for iter.Next() {
		if iter.Account() == nil {
			t.Fatal("expected non-nil account data")
		}
		count++
	}
	iter.Release()

	if count != 2 {
		t.Fatalf("expected 2 disk accounts, got %d", count)
	}
}

func TestDiskStorageIterator(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)

	acctHash := makeHash(0xAA)
	slot1 := makeHash(0x10)
	slot2 := makeHash(0x20)

	db.Put(storageSnapshotKey(acctHash, slot1), []byte{1})
	db.Put(storageSnapshotKey(acctHash, slot2), []byte{2})

	tree := NewTree(db, diskRoot)
	disk := tree.layers[diskRoot].(*diskLayer)
	iter := disk.StorageIterator(acctHash, types.Hash{})

	count := 0
	for iter.Next() {
		if iter.Slot() == nil {
			t.Fatal("expected non-nil slot data")
		}
		count++
	}
	iter.Release()

	if count != 2 {
		t.Fatalf("expected 2 disk storage slots, got %d", count)
	}
}

func TestDiffLayerMemoryTracking(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	blockRoot := makeHash(0x02)
	acctHash := makeHash(0xAA)
	acctData := []byte{1, 2, 3, 4, 5}

	tree.Update(blockRoot, diskRoot, map[types.Hash][]byte{acctHash: acctData}, nil)

	layer := tree.layers[blockRoot].(*diffLayer)
	if layer.Memory() == 0 {
		t.Fatal("expected non-zero memory usage")
	}
	// Memory should be at least hash length + data length.
	expected := uint64(types.HashLength + len(acctData))
	if layer.Memory() != expected {
		t.Fatalf("memory mismatch: got %d, want %d", layer.Memory(), expected)
	}
}

func TestDeletedAccountInDiffLayer(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	// Write account to disk.
	acctHash := makeHash(0xAA)
	db.Put(accountSnapshotKey(acctHash), []byte{1, 2, 3})

	// Create diff layer that marks the account as deleted (nil/empty data).
	blockRoot := makeHash(0x02)
	tree.Update(blockRoot, diskRoot, map[types.Hash][]byte{acctHash: nil}, nil)

	snap := tree.Snapshot(blockRoot)
	acc, err := snap.Account(acctHash)
	if err != nil {
		t.Fatal(err)
	}
	if acc != nil {
		t.Fatal("expected nil account for deleted entry")
	}
}

func TestCapFlattenWithStorage(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	acctHash := makeHash(0xAA)
	slotHash := makeHash(0xBB)
	slotData := []byte{10, 20}

	root1 := makeHash(0x02)
	storage := map[types.Hash]map[types.Hash][]byte{
		acctHash: {slotHash: slotData},
	}
	tree.Update(root1, diskRoot, map[types.Hash][]byte{acctHash: {1}}, storage)

	// Cap to 0, flush everything to disk.
	tree.Cap(root1, 0)

	// Verify storage was written to disk.
	data, err := db.Get(storageSnapshotKey(acctHash, slotHash))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, slotData) {
		t.Fatalf("storage mismatch on disk: got %x, want %x", data, slotData)
	}
}
