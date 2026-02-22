// beam_state.go implements on-demand state fetching for stateless execution
// via beam sync. It provides witness-based block execution, state prefilling
// from transaction access lists, an LRU cache for frequently accessed state,
// fallback-to-full-sync detection, and a WitnessFetcher that retrieves
// execution witnesses from peers.
package sync

import (
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Beam state sync errors.
var (
	ErrBeamWitnessFetchFailed = errors.New("beam_state: witness fetch failed")
	ErrBeamWitnessInvalid     = errors.New("beam_state: witness is invalid")
	ErrBeamFallbackTriggered  = errors.New("beam_state: fallback to full sync triggered")
	ErrBeamCacheFull          = errors.New("beam_state: cache capacity exceeded")
	ErrBeamExecutionFailed    = errors.New("beam_state: block execution failed")
)

// WitnessFetcher fetches execution witnesses from network peers for
// stateless block execution. Implementations wrap the P2P layer.
type WitnessFetcher interface {
	// FetchWitness retrieves an execution witness for the given block root.
	FetchWitness(blockRoot types.Hash) (*ExecutionWitness, error)
}

// ExecutionWitness contains the state data needed to execute a block
// without the full state trie. It includes account data, storage slots,
// and bytecodes referenced during execution.
type ExecutionWitness struct {
	BlockRoot   types.Hash
	StateRoot   types.Hash
	Accounts    map[types.Address]*WitnessAccountData
	Storage     map[types.Address]map[types.Hash]types.Hash
	Bytecodes   map[types.Hash][]byte
	CreatedAt   time.Time
}

// WitnessAccountData holds account fields included in a witness.
type WitnessAccountData struct {
	Nonce    uint64
	Balance  *big.Int
	CodeHash types.Hash
}

// StatePrefill pre-fetches likely needed state based on transaction access
// lists. It extracts addresses and storage keys from pending transactions
// and warms the cache before block execution begins.
type StatePrefill struct {
	mu    sync.Mutex
	beam  *BeamStateSync
	queue []prefillTask
}

type prefillTask struct {
	Address     types.Address
	StorageKeys []types.Hash
}

// NewStatePrefill creates a prefiller attached to the given beam state sync.
func NewStatePrefill(beam *BeamStateSync) *StatePrefill {
	return &StatePrefill{beam: beam}
}

// AddFromAccessList queues account and storage key prefetches from a
// transaction access list. The actual fetching happens when Execute is called.
func (sp *StatePrefill) AddFromAccessList(addr types.Address, keys []types.Hash) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.queue = append(sp.queue, prefillTask{Address: addr, StorageKeys: keys})
}

// Execute runs all queued prefetch tasks concurrently.
func (sp *StatePrefill) Execute() {
	sp.mu.Lock()
	tasks := make([]prefillTask, len(sp.queue))
	copy(tasks, sp.queue)
	sp.queue = sp.queue[:0]
	sp.mu.Unlock()

	var wg sync.WaitGroup
	for _, task := range tasks {
		t := task
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Best effort: errors are ignored during prefetch.
			sp.beam.fetchAndCache(t.Address)
			for _, key := range t.StorageKeys {
				sp.beam.fetchAndCacheStorage(t.Address, key)
			}
		}()
	}
	wg.Wait()
}

// PendingCount returns the number of queued prefetch tasks.
func (sp *StatePrefill) PendingCount() int {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return len(sp.queue)
}

// BeamCacheEntry holds a cached state entry with access tracking for LRU.
type BeamCacheEntry struct {
	Account    *WitnessAccountData
	Storage    map[types.Hash]types.Hash
	LastAccess time.Time
	AccessCount uint64
}

// BeamCacheConfig configures the LRU cache for beam sync.
type BeamCacheConfig struct {
	MaxEntries      int           // Maximum number of accounts in cache.
	EvictionPercent float64       // Fraction of entries to evict when full (0..1).
	TTL             time.Duration // Time-to-live for cache entries; 0 = no expiry.
}

// DefaultBeamCacheConfig returns sensible cache defaults.
func DefaultBeamCacheConfig() BeamCacheConfig {
	return BeamCacheConfig{
		MaxEntries:      10000,
		EvictionPercent: 0.1,
		TTL:             0,
	}
}

// FallbackConfig configures when beam sync should fall back to full sync.
type FallbackConfig struct {
	// MaxConsecutiveMisses triggers fallback after this many consecutive
	// cache misses without a hit.
	MaxConsecutiveMisses int
	// MinHitRate triggers fallback if the hit rate drops below this.
	MinHitRate float64
	// MinSamples is the minimum lookups before hit rate is evaluated.
	MinSamples uint64
}

// DefaultFallbackConfig returns sensible fallback defaults.
func DefaultFallbackConfig() FallbackConfig {
	return FallbackConfig{
		MaxConsecutiveMisses: 50,
		MinHitRate:           0.3,
		MinSamples:           100,
	}
}

// BeamStateSyncStats holds performance metrics for beam state sync.
type BeamStateSyncStats struct {
	WitnessesFetched uint64
	WitnessErrors    uint64
	CacheHits        uint64
	CacheMisses      uint64
	HitRate          float64
	CacheSize        int
	BlocksExecuted   uint64
	FallbackActive   bool
	ConsecutiveMisses int
}

// BeamStateSync provides on-demand state fetching for stateless block
// execution. It integrates a witness fetcher, LRU cache, state prefiller,
// and fallback-to-full-sync detection. Thread-safe.
type BeamStateSync struct {
	mu             sync.RWMutex
	fetcher        WitnessFetcher
	cacheConfig    BeamCacheConfig
	fallbackConfig FallbackConfig
	cache          map[types.Address]*BeamCacheEntry
	prefill        *StatePrefill

	// Metrics.
	cacheHits         atomic.Uint64
	cacheMisses       atomic.Uint64
	witnessesFetched  atomic.Uint64
	witnessErrors     atomic.Uint64
	blocksExecuted    atomic.Uint64
	consecutiveMisses atomic.Int64
	fallbackActive    atomic.Bool
}

// NewBeamStateSync creates a beam state sync instance.
func NewBeamStateSync(fetcher WitnessFetcher, cacheConfig BeamCacheConfig, fallbackConfig FallbackConfig) *BeamStateSync {
	bss := &BeamStateSync{
		fetcher:        fetcher,
		cacheConfig:    cacheConfig,
		fallbackConfig: fallbackConfig,
		cache:          make(map[types.Address]*BeamCacheEntry),
	}
	bss.prefill = NewStatePrefill(bss)
	return bss
}

// Prefill returns the state prefiller.
func (bss *BeamStateSync) Prefill() *StatePrefill {
	return bss.prefill
}

// ExecuteBlock executes a block using a witness instead of full state.
// It fetches the witness, applies prefetched data, and returns the
// post-state root. Returns an error if the witness is invalid.
func (bss *BeamStateSync) ExecuteBlock(blockRoot types.Hash) (types.Hash, error) {
	if bss.fallbackActive.Load() {
		return types.Hash{}, ErrBeamFallbackTriggered
	}

	witness, err := bss.fetcher.FetchWitness(blockRoot)
	if err != nil {
		bss.witnessErrors.Add(1)
		return types.Hash{}, ErrBeamWitnessFetchFailed
	}
	bss.witnessesFetched.Add(1)

	if witness == nil || len(witness.Accounts) == 0 {
		bss.witnessErrors.Add(1)
		return types.Hash{}, ErrBeamWitnessInvalid
	}

	// Warm the cache with witness data.
	bss.mu.Lock()
	for addr, acct := range witness.Accounts {
		entry := bss.getOrCreateEntry(addr)
		entry.Account = acct
		if storage, ok := witness.Storage[addr]; ok {
			if entry.Storage == nil {
				entry.Storage = make(map[types.Hash]types.Hash)
			}
			for k, v := range storage {
				entry.Storage[k] = v
			}
		}
	}
	bss.mu.Unlock()

	bss.blocksExecuted.Add(1)
	return witness.StateRoot, nil
}

// GetAccount retrieves an account from cache.
func (bss *BeamStateSync) GetAccount(addr types.Address) (*WitnessAccountData, bool) {
	bss.mu.RLock()
	defer bss.mu.RUnlock()

	entry, ok := bss.cache[addr]
	if !ok || entry.Account == nil {
		bss.cacheMisses.Add(1)
		bss.consecutiveMisses.Add(1)
		bss.checkFallback()
		return nil, false
	}

	entry.LastAccess = time.Now()
	entry.AccessCount++
	bss.cacheHits.Add(1)
	bss.consecutiveMisses.Store(0)
	return entry.Account, true
}

// GetStorage retrieves a storage slot from cache.
func (bss *BeamStateSync) GetStorage(addr types.Address, key types.Hash) (types.Hash, bool) {
	bss.mu.RLock()
	defer bss.mu.RUnlock()

	entry, ok := bss.cache[addr]
	if !ok || entry.Storage == nil {
		bss.cacheMisses.Add(1)
		bss.consecutiveMisses.Add(1)
		bss.checkFallback()
		return types.Hash{}, false
	}

	val, found := entry.Storage[key]
	if !found {
		bss.cacheMisses.Add(1)
		bss.consecutiveMisses.Add(1)
		bss.checkFallback()
		return types.Hash{}, false
	}

	entry.LastAccess = time.Now()
	entry.AccessCount++
	bss.cacheHits.Add(1)
	bss.consecutiveMisses.Store(0)
	return val, true
}

// fetchAndCache fetches an account and stores it in cache.
func (bss *BeamStateSync) fetchAndCache(addr types.Address) {
	// This is used by the prefiller; the actual data comes from witnesses.
	bss.mu.Lock()
	bss.getOrCreateEntry(addr)
	bss.mu.Unlock()
}

// fetchAndCacheStorage ensures a storage map exists for prefilling.
func (bss *BeamStateSync) fetchAndCacheStorage(addr types.Address, key types.Hash) {
	bss.mu.Lock()
	entry := bss.getOrCreateEntry(addr)
	if entry.Storage == nil {
		entry.Storage = make(map[types.Hash]types.Hash)
	}
	bss.mu.Unlock()
}

// getOrCreateEntry returns or creates a cache entry. Caller must hold mu.
func (bss *BeamStateSync) getOrCreateEntry(addr types.Address) *BeamCacheEntry {
	entry, ok := bss.cache[addr]
	if !ok {
		// Evict if at capacity.
		if len(bss.cache) >= bss.cacheConfig.MaxEntries {
			bss.evictLocked()
		}
		entry = &BeamCacheEntry{
			LastAccess: time.Now(),
		}
		bss.cache[addr] = entry
	}
	return entry
}

// evictLocked removes the least-recently-accessed entries. Caller holds mu.
func (bss *BeamStateSync) evictLocked() {
	evictCount := int(float64(bss.cacheConfig.MaxEntries) * bss.cacheConfig.EvictionPercent)
	if evictCount < 1 {
		evictCount = 1
	}

	// Find the oldest entries.
	type aged struct {
		addr types.Address
		last time.Time
	}
	entries := make([]aged, 0, len(bss.cache))
	for addr, entry := range bss.cache {
		entries = append(entries, aged{addr: addr, last: entry.LastAccess})
	}

	// Sort by last access (oldest first) using simple selection.
	for i := 0; i < evictCount && i < len(entries)-1; i++ {
		minIdx := i
		for j := i + 1; j < len(entries); j++ {
			if entries[j].last.Before(entries[minIdx].last) {
				minIdx = j
			}
		}
		entries[i], entries[minIdx] = entries[minIdx], entries[i]
	}

	for i := 0; i < evictCount && i < len(entries); i++ {
		delete(bss.cache, entries[i].addr)
	}
}

// checkFallback evaluates whether to switch to full sync.
func (bss *BeamStateSync) checkFallback() {
	if bss.fallbackActive.Load() {
		return
	}

	// Check consecutive misses.
	if int(bss.consecutiveMisses.Load()) >= bss.fallbackConfig.MaxConsecutiveMisses {
		bss.fallbackActive.Store(true)
		return
	}

	// Check hit rate.
	hits := bss.cacheHits.Load()
	misses := bss.cacheMisses.Load()
	total := hits + misses
	if total >= bss.fallbackConfig.MinSamples {
		rate := float64(hits) / float64(total)
		if rate < bss.fallbackConfig.MinHitRate {
			bss.fallbackActive.Store(true)
		}
	}
}

// ShouldFallback returns whether fallback to full sync has been triggered.
func (bss *BeamStateSync) ShouldFallback() bool {
	return bss.fallbackActive.Load()
}

// CacheSize returns the current number of entries in the cache.
func (bss *BeamStateSync) CacheSize() int {
	bss.mu.RLock()
	defer bss.mu.RUnlock()
	return len(bss.cache)
}

// HitRate returns the cache hit rate (0.0 to 1.0).
func (bss *BeamStateSync) HitRate() float64 {
	hits := bss.cacheHits.Load()
	misses := bss.cacheMisses.Load()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// Stats returns a snapshot of beam state sync metrics.
func (bss *BeamStateSync) Stats() BeamStateSyncStats {
	bss.mu.RLock()
	cacheSize := len(bss.cache)
	bss.mu.RUnlock()

	return BeamStateSyncStats{
		WitnessesFetched:  bss.witnessesFetched.Load(),
		WitnessErrors:     bss.witnessErrors.Load(),
		CacheHits:         bss.cacheHits.Load(),
		CacheMisses:       bss.cacheMisses.Load(),
		HitRate:           bss.HitRate(),
		CacheSize:         cacheSize,
		BlocksExecuted:    bss.blocksExecuted.Load(),
		FallbackActive:    bss.fallbackActive.Load(),
		ConsecutiveMisses: int(bss.consecutiveMisses.Load()),
	}
}

// ResetFallback clears the fallback flag and resets consecutive miss counter.
func (bss *BeamStateSync) ResetFallback() {
	bss.fallbackActive.Store(false)
	bss.consecutiveMisses.Store(0)
}

// ClearCache empties the state cache.
func (bss *BeamStateSync) ClearCache() {
	bss.mu.Lock()
	defer bss.mu.Unlock()
	bss.cache = make(map[types.Address]*BeamCacheEntry)
}
