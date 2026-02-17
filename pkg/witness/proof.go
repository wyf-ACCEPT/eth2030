package witness

import (
	"errors"
	"fmt"

	"github.com/eth2028/eth2028/core/types"
)

// MaxProofSize is the maximum size of an execution proof (300 KiB per EIP-8025).
const MaxProofSize = 300 * 1024

// Proof type constants.
const (
	ProofTypeSP1   uint8 = 1 // Succinct SP1 prover
	ProofTypeZisK  uint8 = 2 // ZisK prover
	ProofTypeRISC0 uint8 = 3 // RISC Zero prover
)

// ExecutionProof is a ZK proof of correct block execution.
type ExecutionProof struct {
	// ProofType identifies the prover system used.
	ProofType uint8

	// ProofBytes is the serialized proof data.
	ProofBytes []byte
}

// Errors for proof validation.
var (
	ErrProofTooLarge    = errors.New("proof exceeds maximum size")
	ErrUnknownProofType = errors.New("unknown proof type")
	ErrEmptyProof       = errors.New("proof bytes are empty")
	ErrVerificationFail = errors.New("proof verification failed")
)

// Validate checks that the proof meets basic validity requirements.
func (p *ExecutionProof) Validate() error {
	if len(p.ProofBytes) == 0 {
		return ErrEmptyProof
	}
	if len(p.ProofBytes) > MaxProofSize {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrProofTooLarge, len(p.ProofBytes), MaxProofSize)
	}
	switch p.ProofType {
	case ProofTypeSP1, ProofTypeZisK, ProofTypeRISC0:
		return nil
	default:
		return fmt.Errorf("%w: %d", ErrUnknownProofType, p.ProofType)
	}
}

// Size returns the size of the proof in bytes.
func (p *ExecutionProof) Size() int {
	return len(p.ProofBytes)
}

// ProofTypeName returns a human-readable name for the proof type.
func ProofTypeName(pt uint8) string {
	switch pt {
	case ProofTypeSP1:
		return "SP1"
	case ProofTypeZisK:
		return "ZisK"
	case ProofTypeRISC0:
		return "RISC0"
	default:
		return fmt.Sprintf("Unknown(%d)", pt)
	}
}

// Prover is the interface for ZK proof generation backends.
type Prover interface {
	// ProofType returns the proof type this prover generates.
	ProofType() uint8

	// Prove generates an execution proof for the given block execution.
	Prove(witness *ExecutionWitness, stateRoot types.Hash) (*ExecutionProof, error)
}

// Verifier is the interface for ZK proof verification backends.
type Verifier interface {
	// SupportsType returns true if this verifier can verify the given proof type.
	SupportsType(proofType uint8) bool

	// Verify checks that the proof is valid for the given state root.
	Verify(proof *ExecutionProof, stateRoot types.Hash) error
}

// MultiVerifier supports verification of multiple proof types.
type MultiVerifier struct {
	verifiers map[uint8]Verifier
}

// NewMultiVerifier creates a MultiVerifier with the given verifiers.
func NewMultiVerifier(verifiers ...Verifier) *MultiVerifier {
	mv := &MultiVerifier{
		verifiers: make(map[uint8]Verifier),
	}
	for _, v := range verifiers {
		for _, pt := range []uint8{ProofTypeSP1, ProofTypeZisK, ProofTypeRISC0} {
			if v.SupportsType(pt) {
				mv.verifiers[pt] = v
			}
		}
	}
	return mv
}

// Verify checks the proof against registered verifiers.
func (mv *MultiVerifier) Verify(proof *ExecutionProof, stateRoot types.Hash) error {
	if err := proof.Validate(); err != nil {
		return err
	}
	v, ok := mv.verifiers[proof.ProofType]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownProofType, ProofTypeName(proof.ProofType))
	}
	return v.Verify(proof, stateRoot)
}
