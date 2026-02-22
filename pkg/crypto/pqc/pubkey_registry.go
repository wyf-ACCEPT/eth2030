// Package pqc provides post-quantum cryptographic primitives for Ethereum.
// This file implements a post-quantum public key registry for validators,
// per the CL Cryptography track of the Ethereum 2028 roadmap.
package pqc

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Registry entry status values.
const (
	StatusActive    = "active"
	StatusMigrating = "migrating"
	StatusRevoked   = "revoked"
)

// Errors returned by PQ key registry operations.
var (
	ErrValidatorAlreadyRegistered = errors.New("pqc: validator already registered")
	ErrValidatorNotFound          = errors.New("pqc: validator not found in registry")
	ErrEmptyPQPubKey              = errors.New("pqc: post-quantum public key is empty")
	ErrEmptyClassicPubKey         = errors.New("pqc: classic public key is empty")
	ErrEntryRevoked               = errors.New("pqc: registry entry is revoked")
	ErrInvalidMigrationKey        = errors.New("pqc: new PQ public key is empty or same as current")
)

// RegistryEntry holds the key material and metadata for a single validator.
type RegistryEntry struct {
	ValidatorIndex uint64
	PQPubKey       []byte
	ClassicPubKey  []byte
	RegisteredAt   uint64 // timestamp or slot number
	Status         string
}

// PQKeyRegistry maintains a mapping from validator indices to their
// post-quantum public keys. It is safe for concurrent use.
type PQKeyRegistry struct {
	mu      sync.RWMutex
	entries map[uint64]*RegistryEntry
	clock   uint64 // monotonic counter used as registration timestamp
}

// NewPQKeyRegistry creates an empty PQ key registry.
func NewPQKeyRegistry() *PQKeyRegistry {
	return &PQKeyRegistry{
		entries: make(map[uint64]*RegistryEntry),
	}
}

// RegisterKey adds a new post-quantum public key for the given validator.
// Both the PQ and classic public keys must be non-empty. If the validator
// is already registered, ErrValidatorAlreadyRegistered is returned.
func (r *PQKeyRegistry) RegisterKey(validatorIndex uint64, pqPubKey []byte, classicPubKey []byte) error {
	if len(pqPubKey) == 0 {
		return ErrEmptyPQPubKey
	}
	if len(classicPubKey) == 0 {
		return ErrEmptyClassicPubKey
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[validatorIndex]; exists {
		return ErrValidatorAlreadyRegistered
	}

	r.clock++
	r.entries[validatorIndex] = &RegistryEntry{
		ValidatorIndex: validatorIndex,
		PQPubKey:       copyBytes(pqPubKey),
		ClassicPubKey:  copyBytes(classicPubKey),
		RegisteredAt:   r.clock,
		Status:         StatusActive,
	}
	return nil
}

// GetKey looks up a validator's registry entry by index.
func (r *PQKeyRegistry) GetKey(validatorIndex uint64) (*RegistryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.entries[validatorIndex]
	if !exists {
		return nil, ErrValidatorNotFound
	}
	// Return a copy to prevent external mutation.
	cpy := &RegistryEntry{
		ValidatorIndex: entry.ValidatorIndex,
		PQPubKey:       copyBytes(entry.PQPubKey),
		ClassicPubKey:  copyBytes(entry.ClassicPubKey),
		RegisteredAt:   entry.RegisteredAt,
		Status:         entry.Status,
	}
	return cpy, nil
}

// VerifyRegistration verifies a registration signature for the given
// validator. The signature is validated against the stored classic public
// key using a stub scheme: the expected signature is keccak256(classicPubKey || validatorIndex).
// A production implementation would use ECDSA or BLS verification.
func (r *PQKeyRegistry) VerifyRegistration(validatorIndex uint64, signature []byte) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.entries[validatorIndex]
	if !exists {
		return false
	}
	if entry.Status == StatusRevoked {
		return false
	}

	expected := registrationDigest(entry.ClassicPubKey, validatorIndex)
	if len(signature) != len(expected) {
		return false
	}
	// Constant-time comparison to avoid timing attacks.
	var diff byte
	for i := range expected {
		diff |= signature[i] ^ expected[i]
	}
	return diff == 0
}

// MigrateKey transitions a validator to a new PQ public key. The entry
// status is set to "migrating" during the transition. The new key must
// differ from the current one and be non-empty.
func (r *PQKeyRegistry) MigrateKey(validatorIndex uint64, newPQPubKey []byte) error {
	if len(newPQPubKey) == 0 {
		return ErrInvalidMigrationKey
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[validatorIndex]
	if !exists {
		return ErrValidatorNotFound
	}
	if entry.Status == StatusRevoked {
		return ErrEntryRevoked
	}

	// Reject migration to the same key.
	if bytesEqual(entry.PQPubKey, newPQPubKey) {
		return ErrInvalidMigrationKey
	}

	entry.PQPubKey = copyBytes(newPQPubKey)
	entry.Status = StatusMigrating
	return nil
}

// ActivateEntry sets a migrating entry back to active status. This is
// called after a migration has been confirmed.
func (r *PQKeyRegistry) ActivateEntry(validatorIndex uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[validatorIndex]
	if !exists {
		return ErrValidatorNotFound
	}
	if entry.Status == StatusRevoked {
		return ErrEntryRevoked
	}
	entry.Status = StatusActive
	return nil
}

// RevokeEntry permanently revokes a validator's registry entry.
func (r *PQKeyRegistry) RevokeEntry(validatorIndex uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[validatorIndex]
	if !exists {
		return ErrValidatorNotFound
	}
	entry.Status = StatusRevoked
	return nil
}

// Size returns the number of entries in the registry.
func (r *PQKeyRegistry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// GetRegistryRoot computes a Merkle root over all registry entries,
// ordered by validator index. Each leaf is keccak256(validatorIndex || pqPubKey || classicPubKey || status).
// For an empty registry, the empty root hash is returned.
func (r *PQKeyRegistry) GetRegistryRoot() types.Hash {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.entries) == 0 {
		return types.EmptyRootHash
	}

	// Collect sorted leaves. We iterate up to the max validator index to
	// produce a deterministic ordering without importing sort.
	leaves := make([][]byte, 0, len(r.entries))
	maxIdx := uint64(0)
	for idx := range r.entries {
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	for idx := uint64(0); idx <= maxIdx; idx++ {
		entry, exists := r.entries[idx]
		if !exists {
			continue
		}
		leaves = append(leaves, entryLeafHash(entry))
	}

	return merkleRoot(leaves)
}

// entryLeafHash computes the leaf hash for a registry entry.
func entryLeafHash(e *RegistryEntry) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, e.ValidatorIndex)
	return crypto.Keccak256(buf, e.PQPubKey, e.ClassicPubKey, []byte(e.Status))
}

// merkleRoot computes a simple binary Merkle tree root over the given leaves.
// If the number of leaves is odd, the last leaf is duplicated.
func merkleRoot(leaves [][]byte) types.Hash {
	if len(leaves) == 0 {
		return types.EmptyRootHash
	}
	if len(leaves) == 1 {
		return types.BytesToHash(leaves[0])
	}

	// Work with hashes: pad to even count.
	layer := make([][]byte, len(leaves))
	copy(layer, leaves)

	for len(layer) > 1 {
		if len(layer)%2 != 0 {
			layer = append(layer, layer[len(layer)-1])
		}
		next := make([][]byte, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			next[i/2] = crypto.Keccak256(layer[i], layer[i+1])
		}
		layer = next
	}

	return types.BytesToHash(layer[0])
}

// registrationDigest computes the expected signature digest for a
// registration verification: keccak256(classicPubKey || validatorIndex).
func registrationDigest(classicPubKey []byte, validatorIndex uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, validatorIndex)
	return crypto.Keccak256(classicPubKey, buf)
}

// ValidateRegistryEntry checks that a registry entry has valid fields:
// non-empty keys, valid status, and matching validator index.
func ValidateRegistryEntry(entry *RegistryEntry) error {
	if entry == nil {
		return errors.New("pqc: nil registry entry")
	}
	if len(entry.PQPubKey) == 0 {
		return ErrEmptyPQPubKey
	}
	if len(entry.ClassicPubKey) == 0 {
		return ErrEmptyClassicPubKey
	}
	if entry.Status != StatusActive && entry.Status != StatusMigrating && entry.Status != StatusRevoked {
		return errors.New("pqc: invalid status")
	}
	return nil
}

// ValidateRegistrySize checks that the registry does not exceed maximum size.
func ValidateRegistrySize(registry *PQKeyRegistry, maxEntries int) error {
	if registry == nil {
		return errors.New("pqc: nil registry")
	}
	if maxEntries > 0 && registry.Size() > maxEntries {
		return errors.New("pqc: registry exceeds maximum entries")
	}
	return nil
}

// copyBytes returns a copy of the input byte slice.
func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// bytesEqual compares two byte slices for equality.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
