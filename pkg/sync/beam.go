package sync

import (
	"errors"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/eth2028/eth2028/core/types"
)

// Beam sync errors.
var (
	ErrBeamFetchFailed   = errors.New("beam sync: fetch failed")
	ErrBeamNoPeer        = errors.New("beam sync: no peer available")
	ErrBeamAccountNotFound = errors.New("beam sync: account not found")
)

// BeamStateFetcher is the interface for fetching state data on-demand from
// the network. Implementations wrap the p2p layer to retrieve individual
// account and storage data from peers.
type BeamStateFetcher interface {
	// FetchAccount retrieves account data (nonce, balance, code hash) for
	// the given address from the network.
	FetchAccount(addr types.Address) (*BeamAccountData, error)

	// FetchStorage retrieves a storage slot value for the given address
	// and key from the network.
	FetchStorage(addr types.Address, key types.Hash) (types.Hash, error)
}

// BeamAccountData holds account data fetched on-demand during beam sync.
type BeamAccountData struct {
	Nonce    uint64
	Balance  *big.Int
	CodeHash types.Hash
	Code     []byte
}

// BeamSync implements on-demand state fetching for block execution. When the
// EVM needs state that is not locally available, BeamSync fetches it from
// peers in real time. It wraps a local cache and a network fetcher to serve
// state reads.
type BeamSync struct {
	fetcher BeamStateFetcher

	// Local cache of fetched state.
	mu       sync.RWMutex
	accounts map[types.Address]*BeamAccountData
	storage  map[types.Address]map[types.Hash]types.Hash

	// Prefetcher for predictive loading.
	prefetcher *BeamPrefetcher

	// Stats.
	fetchCount   atomic.Uint64
	cacheHits    atomic.Uint64
	cacheMisses  atomic.Uint64
}

// NewBeamSync creates a new beam sync instance with the given network fetcher.
func NewBeamSync(fetcher BeamStateFetcher) *BeamSync {
	bs := &BeamSync{
		fetcher:  fetcher,
		accounts: make(map[types.Address]*BeamAccountData),
		storage:  make(map[types.Address]map[types.Hash]types.Hash),
	}
	bs.prefetcher = NewBeamPrefetcher(bs)
	return bs
}

// FetchAccount retrieves account data, checking the local cache first and
// falling back to the network if not cached.
func (bs *BeamSync) FetchAccount(addr types.Address) (*BeamAccountData, error) {
	// Check cache first.
	bs.mu.RLock()
	if acct, ok := bs.accounts[addr]; ok {
		bs.mu.RUnlock()
		bs.cacheHits.Add(1)
		return acct, nil
	}
	bs.mu.RUnlock()

	// Cache miss: fetch from network.
	bs.cacheMisses.Add(1)
	bs.fetchCount.Add(1)

	acct, err := bs.fetcher.FetchAccount(addr)
	if err != nil {
		return nil, err
	}

	// Store in cache.
	bs.mu.Lock()
	bs.accounts[addr] = acct
	bs.mu.Unlock()

	return acct, nil
}

// FetchStorage retrieves a storage slot, checking the local cache first and
// falling back to the network if not cached.
func (bs *BeamSync) FetchStorage(addr types.Address, key types.Hash) (types.Hash, error) {
	// Check cache first.
	bs.mu.RLock()
	if slots, ok := bs.storage[addr]; ok {
		if val, ok := slots[key]; ok {
			bs.mu.RUnlock()
			bs.cacheHits.Add(1)
			return val, nil
		}
	}
	bs.mu.RUnlock()

	// Cache miss: fetch from network.
	bs.cacheMisses.Add(1)
	bs.fetchCount.Add(1)

	val, err := bs.fetcher.FetchStorage(addr, key)
	if err != nil {
		return types.Hash{}, err
	}

	// Store in cache.
	bs.mu.Lock()
	if _, ok := bs.storage[addr]; !ok {
		bs.storage[addr] = make(map[types.Hash]types.Hash)
	}
	bs.storage[addr][key] = val
	bs.mu.Unlock()

	return val, nil
}

// CacheHitRate returns the ratio of cache hits to total lookups.
// Returns 0.0 if no lookups have been performed.
func (bs *BeamSync) CacheHitRate() float64 {
	hits := bs.cacheHits.Load()
	misses := bs.cacheMisses.Load()
	total := hits + misses
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total)
}

// Stats returns beam sync statistics.
func (bs *BeamSync) Stats() BeamSyncStats {
	return BeamSyncStats{
		FetchCount:  bs.fetchCount.Load(),
		CacheHits:   bs.cacheHits.Load(),
		CacheMisses: bs.cacheMisses.Load(),
		HitRate:     bs.CacheHitRate(),
	}
}

// BeamSyncStats holds beam sync performance metrics.
type BeamSyncStats struct {
	FetchCount  uint64
	CacheHits   uint64
	CacheMisses uint64
	HitRate     float64
}

// Prefetcher returns the beam sync prefetcher.
func (bs *BeamSync) Prefetcher() *BeamPrefetcher {
	return bs.prefetcher
}

// OnDemandDB wraps a BeamSync to serve as a state source. It provides
// the same read interface as a StateDB but fetches state on-demand from
// peers when the data is not locally available. This is the core
// component that enables block execution without pre-downloaded state.
type OnDemandDB struct {
	beam *BeamSync
}

// NewOnDemandDB creates a new on-demand state database backed by beam sync.
func NewOnDemandDB(beam *BeamSync) *OnDemandDB {
	return &OnDemandDB{beam: beam}
}

// GetBalance fetches the balance for an address via beam sync.
func (db *OnDemandDB) GetBalance(addr types.Address) (*big.Int, error) {
	acct, err := db.beam.FetchAccount(addr)
	if err != nil {
		return nil, err
	}
	if acct.Balance == nil {
		return new(big.Int), nil
	}
	return new(big.Int).Set(acct.Balance), nil
}

// GetNonce fetches the nonce for an address via beam sync.
func (db *OnDemandDB) GetNonce(addr types.Address) (uint64, error) {
	acct, err := db.beam.FetchAccount(addr)
	if err != nil {
		return 0, err
	}
	return acct.Nonce, nil
}

// GetCode fetches the code for an address via beam sync.
func (db *OnDemandDB) GetCode(addr types.Address) ([]byte, error) {
	acct, err := db.beam.FetchAccount(addr)
	if err != nil {
		return nil, err
	}
	return acct.Code, nil
}

// GetStorage fetches a storage slot via beam sync.
func (db *OnDemandDB) GetStorage(addr types.Address, key types.Hash) (types.Hash, error) {
	return db.beam.FetchStorage(addr, key)
}

// BeamPrefetcher predicts and pre-fetches state nodes that will likely be
// needed during upcoming block execution. It queues fetch requests that run
// in background goroutines, warming the BeamSync cache.
type BeamPrefetcher struct {
	beam *BeamSync
	wg   sync.WaitGroup
}

// NewBeamPrefetcher creates a new prefetcher for the given beam sync.
func NewBeamPrefetcher(beam *BeamSync) *BeamPrefetcher {
	return &BeamPrefetcher{beam: beam}
}

// PrefetchAccounts queues a batch of account addresses for background
// fetching. The results are stored in the BeamSync cache.
func (p *BeamPrefetcher) PrefetchAccounts(addrs []types.Address) {
	for _, addr := range addrs {
		a := addr // capture loop variable
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			// Ignore errors; prefetch is best-effort.
			_, _ = p.beam.FetchAccount(a)
		}()
	}
}

// PrefetchStorage queues storage slots for background fetching.
func (p *BeamPrefetcher) PrefetchStorage(addr types.Address, keys []types.Hash) {
	for _, key := range keys {
		k := key // capture loop variable
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			_, _ = p.beam.FetchStorage(addr, k)
		}()
	}
}

// Wait blocks until all pending prefetch operations complete.
func (p *BeamPrefetcher) Wait() {
	p.wg.Wait()
}
