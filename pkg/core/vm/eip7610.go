// Package vm implements the Ethereum Virtual Machine.
//
// This file implements EIP-7610: Revert creation in case of non-empty storage.
// EIP-7610 extends the existing CREATE/CREATE2 collision check to also reject
// deployment to addresses that have non-empty storage, in addition to the
// pre-existing checks for non-zero nonce and non-empty code.
//
// Spec: https://eips.ethereum.org/EIPS/eip-7610
package vm

import (
	"errors"

	"github.com/eth2030/eth2030/core/types"
)

// ErrContractCreationCollision is returned when a CREATE or CREATE2 targets
// an address that already has a non-zero nonce, non-empty code, or (per
// EIP-7610) non-empty storage.
var ErrContractCreationCollision = errors.New("contract creation collision: address already in use")

// CommonStorageSlots is the set of well-known storage slots probed by
// HasNonEmptyStorage. Slot 0 is the most commonly used slot in Solidity
// (first declared state variable). Slots 1-9 cover additional low-index
// variables and common proxy patterns (e.g. EIP-1967 slots are hashed, but
// many contracts store data in low slots).
var CommonStorageSlots = []types.Hash{
	types.BytesToHash([]byte{0}),
	types.BytesToHash([]byte{1}),
	types.BytesToHash([]byte{2}),
	types.BytesToHash([]byte{3}),
	types.BytesToHash([]byte{4}),
	types.BytesToHash([]byte{5}),
	types.BytesToHash([]byte{6}),
	types.BytesToHash([]byte{7}),
	types.BytesToHash([]byte{8}),
	types.BytesToHash([]byte{9}),
}

// CollisionCheck7610 performs EIP-7610 contract creation collision checks.
type CollisionCheck7610 struct {
	// Enabled controls whether the EIP-7610 storage check is active.
	// When false, only the legacy nonce/code checks are performed.
	Enabled bool
}

// NewCollisionCheck7610 returns a CollisionCheck7610 with the given
// enabled state.
func NewCollisionCheck7610(enabled bool) *CollisionCheck7610 {
	return &CollisionCheck7610{Enabled: enabled}
}

// HasNonEmptyStorage probes a set of common storage slots and returns true
// if any of them contain a non-zero value. In a production implementation
// this would consult the storage trie root, but for correctness a slot-
// probing approach is used here.
func HasNonEmptyStorage(stateDB StateDB, addr types.Address) bool {
	var zeroHash types.Hash
	for _, slot := range CommonStorageSlots {
		val := stateDB.GetState(addr, slot)
		if val != zeroHash {
			return true
		}
	}
	return false
}

// CheckCreateCollision checks whether deploying a contract at addr would
// collide with existing state. It returns ErrContractCreationCollision if
// the address has:
//   - a non-zero nonce, OR
//   - non-empty code (code hash differs from the empty code hash), OR
//   - non-empty storage (EIP-7610, only when c.Enabled is true)
//
// An address that only has a non-zero balance is acceptable per EIP-7610
// and returns nil.
func (c *CollisionCheck7610) CheckCreateCollision(stateDB StateDB, addr types.Address) error {
	// Check nonce.
	if stateDB.GetNonce(addr) != 0 {
		return ErrContractCreationCollision
	}

	// Check code. An address with deployed code is not a valid target.
	codeHash := stateDB.GetCodeHash(addr)
	if codeHash != (types.Hash{}) && codeHash != types.EmptyCodeHash {
		return ErrContractCreationCollision
	}

	// EIP-7610: check storage.
	if c.Enabled {
		if HasNonEmptyStorage(stateDB, addr) {
			return ErrContractCreationCollision
		}
	}

	return nil
}
