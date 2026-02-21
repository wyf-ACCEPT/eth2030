package witness

import (
	"sync"
	"testing"
)

func makeBlockHash(b byte) [32]byte {
	var h [32]byte
	h[0] = b
	return h
}

func makeStateRoot(b byte) [32]byte {
	var h [32]byte
	h[31] = b
	return h
}

func makeCachedWitness(blockNum uint64, hashByte byte, size uint64) *CachedWitness {
	return &CachedWitness{
		BlockHash:     makeBlockHash(hashByte),
		BlockNumber:   blockNum,
		StateRoot:     makeStateRoot(hashByte),
		AccountProofs: make(map[[32]byte][]byte),
		StorageProofs: make(map[[32]byte]map[[32]byte][]byte),
		CodeChunks:    make(map[[32]byte][]byte),
		Size:          size,
	}
}

func TestWitnessCache_NewCache(t *testing.T) {
	c := NewWitnessCache(64)
	if c == nil {
		t.Fatal("NewWitnessCache returned nil")
	}
	stats := c.Stats()
	if stats.Entries != 0 {
		t.Fatalf("expected 0 entries, got %d", stats.Entries)
	}
	if stats.TotalSize != 0 {
		t.Fatalf("expected 0 total size, got %d", stats.TotalSize)
	}
}

func TestWitnessCache_NewCacheDefaults(t *testing.T) {
	c := NewWitnessCache(0)
	if c.maxBlocks != 128 {
		t.Fatalf("expected default maxBlocks=128, got %d", c.maxBlocks)
	}
}

func TestWitnessCache_StoreAndGet(t *testing.T) {
	c := NewWitnessCache(64)
	bh := makeBlockHash(1)
	w := makeCachedWitness(100, 1, 5000)

	c.StoreWitness(bh, w)

	got, ok := c.GetWitness(bh)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.BlockNumber != 100 {
		t.Fatalf("expected block 100, got %d", got.BlockNumber)
	}
	if got.Size != 5000 {
		t.Fatalf("expected size 5000, got %d", got.Size)
	}
}

func TestWitnessCache_GetMiss(t *testing.T) {
	c := NewWitnessCache(64)
	bh := makeBlockHash(99)

	got, ok := c.GetWitness(bh)
	if ok {
		t.Fatal("expected cache miss")
	}
	if got != nil {
		t.Fatal("expected nil witness on miss")
	}

	stats := c.Stats()
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestWitnessCache_HasWitness(t *testing.T) {
	c := NewWitnessCache(64)
	bh := makeBlockHash(5)

	if c.HasWitness(bh) {
		t.Fatal("should not have witness before storing")
	}

	c.StoreWitness(bh, makeCachedWitness(50, 5, 1000))

	if !c.HasWitness(bh) {
		t.Fatal("should have witness after storing")
	}

	// HasWitness should not affect hit/miss stats.
	stats := c.Stats()
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Fatal("HasWitness should not affect hit/miss stats")
	}
}

func TestWitnessCache_RemoveWitness(t *testing.T) {
	c := NewWitnessCache(64)
	bh := makeBlockHash(10)
	c.StoreWitness(bh, makeCachedWitness(200, 10, 2000))

	c.RemoveWitness(bh)

	if c.HasWitness(bh) {
		t.Fatal("witness should be removed")
	}

	// Removing non-existent witness should be safe.
	c.RemoveWitness(makeBlockHash(99))
}

func TestWitnessCache_PruneBeforeBlock(t *testing.T) {
	c := NewWitnessCache(64)

	// Insert blocks at numbers 100, 200, 300, 400.
	for i, num := range []uint64{100, 200, 300, 400} {
		bh := makeBlockHash(byte(i + 1))
		c.StoreWitness(bh, makeCachedWitness(num, byte(i+1), 1000))
	}

	// Prune everything before block 250.
	pruned := c.PruneBeforeBlock(250)
	if pruned != 2 {
		t.Fatalf("expected 2 pruned, got %d", pruned)
	}

	stats := c.Stats()
	if stats.Entries != 2 {
		t.Fatalf("expected 2 remaining entries, got %d", stats.Entries)
	}

	// Blocks 100, 200 should be gone; 300, 400 remain.
	if c.HasWitness(makeBlockHash(1)) {
		t.Fatal("block 100 should be pruned")
	}
	if c.HasWitness(makeBlockHash(2)) {
		t.Fatal("block 200 should be pruned")
	}
	if !c.HasWitness(makeBlockHash(3)) {
		t.Fatal("block 300 should remain")
	}
	if !c.HasWitness(makeBlockHash(4)) {
		t.Fatal("block 400 should remain")
	}
}

func TestWitnessCache_PruneBeforeBlockNone(t *testing.T) {
	c := NewWitnessCache(64)
	c.StoreWitness(makeBlockHash(1), makeCachedWitness(100, 1, 1000))

	pruned := c.PruneBeforeBlock(50) // all are >= 50
	if pruned != 0 {
		t.Fatalf("expected 0 pruned, got %d", pruned)
	}
}

func TestWitnessCache_TotalSize(t *testing.T) {
	c := NewWitnessCache(64)

	c.StoreWitness(makeBlockHash(1), makeCachedWitness(100, 1, 3000))
	c.StoreWitness(makeBlockHash(2), makeCachedWitness(101, 2, 5000))
	c.StoreWitness(makeBlockHash(3), makeCachedWitness(102, 3, 2000))

	total := c.TotalSize()
	if total != 10000 {
		t.Fatalf("expected total size 10000, got %d", total)
	}
}

func TestWitnessCache_TotalSizeEmpty(t *testing.T) {
	c := NewWitnessCache(64)
	if c.TotalSize() != 0 {
		t.Fatal("empty cache should have 0 total size")
	}
}

func TestWitnessCache_Stats(t *testing.T) {
	c := NewWitnessCache(64)

	c.StoreWitness(makeBlockHash(1), makeCachedWitness(100, 1, 4000))
	c.StoreWitness(makeBlockHash(2), makeCachedWitness(101, 2, 6000))

	// 2 hits.
	c.GetWitness(makeBlockHash(1))
	c.GetWitness(makeBlockHash(2))
	// 1 miss.
	c.GetWitness(makeBlockHash(99))

	stats := c.Stats()
	if stats.Entries != 2 {
		t.Fatalf("expected 2 entries, got %d", stats.Entries)
	}
	if stats.TotalSize != 10000 {
		t.Fatalf("expected 10000 total size, got %d", stats.TotalSize)
	}
	if stats.Hits != 2 {
		t.Fatalf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestWitnessCache_ConcurrentAccess(t *testing.T) {
	c := NewWitnessCache(256)
	var wg sync.WaitGroup

	// 10 goroutines writing.
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				bh := makeBlockHash(byte((id*50 + i) % 256))
				w := makeCachedWitness(uint64(id*50+i), byte((id*50+i)%256), 100)
				c.StoreWitness(bh, w)
			}
		}(g)
	}

	// 10 goroutines reading.
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				bh := makeBlockHash(byte((id*50 + i) % 256))
				c.GetWitness(bh)
			}
		}(g)
	}

	wg.Wait()

	stats := c.Stats()
	if stats.Entries > 256 {
		t.Fatalf("entries should not exceed max, got %d", stats.Entries)
	}
}

func TestWitnessCache_MaxBlocks(t *testing.T) {
	c := NewWitnessCache(3)

	for i := byte(0); i < 5; i++ {
		bh := makeBlockHash(i)
		c.StoreWitness(bh, makeCachedWitness(uint64(i), i, 100))
	}

	stats := c.Stats()
	if stats.Entries != 3 {
		t.Fatalf("expected 3 entries at max capacity, got %d", stats.Entries)
	}

	// Oldest two (0, 1) should be evicted.
	if c.HasWitness(makeBlockHash(0)) {
		t.Fatal("block 0 should be evicted")
	}
	if c.HasWitness(makeBlockHash(1)) {
		t.Fatal("block 1 should be evicted")
	}
	if !c.HasWitness(makeBlockHash(2)) {
		t.Fatal("block 2 should remain")
	}
}

func TestWitnessCache_Eviction(t *testing.T) {
	c := NewWitnessCache(2)

	bh1 := makeBlockHash(1)
	bh2 := makeBlockHash(2)
	bh3 := makeBlockHash(3)

	c.StoreWitness(bh1, makeCachedWitness(100, 1, 1000))
	c.StoreWitness(bh2, makeCachedWitness(101, 2, 2000))

	// Full at 2. Adding a third should evict bh1.
	c.StoreWitness(bh3, makeCachedWitness(102, 3, 3000))

	if c.HasWitness(bh1) {
		t.Fatal("bh1 should be evicted")
	}
	if !c.HasWitness(bh2) {
		t.Fatal("bh2 should remain")
	}
	if !c.HasWitness(bh3) {
		t.Fatal("bh3 should be present")
	}
}

func TestWitnessCache_EmptyCache(t *testing.T) {
	c := NewWitnessCache(64)

	// All operations on empty cache should be safe.
	if c.HasWitness(makeBlockHash(1)) {
		t.Fatal("empty cache should not have witness")
	}
	_, ok := c.GetWitness(makeBlockHash(1))
	if ok {
		t.Fatal("empty cache should miss")
	}
	c.RemoveWitness(makeBlockHash(1))

	pruned := c.PruneBeforeBlock(100)
	if pruned != 0 {
		t.Fatalf("expected 0 pruned from empty cache, got %d", pruned)
	}

	if c.TotalSize() != 0 {
		t.Fatal("empty cache total size should be 0")
	}
}

func TestWitnessCache_LargeWitness(t *testing.T) {
	c := NewWitnessCache(64)
	bh := makeBlockHash(42)

	w := &CachedWitness{
		BlockHash:     bh,
		BlockNumber:   999999,
		StateRoot:     makeStateRoot(42),
		AccountProofs: make(map[[32]byte][]byte),
		StorageProofs: make(map[[32]byte]map[[32]byte][]byte),
		CodeChunks:    make(map[[32]byte][]byte),
		Size:          10_000_000, // 10 MB
	}

	// Fill with some account proofs.
	for i := byte(0); i < 100; i++ {
		key := makeBlockHash(i)
		w.AccountProofs[key] = make([]byte, 512)
	}

	// Fill with storage proofs.
	acctKey := makeBlockHash(1)
	w.StorageProofs[acctKey] = make(map[[32]byte][]byte)
	for i := byte(0); i < 50; i++ {
		slotKey := makeBlockHash(i)
		w.StorageProofs[acctKey][slotKey] = make([]byte, 256)
	}

	// Fill with code chunks.
	for i := byte(0); i < 20; i++ {
		codeKey := makeBlockHash(i)
		w.CodeChunks[codeKey] = make([]byte, 24576) // ~24 KB
	}

	c.StoreWitness(bh, w)

	got, ok := c.GetWitness(bh)
	if !ok {
		t.Fatal("expected cache hit for large witness")
	}
	if len(got.AccountProofs) != 100 {
		t.Fatalf("expected 100 account proofs, got %d", len(got.AccountProofs))
	}
	if len(got.StorageProofs[acctKey]) != 50 {
		t.Fatalf("expected 50 storage proofs, got %d", len(got.StorageProofs[acctKey]))
	}
	if len(got.CodeChunks) != 20 {
		t.Fatalf("expected 20 code chunks, got %d", len(got.CodeChunks))
	}
	if got.Size != 10_000_000 {
		t.Fatalf("expected size 10000000, got %d", got.Size)
	}
}

func TestWitnessCache_DuplicateStore(t *testing.T) {
	c := NewWitnessCache(64)
	bh := makeBlockHash(7)

	w1 := makeCachedWitness(100, 7, 1000)
	w2 := makeCachedWitness(100, 7, 9999)

	c.StoreWitness(bh, w1)
	c.StoreWitness(bh, w2) // overwrite

	got, ok := c.GetWitness(bh)
	if !ok {
		t.Fatal("expected hit after duplicate store")
	}
	if got.Size != 9999 {
		t.Fatalf("expected size 9999 after overwrite, got %d", got.Size)
	}

	stats := c.Stats()
	if stats.Entries != 1 {
		t.Fatalf("expected 1 entry after duplicate, got %d", stats.Entries)
	}
}

func TestWitnessCache_MultipleBlocks(t *testing.T) {
	c := NewWitnessCache(64)

	// Store 20 distinct block witnesses.
	for i := byte(0); i < 20; i++ {
		bh := makeBlockHash(i)
		c.StoreWitness(bh, makeCachedWitness(uint64(i)*10+100, i, uint64(i)*100+500))
	}

	stats := c.Stats()
	if stats.Entries != 20 {
		t.Fatalf("expected 20 entries, got %d", stats.Entries)
	}

	// Verify each block is retrievable.
	for i := byte(0); i < 20; i++ {
		bh := makeBlockHash(i)
		got, ok := c.GetWitness(bh)
		if !ok {
			t.Fatalf("expected hit for block hash byte %d", i)
		}
		expectedNum := uint64(i)*10 + 100
		if got.BlockNumber != expectedNum {
			t.Fatalf("block %d: expected number %d, got %d", i, expectedNum, got.BlockNumber)
		}
	}

	// Verify total size is the sum.
	var expectedSize uint64
	for i := byte(0); i < 20; i++ {
		expectedSize += uint64(i)*100 + 500
	}
	if c.TotalSize() != expectedSize {
		t.Fatalf("expected total size %d, got %d", expectedSize, c.TotalSize())
	}
}

func TestWitnessCache_StoreNilWitness(t *testing.T) {
	c := NewWitnessCache(64)
	bh := makeBlockHash(1)

	// Storing nil should be a no-op.
	c.StoreWitness(bh, nil)

	if c.HasWitness(bh) {
		t.Fatal("nil witness should not be stored")
	}
}
