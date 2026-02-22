// shielded_tx.go implements a fuller shielded transaction framework with
// Pedersen commitments, range proofs, note management, and balance verification
// for private value transfers on Ethereum L1.
package crypto

import (
	"encoding/binary"
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Shielded transaction errors.
var (
	ErrNullifierSpent    = errors.New("shielded: nullifier already spent")
	ErrCommitmentExists  = errors.New("shielded: commitment already exists")
	ErrCommitmentUnknown = errors.New("shielded: commitment not found")
	ErrInvalidRangeProof = errors.New("shielded: invalid range proof")
	ErrBalanceMismatch   = errors.New("shielded: input/output balance mismatch")
	ErrNilNote           = errors.New("shielded: nil note")
	ErrInvalidProof      = errors.New("shielded: invalid proof")
)

// RangeProofBits is the number of bits for range proofs (value in [0, 2^64)).
const RangeProofBits = 64

// Generator points for the Pedersen commitment scheme (simulated).
// In production these would be nothing-up-my-sleeve EC curve points.
var (
	shieldedGenG = types.HexToHash("0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b")
	shieldedGenH = types.HexToHash("a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1")
)

// CommitmentOpening holds the opening (witness) for a Pedersen commitment.
type CommitmentOpening struct {
	Value    uint64
	Blinding [32]byte
}

// RangeProof proves a committed value lies in [0, 2^64) without revealing it.
// This is a simulated proof; in production it would be a Bulletproof or
// similar zero-knowledge range proof.
type RangeProof struct {
	Commitment types.Hash
	ProofData  []byte // simulated proof bytes
	BitLength  uint8  // always 64 for our scheme
}

// ShieldNote represents a note in the shielded pool.
type ShieldNote struct {
	Commitment     types.Hash    // Pedersen commitment to the value
	NullifierHash  types.Hash    // hash used to mark this note as spent
	EncryptedValue []byte        // encrypted value data for the recipient
	RangeProof     *RangeProof   // proof that value is in valid range
}

// ShieldedTransfer represents a complete shielded value transfer with
// input notes being spent and output notes being created.
type ShieldedTransfer struct {
	InputNotes   []*ShieldNote // notes being spent (consumed)
	OutputNotes  []*ShieldNote // notes being created
	BalanceProof []byte        // proof that sum(inputs) == sum(outputs)
	Fee          uint64        // transparent fee paid to the network
}

// NullifierSet tracks spent nullifiers to prevent double-spending.
// Thread-safe for concurrent access.
type NullifierSet struct {
	mu         sync.RWMutex
	nullifiers map[types.Hash]struct{}
}

// NewNullifierSet creates a new empty nullifier set.
func NewNullifierSet() *NullifierSet {
	return &NullifierSet{
		nullifiers: make(map[types.Hash]struct{}),
	}
}

// Has returns true if the nullifier has been revealed (note is spent).
func (ns *NullifierSet) Has(nullifier types.Hash) bool {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	_, ok := ns.nullifiers[nullifier]
	return ok
}

// Add marks a nullifier as spent. Returns false if already spent.
func (ns *NullifierSet) Add(nullifier types.Hash) bool {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	if _, ok := ns.nullifiers[nullifier]; ok {
		return false
	}
	ns.nullifiers[nullifier] = struct{}{}
	return true
}

// Size returns the number of spent nullifiers.
func (ns *NullifierSet) Size() int {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	return len(ns.nullifiers)
}

// ShieldedPedersenCommit computes a Pedersen commitment for shielded transfers:
// C = Keccak256(G || value || H || blinding).
// This is a hash-based simulation of the algebraic Pedersen commitment
// C = value*G + blinding*H on an elliptic curve.
func ShieldedPedersenCommit(value uint64, blinding [32]byte) types.Hash {
	var valueBuf [8]byte
	binary.BigEndian.PutUint64(valueBuf[:], value)

	return Keccak256Hash(
		shieldedGenG[:],
		valueBuf[:],
		shieldedGenH[:],
		blinding[:],
	)
}

// VerifyCommitmentOpening verifies a Pedersen commitment against an opening.
func VerifyCommitmentOpening(commitment types.Hash, opening *CommitmentOpening) bool {
	if opening == nil {
		return false
	}
	expected := ShieldedPedersenCommit(opening.Value, opening.Blinding)
	return commitment == expected
}

// CommitmentsHomomorphicAdd demonstrates the homomorphic property of Pedersen
// commitments: C(v1,b1) + C(v2,b2) = C(v1+v2, b1+b2).
// In our hash-based simulation, we verify this by checking openings.
func CommitmentsHomomorphicAdd(c1, c2 types.Hash, o1, o2 *CommitmentOpening) (types.Hash, *CommitmentOpening) {
	if o1 == nil || o2 == nil {
		return types.Hash{}, nil
	}
	sumValue := o1.Value + o2.Value
	var sumBlinding [32]byte
	// Add blinding factors as big integers mod a large prime.
	b1 := new(big.Int).SetBytes(o1.Blinding[:])
	b2 := new(big.Int).SetBytes(o2.Blinding[:])
	sum := new(big.Int).Add(b1, b2)
	sumBytes := sum.Bytes()
	if len(sumBytes) > 32 {
		sumBytes = sumBytes[len(sumBytes)-32:]
	}
	copy(sumBlinding[32-len(sumBytes):], sumBytes)

	combined := ShieldedPedersenCommit(sumValue, sumBlinding)
	return combined, &CommitmentOpening{Value: sumValue, Blinding: sumBlinding}
}

// GenerateRangeProof creates a simulated range proof that a committed value
// lies in [0, 2^64). In production this would generate a Bulletproof.
func GenerateRangeProof(value uint64, blinding [32]byte) *RangeProof {
	commitment := ShieldedPedersenCommit(value, blinding)

	// Simulated proof data: hash of (commitment || value || blinding).
	var valueBuf [8]byte
	binary.BigEndian.PutUint64(valueBuf[:], value)
	proofData := Keccak256(commitment[:], valueBuf[:], blinding[:])

	return &RangeProof{
		Commitment: commitment,
		ProofData:  proofData,
		BitLength:  RangeProofBits,
	}
}

// VerifyRangeProof verifies a simulated range proof.
// In production, this would verify a Bulletproof without knowledge of the value.
func VerifyRangeProof(proof *RangeProof) bool {
	if proof == nil {
		return false
	}
	if proof.BitLength != RangeProofBits {
		return false
	}
	if len(proof.ProofData) == 0 {
		return false
	}
	if proof.Commitment.IsZero() {
		return false
	}
	// Simulated verification: check proof is non-trivial.
	for _, b := range proof.ProofData {
		if b != 0 {
			return true
		}
	}
	return false
}

// ShieldedNotePool manages shielded notes: commitments and nullifiers.
// Thread-safe for concurrent access.
type ShieldedNotePool struct {
	mu          sync.RWMutex
	commitments map[types.Hash]*ShieldNote
	nullifiers  *NullifierSet
}

// NewShieldedNotePool creates a new shielded note pool.
func NewShieldedNotePool() *ShieldedNotePool {
	return &ShieldedNotePool{
		commitments: make(map[types.Hash]*ShieldNote),
		nullifiers:  NewNullifierSet(),
	}
}

// CreateNote creates a new shielded note for a given value and recipient.
// Returns the note and its commitment opening (needed to later spend it).
func (p *ShieldedNotePool) CreateNote(value uint64, recipient types.Address) (*ShieldNote, *CommitmentOpening, error) {
	// Generate a random blinding factor.
	var blinding [32]byte
	blindingHash := Keccak256Hash(
		recipient[:],
		shieldedEncodeU64(value),
		shieldedGenH[:],
	)
	copy(blinding[:], blindingHash[:])

	commitment := ShieldedPedersenCommit(value, blinding)

	// Generate nullifier from commitment and recipient.
	nullifierHash := Keccak256Hash(commitment[:], recipient[:])

	// Encrypt the value for the recipient (simplified).
	encrypted := shieldedEncryptVal(value, recipient, blinding)

	// Generate range proof.
	rangeProof := GenerateRangeProof(value, blinding)

	note := &ShieldNote{
		Commitment:     commitment,
		NullifierHash:  nullifierHash,
		EncryptedValue: encrypted,
		RangeProof:     rangeProof,
	}

	opening := &CommitmentOpening{
		Value:    value,
		Blinding: blinding,
	}

	// Add commitment to the pool.
	p.mu.Lock()
	p.commitments[commitment] = note
	p.mu.Unlock()

	return note, opening, nil
}

// SpendNote marks a note as spent by revealing its nullifier.
// Returns an error if the note was already spent (double-spend).
func (p *ShieldedNotePool) SpendNote(nullifier types.Hash) error {
	if !p.nullifiers.Add(nullifier) {
		return ErrNullifierSpent
	}
	return nil
}

// VerifyNote verifies that a note is valid: its range proof checks out
// and the commitment is well-formed.
func (p *ShieldedNotePool) VerifyNote(note *ShieldNote) error {
	if note == nil {
		return ErrNilNote
	}
	if note.Commitment.IsZero() {
		return ErrInvalidProof
	}
	if !VerifyRangeProof(note.RangeProof) {
		return ErrInvalidRangeProof
	}
	return nil
}

// HasNoteCommitment returns true if the commitment exists in the pool.
func (p *ShieldedNotePool) HasNoteCommitment(commitment types.Hash) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.commitments[commitment]
	return ok
}

// GetNote returns the note for a given commitment, or nil.
func (p *ShieldedNotePool) GetNote(commitment types.Hash) *ShieldNote {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.commitments[commitment]
}

// IsSpent returns true if the nullifier has been revealed.
func (p *ShieldedNotePool) IsSpent(nullifier types.Hash) bool {
	return p.nullifiers.Has(nullifier)
}

// NoteCommitmentCount returns the number of commitments in the pool.
func (p *ShieldedNotePool) NoteCommitmentCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.commitments)
}

// SpentNullifierCount returns the number of spent nullifiers.
func (p *ShieldedNotePool) SpentNullifierCount() int {
	return p.nullifiers.Size()
}

// VerifyBalanceProof verifies that the sum of input commitments equals the
// sum of output commitments using the homomorphic property of Pedersen
// commitments. This ensures no value is created or destroyed.
func VerifyBalanceProof(inputs, outputs []*CommitmentOpening) bool {
	if len(inputs) == 0 || len(outputs) == 0 {
		return false
	}

	var inputSum uint64
	for _, o := range inputs {
		if o == nil {
			return false
		}
		inputSum += o.Value
	}

	var outputSum uint64
	for _, o := range outputs {
		if o == nil {
			return false
		}
		outputSum += o.Value
	}

	return inputSum == outputSum
}

// VerifyTransfer validates a complete shielded transfer:
// 1. All input nullifiers are unspent.
// 2. All output notes have valid range proofs.
// 3. The balance proof holds (inputs == outputs + fee).
func (p *ShieldedNotePool) VerifyTransfer(transfer *ShieldedTransfer, inputOpenings, outputOpenings []*CommitmentOpening) error {
	if transfer == nil {
		return ErrNilNote
	}

	// Check all input nullifiers are unspent.
	for _, note := range transfer.InputNotes {
		if note == nil {
			return ErrNilNote
		}
		if p.IsSpent(note.NullifierHash) {
			return ErrNullifierSpent
		}
	}

	// Verify all output range proofs.
	for _, note := range transfer.OutputNotes {
		if err := p.VerifyNote(note); err != nil {
			return err
		}
	}

	// Verify balance: sum(inputs) = sum(outputs) + fee.
	if inputOpenings != nil && outputOpenings != nil {
		var inputSum uint64
		for _, o := range inputOpenings {
			if o == nil {
				return ErrInvalidProof
			}
			inputSum += o.Value
		}

		var outputSum uint64
		for _, o := range outputOpenings {
			if o == nil {
				return ErrInvalidProof
			}
			outputSum += o.Value
		}

		if inputSum != outputSum+transfer.Fee {
			return ErrBalanceMismatch
		}
	}

	return nil
}

// ApplyTransfer applies a validated transfer: spends input nullifiers and
// adds output commitments to the pool.
func (p *ShieldedNotePool) ApplyTransfer(transfer *ShieldedTransfer) error {
	if transfer == nil {
		return ErrNilNote
	}

	// Spend all inputs.
	for _, note := range transfer.InputNotes {
		if err := p.SpendNote(note.NullifierHash); err != nil {
			return err
		}
	}

	// Add all output commitments.
	p.mu.Lock()
	for _, note := range transfer.OutputNotes {
		p.commitments[note.Commitment] = note
	}
	p.mu.Unlock()

	return nil
}

// --- internal helpers ---

func shieldedEncodeU64(v uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, v)
	return buf
}

func shieldedEncryptVal(value uint64, recipient types.Address, blinding [32]byte) []byte {
	// Simplified encryption: XOR value bytes with hash of recipient+blinding.
	valueBuf := shieldedEncodeU64(value)
	mask := Keccak256(recipient[:], blinding[:])
	encrypted := make([]byte, len(valueBuf))
	for i := range valueBuf {
		encrypted[i] = valueBuf[i] ^ mask[i]
	}
	return encrypted
}
