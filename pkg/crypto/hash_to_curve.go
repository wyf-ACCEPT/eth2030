// Hash-to-curve implementation for BLS12-381 G1 per IETF RFC 9380.
//
// This implements the hash_to_curve function that maps arbitrary byte strings
// to points on the BLS12-381 G1 curve. The process is:
//
//   1. expand_message_xmd: expand input to uniform random bytes using SHA-256
//   2. hash_to_field: produce two field elements from the expanded bytes
//   3. map_to_curve: map each field element to a curve point via SWU
//   4. Add the two mapped points
//   5. Clear cofactor to ensure the result is in the prime-order subgroup
//
// The map_to_curve step uses the Simplified SWU approach. The current
// implementation delegates to the existing map function (blsMapFpToG1) which
// maps an Fp element to a point on E: y^2 = x^3 + 4 using Shallue-van de
// Woestijne with sign alignment. A production implementation would use the
// SSWU map on the isogenous curve E' followed by the 11-isogeny, as specified
// in RFC 9380 Section 8.8.1.
//
// Constant-time note: The math/big library does not provide constant-time
// operations. This implementation is suitable for consensus verification
// (where inputs are public) but not for private key operations.

package crypto

import (
	"crypto/sha256"
	"errors"
	"math/big"
)

// HashToCurveG1 hashes a message to a G1 point using the given domain
// separation tag (DST), following the hash_to_curve specification for
// BLS12-381 G1 with suite BLS12381G1_XMD:SHA-256_SSWU_RO_.
//
// Steps:
//   1. u = hash_to_field(msg, DST, 2)  -- produce two Fp elements
//   2. Q0 = map_to_curve(u[0])
//   3. Q1 = map_to_curve(u[1])
//   4. R = Q0 + Q1
//   5. P = clear_cofactor(R)
func HashToCurveG1(msg, dst []byte) (*BlsG1Point, error) {
	if len(dst) > 255 {
		return nil, errors.New("hash_to_curve: DST too long")
	}

	// Step 1: Hash to two field elements using expand_message_xmd.
	u0, u1, err := hashToFieldG1(msg, dst)
	if err != nil {
		return nil, err
	}

	// Steps 2-3: Map each field element to G1.
	q0 := blsMapFpToG1(u0)
	q1 := blsMapFpToG1(u1)

	// Step 4: Add the two points.
	r := blsG1Add(q0, q1)

	// Step 5: Clear cofactor.
	r = clearCofactorG1(r)

	return r, nil
}

// EncodeToG1 is the non-uniform (encode_to_curve) variant that uses a
// single field element. Faster but not indifferentiable from a random oracle.
func EncodeToG1(msg, dst []byte) (*BlsG1Point, error) {
	if len(dst) > 255 {
		return nil, errors.New("hash_to_curve: DST too long")
	}

	u, err := hashToSingleFieldG1(msg, dst)
	if err != nil {
		return nil, err
	}

	q := blsMapFpToG1(u)
	return clearCofactorG1(q), nil
}

// --- expand_message_xmd (SHA-256) ---

// expandMessageXMD implements expand_message_xmd from RFC 9380 Section 5.3.1.
// Produces lenInBytes of pseudo-random output from msg and DST using SHA-256.
//
// The construction uses SHA-256 as the hash function H with:
//   - b_in_bytes = 32 (hash output size)
//   - r_in_bytes = 64 (hash input block size)
//
// Steps:
//   1. ell = ceil(len_in_bytes / b_in_bytes)
//   2. DST_prime = DST || I2OSP(len(DST), 1)
//   3. Z_pad = I2OSP(0, r_in_bytes)
//   4. l_i_b_str = I2OSP(len_in_bytes, 2)
//   5. msg_prime = Z_pad || msg || l_i_b_str || I2OSP(0, 1) || DST_prime
//   6. b_0 = H(msg_prime)
//   7. b_1 = H(b_0 || I2OSP(1, 1) || DST_prime)
//   8. for i in (2, ..., ell): b_i = H(strxor(b_0, b_{i-1}) || I2OSP(i, 1) || DST_prime)
//   9. uniform_bytes = b_1 || ... || b_ell, truncated to len_in_bytes
func expandMessageXMD(msg, dst []byte, lenInBytes int) ([]byte, error) {
	bInBytes := 32 // SHA-256 output size
	rInBytes := 64 // SHA-256 block size

	ell := (lenInBytes + bInBytes - 1) / bInBytes
	if ell > 255 {
		return nil, errors.New("expand_message_xmd: output too large")
	}
	if len(dst) > 255 {
		return nil, errors.New("expand_message_xmd: DST too long")
	}

	// DST_prime = DST || I2OSP(len(DST), 1)
	dstPrime := make([]byte, len(dst)+1)
	copy(dstPrime, dst)
	dstPrime[len(dst)] = byte(len(dst))

	// Z_pad = I2OSP(0, r_in_bytes)
	zPad := make([]byte, rInBytes)

	// l_i_b_str = I2OSP(len_in_bytes, 2)
	libStr := []byte{byte(lenInBytes >> 8), byte(lenInBytes)}

	// msg_prime = Z_pad || msg || l_i_b_str || I2OSP(0, 1) || DST_prime
	h := sha256.New()
	h.Write(zPad)
	h.Write(msg)
	h.Write(libStr)
	h.Write([]byte{0})
	h.Write(dstPrime)
	b0 := h.Sum(nil)

	// b_1 = H(b_0 || I2OSP(1, 1) || DST_prime)
	h.Reset()
	h.Write(b0)
	h.Write([]byte{1})
	h.Write(dstPrime)
	b1 := h.Sum(nil)

	uniform := make([]byte, 0, lenInBytes+bInBytes)
	uniform = append(uniform, b1...)
	bPrev := b1

	for i := 2; i <= ell; i++ {
		// strxor(b_0, b_{i-1})
		xored := make([]byte, bInBytes)
		for j := 0; j < bInBytes; j++ {
			xored[j] = b0[j] ^ bPrev[j]
		}
		h.Reset()
		h.Write(xored)
		h.Write([]byte{byte(i)})
		h.Write(dstPrime)
		bi := h.Sum(nil)
		uniform = append(uniform, bi...)
		bPrev = bi
	}

	return uniform[:lenInBytes], nil
}

// hashToFieldG1 produces two Fp elements from msg+DST using expand_message_xmd.
// Each field element is derived from L = 64 bytes (512 bits) to ensure
// uniform distribution after reduction mod p, per RFC 9380 Section 5.2.
// For BLS12-381, L = ceil((ceil(log2(p)) + k) / 8) = ceil((381 + 128) / 8) = 64.
func hashToFieldG1(msg, dst []byte) (*big.Int, *big.Int, error) {
	// count=2, m=1 (Fp is a prime field): 2 * 1 * 64 = 128 bytes
	uniform, err := expandMessageXMD(msg, dst, 128)
	if err != nil {
		return nil, nil, err
	}

	u0 := new(big.Int).SetBytes(uniform[:64])
	u0.Mod(u0, blsP)

	u1 := new(big.Int).SetBytes(uniform[64:128])
	u1.Mod(u1, blsP)

	return u0, u1, nil
}

// hashToSingleFieldG1 produces a single Fp element (count=1) for encode_to_curve.
func hashToSingleFieldG1(msg, dst []byte) (*big.Int, error) {
	uniform, err := expandMessageXMD(msg, dst, 64)
	if err != nil {
		return nil, err
	}
	u := new(big.Int).SetBytes(uniform[:64])
	u.Mod(u, blsP)
	return u, nil
}

// --- Simplified SWU map parameters for BLS12-381 G1 ---
//
// The SSWU map is defined on the isogenous curve E': y^2 = x^3 + A'x + B'
// with parameters (RFC 9380 Section 8.8.1):
//   A' = 0x144698a3b8e9433d693a02c96d4982b0ea985383ee66a8d8e8981aefd881ac98936f8da0e0f97f5cf428082d584c1d
//   B' = 0x12e2908d11688030018b12e8753eee3b2016c1f0f24f4070a0b9c14fcef35ef55a23215a316ceaa5d1cc48e98e172be0
//   Z = 11 (a non-square in Fp satisfying the conditions for SWU)

var (
	sswuA, _ = new(big.Int).SetString(
		"144698a3b8e9433d693a02c96d4982b0ea985383ee66a8d8e8981aefd881ac98936f8da0e0f97f5cf428082d584c1d", 16)
	sswuB, _ = new(big.Int).SetString(
		"12e2908d11688030018b12e8753eee3b2016c1f0f24f4070a0b9c14fcef35ef55a23215a316ceaa5d1cc48e98e172be0", 16)
	sswuZ = big.NewInt(11)
)

// SimplifiedSWU applies the Simplified SWU map to produce a point on the
// isogenous curve E': y^2 = x^3 + A'*x + B'.
//
// Returns (x, y) on E'. The caller is responsible for applying the
// 11-isogeny to map the result to E: y^2 = x^3 + 4.
//
// The map is defined as (RFC 9380 Section 6.6.2):
//   1. tv1 = 1 / (Z^2 * u^4 + Z * u^2)
//   2. x1 = (-B'/A') * (1 + tv1)  [if tv1==0: x1 = B'/(Z*A')]
//   3. gx1 = x1^3 + A'*x1 + B'
//   4. x2 = Z * u^2 * x1
//   5. gx2 = x2^3 + A'*x2 + B'
//   6. if is_square(gx1): x = x1, y = sqrt(gx1)
//      else:               x = x2, y = sqrt(gx2)
//   7. if sgn0(u) != sgn0(y): y = -y
func SimplifiedSWU(u *big.Int) (x, y *big.Int) {
	u2 := blsFpSqr(u)
	zU2 := blsFpMul(sswuZ, u2)
	zU2sq := blsFpSqr(zU2)
	tv1 := blsFpAdd(zU2sq, zU2)

	var x1 *big.Int
	if tv1.Sign() == 0 {
		x1 = blsFpMul(sswuB, blsFpInv(blsFpMul(sswuZ, sswuA)))
	} else {
		negBA := blsFpMul(blsFpNeg(sswuB), blsFpInv(sswuA))
		x1 = blsFpMul(negBA, blsFpAdd(big.NewInt(1), blsFpInv(tv1)))
	}

	gx1 := blsFpAdd(blsFpAdd(blsFpMul(blsFpSqr(x1), x1), blsFpMul(sswuA, x1)), sswuB)

	x2 := blsFpMul(zU2, x1)
	gx2 := blsFpAdd(blsFpAdd(blsFpMul(blsFpSqr(x2), x2), blsFpMul(sswuA, x2)), sswuB)

	if blsFpIsSquare(gx1) {
		x = x1
		y = blsFpSqrt(gx1)
	} else {
		x = x2
		y = blsFpSqrt(gx2)
	}

	if y == nil {
		return new(big.Int), new(big.Int)
	}

	if blsFpSgn0(u) != blsFpSgn0(y) {
		y = blsFpNeg(y)
	}
	return
}

// IsOnIsogenousCurve checks if (x, y) is on E': y^2 = x^3 + A'*x + B'.
func IsOnIsogenousCurve(x, y *big.Int) bool {
	lhs := blsFpSqr(y)
	rhs := blsFpAdd(blsFpAdd(blsFpMul(blsFpSqr(x), x), blsFpMul(sswuA, x)), sswuB)
	return lhs.Cmp(rhs) == 0
}

// --- Cofactor clearing ---

// clearCofactorG1 clears the G1 cofactor to map onto the prime-order subgroup.
// For BLS12-381 G1: h = (x-1)^2 / 3 where x is the BLS parameter.
// h = 0x396c8c005555e1568c00aaab0000aaab
func clearCofactorG1(p *BlsG1Point) *BlsG1Point {
	cofactor, _ := new(big.Int).SetString("396c8c005555e1568c00aaab0000aaab", 16)
	return blsG1ScalarMul(p, cofactor)
}

// --- DST handling ---

// DST constants for standard BLS signature suite.
var (
	// DSTHashToG1 is the standard DST for hashing to G1 in the BLS signature scheme.
	DSTHashToG1 = []byte("BLS_SIG_BLS12381G1_XMD:SHA-256_SSWU_RO_POP_")
)

// ValidateDST checks that a domain separation tag conforms to the spec
// requirements: non-empty and at most 255 bytes.
func ValidateDST(dst []byte) error {
	if len(dst) == 0 {
		return errors.New("hash_to_curve: empty DST")
	}
	if len(dst) > 255 {
		return errors.New("hash_to_curve: DST exceeds 255 bytes")
	}
	return nil
}

// --- Polynomial evaluation helpers ---

// evalPoly evaluates a polynomial at x using Horner's method: sum(c[i]*x^i).
func evalPoly(x *big.Int, coeffs []*big.Int) *big.Int {
	if len(coeffs) == 0 {
		return new(big.Int)
	}
	result := new(big.Int).Set(coeffs[len(coeffs)-1])
	for i := len(coeffs) - 2; i >= 0; i-- {
		result = blsFpMul(result, x)
		result = blsFpAdd(result, coeffs[i])
	}
	return result
}
