package das

import (
	"encoding/binary"
	"sync"
	"testing"
)

// makeHash creates a [32]byte hash with the given prefix value in the first 8 bytes.
func makeHash(prefix uint64) [32]byte {
	var h [32]byte
	binary.BigEndian.PutUint64(h[:8], prefix)
	return h
}

func TestNewSparseBlobPool(t *testing.T) {
	pool := NewSparseBlobPool(4)
	if pool.Sparsity() != 4 {
		t.Fatalf("sparsity = %d, want 4", pool.Sparsity())
	}
	if pool.Size() != 0 {
		t.Fatalf("size = %d, want 0", pool.Size())
	}
}

func TestNewSparseBlobPoolPanicsOnZero(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for sparsity=0")
		}
	}()
	NewSparseBlobPool(0)
}

func TestAddBlobSparsityFilter(t *testing.T) {
	pool := NewSparseBlobPool(4)

	// Hash with prefix 0 mod 4 == 0 -> should be stored.
	h0 := makeHash(0)
	if !pool.AddBlob(h0, []byte("blob0"), 1) {
		t.Fatal("expected blob with prefix 0 to be stored (0 mod 4 == 0)")
	}

	// Hash with prefix 4 mod 4 == 0 -> should be stored.
	h4 := makeHash(4)
	if !pool.AddBlob(h4, []byte("blob4"), 1) {
		t.Fatal("expected blob with prefix 4 to be stored")
	}

	// Hash with prefix 8 mod 4 == 0 -> should be stored.
	h8 := makeHash(8)
	if !pool.AddBlob(h8, []byte("blob8"), 1) {
		t.Fatal("expected blob with prefix 8 to be stored")
	}

	// Hash with prefix 1 mod 4 == 1 -> should be rejected.
	h1 := makeHash(1)
	if pool.AddBlob(h1, []byte("blob1"), 1) {
		t.Fatal("expected blob with prefix 1 to be rejected")
	}

	// Hash with prefix 3 mod 4 == 3 -> should be rejected.
	h3 := makeHash(3)
	if pool.AddBlob(h3, []byte("blob3"), 1) {
		t.Fatal("expected blob with prefix 3 to be rejected")
	}

	if pool.Size() != 3 {
		t.Fatalf("pool size = %d, want 3", pool.Size())
	}
}

func TestAddBlobSparsityOne(t *testing.T) {
	// Sparsity of 1 stores everything.
	pool := NewSparseBlobPool(1)

	for i := uint64(0); i < 10; i++ {
		h := makeHash(i)
		if !pool.AddBlob(h, []byte("data"), 1) {
			t.Fatalf("expected all blobs to be stored with sparsity=1, failed for prefix %d", i)
		}
	}

	if pool.Size() != 10 {
		t.Fatalf("pool size = %d, want 10", pool.Size())
	}
}

func TestSparseBlobPoolHasBlob(t *testing.T) {
	pool := NewSparseBlobPool(1)
	h := makeHash(42)

	if pool.HasBlob(h) {
		t.Fatal("HasBlob should return false before adding")
	}

	pool.AddBlob(h, []byte("data"), 1)

	if !pool.HasBlob(h) {
		t.Fatal("HasBlob should return true after adding")
	}
}

func TestSparseBlobPoolGetBlob(t *testing.T) {
	pool := NewSparseBlobPool(1)
	h := makeHash(42)
	data := []byte("hello world")

	_, ok := pool.GetBlob(h)
	if ok {
		t.Fatal("GetBlob should return false for missing blob")
	}

	pool.AddBlob(h, data, 1)

	got, ok := pool.GetBlob(h)
	if !ok {
		t.Fatal("GetBlob should return true for stored blob")
	}
	if string(got) != "hello world" {
		t.Fatalf("GetBlob data = %q, want %q", got, "hello world")
	}

	// Verify defensive copy - modifying returned data should not affect pool.
	got[0] = 'X'
	got2, _ := pool.GetBlob(h)
	if got2[0] != 'h' {
		t.Fatal("GetBlob should return a copy, not the original data")
	}
}

func TestAddBlobDefensiveCopy(t *testing.T) {
	pool := NewSparseBlobPool(1)
	h := makeHash(0)
	data := []byte("original")

	pool.AddBlob(h, data, 1)

	// Mutate the original slice.
	data[0] = 'X'

	got, ok := pool.GetBlob(h)
	if !ok {
		t.Fatal("expected blob to be stored")
	}
	if got[0] != 'o' {
		t.Fatal("AddBlob should make a defensive copy of the data")
	}
}

func TestAddBlobDuplicate(t *testing.T) {
	pool := NewSparseBlobPool(1)
	h := makeHash(0)

	pool.AddBlob(h, []byte("first"), 1)
	pool.AddBlob(h, []byte("second"), 1) // duplicate, should not increase count

	stats := pool.GetPoolStats()
	if stats.Stored != 1 {
		t.Fatalf("stored = %d, want 1 (duplicate should not increase count)", stats.Stored)
	}
	if stats.TotalAdded != 1 {
		t.Fatalf("totalAdded = %d, want 1", stats.TotalAdded)
	}

	// Original data should be preserved.
	got, _ := pool.GetBlob(h)
	if string(got) != "first" {
		t.Fatalf("GetBlob data = %q, want %q (first insertion should win)", got, "first")
	}
}

func TestSparseBlobPoolPruneExpired(t *testing.T) {
	pool := NewSparseBlobPool(1)

	// Add blobs at different slots.
	for i := uint64(0); i < 10; i++ {
		h := makeHash(i)
		pool.AddBlob(h, []byte("data"), i) // slot = i
	}

	if pool.Size() != 10 {
		t.Fatalf("size = %d, want 10 before pruning", pool.Size())
	}

	// Prune slots < 5 (should remove slots 0,1,2,3,4).
	pruned := pool.PruneExpired(5)
	if pruned != 5 {
		t.Fatalf("pruned = %d, want 5", pruned)
	}
	if pool.Size() != 5 {
		t.Fatalf("size = %d, want 5 after pruning", pool.Size())
	}

	// Verify remaining blobs are the ones with slots >= 5.
	for i := uint64(5); i < 10; i++ {
		h := makeHash(i)
		if !pool.HasBlob(h) {
			t.Fatalf("expected blob with slot %d to survive pruning", i)
		}
	}
	for i := uint64(0); i < 5; i++ {
		h := makeHash(i)
		if pool.HasBlob(h) {
			t.Fatalf("expected blob with slot %d to be pruned", i)
		}
	}
}

func TestSparseBlobPoolPruneNoBlobs(t *testing.T) {
	pool := NewSparseBlobPool(1)
	pruned := pool.PruneExpired(100)
	if pruned != 0 {
		t.Fatalf("pruned = %d, want 0 on empty pool", pruned)
	}
}

func TestSparseBlobPoolPruneAllBlobs(t *testing.T) {
	pool := NewSparseBlobPool(1)
	for i := uint64(0); i < 5; i++ {
		h := makeHash(i)
		pool.AddBlob(h, []byte("data"), i)
	}

	pruned := pool.PruneExpired(100) // cutoff above all slots
	if pruned != 5 {
		t.Fatalf("pruned = %d, want 5", pruned)
	}
	if pool.Size() != 0 {
		t.Fatalf("size = %d, want 0", pool.Size())
	}
}

func TestSparseBlobPoolStats(t *testing.T) {
	pool := NewSparseBlobPool(4)

	// Add blobs: prefixes 0,1,2,3,4,5,6,7
	// Stored (mod 4 == 0): 0, 4 -> 2 stored
	// Rejected: 1, 2, 3, 5, 6, 7 -> 6 rejected
	for i := uint64(0); i < 8; i++ {
		h := makeHash(i)
		pool.AddBlob(h, []byte("data"), 1)
	}

	stats := pool.GetPoolStats()
	if stats.Stored != 2 {
		t.Fatalf("stored = %d, want 2", stats.Stored)
	}
	if stats.TotalAdded != 2 {
		t.Fatalf("totalAdded = %d, want 2", stats.TotalAdded)
	}
	if stats.Rejected != 6 {
		t.Fatalf("rejected = %d, want 6", stats.Rejected)
	}
	if stats.Pruned != 0 {
		t.Fatalf("pruned = %d, want 0", stats.Pruned)
	}

	// Prune one blob.
	pool.PruneExpired(2)
	stats = pool.GetPoolStats()
	if stats.Pruned != 2 {
		t.Fatalf("pruned = %d, want 2", stats.Pruned)
	}
}

func TestSparseBlobPoolBlobSlot(t *testing.T) {
	pool := NewSparseBlobPool(1)
	h := makeHash(0)
	pool.AddBlob(h, []byte("data"), 42)

	slot, ok := pool.BlobSlot(h)
	if !ok {
		t.Fatal("BlobSlot should return true for stored blob")
	}
	if slot != 42 {
		t.Fatalf("slot = %d, want 42", slot)
	}

	_, ok = pool.BlobSlot(makeHash(999))
	if ok {
		t.Fatal("BlobSlot should return false for missing blob")
	}
}

func TestSparseBlobPoolBlobHashes(t *testing.T) {
	pool := NewSparseBlobPool(1)
	expected := make(map[[32]byte]bool)

	for i := uint64(0); i < 5; i++ {
		h := makeHash(i)
		pool.AddBlob(h, []byte("data"), 1)
		expected[h] = true
	}

	hashes := pool.BlobHashes()
	if len(hashes) != 5 {
		t.Fatalf("len(BlobHashes) = %d, want 5", len(hashes))
	}
	for _, h := range hashes {
		if !expected[h] {
			t.Fatalf("unexpected hash in BlobHashes")
		}
	}
}

func TestSparseBlobPoolReset(t *testing.T) {
	pool := NewSparseBlobPool(1)
	for i := uint64(0); i < 5; i++ {
		h := makeHash(i)
		pool.AddBlob(h, []byte("data"), 1)
	}

	pool.Reset()
	if pool.Size() != 0 {
		t.Fatalf("size = %d, want 0 after reset", pool.Size())
	}
	stats := pool.GetPoolStats()
	if stats.Stored != 0 || stats.TotalAdded != 0 || stats.Pruned != 0 || stats.Rejected != 0 {
		t.Fatalf("stats should be zeroed after reset: %+v", stats)
	}
}

func TestSparseBlobPoolMemoryUsage(t *testing.T) {
	pool := NewSparseBlobPool(1)
	pool.AddBlob(makeHash(0), make([]byte, 100), 1)
	pool.AddBlob(makeHash(1), make([]byte, 200), 1)

	usage := pool.MemoryUsage()
	if usage != 300 {
		t.Fatalf("MemoryUsage = %d, want 300", usage)
	}
}

func TestSparseBlobPoolShouldStore(t *testing.T) {
	pool := NewSparseBlobPool(4)

	// Prefix 0 mod 4 == 0 -> should store.
	if !pool.ShouldStore(makeHash(0)) {
		t.Fatal("ShouldStore(0) should be true for sparsity 4")
	}
	// Prefix 1 mod 4 == 1 -> should not store.
	if pool.ShouldStore(makeHash(1)) {
		t.Fatal("ShouldStore(1) should be false for sparsity 4")
	}
}

func TestSparseBlobPoolConcurrentAccess(t *testing.T) {
	pool := NewSparseBlobPool(1)
	var wg sync.WaitGroup

	// Concurrent writers.
	for i := uint64(0); i < 100; i++ {
		wg.Add(1)
		go func(n uint64) {
			defer wg.Done()
			h := makeHash(n)
			pool.AddBlob(h, []byte("data"), n)
		}(i)
	}

	// Concurrent readers.
	for i := uint64(0); i < 100; i++ {
		wg.Add(1)
		go func(n uint64) {
			defer wg.Done()
			h := makeHash(n)
			pool.HasBlob(h)
			pool.GetBlob(h)
		}(i)
	}

	wg.Wait()

	if pool.Size() != 100 {
		t.Fatalf("size = %d, want 100 after concurrent adds", pool.Size())
	}
}

func TestSparseBlobPoolConcurrentPrune(t *testing.T) {
	pool := NewSparseBlobPool(1)
	for i := uint64(0); i < 100; i++ {
		h := makeHash(i)
		pool.AddBlob(h, []byte("data"), i)
	}

	var wg sync.WaitGroup
	// Concurrent reads and prunes.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.PruneExpired(50)
		}()
	}
	for i := uint64(0); i < 100; i++ {
		wg.Add(1)
		go func(n uint64) {
			defer wg.Done()
			pool.HasBlob(makeHash(n))
		}(i)
	}
	wg.Wait()

	// After pruning at cutoff 50, blobs 50-99 should remain.
	if pool.Size() != 50 {
		t.Fatalf("size = %d, want 50 after concurrent prune", pool.Size())
	}
}

func TestSparsityFilterDistribution(t *testing.T) {
	// Verify that sparsity actually produces the expected fraction.
	pool := NewSparseBlobPool(8)
	stored := 0
	total := 10000

	for i := uint64(0); i < uint64(total); i++ {
		h := makeHash(i)
		if pool.AddBlob(h, []byte{0x01}, 1) {
			stored++
		}
	}

	// With sparsity 8, we expect approximately 1/8 = 12.5% stored.
	// Allow a generous margin since the distribution depends on the modulus.
	expectedMin := total/8 - total/80 // ~11.25%
	expectedMax := total/8 + total/80 // ~13.75%

	if stored < expectedMin || stored > expectedMax {
		t.Fatalf("stored %d of %d blobs (sparsity=8), expected between %d and %d",
			stored, total, expectedMin, expectedMax)
	}
}
