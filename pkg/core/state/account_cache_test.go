package state

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func testAddress(b byte) types.Address {
	var addr types.Address
	addr[19] = b
	return addr
}

func testAccount(nonce uint64, balance int64) *types.Account {
	return &types.Account{
		Nonce:    nonce,
		Balance:  big.NewInt(balance),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash.Bytes(),
	}
}

func TestAccountCache_PutAndGet(t *testing.T) {
	cache := NewAccountCache(10)
	addr := testAddress(1)
	acct := testAccount(1, 1000)

	cache.Put(addr, acct)
	got, ok := cache.Get(addr)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Nonce != acct.Nonce {
		t.Errorf("nonce: got %d, want %d", got.Nonce, acct.Nonce)
	}
	if got.Balance.Cmp(acct.Balance) != 0 {
		t.Errorf("balance: got %s, want %s", got.Balance, acct.Balance)
	}
}

func TestAccountCache_GetMiss(t *testing.T) {
	cache := NewAccountCache(10)
	addr := testAddress(1)

	got, ok := cache.Get(addr)
	if ok {
		t.Fatal("expected cache miss")
	}
	if got != nil {
		t.Fatal("expected nil on miss")
	}
}

func TestAccountCache_Delete(t *testing.T) {
	cache := NewAccountCache(10)
	addr := testAddress(1)
	cache.Put(addr, testAccount(1, 100))

	cache.Delete(addr)
	_, ok := cache.Get(addr)
	if ok {
		t.Fatal("expected miss after delete")
	}
	if cache.Len() != 0 {
		t.Errorf("expected len 0, got %d", cache.Len())
	}
}

func TestAccountCache_DeleteNonExistent(t *testing.T) {
	cache := NewAccountCache(10)
	// Should not panic.
	cache.Delete(testAddress(99))
}

func TestAccountCache_Len(t *testing.T) {
	cache := NewAccountCache(10)
	if cache.Len() != 0 {
		t.Fatalf("expected 0, got %d", cache.Len())
	}
	for i := byte(0); i < 5; i++ {
		cache.Put(testAddress(i), testAccount(uint64(i), int64(i)*100))
	}
	if cache.Len() != 5 {
		t.Fatalf("expected 5, got %d", cache.Len())
	}
}

func TestAccountCache_Clear(t *testing.T) {
	cache := NewAccountCache(10)
	for i := byte(0); i < 5; i++ {
		cache.Put(testAddress(i), testAccount(uint64(i), int64(i)*100))
	}
	// Generate some stats.
	cache.Get(testAddress(0))
	cache.Get(testAddress(99))

	cache.Clear()
	if cache.Len() != 0 {
		t.Fatalf("expected 0 after clear, got %d", cache.Len())
	}
	stats := cache.Stats()
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Fatalf("expected stats reset, got hits=%d misses=%d", stats.Hits, stats.Misses)
	}
}

func TestAccountCache_LRUEviction(t *testing.T) {
	cache := NewAccountCache(3)
	addr1 := testAddress(1)
	addr2 := testAddress(2)
	addr3 := testAddress(3)
	addr4 := testAddress(4)

	cache.Put(addr1, testAccount(1, 100))
	cache.Put(addr2, testAccount(2, 200))
	cache.Put(addr3, testAccount(3, 300))

	// Cache is full. addr1 is the LRU entry.
	// Adding addr4 should evict addr1.
	cache.Put(addr4, testAccount(4, 400))

	if cache.Len() != 3 {
		t.Fatalf("expected len 3, got %d", cache.Len())
	}
	if _, ok := cache.Get(addr1); ok {
		t.Fatal("expected addr1 to be evicted")
	}
	if _, ok := cache.Get(addr2); !ok {
		t.Fatal("expected addr2 to still be in cache")
	}
}

func TestAccountCache_LRUAccessPromotes(t *testing.T) {
	cache := NewAccountCache(3)
	addr1 := testAddress(1)
	addr2 := testAddress(2)
	addr3 := testAddress(3)
	addr4 := testAddress(4)

	cache.Put(addr1, testAccount(1, 100))
	cache.Put(addr2, testAccount(2, 200))
	cache.Put(addr3, testAccount(3, 300))

	// Access addr1 to promote it (make it MRU).
	cache.Get(addr1)

	// Now addr2 is LRU. Adding addr4 should evict addr2.
	cache.Put(addr4, testAccount(4, 400))

	if _, ok := cache.Get(addr2); ok {
		t.Fatal("expected addr2 to be evicted after addr1 was promoted")
	}
	if _, ok := cache.Get(addr1); !ok {
		t.Fatal("expected addr1 to remain after promotion")
	}
}

func TestAccountCache_UpdateExisting(t *testing.T) {
	cache := NewAccountCache(10)
	addr := testAddress(1)

	cache.Put(addr, testAccount(1, 100))
	cache.Put(addr, testAccount(2, 200))

	if cache.Len() != 1 {
		t.Fatalf("expected len 1 after update, got %d", cache.Len())
	}
	got, ok := cache.Get(addr)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Nonce != 2 {
		t.Errorf("nonce: got %d, want 2", got.Nonce)
	}
	if got.Balance.Int64() != 200 {
		t.Errorf("balance: got %d, want 200", got.Balance.Int64())
	}
}

func TestAccountCache_DeepCopy(t *testing.T) {
	cache := NewAccountCache(10)
	addr := testAddress(1)
	original := testAccount(1, 1000)

	cache.Put(addr, original)

	// Mutate the original after putting it in the cache.
	original.Nonce = 999
	original.Balance.SetInt64(0)

	got, ok := cache.Get(addr)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Nonce != 1 {
		t.Errorf("cached nonce was mutated: got %d, want 1", got.Nonce)
	}
	if got.Balance.Int64() != 1000 {
		t.Errorf("cached balance was mutated: got %d, want 1000", got.Balance.Int64())
	}

	// Mutate the returned value and verify cache is unaffected.
	got.Nonce = 888
	got.Balance.SetInt64(0)

	got2, _ := cache.Get(addr)
	if got2.Nonce != 1 {
		t.Errorf("cache affected by returned value mutation: got %d, want 1", got2.Nonce)
	}
	if got2.Balance.Int64() != 1000 {
		t.Errorf("cache balance affected by mutation: got %d, want 1000", got2.Balance.Int64())
	}
}

func TestAccountCache_Stats(t *testing.T) {
	cache := NewAccountCache(10)
	addr := testAddress(1)
	cache.Put(addr, testAccount(1, 100))

	// 3 hits.
	cache.Get(addr)
	cache.Get(addr)
	cache.Get(addr)

	// 2 misses.
	cache.Get(testAddress(99))
	cache.Get(testAddress(98))

	stats := cache.Stats()
	if stats.Hits != 3 {
		t.Errorf("hits: got %d, want 3", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("misses: got %d, want 2", stats.Misses)
	}
}

func TestAccountCache_ConcurrentAccess(t *testing.T) {
	cache := NewAccountCache(100)
	var wg sync.WaitGroup

	// Run concurrent puts and gets.
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(idx byte) {
			defer wg.Done()
			addr := testAddress(idx)
			cache.Put(addr, testAccount(uint64(idx), int64(idx)*10))
		}(byte(i))
		go func(idx byte) {
			defer wg.Done()
			addr := testAddress(idx)
			cache.Get(addr)
		}(byte(i))
	}
	wg.Wait()

	// Verify no corruption: all entries should be retrievable.
	for i := byte(0); i < 50; i++ {
		acct, ok := cache.Get(testAddress(i))
		if !ok {
			t.Errorf("expected entry for address %d", i)
			continue
		}
		if acct.Nonce != uint64(i) {
			t.Errorf("addr %d: nonce got %d, want %d", i, acct.Nonce, i)
		}
	}
}

func TestAccountCache_MinMaxSize(t *testing.T) {
	// maxSize < 1 should default to 1.
	cache := NewAccountCache(0)
	cache.Put(testAddress(1), testAccount(1, 100))
	cache.Put(testAddress(2), testAccount(2, 200))

	if cache.Len() != 1 {
		t.Fatalf("expected len 1 with maxSize capped to 1, got %d", cache.Len())
	}
	// Most recent should survive.
	if _, ok := cache.Get(testAddress(2)); !ok {
		t.Fatal("expected most recent entry to survive")
	}
	if _, ok := cache.Get(testAddress(1)); ok {
		t.Fatal("expected first entry to be evicted")
	}
}

func TestAccountCache_PutPromotesOnUpdate(t *testing.T) {
	cache := NewAccountCache(3)
	addr1 := testAddress(1)
	addr2 := testAddress(2)
	addr3 := testAddress(3)
	addr4 := testAddress(4)

	cache.Put(addr1, testAccount(1, 100))
	cache.Put(addr2, testAccount(2, 200))
	cache.Put(addr3, testAccount(3, 300))

	// Update addr1 to promote it.
	cache.Put(addr1, testAccount(10, 1000))

	// Now addr2 is LRU. Adding addr4 should evict addr2.
	cache.Put(addr4, testAccount(4, 400))

	if _, ok := cache.Get(addr2); ok {
		t.Fatal("expected addr2 to be evicted")
	}
	got, ok := cache.Get(addr1)
	if !ok {
		t.Fatal("expected addr1 to remain")
	}
	if got.Nonce != 10 {
		t.Errorf("nonce: got %d, want 10", got.Nonce)
	}
}
