// state_object.go provides an exported StateObject type that wraps an account
// with dirty tracking, journal-based revert support, storage slot caching,
// code hash management, and balance/nonce operations with snapshot support.
//
// While the unexported stateObject in memory_statedb.go is the core internal
// representation, StateObject provides a public API for external consumers
// that need to inspect or manipulate individual account state with full
// change-tracking semantics.
package state

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// StateObject is an exported wrapper providing a rich API for manipulating
// a single Ethereum account's state with change tracking and revert support.
// It records all modifications in a changelog that supports snapshot/revert,
// and maintains a cache of storage reads to avoid repeated lookups.
type StateObject struct {
	address  types.Address
	account  types.Account
	code     []byte
	codeHash types.Hash

	// Storage caching layers.
	originStorage map[types.Hash]types.Hash // Original (committed) storage values.
	dirtyStorage  map[types.Hash]types.Hash // Pending storage modifications.
	readCache     map[types.Hash]types.Hash // Cached reads to avoid re-fetching.

	// Dirty flags track what has been modified since last commit.
	dirtyBalance bool
	dirtyNonce   bool
	dirtyCode    bool

	// Lifecycle flags.
	selfDestructed bool
	created        bool

	// Changelog for revert support.
	changelog []stateObjectChange
	snapshots map[int]int // snapshot ID -> changelog index
	nextSnap  int
}

// stateObjectChange records a single reversible modification to the object.
type stateObjectChange struct {
	kind        changeKind
	prevBalance *big.Int
	prevNonce   uint64
	prevCode    []byte
	prevHash    types.Hash
	slotKey     types.Hash
	prevSlot    types.Hash
	prevExists  bool // whether the dirty slot existed before this write
	prevDestruct bool
}

// changeKind identifies the type of state modification.
type changeKind uint8

const (
	changeBalance    changeKind = iota
	changeNonce
	changeCode
	changeStorage
	changeSelfDestruct
)

// NewStateObject creates a new StateObject for the given address with an
// empty account (zero balance, zero nonce, empty code).
func NewStateObject(addr types.Address) *StateObject {
	return &StateObject{
		address:       addr,
		account:       types.NewAccount(),
		codeHash:      types.EmptyCodeHash,
		originStorage: make(map[types.Hash]types.Hash),
		dirtyStorage:  make(map[types.Hash]types.Hash),
		readCache:     make(map[types.Hash]types.Hash),
		snapshots:     make(map[int]int),
	}
}

// NewStateObjectFromAccount creates a StateObject from an existing account.
func NewStateObjectFromAccount(addr types.Address, acct types.Account, code []byte) *StateObject {
	obj := &StateObject{
		address:       addr,
		account:       acct,
		code:          code,
		originStorage: make(map[types.Hash]types.Hash),
		dirtyStorage:  make(map[types.Hash]types.Hash),
		readCache:     make(map[types.Hash]types.Hash),
		snapshots:     make(map[int]int),
	}
	if len(acct.CodeHash) > 0 {
		obj.codeHash = types.BytesToHash(acct.CodeHash)
	} else {
		obj.codeHash = types.EmptyCodeHash
	}
	return obj
}

// Address returns the account address.
func (o *StateObject) Address() types.Address {
	return o.address
}

// Balance returns a copy of the current balance.
func (o *StateObject) Balance() *big.Int {
	if o.account.Balance == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(o.account.Balance)
}

// SetBalance sets the balance, recording the previous value for revert.
func (o *StateObject) SetBalance(amount *big.Int) {
	o.changelog = append(o.changelog, stateObjectChange{
		kind:        changeBalance,
		prevBalance: o.Balance(),
	})
	o.account.Balance = new(big.Int).Set(amount)
	o.dirtyBalance = true
}

// AddBalance adds amount to the balance, recording a journal entry.
func (o *StateObject) AddBalance(amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	o.SetBalance(new(big.Int).Add(o.Balance(), amount))
}

// SubBalance subtracts amount from the balance, recording a journal entry.
// Callers must ensure the balance is sufficient.
func (o *StateObject) SubBalance(amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	o.SetBalance(new(big.Int).Sub(o.Balance(), amount))
}

// Nonce returns the current nonce.
func (o *StateObject) Nonce() uint64 {
	return o.account.Nonce
}

// SetNonce sets the nonce, recording the previous value for revert.
func (o *StateObject) SetNonce(nonce uint64) {
	o.changelog = append(o.changelog, stateObjectChange{
		kind:      changeNonce,
		prevNonce: o.account.Nonce,
	})
	o.account.Nonce = nonce
	o.dirtyNonce = true
}

// Code returns the contract bytecode (nil for EOAs).
func (o *StateObject) Code() []byte {
	return o.code
}

// CodeHash returns the keccak256 hash of the code.
func (o *StateObject) CodeHash() types.Hash {
	return o.codeHash
}

// SetCode sets the contract code, computing and storing the code hash.
// The previous code and hash are recorded for revert.
func (o *StateObject) SetCode(code []byte) {
	prevCode := make([]byte, len(o.code))
	copy(prevCode, o.code)
	o.changelog = append(o.changelog, stateObjectChange{
		kind:     changeCode,
		prevCode: prevCode,
		prevHash: o.codeHash,
	})

	o.code = make([]byte, len(code))
	copy(o.code, code)

	if len(code) == 0 {
		o.codeHash = types.EmptyCodeHash
	} else {
		o.codeHash = types.BytesToHash(crypto.Keccak256(code))
	}
	o.account.CodeHash = o.codeHash.Bytes()
	o.dirtyCode = true
}

// CodeSize returns the length of the contract bytecode.
func (o *StateObject) CodeSize() int {
	return len(o.code)
}

// HasCode returns true if the account has non-empty code.
func (o *StateObject) HasCode() bool {
	return o.codeHash != types.EmptyCodeHash && o.codeHash != (types.Hash{})
}

// --- Storage operations ---

// GetState returns the current value of a storage slot, checking dirty
// storage first, then the read cache, and finally the origin storage.
func (o *StateObject) GetState(key types.Hash) types.Hash {
	// Check dirty storage first (pending writes).
	if val, ok := o.dirtyStorage[key]; ok {
		return val
	}
	// Check read cache.
	if val, ok := o.readCache[key]; ok {
		return val
	}
	// Fall through to origin (committed) storage.
	val := o.originStorage[key]
	o.readCache[key] = val
	return val
}

// GetCommittedState returns the value of a storage slot as last committed,
// bypassing any dirty (pending) modifications.
func (o *StateObject) GetCommittedState(key types.Hash) types.Hash {
	return o.originStorage[key]
}

// SetState writes a value to a storage slot, recording the previous value
// for revert support.
func (o *StateObject) SetState(key, value types.Hash) {
	prevDirty, prevExists := o.dirtyStorage[key]
	var prev types.Hash
	if prevExists {
		prev = prevDirty
	} else {
		prev = o.originStorage[key]
	}

	o.changelog = append(o.changelog, stateObjectChange{
		kind:       changeStorage,
		slotKey:    key,
		prevSlot:   prev,
		prevExists: prevExists,
	})
	o.dirtyStorage[key] = value
}

// SetOriginStorage loads a committed storage value. This is used during
// state loading to populate the origin layer without triggering dirty tracking.
func (o *StateObject) SetOriginStorage(key, value types.Hash) {
	o.originStorage[key] = value
	// Also populate read cache for fast lookups.
	o.readCache[key] = value
}

// DirtyStorageKeys returns all storage keys that have been modified.
func (o *StateObject) DirtyStorageKeys() []types.Hash {
	keys := make([]types.Hash, 0, len(o.dirtyStorage))
	for k := range o.dirtyStorage {
		keys = append(keys, k)
	}
	return keys
}

// --- Self-destruct ---

// MarkSelfDestructed marks the account for deletion at the end of the tx.
// The balance is set to zero. Previous state is recorded for revert.
func (o *StateObject) MarkSelfDestructed() {
	o.changelog = append(o.changelog, stateObjectChange{
		kind:         changeSelfDestruct,
		prevDestruct: o.selfDestructed,
		prevBalance:  o.Balance(),
	})
	o.selfDestructed = true
	o.account.Balance = new(big.Int)
}

// IsSelfDestructed returns true if the account has been marked for deletion.
func (o *StateObject) IsSelfDestructed() bool {
	return o.selfDestructed
}

// --- Snapshot and revert ---

// Snapshot takes a snapshot of the current changelog position, returning
// a snapshot ID that can be passed to RevertToSnapshot.
func (o *StateObject) Snapshot() int {
	id := o.nextSnap
	o.nextSnap++
	o.snapshots[id] = len(o.changelog)
	return id
}

// RevertToSnapshot rolls back all changes made since the given snapshot.
func (o *StateObject) RevertToSnapshot(id int) {
	idx, ok := o.snapshots[id]
	if !ok {
		return
	}

	// Apply reverts in reverse order.
	for i := len(o.changelog) - 1; i >= idx; i-- {
		o.revertChange(o.changelog[i])
	}
	o.changelog = o.changelog[:idx]

	// Invalidate this and all newer snapshots.
	for sid := range o.snapshots {
		if sid >= id {
			delete(o.snapshots, sid)
		}
	}
}

// revertChange undoes a single changelog entry.
func (o *StateObject) revertChange(ch stateObjectChange) {
	switch ch.kind {
	case changeBalance:
		o.account.Balance = ch.prevBalance
	case changeNonce:
		o.account.Nonce = ch.prevNonce
	case changeCode:
		o.code = ch.prevCode
		o.codeHash = ch.prevHash
		o.account.CodeHash = ch.prevHash.Bytes()
	case changeStorage:
		if ch.prevExists {
			o.dirtyStorage[ch.slotKey] = ch.prevSlot
		} else {
			delete(o.dirtyStorage, ch.slotKey)
		}
	case changeSelfDestruct:
		o.selfDestructed = ch.prevDestruct
		o.account.Balance = ch.prevBalance
	}
}

// --- Commit and query ---

// CommitStorage flushes dirty storage into origin storage and clears
// the dirty layer. This should be called at the end of a successful
// transaction or block.
func (o *StateObject) CommitStorage() {
	for key, val := range o.dirtyStorage {
		if val == (types.Hash{}) {
			delete(o.originStorage, key)
			delete(o.readCache, key)
		} else {
			o.originStorage[key] = val
			o.readCache[key] = val
		}
	}
	o.dirtyStorage = make(map[types.Hash]types.Hash)
	o.dirtyBalance = false
	o.dirtyNonce = false
	o.dirtyCode = false
	o.changelog = o.changelog[:0]
	o.snapshots = make(map[int]int)
	o.nextSnap = 0
}

// IsDirty returns true if any field has uncommitted modifications.
func (o *StateObject) IsDirty() bool {
	return o.dirtyBalance || o.dirtyNonce || o.dirtyCode || len(o.dirtyStorage) > 0
}

// IsEmpty returns true if the account has zero nonce, zero balance,
// and empty code hash, per EIP-161.
func (o *StateObject) IsEmpty() bool {
	if o.account.Nonce != 0 {
		return false
	}
	if o.account.Balance != nil && o.account.Balance.Sign() != 0 {
		return false
	}
	return o.codeHash == types.EmptyCodeHash || o.codeHash == (types.Hash{})
}

// IsCreated returns true if this is a newly created account in the
// current block/transaction.
func (o *StateObject) IsCreated() bool {
	return o.created
}

// MarkCreated flags this account as newly created.
func (o *StateObject) MarkCreated() {
	o.created = true
}

// Account returns a copy of the underlying account data.
func (o *StateObject) Account() types.Account {
	acct := types.Account{
		Nonce: o.account.Nonce,
		Root:  o.account.Root,
	}
	if o.account.Balance != nil {
		acct.Balance = new(big.Int).Set(o.account.Balance)
	} else {
		acct.Balance = new(big.Int)
	}
	if len(o.account.CodeHash) > 0 {
		acct.CodeHash = make([]byte, len(o.account.CodeHash))
		copy(acct.CodeHash, o.account.CodeHash)
	}
	return acct
}

// SetStorageRoot sets the account's storage trie root. This is typically
// set after computing the storage trie during state commitment.
func (o *StateObject) SetStorageRoot(root types.Hash) {
	o.account.Root = root
}

// StorageRoot returns the account's current storage trie root.
func (o *StateObject) StorageRoot() types.Hash {
	return o.account.Root
}

// Copy returns a deep copy of the StateObject.
func (o *StateObject) Copy() *StateObject {
	cp := &StateObject{
		address:        o.address,
		codeHash:       o.codeHash,
		selfDestructed: o.selfDestructed,
		created:        o.created,
		dirtyBalance:   o.dirtyBalance,
		dirtyNonce:     o.dirtyNonce,
		dirtyCode:      o.dirtyCode,
		originStorage:  make(map[types.Hash]types.Hash, len(o.originStorage)),
		dirtyStorage:   make(map[types.Hash]types.Hash, len(o.dirtyStorage)),
		readCache:      make(map[types.Hash]types.Hash, len(o.readCache)),
		snapshots:      make(map[int]int),
		nextSnap:       0,
	}

	// Deep copy account.
	cp.account.Nonce = o.account.Nonce
	cp.account.Root = o.account.Root
	if o.account.Balance != nil {
		cp.account.Balance = new(big.Int).Set(o.account.Balance)
	} else {
		cp.account.Balance = new(big.Int)
	}
	if len(o.account.CodeHash) > 0 {
		cp.account.CodeHash = make([]byte, len(o.account.CodeHash))
		copy(cp.account.CodeHash, o.account.CodeHash)
	}

	// Deep copy code.
	if o.code != nil {
		cp.code = make([]byte, len(o.code))
		copy(cp.code, o.code)
	}

	// Deep copy storage maps.
	for k, v := range o.originStorage {
		cp.originStorage[k] = v
	}
	for k, v := range o.dirtyStorage {
		cp.dirtyStorage[k] = v
	}
	for k, v := range o.readCache {
		cp.readCache[k] = v
	}

	return cp
}
