package snapshot

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/types"
)

// mkHash creates a test hash from a byte value.
func mkHash(b byte) types.Hash {
	var h types.Hash
	h[types.HashLength-1] = b
	return h
}

func TestSnapshotAccountCache_PutAndGet(t *testing.T) {
	cache := NewSnapshotAccountCache(10)
	hash := mkHash(0x01)
	data := []byte{1, 2, 3, 4}
	cache.Put(hash, data)
	got, ok := cache.Get(hash)
	if !ok {
		t.Fatal("expected cache hit")
	}
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("data[%d] = %d, want %d", i, got[i], data[i])
		}
	}
}

func TestSnapshotAccountCache_GetMiss(t *testing.T) {
	cache := NewSnapshotAccountCache(10)
	got, ok := cache.Get(mkHash(0x01))
	if ok {
		t.Fatal("expected cache miss")
	}
	if got != nil {
		t.Fatal("expected nil data on miss")
	}
}

func TestSnapshotAccountCache_CacheNilDeletion(t *testing.T) {
	cache := NewSnapshotAccountCache(10)
	hash := mkHash(0x01)
	cache.Put(hash, nil) // deletion marker
	got, ok := cache.Get(hash)
	if !ok {
		t.Fatal("expected cache hit for nil data")
	}
	if got != nil {
		t.Fatal("expected nil data for deletion marker")
	}
}

func TestSnapshotAccountCache_HitMissStats(t *testing.T) {
	cache := NewSnapshotAccountCache(10)
	hash := mkHash(0x01)
	cache.Put(hash, []byte{1})
	cache.Get(hash)        // hit
	cache.Get(hash)        // hit
	cache.Get(hash)        // hit
	cache.Get(mkHash(0x99)) // miss
	cache.Get(mkHash(0x98)) // miss

	stats := cache.Stats()
	if stats.Hits != 3 {
		t.Errorf("Hits = %d, want 3", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("Misses = %d, want 2", stats.Misses)
	}
	if stats.HitRate() != 0.6 {
		t.Errorf("HitRate = %f, want 0.6", stats.HitRate())
	}
	// Zero-lookup case.
	if (SnapshotCacheStats{}).HitRate() != 0.0 {
		t.Error("expected 0.0 hit rate with no lookups")
	}
}

func TestSnapshotAccountCache_LRUEviction(t *testing.T) {
	cache := NewSnapshotAccountCache(3)
	h1, h2, h3, h4 := mkHash(0x01), mkHash(0x02), mkHash(0x03), mkHash(0x04)
	cache.Put(h1, []byte{1})
	cache.Put(h2, []byte{2})
	cache.Put(h3, []byte{3})
	cache.Put(h4, []byte{4}) // evicts h1

	if cache.Len() != 3 {
		t.Fatalf("Len = %d, want 3", cache.Len())
	}
	if _, ok := cache.Get(h1); ok {
		t.Fatal("expected h1 to be evicted")
	}
	if _, ok := cache.Get(h2); !ok {
		t.Fatal("expected h2 in cache")
	}
	if cache.Stats().Evictions != 1 {
		t.Errorf("Evictions = %d, want 1", cache.Stats().Evictions)
	}
}

func TestSnapshotAccountCache_AccessPromotes(t *testing.T) {
	cache := NewSnapshotAccountCache(3)
	h1, h2, h3, h4 := mkHash(0x01), mkHash(0x02), mkHash(0x03), mkHash(0x04)
	cache.Put(h1, []byte{1})
	cache.Put(h2, []byte{2})
	cache.Put(h3, []byte{3})
	cache.Get(h1)           // promote h1
	cache.Put(h4, []byte{4}) // evicts h2 (now LRU)

	if _, ok := cache.Get(h2); ok {
		t.Fatal("expected h2 evicted after h1 promotion")
	}
	if _, ok := cache.Get(h1); !ok {
		t.Fatal("expected h1 to remain")
	}
}

func TestSnapshotAccountCache_UpdateExisting(t *testing.T) {
	cache := NewSnapshotAccountCache(10)
	hash := mkHash(0x01)
	cache.Put(hash, []byte{1})
	cache.Put(hash, []byte{2, 3})
	if cache.Len() != 1 {
		t.Fatalf("Len = %d, want 1", cache.Len())
	}
	got, _ := cache.Get(hash)
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("data = %v, want [2 3]", got)
	}
}

func TestSnapshotAccountCache_DeepCopy(t *testing.T) {
	cache := NewSnapshotAccountCache(10)
	hash := mkHash(0x01)
	original := []byte{1, 2, 3}
	cache.Put(hash, original)
	original[0] = 99 // mutate after put
	got, _ := cache.Get(hash)
	if got[0] != 1 {
		t.Fatalf("cached data mutated: got[0] = %d, want 1", got[0])
	}
	got[1] = 88 // mutate returned copy
	got2, _ := cache.Get(hash)
	if got2[1] != 2 {
		t.Fatalf("cache affected by mutation: got2[1] = %d, want 2", got2[1])
	}
}

func TestSnapshotAccountCache_DeleteAndContains(t *testing.T) {
	cache := NewSnapshotAccountCache(10)
	hash := mkHash(0x01)
	if cache.Contains(hash) {
		t.Fatal("expected Contains=false for empty cache")
	}
	cache.Put(hash, []byte{1})
	if !cache.Contains(hash) {
		t.Fatal("expected Contains=true after Put")
	}
	cache.Delete(hash)
	if _, ok := cache.Get(hash); ok {
		t.Fatal("expected miss after delete")
	}
	if cache.Len() != 0 {
		t.Fatalf("Len = %d after delete, want 0", cache.Len())
	}
	// Delete non-existent should not panic.
	cache.Delete(mkHash(0xFF))
}

func TestSnapshotAccountCache_Clear(t *testing.T) {
	cache := NewSnapshotAccountCache(10)
	for i := byte(0); i < 5; i++ {
		cache.Put(mkHash(i), []byte{i})
	}
	cache.Get(mkHash(0x00)) // hit
	cache.Get(mkHash(0xFF)) // miss
	cache.Clear()
	if cache.Len() != 0 {
		t.Fatalf("Len = %d after clear, want 0", cache.Len())
	}
	stats := cache.Stats()
	if stats.Hits != 0 || stats.Misses != 0 || stats.Evictions != 0 || stats.Preloads != 0 {
		t.Fatalf("expected all stats reset, got %+v", stats)
	}
}

func TestSnapshotAccountCache_CapAndMinCapacity(t *testing.T) {
	cache := NewSnapshotAccountCache(100)
	if cache.Cap() != 100 {
		t.Fatalf("Cap = %d, want 100", cache.Cap())
	}
	if cache.Len() != 0 {
		t.Fatalf("Len = %d, want 0", cache.Len())
	}
	// Min capacity clamps to 1.
	cache2 := NewSnapshotAccountCache(0)
	if cache2.Cap() != 1 {
		t.Fatalf("Cap = %d, want 1 (min)", cache2.Cap())
	}
	cache2.Put(mkHash(0x01), []byte{1})
	cache2.Put(mkHash(0x02), []byte{2})
	if cache2.Len() != 1 {
		t.Fatalf("Len = %d with cap=1, want 1", cache2.Len())
	}
	if _, ok := cache2.Get(mkHash(0x02)); !ok {
		t.Fatal("expected most recent entry to survive")
	}
}

func TestSnapshotAccountCache_Preload(t *testing.T) {
	cache := NewSnapshotAccountCache(10)
	batch := map[types.Hash][]byte{
		mkHash(0x01): {1}, mkHash(0x02): {2}, mkHash(0x03): {3},
	}
	cache.Preload(batch)
	if cache.Len() != 3 {
		t.Fatalf("Len = %d, want 3", cache.Len())
	}
	for hash, expected := range batch {
		got, ok := cache.Get(hash)
		if !ok {
			t.Fatalf("expected hit for %v", hash)
		}
		if got[0] != expected[0] {
			t.Fatalf("data[%v] = %d, want %d", hash, got[0], expected[0])
		}
	}
	if cache.Stats().Preloads != 3 {
		t.Errorf("Preloads = %d, want 3", cache.Stats().Preloads)
	}
}

func TestSnapshotAccountCache_PreloadDoesNotOverwrite(t *testing.T) {
	cache := NewSnapshotAccountCache(10)
	hash := mkHash(0x01)
	cache.Put(hash, []byte{10, 20})
	cache.Preload(map[types.Hash][]byte{hash: {99}})
	got, _ := cache.Get(hash)
	if len(got) != 2 || got[0] != 10 || got[1] != 20 {
		t.Fatalf("preload overwrote data: got %v, want [10 20]", got)
	}
}

func TestSnapshotAccountCache_PreloadEviction(t *testing.T) {
	cache := NewSnapshotAccountCache(3)
	cache.Put(mkHash(0x01), []byte{1})
	cache.Put(mkHash(0x02), []byte{2})
	cache.Put(mkHash(0x03), []byte{3})
	cache.Preload(map[types.Hash][]byte{mkHash(0x04): {4}, mkHash(0x05): {5}})
	if cache.Len() != 3 {
		t.Fatalf("Len = %d, want 3", cache.Len())
	}
	if cache.Stats().Evictions != 2 {
		t.Errorf("Evictions = %d, want 2", cache.Stats().Evictions)
	}
}

func TestSnapshotAccountCache_Keys(t *testing.T) {
	cache := NewSnapshotAccountCache(10)
	h1, h2, h3 := mkHash(0x01), mkHash(0x02), mkHash(0x03)
	cache.Put(h1, []byte{1})
	cache.Put(h2, []byte{2})
	cache.Put(h3, []byte{3})
	keys := cache.Keys()
	if len(keys) != 3 {
		t.Fatalf("Keys() = %d keys, want 3", len(keys))
	}
	if keys[0] != h3 {
		t.Errorf("keys[0] = %v, want %v (MRU)", keys[0], h3)
	}
	if keys[2] != h1 {
		t.Errorf("keys[2] = %v, want %v (LRU)", keys[2], h1)
	}
}

func TestSnapshotAccountCache_StatsSize(t *testing.T) {
	cache := NewSnapshotAccountCache(100)
	for i := byte(0); i < 10; i++ {
		cache.Put(mkHash(i), []byte{i})
	}
	stats := cache.Stats()
	if stats.Size != 10 {
		t.Fatalf("Size = %d, want 10", stats.Size)
	}
	if stats.Capacity != 100 {
		t.Fatalf("Capacity = %d, want 100", stats.Capacity)
	}
}

func TestSnapshotAccountCache_ConcurrentAccess(t *testing.T) {
	cache := NewSnapshotAccountCache(100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(idx byte) {
			defer wg.Done()
			cache.Put(mkHash(idx), []byte{idx})
		}(byte(i))
		go func(idx byte) {
			defer wg.Done()
			cache.Get(mkHash(idx))
		}(byte(i))
	}
	wg.Wait()
	for i := byte(0); i < 50; i++ {
		got, ok := cache.Get(mkHash(i))
		if !ok {
			t.Errorf("expected entry for hash %d", i)
			continue
		}
		if len(got) != 1 || got[0] != i {
			t.Errorf("hash %d: data = %v, want [%d]", i, got, i)
		}
	}
}

func TestSnapshotAccountCache_ConcurrentPutAndDelete(t *testing.T) {
	cache := NewSnapshotAccountCache(50)
	var wg sync.WaitGroup
	for i := byte(0); i < 20; i++ {
		wg.Add(1)
		go func(idx byte) {
			defer wg.Done()
			cache.Put(mkHash(idx), []byte{idx})
		}(i)
	}
	wg.Wait()
	for i := byte(0); i < 20; i++ {
		wg.Add(1)
		go func(idx byte) {
			defer wg.Done()
			cache.Delete(mkHash(idx))
		}(i)
	}
	wg.Wait()
	if cache.Len() != 0 {
		t.Fatalf("Len = %d after all deletes, want 0", cache.Len())
	}
}

func TestSnapshotAccountCache_ConcurrentPreload(t *testing.T) {
	cache := NewSnapshotAccountCache(200)
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(gi int) {
			defer wg.Done()
			batch := make(map[types.Hash][]byte)
			for i := 0; i < 10; i++ {
				key := byte(gi*10 + i)
				batch[mkHash(key)] = []byte{key}
			}
			cache.Preload(batch)
		}(g)
	}
	wg.Wait()
	if cache.Len() != 100 {
		t.Fatalf("Len = %d, want 100", cache.Len())
	}
}

func TestSnapshotAccountCache_CapacityLimits(t *testing.T) {
	cache := NewSnapshotAccountCache(5)
	for i := byte(0); i < 20; i++ {
		cache.Put(mkHash(i), []byte{i})
	}
	if cache.Len() != 5 {
		t.Fatalf("Len = %d, want 5", cache.Len())
	}
	for i := byte(15); i < 20; i++ {
		if _, ok := cache.Get(mkHash(i)); !ok {
			t.Errorf("expected hash %d in cache", i)
		}
	}
	if cache.Stats().Evictions != 15 {
		t.Errorf("Evictions = %d, want 15", cache.Stats().Evictions)
	}
}

func TestSnapshotAccountCache_PreloadHashesIntegration(t *testing.T) {
	db := rawdb.NewMemoryDB()
	diskRoot := mkHash(0x01)
	tree := NewTree(db, diskRoot)
	h1, h2, h3 := mkHash(0xAA), mkHash(0xBB), mkHash(0xCC)
	db.Put(accountSnapshotKey(h1), []byte{1})
	db.Put(accountSnapshotKey(h2), []byte{2})
	snap := tree.Snapshot(diskRoot)

	cache := NewSnapshotAccountCache(10)
	cache.PreloadHashes(snap, []types.Hash{h1, h2, h3})
	if !cache.Contains(h1) {
		t.Error("expected h1 in cache")
	}
	if !cache.Contains(h2) {
		t.Error("expected h2 in cache")
	}
	if cache.Contains(h3) {
		t.Error("expected h3 NOT in cache (not in snapshot)")
	}
	if cache.Stats().Preloads != 2 {
		t.Errorf("Preloads = %d, want 2", cache.Stats().Preloads)
	}
}

func TestSnapshotAccountCache_PreloadHashesEdgeCases(t *testing.T) {
	// Nil snapshot should not panic.
	cache := NewSnapshotAccountCache(10)
	cache.PreloadHashes(nil, []types.Hash{mkHash(0x01)})
	if cache.Len() != 0 {
		t.Fatalf("Len = %d, want 0", cache.Len())
	}
	// Empty hash list.
	db := rawdb.NewMemoryDB()
	tree := NewTree(db, mkHash(0x01))
	snap := tree.Snapshot(mkHash(0x01))
	cache.PreloadHashes(snap, nil)
	if cache.Len() != 0 {
		t.Fatalf("Len = %d, want 0", cache.Len())
	}
}
