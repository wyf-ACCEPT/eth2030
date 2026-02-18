package crypto

import (
	"encoding/hex"
	"math/big"
	"testing"
)

// --- Field arithmetic tests ---

func TestBlsFpArithmetic(t *testing.T) {
	a := big.NewInt(17)
	b := big.NewInt(23)

	// Add
	sum := blsFpAdd(a, b)
	if sum.Cmp(big.NewInt(40)) != 0 {
		t.Errorf("blsFpAdd(17, 23) = %s, want 40", sum)
	}

	// Sub
	diff := blsFpSub(b, a)
	if diff.Cmp(big.NewInt(6)) != 0 {
		t.Errorf("blsFpSub(23, 17) = %s, want 6", diff)
	}

	// Mul
	prod := blsFpMul(a, b)
	if prod.Cmp(big.NewInt(391)) != 0 {
		t.Errorf("blsFpMul(17, 23) = %s, want 391", prod)
	}

	// Sqr
	sq := blsFpSqr(a)
	if sq.Cmp(big.NewInt(289)) != 0 {
		t.Errorf("blsFpSqr(17) = %s, want 289", sq)
	}

	// Neg: -17 mod p = p - 17
	neg := blsFpNeg(a)
	expected := new(big.Int).Sub(blsP, a)
	if neg.Cmp(expected) != 0 {
		t.Errorf("blsFpNeg(17) = %s, want %s", neg, expected)
	}

	// Inv: a * a^(-1) == 1 mod p
	inv := blsFpInv(a)
	check := blsFpMul(a, inv)
	if check.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("blsFpMul(17, blsFpInv(17)) = %s, want 1", check)
	}
}

func TestBlsFpSqrt(t *testing.T) {
	// 4 is a perfect square: sqrt(4) = 2 or p-2.
	r := blsFpSqrt(big.NewInt(4))
	if r == nil {
		t.Fatal("blsFpSqrt(4) returned nil")
	}
	if blsFpSqr(r).Cmp(big.NewInt(4)) != 0 {
		t.Errorf("sqrt(4)^2 = %s, want 4", blsFpSqr(r))
	}

	// 0 -> 0
	r = blsFpSqrt(big.NewInt(0))
	if r == nil || r.Sign() != 0 {
		t.Errorf("blsFpSqrt(0) = %v, want 0", r)
	}
}

func TestBlsFpModulus(t *testing.T) {
	// BLS12-381 p should be 381 bits.
	if blsP.BitLen() != 381 {
		t.Errorf("blsP bit length = %d, want 381", blsP.BitLen())
	}
	// p should be prime.
	if !blsP.ProbablyPrime(20) {
		t.Error("blsP is not prime")
	}
	// r should be prime.
	if !blsR.ProbablyPrime(20) {
		t.Error("blsR is not prime")
	}
	// r should be 255 bits.
	if blsR.BitLen() != 255 {
		t.Errorf("blsR bit length = %d, want 255", blsR.BitLen())
	}
}

// --- Fp2 arithmetic tests ---

func TestBlsFp2Arithmetic(t *testing.T) {
	a := &blsFp2{c0: big.NewInt(3), c1: big.NewInt(5)}
	b := &blsFp2{c0: big.NewInt(7), c1: big.NewInt(11)}

	// Add
	sum := blsFp2Add(a, b)
	if !sum.equal(&blsFp2{c0: big.NewInt(10), c1: big.NewInt(16)}) {
		t.Errorf("blsFp2Add: unexpected result")
	}

	// Sub
	diff := blsFp2Sub(b, a)
	if !diff.equal(&blsFp2{c0: big.NewInt(4), c1: big.NewInt(6)}) {
		t.Errorf("blsFp2Sub: unexpected result")
	}

	// Mul: (3+5u)(7+11u) = (3*7 - 5*11) + (3*11 + 5*7)u = (21-55) + (33+35)u = -34 + 68u
	prod := blsFp2Mul(a, b)
	expected := &blsFp2{c0: blsFpSub(big.NewInt(21), big.NewInt(55)), c1: big.NewInt(68)}
	if !prod.equal(expected) {
		t.Errorf("blsFp2Mul: got (%s, %s), want (%s, %s)",
			prod.c0, prod.c1, expected.c0, expected.c1)
	}

	// Inv: a * a^(-1) == 1
	inv := blsFp2Inv(a)
	check := blsFp2Mul(a, inv)
	if !check.equal(blsFp2One()) {
		t.Errorf("blsFp2Mul(a, blsFp2Inv(a)) is not one: (%s, %s)", check.c0, check.c1)
	}
}

// --- G1 point tests ---

func TestBlsG1GeneratorOnCurve(t *testing.T) {
	gen := BlsG1Generator()
	x, y := gen.blsG1ToAffine()
	if !blsG1IsOnCurve(x, y) {
		t.Error("G1 generator is not on curve")
	}
}

func TestBlsG1InfinityAdd(t *testing.T) {
	inf := BlsG1Infinity()
	gen := BlsG1Generator()

	// inf + inf = inf
	r := blsG1Add(inf, inf)
	if !r.blsG1IsInfinity() {
		t.Error("inf + inf should be inf")
	}

	// inf + G = G
	r = blsG1Add(inf, gen)
	rx, ry := r.blsG1ToAffine()
	gx, gy := gen.blsG1ToAffine()
	if rx.Cmp(gx) != 0 || ry.Cmp(gy) != 0 {
		t.Error("inf + G should equal G")
	}

	// G + inf = G
	r = blsG1Add(gen, inf)
	rx, ry = r.blsG1ToAffine()
	if rx.Cmp(gx) != 0 || ry.Cmp(gy) != 0 {
		t.Error("G + inf should equal G")
	}
}

func TestBlsG1Double(t *testing.T) {
	gen := BlsG1Generator()

	// 2*G = G + G
	dbl := blsG1Double(gen)
	add := blsG1Add(gen, gen)

	dx, dy := dbl.blsG1ToAffine()
	ax, ay := add.blsG1ToAffine()

	if dx.Cmp(ax) != 0 || dy.Cmp(ay) != 0 {
		t.Error("G+G != 2*G")
	}

	// 2*G should be on the curve.
	if !blsG1IsOnCurve(dx, dy) {
		t.Error("2*G is not on curve")
	}
}

func TestBlsG1ScalarMul(t *testing.T) {
	gen := BlsG1Generator()

	// 1*G = G
	r := blsG1ScalarMul(gen, big.NewInt(1))
	rx, ry := r.blsG1ToAffine()
	gx, gy := gen.blsG1ToAffine()
	if rx.Cmp(gx) != 0 || ry.Cmp(gy) != 0 {
		t.Error("1*G != G")
	}

	// 0*G = inf
	r = blsG1ScalarMul(gen, big.NewInt(0))
	if !r.blsG1IsInfinity() {
		t.Error("0*G should be infinity")
	}

	// r*G = inf (order of the group)
	r = blsG1ScalarMul(gen, blsR)
	if !r.blsG1IsInfinity() {
		t.Error("[r]*G should be infinity")
	}
}

func TestBlsG1SubgroupCheck(t *testing.T) {
	gen := BlsG1Generator()
	if !blsG1InSubgroup(gen) {
		t.Error("G1 generator should be in subgroup")
	}

	inf := BlsG1Infinity()
	if !blsG1InSubgroup(inf) {
		t.Error("infinity should be in subgroup")
	}
}

func TestBlsG1Neg(t *testing.T) {
	gen := BlsG1Generator()
	neg := blsG1Neg(gen)

	// G + (-G) = inf
	r := blsG1Add(gen, neg)
	if !r.blsG1IsInfinity() {
		t.Error("G + (-G) should be infinity")
	}
}

// --- G2 point tests ---

func TestBlsG2GeneratorOnCurve(t *testing.T) {
	gen := BlsG2Generator()
	x, y := gen.blsG2ToAffine()
	if !blsG2IsOnCurve(x, y) {
		t.Error("G2 generator is not on curve")
	}
}

func TestBlsG2InfinityAdd(t *testing.T) {
	inf := BlsG2Infinity()
	gen := BlsG2Generator()

	// inf + inf = inf
	r := blsG2Add(inf, inf)
	if !r.blsG2IsInfinity() {
		t.Error("inf + inf should be inf")
	}

	// inf + G2 = G2
	r = blsG2Add(inf, gen)
	rx, ry := r.blsG2ToAffine()
	gx, gy := gen.blsG2ToAffine()
	if !rx.equal(gx) || !ry.equal(gy) {
		t.Error("inf + G2 should equal G2")
	}
}

func TestBlsG2Double(t *testing.T) {
	gen := BlsG2Generator()

	dbl := blsG2Double(gen)
	add := blsG2Add(gen, gen)

	dx, dy := dbl.blsG2ToAffine()
	ax, ay := add.blsG2ToAffine()

	if !dx.equal(ax) || !dy.equal(ay) {
		t.Error("G2+G2 != 2*G2")
	}

	if !blsG2IsOnCurve(dx, dy) {
		t.Error("2*G2 is not on curve")
	}
}

func TestBlsG2ScalarMul(t *testing.T) {
	gen := BlsG2Generator()

	// 1*G2 = G2
	r := blsG2ScalarMul(gen, big.NewInt(1))
	rx, ry := r.blsG2ToAffine()
	gx, gy := gen.blsG2ToAffine()
	if !rx.equal(gx) || !ry.equal(gy) {
		t.Error("1*G2 != G2")
	}

	// 0*G2 = inf
	r = blsG2ScalarMul(gen, big.NewInt(0))
	if !r.blsG2IsInfinity() {
		t.Error("0*G2 should be infinity")
	}

	// r*G2 = inf
	r = blsG2ScalarMul(gen, blsR)
	if !r.blsG2IsInfinity() {
		t.Error("[r]*G2 should be infinity")
	}
}

func TestBlsG2Neg(t *testing.T) {
	gen := BlsG2Generator()
	neg := blsG2Neg(gen)

	r := blsG2Add(gen, neg)
	if !r.blsG2IsInfinity() {
		t.Error("G2 + (-G2) should be infinity")
	}
}

// --- Encoding/decoding tests ---

func TestDecodeFpValid(t *testing.T) {
	// Encode zero.
	input := make([]byte, 64)
	v, err := decodeFp(input)
	if err != nil {
		t.Fatalf("decodeFp(0) error: %v", err)
	}
	if v.Sign() != 0 {
		t.Errorf("decodeFp(0) = %s, want 0", v)
	}

	// Encode 1.
	input = make([]byte, 64)
	input[63] = 1
	v, err = decodeFp(input)
	if err != nil {
		t.Fatalf("decodeFp(1) error: %v", err)
	}
	if v.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("decodeFp(1) = %s, want 1", v)
	}
}

func TestDecodeFpInvalidTooLarge(t *testing.T) {
	// Set value >= p (use p itself).
	input := make([]byte, 64)
	pBytes := blsP.Bytes()
	copy(input[64-len(pBytes):], pBytes)
	_, err := decodeFp(input)
	if err == nil {
		t.Error("expected error for value >= p")
	}
}

func TestDecodeFpInvalidPadding(t *testing.T) {
	// Top 16 bytes must be zero. Set byte 0 to non-zero.
	input := make([]byte, 64)
	input[0] = 1
	_, err := decodeFp(input)
	if err == nil {
		t.Error("expected error for non-zero padding bytes")
	}
}

func TestDecodeEncodeG1Infinity(t *testing.T) {
	input := make([]byte, 128) // all zeros
	p, err := decodeG1(input)
	if err != nil {
		t.Fatalf("decodeG1(inf) error: %v", err)
	}
	if !p.blsG1IsInfinity() {
		t.Error("expected infinity")
	}

	encoded := encodeG1(p)
	if len(encoded) != 128 {
		t.Fatalf("encodeG1 length = %d, want 128", len(encoded))
	}
	for _, b := range encoded {
		if b != 0 {
			t.Error("encoded infinity should be all zeros")
			break
		}
	}
}

func TestDecodeEncodeG1Generator(t *testing.T) {
	gen := BlsG1Generator()
	encoded := encodeG1(gen)

	decoded, err := decodeG1(encoded)
	if err != nil {
		t.Fatalf("decodeG1 error: %v", err)
	}

	x1, y1 := gen.blsG1ToAffine()
	x2, y2 := decoded.blsG1ToAffine()
	if x1.Cmp(x2) != 0 || y1.Cmp(y2) != 0 {
		t.Error("round-trip encoding failed for G1 generator")
	}
}

func TestDecodeEncodeG2Infinity(t *testing.T) {
	input := make([]byte, 256) // all zeros
	p, err := decodeG2(input)
	if err != nil {
		t.Fatalf("decodeG2(inf) error: %v", err)
	}
	if !p.blsG2IsInfinity() {
		t.Error("expected infinity")
	}

	encoded := encodeG2(p)
	if len(encoded) != 256 {
		t.Fatalf("encodeG2 length = %d, want 256", len(encoded))
	}
}

func TestDecodeEncodeG2Generator(t *testing.T) {
	gen := BlsG2Generator()
	encoded := encodeG2(gen)

	decoded, err := decodeG2(encoded)
	if err != nil {
		t.Fatalf("decodeG2 error: %v", err)
	}

	x1, y1 := gen.blsG2ToAffine()
	x2, y2 := decoded.blsG2ToAffine()
	if !x1.equal(x2) || !y1.equal(y2) {
		t.Error("round-trip encoding failed for G2 generator")
	}
}

// --- Precompile interface tests ---

func TestBLS12G1AddInfinityPlusInfinity(t *testing.T) {
	input := make([]byte, 256) // two infinity points
	result, err := BLS12G1Add(input)
	if err != nil {
		t.Fatalf("BLS12G1Add error: %v", err)
	}
	if len(result) != 128 {
		t.Fatalf("result len = %d, want 128", len(result))
	}
	for _, b := range result {
		if b != 0 {
			t.Error("inf + inf should be inf (all zeros)")
			break
		}
	}
}

func TestBLS12G1AddGeneratorPlusInfinity(t *testing.T) {
	gen := BlsG1Generator()
	genEnc := encodeG1(gen)
	infEnc := make([]byte, 128)

	// G + inf
	input := append(genEnc, infEnc...)
	result, err := BLS12G1Add(input)
	if err != nil {
		t.Fatalf("BLS12G1Add error: %v", err)
	}

	// Should equal G.
	if hex.EncodeToString(result) != hex.EncodeToString(genEnc) {
		t.Error("G + inf should equal G")
	}
}

func TestBLS12G1AddGeneratorPlusGenerator(t *testing.T) {
	gen := BlsG1Generator()
	genEnc := encodeG1(gen)

	input := append(genEnc, genEnc...)
	result, err := BLS12G1Add(input)
	if err != nil {
		t.Fatalf("BLS12G1Add error: %v", err)
	}
	if len(result) != 128 {
		t.Fatalf("result len = %d, want 128", len(result))
	}

	// Decode and verify 2*G is on the curve.
	p, err := decodeG1(result)
	if err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	x, y := p.blsG1ToAffine()
	if !blsG1IsOnCurve(x, y) {
		t.Error("G + G result is not on the curve")
	}

	// Verify it matches 2*G computed directly.
	twoG := blsG1ScalarMul(gen, big.NewInt(2))
	ex, ey := twoG.blsG1ToAffine()
	if x.Cmp(ex) != 0 || y.Cmp(ey) != 0 {
		t.Error("G + G != 2*G from scalar mul")
	}
}

func TestBLS12G1AddInversePoints(t *testing.T) {
	gen := BlsG1Generator()
	neg := blsG1Neg(gen)

	genEnc := encodeG1(gen)
	negEnc := encodeG1(neg)

	input := append(genEnc, negEnc...)
	result, err := BLS12G1Add(input)
	if err != nil {
		t.Fatalf("BLS12G1Add error: %v", err)
	}

	// G + (-G) = infinity
	for _, b := range result {
		if b != 0 {
			t.Error("G + (-G) should be infinity")
			break
		}
	}
}

func TestBLS12G1MulByOne(t *testing.T) {
	gen := BlsG1Generator()
	genEnc := encodeG1(gen)

	// scalar = 1
	scalar := make([]byte, 32)
	scalar[31] = 1

	input := append(genEnc, scalar...)
	result, err := BLS12G1Mul(input)
	if err != nil {
		t.Fatalf("BLS12G1Mul error: %v", err)
	}

	if hex.EncodeToString(result) != hex.EncodeToString(genEnc) {
		t.Error("1 * G should equal G")
	}
}

func TestBLS12G1MulByTwo(t *testing.T) {
	gen := BlsG1Generator()
	genEnc := encodeG1(gen)

	// scalar = 2
	scalar := make([]byte, 32)
	scalar[31] = 2

	input := append(genEnc, scalar...)
	result, err := BLS12G1Mul(input)
	if err != nil {
		t.Fatalf("BLS12G1Mul error: %v", err)
	}

	// Compare with G + G.
	addInput := append(genEnc, genEnc...)
	addResult, err := BLS12G1Add(addInput)
	if err != nil {
		t.Fatalf("BLS12G1Add error: %v", err)
	}

	if hex.EncodeToString(result) != hex.EncodeToString(addResult) {
		t.Error("2 * G should equal G + G")
	}
}

func TestBLS12G1MulByOrder(t *testing.T) {
	gen := BlsG1Generator()
	genEnc := encodeG1(gen)

	// scalar = r (the subgroup order)
	rBytes := blsR.Bytes()
	scalar := make([]byte, 32)
	copy(scalar[32-len(rBytes):], rBytes)

	input := append(genEnc, scalar...)
	result, err := BLS12G1Mul(input)
	if err != nil {
		t.Fatalf("BLS12G1Mul error: %v", err)
	}

	// r * G = infinity
	for _, b := range result {
		if b != 0 {
			t.Error("[r] * G should be infinity")
			break
		}
	}
}

func TestBLS12G1MulInfinityPoint(t *testing.T) {
	// Infinity point * any scalar = infinity
	input := make([]byte, 160)
	input[159] = 5 // scalar = 5
	result, err := BLS12G1Mul(input)
	if err != nil {
		t.Fatalf("BLS12G1Mul error: %v", err)
	}
	for _, b := range result {
		if b != 0 {
			t.Error("inf * 5 should be infinity")
			break
		}
	}
}

func TestBLS12G1MSMSinglePair(t *testing.T) {
	gen := BlsG1Generator()
	genEnc := encodeG1(gen)

	// Single pair: 3 * G
	scalar := make([]byte, 32)
	scalar[31] = 3

	msmInput := append(genEnc, scalar...)
	msmResult, err := BLS12G1MSM(msmInput)
	if err != nil {
		t.Fatalf("BLS12G1MSM error: %v", err)
	}

	// Compare with scalar mul.
	mulInput := append(genEnc, scalar...)
	mulResult, err := BLS12G1Mul(mulInput)
	if err != nil {
		t.Fatalf("BLS12G1Mul error: %v", err)
	}

	if hex.EncodeToString(msmResult) != hex.EncodeToString(mulResult) {
		t.Error("MSM(G, 3) should equal 3*G")
	}
}

func TestBLS12G1MSMTwoPairs(t *testing.T) {
	gen := BlsG1Generator()
	genEnc := encodeG1(gen)

	// Two pairs: 2*G + 3*G = 5*G
	scalar2 := make([]byte, 32)
	scalar2[31] = 2
	scalar3 := make([]byte, 32)
	scalar3[31] = 3

	msmInput := make([]byte, 0, 320)
	msmInput = append(msmInput, genEnc...)
	msmInput = append(msmInput, scalar2...)
	msmInput = append(msmInput, genEnc...)
	msmInput = append(msmInput, scalar3...)

	msmResult, err := BLS12G1MSM(msmInput)
	if err != nil {
		t.Fatalf("BLS12G1MSM error: %v", err)
	}

	// Compare with 5*G.
	scalar5 := make([]byte, 32)
	scalar5[31] = 5
	mulInput := append(genEnc, scalar5...)
	mulResult, err := BLS12G1Mul(mulInput)
	if err != nil {
		t.Fatalf("BLS12G1Mul error: %v", err)
	}

	if hex.EncodeToString(msmResult) != hex.EncodeToString(mulResult) {
		t.Error("MSM(G,2; G,3) should equal 5*G")
	}
}

// --- G2 precompile tests ---

func TestBLS12G2AddInfinityPlusInfinity(t *testing.T) {
	input := make([]byte, 512)
	result, err := BLS12G2Add(input)
	if err != nil {
		t.Fatalf("BLS12G2Add error: %v", err)
	}
	for _, b := range result {
		if b != 0 {
			t.Error("inf + inf should be inf")
			break
		}
	}
}

func TestBLS12G2AddGeneratorPlusInfinity(t *testing.T) {
	gen := BlsG2Generator()
	genEnc := encodeG2(gen)
	infEnc := make([]byte, 256)

	input := append(genEnc, infEnc...)
	result, err := BLS12G2Add(input)
	if err != nil {
		t.Fatalf("BLS12G2Add error: %v", err)
	}

	if hex.EncodeToString(result) != hex.EncodeToString(genEnc) {
		t.Error("G2 + inf should equal G2")
	}
}

func TestBLS12G2AddGeneratorPlusGenerator(t *testing.T) {
	gen := BlsG2Generator()
	genEnc := encodeG2(gen)

	input := append(genEnc, genEnc...)
	result, err := BLS12G2Add(input)
	if err != nil {
		t.Fatalf("BLS12G2Add error: %v", err)
	}

	// Decode and verify 2*G2 is on the curve.
	p, err := decodeG2(result)
	if err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	x, y := p.blsG2ToAffine()
	if !blsG2IsOnCurve(x, y) {
		t.Error("G2 + G2 result is not on the curve")
	}
}

func TestBLS12G2MulByOne(t *testing.T) {
	gen := BlsG2Generator()
	genEnc := encodeG2(gen)

	scalar := make([]byte, 32)
	scalar[31] = 1

	input := append(genEnc, scalar...)
	result, err := BLS12G2Mul(input)
	if err != nil {
		t.Fatalf("BLS12G2Mul error: %v", err)
	}

	if hex.EncodeToString(result) != hex.EncodeToString(genEnc) {
		t.Error("1 * G2 should equal G2")
	}
}

func TestBLS12G2MulByOrder(t *testing.T) {
	gen := BlsG2Generator()
	genEnc := encodeG2(gen)

	rBytes := blsR.Bytes()
	scalar := make([]byte, 32)
	copy(scalar[32-len(rBytes):], rBytes)

	input := append(genEnc, scalar...)
	result, err := BLS12G2Mul(input)
	if err != nil {
		t.Fatalf("BLS12G2Mul error: %v", err)
	}

	for _, b := range result {
		if b != 0 {
			t.Error("[r] * G2 should be infinity")
			break
		}
	}
}

// --- Invalid input tests ---

func TestBLS12G1AddInvalidLength(t *testing.T) {
	_, err := BLS12G1Add(make([]byte, 100))
	if err == nil {
		t.Error("expected error for invalid input length")
	}
}

func TestBLS12G1MulInvalidLength(t *testing.T) {
	_, err := BLS12G1Mul(make([]byte, 100))
	if err == nil {
		t.Error("expected error for invalid input length")
	}
}

func TestBLS12G1MSMInvalidLength(t *testing.T) {
	_, err := BLS12G1MSM(make([]byte, 100))
	if err == nil {
		t.Error("expected error for invalid input length")
	}
}

func TestBLS12G2AddInvalidLength(t *testing.T) {
	_, err := BLS12G2Add(make([]byte, 100))
	if err == nil {
		t.Error("expected error for invalid input length")
	}
}

func TestBLS12G2MulInvalidLength(t *testing.T) {
	_, err := BLS12G2Mul(make([]byte, 100))
	if err == nil {
		t.Error("expected error for invalid input length")
	}
}

func TestBLS12G2MSMInvalidLength(t *testing.T) {
	_, err := BLS12G2MSM(make([]byte, 100))
	if err == nil {
		t.Error("expected error for invalid input length")
	}
}

func TestBLS12PairingInvalidLength(t *testing.T) {
	_, err := BLS12Pairing(make([]byte, 100))
	if err == nil {
		t.Error("expected error for invalid input length")
	}
}

func TestBLS12MapFpToG1InvalidLength(t *testing.T) {
	_, err := BLS12MapFpToG1(make([]byte, 32))
	if err == nil {
		t.Error("expected error for invalid input length")
	}
}

func TestBLS12MapFp2ToG2InvalidLength(t *testing.T) {
	_, err := BLS12MapFp2ToG2(make([]byte, 64))
	if err == nil {
		t.Error("expected error for invalid input length")
	}
}

// --- Pairing tests ---

func TestBLS12PairingAllInfinity(t *testing.T) {
	// One pair, both infinity -> should return 1.
	input := make([]byte, 384)
	result, err := BLS12Pairing(input)
	if err != nil {
		t.Fatalf("BLS12Pairing error: %v", err)
	}
	if len(result) != 32 {
		t.Fatalf("result length = %d, want 32", len(result))
	}
	if result[31] != 1 {
		t.Error("pairing with all infinity should return 1")
	}
}

// TestBLS12PairingBilinearity tests e(a*G1, G2) * e(-a*G1, G2) == 1.
// This verifies the pairing implementation has correct bilinearity.
func TestBLS12PairingBilinearity(t *testing.T) {
	a := big.NewInt(7)

	// Compute a*G1 and -a*G1.
	aG1 := blsG1ScalarMul(BlsG1Generator(), a)
	negAG1 := blsG1Neg(aG1)
	g2 := BlsG2Generator()

	// e(a*G1, G2) * e(-a*G1, G2) should equal 1 in GT.
	result := blsMultiPairing(
		[]*BlsG1Point{aG1, negAG1},
		[]*BlsG2Point{g2, g2},
	)
	if !result {
		t.Fatal("e(aG1, G2) * e(-aG1, G2) should equal 1")
	}
}

// TestBLS12PairingSingleNonTrivial tests that e(G1, G2) is not the identity.
func TestBLS12PairingSingleNonTrivial(t *testing.T) {
	g1 := BlsG1Generator()
	g2 := BlsG2Generator()

	// e(G1, G2) should NOT be 1 (it's a generator of GT).
	result := blsMultiPairing(
		[]*BlsG1Point{g1},
		[]*BlsG2Point{g2},
	)
	if result {
		t.Fatal("e(G1, G2) should not be the identity")
	}
}

// TestBLS12PairingSwapScalar tests e(a*G1, b*G2) == e(b*G1, a*G2).
func TestBLS12PairingSwapScalar(t *testing.T) {
	a := big.NewInt(3)
	b := big.NewInt(5)

	aG1 := blsG1ScalarMul(BlsG1Generator(), a)
	bG2 := blsG2ScalarMul(BlsG2Generator(), b)
	bG1 := blsG1ScalarMul(BlsG1Generator(), b)
	aG2 := blsG2ScalarMul(BlsG2Generator(), a)

	// e(aG1, bG2) * e(-bG1, aG2) should equal 1 if bilinear.
	// Because e(aG1, bG2) = e(G1,G2)^(ab) = e(bG1, aG2).
	negBG1 := blsG1Neg(bG1)
	result := blsMultiPairing(
		[]*BlsG1Point{aG1, negBG1},
		[]*BlsG2Point{bG2, aG2},
	)
	if !result {
		t.Fatal("e(aG1, bG2) * e(-bG1, aG2) should equal 1 (bilinearity)")
	}
}

// --- Map-to-curve tests ---

func TestBLS12MapFpToG1Zero(t *testing.T) {
	// Map 0 -> should produce a valid G1 point.
	input := make([]byte, 64) // zero
	result, err := BLS12MapFpToG1(input)
	if err != nil {
		t.Fatalf("BLS12MapFpToG1(0) error: %v", err)
	}
	if len(result) != 128 {
		t.Fatalf("result length = %d, want 128", len(result))
	}

	// Decode and verify on curve.
	p, err := decodeG1(result)
	if err != nil {
		// The mapped point might not be in the subgroup before clearing cofactor,
		// but after clearing cofactor it should be.
		t.Fatalf("decodeG1 error: %v", err)
	}
	x, y := p.blsG1ToAffine()
	if !p.blsG1IsInfinity() && !blsG1IsOnCurve(x, y) {
		t.Error("mapped G1 point is not on the curve")
	}
}

func TestBLS12MapFpToG1One(t *testing.T) {
	// Map 1 -> should produce a valid G1 point.
	input := make([]byte, 64)
	input[63] = 1
	result, err := BLS12MapFpToG1(input)
	if err != nil {
		t.Fatalf("BLS12MapFpToG1(1) error: %v", err)
	}
	if len(result) != 128 {
		t.Fatalf("result length = %d, want 128", len(result))
	}

	p, err := decodeG1(result)
	if err != nil {
		t.Fatalf("decodeG1 error: %v", err)
	}
	x, y := p.blsG1ToAffine()
	if !p.blsG1IsInfinity() && !blsG1IsOnCurve(x, y) {
		t.Error("mapped G1 point is not on the curve")
	}
}

func TestBLS12MapFpToG1InvalidField(t *testing.T) {
	// Value >= p should fail.
	input := make([]byte, 64)
	pBytes := blsP.Bytes()
	copy(input[64-len(pBytes):], pBytes)
	_, err := BLS12MapFpToG1(input)
	if err == nil {
		t.Error("expected error for field element >= p")
	}
}

func TestBLS12MapFp2ToG2Zero(t *testing.T) {
	// Map (0, 0) -> should produce a valid G2 point.
	input := make([]byte, 128)
	result, err := BLS12MapFp2ToG2(input)
	if err != nil {
		t.Fatalf("BLS12MapFp2ToG2(0) error: %v", err)
	}
	if len(result) != 256 {
		t.Fatalf("result length = %d, want 256", len(result))
	}

	p, err := decodeG2(result)
	if err != nil {
		t.Fatalf("decodeG2 error: %v", err)
	}
	x, y := p.blsG2ToAffine()
	if !p.blsG2IsInfinity() && !blsG2IsOnCurve(x, y) {
		t.Error("mapped G2 point is not on the curve")
	}
}
