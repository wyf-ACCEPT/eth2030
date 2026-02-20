// empty_extended.go adds empty trie sentinel functionality: proper hash
// computation, Merkle root derivation, proof generation for empty state,
// serialization, and static empty hash constants.
package bintrie

import (
	"crypto/sha256"

	"github.com/eth2028/eth2028/core/types"
)

// emptyHashOnce caches the SHA-256 hash of the empty node.
var emptyHashComputed types.Hash

func init() {
	emptyHashComputed = computeEmptyHash()
}

// computeEmptyHash returns the canonical SHA-256 hash for an empty binary
// trie. This is SHA256(zero32 || zero32) representing an empty internal
// node with no children.
func computeEmptyHash() types.Hash {
	h := sha256.New()
	h.Write(zero[:])
	h.Write(zero[:])
	return types.BytesToHash(h.Sum(nil))
}

// EmptyHash returns the canonical hash of an empty binary trie.
func EmptyHash() types.Hash {
	return emptyHashComputed
}

// EmptyMerkleRoot returns the Merkle root of an empty trie. For the
// binary trie this is the SHA-256 hash of two concatenated zero hashes.
func EmptyMerkleRoot() types.Hash {
	return emptyHashComputed
}

// EmptyProof generates an inclusion proof for an empty trie. Since the
// trie is empty, the proof contains no siblings and proves key absence.
func EmptyProof(key []byte) *Proof {
	k := make([]byte, HashSize)
	if len(key) >= HashSize {
		copy(k, key[:HashSize])
	} else {
		copy(k, key)
	}
	return &Proof{
		Key:       k,
		Value:     nil,
		Siblings:  nil,
		Stem:      k[:StemSize],
		LeafIndex: 0,
	}
}

// IsEmptyNode reports whether a BinaryNode is the empty sentinel.
func IsEmptyNode(n BinaryNode) bool {
	_, ok := n.(Empty)
	return ok
}

// EmptyNodeHash returns the hash of an Empty node, matching the Hash()
// method behavior but cached.
func EmptyNodeHash() types.Hash {
	return types.Hash{}
}

// SerializeEmpty returns the serialized form of an Empty node.
// An empty node serializes as a single zero byte.
func SerializeEmpty() []byte {
	return []byte{0}
}

// DeserializeEmpty checks if data represents an empty node.
func DeserializeEmpty(data []byte) (Empty, bool) {
	if len(data) == 1 && data[0] == 0 {
		return Empty{}, true
	}
	if len(data) == 0 {
		return Empty{}, true
	}
	return Empty{}, false
}

// EmptyStateProof generates a proof that a given key does not exist in
// an empty trie. The proof consists of the empty root hash and an empty
// sibling list.
type EmptyStateProof struct {
	Key      []byte
	RootHash types.Hash
}

// NewEmptyStateProof creates an absence proof for the given key.
func NewEmptyStateProof(key []byte) EmptyStateProof {
	k := make([]byte, len(key))
	copy(k, key)
	return EmptyStateProof{
		Key:      k,
		RootHash: EmptyMerkleRoot(),
	}
}

// Verify checks that the proof correctly demonstrates key absence in an
// empty trie. The provided root must match the empty Merkle root.
func (p EmptyStateProof) Verify(root types.Hash) bool {
	return root == EmptyMerkleRoot()
}

// EmptyTrieStats returns statistics for an empty trie.
type EmptyTrieStats struct {
	NodeCount  int
	Height     int
	MerkleRoot types.Hash
}

// GetEmptyTrieStats returns the statistics for an empty trie.
func GetEmptyTrieStats() EmptyTrieStats {
	return EmptyTrieStats{
		NodeCount:  0,
		Height:     0,
		MerkleRoot: EmptyMerkleRoot(),
	}
}

// EmptySubtreeHash returns the hash of an empty subtree at the given
// depth. At depth 0 it is the zero hash; at higher depths it is the
// recursive SHA-256(empty(d-1) || empty(d-1)).
func EmptySubtreeHash(depth int) types.Hash {
	if depth <= 0 {
		return types.Hash{}
	}
	h := types.Hash{}
	hasher := sha256.New()
	for i := 0; i < depth; i++ {
		hasher.Reset()
		hasher.Write(h[:])
		hasher.Write(h[:])
		h = types.BytesToHash(hasher.Sum(nil))
	}
	return h
}
