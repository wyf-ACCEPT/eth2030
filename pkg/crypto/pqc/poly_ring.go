// poly_ring.go implements polynomial arithmetic over the ring Zq[X]/(X^n + 1)
// used in lattice-based cryptographic constructions. The ring parameters
// (n=256, q=3329) match the Kyber/ML-KEM specification.
//
// Operations include addition, subtraction, schoolbook multiplication,
// NTT-based fast multiplication, coefficient reduction, and sampling
// from uniform and centered binomial distributions.
package pqc

import (
	"crypto/sha256"
	"encoding/binary"
)

// Ring parameters matching Kyber (FIPS 203).
const (
	PolyN = 256  // Polynomial degree (X^256 + 1).
	PolyQ = 3329 // Coefficient modulus.
)

// Poly represents a polynomial in Zq[X]/(X^n + 1) with n=256 coefficients.
type Poly struct {
	Coeffs [PolyN]int16
}

// NewPoly creates a zero polynomial.
func NewPoly() *Poly {
	return &Poly{}
}

// NewPolyFromCoeffs creates a polynomial from a coefficient slice.
// If the slice is shorter than PolyN, remaining coefficients are zero.
// If longer, extra coefficients are ignored.
func NewPolyFromCoeffs(coeffs []int16) *Poly {
	p := &Poly{}
	n := len(coeffs)
	if n > PolyN {
		n = PolyN
	}
	for i := 0; i < n; i++ {
		p.Coeffs[i] = polyMod(coeffs[i])
	}
	return p
}

// IsZero returns true if all coefficients are zero.
func (p *Poly) IsZero() bool {
	for _, c := range p.Coeffs {
		if c != 0 {
			return false
		}
	}
	return true
}

// Equal returns true if p and q have the same coefficients.
func (p *Poly) Equal(q *Poly) bool {
	if q == nil {
		return false
	}
	for i := 0; i < PolyN; i++ {
		if polyMod(p.Coeffs[i]) != polyMod(q.Coeffs[i]) {
			return false
		}
	}
	return true
}

// Clone returns a deep copy of the polynomial.
func (p *Poly) Clone() *Poly {
	c := &Poly{}
	copy(c.Coeffs[:], p.Coeffs[:])
	return c
}

// Reduce reduces all coefficients to [0, q).
func (p *Poly) Reduce() {
	for i := range p.Coeffs {
		p.Coeffs[i] = polyMod(p.Coeffs[i])
	}
}

// Add returns p + q mod (X^n + 1, q).
func (p *Poly) Add(q *Poly) *Poly {
	r := &Poly{}
	for i := 0; i < PolyN; i++ {
		r.Coeffs[i] = polyMod(p.Coeffs[i] + q.Coeffs[i])
	}
	return r
}

// Sub returns p - q mod (X^n + 1, q).
func (p *Poly) Sub(q *Poly) *Poly {
	r := &Poly{}
	for i := 0; i < PolyN; i++ {
		r.Coeffs[i] = polyMod(p.Coeffs[i] - q.Coeffs[i])
	}
	return r
}

// MulSchoolbook returns p * q mod (X^n + 1, q) using schoolbook multiplication.
// This is O(n^2) but straightforward and serves as a reference implementation.
func (p *Poly) MulSchoolbook(q *Poly) *Poly {
	r := &Poly{}
	for i := 0; i < PolyN; i++ {
		for j := 0; j < PolyN; j++ {
			prod := int32(p.Coeffs[i]) * int32(q.Coeffs[j])
			idx := i + j
			if idx < PolyN {
				r.Coeffs[idx] = polyMod(r.Coeffs[idx] + int16(prod%int32(PolyQ)))
			} else {
				// X^n = -1 in X^n + 1.
				idx -= PolyN
				r.Coeffs[idx] = polyMod(r.Coeffs[idx] - int16(prod%int32(PolyQ)))
			}
		}
	}
	return r
}

// MulNTT returns p * q using NTT-based fast multiplication.
// Converts to NTT domain, performs pointwise multiply, converts back.
func (p *Poly) MulNTT(q *Poly) *Poly {
	pNTT := polyNTT(p)
	qNTT := polyNTT(q)
	rNTT := polyPointwiseMul(pNTT, qNTT)
	return polyInvNTT(rNTT)
}

// ScalarMul multiplies every coefficient by a scalar mod q.
func (p *Poly) ScalarMul(s int16) *Poly {
	r := &Poly{}
	for i := 0; i < PolyN; i++ {
		r.Coeffs[i] = polyMulMod(p.Coeffs[i], s)
	}
	return r
}

// Negate returns -p mod q.
func (p *Poly) Negate() *Poly {
	r := &Poly{}
	for i := 0; i < PolyN; i++ {
		r.Coeffs[i] = polyMod(-p.Coeffs[i])
	}
	return r
}

// --- NTT operations ---

// polyNTT transforms a polynomial to NTT domain.
func polyNTT(p *Poly) *Poly {
	coeffs := make([]int16, PolyN)
	copy(coeffs, p.Coeffs[:])
	result := NTT(coeffs, PolyQ)
	out := &Poly{}
	copy(out.Coeffs[:], result)
	return out
}

// polyInvNTT transforms from NTT domain back to coefficient domain.
func polyInvNTT(p *Poly) *Poly {
	coeffs := make([]int16, PolyN)
	copy(coeffs, p.Coeffs[:])
	result := InverseNTT(coeffs, PolyQ)
	out := &Poly{}
	copy(out.Coeffs[:], result)
	return out
}

// polyPointwiseMul performs coefficient-wise multiplication in NTT domain.
func polyPointwiseMul(a, b *Poly) *Poly {
	r := &Poly{}
	for i := 0; i < PolyN; i++ {
		r.Coeffs[i] = polyMulMod(a.Coeffs[i], b.Coeffs[i])
	}
	return r
}

// --- Sampling ---

// SampleUniform samples a polynomial with coefficients uniform in [0, q)
// from a seed using SHA-256 as a PRF.
func SampleUniform(seed []byte, nonce byte) *Poly {
	p := &Poly{}
	input := make([]byte, len(seed)+1)
	copy(input, seed)
	input[len(seed)] = nonce

	idx := 0
	state := sha256.Sum256(input)
	for idx < PolyN {
		for b := 0; b+1 < 32 && idx < PolyN; b += 2 {
			val := binary.LittleEndian.Uint16(state[b : b+2])
			val16 := int16(val % uint16(PolyQ))
			p.Coeffs[idx] = val16
			idx++
		}
		if idx < PolyN {
			state = sha256.Sum256(state[:])
		}
	}
	return p
}

// SampleCBD samples a polynomial from the centered binomial distribution
// CBD(eta) using a seed and nonce. Small coefficients in [-eta, eta].
func SampleCBD(seed []byte, nonce byte, eta int) *Poly {
	p := &Poly{}
	input := make([]byte, len(seed)+1)
	copy(input, seed)
	input[len(seed)] = nonce

	// Generate enough random bytes.
	needed := eta * PolyN / 4
	buf := make([]byte, 0, needed+32)
	state := sha256.Sum256(input)
	for len(buf) < needed {
		buf = append(buf, state[:]...)
		state = sha256.Sum256(state[:])
	}

	for i := 0; i < PolyN; i++ {
		var a, b int16
		for j := 0; j < eta; j++ {
			bitIdx := 2*eta*i + j
			byteIdx := bitIdx / 8
			if byteIdx < len(buf) && buf[byteIdx]&(1<<(bitIdx%8)) != 0 {
				a++
			}
		}
		for j := 0; j < eta; j++ {
			bitIdx := 2*eta*i + eta + j
			byteIdx := bitIdx / 8
			if byteIdx < len(buf) && buf[byteIdx]&(1<<(bitIdx%8)) != 0 {
				b++
			}
		}
		p.Coeffs[i] = polyMod(a - b)
	}
	return p
}

// --- Arithmetic helpers ---

// polyMod reduces x modulo PolyQ into [0, q).
func polyMod(x int16) int16 {
	r := int32(x) % int32(PolyQ)
	if r < 0 {
		r += int32(PolyQ)
	}
	return int16(r)
}

// polyMulMod multiplies a and b modulo PolyQ.
func polyMulMod(a, b int16) int16 {
	r := (int32(a) * int32(b)) % int32(PolyQ)
	if r < 0 {
		r += int32(PolyQ)
	}
	return int16(r)
}
