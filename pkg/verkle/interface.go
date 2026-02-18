package verkle

import (
	"github.com/eth2028/eth2028/core/types"
)

// VerkleTree defines the interface for a Verkle tree used as the
// Ethereum state tree (EIP-6800). Implementations may use full
// Pedersen/IPA commitments or placeholder hashes for testing.
//
// Keys and values are variable-length byte slices in the interface,
// but EIP-6800 requires 32-byte keys and 32-byte values. Implementations
// should validate or pad as needed.
type VerkleTree interface {
	// Get retrieves the value stored at the given key.
	// Returns (nil, nil) if the key does not exist.
	Get(key []byte) ([]byte, error)

	// Put stores a value at the given key. Both key and value
	// must be exactly 32 bytes per EIP-6800.
	Put(key []byte, value []byte) error

	// Delete removes the value at the given key. Returns nil
	// if the key did not exist.
	Delete(key []byte) error

	// Commit computes and returns the tree root hash. This is the
	// Verkle state root that goes into the block header.
	Commit() (types.Hash, error)

	// Prove generates an IPA proof for the given key. The proof
	// can demonstrate either inclusion or absence of the key.
	Prove(key []byte) (*VerkleProof, error)
}
