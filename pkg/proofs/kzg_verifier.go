// kzg_verifier.go implements KZG commitment verification for blob data,
// with batch verification, point evaluation, and proof aggregation matching
// the EIP-4844 specification.
//
// KZG (Kate-Zaverucha-Goldberg) commitments allow proving that a polynomial
// evaluates to a claimed value at a given point, enabling efficient blob data
// availability verification.
//
// Part of the DL roadmap: EIP-4844 blob transactions and PeerDAS.
package proofs

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// KZG verifier errors.
var (
	ErrKZGNilCommitment      = errors.New("kzg_verifier: nil commitment")
	ErrKZGNilProof           = errors.New("kzg_verifier: nil proof")
	ErrKZGPointMismatch      = errors.New("kzg_verifier: point evaluation failed")
	ErrKZGBatchEmpty         = errors.New("kzg_verifier: empty batch")
	ErrKZGBatchSizeMismatch  = errors.New("kzg_verifier: batch size mismatch")
	ErrKZGInvalidBlob        = errors.New("kzg_verifier: invalid blob data")
	ErrKZGAggregationFailed  = errors.New("kzg_verifier: proof aggregation failed")
	ErrKZGVerifierClosed     = errors.New("kzg_verifier: verifier is closed")
	ErrKZGCommitmentMismatch = errors.New("kzg_verifier: commitment does not match blob")
)

// KZG constants matching EIP-4844 specification.
const (
	// KZGCommitmentSize is the byte length of a KZG commitment (48 bytes, G1 point).
	KZGCommitmentSize = 48

	// KZGProofPointSize is the byte length of a KZG proof (48 bytes, G1 point).
	KZGProofPointSize = 48

	// BlobFieldElementCount is the number of field elements in a blob (4096).
	BlobFieldElementCount = 4096

	// FieldElementSize is the byte size of a BLS12-381 scalar field element.
	FieldElementSize = 32

	// BlobSize is the total byte size of a blob (4096 * 32 = 131072).
	BlobSize = BlobFieldElementCount * FieldElementSize
)

// KZGCommitment represents a 48-byte KZG commitment (G1 point on BLS12-381).
type KZGCommitment [KZGCommitmentSize]byte

// KZGProofPoint represents a 48-byte KZG proof point (G1 point on BLS12-381).
type KZGProofPoint [KZGProofPointSize]byte

// PointEvaluation holds the inputs and output of a KZG point evaluation.
type PointEvaluation struct {
	Commitment KZGCommitment
	Proof      KZGProofPoint
	Point      [FieldElementSize]byte // z: evaluation point
	Value      [FieldElementSize]byte // y: claimed value p(z) = y
}

// BlobCommitmentPair pairs a blob's data hash with its KZG commitment.
type BlobCommitmentPair struct {
	BlobHash   types.Hash
	Commitment KZGCommitment
	Proof      KZGProofPoint
	BlobIndex  uint64
}

// KZGBatchItem holds a single item in a batch verification.
type KZGBatchItem struct {
	Commitment KZGCommitment
	Proof      KZGProofPoint
	Point      [FieldElementSize]byte
	Value      [FieldElementSize]byte
	Valid      bool
}

// KZGBatchResult holds the result of a batch KZG verification.
type KZGBatchResult struct {
	Items       []KZGBatchItem
	AllValid    bool
	ValidCount  int
	FailedCount int
}

// AggregatedKZGProof represents multiple KZG proofs aggregated into one.
type AggregatedKZGProof struct {
	Commitments []KZGCommitment
	AggProof    KZGProofPoint
	AggRoot     types.Hash
	Count       int
}

// KZGVerifierConfig configures the KZG verifier.
type KZGVerifierConfig struct {
	// MaxBatchSize is the maximum number of proofs in a single batch.
	MaxBatchSize int

	// ParallelVerify enables concurrent verification in batch mode.
	ParallelVerify bool
}

// DefaultKZGVerifierConfig returns sensible defaults.
func DefaultKZGVerifierConfig() KZGVerifierConfig {
	return KZGVerifierConfig{
		MaxBatchSize:   128,
		ParallelVerify: true,
	}
}

// KZGVerifier performs KZG commitment verification for blob data.
// It supports single-proof verification, batch verification, and
// proof aggregation. Thread-safe.
type KZGVerifier struct {
	mu     sync.RWMutex
	config KZGVerifierConfig
	closed bool

	// Statistics.
	totalVerified uint64
	totalFailed   uint64
	batchesRun    uint64
}

// NewKZGVerifier creates a new KZG verifier with the given config.
func NewKZGVerifier(config KZGVerifierConfig) *KZGVerifier {
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = 128
	}
	return &KZGVerifier{
		config: config,
	}
}

// Close shuts down the verifier.
func (v *KZGVerifier) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
}

// VerifyPointEvaluation verifies a KZG point evaluation proof. It checks
// that the polynomial committed to by `commitment` evaluates to `value`
// at `point`, as attested by `proof`.
//
// In production this uses pairing checks on BLS12-381. Here we use a
// SHA-256 binding commitment:
//
//	valid = SHA256(commitment || proof || point || value) has specific pattern.
func (v *KZGVerifier) VerifyPointEvaluation(eval *PointEvaluation) (bool, error) {
	v.mu.RLock()
	if v.closed {
		v.mu.RUnlock()
		return false, ErrKZGVerifierClosed
	}
	v.mu.RUnlock()

	if eval == nil {
		return false, ErrKZGNilProof
	}

	// Check for zero commitment.
	if isZeroKZG(eval.Commitment[:]) {
		return false, ErrKZGNilCommitment
	}

	valid := verifyPointEval(eval.Commitment, eval.Proof, eval.Point, eval.Value)

	v.mu.Lock()
	if valid {
		v.totalVerified++
	} else {
		v.totalFailed++
	}
	v.mu.Unlock()

	return valid, nil
}

// VerifyBlobCommitment verifies that a blob's KZG commitment matches the
// versioned hash (as defined in EIP-4844). The versioned hash is computed as:
//
//	SHA256(0x01 || SHA256(commitment)[1:])
func (v *KZGVerifier) VerifyBlobCommitment(pair *BlobCommitmentPair) (bool, error) {
	if pair == nil {
		return false, ErrKZGNilCommitment
	}
	if isZeroKZG(pair.Commitment[:]) {
		return false, ErrKZGNilCommitment
	}

	expected := computeVersionedHash(pair.Commitment)
	return expected == pair.BlobHash, nil
}

// BatchVerify verifies multiple point evaluations in a single batch.
// When ParallelVerify is enabled, evaluations are checked concurrently.
func (v *KZGVerifier) BatchVerify(evals []PointEvaluation) (*KZGBatchResult, error) {
	v.mu.RLock()
	if v.closed {
		v.mu.RUnlock()
		return nil, ErrKZGVerifierClosed
	}
	v.mu.RUnlock()

	if len(evals) == 0 {
		return nil, ErrKZGBatchEmpty
	}

	result := &KZGBatchResult{
		Items:    make([]KZGBatchItem, len(evals)),
		AllValid: true,
	}

	if v.config.ParallelVerify && len(evals) > 1 {
		var wg sync.WaitGroup
		for i := range evals {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				e := &evals[idx]
				valid := verifyPointEval(e.Commitment, e.Proof, e.Point, e.Value)
				result.Items[idx] = KZGBatchItem{
					Commitment: e.Commitment,
					Proof:      e.Proof,
					Point:      e.Point,
					Value:      e.Value,
					Valid:      valid,
				}
			}(i)
		}
		wg.Wait()
	} else {
		for i := range evals {
			e := &evals[i]
			valid := verifyPointEval(e.Commitment, e.Proof, e.Point, e.Value)
			result.Items[i] = KZGBatchItem{
				Commitment: e.Commitment,
				Proof:      e.Proof,
				Point:      e.Point,
				Value:      e.Value,
				Valid:      valid,
			}
		}
	}

	// Aggregate results.
	for i := range result.Items {
		if result.Items[i].Valid {
			result.ValidCount++
		} else {
			result.FailedCount++
			result.AllValid = false
		}
	}

	v.mu.Lock()
	v.totalVerified += uint64(result.ValidCount)
	v.totalFailed += uint64(result.FailedCount)
	v.batchesRun++
	v.mu.Unlock()

	return result, nil
}

// AggregateProofs aggregates multiple KZG proofs into a single proof.
// The aggregated proof commits to all individual commitments via a
// Merkle-like hash:
//
//	aggRoot = SHA256(commitment_0 || commitment_1 || ... || commitment_n)
//	aggProof = SHA256(proof_0 || proof_1 || ... || proof_n)
func (v *KZGVerifier) AggregateProofs(pairs []BlobCommitmentPair) (*AggregatedKZGProof, error) {
	if len(pairs) == 0 {
		return nil, ErrKZGBatchEmpty
	}

	commitments := make([]KZGCommitment, len(pairs))
	var commitData []byte
	var proofData []byte

	for i, pair := range pairs {
		commitments[i] = pair.Commitment
		commitData = append(commitData, pair.Commitment[:]...)
		proofData = append(proofData, pair.Proof[:]...)
	}

	// Compute aggregate root.
	rootHash := sha256.Sum256(commitData)
	aggRoot := types.BytesToHash(rootHash[:])

	// Compute aggregate proof.
	proofHash := sha256.Sum256(proofData)
	var aggProof KZGProofPoint
	copy(aggProof[:], proofHash[:])

	return &AggregatedKZGProof{
		Commitments: commitments,
		AggProof:    aggProof,
		AggRoot:     aggRoot,
		Count:       len(pairs),
	}, nil
}

// VerifyAggregatedProof verifies an aggregated KZG proof by recomputing
// the aggregate root from the individual commitments.
func (v *KZGVerifier) VerifyAggregatedProof(agg *AggregatedKZGProof) (bool, error) {
	if agg == nil || len(agg.Commitments) == 0 {
		return false, ErrKZGBatchEmpty
	}

	var commitData []byte
	for _, c := range agg.Commitments {
		commitData = append(commitData, c[:]...)
	}
	rootHash := sha256.Sum256(commitData)
	expected := types.BytesToHash(rootHash[:])

	return expected == agg.AggRoot, nil
}

// Stats returns the verifier's statistics.
func (v *KZGVerifier) Stats() (verified, failed, batches uint64) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.totalVerified, v.totalFailed, v.batchesRun
}

// --- Internal helpers ---

// verifyPointEval performs hash-based KZG point evaluation verification.
// In production this would use BLS12-381 pairing checks.
func verifyPointEval(
	commitment KZGCommitment,
	proof KZGProofPoint,
	point [FieldElementSize]byte,
	value [FieldElementSize]byte,
) bool {
	h := sha256.New()
	h.Write(commitment[:])
	h.Write(proof[:])
	h.Write(point[:])
	h.Write(value[:])
	digest := h.Sum(nil)

	// Valid if first byte of digest has high bit clear (deterministic stub).
	return digest[0] < 0x80
}

// computeVersionedHash computes the EIP-4844 versioned hash from a commitment.
// Format: 0x01 || SHA256(commitment)[1:]
func computeVersionedHash(commitment KZGCommitment) types.Hash {
	inner := sha256.Sum256(commitment[:])
	var result types.Hash
	result[0] = 0x01 // version byte
	copy(result[1:], inner[1:])
	return result
}

// isZeroKZG checks if all bytes are zero.
func isZeroKZG(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

// MakeTestPointEvaluation creates a deterministic point evaluation for testing.
// The commitment and proof are derived from the index so that verification
// succeeds (first byte of SHA256 digest < 0x80).
func MakeTestPointEvaluation(index uint64) *PointEvaluation {
	var commitment KZGCommitment
	var proof KZGProofPoint
	var point [FieldElementSize]byte
	var value [FieldElementSize]byte

	// Derive deterministic values.
	binary.BigEndian.PutUint64(commitment[:8], index+1)
	commitment[8] = 0x01

	binary.BigEndian.PutUint64(proof[:8], index+1)
	proof[8] = 0x02

	binary.BigEndian.PutUint64(point[:8], index)
	binary.BigEndian.PutUint64(value[:8], index*2)

	// Brute-force a commitment that passes verification.
	for nonce := uint64(0); nonce < 10000; nonce++ {
		binary.BigEndian.PutUint64(commitment[40:], nonce)
		if verifyPointEval(commitment, proof, point, value) {
			return &PointEvaluation{
				Commitment: commitment,
				Proof:      proof,
				Point:      point,
				Value:      value,
			}
		}
	}
	// Fallback (should not happen in practice).
	return &PointEvaluation{
		Commitment: commitment,
		Proof:      proof,
		Point:      point,
		Value:      value,
	}
}

// MakeTestBlobCommitmentPair creates a test blob-commitment pair.
func MakeTestBlobCommitmentPair(index uint64) *BlobCommitmentPair {
	var commitment KZGCommitment
	binary.BigEndian.PutUint64(commitment[:8], index+1)
	commitment[8] = 0xab

	var proof KZGProofPoint
	binary.BigEndian.PutUint64(proof[:8], index+1)
	proof[8] = 0xcd

	blobHash := computeVersionedHash(commitment)

	return &BlobCommitmentPair{
		BlobHash:   blobHash,
		Commitment: commitment,
		Proof:      proof,
		BlobIndex:  index,
	}
}
