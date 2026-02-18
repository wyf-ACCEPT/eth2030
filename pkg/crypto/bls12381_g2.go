package crypto

// BLS12-381 G2 point operations over the twist curve y^2 = x^3 + 4(1+u)
// in F_p^2 where F_p^2 = F_p[u]/(u^2+1).
//
// Points are represented in Jacobian coordinates (X, Y, Z) where
// X, Y, Z are elements of F_p^2.

import "math/big"

// BlsG2Point represents a point on the BLS12-381 G2 twisted curve.
type BlsG2Point struct {
	x, y, z *blsFp2
}

// BLS12-381 twist curve coefficient: b' = 4(1+u)
var blsTwistB = &blsFp2{
	c0: big.NewInt(4),
	c1: big.NewInt(4),
}

// BLS12-381 G2 generator point coordinates.
var (
	blsG2GenXc0, _ = new(big.Int).SetString(
		"024aa2b2f08f0a91260805272dc51051c6e47ad4fa403b02b4510b647ae3d1770bac0326a805bbefd48056c8c121bdb8", 16)
	blsG2GenXc1, _ = new(big.Int).SetString(
		"13e02b6052719f607dacd3a088274f65596bd0d09920b61ab5da61bbdc7f5049334cf11213945d57e5ac7d055d042b7e", 16)
	blsG2GenYc0, _ = new(big.Int).SetString(
		"0ce5d527727d6e118cc9cdc6da2e351aadfd9baa8cbdd3a76d429a695160d12c923ac9cc3baca289e193548608b82801", 16)
	blsG2GenYc1, _ = new(big.Int).SetString(
		"0606c4a02ea734cc32acd2b02bc28b99cb3e287e85a763af267492ab572e99ab3f370d275cec1da1aaa9075ff05f79be", 16)
)

// BlsG2Generator returns the generator of G2.
func BlsG2Generator() *BlsG2Point {
	return &BlsG2Point{
		x: &blsFp2{c0: new(big.Int).Set(blsG2GenXc0), c1: new(big.Int).Set(blsG2GenXc1)},
		y: &blsFp2{c0: new(big.Int).Set(blsG2GenYc0), c1: new(big.Int).Set(blsG2GenYc1)},
		z: blsFp2One(),
	}
}

// BlsG2Infinity returns the point at infinity for G2.
func BlsG2Infinity() *BlsG2Point {
	return &BlsG2Point{
		x: blsFp2One(),
		y: blsFp2One(),
		z: blsFp2Zero(),
	}
}

func (p *BlsG2Point) blsG2IsInfinity() bool {
	return p.z.isZero()
}

// blsG2FromAffine creates a G2 point from affine coordinates.
func blsG2FromAffine(x, y *blsFp2) *BlsG2Point {
	if x.isZero() && y.isZero() {
		return BlsG2Infinity()
	}
	return &BlsG2Point{
		x: newBlsFp2(x.c0, x.c1),
		y: newBlsFp2(y.c0, y.c1),
		z: blsFp2One(),
	}
}

// blsG2ToAffine converts from Jacobian to affine coordinates.
func (p *BlsG2Point) blsG2ToAffine() (x, y *blsFp2) {
	if p.blsG2IsInfinity() {
		return blsFp2Zero(), blsFp2Zero()
	}
	zInv := blsFp2Inv(p.z)
	zInv2 := blsFp2Sqr(zInv)
	zInv3 := blsFp2Mul(zInv2, zInv)
	return blsFp2Mul(p.x, zInv2), blsFp2Mul(p.y, zInv3)
}

// blsG2IsOnCurve checks if the affine point is on y^2 = x^3 + 4(1+u).
func blsG2IsOnCurve(x, y *blsFp2) bool {
	if x.isZero() && y.isZero() {
		return true
	}
	// Check coordinates are in range [0, p).
	xr0 := new(big.Int).Mod(x.c0, blsP)
	xr1 := new(big.Int).Mod(x.c1, blsP)
	yr0 := new(big.Int).Mod(y.c0, blsP)
	yr1 := new(big.Int).Mod(y.c1, blsP)
	if xr0.Cmp(x.c0) != 0 || xr1.Cmp(x.c1) != 0 {
		return false
	}
	if yr0.Cmp(y.c0) != 0 || yr1.Cmp(y.c1) != 0 {
		return false
	}
	// y^2 == x^3 + b'
	lhs := blsFp2Sqr(y)
	rhs := blsFp2Add(blsFp2Mul(blsFp2Sqr(x), x), blsTwistB)
	return lhs.equal(rhs)
}

// blsG2Add adds two G2 points in Jacobian coordinates.
func blsG2Add(a, b *BlsG2Point) *BlsG2Point {
	if a.blsG2IsInfinity() {
		return &BlsG2Point{newBlsFp2(b.x.c0, b.x.c1), newBlsFp2(b.y.c0, b.y.c1), newBlsFp2(b.z.c0, b.z.c1)}
	}
	if b.blsG2IsInfinity() {
		return &BlsG2Point{newBlsFp2(a.x.c0, a.x.c1), newBlsFp2(a.y.c0, a.y.c1), newBlsFp2(a.z.c0, a.z.c1)}
	}

	z1sq := blsFp2Sqr(a.z)
	z2sq := blsFp2Sqr(b.z)
	u1 := blsFp2Mul(a.x, z2sq)
	u2 := blsFp2Mul(b.x, z1sq)
	s1 := blsFp2Mul(a.y, blsFp2Mul(b.z, z2sq))
	s2 := blsFp2Mul(b.y, blsFp2Mul(a.z, z1sq))

	if u1.equal(u2) {
		if s1.equal(s2) {
			return blsG2Double(a)
		}
		return BlsG2Infinity()
	}

	h := blsFp2Sub(u2, u1)
	i := blsFp2Sqr(blsFp2Add(h, h))
	j := blsFp2Mul(h, i)
	r := blsFp2Sub(s2, s1)
	r = blsFp2Add(r, r)
	v := blsFp2Mul(u1, i)

	x3 := blsFp2Sub(blsFp2Sub(blsFp2Sqr(r), j), blsFp2Add(v, v))
	y3 := blsFp2Sub(blsFp2Mul(r, blsFp2Sub(v, x3)), blsFp2Add(blsFp2Mul(s1, j), blsFp2Mul(s1, j)))
	z3 := blsFp2Mul(blsFp2Sub(blsFp2Sub(blsFp2Sqr(blsFp2Add(a.z, b.z)), z1sq), z2sq), h)

	return &BlsG2Point{x: x3, y: y3, z: z3}
}

// blsG2Double doubles a G2 point in Jacobian coordinates.
func blsG2Double(a *BlsG2Point) *BlsG2Point {
	if a.blsG2IsInfinity() {
		return BlsG2Infinity()
	}

	A := blsFp2Sqr(a.x)
	B := blsFp2Sqr(a.y)
	C := blsFp2Sqr(B)

	D := blsFp2Sub(blsFp2Sub(blsFp2Sqr(blsFp2Add(a.x, B)), A), C)
	D = blsFp2Add(D, D)

	E := blsFp2Add(blsFp2Add(A, A), A)

	x3 := blsFp2Sub(blsFp2Sqr(E), blsFp2Add(D, D))

	eightC := blsFp2Add(blsFp2Add(blsFp2Add(C, C), blsFp2Add(C, C)), blsFp2Add(blsFp2Add(C, C), blsFp2Add(C, C)))
	y3 := blsFp2Sub(blsFp2Mul(E, blsFp2Sub(D, x3)), eightC)

	z3 := blsFp2Mul(blsFp2Add(a.y, a.y), a.z)

	return &BlsG2Point{x: x3, y: y3, z: z3}
}

// blsG2Neg returns -P.
func blsG2Neg(p *BlsG2Point) *BlsG2Point {
	if p.blsG2IsInfinity() {
		return BlsG2Infinity()
	}
	return &BlsG2Point{
		x: newBlsFp2(p.x.c0, p.x.c1),
		y: blsFp2Neg(p.y),
		z: newBlsFp2(p.z.c0, p.z.c1),
	}
}

// blsG2ScalarMul computes k*P for a G2 point using double-and-add.
func blsG2ScalarMul(p *BlsG2Point, k *big.Int) *BlsG2Point {
	if k.Sign() == 0 || p.blsG2IsInfinity() {
		return BlsG2Infinity()
	}
	kMod := new(big.Int).Mod(k, blsR)
	if kMod.Sign() == 0 {
		return BlsG2Infinity()
	}

	r := BlsG2Infinity()
	base := &BlsG2Point{
		x: newBlsFp2(p.x.c0, p.x.c1),
		y: newBlsFp2(p.y.c0, p.y.c1),
		z: newBlsFp2(p.z.c0, p.z.c1),
	}
	for i := kMod.BitLen() - 1; i >= 0; i-- {
		r = blsG2Double(r)
		if kMod.Bit(i) == 1 {
			r = blsG2Add(r, base)
		}
	}
	return r
}

// blsG2InSubgroup checks if a point is in the r-torsion subgroup of G2.
// We check [r]*P == O.
func blsG2InSubgroup(p *BlsG2Point) bool {
	if p.blsG2IsInfinity() {
		return true
	}
	result := blsG2ScalarMul(p, blsR)
	return result.blsG2IsInfinity()
}
