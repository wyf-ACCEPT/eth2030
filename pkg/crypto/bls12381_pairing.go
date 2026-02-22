package crypto

// BLS12-381 optimal ate pairing implementation.
//
// The pairing e: G1 x G2 -> GT is computed using the optimal ate pairing,
// which consists of a Miller loop followed by a final exponentiation.
//
// The tower of extensions:
//   Fp -> Fp2 = Fp[u]/(u^2+1)
//   Fp2 -> Fp6 = Fp2[v]/(v^3 - (1+u))
//   Fp6 -> Fp12 = Fp6[w]/(w^2 - v)
//
// The BLS12-381 parameter: x = -0xd201000000010000

import "math/big"

// BLS12-381 curve parameter x (negative).
var blsX, _ = new(big.Int).SetString("d201000000010000", 16)

// --- Fp6 = Fp2[v]/(v^3 - (1+u)) ---

type blsFp6 struct {
	c0, c1, c2 *blsFp2
}

func blsFp6Zero() *blsFp6 {
	return &blsFp6{c0: blsFp2Zero(), c1: blsFp2Zero(), c2: blsFp2Zero()}
}

func blsFp6One() *blsFp6 {
	return &blsFp6{c0: blsFp2One(), c1: blsFp2Zero(), c2: blsFp2Zero()}
}

func blsFp6Add(a, b *blsFp6) *blsFp6 {
	return &blsFp6{
		c0: blsFp2Add(a.c0, b.c0),
		c1: blsFp2Add(a.c1, b.c1),
		c2: blsFp2Add(a.c2, b.c2),
	}
}

func blsFp6Sub(a, b *blsFp6) *blsFp6 {
	return &blsFp6{
		c0: blsFp2Sub(a.c0, b.c0),
		c1: blsFp2Sub(a.c1, b.c1),
		c2: blsFp2Sub(a.c2, b.c2),
	}
}

// blsFp2MulByNonResidue multiplies an Fp2 element by the non-residue (1+u).
// (1+u) * (a + b*u) = (a - b) + (a + b)*u
func blsFp2MulByNonResidue(e *blsFp2) *blsFp2 {
	return &blsFp2{
		c0: blsFpSub(e.c0, e.c1),
		c1: blsFpAdd(e.c0, e.c1),
	}
}

func blsFp6Mul(a, b *blsFp6) *blsFp6 {
	// Karatsuba multiplication in Fp6.
	t0 := blsFp2Mul(a.c0, b.c0)
	t1 := blsFp2Mul(a.c1, b.c1)
	t2 := blsFp2Mul(a.c2, b.c2)

	c0 := blsFp2Add(t0, blsFp2MulByNonResidue(
		blsFp2Sub(blsFp2Mul(blsFp2Add(a.c1, a.c2), blsFp2Add(b.c1, b.c2)), blsFp2Add(t1, t2))))
	c1 := blsFp2Add(blsFp2Sub(blsFp2Mul(blsFp2Add(a.c0, a.c1), blsFp2Add(b.c0, b.c1)), blsFp2Add(t0, t1)),
		blsFp2MulByNonResidue(t2))
	c2 := blsFp2Add(blsFp2Sub(blsFp2Mul(blsFp2Add(a.c0, a.c2), blsFp2Add(b.c0, b.c2)), blsFp2Add(t0, t2)), t1)

	return &blsFp6{c0: c0, c1: c1, c2: c2}
}

func blsFp6Sqr(a *blsFp6) *blsFp6 {
	s0 := blsFp2Sqr(a.c0)
	ab := blsFp2Mul(a.c0, a.c1)
	s1 := blsFp2Add(ab, ab)
	s2 := blsFp2Sqr(blsFp2Sub(blsFp2Add(a.c0, a.c2), a.c1))
	bc := blsFp2Mul(a.c1, a.c2)
	s3 := blsFp2Add(bc, bc)
	s4 := blsFp2Sqr(a.c2)

	c0 := blsFp2Add(s0, blsFp2MulByNonResidue(s3))
	c1 := blsFp2Add(s1, blsFp2MulByNonResidue(s4))
	c2 := blsFp2Add(blsFp2Add(blsFp2Add(s1, s2), s3), blsFp2Sub(blsFp2Neg(s0), s4))

	return &blsFp6{c0: c0, c1: c1, c2: c2}
}

func blsFp6Neg(a *blsFp6) *blsFp6 {
	return &blsFp6{
		c0: blsFp2Neg(a.c0),
		c1: blsFp2Neg(a.c1),
		c2: blsFp2Neg(a.c2),
	}
}

func blsFp6Inv(a *blsFp6) *blsFp6 {
	t0 := blsFp2Sqr(a.c0)
	t1 := blsFp2Sqr(a.c1)
	t2 := blsFp2Sqr(a.c2)
	t3 := blsFp2Mul(a.c0, a.c1)
	t4 := blsFp2Mul(a.c0, a.c2)
	t5 := blsFp2Mul(a.c1, a.c2)

	c0 := blsFp2Sub(t0, blsFp2MulByNonResidue(t5))
	c1 := blsFp2Sub(blsFp2MulByNonResidue(t2), t3)
	c2 := blsFp2Sub(t1, t4)

	t6 := blsFp2Mul(a.c0, c0)
	t6 = blsFp2Add(t6, blsFp2MulByNonResidue(blsFp2Add(blsFp2Mul(a.c2, c1), blsFp2Mul(a.c1, c2))))
	t6 = blsFp2Inv(t6)

	return &blsFp6{
		c0: blsFp2Mul(c0, t6),
		c1: blsFp2Mul(c1, t6),
		c2: blsFp2Mul(c2, t6),
	}
}

// --- Fp12 = Fp6[w]/(w^2 - v) ---

type blsFp12 struct {
	c0, c1 *blsFp6
}

func blsFp12Zero() *blsFp12 {
	return &blsFp12{c0: blsFp6Zero(), c1: blsFp6Zero()}
}

func blsFp12One() *blsFp12 {
	return &blsFp12{c0: blsFp6One(), c1: blsFp6Zero()}
}

func blsFp12Mul(a, b *blsFp12) *blsFp12 {
	t0 := blsFp6Mul(a.c0, b.c0)
	t1 := blsFp6Mul(a.c1, b.c1)

	// c0 = t0 + t1*v (where v is the Fp6 non-residue: multiply by v means shift in Fp6)
	c0 := blsFp6Add(t0, blsFp6MulByV(t1))
	// c1 = (a.c0 + a.c1)(b.c0 + b.c1) - t0 - t1
	c1 := blsFp6Sub(blsFp6Sub(blsFp6Mul(blsFp6Add(a.c0, a.c1), blsFp6Add(b.c0, b.c1)), t0), t1)

	return &blsFp12{c0: c0, c1: c1}
}

func blsFp12Sqr(a *blsFp12) *blsFp12 {
	ab := blsFp6Mul(a.c0, a.c1)
	c0 := blsFp6Add(blsFp6Mul(blsFp6Add(a.c0, a.c1), blsFp6Add(a.c0, blsFp6MulByV(a.c1))),
		blsFp6Neg(blsFp6Add(ab, blsFp6MulByV(ab))))
	c1 := blsFp6Add(ab, ab)
	return &blsFp12{c0: c0, c1: c1}
}

func blsFp12Inv(a *blsFp12) *blsFp12 {
	t := blsFp6Sub(blsFp6Sqr(a.c0), blsFp6MulByV(blsFp6Sqr(a.c1)))
	t = blsFp6Inv(t)
	return &blsFp12{
		c0: blsFp6Mul(a.c0, t),
		c1: blsFp6Neg(blsFp6Mul(a.c1, t)),
	}
}

func blsFp12Conj(a *blsFp12) *blsFp12 {
	return &blsFp12{
		c0: &blsFp6{
			c0: newBlsFp2(a.c0.c0.c0, a.c0.c0.c1),
			c1: newBlsFp2(a.c0.c1.c0, a.c0.c1.c1),
			c2: newBlsFp2(a.c0.c2.c0, a.c0.c2.c1),
		},
		c1: blsFp6Neg(a.c1),
	}
}

// blsFp6MulByV multiplies an Fp6 element by v (the Fp6 variable).
// v * (c0 + c1*v + c2*v^2) = c2*(1+u) + c0*v + c1*v^2
func blsFp6MulByV(a *blsFp6) *blsFp6 {
	return &blsFp6{
		c0: blsFp2MulByNonResidue(a.c2),
		c1: newBlsFp2(a.c0.c0, a.c0.c1),
		c2: newBlsFp2(a.c1.c0, a.c1.c1),
	}
}

// --- Frobenius constants for BLS12-381 ---

// frobCoeffs1 are the Frobenius p^1 constants for Fp12.
// These are xi^((p-1)*i/6) for the appropriate i values.
// For BLS12-381: xi = 1+u, p = blsP.

var (
	// Frobenius coefficient for Fp6.c1 under p-power Frobenius.
	blsFrobFp6C1_1 = blsFp2FromHex(
		"00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"1a0111ea397fe699ec02408663d4de85aa0d857d89759ad4897d29650fb85f9b409427eb4f49fffd8bfd00000000aaac",
	)
	blsFrobFp6C1_2 = blsFp2FromHex(
		"00000000000000005f19672fdf76ce51ba69c6076a0f77eaddb3a93be6f89688de17d813620a00022e01fffffffefffe",
		"00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
	)
)

func blsFp2FromHex(c0hex, c1hex string) *blsFp2 {
	c0, _ := new(big.Int).SetString(c0hex, 16)
	c1, _ := new(big.Int).SetString(c1hex, 16)
	return &blsFp2{c0: c0, c1: c1}
}

// blsFp12FrobeniusP computes f^p for the Frobenius endomorphism.
func blsFp12FrobeniusP(f *blsFp12) *blsFp12 {
	// Apply conjugation (p-power Frobenius on Fp2 coefficients).
	// This is a simplified version; the full implementation requires
	// precomputed Frobenius constants for each coefficient position.
	// For the pairing check, we use exponentiation as a fallback.
	return blsFp12Exp(f, blsP)
}

// blsFp12Exp computes f^k in Fp12 using square-and-multiply.
func blsFp12Exp(f *blsFp12, k *big.Int) *blsFp12 {
	if k.Sign() == 0 {
		return blsFp12One()
	}
	result := blsFp12One()
	base := &blsFp12{
		c0: &blsFp6{
			c0: newBlsFp2(f.c0.c0.c0, f.c0.c0.c1),
			c1: newBlsFp2(f.c0.c1.c0, f.c0.c1.c1),
			c2: newBlsFp2(f.c0.c2.c0, f.c0.c2.c1),
		},
		c1: &blsFp6{
			c0: newBlsFp2(f.c1.c0.c0, f.c1.c0.c1),
			c1: newBlsFp2(f.c1.c1.c0, f.c1.c1.c1),
			c2: newBlsFp2(f.c1.c2.c0, f.c1.c2.c1),
		},
	}
	for i := k.BitLen() - 1; i >= 0; i-- {
		result = blsFp12Sqr(result)
		if k.Bit(i) == 1 {
			result = blsFp12Mul(result, base)
		}
	}
	return result
}

func (f *blsFp12) isOne() bool {
	return f.c0.c0.equal(blsFp2One()) &&
		f.c0.c1.isZero() &&
		f.c0.c2.isZero() &&
		f.c1.c0.isZero() &&
		f.c1.c1.isZero() &&
		f.c1.c2.isZero()
}

// --- Miller loop ---

// blsLineFunctionAdd computes the line function for point addition in the Miller loop.
// Given R (G2 twist point), Q (affine G2 twist coordinates), P (affine G1 point),
// computes the sparse Fp12 line evaluation and returns the new R = R + Q.
//
// For BLS12-381's D-twist, the untwist map sends (x',y') → (x'/w², y'/w³).
// The chord through the untwisted R' and Q' evaluated at P = (px, py),
// after multiplying by w³ to clear denominators, gives:
//
//	l(P) = (λ·rx - ry) + (-λ·px)·v + py·v·w
//
// where λ = (qy - ry)/(qx - rx), and v, w are the Fp12 tower variables.
func blsLineFunctionAdd(r *BlsG2Point, qx, qy *blsFp2, px, py *big.Int) (*blsFp12, *BlsG2Point) {
	if r.blsG2IsInfinity() {
		return blsFp12One(), blsG2FromAffine(qx, qy)
	}

	rx, ry := r.blsG2ToAffine()

	if rx.equal(qx) && ry.equal(qy) {
		return blsLineFunctionDouble(r, px, py)
	}

	// λ = (qy - ry) / (qx - rx) in Fp2.
	num := blsFp2Sub(qy, ry)
	den := blsFp2Sub(qx, rx)

	if den.isZero() {
		// Points have same x but different y: vertical line.
		// This contributes a factor killed by the final exponentiation.
		return blsFp12One(), BlsG2Infinity()
	}

	lambda := blsFp2Mul(num, blsFp2Inv(den))

	// Sparse Fp12 line evaluation:
	//   c0 = Fp6{λ·rx - ry, -λ·px, 0}
	//   c1 = Fp6{0, py, 0}
	ell0 := blsFp2Sub(blsFp2Mul(lambda, rx), ry)
	ell1 := blsFp2Neg(blsFp2MulScalar(lambda, px))

	f := &blsFp12{
		c0: &blsFp6{c0: ell0, c1: ell1, c2: blsFp2Zero()},
		c1: &blsFp6{c0: blsFp2Zero(), c1: &blsFp2{c0: new(big.Int).Set(py), c1: new(big.Int)}, c2: blsFp2Zero()},
	}

	newR := blsG2Add(r, blsG2FromAffine(qx, qy))
	return f, newR
}

// blsLineFunctionDouble computes the line function for point doubling.
// Given R (G2 twist point), P (affine G1 point), computes the sparse Fp12
// line evaluation and returns the new R = 2R.
//
// The tangent line at the untwisted R' evaluated at P, after the twist map:
//
//	l(P) = (λ·rx - ry) + (-λ·px)·v + py·v·w
//
// where λ = 3·rx²/(2·ry).
func blsLineFunctionDouble(r *BlsG2Point, px, py *big.Int) (*blsFp12, *BlsG2Point) {
	if r.blsG2IsInfinity() {
		return blsFp12One(), BlsG2Infinity()
	}

	rx, ry := r.blsG2ToAffine()

	if ry.isZero() {
		return blsFp12One(), BlsG2Infinity()
	}

	// λ = 3·rx² / (2·ry) in Fp2 (a=0 for the twist curve).
	rxSq := blsFp2Sqr(rx)
	three := &blsFp2{c0: big.NewInt(3), c1: new(big.Int)}
	two := &blsFp2{c0: big.NewInt(2), c1: new(big.Int)}
	num := blsFp2Mul(three, rxSq)
	den := blsFp2Mul(two, ry)
	lambda := blsFp2Mul(num, blsFp2Inv(den))

	// Sparse Fp12 line evaluation:
	//   c0 = Fp6{λ·rx - ry, -λ·px, 0}
	//   c1 = Fp6{0, py, 0}
	ell0 := blsFp2Sub(blsFp2Mul(lambda, rx), ry)
	ell1 := blsFp2Neg(blsFp2MulScalar(lambda, px))

	f := &blsFp12{
		c0: &blsFp6{c0: ell0, c1: ell1, c2: blsFp2Zero()},
		c1: &blsFp6{c0: blsFp2Zero(), c1: &blsFp2{c0: new(big.Int).Set(py), c1: new(big.Int)}, c2: blsFp2Zero()},
	}

	newR := blsG2Double(r)
	return f, newR
}

// blsMillerLoop computes the Miller loop for the optimal ate pairing.
// The loop iterates over the bits of the BLS parameter x.
func blsMillerLoop(p *BlsG1Point, q *BlsG2Point) *blsFp12 {
	if p.blsG1IsInfinity() || q.blsG2IsInfinity() {
		return blsFp12One()
	}

	// Get affine coordinates.
	px, py := p.blsG1ToAffine()
	qx, qy := q.blsG2ToAffine()

	f := blsFp12One()
	r := blsG2FromAffine(qx, qy)

	// Iterate over bits of x = 0xd201000000010000 (from second-highest bit down).
	// x in binary: 1101001000000001000000000000000000000000000000010000000000000000
	for i := blsX.BitLen() - 2; i >= 0; i-- {
		var lineF *blsFp12
		lineF, r = blsLineFunctionDouble(r, px, py)
		f = blsFp12Sqr(f)
		f = blsFp12Mul(f, lineF)

		if blsX.Bit(i) == 1 {
			lineF, r = blsLineFunctionAdd(r, qx, qy, px, py)
			f = blsFp12Mul(f, lineF)
		}
	}

	// For BLS12-381, x is negative, so we need to conjugate f and negate R.
	f = blsFp12Conj(f)

	return f
}

// blsFinalExponentiation computes f^((p^12-1)/r).
// This is split into:
//
//	f^((p^12-1)/r) = f^((p^6-1) * (p^2+1) * ((p^4-p^2+1)/r))
func blsFinalExponentiation(f *blsFp12) *blsFp12 {
	// Easy part: f^(p^6-1) * f^(p^2+1)

	// f1 = f^(p^6-1) = conj(f) * inv(f)
	// Since f^(p^6) = conj(f) for the unitary representation.
	fInv := blsFp12Inv(f)
	f1 := blsFp12Mul(blsFp12Conj(f), fInv)

	// f2 = f1^(p^2+1) = f1^(p^2) * f1
	f1p2 := blsFp12Exp(f1, new(big.Int).Mul(blsP, blsP))
	f2 := blsFp12Mul(f1p2, f1)

	// Hard part: f2^((p^4-p^2+1)/r)
	// This is the computationally expensive part. For correctness, we compute
	// it directly using exponentiation.
	// (p^4 - p^2 + 1) / r
	p2 := new(big.Int).Mul(blsP, blsP)
	p4 := new(big.Int).Mul(p2, p2)
	hardExp := new(big.Int).Sub(p4, p2)
	hardExp.Add(hardExp, big.NewInt(1))
	hardExp.Div(hardExp, blsR)

	return blsFp12Exp(f2, hardExp)
}

// blsMultiPairing checks if the product of pairings equals the identity.
// product(e(P_i, Q_i)) == 1 in GT
func blsMultiPairing(g1Points []*BlsG1Point, g2Points []*BlsG2Point) bool {
	// Compute the product of Miller loop values.
	f := blsFp12One()
	for i := range g1Points {
		if g1Points[i].blsG1IsInfinity() || g2Points[i].blsG2IsInfinity() {
			continue
		}
		fi := blsMillerLoop(g1Points[i], g2Points[i])
		f = blsFp12Mul(f, fi)
	}

	// Apply final exponentiation.
	result := blsFinalExponentiation(f)

	// Check if result is the identity element in GT.
	return result.isOne()
}

// Placeholder references to prevent "unused" errors for the Frobenius constants
// which will be used when the efficient Frobenius is implemented.
var (
	_ = blsFrobFp6C1_1
	_ = blsFrobFp6C1_2
)
