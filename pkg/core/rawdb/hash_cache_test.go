package rawdb

import (
	"fmt"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// testHash creates a deterministic hash from a block number.
func testHash(n uint64) types.Hash {
	return types.BytesToHash([]byte(fmt.Sprintf("block-%d", n)))
}

func TestHashCache_PutGet(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 10, EnableMetrics: true})

	h := testHash(42)
	c.Put(42, h)

	got, ok := c.Get(42)
	if !ok {
		t.Fatal("expected to find block 42 in cache")
	}
	if got != h {
		t.Fatalf("expected hash %v, got %v", h, got)
	}
}

func TestHashCache_GetMiss(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 10, EnableMetrics: true})

	_, ok := c.Get(99)
	if ok {
		t.Fatal("expected miss for block 99")
	}

	stats := c.Stats()
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestHashCache_LRUEviction(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 3, EnableMetrics: true})

	// Fill to capacity.
	c.Put(1, testHash(1))
	c.Put(2, testHash(2))
	c.Put(3, testHash(3))

	if c.Len() != 3 {
		t.Fatalf("expected 3 entries, got %d", c.Len())
	}

	// Adding a 4th entry should evict the LRU (block 1).
	c.Put(4, testHash(4))

	if c.Len() != 3 {
		t.Fatalf("expected 3 entries after eviction, got %d", c.Len())
	}

	if c.Contains(1) {
		t.Error("block 1 should have been evicted (LRU)")
	}
	if !c.Contains(2) || !c.Contains(3) || !c.Contains(4) {
		t.Error("blocks 2,3,4 should still be in cache")
	}

	stats := c.Stats()
	if stats.Evictions != 1 {
		t.Fatalf("expected 1 eviction, got %d", stats.Evictions)
	}
}

func TestHashCache_LRUAccessOrder(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 3, EnableMetrics: true})

	c.Put(1, testHash(1))
	c.Put(2, testHash(2))
	c.Put(3, testHash(3))

	// Access block 1 to make it most recently used.
	c.Get(1)

	// Adding block 4 should now evict block 2 (least recently used).
	c.Put(4, testHash(4))

	if c.Contains(2) {
		t.Error("block 2 should have been evicted")
	}
	if !c.Contains(1) {
		t.Error("block 1 should still be in cache (was recently accessed)")
	}
}

func TestHashCache_ReverseLookup(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 10, EnableMetrics: true})

	h := testHash(100)
	c.Put(100, h)

	num, ok := c.GetByHash(h)
	if !ok {
		t.Fatal("expected reverse lookup to succeed")
	}
	if num != 100 {
		t.Fatalf("expected block number 100, got %d", num)
	}

	// Miss for unknown hash.
	_, ok = c.GetByHash(testHash(999))
	if ok {
		t.Fatal("expected miss for unknown hash")
	}
}

func TestHashCache_Remove(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 10, EnableMetrics: true})

	h := testHash(50)
	c.Put(50, h)

	if !c.Contains(50) {
		t.Fatal("block 50 should be in cache")
	}

	c.Remove(50)

	if c.Contains(50) {
		t.Fatal("block 50 should have been removed")
	}
	if c.Len() != 0 {
		t.Fatalf("expected 0 entries, got %d", c.Len())
	}

	// Reverse lookup should also fail.
	_, ok := c.GetByHash(h)
	if ok {
		t.Fatal("reverse lookup should fail after removal")
	}

	// Removing non-existent should not panic.
	c.Remove(999)
}

func TestHashCache_Purge(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 10, EnableMetrics: true})

	for i := uint64(0); i < 5; i++ {
		c.Put(i, testHash(i))
	}
	if c.Len() != 5 {
		t.Fatalf("expected 5 entries, got %d", c.Len())
	}

	c.Purge()

	if c.Len() != 0 {
		t.Fatalf("expected 0 entries after purge, got %d", c.Len())
	}
	for i := uint64(0); i < 5; i++ {
		if c.Contains(i) {
			t.Errorf("block %d should not be in cache after purge", i)
		}
	}
}

func TestHashCache_Entries(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 10, EnableMetrics: true})

	c.Put(10, testHash(10))
	c.Put(20, testHash(20))
	c.Put(30, testHash(30))

	entries := c.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Entries should be in MRU order: 30, 20, 10.
	if entries[0].Number != 30 {
		t.Errorf("first entry should be block 30 (MRU), got %d", entries[0].Number)
	}
	if entries[1].Number != 20 {
		t.Errorf("second entry should be block 20, got %d", entries[1].Number)
	}
	if entries[2].Number != 10 {
		t.Errorf("third entry should be block 10 (LRU), got %d", entries[2].Number)
	}
}

func TestHashCache_StatsTracking(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 2, EnableMetrics: true})

	c.Put(1, testHash(1))
	c.Put(2, testHash(2))

	// Two hits.
	c.Get(1)
	c.Get(2)
	// One miss.
	c.Get(99)
	// One eviction (adding 3rd to capacity-2 cache).
	c.Put(3, testHash(3))

	stats := c.Stats()
	if stats.Hits != 2 {
		t.Errorf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.Evictions != 1 {
		t.Errorf("expected 1 eviction, got %d", stats.Evictions)
	}
	if stats.Size != 2 {
		t.Errorf("expected size 2, got %d", stats.Size)
	}
}

func TestHashCache_UpdateExisting(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 10, EnableMetrics: true})

	h1 := testHash(1)
	h2 := testHash(999)
	c.Put(1, h1)
	// Update block 1 with a new hash.
	c.Put(1, h2)

	got, ok := c.Get(1)
	if !ok {
		t.Fatal("expected to find block 1")
	}
	if got != h2 {
		t.Fatalf("expected updated hash, got %v", got)
	}

	// Old hash should not resolve in reverse lookup.
	_, ok = c.GetByHash(h1)
	if ok {
		t.Fatal("old hash should not be in reverse lookup after update")
	}

	// New hash should resolve.
	num, ok := c.GetByHash(h2)
	if !ok || num != 1 {
		t.Fatalf("reverse lookup of updated hash should return 1, got %d, ok=%v", num, ok)
	}

	// Size should still be 1 (no duplication).
	if c.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", c.Len())
	}
}

func TestHashCache_ConcurrentAccess(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 100, EnableMetrics: true})

	var wg sync.WaitGroup
	// 50 writers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n uint64) {
			defer wg.Done()
			c.Put(n, testHash(n))
		}(uint64(i))
	}
	// 50 readers, each reading a random block number.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n uint64) {
			defer wg.Done()
			c.Get(n)
			c.GetByHash(testHash(n))
			c.Contains(n)
			c.Len()
		}(uint64(i))
	}
	wg.Wait()

	// Validate cache is consistent.
	if c.Len() > 100 {
		t.Fatalf("cache size %d exceeds max %d", c.Len(), 100)
	}
}

func TestHashCache_DefaultConfig(t *testing.T) {
	cfg := DefaultHashCacheConfig()
	if cfg.MaxEntries != 1024 {
		t.Errorf("expected MaxEntries=1024, got %d", cfg.MaxEntries)
	}
	if !cfg.EnableMetrics {
		t.Error("expected EnableMetrics=true")
	}
}

func TestHashCache_ZeroMaxEntries(t *testing.T) {
	// Zero max entries should default to 1024.
	c := NewHashCache(HashCacheConfig{MaxEntries: 0})
	if c.maxEntries != 1024 {
		t.Fatalf("expected default maxEntries=1024, got %d", c.maxEntries)
	}
}

func TestHashCache_SingleEntry(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 1, EnableMetrics: true})

	c.Put(1, testHash(1))
	if c.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", c.Len())
	}

	// Adding another should evict the first.
	c.Put(2, testHash(2))
	if c.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", c.Len())
	}
	if c.Contains(1) {
		t.Error("block 1 should have been evicted")
	}
	if !c.Contains(2) {
		t.Error("block 2 should be in cache")
	}
}

func TestHashCache_RemoveHead(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 5, EnableMetrics: true})

	c.Put(1, testHash(1))
	c.Put(2, testHash(2))
	c.Put(3, testHash(3))

	// Remove the head (block 3, most recently added).
	c.Remove(3)

	entries := c.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Number != 2 {
		t.Errorf("expected new head to be block 2, got %d", entries[0].Number)
	}
}

func TestHashCache_RemoveTail(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 5, EnableMetrics: true})

	c.Put(1, testHash(1))
	c.Put(2, testHash(2))
	c.Put(3, testHash(3))

	// Remove the tail (block 1, least recently added).
	c.Remove(1)

	entries := c.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[len(entries)-1].Number != 2 {
		t.Errorf("expected new tail to be block 2, got %d", entries[len(entries)-1].Number)
	}
}

func TestHashCache_GetByHashUpdatesLRU(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 3, EnableMetrics: true})

	c.Put(1, testHash(1))
	c.Put(2, testHash(2))
	c.Put(3, testHash(3))

	// Access block 1 via reverse lookup to make it MRU.
	c.GetByHash(testHash(1))

	// Adding block 4 should evict block 2 (now LRU), not block 1.
	c.Put(4, testHash(4))

	if c.Contains(2) {
		t.Error("block 2 should have been evicted")
	}
	if !c.Contains(1) {
		t.Error("block 1 should still be in cache (accessed via GetByHash)")
	}
}

func TestHashCache_MetricsDisabled(t *testing.T) {
	c := NewHashCache(HashCacheConfig{MaxEntries: 10, EnableMetrics: false})

	c.Put(1, testHash(1))
	c.Get(1)
	c.Get(99) // miss

	stats := c.Stats()
	// With metrics disabled, hits/misses should stay at 0.
	if stats.Hits != 0 {
		t.Errorf("expected 0 hits with metrics disabled, got %d", stats.Hits)
	}
	if stats.Misses != 0 {
		t.Errorf("expected 0 misses with metrics disabled, got %d", stats.Misses)
	}
	// Size should still be reported.
	if stats.Size != 1 {
		t.Errorf("expected size 1, got %d", stats.Size)
	}
}
