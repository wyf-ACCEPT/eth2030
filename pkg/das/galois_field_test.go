package das

import (
	"testing"
)

// TestGFAddCommutativity verifies a + b = b + a.
func TestGFAddCommutativity(t *testing.T) {
	initGFTables()
	cases := [][2]GF2_16{{0, 0}, {0, 1}, {1, 1}, {0x1234, 0x5678}, {0xFFFF, 0xFFFF}}
	for _, c := range cases {
		if GFAdd(c[0], c[1]) != GFAdd(c[1], c[0]) {
			t.Errorf("GFAdd not commutative: %d, %d", c[0], c[1])
		}
	}
}

// TestGFAddIdentity verifies a + 0 = a.
func TestGFAddIdentity(t *testing.T) {
	for _, a := range []GF2_16{0, 1, 42, 0xFFFF, 0x8000} {
		if GFAdd(a, 0) != a {
			t.Errorf("GFAdd(%d, 0) = %d, want %d", a, GFAdd(a, 0), a)
		}
	}
}

// TestGFAddSelfInverse verifies a + a = 0 (characteristic 2).
func TestGFAddSelfInverse(t *testing.T) {
	for _, a := range []GF2_16{0, 1, 42, 0xFFFF, 0xABCD} {
		if GFAdd(a, a) != 0 {
			t.Errorf("GFAdd(%d, %d) = %d, want 0", a, a, GFAdd(a, a))
		}
	}
}

// TestGFMulCommutativity verifies a * b = b * a.
func TestGFMulCommutativity(t *testing.T) {
	initGFTables()
	cases := [][2]GF2_16{{0, 5}, {1, 7}, {100, 200}, {0x1234, 0x5678}}
	for _, c := range cases {
		if GFMul(c[0], c[1]) != GFMul(c[1], c[0]) {
			t.Errorf("GFMul not commutative: %d, %d", c[0], c[1])
		}
	}
}

// TestGFMulIdentity verifies a * 1 = a.
func TestGFMulIdentity(t *testing.T) {
	initGFTables()
	for _, a := range []GF2_16{0, 1, 42, 0xFFFF, 0x8000} {
		if GFMul(a, 1) != a {
			t.Errorf("GFMul(%d, 1) = %d, want %d", a, GFMul(a, 1), a)
		}
	}
}

// TestGFMulZero verifies a * 0 = 0.
func TestGFMulZero(t *testing.T) {
	initGFTables()
	for _, a := range []GF2_16{0, 1, 42, 0xFFFF} {
		if GFMul(a, 0) != 0 {
			t.Errorf("GFMul(%d, 0) = %d, want 0", a, GFMul(a, 0))
		}
	}
}

// TestGFMulAssociativity verifies (a * b) * c = a * (b * c).
func TestGFMulAssociativity(t *testing.T) {
	initGFTables()
	a, b, c := GF2_16(123), GF2_16(456), GF2_16(789)
	lhs := GFMul(GFMul(a, b), c)
	rhs := GFMul(a, GFMul(b, c))
	if lhs != rhs {
		t.Errorf("associativity failed: (%d*%d)*%d = %d, %d*(%d*%d) = %d",
			a, b, c, lhs, a, b, c, rhs)
	}
}

// TestGFDistributivity verifies a * (b + c) = a*b + a*c.
func TestGFDistributivity(t *testing.T) {
	initGFTables()
	a, b, c := GF2_16(37), GF2_16(99), GF2_16(250)
	lhs := GFMul(a, GFAdd(b, c))
	rhs := GFAdd(GFMul(a, b), GFMul(a, c))
	if lhs != rhs {
		t.Errorf("distributivity failed: %d*(%d+%d) = %d, %d*%d + %d*%d = %d",
			a, b, c, lhs, a, b, a, c, rhs)
	}
}

// TestGFInverse verifies a * a^{-1} = 1 for all non-zero elements (sampled).
func TestGFInverse(t *testing.T) {
	initGFTables()
	// Test a range of values including boundaries.
	values := []GF2_16{1, 2, 3, 42, 255, 256, 1000, 0x7FFF, 0xFFFE, 0xFFFF}
	for _, a := range values {
		inv := GFInverse(a)
		prod := GFMul(a, inv)
		if prod != 1 {
			t.Errorf("GFInverse(%d) = %d, but %d * %d = %d, want 1",
				a, inv, a, inv, prod)
		}
	}
}

// TestGFInverseZeroPanics verifies that GFInverse(0) panics.
func TestGFInverseZeroPanics(t *testing.T) {
	initGFTables()
	defer func() {
		if r := recover(); r == nil {
			t.Error("GFInverse(0) did not panic")
		}
	}()
	GFInverse(0)
}

// TestGFDivision verifies a / b = a * b^{-1} and (a/b)*b = a.
func TestGFDivision(t *testing.T) {
	initGFTables()
	cases := [][2]GF2_16{{100, 7}, {0xFFFF, 1}, {1, 0xFFFF}, {42, 42}}
	for _, c := range cases {
		a, b := c[0], c[1]
		q := GFDiv(a, b)
		// Verify q * b = a.
		check := GFMul(q, b)
		if check != a {
			t.Errorf("GFDiv(%d, %d) = %d, but %d * %d = %d, want %d",
				a, b, q, q, b, check, a)
		}
	}
}

// TestGFDivZeroPanics verifies that GFDiv(a, 0) panics.
func TestGFDivZeroPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("GFDiv(1, 0) did not panic")
		}
	}()
	GFDiv(1, 0)
}

// TestGFPow verifies exponentiation.
func TestGFPow(t *testing.T) {
	initGFTables()

	// a^0 = 1 for any a.
	if GFPow(42, 0) != 1 {
		t.Error("GFPow(42, 0) != 1")
	}
	// 0^n = 0 for n > 0.
	if GFPow(0, 5) != 0 {
		t.Error("GFPow(0, 5) != 0")
	}
	// a^1 = a.
	if GFPow(7, 1) != 7 {
		t.Errorf("GFPow(7, 1) = %d, want 7", GFPow(7, 1))
	}
	// Verify a^3 = a * a * a.
	a := GF2_16(123)
	expected := GFMul(GFMul(a, a), a)
	if GFPow(a, 3) != expected {
		t.Errorf("GFPow(%d, 3) = %d, want %d", a, GFPow(a, 3), expected)
	}
	// a^{q-1} = 1 for non-zero a (Fermat's little theorem in GF(2^16)).
	if GFPow(a, gfOrder) != 1 {
		t.Errorf("GFPow(%d, %d) = %d, want 1 (Fermat)", a, gfOrder, GFPow(a, gfOrder))
	}
}

// TestGFPowNegative verifies negative exponents.
func TestGFPowNegative(t *testing.T) {
	initGFTables()
	a := GF2_16(7)
	// a^{-1} should equal GFInverse(a).
	if GFPow(a, -1) != GFInverse(a) {
		t.Errorf("GFPow(%d, -1) = %d, want %d", a, GFPow(a, -1), GFInverse(a))
	}
}

// TestGFPolyEval verifies polynomial evaluation.
func TestGFPolyEval(t *testing.T) {
	initGFTables()

	// p(x) = 3 + 5x + 7x^2, evaluate at x = 2.
	// p(2) = 3 ^ (5*2) ^ (7*4) = 3 ^ 10 ^ 28 (XOR in GF(2^16)).
	coeffs := []GF2_16{3, 5, 7}
	x := GF2_16(2)
	result := GFPolyEval(coeffs, x)

	// Manual: 3 + 5*2 + 7*4 in GF(2^16).
	expected := GFAdd(GFAdd(3, GFMul(5, 2)), GFMul(7, GFMul(2, 2)))
	if result != expected {
		t.Errorf("GFPolyEval([3,5,7], 2) = %d, want %d", result, expected)
	}

	// Empty polynomial evaluates to 0.
	if GFPolyEval(nil, 5) != 0 {
		t.Error("GFPolyEval(nil, 5) != 0")
	}

	// Constant polynomial.
	if GFPolyEval([]GF2_16{42}, 100) != 42 {
		t.Error("constant polynomial should return the constant")
	}
}

// TestGFPolyMul verifies polynomial multiplication.
func TestGFPolyMul(t *testing.T) {
	initGFTables()

	// (1 + x) * (1 + x) = 1 + 0*x + x^2 (in char 2, 2x = 0).
	p := []GF2_16{1, 1}
	result := GFPolyMul(p, p)
	if len(result) != 3 {
		t.Fatalf("degree mismatch: got len %d, want 3", len(result))
	}
	// result should be [1, 0, 1] in GF(2^16).
	if result[0] != 1 || result[1] != 0 || result[2] != 1 {
		t.Errorf("(1+x)^2 = [%d, %d, %d], want [1, 0, 1]", result[0], result[1], result[2])
	}

	// Nil input returns nil.
	if GFPolyMul(nil, p) != nil {
		t.Error("GFPolyMul(nil, p) should return nil")
	}
}

// TestGFPolyAdd verifies polynomial addition.
func TestGFPolyAdd(t *testing.T) {
	initGFTables()

	p1 := []GF2_16{1, 2, 3}
	p2 := []GF2_16{4, 5}
	result := GFPolyAdd(p1, p2)
	if len(result) != 3 {
		t.Fatalf("expected length 3, got %d", len(result))
	}
	if result[0] != GFAdd(1, 4) || result[1] != GFAdd(2, 5) || result[2] != 3 {
		t.Errorf("GFPolyAdd result mismatch: [%d, %d, %d]", result[0], result[1], result[2])
	}
}

// TestGFInterpolate verifies Lagrange interpolation roundtrip.
func TestGFInterpolate(t *testing.T) {
	initGFTables()

	// Define a polynomial p(x) = 5 + 3x + 7x^2.
	origCoeffs := []GF2_16{5, 3, 7}
	n := len(origCoeffs)

	// Evaluate at n distinct points.
	xs := make([]GF2_16, n)
	ys := make([]GF2_16, n)
	for i := 0; i < n; i++ {
		xs[i] = GF2_16(i + 1) // Use 1, 2, 3 as evaluation points.
		ys[i] = GFPolyEval(origCoeffs, xs[i])
	}

	// Interpolate back.
	recovered := GFInterpolate(xs, ys)
	if len(recovered) != n {
		t.Fatalf("interpolated polynomial length = %d, want %d", len(recovered), n)
	}
	for i := 0; i < n; i++ {
		if recovered[i] != origCoeffs[i] {
			t.Errorf("recovered[%d] = %d, want %d", i, recovered[i], origCoeffs[i])
		}
	}
}

// TestGFInterpolateLarger tests interpolation with more points.
func TestGFInterpolateLarger(t *testing.T) {
	initGFTables()

	// Degree 5 polynomial.
	origCoeffs := []GF2_16{10, 20, 30, 40, 50, 60}
	n := len(origCoeffs)

	xs := make([]GF2_16, n)
	ys := make([]GF2_16, n)
	for i := 0; i < n; i++ {
		xs[i] = GF2_16(i + 10) // Use 10..15 as evaluation points.
		ys[i] = GFPolyEval(origCoeffs, xs[i])
	}

	recovered := GFInterpolate(xs, ys)
	if len(recovered) != n {
		t.Fatalf("length mismatch: got %d, want %d", len(recovered), n)
	}

	// Verify: the recovered polynomial should produce the same evaluations.
	for i := 0; i < n; i++ {
		val := GFPolyEval(recovered, xs[i])
		if val != ys[i] {
			t.Errorf("recovered poly eval at x=%d: got %d, want %d", xs[i], val, ys[i])
		}
	}
}

// TestGFPolyFromRoots verifies polynomial construction from roots.
func TestGFPolyFromRoots(t *testing.T) {
	initGFTables()

	roots := []GF2_16{3, 7, 11}
	poly := GFPolyFromRoots(roots)

	// The polynomial should evaluate to 0 at each root.
	for _, r := range roots {
		val := GFPolyEval(poly, r)
		if val != 0 {
			t.Errorf("poly(%d) = %d, want 0", r, val)
		}
	}

	// Degree should be len(roots).
	if len(poly) != len(roots)+1 {
		t.Errorf("degree = %d, want %d", len(poly)-1, len(roots))
	}

	// Leading coefficient should be 1 (monic).
	if poly[len(poly)-1] != 1 {
		t.Errorf("leading coefficient = %d, want 1", poly[len(poly)-1])
	}
}

// TestGFExpLog verifies exp/log roundtrip.
func TestGFExpLog(t *testing.T) {
	initGFTables()

	for i := 0; i < gfOrder; i++ {
		a := GFExp(i)
		if a == 0 {
			t.Errorf("GFExp(%d) = 0, generator powers should never be zero", i)
			continue
		}
		logA := GFLog(a)
		if logA != i {
			t.Errorf("GFLog(GFExp(%d)) = %d", i, logA)
		}
	}
}

// TestGFVandermondeRow verifies Vandermonde row construction.
func TestGFVandermondeRow(t *testing.T) {
	initGFTables()
	x := GF2_16(5)
	row := GFVandermondeRow(x, 4)
	if len(row) != 4 {
		t.Fatalf("row length = %d, want 4", len(row))
	}
	if row[0] != 1 {
		t.Errorf("row[0] = %d, want 1", row[0])
	}
	if row[1] != x {
		t.Errorf("row[1] = %d, want %d", row[1], x)
	}
	if row[2] != GFMul(x, x) {
		t.Errorf("row[2] = %d, want %d", row[2], GFMul(x, x))
	}
	if row[3] != GFMul(GFMul(x, x), x) {
		t.Errorf("row[3] = %d, want %d", row[3], GFMul(GFMul(x, x), x))
	}
}

// TestGFTableConsistency verifies the log/exp tables are consistent.
func TestGFTableConsistency(t *testing.T) {
	initGFTables()

	// Every non-zero element should appear exactly once in expTable[0..gfOrder-1].
	seen := make(map[uint16]bool)
	for i := 0; i < gfOrder; i++ {
		v := gfExpTable[i]
		if v == 0 {
			t.Errorf("expTable[%d] = 0, should never be zero", i)
		}
		if seen[v] {
			t.Errorf("expTable[%d] = %d is a duplicate", i, v)
		}
		seen[v] = true
	}
	if len(seen) != gfOrder {
		t.Errorf("expected %d unique elements, got %d", gfOrder, len(seen))
	}
}
