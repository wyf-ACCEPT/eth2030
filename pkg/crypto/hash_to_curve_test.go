package crypto

import (
	"math/big"
	"testing"
)

func TestExpandMessageXMD(t *testing.T) {
	dst := []byte("QUUX-V01-CS02-with-expander-SHA256-128")
	msg := []byte("abc")

	// Expand to 32 bytes.
	out, err := expandMessageXMD(msg, dst, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(out))
	}

	// Expand to 128 bytes.
	out128, err := expandMessageXMD(msg, dst, 128)
	if err != nil {
		t.Fatal(err)
	}
	if len(out128) != 128 {
		t.Fatalf("expected 128 bytes, got %d", len(out128))
	}

	// Same input must produce same output (deterministic).
	out2, _ := expandMessageXMD(msg, dst, 32)
	for i := range out {
		if out[i] != out2[i] {
			t.Fatalf("non-deterministic at byte %d", i)
		}
	}

	// Different messages must produce different outputs.
	outDiff, _ := expandMessageXMD([]byte("def"), dst, 32)
	same := true
	for i := range out {
		if out[i] != outDiff[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("different messages produced same expansion")
	}
}

func TestExpandMessageXMDEmpty(t *testing.T) {
	dst := []byte("test-dst")
	out, err := expandMessageXMD([]byte{}, dst, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(out))
	}
}

func TestExpandMessageXMDLongDST(t *testing.T) {
	dst := make([]byte, 256)
	_, err := expandMessageXMD([]byte("test"), dst, 32)
	if err == nil {
		t.Fatal("expected error for DST > 255 bytes")
	}
}

func TestExpandMessageXMDVaryLength(t *testing.T) {
	dst := []byte("test-lengths")
	msg := []byte("fixed message")

	// Different output lengths should produce different results.
	out32, _ := expandMessageXMD(msg, dst, 32)
	out48, _ := expandMessageXMD(msg, dst, 48)

	// First 32 bytes should differ (different length in the hash).
	same := true
	for i := 0; i < 32; i++ {
		if out32[i] != out48[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("different output lengths produced same prefix")
	}
}

func TestHashToFieldG1(t *testing.T) {
	dst := []byte("BLS12381G1_XMD:SHA-256_SSWU_RO_")
	msg := []byte("test message")

	u0, u1, err := hashToFieldG1(msg, dst)
	if err != nil {
		t.Fatal(err)
	}

	// Both elements must be in range [0, p).
	if u0.Cmp(blsP) >= 0 {
		t.Fatal("u0 >= p")
	}
	if u1.Cmp(blsP) >= 0 {
		t.Fatal("u1 >= p")
	}

	// Non-zero for typical inputs.
	if u0.Sign() == 0 && u1.Sign() == 0 {
		t.Fatal("both field elements are zero (extremely unlikely)")
	}

	// Deterministic.
	u0b, u1b, _ := hashToFieldG1(msg, dst)
	if u0.Cmp(u0b) != 0 || u1.Cmp(u1b) != 0 {
		t.Fatal("non-deterministic hash_to_field")
	}
}

func TestHashToFieldG1DifferentInputs(t *testing.T) {
	dst := []byte("test")
	u0a, u1a, _ := hashToFieldG1([]byte("msg1"), dst)
	u0b, u1b, _ := hashToFieldG1([]byte("msg2"), dst)

	if u0a.Cmp(u0b) == 0 && u1a.Cmp(u1b) == 0 {
		t.Fatal("different messages produced same field elements")
	}
}

func TestSimplifiedSWU(t *testing.T) {
	// Test that SimplifiedSWU produces valid points on E'.
	inputs := []*big.Int{
		big.NewInt(1),
		big.NewInt(42),
		big.NewInt(12345678),
	}

	for i, u := range inputs {
		x, y := SimplifiedSWU(u)
		if x.Sign() == 0 && y.Sign() == 0 {
			continue // degenerate case
		}
		if !IsOnIsogenousCurve(x, y) {
			t.Errorf("test %d: SimplifiedSWU produced off-curve point on E'", i)
		}
	}
}

func TestSimplifiedSWUZero(t *testing.T) {
	x, y := SimplifiedSWU(big.NewInt(0))
	if x.Sign() == 0 && y.Sign() == 0 {
		return // valid degenerate case
	}
	if !IsOnIsogenousCurve(x, y) {
		t.Fatal("SimplifiedSWU(0) produced off-curve point on E'")
	}
}

func TestSimplifiedSWUSignAlignment(t *testing.T) {
	// For u with sgn0(u) == 1, the result y should also have sgn0(y) == 1.
	u := big.NewInt(3) // odd => sgn0 = 1
	_, y := SimplifiedSWU(u)
	if y.Sign() != 0 && blsFpSgn0(u) != blsFpSgn0(y) {
		t.Fatal("sign alignment failed")
	}
}

func TestHashToCurveG1Basic(t *testing.T) {
	dst := []byte("BLS12381G1_XMD:SHA-256_SSWU_RO_")
	msg := []byte("hello world")

	p, err := HashToCurveG1(msg, dst)
	if err != nil {
		t.Fatal(err)
	}

	// Result must be on curve.
	if !p.blsG1IsInfinity() {
		x, y := p.blsG1ToAffine()
		if !blsG1IsOnCurve(x, y) {
			t.Fatal("HashToCurveG1 produced off-curve point")
		}

		// Result must be in the subgroup (cofactor-cleared).
		if !blsG1InSubgroup(p) {
			t.Fatal("HashToCurveG1 result not in subgroup")
		}
	}
}

func TestHashToCurveG1Deterministic(t *testing.T) {
	dst := []byte("test-suite")
	msg := []byte("deterministic check")

	p1, err := HashToCurveG1(msg, dst)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := HashToCurveG1(msg, dst)
	if err != nil {
		t.Fatal(err)
	}

	x1, y1 := p1.blsG1ToAffine()
	x2, y2 := p2.blsG1ToAffine()
	if x1.Cmp(x2) != 0 || y1.Cmp(y2) != 0 {
		t.Fatal("HashToCurveG1 is non-deterministic")
	}
}

func TestHashToCurveG1DifferentMsgs(t *testing.T) {
	dst := []byte("collision-test")
	p1, _ := HashToCurveG1([]byte("msg1"), dst)
	p2, _ := HashToCurveG1([]byte("msg2"), dst)

	x1, y1 := p1.blsG1ToAffine()
	x2, y2 := p2.blsG1ToAffine()
	if x1.Cmp(x2) == 0 && y1.Cmp(y2) == 0 {
		t.Fatal("different messages produced same point")
	}
}

func TestHashToCurveG1DifferentDSTs(t *testing.T) {
	msg := []byte("same message")
	p1, _ := HashToCurveG1(msg, []byte("DST-A"))
	p2, _ := HashToCurveG1(msg, []byte("DST-B"))

	x1, y1 := p1.blsG1ToAffine()
	x2, y2 := p2.blsG1ToAffine()
	if x1.Cmp(x2) == 0 && y1.Cmp(y2) == 0 {
		t.Fatal("different DSTs produced same point")
	}
}

func TestEncodeToG1(t *testing.T) {
	dst := []byte("BLS12381G1_XMD:SHA-256_SSWU_NU_")
	msg := []byte("encode test")

	p, err := EncodeToG1(msg, dst)
	if err != nil {
		t.Fatal(err)
	}

	if !p.blsG1IsInfinity() {
		x, y := p.blsG1ToAffine()
		if !blsG1IsOnCurve(x, y) {
			t.Fatal("EncodeToG1 produced off-curve point")
		}
		if !blsG1InSubgroup(p) {
			t.Fatal("EncodeToG1 result not in subgroup")
		}
	}
}

func TestHashToCurveG1DSTTooLong(t *testing.T) {
	longDST := make([]byte, 256)
	_, err := HashToCurveG1([]byte("test"), longDST)
	if err == nil {
		t.Fatal("expected error for DST > 255 bytes")
	}
}

func TestClearCofactorG1(t *testing.T) {
	gen := BlsG1Generator()
	cleared := clearCofactorG1(gen)

	if !cleared.blsG1IsInfinity() {
		if !blsG1InSubgroup(cleared) {
			t.Fatal("clearCofactorG1 result not in subgroup")
		}
	}
}

func TestValidateDST(t *testing.T) {
	if err := ValidateDST([]byte("ok")); err != nil {
		t.Fatal("valid DST rejected:", err)
	}
	if err := ValidateDST([]byte{}); err == nil {
		t.Fatal("empty DST accepted")
	}
	if err := ValidateDST(make([]byte, 256)); err == nil {
		t.Fatal("DST > 255 accepted")
	}
	if err := ValidateDST(make([]byte, 255)); err != nil {
		t.Fatal("DST of exactly 255 bytes rejected:", err)
	}
}

func TestEvalPoly(t *testing.T) {
	// f(x) = 1 + 2x + 3x^2, evaluate at x=2: 1 + 4 + 12 = 17
	coeffs := []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3)}
	result := evalPoly(big.NewInt(2), coeffs)
	expected := big.NewInt(17)
	result.Mod(result, blsP)
	if result.Cmp(expected) != 0 {
		t.Fatalf("evalPoly: expected %s, got %s", expected, result)
	}
}

func TestEvalPolyEmpty(t *testing.T) {
	result := evalPoly(big.NewInt(5), nil)
	if result.Sign() != 0 {
		t.Fatal("evalPoly with nil coeffs should return 0")
	}
}

func TestEvalPolyConstant(t *testing.T) {
	// f(x) = 7, evaluate at any point.
	coeffs := []*big.Int{big.NewInt(7)}
	result := evalPoly(big.NewInt(999), coeffs)
	if result.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("expected 7, got %s", result)
	}
}

func TestHashToCurveG1SubgroupMultiple(t *testing.T) {
	dst := []byte("subgroup-test-suite")
	messages := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for _, msg := range messages {
		p, err := HashToCurveG1([]byte(msg), dst)
		if err != nil {
			t.Fatalf("HashToCurveG1(%q): %v", msg, err)
		}
		if p.blsG1IsInfinity() {
			continue
		}
		if !blsG1InSubgroup(p) {
			t.Fatalf("HashToCurveG1(%q): result not in subgroup", msg)
		}
	}
}

func TestHashToCurveG1EmptyMessage(t *testing.T) {
	dst := []byte("empty-msg-test")
	p, err := HashToCurveG1([]byte{}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if !p.blsG1IsInfinity() {
		x, y := p.blsG1ToAffine()
		if !blsG1IsOnCurve(x, y) {
			t.Fatal("empty message produced off-curve point")
		}
	}
}

func TestIsOnIsogenousCurve(t *testing.T) {
	// A known point on E' computed by the SWU map.
	x, y := SimplifiedSWU(big.NewInt(7))
	if x.Sign() == 0 && y.Sign() == 0 {
		t.Skip("degenerate SWU output")
	}
	if !IsOnIsogenousCurve(x, y) {
		t.Fatal("SWU output not on isogenous curve")
	}

	// Tampered point should not be on curve.
	if IsOnIsogenousCurve(blsFpAdd(x, big.NewInt(1)), y) {
		t.Fatal("tampered point should not be on curve")
	}
}

func TestExpandMessageXMDMultipleLengths(t *testing.T) {
	dst := []byte("multi-len-test")
	msg := []byte("test")

	for _, length := range []int{16, 32, 48, 64, 96, 128} {
		out, err := expandMessageXMD(msg, dst, length)
		if err != nil {
			t.Fatalf("length %d: %v", length, err)
		}
		if len(out) != length {
			t.Fatalf("length %d: got %d bytes", length, len(out))
		}
	}
}
