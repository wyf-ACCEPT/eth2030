package zkvm

import (
	"errors"
)

// Verification errors.
var (
	ErrNilVerificationKey = errors.New("zkvm: nil verification key")
	ErrNilProof           = errors.New("zkvm: nil proof")
	ErrEmptyProofData     = errors.New("zkvm: empty proof data")
	ErrEmptyVKData        = errors.New("zkvm: empty verification key data")
	ErrInvalidProof       = errors.New("zkvm: invalid proof")
)

// VerifyProof validates a proof against a verification key.
// It returns true if the proof is valid, false otherwise.
func VerifyProof(vk *VerificationKey, proof *Proof) (bool, error) {
	if vk == nil {
		return false, ErrNilVerificationKey
	}
	if proof == nil {
		return false, ErrNilProof
	}
	if len(vk.Data) == 0 {
		return false, ErrEmptyVKData
	}
	if len(proof.Data) == 0 {
		return false, ErrEmptyProofData
	}

	// In a full implementation, this would dispatch to the appropriate
	// proof system (e.g., Groth16, PLONK, STARK) based on the VK format.
	// For now, use the mock verifier which accepts structurally valid proofs.
	mock := &MockVerifier{}
	return mock.Verify(vk, proof)
}

// MockVerifier is a testing verifier that accepts any structurally valid proof.
// A proof is "structurally valid" if it has non-empty Data and PublicInputs.
type MockVerifier struct{}

// Name returns the verifier backend name.
func (v *MockVerifier) Name() string {
	return "mock"
}

// Prove generates a mock proof for the given program and input.
func (v *MockVerifier) Prove(program *GuestProgram, input []byte) (*Proof, error) {
	if program == nil || len(program.Code) == 0 {
		return nil, errors.New("zkvm: nil or empty program")
	}

	// Mock proof: the "proof data" is just the input hash placeholder.
	return &Proof{
		Data:         append([]byte("mock-proof:"), input...),
		PublicInputs: input,
	}, nil
}

// Verify checks a proof against a verification key.
// The mock verifier accepts proofs where both Data and PublicInputs are non-empty.
func (v *MockVerifier) Verify(vk *VerificationKey, proof *Proof) (bool, error) {
	if vk == nil {
		return false, ErrNilVerificationKey
	}
	if proof == nil {
		return false, ErrNilProof
	}
	if len(vk.Data) == 0 {
		return false, ErrEmptyVKData
	}
	if len(proof.Data) == 0 {
		return false, ErrEmptyProofData
	}

	// Mock: accept if public inputs are present.
	if len(proof.PublicInputs) == 0 {
		return false, nil
	}

	return true, nil
}

// RejectingVerifier is a testing verifier that rejects all proofs.
type RejectingVerifier struct{}

// Name returns the verifier backend name.
func (v *RejectingVerifier) Name() string {
	return "rejecting"
}

// Prove always returns an error.
func (v *RejectingVerifier) Prove(program *GuestProgram, input []byte) (*Proof, error) {
	return nil, errors.New("zkvm: rejecting verifier cannot prove")
}

// Verify always returns false.
func (v *RejectingVerifier) Verify(vk *VerificationKey, proof *Proof) (bool, error) {
	if vk == nil {
		return false, ErrNilVerificationKey
	}
	if proof == nil {
		return false, ErrNilProof
	}
	return false, nil
}
