package proofs

import (
	"sync"
	"testing"
	"time"
)

func makeProofHash(b byte) [32]byte {
	var h [32]byte
	h[0] = b
	return h
}

func makeCachedResult(proverID string, valid bool) *CachedProofResult {
	return &CachedProofResult{
		Valid:        valid,
		VerifiedAt:   time.Now().Unix(),
		ProverID:     proverID,
		GasCost:      21000,
		VerifyTimeMs: 15,
	}
}

func TestProofCache_NewCache(t *testing.T) {
	c := NewProofCache(100, 60)
	if c == nil {
		t.Fatal("NewProofCache returned nil")
	}
	stats := c.Stats()
	if stats.Entries != 0 {
		t.Fatalf("expected 0 entries, got %d", stats.Entries)
	}
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Fatal("expected zero hits and misses")
	}
}

func TestProofCache_NewCacheDefaults(t *testing.T) {
	c := NewProofCache(0, 0)
	if c.maxEntries != 1024 {
		t.Fatalf("expected default maxEntries=1024, got %d", c.maxEntries)
	}
	if c.ttlSeconds != 0 {
		t.Fatalf("expected ttlSeconds=0, got %d", c.ttlSeconds)
	}
}

func TestProofCache_CacheAndLookup(t *testing.T) {
	c := NewProofCache(100, 60)
	hash := makeProofHash(1)
	result := makeCachedResult("prover-1", true)

	c.CacheProof(hash, "ZK-SNARK", result)

	got, ok := c.LookupProof(hash)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.ProverID != "prover-1" {
		t.Fatalf("expected prover-1, got %s", got.ProverID)
	}
	if !got.Valid {
		t.Fatal("expected valid=true")
	}
}

func TestProofCache_LookupMiss(t *testing.T) {
	c := NewProofCache(100, 60)
	hash := makeProofHash(99)

	got, ok := c.LookupProof(hash)
	if ok {
		t.Fatal("expected cache miss")
	}
	if got != nil {
		t.Fatal("expected nil result on miss")
	}

	stats := c.Stats()
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestProofCache_Expiration(t *testing.T) {
	// TTL of 1 second.
	c := NewProofCache(100, 1)
	hash := makeProofHash(2)
	result := makeCachedResult("prover-2", true)

	c.CacheProof(hash, "KZG", result)

	// Immediately should be present.
	_, ok := c.LookupProof(hash)
	if !ok {
		t.Fatal("expected hit before expiration")
	}

	// Manually backdate the entry to simulate time passing.
	c.mu.Lock()
	if entry, exists := c.entries[hash]; exists {
		entry.insertedAt = time.Now().Unix() - 5
	}
	c.mu.Unlock()

	// Now should be expired.
	_, ok = c.LookupProof(hash)
	if ok {
		t.Fatal("expected miss after expiration")
	}

	stats := c.Stats()
	if stats.Expirations != 1 {
		t.Fatalf("expected 1 expiration, got %d", stats.Expirations)
	}
}

func TestProofCache_PruneExpired(t *testing.T) {
	c := NewProofCache(100, 1)

	// Insert 3 entries and backdate them.
	for i := byte(0); i < 3; i++ {
		h := makeProofHash(i)
		c.CacheProof(h, "IPA", makeCachedResult("p", true))
	}

	// Backdate all entries.
	c.mu.Lock()
	for _, entry := range c.entries {
		entry.insertedAt = time.Now().Unix() - 10
	}
	c.mu.Unlock()

	pruned := c.PruneExpired()
	if pruned != 3 {
		t.Fatalf("expected 3 pruned, got %d", pruned)
	}

	stats := c.Stats()
	if stats.Entries != 0 {
		t.Fatalf("expected 0 entries after prune, got %d", stats.Entries)
	}
}

func TestProofCache_PruneExpiredNoTTL(t *testing.T) {
	c := NewProofCache(100, 0)
	c.CacheProof(makeProofHash(1), "KZG", makeCachedResult("p", true))

	pruned := c.PruneExpired()
	if pruned != 0 {
		t.Fatalf("expected 0 pruned with zero TTL, got %d", pruned)
	}
}

func TestProofCache_Invalidate(t *testing.T) {
	c := NewProofCache(100, 60)
	hash := makeProofHash(5)
	c.CacheProof(hash, "ZK-STARK", makeCachedResult("prover-5", true))

	c.Invalidate(hash)

	_, ok := c.LookupProof(hash)
	if ok {
		t.Fatal("expected miss after invalidation")
	}

	// Invalidating non-existent entry should be safe.
	c.Invalidate(makeProofHash(99))
}

func TestProofCache_InvalidateByProver(t *testing.T) {
	c := NewProofCache(100, 60)

	// Insert entries from two provers.
	for i := byte(0); i < 5; i++ {
		h := makeProofHash(i)
		prover := "alice"
		if i%2 == 0 {
			prover = "bob"
		}
		c.CacheProof(h, "KZG", makeCachedResult(prover, true))
	}

	removed := c.InvalidateByProver("bob")
	if removed != 3 {
		t.Fatalf("expected 3 removed for bob, got %d", removed)
	}

	stats := c.Stats()
	if stats.Entries != 2 {
		t.Fatalf("expected 2 remaining entries, got %d", stats.Entries)
	}

	// Verify alice's entries are still there.
	_, ok := c.LookupProof(makeProofHash(1))
	if !ok {
		t.Fatal("expected alice's entry to remain")
	}
}

func TestProofCache_InvalidateByProverNone(t *testing.T) {
	c := NewProofCache(100, 60)
	c.CacheProof(makeProofHash(1), "KZG", makeCachedResult("alice", true))

	removed := c.InvalidateByProver("charlie")
	if removed != 0 {
		t.Fatalf("expected 0 removed, got %d", removed)
	}
}

func TestProofCache_Stats(t *testing.T) {
	c := NewProofCache(100, 60)

	c.CacheProof(makeProofHash(1), "KZG", makeCachedResult("p1", true))
	c.CacheProof(makeProofHash(2), "IPA", makeCachedResult("p2", false))

	// 2 hits.
	c.LookupProof(makeProofHash(1))
	c.LookupProof(makeProofHash(2))
	// 1 miss.
	c.LookupProof(makeProofHash(99))

	stats := c.Stats()
	if stats.Entries != 2 {
		t.Fatalf("expected 2 entries, got %d", stats.Entries)
	}
	if stats.Hits != 2 {
		t.Fatalf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestProofCache_HitRate(t *testing.T) {
	c := NewProofCache(100, 60)

	// No lookups: hit rate should be 0.
	if rate := c.HitRate(); rate != 0.0 {
		t.Fatalf("expected 0.0 hit rate, got %f", rate)
	}

	c.CacheProof(makeProofHash(1), "KZG", makeCachedResult("p1", true))

	// 1 hit, 1 miss = 0.5.
	c.LookupProof(makeProofHash(1))
	c.LookupProof(makeProofHash(99))

	rate := c.HitRate()
	if rate < 0.49 || rate > 0.51 {
		t.Fatalf("expected ~0.5 hit rate, got %f", rate)
	}
}

func TestProofCache_ConcurrentAccess(t *testing.T) {
	c := NewProofCache(1000, 60)
	var wg sync.WaitGroup

	// 10 goroutines writing.
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				h := makeProofHash(byte((id*50 + i) % 256))
				c.CacheProof(h, "KZG", makeCachedResult("p", true))
			}
		}(g)
	}

	// 10 goroutines reading.
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				h := makeProofHash(byte((id*50 + i) % 256))
				c.LookupProof(h)
			}
		}(g)
	}

	wg.Wait()

	stats := c.Stats()
	if stats.Entries > 1000 {
		t.Fatalf("entries should not exceed max, got %d", stats.Entries)
	}
}

func TestProofCache_ZeroTTL(t *testing.T) {
	// TTL of 0 means entries never expire by time.
	c := NewProofCache(100, 0)
	hash := makeProofHash(10)
	c.CacheProof(hash, "IPA", makeCachedResult("p", true))

	// Backdate the entry far in the past.
	c.mu.Lock()
	if entry, exists := c.entries[hash]; exists {
		entry.insertedAt = time.Now().Unix() - 999999
	}
	c.mu.Unlock()

	// Should still be found (no TTL expiry).
	_, ok := c.LookupProof(hash)
	if !ok {
		t.Fatal("expected hit with zero TTL regardless of age")
	}
}

func TestProofCache_LargeCache(t *testing.T) {
	c := NewProofCache(500, 3600)

	// Fill with 500 entries.
	for i := 0; i < 500; i++ {
		var h [32]byte
		h[0] = byte(i)
		h[1] = byte(i >> 8)
		c.CacheProof(h, "ZK-SNARK", makeCachedResult("p", true))
	}

	stats := c.Stats()
	if stats.Entries != 500 {
		t.Fatalf("expected 500 entries, got %d", stats.Entries)
	}
}

func TestProofCache_DuplicateCache(t *testing.T) {
	c := NewProofCache(100, 60)
	hash := makeProofHash(7)

	result1 := makeCachedResult("prover-A", true)
	result2 := makeCachedResult("prover-B", false)

	c.CacheProof(hash, "KZG", result1)
	c.CacheProof(hash, "KZG", result2) // overwrite

	got, ok := c.LookupProof(hash)
	if !ok {
		t.Fatal("expected hit after duplicate cache")
	}
	if got.ProverID != "prover-B" {
		t.Fatalf("expected prover-B after overwrite, got %s", got.ProverID)
	}
	if got.Valid {
		t.Fatal("expected valid=false after overwrite")
	}

	// Should still be 1 entry, not 2.
	stats := c.Stats()
	if stats.Entries != 1 {
		t.Fatalf("expected 1 entry after duplicate, got %d", stats.Entries)
	}
}

func TestProofCache_Eviction(t *testing.T) {
	c := NewProofCache(3, 60)

	h1 := makeProofHash(1)
	h2 := makeProofHash(2)
	h3 := makeProofHash(3)
	h4 := makeProofHash(4)

	c.CacheProof(h1, "KZG", makeCachedResult("p1", true))
	c.CacheProof(h2, "KZG", makeCachedResult("p2", true))
	c.CacheProof(h3, "KZG", makeCachedResult("p3", true))

	// Cache is full. Adding h4 should evict h1 (oldest).
	c.CacheProof(h4, "KZG", makeCachedResult("p4", true))

	_, ok := c.LookupProof(h1)
	if ok {
		t.Fatal("h1 should have been evicted")
	}

	_, ok = c.LookupProof(h4)
	if !ok {
		t.Fatal("h4 should be present")
	}

	stats := c.Stats()
	if stats.Evictions != 1 {
		t.Fatalf("expected 1 eviction, got %d", stats.Evictions)
	}
	if stats.Entries != 3 {
		t.Fatalf("expected 3 entries, got %d", stats.Entries)
	}
}

func TestProofCache_CacheResultFields(t *testing.T) {
	c := NewProofCache(100, 60)
	hash := makeProofHash(20)

	now := time.Now().Unix()
	result := &CachedProofResult{
		Valid:        true,
		VerifiedAt:   now,
		ProverID:     "validator-42",
		GasCost:      42000,
		VerifyTimeMs: 250,
	}

	c.CacheProof(hash, "ZK-STARK", result)

	got, ok := c.LookupProof(hash)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Valid != true {
		t.Fatal("Valid mismatch")
	}
	if got.VerifiedAt != now {
		t.Fatalf("VerifiedAt mismatch: got %d, want %d", got.VerifiedAt, now)
	}
	if got.ProverID != "validator-42" {
		t.Fatalf("ProverID mismatch: got %s", got.ProverID)
	}
	if got.GasCost != 42000 {
		t.Fatalf("GasCost mismatch: got %d", got.GasCost)
	}
	if got.VerifyTimeMs != 250 {
		t.Fatalf("VerifyTimeMs mismatch: got %d", got.VerifyTimeMs)
	}
}

func TestProofCache_CacheNilResult(t *testing.T) {
	c := NewProofCache(100, 60)
	hash := makeProofHash(30)

	// Caching nil should be a no-op.
	c.CacheProof(hash, "KZG", nil)

	_, ok := c.LookupProof(hash)
	if ok {
		t.Fatal("nil result should not be cached")
	}
}

func TestProofCache_MultipleEvictions(t *testing.T) {
	c := NewProofCache(2, 60)

	for i := byte(0); i < 10; i++ {
		h := makeProofHash(i)
		c.CacheProof(h, "IPA", makeCachedResult("p", true))
	}

	stats := c.Stats()
	if stats.Entries != 2 {
		t.Fatalf("expected 2 entries, got %d", stats.Entries)
	}
	if stats.Evictions != 8 {
		t.Fatalf("expected 8 evictions, got %d", stats.Evictions)
	}
}
