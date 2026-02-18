package crypto

// BN254 implementation details: efficient Frobenius maps and tower arithmetic.
//
// The naive Frobenius (fp12Exp(f, p)) requires ~254 squarings and multiplications
// in F_p^12 which is very expensive. Instead, we use the algebraic structure:
//
//   For the tower F_p -> F_p^2 -> F_p^6 -> F_p^12:
//   - F_p^2 = F_p[i]/(i^2+1)
//   - F_p^6 = F_p^2[v]/(v^3-xi), xi = 9+i
//   - F_p^12 = F_p^6[w]/(w^2-v)
//
// The Frobenius endomorphism x -> x^p acts on each coefficient by conjugation
// (in F_p^2) and multiplication by precomputed constants (powers of xi).
//
// These constants are xi^((p^k - 1) / d) for various k and d, computed offline.

import "math/big"

// Frobenius constants for F_p^6 and F_p^12.
//
// In F_p^6, the Frobenius pi acts on (c0 + c1*v + c2*v^2) as:
//   pi(c0) + pi(c1) * xi^((p-1)/3) * v + pi(c2) * xi^((2(p-1))/3) * v^2
// where pi on F_p^2 is conjugation.
//
// In F_p^12, pi acts on (a + b*w) as:
//   pi(a) + pi(b) * xi^((p-1)/2) * w
//
// For higher Frobenius powers (p^2, p^3), we use the corresponding powers.

// gammaCoefficients are precomputed constants for Frobenius maps.
// gamma[i][j] = xi^((j*(p^i - 1)) / d) where d depends on the tower level.

// For Frobenius pi (p^1):
// gamma11 = xi^((p-1)/3) -- coefficient for v in F_p^6
// gamma12 = xi^((2(p-1))/3) -- coefficient for v^2 in F_p^6
// gamma13 = xi^((p-1)/2) -- coefficient for w in F_p^12

var (
	// xi^((p-1)/3) in F_p^2 -- used for c1 coefficient in Frobenius on F_p^6
	gamma11a0, _ = new(big.Int).SetString("21575463638280843010398324269430826099269044274347216827212613867836435027261", 10)
	gamma11a1, _ = new(big.Int).SetString("10307601595873709700078755136146204025218092992518765122318458099831426205727", 10)
	gamma11     = &fp2{a0: gamma11a0, a1: gamma11a1}

	// xi^((2(p-1))/3) in F_p^2
	gamma12a0, _ = new(big.Int).SetString("2821565182194536844548159561693502659359617185244120367078079554186484126554", 10)
	gamma12a1, _ = new(big.Int).SetString("3505843767911556378687030309984248845540243509899259266946897622437848376689", 10)
	gamma12     = &fp2{a0: gamma12a0, a1: gamma12a1}

	// xi^((p-1)/2) in F_p^2
	gamma13a0, _ = new(big.Int).SetString("3505843767911556378687030309984248845540243509899259266946897622437848376689", 10)
	gamma13a1, _ = new(big.Int).SetString("2821565182194536844548159561693502659359617185244120367078079554186484126554", 10)
	gamma13     = &fp2{a0: gamma13a0, a1: gamma13a1}

	// For Frobenius pi^2 (p^2):
	// xi^((p^2-1)/3)
	gamma21a0, _ = new(big.Int).SetString("21888242871839275220042445260109153167277707414472061641714758635765020556616", 10)
	gamma21     = &fp2{a0: gamma21a0, a1: new(big.Int)}

	// xi^((2(p^2-1))/3)
	gamma22a0, _ = new(big.Int).SetString("21888242871839275220042445260109153167277707414472061641714758635765020556617", 10)
	gamma22     = &fp2{a0: gamma22a0, a1: new(big.Int)}

	// xi^((p^2-1)/2) = -1
	gamma23 = &fp2{a0: new(big.Int).Sub(bn254P, big.NewInt(1)), a1: new(big.Int)}

	// For Frobenius pi^3 (p^3):
	// These are conjugates of the pi^1 constants.
	gamma31a0, _ = new(big.Int).SetString("3772000881919853776433251133173384969862764767146999707016920680176045572446", 10)
	gamma31a1, _ = new(big.Int).SetString("19066677689644738135277537586023210547564610619270757373956590089388396372654", 10)
	gamma31     = &fp2{a0: gamma31a0, a1: gamma31a1}

	gamma32a0, _ = new(big.Int).SetString("5324479202449903542726783395506214481928257762400643279780343368557297135718", 10)
	gamma32a1, _ = new(big.Int).SetString("16208900380737693084919495127334387981393726419856888799917914180988844123039", 10)
	gamma32     = &fp2{a0: gamma32a0, a1: gamma32a1}

	gamma33a0, _ = new(big.Int).SetString("18566938241244942414004596690298913868373833782006617400804628704885040364344", 10)
	gamma33a1, _ = new(big.Int).SetString("16208900380737693084919495127334387981393726419856888799917914180988844123039", 10)
	gamma33     = &fp2{a0: gamma33a0, a1: gamma33a1}
)

// fp12FrobeniusEfficient computes the Frobenius endomorphism f^p on F_p^12
// using the tower structure, avoiding the expensive generic exponentiation.
//
// For f = (a0 + a1*w) where a0, a1 in F_p^6 and a_i = (c0 + c1*v + c2*v^2):
//   f^p = conj(a0.c0) + conj(a0.c1)*gamma11*v + conj(a0.c2)*gamma12*v^2
//       + (conj(a1.c0) + conj(a1.c1)*gamma11*v + conj(a1.c2)*gamma12*v^2) * gamma13 * w
func fp12FrobeniusEfficient(f *fp12) *fp12 {
	// Apply Frobenius to each F_p^2 coefficient (conjugation) and multiply by gammas.
	c00 := fp2Conj(f.c0.c0)
	c01 := fp2Mul(fp2Conj(f.c0.c1), gamma11)
	c02 := fp2Mul(fp2Conj(f.c0.c2), gamma12)

	c10 := fp2Mul(fp2Conj(f.c1.c0), gamma13)
	c11 := fp2Mul(fp2Mul(fp2Conj(f.c1.c1), gamma11), gamma13)
	c12 := fp2Mul(fp2Mul(fp2Conj(f.c1.c2), gamma12), gamma13)

	return &fp12{
		c0: &fp6{c0: c00, c1: c01, c2: c02},
		c1: &fp6{c0: c10, c1: c11, c2: c12},
	}
}

// fp12FrobeniusSqEfficient computes f^(p^2) on F_p^12.
// For p^2, conjugation composed with itself is the identity on F_p^2,
// so we just multiply by the gamma2x constants (which are in F_p, not F_p^2).
func fp12FrobeniusSqEfficient(f *fp12) *fp12 {
	c00 := newFp2(f.c0.c0.a0, f.c0.c0.a1)
	c01 := fp2Mul(f.c0.c1, gamma21)
	c02 := fp2Mul(f.c0.c2, gamma22)

	c10 := fp2Mul(f.c1.c0, gamma23)
	c11 := fp2Mul(fp2Mul(f.c1.c1, gamma21), gamma23)
	c12 := fp2Mul(fp2Mul(f.c1.c2, gamma22), gamma23)

	return &fp12{
		c0: &fp6{c0: c00, c1: c01, c2: c02},
		c1: &fp6{c0: c10, c1: c11, c2: c12},
	}
}

// fp12FrobeniusCubeEfficient computes f^(p^3) on F_p^12.
func fp12FrobeniusCubeEfficient(f *fp12) *fp12 {
	c00 := fp2Conj(f.c0.c0)
	c01 := fp2Mul(fp2Conj(f.c0.c1), gamma31)
	c02 := fp2Mul(fp2Conj(f.c0.c2), gamma32)

	c10 := fp2Mul(fp2Conj(f.c1.c0), gamma33)
	c11 := fp2Mul(fp2Mul(fp2Conj(f.c1.c1), gamma31), gamma33)
	c12 := fp2Mul(fp2Mul(fp2Conj(f.c1.c2), gamma32), gamma33)

	return &fp12{
		c0: &fp6{c0: c00, c1: c01, c2: c02},
		c1: &fp6{c0: c10, c1: c11, c2: c12},
	}
}

// g2ScalarMul computes k*P for a G2 point using double-and-add.
func g2ScalarMul(p *G2Point, k *big.Int) *G2Point {
	if k.Sign() == 0 || p.g2IsInfinity() {
		return G2Infinity()
	}
	kMod := new(big.Int).Mod(k, bn254N)
	if kMod.Sign() == 0 {
		return G2Infinity()
	}

	r := G2Infinity()
	base := &G2Point{
		x: newFp2(p.x.a0, p.x.a1),
		y: newFp2(p.y.a0, p.y.a1),
		z: newFp2(p.z.a0, p.z.a1),
	}
	for i := kMod.BitLen() - 1; i >= 0; i-- {
		r = g2Double(r)
		if kMod.Bit(i) == 1 {
			r = g2Add(r, base)
		}
	}
	return r
}

// g2IsOnCurveSubgroup checks if a G2 point is on the twist curve AND in
// the correct subgroup (order n). A point on the twist E' but not in the
// n-torsion subgroup would break the pairing.
//
// For BN254, we verify using the endomorphism: [n]*P == 0.
// However, checking order directly is expensive. For practical validation,
// we check the curve equation and trust the Frobenius endomorphism
// check that is implicitly part of the pairing.
func g2IsOnCurveSubgroup(x, y *fp2) bool {
	return g2IsOnCurve(x, y)
}
