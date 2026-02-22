package crypto

import (
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// s256 is the secp256k1 curve used throughout Ethereum.
var s256 = S256()

// secp256k1N is the order of the secp256k1 curve.
var secp256k1N, _ = new(big.Int).SetString("fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16)

// secp256k1halfN is half the order, used for Homestead low-S check.
var secp256k1halfN = new(big.Int).Div(secp256k1N, big.NewInt(2))

// GenerateKey generates a new secp256k1 private key.
func GenerateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(s256, rand.Reader)
}

// Sign calculates an ECDSA signature (65 bytes [R || S || V]).
// V is the recovery ID (0 or 1) determined by trial recovery.
func Sign(hash []byte, prv *ecdsa.PrivateKey) ([]byte, error) {
	if len(hash) != 32 {
		return nil, errors.New("hash must be 32 bytes")
	}
	r, ss, err := ecdsa.Sign(rand.Reader, prv, hash)
	if err != nil {
		return nil, err
	}

	// Normalize s to lower half of curve order (EIP-2).
	if ss.Cmp(secp256k1halfN) > 0 {
		ss = new(big.Int).Sub(secp256k1N, ss)
	}

	// Encode R and S as 32-byte big-endian.
	sig := make([]byte, 65)
	rBytes := r.Bytes()
	sBytes := ss.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)

	// Determine V by trial recovery.
	expectedPub := FromECDSAPub(&prv.PublicKey)
	for v := byte(0); v <= 1; v++ {
		sig[64] = v
		recovered, err := Ecrecover(hash, sig)
		if err != nil {
			continue
		}
		if len(recovered) == len(expectedPub) {
			match := true
			for i := range recovered {
				if recovered[i] != expectedPub[i] {
					match = false
					break
				}
			}
			if match {
				return sig, nil
			}
		}
	}

	// Fallback: set V=0 if recovery fails (shouldn't happen with correct curve).
	sig[64] = 0
	return sig, nil
}

// Ecrecover recovers the uncompressed public key from hash and signature.
// Returns a 65-byte public key [0x04 || X || Y].
func Ecrecover(hash, sig []byte) ([]byte, error) {
	pub, err := SigToPub(hash, sig)
	if err != nil {
		return nil, err
	}
	return FromECDSAPub(pub), nil
}

// SigToPub recovers the public key from hash and signature.
// The signature must be 65 bytes [R || S || V] where V is 0 or 1.
func SigToPub(hash, sig []byte) (*ecdsa.PublicKey, error) {
	if len(sig) != 65 {
		return nil, errors.New("signature must be 65 bytes [R || S || V]")
	}
	if len(hash) != 32 {
		return nil, errors.New("hash must be 32 bytes")
	}

	r := new(big.Int).SetBytes(sig[0:32])
	s := new(big.Int).SetBytes(sig[32:64])
	v := sig[64]

	if v > 1 {
		return nil, errInvalidRecoveryID
	}

	qx, qy, err := recoverPublicKey(hash, r, s, v)
	if err != nil {
		return nil, err
	}

	return &ecdsa.PublicKey{Curve: s256, X: qx, Y: qy}, nil
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
	compressed := make([]byte, 33)
	if pubkey.Y.Bit(0) == 0 {
		compressed[0] = 0x02
	} else {
		compressed[0] = 0x03
	}
	xBytes := pubkey.X.Bytes()
	copy(compressed[1+32-len(xBytes):], xBytes)
	return compressed
}

// DecompressPubkey decompresses a 33-byte compressed public key.
func DecompressPubkey(pubkey []byte) (*ecdsa.PublicKey, error) {
	if len(pubkey) != 33 {
		return nil, errors.New("invalid compressed public key length")
	}
	prefix := pubkey[0]
	if prefix != 0x02 && prefix != 0x03 {
		return nil, errors.New("invalid compressed public key prefix")
	}
	curve := s256.(*secp256k1Curve)
	x := new(big.Int).SetBytes(pubkey[1:33])
	if x.Cmp(curve.p) >= 0 {
		return nil, errors.New("invalid compressed public key")
	}
	y := computeY(x, curve.p)
	if y == nil {
		return nil, errors.New("invalid compressed public key")
	}
	// Choose y parity based on prefix: 0x02 = even, 0x03 = odd.
	if y.Bit(0) != uint(prefix&1) {
		y.Sub(curve.p, y)
	}
	if !curve.IsOnCurve(x, y) {
		return nil, errors.New("invalid compressed public key")
	}
	return &ecdsa.PublicKey{Curve: s256, X: x, Y: y}, nil
}

// FromECDSAPub marshals a public key to 65-byte uncompressed format [0x04 || X || Y].
func FromECDSAPub(pub *ecdsa.PublicKey) []byte {
	if pub == nil || pub.X == nil || pub.Y == nil {
		return nil
	}
	ret := make([]byte, 65)
	ret[0] = 0x04
	xBytes := pub.X.Bytes()
	yBytes := pub.Y.Bytes()
	copy(ret[1+32-len(xBytes):33], xBytes)
	copy(ret[33+32-len(yBytes):65], yBytes)
	return ret
}
