package ssz

import (
	"sync"
	"testing"
	"time"
)

// makeHash creates a [32]byte with the given byte at position 0.
func makeHash(b byte) [32]byte {
	var h [32]byte
	h[0] = b
	return h
}

// TestMerkleCacheNewCache verifies constructor behavior.
func TestMerkleCacheNewCache(t *testing.T) {
	mc := NewMerkleCache(100)
	if mc == nil {
		t.Fatal("NewMerkleCache returned nil")
	}
	if mc.Len() != 0 {
		t.Fatalf("new cache should be empty, got %d", mc.Len())
	}
	if mc.maxEntries != 100 {
		t.Fatalf("maxEntries = %d, want 100", mc.maxEntries)
	}
}

// TestMerkleCacheNewCacheNegative verifies negative maxEntries is clamped to 0.
func TestMerkleCacheNewCacheNegative(t *testing.T) {
	mc := NewMerkleCache(-5)
	if mc.maxEntries != 0 {
		t.Fatalf("negative maxEntries should be clamped to 0, got %d", mc.maxEntries)
	}
}

// TestMerkleCacheGetPutHash verifies basic get/put for hash entries.
func TestMerkleCacheGetPutHash(t *testing.T) {
	mc := NewMerkleCache(10)
	key := makeHash(0x01)
	val := makeHash(0xaa)

	mc.PutHash(key, val)

	got, ok := mc.GetHash(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got != val {
		t.Fatalf("got %x, want %x", got, val)
	}
}

// TestMerkleCacheGetMiss verifies that a lookup for a missing key returns false.
func TestMerkleCacheGetMiss(t *testing.T) {
	mc := NewMerkleCache(10)
	key := makeHash(0x99)

	_, ok := mc.GetHash(key)
	if ok {
		t.Fatal("expected cache miss for absent key")
	}
}

// TestMerkleCacheSubtreeRoot verifies subtree root get/put.
func TestMerkleCacheSubtreeRoot(t *testing.T) {
	mc := NewMerkleCache(10)
	objHash := makeHash(0x10)
	root := makeHash(0xbb)

	mc.PutSubtreeRoot(objHash, 3, root)

	got, ok := mc.GetSubtreeRoot(objHash, 3)
	if !ok {
		t.Fatal("expected subtree root cache hit")
	}
	if got != root {
		t.Fatalf("got %x, want %x", got, root)
	}

	// Different depth should miss.
	_, ok = mc.GetSubtreeRoot(objHash, 4)
	if ok {
		t.Fatal("expected miss for different depth")
	}
}

// TestMerkleCacheInvalidateObject verifies that invalidation removes all
// subtree entries for an object but leaves other objects intact.
func TestMerkleCacheInvalidateObject(t *testing.T) {
	mc := NewMerkleCache(100)
	objA := makeHash(0x01)
	objB := makeHash(0x02)

	mc.PutSubtreeRoot(objA, 1, makeHash(0xa1))
	mc.PutSubtreeRoot(objA, 2, makeHash(0xa2))
	mc.PutSubtreeRoot(objB, 1, makeHash(0xb1))

	if mc.Len() != 3 {
		t.Fatalf("expected 3 entries, got %d", mc.Len())
	}

	mc.InvalidateObject(objA)

	if mc.Len() != 1 {
		t.Fatalf("expected 1 entry after invalidation, got %d", mc.Len())
	}

	// objB should still be present.
	_, ok := mc.GetSubtreeRoot(objB, 1)
	if !ok {
		t.Fatal("objB subtree should survive invalidation of objA")
	}

	// objA should be gone.
	_, ok = mc.GetSubtreeRoot(objA, 1)
	if ok {
		t.Fatal("objA subtree should be invalidated")
	}
}

// TestMerkleCacheLen verifies the Len method counts both hash and subtree entries.
func TestMerkleCacheLen(t *testing.T) {
	mc := NewMerkleCache(100)
	mc.PutHash(makeHash(0x01), makeHash(0x11))
	mc.PutHash(makeHash(0x02), makeHash(0x22))
	mc.PutSubtreeRoot(makeHash(0x03), 1, makeHash(0x33))

	if mc.Len() != 3 {
		t.Fatalf("expected 3, got %d", mc.Len())
	}
}

// TestMerkleCacheClear verifies that Clear empties the cache and resets stats.
func TestMerkleCacheClear(t *testing.T) {
	mc := NewMerkleCache(100)
	mc.PutHash(makeHash(0x01), makeHash(0x11))
	mc.PutSubtreeRoot(makeHash(0x02), 1, makeHash(0x22))

	// Generate some stats.
	mc.GetHash(makeHash(0x01))
	mc.GetHash(makeHash(0x99)) // miss

	mc.Clear()

	if mc.Len() != 0 {
		t.Fatalf("expected 0 after Clear, got %d", mc.Len())
	}
	stats := mc.Stats()
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Fatal("stats should be reset after Clear")
	}
}

// TestMerkleCacheHitRate verifies hit rate calculation.
func TestMerkleCacheHitRate(t *testing.T) {
	mc := NewMerkleCache(10)

	// No lookups yet.
	if mc.HitRate() != 0.0 {
		t.Fatalf("expected 0.0 hit rate with no lookups, got %f", mc.HitRate())
	}

	key := makeHash(0x01)
	mc.PutHash(key, makeHash(0xaa))

	mc.GetHash(key)            // hit
	mc.GetHash(key)            // hit
	mc.GetHash(makeHash(0x99)) // miss

	rate := mc.HitRate()
	// 2 hits out of 3 lookups = ~0.6667
	if rate < 0.66 || rate > 0.67 {
		t.Fatalf("expected hit rate ~0.667, got %f", rate)
	}
}

// TestMerkleCacheStats verifies the Stats snapshot.
func TestMerkleCacheStats(t *testing.T) {
	mc := NewMerkleCache(100)
	mc.PutHash(makeHash(0x01), makeHash(0x11))
	mc.PutHash(makeHash(0x02), makeHash(0x22))

	mc.GetHash(makeHash(0x01)) // hit
	mc.GetHash(makeHash(0x99)) // miss

	stats := mc.Stats()
	if stats.Hits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.Entries != 2 {
		t.Fatalf("expected 2 entries, got %d", stats.Entries)
	}
}

// TestMerkleCachePruneByAge verifies age-based pruning.
func TestMerkleCachePruneByAge(t *testing.T) {
	mc := NewMerkleCache(100)

	// Insert entries with old timestamps.
	mc.mu.Lock()
	oldTime := time.Now().Unix() - 120 // 2 minutes ago
	mc.hashes[makeHash(0x01)] = &cacheEntry{value: makeHash(0x11), timestamp: oldTime}
	mc.hashOrder = append(mc.hashOrder, makeHash(0x01))
	mc.hashes[makeHash(0x02)] = &cacheEntry{value: makeHash(0x22), timestamp: time.Now().Unix()}
	mc.hashOrder = append(mc.hashOrder, makeHash(0x02))
	mc.subtrees[subtreeKey{objectHash: makeHash(0x03), depth: 1}] = &cacheEntry{
		value: makeHash(0x33), timestamp: oldTime,
	}
	mc.subtreeOrder = append(mc.subtreeOrder, subtreeKey{objectHash: makeHash(0x03), depth: 1})
	mc.mu.Unlock()

	// Prune entries older than 60 seconds.
	pruned := mc.PruneByAge(60)
	if pruned != 2 {
		t.Fatalf("expected 2 pruned, got %d", pruned)
	}
	if mc.Len() != 1 {
		t.Fatalf("expected 1 remaining, got %d", mc.Len())
	}

	// The recent entry should survive.
	_, ok := mc.GetHash(makeHash(0x02))
	if !ok {
		t.Fatal("recent entry should survive pruning")
	}
}

// TestMerkleCacheConcurrentAccess verifies thread safety under concurrent
// reads and writes.
func TestMerkleCacheConcurrentAccess(t *testing.T) {
	mc := NewMerkleCache(1000)
	var wg sync.WaitGroup

	// Writers: insert 100 hash entries.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := makeHash(byte(i))
			val := makeHash(byte(i + 100))
			mc.PutHash(key, val)
		}(i)
	}

	// Readers: read concurrently.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := makeHash(byte(i))
			mc.GetHash(key)
		}(i)
	}

	// Subtree writers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mc.PutSubtreeRoot(makeHash(byte(i)), i%5, makeHash(byte(i+50)))
		}(i)
	}

	wg.Wait()

	// Verify no panic and cache is consistent.
	if mc.Len() < 0 {
		t.Fatal("Len should be non-negative")
	}
}

// TestMerkleCacheEviction verifies that entries are evicted when the cache
// is full.
func TestMerkleCacheEviction(t *testing.T) {
	mc := NewMerkleCache(3)

	mc.PutHash(makeHash(0x01), makeHash(0xa1))
	mc.PutHash(makeHash(0x02), makeHash(0xa2))
	mc.PutHash(makeHash(0x03), makeHash(0xa3))

	if mc.Len() != 3 {
		t.Fatalf("expected 3 entries, got %d", mc.Len())
	}

	// Insert a 4th entry, should evict the oldest (0x01).
	mc.PutHash(makeHash(0x04), makeHash(0xa4))
	if mc.Len() != 3 {
		t.Fatalf("expected 3 entries after eviction, got %d", mc.Len())
	}

	// 0x01 should be evicted.
	_, ok := mc.GetHash(makeHash(0x01))
	if ok {
		t.Fatal("oldest entry should have been evicted")
	}

	// 0x04 should be present.
	_, ok = mc.GetHash(makeHash(0x04))
	if !ok {
		t.Fatal("newest entry should be present")
	}

	stats := mc.Stats()
	if stats.Evictions < 1 {
		t.Fatalf("expected at least 1 eviction, got %d", stats.Evictions)
	}
}

// TestMerkleCacheLargeCache verifies the cache works with many entries.
func TestMerkleCacheLargeCache(t *testing.T) {
	mc := NewMerkleCache(10000)

	for i := 0; i < 5000; i++ {
		var key, val [32]byte
		key[0] = byte(i & 0xff)
		key[1] = byte((i >> 8) & 0xff)
		val[0] = byte((i + 1) & 0xff)
		mc.PutHash(key, val)
	}

	// The cache should contain entries (some may collide on key since
	// we only use 2 bytes, but that is fine for this test).
	if mc.Len() == 0 {
		t.Fatal("cache should not be empty")
	}
}

// TestMerkleCacheDuplicatePut verifies that putting the same key twice
// updates the value in place without increasing the entry count.
func TestMerkleCacheDuplicatePut(t *testing.T) {
	mc := NewMerkleCache(10)
	key := makeHash(0x01)

	mc.PutHash(key, makeHash(0xaa))
	mc.PutHash(key, makeHash(0xbb))

	if mc.Len() != 1 {
		t.Fatalf("expected 1 entry after duplicate put, got %d", mc.Len())
	}

	got, ok := mc.GetHash(key)
	if !ok {
		t.Fatal("expected hit after update")
	}
	if got[0] != 0xbb {
		t.Fatalf("expected updated value 0xbb, got 0x%02x", got[0])
	}
}

// TestMerkleCacheZeroEntries verifies a cache with maxEntries=0 stores nothing.
func TestMerkleCacheZeroEntries(t *testing.T) {
	mc := NewMerkleCache(0)

	mc.PutHash(makeHash(0x01), makeHash(0xaa))
	mc.PutSubtreeRoot(makeHash(0x02), 1, makeHash(0xbb))

	if mc.Len() != 0 {
		t.Fatalf("zero-capacity cache should have 0 entries, got %d", mc.Len())
	}

	_, ok := mc.GetHash(makeHash(0x01))
	if ok {
		t.Fatal("zero-capacity cache should always miss")
	}
}

// TestMerkleCacheSubtreeEviction verifies subtree entry eviction.
func TestMerkleCacheSubtreeEviction(t *testing.T) {
	mc := NewMerkleCache(2)

	mc.PutSubtreeRoot(makeHash(0x01), 1, makeHash(0xa1))
	mc.PutSubtreeRoot(makeHash(0x02), 1, makeHash(0xa2))

	// Insert a 3rd subtree, should evict the oldest.
	mc.PutSubtreeRoot(makeHash(0x03), 1, makeHash(0xa3))

	if mc.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", mc.Len())
	}

	_, ok := mc.GetSubtreeRoot(makeHash(0x01), 1)
	if ok {
		t.Fatal("oldest subtree entry should be evicted")
	}
}

// TestMerkleCacheDuplicateSubtreePut verifies updating a subtree entry.
func TestMerkleCacheDuplicateSubtreePut(t *testing.T) {
	mc := NewMerkleCache(10)
	obj := makeHash(0x01)

	mc.PutSubtreeRoot(obj, 3, makeHash(0xaa))
	mc.PutSubtreeRoot(obj, 3, makeHash(0xbb))

	if mc.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", mc.Len())
	}

	got, ok := mc.GetSubtreeRoot(obj, 3)
	if !ok {
		t.Fatal("expected hit")
	}
	if got[0] != 0xbb {
		t.Fatalf("expected updated value 0xbb, got 0x%02x", got[0])
	}
}

// TestMerkleCacheSubtreeKeyBytes verifies the helper that serializes keys.
func TestMerkleCacheSubtreeKeyBytes(t *testing.T) {
	obj := makeHash(0xab)
	buf := subtreeKeyBytes(obj, 7)
	if len(buf) != 40 {
		t.Fatalf("expected 40 bytes, got %d", len(buf))
	}
	if buf[0] != 0xab {
		t.Fatalf("expected first byte 0xab, got 0x%02x", buf[0])
	}
}

// TestMerkleCachePruneByAgeEmpty verifies pruning on an empty cache.
func TestMerkleCachePruneByAgeEmpty(t *testing.T) {
	mc := NewMerkleCache(100)
	pruned := mc.PruneByAge(60)
	if pruned != 0 {
		t.Fatalf("expected 0 pruned on empty cache, got %d", pruned)
	}
}

// TestMerkleCacheHitRateAfterClear verifies hit rate is 0 after clear.
func TestMerkleCacheHitRateAfterClear(t *testing.T) {
	mc := NewMerkleCache(10)
	key := makeHash(0x01)
	mc.PutHash(key, makeHash(0xaa))
	mc.GetHash(key) // hit

	mc.Clear()

	if mc.HitRate() != 0.0 {
		t.Fatalf("expected 0.0 hit rate after clear, got %f", mc.HitRate())
	}
}
