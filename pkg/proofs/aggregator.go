package proofs

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/ssz"
)

// Aggregation errors.
var (
	ErrNoProofs     = errors.New("proofs: no proofs to aggregate")
	ErrNilProof     = errors.New("proofs: nil aggregated proof")
	ErrProofInvalid = errors.New("proofs: proof verification failed")
)

// ProofAggregator defines the interface for aggregating and verifying proofs.
type ProofAggregator interface {
	// Aggregate combines multiple execution proofs into an aggregated proof.
	Aggregate(proofs []ExecutionProof) (*AggregatedProof, error)

	// Verify checks the validity of an aggregated proof.
	Verify(proof *AggregatedProof) (bool, error)
}

// SimpleAggregator aggregates proofs by computing a merkle root over
// individual proof hashes, using SHA-256.
type SimpleAggregator struct{}

// NewSimpleAggregator creates a new SimpleAggregator.
func NewSimpleAggregator() *SimpleAggregator {
	return &SimpleAggregator{}
}

// Aggregate computes a merkle root over individual proof hashes.
func (a *SimpleAggregator) Aggregate(proofs []ExecutionProof) (*AggregatedProof, error) {
	if len(proofs) == 0 {
		return nil, ErrNoProofs
	}

	// Hash each proof to get leaf nodes.
	leaves := make([][32]byte, len(proofs))
	for i, p := range proofs {
		leaves[i] = hashProof(&p)
	}

	// Use SSZ merkleization to compute the root.
	root := ssz.Merkleize(leaves, 0)

	var aggregateRoot types.Hash
	copy(aggregateRoot[:], root[:])

	return &AggregatedProof{
		Proofs:        proofs,
		AggregateRoot: aggregateRoot,
		Valid:         true,
	}, nil
}

// Verify re-computes the merkle root and compares it to the stored aggregate root.
func (a *SimpleAggregator) Verify(proof *AggregatedProof) (bool, error) {
	if proof == nil {
		return false, ErrNilProof
	}
	if len(proof.Proofs) == 0 {
		return false, ErrNoProofs
	}

	leaves := make([][32]byte, len(proof.Proofs))
	for i, p := range proof.Proofs {
		leaves[i] = hashProof(&p)
	}

	root := ssz.Merkleize(leaves, 0)

	var expected types.Hash
	copy(expected[:], root[:])

	return expected == proof.AggregateRoot, nil
}

// hashProof hashes an ExecutionProof into a 32-byte leaf.
func hashProof(p *ExecutionProof) [32]byte {
	h := sha256.New()
	h.Write(p.StateRoot[:])
	h.Write(p.BlockHash[:])
	h.Write(p.ProofData)
	h.Write([]byte(p.ProverID))
	var typeBuf [4]byte
	binary.LittleEndian.PutUint32(typeBuf[:], uint32(p.Type))
	h.Write(typeBuf[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}
