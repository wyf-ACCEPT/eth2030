package zkvm

import (
	"math/big"
	"testing"
)

func TestNewCircuit(t *testing.T) {
	c := NewCircuit()
	if c == nil {
		t.Fatal("NewCircuit returned nil")
	}
	if c.NumVariables() != 1 {
		t.Fatalf("expected 1 variable (OneVar), got %d", c.NumVariables())
	}
	// OneVar should have value 1.
	one := c.GetWitness(OneVar)
	if one.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("OneVar should be 1, got %s", one.String())
	}
	if !c.IsPublic(OneVar) {
		t.Fatal("OneVar should be public")
	}
}

func TestCircuitField(t *testing.T) {
	c := NewCircuit()
	field := c.Field()
	if field.Cmp(bn254ScalarField) != 0 {
		t.Fatal("circuit field should be BN254 scalar field")
	}
}

func TestNewCircuitWithCustomField(t *testing.T) {
	prime := big.NewInt(101)
	c := NewCircuitWithField(prime)
	if c.Field().Cmp(prime) != 0 {
		t.Fatalf("expected custom field 101, got %s", c.Field().String())
	}
}

func TestNewCircuitWithNilField(t *testing.T) {
	c := NewCircuitWithField(nil)
	if c.Field().Cmp(bn254ScalarField) != 0 {
		t.Fatal("nil field should default to BN254")
	}
}

func TestAllocatePublic(t *testing.T) {
	c := NewCircuit()
	v, err := c.AllocatePublic(big.NewInt(42))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v == OneVar {
		t.Fatal("public variable should not be OneVar")
	}
	if !c.IsPublic(v) {
		t.Fatal("allocated variable should be public")
	}
	val := c.GetWitness(v)
	if val.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("expected 42, got %s", val.String())
	}
}

func TestAllocatePublicNilValue(t *testing.T) {
	c := NewCircuit()
	_, err := c.AllocatePublic(nil)
	if err != ErrNilValue {
		t.Fatalf("expected ErrNilValue, got %v", err)
	}
}

func TestAllocatePrivate(t *testing.T) {
	c := NewCircuit()
	v, err := c.AllocatePrivate(big.NewInt(99))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.IsPublic(v) {
		t.Fatal("private variable should not be public")
	}
	val := c.GetWitness(v)
	if val.Cmp(big.NewInt(99)) != 0 {
		t.Fatalf("expected 99, got %s", val.String())
	}
}

func TestAllocatePrivateNilValue(t *testing.T) {
	c := NewCircuit()
	_, err := c.AllocatePrivate(nil)
	if err != ErrNilValue {
		t.Fatalf("expected ErrNilValue, got %v", err)
	}
}

func TestAllocateAfterFinalize(t *testing.T) {
	c := NewCircuit()
	c.Synthesize()

	_, err := c.AllocatePublic(big.NewInt(1))
	if err != ErrCircuitFinalized {
		t.Fatalf("expected ErrCircuitFinalized, got %v", err)
	}
	_, err = c.AllocatePrivate(big.NewInt(1))
	if err != ErrCircuitFinalized {
		t.Fatalf("expected ErrCircuitFinalized, got %v", err)
	}
}

func TestAddConstraint(t *testing.T) {
	c := NewCircuit()
	a, _ := c.AllocatePrivate(big.NewInt(3))
	b, _ := c.AllocatePrivate(big.NewInt(5))
	cv, _ := c.AllocatePrivate(big.NewInt(15)) // 3 * 5 = 15

	err := c.AddConstraint(a, b, cv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.NumConstraints() != 1 {
		t.Fatalf("expected 1 constraint, got %d", c.NumConstraints())
	}

	// Check the constraint is satisfied.
	if err := c.CheckConstraints(); err != nil {
		t.Fatalf("constraint should be satisfied: %v", err)
	}
}

func TestAddConstraintUnsatisfied(t *testing.T) {
	c := NewCircuit()
	a, _ := c.AllocatePrivate(big.NewInt(3))
	b, _ := c.AllocatePrivate(big.NewInt(5))
	cv, _ := c.AllocatePrivate(big.NewInt(10)) // 3 * 5 != 10

	c.AddConstraint(a, b, cv)
	if err := c.CheckConstraints(); err == nil {
		t.Fatal("expected constraint violation")
	}
}

func TestAddConstraintAfterFinalize(t *testing.T) {
	c := NewCircuit()
	c.Synthesize()
	err := c.AddConstraint(OneVar, OneVar, OneVar)
	if err != ErrCircuitFinalized {
		t.Fatalf("expected ErrCircuitFinalized, got %v", err)
	}
}

func TestAssertEqual(t *testing.T) {
	c := NewCircuit()
	a, _ := c.AllocatePrivate(big.NewInt(7))
	b, _ := c.AllocatePrivate(big.NewInt(7))

	err := c.AssertEqual(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := c.CheckConstraints(); err != nil {
		t.Fatalf("equal variables should satisfy: %v", err)
	}
}

func TestAssertEqualFails(t *testing.T) {
	c := NewCircuit()
	a, _ := c.AllocatePrivate(big.NewInt(7))
	b, _ := c.AllocatePrivate(big.NewInt(8))

	c.AssertEqual(a, b)
	if err := c.CheckConstraints(); err == nil {
		t.Fatal("unequal variables should fail assertion")
	}
}

func TestAssertEqualAfterFinalize(t *testing.T) {
	c := NewCircuit()
	v, _ := c.AllocatePrivate(big.NewInt(1))
	c.Synthesize()
	err := c.AssertEqual(v, v)
	if err != ErrCircuitFinalized {
		t.Fatalf("expected ErrCircuitFinalized, got %v", err)
	}
}

func TestAssertBool(t *testing.T) {
	c := NewCircuit()
	v0, _ := c.AllocatePrivate(big.NewInt(0))
	v1, _ := c.AllocatePrivate(big.NewInt(1))

	if err := c.AssertBool(v0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := c.AssertBool(v1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := c.CheckConstraints(); err != nil {
		t.Fatalf("0 and 1 should satisfy bool constraint: %v", err)
	}
}

func TestAssertBoolFails(t *testing.T) {
	c := NewCircuit()
	v, _ := c.AllocatePrivate(big.NewInt(2))
	c.AssertBool(v)
	if err := c.CheckConstraints(); err == nil {
		t.Fatal("value 2 should fail bool assertion")
	}
}

func TestAssertBoolAfterFinalize(t *testing.T) {
	c := NewCircuit()
	v, _ := c.AllocatePrivate(big.NewInt(1))
	c.Synthesize()
	err := c.AssertBool(v)
	if err != ErrCircuitFinalized {
		t.Fatalf("expected ErrCircuitFinalized, got %v", err)
	}
}

func TestAdd(t *testing.T) {
	c := NewCircuit()
	a, _ := c.AllocatePrivate(big.NewInt(10))
	b, _ := c.AllocatePrivate(big.NewInt(20))

	sum, err := c.Add(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := c.GetWitness(sum)
	if val.Cmp(big.NewInt(30)) != 0 {
		t.Fatalf("expected 30, got %s", val.String())
	}
	if err := c.CheckConstraints(); err != nil {
		t.Fatalf("add constraint should be satisfied: %v", err)
	}
}

func TestAddFieldWrapping(t *testing.T) {
	// Use a small field to test wrapping.
	c := NewCircuitWithField(big.NewInt(101))
	a, _ := c.AllocatePrivate(big.NewInt(90))
	b, _ := c.AllocatePrivate(big.NewInt(20))

	sum, err := c.Add(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := c.GetWitness(sum)
	// 90 + 20 = 110 mod 101 = 9.
	if val.Cmp(big.NewInt(9)) != 0 {
		t.Fatalf("expected 9 (mod 101), got %s", val.String())
	}
	if err := c.CheckConstraints(); err != nil {
		t.Fatalf("wrapped add should satisfy constraint: %v", err)
	}
}

func TestAddAfterFinalize(t *testing.T) {
	c := NewCircuit()
	v, _ := c.AllocatePrivate(big.NewInt(1))
	c.Synthesize()
	_, err := c.Add(v, v)
	if err != ErrCircuitFinalized {
		t.Fatalf("expected ErrCircuitFinalized, got %v", err)
	}
}

func TestMul(t *testing.T) {
	c := NewCircuit()
	a, _ := c.AllocatePrivate(big.NewInt(7))
	b, _ := c.AllocatePrivate(big.NewInt(6))

	prod, err := c.Mul(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := c.GetWitness(prod)
	if val.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("expected 42, got %s", val.String())
	}
	if err := c.CheckConstraints(); err != nil {
		t.Fatalf("mul constraint should be satisfied: %v", err)
	}
}

func TestMulFieldWrapping(t *testing.T) {
	c := NewCircuitWithField(big.NewInt(101))
	a, _ := c.AllocatePrivate(big.NewInt(50))
	b, _ := c.AllocatePrivate(big.NewInt(3))

	prod, err := c.Mul(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := c.GetWitness(prod)
	// 50 * 3 = 150 mod 101 = 49.
	if val.Cmp(big.NewInt(49)) != 0 {
		t.Fatalf("expected 49 (mod 101), got %s", val.String())
	}
	if err := c.CheckConstraints(); err != nil {
		t.Fatalf("wrapped mul should satisfy constraint: %v", err)
	}
}

func TestMulAfterFinalize(t *testing.T) {
	c := NewCircuit()
	v, _ := c.AllocatePrivate(big.NewInt(1))
	c.Synthesize()
	_, err := c.Mul(v, v)
	if err != ErrCircuitFinalized {
		t.Fatalf("expected ErrCircuitFinalized, got %v", err)
	}
}

func TestHash(t *testing.T) {
	c := NewCircuit()
	a, _ := c.AllocatePrivate(big.NewInt(1))
	b, _ := c.AllocatePrivate(big.NewInt(2))

	h, err := c.Hash(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := c.GetWitness(h)
	if val == nil || val.Sign() < 0 {
		t.Fatal("hash output should be non-nil and non-negative")
	}
	if val.Cmp(c.Field()) >= 0 {
		t.Fatal("hash output should be within field")
	}
	if err := c.CheckConstraints(); err != nil {
		t.Fatalf("hash constraint should be satisfied: %v", err)
	}
}

func TestHashDeterministic(t *testing.T) {
	c1 := NewCircuit()
	a1, _ := c1.AllocatePrivate(big.NewInt(42))
	h1, _ := c1.Hash(a1)
	v1 := c1.GetWitness(h1)

	c2 := NewCircuit()
	a2, _ := c2.AllocatePrivate(big.NewInt(42))
	h2, _ := c2.Hash(a2)
	v2 := c2.GetWitness(h2)

	if v1.Cmp(v2) != 0 {
		t.Fatal("hash should be deterministic across circuit instances")
	}
}

func TestHashAfterFinalize(t *testing.T) {
	c := NewCircuit()
	v, _ := c.AllocatePrivate(big.NewInt(1))
	c.Synthesize()
	_, err := c.Hash(v)
	if err != ErrCircuitFinalized {
		t.Fatalf("expected ErrCircuitFinalized, got %v", err)
	}
}

func TestSynthesize(t *testing.T) {
	c := NewCircuit()
	a, _ := c.AllocatePublic(big.NewInt(3))
	b, _ := c.AllocatePrivate(big.NewInt(5))
	c.Mul(a, b)

	numC, numV := c.Synthesize()
	if numC != 1 {
		t.Fatalf("expected 1 constraint from mul, got %d", numC)
	}
	// Variables: OneVar(0), a(1), b(2), prod(3) = 4.
	if numV != 4 {
		t.Fatalf("expected 4 variables, got %d", numV)
	}
}

func TestPublicVariables(t *testing.T) {
	c := NewCircuit()
	pub1, _ := c.AllocatePublic(big.NewInt(1))
	pub2, _ := c.AllocatePublic(big.NewInt(2))
	c.AllocatePrivate(big.NewInt(3))

	pubVars := c.PublicVariables()
	if len(pubVars) != 2 {
		t.Fatalf("expected 2 public variables, got %d", len(pubVars))
	}
	// Check that both pub1 and pub2 are in the list.
	found := map[Variable]bool{}
	for _, v := range pubVars {
		found[v] = true
	}
	if !found[pub1] || !found[pub2] {
		t.Fatal("public variable list incomplete")
	}
}

func TestNumPublicInputs(t *testing.T) {
	c := NewCircuit()
	c.AllocatePublic(big.NewInt(1))
	c.AllocatePublic(big.NewInt(2))
	c.AllocatePrivate(big.NewInt(3))

	// Includes OneVar.
	if c.NumPublicInputs() != 3 {
		t.Fatalf("expected 3 public inputs (including OneVar), got %d", c.NumPublicInputs())
	}
}

func TestGetWitnessUnknownVariable(t *testing.T) {
	c := NewCircuit()
	val := c.GetWitness(Variable(999))
	if val.Sign() != 0 {
		t.Fatalf("unknown variable should have value 0, got %s", val.String())
	}
}

func TestCheckConstraintsEmpty(t *testing.T) {
	c := NewCircuit()
	if err := c.CheckConstraints(); err != nil {
		t.Fatalf("empty circuit should satisfy all constraints: %v", err)
	}
}

func TestVerifyBasic(t *testing.T) {
	c := NewCircuit()
	pub, _ := c.AllocatePublic(big.NewInt(42))
	priv, _ := c.AllocatePrivate(big.NewInt(42))
	c.AssertEqual(pub, priv)
	c.Synthesize()

	publicInputs := map[Variable]*big.Int{pub: big.NewInt(42)}
	proofData := make([]byte, 64) // mock proof
	proofData[0] = 0xFF

	valid, err := c.Verify(publicInputs, proofData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Fatal("valid proof should verify")
	}
}

func TestVerifyNotFinalized(t *testing.T) {
	c := NewCircuit()
	_, err := c.Verify(nil, make([]byte, 32))
	if err != ErrCircuitNotReady {
		t.Fatalf("expected ErrCircuitNotReady, got %v", err)
	}
}

func TestVerifyProofTooShort(t *testing.T) {
	c := NewCircuit()
	c.Synthesize()
	_, err := c.Verify(nil, []byte{1, 2, 3})
	if err != ErrProofTooShort {
		t.Fatalf("expected ErrProofTooShort, got %v", err)
	}
}

func TestVerifyPublicInputMismatch(t *testing.T) {
	c := NewCircuit()
	pub, _ := c.AllocatePublic(big.NewInt(42))
	c.Synthesize()

	// Wrong value for public input.
	publicInputs := map[Variable]*big.Int{pub: big.NewInt(99)}
	_, err := c.Verify(publicInputs, make([]byte, 64))
	if err != ErrVerificationFail {
		t.Fatalf("expected ErrVerificationFail, got %v", err)
	}
}

func TestVerifyWrongPublicInputCount(t *testing.T) {
	c := NewCircuit()
	c.AllocatePublic(big.NewInt(1))
	c.AllocatePublic(big.NewInt(2))
	c.Synthesize()

	// Only provide one public input instead of two.
	pub := c.PublicVariables()[0]
	publicInputs := map[Variable]*big.Int{pub: big.NewInt(1)}
	_, err := c.Verify(publicInputs, make([]byte, 64))
	if err != ErrPublicInputCount {
		t.Fatalf("expected ErrPublicInputCount, got %v", err)
	}
}

func TestVerifyNonPublicVariable(t *testing.T) {
	c := NewCircuit()
	pub, _ := c.AllocatePublic(big.NewInt(1))
	priv, _ := c.AllocatePrivate(big.NewInt(2))
	c.Synthesize()

	// Try to provide a private variable as public input.
	publicInputs := map[Variable]*big.Int{
		pub:  big.NewInt(1),
		priv: big.NewInt(2),
	}
	_, err := c.Verify(publicInputs, make([]byte, 64))
	// Should fail because count is wrong (2 provided, 1 expected).
	if err != ErrPublicInputCount {
		t.Fatalf("expected ErrPublicInputCount, got %v", err)
	}
}

// --- Complex circuit tests ---

func TestCircuitArithmeticChain(t *testing.T) {
	// Circuit: (a + b) * c = result
	c := NewCircuit()
	a, _ := c.AllocatePublic(big.NewInt(3))
	b, _ := c.AllocatePrivate(big.NewInt(4))
	cv, _ := c.AllocatePrivate(big.NewInt(5))

	sum, _ := c.Add(a, b) // 3 + 4 = 7
	prod, _ := c.Mul(sum, cv) // 7 * 5 = 35

	val := c.GetWitness(prod)
	if val.Cmp(big.NewInt(35)) != 0 {
		t.Fatalf("expected 35, got %s", val.String())
	}
	if err := c.CheckConstraints(); err != nil {
		t.Fatalf("arithmetic chain should satisfy constraints: %v", err)
	}
}

func TestCircuitBooleanLogic(t *testing.T) {
	// Assert a is boolean, then a * (1 - a) = 0.
	c := NewCircuit()
	a, _ := c.AllocatePrivate(big.NewInt(1))
	c.AssertBool(a)

	// Also test: a * a = a (since a is boolean).
	aSq, _ := c.Mul(a, a)
	c.AssertEqual(a, aSq)

	if err := c.CheckConstraints(); err != nil {
		t.Fatalf("boolean circuit should satisfy: %v", err)
	}
}

func TestCircuitHashAndVerify(t *testing.T) {
	c := NewCircuit()
	input, _ := c.AllocatePublic(big.NewInt(42))
	hash, _ := c.Hash(input)

	// The hash should be a valid field element.
	hashVal := c.GetWitness(hash)
	if hashVal.Sign() < 0 || hashVal.Cmp(c.Field()) >= 0 {
		t.Fatal("hash should be within field")
	}

	numC, numV := c.Synthesize()
	if numC < 1 {
		t.Fatal("hash circuit should have at least 1 constraint")
	}
	if numV < 3 {
		t.Fatalf("expected at least 3 variables, got %d", numV)
	}

	publicInputs := map[Variable]*big.Int{input: big.NewInt(42)}
	valid, err := c.Verify(publicInputs, make([]byte, 64))
	if err != nil {
		t.Fatalf("verification error: %v", err)
	}
	if !valid {
		t.Fatal("hash circuit should verify")
	}
}

func TestLinearConstraint(t *testing.T) {
	c := NewCircuit()
	// 2*a * 3*b = 6*c where a=5, b=7, c=35.
	a, _ := c.AllocatePrivate(big.NewInt(5))
	b, _ := c.AllocatePrivate(big.NewInt(7))
	cv, _ := c.AllocatePrivate(big.NewInt(35))

	err := c.AddLinearConstraint(
		LinearCombination{{Coeff: big.NewInt(2), Var: a}},
		LinearCombination{{Coeff: big.NewInt(3), Var: b}},
		LinearCombination{{Coeff: big.NewInt(6), Var: cv}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2*5 * 3*7 = 10 * 21 = 210 = 6 * 35 = 210. Should satisfy.
	if err := c.CheckConstraints(); err != nil {
		t.Fatalf("linear constraint should satisfy: %v", err)
	}
}

func TestLinearConstraintAfterFinalize(t *testing.T) {
	c := NewCircuit()
	c.Synthesize()
	err := c.AddLinearConstraint(nil, nil, nil)
	if err != ErrCircuitFinalized {
		t.Fatalf("expected ErrCircuitFinalized, got %v", err)
	}
}

// --- Benchmark ---

func BenchmarkCircuitMul100(b *testing.B) {
	for i := 0; i < b.N; i++ {
		c := NewCircuit()
		prev, _ := c.AllocatePrivate(big.NewInt(2))
		for j := 0; j < 100; j++ {
			next, _ := c.AllocatePrivate(big.NewInt(2))
			prev, _ = c.Mul(prev, next)
		}
		c.Synthesize()
	}
}

func BenchmarkCheckConstraints(b *testing.B) {
	c := NewCircuit()
	for j := 0; j < 50; j++ {
		a, _ := c.AllocatePrivate(big.NewInt(int64(j + 1)))
		bv, _ := c.AllocatePrivate(big.NewInt(int64(j + 2)))
		c.Mul(a, bv)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.CheckConstraints()
	}
}
