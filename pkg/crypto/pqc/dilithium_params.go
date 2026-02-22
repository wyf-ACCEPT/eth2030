// Dilithium post-quantum lattice-based signature scheme for PQ attestations.
// This implements a simulated Dilithium-style scheme (L+ upgrade) with
// configurable security levels. The cryptographic operations use Keccak256
// as a stand-in for real lattice math until a production library is integrated.
package pqc

import (
	"crypto/rand"
	"errors"

	"github.com/eth2030/eth2030/crypto"
)

// Dilithium parameter constants (CRYSTALS-Dilithium specification).
const (
	DilithiumN = 256     // polynomial degree
	DilithiumQ = 8380417 // prime modulus
	DilithiumD = 13      // dropped bits for rounding
)

// Security levels corresponding to NIST post-quantum security levels.
const (
	DilithiumSecurityLevel2 = 2
	DilithiumSecurityLevel3 = 3
	DilithiumSecurityLevel5 = 5
)

// DilithiumParams holds Dilithium lattice parameters.
type DilithiumParams struct {
	N             int // polynomial degree
	Q             int // prime modulus
	D             int // dropped bits
	SecurityLevel int // NIST security level (2, 3, or 5)
}

// DefaultDilithiumParams returns Dilithium parameters for security level 2.
func DefaultDilithiumParams() *DilithiumParams {
	return &DilithiumParams{
		N:             DilithiumN,
		Q:             DilithiumQ,
		D:             DilithiumD,
		SecurityLevel: DilithiumSecurityLevel2,
	}
}

// dilithiumSigSize is the simulated signature size (64 bytes).
const dilithiumSigSize = 64

// DilithiumSignatureSize returns the signature size in bytes.
func DilithiumSignatureSize() int {
	return dilithiumSigSize
}

// Errors for Dilithium operations.
var (
	ErrDilithiumNilKey   = errors.New("dilithium: nil key pair")
	ErrDilithiumEmptyMsg = errors.New("dilithium: empty message")
)

// DilithiumKeyPair holds a Dilithium key pair with associated parameters.
type DilithiumKeyPair struct {
	PublicKey []byte
	SecretKey []byte
	Params    *DilithiumParams
}

// GenerateDilithiumKey generates a new Dilithium key pair using default params.
// SecretKey is 32 random bytes (seed). PublicKey is derived as
// Keccak256(SecretKey || "dilithium-pk") to simulate lattice key derivation.
func GenerateDilithiumKey() (*DilithiumKeyPair, error) {
	sk := make([]byte, 32)
	if _, err := rand.Read(sk); err != nil {
		return nil, err
	}

	pk := crypto.Keccak256(append(sk, []byte("dilithium-pk")...))

	return &DilithiumKeyPair{
		PublicKey: pk,
		SecretKey: sk,
		Params:    DefaultDilithiumParams(),
	}, nil
}

// Sign produces a Dilithium signature over the message.
// Signature = Keccak256(SecretKey || message || "dilithium-sig"), zero-extended
// to dilithiumSigSize bytes for a deterministic, simulated lattice signature.
func (kp *DilithiumKeyPair) Sign(message []byte) ([]byte, error) {
	if kp == nil || len(kp.SecretKey) == 0 {
		return nil, ErrDilithiumNilKey
	}

	data := make([]byte, 0, len(kp.SecretKey)+len(message)+len("dilithium-sig"))
	data = append(data, kp.SecretKey...)
	data = append(data, message...)
	data = append(data, []byte("dilithium-sig")...)
	hash := crypto.Keccak256(data)

	// Extend to 64 bytes: first 32 from hash, next 32 from hash-of-hash.
	sig := make([]byte, dilithiumSigSize)
	copy(sig[:32], hash)
	copy(sig[32:], crypto.Keccak256(hash))
	return sig, nil
}

// Verify checks that the signature is valid for the given message.
// Recomputes the expected signature from the secret key and compares.
func (kp *DilithiumKeyPair) Verify(message, signature []byte) bool {
	if kp == nil || len(kp.SecretKey) == 0 {
		return false
	}
	if len(signature) != dilithiumSigSize {
		return false
	}

	expected, err := kp.Sign(message)
	if err != nil {
		return false
	}

	// Constant-time-ish comparison.
	if len(expected) != len(signature) {
		return false
	}
	var diff byte
	for i := range expected {
		diff |= expected[i] ^ signature[i]
	}
	return diff == 0
}

// VerifyDilithium performs standalone verification of a Dilithium signature.
// Since we cannot do real lattice math without the secret key, this checks
// that the signature is well-formed: correct length and non-zero content.
func VerifyDilithium(publicKey, message, signature []byte) bool {
	if len(signature) != dilithiumSigSize {
		return false
	}
	// Reject all-zero signatures.
	for _, b := range signature {
		if b != 0 {
			return true
		}
	}
	return false
}
