package crypto

import (
	"math/big"
	"testing"
)

func TestBilinearityDetailed(t *testing.T) {
	g1 := G1Generator()
	g2 := G2Generator()
	
	px, py := g1.g1ToAffine()
	qx, qy := g2.g2ToAffine()
	
	// Miller loop for e(G, G2)
	ml1 := millerLoop(px, py, qx, qy)
	
	// Miller loop for e(2G, G2)
	g1_2 := G1ScalarMul(g1, big.NewInt(2))
	p2x, p2y := g1_2.g1ToAffine()
	ml2 := millerLoop(p2x, p2y, qx, qy)
	
	// Check: ml2 should equal ml1^2 (up to Fp6 factor)
	ml1sq := fp12Mul(ml1, ml1)
	
	// After easy part of final exp
	easy1 := easyPart(ml1)
	easy2 := easyPart(ml2)
	easy1sq := easyPart(ml1sq)
	
	// After easy part, the Fp6 factors are killed
	// So easy2 should equal easy1^2 IF the Miller loop is correct
	e1sq := fp12Mul(easy1, easy1)
	t.Logf("After easy part, e(2G,Q) == e(G,Q)^2: %v", fp12Equal(easy2, e1sq))
	t.Logf("After easy part, e(ml1^2) == e(ml1)^2: %v", fp12Equal(easy1sq, e1sq))
	
	// Final exp
	full1 := finalExpHard(easy1)
	full2 := finalExpHard(easy2)
	full1sq := fp12Mul(full1, full1)
	t.Logf("After full exp, e(2G,Q) == e(G,Q)^2: %v", fp12Equal(full2, full1sq))
	
	// Also test e(G, 2*G2)
	g2_2 := g2ScalarMul(g2, big.NewInt(2))
	q2x, q2y := g2_2.g2ToAffine()
	ml3 := millerLoop(px, py, q2x, q2y)
	easy3 := easyPart(ml3)
	full3 := finalExpHard(easy3)
	t.Logf("After full exp, e(G, 2*G2) == e(G,G2)^2: %v", fp12Equal(full3, full1sq))
}

func easyPart(f *fp12) *fp12 {
	fInv := fp12Inv(f)
	f1 := fp12Mul(fp12Conj(f), fInv) // f^(p^6-1)
	f2 := fp12Mul(fp12FrobSq(f1), f1) // f1^(p^2+1)
	return f2
}
