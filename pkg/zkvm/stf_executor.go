// stf_executor.go wires the State Transition Function framework (stf.go) to
// real RISC-V execution. It encodes state transition data as guest input,
// executes it on RVCPU, and generates ZK proofs of correct execution via
// the proof backend.
//
// Part of the J+ roadmap for STF in zkISA framework.
package zkvm

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// RealSTFExecutor errors.
var (
	ErrRealSTFNilInput       = errors.New("real_stf: nil input")
	ErrRealSTFNilRegistry    = errors.New("real_stf: nil guest registry")
	ErrRealSTFEmptyTx        = errors.New("real_stf: empty transactions")
	ErrRealSTFNilBlock       = errors.New("real_stf: nil block header")
	ErrRealSTFEncodeFailed   = errors.New("real_stf: encoding failed")
	ErrRealSTFExecFailed     = errors.New("real_stf: execution failed")
	ErrRealSTFProofFailed    = errors.New("real_stf: proof generation failed")
	ErrRealSTFVerifyFailed   = errors.New("real_stf: proof verification failed")
	ErrRealSTFRootMismatch   = errors.New("real_stf: post state root mismatch")
	ErrRealSTFNoSTFProgram   = errors.New("real_stf: STF program not registered")
)

// RealSTFConfig holds configuration for the real STF executor.
type RealSTFConfig struct {
	// GasLimit for RISC-V execution of the STF program.
	GasLimit uint64

	// MaxWitnessSize is the maximum combined witness size in bytes.
	MaxWitnessSize int

	// ProofSystem selects which proof system to use.
	ProofSystem string
}

// DefaultRealSTFConfig returns default configuration values.
func DefaultRealSTFConfig() RealSTFConfig {
	return RealSTFConfig{
		GasLimit:       1 << 24,
		MaxWitnessSize: 16 * 1024 * 1024, // 16 MiB
		ProofSystem:    "stark",
	}
}

// RealSTFOutput holds the result of a real STF execution.
type RealSTFOutput struct {
	// Valid indicates the transition was correct.
	Valid bool

	// PostRoot is the computed post-state root.
	PostRoot types.Hash

	// GasUsed is total gas consumed by the state transition.
	GasUsed uint64

	// CycleCount is the number of RISC-V CPU steps executed.
	CycleCount uint64

	// ProofData is the serialized proof bytes.
	ProofData []byte

	// VerificationKey is for verifying the proof.
	VerificationKey []byte

	// TraceCommitment is the Merkle root of the execution trace.
	TraceCommitment [32]byte

	// PublicInputsHash is SHA-256 of the encoded public inputs.
	PublicInputsHash [32]byte
}

// RealSTFExecutor connects STFInput to RISC-V execution with proof generation.
type RealSTFExecutor struct {
	config   RealSTFConfig
	registry *GuestRegistry
	executor *CanonicalExecutor
	mu       sync.Mutex

	// stfProgramID is the program hash of the registered STF guest program.
	stfProgramID types.Hash
	hasProgram   bool
}

// NewRealSTFExecutor creates a new real STF executor.
func NewRealSTFExecutor(config RealSTFConfig, registry *GuestRegistry) (*RealSTFExecutor, error) {
	if registry == nil {
		return nil, ErrRealSTFNilRegistry
	}

	execConfig := DefaultCanonicalExecutorConfig()
	execConfig.GasLimit = config.GasLimit

	executor, err := NewCanonicalExecutor(registry, execConfig)
	if err != nil {
		return nil, fmt.Errorf("real_stf: create executor: %w", err)
	}

	return &RealSTFExecutor{
		config:   config,
		registry: registry,
		executor: executor,
	}, nil
}

// RegisterSTFProgram registers the STF guest program. This program encodes
// the Ethereum state transition function in RISC-V machine code.
func (re *RealSTFExecutor) RegisterSTFProgram(program []byte) (types.Hash, error) {
	re.mu.Lock()
	defer re.mu.Unlock()

	id, err := re.registry.RegisterGuest(program)
	if err != nil {
		// If already registered, just set it as the STF program.
		if errors.Is(err, ErrGuestAlreadyRegistered) {
			re.stfProgramID = id
			re.hasProgram = true
			return id, nil
		}
		return types.Hash{}, err
	}

	re.stfProgramID = id
	re.hasProgram = true
	return id, nil
}

// ExecuteSTF encodes the state transition as a RISC-V guest input, executes
// it, and returns the output with proof data.
func (re *RealSTFExecutor) ExecuteSTF(input *STFInput) (*RealSTFOutput, error) {
	if input == nil {
		return nil, ErrRealSTFNilInput
	}
	if input.BlockHeader == nil {
		return nil, ErrRealSTFNilBlock
	}
	if len(input.Transactions) == 0 {
		return nil, ErrRealSTFEmptyTx
	}

	re.mu.Lock()
	hasProgram := re.hasProgram
	programID := re.stfProgramID
	re.mu.Unlock()

	if !hasProgram {
		return nil, ErrRealSTFNoSTFProgram
	}

	// Check total witness size.
	totalWitness := 0
	for _, w := range input.Witnesses {
		totalWitness += len(w)
	}
	if totalWitness > re.config.MaxWitnessSize {
		return nil, ErrSTFWitnessTooLarge
	}

	// Encode the STF input as guest program input data.
	encoded := encodeSTFInput(input)

	// Execute the STF program on RVCPU and generate a proof.
	guestOutput, proof, err := re.executor.ExecuteAndProve(programID, encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRealSTFExecFailed, err)
	}

	// Compute the expected post-state root using the same deterministic
	// function as the base STF executor for compatibility.
	computedPost := computePostStateRoot(input.PreStateRoot, input.Transactions, input.Witnesses)
	valid := computedPost == input.PostStateRoot

	output := &RealSTFOutput{
		Valid:      valid,
		PostRoot:   computedPost,
		GasUsed:    computeGasUsed(input.Transactions),
		CycleCount: guestOutput.Steps,
	}

	if proof != nil && proof.ProofResult != nil {
		output.ProofData = proof.ProofResult.ProofBytes
		output.VerificationKey = proof.ProofResult.VerificationKey
		output.TraceCommitment = proof.ProofResult.TraceCommitment
		output.PublicInputsHash = proof.ProofResult.PublicInputsHash
	}

	if !valid {
		return output, ErrRealSTFRootMismatch
	}

	return output, nil
}

// VerifySTFProof verifies a proof from an RealSTFOutput.
func (re *RealSTFExecutor) VerifySTFProof(output *RealSTFOutput) error {
	if output == nil {
		return ErrRealSTFVerifyFailed
	}
	if len(output.ProofData) == 0 {
		return fmt.Errorf("%w: empty proof data", ErrRealSTFVerifyFailed)
	}

	re.mu.Lock()
	hasProgram := re.hasProgram
	programID := re.stfProgramID
	re.mu.Unlock()

	if !hasProgram {
		return ErrRealSTFNoSTFProgram
	}

	// Retrieve the program to compute its SHA-256 hash for verification.
	program, err := re.registry.GetGuest(programID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRealSTFVerifyFailed, err)
	}

	programHash := HashProgram(program)

	result := &ProofResult{
		ProofBytes:       output.ProofData,
		VerificationKey:  output.VerificationKey,
		TraceCommitment:  output.TraceCommitment,
		PublicInputsHash: output.PublicInputsHash,
	}

	valid, err := VerifyExecProof(result, programHash)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRealSTFVerifyFailed, err)
	}
	if !valid {
		return ErrRealSTFVerifyFailed
	}

	return nil
}

// encodeSTFInput serializes an STFInput into bytes for guest program consumption.
// Format: preStateRoot(32) || postStateRoot(32) || blockHash(32) ||
//
//	numTx(4) || [txHash(32)]... || numWitnesses(4) || [witnessLen(4) || witnessData]...
func encodeSTFInput(input *STFInput) []byte {
	blockHash := input.BlockHeader.Hash()

	// Estimate size.
	size := 32 + 32 + 32 + 4 + len(input.Transactions)*32 + 4
	for _, w := range input.Witnesses {
		size += 4 + len(w)
	}

	buf := make([]byte, 0, size)

	// Pre-state root.
	buf = append(buf, input.PreStateRoot[:]...)

	// Post-state root.
	buf = append(buf, input.PostStateRoot[:]...)

	// Block hash.
	buf = append(buf, blockHash[:]...)

	// Transaction count and hashes.
	var countBuf [4]byte
	binary.LittleEndian.PutUint32(countBuf[:], uint32(len(input.Transactions)))
	buf = append(buf, countBuf[:]...)

	for _, tx := range input.Transactions {
		txHash := tx.Hash()
		buf = append(buf, txHash[:]...)
	}

	// Witness count and data.
	binary.LittleEndian.PutUint32(countBuf[:], uint32(len(input.Witnesses)))
	buf = append(buf, countBuf[:]...)

	for _, w := range input.Witnesses {
		binary.LittleEndian.PutUint32(countBuf[:], uint32(len(w)))
		buf = append(buf, countBuf[:]...)
		buf = append(buf, w...)
	}

	return buf
}

// decodeSTFPublicInputs extracts the state roots from encoded STF input.
func decodeSTFPublicInputs(encoded []byte) (preRoot, postRoot types.Hash, err error) {
	if len(encoded) < 64 {
		return types.Hash{}, types.Hash{}, ErrRealSTFEncodeFailed
	}
	copy(preRoot[:], encoded[:32])
	copy(postRoot[:], encoded[32:64])
	return preRoot, postRoot, nil
}

// ComputeSTFCommitment computes a commitment to the STF execution that can
// be verified independently. Returns SHA-256(preRoot || postRoot || blockHash).
func ComputeSTFCommitment(preRoot, postRoot types.Hash, blockHash types.Hash) [32]byte {
	h := sha256.New()
	h.Write(preRoot[:])
	h.Write(postRoot[:])
	h.Write(blockHash[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// GenerateSTFWitness produces an STFInput suitable for real execution from
// a block and pre-state root, collecting witnesses for each transaction.
func (re *RealSTFExecutor) GenerateSTFWitness(preState types.Hash, block *types.Block) (*STFInput, error) {
	if block == nil {
		return nil, ErrRealSTFNilBlock
	}

	header := block.Header()
	txs := block.Transactions()
	if len(txs) == 0 {
		return nil, ErrRealSTFEmptyTx
	}

	// Generate deterministic witnesses per transaction.
	witnesses := make([][]byte, len(txs))
	for i, tx := range txs {
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
