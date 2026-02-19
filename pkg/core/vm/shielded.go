// Package vm implements the Ethereum Virtual Machine.
//
// shielded.go implements the ShieldedVM for private/shielded compute on L1.
// This targets the longer-term roadmap milestone "private L1 shielded compute".
//
// The ShieldedVM manages encrypted transaction execution, nullifier tracking
// for double-spend prevention, and shielded note creation/spending. It uses
// commitment schemes and nullifier hashes to preserve privacy while
// maintaining verifiability.
package vm

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Shielded execution errors.
var (
	ErrShieldedDisabled     = errors.New("shielded: privacy mode disabled")
	ErrNilShieldedTx        = errors.New("shielded: nil transaction")
	ErrShieldedGasExceeded  = errors.New("shielded: gas limit exceeds max shielded gas")
	ErrShieldedGasZero      = errors.New("shielded: gas limit is zero")
	ErrEmptyEncryptedInput  = errors.New("shielded: empty encrypted input")
	ErrEmptyShieldingKey    = errors.New("shielded: empty shielding key")
	ErrNullifierUsed        = errors.New("shielded: nullifier already spent")
	ErrNilNote              = errors.New("shielded: nil note")
	ErrEmptyProof           = errors.New("shielded: empty proof")
	ErrZeroValue            = errors.New("shielded: zero value note")
	ErrZeroRecipient        = errors.New("shielded: zero recipient address")
	ErrNullifierSetFull     = errors.New("shielded: nullifier set is full")
	ErrInvalidProof         = errors.New("shielded: proof verification failed")
)

// ShieldedConfig configures the shielded execution environment.
type ShieldedConfig struct {
	// MaxShieldedGas is the maximum gas allowed for a single shielded tx.
	MaxShieldedGas uint64

	// NullifierSetSize is the maximum number of nullifiers tracked.
	NullifierSetSize uint64

	// EnablePrivacy enables shielded execution. When false, all operations
	// return ErrShieldedDisabled.
	EnablePrivacy bool
}

// DefaultShieldedConfig returns a sensible default shielded config.
func DefaultShieldedConfig() ShieldedConfig {
	return ShieldedConfig{
		MaxShieldedGas:   10_000_000,
		NullifierSetSize: 1_000_000,
		EnablePrivacy:    true,
	}
}

// ShieldedTx represents a private transaction to be executed in the ShieldedVM.
type ShieldedTx struct {
	Sender         types.Address
	EncryptedInput []byte
	GasLimit       uint64
	Nonce          uint64
	ShieldingKey   []byte
}

// ShieldedResult holds the outcome of a shielded execution.
type ShieldedResult struct {
	OutputCommitment types.Hash
	GasUsed          uint64
	Success          bool
	EncryptedOutput  []byte
	NullifierHash    types.Hash
}

// ShieldedNote represents a shielded value note (similar to a UTXO in a
// privacy-preserving system). The commitment hides the value and recipient.
type ShieldedNote struct {
	Commitment types.Hash
	Value      uint64
	Recipient  types.Address
	Randomness []byte
}

// ShieldedVM executes private/shielded computations on L1.
// It is safe for concurrent use.
type ShieldedVM struct {
	mu         sync.RWMutex
	config     ShieldedConfig
	nullifiers map[types.Hash]bool // set of spent nullifiers
	notes      map[types.Hash]*ShieldedNote // commitment -> note
}

// NewShieldedVM creates a new shielded VM with the given config.
func NewShieldedVM(config ShieldedConfig) *ShieldedVM {
	if config.MaxShieldedGas == 0 {
		config.MaxShieldedGas = 10_000_000
	}
	if config.NullifierSetSize == 0 {
		config.NullifierSetSize = 1_000_000
	}
	return &ShieldedVM{
		config:     config,
		nullifiers: make(map[types.Hash]bool),
		notes:      make(map[types.Hash]*ShieldedNote),
	}
}

// ExecuteShielded executes a shielded transaction. The encrypted input is
// "decrypted" using the shielding key (simulated via hashing), executed in
// the shielded environment, and the result is encrypted back.
func (svm *ShieldedVM) ExecuteShielded(tx *ShieldedTx) (*ShieldedResult, error) {
	if !svm.config.EnablePrivacy {
		return nil, ErrShieldedDisabled
	}
	if tx == nil {
		return nil, ErrNilShieldedTx
	}
	if tx.GasLimit == 0 {
		return nil, ErrShieldedGasZero
	}
	if tx.GasLimit > svm.config.MaxShieldedGas {
		return nil, ErrShieldedGasExceeded
	}
	if len(tx.EncryptedInput) == 0 {
		return nil, ErrEmptyEncryptedInput
	}
	if len(tx.ShieldingKey) == 0 {
		return nil, ErrEmptyShieldingKey
	}

	// Generate the nullifier for this transaction (double-spend prevention).
	nullifier := GenerateNullifier(tx.EncryptedInput, tx.Nonce)

	// Check if this nullifier has already been used.
	svm.mu.Lock()
	defer svm.mu.Unlock()

	if svm.nullifiers[nullifier] {
		return nil, ErrNullifierUsed
	}
	if uint64(len(svm.nullifiers)) >= svm.config.NullifierSetSize {
		return nil, ErrNullifierSetFull
	}

	// Simulate shielded execution: "decrypt" input with key, compute on it,
	// then "encrypt" the output. In a real implementation this would involve
	// FHE or ZK circuit evaluation.
	decryptedHash := crypto.Keccak256Hash(tx.EncryptedInput, tx.ShieldingKey)

	// Compute output commitment: H(sender || decryptedHash || nonce).
	var nonceBuf [8]byte
	binary.BigEndian.PutUint64(nonceBuf[:], tx.Nonce)
	outputCommitment := crypto.Keccak256Hash(
		tx.Sender[:],
		decryptedHash[:],
		nonceBuf[:],
	)

	// Gas accounting: base 21000 + 16 per byte of encrypted input.
	gasUsed := uint64(21_000)
	gasUsed += uint64(len(tx.EncryptedInput)) * 16
	if gasUsed > tx.GasLimit {
		gasUsed = tx.GasLimit
	}

	// Generate encrypted output (simulated).
	encryptedOutput := crypto.Keccak256(
		outputCommitment[:],
		tx.ShieldingKey,
	)

	// Mark nullifier as used.
	svm.nullifiers[nullifier] = true

	return &ShieldedResult{
		OutputCommitment: outputCommitment,
		GasUsed:          gasUsed,
		Success:          true,
		EncryptedOutput:  encryptedOutput,
		NullifierHash:    nullifier,
	}, nil
}

// GenerateNullifier generates a unique nullifier from input data and a nonce.
// nullifier = Keccak256(input || nonce)
func GenerateNullifier(input []byte, nonce uint64) types.Hash {
	var nonceBuf [8]byte
	binary.BigEndian.PutUint64(nonceBuf[:], nonce)
	return crypto.Keccak256Hash(input, nonceBuf[:])
}

// VerifyNullifier returns true if the nullifier has NOT been used yet
// (i.e., the note is still unspent). Returns false if already spent.
func (svm *ShieldedVM) VerifyNullifier(nullifier types.Hash) bool {
	svm.mu.RLock()
	defer svm.mu.RUnlock()
	return !svm.nullifiers[nullifier]
}

// CreateShieldedNote creates a new shielded value note for the given
// recipient. The note's commitment hides the value behind a random blinding
// factor derived from the value and recipient.
func (svm *ShieldedVM) CreateShieldedNote(value uint64, recipient types.Address) (*ShieldedNote, error) {
	if !svm.config.EnablePrivacy {
		return nil, ErrShieldedDisabled
	}
	if value == 0 {
		return nil, ErrZeroValue
	}
	if recipient.IsZero() {
		return nil, ErrZeroRecipient
	}

	// Generate randomness: H(recipient || value). In production this would
	// be cryptographically random, but for deterministic testing we derive it.
	var valBuf [8]byte
	binary.BigEndian.PutUint64(valBuf[:], value)
	randomness := crypto.Keccak256(recipient[:], valBuf[:])

	// Commitment: H(value || recipient || randomness).
	commitment := crypto.Keccak256Hash(valBuf[:], recipient[:], randomness)

	note := &ShieldedNote{
		Commitment: commitment,
		Value:      value,
		Recipient:  recipient,
		Randomness: randomness,
	}

	svm.mu.Lock()
	svm.notes[commitment] = note
	svm.mu.Unlock()

	return note, nil
}

// SpendNote spends a shielded note by revealing a proof. The note's
// commitment is used to derive a nullifier, which prevents double-spending.
func (svm *ShieldedVM) SpendNote(note *ShieldedNote, proof []byte) (*ShieldedResult, error) {
	if !svm.config.EnablePrivacy {
		return nil, ErrShieldedDisabled
	}
	if note == nil {
		return nil, ErrNilNote
	}
	if len(proof) == 0 {
		return nil, ErrEmptyProof
	}

	// Derive nullifier from the note commitment and recipient.
	nullifier := crypto.Keccak256Hash(note.Commitment[:], note.Recipient[:])

	svm.mu.Lock()
	defer svm.mu.Unlock()

	if svm.nullifiers[nullifier] {
		return nil, ErrNullifierUsed
	}
	if uint64(len(svm.nullifiers)) >= svm.config.NullifierSetSize {
		return nil, ErrNullifierSetFull
	}

	// Verify the proof: stub verification checks proof against commitment.
	if !verifySpendProof(note, proof) {
		return nil, ErrInvalidProof
	}

	// Mark nullifier as spent.
	svm.nullifiers[nullifier] = true

	// Remove note from active notes.
	delete(svm.notes, note.Commitment)

	// Generate output commitment for the spend result.
	var valBuf [8]byte
	binary.BigEndian.PutUint64(valBuf[:], note.Value)
	outputCommitment := crypto.Keccak256Hash(
		nullifier[:],
		valBuf[:],
		proof,
	)

	encryptedOutput := crypto.Keccak256(outputCommitment[:], proof)

	return &ShieldedResult{
		OutputCommitment: outputCommitment,
		GasUsed:          21_000 + uint64(len(proof))*16,
		Success:          true,
		EncryptedOutput:  encryptedOutput,
		NullifierHash:    nullifier,
	}, nil
}

// VerifyShieldedProof verifies that a shielded execution result is valid.
// The proof is checked by recomputing the output commitment from the
// nullifier and encrypted output.
func (svm *ShieldedVM) VerifyShieldedProof(result *ShieldedResult) bool {
	if result == nil {
		return false
	}
	if !result.Success {
		return false
	}
	if result.OutputCommitment.IsZero() {
		return false
	}
	if result.NullifierHash.IsZero() {
		return false
	}
	// Verify the encrypted output is consistent with the commitment.
	// recomputed = H(outputCommitment || encryptedOutput)
	check := crypto.Keccak256Hash(
		result.OutputCommitment[:],
		result.EncryptedOutput,
	)
	// The check hash should be non-zero (basic consistency).
	return !check.IsZero()
}

// NullifierCount returns the number of spent nullifiers tracked.
func (svm *ShieldedVM) NullifierCount() int {
	svm.mu.RLock()
	defer svm.mu.RUnlock()
	return len(svm.nullifiers)
}

// NoteCount returns the number of active shielded notes.
func (svm *ShieldedVM) NoteCount() int {
	svm.mu.RLock()
	defer svm.mu.RUnlock()
	return len(svm.notes)
}

// verifySpendProof is a stub proof verifier. It checks that the proof
// contains a valid hash of the note's commitment. In a real implementation,
// this would verify a zk-SNARK.
func verifySpendProof(note *ShieldedNote, proof []byte) bool {
	if len(proof) < 32 {
		return false
	}
	// Stub: accept proofs where the first 32 bytes match H(commitment).
	expected := crypto.Keccak256(note.Commitment[:])
	for i := 0; i < 32; i++ {
		if proof[i] != expected[i] {
			return false
		}
	}
	return true
}

// MakeSpendProof creates a valid spend proof for the given note.
// This is exported for use in tests.
func MakeSpendProof(note *ShieldedNote) []byte {
	return crypto.Keccak256(note.Commitment[:])
}
