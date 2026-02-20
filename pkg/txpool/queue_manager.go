package txpool

import (
	"math/big"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Queue capacity constants.
const (
	// DefaultQueueCapPerAccount is the default maximum queued txs per account.
	DefaultQueueCapPerAccount = 64

	// DefaultGlobalQueueCap is the default maximum total queued txs.
	DefaultGlobalQueueCap = 1024
)

// QueueManagerConfig configures the queue manager.
type QueueManagerConfig struct {
	MaxPerAccount int // max queued txs per account (0 = DefaultQueueCapPerAccount)
	MaxGlobal     int // max total queued txs (0 = DefaultGlobalQueueCap)
}

// QueueManager manages future (queued) transactions whose nonces are too high
// for immediate execution. It supports per-account capacity limits, automatic
// promotion when nonce gaps fill, and eviction of old/low-price transactions.
type QueueManager struct {
	mu      sync.RWMutex
	config  QueueManagerConfig
	queues  map[types.Address]*accountQueue
	baseFee *big.Int
	total   int // global count of queued transactions
}

// accountQueue holds queued transactions for a single sender, nonce-sorted.
type accountQueue struct {
	txs        []*types.Transaction // sorted by nonce ascending
	stateNonce uint64
}

// NewQueueManager creates a new QueueManager.
func NewQueueManager(config QueueManagerConfig, baseFee *big.Int) *QueueManager {
	if config.MaxPerAccount <= 0 {
		config.MaxPerAccount = DefaultQueueCapPerAccount
	}
	if config.MaxGlobal <= 0 {
		config.MaxGlobal = DefaultGlobalQueueCap
	}
	qm := &QueueManager{
		config: config,
		queues: make(map[types.Address]*accountQueue),
	}
	if baseFee != nil {
		qm.baseFee = new(big.Int).Set(baseFee)
	}
	return qm
}

// Add inserts a future transaction into the queue for the given sender.
// If the per-account or global capacity is exceeded, the lowest-priced
// transaction from the sender's queue is evicted. Returns the evicted
// transaction (if any) and an error.
func (qm *QueueManager) Add(sender types.Address, tx *types.Transaction) (*types.Transaction, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	aq := qm.getOrCreate(sender)

	// Check for existing tx with same nonce (replace-by-fee).
	idx := sort.Search(len(aq.txs), func(i int) bool {
		return aq.txs[i].Nonce() >= tx.Nonce()
	})
	if idx < len(aq.txs) && aq.txs[idx].Nonce() == tx.Nonce() {
		old := aq.txs[idx]
		if !hasPriceBump(old, tx, qm.baseFee) {
			return nil, ErrReplacementUnderpriced
		}
		aq.txs[idx] = tx
		return old, nil
	}

	// Evict if per-account limit reached.
	var evicted *types.Transaction
	if len(aq.txs) >= qm.config.MaxPerAccount {
		evicted = qm.evictFromAccount(aq)
		if evicted == nil {
			return nil, ErrSenderLimitExceeded
		}
		qm.total--
		// Recompute idx after eviction changed the slice.
		idx = sort.Search(len(aq.txs), func(i int) bool {
			return aq.txs[i].Nonce() >= tx.Nonce()
		})
	}

	// Evict globally if needed.
	if evicted == nil && qm.total >= qm.config.MaxGlobal {
		globalEvicted := qm.evictGlobal()
		if globalEvicted == nil {
			return nil, ErrTxPoolFull
		}
		// If the global evict removed from this sender's account, recompute idx.
		idx = sort.Search(len(aq.txs), func(i int) bool {
			return aq.txs[i].Nonce() >= tx.Nonce()
		})
	}

	// Insert maintaining nonce order.
	aq.txs = append(aq.txs, nil)
	copy(aq.txs[idx+1:], aq.txs[idx:])
	aq.txs[idx] = tx
	qm.total++

	return evicted, nil
}

// Remove deletes a specific transaction by sender and nonce.
func (qm *QueueManager) Remove(sender types.Address, nonce uint64) bool {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	aq, ok := qm.queues[sender]
	if !ok {
		return false
	}

	idx := sort.Search(len(aq.txs), func(i int) bool {
		return aq.txs[i].Nonce() >= nonce
	})
	if idx >= len(aq.txs) || aq.txs[idx].Nonce() != nonce {
		return false
	}
	aq.txs = append(aq.txs[:idx], aq.txs[idx+1:]...)
	qm.total--
	if len(aq.txs) == 0 {
		delete(qm.queues, sender)
	}
	return true
}

// PromoteReady returns and removes the gap-free prefix from the sender's queue
// that is contiguous with the given base nonce. These transactions can be moved
// to the pending list.
func (qm *QueueManager) PromoteReady(sender types.Address, baseNonce uint64) []*types.Transaction {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	aq, ok := qm.queues[sender]
	if !ok || len(aq.txs) == 0 {
		return nil
	}

	var promotable int
	expected := baseNonce
	for _, tx := range aq.txs {
		if tx.Nonce() != expected {
			break
		}
		promotable++
		expected++
	}
	if promotable == 0 {
		return nil
	}

	promoted := make([]*types.Transaction, promotable)
	copy(promoted, aq.txs[:promotable])
	aq.txs = aq.txs[promotable:]
	qm.total -= promotable

	if len(aq.txs) == 0 {
		delete(qm.queues, sender)
	}
	return promoted
}

// UpdateStateNonce updates the sender's state nonce and removes any queued
// transactions with nonces below it (already mined).
func (qm *QueueManager) UpdateStateNonce(sender types.Address, nonce uint64) int {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	aq, ok := qm.queues[sender]
	if !ok {
		return 0
	}
	aq.stateNonce = nonce

	cutoff := 0
	for i, tx := range aq.txs {
		if tx.Nonce() >= nonce {
			cutoff = i
			break
		}
		cutoff = i + 1
	}
	if cutoff == 0 {
		return 0
	}
	removed := cutoff
	aq.txs = aq.txs[cutoff:]
	qm.total -= removed
	if len(aq.txs) == 0 {
		delete(qm.queues, sender)
	}
	return removed
}

// AccountStats returns the number of queued transactions and the state nonce
// for the given sender.
func (qm *QueueManager) AccountStats(sender types.Address) (count int, stateNonce uint64) {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	aq, ok := qm.queues[sender]
	if !ok {
		return 0, 0
	}
	return len(aq.txs), aq.stateNonce
}

// Get retrieves a queued transaction by sender and nonce.
func (qm *QueueManager) Get(sender types.Address, nonce uint64) *types.Transaction {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	aq, ok := qm.queues[sender]
	if !ok {
		return nil
	}
	idx := sort.Search(len(aq.txs), func(i int) bool {
		return aq.txs[i].Nonce() >= nonce
	})
	if idx < len(aq.txs) && aq.txs[idx].Nonce() == nonce {
		return aq.txs[idx]
	}
	return nil
}

// Len returns the total number of queued transactions across all accounts.
func (qm *QueueManager) Len() int {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	return qm.total
}

// AccountCount returns the number of accounts with queued transactions.
func (qm *QueueManager) AccountCount() int {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	return len(qm.queues)
}

// AccountNonces returns all queued nonces for a sender, sorted ascending.
func (qm *QueueManager) AccountNonces(sender types.Address) []uint64 {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	aq, ok := qm.queues[sender]
	if !ok {
		return nil
	}
	nonces := make([]uint64, len(aq.txs))
	for i, tx := range aq.txs {
		nonces[i] = tx.Nonce()
	}
	return nonces
}

// SetBaseFee updates the base fee used for price comparisons.
func (qm *QueueManager) SetBaseFee(baseFee *big.Int) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	if baseFee != nil {
		qm.baseFee = new(big.Int).Set(baseFee)
	} else {
		qm.baseFee = nil
	}
}

// EvictAll removes all queued transactions for the given sender.
func (qm *QueueManager) EvictAll(sender types.Address) int {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	aq, ok := qm.queues[sender]
	if !ok {
		return 0
	}
	count := len(aq.txs)
	qm.total -= count
	delete(qm.queues, sender)
	return count
}

// getOrCreate returns the accountQueue for sender, creating if needed.
// Caller must hold qm.mu.
func (qm *QueueManager) getOrCreate(sender types.Address) *accountQueue {
	aq, ok := qm.queues[sender]
	if !ok {
		aq = &accountQueue{}
		qm.queues[sender] = aq
	}
	return aq
}

// evictFromAccount removes the oldest (lowest-nonce) transaction with the
// lowest effective gas price from the account's queue. Returns the evicted
// transaction or nil if the queue is empty. Caller must hold qm.mu.
func (qm *QueueManager) evictFromAccount(aq *accountQueue) *types.Transaction {
	if len(aq.txs) == 0 {
		return nil
	}

	// Find the lowest-priced transaction.
	worstIdx := 0
	worstPrice := EffectiveGasPrice(aq.txs[0], qm.baseFee)
	for i := 1; i < len(aq.txs); i++ {
		price := EffectiveGasPrice(aq.txs[i], qm.baseFee)
		if price.Cmp(worstPrice) < 0 {
			worstPrice = price
			worstIdx = i
		}
	}

	evicted := aq.txs[worstIdx]
	aq.txs = append(aq.txs[:worstIdx], aq.txs[worstIdx+1:]...)
	return evicted
}

// evictGlobal removes the lowest-priced queued transaction across all accounts.
// Returns the evicted tx or nil if the queue is empty. Caller must hold qm.mu.
func (qm *QueueManager) evictGlobal() *types.Transaction {
	var (
		worstAddr  types.Address
		worstIdx   int
		worstPrice *big.Int
		found      bool
	)

	for addr, aq := range qm.queues {
		for i, tx := range aq.txs {
			price := EffectiveGasPrice(tx, qm.baseFee)
			if !found || price.Cmp(worstPrice) < 0 {
				worstAddr = addr
				worstIdx = i
				worstPrice = price
				found = true
			}
		}
	}
	if !found {
		return nil
	}

	aq := qm.queues[worstAddr]
	evicted := aq.txs[worstIdx]
	aq.txs = append(aq.txs[:worstIdx], aq.txs[worstIdx+1:]...)
	qm.total--
	if len(aq.txs) == 0 {
		delete(qm.queues, worstAddr)
	}
	return evicted
}
