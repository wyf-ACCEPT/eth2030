// polynomial_ops.go implements advanced polynomial operations over GF(2^8)
// for Reed-Solomon erasure coding. It provides syndrome calculation, generator
// polynomial computation, formal derivatives, error locator polynomials via
// the Berlekamp-Massey algorithm, Chien search for error locations, and
// Forney's algorithm for error magnitudes.
//
// These operations are the core building blocks for a full Reed-Solomon
// error correction decoder that can both detect and correct errors in
// coded data. The syndrome-based approach is complementary to the
// interpolation-based approach in reed_solomon_encoder.go.
//
// Key operations:
//   - Syndrome computation: evaluates received polynomial at roots of g(x)
//   - Generator polynomial: constructs g(x) = prod(x - a^i) for RS codes
//   - Berlekamp-Massey: finds the error locator polynomial from syndromes
//   - Chien search: finds error locations from the error locator polynomial
//   - Forney's algorithm: computes error magnitudes at known positions
//   - Formal derivative: computes d/dx of a polynomial over GF(2^8)
//
// Reference: Blahut, "Theory and Practice of Error Control Codes" (1983)
package erasure

// RSGeneratorPoly computes the Reed-Solomon generator polynomial of degree
// nsym over GF(2^8). The generator polynomial is:
//
//	g(x) = (x - a^0)(x - a^1)...(x - a^{nsym-1})
//
// where a is the primitive element (generator = 2) of GF(2^8). The returned
// slice contains coefficients from lowest to highest degree, with the
// leading coefficient (x^nsym) being 1.
func RSGeneratorPoly(nsym int) []GF256 {
	if nsym <= 0 {
		return []GF256{1}
	}
	initGF256Tables()

	gen := []GF256{1}
	for i := 0; i < nsym; i++ {
		root := GF256Exp(i)
		factor := []GF256{root, 1} // (x - a^i) = (a^i + x) in char 2
		gen = GF256PolyMul(gen, factor)
	}
	return gen
}

// RSCalcSyndromes computes nsym syndromes for the received message polynomial.
// Syndrome i = msg(a^i) for i = 0, 1, ..., nsym-1. If all syndromes are zero,
// the message has no detectable errors.
func RSCalcSyndromes(msg []GF256, nsym int) []GF256 {
	if nsym <= 0 || len(msg) == 0 {
		return nil
	}
	initGF256Tables()

	syndromes := make([]GF256, nsym)
	for i := 0; i < nsym; i++ {
		syndromes[i] = GF256PolyEval(msg, GF256Exp(i))
	}
	return syndromes
}

// RSSyndromeIsZero returns true if all syndromes are zero, indicating no
// errors in the received message.
func RSSyndromeIsZero(syndromes []GF256) bool {
	for _, s := range syndromes {
		if s != 0 {
			return false
		}
	}
	return true
}

// RSBerlekampMassey implements the Berlekamp-Massey algorithm to find the
// error locator polynomial Lambda(x) from the given syndromes. The error
// locator polynomial has roots at the inverse positions of errors.
//
// Returns the error locator polynomial with coefficients from constant term
// to highest degree.
func RSBerlekampMassey(syndromes []GF256) []GF256 {
	n := len(syndromes)
	if n == 0 {
		return []GF256{1}
	}
	initGF256Tables()

	// Current error locator polynomial.
	errLoc := []GF256{1}
	// Previous error locator polynomial.
	oldLoc := []GF256{1}

	for i := 0; i < n; i++ {
		// Compute discrepancy delta.
		delta := syndromes[i]
		for j := 1; j < len(errLoc); j++ {
			if i-j >= 0 {
				delta = GF256Add(delta, GF256Mul(errLoc[j], syndromes[i-j]))
			}
		}

		// Shift old locator: multiply by x.
		oldLoc = append([]GF256{0}, oldLoc...)

		if delta != 0 {
			if len(oldLoc) > len(errLoc) {
				// Update: swap roles.
				newLoc := GF256PolyScale(oldLoc, delta)
				oldLoc = GF256PolyScale(errLoc, GF256Inverse(delta))
				errLoc = newLoc
			}
			// Adjust errLoc.
			adj := GF256PolyScale(oldLoc, delta)
			errLoc = GF256PolyAdd(errLoc, adj)
		}
	}

	return errLoc
}

// RSErrorLocatorRoots finds the roots of the error locator polynomial using
// Chien search. It evaluates Lambda(x) at x = a^(-i) for i = 0..254 and
// returns the positions where Lambda evaluates to zero. Each root a^(-j)
// corresponds to an error at position j.
func RSErrorLocatorRoots(errLoc []GF256, msgLen int) []int {
	if len(errLoc) <= 1 {
		return nil
	}
	initGF256Tables()

	var positions []int
	for i := 0; i < msgLen; i++ {
		// Evaluate at a^(-i) = a^(255-i).
		x := GF256Exp(gf256Order - i)
		val := GF256PolyEval(errLoc, x)
		if val == 0 {
			positions = append(positions, i)
		}
	}
	return positions
}

// RSFormalDerivative computes the formal derivative of a polynomial over
// GF(2^8). In characteristic 2, odd-degree terms survive and even-degree
// terms vanish:
//
//	d/dx (a0 + a1*x + a2*x^2 + a3*x^3 + ...) = a1 + a3*x^2 + a5*x^4 + ...
//
// The returned polynomial has degree one less than the input (or empty
// if the input is constant).
func RSFormalDerivative(poly []GF256) []GF256 {
	if len(poly) <= 1 {
		return nil
	}

	result := make([]GF256, len(poly)-1)
	for i := 1; i < len(poly); i++ {
		// In characteristic 2, coefficient i of the derivative is:
		// i * poly[i] where "i" means the GF(2) value of i.
		// Odd i: coefficient = poly[i] (since i mod 2 = 1).
		// Even i: coefficient = 0 (since i mod 2 = 0).
		if i%2 == 1 {
			result[i-1] = poly[i]
		}
		// Even indices contribute 0, which is the default.
	}
	return result
}

// RSForneyAlgorithm computes error magnitudes at known error positions using
// Forney's algorithm. Given the syndromes, error locator polynomial, and
// error positions, it returns the error values at each position.
//
// The formula is: e_j = -Omega(X_j^{-1}) / Lambda'(X_j^{-1})
// where Omega is the error evaluator polynomial and Lambda' is the formal
// derivative of the error locator.
func RSForneyAlgorithm(syndromes []GF256, errLoc []GF256, positions []int) []GF256 {
	if len(positions) == 0 {
		return nil
	}
	initGF256Tables()

	// Compute the error evaluator polynomial: Omega(x) = S(x)*Lambda(x) mod x^nsym.
	omega := rsTruncatedMul(syndromes, errLoc, len(syndromes))

	// Compute the formal derivative of the error locator.
	lambdaPrime := RSFormalDerivative(errLoc)
	if len(lambdaPrime) == 0 {
		return make([]GF256, len(positions))
	}

	magnitudes := make([]GF256, len(positions))
	for i, pos := range positions {
		// X_j^{-1} = a^(-pos) = a^(255-pos).
		xiInv := GF256Exp(gf256Order - pos)

		// Evaluate Omega at X_j^{-1}.
		omegaVal := GF256PolyEval(omega, xiInv)

		// Evaluate Lambda' at X_j^{-1}.
		lpVal := GF256PolyEval(lambdaPrime, xiInv)
		if lpVal == 0 {
			// Degenerate case: skip (error magnitude is 0).
			continue
		}

		// In GF(2^8), negation is identity, so -Omega/Lambda' = Omega/Lambda'.
		magnitudes[i] = GF256Div(omegaVal, lpVal)
	}
	return magnitudes
}

// RSEncodeSystematic performs systematic Reed-Solomon encoding: the message
// symbols appear unchanged at the front of the codeword, followed by nsym
// parity symbols. The parity is computed as the remainder of the message
// polynomial divided by the generator polynomial.
func RSEncodeSystematic(msg []GF256, nsym int) []GF256 {
	if nsym <= 0 || len(msg) == 0 {
		return msg
	}
	initGF256Tables()

	gen := RSGeneratorPoly(nsym)

	// Shift message up by nsym positions: msg(x) * x^nsym.
	shifted := make([]GF256, len(msg)+nsym)
	copy(shifted[nsym:], msg)

	// Compute remainder: shifted mod gen.
	remainder := gf256PolyMod(shifted, gen)

	// Codeword = shifted + remainder (which puts remainder in low positions).
	codeword := make([]GF256, len(shifted))
	copy(codeword, remainder)
	copy(codeword[nsym:], msg)
	return codeword
}

// RSEvaluatorPoly computes the error evaluator polynomial Omega(x) from the
// syndromes and error locator: Omega(x) = S(x) * Lambda(x) mod x^nsym.
func RSEvaluatorPoly(syndromes []GF256, errLoc []GF256, nsym int) []GF256 {
	return rsTruncatedMul(syndromes, errLoc, nsym)
}

// RSPolyDegree returns the degree of a polynomial (index of highest non-zero
// coefficient). Returns -1 for a zero or empty polynomial.
func RSPolyDegree(poly []GF256) int {
	for i := len(poly) - 1; i >= 0; i-- {
		if poly[i] != 0 {
			return i
		}
	}
	return -1
}

// RSPolyNormalize removes leading zero coefficients from a polynomial,
// returning a polynomial with no unnecessary high-order zeros.
func RSPolyNormalize(poly []GF256) []GF256 {
	deg := RSPolyDegree(poly)
	if deg < 0 {
		return []GF256{0}
	}
	return poly[:deg+1]
}

// RSPolyDiv divides polynomial a by polynomial b over GF(2^8), returning
// the quotient and remainder. Returns (nil, nil) if b is zero.
func RSPolyDiv(a, b []GF256) (quotient, remainder []GF256) {
	bDeg := RSPolyDegree(b)
	if bDeg < 0 {
		return nil, nil // division by zero polynomial
	}
	initGF256Tables()

	aDeg := RSPolyDegree(a)
	if aDeg < bDeg {
		return []GF256{0}, append([]GF256(nil), a...)
	}

	// Work on a copy.
	rem := make([]GF256, len(a))
	copy(rem, a)

	quotLen := aDeg - bDeg + 1
	quot := make([]GF256, quotLen)
	bLead := b[bDeg]

	for i := aDeg; i >= bDeg; i-- {
		if rem[i] == 0 {
			continue
		}
		coeff := GF256Div(rem[i], bLead)
		quot[i-bDeg] = coeff

		for j := 0; j <= bDeg; j++ {
			rem[i-bDeg+j] = GF256Add(rem[i-bDeg+j], GF256Mul(coeff, b[j]))
		}
	}

	remainder = RSPolyNormalize(rem[:bDeg+1])
	return quot, remainder
}

// RSPolyGCD computes the greatest common divisor of two polynomials over
// GF(2^8) using the Euclidean algorithm.
func RSPolyGCD(a, b []GF256) []GF256 {
	a = RSPolyNormalize(a)
	b = RSPolyNormalize(b)

	for RSPolyDegree(b) >= 0 && !(len(b) == 1 && b[0] == 0) {
		_, r := RSPolyDiv(a, b)
		a = b
		b = r
	}
	return RSPolyNormalize(a)
}

// --- internal helpers ---

// rsTruncatedMul multiplies two polynomials and truncates the result to
// maxLen terms. This is used for computing Omega(x) = S(x)*Lambda(x) mod x^nsym.
func rsTruncatedMul(p1, p2 []GF256, maxLen int) []GF256 {
	product := GF256PolyMul(p1, p2)
	if len(product) > maxLen {
		return product[:maxLen]
	}
	return product
}

// gf256PolyMod computes dividend mod divisor over GF(2^8).
func gf256PolyMod(dividend, divisor []GF256) []GF256 {
	divDeg := RSPolyDegree(divisor)
	if divDeg < 0 {
		return nil
	}

	rem := make([]GF256, len(dividend))
	copy(rem, dividend)

	divLead := divisor[divDeg]
	remDeg := RSPolyDegree(rem)

	for remDeg >= divDeg {
		coeff := GF256Div(rem[remDeg], divLead)
		for j := 0; j <= divDeg; j++ {
			rem[remDeg-divDeg+j] = GF256Add(rem[remDeg-divDeg+j], GF256Mul(coeff, divisor[j]))
		}
		remDeg = RSPolyDegree(rem)
	}

	if divDeg > len(rem) {
		return rem
	}
	return rem[:divDeg]
}
