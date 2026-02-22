// Package zkvm provides a framework for zkVM guest execution and proof
// verification, supporting EIP-8079 native rollup proof-carrying transactions.
package zkvm

import (
	"github.com/eth2030/eth2030/core/types"
)

// GuestProgram represents a compiled zkVM guest program that implements
// the Ethereum state transition function.
type GuestProgram struct {
	// Code is the compiled guest program bytecode.
	Code []byte

	// EntryPoint is the function to call within the guest program.
	EntryPoint string

	// Version identifies the STF version this program implements.
	Version uint32
}

// VerificationKey is the public verification key for a zkVM proof system.
// It is derived from the guest program and can verify proofs produced by it.
type VerificationKey struct {
	// Data is the serialized verification key.
	Data []byte

	// ProgramHash is the hash of the guest program this key verifies.
	ProgramHash types.Hash
}

// Proof represents a zero-knowledge proof of correct execution.
type Proof struct {
	// Data is the serialized proof.
	Data []byte

	// PublicInputs are the public inputs to the proof circuit.
	// For native rollups, this includes pre/post state roots.
	PublicInputs []byte
}

// ProverBackend defines the interface for a zkVM proving system.
// Implementations provide proof generation and verification for
// specific backends (e.g., SP1, RISC Zero, mock).
type ProverBackend interface {
	// Name returns the name of the prover backend.
	Name() string

	// Prove generates a proof of correct execution.
	Prove(program *GuestProgram, input []byte) (*Proof, error)

	// Verify checks a proof against a verification key.
	Verify(vk *VerificationKey, proof *Proof) (bool, error)
}

// ExecutionResult holds the result of guest program execution.
type ExecutionResult struct {
	// PreStateRoot is the state root before execution.
	PreStateRoot types.Hash

	// PostStateRoot is the state root after execution.
	PostStateRoot types.Hash

	// ReceiptsRoot is the receipts trie root after execution.
	ReceiptsRoot types.Hash

	// GasUsed is the total gas consumed.
	GasUsed uint64

	// Success indicates whether execution completed without error.
	Success bool
}

// GuestInput is the input provided to the zkVM guest program.
// It mirrors the keeper/main.go Payload structure.
type GuestInput struct {
	// ChainID identifies the chain being executed.
	ChainID uint64

	// BlockData is the RLP-encoded block.
	BlockData []byte

	// WitnessData is the RLP-encoded execution witness.
	WitnessData []byte
}
