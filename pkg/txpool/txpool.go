package txpool

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Pool constants.
const (
	// PriceBump is the minimum gas price bump percentage for replace-by-fee.
	PriceBump = 10

	// MaxPoolSize is the maximum number of transactions the pool holds.
	MaxPoolSize = 4096

	// MaxPerSender is the maximum number of transactions per sender.
	MaxPerSender = 16

	// MaxTxSize is the maximum allowed encoded transaction size (128KB).
	MaxTxSize = 128 * 1024

	// MaxNonceGap is the maximum allowed gap between a transaction's nonce
	// and the sender's current state nonce. Transactions with nonces too far
	// ahead are rejected to prevent memory exhaustion from nonce-gap attacks.
	MaxNonceGap = 64

	// EIP-2930 access list gas costs.
	AccessListAddressCost = 2400 // per address in access list
	AccessListStorageCost = 1900 // per storage key in access list

	// priceBumpPercent is kept for internal use (same as PriceBump).
	priceBumpPercent = PriceBump
)

// Error codes for transaction validation.
var (
	ErrAlreadyKnown           = errors.New("already known")
	ErrNonceTooLow            = errors.New("nonce too low")
	ErrNonceTooHigh           = errors.New("nonce too high")
	ErrGasLimit               = errors.New("exceeds block gas limit")
	ErrInsufficientFunds      = errors.New("insufficient funds for gas * price + value")
	ErrIntrinsicGas           = errors.New("intrinsic gas too low")
	ErrTxPoolFull             = errors.New("transaction pool is full")
	ErrNegativeValue          = errors.New("negative value")
	ErrOversizedData          = errors.New("oversized data")
	ErrUnderpriced            = errors.New("transaction underpriced")
	ErrReplacementUnderpriced = errors.New("replacement transaction underpriced")
	ErrSenderLimitExceeded    = errors.New("per-sender transaction limit exceeded")
	ErrFeeCapBelowTip         = errors.New("max fee per gas less than max priority fee per gas")
	ErrFeeCapBelowBaseFee     = errors.New("max fee per gas less than block base fee")
	ErrBlobTxMissingHashes    = errors.New("blob transaction missing versioned hashes")
	ErrBlobFeeCapBelowBaseFee = errors.New("blob fee cap less than blob base fee")
	ErrNegativeGasPrice       = errors.New("negative gas price or fee cap")
	ErrOversizedRLP           = errors.New("encoded transaction too large")
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
		MaxSize:       MaxPoolSize,
		MaxPerSender:  MaxPerSender,
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

func (l *txSortedList) Get(nonce uint64) *types.Transaction {
	idx := sort.Search(len(l.items), func(i int) bool {
		return l.items[i].Nonce() >= nonce
	})
	if idx < len(l.items) && l.items[idx].Nonce() == nonce {
		return l.items[idx]
	}
	return nil
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
	config      Config
	state       StateReader
	baseFee     *big.Int // current base fee, nil if unknown
	blobBaseFee *big.Int // current blob base fee (EIP-4844), nil if unknown

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

	// Validate the transaction.
	if err := pool.validateTx(tx); err != nil {
		return err
	}

	// Determine sender (simplified - would normally recover from signature).
	from := pool.senderOf(tx)

	// Check nonce relative to state to decide pending vs queue.
	stateNonce := pool.state.GetNonce(from)

	if tx.Nonce() < stateNonce {
		return ErrNonceTooLow
	}

	// Nonce gap detection: reject transactions with nonces too far ahead
	// of the current state nonce to prevent memory exhaustion attacks.
	if tx.Nonce() > stateNonce+MaxNonceGap {
		return ErrNonceTooHigh
	}

	// Check for replace-by-fee: existing tx from same sender with same nonce.
	replaced, err := pool.checkReplacement(from, tx)
	if err != nil {
		return err
	}

	// Per-sender limit: count all txs from this sender across pending + queue.
	// Replacements don't increase the count, so skip the check if replacing.
	if !replaced {
		senderCount := pool.senderTxCount(from)
		if senderCount >= pool.config.MaxPerSender {
			return ErrSenderLimitExceeded
		}
	}

	// If pool is full and this isn't a replacement, try eviction.
	if !replaced && pool.lookup.Count() >= pool.config.MaxSize {
		evicted := pool.evictLowest(pool.baseFee)
		if evicted == 0 {
			return ErrTxPoolFull
		}
	}

	// Add to lookup.
	pool.lookup.Add(tx)

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

// senderTxCount returns the total number of transactions from a sender
// across both pending and queued lists.
func (pool *TxPool) senderTxCount(from types.Address) int {
	count := 0
	if list, ok := pool.pending[from]; ok {
		count += list.Len()
	}
	if list, ok := pool.queue[from]; ok {
		count += list.Len()
	}
	return count
}

// checkReplacement handles replace-by-fee logic. If an existing tx with the same
// nonce from the same sender exists, the new tx must have >= 10% higher gas price.
// Returns (true, nil) if replaced, (false, nil) if no existing tx, or
// (false, ErrReplacementUnderpriced) if the bump is insufficient.
func (pool *TxPool) checkReplacement(from types.Address, tx *types.Transaction) (bool, error) {
	// Check pending list first.
	if list, ok := pool.pending[from]; ok {
		if old := list.Get(tx.Nonce()); old != nil {
			if !pool.hasSufficientBump(old, tx) {
				return false, ErrReplacementUnderpriced
			}
			pool.lookup.Remove(old.Hash())
			return true, nil
		}
	}
	// Check queue.
	if list, ok := pool.queue[from]; ok {
		if old := list.Get(tx.Nonce()); old != nil {
			if !pool.hasSufficientBump(old, tx) {
				return false, ErrReplacementUnderpriced
			}
			pool.lookup.Remove(old.Hash())
			return true, nil
		}
	}
	return false, nil
}

// hasSufficientBump checks if newTx has >= priceBumpPercent higher
// effective gas price than oldTx. For EIP-1559 style transactions, both the
// fee cap and tip cap must individually meet the bump threshold.
func (pool *TxPool) hasSufficientBump(oldTx, newTx *types.Transaction) bool {
	oldPrice := EffectiveGasPrice(oldTx, pool.baseFee)
	newPrice := EffectiveGasPrice(newTx, pool.baseFee)

	// New price must be >= old price * (100 + priceBumpPercent) / 100.
	threshold := new(big.Int).Mul(oldPrice, big.NewInt(100+priceBumpPercent))
	threshold.Div(threshold, big.NewInt(100))
	if newPrice.Cmp(threshold) < 0 {
		return false
	}

	// For EIP-1559 style transactions, also require the tip cap to meet the bump.
	// This prevents gaming where a tx with a high fee cap but low tip replaces
	// a tx with a lower fee cap but higher tip.
	if isDynamic(oldTx) && isDynamic(newTx) {
		oldTip := oldTx.GasTipCap()
		newTip := newTx.GasTipCap()
		if oldTip != nil && newTip != nil {
			tipThreshold := new(big.Int).Mul(oldTip, big.NewInt(100+priceBumpPercent))
			tipThreshold.Div(tipThreshold, big.NewInt(100))
			if newTip.Cmp(tipThreshold) < 0 {
				return false
			}
		}
	}

	return true
}

// isDynamic returns true if the transaction is an EIP-1559 style transaction
// (DynamicFeeTx, BlobTx, or SetCodeTx).
func isDynamic(tx *types.Transaction) bool {
	return tx.Type() == types.DynamicFeeTxType ||
		tx.Type() == types.BlobTxType ||
		tx.Type() == types.SetCodeTxType
}

// validateTx performs comprehensive validation of a transaction.
func (pool *TxPool) validateTx(tx *types.Transaction) error {
	// Reject negative values.
	if tx.Value() != nil && tx.Value().Sign() < 0 {
		return ErrNegativeValue
	}

	// Gas price / fee cap must be non-negative.
	if gp := tx.GasPrice(); gp != nil && gp.Sign() < 0 {
		return ErrNegativeGasPrice
	}
	if fc := tx.GasFeeCap(); fc != nil && fc.Sign() < 0 {
		return ErrNegativeGasPrice
	}

	// Gas limit check.
	if tx.Gas() > pool.config.BlockGasLimit {
		return ErrGasLimit
	}

	// RLP size limit enforcement: reject transactions exceeding 128KB encoded size.
	if rlpBytes, err := tx.EncodeRLP(); err == nil {
		if len(rlpBytes) > MaxTxSize {
			return ErrOversizedRLP
		}
	}

	// Data size check (max 128KB).
	if len(tx.Data()) > MaxTxSize {
		return ErrOversizedData
	}

	// EIP-2930 access list gas accounting: include access list cost in intrinsic gas.
	intrinsicGas := IntrinsicGas(tx.Data(), tx.To() == nil)
	intrinsicGas += AccessListGas(tx.AccessList())
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

	// EIP-1559 (type 2): maxFeePerGas must be >= maxPriorityFeePerGas.
	if tx.Type() == types.DynamicFeeTxType || tx.Type() == types.BlobTxType || tx.Type() == types.SetCodeTxType {
		feeCap := tx.GasFeeCap()
		tipCap := tx.GasTipCap()
		if feeCap != nil && tipCap != nil && feeCap.Cmp(tipCap) < 0 {
			return ErrFeeCapBelowTip
		}
	}

	// EIP-1559 base fee validation: reject txs with GasFeeCap below the current base fee.
	if pool.baseFee != nil {
		feeCap := tx.GasFeeCap()
		if feeCap == nil {
			feeCap = tx.GasPrice()
		}
		if feeCap != nil && feeCap.Cmp(pool.baseFee) < 0 {
			return ErrFeeCapBelowBaseFee
		}
	}

	// Blob transaction (type 3) validation.
	if tx.Type() == types.BlobTxType {
		if len(tx.BlobHashes()) == 0 {
			return ErrBlobTxMissingHashes
		}
		// EIP-4844: validate blob gas fee cap against the current blob base fee.
		if pool.blobBaseFee != nil {
			blobFeeCap := tx.BlobGasFeeCap()
			if blobFeeCap == nil || blobFeeCap.Cmp(pool.blobBaseFee) < 0 {
				return ErrBlobFeeCapBelowBaseFee
			}
		}
	}

	// Balance check: sender must have enough for value + gas * gasPrice.
	from := pool.senderOf(tx)
	balance := pool.state.GetBalance(from)
	if balance != nil {
		cost := pool.txCost(tx)
		if balance.Cmp(cost) < 0 {
			return ErrInsufficientFunds
		}
	}

	return nil
}

// txCost returns the maximum cost a transaction could incur:
// gas * gasPrice + value (+ blobGas * blobFeeCap for blob txs).
func (pool *TxPool) txCost(tx *types.Transaction) *big.Int {
	gasPrice := tx.GasPrice()
	if gasPrice == nil {
		gasPrice = new(big.Int)
	}
	cost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(tx.Gas()))
	if v := tx.Value(); v != nil {
		cost.Add(cost, v)
	}
	// For blob txs, add blob gas cost.
	if tx.Type() == types.BlobTxType {
		blobFeeCap := tx.BlobGasFeeCap()
		if blobFeeCap != nil {
			blobCost := new(big.Int).Mul(blobFeeCap, new(big.Int).SetUint64(tx.BlobGas()))
			cost.Add(cost, blobCost)
		}
	}
	return cost
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
// After removal, queued transactions are promoted if their nonces become sequential.
func (pool *TxPool) Remove(hash types.Hash) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	tx := pool.lookup.Get(hash)
	if tx == nil {
		return
	}
	pool.lookup.Remove(hash)

	from := pool.senderOf(tx)
	wasPending := false

	if list, ok := pool.pending[from]; ok {
		if list.Remove(tx.Nonce()) {
			wasPending = true
		}
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

	// If a pending tx was removed, try to promote queued txs to fill the gap.
	if wasPending {
		pool.promoteQueue(from)
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
// Otherwise recovers the sender from the ECDSA signature via ecrecover.
func (pool *TxPool) senderOf(tx *types.Transaction) types.Address {
	if from := tx.Sender(); from != nil {
		return *from
	}
	// Recover sender from signature using ecrecover.
	sigHash := tx.SigningHash()
	v, r, s := tx.RawSignatureValues()
	if r == nil || s == nil {
		return types.Address{}
	}
	// Build 65-byte signature [R || S || V].
	sig := make([]byte, 65)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)
	// Compute recovery ID from V.
	if v != nil {
		vVal := v.Uint64()
		switch tx.Type() {
		case types.LegacyTxType:
			// EIP-155: V = chainID*2 + 35 + recovery_id
			// Pre-EIP-155: V = 27 + recovery_id
			if vVal >= 35 {
				chainID := tx.ChainId()
				if chainID != nil && chainID.Sign() > 0 {
					vVal -= chainID.Uint64()*2 + 35
				}
			} else if vVal >= 27 {
				vVal -= 27
			}
		default:
			// Typed transactions: V is 0 or 1 directly.
		}
		sig[64] = byte(vVal)
	}
	pub, err := crypto.SigToPub(sigHash[:], sig)
	if err != nil {
		return types.Address{}
	}
	addr := crypto.PubkeyToAddress(*pub)
	tx.SetSender(addr)
	return addr
}

// EffectiveGasPrice calculates the effective gas price for a transaction
// given a base fee. For legacy transactions, this is simply GasPrice.
// For EIP-1559 transactions: min(MaxFeePerGas, BaseFee + MaxPriorityFeePerGas).
// If baseFee is nil, returns GasFeeCap (MaxFeePerGas) as the effective price.
func EffectiveGasPrice(tx *types.Transaction, baseFee *big.Int) *big.Int {
	if baseFee == nil || tx.Type() == types.LegacyTxType || tx.Type() == types.AccessListTxType {
		gp := tx.GasPrice()
		if gp == nil {
			return new(big.Int)
		}
		return new(big.Int).Set(gp)
	}
	// EIP-1559 style: effective = min(feeCap, baseFee + tipCap)
	feeCap := tx.GasFeeCap()
	tipCap := tx.GasTipCap()
	if feeCap == nil {
		return new(big.Int)
	}
	if tipCap == nil {
		tipCap = new(big.Int)
	}
	// baseFee + tipCap
	effectiveTip := new(big.Int).Add(baseFee, tipCap)
	// min(feeCap, baseFee + tipCap)
	if effectiveTip.Cmp(feeCap) > 0 {
		return new(big.Int).Set(feeCap)
	}
	return effectiveTip
}

// PendingSorted returns all pending transactions sorted by effective gas price
// (descending). Higher-priced transactions come first for block building.
func (pool *TxPool) PendingSorted() []*types.Transaction {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	baseFee := pool.baseFee

	var all []*types.Transaction
	for _, list := range pool.pending {
		all = append(all, list.items...)
	}

	sort.Slice(all, func(i, j int) bool {
		pi := EffectiveGasPrice(all[i], baseFee)
		pj := EffectiveGasPrice(all[j], baseFee)
		return pi.Cmp(pj) > 0
	})
	return all
}

// evictLowest removes the transaction with the lowest effective gas price from
// the pool. It protects the highest-nonce pending tx for each sender (so every
// sender keeps at least one tx). Returns the number of evicted transactions.
func (pool *TxPool) evictLowest(baseFee *big.Int) int {
	// Collect all transactions with their effective prices, excluding
	// protected txs (highest-nonce pending tx per sender).
	type candidate struct {
		tx    *types.Transaction
		from  types.Address
		price *big.Int
		queue bool // whether the tx is in the queue (not pending)
	}

	var candidates []candidate

	// Gather pending txs. Protect the highest-nonce tx per sender.
	for addr, list := range pool.pending {
		if list.Len() == 0 {
			continue
		}
		for i, tx := range list.items {
			// Protect the last (highest-nonce) tx if it's the only one.
			if list.Len() == 1 {
				continue
			}
			// Protect the highest-nonce pending tx.
			if i == list.Len()-1 {
				continue
			}
			candidates = append(candidates, candidate{
				tx:    tx,
				from:  addr,
				price: EffectiveGasPrice(tx, baseFee),
				queue: false,
			})
		}
	}

	// All queued txs are eviction candidates.
	for addr, list := range pool.queue {
		for _, tx := range list.items {
			candidates = append(candidates, candidate{
				tx:    tx,
				from:  addr,
				price: EffectiveGasPrice(tx, baseFee),
				queue: true,
			})
		}
	}

	if len(candidates) == 0 {
		return 0
	}

	// Sort by price ascending so the cheapest is first.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].price.Cmp(candidates[j].price) < 0
	})

	// Evict the cheapest.
	c := candidates[0]
	pool.lookup.Remove(c.tx.Hash())
	if c.queue {
		if list, ok := pool.queue[c.from]; ok {
			list.Remove(c.tx.Nonce())
			if list.Len() == 0 {
				delete(pool.queue, c.from)
			}
		}
	} else {
		if list, ok := pool.pending[c.from]; ok {
			list.Remove(c.tx.Nonce())
			if list.Len() == 0 {
				delete(pool.pending, c.from)
			}
		}
	}
	return 1
}

// SetBaseFee updates the pool's base fee and demotes pending transactions
// that can no longer afford the new base fee to the queue.
func (pool *TxPool) SetBaseFee(baseFee *big.Int) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pool.baseFee = new(big.Int).Set(baseFee)

	// Demote pending txs whose max fee is below the new base fee.
	for addr, list := range pool.pending {
		var demote []*types.Transaction
		for _, tx := range list.items {
			feeCap := tx.GasFeeCap()
			if feeCap == nil {
				feeCap = tx.GasPrice()
			}
			if feeCap != nil && feeCap.Cmp(baseFee) < 0 {
				demote = append(demote, tx)
			}
		}
		for _, tx := range demote {
			list.Remove(tx.Nonce())
			pool.addQueue(addr, tx)
		}
		if list.Len() == 0 {
			delete(pool.pending, addr)
		}
	}
}

// IntrinsicGas computes the intrinsic gas for a transaction (excluding access list).
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

// AccessListGas computes the gas cost of an EIP-2930 access list.
// Each address costs 2400 gas and each storage key costs 1900 gas.
func AccessListGas(al types.AccessList) uint64 {
	if len(al) == 0 {
		return 0
	}
	var gas uint64
	for _, tuple := range al {
		gas += AccessListAddressCost
		gas += uint64(len(tuple.StorageKeys)) * AccessListStorageCost
	}
	return gas
}

// SetBlobBaseFee updates the pool's blob base fee (EIP-4844). Blob transactions
// with a blob fee cap below this value will be rejected during validation.
func (pool *TxPool) SetBlobBaseFee(blobBaseFee *big.Int) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.blobBaseFee = new(big.Int).Set(blobBaseFee)
}
