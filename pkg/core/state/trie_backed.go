package state

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
	"github.com/eth2028/eth2028/rlp"
	"github.com/eth2028/eth2028/trie"
)

// TrieBackedStateDB wraps a MemoryStateDB and adds proper trie-backed state
// root computation via IntermediateRoot. It delegates all state operations to
// the underlying MemoryStateDB and only overrides root computation to build
// real Merkle Patricia Tries from account and storage data.
type TrieBackedStateDB struct {
	*MemoryStateDB
}

// NewTrieBackedStateDB creates a new TrieBackedStateDB wrapping a fresh
// MemoryStateDB.
func NewTrieBackedStateDB() *TrieBackedStateDB {
	return &TrieBackedStateDB{
		MemoryStateDB: NewMemoryStateDB(),
	}
}

// NewTrieBackedFromMemory wraps an existing MemoryStateDB with trie-backed
// root computation.
func NewTrieBackedFromMemory(mem *MemoryStateDB) *TrieBackedStateDB {
	return &TrieBackedStateDB{
		MemoryStateDB: mem,
	}
}

// IntermediateRoot computes the current state root by building real Merkle
// Patricia Tries from all account and storage data.
//
// When deleteEmpty is true (EIP-161), accounts with zero nonce, zero balance,
// and empty code hash are removed from the state before root computation.
//
// Account trie:
//
//	key   = Keccak256(address)
//	value = RLP([nonce, balance, storageRoot, codeHash])
//
// Storage trie (per account):
//
//	key   = Keccak256(slot)
//	value = RLP(value)  -- with leading zeros trimmed
func (s *TrieBackedStateDB) IntermediateRoot(deleteEmpty bool) types.Hash {
	if deleteEmpty {
		s.deleteEmptyAccounts()
	}

	if len(s.stateObjects) == 0 {
		return types.EmptyRootHash
	}

	// Check whether any non-self-destructed accounts remain.
	hasLive := false
	for _, obj := range s.stateObjects {
		if !obj.selfDestructed {
			hasLive = true
			break
		}
	}
	if !hasLive {
		return types.EmptyRootHash
	}

	stateTrie := trie.New()

	for addr, obj := range s.stateObjects {
		if obj.selfDestructed {
			continue
		}

		// Compute storage root for this account.
		storageRoot := computeTrieStorageRoot(obj)

		// Determine code hash; default to empty code hash for EOAs.
		codeHash := obj.account.CodeHash
		if len(codeHash) == 0 {
			codeHash = types.EmptyCodeHash.Bytes()
		}

		// RLP-encode the account: [nonce, balance, storageRoot, codeHash].
		acc := rlpAccount{
			Nonce:    obj.account.Nonce,
			Balance:  obj.account.Balance,
			Root:     storageRoot[:],
			CodeHash: codeHash,
		}
		encoded, err := rlp.EncodeToBytes(acc)
		if err != nil {
			// Should not happen with valid state; skip account on error.
			continue
		}

		// Key is Keccak256(address).
		hashedAddr := crypto.Keccak256(addr[:])
		stateTrie.Put(hashedAddr, encoded)
	}

	return stateTrie.Hash()
}

// deleteEmptyAccounts removes all accounts that are considered empty per
// EIP-161: zero nonce, zero balance, and empty code hash.
func (s *TrieBackedStateDB) deleteEmptyAccounts() {
	for addr, obj := range s.stateObjects {
		if obj.selfDestructed {
			continue
		}
		if isEmptyAccount(obj) {
			delete(s.stateObjects, addr)
		}
	}
}

// isEmptyAccount returns true if the account has zero nonce, zero balance,
// and an empty code hash (or no code hash set).
func isEmptyAccount(obj *stateObject) bool {
	if obj.account.Nonce != 0 {
		return false
	}
	if obj.account.Balance != nil && obj.account.Balance.Sign() != 0 {
		return false
	}
	ch := types.BytesToHash(obj.account.CodeHash)
	return ch == types.EmptyCodeHash || ch == (types.Hash{})
}

// computeTrieStorageRoot builds a storage trie from the account's committed
// and dirty storage, using the proper Ethereum encoding:
//
//	key   = Keccak256(slot)
//	value = RLP(value)  -- with leading zeros trimmed from the uint256
//
// Returns EmptyRootHash if the account has no storage.
//
// Note: This delegates to computeStorageRoot (defined in memory_statedb.go)
// which now uses the same proper Ethereum encoding.
func computeTrieStorageRoot(obj *stateObject) types.Hash {
	return computeStorageRoot(obj)
}

// StorageRoot overrides MemoryStateDB.StorageRoot to use the proper Ethereum
// storage trie encoding: key = Keccak256(slot), value = RLP(trimmedValue).
// Returns EmptyRootHash if the account does not exist or has no storage.
func (s *TrieBackedStateDB) StorageRoot(addr types.Address) types.Hash {
	obj := s.stateObjects[addr]
	if obj == nil {
		return types.EmptyRootHash
	}
	return computeTrieStorageRoot(obj)
}

// GetRoot overrides MemoryStateDB.GetRoot to use the trie-backed
// computation (equivalent to IntermediateRoot(false)).
func (s *TrieBackedStateDB) GetRoot() types.Hash {
	return s.IntermediateRoot(false)
}

// Commit overrides MemoryStateDB.Commit to flush dirty storage and then
// compute the root using the trie-backed path.
func (s *TrieBackedStateDB) Commit() (types.Hash, error) {
	// Flush dirty storage to committed storage.
	for _, obj := range s.stateObjects {
		for key, val := range obj.dirtyStorage {
			if val == (types.Hash{}) {
				delete(obj.committedStorage, key)
			} else {
				obj.committedStorage[key] = val
			}
		}
		obj.dirtyStorage = make(map[types.Hash]types.Hash)
	}

	return s.IntermediateRoot(false), nil
}

// Copy returns a deep copy of the TrieBackedStateDB.
func (s *TrieBackedStateDB) Copy() *TrieBackedStateDB {
	return &TrieBackedStateDB{
		MemoryStateDB: s.MemoryStateDB.Copy(),
	}
}

// Verify interface compliance at compile time.
var _ StateDB = (*TrieBackedStateDB)(nil)

// Verify that TrieBackedStateDB can be used wherever MemoryStateDB is
// expected via embedding. The IntermediateRoot method is additional.
var _ interface {
	IntermediateRoot(deleteEmpty bool) types.Hash
} = (*TrieBackedStateDB)(nil)

// ensureBalance guarantees the account's balance is not nil.
// This is a safety helper used during root computation.
func ensureBalance(obj *stateObject) *big.Int {
	if obj.account.Balance == nil {
		return new(big.Int)
	}
	return obj.account.Balance
}
