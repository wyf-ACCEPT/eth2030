package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"golang.org/x/crypto/sha3"
)

// DefaultEpochCutoff is the maximum number of epochs in the past for which
// custody proofs are accepted. Proofs for epochs older than this are rejected.
const DefaultEpochCutoff = 256

// Custody proof errors.
var (
	ErrInvalidCustodyProof = errors.New("das: invalid custody proof")
	ErrChallengeExpired    = errors.New("das: challenge has expired")
	ErrChallengeNotFound   = errors.New("das: challenge not found")
	ErrInvalidColumn       = errors.New("das: invalid column index in proof")
	ErrProofEpochTooOld    = errors.New("das: proof epoch is older than cutoff")
	ErrDeadlinePassed      = errors.New("das: challenge deadline has passed")
	ErrProofReplay         = errors.New("das: duplicate proof submission")
)

// CustodyProof attests that a node has custodied specific columns for an epoch.
// The proof is a hash commitment over the node's ID, epoch, column indices,
// and the actual data held, making it infeasible to forge without possessing
// the data.
type CustodyProof struct {
	// NodeID identifies the node claiming custody.
	NodeID [32]byte

	// Epoch is the epoch for which custody is attested.
	Epoch uint64

	// ColumnIndices lists the columns the node claims to custody.
	ColumnIndices []uint64

	// Proof is the hash commitment: keccak256(nodeID || epoch || columns || data).
	Proof []byte
}

// CustodyChallenge is issued when a node's custody claim is questioned.
// The target must respond with a valid CustodyProof before the deadline.
type CustodyChallenge struct {
	// ID uniquely identifies this challenge.
	ID types.Hash

	// Challenger is the address that issued the challenge.
	Challenger types.Address

	// Target is the address of the challenged node.
	Target types.Address

	// Column is the specific column index being challenged.
	Column uint64

	// Epoch is the epoch being challenged.
	Epoch uint64

	// Deadline is the slot by which the target must respond.
	Deadline uint64
}

// GenerateCustodyProof creates a custody proof for the given node, epoch,
// and column data. The data parameter is the raw cell data for each column
// (flattened), used as input to the proof commitment.
func GenerateCustodyProof(nodeID [32]byte, epoch uint64, columns []uint64, data []byte) *CustodyProof {
	proof := computeCustodyHash(nodeID, epoch, columns, data)
	return &CustodyProof{
		NodeID:        nodeID,
		Epoch:         epoch,
		ColumnIndices: columns,
		Proof:         proof,
	}
}

// VerifyCustodyProof validates a custody proof's structure and consistency.
// It checks that:
//   - The proof has a valid length (32 bytes)
//   - All column indices are in range
//   - Column indices are unique
//
// Note: full verification requires re-computing the hash with the actual data,
// which the verifier obtains by sampling. This function performs structural checks.
func VerifyCustodyProof(proof *CustodyProof) bool {
	if proof == nil || len(proof.Proof) != 32 {
		return false
	}
	if len(proof.ColumnIndices) == 0 {
		return false
	}

	// Check column indices are valid and unique.
	seen := make(map[uint64]bool, len(proof.ColumnIndices))
	for _, col := range proof.ColumnIndices {
		if col >= NumberOfColumns {
			return false
		}
		if seen[col] {
			return false
		}
		seen[col] = true
	}

	return true
}

// VerifyCustodyProofWithData re-computes the custody hash with the provided data
// and verifies it matches the proof.
func VerifyCustodyProofWithData(proof *CustodyProof, data []byte) bool {
	if !VerifyCustodyProof(proof) {
		return false
	}

	expected := computeCustodyHash(proof.NodeID, proof.Epoch, proof.ColumnIndices, data)
	if len(expected) != len(proof.Proof) {
		return false
	}
	for i := range expected {
		if expected[i] != proof.Proof[i] {
			return false
		}
	}
	return true
}

// CreateChallenge creates a new custody challenge targeting a specific node
// and column for a given epoch.
func CreateChallenge(challenger, target types.Address, column uint64, epoch, deadline uint64) (*CustodyChallenge, error) {
	if column >= NumberOfColumns {
		return nil, fmt.Errorf("%w: column %d >= %d", ErrInvalidColumn, column, NumberOfColumns)
	}

	id := computeChallengeID(challenger, target, column, epoch)

	return &CustodyChallenge{
		ID:         id,
		Challenger: challenger,
		Target:     target,
		Column:     column,
		Epoch:      epoch,
		Deadline:   deadline,
	}, nil
}

// RespondToChallenge validates a custody proof in response to a challenge.
// Returns true if the proof is valid for the challenged column and epoch.
func RespondToChallenge(challenge *CustodyChallenge, proof *CustodyProof) bool {
	if challenge == nil || proof == nil {
		return false
	}

	// Verify the proof covers the challenged epoch.
	if proof.Epoch != challenge.Epoch {
		return false
	}

	// Verify the proof includes the challenged column.
	found := false
	for _, col := range proof.ColumnIndices {
		if col == challenge.Column {
			found = true
			break
		}
	}
	if !found {
		return false
	}

	// Verify structural validity.
	return VerifyCustodyProof(proof)
}

// VerifyCustodyProofWithEpoch validates a custody proof with epoch bounds
// checking. It rejects proofs for epochs older than currentEpoch - cutoff.
func VerifyCustodyProofWithEpoch(proof *CustodyProof, currentEpoch, cutoff uint64) error {
	if !VerifyCustodyProof(proof) {
		return ErrInvalidCustodyProof
	}
	if cutoff > 0 && currentEpoch > cutoff && proof.Epoch < currentEpoch-cutoff {
		return fmt.Errorf("%w: proof epoch %d, current %d, cutoff %d",
			ErrProofEpochTooOld, proof.Epoch, currentEpoch, cutoff)
	}
	return nil
}

// ValidateChallengeDeadline checks that a challenge response is submitted
// before the deadline. Returns an error if currentSlot >= deadline.
func ValidateChallengeDeadline(challenge *CustodyChallenge, currentSlot uint64) error {
	if challenge == nil {
		return ErrChallengeNotFound
	}
	if currentSlot >= challenge.Deadline {
		return fmt.Errorf("%w: current slot %d >= deadline %d",
			ErrDeadlinePassed, currentSlot, challenge.Deadline)
	}
	return nil
}

// ValidateCustodyChallenge checks that a custody challenge is well-formed:
// valid column, non-zero addresses, deadline in the future, and epoch cutoff.
func ValidateCustodyChallenge(challenge *CustodyChallenge, currentSlot, currentEpoch uint64) error {
	if challenge == nil {
		return ErrChallengeNotFound
	}
	if challenge.Column >= NumberOfColumns {
		return fmt.Errorf("%w: column %d >= %d", ErrInvalidColumn, challenge.Column, NumberOfColumns)
	}
	emptyAddr := types.Address{}
	if challenge.Challenger == emptyAddr {
		return errors.New("das: challenger address is empty")
	}
	if challenge.Target == emptyAddr {
		return errors.New("das: target address is empty")
	}
	if challenge.Deadline <= currentSlot {
		return fmt.Errorf("%w: deadline %d <= current slot %d", ErrDeadlinePassed, challenge.Deadline, currentSlot)
	}
	if currentEpoch > DefaultEpochCutoff && challenge.Epoch < currentEpoch-DefaultEpochCutoff {
		return fmt.Errorf("%w: epoch %d too old (current %d, cutoff %d)",
			ErrProofEpochTooOld, challenge.Epoch, currentEpoch, DefaultEpochCutoff)
	}
	return nil
}

// replayKey uniquely identifies a proof submission for replay protection.
type replayKey struct {
	NodeID [32]byte
	Epoch  uint64
	Column uint64
}

// CustodyProofTracker tracks submitted custody proofs to prevent replay attacks.
// Thread-safe.
type CustodyProofTracker struct {
	mu   sync.RWMutex
	seen map[replayKey]bool
}

// NewCustodyProofTracker creates a new proof tracker.
func NewCustodyProofTracker() *CustodyProofTracker {
	return &CustodyProofTracker{
		seen: make(map[replayKey]bool),
	}
}

// RecordProof records a proof submission. Returns ErrProofReplay if the
// (nodeID, epoch, column) tuple has already been seen.
func (t *CustodyProofTracker) RecordProof(proof *CustodyProof) error {
	if proof == nil {
		return ErrInvalidCustodyProof
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, col := range proof.ColumnIndices {
		key := replayKey{
			NodeID: proof.NodeID,
			Epoch:  proof.Epoch,
			Column: col,
		}
		if t.seen[key] {
			return fmt.Errorf("%w: nodeID %x, epoch %d, column %d",
				ErrProofReplay, proof.NodeID[:4], proof.Epoch, col)
		}
	}

	// Record all columns.
	for _, col := range proof.ColumnIndices {
		key := replayKey{
			NodeID: proof.NodeID,
			Epoch:  proof.Epoch,
			Column: col,
		}
		t.seen[key] = true
	}
	return nil
}

// HasSeen returns true if a proof for the given (nodeID, epoch, column)
// has already been recorded.
func (t *CustodyProofTracker) HasSeen(nodeID [32]byte, epoch, column uint64) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.seen[replayKey{NodeID: nodeID, Epoch: epoch, Column: column}]
}

// PruneEpoch removes all entries for epochs older than minEpoch.
func (t *CustodyProofTracker) PruneEpoch(minEpoch uint64) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	pruned := 0
	for key := range t.seen {
		if key.Epoch < minEpoch {
			delete(t.seen, key)
			pruned++
		}
	}
	return pruned
}

// computeCustodyHash computes the custody proof hash.
// hash = keccak256(nodeID || epoch || sortedColumns || data)
func computeCustodyHash(nodeID [32]byte, epoch uint64, columns []uint64, data []byte) []byte {
	h := sha3.NewLegacyKeccak256()

	// Write node ID.
	h.Write(nodeID[:])

	// Write epoch (little-endian).
	var epochBytes [8]byte
	binary.LittleEndian.PutUint64(epochBytes[:], epoch)
	h.Write(epochBytes[:])

	// Write column indices.
	for _, col := range columns {
		var colBytes [8]byte
		binary.LittleEndian.PutUint64(colBytes[:], col)
		h.Write(colBytes[:])
	}

	// Write data.
	h.Write(data)

	return h.Sum(nil)
}

// computeChallengeID generates a unique challenge ID.
func computeChallengeID(challenger, target types.Address, column, epoch uint64) types.Hash {
	var buf [20 + 20 + 8 + 8]byte
	copy(buf[:20], challenger[:])
	copy(buf[20:40], target[:])
	binary.LittleEndian.PutUint64(buf[40:48], column)
	binary.LittleEndian.PutUint64(buf[48:56], epoch)

	h := sha3.NewLegacyKeccak256()
	h.Write(buf[:])
	var result types.Hash
	h.Sum(result[:0])
	return result
}
