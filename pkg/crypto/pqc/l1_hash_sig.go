// Post-Quantum L1 Hash-Based Full Signatures (M+ roadmap)
//
// Implements a complete standalone XMSS/LMS-style hash-based signature scheme
// for L1 consensus. Unlike the existing hash_sig.go (which provides
// share-based signature operations), this provides a self-contained signer
// that manages its own key tree, signature counter, and key exhaustion.
//
// The scheme builds a Merkle tree of Winternitz one-time signature (OTS) public
// keys. Each signature consumes one OTS leaf, so the tree height determines the
// maximum number of signatures (2^height). Key exhaustion is tracked and
// enforced to prevent one-time key reuse.
//
// Security relies only on Keccak256 pre-image resistance, providing full
// post-quantum security without lattice or number-theoretic assumptions.
package pqc

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// L1 hash signature errors.
var (
	ErrL1HashKeysExhausted     = errors.New("pqc: L1 hash signer keys exhausted")
	ErrL1HashInvalidSignature  = errors.New("pqc: L1 hash signature invalid")
	ErrL1HashInvalidTreeHeight = errors.New("pqc: L1 hash tree height must be 1-20")
)

// L1 hash signature constants.
const (
	l1SeedSize      = 32 // random seed size in bytes
	l1WDefault      = 16 // default Winternitz parameter
	l1HeightDefault = 10 // default tree height (1024 signatures)

	// l1ChainLen16 is the number of OTS chains for w=16:
	// ceil(256/4) = 64 message chains + 3 checksum chains.
	l1ChainLen16 = 67
)

// L1HashSignerConfig configures the L1 hash-based signer.
type L1HashSignerConfig struct {
	TreeHeight    int    // Merkle tree height (1-20); max signatures = 2^height
	WinternitzW   int    // Winternitz parameter (4 or 16)
	HashFunction  string // hash function name (only "keccak256" supported)
	MaxSignatures uint64 // explicit cap (0 = use 2^height)
}

// DefaultL1HashSignerConfig returns a sensible default config.
func DefaultL1HashSignerConfig() L1HashSignerConfig {
	return L1HashSignerConfig{
		TreeHeight:    l1HeightDefault,
		WinternitzW:   l1WDefault,
		HashFunction:  "keccak256",
		MaxSignatures: 0,
	}
}

// L1HashKeyPair holds an XMSS-style key pair for L1 hash-based signing.
type L1HashKeyPair struct {
	PublicRoot          []byte    // Merkle root of OTS public keys
	PrivateSeeds        []byte    // seed material (l1SeedSize bytes)
	RemainingSignatures uint64    // how many OTS keys are left
	CreatedTime         time.Time // when the key pair was generated
}

// L1HashSignature is a complete hash-based signature for L1 consensus.
type L1HashSignature struct {
	MessageHash  []byte       // Keccak256 of signed message
	AuthPath     []types.Hash // Merkle authentication path from leaf to root
	LeafIndex    uint32       // which OTS leaf was used
	OTSSignature [][]byte     // Winternitz OTS signature chains
	TreeRoot     []byte       // Merkle root (public key) for verification
}

// L1HashSigner manages a stateful XMSS-style hash-based signer.
// It tracks signature usage and prevents one-time key reuse.
// All methods are safe for concurrent use.
type L1HashSigner struct {
	mu            sync.Mutex
	height        int
	winternitzW   int
	chainLen      int
	maxSignatures uint64
	sigCounter    uint64 // next leaf index to use
	seed          []byte // private seed
	publicRoot    []byte // cached Merkle root
	createdTime   time.Time
	initialized   bool
}

// NewL1HashSigner creates a new L1 hash-based signer from the given config.
// Returns an error if the tree height is out of range.
func NewL1HashSigner(config L1HashSignerConfig) (*L1HashSigner, error) {
	if config.TreeHeight < 1 || config.TreeHeight > 20 {
		return nil, ErrL1HashInvalidTreeHeight
	}
	w := config.WinternitzW
	if w != 4 && w != 16 {
		w = l1WDefault
	}

	chainLen := l1ChainLen16
	if w == 4 {
		// w=4: ceil(256/2) = 128 chains + 5 checksum chains.
		chainLen = 133
	}

	maxSigs := uint64(1) << uint(config.TreeHeight)
	if config.MaxSignatures > 0 && config.MaxSignatures < maxSigs {
		maxSigs = config.MaxSignatures
	}

	return &L1HashSigner{
		height:        config.TreeHeight,
		winternitzW:   w,
		chainLen:      chainLen,
		maxSignatures: maxSigs,
	}, nil
}

// GenerateKeyPair generates a new XMSS-style key pair. The signer must not
// already have a key pair (create a new signer for a new key).
func (s *L1HashSigner) GenerateKeyPair() (*L1HashKeyPair, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	seed := make([]byte, l1SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}

	// Build the Merkle tree to compute the public root.
	root := s.computeRoot(seed)

	s.seed = seed
	s.publicRoot = root
	s.sigCounter = 0
	s.createdTime = time.Now()
	s.initialized = true

	return &L1HashKeyPair{
		PublicRoot:          cloneBytes(root),
		PrivateSeeds:        cloneBytes(seed),
		RemainingSignatures: s.maxSignatures,
		CreatedTime:         s.createdTime,
	}, nil
}

// Sign creates a one-time hash-based signature for the given message.
// Each call consumes one OTS leaf. Returns ErrL1HashKeysExhausted if all
// leaves have been used.
func (s *L1HashSigner) Sign(message []byte) (*L1HashSignature, error) {
	if len(message) == 0 {
		return nil, ErrL1HashInvalidSignature
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.initialized {
		return nil, ErrL1HashKeysExhausted
	}
	if s.sigCounter >= s.maxSignatures {
		return nil, ErrL1HashKeysExhausted
	}

	leafIndex := uint32(s.sigCounter)

	// Hash the message.
	msgHash := crypto.Keccak256(message)

	// Derive OTS private key for this leaf.
	otsPriv := s.deriveOTSPrivateL1(s.seed, leafIndex)

	// Produce Winternitz OTS signature.
	otsSig := s.otsSignL1(otsPriv, msgHash)

	// Build tree and extract auth path.
	tree := s.buildTreeL1(s.seed)
	authPath := s.extractAuthPath(tree, leafIndex)

	// Advance counter.
	s.sigCounter++

	return &L1HashSignature{
		MessageHash:  msgHash,
		AuthPath:     authPath,
		LeafIndex:    leafIndex,
		OTSSignature: otsSig,
		TreeRoot:     cloneBytes(s.publicRoot),
	}, nil
}

// Verify checks a hash-based signature against a message and public root.
// This is a static method that does not require signer state.
func (s *L1HashSigner) Verify(message []byte, sig *L1HashSignature, pubRoot []byte) (bool, error) {
	if len(message) == 0 || sig == nil || len(pubRoot) == 0 {
		return false, ErrL1HashInvalidSignature
	}
	if len(sig.OTSSignature) != s.chainLen {
		return false, ErrL1HashInvalidSignature
	}
	if len(sig.AuthPath) != s.height {
		return false, ErrL1HashInvalidSignature
	}
	if sig.LeafIndex >= uint32(1<<uint(s.height)) {
		return false, ErrL1HashInvalidSignature
	}

	// Hash message and compare.
	msgHash := crypto.Keccak256(message)

	// Recover OTS public key from signature.
	otsPub := s.otsRecoverL1(sig.OTSSignature, msgHash)

	// Hash the OTS public key to get the leaf hash.
	leaf := crypto.Keccak256Hash(otsPub...)

	// Walk the auth path from leaf to root.
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

	if computed != types.BytesToHash(pubRoot) {
		return false, ErrL1HashInvalidSignature
	}
	return true, nil
}

// RemainingSignatures returns how many more signatures can be produced.
func (s *L1HashSigner) RemainingSignatures() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		return 0
	}
	if s.sigCounter >= s.maxSignatures {
		return 0
	}
	return s.maxSignatures - s.sigCounter
}

// IsExhausted returns true when all OTS keys have been used.
func (s *L1HashSigner) IsExhausted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		return true
	}
	return s.sigCounter >= s.maxSignatures
}

// --- internal helpers ---

// computeRoot builds the full Merkle tree and returns the root hash.
func (s *L1HashSigner) computeRoot(seed []byte) []byte {
	tree := s.buildTreeL1(seed)
	return tree.Nodes[1][:]
}

// buildTreeL1 constructs the Merkle tree of OTS public keys.
func (s *L1HashSigner) buildTreeL1(seed []byte) *MerkleTree {
	numLeaves := 1 << uint(s.height)
	leaves := make([]types.Hash, numLeaves)

	for i := 0; i < numLeaves; i++ {
		otsPriv := s.deriveOTSPrivateL1(seed, uint32(i))
		otsPub := s.otsPublicFromPrivateL1(otsPriv)
		leaves[i] = crypto.Keccak256Hash(otsPub...)
	}

	return BuildMerkleTree(leaves)
}

// extractAuthPath extracts the authentication path for the given leaf.
func (s *L1HashSigner) extractAuthPath(tree *MerkleTree, leafIndex uint32) []types.Hash {
	path := make([]types.Hash, s.height)
	size := 1 << uint(s.height)
	nodeIdx := int(leafIndex) + size

	for level := 0; level < s.height; level++ {
		if nodeIdx&1 == 0 {
			path[level] = tree.Nodes[nodeIdx+1]
		} else {
			path[level] = tree.Nodes[nodeIdx-1]
		}
		nodeIdx >>= 1
	}
	return path
}

// deriveOTSPrivateL1 derives the OTS private key chains for a leaf index.
func (s *L1HashSigner) deriveOTSPrivateL1(seed []byte, leafIndex uint32) [][]byte {
	idxBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(idxBuf, leafIndex)
	// Domain-separate from the existing hash_sig.go by including "l1-ots".
	leafSeed := crypto.Keccak256(seed, idxBuf, []byte("l1-ots"))
	return s.deriveOTSChainsL1(leafSeed)
}

// deriveOTSChainsL1 derives chainLen private key chains from a seed.
func (s *L1HashSigner) deriveOTSChainsL1(seed []byte) [][]byte {
	chains := make([][]byte, s.chainLen)
	for i := 0; i < s.chainLen; i++ {
		iBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(iBuf, uint32(i))
		chains[i] = crypto.Keccak256(seed, iBuf)
	}
	return chains
}

// otsPublicFromPrivateL1 computes OTS public key by hashing each chain w-1 times.
func (s *L1HashSigner) otsPublicFromPrivateL1(privChains [][]byte) [][]byte {
	w := s.winternitzW
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

// otsSignL1 produces a Winternitz OTS signature from private chains and msg hash.
func (s *L1HashSigner) otsSignL1(privChains [][]byte, msgHash []byte) [][]byte {
	digits := s.messageDigitsL1(msgHash)
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

// otsRecoverL1 recovers the OTS public key from signature and msg hash.
func (s *L1HashSigner) otsRecoverL1(sig [][]byte, msgHash []byte) [][]byte {
	digits := s.messageDigitsL1(msgHash)
	w := s.winternitzW
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

// messageDigitsL1 converts a 32-byte hash into Winternitz digits plus checksum.
func (s *L1HashSigner) messageDigitsL1(msgHash []byte) []int {
	w := s.winternitzW
	var digits []int

	if w == 16 {
		for _, b := range msgHash {
			digits = append(digits, int(b>>4))
			digits = append(digits, int(b&0x0f))
		}
	} else {
		for _, b := range msgHash {
			digits = append(digits, int((b>>6)&0x03))
			digits = append(digits, int((b>>4)&0x03))
			digits = append(digits, int((b>>2)&0x03))
			digits = append(digits, int(b&0x03))
		}
	}

	// Compute checksum.
	checksum := 0
	for _, d := range digits {
		checksum += (w - 1) - d
	}

	numChecksumDigits := s.chainLen - len(digits)
	for i := numChecksumDigits - 1; i >= 0; i-- {
		digits = append(digits, checksum%w)
		checksum /= w
	}

	return digits
}

// cloneBytes returns a copy of a byte slice.
func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
