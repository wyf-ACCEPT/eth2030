package txpool

import (
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func makePriorityEntry(hash byte, sender byte, gasPrice uint64, nonce uint64) *PriorityEntry {
	var h types.Hash
	h[0] = hash
	var s types.Address
	s[0] = sender
	return &PriorityEntry{
		TxHash: h, Sender: s, GasPrice: gasPrice,
		Nonce: nonce, Timestamp: time.Now(), Size: 100,
	}
}

func TestPriorityPool_NewPool(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 100, MinGasPrice: 1000})
	if pp == nil {
		t.Fatal("NewPriorityPool returned nil")
	}
	if pp.Size() != 0 {
		t.Fatalf("expected size 0, got %d", pp.Size())
	}
}

func TestPriorityPool_AddAndPeek(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	for _, tc := range []struct {
		h  byte
		s  byte
		gp uint64
		n  uint64
	}{{1, 1, 500, 0}, {2, 1, 1000, 1}, {3, 2, 750, 0}} {
		if added, err := pp.Add(makePriorityEntry(tc.h, tc.s, tc.gp, tc.n)); !added || err != nil {
			t.Fatalf("Add failed: added=%v err=%v", added, err)
		}
	}
	if pp.Size() != 3 {
		t.Fatalf("expected size 3, got %d", pp.Size())
	}
	top := pp.Peek()
	if top == nil || top.GasPrice != 1000 {
		t.Fatalf("expected peek gas price 1000, got %v", top)
	}
	if pp.Size() != 3 {
		t.Fatal("peek should not change size")
	}
}

func TestPriorityPool_Pop(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	pp.Add(makePriorityEntry(1, 1, 500, 0))
	pp.Add(makePriorityEntry(2, 1, 1000, 1))
	pp.Add(makePriorityEntry(3, 2, 750, 0))

	expected := []uint64{1000, 750, 500}
	for i, exp := range expected {
		top := pp.Pop()
		if top == nil || top.GasPrice != exp {
			t.Fatalf("pop %d: expected gas price %d, got %v", i, exp, top)
		}
	}
	if pp.Pop() != nil {
		t.Fatal("expected nil from empty pool")
	}
}

func TestPriorityPool_Remove(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	e1 := makePriorityEntry(1, 1, 500, 0)
	e2 := makePriorityEntry(2, 1, 1000, 1)
	pp.Add(e1)
	pp.Add(e2)

	if !pp.Remove(e2.TxHash) {
		t.Fatal("expected Remove to return true")
	}
	if pp.Size() != 1 {
		t.Fatalf("expected size 1, got %d", pp.Size())
	}
	if pp.Remove(e2.TxHash) {
		t.Fatal("expected Remove to return false for missing entry")
	}
}

func TestPriorityPool_BelowMinGasPrice(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 1000})
	added, err := pp.Add(makePriorityEntry(1, 1, 500, 0))
	if added {
		t.Fatal("should not add entry below min gas price")
	}
	if err != ErrPriorityBelowMin {
		t.Fatalf("expected ErrPriorityBelowMin, got %v", err)
	}
}

func TestPriorityPool_Duplicate(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	e := makePriorityEntry(1, 1, 500, 0)
	pp.Add(e)
	added, err := pp.Add(e)
	if added {
		t.Fatal("should not add duplicate")
	}
	if err != ErrPriorityDuplicate {
		t.Fatalf("expected ErrPriorityDuplicate, got %v", err)
	}
}

func TestPriorityPool_CapacityEviction(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 3, MinGasPrice: 100})
	e1 := makePriorityEntry(1, 1, 300, 0)
	pp.Add(e1)
	pp.Add(makePriorityEntry(2, 1, 500, 1))
	pp.Add(makePriorityEntry(3, 2, 700, 0))

	e4 := makePriorityEntry(4, 2, 900, 1)
	added, err := pp.Add(e4)
	if !added || err != nil {
		t.Fatalf("expected add with eviction, got added=%v err=%v", added, err)
	}
	if pp.Size() != 3 {
		t.Fatalf("expected size 3, got %d", pp.Size())
	}
	if pp.Contains(e1.TxHash) {
		t.Fatal("e1 should have been evicted")
	}
	if !pp.Contains(e4.TxHash) {
		t.Fatal("e4 should be in pool")
	}
}

func TestPriorityPool_CapacityRejectLow(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 2, MinGasPrice: 100})
	pp.Add(makePriorityEntry(1, 1, 500, 0))
	pp.Add(makePriorityEntry(2, 1, 700, 1))
	added, err := pp.Add(makePriorityEntry(3, 2, 200, 0))
	if added {
		t.Fatal("should reject lower-priced entry when full")
	}
	if err != ErrPriorityBelowMin {
		t.Fatalf("expected ErrPriorityBelowMin, got %v", err)
	}
}

func TestPriorityPool_GetBySender(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	s1, s2, s3 := types.Address{1}, types.Address{2}, types.Address{3}

	pp.Add(&PriorityEntry{TxHash: types.Hash{1}, Sender: s1, GasPrice: 500, Timestamp: time.Now(), Size: 100})
	pp.Add(&PriorityEntry{TxHash: types.Hash{2}, Sender: s1, GasPrice: 700, Nonce: 1, Timestamp: time.Now(), Size: 100})
	pp.Add(&PriorityEntry{TxHash: types.Hash{3}, Sender: s2, GasPrice: 600, Timestamp: time.Now(), Size: 100})

	if len(pp.GetBySender(s1)) != 2 {
		t.Fatal("expected 2 entries for s1")
	}
	if len(pp.GetBySender(s2)) != 1 {
		t.Fatal("expected 1 entry for s2")
	}
	if len(pp.GetBySender(s3)) != 0 {
		t.Fatal("expected 0 entries for s3")
	}
}

func TestPriorityPool_MinGasPriceMethod(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	if pp.MinGasPrice() != 0 {
		t.Fatal("expected 0 for empty pool")
	}
	pp.Add(makePriorityEntry(1, 1, 500, 0))
	pp.Add(makePriorityEntry(2, 1, 300, 1))
	pp.Add(makePriorityEntry(3, 2, 800, 0))
	if pp.MinGasPrice() != 300 {
		t.Fatalf("expected 300, got %d", pp.MinGasPrice())
	}
}

func TestPriorityPool_Flush(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	pp.Add(makePriorityEntry(1, 1, 500, 0))
	pp.Add(makePriorityEntry(2, 1, 1000, 1))
	pp.Add(makePriorityEntry(3, 2, 750, 0))

	entries := pp.Flush()
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	expected := []uint64{1000, 750, 500}
	for i, exp := range expected {
		if entries[i].GasPrice != exp {
			t.Fatalf("flush[%d]: expected %d, got %d", i, exp, entries[i].GasPrice)
		}
	}
	if pp.Size() != 0 {
		t.Fatal("pool should be empty after flush")
	}
}

func TestPriorityPool_Contains(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	e := makePriorityEntry(1, 1, 500, 0)
	pp.Add(e)
	if !pp.Contains(e.TxHash) {
		t.Fatal("expected true")
	}
	if pp.Contains(types.Hash{0xFF}) {
		t.Fatal("expected false for missing")
	}
}

func TestPriorityPool_PeekEmpty(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	if pp.Peek() != nil {
		t.Fatal("expected nil from empty pool")
	}
}

func TestPriorityPool_PopEmpty(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	if pp.Pop() != nil {
		t.Fatal("expected nil from empty pool")
	}
}

func TestPriorityPool_NilEntry(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	added, err := pp.Add(nil)
	if added || err == nil {
		t.Fatal("should reject nil entry")
	}
}

func TestPriorityPool_ZeroMinGasPrice(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 0})
	added, err := pp.Add(makePriorityEntry(1, 1, 0, 0))
	if !added || err != nil {
		t.Fatalf("should accept 0 gas when min is 0: added=%v err=%v", added, err)
	}
}

func TestPriorityPool_UnlimitedCapacity(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 0, MinGasPrice: 100})
	for i := byte(0); i < 50; i++ {
		if added, err := pp.Add(makePriorityEntry(i, 1, uint64(200+i), uint64(i))); !added || err != nil {
			t.Fatalf("add %d failed: added=%v err=%v", i, added, err)
		}
	}
	if pp.Size() != 50 {
		t.Fatalf("expected 50, got %d", pp.Size())
	}
}

func TestPriorityPool_RemoveAndPop(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	e1 := makePriorityEntry(1, 1, 500, 0)
	e2 := makePriorityEntry(2, 1, 1000, 1)
	pp.Add(e1)
	pp.Add(e2)
	pp.Add(makePriorityEntry(3, 2, 750, 0))

	pp.Remove(e2.TxHash)
	top := pp.Pop()
	if top == nil || top.GasPrice != 750 {
		t.Fatalf("expected 750 after removing top, got %v", top)
	}
}

func TestPriorityPool_ConcurrentAccess(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 1000, MinGasPrice: 100})
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				var h types.Hash
				h[0], h[1] = byte(gID), byte(i)
				pp.Add(&PriorityEntry{
					TxHash: h, Sender: types.Address{byte(gID)},
					GasPrice: uint64(500 + gID*10 + i), Nonce: uint64(i),
					Timestamp: time.Now(), Size: 100,
				})
			}
		}(g)
	}
	wg.Wait()
	if pp.Size() != 100 {
		t.Fatalf("expected 100, got %d", pp.Size())
	}
	// Concurrent reads.
	wg.Add(3)
	go func() { defer wg.Done(); pp.Peek() }()
	go func() { defer wg.Done(); pp.Size() }()
	go func() { defer wg.Done(); pp.MinGasPrice() }()
	wg.Wait()
}

func TestPriorityPool_FlushEmpty(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	if len(pp.Flush()) != 0 {
		t.Fatal("expected 0 from empty flush")
	}
}

func TestPriorityPool_TimestampTiebreaker(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	now := time.Now()
	e1 := &PriorityEntry{TxHash: types.Hash{1}, Sender: types.Address{1}, GasPrice: 500, Timestamp: now, Size: 100}
	e2 := &PriorityEntry{TxHash: types.Hash{2}, Sender: types.Address{2}, GasPrice: 500, Timestamp: now.Add(time.Second), Size: 100}
	pp.Add(e1)
	pp.Add(e2)
	top := pp.Pop()
	if top == nil || top.TxHash != e1.TxHash {
		t.Fatal("earlier timestamp should come first on tie")
	}
}

func TestPriorityPool_EvictionThresholdEntries(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 3, MinGasPrice: 100, EvictionThreshold: 600})
	pp.Add(makePriorityEntry(1, 1, 300, 0))
	pp.Add(makePriorityEntry(2, 1, 500, 1))
	pp.Add(makePriorityEntry(3, 2, 700, 0))
	added, err := pp.Add(makePriorityEntry(4, 2, 900, 1))
	if !added || err != nil {
		t.Fatalf("expected add with eviction: added=%v err=%v", added, err)
	}
	if pp.Size() != 3 {
		t.Fatalf("expected 3, got %d", pp.Size())
	}
}

func TestPriorityPool_SizeAfterOperations(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 10, MinGasPrice: 100})
	pp.Add(makePriorityEntry(1, 1, 500, 0))
	pp.Add(makePriorityEntry(2, 1, 700, 1))
	if pp.Size() != 2 {
		t.Fatalf("expected 2, got %d", pp.Size())
	}
	pp.Pop()
	if pp.Size() != 1 {
		t.Fatalf("expected 1, got %d", pp.Size())
	}
	pp.Remove(types.Hash{1})
	if pp.Size() != 0 {
		t.Fatalf("expected 0, got %d", pp.Size())
	}
}

func TestPriorityPool_EqualGasPriceEviction(t *testing.T) {
	pp := NewPriorityPool(PriorityPoolConfig{MaxPoolSize: 2, MinGasPrice: 100})
	pp.Add(makePriorityEntry(1, 1, 500, 0))
	pp.Add(makePriorityEntry(2, 1, 500, 1))
	added, _ := pp.Add(makePriorityEntry(3, 2, 500, 0))
	if added {
		t.Fatal("should reject equal gas price entry when full")
	}
}
