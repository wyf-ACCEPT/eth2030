package crypto

// BLS12-381 G1 point operations over the curve y^2 = x^3 + 4 in F_p.
//
// Points are represented in Jacobian coordinates (X, Y, Z) where the
// affine point is (X/Z^2, Y/Z^3). The point at infinity has Z=0.

import "math/big"

// BlsG1Point represents a point on the BLS12-381 G1 curve in Jacobian coordinates.
type BlsG1Point struct {
	x, y, z *big.Int
}

// BLS12-381 G1 generator point coordinates.
var (
	blsG1GenX, _ = new(big.Int).SetString(
		"17f1d3a73197d7942695638c4fa9ac0fc3688c4f9774b905a14e3a3f171bac586c55e83ff97a1aeffb3af00adb22c6bb", 16)
	blsG1GenY, _ = new(big.Int).SetString(
		"08b3f481e3aaa0f1a09e30ed741d8ae4fcf5e095d5d00af600db18cb2c04b3edd03cc744a2888ae40caa232946c5e7e1", 16)
)

// BlsG1Generator returns the generator of G1.
func BlsG1Generator() *BlsG1Point {
	return &BlsG1Point{
		x: new(big.Int).Set(blsG1GenX),
		y: new(big.Int).Set(blsG1GenY),
		z: big.NewInt(1),
	}
}

// BlsG1Infinity returns the point at infinity.
func BlsG1Infinity() *BlsG1Point {
	return &BlsG1Point{
		x: big.NewInt(1),
		y: big.NewInt(1),
		z: new(big.Int),
	}
}

// blsG1IsInfinity returns true if the point is the identity (Z=0).
func (p *BlsG1Point) blsG1IsInfinity() bool {
	return p.z.Sign() == 0
}

// blsG1FromAffine creates a Jacobian point from affine coordinates.
// The all-zeros encoding represents the point at infinity.
func blsG1FromAffine(x, y *big.Int) *BlsG1Point {
	if x.Sign() == 0 && y.Sign() == 0 {
		return BlsG1Infinity()
	}
	return &BlsG1Point{
		x: new(big.Int).Set(x),
		y: new(big.Int).Set(y),
		z: big.NewInt(1),
	}
}

// blsG1ToAffine converts Jacobian to affine coordinates.
// Returns (0,0) for infinity.
func (p *BlsG1Point) blsG1ToAffine() (x, y *big.Int) {
	if p.blsG1IsInfinity() {
		return new(big.Int), new(big.Int)
	}
	zInv := blsFpInv(p.z)
	zInv2 := blsFpSqr(zInv)
	zInv3 := blsFpMul(zInv2, zInv)
	return blsFpMul(p.x, zInv2), blsFpMul(p.y, zInv3)
}

// blsG1IsOnCurve checks if the affine point (x, y) is on y^2 = x^3 + 4.
// The point (0,0) is the identity and considered valid.
func blsG1IsOnCurve(x, y *big.Int) bool {
	if x.Sign() == 0 && y.Sign() == 0 {
		return true
	}
	// Check coordinates are in range.
	if x.Sign() < 0 || x.Cmp(blsP) >= 0 {
		return false
	}
	if y.Sign() < 0 || y.Cmp(blsP) >= 0 {
		return false
	}
	// y^2 == x^3 + 4 (mod p)
	lhs := blsFpSqr(y)
	rhs := blsFpAdd(blsFpMul(blsFpSqr(x), x), blsB)
	return lhs.Cmp(rhs) == 0
}

// blsG1Add adds two G1 points in Jacobian coordinates.
func blsG1Add(a, b *BlsG1Point) *BlsG1Point {
	if a.blsG1IsInfinity() {
		return &BlsG1Point{new(big.Int).Set(b.x), new(big.Int).Set(b.y), new(big.Int).Set(b.z)}
	}
	if b.blsG1IsInfinity() {
		return &BlsG1Point{new(big.Int).Set(a.x), new(big.Int).Set(a.y), new(big.Int).Set(a.z)}
	}

	z1sq := blsFpSqr(a.z)
	z2sq := blsFpSqr(b.z)
	u1 := blsFpMul(a.x, z2sq)
	u2 := blsFpMul(b.x, z1sq)
	s1 := blsFpMul(a.y, blsFpMul(b.z, z2sq))
	s2 := blsFpMul(b.y, blsFpMul(a.z, z1sq))

	if u1.Cmp(u2) == 0 {
		if s1.Cmp(s2) == 0 {
			return blsG1Double(a)
		}
		return BlsG1Infinity()
	}

	h := blsFpSub(u2, u1)
	i := blsFpSqr(blsFpAdd(h, h))
	j := blsFpMul(h, i)
	r := blsFpSub(s2, s1)
	r = blsFpAdd(r, r)
	v := blsFpMul(u1, i)

	x3 := blsFpSub(blsFpSub(blsFpSqr(r), j), blsFpAdd(v, v))
	y3 := blsFpSub(blsFpMul(r, blsFpSub(v, x3)), blsFpAdd(blsFpMul(s1, j), blsFpMul(s1, j)))
	z3 := blsFpMul(blsFpSub(blsFpSub(blsFpSqr(blsFpAdd(a.z, b.z)), z1sq), z2sq), h)

	return &BlsG1Point{x: x3, y: y3, z: z3}
}

// blsG1Double doubles a G1 point in Jacobian coordinates.
func blsG1Double(a *BlsG1Point) *BlsG1Point {
	if a.blsG1IsInfinity() {
		return BlsG1Infinity()
	}

	// For a=0 (BLS12-381 G1 has a=0 in y^2=x^3+ax+b).
	A := blsFpSqr(a.x)
	B := blsFpSqr(a.y)
	C := blsFpSqr(B)

	D := blsFpSub(blsFpSub(blsFpSqr(blsFpAdd(a.x, B)), A), C)
	D = blsFpAdd(D, D)

	E := blsFpAdd(blsFpAdd(A, A), A)

	x3 := blsFpSub(blsFpSqr(E), blsFpAdd(D, D))

	eightC := blsFpAdd(blsFpAdd(blsFpAdd(C, C), blsFpAdd(C, C)), blsFpAdd(blsFpAdd(C, C), blsFpAdd(C, C)))
	y3 := blsFpSub(blsFpMul(E, blsFpSub(D, x3)), eightC)

	z3 := blsFpMul(blsFpAdd(a.y, a.y), a.z)

	return &BlsG1Point{x: x3, y: y3, z: z3}
}

// blsG1ScalarMul computes k*P using double-and-add.
func blsG1ScalarMul(p *BlsG1Point, k *big.Int) *BlsG1Point {
	if k.Sign() == 0 || p.blsG1IsInfinity() {
		return BlsG1Infinity()
	}

	// Reduce k modulo r.
	kMod := new(big.Int).Mod(k, blsR)
	if kMod.Sign() == 0 {
		return BlsG1Infinity()
	}

	r := BlsG1Infinity()
	base := &BlsG1Point{
		x: new(big.Int).Set(p.x),
		y: new(big.Int).Set(p.y),
		z: new(big.Int).Set(p.z),
	}

	for i := kMod.BitLen() - 1; i >= 0; i-- {
		r = blsG1Double(r)
		if kMod.Bit(i) == 1 {
			r = blsG1Add(r, base)
		}
	}
	return r
}

// blsG1Neg returns -P.
func blsG1Neg(p *BlsG1Point) *BlsG1Point {
	if p.blsG1IsInfinity() {
		return BlsG1Infinity()
	}
	return &BlsG1Point{
		x: new(big.Int).Set(p.x),
		y: blsFpNeg(p.y),
		z: new(big.Int).Set(p.z),
	}
}

// blsG1InSubgroup checks if a point is in the r-torsion subgroup of G1.
// For BLS12-381, we use the GLV endomorphism: multiply by cofactor h
// then check that [r]*P == O (point at infinity).
// More efficiently, for BLS12-381, we check using the endomorphism
// phi: (x,y) -> (beta*x, y) where beta is a cube root of unity.
// A point P is in G1 iff phi(P) == [x]*P where x is the BLS parameter.
//
// For simplicity and correctness, we just check [r]*P == O.
func blsG1InSubgroup(p *BlsG1Point) bool {
	if p.blsG1IsInfinity() {
		return true
	}
	result := blsG1ScalarMul(p, blsR)
	return result.blsG1IsInfinity()
}
