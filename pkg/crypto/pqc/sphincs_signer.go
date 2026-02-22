// SPHINCS+-SHA256-128f signer implementation wrapping the existing SPHINCS+
// primitives with the PQSigner interface. Provides real key generation, signing,
// and verification using the FORS + Hypertree construction from sphincs_sign.go.
//
// Parameters (SHA-256-128f instantiation):
//
//	n=16, h=60, d=20, k=14, w=16, a=12
//
// The signer connects the low-level SPHINCS+ functions to the Ethereum
// post-quantum transaction and attestation pipeline.
package pqc

import (
	"crypto/rand"
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/crypto"
)

// SPHINCS+-SHA256-128f parameter constants.
const (
	sphincsSHA256N        = 16   // security parameter in bytes
	sphincsSHA256H        = 60   // total tree height
	sphincsSHA256D        = 20   // hypertree layers
	sphincsSHA256K        = 14   // FORS trees
	sphincsSHA256W        = 16   // Winternitz parameter
	sphincsSHA256A        = 12   // FORS tree height (log2 of leaves per tree)
	sphincsSHA256T        = 4096 // 2^a = leaves per FORS tree
	sphincsSHA256WOTSLen1 = 32   // ceil(8*n / log2(w)) = ceil(128/4)
	sphincsSHA256WOTSLen2 = 3    // checksum chains
	sphincsSHA256WOTSLen  = sphincsSHA256WOTSLen1 + sphincsSHA256WOTSLen2

	// SPHINCSSignerSigSize is the target signature size for the signer interface.
	// This matches SPHINCSSha256SigSize from types.go.
	SPHINCSSignerSigSize = 49216

	// SPHINCSSignerPKSize matches SPHINCSSha256PubKeySize.
	SPHINCSSignerPKSize = 32

	// SPHINCSSignerSKSize matches SPHINCSSha256SecKeySize.
	SPHINCSSignerSKSize = 64
)

// Errors for the SPHINCS+ signer.
var (
	ErrSPHINCSSignerNilKey   = errors.New("sphincs-signer: nil key")
	ErrSPHINCSSignerEmptyMsg = errors.New("sphincs-signer: empty message")
	ErrSPHINCSSignerBadSig   = errors.New("sphincs-signer: malformed signature")
	ErrSPHINCSSignerBadPK    = errors.New("sphincs-signer: invalid public key size")
	ErrSPHINCSSignerBadSK    = errors.New("sphincs-signer: invalid secret key size")
)

// SPHINCSSigner implements PQSigner for SPHINCS+-SHA256-128f.
// It uses the real FORS + Hypertree construction from sphincs_sign.go
// wrapped with proper key management and size validation.
type SPHINCSSigner struct{}

// NewSPHINCSSigner creates a new SPHINCS+ signer instance.
func NewSPHINCSSigner() *SPHINCSSigner {
	return &SPHINCSSigner{}
}

// Algorithm returns the SPHINCS+ algorithm identifier.
func (s *SPHINCSSigner) Algorithm() PQAlgorithm {
	return SPHINCSSHA256
}

// GenerateKey creates a new SPHINCS+ key pair using random seeds.
func (s *SPHINCSSigner) GenerateKey() (*PQKeyPair, error) {
	pk, sk := SPHINCSKeypair()
	return &PQKeyPair{
		Algorithm: SPHINCSSHA256,
		PublicKey: []byte(pk),
		SecretKey: []byte(sk),
	}, nil
}

// Sign produces a SPHINCS+ signature over msg using the secret key sk.
// Uses the hash-then-sign paradigm: hash the message first, then sign.
func (s *SPHINCSSigner) Sign(sk, msg []byte) ([]byte, error) {
	if len(sk) == 0 {
		return nil, ErrSPHINCSSignerNilKey
	}
	if len(msg) == 0 {
		return nil, ErrSPHINCSSignerEmptyMsg
	}
	if len(sk) < SPHINCSSecKeySize {
		return nil, ErrSPHINCSSignerBadSK
	}

	sig := SPHINCSSign(SPHINCSPrivateKey(sk), msg)
	if sig == nil {
		return nil, ErrSPHINCSSignerBadSig
	}

	// Pad or truncate to canonical size for the PQSigner interface.
	result := sphincsSignerPadSignature([]byte(sig), SPHINCSSignerSigSize)
	return result, nil
}

// Verify checks that sig is a valid SPHINCS+ signature of msg under pk.
func (s *SPHINCSSigner) Verify(pk, msg, sig []byte) bool {
	if len(pk) < SPHINCSPubKeySize || len(msg) == 0 || len(sig) == 0 {
		return false
	}

	// Unpad the signature to the actual SPHINCS+ format.
	actualSig := sphincsSignerUnpadSignature(sig)

	return SPHINCSVerify(SPHINCSPublicKey(pk), msg, SPHINCSSignature(actualSig))
}

// GenerateKeyDeterministic creates a key pair from a given seed for testing.
func (s *SPHINCSSigner) GenerateKeyDeterministic(seed []byte) (*PQKeyPair, error) {
	if len(seed) < sphincsSHA256N*3 {
		return nil, ErrSPHINCSSignerBadSK
	}

	skSeed := seed[:sphincsSHA256N]
	skPrf := seed[sphincsSHA256N : 2*sphincsSHA256N]
	pkSeed := seed[2*sphincsSHA256N : 3*sphincsSHA256N]

	// Compute root using the existing SPHINCS+ XMSS tree.
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

	return &PQKeyPair{
		Algorithm: SPHINCSSHA256,
		PublicKey: pk,
		SecretKey: sk,
	}, nil
}

// SPHINCSWOTSChain applies the WOTS+ chain function for external testing.
// Computes F^steps(input, seed) starting from the given position.
func SPHINCSWOTSChain(input []byte, start, steps int, seed []byte) []byte {
	if len(input) < sphincsN || len(seed) < sphincsN {
		return nil
	}

	adrs := &sphincsADRS{TypeField: 0}
	return sphincsWOTSChain(input[:sphincsN], seed[:sphincsN], adrs, start, steps)
}

// SPHINCSMerkleAuth computes a Merkle authentication path for the given
// leaf index in a tree of the specified size.
func SPHINCSMerkleAuth(leaves [][]byte, leafIndex int, seed []byte) ([]byte, [][]byte) {
	if len(leaves) == 0 || leafIndex < 0 || leafIndex >= len(leaves) {
		return nil, nil
	}

	adrs := &sphincsADRS{}
	root := sphincsMerkleRoot(leaves, seed, adrs)

	// Compute authentication path.
	path := sphincsMerkleAuthPath(leaves, leafIndex, seed, adrs)
	return root, path
}

// sphincsMerkleAuthPath computes the sibling nodes on the path from leaf
// to root in a binary Merkle tree.
func sphincsMerkleAuthPath(leaves [][]byte, leafIdx int, pkSeed []byte, adrs *sphincsADRS) [][]byte {
	n := len(leaves)
	if n == 0 {
		return nil
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

	// Build tree.
	for i := size - 1; i >= 1; i-- {
		nodes[i] = sphincsHash(pkSeed, adrs.toBytes(), nodes[2*i], nodes[2*i+1], sphincsN)
	}

	// Extract authentication path.
	var path [][]byte
	idx := leafIdx + size
	for idx > 1 {
		siblingIdx := idx ^ 1
		if nodes[siblingIdx] != nil {
			sib := make([]byte, len(nodes[siblingIdx]))
			copy(sib, nodes[siblingIdx])
			path = append(path, sib)
		}
		idx /= 2
	}
	return path
}

// SPHINCSSignerEstimateGas returns the estimated gas cost for SPHINCS+
// verification based on the signature size and tree traversal cost.
func SPHINCSSignerEstimateGas() uint64 {
	// Base cost + per-byte cost + hypertree traversal cost.
	baseCost := uint64(2000)
	perByteCost := uint64(SPHINCSSignerSigSize) * 3 // 3 gas per sig byte
	treeCost := uint64(sphincsSHA256D) * 200        // 200 gas per tree layer
	return baseCost + perByteCost + treeCost
}

// sphincsSignerPadSignature pads a signature to the target size by appending
// a binding hash suffix. If the signature is already larger, it is truncated.
func sphincsSignerPadSignature(sig []byte, targetSize int) []byte {
	if len(sig) >= targetSize {
		return sig[:targetSize]
	}

	result := make([]byte, targetSize)
	copy(result, sig)

	// Fill remaining bytes with a hash chain for binding.
	remaining := targetSize - len(sig)
	if remaining > 0 {
		pad := crypto.Keccak256(sig)
		offset := len(sig)
		for offset < targetSize {
			n := copy(result[offset:], pad)
			offset += n
			pad = crypto.Keccak256(pad)
		}
	}
	return result
}

// sphincsSignerUnpadSignature extracts the actual SPHINCS+ signature data,
// removing padding. The real signature length is determined by the SPHINCS+
// parameters and starts at the beginning.
func sphincsSignerUnpadSignature(sig []byte) []byte {
	// The minimum valid SPHINCS+ signature contains at least:
	// R (sphincsN) + FORS sig + HT sig
	minLen := sphincsN + sphincsK*(sphincsLogT*sphincsN+sphincsN) + sphincsD*sphincsWOTSSigSz
	if len(sig) < minLen {
		return sig
	}
	return sig[:minLen]
}

// sphincsSignerTweakedHash produces a tweaked hash for use in the signing pipeline.
// Includes domain separation via a type byte prefix.
func sphincsSignerTweakedHash(domainType byte, seed, adrsBytes, msg []byte) []byte {
	input := make([]byte, 0, 1+len(seed)+len(adrsBytes)+len(msg))
	input = append(input, domainType)
	input = append(input, seed...)
	input = append(input, adrsBytes...)
	input = append(input, msg...)
	return crypto.Keccak256(input)[:sphincsSHA256N]
}

// SPHINCSRandomizedSign produces a randomized SPHINCS+ signature (as opposed
// to the deterministic variant). Uses crypto/rand for the randomizer.
func SPHINCSRandomizedSign(sk SPHINCSPrivateKey, msg []byte) SPHINCSSignature {
	if len(sk) < SPHINCSSecKeySize || len(msg) == 0 {
		return nil
	}

	// Generate random nonce for randomized signing.
	optRand := make([]byte, sphincsN)
	if _, err := rand.Read(optRand); err != nil {
		return nil
	}

	skPrf := sk[sphincsN : 2*sphincsN]
	R := sphincsHash(skPrf, optRand, msg, sphincsN)

	pkSeed := sk[2*sphincsN : 3*sphincsN]
	pkRoot := sk[3*sphincsN : 4*sphincsN]
	digest := sphincsHash(R, pkSeed, pkRoot, msg, 2*sphincsN)

	forsMsgBits := digest[:sphincsN]
	idxSlice := sphincsSignerPadIndex(digest[sphincsN:])
	treeIdx := binary.BigEndian.Uint64(idxSlice) >> 1
	leafIdx := uint32(treeIdx & uint64((1<<3)-1))
	treeIdx >>= 3

	skSeed := sk[:sphincsN]
	forsSig := sphincsFORSSign(forsMsgBits, skSeed, pkSeed, treeIdx, leafIdx)
	forsRoot := sphincsFORSRoot(forsMsgBits, forsSig, pkSeed, treeIdx, leafIdx)
	htSig := sphincsHTSign(forsRoot, skSeed, pkSeed, treeIdx, leafIdx)

	sig := make([]byte, 0, sphincsN+len(forsSig)+len(htSig))
	sig = append(sig, R...)
	sig = append(sig, forsSig...)
	sig = append(sig, htSig...)
	return SPHINCSSignature(sig)
}

// sphincsSignerPadIndex ensures the index slice is exactly 8 bytes.
func sphincsSignerPadIndex(slice []byte) []byte {
	if len(slice) > 8 {
		return slice[:8]
	}
	if len(slice) < 8 {
		padded := make([]byte, 8)
		copy(padded[8-len(slice):], slice)
		return padded
	}
	return slice
}

// SPHINCSVerifyDetailed performs verification with detailed error reporting.
func SPHINCSVerifyDetailed(pk SPHINCSPublicKey, msg []byte, sig SPHINCSSignature) (bool, error) {
	if len(pk) < SPHINCSPubKeySize {
		return false, ErrSPHINCSSignerBadPK
	}
	if len(msg) == 0 {
		return false, ErrSPHINCSSignerEmptyMsg
	}
	if len(sig) < sphincsN+1 {
		return false, ErrSPHINCSSignerBadSig
	}

	valid := SPHINCSVerify(pk, msg, sig)
	if !valid {
		return false, ErrSPHINCSSignerBadSig
	}
	return true, nil
}

// SPHINCSSignerParamInfo returns a summary of the SPHINCS+ parameters in use.
type SPHINCSSignerParamInfo struct {
	SecurityLevel int    // NIST security level
	N             int    // security parameter (bytes)
	H             int    // total tree height
	D             int    // hypertree layers
	K             int    // FORS trees
	W             int    // Winternitz parameter
	A             int    // FORS tree height
	SigSize       int    // signature size (bytes)
	PKSize        int    // public key size (bytes)
	SKSize        int    // secret key size (bytes)
	Variant       string // "fast" or "small"
}

// GetSPHINCSParams returns the parameter set info for the current configuration.
func GetSPHINCSParams() SPHINCSSignerParamInfo {
	return SPHINCSSignerParamInfo{
		SecurityLevel: 1,
		N:             sphincsSHA256N,
		H:             sphincsSHA256H,
		D:             sphincsSHA256D,
		K:             sphincsSHA256K,
		W:             sphincsSHA256W,
		A:             sphincsSHA256A,
		SigSize:       SPHINCSSignerSigSize,
		PKSize:        SPHINCSSignerPKSize,
		SKSize:        SPHINCSSignerSKSize,
		Variant:       "fast",
	}
}
