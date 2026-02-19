package crypto

import (
	"math/big"
	"testing"
)

func TestBanderFrModulus(t *testing.T) {
	// Verify the BLS12-381 scalar field modulus is correct.
	expected, _ := new(big.Int).SetString(
		"73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001", 16)
	if banderFr.Cmp(expected) != 0 {
		t.Fatalf("banderFr mismatch: got %s, want %s", banderFr.Text(16), expected.Text(16))
	}
}

func TestBanderFieldArithmetic(t *testing.T) {
	a := big.NewInt(7)
	b := big.NewInt(11)

	// Addition
	sum := banderFrAdd(a, b)
	if sum.Cmp(big.NewInt(18)) != 0 {
		t.Errorf("7 + 11 = %s, want 18", sum.Text(10))
	}

	// Subtraction
	diff := banderFrSub(b, a)
	if diff.Cmp(big.NewInt(4)) != 0 {
		t.Errorf("11 - 7 = %s, want 4", diff.Text(10))
	}

	// Subtraction wrapping
	diff2 := banderFrSub(a, b)
	expected := banderFrAdd(banderFr, new(big.Int).Sub(a, b))
	if diff2.Cmp(expected) != 0 {
		t.Errorf("7 - 11 mod r mismatch")
	}

	// Multiplication
	prod := banderFrMul(a, b)
	if prod.Cmp(big.NewInt(77)) != 0 {
		t.Errorf("7 * 11 = %s, want 77", prod.Text(10))
	}

	// Inverse
	inv := banderFrInv(a)
	check := banderFrMul(a, inv)
	if check.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("7 * 7^{-1} = %s, want 1", check.Text(10))
	}

	// Negation
	neg := banderFrNeg(a)
	sum2 := banderFrAdd(a, neg)
	if sum2.Sign() != 0 {
		t.Errorf("7 + (-7) = %s, want 0", sum2.Text(10))
	}
}

func TestBanderIdentity(t *testing.T) {
	id := BanderIdentity()
	if !id.BanderIsIdentity() {
		t.Error("identity point should be identity")
	}
}

func TestBanderGeneratorOnCurve(t *testing.T) {
	g := BanderGenerator()
	x, y := g.BanderToAffine()
	if !banderIsOnCurve(x, y) {
		t.Fatal("generator point is not on the curve")
	}
}

func TestBanderAddIdentity(t *testing.T) {
	g := BanderGenerator()
	id := BanderIdentity()

	// G + O = G
	result := BanderAdd(g, id)
	if !BanderEqual(result, g) {
		t.Error("G + identity should equal G")
	}

	// O + G = G
	result2 := BanderAdd(id, g)
	if !BanderEqual(result2, g) {
		t.Error("identity + G should equal G")
	}
}

func TestBanderDoubleConsistency(t *testing.T) {
	g := BanderGenerator()

	// 2*G via doubling
	doubled := BanderDouble(g)

	// 2*G via addition
	added := BanderAdd(g, g)

	if !BanderEqual(doubled, added) {
		t.Error("double(G) != G + G")
	}
}

func TestBanderScalarMulBasic(t *testing.T) {
	g := BanderGenerator()

	// 0 * G = O
	result := BanderScalarMul(g, big.NewInt(0))
	if !result.BanderIsIdentity() {
		t.Error("0 * G should be identity")
	}

	// 1 * G = G
	result = BanderScalarMul(g, big.NewInt(1))
	if !BanderEqual(result, g) {
		t.Error("1 * G should equal G")
	}

	// 2 * G = G + G
	result2 := BanderScalarMul(g, big.NewInt(2))
	expected := BanderAdd(g, g)
	if !BanderEqual(result2, expected) {
		t.Error("2 * G should equal G + G")
	}

	// 3 * G = G + G + G
	result3 := BanderScalarMul(g, big.NewInt(3))
	expected3 := BanderAdd(expected, g)
	if !BanderEqual(result3, expected3) {
		t.Error("3 * G should equal G + G + G")
	}
}

func TestBanderScalarMulOrder(t *testing.T) {
	g := BanderGenerator()

	// n * G = O (the subgroup order should give identity).
	result := BanderScalarMul(g, banderN)
	if !result.BanderIsIdentity() {
		t.Error("n * G should be identity")
	}

	// (n-1)*G + G = O (verify without the mod-n shortcut).
	nm1 := new(big.Int).Sub(banderN, big.NewInt(1))
	nm1G := BanderScalarMul(g, nm1)
	sum := BanderAdd(nm1G, g)
	if !sum.BanderIsIdentity() {
		t.Error("(n-1)*G + G should be identity")
	}

	// (n-1)*G should equal -G.
	negG := BanderNeg(g)
	if !BanderEqual(nm1G, negG) {
		t.Error("(n-1)*G should equal -G")
	}
}

func TestBanderNeg(t *testing.T) {
	g := BanderGenerator()
	neg := BanderNeg(g)

	// G + (-G) = O
	result := BanderAdd(g, neg)
	if !result.BanderIsIdentity() {
		t.Error("G + (-G) should be identity")
	}
}

func TestBanderScalarMulDistributive(t *testing.T) {
	g := BanderGenerator()
	a := big.NewInt(17)
	b := big.NewInt(23)

	// (a+b)*G = a*G + b*G
	aG := BanderScalarMul(g, a)
	bG := BanderScalarMul(g, b)
	sumGB := BanderAdd(aG, bG)

	abSum := new(big.Int).Add(a, b)
	direct := BanderScalarMul(g, abSum)

	if !BanderEqual(sumGB, direct) {
		t.Error("(a+b)*G != a*G + b*G")
	}
}

func TestBanderMSM(t *testing.T) {
	g := BanderGenerator()
	g2 := BanderScalarMul(g, big.NewInt(2))
	g3 := BanderScalarMul(g, big.NewInt(3))

	points := []*BanderPoint{g, g2, g3}
	scalars := []*big.Int{big.NewInt(5), big.NewInt(7), big.NewInt(11)}

	// MSM = 5*G + 7*(2G) + 11*(3G) = 5G + 14G + 33G = 52G
	result := BanderMSM(points, scalars)
	expected := BanderScalarMul(g, big.NewInt(52))

	if !BanderEqual(result, expected) {
		t.Error("MSM result mismatch")
	}
}

func TestBanderSerializeDeserialize(t *testing.T) {
	g := BanderGenerator()

	serialized := BanderSerialize(g)
	deserialized, err := BanderDeserialize(serialized)
	if err != nil {
		t.Fatalf("deserialization error: %v", err)
	}

	if !BanderEqual(g, deserialized) {
		t.Error("deserialized point does not match original")
	}
}

func TestBanderSerializeIdentity(t *testing.T) {
	id := BanderIdentity()
	serialized := BanderSerialize(id)

	// Check it is deterministic.
	serialized2 := BanderSerialize(id)
	if serialized != serialized2 {
		t.Error("identity serialization not deterministic")
	}
}

func TestBanderSerializeRoundtrip(t *testing.T) {
	g := BanderGenerator()

	// Test several scalar multiples.
	for i := int64(2); i < 20; i++ {
		p := BanderScalarMul(g, big.NewInt(i))
		data := BanderSerialize(p)
		recovered, err := BanderDeserialize(data)
		if err != nil {
			t.Fatalf("round-trip %d: deserialize error: %v", i, err)
		}
		if !BanderEqual(p, recovered) {
			t.Errorf("round-trip %d: point mismatch", i)
		}
	}
}

func TestBanderMapToField(t *testing.T) {
	g := BanderGenerator()
	f := BanderMapToField(g)

	if f.Sign() == 0 {
		t.Error("map-to-field of generator should be nonzero")
	}

	// Identity maps to zero.
	id := BanderIdentity()
	fid := BanderMapToField(id)
	if fid.Sign() != 0 {
		t.Error("map-to-field of identity should be zero")
	}

	// Different points should map to different field elements.
	g2 := BanderScalarMul(g, big.NewInt(2))
	f2 := BanderMapToField(g2)
	if f.Cmp(f2) == 0 {
		t.Error("different points should map to different field elements")
	}
}

func TestBanderMapToBytes(t *testing.T) {
	g := BanderGenerator()
	b := BanderMapToBytes(g)

	allZero := true
	for _, v := range b {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("map-to-bytes of generator should not be all zeros")
	}
}

func TestPedersenGenerators(t *testing.T) {
	gens := GeneratePedersenGenerators()

	// All generators should be on the curve.
	for i, g := range gens {
		if g == nil {
			t.Fatalf("generator %d is nil", i)
		}
		x, y := g.BanderToAffine()
		if !banderIsOnCurve(x, y) {
			t.Fatalf("generator %d not on curve", i)
		}
	}

	// Generators should be distinct.
	for i := 0; i < NumPedersenGenerators; i++ {
		for j := i + 1; j < NumPedersenGenerators; j++ {
			if BanderEqual(gens[i], gens[j]) {
				t.Fatalf("generators %d and %d are equal", i, j)
			}
		}
	}
}

func TestPedersenCommit(t *testing.T) {
	// Commit to [1, 0, 0, ...]
	values := make([]*big.Int, 4)
	values[0] = big.NewInt(1)
	values[1] = big.NewInt(0)
	values[2] = big.NewInt(0)
	values[3] = big.NewInt(0)

	c := PedersenCommit(values)
	if c.BanderIsIdentity() {
		t.Error("commitment to [1,0,0,0] should not be identity")
	}

	// Commit to all zeros should be identity.
	zeros := make([]*big.Int, 4)
	for i := range zeros {
		zeros[i] = big.NewInt(0)
	}
	cZero := PedersenCommit(zeros)
	if !cZero.BanderIsIdentity() {
		t.Error("commitment to all zeros should be identity")
	}

	// Linearity: Commit([a]) + Commit([b]) = Commit([a+b])
	va := make([]*big.Int, 4)
	va[0] = big.NewInt(5)
	va[1] = big.NewInt(3)
	va[2] = big.NewInt(0)
	va[3] = big.NewInt(0)

	vb := make([]*big.Int, 4)
	vb[0] = big.NewInt(7)
	vb[1] = big.NewInt(2)
	vb[2] = big.NewInt(0)
	vb[3] = big.NewInt(0)

	ca := PedersenCommit(va)
	cb := PedersenCommit(vb)
	cSum := BanderAdd(ca, cb)

	vab := make([]*big.Int, 4)
	vab[0] = big.NewInt(12)
	vab[1] = big.NewInt(5)
	vab[2] = big.NewInt(0)
	vab[3] = big.NewInt(0)
	cDirect := PedersenCommit(vab)

	if !BanderEqual(cSum, cDirect) {
		t.Error("Pedersen commitment linearity violated")
	}
}

func TestPedersenCommitBytes(t *testing.T) {
	values := make([]*big.Int, 4)
	values[0] = big.NewInt(42)
	values[1] = big.NewInt(0)
	values[2] = big.NewInt(0)
	values[3] = big.NewInt(0)

	b := PedersenCommitBytes(values)

	allZero := true
	for _, v := range b {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("commitment bytes should not be all zeros")
	}

	// Same input should give same output.
	b2 := PedersenCommitBytes(values)
	if b != b2 {
		t.Error("commitment bytes not deterministic")
	}
}

func TestBanderIsOnCurve(t *testing.T) {
	// Generator should be on curve.
	gx, gy := BanderGenerator().BanderToAffine()
	if !banderIsOnCurve(gx, gy) {
		t.Error("generator not on curve")
	}

	// Origin (0, 1) should be on curve (identity of twisted Edwards).
	if !banderIsOnCurve(big.NewInt(0), big.NewInt(1)) {
		t.Error("(0, 1) should be on curve")
	}

	// Random point should not be on curve.
	if banderIsOnCurve(big.NewInt(42), big.NewInt(43)) {
		t.Error("(42, 43) should not be on curve")
	}
}

func TestBanderEqualSamePoint(t *testing.T) {
	g := BanderGenerator()
	g2 := BanderScalarMul(g, big.NewInt(1))
	if !BanderEqual(g, g2) {
		t.Error("same point should be equal")
	}
}

func TestBanderEqualDifferentPoints(t *testing.T) {
	g := BanderGenerator()
	g2 := BanderScalarMul(g, big.NewInt(2))
	if BanderEqual(g, g2) {
		t.Error("different points should not be equal")
	}
}

func TestBanderScalarMulAssociative(t *testing.T) {
	g := BanderGenerator()
	a := big.NewInt(7)
	b := big.NewInt(13)

	// (a*b)*G = a*(b*G)
	ab := banderFrMul(a, b)
	lhs := BanderScalarMul(g, ab)
	bG := BanderScalarMul(g, b)
	rhs := BanderScalarMul(bG, a)

	if !BanderEqual(lhs, rhs) {
		t.Error("scalar multiplication not associative")
	}
}

func TestBanderFr(t *testing.T) {
	r := BanderFr()
	if r.Cmp(banderFr) != 0 {
		t.Error("BanderFr() should return the field modulus")
	}
	// Should be a copy, not the same pointer.
	r.SetInt64(0)
	if banderFr.Sign() == 0 {
		t.Error("BanderFr() should return a copy")
	}
}

func TestBanderCurveEquation(t *testing.T) {
	// Verify the curve parameters are consistent.
	// For the twisted Edwards curve -5x^2 + y^2 = 1 + d*x^2*y^2,
	// the identity (0, 1) should satisfy: -5*0 + 1 = 1 + d*0 => 1 = 1.
	x := big.NewInt(0)
	y := big.NewInt(1)
	lhs := banderFrAdd(banderFrMul(banderA, banderFrSqr(x)), banderFrSqr(y))
	rhs := banderFrAdd(big.NewInt(1), banderFrMul(banderD, banderFrMul(banderFrSqr(x), banderFrSqr(y))))
	if lhs.Cmp(rhs) != 0 {
		t.Error("curve equation not satisfied at identity")
	}
}
