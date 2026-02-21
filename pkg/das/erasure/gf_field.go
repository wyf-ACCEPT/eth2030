// gf_field.go implements a struct-based GF(2^8) Galois Field with pre-computed
// lookup tables for efficient arithmetic operations used in Reed-Solomon coding.
// The field uses the primitive polynomial 0x11D (x^8 + x^4 + x^3 + x^2 + 1).
//
// This implementation wraps all operations in a GaloisField struct, providing
// Vandermonde matrix generation, matrix-vector multiplication, and polynomial
// evaluation as instance methods.
//
// Reference: consensus-specs/specs/fulu/das-core.md (erasure coding)
package erasure

// GaloisField provides GF(2^8) arithmetic operations using pre-computed
// log/exp, multiplication, and inverse lookup tables. All operations are
// performed modulo the primitive polynomial 0x11D.
type GaloisField struct {
	// logTbl maps non-zero field elements to their discrete log.
	logTbl [256]uint8
	// expTbl maps exponents to field elements (doubled for wraparound).
	expTbl [512]uint8
	// mulTbl is a direct multiplication lookup table.
	mulTbl [256][256]uint8
	// invTbl maps non-zero elements to their multiplicative inverse.
	invTbl [256]uint8
}

// GF(2^8) constants for the struct-based field.
const (
	gfFieldModulus   = 0x11D // x^8 + x^4 + x^3 + x^2 + 1
	gfFieldOrder     = 255   // 2^8 - 1
	gfFieldGenerator = 2     // primitive element
)

// NewGaloisField initializes a new GF(2^8) instance with pre-computed
// log, exp, multiplication, and inverse lookup tables using the primitive
// polynomial 0x11D.
func NewGaloisField() *GaloisField {
	gf := &GaloisField{}
	gf.initTables()
	return gf
}

// initTables pre-computes all lookup tables.
func (gf *GaloisField) initTables() {
	// Build exp and log tables from the generator.
	var x uint16 = 1
	for i := 0; i < gfFieldOrder; i++ {
		gf.expTbl[i] = uint8(x)
		gf.logTbl[x] = uint8(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= gfFieldModulus
		}
	}
	// Fill second half for wraparound.
	for i := 0; i < gfFieldOrder; i++ {
		gf.expTbl[i+gfFieldOrder] = gf.expTbl[i]
	}

	// Build direct multiplication table.
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b++ {
			if a == 0 || b == 0 {
				gf.mulTbl[a][b] = 0
			} else {
				logSum := uint16(gf.logTbl[a]) + uint16(gf.logTbl[b])
				if logSum >= gfFieldOrder {
					logSum -= gfFieldOrder
				}
				gf.mulTbl[a][b] = gf.expTbl[logSum]
			}
		}
	}

	// Build inverse table.
	gf.invTbl[0] = 0
	for a := 1; a < 256; a++ {
		invLog := gfFieldOrder - uint16(gf.logTbl[a])
		if invLog >= gfFieldOrder {
			invLog -= gfFieldOrder
		}
		gf.invTbl[a] = gf.expTbl[invLog]
	}
}

// Mul returns a * b in GF(2^8) using the pre-computed multiplication table.
func (gf *GaloisField) Mul(a, b byte) byte {
	return gf.mulTbl[a][b]
}

// Div returns a / b in GF(2^8). Panics if b is zero.
func (gf *GaloisField) Div(a, b byte) byte {
	if b == 0 {
		panic("erasure/gf_field: division by zero")
	}
	if a == 0 {
		return 0
	}
	logA := uint16(gf.logTbl[a])
	logB := uint16(gf.logTbl[b])
	logResult := (logA + gfFieldOrder - logB) % gfFieldOrder
	return gf.expTbl[logResult]
}

// Inv returns the multiplicative inverse of a in GF(2^8). Panics if a is zero.
func (gf *GaloisField) Inv(a byte) byte {
	if a == 0 {
		panic("erasure/gf_field: inverse of zero")
	}
	return gf.invTbl[a]
}

// Pow returns a^n in GF(2^8) using log/exp tables.
func (gf *GaloisField) Pow(a byte, n int) byte {
	if n == 0 {
		return 1
	}
	if a == 0 {
		return 0
	}
	if n < 0 {
		a = gf.Inv(a)
		n = -n
	}
	logA := uint32(gf.logTbl[a])
	logResult := (logA * uint32(n)) % gfFieldOrder
	return gf.expTbl[logResult]
}

// Exp returns g^i where g is the primitive generator of GF(2^8).
func (gf *GaloisField) Exp(i int) byte {
	idx := i % gfFieldOrder
	if idx < 0 {
		idx += gfFieldOrder
	}
	return gf.expTbl[idx]
}

// Log returns the discrete logarithm of a (base generator). Panics if a is zero.
func (gf *GaloisField) Log(a byte) int {
	if a == 0 {
		panic("erasure/gf_field: log of zero")
	}
	return int(gf.logTbl[a])
}

// Add returns a + b in GF(2^8). Addition in characteristic 2 is XOR.
func (gf *GaloisField) Add(a, b byte) byte {
	return a ^ b
}

// Sub returns a - b in GF(2^8). Subtraction equals addition in characteristic 2.
func (gf *GaloisField) Sub(a, b byte) byte {
	return a ^ b
}

// GenerateVandermondeMatrix generates a rows x cols Vandermonde matrix over
// GF(2^8). Row i has values [1, g^i, (g^i)^2, ..., (g^i)^(cols-1)] where
// g is the primitive generator. This matrix is commonly used as the encoding
// matrix in Reed-Solomon erasure codes.
func (gf *GaloisField) GenerateVandermondeMatrix(rows, cols int) [][]byte {
	if rows <= 0 || cols <= 0 {
		return nil
	}
	matrix := make([][]byte, rows)
	for i := 0; i < rows; i++ {
		matrix[i] = make([]byte, cols)
		x := gf.Exp(i) // evaluation point = g^i
		val := byte(1)
		for j := 0; j < cols; j++ {
			matrix[i][j] = val
			val = gf.Mul(val, x)
		}
	}
	return matrix
}

// MatMul performs matrix-vector multiplication over GF(2^8).
// Given a matrix of dimensions m x n and a vector of length n,
// returns a vector of length m where result[i] = sum_j(matrix[i][j] * data[j]).
// The sum is over GF(2^8), which means XOR.
func (gf *GaloisField) MatMul(matrix [][]byte, data []byte) []byte {
	if len(matrix) == 0 || len(data) == 0 {
		return nil
	}
	rows := len(matrix)
	result := make([]byte, rows)
	for i := 0; i < rows; i++ {
		var acc byte
		row := matrix[i]
		n := len(row)
		if n > len(data) {
			n = len(data)
		}
		for j := 0; j < n; j++ {
			acc ^= gf.Mul(row[j], data[j])
		}
		result[i] = acc
	}
	return result
}

// PolyEval evaluates a polynomial at point x using Horner's method.
// coeffs[0] is the constant term, coeffs[len-1] is the highest degree term.
func (gf *GaloisField) PolyEval(coeffs []byte, x byte) byte {
	if len(coeffs) == 0 {
		return 0
	}
	result := coeffs[len(coeffs)-1]
	for i := len(coeffs) - 2; i >= 0; i-- {
		result = gf.Add(gf.Mul(result, x), coeffs[i])
	}
	return result
}

// PolyMul multiplies two polynomials over GF(2^8).
func (gf *GaloisField) PolyMul(p1, p2 []byte) []byte {
	if len(p1) == 0 || len(p2) == 0 {
		return nil
	}
	result := make([]byte, len(p1)+len(p2)-1)
	for i, a := range p1 {
		for j, b := range p2 {
			result[i+j] ^= gf.Mul(a, b)
		}
	}
	return result
}

// PolyInterpolate performs Lagrange interpolation over GF(2^8).
// Given n points (xs[i], ys[i]), returns a polynomial of degree < n
// such that poly(xs[i]) = ys[i].
func (gf *GaloisField) PolyInterpolate(xs, ys []byte) []byte {
	n := len(xs)
	if n != len(ys) {
		panic("erasure/gf_field: xs and ys must have the same length")
	}
	if n == 0 {
		return nil
	}

	result := make([]byte, n)
	for i := 0; i < n; i++ {
		denom := byte(1)
		for j := 0; j < n; j++ {
			if j != i {
				d := gf.Sub(xs[i], xs[j])
				if d == 0 {
					panic("erasure/gf_field: duplicate x values")
				}
				denom = gf.Mul(denom, d)
			}
		}
		factor := gf.Div(ys[i], denom)

		basis := []byte{1}
		for j := 0; j < n; j++ {
			if j != i {
				term := []byte{xs[j], 1}
				basis = gf.PolyMul(basis, term)
			}
		}

		for k := 0; k < len(basis) && k < n; k++ {
			result[k] ^= gf.Mul(basis[k], factor)
		}
	}
	return result
}
