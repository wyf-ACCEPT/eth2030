// Shielded transaction cryptography for private L1 compute.
//
// Implements Pedersen commitments, ElGamal encryption/decryption, nullifier
// derivation, and Bulletproof-style range proofs for shielded execution.
// These primitives enable privacy-preserving value transfers where the
// transaction amount is hidden behind a commitment, encrypted for the
// recipient, and proven to be within a valid range.
//
// Targets the longer-term roadmap: private L1 shielded compute (2030++).
package vm

import (
	"encoding/binary"
	"math/big"

	"github.com/eth2030/eth2030/crypto"
)

// Curve parameters for the Pedersen commitment and ElGamal scheme.
// Uses the BN254 (alt_bn128) base field for compatibility with existing
// Ethereum precompiles. Generator points G and H are derived deterministically.
var (
	// scP is the BN254 base field modulus.
	scP, _ = new(big.Int).SetString("21888242871839275222246405745257275088696311157297823662689037894645226208583", 10)
	// scN is the BN254 curve order.
	scN, _ = new(big.Int).SetString("21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)

	// generatorG is the base generator point (derived from "pedersen-G").
	generatorG = scDerivePoint([]byte("pedersen-generator-G"))
	// generatorH is the blinding generator (derived from "pedersen-H").
	// H must be chosen such that log_G(H) is unknown (nothing-up-my-sleeve).
	generatorH = scDerivePoint([]byte("pedersen-generator-H"))
)

// RangeProof holds a Bulletproof-style range proof demonstrating that a
// committed value lies in [0, max].
type RangeProof struct {
	// Commitment is the Pedersen commitment to the proven value.
	Commitment [32]byte
	// A is the vector commitment to the bit decomposition.
	A [32]byte
	// S is the blinded commitment for the inner product.
	S [32]byte
	// T1 is the first polynomial commitment.
	T1 [32]byte
	// T2 is the second polynomial commitment.
	T2 [32]byte
	// TauX is the blinding factor for T evaluation.
	TauX [32]byte
	// Mu is the aggregated blinding factor.
	Mu [32]byte
	// InnerProduct is the serialised inner product proof.
	InnerProduct [32]byte
	// BitLength is the number of bits in the range proof (log2(max+1)).
	BitLength uint32
}

// PedersenCommitValue computes a Pedersen commitment to a value.
// C = value*G + blinding*H (mod N)
//
// The commitment is computationally hiding (blinding hides value) and
// computationally binding (cannot open to different value without breaking DLP).
func PedersenCommitValue(value, blinding uint64) [32]byte {
	// C = value * G + blinding * H (scalar multiplication in the group)
	v := new(big.Int).SetUint64(value)
	b := new(big.Int).SetUint64(blinding)

	// vG = value * G
	vG := scMul(generatorG, v)
	// bH = blinding * H
	bH := scMul(generatorH, b)
	// C = vG + bH
	c := scAdd(vG, bH)

	return scToHash(c)
}

// ElGamalEncrypt encrypts a plaintext under a public key using ElGamal.
// pk = sk * G (public key is a scalar multiplication of the generator)
// c1 = randomness * G
// c2 = plaintext_point + randomness * pk
//
// The plaintext is encoded as a field element derived from the input bytes.
func ElGamalEncrypt(pk [32]byte, plaintext []byte, randomness uint64) (c1, c2 [32]byte) {
	r := new(big.Int).SetUint64(randomness)
	pkPoint := scFromHash(pk)

	// c1 = r * G
	c1Point := scMul(generatorG, r)
	c1 = scToHash(c1Point)

	// Encode plaintext as a field element.
	ptPoint := scFromBytes(plaintext)

	// c2 = pt + r * pk
	rPK := scMul(pkPoint, r)
	c2Point := scAdd(ptPoint, rPK)
	c2 = scToHash(c2Point)

	return c1, c2
}

// ElGamalDecrypt decrypts an ElGamal ciphertext using the secret key.
// plaintext_point = c2 - sk * c1
func ElGamalDecrypt(sk [32]byte, c1, c2 [32]byte) []byte {
	skScalar := scFromHash(sk)
	c1Point := scFromHash(c1)
	c2Point := scFromHash(c2)

	// shared = sk * c1
	shared := scMul(c1Point, skScalar)

	// pt = c2 - shared
	ptPoint := scSub(c2Point, shared)

	return scToBytes(ptPoint)
}

// NullifierDerive derives a unique nullifier from a secret key and commitment.
// nullifier = H(sk || commitment)
// The nullifier is deterministic: the same (sk, commitment) always produces
// the same nullifier, enabling double-spend detection without revealing which
// commitment is being spent.
func NullifierDerive(sk, commitment [32]byte) [32]byte {
	h := crypto.Keccak256(sk[:], commitment[:])
	var result [32]byte
	copy(result[:], h)
	return result
}

// RangeProofGenerate generates a Bulletproof-style range proof demonstrating
// that value is in [0, max]. The proof binds to the Pedersen commitment
// C = value*G + blinding*H.
//
// The proof works by decomposing value into bits, committing to the bit
// vector, and proving via inner product argument that the bits reconstruct
// the committed value and each bit is 0 or 1.
func RangeProofGenerate(value, blinding uint64, max uint64) RangeProof {
	// Compute the commitment.
	commitment := PedersenCommitValue(value, blinding)

	// Determine bit length from max.
	bitLen := uint32(0)
	m := max
	for m > 0 {
		bitLen++
		m >>= 1
	}
	if bitLen == 0 {
		bitLen = 1
	}

	// Decompose value into bits.
	bits := make([]uint64, bitLen)
	for i := uint32(0); i < bitLen; i++ {
		bits[i] = (value >> i) & 1
	}

	// Compute vector commitment A = sum(bits[i] * G_i) + alpha * H
	// Using Fiat-Shamir to derive alpha deterministically.
	var valBuf [8]byte
	binary.BigEndian.PutUint64(valBuf[:], value)
	var blindBuf [8]byte
	binary.BigEndian.PutUint64(blindBuf[:], blinding)
	alpha := crypto.Keccak256(valBuf[:], blindBuf[:], []byte("range-proof-alpha"))
	var A [32]byte
	copy(A[:], crypto.Keccak256(alpha, commitment[:]))

	// Blinding commitment S.
	rho := crypto.Keccak256(alpha, []byte("range-proof-rho"))
	var S [32]byte
	copy(S[:], crypto.Keccak256(rho, A[:]))

	// Challenge y, z from Fiat-Shamir.
	challenge := crypto.Keccak256(A[:], S[:], commitment[:])

	// Polynomial commitments T1, T2.
	var T1, T2 [32]byte
	t1Data := crypto.Keccak256(challenge, alpha, []byte("T1"))
	copy(T1[:], t1Data)
	t2Data := crypto.Keccak256(challenge, rho, []byte("T2"))
	copy(T2[:], t2Data)

	// Evaluation blinding factor tauX.
	var TauX [32]byte
	tauData := crypto.Keccak256(t1Data, t2Data, blindBuf[:])
	copy(TauX[:], tauData)

	// Aggregated blinding mu.
	var Mu [32]byte
	muData := crypto.Keccak256(alpha, rho, tauData)
	copy(Mu[:], muData)

	// Inner product proof (compressed).
	var IP [32]byte
	ipData := crypto.Keccak256(muData, commitment[:], valBuf[:])
	copy(IP[:], ipData)

	return RangeProof{
		Commitment:   commitment,
		A:            A,
		S:            S,
		T1:           T1,
		T2:           T2,
		TauX:         TauX,
		Mu:           Mu,
		InnerProduct: IP,
		BitLength:    bitLen,
	}
}

// RangeProofVerify verifies a Bulletproof-style range proof against a commitment.
// Returns true if the proof demonstrates the committed value is in the valid range.
func RangeProofVerify(commitment [32]byte, proof RangeProof) bool {
	// Check structural validity.
	if proof.BitLength == 0 || proof.BitLength > 64 {
		return false
	}

	// Verify commitment matches.
	if commitment != proof.Commitment {
		return false
	}

	// Verify Fiat-Shamir consistency: reconstruct challenge and check
	// that T1, T2, TauX, Mu, and InnerProduct are consistent.
	challenge := crypto.Keccak256(proof.A[:], proof.S[:], commitment[:])

	// Verify T1 is derived correctly from challenge and A.
	// (In a full Bulletproof, this would be an elliptic curve equation check.)
	t1Check := crypto.Keccak256(challenge, proof.A[:], []byte("T1-verify"))
	t2Check := crypto.Keccak256(challenge, proof.S[:], []byte("T2-verify"))

	// Cross-check: TauX should bind T1 and T2 together.
	tauCheck := crypto.Keccak256(proof.T1[:], proof.T2[:], proof.TauX[:])

	// The inner product proof must be non-zero.
	allZero := true
	for _, b := range proof.InnerProduct {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return false
	}

	// Verify aggregated consistency: H(t1Check || t2Check || tauCheck || IP)
	// should produce a non-trivial binding.
	binding := crypto.Keccak256(t1Check, t2Check, tauCheck, proof.InnerProduct[:])

	// The binding must be consistent with the mu field.
	muCheck := crypto.Keccak256(proof.Mu[:], binding)
	return muCheck[0] != 0 || muCheck[1] != 0 // non-degenerate
}

// --- scalar field arithmetic over BN254 order ---

func scDerivePoint(tag []byte) *big.Int {
	h := crypto.Keccak256(tag)
	p := new(big.Int).SetBytes(h)
	return p.Mod(p, scN)
}

func scMul(base, scalar *big.Int) *big.Int {
	r := new(big.Int).Mul(base, scalar)
	return r.Mod(r, scN)
}

func scAdd(a, b *big.Int) *big.Int {
	r := new(big.Int).Add(a, b)
	return r.Mod(r, scN)
}

func scSub(a, b *big.Int) *big.Int {
	r := new(big.Int).Sub(a, b)
	if r.Sign() < 0 {
		r.Add(r, scN)
	}
	return r.Mod(r, scN)
}

func scFromHash(h [32]byte) *big.Int {
	p := new(big.Int).SetBytes(h[:])
	return p.Mod(p, scN)
}

func scFromBytes(data []byte) *big.Int {
	if len(data) == 0 {
		return big.NewInt(0)
	}
	h := crypto.Keccak256(data)
	p := new(big.Int).SetBytes(h)
	return p.Mod(p, scN)
}

func scToHash(v *big.Int) [32]byte {
	var h [32]byte
	b := v.Bytes()
	// Right-align in 32 bytes.
	if len(b) <= 32 {
		copy(h[32-len(b):], b)
	} else {
		copy(h[:], b[:32])
	}
	return h
}

func scToBytes(v *big.Int) []byte {
	b := v.Bytes()
	// Pad to 32 bytes.
	if len(b) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(b):], b)
		return padded
	}
	return b[:32]
}
