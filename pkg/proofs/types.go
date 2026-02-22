// Package proofs provides proof aggregation for the ETH2030 client.
// It supports multiple proof types (ZK-SNARK, ZK-STARK, IPA, KZG) and
// allows aggregation and verification of execution proofs.
package proofs

import "github.com/eth2030/eth2030/core/types"

// ProofType identifies the cryptographic proof system used.
type ProofType uint8

const (
	ZKSNARK ProofType = 0
	ZKSTARK ProofType = 1
	IPA     ProofType = 2
	KZG     ProofType = 3
)

// String returns the name of the proof type.
func (pt ProofType) String() string {
	switch pt {
	case ZKSNARK:
		return "ZK-SNARK"
	case ZKSTARK:
		return "ZK-STARK"
	case IPA:
		return "IPA"
	case KZG:
		return "KZG"
	default:
		return "unknown"
	}
}

// ExecutionProof represents a single execution proof from a prover.
type ExecutionProof struct {
	StateRoot types.Hash
	BlockHash types.Hash
	ProofData []byte
	ProverID  string
	Type      ProofType
}

// AggregatedProof holds the result of aggregating multiple execution proofs.
type AggregatedProof struct {
	Proofs        []ExecutionProof
	AggregateRoot types.Hash
	Valid         bool
}
