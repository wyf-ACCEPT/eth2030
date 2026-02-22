package rollup

import (
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

// mockStateReader returns deterministic state data for a given root.
func mockStateReader(root [32]byte) ([]byte, error) {
	return crypto.Keccak256(root[:]), nil
}

// mockTxExecutor executes a tx by hashing preState with tx data.
func mockTxExecutor(preState [32]byte, tx []byte) ([32]byte, error) {
	return ComputeStateTransition(preState, tx)
}

// mockStateVerifier always returns false (transition is invalid = fraud confirmed).
func mockStateVerifierFraud(preState, postState [32]byte, proof []byte) bool {
	return false
}

// mockStateVerifierValid always returns true (transition is valid = no fraud).
func mockStateVerifierValid(preState, postState [32]byte, proof []byte) bool {
	return true
}

func TestNewFraudProofGenerator(t *testing.T) {
	g, err := NewFraudProofGenerator(mockStateReader, mockTxExecutor)
	if err != nil {
		t.Fatalf("should create generator: %v", err)
	}
	if g == nil {
		t.Fatal("generator should not be nil")
	}
}

func TestNewFraudProofGeneratorNilReader(t *testing.T) {
	_, err := NewFraudProofGenerator(nil, mockTxExecutor)
	if err != ErrFraudNilStateReader {
		t.Fatalf("expected ErrFraudNilStateReader, got %v", err)
	}
}

func TestNewFraudProofGeneratorNilExecutor(t *testing.T) {
	_, err := NewFraudProofGenerator(mockStateReader, nil)
	if err != ErrFraudNilTxExecutor {
		t.Fatalf("expected ErrFraudNilTxExecutor, got %v", err)
	}
}

func TestGenerateStateRootProof(t *testing.T) {
	g, _ := NewFraudProofGenerator(mockStateReader, mockTxExecutor)

	expected := [32]byte{0x01, 0x02, 0x03}
	actual := [32]byte{0x04, 0x05, 0x06}

	proof, err := g.GenerateStateRootProof(100, expected, actual)
	if err != nil {
		t.Fatalf("should generate proof: %v", err)
	}
	if proof.Type != InvalidStateRoot {
		t.Fatalf("expected InvalidStateRoot type, got %d", proof.Type)
	}
	if proof.BlockNumber != 100 {
		t.Fatalf("expected block 100, got %d", proof.BlockNumber)
	}
	if len(proof.Proof) == 0 {
		t.Fatal("proof data should not be empty")
	}
}

func TestGenerateStateRootProofErrors(t *testing.T) {
	g, _ := NewFraudProofGenerator(mockStateReader, mockTxExecutor)

	// Zero block number.
	_, err := g.GenerateStateRootProof(0, [32]byte{1}, [32]byte{2})
	if err != ErrFraudBlockNumberZero {
		t.Fatalf("expected ErrFraudBlockNumberZero, got %v", err)
	}

	// Zero expected root.
	_, err = g.GenerateStateRootProof(1, [32]byte{}, [32]byte{2})
	if err != ErrFraudProofPreStateZero {
		t.Fatalf("expected ErrFraudProofPreStateZero, got %v", err)
	}

	// Zero actual root.
	_, err = g.GenerateStateRootProof(1, [32]byte{1}, [32]byte{})
	if err != ErrFraudProofPostStateZero {
		t.Fatalf("expected ErrFraudProofPostStateZero, got %v", err)
	}

	// Matching roots (no fraud).
	same := [32]byte{0x01}
	_, err = g.GenerateStateRootProof(1, same, same)
	if err != ErrFraudProofRootsMatch {
		t.Fatalf("expected ErrFraudProofRootsMatch, got %v", err)
	}
}

func TestGenerateSingleStepProof(t *testing.T) {
	g, _ := NewFraudProofGenerator(mockStateReader, mockTxExecutor)

	preState := [32]byte{0x01}
	txData := []byte{0xaa, 0xbb, 0xcc}

	// Compute the correct post-state.
	correctPost, _ := ComputeStateTransition(preState, txData)

	// Claim a wrong post-state.
	wrongPost := [32]byte{0xff}

	proof, err := g.GenerateSingleStepProof(50, 3, preState, wrongPost, txData)
	if err != nil {
		t.Fatalf("should generate single step proof: %v", err)
	}
	if proof.StepIndex != 3 {
		t.Fatalf("expected step index 3, got %d", proof.StepIndex)
	}
	if proof.ExpectedRoot != correctPost {
		t.Fatal("expected root should match computed post-state")
	}
}

func TestGenerateSingleStepProofNoFraud(t *testing.T) {
	g, _ := NewFraudProofGenerator(mockStateReader, mockTxExecutor)

	preState := [32]byte{0x01}
	txData := []byte{0xaa, 0xbb}
	correctPost, _ := ComputeStateTransition(preState, txData)

	_, err := g.GenerateSingleStepProof(50, 0, preState, correctPost, txData)
	if err != ErrFraudProofRootsMatch {
		t.Fatalf("expected ErrFraudProofRootsMatch, got %v", err)
	}
}

func TestGenerateSingleStepProofErrors(t *testing.T) {
	g, _ := NewFraudProofGenerator(mockStateReader, mockTxExecutor)

	_, err := g.GenerateSingleStepProof(0, 0, [32]byte{1}, [32]byte{2}, []byte{0x01})
	if err != ErrFraudBlockNumberZero {
		t.Fatalf("expected ErrFraudBlockNumberZero, got %v", err)
	}

	_, err = g.GenerateSingleStepProof(1, 0, [32]byte{1}, [32]byte{2}, nil)
	if err != ErrFraudTxEmpty {
		t.Fatalf("expected ErrFraudTxEmpty, got %v", err)
	}
}

func TestVerifyFraudProofValid(t *testing.T) {
	g, _ := NewFraudProofGenerator(mockStateReader, mockTxExecutor)
	v, _ := NewFraudProofVerifier(mockStateVerifierFraud)

	expected := [32]byte{0x01}
	actual := [32]byte{0x02}
	proof, err := g.GenerateStateRootProof(10, expected, actual)
	if err != nil {
		t.Fatalf("should generate proof: %v", err)
	}

	valid, err := v.VerifyFraudProof(proof)
	if err != nil {
		t.Fatalf("verification should not error: %v", err)
	}
	if !valid {
		t.Fatal("fraud proof should be valid (fraud confirmed)")
	}
}

func TestVerifyFraudProofNoFraud(t *testing.T) {
	g, _ := NewFraudProofGenerator(mockStateReader, mockTxExecutor)
	v, _ := NewFraudProofVerifier(mockStateVerifierValid)

	expected := [32]byte{0x01}
	actual := [32]byte{0x02}
	proof, _ := g.GenerateStateRootProof(10, expected, actual)

	valid, err := v.VerifyFraudProof(proof)
	if err != nil {
		t.Fatalf("verification should not error: %v", err)
	}
	if valid {
		t.Fatal("fraud proof should be invalid (no fraud)")
	}
}

func TestVerifyFraudProofNil(t *testing.T) {
	v, _ := NewFraudProofVerifier(mockStateVerifierFraud)
	_, err := v.VerifyFraudProof(nil)
	if err != ErrFraudProofNil {
		t.Fatalf("expected ErrFraudProofNil, got %v", err)
	}
}

func TestVerifyFraudProofEmptyData(t *testing.T) {
	v, _ := NewFraudProofVerifier(mockStateVerifierFraud)
	proof := &FraudProof{
		Type:          InvalidStateRoot,
		PreStateRoot:  [32]byte{0x01},
		PostStateRoot: [32]byte{0x02},
	}
	_, err := v.VerifyFraudProof(proof)
	if err != ErrFraudProofDataEmpty {
		t.Fatalf("expected ErrFraudProofDataEmpty, got %v", err)
	}
}

func TestVerifyFraudProofUnknownType(t *testing.T) {
	v, _ := NewFraudProofVerifier(mockStateVerifierFraud)
	proof := &FraudProof{
		Type:          FraudProofType(99),
		PreStateRoot:  [32]byte{0x01},
		PostStateRoot: [32]byte{0x02},
		Proof:         make([]byte, 128),
	}
	_, err := v.VerifyFraudProof(proof)
	if err != ErrFraudProofTypeUnknown {
		t.Fatalf("expected ErrFraudProofTypeUnknown, got %v", err)
	}
}

func TestNewFraudProofVerifierNil(t *testing.T) {
	_, err := NewFraudProofVerifier(nil)
	if err != ErrFraudNilStateVerifier {
		t.Fatalf("expected ErrFraudNilStateVerifier, got %v", err)
	}
}

func TestComputeStateTransition(t *testing.T) {
	preState := [32]byte{0x01, 0x02}
	tx := []byte{0xaa, 0xbb, 0xcc}

	result, err := ComputeStateTransition(preState, tx)
	if err != nil {
		t.Fatalf("should compute: %v", err)
	}
	if result == ([32]byte{}) {
		t.Fatal("result should not be zero")
	}

	// Deterministic.
	result2, _ := ComputeStateTransition(preState, tx)
	if result != result2 {
		t.Fatal("should be deterministic")
	}
}

func TestComputeStateTransitionErrors(t *testing.T) {
	_, err := ComputeStateTransition([32]byte{}, []byte{0x01})
	if err != ErrFraudProofPreStateZero {
		t.Fatalf("expected ErrFraudProofPreStateZero, got %v", err)
	}

	_, err = ComputeStateTransition([32]byte{0x01}, nil)
	if err != ErrFraudTxEmpty {
		t.Fatalf("expected ErrFraudTxEmpty, got %v", err)
	}
}

func TestInteractiveVerificationBisection(t *testing.T) {
	iv := NewInteractiveVerification(100, 0, 16)

	if iv.IsConverged() {
		t.Fatal("should not be converged initially")
	}
	if iv.BlockNumber() != 100 {
		t.Fatalf("expected block 100, got %d", iv.BlockNumber())
	}

	// First bisection: disagreement at midpoint.
	start, end, err := iv.BisectionStep(
		[32]byte{0x01}, // claimer
		[32]byte{0x02}, // challenger
	)
	if err != nil {
		t.Fatalf("first bisection should succeed: %v", err)
	}
	if start != 0 || end != 8 {
		t.Fatalf("expected range [0, 8], got [%d, %d]", start, end)
	}

	// Second bisection: agreement at midpoint.
	start, end, err = iv.BisectionStep(
		[32]byte{0xaa}, // same roots = agree
		[32]byte{0xaa},
	)
	if err != nil {
		t.Fatalf("second bisection should succeed: %v", err)
	}
	if start != 4 || end != 8 {
		t.Fatalf("expected range [4, 8], got [%d, %d]", start, end)
	}

	// Continue bisecting until convergence.
	_, _, err = iv.BisectionStep([32]byte{0xbb}, [32]byte{0xcc})
	if err != nil {
		t.Fatalf("third bisection should succeed: %v", err)
	}
	// [4, 6] now

	_, _, err = iv.BisectionStep([32]byte{0xdd}, [32]byte{0xee})
	// Should converge at [4, 5] -> disputed step = 4
	if err != nil {
		t.Fatalf("fourth bisection should succeed: %v", err)
	}

	if !iv.IsConverged() {
		t.Fatal("should be converged after narrowing to single step")
	}
	if iv.DisputedStep() != 4 {
		t.Fatalf("expected disputed step 4, got %d", iv.DisputedStep())
	}
}

func TestInteractiveVerificationAlreadyConverged(t *testing.T) {
	iv := NewInteractiveVerification(1, 0, 2)
	// First step should converge immediately.
	_, _, err := iv.BisectionStep([32]byte{0x01}, [32]byte{0x02})
	if err != nil {
		t.Fatalf("should succeed: %v", err)
	}

	// Second step should fail.
	_, _, err = iv.BisectionStep([32]byte{0x01}, [32]byte{0x02})
	if err != ErrBisectionConverged {
		t.Fatalf("expected ErrBisectionConverged, got %v", err)
	}
}

func TestGenerateBisectionProof(t *testing.T) {
	iv := NewInteractiveVerification(50, 0, 2)
	iv.BisectionStep([32]byte{0x01}, [32]byte{0x02})

	proof, err := iv.GenerateBisectionProof()
	if err != nil {
		t.Fatalf("should generate bisection proof: %v", err)
	}
	if proof.BlockNumber != 50 {
		t.Fatalf("expected block 50, got %d", proof.BlockNumber)
	}
	if len(proof.Proof) == 0 {
		t.Fatal("proof data should not be empty")
	}
}

func TestGenerateBisectionProofNotConverged(t *testing.T) {
	iv := NewInteractiveVerification(1, 0, 100)
	_, err := iv.GenerateBisectionProof()
	if err == nil {
		t.Fatal("should fail when not converged")
	}
}

func TestComputeProofHash(t *testing.T) {
	proof := &FraudProof{
		Type:          InvalidStateRoot,
		BlockNumber:   10,
		PreStateRoot:  [32]byte{0x01},
		PostStateRoot: [32]byte{0x02},
		Proof:         []byte{0xaa, 0xbb},
	}
	hash := computeProofHash(proof)
	if hash.IsZero() {
		t.Fatal("proof hash should not be zero")
	}

	// Deterministic.
	hash2 := computeProofHash(proof)
	if hash != hash2 {
		t.Fatal("proof hash should be deterministic")
	}

	// Nil proof.
	nilHash := computeProofHash(nil)
	if !nilHash.IsZero() {
		t.Fatal("nil proof hash should be zero")
	}
}
