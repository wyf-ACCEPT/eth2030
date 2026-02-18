package crypto

// BLS12-381 extension field F_p^2 = F_p[u] / (u^2 + 1).
//
// Elements are represented as (c0 + c1*u) where c0, c1 in F_p.
// This is used for G2 point coordinates on the twist curve.

import "math/big"

// blsFp2 represents an element of F_p^2 as (c0 + c1*u).
type blsFp2 struct {
	c0, c1 *big.Int
}

func newBlsFp2(c0, c1 *big.Int) *blsFp2 {
	return &blsFp2{c0: new(big.Int).Set(c0), c1: new(big.Int).Set(c1)}
}

func blsFp2Zero() *blsFp2 {
	return &blsFp2{c0: new(big.Int), c1: new(big.Int)}
}

func blsFp2One() *blsFp2 {
	return &blsFp2{c0: big.NewInt(1), c1: new(big.Int)}
}

func (e *blsFp2) isZero() bool {
	return e.c0.Sign() == 0 && e.c1.Sign() == 0
}

func (e *blsFp2) equal(f *blsFp2) bool {
	a0 := new(big.Int).Mod(e.c0, blsP)
	a1 := new(big.Int).Mod(e.c1, blsP)
	b0 := new(big.Int).Mod(f.c0, blsP)
	b1 := new(big.Int).Mod(f.c1, blsP)
	return a0.Cmp(b0) == 0 && a1.Cmp(b1) == 0
}

// blsFp2Add returns e + f in F_p^2.
func blsFp2Add(e, f *blsFp2) *blsFp2 {
	return &blsFp2{
		c0: blsFpAdd(e.c0, f.c0),
		c1: blsFpAdd(e.c1, f.c1),
	}
}

// blsFp2Sub returns e - f in F_p^2.
func blsFp2Sub(e, f *blsFp2) *blsFp2 {
	return &blsFp2{
		c0: blsFpSub(e.c0, f.c0),
		c1: blsFpSub(e.c1, f.c1),
	}
}

// blsFp2Mul returns e * f in F_p^2.
// (a0 + a1*u)(b0 + b1*u) = (a0*b0 - a1*b1) + (a0*b1 + a1*b0)*u
func blsFp2Mul(e, f *blsFp2) *blsFp2 {
	v0 := blsFpMul(e.c0, f.c0)
	v1 := blsFpMul(e.c1, f.c1)
	return &blsFp2{
		c0: blsFpSub(v0, v1),
		c1: blsFpSub(blsFpMul(blsFpAdd(e.c0, e.c1), blsFpAdd(f.c0, f.c1)), blsFpAdd(v0, v1)),
	}
}

// blsFp2Sqr returns e^2 in F_p^2.
func blsFp2Sqr(e *blsFp2) *blsFp2 {
	ab := blsFpMul(e.c0, e.c1)
	return &blsFp2{
		c0: blsFpMul(blsFpAdd(e.c0, e.c1), blsFpSub(e.c0, e.c1)),
		c1: blsFpAdd(ab, ab),
	}
}

// blsFp2Neg returns -e in F_p^2.
func blsFp2Neg(e *blsFp2) *blsFp2 {
	return &blsFp2{
		c0: blsFpNeg(e.c0),
		c1: blsFpNeg(e.c1),
	}
}

// blsFp2Conj returns the conjugate of e: (c0 - c1*u).
func blsFp2Conj(e *blsFp2) *blsFp2 {
	return &blsFp2{
		c0: new(big.Int).Set(e.c0),
		c1: blsFpNeg(e.c1),
	}
}

// blsFp2Inv returns e^(-1) in F_p^2.
// (a + b*u)^(-1) = (a - b*u) / (a^2 + b^2)
func blsFp2Inv(e *blsFp2) *blsFp2 {
	t := blsFpAdd(blsFpSqr(e.c0), blsFpSqr(e.c1))
	inv := blsFpInv(t)
	return &blsFp2{
		c0: blsFpMul(e.c0, inv),
		c1: blsFpMul(blsFpNeg(e.c1), inv),
	}
}

// blsFp2MulScalar returns e * s where s is in F_p.
func blsFp2MulScalar(e *blsFp2, s *big.Int) *blsFp2 {
	return &blsFp2{
		c0: blsFpMul(e.c0, s),
		c1: blsFpMul(e.c1, s),
	}
}

// blsFp2Sgn0 returns the "sign" of an Fp2 element per the hash-to-curve spec.
// sign_0(x) = sgn0(x_0) || (x_0 == 0 && sgn0(x_1))
func blsFp2Sgn0(e *blsFp2) int {
	sign0 := blsFpSgn0(e.c0)
	zero0 := 0
	if new(big.Int).Mod(e.c0, blsP).Sign() == 0 {
		zero0 = 1
	}
	sign1 := blsFpSgn0(e.c1)
	return sign0 | (zero0 & sign1)
}

// blsFp2Sqrt returns a square root of e in Fp2, or nil if none exists.
// For p = 3 mod 4: sqrt(a) = a^((p^2+7)/16) with verification.
// We use the algorithm: since p = 3 mod 4, for Fp2 = Fp[u]/(u^2+1):
//   sqrt(a+bu) via the formula for sqrt in Fp2.
func blsFp2Sqrt(e *blsFp2) *blsFp2 {
	if e.isZero() {
		return blsFp2Zero()
	}

	// Compute norm = c0^2 + c1^2 (norm of the Fp2 element).
	norm := blsFpAdd(blsFpSqr(e.c0), blsFpSqr(e.c1))

	// Check that the norm is a quadratic residue.
	if !blsFpIsSquare(norm) {
		return nil
	}

	// alpha = norm^((p-3)/4) mod p
	exp := new(big.Int).Sub(blsP, big.NewInt(3))
	exp.Rsh(exp, 2)
	_ = blsFpExp(norm, exp)

	// x0 = (c0 + norm * alpha) / 2  (candidate for the real part of the sqrt)
	// But we need to be more careful. Let's use a direct approach.
	// t = norm^((p+1)/4) = sqrt(norm) in Fp
	sqrtNorm := blsFpSqrt(norm)
	if sqrtNorm == nil {
		return nil
	}

	// candidate x0 = (c0 + sqrtNorm) / 2
	two := big.NewInt(2)
	twoInv := blsFpInv(two)
	x0 := blsFpMul(blsFpAdd(e.c0, sqrtNorm), twoInv)

	if blsFpIsSquare(x0) {
		// sqrt of x0 exists in Fp
		sqrtX0 := blsFpSqrt(x0)
		if sqrtX0 == nil {
			return nil
		}
		// x1 = c1 / (2 * sqrtX0)
		x1 := blsFpMul(e.c1, blsFpInv(blsFpAdd(sqrtX0, sqrtX0)))
		result := &blsFp2{c0: sqrtX0, c1: x1}
		// Verify: result^2 == e
		if blsFp2Sqr(result).equal(e) {
			return result
		}
	}

	// Try the other candidate: x0 = (c0 - sqrtNorm) / 2
	x0 = blsFpMul(blsFpSub(e.c0, sqrtNorm), twoInv)
	if blsFpIsSquare(x0) {
		sqrtX0 := blsFpSqrt(x0)
		if sqrtX0 == nil {
			return nil
		}
		x1 := blsFpMul(e.c1, blsFpInv(blsFpAdd(sqrtX0, sqrtX0)))
		result := &blsFp2{c0: sqrtX0, c1: x1}
		if blsFp2Sqr(result).equal(e) {
			return result
		}
	}

	return nil
}

// blsFp2IsSquare checks if an Fp2 element is a quadratic residue.
// In Fp2 with p = 3 mod 4: a is QR iff norm(a) = c0^2 + c1^2 is QR in Fp.
func blsFp2IsSquare(e *blsFp2) bool {
	if e.isZero() {
		return true
	}
	norm := blsFpAdd(blsFpSqr(e.c0), blsFpSqr(e.c1))
	return blsFpIsSquare(norm)
}

// blsFp2MulByU multiplies e by the non-residue u in Fp2.
// u * (c0 + c1*u) = c1*u^2 + c0*u = -c1 + c0*u (since u^2 = -1).
func blsFp2MulByU(e *blsFp2) *blsFp2 {
	return &blsFp2{
		c0: blsFpNeg(e.c1),
		c1: new(big.Int).Set(e.c0),
	}
}
