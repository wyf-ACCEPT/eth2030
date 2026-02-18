package crypto

// BN254 extension field F_p^6 = F_p^2[v] / (v^3 - (9+i)).
//
// Elements are (c0 + c1*v + c2*v^2) where c0, c1, c2 in F_p^2.
// The non-residue for the extension is xi = (9+i), so v^3 = 9+i.

// fp6 represents an element of F_p^6.
type fp6 struct {
	c0, c1, c2 *fp2
}

func fp6Zero() *fp6 {
	return &fp6{c0: fp2Zero(), c1: fp2Zero(), c2: fp2Zero()}
}

func fp6One() *fp6 {
	return &fp6{c0: fp2One(), c1: fp2Zero(), c2: fp2Zero()}
}

func (e *fp6) isZero() bool {
	return e.c0.isZero() && e.c1.isZero() && e.c2.isZero()
}

// fp6Add returns e + f.
func fp6Add(e, f *fp6) *fp6 {
	return &fp6{
		c0: fp2Add(e.c0, f.c0),
		c1: fp2Add(e.c1, f.c1),
		c2: fp2Add(e.c2, f.c2),
	}
}

// fp6Sub returns e - f.
func fp6Sub(e, f *fp6) *fp6 {
	return &fp6{
		c0: fp2Sub(e.c0, f.c0),
		c1: fp2Sub(e.c1, f.c1),
		c2: fp2Sub(e.c2, f.c2),
	}
}

// fp6Neg returns -e.
func fp6Neg(e *fp6) *fp6 {
	return &fp6{
		c0: fp2Neg(e.c0),
		c1: fp2Neg(e.c1),
		c2: fp2Neg(e.c2),
	}
}

// fp6Mul returns e * f using Karatsuba.
// v^3 = xi = (9+i), so when reducing, multiply overflow by xi.
func fp6Mul(e, f *fp6) *fp6 {
	// Toom-Cook / Karatsuba for degree-2 polys over F_p^2.
	t0 := fp2Mul(e.c0, f.c0)
	t1 := fp2Mul(e.c1, f.c1)
	t2 := fp2Mul(e.c2, f.c2)

	// c0 = t0 + xi * ((c1+c2)(f1+f2) - t1 - t2)
	c0 := fp2Add(t0, fp2MulByNonResidue(
		fp2Sub(fp2Sub(fp2Mul(fp2Add(e.c1, e.c2), fp2Add(f.c1, f.c2)), t1), t2)))

	// c1 = (c0+c1)(f0+f1) - t0 - t1 + xi*t2
	c1 := fp2Add(
		fp2Sub(fp2Sub(fp2Mul(fp2Add(e.c0, e.c1), fp2Add(f.c0, f.c1)), t0), t1),
		fp2MulByNonResidue(t2))

	// c2 = (c0+c2)(f0+f2) - t0 - t2 + t1
	c2 := fp2Add(
		fp2Sub(fp2Sub(fp2Mul(fp2Add(e.c0, e.c2), fp2Add(f.c0, f.c2)), t0), t2),
		t1)

	return &fp6{c0: c0, c1: c1, c2: c2}
}

// fp6Sqr returns e^2.
func fp6Sqr(e *fp6) *fp6 {
	s0 := fp2Sqr(e.c0)
	ab := fp2Mul(e.c0, e.c1)
	s1 := fp2Add(ab, ab)
	s2 := fp2Sqr(fp2Sub(fp2Add(e.c0, e.c2), e.c1))
	bc := fp2Mul(e.c1, e.c2)
	s3 := fp2Add(bc, bc)
	s4 := fp2Sqr(e.c2)

	// c0 = s0 + xi*s3
	c0 := fp2Add(s0, fp2MulByNonResidue(s3))
	// c1 = s1 + xi*s4
	c1 := fp2Add(s1, fp2MulByNonResidue(s4))
	// c2 = s1 + s2 + s3 - s0 - s4
	c2 := fp2Sub(fp2Sub(fp2Add(fp2Add(s1, s2), s3), s0), s4)

	return &fp6{c0: c0, c1: c1, c2: c2}
}

// fp6Inv returns e^(-1).
func fp6Inv(e *fp6) *fp6 {
	// Using the formula for cubic extension inverses.
	// A = c0^2 - xi*c1*c2
	// B = xi*c2^2 - c0*c1
	// C = c1^2 - c0*c2
	// inv = 1/(c0*A + xi*(c2*B + c1*C))
	a := fp2Sub(fp2Sqr(e.c0), fp2MulByNonResidue(fp2Mul(e.c1, e.c2)))
	b := fp2Sub(fp2MulByNonResidue(fp2Sqr(e.c2)), fp2Mul(e.c0, e.c1))
	c := fp2Sub(fp2Sqr(e.c1), fp2Mul(e.c0, e.c2))

	f := fp2Add(fp2Mul(e.c0, a),
		fp2MulByNonResidue(fp2Add(fp2Mul(e.c2, b), fp2Mul(e.c1, c))))
	fInv := fp2Inv(f)

	return &fp6{
		c0: fp2Mul(a, fInv),
		c1: fp2Mul(b, fInv),
		c2: fp2Mul(c, fInv),
	}
}

// fp6MulByFp2 multiplies an fp6 element by an fp2 scalar (in the c0 position).
func fp6MulByFp2(e *fp6, s *fp2) *fp6 {
	return &fp6{
		c0: fp2Mul(e.c0, s),
		c1: fp2Mul(e.c1, s),
		c2: fp2Mul(e.c2, s),
	}
}
