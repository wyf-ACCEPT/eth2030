package erasure

import (
	"testing"
)

func newTestGF() *GaloisField {
	return NewGaloisField()
}

// --- Multiplication tests ---

func TestGaloisFieldMulCommutativity(t *testing.T) {
	gf := newTestGF()
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b++ {
			if gf.Mul(byte(a), byte(b)) != gf.Mul(byte(b), byte(a)) {
				t.Fatalf("Mul not commutative: %d * %d", a, b)
			}
		}
	}
}

func TestGaloisFieldMulAssociativity(t *testing.T) {
	gf := newTestGF()
	triples := [][3]byte{{3, 7, 11}, {100, 200, 50}, {255, 254, 253}, {1, 128, 64}}
	for _, tr := range triples {
		a, b, c := tr[0], tr[1], tr[2]
		lhs := gf.Mul(gf.Mul(a, b), c)
		rhs := gf.Mul(a, gf.Mul(b, c))
		if lhs != rhs {
			t.Errorf("associativity: (%d*%d)*%d=%d, %d*(%d*%d)=%d", a, b, c, lhs, a, b, c, rhs)
		}
	}
}

func TestGaloisFieldMulIdentity(t *testing.T) {
	gf := newTestGF()
	for a := 0; a < 256; a++ {
		if gf.Mul(byte(a), 1) != byte(a) {
			t.Fatalf("Mul(%d, 1) = %d, want %d", a, gf.Mul(byte(a), 1), a)
		}
	}
}

func TestGaloisFieldMulZero(t *testing.T) {
	gf := newTestGF()
	for a := 0; a < 256; a++ {
		if gf.Mul(byte(a), 0) != 0 {
			t.Fatalf("Mul(%d, 0) = %d, want 0", a, gf.Mul(byte(a), 0))
		}
	}
}

// --- Division tests ---

func TestGaloisFieldDivInverse(t *testing.T) {
	gf := newTestGF()
	// a/b * b should equal a for all non-zero b.
	for a := 1; a < 256; a++ {
		for b := 1; b < 256; b++ {
			q := gf.Div(byte(a), byte(b))
			check := gf.Mul(q, byte(b))
			if check != byte(a) {
				t.Fatalf("Div(%d,%d)=%d, but %d*%d=%d, want %d", a, b, q, q, b, check, a)
			}
		}
	}
}

func TestGaloisFieldDivByZeroPanics(t *testing.T) {
	gf := newTestGF()
	defer func() {
		if r := recover(); r == nil {
			t.Error("Div(1, 0) did not panic")
		}
	}()
	gf.Div(1, 0)
}

func TestGaloisFieldDivZeroNumerator(t *testing.T) {
	gf := newTestGF()
	if gf.Div(0, 42) != 0 {
		t.Error("Div(0, 42) should be 0")
	}
}

// --- Inverse tests ---

func TestGaloisFieldInvCorrectness(t *testing.T) {
	gf := newTestGF()
	for a := 1; a < 256; a++ {
		inv := gf.Inv(byte(a))
		prod := gf.Mul(byte(a), inv)
		if prod != 1 {
			t.Fatalf("Inv(%d)=%d, but %d*%d=%d, want 1", a, inv, a, inv, prod)
		}
	}
}

func TestGaloisFieldInvZeroPanics(t *testing.T) {
	gf := newTestGF()
	defer func() {
		if r := recover(); r == nil {
			t.Error("Inv(0) did not panic")
		}
	}()
	gf.Inv(0)
}

// --- Power tests ---

func TestGaloisFieldPowCorrectness(t *testing.T) {
	gf := newTestGF()

	// a^0 = 1
	if gf.Pow(42, 0) != 1 {
		t.Error("Pow(42, 0) != 1")
	}
	// 0^n = 0
	if gf.Pow(0, 5) != 0 {
		t.Error("Pow(0, 5) != 0")
	}
	// a^1 = a
	if gf.Pow(7, 1) != 7 {
		t.Errorf("Pow(7, 1) = %d, want 7", gf.Pow(7, 1))
	}
	// a^3 = a*a*a
	a := byte(3)
	expected := gf.Mul(gf.Mul(a, a), a)
	if gf.Pow(a, 3) != expected {
		t.Errorf("Pow(%d, 3) = %d, want %d", a, gf.Pow(a, 3), expected)
	}
	// Fermat's little theorem: a^255 = 1 for non-zero a.
	if gf.Pow(a, gfFieldOrder) != 1 {
		t.Errorf("Pow(%d, %d) = %d, want 1 (Fermat)", a, gfFieldOrder, gf.Pow(a, gfFieldOrder))
	}
}

func TestGaloisFieldPowNegative(t *testing.T) {
	gf := newTestGF()
	a := byte(7)
	if gf.Pow(a, -1) != gf.Inv(a) {
		t.Errorf("Pow(%d, -1) = %d, want %d", a, gf.Pow(a, -1), gf.Inv(a))
	}
}

// --- Vandermonde matrix tests ---

func TestGaloisFieldVandermondeDimensions(t *testing.T) {
	gf := newTestGF()
	m := gf.GenerateVandermondeMatrix(4, 3)
	if len(m) != 4 {
		t.Fatalf("rows = %d, want 4", len(m))
	}
	for i, row := range m {
		if len(row) != 3 {
			t.Errorf("row %d: cols = %d, want 3", i, len(row))
		}
	}
}

func TestGaloisFieldVandermondeProperties(t *testing.T) {
	gf := newTestGF()
	m := gf.GenerateVandermondeMatrix(5, 4)

	// First column should be all 1s (x^0 = 1 for any x).
	for i, row := range m {
		if row[0] != 1 {
			t.Errorf("row %d col 0 = %d, want 1", i, row[0])
		}
	}

	// Row 0 uses g^0 = 1, so all entries should be 1.
	for j, val := range m[0] {
		if val != 1 {
			t.Errorf("row 0 col %d = %d, want 1 (1^j = 1)", j, val)
		}
	}

	// Row i, col j should equal (g^i)^j.
	for i := 0; i < 5; i++ {
		x := gf.Exp(i)
		for j := 0; j < 4; j++ {
			expected := gf.Pow(x, j)
			if m[i][j] != expected {
				t.Errorf("m[%d][%d] = %d, want %d", i, j, m[i][j], expected)
			}
		}
	}
}

func TestGaloisFieldVandermondeNil(t *testing.T) {
	gf := newTestGF()
	if gf.GenerateVandermondeMatrix(0, 3) != nil {
		t.Error("expected nil for 0 rows")
	}
	if gf.GenerateVandermondeMatrix(3, 0) != nil {
		t.Error("expected nil for 0 cols")
	}
}

// --- Matrix multiplication tests ---

func TestGaloisFieldMatMul(t *testing.T) {
	gf := newTestGF()

	// Identity matrix * vector = vector.
	identity := [][]byte{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
	}
	data := []byte{10, 20, 30}
	result := gf.MatMul(identity, data)
	if len(result) != 3 {
		t.Fatalf("result length = %d, want 3", len(result))
	}
	for i, v := range result {
		if v != data[i] {
			t.Errorf("result[%d] = %d, want %d", i, v, data[i])
		}
	}
}

func TestGaloisFieldMatMulVandermonde(t *testing.T) {
	gf := newTestGF()

	// Encode using Vandermonde: result is polynomial evaluation at g^i.
	data := []byte{5, 10, 15}
	m := gf.GenerateVandermondeMatrix(4, 3)
	result := gf.MatMul(m, data)

	if len(result) != 4 {
		t.Fatalf("result length = %d, want 4", len(result))
	}

	// Verify: result[i] should equal PolyEval(data, g^i).
	for i := 0; i < 4; i++ {
		x := gf.Exp(i)
		expected := gf.PolyEval(data, x)
		if result[i] != expected {
			t.Errorf("result[%d] = %d, want %d (PolyEval at g^%d)", i, result[i], expected, i)
		}
	}
}

func TestGaloisFieldMatMulEmpty(t *testing.T) {
	gf := newTestGF()
	if gf.MatMul(nil, []byte{1}) != nil {
		t.Error("nil matrix should return nil")
	}
	if gf.MatMul([][]byte{{1}}, nil) != nil {
		t.Error("nil data should return nil")
	}
}

// --- Polynomial evaluation tests ---

func TestGaloisFieldPolyEval(t *testing.T) {
	gf := newTestGF()

	// p(x) = 3 + 5x + 7x^2, evaluate at x = 2.
	coeffs := []byte{3, 5, 7}
	x := byte(2)
	result := gf.PolyEval(coeffs, x)
	expected := gf.Add(gf.Add(3, gf.Mul(5, 2)), gf.Mul(7, gf.Mul(2, 2)))
	if result != expected {
		t.Errorf("PolyEval([3,5,7], 2) = %d, want %d", result, expected)
	}
}

func TestGaloisFieldPolyEvalKnownPoints(t *testing.T) {
	gf := newTestGF()

	// Constant polynomial p(x) = 42.
	if gf.PolyEval([]byte{42}, 100) != 42 {
		t.Error("constant poly should return the constant")
	}
	// Empty polynomial evaluates to 0.
	if gf.PolyEval(nil, 5) != 0 {
		t.Error("empty poly should return 0")
	}
	// p(x) = x, so p(7) = 7.
	if gf.PolyEval([]byte{0, 1}, 7) != 7 {
		t.Errorf("PolyEval([0,1], 7) = %d, want 7", gf.PolyEval([]byte{0, 1}, 7))
	}
}

// --- Log/Exp table consistency ---

func TestGaloisFieldLogExpConsistency(t *testing.T) {
	gf := newTestGF()

	// Every non-zero element should appear exactly once in expTbl[0..254].
	seen := make(map[byte]bool)
	for i := 0; i < gfFieldOrder; i++ {
		v := gf.Exp(i)
		if v == 0 {
			t.Errorf("Exp(%d) = 0", i)
		}
		if seen[v] {
			t.Errorf("Exp(%d) = %d is a duplicate", i, v)
		}
		seen[v] = true
	}
	if len(seen) != gfFieldOrder {
		t.Errorf("expected %d unique elements, got %d", gfFieldOrder, len(seen))
	}
}

func TestGaloisFieldLogExpRoundtrip(t *testing.T) {
	gf := newTestGF()
	for i := 0; i < gfFieldOrder; i++ {
		a := gf.Exp(i)
		logA := gf.Log(a)
		if logA != i {
			t.Errorf("Log(Exp(%d)) = %d", i, logA)
		}
	}
}

func TestGaloisFieldLogZeroPanics(t *testing.T) {
	gf := newTestGF()
	defer func() {
		if r := recover(); r == nil {
			t.Error("Log(0) did not panic")
		}
	}()
	gf.Log(0)
}

// --- Full field coverage ---

func TestGaloisFieldFullFieldCoverage(t *testing.T) {
	gf := newTestGF()

	// Verify a * inv(a) = 1 for all 255 non-zero elements.
	for a := 1; a < 256; a++ {
		inv := gf.Inv(byte(a))
		prod := gf.Mul(byte(a), inv)
		if prod != 1 {
			t.Fatalf("a=%d: Mul(%d, Inv(%d))=%d, want 1", a, a, a, prod)
		}
	}
}

func TestGaloisFieldDistributivity(t *testing.T) {
	gf := newTestGF()
	// a * (b + c) = a*b + a*c for sampled values.
	cases := [][3]byte{{37, 99, 250}, {1, 254, 255}, {128, 64, 32}}
	for _, c := range cases {
		a, b, cc := c[0], c[1], c[2]
		lhs := gf.Mul(a, gf.Add(b, cc))
		rhs := gf.Add(gf.Mul(a, b), gf.Mul(a, cc))
		if lhs != rhs {
			t.Errorf("distributivity: %d*(%d+%d)=%d, %d*%d+%d*%d=%d", a, b, cc, lhs, a, b, a, cc, rhs)
		}
	}
}

func TestGaloisFieldPolyInterpolateRoundtrip(t *testing.T) {
	gf := newTestGF()

	// Define p(x) = 5 + 3x + 7x^2, evaluate at 3 points, interpolate back.
	origCoeffs := []byte{5, 3, 7}
	n := len(origCoeffs)

	xs := make([]byte, n)
	ys := make([]byte, n)
	for i := 0; i < n; i++ {
		xs[i] = byte(i + 1)
		ys[i] = gf.PolyEval(origCoeffs, xs[i])
	}

	recovered := gf.PolyInterpolate(xs, ys)
	if len(recovered) != n {
		t.Fatalf("interpolated len = %d, want %d", len(recovered), n)
	}
	for i := 0; i < n; i++ {
		if recovered[i] != origCoeffs[i] {
			t.Errorf("recovered[%d] = %d, want %d", i, recovered[i], origCoeffs[i])
		}
	}
}

func TestGaloisFieldAddSubEquality(t *testing.T) {
	gf := newTestGF()
	// In GF(2^8), addition and subtraction are both XOR.
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b++ {
			if gf.Add(byte(a), byte(b)) != gf.Sub(byte(a), byte(b)) {
				t.Fatalf("Add(%d,%d) != Sub(%d,%d)", a, b, a, b)
			}
		}
	}
}
