// fraud_proof.go implements fraud proof generation and verification for
// optimistic rollups. It supports three fraud proof types (invalid state root,
// invalid receipt, invalid transaction) and provides both single-step proof
// generation and an interactive bisection protocol for dispute resolution.
// This is part of the EL roadmap: native rollups -> mandatory proofs.
package rollup

import (
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// FraudProofType identifies the kind of fraud being proven.
type FraudProofType uint8

const (
	// InvalidStateRoot indicates the block's post-state root is incorrect.
	InvalidStateRoot FraudProofType = iota + 1

	// InvalidReceipt indicates a transaction receipt is incorrect.
	InvalidReceipt

	// InvalidTransaction indicates a transaction within the block is invalid.
	InvalidTransaction
)

// Fraud proof errors.
var (
	ErrFraudProofNil            = errors.New("fraud_proof: nil fraud proof")
	ErrFraudProofTypeUnknown    = errors.New("fraud_proof: unknown proof type")
	ErrFraudProofPreStateZero   = errors.New("fraud_proof: pre-state root is zero")
	ErrFraudProofPostStateZero  = errors.New("fraud_proof: post-state root is zero")
	ErrFraudProofDataEmpty      = errors.New("fraud_proof: proof data is empty")
	ErrFraudProofInvalid        = errors.New("fraud_proof: proof verification failed")
	ErrFraudProofRootsMatch     = errors.New("fraud_proof: pre and post state roots match (no fraud)")
	ErrFraudBlockNumberZero     = errors.New("fraud_proof: block number must be non-zero")
	ErrFraudNilStateReader      = errors.New("fraud_proof: nil state reader function")
	ErrFraudNilTxExecutor       = errors.New("fraud_proof: nil transaction executor function")
	ErrFraudNilStateVerifier    = errors.New("fraud_proof: nil state verifier function")
	ErrFraudTxEmpty             = errors.New("fraud_proof: transaction data is empty")
	ErrBisectionNilClaim        = errors.New("fraud_proof: nil bisection claim")
	ErrBisectionStepIndexMatch  = errors.New("fraud_proof: bisection step indices match")
	ErrBisectionConverged       = errors.New("fraud_proof: bisection has converged to single step")
)

// FraudProof represents a proof that a rollup block contains an invalid
// state transition. It pins the exact step where the fraud occurred and
// provides the pre/post state roots and proof data needed for verification.
type FraudProof struct {
	// Type identifies what kind of fraud is being proven.
	Type FraudProofType

	// BlockNumber is the L2 block number containing the fraud.
	BlockNumber uint64

	// StepIndex is the transaction index within the block where fraud occurs.
	StepIndex uint64

	// PreStateRoot is the state root before the fraudulent step.
	PreStateRoot [32]byte

	// PostStateRoot is the claimed (incorrect) state root after the step.
	PostStateRoot [32]byte

	// ExpectedRoot is the correct post-state root (computed by the challenger).
	ExpectedRoot [32]byte

	// Proof contains the encoded proof data (state witness, tx data, etc.).
	Proof []byte
}

// StateReaderFunc reads state given a root hash, returning the state data.
type StateReaderFunc func(root [32]byte) ([]byte, error)

// TxExecutorFunc executes a transaction against a pre-state, returning
// the resulting post-state root.
type TxExecutorFunc func(preState [32]byte, tx []byte) ([32]byte, error)

// StateVerifierFunc verifies a state transition, returning true if the
// transition from preState to postState is valid given the proof.
type StateVerifierFunc func(preState, postState [32]byte, proof []byte) bool

// FraudProofGenerator generates fraud proofs by comparing expected and actual
// state roots after executing transactions.
type FraudProofGenerator struct {
	// stateReader reads state data for a given state root.
	stateReader StateReaderFunc

	// txExecutor executes a single transaction against a state.
	txExecutor TxExecutorFunc
}

// NewFraudProofGenerator creates a new fraud proof generator with the given
// state reader and transaction executor.
func NewFraudProofGenerator(
	stateReader StateReaderFunc,
	txExecutor TxExecutorFunc,
) (*FraudProofGenerator, error) {
	if stateReader == nil {
		return nil, ErrFraudNilStateReader
	}
	if txExecutor == nil {
		return nil, ErrFraudNilTxExecutor
	}
	return &FraudProofGenerator{
		stateReader: stateReader,
		txExecutor:  txExecutor,
	}, nil
}

// GenerateStateRootProof generates a fraud proof for an invalid state root.
// It takes the block number, the expected (correct) root, and the actual
// (claimed incorrect) root. The proof binds the two roots together with a
// Keccak256 commitment.
func (g *FraudProofGenerator) GenerateStateRootProof(
	blockNumber uint64,
	expectedRoot, actualRoot [32]byte,
) (*FraudProof, error) {
	if blockNumber == 0 {
		return nil, ErrFraudBlockNumberZero
	}
	if expectedRoot == ([32]byte{}) {
		return nil, ErrFraudProofPreStateZero
	}
	if actualRoot == ([32]byte{}) {
		return nil, ErrFraudProofPostStateZero
	}
	if expectedRoot == actualRoot {
		return nil, ErrFraudProofRootsMatch
	}

	// Read the state data for the expected root.
	stateData, err := g.stateReader(expectedRoot)
	if err != nil {
		return nil, err
	}

	// Build the proof: commitment over state data, expected root, and actual root.
	proof := buildStateRootProofData(expectedRoot, actualRoot, stateData)

	return &FraudProof{
		Type:          InvalidStateRoot,
		BlockNumber:   blockNumber,
		StepIndex:     0,
		PreStateRoot:  expectedRoot,
		PostStateRoot: actualRoot,
		ExpectedRoot:  expectedRoot,
		Proof:         proof,
	}, nil
}

// GenerateSingleStepProof generates a fraud proof for a single transaction
// step within a block. It executes the transaction at txIndex against the
// preState and compares the result with the claimed postState.
func (g *FraudProofGenerator) GenerateSingleStepProof(
	blockNumber uint64,
	txIndex uint64,
	preState, postState [32]byte,
	txData []byte,
) (*FraudProof, error) {
	if blockNumber == 0 {
		return nil, ErrFraudBlockNumberZero
	}
	if preState == ([32]byte{}) {
		return nil, ErrFraudProofPreStateZero
	}
	if postState == ([32]byte{}) {
		return nil, ErrFraudProofPostStateZero
	}
	if len(txData) == 0 {
		return nil, ErrFraudTxEmpty
	}

	// Execute the transaction to get the correct post-state.
	expectedRoot, err := g.txExecutor(preState, txData)
	if err != nil {
		return nil, err
	}

	// If the roots match, there is no fraud.
	if expectedRoot == postState {
		return nil, ErrFraudProofRootsMatch
	}

	// Build proof data binding preState, postState, expectedRoot, and txData.
	proof := buildSingleStepProofData(preState, postState, expectedRoot, txData)

	return &FraudProof{
		Type:          InvalidStateRoot,
		BlockNumber:   blockNumber,
		StepIndex:     txIndex,
		PreStateRoot:  preState,
		PostStateRoot: postState,
		ExpectedRoot:  expectedRoot,
		Proof:         proof,
	}, nil
}

// FraudProofVerifier verifies fraud proofs submitted by challengers.
type FraudProofVerifier struct {
	// stateVerifier checks if a state transition is valid.
	stateVerifier StateVerifierFunc
}

// NewFraudProofVerifier creates a new verifier with the given state verifier.
func NewFraudProofVerifier(verifier StateVerifierFunc) (*FraudProofVerifier, error) {
	if verifier == nil {
		return nil, ErrFraudNilStateVerifier
	}
	return &FraudProofVerifier{
		stateVerifier: verifier,
	}, nil
}

// VerifyFraudProof checks whether a fraud proof is valid. It verifies the
// proof data, checks that the pre and post state roots are consistent, and
// confirms that the claimed fraud actually exists.
// Returns true if fraud is confirmed (proof is valid), false otherwise.
func (v *FraudProofVerifier) VerifyFraudProof(proof *FraudProof) (bool, error) {
	if proof == nil {
		return false, ErrFraudProofNil
	}
	if proof.Type < InvalidStateRoot || proof.Type > InvalidTransaction {
		return false, ErrFraudProofTypeUnknown
	}
	if proof.PreStateRoot == ([32]byte{}) {
		return false, ErrFraudProofPreStateZero
	}
	if proof.PostStateRoot == ([32]byte{}) {
		return false, ErrFraudProofPostStateZero
	}
	if len(proof.Proof) == 0 {
		return false, ErrFraudProofDataEmpty
	}

	// Verify the proof data integrity.
	if !verifyProofIntegrity(proof) {
		return false, ErrFraudProofInvalid
	}

	// Check that the state verifier confirms the fraud.
	// The claimed transition (preState -> postState) should be INVALID.
	// If the verifier says it's valid, then there's no fraud.
	if v.stateVerifier(proof.PreStateRoot, proof.PostStateRoot, proof.Proof) {
		// The transition is valid, so the fraud proof is invalid.
		return false, nil
	}

	// The transition is invalid, confirming the fraud.
	return true, nil
}

// ComputeStateTransition executes a single transaction against a pre-state
// root and returns the resulting post-state root. This uses a deterministic
// Keccak256 computation as a stand-in for real EVM execution.
func ComputeStateTransition(preState [32]byte, tx []byte) ([32]byte, error) {
	if preState == ([32]byte{}) {
		return [32]byte{}, ErrFraudProofPreStateZero
	}
	if len(tx) == 0 {
		return [32]byte{}, ErrFraudTxEmpty
	}

	// Deterministic state transition: Keccak256(preState || tx).
	hash := crypto.Keccak256(preState[:], tx)
	var result [32]byte
	copy(result[:], hash)
	return result, nil
}

// InteractiveVerification implements a multi-round bisection protocol for
// narrowing down the exact step where a fraud occurred. Two parties submit
// claims about intermediate state roots, and the protocol bisects the range
// until it converges on a single step.
type InteractiveVerification struct {
	// blockNumber is the block being disputed.
	blockNumber uint64

	// startStep is the first step index in the dispute range.
	startStep uint64

	// endStep is the last step index in the dispute range.
	endStep uint64

	// claimerRoots contains the claimer's state roots at each bisection point.
	claimerRoots map[uint64][32]byte

	// challengerRoots contains the challenger's state roots at each bisection.
	challengerRoots map[uint64][32]byte

	// converged is true when the dispute has been narrowed to a single step.
	converged bool

	// disputedStep is the step index where the parties disagree, set on convergence.
	disputedStep uint64
}

// NewInteractiveVerification creates a new bisection protocol instance for
// the given block and step range.
func NewInteractiveVerification(blockNumber, startStep, endStep uint64) *InteractiveVerification {
	return &InteractiveVerification{
		blockNumber:     blockNumber,
		startStep:       startStep,
		endStep:         endStep,
		claimerRoots:    make(map[uint64][32]byte),
		challengerRoots: make(map[uint64][32]byte),
	}
}

// IsConverged returns true when the bisection has narrowed to a single step.
func (iv *InteractiveVerification) IsConverged() bool {
	return iv.converged
}

// DisputedStep returns the step index where the dispute was localized.
// Only meaningful when IsConverged returns true.
func (iv *InteractiveVerification) DisputedStep() uint64 {
	return iv.disputedStep
}

// BlockNumber returns the block being disputed.
func (iv *InteractiveVerification) BlockNumber() uint64 {
	return iv.blockNumber
}

// BisectionStep performs one round of the bisection protocol. Both parties
// submit their claimed state roots at a midpoint, and the protocol narrows
// the dispute range to the half where the roots diverge.
// Returns the updated range (start, end) after bisection.
func (iv *InteractiveVerification) BisectionStep(
	claimerRoot, challengerRoot [32]byte,
) (uint64, uint64, error) {
	if iv.converged {
		return iv.disputedStep, iv.disputedStep, ErrBisectionConverged
	}

	if iv.endStep <= iv.startStep+1 {
		iv.converged = true
		iv.disputedStep = iv.startStep
		return iv.startStep, iv.endStep, ErrBisectionConverged
	}

	// Compute midpoint.
	mid := (iv.startStep + iv.endStep) / 2

	// Store both parties' claimed roots at the midpoint.
	iv.claimerRoots[mid] = claimerRoot
	iv.challengerRoots[mid] = challengerRoot

	// If the roots match at the midpoint, the disagreement is in the
	// upper half; otherwise it's in the lower half.
	if claimerRoot == challengerRoot {
		iv.startStep = mid
	} else {
		iv.endStep = mid
	}

	// Check for convergence.
	if iv.endStep <= iv.startStep+1 {
		iv.converged = true
		iv.disputedStep = iv.startStep
	}

	return iv.startStep, iv.endStep, nil
}

// GenerateBisectionProof generates a fraud proof once the bisection protocol
// has converged to a single step. It uses the final disputed step and the
// divergent state roots from both parties.
func (iv *InteractiveVerification) GenerateBisectionProof() (*FraudProof, error) {
	if !iv.converged {
		return nil, errors.New("fraud_proof: bisection not yet converged")
	}

	claimerRoot := iv.claimerRoots[iv.disputedStep]
	challengerRoot := iv.challengerRoots[iv.disputedStep]

	// Build proof data from the converged bisection.
	var proofData []byte
	var stepBuf [8]byte
	binary.BigEndian.PutUint64(stepBuf[:], iv.disputedStep)
	proofData = append(proofData, stepBuf[:]...)
	proofData = append(proofData, claimerRoot[:]...)
	proofData = append(proofData, challengerRoot[:]...)

	// Add a commitment binding the block number and step.
	var blockBuf [8]byte
	binary.BigEndian.PutUint64(blockBuf[:], iv.blockNumber)
	commitment := crypto.Keccak256(blockBuf[:], stepBuf[:], claimerRoot[:], challengerRoot[:])
	proofData = append(proofData, commitment...)

	return &FraudProof{
		Type:          InvalidStateRoot,
		BlockNumber:   iv.blockNumber,
		StepIndex:     iv.disputedStep,
		PreStateRoot:  claimerRoot,
		PostStateRoot: challengerRoot,
		Proof:         proofData,
	}, nil
}

// --- Internal helpers ---

// buildStateRootProofData builds the proof data for a state root fraud proof.
// Format: [expectedRoot(32)] [actualRoot(32)] [stateDataHash(32)] [commitment(32)]
func buildStateRootProofData(expectedRoot, actualRoot [32]byte, stateData []byte) []byte {
	stateDataHash := crypto.Keccak256(stateData)
	commitment := crypto.Keccak256(expectedRoot[:], actualRoot[:], stateDataHash)

	proof := make([]byte, 0, 128)
	proof = append(proof, expectedRoot[:]...)
	proof = append(proof, actualRoot[:]...)
	proof = append(proof, stateDataHash...)
	proof = append(proof, commitment...)
	return proof
}

// buildSingleStepProofData builds proof data for a single-step fraud proof.
// Format: [preState(32)] [postState(32)] [expectedRoot(32)] [txHash(32)] [commitment(32)]
func buildSingleStepProofData(preState, postState, expectedRoot [32]byte, txData []byte) []byte {
	txHash := crypto.Keccak256(txData)
	commitment := crypto.Keccak256(preState[:], postState[:], expectedRoot[:], txHash)

	proof := make([]byte, 0, 160)
	proof = append(proof, preState[:]...)
	proof = append(proof, postState[:]...)
	proof = append(proof, expectedRoot[:]...)
	proof = append(proof, txHash...)
	proof = append(proof, commitment...)
	return proof
}

// verifyProofIntegrity checks the internal consistency of a fraud proof by
// recomputing and verifying its commitment hash. All proof types use the
// same commitment format: [root1(32)][root2(32)][dataHash(32)][commitment(32)].
func verifyProofIntegrity(proof *FraudProof) bool {
	if len(proof.Proof) < 128 {
		return false
	}
	stateDataHash := proof.Proof[64:96]
	commitment := proof.Proof[96:128]
	recomputed := crypto.Keccak256(proof.Proof[0:32], proof.Proof[32:64], stateDataHash)
	for i := range commitment {
		if commitment[i] != recomputed[i] {
			return false
		}
	}
	return true
}

// computeProofHash computes the Keccak256 hash of a fraud proof for
// identification purposes.
func computeProofHash(proof *FraudProof) types.Hash {
	if proof == nil {
		return types.Hash{}
	}
	var data []byte
	data = append(data, byte(proof.Type))
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], proof.BlockNumber)
	data = append(data, buf[:]...)
	binary.BigEndian.PutUint64(buf[:], proof.StepIndex)
	data = append(data, buf[:]...)
	data = append(data, proof.PreStateRoot[:]...)
	data = append(data, proof.PostStateRoot[:]...)
	data = append(data, proof.Proof...)
	return crypto.Keccak256Hash(data)
}
