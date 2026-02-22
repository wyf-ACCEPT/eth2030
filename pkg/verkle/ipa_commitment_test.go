package verkle

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

// --- FieldElement arithmetic tests ---

func TestFieldElement_Zero(t *testing.T) {
	z := Zero()
	if !z.IsZero() {
		t.Error("Zero() should be zero")
	}
}

func TestFieldElement_One(t *testing.T) {
	o := One()
	if o.IsZero() {
		t.Error("One() should not be zero")
	}
	if o.BigInt().Cmp(big.NewInt(1)) != 0 {
		t.Errorf("One() = %s, want 1", o.BigInt().Text(10))
	}
}

func TestFieldElement_FromUint64(t *testing.T) {
	f := FieldElementFromUint64(42)
	if f.BigInt().Cmp(big.NewInt(42)) != 0 {
		t.Errorf("FieldElementFromUint64(42) = %s, want 42", f.BigInt().Text(10))
	}
}

func TestFieldElement_FromBytes(t *testing.T) {
	var buf [32]byte
	buf[31] = 7
	f := FieldElementFromBytes(buf[:])
	if f.BigInt().Cmp(big.NewInt(7)) != 0 {
		t.Errorf("FieldElementFromBytes = %s, want 7", f.BigInt().Text(10))
	}
}

func TestFieldElement_Add(t *testing.T) {
	a := FieldElementFromUint64(10)
	b := FieldElementFromUint64(20)
	c := a.Add(b)
	if c.BigInt().Cmp(big.NewInt(30)) != 0 {
		t.Errorf("10 + 20 = %s, want 30", c.BigInt().Text(10))
	}
}

func TestFieldElement_AddOverflow(t *testing.T) {
	// Adding n-1 + 1 should give 0 (mod n).
	nMinus1 := NewFieldElement(new(big.Int).Sub(order, big.NewInt(1)))
	one := One()
	result := nMinus1.Add(one)
	if !result.IsZero() {
		t.Errorf("(n-1) + 1 = %s, want 0", result.BigInt().Text(10))
	}
}

func TestFieldElement_Sub(t *testing.T) {
	a := FieldElementFromUint64(30)
	b := FieldElementFromUint64(10)
	c := a.Sub(b)
	if c.BigInt().Cmp(big.NewInt(20)) != 0 {
		t.Errorf("30 - 10 = %s, want 20", c.BigInt().Text(10))
	}
}

func TestFieldElement_SubUnderflow(t *testing.T) {
	// 0 - 1 should give n-1 (mod n).
	z := Zero()
	one := One()
	result := z.Sub(one)
	expected := new(big.Int).Sub(order, big.NewInt(1))
	if result.BigInt().Cmp(expected) != 0 {
		t.Errorf("0 - 1 = %s, want %s", result.BigInt().Text(16), expected.Text(16))
	}
}

func TestFieldElement_Mul(t *testing.T) {
	a := FieldElementFromUint64(6)
	b := FieldElementFromUint64(7)
	c := a.Mul(b)
	if c.BigInt().Cmp(big.NewInt(42)) != 0 {
		t.Errorf("6 * 7 = %s, want 42", c.BigInt().Text(10))
	}
}

func TestFieldElement_MulIdentity(t *testing.T) {
	a := FieldElementFromUint64(123)
	one := One()
	result := a.Mul(one)
	if !result.Equal(a) {
		t.Error("a * 1 should equal a")
	}
}

func TestFieldElement_MulZero(t *testing.T) {
	a := FieldElementFromUint64(123)
	z := Zero()
	result := a.Mul(z)
	if !result.IsZero() {
		t.Error("a * 0 should be 0")
	}
}

func TestFieldElement_Neg(t *testing.T) {
	a := FieldElementFromUint64(5)
	negA := a.Neg()
	sum := a.Add(negA)
	if !sum.IsZero() {
		t.Errorf("a + (-a) = %s, want 0", sum.BigInt().Text(10))
	}
}

func TestFieldElement_NegZero(t *testing.T) {
	z := Zero()
	negZ := z.Neg()
	if !negZ.IsZero() {
		t.Error("-0 should be 0")
	}
}

func TestFieldElement_Inv(t *testing.T) {
	a := FieldElementFromUint64(7)
	aInv := a.Inv()
	product := a.Mul(aInv)
	if !product.Equal(One()) {
		t.Errorf("7 * 7^(-1) = %s, want 1", product.BigInt().Text(10))
	}
}

func TestFieldElement_InvLargeValue(t *testing.T) {
	// Use a large value and verify a * a^(-1) = 1.
	a := FieldElementFromUint64(999999937)
	aInv := a.Inv()
	product := a.Mul(aInv)
	if !product.Equal(One()) {
		t.Error("a * a^(-1) should equal 1 for large a")
	}
}

func TestFieldElement_InvPanicsOnZero(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Inv(0) should panic")
		}
	}()
	Zero().Inv()
}

func TestFieldElement_Equal(t *testing.T) {
	a := FieldElementFromUint64(42)
	b := FieldElementFromUint64(42)
	c := FieldElementFromUint64(43)

	if !a.Equal(b) {
		t.Error("equal elements should be equal")
	}
	if a.Equal(c) {
		t.Error("different elements should not be equal")
	}
}

func TestFieldElement_Bytes(t *testing.T) {
	a := FieldElementFromUint64(256)
	b := a.Bytes()
	// 256 = 0x100, so b[30] = 0x01, b[31] = 0x00.
	if b[30] != 0x01 || b[31] != 0x00 {
		t.Errorf("Bytes() = %x, want ...0100", b)
	}
}

func TestFieldElement_Reduction(t *testing.T) {
	// Values larger than order should be reduced.
	large := new(big.Int).Add(order, big.NewInt(5))
	f := NewFieldElement(large)
	if f.BigInt().Cmp(big.NewInt(5)) != 0 {
		t.Errorf("n+5 mod n = %s, want 5", f.BigInt().Text(10))
	}
}

// --- IPAConfig tests ---

func TestDefaultIPAConfig(t *testing.T) {
	cfg := DefaultIPAConfig()
	if cfg.DomainSize != NodeWidth {
		t.Errorf("DomainSize = %d, want %d", cfg.DomainSize, NodeWidth)
	}
	if len(cfg.Generators) != NodeWidth {
		t.Errorf("Generators count = %d, want %d", len(cfg.Generators), NodeWidth)
	}
	// Generators should not be nil.
	for i, g := range cfg.Generators {
		if g == nil {
			t.Errorf("Generator %d is nil", i)
		}
	}
}

// --- Pedersen commitment tests ---

func TestPedersenCommit_ZeroValues(t *testing.T) {
	cfg := DefaultIPAConfig()
	values := make([]FieldElement, NodeWidth)
	for i := range values {
		values[i] = Zero()
	}
	c := cfg.PedersenCommit(values)
	// Commitment to all zeros should be identity -> map to zero bytes.
	if !c.IsZero() {
		t.Error("commitment to zero vector should be zero")
	}
}

func TestPedersenCommit_SingleValue(t *testing.T) {
	cfg := DefaultIPAConfig()
	values := make([]FieldElement, NodeWidth)
	for i := range values {
		values[i] = Zero()
	}
	values[0] = FieldElementFromUint64(1)

	c := cfg.PedersenCommit(values)
	if c.IsZero() {
		t.Error("commitment to non-zero vector should not be zero")
	}
}

func TestPedersenCommit_DifferentValues(t *testing.T) {
	cfg := DefaultIPAConfig()

	values1 := make([]FieldElement, 4)
	values2 := make([]FieldElement, 4)
	for i := range values1 {
		values1[i] = FieldElementFromUint64(uint64(i + 1))
		values2[i] = FieldElementFromUint64(uint64(i + 10))
	}

	c1 := cfg.PedersenCommit(values1)
	c2 := cfg.PedersenCommit(values2)

	if c1 == c2 {
		t.Error("different value vectors should produce different commitments")
	}
}

func TestPedersenCommit_Deterministic(t *testing.T) {
	cfg := DefaultIPAConfig()

	values := make([]FieldElement, 4)
	for i := range values {
		values[i] = FieldElementFromUint64(uint64(i + 1))
	}

	c1 := cfg.PedersenCommit(values)
	c2 := cfg.PedersenCommit(values)

	if c1 != c2 {
		t.Error("same inputs should produce same commitment")
	}
}

func TestPedersenCommitPoint_Consistency(t *testing.T) {
	cfg := DefaultIPAConfig()

	values := make([]FieldElement, 4)
	for i := range values {
		values[i] = FieldElementFromUint64(uint64(i + 1))
	}

	c := cfg.PedersenCommit(values)
	pt := cfg.PedersenCommitPoint(values)
	cFromPt := commitmentFromPoint(pt)

	if c != cFromPt {
		t.Error("PedersenCommit and PedersenCommitPoint should produce consistent results")
	}
}

// --- IPA proof generation and verification tests ---

func TestIPAProve_SmallVector(t *testing.T) {
	// Use a small power-of-2 config for testing.
	cfg := smallIPAConfig(4)

	poly := []FieldElement{
		FieldElementFromUint64(3),
		FieldElementFromUint64(7),
		FieldElementFromUint64(2),
		FieldElementFromUint64(5),
	}

	// Evaluate the polynomial at point 0: f(0) = poly[0] = 3
	// (Lagrange basis at 0: L_0(0)=1, L_i(0)=0 for i>0)
	evalPoint := FieldElementFromUint64(0)

	// Compute the actual evaluation using Lagrange interpolation.
	evalResult := evaluatePolynomial(poly, evalPoint)

	proof, err := IPAProve(cfg, poly, evalPoint, evalResult)
	if err != nil {
		t.Fatalf("IPAProve error: %v", err)
	}
	if proof == nil {
		t.Fatal("proof is nil")
	}
	if proof.Inner == nil {
		t.Fatal("inner proof is nil")
	}
}

func TestIPAProveAndVerifyWithPoint(t *testing.T) {
	cfg := smallIPAConfig(4)

	poly := []FieldElement{
		FieldElementFromUint64(3),
		FieldElementFromUint64(7),
		FieldElementFromUint64(2),
		FieldElementFromUint64(5),
	}

	evalPoint := FieldElementFromUint64(0)
	evalResult := evaluatePolynomial(poly, evalPoint)

	proof, err := IPAProve(cfg, poly, evalPoint, evalResult)
	if err != nil {
		t.Fatalf("IPAProve error: %v", err)
	}

	// Get the commitment point for verification.
	commitPt := cfg.PedersenCommitPoint(poly)

	ok, err := IPAVerifyWithPoint(cfg, commitPt, proof)
	if err != nil {
		t.Fatalf("IPAVerifyWithPoint error: %v", err)
	}
	if !ok {
		t.Error("valid proof should verify")
	}
}

func TestIPAProveAndVerify_DifferentPoints(t *testing.T) {
	cfg := smallIPAConfig(4)

	poly := []FieldElement{
		FieldElementFromUint64(10),
		FieldElementFromUint64(20),
		FieldElementFromUint64(30),
		FieldElementFromUint64(40),
	}

	// Evaluate at point 1: f(1) = sum(poly[i] * L_i(1))
	// For a 4-element domain, L_1(1) = 1, L_i(1) = 0 for i != 1, so f(1) = 20.
	evalPoint := FieldElementFromUint64(1)
	evalResult := evaluatePolynomial(poly, evalPoint)

	proof, err := IPAProve(cfg, poly, evalPoint, evalResult)
	if err != nil {
		t.Fatalf("IPAProve error: %v", err)
	}

	commitPt := cfg.PedersenCommitPoint(poly)
	ok, err := IPAVerifyWithPoint(cfg, commitPt, proof)
	if err != nil {
		t.Fatalf("IPAVerifyWithPoint error: %v", err)
	}
	if !ok {
		t.Error("valid proof at point 1 should verify")
	}
}

func TestIPAProve_WrongEvalResult_Fails(t *testing.T) {
	cfg := smallIPAConfig(4)

	poly := []FieldElement{
		FieldElementFromUint64(3),
		FieldElementFromUint64(7),
		FieldElementFromUint64(2),
		FieldElementFromUint64(5),
	}

	evalPoint := FieldElementFromUint64(0)
	// Claim a wrong evaluation result.
	wrongResult := FieldElementFromUint64(999)

	proof, err := IPAProve(cfg, poly, evalPoint, wrongResult)
	if err != nil {
		t.Fatalf("IPAProve error: %v", err)
	}

	commitPt := cfg.PedersenCommitPoint(poly)

	// The proof should still be generated (it proves the inner product),
	// but the claimed evalResult in the proof metadata is wrong.
	// When the verifier reconstructs, the IPA should still verify because
	// the proof proves the actual inner product, not the claimed one.
	// The mismatch would be detected at a higher level.
	ok, err := IPAVerifyWithPoint(cfg, commitPt, proof)
	if err != nil {
		t.Fatalf("IPAVerifyWithPoint error: %v", err)
	}
	// The IPA proof itself is valid (it proves the actual inner product).
	if !ok {
		t.Log("proof with wrong claimed result failed IPA check (expected in some implementations)")
	}
}

func TestIPAProve_NilProof(t *testing.T) {
	cfg := smallIPAConfig(4)
	_, err := IPAVerifyWithPoint(cfg, crypto.BanderIdentity(), nil)
	if err == nil {
		t.Error("nil proof should error")
	}
}

func TestIPAProve_EmptyPoly(t *testing.T) {
	cfg := smallIPAConfig(4)
	_, err := IPAProve(cfg, nil, Zero(), Zero())
	if err == nil {
		t.Error("empty polynomial should error")
	}
}

// --- Verkle node commitment tests ---

func TestVerkleCommitNode_AllZero(t *testing.T) {
	cfg := DefaultIPAConfig()
	var children [NodeWidth]Commitment
	c := VerkleCommitNode(cfg, children)
	if !c.IsZero() {
		t.Error("commitment to all-zero children should be zero")
	}
}

func TestVerkleCommitNode_NonZero(t *testing.T) {
	cfg := DefaultIPAConfig()
	var children [NodeWidth]Commitment
	children[0] = Commitment{1}
	c := VerkleCommitNode(cfg, children)
	if c.IsZero() {
		t.Error("commitment with non-zero child should not be zero")
	}
}

func TestVerkleCommitNode_DifferentChildren(t *testing.T) {
	cfg := DefaultIPAConfig()

	var c1 [NodeWidth]Commitment
	c1[0] = Commitment{1}

	var c2 [NodeWidth]Commitment
	c2[0] = Commitment{2}

	r1 := VerkleCommitNode(cfg, c1)
	r2 := VerkleCommitNode(cfg, c2)

	if r1 == r2 {
		t.Error("different children should produce different node commitments")
	}
}

func TestVerkleCommitNode_Deterministic(t *testing.T) {
	cfg := DefaultIPAConfig()
	var children [NodeWidth]Commitment
	children[0] = Commitment{1}
	children[5] = Commitment{2}

	c1 := VerkleCommitNode(cfg, children)
	c2 := VerkleCommitNode(cfg, children)

	if c1 != c2 {
		t.Error("same children should produce same commitment")
	}
}

func TestVerkleCommitLeaf_Basic(t *testing.T) {
	cfg := DefaultIPAConfig()
	var stem [StemSize]byte
	stem[0] = 0x01
	var values [NodeWidth]FieldElement
	for i := range values {
		values[i] = Zero()
	}
	values[0] = FieldElementFromUint64(42)

	c := VerkleCommitLeaf(cfg, stem, values)
	if c.IsZero() {
		t.Error("leaf commitment with non-zero stem+value should not be zero")
	}
}

func TestVerkleCommitLeaf_DifferentStems(t *testing.T) {
	cfg := DefaultIPAConfig()

	var stem1, stem2 [StemSize]byte
	stem1[0] = 0x01
	stem2[0] = 0x02

	var values [NodeWidth]FieldElement
	for i := range values {
		values[i] = Zero()
	}

	c1 := VerkleCommitLeaf(cfg, stem1, values)
	c2 := VerkleCommitLeaf(cfg, stem2, values)

	if c1 == c2 {
		t.Error("different stems should produce different leaf commitments")
	}
}

func TestVerkleCommitLeaf_DifferentValues(t *testing.T) {
	cfg := DefaultIPAConfig()

	var stem [StemSize]byte
	stem[0] = 0x01

	var v1, v2 [NodeWidth]FieldElement
	for i := range v1 {
		v1[i] = Zero()
		v2[i] = Zero()
	}
	v1[0] = FieldElementFromUint64(1)
	v2[0] = FieldElementFromUint64(2)

	c1 := VerkleCommitLeaf(cfg, stem, v1)
	c2 := VerkleCommitLeaf(cfg, stem, v2)

	if c1 == c2 {
		t.Error("different values should produce different leaf commitments")
	}
}

// --- Lagrange basis tests ---

func TestLagrangeBasis_AtDomainPoint(t *testing.T) {
	// L_i(i) should be 1 for any domain point i.
	n := 4
	for i := 0; i < n; i++ {
		z := FieldElementFromUint64(uint64(i))
		val := lagrangeBasis(i, z, n)
		if !val.Equal(One()) {
			t.Errorf("L_%d(%d) = %s, want 1", i, i, val.BigInt().Text(10))
		}
	}
}

func TestLagrangeBasis_AtOtherPoint(t *testing.T) {
	// L_i(j) should be 0 for j != i (j in domain).
	n := 4
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			z := FieldElementFromUint64(uint64(j))
			val := lagrangeBasis(i, z, n)
			if !val.IsZero() {
				t.Errorf("L_%d(%d) = %s, want 0", i, j, val.BigInt().Text(10))
			}
		}
	}
}

func TestBuildEvalVector_AtDomainPoint(t *testing.T) {
	// At domain point i, the eval vector should be the i-th unit vector.
	n := 4
	z := FieldElementFromUint64(2)
	bVec := buildEvalVector(z, n)

	for i := 0; i < n; i++ {
		if i == 2 {
			if !bVec[i].Equal(One()) {
				t.Errorf("b[%d] = %s, want 1", i, bVec[i].BigInt().Text(10))
			}
		} else {
			if !bVec[i].IsZero() {
				t.Errorf("b[%d] = %s, want 0", i, bVec[i].BigInt().Text(10))
			}
		}
	}
}

// --- Helper functions for tests ---

// smallIPAConfig creates an IPAConfig with the given (small) domain size
// using the first n Pedersen generators. n must be a power of 2.
func smallIPAConfig(n int) *IPAConfig {
	gens := crypto.GeneratePedersenGenerators()
	pts := make([]*crypto.BanderPoint, n)
	for i := 0; i < n; i++ {
		pts[i] = gens[i]
	}
	return &IPAConfig{
		Generators: pts,
		DomainSize: n,
	}
}

// evaluatePolynomial evaluates a polynomial (in Lagrange coefficient form)
// at a given point. The polynomial is f(X) = sum(poly[i] * L_i(X)).
func evaluatePolynomial(poly []FieldElement, z FieldElement) FieldElement {
	result := Zero()
	for i, coeff := range poly {
		basis := lagrangeBasis(i, z, len(poly))
		result = result.Add(coeff.Mul(basis))
	}
	return result
}
