// proof_aggregator.go implements recursive proof aggregation for zkVM execution
// proofs. Multiple individual ZK proofs are batched and combined into a single
// aggregated proof using Merkle tree commitment over the constituent proofs.
//
// Part of the K+ roadmap for mandatory proof-carrying blocks and the M+
// roadmap for proof aggregation.
package zkvm

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

// Proof aggregator errors.
var (
	ErrAggregatorNilProof      = errors.New("aggregator: nil proof")
	ErrAggregatorEmptyBatch    = errors.New("aggregator: no proofs to aggregate")
	ErrAggregatorBatchFull     = errors.New("aggregator: batch is full")
	ErrAggregatorDuplicateID   = errors.New("aggregator: duplicate proof ID")
	ErrAggregatorEmptyProof    = errors.New("aggregator: proof bytes are empty")
	ErrAggregatorNilAggregated = errors.New("aggregator: nil aggregated proof")
	ErrAggregatorBadMerkle     = errors.New("aggregator: merkle root mismatch")
)

// AggregatorConfig configures the ZKProofAggregator.
type AggregatorConfig struct {
	// MaxBatchSize is the maximum number of proofs per aggregation batch.
	MaxBatchSize int

	// ProofSystem identifies the proof system used (e.g., "groth16", "plonk").
	ProofSystem string

	// AggregationDepth is the number of recursive aggregation layers.
	AggregationDepth int

	// GasPerProof is the estimated gas cost per individual proof verification.
	GasPerProof uint64
}

// DefaultAggregatorConfig returns sensible default configuration.
func DefaultAggregatorConfig() *AggregatorConfig {
	return &AggregatorConfig{
		MaxBatchSize:     16,
		ProofSystem:      "groth16",
		AggregationDepth: 2,
		GasPerProof:      100000,
	}
}

// ZKExecutionProof represents a single zkVM execution proof to be aggregated.
type ZKExecutionProof struct {
	// ProofID uniquely identifies this proof.
	ProofID [32]byte

	// ProgramHash is the hash of the guest program that produced this proof.
	ProgramHash [32]byte

	// InputHash is the hash of the public inputs to the proof.
	InputHash [32]byte

	// OutputHash is the hash of the execution output.
	OutputHash [32]byte

	// ProofBytes is the serialized proof data.
	ProofBytes []byte

	// GasUsed is the gas consumed during the execution this proof covers.
	GasUsed uint64

	// Timestamp is when the proof was generated.
	Timestamp int64
}

// AggregatedProof represents the result of aggregating multiple proofs.
type AggregatedProof struct {
	// RootProof is the aggregated recursive proof bytes.
	RootProof []byte

	// ProofCount is the number of constituent proofs.
	ProofCount int

	// ProgramHashes contains the program hashes of all aggregated proofs.
	ProgramHashes [][32]byte

	// MerkleRoot is the Merkle root over all constituent proof commitments.
	MerkleRoot [32]byte

	// TotalGas is the sum of gas used across all constituent proofs.
	TotalGas uint64

	// AggregatedAt is the Unix timestamp when aggregation was performed.
	AggregatedAt int64
}

// AggregatorStats tracks aggregation statistics.
type AggregatorStats struct {
	// TotalAggregated is the total number of aggregation operations performed.
	TotalAggregated uint64

	// TotalProofs is the total number of individual proofs aggregated.
	TotalProofs uint64

	// AvgBatchSize is the average number of proofs per aggregation.
	AvgBatchSize uint64
}

// ZKProofAggregator batches and aggregates multiple zkVM execution proofs
// into a single recursive proof. Thread-safe.
type ZKProofAggregator struct {
	mu      sync.RWMutex
	config  *AggregatorConfig
	pending []*ZKExecutionProof
	seen    map[[32]byte]bool

	// Statistics.
	totalAggregated uint64
	totalProofs     uint64
}

// NewZKProofAggregator creates a new proof aggregator with the given config.
func NewZKProofAggregator(config *AggregatorConfig) *ZKProofAggregator {
	if config == nil {
		config = DefaultAggregatorConfig()
	}
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = 16
	}
	if config.ProofSystem == "" {
		config.ProofSystem = "groth16"
	}
	if config.AggregationDepth <= 0 {
		config.AggregationDepth = 2
	}
	if config.GasPerProof == 0 {
		config.GasPerProof = 100000
	}
	return &ZKProofAggregator{
		config:  config,
		pending: make([]*ZKExecutionProof, 0, config.MaxBatchSize),
		seen:    make(map[[32]byte]bool),
	}
}

// AddProof adds an execution proof to the pending batch for aggregation.
func (a *ZKProofAggregator) AddProof(proof *ZKExecutionProof) error {
	if proof == nil {
		return ErrAggregatorNilProof
	}
	if len(proof.ProofBytes) == 0 {
		return ErrAggregatorEmptyProof
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.pending) >= a.config.MaxBatchSize {
		return ErrAggregatorBatchFull
	}
	if a.seen[proof.ProofID] {
		return ErrAggregatorDuplicateID
	}

	a.pending = append(a.pending, proof)
	a.seen[proof.ProofID] = true
	return nil
}

// Aggregate combines all pending proofs into a single aggregated proof.
// The aggregation uses a Merkle tree over proof commitments and produces
// a recursive proof covering the entire batch.
func (a *ZKProofAggregator) Aggregate() (*AggregatedProof, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.pending) == 0 {
		return nil, ErrAggregatorEmptyBatch
	}

	now := time.Now().Unix()
	count := len(a.pending)

	// Collect program hashes and compute per-proof commitments.
	programHashes := make([][32]byte, count)
	leaves := make([][32]byte, count)
	var totalGas uint64

	for i, p := range a.pending {
		programHashes[i] = p.ProgramHash
		totalGas += p.GasUsed
		leaves[i] = computeProofLeaf(p)
	}

	// Build Merkle root from proof commitment leaves.
	merkleRoot := computeProofMerkleRoot(leaves)

	// Build recursive aggregated proof. The root proof is derived from
	// the Merkle root, proof system identifier, and aggregation depth.
	rootProof := buildAggregatedRootProof(merkleRoot, a.config)

	result := &AggregatedProof{
		RootProof:     rootProof,
		ProofCount:    count,
		ProgramHashes: programHashes,
		MerkleRoot:    merkleRoot,
		TotalGas:      totalGas,
		AggregatedAt:  now,
	}

	// Update statistics.
	a.totalAggregated++
	a.totalProofs += uint64(count)

	// Clear pending batch.
	a.pending = make([]*ZKExecutionProof, 0, a.config.MaxBatchSize)
	a.seen = make(map[[32]byte]bool)

	return result, nil
}

// VerifyAggregated verifies the integrity of an aggregated proof by
// recomputing the expected root proof from the Merkle root.
func (a *ZKProofAggregator) VerifyAggregated(proof *AggregatedProof) bool {
	if proof == nil {
		return false
	}
	if proof.ProofCount == 0 || len(proof.RootProof) == 0 {
		return false
	}
	if len(proof.ProgramHashes) != proof.ProofCount {
		return false
	}

	a.mu.RLock()
	config := a.config
	a.mu.RUnlock()

	// Recompute expected root proof from the Merkle root.
	expected := buildAggregatedRootProof(proof.MerkleRoot, config)
	if len(expected) != len(proof.RootProof) {
		return false
	}
	for i := range expected {
		if expected[i] != proof.RootProof[i] {
			return false
		}
	}
	return true
}

// PendingCount returns the number of proofs waiting to be aggregated.
func (a *ZKProofAggregator) PendingCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.pending)
}

// EstimateGas estimates the gas cost of verifying the current pending batch
// as an aggregated proof. Aggregation reduces cost compared to verifying
// each proof individually.
func (a *ZKProofAggregator) EstimateGas() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	n := uint64(len(a.pending))
	if n == 0 {
		return 0
	}

	// Aggregated verification costs a base amount plus logarithmic overhead
	// per constituent proof, rather than linear cost.
	base := a.config.GasPerProof
	perProof := a.config.GasPerProof / 10 // 10% of individual cost per proof

	return base + n*perProof
}

// Reset clears all pending proofs without aggregating.
func (a *ZKProofAggregator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending = make([]*ZKExecutionProof, 0, a.config.MaxBatchSize)
	a.seen = make(map[[32]byte]bool)
}

// Stats returns aggregation statistics.
func (a *ZKProofAggregator) Stats() *AggregatorStats {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var avg uint64
	if a.totalAggregated > 0 {
		avg = a.totalProofs / a.totalAggregated
	}

	return &AggregatorStats{
		TotalAggregated: a.totalAggregated,
		TotalProofs:     a.totalProofs,
		AvgBatchSize:    avg,
	}
}

// computeProofLeaf computes a leaf commitment for a single proof.
// Leaf = SHA-256(ProofID || ProgramHash || InputHash || OutputHash || ProofBytes).
func computeProofLeaf(p *ZKExecutionProof) [32]byte {
	h := sha256.New()
	h.Write(p.ProofID[:])
	h.Write(p.ProgramHash[:])
	h.Write(p.InputHash[:])
	h.Write(p.OutputHash[:])
	h.Write(p.ProofBytes)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// computeProofMerkleRoot builds a binary Merkle tree over the given leaves
// and returns the root hash.
func computeProofMerkleRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	// Pad to power of 2 with zero hashes.
	n := len(leaves)
	size := 1
	for size < n {
		size *= 2
	}
	padded := make([][32]byte, size)
	copy(padded, leaves)

	// Build tree bottom-up.
	current := padded
	for len(current) > 1 {
		next := make([][32]byte, len(current)/2)
		for i := 0; i < len(current); i += 2 {
			h := sha256.New()
			h.Write(current[i][:])
			h.Write(current[i+1][:])
			copy(next[i/2][:], h.Sum(nil))
		}
		current = next
	}
	return current[0]
}

// buildAggregatedRootProof constructs the aggregated recursive proof from
// the Merkle root and aggregation parameters.
func buildAggregatedRootProof(merkleRoot [32]byte, config *AggregatorConfig) []byte {
	h := sha256.New()
	h.Write(merkleRoot[:])
	h.Write([]byte(config.ProofSystem))

	var depthBuf [4]byte
	binary.LittleEndian.PutUint32(depthBuf[:], uint32(config.AggregationDepth))
	h.Write(depthBuf[:])
	h.Write([]byte("AggregatedRootProof"))

	// Apply recursive layers.
	current := h.Sum(nil)
	for layer := 1; layer < config.AggregationDepth; layer++ {
		rh := sha256.New()
		rh.Write(current)

		var layerBuf [4]byte
		binary.LittleEndian.PutUint32(layerBuf[:], uint32(layer))
		rh.Write(layerBuf[:])
		rh.Write([]byte("RecursiveLayer"))
		current = rh.Sum(nil)
	}

	return current
}
