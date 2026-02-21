package snapshot

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/types"
)

// makeLMHash creates a hash from a single byte for layer merger tests.
func makeLMHash(b byte) types.Hash {
	var h types.Hash
	h[types.HashLength-1] = b
	return h
}

func makeLMDisk() (*diskLayer, *rawdb.MemoryDB) {
	db := rawdb.NewMemoryDB()
	return &diskLayer{diskdb: db, root: makeLMHash(0x01)}, db
}

func makeLMDiff(parent snapshot, root byte, accounts map[types.Hash][]byte, storage map[types.Hash]map[types.Hash][]byte) *diffLayer {
	return newDiffLayer(parent, makeLMHash(root), accounts, storage)
}

// --- Merge tests ---

func TestLayerMergerSingleLayer(t *testing.T) {
	disk, _ := makeLMDisk()
	dl := makeLMDiff(disk, 0x02, map[types.Hash][]byte{
		makeLMHash(0xAA): {1, 2},
		makeLMHash(0xBB): {3, 4},
	}, nil)

	merger := NewLayerMerger(PolicyNewestWins)
	result, err := merger.Merge([]*diffLayer{dl})
	if err != nil {
		t.Fatalf("Merge single layer: %v", err)
	}
	if result.Stats.LayersProcessed != 1 {
		t.Fatalf("expected 1 layer processed, got %d", result.Stats.LayersProcessed)
	}
	if result.Stats.AccountsMerged != 2 {
		t.Fatalf("expected 2 accounts merged, got %d", result.Stats.AccountsMerged)
	}
}

func TestLayerMergerNewestWins(t *testing.T) {
	disk, _ := makeLMDisk()
	acct := makeLMHash(0xAA)
	dl1 := makeLMDiff(disk, 0x02, map[types.Hash][]byte{acct: {1}}, nil)
	dl2 := makeLMDiff(dl1, 0x03, map[types.Hash][]byte{acct: {2}}, nil)

	merger := NewLayerMerger(PolicyNewestWins)
	result, err := merger.Merge([]*diffLayer{dl1, dl2})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Newest layer (dl2) has data {2} which should win.
	data, ok := result.Merged.accountData[acct]
	if !ok {
		t.Fatal("expected account in merged layer")
	}
	if !bytes.Equal(data, []byte{2}) {
		t.Fatalf("expected data [2], got %v", data)
	}
	if result.Stats.AccountConflicts != 1 {
		t.Fatalf("expected 1 conflict, got %d", result.Stats.AccountConflicts)
	}
}

func TestLayerMergerOldestWins(t *testing.T) {
	disk, _ := makeLMDisk()
	acct := makeLMHash(0xAA)
	dl1 := makeLMDiff(disk, 0x02, map[types.Hash][]byte{acct: {1}}, nil)
	dl2 := makeLMDiff(dl1, 0x03, map[types.Hash][]byte{acct: {2}}, nil)

	merger := NewLayerMerger(PolicyOldestWins)
	result, err := merger.Merge([]*diffLayer{dl1, dl2})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Oldest layer (dl1) has data {1} which should win.
	data, ok := result.Merged.accountData[acct]
	if !ok {
		t.Fatal("expected account in merged layer")
	}
	if !bytes.Equal(data, []byte{1}) {
		t.Fatalf("expected data [1], got %v", data)
	}
}

func TestLayerMergerDeletionTracking(t *testing.T) {
	disk, _ := makeLMDisk()
	acct := makeLMHash(0xCC)
	dl := makeLMDiff(disk, 0x02, map[types.Hash][]byte{acct: nil}, nil)

	merger := NewLayerMerger(PolicyNewestWins)
	result, err := merger.Merge([]*diffLayer{dl})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if result.Stats.AccountDeletions != 1 {
		t.Fatalf("expected 1 deletion, got %d", result.Stats.AccountDeletions)
	}
	// Verify the deletion marker is preserved.
	data, ok := result.Merged.accountData[acct]
	if !ok {
		t.Fatal("expected account key present for deletion")
	}
	if data != nil {
		t.Fatalf("expected nil data for deletion, got %v", data)
	}
}

func TestLayerMergerEmptyLayers(t *testing.T) {
	merger := NewLayerMerger(PolicyNewestWins)
	_, err := merger.Merge(nil)
	if err != ErrNoLayersToCompact {
		t.Fatalf("expected ErrNoLayersToCompact, got %v", err)
	}
}

func TestLayerMergerStorageMerge(t *testing.T) {
	disk, _ := makeLMDisk()
	acct := makeLMHash(0xAA)
	slot1 := makeLMHash(0x10)
	slot2 := makeLMHash(0x20)

	storage1 := map[types.Hash]map[types.Hash][]byte{
		acct: {slot1: {10}},
	}
	storage2 := map[types.Hash]map[types.Hash][]byte{
		acct: {slot2: {20}},
	}

	dl1 := makeLMDiff(disk, 0x02, map[types.Hash][]byte{acct: {1}}, storage1)
	dl2 := makeLMDiff(dl1, 0x03, nil, storage2)

	merger := NewLayerMerger(PolicyNewestWins)
	result, err := merger.Merge([]*diffLayer{dl1, dl2})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if result.Stats.StorageSlotsMerged != 2 {
		t.Fatalf("expected 2 storage slots, got %d", result.Stats.StorageSlotsMerged)
	}
}

func TestLayerMergerStorageConflict(t *testing.T) {
	disk, _ := makeLMDisk()
	acct := makeLMHash(0xAA)
	slot := makeLMHash(0x10)

	storage1 := map[types.Hash]map[types.Hash][]byte{
		acct: {slot: {10}},
	}
	storage2 := map[types.Hash]map[types.Hash][]byte{
		acct: {slot: {20}},
	}

	dl1 := makeLMDiff(disk, 0x02, nil, storage1)
	dl2 := makeLMDiff(dl1, 0x03, nil, storage2)

	merger := NewLayerMerger(PolicyNewestWins)
	result, err := merger.Merge([]*diffLayer{dl1, dl2})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if result.Stats.StorageConflicts != 1 {
		t.Fatalf("expected 1 storage conflict, got %d", result.Stats.StorageConflicts)
	}
	// Newest wins: slot should be {20}.
	data := result.Merged.storageData[acct][slot]
	if !bytes.Equal(data, []byte{20}) {
		t.Fatalf("expected storage [20], got %v", data)
	}
}

func TestLayerMergerStorageDeletion(t *testing.T) {
	disk, _ := makeLMDisk()
	acct := makeLMHash(0xAA)
	slot := makeLMHash(0x10)

	storage := map[types.Hash]map[types.Hash][]byte{
		acct: {slot: nil},
	}
	dl := makeLMDiff(disk, 0x02, nil, storage)

	merger := NewLayerMerger(PolicyNewestWins)
	result, err := merger.Merge([]*diffLayer{dl})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if result.Stats.StorageDeletions != 1 {
		t.Fatalf("expected 1 storage deletion, got %d", result.Stats.StorageDeletions)
	}
}

func TestLayerMergerSortedAccountKeys(t *testing.T) {
	disk, _ := makeLMDisk()
	dl := makeLMDiff(disk, 0x02, map[types.Hash][]byte{
		makeLMHash(0xCC): {3},
		makeLMHash(0xAA): {1},
		makeLMHash(0xBB): {2},
	}, nil)

	merger := NewLayerMerger(PolicyNewestWins)
	result, err := merger.Merge([]*diffLayer{dl})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(result.SortedAccountKeys) != 3 {
		t.Fatalf("expected 3 sorted keys, got %d", len(result.SortedAccountKeys))
	}
	for i := 1; i < len(result.SortedAccountKeys); i++ {
		if !hashLess(result.SortedAccountKeys[i-1], result.SortedAccountKeys[i]) {
			t.Fatal("sorted keys not in ascending order")
		}
	}
}

func TestLayerMergerThreeLayers(t *testing.T) {
	disk, _ := makeLMDisk()
	dl1 := makeLMDiff(disk, 0x02, map[types.Hash][]byte{makeLMHash(0xAA): {1}}, nil)
	dl2 := makeLMDiff(dl1, 0x03, map[types.Hash][]byte{makeLMHash(0xBB): {2}}, nil)
	dl3 := makeLMDiff(dl2, 0x04, map[types.Hash][]byte{makeLMHash(0xCC): {3}}, nil)

	merger := NewLayerMerger(PolicyNewestWins)
	result, err := merger.Merge([]*diffLayer{dl1, dl2, dl3})
	if err != nil {
		t.Fatalf("Merge 3 layers: %v", err)
	}
	if result.Stats.LayersProcessed != 3 {
		t.Fatalf("expected 3 layers processed, got %d", result.Stats.LayersProcessed)
	}
	if result.Stats.AccountsMerged != 3 {
		t.Fatalf("expected 3 accounts, got %d", result.Stats.AccountsMerged)
	}
	// Root should be from newest layer.
	if result.Merged.root != makeLMHash(0x04) {
		t.Fatalf("expected root from newest layer, got %v", result.Merged.root)
	}
}

func TestLayerMergerMemoryStats(t *testing.T) {
	disk, _ := makeLMDisk()
	dl := makeLMDiff(disk, 0x02, map[types.Hash][]byte{
		makeLMHash(0xAA): {1, 2, 3, 4, 5},
	}, nil)

	merger := NewLayerMerger(PolicyNewestWins)
	result, err := merger.Merge([]*diffLayer{dl})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if result.Stats.MemoryBefore == 0 {
		t.Fatal("expected non-zero memory before")
	}
	if result.Stats.MemoryAfter == 0 {
		t.Fatal("expected non-zero memory after")
	}
}

func TestLayerMergerCumulativeStats(t *testing.T) {
	disk, _ := makeLMDisk()
	dl1 := makeLMDiff(disk, 0x02, map[types.Hash][]byte{makeLMHash(0xAA): {1}}, nil)
	dl2 := makeLMDiff(dl1, 0x03, map[types.Hash][]byte{makeLMHash(0xBB): {2}}, nil)

	merger := NewLayerMerger(PolicyNewestWins)
	merger.Merge([]*diffLayer{dl1})
	merger.Merge([]*diffLayer{dl2})

	stats := merger.CumulativeStats()
	if stats.LayersProcessed != 2 {
		t.Fatalf("expected cumulative 2 layers, got %d", stats.LayersProcessed)
	}
}

// --- SeekAccount tests ---

func TestSeekAccountFound(t *testing.T) {
	keys := []types.Hash{makeLMHash(0x10), makeLMHash(0x20), makeLMHash(0x30)}
	idx, h := SeekAccount(keys, makeLMHash(0x20))
	if idx != 1 {
		t.Fatalf("expected index 1, got %d", idx)
	}
	if h != makeLMHash(0x20) {
		t.Fatalf("expected hash 0x20, got %v", h)
	}
}

func TestSeekAccountBetween(t *testing.T) {
	keys := []types.Hash{makeLMHash(0x10), makeLMHash(0x30)}
	idx, h := SeekAccount(keys, makeLMHash(0x20))
	if idx != 1 {
		t.Fatalf("expected index 1 for seek between, got %d", idx)
	}
	if h != makeLMHash(0x30) {
		t.Fatalf("expected hash 0x30, got %v", h)
	}
}

func TestSeekAccountNotFound(t *testing.T) {
	keys := []types.Hash{makeLMHash(0x10), makeLMHash(0x20)}
	idx, _ := SeekAccount(keys, makeLMHash(0x30))
	if idx != -1 {
		t.Fatalf("expected -1 for not found, got %d", idx)
	}
}

func TestSeekAccountEmpty(t *testing.T) {
	idx, _ := SeekAccount(nil, makeLMHash(0x10))
	if idx != -1 {
		t.Fatalf("expected -1 for empty keys, got %d", idx)
	}
}

func TestSeekAccountFirst(t *testing.T) {
	keys := []types.Hash{makeLMHash(0x10), makeLMHash(0x20), makeLMHash(0x30)}
	idx, h := SeekAccount(keys, makeLMHash(0x05))
	if idx != 0 {
		t.Fatalf("expected index 0 for seek before first, got %d", idx)
	}
	if h != makeLMHash(0x10) {
		t.Fatalf("expected first hash, got %v", h)
	}
}

// --- CollectLayerChain tests ---

func TestCollectLayerChainMultiple(t *testing.T) {
	disk, _ := makeLMDisk()
	dl1 := makeLMDiff(disk, 0x02, nil, nil)
	dl2 := makeLMDiff(dl1, 0x03, nil, nil)
	dl3 := makeLMDiff(dl2, 0x04, nil, nil)

	chain := CollectLayerChain(dl3)
	if len(chain) != 3 {
		t.Fatalf("expected 3 layers in chain, got %d", len(chain))
	}
	// Chain should be newest-first.
	if chain[0].root != makeLMHash(0x04) {
		t.Fatal("expected newest layer first")
	}
	if chain[2].root != makeLMHash(0x02) {
		t.Fatal("expected oldest layer last")
	}
}

func TestCollectLayerChainSingle(t *testing.T) {
	disk, _ := makeLMDisk()
	dl := makeLMDiff(disk, 0x02, nil, nil)
	chain := CollectLayerChain(dl)
	if len(chain) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(chain))
	}
}

// --- ReverseLayerChain tests ---

func TestReverseLayerChain(t *testing.T) {
	disk, _ := makeLMDisk()
	dl1 := makeLMDiff(disk, 0x02, nil, nil)
	dl2 := makeLMDiff(dl1, 0x03, nil, nil)
	dl3 := makeLMDiff(dl2, 0x04, nil, nil)

	chain := []*diffLayer{dl1, dl2, dl3}
	ReverseLayerChain(chain)
	if chain[0].root != makeLMHash(0x04) {
		t.Fatal("expected reversed order")
	}
	if chain[2].root != makeLMHash(0x02) {
		t.Fatal("expected reversed order")
	}
}

func TestReverseLayerChainEmpty(t *testing.T) {
	// Should not panic.
	ReverseLayerChain(nil)
	ReverseLayerChain([]*diffLayer{})
}

// --- MergedAccountHashes tests ---

func TestMergedAccountHashes(t *testing.T) {
	disk, _ := makeLMDisk()
	dl1 := makeLMDiff(disk, 0x02, map[types.Hash][]byte{
		makeLMHash(0xAA): {1},
		makeLMHash(0xBB): {2},
	}, nil)
	dl2 := makeLMDiff(dl1, 0x03, map[types.Hash][]byte{
		makeLMHash(0xBB): {3},
		makeLMHash(0xCC): {4},
	}, nil)

	hashes := MergedAccountHashes([]*diffLayer{dl1, dl2})
	if len(hashes) != 3 {
		t.Fatalf("expected 3 unique hashes, got %d", len(hashes))
	}
	// Should be sorted.
	for i := 1; i < len(hashes); i++ {
		if !hashLess(hashes[i-1], hashes[i]) {
			t.Fatal("hashes not sorted")
		}
	}
}

func TestMergedAccountHashesEmpty(t *testing.T) {
	hashes := MergedAccountHashes(nil)
	if len(hashes) != 0 {
		t.Fatalf("expected 0 hashes, got %d", len(hashes))
	}
}

// --- MergedStorageHashes tests ---

func TestMergedStorageHashes(t *testing.T) {
	disk, _ := makeLMDisk()
	acct := makeLMHash(0xAA)
	slot1 := makeLMHash(0x10)
	slot2 := makeLMHash(0x20)

	dl1 := makeLMDiff(disk, 0x02, nil, map[types.Hash]map[types.Hash][]byte{
		acct: {slot1: {10}},
	})
	dl2 := makeLMDiff(dl1, 0x03, nil, map[types.Hash]map[types.Hash][]byte{
		acct: {slot2: {20}},
	})

	result := MergedStorageHashes([]*diffLayer{dl1, dl2})
	slots, ok := result[acct]
	if !ok {
		t.Fatal("expected account in merged storage hashes")
	}
	if len(slots) != 2 {
		t.Fatalf("expected 2 unique slots, got %d", len(slots))
	}
}

// --- Policy tests ---

func TestConflictPolicyString(t *testing.T) {
	cases := []struct {
		p    ConflictPolicy
		want string
	}{
		{PolicyNewestWins, "newest-wins"},
		{PolicyOldestWins, "oldest-wins"},
		{ConflictPolicy(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.p.String(); got != tc.want {
			t.Fatalf("ConflictPolicy(%d).String() = %q, want %q", tc.p, got, tc.want)
		}
	}
}

func TestLayerMergerSetPolicy(t *testing.T) {
	merger := NewLayerMerger(PolicyNewestWins)
	if merger.Policy() != PolicyNewestWins {
		t.Fatal("expected PolicyNewestWins")
	}
	merger.SetPolicy(PolicyOldestWins)
	if merger.Policy() != PolicyOldestWins {
		t.Fatal("expected PolicyOldestWins after SetPolicy")
	}
}

// --- MergeStats helper test ---

func TestMergeStatsMemorySaved(t *testing.T) {
	stats := MergeStats{MemoryBefore: 1000, MemoryAfter: 600}
	if stats.MemorySaved() != 400 {
		t.Fatalf("expected 400 bytes saved, got %d", stats.MemorySaved())
	}

	// No savings case.
	stats2 := MergeStats{MemoryBefore: 100, MemoryAfter: 200}
	if stats2.MemorySaved() != 0 {
		t.Fatalf("expected 0 bytes saved, got %d", stats2.MemorySaved())
	}
}

func TestLayerMergerDataIndependence(t *testing.T) {
	// Verify that the merged layer's data is independent of the original.
	disk, _ := makeLMDisk()
	acct := makeLMHash(0xAA)
	origData := []byte{1, 2, 3}
	dl := makeLMDiff(disk, 0x02, map[types.Hash][]byte{acct: origData}, nil)

	merger := NewLayerMerger(PolicyNewestWins)
	result, err := merger.Merge([]*diffLayer{dl})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Mutate original data.
	origData[0] = 99

	// Merged data should be unaffected.
	data := result.Merged.accountData[acct]
	if data[0] == 99 {
		t.Fatal("merged data should be independent of original")
	}
}
