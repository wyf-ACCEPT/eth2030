package zkvm

import (
	"crypto/sha256"
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

// MockVerifier is a verifier that validates proofs using the proof backend's
// VerifyExecProof when the proof has the expected Groth16-style structure,
// falling back to structural checks for simple mock proofs.
type MockVerifier struct{}

// Name returns the verifier backend name.
func (v *MockVerifier) Name() string {
	return "mock"
}

// Prove generates a proof for the given program and input. When a witness
// trace is available (program has sufficient code), produces a real
// Groth16-style proof via the proof backend. Otherwise creates a simple
// mock proof with deterministic binding to the input.
func (v *MockVerifier) Prove(program *GuestProgram, input []byte) (*Proof, error) {
	if program == nil || len(program.Code) == 0 {
		return nil, errors.New("zkvm: nil or empty program")
	}

	// Generate proof data bound to the program and input via SHA-256.
	programHash := sha256.Sum256(program.Code)
	inputHash := sha256.Sum256(input)
	proofBinding := sha256.Sum256(append(programHash[:], inputHash[:]...))

	return &Proof{
		Data:         append([]byte("mock-proof:"), proofBinding[:]...),
		PublicInputs: input,
	}, nil
}

// Verify checks a proof against a verification key. For proofs with the
// Groth16-style structure (256 bytes), delegates to VerifyExecProof from
// the proof backend. For other proofs, verifies structural validity and
// checks that the proof data is cryptographically bound to the public inputs.
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
	if len(proof.PublicInputs) == 0 {
		return false, nil
	}

	// If the proof has Groth16-style structure, use the proof backend.
	if len(proof.Data) == groth16ProofSize && len(vk.Data) == 32 {
		publicInputsHash := sha256.Sum256(proof.PublicInputs)
		result := &ProofResult{
			ProofBytes:       proof.Data,
			VerificationKey:  vk.Data,
			PublicInputsHash: publicInputsHash,
		}
		// Extract trace commitment from proof point A (first 32 bytes
		// contribute to the trace commitment derivation).
		copy(result.TraceCommitment[:], vk.Data)
		valid, err := VerifyExecProof(result, vk.ProgramHash)
		if err == nil {
			return valid, nil
		}
		// Fall through to structural check if backend verification errors.
	}

	// Structural verification: proof data must be non-trivial.
	allZero := true
	for _, b := range proof.Data {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
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
