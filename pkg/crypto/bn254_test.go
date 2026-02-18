package crypto

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"
)

// TestG1OnCurve verifies basic curve membership checks.
func TestG1OnCurve(t *testing.T) {
	// Generator (1, 2) should be on the curve.
	if !g1IsOnCurve(big.NewInt(1), big.NewInt(2)) {
		t.Fatal("generator (1,2) should be on the curve")
	}

	// Identity (0, 0) should be valid.
	if !g1IsOnCurve(big.NewInt(0), big.NewInt(0)) {
		t.Fatal("identity (0,0) should be valid")
	}

	// Random invalid point.
	if g1IsOnCurve(big.NewInt(1), big.NewInt(3)) {
		t.Fatal("(1,3) should not be on the curve")
	}
}

// TestG1AddIdentity verifies P + 0 = P.
func TestG1AddIdentity(t *testing.T) {
	gen := G1Generator()
	inf := G1Infinity()

	r := g1Add(gen, inf)
	rx, ry := r.g1ToAffine()
	if rx.Cmp(big.NewInt(1)) != 0 || ry.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("G + 0 = (%s, %s), want (1, 2)", rx, ry)
	}

	r2 := g1Add(inf, gen)
	rx2, ry2 := r2.g1ToAffine()
	if rx2.Cmp(big.NewInt(1)) != 0 || ry2.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("0 + G = (%s, %s), want (1, 2)", rx2, ry2)
	}
}

// TestG1AddInverse verifies P + (-P) = 0.
func TestG1AddInverse(t *testing.T) {
	gen := G1Generator()
	neg := g1Neg(gen)

	r := g1Add(gen, neg)
	if !r.g1IsInfinity() {
		rx, ry := r.g1ToAffine()
		t.Fatalf("G + (-G) should be infinity, got (%s, %s)", rx, ry)
	}
}

// TestG1Double verifies 2*G via doubling.
func TestG1Double(t *testing.T) {
	gen := G1Generator()
	d := g1Double(gen)
	dx, dy := d.g1ToAffine()

	// 2G should also be on the curve.
	if !g1IsOnCurve(dx, dy) {
		t.Fatal("2*G should be on the curve")
	}

	// Verify via addition.
	a := g1Add(gen, gen)
	ax, ay := a.g1ToAffine()
	if dx.Cmp(ax) != 0 || dy.Cmp(ay) != 0 {
		t.Fatalf("2*G via double != 2*G via add: (%s,%s) != (%s,%s)", dx, dy, ax, ay)
	}
}

// TestG1ScalarMul verifies scalar multiplication.
func TestG1ScalarMul(t *testing.T) {
	gen := G1Generator()

	// 1*G = G
	r := G1ScalarMul(gen, big.NewInt(1))
	rx, ry := r.g1ToAffine()
	if rx.Cmp(big.NewInt(1)) != 0 || ry.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("1*G = (%s, %s), want (1, 2)", rx, ry)
	}

	// 0*G = infinity
	r0 := G1ScalarMul(gen, big.NewInt(0))
	if !r0.g1IsInfinity() {
		t.Fatal("0*G should be infinity")
	}

	// n*G = infinity (curve order)
	rn := G1ScalarMul(gen, bn254N)
	if !rn.g1IsInfinity() {
		rnx, rny := rn.g1ToAffine()
		t.Fatalf("n*G should be infinity, got (%s, %s)", rnx, rny)
	}

	// 2*G via scalar mul should match doubling.
	r2 := G1ScalarMul(gen, big.NewInt(2))
	r2x, r2y := r2.g1ToAffine()
	d := g1Double(gen)
	dx, dy := d.g1ToAffine()
	if r2x.Cmp(dx) != 0 || r2y.Cmp(dy) != 0 {
		t.Fatalf("2*G scalar mul != double: (%s,%s) != (%s,%s)", r2x, r2y, dx, dy)
	}
}

// TestBN254AddPrecompile tests the precompile interface for BN254Add.
func TestBN254AddPrecompile(t *testing.T) {
	// Add two identity points -> identity.
	input := make([]byte, 128)
	out, err := BN254Add(input)
	if err != nil {
		t.Fatalf("BN254Add(0+0): %v", err)
	}
	if !bytes.Equal(out, make([]byte, 64)) {
		t.Fatalf("0+0 should be identity, got %x", out)
	}

	// G + 0 = G.
	input2 := make([]byte, 128)
	input2[31] = 1 // x=1
	input2[63] = 2 // y=2
	out2, err := BN254Add(input2)
	if err != nil {
		t.Fatalf("BN254Add(G+0): %v", err)
	}
	if out2[31] != 1 || out2[63] != 2 {
		t.Fatalf("G+0 = %x, want G=(1,2)", out2)
	}

	// Short input padding.
	shortInput := []byte{0x01}
	_, err = BN254Add(shortInput)
	// x=1, y=0 is not on the curve (1^3 + 3 = 4, sqrt(4) = 2, not 0)
	if err == nil {
		t.Fatal("expected error for invalid point (1, 0)")
	}
}

// TestBN254ScalarMulPrecompile tests the precompile interface for BN254ScalarMul.
func TestBN254ScalarMulPrecompile(t *testing.T) {
	// 0 * G = identity
	input := make([]byte, 96)
	input[31] = 1 // x=1
	input[63] = 2 // y=2
	// scalar = 0
	out, err := BN254ScalarMul(input)
	if err != nil {
		t.Fatalf("BN254ScalarMul(0*G): %v", err)
	}
	if !bytes.Equal(out, make([]byte, 64)) {
		t.Fatalf("0*G should be identity, got %x", out)
	}

	// 1 * G = G
	input2 := make([]byte, 96)
	input2[31] = 1  // x=1
	input2[63] = 2  // y=2
	input2[95] = 1  // scalar=1
	out2, err := BN254ScalarMul(input2)
	if err != nil {
		t.Fatalf("BN254ScalarMul(1*G): %v", err)
	}
	if out2[31] != 1 || out2[63] != 2 {
		t.Fatalf("1*G = %x, want G=(1,2)", out2)
	}

	// 2 * G
	input3 := make([]byte, 96)
	input3[31] = 1 // x=1
	input3[63] = 2 // y=2
	input3[95] = 2 // scalar=2
	out3, err := BN254ScalarMul(input3)
	if err != nil {
		t.Fatalf("BN254ScalarMul(2*G): %v", err)
	}
	// Verify 2*G is on the curve.
	x3 := new(big.Int).SetBytes(out3[0:32])
	y3 := new(big.Int).SetBytes(out3[32:64])
	if !g1IsOnCurve(x3, y3) {
		t.Fatalf("2*G = (%s, %s) is not on the curve", x3, y3)
	}
}

// TestBN254ScalarMul_KnownVector verifies against known 2*G coordinates.
func TestBN254ScalarMul_KnownVector(t *testing.T) {
	// 2*G on BN254 is known to be:
	// x = 1368015179489954701390400359078579693043519447331113978918064868415326638035
	// y = 9918110051302171585080402603319702774565515993150576347155970296011118125764
	gen := G1Generator()
	r := G1ScalarMul(gen, big.NewInt(2))
	rx, ry := r.g1ToAffine()

	wantX, _ := new(big.Int).SetString("1368015179489954701390400359078579693043519447331113978918064868415326638035", 10)
	wantY, _ := new(big.Int).SetString("9918110051302171585080402603319702774565515993150576347155970296011118125764", 10)

	if rx.Cmp(wantX) != 0 || ry.Cmp(wantY) != 0 {
		t.Fatalf("2*G = (%s, %s), want (%s, %s)", rx, ry, wantX, wantY)
	}
}

// TestBN254AddKnownVector verifies G + G = 2G using the precompile interface.
func TestBN254AddKnownVector(t *testing.T) {
	input := make([]byte, 128)
	input[31] = 1  // x1=1
	input[63] = 2  // y1=2
	input[95] = 1  // x2=1
	input[127] = 2 // y2=2

	out, err := BN254Add(input)
	if err != nil {
		t.Fatalf("BN254Add(G+G): %v", err)
	}

	x := new(big.Int).SetBytes(out[0:32])
	y := new(big.Int).SetBytes(out[32:64])

	wantX, _ := new(big.Int).SetString("1368015179489954701390400359078579693043519447331113978918064868415326638035", 10)
	wantY, _ := new(big.Int).SetString("9918110051302171585080402603319702774565515993150576347155970296011118125764", 10)

	if x.Cmp(wantX) != 0 || y.Cmp(wantY) != 0 {
		t.Fatalf("G+G = (%s, %s), want (%s, %s)", x, y, wantX, wantY)
	}
}

// TestBN254InvalidPoints tests that invalid points are rejected.
func TestBN254InvalidPoints(t *testing.T) {
	// Point not on curve for add.
	input := make([]byte, 128)
	input[31] = 1 // x=1
	input[63] = 3 // y=3 (not on curve)
	_, err := BN254Add(input)
	if err == nil {
		t.Fatal("expected error for invalid point in add")
	}

	// Point not on curve for scalar mul.
	input2 := make([]byte, 96)
	input2[31] = 1 // x=1
	input2[63] = 3 // y=3
	input2[95] = 1 // scalar=1
	_, err = BN254ScalarMul(input2)
	if err == nil {
		t.Fatal("expected error for invalid point in scalar mul")
	}
}

// TestBN254PairingEmptyInput tests that empty pairing input returns true (1).
func TestBN254PairingEmptyInput(t *testing.T) {
	out, err := BN254PairingCheck(nil)
	if err != nil {
		t.Fatalf("empty pairing: %v", err)
	}
	if out[31] != 1 {
		t.Fatalf("empty pairing should return 1, got %x", out)
	}
}

// TestBN254PairingInvalidLength tests that non-192-multiple lengths are rejected.
func TestBN254PairingInvalidLength(t *testing.T) {
	_, err := BN254PairingCheck(make([]byte, 100))
	if err == nil {
		t.Fatal("expected error for invalid pairing input length")
	}
}

// TestG2OnCurve verifies the G2 generator is on the twist curve.
func TestG2OnCurve(t *testing.T) {
	gen := G2Generator()
	x, y := gen.g2ToAffine()
	if !g2IsOnCurve(x, y) {
		t.Fatal("G2 generator should be on the twist curve")
	}
}

// TestBN254PairingWithIdentityG1 tests pairing with G1 identity.
func TestBN254PairingWithIdentityG1(t *testing.T) {
	// e(0, Q) = 1 for any Q.
	// Input: G1=(0,0), G2=generator
	input := make([]byte, 192)
	// G1 is (0,0) - identity.
	// G2 generator coordinates (big-endian 32-byte each):
	// x_imag, x_real, y_imag, y_real
	xImag := g2GenXa1.Bytes()
	xReal := g2GenXa0.Bytes()
	yImag := g2GenYa1.Bytes()
	yReal := g2GenYa0.Bytes()
	copy(input[96-len(xImag):96], xImag)
	copy(input[128-len(xReal):128], xReal)
	copy(input[160-len(yImag):160], yImag)
	copy(input[192-len(yReal):192], yReal)

	out, err := BN254PairingCheck(input)
	if err != nil {
		t.Fatalf("pairing with identity G1: %v", err)
	}
	if out[31] != 1 {
		t.Fatalf("e(0, Q) should be 1, got %x", out)
	}
}

// TestBN254PairingWithIdentityG2 tests pairing with G2 identity.
func TestBN254PairingWithIdentityG2(t *testing.T) {
	// e(P, 0) = 1 for any P.
	input := make([]byte, 192)
	input[31] = 1 // G1 x=1
	input[63] = 2 // G1 y=2
	// G2 is (0,0,0,0) - identity (all zeros already).

	out, err := BN254PairingCheck(input)
	if err != nil {
		t.Fatalf("pairing with identity G2: %v", err)
	}
	if out[31] != 1 {
		t.Fatalf("e(P, 0) should be 1, got %x", out)
	}
}

// TestBN254PairingBilinearity tests e(a*P, b*Q) = e(P, Q)^(ab).
// We check e(2*G1, G2) * e(G1, -G2)^2 = 1
// i.e., e(2G, Q) = e(G, Q)^2, which means e(2G, Q) * e(G, -Q)^2 = 1.
// Simpler: e(G, Q) * e(-G, Q) = 1.
func TestBN254PairingBasicBilinearity(t *testing.T) {
	// e(G, Q) * e(-G, Q) should equal 1.
	// This is because e(G, Q) * e(-G, Q) = e(G + (-G), Q) = e(0, Q) = 1.
	input := make([]byte, 384) // 2 pairs

	// Pair 1: (G1, G2)
	input[31] = 1 // G1 x=1
	input[63] = 2 // G1 y=2
	xImag := g2GenXa1.Bytes()
	xReal := g2GenXa0.Bytes()
	yImag := g2GenYa1.Bytes()
	yReal := g2GenYa0.Bytes()
	copy(input[96-len(xImag):96], xImag)
	copy(input[128-len(xReal):128], xReal)
	copy(input[160-len(yImag):160], yImag)
	copy(input[192-len(yReal):192], yReal)

	// Pair 2: (-G1, G2)
	negY := fpNeg(big.NewInt(2)) // -2 mod p
	input[192+31] = 1            // -G1 x=1
	negYBytes := negY.Bytes()
	copy(input[192+64-len(negYBytes):192+64], negYBytes) // -G1 y = p-2
	copy(input[192+96-len(xImag):192+96], xImag)
	copy(input[192+128-len(xReal):192+128], xReal)
	copy(input[192+160-len(yImag):192+160], yImag)
	copy(input[192+192-len(yReal):192+192], yReal)

	out, err := BN254PairingCheck(input)
	if err != nil {
		t.Fatalf("pairing bilinearity check: %v", err)
	}
	if out[31] != 1 {
		t.Fatalf("e(G,Q)*e(-G,Q) should be 1, got %x", out)
	}
}

// TestBN254AddPrecompileHex tests with go-ethereum test vectors.
func TestBN254AddPrecompileHex(t *testing.T) {
	// Test vector: G + G = 2G
	// G = (1, 2), 2G is known.
	input := make([]byte, 128)
	input[31] = 1
	input[63] = 2
	input[95] = 1
	input[127] = 2

	out, err := BN254Add(input)
	if err != nil {
		t.Fatalf("add G+G: %v", err)
	}

	wantHex := "030644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd3" +
		"15ed738c0e0a7c92e7845f96b2ae9c0a68a6a449e3538fc7ff3ebf7a5a18a2c4"
	want, _ := hex.DecodeString(wantHex)
	if !bytes.Equal(out, want) {
		t.Fatalf("G+G:\n  got  %x\n  want %s", out, wantHex)
	}
}

// TestBN254ScalarMulPrecompileHex tests scalar mul with known hex vectors.
func TestBN254ScalarMulPrecompileHex(t *testing.T) {
	// 2 * G = 2G
	input := make([]byte, 96)
	input[31] = 1
	input[63] = 2
	input[95] = 2

	out, err := BN254ScalarMul(input)
	if err != nil {
		t.Fatalf("scalarMul 2*G: %v", err)
	}

	wantHex := "030644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd3" +
		"15ed738c0e0a7c92e7845f96b2ae9c0a68a6a449e3538fc7ff3ebf7a5a18a2c4"
	want, _ := hex.DecodeString(wantHex)
	if !bytes.Equal(out, want) {
		t.Fatalf("2*G:\n  got  %x\n  want %s", out, wantHex)
	}
}

// TestFp2Arithmetic tests basic F_p^2 operations.
func TestFp2Arithmetic(t *testing.T) {
	a := &fp2{a0: big.NewInt(3), a1: big.NewInt(4)}
	b := &fp2{a0: big.NewInt(5), a1: big.NewInt(7)}

	// (3+4i)(5+7i) = 15+21i+20i+28i^2 = 15+41i-28 = -13+41i
	c := fp2Mul(a, b)
	want0 := fpSub(big.NewInt(15), big.NewInt(28)) // -13 mod p
	want1 := big.NewInt(41)

	if c.a0.Cmp(want0) != 0 || c.a1.Cmp(want1) != 0 {
		t.Fatalf("fp2Mul = (%s, %s), want (%s, 41)", c.a0, c.a1, want0)
	}

	// Inverse: a * a^(-1) = 1
	aInv := fp2Inv(a)
	prod := fp2Mul(a, aInv)
	if prod.a0.Cmp(big.NewInt(1)) != 0 || prod.a1.Sign() != 0 {
		t.Fatalf("a * a^(-1) = (%s, %s), want (1, 0)", prod.a0, prod.a1)
	}
}

// TestFp12Arithmetic tests basic F_p^12 operations.
func TestFp12Arithmetic(t *testing.T) {
	one := fp12One()
	if !one.isOne() {
		t.Fatal("fp12One should be one")
	}

	prod := fp12Mul(one, one)
	if !prod.isOne() {
		t.Fatal("1*1 should be 1 in F_p^12")
	}

	inv := fp12Inv(one)
	if !inv.isOne() {
		t.Fatal("1^(-1) should be 1 in F_p^12")
	}
}

// TestPairingSingle tests that e(G1, G2) != 1 (non-trivial pairing).
func TestPairingSingle(t *testing.T) {
	g1 := G1Generator()
	g2 := G2Generator()

	result := BN254Pair(g1, g2)
	if result.isOne() {
		t.Fatal("e(G1, G2) should NOT be 1 for non-trivial generators")
	}
}

// TestPairingInfinity tests that e(0, Q) = 1.
func TestPairingInfinity(t *testing.T) {
	inf := G1Infinity()
	g2 := G2Generator()
	result := BN254Pair(inf, g2)
	if !result.isOne() {
		t.Fatal("e(0, G2) should be 1")
	}
}

// TestMillerLoopDirect tests the Miller loop output before final exponentiation.
func TestMillerLoopDirect(t *testing.T) {
	g1 := G1Generator()
	g2 := G2Generator()

	px, py := g1.g1ToAffine()
	qx, qy := g2.g2ToAffine()

	ml := millerLoop(px, py, qx, qy)
	if ml.isOne() {
		t.Fatal("Miller loop output should not be 1 before final exp")
	}
}

// TestBN254PairingNonTrivialFail tests that an incorrect pairing returns 0.
func TestBN254PairingNonTrivialFail(t *testing.T) {
	// e(G, Q) alone (without cancellation) should not be 1.
	input := make([]byte, 192)
	input[31] = 1 // G1 x=1
	input[63] = 2 // G1 y=2
	xImag := g2GenXa1.Bytes()
	xReal := g2GenXa0.Bytes()
	yImag := g2GenYa1.Bytes()
	yReal := g2GenYa0.Bytes()
	copy(input[96-len(xImag):96], xImag)
	copy(input[128-len(xReal):128], xReal)
	copy(input[160-len(yImag):160], yImag)
	copy(input[192-len(yReal):192], yReal)

	out, err := BN254PairingCheck(input)
	if err != nil {
		t.Fatalf("pairing single: %v", err)
	}
	if out[31] != 0 {
		t.Fatalf("e(G,Q) alone should NOT be 1, got %x", out)
	}
}

// TestBN254ScalarMulLargeScalar tests scalar mul with a large scalar.
func TestBN254ScalarMulLargeScalar(t *testing.T) {
	// Use n-1 as scalar, should give -G.
	gen := G1Generator()
	nMinus1 := new(big.Int).Sub(bn254N, big.NewInt(1))
	r := G1ScalarMul(gen, nMinus1)
	rx, ry := r.g1ToAffine()

	// -G = (1, p-2)
	if rx.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("(n-1)*G x = %s, want 1", rx)
	}
	negY := new(big.Int).Sub(bn254P, big.NewInt(2))
	if ry.Cmp(negY) != 0 {
		t.Fatalf("(n-1)*G y = %s, want %s", ry, negY)
	}
}

// TestBN254CoordinatesOutOfRange tests that coordinates >= p are rejected.
func TestBN254CoordinatesOutOfRange(t *testing.T) {
	// x = p (out of range)
	input := make([]byte, 128)
	pBytes := bn254P.Bytes()
	copy(input[32-len(pBytes):32], pBytes) // x = p
	// y = 0
	_, err := BN254Add(input)
	if err == nil {
		t.Fatal("expected error for x >= p")
	}
}
