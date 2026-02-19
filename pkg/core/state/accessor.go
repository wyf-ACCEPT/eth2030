// accessor.go implements a read-only state accessor for historical state
// queries and a StateDiff type for tracking state changes between blocks.
// The HistoricalAccessor provides a lightweight, immutable view of state
// at a specific block number, useful for serving historical RPC queries
// without requiring a full StateDB.
package state

import (
	"math/big"
	"sort"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// StateAccessor provides read-only access to account state at a point in time.
type StateAccessor interface {
	GetBalance(addr types.Address) *big.Int
	GetNonce(addr types.Address) uint64
	GetCode(addr types.Address) []byte
	GetCodeHash(addr types.Address) types.Hash
	GetStorageAt(addr types.Address, key types.Hash) types.Hash
	Exist(addr types.Address) bool
}

// historicalAccount stores the state of a single account.
type historicalAccount struct {
	balance *big.Int
	nonce   uint64
	code    []byte
	storage map[types.Hash]types.Hash
}

// HistoricalAccessor is a read-only state accessor backed by in-memory
// account data at a specific block number. It implements StateAccessor.
type HistoricalAccessor struct {
	blockNumber uint64
	accounts    map[types.Address]*historicalAccount
}

// NewHistoricalAccessor creates a new HistoricalAccessor for the given block.
func NewHistoricalAccessor(blockNumber uint64) *HistoricalAccessor {
	return &HistoricalAccessor{
		blockNumber: blockNumber,
		accounts:    make(map[types.Address]*historicalAccount),
	}
}

// SetAccount adds or updates an account in the accessor. The balance is
// deep-copied, and the code is copied to prevent aliasing.
func (ha *HistoricalAccessor) SetAccount(addr types.Address, balance *big.Int, nonce uint64, code []byte) {
	acct, ok := ha.accounts[addr]
	if !ok {
		acct = &historicalAccount{
			storage: make(map[types.Hash]types.Hash),
		}
		ha.accounts[addr] = acct
	}

	if balance != nil {
		acct.balance = new(big.Int).Set(balance)
	} else {
		acct.balance = new(big.Int)
	}

	acct.nonce = nonce

	if code != nil {
		acct.code = make([]byte, len(code))
		copy(acct.code, code)
	} else {
		acct.code = nil
	}
}

// SetStorage sets a storage value for an account. If the account does not
// exist, it is created with zero balance and nonce.
func (ha *HistoricalAccessor) SetStorage(addr types.Address, key types.Hash, value types.Hash) {
	acct, ok := ha.accounts[addr]
	if !ok {
		acct = &historicalAccount{
			balance: new(big.Int),
			storage: make(map[types.Hash]types.Hash),
		}
		ha.accounts[addr] = acct
	}
	acct.storage[key] = value
}

// BlockNumber returns the block number this accessor represents.
func (ha *HistoricalAccessor) BlockNumber() uint64 {
	return ha.blockNumber
}

// AccountCount returns the number of accounts in the accessor.
func (ha *HistoricalAccessor) AccountCount() int {
	return len(ha.accounts)
}

// StorageCount returns the number of storage slots for a given account.
// Returns 0 if the account does not exist.
func (ha *HistoricalAccessor) StorageCount(addr types.Address) int {
	acct, ok := ha.accounts[addr]
	if !ok {
		return 0
	}
	return len(acct.storage)
}

// GetBalance returns the balance of the given address. Returns zero if
// the account does not exist.
func (ha *HistoricalAccessor) GetBalance(addr types.Address) *big.Int {
	acct, ok := ha.accounts[addr]
	if !ok {
		return new(big.Int)
	}
	return new(big.Int).Set(acct.balance)
}

// GetNonce returns the nonce of the given address. Returns 0 if the
// account does not exist.
func (ha *HistoricalAccessor) GetNonce(addr types.Address) uint64 {
	acct, ok := ha.accounts[addr]
	if !ok {
		return 0
	}
	return acct.nonce
}

// GetCode returns the bytecode of the given address. Returns nil if the
// account does not exist or has no code.
func (ha *HistoricalAccessor) GetCode(addr types.Address) []byte {
	acct, ok := ha.accounts[addr]
	if !ok {
		return nil
	}
	return acct.code
}

// GetCodeHash returns the Keccak256 hash of the account's code. Returns
// EmptyCodeHash if the account has no code, or a zero hash if the account
// does not exist.
func (ha *HistoricalAccessor) GetCodeHash(addr types.Address) types.Hash {
	acct, ok := ha.accounts[addr]
	if !ok {
		return types.Hash{}
	}
	if len(acct.code) == 0 {
		return types.EmptyCodeHash
	}
	return types.BytesToHash(crypto.Keccak256(acct.code))
}

// GetStorageAt returns the storage value at the given key for the address.
// Returns the zero hash if the account or key does not exist.
func (ha *HistoricalAccessor) GetStorageAt(addr types.Address, key types.Hash) types.Hash {
	acct, ok := ha.accounts[addr]
	if !ok {
		return types.Hash{}
	}
	return acct.storage[key]
}

// Exist returns true if the given address has an account in this accessor.
func (ha *HistoricalAccessor) Exist(addr types.Address) bool {
	_, ok := ha.accounts[addr]
	return ok
}

// Verify interface compliance at compile time.
var _ StateAccessor = (*HistoricalAccessor)(nil)

// StateChange records a single field change on an account.
type StateChange struct {
	Address types.Address
	Field   string      // "balance", "nonce", or "storage:<key>"
	Before  interface{} // Value before the change.
	After   interface{} // Value after the change.
}

// StateDiff accumulates state changes between two blocks. It can be applied
// to a HistoricalAccessor to advance it from one block to the next.
type StateDiff struct {
	changes []StateChange
}

// NewStateDiff creates a new empty StateDiff.
func NewStateDiff() *StateDiff {
	return &StateDiff{}
}

// AddBalanceChange records a balance change for an address.
func (sd *StateDiff) AddBalanceChange(addr types.Address, before, after *big.Int) {
	sd.changes = append(sd.changes, StateChange{
		Address: addr,
		Field:   "balance",
		Before:  new(big.Int).Set(before),
		After:   new(big.Int).Set(after),
	})
}

// AddNonceChange records a nonce change for an address.
func (sd *StateDiff) AddNonceChange(addr types.Address, before, after uint64) {
	sd.changes = append(sd.changes, StateChange{
		Address: addr,
		Field:   "nonce",
		Before:  before,
		After:   after,
	})
}

// AddStorageChange records a storage slot change for an address.
func (sd *StateDiff) AddStorageChange(addr types.Address, key types.Hash, before, after types.Hash) {
	sd.changes = append(sd.changes, StateChange{
		Address: addr,
		Field:   "storage:" + key.Hex(),
		Before:  before,
		After:   after,
	})
}

// Changes returns all recorded state changes, sorted by address then field.
func (sd *StateDiff) Changes() []StateChange {
	sorted := make([]StateChange, len(sd.changes))
	copy(sorted, sd.changes)

	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Address != sorted[j].Address {
			return sorted[i].Address.Hex() < sorted[j].Address.Hex()
		}
		return sorted[i].Field < sorted[j].Field
	})

	return sorted
}

// Apply applies the diff to a HistoricalAccessor, setting each account's
// field to the "after" value. Accounts that do not exist in the accessor
// are created as needed.
func (sd *StateDiff) Apply(accessor *HistoricalAccessor) {
	for _, change := range sd.changes {
		// Ensure the account exists.
		if !accessor.Exist(change.Address) {
			accessor.SetAccount(change.Address, new(big.Int), 0, nil)
		}

		switch change.Field {
		case "balance":
			bal, ok := change.After.(*big.Int)
			if !ok {
				continue
			}
			acct := accessor.accounts[change.Address]
			acct.balance = new(big.Int).Set(bal)

		case "nonce":
			nonce, ok := change.After.(uint64)
			if !ok {
				continue
			}
			acct := accessor.accounts[change.Address]
			acct.nonce = nonce

		default:
			// Storage change: field is "storage:<key hex>".
			if len(change.Field) > 8 && change.Field[:8] == "storage:" {
				val, ok := change.After.(types.Hash)
				if !ok {
					continue
				}
				keyHex := change.Field[8:]
				storageKey := types.HexToHash(keyHex)
				acct := accessor.accounts[change.Address]
				if acct.storage == nil {
					acct.storage = make(map[types.Hash]types.Hash)
				}
				acct.storage[storageKey] = val
			}
		}
	}
}
