package sync

import (
	"errors"
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// mockBeamFetcher is a test double for BeamStateFetcher.
type mockBeamFetcher struct {
	accounts map[types.Address]*BeamAccountData
	storage  map[types.Address]map[types.Hash]types.Hash

	fetchAccountCalls atomic.Uint64
	fetchStorageCalls atomic.Uint64
}

func newMockBeamFetcher() *mockBeamFetcher {
	return &mockBeamFetcher{
		accounts: make(map[types.Address]*BeamAccountData),
		storage:  make(map[types.Address]map[types.Hash]types.Hash),
	}
}

func (m *mockBeamFetcher) FetchAccount(addr types.Address) (*BeamAccountData, error) {
	m.fetchAccountCalls.Add(1)
	acct, ok := m.accounts[addr]
	if !ok {
		return &BeamAccountData{Balance: new(big.Int)}, nil
	}
	return acct, nil
}

func (m *mockBeamFetcher) FetchStorage(addr types.Address, key types.Hash) (types.Hash, error) {
	m.fetchStorageCalls.Add(1)
	if slots, ok := m.storage[addr]; ok {
		if val, ok := slots[key]; ok {
			return val, nil
		}
	}
	return types.Hash{}, nil
}

func TestBeamSync_FetchAccount(t *testing.T) {
	mock := newMockBeamFetcher()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	mock.accounts[addr] = &BeamAccountData{
		Nonce:   5,
		Balance: big.NewInt(1000),
	}

	bs := NewBeamSync(mock)

	acct, err := bs.FetchAccount(addr)
	if err != nil {
		t.Fatalf("FetchAccount: %v", err)
	}
	if acct.Nonce != 5 {
		t.Fatalf("nonce: want 5, got %d", acct.Nonce)
	}
	if acct.Balance.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("balance: want 1000, got %s", acct.Balance)
	}
}

func TestBeamSync_FetchAccountCaching(t *testing.T) {
	mock := newMockBeamFetcher()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	mock.accounts[addr] = &BeamAccountData{
		Nonce:   1,
		Balance: big.NewInt(100),
	}

	bs := NewBeamSync(mock)

	// First fetch goes to network.
	_, err := bs.FetchAccount(addr)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	// Second fetch should hit cache.
	_, err = bs.FetchAccount(addr)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}

	// Mock should have been called only once.
	if mock.fetchAccountCalls.Load() != 1 {
		t.Fatalf("mock calls: want 1, got %d", mock.fetchAccountCalls.Load())
	}

	stats := bs.Stats()
	if stats.CacheHits != 1 {
		t.Fatalf("cache hits: want 1, got %d", stats.CacheHits)
	}
	if stats.CacheMisses != 1 {
		t.Fatalf("cache misses: want 1, got %d", stats.CacheMisses)
	}
}

func TestBeamSync_FetchStorage(t *testing.T) {
	mock := newMockBeamFetcher()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	slot := types.HexToHash("0x01")
	val := types.HexToHash("0xbeef")

	mock.storage[addr] = map[types.Hash]types.Hash{slot: val}

	bs := NewBeamSync(mock)

	result, err := bs.FetchStorage(addr, slot)
	if err != nil {
		t.Fatalf("FetchStorage: %v", err)
	}
	if result != val {
		t.Fatalf("storage value: want %s, got %s", val.Hex(), result.Hex())
	}
}

func TestBeamSync_StoragePrefetching(t *testing.T) {
	mock := newMockBeamFetcher()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	slot1 := types.HexToHash("0x01")
	slot2 := types.HexToHash("0x02")
	val1 := types.HexToHash("0xaa")
	val2 := types.HexToHash("0xbb")

	mock.storage[addr] = map[types.Hash]types.Hash{
		slot1: val1,
		slot2: val2,
	}

	bs := NewBeamSync(mock)

	// Prefetch storage slots.
	bs.Prefetcher().PrefetchStorage(addr, []types.Hash{slot1, slot2})
	bs.Prefetcher().Wait()

	// Now reads should hit the cache.
	r1, err := bs.FetchStorage(addr, slot1)
	if err != nil {
		t.Fatalf("FetchStorage slot1: %v", err)
	}
	if r1 != val1 {
		t.Fatalf("slot1: want %s, got %s", val1.Hex(), r1.Hex())
	}

	r2, err := bs.FetchStorage(addr, slot2)
	if err != nil {
		t.Fatalf("FetchStorage slot2: %v", err)
	}
	if r2 != val2 {
		t.Fatalf("slot2: want %s, got %s", val2.Hex(), r2.Hex())
	}

	// Both should be cache hits (fetched during prefetch, then read from cache).
	stats := bs.Stats()
	if stats.CacheHits != 2 {
		t.Fatalf("cache hits: want 2, got %d", stats.CacheHits)
	}
}

func TestBeamSync_CacheHitRate(t *testing.T) {
	mock := newMockBeamFetcher()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	mock.accounts[addr] = &BeamAccountData{
		Nonce:   1,
		Balance: big.NewInt(100),
	}

	bs := NewBeamSync(mock)

	// Initial rate should be 0.
	if rate := bs.CacheHitRate(); rate != 0.0 {
		t.Fatalf("initial hit rate: want 0.0, got %f", rate)
	}

	// First fetch is a miss.
	_, _ = bs.FetchAccount(addr)

	// Second fetch is a hit.
	_, _ = bs.FetchAccount(addr)

	// 1 hit, 1 miss = 50%.
	rate := bs.CacheHitRate()
	if rate < 0.49 || rate > 0.51 {
		t.Fatalf("hit rate: want ~0.5, got %f", rate)
	}
}

func TestBeamSync_FallbackToNetworkOnCacheMiss(t *testing.T) {
	mock := newMockBeamFetcher()
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	mock.accounts[addr1] = &BeamAccountData{
		Nonce:   1,
		Balance: big.NewInt(100),
	}
	mock.accounts[addr2] = &BeamAccountData{
		Nonce:   2,
		Balance: big.NewInt(200),
	}

	bs := NewBeamSync(mock)

	// Pre-warm only addr1.
	_, _ = bs.FetchAccount(addr1)

	// addr2 should fall back to network.
	acct2, err := bs.FetchAccount(addr2)
	if err != nil {
		t.Fatalf("FetchAccount addr2: %v", err)
	}
	if acct2.Nonce != 2 {
		t.Fatalf("addr2 nonce: want 2, got %d", acct2.Nonce)
	}

	// Mock should have been called twice (once for each address).
	if mock.fetchAccountCalls.Load() != 2 {
		t.Fatalf("mock calls: want 2, got %d", mock.fetchAccountCalls.Load())
	}
}

func TestOnDemandDB_GetBalance(t *testing.T) {
	mock := newMockBeamFetcher()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	mock.accounts[addr] = &BeamAccountData{
		Nonce:   3,
		Balance: big.NewInt(42),
	}

	bs := NewBeamSync(mock)
	db := NewOnDemandDB(bs)

	bal, err := db.GetBalance(addr)
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if bal.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("balance: want 42, got %s", bal)
	}
}

func TestOnDemandDB_GetNonce(t *testing.T) {
	mock := newMockBeamFetcher()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	mock.accounts[addr] = &BeamAccountData{
		Nonce:   99,
		Balance: new(big.Int),
	}

	bs := NewBeamSync(mock)
	db := NewOnDemandDB(bs)

	nonce, err := db.GetNonce(addr)
	if err != nil {
		t.Fatalf("GetNonce: %v", err)
	}
	if nonce != 99 {
		t.Fatalf("nonce: want 99, got %d", nonce)
	}
}

func TestOnDemandDB_GetCode(t *testing.T) {
	mock := newMockBeamFetcher()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	code := []byte{0x60, 0x00, 0xf3}
	mock.accounts[addr] = &BeamAccountData{
		Balance: new(big.Int),
		Code:    code,
	}

	bs := NewBeamSync(mock)
	db := NewOnDemandDB(bs)

	result, err := db.GetCode(addr)
	if err != nil {
		t.Fatalf("GetCode: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("code length: want 3, got %d", len(result))
	}
}

func TestOnDemandDB_GetStorage(t *testing.T) {
	mock := newMockBeamFetcher()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	slot := types.HexToHash("0x01")
	val := types.HexToHash("0xcafe")
	mock.storage[addr] = map[types.Hash]types.Hash{slot: val}

	bs := NewBeamSync(mock)
	db := NewOnDemandDB(bs)

	result, err := db.GetStorage(addr, slot)
	if err != nil {
		t.Fatalf("GetStorage: %v", err)
	}
	if result != val {
		t.Fatalf("storage: want %s, got %s", val.Hex(), result.Hex())
	}
}

func TestBeamPrefetcher_PrefetchAccounts(t *testing.T) {
	mock := newMockBeamFetcher()
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	mock.accounts[addr1] = &BeamAccountData{Nonce: 1, Balance: big.NewInt(10)}
	mock.accounts[addr2] = &BeamAccountData{Nonce: 2, Balance: big.NewInt(20)}

	bs := NewBeamSync(mock)

	bs.Prefetcher().PrefetchAccounts([]types.Address{addr1, addr2})
	bs.Prefetcher().Wait()

	// Now both should be cached.
	acct1, _ := bs.FetchAccount(addr1)
	acct2, _ := bs.FetchAccount(addr2)

	if acct1.Nonce != 1 {
		t.Fatalf("addr1 nonce: want 1, got %d", acct1.Nonce)
	}
	if acct2.Nonce != 2 {
		t.Fatalf("addr2 nonce: want 2, got %d", acct2.Nonce)
	}

	// Both reads should be cache hits.
	stats := bs.Stats()
	if stats.CacheHits != 2 {
		t.Fatalf("cache hits: want 2, got %d", stats.CacheHits)
	}
}

// errorFetcher always returns an error on fetch.
type errorFetcher struct{}

func (e *errorFetcher) FetchAccount(addr types.Address) (*BeamAccountData, error) {
	return nil, errors.New("network error")
}

func (e *errorFetcher) FetchStorage(addr types.Address, key types.Hash) (types.Hash, error) {
	return types.Hash{}, errors.New("network error")
}

func TestBeamSync_FetchError(t *testing.T) {
	bs := NewBeamSync(&errorFetcher{})

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	_, err := bs.FetchAccount(addr)
	if err == nil {
		t.Fatal("expected error on fetch, got nil")
	}

	_, err = bs.FetchStorage(addr, types.HexToHash("0x01"))
	if err == nil {
		t.Fatal("expected error on storage fetch, got nil")
	}
}

func TestBeamSync_StorageCaching(t *testing.T) {
	mock := newMockBeamFetcher()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	slot := types.HexToHash("0x01")
	val := types.HexToHash("0xdead")
	mock.storage[addr] = map[types.Hash]types.Hash{slot: val}

	bs := NewBeamSync(mock)

	// First fetch goes to network.
	_, _ = bs.FetchStorage(addr, slot)
	// Second fetch should hit cache.
	_, _ = bs.FetchStorage(addr, slot)

	if mock.fetchStorageCalls.Load() != 1 {
		t.Fatalf("mock storage calls: want 1, got %d", mock.fetchStorageCalls.Load())
	}

	stats := bs.Stats()
	if stats.CacheHits != 1 {
		t.Fatalf("cache hits: want 1, got %d", stats.CacheHits)
	}
}
