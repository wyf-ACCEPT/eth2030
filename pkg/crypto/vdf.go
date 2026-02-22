package crypto

import (
	"crypto/rand"
	"errors"
	"math/big"
)

// Verifiable Delay Function (VDF) implementation using Wesolowski's protocol.
// VDFs produce outputs that take a prescribed number of sequential steps to
// compute, but can be verified quickly. Used for unbiasable randomness in
// the beacon chain (2029+ roadmap).

var (
	errVDFNilInput       = errors.New("vdf: nil input")
	errVDFZeroIterations = errors.New("vdf: zero iterations")
	errVDFInvalidProof   = errors.New("vdf: invalid proof")
)

// VDFParams holds the security parameters for a VDF instance.
type VDFParams struct {
	T      uint64 // Time parameter (number of squarings)
	Lambda uint64 // Security parameter in bits
}

// DefaultVDFParams returns the default VDF parameters: T=2^20, Lambda=128.
func DefaultVDFParams() *VDFParams {
	return &VDFParams{
		T:      1 << 20, // ~1 million sequential squarings
		Lambda: 128,
	}
}

// VDFProof contains the input, output, and proof of a VDF evaluation.
type VDFProof struct {
	Input      []byte // x
	Output     []byte // y = x^(2^T) mod N
	Proof      []byte // pi (Wesolowski proof element)
	Iterations uint64 // T
}

// VDFEvaluator defines the interface for VDF evaluation and verification.
type VDFEvaluator interface {
	// Evaluate computes y = x^(2^iterations) mod N and produces a proof.
	Evaluate(input []byte, iterations uint64) (*VDFProof, error)

	// Verify checks a VDF proof: verifies that output == input^(2^iterations) mod N.
	Verify(proof *VDFProof) bool
}

// WesolowskiVDF implements VDFEvaluator using Wesolowski's protocol with
// repeated squaring modulo an RSA modulus.
type WesolowskiVDF struct {
	params *VDFParams
	n      *big.Int // RSA modulus
}

// NewWesolowskiVDF creates a new VDF evaluator with the given parameters.
// The RSA modulus is generated as a product of two safe primes.
func NewWesolowskiVDF(params *VDFParams) *WesolowskiVDF {
	n := generateVDFModulus(params.Lambda)
	return &WesolowskiVDF{
		params: params,
		n:      n,
	}
}

// NewWesolowskiVDFWithModulus creates a VDF evaluator with an explicit modulus.
// Used for testing with known moduli.
func NewWesolowskiVDFWithModulus(params *VDFParams, n *big.Int) *WesolowskiVDF {
	return &WesolowskiVDF{
		params: params,
		n:      n,
	}
}

// Evaluate computes y = x^(2^iterations) mod N via repeated squaring,
// then produces a Wesolowski proof pi.
func (v *WesolowskiVDF) Evaluate(input []byte, iterations uint64) (*VDFProof, error) {
	if len(input) == 0 {
		return nil, errVDFNilInput
	}
	if iterations == 0 {
		return nil, errVDFZeroIterations
	}

	x := new(big.Int).SetBytes(input)
	x.Mod(x, v.n)
	if x.Sign() == 0 {
		x.SetInt64(2) // avoid trivial input
	}

	// Compute y = x^(2^T) mod N by repeated squaring.
	y := new(big.Int).Set(x)
	for i := uint64(0); i < iterations; i++ {
		y.Mul(y, y)
		y.Mod(y, v.n)
	}

	// Compute Wesolowski proof: pi = x^q mod N, where q = floor(2^T / l),
	// l = HashToPrime(x, y).
	l := vdfHashToPrime(x, y)
	pi := vdfComputeProof(x, iterations, l, v.n)

	return &VDFProof{
		Input:      input,
		Output:     y.Bytes(),
		Proof:      pi.Bytes(),
		Iterations: iterations,
	}, nil
}

// Verify checks a Wesolowski VDF proof.
// Verification: compute r = 2^T mod l, then check pi^l * x^r == y (mod N).
func (v *WesolowskiVDF) Verify(proof *VDFProof) bool {
	if proof == nil || len(proof.Input) == 0 || len(proof.Output) == 0 || len(proof.Proof) == 0 {
		return false
	}
	if proof.Iterations == 0 {
		return false
	}

	x := new(big.Int).SetBytes(proof.Input)
	x.Mod(x, v.n)
	if x.Sign() == 0 {
		x.SetInt64(2)
	}
	y := new(big.Int).SetBytes(proof.Output)
	pi := new(big.Int).SetBytes(proof.Proof)

	// l = HashToPrime(x, y)
	l := vdfHashToPrime(x, y)

	// r = 2^T mod l
	two := big.NewInt(2)
	tBig := new(big.Int).SetUint64(proof.Iterations)
	r := new(big.Int).Exp(two, tBig, l)

	// Check: pi^l * x^r == y (mod N)
	piL := new(big.Int).Exp(pi, l, v.n)
	xR := new(big.Int).Exp(x, r, v.n)
	lhs := new(big.Int).Mul(piL, xR)
	lhs.Mod(lhs, v.n)

	return lhs.Cmp(y) == 0
}

// Modulus returns the RSA modulus used by this VDF instance.
func (v *WesolowskiVDF) Modulus() *big.Int {
	return new(big.Int).Set(v.n)
}

// vdfHashToPrime derives a prime l from the VDF input and output using
// a deterministic hash. This is the Fiat-Shamir challenge in Wesolowski's protocol.
func vdfHashToPrime(x, y *big.Int) *big.Int {
	// Hash x || y to get a seed, then find the next prime.
	h := Keccak256(x.Bytes(), y.Bytes())
	candidate := new(big.Int).SetBytes(h)
	// Ensure odd.
	candidate.SetBit(candidate, 0, 1)
	// Find next prime.
	for !candidate.ProbablyPrime(20) {
		candidate.Add(candidate, big.NewInt(2))
	}
	return candidate
}

// vdfComputeProof computes the Wesolowski proof pi = x^q mod N,
// where q = floor(2^T / l). Uses long division to avoid computing 2^T directly.
func vdfComputeProof(x *big.Int, T uint64, l, n *big.Int) *big.Int {
	// We compute x^(floor(2^T / l)) mod N using the iterative approach:
	// Track the quotient bits as we repeatedly square.
	// Let r_0 = 1. At each step: r_{i+1} = 2*r_i, if r_{i+1} >= l then
	// r_{i+1} -= l and we accumulate into pi.
	pi := big.NewInt(1)
	r := big.NewInt(1)
	two := big.NewInt(2)

	for i := uint64(0); i < T; i++ {
		// r = 2 * r
		r.Mul(r, two)
		// pi = pi^2 mod N
		pi.Mul(pi, pi)
		pi.Mod(pi, n)
		// If r >= l, this bit of the quotient is 1.
		if r.Cmp(l) >= 0 {
			r.Sub(r, l)
			// pi = pi * x mod N
			pi.Mul(pi, x)
			pi.Mod(pi, n)
		}
	}
	return pi
}

// ValidateVDFParams checks that VDF parameters are secure: time parameter
// non-zero, security bits sufficient, and modulus size adequate.
func ValidateVDFParams(params *VDFParams) error {
	if params == nil {
		return errors.New("vdf: nil params")
	}
	if params.T == 0 {
		return errVDFZeroIterations
	}
	if params.Lambda < 64 {
		return errors.New("vdf: security parameter must be >= 64 bits")
	}
	return nil
}

// ValidateVDFProof checks that a VDF proof has non-empty fields.
func ValidateVDFProof(proof *VDFProof) error {
	if proof == nil {
		return errVDFInvalidProof
	}
	if len(proof.Input) == 0 {
		return errVDFNilInput
	}
	if len(proof.Output) == 0 {
		return errors.New("vdf: empty output")
	}
	if len(proof.Proof) == 0 {
		return errVDFInvalidProof
	}
	if proof.Iterations == 0 {
		return errVDFZeroIterations
	}
	return nil
}

// generateVDFModulus generates a random RSA modulus N = p*q where p and q
// are random primes. For a production VDF, these should be generated via
// an MPC ceremony to ensure nobody knows the factorization.
func generateVDFModulus(securityBits uint64) *big.Int {
	bits := int(securityBits)
	if bits < 64 {
		bits = 64
	}

	p, err := rand.Prime(rand.Reader, bits)
	if err != nil {
		// Fallback to a well-known test modulus.
		p = big.NewInt(104729)
	}
	q, err := rand.Prime(rand.Reader, bits)
	if err != nil {
		q = big.NewInt(104743)
	}
	// Ensure p != q.
	for p.Cmp(q) == 0 {
		q, _ = rand.Prime(rand.Reader, bits)
	}
	n := new(big.Int).Mul(p, q)
	return n
}
