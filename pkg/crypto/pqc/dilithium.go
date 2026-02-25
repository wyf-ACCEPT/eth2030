package pqc

import (
	"github.com/eth2030/eth2030/crypto"
)

// DilithiumSigner implements PQSigner for CRYSTALS-Dilithium3.
// Uses real lattice-based key generation, signing, and verification
// from dilithium_sign.go (Fiat-Shamir with aborts over Z_q[X]/(X^N+1)).
type DilithiumSigner struct{}

func (d *DilithiumSigner) Algorithm() PQAlgorithm { return DILITHIUM3 }

// GenerateKey generates a real Dilithium-3 lattice key pair.
// Returns keys at the real lattice sizes (DSign3PubKeyBytes/DSign3SecKeyBytes).
func (d *DilithiumSigner) GenerateKey() (*PQKeyPair, error) {
	pk, sk := DilithiumKeypair()

	// Return keys at the canonical PQKeyPair sizes. If the real lattice
	// sizes are larger, we return the full keys. If smaller, we pad.
	pubKey := make([]byte, Dilithium3PubKeySize)
	secKey := make([]byte, Dilithium3SecKeySize)
	if DSign3PubKeyBytes > Dilithium3PubKeySize {
		pubKey = make([]byte, DSign3PubKeyBytes)
	}
	if DSign3SecKeyBytes > Dilithium3SecKeySize {
		secKey = make([]byte, DSign3SecKeyBytes)
	}
	copy(pubKey, pk)
	copy(secKey, sk)

	return &PQKeyPair{
		Algorithm: DILITHIUM3,
		PublicKey: pubKey,
		SecretKey: secKey,
	}, nil
}

// Sign produces a real Dilithium-3 lattice signature.
func (d *DilithiumSigner) Sign(sk, msg []byte) ([]byte, error) {
	if len(sk) < DSign3SecKeyBytes {
		return nil, ErrInvalidKeySize
	}
	sig := DilithiumSign(DSign3PrivateKey(sk[:DSign3SecKeyBytes]), msg)
	if sig == nil {
		return nil, ErrVerifyFailed
	}
	// Return signature at real lattice size.
	out := make([]byte, Dilithium3SigSize)
	if DSign3SigBytes > Dilithium3SigSize {
		out = make([]byte, DSign3SigBytes)
	}
	copy(out, sig)
	return out, nil
}

// Verify checks a real Dilithium-3 lattice signature against the public key.
func (d *DilithiumSigner) Verify(pk, msg, sig []byte) bool {
	if len(pk) < DSign3PubKeyBytes || len(sig) < DSign3SigBytes {
		return false
	}
	return DilithiumVerify(DSign3PublicKey(pk[:DSign3PubKeyBytes]), msg, DSign3Signature(sig[:DSign3SigBytes]))
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
