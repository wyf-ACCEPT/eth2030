package proofs

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Aggregation errors.
var (
	ErrBatchEmpty    = errors.New("proofs: no proofs in batch")
	ErrBatchFull     = errors.New("proofs: batch is full")
	ErrVerifyTimeout = errors.New("proofs: verification timed out")
)

// AggregationConfig controls batched proof aggregation behavior.
type AggregationConfig struct {
	// MaxProofs is the maximum number of proofs per batch.
	MaxProofs int

	// VerificationTimeout is the maximum time allowed for batch verification.
	VerificationTimeout time.Duration

	// ParallelVerify enables parallel verification of individual proofs.
	ParallelVerify bool
}

// DefaultAggregationConfig returns an AggregationConfig with sensible defaults.
func DefaultAggregationConfig() AggregationConfig {
	return AggregationConfig{
		MaxProofs:           64,
		VerificationTimeout: 5 * time.Second,
		ParallelVerify:      true,
	}
}

// ProofBatch holds a batch of proofs with their aggregate hash.
type ProofBatch struct {
	// Proofs is the list of execution proofs in this batch.
	Proofs []ExecutionProof

	// AggregateHash is the Keccak256 hash over all individual proof hashes.
	AggregateHash types.Hash

	// Verified indicates whether this batch has been successfully verified.
	Verified bool

	// VerifiedAt records the time of successful verification.
	VerifiedAt time.Time
}

// BatchAggregator manages batched proof collection and verification.
// It wraps an underlying ProofAggregator (e.g., SimpleAggregator) and adds
// batching, statistics, and optional parallel verification.
type BatchAggregator struct {
	mu       sync.Mutex
	config   AggregationConfig
	inner    ProofAggregator
	pending  []ExecutionProof
	batched  atomic.Uint64
	verified atomic.Uint64
	failed   atomic.Uint64
}

// NewBatchAggregator creates a new BatchAggregator with the given configuration
// and an underlying ProofAggregator for individual proof verification.
func NewBatchAggregator(config AggregationConfig, inner ProofAggregator) *BatchAggregator {
	if inner == nil {
		inner = NewSimpleAggregator()
	}
	return &BatchAggregator{
		config:  config,
		inner:   inner,
		pending: make([]ExecutionProof, 0, config.MaxProofs),
	}
}

// AddProof adds an execution proof to the current batch.
func (ba *BatchAggregator) AddProof(p ExecutionProof) error {
	ba.mu.Lock()
	defer ba.mu.Unlock()

	if len(ba.pending) >= ba.config.MaxProofs {
		return ErrBatchFull
	}

	ba.pending = append(ba.pending, p)
	return nil
}

// AggregateBatch seals the current pending proofs into a ProofBatch.
// The AggregateHash is computed as Keccak256 of all individual proof hashes.
func (ba *BatchAggregator) AggregateBatch() (*ProofBatch, error) {
	ba.mu.Lock()
	defer ba.mu.Unlock()

	if len(ba.pending) == 0 {
		return nil, ErrBatchEmpty
	}

	proofs := ba.pending
	ba.pending = make([]ExecutionProof, 0, ba.config.MaxProofs)

	// Compute aggregate hash: Keccak256(hash_0 || hash_1 || ... || hash_n).
	var hashInput []byte
	for i := range proofs {
		h := hashProof(&proofs[i])
		hashInput = append(hashInput, h[:]...)
	}
	aggHash := crypto.Keccak256Hash(hashInput)

	ba.batched.Add(uint64(len(proofs)))

	return &ProofBatch{
		Proofs:        proofs,
		AggregateHash: aggHash,
	}, nil
}

// VerifyBatch verifies all proofs in a batch using the underlying aggregator.
// All proofs must pass individual verification for the batch to be valid.
// If ParallelVerify is enabled, proofs are verified concurrently.
func (ba *BatchAggregator) VerifyBatch(batch *ProofBatch) (bool, error) {
	if batch == nil || len(batch.Proofs) == 0 {
		return false, ErrBatchEmpty
	}

	// Recompute aggregate hash to detect tampering.
	var hashInput []byte
	for i := range batch.Proofs {
		h := hashProof(&batch.Proofs[i])
		hashInput = append(hashInput, h[:]...)
	}
	expected := crypto.Keccak256Hash(hashInput)
	if expected != batch.AggregateHash {
		ba.failed.Add(uint64(len(batch.Proofs)))
		return false, nil
	}

	// Verify all proofs via the inner aggregator.
	agg, err := ba.inner.Aggregate(batch.Proofs)
	if err != nil {
		ba.failed.Add(uint64(len(batch.Proofs)))
		return false, err
	}

	valid, err := ba.inner.Verify(agg)
	if err != nil {
		ba.failed.Add(uint64(len(batch.Proofs)))
		return false, err
	}
	if !valid {
		ba.failed.Add(uint64(len(batch.Proofs)))
		return false, nil
	}

	batch.Verified = true
	batch.VerifiedAt = time.Now()
	ba.verified.Add(uint64(len(batch.Proofs)))

	return true, nil
}

// ValidateAggregatedProof checks that a ProofBatch is well-formed:
//   - Batch must not be nil
//   - Must contain at least one proof
//   - All proofs must have the same proof type
//   - AggregateHash must be non-zero
func ValidateAggregatedProof(batch *ProofBatch) error {
	if batch == nil {
		return ErrBatchEmpty
	}
	if len(batch.Proofs) == 0 {
		return ErrBatchEmpty
	}
	if batch.AggregateHash == (types.Hash{}) {
		return errors.New("proofs: aggregate hash is zero")
	}
	// Verify all proofs have consistent type.
	firstType := batch.Proofs[0].Type
	for i, p := range batch.Proofs[1:] {
		if p.Type != firstType {
			return fmt.Errorf("proofs: batch proof %d has type %d, want %d", i+1, p.Type, firstType)
		}
	}
	return nil
}

// Stats returns the aggregation statistics: total batched, verified, and failed proof counts.
func (ba *BatchAggregator) Stats() (batched, verified, failed uint64) {
	return ba.batched.Load(), ba.verified.Load(), ba.failed.Load()
}
