// ML-DSA-65 lattice-based signing (FIPS 204) with Fiat-Shamir heuristic.
// Parameters: k=6, l=5, q=8380417, N=64 (reduced for schoolbook mul).
package pqc

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"golang.org/x/crypto/sha3"
)

const (
	mldsaN      = 64
	mldsaQ      = 8380417
	mldsaK      = 6
	mldsaL      = 5
	mldsaEta    = 4
	mldsaGamma1 = 524288
	mldsaGamma2 = 261888
	mldsaBeta   = 196
	mldsaTau    = 12
	mldsaD      = 13
	mldsaSeedSz = 32

	MLDSAPublicKeySize  = 1568 // rho(32) + k*N*4
	MLDSAPrivateKeySize = 4480 // rho(32)+K(32)+tr(64)+s1(1280)+s2(1536)+t0(1536)
	MLDSASignatureSize  = 1376 // cTilde(32) + z(l*N*4=1280) + hint(64)
)

var (
	ErrMLDSANilKey     = errors.New("mldsa: nil key")
	ErrMLDSAEmptyMsg   = errors.New("mldsa: empty message")
	ErrMLDSABadSig     = errors.New("mldsa: malformed signature")
	ErrMLDSABadPK      = errors.New("mldsa: invalid public key size")
	ErrMLDSABadSK      = errors.New("mldsa: invalid secret key size")
	ErrMLDSARejection  = errors.New("mldsa: rejection sampling exceeded iterations")
	ErrMLDSAVerifyFail = errors.New("mldsa: verification failed")
)

type mldsaPoly [mldsaN]int64

// MLDSAKeyPair holds an ML-DSA-65 key pair (serialized + structured).
type MLDSAKeyPair struct {
	PublicKey []byte
	SecretKey []byte
	rho       []byte
	kSeed     []byte
	tr        []byte
	s1        []mldsaPoly
	s2        []mldsaPoly
	t0        []mldsaPoly
	t1        []mldsaPoly
	aMatrix   [][]mldsaPoly
}

// MLDSASigner implements lattice-based signing per FIPS 204.
type MLDSASigner struct{}

func NewMLDSASigner() *MLDSASigner          { return &MLDSASigner{} }
func (s *MLDSASigner) Algorithm() PQAlgorithm { return DILITHIUM3 }

func (s *MLDSASigner) GenerateKey() (*MLDSAKeyPair, error) {
	xi := make([]byte, mldsaSeedSz)
	if _, err := rand.Read(xi); err != nil {
		return nil, err
	}

	expanded := mldsaSHAKE256(xi, 128)
	rho, rhoPrime, kSeed := expanded[:32], expanded[32:96], expanded[96:128]
	aMatrix := mldsaExpandA(rho)
	s1 := make([]mldsaPoly, mldsaL)
	for j := 0; j < mldsaL; j++ { s1[j] = mldsaSampleCBD(rhoPrime, uint16(j)) }
	s2 := make([]mldsaPoly, mldsaK)
	for i := 0; i < mldsaK; i++ { s2[i] = mldsaSampleCBD(rhoPrime, uint16(mldsaL+i)) }
	t := mldsaMatVecMul(aMatrix, s1)
	for i := 0; i < mldsaK; i++ { t[i] = mldsaPolyAdd(t[i], s2[i]) }
	t1 := make([]mldsaPoly, mldsaK)
	t0 := make([]mldsaPoly, mldsaK)
	for i := 0; i < mldsaK; i++ { t1[i], t0[i] = mldsaPower2Round(t[i]) }
	pk := mldsaSerializePK(rho, t1)
	tr := mldsaSHAKE256(pk, 64)
	sk := mldsaSerializeSK(rho, kSeed, tr, s1, s2, t0)

	return &MLDSAKeyPair{pk, sk, copySlice(rho), copySlice(kSeed), tr, s1, s2, t0, t1, aMatrix}, nil
}

// Sign produces an ML-DSA-65 signature using Fiat-Shamir with abort.
func (s *MLDSASigner) Sign(key *MLDSAKeyPair, msg []byte) ([]byte, error) {
	if key == nil { return nil, ErrMLDSANilKey }
	if len(msg) == 0 { return nil, ErrMLDSAEmptyMsg }
	mu := mldsaSHAKE256(append(key.tr, msg...), 64)
	rhoPrime := mldsaSHAKE256(append(key.kSeed, mu...), 64)
	for kappa := 0; kappa < 512; kappa++ {
		y := make([]mldsaPoly, mldsaL)
		for j := 0; j < mldsaL; j++ { y[j] = mldsaSampleGamma1(rhoPrime, uint16(kappa*mldsaL+j)) }
		w := mldsaMatVecMul(key.aMatrix, y)
		w1 := make([]mldsaPoly, mldsaK)
		for i := 0; i < mldsaK; i++ { w1[i] = mldsaHighBits(w[i]) }
		cInput := append([]byte{}, mu...)
		for i := 0; i < mldsaK; i++ { cInput = append(cInput, mldsaPackPoly(w1[i])...) }
		cTilde := mldsaSHAKE256(cInput, 32)
		c := mldsaSampleInBall(cTilde)
		z := make([]mldsaPoly, mldsaL)
		reject := false
		for j := 0; j < mldsaL; j++ {
			z[j] = mldsaPolyAdd(y[j], mldsaPolyMul(c, key.s1[j]))
			if !mldsaCheckNorm(z[j], mldsaGamma1-mldsaBeta) { reject = true; break }
		}
		if reject { continue }
		lowReject := false
		for i := 0; i < mldsaK; i++ {
			r0 := mldsaLowBits(mldsaPolySub(w[i], mldsaPolyMul(c, key.s2[i])))
			if !mldsaCheckNorm(r0, mldsaGamma2-mldsaBeta) { lowReject = true; break }
		}
		if lowReject { continue }
		// Verify verifier will reconstruct the same w1: w'=A*z-c*t1*2^d.
		az := mldsaMatVecMul(key.aMatrix, z)
		highMatch := true
		for i := 0; i < mldsaK && highMatch; i++ {
			w1Pi := mldsaHighBits(mldsaPolySub(az[i], mldsaPolyMul(c, mldsaPolyShiftLeft(key.t1[i], mldsaD))))
			for k := 0; k < mldsaN; k++ { if w1[i][k] != w1Pi[k] { highMatch = false; break } }
		}
		if !highMatch { continue }
		sig := make([]byte, 0, MLDSASignatureSize)
		sig = append(sig, cTilde...)
		for j := 0; j < mldsaL; j++ { sig = append(sig, mldsaPackPoly(z[j])...) }
		for len(sig) < MLDSASignatureSize { sig = append(sig, 0) }
		return sig[:MLDSASignatureSize], nil
	}
	return nil, ErrMLDSARejection
}

// Verify checks an ML-DSA-65 signature against a public key and message.
func (s *MLDSASigner) Verify(pk, msg, sig []byte) bool {
	if len(pk) < MLDSAPublicKeySize || len(sig) < MLDSASignatureSize || len(msg) == 0 { return false }
	rho, t1 := mldsaDeserializePK(pk)
	if rho == nil { return false }
	tr := mldsaSHAKE256(pk[:MLDSAPublicKeySize], 64)
	mu := mldsaSHAKE256(append(tr, msg...), 64)
	cTilde := sig[:32]
	z := make([]mldsaPoly, mldsaL)
	off, polySize := 32, mldsaN*4
	for j := 0; j < mldsaL; j++ {
		if off+polySize > len(sig) { return false }
		z[j] = mldsaUnpackPoly(sig[off : off+polySize])
		off += polySize
	}
	for j := 0; j < mldsaL; j++ { if !mldsaCheckNorm(z[j], mldsaGamma1-mldsaBeta) { return false } }
	aMatrix := mldsaExpandA(rho)
	c := mldsaSampleInBall(cTilde)
	az := mldsaMatVecMul(aMatrix, z)
	w1Prime := make([]mldsaPoly, mldsaK)
	for i := 0; i < mldsaK; i++ {
		ct1 := mldsaPolyMul(c, mldsaPolyShiftLeft(t1[i], mldsaD))
		w1Prime[i] = mldsaHighBits(mldsaPolySub(az[i], ct1))
	}
	cInput := append([]byte{}, mu...)
	for i := 0; i < mldsaK; i++ { cInput = append(cInput, mldsaPackPoly(w1Prime[i])...) }
	cTildePrime := mldsaSHAKE256(cInput, 32)
	var diff byte
	for i := 0; i < 32; i++ { diff |= cTilde[i] ^ cTildePrime[i] }
	return diff == 0
}

func mldsaSerializePK(rho []byte, t1 []mldsaPoly) []byte {
	pk := make([]byte, 0, MLDSAPublicKeySize)
	pk = append(pk, rho...)
	for i := 0; i < mldsaK; i++ { pk = append(pk, mldsaPackPoly(t1[i])...) }
	for len(pk) < MLDSAPublicKeySize { pk = append(pk, 0) }
	return pk[:MLDSAPublicKeySize]
}

func mldsaDeserializePK(pk []byte) ([]byte, []mldsaPoly) {
	if len(pk) < 32 { return nil, nil }
	t1 := make([]mldsaPoly, mldsaK)
	off, polySize := 32, mldsaN*4
	for i := 0; i < mldsaK; i++ {
		if off+polySize > len(pk) { continue }
		t1[i] = mldsaUnpackPoly(pk[off : off+polySize])
		off += polySize
	}
	return pk[:32], t1
}

func mldsaSerializeSK(rho, kSeed, tr []byte, s1, s2, t0 []mldsaPoly) []byte {
	sk := make([]byte, 0, MLDSAPrivateKeySize)
	sk = append(sk, rho...)
	sk = append(sk, kSeed...)
	sk = append(sk, tr...)
	for j := 0; j < mldsaL; j++ { sk = append(sk, mldsaPackPoly(s1[j])...) }
	for i := 0; i < mldsaK; i++ { sk = append(sk, mldsaPackPoly(s2[i])...) }
	for i := 0; i < mldsaK; i++ { sk = append(sk, mldsaPackPoly(t0[i])...) }
	for len(sk) < MLDSAPrivateKeySize { sk = append(sk, 0) }
	return sk[:MLDSAPrivateKeySize]
}

func mldsaPackPoly(p mldsaPoly) []byte {
	out := make([]byte, mldsaN*4)
	for i := 0; i < mldsaN; i++ { binary.LittleEndian.PutUint32(out[4*i:], uint32(mldsaModQ(p[i]))) }
	return out
}

func mldsaUnpackPoly(data []byte) mldsaPoly {
	var p mldsaPoly
	for i := 0; i < mldsaN && 4*(i+1) <= len(data); i++ { p[i] = int64(binary.LittleEndian.Uint32(data[4*i : 4*i+4])) }
	return p
}

func copySlice(b []byte) []byte { c := make([]byte, len(b)); copy(c, b); return c }

func mldsaModQ(x int64) int64 {
	r := x % mldsaQ
	if r < 0 { r += mldsaQ }
	return r
}

func mldsaPolyAdd(a, b mldsaPoly) mldsaPoly {
	var c mldsaPoly
	for i := 0; i < mldsaN; i++ { c[i] = mldsaModQ(a[i] + b[i]) }
	return c
}

func mldsaPolySub(a, b mldsaPoly) mldsaPoly {
	var c mldsaPoly
	for i := 0; i < mldsaN; i++ { c[i] = mldsaModQ(a[i] - b[i]) }
	return c
}

// mldsaPolyMul: schoolbook polynomial multiplication mod (X^N+1) mod Q.
func mldsaPolyMul(a, b mldsaPoly) mldsaPoly {
	var c mldsaPoly
	for i := 0; i < mldsaN; i++ {
		for j := 0; j < mldsaN; j++ {
			prod := mldsaModQ(a[i] * b[j])
			if idx := i + j; idx < mldsaN {
				c[idx] = mldsaModQ(c[idx] + prod)
			} else {
				c[idx-mldsaN] = mldsaModQ(c[idx-mldsaN] - prod)
			}
		}
	}
	return c
}

func mldsaPolyShiftLeft(a mldsaPoly, d int) mldsaPoly {
	var c mldsaPoly
	shift := int64(1) << uint(d)
	for i := 0; i < mldsaN; i++ { c[i] = mldsaModQ(a[i] * shift) }
	return c
}

func mldsaMatVecMul(a [][]mldsaPoly, s []mldsaPoly) []mldsaPoly {
	t := make([]mldsaPoly, mldsaK)
	for i := 0; i < mldsaK; i++ {
		for j := 0; j < mldsaL; j++ { t[i] = mldsaPolyAdd(t[i], mldsaPolyMul(a[i][j], s[j])) }
	}
	return t
}

func mldsaPower2Round(a mldsaPoly) (mldsaPoly, mldsaPoly) {
	var a1, a0 mldsaPoly
	d2, halfD2 := int64(1)<<uint(mldsaD), int64(1)<<uint(mldsaD-1)
	for i := 0; i < mldsaN; i++ {
		r := mldsaModQ(a[i])
		r0 := r % d2
		if r0 > halfD2 { r0 -= d2 }
		a1[i] = (r - r0) / d2
		a0[i] = r0
	}
	return a1, a0
}

func mldsaHighBits(a mldsaPoly) mldsaPoly {
	var h mldsaPoly
	alpha := int64(2 * mldsaGamma2)
	for i := 0; i < mldsaN; i++ {
		r := mldsaModQ(a[i])
		r0 := r % alpha
		if r0 > alpha/2 { r0 -= alpha }
		if r-r0 == mldsaQ-1 { h[i] = 0 } else { h[i] = (r - r0) / alpha }
	}
	return h
}

func mldsaLowBits(a mldsaPoly) mldsaPoly {
	var lo mldsaPoly
	alpha := int64(2 * mldsaGamma2)
	for i := 0; i < mldsaN; i++ {
		r := mldsaModQ(a[i])
		r0 := r % alpha
		if r0 > alpha/2 { r0 -= alpha }
		if r-r0 == mldsaQ-1 { lo[i] = -1 } else { lo[i] = r0 }
	}
	return lo
}

func mldsaCheckNorm(a mldsaPoly, bound int64) bool {
	for i := 0; i < mldsaN; i++ {
		v := mldsaModQ(a[i])
		if v > mldsaQ/2 { v = mldsaQ - v }
		if v >= bound { return false }
	}
	return true
}

func mldsaSHAKE256(data []byte, outLen int) []byte {
	h := sha3.NewShake256()
	h.Write(data)
	out := make([]byte, outLen)
	h.Read(out)
	return out
}

func mldsaExpandA(rho []byte) [][]mldsaPoly {
	a := make([][]mldsaPoly, mldsaK)
	for i := 0; i < mldsaK; i++ {
		a[i] = make([]mldsaPoly, mldsaL)
		for j := 0; j < mldsaL; j++ {
			seed := make([]byte, len(rho)+2)
			copy(seed, rho)
			seed[len(rho)], seed[len(rho)+1] = byte(j), byte(i)
			a[i][j] = mldsaRejSample(seed)
		}
	}
	return a
}

func mldsaRejSample(seed []byte) mldsaPoly {
	var p mldsaPoly
	h := sha3.NewShake256()
	h.Write(seed)
	buf := make([]byte, 3)
	for idx := 0; idx < mldsaN; {
		h.Read(buf)
		val := int64(buf[0]) | (int64(buf[1]) << 8) | (int64(buf[2]&0x7F) << 16)
		if val < mldsaQ { p[idx] = val; idx++ }
	}
	return p
}

func mldsaSampleCBD(seed []byte, nonce uint16) mldsaPoly {
	var p mldsaPoly
	input := make([]byte, len(seed)+2)
	copy(input, seed)
	binary.LittleEndian.PutUint16(input[len(seed):], nonce)
	stream := mldsaSHAKE256(input, mldsaN*2)
	for i := 0; i < mldsaN; i++ { p[i] = int64(stream[2*i])%int64(2*mldsaEta+1) - mldsaEta }
	return p
}

func mldsaSampleGamma1(seed []byte, nonce uint16) mldsaPoly {
	var p mldsaPoly
	input := make([]byte, len(seed)+2)
	copy(input, seed)
	binary.LittleEndian.PutUint16(input[len(seed):], nonce)
	stream := mldsaSHAKE256(input, mldsaN*4)
	for i := 0; i < mldsaN; i++ {
		val := int64(binary.LittleEndian.Uint32(stream[4*i:4*i+4])) & 0xFFFFF
		if val > 2*mldsaGamma1 { val = 2 * mldsaGamma1 }
		p[i] = mldsaModQ(int64(mldsaGamma1) - val)
	}
	return p
}

func mldsaSampleInBall(seed []byte) mldsaPoly {
	var c mldsaPoly
	h := sha3.NewShake256()
	h.Write(seed)
	signs := make([]byte, 8)
	h.Read(signs)
	signBits := binary.LittleEndian.Uint64(signs)
	posBuf := make([]byte, 1)
	for i := mldsaN - mldsaTau; i < mldsaN; i++ {
		for {
			h.Read(posBuf)
			j := int(posBuf[0]) % (i + 1)
			if j <= i {
				c[i] = c[j]
				if signBits&1 != 0 { c[j] = mldsaQ - 1 } else { c[j] = 1 }
				signBits >>= 1
				break
			}
		}
	}
	return c
}
