// Post-Quantum L1 Hash-Based Full Signatures V2 (M+ roadmap)
//
// Implements a complete standalone XMSS/LMS-style hash-based signature scheme
// for L1 transaction signing. This is a separate, more complete L1-focused
// scheme distinct from the basic hash_sig.go and from l1_hash_sig.go. The API
// accepts explicit key pairs for signing, making it suitable for transaction
// signing workflows where the caller manages key material externally.
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

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// L1 V2 hash signature errors.
var (
	ErrL1KeyExhausted     = errors.New("pqc: L1 key pair exhausted, no OTS leaves remaining")
	ErrL1InvalidSignature = errors.New("pqc: L1 signature is invalid")
	ErrL1InvalidAuthPath  = errors.New("pqc: L1 auth path is invalid or does not match tree")
)

// L1 V2 hash signature constants.
const (
	l1v2SeedSize      = 32 // random seed size in bytes
	l1v2WDefault      = 16 // default Winternitz parameter
	l1v2HeightDefault = 10 // default tree height (1024 signatures)

	// l1v2ChainLen16 is the number of OTS chains for w=16.
	l1v2ChainLen16 = 67

	// l1v2ChainLen4 is the number of OTS chains for w=4.
	l1v2ChainLen4 = 133
)

// L1SignerConfig configures the L1 V2 hash-based signer.
type L1SignerConfig struct {
	TreeHeight        int    // Merkle tree height (1-20); max signatures = 2^height
	WinternitzParam   int    // Winternitz parameter (4 or 16)
	KeyLifetimeBlocks uint64 // advisory lifetime in blocks (0 = unlimited)
}

// DefaultL1SignerConfig returns a sensible default config.
func DefaultL1SignerConfig() L1SignerConfig {
	return L1SignerConfig{
		TreeHeight:        l1v2HeightDefault,
		WinternitzParam:   l1v2WDefault,
		KeyLifetimeBlocks: 0,
	}
}

// L1KeyPair holds a Merkle tree key pair for L1 hash-based signing.
// The UsedCount and MaxUses fields track how many OTS leaves have been consumed.
type L1KeyPair struct {
	PublicRoot   []byte // Merkle root of OTS public keys (32 bytes)
	PrivateSeeds []byte // seed material
	TreeHeight   int    // tree height
	UsedCount    uint64 // how many OTS keys have been used
	MaxUses      uint64 // maximum OTS keys available (2^height)
}

// L1Signature is a complete hash-based signature for L1 transactions.
type L1Signature struct {
	MessageHash []byte       // Keccak256 of signed message
	AuthPath    []types.Hash // Merkle authentication path from leaf to root
	LeafIndex   uint32       // which OTS leaf was used
	OTSSigBytes [][]byte     // Winternitz OTS signature chains
	PublicRoot  []byte       // Merkle root (public key) for verification
}

// L1HashSignerV2 manages a stateful XMSS-style hash-based signer.
// Unlike L1HashSigner, the Sign method accepts an explicit L1KeyPair,
// and internal state is tracked in the key pair itself.
// All methods are safe for concurrent use.
type L1HashSignerV2 struct {
	mu          sync.Mutex
	height      int
	winternitzW int
	chainLen    int
}

// NewL1HashSignerV2 creates a new L1 V2 hash-based signer from the given config.
func NewL1HashSignerV2(config L1SignerConfig) *L1HashSignerV2 {
	h := config.TreeHeight
	if h < 1 {
		h = l1v2HeightDefault
	}
	if h > 20 {
		h = 20
	}

	w := config.WinternitzParam
	if w != 4 && w != 16 {
		w = l1v2WDefault
	}

	chainLen := l1v2ChainLen16
	if w == 4 {
		chainLen = l1v2ChainLen4
	}

	return &L1HashSignerV2{
		height:      h,
		winternitzW: w,
		chainLen:    chainLen,
	}
}

// GenerateKeyPair generates a new XMSS-style Merkle tree key pair.
func (s *L1HashSignerV2) GenerateKeyPair() (*L1KeyPair, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	seed := make([]byte, l1v2SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}

	root := s.computeRootV2(seed)
	maxUses := uint64(1) << uint(s.height)

	return &L1KeyPair{
		PublicRoot:   dupSlice(root),
		PrivateSeeds: dupSlice(seed),
		TreeHeight:   s.height,
		UsedCount:    0,
		MaxUses:      maxUses,
	}, nil
}

// Sign creates a one-time hash-based signature for the given message using
// the provided key pair. Each call consumes one OTS leaf. Returns
// ErrL1KeyExhausted if all leaves have been used.
func (s *L1HashSignerV2) Sign(keyPair *L1KeyPair, message []byte) (*L1Signature, error) {
	if keyPair == nil {
		return nil, ErrL1KeyExhausted
	}
	if len(message) == 0 {
		return nil, ErrL1InvalidSignature
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if keyPair.UsedCount >= keyPair.MaxUses {
		return nil, ErrL1KeyExhausted
	}

	leafIndex := uint32(keyPair.UsedCount)

	// Hash the message.
	msgHash := crypto.Keccak256(message)

	// Derive OTS private key for this leaf.
	otsPriv := s.deriveOTSPrivV2(keyPair.PrivateSeeds, leafIndex)

	// Produce Winternitz OTS signature.
	otsSig := s.otsSignV2(otsPriv, msgHash)

	// Build tree and extract auth path.
	tree := s.buildTreeV2(keyPair.PrivateSeeds)
	authPath := s.extractAuthPathV2(tree, leafIndex)

	// Advance counter.
	keyPair.UsedCount++

	return &L1Signature{
		MessageHash: msgHash,
		AuthPath:    authPath,
		LeafIndex:   leafIndex,
		OTSSigBytes: otsSig,
		PublicRoot:  dupSlice(keyPair.PublicRoot),
	}, nil
}

// Verify checks a hash-based signature against a message.
// This is a stateless verification that does not require signer state.
func (s *L1HashSignerV2) Verify(sig *L1Signature, message []byte) (bool, error) {
	if sig == nil || len(message) == 0 || len(sig.PublicRoot) == 0 {
		return false, ErrL1InvalidSignature
	}
	if len(sig.OTSSigBytes) != s.chainLen {
		return false, ErrL1InvalidSignature
	}
	if len(sig.AuthPath) != s.height {
		return false, ErrL1InvalidAuthPath
	}
	if sig.LeafIndex >= uint32(1<<uint(s.height)) {
		return false, ErrL1InvalidAuthPath
	}

	// Hash message and compare.
	msgHash := crypto.Keccak256(message)

	// Recover OTS public key from signature.
	otsPub := s.otsRecoverV2(sig.OTSSigBytes, msgHash)

	// Hash the OTS public key to get the leaf hash.
	leaf := crypto.Keccak256Hash(otsPub...)

	// Walk the auth path from leaf to root.
	computed := leaf
	idx := sig.LeafIndex
	for _, sibling := range sig.AuthPath {
		if idx&1 == 0 {
			computed = crypto.Keccak256Hash(computed[:], sibling[:])
		} else {
			computed = crypto.Keccak256Hash(sibling[:], computed[:])
		}
		idx >>= 1
	}

	if computed != types.BytesToHash(sig.PublicRoot) {
		return false, ErrL1InvalidSignature
	}
	return true, nil
}

// RemainingSignatures returns how many OTS leaves remain for the key pair.
func (s *L1HashSignerV2) RemainingSignatures(keyPair *L1KeyPair) int {
	if keyPair == nil {
		return 0
	}
	if keyPair.UsedCount >= keyPair.MaxUses {
		return 0
	}
	return int(keyPair.MaxUses - keyPair.UsedCount)
}

// IsKeyExhausted returns true when all OTS keys have been used.
func (s *L1HashSignerV2) IsKeyExhausted(keyPair *L1KeyPair) bool {
	if keyPair == nil {
		return true
	}
	return keyPair.UsedCount >= keyPair.MaxUses
}

// --- internal helpers ---

// computeRootV2 builds the full Merkle tree and returns the root hash.
func (s *L1HashSignerV2) computeRootV2(seed []byte) []byte {
	tree := s.buildTreeV2(seed)
	return tree.Nodes[1][:]
}

// buildTreeV2 constructs the Merkle tree of OTS public keys.
func (s *L1HashSignerV2) buildTreeV2(seed []byte) *MerkleTree {
	numLeaves := 1 << uint(s.height)
	leaves := make([]types.Hash, numLeaves)

	for i := 0; i < numLeaves; i++ {
		otsPriv := s.deriveOTSPrivV2(seed, uint32(i))
		otsPub := s.otsPublicFromPrivV2(otsPriv)
		leaves[i] = crypto.Keccak256Hash(otsPub...)
	}

	return BuildMerkleTree(leaves)
}

// extractAuthPathV2 extracts the authentication path for the given leaf.
func (s *L1HashSignerV2) extractAuthPathV2(tree *MerkleTree, leafIndex uint32) []types.Hash {
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

// deriveOTSPrivV2 derives the OTS private key chains for a leaf index.
func (s *L1HashSignerV2) deriveOTSPrivV2(seed []byte, leafIndex uint32) [][]byte {
	idxBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(idxBuf, leafIndex)
	// Domain-separate with "l1v2-ots" to avoid collisions with other schemes.
	leafSeed := crypto.Keccak256(seed, idxBuf, []byte("l1v2-ots"))
	return s.deriveOTSChainsV2(leafSeed)
}

// deriveOTSChainsV2 derives chainLen private key chains from a seed.
func (s *L1HashSignerV2) deriveOTSChainsV2(seed []byte) [][]byte {
	chains := make([][]byte, s.chainLen)
	for i := 0; i < s.chainLen; i++ {
		iBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(iBuf, uint32(i))
		chains[i] = crypto.Keccak256(seed, iBuf)
	}
	return chains
}

// otsPublicFromPrivV2 computes OTS public key by hashing each chain w-1 times.
func (s *L1HashSignerV2) otsPublicFromPrivV2(privChains [][]byte) [][]byte {
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

// otsSignV2 produces a Winternitz OTS signature from private chains and msg hash.
func (s *L1HashSignerV2) otsSignV2(privChains [][]byte, msgHash []byte) [][]byte {
	digits := s.messageDigitsV2(msgHash)
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

// otsRecoverV2 recovers the OTS public key from signature and msg hash.
func (s *L1HashSignerV2) otsRecoverV2(sig [][]byte, msgHash []byte) [][]byte {
	digits := s.messageDigitsV2(msgHash)
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

// messageDigitsV2 converts a 32-byte hash into Winternitz digits plus checksum.
func (s *L1HashSignerV2) messageDigitsV2(msgHash []byte) []int {
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

// dupSlice returns a copy of a byte slice.
func dupSlice(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
