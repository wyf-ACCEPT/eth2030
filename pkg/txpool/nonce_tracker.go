// nonce_tracker.go implements per-account nonce tracking for the transaction
// pool. NonceTracker manages the expected next nonce for each sender, detects
// nonce gaps, and provides efficient nonce lookup and update operations.
package txpool

import (
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// NonceGap represents a gap in the nonce sequence for a sender.
type NonceGap struct {
	Address  types.Address
	Expected uint64 // the expected nonce that is missing
	TxNonce  uint64 // the transaction nonce that caused the gap detection
}

// NonceTrackerConfig configures the NonceTracker.
type NonceTrackerConfig struct {
	// MaxNonceAhead is the maximum nonce distance ahead of the current state
	// nonce that is allowed. Transactions further ahead are rejected.
	MaxNonceAhead uint64
}

// DefaultNonceTrackerConfig returns sensible defaults.
func DefaultNonceTrackerConfig() NonceTrackerConfig {
	return NonceTrackerConfig{
		MaxNonceAhead: MaxNonceGap,
	}
}

// accountNonceState holds nonce tracking state for a single account.
type accountNonceState struct {
	stateNonce  uint64          // nonce from the blockchain state
	pendingMax  uint64          // highest nonce seen in pool txs
	hasPending  bool            // whether any pool nonce has been set
	knownNonces map[uint64]bool // set of nonces currently in the pool
}

// NonceTracker manages expected nonces for transaction senders. It tracks
// both the on-chain state nonce and the pool's pending nonce to detect gaps
// and determine whether a transaction's nonce is valid.
type NonceTracker struct {
	config NonceTrackerConfig
	state  StateReader

	mu       sync.RWMutex
	accounts map[types.Address]*accountNonceState
}

// NewNonceTracker creates a new NonceTracker with the given configuration
// and state reader for on-chain nonce lookups.
func NewNonceTracker(config NonceTrackerConfig, state StateReader) *NonceTracker {
	return &NonceTracker{
		config:   config,
		state:    state,
		accounts: make(map[types.Address]*accountNonceState),
	}
}

// GetNonce returns the expected next nonce for the given address. If the
// address has pending transactions, it returns one past the highest pending
// nonce. Otherwise, it returns the on-chain state nonce.
func (nt *NonceTracker) GetNonce(addr types.Address) uint64 {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	acct, ok := nt.accounts[addr]
	if !ok {
		if nt.state != nil {
			return nt.state.GetNonce(addr)
		}
		return 0
	}

	if acct.hasPending {
		return acct.pendingMax + 1
	}
	return acct.stateNonce
}

// SetNonce explicitly sets the expected nonce for an address. This is
// typically called after processing a new block to update state nonces.
func (nt *NonceTracker) SetNonce(addr types.Address, nonce uint64) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	acct := nt.getOrCreateLocked(addr)
	acct.stateNonce = nonce

	// Remove any known nonces below the new state nonce.
	for n := range acct.knownNonces {
		if n < nonce {
			delete(acct.knownNonces, n)
		}
	}

	// Recalculate pendingMax.
	nt.recalcPendingMaxLocked(acct)
}

// TrackTx records that a transaction with the given nonce exists in the pool
// for the specified address. This updates the pending max nonce.
func (nt *NonceTracker) TrackTx(addr types.Address, nonce uint64) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	acct := nt.getOrCreateLocked(addr)
	acct.knownNonces[nonce] = true

	if !acct.hasPending || nonce > acct.pendingMax {
		acct.pendingMax = nonce
		acct.hasPending = true
	}
}

// UntrackTx removes a nonce from tracking (e.g., when a tx is removed
// from the pool or included in a block).
func (nt *NonceTracker) UntrackTx(addr types.Address, nonce uint64) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	acct, ok := nt.accounts[addr]
	if !ok {
		return
	}

	delete(acct.knownNonces, nonce)
	nt.recalcPendingMaxLocked(acct)

	// Clean up empty accounts.
	if len(acct.knownNonces) == 0 {
		delete(nt.accounts, addr)
	}
}

// DetectGap checks whether a transaction with the given nonce from addr
// would create a gap in the nonce sequence. Returns a NonceGap if there
// is a gap, or nil if the nonce is contiguous.
func (nt *NonceTracker) DetectGap(addr types.Address, txNonce uint64) *NonceGap {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	var stateNonce uint64
	acct, ok := nt.accounts[addr]
	if ok {
		stateNonce = acct.stateNonce
	} else if nt.state != nil {
		stateNonce = nt.state.GetNonce(addr)
	}

	// If txNonce equals state nonce, no gap.
	if txNonce == stateNonce {
		return nil
	}

	// If txNonce is below state nonce, that's a stale nonce (not a gap).
	if txNonce < stateNonce {
		return nil
	}

	// Check for contiguous nonces from stateNonce to txNonce.
	if ok {
		for n := stateNonce; n < txNonce; n++ {
			if !acct.knownNonces[n] {
				return &NonceGap{
					Address:  addr,
					Expected: n,
					TxNonce:  txNonce,
				}
			}
		}
		return nil
	}

	// No account tracked and txNonce > stateNonce means gap.
	if txNonce > stateNonce {
		return &NonceGap{
			Address:  addr,
			Expected: stateNonce,
			TxNonce:  txNonce,
		}
	}
	return nil
}

// IsTooFarAhead returns true if the given nonce is too far ahead of the
// current state nonce for the address (exceeds MaxNonceAhead).
func (nt *NonceTracker) IsTooFarAhead(addr types.Address, txNonce uint64) bool {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	var stateNonce uint64
	if acct, ok := nt.accounts[addr]; ok {
		stateNonce = acct.stateNonce
	} else if nt.state != nil {
		stateNonce = nt.state.GetNonce(addr)
	}

	return txNonce > stateNonce+nt.config.MaxNonceAhead
}

// AllGaps returns all nonce gaps for the given address between the state
// nonce and the highest tracked nonce.
func (nt *NonceTracker) AllGaps(addr types.Address) []uint64 {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	acct, ok := nt.accounts[addr]
	if !ok || !acct.hasPending {
		return nil
	}

	var gaps []uint64
	for n := acct.stateNonce; n <= acct.pendingMax; n++ {
		if !acct.knownNonces[n] {
			gaps = append(gaps, n)
		}
	}
	return gaps
}

// KnownNonces returns all tracked nonces for the given address, sorted.
func (nt *NonceTracker) KnownNonces(addr types.Address) []uint64 {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	acct, ok := nt.accounts[addr]
	if !ok {
		return nil
	}

	nonces := make([]uint64, 0, len(acct.knownNonces))
	for n := range acct.knownNonces {
		nonces = append(nonces, n)
	}
	// Sort the nonces.
	for i := 1; i < len(nonces); i++ {
		for j := i; j > 0 && nonces[j] < nonces[j-1]; j-- {
			nonces[j], nonces[j-1] = nonces[j-1], nonces[j]
		}
	}
	return nonces
}

// AccountCount returns the number of tracked accounts.
func (nt *NonceTracker) AccountCount() int {
	nt.mu.RLock()
	defer nt.mu.RUnlock()
	return len(nt.accounts)
}

// Reset updates the state reader and clears all tracking data. Called
// when the pool is reset after chain reorganization.
func (nt *NonceTracker) Reset(state StateReader) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	nt.state = state
	nt.accounts = make(map[types.Address]*accountNonceState)
}

// getOrCreateLocked returns the account state, creating if needed.
// Caller must hold nt.mu write lock.
func (nt *NonceTracker) getOrCreateLocked(addr types.Address) *accountNonceState {
	acct, ok := nt.accounts[addr]
	if !ok {
		var stateNonce uint64
		if nt.state != nil {
			stateNonce = nt.state.GetNonce(addr)
		}
		acct = &accountNonceState{
			stateNonce:  stateNonce,
			knownNonces: make(map[uint64]bool),
		}
		nt.accounts[addr] = acct
	}
	return acct
}

// recalcPendingMaxLocked recalculates the pendingMax from knownNonces.
// Caller must hold nt.mu write lock.
func (nt *NonceTracker) recalcPendingMaxLocked(acct *accountNonceState) {
	if len(acct.knownNonces) == 0 {
		acct.hasPending = false
		acct.pendingMax = 0
		return
	}
	var max uint64
	first := true
	for n := range acct.knownNonces {
		if first || n > max {
			max = n
			first = false
		}
	}
	acct.pendingMax = max
	acct.hasPending = true
}
