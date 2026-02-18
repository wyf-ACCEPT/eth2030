package crypto

// BN254 G2 point operations over the twisted curve y^2 = x^3 + 3/(9+i)
// in F_p^2.
//
// The twist maps G2 points from E'(F_p^2) to E(F_p^12).
// Points are represented in Jacobian coordinates (X, Y, Z) where
// X, Y, Z are elements of F_p^2.

import "math/big"

// G2Point represents a point on the BN254 G2 twisted curve.
type G2Point struct {
	x, y, z *fp2
}

// BN254 twist curve coefficient: b' = 3/(9+i) = 3 * (9+i)^(-1)
// Precomputed: b' = (19485874751759354771024239261021720505790618469301721065564631296452457478373 +
// 266929791119991161246907387137283842545076965332900288569378510910307636690*i)
var (
	twistBa0, _ = new(big.Int).SetString("19485874751759354771024239261021720505790618469301721065564631296452457478373", 10)
	twistBa1, _ = new(big.Int).SetString("266929791119991161246907387137283842545076965332900288569378510910307636690", 10)
	twistB      = &fp2{a0: twistBa0, a1: twistBa1}
)

// G2 generator point coordinates.
var (
	g2GenXa0, _ = new(big.Int).SetString("10857046999023057135944570762232829481370756359578518086990519993285655852781", 10)
	g2GenXa1, _ = new(big.Int).SetString("11559732032986387107991004021392285783925812861821192530917403151452391805634", 10)
	g2GenYa0, _ = new(big.Int).SetString("8495653923123431417604973247489272438418190587263600148770280649306958101930", 10)
	g2GenYa1, _ = new(big.Int).SetString("4082367875863433681332203403145435568316851327593401208105741076214120093531", 10)
)

// G2Generator returns the generator of G2.
func G2Generator() *G2Point {
	return &G2Point{
		x: &fp2{a0: new(big.Int).Set(g2GenXa0), a1: new(big.Int).Set(g2GenXa1)},
		y: &fp2{a0: new(big.Int).Set(g2GenYa0), a1: new(big.Int).Set(g2GenYa1)},
		z: fp2One(),
	}
}

// G2Infinity returns the point at infinity for G2.
func G2Infinity() *G2Point {
	return &G2Point{
		x: fp2One(),
		y: fp2One(),
		z: fp2Zero(),
	}
}

func (p *G2Point) g2IsInfinity() bool {
	return p.z.isZero()
}

// g2FromAffine creates a G2 point from affine coordinates.
func g2FromAffine(x, y *fp2) *G2Point {
	if x.isZero() && y.isZero() {
		return G2Infinity()
	}
	return &G2Point{
		x: newFp2(x.a0, x.a1),
		y: newFp2(y.a0, y.a1),
		z: fp2One(),
	}
}

// g2ToAffine converts from Jacobian to affine coordinates.
func (p *G2Point) g2ToAffine() (x, y *fp2) {
	if p.g2IsInfinity() {
		return fp2Zero(), fp2Zero()
	}
	zInv := fp2Inv(p.z)
	zInv2 := fp2Sqr(zInv)
	zInv3 := fp2Mul(zInv2, zInv)
	return fp2Mul(p.x, zInv2), fp2Mul(p.y, zInv3)
}

// g2IsOnCurve checks if the affine point is on y^2 = x^3 + b'.
func g2IsOnCurve(x, y *fp2) bool {
	if x.isZero() && y.isZero() {
		return true
	}
	// Check coordinates are in range [0, p).
	xr0 := new(big.Int).Mod(x.a0, bn254P)
	xr1 := new(big.Int).Mod(x.a1, bn254P)
	yr0 := new(big.Int).Mod(y.a0, bn254P)
	yr1 := new(big.Int).Mod(y.a1, bn254P)
	if xr0.Cmp(x.a0) != 0 || xr1.Cmp(x.a1) != 0 {
		return false
	}
	if yr0.Cmp(y.a0) != 0 || yr1.Cmp(y.a1) != 0 {
		return false
	}
	// y^2 == x^3 + b'
	lhs := fp2Sqr(y)
	rhs := fp2Add(fp2Mul(fp2Sqr(x), x), twistB)
	return lhs.equal(rhs)
}

// g2Add adds two G2 points in Jacobian coordinates.
func g2Add(a, b *G2Point) *G2Point {
	if a.g2IsInfinity() {
		return &G2Point{newFp2(b.x.a0, b.x.a1), newFp2(b.y.a0, b.y.a1), newFp2(b.z.a0, b.z.a1)}
	}
	if b.g2IsInfinity() {
		return &G2Point{newFp2(a.x.a0, a.x.a1), newFp2(a.y.a0, a.y.a1), newFp2(a.z.a0, a.z.a1)}
	}

	z1sq := fp2Sqr(a.z)
	z2sq := fp2Sqr(b.z)
	u1 := fp2Mul(a.x, z2sq)
	u2 := fp2Mul(b.x, z1sq)
	s1 := fp2Mul(a.y, fp2Mul(b.z, z2sq))
	s2 := fp2Mul(b.y, fp2Mul(a.z, z1sq))

	if u1.equal(u2) {
		if s1.equal(s2) {
			return g2Double(a)
		}
		return G2Infinity()
	}

	h := fp2Sub(u2, u1)
	i := fp2Sqr(fp2Add(h, h))
	j := fp2Mul(h, i)
	r := fp2Sub(s2, s1)
	r = fp2Add(r, r)
	v := fp2Mul(u1, i)

	x3 := fp2Sub(fp2Sub(fp2Sqr(r), j), fp2Add(v, v))
	y3 := fp2Sub(fp2Mul(r, fp2Sub(v, x3)), fp2Add(fp2Mul(s1, j), fp2Mul(s1, j)))
	z3 := fp2Mul(fp2Sub(fp2Sub(fp2Sqr(fp2Add(a.z, b.z)), z1sq), z2sq), h)

	return &G2Point{x: x3, y: y3, z: z3}
}

// g2Double doubles a G2 point in Jacobian coordinates.
func g2Double(a *G2Point) *G2Point {
	if a.g2IsInfinity() {
		return G2Infinity()
	}

	A := fp2Sqr(a.x)
	B := fp2Sqr(a.y)
	C := fp2Sqr(B)

	D := fp2Sub(fp2Sub(fp2Sqr(fp2Add(a.x, B)), A), C)
	D = fp2Add(D, D)

	E := fp2Add(fp2Add(A, A), A)

	x3 := fp2Sub(fp2Sqr(E), fp2Add(D, D))

	eightC := fp2Add(fp2Add(fp2Add(C, C), fp2Add(C, C)), fp2Add(fp2Add(C, C), fp2Add(C, C)))
	y3 := fp2Sub(fp2Mul(E, fp2Sub(D, x3)), eightC)

	z3 := fp2Mul(fp2Add(a.y, a.y), a.z)

	return &G2Point{x: x3, y: y3, z: z3}
}

// g2Neg returns -P.
func g2Neg(p *G2Point) *G2Point {
	if p.g2IsInfinity() {
		return G2Infinity()
	}
	return &G2Point{
		x: newFp2(p.x.a0, p.x.a1),
		y: fp2Neg(p.y),
		z: newFp2(p.z.a0, p.z.a1),
	}
}
