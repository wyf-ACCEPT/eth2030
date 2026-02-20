// Post-Quantum Key Exchange (I+ roadmap: post quantum custody)
//
// Implements a simplified Kyber-768 (ML-KEM) key encapsulation mechanism.
// Kyber is a lattice-based KEM selected by NIST for post-quantum key
// establishment (FIPS 203). This module provides the core algebraic
// building blocks (NTT-based polynomial arithmetic over Z_q[X]/(X^n+1))
// and the encapsulation/decapsulation workflow.
//
// Security relies on the Module Learning With Errors (MLWE) problem.
// The parameter set Kyber-768 targets NIST security level 3 (~AES-192).
//
// NTT arithmetic and polynomial helpers are in kyber_ntt.go.
package pqc

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

// Kyber parameter constants for Kyber-768 (NIST level 3).
const (
	KyberN    = 256  // Polynomial degree.
	KyberQ    = 3329 // Modulus q.
	KyberK    = 3    // Module rank k (Kyber-768).
	KyberEta1 = 2    // CBD noise parameter for keygen and encryption.
	KyberEta2 = 2    // CBD noise parameter for encryption noise.

	// Derived sizes.
	kyberPolyBytes        = 384  // 12 bits per coefficient, 256 coeffs.
	kyberPolyCompBytes    = 320  // Compressed polynomial (du=10 bits): 256*10/8.
	kyberPolyCompBytesV   = 128  // Compressed polynomial v (dv=4 bits): 256*4/8.
	kyberPublicKeySize    = KyberK*kyberPolyBytes + 32
	kyberSecretKeySize    = KyberK * kyberPolyBytes
	kyberCiphertextSize   = KyberK*kyberPolyCompBytes + kyberPolyCompBytesV
	kyberSharedSecretSize = 32
)

// KyberParams holds the parameter set for a Kyber KEM instance.
type KyberParams struct {
	N    int   // Polynomial degree (256).
	Q    int16 // Modulus (3329).
	K    int   // Module rank.
	Eta1 int   // Noise parameter for key generation.
	Eta2 int   // Noise parameter for encryption.
}

// DefaultKyberParams returns the Kyber-768 parameter set.
func DefaultKyberParams() KyberParams {
	return KyberParams{
		N:    KyberN,
		Q:    KyberQ,
		K:    KyberK,
		Eta1: KyberEta1,
		Eta2: KyberEta2,
	}
}

// KyberKeyPair holds a Kyber key pair for key encapsulation.
type KyberKeyPair struct {
	PublicKey []byte // Encoded public key (pk = A*s + e || seed).
	SecretKey []byte // Encoded secret key (s polynomial vector).
	params    KyberParams
}

// Kyber key exchange errors.
var (
	ErrKyberInvalidPubKeySize = errors.New("pqc: invalid Kyber public key size")
	ErrKyberInvalidSecKeySize = errors.New("pqc: invalid Kyber secret key size")
	ErrKyberInvalidCiphertext = errors.New("pqc: invalid Kyber ciphertext size")
	ErrKyberDecapsFailure     = errors.New("pqc: Kyber decapsulation failure")
	ErrKyberRandFailure       = errors.New("pqc: random number generation failed")
)

// GenerateKyberKeyPair generates a Kyber-768 key pair using the standard
// KEM key generation procedure.
func GenerateKyberKeyPair() (*KyberKeyPair, error) {
	return GenerateKyberKeyPairWithReader(rand.Reader)
}

// GenerateKyberKeyPairWithReader generates a Kyber-768 key pair using
// the provided randomness source.
func GenerateKyberKeyPairWithReader(rng io.Reader) (*KyberKeyPair, error) {
	params := DefaultKyberParams()

	// Generate random seed for matrix A.
	seed := make([]byte, 32)
	if _, err := io.ReadFull(rng, seed); err != nil {
		return nil, ErrKyberRandFailure
	}

	// Generate secret vector s with small coefficients.
	s := make([][]int16, params.K)
	for i := 0; i < params.K; i++ {
		s[i] = sampleCBD(rng, params.Eta1, params.N)
	}

	// Generate error vector e with small coefficients.
	e := make([][]int16, params.K)
	for i := 0; i < params.K; i++ {
		e[i] = sampleCBD(rng, params.Eta2, params.N)
	}

	// Generate matrix A from seed (deterministic expansion).
	matA := expandMatrix(seed, params.K, params.N, params.Q)

	// Compute t = A*s + e in NTT domain.
	sNTT := make([][]int16, params.K)
	for i := 0; i < params.K; i++ {
		sNTT[i] = NTT(s[i], params.Q)
	}

	t := make([][]int16, params.K)
	for i := 0; i < params.K; i++ {
		t[i] = make([]int16, params.N)
		for j := 0; j < params.K; j++ {
			prod := PolyMul(matA[i][j], sNTT[j], params.Q)
			for c := 0; c < params.N; c++ {
				t[i][c] = modQ16(t[i][c]+prod[c], params.Q)
			}
		}
		eNTT := NTT(e[i], params.Q)
		for c := 0; c < params.N; c++ {
			t[i][c] = modQ16(t[i][c]+eNTT[c], params.Q)
		}
	}

	// Encode public key: t || seed.
	pk := make([]byte, 0, kyberPublicKeySize)
	for i := 0; i < params.K; i++ {
		pk = append(pk, encodePolynomial(t[i], 12)...)
	}
	pk = append(pk, seed...)

	// Encode secret key: s in NTT domain.
	sk := make([]byte, 0, kyberSecretKeySize)
	for i := 0; i < params.K; i++ {
		sk = append(sk, encodePolynomial(sNTT[i], 12)...)
	}

	return &KyberKeyPair{
		PublicKey: pk,
		SecretKey: sk,
		params:    params,
	}, nil
}

// KyberEncapsulate performs KEM encapsulation: given a public key, produces
// a ciphertext and shared secret. The shared secret is derived via SHA-256.
func KyberEncapsulate(pk []byte) (ciphertext, sharedSecret []byte, err error) {
	return KyberEncapsulateWithReader(pk, rand.Reader)
}

// KyberEncapsulateWithReader performs encapsulation with a given RNG.
func KyberEncapsulateWithReader(pk []byte, rng io.Reader) ([]byte, []byte, error) {
	if len(pk) != kyberPublicKeySize {
		return nil, nil, ErrKyberInvalidPubKeySize
	}

	params := DefaultKyberParams()

	// Decode public key.
	t := make([][]int16, params.K)
	for i := 0; i < params.K; i++ {
		t[i] = decodePolynomial(pk[i*kyberPolyBytes:(i+1)*kyberPolyBytes], 12, params.N)
	}
	seed := pk[params.K*kyberPolyBytes:]

	// Regenerate matrix A.
	matA := expandMatrix(seed, params.K, params.N, params.Q)

	// Sample encryption randomness.
	r := make([][]int16, params.K)
	for i := 0; i < params.K; i++ {
		r[i] = sampleCBD(rng, params.Eta1, params.N)
	}
	e1 := make([][]int16, params.K)
	for i := 0; i < params.K; i++ {
		e1[i] = sampleCBD(rng, params.Eta2, params.N)
	}
	e2 := sampleCBD(rng, params.Eta2, params.N)

	// Generate random message to encapsulate.
	msg := make([]byte, 32)
	if _, err := io.ReadFull(rng, msg); err != nil {
		return nil, nil, ErrKyberRandFailure
	}

	// Compute u = A^T * r + e1 in NTT domain.
	rNTT := make([][]int16, params.K)
	for i := 0; i < params.K; i++ {
		rNTT[i] = NTT(r[i], params.Q)
	}

	u := make([][]int16, params.K)
	for i := 0; i < params.K; i++ {
		u[i] = make([]int16, params.N)
		for j := 0; j < params.K; j++ {
			// A^T[i][j] = A[j][i]
			prod := PolyMul(matA[j][i], rNTT[j], params.Q)
			for c := 0; c < params.N; c++ {
				u[i][c] = modQ16(u[i][c]+prod[c], params.Q)
			}
		}
		// Convert back from NTT and add e1.
		u[i] = InverseNTT(u[i], params.Q)
		for c := 0; c < params.N; c++ {
			u[i][c] = modQ16(u[i][c]+e1[i][c], params.Q)
		}
	}

	// Compute v = t^T * r + e2 + encode(msg).
	v := make([]int16, params.N)
	for j := 0; j < params.K; j++ {
		prod := PolyMul(t[j], rNTT[j], params.Q)
		prodTime := InverseNTT(prod, params.Q)
		for c := 0; c < params.N; c++ {
			v[c] = modQ16(v[c]+prodTime[c], params.Q)
		}
	}
	for c := 0; c < params.N; c++ {
		v[c] = modQ16(v[c]+e2[c], params.Q)
	}
	// Encode message bits into polynomial coefficients.
	msgPoly := decodeMessage(msg, params.N, params.Q)
	for c := 0; c < params.N; c++ {
		v[c] = modQ16(v[c]+msgPoly[c], params.Q)
	}

	// Compress and encode ciphertext.
	ct := make([]byte, 0, kyberCiphertextSize)
	for i := 0; i < params.K; i++ {
		ct = append(ct, CompressBytes(u[i], 10)...)
	}
	ct = append(ct, CompressBytes(v, 4)...)

	// Derive shared secret: SHA-256(msg || H(ct)).
	ctHash := sha256.Sum256(ct)
	ssInput := make([]byte, 0, 64)
	ssInput = append(ssInput, msg...)
	ssInput = append(ssInput, ctHash[:]...)
	ss := sha256.Sum256(ssInput)

	return ct, ss[:], nil
}

// KyberDecapsulate performs KEM decapsulation: given a secret key and
// ciphertext, recovers the shared secret.
func KyberDecapsulate(sk, ciphertext []byte) ([]byte, error) {
	if len(sk) != kyberSecretKeySize {
		return nil, ErrKyberInvalidSecKeySize
	}
	if len(ciphertext) != kyberCiphertextSize {
		return nil, ErrKyberInvalidCiphertext
	}

	params := DefaultKyberParams()

	// Decode secret key (s in NTT domain).
	s := make([][]int16, params.K)
	for i := 0; i < params.K; i++ {
		s[i] = decodePolynomial(sk[i*kyberPolyBytes:(i+1)*kyberPolyBytes], 12, params.N)
	}

	// Decode ciphertext: u (compressed at 10 bits) and v (compressed at 4 bits).
	u := make([][]int16, params.K)
	for i := 0; i < params.K; i++ {
		u[i] = DecompressBytes(
			ciphertext[i*kyberPolyCompBytes:(i+1)*kyberPolyCompBytes],
			10, params.N,
		)
	}
	vOffset := params.K * kyberPolyCompBytes
	v := DecompressBytes(ciphertext[vOffset:], 4, params.N)

	// Compute s^T * u.
	stu := make([]int16, params.N)
	for j := 0; j < params.K; j++ {
		uNTT := NTT(u[j], params.Q)
		prod := PolyMul(s[j], uNTT, params.Q)
		prodTime := InverseNTT(prod, params.Q)
		for c := 0; c < params.N; c++ {
			stu[c] = modQ16(stu[c]+prodTime[c], params.Q)
		}
	}

	// Recover message: decode(v - s^T*u).
	mp := make([]int16, params.N)
	for c := 0; c < params.N; c++ {
		mp[c] = modQ16(v[c]-stu[c], params.Q)
	}
	msg := encodeMessage(mp, params.N, params.Q)

	// Derive shared secret: SHA-256(msg || H(ct)).
	ctHash := sha256.Sum256(ciphertext)
	ssInput := make([]byte, 0, 64)
	ssInput = append(ssInput, msg...)
	ssInput = append(ssInput, ctHash[:]...)
	ss := sha256.Sum256(ssInput)

	return ss[:], nil
}
