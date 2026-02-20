// commitment_tree.go implements an append-only Merkle tree accumulator for
// output commitments in the shielded transfer system. The tree has a fixed
// depth of 32, supporting up to 2^32 commitments.
//
// SHA-256 is used for all hashing, providing post-quantum security.
// The tree supports efficient incremental root updates when new commitments
// are appended, and generates Merkle proofs for commitment inclusion.
package crypto

import (
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// CommitTreeDepth is the depth of the commitment Merkle tree.
const CommitTreeDepth = 32

// Maximum number of leaves: 2^32.
const commitTreeMaxLeaves = 1 << CommitTreeDepth

// Commitment tree errors.
var (
	ErrCommitTreeFull     = errors.New("commitment_tree: tree is full")
	ErrCommitTreeBadIndex = errors.New("commitment_tree: index out of range")
	ErrCommitTreeBadProof = errors.New("commitment_tree: invalid proof")
	ErrCommitTreeEmpty    = errors.New("commitment_tree: tree is empty")
)

// Domain separators for the commitment tree.
var (
	ctDomainLeaf = []byte{0x10}
	ctDomainNode = []byte{0x11}
)

// ctEmptyHashes[i] is the hash of an empty subtree at depth i (0 = leaf).
var ctEmptyHashes [CommitTreeDepth + 1][32]byte

func init() {
	// Level 0: empty leaf.
	h := sha256.New()
	h.Write(ctDomainLeaf)
	copy(ctEmptyHashes[0][:], h.Sum(nil))

	for i := 1; i <= CommitTreeDepth; i++ {
		h.Reset()
		h.Write(ctDomainNode)
		h.Write(ctEmptyHashes[i-1][:])
		h.Write(ctEmptyHashes[i-1][:])
		copy(ctEmptyHashes[i][:], h.Sum(nil))
	}
}

// ctHashLeaf hashes a commitment leaf with domain separation.
func ctHashLeaf(commitment types.Hash) [32]byte {
	h := sha256.New()
	h.Write(ctDomainLeaf)
	h.Write(commitment[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// ctHashNode hashes two child nodes with domain separation.
func ctHashNode(left, right [32]byte) [32]byte {
	h := sha256.New()
	h.Write(ctDomainNode)
	h.Write(left[:])
	h.Write(right[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// CommitmentTreeProof is a Merkle proof for a commitment in the tree.
type CommitmentTreeProof struct {
	Index    uint64
	Siblings [CommitTreeDepth][32]byte
}

// CommitmentTree is an append-only Merkle tree accumulator.
// It stores leaf hashes and intermediate node caches for efficient
// incremental root updates.
type CommitmentTree struct {
	mu       sync.RWMutex
	leaves   []types.Hash    // raw commitments
	hashes   [][32]byte      // leaf hashes
	filledAt [CommitTreeDepth][32]byte // cache of filled subtrees at each level
	nextIdx  uint64
	root     [32]byte
}

// NewCommitmentTree creates a new empty commitment tree.
func NewCommitmentTree() *CommitmentTree {
	ct := &CommitmentTree{
		leaves: make([]types.Hash, 0, 1024),
		hashes: make([][32]byte, 0, 1024),
		root:   ctEmptyHashes[CommitTreeDepth],
	}
	return ct
}

// Root returns the current Merkle root.
func (ct *CommitmentTree) Root() types.Hash {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return types.Hash(ct.root)
}

// Size returns the number of commitments in the tree.
func (ct *CommitmentTree) Size() uint64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.nextIdx
}

// Append adds a commitment to the tree and returns its index and new root.
func (ct *CommitmentTree) Append(commitment types.Hash) (uint64, types.Hash, error) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.nextIdx >= uint64(commitTreeMaxLeaves) {
		return 0, types.Hash{}, ErrCommitTreeFull
	}

	idx := ct.nextIdx
	leafHash := ctHashLeaf(commitment)

	ct.leaves = append(ct.leaves, commitment)
	ct.hashes = append(ct.hashes, leafHash)
	ct.nextIdx++

	// Update the incremental root using the filled-subtree cache.
	ct.root = ct.incrementalRoot(idx, leafHash)

	return idx, types.Hash(ct.root), nil
}

// BatchAppend appends multiple commitments and returns their starting index
// and the final root.
func (ct *CommitmentTree) BatchAppend(commitments []types.Hash) (uint64, types.Hash, error) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.nextIdx+uint64(len(commitments)) > uint64(commitTreeMaxLeaves) {
		return 0, types.Hash{}, ErrCommitTreeFull
	}

	startIdx := ct.nextIdx
	for _, c := range commitments {
		leafHash := ctHashLeaf(c)
		ct.leaves = append(ct.leaves, c)
		ct.hashes = append(ct.hashes, leafHash)
		ct.root = ct.incrementalRoot(ct.nextIdx, leafHash)
		ct.nextIdx++
	}

	return startIdx, types.Hash(ct.root), nil
}

// MerkleProof generates a Merkle proof for the commitment at the given index.
func (ct *CommitmentTree) MerkleProof(index uint64) (*CommitmentTreeProof, error) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	if index >= ct.nextIdx {
		return nil, ErrCommitTreeBadIndex
	}

	proof := &CommitmentTreeProof{Index: index}

	// Rebuild the tree layers to extract siblings.
	n := ct.nextIdx
	layer := make([][32]byte, n)
	copy(layer, ct.hashes[:n])

	for level := 0; level < CommitTreeDepth; level++ {
		// Pad to even length.
		if len(layer)%2 != 0 {
			layer = append(layer, ctEmptyHashes[level])
		}

		sibIdx := index ^ 1 // sibling index
		if sibIdx < uint64(len(layer)) {
			proof.Siblings[level] = layer[sibIdx]
		} else {
			proof.Siblings[level] = ctEmptyHashes[level]
		}

		// Compute next layer.
		nextLayer := make([][32]byte, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			nextLayer[i/2] = ctHashNode(layer[i], layer[i+1])
		}
		layer = nextLayer
		index /= 2
	}
	return proof, nil
}

// VerifyCommitmentProof verifies a Merkle proof for a commitment against a root.
func VerifyCommitmentProof(commitment types.Hash, proof *CommitmentTreeProof, root types.Hash) bool {
	if proof == nil {
		return false
	}

	current := ctHashLeaf(commitment)
	idx := proof.Index

	for level := 0; level < CommitTreeDepth; level++ {
		sibling := proof.Siblings[level]
		if idx%2 == 0 {
			current = ctHashNode(current, sibling)
		} else {
			current = ctHashNode(sibling, current)
		}
		idx /= 2
	}

	return types.Hash(current) == root
}

// incrementalRoot updates the root after inserting a leaf at the given index.
// Uses the filledAt cache to avoid recomputing the entire tree.
func (ct *CommitmentTree) incrementalRoot(index uint64, leafHash [32]byte) [32]byte {
	current := leafHash

	for level := 0; level < CommitTreeDepth; level++ {
		if index%2 == 0 {
			// We are the left child; right sibling is empty.
			ct.filledAt[level] = current
			current = ctHashNode(current, ctEmptyHashes[level])
		} else {
			// We are the right child; left sibling is the cached filled node.
			current = ctHashNode(ct.filledAt[level], current)
		}
		index /= 2
	}
	return current
}
