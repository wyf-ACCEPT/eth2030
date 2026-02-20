package trie

import (
	"errors"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// BinaryProof is a Merkle proof for a key in a binary trie. It contains
// the sibling hashes along the path from the root to the leaf, plus the
// depth at which the leaf was found.
type BinaryProof struct {
	// Siblings contains the sibling hash at each level, from root to leaf.
	Siblings []types.Hash
	// Key is the full 32-byte hashed key of the proven leaf.
	Key types.Hash
	// Value is the value stored at the leaf (nil for absence proofs).
	Value []byte
}

// ErrBinaryProofInvalid is returned when a binary Merkle proof fails verification.
var ErrBinaryProofInvalid = errors.New("binary trie: invalid proof")

// Prove generates a Merkle proof for the given key. The key is hashed with
// keccak256. Returns ErrNotFound if the key does not exist.
func (t *BinaryTrie) Prove(key []byte) (*BinaryProof, error) {
	hk := crypto.Keccak256Hash(key)
	return t.ProveHashed(hk)
}

// ProveHashed generates a Merkle proof for a pre-hashed key.
func (t *BinaryTrie) ProveHashed(hk types.Hash) (*BinaryProof, error) {
	// First compute hashes so they're cached.
	t.Hash()

	var siblings []types.Hash
	n := t.root
	for depth := 0; n != nil; depth++ {
		if n.isLeaf {
			if n.key != hk {
				return nil, ErrNotFound
			}
			return &BinaryProof{
				Siblings: siblings,
				Key:      hk,
				Value:    copyBytes(n.value),
			}, nil
		}
		// Collect sibling hash at this level.
		if getBit(hk, depth) == 0 {
			siblings = append(siblings, hashBinaryNode(n.right))
			n = n.left
		} else {
			siblings = append(siblings, hashBinaryNode(n.left))
			n = n.right
		}
	}
	return nil, ErrNotFound
}

// verifyBinaryProofSimple verifies that a binary Merkle proof is valid against
// the given root hash. Returns nil on success, ErrBinaryProofInvalid on failure.
func verifyBinaryProofSimple(rootHash types.Hash, proof *BinaryProof) error {
	if proof == nil {
		return ErrBinaryProofInvalid
	}

	// Reconstruct the leaf hash.
	buf := make([]byte, 1+32+len(proof.Value))
	buf[0] = 0x00
	copy(buf[1:33], proof.Key[:])
	copy(buf[33:], proof.Value)
	current := crypto.Keccak256Hash(buf)

	// Walk back from the leaf to the root, combining with siblings.
	for i := len(proof.Siblings) - 1; i >= 0; i-- {
		depth := i
		sibling := proof.Siblings[i]
		branchBuf := make([]byte, 1+32+32)
		branchBuf[0] = 0x01
		if getBit(proof.Key, depth) == 0 {
			copy(branchBuf[1:33], current[:])
			copy(branchBuf[33:65], sibling[:])
		} else {
			copy(branchBuf[1:33], sibling[:])
			copy(branchBuf[33:65], current[:])
		}
		current = crypto.Keccak256Hash(branchBuf)
	}

	if current != rootHash {
		return ErrBinaryProofInvalid
	}
	return nil
}

// ProofSize returns the byte size of a binary proof (number of siblings * 32 bytes + key + value).
func (p *BinaryProof) ProofSize() int {
	if p == nil {
		return 0
	}
	return len(p.Siblings)*32 + 32 + len(p.Value)
}
