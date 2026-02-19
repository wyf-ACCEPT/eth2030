package light

import (
	"sync"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

func TestProofCache_PutGet(t *testing.T) {
	cache := NewProofCache(100, 10*time.Minute)

	key := CacheKey{
		BlockNumber: 1000,
		Address:     types.HexToAddress("0xaaaa"),
		Type:        ProofTypeAccount,
	}
	proof := []byte{0x01, 0x02, 0x03}
	value := []byte{0xaa, 0xbb}

	cache.Put(key, proof, value)

	got, ok := cache.Get(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got.Proof) != 3 || got.Proof[0] != 0x01 {
		t.Errorf("proof mismatch: got %x", got.Proof)
	}
	if len(got.Value) != 2 || got.Value[0] != 0xaa {
		t.Errorf("value mismatch: got %x", got.Value)
	}

	// Verify the cache returns a copy (modifying original should not affect cache).
	proof[0] = 0xff
	got2, ok := cache.Get(key)
	if !ok {
		t.Fatal("expected cache hit on second get")
	}
	if got2.Proof[0] != 0x01 {
		t.Error("cache should hold independent copy of proof data")
	}
}

func TestProofCache_Miss(t *testing.T) {
	cache := NewProofCache(10, time.Minute)

	key := CacheKey{BlockNumber: 999, Type: ProofTypeHeader}
	_, ok := cache.Get(key)
	if ok {
		t.Error("expected cache miss for absent key")
	}

	stats := cache.Stats()
	if stats.Misses != 1 {
		t.Errorf("misses = %d, want 1", stats.Misses)
	}
}

func TestProofCache_Update(t *testing.T) {
	cache := NewProofCache(10, time.Minute)

	key := CacheKey{BlockNumber: 42, Type: ProofTypeStorage}
	cache.Put(key, []byte{0x01}, []byte{0x10})
	cache.Put(key, []byte{0x02}, []byte{0x20})

	got, ok := cache.Get(key)
	if !ok {
		t.Fatal("expected cache hit after update")
	}
	if got.Proof[0] != 0x02 || got.Value[0] != 0x20 {
		t.Errorf("expected updated values, got proof=%x value=%x", got.Proof, got.Value)
	}

	if cache.Len() != 1 {
		t.Errorf("cache len = %d, want 1 after update", cache.Len())
	}
}

func TestProofCache_EvictionLRU(t *testing.T) {
	cache := NewProofCache(3, time.Hour)

	// Insert 3 entries: keys 0, 1, 2.
	for i := uint64(0); i < 3; i++ {
		key := CacheKey{BlockNumber: i, Type: ProofTypeAccount}
		cache.Put(key, []byte{byte(i)}, nil)
	}

	if cache.Len() != 3 {
		t.Fatalf("cache len = %d, want 3", cache.Len())
	}

	// Access key 0 to make it most recently used.
	cache.Get(CacheKey{BlockNumber: 0, Type: ProofTypeAccount})

	// Insert key 3 -- should evict key 1 (least recently used).
	cache.Put(CacheKey{BlockNumber: 3, Type: ProofTypeAccount}, []byte{0x03}, nil)

	if cache.Len() != 3 {
		t.Fatalf("cache len = %d, want 3 after eviction", cache.Len())
	}

	// Key 1 should be evicted.
	_, ok := cache.Get(CacheKey{BlockNumber: 1, Type: ProofTypeAccount})
	if ok {
		t.Error("key 1 should have been evicted")
	}

	// Key 0 should still be present (was promoted by Get).
	_, ok = cache.Get(CacheKey{BlockNumber: 0, Type: ProofTypeAccount})
	if !ok {
		t.Error("key 0 should still be present")
	}

	// Key 2 should still be present.
	_, ok = cache.Get(CacheKey{BlockNumber: 2, Type: ProofTypeAccount})
	if !ok {
		t.Error("key 2 should still be present")
	}

	stats := cache.Stats()
	if stats.Evictions == 0 {
		t.Error("expected at least one eviction")
	}
}

func TestProofCache_TTLExpiry(t *testing.T) {
	now := time.Now()
	clock := now

	cache := NewProofCache(10, 5*time.Second)
	cache.nowFunc = func() time.Time { return clock }

	key := CacheKey{BlockNumber: 100, Type: ProofTypeHeader}
	cache.Put(key, []byte{0x01}, nil)

	// Immediately should be a hit.
	_, ok := cache.Get(key)
	if !ok {
		t.Fatal("expected hit before TTL expires")
	}

	// Advance time past TTL.
	clock = now.Add(6 * time.Second)

	_, ok = cache.Get(key)
	if ok {
		t.Error("expected miss after TTL expires")
	}

	// Entry should be removed after expiry check.
	if cache.Len() != 0 {
		t.Errorf("cache len = %d, want 0 after TTL expiry", cache.Len())
	}
}

func TestProofCache_ExplicitEvict(t *testing.T) {
	cache := NewProofCache(10, time.Hour)

	key := CacheKey{BlockNumber: 5, Type: ProofTypeStorage}
	cache.Put(key, []byte{0xff}, nil)

	removed := cache.Evict(key)
	if !removed {
		t.Error("expected Evict to return true")
	}

	_, ok := cache.Get(key)
	if ok {
		t.Error("expected miss after explicit eviction")
	}

	// Evicting a nonexistent key should return false.
	if cache.Evict(key) {
		t.Error("expected Evict of absent key to return false")
	}
}

func TestProofCache_ConcurrentAccess(t *testing.T) {
	cache := NewProofCache(100, time.Minute)
	var wg sync.WaitGroup

	// Concurrent writers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := CacheKey{BlockNumber: uint64(n), Type: ProofTypeAccount}
			cache.Put(key, []byte{byte(n)}, []byte{byte(n + 1)})
		}(i)
	}

	// Concurrent readers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := CacheKey{BlockNumber: uint64(n), Type: ProofTypeAccount}
			cache.Get(key)
		}(i)
	}

	wg.Wait()

	// All entries should be present.
	if cache.Len() > 100 {
		t.Errorf("cache len = %d, exceeds max size 100", cache.Len())
	}
}

func TestProofCache_Statistics(t *testing.T) {
	cache := NewProofCache(10, time.Hour)

	key := CacheKey{BlockNumber: 1, Type: ProofTypeHeader}

	// Miss.
	cache.Get(key)

	// Insert.
	cache.Put(key, []byte{0x01}, []byte{0x02})

	// Hit.
	cache.Get(key)

	stats := cache.Stats()
	if stats.Hits != 1 {
		t.Errorf("hits = %d, want 1", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("misses = %d, want 1", stats.Misses)
	}
	if stats.Inserts != 1 {
		t.Errorf("inserts = %d, want 1", stats.Inserts)
	}
	if stats.MemoryUsed == 0 {
		t.Error("memory used should be > 0 after insert")
	}

	hr := stats.HitRate()
	if hr < 0.49 || hr > 0.51 {
		t.Errorf("hit rate = %f, want ~0.5", hr)
	}
}

func TestProofCache_HitRateZero(t *testing.T) {
	stats := CacheStats{}
	if hr := stats.HitRate(); hr != 0 {
		t.Errorf("hit rate with zero lookups = %f, want 0", hr)
	}
}

func TestProofCache_MaxSizeDefault(t *testing.T) {
	cache := NewProofCache(0, time.Minute)
	if cache.MaxSize() != 1024 {
		t.Errorf("default max size = %d, want 1024", cache.MaxSize())
	}
}

func TestProofCache_Prefetch(t *testing.T) {
	cache := NewProofCache(100, time.Minute)

	addr := types.HexToAddress("0xbbbb")
	storageKey := types.HexToHash("0x01")

	fetchCalled := make(chan struct{}, 10)
	fetch := func(block uint64) ([]byte, []byte) {
		fetchCalled <- struct{}{}
		return []byte{byte(block)}, []byte{byte(block + 1)}
	}

	cache.Prefetch(100, addr, storageKey, ProofTypeStorage, 5, fetch)

	// Wait for all prefetch calls.
	for i := 0; i < 5; i++ {
		select {
		case <-fetchCalled:
		case <-time.After(2 * time.Second):
			t.Fatalf("prefetch timeout waiting for call %d", i)
		}
	}

	// Give a moment for the Put calls to complete.
	time.Sleep(50 * time.Millisecond)

	// Verify all prefetched entries are in the cache.
	for i := uint64(0); i < 5; i++ {
		key := CacheKey{
			BlockNumber: 100 + i,
			Address:     addr,
			StorageKey:  storageKey,
			Type:        ProofTypeStorage,
		}
		got, ok := cache.Get(key)
		if !ok {
			t.Errorf("expected prefetched entry for block %d", 100+i)
			continue
		}
		if got.Proof[0] != byte(100+i) {
			t.Errorf("block %d proof = %x, want %x", 100+i, got.Proof[0], byte(100+i))
		}
	}
}

func TestProofCache_PrefetchNilFetch(t *testing.T) {
	cache := NewProofCache(10, time.Minute)
	// Should not panic with nil fetch function.
	cache.Prefetch(0, types.Address{}, types.Hash{}, ProofTypeHeader, 5, nil)
}

func TestProofCache_PrefetchZeroCount(t *testing.T) {
	cache := NewProofCache(10, time.Minute)
	called := false
	cache.Prefetch(0, types.Address{}, types.Hash{}, ProofTypeHeader, 0, func(block uint64) ([]byte, []byte) {
		called = true
		return nil, nil
	})
	time.Sleep(50 * time.Millisecond)
	if called {
		t.Error("fetch should not be called with count=0")
	}
}

func TestProofCache_MemoryTracking(t *testing.T) {
	cache := NewProofCache(10, time.Hour)

	key := CacheKey{BlockNumber: 1, Type: ProofTypeAccount}
	cache.Put(key, make([]byte, 100), make([]byte, 50))

	stats := cache.Stats()
	if stats.MemoryUsed == 0 {
		t.Error("memory should be tracked after insert")
	}

	beforeEvict := stats.MemoryUsed
	cache.Evict(key)

	stats = cache.Stats()
	if stats.MemoryUsed >= beforeEvict {
		t.Error("memory should decrease after eviction")
	}
}

func TestProofCache_DifferentProofTypes(t *testing.T) {
	cache := NewProofCache(10, time.Hour)

	base := CacheKey{BlockNumber: 1, Address: types.HexToAddress("0xaa")}

	// Same block+address, different proof types should be separate entries.
	headerKey := base
	headerKey.Type = ProofTypeHeader
	cache.Put(headerKey, []byte{0x01}, nil)

	accountKey := base
	accountKey.Type = ProofTypeAccount
	cache.Put(accountKey, []byte{0x02}, nil)

	storageKey := base
	storageKey.Type = ProofTypeStorage
	cache.Put(storageKey, []byte{0x03}, nil)

	if cache.Len() != 3 {
		t.Errorf("cache len = %d, want 3 for different proof types", cache.Len())
	}

	got, ok := cache.Get(accountKey)
	if !ok || got.Proof[0] != 0x02 {
		t.Error("account proof mismatch")
	}
}
