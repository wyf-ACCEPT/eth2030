package engine

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func makePayload(id byte, size int) *CachedPayload {
	var h types.Hash
	h[0] = id
	return &CachedPayload{
		ID:           h,
		ParentHash:   types.Hash{},
		FeeRecipient: types.Address{},
		Timestamp:    uint64(time.Now().Unix()),
		GasLimit:     30_000_000,
		BaseFee:      big.NewInt(1_000_000_000),
		Transactions: [][]byte{{0x01}},
		CreatedAt:    time.Now(),
		Size:         size,
	}
}

func TestPayloadCacheStore(t *testing.T) {
	cache := NewPayloadCache(DefaultPayloadCacheConfig())
	p := makePayload(1, 100)

	if err := cache.Store(p); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	got, ok := cache.Get(p.ID)
	if !ok {
		t.Fatal("expected payload to be found after store")
	}
	if got.ID != p.ID {
		t.Fatalf("want ID %v, got %v", p.ID, got.ID)
	}
}

func TestPayloadCacheGet(t *testing.T) {
	cache := NewPayloadCache(DefaultPayloadCacheConfig())
	p := makePayload(2, 200)

	cache.Store(p)

	got, ok := cache.Get(p.ID)
	if !ok {
		t.Fatal("expected payload to be found")
	}
	if got.GasLimit != p.GasLimit {
		t.Fatalf("want GasLimit %d, got %d", p.GasLimit, got.GasLimit)
	}
	if got.BaseFee.Cmp(p.BaseFee) != 0 {
		t.Fatalf("want BaseFee %v, got %v", p.BaseFee, got.BaseFee)
	}
}

func TestPayloadCacheGetMissing(t *testing.T) {
	cache := NewPayloadCache(DefaultPayloadCacheConfig())

	_, ok := cache.Get(types.Hash{0xff})
	if ok {
		t.Fatal("expected false for missing payload")
	}
}

func TestPayloadCacheDelete(t *testing.T) {
	cache := NewPayloadCache(DefaultPayloadCacheConfig())
	p := makePayload(3, 100)

	cache.Store(p)
	cache.Delete(p.ID)

	_, ok := cache.Get(p.ID)
	if ok {
		t.Fatal("expected payload to be gone after delete")
	}
	if cache.Size() != 0 {
		t.Fatalf("want size 0, got %d", cache.Size())
	}
}

func TestPayloadCachePrune(t *testing.T) {
	config := DefaultPayloadCacheConfig()
	config.PayloadTTL = 50 * time.Millisecond
	cache := NewPayloadCache(config)

	p := makePayload(4, 100)
	p.CreatedAt = time.Now().Add(-100 * time.Millisecond) // already expired
	cache.Store(p)

	// Store a fresh payload that should survive pruning.
	fresh := makePayload(5, 100)
	cache.Store(fresh)

	cache.Prune()

	if cache.Size() != 1 {
		t.Fatalf("want 1 payload after prune, got %d", cache.Size())
	}
	_, ok := cache.Get(p.ID)
	if ok {
		t.Fatal("expired payload should have been pruned")
	}
	_, ok = cache.Get(fresh.ID)
	if !ok {
		t.Fatal("fresh payload should still exist")
	}
}

func TestPayloadCacheLRU(t *testing.T) {
	config := DefaultPayloadCacheConfig()
	config.MaxPayloads = 3
	cache := NewPayloadCache(config)

	// Store 3 payloads.
	p1 := makePayload(1, 100)
	p2 := makePayload(2, 100)
	p3 := makePayload(3, 100)

	cache.Store(p1)
	time.Sleep(time.Millisecond)
	cache.Store(p2)
	time.Sleep(time.Millisecond)
	cache.Store(p3)

	// Access p1 to make it most recent.
	cache.Get(p1.ID)
	time.Sleep(time.Millisecond)

	// Store p4; p2 should be evicted as LRU (p1 was just accessed).
	p4 := makePayload(4, 100)
	cache.Store(p4)

	if cache.Size() != 3 {
		t.Fatalf("want 3 payloads, got %d", cache.Size())
	}
	_, ok := cache.Get(p2.ID)
	if ok {
		t.Fatal("p2 should have been evicted as LRU")
	}
	// p1, p3, p4 should still be present.
	if _, ok := cache.Get(p1.ID); !ok {
		t.Fatal("p1 should still be cached")
	}
	if _, ok := cache.Get(p3.ID); !ok {
		t.Fatal("p3 should still be cached")
	}
	if _, ok := cache.Get(p4.ID); !ok {
		t.Fatal("p4 should still be cached")
	}
}

func TestPayloadCacheSize(t *testing.T) {
	cache := NewPayloadCache(DefaultPayloadCacheConfig())

	if cache.Size() != 0 {
		t.Fatalf("want 0, got %d", cache.Size())
	}

	cache.Store(makePayload(1, 500))
	cache.Store(makePayload(2, 300))

	if cache.Size() != 2 {
		t.Fatalf("want 2, got %d", cache.Size())
	}
	if cache.TotalBytes() != 800 {
		t.Fatalf("want 800 bytes, got %d", cache.TotalBytes())
	}
}

func TestConcurrentAccess(t *testing.T) {
	cache := NewPayloadCache(DefaultPayloadCacheConfig())

	var wg sync.WaitGroup
	// Concurrent stores.
	for i := byte(0); i < 20; i++ {
		wg.Add(1)
		go func(id byte) {
			defer wg.Done()
			cache.Store(makePayload(id, 100))
		}(i)
	}

	// Concurrent reads.
	for i := byte(0); i < 20; i++ {
		wg.Add(1)
		go func(id byte) {
			defer wg.Done()
			cache.Get(types.Hash{id})
		}(i)
	}

	wg.Wait()

	if cache.Size() != 20 {
		t.Fatalf("want 20, got %d", cache.Size())
	}
}

func TestPayloadCacheStoreOversized(t *testing.T) {
	config := DefaultPayloadCacheConfig()
	config.MaxPayloadSize = 100
	cache := NewPayloadCache(config)

	p := makePayload(1, 200) // exceeds max
	err := cache.Store(p)
	if err == nil {
		t.Fatal("expected error for oversized payload")
	}
	if cache.Size() != 0 {
		t.Fatalf("want 0, got %d", cache.Size())
	}
}

func TestPayloadCacheStoreNil(t *testing.T) {
	cache := NewPayloadCache(DefaultPayloadCacheConfig())
	err := cache.Store(nil)
	if err == nil {
		t.Fatal("expected error for nil payload")
	}
}
