package crypto

// BN254 finite field arithmetic over F_p.
//
// The BN254 (alt_bn128) curve is defined over F_p where:
//   p = 21888242871839275222246405745257275088696311157297823662689037894645226208583
//
// This file provides modular arithmetic primitives for the base field.

import "math/big"

// BN254 curve parameters.
var (
	// p is the base field modulus.
	bn254P, _ = new(big.Int).SetString("21888242871839275222246405745257275088696311157297823662689037894645226208583", 10)
	// n is the curve order (number of points on E(F_p)).
	bn254N, _ = new(big.Int).SetString("21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)
	// b is the curve coefficient in y^2 = x^3 + b.
	bn254B = big.NewInt(3)
)

// fpAdd returns (a + b) mod p.
func fpAdd(a, b *big.Int) *big.Int {
	r := new(big.Int).Add(a, b)
	return r.Mod(r, bn254P)
}

// fpSub returns (a - b) mod p.
func fpSub(a, b *big.Int) *big.Int {
	r := new(big.Int).Sub(a, b)
	return r.Mod(r, bn254P)
}

// fpMul returns (a * b) mod p.
func fpMul(a, b *big.Int) *big.Int {
	r := new(big.Int).Mul(a, b)
	return r.Mod(r, bn254P)
}

// fpNeg returns (-a) mod p.
func fpNeg(a *big.Int) *big.Int {
	if a.Sign() == 0 {
		return new(big.Int)
	}
	return new(big.Int).Sub(bn254P, new(big.Int).Mod(a, bn254P))
}

// fpInv returns a^(-1) mod p using Fermat's little theorem.
func fpInv(a *big.Int) *big.Int {
	return new(big.Int).ModInverse(a, bn254P)
}

// fpSqr returns a^2 mod p.
func fpSqr(a *big.Int) *big.Int {
	r := new(big.Int).Mul(a, a)
	return r.Mod(r, bn254P)
}

// fpExp returns a^e mod p.
func fpExp(a, e *big.Int) *big.Int {
	return new(big.Int).Exp(a, e, bn254P)
}
