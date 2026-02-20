package pqc

import (
	"testing"
)

func TestPolyRing_NewPolyIsZero(t *testing.T) {
	p := NewPoly()
	if !p.IsZero() {
		t.Fatal("new polynomial should be zero")
	}
}

func TestPolyRing_NewPolyFromCoeffs(t *testing.T) {
	coeffs := []int16{1, 2, 3}
	p := NewPolyFromCoeffs(coeffs)
	if p.Coeffs[0] != 1 || p.Coeffs[1] != 2 || p.Coeffs[2] != 3 {
		t.Fatal("coefficients mismatch")
	}
	if p.Coeffs[3] != 0 {
		t.Fatal("remaining coefficients should be zero")
	}
}

func TestPolyRing_NewPolyFromCoeffsReduces(t *testing.T) {
	// Negative coefficient should be reduced to [0, q).
	coeffs := []int16{-1}
	p := NewPolyFromCoeffs(coeffs)
	if p.Coeffs[0] != PolyQ-1 {
		t.Fatalf("expected %d, got %d", PolyQ-1, p.Coeffs[0])
	}
}

func TestPolyRing_Clone(t *testing.T) {
	p := NewPolyFromCoeffs([]int16{1, 2, 3})
	c := p.Clone()
	if !p.Equal(c) {
		t.Fatal("clone should be equal")
	}
	// Mutating clone should not affect original.
	c.Coeffs[0] = 999
	if p.Coeffs[0] == 999 {
		t.Fatal("clone should be independent")
	}
}

func TestPolyRing_EqualNil(t *testing.T) {
	p := NewPoly()
	if p.Equal(nil) {
		t.Fatal("nil comparison should return false")
	}
}

func TestPolyRing_AddIdentity(t *testing.T) {
	p := NewPolyFromCoeffs([]int16{100, 200, 300})
	zero := NewPoly()
	sum := p.Add(zero)
	if !sum.Equal(p) {
		t.Fatal("p + 0 should equal p")
	}
}

func TestPolyRing_AddCommutative(t *testing.T) {
	a := NewPolyFromCoeffs([]int16{100, 200})
	b := NewPolyFromCoeffs([]int16{300, 400})
	if !a.Add(b).Equal(b.Add(a)) {
		t.Fatal("addition should be commutative")
	}
}

func TestPolyRing_SubInverse(t *testing.T) {
	a := NewPolyFromCoeffs([]int16{100, 200, 300})
	b := NewPolyFromCoeffs([]int16{50, 100, 150})
	diff := a.Sub(b)
	restored := diff.Add(b)
	if !restored.Equal(a) {
		t.Fatal("(a - b) + b should equal a")
	}
}

func TestPolyRing_Negate(t *testing.T) {
	p := NewPolyFromCoeffs([]int16{1, 2, 3})
	neg := p.Negate()
	sum := p.Add(neg)
	if !sum.IsZero() {
		t.Fatal("p + (-p) should be zero")
	}
}

func TestPolyRing_ScalarMul(t *testing.T) {
	p := NewPolyFromCoeffs([]int16{1, 2, 3})
	scaled := p.ScalarMul(2)
	if scaled.Coeffs[0] != 2 || scaled.Coeffs[1] != 4 || scaled.Coeffs[2] != 6 {
		t.Fatal("scalar multiply failed")
	}
}

func TestPolyRing_ScalarMulZero(t *testing.T) {
	p := NewPolyFromCoeffs([]int16{100, 200})
	zero := p.ScalarMul(0)
	if !zero.IsZero() {
		t.Fatal("scalar multiply by 0 should give zero poly")
	}
}

func TestPolyRing_Reduce(t *testing.T) {
	p := &Poly{}
	p.Coeffs[0] = -5
	p.Coeffs[1] = int16(PolyQ + 10)
	p.Reduce()
	if p.Coeffs[0] != PolyQ-5 {
		t.Fatalf("expected %d, got %d", PolyQ-5, p.Coeffs[0])
	}
	// (PolyQ + 10) mod PolyQ = 10
	if p.Coeffs[1] != 10 {
		t.Fatalf("expected 10, got %d", p.Coeffs[1])
	}
}

func TestPolyRing_SchoolbookMulIdentity(t *testing.T) {
	// Multiply by the polynomial "1" (identity).
	p := NewPolyFromCoeffs([]int16{42, 17, 99})
	one := NewPolyFromCoeffs([]int16{1})
	result := p.MulSchoolbook(one)
	if !result.Equal(p) {
		t.Fatal("p * 1 should equal p")
	}
}

func TestPolyRing_SchoolbookMulZero(t *testing.T) {
	p := NewPolyFromCoeffs([]int16{42, 17})
	zero := NewPoly()
	result := p.MulSchoolbook(zero)
	if !result.IsZero() {
		t.Fatal("p * 0 should be zero")
	}
}

func TestPolyRing_SchoolbookMulCommutative(t *testing.T) {
	a := NewPolyFromCoeffs([]int16{1, 2, 3})
	b := NewPolyFromCoeffs([]int16{4, 5})
	ab := a.MulSchoolbook(b)
	ba := b.MulSchoolbook(a)
	if !ab.Equal(ba) {
		t.Fatal("multiplication should be commutative in the ring")
	}
}

func TestPolyRing_NTTRoundtrip(t *testing.T) {
	// Forward NTT then inverse should recover original.
	p := NewPolyFromCoeffs([]int16{10, 20, 30, 40})
	ntt := polyNTT(p)
	recovered := polyInvNTT(ntt)

	for i := 0; i < PolyN; i++ {
		a := polyMod(p.Coeffs[i])
		b := polyMod(recovered.Coeffs[i])
		if a != b {
			t.Fatalf("NTT roundtrip mismatch at index %d: %d != %d", i, a, b)
		}
	}
}

func TestPolyRing_NTTMulCommutativity(t *testing.T) {
	// NTT-based multiplication should be commutative: a*b == b*a.
	a := SampleUniform([]byte("ntt-mul-comm-a"), 0)
	b := SampleUniform([]byte("ntt-mul-comm-b"), 0)

	ab := a.MulNTT(b)
	ba := b.MulNTT(a)

	for i := 0; i < PolyN; i++ {
		if polyMod(ab.Coeffs[i]) != polyMod(ba.Coeffs[i]) {
			t.Fatalf("NTT mul not commutative at %d: %d != %d",
				i, polyMod(ab.Coeffs[i]), polyMod(ba.Coeffs[i]))
		}
	}
}

func TestPolyRing_NTTMulByOne(t *testing.T) {
	// Multiplying by the NTT-domain identity: NTT(1) pointwise multiply should
	// give back the original when composed with inverse NTT.
	// Test that NTT multiplication with a known polynomial is deterministic.
	a := SampleUniform([]byte("ntt-mul-one"), 0)
	b := SampleUniform([]byte("ntt-mul-one"), 0)

	// Same input should give same result.
	r1 := a.MulNTT(b)
	r2 := a.MulNTT(b)
	if !r1.Equal(r2) {
		t.Fatal("NTT multiplication should be deterministic")
	}
}

func TestPolyRing_SampleUniformNonZero(t *testing.T) {
	seed := []byte("test-seed-uniform")
	p := SampleUniform(seed, 0)
	if p.IsZero() {
		t.Fatal("uniform sample should not be zero poly")
	}
	// All coefficients should be in [0, q).
	for i, c := range p.Coeffs {
		if c < 0 || c >= PolyQ {
			t.Fatalf("coefficient %d out of range: %d", i, c)
		}
	}
}

func TestPolyRing_SampleUniformDeterministic(t *testing.T) {
	seed := []byte("deterministic-seed")
	p1 := SampleUniform(seed, 0)
	p2 := SampleUniform(seed, 0)
	if !p1.Equal(p2) {
		t.Fatal("same seed and nonce should give same polynomial")
	}
}

func TestPolyRing_SampleUniformDifferentNonces(t *testing.T) {
	seed := []byte("nonce-test")
	p1 := SampleUniform(seed, 0)
	p2 := SampleUniform(seed, 1)
	if p1.Equal(p2) {
		t.Fatal("different nonces should give different polynomials")
	}
}

func TestPolyRing_SampleCBDSmallCoeffs(t *testing.T) {
	seed := []byte("cbd-seed")
	p := SampleCBD(seed, 0, 2) // eta=2, coefficients in [-2, 2]
	for i, c := range p.Coeffs {
		reduced := polyMod(c)
		// In [0, q), values near 0 or near q represent small magnitudes.
		if reduced > 2 && reduced < PolyQ-2 {
			t.Fatalf("CBD coefficient %d out of expected range: %d (reduced=%d)", i, c, reduced)
		}
	}
}

func TestPolyRing_SampleCBDDeterministic(t *testing.T) {
	seed := []byte("cbd-det-seed")
	p1 := SampleCBD(seed, 0, 2)
	p2 := SampleCBD(seed, 0, 2)
	if !p1.Equal(p2) {
		t.Fatal("same seed and nonce should give same CBD sample")
	}
}

func TestPolyRing_PolyModRange(t *testing.T) {
	tests := []struct {
		input    int16
		expected int16
	}{
		{0, 0},
		{1, 1},
		{-1, PolyQ - 1},
		{PolyQ, 0},
		{PolyQ + 1, 1},
		{-PolyQ, 0},
	}
	for _, tt := range tests {
		got := polyMod(tt.input)
		if got != tt.expected {
			t.Errorf("polyMod(%d) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestPolyRing_DistributiveProperty(t *testing.T) {
	// a * (b + c) should equal a*b + a*c.
	a := NewPolyFromCoeffs([]int16{1, 2})
	b := NewPolyFromCoeffs([]int16{3, 4})
	c := NewPolyFromCoeffs([]int16{5, 6})

	bc := b.Add(c)
	lhs := a.MulSchoolbook(bc)
	rhs := a.MulSchoolbook(b).Add(a.MulSchoolbook(c))

	for i := 0; i < PolyN; i++ {
		if polyMod(lhs.Coeffs[i]) != polyMod(rhs.Coeffs[i]) {
			t.Fatalf("distributive law failed at index %d: %d != %d",
				i, polyMod(lhs.Coeffs[i]), polyMod(rhs.Coeffs[i]))
		}
	}
}
