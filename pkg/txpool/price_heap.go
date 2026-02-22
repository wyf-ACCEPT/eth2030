package txpool

import (
	"container/heap"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// PriceHeap maintains transactions sorted by effective gas price for efficient
// block building. It keeps separate pending and queued heaps, supports lazy
// deletion of stale entries, nonce-gap detection, and re-heaping when the
// base fee changes.

// priceEntry wraps a transaction with cached effective price and deletion flag.
type priceEntry struct {
	tx             *types.Transaction
	sender         types.Address
	effectivePrice *big.Int // cached effective gas price
	deleted        bool     // lazy deletion marker
	index          int      // position in heap slice
}

// ----------------------------------------------------------------
// minPriceHeap: min-heap by effective gas price (cheapest first).
// Used for eviction: Pop() yields the lowest-priced transaction.
// ----------------------------------------------------------------

type minPriceHeap []*priceEntry

func (h minPriceHeap) Len() int { return len(h) }

func (h minPriceHeap) Less(i, j int) bool {
	return h[i].effectivePrice.Cmp(h[j].effectivePrice) < 0
}

func (h minPriceHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *minPriceHeap) Push(x interface{}) {
	entry := x.(*priceEntry)
	entry.index = len(*h)
	*h = append(*h, entry)
}

func (h *minPriceHeap) Pop() interface{} {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	entry.index = -1
	*h = old[:n-1]
	return entry
}

// ----------------------------------------------------------------
// maxTipHeap: max-heap by priority fee tip (highest tip first).
// Used for block building: Pop() yields the highest-tipping transaction.
// ----------------------------------------------------------------

type maxTipHeap []*priceEntry

func (h maxTipHeap) Len() int { return len(h) }

func (h maxTipHeap) Less(i, j int) bool {
	return h[i].effectivePrice.Cmp(h[j].effectivePrice) > 0
}

func (h maxTipHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *maxTipHeap) Push(x interface{}) {
	entry := x.(*priceEntry)
	entry.index = len(*h)
	*h = append(*h, entry)
}

func (h *maxTipHeap) Pop() interface{} {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	entry.index = -1
	*h = old[:n-1]
	return entry
}

// ----------------------------------------------------------------
// PriceHeap: combined price-sorted transaction heap.
// ----------------------------------------------------------------

// PriceHeap tracks transactions sorted by gas price for efficient eviction
// and block building. It separates pending (executable) and queued (future)
// transactions, supports lazy deletion, nonce-gap detection, and adjusts
// effective prices when the base fee changes.
type PriceHeap struct {
	mu sync.RWMutex

	// pending holds executable transactions sorted by effective price (min-heap
	// for eviction, max-heap for block building).
	pendingMin minPriceHeap
	pendingMax maxTipHeap

	// queued holds future transactions sorted by effective price (min-heap).
	queuedMin minPriceHeap

	// byHash enables O(1) lookup and lazy deletion by transaction hash.
	byHash map[types.Hash]*priceEntry

	// bySender tracks per-sender nonces for gap detection.
	bySender map[types.Address]map[uint64]*priceEntry

	// baseFee is the current block base fee used to compute effective prices.
	baseFee *big.Int

	// staleCount tracks the number of lazily deleted entries awaiting cleanup.
	staleCount int
}

// NewPriceHeap creates a new PriceHeap with the given initial base fee.
// If baseFee is nil, legacy gas prices are used directly.
func NewPriceHeap(baseFee *big.Int) *PriceHeap {
	ph := &PriceHeap{
		byHash:   make(map[types.Hash]*priceEntry),
		bySender: make(map[types.Address]map[uint64]*priceEntry),
	}
	if baseFee != nil {
		ph.baseFee = new(big.Int).Set(baseFee)
	}
	heap.Init(&ph.pendingMin)
	heap.Init(&ph.pendingMax)
	heap.Init(&ph.queuedMin)
	return ph
}

// AddPending inserts an executable transaction into the pending heaps.
func (ph *PriceHeap) AddPending(tx *types.Transaction, sender types.Address) {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	hash := tx.Hash()
	if _, exists := ph.byHash[hash]; exists {
		return
	}

	price := EffectiveGasPrice(tx, ph.baseFee)
	entry := &priceEntry{
		tx:             tx,
		sender:         sender,
		effectivePrice: price,
	}

	ph.byHash[hash] = entry
	ph.trackSenderNonce(sender, tx.Nonce(), entry)

	// Insert into both pending heaps. The entry is shared; each heap tracks
	// its own index, so we need separate entry copies for index tracking.
	minEntry := &priceEntry{
		tx: tx, sender: sender, effectivePrice: price,
	}
	maxEntry := &priceEntry{
		tx: tx, sender: sender, effectivePrice: price,
	}
	// Link them through byHash for lazy deletion.
	ph.byHash[hash] = minEntry

	heap.Push(&ph.pendingMin, minEntry)
	heap.Push(&ph.pendingMax, maxEntry)
}

// AddQueued inserts a future transaction into the queued heap.
func (ph *PriceHeap) AddQueued(tx *types.Transaction, sender types.Address) {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	hash := tx.Hash()
	if _, exists := ph.byHash[hash]; exists {
		return
	}

	price := EffectiveGasPrice(tx, ph.baseFee)
	entry := &priceEntry{
		tx:             tx,
		sender:         sender,
		effectivePrice: price,
	}

	ph.byHash[hash] = entry
	ph.trackSenderNonce(sender, tx.Nonce(), entry)
	heap.Push(&ph.queuedMin, entry)
}

// Remove lazily marks a transaction for deletion. The entry is skipped during
// subsequent Pop operations and physically removed during cleanup.
func (ph *PriceHeap) Remove(hash types.Hash) {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	entry, ok := ph.byHash[hash]
	if !ok {
		return
	}
	entry.deleted = true
	ph.staleCount++
	delete(ph.byHash, hash)
	ph.untrackSenderNonce(entry.sender, entry.tx.Nonce())
}

// PopCheapest returns and removes the pending transaction with the lowest
// effective gas price, skipping lazily deleted entries. Returns nil if
// no live pending transactions exist.
func (ph *PriceHeap) PopCheapest() *types.Transaction {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	for ph.pendingMin.Len() > 0 {
		entry := heap.Pop(&ph.pendingMin).(*priceEntry)
		if entry.deleted {
			ph.staleCount--
			continue
		}
		entry.deleted = true // mark so maxTipHeap skips it
		ph.staleCount++
		return entry.tx
	}
	return nil
}

// PeekHighestTip returns the pending transaction with the highest effective
// gas tip without removing it, skipping stale entries.
func (ph *PriceHeap) PeekHighestTip() *types.Transaction {
	ph.mu.RLock()
	defer ph.mu.RUnlock()

	for i := 0; i < ph.pendingMax.Len(); i++ {
		entry := ph.pendingMax[i]
		if !entry.deleted {
			return entry.tx
		}
	}
	return nil
}

// PopCheapestQueued returns and removes the queued transaction with the lowest
// effective gas price, skipping lazily deleted entries.
func (ph *PriceHeap) PopCheapestQueued() *types.Transaction {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	for ph.queuedMin.Len() > 0 {
		entry := heap.Pop(&ph.queuedMin).(*priceEntry)
		if entry.deleted {
			ph.staleCount--
			continue
		}
		entry.deleted = true
		return entry.tx
	}
	return nil
}

// DetectNonceGaps returns transactions from the given sender that have a
// nonce gap relative to the provided base nonce (typically the sender's
// current state nonce). A gap exists when expected sequential nonces are missing.
func (ph *PriceHeap) DetectNonceGaps(sender types.Address, baseNonce uint64) []uint64 {
	ph.mu.RLock()
	defer ph.mu.RUnlock()

	nonceMap, ok := ph.bySender[sender]
	if !ok || len(nonceMap) == 0 {
		return nil
	}

	// Find the maximum nonce for this sender.
	maxNonce := baseNonce
	for nonce, entry := range nonceMap {
		if !entry.deleted && nonce > maxNonce {
			maxNonce = nonce
		}
	}

	// Identify missing nonces between baseNonce and maxNonce.
	var gaps []uint64
	for n := baseNonce; n < maxNonce; n++ {
		entry, exists := nonceMap[n]
		if !exists || entry.deleted {
			gaps = append(gaps, n)
		}
	}
	return gaps
}

// SetBaseFee updates the base fee and recomputes effective gas prices for
// all live entries. Both pending and queued heaps are rebuilt.
func (ph *PriceHeap) SetBaseFee(baseFee *big.Int) {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	if baseFee != nil {
		ph.baseFee = new(big.Int).Set(baseFee)
	} else {
		ph.baseFee = nil
	}

	// Recompute effective prices and rebuild heaps.
	ph.reheapLocked()
}

// reheapLocked rebuilds all heaps after a base fee change or cleanup.
// Caller must hold ph.mu.
func (ph *PriceHeap) reheapLocked() {
	// Collect all live entries.
	var pendingEntries, queuedEntries []*priceEntry

	for i := 0; i < ph.pendingMin.Len(); i++ {
		e := ph.pendingMin[i]
		if !e.deleted {
			e.effectivePrice = EffectiveGasPrice(e.tx, ph.baseFee)
			pendingEntries = append(pendingEntries, e)
		}
	}
	for i := 0; i < ph.queuedMin.Len(); i++ {
		e := ph.queuedMin[i]
		if !e.deleted {
			e.effectivePrice = EffectiveGasPrice(e.tx, ph.baseFee)
			queuedEntries = append(queuedEntries, e)
		}
	}

	// Reset heaps.
	ph.pendingMin = make(minPriceHeap, 0, len(pendingEntries))
	ph.pendingMax = make(maxTipHeap, 0, len(pendingEntries))
	ph.queuedMin = make(minPriceHeap, 0, len(queuedEntries))

	for _, e := range pendingEntries {
		minE := &priceEntry{
			tx: e.tx, sender: e.sender, effectivePrice: e.effectivePrice,
		}
		maxE := &priceEntry{
			tx: e.tx, sender: e.sender, effectivePrice: e.effectivePrice,
		}
		ph.pendingMin = append(ph.pendingMin, minE)
		ph.pendingMax = append(ph.pendingMax, maxE)
	}
	for _, e := range queuedEntries {
		qE := &priceEntry{
			tx: e.tx, sender: e.sender, effectivePrice: e.effectivePrice,
		}
		ph.queuedMin = append(ph.queuedMin, qE)
	}

	heap.Init(&ph.pendingMin)
	heap.Init(&ph.pendingMax)
	heap.Init(&ph.queuedMin)
	ph.staleCount = 0
}

// Cleanup removes all lazily deleted entries from the heaps.
// Call periodically to reclaim memory.
func (ph *PriceHeap) Cleanup() {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	if ph.staleCount == 0 {
		return
	}
	ph.reheapLocked()
}

// PendingCount returns the number of live pending transactions.
func (ph *PriceHeap) PendingCount() int {
	ph.mu.RLock()
	defer ph.mu.RUnlock()

	count := 0
	for i := 0; i < ph.pendingMin.Len(); i++ {
		if !ph.pendingMin[i].deleted {
			count++
		}
	}
	return count
}

// QueuedCount returns the number of live queued transactions.
func (ph *PriceHeap) QueuedCount() int {
	ph.mu.RLock()
	defer ph.mu.RUnlock()

	count := 0
	for i := 0; i < ph.queuedMin.Len(); i++ {
		if !ph.queuedMin[i].deleted {
			count++
		}
	}
	return count
}

// StaleCount returns the number of lazily deleted entries awaiting cleanup.
func (ph *PriceHeap) StaleCount() int {
	ph.mu.RLock()
	defer ph.mu.RUnlock()
	return ph.staleCount
}

// trackSenderNonce records a nonce -> entry mapping for a sender.
func (ph *PriceHeap) trackSenderNonce(sender types.Address, nonce uint64, entry *priceEntry) {
	nonceMap, ok := ph.bySender[sender]
	if !ok {
		nonceMap = make(map[uint64]*priceEntry)
		ph.bySender[sender] = nonceMap
	}
	nonceMap[nonce] = entry
}

// untrackSenderNonce removes a nonce entry for a sender.
func (ph *PriceHeap) untrackSenderNonce(sender types.Address, nonce uint64) {
	nonceMap, ok := ph.bySender[sender]
	if !ok {
		return
	}
	delete(nonceMap, nonce)
	if len(nonceMap) == 0 {
		delete(ph.bySender, sender)
	}
}
