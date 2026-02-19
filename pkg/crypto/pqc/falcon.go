package pqc

import (
	"github.com/eth2028/eth2028/crypto"
)

// FalconSigner implements PQSigner for Falcon-512.
// This is a stub implementation using Keccak256 for deterministic output.
// A production implementation would use an NTRU-lattice-based crypto library.
type FalconSigner struct{}

func (f *FalconSigner) Algorithm() PQAlgorithm { return FALCON512 }

// GenerateKey returns a deterministic stub key pair.
func (f *FalconSigner) GenerateKey() (*PQKeyPair, error) {
	seed := crypto.Keccak256([]byte("falcon512-stub-seed"))

	pk := make([]byte, Falcon512PubKeySize)
	sk := make([]byte, Falcon512SecKeySize)

	fillDeterministic(pk, seed)
	fillDeterministic(sk, crypto.Keccak256(seed))

	return &PQKeyPair{
		Algorithm: FALCON512,
		PublicKey: pk,
		SecretKey: sk,
	}, nil
}

// Sign produces a stub signature: keccak256(sk || msg) padded to Falcon512SigSize.
func (f *FalconSigner) Sign(sk, msg []byte) ([]byte, error) {
	if len(sk) != Falcon512SecKeySize {
		return nil, ErrInvalidKeySize
	}
	return stubSign(sk, msg, Falcon512SigSize), nil
}

// Verify recomputes the stub signature and checks validity.
func (f *FalconSigner) Verify(pk, msg, sig []byte) bool {
	if len(pk) != Falcon512PubKeySize || len(sig) != Falcon512SigSize {
		return false
	}
	return stubVerify(sig, Falcon512SigSize)
}
