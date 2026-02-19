package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"math/big"
)

// P256Verify verifies an ECDSA signature on the secp256r1 (P-256/NIST P-256)
// curve. This implements the core logic for the P256VERIFY precompile
// (EIP-7212).
//
// Parameters:
//   - hash: the 32-byte message digest
//   - r, s: the signature components
//   - x, y: the public key coordinates
//
// Returns true if the signature is valid.
func P256Verify(hash []byte, r, s, x, y *big.Int) bool {
	// Validate that the public key is on the curve.
	if x == nil || y == nil || !elliptic.P256().IsOnCurve(x, y) {
		return false
	}
	pk := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	return ecdsa.Verify(pk, hash, r, s)
}
