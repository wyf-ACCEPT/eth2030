package crypto

// BLS12-381 finite field arithmetic over F_p.
//
// The BLS12-381 curve is defined over F_p where:
//   p = 0x1a0111ea397fe69a4b1ba7b6434bacd764774b84f38512bf6730d2a0f6b0f6241eabfffeb153ffffb9feffffffffaaab
//
// This file provides modular arithmetic primitives for the base field.

import "math/big"

// BLS12-381 curve parameters.
var (
	// blsP is the base field modulus.
	blsP, _ = new(big.Int).SetString(
		"1a0111ea397fe69a4b1ba7b6434bacd764774b84f38512bf6730d2a0f6b0f6241eabfffeb153ffffb9feffffffffaaab", 16)
	// blsR is the subgroup order.
	blsR, _ = new(big.Int).SetString(
		"73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001", 16)
	// blsB is the curve coefficient b = 4 for G1: y^2 = x^3 + 4.
	blsB = big.NewInt(4)
)

// blsFpAdd returns (a + b) mod p.
func blsFpAdd(a, b *big.Int) *big.Int {
	r := new(big.Int).Add(a, b)
	return r.Mod(r, blsP)
}

// blsFpSub returns (a - b) mod p.
func blsFpSub(a, b *big.Int) *big.Int {
	r := new(big.Int).Sub(a, b)
	return r.Mod(r, blsP)
}

// blsFpMul returns (a * b) mod p.
func blsFpMul(a, b *big.Int) *big.Int {
	r := new(big.Int).Mul(a, b)
	return r.Mod(r, blsP)
}

// blsFpNeg returns (-a) mod p.
func blsFpNeg(a *big.Int) *big.Int {
	if a.Sign() == 0 {
		return new(big.Int)
	}
	return new(big.Int).Sub(blsP, new(big.Int).Mod(a, blsP))
}

// blsFpInv returns a^(-1) mod p.
func blsFpInv(a *big.Int) *big.Int {
	return new(big.Int).ModInverse(a, blsP)
}

// blsFpSqr returns a^2 mod p.
func blsFpSqr(a *big.Int) *big.Int {
	r := new(big.Int).Mul(a, a)
	return r.Mod(r, blsP)
}

// blsFpExp returns a^e mod p.
func blsFpExp(a, e *big.Int) *big.Int {
	return new(big.Int).Exp(a, e, blsP)
}

// blsFpSqrt returns a square root of a mod p, or nil if none exists.
// Uses the Tonelli-Shanks algorithm. For BLS12-381, p = 3 mod 4,
// so sqrt(a) = a^((p+1)/4) mod p.
func blsFpSqrt(a *big.Int) *big.Int {
	if a.Sign() == 0 {
		return new(big.Int)
	}
	// For p = 3 mod 4: sqrt(a) = a^((p+1)/4)
	exp := new(big.Int).Add(blsP, big.NewInt(1))
	exp.Rsh(exp, 2)
	r := blsFpExp(a, exp)
	// Verify: r^2 == a mod p
	if blsFpSqr(r).Cmp(new(big.Int).Mod(a, blsP)) != 0 {
		return nil
	}
	return r
}

// blsFpIsSquare checks if a is a quadratic residue mod p.
// Uses Euler's criterion: a^((p-1)/2) == 1 mod p.
func blsFpIsSquare(a *big.Int) bool {
	if a.Sign() == 0 {
		return true
	}
	exp := new(big.Int).Sub(blsP, big.NewInt(1))
	exp.Rsh(exp, 1)
	r := blsFpExp(a, exp)
	return r.Cmp(big.NewInt(1)) == 0
}

// blsFpSgn0 returns the "sign" of a field element per the hash-to-curve spec.
// Returns 1 if a mod 2 == 1, 0 otherwise.
func blsFpSgn0(a *big.Int) int {
	t := new(big.Int).Mod(a, blsP)
	return int(t.Bit(0))
}

// blsFpCmov returns a if b==0, else c (constant-time selection for field elements).
func blsFpCmov(a, c *big.Int, b int) *big.Int {
	if b != 0 {
		return new(big.Int).Set(c)
	}
	return new(big.Int).Set(a)
}
