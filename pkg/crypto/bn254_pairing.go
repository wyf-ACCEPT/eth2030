package crypto

// BN254 optimal Ate pairing implementation.
//
// Uses the tower: F_p^12 = F_p^6[w]/(w^2-v), F_p^6 = F_p^2[v]/(v^3-xi),
// F_p^2 = F_p[i]/(i^2+1), with xi = 9+i.
//
// The D-type sextic twist maps (x', y') in E'(F_p^2) to
// (x'*w^2, y'*w^3) in E(F_p^12).
//
// The implementation follows the structure of the cloudflare/bn256 library
// used by go-ethereum, adapted for our tower representation.

import "math/big"

// bn254U is the BN parameter u such that p = 36u^4 + 36u^3 + 24u^2 + 6u + 1
// and the ate loop count = |6u+2| = 29793968203157093288.
var bn254U, _ = new(big.Int).SetString("4965661367192848881", 10)

// sixuPlus2NAF is 6u+2 in non-adjacent form, LSB first.
var sixuPlus2NAF = []int8{0, 0, 0, 1, 0, 1, 0, -1, 0, 0, 1, -1, 0, 0, 1, 0,
	0, 1, 1, 0, -1, 0, 0, 1, 0, -1, 0, 0, 0, 0, 1, 1,
	1, 0, 0, -1, 0, 0, 1, 0, 0, 0, 0, 0, -1, 0, 0, 1,
	1, 0, 0, -1, 0, 0, 0, 1, 1, 0, -1, 0, 0, 1, 0, 1, 1}

// BN254Pair computes the optimal Ate pairing e(P, Q).
func BN254Pair(p *G1Point, q *G2Point) *fp12 {
	if p.g1IsInfinity() || q.g2IsInfinity() {
		return fp12One()
	}
	px, py := p.g1ToAffine()
	qx, qy := q.g2ToAffine()
	f := millerLoop(px, py, qx, qy)
	return finalExp(f)
}

// bn254MultiPairing checks prod e(Pi, Qi) == 1 in G_T.
func bn254MultiPairing(g1Points []*G1Point, g2Points []*G2Point) bool {
	if len(g1Points) != len(g2Points) {
		return false
	}
	f := fp12One()
	for i := range g1Points {
		if g1Points[i].g1IsInfinity() || g2Points[i].g2IsInfinity() {
			continue
		}
		px, py := g1Points[i].g1ToAffine()
		qx, qy := g2Points[i].g2ToAffine()
		ml := millerLoop(px, py, qx, qy)
		f = fp12Mul(f, ml)
	}
	result := finalExp(f)
	return result.isOne()
}

// twist point in Jacobian coordinates for the Miller loop.
type twistPointJ struct {
	x, y, z, t *fp2 // t = z^2
}

// lineFunctionDouble computes the tangent line at R (Jacobian), updates R to 2R,
// and returns the line evaluation coefficients a, b, c for sparse Fp12 multiply.
// The line element in Fp12 is: c + (a*v + b*v^2)*w.
func lineFunctionDouble(r *twistPointJ, qx, qy *big.Int) (a, b, c *fp2, rOut *twistPointJ) {
	// Algorithm from "Faster Computation of the Tate Pairing" for a=0 curves.
	A := fp2Sqr(r.x)
	B := fp2Sqr(r.y)
	C := fp2Sqr(B)

	D := fp2Add(r.x, B)
	D = fp2Sqr(D)
	D = fp2Sub(D, A)
	D = fp2Sub(D, C)
	D = fp2Add(D, D)

	E := fp2Add(fp2Add(A, A), A) // 3A

	G := fp2Sqr(E)

	rOut = &twistPointJ{}
	rOut.x = fp2Sub(fp2Sub(G, D), D)

	rOut.z = fp2Add(r.y, r.z)
	rOut.z = fp2Sqr(rOut.z)
	rOut.z = fp2Sub(rOut.z, B)
	rOut.z = fp2Sub(rOut.z, r.t)

	rOut.y = fp2Sub(D, rOut.x)
	rOut.y = fp2Mul(rOut.y, E)
	t := fp2Add(C, C)
	t = fp2Add(t, t)
	t = fp2Add(t, t)
	rOut.y = fp2Sub(rOut.y, t)

	rOut.t = fp2Sqr(rOut.z)

	// Line coefficients.
	t = fp2Mul(E, r.t)
	t = fp2Add(t, t)
	b = fp2Neg(t)
	b = fp2MulScalar(b, qx) // b = -2*E*r.t * qx

	a = fp2Add(r.x, E)
	a = fp2Sqr(a)
	a = fp2Sub(a, A)
	a = fp2Sub(a, G)
	t = fp2Add(B, B)
	t = fp2Add(t, t)
	a = fp2Sub(a, t) // a = (rx+E)^2 - A - G - 4B

	c = fp2Mul(rOut.z, r.t)
	c = fp2Add(c, c)
	c = fp2MulScalar(c, qy) // c = 2*rOut.z*r.t * qy

	return
}

// lineFunctionAdd computes the line through R and P (affine twist point),
// updates R to R+P, returns line evaluation coefficients.
func lineFunctionAdd(r *twistPointJ, px, py *fp2, qx, qy *big.Int, r2 *fp2) (a, b, c *fp2, rOut *twistPointJ) {
	// Mixed addition algorithm from "Faster Computation of the Tate Pairing".
	B := fp2Mul(px, r.t) // px * r.t

	D := fp2Add(py, r.z)
	D = fp2Sqr(D)
	D = fp2Sub(D, r2)
	D = fp2Sub(D, r.t)
	D = fp2Mul(D, r.t)

	H := fp2Sub(B, r.x)
	I := fp2Sqr(H)

	E := fp2Add(I, I)
	E = fp2Add(E, E) // 4*I

	J := fp2Mul(H, E)

	L1 := fp2Sub(D, r.y)
	L1 = fp2Sub(L1, r.y)

	V := fp2Mul(r.x, E)

	rOut = &twistPointJ{}
	rOut.x = fp2Sub(fp2Sub(fp2Sqr(L1), J), fp2Add(V, V))

	rOut.z = fp2Add(r.z, H)
	rOut.z = fp2Sqr(rOut.z)
	rOut.z = fp2Sub(rOut.z, r.t)
	rOut.z = fp2Sub(rOut.z, I)

	t := fp2Sub(V, rOut.x)
	t = fp2Mul(t, L1)
	t2 := fp2Mul(r.y, J)
	t2 = fp2Add(t2, t2)
	rOut.y = fp2Sub(t, t2)

	rOut.t = fp2Sqr(rOut.z)

	// Line coefficients.
	t = fp2Add(py, rOut.z)
	t = fp2Sqr(t)
	t = fp2Sub(t, r2)
	t = fp2Sub(t, rOut.t)

	t2 = fp2Mul(L1, px)
	t2 = fp2Add(t2, t2)
	a = fp2Sub(t2, t)

	c = fp2MulScalar(rOut.z, qy)
	c = fp2Add(c, c)

	b = fp2Neg(L1)
	b = fp2MulScalar(b, qx)
	b = fp2Add(b, b)

	return
}

// mulLine multiplies ret by the sparse line element c + (a*v + b*v^2)*w.
// This is a specialized Fp12 multiplication that exploits sparsity.
//
// In our tower: Fp12 = c0 + c1*w, Fp6 = c0 + c1*v + c2*v^2.
// The line element has c0 = (c, 0, 0) and c1 = (0, a, b) in Fp6,
// where (c0, c1, c2) means c0 + c1*v + c2*v^2.
//
// Reference mapping from cloudflare/bn256 gfP6{x,y,z} = x*tau^2 + y*tau + z:
//   ref.x = our c2, ref.y = our c1, ref.z = our c0
// So ref's a2 = {x:0, y:a, z:b} = our fp6{c0:b, c1:a, c2:0}.
func mulLine(ret *fp12, a, b, c *fp2) *fp12 {
	// The line's Fp6 representation for the w-coefficient:
	// ref: a2 = {x:0, y:a, z:b} meaning 0*tau^2 + a*tau + b
	// ours: (c0=b, c1=a, c2=0) meaning b + a*v + 0*v^2
	lineC1 := &fp6{c0: b, c1: a, c2: fp2Zero()}

	a2 := fp6Mul(lineC1, ret.c1) // lineC1 * ret.c1
	t3 := fp6MulByFp2(ret.c0, c) // ret.c0 * c (scalar mult of Fp6 by Fp2)

	// Karatsuba: new_c1 = (ret.c1 + ret.c0) * lineSum - a2 - t3
	// where lineSum = lineC0 + lineC1 = (c,0,0) + (b,a,0) = (b+c, a, 0)
	// ref: t2 = {x:0, y:a, z:b+c}
	t := fp2Add(b, c)
	lineSum := &fp6{c0: t, c1: a, c2: fp2Zero()}

	retXplusY := fp6Add(ret.c1, ret.c0)

	newC1 := fp6Mul(retXplusY, lineSum)
	newC1 = fp6Sub(newC1, a2)
	newC1 = fp6Sub(newC1, t3)

	// new_c0 = MulByV(a2) + t3
	// (since w^2 = v, the cross term a2*w^2 = a2*v)
	newC0 := fp6Add(fp6MulByV(a2), t3)

	return &fp12{c0: newC0, c1: newC1}
}

// millerLoop performs the Miller loop for the optimal Ate pairing using
// projective twist point coordinates and NAF representation of 6u+2.
func millerLoop(px, py *big.Int, qx, qy *fp2) *fp12 {
	ret := fp12One()

	// Start with affine twist point as Jacobian (z=1, t=1).
	one := &fp2{a0: new(big.Int).SetInt64(1), a1: new(big.Int)}
	r := &twistPointJ{
		x: newFp2(qx.a0, qx.a1),
		y: newFp2(qy.a0, qy.a1),
		z: newFp2(one.a0, one.a1),
		t: newFp2(one.a0, one.a1),
	}

	// Negative of the affine twist point.
	minusQy := fp2Neg(qy)

	r2 := fp2Sqr(qy) // for line function add

	for i := len(sixuPlus2NAF) - 1; i > 0; i-- {
		a, b, c, newR := lineFunctionDouble(r, px, py)
		if i != len(sixuPlus2NAF)-1 {
			ret = fp12Sqr(ret)
		}
		ret = mulLine(ret, a, b, c)
		r = newR

		switch sixuPlus2NAF[i-1] {
		case 1:
			a, b, c, newR = lineFunctionAdd(r, qx, qy, px, py, r2)
			ret = mulLine(ret, a, b, c)
			r = newR
		case -1:
			a, b, c, newR = lineFunctionAdd(r, qx, minusQy, px, py, r2)
			ret = mulLine(ret, a, b, c)
			r = newR
		}
	}

	// Two extra steps: add Q1 (Frobenius of Q) and -Q2 (neg-Frobenius^2 of Q).
	q1x, q1y := frobeniusEndomorphism(qx, qy)

	r2 = fp2Sqr(q1y)
	a, b, c, newR := lineFunctionAdd(r, q1x, q1y, px, py, r2)
	ret = mulLine(ret, a, b, c)
	r = newR

	// For Q2: x gets multiplied by xiToPSqMinus1Over3, y stays the same.
	// This gives -Q2 (the minus comes from the p^2 Frobenius on y).
	minusQ2x := fp2MulScalar(qx, frobSqXa0) // xiToPSqMinus1Over3 is a scalar in Fp
	minusQ2y := newFp2(qy.a0, qy.a1)         // y unchanged = -Q2's y

	r2 = fp2Sqr(minusQ2y)
	a, b, c, _ = lineFunctionAdd(r, minusQ2x, minusQ2y, px, py, r2)
	ret = mulLine(ret, a, b, c)

	return ret
}

// Frobenius endomorphism constants for G2.
var (
	frobXa0, _ = new(big.Int).SetString("21575463638280843010398324269430826099269044274347216827212613867836435027261", 10)
	frobXa1, _ = new(big.Int).SetString("10307601595873709700152284273816112264069230130616436755625194854815875713954", 10)
	frobYa0, _ = new(big.Int).SetString("2821565182194536844548159561693502659359617185244120367078079554186484126554", 10)
	frobYa1, _ = new(big.Int).SetString("3505843767911556378687030309984248845540243509899259641013678093033130930403", 10)

	xiToPMinus1Over3Twist = &fp2{a0: frobXa0, a1: frobXa1}
	xiToPMinus1Over2Twist = &fp2{a0: frobYa0, a1: frobYa1}
)

func frobeniusEndomorphism(qx, qy *fp2) (*fp2, *fp2) {
	x := fp2Mul(fp2Conj(qx), xiToPMinus1Over3Twist)
	y := fp2Mul(fp2Conj(qy), xiToPMinus1Over2Twist)
	return x, y
}

var (
	frobSqXa0, _ = new(big.Int).SetString("21888242871839275220042445260109153167277707414472061641714758635765020556616", 10)
)

// finalExp computes f^((p^12-1)/n).
func finalExp(f *fp12) *fp12 {
	// Easy part: f^((p^6-1)*(p^2+1))
	fInv := fp12Inv(f)
	f1 := fp12Mul(fp12Conj(f), fInv) // f^(p^6-1)
	f2 := fp12Mul(fp12FrobSq(f1), f1) // f1^(p^2+1)
	return finalExpHard(f2)
}

func finalExpHard(f *fp12) *fp12 {
	fu := fp12Exp(f, bn254U)
	fu2 := fp12Exp(fu, bn254U)
	fu3 := fp12Exp(fu2, bn254U)

	fp1 := fp12Frob(f)
	fp2_ := fp12FrobSq(f)
	fp3 := fp12Frob3(f)

	fup := fp12Frob(fu)
	fu2p := fp12Frob(fu2)
	fu3p := fp12Frob(fu3)
	fu2p2 := fp12FrobSq(fu2)

	y0 := fp12Mul(fp12Mul(fp1, fp2_), fp3)
	y1 := fp12Conj(f)
	y2 := fu2p2
	y3 := fp12Conj(fup)
	y4 := fp12Mul(fp12Conj(fu), fp12Conj(fu2p))
	y5 := fp12Conj(fu2)
	y6 := fp12Conj(fp12Mul(fu3, fu3p))

	t0 := fp12Mul(fp12Mul(fp12Sqr(y6), y4), y5)
	t1 := fp12Mul(fp12Mul(y3, y5), t0)
	t0 = fp12Mul(t0, y2)
	t1 = fp12Mul(fp12Sqr(t1), t0)
	t1 = fp12Sqr(t1)
	t0 = fp12Mul(t1, y1)
	t1 = fp12Mul(t1, y0)
	t0 = fp12Mul(fp12Sqr(t0), t1)

	return t0
}

// fp12Frob computes f^p (Frobenius endomorphism) using precomputed constants.
func fp12Frob(f *fp12) *fp12 { return fp12FrobeniusEfficient(f) }

// fp12FrobSq computes f^(p^2) using precomputed constants.
func fp12FrobSq(f *fp12) *fp12 { return fp12FrobeniusSqEfficient(f) }

// fp12Frob3 computes f^(p^3) using precomputed constants.
func fp12Frob3(f *fp12) *fp12 { return fp12FrobeniusCubeEfficient(f) }

