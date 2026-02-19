package light

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// CL proof type constants for the real-time CL proof system (I+ upgrade).
const (
	CLProofTypeStateRoot  = 0
	CLProofTypeValidator  = 1
	CLProofTypeBalance    = 2
	CLProofTypeCommittee  = 3
)

// CL proof errors.
var (
	ErrCLProofInvalidType  = errors.New("light: invalid CL proof type")
	ErrCLProofNilRoot      = errors.New("light: state root must not be zero")
	ErrCLProofEmptyPubkey  = errors.New("light: validator pubkey must not be empty")
)

// CLProofConfig configures the CL proof generator.
type CLProofConfig struct {
	// MaxProofDepth is the maximum depth of Merkle proof branches.
	MaxProofDepth int
	// CacheSize is the number of proofs to cache.
	CacheSize int
	// ProofTTL is the time-to-live for generated proofs.
	ProofTTL time.Duration
}

// DefaultCLProofConfig returns the default CL proof configuration.
func DefaultCLProofConfig() CLProofConfig {
	return CLProofConfig{
		MaxProofDepth: 40,
		CacheSize:     1000,
		ProofTTL:      12 * time.Second,
	}
}

// CLProof represents a Merkle proof for consensus layer state.
type CLProof struct {
	Type      int
	Slot      uint64
	Root      types.Hash
	Proof     [][]byte // Merkle branch from leaf to root
	Leaf      []byte
	LeafIndex uint64
	Timestamp time.Time
}

// CLProofGenerator manages generation and caching of CL state proofs.
type CLProofGenerator struct {
	config CLProofConfig
	mu     sync.RWMutex
	cache  map[clProofKey]*CLProof
	count  atomic.Uint64
}

// clProofKey is the cache key for CL proofs.
type clProofKey struct {
	proofType int
	slot      uint64
	index     uint64
}

// NewCLProofGenerator creates a new CL proof generator.
func NewCLProofGenerator(config CLProofConfig) *CLProofGenerator {
	return &CLProofGenerator{
		config: config,
		cache:  make(map[clProofKey]*CLProof, config.CacheSize),
	}
}

// GenerateStateRootProof generates a proof that a state root is valid at a
// given slot. The proof is a simulated Merkle branch: a hash chain from the
// leaf (slot data) to the claimed root.
func (g *CLProofGenerator) GenerateStateRootProof(slot uint64, stateRoot types.Hash) (*CLProof, error) {
	if stateRoot.IsZero() {
		return nil, ErrCLProofNilRoot
	}

	// Build the leaf: Keccak256(slot || stateRoot).
	slotBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(slotBytes, slot)
	leaf := crypto.Keccak256(slotBytes, stateRoot.Bytes())

	// Build a simulated Merkle branch of depth up to MaxProofDepth.
	// Each level hashes the previous with a sibling derived from the level index.
	proof := g.buildMerkleBranch(leaf, slot)

	// The root is derived by walking the branch upward.
	root := computeRootFromBranch(leaf, proof, slot)

	p := &CLProof{
		Type:      CLProofTypeStateRoot,
		Slot:      slot,
		Root:      types.BytesToHash(root),
		Proof:     proof,
		Leaf:      leaf,
		LeafIndex: slot,
		Timestamp: time.Now(),
	}

	g.cacheProof(clProofKey{CLProofTypeStateRoot, slot, 0}, p)
	g.count.Add(1)
	return p, nil
}

// GenerateValidatorProof generates a proof of a validator's existence and
// balance at a given slot. Leaf = Keccak256(validatorIndex || pubkey || balance).
func (g *CLProofGenerator) GenerateValidatorProof(slot uint64, validatorIndex uint64, pubkey []byte, balance uint64) (*CLProof, error) {
	if len(pubkey) == 0 {
		return nil, ErrCLProofEmptyPubkey
	}

	// Build leaf: Keccak256(validatorIndex || pubkey || balance).
	indexBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(indexBytes, validatorIndex)
	balBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(balBytes, balance)

	leaf := crypto.Keccak256(indexBytes, pubkey, balBytes)

	proof := g.buildMerkleBranch(leaf, validatorIndex)
	root := computeRootFromBranch(leaf, proof, validatorIndex)

	p := &CLProof{
		Type:      CLProofTypeValidator,
		Slot:      slot,
		Root:      types.BytesToHash(root),
		Proof:     proof,
		Leaf:      leaf,
		LeafIndex: validatorIndex,
		Timestamp: time.Now(),
	}

	g.cacheProof(clProofKey{CLProofTypeValidator, slot, validatorIndex}, p)
	g.count.Add(1)
	return p, nil
}

// GenerateBalanceProof generates a proof of a validator's balance at a given slot.
func (g *CLProofGenerator) GenerateBalanceProof(slot uint64, validatorIndex uint64, balance uint64) (*CLProof, error) {
	// Build leaf: Keccak256(validatorIndex || balance).
	indexBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(indexBytes, validatorIndex)
	balBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(balBytes, balance)

	leaf := crypto.Keccak256(indexBytes, balBytes)

	proof := g.buildMerkleBranch(leaf, validatorIndex)
	root := computeRootFromBranch(leaf, proof, validatorIndex)

	p := &CLProof{
		Type:      CLProofTypeBalance,
		Slot:      slot,
		Root:      types.BytesToHash(root),
		Proof:     proof,
		Leaf:      leaf,
		LeafIndex: validatorIndex,
		Timestamp: time.Now(),
	}

	g.cacheProof(clProofKey{CLProofTypeBalance, slot, validatorIndex}, p)
	g.count.Add(1)
	return p, nil
}

// VerifyProof verifies a Merkle proof by recomputing the root from the leaf
// and branch, and checking it matches the claimed root.
func VerifyProof(proof *CLProof) bool {
	if proof == nil || len(proof.Leaf) == 0 || len(proof.Proof) == 0 {
		return false
	}

	computed := computeRootFromBranch(proof.Leaf, proof.Proof, proof.LeafIndex)
	return types.BytesToHash(computed) == proof.Root
}

// ProofsGenerated returns the total number of proofs generated.
func (g *CLProofGenerator) ProofsGenerated() uint64 {
	return g.count.Load()
}

// buildMerkleBranch constructs a simulated Merkle branch of sibling hashes.
// At each level, the sibling is derived deterministically from the level
// index and the leaf index, allowing independent verification.
func (g *CLProofGenerator) buildMerkleBranch(leaf []byte, leafIndex uint64) [][]byte {
	depth := g.config.MaxProofDepth
	if depth <= 0 {
		depth = 40
	}

	branch := make([][]byte, depth)
	for i := 0; i < depth; i++ {
		// Deterministic sibling: hash of (level || leafIndex).
		levelBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(levelBytes, uint64(i))
		idxBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(idxBytes, leafIndex)
		sibling := crypto.Keccak256(levelBytes, idxBytes)
		branch[i] = sibling
	}
	return branch
}

// computeRootFromBranch walks a Merkle branch from leaf to root.
// At each level, the current hash is combined with the branch sibling:
// if the bit at that level in leafIndex is 0, current is left; otherwise right.
func computeRootFromBranch(leaf []byte, branch [][]byte, leafIndex uint64) []byte {
	current := make([]byte, len(leaf))
	copy(current, leaf)

	for i, sibling := range branch {
		if (leafIndex>>uint(i))&1 == 0 {
			// current is left child, sibling is right.
			current = crypto.Keccak256(current, sibling)
		} else {
			// sibling is left child, current is right.
			current = crypto.Keccak256(sibling, current)
		}
	}
	return current
}

// cacheProof stores a proof in the internal cache, evicting old entries
// if the cache exceeds its configured size.
func (g *CLProofGenerator) cacheProof(key clProofKey, proof *CLProof) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Simple eviction: if at capacity, clear expired entries or oldest.
	if len(g.cache) >= g.config.CacheSize {
		now := time.Now()
		for k, v := range g.cache {
			if now.Sub(v.Timestamp) > g.config.ProofTTL {
				delete(g.cache, k)
			}
		}
		// If still at capacity, just remove one arbitrary entry.
		if len(g.cache) >= g.config.CacheSize {
			for k := range g.cache {
				delete(g.cache, k)
				break
			}
		}
	}

	g.cache[key] = proof
}
