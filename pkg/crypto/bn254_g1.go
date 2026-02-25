package crypto

// BN254 G1 point operations over the curve y^2 = x^3 + 3 in F_p.
//
// Points are represented in Jacobian coordinates (X, Y, Z) where the
// affine point is (X/Z^2, Y/Z^3). The point at infinity has Z=0.

import "math/big"

// G1Point represents a point on the BN254 G1 curve in Jacobian coordinates.
type G1Point struct {
	x, y, z *big.Int
}

// G1Generator returns the generator of G1: (1, 2).
func G1Generator() *G1Point {
	return &G1Point{
		x: big.NewInt(1),
		y: big.NewInt(2),
		z: big.NewInt(1),
	}
}

// G1Infinity returns the point at infinity.
func G1Infinity() *G1Point {
	return &G1Point{
		x: big.NewInt(1),
		y: big.NewInt(1),
		z: new(big.Int),
	}
}

// Marshal serializes the G1 point to uncompressed affine bytes (64 bytes: X || Y).
func (p *G1Point) Marshal() []byte {
	if p.g1IsInfinity() {
		return make([]byte, 64)
	}
	ax, ay := p.g1ToAffine()
	out := make([]byte, 64)
	axBytes := ax.Bytes()
	ayBytes := ay.Bytes()
	copy(out[32-len(axBytes):32], axBytes)
	copy(out[64-len(ayBytes):64], ayBytes)
	return out
}

// g1IsInfinity returns true if the point is the identity (Z=0).
func (p *G1Point) g1IsInfinity() bool {
	return p.z.Sign() == 0
}

// g1FromAffine creates a Jacobian point from affine coordinates.
// (0,0) is treated as the point at infinity.
func g1FromAffine(x, y *big.Int) *G1Point {
	if x.Sign() == 0 && y.Sign() == 0 {
		return G1Infinity()
	}
	return &G1Point{
		x: new(big.Int).Set(x),
		y: new(big.Int).Set(y),
		z: big.NewInt(1),
	}
}

// g1ToAffine converts Jacobian to affine coordinates. Returns (0,0) for infinity.
func (p *G1Point) g1ToAffine() (x, y *big.Int) {
	if p.g1IsInfinity() {
		return new(big.Int), new(big.Int)
	}
	zInv := fpInv(p.z)
	zInv2 := fpSqr(zInv)
	zInv3 := fpMul(zInv2, zInv)
	return fpMul(p.x, zInv2), fpMul(p.y, zInv3)
}

// g1IsOnCurve checks if the affine point (x, y) is on y^2 = x^3 + 3.
// The point (0,0) is the identity and considered valid.
func g1IsOnCurve(x, y *big.Int) bool {
	if x.Sign() == 0 && y.Sign() == 0 {
		return true
	}
	// Check coordinates are in range.
	if x.Sign() < 0 || x.Cmp(bn254P) >= 0 {
		return false
	}
	if y.Sign() < 0 || y.Cmp(bn254P) >= 0 {
		return false
	}
	// y^2 == x^3 + 3 (mod p)
	lhs := fpSqr(y)
	rhs := fpAdd(fpMul(fpSqr(x), x), bn254B)
	return lhs.Cmp(rhs) == 0
}

// g1Add adds two G1 points in Jacobian coordinates.
func g1Add(a, b *G1Point) *G1Point {
	if a.g1IsInfinity() {
		return &G1Point{new(big.Int).Set(b.x), new(big.Int).Set(b.y), new(big.Int).Set(b.z)}
	}
	if b.g1IsInfinity() {
		return &G1Point{new(big.Int).Set(a.x), new(big.Int).Set(a.y), new(big.Int).Set(a.z)}
	}

	// Standard Jacobian addition.
	z1sq := fpSqr(a.z)
	z2sq := fpSqr(b.z)
	u1 := fpMul(a.x, z2sq)
	u2 := fpMul(b.x, z1sq)
	s1 := fpMul(a.y, fpMul(b.z, z2sq))
	s2 := fpMul(b.y, fpMul(a.z, z1sq))

	if u1.Cmp(u2) == 0 {
		if s1.Cmp(s2) == 0 {
			return g1Double(a)
		}
		return G1Infinity()
	}

	h := fpSub(u2, u1)
	i := fpSqr(fpAdd(h, h)) // i = (2h)^2
	j := fpMul(h, i)
	r := fpSub(s2, s1)
	r = fpAdd(r, r) // r = 2*(s2-s1)
	v := fpMul(u1, i)

	// X3 = r^2 - j - 2*v
	x3 := fpSub(fpSub(fpSqr(r), j), fpAdd(v, v))

	// Y3 = r*(v - x3) - 2*s1*j
	y3 := fpSub(fpMul(r, fpSub(v, x3)), fpAdd(fpMul(s1, j), fpMul(s1, j)))

	// Z3 = ((z1+z2)^2 - z1^2 - z2^2) * h
	z3 := fpMul(fpSub(fpSub(fpSqr(fpAdd(a.z, b.z)), z1sq), z2sq), h)

	return &G1Point{x: x3, y: y3, z: z3}
}

// g1Double doubles a G1 point in Jacobian coordinates.
func g1Double(a *G1Point) *G1Point {
	if a.g1IsInfinity() {
		return G1Infinity()
	}

	// For a=0 (BN254 has a=0 in y^2=x^3+ax+b).
	A := fpSqr(a.x)
	B := fpSqr(a.y)
	C := fpSqr(B)

	// D = 2*((x+B)^2 - A - C)
	D := fpSub(fpSub(fpSqr(fpAdd(a.x, B)), A), C)
	D = fpAdd(D, D)

	// E = 3*A
	E := fpAdd(fpAdd(A, A), A)

	// X3 = E^2 - 2*D
	x3 := fpSub(fpSqr(E), fpAdd(D, D))

	// Y3 = E*(D-X3) - 8*C
	eightC := fpAdd(fpAdd(fpAdd(C, C), fpAdd(C, C)), fpAdd(fpAdd(C, C), fpAdd(C, C)))
	y3 := fpSub(fpMul(E, fpSub(D, x3)), eightC)

	// Z3 = 2*Y*Z
	z3 := fpMul(fpAdd(a.y, a.y), a.z)

	return &G1Point{x: x3, y: y3, z: z3}
}

// G1ScalarMul computes k*P using double-and-add.
func G1ScalarMul(p *G1Point, k *big.Int) *G1Point {
	if k.Sign() == 0 || p.g1IsInfinity() {
		return G1Infinity()
	}

	// Reduce k modulo n.
	kMod := new(big.Int).Mod(k, bn254N)
	if kMod.Sign() == 0 {
		return G1Infinity()
	}

	r := G1Infinity()
	base := &G1Point{
		x: new(big.Int).Set(p.x),
		y: new(big.Int).Set(p.y),
		z: new(big.Int).Set(p.z),
	}

	for i := kMod.BitLen() - 1; i >= 0; i-- {
		r = g1Double(r)
		if kMod.Bit(i) == 1 {
			r = g1Add(r, base)
		}
	}
	return r
}

// g1Neg returns -P.
func g1Neg(p *G1Point) *G1Point {
	if p.g1IsInfinity() {
		return G1Infinity()
	}
	return &G1Point{
		x: new(big.Int).Set(p.x),
		y: fpNeg(p.y),
		z: new(big.Int).Set(p.z),
	}
}
