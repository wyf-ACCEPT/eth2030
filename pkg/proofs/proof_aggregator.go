// proof_aggregator.go implements proof aggregation for the eth2030 client.
// This aligns with the EL EVM roadmap: proof aggregation. It provides a
// multi-proof aggregator that collects individual proofs (Merkle, KZG, STARK),
// aggregates them into a single batched proof, and supports verification,
// gas savings estimation, and batch splitting.
package proofs

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Proof aggregation errors.
var (
	ErrSingleProofNil        = errors.New("proof_agg: nil single proof")
	ErrSingleProofNoData     = errors.New("proof_agg: proof data is empty")
	ErrSingleProofBadType    = errors.New("proof_agg: unknown proof type")
	ErrAggNothingToAggregate = errors.New("proof_agg: no proofs to aggregate")
	ErrAggProofNil           = errors.New("proof_agg: nil aggregated proof")
	ErrAggProofEmpty         = errors.New("proof_agg: aggregated proof has no proofs")
	ErrAggProofTampered      = errors.New("proof_agg: aggregated proof commitment mismatch")
	ErrAggBatchSizeZero      = errors.New("proof_agg: max batch size must be positive")
	ErrAggGasNumZero         = errors.New("proof_agg: numProofs must be positive")
)

// SingleProofType identifies the type of a single proof.
type SingleProofType uint8

const (
	// MerkleProof is a Merkle inclusion/exclusion proof.
	MerkleProof SingleProofType = iota

	// KZGProof is a KZG polynomial commitment proof.
	KZGProof

	// STARKProof is a STARK-based validity proof.
	STARKProof
)

// String returns the name of the proof type.
func (t SingleProofType) String() string {
	switch t {
	case MerkleProof:
		return "Merkle"
	case KZGProof:
		return "KZG"
	case STARKProof:
		return "STARK"
	default:
		return "unknown"
	}
}

// IsValid returns true if the proof type is a known type.
func (t SingleProofType) IsValid() bool {
	return t <= STARKProof
}

// Gas costs for individual proof verification by type.
const (
	MerkleVerifyGas uint64 = 5_000
	KZGVerifyGas    uint64 = 50_000
	STARKVerifyGas  uint64 = 200_000

	// AggregateVerifyBaseGas is the base cost of verifying an aggregated proof.
	AggregateVerifyBaseGas uint64 = 100_000

	// AggregateVerifyPerProofGas is the marginal cost per proof in an aggregate.
	AggregateVerifyPerProofGas uint64 = 1_000
)

// SingleProof represents an individual proof to be aggregated.
type SingleProof struct {
	// Type is the proof system (Merkle, KZG, STARK).
	Type SingleProofType

	// Data is the encoded proof data.
	Data []byte

	// PublicInputs are the public inputs for this proof.
	PublicInputs [][]byte

	// BlockHash ties this proof to a specific block.
	BlockHash types.Hash

	// ProverID identifies the prover that generated this proof.
	ProverID string
}

// Hash computes a SHA-256 digest of the single proof for aggregation.
func (p *SingleProof) Hash() [32]byte {
	h := sha256.New()
	h.Write([]byte{byte(p.Type)})
	h.Write(p.Data)
	for _, inp := range p.PublicInputs {
		h.Write(inp)
	}
	h.Write(p.BlockHash[:])
	h.Write([]byte(p.ProverID))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// AggregatedSingleProof holds the result of aggregating multiple single proofs.
type AggregatedSingleProof struct {
	// Proofs is the list of individual proofs that were aggregated.
	Proofs []SingleProof

	// MergedCommitment is the combined commitment over all proof hashes.
	MergedCommitment types.Hash

	// BatchSize is the number of proofs in this aggregate.
	BatchSize int
}

// GasSavingsEstimate holds the result of gas savings estimation.
type GasSavingsEstimate struct {
	// IndividualGas is the total gas cost for verifying all proofs individually.
	IndividualGas uint64

	// AggregatedGas is the gas cost for verifying the aggregated proof.
	AggregatedGas uint64

	// Savings is the gas saved by aggregation.
	Savings uint64

	// SavingsPercent is the percentage of gas saved (0-100).
	SavingsPercent float64
}

// MultiProofAggregator collects and aggregates multiple single proofs.
// Thread-safe for concurrent proof submission.
type MultiProofAggregator struct {
	mu      sync.Mutex
	pending []SingleProof
}

// NewMultiProofAggregator creates a new empty MultiProofAggregator.
func NewMultiProofAggregator() *MultiProofAggregator {
	return &MultiProofAggregator{
		pending: make([]SingleProof, 0),
	}
}

// AddProof adds a single proof to the aggregation batch. The proof is
// validated before being added.
func (a *MultiProofAggregator) AddProof(proof SingleProof) error {
	if len(proof.Data) == 0 {
		return ErrSingleProofNoData
	}
	if !proof.Type.IsValid() {
		return ErrSingleProofBadType
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Deep copy the proof data to prevent external mutation.
	cp := SingleProof{
		Type:         proof.Type,
		Data:         append([]byte(nil), proof.Data...),
		PublicInputs: make([][]byte, len(proof.PublicInputs)),
		BlockHash:    proof.BlockHash,
		ProverID:     proof.ProverID,
	}
	for i, inp := range proof.PublicInputs {
		cp.PublicInputs[i] = append([]byte(nil), inp...)
	}

	a.pending = append(a.pending, cp)
	return nil
}

// PendingCount returns the number of proofs waiting to be aggregated.
func (a *MultiProofAggregator) PendingCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.pending)
}

// Aggregate combines all pending proofs into a single aggregated proof.
// The merged commitment is computed as SHA-256 of all individual proof hashes
// concatenated together. After aggregation, the pending list is cleared.
func (a *MultiProofAggregator) Aggregate() (*AggregatedSingleProof, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.pending) == 0 {
		return nil, ErrAggNothingToAggregate
	}

	proofs := a.pending
	a.pending = make([]SingleProof, 0)

	commitment := computeMergedCommitment(proofs)

	return &AggregatedSingleProof{
		Proofs:           proofs,
		MergedCommitment: commitment,
		BatchSize:        len(proofs),
	}, nil
}

// VerifyAggregated verifies an aggregated proof by recomputing the merged
// commitment and comparing it to the stored commitment.
func VerifyAggregated(agg *AggregatedSingleProof) (bool, error) {
	if agg == nil {
		return false, ErrAggProofNil
	}
	if len(agg.Proofs) == 0 {
		return false, ErrAggProofEmpty
	}
	if agg.BatchSize != len(agg.Proofs) {
		return false, ErrAggProofTampered
	}

	// Recompute the commitment.
	expected := computeMergedCommitment(agg.Proofs)
	if expected != agg.MergedCommitment {
		return false, nil
	}

	// Verify each individual proof is well-formed.
	for i := range agg.Proofs {
		if len(agg.Proofs[i].Data) == 0 {
			return false, nil
		}
		if !agg.Proofs[i].Type.IsValid() {
			return false, nil
		}
	}

	return true, nil
}

// EstimateGasSavings estimates the gas saved by aggregating numProofs proofs
// of the given type versus verifying them individually.
func EstimateGasSavings(proofType SingleProofType, numProofs int) (*GasSavingsEstimate, error) {
	if numProofs <= 0 {
		return nil, ErrAggGasNumZero
	}

	var perProofGas uint64
	switch proofType {
	case MerkleProof:
		perProofGas = MerkleVerifyGas
	case KZGProof:
		perProofGas = KZGVerifyGas
	case STARKProof:
		perProofGas = STARKVerifyGas
	default:
		return nil, ErrSingleProofBadType
	}

	individualGas := perProofGas * uint64(numProofs)
	aggregatedGas := AggregateVerifyBaseGas + AggregateVerifyPerProofGas*uint64(numProofs)

	var savings uint64
	if individualGas > aggregatedGas {
		savings = individualGas - aggregatedGas
	}

	var pct float64
	if individualGas > 0 {
		pct = math.Round(float64(savings)/float64(individualGas)*10000) / 100
	}

	return &GasSavingsEstimate{
		IndividualGas:  individualGas,
		AggregatedGas:  aggregatedGas,
		Savings:        savings,
		SavingsPercent: pct,
	}, nil
}

// SplitBatch splits the proofs in an aggregated proof into sub-batches of at
// most maxBatchSize proofs each. Each sub-batch is a fully valid aggregated
// proof with its own merged commitment.
func SplitBatch(agg *AggregatedSingleProof, maxBatchSize int) ([]*AggregatedSingleProof, error) {
	if agg == nil {
		return nil, ErrAggProofNil
	}
	if len(agg.Proofs) == 0 {
		return nil, ErrAggProofEmpty
	}
	if maxBatchSize <= 0 {
		return nil, ErrAggBatchSizeZero
	}

	var batches []*AggregatedSingleProof
	proofs := agg.Proofs

	for i := 0; i < len(proofs); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(proofs) {
			end = len(proofs)
		}

		chunk := proofs[i:end]
		commitment := computeMergedCommitment(chunk)

		batches = append(batches, &AggregatedSingleProof{
			Proofs:           chunk,
			MergedCommitment: commitment,
			BatchSize:        len(chunk),
		})
	}

	return batches, nil
}

// ProofTypeCounts returns the count of each proof type in an aggregated proof.
func ProofTypeCounts(agg *AggregatedSingleProof) map[SingleProofType]int {
	counts := make(map[SingleProofType]int)
	if agg == nil {
		return counts
	}
	for _, p := range agg.Proofs {
		counts[p.Type]++
	}
	return counts
}

// EstimateMixedGasSavings estimates gas savings for an aggregated proof with
// mixed proof types.
func EstimateMixedGasSavings(agg *AggregatedSingleProof) (*GasSavingsEstimate, error) {
	if agg == nil {
		return nil, ErrAggProofNil
	}
	if len(agg.Proofs) == 0 {
		return nil, ErrAggProofEmpty
	}

	var individualGas uint64
	for _, p := range agg.Proofs {
		switch p.Type {
		case MerkleProof:
			individualGas += MerkleVerifyGas
		case KZGProof:
			individualGas += KZGVerifyGas
		case STARKProof:
			individualGas += STARKVerifyGas
		}
	}

	aggregatedGas := AggregateVerifyBaseGas + AggregateVerifyPerProofGas*uint64(len(agg.Proofs))

	var savings uint64
	if individualGas > aggregatedGas {
		savings = individualGas - aggregatedGas
	}

	var pct float64
	if individualGas > 0 {
		pct = math.Round(float64(savings)/float64(individualGas)*10000) / 100
	}

	return &GasSavingsEstimate{
		IndividualGas:  individualGas,
		AggregatedGas:  aggregatedGas,
		Savings:        savings,
		SavingsPercent: pct,
	}, nil
}

// --- Internal helpers ---

// computeMergedCommitment computes the merged commitment for a set of proofs.
// It concatenates all individual proof hashes and takes the SHA-256 digest,
// then encodes the batch size into the final 4 bytes for binding.
func computeMergedCommitment(proofs []SingleProof) types.Hash {
	h := sha256.New()
	for i := range proofs {
		ph := proofs[i].Hash()
		h.Write(ph[:])
	}
	// Include batch size in the commitment for integrity.
	var sizeBuf [4]byte
	binary.BigEndian.PutUint32(sizeBuf[:], uint32(len(proofs)))
	h.Write(sizeBuf[:])

	var result types.Hash
	copy(result[:], h.Sum(nil))
	return result
}
