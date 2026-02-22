// unified_hash_signer.go unifies the PQ L1 hash-based signature schemes
// from l1_hash_sig.go (V1) and l1_hash_sig_v2.go (V2) into a single
// XMSS (Extended Merkle Signature Scheme) implementation with WOTS+
// one-time signatures.
//
// Key features:
//   - XMSS with WOTS+ one-time signatures (SHA-256 based)
//   - Configurable tree heights: H=10 (1024 sigs), H=16 (65536 sigs), H=20 (1M sigs)
//   - Key exhaustion tracking with automatic rotation signaling
//   - XMSSKeyManager for multi-tree management
//   - SHA-256 throughout for quantum resistance
package pqc

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Unified hash signer errors.
var (
	ErrXMSSInvalidHeight     = errors.New("xmss: tree height must be 10, 16, or 20")
	ErrXMSSKeysExhausted     = errors.New("xmss: all OTS keys exhausted")
	ErrXMSSNotInitialized    = errors.New("xmss: key pair not initialized")
	ErrXMSSInvalidSignature  = errors.New("xmss: invalid signature")
	ErrXMSSInvalidAuthPath   = errors.New("xmss: auth path length mismatch")
	ErrXMSSInvalidLeafIndex  = errors.New("xmss: leaf index out of range")
	ErrXMSSEmptyMessage      = errors.New("xmss: empty message")
	ErrXMSSRotationNeeded    = errors.New("xmss: key rotation required")
	ErrXMSSManagerEmpty      = errors.New("xmss: key manager has no active trees")
)

// Supported tree heights.
const (
	XMSSHeight10 = 10 // 1024 signatures
	XMSSHeight16 = 16 // 65536 signatures
	XMSSHeight20 = 20 // 1048576 signatures
)

// XMSS constants.
const (
	xmssSeedSize   = 32  // SHA-256 output / seed size
	xmssWOTSW      = 16  // Winternitz parameter
	xmssChainLen   = 67  // ceil(256/4) + 3 checksum chains for w=16
	xmssRotateWarn = 90  // warn at 90% exhaustion
)

// Domain separators for XMSS SHA-256 hashing.
var (
	xmssDomainOTS  = []byte("xmss-ots-v1")
	xmssDomainTree = []byte("xmss-tree-v1")
	xmssDomainMsg  = []byte("xmss-msg-v1")
	xmssDomainPRF  = []byte("xmss-prf-v1")
)

// xmssSHA256 computes SHA-256 with domain separation.
func xmssSHA256(domain []byte, data ...[]byte) [32]byte {
	h := sha256.New()
	h.Write(domain)
	for _, d := range data {
		h.Write(d)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// XMSSPublicKey is the public key for XMSS signature verification.
type XMSSPublicKey struct {
	Root   [32]byte // Merkle tree root
	Height int      // Tree height
}

// XMSSPrivateKey is the private key material for XMSS signing.
type XMSSPrivateKey struct {
	Seed       [32]byte // Random seed for key derivation
	Height     int      // Tree height
	MaxLeaves  uint64   // Maximum signatures (2^height)
	UsedLeaves uint64   // Number of OTS keys consumed
}

// XMSSSignature is a complete XMSS signature.
type XMSSSignature struct {
	LeafIndex    uint32
	AuthPath     [][32]byte // Merkle authentication path
	OTSSignature [][32]byte // WOTS+ signature chains
	PublicRoot   [32]byte   // For verification
}

// GenerateXMSSKeyPair generates an XMSS key pair with the given tree height.
// Supported heights: 10 (1024 sigs), 16 (65536 sigs), 20 (1M sigs).
func GenerateXMSSKeyPair(height int) (*XMSSPublicKey, *XMSSPrivateKey, error) {
	if height != XMSSHeight10 && height != XMSSHeight16 && height != XMSSHeight20 {
		return nil, nil, ErrXMSSInvalidHeight
	}

	var seed [32]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, nil, err
	}

	maxLeaves := uint64(1) << uint(height)
	root := xmssComputeRoot(seed, height)

	pk := &XMSSPublicKey{Root: root, Height: height}
	sk := &XMSSPrivateKey{
		Seed:       seed,
		Height:     height,
		MaxLeaves:  maxLeaves,
		UsedLeaves: 0,
	}
	return pk, sk, nil
}

// XMSSSign signs a message using the next available OTS key.
// Returns the signature, auth path, and leaf index used.
func XMSSSign(sk *XMSSPrivateKey, msg []byte) (*XMSSSignature, error) {
	if sk == nil {
		return nil, ErrXMSSNotInitialized
	}
	if len(msg) == 0 {
		return nil, ErrXMSSEmptyMessage
	}
	if sk.UsedLeaves >= sk.MaxLeaves {
		return nil, ErrXMSSKeysExhausted
	}

	leafIndex := uint32(sk.UsedLeaves)

	// Hash the message with domain separation.
	msgHash := xmssSHA256(xmssDomainMsg, msg)

	// Derive OTS private key for this leaf.
	otsPriv := xmssDeriveOTSPrivate(sk.Seed, leafIndex)

	// Produce WOTS+ signature.
	otsSig := xmssWOTSSign(otsPriv, msgHash)

	// Build tree and extract auth path.
	tree := xmssBuildTree(sk.Seed, sk.Height)
	authPath := xmssExtractAuthPath(tree, leafIndex, sk.Height)

	// Advance leaf counter.
	sk.UsedLeaves++

	return &XMSSSignature{
		LeafIndex:    leafIndex,
		AuthPath:     authPath,
		OTSSignature: otsSig,
		PublicRoot:    tree[1],
	}, nil
}

// XMSSVerify verifies an XMSS signature against a public key and message.
func XMSSVerify(pk *XMSSPublicKey, msg []byte, sig *XMSSSignature) bool {
	if pk == nil || sig == nil || len(msg) == 0 {
		return false
	}
	if len(sig.OTSSignature) != xmssChainLen {
		return false
	}
	if len(sig.AuthPath) != pk.Height {
		return false
	}
	if sig.LeafIndex >= uint32(1<<uint(pk.Height)) {
		return false
	}

	// Hash the message.
	msgHash := xmssSHA256(xmssDomainMsg, msg)

	// Recover OTS public key from signature.
	otsPub := xmssWOTSRecover(sig.OTSSignature, msgHash)

	// Hash to get leaf value.
	var pubConcat []byte
	for _, chain := range otsPub {
		pubConcat = append(pubConcat, chain[:]...)
	}
	leaf := xmssSHA256(xmssDomainTree, pubConcat)

	// Walk auth path to root.
	current := leaf
	idx := uint32(sig.LeafIndex)
	for _, sibling := range sig.AuthPath {
		if idx&1 == 0 {
			current = xmssSHA256(xmssDomainTree, current[:], sibling[:])
		} else {
			current = xmssSHA256(xmssDomainTree, sibling[:], current[:])
		}
		idx >>= 1
	}

	return current == pk.Root
}

// XMSSKeyManager manages multiple XMSS trees for automatic key rotation.
// When one tree is exhausted, it rotates to the next available tree.
type XMSSKeyManager struct {
	mu     sync.Mutex
	trees  []*xmssTreeEntry
	active int
	height int
}

type xmssTreeEntry struct {
	pk *XMSSPublicKey
	sk *XMSSPrivateKey
}

// NewXMSSKeyManager creates a key manager with the given tree height.
// It pre-generates one tree; additional trees are added on demand.
func NewXMSSKeyManager(height int) (*XMSSKeyManager, error) {
	pk, sk, err := GenerateXMSSKeyPair(height)
	if err != nil {
		return nil, err
	}
	mgr := &XMSSKeyManager{
		height: height,
		active: 0,
		trees: []*xmssTreeEntry{
			{pk: pk, sk: sk},
		},
	}
	return mgr, nil
}

// Sign signs a message using the active tree. Automatically rotates
// to the next tree if the current one is exhausted.
func (m *XMSSKeyManager) Sign(msg []byte) (*XMSSSignature, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.trees) == 0 {
		return nil, ErrXMSSManagerEmpty
	}

	entry := m.trees[m.active]

	// Check if rotation is needed.
	if entry.sk.UsedLeaves >= entry.sk.MaxLeaves {
		// Try to rotate to next tree.
		if m.active+1 < len(m.trees) {
			m.active++
			entry = m.trees[m.active]
		} else {
			// Generate a new tree.
			pk, sk, err := GenerateXMSSKeyPair(m.height)
			if err != nil {
				return nil, err
			}
			m.trees = append(m.trees, &xmssTreeEntry{pk: pk, sk: sk})
			m.active = len(m.trees) - 1
			entry = m.trees[m.active]
		}
	}

	return XMSSSign(entry.sk, msg)
}

// ActivePublicKey returns the public key of the currently active tree.
func (m *XMSSKeyManager) ActivePublicKey() *XMSSPublicKey {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.trees) == 0 {
		return nil
	}
	return m.trees[m.active].pk
}

// RemainingSignatures returns the remaining OTS keys in the active tree.
func (m *XMSSKeyManager) RemainingSignatures() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.trees) == 0 {
		return 0
	}
	entry := m.trees[m.active]
	if entry.sk.UsedLeaves >= entry.sk.MaxLeaves {
		return 0
	}
	return entry.sk.MaxLeaves - entry.sk.UsedLeaves
}

// NeedsRotation returns true if the active tree is >90% exhausted.
func (m *XMSSKeyManager) NeedsRotation() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.trees) == 0 {
		return true
	}
	entry := m.trees[m.active]
	threshold := entry.sk.MaxLeaves * xmssRotateWarn / 100
	return entry.sk.UsedLeaves >= threshold
}

// TreeCount returns the total number of managed trees.
func (m *XMSSKeyManager) TreeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.trees)
}

// --- internal helpers ---

// xmssComputeRoot builds the Merkle tree and returns the root.
func xmssComputeRoot(seed [32]byte, height int) [32]byte {
	tree := xmssBuildTree(seed, height)
	return tree[1]
}

// xmssBuildTree constructs the Merkle tree of WOTS+ public keys.
// Returns a 1-indexed array where tree[1] is the root.
func xmssBuildTree(seed [32]byte, height int) [][32]byte {
	numLeaves := 1 << uint(height)
	size := 2 * numLeaves
	tree := make([][32]byte, size)

	// Compute leaf nodes.
	for i := 0; i < numLeaves; i++ {
		otsPriv := xmssDeriveOTSPrivate(seed, uint32(i))
		otsPub := xmssWOTSPublicFromPrivate(otsPriv)
		var pubConcat []byte
		for _, chain := range otsPub {
			pubConcat = append(pubConcat, chain[:]...)
		}
		tree[numLeaves+i] = xmssSHA256(xmssDomainTree, pubConcat)
	}

	// Build internal nodes bottom-up.
	for i := numLeaves - 1; i >= 1; i-- {
		tree[i] = xmssSHA256(xmssDomainTree, tree[2*i][:], tree[2*i+1][:])
	}

	return tree
}

// xmssExtractAuthPath extracts the authentication path for a leaf.
func xmssExtractAuthPath(tree [][32]byte, leafIndex uint32, height int) [][32]byte {
	path := make([][32]byte, height)
	numLeaves := 1 << uint(height)
	nodeIdx := int(leafIndex) + numLeaves

	for level := 0; level < height; level++ {
		if nodeIdx&1 == 0 {
			path[level] = tree[nodeIdx+1]
		} else {
			path[level] = tree[nodeIdx-1]
		}
		nodeIdx >>= 1
	}
	return path
}

// xmssDeriveOTSPrivate derives WOTS+ private key chains for a leaf.
func xmssDeriveOTSPrivate(seed [32]byte, leafIndex uint32) [][32]byte {
	var idxBuf [4]byte
	binary.BigEndian.PutUint32(idxBuf[:], leafIndex)
	leafSeed := xmssSHA256(xmssDomainPRF, seed[:], idxBuf[:])

	chains := make([][32]byte, xmssChainLen)
	for i := 0; i < xmssChainLen; i++ {
		var iBuf [4]byte
		binary.BigEndian.PutUint32(iBuf[:], uint32(i))
		chains[i] = xmssSHA256(xmssDomainOTS, leafSeed[:], iBuf[:])
	}
	return chains
}

// xmssWOTSPublicFromPrivate hashes each chain w-1 times.
func xmssWOTSPublicFromPrivate(privChains [][32]byte) [][32]byte {
	pub := make([][32]byte, len(privChains))
	for i, chain := range privChains {
		val := chain
		for j := 0; j < xmssWOTSW-1; j++ {
			val = sha256.Sum256(val[:])
		}
		pub[i] = val
	}
	return pub
}

// xmssWOTSSign produces a WOTS+ signature.
func xmssWOTSSign(privChains [][32]byte, msgHash [32]byte) [][32]byte {
	digits := xmssMessageDigits(msgHash[:])
	sig := make([][32]byte, xmssChainLen)

	for i := 0; i < xmssChainLen; i++ {
		val := privChains[i]
		d := 0
		if i < len(digits) {
			d = digits[i]
		}
		for j := 0; j < d; j++ {
			val = sha256.Sum256(val[:])
		}
		sig[i] = val
	}
	return sig
}

// xmssWOTSRecover recovers the WOTS+ public key from a signature.
func xmssWOTSRecover(sig [][32]byte, msgHash [32]byte) [][32]byte {
	digits := xmssMessageDigits(msgHash[:])
	pub := make([][32]byte, len(sig))

	for i, chain := range sig {
		val := chain
		d := 0
		if i < len(digits) {
			d = digits[i]
		}
		remaining := xmssWOTSW - 1 - d
		for j := 0; j < remaining; j++ {
			val = sha256.Sum256(val[:])
		}
		pub[i] = val
	}
	return pub
}

// xmssMessageDigits converts a hash to Winternitz digits plus checksum.
func xmssMessageDigits(msgHash []byte) []int {
	var digits []int
	for _, b := range msgHash {
		digits = append(digits, int(b>>4))
		digits = append(digits, int(b&0x0f))
	}

	checksum := 0
	for _, d := range digits {
		checksum += (xmssWOTSW - 1) - d
	}

	numChecksumDigits := xmssChainLen - len(digits)
	for i := numChecksumDigits - 1; i >= 0; i-- {
		digits = append(digits, checksum%xmssWOTSW)
		checksum /= xmssWOTSW
	}
	return digits
}

// MaxSignaturesForHeight returns the maximum signatures for a given tree height.
func MaxSignaturesForHeight(height int) uint64 {
	if height < 1 || height > 20 {
		return 0
	}
	return uint64(1) << uint(height)
}

// XMSSPublicKeys returns all public keys managed by the key manager.
func (m *XMSSKeyManager) XMSSPublicKeys() []types.Hash {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]types.Hash, len(m.trees))
	for i, entry := range m.trees {
		keys[i] = types.Hash(entry.pk.Root)
	}
	return keys
}
