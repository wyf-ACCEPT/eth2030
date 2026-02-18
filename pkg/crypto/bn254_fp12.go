package crypto

// BN254 extension field F_p^12 = F_p^6[w] / (w^2 - v).
//
// Elements are (c0 + c1*w) where c0, c1 in F_p^6.
// This is the target group for the pairing: G_T lives in F_p^12.

import "math/big"

// fp12 represents an element of F_p^12.
type fp12 struct {
	c0, c1 *fp6
}

func fp12Zero() *fp12 {
	return &fp12{c0: fp6Zero(), c1: fp6Zero()}
}

func fp12One() *fp12 {
	return &fp12{c0: fp6One(), c1: fp6Zero()}
}

func (e *fp12) isOne() bool {
	return !e.c0.c0.isZero() &&
		e.c0.c0.a0.Cmp(big.NewInt(1)) == 0 &&
		e.c0.c0.a1.Sign() == 0 &&
		e.c0.c1.isZero() && e.c0.c2.isZero() &&
		e.c1.isZero()
}

// fp12Mul returns e * f.
// (a + b*w)(c + d*w) = (ac + bd*v) + (ad + bc)*w
// where v^3 = xi, and w^2 = v, so bd*v means we shift bd into fp6 by multiplying
// by the element v (which shifts c0->c1->c2 with wrap via xi).
func fp12Mul(e, f *fp12) *fp12 {
	t1 := fp6Mul(e.c0, f.c0)
	t2 := fp6Mul(e.c1, f.c1)

	// c0 = t1 + t2*v (multiply t2 by v in F_p^6: shift coefficients)
	c0 := fp6Add(t1, fp6MulByV(t2))

	// c1 = (e.c0 + e.c1)(f.c0 + f.c1) - t1 - t2
	c1 := fp6Sub(fp6Sub(fp6Mul(fp6Add(e.c0, e.c1), fp6Add(f.c0, f.c1)), t1), t2)

	return &fp12{c0: c0, c1: c1}
}

// fp12Sqr returns e^2.
func fp12Sqr(e *fp12) *fp12 {
	ab := fp6Mul(e.c0, e.c1)

	// c0 = (a+b)(a+b*v) - ab - ab*v
	//    = a^2 + b^2*v
	t := fp6Add(e.c0, e.c1)
	u := fp6Add(e.c0, fp6MulByV(e.c1))
	c0 := fp6Sub(fp6Sub(fp6Mul(t, u), ab), fp6MulByV(ab))
	c1 := fp6Add(ab, ab)

	return &fp12{c0: c0, c1: c1}
}

// fp12Inv returns e^(-1).
func fp12Inv(e *fp12) *fp12 {
	// (a + b*w)^(-1) = (a - b*w) / (a^2 - b^2*v)
	t := fp6Sub(fp6Sqr(e.c0), fp6MulByV(fp6Sqr(e.c1)))
	tInv := fp6Inv(t)
	return &fp12{
		c0: fp6Mul(e.c0, tInv),
		c1: fp6Neg(fp6Mul(e.c1, tInv)),
	}
}

// fp12Conj returns the "conjugate" e.c0 - e.c1*w.
// For unitary elements (norm=1), this equals the inverse.
func fp12Conj(e *fp12) *fp12 {
	return &fp12{
		c0: &fp6{
			c0: newFp2(e.c0.c0.a0, e.c0.c0.a1),
			c1: newFp2(e.c0.c1.a0, e.c0.c1.a1),
			c2: newFp2(e.c0.c2.a0, e.c0.c2.a1),
		},
		c1: fp6Neg(e.c1),
	}
}

// fp6MulByV multiplies an fp6 element by v.
// In F_p^6 = F_p^2[v]/(v^3-xi), multiplying by v shifts:
// (c0 + c1*v + c2*v^2) * v = c2*xi + c0*v + c1*v^2
func fp6MulByV(e *fp6) *fp6 {
	return &fp6{
		c0: fp2MulByNonResidue(e.c2),
		c1: newFp2(e.c0.a0, e.c0.a1),
		c2: newFp2(e.c1.a0, e.c1.a1),
	}
}

// fp12Exp raises e to the power k in F_p^12.
func fp12Exp(e *fp12, k *big.Int) *fp12 {
	if k.Sign() == 0 {
		return fp12One()
	}
	r := fp12One()
	base := &fp12{
		c0: &fp6{
			c0: newFp2(e.c0.c0.a0, e.c0.c0.a1),
			c1: newFp2(e.c0.c1.a0, e.c0.c1.a1),
			c2: newFp2(e.c0.c2.a0, e.c0.c2.a1),
		},
		c1: &fp6{
			c0: newFp2(e.c1.c0.a0, e.c1.c0.a1),
			c1: newFp2(e.c1.c1.a0, e.c1.c1.a1),
			c2: newFp2(e.c1.c2.a0, e.c1.c2.a1),
		},
	}
	for i := k.BitLen() - 1; i >= 0; i-- {
		r = fp12Sqr(r)
		if k.Bit(i) == 1 {
			r = fp12Mul(r, base)
		}
	}
	return r
}
