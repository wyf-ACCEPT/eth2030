// r1cs_solver.go implements an R1CS (Rank-1 Constraint System) solver for
// the zkVM package. R1CS is the standard arithmetic constraint system used
// by ZK-SNARK backends (Groth16, PLONK, Marlin). This solver supports:
//   - Sparse constraint representation (A, B, C vectors of terms)
//   - Forward witness solving from public inputs
//   - Full constraint satisfaction verification
//   - Convenience gates: multiplication, addition, constant assignment
//
// Part of the K+/M+ roadmap for canonical guest verification and mandatory proofs.
package zkvm

import (
	"errors"
	"math/big"
)

// R1CS solver errors.
var (
	ErrR1CSNoVariables       = errors.New("r1cs: number of variables must be positive")
	ErrR1CSPublicExceedsVars = errors.New("r1cs: public inputs exceed total variables")
	ErrR1CSWitnessSize       = errors.New("r1cs: witness size does not match variable count")
	ErrR1CSConstraintFailed  = errors.New("r1cs: constraint not satisfied")
	ErrR1CSNoConstraints     = errors.New("r1cs: no constraints defined")
	ErrR1CSIndexOOB          = errors.New("r1cs: variable index out of bounds")
	ErrR1CSPublicInputSize   = errors.New("r1cs: public input count mismatch")
	ErrR1CSSolveUnsupported  = errors.New("r1cs: cannot solve for all unknowns")
)

// SparseTerm is a single (coefficient, variable_index) pair in a sparse
// linear combination. The linear combination evaluates to
// sum(coefficient_i * witness[index_i]).
type SparseTerm struct {
	Index       int
	Coefficient int64
}

// SparseConstraint represents a single R1CS constraint in the form:
//
//	<A, w> * <B, w> = <C, w>
//
// where A, B, C are sparse linear combinations (vectors of SparseTerm)
// and w is the witness vector.
type SparseConstraint struct {
	A []SparseTerm
	B []SparseTerm
	C []SparseTerm
}

// R1CSSystem holds a complete R1CS constraint system.
type R1CSSystem struct {
	// Constraints is the ordered list of R1CS constraints.
	Constraints []SparseConstraint

	// NumVariables is the total number of witness variables.
	// Variable 0 is reserved for the constant 1.
	NumVariables int

	// NumPublic is the number of public input variables.
	// Public inputs occupy indices 1..NumPublic (index 0 is the constant 1).
	NumPublic int

	// field is the prime field modulus for arithmetic. If nil, plain int64 is used.
	field *big.Int
}

// NewR1CSSystem creates a new R1CS system with the given dimensions.
// Variable 0 is always the constant 1.
func NewR1CSSystem(numVars, numPublic int) (*R1CSSystem, error) {
	if numVars <= 0 {
		return nil, ErrR1CSNoVariables
	}
	if numPublic < 0 || numPublic >= numVars {
		return nil, ErrR1CSPublicExceedsVars
	}
	return &R1CSSystem{
		Constraints:  make([]SparseConstraint, 0, 16),
		NumVariables: numVars,
		NumPublic:    numPublic,
	}, nil
}

// NewR1CSSystemWithField creates an R1CS system over a prime field.
func NewR1CSSystemWithField(numVars, numPublic int, field *big.Int) (*R1CSSystem, error) {
	sys, err := NewR1CSSystem(numVars, numPublic)
	if err != nil {
		return nil, err
	}
	if field != nil && field.Sign() > 0 {
		sys.field = new(big.Int).Set(field)
	}
	return sys, nil
}

// AddConstraint adds a raw R1CS constraint: <A, w> * <B, w> = <C, w>.
func (sys *R1CSSystem) AddConstraint(a, b, c []SparseTerm) error {
	if err := sys.validateSparseTerms(a); err != nil {
		return err
	}
	if err := sys.validateSparseTerms(b); err != nil {
		return err
	}
	if err := sys.validateSparseTerms(c); err != nil {
		return err
	}
	sys.Constraints = append(sys.Constraints, SparseConstraint{
		A: copySparseTerms(a),
		B: copySparseTerms(b),
		C: copySparseTerms(c),
	})
	return nil
}

// AddMultiplicationGate adds a constraint: w[left] * w[right] = w[output].
func (sys *R1CSSystem) AddMultiplicationGate(left, right, output int) error {
	if left < 0 || left >= sys.NumVariables {
		return ErrR1CSIndexOOB
	}
	if right < 0 || right >= sys.NumVariables {
		return ErrR1CSIndexOOB
	}
	if output < 0 || output >= sys.NumVariables {
		return ErrR1CSIndexOOB
	}
	a := []SparseTerm{{Index: left, Coefficient: 1}}
	b := []SparseTerm{{Index: right, Coefficient: 1}}
	c := []SparseTerm{{Index: output, Coefficient: 1}}
	sys.Constraints = append(sys.Constraints, SparseConstraint{A: a, B: b, C: c})
	return nil
}

// AddAdditionGate adds constraints representing w[a] + w[b] = w[result].
// Encoded as: (w[a] + w[b]) * 1 = w[result].
func (sys *R1CSSystem) AddAdditionGate(a, b, result int) error {
	if a < 0 || a >= sys.NumVariables {
		return ErrR1CSIndexOOB
	}
	if b < 0 || b >= sys.NumVariables {
		return ErrR1CSIndexOOB
	}
	if result < 0 || result >= sys.NumVariables {
		return ErrR1CSIndexOOB
	}
	aTerms := []SparseTerm{{Index: a, Coefficient: 1}, {Index: b, Coefficient: 1}}
	bTerms := []SparseTerm{{Index: 0, Coefficient: 1}} // constant 1
	cTerms := []SparseTerm{{Index: result, Coefficient: 1}}
	sys.Constraints = append(sys.Constraints, SparseConstraint{A: aTerms, B: bTerms, C: cTerms})
	return nil
}

// AddConstantGate adds a constraint: w[variable] = value.
// Encoded as: w[variable] * 1 = value * 1.
func (sys *R1CSSystem) AddConstantGate(variable int, value int64) error {
	if variable < 0 || variable >= sys.NumVariables {
		return ErrR1CSIndexOOB
	}
	aTerms := []SparseTerm{{Index: variable, Coefficient: 1}}
	bTerms := []SparseTerm{{Index: 0, Coefficient: 1}} // constant 1
	cTerms := []SparseTerm{{Index: 0, Coefficient: value}}
	sys.Constraints = append(sys.Constraints, SparseConstraint{A: aTerms, B: bTerms, C: cTerms})
	return nil
}

// ConstraintCount returns the number of constraints.
func (sys *R1CSSystem) ConstraintCount() int {
	return len(sys.Constraints)
}

// EvalLinearCombination evaluates a sparse linear combination against a
// witness vector: sum(coefficient_i * witness[index_i]).
func (sys *R1CSSystem) EvalLinearCombination(terms []SparseTerm, witness []int64) int64 {
	var result int64
	for _, term := range terms {
		if term.Index >= 0 && term.Index < len(witness) {
			result += term.Coefficient * witness[term.Index]
		}
	}
	return result
}

// evalLinearCombinationBig evaluates using big.Int modular arithmetic.
func (sys *R1CSSystem) evalLinearCombinationBig(terms []SparseTerm, witness []*big.Int) *big.Int {
	result := new(big.Int)
	for _, term := range terms {
		if term.Index >= 0 && term.Index < len(witness) {
			coeff := big.NewInt(term.Coefficient)
			prod := new(big.Int).Mul(coeff, witness[term.Index])
			result.Add(result, prod)
		}
	}
	if sys.field != nil {
		result.Mod(result, sys.field)
	}
	return result
}

// Verify checks that all constraints are satisfied by the given witness.
// The witness must have exactly NumVariables elements. witness[0] must be 1.
func (sys *R1CSSystem) Verify(witness []int64) bool {
	if len(witness) != sys.NumVariables {
		return false
	}
	if witness[0] != 1 {
		return false
	}

	if sys.field != nil {
		return sys.verifyBig(witness)
	}

	for _, c := range sys.Constraints {
		aVal := sys.EvalLinearCombination(c.A, witness)
		bVal := sys.EvalLinearCombination(c.B, witness)
		cVal := sys.EvalLinearCombination(c.C, witness)
		if aVal*bVal != cVal {
			return false
		}
	}
	return true
}

// verifyBig verifies constraints using modular arithmetic.
func (sys *R1CSSystem) verifyBig(witness []int64) bool {
	bigWitness := make([]*big.Int, len(witness))
	for i, v := range witness {
		bigWitness[i] = big.NewInt(v)
	}

	for _, c := range sys.Constraints {
		aVal := sys.evalLinearCombinationBig(c.A, bigWitness)
		bVal := sys.evalLinearCombinationBig(c.B, bigWitness)
		cVal := sys.evalLinearCombinationBig(c.C, bigWitness)

		ab := new(big.Int).Mul(aVal, bVal)
		if sys.field != nil {
			ab.Mod(ab, sys.field)
			cVal.Mod(cVal, sys.field)
		}
		if ab.Cmp(cVal) != 0 {
			return false
		}
	}
	return true
}

// Solve attempts to compute a complete witness from public inputs.
// publicInputs should have exactly NumPublic elements (for variables 1..NumPublic).
// The solver fills variable 0 with 1, sets public inputs, then attempts
// forward propagation through constraints.
func (sys *R1CSSystem) Solve(publicInputs []int64) ([]int64, error) {
	if len(publicInputs) != sys.NumPublic {
		return nil, ErrR1CSPublicInputSize
	}
	if len(sys.Constraints) == 0 {
		return nil, ErrR1CSNoConstraints
	}

	witness := make([]int64, sys.NumVariables)
	known := make([]bool, sys.NumVariables)

	// Variable 0 is the constant 1.
	witness[0] = 1
	known[0] = true

	// Set public inputs at positions 1..NumPublic.
	for i, val := range publicInputs {
		witness[i+1] = val
		known[i+1] = true
	}

	// Forward propagation: iterate over constraints, attempting to solve
	// for unknowns. Repeat until no progress is made.
	changed := true
	for changed {
		changed = false
		for _, c := range sys.Constraints {
			if sys.trySolveConstraint(c, witness, known) {
				changed = true
			}
		}
	}

	// Check if all variables are determined.
	for i := range known {
		if !known[i] {
			return nil, ErrR1CSSolveUnsupported
		}
	}

	// Final verification.
	if !sys.Verify(witness) {
		return nil, ErrR1CSConstraintFailed
	}

	return witness, nil
}

// trySolveConstraint attempts to solve a single constraint for one unknown.
func (sys *R1CSSystem) trySolveConstraint(c SparseConstraint, witness []int64, known []bool) bool {
	// Strategy 1: If A and B are fully known, solve for one unknown in C.
	if sparseAllKnown(c.A, known) && sparseAllKnown(c.B, known) {
		aVal := sys.EvalLinearCombination(c.A, witness)
		bVal := sys.EvalLinearCombination(c.B, witness)
		product := aVal * bVal

		idx, coeff, kSum, cnt := sparseFindUnknown(c.C, witness, known)
		if cnt == 1 && coeff != 0 {
			val := product - kSum
			if val%coeff == 0 {
				witness[idx] = val / coeff
				known[idx] = true
				return true
			}
		}
	}

	// Strategy 2: If B and C are fully known, solve for one unknown in A.
	if sparseAllKnown(c.B, known) && sparseAllKnown(c.C, known) {
		bVal := sys.EvalLinearCombination(c.B, witness)
		cVal := sys.EvalLinearCombination(c.C, witness)

		if bVal != 0 {
			idx, coeff, kSum, cnt := sparseFindUnknown(c.A, witness, known)
			if cnt == 1 && coeff != 0 {
				val := cVal - kSum*bVal
				denom := bVal * coeff
				if denom != 0 && val%denom == 0 {
					witness[idx] = val / denom
					known[idx] = true
					return true
				}
			}
		}
	}

	// Strategy 3: If A and C are fully known, solve for one unknown in B.
	if sparseAllKnown(c.A, known) && sparseAllKnown(c.C, known) {
		aVal := sys.EvalLinearCombination(c.A, witness)
		cVal := sys.EvalLinearCombination(c.C, witness)

		if aVal != 0 {
			idx, coeff, kSum, cnt := sparseFindUnknown(c.B, witness, known)
			if cnt == 1 && coeff != 0 {
				val := cVal - kSum*aVal
				denom := aVal * coeff
				if denom != 0 && val%denom == 0 {
					witness[idx] = val / denom
					known[idx] = true
					return true
				}
			}
		}
	}

	return false
}

// sparseAllKnown checks if all variables in a sparse LC are known.
func sparseAllKnown(terms []SparseTerm, known []bool) bool {
	for _, t := range terms {
		if t.Index >= 0 && t.Index < len(known) && !known[t.Index] {
			return false
		}
	}
	return true
}

// sparseFindUnknown finds a single unknown in a sparse linear combination.
func sparseFindUnknown(terms []SparseTerm, witness []int64, known []bool) (int, int64, int64, int) {
	unknownIdx := -1
	unknownCoeff := int64(0)
	knownSum := int64(0)
	unknownCount := 0

	for _, t := range terms {
		if t.Index >= 0 && t.Index < len(known) {
			if known[t.Index] {
				knownSum += t.Coefficient * witness[t.Index]
			} else {
				unknownCount++
				unknownIdx = t.Index
				unknownCoeff = t.Coefficient
			}
		}
	}
	return unknownIdx, unknownCoeff, knownSum, unknownCount
}

// --- Helpers ---

func (sys *R1CSSystem) validateSparseTerms(terms []SparseTerm) error {
	for _, t := range terms {
		if t.Index < 0 || t.Index >= sys.NumVariables {
			return ErrR1CSIndexOOB
		}
	}
	return nil
}

func copySparseTerms(terms []SparseTerm) []SparseTerm {
	if len(terms) == 0 {
		return nil
	}
	cp := make([]SparseTerm, len(terms))
	copy(cp, terms)
	return cp
}

// R1CSStats holds statistics about an R1CS system.
type R1CSStats struct {
	NumConstraints  int
	NumVariables    int
	NumPublicInputs int
	NumPrivateWires int
	TotalTerms      int
}

// Stats returns summary statistics for the R1CS system.
func (sys *R1CSSystem) Stats() R1CSStats {
	totalTerms := 0
	for _, c := range sys.Constraints {
		totalTerms += len(c.A) + len(c.B) + len(c.C)
	}
	return R1CSStats{
		NumConstraints:  len(sys.Constraints),
		NumVariables:    sys.NumVariables,
		NumPublicInputs: sys.NumPublic,
		NumPrivateWires: sys.NumVariables - sys.NumPublic - 1,
		TotalTerms:      totalTerms,
	}
}
