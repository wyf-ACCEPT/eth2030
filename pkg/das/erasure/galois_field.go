// galois_field.go implements GF(2^8) Galois field arithmetic for Reed-Solomon
// erasure coding. The field uses the irreducible polynomial x^8 + x^4 + x^3 + x^2 + 1
// (0x11D) which is standard for GF(2^8) in storage/coding applications.
//
// All non-zero field elements are expressed as powers of the primitive element
// (generator = 2), enabling O(1) multiplication and division via pre-computed
// logarithm and antilogarithm lookup tables.
//
// Operations provided:
//   - Field addition/subtraction (XOR in characteristic 2)
//   - Multiplication and division via log/exp tables
//   - Polynomial evaluation (Horner's method)
//   - Lagrange interpolation over GF(2^8)
//   - Pre-computed lookup tables for performance
package erasure

import "sync"

// GF256 represents an element of the Galois field GF(2^8).
// The field has 256 elements: {0, 1, ..., 255}.
type GF256 uint8

// GF(2^8) constants.
const (
	// gf256Modulus is the irreducible polynomial x^8 + x^4 + x^3 + x^2 + 1.
	// In binary: 1_0001_1101 = 0x11D. Only the low 8 bits are used for
	// reduction after multiplication.
	gf256Modulus = 0x11D

	// gf256Order is the number of non-zero elements in GF(2^8): 2^8 - 1 = 255.
	gf256Order = 255

	// gf256Generator is the primitive element of GF(2^8) for polynomial 0x11D.
	// The value 2 generates all 255 non-zero elements.
	gf256Generator = 2
)

// Pre-computed log and exp tables for GF(2^8).
var (
	gf256LogTable [256]uint8      // gf256LogTable[a] = discrete log base g of a
	gf256ExpTable [512]uint8      // doubled for wraparound avoidance
	gf256MulTable [256][256]uint8 // direct multiplication lookup
	gf256InvTable [256]uint8      // multiplicative inverse table
	gf256InitOnce sync.Once
)

// initGF256Tables precomputes log, exp, multiplication, and inverse tables.
func initGF256Tables() {
	gf256InitOnce.Do(func() {
		// Build exp and log tables from generator.
		var x uint16 = 1 // g^0 = 1
		for i := 0; i < gf256Order; i++ {
			gf256ExpTable[i] = uint8(x)
			gf256LogTable[x] = uint8(i)

			// Multiply by generator in GF(2^8).
			x <<= 1
			if x&0x100 != 0 {
				x ^= gf256Modulus
			}
		}
		// Fill second half for easy modular wraparound.
		for i := 0; i < gf256Order; i++ {
			gf256ExpTable[i+gf256Order] = gf256ExpTable[i]
		}

		// Build direct multiplication table.
		for a := 0; a < 256; a++ {
			for b := 0; b < 256; b++ {
				if a == 0 || b == 0 {
					gf256MulTable[a][b] = 0
				} else {
					logSum := uint16(gf256LogTable[a]) + uint16(gf256LogTable[b])
					if logSum >= gf256Order {
						logSum -= gf256Order
					}
					gf256MulTable[a][b] = gf256ExpTable[logSum]
				}
			}
		}

		// Build inverse table.
		gf256InvTable[0] = 0 // inverse of 0 is undefined, store 0
		for a := 1; a < 256; a++ {
			invLog := gf256Order - uint16(gf256LogTable[a])
			if invLog >= gf256Order {
				invLog -= gf256Order
			}
			gf256InvTable[a] = gf256ExpTable[invLog]
		}
	})
}

// GF256Add returns a + b in GF(2^8). Addition in characteristic 2 is XOR.
func GF256Add(a, b GF256) GF256 {
	return a ^ b
}

// GF256Sub returns a - b in GF(2^8). Subtraction equals addition (XOR)
// in characteristic 2.
func GF256Sub(a, b GF256) GF256 {
	return a ^ b
}

// GF256Mul returns a * b in GF(2^8) using the pre-computed multiplication table.
func GF256Mul(a, b GF256) GF256 {
	initGF256Tables()
	return GF256(gf256MulTable[a][b])
}

// GF256Div returns a / b in GF(2^8). Panics if b is zero.
func GF256Div(a, b GF256) GF256 {
	if b == 0 {
		panic("erasure/gf256: division by zero")
	}
	if a == 0 {
		return 0
	}
	initGF256Tables()
	logA := uint16(gf256LogTable[a])
	logB := uint16(gf256LogTable[b])
	logResult := (logA + gf256Order - logB) % gf256Order
	return GF256(gf256ExpTable[logResult])
}

// GF256Inverse returns the multiplicative inverse of a in GF(2^8).
// Panics if a is zero.
func GF256Inverse(a GF256) GF256 {
	if a == 0 {
		panic("erasure/gf256: inverse of zero")
	}
	initGF256Tables()
	return GF256(gf256InvTable[a])
}

// GF256Pow returns a^n in GF(2^8) using log/exp tables.
func GF256Pow(a GF256, n int) GF256 {
	if n == 0 {
		return 1
	}
	if a == 0 {
		return 0
	}
	initGF256Tables()
	if n < 0 {
		a = GF256Inverse(a)
		n = -n
	}
	logA := uint32(gf256LogTable[a])
	logResult := (logA * uint32(n)) % gf256Order
	return GF256(gf256ExpTable[logResult])
}

// GF256Exp returns g^i where g is the primitive generator.
func GF256Exp(i int) GF256 {
	initGF256Tables()
	idx := i % gf256Order
	if idx < 0 {
		idx += gf256Order
	}
	return GF256(gf256ExpTable[idx])
}

// GF256Log returns the discrete logarithm of a (base generator).
// Panics if a is zero.
func GF256Log(a GF256) int {
	if a == 0 {
		panic("erasure/gf256: log of zero")
	}
	initGF256Tables()
	return int(gf256LogTable[a])
}

// GF256PolyEval evaluates a polynomial at point x using Horner's method.
// coeffs[0] is the constant term, coeffs[len-1] is the highest degree term.
func GF256PolyEval(coeffs []GF256, x GF256) GF256 {
	if len(coeffs) == 0 {
		return 0
	}
	result := coeffs[len(coeffs)-1]
	for i := len(coeffs) - 2; i >= 0; i-- {
		result = GF256Add(GF256Mul(result, x), coeffs[i])
	}
	return result
}

// GF256PolyMul multiplies two polynomials in GF(2^8)[x].
func GF256PolyMul(p1, p2 []GF256) []GF256 {
	if len(p1) == 0 || len(p2) == 0 {
		return nil
	}
	result := make([]GF256, len(p1)+len(p2)-1)
	for i, a := range p1 {
		for j, b := range p2 {
			result[i+j] = GF256Add(result[i+j], GF256Mul(a, b))
		}
	}
	return result
}

// GF256PolyAdd adds two polynomials in GF(2^8)[x].
func GF256PolyAdd(p1, p2 []GF256) []GF256 {
	maxLen := len(p1)
	if len(p2) > maxLen {
		maxLen = len(p2)
	}
	result := make([]GF256, maxLen)
	for i := 0; i < len(p1); i++ {
		result[i] = GF256Add(result[i], p1[i])
	}
	for i := 0; i < len(p2); i++ {
		result[i] = GF256Add(result[i], p2[i])
	}
	return result
}

// GF256PolyScale multiplies a polynomial by a scalar in GF(2^8).
func GF256PolyScale(poly []GF256, scalar GF256) []GF256 {
	result := make([]GF256, len(poly))
	for i, c := range poly {
		result[i] = GF256Mul(c, scalar)
	}
	return result
}

// GF256Interpolate performs Lagrange interpolation over GF(2^8).
// Given n points (xs[i], ys[i]), returns a polynomial of degree < n such
// that poly(xs[i]) = ys[i] for all i.
// Panics if xs and ys have different lengths or if xs contains duplicates.
func GF256Interpolate(xs, ys []GF256) []GF256 {
	n := len(xs)
	if n != len(ys) {
		panic("erasure/gf256: xs and ys must have the same length")
	}
	if n == 0 {
		return nil
	}

	result := make([]GF256, n)

	for i := 0; i < n; i++ {
		// Compute denominator: prod_{j!=i} (xs[i] - xs[j]).
		denom := GF256(1)
		for j := 0; j < n; j++ {
			if j != i {
				d := GF256Sub(xs[i], xs[j])
				if d == 0 {
					panic("erasure/gf256: duplicate x values in interpolation")
				}
				denom = GF256Mul(denom, d)
			}
		}

		// Scale factor: ys[i] / denom.
		factor := GF256Div(ys[i], denom)

		// Build numerator polynomial: prod_{j!=i} (x - xs[j]).
		basis := []GF256{1}
		for j := 0; j < n; j++ {
			if j != i {
				term := []GF256{xs[j], 1}
				basis = GF256PolyMul(basis, term)
			}
		}

		// Add factor * basis to result.
		scaled := GF256PolyScale(basis, factor)
		for k := 0; k < len(scaled) && k < n; k++ {
			result[k] = GF256Add(result[k], scaled[k])
		}
	}

	return result
}

// GF256VandermondeRow computes [1, x, x^2, ..., x^{n-1}] in GF(2^8).
func GF256VandermondeRow(x GF256, n int) []GF256 {
	if n <= 0 {
		return nil
	}
	row := make([]GF256, n)
	row[0] = 1
	for i := 1; i < n; i++ {
		row[i] = GF256Mul(row[i-1], x)
	}
	return row
}

// GF256PolyFromRoots constructs a monic polynomial with the given roots.
// Returns (x - r0)(x - r1)...(x - r_{n-1}).
func GF256PolyFromRoots(roots []GF256) []GF256 {
	if len(roots) == 0 {
		return []GF256{1}
	}
	poly := []GF256{roots[0], 1}
	for i := 1; i < len(roots); i++ {
		factor := []GF256{roots[i], 1}
		poly = GF256PolyMul(poly, factor)
	}
	return poly
}
