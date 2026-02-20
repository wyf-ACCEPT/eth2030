// Enhanced Falcon NTRU lattice-based signature scheme over Z_q[X]/(X^N+1).
// Uses Fiat-Shamir-with-aborts: pick short y, w = h*y, c = H(w||msg),
// z = y + c*f. Verify: w' = h*z - c*g, check H(w'||msg) = c.
package pqc

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"

	"github.com/eth2028/eth2028/crypto"
)

// FalconEnhancedParams holds NTRU lattice parameters.
type FalconEnhancedParams struct {
	N     int     // polynomial degree
	Q     int64   // prime modulus (12289)
	Sigma float64 // masking range parameter
	Beta  float64 // norm bound override (0 = auto)
}

// DefaultFalconEnhancedParams returns Falcon-512-like parameters.
func DefaultFalconEnhancedParams() FalconEnhancedParams {
	return FalconEnhancedParams{N: 64, Q: 12289, Sigma: 165.7}
}

// Falcon1024EnhancedParams returns Falcon-1024-like parameters.
func Falcon1024EnhancedParams() FalconEnhancedParams {
	return FalconEnhancedParams{N: 128, Q: 12289, Sigma: 168.4}
}

// falconBeta returns the infinity-norm bound for z coefficients.
func falconBeta(p FalconEnhancedParams) int64 {
	return p.Q/4 - 64 // gamma - tau*eta
}

// FalconEnhancedKeyPair holds an NTRU lattice key pair.
type FalconEnhancedKeyPair struct {
	PublicKey []byte  // (g || h) serialised
	SecretKey []byte  // f serialised
	F         []int64 // short secret f
	G         []int64 // short g
	H         []int64 // public h = g * f^{-1} mod q
	Params    FalconEnhancedParams
}

// FalconEnhancedSig holds a Falcon signature.
type FalconEnhancedSig struct {
	SigPoly []byte  // response z as int16 LE
	S2      []int64 // z coefficients
	Nonce   []byte  // 40-byte nonce
	Salt    []byte  // 32-byte challenge hash
}

// FalconEnhancedSigner signs and verifies using NTRU lattice trapdoors.
type FalconEnhancedSigner struct{ params FalconEnhancedParams }

// NewFalconEnhancedSigner creates a signer.
func NewFalconEnhancedSigner(p FalconEnhancedParams) *FalconEnhancedSigner {
	return &FalconEnhancedSigner{params: p}
}

// Errors for the enhanced Falcon scheme.
var (
	ErrFalconEnhancedNilKey       = errors.New("falcon-enhanced: nil key pair")
	ErrFalconEnhancedEmptyMsg     = errors.New("falcon-enhanced: empty message")
	ErrFalconEnhancedBadSig       = errors.New("falcon-enhanced: malformed signature")
	ErrFalconEnhancedNormTooLarge = errors.New("falcon-enhanced: norm exceeds bound")
	ErrFalconEnhancedInvertFail   = errors.New("falcon-enhanced: not invertible")
	ErrFalconEnhancedRejection    = errors.New("falcon-enhanced: rejection limit")
)

// --- Ring arithmetic mod (X^N+1) mod q ---

func fMod(x, q int64) int64 {
	r := x % q
	if r < 0 {
		r += q
	}
	return r
}

func fCenter(x, q int64) int64 {
	r := fMod(x, q)
	if r > q/2 {
		r -= q
	}
	return r
}

func fRingMul(a, b []int64, n int, q int64) []int64 {
	c := make([]int64, n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			idx := i + j
			p := fMod(a[i]*b[j], q)
			if idx < n {
				c[idx] = fMod(c[idx]+p, q)
			} else {
				c[idx-n] = fMod(c[idx-n]-p, q)
			}
		}
	}
	return c
}

func fRingSub(a, b []int64, q int64) []int64 {
	c := make([]int64, len(a))
	for i := range a {
		c[i] = fMod(a[i]-b[i], q)
	}
	return c
}

func fRingAdd(a, b []int64, q int64) []int64 {
	c := make([]int64, len(a))
	for i := range a {
		c[i] = fMod(a[i]+b[i], q)
	}
	return c
}

// --- Modular inverse and polynomial inversion ---

func fModInverse(a, m int64) (int64, bool) {
	if m <= 1 {
		return 0, m == 1
	}
	g, x, _ := fEGCD(a%m, m)
	if g != 1 {
		return 0, false
	}
	return ((x % m) + m) % m, true
}

func fEGCD(a, b int64) (int64, int64, int64) {
	if a < 0 {
		a = -a
	}
	if a == 0 {
		return b, 0, 1
	}
	g, x, y := fEGCD(b%a, a)
	return g, y - (b/a)*x, x
}

// fRingInvert inverts f mod (X^N+1) mod q via negacyclic matrix Gaussian elim.
func fRingInvert(f []int64, n int, q int64) ([]int64, error) {
	mat := make([][]int64, n)
	for i := 0; i < n; i++ {
		mat[i] = make([]int64, 2*n)
		for j := 0; j < n; j++ {
			if d := i - j; d >= 0 {
				mat[i][j] = fMod(f[d], q)
			} else {
				mat[i][j] = fMod(-f[d+n], q)
			}
		}
		mat[i][n+i] = 1
	}
	for col := 0; col < n; col++ {
		piv := -1
		for r := col; r < n; r++ {
			if mat[r][col] != 0 {
				piv = r
				break
			}
		}
		if piv == -1 {
			return nil, ErrFalconEnhancedInvertFail
		}
		mat[col], mat[piv] = mat[piv], mat[col]
		inv, ok := fModInverse(mat[col][col], q)
		if !ok {
			return nil, ErrFalconEnhancedInvertFail
		}
		for j := range mat[col] {
			mat[col][j] = fMod(mat[col][j]*inv, q)
		}
		for r := 0; r < n; r++ {
			if r == col || mat[r][col] == 0 {
				continue
			}
			fac := mat[r][col]
			for j := range mat[r] {
				mat[r][j] = fMod(mat[r][j]-fac*mat[col][j], q)
			}
		}
	}
	res := make([]int64, n)
	for i := range res {
		res[i] = mat[i][n]
	}
	return res, nil
}

// --- Sampling and hashing ---

func fSampleSmall(seed []byte, idx, n, bound int) []int64 {
	p := make([]int64, n)
	tag := make([]byte, len(seed)+3)
	copy(tag, seed)
	tag[len(seed)] = byte(idx >> 8)
	tag[len(seed)+1] = byte(idx)
	tag[len(seed)+2] = 0xFE
	for i := 0; i < n; i += 8 {
		h := crypto.Keccak256(tag, []byte{byte(i >> 8), byte(i)})
		for j := 0; j < 8 && i+j < n; j++ {
			v := int64(h[j]) % int64(2*bound+1)
			p[i+j] = v - int64(bound)
		}
	}
	return p
}

func fSampleMask(n int, gamma int64) ([]int64, error) {
	p := make([]int64, n)
	buf := make([]byte, n*4)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	span := 2 * gamma
	for i := 0; i < n; i++ {
		v := int64(binary.LittleEndian.Uint32(buf[i*4:])) % span
		p[i] = v - gamma
	}
	return p, nil
}

func fChallengeHash(w []int64, q int64, msg []byte) []byte {
	wb := make([]byte, len(w)*2)
	for i, c := range w {
		binary.LittleEndian.PutUint16(wb[i*2:], uint16(fMod(c, q)))
	}
	return crypto.Keccak256(wb, msg)
}

func fExpandChallenge(hash []byte, n, tau int) []int64 {
	c := make([]int64, n)
	for i := 0; i < tau && i < n; i++ {
		pos := int(hash[i%32]) % n
		if hash[(i+16)%32]&(1<<uint(i%8)) == 0 {
			c[pos] = 1
		} else {
			c[pos] = -1
		}
	}
	return c
}

// --- Key generation ---

func (sn *FalconEnhancedSigner) GenerateKey() (*FalconEnhancedKeyPair, error) {
	pr := sn.params
	n, q := pr.N, pr.Q
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	var f, g []int64
	var fInv []int64
	for att := 0; att < 100; att++ {
		as := crypto.Keccak256(seed, []byte{byte(att)})
		f = fSampleSmall(as, 0, n, 2)
		g = fSampleSmall(as, 1, n, 2)
		if f[0]%2 == 0 {
			f[0]++
		}
		var err error
		fInv, err = fRingInvert(f, n, q)
		if err != nil {
			continue
		}
		chk := fRingMul(f, fInv, n, q)
		if chk[0] != 1 {
			fInv = nil
			continue
		}
		ok := true
		for i := 1; i < n; i++ {
			if chk[i] != 0 {
				ok = false
				break
			}
		}
		if ok {
			break
		}
		fInv = nil
	}
	if fInv == nil {
		return nil, ErrFalconEnhancedInvertFail
	}
	h := fRingMul(g, fInv, n, q)

	// Public key: g (N*2) || h (N*2).
	pk := make([]byte, 2*n*2)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(pk[i*2:], uint16(int16(g[i])))
	}
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(pk[n*2+i*2:], uint16(h[i]))
	}
	sk := make([]byte, n*2)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(sk[i*2:], uint16(int16(f[i])))
	}
	return &FalconEnhancedKeyPair{
		PublicKey: pk, SecretKey: sk,
		F: f, G: g, H: h, Params: pr,
	}, nil
}

// --- Signing (Fiat-Shamir with aborts) ---

func (sn *FalconEnhancedSigner) Sign(
	key *FalconEnhancedKeyPair, msg []byte,
) (*FalconEnhancedSig, error) {
	if key == nil {
		return nil, ErrFalconEnhancedNilKey
	}
	if len(msg) == 0 {
		return nil, ErrFalconEnhancedEmptyMsg
	}
	pr := key.Params
	n, q := pr.N, pr.Q
	gamma := q / 4
	beta := falconBeta(pr)

	for att := 0; att < 512; att++ {
		y, err := fSampleMask(n, gamma)
		if err != nil {
			return nil, err
		}
		yq := make([]int64, n)
		for i, v := range y {
			yq[i] = fMod(v, q)
		}
		w := fRingMul(key.H, yq, n, q)
		cHash := fChallengeHash(w, q, msg)
		cPoly := fExpandChallenge(cHash, n, 32)

		cf := fRingMul(cPoly, key.F, n, q)
		z := make([]int64, n)
		reject := false
		for i := 0; i < n; i++ {
			z[i] = y[i] + fCenter(cf[i], q)
			if z[i] > beta || z[i] < -beta {
				reject = true
				break
			}
		}
		if reject {
			continue
		}

		sp := make([]byte, n*2)
		for i := 0; i < n; i++ {
			binary.LittleEndian.PutUint16(sp[i*2:], uint16(int16(z[i])))
		}
		nonce := make([]byte, 40)
		rand.Read(nonce)

		return &FalconEnhancedSig{
			SigPoly: sp, S2: z, Nonce: nonce, Salt: cHash,
		}, nil
	}
	return nil, ErrFalconEnhancedRejection
}

// --- Verification ---

func (sn *FalconEnhancedSigner) Verify(
	pk, msg []byte, sig *FalconEnhancedSig,
) (bool, error) {
	if sig == nil {
		return false, ErrFalconEnhancedBadSig
	}
	if len(msg) == 0 {
		return false, ErrFalconEnhancedEmptyMsg
	}
	pr := sn.params
	n, q := pr.N, pr.Q
	beta := falconBeta(pr)

	if len(pk) != 2*n*2 || len(sig.SigPoly) != n*2 {
		return false, ErrFalconEnhancedBadSig
	}
	if len(sig.Salt) != 32 {
		return false, ErrFalconEnhancedBadSig
	}

	g := make([]int64, n)
	for i := 0; i < n; i++ {
		g[i] = int64(int16(binary.LittleEndian.Uint16(pk[i*2:])))
	}
	h := make([]int64, n)
	for i := 0; i < n; i++ {
		h[i] = int64(binary.LittleEndian.Uint16(pk[n*2+i*2:]))
	}
	z := make([]int64, n)
	for i := 0; i < n; i++ {
		z[i] = int64(int16(binary.LittleEndian.Uint16(sig.SigPoly[i*2:])))
	}

	for _, zi := range z {
		if zi > beta || zi < -beta {
			return false, nil
		}
	}

	cHash := sig.Salt
	cPoly := fExpandChallenge(cHash, n, 32)

	zq := make([]int64, n)
	for i, v := range z {
		zq[i] = fMod(v, q)
	}
	hz := fRingMul(h, zq, n, q)
	gq := make([]int64, n)
	for i, v := range g {
		gq[i] = fMod(v, q)
	}
	cg := fRingMul(cPoly, gq, n, q)
	wPrime := fRingSub(hz, cg, q)

	if !bytes.Equal(fChallengeHash(wPrime, q, msg), cHash) {
		return false, nil
	}
	return true, nil
}

// --- Size helpers ---

// FalconEnhancedKeySize returns the public key size (g+h).
func FalconEnhancedKeySize(p FalconEnhancedParams) int { return 2 * p.N * 2 }

// FalconEnhancedSigSize returns the approximate signature size.
func FalconEnhancedSigSize(p FalconEnhancedParams) int { return p.N*2 + 40 + 32 }

// fNormSqCentered computes the squared L2 norm (centered mod q).
func fNormSqCentered(a []int64, q int64) float64 {
	var s float64
	for _, c := range a {
		v := float64(fCenter(c, q))
		s += v * v
	}
	return s
}
