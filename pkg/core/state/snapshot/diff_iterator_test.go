package snapshot

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/types"
)

func makeDIHash(b byte) types.Hash {
	var h types.Hash
	h[types.HashLength-1] = b
	return h
}

func makeDIPrefixHash(prefix byte, suffix byte) types.Hash {
	var h types.Hash
	h[0] = prefix
	h[types.HashLength-1] = suffix
	return h
}

func makeDILayer(parent snapshot, root types.Hash, accounts map[types.Hash][]byte, storage map[types.Hash]map[types.Hash][]byte) *diffLayer {
	return newDiffLayer(parent, root, accounts, storage)
}

func TestDiffIteratorSingleLayerAccountChanges(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	accounts := map[types.Hash][]byte{
		makeDIHash(0x0A): {1, 2},
		makeDIHash(0x0B): {3, 4},
		makeDIHash(0x0C): nil, // deletion
	}
	dl := makeDILayer(disk, makeDIHash(0x02), accounts, nil)
	it := NewDiffLayerIterator(dl)

	changes := it.AccountChanges()
	if len(changes) != 3 {
		t.Fatalf("expected 3 account changes, got %d", len(changes))
	}

	// Check sorted order.
	for i := 1; i < len(changes); i++ {
		if !hashLess(changes[i-1].Hash, changes[i].Hash) {
			t.Fatal("account changes not sorted")
		}
	}
}

func TestDiffIteratorDeletedAccountChange(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	acctHash := makeDIHash(0x0D)
	accounts := map[types.Hash][]byte{acctHash: nil}
	dl := makeDILayer(disk, makeDIHash(0x02), accounts, nil)
	it := NewDiffLayerIterator(dl)

	changes := it.AccountChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Change != ChangeDeleted {
		t.Fatalf("expected deleted change, got %v", changes[0].Change)
	}
	if changes[0].Data != nil {
		t.Fatal("expected nil data for deletion")
	}
}

func TestDiffIteratorMultiLayerDedup(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	acctHash := makeDIHash(0x0E)
	dl1 := makeDILayer(disk, makeDIHash(0x02), map[types.Hash][]byte{acctHash: {1}}, nil)
	dl2 := makeDILayer(dl1, makeDIHash(0x03), map[types.Hash][]byte{acctHash: {2}}, nil)

	it := NewMultiDiffLayerIterator([]*diffLayer{dl1, dl2})
	changes := it.AccountChanges()

	if len(changes) != 1 {
		t.Fatalf("expected 1 deduped change, got %d", len(changes))
	}
	// Latest layer (dl2) should win.
	if !bytes.Equal(changes[0].Data, []byte{2}) {
		t.Fatalf("expected data [2], got %v", changes[0].Data)
	}
	if changes[0].LayerID != 1 {
		t.Fatalf("expected layer ID 1, got %d", changes[0].LayerID)
	}
}

func TestDiffIteratorStorageChanges(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	acctHash := makeDIHash(0xAA)
	slot1 := makeDIHash(0x10)
	slot2 := makeDIHash(0x20)
	storage := map[types.Hash]map[types.Hash][]byte{
		acctHash: {
			slot1: {10},
			slot2: {20},
		},
	}
	dl := makeDILayer(disk, makeDIHash(0x02), map[types.Hash][]byte{acctHash: {1}}, storage)
	it := NewDiffLayerIterator(dl)

	changes := it.StorageChanges(acctHash)
	if len(changes) != 2 {
		t.Fatalf("expected 2 storage changes, got %d", len(changes))
	}
	// Sorted by slot hash.
	if !hashLess(changes[0].SlotHash, changes[1].SlotHash) {
		t.Fatal("storage changes not sorted by slot hash")
	}
}

func TestDiffIteratorStorageChangesMultiLayer(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	acctHash := makeDIHash(0xBB)
	slotHash := makeDIHash(0x30)

	storage1 := map[types.Hash]map[types.Hash][]byte{
		acctHash: {slotHash: {1}},
	}
	storage2 := map[types.Hash]map[types.Hash][]byte{
		acctHash: {slotHash: {2}},
	}

	dl1 := makeDILayer(disk, makeDIHash(0x02), nil, storage1)
	dl2 := makeDILayer(dl1, makeDIHash(0x03), nil, storage2)

	it := NewMultiDiffLayerIterator([]*diffLayer{dl1, dl2})
	changes := it.StorageChanges(acctHash)

	if len(changes) != 1 {
		t.Fatalf("expected 1 deduped storage change, got %d", len(changes))
	}
	if !bytes.Equal(changes[0].Data, []byte{2}) {
		t.Fatalf("expected latest data [2], got %v", changes[0].Data)
	}
}

func TestDiffIteratorAllStorageChanges(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	acct1 := makeDIHash(0xAA)
	acct2 := makeDIHash(0xBB)
	storage := map[types.Hash]map[types.Hash][]byte{
		acct1: {makeDIHash(0x10): {1}},
		acct2: {makeDIHash(0x20): {2}},
	}
	dl := makeDILayer(disk, makeDIHash(0x02), nil, storage)
	it := NewDiffLayerIterator(dl)

	all := it.AllStorageChanges()
	if len(all) != 2 {
		t.Fatalf("expected 2 total storage changes, got %d", len(all))
	}
}

func TestDiffIteratorBuildChangeSet(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	acctHash := makeDIHash(0xCC)
	slotHash := makeDIHash(0x40)
	accounts := map[types.Hash][]byte{acctHash: {1}}
	storage := map[types.Hash]map[types.Hash][]byte{
		acctHash: {slotHash: {10}},
	}
	dl := makeDILayer(disk, makeDIHash(0x02), accounts, storage)
	it := NewDiffLayerIterator(dl)

	cs := it.BuildChangeSet()
	if cs.AccountCount() != 1 {
		t.Fatalf("expected 1 account in change set, got %d", cs.AccountCount())
	}
	if cs.StorageCount() != 1 {
		t.Fatalf("expected 1 storage in change set, got %d", cs.StorageCount())
	}
}

func TestDiffIteratorPrefixFilter(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	h1 := makeDIPrefixHash(0xAA, 0x01)
	h2 := makeDIPrefixHash(0xAA, 0x02)
	h3 := makeDIPrefixHash(0xBB, 0x03)

	accounts := map[types.Hash][]byte{
		h1: {1},
		h2: {2},
		h3: {3},
	}
	dl := makeDILayer(disk, makeDIHash(0x02), accounts, nil)
	it := NewDiffLayerIterator(dl)
	it.SetPrefix([]byte{0xAA})

	changes := it.AccountChanges()
	if len(changes) != 2 {
		t.Fatalf("expected 2 filtered changes, got %d", len(changes))
	}
	for _, ch := range changes {
		if ch.Hash[0] != 0xAA {
			t.Fatalf("unexpected prefix: %x", ch.Hash[0])
		}
	}
}

func TestDiffIteratorReverseOrder(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	acctHash := makeDIHash(0xDD)
	dl1 := makeDILayer(disk, makeDIHash(0x02), map[types.Hash][]byte{acctHash: {1}}, nil)
	dl2 := makeDILayer(dl1, makeDIHash(0x03), map[types.Hash][]byte{acctHash: {2}}, nil)

	it := NewReverseDiffLayerIterator([]*diffLayer{dl1, dl2})
	changes := it.AccountChanges()

	// In reverse mode with same key, last-processed layer wins.
	// Reverse processes newest first, then oldest. So dl1 (oldest) overwrites dl2.
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	// In reverse iteration, dl1 is processed last so it wins.
	if !bytes.Equal(changes[0].Data, []byte{1}) {
		t.Fatalf("expected data from first layer in reverse, got %v", changes[0].Data)
	}
}

func TestDiffIteratorLayerCount(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	dl1 := makeDILayer(disk, makeDIHash(0x02), nil, nil)
	dl2 := makeDILayer(dl1, makeDIHash(0x03), nil, nil)

	it := NewMultiDiffLayerIterator([]*diffLayer{dl1, dl2})
	if it.LayerCount() != 2 {
		t.Fatalf("expected 2 layers, got %d", it.LayerCount())
	}
}

func TestDiffIteratorIsReverse(t *testing.T) {
	it := NewDiffLayerIterator(nil)
	if it.IsReverse() {
		t.Fatal("expected forward iterator")
	}

	revIt := NewReverseDiffLayerIterator(nil)
	if !revIt.IsReverse() {
		t.Fatal("expected reverse iterator")
	}
}

func TestDiffIteratorEmptyLayers(t *testing.T) {
	it := NewMultiDiffLayerIterator(nil)
	changes := it.AccountChanges()
	if len(changes) != 0 {
		t.Fatalf("expected 0 changes from empty layers, got %d", len(changes))
	}
}

func TestDiffIteratorMergeDiffLayers(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeDIHash(0x01)
	tree := NewTree(db, diskRoot)

	root1 := makeDIHash(0x02)
	root2 := makeDIHash(0x03)

	tree.Update(root1, diskRoot, map[types.Hash][]byte{makeDIHash(0xAA): {1}}, nil)
	tree.Update(root2, root1, map[types.Hash][]byte{makeDIHash(0xBB): {2}}, nil)

	layers := MergeDiffLayers(tree, root2)
	if len(layers) != 2 {
		t.Fatalf("expected 2 diff layers, got %d", len(layers))
	}
	// First layer (oldest) should be root1.
	if layers[0].root != root1 {
		t.Fatalf("expected first layer root %v, got %v", root1, layers[0].root)
	}
}

func TestDiffIteratorMergeDiffLayersUnknownRoot(t *testing.T) {
	db := rawdb.NewMemoryDB()
	tree := NewTree(db, makeDIHash(0x01))

	layers := MergeDiffLayers(tree, makeDIHash(0xFF))
	if layers != nil {
		t.Fatal("expected nil for unknown root")
	}
}

func TestDiffIteratorFilterAccountsByPrefix(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	h1 := makeDIPrefixHash(0xAA, 0x01)
	h2 := makeDIPrefixHash(0xBB, 0x02)

	dl := makeDILayer(disk, makeDIHash(0x02), map[types.Hash][]byte{h1: {1}, h2: {2}}, nil)
	matches := FilterAccountsByPrefix(dl, []byte{0xAA})

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0] != h1 {
		t.Fatalf("expected %v, got %v", h1, matches[0])
	}
}

func TestDiffIteratorFilterAccountsNoPrefix(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	dl := makeDILayer(disk, makeDIHash(0x02), map[types.Hash][]byte{
		makeDIHash(0x01): {1},
		makeDIHash(0x02): {2},
	}, nil)
	matches := FilterAccountsByPrefix(dl, nil)

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches with nil prefix, got %d", len(matches))
	}
}

func TestDiffIteratorChangeTypeString(t *testing.T) {
	cases := []struct {
		ct   ChangeType
		want string
	}{
		{ChangeCreated, "created"},
		{ChangeModified, "modified"},
		{ChangeDeleted, "deleted"},
		{ChangeType(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.ct.String(); got != tc.want {
			t.Fatalf("ChangeType(%d).String() = %q, want %q", tc.ct, got, tc.want)
		}
	}
}

func TestDiffIteratorCopyBytesNil(t *testing.T) {
	result := copyBytes(nil)
	if result != nil {
		t.Fatal("expected nil from copyBytes(nil)")
	}
}

func TestDiffIteratorCopyBytesNonNil(t *testing.T) {
	original := []byte{1, 2, 3}
	result := copyBytes(original)
	if !bytes.Equal(result, original) {
		t.Fatal("copy mismatch")
	}
	// Mutate original; copy should be unaffected.
	original[0] = 99
	if result[0] == 99 {
		t.Fatal("copy is not independent")
	}
}

func TestDiffIteratorStorageChangesNonexistentAccount(t *testing.T) {
	db := rawdb.NewMemoryDB()
	disk := &diskLayer{diskdb: db, root: makeDIHash(0x01)}

	dl := makeDILayer(disk, makeDIHash(0x02), nil, nil)
	it := NewDiffLayerIterator(dl)

	changes := it.StorageChanges(makeDIHash(0xFF))
	if len(changes) != 0 {
		t.Fatalf("expected 0 storage changes for nonexistent account, got %d", len(changes))
	}
}
