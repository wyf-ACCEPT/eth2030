package das

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
	"golang.org/x/crypto/sha3"
)

// Custody proof errors.
var (
	ErrInvalidCustodyProof = errors.New("das: invalid custody proof")
	ErrChallengeExpired    = errors.New("das: challenge has expired")
	ErrChallengeNotFound   = errors.New("das: challenge not found")
	ErrInvalidColumn       = errors.New("das: invalid column index in proof")
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
