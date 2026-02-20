package crypto

// Comprehensive tests for BN254 field arithmetic (Fp, Fp2, Fp6, Fp12),
// G1/G2 point operations, Frobenius maps, and pairing internals.
//
// These tests focus on algebraic properties, serialization round-trips,
// and edge cases not covered by the existing test suites.

import (
	"math/big"
	"testing"
)

// ---------------------------------------------------------------------------
// Fp arithmetic
// ---------------------------------------------------------------------------

func TestBN254FpExpIdentity(t *testing.T) {
	a := big.NewInt(7)

	// a^0 = 1
	r := fpExp(a, big.NewInt(0))
	if r.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("fpExp(7, 0) = %s, want 1", r)
	}

	// a^1 = a
	r = fpExp(a, big.NewInt(1))
	if r.Cmp(a) != 0 {
		t.Errorf("fpExp(7, 1) = %s, want 7", r)
	}
}

func TestBN254FpExpFermat(t *testing.T) {
	// Fermat's little theorem: a^(p-1) = 1 mod p for non-zero a.
	a := big.NewInt(42)
	pMinus1 := new(big.Int).Sub(bn254P, big.NewInt(1))
	r := fpExp(a, pMinus1)
	if r.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("42^(p-1) mod p = %s, want 1", r)
	}
}

func TestBN254FpExpConsistency(t *testing.T) {
	a := big.NewInt(5)

	// a^2 via exp == sqr
	expResult := fpExp(a, big.NewInt(2))
	sqrResult := fpSqr(a)
	if expResult.Cmp(sqrResult) != 0 {
		t.Error("fpExp(5, 2) != fpSqr(5)")
	}

	// a^3 == mul(sqr(a), a)
	expResult = fpExp(a, big.NewInt(3))
	mulResult := fpMul(sqrResult, a)
	if expResult.Cmp(mulResult) != 0 {
		t.Error("fpExp(5, 3) != fpMul(fpSqr(5), 5)")
	}
}

func TestBN254FpInvSelf(t *testing.T) {
	a := big.NewInt(13)
	inv1 := fpInv(a)
	inv2 := fpInv(inv1)
	if inv2.Cmp(a) != 0 {
		t.Errorf("inv(inv(13)) = %s, want 13", inv2)
	}
}

func TestBN254FpNegDoubleNeg(t *testing.T) {
	a := big.NewInt(99)
	doubleNeg := fpNeg(fpNeg(a))
	if doubleNeg.Cmp(a) != 0 {
		t.Errorf("-(-a) = %s, want %s", doubleNeg, a)
	}
}

func TestBN254FpDistributive(t *testing.T) {
	a := big.NewInt(7)
	b := big.NewInt(11)
	c := big.NewInt(13)

	// a * (b + c) == a*b + a*c
	lhs := fpMul(a, fpAdd(b, c))
	rhs := fpAdd(fpMul(a, b), fpMul(a, c))
	if lhs.Cmp(rhs) != 0 {
		t.Error("Fp is not distributive")
	}
}

func TestBN254FpAssociative(t *testing.T) {
	a := big.NewInt(100)
	b := big.NewInt(200)
	c := big.NewInt(300)

	// (a + b) + c == a + (b + c)
	lhs := fpAdd(fpAdd(a, b), c)
	rhs := fpAdd(a, fpAdd(b, c))
	if lhs.Cmp(rhs) != 0 {
		t.Error("Fp addition is not associative")
	}

	// (a * b) * c == a * (b * c)
	lhs = fpMul(fpMul(a, b), c)
	rhs = fpMul(a, fpMul(b, c))
	if lhs.Cmp(rhs) != 0 {
		t.Error("Fp multiplication is not associative")
	}
}

// ---------------------------------------------------------------------------
// Fp2 arithmetic
// ---------------------------------------------------------------------------

func TestBN254Fp2MulByNonResidueCheck(t *testing.T) {
	// fp2MulByNonResidue multiplies by (9+i).
	// (9+i)(a + b*i) = (9a - b) + (a + 9b)*i
	a := &fp2{a0: big.NewInt(3), a1: big.NewInt(5)}
	r := fp2MulByNonResidue(a)

	expectedA0 := fpSub(fpMul(big.NewInt(9), big.NewInt(3)), big.NewInt(5))
	expectedA1 := fpAdd(big.NewInt(3), fpMul(big.NewInt(9), big.NewInt(5)))
	expected := &fp2{a0: expectedA0, a1: expectedA1}
	if !r.equal(expected) {
		t.Errorf("fp2MulByNonResidue mismatch: got (%s, %s), want (%s, %s)",
			r.a0, r.a1, expected.a0, expected.a1)
	}
}

func TestBN254Fp2MulScalarProperties(t *testing.T) {
	a := &fp2{a0: big.NewInt(5), a1: big.NewInt(7)}

	// Multiply by 1.
	r := fp2MulScalar(a, big.NewInt(1))
	if !r.equal(a) {
		t.Error("Fp2 * 1 should be identity")
	}

	// Multiply by 0.
	r = fp2MulScalar(a, big.NewInt(0))
	if !r.isZero() {
		t.Error("Fp2 * 0 should be zero")
	}
}

func TestBN254Fp2InvSelf(t *testing.T) {
	a := &fp2{a0: big.NewInt(17), a1: big.NewInt(23)}
	inv1 := fp2Inv(a)
	inv2 := fp2Inv(inv1)
	if !inv2.equal(a) {
		t.Error("inv(inv(a)) should equal a in Fp2")
	}
}

func TestBN254Fp2ConjMulIsNorm(t *testing.T) {
	a := &fp2{a0: big.NewInt(11), a1: big.NewInt(13)}
	conj := fp2Conj(a)
	prod := fp2Mul(a, conj)
	if prod.a1.Sign() != 0 {
		t.Error("a * conj(a) should be real in Fp2")
	}
	expectedNorm := fpAdd(fpSqr(big.NewInt(11)), fpSqr(big.NewInt(13)))
	if prod.a0.Cmp(expectedNorm) != 0 {
		t.Errorf("a * conj(a) = %s, want %s", prod.a0, expectedNorm)
	}
}

func TestBN254Fp2MulAssociativity(t *testing.T) {
	a := &fp2{a0: big.NewInt(3), a1: big.NewInt(5)}
	b := &fp2{a0: big.NewInt(7), a1: big.NewInt(11)}
	c := &fp2{a0: big.NewInt(13), a1: big.NewInt(17)}

	// (a*b)*c == a*(b*c)
	lhs := fp2Mul(fp2Mul(a, b), c)
	rhs := fp2Mul(a, fp2Mul(b, c))
	if !lhs.equal(rhs) {
		t.Error("Fp2 multiplication is not associative")
	}
}

// ---------------------------------------------------------------------------
// Fp6 arithmetic
// ---------------------------------------------------------------------------

func TestBN254Fp6MulByFp2Properties(t *testing.T) {
	a := &fp6{
		c0: &fp2{a0: big.NewInt(3), a1: big.NewInt(4)},
		c1: &fp2{a0: big.NewInt(5), a1: big.NewInt(6)},
		c2: &fp2{a0: big.NewInt(7), a1: big.NewInt(8)},
	}

	// Multiply by Fp2(1) should be identity.
	r := fp6MulByFp2(a, fp2One())
	if !r.c0.equal(a.c0) || !r.c1.equal(a.c1) || !r.c2.equal(a.c2) {
		t.Error("fp6MulByFp2 by 1 should be identity")
	}

	// Multiply by Fp2(0) should be zero.
	r = fp6MulByFp2(a, fp2Zero())
	if !r.c0.isZero() || !r.c1.isZero() || !r.c2.isZero() {
		t.Error("fp6MulByFp2 by 0 should be zero")
	}
}

func TestBN254Fp6MulByVShift(t *testing.T) {
	one := fp6One()
	vTimesOne := fp6MulByV(one)

	// v * (1 + 0*v + 0*v^2) = xi*0 + 1*v + 0*v^2 = (0, 1, 0)
	if !vTimesOne.c0.isZero() {
		t.Error("v*1 should have c0 = 0")
	}
	if !vTimesOne.c1.equal(fp2One()) {
		t.Error("v*1 should have c1 = 1")
	}
	if !vTimesOne.c2.isZero() {
		t.Error("v*1 should have c2 = 0")
	}
}

func TestBN254Fp6NegProperties(t *testing.T) {
	a := &fp6{
		c0: &fp2{a0: big.NewInt(3), a1: big.NewInt(4)},
		c1: &fp2{a0: big.NewInt(5), a1: big.NewInt(6)},
		c2: &fp2{a0: big.NewInt(7), a1: big.NewInt(8)},
	}

	neg := fp6Neg(a)
	sum := fp6Add(a, neg)
	if !sum.c0.isZero() || !sum.c1.isZero() || !sum.c2.isZero() {
		t.Error("a + (-a) should be zero in Fp6")
	}
}

func TestBN254Fp6SquaringMatchesMul(t *testing.T) {
	a := &fp6{
		c0: &fp2{a0: big.NewInt(11), a1: big.NewInt(13)},
		c1: &fp2{a0: big.NewInt(17), a1: big.NewInt(19)},
		c2: &fp2{a0: big.NewInt(23), a1: big.NewInt(29)},
	}
	sqr := fp6Sqr(a)
	mul := fp6Mul(a, a)
	if !sqr.c0.equal(mul.c0) || !sqr.c1.equal(mul.c1) || !sqr.c2.equal(mul.c2) {
		t.Error("Fp6: sqr(a) != mul(a, a)")
	}
}

func TestBN254Fp6InvSelf(t *testing.T) {
	a := &fp6{
		c0: &fp2{a0: big.NewInt(3), a1: big.NewInt(4)},
		c1: &fp2{a0: big.NewInt(5), a1: big.NewInt(6)},
		c2: &fp2{a0: big.NewInt(7), a1: big.NewInt(8)},
	}
	aInv := fp6Inv(a)
	product := fp6Mul(a, aInv)
	if !product.c0.equal(fp2One()) || !product.c1.isZero() || !product.c2.isZero() {
		t.Error("a * a^(-1) should be 1 in Fp6")
	}
}

// ---------------------------------------------------------------------------
// Fp12 arithmetic
// ---------------------------------------------------------------------------

func TestBN254Fp12ExpZero(t *testing.T) {
	f := &fp12{
		c0: &fp6{
			c0: &fp2{a0: big.NewInt(5), a1: big.NewInt(7)},
			c1: &fp2{a0: big.NewInt(11), a1: big.NewInt(13)},
			c2: &fp2{a0: big.NewInt(17), a1: big.NewInt(19)},
		},
		c1: &fp6{
			c0: &fp2{a0: big.NewInt(23), a1: big.NewInt(29)},
			c1: &fp2{a0: big.NewInt(31), a1: big.NewInt(37)},
			c2: &fp2{a0: big.NewInt(41), a1: big.NewInt(43)},
		},
	}

	// f^0 = 1
	r := fp12Exp(f, big.NewInt(0))
	if !r.isOne() {
		t.Error("f^0 should be 1 in Fp12")
	}

	// f^1 = f
	r = fp12Exp(f, big.NewInt(1))
	if !r.c0.c0.equal(f.c0.c0) ||
		!r.c0.c1.equal(f.c0.c1) ||
		!r.c0.c2.equal(f.c0.c2) ||
		!r.c1.c0.equal(f.c1.c0) ||
		!r.c1.c1.equal(f.c1.c1) ||
		!r.c1.c2.equal(f.c1.c2) {
		t.Error("f^1 should equal f in Fp12")
	}
}

func TestBN254Fp12ConjDouble(t *testing.T) {
	// conj(conj(f)) should equal f.
	f := &fp12{
		c0: &fp6{
			c0: &fp2{a0: big.NewInt(2), a1: big.NewInt(3)},
			c1: &fp2{a0: big.NewInt(5), a1: big.NewInt(7)},
			c2: &fp2{a0: big.NewInt(11), a1: big.NewInt(13)},
		},
		c1: &fp6{
			c0: &fp2{a0: big.NewInt(17), a1: big.NewInt(19)},
			c1: &fp2{a0: big.NewInt(23), a1: big.NewInt(29)},
			c2: &fp2{a0: big.NewInt(31), a1: big.NewInt(37)},
		},
	}
	cc := fp12Conj(fp12Conj(f))
	if !cc.c0.c0.equal(f.c0.c0) ||
		!cc.c0.c1.equal(f.c0.c1) ||
		!cc.c0.c2.equal(f.c0.c2) ||
		!cc.c1.c0.equal(f.c1.c0) ||
		!cc.c1.c1.equal(f.c1.c1) ||
		!cc.c1.c2.equal(f.c1.c2) {
		t.Error("conj(conj(f)) should equal f in Fp12")
	}
}

func TestBN254Fp12MulInverseNonTrivial(t *testing.T) {
	f := &fp12{
		c0: &fp6{
			c0: &fp2{a0: big.NewInt(2), a1: big.NewInt(3)},
			c1: &fp2{a0: big.NewInt(5), a1: big.NewInt(7)},
			c2: &fp2{a0: big.NewInt(11), a1: big.NewInt(13)},
		},
		c1: &fp6{
			c0: &fp2{a0: big.NewInt(17), a1: big.NewInt(19)},
			c1: &fp2{a0: big.NewInt(23), a1: big.NewInt(29)},
			c2: &fp2{a0: big.NewInt(31), a1: big.NewInt(37)},
		},
	}
	fInv := fp12Inv(f)
	product := fp12Mul(f, fInv)
	if !product.isOne() {
		t.Error("f * f^(-1) should be 1 in Fp12")
	}
}

func TestBN254Fp12SquaringMatchesMul(t *testing.T) {
	f := &fp12{
		c0: &fp6{
			c0: &fp2{a0: big.NewInt(2), a1: big.NewInt(3)},
			c1: &fp2{a0: big.NewInt(5), a1: big.NewInt(7)},
			c2: &fp2{a0: big.NewInt(11), a1: big.NewInt(13)},
		},
		c1: &fp6{
			c0: &fp2{a0: big.NewInt(17), a1: big.NewInt(19)},
			c1: &fp2{a0: big.NewInt(23), a1: big.NewInt(29)},
			c2: &fp2{a0: big.NewInt(31), a1: big.NewInt(37)},
		},
	}
	sqr := fp12Sqr(f)
	mul := fp12Mul(f, f)
	if !sqr.c0.c0.equal(mul.c0.c0) ||
		!sqr.c0.c1.equal(mul.c0.c1) ||
		!sqr.c0.c2.equal(mul.c0.c2) ||
		!sqr.c1.c0.equal(mul.c1.c0) ||
		!sqr.c1.c1.equal(mul.c1.c1) ||
		!sqr.c1.c2.equal(mul.c1.c2) {
		t.Error("fp12Sqr(f) != fp12Mul(f, f)")
	}
}

// ---------------------------------------------------------------------------
// Frobenius maps
// ---------------------------------------------------------------------------

func TestBN254FrobeniusEfficiencyOnOne(t *testing.T) {
	one := fp12One()

	// Frobenius(1) = 1 for all powers.
	frob1 := fp12FrobeniusEfficient(one)
	if !frob1.isOne() {
		t.Error("Frobenius(1) should be 1")
	}

	frob2 := fp12FrobeniusSqEfficient(one)
	if !frob2.isOne() {
		t.Error("Frobenius^2(1) should be 1")
	}

	frob3 := fp12FrobeniusCubeEfficient(one)
	if !frob3.isOne() {
		t.Error("Frobenius^3(1) should be 1")
	}
}

// ---------------------------------------------------------------------------
// G1 point operations
// ---------------------------------------------------------------------------

func TestBN254G1AddAssociativity(t *testing.T) {
	gen := G1Generator()
	twoG := g1Double(gen)
	threeG := g1Add(gen, twoG)

	lhs := g1Add(g1Add(gen, twoG), threeG)
	rhs := g1Add(gen, g1Add(twoG, threeG))

	lx, ly := lhs.g1ToAffine()
	rx, ry := rhs.g1ToAffine()
	if lx.Cmp(rx) != 0 || ly.Cmp(ry) != 0 {
		t.Error("BN254 G1 addition is not associative")
	}
}

func TestBN254G1AddCommutativityArith(t *testing.T) {
	gen := G1Generator()
	twoG := g1Double(gen)

	ab := g1Add(gen, twoG)
	ba := g1Add(twoG, gen)

	abx, aby := ab.g1ToAffine()
	bax, bay := ba.g1ToAffine()
	if abx.Cmp(bax) != 0 || aby.Cmp(bay) != 0 {
		t.Error("BN254 G1 addition is not commutative")
	}
}

func TestBN254G1ScalarMulThree(t *testing.T) {
	gen := G1Generator()

	// 3*G == G + G + G
	threeG := G1ScalarMul(gen, big.NewInt(3))
	added := g1Add(g1Add(gen, gen), gen)

	tx, ty := threeG.g1ToAffine()
	ax, ay := added.g1ToAffine()
	if tx.Cmp(ax) != 0 || ty.Cmp(ay) != 0 {
		t.Error("3*G via scalar mul does not match G+G+G")
	}
}

func TestBN254G1ScalarMulNPlusOne(t *testing.T) {
	gen := G1Generator()

	// (n+1)*G == G
	nPlusOne := new(big.Int).Add(bn254N, big.NewInt(1))
	r := G1ScalarMul(gen, nPlusOne)
	rx, ry := r.g1ToAffine()
	gx, gy := gen.g1ToAffine()
	if rx.Cmp(gx) != 0 || ry.Cmp(gy) != 0 {
		t.Error("(n+1)*G should equal G")
	}
}

func TestBN254G1AffineRoundTrip(t *testing.T) {
	gen := G1Generator()
	fiveG := G1ScalarMul(gen, big.NewInt(5))

	ax, ay := fiveG.g1ToAffine()
	if !g1IsOnCurve(ax, ay) {
		t.Fatal("5*G is not on the curve")
	}

	reconstructed := g1FromAffine(ax, ay)
	rx, ry := reconstructed.g1ToAffine()
	if ax.Cmp(rx) != 0 || ay.Cmp(ry) != 0 {
		t.Error("BN254 G1 affine round-trip failed for 5*G")
	}
}

func TestBN254G1InfinityAffineRoundTrip(t *testing.T) {
	inf := G1Infinity()
	x, y := inf.g1ToAffine()
	if x.Sign() != 0 || y.Sign() != 0 {
		t.Error("BN254 G1 infinity affine should be (0, 0)")
	}

	reconstructed := g1FromAffine(x, y)
	if !reconstructed.g1IsInfinity() {
		t.Error("reconstructed from (0,0) should be infinity")
	}
}

// ---------------------------------------------------------------------------
// G2 point operations
// ---------------------------------------------------------------------------

func TestBN254G2AddAssociativity(t *testing.T) {
	gen := G2Generator()
	twoG := g2Double(gen)
	threeG := g2Add(gen, twoG)

	lhs := g2Add(g2Add(gen, twoG), threeG)
	rhs := g2Add(gen, g2Add(twoG, threeG))

	lx, ly := lhs.g2ToAffine()
	rx, ry := rhs.g2ToAffine()
	if !lx.equal(rx) || !ly.equal(ry) {
		t.Error("BN254 G2 addition is not associative")
	}
}

func TestBN254G2AddCommutativity(t *testing.T) {
	gen := G2Generator()
	twoG := g2Double(gen)

	ab := g2Add(gen, twoG)
	ba := g2Add(twoG, gen)

	abx, aby := ab.g2ToAffine()
	bax, bay := ba.g2ToAffine()
	if !abx.equal(bax) || !aby.equal(bay) {
		t.Error("BN254 G2 addition is not commutative")
	}
}

func TestBN254G2ScalarMulThree(t *testing.T) {
	gen := G2Generator()

	threeG := g2ScalarMul(gen, big.NewInt(3))
	added := g2Add(g2Add(gen, gen), gen)

	tx, ty := threeG.g2ToAffine()
	ax, ay := added.g2ToAffine()
	if !tx.equal(ax) || !ty.equal(ay) {
		t.Error("3*G2 via scalar mul does not match G2+G2+G2")
	}
}

func TestBN254G2NegDoubleNeg(t *testing.T) {
	gen := G2Generator()
	neg := g2Neg(gen)
	doubleNeg := g2Neg(neg)

	x1, y1 := gen.g2ToAffine()
	x2, y2 := doubleNeg.g2ToAffine()
	if !x1.equal(x2) || !y1.equal(y2) {
		t.Error("BN254 G2: -(-G) should equal G")
	}
}

func TestBN254G2AffineRoundTrip(t *testing.T) {
	gen := G2Generator()
	fiveG := g2ScalarMul(gen, big.NewInt(5))

	ax, ay := fiveG.g2ToAffine()
	if !g2IsOnCurve(ax, ay) {
		t.Fatal("5*G2 is not on the twist curve")
	}

	reconstructed := g2FromAffine(ax, ay)
	rx, ry := reconstructed.g2ToAffine()
	if !ax.equal(rx) || !ay.equal(ry) {
		t.Error("BN254 G2 affine round-trip failed for 5*G2")
	}
}

func TestBN254G2InfinityAffineRoundTrip(t *testing.T) {
	inf := G2Infinity()
	x, y := inf.g2ToAffine()
	if !x.isZero() || !y.isZero() {
		t.Error("BN254 G2 infinity affine should be (0, 0)")
	}

	reconstructed := g2FromAffine(x, y)
	if !reconstructed.g2IsInfinity() {
		t.Error("BN254 G2 reconstructed from (0,0) should be infinity")
	}
}

func TestBN254G2OnCurveSubgroup(t *testing.T) {
	gen := G2Generator()
	gx, gy := gen.g2ToAffine()
	if !g2IsOnCurveSubgroup(gx, gy) {
		t.Error("G2 generator should be in subgroup")
	}

	if !g2IsOnCurveSubgroup(fp2Zero(), fp2Zero()) {
		t.Error("identity should be in subgroup")
	}
}

// ---------------------------------------------------------------------------
// Pairing
// ---------------------------------------------------------------------------

func TestBN254PairNonDegeneracy(t *testing.T) {
	g1 := G1Generator()
	g2 := G2Generator()

	result := BN254Pair(g1, g2)
	if result.isOne() {
		t.Error("e(G1, G2) should not be the identity")
	}
}

func TestBN254PairInfinityG1(t *testing.T) {
	inf := G1Infinity()
	g2 := G2Generator()

	result := BN254Pair(inf, g2)
	if !result.isOne() {
		t.Error("e(O, G2) should be 1")
	}
}

func TestBN254PairInfinityG2(t *testing.T) {
	g1 := G1Generator()
	inf := G2Infinity()

	result := BN254Pair(g1, inf)
	if !result.isOne() {
		t.Error("e(G1, O) should be 1")
	}
}

func TestBN254MultiPairingBilinearity(t *testing.T) {
	// e(a*G1, G2) * e(-a*G1, G2) = 1
	a := big.NewInt(5)
	aG1 := G1ScalarMul(G1Generator(), a)
	negAG1 := g1Neg(aG1)
	g2 := G2Generator()

	result := bn254MultiPairing(
		[]*G1Point{aG1, negAG1},
		[]*G2Point{g2, g2},
	)
	if !result {
		t.Error("e(aG1, G2) * e(-aG1, G2) should be 1")
	}
}

func TestBN254MultiPairingScalarSwap(t *testing.T) {
	// e(a*G1, b*G2) * e(-b*G1, a*G2) = 1
	a := big.NewInt(3)
	b := big.NewInt(7)

	aG1 := G1ScalarMul(G1Generator(), a)
	bG2 := g2ScalarMul(G2Generator(), b)
	negBG1 := g1Neg(G1ScalarMul(G1Generator(), b))
	aG2 := g2ScalarMul(G2Generator(), a)

	result := bn254MultiPairing(
		[]*G1Point{aG1, negBG1},
		[]*G2Point{bG2, aG2},
	)
	if !result {
		t.Error("e(aG1, bG2) * e(-bG1, aG2) should be 1")
	}
}

func TestBN254MultiPairingMismatchLength(t *testing.T) {
	// Mismatched lengths should return false.
	result := bn254MultiPairing(
		[]*G1Point{G1Generator()},
		[]*G2Point{G2Generator(), G2Generator()},
	)
	if result {
		t.Error("mismatched pairing lengths should return false")
	}
}

func TestBN254MultiPairingLinearity(t *testing.T) {
	// e(a*G1, G2) * e(b*G1, G2) * e(-(a+b)*G1, G2) = 1
	a := big.NewInt(4)
	b := big.NewInt(9)
	aPlusB := big.NewInt(13)

	aG1 := G1ScalarMul(G1Generator(), a)
	bG1 := G1ScalarMul(G1Generator(), b)
	negAbG1 := g1Neg(G1ScalarMul(G1Generator(), aPlusB))
	g2 := G2Generator()

	result := bn254MultiPairing(
		[]*G1Point{aG1, bG1, negAbG1},
		[]*G2Point{g2, g2, g2},
	)
	if !result {
		t.Error("e(aG1,G2)*e(bG1,G2)*e(-(a+b)G1,G2) should be 1")
	}
}

// ---------------------------------------------------------------------------
// BN254 impl helpers
// ---------------------------------------------------------------------------

func TestBigFromStrValid(t *testing.T) {
	v := bigFromStr("12345")
	if v.Cmp(big.NewInt(12345)) != 0 {
		t.Errorf("bigFromStr(\"12345\") = %s, want 12345", v)
	}
}

func TestBigFromStrPanicsOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("bigFromStr with invalid input should panic")
		}
	}()
	bigFromStr("not_a_number")
}

func TestFrobeniusEndomorphismOnGen(t *testing.T) {
	gen := G2Generator()
	gx, gy := gen.g2ToAffine()

	// The Frobenius endomorphism should produce a valid Fp2 result.
	qx, qy := frobeniusEndomorphism(gx, gy)
	if qx.isZero() && qy.isZero() {
		t.Error("Frobenius of G2 generator should not be zero")
	}
}
