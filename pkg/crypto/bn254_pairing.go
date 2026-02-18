package crypto

// BN254 optimal Ate pairing implementation.
//
// Uses the tower: F_p^12 = F_p^6[w]/(w^2-v), F_p^6 = F_p^2[v]/(v^3-xi),
// F_p^2 = F_p[i]/(i^2+1), with xi = 9+i.
//
// The D-type sextic twist maps (x', y') in E'(F_p^2) to
// (x'/w^2, y'/w^3) in E(F_p^12).
//
// Line evaluation for the untwisted approach: given a line on the twist curve
// l(x, y) = a*y' - b*x' + c (in F_p^2 coords), the evaluation at a G1 point
// P = (px, py) via the twist becomes sparse in F_p^12:
//   a*py * w^3 - b*px * w^2 + c
// which maps to specific positions in the tower.

import "math/big"

// ateLoopCount is |6u+2| for BN254: 29793968203157093288.
var ateLoopCount, _ = new(big.Int).SetString("29793968203157093288", 10)

// BN parameter u.
var bn254U, _ = new(big.Int).SetString("4965661367071055296", 10)

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

// millerLoop performs the Miller loop for the optimal Ate pairing.
func millerLoop(px, py *big.Int, qx, qy *fp2) *fp12 {
	f := fp12One()

	// R starts at Q (affine).
	rx := newFp2(qx.a0, qx.a1)
	ry := newFp2(qy.a0, qy.a1)

	for i := ateLoopCount.BitLen() - 2; i >= 0; i-- {
		f = fp12Sqr(f)
		f = fp12Mul(f, lineDouble(&rx, &ry, px, py))

		if ateLoopCount.Bit(i) == 1 {
			f = fp12Mul(f, lineAdd(&rx, &ry, qx, qy, px, py))
		}
	}

	// Two extra steps for BN254.
	q1x, q1y := frobeniusEndomorphism(qx, qy)
	f = fp12Mul(f, lineAdd(&rx, &ry, q1x, q1y, px, py))

	q2x, q2y := frobeniusEndomorphismSq(qx, qy)
	q2y = fp2Neg(q2y)
	f = fp12Mul(f, lineAdd(&rx, &ry, q2x, q2y, px, py))

	return f
}

// lineDouble computes the tangent line at R, updates R to 2R, and evaluates at P.
// Returns a sparse F_p^12 element.
//
// The tangent at (rx, ry): y - ry = lambda*(x - rx) where lambda = 3rx^2/(2ry).
// Rearranging: lambda*x - y + (ry - lambda*rx) = 0.
// At the twist unmap, this becomes (in F_p^12):
//   (ry - lambda*rx) + (-lambda*px)*w^2 + (py)*w^3
func lineDouble(rx, ry **fp2, px, py *big.Int) *fp12 {
	rxSq := fp2Sqr(*rx)
	num := fp2Add(fp2Add(rxSq, rxSq), rxSq) // 3rx^2
	den := fp2Add(*ry, *ry)                   // 2ry

	if den.isZero() {
		*rx = fp2Zero()
		*ry = fp2Zero()
		return fp12One()
	}

	lambda := fp2Mul(num, fp2Inv(den))

	// Compute new R = 2R.
	newx := fp2Sub(fp2Sub(fp2Sqr(lambda), *rx), *rx)
	newy := fp2Sub(fp2Mul(lambda, fp2Sub(*rx, newx)), *ry)

	// Line eval at P.
	pxf := &fp2{a0: new(big.Int).Set(px), a1: new(big.Int)}
	pyf := &fp2{a0: new(big.Int).Set(py), a1: new(big.Int)}

	a := fp2Sub(*ry, fp2Mul(lambda, *rx)) // ry - lambda*rx (constant term)
	b := fp2Mul(fp2Neg(lambda), pxf)      // -lambda*px (w^2 coefficient)
	c := pyf                               // py (w^3 coefficient)

	*rx = newx
	*ry = newy

	return sparseLineToFp12(a, b, c)
}

// lineAdd computes the chord through R and Q, updates R to R+Q, evals at P.
func lineAdd(rx, ry **fp2, qx, qy *fp2, px, py *big.Int) *fp12 {
	if (*rx).equal(qx) {
		if (*ry).equal(qy) {
			return lineDouble(rx, ry, px, py)
		}
		*rx = fp2Zero()
		*ry = fp2Zero()
		return fp12One()
	}

	lambda := fp2Mul(fp2Sub(qy, *ry), fp2Inv(fp2Sub(qx, *rx)))

	newx := fp2Sub(fp2Sub(fp2Sqr(lambda), *rx), qx)
	newy := fp2Sub(fp2Mul(lambda, fp2Sub(*rx, newx)), *ry)

	pxf := &fp2{a0: new(big.Int).Set(px), a1: new(big.Int)}
	pyf := &fp2{a0: new(big.Int).Set(py), a1: new(big.Int)}

	a := fp2Sub(*ry, fp2Mul(lambda, *rx))
	b := fp2Mul(fp2Neg(lambda), pxf)
	c := pyf

	*rx = newx
	*ry = newy

	return sparseLineToFp12(a, b, c)
}

// sparseLineToFp12 embeds the line eval a + b*w^2 + c*w^3 into F_p^12.
// In the tower F_p^12 = F_p^6[w]/(w^2-v):
// - w^2 = v, which in F_p^6 = F_p^2[v]/(v^3-xi) is (0, 1, 0).
// - w^3 = w*v, which is w * (0,1,0) in F_p^12 = c1 of F_p^12 with F_p^6 = (0,1,0).
//
// So the element is:
//   c0 = a + b*v = (a, b, 0) in F_p^6
//   c1 = c*v = (0, c, 0) in F_p^6
func sparseLineToFp12(a, b, c *fp2) *fp12 {
	return &fp12{
		c0: &fp6{c0: a, c1: b, c2: fp2Zero()},
		c1: &fp6{c0: fp2Zero(), c1: c, c2: fp2Zero()},
	}
}

// Frobenius endomorphism constants for G2.
var (
	frobXa0, _ = new(big.Int).SetString("21575463638280843010398324269430826099269044274347216827212613867836435027261", 10)
	frobXa1, _ = new(big.Int).SetString("10307601595873709700078755136146204025218092992518765122318458099831426205727", 10)
	frobYa0, _ = new(big.Int).SetString("2821565182194536844548159561693502659359617185244120367078079554186484126554", 10)
	frobYa1, _ = new(big.Int).SetString("3505843767911556378687030309984248845540243509899259266946897622437848376689", 10)

	xiToPMinus1Over3 = &fp2{a0: frobXa0, a1: frobXa1}
	xiToPMinus1Over2 = &fp2{a0: frobYa0, a1: frobYa1}
)

func frobeniusEndomorphism(qx, qy *fp2) (*fp2, *fp2) {
	x := fp2Mul(fp2Conj(qx), xiToPMinus1Over3)
	y := fp2Mul(fp2Conj(qy), xiToPMinus1Over2)
	return x, y
}

var (
	frobSqXa0, _ = new(big.Int).SetString("21888242871839275220042445260109153167277707414472061641714758635765020556616", 10)
	frobSqYa0, _ = new(big.Int).SetString("21888242871839275222246405745257275088696311157297823662689037894645226208582", 10)

	xiToPSqMinus1Over3 = &fp2{a0: frobSqXa0, a1: new(big.Int)}
	xiToPSqMinus1Over2 = &fp2{a0: frobSqYa0, a1: new(big.Int)}
)

func frobeniusEndomorphismSq(qx, qy *fp2) (*fp2, *fp2) {
	x := fp2Mul(qx, xiToPSqMinus1Over3)
	y := fp2Mul(qy, xiToPSqMinus1Over2)
	return x, y
}

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

	y0 := fp12Mul(fp12Mul(fp1, fp12Sqr(fp2_)), fp3)
	y1 := fp12Conj(f)
	y2 := fu2p2
	y3 := fp12Conj(fup)
	y4 := fp12Mul(fp12Conj(fu), fp12Conj(fu2p))
	y5 := fp12Conj(fu2)
	y6 := fp12Mul(fu3, fu3p)

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

// fp12Frob computes f^p using the efficient tower-based Frobenius map.
func fp12Frob(f *fp12) *fp12 { return fp12FrobeniusEfficient(f) }

// fp12FrobSq computes f^(p^2) using the efficient tower-based Frobenius map.
func fp12FrobSq(f *fp12) *fp12 { return fp12FrobeniusSqEfficient(f) }

// fp12Frob3 computes f^(p^3) using the efficient tower-based Frobenius map.
func fp12Frob3(f *fp12) *fp12 { return fp12FrobeniusCubeEfficient(f) }
