package txpool

import (
	"container/heap"
	"errors"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Priority pool error codes.
var (
	ErrPriorityBelowMin  = errors.New("gas price below minimum threshold")
	ErrPriorityDuplicate = errors.New("transaction already in priority pool")
)

// PriorityPoolConfig configures the priority pool behavior.
type PriorityPoolConfig struct {
	MaxPoolSize       int    // maximum entries in the pool (0 = unlimited)
	MinGasPrice       uint64 // minimum gas price to accept (wei)
	EvictionThreshold uint64 // entries below this gas price are evicted first
}

// PriorityEntry represents a transaction entry in the priority pool,
// ordered by gas price for block building prioritization.
type PriorityEntry struct {
	TxHash    types.Hash
	Sender    types.Address
	GasPrice  uint64
	Nonce     uint64
	Timestamp time.Time
	Size      uint64
}

// priorityHeap implements container/heap.Interface as a max-heap
// sorted by gas price (highest first). Ties are broken by timestamp
// (earlier entries have higher priority).
type priorityHeap []*PriorityEntry

func (h priorityHeap) Len() int { return len(h) }

func (h priorityHeap) Less(i, j int) bool {
	if h[i].GasPrice != h[j].GasPrice {
		return h[i].GasPrice > h[j].GasPrice // max-heap: higher price first
	}
	return h[i].Timestamp.Before(h[j].Timestamp) // earlier timestamp first
}

func (h priorityHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *priorityHeap) Push(x interface{}) {
	*h = append(*h, x.(*PriorityEntry))
}

func (h *priorityHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*h = old[:n-1]
	return item
}

// PriorityPool is a gas-price-priority max-heap for pending transactions.
// It supports capacity-limited insertion with automatic eviction of the
// lowest gas price entries. It is safe for concurrent use.
type PriorityPool struct {
	mu     sync.RWMutex
	config PriorityPoolConfig
	h      priorityHeap
	index  map[types.Hash]int // tx hash -> heap index for O(1) lookup
}

// NewPriorityPool creates a new PriorityPool with the given configuration.
func NewPriorityPool(config PriorityPoolConfig) *PriorityPool {
	pp := &PriorityPool{
		config: config,
		index:  make(map[types.Hash]int),
	}
	heap.Init(&pp.h)
	return pp
}

// rebuildIndex rebuilds the hash-to-index mapping after a heap operation.
// Must be called with mu held.
func (pp *PriorityPool) rebuildIndex() {
	pp.index = make(map[types.Hash]int, len(pp.h))
	for i, entry := range pp.h {
		pp.index[entry.TxHash] = i
	}
}

// Add inserts an entry into the priority pool. If the pool is at capacity,
// the entry with the lowest gas price is evicted (only if the new entry
// has a higher gas price). Returns (true, nil) if added, (false, error)
// otherwise.
func (pp *PriorityPool) Add(entry *PriorityEntry) (bool, error) {
	if entry == nil {
		return false, errors.New("nil entry")
	}

	pp.mu.Lock()
	defer pp.mu.Unlock()

	// Check minimum gas price.
	if entry.GasPrice < pp.config.MinGasPrice {
		return false, ErrPriorityBelowMin
	}

	// Check for duplicate.
	if _, exists := pp.index[entry.TxHash]; exists {
		return false, ErrPriorityDuplicate
	}

	// If at capacity, evict the lowest gas price entry.
	if pp.config.MaxPoolSize > 0 && len(pp.h) >= pp.config.MaxPoolSize {
		lowest := pp.findLowest()
		if lowest == nil || entry.GasPrice <= lowest.GasPrice {
			// New entry is not better than the worst; reject it.
			return false, ErrPriorityBelowMin
		}
		pp.removeByHash(lowest.TxHash)
	}

	heap.Push(&pp.h, entry)
	pp.rebuildIndex()
	return true, nil
}

// Remove removes an entry by its transaction hash. Returns true if found.
func (pp *PriorityPool) Remove(txHash types.Hash) bool {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return pp.removeByHash(txHash)
}

// removeByHash removes an entry by hash. Caller must hold mu.
func (pp *PriorityPool) removeByHash(txHash types.Hash) bool {
	idx, ok := pp.index[txHash]
	if !ok {
		return false
	}
	heap.Remove(&pp.h, idx)
	pp.rebuildIndex()
	return true
}

// Peek returns the highest gas price entry without removing it.
// Returns nil if the pool is empty.
func (pp *PriorityPool) Peek() *PriorityEntry {
	pp.mu.RLock()
	defer pp.mu.RUnlock()

	if len(pp.h) == 0 {
		return nil
	}
	return pp.h[0]
}

// Pop returns and removes the highest gas price entry.
// Returns nil if the pool is empty.
func (pp *PriorityPool) Pop() *PriorityEntry {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	if len(pp.h) == 0 {
		return nil
	}
	entry := heap.Pop(&pp.h).(*PriorityEntry)
	delete(pp.index, entry.TxHash)
	pp.rebuildIndex()
	return entry
}

// GetBySender returns all entries from a given sender address, ordered
// by gas price descending. Returns nil if no entries are found.
func (pp *PriorityPool) GetBySender(sender types.Address) []*PriorityEntry {
	pp.mu.RLock()
	defer pp.mu.RUnlock()

	var result []*PriorityEntry
	for _, entry := range pp.h {
		if entry.Sender == sender {
			result = append(result, entry)
		}
	}
	return result
}

// Size returns the number of entries in the pool.
func (pp *PriorityPool) Size() int {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	return len(pp.h)
}

// MinGasPrice returns the minimum gas price currently in the pool.
// Returns 0 if the pool is empty.
func (pp *PriorityPool) MinGasPrice() uint64 {
	pp.mu.RLock()
	defer pp.mu.RUnlock()

	lowest := pp.findLowest()
	if lowest == nil {
		return 0
	}
	return lowest.GasPrice
}

// findLowest scans the heap for the entry with the lowest gas price.
// Caller must hold at least an RLock on mu.
func (pp *PriorityPool) findLowest() *PriorityEntry {
	if len(pp.h) == 0 {
		return nil
	}
	// In a max-heap, the minimum is among the leaves (last half of array).
	min := pp.h[0]
	for _, e := range pp.h {
		if e.GasPrice < min.GasPrice {
			min = e
		}
	}
	return min
}

// Flush drains all entries from the pool in gas price priority order
// (highest first). The pool is empty after this call.
func (pp *PriorityPool) Flush() []*PriorityEntry {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	result := make([]*PriorityEntry, 0, len(pp.h))
	for len(pp.h) > 0 {
		entry := heap.Pop(&pp.h).(*PriorityEntry)
		result = append(result, entry)
	}
	pp.index = make(map[types.Hash]int)
	return result
}

// Contains returns true if an entry with the given hash is in the pool.
func (pp *PriorityPool) Contains(txHash types.Hash) bool {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	_, ok := pp.index[txHash]
	return ok
}
