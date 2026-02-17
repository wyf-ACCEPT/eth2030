package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

func makeTestState(balance int64) *state.MemoryStateDB {
	s := state.NewMemoryStateDB()
	s.AddBalance(types.Address{1}, big.NewInt(balance))
	return s
}

func TestStateCache_PutAndGet(t *testing.T) {
	sc := newStateCache()

	hash := types.Hash{0xAA}
	sdb := makeTestState(100)
	sc.put(hash, 10, sdb)

	got, ok := sc.get(hash)
	if !ok {
		t.Fatal("expected cache hit")
	}
	bal := got.GetBalance(types.Address{1})
	if bal.Int64() != 100 {
		t.Fatalf("balance mismatch: got %d, want 100", bal.Int64())
	}
}

func TestStateCache_NotFound(t *testing.T) {
	sc := newStateCache()
	_, ok := sc.get(types.Hash{0xFF})
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestStateCache_Isolation(t *testing.T) {
	sc := newStateCache()
	hash := types.Hash{0xBB}
	sdb := makeTestState(200)
	sc.put(hash, 5, sdb)

	// Modify the returned copy.
	got, _ := sc.get(hash)
	got.AddBalance(types.Address{1}, big.NewInt(999))

	// Original should be unchanged.
	got2, _ := sc.get(hash)
	bal := got2.GetBalance(types.Address{1})
	if bal.Int64() != 200 {
		t.Fatalf("cache should be isolated: got %d, want 200", bal.Int64())
	}
}

func TestStateCache_Eviction(t *testing.T) {
	sc := newStateCache()

	// Fill beyond max.
	for i := 0; i < maxCachedStates+10; i++ {
		hash := types.Hash{byte(i)}
		sc.put(hash, uint64(i), makeTestState(int64(i)))
	}

	// Should not exceed max.
	sc.mu.RLock()
	count := len(sc.snapshots)
	sc.mu.RUnlock()
	if count > maxCachedStates {
		t.Fatalf("expected at most %d cached states, got %d", maxCachedStates, count)
	}

	// Oldest entries should have been evicted.
	_, ok := sc.get(types.Hash{0})
	if ok {
		t.Fatal("expected oldest entry to be evicted")
	}

	// Newest entries should still be present.
	_, ok = sc.get(types.Hash{byte(maxCachedStates + 9)})
	if !ok {
		t.Fatal("expected newest entry to be present")
	}
}

func TestStateCache_Closest(t *testing.T) {
	sc := newStateCache()

	sc.put(types.Hash{0x10}, 0, makeTestState(0))
	sc.put(types.Hash{0x20}, 16, makeTestState(16))
	sc.put(types.Hash{0x30}, 32, makeTestState(32))
	sc.put(types.Hash{0x40}, 48, makeTestState(48))

	// Find closest to block 40.
	_, num, ok := sc.closest(40)
	if !ok {
		t.Fatal("expected match")
	}
	if num != 32 {
		t.Fatalf("closest: got %d, want 32", num)
	}

	// Find closest to block 48.
	_, num, ok = sc.closest(48)
	if !ok {
		t.Fatal("expected match")
	}
	if num != 48 {
		t.Fatalf("closest: got %d, want 48", num)
	}

	// Find closest to block 5 (should get genesis at 0).
	_, num, ok = sc.closest(5)
	if !ok {
		t.Fatal("expected match")
	}
	if num != 0 {
		t.Fatalf("closest: got %d, want 0", num)
	}
}

func TestStateCache_Remove(t *testing.T) {
	sc := newStateCache()
	hash := types.Hash{0xCC}
	sc.put(hash, 10, makeTestState(100))

	sc.remove(hash)

	_, ok := sc.get(hash)
	if ok {
		t.Fatal("expected cache miss after remove")
	}
}

func TestStateCache_Clear(t *testing.T) {
	sc := newStateCache()
	for i := 0; i < 10; i++ {
		sc.put(types.Hash{byte(i)}, uint64(i), makeTestState(int64(i)))
	}
	sc.clear()

	sc.mu.RLock()
	count := len(sc.snapshots)
	sc.mu.RUnlock()
	if count != 0 {
		t.Fatalf("expected 0 after clear, got %d", count)
	}
}

func TestShouldSnapshot(t *testing.T) {
	if !shouldSnapshot(0) {
		t.Fatal("block 0 should trigger snapshot")
	}
	if !shouldSnapshot(16) {
		t.Fatal("block 16 should trigger snapshot")
	}
	if shouldSnapshot(7) {
		t.Fatal("block 7 should not trigger snapshot")
	}
	if !shouldSnapshot(32) {
		t.Fatal("block 32 should trigger snapshot")
	}
}
