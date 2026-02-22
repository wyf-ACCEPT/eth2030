package crypto

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// testHash returns a deterministic hash from a byte value.
func testHash(b byte) types.Hash {
	return Keccak256Hash([]byte{b})
}

// testEntry returns a SigCacheEntry with the given validity.
func testEntry(addrByte byte, valid bool, sigType SignatureType) SigCacheEntry {
	return SigCacheEntry{
		Signer:  types.BytesToAddress([]byte{addrByte}),
		Valid:   valid,
		SigType: sigType,
	}
}

func TestNewSignatureCache_DefaultCapacity(t *testing.T) {
	c := NewSignatureCache(0)
	if c.capacity != DefaultSigCacheSize {
		t.Fatalf("expected default capacity %d, got %d", DefaultSigCacheSize, c.capacity)
	}
	c2 := NewSignatureCache(-5)
	if c2.capacity != DefaultSigCacheSize {
		t.Fatalf("expected default capacity for negative input, got %d", c2.capacity)
	}
}

func TestNewSignatureCache_CustomCapacity(t *testing.T) {
	c := NewSignatureCache(128)
	if c.capacity != 128 {
		t.Fatalf("expected capacity 128, got %d", c.capacity)
	}
}

func TestSigCacheKey_Deterministic(t *testing.T) {
	sig := []byte{0x01, 0x02, 0x03}
	msgHash := testHash(0xAA)

	k1 := SigCacheKey(SigTypeECDSA, sig, msgHash)
	k2 := SigCacheKey(SigTypeECDSA, sig, msgHash)
	if k1 != k2 {
		t.Fatal("SigCacheKey is not deterministic")
	}
}

func TestSigCacheKey_DifferentTypes(t *testing.T) {
	sig := []byte{0x01, 0x02, 0x03}
	msgHash := testHash(0xBB)

	k1 := SigCacheKey(SigTypeECDSA, sig, msgHash)
	k2 := SigCacheKey(SigTypeBLS, sig, msgHash)
	if k1 == k2 {
		t.Fatal("different sig types should produce different keys")
	}
}

func TestSigCacheKey_DifferentSigs(t *testing.T) {
	msgHash := testHash(0xCC)

	k1 := SigCacheKey(SigTypeECDSA, []byte{0x01}, msgHash)
	k2 := SigCacheKey(SigTypeECDSA, []byte{0x02}, msgHash)
	if k1 == k2 {
		t.Fatal("different sigs should produce different keys")
	}
}

func TestSignatureCache_AddAndGet(t *testing.T) {
	c := NewSignatureCache(16)
	key := testHash(0x01)
	entry := testEntry(0xAA, true, SigTypeECDSA)

	c.Add(key, entry)

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Signer != entry.Signer || got.Valid != entry.Valid || got.SigType != entry.SigType {
		t.Fatalf("entry mismatch: got %+v, want %+v", got, entry)
	}
}

func TestSignatureCache_Miss(t *testing.T) {
	c := NewSignatureCache(16)
	key := testHash(0x99)

	_, ok := c.Get(key)
	if ok {
		t.Fatal("expected cache miss for unknown key")
	}
}

func TestSignatureCache_HitMissCounters(t *testing.T) {
	c := NewSignatureCache(16)
	key := testHash(0x01)
	c.Add(key, testEntry(0xAA, true, SigTypeECDSA))

	// One miss
	c.Get(testHash(0x99))
	if c.Misses() != 1 {
		t.Fatalf("expected 1 miss, got %d", c.Misses())
	}
	if c.Hits() != 0 {
		t.Fatalf("expected 0 hits, got %d", c.Hits())
	}

	// One hit
	c.Get(key)
	if c.Hits() != 1 {
		t.Fatalf("expected 1 hit, got %d", c.Hits())
	}
}

func TestSignatureCache_HitRate(t *testing.T) {
	c := NewSignatureCache(16)
	// No lookups yet.
	if c.HitRate() != 0 {
		t.Fatal("expected 0 hit rate with no lookups")
	}

	key := testHash(0x01)
	c.Add(key, testEntry(0xAA, true, SigTypeECDSA))

	c.Get(key)            // hit
	c.Get(testHash(0x99)) // miss

	rate := c.HitRate()
	if rate < 0.49 || rate > 0.51 {
		t.Fatalf("expected ~0.5 hit rate, got %f", rate)
	}
}

func TestSignatureCache_Contains(t *testing.T) {
	c := NewSignatureCache(16)
	key := testHash(0x01)
	c.Add(key, testEntry(0xAA, true, SigTypeECDSA))

	if !c.Contains(key) {
		t.Fatal("expected Contains to return true for added key")
	}
	if c.Contains(testHash(0x99)) {
		t.Fatal("expected Contains to return false for missing key")
	}
}

func TestSignatureCache_Len(t *testing.T) {
	c := NewSignatureCache(16)
	if c.Len() != 0 {
		t.Fatalf("expected empty cache, got len %d", c.Len())
	}

	for i := byte(0); i < 5; i++ {
		c.Add(testHash(i), testEntry(i, true, SigTypeECDSA))
	}
	if c.Len() != 5 {
		t.Fatalf("expected len 5, got %d", c.Len())
	}
}

func TestSignatureCache_Eviction(t *testing.T) {
	capacity := 4
	c := NewSignatureCache(capacity)

	// Fill to capacity.
	keys := make([]types.Hash, capacity+2)
	for i := 0; i < capacity+2; i++ {
		keys[i] = testHash(byte(i))
		c.Add(keys[i], testEntry(byte(i), true, SigTypeECDSA))
	}

	// Cache should not exceed capacity.
	if c.Len() != capacity {
		t.Fatalf("expected len %d after eviction, got %d", capacity, c.Len())
	}

	// The first entries (LRU) should have been evicted.
	if c.Contains(keys[0]) {
		t.Fatal("expected key 0 to be evicted")
	}
	if c.Contains(keys[1]) {
		t.Fatal("expected key 1 to be evicted")
	}

	// The most recent entries should still be present.
	if !c.Contains(keys[capacity+1]) {
		t.Fatal("expected most recent key to be present")
	}
}

func TestSignatureCache_LRUPromotion(t *testing.T) {
	c := NewSignatureCache(3)

	k0 := testHash(0)
	k1 := testHash(1)
	k2 := testHash(2)
	k3 := testHash(3)

	c.Add(k0, testEntry(0, true, SigTypeECDSA))
	c.Add(k1, testEntry(1, true, SigTypeECDSA))
	c.Add(k2, testEntry(2, true, SigTypeECDSA))

	// Access k0, promoting it to most-recently-used.
	c.Get(k0)

	// Add k3 -- should evict k1 (the LRU), not k0.
	c.Add(k3, testEntry(3, true, SigTypeECDSA))

	if c.Contains(k1) {
		t.Fatal("expected k1 to be evicted (LRU)")
	}
	if !c.Contains(k0) {
		t.Fatal("expected k0 to survive after LRU promotion")
	}
}

func TestSignatureCache_UpdateExisting(t *testing.T) {
	c := NewSignatureCache(16)
	key := testHash(0x01)

	c.Add(key, testEntry(0xAA, false, SigTypeECDSA))
	c.Add(key, testEntry(0xBB, true, SigTypeBLS))

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected hit after update")
	}
	if got.Signer != (types.BytesToAddress([]byte{0xBB})) {
		t.Fatal("expected updated signer")
	}
	if !got.Valid || got.SigType != SigTypeBLS {
		t.Fatal("expected updated entry fields")
	}

	// Len should not increase for duplicate key.
	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}
}

func TestSignatureCache_Remove(t *testing.T) {
	c := NewSignatureCache(16)
	key := testHash(0x01)
	c.Add(key, testEntry(0xAA, true, SigTypeECDSA))

	if !c.Remove(key) {
		t.Fatal("expected Remove to return true for existing key")
	}
	if c.Len() != 0 {
		t.Fatalf("expected len 0 after remove, got %d", c.Len())
	}
	if c.Remove(key) {
		t.Fatal("expected Remove to return false for already-removed key")
	}
}

func TestSignatureCache_Purge(t *testing.T) {
	c := NewSignatureCache(16)
	for i := byte(0); i < 10; i++ {
		c.Add(testHash(i), testEntry(i, true, SigTypeECDSA))
	}
	c.Get(testHash(0))  // hit
	c.Get(testHash(99)) // miss

	c.Purge()

	if c.Len() != 0 {
		t.Fatalf("expected empty cache after purge, got %d", c.Len())
	}
	if c.Hits() != 0 || c.Misses() != 0 {
		t.Fatal("expected counters reset after purge")
	}
}

func TestSignatureCache_ConcurrentAccess(t *testing.T) {
	c := NewSignatureCache(256)
	const goroutines = 16
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := testHash(byte(id*opsPerGoroutine + i))
				entry := testEntry(byte(id), true, SigTypeECDSA)

				c.Add(key, entry)
				c.Get(key)
				c.Contains(key)
				c.Len()
			}
		}(g)
	}
	wg.Wait()

	// Verify no panic occurred and cache is consistent.
	if c.Len() > 256 {
		t.Fatalf("cache exceeded capacity: %d", c.Len())
	}
	total := c.Hits() + c.Misses()
	if total == 0 {
		t.Fatal("expected some lookups")
	}
}

func TestSignatureCache_EvictionOrder(t *testing.T) {
	// With capacity 2, inserting 3 items should evict the first.
	c := NewSignatureCache(2)
	k0 := testHash(10)
	k1 := testHash(11)
	k2 := testHash(12)

	c.Add(k0, testEntry(0, true, SigTypeECDSA))
	c.Add(k1, testEntry(1, true, SigTypeECDSA))
	c.Add(k2, testEntry(2, true, SigTypeECDSA))

	if c.Contains(k0) {
		t.Fatal("expected k0 evicted")
	}
	if !c.Contains(k1) || !c.Contains(k2) {
		t.Fatal("expected k1 and k2 to remain")
	}
}
