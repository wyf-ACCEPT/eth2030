package state

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// StateExpiryConfig configures the epoch-based state expiry mechanism.
// State that has not been accessed for ExpiryPeriod epochs gets expired
// and requires a witness proof to resurrect.
type StateExpiryConfig struct {
	// ExpiryPeriod is the number of epochs of inactivity before state expires.
	ExpiryPeriod uint64
	// MaxWitnessSize is the maximum allowed witness proof size in bytes.
	MaxWitnessSize int
	// RevivalGasCost is the gas charged to revive an expired account.
	RevivalGasCost uint64
}

// DefaultStateExpiryConfig returns a StateExpiryConfig with sensible defaults.
func DefaultStateExpiryConfig() StateExpiryConfig {
	return StateExpiryConfig{
		ExpiryPeriod:   256,
		MaxWitnessSize: 128 * 1024, // 128 KiB
		RevivalGasCost: 50000,
	}
}

// ExpiryRecord tracks the expiry state of a single account, including
// which storage keys have been accessed.
type ExpiryRecord struct {
	Address         types.Address
	LastAccessEpoch uint64
	StorageKeys     map[types.Hash]uint64 // storage key -> last access epoch
	Expired         bool
}

// ExpiryStats summarizes the current state of the expiry manager.
type ExpiryStats struct {
	TotalAccounts    int
	ExpiredCount     int
	ActiveCount      int
	LastExpiredEpoch uint64
}

// StateExpiryManager tracks access epochs for accounts and storage keys,
// expires stale state, and handles revival with witness proofs.
// All operations are thread-safe.
type StateExpiryManager struct {
	mu      sync.RWMutex
	config  StateExpiryConfig
	records map[types.Address]*ExpiryRecord

	// lastExpiredEpoch tracks the most recent epoch at which state was expired.
	lastExpiredEpoch uint64
}

// NewStateExpiryManager creates a new StateExpiryManager with the given config.
func NewStateExpiryManager(config StateExpiryConfig) *StateExpiryManager {
	return &StateExpiryManager{
		config:  config,
		records: make(map[types.Address]*ExpiryRecord),
	}
}

// TouchAccount marks an account as accessed at the given epoch. If the account
// is already expired, touching it has no effect (use ReviveAccount instead).
// If the account is not yet tracked, it is added.
func (m *StateExpiryManager) TouchAccount(addr types.Address, epoch uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.records[addr]
	if !ok {
		m.records[addr] = &ExpiryRecord{
			Address:         addr,
			LastAccessEpoch: epoch,
			StorageKeys:     make(map[types.Hash]uint64),
		}
		return
	}

	// Do not update expired accounts; they must be revived first.
	if rec.Expired {
		return
	}
	if epoch > rec.LastAccessEpoch {
		rec.LastAccessEpoch = epoch
	}
}

// TouchStorage marks a storage key under the given account as accessed
// at the given epoch. The account is also touched. If the account is
// expired, the touch is ignored.
func (m *StateExpiryManager) TouchStorage(addr types.Address, key types.Hash, epoch uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.records[addr]
	if !ok {
		rec = &ExpiryRecord{
			Address:         addr,
			LastAccessEpoch: epoch,
			StorageKeys:     make(map[types.Hash]uint64),
		}
		m.records[addr] = rec
	}

	if rec.Expired {
		return
	}

	if epoch > rec.LastAccessEpoch {
		rec.LastAccessEpoch = epoch
	}
	if existing, found := rec.StorageKeys[key]; !found || epoch > existing {
		rec.StorageKeys[key] = epoch
	}
}

// ExpireStaleState scans all tracked accounts and marks those whose
// last access epoch is more than ExpiryPeriod behind currentEpoch.
// Returns the list of addresses that were newly expired.
func (m *StateExpiryManager) ExpireStaleState(currentEpoch uint64) []types.Address {
	m.mu.Lock()
	defer m.mu.Unlock()

	var expired []types.Address
	for addr, rec := range m.records {
		if rec.Expired {
			continue
		}
		if currentEpoch > rec.LastAccessEpoch+m.config.ExpiryPeriod {
			rec.Expired = true
			expired = append(expired, addr)
		}
	}
	if len(expired) > 0 {
		m.lastExpiredEpoch = currentEpoch
	}
	return expired
}

// IsExpired returns true if the given address is currently expired.
// Returns false for unknown addresses.
func (m *StateExpiryManager) IsExpired(addr types.Address) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rec, ok := m.records[addr]
	if !ok {
		return false
	}
	return rec.Expired
}

// Errors returned by ReviveAccount.
var (
	errStateExpiryNotFound   = errors.New("state_expiry: account not tracked")
	errStateExpiryNotExpired = errors.New("state_expiry: account is not expired")
	errStateExpiryBadProof   = errors.New("state_expiry: invalid or empty witness proof")
	errStateExpiryProofSize  = errors.New("state_expiry: witness proof exceeds MaxWitnessSize")
)

// ReviveAccount revives an expired account using a witness proof.
// The proof must be non-empty and within MaxWitnessSize. Returns an error
// if the account is not tracked, not expired, or the proof is invalid.
func (m *StateExpiryManager) ReviveAccount(addr types.Address, proof []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.records[addr]
	if !ok {
		return errStateExpiryNotFound
	}
	if !rec.Expired {
		return errStateExpiryNotExpired
	}
	if len(proof) == 0 {
		return errStateExpiryBadProof
	}
	if len(proof) > m.config.MaxWitnessSize {
		return errStateExpiryProofSize
	}

	rec.Expired = false
	// Storage keys remain; their access epochs are stale but the account
	// is considered freshly touched at the last expired epoch (caller should
	// follow up with TouchAccount at the current epoch).
	return nil
}

// GetExpiryStats returns a snapshot of the current expiry statistics.
func (m *StateExpiryManager) GetExpiryStats() ExpiryStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := ExpiryStats{
		TotalAccounts:    len(m.records),
		LastExpiredEpoch: m.lastExpiredEpoch,
	}
	for _, rec := range m.records {
		if rec.Expired {
			stats.ExpiredCount++
		} else {
			stats.ActiveCount++
		}
	}
	return stats
}

// GetRecord returns a copy of the ExpiryRecord for the given address,
// or nil if the address is not tracked.
func (m *StateExpiryManager) GetRecord(addr types.Address) *ExpiryRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rec, ok := m.records[addr]
	if !ok {
		return nil
	}
	// Return a deep copy to prevent data races.
	cp := &ExpiryRecord{
		Address:         rec.Address,
		LastAccessEpoch: rec.LastAccessEpoch,
		Expired:         rec.Expired,
		StorageKeys:     make(map[types.Hash]uint64, len(rec.StorageKeys)),
	}
	for k, v := range rec.StorageKeys {
		cp.StorageKeys[k] = v
	}
	return cp
}

// Config returns the current configuration.
func (m *StateExpiryManager) Config() StateExpiryConfig {
	return m.config
}
