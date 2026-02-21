package erasure

import (
	"testing"
)

// --- RSGeneratorPoly tests ---

func TestRSGeneratorPolyDegreeOne(t *testing.T) {
	// g(x) for nsym=1 should be (x - a^0) = (x + 1) = [1, 1].
	gen := RSGeneratorPoly(1)
	if len(gen) != 2 {
		t.Fatalf("expected degree 1 (len 2), got len %d", len(gen))
	}
	if gen[1] != 1 {
		t.Fatalf("expected monic polynomial, leading coeff = %d", gen[1])
	}
}

func TestRSGeneratorPolyDegreeTwo(t *testing.T) {
	gen := RSGeneratorPoly(2)
	if len(gen) != 3 {
		t.Fatalf("expected degree 2 (len 3), got len %d", len(gen))
	}
	// Must be monic.
	if gen[2] != 1 {
		t.Fatalf("expected monic, got leading coeff %d", gen[2])
	}
}

func TestRSGeneratorPolyRoots(t *testing.T) {
	// g(a^i) should be 0 for i = 0..nsym-1.
	initGF256Tables()
	nsym := 4
	gen := RSGeneratorPoly(nsym)
	for i := 0; i < nsym; i++ {
		root := GF256Exp(i)
		val := GF256PolyEval(gen, root)
		if val != 0 {
			t.Errorf("g(a^%d) = %d, want 0", i, val)
		}
	}
}

func TestRSGeneratorPolyZero(t *testing.T) {
	gen := RSGeneratorPoly(0)
	if len(gen) != 1 || gen[0] != 1 {
		t.Fatalf("expected [1] for nsym=0, got %v", gen)
	}
}

func TestRSGeneratorPolyNegative(t *testing.T) {
	gen := RSGeneratorPoly(-1)
	if len(gen) != 1 || gen[0] != 1 {
		t.Fatalf("expected [1] for nsym=-1, got %v", gen)
	}
}

// --- RSCalcSyndromes tests ---

func TestRSCalcSyndromesNoErrors(t *testing.T) {
	initGF256Tables()
	// Encode a message with RSEncodeSystematic, then syndromes should be zero.
	msg := []GF256{1, 2, 3, 4}
	nsym := 4
	codeword := RSEncodeSystematic(msg, nsym)
	syndromes := RSCalcSyndromes(codeword, nsym)
	if !RSSyndromeIsZero(syndromes) {
		t.Fatalf("expected zero syndromes for valid codeword, got %v", syndromes)
	}
}

func TestRSCalcSyndromesWithErrors(t *testing.T) {
	initGF256Tables()
	msg := []GF256{10, 20, 30}
	nsym := 4
	codeword := RSEncodeSystematic(msg, nsym)
	// Introduce an error.
	codeword[0] ^= 0x42
	syndromes := RSCalcSyndromes(codeword, nsym)
	if RSSyndromeIsZero(syndromes) {
		t.Fatal("expected non-zero syndromes after error injection")
	}
}

func TestRSCalcSyndromesEmpty(t *testing.T) {
	result := RSCalcSyndromes(nil, 4)
	if result != nil {
		t.Fatalf("expected nil for empty message, got %v", result)
	}
}

func TestRSCalcSyndromesZeroNsym(t *testing.T) {
	result := RSCalcSyndromes([]GF256{1, 2}, 0)
	if result != nil {
		t.Fatalf("expected nil for nsym=0, got %v", result)
	}
}

// --- RSSyndromeIsZero tests ---

func TestRSSyndromeIsZeroTrue(t *testing.T) {
	if !RSSyndromeIsZero([]GF256{0, 0, 0}) {
		t.Fatal("expected true for all-zero syndromes")
	}
}

func TestRSSyndromeIsZeroFalse(t *testing.T) {
	if RSSyndromeIsZero([]GF256{0, 1, 0}) {
		t.Fatal("expected false for non-zero syndromes")
	}
}

func TestRSSyndromeIsZeroEmpty(t *testing.T) {
	if !RSSyndromeIsZero(nil) {
		t.Fatal("expected true for nil syndromes")
	}
}

// --- RSBerlekampMassey tests ---

func TestRSBerlekampMasseyEmpty(t *testing.T) {
	loc := RSBerlekampMassey(nil)
	if len(loc) != 1 || loc[0] != 1 {
		t.Fatalf("expected [1] for empty syndromes, got %v", loc)
	}
}

func TestRSBerlekampMasseyZeroSyndromes(t *testing.T) {
	loc := RSBerlekampMassey([]GF256{0, 0, 0, 0})
	// With zero syndromes, the error locator should be [1] (no errors).
	if loc[0] != 1 {
		t.Fatalf("expected constant term 1, got %d", loc[0])
	}
}

func TestRSBerlekampMasseyOneError(t *testing.T) {
	initGF256Tables()
	// Create a codeword with one error.
	msg := []GF256{5, 10, 15, 20}
	nsym := 4
	codeword := RSEncodeSystematic(msg, nsym)
	// Inject error at position 2.
	codeword[2] ^= 0x33
	syndromes := RSCalcSyndromes(codeword, nsym)

	loc := RSBerlekampMassey(syndromes)
	// Error locator should be degree 1 for a single error.
	deg := RSPolyDegree(loc)
	if deg != 1 {
		t.Fatalf("expected degree 1 error locator for 1 error, got degree %d", deg)
	}
}

// --- RSErrorLocatorRoots tests ---

func TestRSErrorLocatorRootsOneError(t *testing.T) {
	initGF256Tables()
	msg := []GF256{5, 10, 15, 20}
	nsym := 4
	codeword := RSEncodeSystematic(msg, nsym)
	errPos := 3
	codeword[errPos] ^= 0x77
	syndromes := RSCalcSyndromes(codeword, nsym)
	loc := RSBerlekampMassey(syndromes)
	positions := RSErrorLocatorRoots(loc, len(codeword))

	if len(positions) == 0 {
		t.Fatal("expected to find at least one error position")
	}
	found := false
	for _, p := range positions {
		if p == errPos {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error at position %d, got positions %v", errPos, positions)
	}
}

func TestRSErrorLocatorRootsEmpty(t *testing.T) {
	positions := RSErrorLocatorRoots([]GF256{1}, 10)
	if positions != nil {
		t.Fatalf("expected nil for degree-0 locator, got %v", positions)
	}
}

// --- RSFormalDerivative tests ---

func TestRSFormalDerivativeConstant(t *testing.T) {
	result := RSFormalDerivative([]GF256{42})
	if result != nil {
		t.Fatalf("expected nil for constant polynomial, got %v", result)
	}
}

func TestRSFormalDerivativeLinear(t *testing.T) {
	// d/dx (a + bx) = b in char 2 (odd degree term survives).
	result := RSFormalDerivative([]GF256{5, 7})
	if len(result) != 1 || result[0] != 7 {
		t.Fatalf("expected [7], got %v", result)
	}
}

func TestRSFormalDerivativeQuadratic(t *testing.T) {
	// d/dx (a + bx + cx^2) = b + 0*x = b (only odd-degree terms survive).
	result := RSFormalDerivative([]GF256{3, 5, 7})
	if len(result) != 2 {
		t.Fatalf("expected length 2, got %d", len(result))
	}
	if result[0] != 5 {
		t.Errorf("result[0] = %d, want 5", result[0])
	}
	if result[1] != 0 {
		t.Errorf("result[1] = %d, want 0 (even degree vanishes)", result[1])
	}
}

func TestRSFormalDerivativeCubic(t *testing.T) {
	// d/dx (a + bx + cx^2 + dx^3) = b + 0*x + d*x^2.
	result := RSFormalDerivative([]GF256{1, 2, 3, 4})
	if len(result) != 3 {
		t.Fatalf("expected length 3, got %d", len(result))
	}
	if result[0] != 2 {
		t.Errorf("result[0] = %d, want 2", result[0])
	}
	if result[1] != 0 {
		t.Errorf("result[1] = %d, want 0", result[1])
	}
	if result[2] != 4 {
		t.Errorf("result[2] = %d, want 4", result[2])
	}
}

func TestRSFormalDerivativeEmpty(t *testing.T) {
	result := RSFormalDerivative(nil)
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

// --- RSForneyAlgorithm tests ---

func TestRSForneyAlgorithmEmpty(t *testing.T) {
	result := RSForneyAlgorithm(nil, nil, nil)
	if result != nil {
		t.Fatalf("expected nil for empty positions, got %v", result)
	}
}

func TestRSForneyAlgorithmSingleError(t *testing.T) {
	initGF256Tables()
	msg := []GF256{10, 20, 30, 40}
	nsym := 4
	codeword := RSEncodeSystematic(msg, nsym)
	errPos := 2
	errVal := GF256(0x55)
	codeword[errPos] ^= errVal

	syndromes := RSCalcSyndromes(codeword, nsym)
	loc := RSBerlekampMassey(syndromes)
	positions := RSErrorLocatorRoots(loc, len(codeword))
	magnitudes := RSForneyAlgorithm(syndromes, loc, positions)

	if len(magnitudes) == 0 {
		t.Fatal("expected non-empty magnitudes")
	}
	// At least one magnitude should be non-zero.
	hasNonZero := false
	for _, m := range magnitudes {
		if m != 0 {
			hasNonZero = true
		}
	}
	if !hasNonZero {
		t.Fatal("expected at least one non-zero magnitude")
	}
}

// --- RSEncodeSystematic tests ---

func TestRSEncodeSystematicPreservesMessage(t *testing.T) {
	initGF256Tables()
	msg := []GF256{10, 20, 30, 40, 50}
	nsym := 4
	codeword := RSEncodeSystematic(msg, nsym)

	// Message should appear at positions nsym..nsym+len(msg)-1.
	for i, v := range msg {
		if codeword[nsym+i] != v {
			t.Errorf("codeword[%d] = %d, want %d", nsym+i, codeword[nsym+i], v)
		}
	}
}

func TestRSEncodeSystematicZeroSyndromes(t *testing.T) {
	initGF256Tables()
	msg := []GF256{1, 2, 3}
	nsym := 3
	codeword := RSEncodeSystematic(msg, nsym)
	syndromes := RSCalcSyndromes(codeword, nsym)
	if !RSSyndromeIsZero(syndromes) {
		t.Fatalf("systematic encoding should produce zero syndromes, got %v", syndromes)
	}
}

func TestRSEncodeSystematicEmptyMsg(t *testing.T) {
	result := RSEncodeSystematic(nil, 4)
	if result != nil {
		t.Fatalf("expected nil for empty msg, got %v", result)
	}
}

func TestRSEncodeSystematicZeroNsym(t *testing.T) {
	msg := []GF256{1, 2, 3}
	result := RSEncodeSystematic(msg, 0)
	if len(result) != len(msg) {
		t.Fatalf("expected passthrough for nsym=0, got len %d", len(result))
	}
}

// --- RSPolyDegree tests ---

func TestRSPolyDegreeNormal(t *testing.T) {
	if RSPolyDegree([]GF256{1, 2, 3}) != 2 {
		t.Fatal("expected degree 2")
	}
}

func TestRSPolyDegreeWithTrailingZeros(t *testing.T) {
	if RSPolyDegree([]GF256{1, 2, 0, 0}) != 1 {
		t.Fatalf("expected degree 1, got %d", RSPolyDegree([]GF256{1, 2, 0, 0}))
	}
}

func TestRSPolyDegreeZero(t *testing.T) {
	if RSPolyDegree([]GF256{0, 0, 0}) != -1 {
		t.Fatal("expected degree -1 for zero polynomial")
	}
}

func TestRSPolyDegreeEmpty(t *testing.T) {
	if RSPolyDegree(nil) != -1 {
		t.Fatal("expected degree -1 for nil polynomial")
	}
}

// --- RSPolyNormalize tests ---

func TestRSPolyNormalizeRemovesLeadingZeros(t *testing.T) {
	result := RSPolyNormalize([]GF256{5, 3, 0, 0})
	if len(result) != 2 {
		t.Fatalf("expected length 2 after normalization, got %d", len(result))
	}
	if result[0] != 5 || result[1] != 3 {
		t.Fatalf("expected [5, 3], got %v", result)
	}
}

func TestRSPolyNormalizeZeroPoly(t *testing.T) {
	result := RSPolyNormalize([]GF256{0, 0, 0})
	if len(result) != 1 || result[0] != 0 {
		t.Fatalf("expected [0] for zero poly, got %v", result)
	}
}

func TestRSPolyNormalizeAlreadyNormal(t *testing.T) {
	result := RSPolyNormalize([]GF256{1, 2, 3})
	if len(result) != 3 {
		t.Fatalf("expected no change, got length %d", len(result))
	}
}

// --- RSPolyDiv tests ---

func TestRSPolyDivSimple(t *testing.T) {
	initGF256Tables()
	// (x^2 + 1) / (x + 1) in GF(2^8).
	a := []GF256{1, 0, 1}
	b := []GF256{1, 1}
	q, r := RSPolyDiv(a, b)
	if q == nil || r == nil {
		t.Fatal("expected non-nil quotient and remainder")
	}
	// Verify: a = b*q + r.
	bq := GF256PolyMul(b, q)
	reconstructed := GF256PolyAdd(bq, r)
	for i := 0; i < len(a); i++ {
		expected := a[i]
		got := GF256(0)
		if i < len(reconstructed) {
			got = reconstructed[i]
		}
		if got != expected {
			t.Errorf("position %d: got %d, want %d", i, got, expected)
		}
	}
}

func TestRSPolyDivByZero(t *testing.T) {
	q, r := RSPolyDiv([]GF256{1, 2}, []GF256{0})
	if q != nil || r != nil {
		t.Fatal("expected nil for division by zero polynomial")
	}
}

func TestRSPolyDivSmallerDividend(t *testing.T) {
	initGF256Tables()
	// Dividend degree < divisor degree: quotient is 0, remainder is dividend.
	a := []GF256{5}
	b := []GF256{1, 1}
	q, r := RSPolyDiv(a, b)
	if RSPolyDegree(q) > 0 {
		t.Fatalf("expected zero quotient, got %v", q)
	}
	if r[0] != 5 {
		t.Fatalf("expected remainder [5], got %v", r)
	}
}

// --- RSPolyGCD tests ---

func TestRSPolyGCDSelf(t *testing.T) {
	initGF256Tables()
	p := []GF256{1, 1} // x + 1
	gcd := RSPolyGCD(p, p)
	deg := RSPolyDegree(gcd)
	if deg != 1 {
		t.Fatalf("expected degree 1 for GCD(p, p), got %d", deg)
	}
}

func TestRSPolyGCDCoprime(t *testing.T) {
	initGF256Tables()
	// (x + 1) and (x + a^1) should be coprime for a^1 != 1.
	p1 := []GF256{1, 1}
	p2 := []GF256{GF256Exp(1), 1}
	gcd := RSPolyGCD(p1, p2)
	deg := RSPolyDegree(gcd)
	if deg != 0 {
		t.Fatalf("expected degree 0 (coprime), got degree %d", deg)
	}
}

// --- RSEvaluatorPoly tests ---

func TestRSEvaluatorPolyLength(t *testing.T) {
	initGF256Tables()
	syndromes := []GF256{1, 2, 3, 4}
	errLoc := []GF256{1, 5}
	omega := RSEvaluatorPoly(syndromes, errLoc, len(syndromes))
	if len(omega) > len(syndromes) {
		t.Fatalf("expected truncated to nsym, got len %d", len(omega))
	}
}

// --- End-to-end error correction test ---

func TestRSEndToEndSingleErrorCorrection(t *testing.T) {
	initGF256Tables()

	msg := []GF256{72, 101, 108, 108, 111} // "Hello"
	nsym := 6
	codeword := RSEncodeSystematic(msg, nsym)

	// Verify no errors initially.
	syn := RSCalcSyndromes(codeword, nsym)
	if !RSSyndromeIsZero(syn) {
		t.Fatal("initial codeword has non-zero syndromes")
	}

	// Inject a single error.
	errPos := 4
	errVal := GF256(0xAA)
	codeword[errPos] ^= errVal

	// Detect: syndromes should be non-zero.
	syn = RSCalcSyndromes(codeword, nsym)
	if RSSyndromeIsZero(syn) {
		t.Fatal("expected non-zero syndromes after error injection")
	}

	// Locate error using Berlekamp-Massey + Chien search.
	loc := RSBerlekampMassey(syn)
	positions := RSErrorLocatorRoots(loc, len(codeword))

	found := false
	for _, p := range positions {
		if p == errPos {
			found = true
		}
	}
	if !found {
		t.Fatalf("Chien search did not find error at position %d; found %v", errPos, positions)
	}
}
