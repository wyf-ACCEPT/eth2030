package crypto

import (
	"math/big"
	"testing"
)

func TestDefaultVDFParams(t *testing.T) {
	params := DefaultVDFParams()
	if params.T != 1<<20 {
		t.Errorf("expected T=2^20, got %d", params.T)
	}
	if params.Lambda != 128 {
		t.Errorf("expected Lambda=128, got %d", params.Lambda)
	}
}

func TestWesolowskiVDF_EvaluateAndVerify(t *testing.T) {
	// Use small parameters for testing speed.
	params := &VDFParams{T: 10, Lambda: 64}
	vdf := NewWesolowskiVDF(params)

	input := []byte("test input for vdf")
	proof, err := vdf.Evaluate(input, params.T)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if proof.Iterations != params.T {
		t.Errorf("expected iterations=%d, got %d", params.T, proof.Iterations)
	}
	if len(proof.Output) == 0 {
		t.Fatal("output is empty")
	}
	if len(proof.Proof) == 0 {
		t.Fatal("proof is empty")
	}

	// Verify the proof.
	if !vdf.Verify(proof) {
		t.Fatal("valid proof failed verification")
	}
}

func TestWesolowskiVDF_VerifyRejectsTampered(t *testing.T) {
	params := &VDFParams{T: 8, Lambda: 64}
	vdf := NewWesolowskiVDF(params)

	input := []byte("vdf tamper test")
	proof, err := vdf.Evaluate(input, params.T)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	// Tamper with the output.
	tampered := &VDFProof{
		Input:      proof.Input,
		Output:     append([]byte{0xff}, proof.Output[1:]...),
		Proof:      proof.Proof,
		Iterations: proof.Iterations,
	}
	if vdf.Verify(tampered) {
		t.Fatal("tampered output should fail verification")
	}

	// Tamper with the proof.
	tampered2 := &VDFProof{
		Input:      proof.Input,
		Output:     proof.Output,
		Proof:      append([]byte{0xff}, proof.Proof[1:]...),
		Iterations: proof.Iterations,
	}
	if vdf.Verify(tampered2) {
		t.Fatal("tampered proof should fail verification")
	}
}

func TestWesolowskiVDF_NilInput(t *testing.T) {
	params := &VDFParams{T: 5, Lambda: 64}
	vdf := NewWesolowskiVDF(params)

	_, err := vdf.Evaluate(nil, 5)
	if err != errVDFNilInput {
		t.Fatalf("expected errVDFNilInput, got %v", err)
	}

	_, err = vdf.Evaluate([]byte{}, 5)
	if err != errVDFNilInput {
		t.Fatalf("expected errVDFNilInput for empty input, got %v", err)
	}
}

func TestWesolowskiVDF_ZeroIterations(t *testing.T) {
	params := &VDFParams{T: 5, Lambda: 64}
	vdf := NewWesolowskiVDF(params)

	_, err := vdf.Evaluate([]byte("test"), 0)
	if err != errVDFZeroIterations {
		t.Fatalf("expected errVDFZeroIterations, got %v", err)
	}
}

func TestWesolowskiVDF_VerifyNilProof(t *testing.T) {
	params := &VDFParams{T: 5, Lambda: 64}
	vdf := NewWesolowskiVDF(params)

	if vdf.Verify(nil) {
		t.Fatal("nil proof should fail verification")
	}

	if vdf.Verify(&VDFProof{}) {
		t.Fatal("empty proof should fail verification")
	}
}

func TestWesolowskiVDF_Deterministic(t *testing.T) {
	// Same input + modulus should produce the same output.
	n := big.NewInt(0)
	n.SetString("10967535067461", 10) // small composite for testing
	params := &VDFParams{T: 10, Lambda: 64}
	vdf := NewWesolowskiVDFWithModulus(params, n)

	input := []byte("deterministic test")
	proof1, err := vdf.Evaluate(input, params.T)
	if err != nil {
		t.Fatalf("first Evaluate failed: %v", err)
	}

	proof2, err := vdf.Evaluate(input, params.T)
	if err != nil {
		t.Fatalf("second Evaluate failed: %v", err)
	}

	if string(proof1.Output) != string(proof2.Output) {
		t.Fatal("same input should produce same output")
	}
}

func TestWesolowskiVDF_DifferentInputs(t *testing.T) {
	params := &VDFParams{T: 8, Lambda: 64}
	vdf := NewWesolowskiVDF(params)

	proof1, err := vdf.Evaluate([]byte("input A"), params.T)
	if err != nil {
		t.Fatalf("Evaluate A failed: %v", err)
	}
	proof2, err := vdf.Evaluate([]byte("input B"), params.T)
	if err != nil {
		t.Fatalf("Evaluate B failed: %v", err)
	}

	if string(proof1.Output) == string(proof2.Output) {
		t.Fatal("different inputs should produce different outputs")
	}
}

func TestWesolowskiVDF_WithExplicitModulus(t *testing.T) {
	// Use a known small modulus for reproducibility.
	n := new(big.Int).SetInt64(7 * 11) // 77
	params := &VDFParams{T: 5, Lambda: 64}
	vdf := NewWesolowskiVDFWithModulus(params, n)

	proof, err := vdf.Evaluate([]byte{3}, 5)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if !vdf.Verify(proof) {
		t.Fatal("valid proof failed verification with explicit modulus")
	}
}

func TestVDFHashToPrime(t *testing.T) {
	x := big.NewInt(42)
	y := big.NewInt(99)
	l := vdfHashToPrime(x, y)

	if !l.ProbablyPrime(20) {
		t.Fatal("vdfHashToPrime should return a prime")
	}
	if l.Sign() <= 0 {
		t.Fatal("prime should be positive")
	}
}

func TestGenerateVDFModulus(t *testing.T) {
	n := generateVDFModulus(64)
	if n.Sign() <= 0 {
		t.Fatal("modulus should be positive")
	}
	// Modulus should be composite (product of two primes).
	if n.ProbablyPrime(20) {
		t.Fatal("modulus should be composite, not prime")
	}
}
