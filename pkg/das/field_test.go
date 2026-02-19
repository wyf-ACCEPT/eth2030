package das

import (
	"math/big"
	"testing"
)

func TestFieldElementBasicArithmetic(t *testing.T) {
	a := NewFieldElementFromUint64(7)
	b := NewFieldElementFromUint64(5)

	// Addition.
	sum := a.Add(b)
	if !sum.Equal(NewFieldElementFromUint64(12)) {
		t.Errorf("7 + 5 = %v, want 12", sum.v)
	}

	// Subtraction.
	diff := a.Sub(b)
	if !diff.Equal(NewFieldElementFromUint64(2)) {
		t.Errorf("7 - 5 = %v, want 2", diff.v)
	}

	// Multiplication.
	prod := a.Mul(b)
	if !prod.Equal(NewFieldElementFromUint64(35)) {
		t.Errorf("7 * 5 = %v, want 35", prod.v)
	}

	// Negation.
	neg := a.Neg()
	sum2 := a.Add(neg)
	if !sum2.IsZero() {
		t.Errorf("7 + (-7) = %v, want 0", sum2.v)
	}

	// Zero negation.
	zeroNeg := FieldZero().Neg()
	if !zeroNeg.IsZero() {
		t.Errorf("Neg(0) = %v, want 0", zeroNeg.v)
	}
}

func TestFieldElementInverse(t *testing.T) {
	a := NewFieldElementFromUint64(7)
	inv := a.Inv()
	prod := a.Mul(inv)
	if !prod.Equal(FieldOne()) {
		t.Errorf("7 * 7^{-1} = %v, want 1", prod.v)
	}

	// Inverse of 1 is 1.
	oneInv := FieldOne().Inv()
	if !oneInv.Equal(FieldOne()) {
		t.Errorf("1^{-1} = %v, want 1", oneInv.v)
	}

	// Inverse of 0 is 0 (by convention).
	zeroInv := FieldZero().Inv()
	if !zeroInv.IsZero() {
		t.Errorf("0^{-1} = %v, want 0", zeroInv.v)
	}
}

func TestFieldElementExp(t *testing.T) {
	a := NewFieldElementFromUint64(3)
	result := a.Exp(big.NewInt(10))
	if !result.Equal(NewFieldElementFromUint64(59049)) {
		t.Errorf("3^10 = %v, want 59049", result.v)
	}

	// a^0 = 1.
	result = a.Exp(big.NewInt(0))
	if !result.Equal(FieldOne()) {
		t.Errorf("3^0 = %v, want 1", result.v)
	}
}

func TestFieldElementDiv(t *testing.T) {
	a := NewFieldElementFromUint64(35)
	b := NewFieldElementFromUint64(7)
	result := a.Div(b)
	if !result.Equal(NewFieldElementFromUint64(5)) {
		t.Errorf("35 / 7 = %v, want 5", result.v)
	}
}

func TestFieldElementModularReduction(t *testing.T) {
	// Subtraction wrapping around.
	a := NewFieldElementFromUint64(3)
	b := NewFieldElementFromUint64(5)
	result := a.Sub(b) // 3 - 5 = -2 mod p = p - 2
	expected := NewFieldElement(new(big.Int).Sub(blsModulus, big.NewInt(2)))
	if !result.Equal(expected) {
		t.Errorf("3 - 5 mod p = %v, want p-2", result.v)
	}

	// Adding back should give the original.
	check := result.Add(b)
	if !check.Equal(a) {
		t.Errorf("(3 - 5) + 5 = %v, want 3", check.v)
	}
}

func TestNewFieldElement(t *testing.T) {
	// Value larger than modulus gets reduced.
	large := new(big.Int).Add(blsModulus, big.NewInt(42))
	fe := NewFieldElement(large)
	if !fe.Equal(NewFieldElementFromUint64(42)) {
		t.Errorf("(p + 42) mod p = %v, want 42", fe.v)
	}

	// Negative big.Int gets properly reduced.
	neg := big.NewInt(-1)
	fe = NewFieldElement(neg)
	expected := NewFieldElement(new(big.Int).Sub(blsModulus, big.NewInt(1)))
	if !fe.Equal(expected) {
		t.Errorf("-1 mod p = %v, want p-1", fe.v)
	}
}

func TestRootOfUnity(t *testing.T) {
	// A primitive n-th root of unity w satisfies w^n = 1.
	for _, n := range []uint64{1, 2, 4, 8, 16, 64, 256} {
		w := rootOfUnity(n)
		result := w.Exp(new(big.Int).SetUint64(n))
		if !result.Equal(FieldOne()) {
			t.Errorf("rootOfUnity(%d)^%d = %v, want 1", n, n, result.v)
		}
		// For n > 1, w^(n/2) should NOT be 1 (primitive root).
		if n > 1 {
			halfPow := w.Exp(new(big.Int).SetUint64(n / 2))
			if halfPow.Equal(FieldOne()) {
				t.Errorf("rootOfUnity(%d)^(%d/2) = 1, not a primitive root", n, n)
			}
		}
	}
}

func TestComputeRootsOfUnity(t *testing.T) {
	n := uint64(8)
	roots := computeRootsOfUnity(n)
	if len(roots) != int(n) {
		t.Fatalf("len(roots) = %d, want %d", len(roots), n)
	}

	// First root should be 1.
	if !roots[0].Equal(FieldOne()) {
		t.Errorf("roots[0] = %v, want 1", roots[0].v)
	}

	// Last root raised to the power should give 1.
	w := roots[1]
	wN := w.Exp(new(big.Int).SetUint64(n))
	if !wN.Equal(FieldOne()) {
		t.Errorf("w^%d = %v, want 1", n, wN.v)
	}

	// All roots should be distinct.
	seen := make(map[string]bool)
	for i, r := range roots {
		key := r.v.String()
		if seen[key] {
			t.Errorf("duplicate root at index %d", i)
		}
		seen[key] = true
	}
}

func TestFFTRoundtrip(t *testing.T) {
	// Create a polynomial [1, 2, 3, 4] and verify FFT -> InverseFFT roundtrip.
	vals := []FieldElement{
		NewFieldElementFromUint64(1),
		NewFieldElementFromUint64(2),
		NewFieldElementFromUint64(3),
		NewFieldElementFromUint64(4),
	}

	transformed := FFT(vals)
	recovered := InverseFFT(transformed)

	for i, v := range recovered {
		if !v.Equal(vals[i]) {
			t.Errorf("roundtrip[%d] = %v, want %v", i, v.v, vals[i].v)
		}
	}
}

func TestFFTSingleElement(t *testing.T) {
	vals := []FieldElement{NewFieldElementFromUint64(42)}
	result := FFT(vals)
	if len(result) != 1 || !result[0].Equal(vals[0]) {
		t.Errorf("FFT of single element should be identity")
	}
}

func TestFFTEmpty(t *testing.T) {
	result := FFT(nil)
	if len(result) != 0 {
		t.Errorf("FFT of empty should return empty, got len %d", len(result))
	}
}

func TestFFTEvaluationProperty(t *testing.T) {
	// For polynomial p(x) = 1 + 2x + 3x^2 + 4x^3
	// FFT evaluates p at the 4th roots of unity.
	coeffs := []FieldElement{
		NewFieldElementFromUint64(1),
		NewFieldElementFromUint64(2),
		NewFieldElementFromUint64(3),
		NewFieldElementFromUint64(4),
	}

	evals := FFT(coeffs)
	roots := computeRootsOfUnity(4)

	// Verify each evaluation: p(roots[i]) == evals[i].
	for i := 0; i < 4; i++ {
		expected := FieldZero()
		xPow := FieldOne()
		for j := 0; j < 4; j++ {
			expected = expected.Add(coeffs[j].Mul(xPow))
			xPow = xPow.Mul(roots[i])
		}
		if !evals[i].Equal(expected) {
			t.Errorf("FFT[%d] = %v, want %v (evaluation at root %v)",
				i, evals[i].v, expected.v, roots[i].v)
		}
	}
}

func TestInverseFFTLarger(t *testing.T) {
	n := 16
	vals := make([]FieldElement, n)
	for i := range vals {
		vals[i] = NewFieldElementFromUint64(uint64(i * 7))
	}

	transformed := FFT(vals)
	recovered := InverseFFT(transformed)

	for i, v := range recovered {
		if !v.Equal(vals[i]) {
			t.Errorf("roundtrip[%d] = %v, want %v", i, v.v, vals[i].v)
		}
	}
}
