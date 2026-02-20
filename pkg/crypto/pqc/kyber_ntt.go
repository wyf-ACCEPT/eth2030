// kyber_ntt.go provides NTT-based polynomial arithmetic and encoding helpers
// for the Kyber-768 KEM implementation in key_exchange.go.
//
// Functions: NTT, InverseNTT, PolyMul (pointwise), CompressBytes,
// DecompressBytes, modQ16, sampleCBD, expandMatrix, encodePolynomial,
// decodePolynomial, decodeMessage, encodeMessage.
package pqc

import (
	"crypto/sha256"
	"encoding/binary"
	"io"
)

// kyberZetas contains the precomputed NTT twiddle factors for Kyber
// (q = 3329, primitive root of unity = 17).
var kyberZetas = [128]int16{
	1, 1729, 2580, 3289, 2642, 630, 1897, 848,
	1062, 1919, 193, 797, 2786, 3260, 569, 1746,
	296, 2447, 1339, 1476, 3046, 56, 2240, 1333,
	1426, 2094, 535, 2882, 2393, 2879, 1974, 821,
	289, 331, 3253, 1756, 1197, 2304, 2277, 2055,
	650, 1977, 2513, 632, 2865, 33, 1320, 1915,
	2319, 1435, 807, 452, 1438, 2868, 1534, 2402,
	2647, 2617, 1481, 648, 2474, 3110, 1227, 910,
	17, 2761, 583, 2649, 1637, 723, 2288, 1100,
	1409, 2662, 3281, 233, 756, 2156, 3015, 3050,
	1703, 1651, 2789, 1789, 1847, 952, 1461, 2687,
	939, 2308, 2437, 2388, 733, 2337, 268, 641,
	1584, 2298, 2037, 3220, 375, 2549, 2090, 1645,
	1063, 319, 2773, 757, 2099, 561, 2466, 2594,
	2804, 1092, 403, 1026, 1143, 2150, 2775, 886,
	1722, 1212, 1874, 1029, 2110, 2935, 885, 2154,
}

// bitReverse7 reverses the lower 7 bits of x.
func bitReverse7(x int) int {
	var r int
	for i := 0; i < 7; i++ {
		r = (r << 1) | (x & 1)
		x >>= 1
	}
	return r
}

// NTT performs the Number Theoretic Transform on polynomial a (mod q).
// Returns the NTT-domain representation.
func NTT(a []int16, q int16) []int16 {
	n := len(a)
	out := make([]int16, n)
	copy(out, a)

	k := 1
	for length := n / 2; length >= 2; length /= 2 {
		for start := 0; start < n; start += 2 * length {
			zeta := kyberZetas[k]
			k++
			for j := start; j < start+length; j++ {
				t := mulModQ(zeta, out[j+length], q)
				out[j+length] = modQ16(out[j]-t, q)
				out[j] = modQ16(out[j]+t, q)
			}
		}
	}
	return out
}

// InverseNTT performs the inverse NTT, converting from NTT domain back
// to coefficient domain.
func InverseNTT(a []int16, q int16) []int16 {
	n := len(a)
	out := make([]int16, n)
	copy(out, a)

	k := 127
	for length := 2; length <= n/2; length *= 2 {
		for start := 0; start < n; start += 2 * length {
			zeta := kyberZetas[k]
			k--
			for j := start; j < start+length; j++ {
				t := out[j]
				out[j] = modQ16(t+out[j+length], q)
				out[j+length] = mulModQ(zeta, modQ16(out[j+length]-t, q), q)
			}
		}
	}

	// Scale by (n/2)^(-1) mod q. The NTT butterfly stops at length=2
	// (not length=1), giving 7 levels for n=256, so we divide by n/2.
	nInv := modInverse(int16(n/2), q)
	for i := range out {
		out[i] = mulModQ(out[i], nInv, q)
	}
	return out
}

// PolyMul performs pointwise multiplication of two NTT-domain polynomials.
func PolyMul(a, b []int16, q int16) []int16 {
	n := len(a)
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = mulModQ(a[i], b[i], q)
	}
	return out
}

// CompressBytes compresses polynomial coefficients to d bits each and
// packs them into bytes.
func CompressBytes(a []int16, d int) []byte {
	n := len(a)
	q := int32(KyberQ)
	totalBits := n * d
	out := make([]byte, (totalBits+7)/8)

	bitPos := 0
	for i := 0; i < n; i++ {
		// Compress: round(2^d / q * x) mod 2^d.
		x := int32(a[i])
		if x < 0 {
			x += q
		}
		compressed := ((x << d) + q/2) / q
		mask := (int32(1) << d) - 1
		compressed &= mask

		// Pack bits.
		for b := 0; b < d; b++ {
			if compressed&(1<<b) != 0 {
				out[bitPos/8] |= 1 << (bitPos % 8)
			}
			bitPos++
		}
	}
	return out
}

// DecompressBytes decompresses d-bit packed values to polynomial coefficients.
func DecompressBytes(data []byte, d, n int) []int16 {
	q := int32(KyberQ)
	out := make([]int16, n)

	bitPos := 0
	for i := 0; i < n; i++ {
		var val int32
		for b := 0; b < d; b++ {
			if bitPos/8 < len(data) && data[bitPos/8]&(1<<(bitPos%8)) != 0 {
				val |= 1 << b
			}
			bitPos++
		}
		// Decompress: round(q / 2^d * x).
		out[i] = int16((val*q + (1 << (d - 1))) >> d)
	}
	return out
}

// modQ16 reduces x modulo q into [0, q).
func modQ16(x, q int16) int16 {
	r := int32(x) % int32(q)
	if r < 0 {
		r += int32(q)
	}
	return int16(r)
}

// mulModQ multiplies a and b modulo q.
func mulModQ(a, b, q int16) int16 {
	r := (int32(a) * int32(b)) % int32(q)
	if r < 0 {
		r += int32(q)
	}
	return int16(r)
}

// modInverse computes the modular inverse of a mod q using the extended
// Euclidean algorithm.
func modInverse(a, q int16) int16 {
	t, newT := int32(0), int32(1)
	r, newR := int32(q), int32(a)
	if newR < 0 {
		newR += int32(q)
	}
	for newR != 0 {
		quotient := r / newR
		t, newT = newT, t-quotient*newT
		r, newR = newR, r-quotient*newR
	}
	if t < 0 {
		t += int32(q)
	}
	return int16(t)
}

// sampleCBD samples a polynomial with coefficients from the Centered Binomial
// Distribution with parameter eta.
func sampleCBD(rng io.Reader, eta, n int) []int16 {
	out := make([]int16, n)
	buf := make([]byte, eta*n/4+1)
	io.ReadFull(rng, buf)

	for i := 0; i < n; i++ {
		var a, b int16
		for j := 0; j < eta; j++ {
			bitIdx := 2*eta*i + j
			byteIdx := bitIdx / 8
			if byteIdx < len(buf) && buf[byteIdx]&(1<<(bitIdx%8)) != 0 {
				a++
			}
		}
		for j := 0; j < eta; j++ {
			bitIdx := 2*eta*i + eta + j
			byteIdx := bitIdx / 8
			if byteIdx < len(buf) && buf[byteIdx]&(1<<(bitIdx%8)) != 0 {
				b++
			}
		}
		out[i] = a - b
	}
	return out
}

// expandMatrix deterministically generates a k x k matrix of polynomials
// from a seed using SHA-256 as XOF.
func expandMatrix(seed []byte, k, n int, q int16) [][][]int16 {
	mat := make([][][]int16, k)
	for i := 0; i < k; i++ {
		mat[i] = make([][]int16, k)
		for j := 0; j < k; j++ {
			mat[i][j] = make([]int16, n)

			// Derive stream from seed || i || j.
			input := make([]byte, len(seed)+2)
			copy(input, seed)
			input[len(seed)] = byte(i)
			input[len(seed)+1] = byte(j)

			// Use repeated SHA-256 as a simple XOF.
			idx := 0
			state := sha256.Sum256(input)
			for idx < n {
				for b := 0; b+1 < len(state) && idx < n; b += 2 {
					val := int16(binary.LittleEndian.Uint16(state[b : b+2]))
					val = modQ16(val, q)
					mat[i][j][idx] = val
					idx++
				}
				if idx < n {
					state = sha256.Sum256(state[:])
				}
			}
		}
	}
	return mat
}

// encodePolynomial encodes polynomial coefficients at the given bit width
// into a packed byte slice.
func encodePolynomial(a []int16, bits int) []byte {
	n := len(a)
	totalBits := n * bits
	out := make([]byte, (totalBits+7)/8)

	bitPos := 0
	for i := 0; i < n; i++ {
		val := uint16(a[i])
		for b := 0; b < bits; b++ {
			if val&(1<<b) != 0 {
				out[bitPos/8] |= 1 << (bitPos % 8)
			}
			bitPos++
		}
	}
	return out
}

// decodePolynomial decodes a packed byte slice into polynomial coefficients
// at the given bit width.
func decodePolynomial(data []byte, bits, n int) []int16 {
	out := make([]int16, n)
	bitPos := 0
	for i := 0; i < n; i++ {
		var val uint16
		for b := 0; b < bits; b++ {
			if bitPos/8 < len(data) && data[bitPos/8]&(1<<(bitPos%8)) != 0 {
				val |= 1 << b
			}
			bitPos++
		}
		out[i] = int16(val)
	}
	return out
}

// decodeMessage converts a message byte array into a polynomial where each
// bit maps to a coefficient of q/2 (for Kyber message encoding).
func decodeMessage(msg []byte, n int, q int16) []int16 {
	out := make([]int16, n)
	half := q / 2
	for i := 0; i < n && i/8 < len(msg); i++ {
		if msg[i/8]&(1<<(i%8)) != 0 {
			out[i] = half
		}
	}
	return out
}

// encodeMessage recovers a message from polynomial coefficients by comparing
// each coefficient to q/2 (threshold decoding).
func encodeMessage(a []int16, n int, q int16) []byte {
	out := make([]byte, (n+7)/8)
	half := int32(q) / 2
	qInt := int32(q)

	for i := 0; i < n; i++ {
		// The coefficient closest to q/2 means bit 1, closest to 0 means bit 0.
		x := int32(a[i])
		if x < 0 {
			x += qInt
		}
		// Check if x is closer to q/2 than to 0 or q.
		dist0 := x
		if dist0 > qInt-dist0 {
			dist0 = qInt - dist0
		}
		distHalf := x - half
		if distHalf < 0 {
			distHalf = -distHalf
		}
		if distHalf < dist0 {
			out[i/8] |= 1 << (i % 8)
		}
	}
	return out
}
