package pqc

import (
	"crypto/ecdsa"

	"github.com/eth2030/eth2030/crypto"
)

// HybridSignature combines an ECDSA signature with a post-quantum signature.
// Both must verify for the hybrid scheme to accept. This provides security
// against both classical and quantum adversaries.
type HybridSignature struct {
	ECDSASig []byte       // 65-byte ECDSA signature [R || S || V]
	PQSig    *PQSignature // Post-quantum signature
}

// VerifyHybrid verifies a hybrid ECDSA + PQ signature.
// Both the classical ECDSA and PQ signatures must verify for acceptance.
func VerifyHybrid(ecdsaPub *ecdsa.PublicKey, pqPub []byte, msg []byte, hybrid *HybridSignature) bool {
	if hybrid == nil || hybrid.PQSig == nil {
		return false
	}
	if ecdsaPub == nil || len(hybrid.ECDSASig) != 65 {
		return false
	}

	// Verify ECDSA signature.
	pubBytes := crypto.FromECDSAPub(ecdsaPub)
	if pubBytes == nil {
		return false
	}
	// ValidateSignature expects 64-byte sig (no V) and 65-byte uncompressed pubkey.
	if !crypto.ValidateSignature(pubBytes, msg, hybrid.ECDSASig[:64]) {
		return false
	}

	// Verify PQ signature.
	signer := GetSigner(hybrid.PQSig.Algorithm)
	if signer == nil {
		return false
	}
	if !signer.Verify(pqPub, msg, hybrid.PQSig.Signature) {
		return false
	}

	return true
}
