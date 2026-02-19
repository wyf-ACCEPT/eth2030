// Package vops implements Validity-Only Partial Statelessness (VOPS) for
// stateless block validation. VOPS allows nodes to validate state transitions
// using only a partial state subset and validity proofs, without storing
// the entire world state.
package vops

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// AccountState represents the state of a single account in a partial state.
type AccountState struct {
	Nonce       uint64
	Balance     *big.Int
	CodeHash    types.Hash
	StorageRoot types.Hash
}

// PartialState holds a subset of the world state needed to execute one or
// more transactions. It contains account data, storage slots, and contract
// code for all accessed state entries.
type PartialState struct {
	Accounts map[types.Address]*AccountState
	Storage  map[types.Address]map[types.Hash]types.Hash
	Code     map[types.Address][]byte
}

// NewPartialState creates an empty partial state.
func NewPartialState() *PartialState {
	return &PartialState{
		Accounts: make(map[types.Address]*AccountState),
		Storage:  make(map[types.Address]map[types.Hash]types.Hash),
		Code:     make(map[types.Address][]byte),
	}
}

// GetAccount returns the account state for addr, or nil if not present.
func (ps *PartialState) GetAccount(addr types.Address) *AccountState {
	return ps.Accounts[addr]
}

// SetAccount sets the account state for addr.
func (ps *PartialState) SetAccount(addr types.Address, acct *AccountState) {
	ps.Accounts[addr] = acct
}

// GetStorage returns the value of a storage slot, or zero hash if absent.
func (ps *PartialState) GetStorage(addr types.Address, key types.Hash) types.Hash {
	if slots, ok := ps.Storage[addr]; ok {
		return slots[key]
	}
	return types.Hash{}
}

// SetStorage stores a value for a storage slot.
func (ps *PartialState) SetStorage(addr types.Address, key, value types.Hash) {
	if _, ok := ps.Storage[addr]; !ok {
		ps.Storage[addr] = make(map[types.Hash]types.Hash)
	}
	ps.Storage[addr][key] = value
}

// ValidityProof attests that a state transition from PreStateRoot to
// PostStateRoot is valid for the given set of accessed keys.
type ValidityProof struct {
	PreStateRoot  types.Hash
	PostStateRoot types.Hash
	AccessedKeys  [][]byte
	ProofData     []byte
}

// ExecutionResult contains the outcome of executing a transaction against
// partial state.
type ExecutionResult struct {
	GasUsed      uint64
	Success      bool
	AccessedKeys [][]byte
	PostState    *PartialState
}

// VOPSConfig holds configuration for the VOPS partial executor.
type VOPSConfig struct {
	// MaxStateSize limits the number of state entries in a partial state.
	MaxStateSize int
}

// DefaultVOPSConfig returns a VOPSConfig with sensible defaults.
func DefaultVOPSConfig() VOPSConfig {
	return VOPSConfig{
		MaxStateSize: 10000,
	}
}
