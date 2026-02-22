package state

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// ExpiryConfig controls the state expiry mechanism.
type ExpiryConfig struct {
	// ExpiryPeriod is the number of blocks after last access before an account expires.
	ExpiryPeriod uint64
	// RevivalGasCost is the gas cost to revive an expired account.
	RevivalGasCost uint64
	// MaxExpiredPerBlock is the maximum number of accounts to expire per block.
	MaxExpiredPerBlock int
}

// DefaultExpiryConfig returns an ExpiryConfig with sensible defaults.
func DefaultExpiryConfig() ExpiryConfig {
	return ExpiryConfig{
		ExpiryPeriod:       1_000_000,
		RevivalGasCost:     25000,
		MaxExpiredPerBlock: 100,
	}
}

// AccountExpiry tracks the expiry state of a single account.
type AccountExpiry struct {
	Address      types.Address
	LastAccessed uint64
	Expired      bool
	ExpiryBlock  uint64
	StateRoot    types.Hash // state root at expiry time, for revival proof verification
}

// ExpiryManager manages account expiry and revival.
type ExpiryManager struct {
	mu       sync.RWMutex
	config   ExpiryConfig
	accounts map[types.Address]*AccountExpiry
}

// NewExpiryManager creates a new expiry manager with the given config.
func NewExpiryManager(config ExpiryConfig) *ExpiryManager {
	return &ExpiryManager{
		config:   config,
		accounts: make(map[types.Address]*AccountExpiry),
	}
}

// TouchAccount marks an account as recently accessed at the given block number.
// If the account was previously expired, touching it does not revive it;
// use ReviveAccount for that.
func (m *ExpiryManager) TouchAccount(addr types.Address, blockNumber uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	acct, ok := m.accounts[addr]
	if !ok {
		m.accounts[addr] = &AccountExpiry{
			Address:      addr,
			LastAccessed: blockNumber,
		}
		return
	}

	// Only update last accessed if not expired.
	if !acct.Expired {
		acct.LastAccessed = blockNumber
	}
}

// CheckExpiry returns true if the account is expired at the given block number.
// An account is expired if it has been touched AND the current block exceeds
// LastAccessed + ExpiryPeriod. Accounts that have never been touched are
// not considered expired (they are unknown to the manager).
func (m *ExpiryManager) CheckExpiry(addr types.Address, currentBlock uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	acct, ok := m.accounts[addr]
	if !ok {
		return false
	}

	if acct.Expired {
		return true
	}

	return currentBlock > acct.LastAccessed+m.config.ExpiryPeriod
}

// ExpireAccounts scans all tracked accounts and marks those that have expired
// at the given block number. Returns the addresses of newly expired accounts.
// At most MaxExpiredPerBlock accounts are expired per call.
func (m *ExpiryManager) ExpireAccounts(currentBlock uint64) []types.Address {
	m.mu.Lock()
	defer m.mu.Unlock()

	var expired []types.Address
	for addr, acct := range m.accounts {
		if len(expired) >= m.config.MaxExpiredPerBlock {
			break
		}
		if acct.Expired {
			continue
		}
		if currentBlock > acct.LastAccessed+m.config.ExpiryPeriod {
			acct.Expired = true
			acct.ExpiryBlock = currentBlock
			expired = append(expired, addr)
		}
	}
	return expired
}

var (
	errAccountNotExpired = errors.New("expiry: account is not expired")
	errInvalidProof      = errors.New("expiry: invalid revival proof")
	errAccountNotFound   = errors.New("expiry: account not found")
)

// ReviveAccount revives an expired account using a revival proof.
// The proof must be non-empty (placeholder validation). Returns an error
// if the account is not expired or the proof is invalid.
func (m *ExpiryManager) ReviveAccount(addr types.Address, proof []byte, currentBlock uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	acct, ok := m.accounts[addr]
	if !ok {
		return errAccountNotFound
	}

	if !acct.Expired {
		return errAccountNotExpired
	}

	// Placeholder proof validation: require non-empty proof.
	if len(proof) == 0 {
		return errInvalidProof
	}

	acct.Expired = false
	acct.ExpiryBlock = 0
	acct.LastAccessed = currentBlock
	acct.StateRoot = types.Hash{} // clear revival proof root
	return nil
}

// GetExpiry returns the expiry info for an account, or nil if not tracked.
func (m *ExpiryManager) GetExpiry(addr types.Address) *AccountExpiry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	acct, ok := m.accounts[addr]
	if !ok {
		return nil
	}
	// Return a copy to prevent races.
	cp := *acct
	return &cp
}

// ExpiredCount returns the number of currently expired accounts.
func (m *ExpiryManager) ExpiredCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, acct := range m.accounts {
		if acct.Expired {
			count++
		}
	}
	return count
}

// ActiveCount returns the number of active (non-expired) tracked accounts.
func (m *ExpiryManager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, acct := range m.accounts {
		if !acct.Expired {
			count++
		}
	}
	return count
}
