package crypto

// KZG polynomial commitment verification for EIP-4844 point evaluation.
//
// The KZG (Kate-Zaverucha-Goldberg) scheme allows verifying that a
// polynomial p(X) committed as C = [p(s)]_1 evaluates to y at point z,
// given a proof pi = [(p(s) - y) / (s - z)]_1.
//
// The verification equation uses a pairing check:
//   e(C - [y]G1, G2) == e(pi, [s]G2 - [z]G2)
//
// Which is equivalent to checking:
//   e(C - [y]G1, G2) * e(-pi, [s]G2 - [z]G2) == 1
//
// The 48-byte compressed G1 format follows the ZCash serialization:
//   - Bit 7 of byte 0: compression flag (1 = compressed)
//   - Bit 6 of byte 0: infinity flag (1 = point at infinity)
//   - Bit 5 of byte 0: sort flag (1 = lexicographically largest y)
//   - Remaining bits: big-endian x coordinate

import (
	"errors"
	"math/big"
)

var (
	errKZGInvalidProof      = errors.New("kzg: invalid proof")
	errKZGInvalidCommitment = errors.New("kzg: invalid commitment")
	errKZGInvalidPoint      = errors.New("kzg: point not on curve")
	errKZGInvalidFieldElem  = errors.New("kzg: invalid field element")
	errKZGVerifyFailed      = errors.New("kzg: proof verification failed")
)

// KZG compressed G1 point size (ZCash format).
const kzgCompressedG1Size = 48

// kzgTrustedSetupG2 is [s]G2 from the trusted setup ceremony.
// For a real deployment this would be loaded from the actual Ethereum
// KZG ceremony output. Here we use a test point [s]G2 = [42]G2 so
// that unit tests can construct valid proofs against a known secret.
var kzgTrustedSetupG2 *BlsG2Point

func init() {
	secret := big.NewInt(42)
	kzgTrustedSetupG2 = blsG2ScalarMul(BlsG2Generator(), secret)
}

// KZGSetTrustedSetupG2 overrides the trusted setup G2 point.
// This is intended for testing only.
func KZGSetTrustedSetupG2(p *BlsG2Point) {
	kzgTrustedSetupG2 = p
}

// KZGGetTrustedSetupG2 returns the current [s]G2 point from the trusted setup.
func KZGGetTrustedSetupG2() *BlsG2Point {
	return kzgTrustedSetupG2
}

// KZGVerifyProof verifies a KZG opening proof.
//
// Given:
//   - commitment C (G1 point): the polynomial commitment [p(s)]_1
//   - z (scalar): the evaluation point
//   - y (scalar): the claimed evaluation p(z) = y
//   - proof pi (G1 point): the proof [(p(s) - y) / (s - z)]_1
//
// Verifies: e(C - [y]G1, G2) == e(pi, [s]G2 - [z]G2)
//
// This is equivalent to the pairing check:
//
//	e(C - [y]G1, G2) * e(-pi, [s]G2 - [z]G2) == 1
func KZGVerifyProof(commitment *BlsG1Point, z, y *big.Int, proof *BlsG1Point) bool {
	// Validate scalars are in range [0, r).
	if z.Sign() < 0 || z.Cmp(blsR) >= 0 {
		return false
	}
	if y.Sign() < 0 || y.Cmp(blsR) >= 0 {
		return false
	}

	g1Gen := BlsG1Generator()
	g2Gen := BlsG2Generator()

	// Compute LHS: C - [y]G1
	yG1 := blsG1ScalarMul(g1Gen, y)
	lhsG1 := blsG1Add(commitment, blsG1Neg(yG1))

	// Compute RHS G2: [s]G2 - [z]G2
	zG2 := blsG2ScalarMul(g2Gen, z)
	rhsG2 := blsG2Add(kzgTrustedSetupG2, blsG2Neg(zG2))

	// Pairing check: e(lhsG1, G2) * e(-proof, rhsG2) == 1
	// Equivalently: e(C - [y]G1, G2) * e(-pi, [s]G2 - [z]G2) == 1
	negProof := blsG1Neg(proof)

	return blsMultiPairing(
		[]*BlsG1Point{lhsG1, negProof},
		[]*BlsG2Point{g2Gen, rhsG2},
	)
}

// KZGDecompressG1 deserializes a 48-byte compressed G1 point in ZCash format.
//
// Format (big-endian, most significant byte first):
//   - Bit 7 of byte[0]: compression flag (must be 1)
//   - Bit 6 of byte[0]: infinity flag (1 if point at infinity)
//   - Bit 5 of byte[0]: sort flag (1 if y is the lexicographically larger value)
//   - Remaining 381 bits: x coordinate
func KZGDecompressG1(data []byte) (*BlsG1Point, error) {
	if len(data) != kzgCompressedG1Size {
		return nil, errKZGInvalidPoint
	}

	// Copy to avoid mutating input.
	buf := make([]byte, kzgCompressedG1Size)
	copy(buf, data)

	// Extract flags from the most significant byte.
	flags := buf[0] >> 5
	compressedFlag := (flags >> 2) & 1
	infinityFlag := (flags >> 1) & 1
	sortFlag := flags & 1

	// Must be compressed format.
	if compressedFlag != 1 {
		return nil, errKZGInvalidPoint
	}

	// Clear the flag bits to get the x coordinate.
	buf[0] &= 0x1f

	// Point at infinity.
	if infinityFlag == 1 {
		// All remaining bytes (including sort flag) must be zero per spec.
		if sortFlag != 0 {
			return nil, errKZGInvalidPoint
		}
		for _, b := range buf {
			if b != 0 {
				return nil, errKZGInvalidPoint
			}
		}
		return BlsG1Infinity(), nil
	}

	// Deserialize x coordinate.
	x := new(big.Int).SetBytes(buf)
	if x.Cmp(blsP) >= 0 {
		return nil, errKZGInvalidPoint
	}

	// Compute y from the curve equation y^2 = x^3 + 4.
	x3 := blsFpMul(blsFpSqr(x), x)
	rhs := blsFpAdd(x3, blsB) // x^3 + 4
	y := blsFpSqrt(rhs)
	if y == nil {
		return nil, errKZGInvalidPoint
	}

	// Choose the correct y based on the sort flag.
	// The sort flag indicates the "larger" y value.
	// In BLS12-381, the "larger" value is the one where y > (p-1)/2.
	pMinus1Over2 := new(big.Int).Sub(blsP, big.NewInt(1))
	pMinus1Over2.Rsh(pMinus1Over2, 1)

	yIsLarger := y.Cmp(pMinus1Over2) > 0
	if yIsLarger != (sortFlag == 1) {
		y = blsFpNeg(y)
	}

	// Validate the point is on the curve and in the subgroup.
	if !blsG1IsOnCurve(x, y) {
		return nil, errKZGInvalidPoint
	}

	p := blsG1FromAffine(x, y)

	if !blsG1InSubgroup(p) {
		return nil, errKZGInvalidPoint
	}

	return p, nil
}

// KZGCompressG1 serializes a G1 point to 48-byte compressed ZCash format.
func KZGCompressG1(p *BlsG1Point) []byte {
	out := make([]byte, kzgCompressedG1Size)

	if p.blsG1IsInfinity() {
		// Compression flag set, infinity flag set.
		out[0] = 0xc0
		return out
	}

	x, y := p.blsG1ToAffine()

	// Serialize x coordinate as big-endian 48 bytes.
	xBytes := x.Bytes()
	copy(out[kzgCompressedG1Size-len(xBytes):], xBytes)

	// Set compression flag (bit 7).
	out[0] |= 0x80

	// Set sort flag (bit 5) if y is the "larger" value.
	pMinus1Over2 := new(big.Int).Sub(blsP, big.NewInt(1))
	pMinus1Over2.Rsh(pMinus1Over2, 1)
	if y.Cmp(pMinus1Over2) > 0 {
		out[0] |= 0x20
	}

	return out
}

// KZGVerifyFromBytes verifies a KZG proof from raw byte inputs as used in
// the EIP-4844 point evaluation precompile.
//
// Parameters:
//   - commitment: 48-byte compressed G1 point
//   - z: 32-byte big-endian scalar (evaluation point)
//   - y: 32-byte big-endian scalar (claimed evaluation)
//   - proof: 48-byte compressed G1 point
//
// Returns nil on success, or an error describing the failure.
func KZGVerifyFromBytes(commitment []byte, z, y *big.Int, proof []byte) error {
	// Decompress the commitment.
	commitPoint, err := KZGDecompressG1(commitment)
	if err != nil {
		return errKZGInvalidCommitment
	}

	// Decompress the proof.
	proofPoint, err := KZGDecompressG1(proof)
	if err != nil {
		return errKZGInvalidProof
	}

	// Verify the pairing equation.
	if !KZGVerifyProof(commitPoint, z, y, proofPoint) {
		return errKZGVerifyFailed
	}

	return nil
}

// KZGCommit computes a KZG commitment for a polynomial evaluated at the
// trusted setup secret s. For testing purposes, this takes the polynomial
// value p(s) directly and returns [p(s)]G1.
func KZGCommit(polyAtS *big.Int) *BlsG1Point {
	return blsG1ScalarMul(BlsG1Generator(), polyAtS)
}

// KZGComputeProof computes a KZG opening proof for a polynomial commitment.
// Given the secret s, evaluation point z, and polynomial value p(s), y = p(z):
//
//	proof = [(p(s) - y) / (s - z)]G1
//
// This function is for testing: in practice the prover knows the polynomial
// coefficients and computes the proof differently.
func KZGComputeProof(secret, z, polyAtS, y *big.Int) *BlsG1Point {
	// quotient = (p(s) - y) / (s - z) mod r
	num := new(big.Int).Sub(polyAtS, y)
	num.Mod(num, blsR)
	den := new(big.Int).Sub(secret, z)
	den.Mod(den, blsR)
	denInv := new(big.Int).ModInverse(den, blsR)
	if denInv == nil {
		// s == z, which should not happen in practice.
		return BlsG1Infinity()
	}
	quotient := new(big.Int).Mul(num, denInv)
	quotient.Mod(quotient, blsR)
	return blsG1ScalarMul(BlsG1Generator(), quotient)
}
