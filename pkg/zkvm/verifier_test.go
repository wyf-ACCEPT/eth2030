package zkvm

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- VerifyProof function tests ---

func TestVerifyProofValidProof(t *testing.T) {
	vk := &VerificationKey{
		Data:        []byte("vk-data"),
		ProgramHash: types.Hash{0x01},
	}
	proof := &Proof{
		Data:         []byte("proof-data"),
		PublicInputs: []byte("inputs"),
	}

	valid, err := VerifyProof(vk, proof)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Error("expected valid proof")
	}
}

func TestVerifyProofNilVKReturnsError(t *testing.T) {
	proof := &Proof{Data: []byte("d"), PublicInputs: []byte("i")}
	valid, err := VerifyProof(nil, proof)
	if err != ErrNilVerificationKey {
		t.Errorf("expected ErrNilVerificationKey, got %v", err)
	}
	if valid {
		t.Error("expected false for nil VK")
	}
}

func TestVerifyProofNilProofArg(t *testing.T) {
	vk := &VerificationKey{Data: []byte("vk")}
	valid, err := VerifyProof(vk, nil)
	if err != ErrNilProof {
		t.Errorf("expected ErrNilProof, got %v", err)
	}
	if valid {
		t.Error("expected false for nil proof")
	}
}

func TestVerifyProofEmptyVKData(t *testing.T) {
	vk := &VerificationKey{Data: []byte{}}
	proof := &Proof{Data: []byte("proof"), PublicInputs: []byte("i")}
	valid, err := VerifyProof(vk, proof)
	if err != ErrEmptyVKData {
		t.Errorf("expected ErrEmptyVKData, got %v", err)
	}
	if valid {
		t.Error("expected false for empty VK data")
	}
}

func TestVerifyProofEmptyProofDataReturnsError(t *testing.T) {
	vk := &VerificationKey{Data: []byte("vk")}
	proof := &Proof{Data: []byte{}, PublicInputs: []byte("i")}
	valid, err := VerifyProof(vk, proof)
	if err != ErrEmptyProofData {
		t.Errorf("expected ErrEmptyProofData, got %v", err)
	}
	if valid {
		t.Error("expected false for empty proof data")
	}
}

func TestVerifyProofNoPublicInputsFails(t *testing.T) {
	vk := &VerificationKey{Data: []byte("vk")}
	proof := &Proof{Data: []byte("proof"), PublicInputs: nil}
	valid, err := VerifyProof(vk, proof)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("proof with no public inputs should be invalid")
	}
}

func TestVerifyProofEmptyPublicInputsFails(t *testing.T) {
	vk := &VerificationKey{Data: []byte("vk")}
	proof := &Proof{Data: []byte("proof"), PublicInputs: []byte{}}
	valid, err := VerifyProof(vk, proof)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("proof with empty public inputs should be invalid")
	}
}

// --- MockVerifier tests ---

func TestMockVerifierNameValue(t *testing.T) {
	v := &MockVerifier{}
	if v.Name() != "mock" {
		t.Errorf("Name() = %q, want %q", v.Name(), "mock")
	}
}

func TestMockVerifierProveSuccess(t *testing.T) {
	v := &MockVerifier{}
	program := &GuestProgram{
		Code:       []byte("code"),
		EntryPoint: "main",
		Version:    1,
	}
	input := []byte("test-input")

	proof, err := v.Prove(program, input)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if proof == nil {
		t.Fatal("expected non-nil proof")
	}
	if !bytes.HasPrefix(proof.Data, []byte("mock-proof:")) {
		t.Error("proof data should start with mock-proof: prefix")
	}
	if !bytes.Equal(proof.PublicInputs, input) {
		t.Error("public inputs should equal the input")
	}
}

func TestMockVerifierProveNilProgram(t *testing.T) {
	v := &MockVerifier{}
	_, err := v.Prove(nil, []byte("input"))
	if err == nil {
		t.Error("expected error for nil program")
	}
}

func TestMockVerifierProveEmptyCode(t *testing.T) {
	v := &MockVerifier{}
	_, err := v.Prove(&GuestProgram{Code: []byte{}}, []byte("input"))
	if err == nil {
		t.Error("expected error for empty program code")
	}
}

func TestMockVerifierProveNilInput(t *testing.T) {
	v := &MockVerifier{}
	program := &GuestProgram{Code: []byte("code")}
	proof, err := v.Prove(program, nil)
	if err != nil {
		t.Fatalf("Prove with nil input: %v", err)
	}
	if proof == nil {
		t.Fatal("expected non-nil proof")
	}
}

func TestMockVerifierVerifyAcceptsValidProof(t *testing.T) {
	v := &MockVerifier{}
	vk := &VerificationKey{Data: []byte("vk")}
	proof := &Proof{Data: []byte("data"), PublicInputs: []byte("inputs")}

	valid, err := v.Verify(vk, proof)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !valid {
		t.Error("expected valid proof")
	}
}

func TestMockVerifierVerifyRejectsNoPublicInputs(t *testing.T) {
	v := &MockVerifier{}
	vk := &VerificationKey{Data: []byte("vk")}
	proof := &Proof{Data: []byte("data"), PublicInputs: nil}

	valid, err := v.Verify(vk, proof)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if valid {
		t.Error("should reject proof with no public inputs")
	}
}

func TestMockVerifierVerifyNilVK(t *testing.T) {
	v := &MockVerifier{}
	proof := &Proof{Data: []byte("d"), PublicInputs: []byte("i")}
	_, err := v.Verify(nil, proof)
	if err != ErrNilVerificationKey {
		t.Errorf("expected ErrNilVerificationKey, got %v", err)
	}
}

func TestMockVerifierVerifyNilProof(t *testing.T) {
	v := &MockVerifier{}
	vk := &VerificationKey{Data: []byte("vk")}
	_, err := v.Verify(vk, nil)
	if err != ErrNilProof {
		t.Errorf("expected ErrNilProof, got %v", err)
	}
}

func TestMockVerifierVerifyEmptyVKData(t *testing.T) {
	v := &MockVerifier{}
	vk := &VerificationKey{Data: []byte{}}
	proof := &Proof{Data: []byte("d"), PublicInputs: []byte("i")}
	_, err := v.Verify(vk, proof)
	if err != ErrEmptyVKData {
		t.Errorf("expected ErrEmptyVKData, got %v", err)
	}
}

func TestMockVerifierVerifyEmptyProofData(t *testing.T) {
	v := &MockVerifier{}
	vk := &VerificationKey{Data: []byte("vk")}
	proof := &Proof{Data: []byte{}, PublicInputs: []byte("i")}
	_, err := v.Verify(vk, proof)
	if err != ErrEmptyProofData {
		t.Errorf("expected ErrEmptyProofData, got %v", err)
	}
}

// --- RejectingVerifier tests ---

func TestRejectingVerifierNameValue(t *testing.T) {
	v := &RejectingVerifier{}
	if v.Name() != "rejecting" {
		t.Errorf("Name() = %q, want %q", v.Name(), "rejecting")
	}
}

func TestRejectingVerifierProveAlwaysFails(t *testing.T) {
	v := &RejectingVerifier{}
	_, err := v.Prove(&GuestProgram{Code: []byte("code")}, []byte("input"))
	if err == nil {
		t.Error("expected error from rejecting prover")
	}
}

func TestRejectingVerifierVerifyAlwaysRejects(t *testing.T) {
	v := &RejectingVerifier{}
	vk := &VerificationKey{Data: []byte("vk")}
	proof := &Proof{Data: []byte("proof"), PublicInputs: []byte("inputs")}

	valid, err := v.Verify(vk, proof)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("rejecting verifier should always return false")
	}
}

func TestRejectingVerifierVerifyNilVK(t *testing.T) {
	v := &RejectingVerifier{}
	proof := &Proof{Data: []byte("d"), PublicInputs: []byte("i")}
	_, err := v.Verify(nil, proof)
	if err != ErrNilVerificationKey {
		t.Errorf("expected ErrNilVerificationKey, got %v", err)
	}
}

func TestRejectingVerifierVerifyNilProof(t *testing.T) {
	v := &RejectingVerifier{}
	vk := &VerificationKey{Data: []byte("vk")}
	_, err := v.Verify(vk, nil)
	if err != ErrNilProof {
		t.Errorf("expected ErrNilProof, got %v", err)
	}
}

// --- Error variable tests ---

func TestVerifierErrorMessages(t *testing.T) {
	tests := map[string]error{
		"zkvm: nil verification key":  ErrNilVerificationKey,
		"zkvm: nil proof":             ErrNilProof,
		"zkvm: empty proof data":      ErrEmptyProofData,
		"zkvm: empty verification key data": ErrEmptyVKData,
		"zkvm: invalid proof":         ErrInvalidProof,
	}
	for expected, e := range tests {
		if e == nil {
			t.Errorf("error for %q is nil", expected)
			continue
		}
		if e.Error() != expected {
			t.Errorf("error message = %q, want %q", e.Error(), expected)
		}
	}
}

// --- ProveAndVerify integration test ---

func TestMockProveAndVerifyRoundTrip(t *testing.T) {
	v := &MockVerifier{}
	program := &GuestProgram{
		Code:       []byte("stf-bytecode"),
		EntryPoint: "execute",
		Version:    2,
	}
	input := []byte("block-execution-input")

	proof, err := v.Prove(program, input)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}

	vk := &VerificationKey{
		Data:        []byte("verification-key"),
		ProgramHash: types.Hash{0xAA},
	}

	valid, err := v.Verify(vk, proof)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !valid {
		t.Error("proof generated by MockVerifier should verify")
	}
}

func TestVerifyProofUsesMockVerifier(t *testing.T) {
	// VerifyProof dispatches to MockVerifier internally.
	vk := &VerificationKey{Data: []byte("vk")}
	proof := &Proof{Data: []byte("data"), PublicInputs: []byte("inputs")}

	valid, err := VerifyProof(vk, proof)
	if err != nil {
		t.Fatalf("VerifyProof: %v", err)
	}
	if !valid {
		t.Error("expected valid proof from VerifyProof using mock backend")
	}
}
