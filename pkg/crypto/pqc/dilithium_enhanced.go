// Enhanced Dilithium lattice-based signature scheme with modular arithmetic
// over polynomial rings. Uses real lattice operations (mod Q, rejection
// sampling, polynomial multiplication) with Keccak256 as XOF.
package pqc

import (
	"crypto/rand"
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/crypto"
)

// EnhancedDilithiumConfig holds lattice parameters for the enhanced scheme.
type EnhancedDilithiumConfig struct {
	SecurityLevel int   // NIST level: 2, 3, or 5
	N             int   // lattice dimension (polynomial degree)
	Q             int64 // prime modulus
	K             int   // rows in public matrix A
	L             int   // columns in public matrix A / secret vector width
	Eta           int   // secret coefficient bound
	Gamma1        int64 // masking range for signing
	Gamma2        int64 // low-order rounding range
	Beta          int64 // rejection bound: tau * eta
	Tau           int   // number of +/-1 in challenge polynomial
}

// DefaultEnhancedDilithiumConfig returns config for the given security level.
func DefaultEnhancedDilithiumConfig(level int) EnhancedDilithiumConfig {
	switch level {
	case 5:
		return EnhancedDilithiumConfig{
			SecurityLevel: 5, N: 64, Q: 8380417,
			K: 8, L: 7, Eta: 2, Gamma1: 524288, Gamma2: 261888,
			Beta: 120, Tau: 60,
		}
	case 3:
		return EnhancedDilithiumConfig{
			SecurityLevel: 3, N: 64, Q: 8380417,
			K: 6, L: 5, Eta: 4, Gamma1: 524288, Gamma2: 261888,
			Beta: 196, Tau: 49,
		}
	default: // level 2
		return EnhancedDilithiumConfig{
			SecurityLevel: 2, N: 64, Q: 8380417,
			K: 4, L: 4, Eta: 2, Gamma1: 131072, Gamma2: 95232,
			Beta: 78, Tau: 39,
		}
	}
}

// EnhancedDilithiumKeypair holds a lattice-based key pair.
type EnhancedDilithiumKeypair struct {
	PublicMatrix [][]int64 // A: k*l*N coefficients flattened to k*l polynomials
	PublicT      []int64   // t = A*s1 + s2, k*N coefficients
	SecretS1     []int64   // s1: l*N coefficients (small)
	SecretS2     []int64   // s2: k*N coefficients (small)
	PublicKey    []byte    // serialised public key
	SecretKey    []byte    // serialised secret key
	Config       EnhancedDilithiumConfig
}

// EnhancedDilithiumSig holds a lattice signature.
type EnhancedDilithiumSig struct {
	Z             []int64 // response vector: l*N coefficients
	Hint          []byte  // hint bits for high-order reconstruction
	ChallengeHash []byte  // 32-byte challenge hash
}

// EnhancedDilithiumSigner performs lattice-based signing and verification.
type EnhancedDilithiumSigner struct {
	config EnhancedDilithiumConfig
}

// NewEnhancedDilithiumSigner creates a signer with the given config.
func NewEnhancedDilithiumSigner(config EnhancedDilithiumConfig) *EnhancedDilithiumSigner {
	return &EnhancedDilithiumSigner{config: config}
}

// Errors specific to the enhanced Dilithium scheme.
var (
	ErrEnhancedDilithiumRejection = errors.New("enhanced-dilithium: rejection sampling exceeded max iterations")
	ErrEnhancedDilithiumNilKey    = errors.New("enhanced-dilithium: nil key pair")
	ErrEnhancedDilithiumEmptyMsg  = errors.New("enhanced-dilithium: empty message")
	ErrEnhancedDilithiumBadSig    = errors.New("enhanced-dilithium: malformed signature")
)

// serializeCoeffs concatenates seed with int64 coefficients as little-endian uint32.
func serializeCoeffs(seed []byte, coeffs []int64) []byte {
	out := make([]byte, 0, len(seed)+len(coeffs)*4)
	out = append(out, seed...)
	for _, c := range coeffs {
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], uint32(c))
		out = append(out, buf[:]...)
	}
	return out
}

// computeAzMinusCt computes w' = A*z - c*t, the verifier's reconstruction.
func computeAzMinusCt(aMatrix [][]int64, z, cPoly, t []int64, cfg EnhancedDilithiumConfig, n int, q int64) []int64 {
	wPrime := make([]int64, cfg.K*n)
	for i := 0; i < cfg.K; i++ {
		wi := make([]int64, n)
		for j := 0; j < cfg.L; j++ {
			aPoly := aMatrix[i*cfg.L+j]
			zPoly := z[j*n : (j+1)*n]
			product := polyMulModQ(aPoly, zPoly, n, q)
			wi = polyAddModQ(wi, product, q)
		}
		tPoly := t[i*n : (i+1)*n]
		ct := polyMulModQ(cPoly, tPoly, n, q)
		wi = polySubModQ(wi, ct, q)
		copy(wPrime[i*n:(i+1)*n], wi)
	}
	return wPrime
}

// highBitsVec extracts high-order bits from a coefficient vector.
func highBitsVec(coeffs []int64, length int, q, gamma2 int64) []byte {
	out := make([]byte, length)
	for i := 0; i < length; i++ {
		r := modQ(coeffs[i], q)
		if gamma2 > 0 {
			out[i] = byte(r / gamma2 % 256)
		}
	}
	return out
}

// expandChallenge expands a 32-byte hash into a sparse challenge polynomial.
func expandChallenge(cHash []byte, n, tau int, q int64) []int64 {
	cPoly := make([]int64, n)
	for i := 0; i < tau && i < n; i++ {
		pos := int(cHash[i%32]) % n
		if cHash[(i+16)%32]&1 == 0 {
			cPoly[pos] = modQ(cPoly[pos]+1, q)
		} else {
			cPoly[pos] = modQ(cPoly[pos]+q-1, q)
		}
	}
	return cPoly
}

// modQ reduces x into [0, Q).
func modQ(x int64, q int64) int64 {
	r := x % q
	if r < 0 {
		r += q
	}
	return r
}

// centeredMod reduces x into (-Q/2, Q/2].
func centeredMod(x int64, q int64) int64 {
	r := modQ(x, q)
	if r > q/2 {
		r -= q
	}
	return r
}

// polyMulModQ multiplies two polynomials mod (X^N + 1) mod Q (schoolbook).
func polyMulModQ(a, b []int64, n int, q int64) []int64 {
	c := make([]int64, n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			idx := i + j
			prod := modQ(a[i]*b[j], q)
			if idx < n {
				c[idx] = modQ(c[idx]+prod, q)
			} else {
				// Reduction by X^N + 1: coefficient wraps with negation.
				c[idx-n] = modQ(c[idx-n]-prod, q)
			}
		}
	}
	return c
}

// polyAddModQ adds two polynomials mod Q.
func polyAddModQ(a, b []int64, q int64) []int64 {
	n := len(a)
	c := make([]int64, n)
	for i := 0; i < n; i++ {
		c[i] = modQ(a[i]+b[i], q)
	}
	return c
}

// polySubModQ subtracts b from a mod Q.
func polySubModQ(a, b []int64, q int64) []int64 {
	n := len(a)
	c := make([]int64, n)
	for i := 0; i < n; i++ {
		c[i] = modQ(a[i]-b[i], q)
	}
	return c
}

// expandSeed derives a pseudorandom polynomial from seed using Keccak256.
func expandSeed(seed []byte, index int, n int, q int64) []int64 {
	poly := make([]int64, n)
	// Copy seed to avoid aliasing issues with append.
	tag := make([]byte, len(seed)+2)
	copy(tag, seed)
	tag[len(seed)] = byte(index >> 8)
	tag[len(seed)+1] = byte(index)
	for i := 0; i < n; i += 4 {
		h := crypto.Keccak256(tag, []byte{byte(i >> 8), byte(i)})
		for j := 0; j < 4 && i+j < n; j++ {
			val := int64(binary.LittleEndian.Uint32(h[j*4:j*4+4])) & 0x7FFFFF
			poly[i+j] = modQ(val, q)
		}
	}
	return poly
}

// sampleSmall generates a polynomial with coefficients in [-eta, eta].
func sampleSmall(seed []byte, index int, n int, eta int) []int64 {
	poly := make([]int64, n)
	tag := make([]byte, len(seed)+3)
	copy(tag, seed)
	tag[len(seed)] = byte(index >> 8)
	tag[len(seed)+1] = byte(index)
	tag[len(seed)+2] = 0xFF
	for i := 0; i < n; i += 8 {
		h := crypto.Keccak256(tag, []byte{byte(i >> 8), byte(i)})
		for j := 0; j < 8 && i+j < n; j++ {
			// Map byte to range [-eta, eta].
			b := int64(h[j]) % int64(2*eta+1)
			poly[i+j] = b - int64(eta)
		}
	}
	return poly
}

// GenerateKey generates a lattice-based key pair with rejection sampling.
func (s *EnhancedDilithiumSigner) GenerateKey() (*EnhancedDilithiumKeypair, error) {
	cfg := s.config
	n := cfg.N
	q := cfg.Q

	// Random seed for matrix and secret generation.
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}

	// Expand public matrix A (k x l polynomials).
	aMatrix := make([][]int64, cfg.K*cfg.L)
	for i := 0; i < cfg.K; i++ {
		for j := 0; j < cfg.L; j++ {
			aMatrix[i*cfg.L+j] = expandSeed(seed, i*cfg.L+j, n, q)
		}
	}

	// Generate small secret vectors s1 (l polynomials) and s2 (k polynomials).
	skSeed := crypto.Keccak256(seed, []byte("secret"))
	s1 := make([]int64, cfg.L*n)
	for j := 0; j < cfg.L; j++ {
		poly := sampleSmall(skSeed, j, n, cfg.Eta)
		copy(s1[j*n:(j+1)*n], poly)
	}
	s2 := make([]int64, cfg.K*n)
	for i := 0; i < cfg.K; i++ {
		poly := sampleSmall(skSeed, cfg.L+i, n, cfg.Eta)
		copy(s2[i*n:(i+1)*n], poly)
	}

	// Compute t = A*s1 + s2.
	t := make([]int64, cfg.K*n)
	for i := 0; i < cfg.K; i++ {
		ti := make([]int64, n)
		for j := 0; j < cfg.L; j++ {
			aPoly := aMatrix[i*cfg.L+j]
			s1Poly := s1[j*n : (j+1)*n]
			product := polyMulModQ(aPoly, s1Poly, n, q)
			ti = polyAddModQ(ti, product, q)
		}
		s2Poly := s2[i*n : (i+1)*n]
		ti = polyAddModQ(ti, s2Poly, q)
		copy(t[i*n:(i+1)*n], ti)
	}

	// Serialize keys: seed || coefficients.
	pkBytes := serializeCoeffs(seed, t)
	skCoeffs := make([]int64, len(s1)+len(s2))
	copy(skCoeffs, s1)
	copy(skCoeffs[len(s1):], s2)
	skBytes := serializeCoeffs(seed, skCoeffs)

	return &EnhancedDilithiumKeypair{
		PublicMatrix: aMatrix,
		PublicT:      t,
		SecretS1:     s1,
		SecretS2:     s2,
		PublicKey:    pkBytes,
		SecretKey:    skBytes,
		Config:       cfg,
	}, nil
}

// Sign produces a lattice signature with rejection sampling.
func (s *EnhancedDilithiumSigner) Sign(key *EnhancedDilithiumKeypair, message []byte) (*EnhancedDilithiumSig, error) {
	if key == nil {
		return nil, ErrEnhancedDilithiumNilKey
	}
	if len(message) == 0 {
		return nil, ErrEnhancedDilithiumEmptyMsg
	}

	cfg := key.Config
	n := cfg.N
	q := cfg.Q
	maxIter := 256

	// Message hash mu = H(pubkey || message).
	mu := crypto.Keccak256(key.PublicKey, message)

	for attempt := 0; attempt < maxIter; attempt++ {
		// Generate masking vector y (l polynomials, coefficients in [-gamma1, gamma1)).
		ySeed := make([]byte, 32)
		if _, err := rand.Read(ySeed); err != nil {
			return nil, err
		}

		y := make([]int64, cfg.L*n)
		for j := 0; j < cfg.L; j++ {
			poly := expandSeed(ySeed, j+attempt*cfg.L, n, q)
			for k := 0; k < n; k++ {
				y[j*n+k] = centeredMod(poly[k], cfg.Gamma1*2)
			}
		}

		// Compute w = A*y.
		w := make([]int64, cfg.K*n)
		for i := 0; i < cfg.K; i++ {
			wi := make([]int64, n)
			for j := 0; j < cfg.L; j++ {
				aPoly := key.PublicMatrix[i*cfg.L+j]
				yPoly := y[j*n : (j+1)*n]
				product := polyMulModQ(aPoly, yPoly, n, q)
				wi = polyAddModQ(wi, product, q)
			}
			copy(w[i*n:(i+1)*n], wi)
		}

		// High-order bits of w for Fiat-Shamir challenge.
		wHigh := highBitsVec(w, cfg.K*n, q, cfg.Gamma2)

		// Challenge hash c = H(mu || wHigh).
		cHash := crypto.Keccak256(mu, wHigh)

		// Expand challenge to a sparse polynomial.
		cPoly := expandChallenge(cHash, n, cfg.Tau, q)

		// Compute z = y + c*s1.
		z := make([]int64, cfg.L*n)
		reject := false
		for j := 0; j < cfg.L; j++ {
			s1Poly := key.SecretS1[j*n : (j+1)*n]
			cs1 := polyMulModQ(cPoly, s1Poly, n, q)
			yPoly := y[j*n : (j+1)*n]
			zPoly := polyAddModQ(yPoly, cs1, q)

			// Rejection sampling: check ||z||_inf < gamma1 - beta.
			bound := cfg.Gamma1 - cfg.Beta
			for k := 0; k < n; k++ {
				zc := centeredMod(zPoly[k], q)
				if zc > bound || zc < -bound {
					reject = true
					break
				}
				zPoly[k] = modQ(zc, q)
			}
			if reject {
				break
			}
			copy(z[j*n:(j+1)*n], zPoly)
		}
		if reject {
			continue
		}

		// Verify that HighBits(A*z - c*t) == HighBits(w).
		// This is needed so the verifier can reconstruct the same challenge.
		// w' = A*z - c*t = w - c*s2; the high bits must agree.
		wPrime := computeAzMinusCt(key.PublicMatrix, z, cPoly, key.PublicT, cfg, n, q)
		wPrimeHigh := highBitsVec(wPrime, cfg.K*n, q, cfg.Gamma2)

		highMatch := true
		for i := range wHigh {
			if wHigh[i] != wPrimeHigh[i] {
				highMatch = false
				break
			}
		}
		if !highMatch {
			continue // retry with fresh y
		}

		return &EnhancedDilithiumSig{
			Z:             z,
			Hint:          wHigh,
			ChallengeHash: cHash,
		}, nil
	}

	return nil, ErrEnhancedDilithiumRejection
}

// Verify checks a lattice signature against a serialised public key.
func (s *EnhancedDilithiumSigner) Verify(pubKey []byte, message []byte, sig *EnhancedDilithiumSig) (bool, error) {
	if sig == nil {
		return false, ErrEnhancedDilithiumBadSig
	}
	if len(message) == 0 {
		return false, ErrEnhancedDilithiumEmptyMsg
	}

	cfg := s.config
	n := cfg.N
	q := cfg.Q

	// Deserialise public key: seed (32 bytes) + t coefficients.
	expectedPKLen := 32 + cfg.K*n*4
	if len(pubKey) < expectedPKLen {
		return false, ErrEnhancedDilithiumBadSig
	}

	seed := pubKey[:32]
	t := make([]int64, cfg.K*n)
	for i := range t {
		t[i] = int64(binary.LittleEndian.Uint32(pubKey[32+i*4 : 32+i*4+4]))
	}

	// Reconstruct matrix A from seed.
	aMatrix := make([][]int64, cfg.K*cfg.L)
	for i := 0; i < cfg.K; i++ {
		for j := 0; j < cfg.L; j++ {
			aMatrix[i*cfg.L+j] = expandSeed(seed, i*cfg.L+j, n, q)
		}
	}

	// Check z coefficients are within bounds.
	if len(sig.Z) != cfg.L*n {
		return false, ErrEnhancedDilithiumBadSig
	}
	bound := cfg.Gamma1 - cfg.Beta
	for _, zc := range sig.Z {
		zCentered := centeredMod(zc, q)
		if zCentered > bound || zCentered < -bound {
			return false, nil
		}
	}

	// Expand challenge polynomial from challenge hash.
	cHash := sig.ChallengeHash
	if len(cHash) != 32 {
		return false, ErrEnhancedDilithiumBadSig
	}
	cPoly := expandChallenge(cHash, n, cfg.Tau, q)

	// Compute w' = A*z - c*t.
	wPrime := make([]int64, cfg.K*n)
	for i := 0; i < cfg.K; i++ {
		wi := make([]int64, n)
		for j := 0; j < cfg.L; j++ {
			aPoly := aMatrix[i*cfg.L+j]
			zPoly := sig.Z[j*n : (j+1)*n]
			product := polyMulModQ(aPoly, zPoly, n, q)
			wi = polyAddModQ(wi, product, q)
		}
		tPoly := t[i*n : (i+1)*n]
		ct := polyMulModQ(cPoly, tPoly, n, q)
		wi = polySubModQ(wi, ct, q)
		copy(wPrime[i*n:(i+1)*n], wi)
	}

	// Recompute high-order bits and challenge hash.
	mu := crypto.Keccak256(pubKey, message)
	wHigh := highBitsVec(wPrime, cfg.K*n, q, cfg.Gamma2)
	expectedHash := crypto.Keccak256(mu, wHigh)
	for i := range expectedHash {
		if expectedHash[i] != cHash[i] {
			return false, nil
		}
	}
	return true, nil
}

// EnhancedKeySize returns the public key size in bytes for a given security level.
func EnhancedKeySize(level int) int {
	cfg := DefaultEnhancedDilithiumConfig(level)
	return 32 + cfg.K*cfg.N*4
}

// SigSize returns the signature size in bytes for a given security level.
func EnhancedSigSize(level int) int {
	cfg := DefaultEnhancedDilithiumConfig(level)
	// z vector + hint + challenge hash.
	return cfg.L*cfg.N*4 + cfg.K*cfg.N + 32
}
