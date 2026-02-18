package crypto

// BN254 precompile interface functions.
//
// These functions provide the EVM precompile interface for BN254 (alt_bn128)
// elliptic curve operations as defined in EIP-196 and EIP-197.

import (
	"errors"
	"math/big"
)

var (
	errBN254InvalidPoint  = errors.New("bn254: invalid point")
	errBN254InvalidG2     = errors.New("bn254: invalid G2 point")
	errBN254InvalidLength = errors.New("bn254: invalid input length")
)

// BN254Add performs point addition on the BN254 curve (precompile 0x06).
// Input: 128 bytes (x1, y1, x2, y2) as 32-byte big-endian integers.
// Short input is right-padded with zeros.
// Output: 64 bytes (x3, y3).
func BN254Add(input []byte) ([]byte, error) {
	input = bn254PadRight(input, 128)

	x1 := new(big.Int).SetBytes(input[0:32])
	y1 := new(big.Int).SetBytes(input[32:64])
	x2 := new(big.Int).SetBytes(input[64:96])
	y2 := new(big.Int).SetBytes(input[96:128])

	// Validate both points.
	if !g1IsOnCurve(x1, y1) {
		return nil, errBN254InvalidPoint
	}
	if !g1IsOnCurve(x2, y2) {
		return nil, errBN254InvalidPoint
	}

	p1 := g1FromAffine(x1, y1)
	p2 := g1FromAffine(x2, y2)

	r := g1Add(p1, p2)
	rx, ry := r.g1ToAffine()

	return bn254EncodeG1(rx, ry), nil
}

// BN254ScalarMul performs scalar multiplication on the BN254 curve (precompile 0x07).
// Input: 96 bytes (x, y, s) as 32-byte big-endian integers.
// Short input is right-padded with zeros.
// Output: 64 bytes (x', y').
func BN254ScalarMul(input []byte) ([]byte, error) {
	input = bn254PadRight(input, 96)

	x := new(big.Int).SetBytes(input[0:32])
	y := new(big.Int).SetBytes(input[32:64])
	s := new(big.Int).SetBytes(input[64:96])

	if !g1IsOnCurve(x, y) {
		return nil, errBN254InvalidPoint
	}

	p := g1FromAffine(x, y)
	r := G1ScalarMul(p, s)
	rx, ry := r.g1ToAffine()

	return bn254EncodeG1(rx, ry), nil
}

// BN254PairingCheck performs the pairing check (precompile 0x08).
// Input: k * 192 bytes, each 192-byte chunk is (G1_x, G1_y, G2_x_imag, G2_x_real,
// G2_y_imag, G2_y_real) as 32-byte big-endian integers.
// Output: 32 bytes, 1 if product of pairings equals identity, 0 otherwise.
func BN254PairingCheck(input []byte) ([]byte, error) {
	if len(input)%192 != 0 {
		return nil, errBN254InvalidLength
	}

	k := len(input) / 192
	if k == 0 {
		// Empty input: trivially true (empty product is identity).
		return bn254PairingResult(true), nil
	}

	g1Points := make([]*G1Point, k)
	g2Points := make([]*G2Point, k)

	for i := 0; i < k; i++ {
		offset := i * 192

		// Parse G1 point (64 bytes).
		g1x := new(big.Int).SetBytes(input[offset : offset+32])
		g1y := new(big.Int).SetBytes(input[offset+32 : offset+64])

		if !g1IsOnCurve(g1x, g1y) {
			return nil, errBN254InvalidPoint
		}
		g1Points[i] = g1FromAffine(g1x, g1y)

		// Parse G2 point (128 bytes).
		// Layout: x_imag(32) | x_real(32) | y_imag(32) | y_real(32)
		g2xImag := new(big.Int).SetBytes(input[offset+64 : offset+96])
		g2xReal := new(big.Int).SetBytes(input[offset+96 : offset+128])
		g2yImag := new(big.Int).SetBytes(input[offset+128 : offset+160])
		g2yReal := new(big.Int).SetBytes(input[offset+160 : offset+192])

		// Validate field elements.
		if g2xImag.Cmp(bn254P) >= 0 || g2xReal.Cmp(bn254P) >= 0 ||
			g2yImag.Cmp(bn254P) >= 0 || g2yReal.Cmp(bn254P) >= 0 {
			return nil, errBN254InvalidG2
		}

		g2x := &fp2{a0: g2xReal, a1: g2xImag}
		g2y := &fp2{a0: g2yReal, a1: g2yImag}

		// Check if G2 point is the identity.
		if g2x.isZero() && g2y.isZero() {
			g2Points[i] = G2Infinity()
			continue
		}

		if !g2IsOnCurve(g2x, g2y) {
			return nil, errBN254InvalidG2
		}
		g2Points[i] = g2FromAffine(g2x, g2y)
	}

	ok := bn254CheckPairing(g1Points, g2Points)
	return bn254PairingResult(ok), nil
}

// bn254CheckPairing performs the actual multi-pairing check.
func bn254CheckPairing(g1Points []*G1Point, g2Points []*G2Point) bool {
	return bn254MultiPairing(g1Points, g2Points)
}

// bn254EncodeG1 encodes a G1 point as 64 bytes (x, y) big-endian.
func bn254EncodeG1(x, y *big.Int) []byte {
	out := make([]byte, 64)
	xBytes := x.Bytes()
	yBytes := y.Bytes()
	copy(out[32-len(xBytes):32], xBytes)
	copy(out[64-len(yBytes):64], yBytes)
	return out
}

// bn254PairingResult encodes a pairing check result as 32 bytes.
func bn254PairingResult(ok bool) []byte {
	out := make([]byte, 32)
	if ok {
		out[31] = 1
	}
	return out
}

// bn254PadRight pads data with zeros on the right to reach minLen.
func bn254PadRight(data []byte, minLen int) []byte {
	if len(data) >= minLen {
		return data[:minLen]
	}
	padded := make([]byte, minLen)
	copy(padded, data)
	return padded
}
