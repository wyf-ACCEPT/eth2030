package sync

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// mockWitnessFetcher is a test double for WitnessFetcher.
type mockWitnessFetcher struct {
	witnesses map[types.Hash]*ExecutionWitness
	err       error
	calls     int
}

func newMockWitnessFetcher() *mockWitnessFetcher {
	return &mockWitnessFetcher{
		witnesses: make(map[types.Hash]*ExecutionWitness),
	}
}

func (m *mockWitnessFetcher) FetchWitness(blockRoot types.Hash) (*ExecutionWitness, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	w, ok := m.witnesses[blockRoot]
	if !ok {
		return nil, errors.New("witness not found")
	}
	return w, nil
}

func makeTestWitness(blockRoot types.Hash) *ExecutionWitness {
	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	return &ExecutionWitness{
		BlockRoot: blockRoot,
		StateRoot: types.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		Accounts: map[types.Address]*WitnessAccountData{
			addr: {
				Nonce:   10,
				Balance: big.NewInt(5000),
			},
		},
		Storage: map[types.Address]map[types.Hash]types.Hash{
			addr: {
				types.HexToHash("0x01"): types.HexToHash("0xdead"),
			},
		},
		CreatedAt: time.Now(),
	}
}

func TestBeamStateSync_ExecuteBlock(t *testing.T) {
	fetcher := newMockWitnessFetcher()
	blockRoot := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	witness := makeTestWitness(blockRoot)
	fetcher.witnesses[blockRoot] = witness

	bss := NewBeamStateSync(fetcher, DefaultBeamCacheConfig(), DefaultFallbackConfig())

	stateRoot, err := bss.ExecuteBlock(blockRoot)
	if err != nil {
		t.Fatalf("ExecuteBlock: %v", err)
	}
	if stateRoot != witness.StateRoot {
		t.Fatalf("state root mismatch: want %s, got %s", witness.StateRoot.Hex(), stateRoot.Hex())
	}

	stats := bss.Stats()
	if stats.WitnessesFetched != 1 {
		t.Fatalf("witnesses fetched: want 1, got %d", stats.WitnessesFetched)
	}
	if stats.BlocksExecuted != 1 {
		t.Fatalf("blocks executed: want 1, got %d", stats.BlocksExecuted)
	}
}

func TestBeamStateSync_ExecuteBlockFetchError(t *testing.T) {
	fetcher := newMockWitnessFetcher()
	fetcher.err = errors.New("network failure")

	bss := NewBeamStateSync(fetcher, DefaultBeamCacheConfig(), DefaultFallbackConfig())

	blockRoot := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	_, err := bss.ExecuteBlock(blockRoot)
	if !errors.Is(err, ErrBeamWitnessFetchFailed) {
		t.Fatalf("expected ErrBeamWitnessFetchFailed, got: %v", err)
	}

	stats := bss.Stats()
	if stats.WitnessErrors != 1 {
		t.Fatalf("witness errors: want 1, got %d", stats.WitnessErrors)
	}
}

func TestBeamStateSync_CacheWarmFromWitness(t *testing.T) {
	fetcher := newMockWitnessFetcher()
	blockRoot := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	witness := makeTestWitness(blockRoot)
	fetcher.witnesses[blockRoot] = witness

	bss := NewBeamStateSync(fetcher, DefaultBeamCacheConfig(), DefaultFallbackConfig())

	_, err := bss.ExecuteBlock(blockRoot)
	if err != nil {
		t.Fatalf("ExecuteBlock: %v", err)
	}

	// Account should be in cache.
	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	acct, found := bss.GetAccount(addr)
	if !found {
		t.Fatal("expected account in cache after ExecuteBlock")
	}
	if acct.Nonce != 10 {
		t.Fatalf("nonce: want 10, got %d", acct.Nonce)
	}

	// Storage should be in cache.
	slot := types.HexToHash("0x01")
	val, found := bss.GetStorage(addr, slot)
	if !found {
		t.Fatal("expected storage in cache after ExecuteBlock")
	}
	expected := types.HexToHash("0xdead")
	if val != expected {
		t.Fatalf("storage: want %s, got %s", expected.Hex(), val.Hex())
	}
}

func TestBeamStateSync_CacheMiss(t *testing.T) {
	fetcher := newMockWitnessFetcher()
	bss := NewBeamStateSync(fetcher, DefaultBeamCacheConfig(), DefaultFallbackConfig())

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	_, found := bss.GetAccount(addr)
	if found {
		t.Fatal("expected cache miss for unknown account")
	}

	_, found = bss.GetStorage(addr, types.HexToHash("0x01"))
	if found {
		t.Fatal("expected cache miss for unknown storage")
	}

	stats := bss.Stats()
	if stats.CacheMisses != 2 {
		t.Fatalf("cache misses: want 2, got %d", stats.CacheMisses)
	}
}

func TestBeamStateSync_CacheHitRate(t *testing.T) {
	fetcher := newMockWitnessFetcher()
	blockRoot := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	witness := makeTestWitness(blockRoot)
	fetcher.witnesses[blockRoot] = witness

	bss := NewBeamStateSync(fetcher, DefaultBeamCacheConfig(), DefaultFallbackConfig())

	if rate := bss.HitRate(); rate != 0 {
		t.Fatalf("initial hit rate: want 0, got %f", rate)
	}

	_, _ = bss.ExecuteBlock(blockRoot)
	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	// Hit.
	bss.GetAccount(addr)
	// Miss.
	bss.GetAccount(types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))

	rate := bss.HitRate()
	if rate < 0.49 || rate > 0.51 {
		t.Fatalf("hit rate: want ~0.5, got %f", rate)
	}
}

func TestBeamStateSync_CacheEviction(t *testing.T) {
	cfg := DefaultBeamCacheConfig()
	cfg.MaxEntries = 5
	cfg.EvictionPercent = 0.4

	fetcher := newMockWitnessFetcher()
	bss := NewBeamStateSync(fetcher, cfg, DefaultFallbackConfig())

	// Fill cache to capacity through getOrCreateEntry.
	for i := 0; i < 5; i++ {
		addr := types.Address{}
		addr[19] = byte(i)
		bss.mu.Lock()
		bss.cache[addr] = &BeamCacheEntry{
			Account:    &WitnessAccountData{Nonce: uint64(i)},
			LastAccess: time.Now().Add(time.Duration(-i) * time.Second),
		}
		bss.mu.Unlock()
	}

	if bss.CacheSize() != 5 {
		t.Fatalf("expected 5 entries before eviction, got %d", bss.CacheSize())
	}

	// Adding one more triggers eviction of the oldest entries.
	addr := types.Address{}
	addr[19] = 0xff
	bss.mu.Lock()
	bss.getOrCreateEntry(addr)
	bss.mu.Unlock()

	size := bss.CacheSize()
	// After evicting 2 entries (40% of 5) and adding 1 new, expect 4.
	if size > cfg.MaxEntries {
		t.Fatalf("cache size after eviction: want <= %d, got %d", cfg.MaxEntries, size)
	}
	// Verify that eviction actually reduced the count.
	if size >= 5+1 {
		t.Fatalf("expected eviction to reduce cache size, got %d", size)
	}
}

func TestBeamStateSync_FallbackOnConsecutiveMisses(t *testing.T) {
	fallback := DefaultFallbackConfig()
	fallback.MaxConsecutiveMisses = 3

	fetcher := newMockWitnessFetcher()
	bss := NewBeamStateSync(fetcher, DefaultBeamCacheConfig(), fallback)

	// Generate consecutive misses.
	for i := 0; i < 3; i++ {
		addr := types.Address{}
		addr[19] = byte(i)
		bss.GetAccount(addr)
	}

	if !bss.ShouldFallback() {
		t.Fatal("expected fallback to be triggered after consecutive misses")
	}

	// ExecuteBlock should return fallback error.
	blockRoot := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	_, err := bss.ExecuteBlock(blockRoot)
	if !errors.Is(err, ErrBeamFallbackTriggered) {
		t.Fatalf("expected ErrBeamFallbackTriggered, got: %v", err)
	}
}

func TestBeamStateSync_ResetFallback(t *testing.T) {
	fallback := DefaultFallbackConfig()
	fallback.MaxConsecutiveMisses = 2

	fetcher := newMockWitnessFetcher()
	bss := NewBeamStateSync(fetcher, DefaultBeamCacheConfig(), fallback)

	// Trigger fallback.
	for i := 0; i < 2; i++ {
		bss.GetAccount(types.Address{byte(i)})
	}
	if !bss.ShouldFallback() {
		t.Fatal("expected fallback")
	}

	bss.ResetFallback()
	if bss.ShouldFallback() {
		t.Fatal("expected fallback to be cleared after reset")
	}
}

func TestBeamStateSync_StatePrefill(t *testing.T) {
	fetcher := newMockWitnessFetcher()
	bss := NewBeamStateSync(fetcher, DefaultBeamCacheConfig(), DefaultFallbackConfig())

	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	keys := []types.Hash{types.HexToHash("0x01"), types.HexToHash("0x02")}

	bss.Prefill().AddFromAccessList(addr, keys)
	if bss.Prefill().PendingCount() != 1 {
		t.Fatalf("expected 1 pending task, got %d", bss.Prefill().PendingCount())
	}

	bss.Prefill().Execute()
	if bss.Prefill().PendingCount() != 0 {
		t.Fatalf("expected 0 pending tasks after execute, got %d", bss.Prefill().PendingCount())
	}

	// Cache entry should exist for prefetched address.
	if bss.CacheSize() < 1 {
		t.Fatal("expected at least 1 cache entry after prefill")
	}
}

func TestBeamStateSync_InvalidWitness(t *testing.T) {
	fetcher := newMockWitnessFetcher()
	blockRoot := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	// Empty witness (no accounts).
	fetcher.witnesses[blockRoot] = &ExecutionWitness{
		BlockRoot: blockRoot,
		Accounts:  map[types.Address]*WitnessAccountData{},
	}

	bss := NewBeamStateSync(fetcher, DefaultBeamCacheConfig(), DefaultFallbackConfig())

	_, err := bss.ExecuteBlock(blockRoot)
	if !errors.Is(err, ErrBeamWitnessInvalid) {
		t.Fatalf("expected ErrBeamWitnessInvalid, got: %v", err)
	}
}

func TestBeamStateSync_ClearCache(t *testing.T) {
	fetcher := newMockWitnessFetcher()
	bss := NewBeamStateSync(fetcher, DefaultBeamCacheConfig(), DefaultFallbackConfig())

	// Add some entries.
	bss.mu.Lock()
	bss.cache[types.Address{1}] = &BeamCacheEntry{Account: &WitnessAccountData{Nonce: 1}}
	bss.cache[types.Address{2}] = &BeamCacheEntry{Account: &WitnessAccountData{Nonce: 2}}
	bss.mu.Unlock()

	if bss.CacheSize() != 2 {
		t.Fatalf("expected 2 entries, got %d", bss.CacheSize())
	}

	bss.ClearCache()
	if bss.CacheSize() != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", bss.CacheSize())
	}
}

func TestBeamStateSync_ConsecutiveMissResetOnHit(t *testing.T) {
	fallback := DefaultFallbackConfig()
	fallback.MaxConsecutiveMisses = 10

	fetcher := newMockWitnessFetcher()
	bss := NewBeamStateSync(fetcher, DefaultBeamCacheConfig(), fallback)

	// Add an account to cache.
	addr := types.Address{0xaa}
	bss.mu.Lock()
	bss.cache[addr] = &BeamCacheEntry{
		Account:    &WitnessAccountData{Nonce: 1},
		LastAccess: time.Now(),
	}
	bss.mu.Unlock()

	// Generate some misses.
	for i := 0; i < 5; i++ {
		bss.GetAccount(types.Address{byte(i)})
	}
	if int(bss.consecutiveMisses.Load()) != 5 {
		t.Fatalf("expected 5 consecutive misses, got %d", bss.consecutiveMisses.Load())
	}

	// Hit resets counter.
	bss.GetAccount(addr)
	if bss.consecutiveMisses.Load() != 0 {
		t.Fatalf("expected 0 consecutive misses after hit, got %d", bss.consecutiveMisses.Load())
	}
}
