// Merkle multi-proof generation and verification for binary trees.
//
// A multi-proof demonstrates that a set of leaf values exist at specific
// positions in a Merkle tree, using the minimal set of internal nodes
// required for verification. This reduces proof size when proving multiple
// leaves compared to independent single proofs.
//
// The tree uses generalized indices: the root is at index 1, and for any
// node at index i, its left child is at 2i and its right child is at 2i+1.
// Leaves of a tree with 2^d leaves are at indices [2^d, 2^(d+1) - 1].
//
// This implementation is used for SSZ Merkle proofs in the beacon chain
// (EIP-4881) and for execution witness proofs.

package crypto

import (
	"errors"
	"math/bits"
	"sort"
)

// MerkleMultiProof contains the data needed to verify multiple leaves
// against a Merkle root using generalized indices.
type MerkleMultiProof struct {
	// Leaves are the leaf values being proved, in the order of their
	// generalized indices.
	Leaves []MerkleLeaf

	// Proof contains the minimal set of internal node hashes required
	// to reconstruct the root. Ordered by generalized index (ascending).
	Proof []MerkleNode

	// Depth is the tree depth (number of levels below root).
	Depth uint
}

// MerkleLeaf represents a leaf in the multi-proof.
type MerkleLeaf struct {
	// GeneralizedIndex is the position of this leaf in the complete tree.
	GeneralizedIndex uint64

	// Hash is the 32-byte hash of the leaf value.
	Hash [32]byte
}

// MerkleNode represents an internal node in the multi-proof.
type MerkleNode struct {
	// GeneralizedIndex is the position of this node.
	GeneralizedIndex uint64

	// Hash is the 32-byte hash of this node.
	Hash [32]byte
}

// --- Generalized index helpers ---

// GeneralizedIndex computes the generalized index for a leaf at the given
// position in a tree of the given depth.
// For depth d, leaves are at indices [2^d, 2^(d+1) - 1].
// Leaf position 0 maps to generalized index 2^d.
func GeneralizedIndex(depth uint, leafPos uint64) uint64 {
	return (1 << depth) + leafPos
}

// Parent returns the generalized index of the parent node.
func Parent(gi uint64) uint64 {
	return gi / 2
}

// Sibling returns the generalized index of the sibling node.
func Sibling(gi uint64) uint64 {
	return gi ^ 1
}

// IsLeft returns true if the generalized index represents a left child.
func IsLeft(gi uint64) bool {
	return gi%2 == 0
}

// Depth returns the depth (level) of a generalized index.
// The root (gi=1) is at depth 0.
func DepthOfGI(gi uint64) uint {
	if gi == 0 {
		return 0
	}
	return uint(bits.Len64(gi) - 1)
}

// PathToRoot returns the generalized indices along the path from gi to the
// root (exclusive of gi, inclusive of root=1).
func PathToRoot(gi uint64) []uint64 {
	var path []uint64
	for gi > 1 {
		gi = Parent(gi)
		path = append(path, gi)
	}
	return path
}

// --- Multi-proof generation ---

// GenerateMultiProof builds a multi-proof for the given leaf indices
// from a complete binary Merkle tree represented as a flat array.
//
// The tree array is indexed by generalized index: tree[1] is the root,
// tree[2] and tree[3] are its children, etc. tree[0] is unused.
// Leaves are at indices [2^depth, 2^(depth+1) - 1].
//
// The proof contains the minimal set of sibling hashes needed to
// reconstruct the root from the given leaves.
func GenerateMultiProof(tree [][32]byte, depth uint, leafIndices []uint64) (*MerkleMultiProof, error) {
	treeSize := uint64(1) << (depth + 1)
	if uint64(len(tree)) < treeSize {
		return nil, errors.New("merkle: tree array too small for given depth")
	}
	if len(leafIndices) == 0 {
		return nil, errors.New("merkle: no leaf indices provided")
	}

	// Convert leaf positions to generalized indices.
	gis := make([]uint64, len(leafIndices))
	for i, pos := range leafIndices {
		gi := GeneralizedIndex(depth, pos)
		if gi >= treeSize {
			return nil, errors.New("merkle: leaf index out of range")
		}
		gis[i] = gi
	}

	// Deduplicate and sort.
	gis = dedup(gis)

	// Collect the set of nodes that the verifier can compute (known set).
	// Start with the leaf GIs.
	known := make(map[uint64]bool)
	for _, gi := range gis {
		known[gi] = true
	}

	// Walk up from each leaf to root, collecting which nodes we'll need.
	needed := make(map[uint64]bool)
	for _, gi := range gis {
		cur := gi
		for cur > 1 {
			sib := Sibling(cur)
			if !known[sib] {
				needed[sib] = true
			}
			par := Parent(cur)
			known[par] = true
			cur = par
		}
	}

	// Remove any node from needed that is already in our leaf set
	// or can be computed from known nodes.
	proofGIs := computeMinimalProof(gis, known, needed)

	// Build proof.
	leaves := make([]MerkleLeaf, len(gis))
	for i, gi := range gis {
		leaves[i] = MerkleLeaf{
			GeneralizedIndex: gi,
			Hash:             tree[gi],
		}
	}

	proofNodes := make([]MerkleNode, len(proofGIs))
	for i, gi := range proofGIs {
		proofNodes[i] = MerkleNode{
			GeneralizedIndex: gi,
			Hash:             tree[gi],
		}
	}

	return &MerkleMultiProof{
		Leaves: leaves,
		Proof:  proofNodes,
		Depth:  depth,
	}, nil
}

// computeMinimalProof returns the sorted set of generalized indices that
// must be included in the proof (nodes the verifier cannot compute).
func computeMinimalProof(leafGIs []uint64, known, needed map[uint64]bool) []uint64 {
	// The proof set is nodes that are needed but not known from the leaves.
	leafSet := make(map[uint64]bool)
	for _, gi := range leafGIs {
		leafSet[gi] = true
	}

	var proofGIs []uint64
	for gi := range needed {
		if !leafSet[gi] {
			proofGIs = append(proofGIs, gi)
		}
	}

	sort.Slice(proofGIs, func(i, j int) bool {
		return proofGIs[i] < proofGIs[j]
	})
	return proofGIs
}

// --- Multi-proof verification ---

// VerifyMultiProof checks that the given multi-proof is consistent with
// the provided root hash.
//
// The verification reconstructs the root by combining the leaf hashes
// with the proof nodes, working from the bottom of the tree upward.
func VerifyMultiProof(root [32]byte, proof *MerkleMultiProof) bool {
	if proof == nil || len(proof.Leaves) == 0 {
		return false
	}

	// Build a map of all known hashes: leaves + proof nodes.
	hashes := make(map[uint64][32]byte)
	for _, leaf := range proof.Leaves {
		hashes[leaf.GeneralizedIndex] = leaf.Hash
	}
	for _, node := range proof.Proof {
		hashes[node.GeneralizedIndex] = node.Hash
	}

	// Collect all generalized indices and sort by depth (deepest first).
	var allGIs []uint64
	for gi := range hashes {
		allGIs = append(allGIs, gi)
	}
	sort.Slice(allGIs, func(i, j int) bool {
		// Sort by depth descending (deeper nodes first), then by GI.
		di, dj := DepthOfGI(allGIs[i]), DepthOfGI(allGIs[j])
		if di != dj {
			return di > dj
		}
		return allGIs[i] < allGIs[j]
	})

	// Process nodes bottom-up: for each pair of siblings, compute parent.
	for _, gi := range allGIs {
		if gi <= 1 {
			continue
		}
		sib := Sibling(gi)
		sibHash, hasSib := hashes[sib]
		if !hasSib {
			continue // sibling not available yet; will be computed later
		}

		par := Parent(gi)
		if _, has := hashes[par]; has {
			continue // parent already known
		}

		myHash := hashes[gi]
		var left, right [32]byte
		if IsLeft(gi) {
			left = myHash
			right = sibHash
		} else {
			left = sibHash
			right = myHash
		}

		parentHash := merkleHashPair(left, right)
		hashes[par] = parentHash
	}

	// Iteratively compute parents until we reach root or can't proceed.
	changed := true
	for changed {
		changed = false
		for gi := range hashes {
			if gi <= 1 {
				continue
			}
			sib := Sibling(gi)
			sibHash, hasSib := hashes[sib]
			if !hasSib {
				continue
			}
			par := Parent(gi)
			if _, has := hashes[par]; has {
				continue
			}

			myHash := hashes[gi]
			var left, right [32]byte
			if IsLeft(gi) {
				left = myHash
				right = sibHash
			} else {
				left = sibHash
				right = myHash
			}

			hashes[par] = merkleHashPair(left, right)
			changed = true
		}
	}

	// Check root.
	computedRoot, ok := hashes[1]
	if !ok {
		return false
	}
	return computedRoot == root
}

// merkleHashPair computes the Merkle tree parent hash from two children.
// Uses Keccak256(left || right).
func merkleHashPair(left, right [32]byte) [32]byte {
	data := make([]byte, 64)
	copy(data[:32], left[:])
	copy(data[32:], right[:])
	h := Keccak256(data)
	var result [32]byte
	copy(result[:], h)
	return result
}

// --- Proof compaction ---

// CompactMultiProof removes redundant proof nodes that can be computed
// from other proof nodes + leaves. This handles the case where two
// proved leaves share a common ancestor and the sibling of one ancestor
// is itself an ancestor of another proved leaf.
func CompactMultiProof(proof *MerkleMultiProof) *MerkleMultiProof {
	if proof == nil || len(proof.Leaves) <= 1 {
		return proof
	}

	// Build set of all known GIs from leaves.
	known := make(map[uint64]bool)
	for _, leaf := range proof.Leaves {
		known[leaf.GeneralizedIndex] = true
	}

	// Walk each leaf to root, marking computable parents.
	for _, leaf := range proof.Leaves {
		cur := leaf.GeneralizedIndex
		for cur > 1 {
			par := Parent(cur)
			sib := Sibling(cur)
			if known[sib] {
				known[par] = true
			}
			cur = par
		}
	}

	// Filter proof nodes: keep only those not computable from leaves.
	var compacted []MerkleNode
	for _, node := range proof.Proof {
		if !known[node.GeneralizedIndex] {
			compacted = append(compacted, node)
			known[node.GeneralizedIndex] = true
			// Propagate knowledge upward.
			cur := node.GeneralizedIndex
			for cur > 1 {
				par := Parent(cur)
				sib := Sibling(cur)
				if known[sib] {
					known[par] = true
				}
				cur = par
			}
		}
	}

	return &MerkleMultiProof{
		Leaves: proof.Leaves,
		Proof:  compacted,
		Depth:  proof.Depth,
	}
}

// --- Tree construction helpers ---

// BuildMerkleTree constructs a binary Merkle tree from the given leaves.
// Returns the flat tree array indexed by generalized index.
// The number of leaves is rounded up to the next power of 2 (zero-filled).
func BuildMerkleTree(leaves [][32]byte) ([][32]byte, uint) {
	n := len(leaves)
	if n == 0 {
		n = 1
	}
	// Round up to power of 2.
	depth := uint(0)
	size := 1
	for size < n {
		size *= 2
		depth++
	}
	if depth == 0 && n > 0 {
		depth = 1
		size = 2
	}

	treeSize := 2 * size
	tree := make([][32]byte, treeSize)

	// Place leaves at [size, 2*size).
	for i := 0; i < len(leaves); i++ {
		tree[size+i] = leaves[i]
	}

	// Build internal nodes bottom-up.
	for i := size - 1; i >= 1; i-- {
		tree[i] = merkleHashPair(tree[2*i], tree[2*i+1])
	}

	return tree, depth
}

// MerkleRoot computes the Merkle root of a set of leaves.
func MerkleRoot(leaves [][32]byte) [32]byte {
	tree, _ := BuildMerkleTree(leaves)
	if len(tree) < 2 {
		return [32]byte{}
	}
	return tree[1]
}

// --- Utilities ---

// dedup removes duplicate uint64 values and sorts the result.
func dedup(vals []uint64) []uint64 {
	seen := make(map[uint64]bool)
	var result []uint64
	for _, v := range vals {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// ProofSize returns the number of proof nodes needed for a multi-proof
// of k leaves in a tree of the given depth. This is an upper bound;
// actual proof size may be smaller due to shared internal nodes.
func ProofSize(depth uint, k int) int {
	if k == 0 {
		return 0
	}
	// Upper bound: k * depth (each leaf needs at most depth siblings).
	// Shared nodes reduce this. Worst case is k independent paths.
	return k * int(depth)
}
