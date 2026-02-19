package das

import (
	"math/big"
)

// BLS12-381 scalar field order (r).
// r = 0x73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001
var blsModulus = func() *big.Int {
	r, _ := new(big.Int).SetString("73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001", 16)
	return r
}()

// FieldElement represents an element of the BLS12-381 scalar field.
// All arithmetic is performed modulo the field order.
type FieldElement struct {
	v *big.Int
}

// NewFieldElement creates a FieldElement from a big.Int, reducing mod p.
func NewFieldElement(v *big.Int) FieldElement {
	r := new(big.Int).Mod(v, blsModulus)
	return FieldElement{v: r}
}

// NewFieldElementFromUint64 creates a FieldElement from a uint64.
func NewFieldElementFromUint64(v uint64) FieldElement {
	return FieldElement{v: new(big.Int).SetUint64(v)}
}

// Zero returns the additive identity.
func FieldZero() FieldElement {
	return FieldElement{v: new(big.Int)}
}

// One returns the multiplicative identity.
func FieldOne() FieldElement {
	return FieldElement{v: big.NewInt(1)}
}

// IsZero returns true if the element is zero.
func (a FieldElement) IsZero() bool {
	return a.v.Sign() == 0
}

// Equal returns true if two field elements are equal.
func (a FieldElement) Equal(b FieldElement) bool {
	return a.v.Cmp(b.v) == 0
}

// BigInt returns a copy of the underlying big.Int.
func (a FieldElement) BigInt() *big.Int {
	return new(big.Int).Set(a.v)
}

// Add returns a + b mod p.
func (a FieldElement) Add(b FieldElement) FieldElement {
	r := new(big.Int).Add(a.v, b.v)
	r.Mod(r, blsModulus)
	return FieldElement{v: r}
}

// Sub returns a - b mod p.
func (a FieldElement) Sub(b FieldElement) FieldElement {
	r := new(big.Int).Sub(a.v, b.v)
	r.Mod(r, blsModulus)
	return FieldElement{v: r}
}

// Mul returns a * b mod p.
func (a FieldElement) Mul(b FieldElement) FieldElement {
	r := new(big.Int).Mul(a.v, b.v)
	r.Mod(r, blsModulus)
	return FieldElement{v: r}
}

// Neg returns -a mod p.
func (a FieldElement) Neg() FieldElement {
	if a.v.Sign() == 0 {
		return FieldZero()
	}
	r := new(big.Int).Sub(blsModulus, a.v)
	return FieldElement{v: r}
}

// Inv returns the multiplicative inverse a^{-1} mod p.
// Returns zero if a is zero.
func (a FieldElement) Inv() FieldElement {
	if a.v.Sign() == 0 {
		return FieldZero()
	}
	r := new(big.Int).ModInverse(a.v, blsModulus)
	return FieldElement{v: r}
}

// Exp returns a^exp mod p.
func (a FieldElement) Exp(exp *big.Int) FieldElement {
	r := new(big.Int).Exp(a.v, exp, blsModulus)
	return FieldElement{v: r}
}

// Div returns a / b mod p (i.e., a * b^{-1}).
func (a FieldElement) Div(b FieldElement) FieldElement {
	return a.Mul(b.Inv())
}

// rootOfUnity computes a primitive n-th root of unity in the BLS12-381 scalar field.
// n must be a power of 2 and must divide (p-1).
// The BLS12-381 scalar field has order p where p-1 = 2^32 * q, so it supports
// roots of unity up to 2^32.
func rootOfUnity(n uint64) FieldElement {
	if n == 0 || n&(n-1) != 0 {
		panic("das: rootOfUnity: n must be a power of 2")
	}

	// Generator of the 2^32 subgroup.
	// g = 5^((p-1)/2^32) mod p
	pMinus1 := new(big.Int).Sub(blsModulus, big.NewInt(1))
	twoTo32 := new(big.Int).Lsh(big.NewInt(1), 32)
	cofactor := new(big.Int).Div(pMinus1, twoTo32)
	g := new(big.Int).Exp(big.NewInt(5), cofactor, blsModulus)

	// To get an n-th root, raise g to the power 2^32 / n.
	exp := new(big.Int).SetUint64(uint64(1) << 32 / n)
	root := new(big.Int).Exp(g, exp, blsModulus)
	return FieldElement{v: root}
}

// computeRootsOfUnity returns the n-th roots of unity [w^0, w^1, ..., w^{n-1}]
// where w is a primitive n-th root of unity.
func computeRootsOfUnity(n uint64) []FieldElement {
	w := rootOfUnity(n)
	roots := make([]FieldElement, n)
	roots[0] = FieldOne()
	for i := uint64(1); i < n; i++ {
		roots[i] = roots[i-1].Mul(w)
	}
	return roots
}

// FFT computes the Number Theoretic Transform (forward FFT) of vals
// over the BLS12-381 scalar field. len(vals) must be a power of 2.
func FFT(vals []FieldElement) []FieldElement {
	n := len(vals)
	if n <= 1 {
		out := make([]FieldElement, n)
		copy(out, vals)
		return out
	}
	if n&(n-1) != 0 {
		panic("das: FFT: length must be a power of 2")
	}
	roots := computeRootsOfUnity(uint64(n))
	return fftInner(vals, roots)
}

// InverseFFT computes the inverse NTT (inverse FFT) of vals.
func InverseFFT(vals []FieldElement) []FieldElement {
	n := len(vals)
	if n <= 1 {
		out := make([]FieldElement, n)
		copy(out, vals)
		return out
	}
	if n&(n-1) != 0 {
		panic("das: InverseFFT: length must be a power of 2")
	}
	roots := computeRootsOfUnity(uint64(n))

	// Inverse roots: reverse the root array (except index 0).
	invRoots := make([]FieldElement, n)
	invRoots[0] = roots[0]
	for i := 1; i < n; i++ {
		invRoots[i] = roots[n-i]
	}

	result := fftInner(vals, invRoots)

	// Divide by n.
	nInv := NewFieldElementFromUint64(uint64(n)).Inv()
	for i := range result {
		result[i] = result[i].Mul(nInv)
	}
	return result
}

// fftInner performs the Cooley-Tukey butterfly FFT using precomputed roots.
func fftInner(vals []FieldElement, roots []FieldElement) []FieldElement {
	n := len(vals)
	if n == 1 {
		return []FieldElement{{v: new(big.Int).Set(vals[0].v)}}
	}

	half := n / 2
	even := make([]FieldElement, half)
	odd := make([]FieldElement, half)
	evenRoots := make([]FieldElement, half)
	for i := 0; i < half; i++ {
		even[i] = vals[2*i]
		odd[i] = vals[2*i+1]
		evenRoots[i] = roots[2*i]
	}

	yEven := fftInner(even, evenRoots)
	yOdd := fftInner(odd, evenRoots)

	result := make([]FieldElement, n)
	for i := 0; i < half; i++ {
		t := roots[i].Mul(yOdd[i])
		result[i] = yEven[i].Add(t)
		result[i+half] = yEven[i].Sub(t)
	}
	return result
}
