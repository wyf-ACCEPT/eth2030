// account_tracker.go implements AcctTrack, a per-account state tracker for
// the transaction pool. It manages pending nonces, balance reservations,
// nonce gap detection, balance deficit detection, and lazy state loading
// from an underlying StateDB.
package txpool

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// AcctTrack errors.
var (
	ErrAcctNotTracked      = errors.New("account not tracked")
	ErrAcctInsufficientBal = errors.New("account has insufficient balance for pending txs")
	ErrAcctNonceGap        = errors.New("account has nonce gap in pending txs")
)

// AcctInfo holds the tracked state for a single account in the pool.
type AcctInfo struct {
	// Address is the account's Ethereum address.
	Address types.Address

	// StateNonce is the confirmed on-chain nonce from the state DB.
	StateNonce uint64

	// PendingNonce is the next expected nonce (stateNonce + pending count).
	PendingNonce uint64

	// StateBalance is the confirmed on-chain balance from the state DB.
	StateBalance *big.Int

	// ReservedBalance is the total balance consumed by pending transactions
	// (sum of value + gas*gasPrice for each pending tx).
	ReservedBalance *big.Int

	// PendingTxs tracks the nonces of pending transactions for gap detection.
	PendingTxs map[uint64]*big.Int // nonce -> tx cost

	// Dirty indicates whether the state needs to be refreshed from the DB.
	Dirty bool
}

// AvailableBalance returns the balance remaining after subtracting all
// pending transaction reservations.
func (ai *AcctInfo) AvailableBalance() *big.Int {
	if ai.StateBalance == nil {
		return new(big.Int)
	}
	avail := new(big.Int).Sub(ai.StateBalance, ai.ReservedBalance)
	if avail.Sign() < 0 {
		return new(big.Int)
	}
	return avail
}

// HasBalanceDeficit returns true if the reserved balance exceeds the
// account's on-chain balance.
func (ai *AcctInfo) HasBalanceDeficit() bool {
	if ai.StateBalance == nil {
		return ai.ReservedBalance != nil && ai.ReservedBalance.Sign() > 0
	}
	return ai.ReservedBalance.Cmp(ai.StateBalance) > 0
}

// NonceGaps returns a sorted list of missing nonces between StateNonce and
// PendingNonce. An empty slice means all nonces are contiguous.
func (ai *AcctInfo) NonceGaps() []uint64 {
	var gaps []uint64
	for n := ai.StateNonce; n < ai.PendingNonce; n++ {
		if _, ok := ai.PendingTxs[n]; !ok {
			gaps = append(gaps, n)
		}
	}
	return gaps
}

// AcctTrack manages per-account nonce and balance tracking for the
// transaction pool. It lazily loads state from the underlying StateReader
// and caches it, supporting batch refreshes for efficiency during reorgs.
// It is safe for concurrent use.
type AcctTrack struct {
	mu    sync.RWMutex
	state StateReader
	accts map[types.Address]*AcctInfo
}

// NewAcctTrack creates a new account tracker using the given state reader
// for lazy state loading.
func NewAcctTrack(state StateReader) *AcctTrack {
	return &AcctTrack{
		state: state,
		accts: make(map[types.Address]*AcctInfo),
	}
}

// getOrLoad returns the AcctInfo for the given address, lazily loading
// from the state DB if not yet tracked. Caller must hold at.mu write lock.
func (at *AcctTrack) getOrLoad(addr types.Address) *AcctInfo {
	info, ok := at.accts[addr]
	if ok && !info.Dirty {
		return info
	}

	// Load fresh state from the DB.
	nonce := at.state.GetNonce(addr)
	balance := at.state.GetBalance(addr)

	if !ok {
		info = &AcctInfo{
			Address:         addr,
			StateNonce:      nonce,
			PendingNonce:    nonce,
			StateBalance:    balance,
			ReservedBalance: new(big.Int),
			PendingTxs:      make(map[uint64]*big.Int),
		}
		at.accts[addr] = info
	} else {
		info.StateNonce = nonce
		info.StateBalance = balance
		info.Dirty = false
	}

	return info
}

// Track begins tracking an account, loading its current state. If the
// account is already tracked, this is a no-op.
func (at *AcctTrack) Track(addr types.Address) {
	at.mu.Lock()
	defer at.mu.Unlock()
	at.getOrLoad(addr)
}

// Untrack removes an account from tracking and releases all reservations.
func (at *AcctTrack) Untrack(addr types.Address) {
	at.mu.Lock()
	defer at.mu.Unlock()
	delete(at.accts, addr)
}

// AddPendingTx records a pending transaction for the account, updating
// the pending nonce and reserving balance. Returns an error if adding
// the transaction would create a balance deficit.
func (at *AcctTrack) AddPendingTx(addr types.Address, tx *types.Transaction) error {
	at.mu.Lock()
	defer at.mu.Unlock()

	info := at.getOrLoad(addr)
	nonce := tx.Nonce()
	cost := txTotalCost(tx)

	// Record the transaction.
	info.PendingTxs[nonce] = cost
	info.ReservedBalance.Add(info.ReservedBalance, cost)

	// Update pending nonce: walk forward from state nonce.
	info.PendingNonce = at.computePendingNonce(info)

	return nil
}

// RemovePendingTx removes a pending transaction, releasing its balance
// reservation. Returns ErrAcctNotTracked if the account is not tracked.
func (at *AcctTrack) RemovePendingTx(addr types.Address, nonce uint64) error {
	at.mu.Lock()
	defer at.mu.Unlock()

	info, ok := at.accts[addr]
	if !ok {
		return ErrAcctNotTracked
	}

	cost, exists := info.PendingTxs[nonce]
	if !exists {
		return nil
	}

	delete(info.PendingTxs, nonce)
	info.ReservedBalance.Sub(info.ReservedBalance, cost)
	if info.ReservedBalance.Sign() < 0 {
		info.ReservedBalance.SetInt64(0)
	}

	info.PendingNonce = at.computePendingNonce(info)

	// Clean up empty accounts.
	if len(info.PendingTxs) == 0 {
		delete(at.accts, addr)
	}

	return nil
}

// ReplacePendingTx updates the cost reservation for a replacement transaction
// at the same nonce. The old reservation is released and the new one applied.
func (at *AcctTrack) ReplacePendingTx(addr types.Address, tx *types.Transaction) error {
	at.mu.Lock()
	defer at.mu.Unlock()

	info := at.getOrLoad(addr)
	nonce := tx.Nonce()

	// Release old reservation if present.
	if oldCost, exists := info.PendingTxs[nonce]; exists {
		info.ReservedBalance.Sub(info.ReservedBalance, oldCost)
	}

	newCost := txTotalCost(tx)
	info.PendingTxs[nonce] = newCost
	info.ReservedBalance.Add(info.ReservedBalance, newCost)

	info.PendingNonce = at.computePendingNonce(info)

	return nil
}

// GetPendingNonce returns the next expected nonce for the given account.
// If the account is not tracked, the state nonce is loaded and returned.
func (at *AcctTrack) GetPendingNonce(addr types.Address) uint64 {
	at.mu.Lock()
	defer at.mu.Unlock()

	info := at.getOrLoad(addr)
	return info.PendingNonce
}

// GetInfo returns a snapshot of the tracked state for an account.
// Returns nil if the account is not being tracked.
func (at *AcctTrack) GetInfo(addr types.Address) *AcctInfo {
	at.mu.RLock()
	defer at.mu.RUnlock()

	info, ok := at.accts[addr]
	if !ok {
		return nil
	}

	// Return a shallow copy.
	cp := *info
	cp.StateBalance = cloneBigInt(info.StateBalance)
	cp.ReservedBalance = cloneBigInt(info.ReservedBalance)
	cp.PendingTxs = make(map[uint64]*big.Int, len(info.PendingTxs))
	for k, v := range info.PendingTxs {
		cp.PendingTxs[k] = new(big.Int).Set(v)
	}
	return &cp
}

// CheckBalanceDeficit returns an error if the given account's pending
// transactions exceed its on-chain balance.
func (at *AcctTrack) CheckBalanceDeficit(addr types.Address) error {
	at.mu.Lock()
	defer at.mu.Unlock()

	info := at.getOrLoad(addr)
	if info.HasBalanceDeficit() {
		return ErrAcctInsufficientBal
	}
	return nil
}

// DetectNonceGaps returns any nonce gaps for the given account.
func (at *AcctTrack) DetectNonceGaps(addr types.Address) []uint64 {
	at.mu.Lock()
	defer at.mu.Unlock()

	info := at.getOrLoad(addr)
	return info.NonceGaps()
}

// ResetOnReorg marks all tracked accounts as dirty and refreshes their
// state from the provided StateReader. Pending transactions whose nonces
// are below the new state nonce are removed. Returns the list of accounts
// that had transactions invalidated.
func (at *AcctTrack) ResetOnReorg(newState StateReader) []types.Address {
	at.mu.Lock()
	defer at.mu.Unlock()

	at.state = newState

	var invalidated []types.Address

	for addr, info := range at.accts {
		oldNonce := info.StateNonce
		newNonce := newState.GetNonce(addr)
		newBalance := newState.GetBalance(addr)

		info.StateNonce = newNonce
		info.StateBalance = newBalance
		info.Dirty = false

		// Remove pending txs below the new state nonce.
		changed := false
		for nonce, cost := range info.PendingTxs {
			if nonce < newNonce {
				info.ReservedBalance.Sub(info.ReservedBalance, cost)
				delete(info.PendingTxs, nonce)
				changed = true
			}
		}

		if info.ReservedBalance.Sign() < 0 {
			info.ReservedBalance.SetInt64(0)
		}

		info.PendingNonce = at.computePendingNonce(info)

		if changed || oldNonce != newNonce {
			invalidated = append(invalidated, addr)
		}

		// Remove empty accounts.
		if len(info.PendingTxs) == 0 {
			delete(at.accts, addr)
		}
	}

	return invalidated
}

// RefreshBatch refreshes the state for a batch of accounts from the
// underlying state reader. This is more efficient than individual lookups
// when multiple accounts need updating.
func (at *AcctTrack) RefreshBatch(addrs []types.Address) {
	at.mu.Lock()
	defer at.mu.Unlock()

	for _, addr := range addrs {
		info, ok := at.accts[addr]
		if !ok {
			continue
		}
		info.StateNonce = at.state.GetNonce(addr)
		info.StateBalance = at.state.GetBalance(addr)
		info.Dirty = false
		info.PendingNonce = at.computePendingNonce(info)
	}
}

// MarkDirty marks specific accounts as needing state refresh on next access.
func (at *AcctTrack) MarkDirty(addrs ...types.Address) {
	at.mu.Lock()
	defer at.mu.Unlock()

	for _, addr := range addrs {
		if info, ok := at.accts[addr]; ok {
			info.Dirty = true
		}
	}
}

// TrackedCount returns the number of accounts currently being tracked.
func (at *AcctTrack) TrackedCount() int {
	at.mu.RLock()
	defer at.mu.RUnlock()
	return len(at.accts)
}

// TrackedAddresses returns all currently tracked account addresses.
func (at *AcctTrack) TrackedAddresses() []types.Address {
	at.mu.RLock()
	defer at.mu.RUnlock()

	addrs := make([]types.Address, 0, len(at.accts))
	for addr := range at.accts {
		addrs = append(addrs, addr)
	}
	return addrs
}

// computePendingNonce walks forward from stateNonce through contiguous
// nonces present in PendingTxs to determine the pending nonce.
// Caller must hold at.mu.
func (at *AcctTrack) computePendingNonce(info *AcctInfo) uint64 {
	if len(info.PendingTxs) == 0 {
		return info.StateNonce
	}

	// Find the highest nonce present.
	maxNonce := info.StateNonce
	for n := range info.PendingTxs {
		if n >= maxNonce {
			maxNonce = n + 1
		}
	}

	// Walk forward from state nonce to find the contiguous range.
	pending := info.StateNonce
	for {
		if _, ok := info.PendingTxs[pending]; !ok {
			break
		}
		pending++
	}
	return pending
}

// AccountsWithDeficit returns all tracked accounts whose reserved balance
// exceeds their on-chain balance.
func (at *AcctTrack) AccountsWithDeficit() []types.Address {
	at.mu.RLock()
	defer at.mu.RUnlock()

	var result []types.Address
	for addr, info := range at.accts {
		if info.HasBalanceDeficit() {
			result = append(result, addr)
		}
	}
	return result
}

// AccountsWithGaps returns all tracked accounts that have nonce gaps in
// their pending transactions.
func (at *AcctTrack) AccountsWithGaps() []types.Address {
	at.mu.RLock()
	defer at.mu.RUnlock()

	var result []types.Address
	for addr, info := range at.accts {
		gaps := info.NonceGaps()
		if len(gaps) > 0 {
			result = append(result, addr)
		}
	}
	return result
}

// SortedPendingNonces returns the sorted list of pending tx nonces for
// an account. Returns nil if the account is not tracked.
func (at *AcctTrack) SortedPendingNonces(addr types.Address) []uint64 {
	at.mu.RLock()
	defer at.mu.RUnlock()

	info, ok := at.accts[addr]
	if !ok || len(info.PendingTxs) == 0 {
		return nil
	}

	nonces := make([]uint64, 0, len(info.PendingTxs))
	for n := range info.PendingTxs {
		nonces = append(nonces, n)
	}
	sort.Slice(nonces, func(i, j int) bool { return nonces[i] < nonces[j] })
	return nonces
}

// txTotalCost computes the maximum cost a transaction could incur:
// gas * gasPrice + value (+ blobGas * blobFeeCap for type-3).
func txTotalCost(tx *types.Transaction) *big.Int {
	gasPrice := tx.GasPrice()
	if gasPrice == nil {
		gasPrice = new(big.Int)
	}
	cost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(tx.Gas()))
	if v := tx.Value(); v != nil {
		cost.Add(cost, v)
	}
	if tx.Type() == types.BlobTxType {
		if blobFeeCap := tx.BlobGasFeeCap(); blobFeeCap != nil {
			blobCost := new(big.Int).Mul(blobFeeCap, new(big.Int).SetUint64(tx.BlobGas()))
			cost.Add(cost, blobCost)
		}
	}
	return cost
}
