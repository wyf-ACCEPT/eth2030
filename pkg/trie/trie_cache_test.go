package trie

import (
	"testing"

	"github.com/eth2028/eth2028/crypto"
)

// makeHash produces a deterministic hash for testing from an integer.
func makeHash(n int) [32]byte {
	data := []byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	return crypto.Keccak256Hash(data)
}

func TestTrieCacheBasic(t *testing.T) {
	cache := NewTrieCache(1024)
	h := makeHash(1)
	data := []byte{0xab, 0xcd, 0xef}

	// Initially not found.
	if _, ok := cache.Get(h); ok {
		t.Fatal("expected miss on empty cache")
	}

	// Put and retrieve.
	cache.Put(h, data)
	got, ok := cache.Get(h)
	if !ok {
		t.Fatal("expected hit after Put")
	}
	if len(got) != 3 || got[0] != 0xab || got[1] != 0xcd || got[2] != 0xef {
		t.Errorf("unexpected data: %x", got)
	}

	// Len and Size.
	if cache.Len() != 1 {
		t.Errorf("Len = %d, want 1", cache.Len())
	}
	if cache.Size() != 3 {
		t.Errorf("Size = %d, want 3", cache.Size())
	}
}

func TestTrieCacheUpdate(t *testing.T) {
	cache := NewTrieCache(1024)
	h := makeHash(42)

	cache.Put(h, []byte{0x01, 0x02})
	if cache.Size() != 2 {
		t.Errorf("Size = %d, want 2", cache.Size())
	}

	// Update with larger data.
	cache.Put(h, []byte{0x03, 0x04, 0x05, 0x06})
	if cache.Len() != 1 {
		t.Errorf("Len = %d, want 1 after update", cache.Len())
	}
	if cache.Size() != 4 {
		t.Errorf("Size = %d, want 4 after update", cache.Size())
	}

	got, ok := cache.Get(h)
	if !ok || len(got) != 4 {
		t.Fatalf("expected updated data, got ok=%v len=%d", ok, len(got))
	}
}

func TestTrieCacheDelete(t *testing.T) {
	cache := NewTrieCache(1024)
	h := makeHash(99)

	cache.Put(h, []byte{0xff})
	cache.Delete(h)

	if _, ok := cache.Get(h); ok {
		t.Error("expected miss after Delete")
	}
	if cache.Len() != 0 {
		t.Errorf("Len = %d, want 0 after Delete", cache.Len())
	}
	if cache.Size() != 0 {
		t.Errorf("Size = %d, want 0 after Delete", cache.Size())
	}
}

func TestTrieCacheLRUEviction(t *testing.T) {
	// Cache with maxSize=10 bytes.
	cache := NewTrieCache(10)

	// Insert entries of 3 bytes each: 4 entries = 12 bytes > 10.
	for i := 0; i < 4; i++ {
		h := makeHash(i)
		cache.Put(h, []byte{byte(i), byte(i), byte(i)})
	}

	// The first entry should have been evicted.
	h0 := makeHash(0)
	if _, ok := cache.Get(h0); ok {
		t.Error("expected entry 0 to be evicted")
	}

	// The last 3 entries should still be present.
	for i := 1; i < 4; i++ {
		h := makeHash(i)
		if _, ok := cache.Get(h); !ok {
			t.Errorf("expected entry %d to still be cached", i)
		}
	}
}

func TestTrieCachePrune(t *testing.T) {
	cache := NewTrieCache(100)
	for i := 0; i < 10; i++ {
		h := makeHash(i)
		cache.Put(h, make([]byte, 10)) // 10 entries * 10 bytes = 100 bytes
	}

	if cache.Size() != 100 {
		t.Fatalf("Size = %d, want 100", cache.Size())
	}

	// Prune to 50 bytes: should evict 5 entries.
	evicted := cache.Prune(50)
	if evicted != 5 {
		t.Errorf("Prune evicted %d, want 5", evicted)
	}
	if cache.Len() != 5 {
		t.Errorf("Len after prune = %d, want 5", cache.Len())
	}
	if cache.Size() != 50 {
		t.Errorf("Size after prune = %d, want 50", cache.Size())
	}
}

func TestTrieCacheStats(t *testing.T) {
	cache := NewTrieCache(1024)
	h := makeHash(1)
	cache.Put(h, []byte{0x01})

	// 1 hit.
	cache.Get(h)
	// 1 miss.
	cache.Get(makeHash(999))

	stats := cache.Stats()
	if stats.Hits != 1 {
		t.Errorf("Hits = %d, want 1", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Misses = %d, want 1", stats.Misses)
	}
	if stats.EntryCount != 1 {
		t.Errorf("EntryCount = %d, want 1", stats.EntryCount)
	}
	if stats.CurrentSize != 1 {
		t.Errorf("CurrentSize = %d, want 1", stats.CurrentSize)
	}
}

func TestTrieCacheHitRate(t *testing.T) {
	cache := NewTrieCache(1024)

	// No lookups: hit rate = 0.
	if rate := cache.HitRate(); rate != 0 {
		t.Errorf("HitRate with no lookups = %f, want 0", rate)
	}

	h := makeHash(1)
	cache.Put(h, []byte{0x01})

	// 3 hits, 1 miss = 75%.
	cache.Get(h)
	cache.Get(h)
	cache.Get(h)
	cache.Get(makeHash(999))

	rate := cache.HitRate()
	if rate < 0.74 || rate > 0.76 {
		t.Errorf("HitRate = %f, want ~0.75", rate)
	}
}

func TestTrieCacheReset(t *testing.T) {
	cache := NewTrieCache(1024)
	h := makeHash(1)
	cache.Put(h, []byte{0x01, 0x02})
	cache.Get(h)
	cache.Get(makeHash(2))

	cache.Reset()

	if cache.Len() != 0 {
		t.Errorf("Len after Reset = %d, want 0", cache.Len())
	}
	if cache.Size() != 0 {
		t.Errorf("Size after Reset = %d, want 0", cache.Size())
	}
	stats := cache.Stats()
	if stats.Hits != 0 || stats.Misses != 0 || stats.Evictions != 0 {
		t.Errorf("stats not reset: %+v", stats)
	}
}

func TestTrieCacheEvictionStats(t *testing.T) {
	// 5 bytes max: each entry is 5 bytes, so second insert evicts first.
	cache := NewTrieCache(5)

	cache.Put(makeHash(1), make([]byte, 5))
	cache.Put(makeHash(2), make([]byte, 5))

	stats := cache.Stats()
	if stats.Evictions != 1 {
		t.Errorf("Evictions = %d, want 1", stats.Evictions)
	}
}

func TestTrieCacheZeroMaxSize(t *testing.T) {
	// maxSize=0 disables automatic eviction (eviction loop checks maxSize > 0).
	// Entries are stored without limit. Use Prune() for manual eviction.
	cache := NewTrieCache(0)
	h1 := makeHash(1)
	h2 := makeHash(2)

	cache.Put(h1, []byte{0x01})
	cache.Put(h2, []byte{0x02})

	// Both entries should be present since maxSize=0 means no auto-eviction.
	if cache.Len() != 2 {
		t.Errorf("Len = %d, expected 2 for maxSize=0", cache.Len())
	}
	if _, ok := cache.Get(h1); !ok {
		t.Error("expected h1 to be present")
	}
	if _, ok := cache.Get(h2); !ok {
		t.Error("expected h2 to be present")
	}

	// Manual prune should still work.
	evicted := cache.Prune(1)
	if evicted != 1 {
		t.Errorf("Prune evicted %d, want 1", evicted)
	}
	if cache.Len() != 1 {
		t.Errorf("Len after Prune = %d, want 1", cache.Len())
	}
}

func TestTrieCacheDeleteNonexistent(t *testing.T) {
	cache := NewTrieCache(1024)

	// Deleting a non-existent entry should be a no-op.
	cache.Delete(makeHash(42))
	if cache.Len() != 0 {
		t.Error("expected empty cache after deleting non-existent key")
	}
}
