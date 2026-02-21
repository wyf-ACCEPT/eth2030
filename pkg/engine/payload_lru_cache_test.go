package engine

import (
	"sync"
	"testing"
)

// makeLRUPayload creates an LRUCachePayload with a unique block number and hash.
func makeLRUPayload(n uint64) *LRUCachePayload {
	var bh [32]byte
	bh[0] = byte(n)
	bh[1] = byte(n >> 8)
	return &LRUCachePayload{
		ParentHash:    [32]byte{0xaa},
		FeeRecipient:  [20]byte{0xbb},
		StateRoot:     [32]byte{0xcc},
		ReceiptsRoot:  [32]byte{0xdd},
		BlockNumber:   n,
		GasLimit:      30_000_000,
		GasUsed:       21_000,
		Timestamp:     1_700_000_000 + n,
		BaseFeePerGas: 1_000_000_000,
		BlockHash:     bh,
		Transactions:  [][]byte{{0x01, 0x02}},
	}
}

func makePID(id byte) PayloadID {
	return PayloadID{id}
}

func TestPayloadLRUCacheNew(t *testing.T) {
	c := NewPayloadLRUCache(32)
	if c.Len() != 0 {
		t.Fatalf("new cache should be empty, got %d", c.Len())
	}
	if c.maxEntries != 32 {
		t.Fatalf("expected maxEntries 32, got %d", c.maxEntries)
	}
}

func TestPayloadLRUCacheNewDefault(t *testing.T) {
	c := NewPayloadLRUCache(0)
	if c.maxEntries != DefaultLRUCacheMaxEntries {
		t.Fatalf("expected default %d, got %d", DefaultLRUCacheMaxEntries, c.maxEntries)
	}
}

func TestPayloadLRUCachePut(t *testing.T) {
	c := NewPayloadLRUCache(64)
	if err := c.Put(makePID(1), makeLRUPayload(100)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}
}

func TestPayloadLRUCachePutNil(t *testing.T) {
	c := NewPayloadLRUCache(64)
	if err := c.Put(makePID(1), nil); err == nil {
		t.Fatal("expected error for nil payload")
	}
}

func TestPayloadLRUCacheGet(t *testing.T) {
	c := NewPayloadLRUCache(64)
	pid := makePID(1)
	p := makeLRUPayload(100)
	c.Put(pid, p)

	got, ok := c.Get(pid)
	if !ok {
		t.Fatal("expected payload to be found")
	}
	if got.BlockNumber != 100 {
		t.Fatalf("expected BlockNumber 100, got %d", got.BlockNumber)
	}
	if got.GasLimit != 30_000_000 {
		t.Fatalf("expected GasLimit 30000000, got %d", got.GasLimit)
	}
}

func TestPayloadLRUCacheGetMiss(t *testing.T) {
	c := NewPayloadLRUCache(64)
	_, ok := c.Get(makePID(99))
	if ok {
		t.Fatal("expected miss for non-existent key")
	}
	stats := c.Stats()
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestPayloadLRUCacheGetByBlockHash(t *testing.T) {
	c := NewPayloadLRUCache(64)
	p := makeLRUPayload(42)
	c.Put(makePID(1), p)

	got, ok := c.GetByBlockHash(p.BlockHash)
	if !ok {
		t.Fatal("expected to find payload by block hash")
	}
	if got.BlockNumber != 42 {
		t.Fatalf("expected BlockNumber 42, got %d", got.BlockNumber)
	}
}

func TestPayloadLRUCacheGetByBlockHashMiss(t *testing.T) {
	c := NewPayloadLRUCache(64)
	_, ok := c.GetByBlockHash([32]byte{0xff, 0xff})
	if ok {
		t.Fatal("expected miss for unknown block hash")
	}
}

func TestPayloadLRUCacheEviction(t *testing.T) {
	c := NewPayloadLRUCache(3)
	c.Put(makePID(1), makeLRUPayload(1))
	c.Put(makePID(2), makeLRUPayload(2))
	c.Put(makePID(3), makeLRUPayload(3))

	// Cache is full. Adding a 4th entry should evict the oldest (PID 1).
	c.Put(makePID(4), makeLRUPayload(4))

	if c.Len() != 3 {
		t.Fatalf("expected len 3 after eviction, got %d", c.Len())
	}
	if _, ok := c.Get(makePID(1)); ok {
		t.Fatal("PID 1 should have been evicted")
	}
	if _, ok := c.Get(makePID(4)); !ok {
		t.Fatal("PID 4 should be present")
	}
	stats := c.Stats()
	if stats.Evictions != 1 {
		t.Fatalf("expected 1 eviction, got %d", stats.Evictions)
	}
}

func TestPayloadLRUCacheLRUOrder(t *testing.T) {
	c := NewPayloadLRUCache(3)
	c.Put(makePID(1), makeLRUPayload(1))
	c.Put(makePID(2), makeLRUPayload(2))
	c.Put(makePID(3), makeLRUPayload(3))

	// Access PID 1 to move it to front.
	c.Get(makePID(1))

	// Adding PID 4 should now evict PID 2 (the true LRU).
	c.Put(makePID(4), makeLRUPayload(4))

	if _, ok := c.Get(makePID(2)); ok {
		t.Fatal("PID 2 should have been evicted as LRU")
	}
	if _, ok := c.Get(makePID(1)); !ok {
		t.Fatal("PID 1 should still be present after access")
	}
}

func TestPayloadLRUCacheRemove(t *testing.T) {
	c := NewPayloadLRUCache(64)
	pid := makePID(1)
	c.Put(pid, makeLRUPayload(1))

	ok := c.Remove(pid)
	if !ok {
		t.Fatal("Remove should return true for existing entry")
	}
	if c.Len() != 0 {
		t.Fatalf("expected len 0 after remove, got %d", c.Len())
	}
	// Removing again should return false.
	if c.Remove(pid) {
		t.Fatal("second Remove should return false")
	}
}

func TestPayloadLRUCacheLen(t *testing.T) {
	c := NewPayloadLRUCache(64)
	for i := byte(0); i < 10; i++ {
		c.Put(makePID(i), makeLRUPayload(uint64(i)))
	}
	if c.Len() != 10 {
		t.Fatalf("expected len 10, got %d", c.Len())
	}
}

func TestPayloadLRUCacheClear(t *testing.T) {
	c := NewPayloadLRUCache(64)
	c.Put(makePID(1), makeLRUPayload(1))
	c.Put(makePID(2), makeLRUPayload(2))
	c.Get(makePID(1)) // increment hit counter

	c.Clear()

	if c.Len() != 0 {
		t.Fatalf("expected len 0 after clear, got %d", c.Len())
	}
	stats := c.Stats()
	if stats.Hits != 0 || stats.Misses != 0 || stats.Evictions != 0 {
		t.Fatalf("expected zero stats after clear, got %+v", stats)
	}
}

func TestPayloadLRUCacheStats(t *testing.T) {
	c := NewPayloadLRUCache(2)
	c.Put(makePID(1), makeLRUPayload(1))

	// 1 hit
	c.Get(makePID(1))
	// 2 misses
	c.Get(makePID(99))
	c.Get(makePID(98))

	// Force eviction
	c.Put(makePID(2), makeLRUPayload(2))
	c.Put(makePID(3), makeLRUPayload(3))

	stats := c.Stats()
	if stats.Hits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Fatalf("expected 2 misses, got %d", stats.Misses)
	}
	if stats.Evictions != 1 {
		t.Fatalf("expected 1 eviction, got %d", stats.Evictions)
	}
}

func TestPayloadLRUCacheConcurrentAccess(t *testing.T) {
	c := NewPayloadLRUCache(64)
	var wg sync.WaitGroup

	// Concurrent puts.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.Put(makePID(byte(n)), makeLRUPayload(uint64(n)))
		}(i)
	}

	// Concurrent gets.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.Get(makePID(byte(n)))
		}(i)
	}

	// Concurrent removes.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.Remove(makePID(byte(n)))
		}(i)
	}

	wg.Wait()

	// Ensure no panic and cache is in a consistent state.
	if c.Len() < 0 || c.Len() > 64 {
		t.Fatalf("unexpected len %d", c.Len())
	}
}

func TestPayloadLRUCacheMaxEntries(t *testing.T) {
	c := NewPayloadLRUCache(5)
	for i := byte(0); i < 20; i++ {
		c.Put(makePID(i), makeLRUPayload(uint64(i)))
	}
	if c.Len() != 5 {
		t.Fatalf("expected max 5 entries, got %d", c.Len())
	}
	// The last 5 entries (15-19) should be present.
	for i := byte(15); i < 20; i++ {
		if _, ok := c.Get(makePID(i)); !ok {
			t.Fatalf("PID %d should be present", i)
		}
	}
}

func TestPayloadLRUCacheDuplicatePut(t *testing.T) {
	c := NewPayloadLRUCache(64)
	pid := makePID(1)
	p1 := makeLRUPayload(100)
	c.Put(pid, p1)

	// Update with new payload.
	p2 := makeLRUPayload(200)
	c.Put(pid, p2)

	if c.Len() != 1 {
		t.Fatalf("expected len 1 after duplicate put, got %d", c.Len())
	}
	got, ok := c.Get(pid)
	if !ok {
		t.Fatal("expected entry after duplicate put")
	}
	if got.BlockNumber != 200 {
		t.Fatalf("expected updated BlockNumber 200, got %d", got.BlockNumber)
	}
	// Old block hash should no longer be indexed.
	if _, ok := c.GetByBlockHash(p1.BlockHash); ok {
		t.Fatal("old block hash should not be indexed after update")
	}
}

func TestPayloadLRUCacheZeroEntries(t *testing.T) {
	// maxEntries=0 should use the default.
	c := NewPayloadLRUCache(0)
	if c.maxEntries != DefaultLRUCacheMaxEntries {
		t.Fatalf("expected default max entries, got %d", c.maxEntries)
	}
	// Should still work normally.
	c.Put(makePID(1), makeLRUPayload(1))
	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}
}

func TestPayloadLRUCacheLargePayload(t *testing.T) {
	c := NewPayloadLRUCache(64)
	p := makeLRUPayload(1)
	// Add a large transaction list.
	p.Transactions = make([][]byte, 1000)
	for i := range p.Transactions {
		p.Transactions[i] = make([]byte, 1024)
		for j := range p.Transactions[i] {
			p.Transactions[i][j] = byte(i + j)
		}
	}
	if err := c.Put(makePID(1), p); err != nil {
		t.Fatalf("Put large payload failed: %v", err)
	}
	got, ok := c.Get(makePID(1))
	if !ok {
		t.Fatal("expected large payload to be found")
	}
	if len(got.Transactions) != 1000 {
		t.Fatalf("expected 1000 txs, got %d", len(got.Transactions))
	}
}

func TestPayloadLRUCacheRemoveMiddle(t *testing.T) {
	c := NewPayloadLRUCache(64)
	c.Put(makePID(1), makeLRUPayload(1))
	c.Put(makePID(2), makeLRUPayload(2))
	c.Put(makePID(3), makeLRUPayload(3))

	// Remove middle entry.
	c.Remove(makePID(2))
	if c.Len() != 2 {
		t.Fatalf("expected len 2, got %d", c.Len())
	}
	if _, ok := c.Get(makePID(1)); !ok {
		t.Fatal("PID 1 should still be present")
	}
	if _, ok := c.Get(makePID(3)); !ok {
		t.Fatal("PID 3 should still be present")
	}
}

func TestPayloadLRUCacheBlockHashIndexUpdate(t *testing.T) {
	c := NewPayloadLRUCache(64)
	p1 := makeLRUPayload(1)
	p2 := makeLRUPayload(2)

	// Two different payloads with different block hashes.
	c.Put(makePID(1), p1)
	c.Put(makePID(2), p2)

	// Both should be findable by block hash.
	if _, ok := c.GetByBlockHash(p1.BlockHash); !ok {
		t.Fatal("p1 should be findable by block hash")
	}
	if _, ok := c.GetByBlockHash(p2.BlockHash); !ok {
		t.Fatal("p2 should be findable by block hash")
	}

	// Remove p1 and verify its hash index is cleaned up.
	c.Remove(makePID(1))
	if _, ok := c.GetByBlockHash(p1.BlockHash); ok {
		t.Fatal("p1 block hash should be removed from index")
	}
}

func TestPayloadLRUCacheStatsAfterOperations(t *testing.T) {
	c := NewPayloadLRUCache(2)

	// 3 puts, which triggers 1 eviction on the 3rd.
	c.Put(makePID(1), makeLRUPayload(1))
	c.Put(makePID(2), makeLRUPayload(2))
	c.Put(makePID(3), makeLRUPayload(3))

	// 1 hit and 1 miss.
	c.Get(makePID(3))
	c.Get(makePID(99))

	stats := c.Stats()
	if stats.Hits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.Evictions != 1 {
		t.Fatalf("expected 1 eviction, got %d", stats.Evictions)
	}
}

func TestPayloadLRUCacheSingleEntry(t *testing.T) {
	c := NewPayloadLRUCache(1)
	c.Put(makePID(1), makeLRUPayload(1))
	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}

	// Adding another should evict the first.
	c.Put(makePID(2), makeLRUPayload(2))
	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}
	if _, ok := c.Get(makePID(1)); ok {
		t.Fatal("PID 1 should have been evicted")
	}
	if _, ok := c.Get(makePID(2)); !ok {
		t.Fatal("PID 2 should be present")
	}
}

func TestPayloadLRUCacheMultipleEvictions(t *testing.T) {
	c := NewPayloadLRUCache(2)
	for i := byte(0); i < 10; i++ {
		c.Put(makePID(i), makeLRUPayload(uint64(i)))
	}
	stats := c.Stats()
	if stats.Evictions != 8 {
		t.Fatalf("expected 8 evictions, got %d", stats.Evictions)
	}
	if c.Len() != 2 {
		t.Fatalf("expected len 2, got %d", c.Len())
	}
}

func TestPayloadLRUCacheGetByBlockHashAfterEviction(t *testing.T) {
	c := NewPayloadLRUCache(2)
	p1 := makeLRUPayload(1)
	p2 := makeLRUPayload(2)
	p3 := makeLRUPayload(3)

	c.Put(makePID(1), p1)
	c.Put(makePID(2), p2)
	c.Put(makePID(3), p3) // evicts PID 1

	// p1's block hash should no longer be indexed.
	if _, ok := c.GetByBlockHash(p1.BlockHash); ok {
		t.Fatal("evicted payload's block hash should not be indexed")
	}
	// p3 should be findable.
	if _, ok := c.GetByBlockHash(p3.BlockHash); !ok {
		t.Fatal("p3 should be findable by block hash")
	}
}

func TestPayloadLRUCacheClearAndReuse(t *testing.T) {
	c := NewPayloadLRUCache(64)
	for i := byte(0); i < 10; i++ {
		c.Put(makePID(i), makeLRUPayload(uint64(i)))
	}
	c.Clear()

	// Verify we can reuse after clear.
	c.Put(makePID(50), makeLRUPayload(50))
	got, ok := c.Get(makePID(50))
	if !ok || got.BlockNumber != 50 {
		t.Fatal("cache should be usable after clear")
	}
}

