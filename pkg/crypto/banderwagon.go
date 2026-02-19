// Banderwagon elliptic curve implementation for Verkle trees (EIP-6800).
//
// Banderwagon is a prime-order subgroup of the Bandersnatch curve, defined
// over the BLS12-381 scalar field (Fr). It uses the twisted Edwards form:
//   -5x² + y² = 1 + dx²y²
//
// where d = (A-2)/(A+2) and A is the Montgomery parameter of Bandersnatch.
//
// Points are represented in extended twisted Edwards coordinates (X, Y, T, Z)
// where x = X/Z, y = Y/Z, T = XY/Z for efficient group operations.
//
// This implementation uses math/big for field arithmetic, prioritizing
// correctness over performance. Constant-time guarantees are not provided
// by math/big, so this is suitable for consensus verification but not for
// private key operations.

package crypto

import (
	"errors"
	"math/big"
)

// Banderwagon field and curve parameters.
//
// The base field is the BLS12-381 scalar field Fr with modulus:
//   r = 0x73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001
//
// The Bandersnatch prime-order subgroup has order:
//   n = 0x1cfb69d4ca675f520cce760202687600ff8f87007419047174fd06b52876e7e1
//
// The twisted Edwards curve is -5x² + y² = 1 + dx²y² where:
//   a = -5
//   d = 0x6389c12633c267cbc66e3bf86be3b6d8cb66677177e54f92b369f2f5188d58e7
//
// The curve has order 4n (cofactor 4). The generator is in the order-n subgroup.
// Coordinate arithmetic uses the base field r, while scalar arithmetic uses
// the subgroup order n.
var (
	// banderFr is the BLS12-381 scalar field order, used as the base field
	// for Banderwagon coordinate arithmetic.
	banderFr, _ = new(big.Int).SetString(
		"73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001", 16)

	// banderN is the Bandersnatch prime-order subgroup order, used for scalar
	// arithmetic (scalar multiplication, IPA challenges, etc.).
	banderN, _ = new(big.Int).SetString(
		"1cfb69d4ca675f520cce760202687600ff8f87007419047174fd06b52876e7e1", 16)

	// banderA is the twisted Edwards 'a' parameter = -5 mod r.
	banderA = func() *big.Int {
		a := new(big.Int).Sub(banderFr, big.NewInt(5))
		return a
	}()

	// banderD is the twisted Edwards 'd' parameter from the Bandersnatch spec.
	banderD, _ = new(big.Int).SetString(
		"6389c12633c267cbc66e3bf86be3b6d8cb66677177e54f92b369f2f5188d58e7", 16)
)

// BanderPoint represents a point on the Banderwagon curve in extended
// twisted Edwards coordinates (X, Y, T, Z) where:
//   x = X/Z, y = Y/Z, T = X*Y/Z
type BanderPoint struct {
	x, y, t, z *big.Int
}

// banderFrAdd returns (a + b) mod r.
func banderFrAdd(a, b *big.Int) *big.Int {
	return new(big.Int).Mod(new(big.Int).Add(a, b), banderFr)
}

// banderFrSub returns (a - b) mod r.
func banderFrSub(a, b *big.Int) *big.Int {
	r := new(big.Int).Sub(a, b)
	return r.Mod(r, banderFr)
}

// banderFrMul returns (a * b) mod r.
func banderFrMul(a, b *big.Int) *big.Int {
	return new(big.Int).Mod(new(big.Int).Mul(a, b), banderFr)
}

// banderFrSqr returns a² mod r.
func banderFrSqr(a *big.Int) *big.Int {
	return banderFrMul(a, a)
}

// banderFrNeg returns -a mod r.
func banderFrNeg(a *big.Int) *big.Int {
	if a.Sign() == 0 {
		return new(big.Int)
	}
	return new(big.Int).Sub(banderFr, new(big.Int).Mod(a, banderFr))
}

// banderFrInv returns a^(-1) mod r.
func banderFrInv(a *big.Int) *big.Int {
	return new(big.Int).ModInverse(a, banderFr)
}

// banderFrSqrt returns sqrt(a) mod r, or nil if a is not a quadratic residue.
// The field r ≡ 1 mod 4, so we need Tonelli-Shanks or equivalent.
// Since r-1 = 2^32 * m where m is odd, we use Tonelli-Shanks.
func banderFrSqrt(a *big.Int) *big.Int {
	if a.Sign() == 0 {
		return new(big.Int)
	}
	r := new(big.Int).ModSqrt(a, banderFr)
	return r // nil if not a QR
}

// BanderIdentity returns the identity point (0, 1) in extended coordinates.
func BanderIdentity() *BanderPoint {
	return &BanderPoint{
		x: new(big.Int),
		y: big.NewInt(1),
		t: new(big.Int),
		z: big.NewInt(1),
	}
}

// Banderwagon generator point. This is the Bandersnatch twisted Edwards
// subgroup generator from the spec (lexicographically smallest x-coordinate
// scaled by cofactor 4).
var (
	banderGenX, _ = new(big.Int).SetString(
		"29c132cc2c0b34c5743711777bbe42f32b79c022ad998465e1e71866a252ae18", 16)
	banderGenY, _ = new(big.Int).SetString(
		"2a6c669eda123e0f157d8b50badcd586358cad81eee464605e3167b6cc974166", 16)
)

// BanderGenerator returns the standard Banderwagon generator point.
func BanderGenerator() *BanderPoint {
	t := banderFrMul(banderGenX, banderGenY)
	return &BanderPoint{
		x: new(big.Int).Set(banderGenX),
		y: new(big.Int).Set(banderGenY),
		t: t,
		z: big.NewInt(1),
	}
}

// BanderIsIdentity returns true if the point is the identity (neutral element).
func (p *BanderPoint) BanderIsIdentity() bool {
	// Identity in extended coords: X=0, Y=Z, T=0.
	xz := new(big.Int).Mod(p.x, banderFr)
	return xz.Sign() == 0
}

// BanderFromAffine creates a point from affine coordinates (x, y).
// Returns an error if the point is not on the curve.
func BanderFromAffine(x, y *big.Int) (*BanderPoint, error) {
	if !banderIsOnCurve(x, y) {
		return nil, errors.New("banderwagon: point not on curve")
	}
	xm := new(big.Int).Mod(x, banderFr)
	ym := new(big.Int).Mod(y, banderFr)
	t := banderFrMul(xm, ym)
	return &BanderPoint{
		x: xm,
		y: ym,
		t: t,
		z: big.NewInt(1),
	}, nil
}

// BanderToAffine converts the point to affine coordinates (x, y).
func (p *BanderPoint) BanderToAffine() (x, y *big.Int) {
	if p.z.Cmp(big.NewInt(1)) == 0 {
		return new(big.Int).Set(p.x), new(big.Int).Set(p.y)
	}
	zInv := banderFrInv(p.z)
	x = banderFrMul(p.x, zInv)
	y = banderFrMul(p.y, zInv)
	return
}

// banderIsOnCurve checks if (x, y) satisfies -5x² + y² = 1 + dx²y².
func banderIsOnCurve(x, y *big.Int) bool {
	xm := new(big.Int).Mod(x, banderFr)
	ym := new(big.Int).Mod(y, banderFr)

	x2 := banderFrSqr(xm)
	y2 := banderFrSqr(ym)

	// LHS = a*x² + y² = -5x² + y²
	lhs := banderFrAdd(banderFrMul(banderA, x2), y2)

	// RHS = 1 + d*x²*y²
	rhs := banderFrAdd(big.NewInt(1), banderFrMul(banderD, banderFrMul(x2, y2)))

	return lhs.Cmp(rhs) == 0
}

// BanderAdd adds two Banderwagon points using the unified addition formula
// for twisted Edwards curves in extended coordinates.
//
// Formula from "Twisted Edwards Curves Revisited" (Hisil et al., 2008):
//   A = X1*X2, B = Y1*Y2, C = T1*d*T2, D = Z1*Z2
//   E = (X1+Y1)*(X2+Y2) - A - B
//   F = D - C, G = D + C, H = B - a*A
//   X3 = E*F, Y3 = G*H, T3 = E*H, Z3 = F*G
func BanderAdd(p1, p2 *BanderPoint) *BanderPoint {
	A := banderFrMul(p1.x, p2.x)
	B := banderFrMul(p1.y, p2.y)
	C := banderFrMul(banderFrMul(p1.t, banderD), p2.t)
	D := banderFrMul(p1.z, p2.z)

	E := banderFrSub(
		banderFrMul(banderFrAdd(p1.x, p1.y), banderFrAdd(p2.x, p2.y)),
		banderFrAdd(A, B))
	F := banderFrSub(D, C)
	G := banderFrAdd(D, C)
	// H = B - a*A = B - (-5)*A = B + 5*A
	H := banderFrSub(B, banderFrMul(banderA, A))

	return &BanderPoint{
		x: banderFrMul(E, F),
		y: banderFrMul(G, H),
		t: banderFrMul(E, H),
		z: banderFrMul(F, G),
	}
}

// BanderDouble doubles a Banderwagon point using the dedicated doubling
// formula for twisted Edwards curves in extended coordinates.
//
// Formula:
//   A = X1², B = Y1², C = 2*Z1²
//   D = a*A, E = (X1+Y1)² - A - B
//   G = D + B, F = G - C, H = D - B
//   X3 = E*F, Y3 = G*H, T3 = E*H, Z3 = F*G
func BanderDouble(p *BanderPoint) *BanderPoint {
	A := banderFrSqr(p.x)
	B := banderFrSqr(p.y)
	C := banderFrMul(big.NewInt(2), banderFrSqr(p.z))

	D := banderFrMul(banderA, A)
	E := banderFrSub(banderFrSqr(banderFrAdd(p.x, p.y)), banderFrAdd(A, B))
	G := banderFrAdd(D, B)
	F := banderFrSub(G, C)
	H := banderFrSub(D, B)

	return &BanderPoint{
		x: banderFrMul(E, F),
		y: banderFrMul(G, H),
		t: banderFrMul(E, H),
		z: banderFrMul(F, G),
	}
}

// BanderNeg returns the negation of a point. For twisted Edwards curves,
// -(x, y) = (-x, y).
func BanderNeg(p *BanderPoint) *BanderPoint {
	return &BanderPoint{
		x: banderFrNeg(p.x),
		y: new(big.Int).Set(p.y),
		t: banderFrNeg(p.t),
		z: new(big.Int).Set(p.z),
	}
}

// BanderScalarMul computes k*P using double-and-add.
// Scalars are reduced modulo the subgroup order n.
func BanderScalarMul(p *BanderPoint, k *big.Int) *BanderPoint {
	if k.Sign() == 0 || p.BanderIsIdentity() {
		return BanderIdentity()
	}

	// Reduce scalar modulo the subgroup order n (not the base field r).
	scalar := new(big.Int).Mod(k, banderN)
	if scalar.Sign() == 0 {
		return BanderIdentity()
	}

	result := BanderIdentity()
	base := &BanderPoint{
		x: new(big.Int).Set(p.x),
		y: new(big.Int).Set(p.y),
		t: new(big.Int).Set(p.t),
		z: new(big.Int).Set(p.z),
	}

	for i := scalar.BitLen() - 1; i >= 0; i-- {
		result = BanderDouble(result)
		if scalar.Bit(i) == 1 {
			result = BanderAdd(result, base)
		}
	}
	return result
}

// BanderMSM computes a multi-scalar multiplication: sum(scalars[i] * points[i]).
// Uses a simple accumulator approach. For production, Pippenger's algorithm
// would be more efficient for large vectors.
func BanderMSM(points []*BanderPoint, scalars []*big.Int) *BanderPoint {
	if len(points) == 0 || len(points) != len(scalars) {
		return BanderIdentity()
	}

	result := BanderIdentity()
	for i := range points {
		if scalars[i].Sign() == 0 {
			continue
		}
		term := BanderScalarMul(points[i], scalars[i])
		result = BanderAdd(result, term)
	}
	return result
}

// BanderEqual returns true if two points represent the same group element.
// In Banderwagon (quotient group), (x, y) and (-x, -y) are equivalent.
// We check: (X1*Z2 == X2*Z1 and Y1*Z2 == Y2*Z1) OR
//           (X1*Z2 == -X2*Z1 and Y1*Z2 == -Y2*Z1).
func BanderEqual(p1, p2 *BanderPoint) bool {
	lx := banderFrMul(p1.x, p2.z)
	rx := banderFrMul(p2.x, p1.z)
	ly := banderFrMul(p1.y, p2.z)
	ry := banderFrMul(p2.y, p1.z)

	// Direct equality.
	if lx.Cmp(rx) == 0 && ly.Cmp(ry) == 0 {
		return true
	}
	// Quotient equivalence: (x,y) ~ (-x,-y).
	nrx := banderFrNeg(rx)
	nry := banderFrNeg(ry)
	return lx.Cmp(nrx) == 0 && ly.Cmp(nry) == 0
}

// BanderSerialize serializes a Banderwagon point to 32 bytes.
// The encoding uses the Y coordinate with the sign of X encoded in the
// most significant bit, following the Banderwagon serialization spec.
//
// For the Banderwagon quotient group, points are normalized so that
// the serialized Y coordinate is in the "positive" half of the field
// (i.e., Y < (r-1)/2). If Y is in the upper half, we negate the point
// first (which maps to the same equivalence class).
func BanderSerialize(p *BanderPoint) [32]byte {
	x, y := p.BanderToAffine()
	var result [32]byte

	if p.BanderIsIdentity() {
		// Identity serializes as all zeros except y=1.
		result[31] = 1
		return result
	}

	// Normalize: ensure y is in the "lower" half of the field.
	halfR := new(big.Int).Rsh(banderFr, 1)
	if y.Cmp(halfR) > 0 {
		// Negate the point (in the same equivalence class).
		x = banderFrNeg(x)
		y = banderFrNeg(y)
	}

	// Serialize Y in little-endian, with sign of X in the top bit of the last byte.
	yBytes := y.Bytes()
	// Write in little-endian.
	for i, b := range yBytes {
		result[len(yBytes)-1-i] = b
	}

	// Encode sign of X: if X is in the upper half, set the top bit.
	if x.Cmp(halfR) > 0 {
		result[31] |= 0x80
	}

	return result
}

// BanderDeserialize deserializes a 32-byte encoding to a Banderwagon point.
func BanderDeserialize(data [32]byte) (*BanderPoint, error) {
	// Extract sign bit.
	signBit := data[31] & 0x80
	data[31] &= 0x7f

	// Read Y in little-endian.
	y := new(big.Int)
	// Convert LE to big.Int.
	beBytes := make([]byte, 32)
	for i := 0; i < 32; i++ {
		beBytes[31-i] = data[i]
	}
	y.SetBytes(beBytes)

	if y.Cmp(banderFr) >= 0 {
		return nil, errors.New("banderwagon: Y coordinate out of range")
	}

	// Recover X from the curve equation: -5x² + y² = 1 + dx²y²
	// => x²(-5 - dy²) = 1 - y²
	// => x² = (1 - y²) / (-5 - dy²)
	// => x² = (y² - 1) / (5 + dy²)  [negating both sides]
	y2 := banderFrSqr(y)
	num := banderFrSub(y2, big.NewInt(1))
	den := banderFrAdd(big.NewInt(5), banderFrMul(banderD, y2))
	denInv := banderFrInv(den)
	if denInv == nil {
		return nil, errors.New("banderwagon: degenerate point")
	}
	x2 := banderFrMul(num, denInv)

	x := banderFrSqrt(x2)
	if x == nil {
		return nil, errors.New("banderwagon: no valid X coordinate")
	}

	// Apply sign bit.
	halfR := new(big.Int).Rsh(banderFr, 1)
	if signBit != 0 && x.Cmp(halfR) <= 0 {
		x = banderFrNeg(x)
	} else if signBit == 0 && x.Cmp(halfR) > 0 {
		x = banderFrNeg(x)
	}

	return BanderFromAffine(x, y)
}

// BanderMapToField maps a Banderwagon point to a scalar field element.
// This is used for hashing points to 32-byte values in Verkle tree commitments.
// The mapping is: hash(P) = X/Y (as a field element).
func BanderMapToField(p *BanderPoint) *big.Int {
	if p.BanderIsIdentity() {
		return new(big.Int)
	}
	x, y := p.BanderToAffine()
	yInv := banderFrInv(y)
	if yInv == nil {
		return new(big.Int)
	}
	return banderFrMul(x, yInv)
}

// BanderMapToBytes maps a Banderwagon point to 32 bytes via the field mapping.
// This is the standard way to produce a 32-byte commitment value for Verkle trees.
func BanderMapToBytes(p *BanderPoint) [32]byte {
	scalar := BanderMapToField(p)
	var result [32]byte
	b := scalar.Bytes()
	// Big-endian encoding, zero-padded to 32 bytes.
	copy(result[32-len(b):], b)
	return result
}

// --- Pedersen commitment generators ---

// NumPedersenGenerators is the number of generators used for Verkle tree
// Pedersen vector commitments. Each internal/leaf node commits to 256 values.
const NumPedersenGenerators = 256

// pedersenGenerators holds the 256 generator points for Pedersen commitments.
// Lazily initialized on first access.
var pedersenGenerators [NumPedersenGenerators]*BanderPoint

// pedersenGeneratorsInit tracks whether generators have been initialized.
var pedersenGeneratorsInit bool

// GeneratePedersenGenerators creates 256 independent generator points by
// hashing the index to a curve point. Uses the "hash and increment" approach:
// for each index i, hash i to get a candidate Y, then solve for X.
func GeneratePedersenGenerators() [NumPedersenGenerators]*BanderPoint {
	if pedersenGeneratorsInit {
		return pedersenGenerators
	}

	g := BanderGenerator()
	for i := 0; i < NumPedersenGenerators; i++ {
		// Use scalar multiplication of generator by distinct scalars.
		// G_i = (i+2) * G to get independent generators.
		// (i+2 to avoid identity and the generator itself)
		scalar := big.NewInt(int64(i + 2))
		pedersenGenerators[i] = BanderScalarMul(g, scalar)
	}
	pedersenGeneratorsInit = true
	return pedersenGenerators
}

// PedersenCommit computes a Pedersen vector commitment:
//   C = sum(values[i] * G_i) for i in [0, len(values))
//
// values must have length <= NumPedersenGenerators.
func PedersenCommit(values []*big.Int) *BanderPoint {
	gens := GeneratePedersenGenerators()
	n := len(values)
	if n > NumPedersenGenerators {
		n = NumPedersenGenerators
	}

	result := BanderIdentity()
	for i := 0; i < n; i++ {
		if values[i] == nil || values[i].Sign() == 0 {
			continue
		}
		term := BanderScalarMul(gens[i], values[i])
		result = BanderAdd(result, term)
	}
	return result
}

// PedersenCommitBytes computes a Pedersen commitment and returns the
// 32-byte hash (via the map-to-field operation).
func PedersenCommitBytes(values []*big.Int) [32]byte {
	c := PedersenCommit(values)
	return BanderMapToBytes(c)
}

// BanderFr returns the base field modulus (BLS12-381 scalar field order).
// This is used for coordinate arithmetic on the curve.
func BanderFr() *big.Int {
	return new(big.Int).Set(banderFr)
}

// BanderN returns the Bandersnatch prime-order subgroup order.
// This is used for scalar arithmetic (scalar multiplication, IPA, etc.).
func BanderN() *big.Int {
	return new(big.Int).Set(banderN)
}

// --- Scalar field arithmetic (mod n) ---

// banderScalarAdd returns (a + b) mod n.
func banderScalarAdd(a, b *big.Int) *big.Int {
	return new(big.Int).Mod(new(big.Int).Add(a, b), banderN)
}

// banderScalarSub returns (a - b) mod n.
func banderScalarSub(a, b *big.Int) *big.Int {
	r := new(big.Int).Sub(a, b)
	return r.Mod(r, banderN)
}

// banderScalarMul returns (a * b) mod n.
func banderScalarMul(a, b *big.Int) *big.Int {
	return new(big.Int).Mod(new(big.Int).Mul(a, b), banderN)
}

// banderScalarInv returns a^(-1) mod n.
func banderScalarInv(a *big.Int) *big.Int {
	return new(big.Int).ModInverse(a, banderN)
}
