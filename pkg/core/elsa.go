// Package core implements the Exposed ELSA (Execution Layer State Access)
// interface, providing external read-only access to execution layer state
// with proof generation, batch queries, and change subscriptions.
package core

import (
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/trie"
)

var (
	ErrELSAAccountNotFound = errors.New("elsa: account not found")
	ErrELSABatchTooLarge   = errors.New("elsa: batch size exceeds maximum")
	ErrELSAMaxSubscription = errors.New("elsa: max subscriptions reached")
	ErrELSANilState        = errors.New("elsa: state database is nil")
)

// ELSAConfig holds configuration for the ELSA instance.
type ELSAConfig struct {
	MaxBatchSize     int // maximum addresses in a batch query
	MaxSubscriptions int // maximum concurrent subscriptions
	CacheSize        int // number of cached account lookups
}

// DefaultELSAConfig returns sensible defaults.
func DefaultELSAConfig() ELSAConfig {
	return ELSAConfig{
		MaxBatchSize:     1000,
		MaxSubscriptions: 256,
		CacheSize:        4096,
	}
}

// ELSAAccount represents an account as returned by the ELSA interface.
type ELSAAccount struct {
	Address     types.Address
	Balance     *big.Int
	Nonce       uint64
	CodeHash    types.Hash
	StorageRoot types.Hash
}

// ELSAProof contains a Merkle proof for an account and optional storage slots.
type ELSAProof struct {
	Address       types.Address
	AccountProof  [][]byte
	StorageProofs []*StorageProof
	Balance       *big.Int
	Nonce         uint64
	CodeHash      types.Hash
	StorageRoot   types.Hash
}

// StorageProof holds the proof for a single storage slot.
type StorageProof struct {
	Key   types.Hash
	Value types.Hash
	Proof [][]byte
}

// StateChange represents a single state mutation observed via subscription.
type StateChange struct {
	Address     types.Address
	Slot        types.Hash
	OldValue    types.Hash
	NewValue    types.Hash
	BlockNumber uint64
}

// StateChangeSubscription receives state change notifications for an address.
type StateChangeSubscription struct {
	Changes chan *StateChange
	addr    types.Address

	mu     sync.Mutex
	closed bool
}

// Close shuts down the subscription, closing the channel.
func (s *StateChangeSubscription) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.Changes)
	}
}

// IsClosed reports whether the subscription has been closed.
func (s *StateChangeSubscription) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// ELSA provides external read-only access to execution layer state.
// All methods are safe for concurrent use.
type ELSA struct {
	mu     sync.RWMutex
	stateDB *state.MemoryStateDB
	config  ELSAConfig

	// subscriptions maps address -> list of active subscriptions.
	subs     map[types.Address][]*StateChangeSubscription
	subCount int
}

// NewELSA creates a new ELSA instance backed by the given state database.
func NewELSA(config ELSAConfig) *ELSA {
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = DefaultELSAConfig().MaxBatchSize
	}
	if config.MaxSubscriptions <= 0 {
		config.MaxSubscriptions = DefaultELSAConfig().MaxSubscriptions
	}
	if config.CacheSize <= 0 {
		config.CacheSize = DefaultELSAConfig().CacheSize
	}
	return &ELSA{
		stateDB: state.NewMemoryStateDB(),
		config:  config,
		subs:    make(map[types.Address][]*StateChangeSubscription),
	}
}

// SetState replaces the underlying state database. This is used when the
// canonical chain advances and the ELSA needs to point at a new state.
func (e *ELSA) SetState(db *state.MemoryStateDB) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stateDB = db
}

// State returns the underlying state database (for tests/internal use).
func (e *ELSA) State() *state.MemoryStateDB {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.stateDB
}

// GetAccount retrieves the account state for the given address.
func (e *ELSA) GetAccount(addr types.Address) (*ELSAAccount, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.stateDB == nil {
		return nil, ErrELSANilState
	}

	if !e.stateDB.Exist(addr) {
		return nil, ErrELSAAccountNotFound
	}

	return &ELSAAccount{
		Address:     addr,
		Balance:     e.stateDB.GetBalance(addr),
		Nonce:       e.stateDB.GetNonce(addr),
		CodeHash:    e.stateDB.GetCodeHash(addr),
		StorageRoot: e.stateDB.StorageRoot(addr),
	}, nil
}

// GetStorage returns the value stored at the given slot for the address.
func (e *ELSA) GetStorage(addr types.Address, slot types.Hash) (types.Hash, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.stateDB == nil {
		return types.Hash{}, ErrELSANilState
	}
	return e.stateDB.GetState(addr, slot), nil
}

// GetCode returns the contract bytecode for the given address.
func (e *ELSA) GetCode(addr types.Address) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.stateDB == nil {
		return nil, ErrELSANilState
	}
	return e.stateDB.GetCode(addr), nil
}

// GetProof generates a Merkle proof for an account and the specified storage
// slots. The proof can be independently verified using VerifyProof.
func (e *ELSA) GetProof(addr types.Address, slots []types.Hash) (*ELSAProof, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.stateDB == nil {
		return nil, ErrELSANilState
	}

	stateTrie := e.stateDB.BuildStateTrie()
	storageTrie := e.stateDB.BuildStorageTrie(addr)

	ap, err := trie.ProveAccountWithStorage(stateTrie, addr, storageTrie, slots)
	if err != nil {
		return nil, err
	}

	proof := &ELSAProof{
		Address:      addr,
		AccountProof: ap.AccountProof,
		Balance:      ap.Balance,
		Nonce:        ap.Nonce,
		CodeHash:     ap.CodeHash,
		StorageRoot:  ap.StorageHash,
	}

	for _, sp := range ap.StorageProof {
		proof.StorageProofs = append(proof.StorageProofs, &StorageProof{
			Key:   sp.Key,
			Value: types.BytesToHash(sp.Value.Bytes()),
			Proof: sp.Proof,
		})
	}

	return proof, nil
}

// VerifyProof checks whether the given proof is valid against the state root.
// It verifies the account proof and all storage proofs.
func VerifyProof(root types.Hash, proof *ELSAProof) bool {
	if proof == nil {
		return false
	}

	// Verify the account proof against the state root.
	addrHash := crypto.Keccak256(proof.Address[:])
	_, err := trie.VerifyProof(root, addrHash, proof.AccountProof)
	if err != nil {
		return false
	}

	// Verify each storage proof against the storage root.
	for _, sp := range proof.StorageProofs {
		slotHash := crypto.Keccak256(sp.Key[:])
		_, err := trie.VerifyProof(proof.StorageRoot, slotHash, sp.Proof)
		if err != nil {
			return false
		}
	}

	return true
}

// BatchGetAccounts retrieves multiple accounts in a single call.
func (e *ELSA) BatchGetAccounts(addrs []types.Address) ([]*ELSAAccount, error) {
	if len(addrs) > e.config.MaxBatchSize {
		return nil, ErrELSABatchTooLarge
	}

	results := make([]*ELSAAccount, len(addrs))
	for i, addr := range addrs {
		acct, err := e.GetAccount(addr)
		if err != nil {
			// For batch, include nil for missing accounts.
			results[i] = nil
			continue
		}
		results[i] = acct
	}
	return results, nil
}

// SubscribeStateChanges creates a subscription for state changes to the
// given address. Returns a StateChangeSubscription whose Changes channel
// will receive updates.
func (e *ELSA) SubscribeStateChanges(addr types.Address) (*StateChangeSubscription, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.subCount >= e.config.MaxSubscriptions {
		return nil, ErrELSAMaxSubscription
	}

	sub := &StateChangeSubscription{
		Changes: make(chan *StateChange, 64),
		addr:    addr,
	}
	e.subs[addr] = append(e.subs[addr], sub)
	e.subCount++
	return sub, nil
}

// Unsubscribe removes a subscription and closes it.
func (e *ELSA) Unsubscribe(sub *StateChangeSubscription) {
	e.mu.Lock()
	defer e.mu.Unlock()

	sub.Close()

	subs := e.subs[sub.addr]
	for i, s := range subs {
		if s == sub {
			e.subs[sub.addr] = append(subs[:i], subs[i+1:]...)
			e.subCount--
			break
		}
	}
}

// NotifyStateChange dispatches a state change to all subscriptions watching
// the affected address. Non-blocking: drops the change if a subscriber's
// channel is full.
func (e *ELSA) NotifyStateChange(change *StateChange) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	subs := e.subs[change.Address]
	for _, sub := range subs {
		sub.mu.Lock()
		if !sub.closed {
			select {
			case sub.Changes <- change:
			default:
				// Drop if channel full to avoid blocking.
			}
		}
		sub.mu.Unlock()
	}
}

// SubscriptionCount returns the number of active subscriptions.
func (e *ELSA) SubscriptionCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.subCount
}
