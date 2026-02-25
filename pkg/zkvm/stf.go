package zkvm

import (
	"errors"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// State Transition Function (STF) framework for zkISA/eRISC proof generation.
// Part of the J+ upgrade path, enabling mandatory proof-carrying blocks
// where each state transition is verified via a zero-knowledge proof.

// STF configuration defaults.
const (
	DefaultMaxWitnessSize = 16 * 1024 * 1024 // 16 MiB
	DefaultMaxProofSize   = 1 * 1024 * 1024  // 1 MiB
	DefaultTargetCycles   = 1 << 24          // ~16M cycles
	DefaultSTFProofSystem = "plonk"

	// Simulated cycle costs per operation type.
	cyclesPerTransaction = 10000
	cyclesPerWitnessKB   = 100
	cyclesOverhead       = 50000
)

// STF errors.
var (
	ErrSTFNilInput           = errors.New("stf: nil input")
	ErrSTFEmptyTransactions  = errors.New("stf: empty transactions")
	ErrSTFPostRootMismatch   = errors.New("stf: post state root mismatch")
	ErrSTFWitnessTooLarge    = errors.New("stf: witness exceeds maximum size")
	ErrSTFProofTooLarge      = errors.New("stf: proof exceeds maximum size")
	ErrSTFInvalidProofSystem = errors.New("stf: invalid proof system")
	ErrSTFNilBlock           = errors.New("stf: nil block header")
)

// Valid proof systems.
var validProofSystems = map[string]bool{
	"groth16": true,
	"plonk":   true,
	"stark":   true,
}

// STFConfig holds configuration for the STF executor.
type STFConfig struct {
	// MaxWitnessSize is the maximum allowed witness size in bytes.
	MaxWitnessSize int

	// MaxProofSize is the maximum allowed proof size in bytes.
	MaxProofSize int

	// TargetCycles is the target number of cycles for proof generation.
	TargetCycles uint64

	// ProofSystem selects the proof system: "groth16", "plonk", or "stark".
	ProofSystem string
}

// DefaultSTFConfig returns the default STF configuration.
func DefaultSTFConfig() STFConfig {
	return STFConfig{
		MaxWitnessSize: DefaultMaxWitnessSize,
		MaxProofSize:   DefaultMaxProofSize,
		TargetCycles:   DefaultTargetCycles,
		ProofSystem:    DefaultSTFProofSystem,
	}
}

// STFInput holds all data needed for a state transition validation.
type STFInput struct {
	// PreStateRoot is the state root before the transition.
	PreStateRoot types.Hash

	// PostStateRoot is the claimed state root after the transition.
	PostStateRoot types.Hash

	// BlockHeader is the header of the block being validated.
	BlockHeader *types.Header

	// Transactions is the list of transactions in the block.
	Transactions []*types.Transaction

	// Witnesses contains the execution witnesses for stateless validation.
	Witnesses [][]byte
}

// STFOutput holds the result of an STF validation.
type STFOutput struct {
	// Valid indicates whether the state transition is correct.
	Valid bool

	// PostRoot is the computed post-state root.
	PostRoot types.Hash

	// GasUsed is the total gas consumed by the transition.
	GasUsed uint64

	// ProofData is the serialized zero-knowledge proof.
	ProofData []byte

	// CycleCount is the number of cycles used during proof generation.
	CycleCount uint64
}

// STFExecutor manages execution and validation of state transitions.
type STFExecutor struct {
	config STFConfig
}

// NewSTFExecutor creates a new STF executor with the given configuration.
func NewSTFExecutor(config STFConfig) *STFExecutor {
	return &STFExecutor{config: config}
}

// ValidateTransition validates a state transition given the input data.
// It computes the expected post-state root from the pre-state and transactions,
// compares it with the claimed PostStateRoot, and generates proof data.
func (e *STFExecutor) ValidateTransition(input STFInput) (*STFOutput, error) {
	if input.BlockHeader == nil {
		return nil, ErrSTFNilBlock
	}
	if len(input.Transactions) == 0 {
		return nil, ErrSTFEmptyTransactions
	}

	// Check witness size constraints.
	totalWitnessSize := 0
	for _, w := range input.Witnesses {
		totalWitnessSize += len(w)
	}
	if totalWitnessSize > e.config.MaxWitnessSize {
		return nil, ErrSTFWitnessTooLarge
	}

	// Compute the expected post-state root by hashing pre-state + transactions.
	// In production, this would run the full EVM state transition.
	computedPost := computePostStateRoot(input.PreStateRoot, input.Transactions, input.Witnesses)

	// Compute gas used (simulated: sum of transaction gas hints).
	gasUsed := computeGasUsed(input.Transactions)

	// Compute cycle count based on input complexity.
	cycleCount := computeCycleCount(input.Transactions, input.Witnesses)

	// Compare computed post-state with claimed post-state.
	valid := computedPost == input.PostStateRoot

	// Generate proof data: hash of (preState || computedPost || blockHash).
	blockHash := input.BlockHeader.Hash()
	proofData := crypto.Keccak256(
		input.PreStateRoot[:],
		computedPost[:],
		blockHash[:],
	)

	output := &STFOutput{
		Valid:      valid,
		PostRoot:   computedPost,
		GasUsed:    gasUsed,
		ProofData:  proofData,
		CycleCount: cycleCount,
	}

	if !valid {
		return output, ErrSTFPostRootMismatch
	}

	return output, nil
}

// GenerateWitness generates an STFInput from a block header and pre-state root.
// This collects all the data needed for stateless validation.
func (e *STFExecutor) GenerateWitness(preState types.Hash, block *types.Block) (*STFInput, error) {
	if block == nil {
		return nil, ErrSTFNilBlock
	}

	header := block.Header()
	txs := block.Transactions()

	// Collect witness data. In production, this would gather state proofs
	// for all accounts and storage slots touched by the transactions.
	// For now, generate a deterministic witness from the block data.
	witnesses := make([][]byte, len(txs))
	for i, tx := range txs {
		// Simulated witness: hash of (preState || txHash).
		txHash := tx.Hash()
		witnesses[i] = crypto.Keccak256(preState[:], txHash[:])
	}

	// Compute expected post-state root.
	postState := computePostStateRoot(preState, txs, witnesses)

	return &STFInput{
		PreStateRoot:  preState,
		PostStateRoot: postState,
		BlockHeader:   header,
		Transactions:  txs,
		Witnesses:     witnesses,
	}, nil
}

// VerifyProof verifies the integrity of a proof in an STFOutput.
// It checks that the proof data is structurally valid: non-empty, associated
// with a valid transition, and matches the expected Keccak-256 hash size
// (32 bytes for the current proof commitment scheme) or a full Groth16
// proof structure (256 bytes from the proof backend).
func (e *STFExecutor) VerifyProof(output STFOutput) bool {
	if len(output.ProofData) == 0 {
		return false
	}
	if !output.Valid {
		return false
	}

	// Accept proof data that matches either:
	// - Keccak-256 commitment (32 bytes) from STF ValidateTransition
	// - Full Groth16 proof structure (256 bytes) from the proof backend
	switch len(output.ProofData) {
	case 32:
		// Keccak-256 proof commitment from ValidateTransition.
		return true
	case groth16ProofSize:
		// Full Groth16 proof from the proof backend. Verify structural integrity
		// by checking that the proof bytes are non-zero (a zero proof would
		// indicate a failed proof generation).
		allZero := true
		for _, b := range output.ProofData {
			if b != 0 {
				allZero = false
				break
			}
		}
		return !allZero
	default:
		return false
	}
}

// computePostStateRoot deterministically derives the post-state root from
// the pre-state root, transactions, and witnesses.
func computePostStateRoot(preState types.Hash, txs []*types.Transaction, witnesses [][]byte) types.Hash {
	// Hash pre-state with all transaction hashes and witness data.
	hashInput := make([]byte, 0, 32+len(txs)*32+len(witnesses)*32)
	hashInput = append(hashInput, preState[:]...)

	for _, tx := range txs {
		txHash := tx.Hash()
		hashInput = append(hashInput, txHash[:]...)
	}
	for _, w := range witnesses {
		wHash := crypto.Keccak256(w)
		hashInput = append(hashInput, wHash...)
	}

	h := crypto.Keccak256(hashInput)
	var postRoot types.Hash
	copy(postRoot[:], h)
	return postRoot
}

// ValidateSTFInput checks that an STFInput is well-formed before execution:
//   - Input must not be nil
//   - BlockHeader must not be nil
//   - Transactions must not be empty
//   - PreStateRoot must not be zero
func ValidateSTFInput(input *STFInput) error {
	if input == nil {
		return ErrSTFNilInput
	}
	if input.BlockHeader == nil {
		return ErrSTFNilBlock
	}
	if len(input.Transactions) == 0 {
		return ErrSTFEmptyTransactions
	}
	if input.PreStateRoot == (types.Hash{}) {
		return errors.New("stf: zero pre-state root")
	}
	return nil
}

// computeGasUsed estimates gas usage from the transactions.
func computeGasUsed(txs []*types.Transaction) uint64 {
	// Simulated: 21000 base gas per transaction.
	return uint64(len(txs)) * 21000
}

// computeCycleCount estimates the number of proving cycles.
func computeCycleCount(txs []*types.Transaction, witnesses [][]byte) uint64 {
	cycles := uint64(cyclesOverhead)
	cycles += uint64(len(txs)) * cyclesPerTransaction

	for _, w := range witnesses {
		kb := uint64(len(w)+1023) / 1024
		cycles += kb * cyclesPerWitnessKB
	}

	return cycles
}
