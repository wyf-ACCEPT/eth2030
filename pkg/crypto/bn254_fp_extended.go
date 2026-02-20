package crypto

// Complete BN254 field arithmetic extensions: Montgomery form conversion,
// field inversion with edge cases, square root, Legendre symbol, batch
// inversion, field serialization/deserialization, and utility functions.

import (
	"errors"
	"math/big"
)

// BN254 field error sentinels.
var (
	errBN254InvalidField = errors.New("bn254: invalid field element")
	errBN254NoSqrt       = errors.New("bn254: no square root exists")
	errBN254ZeroDivision = errors.New("bn254: division by zero")
)

// Montgomery form parameters for BN254 Fp.
// R = 2^256 mod p, R^2 mod p (for Montgomery multiplication).
var (
	bn254MontR, _  = new(big.Int).SetString("6b0064a1919237eb5ea8b4376e1baf5530e8f84b5f3fa6d1c4c07c918fa7e37", 16)
	bn254MontR2, _ = new(big.Int).SetString("6d89f71cab8351f47ab1eff0a417ff6b5e71911d44501fbf32cfc5b538afa89", 16)
)

// FpElement is a BN254 field element wrapper providing a method-based API.
type FpElement struct {
	v *big.Int
}

// NewFpElement creates a field element from a big.Int, reducing mod p.
func NewFpElement(v *big.Int) *FpElement {
	r := new(big.Int).Mod(v, bn254P)
	if r.Sign() < 0 {
		r.Add(r, bn254P)
	}
	return &FpElement{v: r}
}

// NewFpElementFromUint64 creates a field element from a uint64.
func NewFpElementFromUint64(v uint64) *FpElement {
	return &FpElement{v: new(big.Int).SetUint64(v)}
}

// FpZero returns the additive identity.
func FpZero() *FpElement {
	return &FpElement{v: new(big.Int)}
}

// FpOne returns the multiplicative identity.
func FpOne() *FpElement {
	return &FpElement{v: big.NewInt(1)}
}

// BigInt returns a copy of the internal value.
func (e *FpElement) BigInt() *big.Int {
	return new(big.Int).Set(e.v)
}

// IsZero returns true if the element is zero.
func (e *FpElement) IsZero() bool {
	return e.v.Sign() == 0
}

// IsOne returns true if the element is one.
func (e *FpElement) IsOne() bool {
	return e.v.Cmp(big.NewInt(1)) == 0
}

// Equal returns true if two field elements are equal.
func (e *FpElement) Equal(other *FpElement) bool {
	return e.v.Cmp(other.v) == 0
}

// Add returns e + f mod p.
func (e *FpElement) Add(f *FpElement) *FpElement {
	return &FpElement{v: fpAdd(e.v, f.v)}
}

// Sub returns e - f mod p.
func (e *FpElement) Sub(f *FpElement) *FpElement {
	return &FpElement{v: fpSub(e.v, f.v)}
}

// Mul returns e * f mod p.
func (e *FpElement) Mul(f *FpElement) *FpElement {
	return &FpElement{v: fpMul(e.v, f.v)}
}

// Sqr returns e^2 mod p.
func (e *FpElement) Sqr() *FpElement {
	return &FpElement{v: fpSqr(e.v)}
}

// Neg returns -e mod p.
func (e *FpElement) Neg() *FpElement {
	return &FpElement{v: fpNeg(e.v)}
}

// Inv returns e^{-1} mod p. Returns nil if e is zero.
func (e *FpElement) Inv() *FpElement {
	if e.IsZero() {
		return nil
	}
	return &FpElement{v: fpInv(e.v)}
}

// Exp returns e^exp mod p.
func (e *FpElement) Exp(exp *big.Int) *FpElement {
	return &FpElement{v: fpExp(e.v, exp)}
}

// fpSqrt returns the square root of a mod p, or nil if none exists.
// BN254's p satisfies p = 3 mod 4, so sqrt(a) = a^((p+1)/4) mod p.
func fpSqrt(a *big.Int) *big.Int {
	if a.Sign() == 0 {
		return new(big.Int)
	}
	amod := new(big.Int).Mod(a, bn254P)
	// p = 3 mod 4: sqrt(a) = a^((p+1)/4)
	exp := new(big.Int).Add(bn254P, big.NewInt(1))
	exp.Rsh(exp, 2)
	r := new(big.Int).Exp(amod, exp, bn254P)
	// Verify: r^2 == a mod p
	if new(big.Int).Mul(r, r).Mod(new(big.Int).Mul(r, r), bn254P).Cmp(amod) != 0 {
		return nil
	}
	return r
}

// Sqrt returns the square root of e mod p, or nil if e is not a QR.
func (e *FpElement) Sqrt() *FpElement {
	r := fpSqrt(e.v)
	if r == nil {
		return nil
	}
	return &FpElement{v: r}
}

// fpLegendreSymbol returns the Legendre symbol (a/p):
//
//	1  if a is a non-zero quadratic residue mod p
//	-1 if a is a non-residue mod p
//	0  if a == 0 mod p
func fpLegendreSymbol(a *big.Int) int {
	if a.Sign() == 0 || new(big.Int).Mod(a, bn254P).Sign() == 0 {
		return 0
	}
	exp := new(big.Int).Sub(bn254P, big.NewInt(1))
	exp.Rsh(exp, 1) // (p-1)/2
	r := fpExp(a, exp)
	if r.Cmp(big.NewInt(1)) == 0 {
		return 1
	}
	// r == p-1 means -1.
	return -1
}

// LegendreSymbol returns the Legendre symbol (e / p).
func (e *FpElement) LegendreSymbol() int {
	return fpLegendreSymbol(e.v)
}

// IsQuadraticResidue returns true if e is a QR mod p (or zero).
func (e *FpElement) IsQuadraticResidue() bool {
	ls := fpLegendreSymbol(e.v)
	return ls == 0 || ls == 1
}

// fpBatchInverse computes the modular inverse of each element in the slice
// using Montgomery's batch inversion trick, requiring only one modular
// inversion and O(n) multiplications. This is significantly faster than
// computing n individual inversions.
func fpBatchInverse(elems []*big.Int) ([]*big.Int, error) {
	n := len(elems)
	if n == 0 {
		return nil, nil
	}

	// Check for zeros.
	for i, e := range elems {
		if e.Sign() == 0 || new(big.Int).Mod(e, bn254P).Sign() == 0 {
			return nil, errors.New("bn254: zero element in batch at index " + big.NewInt(int64(i)).String())
		}
	}

	// Compute prefix products: prefix[i] = prod(elems[0..i]) mod p.
	prefix := make([]*big.Int, n)
	prefix[0] = new(big.Int).Mod(elems[0], bn254P)
	for i := 1; i < n; i++ {
		prefix[i] = fpMul(prefix[i-1], elems[i])
	}

	// Compute the inverse of the total product.
	totalInv := fpInv(prefix[n-1])
	if totalInv == nil {
		return nil, errBN254ZeroDivision
	}

	// Unwind to get individual inverses.
	result := make([]*big.Int, n)
	for i := n - 1; i > 0; i-- {
		result[i] = fpMul(totalInv, prefix[i-1])
		totalInv = fpMul(totalInv, elems[i])
	}
	result[0] = totalInv

	return result, nil
}

// FpBatchInverse computes batch modular inverse using Montgomery's trick.
func FpBatchInverse(elems []*FpElement) ([]*FpElement, error) {
	raws := make([]*big.Int, len(elems))
	for i, e := range elems {
		raws[i] = e.v
	}
	invs, err := fpBatchInverse(raws)
	if err != nil {
		return nil, err
	}
	result := make([]*FpElement, len(invs))
	for i, inv := range invs {
		result[i] = &FpElement{v: inv}
	}
	return result, nil
}

// fpSerialize writes a field element as a 32-byte big-endian byte slice.
func fpSerialize(a *big.Int) []byte {
	out := make([]byte, 32)
	b := new(big.Int).Mod(a, bn254P).Bytes()
	copy(out[32-len(b):], b)
	return out
}

// fpDeserialize reads a 32-byte big-endian field element and validates it.
func fpDeserialize(data []byte) (*big.Int, error) {
	if len(data) != 32 {
		return nil, errBN254InvalidField
	}
	v := new(big.Int).SetBytes(data)
	if v.Cmp(bn254P) >= 0 {
		return nil, errBN254InvalidField
	}
	return v, nil
}

// Serialize writes e as a 32-byte big-endian representation.
func (e *FpElement) Serialize() []byte {
	return fpSerialize(e.v)
}

// FpDeserialize reads a field element from 32 bytes.
func FpDeserialize(data []byte) (*FpElement, error) {
	v, err := fpDeserialize(data)
	if err != nil {
		return nil, err
	}
	return &FpElement{v: v}, nil
}

// ToMontgomery converts a standard field element to Montgomery form.
// mont(a) = a * R mod p, where R = 2^256 mod p.
func (e *FpElement) ToMontgomery() *FpElement {
	return &FpElement{v: fpMul(e.v, bn254MontR)}
}

// FromMontgomery converts a Montgomery form element back to standard form.
// a = mont(a) * R^{-1} mod p.
func (e *FpElement) FromMontgomery() *FpElement {
	rInv := fpInv(bn254MontR)
	return &FpElement{v: fpMul(e.v, rInv)}
}

// fpDiv computes a / b mod p. Returns error if b is zero.
func fpDiv(a, b *big.Int) (*big.Int, error) {
	if b.Sign() == 0 || new(big.Int).Mod(b, bn254P).Sign() == 0 {
		return nil, errBN254ZeroDivision
	}
	bInv := fpInv(b)
	return fpMul(a, bInv), nil
}

// Div returns e / f mod p.
func (e *FpElement) Div(f *FpElement) (*FpElement, error) {
	r, err := fpDiv(e.v, f.v)
	if err != nil {
		return nil, err
	}
	return &FpElement{v: r}, nil
}

// fpDouble returns 2*a mod p (cheaper than general addition).
func fpDouble(a *big.Int) *big.Int {
	r := new(big.Int).Lsh(a, 1)
	if r.Cmp(bn254P) >= 0 {
		r.Sub(r, bn254P)
	}
	return r
}

// Double returns 2*e mod p.
func (e *FpElement) Double() *FpElement {
	return &FpElement{v: fpDouble(e.v)}
}

// fpMultiExp computes sum(bases[i] * scalars[i]) mod p using the simple
// multiply-and-accumulate strategy.
func fpMultiExp(bases, scalars []*big.Int) *big.Int {
	if len(bases) != len(scalars) {
		return new(big.Int)
	}
	result := new(big.Int)
	for i := range bases {
		term := fpMul(bases[i], scalars[i])
		result = fpAdd(result, term)
	}
	return result
}

// FpMultiExp computes sum(bases[i] * scalars[i]) mod p.
func FpMultiExp(bases, scalars []*FpElement) *FpElement {
	if len(bases) != len(scalars) {
		return FpZero()
	}
	bRaw := make([]*big.Int, len(bases))
	sRaw := make([]*big.Int, len(scalars))
	for i := range bases {
		bRaw[i] = bases[i].v
		sRaw[i] = scalars[i].v
	}
	return &FpElement{v: fpMultiExp(bRaw, sRaw)}
}

// fpSign returns the "sign" of a field element: 0 if even, 1 if odd.
func fpSign(a *big.Int) int {
	return int(new(big.Int).Mod(a, bn254P).Bit(0))
}

// Sign returns the parity bit of e (0 = even, 1 = odd).
func (e *FpElement) Sign() int {
	return fpSign(e.v)
}
