package crypto

import (
	"crypto/elliptic"
	"errors"
	"math/big"
	"sync"
)

// secp256k1 curve parameters from SEC 2: https://www.secg.org/sec2-v2.pdf

var initonce sync.Once
var secp256k1Instance *secp256k1Curve

func initSecp256k1() {
	p, _ := new(big.Int).SetString("fffffffffffffffffffffffffffffffffffffffffffffffffffffffefffffc2f", 16)
	n, _ := new(big.Int).SetString("fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16)
	gx, _ := new(big.Int).SetString("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798", 16)
	gy, _ := new(big.Int).SetString("483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8", 16)

	secp256k1Instance = &secp256k1Curve{
		p:  p,
		n:  n,
		b:  big.NewInt(7),
		gx: gx,
		gy: gy,
		params: &elliptic.CurveParams{
			P:       p,
			N:       n,
			B:       big.NewInt(7),
			Gx:      gx,
			Gy:      gy,
			BitSize: 256,
			Name:    "secp256k1",
		},
	}
}

// secp256k1Curve implements elliptic.Curve for the secp256k1 curve.
type secp256k1Curve struct {
	p, n, b *big.Int
	gx, gy  *big.Int
	params  *elliptic.CurveParams
}

// S256 returns the secp256k1 elliptic curve.
func S256() elliptic.Curve {
	initonce.Do(initSecp256k1)
	return secp256k1Instance
}

func (c *secp256k1Curve) Params() *elliptic.CurveParams {
	return c.params
}

// IsOnCurve checks if (x, y) satisfies y^2 = x^3 + 7 (mod p).
func (c *secp256k1Curve) IsOnCurve(x, y *big.Int) bool {
	if x == nil || y == nil {
		return false
	}
	if x.Sign() < 0 || y.Sign() < 0 {
		return false
	}
	if x.Cmp(c.p) >= 0 || y.Cmp(c.p) >= 0 {
		return false
	}
	// y^2 mod p
	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, c.p)

	// x^3 + 7 mod p
	x3 := new(big.Int).Mul(x, x)
	x3.Mod(x3, c.p)
	x3.Mul(x3, x)
	x3.Mod(x3, c.p)
	x3.Add(x3, c.b)
	x3.Mod(x3, c.p)

	return y2.Cmp(x3) == 0
}

// Add returns the sum of (x1,y1) and (x2,y2) on the curve.
func (c *secp256k1Curve) Add(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) {
	// Handle point at infinity.
	if x1.Sign() == 0 && y1.Sign() == 0 {
		return new(big.Int).Set(x2), new(big.Int).Set(y2)
	}
	if x2.Sign() == 0 && y2.Sign() == 0 {
		return new(big.Int).Set(x1), new(big.Int).Set(y1)
	}

	// If points are the same, use Double.
	if x1.Cmp(x2) == 0 && y1.Cmp(y2) == 0 {
		return c.Double(x1, y1)
	}

	// If x1 == x2 but y1 != y2, result is point at infinity.
	if x1.Cmp(x2) == 0 {
		return new(big.Int), new(big.Int)
	}

	// slope = (y2 - y1) / (x2 - x1) mod p
	dy := new(big.Int).Sub(y2, y1)
	dy.Mod(dy, c.p)
	dx := new(big.Int).Sub(x2, x1)
	dx.Mod(dx, c.p)
	dxInv := new(big.Int).ModInverse(dx, c.p)
	if dxInv == nil {
		return new(big.Int), new(big.Int)
	}
	slope := new(big.Int).Mul(dy, dxInv)
	slope.Mod(slope, c.p)

	// x3 = slope^2 - x1 - x2 mod p
	x3 := new(big.Int).Mul(slope, slope)
	x3.Sub(x3, x1)
	x3.Sub(x3, x2)
	x3.Mod(x3, c.p)

	// y3 = slope*(x1 - x3) - y1 mod p
	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(y3, slope)
	y3.Sub(y3, y1)
	y3.Mod(y3, c.p)

	return x3, y3
}

// Double returns 2*(x,y) on the curve.
func (c *secp256k1Curve) Double(x1, y1 *big.Int) (*big.Int, *big.Int) {
	if y1.Sign() == 0 {
		return new(big.Int), new(big.Int)
	}

	// slope = (3*x1^2 + a) / (2*y1) mod p
	// For secp256k1, a = 0, so slope = 3*x1^2 / (2*y1)
	x1sq := new(big.Int).Mul(x1, x1)
	x1sq.Mod(x1sq, c.p)
	num := new(big.Int).Mul(big.NewInt(3), x1sq)
	num.Mod(num, c.p)

	den := new(big.Int).Mul(big.NewInt(2), y1)
	den.Mod(den, c.p)
	denInv := new(big.Int).ModInverse(den, c.p)
	if denInv == nil {
		return new(big.Int), new(big.Int)
	}
	slope := new(big.Int).Mul(num, denInv)
	slope.Mod(slope, c.p)

	// x3 = slope^2 - 2*x1 mod p
	x3 := new(big.Int).Mul(slope, slope)
	x3.Sub(x3, new(big.Int).Mul(big.NewInt(2), x1))
	x3.Mod(x3, c.p)

	// y3 = slope*(x1 - x3) - y1 mod p
	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(y3, slope)
	y3.Sub(y3, y1)
	y3.Mod(y3, c.p)

	return x3, y3
}

// ScalarMult returns k*(x,y) using double-and-add.
func (c *secp256k1Curve) ScalarMult(bx, by *big.Int, k []byte) (*big.Int, *big.Int) {
	// Convert k to big.Int and reduce modulo N.
	scalar := new(big.Int).SetBytes(k)
	scalar.Mod(scalar, c.n)

	if scalar.Sign() == 0 {
		return new(big.Int), new(big.Int)
	}

	rx, ry := new(big.Int), new(big.Int) // point at infinity
	px, py := new(big.Int).Set(bx), new(big.Int).Set(by)

	for i := scalar.BitLen() - 1; i >= 0; i-- {
		rx, ry = c.Double(rx, ry)
		if scalar.Bit(i) == 1 {
			rx, ry = c.Add(rx, ry, px, py)
		}
	}

	return rx, ry
}

// ScalarBaseMult returns k*G where G is the base point.
func (c *secp256k1Curve) ScalarBaseMult(k []byte) (*big.Int, *big.Int) {
	return c.ScalarMult(c.gx, c.gy, k)
}

// recoverPublicKey recovers the public key from a hash and signature (r, s, v).
// v is the recovery ID (0 or 1).
func recoverPublicKey(hash []byte, r, s *big.Int, v byte) (*big.Int, *big.Int, error) {
	curve := S256().(*secp256k1Curve)

	// Step 1: x = r (for v < 2; for v >= 2, x = r + N, but that's extremely rare).
	x := new(big.Int).Set(r)
	if x.Cmp(curve.p) >= 0 {
		return nil, nil, errInvalidRecoveryID
	}

	// Step 2: Compute y from x using the curve equation y^2 = x^3 + 7 (mod p).
	y := computeY(x, curve.p)
	if y == nil {
		return nil, nil, errInvalidSignature
	}

	// Choose the correct y based on parity.
	if y.Bit(0) != uint(v&1) {
		y.Sub(curve.p, y)
	}

	// Step 3: Verify the point is on the curve.
	if !curve.IsOnCurve(x, y) {
		return nil, nil, errInvalidSignature
	}

	// Step 4: Recover the public key.
	// Q = r^{-1} * (s*R - e*G)
	rInv := new(big.Int).ModInverse(r, curve.n)
	if rInv == nil {
		return nil, nil, errInvalidSignature
	}

	// e = hash as big.Int
	e := new(big.Int).SetBytes(hash)

	// s*R
	sRx, sRy := curve.ScalarMult(x, y, s.Bytes())

	// e*G
	eGx, eGy := curve.ScalarBaseMult(e.Bytes())

	// -e*G (negate y coordinate)
	negEGy := new(big.Int).Sub(curve.p, eGy)

	// s*R - e*G
	diffX, diffY := curve.Add(sRx, sRy, eGx, negEGy)

	// Q = r^{-1} * (s*R - e*G)
	qx, qy := curve.ScalarMult(diffX, diffY, rInv.Bytes())

	if qx.Sign() == 0 && qy.Sign() == 0 {
		return nil, nil, errInvalidSignature
	}

	return qx, qy, nil
}

// computeY computes y = sqrt(x^3 + 7) mod p.
// For secp256k1, p ≡ 3 (mod 4), so sqrt(a) = a^((p+1)/4) mod p.
func computeY(x, p *big.Int) *big.Int {
	// y^2 = x^3 + 7
	x3 := new(big.Int).Mul(x, x)
	x3.Mod(x3, p)
	x3.Mul(x3, x)
	x3.Mod(x3, p)
	x3.Add(x3, big.NewInt(7))
	x3.Mod(x3, p)

	// y = (x^3 + 7)^((p+1)/4) mod p
	exp := new(big.Int).Add(p, big.NewInt(1))
	exp.Rsh(exp, 2) // (p+1)/4
	y := new(big.Int).Exp(x3, exp, p)

	// Verify: y^2 mod p == x^3 + 7 mod p
	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, p)
	if y2.Cmp(x3) != 0 {
		return nil // no square root exists
	}
	return y
}

var (
	errInvalidSignature  = errors.New("invalid signature")
	errInvalidRecoveryID = errors.New("invalid recovery ID")
)
