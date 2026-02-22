package txpool

import (
	"math/big"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// PendingList manages per-account pending transaction queues with nonce ordering,
// gap detection, ready-prefix promotion, balance/nonce state updates, and
// replace-by-fee enforcement (10% minimum price bump).
type PendingList struct {
	mu       sync.RWMutex
	accounts map[types.Address]*accountPending
	baseFee  *big.Int
}

// accountPending holds nonce-sorted transactions for a single sender.
type accountPending struct {
	txs        []*types.Transaction // sorted by nonce ascending
	stateNonce uint64
	balance    *big.Int
}

// NewPendingList creates a new PendingList.
func NewPendingList(baseFee *big.Int) *PendingList {
	pl := &PendingList{
		accounts: make(map[types.Address]*accountPending),
	}
	if baseFee != nil {
		pl.baseFee = new(big.Int).Set(baseFee)
	}
	return pl
}

// Add inserts a transaction into the pending list for the given sender.
// If a transaction with the same nonce already exists, the new one must have
// at least a 10% effective gas price bump, otherwise ErrReplacementUnderpriced
// is returned. Returns (replaced, error).
func (pl *PendingList) Add(sender types.Address, tx *types.Transaction) (bool, error) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	acct := pl.getOrCreateAccount(sender)

	idx := sort.Search(len(acct.txs), func(i int) bool {
		return acct.txs[i].Nonce() >= tx.Nonce()
	})

	// Check for replacement.
	if idx < len(acct.txs) && acct.txs[idx].Nonce() == tx.Nonce() {
		old := acct.txs[idx]
		if !hasPriceBump(old, tx, pl.baseFee) {
			return false, ErrReplacementUnderpriced
		}
		acct.txs[idx] = tx
		return true, nil
	}

	// Insert at the correct position, maintaining nonce order.
	acct.txs = append(acct.txs, nil)
	copy(acct.txs[idx+1:], acct.txs[idx:])
	acct.txs[idx] = tx
	return false, nil
}

// Remove deletes a transaction by nonce from the given sender's pending list.
// Returns true if found and removed.
func (pl *PendingList) Remove(sender types.Address, nonce uint64) bool {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	acct, ok := pl.accounts[sender]
	if !ok {
		return false
	}

	idx := sort.Search(len(acct.txs), func(i int) bool {
		return acct.txs[i].Nonce() >= nonce
	})
	if idx >= len(acct.txs) || acct.txs[idx].Nonce() != nonce {
		return false
	}
	acct.txs = append(acct.txs[:idx], acct.txs[idx+1:]...)
	if len(acct.txs) == 0 {
		delete(pl.accounts, sender)
	}
	return true
}

// Get retrieves a specific transaction by sender and nonce.
func (pl *PendingList) Get(sender types.Address, nonce uint64) *types.Transaction {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	acct, ok := pl.accounts[sender]
	if !ok {
		return nil
	}
	idx := sort.Search(len(acct.txs), func(i int) bool {
		return acct.txs[i].Nonce() >= nonce
	})
	if idx < len(acct.txs) && acct.txs[idx].Nonce() == nonce {
		return acct.txs[idx]
	}
	return nil
}

// DetectGaps returns missing nonces between the sender's state nonce and the
// maximum nonce in their pending list. If the list is contiguous, returns nil.
func (pl *PendingList) DetectGaps(sender types.Address) []uint64 {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	acct, ok := pl.accounts[sender]
	if !ok || len(acct.txs) == 0 {
		return nil
	}

	startNonce := acct.stateNonce
	maxNonce := acct.txs[len(acct.txs)-1].Nonce()

	// Build a set of present nonces for O(1) lookup.
	present := make(map[uint64]struct{}, len(acct.txs))
	for _, tx := range acct.txs {
		present[tx.Nonce()] = struct{}{}
	}

	var gaps []uint64
	for n := startNonce; n <= maxNonce; n++ {
		if _, ok := present[n]; !ok {
			gaps = append(gaps, n)
		}
	}
	return gaps
}

// Ready returns the gap-free prefix of transactions starting from the sender's
// state nonce. These transactions are immediately executable.
func (pl *PendingList) Ready(sender types.Address) []*types.Transaction {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	acct, ok := pl.accounts[sender]
	if !ok || len(acct.txs) == 0 {
		return nil
	}

	var ready []*types.Transaction
	expectedNonce := acct.stateNonce
	for _, tx := range acct.txs {
		if tx.Nonce() != expectedNonce {
			break
		}
		ready = append(ready, tx)
		expectedNonce++
	}
	return ready
}

// Promote extracts and returns the gap-free prefix of executable transactions
// starting from the sender's state nonce, removing them from the pending list.
func (pl *PendingList) Promote(sender types.Address) []*types.Transaction {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	acct, ok := pl.accounts[sender]
	if !ok || len(acct.txs) == 0 {
		return nil
	}

	var readyCount int
	expectedNonce := acct.stateNonce
	for _, tx := range acct.txs {
		if tx.Nonce() != expectedNonce {
			break
		}
		readyCount++
		expectedNonce++
	}
	if readyCount == 0 {
		return nil
	}

	ready := make([]*types.Transaction, readyCount)
	copy(ready, acct.txs[:readyCount])
	acct.txs = acct.txs[readyCount:]
	if len(acct.txs) == 0 {
		delete(pl.accounts, sender)
	}
	return ready
}

// UpdateState updates the sender's state nonce and balance. Transactions with
// nonces below the new state nonce are removed (already mined). Returns the
// removed transactions.
func (pl *PendingList) UpdateState(sender types.Address, nonce uint64, balance *big.Int) []*types.Transaction {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	acct := pl.getOrCreateAccount(sender)
	acct.stateNonce = nonce
	if balance != nil {
		acct.balance = new(big.Int).Set(balance)
	}

	// Remove transactions with nonces below the new state nonce.
	var removed []*types.Transaction
	cutoff := 0
	for i, tx := range acct.txs {
		if tx.Nonce() >= nonce {
			cutoff = i
			break
		}
		removed = append(removed, tx)
		cutoff = i + 1
	}
	if cutoff > 0 {
		acct.txs = acct.txs[cutoff:]
	}
	if len(acct.txs) == 0 {
		delete(pl.accounts, sender)
	}
	return removed
}

// ByGasPrice returns all pending transactions across all accounts, sorted by
// effective gas price in descending order (highest price first).
func (pl *PendingList) ByGasPrice() []*types.Transaction {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	baseFee := pl.baseFee
	var all []*types.Transaction
	for _, acct := range pl.accounts {
		all = append(all, acct.txs...)
	}

	sort.Slice(all, func(i, j int) bool {
		pi := EffectiveGasPrice(all[i], baseFee)
		pj := EffectiveGasPrice(all[j], baseFee)
		return pi.Cmp(pj) > 0
	})
	return all
}

// SetBaseFee updates the base fee used for effective gas price calculations.
func (pl *PendingList) SetBaseFee(baseFee *big.Int) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if baseFee != nil {
		pl.baseFee = new(big.Int).Set(baseFee)
	} else {
		pl.baseFee = nil
	}
}

// Len returns the total number of pending transactions across all accounts.
func (pl *PendingList) Len() int {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	count := 0
	for _, acct := range pl.accounts {
		count += len(acct.txs)
	}
	return count
}

// AccountLen returns the number of pending transactions for a specific sender.
func (pl *PendingList) AccountLen(sender types.Address) int {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	acct, ok := pl.accounts[sender]
	if !ok {
		return 0
	}
	return len(acct.txs)
}

// Senders returns all sender addresses that have pending transactions.
func (pl *PendingList) Senders() []types.Address {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	senders := make([]types.Address, 0, len(pl.accounts))
	for addr := range pl.accounts {
		senders = append(senders, addr)
	}
	return senders
}

// getOrCreateAccount returns the accountPending for the given address,
// creating one if it does not exist. Caller must hold pl.mu.
func (pl *PendingList) getOrCreateAccount(sender types.Address) *accountPending {
	acct, ok := pl.accounts[sender]
	if !ok {
		acct = &accountPending{
			balance: new(big.Int),
		}
		pl.accounts[sender] = acct
	}
	return acct
}

// hasPriceBump checks whether newTx has at least a 10% effective gas price
// bump over oldTx. Both the effective gas price and (for EIP-1559 txs) the
// tip cap must individually meet the bump threshold.
func hasPriceBump(oldTx, newTx *types.Transaction, baseFee *big.Int) bool {
	oldPrice := EffectiveGasPrice(oldTx, baseFee)
	newPrice := EffectiveGasPrice(newTx, baseFee)

	threshold := new(big.Int).Mul(oldPrice, big.NewInt(100+PriceBump))
	threshold.Div(threshold, big.NewInt(100))
	if newPrice.Cmp(threshold) < 0 {
		return false
	}

	// For EIP-1559 style transactions, also require the tip cap bump.
	if isDynamic(oldTx) && isDynamic(newTx) {
		oldTip := oldTx.GasTipCap()
		newTip := newTx.GasTipCap()
		if oldTip != nil && newTip != nil {
			tipThreshold := new(big.Int).Mul(oldTip, big.NewInt(100+PriceBump))
			tipThreshold.Div(tipThreshold, big.NewInt(100))
			if newTip.Cmp(tipThreshold) < 0 {
				return false
			}
		}
	}
	return true
}
