// Package das - galois_field.go implements GF(2^16) finite field arithmetic
// for Reed-Solomon erasure coding. The field uses the irreducible polynomial
// x^16 + x^12 + x^3 + x + 1 (0x1100B) as the reduction polynomial.
//
// All non-zero field elements can be expressed as powers of a primitive
// element (generator), enabling multiplication and division via logarithm
// and antilogarithm lookup tables for O(1) operations.
package das

import "sync"

// GF2_16 represents an element of the Galois field GF(2^16).
// The field has 65536 elements: {0, 1, ..., 65535}.
type GF2_16 uint16

// GF(2^16) constants.
const (
	// gfModulus is the irreducible polynomial x^16 + x^12 + x^3 + x + 1.
	// In binary: 1_0001_0000_0000_1011 = 0x1100B.
	gfModulus = 0x1100B

	// gfOrder is the number of non-zero elements in GF(2^16): 2^16 - 1 = 65535.
	gfOrder = (1 << 16) - 1

	// gfGenerator is the primitive element (generator) of the multiplicative
	// group of GF(2^16). The value 2 is a generator for the polynomial 0x1100B.
	gfGenerator = 2
)

// Log and antilog tables for fast multiplication and division.
// logTable[a] = discrete log base g of a (for a != 0).
// expTable[i] = g^i mod p(x).
// These are initialized once via sync.Once.
var (
	gfLogTable [gfOrder + 1]uint16 // logTable[0] is unused (log(0) undefined)
	gfExpTable [2 * gfOrder]uint16 // doubled for wraparound avoidance
	gfInitOnce sync.Once
)

// initGFTables precomputes the log and antilog (exp) tables for GF(2^16).
// Uses the primitive element gfGenerator and reduces by gfModulus.
func initGFTables() {
	gfInitOnce.Do(func() {
		var x uint32 = 1 // g^0 = 1
		for i := 0; i < gfOrder; i++ {
			gfExpTable[i] = uint16(x)
			gfLogTable[x] = uint16(i)

			// Multiply by the generator in GF(2^16).
			x <<= 1 // x * 2 (shift left = multiply by x in polynomial repr)
			if x&(1<<16) != 0 {
				x ^= gfModulus // reduce by the irreducible polynomial
			}
		}
		// Fill the second half of expTable for easy modular wraparound.
		// This allows us to do expTable[logTable[a] + logTable[b]] without
		// an explicit mod operation.
		for i := 0; i < gfOrder; i++ {
			gfExpTable[i+gfOrder] = gfExpTable[i]
		}
	})
}

// GFAdd returns a + b in GF(2^16). Addition in characteristic 2 is XOR.
func GFAdd(a, b GF2_16) GF2_16 {
	return a ^ b
}

// GFSub returns a - b in GF(2^16). In characteristic 2, subtraction = addition = XOR.
func GFSub(a, b GF2_16) GF2_16 {
	return a ^ b
}

// GFMul returns a * b in GF(2^16) using log/antilog tables.
// Returns 0 if either operand is 0.
func GFMul(a, b GF2_16) GF2_16 {
	if a == 0 || b == 0 {
		return 0
	}
	initGFTables()
	logSum := uint32(gfLogTable[a]) + uint32(gfLogTable[b])
	// The expTable is doubled, so we can index directly without mod.
	return GF2_16(gfExpTable[logSum])
}

// GFDiv returns a / b in GF(2^16). Panics if b is zero.
func GFDiv(a, b GF2_16) GF2_16 {
	if b == 0 {
		panic("das/gf: division by zero in GF(2^16)")
	}
	if a == 0 {
		return 0
	}
	initGFTables()
	// log(a/b) = log(a) - log(b), but we must handle underflow.
	logA := uint32(gfLogTable[a])
	logB := uint32(gfLogTable[b])
	// Add gfOrder to prevent underflow before subtraction.
	logResult := (logA + gfOrder - logB) % gfOrder
	return GF2_16(gfExpTable[logResult])
}

// GFInverse returns the multiplicative inverse of a in GF(2^16).
// Panics if a is zero.
func GFInverse(a GF2_16) GF2_16 {
	if a == 0 {
		panic("das/gf: inverse of zero in GF(2^16)")
	}
	initGFTables()
	// a^{-1} = g^{order - log(a)}.
	logA := uint32(gfLogTable[a])
	invLog := gfOrder - logA
	return GF2_16(gfExpTable[invLog])
}

// GFPow returns a^n in GF(2^16).
// Handles n == 0 (returns 1) and a == 0 (returns 0 for n > 0).
func GFPow(a GF2_16, n int) GF2_16 {
	if n == 0 {
		return 1
	}
	if a == 0 {
		return 0
	}
	if n < 0 {
		// a^(-n) = (a^{-1})^n
		a = GFInverse(a)
		n = -n
	}
	initGFTables()
	logA := uint32(gfLogTable[a])
	// log(a^n) = n * log(a) mod (2^16 - 1)
	logResult := (logA * uint32(n)) % gfOrder
	return GF2_16(gfExpTable[logResult])
}

// GFPolyEval evaluates a polynomial at point x in GF(2^16) using Horner's method.
// coeffs[0] is the constant term, coeffs[1] is the x coefficient, etc.
// Returns 0 for empty coefficient slices.
func GFPolyEval(coeffs []GF2_16, x GF2_16) GF2_16 {
	if len(coeffs) == 0 {
		return 0
	}
	// Horner's method: result = c_n, then result = result * x + c_{n-1}, ...
	result := coeffs[len(coeffs)-1]
	for i := len(coeffs) - 2; i >= 0; i-- {
		result = GFAdd(GFMul(result, x), coeffs[i])
	}
	return result
}

// GFPolyMul multiplies two polynomials in GF(2^16)[x].
// Returns a polynomial whose degree is deg(p1) + deg(p2).
// coeffs[0] is the constant term for both inputs and output.
func GFPolyMul(p1, p2 []GF2_16) []GF2_16 {
	if len(p1) == 0 || len(p2) == 0 {
		return nil
	}
	result := make([]GF2_16, len(p1)+len(p2)-1)
	for i, a := range p1 {
		for j, b := range p2 {
			result[i+j] = GFAdd(result[i+j], GFMul(a, b))
		}
	}
	return result
}

// GFPolyAdd adds two polynomials in GF(2^16)[x].
// The result has length max(len(p1), len(p2)).
func GFPolyAdd(p1, p2 []GF2_16) []GF2_16 {
	maxLen := len(p1)
	if len(p2) > maxLen {
		maxLen = len(p2)
	}
	result := make([]GF2_16, maxLen)
	for i := 0; i < len(p1); i++ {
		result[i] = GFAdd(result[i], p1[i])
	}
	for i := 0; i < len(p2); i++ {
		result[i] = GFAdd(result[i], p2[i])
	}
	return result
}

// GFPolyScale multiplies a polynomial by a scalar in GF(2^16).
func GFPolyScale(poly []GF2_16, scalar GF2_16) []GF2_16 {
	result := make([]GF2_16, len(poly))
	for i, c := range poly {
		result[i] = GFMul(c, scalar)
	}
	return result
}

// GFVandermondeRow computes a row of a Vandermonde matrix: [1, x, x^2, ..., x^{n-1}].
func GFVandermondeRow(x GF2_16, n int) []GF2_16 {
	if n <= 0 {
		return nil
	}
	row := make([]GF2_16, n)
	row[0] = 1
	for i := 1; i < n; i++ {
		row[i] = GFMul(row[i-1], x)
	}
	return row
}

// GFPolyFromRoots constructs a monic polynomial with the given roots.
// For roots [r0, r1, ..., r_{n-1}], returns (x - r0)(x - r1)...(x - r_{n-1}).
// In GF(2^k), subtraction is XOR, so (x - r) = (x + r) = (x ^ r).
// The result has degree n and leading coefficient 1.
func GFPolyFromRoots(roots []GF2_16) []GF2_16 {
	if len(roots) == 0 {
		return []GF2_16{1}
	}
	// Start with (x + roots[0]) = [roots[0], 1].
	poly := []GF2_16{roots[0], 1}
	for i := 1; i < len(roots); i++ {
		// Multiply by (x + roots[i]).
		factor := []GF2_16{roots[i], 1}
		poly = GFPolyMul(poly, factor)
	}
	return poly
}

// GFInterpolate performs Lagrange interpolation over GF(2^16).
// Given n points (xs[i], ys[i]), returns a polynomial of degree < n
// such that poly(xs[i]) = ys[i] for all i.
// Panics if xs and ys have different lengths or if xs contains duplicates.
func GFInterpolate(xs, ys []GF2_16) []GF2_16 {
	n := len(xs)
	if n != len(ys) {
		panic("das/gf: xs and ys must have the same length")
	}
	if n == 0 {
		return nil
	}

	// result starts as the zero polynomial.
	result := make([]GF2_16, n)

	for i := 0; i < n; i++ {
		// Compute the i-th Lagrange basis polynomial L_i(x).
		// L_i(x) = prod_{j!=i} (x - xs[j]) / (xs[i] - xs[j])

		// Compute the denominator: prod_{j!=i} (xs[i] - xs[j]).
		// In GF(2^k), xs[i] - xs[j] = xs[i] ^ xs[j].
		denom := GF2_16(1)
		for j := 0; j < n; j++ {
			if j != i {
				d := GFSub(xs[i], xs[j])
				if d == 0 {
					panic("das/gf: duplicate x values in interpolation")
				}
				denom = GFMul(denom, d)
			}
		}

		// Scale factor: ys[i] / denom.
		factor := GFDiv(ys[i], denom)

		// Build the numerator polynomial: prod_{j!=i} (x - xs[j]).
		basis := []GF2_16{1}
		for j := 0; j < n; j++ {
			if j != i {
				// Multiply basis by (x + xs[j]) = [xs[j], 1].
				term := []GF2_16{xs[j], 1}
				basis = GFPolyMul(basis, term)
			}
		}

		// Add factor * basis to result.
		scaled := GFPolyScale(basis, factor)
		for k := 0; k < len(scaled) && k < n; k++ {
			result[k] = GFAdd(result[k], scaled[k])
		}
	}

	return result
}

// GFExp returns the element g^i where g is the primitive generator.
// i is taken modulo gfOrder.
func GFExp(i int) GF2_16 {
	initGFTables()
	idx := i % gfOrder
	if idx < 0 {
		idx += gfOrder
	}
	return GF2_16(gfExpTable[idx])
}

// GFLog returns the discrete logarithm of a (base generator).
// Panics if a is zero.
func GFLog(a GF2_16) int {
	if a == 0 {
		panic("das/gf: log of zero in GF(2^16)")
	}
	initGFTables()
	return int(gfLogTable[a])
}
