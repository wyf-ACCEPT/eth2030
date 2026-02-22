package proofs

import (
	"errors"
	"testing"
)

// --- ProofSet and RegisterProof tests ---

func TestRegisterProof_Valid(t *testing.T) {
	ps := NewProofSet()
	err := RegisterProof(ps, BlockProofState, []byte{0x01, 0x02})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ps.Has(BlockProofState) {
		t.Fatal("expected proof set to contain StateProof")
	}
}

func TestRegisterProof_AllTypes(t *testing.T) {
	ps := NewProofSet()
	for _, pt := range AllBlockProofTypes {
		if err := RegisterProof(ps, pt, []byte{0xaa}); err != nil {
			t.Fatalf("failed to register %s: %v", pt, err)
		}
	}
	if ps.Count() != 5 {
		t.Fatalf("expected 5 proof types, got %d", ps.Count())
	}
}

func TestRegisterProof_InvalidType(t *testing.T) {
	ps := NewProofSet()
	err := RegisterProof(ps, "FakeProof", []byte{0x01})
	if !errors.Is(err, ErrInvalidProofType) {
		t.Fatalf("expected ErrInvalidProofType, got %v", err)
	}
}

func TestRegisterProof_DuplicateProof(t *testing.T) {
	ps := NewProofSet()
	RegisterProof(ps, BlockProofState, []byte{0x01})
	err := RegisterProof(ps, BlockProofState, []byte{0x02})
	if !errors.Is(err, ErrDuplicateProof) {
		t.Fatalf("expected ErrDuplicateProof, got %v", err)
	}
}

func TestRegisterProof_EmptyData(t *testing.T) {
	ps := NewProofSet()
	err := RegisterProof(ps, BlockProofState, nil)
	if err != ErrEmptyProofData {
		t.Fatalf("expected ErrEmptyProofData, got %v", err)
	}
	err = RegisterProof(ps, BlockProofState, []byte{})
	if err != ErrEmptyProofData {
		t.Fatalf("expected ErrEmptyProofData for empty slice, got %v", err)
	}
}

func TestRegisterProof_NilProofSet(t *testing.T) {
	err := RegisterProof(nil, BlockProofState, []byte{0x01})
	if err != ErrNilProofSet {
		t.Fatalf("expected ErrNilProofSet, got %v", err)
	}
}

func TestRegisterProof_DataIsCopied(t *testing.T) {
	ps := NewProofSet()
	data := []byte{0x01, 0x02, 0x03}
	RegisterProof(ps, BlockProofState, data)
	// Mutate the original data.
	data[0] = 0xff
	entry := ps.Get(BlockProofState)
	if entry.Data[0] == 0xff {
		t.Fatal("RegisterProof should copy data, not reference original")
	}
}

// --- ProofSet methods ---

func TestProofSet_EmptySet(t *testing.T) {
	ps := NewProofSet()
	if ps.Count() != 0 {
		t.Fatalf("expected empty set, got count %d", ps.Count())
	}
	if ps.Has(BlockProofState) {
		t.Fatal("empty set should not have any proof type")
	}
	if ps.Get(BlockProofState) != nil {
		t.Fatal("Get on empty set should return nil")
	}
	types := ps.Types()
	if len(types) != 0 {
		t.Fatalf("expected 0 types, got %d", len(types))
	}
}

func TestProofSet_Types(t *testing.T) {
	ps := NewProofSet()
	RegisterProof(ps, BlockProofState, []byte{0x01})
	RegisterProof(ps, BlockProofWitness, []byte{0x02})

	types := ps.Types()
	if len(types) != 2 {
		t.Fatalf("expected 2 types, got %d", len(types))
	}
	// Both should be present (order not guaranteed).
	found := map[string]bool{}
	for _, tp := range types {
		found[tp] = true
	}
	if !found[BlockProofState] || !found[BlockProofWitness] {
		t.Fatalf("expected StateProof and WitnessProof, got %v", types)
	}
}

// --- ValidateBlockProofs tests ---

func TestValidateBlockProofs_Valid3of5(t *testing.T) {
	ps := NewProofSet()
	RegisterProof(ps, BlockProofState, []byte{0x01})
	RegisterProof(ps, BlockProofReceipt, []byte{0x02})
	RegisterProof(ps, BlockProofStorage, []byte{0x03})

	valid, err := ValidateBlockProofs(ps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Fatal("3 of 5 proofs should be valid")
	}
}

func TestValidateBlockProofs_Valid5of5(t *testing.T) {
	ps := NewProofSet()
	for _, pt := range AllBlockProofTypes {
		RegisterProof(ps, pt, []byte{0xab})
	}

	valid, err := ValidateBlockProofs(ps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Fatal("5 of 5 proofs should be valid")
	}
}

func TestValidateBlockProofs_Valid4of5(t *testing.T) {
	ps := NewProofSet()
	RegisterProof(ps, BlockProofState, []byte{0x01})
	RegisterProof(ps, BlockProofReceipt, []byte{0x02})
	RegisterProof(ps, BlockProofStorage, []byte{0x03})
	RegisterProof(ps, BlockProofWitness, []byte{0x04})

	valid, err := ValidateBlockProofs(ps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Fatal("4 of 5 proofs should be valid")
	}
}

func TestValidateBlockProofs_Invalid2of5(t *testing.T) {
	ps := NewProofSet()
	RegisterProof(ps, BlockProofState, []byte{0x01})
	RegisterProof(ps, BlockProofReceipt, []byte{0x02})

	valid, err := ValidateBlockProofs(ps)
	if valid {
		t.Fatal("2 of 5 proofs should not be valid")
	}
	if !errors.Is(err, ErrInsufficientProofs) {
		t.Fatalf("expected ErrInsufficientProofs, got %v", err)
	}
}

func TestValidateBlockProofs_Invalid1of5(t *testing.T) {
	ps := NewProofSet()
	RegisterProof(ps, BlockProofExecution, []byte{0x01})

	valid, err := ValidateBlockProofs(ps)
	if valid {
		t.Fatal("1 of 5 proofs should not be valid")
	}
	if !errors.Is(err, ErrInsufficientProofs) {
		t.Fatalf("expected ErrInsufficientProofs, got %v", err)
	}
}

func TestValidateBlockProofs_EmptySet(t *testing.T) {
	ps := NewProofSet()
	valid, err := ValidateBlockProofs(ps)
	if valid {
		t.Fatal("empty set should not be valid")
	}
	if !errors.Is(err, ErrInsufficientProofs) {
		t.Fatalf("expected ErrInsufficientProofs, got %v", err)
	}
}

func TestValidateBlockProofs_NilProofSet(t *testing.T) {
	valid, err := ValidateBlockProofs(nil)
	if valid {
		t.Fatal("nil proof set should not be valid")
	}
	if err != ErrNilProofSet {
		t.Fatalf("expected ErrNilProofSet, got %v", err)
	}
}

func TestValidateBlockProofs_ExactThreshold(t *testing.T) {
	// Test all C(5,3) = 10 combinations of exactly 3 proof types.
	combos := [][3]string{
		{BlockProofState, BlockProofReceipt, BlockProofStorage},
		{BlockProofState, BlockProofReceipt, BlockProofWitness},
		{BlockProofState, BlockProofReceipt, BlockProofExecution},
		{BlockProofState, BlockProofStorage, BlockProofWitness},
		{BlockProofState, BlockProofStorage, BlockProofExecution},
		{BlockProofState, BlockProofWitness, BlockProofExecution},
		{BlockProofReceipt, BlockProofStorage, BlockProofWitness},
		{BlockProofReceipt, BlockProofStorage, BlockProofExecution},
		{BlockProofReceipt, BlockProofWitness, BlockProofExecution},
		{BlockProofStorage, BlockProofWitness, BlockProofExecution},
	}

	for i, combo := range combos {
		ps := NewProofSet()
		for _, pt := range combo {
			RegisterProof(ps, pt, []byte{byte(i + 1)})
		}
		valid, err := ValidateBlockProofs(ps)
		if err != nil {
			t.Fatalf("combo %d: unexpected error: %v", i, err)
		}
		if !valid {
			t.Fatalf("combo %d: exactly 3 proofs should be valid", i)
		}
	}
}

// --- MandatoryProofValidator tests ---

func TestMandatoryProofValidator_CustomThreshold(t *testing.T) {
	v := NewMandatoryProofValidatorWithThreshold(2)
	ps := NewProofSet()
	RegisterProof(ps, BlockProofState, []byte{0x01})
	RegisterProof(ps, BlockProofReceipt, []byte{0x02})

	valid, err := v.Validate(ps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Fatal("2 of 5 should be valid with threshold 2")
	}
}

func TestMandatoryProofValidator_ThresholdClampedHigh(t *testing.T) {
	v := NewMandatoryProofValidatorWithThreshold(99)
	if v.MinRequired != 5 {
		t.Fatalf("expected threshold clamped to 5, got %d", v.MinRequired)
	}
}

func TestMandatoryProofValidator_ThresholdClampedLow(t *testing.T) {
	v := NewMandatoryProofValidatorWithThreshold(-1)
	if v.MinRequired != 0 {
		t.Fatalf("expected threshold clamped to 0, got %d", v.MinRequired)
	}
	// Even an empty set should pass with threshold 0.
	valid, err := v.Validate(NewProofSet())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Fatal("threshold 0 should pass with empty set")
	}
}

func TestMandatoryProofValidator_WeightedScore(t *testing.T) {
	v := NewMandatoryProofValidator()
	// Override weights for testing.
	v.Requirements[0].Weight = 3 // StateProof
	v.Requirements[1].Weight = 2 // ReceiptProof
	v.Requirements[2].Weight = 1 // StorageProof

	ps := NewProofSet()
	RegisterProof(ps, BlockProofState, []byte{0x01})
	RegisterProof(ps, BlockProofReceipt, []byte{0x02})

	score := v.WeightedScore(ps)
	if score != 5 { // 3 + 2
		t.Fatalf("expected weighted score 5, got %d", score)
	}
}

func TestMandatoryProofValidator_WeightedScoreNilSet(t *testing.T) {
	v := NewMandatoryProofValidator()
	score := v.WeightedScore(nil)
	if score != 0 {
		t.Fatalf("expected 0 for nil set, got %d", score)
	}
}

func TestMandatoryProofValidator_MissingTypes(t *testing.T) {
	v := NewMandatoryProofValidator()
	ps := NewProofSet()
	RegisterProof(ps, BlockProofState, []byte{0x01})
	RegisterProof(ps, BlockProofExecution, []byte{0x02})

	missing := v.MissingTypes(ps)
	if len(missing) != 3 {
		t.Fatalf("expected 3 missing types, got %d: %v", len(missing), missing)
	}
	// Verify the missing ones are correct.
	expected := map[string]bool{
		BlockProofReceipt: true,
		BlockProofStorage: true,
		BlockProofWitness: true,
	}
	for _, m := range missing {
		if !expected[m] {
			t.Fatalf("unexpected missing type: %s", m)
		}
	}
}

func TestMandatoryProofValidator_MissingTypesNilSet(t *testing.T) {
	v := NewMandatoryProofValidator()
	missing := v.MissingTypes(nil)
	if len(missing) != 5 {
		t.Fatalf("expected 5 missing types for nil set, got %d", len(missing))
	}
}

func TestMandatoryProofValidator_ValidateNilSet(t *testing.T) {
	v := NewMandatoryProofValidator()
	valid, err := v.Validate(nil)
	if valid {
		t.Fatal("nil set should not validate")
	}
	if err != ErrNilProofSet {
		t.Fatalf("expected ErrNilProofSet, got %v", err)
	}
}

func TestAllBlockProofTypes_Count(t *testing.T) {
	if len(AllBlockProofTypes) != 5 {
		t.Fatalf("expected 5 block proof types, got %d", len(AllBlockProofTypes))
	}
}

func TestIsValidBlockProofType(t *testing.T) {
	for _, pt := range AllBlockProofTypes {
		if !isValidBlockProofType(pt) {
			t.Fatalf("%s should be valid", pt)
		}
	}
	if isValidBlockProofType("NotAProofType") {
		t.Fatal("NotAProofType should not be valid")
	}
	if isValidBlockProofType("") {
		t.Fatal("empty string should not be valid")
	}
}

func TestValidateProofSubmission(t *testing.T) {
	validProver := [32]byte{1, 2, 3}
	// Valid submission.
	if err := ValidateProofSubmission(BlockProofState, []byte{0x01}, validProver); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Empty proof type.
	if err := ValidateProofSubmission("", []byte{0x01}, validProver); err == nil {
		t.Fatal("expected error for empty proof type")
	}

	// Invalid proof type.
	if err := ValidateProofSubmission("FakeProof", []byte{0x01}, validProver); err == nil {
		t.Fatal("expected error for unknown proof type")
	}

	// Empty proof data.
	if err := ValidateProofSubmission(BlockProofState, nil, validProver); err == nil {
		t.Fatal("expected error for empty proof data")
	}

	// Zero prover hash.
	if err := ValidateProofSubmission(BlockProofState, []byte{0x01}, [32]byte{}); err == nil {
		t.Fatal("expected error for zero prover hash")
	}
}
