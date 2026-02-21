package crypto

import (
	"sync"
	"testing"
)

// makeSigTestHash returns a deterministic [32]byte hash from a seed byte.
func makeSigTestHash(b byte) [32]byte {
	h := Keccak256Hash([]byte{b})
	var out [32]byte
	copy(out[:], h[:])
	return out
}

// makeSigTestSig returns a deterministic [65]byte signature from a seed byte.
func makeSigTestSig(b byte) [65]byte {
	var sig [65]byte
	sig[0] = b
	sig[64] = 27 // recovery id
	return sig
}

// makeSigTestAddr returns a deterministic [20]byte address from a seed byte.
func makeSigTestAddr(b byte) [20]byte {
	var addr [20]byte
	addr[19] = b
	return addr
}

func TestSigLRU_NewCache(t *testing.T) {
	c := NewSigLRUCache(128)
	if c.Len() != 0 {
		t.Fatalf("expected empty cache, got %d", c.Len())
	}
	if c.capacity != 128 {
		t.Fatalf("expected capacity 128, got %d", c.capacity)
	}
}

func TestSigLRU_NewCacheDefaultCapacity(t *testing.T) {
	c := NewSigLRUCache(0)
	if c.capacity != 4096 {
		t.Fatalf("expected default capacity 4096, got %d", c.capacity)
	}
	c2 := NewSigLRUCache(-10)
	if c2.capacity != 4096 {
		t.Fatalf("expected default capacity for negative, got %d", c2.capacity)
	}
}

func TestSigLRU_AddLookup(t *testing.T) {
	c := NewSigLRUCache(16)
	hash := makeSigTestHash(0x01)
	sig := makeSigTestSig(0xAA)
	addr := makeSigTestAddr(0xBB)

	c.Add(hash, sig, addr)

	got, found := c.Lookup(hash, sig)
	if !found {
		t.Fatal("expected cache hit")
	}
	if got != addr {
		t.Fatalf("address mismatch: got %x, want %x", got, addr)
	}
}

func TestSigLRU_LookupMiss(t *testing.T) {
	c := NewSigLRUCache(16)
	hash := makeSigTestHash(0x99)
	sig := makeSigTestSig(0x99)

	_, found := c.Lookup(hash, sig)
	if found {
		t.Fatal("expected cache miss for unknown key")
	}
}

func TestSigLRU_CacheEviction(t *testing.T) {
	c := NewSigLRUCache(3)

	// Add 5 entries to a capacity-3 cache.
	for i := byte(0); i < 5; i++ {
		c.Add(makeSigTestHash(i), makeSigTestSig(i), makeSigTestAddr(i))
	}

	if c.Len() != 3 {
		t.Fatalf("expected len 3 after eviction, got %d", c.Len())
	}

	// First two entries should have been evicted.
	_, found0 := c.Lookup(makeSigTestHash(0), makeSigTestSig(0))
	if found0 {
		t.Fatal("expected entry 0 to be evicted")
	}
	_, found1 := c.Lookup(makeSigTestHash(1), makeSigTestSig(1))
	if found1 {
		t.Fatal("expected entry 1 to be evicted")
	}

	// Most recent entries should still be present.
	_, found4 := c.Lookup(makeSigTestHash(4), makeSigTestSig(4))
	if !found4 {
		t.Fatal("expected entry 4 to be present")
	}
}

func TestSigLRU_CacheHitRate(t *testing.T) {
	c := NewSigLRUCache(16)

	// No lookups yet.
	if c.HitRate() != 0 {
		t.Fatal("expected 0 hit rate with no lookups")
	}

	hash := makeSigTestHash(0x01)
	sig := makeSigTestSig(0x01)
	c.Add(hash, sig, makeSigTestAddr(0x01))

	c.Lookup(hash, sig)                                     // hit
	c.Lookup(makeSigTestHash(0x99), makeSigTestSig(0x99))    // miss

	rate := c.HitRate()
	if rate < 0.49 || rate > 0.51 {
		t.Fatalf("expected ~0.5 hit rate, got %f", rate)
	}
}

func TestSigLRU_Remove(t *testing.T) {
	c := NewSigLRUCache(16)
	hash := makeSigTestHash(0x01)
	sig := makeSigTestSig(0x01)
	c.Add(hash, sig, makeSigTestAddr(0x01))

	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}

	c.Remove(hash)

	if c.Len() != 0 {
		t.Fatalf("expected len 0 after remove, got %d", c.Len())
	}

	_, found := c.Lookup(hash, sig)
	if found {
		t.Fatal("expected miss after remove")
	}
}

func TestSigLRU_Clear(t *testing.T) {
	c := NewSigLRUCache(16)
	for i := byte(0); i < 10; i++ {
		c.Add(makeSigTestHash(i), makeSigTestSig(i), makeSigTestAddr(i))
	}
	c.Lookup(makeSigTestHash(0), makeSigTestSig(0)) // hit
	c.Lookup(makeSigTestHash(99), makeSigTestSig(99)) // miss

	c.Clear()

	if c.Len() != 0 {
		t.Fatalf("expected empty cache after clear, got %d", c.Len())
	}
	stats := c.Stats()
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Fatal("expected counters reset after clear")
	}
}

func TestSigLRU_Len(t *testing.T) {
	c := NewSigLRUCache(100)
	if c.Len() != 0 {
		t.Fatalf("expected empty cache, got len %d", c.Len())
	}
	for i := byte(0); i < 7; i++ {
		c.Add(makeSigTestHash(i), makeSigTestSig(i), makeSigTestAddr(i))
	}
	if c.Len() != 7 {
		t.Fatalf("expected len 7, got %d", c.Len())
	}
}

func TestSigLRU_ConcurrentAccess(t *testing.T) {
	c := NewSigLRUCache(512)
	const goroutines = 8
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				hash := makeSigTestHash(byte(id*opsPerGoroutine + i))
				sig := makeSigTestSig(byte(id))
				addr := makeSigTestAddr(byte(id))

				c.Add(hash, sig, addr)
				c.Lookup(hash, sig)
				c.Len()
			}
		}(g)
	}
	wg.Wait()

	// After all goroutines complete, verify the cache is in a consistent state.
	// The cache should not vastly exceed capacity (small overshoot is acceptable
	// due to concurrent add timing).
	if c.Len() > 512 {
		t.Fatalf("cache greatly exceeded capacity: %d", c.Len())
	}
	stats := c.Stats()
	if stats.Hits+stats.Misses == 0 {
		t.Fatal("expected some lookups")
	}
}

func TestSigLRU_ZeroCapacity(t *testing.T) {
	c := NewSigLRUCache(0)
	// Should use default capacity.
	if c.capacity != 4096 {
		t.Fatalf("expected default capacity, got %d", c.capacity)
	}
	// Should work normally.
	hash := makeSigTestHash(0x01)
	sig := makeSigTestSig(0x01)
	c.Add(hash, sig, makeSigTestAddr(0x01))
	_, found := c.Lookup(hash, sig)
	if !found {
		t.Fatal("expected hit")
	}
}

func TestSigLRU_Stats(t *testing.T) {
	c := NewSigLRUCache(16)
	hash := makeSigTestHash(0x01)
	sig := makeSigTestSig(0x01)
	c.Add(hash, sig, makeSigTestAddr(0x01))

	c.Lookup(hash, sig)                                  // hit
	c.Lookup(makeSigTestHash(0x99), makeSigTestSig(0x99)) // miss

	stats := c.Stats()
	if stats.Hits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.Entries != 1 {
		t.Fatalf("expected 1 entry, got %d", stats.Entries)
	}
}

func TestSigLRU_DuplicateAdd(t *testing.T) {
	c := NewSigLRUCache(16)
	hash := makeSigTestHash(0x01)
	sig := makeSigTestSig(0x01)

	c.Add(hash, sig, makeSigTestAddr(0xAA))
	c.Add(hash, sig, makeSigTestAddr(0xBB)) // update

	if c.Len() != 1 {
		t.Fatalf("expected len 1 after duplicate add, got %d", c.Len())
	}

	got, found := c.Lookup(hash, sig)
	if !found {
		t.Fatal("expected hit after duplicate add")
	}
	expected := makeSigTestAddr(0xBB)
	if got != expected {
		t.Fatalf("expected updated address %x, got %x", expected, got)
	}
}

func TestSigLRU_LargeCapacity(t *testing.T) {
	c := NewSigLRUCache(10000)
	for i := 0; i < 5000; i++ {
		hash := makeSigTestHash(byte(i % 256))
		// Use different sigs to avoid collisions on the same hash byte.
		var sig [65]byte
		sig[0] = byte(i >> 8)
		sig[1] = byte(i)
		sig[64] = 27
		c.Add(hash, sig, makeSigTestAddr(byte(i)))
	}
	if c.Len() > 10000 {
		t.Fatalf("exceeded capacity: %d", c.Len())
	}
	if c.Len() == 0 {
		t.Fatal("expected non-empty cache")
	}
}

func TestSigLRU_DifferentSigs(t *testing.T) {
	c := NewSigLRUCache(16)
	hash := makeSigTestHash(0x01)
	sig1 := makeSigTestSig(0xAA)
	sig2 := makeSigTestSig(0xBB)
	addr1 := makeSigTestAddr(0x01)
	addr2 := makeSigTestAddr(0x02)

	c.Add(hash, sig1, addr1)
	c.Add(hash, sig2, addr2)

	// Both should be present since they have different sigs.
	got1, found1 := c.Lookup(hash, sig1)
	if !found1 {
		t.Fatal("expected hit for sig1")
	}
	if got1 != addr1 {
		t.Fatalf("wrong address for sig1: got %x, want %x", got1, addr1)
	}

	got2, found2 := c.Lookup(hash, sig2)
	if !found2 {
		t.Fatal("expected hit for sig2")
	}
	if got2 != addr2 {
		t.Fatalf("wrong address for sig2: got %x, want %x", got2, addr2)
	}

	if c.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", c.Len())
	}
}

func TestSigLRU_ConsistentResults(t *testing.T) {
	c := NewSigLRUCache(16)
	hash := makeSigTestHash(0x42)
	sig := makeSigTestSig(0x42)
	addr := makeSigTestAddr(0x42)

	c.Add(hash, sig, addr)

	// Multiple lookups should return the same result.
	for i := 0; i < 20; i++ {
		got, found := c.Lookup(hash, sig)
		if !found {
			t.Fatalf("lookup %d: expected hit", i)
		}
		if got != addr {
			t.Fatalf("lookup %d: got %x, want %x", i, got, addr)
		}
	}

	stats := c.Stats()
	if stats.Hits != 20 {
		t.Fatalf("expected 20 hits, got %d", stats.Hits)
	}
}

func TestSigLRU_LRUPromotion(t *testing.T) {
	c := NewSigLRUCache(3)

	h0, s0, a0 := makeSigTestHash(0), makeSigTestSig(0), makeSigTestAddr(0)
	h1, s1, a1 := makeSigTestHash(1), makeSigTestSig(1), makeSigTestAddr(1)
	h2, s2, a2 := makeSigTestHash(2), makeSigTestSig(2), makeSigTestAddr(2)
	h3, s3, a3 := makeSigTestHash(3), makeSigTestSig(3), makeSigTestAddr(3)

	c.Add(h0, s0, a0)
	c.Add(h1, s1, a1)
	c.Add(h2, s2, a2)

	// Access h0 to promote it.
	c.Lookup(h0, s0)

	// Add h3 -- should evict h1 (LRU), not h0.
	c.Add(h3, s3, a3)

	_, found1 := c.Lookup(h1, s1)
	if found1 {
		t.Fatal("expected h1 to be evicted (LRU)")
	}
	_, found0 := c.Lookup(h0, s0)
	if !found0 {
		t.Fatal("expected h0 to survive after LRU promotion")
	}
}

func TestSigLRU_RemoveNonExistent(t *testing.T) {
	c := NewSigLRUCache(16)
	hash := makeSigTestHash(0x01)
	sig := makeSigTestSig(0x01)
	c.Add(hash, sig, makeSigTestAddr(0x01))

	// Remove a different hash -- should not affect existing entry.
	c.Remove(makeSigTestHash(0x99))

	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}
	_, found := c.Lookup(hash, sig)
	if !found {
		t.Fatal("expected hit after removing non-existent hash")
	}
}
