package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"golang.org/x/crypto/sha3"
)

// Custody verification errors.
var (
	ErrNilCustodyProofV2       = errors.New("das: custody proof is nil")
	ErrEmptyCustodyData        = errors.New("das: custody proof data is empty")
	ErrEmptyCommitment         = errors.New("das: custody proof commitment is empty")
	ErrCellIndexOutOfRange     = errors.New("das: cell index out of range")
	ErrBlobIndexOutOfRange     = errors.New("das: blob index out of range")
	ErrSubnetOutOfRange        = errors.New("das: subnet ID out of range")
	ErrMerklePathInvalid       = errors.New("das: merkle path verification failed")
	ErrChallengeWindowExceeded = errors.New("das: challenge window exceeded")
	ErrNoRequiredCells         = errors.New("das: no required cells in challenge")
	ErrResponseCountMismatch   = errors.New("das: response count does not match required cells")
	ErrResponseChallengeID     = errors.New("das: response challenge ID mismatch")
	ErrResponseCellNotRequired = errors.New("das: response cell index not in required set")
	ErrResponseEmptyData       = errors.New("das: response data is empty")
	ErrResponseEmptyProof      = errors.New("das: response proof is empty")
)

// CustodyVerifyConfig configures the CustodyVerifier.
type CustodyVerifyConfig struct {
	// ChallengeWindow is the number of slots a challenge remains valid.
	ChallengeWindow uint64

	// MinCellsPerChallenge is the minimum number of cells requested per challenge.
	MinCellsPerChallenge uint64

	// PenaltyBase is the base penalty amount in gwei for failed custody.
	PenaltyBase uint64

	// PenaltyMultiplier scales penalties for repeated failures.
	PenaltyMultiplier uint64
}

// DefaultCustodyVerifyConfig returns a sensible default configuration.
func DefaultCustodyVerifyConfig() CustodyVerifyConfig {
	return CustodyVerifyConfig{
		ChallengeWindow:      256,
		MinCellsPerChallenge: 4,
		PenaltyBase:          1_000_000, // 1M gwei = 0.001 ETH
		PenaltyMultiplier:    2,
	}
}

// CustodyProofV2 is an enhanced custody proof that includes cell-level data
// and a Merkle inclusion path for verification.
type CustodyProofV2 struct {
	// NodeID identifies the node claiming custody.
	NodeID types.Hash

	// SubnetID is the subnet this proof is for.
	SubnetID uint64

	// BlobIndex identifies the blob in the block.
	BlobIndex uint64

	// CellIndex identifies the cell within the extended blob.
	CellIndex uint64

	// Data is the raw cell data.
	Data []byte

	// Commitment is the KZG commitment for this blob.
	Commitment []byte

	// MerklePath is the Merkle proof from the cell to the commitment root.
	MerklePath []types.Hash
}

// CustodyChallengeV2 represents a challenge issued to a node to prove custody.
type CustodyChallengeV2 struct {
	// ChallengeID uniquely identifies this challenge.
	ChallengeID types.Hash

	// NodeID is the challenged node.
	NodeID types.Hash

	// Epoch is the epoch being challenged.
	Epoch uint64

	// RequiredCells lists the cell indices the node must prove custody of.
	RequiredCells []uint64

	// Deadline is the slot by which the node must respond.
	Deadline uint64
}

// CustodyResponse is a response to a custody challenge for a single cell.
type CustodyResponse struct {
	// ChallengeID links this response to its challenge.
	ChallengeID types.Hash

	// CellIndex is the cell this response covers.
	CellIndex uint64

	// Data is the raw cell data.
	Data []byte

	// Proof is the KZG proof for this cell.
	Proof []byte
}

// PenaltyCalculator computes penalties for nodes that fail custody challenges.
type PenaltyCalculator struct {
	base       uint64
	multiplier uint64
}

// NewPenaltyCalculator creates a penalty calculator with the given parameters.
func NewPenaltyCalculator(base, multiplier uint64) *PenaltyCalculator {
	return &PenaltyCalculator{
		base:       base,
		multiplier: multiplier,
	}
}

// CalculatePenalty returns the penalty amount for a node with the given number
// of failed challenges. The penalty grows exponentially:
// penalty = base * multiplier^failedChallenges
// Capped at base * multiplier^10 to avoid overflow.
func (pc *PenaltyCalculator) CalculatePenalty(nodeID types.Hash, failedChallenges uint64) uint64 {
	if failedChallenges == 0 {
		return 0
	}

	// Cap the exponent to prevent overflow.
	exp := failedChallenges
	if exp > 10 {
		exp = 10
	}

	penalty := pc.base
	for i := uint64(1); i < exp; i++ {
		next := penalty * pc.multiplier
		// Overflow check.
		if next/pc.multiplier != penalty {
			return ^uint64(0) // max uint64 on overflow
		}
		penalty = next
	}
	return penalty
}

// CustodyVerifier verifies data availability custody proofs. It is safe
// for concurrent use.
type CustodyVerifier struct {
	config     CustodyVerifyConfig
	penalty    *PenaltyCalculator
	mu         sync.RWMutex
	challenges map[types.Hash]*CustodyChallengeV2
}

// NewCustodyVerifier creates a new verifier with the given configuration.
func NewCustodyVerifier(config CustodyVerifyConfig) *CustodyVerifier {
	return &CustodyVerifier{
		config:     config,
		penalty:    NewPenaltyCalculator(config.PenaltyBase, config.PenaltyMultiplier),
		challenges: make(map[types.Hash]*CustodyChallengeV2),
	}
}

// VerifyCustodyProof validates a CustodyProofV2 for structural correctness and
// Merkle path consistency. Returns true if valid, false with error if not.
func (cv *CustodyVerifier) VerifyCustodyProof(proof *CustodyProofV2) (bool, error) {
	if proof == nil {
		return false, ErrNilCustodyProofV2
	}
	if len(proof.Data) == 0 {
		return false, ErrEmptyCustodyData
	}
	if len(proof.Commitment) == 0 {
		return false, ErrEmptyCommitment
	}
	if proof.CellIndex >= CellsPerExtBlob {
		return false, fmt.Errorf("%w: %d >= %d", ErrCellIndexOutOfRange, proof.CellIndex, CellsPerExtBlob)
	}
	if proof.BlobIndex >= MaxBlobCommitmentsPerBlock {
		return false, fmt.Errorf("%w: %d >= %d", ErrBlobIndexOutOfRange, proof.BlobIndex, MaxBlobCommitmentsPerBlock)
	}
	if proof.SubnetID >= DataColumnSidecarSubnetCount {
		return false, fmt.Errorf("%w: %d >= %d", ErrSubnetOutOfRange, proof.SubnetID, DataColumnSidecarSubnetCount)
	}

	// Verify the Merkle path if present.
	if len(proof.MerklePath) > 0 {
		if !verifyMerklePath(proof.Data, proof.CellIndex, proof.MerklePath, proof.Commitment) {
			return false, ErrMerklePathInvalid
		}
	}

	return true, nil
}

// GenerateCustodyChallenge creates a challenge for a node to prove custody
// of specific cells in the given epoch.
func (cv *CustodyVerifier) GenerateCustodyChallenge(nodeID types.Hash, epoch uint64) (*CustodyChallengeV2, error) {
	minCells := cv.config.MinCellsPerChallenge
	if minCells == 0 {
		minCells = 4
	}

	// Derive required cells deterministically from nodeID and epoch.
	cells := deriveCellIndices(nodeID, epoch, minCells)
	if len(cells) == 0 {
		return nil, ErrNoRequiredCells
	}

	// Compute challenge ID.
	challengeID := computeChallengeIDV2(nodeID, epoch, cells)

	deadline := (epoch+1)*32 + cv.config.ChallengeWindow

	challenge := &CustodyChallengeV2{
		ChallengeID:   challengeID,
		NodeID:        nodeID,
		Epoch:         epoch,
		RequiredCells: cells,
		Deadline:      deadline,
	}

	cv.mu.Lock()
	cv.challenges[challengeID] = challenge
	cv.mu.Unlock()

	return challenge, nil
}

// RespondToChallenge processes a set of custody responses for a challenge.
// All required cells must be covered.
func (cv *CustodyVerifier) RespondToChallenge(challenge *CustodyChallengeV2, responses []*CustodyResponse) error {
	if challenge == nil {
		return ErrChallengeNotFound
	}
	if len(responses) < len(challenge.RequiredCells) {
		return fmt.Errorf("%w: got %d, need %d",
			ErrResponseCountMismatch, len(responses), len(challenge.RequiredCells))
	}

	// Build set of required cells.
	required := make(map[uint64]bool, len(challenge.RequiredCells))
	for _, c := range challenge.RequiredCells {
		required[c] = true
	}

	// Validate each response.
	for _, resp := range responses {
		if !cv.ValidateCustodyResponse(challenge, resp) {
			return fmt.Errorf("invalid response for cell %d", resp.CellIndex)
		}
		delete(required, resp.CellIndex)
	}

	// Check all required cells were covered.
	if len(required) > 0 {
		return fmt.Errorf("%w: %d cells still uncovered",
			ErrResponseCountMismatch, len(required))
	}

	// Clean up the tracked challenge.
	cv.mu.Lock()
	delete(cv.challenges, challenge.ChallengeID)
	cv.mu.Unlock()

	return nil
}

// ValidateCustodyResponse checks whether a single custody response is valid
// for the given challenge.
func (cv *CustodyVerifier) ValidateCustodyResponse(challenge *CustodyChallengeV2, response *CustodyResponse) bool {
	if challenge == nil || response == nil {
		return false
	}
	if response.ChallengeID != challenge.ChallengeID {
		return false
	}
	if len(response.Data) == 0 {
		return false
	}
	if len(response.Proof) == 0 {
		return false
	}

	// Check that the cell index is in the required set.
	found := false
	for _, c := range challenge.RequiredCells {
		if c == response.CellIndex {
			found = true
			break
		}
	}
	if !found {
		return false
	}

	// Verify the response proof: hash(challengeID || cellIndex || data) == proof.
	expected := computeResponseHash(response.ChallengeID, response.CellIndex, response.Data)
	if len(expected) != len(response.Proof) {
		return false
	}
	for i := range expected {
		if expected[i] != response.Proof[i] {
			return false
		}
	}

	return true
}

// CalculatePenalty returns the penalty for a node with the given number of
// failed challenges.
func (cv *CustodyVerifier) CalculatePenalty(nodeID types.Hash, failedChallenges uint64) uint64 {
	return cv.penalty.CalculatePenalty(nodeID, failedChallenges)
}

// PendingChallenges returns the number of outstanding challenges.
func (cv *CustodyVerifier) PendingChallenges() int {
	cv.mu.RLock()
	defer cv.mu.RUnlock()
	return len(cv.challenges)
}

// --- Internal helpers ---

// verifyMerklePath checks a simplified Merkle proof. We hash the data with each
// sibling in the path, alternating sides based on the cell index bits.
func verifyMerklePath(data []byte, cellIndex uint64, path []types.Hash, commitment []byte) bool {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	current := h.Sum(nil)

	idx := cellIndex
	for _, sibling := range path {
		h2 := sha3.NewLegacyKeccak256()
		if idx%2 == 0 {
			h2.Write(current)
			h2.Write(sibling[:])
		} else {
			h2.Write(sibling[:])
			h2.Write(current)
		}
		current = h2.Sum(nil)
		idx /= 2
	}

	// Compare against the commitment hash.
	commitHash := sha3.NewLegacyKeccak256()
	commitHash.Write(commitment)
	expected := commitHash.Sum(nil)

	if len(current) != len(expected) {
		return false
	}
	for i := range current {
		if current[i] != expected[i] {
			return false
		}
	}
	return true
}

// deriveCellIndices deterministically generates cell indices for a challenge
// using keccak256(nodeID || epoch || counter).
func deriveCellIndices(nodeID types.Hash, epoch uint64, count uint64) []uint64 {
	cells := make([]uint64, 0, count)
	seen := make(map[uint64]bool)

	var epochBuf [8]byte
	binary.LittleEndian.PutUint64(epochBuf[:], epoch)

	counter := uint64(0)
	for uint64(len(cells)) < count && counter < count*10 {
		var counterBuf [8]byte
		binary.LittleEndian.PutUint64(counterBuf[:], counter)

		h := sha3.NewLegacyKeccak256()
		h.Write(nodeID[:])
		h.Write(epochBuf[:])
		h.Write(counterBuf[:])
		digest := h.Sum(nil)

		idx := binary.LittleEndian.Uint64(digest[:8]) % CellsPerExtBlob
		if !seen[idx] {
			seen[idx] = true
			cells = append(cells, idx)
		}
		counter++
	}
	return cells
}

// computeChallengeIDV2 generates a unique challenge ID.
func computeChallengeIDV2(nodeID types.Hash, epoch uint64, cells []uint64) types.Hash {
	h := sha3.NewLegacyKeccak256()
	h.Write(nodeID[:])

	var epochBuf [8]byte
	binary.LittleEndian.PutUint64(epochBuf[:], epoch)
	h.Write(epochBuf[:])

	for _, c := range cells {
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], c)
		h.Write(buf[:])
	}

	var result types.Hash
	h.Sum(result[:0])
	return result
}

// computeResponseHash computes the expected proof hash for a custody response.
func computeResponseHash(challengeID types.Hash, cellIndex uint64, data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(challengeID[:])

	var cellBuf [8]byte
	binary.LittleEndian.PutUint64(cellBuf[:], cellIndex)
	h.Write(cellBuf[:])

	h.Write(data)
	return h.Sum(nil)
}

// MakeResponseProof creates a valid proof for a custody response. This is a
// helper for nodes responding to challenges.
func MakeResponseProof(challengeID types.Hash, cellIndex uint64, data []byte) []byte {
	return computeResponseHash(challengeID, cellIndex, data)
}
