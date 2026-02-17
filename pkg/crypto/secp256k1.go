package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// TODO: Replace elliptic.P256() with actual secp256k1 curve parameters.
// Go stdlib does not include secp256k1; using P256 as a placeholder.
var s256 = elliptic.P256()

// secp256k1N is the order of the secp256k1 curve.
// This is the real secp256k1 N value used for signature validation.
var secp256k1N, _ = new(big.Int).SetString("fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16)

// secp256k1halfN is half the order, used for Homestead low-S check.
var secp256k1halfN = new(big.Int).Div(secp256k1N, big.NewInt(2))

// GenerateKey generates a new secp256k1 private key.
func GenerateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(s256, rand.Reader)
}

// Sign calculates an ECDSA signature (65 bytes [R || S || V]).
// TODO: V (recovery ID) is set to 0 as a placeholder. A proper implementation
// requires trial recovery to determine the correct V value.
func Sign(hash []byte, prv *ecdsa.PrivateKey) ([]byte, error) {
	if len(hash) != 32 {
		return nil, errors.New("hash must be 32 bytes")
	}
	r, ss, err := ecdsa.Sign(rand.Reader, prv, hash)
	if err != nil {
		return nil, err
	}
	// Encode R and S as 32-byte big-endian, plus V=0 placeholder.
	sig := make([]byte, 65)
	rBytes := r.Bytes()
	sBytes := ss.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)
	sig[64] = 0 // V placeholder
	return sig, nil
}

// Ecrecover recovers the uncompressed public key from hash and signature.
// TODO: Proper ecrecover requires secp256k1 curve and recovery ID (V).
// This placeholder verifies the signature against the recovered key.
func Ecrecover(hash, sig []byte) ([]byte, error) {
	pub, err := SigToPub(hash, sig)
	if err != nil {
		return nil, err
	}
	return FromECDSAPub(pub), nil
}

// SigToPub recovers the public key from hash and signature.
// TODO: This is a placeholder. Real implementation needs secp256k1 recovery
// using the V byte. Currently returns an error as proper recovery is not
// possible with the P256 placeholder curve.
func SigToPub(hash, sig []byte) (*ecdsa.PublicKey, error) {
	if len(sig) != 65 {
		return nil, errors.New("signature must be 65 bytes [R || S || V]")
	}
	if len(hash) != 32 {
		return nil, errors.New("hash must be 32 bytes")
	}
	// TODO: Implement proper secp256k1 public key recovery using V byte.
	return nil, errors.New("ecrecover not implemented: requires secp256k1 curve")
}

// ValidateSignature verifies that the given signature (64 bytes, no V) is valid
// for the provided 65-byte uncompressed public key and 32-byte hash.
func ValidateSignature(pubkey, hash, sig []byte) bool {
	if len(sig) != 64 {
		return false
	}
	if len(hash) != 32 {
		return false
	}
	if len(pubkey) != 65 || pubkey[0] != 0x04 {
		return false
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	x := new(big.Int).SetBytes(pubkey[1:33])
	y := new(big.Int).SetBytes(pubkey[33:65])
	pub := &ecdsa.PublicKey{Curve: s256, X: x, Y: y}
	return ecdsa.Verify(pub, hash, r, s)
}

// ValidateSignatureValues checks r, s, v for validity per Homestead rules.
// If homestead is true, s must be in the lower half of the curve order.
func ValidateSignatureValues(v byte, r, s *big.Int, homestead bool) bool {
	if r == nil || s == nil {
		return false
	}
	if v > 1 {
		return false
	}
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return false
	}
	if r.Cmp(secp256k1N) >= 0 || s.Cmp(secp256k1N) >= 0 {
		return false
	}
	if homestead && s.Cmp(secp256k1halfN) > 0 {
		return false
	}
	return true
}

// PubkeyToAddress derives the Ethereum address from a public key.
// Address = Keccak256(pubkey[1:])[12:]
func PubkeyToAddress(p ecdsa.PublicKey) types.Address {
	pubBytes := FromECDSAPub(&p)
	if pubBytes == nil {
		return types.Address{}
	}
	hash := Keccak256(pubBytes[1:])
	return types.BytesToAddress(hash[12:])
}

// CompressPubkey compresses a 65-byte uncompressed public key to 33 bytes.
func CompressPubkey(pubkey *ecdsa.PublicKey) []byte {
	if pubkey == nil || pubkey.X == nil || pubkey.Y == nil {
		return nil
	}
	return elliptic.MarshalCompressed(s256, pubkey.X, pubkey.Y)
}

// DecompressPubkey decompresses a 33-byte compressed public key.
func DecompressPubkey(pubkey []byte) (*ecdsa.PublicKey, error) {
	if len(pubkey) != 33 {
		return nil, errors.New("invalid compressed public key length")
	}
	x, y := elliptic.UnmarshalCompressed(s256, pubkey)
	if x == nil {
		return nil, errors.New("invalid compressed public key")
	}
	return &ecdsa.PublicKey{Curve: s256, X: x, Y: y}, nil
}

// FromECDSAPub marshals a public key to 65-byte uncompressed format.
func FromECDSAPub(pub *ecdsa.PublicKey) []byte {
	if pub == nil || pub.X == nil || pub.Y == nil {
		return nil
	}
	return elliptic.Marshal(pub.Curve, pub.X, pub.Y)
}
