// state_prefetch.go implements predictive state prefetching for parallel
// transaction execution. It analyzes transaction access patterns to predict
// which accounts and storage slots will be needed, then pre-warms them in
// background goroutines before execution begins. This reduces cold-access
// latency in the critical path of gigagas execution.
package vm

import (
	"encoding/binary"
	"sync"
	"sync/atomic"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// PrefetchRequest represents a unit of state to prefetch.
type PrefetchRequest struct {
	Address     types.Address
	StorageKeys []types.Hash
}

// PrefetchStats tracks prefetching performance.
type PrefetchStats struct {
	Requested   uint64 // number of prefetch requests issued
	Completed   uint64 // number of prefetch tasks completed
	CacheHits   uint64 // items already in cache (warm)
	CacheMisses uint64 // items loaded from cold storage
}

// stateCache is a concurrent-safe cache for pre-warmed state data.
type stateCache struct {
	mu       sync.RWMutex
	accounts map[types.Address]bool       // true if warmed
	slots    map[storageSlotKey]bool      // true if warmed
}

// storageSlotKey uniquely identifies a storage slot.
type storageSlotKey struct {
	addr types.Address
	key  types.Hash
}

func newStateCache() *stateCache {
	return &stateCache{
		accounts: make(map[types.Address]bool),
		slots:    make(map[storageSlotKey]bool),
	}
}

func (c *stateCache) warmAccount(addr types.Address) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accounts[addr] {
		return true // already warm
	}
	c.accounts[addr] = true
	return false
}

func (c *stateCache) warmSlot(addr types.Address, key types.Hash) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	sk := storageSlotKey{addr: addr, key: key}
	if c.slots[sk] {
		return true // already warm
	}
	c.slots[sk] = true
	return false
}

func (c *stateCache) isAccountWarm(addr types.Address) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.accounts[addr]
}

func (c *stateCache) isSlotWarm(addr types.Address, key types.Hash) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.slots[storageSlotKey{addr: addr, key: key}]
}

func (c *stateCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accounts = make(map[types.Address]bool)
	c.slots = make(map[storageSlotKey]bool)
}

func (c *stateCache) accountCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.accounts)
}

func (c *stateCache) slotCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.slots)
}

// AccessPatternPredictor analyzes transactions to predict which storage
// slots they will access. It uses the transaction's to, from, and calldata
// to infer likely storage accesses without actually executing the tx.
type AccessPatternPredictor struct{}

// NewAccessPatternPredictor creates a new access pattern predictor.
func NewAccessPatternPredictor() *AccessPatternPredictor {
	return &AccessPatternPredictor{}
}

// PredictAccess analyzes a transaction and returns predicted state accesses.
func (p *AccessPatternPredictor) PredictAccess(tx *types.Transaction) *PrefetchRequest {
	if tx == nil {
		return nil
	}

	req := &PrefetchRequest{}

	// Always prefetch the sender if available.
	if sender := tx.Sender(); sender != nil {
		req.Address = *sender
	}

	// Always prefetch the recipient.
	if to := tx.To(); to != nil {
		req.Address = *to

		// For contract calls, predict storage slots from calldata.
		data := tx.Data()
		if len(data) >= 4 {
			req.StorageKeys = append(req.StorageKeys, p.predictSlots(to, data)...)
		}
	}

	// Include access list entries.
	for _, tuple := range tx.AccessList() {
		for _, key := range tuple.StorageKeys {
			req.StorageKeys = append(req.StorageKeys, key)
		}
	}

	return req
}

// predictSlots predicts storage slot accesses from calldata patterns.
// This uses common Solidity storage layout heuristics:
//   - Function selector (4 bytes) -> predict slot keccak(selector)
//   - 32-byte-aligned args -> predict mapping slots keccak(arg, slot)
//   - Address args -> predict balance slot keccak(addr, 0)
func (p *AccessPatternPredictor) predictSlots(to *types.Address, data []byte) []types.Hash {
	var slots []types.Hash

	// Predict slot from function selector.
	selectorSlot := crypto.Keccak256Hash(data[:4])
	slots = append(slots, selectorSlot)

	// For each 32-byte argument, predict mapping-style slots.
	args := data[4:]
	for offset := 0; offset+32 <= len(args) && offset < 128; offset += 32 {
		arg := args[offset : offset+32]

		// Predict keccak(arg || slotIndex) for first few slot indices.
		// This handles common Solidity mappings: mapping(key => value)
		// stored at keccak256(key . slot).
		for slotIdx := uint64(0); slotIdx < 4; slotIdx++ {
			var buf [40]byte // 32 bytes arg + 8 bytes slot index
			copy(buf[:32], arg)
			binary.BigEndian.PutUint64(buf[32:], slotIdx)
			slots = append(slots, crypto.Keccak256Hash(buf[:]))
		}

		// If the argument looks like an address (first 12 bytes zero),
		// also predict a balance/allowance slot.
		isAddr := true
		for i := 0; i < 12; i++ {
			if arg[i] != 0 {
				isAddr = false
				break
			}
		}
		if isAddr {
			balanceSlot := crypto.Keccak256Hash(arg)
			slots = append(slots, balanceSlot)
		}
	}

	return slots
}

// StatePrefetcher manages background prefetching of state data. It spawns
// worker goroutines to warm the state cache before transaction execution.
type StatePrefetcher struct {
	state     StateDB
	cache     *stateCache
	predictor *AccessPatternPredictor

	// Worker pool.
	workers  int
	tasks    chan PrefetchRequest
	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}

	// Stats (atomic for lock-free reads).
	requested   atomic.Uint64
	completed   atomic.Uint64
	cacheHits   atomic.Uint64
	cacheMisses atomic.Uint64
}

// NewStatePrefetcher creates a prefetcher with the given worker count.
// If workers <= 0, defaults to 4.
func NewStatePrefetcher(state StateDB, workers int) *StatePrefetcher {
	if workers <= 0 {
		workers = 4
	}
	sp := &StatePrefetcher{
		state:     state,
		cache:     newStateCache(),
		predictor: NewAccessPatternPredictor(),
		workers:   workers,
		tasks:     make(chan PrefetchRequest, 512),
		stopCh:    make(chan struct{}),
	}
	for i := 0; i < workers; i++ {
		sp.wg.Add(1)
		go sp.worker()
	}
	return sp
}

// worker processes prefetch requests from the task channel.
func (sp *StatePrefetcher) worker() {
	defer sp.wg.Done()
	for {
		select {
		case req, ok := <-sp.tasks:
			if !ok {
				return
			}
			sp.executePrefetch(req)
		case <-sp.stopCh:
			return
		}
	}
}

// executePrefetch warms the cache for a single prefetch request.
func (sp *StatePrefetcher) executePrefetch(req PrefetchRequest) {
	// Warm the account.
	if !req.Address.IsZero() {
		wasWarm := sp.cache.warmAccount(req.Address)
		if wasWarm {
			sp.cacheHits.Add(1)
		} else {
			sp.cacheMisses.Add(1)
			// Touch state to trigger actual loading in a disk-backed DB.
			sp.state.GetBalance(req.Address)
			sp.state.GetNonce(req.Address)
		}
	}

	// Warm storage slots.
	for _, key := range req.StorageKeys {
		if !req.Address.IsZero() {
			wasWarm := sp.cache.warmSlot(req.Address, key)
			if wasWarm {
				sp.cacheHits.Add(1)
			} else {
				sp.cacheMisses.Add(1)
				sp.state.GetState(req.Address, key)
			}
		}
	}

	sp.completed.Add(1)
}

// PrefetchState analyzes the given transactions and prefetches predicted
// state accesses in the background. Non-blocking: returns immediately.
func (sp *StatePrefetcher) PrefetchState(txs []*types.Transaction) {
	for _, tx := range txs {
		req := sp.predictor.PredictAccess(tx)
		if req == nil {
			continue
		}
		sp.requested.Add(1)
		sp.enqueue(*req)

		// Also prefetch the sender account.
		if sender := tx.Sender(); sender != nil {
			sp.requested.Add(1)
			sp.enqueue(PrefetchRequest{Address: *sender})
		}
	}
}

// PrefetchAddress prefetches a single account in the background.
func (sp *StatePrefetcher) PrefetchAddress(addr types.Address) {
	sp.requested.Add(1)
	sp.enqueue(PrefetchRequest{Address: addr})
}

// PrefetchSlots prefetches specific storage slots for an address.
func (sp *StatePrefetcher) PrefetchSlots(addr types.Address, keys []types.Hash) {
	sp.requested.Add(1)
	sp.enqueue(PrefetchRequest{Address: addr, StorageKeys: keys})
}

// enqueue sends a request to the worker pool. Non-blocking: drops if full.
func (sp *StatePrefetcher) enqueue(req PrefetchRequest) {
	select {
	case sp.tasks <- req:
	case <-sp.stopCh:
	default:
		// Drop if queue is full; prefetching is best-effort.
	}
}

// Wait blocks until all queued prefetch tasks have been processed.
func (sp *StatePrefetcher) Wait() {
	// Spin-wait for all tasks to complete.
	for sp.completed.Load() < sp.requested.Load() {
		// yield
	}
}

// Stop shuts down the prefetcher's worker pool. Safe to call multiple times.
func (sp *StatePrefetcher) Stop() {
	sp.stopOnce.Do(func() {
		close(sp.stopCh)
		sp.wg.Wait()
	})
}

// Stats returns current prefetching statistics.
func (sp *StatePrefetcher) Stats() PrefetchStats {
	return PrefetchStats{
		Requested:   sp.requested.Load(),
		Completed:   sp.completed.Load(),
		CacheHits:   sp.cacheHits.Load(),
		CacheMisses: sp.cacheMisses.Load(),
	}
}

// IsWarm returns whether an account has been pre-warmed.
func (sp *StatePrefetcher) IsWarm(addr types.Address) bool {
	return sp.cache.isAccountWarm(addr)
}

// IsSlotWarm returns whether a storage slot has been pre-warmed.
func (sp *StatePrefetcher) IsSlotWarm(addr types.Address, key types.Hash) bool {
	return sp.cache.isSlotWarm(addr, key)
}

// Reset clears the cache and resets stats for a new block.
func (sp *StatePrefetcher) Reset() {
	sp.cache.reset()
	sp.requested.Store(0)
	sp.completed.Store(0)
	sp.cacheHits.Store(0)
	sp.cacheMisses.Store(0)
}

// CacheSize returns the number of warmed accounts and slots.
func (sp *StatePrefetcher) CacheSize() (accounts int, slots int) {
	return sp.cache.accountCount(), sp.cache.slotCount()
}
