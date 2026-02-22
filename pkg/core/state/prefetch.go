package state

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Prefetcher proactively loads account and storage state needed by upcoming
// blocks. It runs background goroutines that populate a local cache, so that
// when the EVM reads the state during execution the data is already resident
// in memory. This is particularly useful for beam sync and stateless
// execution where state must be fetched over the network.
type Prefetcher struct {
	mu    sync.Mutex
	cache *prefetchCache

	// Stats tracking.
	requests   atomic.Uint64
	hits       atomic.Uint64
	startTimes map[prefetchKey]time.Time

	totalLatency atomic.Int64 // nanoseconds
	completed    atomic.Uint64

	// done channel for WaitForPrefetch.
	pending sync.WaitGroup
}

// prefetchKey uniquely identifies a prefetch request (account or storage slot).
type prefetchKey struct {
	addr types.Address
	slot types.Hash // zero hash for account-level prefetches
}

// prefetchCache is a thread-safe cache of prefetched state entries.
type prefetchCache struct {
	mu       sync.RWMutex
	accounts map[types.Address]*prefetchedAccount
}

// prefetchedAccount stores pre-loaded account data and storage slots.
type prefetchedAccount struct {
	loaded  bool
	nonce   uint64
	balance []byte // big.Int bytes for safe concurrent access
	storage map[types.Hash]types.Hash
}

// PrefetchStats reports prefetcher efficiency metrics.
type PrefetchStats struct {
	Requests       uint64
	Hits           uint64
	HitRate        float64
	AvgLatencyNs   int64
	CompletedCount uint64
}

// NewPrefetcher creates a new state prefetcher.
func NewPrefetcher() *Prefetcher {
	return &Prefetcher{
		cache: &prefetchCache{
			accounts: make(map[types.Address]*prefetchedAccount),
		},
		startTimes: make(map[prefetchKey]time.Time),
	}
}

// PrefetchAccount queues an account for background loading. The fetch
// function is called in a goroutine to load the data; its result is
// stored in the prefetch cache.
func (p *Prefetcher) PrefetchAccount(addr types.Address, fetch func(types.Address) (nonce uint64, balance []byte, err error)) {
	p.requests.Add(1)

	key := prefetchKey{addr: addr}
	p.mu.Lock()
	p.startTimes[key] = time.Now()
	p.mu.Unlock()

	p.pending.Add(1)
	go func() {
		defer p.pending.Done()

		nonce, balance, err := fetch(addr)
		if err != nil {
			return
		}

		p.cache.mu.Lock()
		if _, ok := p.cache.accounts[addr]; !ok {
			p.cache.accounts[addr] = &prefetchedAccount{
				storage: make(map[types.Hash]types.Hash),
			}
		}
		acct := p.cache.accounts[addr]
		acct.loaded = true
		acct.nonce = nonce
		acct.balance = make([]byte, len(balance))
		copy(acct.balance, balance)
		p.cache.mu.Unlock()

		// Record latency.
		p.mu.Lock()
		start, ok := p.startTimes[key]
		if ok {
			delete(p.startTimes, key)
		}
		p.mu.Unlock()
		if ok {
			p.totalLatency.Add(int64(time.Since(start)))
		}
		p.completed.Add(1)
	}()
}

// PrefetchStorage queues a storage slot for background loading.
func (p *Prefetcher) PrefetchStorage(addr types.Address, slot types.Hash, fetch func(types.Address, types.Hash) (types.Hash, error)) {
	p.requests.Add(1)

	key := prefetchKey{addr: addr, slot: slot}
	p.mu.Lock()
	p.startTimes[key] = time.Now()
	p.mu.Unlock()

	p.pending.Add(1)
	go func() {
		defer p.pending.Done()

		val, err := fetch(addr, slot)
		if err != nil {
			return
		}

		p.cache.mu.Lock()
		if _, ok := p.cache.accounts[addr]; !ok {
			p.cache.accounts[addr] = &prefetchedAccount{
				storage: make(map[types.Hash]types.Hash),
			}
		}
		p.cache.accounts[addr].storage[slot] = val
		p.cache.mu.Unlock()

		p.mu.Lock()
		start, ok := p.startTimes[key]
		if ok {
			delete(p.startTimes, key)
		}
		p.mu.Unlock()
		if ok {
			p.totalLatency.Add(int64(time.Since(start)))
		}
		p.completed.Add(1)
	}()
}

// GetAccount checks the prefetch cache for a previously loaded account.
// Returns (nonce, balance, true) if the account was prefetched, or
// (0, nil, false) if not.
func (p *Prefetcher) GetAccount(addr types.Address) (uint64, []byte, bool) {
	p.cache.mu.RLock()
	defer p.cache.mu.RUnlock()

	acct, ok := p.cache.accounts[addr]
	if !ok || !acct.loaded {
		return 0, nil, false
	}
	p.hits.Add(1)
	bal := make([]byte, len(acct.balance))
	copy(bal, acct.balance)
	return acct.nonce, bal, true
}

// GetStorage checks the prefetch cache for a previously loaded storage slot.
// Returns the value and true if found, zero hash and false otherwise.
func (p *Prefetcher) GetStorage(addr types.Address, slot types.Hash) (types.Hash, bool) {
	p.cache.mu.RLock()
	defer p.cache.mu.RUnlock()

	acct, ok := p.cache.accounts[addr]
	if !ok {
		return types.Hash{}, false
	}
	val, ok := acct.storage[slot]
	if ok {
		p.hits.Add(1)
	}
	return val, ok
}

// WaitForPrefetch blocks until all pending prefetch requests have completed.
func (p *Prefetcher) WaitForPrefetch() {
	p.pending.Wait()
}

// Stats returns current prefetch statistics.
func (p *Prefetcher) Stats() PrefetchStats {
	requests := p.requests.Load()
	hits := p.hits.Load()
	completedCount := p.completed.Load()

	var hitRate float64
	if requests > 0 {
		hitRate = float64(hits) / float64(requests)
	}

	var avgLatency int64
	if completedCount > 0 {
		avgLatency = p.totalLatency.Load() / int64(completedCount)
	}

	return PrefetchStats{
		Requests:       requests,
		Hits:           hits,
		HitRate:        hitRate,
		AvgLatencyNs:   avgLatency,
		CompletedCount: completedCount,
	}
}
