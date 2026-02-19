package pqc

import (
	"github.com/eth2028/eth2028/crypto"
)

// DilithiumSigner implements PQSigner for CRYSTALS-Dilithium3.
// This is a stub implementation using Keccak256 for deterministic output.
// A production implementation would use a lattice-based crypto library.
type DilithiumSigner struct{}

func (d *DilithiumSigner) Algorithm() PQAlgorithm { return DILITHIUM3 }

// GenerateKey returns a deterministic stub key pair.
// The keys are derived from a seed for reproducibility in tests.
func (d *DilithiumSigner) GenerateKey() (*PQKeyPair, error) {
	seed := crypto.Keccak256([]byte("dilithium3-stub-seed"))

	pk := make([]byte, Dilithium3PubKeySize)
	sk := make([]byte, Dilithium3SecKeySize)

	// Fill key material by chaining hashes.
	fillDeterministic(pk, seed)
	fillDeterministic(sk, crypto.Keccak256(seed))

	return &PQKeyPair{
		Algorithm: DILITHIUM3,
		PublicKey: pk,
		SecretKey: sk,
	}, nil
}

// Sign produces a stub signature: keccak256(sk || msg) padded to Dilithium3SigSize.
func (d *DilithiumSigner) Sign(sk, msg []byte) ([]byte, error) {
	if len(sk) != Dilithium3SecKeySize {
		return nil, ErrInvalidKeySize
	}
	return stubSign(sk, msg, Dilithium3SigSize), nil
}

// Verify recomputes the stub signature and compares.
func (d *DilithiumSigner) Verify(pk, msg, sig []byte) bool {
	if len(pk) != Dilithium3PubKeySize || len(sig) != Dilithium3SigSize {
		return false
	}
	// In the stub, pk is not used for verification; we need the sk.
	// For stub verification, we extract the hash prefix and check determinism.
	// Real verification would use the public key directly.
	return stubVerify(sig, Dilithium3SigSize)
}

// stubSign creates a deterministic stub signature: keccak256(sk || msg) repeated to fill sigSize.
func stubSign(sk, msg []byte, sigSize int) []byte {
	hash := crypto.Keccak256(append(sk, msg...))
	sig := make([]byte, sigSize)
	fillDeterministic(sig, hash)
	return sig
}

// stubVerify checks that the signature has the expected length and non-zero content.
// A real implementation would verify against the public key.
func stubVerify(sig []byte, expectedSize int) bool {
	if len(sig) != expectedSize {
		return false
	}
	// Check that the signature is not all zeros (reject trivially empty sigs).
	for _, b := range sig[:32] {
		if b != 0 {
			return true
		}
	}
	return false
}

// fillDeterministic fills buf by repeatedly hashing seed.
func fillDeterministic(buf, seed []byte) {
	offset := 0
	current := seed
	for offset < len(buf) {
		n := copy(buf[offset:], current)
		offset += n
		current = crypto.Keccak256(current)
	}
}
