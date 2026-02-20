// snapshot_gen.go implements a state snapshot generator for fast state access.
// It allows building an in-memory representation of account and storage state
// with progress tracking and deterministic root computation.
package state

import (
	"math/big"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// SnapshotGenConfig holds configuration for the snapshot generator.
type SnapshotGenConfig struct {
	// BatchSize is the number of accounts to process per generation batch.
	BatchSize int

	// MaxAccounts is the maximum number of accounts the snapshot can hold.
	// Zero means unlimited.
	MaxAccounts uint64

	// AsyncGenerate enables asynchronous snapshot generation.
	AsyncGenerate bool
}

// AccountSnapshot represents a single account in the snapshot.
type AccountSnapshot struct {
	Address  types.Address
	Balance  *big.Int
	Nonce    uint64
	CodeHash types.Hash
	Root     types.Hash
}

// SnapshotGenerator builds and manages an in-memory state snapshot for fast
// account and storage access. All methods are safe for concurrent use.
type SnapshotGenerator struct {
	config SnapshotGenConfig

	mu       sync.RWMutex
	accounts map[types.Address]*AccountSnapshot
	storage  map[types.Address]map[types.Hash]types.Hash

	// progress tracks generation progress from 0.0 to 1.0.
	progress float64
}

// NewSnapshotGenerator creates a new snapshot generator with the given config.
func NewSnapshotGenerator(config SnapshotGenConfig) *SnapshotGenerator {
	if config.BatchSize <= 0 {
		config.BatchSize = 256
	}
	return &SnapshotGenerator{
		config:   config,
		accounts: make(map[types.Address]*AccountSnapshot),
		storage:  make(map[types.Address]map[types.Hash]types.Hash),
	}
}

// AddAccount adds or updates an account in the snapshot. Returns an error if
// the maximum account limit has been reached.
func (sg *SnapshotGenerator) AddAccount(snap AccountSnapshot) error {
	sg.mu.Lock()
	defer sg.mu.Unlock()

	// Check limit only when adding a new account, not updating an existing one.
	if sg.config.MaxAccounts > 0 {
		if _, exists := sg.accounts[snap.Address]; !exists {
			if uint64(len(sg.accounts)) >= sg.config.MaxAccounts {
				return errMaxAccountsReached
			}
		}
	}

	// Defensive copy of Balance to avoid aliasing.
	copied := snap
	if snap.Balance != nil {
		copied.Balance = new(big.Int).Set(snap.Balance)
	} else {
		copied.Balance = new(big.Int)
	}
	sg.accounts[snap.Address] = &copied
	return nil
}

// GetAccount looks up an account by address. Returns nil if not found.
func (sg *SnapshotGenerator) GetAccount(addr types.Address) *AccountSnapshot {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	snap, ok := sg.accounts[addr]
	if !ok {
		return nil
	}
	// Return a copy to avoid data races.
	result := *snap
	if snap.Balance != nil {
		result.Balance = new(big.Int).Set(snap.Balance)
	}
	return &result
}

// AddStorage adds a storage key-value entry for the given account address.
// If the account does not exist in the snapshot, the storage entry is still
// recorded (it can be queried or will be associated when the account is added).
func (sg *SnapshotGenerator) AddStorage(addr types.Address, key, value types.Hash) {
	sg.mu.Lock()
	defer sg.mu.Unlock()

	m, ok := sg.storage[addr]
	if !ok {
		m = make(map[types.Hash]types.Hash)
		sg.storage[addr] = m
	}
	m[key] = value
}

// GetStorage returns the storage value for the given account and key.
// Returns the zero hash if not found.
func (sg *SnapshotGenerator) GetStorage(addr types.Address, key types.Hash) types.Hash {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	m, ok := sg.storage[addr]
	if !ok {
		return types.Hash{}
	}
	return m[key]
}

// AccountCount returns the number of accounts in the snapshot.
func (sg *SnapshotGenerator) AccountCount() int {
	sg.mu.RLock()
	defer sg.mu.RUnlock()
	return len(sg.accounts)
}

// StorageCount returns the number of storage entries for a given account.
func (sg *SnapshotGenerator) StorageCount(addr types.Address) int {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	m, ok := sg.storage[addr]
	if !ok {
		return 0
	}
	return len(m)
}

// ComputeRoot computes a deterministic root hash over all accounts in the
// snapshot. Accounts are sorted by address and hashed together with their
// balance, nonce, code hash, and root. This is a simplified hash computation
// for snapshot integrity, not a full Merkle Patricia Trie root.
func (sg *SnapshotGenerator) ComputeRoot() types.Hash {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	if len(sg.accounts) == 0 {
		return types.EmptyRootHash
	}

	// Collect and sort addresses for deterministic ordering.
	addrs := make([]types.Address, 0, len(sg.accounts))
	for addr := range sg.accounts {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return compareAddresses(addrs[i], addrs[j]) < 0
	})

	// Hash all account data together.
	var combined []byte
	for _, addr := range addrs {
		snap := sg.accounts[addr]
		combined = append(combined, addr[:]...)
		combined = append(combined, snap.CodeHash[:]...)
		combined = append(combined, snap.Root[:]...)
		// Encode nonce as 8 bytes big-endian.
		nonce := snap.Nonce
		nonceBytes := [8]byte{
			byte(nonce >> 56), byte(nonce >> 48), byte(nonce >> 40), byte(nonce >> 32),
			byte(nonce >> 24), byte(nonce >> 16), byte(nonce >> 8), byte(nonce),
		}
		combined = append(combined, nonceBytes[:]...)
		// Encode balance as big-endian bytes.
		if snap.Balance != nil {
			combined = append(combined, snap.Balance.Bytes()...)
		}
	}
	return crypto.Keccak256Hash(combined)
}

// GenerateProgress returns the current generation progress as a value between
// 0.0 (not started) and 1.0 (complete).
func (sg *SnapshotGenerator) GenerateProgress() float64 {
	sg.mu.RLock()
	defer sg.mu.RUnlock()
	return sg.progress
}

// SetProgress sets the generation progress. Values are clamped to [0.0, 1.0].
func (sg *SnapshotGenerator) SetProgress(p float64) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	sg.progress = p
}

// Export returns all accounts sorted by address. Each returned snapshot is a
// copy to prevent mutation of internal state.
func (sg *SnapshotGenerator) Export() []AccountSnapshot {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	result := make([]AccountSnapshot, 0, len(sg.accounts))
	for _, snap := range sg.accounts {
		copied := *snap
		if snap.Balance != nil {
			copied.Balance = new(big.Int).Set(snap.Balance)
		}
		result = append(result, copied)
	}
	// Sort by address for deterministic output.
	sort.Slice(result, func(i, j int) bool {
		return compareAddresses(result[i].Address, result[j].Address) < 0
	})
	return result
}

// compareAddresses does byte-level comparison of two addresses, returning
// -1, 0, or 1.
func compareAddresses(a, b types.Address) int {
	for i := 0; i < types.AddressLength; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// errMaxAccountsReached is returned when trying to add an account beyond the
// configured MaxAccounts limit.
var errMaxAccountsReached = errorf("snapshot generator: max accounts reached")

func errorf(msg string) error {
	return &snapGenError{msg: msg}
}

type snapGenError struct {
	msg string
}

func (e *snapGenError) Error() string {
	return e.msg
}
