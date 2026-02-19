// Package crypto implements cryptographic primitives for the Ethereum client.
// This file implements threshold cryptography for encrypted mempool support,
// using Shamir's Secret Sharing with Feldman Verifiable Secret Sharing (VSS).
// The scheme allows t-of-n threshold encryption/decryption where t parties
// must cooperate to decrypt a message, preventing any single party from
// observing transaction contents before ordering.
//
// We use a safe-prime group: p = 2q+1 where both p and q are prime.
// The generator g has order q in Z_p^*, ensuring all Lagrange interpolation
// and Feldman VSS computations work correctly over a prime-order group.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"math/big"
)

var (
	ErrInvalidThreshold    = errors.New("threshold: t must be >= 1 and <= n")
	ErrInsufficientShares  = errors.New("threshold: insufficient shares for reconstruction")
	ErrDuplicateShareIndex = errors.New("threshold: duplicate share index")
	ErrInvalidShare        = errors.New("threshold: share verification failed")
	ErrDecryptionFailed    = errors.New("threshold: decryption failed")
	ErrInvalidCiphertext   = errors.New("threshold: invalid ciphertext")
)

// thresholdParams holds the group parameters for the threshold scheme.
// We use a safe prime: p = 2q+1, generator g of order q in Z_p^*.
// Polynomial arithmetic and Lagrange interpolation use modulus q.
// Group element exponentiation uses modulus p.
var thresholdParams struct {
	p *big.Int // safe prime, modulus for group element operations
	q *big.Int // prime subgroup order, modulus for polynomial/exponent arithmetic
	g *big.Int // generator of order q in Z_p^*
}

func init() {
	// Safe prime: q = 2^255 - 18057 (prime), p = 2q+1 (also prime).
	// Both verified via probabilistic primality testing.
	thresholdParams.q = new(big.Int).Sub(
		new(big.Int).Exp(big.NewInt(2), big.NewInt(255), nil),
		big.NewInt(18057),
	)
	thresholdParams.p = new(big.Int).Mul(thresholdParams.q, big.NewInt(2))
	thresholdParams.p.Add(thresholdParams.p, big.NewInt(1))

	// For a safe prime p = 2q+1, the quadratic residues form a subgroup of
	// order q. g = 4 = 2^2 is a generator of this order-q subgroup:
	// g^q mod p = 1 (verified).
	thresholdParams.g = big.NewInt(4)
}

// ThresholdScheme holds the configuration for a t-of-n threshold scheme.
type ThresholdScheme struct {
	T int // threshold: minimum number of shares needed
	N int // total number of parties
}

// NewThresholdScheme creates a new threshold scheme with the given parameters.
// Requires 1 <= t <= n.
func NewThresholdScheme(t, n int) (*ThresholdScheme, error) {
	if t < 1 || t > n {
		return nil, ErrInvalidThreshold
	}
	return &ThresholdScheme{T: t, N: n}, nil
}

// Share represents a single party's share of the secret.
type Share struct {
	Index int      // party index (1-based)
	Value *big.Int // share value f(index) mod q
}

// VerifiableShare is a share with a proof of correctness using Feldman VSS.
// The commitments are g^{a_i} mod p for each polynomial coefficient a_i.
type VerifiableShare struct {
	Share       Share
	Commitments []*big.Int // g^{a_0}, g^{a_1}, ..., g^{a_{t-1}} mod p
}

// KeyGenResult holds the output of threshold key generation.
type KeyGenResult struct {
	Shares      []Share    // one share per party
	PublicKey   *big.Int   // g^{secret} mod p (for encryption)
	Commitments []*big.Int // Feldman VSS commitments
}

// KeyGeneration generates a random secret and splits it into n shares
// using Shamir's Secret Sharing over a degree-(t-1) polynomial mod q.
// Returns the shares and Feldman VSS commitments for verification.
func (ts *ThresholdScheme) KeyGeneration() (*KeyGenResult, error) {
	q := thresholdParams.q
	p := thresholdParams.p
	g := thresholdParams.g

	// Generate random polynomial coefficients: a_0, a_1, ..., a_{t-1} mod q.
	// a_0 is the secret.
	coeffs := make([]*big.Int, ts.T)
	for i := 0; i < ts.T; i++ {
		c, err := rand.Int(rand.Reader, q)
		if err != nil {
			return nil, err
		}
		coeffs[i] = c
	}

	// Compute Feldman VSS commitments: C_i = g^{a_i} mod p.
	commitments := make([]*big.Int, ts.T)
	for i, c := range coeffs {
		commitments[i] = new(big.Int).Exp(g, c, p)
	}

	// Evaluate polynomial at each party's index (1-based) mod q.
	shares := make([]Share, ts.N)
	for i := 0; i < ts.N; i++ {
		x := big.NewInt(int64(i + 1))
		y := evaluatePolynomial(coeffs, x, q)
		shares[i] = Share{Index: i + 1, Value: y}
	}

	// Public key is g^{secret} mod p = g^{a_0} mod p = commitments[0].
	publicKey := new(big.Int).Set(commitments[0])

	return &KeyGenResult{
		Shares:      shares,
		PublicKey:   publicKey,
		Commitments: commitments,
	}, nil
}

// VerifyShare checks that a share is consistent with the Feldman VSS commitments.
// Verifies: g^{share.Value} mod p == product( C_j^{index^j} ) mod p for j=0..t-1.
// Exponent powers index^j are computed mod q (the subgroup order).
func VerifyShare(share Share, commitments []*big.Int) bool {
	if len(commitments) == 0 || share.Value == nil {
		return false
	}

	p := thresholdParams.p
	q := thresholdParams.q
	g := thresholdParams.g

	// Left side: g^{f(i)} mod p.
	lhs := new(big.Int).Exp(g, share.Value, p)

	// Right side: product of C_j^{i^j mod q} mod p, for j = 0..t-1.
	rhs := big.NewInt(1)
	x := big.NewInt(int64(share.Index))
	xPow := big.NewInt(1) // x^j mod q

	for _, cj := range commitments {
		term := new(big.Int).Exp(cj, xPow, p)
		rhs.Mul(rhs, term)
		rhs.Mod(rhs, p)

		xPow = new(big.Int).Mul(xPow, x)
		xPow.Mod(xPow, q)
	}

	return lhs.Cmp(rhs) == 0
}

// MakeVerifiableShare wraps a share with Feldman VSS commitments.
func MakeVerifiableShare(share Share, commitments []*big.Int) VerifiableShare {
	return VerifiableShare{
		Share:       share,
		Commitments: commitments,
	}
}

// EncryptedMessage holds a threshold-encrypted message.
// The symmetric key is derived from an ElGamal-style shared secret so that
// t parties must cooperate to recover it.
type EncryptedMessage struct {
	// Ephemeral is g^r mod p where r is the random ephemeral secret.
	Ephemeral *big.Int
	// Ciphertext is the AES-GCM encrypted payload.
	Ciphertext []byte
	// Nonce for AES-GCM.
	Nonce []byte
}

// DecryptionShare is a party's contribution to threshold decryption.
type DecryptionShare struct {
	Index int      // party index (1-based)
	Value *big.Int // ephemeral^{share_value} mod p
}

// ShareEncrypt encrypts a message to the threshold group using the public key.
// Uses ElGamal-style key encapsulation with AES-GCM for the payload.
func ShareEncrypt(publicKey *big.Int, message []byte) (*EncryptedMessage, error) {
	if publicKey == nil || publicKey.Sign() == 0 {
		return nil, errors.New("threshold: nil or zero public key")
	}

	q := thresholdParams.q
	p := thresholdParams.p
	g := thresholdParams.g

	// Generate ephemeral secret r in [0, q).
	r, err := rand.Int(rand.Reader, q)
	if err != nil {
		return nil, err
	}

	// Ephemeral = g^r mod p.
	ephemeral := new(big.Int).Exp(g, r, p)

	// Shared secret = publicKey^r mod p = g^{secret*r} mod p.
	sharedSecret := new(big.Int).Exp(publicKey, r, p)

	// Derive symmetric key from shared secret via Keccak256.
	symKey := Keccak256(sharedSecret.Bytes())[:32]

	// Encrypt message with AES-GCM.
	block, err := aes.NewCipher(symKey)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := aesGCM.Seal(nil, nonce, message, nil)

	return &EncryptedMessage{
		Ephemeral:  ephemeral,
		Ciphertext: ciphertext,
		Nonce:      nonce,
	}, nil
}

// ShareDecrypt computes a party's decryption share.
// Each party computes ephemeral^{share_value} mod p.
func ShareDecrypt(share Share, ephemeral *big.Int) DecryptionShare {
	p := thresholdParams.p
	value := new(big.Int).Exp(ephemeral, share.Value, p)
	return DecryptionShare{
		Index: share.Index,
		Value: value,
	}
}

// CombineShares combines t decryption shares to recover the original message.
// Uses Lagrange interpolation in the exponent to reconstruct the shared secret,
// then decrypts the AES-GCM ciphertext.
//
// The reconstruction works because:
//
//	D_i = ephemeral^{f(i)} = g^{r * f(i)}
//	product(D_i^{lambda_i}) = g^{r * sum(f(i)*lambda_i)} = g^{r*secret}
//
// where lambda_i are Lagrange coefficients computed mod q.
func CombineShares(shares []DecryptionShare, encrypted *EncryptedMessage) ([]byte, error) {
	if encrypted == nil {
		return nil, ErrInvalidCiphertext
	}
	if len(shares) == 0 {
		return nil, ErrInsufficientShares
	}

	// Check for duplicate indices.
	seen := make(map[int]bool)
	for _, s := range shares {
		if seen[s.Index] {
			return nil, ErrDuplicateShareIndex
		}
		seen[s.Index] = true
	}

	p := thresholdParams.p
	q := thresholdParams.q

	// Reconstruct shared secret using Lagrange interpolation in the exponent.
	// Lagrange coefficients are computed mod q (the prime subgroup order).
	sharedSecret := big.NewInt(1)
	for i, si := range shares {
		lambda := lagrangeCoefficientModQ(shares, i, q)
		// D_i^{lambda_i} mod p
		term := new(big.Int).Exp(si.Value, lambda, p)
		sharedSecret.Mul(sharedSecret, term)
		sharedSecret.Mod(sharedSecret, p)
	}

	// Derive symmetric key from shared secret.
	symKey := Keccak256(sharedSecret.Bytes())[:32]

	// Decrypt with AES-GCM.
	block, err := aes.NewCipher(symKey)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	plaintext, err := aesGCM.Open(nil, encrypted.Nonce, encrypted.Ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// evaluatePolynomial evaluates f(x) = sum(coeffs[i] * x^i) mod modulus.
func evaluatePolynomial(coeffs []*big.Int, x, modulus *big.Int) *big.Int {
	result := new(big.Int)
	xPow := big.NewInt(1) // x^0 = 1

	for _, c := range coeffs {
		term := new(big.Int).Mul(c, xPow)
		term.Mod(term, modulus)
		result.Add(result, term)
		result.Mod(result, modulus)

		xPow = new(big.Int).Mul(xPow, x)
		xPow.Mod(xPow, modulus)
	}

	return result
}

// LagrangeInterpolate reconstructs the secret (f(0)) from t shares
// using Lagrange interpolation over GF(q).
func LagrangeInterpolate(shares []Share) (*big.Int, error) {
	if len(shares) == 0 {
		return nil, ErrInsufficientShares
	}

	q := thresholdParams.q

	// Check for duplicate indices.
	seen := make(map[int]bool)
	for _, s := range shares {
		if seen[s.Index] {
			return nil, ErrDuplicateShareIndex
		}
		seen[s.Index] = true
	}

	result := new(big.Int)
	for i, si := range shares {
		num := big.NewInt(1)
		den := big.NewInt(1)
		xi := big.NewInt(int64(si.Index))

		for j, sj := range shares {
			if i == j {
				continue
			}
			xj := big.NewInt(int64(sj.Index))

			// num *= (0 - xj) mod q = num * (q - xj)
			negXj := new(big.Int).Sub(q, xj)
			num.Mul(num, negXj)
			num.Mod(num, q)

			// den *= (xi - xj) mod q
			diff := new(big.Int).Sub(xi, xj)
			diff.Mod(diff, q)
			den.Mul(den, diff)
			den.Mod(den, q)
		}

		denInv := new(big.Int).ModInverse(den, q)
		if denInv == nil {
			return nil, errors.New("threshold: modular inverse failed")
		}
		lambda := new(big.Int).Mul(num, denInv)
		lambda.Mod(lambda, q)

		// result += share_i * lambda_i mod q
		term := new(big.Int).Mul(si.Value, lambda)
		term.Mod(term, q)
		result.Add(result, term)
		result.Mod(result, q)
	}

	return result, nil
}

// lagrangeCoefficientModQ computes the Lagrange coefficient for the share at
// position idx among the given decryption shares, evaluated at x=0, mod q.
// Since q is prime, all nonzero elements have modular inverses.
func lagrangeCoefficientModQ(shares []DecryptionShare, idx int, q *big.Int) *big.Int {
	num := big.NewInt(1)
	den := big.NewInt(1)
	xi := big.NewInt(int64(shares[idx].Index))

	for j, sj := range shares {
		if j == idx {
			continue
		}
		xj := big.NewInt(int64(sj.Index))

		// num *= (0 - xj) mod q
		negXj := new(big.Int).Sub(q, xj)
		num.Mul(num, negXj)
		num.Mod(num, q)

		// den *= (xi - xj) mod q
		diff := new(big.Int).Sub(xi, xj)
		diff.Mod(diff, q)
		den.Mul(den, diff)
		den.Mod(den, q)
	}

	denInv := new(big.Int).ModInverse(den, q)
	if denInv == nil {
		return big.NewInt(0)
	}

	result := new(big.Int).Mul(num, denInv)
	result.Mod(result, q)
	return result
}
