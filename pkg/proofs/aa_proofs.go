// aa_proofs.go implements zero-knowledge proofs for account abstraction
// validation (2030++ era). These proofs allow AA validation to be verified
// without revealing the internal details of the validation logic, enabling
// privacy-preserving account abstraction on Ethereum L1.
package proofs

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// AA proof errors.
var (
	ErrAANilUserOp          = errors.New("aa_proofs: nil user operation")
	ErrAAEmptyEntryPoint    = errors.New("aa_proofs: empty entry point address")
	ErrAAEntryPointNotAllowed = errors.New("aa_proofs: entry point not in allowed set")
	ErrAAExceedsMaxGas      = errors.New("aa_proofs: verification gas exceeds maximum")
	ErrAANilProof           = errors.New("aa_proofs: nil proof")
	ErrAAInvalidProof       = errors.New("aa_proofs: proof verification failed")
	ErrAAEmptyBatch         = errors.New("aa_proofs: empty proof batch")
	ErrAACompressFailed     = errors.New("aa_proofs: proof compression failed")
	ErrAADecompressFailed   = errors.New("aa_proofs: proof decompression failed")
	ErrAADecompressTooShort = errors.New("aa_proofs: compressed data too short")
)

// Default AA proof configuration constants.
const (
	DefaultMaxVerificationGas uint64 = 1_500_000
	AAProofVersion            byte   = 0x01

	// Header sizes for compressed proof format.
	compressedHeaderSize = 1 + 32 + 32 + 20 + 8 + 4 // version + commitment + validationHash + entryPoint + gas + dataLen
)

// UserOperation represents an ERC-4337-style user operation submitted to
// the AA entry point for validation. This is the input to proof generation.
type UserOperation struct {
	Sender               types.Address
	Nonce                uint64
	InitCode             []byte
	CallData             []byte
	CallGasLimit         uint64
	VerificationGasLimit uint64
	PreVerificationGas   uint64
	MaxFeePerGas         uint64
	MaxPriorityFeePerGas uint64
	PaymasterAndData     []byte
	Signature            []byte
}

// Hash computes the canonical hash of the user operation.
func (op *UserOperation) Hash() types.Hash {
	var buf []byte
	buf = append(buf, op.Sender[:]...)
	buf = binary.BigEndian.AppendUint64(buf, op.Nonce)
	buf = append(buf, crypto.Keccak256(op.InitCode)...)
	buf = append(buf, crypto.Keccak256(op.CallData)...)
	buf = binary.BigEndian.AppendUint64(buf, op.CallGasLimit)
	buf = binary.BigEndian.AppendUint64(buf, op.VerificationGasLimit)
	buf = binary.BigEndian.AppendUint64(buf, op.PreVerificationGas)
	buf = binary.BigEndian.AppendUint64(buf, op.MaxFeePerGas)
	buf = binary.BigEndian.AppendUint64(buf, op.MaxPriorityFeePerGas)
	buf = append(buf, crypto.Keccak256(op.PaymasterAndData)...)
	return crypto.Keccak256Hash(buf)
}

// AAProofConfig configures the AA proof generation and verification system.
type AAProofConfig struct {
	// MaxVerificationGas is the maximum gas allowed for on-chain verification.
	MaxVerificationGas uint64

	// AllowedEntryPoints is the set of entry point addresses accepted for
	// proof generation. If empty, the canonical entry point is used.
	AllowedEntryPoints []types.Address
}

// DefaultAAProofConfig returns a config with the canonical AA entry point
// and default gas limits.
func DefaultAAProofConfig() *AAProofConfig {
	return &AAProofConfig{
		MaxVerificationGas: DefaultMaxVerificationGas,
		AllowedEntryPoints: []types.Address{types.HexToAddress("0x0000000000000000000000000000000000007701")},
	}
}

// isEntryPointAllowed checks if an entry point is in the allowed set.
func (c *AAProofConfig) isEntryPointAllowed(ep types.Address) bool {
	if len(c.AllowedEntryPoints) == 0 {
		return true
	}
	for _, allowed := range c.AllowedEntryPoints {
		if allowed == ep {
			return true
		}
	}
	return false
}

// AAProof is a zero-knowledge proof that an AA user operation passed
// validation without revealing validation internals.
type AAProof struct {
	// Commitment is the cryptographic commitment binding the proof to
	// a specific user operation and entry point.
	Commitment types.Hash

	// ValidationHash is the hash of the validation result. In production
	// this would be the SNARK/STARK output; here it is a deterministic
	// stub for the proof-of-concept.
	ValidationHash types.Hash

	// EntryPoint is the entry point contract address used during validation.
	EntryPoint types.Address

	// GasUsed is the gas consumed by the proof verification circuit.
	GasUsed uint64

	// ProofData is the serialized ZK proof bytes.
	ProofData []byte
}

// ProofSize returns the total byte size of the proof.
func (p *AAProof) ProofSize() int {
	if p == nil {
		return 0
	}
	// Two 32-byte hashes + 20-byte address + 8-byte gas + proof data.
	return 32 + 32 + 20 + 8 + len(p.ProofData)
}

// AAProofGenerator manages zero-knowledge proof generation for AA validation.
type AAProofGenerator struct {
	mu     sync.RWMutex
	config *AAProofConfig
}

// NewAAProofGenerator creates a new generator with the given config.
func NewAAProofGenerator(config *AAProofConfig) *AAProofGenerator {
	if config == nil {
		config = DefaultAAProofConfig()
	}
	return &AAProofGenerator{config: config}
}

// GenerateValidationProof creates a ZK proof that the given user operation
// passes validation at the specified entry point. The proof commits to the
// operation hash and entry point, and produces a deterministic validation
// hash using Keccak256 as a stub for a real ZK circuit.
func (g *AAProofGenerator) GenerateValidationProof(userOp *UserOperation, entryPoint types.Address) (*AAProof, error) {
	if userOp == nil {
		return nil, ErrAANilUserOp
	}
	if entryPoint.IsZero() {
		return nil, ErrAAEmptyEntryPoint
	}

	g.mu.RLock()
	config := g.config
	g.mu.RUnlock()

	if !config.isEntryPointAllowed(entryPoint) {
		return nil, ErrAAEntryPointNotAllowed
	}

	if userOp.VerificationGasLimit > config.MaxVerificationGas {
		return nil, ErrAAExceedsMaxGas
	}

	opHash := userOp.Hash()

	// Commitment: H(opHash || entryPoint).
	commitment := crypto.Keccak256Hash(opHash[:], entryPoint[:])

	// Validation hash: H("aa-validation" || commitment || signature).
	// This simulates the ZK circuit output binding the proof.
	validationHash := crypto.Keccak256Hash(
		[]byte("aa-validation"),
		commitment[:],
		userOp.Signature,
	)

	// Proof data: H("aa-proof" || commitment || validationHash).
	proofData := crypto.Keccak256(
		[]byte("aa-proof"),
		commitment[:],
		validationHash[:],
	)

	// Simulate gas: base cost + proportional to operation complexity.
	gasUsed := uint64(50_000) + uint64(len(userOp.CallData))*16

	return &AAProof{
		Commitment:     commitment,
		ValidationHash: validationHash,
		EntryPoint:     entryPoint,
		GasUsed:        gasUsed,
		ProofData:      proofData,
	}, nil
}

// VerifyValidationProof checks that an AA proof is internally consistent.
// It recomputes the proof data from the commitment and validation hash,
// and verifies the binding.
func VerifyValidationProof(proof *AAProof) bool {
	if proof == nil {
		return false
	}
	if proof.Commitment.IsZero() || proof.ValidationHash.IsZero() {
		return false
	}
	if proof.EntryPoint.IsZero() {
		return false
	}
	if len(proof.ProofData) == 0 {
		return false
	}

	// Recompute expected proof data.
	expected := crypto.Keccak256(
		[]byte("aa-proof"),
		proof.Commitment[:],
		proof.ValidationHash[:],
	)
	if len(expected) != len(proof.ProofData) {
		return false
	}
	for i := range expected {
		if expected[i] != proof.ProofData[i] {
			return false
		}
	}
	return true
}

// BatchVerifyAAProofs verifies multiple AA proofs and returns counts of
// valid and invalid proofs.
func BatchVerifyAAProofs(proofs []*AAProof) (valid, invalid int) {
	if len(proofs) == 0 {
		return 0, 0
	}
	for _, p := range proofs {
		if VerifyValidationProof(p) {
			valid++
		} else {
			invalid++
		}
	}
	return valid, invalid
}

// CompressProof serialises an AAProof into a compact binary format:
//
//	[version:1][commitment:32][validationHash:32][entryPoint:20][gasUsed:8][dataLen:4][proofData:variable]
func CompressProof(proof *AAProof) ([]byte, error) {
	if proof == nil {
		return nil, ErrAANilProof
	}
	if len(proof.ProofData) == 0 {
		return nil, ErrAACompressFailed
	}

	size := compressedHeaderSize + len(proof.ProofData)
	buf := make([]byte, size)
	off := 0

	buf[off] = AAProofVersion
	off++
	copy(buf[off:off+32], proof.Commitment[:])
	off += 32
	copy(buf[off:off+32], proof.ValidationHash[:])
	off += 32
	copy(buf[off:off+20], proof.EntryPoint[:])
	off += 20
	binary.BigEndian.PutUint64(buf[off:off+8], proof.GasUsed)
	off += 8
	binary.BigEndian.PutUint32(buf[off:off+4], uint32(len(proof.ProofData)))
	off += 4
	copy(buf[off:], proof.ProofData)

	return buf, nil
}

// DecompressProof deserialises compressed bytes back into an AAProof.
func DecompressProof(data []byte) (*AAProof, error) {
	if len(data) < compressedHeaderSize {
		return nil, ErrAADecompressTooShort
	}
	off := 0

	version := data[off]
	off++
	if version != AAProofVersion {
		return nil, ErrAADecompressFailed
	}

	var commitment types.Hash
	copy(commitment[:], data[off:off+32])
	off += 32

	var validationHash types.Hash
	copy(validationHash[:], data[off:off+32])
	off += 32

	var entryPoint types.Address
	copy(entryPoint[:], data[off:off+20])
	off += 20

	gasUsed := binary.BigEndian.Uint64(data[off : off+8])
	off += 8

	dataLen := binary.BigEndian.Uint32(data[off : off+4])
	off += 4

	if uint32(len(data)-off) < dataLen {
		return nil, ErrAADecompressTooShort
	}

	proofData := make([]byte, dataLen)
	copy(proofData, data[off:off+int(dataLen)])

	return &AAProof{
		Commitment:     commitment,
		ValidationHash: validationHash,
		EntryPoint:     entryPoint,
		GasUsed:        gasUsed,
		ProofData:      proofData,
	}, nil
}
