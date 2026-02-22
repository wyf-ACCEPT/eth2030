package state

import (
	"fmt"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func TestPrefetcher_PrefetchAccountScheduling(t *testing.T) {
	pf := NewPrefetcher()

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	balance := big.NewInt(42).Bytes()

	pf.PrefetchAccount(addr, func(a types.Address) (uint64, []byte, error) {
		return 5, balance, nil
	})

	pf.WaitForPrefetch()

	nonce, bal, ok := pf.GetAccount(addr)
	if !ok {
		t.Fatal("account should be prefetched")
	}
	if nonce != 5 {
		t.Fatalf("nonce: want 5, got %d", nonce)
	}
	if new(big.Int).SetBytes(bal).Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("balance: want 42, got %s", new(big.Int).SetBytes(bal))
	}
}

func TestPrefetcher_PrefetchCompletion(t *testing.T) {
	pf := NewPrefetcher()

	addr := types.HexToAddress("0x2222222222222222222222222222222222222222")

	var completed atomic.Bool
	pf.PrefetchAccount(addr, func(a types.Address) (uint64, []byte, error) {
		time.Sleep(10 * time.Millisecond)
		completed.Store(true)
		return 1, nil, nil
	})

	// Should not be completed immediately.
	// (This is best-effort; the goroutine may have already completed.)

	pf.WaitForPrefetch()

	if !completed.Load() {
		t.Fatal("prefetch should be completed after WaitForPrefetch")
	}

	_, _, ok := pf.GetAccount(addr)
	if !ok {
		t.Fatal("account should be available after wait")
	}
}

func TestPrefetcher_ConcurrentPrefetchRequests(t *testing.T) {
	pf := NewPrefetcher()

	const count = 20
	addrs := make([]types.Address, count)
	for i := range addrs {
		addrs[i] = types.HexToAddress(fmt.Sprintf("0x%040x", i+1))
	}

	for i := 0; i < count; i++ {
		a := addrs[i]
		n := uint64(i)
		pf.PrefetchAccount(a, func(_ types.Address) (uint64, []byte, error) {
			time.Sleep(time.Millisecond)
			return n, big.NewInt(int64(n * 100)).Bytes(), nil
		})
	}

	pf.WaitForPrefetch()

	for i := 0; i < count; i++ {
		nonce, _, ok := pf.GetAccount(addrs[i])
		if !ok {
			t.Fatalf("account %d should be prefetched", i)
		}
		if nonce != uint64(i) {
			t.Fatalf("account %d nonce: want %d, got %d", i, i, nonce)
		}
	}
}

func TestPrefetcher_StatsTracking(t *testing.T) {
	pf := NewPrefetcher()

	// Initial stats should be zero.
	stats := pf.Stats()
	if stats.Requests != 0 {
		t.Fatalf("initial requests: want 0, got %d", stats.Requests)
	}

	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	pf.PrefetchAccount(addr1, func(a types.Address) (uint64, []byte, error) {
		return 1, nil, nil
	})
	pf.PrefetchAccount(addr2, func(a types.Address) (uint64, []byte, error) {
		return 2, nil, nil
	})

	pf.WaitForPrefetch()

	stats = pf.Stats()
	if stats.Requests != 2 {
		t.Fatalf("requests: want 2, got %d", stats.Requests)
	}
	if stats.CompletedCount != 2 {
		t.Fatalf("completed: want 2, got %d", stats.CompletedCount)
	}

	// Now read one account to register a hit.
	pf.GetAccount(addr1)

	stats = pf.Stats()
	if stats.Hits != 1 {
		t.Fatalf("hits: want 1, got %d", stats.Hits)
	}
	if stats.HitRate < 0.4 || stats.HitRate > 0.6 {
		t.Fatalf("hit rate: want ~0.5, got %f", stats.HitRate)
	}
}

func TestPrefetcher_PrefetchStorage(t *testing.T) {
	pf := NewPrefetcher()

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	slot := types.HexToHash("0x01")
	expected := types.HexToHash("0xabcd")

	pf.PrefetchStorage(addr, slot, func(a types.Address, s types.Hash) (types.Hash, error) {
		return expected, nil
	})

	pf.WaitForPrefetch()

	val, ok := pf.GetStorage(addr, slot)
	if !ok {
		t.Fatal("storage should be prefetched")
	}
	if val != expected {
		t.Fatalf("storage value: want %s, got %s", expected.Hex(), val.Hex())
	}
}

func TestPrefetcher_StorageStatsTracking(t *testing.T) {
	pf := NewPrefetcher()

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	slot1 := types.HexToHash("0x01")
	slot2 := types.HexToHash("0x02")

	pf.PrefetchStorage(addr, slot1, func(a types.Address, s types.Hash) (types.Hash, error) {
		return types.HexToHash("0xaa"), nil
	})
	pf.PrefetchStorage(addr, slot2, func(a types.Address, s types.Hash) (types.Hash, error) {
		return types.HexToHash("0xbb"), nil
	})

	pf.WaitForPrefetch()

	stats := pf.Stats()
	if stats.Requests != 2 {
		t.Fatalf("requests: want 2, got %d", stats.Requests)
	}

	// Read one slot.
	pf.GetStorage(addr, slot1)
	stats = pf.Stats()
	if stats.Hits != 1 {
		t.Fatalf("hits: want 1, got %d", stats.Hits)
	}
}

func TestPrefetcher_CacheMiss(t *testing.T) {
	pf := NewPrefetcher()

	addr := types.HexToAddress("0x9999999999999999999999999999999999999999")

	_, _, ok := pf.GetAccount(addr)
	if ok {
		t.Fatal("cache miss should return false")
	}

	_, ok = pf.GetStorage(addr, types.HexToHash("0x01"))
	if ok {
		t.Fatal("cache miss on storage should return false")
	}
}

func TestPrefetcher_LatencyTracking(t *testing.T) {
	pf := NewPrefetcher()

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	pf.PrefetchAccount(addr, func(a types.Address) (uint64, []byte, error) {
		time.Sleep(5 * time.Millisecond)
		return 0, nil, nil
	})

	pf.WaitForPrefetch()

	stats := pf.Stats()
	if stats.AvgLatencyNs <= 0 {
		t.Fatalf("avg latency should be positive, got %d", stats.AvgLatencyNs)
	}
}
