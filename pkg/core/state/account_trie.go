// account_trie.go provides AccountTrieDB, which wraps a trie implementation
// to store and retrieve Ethereum account state. It handles RLP encoding of
// accounts and storage slots, key hashing via Keccak256, and per-account
// storage trie management.
package state

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
	"github.com/eth2028/eth2028/rlp"
)

// Errors returned by AccountTrieDB.
var (
	ErrAccountNotFound = errors.New("account trie: account not found")
	ErrStorageNotFound = errors.New("account trie: storage key not found")
	ErrTrieUpdate      = errors.New("account trie: trie update failed")
)

// TrieInterface defines the minimal trie operations needed by AccountTrieDB.
// Any trie implementation that supports Get, Update, Delete, and Hash can be
// plugged in (e.g., Merkle Patricia Trie, Verkle trie).
type TrieInterface interface {
	// Get retrieves the value for the given key. Returns a non-nil error
	// if the key is not found.
	Get(key []byte) ([]byte, error)

	// Update inserts or updates a key-value pair. If value is nil or empty,
	// the key should be deleted.
	Update(key, value []byte) error

	// Delete removes the key from the trie.
	Delete(key []byte) error

	// Hash returns the root hash of the trie in its current state.
	Hash() [32]byte
}

// trieRLPAccount is the RLP-serializable representation of an Ethereum
// account as stored in the state trie: [nonce, balance, storageRoot, codeHash].
type trieRLPAccount struct {
	Nonce    uint64
	Balance  *big.Int
	Root     []byte // 32 bytes: storage trie root
	CodeHash []byte // 32 bytes: keccak256 of code
}

// AccountTrieDB wraps a trie to provide account-level state operations.
// It manages a top-level account trie and per-account storage tries.
type AccountTrieDB struct {
	accountTrie  TrieInterface
	storageTries map[types.Address]TrieInterface

	// newTrieFn creates a new empty trie for storage. If nil, storage
	// operations will return errors.
	newTrieFn func() TrieInterface
}

// NewAccountTrieDB creates an AccountTrieDB using the given trie for the
// top-level account trie. Storage tries are created on demand using
// newTrieFn. If newTrieFn is nil, storage operations are not supported.
func NewAccountTrieDB(trie TrieInterface, newTrieFn func() TrieInterface) *AccountTrieDB {
	return &AccountTrieDB{
		accountTrie:  trie,
		storageTries: make(map[types.Address]TrieInterface),
		newTrieFn:    newTrieFn,
	}
}

// GetAccount retrieves and decodes the account at the given address.
// The trie key is Keccak256(address). Returns ErrAccountNotFound if
// the account does not exist.
func (db *AccountTrieDB) GetAccount(address [20]byte) (*types.Account, error) {
	key := crypto.Keccak256(address[:])
	data, err := db.accountTrie.Get(key)
	if err != nil {
		return nil, ErrAccountNotFound
	}

	acc, err := decodeTrieAccount(data)
	if err != nil {
		return nil, fmt.Errorf("account trie: decode account: %w", err)
	}
	return acc, nil
}

// UpdateAccount RLP-encodes the account and stores it in the trie.
// The trie key is Keccak256(address).
func (db *AccountTrieDB) UpdateAccount(address [20]byte, account *types.Account) error {
	if account == nil {
		return db.DeleteAccount(address)
	}

	encoded, err := encodeTrieAccount(account)
	if err != nil {
		return fmt.Errorf("account trie: encode account: %w", err)
	}

	key := crypto.Keccak256(address[:])
	if err := db.accountTrie.Update(key, encoded); err != nil {
		return fmt.Errorf("%w: %v", ErrTrieUpdate, err)
	}
	return nil
}

// DeleteAccount removes the account from the trie and discards its
// associated storage trie.
func (db *AccountTrieDB) DeleteAccount(address [20]byte) error {
	key := crypto.Keccak256(address[:])
	if err := db.accountTrie.Delete(key); err != nil {
		return fmt.Errorf("account trie: delete: %w", err)
	}
	addr := types.BytesToAddress(address[:])
	delete(db.storageTries, addr)
	return nil
}

// GetStorage retrieves a storage value from the account's storage trie.
// The storage trie key is Keccak256(slot). The returned value is the
// raw 32-byte storage value. Returns ErrStorageNotFound if the key does
// not exist.
func (db *AccountTrieDB) GetStorage(address [20]byte, key [32]byte) ([32]byte, error) {
	addr := types.BytesToAddress(address[:])
	st, ok := db.storageTries[addr]
	if !ok {
		return [32]byte{}, ErrStorageNotFound
	}

	hashedKey := crypto.Keccak256(key[:])
	data, err := st.Get(hashedKey)
	if err != nil {
		return [32]byte{}, ErrStorageNotFound
	}

	// Decode the RLP-encoded value.
	val, err := decodeStorageValue(data)
	if err != nil {
		return [32]byte{}, fmt.Errorf("account trie: decode storage: %w", err)
	}
	return val, nil
}

// SetStorage sets a value in the account's storage trie. If the value is
// all zeros, the key is deleted. Creates a new storage trie on demand.
func (db *AccountTrieDB) SetStorage(address [20]byte, key, value [32]byte) error {
	addr := types.BytesToAddress(address[:])
	st, ok := db.storageTries[addr]
	if !ok {
		if db.newTrieFn == nil {
			return errors.New("account trie: no trie factory for storage")
		}
		st = db.newTrieFn()
		db.storageTries[addr] = st
	}

	hashedKey := crypto.Keccak256(key[:])

	// Zero value means delete.
	if value == ([32]byte{}) {
		return st.Delete(hashedKey)
	}

	// RLP-encode the value with leading zeros trimmed.
	encoded, err := encodeStorageValue(value)
	if err != nil {
		return fmt.Errorf("account trie: encode storage: %w", err)
	}

	return st.Update(hashedKey, encoded)
}

// Root returns the current root hash of the account trie.
func (db *AccountTrieDB) Root() [32]byte {
	return db.accountTrie.Hash()
}

// Commit flushes all storage trie changes, updates account storage roots
// in the account trie, and returns the final root hash. After commit,
// the AccountTrieDB continues to be usable.
func (db *AccountTrieDB) Commit() ([32]byte, error) {
	// For each account with a storage trie, update the account's storage root.
	for addr, st := range db.storageTries {
		storageRoot := st.Hash()

		// Retrieve the current account.
		var addrBytes [20]byte
		copy(addrBytes[:], addr[:])
		acc, err := db.GetAccount(addrBytes)
		if err != nil {
			// Account may have been deleted; skip.
			continue
		}

		acc.Root = types.BytesToHash(storageRoot[:])
		if err := db.UpdateAccount(addrBytes, acc); err != nil {
			return [32]byte{}, fmt.Errorf("account trie: commit storage root: %w", err)
		}
	}

	return db.accountTrie.Hash(), nil
}

// StorageRoot returns the current root hash of the storage trie for the
// given address. Returns EmptyRootHash if no storage trie exists.
func (db *AccountTrieDB) StorageRoot(address [20]byte) [32]byte {
	addr := types.BytesToAddress(address[:])
	st, ok := db.storageTries[addr]
	if !ok {
		return types.EmptyRootHash
	}
	return st.Hash()
}

// --- RLP encoding/decoding helpers ---

// encodeTrieAccount RLP-encodes an account as [nonce, balance, root, codeHash].
func encodeTrieAccount(acc *types.Account) ([]byte, error) {
	balance := acc.Balance
	if balance == nil {
		balance = new(big.Int)
	}

	codeHash := acc.CodeHash
	if len(codeHash) == 0 {
		codeHash = types.EmptyCodeHash.Bytes()
	}

	ra := trieRLPAccount{
		Nonce:    acc.Nonce,
		Balance:  balance,
		Root:     acc.Root[:],
		CodeHash: codeHash,
	}
	return rlp.EncodeToBytes(ra)
}

// decodeTrieAccount decodes an RLP-encoded account.
func decodeTrieAccount(data []byte) (*types.Account, error) {
	s := rlp.NewStreamFromBytes(data)

	if _, err := s.List(); err != nil {
		return nil, fmt.Errorf("decode outer list: %w", err)
	}

	nonce, err := s.Uint64()
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}

	balBytes, err := s.Bytes()
	if err != nil {
		return nil, fmt.Errorf("decode balance: %w", err)
	}
	balance := new(big.Int).SetBytes(balBytes)

	rootBytes, err := s.Bytes()
	if err != nil {
		return nil, fmt.Errorf("decode root: %w", err)
	}

	codeHashBytes, err := s.Bytes()
	if err != nil {
		return nil, fmt.Errorf("decode code hash: %w", err)
	}

	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("decode list end: %w", err)
	}

	acc := &types.Account{
		Nonce:    nonce,
		Balance:  balance,
		Root:     types.BytesToHash(rootBytes),
		CodeHash: codeHashBytes,
	}
	return acc, nil
}

// encodeStorageValue RLP-encodes a storage value with leading zeros trimmed.
func encodeStorageValue(val [32]byte) ([]byte, error) {
	trimmed := trieTrimeLeadingZeros(val[:])
	return rlp.EncodeToBytes(trimmed)
}

// decodeStorageValue decodes an RLP-encoded storage value into a 32-byte
// array, right-aligning the decoded bytes.
func decodeStorageValue(data []byte) ([32]byte, error) {
	s := rlp.NewStreamFromBytes(data)
	b, err := s.Bytes()
	if err != nil {
		return [32]byte{}, err
	}

	var result [32]byte
	if len(b) > 32 {
		return [32]byte{}, errors.New("storage value too large")
	}
	copy(result[32-len(b):], b)
	return result, nil
}

// trieTrimeLeadingZeros strips leading zero bytes from a byte slice.
func trieTrimeLeadingZeros(b []byte) []byte {
	for i, v := range b {
		if v != 0 {
			return b[i:]
		}
	}
	return []byte{}
}
