package zkvm

import (
	"math/big"
	"testing"
)

// --- NewR1CSSystem tests ---

func TestNewR1CSSystem(t *testing.T) {
	sys, err := NewR1CSSystem(10, 3)
	if err != nil {
		t.Fatalf("NewR1CSSystem: %v", err)
	}
	if sys.NumVariables != 10 {
		t.Errorf("expected 10 variables, got %d", sys.NumVariables)
	}
	if sys.NumPublic != 3 {
		t.Errorf("expected 3 public, got %d", sys.NumPublic)
	}
	if sys.ConstraintCount() != 0 {
		t.Errorf("expected 0 constraints, got %d", sys.ConstraintCount())
	}
}

func TestNewR1CSSystemInvalidVars(t *testing.T) {
	_, err := NewR1CSSystem(0, 0)
	if err != ErrR1CSNoVariables {
		t.Errorf("expected ErrR1CSNoVariables, got %v", err)
	}
}

func TestNewR1CSSystemPublicExceedsVars(t *testing.T) {
	_, err := NewR1CSSystem(5, 5)
	if err != ErrR1CSPublicExceedsVars {
		t.Errorf("expected ErrR1CSPublicExceedsVars, got %v", err)
	}
}

// --- Constraint addition tests ---

func TestR1CSAddConstraint(t *testing.T) {
	sys, _ := NewR1CSSystem(5, 1)
	a := []SparseTerm{{Index: 1, Coefficient: 1}}
	b := []SparseTerm{{Index: 2, Coefficient: 1}}
	c := []SparseTerm{{Index: 3, Coefficient: 1}}

	err := sys.AddConstraint(a, b, c)
	if err != nil {
		t.Fatalf("AddConstraint: %v", err)
	}
	if sys.ConstraintCount() != 1 {
		t.Errorf("expected 1 constraint, got %d", sys.ConstraintCount())
	}
}

func TestR1CSAddConstraintOOB(t *testing.T) {
	sys, _ := NewR1CSSystem(5, 1)
	a := []SparseTerm{{Index: 10, Coefficient: 1}} // out of bounds

	err := sys.AddConstraint(a, nil, nil)
	if err != ErrR1CSIndexOOB {
		t.Errorf("expected ErrR1CSIndexOOB, got %v", err)
	}
}

func TestR1CSAddMultiplicationGate(t *testing.T) {
	sys, _ := NewR1CSSystem(5, 1)
	err := sys.AddMultiplicationGate(1, 2, 3)
	if err != nil {
		t.Fatalf("AddMultiplicationGate: %v", err)
	}
	if sys.ConstraintCount() != 1 {
		t.Errorf("expected 1 constraint, got %d", sys.ConstraintCount())
	}
}

func TestR1CSAddMultiplicationGateOOB(t *testing.T) {
	sys, _ := NewR1CSSystem(5, 1)
	err := sys.AddMultiplicationGate(-1, 2, 3)
	if err != ErrR1CSIndexOOB {
		t.Errorf("expected ErrR1CSIndexOOB, got %v", err)
	}
}

func TestR1CSAddAdditionGate(t *testing.T) {
	sys, _ := NewR1CSSystem(5, 1)
	err := sys.AddAdditionGate(1, 2, 3)
	if err != nil {
		t.Fatalf("AddAdditionGate: %v", err)
	}
}

func TestR1CSAddAdditionGateOOB(t *testing.T) {
	sys, _ := NewR1CSSystem(5, 1)
	err := sys.AddAdditionGate(1, 2, 10)
	if err != ErrR1CSIndexOOB {
		t.Errorf("expected ErrR1CSIndexOOB, got %v", err)
	}
}

func TestR1CSAddConstantGate(t *testing.T) {
	sys, _ := NewR1CSSystem(5, 1)
	err := sys.AddConstantGate(1, 42)
	if err != nil {
		t.Fatalf("AddConstantGate: %v", err)
	}
}

func TestR1CSAddConstantGateOOB(t *testing.T) {
	sys, _ := NewR1CSSystem(5, 1)
	err := sys.AddConstantGate(5, 42)
	if err != ErrR1CSIndexOOB {
		t.Errorf("expected ErrR1CSIndexOOB, got %v", err)
	}
}

// --- Verify tests ---

// Simple circuit: w[1] * w[2] = w[3]
func TestR1CSVerifySimpleMul(t *testing.T) {
	sys, _ := NewR1CSSystem(4, 1)
	sys.AddMultiplicationGate(1, 2, 3)

	// Witness: [1, 3, 7, 21] -> 3 * 7 = 21
	witness := []int64{1, 3, 7, 21}
	if !sys.Verify(witness) {
		t.Error("valid witness should verify")
	}

	// Bad witness: [1, 3, 7, 20] -> 3 * 7 != 20
	badWitness := []int64{1, 3, 7, 20}
	if sys.Verify(badWitness) {
		t.Error("invalid witness should not verify")
	}
}

// Circuit: w[1] + w[2] = w[3]
func TestR1CSVerifySimpleAdd(t *testing.T) {
	sys, _ := NewR1CSSystem(4, 1)
	sys.AddAdditionGate(1, 2, 3)

	// 5 + 10 = 15
	witness := []int64{1, 5, 10, 15}
	if !sys.Verify(witness) {
		t.Error("valid addition witness should verify")
	}

	// 5 + 10 != 16
	bad := []int64{1, 5, 10, 16}
	if sys.Verify(bad) {
		t.Error("invalid addition witness should not verify")
	}
}

// Circuit: w[1] = 42 (constant gate)
func TestR1CSVerifyConstant(t *testing.T) {
	sys, _ := NewR1CSSystem(4, 1)
	sys.AddConstantGate(1, 42)

	witness := []int64{1, 42, 0, 0}
	if !sys.Verify(witness) {
		t.Error("constant witness should verify")
	}

	bad := []int64{1, 43, 0, 0}
	if sys.Verify(bad) {
		t.Error("wrong constant should not verify")
	}
}

func TestR1CSVerifyWrongSize(t *testing.T) {
	sys, _ := NewR1CSSystem(4, 1)
	sys.AddMultiplicationGate(1, 2, 3)

	if sys.Verify([]int64{1, 3, 7}) { // too short
		t.Error("wrong size witness should not verify")
	}
}

func TestR1CSVerifyBadConstantWire(t *testing.T) {
	sys, _ := NewR1CSSystem(4, 1)
	sys.AddMultiplicationGate(1, 2, 3)

	if sys.Verify([]int64{0, 3, 7, 21}) {
		t.Error("witness[0] != 1 should not verify")
	}
}

// --- EvalLinearCombination tests ---

func TestR1CSEvalLinearCombination(t *testing.T) {
	sys, _ := NewR1CSSystem(5, 1)
	terms := []SparseTerm{
		{Index: 1, Coefficient: 3},
		{Index: 2, Coefficient: 5},
	}
	witness := []int64{1, 10, 20, 0, 0}
	// 3*10 + 5*20 = 130
	got := sys.EvalLinearCombination(terms, witness)
	if got != 130 {
		t.Errorf("expected 130, got %d", got)
	}
}

func TestR1CSEvalLinearCombinationEmpty(t *testing.T) {
	sys, _ := NewR1CSSystem(5, 1)
	witness := []int64{1, 10, 20, 0, 0}
	got := sys.EvalLinearCombination(nil, witness)
	if got != 0 {
		t.Errorf("expected 0 for empty LC, got %d", got)
	}
}

// --- Solve tests ---

func TestR1CSSolveSimpleMul(t *testing.T) {
	// Variables: 0=const(1), 1=x(public), 2=y(private), 3=z(private)
	sys, _ := NewR1CSSystem(4, 1)
	sys.AddConstantGate(2, 7)          // w[2] = 7
	sys.AddMultiplicationGate(1, 2, 3) // w[1] * w[2] = w[3]

	witness, err := sys.Solve([]int64{3})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if witness[0] != 1 {
		t.Errorf("witness[0] should be 1, got %d", witness[0])
	}
	if witness[1] != 3 {
		t.Errorf("witness[1] should be 3, got %d", witness[1])
	}
	if witness[2] != 7 {
		t.Errorf("witness[2] should be 7, got %d", witness[2])
	}
	if witness[3] != 21 {
		t.Errorf("witness[3] should be 21, got %d", witness[3])
	}
}

func TestR1CSSolveSimpleAdd(t *testing.T) {
	sys, _ := NewR1CSSystem(4, 2)
	sys.AddAdditionGate(1, 2, 3) // w[1] + w[2] = w[3]

	witness, err := sys.Solve([]int64{10, 20})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if witness[3] != 30 {
		t.Errorf("expected w[3]=30, got %d", witness[3])
	}
}

func TestR1CSSolveNoConstraints(t *testing.T) {
	sys, _ := NewR1CSSystem(3, 1)
	_, err := sys.Solve([]int64{5})
	if err != ErrR1CSNoConstraints {
		t.Errorf("expected ErrR1CSNoConstraints, got %v", err)
	}
}

func TestR1CSSolveWrongPublicCount(t *testing.T) {
	sys, _ := NewR1CSSystem(4, 2)
	sys.AddAdditionGate(1, 2, 3)
	_, err := sys.Solve([]int64{5})
	if err != ErrR1CSPublicInputSize {
		t.Errorf("expected ErrR1CSPublicInputSize, got %v", err)
	}
}

// --- Multi-constraint circuit ---

// Circuit: x^2 + x + 5 = y
// Variables: 0=1, 1=x(pub), 2=y(pub), 3=x^2(priv), 4=x^2+x(priv)
// Public inputs at indices 1..NumPublic: w[1]=x, w[2]=y.
func TestR1CSSolveQuadratic(t *testing.T) {
	sys, _ := NewR1CSSystem(5, 2)

	// x * x = x_sq (w[1] * w[1] = w[3])
	sys.AddConstraint(
		[]SparseTerm{{Index: 1, Coefficient: 1}},
		[]SparseTerm{{Index: 1, Coefficient: 1}},
		[]SparseTerm{{Index: 3, Coefficient: 1}},
	)
	// (x_sq + x) * 1 = temp (w[3] + w[1]) * 1 = w[4]
	sys.AddConstraint(
		[]SparseTerm{{Index: 3, Coefficient: 1}, {Index: 1, Coefficient: 1}},
		[]SparseTerm{{Index: 0, Coefficient: 1}},
		[]SparseTerm{{Index: 4, Coefficient: 1}},
	)
	// (temp + 5) * 1 = y  (w[4] + 5*w[0]) * 1 = w[2]
	sys.AddConstraint(
		[]SparseTerm{{Index: 4, Coefficient: 1}, {Index: 0, Coefficient: 5}},
		[]SparseTerm{{Index: 0, Coefficient: 1}},
		[]SparseTerm{{Index: 2, Coefficient: 1}},
	)

	// x=3: 9 + 3 + 5 = 17
	witness, err := sys.Solve([]int64{3, 17})
	if err != nil {
		t.Fatalf("Solve quadratic: %v", err)
	}
	if witness[1] != 3 {
		t.Errorf("expected x=3, got %d", witness[1])
	}
	if witness[2] != 17 {
		t.Errorf("expected y=17, got %d", witness[2])
	}
	if witness[3] != 9 {
		t.Errorf("expected x^2=9, got %d", witness[3])
	}
	if witness[4] != 12 {
		t.Errorf("expected x^2+x=12, got %d", witness[4])
	}

	if !sys.Verify(witness) {
		t.Error("solved witness should verify")
	}
}

// --- Stats test ---

func TestR1CSStats(t *testing.T) {
	sys, _ := NewR1CSSystem(10, 3)
	sys.AddMultiplicationGate(1, 2, 3)
	sys.AddAdditionGate(3, 4, 5)
	sys.AddConstantGate(6, 99)

	stats := sys.Stats()
	if stats.NumConstraints != 3 {
		t.Errorf("expected 3 constraints, got %d", stats.NumConstraints)
	}
	if stats.NumVariables != 10 {
		t.Errorf("expected 10 variables, got %d", stats.NumVariables)
	}
	if stats.NumPublicInputs != 3 {
		t.Errorf("expected 3 public inputs, got %d", stats.NumPublicInputs)
	}
	if stats.NumPrivateWires != 6 {
		t.Errorf("expected 6 private wires, got %d", stats.NumPrivateWires)
	}
	if stats.TotalTerms == 0 {
		t.Error("expected non-zero total terms")
	}
}

// --- Field arithmetic test ---

func TestR1CSWithField(t *testing.T) {
	field := big.NewInt(101) // small prime
	sys, err := NewR1CSSystemWithField(4, 1, field)
	if err != nil {
		t.Fatalf("NewR1CSSystemWithField: %v", err)
	}
	if sys.field == nil {
		t.Fatal("field should be set")
	}

	// w[1] * w[2] = w[3] (mod 101)
	sys.AddMultiplicationGate(1, 2, 3)

	// 50 * 3 = 150, but 150 mod 101 = 49
	witness := []int64{1, 50, 3, 49}
	if !sys.Verify(witness) {
		t.Error("modular witness should verify")
	}

	// 50 * 3 mod 101 = 49, so w[3]=50 should NOT verify (50 != 49 mod 101)
	bad := []int64{1, 50, 3, 50}
	if sys.Verify(bad) {
		t.Error("w[3]=50 should not verify (50*3 mod 101 = 49, not 50)")
	}
}

// --- Chained gates test ---

func TestR1CSVerifyChainedGates(t *testing.T) {
	// (a + b) * c = d
	// Variables: 0=1, 1=a, 2=b, 3=c, 4=a+b, 5=d
	sys, _ := NewR1CSSystem(6, 3)
	sys.AddAdditionGate(1, 2, 4)       // a + b = temp
	sys.AddMultiplicationGate(4, 3, 5) // temp * c = d

	// a=2, b=3, c=4 -> (2+3)*4 = 20
	witness := []int64{1, 2, 3, 4, 5, 20}
	if !sys.Verify(witness) {
		t.Error("chained gates witness should verify")
	}
}
