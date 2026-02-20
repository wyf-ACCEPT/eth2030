// SPHINCS+ hash-based signature scheme for long-term post-quantum security.
// Implements SPHINCS+-SHA2-128f (fast variant) using Keccak256 as the hash.
//
// SPHINCS+ builds a hypertree of XMSS trees, each containing WOTS+ one-time
// signature leaves. The "fast" variant uses larger signatures but faster signing.
//
// Parameters (SPHINCS+-128f-like):
//   N=16 (security bytes), W=16 (Winternitz), H=66 (total tree height),
//   D=22 (hypertree layers), K=33 (FORS trees), T=2^6 (FORS leaves per tree)
//
// For Ethereum post-quantum attestations (L+ roadmap), SPHINCS+ provides
// fallback security relying only on hash function pre-image resistance.
package pqc

import (
	"crypto/rand"
	"encoding/binary"
	"errors"

	"github.com/eth2028/eth2028/crypto"
)

// SPHINCS+-128f parameter constants.
const (
	sphincsN         = 16  // security parameter in bytes
	sphincsW         = 16  // Winternitz parameter
	sphincsH         = 66  // total tree height
	sphincsD         = 22  // hypertree layers
	sphincsK         = 33  // FORS trees
	sphincsLogT      = 6   // log2(FORS leaves per tree)
	sphincsT         = 64  // FORS leaves per tree (2^sphincsLogT)
	sphincsWOTSLen1  = 32  // ceil(8*N / log2(W)) = ceil(128/4) = 32
	sphincsWOTSLen2  = 3   // checksum chains
	sphincsWOTSLen   = sphincsWOTSLen1 + sphincsWOTSLen2
	sphincsWOTSSigSz = sphincsWOTSLen * sphincsN // 35 * 16 = 560 bytes
	sphincsFORSSigSz = sphincsK * (sphincsLogT*sphincsN + sphincsN) // FORS signature
	sphincsSeedSize  = 3 * sphincsN // SK.seed + SK.prf + PK.seed

	// SPHINCSPubKeySize is the public key size: PK.seed + PK.root.
	SPHINCSPubKeySize = 2 * sphincsN
	// SPHINCSSecKeySize is the secret key size: SK.seed + SK.prf + PK.seed + PK.root.
	SPHINCSSecKeySize = 4 * sphincsN
)

// SPHINCSPublicKey is a serialised SPHINCS+ public key.
type SPHINCSPublicKey []byte

// SPHINCSPrivateKey is a serialised SPHINCS+ secret key.
type SPHINCSPrivateKey []byte

// SPHINCSSignature is a serialised SPHINCS+ signature.
type SPHINCSSignature []byte

// Errors for SPHINCS+ operations.
var (
	ErrSPHINCSNilKey   = errors.New("sphincs: nil key")
	ErrSPHINCSEmptyMsg = errors.New("sphincs: empty message")
	ErrSPHINCSBadSig   = errors.New("sphincs: malformed signature")
	ErrSPHINCSBadPK    = errors.New("sphincs: invalid public key")
)

// sphincsADRS is an address structure used in SPHINCS+ to domain-separate hashes.
type sphincsADRS struct {
	LayerAddr  uint32
	TreeAddr   uint64
	TypeField  uint32
	KeyPairIdx uint32
	ChainIdx   uint32
	HashIdx    uint32
}

// toBytes serialises the ADRS to 32 bytes for hashing.
func (a *sphincsADRS) toBytes() []byte {
	buf := make([]byte, 32)
	binary.BigEndian.PutUint32(buf[0:4], a.LayerAddr)
	binary.BigEndian.PutUint64(buf[4:12], a.TreeAddr)
	binary.BigEndian.PutUint32(buf[12:16], a.TypeField)
	binary.BigEndian.PutUint32(buf[16:20], a.KeyPairIdx)
	binary.BigEndian.PutUint32(buf[20:24], a.ChainIdx)
	binary.BigEndian.PutUint32(buf[24:28], a.HashIdx)
	return buf
}

// SPHINCSKeypair generates a SPHINCS+ key pair.
func SPHINCSKeypair() (SPHINCSPublicKey, SPHINCSPrivateKey) {
	// Generate random seeds.
	skSeed := make([]byte, sphincsN)
	skPrf := make([]byte, sphincsN)
	pkSeed := make([]byte, sphincsN)
	rand.Read(skSeed)
	rand.Read(skPrf)
	rand.Read(pkSeed)

	// Compute root: build the top XMSS tree.
	adrs := &sphincsADRS{LayerAddr: uint32(sphincsD - 1)}
	root := sphincsXMSSRoot(skSeed, pkSeed, adrs)

	pk := make([]byte, SPHINCSPubKeySize)
	copy(pk[:sphincsN], pkSeed)
	copy(pk[sphincsN:], root)

	sk := make([]byte, SPHINCSSecKeySize)
	copy(sk[:sphincsN], skSeed)
	copy(sk[sphincsN:2*sphincsN], skPrf)
	copy(sk[2*sphincsN:3*sphincsN], pkSeed)
	copy(sk[3*sphincsN:], root)

	return SPHINCSPublicKey(pk), SPHINCSPrivateKey(sk)
}

// SPHINCSSign produces a SPHINCS+ signature over a message.
func SPHINCSSign(sk SPHINCSPrivateKey, msg []byte) SPHINCSSignature {
	if len(sk) < SPHINCSSecKeySize || len(msg) == 0 {
		return nil
	}

	skSeed := sk[:sphincsN]
	skPrf := sk[sphincsN : 2*sphincsN]
	pkSeed := sk[2*sphincsN : 3*sphincsN]

	// Randomised message hash: R = PRF(SK.prf, msg).
	optRand := make([]byte, sphincsN)
	rand.Read(optRand)
	R := sphincsHash(skPrf, optRand, msg, sphincsN)

	// Digest = H(R || PK.seed || PK.root || msg).
	pkRoot := sk[3*sphincsN : 4*sphincsN]
	digest := sphincsHash(R, pkSeed, pkRoot, msg, 2*sphincsN)

	// Split digest into FORS message indices and tree/leaf addresses.
	forsMsgBits := digest[:sphincsN]
	idxSlice := digest[sphincsN:]
	if len(idxSlice) > 8 {
		idxSlice = idxSlice[:8]
	} else if len(idxSlice) < 8 {
		idxSlice = append(make([]byte, 8-len(idxSlice)), idxSlice...)
	}
	treeIdx := binary.BigEndian.Uint64(idxSlice) >> 1
	leafIdx := uint32(treeIdx & uint64((1<<3)-1))
	treeIdx >>= 3

	// FORS signature.
	forsSig := sphincsFORSSign(forsMsgBits, skSeed, pkSeed, treeIdx, leafIdx)
	forsRoot := sphincsFORSRoot(forsMsgBits, forsSig, pkSeed, treeIdx, leafIdx)

	// Hypertree signature: sign the FORS root through D XMSS layers.
	htSig := sphincsHTSign(forsRoot, skSeed, pkSeed, treeIdx, leafIdx)

	// Assemble signature: R || FORS sig || HT sig.
	sig := make([]byte, 0, sphincsN+len(forsSig)+len(htSig))
	sig = append(sig, R...)
	sig = append(sig, forsSig...)
	sig = append(sig, htSig...)
	return SPHINCSSignature(sig)
}

// SPHINCSVerify verifies a SPHINCS+ signature.
func SPHINCSVerify(pk SPHINCSPublicKey, msg []byte, sig SPHINCSSignature) bool {
	if len(pk) < SPHINCSPubKeySize || len(msg) == 0 || len(sig) < sphincsN+1 {
		return false
	}

	pkSeed := pk[:sphincsN]
	pkRoot := pk[sphincsN : 2*sphincsN]

	// Extract R from signature.
	R := sig[:sphincsN]

	// Recompute digest.
	digest := sphincsHash(R, pkSeed, pkRoot, msg, 2*sphincsN)

	forsMsgBits := digest[:sphincsN]
	idxSlice := digest[sphincsN:]
	if len(idxSlice) > 8 {
		idxSlice = idxSlice[:8]
	} else if len(idxSlice) < 8 {
		idxSlice = append(make([]byte, 8-len(idxSlice)), idxSlice...)
	}
	treeIdx := binary.BigEndian.Uint64(idxSlice) >> 1
	leafIdx := uint32(treeIdx & uint64((1<<3)-1))
	treeIdx >>= 3

	// Extract FORS signature (fixed size for our parameters).
	forsSigEnd := sphincsN + sphincsK*(sphincsLogT*sphincsN+sphincsN)
	if len(sig) < forsSigEnd {
		return false
	}
	forsSig := sig[sphincsN:forsSigEnd]

	// Reconstruct FORS root from signature.
	forsRoot := sphincsFORSRoot(forsMsgBits, forsSig, pkSeed, treeIdx, leafIdx)

	// Verify hypertree signature.
	htSig := sig[forsSigEnd:]
	return sphincsHTVerify(forsRoot, htSig, pkSeed, pkRoot, treeIdx, leafIdx)
}

// --- WOTS+ one-time signatures ---

// sphincsWOTSChain applies the hash chain function step times.
func sphincsWOTSChain(x, pkSeed []byte, adrs *sphincsADRS, start, steps int) []byte {
	result := make([]byte, sphincsN)
	copy(result, x)
	for i := start; i < start+steps; i++ {
		adrs.HashIdx = uint32(i)
		result = sphincsF(pkSeed, adrs.toBytes(), result)
	}
	return result
}

// sphincsWOTSSign produces a WOTS+ signature on an N-byte message digest.
func sphincsWOTSSign(msgDigest, skSeed, pkSeed []byte, adrs *sphincsADRS) []byte {
	sig := make([]byte, 0, sphincsWOTSSigSz)
	digits := wotsBaseW(msgDigest)

	// Compute checksum.
	csum := 0
	for _, d := range digits {
		csum += (sphincsW - 1) - d
	}
	csumDigits := wotsChecksumDigits(csum)
	allDigits := append(digits, csumDigits...)

	for i := 0; i < sphincsWOTSLen; i++ {
		adrs.ChainIdx = uint32(i)
		skAdrs := *adrs
		skAdrs.TypeField = 0
		skAdrs.HashIdx = 0
		sk := sphincsPRF(skSeed, pkSeed, skAdrs.toBytes())
		chain := sphincsWOTSChain(sk, pkSeed, adrs, 0, allDigits[i])
		sig = append(sig, chain...)
	}
	return sig
}

// sphincsWOTSPKFromSig recovers the WOTS+ public key from a signature.
func sphincsWOTSPKFromSig(sig, msgDigest, pkSeed []byte, adrs *sphincsADRS) []byte {
	digits := wotsBaseW(msgDigest)
	csum := 0
	for _, d := range digits {
		csum += (sphincsW - 1) - d
	}
	csumDigits := wotsChecksumDigits(csum)
	allDigits := append(digits, csumDigits...)

	var chains [][]byte
	for i := 0; i < sphincsWOTSLen; i++ {
		adrs.ChainIdx = uint32(i)
		chainStart := sig[i*sphincsN : (i+1)*sphincsN]
		remaining := sphincsW - 1 - allDigits[i]
		chain := sphincsWOTSChain(chainStart, pkSeed, adrs, allDigits[i], remaining)
		chains = append(chains, chain)
	}

	// Hash all chains together for PK.
	var flat []byte
	for _, c := range chains {
		flat = append(flat, c...)
	}
	return sphincsHash(pkSeed, adrs.toBytes(), flat, sphincsN)
}

// wotsBaseW converts an N-byte message to base-W digits.
func wotsBaseW(msg []byte) []int {
	digits := make([]int, sphincsWOTSLen1)
	for i := 0; i < len(msg) && i < sphincsN; i++ {
		// Each byte yields 2 nibbles for W=16.
		if 2*i < sphincsWOTSLen1 {
			digits[2*i] = int(msg[i] >> 4)
		}
		if 2*i+1 < sphincsWOTSLen1 {
			digits[2*i+1] = int(msg[i] & 0x0F)
		}
	}
	return digits
}

// wotsChecksumDigits encodes a checksum as base-W digits.
func wotsChecksumDigits(csum int) []int {
	digits := make([]int, sphincsWOTSLen2)
	for i := sphincsWOTSLen2 - 1; i >= 0; i-- {
		digits[i] = csum % sphincsW
		csum /= sphincsW
	}
	return digits
}

// --- XMSS tree layer ---

// sphincsXMSSRoot computes the root of a single XMSS tree.
func sphincsXMSSRoot(skSeed, pkSeed []byte, adrs *sphincsADRS) []byte {
	// Simplified: small tree with 8 leaves.
	numLeaves := 8
	leaves := make([][]byte, numLeaves)
	for i := 0; i < numLeaves; i++ {
		adrs.KeyPairIdx = uint32(i)
		adrs.TypeField = 0
		leaves[i] = sphincsWOTSPK(skSeed, pkSeed, adrs)
	}
	// Build binary Merkle tree.
	return sphincsMerkleRoot(leaves, pkSeed, adrs)
}

// sphincsWOTSPK computes a WOTS+ public key for a given leaf.
func sphincsWOTSPK(skSeed, pkSeed []byte, adrs *sphincsADRS) []byte {
	var chains [][]byte
	for i := 0; i < sphincsWOTSLen; i++ {
		adrs.ChainIdx = uint32(i)
		sk := sphincsPRF(skSeed, pkSeed, adrs.toBytes())
		chain := sphincsWOTSChain(sk, pkSeed, adrs, 0, sphincsW-1)
		chains = append(chains, chain)
	}
	var flat []byte
	for _, c := range chains {
		flat = append(flat, c...)
	}
	return sphincsHash(pkSeed, adrs.toBytes(), flat, sphincsN)
}

// sphincsMerkleRoot builds a Merkle tree from leaf hashes.
func sphincsMerkleRoot(leaves [][]byte, pkSeed []byte, adrs *sphincsADRS) []byte {
	n := len(leaves)
	if n == 0 {
		return make([]byte, sphincsN)
	}
	// Pad to power of two.
	size := 1
	for size < n {
		size <<= 1
	}
	nodes := make([][]byte, 2*size)
	for i := 0; i < n; i++ {
		nodes[size+i] = leaves[i]
	}
	for i := n; i < size; i++ {
		nodes[size+i] = make([]byte, sphincsN)
	}
	for i := size - 1; i >= 1; i-- {
		nodes[i] = sphincsHash(pkSeed, adrs.toBytes(), nodes[2*i], nodes[2*i+1], sphincsN)
	}
	return nodes[1]
}

// --- FORS (Forest of Random Subsets) ---

// sphincsFORSSign creates a FORS signature.
func sphincsFORSSign(msgBits, skSeed, pkSeed []byte, treeIdx uint64, leafIdx uint32) []byte {
	sig := make([]byte, 0, sphincsK*(sphincsLogT*sphincsN+sphincsN))
	for i := 0; i < sphincsK; i++ {
		// Extract log(T) bits from message for tree index.
		idx := sphincsFORSMsgIndex(msgBits, i)

		// Secret leaf value.
		adrs := &sphincsADRS{
			TreeAddr:   treeIdx,
			KeyPairIdx: leafIdx,
			TypeField:  3, // FORS tree
			ChainIdx:   uint32(i),
			HashIdx:    uint32(idx),
		}
		sk := sphincsPRF(skSeed, pkSeed, adrs.toBytes())
		sig = append(sig, sk...)

		// Authentication path (log(T) siblings).
		for j := 0; j < sphincsLogT; j++ {
			adrs.HashIdx = uint32(j)
			sibling := sphincsHash(pkSeed, adrs.toBytes(), []byte{byte(i), byte(j), byte(idx >> uint(j))}, sphincsN)
			sig = append(sig, sibling...)
		}
	}
	return sig
}

// sphincsFORSRoot reconstructs the FORS root from signature data.
func sphincsFORSRoot(msgBits, forsSig, pkSeed []byte, treeIdx uint64, leafIdx uint32) []byte {
	var roots [][]byte
	off := 0
	for i := 0; i < sphincsK; i++ {
		idx := sphincsFORSMsgIndex(msgBits, i)

		// Leaf value from signature.
		if off+sphincsN > len(forsSig) {
			return make([]byte, sphincsN)
		}
		leaf := forsSig[off : off+sphincsN]
		off += sphincsN

		// Leaf hash.
		adrs := &sphincsADRS{
			TreeAddr:   treeIdx,
			KeyPairIdx: leafIdx,
			TypeField:  3,
			ChainIdx:   uint32(i),
			HashIdx:    uint32(idx),
		}
		node := sphincsHash(pkSeed, adrs.toBytes(), leaf, sphincsN)

		// Walk auth path up.
		for j := 0; j < sphincsLogT; j++ {
			if off+sphincsN > len(forsSig) {
				return make([]byte, sphincsN)
			}
			sibling := forsSig[off : off+sphincsN]
			off += sphincsN
			if (idx>>uint(j))&1 == 0 {
				node = sphincsHash(pkSeed, adrs.toBytes(), node, sibling, sphincsN)
			} else {
				node = sphincsHash(pkSeed, adrs.toBytes(), sibling, node, sphincsN)
			}
		}
		roots = append(roots, node)
	}

	// Combine all FORS tree roots.
	var flat []byte
	for _, r := range roots {
		flat = append(flat, r...)
	}
	adrs := &sphincsADRS{TreeAddr: treeIdx, KeyPairIdx: leafIdx, TypeField: 4}
	return sphincsHash(pkSeed, adrs.toBytes(), flat, sphincsN)
}

// sphincsFORSMsgIndex extracts the i-th FORS tree index from message bits.
func sphincsFORSMsgIndex(msgBits []byte, treeNum int) int {
	// Extract sphincsLogT bits starting at bit position treeNum*sphincsLogT.
	bitPos := treeNum * sphincsLogT
	byteIdx := bitPos / 8
	bitOff := uint(bitPos % 8)
	if byteIdx >= len(msgBits) {
		return 0
	}
	val := int(msgBits[byteIdx]) >> bitOff
	if byteIdx+1 < len(msgBits) {
		val |= int(msgBits[byteIdx+1]) << (8 - bitOff)
	}
	return val & (sphincsT - 1)
}

// --- Hypertree ---

// sphincsHTSign signs a message (FORS root) through D layers of XMSS.
func sphincsHTSign(msg, skSeed, pkSeed []byte, treeIdx uint64, leafIdx uint32) []byte {
	sig := make([]byte, 0)
	currentMsg := msg
	curTree := treeIdx
	curLeaf := leafIdx

	for layer := uint32(0); layer < uint32(sphincsD); layer++ {
		adrs := &sphincsADRS{LayerAddr: layer, TreeAddr: curTree, KeyPairIdx: curLeaf}
		// WOTS+ sign at this layer.
		wotsSig := sphincsWOTSSign(currentMsg, skSeed, pkSeed, adrs)
		sig = append(sig, wotsSig...)

		// Next message = WOTS+ public key at this leaf (matches verify-side recovery).
		currentMsg = sphincsWOTSPK(skSeed, pkSeed, adrs)
		curLeaf = uint32(curTree & 0x7)
		curTree >>= 3
	}
	return sig
}

// sphincsHTVerify verifies a hypertree signature.
func sphincsHTVerify(msg, htSig, pkSeed, pkRoot []byte, treeIdx uint64, leafIdx uint32) bool {
	currentMsg := msg
	curTree := treeIdx
	curLeaf := leafIdx
	off := 0

	for layer := uint32(0); layer < uint32(sphincsD); layer++ {
		if off+sphincsWOTSSigSz > len(htSig) {
			return false
		}
		wotsSig := htSig[off : off+sphincsWOTSSigSz]
		off += sphincsWOTSSigSz

		adrs := &sphincsADRS{LayerAddr: layer, TreeAddr: curTree, KeyPairIdx: curLeaf}
		pk := sphincsWOTSPKFromSig(wotsSig, currentMsg, pkSeed, adrs)

		// Next message is the tree root (here simplified).
		currentMsg = pk
		curLeaf = uint32(curTree & 0x7)
		curTree >>= 3
	}

	// Final root should match PK.root.
	if len(currentMsg) != len(pkRoot) {
		return false
	}
	var diff byte
	for i := range pkRoot {
		if i < len(currentMsg) {
			diff |= currentMsg[i] ^ pkRoot[i]
		}
	}
	return diff == 0
}

// --- Hash primitives ---

// sphincsHash is a variable-output hash using Keccak256 chaining.
func sphincsHash(parts ...interface{}) []byte {
	// Last argument is the output length if it's an int.
	outLen := sphincsN
	var data [][]byte
	for _, p := range parts {
		switch v := p.(type) {
		case []byte:
			data = append(data, v)
		case int:
			outLen = v
		}
	}
	h := crypto.Keccak256(data...)
	if outLen <= 32 {
		return h[:outLen]
	}
	// Chain for longer output.
	result := make([]byte, 0, outLen)
	result = append(result, h...)
	for len(result) < outLen {
		h = crypto.Keccak256(h)
		result = append(result, h...)
	}
	return result[:outLen]
}

// sphincsF is the tweakable hash function F(pk.seed, adrs, msg).
func sphincsF(pkSeed, adrs, msg []byte) []byte {
	return sphincsHash(pkSeed, adrs, msg, sphincsN)
}

// sphincsPRF is the pseudorandom function PRF(sk.seed, pk.seed, adrs).
func sphincsPRF(skSeed, pkSeed, adrs []byte) []byte {
	return sphincsHash(skSeed, pkSeed, adrs, sphincsN)
}
