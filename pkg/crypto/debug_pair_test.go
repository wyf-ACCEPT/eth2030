package crypto

import (
	"encoding/hex"
	"math/big"
	"testing"
)

func TestDebugPairing(t *testing.T) {
	// two_point_match_3 test:
	// Pair 1: G1=(1,2), Q1=specific non-standard G2 point
	// Pair 2: G1=2G, Q2=standard G2 generator
	// Expected: product of pairings == 1
	
	inp, _ := hex.DecodeString("00000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002203e205db4f19b37b60121b83a7333706db86431c6d835849957ed8c3928ad7927dc7234fd11d3e8c36c59277c3e6f149d5cd3cfa9a62aee49f8130962b4b3b9195e8aa5b7827463722b8c153931579d3505566b4edf48d498e185f0509de15204bb53b8977e5f92a0bc372742c4830944a59b4fe6b1c0466e2a6dad122b5d2e030644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd31a76dae6d3272396d0cbe61fced2bc532edac647851e3ac53ce1cc9c7e645a83198e9393920d483a7260bfb731fb5d25f1aa493335a9e71297e485b7aef312c21800deef121f1e76426a00665e5c4479674322d4f75edadd46debd5cd992f6ed090689d0585ff075ec9e99ad690c3395bc4b313370b38ef355acdadcd122975b12c85ea5db8c6deb4aab71808dcb408fe3d1e7690c43d37b4ce6cc0166fa7daa")
	
	// Parse pair 1
	g1x1 := new(big.Int).SetBytes(inp[0:32])
	g1y1 := new(big.Int).SetBytes(inp[32:64])
	
	q1xIm := new(big.Int).SetBytes(inp[64:96])
	q1xRe := new(big.Int).SetBytes(inp[96:128])
	q1yIm := new(big.Int).SetBytes(inp[128:160])
	q1yRe := new(big.Int).SetBytes(inp[160:192])
	
	// Parse pair 2
	g1x2 := new(big.Int).SetBytes(inp[192:224])
	g1y2 := new(big.Int).SetBytes(inp[224:256])
	
	q2xIm := new(big.Int).SetBytes(inp[256:288])
	q2xRe := new(big.Int).SetBytes(inp[288:320])
	q2yIm := new(big.Int).SetBytes(inp[320:352])
	q2yRe := new(big.Int).SetBytes(inp[352:384])
	
	t.Logf("Pair 1: G1=(%s, %s)", g1x1, g1y1)
	t.Logf("Pair 1: Q1 x=(%s + %s*i)", q1xRe, q1xIm)
	t.Logf("Pair 1: Q1 y=(%s + %s*i)", q1yRe, q1yIm)
	
	t.Logf("Pair 2: G1=(%s, %s)", g1x2, g1y2)
	t.Logf("Pair 2: Q2 x=(%s + %s*i)", q2xRe, q2xIm)
	t.Logf("Pair 2: Q2 y=(%s + %s*i)", q2yRe, q2yIm)
	
	// Check Q2 is standard generator
	t.Logf("Q2 is standard gen: x_im=%v x_re=%v y_im=%v y_re=%v",
		q2xIm.Cmp(g2GenXa1) == 0,
		q2xRe.Cmp(g2GenXa0) == 0,
		q2yIm.Cmp(g2GenYa1) == 0,
		q2yRe.Cmp(g2GenYa0) == 0)
	
	// Check Q1 is on twist curve
	q1x := &fp2{a0: q1xRe, a1: q1xIm}
	q1y := &fp2{a0: q1yRe, a1: q1yIm}
	t.Logf("Q1 on twist: %v", g2IsOnCurve(q1x, q1y))
	
	// Compute pair 1 Miller loop
	ml1 := millerLoop(g1x1, g1y1, q1x, q1y)
	t.Logf("ML1 is one: %v", ml1.isOne())
	
	// Compute pair 2 Miller loop  
	q2x := &fp2{a0: q2xRe, a1: q2xIm}
	q2y := &fp2{a0: q2yRe, a1: q2yIm}
	ml2 := millerLoop(g1x2, g1y2, q2x, q2y)
	t.Logf("ML2 is one: %v", ml2.isOne())
	
	// Product of Miller loops
	product := fp12Mul(ml1, ml2)
	t.Logf("Product ML is one: %v", product.isOne())
	
	// Final exp of product
	result := finalExp(product)
	t.Logf("Final result is one: %v", result.isOne())
	
	// Now test each separately  
	e1 := finalExp(ml1)
	t.Logf("e(G, Q1) is one: %v", e1.isOne())
	e2 := finalExp(ml2)
	t.Logf("e(2G, G2gen) is one: %v", e2.isOne())
	
	// Product of separate pairings
	e12 := fp12Mul(e1, e2)
	t.Logf("e1*e2 is one: %v", e12.isOne())
	
	// Check: e(2G, G2gen) should equal e(G, G2gen)^2
	gen1 := G1Generator()
	gen2 := G2Generator()
	eGG := BN254Pair(gen1, gen2)
	eGG2 := fp12Mul(eGG, eGG) // e(G, G2)^2
	t.Logf("e(G,G2)^2 == e(2G,G2): %v", fp12Equal(eGG2, e2))
	
	// If Q1 should make product=1 with (2G, G2gen):
	// e(G,Q1) * e(2G,G2gen) = 1
	// e(G,Q1) = e(2G,G2gen)^(-1) = e(G,G2gen)^(-2)
	// This means Q1 = -2 * G2gen
	
	// Compute -2*G2gen
	neg2G2 := g2ScalarMul(gen2, big.NewInt(2))
	neg2G2 = g2Neg(neg2G2)
	n2x, n2y := neg2G2.g2ToAffine()
	t.Logf("-2*G2gen x = (%s + %s*i)", n2x.a0, n2x.a1)
	t.Logf("-2*G2gen y = (%s + %s*i)", n2y.a0, n2y.a1)
	t.Logf("Q1 == -2*G2gen: x=%v y=%v", q1x.equal(n2x), q1y.equal(n2y))
}

func fp12Equal(a, b *fp12) bool {
	return a.c0.c0.equal(b.c0.c0) && a.c0.c1.equal(b.c0.c1) && a.c0.c2.equal(b.c0.c2) &&
		a.c1.c0.equal(b.c1.c0) && a.c1.c1.equal(b.c1.c1) && a.c1.c2.equal(b.c1.c2)
}
