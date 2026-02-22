package vm

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func newTestOpGasCache() *OpGasCache {
	return NewOpGasCache(DefaultOpGasCacheConfig())
}

func testAddr(b byte) types.Address {
	var addr types.Address
	addr[19] = b
	return addr
}

func testSlot(b byte) types.Hash {
	var h types.Hash
	h[31] = b
	return h
}

func TestOpGasCache_EmptyLookup(t *testing.T) {
	c := newTestOpGasCache()
	cost, hit := c.Lookup(SLOAD, testAddr(1), testSlot(1), 100)
	if hit {
		t.Error("expected miss on empty cache")
	}
	if cost != 0 {
		t.Errorf("cost = %d, want 0 on miss", cost)
	}
}

func TestOpGasCache_StoreAndLookup(t *testing.T) {
	c := newTestOpGasCache()
	addr := testAddr(1)
	slot := testSlot(1)
	c.Store(SLOAD, addr, slot, 2100, false, 100)

	cost, hit := c.Lookup(SLOAD, addr, slot, 100)
	if !hit {
		t.Fatal("expected hit after store")
	}
	if cost != 2100 {
		t.Errorf("cost = %d, want 2100", cost)
	}
}

func TestOpGasCache_BlockTransitionInvalidates(t *testing.T) {
	c := newTestOpGasCache()
	addr := testAddr(1)
	slot := testSlot(1)
	c.Store(SLOAD, addr, slot, 2100, false, 100)

	// Lookup with different block number should miss.
	_, hit := c.Lookup(SLOAD, addr, slot, 101)
	if hit {
		t.Error("expected miss after block transition")
	}
}

func TestOpGasCache_StoreInvalidatesOnNewBlock(t *testing.T) {
	c := newTestOpGasCache()
	addr := testAddr(1)
	slot := testSlot(1)

	c.Store(SLOAD, addr, slot, 2100, false, 100)
	if c.Size() != 1 {
		t.Errorf("size = %d, want 1", c.Size())
	}

	// Store with new block should reset.
	c.Store(SLOAD, addr, slot, 100, true, 101)
	if c.BlockNumber() != 101 {
		t.Errorf("blockNum = %d, want 101", c.BlockNumber())
	}
	cost, hit := c.Lookup(SLOAD, addr, slot, 101)
	if !hit || cost != 100 {
		t.Errorf("after block reset: hit=%v cost=%d, want hit=true cost=100", hit, cost)
	}
}

func TestOpGasCache_InvalidateEntry(t *testing.T) {
	c := newTestOpGasCache()
	addr := testAddr(1)
	slot := testSlot(1)
	c.Store(SLOAD, addr, slot, 2100, false, 100)
	c.Invalidate(SLOAD, addr, slot)
	_, hit := c.Lookup(SLOAD, addr, slot, 100)
	if hit {
		t.Error("expected miss after invalidation")
	}
}

func TestOpGasCache_InvalidateSlot(t *testing.T) {
	c := newTestOpGasCache()
	addr := testAddr(1)
	slot := testSlot(1)
	c.Store(SLOAD, addr, slot, 2100, false, 100)
	c.Store(SSTORE, addr, slot, 20000, false, 100)

	c.InvalidateSlot(addr, slot)
	_, hit1 := c.Lookup(SLOAD, addr, slot, 100)
	_, hit2 := c.Lookup(SSTORE, addr, slot, 100)
	if hit1 || hit2 {
		t.Error("expected both SLOAD and SSTORE to be invalidated")
	}
}

func TestOpGasCache_InvalidateAddress(t *testing.T) {
	c := newTestOpGasCache()
	addr := testAddr(1)
	c.Store(SLOAD, addr, testSlot(1), 2100, false, 100)
	c.Store(SLOAD, addr, testSlot(2), 100, true, 100)
	c.Store(SLOAD, testAddr(2), testSlot(1), 2100, false, 100)

	c.InvalidateAddress(addr)

	_, hit1 := c.Lookup(SLOAD, addr, testSlot(1), 100)
	_, hit2 := c.Lookup(SLOAD, addr, testSlot(2), 100)
	_, hit3 := c.Lookup(SLOAD, testAddr(2), testSlot(1), 100)

	if hit1 || hit2 {
		t.Error("expected entries for addr to be invalidated")
	}
	if !hit3 {
		t.Error("expected entry for different addr to remain")
	}
}

func TestOpGasCache_Reset(t *testing.T) {
	c := newTestOpGasCache()
	c.Store(SLOAD, testAddr(1), testSlot(1), 2100, false, 100)
	c.Reset()
	if c.Size() != 0 {
		t.Errorf("size after reset = %d, want 0", c.Size())
	}
	if c.BlockNumber() != 0 {
		t.Errorf("blockNum after reset = %d, want 0", c.BlockNumber())
	}
}

func TestOpGasCache_Eviction(t *testing.T) {
	cfg := OpGasCacheConfig{MaxEntries: 3, EnableSpec: true}
	c := NewOpGasCache(cfg)

	c.Store(SLOAD, testAddr(1), testSlot(1), 100, true, 100)
	c.Store(SLOAD, testAddr(2), testSlot(2), 200, true, 100)
	c.Store(SLOAD, testAddr(3), testSlot(3), 300, true, 100)

	// This should trigger eviction.
	c.Store(SLOAD, testAddr(4), testSlot(4), 400, true, 100)

	if c.Size() != 3 {
		t.Errorf("size after eviction = %d, want 3", c.Size())
	}
	// The new entry should be present.
	cost, hit := c.Lookup(SLOAD, testAddr(4), testSlot(4), 100)
	if !hit || cost != 400 {
		t.Errorf("new entry: hit=%v cost=%d, want hit=true cost=400", hit, cost)
	}
}

func TestOpGasCache_Stats(t *testing.T) {
	c := newTestOpGasCache()
	addr := testAddr(1)
	slot := testSlot(1)

	// Miss.
	c.Lookup(SLOAD, addr, slot, 100)
	// Store + hit.
	c.Store(SLOAD, addr, slot, 2100, false, 100)
	c.Lookup(SLOAD, addr, slot, 100)

	snap := c.Stats().Snapshot()
	if snap.Hits != 1 {
		t.Errorf("hits = %d, want 1", snap.Hits)
	}
	if snap.Misses != 1 {
		t.Errorf("misses = %d, want 1", snap.Misses)
	}
	if snap.Inserts != 1 {
		t.Errorf("inserts = %d, want 1", snap.Inserts)
	}
}

func TestOpGasCache_HitRate(t *testing.T) {
	c := newTestOpGasCache()
	addr := testAddr(1)
	slot := testSlot(1)

	c.Store(SLOAD, addr, slot, 2100, false, 100)
	// 1 hit + 1 miss = 50%.
	c.Lookup(SLOAD, addr, slot, 100) // hit
	c.Lookup(SLOAD, addr, testSlot(2), 100) // miss

	rate := c.Stats().HitRate()
	if rate < 0.49 || rate > 0.51 {
		t.Errorf("hitRate = %f, want ~0.5", rate)
	}
}

func TestOpGasCache_HitRateEmpty(t *testing.T) {
	c := newTestOpGasCache()
	if c.Stats().HitRate() != 0.0 {
		t.Errorf("hitRate(empty) = %f, want 0.0", c.Stats().HitRate())
	}
}

func TestOpGasCache_Entries(t *testing.T) {
	c := newTestOpGasCache()
	c.Store(SLOAD, testAddr(1), testSlot(1), 2100, false, 100)
	c.Store(BALANCE, testAddr(2), types.Hash{}, 2600, false, 100)
	entries := c.Entries()
	if len(entries) != 2 {
		t.Errorf("entries = %d, want 2", len(entries))
	}
}

func TestOpGasCache_StatsSnapshot(t *testing.T) {
	c := newTestOpGasCache()
	c.Store(SLOAD, testAddr(1), testSlot(1), 2100, false, 100)
	c.Lookup(SLOAD, testAddr(1), testSlot(1), 100)
	snap := c.Stats().Snapshot()
	s := snap.String()
	if len(s) == 0 {
		t.Error("stats string should not be empty")
	}
}

func TestOpGasCache_ConcurrentAccess(t *testing.T) {
	c := newTestOpGasCache()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			addr := testAddr(byte(n % 10))
			slot := testSlot(byte(n))
			c.Store(SLOAD, addr, slot, uint64(100+n), true, 100)
		}(i)
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			addr := testAddr(byte(n % 10))
			slot := testSlot(byte(n))
			c.Lookup(SLOAD, addr, slot, 100)
		}(i)
	}
	wg.Wait()

	if c.Size() == 0 {
		t.Error("size should be > 0 after concurrent writes")
	}
}

func TestOpGasCache_DifferentOpsNotConfused(t *testing.T) {
	c := newTestOpGasCache()
	addr := testAddr(1)
	slot := testSlot(1)

	c.Store(SLOAD, addr, slot, 2100, false, 100)
	c.Store(SSTORE, addr, slot, 20000, false, 100)

	cost1, hit1 := c.Lookup(SLOAD, addr, slot, 100)
	cost2, hit2 := c.Lookup(SSTORE, addr, slot, 100)

	if !hit1 || cost1 != 2100 {
		t.Errorf("SLOAD: hit=%v cost=%d, want hit=true cost=2100", hit1, cost1)
	}
	if !hit2 || cost2 != 20000 {
		t.Errorf("SSTORE: hit=%v cost=%d, want hit=true cost=20000", hit2, cost2)
	}
}

// --- GasBudgetTracker tests ---

func TestGasBudgetTracker_UnlimitedBudget(t *testing.T) {
	tracker := NewGasBudgetTracker(GasBudgetUnlimited, nil)
	if tracker.Budget() != 0 {
		t.Errorf("budget = %d, want 0 (unlimited)", tracker.Budget())
	}
	ok := tracker.Consume(1000000)
	if !ok {
		t.Error("consume should always succeed with unlimited budget")
	}
	if tracker.WouldExceedBudget(1000000) {
		t.Error("should never exceed with unlimited budget")
	}
}

func TestGasBudgetTracker_ConsumeWithinBudget(t *testing.T) {
	tracker := NewGasBudgetTracker(10000, nil)
	ok := tracker.Consume(5000)
	if !ok {
		t.Error("consume within budget should succeed")
	}
	if tracker.Consumed() != 5000 {
		t.Errorf("consumed = %d, want 5000", tracker.Consumed())
	}
	if tracker.Remaining() != 5000 {
		t.Errorf("remaining = %d, want 5000", tracker.Remaining())
	}
}

func TestGasBudgetTracker_ConsumeExceedsBudget(t *testing.T) {
	tracker := NewGasBudgetTracker(10000, nil)
	tracker.Consume(8000)
	ok := tracker.Consume(5000)
	if ok {
		t.Error("consume exceeding budget should fail")
	}
	if !tracker.IsAborted() {
		t.Error("should be aborted after exceeding budget")
	}
}

func TestGasBudgetTracker_WouldExceedBudget(t *testing.T) {
	tracker := NewGasBudgetTracker(10000, nil)
	tracker.Consume(8000)
	if !tracker.WouldExceedBudget(5000) {
		t.Error("8000+5000 should exceed budget of 10000")
	}
	if tracker.WouldExceedBudget(1000) {
		t.Error("8000+1000 should not exceed budget of 10000")
	}
}

func TestGasBudgetTracker_Estimate(t *testing.T) {
	tracker := NewGasBudgetTracker(10000, nil)
	tracker.Estimate(3000)
	tracker.Estimate(2000)
	if tracker.Estimated() != 5000 {
		t.Errorf("estimated = %d, want 5000", tracker.Estimated())
	}
	// Estimated should not affect consumed.
	if tracker.Consumed() != 0 {
		t.Errorf("consumed = %d, want 0", tracker.Consumed())
	}
}

func TestGasBudgetTracker_Utilization(t *testing.T) {
	tracker := NewGasBudgetTracker(10000, nil)
	tracker.Consume(2500)
	util := tracker.Utilization()
	if util < 0.24 || util > 0.26 {
		t.Errorf("utilization = %f, want ~0.25", util)
	}
}

func TestGasBudgetTracker_UtilizationUnlimited(t *testing.T) {
	tracker := NewGasBudgetTracker(GasBudgetUnlimited, nil)
	if tracker.Utilization() != 0.0 {
		t.Errorf("utilization(unlimited) = %f, want 0.0", tracker.Utilization())
	}
}

func TestGasBudgetTracker_Reset(t *testing.T) {
	tracker := NewGasBudgetTracker(10000, nil)
	tracker.Consume(8000)
	tracker.Estimate(5000)
	tracker.Reset(20000)

	if tracker.Budget() != 20000 {
		t.Errorf("budget after reset = %d, want 20000", tracker.Budget())
	}
	if tracker.Consumed() != 0 {
		t.Errorf("consumed after reset = %d, want 0", tracker.Consumed())
	}
	if tracker.Estimated() != 0 {
		t.Errorf("estimated after reset = %d, want 0", tracker.Estimated())
	}
	if tracker.IsAborted() {
		t.Error("should not be aborted after reset")
	}
}

func TestGasBudgetTracker_AbortedConsumeFails(t *testing.T) {
	tracker := NewGasBudgetTracker(100, nil)
	tracker.Consume(200) // exceeds budget, sets aborted
	ok := tracker.Consume(1)
	if ok {
		t.Error("consume after abort should fail")
	}
}

func TestGasBudgetTracker_LookupAndConsume(t *testing.T) {
	cache := newTestOpGasCache()
	addr := testAddr(1)
	slot := testSlot(1)
	cache.Store(SLOAD, addr, slot, 2100, false, 100)

	tracker := NewGasBudgetTracker(10000, cache)
	cost, hit, ok := tracker.LookupAndConsume(SLOAD, addr, slot, 100)
	if !hit {
		t.Error("expected cache hit")
	}
	if cost != 2100 {
		t.Errorf("cost = %d, want 2100", cost)
	}
	if !ok {
		t.Error("consume should succeed within budget")
	}
	if tracker.Consumed() != 2100 {
		t.Errorf("consumed = %d, want 2100", tracker.Consumed())
	}
}

func TestGasBudgetTracker_LookupAndConsumeMiss(t *testing.T) {
	cache := newTestOpGasCache()
	tracker := NewGasBudgetTracker(10000, cache)
	cost, hit, ok := tracker.LookupAndConsume(SLOAD, testAddr(1), testSlot(1), 100)
	if hit {
		t.Error("expected cache miss")
	}
	if cost != 0 {
		t.Errorf("cost on miss = %d, want 0", cost)
	}
	if !ok {
		t.Error("ok should be true on miss")
	}
}

func TestGasBudgetTracker_LookupAndConsumeNoCache(t *testing.T) {
	tracker := NewGasBudgetTracker(10000, nil)
	cost, hit, ok := tracker.LookupAndConsume(SLOAD, testAddr(1), testSlot(1), 100)
	if hit {
		t.Error("expected no hit with nil cache")
	}
	if cost != 0 {
		t.Errorf("cost = %d, want 0", cost)
	}
	if !ok {
		t.Error("ok should be true with nil cache")
	}
}

func TestGasBudgetTracker_RemainingExhausted(t *testing.T) {
	tracker := NewGasBudgetTracker(100, nil)
	tracker.Consume(200)
	if tracker.Remaining() != 0 {
		t.Errorf("remaining = %d, want 0 when exhausted", tracker.Remaining())
	}
}

func TestOpGasCache_DefaultConfig(t *testing.T) {
	cfg := DefaultOpGasCacheConfig()
	if cfg.MaxEntries != OpGasCacheDefaultSize {
		t.Errorf("MaxEntries = %d, want %d", cfg.MaxEntries, OpGasCacheDefaultSize)
	}
	if !cfg.EnableSpec {
		t.Error("EnableSpec should default to true")
	}
}
