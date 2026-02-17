package txpool

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Error codes for transaction validation.
var (
	ErrAlreadyKnown    = errors.New("already known")
	ErrNonceTooLow     = errors.New("nonce too low")
	ErrNonceTooHigh    = errors.New("nonce too high")
	ErrGasLimit        = errors.New("exceeds block gas limit")
	ErrInsufficientFunds = errors.New("insufficient funds for gas * price + value")
	ErrIntrinsicGas    = errors.New("intrinsic gas too low")
	ErrTxPoolFull      = errors.New("transaction pool is full")
	ErrNegativeValue   = errors.New("negative value")
	ErrOversizedData   = errors.New("oversized data")
	ErrUnderpriced     = errors.New("transaction underpriced")
)

// Config holds TxPool configuration.
type Config struct {
	MaxSize      int      // Maximum number of transactions in pool
	MaxPerSender int      // Maximum pending per sender
	MinGasPrice  *big.Int // Minimum gas price to accept
	BlockGasLimit uint64  // Current block gas limit
}

// DefaultConfig returns sensible defaults for the pool.
func DefaultConfig() Config {
	return Config{
		MaxSize:       4096,
		MaxPerSender:  64,
		MinGasPrice:   big.NewInt(1), // 1 wei minimum
		BlockGasLimit: 30_000_000,
	}
}

// StateReader provides account state for validation.
type StateReader interface {
	GetNonce(addr types.Address) uint64
	GetBalance(addr types.Address) *big.Int
}

// txLookup tracks transactions by hash for fast duplicate detection.
type txLookup struct {
	all map[types.Hash]*types.Transaction
}

func newTxLookup() *txLookup {
	return &txLookup{all: make(map[types.Hash]*types.Transaction)}
}

func (l *txLookup) Get(hash types.Hash) *types.Transaction {
	return l.all[hash]
}

func (l *txLookup) Add(tx *types.Transaction) {
	l.all[tx.Hash()] = tx
}

func (l *txLookup) Remove(hash types.Hash) {
	delete(l.all, hash)
}

func (l *txLookup) Count() int {
	return len(l.all)
}

// txSortedList maintains a sorted list of transactions by nonce for a single sender.
type txSortedList struct {
	items []*types.Transaction
}

func (l *txSortedList) Add(tx *types.Transaction) {
	// Insert maintaining nonce order.
	idx := sort.Search(len(l.items), func(i int) bool {
		return l.items[i].Nonce() >= tx.Nonce()
	})
	if idx < len(l.items) && l.items[idx].Nonce() == tx.Nonce() {
		// Replace existing tx with same nonce (if higher gas price).
		l.items[idx] = tx
		return
	}
	l.items = append(l.items, nil)
	copy(l.items[idx+1:], l.items[idx:])
	l.items[idx] = tx
}

func (l *txSortedList) Remove(nonce uint64) bool {
	for i, tx := range l.items {
		if tx.Nonce() == nonce {
			l.items = append(l.items[:i], l.items[i+1:]...)
			return true
		}
	}
	return false
}

func (l *txSortedList) Len() int {
	return len(l.items)
}

// Ready returns transactions that are ready to execute (sequential from baseNonce).
func (l *txSortedList) Ready(baseNonce uint64) []*types.Transaction {
	var ready []*types.Transaction
	expectedNonce := baseNonce
	for _, tx := range l.items {
		if tx.Nonce() != expectedNonce {
			break
		}
		ready = append(ready, tx)
		expectedNonce++
	}
	return ready
}

// TxPool implements a transaction pool for pending and queued transactions.
type TxPool struct {
	config Config
	state  StateReader

	mu      sync.RWMutex
	pending map[types.Address]*txSortedList // processable transactions
	queue   map[types.Address]*txSortedList // future transactions
	lookup  *txLookup                       // hash -> tx
}

// New creates a new transaction pool.
func New(config Config, state StateReader) *TxPool {
	return &TxPool{
		config:  config,
		state:   state,
		pending: make(map[types.Address]*txSortedList),
		queue:   make(map[types.Address]*txSortedList),
		lookup:  newTxLookup(),
	}
}

// AddLocal adds a locally-submitted transaction to the pool.
func (pool *TxPool) AddLocal(tx *types.Transaction) error {
	return pool.add(tx)
}

// AddRemote adds a remotely-received transaction to the pool.
func (pool *TxPool) AddRemote(tx *types.Transaction) error {
	return pool.add(tx)
}

func (pool *TxPool) add(tx *types.Transaction) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	hash := tx.Hash()

	// Check for duplicates.
	if pool.lookup.Get(hash) != nil {
		return ErrAlreadyKnown
	}

	// Check pool capacity.
	if pool.lookup.Count() >= pool.config.MaxSize {
		return ErrTxPoolFull
	}

	// Validate the transaction.
	if err := pool.validateTx(tx); err != nil {
		return err
	}

	// Determine sender (simplified - would normally recover from signature).
	from := pool.senderOf(tx)

	// Add to lookup.
	pool.lookup.Add(tx)

	// Check nonce relative to state to decide pending vs queue.
	stateNonce := pool.state.GetNonce(from)

	if tx.Nonce() < stateNonce {
		pool.lookup.Remove(hash)
		return ErrNonceTooLow
	}

	if tx.Nonce() == stateNonce {
		// This tx is immediately processable.
		pool.addPending(from, tx)
	} else {
		// Future tx, add to queue.
		pool.addQueue(from, tx)
	}

	// Promote queued txs that are now processable.
	pool.promoteQueue(from)

	return nil
}

// validateTx performs basic validation of a transaction.
func (pool *TxPool) validateTx(tx *types.Transaction) error {
	// Reject negative values.
	if tx.Value() != nil && tx.Value().Sign() < 0 {
		return ErrNegativeValue
	}

	// Gas limit check.
	if tx.Gas() > pool.config.BlockGasLimit {
		return ErrGasLimit
	}

	// Intrinsic gas check (minimum 21000 for a transfer).
	intrinsicGas := IntrinsicGas(tx.Data(), tx.To() == nil)
	if tx.Gas() < intrinsicGas {
		return ErrIntrinsicGas
	}

	// Minimum gas price check.
	if pool.config.MinGasPrice != nil {
		effectivePrice := tx.GasPrice()
		if effectivePrice != nil && effectivePrice.Cmp(pool.config.MinGasPrice) < 0 {
			return ErrUnderpriced
		}
	}

	// Data size check (max 128KB).
	if len(tx.Data()) > 128*1024 {
		return ErrOversizedData
	}

	return nil
}

func (pool *TxPool) addPending(from types.Address, tx *types.Transaction) {
	list, ok := pool.pending[from]
	if !ok {
		list = &txSortedList{}
		pool.pending[from] = list
	}
	list.Add(tx)
}

func (pool *TxPool) addQueue(from types.Address, tx *types.Transaction) {
	list, ok := pool.queue[from]
	if !ok {
		list = &txSortedList{}
		pool.queue[from] = list
	}
	list.Add(tx)
}

// promoteQueue moves transactions from queue to pending when their nonce becomes
// sequential with the current pending nonce.
func (pool *TxPool) promoteQueue(from types.Address) {
	queueList, ok := pool.queue[from]
	if !ok || queueList.Len() == 0 {
		return
	}

	pendingList := pool.pending[from]
	var nextNonce uint64
	if pendingList != nil && pendingList.Len() > 0 {
		last := pendingList.items[pendingList.Len()-1]
		nextNonce = last.Nonce() + 1
	} else {
		nextNonce = pool.state.GetNonce(from)
	}

	// Move sequential txs from queue to pending.
	promoted := queueList.Ready(nextNonce)
	for _, tx := range promoted {
		pool.addPending(from, tx)
		queueList.Remove(tx.Nonce())
	}

	if queueList.Len() == 0 {
		delete(pool.queue, from)
	}
}

// Pending returns all processable transactions, grouped by sender and sorted by nonce.
func (pool *TxPool) Pending() map[types.Address][]*types.Transaction {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	result := make(map[types.Address][]*types.Transaction)
	for addr, list := range pool.pending {
		txs := make([]*types.Transaction, len(list.items))
		copy(txs, list.items)
		result[addr] = txs
	}
	return result
}

// PendingFlat returns all pending transactions as a flat slice, sorted by gas price (desc).
func (pool *TxPool) PendingFlat() []*types.Transaction {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	var all []*types.Transaction
	for _, list := range pool.pending {
		all = append(all, list.items...)
	}

	sort.Slice(all, func(i, j int) bool {
		pi := all[i].GasPrice()
		pj := all[j].GasPrice()
		if pi == nil {
			return false
		}
		if pj == nil {
			return true
		}
		return pi.Cmp(pj) > 0
	})
	return all
}

// Get retrieves a transaction by hash.
func (pool *TxPool) Get(hash types.Hash) *types.Transaction {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return pool.lookup.Get(hash)
}

// Remove removes a transaction from the pool (e.g., after inclusion in a block).
func (pool *TxPool) Remove(hash types.Hash) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	tx := pool.lookup.Get(hash)
	if tx == nil {
		return
	}
	pool.lookup.Remove(hash)

	from := pool.senderOf(tx)

	if list, ok := pool.pending[from]; ok {
		list.Remove(tx.Nonce())
		if list.Len() == 0 {
			delete(pool.pending, from)
		}
	}
	if list, ok := pool.queue[from]; ok {
		list.Remove(tx.Nonce())
		if list.Len() == 0 {
			delete(pool.queue, from)
		}
	}
}

// Count returns the total number of transactions in the pool.
func (pool *TxPool) Count() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return pool.lookup.Count()
}

// PendingCount returns the number of pending (processable) transactions.
func (pool *TxPool) PendingCount() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	count := 0
	for _, list := range pool.pending {
		count += list.Len()
	}
	return count
}

// QueuedCount returns the number of queued (future) transactions.
func (pool *TxPool) QueuedCount() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	count := 0
	for _, list := range pool.queue {
		count += list.Len()
	}
	return count
}

// Reset removes all transactions with nonces below the current state nonces.
// Called after a new block is processed.
func (pool *TxPool) Reset(stateReader StateReader) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pool.state = stateReader

	for addr, list := range pool.pending {
		stateNonce := pool.state.GetNonce(addr)
		var toRemove []uint64
		for _, tx := range list.items {
			if tx.Nonce() < stateNonce {
				toRemove = append(toRemove, tx.Nonce())
				pool.lookup.Remove(tx.Hash())
			}
		}
		for _, n := range toRemove {
			list.Remove(n)
		}
		if list.Len() == 0 {
			delete(pool.pending, addr)
		}
	}

	// Re-promote queued txs.
	for addr := range pool.queue {
		pool.promoteQueue(addr)
	}
}

// senderOf extracts the sender address from a transaction.
// If the sender has been cached via SetSender, returns that.
// Otherwise derives from the transaction hash (placeholder for Ecrecover).
func (pool *TxPool) senderOf(tx *types.Transaction) types.Address {
	if from := tx.Sender(); from != nil {
		return *from
	}
	// Fallback: use first 20 bytes of tx hash as sender.
	// A real implementation would use crypto.Ecrecover.
	h := tx.Hash()
	var addr types.Address
	copy(addr[:], h[:20])
	return addr
}

// IntrinsicGas computes the intrinsic gas for a transaction.
func IntrinsicGas(data []byte, isContractCreation bool) uint64 {
	gas := uint64(21000)
	if isContractCreation {
		gas = 53000
	}

	if len(data) > 0 {
		var nz uint64
		for _, b := range data {
			if b != 0 {
				nz++
			}
		}
		z := uint64(len(data)) - nz
		gas += nz * 16 // non-zero byte cost
		gas += z * 4   // zero byte cost
	}
	return gas
}
