// proof_verifier.go implements a Merkle proof verifier for light clients.
// It supports single and batch proof verification as well as proof creation,
// suitable for verifying state inclusion proofs against beacon state roots.
// This is part of the eth2030 light client infrastructure.
package light

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Proof verifier errors.
var (
	ErrProofNilRoot       = errors.New("proof verifier: root must not be zero")
	ErrProofNilLeaf       = errors.New("proof verifier: leaf must not be zero")
	ErrProofEmptyPath     = errors.New("proof verifier: path must not be empty")
	ErrProofDepthExceeded = errors.New("proof verifier: proof depth exceeds maximum")
	ErrProofEmptyBatch    = errors.New("proof verifier: empty proof batch")
	ErrProofNoLeaves      = errors.New("proof verifier: no leaves provided")
	ErrProofIndexOutOfRange = errors.New("proof verifier: index out of range")
	ErrProofNotPowerOfTwo = errors.New("proof verifier: leaf count must be a power of two")
)

// ProofVerifierConfig configures the ProofVerifier.
type ProofVerifierConfig struct {
	// MaxProofDepth is the maximum allowed depth for Merkle proofs.
	MaxProofDepth int
	// AllowPartialProofs permits verification of proofs with depth less
	// than the tree height.
	AllowPartialProofs bool
	// CacheSize is the number of verified proofs to cache.
	CacheSize int
}

// DefaultProofVerifierConfig returns sensible defaults for the verifier.
func DefaultProofVerifierConfig() ProofVerifierConfig {
	return ProofVerifierConfig{
		MaxProofDepth:      64,
		AllowPartialProofs: false,
		CacheSize:          256,
	}
}

// MerkleProof represents a Merkle inclusion proof for a leaf in a tree.
type MerkleProof struct {
	Root  types.Hash
	Leaf  types.Hash
	Path  []types.Hash // sibling hashes from leaf to root
	Index uint64       // position of the leaf in the tree
}

// proofCacheKey uniquely identifies a verified proof for caching.
type proofCacheKey struct {
	root  types.Hash
	leaf  types.Hash
	index uint64
}

// ProofVerifier verifies Merkle inclusion proofs for light clients.
// All methods are safe for concurrent use.
type ProofVerifier struct {
	config   ProofVerifierConfig
	mu       sync.RWMutex
	cache    map[proofCacheKey]bool
	verified atomic.Uint64
}

// NewProofVerifier creates a new ProofVerifier with the given config.
func NewProofVerifier(config ProofVerifierConfig) *ProofVerifier {
	if config.MaxProofDepth <= 0 {
		config.MaxProofDepth = 64
	}
	if config.CacheSize <= 0 {
		config.CacheSize = 256
	}
	return &ProofVerifier{
		config: config,
		cache:  make(map[proofCacheKey]bool, config.CacheSize),
	}
}

// VerifyMerkleProof verifies a single Merkle inclusion proof. It recomputes
// the root from the leaf and path, and checks that it matches the claimed
// root in the proof.
func (pv *ProofVerifier) VerifyMerkleProof(proof MerkleProof) (bool, error) {
	if proof.Root.IsZero() {
		return false, ErrProofNilRoot
	}
	if proof.Leaf.IsZero() {
		return false, ErrProofNilLeaf
	}
	if len(proof.Path) == 0 {
		return false, ErrProofEmptyPath
	}
	if len(proof.Path) > pv.config.MaxProofDepth {
		return false, ErrProofDepthExceeded
	}

	// Check cache first.
	key := proofCacheKey{root: proof.Root, leaf: proof.Leaf, index: proof.Index}
	pv.mu.RLock()
	if result, ok := pv.cache[key]; ok {
		pv.mu.RUnlock()
		return result, nil
	}
	pv.mu.RUnlock()

	valid := pv.VerifyBranch(proof.Root, proof.Leaf, proof.Path, proof.Index)

	// Cache the result.
	pv.mu.Lock()
	pv.cacheResult(key, valid)
	pv.mu.Unlock()

	pv.verified.Add(1)
	return valid, nil
}

// VerifyMultiProof verifies multiple Merkle proofs. Returns true only if
// all proofs are valid. Stops at the first invalid proof.
func (pv *ProofVerifier) VerifyMultiProof(proofs []MerkleProof) (bool, error) {
	if len(proofs) == 0 {
		return false, ErrProofEmptyBatch
	}
	for i := range proofs {
		valid, err := pv.VerifyMerkleProof(proofs[i])
		if err != nil {
			return false, err
		}
		if !valid {
			return false, nil
		}
	}
	return true, nil
}

// CreateMerkleProof creates a Merkle inclusion proof for a leaf at the given
// index from a set of leaves. The leaf count must be a power of two.
func (pv *ProofVerifier) CreateMerkleProof(leaves []types.Hash, index uint64) (*MerkleProof, error) {
	if len(leaves) == 0 {
		return nil, ErrProofNoLeaves
	}
	if !isPowerOfTwo(uint64(len(leaves))) {
		return nil, ErrProofNotPowerOfTwo
	}
	if index >= uint64(len(leaves)) {
		return nil, ErrProofIndexOutOfRange
	}

	// Build the full tree bottom-up and collect the sibling path.
	depth := log2(uint64(len(leaves)))
	if depth > pv.config.MaxProofDepth {
		return nil, ErrProofDepthExceeded
	}

	// Initialize current layer with leaf hashes.
	layer := make([]types.Hash, len(leaves))
	copy(layer, leaves)

	path := make([]types.Hash, 0, depth)
	idx := index

	for len(layer) > 1 {
		// Record the sibling at the current level.
		if idx%2 == 0 {
			// Sibling is to the right.
			if idx+1 < uint64(len(layer)) {
				path = append(path, layer[idx+1])
			} else {
				path = append(path, layer[idx])
			}
		} else {
			// Sibling is to the left.
			path = append(path, layer[idx-1])
		}

		// Build the next layer up.
		next := make([]types.Hash, (len(layer)+1)/2)
		for i := 0; i < len(layer); i += 2 {
			if i+1 < len(layer) {
				h := crypto.Keccak256(layer[i][:], layer[i+1][:])
				copy(next[i/2][:], h)
			} else {
				next[i/2] = layer[i]
			}
		}
		layer = next
		idx = idx / 2
	}

	root := layer[0]

	return &MerkleProof{
		Root:  root,
		Leaf:  leaves[index],
		Path:  path,
		Index: index,
	}, nil
}

// ComputeMerkleRoot computes the Merkle root of a set of leaves. If the
// number of leaves is not a power of two, the tree is padded with zero hashes.
func (pv *ProofVerifier) ComputeMerkleRoot(leaves []types.Hash) types.Hash {
	if len(leaves) == 0 {
		return types.Hash{}
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	// Pad to power of two if needed.
	padded := padToPowerOfTwo(leaves)

	// Build tree bottom-up.
	layer := make([]types.Hash, len(padded))
	copy(layer, padded)

	for len(layer) > 1 {
		next := make([]types.Hash, (len(layer)+1)/2)
		for i := 0; i < len(layer); i += 2 {
			if i+1 < len(layer) {
				h := crypto.Keccak256(layer[i][:], layer[i+1][:])
				copy(next[i/2][:], h)
			} else {
				next[i/2] = layer[i]
			}
		}
		layer = next
	}

	return layer[0]
}

// VerifyBranch verifies that a leaf is included in a Merkle tree with the
// given root by walking the branch (path of sibling hashes) from leaf to
// root. The index determines whether the leaf is on the left or right at
// each level.
func (pv *ProofVerifier) VerifyBranch(root types.Hash, leaf types.Hash, branch []types.Hash, index uint64) bool {
	current := leaf
	for i, sibling := range branch {
		if (index>>uint(i))&1 == 0 {
			// current is left child, sibling is right.
			h := crypto.Keccak256(current[:], sibling[:])
			copy(current[:], h)
		} else {
			// sibling is left child, current is right.
			h := crypto.Keccak256(sibling[:], current[:])
			copy(current[:], h)
		}
	}
	return current == root
}

// ProofsVerified returns the total number of proofs verified.
func (pv *ProofVerifier) ProofsVerified() uint64 {
	return pv.verified.Load()
}

// cacheResult stores a proof verification result. Must be called with mu held.
func (pv *ProofVerifier) cacheResult(key proofCacheKey, valid bool) {
	if len(pv.cache) >= pv.config.CacheSize {
		// Evict one arbitrary entry.
		for k := range pv.cache {
			delete(pv.cache, k)
			break
		}
	}
	pv.cache[key] = valid
}

// isPowerOfTwo returns true if n is a power of two (and > 0).
func isPowerOfTwo(n uint64) bool {
	return n > 0 && (n&(n-1)) == 0
}

// log2 returns the base-2 logarithm of n (assumes n is a power of two).
func log2(n uint64) int {
	count := 0
	for n > 1 {
		n >>= 1
		count++
	}
	return count
}

// padToPowerOfTwo pads the slice with zero hashes to the next power of two.
func padToPowerOfTwo(leaves []types.Hash) []types.Hash {
	n := uint64(len(leaves))
	if isPowerOfTwo(n) {
		return leaves
	}
	// Find next power of two.
	target := uint64(1)
	for target < n {
		target <<= 1
	}
	padded := make([]types.Hash, target)
	copy(padded, leaves)
	return padded
}
