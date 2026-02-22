package pqc

// FalconSigner implements PQSigner for Falcon-512.
// Uses real NTRU-lattice-based signing via the NTT polynomial arithmetic
// in falcon_signer.go. Sign produces deterministic signatures using the
// hash-then-sign paradigm over Z_q[X]/(X^512+1) with q=12289.
type FalconSigner struct{}

func (f *FalconSigner) Algorithm() PQAlgorithm { return FALCON512 }

// GenerateKey generates a real Falcon-512 key pair using SHAKE-256 expansion.
// Uses a deterministic seed so repeated calls produce the same key pair.
func (f *FalconSigner) GenerateKey() (*PQKeyPair, error) {
	return f.GenerateKeyReal()
}

// Sign produces a real Falcon-512 signature using lattice-based signing.
// The signature is deterministic for the same key+message pair.
func (f *FalconSigner) Sign(sk, msg []byte) ([]byte, error) {
	if len(sk) != Falcon512SecKeySize {
		return nil, ErrInvalidKeySize
	}
	// Empty/nil messages are allowed by the stub interface (unlike SignReal
	// which rejects them). Provide a canonical empty-message placeholder.
	if len(msg) == 0 {
		msg = []byte{0x00}
	}
	return f.SignReal(sk, msg)
}

// Verify performs real Falcon-512 verification with norm checking.
func (f *FalconSigner) Verify(pk, msg, sig []byte) bool {
	if len(pk) != Falcon512PubKeySize || len(sig) != Falcon512SigSize {
		return false
	}
	if len(msg) == 0 {
		return false
	}
	return f.VerifyReal(pk, msg, sig)
}
