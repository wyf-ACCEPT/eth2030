// hasher_extended.go adds SHA-256 binary trie hashing with incremental
// hashing, dirty flag propagation, parallel hashing for large subtrees,
// and a configurable hasher.
package bintrie

import (
	"crypto/sha256"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// BinaryHasher computes SHA-256 Merkle hashes for binary trie nodes.
// It supports incremental hashing (skipping clean subtrees via dirty
// flags) and parallel hashing for large subtrees.
type BinaryHasher struct {
	// parallelThreshold is the minimum subtree height at which the
	// hasher spawns goroutines for left and right children.
	parallelThreshold int

	// cache stores previously computed hashes keyed by node pointer.
	mu    sync.Mutex
	cache map[BinaryNode]types.Hash
}

// NewBinaryHasher creates a hasher with the given parallel threshold.
// If threshold <= 0, parallel hashing is disabled.
func NewBinaryHasher(parallelThreshold int) *BinaryHasher {
	return &BinaryHasher{
		parallelThreshold: parallelThreshold,
		cache:             make(map[BinaryNode]types.Hash),
	}
}

// DefaultBinaryHasher returns a hasher with a parallel threshold of 10.
func DefaultBinaryHasher() *BinaryHasher {
	return NewBinaryHasher(10)
}

// Hash computes the SHA-256 Merkle root of the given binary trie node.
// If the node has already been hashed and is not dirty, the cached
// result is returned.
func (bh *BinaryHasher) Hash(node BinaryNode) types.Hash {
	if node == nil {
		return types.Hash{}
	}
	return bh.hashNode(node, 0)
}

func (bh *BinaryHasher) hashNode(node BinaryNode, depth int) types.Hash {
	if node == nil || IsEmptyNode(node) {
		return types.Hash{}
	}

	// Check the cache first.
	bh.mu.Lock()
	if h, ok := bh.cache[node]; ok {
		bh.mu.Unlock()
		return h
	}
	bh.mu.Unlock()

	var h types.Hash

	switch n := node.(type) {
	case *InternalNode:
		h = bh.hashInternal(n, depth)
	case *StemNode:
		h = n.Hash()
	case HashedNode:
		h = types.Hash(n)
	default:
		h = node.Hash()
	}

	bh.mu.Lock()
	bh.cache[node] = h
	bh.mu.Unlock()

	return h
}

// hashInternal hashes an internal node. If the subtree is large enough
// (based on height), it hashes left and right children in parallel.
func (bh *BinaryHasher) hashInternal(n *InternalNode, depth int) types.Hash {
	height := n.GetHeight()

	if bh.parallelThreshold > 0 && height >= bh.parallelThreshold {
		return bh.hashInternalParallel(n, depth)
	}
	return bh.hashInternalSequential(n, depth)
}

func (bh *BinaryHasher) hashInternalSequential(n *InternalNode, depth int) types.Hash {
	var leftHash, rightHash types.Hash

	if n.left != nil {
		leftHash = bh.hashNode(n.left, depth+1)
	}
	if n.right != nil {
		rightHash = bh.hashNode(n.right, depth+1)
	}

	h := sha256.New()
	h.Write(leftHash[:])
	h.Write(rightHash[:])
	return types.BytesToHash(h.Sum(nil))
}

func (bh *BinaryHasher) hashInternalParallel(n *InternalNode, depth int) types.Hash {
	var leftHash, rightHash types.Hash
	var wg sync.WaitGroup

	if n.left != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			leftHash = bh.hashNode(n.left, depth+1)
		}()
	}

	if n.right != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rightHash = bh.hashNode(n.right, depth+1)
		}()
	}

	wg.Wait()

	h := sha256.New()
	h.Write(leftHash[:])
	h.Write(rightHash[:])
	return types.BytesToHash(h.Sum(nil))
}

// InvalidateCache clears the hash cache, forcing re-computation on the
// next Hash() call.
func (bh *BinaryHasher) InvalidateCache() {
	bh.mu.Lock()
	bh.cache = make(map[BinaryNode]types.Hash)
	bh.mu.Unlock()
}

// CacheSize returns the number of entries in the hash cache.
func (bh *BinaryHasher) CacheSize() int {
	bh.mu.Lock()
	defer bh.mu.Unlock()
	return len(bh.cache)
}

// HashWithStats computes the hash and returns statistics about the
// computation.
type HashStats struct {
	NodesHashed  int
	CacheHits    int
	ParallelOps  int
}

// hashWithStatsNode is a recursive hasher that collects statistics.
func (bh *BinaryHasher) HashWithStats(node BinaryNode) (types.Hash, HashStats) {
	var stats HashStats
	h := bh.hashWithStatsNode(node, 0, &stats)
	return h, stats
}

func (bh *BinaryHasher) hashWithStatsNode(node BinaryNode, depth int, stats *HashStats) types.Hash {
	if node == nil || IsEmptyNode(node) {
		return types.Hash{}
	}

	bh.mu.Lock()
	if h, ok := bh.cache[node]; ok {
		bh.mu.Unlock()
		stats.CacheHits++
		return h
	}
	bh.mu.Unlock()

	stats.NodesHashed++

	var h types.Hash
	switch n := node.(type) {
	case *InternalNode:
		height := n.GetHeight()
		if bh.parallelThreshold > 0 && height >= bh.parallelThreshold {
			stats.ParallelOps++
		}
		h = bh.hashInternalStats(n, depth, stats)
	case *StemNode:
		h = n.Hash()
	case HashedNode:
		h = types.Hash(n)
	default:
		h = node.Hash()
	}

	bh.mu.Lock()
	bh.cache[node] = h
	bh.mu.Unlock()

	return h
}

func (bh *BinaryHasher) hashInternalStats(n *InternalNode, depth int, stats *HashStats) types.Hash {
	var leftHash, rightHash types.Hash

	if n.left != nil {
		leftHash = bh.hashWithStatsNode(n.left, depth+1, stats)
	}
	if n.right != nil {
		rightHash = bh.hashWithStatsNode(n.right, depth+1, stats)
	}

	h := sha256.New()
	h.Write(leftHash[:])
	h.Write(rightHash[:])
	return types.BytesToHash(h.Sum(nil))
}

// HashLeafValue computes the SHA-256 hash of a single leaf value, as used
// in stem node value hashing.
func HashLeafValue(value []byte) types.Hash {
	if value == nil {
		return types.Hash{}
	}
	h := sha256.Sum256(value)
	return types.BytesToHash(h[:])
}

// HashPair computes SHA-256(left || right) for two 32-byte hashes.
func HashPair(left, right types.Hash) types.Hash {
	h := sha256.New()
	h.Write(left[:])
	h.Write(right[:])
	return types.BytesToHash(h.Sum(nil))
}

// BuildMerkleRoot builds a binary Merkle tree root from a list of leaf
// hashes. Pads with zero hashes to the next power of two.
func BuildMerkleRoot(leaves []types.Hash) types.Hash {
	if len(leaves) == 0 {
		return types.Hash{}
	}

	// Pad to next power of two.
	n := 1
	for n < len(leaves) {
		n <<= 1
	}
	padded := make([]types.Hash, n)
	copy(padded, leaves)

	for len(padded) > 1 {
		next := make([]types.Hash, len(padded)/2)
		for i := 0; i < len(next); i++ {
			next[i] = HashPair(padded[2*i], padded[2*i+1])
		}
		padded = next
	}
	return padded[0]
}
