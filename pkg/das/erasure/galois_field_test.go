package erasure

import (
	"testing"
)

func TestGF256AddCommutativity(t *testing.T) {
	cases := [][2]GF256{{0, 0}, {0, 1}, {1, 1}, {0x12, 0x34}, {0xFF, 0xFF}}
	for _, c := range cases {
		if GF256Add(c[0], c[1]) != GF256Add(c[1], c[0]) {
			t.Errorf("GF256Add not commutative: %d, %d", c[0], c[1])
		}
	}
}

func TestGF256AddIdentity(t *testing.T) {
	for _, a := range []GF256{0, 1, 42, 0xFF, 0x80} {
		if GF256Add(a, 0) != a {
			t.Errorf("GF256Add(%d, 0) = %d, want %d", a, GF256Add(a, 0), a)
		}
	}
}

func TestGF256AddSelfInverse(t *testing.T) {
	for _, a := range []GF256{0, 1, 42, 0xFF, 0xAB} {
		if GF256Add(a, a) != 0 {
			t.Errorf("GF256Add(%d, %d) = %d, want 0", a, a, GF256Add(a, a))
		}
	}
}

func TestGF256SubEqAdd(t *testing.T) {
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b++ {
			if GF256Sub(GF256(a), GF256(b)) != GF256Add(GF256(a), GF256(b)) {
				t.Fatalf("Sub != Add at (%d, %d)", a, b)
			}
		}
	}
}

func TestGF256MulCommutativity(t *testing.T) {
	initGF256Tables()
	cases := [][2]GF256{{0, 5}, {1, 7}, {100, 200}, {0x12, 0x78}}
	for _, c := range cases {
		if GF256Mul(c[0], c[1]) != GF256Mul(c[1], c[0]) {
			t.Errorf("GF256Mul not commutative: %d, %d", c[0], c[1])
		}
	}
}

func TestGF256MulIdentity(t *testing.T) {
	initGF256Tables()
	for a := 0; a < 256; a++ {
		if GF256Mul(GF256(a), 1) != GF256(a) {
			t.Errorf("GF256Mul(%d, 1) = %d, want %d", a, GF256Mul(GF256(a), 1), a)
		}
	}
}

func TestGF256MulZero(t *testing.T) {
	initGF256Tables()
	for a := 0; a < 256; a++ {
		if GF256Mul(GF256(a), 0) != 0 {
			t.Errorf("GF256Mul(%d, 0) = %d, want 0", a, GF256Mul(GF256(a), 0))
		}
	}
}

func TestGF256MulAssociativity(t *testing.T) {
	initGF256Tables()
	triples := [][3]GF256{{3, 7, 11}, {100, 200, 50}, {0xFF, 0xFE, 0xFD}}
	for _, tr := range triples {
		a, b, c := tr[0], tr[1], tr[2]
		lhs := GF256Mul(GF256Mul(a, b), c)
		rhs := GF256Mul(a, GF256Mul(b, c))
		if lhs != rhs {
			t.Errorf("associativity failed: (%d*%d)*%d = %d, %d*(%d*%d) = %d",
				a, b, c, lhs, a, b, c, rhs)
		}
	}
}

func TestGF256Distributivity(t *testing.T) {
	initGF256Tables()
	a, b, c := GF256(37), GF256(99), GF256(250)
	lhs := GF256Mul(a, GF256Add(b, c))
	rhs := GF256Add(GF256Mul(a, b), GF256Mul(a, c))
	if lhs != rhs {
		t.Errorf("distributivity failed: %d*(%d+%d)=%d, %d*%d+%d*%d=%d",
			a, b, c, lhs, a, b, a, c, rhs)
	}
}

func TestGF256InverseAll(t *testing.T) {
	initGF256Tables()
	for a := 1; a < 256; a++ {
		inv := GF256Inverse(GF256(a))
		prod := GF256Mul(GF256(a), inv)
		if prod != 1 {
			t.Fatalf("GF256Inverse(%d) = %d, but %d * %d = %d, want 1",
				a, inv, a, inv, prod)
		}
	}
}

func TestGF256InverseZeroPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("GF256Inverse(0) did not panic")
		}
	}()
	GF256Inverse(0)
}

func TestGF256Division(t *testing.T) {
	initGF256Tables()
	cases := [][2]GF256{{100, 7}, {0xFF, 1}, {1, 0xFF}, {42, 42}}
	for _, c := range cases {
		a, b := c[0], c[1]
		q := GF256Div(a, b)
		check := GF256Mul(q, b)
		if check != a {
			t.Errorf("GF256Div(%d, %d) = %d, but %d * %d = %d, want %d",
				a, b, q, q, b, check, a)
		}
	}
}

func TestGF256DivZeroPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("GF256Div(1, 0) did not panic")
		}
	}()
	GF256Div(1, 0)
}

func TestGF256DivZeroNumerator(t *testing.T) {
	if GF256Div(0, 5) != 0 {
		t.Error("GF256Div(0, 5) should be 0")
	}
}

func TestGF256Pow(t *testing.T) {
	initGF256Tables()

	if GF256Pow(42, 0) != 1 {
		t.Error("GF256Pow(42, 0) != 1")
	}
	if GF256Pow(0, 5) != 0 {
		t.Error("GF256Pow(0, 5) != 0")
	}
	if GF256Pow(7, 1) != 7 {
		t.Errorf("GF256Pow(7, 1) = %d, want 7", GF256Pow(7, 1))
	}

	a := GF256(3)
	expected := GF256Mul(GF256Mul(a, a), a)
	if GF256Pow(a, 3) != expected {
		t.Errorf("GF256Pow(%d, 3) = %d, want %d", a, GF256Pow(a, 3), expected)
	}

	// Fermat's little theorem: a^{255} = 1.
	if GF256Pow(a, gf256Order) != 1 {
		t.Errorf("GF256Pow(%d, %d) = %d, want 1 (Fermat)", a, gf256Order, GF256Pow(a, gf256Order))
	}
}

func TestGF256PowNegative(t *testing.T) {
	initGF256Tables()
	a := GF256(7)
	if GF256Pow(a, -1) != GF256Inverse(a) {
		t.Errorf("GF256Pow(%d, -1) = %d, want %d", a, GF256Pow(a, -1), GF256Inverse(a))
	}
}

func TestGF256ExpLogRoundtrip(t *testing.T) {
	initGF256Tables()
	for i := 0; i < gf256Order; i++ {
		a := GF256Exp(i)
		if a == 0 {
			t.Errorf("GF256Exp(%d) = 0, generator powers should never be zero", i)
			continue
		}
		logA := GF256Log(a)
		if logA != i {
			t.Errorf("GF256Log(GF256Exp(%d)) = %d", i, logA)
		}
	}
}

func TestGF256LogZeroPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("GF256Log(0) did not panic")
		}
	}()
	GF256Log(0)
}

func TestGF256PolyEval(t *testing.T) {
	initGF256Tables()

	// p(x) = 3 + 5x + 7x^2, evaluate at x = 2.
	coeffs := []GF256{3, 5, 7}
	x := GF256(2)
	result := GF256PolyEval(coeffs, x)
	expected := GF256Add(GF256Add(3, GF256Mul(5, 2)), GF256Mul(7, GF256Mul(2, 2)))
	if result != expected {
		t.Errorf("GF256PolyEval([3,5,7], 2) = %d, want %d", result, expected)
	}

	if GF256PolyEval(nil, 5) != 0 {
		t.Error("GF256PolyEval(nil, 5) != 0")
	}

	if GF256PolyEval([]GF256{42}, 100) != 42 {
		t.Error("constant polynomial should return the constant")
	}
}

func TestGF256PolyMul(t *testing.T) {
	initGF256Tables()

	// (1 + x) * (1 + x) in char 2 = 1 + 0*x + x^2.
	p := []GF256{1, 1}
	result := GF256PolyMul(p, p)
	if len(result) != 3 {
		t.Fatalf("degree mismatch: got len %d, want 3", len(result))
	}
	if result[0] != 1 || result[1] != 0 || result[2] != 1 {
		t.Errorf("(1+x)^2 = [%d, %d, %d], want [1, 0, 1]", result[0], result[1], result[2])
	}

	if GF256PolyMul(nil, p) != nil {
		t.Error("GF256PolyMul(nil, p) should return nil")
	}
}

func TestGF256PolyAdd(t *testing.T) {
	p1 := []GF256{1, 2, 3}
	p2 := []GF256{4, 5}
	result := GF256PolyAdd(p1, p2)
	if len(result) != 3 {
		t.Fatalf("expected length 3, got %d", len(result))
	}
	if result[0] != GF256Add(1, 4) || result[1] != GF256Add(2, 5) || result[2] != 3 {
		t.Errorf("GF256PolyAdd mismatch: [%d, %d, %d]", result[0], result[1], result[2])
	}
}

func TestGF256InterpolateRoundtrip(t *testing.T) {
	initGF256Tables()

	// Define p(x) = 5 + 3x + 7x^2.
	origCoeffs := []GF256{5, 3, 7}
	n := len(origCoeffs)

	xs := make([]GF256, n)
	ys := make([]GF256, n)
	for i := 0; i < n; i++ {
		xs[i] = GF256(i + 1)
		ys[i] = GF256PolyEval(origCoeffs, xs[i])
	}

	recovered := GF256Interpolate(xs, ys)
	if len(recovered) != n {
		t.Fatalf("interpolated polynomial length = %d, want %d", len(recovered), n)
	}
	for i := 0; i < n; i++ {
		if recovered[i] != origCoeffs[i] {
			t.Errorf("recovered[%d] = %d, want %d", i, recovered[i], origCoeffs[i])
		}
	}
}

func TestGF256InterpolateLarger(t *testing.T) {
	initGF256Tables()

	// Degree-5 polynomial.
	origCoeffs := []GF256{10, 20, 30, 40, 50, 60}
	n := len(origCoeffs)

	xs := make([]GF256, n)
	ys := make([]GF256, n)
	for i := 0; i < n; i++ {
		xs[i] = GF256(i + 10)
		ys[i] = GF256PolyEval(origCoeffs, xs[i])
	}

	recovered := GF256Interpolate(xs, ys)
	for i := 0; i < n; i++ {
		val := GF256PolyEval(recovered, xs[i])
		if val != ys[i] {
			t.Errorf("recovered poly at x=%d: got %d, want %d", xs[i], val, ys[i])
		}
	}
}

func TestGF256InterpolateDuplicatePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("GF256Interpolate with duplicate xs did not panic")
		}
	}()
	GF256Interpolate([]GF256{1, 1}, []GF256{5, 10})
}

func TestGF256InterpolateLengthMismatchPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("GF256Interpolate with mismatched lengths did not panic")
		}
	}()
	GF256Interpolate([]GF256{1, 2}, []GF256{5})
}

func TestGF256PolyFromRoots(t *testing.T) {
	initGF256Tables()

	roots := []GF256{3, 7, 11}
	poly := GF256PolyFromRoots(roots)

	for _, r := range roots {
		val := GF256PolyEval(poly, r)
		if val != 0 {
			t.Errorf("poly(%d) = %d, want 0", r, val)
		}
	}

	if len(poly) != len(roots)+1 {
		t.Errorf("degree = %d, want %d", len(poly)-1, len(roots))
	}
	if poly[len(poly)-1] != 1 {
		t.Errorf("leading coefficient = %d, want 1", poly[len(poly)-1])
	}
}

func TestGF256PolyFromRootsEmpty(t *testing.T) {
	poly := GF256PolyFromRoots(nil)
	if len(poly) != 1 || poly[0] != 1 {
		t.Error("empty roots should give constant polynomial [1]")
	}
}

func TestGF256VandermondeRow(t *testing.T) {
	initGF256Tables()
	x := GF256(5)
	row := GF256VandermondeRow(x, 4)
	if len(row) != 4 {
		t.Fatalf("row length = %d, want 4", len(row))
	}
	if row[0] != 1 {
		t.Errorf("row[0] = %d, want 1", row[0])
	}
	if row[1] != x {
		t.Errorf("row[1] = %d, want %d", row[1], x)
	}
	if row[2] != GF256Mul(x, x) {
		t.Errorf("row[2] = %d, want %d", row[2], GF256Mul(x, x))
	}
	if row[3] != GF256Mul(GF256Mul(x, x), x) {
		t.Errorf("row[3] = %d, want %d", row[3], GF256Mul(GF256Mul(x, x), x))
	}
}

func TestGF256VandermondeRowZeroLength(t *testing.T) {
	row := GF256VandermondeRow(5, 0)
	if row != nil {
		t.Error("zero length row should be nil")
	}
}

func TestGF256TableConsistency(t *testing.T) {
	initGF256Tables()

	// Every non-zero element should appear exactly once in expTable[0..254].
	seen := make(map[uint8]bool)
	for i := 0; i < gf256Order; i++ {
		v := gf256ExpTable[i]
		if v == 0 {
			t.Errorf("expTable[%d] = 0", i)
		}
		if seen[v] {
			t.Errorf("expTable[%d] = %d is a duplicate", i, v)
		}
		seen[v] = true
	}
	if len(seen) != gf256Order {
		t.Errorf("expected %d unique elements, got %d", gf256Order, len(seen))
	}
}

func TestGF256MulTableConsistency(t *testing.T) {
	initGF256Tables()
	// Verify multiplication table matches log/exp computation.
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b++ {
			expected := GF256(0)
			if a != 0 && b != 0 {
				logSum := uint16(gf256LogTable[a]) + uint16(gf256LogTable[b])
				if logSum >= gf256Order {
					logSum -= gf256Order
				}
				expected = GF256(gf256ExpTable[logSum])
			}
			got := GF256(gf256MulTable[a][b])
			if got != expected {
				t.Fatalf("MulTable[%d][%d] = %d, want %d", a, b, got, expected)
			}
		}
	}
}

func TestGF256PolyScaleIdentity(t *testing.T) {
	initGF256Tables()
	poly := []GF256{5, 10, 15}
	scaled := GF256PolyScale(poly, 1)
	for i := range poly {
		if scaled[i] != poly[i] {
			t.Errorf("scaled[%d] = %d, want %d", i, scaled[i], poly[i])
		}
	}
}

func TestGF256PolyScaleZero(t *testing.T) {
	initGF256Tables()
	poly := []GF256{5, 10, 15}
	scaled := GF256PolyScale(poly, 0)
	for i := range scaled {
		if scaled[i] != 0 {
			t.Errorf("scaled[%d] = %d, want 0", i, scaled[i])
		}
	}
}

func BenchmarkGF256Mul(b *testing.B) {
	initGF256Tables()
	a, c := GF256(123), GF256(234)
	for i := 0; i < b.N; i++ {
		GF256Mul(a, c)
	}
}

func BenchmarkGF256Div(b *testing.B) {
	initGF256Tables()
	a, c := GF256(123), GF256(234)
	for i := 0; i < b.N; i++ {
		GF256Div(a, c)
	}
}

func BenchmarkGF256Interpolate8(b *testing.B) {
	initGF256Tables()
	n := 8
	xs := make([]GF256, n)
	ys := make([]GF256, n)
	for i := 0; i < n; i++ {
		xs[i] = GF256(i + 1)
		ys[i] = GF256(i*37 + 5)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GF256Interpolate(xs, ys)
	}
}
