// Post-Quantum Hash-Based One-Time Signatures (M+ roadmap: post quantum L1)
//
// Implements Winternitz One-Time Signature (WOTS+) as defined in RFC 8391
// (XMSS) using SHA-256 as the underlying hash function. WOTS+ provides
// quantum-resistant one-time signatures based solely on the collision
// resistance of the hash function.
//
// The signature consists of chains of hash iterations. Each chain corresponds
// to one digit of the base-W representation of the message digest plus a
// checksum to prevent existential forgery.
//
// Parameters (WOTS+-SHA256-W16):
//   - W = 16 (Winternitz parameter, base for digit encoding)
//   - N = 32 (hash output length in bytes, SHA-256)
//   - Len1 = 64 (message chains: ceil(8*N / log2(W)) = 256/4 = 64)
//   - Len2 = 3 (checksum chains: floor(log2(Len1*(W-1))) / log2(W) + 1)
//   - Len = 67 (total chains = Len1 + Len2)
package pqc

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

// WOTS+ parameter constants (W=16, N=32).
const (
	WOTSParamW    = 16 // Winternitz parameter (base).
	WOTSParamN    = 32 // Hash output size in bytes.
	WOTSParamLen1 = 64 // Message chains: ceil(8*32 / log2(16)).
	WOTSParamLen2 = 3  // Checksum chains.
	WOTSParamLen  = 67 // Total chains (Len1 + Len2).
)

// WOTSParams holds the full parameter set for a WOTS+ instance.
type WOTSParams struct {
	W    int // Winternitz parameter (base).
	N    int // Hash output length in bytes.
	Len1 int // Number of message digit chains.
	Len2 int // Number of checksum digit chains.
	Len  int // Total number of chains (Len1 + Len2).
}

// DefaultWOTSParams returns the standard WOTS+-SHA256-W16 parameters.
func DefaultWOTSParams() WOTSParams {
	return WOTSParams{
		W:    WOTSParamW,
		N:    WOTSParamN,
		Len1: WOTSParamLen1,
		Len2: WOTSParamLen2,
		Len:  WOTSParamLen,
	}
}

// WOTSKeyPair holds a WOTS+ key pair.
type WOTSKeyPair struct {
	SecretKey [][32]byte // Len secret chain seeds.
	PublicKey [][32]byte // Len public chain endpoints.
	Seed      [32]byte   // Original seed used for chain hashing (WOTS+ tweak).
	Params    WOTSParams
}

// WOTS+ errors.
var (
	ErrWOTSInvalidSeed      = errors.New("pqc: WOTS+ seed must be 32 bytes")
	ErrWOTSInvalidMessage   = errors.New("pqc: WOTS+ message must be 32 bytes")
	ErrWOTSInvalidSignature = errors.New("pqc: WOTS+ signature has wrong chain count")
	ErrWOTSInvalidPublicKey = errors.New("pqc: WOTS+ public key has wrong chain count")
	ErrWOTSVerifyFailed     = errors.New("pqc: WOTS+ verification failed")
)

// GenerateWOTSKeyPair generates a WOTS+ key pair from a 32-byte seed.
// The secret key chains are derived deterministically from the seed.
// The public key chains are the endpoints of W-1 hash iterations.
func GenerateWOTSKeyPair(seed [32]byte) *WOTSKeyPair {
	params := DefaultWOTSParams()
	sk := make([][32]byte, params.Len)
	pk := make([][32]byte, params.Len)

	// The chain seed is derived from the keygen seed for domain separation.
	// All chain operations (keygen, sign, verify) must use the same seed.
	chainSeed := sha256.Sum256(append(seed[:], []byte("WOTS+chain")...))

	// Derive each secret key chain from seed.
	for i := 0; i < params.Len; i++ {
		sk[i] = deriveWOTSChainKey(seed, i)
		// Public key = chain endpoint: hash W-1 times from secret key.
		pk[i] = chainHash(sk[i], 0, params.W-1, chainSeed)
	}

	return &WOTSKeyPair{
		SecretKey: sk,
		PublicKey: pk,
		Seed:      chainSeed,
		Params:    params,
	}
}

// WOTSSign produces a WOTS+ signature for a 32-byte message digest.
// The signature consists of Len 32-byte chain values, each hashed
// d_i times from the corresponding secret key, where d_i is the
// i-th base-W digit of the message plus checksum.
// The seed must be the same seed used during key generation.
func WOTSSign(sk [][32]byte, message [32]byte) ([][32]byte, error) {
	return WOTSSignWithSeed(sk, message, [32]byte{})
}

// WOTSSignWithSeed produces a WOTS+ signature using an explicit chain seed.
func WOTSSignWithSeed(sk [][32]byte, message [32]byte, seed [32]byte) ([][32]byte, error) {
	params := DefaultWOTSParams()
	if len(sk) != params.Len {
		return nil, ErrWOTSInvalidSignature
	}

	// Compute base-W representation of message + checksum.
	digits := baseWChain(message[:], params.W)
	checksum := computeChecksum(message[:], params.W)
	csDigits := checksumToBaseW(checksum, params.W, params.Len2)
	allDigits := append(digits, csDigits...)

	sig := make([][32]byte, params.Len)
	for i := 0; i < params.Len; i++ {
		d := 0
		if i < len(allDigits) {
			d = allDigits[i]
		}
		// Sign: hash d times from secret key.
		sig[i] = chainHash(sk[i], 0, d, seed)
	}

	return sig, nil
}

// WOTSVerify verifies a WOTS+ signature against a public key and message.
// For each chain, the verifier completes the remaining hash iterations
// (W-1-d_i) from the signature value and checks it matches the public key.
func WOTSVerify(pk, signature [][32]byte, message [32]byte) bool {
	return WOTSVerifyWithSeed(pk, signature, message, [32]byte{})
}

// WOTSVerifyWithSeed verifies with an explicit chain seed.
func WOTSVerifyWithSeed(pk, signature [][32]byte, message [32]byte, seed [32]byte) bool {
	params := DefaultWOTSParams()
	if len(pk) != params.Len || len(signature) != params.Len {
		return false
	}

	// Compute base-W digits of message + checksum.
	digits := baseWChain(message[:], params.W)
	checksum := computeChecksum(message[:], params.W)
	csDigits := checksumToBaseW(checksum, params.W, params.Len2)
	allDigits := append(digits, csDigits...)

	for i := 0; i < params.Len; i++ {
		d := 0
		if i < len(allDigits) {
			d = allDigits[i]
		}
		// Complete the chain from signature value.
		remaining := params.W - 1 - d
		computed := chainHash(signature[i], d, remaining, seed)
		if computed != pk[i] {
			return false
		}
	}

	return true
}

// chainHash computes a hash chain: starting from 'input', applies the
// keyed hash function 'steps' times. The 'start' parameter indicates the
// chain position (used to domain-separate each step). The 'seed' provides
// additional randomization (WOTS+ tweak).
//
// Each step computes: H(seed || toByte(start+i) || input)
func chainHash(input [32]byte, start, steps int, seed [32]byte) [32]byte {
	result := input
	for i := 0; i < steps; i++ {
		result = wotsHash(seed, start+i, result)
	}
	return result
}

// wotsHash is the keyed hash function used in chain computation.
// H(seed || toByte(position) || value)
func wotsHash(seed [32]byte, position int, value [32]byte) [32]byte {
	var buf [32 + 4 + 32]byte
	copy(buf[:32], seed[:])
	binary.BigEndian.PutUint32(buf[32:36], uint32(position))
	copy(buf[36:68], value[:])
	return sha256.Sum256(buf[:])
}

// computeChecksum computes the Winternitz checksum for a message digest.
// The checksum prevents existential forgery by making it impossible to
// increase any digit without decreasing others.
//
// checksum = sum(W-1-d_i) for all message digits d_i
func computeChecksum(msgDigest []byte, w int) int {
	digits := baseWChain(msgDigest, w)
	checksum := 0
	for _, d := range digits {
		checksum += (w - 1) - d
	}
	return checksum
}

// baseWChain converts a byte slice to its base-W digit representation.
// For W=16, each byte yields two 4-bit digits (nibbles).
// For W=4, each byte yields four 2-bit digits.
// For W=256, each byte is one digit.
func baseWChain(digest []byte, w int) []int {
	var digits []int

	switch w {
	case 16:
		// Each byte -> two 4-bit nibbles (high nibble first).
		for _, b := range digest {
			digits = append(digits, int(b>>4))
			digits = append(digits, int(b&0x0F))
		}
	case 4:
		// Each byte -> four 2-bit crumbs.
		for _, b := range digest {
			digits = append(digits, int((b>>6)&0x03))
			digits = append(digits, int((b>>4)&0x03))
			digits = append(digits, int((b>>2)&0x03))
			digits = append(digits, int(b&0x03))
		}
	case 256:
		// Each byte is one digit.
		for _, b := range digest {
			digits = append(digits, int(b))
		}
	default:
		// Generic base-W decomposition using bit manipulation.
		bitsPerDigit := 0
		for v := w - 1; v > 0; v >>= 1 {
			bitsPerDigit++
		}
		mask := w - 1
		bitBuf := 0
		bitCount := 0
		for _, b := range digest {
			bitBuf |= int(b) << bitCount
			bitCount += 8
			for bitCount >= bitsPerDigit {
				digits = append(digits, bitBuf&mask)
				bitBuf >>= bitsPerDigit
				bitCount -= bitsPerDigit
			}
		}
		if bitCount > 0 {
			digits = append(digits, bitBuf&mask)
		}
	}

	return digits
}

// checksumToBaseW converts the integer checksum to a base-W digit sequence.
func checksumToBaseW(checksum, w, numDigits int) []int {
	digits := make([]int, numDigits)
	for i := numDigits - 1; i >= 0; i-- {
		digits[i] = checksum % w
		checksum /= w
	}
	return digits
}

// deriveWOTSChainKey derives the i-th secret key chain from a seed using
// SHA-256. Each chain key is domain-separated by its index.
func deriveWOTSChainKey(seed [32]byte, index int) [32]byte {
	var buf [32 + 4]byte
	copy(buf[:32], seed[:])
	binary.BigEndian.PutUint32(buf[32:36], uint32(index))
	return sha256.Sum256(buf[:])
}

// WOTSPublicKeyHash returns a single 32-byte hash representing the entire
// public key. This is the leaf hash used in Merkle tree constructions
// like XMSS.
func WOTSPublicKeyHash(pk [][32]byte) [32]byte {
	h := sha256.New()
	for i := range pk {
		h.Write(pk[i][:])
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// WOTSSignatureSize returns the size in bytes of a WOTS+ signature.
func WOTSSignatureSize() int {
	return WOTSParamLen * WOTSParamN
}

// WOTSPublicKeySize returns the size in bytes of a WOTS+ public key.
func WOTSPublicKeySize() int {
	return WOTSParamLen * WOTSParamN
}

// LamportKeyPair is a basic Lamport one-time signature key pair.
// Included for comparison and as a building block.
type LamportKeyPair struct {
	SecretKey [256][2][32]byte // 256 bit positions, 2 preimages each.
	PublicKey [256][2][32]byte // SHA-256 hashes of secret key values.
}

// GenerateLamportKeyPair creates a Lamport OTS key pair from a seed.
func GenerateLamportKeyPair(seed [32]byte) *LamportKeyPair {
	kp := &LamportKeyPair{}
	for i := 0; i < 256; i++ {
		for b := 0; b < 2; b++ {
			// Derive secret: SHA-256(seed || i || b).
			var buf [32 + 4 + 1]byte
			copy(buf[:32], seed[:])
			binary.BigEndian.PutUint32(buf[32:36], uint32(i))
			buf[36] = byte(b)
			kp.SecretKey[i][b] = sha256.Sum256(buf[:])
			kp.PublicKey[i][b] = sha256.Sum256(kp.SecretKey[i][b][:])
		}
	}
	return kp
}

// LamportSign signs a 32-byte message with a Lamport key pair.
// Returns 256 preimage values corresponding to the message bits.
func LamportSign(sk *[256][2][32]byte, message [32]byte) [256][32]byte {
	var sig [256][32]byte
	for i := 0; i < 256; i++ {
		bit := (message[i/8] >> (i % 8)) & 1
		sig[i] = sk[i][bit]
	}
	return sig
}

// LamportVerify verifies a Lamport signature against a public key.
func LamportVerify(pk *[256][2][32]byte, sig [256][32]byte, message [32]byte) bool {
	for i := 0; i < 256; i++ {
		bit := (message[i/8] >> (i % 8)) & 1
		h := sha256.Sum256(sig[i][:])
		if h != pk[i][bit] {
			return false
		}
	}
	return true
}

// CompareSchemes holds metrics for comparing OTS schemes.
type CompareSchemes struct {
	Name          string
	SecretKeySize int
	PublicKeySize int
	SignatureSize int
}

// WOTSMetrics returns size metrics for the WOTS+ scheme.
func WOTSMetrics() CompareSchemes {
	return CompareSchemes{
		Name:          "WOTS+-SHA256-W16",
		SecretKeySize: WOTSParamLen * 32,
		PublicKeySize: WOTSParamLen * 32,
		SignatureSize: WOTSParamLen * 32,
	}
}

// LamportMetrics returns size metrics for the Lamport scheme.
func LamportMetrics() CompareSchemes {
	return CompareSchemes{
		Name:          "Lamport-SHA256",
		SecretKeySize: 256 * 2 * 32,
		PublicKeySize: 256 * 2 * 32,
		SignatureSize: 256 * 32,
	}
}
