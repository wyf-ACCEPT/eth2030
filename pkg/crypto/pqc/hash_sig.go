// Post-Quantum Hash-Based Signatures (M+ roadmap: post quantum L1 hash-based)
//
// Implements an XMSS/LMS-style hash-based signature scheme using Keccak256.
// The scheme builds a Merkle tree of one-time signature (OTS) public keys
// using a Winternitz construction. Each leaf corresponds to one OTS key pair,
// so the tree height determines the maximum number of signatures (2^height).
//
// Security relies only on Keccak256 pre-image resistance, providing full
// post-quantum security without lattice or number-theoretic assumptions.
package pqc

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Hash-based signature constants.
const (
	// hashSigChainLen is the number of 32-byte chains in Winternitz OTS.
	// With w=16, we need ceil(256/4) = 64 chains for a 256-bit message,
	// plus 3 checksum chains.
	hashSigChainLen = 67

	// hashSigSeedSize is the byte length of the random seed per key pair.
	hashSigSeedSize = 32
)

// Hash-based signature errors.
var (
	ErrHashSigInvalidHeight   = errors.New("pqc: tree height must be 1-20")
	ErrHashSigInvalidWinternitz = errors.New("pqc: Winternitz parameter must be 4 or 16")
	ErrHashSigNoKeysLeft      = errors.New("pqc: no remaining one-time keys")
	ErrHashSigNilPrivateKey   = errors.New("pqc: nil private key")
	ErrHashSigNilMessage      = errors.New("pqc: nil message")
	ErrHashSigNilSignature    = errors.New("pqc: nil signature")
	ErrHashSigInvalidLeaf     = errors.New("pqc: leaf index exceeds tree size")
	ErrHashSigBadAuthPath     = errors.New("pqc: auth path length mismatch")
)

// HashSigKeyPair holds a hash-based signature key pair.
type HashSigKeyPair struct {
	PublicKey           []byte // Merkle root of OTS public keys
	PrivateKey          []byte // Seed + state (seed || leafIndex as 4 bytes)
	Height              int
	RemainingSignatures int
}

// HashSignature is a hash-based signature consisting of a Winternitz
// OTS signature and a Merkle authentication path from the leaf to root.
type HashSignature struct {
	LeafIndex    uint32
	AuthPath     []types.Hash
	OTSSignature [][]byte
	PublicKey    []byte
}

// MerkleTree is a binary hash tree of OTS public keys.
type MerkleTree struct {
	Height int
	Nodes  []types.Hash // 2^(height+1) nodes, index 1 = root
	Leaves int
}

// HashSigScheme implements XMSS/LMS-style hash-based signatures.
// All methods are safe for concurrent use.
type HashSigScheme struct {
	mu              sync.Mutex
	height          int
	winternitzParam int // Winternitz parameter w (4 or 16)
	chainLen        int // number of OTS chains
}

// NewHashSigScheme creates a new hash-based signature scheme.
// height determines the tree depth (max signatures = 2^height).
// winternitzParam is the Winternitz parameter (4 or 16).
func NewHashSigScheme(height int, winternitzParam int) *HashSigScheme {
	if height < 1 {
		height = 10
	}
	if height > 20 {
		height = 20
	}
	if winternitzParam != 4 && winternitzParam != 16 {
		winternitzParam = 16
	}

	chainLen := hashSigChainLen
	if winternitzParam == 4 {
		// w=4: ceil(256/2) = 128 chains + checksum chains.
		chainLen = 133
	}

	return &HashSigScheme{
		height:          height,
		winternitzParam: winternitzParam,
		chainLen:        chainLen,
	}
}

// GenerateKeyPair generates a new hash-based key pair.
// The private key encodes the random seed and the current leaf index (0).
// The public key is the Merkle root of all OTS public keys.
func (s *HashSigScheme) GenerateKeyPair() (*HashSigKeyPair, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	seed := make([]byte, hashSigSeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}

	// Build the Merkle tree of OTS public keys.
	tree := s.buildTree(seed)

	// Private key = seed || leafIndex(0) as 4 bytes.
	privKey := make([]byte, hashSigSeedSize+4)
	copy(privKey, seed)
	// leafIndex starts at 0 (already zero-filled).

	maxSigs := 1 << s.height

	return &HashSigKeyPair{
		PublicKey:           tree.Nodes[1][:], // root
		PrivateKey:          privKey,
		Height:              s.height,
		RemainingSignatures: maxSigs,
	}, nil
}

// Sign creates a hash-based signature for the given message.
// Each call consumes one OTS key, decrementing RemainingSignatures.
func (s *HashSigScheme) Sign(privateKey []byte, message []byte) (*HashSignature, error) {
	if len(privateKey) < hashSigSeedSize+4 {
		return nil, ErrHashSigNilPrivateKey
	}
	if len(message) == 0 {
		return nil, ErrHashSigNilMessage
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	seed := privateKey[:hashSigSeedSize]
	leafIndex := binary.BigEndian.Uint32(privateKey[hashSigSeedSize:])

	maxLeaves := uint32(1 << s.height)
	if leafIndex >= maxLeaves {
		return nil, ErrHashSigNoKeysLeft
	}

	// Derive the OTS key pair for this leaf.
	otsPriv := s.deriveOTSPrivate(seed, leafIndex)

	// Hash the message to get the digest to sign.
	msgHash := crypto.Keccak256(message)

	// Produce the Winternitz OTS signature.
	otsSig := s.otsSignInternal(otsPriv, msgHash)

	// Build the Merkle tree and extract the authentication path.
	tree := s.buildTree(seed)
	authPath := s.authPath(tree, leafIndex)

	// Advance the leaf index in the private key.
	binary.BigEndian.PutUint32(privateKey[hashSigSeedSize:], leafIndex+1)

	return &HashSignature{
		LeafIndex:    leafIndex,
		AuthPath:     authPath,
		OTSSignature: otsSig,
		PublicKey:    tree.Nodes[1][:],
	}, nil
}

// Verify checks a hash-based signature against a public key and message.
func (s *HashSigScheme) Verify(publicKey []byte, message []byte, sig *HashSignature) bool {
	if len(publicKey) == 0 || len(message) == 0 || sig == nil {
		return false
	}
	if len(sig.OTSSignature) != s.chainLen {
		return false
	}
	if len(sig.AuthPath) != s.height {
		return false
	}
	if sig.LeafIndex >= uint32(1<<s.height) {
		return false
	}

	// Hash message.
	msgHash := crypto.Keccak256(message)

	// Recover the OTS public key from the signature.
	otsPub := s.otsRecoverPublic(sig.OTSSignature, msgHash)

	// Hash the OTS public key to get the leaf.
	leaf := crypto.Keccak256Hash(otsPub...)

	// Walk the authentication path to compute the root.
	computed := leaf
	idx := uint32(sig.LeafIndex)
	for _, sibling := range sig.AuthPath {
		if idx&1 == 0 {
			computed = crypto.Keccak256Hash(computed[:], sibling[:])
		} else {
			computed = crypto.Keccak256Hash(sibling[:], computed[:])
		}
		idx >>= 1
	}

	return computed == types.BytesToHash(publicKey)
}

// OTSSign creates a Winternitz one-time signature. The key should be a
// 32-byte secret; message should be a 32-byte hash digest.
func (s *HashSigScheme) OTSSign(key []byte, message []byte) [][]byte {
	if len(key) == 0 || len(message) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	msgHash := crypto.Keccak256(message)
	privChains := s.deriveOTSChains(key)
	return s.otsSignInternal(privChains, msgHash)
}

// OTSVerify verifies a Winternitz OTS signature.
func (s *HashSigScheme) OTSVerify(publicKey []byte, message []byte, sig [][]byte) bool {
	if len(publicKey) == 0 || len(message) == 0 || len(sig) != s.chainLen {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	msgHash := crypto.Keccak256(message)
	recovered := s.otsRecoverPublic(sig, msgHash)
	otsPubHash := crypto.Keccak256(recovered...)
	return types.BytesToHash(otsPubHash) == types.BytesToHash(publicKey)
}

// RemainingSignatures returns how many signatures can still be produced
// from the given private key.
func (s *HashSigScheme) RemainingSignatures(privateKey []byte) int {
	if len(privateKey) < hashSigSeedSize+4 {
		return 0
	}
	leafIndex := binary.BigEndian.Uint32(privateKey[hashSigSeedSize:])
	maxLeaves := uint32(1 << s.height)
	if leafIndex >= maxLeaves {
		return 0
	}
	return int(maxLeaves - leafIndex)
}

// BuildMerkleTree builds a Merkle tree from the given leaf hashes.
func BuildMerkleTree(leaves []types.Hash) *MerkleTree {
	n := len(leaves)
	if n == 0 {
		return &MerkleTree{Height: 0, Leaves: 0}
	}

	// Compute height.
	height := 0
	size := 1
	for size < n {
		size <<= 1
		height++
	}

	// Allocate nodes: indices 1..2*size-1 (1-indexed).
	nodes := make([]types.Hash, 2*size+1)

	// Fill leaves at positions [size..2*size-1].
	for i := 0; i < n; i++ {
		nodes[size+i] = leaves[i]
	}

	// Build internal nodes bottom-up.
	for i := size - 1; i >= 1; i-- {
		left := nodes[2*i]
		right := nodes[2*i+1]
		nodes[i] = crypto.Keccak256Hash(left[:], right[:])
	}

	return &MerkleTree{
		Height: height,
		Nodes:  nodes,
		Leaves: n,
	}
}

// --- internal helpers ---

// buildTree constructs the full Merkle tree of OTS public keys from a seed.
func (s *HashSigScheme) buildTree(seed []byte) *MerkleTree {
	numLeaves := 1 << s.height
	leaves := make([]types.Hash, numLeaves)

	for i := 0; i < numLeaves; i++ {
		otsPriv := s.deriveOTSPrivate(seed, uint32(i))
		otsPub := s.otsPublicFromPrivate(otsPriv)
		leaves[i] = crypto.Keccak256Hash(otsPub...)
	}

	return BuildMerkleTree(leaves)
}

// authPath extracts the authentication path for a given leaf index.
func (s *HashSigScheme) authPath(tree *MerkleTree, leafIndex uint32) []types.Hash {
	path := make([]types.Hash, s.height)
	size := 1 << s.height
	nodeIdx := int(leafIndex) + size

	for level := 0; level < s.height; level++ {
		// Sibling is the node at the same level on the other side.
		if nodeIdx&1 == 0 {
			path[level] = tree.Nodes[nodeIdx+1]
		} else {
			path[level] = tree.Nodes[nodeIdx-1]
		}
		nodeIdx >>= 1
	}
	return path
}

// deriveOTSPrivate derives the OTS private key chains for a leaf index.
func (s *HashSigScheme) deriveOTSPrivate(seed []byte, leafIndex uint32) [][]byte {
	idxBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(idxBuf, leafIndex)
	leafSeed := crypto.Keccak256(seed, idxBuf)
	return s.deriveOTSChains(leafSeed)
}

// deriveOTSChains derives chainLen private key chains from a seed.
func (s *HashSigScheme) deriveOTSChains(seed []byte) [][]byte {
	chains := make([][]byte, s.chainLen)
	for i := 0; i < s.chainLen; i++ {
		iBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(iBuf, uint32(i))
		chains[i] = crypto.Keccak256(seed, iBuf)
	}
	return chains
}

// otsPublicFromPrivate computes the OTS public key by hashing each
// private chain w-1 times.
func (s *HashSigScheme) otsPublicFromPrivate(privChains [][]byte) [][]byte {
	w := s.winternitzParam
	pub := make([][]byte, len(privChains))
	for i, chain := range privChains {
		val := make([]byte, len(chain))
		copy(val, chain)
		for j := 0; j < w-1; j++ {
			val = crypto.Keccak256(val)
		}
		pub[i] = val
	}
	return pub
}

// otsSignInternal produces a Winternitz OTS signature from private chains
// and a message hash. Each chain is hashed d_i times, where d_i is the
// i-th nibble (w=16) or crumb (w=4) of the message hash + checksum.
func (s *HashSigScheme) otsSignInternal(privChains [][]byte, msgHash []byte) [][]byte {
	digits := s.messageDigits(msgHash)
	sig := make([][]byte, s.chainLen)

	for i := 0; i < s.chainLen; i++ {
		val := make([]byte, len(privChains[i]))
		copy(val, privChains[i])
		d := 0
		if i < len(digits) {
			d = digits[i]
		}
		for j := 0; j < d; j++ {
			val = crypto.Keccak256(val)
		}
		sig[i] = val
	}
	return sig
}

// otsRecoverPublic recovers the OTS public key from a signature and
// message hash by completing the remaining hash iterations.
func (s *HashSigScheme) otsRecoverPublic(sig [][]byte, msgHash []byte) [][]byte {
	digits := s.messageDigits(msgHash)
	w := s.winternitzParam
	pub := make([][]byte, len(sig))

	for i, chain := range sig {
		val := make([]byte, len(chain))
		copy(val, chain)
		d := 0
		if i < len(digits) {
			d = digits[i]
		}
		remaining := w - 1 - d
		for j := 0; j < remaining; j++ {
			val = crypto.Keccak256(val)
		}
		pub[i] = val
	}
	return pub
}

// messageDigits converts a 32-byte message hash into a slice of Winternitz
// digits (each 0..w-1) plus checksum digits.
func (s *HashSigScheme) messageDigits(msgHash []byte) []int {
	w := s.winternitzParam
	var digits []int

	if w == 16 {
		// Each byte yields 2 nibbles.
		for _, b := range msgHash {
			digits = append(digits, int(b>>4))
			digits = append(digits, int(b&0x0f))
		}
	} else {
		// w=4: each byte yields 4 crumbs (2-bit groups).
		for _, b := range msgHash {
			digits = append(digits, int((b>>6)&0x03))
			digits = append(digits, int((b>>4)&0x03))
			digits = append(digits, int((b>>2)&0x03))
			digits = append(digits, int(b&0x03))
		}
	}

	// Compute checksum: sum of (w-1-d) for each digit.
	checksum := 0
	for _, d := range digits {
		checksum += (w - 1) - d
	}

	// Encode checksum as digits. For w=16, 3 extra digits; for w=4, 5 extra.
	numChecksumDigits := s.chainLen - len(digits)
	for i := numChecksumDigits - 1; i >= 0; i-- {
		digits = append(digits, checksum%w)
		checksum /= w
	}

	return digits
}
