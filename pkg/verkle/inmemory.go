package verkle

import (
	"errors"
	"fmt"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Compile-time interface check.
var _ VerkleTree = (*InMemoryVerkleTree)(nil)

// InMemoryVerkleTree is a VerkleTree implementation backed by the
// existing Tree structure. It wraps the fixed-size key/value Tree
// methods behind the byte-slice VerkleTree interface and uses
// Keccak256 as a placeholder for Pedersen commitment hashing.
type InMemoryVerkleTree struct {
	tree *Tree
}

// NewInMemoryVerkleTree creates a new empty in-memory Verkle tree.
func NewInMemoryVerkleTree() *InMemoryVerkleTree {
	return &InMemoryVerkleTree{
		tree: NewTree(),
	}
}

var (
	errInvalidKeySize   = errors.New("verkle: key must be 32 bytes")
	errInvalidValueSize = errors.New("verkle: value must be 32 bytes")
)

// validateKey checks that the key is exactly KeySize bytes.
func validateKey(key []byte) ([KeySize]byte, error) {
	if len(key) != KeySize {
		return [KeySize]byte{}, fmt.Errorf("%w (got %d)", errInvalidKeySize, len(key))
	}
	var k [KeySize]byte
	copy(k[:], key)
	return k, nil
}

// validateValue checks that the value is exactly ValueSize bytes.
func validateValue(value []byte) ([ValueSize]byte, error) {
	if len(value) != ValueSize {
		return [ValueSize]byte{}, fmt.Errorf("%w (got %d)", errInvalidValueSize, len(value))
	}
	var v [ValueSize]byte
	copy(v[:], value)
	return v, nil
}

// Get retrieves the value stored at the given 32-byte key.
// Returns (nil, nil) if the key does not exist.
func (t *InMemoryVerkleTree) Get(key []byte) ([]byte, error) {
	k, err := validateKey(key)
	if err != nil {
		return nil, err
	}
	val, err := t.tree.Get(k)
	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, nil
	}
	result := make([]byte, ValueSize)
	copy(result, val[:])
	return result, nil
}

// Put stores a 32-byte value at the given 32-byte key.
func (t *InMemoryVerkleTree) Put(key []byte, value []byte) error {
	k, err := validateKey(key)
	if err != nil {
		return err
	}
	v, err := validateValue(value)
	if err != nil {
		return err
	}
	return t.tree.Put(k, v)
}

// Delete removes the value at the given 32-byte key.
func (t *InMemoryVerkleTree) Delete(key []byte) error {
	k, err := validateKey(key)
	if err != nil {
		return err
	}
	return t.tree.Delete(k)
}

// Commit computes and returns the tree root hash.
// Uses Keccak256 of the commitment bytes as the hash (placeholder
// for actual Pedersen vector commitment).
func (t *InMemoryVerkleTree) Commit() (types.Hash, error) {
	c := t.tree.RootCommitment()
	// Hash the commitment with Keccak256 to produce the state root.
	h := crypto.Keccak256Hash(c[:])
	return h, nil
}

// Prove generates an IPA proof for the given key.
// This is a stub implementation that constructs the proof structure
// by traversing the tree and collecting commitments along the path.
// In production, this would compute a real IPA multipoint argument.
func (t *InMemoryVerkleTree) Prove(key []byte) (*VerkleProof, error) {
	k, err := validateKey(key)
	if err != nil {
		return nil, err
	}

	elements := t.collectProofElements(k)
	return t.buildProof(k, elements), nil
}

// collectProofElements traverses the tree along the key path and
// collects commitments for the proof.
func (t *InMemoryVerkleTree) collectProofElements(key [KeySize]byte) ProofElements {
	stem, suffix := splitKey(key)
	elements := ProofElements{
		PathCommitments: []Commitment{t.tree.root.Commit()},
	}

	node := t.tree.root
	for depth := 0; depth < StemSize; depth++ {
		child := node.Child(stem[depth])
		if child == nil {
			elements.Depth = uint8(depth)
			return elements
		}

		switch c := child.(type) {
		case *LeafNode:
			elements.Depth = uint8(depth + 1)
			if c.stem == stem {
				elements.Leaf = c
				elements.PathCommitments = append(elements.PathCommitments, c.Commit())
			}
			return elements

		case *InternalNode:
			elements.PathCommitments = append(elements.PathCommitments, c.Commit())
			node = c

		default:
			elements.Depth = uint8(depth)
			return elements
		}
	}

	// At maximum depth, check for leaf.
	_ = suffix
	elements.Depth = uint8(StemSize)
	return elements
}

// buildProof constructs a VerkleProof from collected path elements.
// The IPA proof bytes are a placeholder (Keccak256 of the path).
func (t *InMemoryVerkleTree) buildProof(key [KeySize]byte, elements ProofElements) *VerkleProof {
	proof := &VerkleProof{
		CommitmentsByPath: elements.PathCommitments,
		Depth:             elements.Depth,
		Key:               key,
	}

	if elements.Leaf != nil {
		proof.ExtensionPresent = true
		_, suffix := splitKey(key)
		val := elements.Leaf.Get(suffix)
		if val != nil {
			v := *val
			proof.Value = &v
		}
		proof.D = elements.Leaf.Commit()
	}

	// Build placeholder IPA proof: hash all path commitments.
	// In production this would be a real IPA multipoint argument
	// containing L/R curve point pairs and the final scalar.
	var ipaInput []byte
	for _, c := range elements.PathCommitments {
		ipaInput = append(ipaInput, c[:]...)
	}
	ipaInput = append(ipaInput, key[:]...)
	proof.IPAProof = crypto.Keccak256(ipaInput)

	return proof
}

// InnerTree returns the underlying Tree for direct access.
// This is useful for tests and low-level operations.
func (t *InMemoryVerkleTree) InnerTree() *Tree {
	return t.tree
}
