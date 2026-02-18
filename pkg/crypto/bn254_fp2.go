package crypto

// BN254 extension field F_p^2 = F_p[i] / (i^2 + 1).
//
// Elements are represented as (a0 + a1*i) where a0, a1 in F_p.
// This is used for G2 point coordinates on the twist curve.

import "math/big"

// fp2 represents an element of F_p^2 as (a0 + a1*i).
type fp2 struct {
	a0, a1 *big.Int
}

func newFp2(a0, a1 *big.Int) *fp2 {
	return &fp2{a0: new(big.Int).Set(a0), a1: new(big.Int).Set(a1)}
}

func fp2Zero() *fp2 {
	return &fp2{a0: new(big.Int), a1: new(big.Int)}
}

func fp2One() *fp2 {
	return &fp2{a0: big.NewInt(1), a1: new(big.Int)}
}

func (e *fp2) isZero() bool {
	return e.a0.Sign() == 0 && e.a1.Sign() == 0
}

func (e *fp2) equal(f *fp2) bool {
	a0 := new(big.Int).Mod(e.a0, bn254P)
	a1 := new(big.Int).Mod(e.a1, bn254P)
	b0 := new(big.Int).Mod(f.a0, bn254P)
	b1 := new(big.Int).Mod(f.a1, bn254P)
	return a0.Cmp(b0) == 0 && a1.Cmp(b1) == 0
}

// fp2Add returns e + f in F_p^2.
func fp2Add(e, f *fp2) *fp2 {
	return &fp2{
		a0: fpAdd(e.a0, f.a0),
		a1: fpAdd(e.a1, f.a1),
	}
}

// fp2Sub returns e - f in F_p^2.
func fp2Sub(e, f *fp2) *fp2 {
	return &fp2{
		a0: fpSub(e.a0, f.a0),
		a1: fpSub(e.a1, f.a1),
	}
}

// fp2Mul returns e * f in F_p^2.
// (a0 + a1*i)(b0 + b1*i) = (a0*b0 - a1*b1) + (a0*b1 + a1*b0)*i
func fp2Mul(e, f *fp2) *fp2 {
	// Karatsuba optimization:
	// v0 = a0*b0, v1 = a1*b1
	// real = v0 - v1
	// imag = (a0+a1)*(b0+b1) - v0 - v1
	v0 := fpMul(e.a0, f.a0)
	v1 := fpMul(e.a1, f.a1)
	return &fp2{
		a0: fpSub(v0, v1),
		a1: fpSub(fpMul(fpAdd(e.a0, e.a1), fpAdd(f.a0, f.a1)), fpAdd(v0, v1)),
	}
}

// fp2Sqr returns e^2 in F_p^2.
func fp2Sqr(e *fp2) *fp2 {
	// (a + b*i)^2 = (a^2 - b^2) + 2*a*b*i
	// Optimized: (a+b)(a-b) for real part.
	ab := fpMul(e.a0, e.a1)
	return &fp2{
		a0: fpMul(fpAdd(e.a0, e.a1), fpSub(e.a0, e.a1)),
		a1: fpAdd(ab, ab),
	}
}

// fp2Neg returns -e in F_p^2.
func fp2Neg(e *fp2) *fp2 {
	return &fp2{
		a0: fpNeg(e.a0),
		a1: fpNeg(e.a1),
	}
}

// fp2Conj returns the conjugate of e: (a0 - a1*i).
func fp2Conj(e *fp2) *fp2 {
	return &fp2{
		a0: new(big.Int).Set(e.a0),
		a1: fpNeg(e.a1),
	}
}

// fp2Inv returns e^(-1) in F_p^2.
// (a + b*i)^(-1) = (a - b*i) / (a^2 + b^2)
func fp2Inv(e *fp2) *fp2 {
	// norm = a0^2 + a1^2 (since i^2 = -1)
	t := fpAdd(fpSqr(e.a0), fpSqr(e.a1))
	inv := fpInv(t)
	return &fp2{
		a0: fpMul(e.a0, inv),
		a1: fpMul(fpNeg(e.a1), inv),
	}
}

// fp2MulScalar returns e * s where s is in F_p.
func fp2MulScalar(e *fp2, s *big.Int) *fp2 {
	return &fp2{
		a0: fpMul(e.a0, s),
		a1: fpMul(e.a1, s),
	}
}

// fp2MulByNonResidue multiplies by the non-residue (9+i) used in the sextic
// twist for BN254. This is used in the tower extension F_p^6 and F_p^12.
// (a + b*i)(9 + i) = (9a - b) + (a + 9b)*i
func fp2MulByNonResidue(e *fp2) *fp2 {
	nine := big.NewInt(9)
	return &fp2{
		a0: fpSub(fpMul(e.a0, nine), e.a1),
		a1: fpAdd(fpMul(e.a1, nine), e.a0),
	}
}
