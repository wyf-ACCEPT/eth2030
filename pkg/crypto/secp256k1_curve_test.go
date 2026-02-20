package crypto

// Comprehensive tests for secp256k1 curve operations: IsOnCurve, Add, Double,
// ScalarMult, ScalarBaseMult, recoverPublicKey, and computeY.
//
// Tests focus on algebraic properties, known vectors, edge cases,
// and serialization round-trips.

import (
	"math/big"
	"testing"
)

// ---------------------------------------------------------------------------
// Curve parameters
// ---------------------------------------------------------------------------

func TestSecp256k1CurveParamsValid(t *testing.T) {
	curve := S256().(*secp256k1Curve)

	// p should be prime.
	if !curve.p.ProbablyPrime(20) {
		t.Error("secp256k1 p is not prime")
	}

	// n should be prime.
	if !curve.n.ProbablyPrime(20) {
		t.Error("secp256k1 n is not prime")
	}

	// p should be 256 bits.
	if curve.p.BitLen() != 256 {
		t.Errorf("p bit length = %d, want 256", curve.p.BitLen())
	}

	// n should be 256 bits.
	if curve.n.BitLen() != 256 {
		t.Errorf("n bit length = %d, want 256", curve.n.BitLen())
	}

	// b = 7.
	if curve.b.Cmp(big.NewInt(7)) != 0 {
		t.Errorf("b = %s, want 7", curve.b)
	}
}

func TestSecp256k1GeneratorOnCurve(t *testing.T) {
	curve := S256()
	params := curve.Params()
	if !curve.IsOnCurve(params.Gx, params.Gy) {
		t.Error("generator is not on the curve")
	}
}

// ---------------------------------------------------------------------------
// IsOnCurve
// ---------------------------------------------------------------------------

func TestSecp256k1IsOnCurveGenerator(t *testing.T) {
	curve := S256()
	params := curve.Params()
	if !curve.IsOnCurve(params.Gx, params.Gy) {
		t.Error("generator should be on curve")
	}
}

func TestSecp256k1IsOnCurveKnownPoint(t *testing.T) {
	curve := S256()
	params := curve.Params()

	// 2*G should be on the curve.
	x2, y2 := curve.Double(params.Gx, params.Gy)
	if !curve.IsOnCurve(x2, y2) {
		t.Error("2*G should be on the curve")
	}

	// 3*G should be on the curve.
	x3, y3 := curve.ScalarBaseMult(big.NewInt(3).Bytes())
	if !curve.IsOnCurve(x3, y3) {
		t.Error("3*G should be on the curve")
	}
}

func TestSecp256k1IsOnCurveInvalid(t *testing.T) {
	curve := S256()
	c := curve.(*secp256k1Curve)

	tests := []struct {
		name string
		x, y *big.Int
	}{
		{"(0,1)", big.NewInt(0), big.NewInt(1)},
		{"(1,1)", big.NewInt(1), big.NewInt(1)},
		{"(2,3)", big.NewInt(2), big.NewInt(3)},
		{"x=p", new(big.Int).Set(c.p), big.NewInt(2)},
		{"y=p", big.NewInt(1), new(big.Int).Set(c.p)},
		{"x=p+1", new(big.Int).Add(c.p, big.NewInt(1)), big.NewInt(2)},
		{"negative x", big.NewInt(-1), big.NewInt(2)},
		{"negative y", big.NewInt(1), big.NewInt(-1)},
		{"nil x", nil, big.NewInt(2)},
		{"nil y", big.NewInt(1), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if curve.IsOnCurve(tc.x, tc.y) {
				t.Error("should not be on curve")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Point addition
// ---------------------------------------------------------------------------

func TestSecp256k1AddIdentityProperty(t *testing.T) {
	curve := S256()
	params := curve.Params()
	inf := new(big.Int)

	// G + O = G
	x, y := curve.Add(params.Gx, params.Gy, inf, inf)
	if x.Cmp(params.Gx) != 0 || y.Cmp(params.Gy) != 0 {
		t.Error("G + O should equal G")
	}

	// O + G = G
	x, y = curve.Add(inf, inf, params.Gx, params.Gy)
	if x.Cmp(params.Gx) != 0 || y.Cmp(params.Gy) != 0 {
		t.Error("O + G should equal G")
	}

	// O + O = O
	x, y = curve.Add(inf, inf, inf, inf)
	if x.Sign() != 0 || y.Sign() != 0 {
		t.Error("O + O should equal O")
	}
}

func TestSecp256k1AddInverseProperty(t *testing.T) {
	curve := S256().(*secp256k1Curve)
	params := curve.Params()

	negGy := new(big.Int).Sub(curve.p, params.Gy)
	x, y := curve.Add(params.Gx, params.Gy, params.Gx, negGy)
	if x.Sign() != 0 || y.Sign() != 0 {
		t.Error("G + (-G) should be point at infinity")
	}
}

func TestSecp256k1AddCommutativity(t *testing.T) {
	curve := S256()
	params := curve.Params()

	gx, gy := params.Gx, params.Gy
	twoGx, twoGy := curve.Double(gx, gy)

	abx, aby := curve.Add(gx, gy, twoGx, twoGy)
	bax, bay := curve.Add(twoGx, twoGy, gx, gy)

	if abx.Cmp(bax) != 0 || aby.Cmp(bay) != 0 {
		t.Error("point addition is not commutative")
	}
}

func TestSecp256k1AddAssociativity(t *testing.T) {
	curve := S256()
	params := curve.Params()

	ax, ay := params.Gx, params.Gy
	bx, by := curve.Double(ax, ay)
	cx, cy := curve.ScalarBaseMult(big.NewInt(3).Bytes())

	// (A+B)+C
	abx, aby := curve.Add(ax, ay, bx, by)
	lhsX, lhsY := curve.Add(abx, aby, cx, cy)

	// A+(B+C)
	bcx, bcy := curve.Add(bx, by, cx, cy)
	rhsX, rhsY := curve.Add(ax, ay, bcx, bcy)

	if lhsX.Cmp(rhsX) != 0 || lhsY.Cmp(rhsY) != 0 {
		t.Error("point addition is not associative")
	}
}

func TestSecp256k1AddSameXDifferentY(t *testing.T) {
	// P + (-P) = O (same x, different y).
	curve := S256().(*secp256k1Curve)
	params := curve.Params()

	negGy := new(big.Int).Sub(curve.p, params.Gy)
	x, y := curve.Add(params.Gx, params.Gy, params.Gx, negGy)
	if x.Sign() != 0 || y.Sign() != 0 {
		t.Error("P + (-P) should be infinity")
	}
}

// ---------------------------------------------------------------------------
// Point doubling
// ---------------------------------------------------------------------------

func TestSecp256k1DoubleMatchesAddCurve(t *testing.T) {
	curve := S256()
	params := curve.Params()

	dx, dy := curve.Double(params.Gx, params.Gy)
	ax, ay := curve.Add(params.Gx, params.Gy, params.Gx, params.Gy)

	if dx.Cmp(ax) != 0 || dy.Cmp(ay) != 0 {
		t.Error("2*G via Double != G+G via Add")
	}
}

func TestSecp256k1DoubleIdentityCurve(t *testing.T) {
	curve := S256()
	x, y := curve.Double(new(big.Int), new(big.Int))
	if x.Sign() != 0 || y.Sign() != 0 {
		t.Error("2*O should equal O")
	}
}

func TestSecp256k1DoubleOnCurve(t *testing.T) {
	curve := S256()
	params := curve.Params()

	// 2*G should be on the curve.
	x, y := curve.Double(params.Gx, params.Gy)
	if !curve.IsOnCurve(x, y) {
		t.Error("2*G should be on the curve")
	}

	// 4*G should be on the curve.
	x4, y4 := curve.Double(x, y)
	if !curve.IsOnCurve(x4, y4) {
		t.Error("4*G should be on the curve")
	}
}

// ---------------------------------------------------------------------------
// Scalar multiplication
// ---------------------------------------------------------------------------

func TestSecp256k1ScalarMultZeroCurve(t *testing.T) {
	curve := S256()
	params := curve.Params()

	x, y := curve.ScalarMult(params.Gx, params.Gy, []byte{0})
	if x.Sign() != 0 || y.Sign() != 0 {
		t.Error("0*G should be point at infinity")
	}
}

func TestSecp256k1ScalarMultOne(t *testing.T) {
	curve := S256()
	params := curve.Params()

	x, y := curve.ScalarMult(params.Gx, params.Gy, []byte{1})
	if x.Cmp(params.Gx) != 0 || y.Cmp(params.Gy) != 0 {
		t.Error("1*G should equal G")
	}
}

func TestSecp256k1ScalarMultTwo(t *testing.T) {
	curve := S256()
	params := curve.Params()

	x, y := curve.ScalarMult(params.Gx, params.Gy, []byte{2})
	dx, dy := curve.Double(params.Gx, params.Gy)
	if x.Cmp(dx) != 0 || y.Cmp(dy) != 0 {
		t.Error("2*G via ScalarMult should match Double")
	}
}

func TestSecp256k1ScalarMultThree(t *testing.T) {
	curve := S256()
	params := curve.Params()

	x3, y3 := curve.ScalarMult(params.Gx, params.Gy, []byte{3})
	x2, y2 := curve.Double(params.Gx, params.Gy)
	xa, ya := curve.Add(x2, y2, params.Gx, params.Gy)

	if x3.Cmp(xa) != 0 || y3.Cmp(ya) != 0 {
		t.Error("3*G via ScalarMult should match 2*G + G")
	}
}

func TestSecp256k1ScalarMultOrderCurve(t *testing.T) {
	curve := S256()
	params := curve.Params()

	x, y := curve.ScalarMult(params.Gx, params.Gy, params.N.Bytes())
	if x.Sign() != 0 || y.Sign() != 0 {
		t.Error("n*G should be point at infinity")
	}
}

func TestSecp256k1ScalarMultNMinusOne(t *testing.T) {
	curve := S256().(*secp256k1Curve)
	params := curve.Params()

	nMinusOne := new(big.Int).Sub(params.N, big.NewInt(1))
	x, y := curve.ScalarMult(params.Gx, params.Gy, nMinusOne.Bytes())

	// (n-1)*G = -G = (Gx, p - Gy).
	negGy := new(big.Int).Sub(curve.p, params.Gy)
	if x.Cmp(params.Gx) != 0 || y.Cmp(negGy) != 0 {
		t.Errorf("(n-1)*G = (%s, %s), want (%s, %s)", x, y, params.Gx, negGy)
	}
}

func TestSecp256k1ScalarMultNPlusOne(t *testing.T) {
	curve := S256()
	params := curve.Params()

	nPlusOne := new(big.Int).Add(params.N, big.NewInt(1))
	x, y := curve.ScalarMult(params.Gx, params.Gy, nPlusOne.Bytes())

	// (n+1)*G = G (since n*G = O).
	if x.Cmp(params.Gx) != 0 || y.Cmp(params.Gy) != 0 {
		t.Error("(n+1)*G should equal G")
	}
}

func TestSecp256k1ScalarMultLargeScalar(t *testing.T) {
	curve := S256()
	params := curve.Params()

	// 2n*G = O (scalar reduced mod n).
	twoN := new(big.Int).Mul(params.N, big.NewInt(2))
	x, y := curve.ScalarMult(params.Gx, params.Gy, twoN.Bytes())
	if x.Sign() != 0 || y.Sign() != 0 {
		t.Error("2n*G should be point at infinity")
	}
}

func TestSecp256k1ScalarMultResultOnCurve(t *testing.T) {
	curve := S256()
	params := curve.Params()

	scalars := []int64{2, 5, 10, 42, 100, 255}
	for _, k := range scalars {
		x, y := curve.ScalarMult(params.Gx, params.Gy, big.NewInt(k).Bytes())
		if x.Sign() == 0 && y.Sign() == 0 {
			continue // infinity is valid
		}
		if !curve.IsOnCurve(x, y) {
			t.Errorf("%d*G is not on the curve", k)
		}
	}
}

// ---------------------------------------------------------------------------
// ScalarBaseMult
// ---------------------------------------------------------------------------

func TestSecp256k1ScalarBaseMultConsistencyCurve(t *testing.T) {
	curve := S256()
	params := curve.Params()

	scalars := []int64{1, 2, 3, 5, 42, 255}
	for _, k := range scalars {
		x1, y1 := curve.ScalarBaseMult(big.NewInt(k).Bytes())
		x2, y2 := curve.ScalarMult(params.Gx, params.Gy, big.NewInt(k).Bytes())
		if x1.Cmp(x2) != 0 || y1.Cmp(y2) != 0 {
			t.Errorf("k=%d: ScalarBaseMult != ScalarMult(G, k)", k)
		}
	}
}

func TestSecp256k1ScalarBaseMultZero(t *testing.T) {
	curve := S256()
	x, y := curve.ScalarBaseMult([]byte{0})
	if x.Sign() != 0 || y.Sign() != 0 {
		t.Error("ScalarBaseMult(0) should be point at infinity")
	}
}

func TestSecp256k1ScalarBaseMultOrder(t *testing.T) {
	curve := S256()
	params := curve.Params()
	x, y := curve.ScalarBaseMult(params.N.Bytes())
	if x.Sign() != 0 || y.Sign() != 0 {
		t.Error("ScalarBaseMult(N) should be point at infinity")
	}
}

// ---------------------------------------------------------------------------
// Known vectors for 2*G
// ---------------------------------------------------------------------------

func TestSecp256k1DoubleGeneratorKnownVector(t *testing.T) {
	curve := S256()
	params := curve.Params()

	x, y := curve.Double(params.Gx, params.Gy)

	// 2*G must be on the curve and consistent with G+G.
	if !curve.IsOnCurve(x, y) {
		t.Error("2*G is not on the curve")
	}

	ax, ay := curve.Add(params.Gx, params.Gy, params.Gx, params.Gy)
	if x.Cmp(ax) != 0 || y.Cmp(ay) != 0 {
		t.Errorf("2*G via Double = (%x, %x), G+G via Add = (%x, %x)", x, y, ax, ay)
	}

	// Known x-coordinate of 2*G for secp256k1:
	wantX, _ := new(big.Int).SetString("c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5", 16)
	if x.Cmp(wantX) != 0 {
		t.Errorf("2*G x = %x, want %x", x, wantX)
	}
}

// ---------------------------------------------------------------------------
// computeY
// ---------------------------------------------------------------------------

func TestComputeYGenerator(t *testing.T) {
	curve := S256().(*secp256k1Curve)

	y := computeY(curve.gx, curve.p)
	if y == nil {
		t.Fatal("computeY(Gx) returned nil")
	}

	// y should be either Gy or p - Gy.
	negGy := new(big.Int).Sub(curve.p, curve.gy)
	if y.Cmp(curve.gy) != 0 && y.Cmp(negGy) != 0 {
		t.Errorf("computeY(Gx) = %s, want %s or %s", y, curve.gy, negGy)
	}
}

func TestComputeYInvalidX(t *testing.T) {
	curve := S256().(*secp256k1Curve)

	// x = 0: x^3 + 7 = 7. Check if 7 has a sqrt mod p.
	// If not, computeY returns nil.
	y := computeY(big.NewInt(0), curve.p)
	if y != nil {
		// If it does have a sqrt, verify it.
		y2 := new(big.Int).Mul(y, y)
		y2.Mod(y2, curve.p)
		if y2.Cmp(big.NewInt(7)) != 0 {
			t.Errorf("computeY(0)^2 = %s, want 7", y2)
		}
	}
}

func TestComputeY2G(t *testing.T) {
	curve := S256().(*secp256k1Curve)
	params := curve.Params()

	twoGx, _ := curve.Double(params.Gx, params.Gy)
	y := computeY(twoGx, curve.p)
	if y == nil {
		t.Fatal("computeY(2G.x) returned nil")
	}

	// Verify: y^2 = x^3 + 7 mod p.
	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, curve.p)

	x3 := new(big.Int).Mul(twoGx, twoGx)
	x3.Mod(x3, curve.p)
	x3.Mul(x3, twoGx)
	x3.Mod(x3, curve.p)
	x3.Add(x3, big.NewInt(7))
	x3.Mod(x3, curve.p)

	if y2.Cmp(x3) != 0 {
		t.Error("computeY(2G.x)^2 != 2G.x^3 + 7")
	}
}

// ---------------------------------------------------------------------------
// recoverPublicKey
// ---------------------------------------------------------------------------

func TestRecoverPublicKeyRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	hash := Keccak256([]byte("recovery test message"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	v := sig[64]

	pubX, pubY, err := recoverPublicKey(hash, r, s, v)
	if err != nil {
		t.Fatalf("recoverPublicKey failed: %v", err)
	}

	if pubX.Cmp(key.PublicKey.X) != 0 || pubY.Cmp(key.PublicKey.Y) != 0 {
		t.Error("recovered public key does not match original")
	}
}

func TestRecoverPublicKeyInvalidR(t *testing.T) {
	curve := S256().(*secp256k1Curve)
	hash := make([]byte, 32)

	// r >= p should fail.
	_, _, err := recoverPublicKey(hash, new(big.Int).Set(curve.p), big.NewInt(1), 0)
	if err == nil {
		t.Error("recoverPublicKey should reject r >= p")
	}
}

func TestRecoverPublicKeyInvalidV(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	hash := Keccak256([]byte("bad v test"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])

	// Use the opposite v value; recovery should succeed but return a different key.
	v := sig[64] ^ 1
	pubX, pubY, err := recoverPublicKey(hash, r, s, v)
	if err != nil {
		// If it errors, that's fine.
		return
	}
	// If no error, the recovered key should differ.
	if pubX.Cmp(key.PublicKey.X) == 0 && pubY.Cmp(key.PublicKey.Y) == 0 {
		t.Error("opposite v should recover a different key")
	}
}

func TestRecoverPublicKeyZeroR(t *testing.T) {
	hash := make([]byte, 32)
	// r = 0: there's no valid x coordinate for this.
	_, _, err := recoverPublicKey(hash, big.NewInt(0), big.NewInt(1), 0)
	if err == nil {
		t.Error("recoverPublicKey should reject r = 0")
	}
}

// ---------------------------------------------------------------------------
// S256 singleton
// ---------------------------------------------------------------------------

func TestS256ReturnsConsistentInstance(t *testing.T) {
	c1 := S256()
	c2 := S256()
	if c1 != c2 {
		t.Error("S256() should always return the same instance")
	}
}

func TestSecp256k1CurveParamsInterface(t *testing.T) {
	curve := S256()
	params := curve.Params()

	if params.Name != "secp256k1" {
		t.Errorf("Name = %s, want secp256k1", params.Name)
	}
	if params.BitSize != 256 {
		t.Errorf("BitSize = %d, want 256", params.BitSize)
	}
	if params.P == nil || params.N == nil || params.B == nil || params.Gx == nil || params.Gy == nil {
		t.Error("Params() has nil fields")
	}
}

// ---------------------------------------------------------------------------
// Distributive property of scalar mul
// ---------------------------------------------------------------------------

func TestSecp256k1ScalarMultDistributive(t *testing.T) {
	curve := S256()
	params := curve.Params()

	// k*(G + 2G) should equal k*G + k*2G = k*3G
	twoGx, twoGy := curve.Double(params.Gx, params.Gy)
	threeGx, threeGy := curve.Add(params.Gx, params.Gy, twoGx, twoGy)

	k := big.NewInt(7)

	// k * 3G
	kThreeGx, kThreeGy := curve.ScalarMult(threeGx, threeGy, k.Bytes())

	// k*G + k*2G
	kGx, kGy := curve.ScalarMult(params.Gx, params.Gy, k.Bytes())
	kTwoGx, kTwoGy := curve.ScalarMult(twoGx, twoGy, k.Bytes())
	sumX, sumY := curve.Add(kGx, kGy, kTwoGx, kTwoGy)

	if kThreeGx.Cmp(sumX) != 0 || kThreeGy.Cmp(sumY) != 0 {
		t.Error("k*(A+B) != k*A + k*B")
	}
}

// ---------------------------------------------------------------------------
// Multiple sequential doublings
// ---------------------------------------------------------------------------

func TestSecp256k1SequentialDoublings(t *testing.T) {
	curve := S256()
	params := curve.Params()

	// 2^k * G for k=1..8 should all be on the curve.
	x, y := new(big.Int).Set(params.Gx), new(big.Int).Set(params.Gy)
	for k := 1; k <= 8; k++ {
		x, y = curve.Double(x, y)
		if !curve.IsOnCurve(x, y) {
			t.Errorf("2^%d * G is not on the curve", k)
		}
	}

	// 2^8 * G via doubling should match 256*G via scalar mul.
	expected_x, expected_y := curve.ScalarBaseMult(big.NewInt(256).Bytes())
	if x.Cmp(expected_x) != 0 || y.Cmp(expected_y) != 0 {
		t.Error("8 sequential doublings != 256*G")
	}
}
