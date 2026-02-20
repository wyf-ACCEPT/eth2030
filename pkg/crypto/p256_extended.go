package crypto

// Full P-256 (secp256r1/NIST P-256) ECDSA operations: key generation,
// signing, verification, public key recovery, DER encoding, compressed
// key handling, and NIST curve utilities. Complements the existing
// P256Verify function for the EIP-7212 precompile.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/asn1"
	"errors"
	"math/big"
)

// P-256 curve parameters cached for efficiency.
var (
	p256Curve  = elliptic.P256()
	p256Params = p256Curve.Params()
	p256N      = p256Params.N
	p256HalfN  = new(big.Int).Rsh(p256N, 1)
)

// P-256 error sentinels.
var (
	errP256InvalidKey    = errors.New("p256: invalid key")
	errP256InvalidSig    = errors.New("p256: invalid signature")
	errP256InvalidDER    = errors.New("p256: invalid DER encoding")
	errP256RecoveryFail  = errors.New("p256: public key recovery failed")
	errP256OffCurve      = errors.New("p256: point not on curve")
	errP256InvalidPubKey = errors.New("p256: invalid public key encoding")
)

// P256GenerateKey generates a new P-256 ECDSA private key.
func P256GenerateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(p256Curve, rand.Reader)
}

// P256Sign signs a 32-byte hash using ECDSA on P-256. Returns a 64-byte
// signature [R(32) || S(32)] with S normalized to the lower half of the
// curve order (for signature malleability prevention).
func P256Sign(hash []byte, prv *ecdsa.PrivateKey) ([]byte, error) {
	if len(hash) != 32 {
		return nil, errors.New("p256: hash must be 32 bytes")
	}
	if prv == nil || prv.Curve != p256Curve {
		return nil, errP256InvalidKey
	}

	r, s, err := ecdsa.Sign(rand.Reader, prv, hash)
	if err != nil {
		return nil, err
	}

	// Normalize S to lower half (prevent malleability).
	if s.Cmp(p256HalfN) > 0 {
		s = new(big.Int).Sub(p256N, s)
	}

	sig := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)
	return sig, nil
}

// P256VerifyCompact verifies a 64-byte compact P-256 ECDSA signature.
func P256VerifyCompact(hash, sig []byte, pub *ecdsa.PublicKey) bool {
	if len(hash) != 32 || len(sig) != 64 || pub == nil {
		return false
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	return P256Verify(hash, r, s, pub.X, pub.Y)
}

// p256ECDSASig is the ASN.1 structure for DER-encoded ECDSA signatures.
type p256ECDSASig struct {
	R, S *big.Int
}

// P256SignDER signs a hash using P-256 and returns a DER-encoded signature.
func P256SignDER(hash []byte, prv *ecdsa.PrivateKey) ([]byte, error) {
	if len(hash) != 32 {
		return nil, errors.New("p256: hash must be 32 bytes")
	}
	if prv == nil || prv.Curve != p256Curve {
		return nil, errP256InvalidKey
	}

	r, s, err := ecdsa.Sign(rand.Reader, prv, hash)
	if err != nil {
		return nil, err
	}

	return asn1.Marshal(p256ECDSASig{R: r, S: s})
}

// P256VerifyDER verifies a DER-encoded P-256 ECDSA signature.
func P256VerifyDER(hash, derSig []byte, pub *ecdsa.PublicKey) bool {
	if len(hash) != 32 || pub == nil || len(derSig) == 0 {
		return false
	}
	var sig p256ECDSASig
	rest, err := asn1.Unmarshal(derSig, &sig)
	if err != nil || len(rest) > 0 {
		return false
	}
	if sig.R == nil || sig.S == nil {
		return false
	}
	if sig.R.Sign() <= 0 || sig.S.Sign() <= 0 {
		return false
	}
	return P256Verify(hash, sig.R, sig.S, pub.X, pub.Y)
}

// P256MarshalDER encodes an ECDSA signature (r, s) in DER format.
func P256MarshalDER(r, s *big.Int) ([]byte, error) {
	if r == nil || s == nil || r.Sign() <= 0 || s.Sign() <= 0 {
		return nil, errP256InvalidSig
	}
	return asn1.Marshal(p256ECDSASig{R: r, S: s})
}

// P256UnmarshalDER decodes a DER-encoded ECDSA signature into (r, s).
func P256UnmarshalDER(der []byte) (r, s *big.Int, err error) {
	var sig p256ECDSASig
	rest, err := asn1.Unmarshal(der, &sig)
	if err != nil {
		return nil, nil, errP256InvalidDER
	}
	if len(rest) > 0 {
		return nil, nil, errP256InvalidDER
	}
	if sig.R == nil || sig.S == nil || sig.R.Sign() <= 0 || sig.S.Sign() <= 0 {
		return nil, nil, errP256InvalidSig
	}
	return sig.R, sig.S, nil
}

// P256CompressPubkey compresses a P-256 public key to 33 bytes.
// Returns [0x02 || X] if Y is even, [0x03 || X] if Y is odd.
func P256CompressPubkey(pub *ecdsa.PublicKey) ([]byte, error) {
	if pub == nil || pub.X == nil || pub.Y == nil || pub.Curve != p256Curve {
		return nil, errP256InvalidKey
	}
	if !p256Curve.IsOnCurve(pub.X, pub.Y) {
		return nil, errP256OffCurve
	}
	compressed := make([]byte, 33)
	if pub.Y.Bit(0) == 0 {
		compressed[0] = 0x02
	} else {
		compressed[0] = 0x03
	}
	xBytes := pub.X.Bytes()
	copy(compressed[1+32-len(xBytes):], xBytes)
	return compressed, nil
}

// P256DecompressPubkey decompresses a 33-byte compressed P-256 public key.
func P256DecompressPubkey(compressed []byte) (*ecdsa.PublicKey, error) {
	if len(compressed) != 33 {
		return nil, errP256InvalidPubKey
	}
	prefix := compressed[0]
	if prefix != 0x02 && prefix != 0x03 {
		return nil, errP256InvalidPubKey
	}

	x := new(big.Int).SetBytes(compressed[1:33])
	if x.Sign() < 0 || x.Cmp(p256Params.P) >= 0 {
		return nil, errP256InvalidPubKey
	}

	// Compute y^2 = x^3 - 3x + b (mod p) for P-256: a = -3, b = params.B.
	p := p256Params.P
	x3 := new(big.Int).Mul(x, x)
	x3.Mod(x3, p)
	x3.Mul(x3, x)
	x3.Mod(x3, p)

	threeX := new(big.Int).Mul(big.NewInt(3), x)
	threeX.Mod(threeX, p)

	y2 := new(big.Int).Sub(x3, threeX)
	y2.Add(y2, p256Params.B)
	y2.Mod(y2, p)

	// Compute sqrt(y2) mod p. P-256 has p = 3 mod 4.
	// sqrt(a) = a^((p+1)/4) mod p.
	exp := new(big.Int).Add(p, big.NewInt(1))
	exp.Rsh(exp, 2)
	y := new(big.Int).Exp(y2, exp, p)

	// Verify: y^2 == y2 mod p.
	check := new(big.Int).Mul(y, y)
	check.Mod(check, p)
	if check.Cmp(y2) != 0 {
		return nil, errP256OffCurve
	}

	// Select parity.
	if y.Bit(0) != uint(prefix&1) {
		y.Sub(p, y)
	}

	if !p256Curve.IsOnCurve(x, y) {
		return nil, errP256OffCurve
	}
	return &ecdsa.PublicKey{Curve: p256Curve, X: x, Y: y}, nil
}

// P256MarshalUncompressed returns the 65-byte uncompressed representation
// [0x04 || X(32) || Y(32)].
func P256MarshalUncompressed(pub *ecdsa.PublicKey) ([]byte, error) {
	if pub == nil || pub.X == nil || pub.Y == nil {
		return nil, errP256InvalidKey
	}
	ret := make([]byte, 65)
	ret[0] = 0x04
	xBytes := pub.X.Bytes()
	yBytes := pub.Y.Bytes()
	copy(ret[1+32-len(xBytes):33], xBytes)
	copy(ret[33+32-len(yBytes):65], yBytes)
	return ret, nil
}

// P256UnmarshalPubkey parses a P-256 public key from either compressed
// (33 bytes) or uncompressed (65 bytes) encoding.
func P256UnmarshalPubkey(data []byte) (*ecdsa.PublicKey, error) {
	switch len(data) {
	case 33:
		return P256DecompressPubkey(data)
	case 65:
		if data[0] != 0x04 {
			return nil, errP256InvalidPubKey
		}
		x := new(big.Int).SetBytes(data[1:33])
		y := new(big.Int).SetBytes(data[33:65])
		if !p256Curve.IsOnCurve(x, y) {
			return nil, errP256OffCurve
		}
		return &ecdsa.PublicKey{Curve: p256Curve, X: x, Y: y}, nil
	default:
		return nil, errP256InvalidPubKey
	}
}

// P256RecoverPubkey attempts to recover the P-256 public key from a hash,
// compact signature [R(32)||S(32)], and recovery ID (0 or 1).
// This uses the ECDSA public key recovery algorithm:
//   1. Parse R, S from the signature.
//   2. Compute candidate R point from r and recID.
//   3. Compute the public key Q = r^{-1} * (s*R - e*G).
func P256RecoverPubkey(hash []byte, sig []byte, recID byte) (*ecdsa.PublicKey, error) {
	if len(hash) != 32 || len(sig) != 64 || recID > 1 {
		return nil, errP256RecoveryFail
	}

	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return nil, errP256RecoveryFail
	}
	if r.Cmp(p256N) >= 0 || s.Cmp(p256N) >= 0 {
		return nil, errP256RecoveryFail
	}

	// Recover the R point on the curve.
	// x coordinate = r (we ignore the case r + N < p for simplicity since
	// for P-256, N and P are very close, making this case astronomically rare).
	x := new(big.Int).Set(r)

	// Compute y from x on the P-256 curve: y^2 = x^3 - 3x + b.
	p := p256Params.P
	x3 := new(big.Int).Mul(x, x)
	x3.Mod(x3, p)
	x3.Mul(x3, x)
	x3.Mod(x3, p)

	threeX := new(big.Int).Mul(big.NewInt(3), x)
	threeX.Mod(threeX, p)

	y2 := new(big.Int).Sub(x3, threeX)
	y2.Add(y2, p256Params.B)
	y2.Mod(y2, p)

	// Compute sqrt.
	exp := new(big.Int).Add(p, big.NewInt(1))
	exp.Rsh(exp, 2)
	y := new(big.Int).Exp(y2, exp, p)
	check := new(big.Int).Mul(y, y)
	check.Mod(check, p)
	if check.Cmp(y2) != 0 {
		return nil, errP256RecoveryFail
	}

	// Choose y parity based on recID.
	if y.Bit(0) != uint(recID) {
		y.Sub(p, y)
	}

	if !p256Curve.IsOnCurve(x, y) {
		return nil, errP256RecoveryFail
	}

	// Q = r^{-1} * (s*R - e*G)
	rInv := new(big.Int).ModInverse(r, p256N)
	if rInv == nil {
		return nil, errP256RecoveryFail
	}

	e := new(big.Int).SetBytes(hash)

	// s*R
	sRx, sRy := p256Curve.ScalarMult(x, y, s.Bytes())
	// e*G
	eGx, eGy := p256Curve.ScalarBaseMult(e.Bytes())
	// -e*G
	negEGy := new(big.Int).Sub(p, eGy)

	// s*R - e*G
	sumX, sumY := p256Curve.Add(sRx, sRy, eGx, negEGy)

	// r^{-1} * (s*R - e*G)
	qx, qy := p256Curve.ScalarMult(sumX, sumY, rInv.Bytes())

	if !p256Curve.IsOnCurve(qx, qy) {
		return nil, errP256RecoveryFail
	}

	return &ecdsa.PublicKey{Curve: p256Curve, X: qx, Y: qy}, nil
}

// P256ValidateSignatureValues checks that r and s are in valid range for
// P-256 ECDSA. If lowS is true, also checks that S is in the lower half.
func P256ValidateSignatureValues(r, s *big.Int, lowS bool) bool {
	if r == nil || s == nil {
		return false
	}
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return false
	}
	if r.Cmp(p256N) >= 0 || s.Cmp(p256N) >= 0 {
		return false
	}
	if lowS && s.Cmp(p256HalfN) > 0 {
		return false
	}
	return true
}

// P256ScalarBaseMult computes k*G on P-256 and returns the resulting point.
func P256ScalarBaseMult(k *big.Int) (x, y *big.Int) {
	return p256Curve.ScalarBaseMult(k.Bytes())
}

// P256ScalarMult computes k*P on P-256 for point (px, py).
func P256ScalarMult(px, py, k *big.Int) (x, y *big.Int) {
	return p256Curve.ScalarMult(px, py, k.Bytes())
}

// P256PointAdd adds two points on P-256.
func P256PointAdd(x1, y1, x2, y2 *big.Int) (x, y *big.Int) {
	return p256Curve.Add(x1, y1, x2, y2)
}

// P256IsOnCurve checks if (x, y) is on the P-256 curve.
func P256IsOnCurve(x, y *big.Int) bool {
	return p256Curve.IsOnCurve(x, y)
}
