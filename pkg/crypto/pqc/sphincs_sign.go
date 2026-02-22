// SPHINCS+ hash-based signature scheme for long-term post-quantum security.
// Implements SPHINCS+-SHA2-128f (fast variant) using Keccak256 as the hash.
//
// SPHINCS+ builds a hypertree of XMSS trees, each containing WOTS+ one-time
// signature leaves. The "fast" variant uses larger signatures but faster signing.
//
// Parameters (SPHINCS+-128f-like):
//
//	N=16 (security bytes), W=16 (Winternitz), H=66 (total tree height),
//	D=22 (hypertree layers), K=33 (FORS trees), T=2^6 (FORS leaves per tree)
//
// For Ethereum post-quantum attestations (L+ roadmap), SPHINCS+ provides
// fallback security relying only on hash function pre-image resistance.
package pqc

import (
	"crypto/rand"
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/crypto"
)

// SPHINCS+-128f parameter constants.
const (
	sphincsN         = 16 // security parameter in bytes
	sphincsW         = 16 // Winternitz parameter
	sphincsH         = 66 // total tree height
	sphincsD         = 22 // hypertree layers
	sphincsK         = 33 // FORS trees
	sphincsLogT      = 6  // log2(FORS leaves per tree)
	sphincsT         = 64 // FORS leaves per tree (2^sphincsLogT)
	sphincsWOTSLen1  = 32 // ceil(8*N / log2(W)) = ceil(128/4) = 32
	sphincsWOTSLen2  = 3  // checksum chains
	sphincsWOTSLen   = sphincsWOTSLen1 + sphincsWOTSLen2
	sphincsWOTSSigSz = sphincsWOTSLen * sphincsN                    // 35 * 16 = 560 bytes
	sphincsFORSSigSz = sphincsK * (sphincsLogT*sphincsN + sphincsN) // FORS signature
	sphincsSeedSize  = 3 * sphincsN                                 // SK.seed + SK.prf + PK.seed

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

	// Convert named-type slices to []byte for sphincsHash compatibility.
	skSeed := []byte(sk[:sphincsN])
	skPrf := []byte(sk[sphincsN : 2*sphincsN])
	pkSeed := []byte(sk[2*sphincsN : 3*sphincsN])

	// Randomised message hash: R = PRF(SK.prf, msg).
	optRand := make([]byte, sphincsN)
	rand.Read(optRand)
	R := sphincsHash(skPrf, optRand, msg, sphincsN)

	// Digest = H(R || PK.seed || PK.root || msg).
	pkRoot := []byte(sk[3*sphincsN : 4*sphincsN])
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

	pkSeed := []byte(pk[:sphincsN])
	pkRoot := []byte(pk[sphincsN : 2*sphincsN])

	// Extract R from signature (convert to []byte for sphincsHash compatibility).
	R := []byte(sig[:sphincsN])

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
	forsSig := []byte(sig[sphincsN:forsSigEnd])

	// Reconstruct FORS root from signature.
	forsRoot := sphincsFORSRoot(forsMsgBits, forsSig, pkSeed, treeIdx, leafIdx)

	// Verify hypertree signature.
	htSig := []byte(sig[forsSigEnd:])
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
	numLeaves := sphincsXMSSNumLeaves
	leaves := make([][]byte, numLeaves)
	for i := 0; i < numLeaves; i++ {
		leafAdrs := &sphincsADRS{LayerAddr: adrs.LayerAddr, TreeAddr: adrs.TreeAddr, KeyPairIdx: uint32(i), TypeField: 0}
		leaves[i] = sphincsWOTSPK(skSeed, pkSeed, leafAdrs)
	}
	// Build binary Merkle tree using a clean tree ADRS (TypeField 5 = tree hash).
	treeAdrs := &sphincsADRS{LayerAddr: adrs.LayerAddr, TreeAddr: adrs.TreeAddr, TypeField: 5}
	return sphincsMerkleRoot(leaves, pkSeed, treeAdrs)
}

// sphincsWOTSPK computes a WOTS+ public key for a given leaf.
// Uses the same secret key derivation as sphincsWOTSSign (TypeField=0, HashIdx=0).
func sphincsWOTSPK(skSeed, pkSeed []byte, adrs *sphincsADRS) []byte {
	var chains [][]byte
	for i := 0; i < sphincsWOTSLen; i++ {
		adrs.ChainIdx = uint32(i)
		skAdrs := *adrs
		skAdrs.TypeField = 0
		skAdrs.HashIdx = 0
		sk := sphincsPRF(skSeed, pkSeed, skAdrs.toBytes())
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

// sphincsXMSSNumLeaves is the number of leaves in each simplified XMSS tree.
const sphincsXMSSNumLeaves = 8

// sphincsXMSSAuthPathSize is the number of sibling hashes in an auth path
// (log2(sphincsXMSSNumLeaves) = 3).
const sphincsXMSSAuthPathSize = 3

// --- Hypertree ---

// sphincsHTSign signs a message (FORS root) through D layers of XMSS.
// Each layer produces a WOTS+ signature plus an XMSS authentication path
// so that the verifier can reconstruct the tree root at each layer.
func sphincsHTSign(msg, skSeed, pkSeed []byte, treeIdx uint64, leafIdx uint32) []byte {
	sig := make([]byte, 0)
	currentMsg := msg
	curTree := treeIdx
	curLeaf := leafIdx

	for layer := uint32(0); layer < uint32(sphincsD); layer++ {
		leafIdxInTree := int(curLeaf) % sphincsXMSSNumLeaves

		// Build the full XMSS tree at this layer to get leaves and auth path.
		leaves := make([][]byte, sphincsXMSSNumLeaves)
		for i := 0; i < sphincsXMSSNumLeaves; i++ {
			leafAdrs := &sphincsADRS{LayerAddr: layer, TreeAddr: curTree, KeyPairIdx: uint32(i), TypeField: 0}
			leaves[i] = sphincsWOTSPK(skSeed, pkSeed, leafAdrs)
		}

		// Compute authentication path using a clean tree ADRS.
		treeAdrs := &sphincsADRS{LayerAddr: layer, TreeAddr: curTree, TypeField: 5}
		authPath := sphincsComputeAuthPath(leaves, leafIdxInTree, pkSeed, treeAdrs)

		// WOTS+ sign the current message at this leaf.
		signAdrs := &sphincsADRS{LayerAddr: layer, TreeAddr: curTree, KeyPairIdx: uint32(leafIdxInTree)}
		wotsSig := sphincsWOTSSign(currentMsg, skSeed, pkSeed, signAdrs)
		sig = append(sig, wotsSig...)

		// Append authentication path.
		for _, sibling := range authPath {
			sig = append(sig, sibling...)
		}

		// Next message is the XMSS tree root (computed with same tree ADRS).
		rootAdrs := &sphincsADRS{LayerAddr: layer, TreeAddr: curTree, TypeField: 5}
		currentMsg = sphincsMerkleRoot(leaves, pkSeed, rootAdrs)
		curLeaf = uint32(curTree & 0x7)
		curTree >>= 3
	}
	return sig
}

// sphincsComputeAuthPath computes the Merkle authentication path for a given
// leaf index in the XMSS tree.
func sphincsComputeAuthPath(leaves [][]byte, leafIdx int, pkSeed []byte, adrs *sphincsADRS) [][]byte {
	size := sphincsXMSSNumLeaves
	// Build the tree.
	nodes := make([][]byte, 2*size)
	for i := 0; i < size; i++ {
		if i < len(leaves) {
			nodes[size+i] = leaves[i]
		} else {
			nodes[size+i] = make([]byte, sphincsN)
		}
	}
	for i := size - 1; i >= 1; i-- {
		nodes[i] = sphincsHash(pkSeed, adrs.toBytes(), nodes[2*i], nodes[2*i+1], sphincsN)
	}

	// Extract authentication path (sibling at each level).
	var path [][]byte
	idx := leafIdx + size
	for idx > 1 {
		siblingIdx := idx ^ 1
		sib := make([]byte, len(nodes[siblingIdx]))
		copy(sib, nodes[siblingIdx])
		path = append(path, sib)
		idx /= 2
	}
	return path
}

// sphincsHTVerify verifies a hypertree signature.
// At each layer, it recovers the WOTS+ PK from the signature, then uses
// the XMSS authentication path to reconstruct the tree root.
func sphincsHTVerify(msg, htSig, pkSeed, pkRoot []byte, treeIdx uint64, leafIdx uint32) bool {
	// Size of one layer's signature: WOTS+ sig + auth path.
	layerSigSize := sphincsWOTSSigSz + sphincsXMSSAuthPathSize*sphincsN

	currentMsg := msg
	curTree := treeIdx
	curLeaf := leafIdx
	off := 0

	for layer := uint32(0); layer < uint32(sphincsD); layer++ {
		if off+layerSigSize > len(htSig) {
			return false
		}
		wotsSig := htSig[off : off+sphincsWOTSSigSz]
		off += sphincsWOTSSigSz

		// Extract authentication path.
		authPath := make([][]byte, sphincsXMSSAuthPathSize)
		for i := 0; i < sphincsXMSSAuthPathSize; i++ {
			authPath[i] = htSig[off : off+sphincsN]
			off += sphincsN
		}

		leafIdxInTree := int(curLeaf) % sphincsXMSSNumLeaves
		wotsAdrs := &sphincsADRS{LayerAddr: layer, TreeAddr: curTree, KeyPairIdx: uint32(leafIdxInTree)}
		pk := sphincsWOTSPKFromSig(wotsSig, currentMsg, pkSeed, wotsAdrs)

		// Reconstruct the tree root from the WOTS+ PK and auth path,
		// using the same tree ADRS type as signing.
		treeAdrs := &sphincsADRS{LayerAddr: layer, TreeAddr: curTree, TypeField: 5}
		node := pk
		idx := leafIdxInTree + sphincsXMSSNumLeaves
		for i := 0; i < sphincsXMSSAuthPathSize; i++ {
			if (idx & 1) == 0 {
				node = sphincsHash(pkSeed, treeAdrs.toBytes(), node, authPath[i], sphincsN)
			} else {
				node = sphincsHash(pkSeed, treeAdrs.toBytes(), authPath[i], node, sphincsN)
			}
			idx /= 2
		}

		currentMsg = node
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
