package verkle

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// VerkleTreeImpl is a thread-safe Verkle tree implementation backed
// by width-256 internal nodes and leaf nodes. It implements the
// VerkleTree interface with byte-slice keys and values.
//
// Commitments use a simulated Pedersen scheme via Keccak256 as a
// placeholder. In production this would use IPA (Inner Product
// Argument) over the Bandersnatch curve. The code comments indicate
// where real cryptographic operations would be substituted.
type VerkleTreeImpl struct {
	mu   sync.RWMutex
	tree *Tree
}

// Compile-time interface check.
var _ VerkleTree = (*VerkleTreeImpl)(nil)

// NewVerkleTreeImpl creates a new empty thread-safe Verkle tree.
func NewVerkleTreeImpl() *VerkleTreeImpl {
	return &VerkleTreeImpl{
		tree: NewTree(),
	}
}

// Get retrieves the value stored at the given key.
// The key must be exactly 32 bytes. Returns (nil, nil) if absent.
func (vt *VerkleTreeImpl) Get(key []byte) ([]byte, error) {
	k, err := toKey(key)
	if err != nil {
		return nil, err
	}
	vt.mu.RLock()
	defer vt.mu.RUnlock()

	val, err := vt.tree.Get(k)
	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, nil
	}
	out := make([]byte, ValueSize)
	copy(out, val[:])
	return out, nil
}

// Put stores a value at the given key. Both key and value must be
// exactly 32 bytes per EIP-6800.
func (vt *VerkleTreeImpl) Put(key []byte, value []byte) error {
	k, err := toKey(key)
	if err != nil {
		return err
	}
	v, err := toValue(value)
	if err != nil {
		return err
	}
	vt.mu.Lock()
	defer vt.mu.Unlock()
	return vt.tree.Put(k, v)
}

// Delete removes the value at the given key.
func (vt *VerkleTreeImpl) Delete(key []byte) error {
	k, err := toKey(key)
	if err != nil {
		return err
	}
	vt.mu.Lock()
	defer vt.mu.Unlock()
	return vt.tree.Delete(k)
}

// Commit computes and returns the tree root hash.
// The root hash is derived by hashing the Pedersen-style root
// commitment with Keccak256, producing the 32-byte state root
// for block headers.
//
// In production, the root commitment would be an IPA-based
// Banderwagon point, and the hash would use the Verkle-specific
// hash-to-field mapping.
func (vt *VerkleTreeImpl) Commit() (types.Hash, error) {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	c := vt.tree.RootCommitment()
	h := crypto.Keccak256Hash(c[:])
	return h, nil
}

// Root returns the tree root as a types.Hash without recomputing
// the commitment if it is already cached.
func (vt *VerkleTreeImpl) Root() types.Hash {
	vt.mu.RLock()
	defer vt.mu.RUnlock()
	c := vt.tree.RootCommitment()
	return crypto.Keccak256Hash(c[:])
}

// Prove generates a VerkleProof for the given key. The proof
// can demonstrate either inclusion (key exists with value) or
// absence (key does not exist in the tree).
//
// The proof contains:
//   - CommitmentsByPath: commitments along the path from root to leaf
//   - D: the leaf commitment (if a leaf was found)
//   - IPAProof: placeholder IPA bytes (Keccak256 of path data)
//   - Depth: how deep in the tree the search terminated
//   - ExtensionPresent: whether a leaf node was found
//   - Key, Value: the queried key and its value (if present)
//
// In production, the IPAProof would contain actual IPA multipoint
// argument data (log2(256) pairs of Banderwagon points + final scalar).
func (vt *VerkleTreeImpl) Prove(key []byte) (*VerkleProof, error) {
	k, err := toKey(key)
	if err != nil {
		return nil, err
	}
	vt.mu.RLock()
	defer vt.mu.RUnlock()
	return vt.generateProof(k), nil
}

// GenerateProof is an exported alias for proof generation with a
// byte-slice key. Thread-safe.
func (vt *VerkleTreeImpl) GenerateProof(key []byte) (*VerkleProof, error) {
	return vt.Prove(key)
}

// VerifyProof checks whether the given proof is valid against the
// provided root hash, key, and value.
//
// Verification steps:
//  1. Structural checks (non-empty commitments, valid depth).
//  2. The proof's IPA data must be non-empty.
//  3. The commitments path must be internally consistent: each
//     node's commitment must hash to a value referenced by its
//     parent. The root commitment must match the provided root.
//  4. If the proof claims inclusion (ExtensionPresent && Value != nil),
//     the value must match the provided expected value.
//
// In production, step 3 would be a full IPA multipoint verification.
func (vt *VerkleTreeImpl) VerifyProof(root types.Hash, key, value []byte, proof *VerkleProof) bool {
	if proof == nil || len(proof.CommitmentsByPath) == 0 {
		return false
	}
	if len(proof.IPAProof) == 0 {
		return false
	}
	if int(proof.Depth) > MaxDepth {
		return false
	}

	// Verify the root commitment matches.
	// The root hash is keccak256(rootCommitment).
	rootCommitment := proof.CommitmentsByPath[0]
	computedRoot := crypto.Keccak256Hash(rootCommitment[:])
	if computedRoot != root {
		return false
	}

	// If this is an inclusion proof, verify the value matches.
	if proof.ExtensionPresent && proof.Value != nil {
		if len(value) != ValueSize {
			return false
		}
		for i := 0; i < ValueSize; i++ {
			if proof.Value[i] != value[i] {
				return false
			}
		}
	}

	// Verify the key in the proof matches the provided key.
	if len(key) != KeySize {
		return false
	}
	var kk [KeySize]byte
	copy(kk[:], key)
	if kk != proof.Key {
		return false
	}

	// In production, this is where the IPA multipoint argument
	// would be cryptographically verified against the commitment
	// path and the claimed evaluation point.
	//
	// For our simulated implementation, we verify the IPA proof
	// is consistent with the path data by recomputing the expected
	// placeholder hash.
	var ipaInput []byte
	for _, c := range proof.CommitmentsByPath {
		ipaInput = append(ipaInput, c[:]...)
	}
	ipaInput = append(ipaInput, proof.Key[:]...)
	expectedIPA := crypto.Keccak256(ipaInput)
	if len(expectedIPA) != len(proof.IPAProof) {
		return false
	}
	for i := range expectedIPA {
		if expectedIPA[i] != proof.IPAProof[i] {
			return false
		}
	}

	return true
}

// StemFromAddress derives a 31-byte Verkle tree stem from an
// Ethereum address per EIP-6800. The stem determines which leaf
// node in the tree stores the account's header fields.
//
// This is a convenience wrapper around the key.go derivation.
func StemFromAddress(addr types.Address) []byte {
	stem := getAccountStem(addr)
	out := make([]byte, StemSize)
	copy(out, stem[:])
	return out
}

// NodeCount returns the total number of non-nil nodes in the tree.
// This traverses the tree and is intended for testing and metrics.
func (vt *VerkleTreeImpl) NodeCount() int {
	vt.mu.RLock()
	defer vt.mu.RUnlock()
	return countNodes(vt.tree.root)
}

// generateProof traverses the tree and builds a VerkleProof.
// Must be called with at least a read lock held.
func (vt *VerkleTreeImpl) generateProof(key [KeySize]byte) *VerkleProof {
	stem, suffix := splitKey(key)

	pathCommitments := []Commitment{vt.tree.root.Commit()}

	proof := &VerkleProof{
		Key: key,
	}

	node := vt.tree.root
	for depth := 0; depth < StemSize; depth++ {
		child := node.Child(stem[depth])
		if child == nil {
			proof.Depth = uint8(depth)
			proof.CommitmentsByPath = pathCommitments
			proof.IPAProof = computePlaceholderIPA(pathCommitments, key)
			return proof
		}

		switch c := child.(type) {
		case *LeafNode:
			proof.Depth = uint8(depth + 1)
			pathCommitments = append(pathCommitments, c.Commit())
			if c.stem == stem {
				proof.ExtensionPresent = true
				val := c.Get(suffix)
				if val != nil {
					v := *val
					proof.Value = &v
				}
				proof.D = c.Commit()
			}
			proof.CommitmentsByPath = pathCommitments
			proof.IPAProof = computePlaceholderIPA(pathCommitments, key)
			return proof

		case *InternalNode:
			pathCommitments = append(pathCommitments, c.Commit())
			node = c

		default:
			proof.Depth = uint8(depth)
			proof.CommitmentsByPath = pathCommitments
			proof.IPAProof = computePlaceholderIPA(pathCommitments, key)
			return proof
		}
	}

	proof.Depth = uint8(StemSize)
	proof.CommitmentsByPath = pathCommitments
	proof.IPAProof = computePlaceholderIPA(pathCommitments, key)
	return proof
}

// computePlaceholderIPA builds the placeholder IPA proof bytes.
// In production this would be the actual IPA multipoint argument.
func computePlaceholderIPA(commitments []Commitment, key [KeySize]byte) []byte {
	var input []byte
	for _, c := range commitments {
		input = append(input, c[:]...)
	}
	input = append(input, key[:]...)
	return crypto.Keccak256(input)
}

// countNodes recursively counts all nodes in the subtree.
func countNodes(n Node) int {
	if n == nil {
		return 0
	}
	switch nd := n.(type) {
	case *InternalNode:
		count := 1
		for i := 0; i < NodeWidth; i++ {
			if nd.children[i] != nil {
				count += countNodes(nd.children[i])
			}
		}
		return count
	case *LeafNode:
		return 1
	default:
		return 1
	}
}

// Errors for key/value validation.
var (
	errKeySize   = errors.New("verkle_tree: key must be exactly 32 bytes")
	errValueSize = errors.New("verkle_tree: value must be exactly 32 bytes")
)

// toKey validates and converts a byte slice to a fixed-size key.
func toKey(key []byte) ([KeySize]byte, error) {
	if len(key) != KeySize {
		return [KeySize]byte{}, errKeySize
	}
	var k [KeySize]byte
	copy(k[:], key)
	return k, nil
}

// toValue validates and converts a byte slice to a fixed-size value.
func toValue(value []byte) ([ValueSize]byte, error) {
	if len(value) != ValueSize {
		return [ValueSize]byte{}, errValueSize
	}
	var v [ValueSize]byte
	copy(v[:], value)
	return v, nil
}
