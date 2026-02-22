package state

import (
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
)

// TxPrefetcher pre-loads state data for upcoming transactions using a pool
// of background goroutines. It extracts sender, receiver, and access list
// addresses from each transaction and ensures the corresponding state objects
// are warm in the underlying MemoryStateDB before execution begins.
//
// This is especially valuable for parallel transaction execution pipelines
// where cold state lookups would otherwise cause sequential bottlenecks.
type TxPrefetcher struct {
	db     *MemoryStateDB
	mu     sync.Mutex
	closed atomic.Bool

	// Worker pool control.
	workers  int
	tasks    chan prefetchTask
	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}

	// Stats tracking.
	hits    atomic.Uint64
	misses  atomic.Uint64
	pending atomic.Int64
}

// prefetchTask describes a single unit of work for the prefetch pool.
type prefetchTask struct {
	addr types.Address
	keys []types.Hash // nil means account-only prefetch
}

// TxPrefetchStats reports hit/miss/pending counts for the prefetcher.
type TxPrefetchStats struct {
	Hits    uint64
	Misses  uint64
	Pending int64
}

// NewTxPrefetcher creates a prefetcher with the given number of worker
// goroutines. If workers <= 0 it defaults to 4.
func NewTxPrefetcher(db *MemoryStateDB, workers int) *TxPrefetcher {
	if workers <= 0 {
		workers = 4
	}
	p := &TxPrefetcher{
		db:      db,
		workers: workers,
		tasks:   make(chan prefetchTask, 256),
		stopCh:  make(chan struct{}),
	}
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

// worker processes prefetch tasks from the task channel.
func (p *TxPrefetcher) worker() {
	defer p.wg.Done()
	for {
		select {
		case task, ok := <-p.tasks:
			if !ok {
				return
			}
			p.executePrefetch(task)
		case <-p.stopCh:
			return
		}
	}
}

// executePrefetch performs the actual state warming for a single task.
func (p *TxPrefetcher) executePrefetch(task prefetchTask) {
	defer p.pending.Add(-1)

	p.mu.Lock()
	existed := p.db.stateObjects[task.addr] != nil
	if !existed {
		p.db.stateObjects[task.addr] = newStateObject()
	}
	p.mu.Unlock()

	if existed {
		p.hits.Add(1)
	} else {
		p.misses.Add(1)
	}

	// For storage key prefetches, ensure the state object exists (already done)
	// and in a disk-backed implementation, trigger async reads of the keys.
	// For MemoryStateDB, the account creation above is sufficient.
}

// Prefetch queues all addresses touched by the given transactions for
// background prefetching. It extracts sender, receiver, and access list
// entries from each transaction.
func (p *TxPrefetcher) Prefetch(txs []*types.Transaction) {
	if p.closed.Load() {
		return
	}
	for _, tx := range txs {
		// Prefetch the receiver if present.
		if to := tx.To(); to != nil {
			p.enqueueAddress(*to)
		}

		// Prefetch the cached sender if available.
		if sender := tx.Sender(); sender != nil {
			p.enqueueAddress(*sender)
		}

		// Prefetch access list addresses and their storage keys.
		for _, tuple := range tx.AccessList() {
			if len(tuple.StorageKeys) > 0 {
				p.enqueueStorage(tuple.Address, tuple.StorageKeys)
			} else {
				p.enqueueAddress(tuple.Address)
			}
		}
	}
}

// PrefetchAddress queues a single address for background state loading.
func (p *TxPrefetcher) PrefetchAddress(addr types.Address) {
	if p.closed.Load() {
		return
	}
	p.enqueueAddress(addr)
}

// PrefetchStorage queues specific storage slots for background loading.
func (p *TxPrefetcher) PrefetchStorage(addr types.Address, keys []types.Hash) {
	if p.closed.Load() {
		return
	}
	p.enqueueStorage(addr, keys)
}

// enqueueAddress sends an account prefetch task to the worker pool.
func (p *TxPrefetcher) enqueueAddress(addr types.Address) {
	p.pending.Add(1)
	select {
	case p.tasks <- prefetchTask{addr: addr}:
	case <-p.stopCh:
		p.pending.Add(-1)
	}
}

// enqueueStorage sends a storage prefetch task to the worker pool.
func (p *TxPrefetcher) enqueueStorage(addr types.Address, keys []types.Hash) {
	p.pending.Add(1)
	select {
	case p.tasks <- prefetchTask{addr: addr, keys: keys}:
	case <-p.stopCh:
		p.pending.Add(-1)
	}
}

// TxPrefetcherStats returns current hit/miss/pending counts.
func (p *TxPrefetcher) TxPrefetcherStats() TxPrefetchStats {
	return TxPrefetchStats{
		Hits:    p.hits.Load(),
		Misses:  p.misses.Load(),
		Pending: p.pending.Load(),
	}
}

// Close shuts down the prefetcher's worker pool. It is safe to call
// multiple times. After Close, Prefetch calls become no-ops.
func (p *TxPrefetcher) Close() {
	p.stopOnce.Do(func() {
		p.closed.Store(true)
		close(p.stopCh)
		p.wg.Wait()
	})
}

// Wait blocks until all currently pending prefetch tasks have been processed.
func (p *TxPrefetcher) Wait() {
	// Drain: keep checking until pending reaches zero. Because workers
	// decrement pending after processing, spinning briefly is acceptable
	// for test synchronization.
	for p.pending.Load() > 0 {
		// Yield to let workers finish.
	}
}

// IsPrefetched returns true if the given address has a state object in the DB.
func (p *TxPrefetcher) IsPrefetched(addr types.Address) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.db.stateObjects[addr] != nil
}
