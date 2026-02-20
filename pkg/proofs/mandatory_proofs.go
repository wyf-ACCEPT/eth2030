// mandatory_proofs.go implements the K+ roadmap mandatory 3-of-5 block proof
// validation. Blocks must include at least 3 of 5 proof types to be valid.
// The 5 proof types are: StateProof, ReceiptProof, StorageProof, WitnessProof,
// ExecutionProof.
package proofs

import (
	"errors"
	"fmt"
)

// Block-level proof type constants (distinct from the cryptographic ProofType
// in types.go which represents ZK-SNARK/ZK-STARK/IPA/KZG).
const (
	BlockProofState     = "StateProof"
	BlockProofReceipt   = "ReceiptProof"
	BlockProofStorage   = "StorageProof"
	BlockProofWitness   = "WitnessProof"
	BlockProofExecution = "ExecutionProof"
)

// AllBlockProofTypes lists all 5 valid block proof types.
var AllBlockProofTypes = []string{
	BlockProofState,
	BlockProofReceipt,
	BlockProofStorage,
	BlockProofWitness,
	BlockProofExecution,
}

// DefaultMinRequired is the minimum number of proof types required (3 of 5).
const DefaultMinRequired = 3

// Mandatory block proof validation errors.
var (
	ErrInsufficientProofs = errors.New("mandatory_proofs: insufficient proofs, need at least 3 of 5")
	ErrInvalidProofType   = errors.New("mandatory_proofs: invalid proof type")
	ErrDuplicateProof     = errors.New("mandatory_proofs: duplicate proof type")
	ErrEmptyProofData     = errors.New("mandatory_proofs: proof data is empty")
	ErrNilProofSet        = errors.New("mandatory_proofs: nil proof set")
)

// isValidBlockProofType returns true if the given string is a known block proof type.
func isValidBlockProofType(pt string) bool {
	for _, t := range AllBlockProofTypes {
		if t == pt {
			return true
		}
	}
	return false
}

// ProofRequirement describes a single proof type's configuration within
// the mandatory proof validator.
type ProofRequirement struct {
	// ProofType is the name of the proof type (e.g. "StateProof").
	ProofType string
	// Required indicates whether this proof type is mandatory.
	Required bool
	// Weight is the importance weight for this proof type (1-10).
	Weight int
}

// ProofEntry holds the data for a single registered proof within a ProofSet.
type ProofEntry struct {
	ProofType string
	Data      []byte
}

// ProofSet tracks which block proof types are present in a block.
type ProofSet struct {
	entries map[string]*ProofEntry
}

// NewProofSet creates a new empty ProofSet.
func NewProofSet() *ProofSet {
	return &ProofSet{
		entries: make(map[string]*ProofEntry),
	}
}

// Has returns true if the proof set contains the given proof type.
func (ps *ProofSet) Has(proofType string) bool {
	_, ok := ps.entries[proofType]
	return ok
}

// Count returns the number of distinct proof types in the set.
func (ps *ProofSet) Count() int {
	return len(ps.entries)
}

// Types returns the list of proof type names present in the set.
func (ps *ProofSet) Types() []string {
	result := make([]string, 0, len(ps.entries))
	for t := range ps.entries {
		result = append(result, t)
	}
	return result
}

// Get returns the proof entry for the given type, or nil if not present.
func (ps *ProofSet) Get(proofType string) *ProofEntry {
	return ps.entries[proofType]
}

// RegisterProof adds a proof of the given type with the provided data to the
// proof set. Returns ErrInvalidProofType for unknown types, ErrDuplicateProof
// if the type is already present, and ErrEmptyProofData if data is nil/empty.
func RegisterProof(proofSet *ProofSet, proofType string, data []byte) error {
	if proofSet == nil {
		return ErrNilProofSet
	}
	if !isValidBlockProofType(proofType) {
		return fmt.Errorf("%w: %s", ErrInvalidProofType, proofType)
	}
	if len(data) == 0 {
		return ErrEmptyProofData
	}
	if proofSet.Has(proofType) {
		return fmt.Errorf("%w: %s", ErrDuplicateProof, proofType)
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	proofSet.entries[proofType] = &ProofEntry{
		ProofType: proofType,
		Data:      cp,
	}
	return nil
}

// MandatoryProofValidator validates that a block includes the required
// number of proof types (3 of 5 by default).
type MandatoryProofValidator struct {
	// MinRequired is the minimum number of proof types needed.
	MinRequired int
	// Requirements describes each proof type's configuration.
	Requirements []ProofRequirement
}

// NewMandatoryProofValidator creates a validator with default 3-of-5 config.
func NewMandatoryProofValidator() *MandatoryProofValidator {
	reqs := make([]ProofRequirement, len(AllBlockProofTypes))
	for i, pt := range AllBlockProofTypes {
		reqs[i] = ProofRequirement{
			ProofType: pt,
			Required:  false, // none individually required, just 3 of 5
			Weight:    1,
		}
	}
	return &MandatoryProofValidator{
		MinRequired:  DefaultMinRequired,
		Requirements: reqs,
	}
}

// NewMandatoryProofValidatorWithThreshold creates a validator with a custom
// minimum threshold.
func NewMandatoryProofValidatorWithThreshold(minRequired int) *MandatoryProofValidator {
	v := NewMandatoryProofValidator()
	if minRequired < 0 {
		minRequired = 0
	}
	if minRequired > len(AllBlockProofTypes) {
		minRequired = len(AllBlockProofTypes)
	}
	v.MinRequired = minRequired
	return v
}

// ValidateBlockProofs checks whether the proof set satisfies the 3-of-5
// mandatory proof rule. Returns (true, nil) if valid, (false, error) otherwise.
func ValidateBlockProofs(proofSet *ProofSet) (bool, error) {
	v := NewMandatoryProofValidator()
	return v.Validate(proofSet)
}

// Validate checks the proof set against this validator's requirements.
func (v *MandatoryProofValidator) Validate(proofSet *ProofSet) (bool, error) {
	if proofSet == nil {
		return false, ErrNilProofSet
	}

	// Count how many valid proof types are present.
	count := 0
	for _, req := range v.Requirements {
		if proofSet.Has(req.ProofType) {
			count++
		}
	}

	if count < v.MinRequired {
		return false, fmt.Errorf("%w: have %d, need %d",
			ErrInsufficientProofs, count, v.MinRequired)
	}
	return true, nil
}

// WeightedScore returns the sum of weights for all proof types present in
// the proof set. Higher scores indicate more comprehensive proof coverage.
func (v *MandatoryProofValidator) WeightedScore(proofSet *ProofSet) int {
	if proofSet == nil {
		return 0
	}
	score := 0
	for _, req := range v.Requirements {
		if proofSet.Has(req.ProofType) {
			score += req.Weight
		}
	}
	return score
}

// MissingTypes returns the proof types that are not present in the proof set.
func (v *MandatoryProofValidator) MissingTypes(proofSet *ProofSet) []string {
	if proofSet == nil {
		return AllBlockProofTypes
	}
	var missing []string
	for _, req := range v.Requirements {
		if !proofSet.Has(req.ProofType) {
			missing = append(missing, req.ProofType)
		}
	}
	return missing
}
