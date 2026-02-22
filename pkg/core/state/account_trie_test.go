package state

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// mockTrie implements TrieInterface for testing with a simple map.
type mockTrie struct {
	data map[string][]byte
}

func newMockTrie() *mockTrie {
	return &mockTrie{data: make(map[string][]byte)}
}

func (m *mockTrie) Get(key []byte) ([]byte, error) {
	v, ok := m.data[string(key)]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (m *mockTrie) Update(key, value []byte) error {
	if len(value) == 0 {
		return m.Delete(key)
	}
	m.data[string(key)] = value
	return nil
}

func (m *mockTrie) Delete(key []byte) error {
	delete(m.data, string(key))
	return nil
}

func (m *mockTrie) Hash() [32]byte {
	// Deterministic but trivial hash for tests: count of entries.
	var h [32]byte
	h[31] = byte(len(m.data))
	return h
}

func newTestAccountTrieDB() *AccountTrieDB {
	return NewAccountTrieDB(newMockTrie(), func() TrieInterface {
		return newMockTrie()
	})
}

func TestAccountTrieDB_GetUpdateDelete(t *testing.T) {
	db := newTestAccountTrieDB()

	addr := types.HexToAddress("0x1234")
	var addrBytes [20]byte
	copy(addrBytes[:], addr[:])

	// Account should not exist initially.
	_, err := db.GetAccount(addrBytes)
	if err != ErrAccountNotFound {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}

	// Create and store account.
	acc := &types.Account{
		Nonce:    10,
		Balance:  big.NewInt(1000),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash.Bytes(),
	}

	if err := db.UpdateAccount(addrBytes, acc); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}

	// Retrieve account.
	got, err := db.GetAccount(addrBytes)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}

	if got.Nonce != 10 {
		t.Errorf("nonce = %d, want 10", got.Nonce)
	}
	if got.Balance.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("balance = %v, want 1000", got.Balance)
	}

	// Delete account.
	if err := db.DeleteAccount(addrBytes); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}

	_, err = db.GetAccount(addrBytes)
	if err != ErrAccountNotFound {
		t.Fatalf("expected ErrAccountNotFound after delete, got %v", err)
	}
}

func TestAccountTrieDB_NilAccountDelete(t *testing.T) {
	db := newTestAccountTrieDB()
	var addrBytes [20]byte
	addrBytes[19] = 0x01

	// UpdateAccount with nil should delete.
	err := db.UpdateAccount(addrBytes, nil)
	if err != nil {
		t.Fatalf("UpdateAccount(nil): %v", err)
	}
}

func TestAccountTrieDB_Storage(t *testing.T) {
	db := newTestAccountTrieDB()

	addr := types.HexToAddress("0xabcd")
	var addrBytes [20]byte
	copy(addrBytes[:], addr[:])

	key := types.HexToHash("0x01")
	var keyBytes [32]byte
	copy(keyBytes[:], key[:])

	val := types.HexToHash("0xff")
	var valBytes [32]byte
	copy(valBytes[:], val[:])

	// Set storage.
	if err := db.SetStorage(addrBytes, keyBytes, valBytes); err != nil {
		t.Fatalf("SetStorage: %v", err)
	}

	// Get storage.
	got, err := db.GetStorage(addrBytes, keyBytes)
	if err != nil {
		t.Fatalf("GetStorage: %v", err)
	}
	if got != valBytes {
		t.Errorf("storage value mismatch: got %x, want %x", got, valBytes)
	}

	// Delete storage (set to zero).
	var zero [32]byte
	if err := db.SetStorage(addrBytes, keyBytes, zero); err != nil {
		t.Fatalf("SetStorage(zero): %v", err)
	}

	_, err = db.GetStorage(addrBytes, keyBytes)
	if err != ErrStorageNotFound {
		t.Fatalf("expected ErrStorageNotFound after delete, got %v", err)
	}
}

func TestAccountTrieDB_StorageNotFound(t *testing.T) {
	db := newTestAccountTrieDB()
	var addrBytes [20]byte
	var keyBytes [32]byte
	keyBytes[31] = 1

	_, err := db.GetStorage(addrBytes, keyBytes)
	if err != ErrStorageNotFound {
		t.Fatalf("expected ErrStorageNotFound, got %v", err)
	}
}

func TestAccountTrieDB_Root(t *testing.T) {
	db := newTestAccountTrieDB()

	root1 := db.Root()

	// Add an account.
	var addrBytes [20]byte
	addrBytes[19] = 0x01
	acc := &types.Account{
		Nonce:    1,
		Balance:  big.NewInt(100),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash.Bytes(),
	}
	if err := db.UpdateAccount(addrBytes, acc); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}

	root2 := db.Root()
	if root1 == root2 {
		t.Fatal("root should change after inserting an account")
	}
}

func TestAccountTrieDB_Commit(t *testing.T) {
	db := newTestAccountTrieDB()

	addr := types.HexToAddress("0x1234")
	var addrBytes [20]byte
	copy(addrBytes[:], addr[:])

	// Create account.
	acc := &types.Account{
		Nonce:    1,
		Balance:  big.NewInt(500),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash.Bytes(),
	}
	if err := db.UpdateAccount(addrBytes, acc); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}

	// Set some storage.
	var key, val [32]byte
	key[31] = 0x01
	val[31] = 0x42
	if err := db.SetStorage(addrBytes, key, val); err != nil {
		t.Fatalf("SetStorage: %v", err)
	}

	// Commit.
	root, err := db.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Root should be non-zero.
	if root == ([32]byte{}) {
		t.Fatal("commit root should not be zero")
	}

	// Verify the account's storage root was updated.
	gotAcc, err := db.GetAccount(addrBytes)
	if err != nil {
		t.Fatalf("GetAccount after commit: %v", err)
	}
	storageRoot := db.StorageRoot(addrBytes)
	if gotAcc.Root != types.BytesToHash(storageRoot[:]) {
		t.Errorf("account storage root mismatch after commit: %x != %x",
			gotAcc.Root, storageRoot)
	}
}

func TestAccountTrieDB_StorageRoot(t *testing.T) {
	db := newTestAccountTrieDB()

	var addrBytes [20]byte
	addrBytes[19] = 0x99

	// No storage trie yet: should return EmptyRootHash.
	root := db.StorageRoot(addrBytes)
	if root != types.EmptyRootHash {
		t.Fatalf("empty storage root = %x, want EmptyRootHash", root)
	}

	// Add storage and check root changes.
	var key, val [32]byte
	key[31] = 0x01
	val[31] = 0x01
	if err := db.SetStorage(addrBytes, key, val); err != nil {
		t.Fatalf("SetStorage: %v", err)
	}

	root2 := db.StorageRoot(addrBytes)
	if root2 == types.EmptyRootHash {
		t.Fatal("storage root should not be EmptyRootHash after setting a value")
	}
}

func TestAccountTrieDB_NoTrieFactory(t *testing.T) {
	// Create without a trie factory.
	db := NewAccountTrieDB(newMockTrie(), nil)

	var addrBytes [20]byte
	var key, val [32]byte
	key[31] = 1
	val[31] = 1

	err := db.SetStorage(addrBytes, key, val)
	if err == nil {
		t.Fatal("expected error when no trie factory, got nil")
	}
}

func TestTrieRLPRoundTrip(t *testing.T) {
	acc := &types.Account{
		Nonce:    42,
		Balance:  big.NewInt(123456789),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash.Bytes(),
	}

	encoded, err := encodeTrieAccount(acc)
	if err != nil {
		t.Fatalf("encodeTrieAccount: %v", err)
	}

	decoded, err := decodeTrieAccount(encoded)
	if err != nil {
		t.Fatalf("decodeTrieAccount: %v", err)
	}

	if decoded.Nonce != acc.Nonce {
		t.Errorf("nonce = %d, want %d", decoded.Nonce, acc.Nonce)
	}
	if decoded.Balance.Cmp(acc.Balance) != 0 {
		t.Errorf("balance = %v, want %v", decoded.Balance, acc.Balance)
	}
	if decoded.Root != acc.Root {
		t.Errorf("root = %x, want %x", decoded.Root, acc.Root)
	}
}

func TestStorageValueRoundTrip(t *testing.T) {
	var val [32]byte
	val[28] = 0x01
	val[29] = 0x02
	val[30] = 0x03
	val[31] = 0x04

	encoded, err := encodeStorageValue(val)
	if err != nil {
		t.Fatalf("encodeStorageValue: %v", err)
	}

	decoded, err := decodeStorageValue(encoded)
	if err != nil {
		t.Fatalf("decodeStorageValue: %v", err)
	}

	if decoded != val {
		t.Errorf("storage round-trip mismatch: got %x, want %x", decoded, val)
	}
}
